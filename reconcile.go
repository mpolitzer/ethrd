package ethrd

import (
	"bytes"
	"cmp"

	"github.com/mpolitzer/ethrd/db"

	"github.com/ethereum/go-ethereum/core/types"
)

// reconcile merge-joins O (observed) against S (speculated), both sorted by
// the merge key (block_number, index, block_hash), a total order consistent
// with the log identity (block_hash, log_index) in doc/ethrd.md.
//
// The scan is the arbiter: everything in O is true; everything in S that O
// does not confirm is false. The result is one edit per differing entry, in
// non-decreasing block order: observed logs carry confirmTags, reorged logs
// carry db.TagRemoved.
//
// O(len(O)+len(S)), no allocation beyond the result.
func reconcile(observed []types.Log, speculated []db.Log, confirmTags uint64) []db.Log {
	result := make([]db.Log, 0, len(observed)+len(speculated))
	o, s := 0, 0
	for o < len(observed) && s < len(speculated) {
		switch ord(observed[o], speculated[s]) {
		case 0: // same merge key (same log) -> confirmed
			result = append(result, dbLog(observed[o], confirmTags))
			o++
			s++
		case -1: // observed[o] < speculated[s] -> new
			result = append(result, dbLog(observed[o], confirmTags))
			o++
		case 1: // observed[o] > speculated[s] -> tombstone
			speculated[s].Tags |= db.TagRemoved
			result = append(result, speculated[s])
			s++
		}
	}
	for ; o < len(observed); o++ {
		result = append(result, dbLog(observed[o], confirmTags))
	}
	for ; s < len(speculated); s++ {
		speculated[s].Tags |= db.TagRemoved
		result = append(result, speculated[s])
	}
	return result
}

// ord compares two logs by the merge key: (block_number, index, block_hash).
func ord(observed types.Log, speculated db.Log) int {
	if c := cmp.Compare(observed.BlockNumber, uint64(speculated.BlockNumber)); c != 0 {
		return c
	}
	if c := cmp.Compare(observed.Index, uint(speculated.LogIndex)); c != 0 {
		return c
	}
	return bytes.Compare(observed.BlockHash[:], speculated.BlockHash[:])
}

func dbLog(in types.Log, tags uint64) (out db.Log) {
	out.From(in, tags)
	return
}
