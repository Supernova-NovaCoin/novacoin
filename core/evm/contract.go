// Package evm implements the Ethereum Virtual Machine for NovaCoin.
package evm

import (
	"math/big"

	"github.com/Supernova-NovaCoin/novacoin/core/state"
)

// Contract represents a contract in the EVM.
type Contract struct {
	// CallerAddress is the caller of the contract.
	CallerAddress state.Address

	// Address is the address of this contract.
	Address state.Address

	// Code is the contract's bytecode.
	Code Code

	// CodeHash is the hash of the code.
	CodeHash state.Hash

	// CodeAddr is the address where the code was loaded from.
	// May differ from Address in DELEGATECALL.
	CodeAddr *state.Address

	// Input is the call data.
	Input []byte

	// Value is the value transferred with the call.
	Value *big.Int

	// Gas is the remaining gas.
	Gas uint64

	// JumpDests is the analysis of valid jump destinations.
	JumpDests *JumpDestAnalysis
}

// NewContract creates a new contract.
func NewContract(caller, addr state.Address, value *big.Int, gas uint64) *Contract {
	return &Contract{
		CallerAddress: caller,
		Address:       addr,
		Value:         value,
		Gas:           gas,
	}
}

// SetCode sets the contract's code.
func (c *Contract) SetCode(hash state.Hash, code Code) {
	c.Code = code
	c.CodeHash = hash
	c.JumpDests = NewJumpDestAnalysis(code)
}

// SetCallCode sets the code from another address (for DELEGATECALL).
func (c *Contract) SetCallCode(addr *state.Address, hash state.Hash, code Code) {
	c.CodeAddr = addr
	c.CodeHash = hash
	c.Code = code
	c.JumpDests = NewJumpDestAnalysis(code)
}

// GetOp returns the opcode at the given position.
func (c *Contract) GetOp(n uint64) OpCode {
	return c.Code.GetOp(n)
}

// GetByte returns the byte at the given position.
func (c *Contract) GetByte(n uint64) byte {
	if n >= uint64(len(c.Code)) {
		return 0
	}
	return c.Code[n]
}

// Caller returns the caller's address.
func (c *Contract) Caller() state.Address {
	return c.CallerAddress
}

// Self returns this contract's address.
func (c *Contract) Self() state.Address {
	return c.Address
}

// UseGas deducts gas from the contract.
func (c *Contract) UseGas(gas uint64) bool {
	if c.Gas < gas {
		return false
	}
	c.Gas -= gas
	return true
}

// RefundGas adds gas back to the contract.
func (c *Contract) RefundGas(gas uint64) {
	c.Gas += gas
}

// ValidJumpDest checks if a position is a valid jump destination.
func (c *Contract) ValidJumpDest(dest *big.Int) bool {
	if dest.BitLen() > 64 {
		return false
	}
	return c.JumpDests.Valid(dest.Uint64())
}

// AsDelegate creates a delegate contract (for DELEGATECALL).
func (c *Contract) AsDelegate() *Contract {
	delegate := &Contract{
		CallerAddress: c.CallerAddress,
		Address:       c.Address,
		Value:         c.Value,
		Gas:           c.Gas,
	}
	return delegate
}

// === Call Context ===

// CallContext provides context for contract calls.
type CallContext struct {
	// Origin is the original sender of the transaction.
	Origin state.Address

	// GasPrice is the gas price for this transaction.
	GasPrice *big.Int

	// Coinbase is the block's coinbase address.
	Coinbase state.Address

	// BlockNumber is the current block number.
	BlockNumber *big.Int

	// Time is the block timestamp.
	Time uint64

	// GasLimit is the block gas limit.
	GasLimit uint64

	// Difficulty is the block difficulty (deprecated, use PrevRandao).
	Difficulty *big.Int

	// PrevRandao is the previous block's RANDAO value (EIP-4399).
	PrevRandao state.Hash

	// BaseFee is the block's base fee (EIP-1559).
	BaseFee *big.Int

	// BlobBaseFee is the blob base fee (EIP-4844).
	BlobBaseFee *big.Int

	// BlobHashes are the versioned blob hashes (EIP-4844).
	BlobHashes []state.Hash

	// ChainID is the chain identifier.
	ChainID *big.Int
}

