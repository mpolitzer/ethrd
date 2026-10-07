package ethrd

import (
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mpolitzer/ethrd/fixture"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
)

// ABIs for the synthetic test events, as JSON.
const (
	anonymousEventABI = `[{"type":"event","name":"e","anonymous":true,"inputs":[{"name":"id","type":"uint256","indexed":true}]}]`
	collidingInputABI = `[{"type":"event","name":"e","inputs":[{"name":"id","type":"uint256"},{"name":"id","type":"uint256"}]}]`
	indexedStringABI  = `[{"type":"event","name":"note","inputs":[{"name":"who","type":"address","indexed":true},{"name":"text","type":"string","indexed":true}]}]`
)

func fixtureDemuxer(t *testing.T) (Demux, abi.ABI) {
	t.Helper()
	parsed, err := fixture.FixtureMetaData.ParseABI()
	assert.NoError(t, err)
	demuxer, err := cABI{"Fixture": *parsed}.New()
	assert.NoError(t, err)
	return demuxer, *parsed
}

func mustABI(t *testing.T, json string) abi.ABI {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(json))
	assert.NoError(t, err)
	return parsed
}

func TestNewDemuxer(t *testing.T) {
	_, err := NewDemux([]byte("{"))
	assert.Error(t, err)

	demuxer, err := NewDemux([]byte(`{"Fixture":` + fixture.FixtureMetaData.ABI + `}`))
	assert.NoError(t, err)
	assert.Len(t, demuxer, 1)
}

func TestNewDemuxerRepeatedEvents(t *testing.T) {
	note := mustABI(t, indexedStringABI)
	demuxer, err := cABI{"Tournament": note, "ITournament": note}.New()
	assert.NoError(t, err)
	assert.Len(t, demuxer, 1)
	// first in sorted contract name order wins
	assert.Equal(t, "ITournament.note", demuxer[note.Events["note"].ID].String())
}

func TestDemuxerParse(t *testing.T) {
	demuxer, fixtureABI := fixtureDemuxer(t)
	event := fixtureABI.Events["updated"]

	// Layout must match codegen, so the unsafe cast in Handler is sound.
	assert.True(t, sameLayout(demuxer[event.ID].typ, reflect.TypeOf(fixture.FixtureUpdated{})))

	log := types.Log{
		Topics: []common.Hash{
			event.ID,
			common.HexToHash("0x07"), // id=7, right-aligned in the topic
		},
		Data: common.LeftPadBytes(big.NewInt(42).Bytes(), 32),
	}

	name, err := demuxer.ParseKey(log)
	assert.NoError(t, err)
	assert.Equal(t, "Fixture.updated", name)

	var gotName string
	var got *fixture.FixtureUpdated
	handle := Handler(func(name string, ev *fixture.FixtureUpdated) {
		gotName, got = name, ev
	})
	name, ptr, err := demuxer.Parse(log)
	assert.NoError(t, err)
	handle(name, ptr)

	assert.Equal(t, "Fixture.updated", gotName)
	assert.Equal(t, big.NewInt(7), got.Id)
	assert.Equal(t, big.NewInt(42), got.Count)
	assert.Equal(t, log, *got.Raw)
}

func TestDemuxerErrors(t *testing.T) {
	demuxer, fixtureABI := fixtureDemuxer(t)

	t.Run("unknown signature", func(t *testing.T) {
		log := types.Log{Topics: []common.Hash{common.HexToHash("0x01")}}
		_, err := demuxer.ParseKey(log)
		assert.ErrorIs(t, err, os.ErrNotExist)
		_, _, err = demuxer.Parse(log)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("anonymous event skipped", func(t *testing.T) {
		demuxer, err := cABI{"A": mustABI(t, anonymousEventABI)}.New()
		assert.NoError(t, err)
		assert.Empty(t, demuxer)
	})

	t.Run("no topics", func(t *testing.T) {
		_, err := demuxer.ParseKey(types.Log{})
		assert.ErrorIs(t, err, os.ErrNotExist)
		_, _, err = demuxer.Parse(types.Log{})
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("unpack error", func(t *testing.T) {
		log := types.Log{
			Topics: []common.Hash{fixtureABI.Events["updated"].ID},
			Data:   []byte{0x01}, // too short for a uint256
		}
		_, _, err := demuxer.Parse(log)
		assert.Error(t, err)
	})

	t.Run("colliding input names", func(t *testing.T) {
		_, err := cABI{"Bad": mustABI(t, collidingInputABI)}.New()
		assert.Error(t, err)
	})
}

func TestDemuxerIndexedString(t *testing.T) {
	noteABI := mustABI(t, indexedStringABI)
	demuxer, err := cABI{"C": noteABI}.New()
	assert.NoError(t, err)
	event := noteABI.Events["note"]

	// an indexed string surfaces as the keccak hash stored in the topic
	textField, ok := demuxer[event.ID].typ.FieldByName("Text")
	assert.True(t, ok)
	assert.Equal(t, reflect.TypeFor[common.Hash](), textField.Type)

	who := common.HexToAddress("0x4242424242424242424242424242424242424242")
	text := common.HexToHash("0x1234123412341234123412341234123412341234123412341234123412341234")
	log := types.Log{
		Topics: []common.Hash{
			event.ID,
			common.BytesToHash(common.LeftPadBytes(who.Bytes(), 32)),
			text,
		},
	}

	type note struct {
		Who  common.Address
		Text common.Hash
		Raw  *types.Log
	}
	var got *note
	handle := Handler(func(_ string, ev *note) { got = ev })
	name, ptr, err := demuxer.Parse(log)
	assert.NoError(t, err)
	handle(name, ptr)

	assert.Equal(t, who, got.Who)
	assert.Equal(t, text, got.Text)
	assert.Equal(t, log, *got.Raw)
}

// sameLayout compares fields one by one, since reflect.Type == is identity, not layout.
func sameLayout(a, b reflect.Type) bool {
	if a.NumField() != b.NumField() {
		return false
	}
	for i := range a.NumField() {
		fa, fb := a.Field(i), b.Field(i)
		if fa.Name != fb.Name || fa.Type != fb.Type {
			return false
		}
	}
	return true
}
