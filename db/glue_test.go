package db

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
)

func TestLogRoundTrip(t *testing.T) {
	const tags = uint64(0b101)

	in := types.Log{
		TxHash:         common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		TxIndex:        7,
		Index:          42,
		BlockNumber:    1_000_000,
		BlockHash:      common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		BlockTimestamp: 1_700_000_000,
		Address:        common.HexToAddress("0x4242424242424242424242424242424242424242"),
		Topics: []common.Hash{
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
			common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
		},
		Data: []byte{0xde, 0xad, 0xbe, 0xef},
	}

	var log Log
	log.From(in, tags)

	out, gotTags := log.To()
	assert.Equal(t, in, out)
	assert.Equal(t, tags, gotTags)
}