// NewCallContext creates a new call context.
func NewCallContext(origin state.Address, gasPrice *big.Int) *CallContext {
	return &CallContext{
		Origin:   origin,
		GasPrice: gasPrice,
		ChainID:  big.NewInt(1),
	}
}

// CanTransfer checks if the sender has enough balance.
type CanTransferFunc func(db StateDB, addr state.Address, amount *big.Int) bool

// Transfer moves value between accounts.
type TransferFunc func(db StateDB, sender, recipient state.Address, amount *big.Int)

// GetHash returns the hash of a block by number.
type GetHashFunc func(uint64) state.Hash

// StateDB interface for state access.
type StateDB interface {
	// Account methods
	CreateAccount(state.Address)
	SubBalance(state.Address, *big.Int)
	AddBalance(state.Address, *big.Int)
	GetBalance(state.Address) *big.Int
	GetNonce(state.Address) uint64
	SetNonce(state.Address, uint64)

	// Code methods
	GetCode(state.Address) []byte
	GetCodeSize(state.Address) int
	GetCodeHash(state.Address) state.Hash
	SetCode(state.Address, []byte)

	// Storage methods
	GetState(state.Address, state.Hash) state.Hash
	SetState(state.Address, state.Hash, state.Hash)
	GetCommittedState(state.Address, state.Hash) state.Hash

	// Transient storage (EIP-1153)
	GetTransientState(state.Address, state.Hash) state.Hash
	SetTransientState(state.Address, state.Hash, state.Hash)

	// Suicide/Selfdestruct
	HasSuicided(state.Address) bool
	Suicide(state.Address) bool

	// Existence
	Exist(state.Address) bool
	Empty(state.Address) bool

	// Access list (EIP-2929)
	AddAddressToAccessList(state.Address)
	AddSlotToAccessList(state.Address, state.Hash)
	AddressInAccessList(state.Address) bool
	SlotInAccessList(state.Address, state.Hash) (addressPresent, slotPresent bool)

	// Snapshot/Revert
	Snapshot() int
	RevertToSnapshot(int)

	// Refunds
	AddRefund(uint64)
	SubRefund(uint64)
	GetRefund() uint64
}

// === Precompiled Contracts ===

// PrecompiledContract is the interface for precompiled contracts.
type PrecompiledContract interface {
	// RequiredGas returns the gas required for the call.
	RequiredGas(input []byte) uint64

	// Run executes the precompiled contract.
	Run(input []byte) ([]byte, error)
}

// PrecompiledContracts maps addresses to precompiled contracts.
var PrecompiledContracts = map[state.Address]PrecompiledContract{}

// IsPrecompiled checks if an address is a precompiled contract.
func IsPrecompiled(addr state.Address) bool {
	_, ok := PrecompiledContracts[addr]
	return ok
}

// === Call Results ===

// ExecutionResult contains the result of EVM execution.
type ExecutionResult struct {
	// UsedGas is the total gas used.
	UsedGas uint64

	// Err is any error that occurred.
	Err error

	// ReturnData is the return data from the call.
	ReturnData []byte

	// ContractAddress is the address of a created contract.
	ContractAddress state.Address
}

// Unwrap returns the execution result's data or error.
func (r *ExecutionResult) Unwrap() ([]byte, error) {
	return r.ReturnData, r.Err
}

// Revert returns true if the execution was reverted.
func (r *ExecutionResult) Revert() bool {
	return r.Err == ErrExecutionReverted
}

// Failed returns true if the execution failed.
func (r *ExecutionResult) Failed() bool {
	return r.Err != nil
}
