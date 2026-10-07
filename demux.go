// Package ethrd unpacks logs into Go event structs via reflection. Layouts
// mirror abigen v2, so parsed pointers cast to the generated types.
package ethrd

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"unsafe"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// MetaLog describes one event: name, ABI, and the Go type of its data.
type MetaLog struct {
	contractName string
	eventName    string
	abi          abi.ABI
	typ          reflect.Type
}

// Demux routes logs to event metadata by event signature (topics[0]).
type Demux map[common.Hash]MetaLog

// NewDemux parses a combined ABI (a map of contract name to ABI JSON) and
// routes logs to events by signature (topics[0]). The same event may be
// declared by several contracts (e.g. an interface and its implementation);
// the first in sorted contract name order wins.
func NewDemux(abiContents []byte) (Demux, error) {
	var cABI cABI
	if err := json.Unmarshal(abiContents, &cABI); err != nil {
		return nil, fmt.Errorf("parse combined-abi: %w", err)
	}
	return cABI.New()
}

func (cABI cABI) New() (Demux, error) {
	m := Demux{}
	names := make([]string, 0, len(cABI))
	for name := range cABI {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		contract := cABI[name]
		for _, event := range contract.Events {
			if event.Anonymous {
				continue // no topics[0] to route on, cf. combinedABI.Topics
			}
			if _, exist := m[event.ID]; exist {
				continue // same signature, e.g. interface and implementation
			}
			typ, err := makeStruct(event)
			if err != nil {
				return nil, fmt.Errorf("event %s.%s: %w",
					name, event.Name, err)
			}
			m[event.ID] = MetaLog{
				contractName: name,
				eventName:    event.Name,
				abi:          contract,
				typ:          typ,
			}
		}
	}
	return m, nil
}

func (m MetaLog) String() string {
	return fmt.Sprintf("%s.%s", m.contractName, m.eventName)
}

// ParseKey returns the "contract.event" name of the log, without unpacking it.
func (d Demux) ParseKey(log types.Log) (string, error) {
	if len(log.Topics) == 0 {
		return "", os.ErrNotExist
	}
	if m, found := d[log.Topics[0]]; found {
		return m.String(), nil
	}
	return "", os.ErrNotExist
}

// Decode unpacks the log per its signature (topics[0]) and returns the
// decoded event as a JSON-serializable value.
func (d Demux) Decode(log types.Log) (string, any, error) {
	if len(log.Topics) == 0 {
		return "", nil, os.ErrNotExist
	}
	m, found := d[log.Topics[0]]
	if !found {
		return "", nil, os.ErrNotExist
	}
	value := reflect.New(m.typ)
	if err := unpackLog(m.abi, m.eventName, log, value.Interface()); err != nil {
		return "", nil, err
	}
	raw := log
	value.Elem().FieldByName("Raw").Set(reflect.ValueOf(&raw))
	return m.String(), value.Interface(), nil
}

// Parse unpacks the log per its signature (topics[0]). The pointer must be
// cast to the matching generated type, e.g. via Handler.
func (d Demux) Parse(log types.Log) (string, unsafe.Pointer, error) {
	s, v, err := d.Decode(log)
	if err != nil {
		return "", nil, err
	}
	return s, reflect.ValueOf(v).UnsafePointer(), nil
}

// Handler adapts a typed handler to Demuxer.Parse's callback shape. T must
// be the event's generated type, e.g. fixture.FixtureUpdated.
func Handler[T any](f func(s string, ev *T)) func(string, unsafe.Pointer) {
	return func(s string, p unsafe.Pointer) {
		f(s, (*T)(p))
	}
}

// makeStruct builds the event's data type, mirroring abigen v2: one field
// per input (named via abi.ToCamelCase), in order, plus Raw *types.Log last.
func makeStruct(event abi.Event) (reflect.Type, error) {
	fields := make([]reflect.StructField, 0, len(event.Inputs)+1)
	seen := map[string]struct{}{"Raw": {}} // reserved for the Raw field
	for i, input := range event.Inputs {
		name := abi.ToCamelCase(input.Name)
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("input %d name %q collides with another field", i, input.Name)
		}
		seen[name] = struct{}{}
		fields = append(fields, reflect.StructField{
			Name: name,
			Type: inputGoType(input),
		})
	}
	fields = append(fields, reflect.StructField{
		Name: "Raw",
		Type: reflect.TypeFor[*types.Log](),
	})
	return reflect.StructOf(fields), nil
}

// inputGoType returns the Go type of an input, mirroring abigen v2: indexed
// string/bytes are keccak hashes in the topic, so they surface as common.Hash.
func inputGoType(input abi.Argument) reflect.Type {
	if input.Indexed && (input.Type.T == abi.StringTy || input.Type.T == abi.BytesTy) {
		return reflect.TypeFor[common.Hash]()
	}
	return input.Type.GetType()
}

// unpackLog unpacks the log's data and indexed topics into out (cf. abigen v2's Unpack*Event).
func unpackLog(a abi.ABI, event string, log types.Log, out any) error {
	err := a.UnpackIntoInterface(out, event, log.Data)
	if err != nil {
		return err
	}
	var indexed abi.Arguments
	for _, arg := range a.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	return abi.ParseTopics(out, indexed, log.Topics[1:])
}
