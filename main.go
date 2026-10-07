package ethrd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"math/big"
	"time"

	"github.com/mpolitzer/ethrd/db"
	"github.com/mpolitzer/redacted"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/time/rate"
)

var errNoSubscriptions = errors.New("provider does not support subscriptions: use a websocket or ipc endpoint")

type Client interface {
	FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error)
	SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
}

type BootstrapConf struct {
	BlockNumber uint64                 `yaml:"block-number"`
	CombinedABI redacted.Value[[]byte] `yaml:"combined-abi"`
}

type Conf struct {
	PollInterval time.Duration  `yaml:"poll-interval"`
	Filter       FilterConf     `yaml:"filter"`
	Bootstrap    *BootstrapConf `yaml:"bootstrap"`
	LogBufSize   uint64         `yaml:"log-buf-size"`
}

type State struct {
	db           *sql.DB
	client       Client
	filter       FilterState
	queries      *db.Queries
	demux        Demux
	poll         time.Duration
	subscription event.Subscription
	logs         chan types.Log
	logger       *slog.Logger
}

type cABI map[string]abi.ABI

func (cABI cABI) Topics() []common.Hash {
	seen := make(map[common.Hash]struct{}, len(cABI))
	topics := make([]common.Hash, 0, len(cABI))
	for _, entry := range cABI {
		for _, event := range entry.Events {
			if event.Anonymous {
				continue
			}
			if _, ok := seen[event.ID]; ok {
				continue
			}
			seen[event.ID] = struct{}{}
			topics = append(topics, event.ID)
		}
	}
	return topics
}

func New(
	ctx context.Context,
	conf *Conf,
	logger *slog.Logger,
	client Client,
	mainDB *sql.DB,
) (*State, error) {
	q := db.New(mainDB)

	if err := db.Migrate(logger, mainDB); err != nil {
		return nil, err
	}

	// bootstrap checks and cABI (combined) parse
	var abiContents []byte
	if conf.Bootstrap != nil {
		abiContents = conf.Bootstrap.CombinedABI.Value()
	} else {
		abiDB, err := q.GetCombinedABI(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, errors.New("state not found: configure bootstrap (block-number, combined-abi)")
			}
			return nil, fmt.Errorf("select state: %w", err)
		}
		abiContents = []byte(abiDB)
	}
	var cABI cABI
	if err := json.Unmarshal(abiContents, &cABI); err != nil {
		return nil, fmt.Errorf("parse combined-abi: %w", err)
	}
	demux, err := cABI.New()
	if err != nil {
		return nil, fmt.Errorf("build demuxer: %w", err)
	}
	if conf.Bootstrap != nil {
		if err := q.UpsertState(ctx, db.UpsertStateParams{
			Frontier:    conf.Bootstrap.BlockNumber,
			CombinedAbi: string(abiContents),
		}); err != nil {
			return nil, fmt.Errorf("upsert state: %w", err)
		}
	}

	// poll checks
	if conf.PollInterval <= 0 {
		return nil, errors.New("invalid poll-interval <= 0")
	}
	if conf.LogBufSize == 0 {
		return nil, errors.New("invalid log-buf-size == 0")
	}

	// filter checks
	if conf.Filter.ChunkSize == 0 {
		return nil, errors.New("invalid chunk-size == 0")
	}
	if conf.Filter.BurstLimit == 0 {
		return nil, errors.New("invalid burst-limit == 0")
	}
	if conf.Filter.RateLimit == 0 {
		return nil, errors.New("invalid rate-limit == 0")
	}

	// client checks: clients that expose the underlying *rpc.Client (e.g.
	// *ethclient.Client) are checked for subscription support; others are trusted.
	if rc, ok := client.(interface{ Client() *rpc.Client }); ok && !rc.Client().SupportsSubscriptions() {
		return nil, errNoSubscriptions
	}

	return &State{
		db: mainDB,
		filter: FilterState{
			Topics:    [][]common.Hash{cABI.Topics()},
			ChunkSize: new(big.Int).SetUint64(conf.Filter.ChunkSize),
			Limiter:   rate.NewLimiter(conf.Filter.RateLimit, conf.Filter.BurstLimit),
		},
		queries: q,
		demux:   demux,
		client:  client,
		poll:    conf.PollInterval,
		logs:    make(chan types.Log, conf.LogBufSize),
		logger:  logger,
	}, nil
}

