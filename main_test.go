package ethrd

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/mpolitzer/ethrd/db"
	"github.com/mpolitzer/redacted"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbHandle, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbHandle.Close() })
	require.NoError(t, db.Migrate(slog.New(slog.DiscardHandler), dbHandle))
	return dbHandle
}

func newTestState(t *testing.T, c Client) *State {
	t.Helper()
	d := newTestDB(t)
	require.NoError(t, db.New(d).UpsertState(context.Background(), db.UpsertStateParams{
		Frontier:    0,
		CombinedAbi: "{}",
	}))
	return &State{
		db:      d,
		queries: db.New(d),
		filter: FilterState{
			ChunkSize: big.NewInt(8),
			Limiter:   rate.NewLimiter(1000, 8),
		},
		client: c,
		logger: slog.New(slog.DiscardHandler),
	}
}

func TestNew(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.DiscardHandler)

	abiBytes, err := os.ReadFile("combined-abi.json")
	require.NoError(t, err)
	base := func() *Conf {
		return &Conf{
			PollInterval: time.Second,
			LogBufSize:   16,
			Filter:       FilterConf{ChunkSize: 8, BurstLimit: 8, RateLimit: 100},
			Bootstrap: &BootstrapConf{
				BlockNumber: 0,
				CombinedABI: redacted.NewValue(abiBytes),
			},
		}
	}

	t.Run("bootstrap and reuse", func(t *testing.T) {
		d := newTestDB(t)
		_, err := New(ctx, base(), logger, nil, d) // bootstrap creates the state row
		require.NoError(t, err)
		q := db.New(d)
		gotABI, err := q.GetCombinedABI(ctx)
		require.NoError(t, err)
		assert.Equal(t, string(abiBytes), gotABI)
		cursor, err := q.GetCursor(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), cursor)

		conf := base()
		conf.Bootstrap = nil
		_, err = New(ctx, conf, logger, nil, d)
		require.NoError(t, err)
	})

	t.Run("missing state", func(t *testing.T) {
		d := newTestDB(t)
		conf := base()
		conf.Bootstrap = nil
		_, err := New(ctx, conf, logger, nil, d)
		assert.ErrorContains(t, err, "state not found")
	})

	t.Run("bad abi", func(t *testing.T) {
		d := newTestDB(t)
		conf := base()
		conf.Bootstrap.CombinedABI = redacted.NewValue([]byte("{"))
		_, err := New(ctx, conf, logger, nil, d)
		assert.ErrorContains(t, err, "parse combined-abi")
	})

	t.Run("invalid conf", func(t *testing.T) {
		tests := []struct {
			name string
			mut  func(*Conf)
		}{
			{"poll-interval", func(c *Conf) { c.PollInterval = 0 }},
			{"log-buf-size", func(c *Conf) { c.LogBufSize = 0 }},
			{"chunk-size", func(c *Conf) { c.Filter.ChunkSize = 0 }},
			{"burst-limit", func(c *Conf) { c.Filter.BurstLimit = 0 }},
			{"rate-limit", func(c *Conf) { c.Filter.RateLimit = 0 }},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				d := newTestDB(t)
				conf := base()
				test.mut(conf)
				_, err := New(ctx, conf, logger, nil, d)
				assert.Error(t, err)
			})
		}
	})
}

type stubClient struct {
	nr   uint64
	logs []types.Log
}

func (s *stubClient) FilterLogs(context.Context, ethereum.FilterQuery) ([]types.Log, error) {
	return s.logs, nil
}

func (s *stubClient) SubscribeFilterLogs(context.Context, ethereum.FilterQuery, chan<- types.Log) (ethereum.Subscription, error) {
	panic("stubClient: not implemented")
}

func (s *stubClient) HeaderByNumber(context.Context, *big.Int) (*types.Header, error) {
	return &types.Header{Number: big.NewInt(int64(s.nr))}, nil
}

func TestScanControl(t *testing.T) {
	ctx := context.Background()
	log := types.Log{
		BlockNumber: 2,
		BlockHash:   common.HexToHash("0x02"),
		Address:     common.HexToAddress("0x01"),
		Data:        []byte{}, // chain logs always carry non-nil data
	}

	t.Run("consumer stop", func(t *testing.T) {
		s := newTestState(t, &stubClient{nr: 5, logs: []types.Log{log}})
		ok, err := s.Scan(ctx, rpc.LatestBlockNumber, latestChunk, func(db.Log) bool { return false })
		assert.False(t, ok)
		assert.NoError(t, err)
	})

	t.Run("onChunk error", func(t *testing.T) {
		s := newTestState(t, &stubClient{nr: 5, logs: []types.Log{log}})
		boom := errors.New("boom")
		ok, err := s.Scan(ctx, rpc.LatestBlockNumber,
			func(context.Context, *sql.DB, FilterChunk) ([]db.Log, error) { return nil, boom },
			func(db.Log) bool { return true })
		assert.False(t, ok)
		assert.ErrorIs(t, err, boom)
	})

	t.Run("ctx canceled", func(t *testing.T) {
		s := newTestState(t, &stubClient{nr: 5})
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		ok, err := s.Scan(cctx, rpc.LatestBlockNumber, latestChunk, func(db.Log) bool { return true })
		assert.False(t, ok)
		assert.Error(t, err)
	})
}
