package ethrd

import (
	"testing"

	"github.com/mpolitzer/ethrd/db"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
)

func TestReconcile(t *testing.T) {
	const confirmTags = db.TagFinalized | db.TagSafe | db.TagLatest
	typesLogs := []types.Log{
		{BlockNumber: 1, BlockHash: common.HexToHash("0x01")},
		{BlockNumber: 2, BlockHash: common.HexToHash("0x02")},
		{BlockNumber: 3, BlockHash: common.HexToHash("0x03")},
	}
	// Speculated rows as the fast path would have inserted them.
	dbLogs := []db.Log{
		dbLog(typesLogs[0], db.TagLatest),
		dbLog(typesLogs[1], db.TagLatest),
		dbLog(typesLogs[2], db.TagLatest),
	}
	tomb := func(l db.Log) db.Log {
		l.Tags |= db.TagRemoved
		return l
	}

	tests := []struct {
		name       string
		observed   []types.Log
		speculated []db.Log
		want       []db.Log
	}{
		{
			name:       "confirm and create",
			observed:   typesLogs[0:2],
			speculated: []db.Log{dbLogs[0]},
			want: []db.Log{
				dbLog(typesLogs[0], confirmTags),
				dbLog(typesLogs[1], confirmTags),
			},
		},
		{
			name:       "tombstone_create_trailing_tombstone",
			observed:   typesLogs[1:2],
			speculated: []db.Log{dbLogs[0], dbLogs[2]},
			want: []db.Log{
				tomb(dbLogs[0]),
				dbLog(typesLogs[1], confirmTags),
				tomb(dbLogs[2]),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, reconcile(test.observed, test.speculated, confirmTags))
		})
	}
}

// TestOrd pins the index level of the merge key; the block-number and
// block-hash levels are exercised by TestReconcile.
func TestOrd(t *testing.T) {
	assert.Equal(t, -1, ord(
		types.Log{BlockNumber: 1, Index: 1},
		db.Log{BlockNumber: 1, LogIndex: 2},
	))
}
