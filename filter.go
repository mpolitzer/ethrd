package ethrd

import (
	"context"
	"iter"
	"math/big"
	"slices"

	"github.com/codeGROOVE-dev/retry"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/time/rate"
)

var (
	one = big.NewInt(1)
)

type FilterConf struct {
	ChunkSize  uint64     `yaml:"chunk-size"`
	BurstLimit int        `yaml:"burst-limit"`
	RateLimit  rate.Limit `yaml:"rate-limit"`
}

// FilterChunk is one contiguous block range [Lo, Hi] and the logs found in it.
type FilterChunk struct {
	Lo   uint64
	Hi   uint64
	Logs []types.Log
}

type FilterState struct {
	ChunkSize *big.Int
	Limiter   *rate.Limiter
	RetryOpts []retry.Option // empty for defaults (here for testability)

	// The Topic list restricts matches to particular event topics. Each event has a list
	// of topics. Topics matches a prefix of that list. An empty element slice matches any
	// topic. Non-empty elements represent an alternative that matches any of the
	// contained topics.
	//
	// Examples:
	// {} or nil          matches any topic list
	// {{A}}              matches topic A in first position
	// {{}, {B}}          matches any topic in first position AND B in second position
	// {{A}, {B}}         matches topic A in first position AND B in second position
	// {{A, B}, {C, D}}   matches topic (A OR B) in first position AND (C OR D) in second position
	Topics    [][]common.Hash
	Addresses []common.Address // restricts matches to events created by specific contracts
}

// filterLogs scans [from, to] for logs matching Addresses/Topics, yielding
// one chunk per ChunkSize blocks, each paced by Limiter and fetched through
// retry (RetryOpts + ctx).
//
// It yields (zero, err) at most once; the caller resumes from the first
// unprocessed block (last chunk.Hi+1, or from if nothing was scanned).
// Cancelling ctx stops the scan; resuming is safe.
//
// Within a run each log is delivered exactly once; across runs delivery is
// at-least-once, so consumers must deduplicate.
//
// Precondition: ChunkSize >= 1, Limiter non-nil.
func (s *FilterState) filterLogs(
	ctx context.Context,
	client Client,
	from uint64,
	to uint64,
) iter.Seq2[FilterChunk, error] {
	return func(yield func(FilterChunk, error) bool) {
		if to < from {
			return
		}
		retryOpts := slices.Concat(s.RetryOpts, []retry.Option{retry.Context(ctx)})
		fromBlock := new(big.Int).SetUint64(from)
		toBlock := new(big.Int).SetUint64(to)

		fq := ethereum.FilterQuery{
			FromBlock: new(big.Int).Set(fromBlock),
			ToBlock: new(big.Int).Add(
				fromBlock,
				new(big.Int).Sub(s.ChunkSize, one),
			),
			Addresses: s.Addresses,
			Topics:    s.Topics,
		}

		if fq.ToBlock.Cmp(toBlock) > 0 {
			fq.ToBlock.Set(toBlock)
		}

		for fq.FromBlock.Cmp(toBlock) <= 0 {
			if err := ctx.Err(); err != nil {
				yield(FilterChunk{}, err)
				return
			}

			if err := s.Limiter.Wait(ctx); err != nil {
				yield(FilterChunk{}, err)
				return
			}

			logs, err := retry.DoWithData(func() ([]types.Log, error) {
				return client.FilterLogs(ctx, fq)
			}, retryOpts...)
			if err != nil {
				yield(FilterChunk{}, err)
				return
			}

			if !yield(
				FilterChunk{
					Lo:   fq.FromBlock.Uint64(),
					Hi:   fq.ToBlock.Uint64(),
					Logs: logs,
				},
				nil,
			) {
				return
			}

			// FilterQuery is closed on both ends: [FromBlock, ToBlock]. A
			// increment on both values would result in repetitions, and
			// thus duplicate events, a.k.a, a bug. So we do this little
			// "dance" below instead.
			// ------------------------
			//  [  delta  |  delta  ]
			//  ^         ^
			// From       To
			// ------------------------
			//  [  delta  |  delta  ]
			//             ^        ^
			//            From      To
			// ------------------------
			fq.FromBlock.Add(fq.ToBlock, one)
			fq.ToBlock.Add(fq.ToBlock, s.ChunkSize)
			if fq.ToBlock.Cmp(toBlock) > 0 {
				fq.ToBlock.Set(toBlock)
			}
		}
	}
}

func (f *FilterState) GetBlockNumber(
	ctx context.Context,
	client Client,
	bn rpc.BlockNumber,
) (uint64, error) {
	retryOpts := slices.Concat(f.RetryOpts, []retry.Option{retry.Context(ctx)})
	hdr, err := retry.DoWithData(func() (*types.Header, error) {
		return client.HeaderByNumber(ctx, big.NewInt(int64(bn)))
	}, retryOpts...)
	if err != nil {
		return 0, err
	}
	return hdr.Number.Uint64(), nil
}
