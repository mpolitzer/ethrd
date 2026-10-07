//go:build e2e

package ethrd

import (
	"context"
	"database/sql"
	"log/slog"
	"math/big"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/mpolitzer/ethrd/db"
	"github.com/mpolitzer/ethrd/fixture"
	"github.com/mpolitzer/redacted"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// e2eConf returns a fast-polling conf bootstrapped from combined-abi.json.
func e2eConf(t *testing.T) *Conf {
	t.Helper()
	abiBytes, err := os.ReadFile("combined-abi.json")
	require.NoError(t, err)
	return &Conf{
		PollInterval: 50 * time.Millisecond,
		LogBufSize:   16,
		Filter:       FilterConf{ChunkSize: 8, BurstLimit: 100, RateLimit: 1000},
		Bootstrap: &BootstrapConf{
			BlockNumber: 0,
			CombinedABI: redacted.NewValue(abiBytes),
		},
	}
}

// simEnv is a simulated blockchain + in-memory sqlite + State, with the
// fixture contract deployed at block 1.
type simEnv struct {
	t        *testing.T
	sim      *simulated.Backend
	client   simulated.Client
	auth     *bind.TransactOpts
	contract *fixture.Fixture
	instance *bind.BoundContract
	db       *sql.DB
	q        *db.Queries
	state    *State
}

func newSimEnv(t *testing.T, conf *Conf) *simEnv {
	t.Helper()
	ctx := context.Background()

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	auth := bind.NewKeyedTransactor(key, params.AllDevChainProtocolChanges.ChainID)
	sim := simulated.NewBackend(map[common.Address]types.Account{
		auth.From: {Balance: big.NewInt(9e18)},
	})
	t.Cleanup(func() { _ = sim.Close() })
	client := sim.Client()

	dbHandle, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbHandle.Close() })

	// Deploy the fixture contract -> block 1.
	res, err := bind.LinkAndDeploy(
		&bind.DeploymentParams{Contracts: []*bind.MetaData{&fixture.FixtureMetaData}},
		bind.DefaultDeployer(auth, client),
	)
	require.NoError(t, err)
	sim.Commit()
	addr, err := bind.WaitDeployed(ctx, client, res.Txs[fixture.FixtureMetaData.ID].Hash())
	require.NoError(t, err)

	contract := fixture.NewFixture()
	e := &simEnv{
		t:        t,
		sim:      sim,
		client:   client,
		auth:     auth,
		contract: contract,
		instance: contract.Instance(client, addr),
		db:       dbHandle,
		q:        db.New(dbHandle),
	}
	e.state, err = New(ctx, conf, slog.New(slog.DiscardHandler), client, dbHandle)
	require.NoError(t, err)
	return e
}

// set sends fixture.set(id) and mines it into a new block.
func (e *simEnv) set(id int64) *types.Header {
	e.t.Helper()
	_, err := bind.Transact(e.instance, e.auth, e.contract.PackSet(big.NewInt(id)))
	require.NoError(e.t, err)
	e.sim.Commit()
	return e.latest()
}

func (e *simEnv) latest() *types.Header {
	e.t.Helper()
	return e.headerByNumber(rpc.LatestBlockNumber)
}

func (e *simEnv) header(n uint64) *types.Header {
	e.t.Helper()
	return e.headerByNumber(rpc.BlockNumber(n))
}

func (e *simEnv) headerByNumber(n rpc.BlockNumber) *types.Header {
	e.t.Helper()
	h, err := e.client.HeaderByNumber(context.Background(), big.NewInt(int64(n)))
	require.NoError(e.t, err)
	return h
}

// mineTo commits empty blocks until the head reaches n.
func (e *simEnv) mineTo(n uint64) {
	e.t.Helper()
	for h := e.latest(); h.Number.Uint64() < n; h = e.latest() {
		e.sim.Commit()
	}
}

// waitFor polls cond every 10ms until it is true or d elapses.
func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.True(t, cond(), "condition not met within %s", d)
}

// blockLog returns the single log of canonical block n.
func blockLog(t *testing.T, e *simEnv, n uint64) types.Log {
	t.Helper()
	logs, err := e.client.FilterLogs(context.Background(), ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(n)),
		ToBlock:   big.NewInt(int64(n)),
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	return logs[0]
}

func rowByHash(t *testing.T, rows []db.Log, h common.Hash) db.Log {
	t.Helper()
	for _, r := range rows {
		if r.BlockHash == h {
			return r
		}
	}
	t.Fatalf("no row for block hash %s", h.Hex())
	return db.Log{}
}

