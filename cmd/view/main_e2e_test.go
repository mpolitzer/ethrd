//go:build e2e

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	ethrd "github.com/mpolitzer/ethrd"
	"github.com/mpolitzer/ethrd/db"
	"github.com/mpolitzer/ethrd/fixture"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// seedDB migrates a temp db file, stores the combined ABI, and writes n logs
// (one per block 1..n) of the fixture's updated event.
func seedDB(t *testing.T, n uint64) (*db.Queries, ethrd.Demux) {
	t.Helper()
	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "log.db")
	sqlDB, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(slog.New(slog.DiscardHandler), sqlDB))

	abi, err := os.ReadFile(filepath.Join("..", "..", "combined-abi.json"))
	require.NoError(t, err)

	q := db.New(sqlDB)
	require.NoError(t, q.UpsertState(ctx, db.UpsertStateParams{Frontier: 0, CombinedAbi: string(abi)}))

	demux, err := ethrd.NewDemux(abi)
	require.NoError(t, err)

	event := fixture.NewFixture().GetABI().Events["updated"]
	for i := uint64(1); i <= n; i++ {
		var l db.Log
		l.From(types.Log{
			Address:     common.HexToAddress("0x1"),
			Topics:      []common.Hash{event.ID, common.BytesToHash(common.LeftPadBytes(big.NewInt(int64(i)).Bytes(), 32))},
			Data:        common.LeftPadBytes(big.NewInt(int64(2 * i)).Bytes(), 32),
			BlockNumber: i,
			BlockHash:   common.BigToHash(big.NewInt(int64(i))),
			TxHash:      common.BigToHash(big.NewInt(int64(i) + 1000)),
		}, db.TagLatest|db.TagSafe|db.TagFinalized)
		_, err := q.ApplyLogEdit(ctx, l)
		require.NoError(t, err)
	}
	return q, demux
}

// newData mirrors main's construction (lazy fetch: no pages preloaded).
func newData(q *db.Queries, demux ethrd.Demux) *data {
	return &data{
		q:     q,
		demux: demux,
		ascw:  db.SelectLogsWindowAscParams{Limit: chunkSize},
		descw: db.SelectLogsWindowDescParams{BlockNumber: math.MaxInt64, LogIndex: math.MaxInt64, Limit: chunkSize},
	}
}

func TestDescPaging(t *testing.T) {
	ctx := context.Background()
	const n = 3 * chunkSize
	d := newData(seedDB(t, n))

	log, err := d.at(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(n), log.BlockNumber)
	assert.Len(t, d.descl, 1)

	log, err = d.at(ctx, chunkSize-1)
	require.NoError(t, err)
	assert.Equal(t, uint64(n-chunkSize+1), log.BlockNumber)

	// Crossing the page boundary lazily fetches page 2.
	log, err = d.at(ctx, chunkSize)
	require.NoError(t, err)
	assert.Equal(t, uint64(n-chunkSize), log.BlockNumber)
	assert.Len(t, d.descl, 2)

	log, err = d.at(ctx, n-1)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), log.BlockNumber)
	assert.Len(t, d.descl, 3)

	_, err = d.at(ctx, n)
	assert.ErrorIs(t, err, oob)
}

func TestAscPaging(t *testing.T) {
	ctx := context.Background()
	const n = 3 * chunkSize
	d := newData(seedDB(t, n))

	// The G key path: tview's end() selects row MaxInt64-1, the asc branch.
	log, err := d.at(ctx, math.MaxInt64-1)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), log.BlockNumber)
	assert.Len(t, d.ascl, 1)

	log, err = d.at(ctx, math.MaxInt64-2)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), log.BlockNumber)

	log, err = d.at(ctx, math.MaxInt64-1-(chunkSize-1))
	require.NoError(t, err)
	assert.Equal(t, uint64(chunkSize), log.BlockNumber)

	// Crossing the page boundary lazily fetches page 2.
	log, err = d.at(ctx, math.MaxInt64-1-chunkSize)
	require.NoError(t, err)
	assert.Equal(t, uint64(chunkSize+1), log.BlockNumber)
	assert.Len(t, d.ascl, 2)

	// The middle of the table is the gap between the two windows.
	_, err = d.at(ctx, math.MaxInt64/2+1)
	assert.ErrorIs(t, err, oob)
}

func TestGetCell(t *testing.T) {
	const n = 3 * chunkSize
	d := newData(seedDB(t, n))

	cell := d.GetCell(0, 0)
	require.NotNil(t, cell)
	assert.Contains(t, cell.Text, "Fixture.updated")
	assert.Contains(t, cell.Text, fmt.Sprintf("%d", n))

	cell = d.GetCell(n, 0) // oob -> placeholder
	require.NotNil(t, cell)
	assert.Contains(t, cell.Text, "-")
}