// Run ingests logs via subscription and scan (doc/ethrd.md), yielding every
// persisted edit after commit. Tags mark the delivery: TagLatest for the
// fast path, TagLatest|TagSafe|TagFinalized for confirms, tags|TagRemoved
// for tombstones.
//
// Delivery is pull-based: the producer advances only as the consumer ranges.
// The same log is yielded at latest and again at finalized; consumers keep
// the highest commitment per identity (block_hash, log_index). Edits are
// block-ordered within a scan; across paths interleaving is possible.
//
// It yields (zero, err) at most once on failure, and returns when ctx is
// done or the consumer stops ranging.
func (s *State) Run(ctx context.Context) iter.Seq2[db.Log, error] {
	return func(yield func(db.Log, error) bool) {
		emit := func(ev db.Log) bool {
			return yield(ev, nil)
		}

		if ok, err := s.Scan(ctx, rpc.FinalizedBlockNumber, finalizedChunk, emit); !ok {
			if err != nil {
				yield(db.Log{}, err)
			}
			return
		}
		s.subscription = event.ResubscribeErr(
			s.poll,
			func(ctx context.Context, err error) (event.Subscription, error) {
				return s.client.SubscribeFilterLogs(ctx, ethereum.FilterQuery{
					Addresses: s.filter.Addresses,
					Topics:    s.filter.Topics,
				}, s.logs)
			})
		if ok, err := s.Scan(ctx, rpc.LatestBlockNumber, latestChunk, emit); !ok {
			if err != nil {
				yield(db.Log{}, err)
			}
			return
		}

		ticker := time.NewTicker(s.poll)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case log := <-s.logs:
				if log.Removed {
					continue
				}
				edit := dbLog(log, db.TagLatest)
				if _, err := s.queries.ApplyLogEdit(ctx, edit); err != nil {
					yield(db.Log{}, err)
					return
				}
				if !emit(edit) {
					return
				}
			case <-ticker.C:
				if ok, err := s.Scan(ctx, rpc.FinalizedBlockNumber, finalizedChunk, emit); !ok {
					if err != nil {
						yield(db.Log{}, err)
					}
					return
				}
			}
		}
	}
}

// Scan reads, reconciles and persists logs in [cursor, nr], yielding each
// persisted edit via emit.
//
// It returns (false, nil) when the consumer stops ranging, (false, err) on
// failure, and (true, nil) when the range is exhausted.
func (s *State) Scan(
	ctx context.Context,
	commitment rpc.BlockNumber,
	onChunk func(context.Context, *sql.DB, FilterChunk) ([]db.Log, error),
	emit func(db.Log) bool,
) (bool, error) {
	nr, err := s.filter.GetBlockNumber(ctx, s.client, commitment)
	if err != nil {
		return false, err
	}

	cursor, err := s.queries.GetCursor(ctx)
	if err != nil {
		return false, err
	}

	s.logger.Debug("scan", "from", cursor, "to", nr)
	for observed, err := range s.filter.filterLogs(ctx, s.client, cursor, nr) {
		if err != nil {
			return false, err
		}
		edits, err := onChunk(ctx, s.db, observed)
		if err != nil {
			return false, err
		}
		for _, e := range edits {
			if !emit(e) {
				return false, nil
			}
		}
	}
	return true, nil
}

// finalizedChunk reconciles a chunk against speculative rows, persists the
// result, and returns the applied edits.
func finalizedChunk(
	ctx context.Context,
	DB *sql.DB,
	observed FilterChunk,
) ([]db.Log, error) {
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit
	qtx := db.New(DB).WithTx(tx)

	speculativeLogs, err := qtx.SelectLogs(ctx, db.SelectLogsParams{Lo: observed.Lo, Hi: observed.Hi})
	if err != nil {
		return nil, err
	}

	tags := db.TagFinalized | db.TagSafe | db.TagLatest
	edits := reconcile(observed.Logs, speculativeLogs, tags)
	for _, l := range edits {
		if _, err := qtx.ApplyLogEdit(ctx, l); err != nil {
			return nil, err
		}
	}

	if err := qtx.AdvanceFinalizedFrontier(ctx, observed.Hi+1); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return edits, nil
}

// latestChunk persists a chunk as TagLatest and returns the applied edits.
func latestChunk(
	ctx context.Context,
	DB *sql.DB,
	observed FilterChunk,
) ([]db.Log, error) {
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit
	qtx := db.New(DB).WithTx(tx)

	edits := make([]db.Log, 0, len(observed.Logs))
	for _, log := range observed.Logs {
		edit := dbLog(log, db.TagLatest)
		if _, err := qtx.ApplyLogEdit(ctx, edit); err != nil {
			return nil, err
		}
		edits = append(edits, edit)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return edits, nil
}
