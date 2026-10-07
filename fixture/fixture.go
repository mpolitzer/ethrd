// Code generated via abigen V2 - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package fixture

import (
	"bytes"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = bytes.Equal
	_ = errors.New
	_ = big.NewInt
	_ = common.Big1
	_ = types.BloomLookup
	_ = abi.ConvertType
)

// FixtureMetaData contains all meta data concerning the Fixture contract.
var FixtureMetaData = bind.MetaData{
	ABI: "[{\"type\":\"function\",\"name\":\"get\",\"inputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"set\",\"inputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"event\",\"name\":\"updated\",\"inputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"count\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false}]",
	ID:  "Fixture",
	Bin: "0x6080604052348015600e575f5ffd5b5061022d8061001c5f395ff3fe608060405234801561000f575f5ffd5b5060043610610034575f3560e01c806360fe47b1146100385780639507d39a14610054575b5f5ffd5b610052600480360381019061004d9190610130565b610084565b005b61006e60048036038101906100699190610130565b6100e0565b60405161007b919061016a565b60405180910390f35b807fe1f295a2e24e62929fba60522642786eac23827e1a3aab2465a028da4ef4c0545f5f8481526020019081526020015f205f81546100c2906101b0565b9190508190556040516100d5919061016a565b60405180910390a250565b5f5f5f8381526020019081526020015f20549050919050565b5f5ffd5b5f819050919050565b61010f816100fd565b8114610119575f5ffd5b50565b5f8135905061012a81610106565b92915050565b5f60208284031215610145576101446100f9565b5b5f6101528482850161011c565b91505092915050565b610164816100fd565b82525050565b5f60208201905061017d5f83018461015b565b92915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f6101ba826100fd565b91507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff82036101ec576101eb610183565b5b60018201905091905056fea2646970667358221220bd1fb8d867347f188f87379382d5ba5e7b5a0aa95aa7b0f49b572009e1e8b8ff64736f6c634300081e0033",
}

// Fixture is an auto generated Go binding around an Ethereum contract.
type Fixture struct {
	abi abi.ABI
}

// GetABI returns the ABI associated with this contract binding.
func (c *Fixture) GetABI() abi.ABI {
	return c.abi
}

// NewFixture creates a new instance of Fixture.
func NewFixture() *Fixture {
	parsed, err := FixtureMetaData.ParseABI()
	if err != nil {
		panic(errors.New("invalid ABI: " + err.Error()))
	}
	return &Fixture{abi: *parsed}
}

// Instance creates a wrapper for a deployed contract instance at the given address.
// Use this to create the instance object passed to abigen v2 library functions Call, Transact, etc.
func (c *Fixture) Instance(backend bind.ContractBackend, addr common.Address) *bind.BoundContract {
	return bind.NewBoundContract(addr, c.abi, backend, backend, backend)
}

// PackGet is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x9507d39a.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function get(uint256 id) view returns(uint256)
func (fixture *Fixture) PackGet(id *big.Int) []byte {
	enc, err := fixture.abi.Pack("get", id)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackGet is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x9507d39a.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function get(uint256 id) view returns(uint256)
func (fixture *Fixture) TryPackGet(id *big.Int) ([]byte, error) {
	return fixture.abi.Pack("get", id)
}

// UnpackGet is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x9507d39a.
//
// Solidity: function get(uint256 id) view returns(uint256)
func (fixture *Fixture) UnpackGet(data []byte) (*big.Int, error) {
	out, err := fixture.abi.Unpack("get", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackSet is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x60fe47b1.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function set(uint256 id) returns()
func (fixture *Fixture) PackSet(id *big.Int) []byte {
	enc, err := fixture.abi.Pack("set", id)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSet is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x60fe47b1.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function set(uint256 id) returns()
func (fixture *Fixture) TryPackSet(id *big.Int) ([]byte, error) {
	return fixture.abi.Pack("set", id)
}

// FixtureUpdated represents a updated event raised by the Fixture contract.
type FixtureUpdated struct {
	Id    *big.Int
	Count *big.Int
	Raw   *types.Log // Blockchain specific contextual infos
}

const FixtureUpdatedEventName = "updated"

// ContractEventName returns the user-defined event name.
func (FixtureUpdated) ContractEventName() string {
	return FixtureUpdatedEventName
}

// UnpackUpdatedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event updated(uint256 indexed id, uint256 count)
func (fixture *Fixture) UnpackUpdatedEvent(log *types.Log) (*FixtureUpdated, error) {
	event := "updated"
	if len(log.Topics) == 0 {
		return nil, bind.ErrNoEventSignature
	}
	if log.Topics[0] != fixture.abi.Events[event].ID {
		return nil, bind.ErrEventSignatureMismatch
	}
	out := new(FixtureUpdated)
	if len(log.Data) > 0 {
		if err := fixture.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range fixture.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}