// hasTags reports whether the tag sets observed for block hash h contain all
// of tags.
func hasTags(seen map[common.Hash]map[uint64]struct{}, h common.Hash, tags ...uint64) bool {
	for _, tag := range tags {
		if _, ok := seen[h][tag]; !ok {
			return false
		}
	}
	return true
}

func TestRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	confirmTags := db.TagLatest | db.TagSafe | db.TagFinalized

	// Mine log A before Run starts: the startup latestChunk scan (head is 2,
	// not 1) persists it. That row appearing is the sync point — the
	// subscription is registered right before that scan, so the fast path is
	// live from here on. Starting Run first would race the subscription
	// registration against set's Commit.
	e := newSimEnv(t, e2eConf(t))
	a := e.set(1) // block 2, log A

	var mu sync.Mutex
	seen := map[common.Hash]map[uint64]struct{}{} // block hash -> observed tag values
	var runErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev, err := range e.state.Run(ctx) {
			if err != nil {
				runErr = err
				return
			}
			mu.Lock()
			if seen[ev.BlockHash] == nil {
				seen[ev.BlockHash] = make(map[uint64]struct{})
			}
			seen[ev.BlockHash][ev.Tags] = struct{}{}
			mu.Unlock()
		}
	}()

	waitFor(t, 10*time.Second, func() bool {
		rows, err := e.q.SelectLogs(ctx, db.SelectLogsParams{Lo: 0, Hi: 100})
		return err == nil && len(rows) == 1
	})

	c := e.set(3) // block 3, log C: delivered by the subscription
	e.mineTo(32)

	waitFor(t, 10*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hasTags(seen, a.Hash(), db.TagLatest, confirmTags) &&
			hasTags(seen, c.Hash(), db.TagLatest, confirmTags)
	})

	cancel()
	<-done
	// Cancelling mid-scan surfaces that scan's ctx error; any other error
	// is a failure.
	if runErr != nil {
		require.ErrorIs(t, runErr, context.Canceled)
	}

	rows, err := e.q.SelectLogs(context.Background(), db.SelectLogsParams{Lo: 0, Hi: 100})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.Equal(t, confirmTags, r.Tags)
	}
	cursor, err := e.q.GetCursor(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(33), cursor)
}

func TestScan(t *testing.T) {
	ctx := context.Background()
	confirmTags := db.TagLatest | db.TagSafe | db.TagFinalized

	// Block 2 holds log A, reorged into log B, then mined to 32.
	e := newSimEnv(t, e2eConf(t))
	e.set(1) // block 2, log A
	a := blockLog(t, e, 2)
	require.NoError(t, e.sim.Fork(e.header(1).Hash()))
	e.sim.Rollback() // Fork re-queues mined txs; clear the pool for a clean reorg
	e.set(2)         // block 2', log B
	b := blockLog(t, e, 2)
	require.NotEqual(t, a.BlockHash, b.BlockHash)
	// Seed the speculative state exactly as the fast path would.
	_, err := e.q.ApplyLogEdit(ctx, dbLog(a, db.TagLatest))
	require.NoError(t, err)
	e.mineTo(32)

	var edits []db.Log
	ok, err := e.state.Scan(ctx, rpc.FinalizedBlockNumber, finalizedChunk,
		func(l db.Log) bool { edits = append(edits, l); return true })
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, edits, 2)

	byHash := make(map[common.Hash]db.Log, len(edits))
	for _, l := range edits {
		byHash[l.BlockHash] = l
	}
	assert.Equal(t, db.TagLatest|db.TagRemoved, byHash[a.BlockHash].Tags) // del(A)
	assert.Equal(t, confirmTags, byHash[b.BlockHash].Tags)                // new(B)

	rows, err := e.q.SelectLogs(ctx, db.SelectLogsParams{Lo: 0, Hi: 100})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, db.TagLatest|db.TagRemoved, rowByHash(t, rows, a.BlockHash).Tags)
	assert.Equal(t, confirmTags, rowByHash(t, rows, b.BlockHash).Tags)

	cursor, err := e.q.GetCursor(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(33), cursor)

	// Re-scanning a settled range is a no-op: every write is idempotent (doc/ethrd.md).
	edits = nil
	ok, err = e.state.Scan(ctx, rpc.FinalizedBlockNumber, finalizedChunk,
		func(l db.Log) bool { edits = append(edits, l); return true })
	require.NoError(t, err)
	require.True(t, ok)
	assert.Empty(t, edits)
}
