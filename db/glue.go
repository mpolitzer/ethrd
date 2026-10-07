package db

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func (out *Log) From(in types.Log, tags uint64) {
	out.TxHash = in.TxHash
	out.TxIndex = uint64(in.TxIndex)
	out.LogIndex = uint64(in.Index)
	out.BlockNumber = in.BlockNumber
	out.BlockHash = in.BlockHash
	out.BlockTimestamp = in.BlockTimestamp
	out.Address = in.Address
	out.Topics = encodeTopics(in.Topics)
	out.Data = in.Data
	out.Tags = tags
}

func (in *Log) To() (types.Log, uint64) {
	return types.Log{
		TxHash:         in.TxHash,
		TxIndex:        uint(in.TxIndex),
		Index:          uint(in.LogIndex),
		BlockNumber:    in.BlockNumber,
		BlockHash:      in.BlockHash,
		BlockTimestamp: in.BlockTimestamp,
		Address:        in.Address,
		Topics:         decodeTopics(in.Topics),
		Data:           in.Data,
	}, in.Tags
}

func encodeTopics(topics []common.Hash) []byte {
	b := make([]byte, len(topics)*32)
	for i, t := range topics {
		copy(b[i*32:(i+1)*32], t[:])
	}
	return b
}

func decodeTopics(topics []byte) []common.Hash {
	n := len(topics) / 32
	out := make([]common.Hash, n)
	for i := range out {
		lo := i * 32
		hi := lo + 32
		copy(out[i][:], topics[lo:hi])
	}
	return out
}
