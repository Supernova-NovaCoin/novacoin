// Package evm implements the Ethereum Virtual Machine for NovaCoin.
package evm

import (
	"errors"
	"math/big"
	"sync/atomic"

	"github.com/novacoin/novacoin/core/state"
	"github.com/novacoin/novacoin/core/types"
)

// Common errors.
var (
	ErrOutOfGas                 = errors.New("out of gas")
	ErrCodeStoreOutOfGas        = errors.New("contract creation code storage out of gas")
	ErrDepth                    = errors.New("max call depth exceeded")
	ErrInsufficientBalance      = errors.New("insufficient balance for transfer")
	ErrContractAddressCollision = errors.New("contract address collision")
	ErrExecutionReverted        = errors.New("execution reverted")
	ErrMaxCodeSizeExceeded      = errors.New("max code size exceeded")
	ErrInvalidJump              = errors.New("invalid jump destination")
	ErrWriteProtection          = errors.New("write protection")
	ErrReturnDataOutOfBounds    = errors.New("return data out of bounds")
	ErrInvalidCode              = errors.New("invalid code: must not begin with 0xef")
	ErrStackUnderflow_          = errors.New("stack underflow")
	ErrStackOverflow_           = errors.New("stack overflow")
	ErrInvalidOpcode            = errors.New("invalid opcode")
)

// Config contains EVM configuration.
type Config struct {
	// Debug enables debug mode.
	Debug bool

	// Tracer is the execution tracer.
	Tracer Tracer

	// MaxCallDepth is the maximum call depth.
	MaxCallDepth int

	// MaxCodeSize is the maximum contract code size.
	MaxCodeSize int

	// EnablePreimageRecording enables preimage recording.
	EnablePreimageRecording bool

	// JumpTable is the jump table for opcode execution.
	JumpTable JumpTable

	// ExtraEips is a list of extra EIPs to enable.
	ExtraEips []int
}

// DefaultConfig returns the default EVM configuration.
func DefaultConfig() *Config {
	return &Config{
		MaxCallDepth: 1024,
		MaxCodeSize:  24576, // EIP-170: 24KB
	}
}

// EVM represents the Ethereum Virtual Machine.
type EVM struct {
	// Config is the EVM configuration.
	Config *Config

	// Context provides block/transaction context.
	Context *CallContext

	// StateDB is the state database.
	StateDB StateDB

	// Depth is the current call depth.
	depth int

	// Gas costs configuration.
	gasCosts *GasCosts

	// Abort flag.
	abort int32

	// Call gas temp.
	callGasTemp uint64
}

// NewEVM creates a new EVM instance.
func NewEVM(ctx *CallContext, stateDB StateDB, config *Config) *EVM {
	if config == nil {
		config = DefaultConfig()
	}

	return &EVM{
		Config:   config,
		Context:  ctx,
		StateDB:  stateDB,
		gasCosts: DefaultGasCosts(),
	}
}

// Reset resets the EVM for a new transaction.
func (evm *EVM) Reset(ctx *CallContext, stateDB StateDB) {
	evm.Context = ctx
	evm.StateDB = stateDB
	evm.depth = 0
	atomic.StoreInt32(&evm.abort, 0)
}

// Cancel aborts EVM execution.
func (evm *EVM) Cancel() {
	atomic.StoreInt32(&evm.abort, 1)
}

// Cancelled returns true if execution was cancelled.
func (evm *EVM) Cancelled() bool {
	return atomic.LoadInt32(&evm.abort) == 1
}

// Interpreter represents the EVM interpreter.
type Interpreter struct {
	evm *EVM

	// readOnly prevents state modifications in static calls.
	readOnly bool

	// returnData holds the last call's return data.
	returnData []byte
}

// NewInterpreter creates a new interpreter.
func NewInterpreter(evm *EVM) *Interpreter {
	return &Interpreter{
		evm: evm,
	}
}

// Run executes the contract code.
func (in *Interpreter) Run(contract *Contract, input []byte, readOnly bool) ([]byte, error) {
	// Set read-only mode for STATICCALL
	if readOnly && !in.readOnly {
		in.readOnly = true
		defer func() { in.readOnly = false }()
	}

	// Reset return data
	in.returnData = nil

	// Check for empty code
	if len(contract.Code) == 0 {
		return nil, nil
	}

	// Initialize state
	var (
		pc     uint64        // Program counter
		stack  = NewStack()  // Stack
		mem    = NewMemory() // Memory
		cost   uint64        // Current operation gas cost
		logged bool          // For debugging
	)

	// Set input
	contract.Input = input

	// Return stack to pool when done
	defer ReturnStack(stack)

	// Main execution loop
	for {
		// Check for abort
		if in.evm.Cancelled() {
			return nil, errors.New("execution cancelled")
		}

		// Get the current opcode
		op := contract.GetOp(pc)

		// Get operation info
		info := OpTable[op]

		// Check if opcode is valid
		if !info.Valid {
			return nil, ErrInvalidOpcode
		}

		// Check stack requirements
		if err := stack.Require(info.StackPop); err != nil {
			return nil, ErrStackUnderflow_
		}
		if err := stack.ValidSize(info.StackPush - info.StackPop); err != nil {
			return nil, ErrStackOverflow_
		}

		// Check read-only violation
		if in.readOnly && info.Writes {
			return nil, ErrWriteProtection
		}

		// Calculate base gas cost
		cost = info.Gas

		// Special gas handling for certain opcodes
		switch op {
		case SLOAD:
			cost = in.gasCostSLoad(contract)
		case SSTORE:
			cost = in.gasCostSStore(contract, stack)
		case EXP:
			cost = in.gasCostExp(stack)
		case KECCAK256, CALLDATACOPY, CODECOPY, EXTCODECOPY, RETURNDATACOPY, MCOPY:
			cost += in.gasCostCopy(stack, op)
		case LOG0, LOG1, LOG2, LOG3, LOG4:
			cost += in.gasCostLog(stack, op)
		case CALL, CALLCODE, DELEGATECALL, STATICCALL:
			cost = in.gasCostCall(contract, stack, op)
		case CREATE, CREATE2:
			cost = in.gasCostCreate(stack, op)
		}

		// Check gas
		if !contract.UseGas(cost) {
			return nil, ErrOutOfGas
		}

		// Execute the opcode
		res, err := in.execute(op, contract, stack, mem, pc)
		if err != nil {
			return nil, err
		}

		// Debug logging
		if in.evm.Config.Debug && !logged {
			logged = true
		}

		// Handle return/stop
		if info.Halts {
			return res, nil
		}

		// Handle jumps
		if info.Jumps {
			pc = stack.Pop().Uint64()
			if op == JUMPI {
				// Pop condition but PC is already set by execute
			}
		} else {
			// Advance program counter
			pc++
			if op.IsPush() {
				pc += uint64(op.PushBytes())
			}
		}
	}
}

// execute runs a single opcode.
func (in *Interpreter) execute(op OpCode, contract *Contract, stack *Stack, mem *Memory, pc uint64) ([]byte, error) {
	switch op {
	// === Arithmetic ===
	case STOP:
		return nil, nil

	case ADD:
		x, y := stack.Pop(), stack.Pop()
		stack.Push(U256(new(big.Int).Add(x, y)))

	case MUL:
		x, y := stack.Pop(), stack.Pop()
		stack.Push(U256(new(big.Int).Mul(x, y)))

	case SUB:
		x, y := stack.Pop(), stack.Pop()
		stack.Push(U256(new(big.Int).Sub(x, y)))

	case DIV:
		x, y := stack.Pop(), stack.Pop()
		if y.Sign() == 0 {
			stack.Push(new(big.Int))
		} else {
			stack.Push(U256(new(big.Int).Div(x, y)))
		}

	case SDIV:
		x, y := S256(stack.Pop()), S256(stack.Pop())
		if y.Sign() == 0 {
			stack.Push(new(big.Int))
		} else {
			stack.Push(U256(new(big.Int).Div(x, y)))
		}

	case MOD:
		x, y := stack.Pop(), stack.Pop()
		if y.Sign() == 0 {
			stack.Push(new(big.Int))
		} else {
			stack.Push(U256(new(big.Int).Mod(x, y)))
		}

	case SMOD:
		x, y := S256(stack.Pop()), S256(stack.Pop())
		if y.Sign() == 0 {
			stack.Push(new(big.Int))
		} else {
			res := new(big.Int).Mod(x, y)
			if x.Sign() < 0 {
				res.Neg(res)
			}
			stack.Push(U256(res))
		}

	case ADDMOD:
		x, y, z := stack.Pop(), stack.Pop(), stack.Pop()
		if z.Sign() == 0 {
			stack.Push(new(big.Int))
		} else {
			stack.Push(U256(new(big.Int).Mod(new(big.Int).Add(x, y), z)))
		}

	case MULMOD:
		x, y, z := stack.Pop(), stack.Pop(), stack.Pop()
		if z.Sign() == 0 {
			stack.Push(new(big.Int))
		} else {
			stack.Push(U256(new(big.Int).Mod(new(big.Int).Mul(x, y), z)))
		}

	case EXP:
		base, exp := stack.Pop(), stack.Pop()
		stack.Push(U256(new(big.Int).Exp(base, exp, new(big.Int).Add(Uint256Max, big.NewInt(1)))))

	case SIGNEXTEND:
		back, x := stack.Pop(), stack.Pop()
		if back.Cmp(big.NewInt(31)) < 0 {
			bit := uint(back.Uint64()*8 + 7)
			mask := new(big.Int).Lsh(big.NewInt(1), bit)
			mask.Sub(mask, big.NewInt(1))
			if x.Bit(int(bit)) == 1 {
				x.Or(x, new(big.Int).Not(mask))
			} else {
				x.And(x, mask)
			}
			stack.Push(U256(x))
		} else {
			stack.Push(x)
		}

	// === Comparison ===
	case LT:
		x, y := stack.Pop(), stack.Pop()
		if x.Cmp(y) < 0 {
			stack.Push(big.NewInt(1))
		} else {
			stack.Push(new(big.Int))
		}

	case GT:
		x, y := stack.Pop(), stack.Pop()
		if x.Cmp(y) > 0 {
			stack.Push(big.NewInt(1))
		} else {
			stack.Push(new(big.Int))
		}

	case SLT:
		x, y := S256(stack.Pop()), S256(stack.Pop())
		if x.Cmp(y) < 0 {
			stack.Push(big.NewInt(1))
		} else {
			stack.Push(new(big.Int))
		}

	case SGT:
		x, y := S256(stack.Pop()), S256(stack.Pop())
		if x.Cmp(y) > 0 {
			stack.Push(big.NewInt(1))
		} else {
			stack.Push(new(big.Int))
		}

	case EQ:
		x, y := stack.Pop(), stack.Pop()
		if x.Cmp(y) == 0 {
			stack.Push(big.NewInt(1))
		} else {
			stack.Push(new(big.Int))
		}

	case ISZERO:
		x := stack.Pop()
		if x.Sign() == 0 {
			stack.Push(big.NewInt(1))
		} else {
			stack.Push(new(big.Int))
		}

	// === Bitwise ===
	case AND:
		x, y := stack.Pop(), stack.Pop()
		stack.Push(new(big.Int).And(x, y))

	case OR:
		x, y := stack.Pop(), stack.Pop()
		stack.Push(new(big.Int).Or(x, y))

	case XOR:
		x, y := stack.Pop(), stack.Pop()
		stack.Push(new(big.Int).Xor(x, y))

	case NOT:
		x := stack.Pop()
		stack.Push(U256(new(big.Int).Not(x)))

	case BYTE:
		th, val := stack.Pop(), stack.Pop()
		if th.Cmp(big.NewInt(32)) < 0 {
			b := byte(val.Rsh(val, uint(248-th.Uint64()*8)).Uint64() & 0xFF)
			stack.Push(new(big.Int).SetUint64(uint64(b)))
		} else {
			stack.Push(new(big.Int))
		}

	case SHL:
		shift, value := stack.Pop(), stack.Pop()
		if shift.Cmp(big.NewInt(256)) >= 0 {
			stack.Push(new(big.Int))
		} else {
			stack.Push(U256(new(big.Int).Lsh(value, uint(shift.Uint64()))))
		}

	case SHR:
		shift, value := stack.Pop(), stack.Pop()
		if shift.Cmp(big.NewInt(256)) >= 0 {
			stack.Push(new(big.Int))
		} else {
			stack.Push(new(big.Int).Rsh(value, uint(shift.Uint64())))
		}

	case SAR:
		shift, value := stack.Pop(), S256(stack.Pop())
		if shift.Cmp(big.NewInt(256)) >= 0 {
			if value.Sign() >= 0 {
				stack.Push(new(big.Int))
			} else {
				stack.Push(new(big.Int).SetInt64(-1))
			}
		} else {
			stack.Push(U256(new(big.Int).Rsh(value, uint(shift.Uint64()))))
		}

	// === Keccak256 ===
	case KECCAK256:
		offset, size := stack.Pop(), stack.Pop()
		data := mem.GetCopy(int64(offset.Uint64()), int64(size.Uint64()))
		hash := keccak256Hash(data)
		stack.Push(new(big.Int).SetBytes(hash[:]))

	// === Environment ===
	case ADDRESS:
		stack.Push(new(big.Int).SetBytes(contract.Address[:]))

	case BALANCE:
		addr := stack.Pop()
		balance := in.evm.StateDB.GetBalance(BigToAddress(addr))
		stack.Push(balance)

	case ORIGIN:
		stack.Push(new(big.Int).SetBytes(in.evm.Context.Origin[:]))

	case CALLER:
		stack.Push(new(big.Int).SetBytes(contract.CallerAddress[:]))

	case CALLVALUE:
		stack.Push(new(big.Int).Set(contract.Value))

	case CALLDATALOAD:
		offset := stack.Pop()
		data := getDataBig(contract.Input, offset, big.NewInt(32))
		stack.Push(new(big.Int).SetBytes(data))

	case CALLDATASIZE:
		stack.Push(big.NewInt(int64(len(contract.Input))))

	case CALLDATACOPY:
		memOffset, dataOffset, length := stack.Pop(), stack.Pop(), stack.Pop()
		data := getDataBig(contract.Input, dataOffset, length)
		mem.Set(memOffset.Uint64(), length.Uint64(), data)

	case CODESIZE:
		stack.Push(big.NewInt(int64(len(contract.Code))))

	case CODECOPY:
		memOffset, codeOffset, length := stack.Pop(), stack.Pop(), stack.Pop()
		data := contract.Code.GetData(codeOffset.Uint64(), length.Uint64())
		mem.Set(memOffset.Uint64(), length.Uint64(), data)

	case GASPRICE:
		stack.Push(new(big.Int).Set(in.evm.Context.GasPrice))

	case EXTCODESIZE:
		addr := stack.Pop()
		size := in.evm.StateDB.GetCodeSize(BigToAddress(addr))
		stack.Push(big.NewInt(int64(size)))

	case EXTCODECOPY:
		addr := stack.Pop()
		memOffset, codeOffset, length := stack.Pop(), stack.Pop(), stack.Pop()
		code := in.evm.StateDB.GetCode(BigToAddress(addr))
		data := getData(code, codeOffset.Uint64(), length.Uint64())
		mem.Set(memOffset.Uint64(), length.Uint64(), data)

	case RETURNDATASIZE:
		stack.Push(big.NewInt(int64(len(in.returnData))))

	case RETURNDATACOPY:
		memOffset, dataOffset, length := stack.Pop(), stack.Pop(), stack.Pop()
		end := new(big.Int).Add(dataOffset, length)
		if end.BitLen() > 64 || end.Uint64() > uint64(len(in.returnData)) {
			return nil, ErrReturnDataOutOfBounds
		}
		data := in.returnData[dataOffset.Uint64():end.Uint64()]
		mem.Set(memOffset.Uint64(), length.Uint64(), data)

	case EXTCODEHASH:
		addr := stack.Pop()
		if in.evm.StateDB.Empty(BigToAddress(addr)) {
			stack.Push(new(big.Int))
		} else {
			hash := in.evm.StateDB.GetCodeHash(BigToAddress(addr))
			stack.Push(new(big.Int).SetBytes(hash[:]))
		}

	// === Block Information ===
	case BLOCKHASH:
		num := stack.Pop()
		// Only 256 most recent blocks available
		current := in.evm.Context.BlockNumber.Uint64()
		if num.Uint64() < current && current-num.Uint64() <= 256 {
			// Would need GetHash function - simplified
			stack.Push(new(big.Int))
		} else {
			stack.Push(new(big.Int))
		}

	case COINBASE:
		stack.Push(new(big.Int).SetBytes(in.evm.Context.Coinbase[:]))

	case TIMESTAMP:
		stack.Push(big.NewInt(int64(in.evm.Context.Time)))

	case NUMBER:
		stack.Push(new(big.Int).Set(in.evm.Context.BlockNumber))

	case PREVRANDAO:
		stack.Push(new(big.Int).SetBytes(in.evm.Context.PrevRandao[:]))

	case GASLIMIT:
		stack.Push(big.NewInt(int64(in.evm.Context.GasLimit)))

	case CHAINID:
		stack.Push(new(big.Int).Set(in.evm.Context.ChainID))

	case SELFBALANCE:
		balance := in.evm.StateDB.GetBalance(contract.Address)
		stack.Push(balance)

	case BASEFEE:
		stack.Push(new(big.Int).Set(in.evm.Context.BaseFee))

	// === Stack/Memory/Storage ===
	case POP:
		stack.Pop()

	case MLOAD:
		offset := stack.Pop()
		data := mem.GetCopy(int64(offset.Uint64()), 32)
		stack.Push(new(big.Int).SetBytes(data))

	case MSTORE:
		offset, val := stack.Pop(), stack.Pop()
		mem.Set32(offset.Uint64(), val)

	case MSTORE8:
		offset, val := stack.Pop(), stack.Pop()
		mem.SetByte(offset.Uint64(), byte(val.Uint64()&0xFF))

	case SLOAD:
		key := stack.Pop()
		val := in.evm.StateDB.GetState(contract.Address, BigToHash(key))
		stack.Push(new(big.Int).SetBytes(val[:]))

	case SSTORE:
		key, val := stack.Pop(), stack.Pop()
		in.evm.StateDB.SetState(contract.Address, BigToHash(key), BigToHash(val))

	case JUMP:
		dest := stack.Pop()
		if !contract.ValidJumpDest(dest) {
			return nil, ErrInvalidJump
		}
		// PC will be set after this

	case JUMPI:
		dest, cond := stack.Pop(), stack.Pop()
		if cond.Sign() != 0 {
			if !contract.ValidJumpDest(dest) {
				return nil, ErrInvalidJump
			}
			stack.Push(dest) // Will be popped by PC setting
		}

	case PC:
		stack.Push(big.NewInt(int64(pc)))

	case MSIZE:
		stack.Push(big.NewInt(int64(mem.Len())))

	case GAS:
		stack.Push(big.NewInt(int64(contract.Gas)))

	case JUMPDEST:
		// No operation, just a marker

	case TLOAD:
		key := stack.Pop()
		val := in.evm.StateDB.GetTransientState(contract.Address, BigToHash(key))
		stack.Push(new(big.Int).SetBytes(val[:]))

	case TSTORE:
		key, val := stack.Pop(), stack.Pop()
		in.evm.StateDB.SetTransientState(contract.Address, BigToHash(key), BigToHash(val))

	// === Push ===
	case PUSH0:
		stack.Push(new(big.Int))

	case PUSH1, PUSH2, PUSH3, PUSH4, PUSH5, PUSH6, PUSH7, PUSH8,
		PUSH9, PUSH10, PUSH11, PUSH12, PUSH13, PUSH14, PUSH15, PUSH16,
		PUSH17, PUSH18, PUSH19, PUSH20, PUSH21, PUSH22, PUSH23, PUSH24,
		PUSH25, PUSH26, PUSH27, PUSH28, PUSH29, PUSH30, PUSH31, PUSH32:
		size := op.PushBytes()
		data := contract.Code.GetData(pc+1, uint64(size))
		stack.Push(new(big.Int).SetBytes(data))

	// === Dup ===
	case DUP1, DUP2, DUP3, DUP4, DUP5, DUP6, DUP7, DUP8,
		DUP9, DUP10, DUP11, DUP12, DUP13, DUP14, DUP15, DUP16:
		stack.Dup(op.DupPosition())

	// === Swap ===
	case SWAP1, SWAP2, SWAP3, SWAP4, SWAP5, SWAP6, SWAP7, SWAP8,
		SWAP9, SWAP10, SWAP11, SWAP12, SWAP13, SWAP14, SWAP15, SWAP16:
		stack.Swap(op.SwapPosition())

	// === Log ===
	case LOG0, LOG1, LOG2, LOG3, LOG4:
		// Read memory offset and size
		offset, size := stack.Pop(), stack.Pop()

		// Read topics
		topics := make([]state.Hash, op.LogTopics())
		for i := 0; i < op.LogTopics(); i++ {
			topics[i] = BigToHash(stack.Pop())
		}

		// Get data
		data := mem.GetCopy(int64(offset.Uint64()), int64(size.Uint64()))

		// Create log (would add to state)
		_ = types.NewLog(contract.Address, topics, data)

	// === System ===
	case CREATE:
		value, offset, size := stack.Pop(), stack.Pop(), stack.Pop()
		input := mem.GetCopy(int64(offset.Uint64()), int64(size.Uint64()))
		// Simplified - would create new contract
		_, _ = value, input
		stack.Push(new(big.Int))

	case CREATE2:
		value, offset, size, salt := stack.Pop(), stack.Pop(), stack.Pop(), stack.Pop()
		input := mem.GetCopy(int64(offset.Uint64()), int64(size.Uint64()))
		// Simplified - would create new contract with salt
		_, _, _ = value, input, salt
		stack.Push(new(big.Int))

	case CALL:
		gas, addr, value := stack.Pop(), stack.Pop(), stack.Pop()
		inOffset, inSize, retOffset, retSize := stack.Pop(), stack.Pop(), stack.Pop(), stack.Pop()
		// Simplified - would make call
		_, _, _, _, _, _ = gas, addr, value, inOffset, inSize, retOffset
		_ = retSize
		stack.Push(big.NewInt(1)) // Success

	case CALLCODE:
		gas, addr, value := stack.Pop(), stack.Pop(), stack.Pop()
		inOffset, inSize, retOffset, retSize := stack.Pop(), stack.Pop(), stack.Pop(), stack.Pop()
		_, _, _, _, _, _, _ = gas, addr, value, inOffset, inSize, retOffset, retSize
		stack.Push(big.NewInt(1))

	case DELEGATECALL:
		gas, addr := stack.Pop(), stack.Pop()
		inOffset, inSize, retOffset, retSize := stack.Pop(), stack.Pop(), stack.Pop(), stack.Pop()
		_, _, _, _, _, _ = gas, addr, inOffset, inSize, retOffset, retSize
		stack.Push(big.NewInt(1))

	case STATICCALL:
		gas, addr := stack.Pop(), stack.Pop()
		inOffset, inSize, retOffset, retSize := stack.Pop(), stack.Pop(), stack.Pop(), stack.Pop()
		_, _, _, _, _, _ = gas, addr, inOffset, inSize, retOffset, retSize
		stack.Push(big.NewInt(1))

	case RETURN:
		offset, size := stack.Pop(), stack.Pop()
		ret := mem.GetCopy(int64(offset.Uint64()), int64(size.Uint64()))
		return ret, nil

	case REVERT:
		offset, size := stack.Pop(), stack.Pop()
		ret := mem.GetCopy(int64(offset.Uint64()), int64(size.Uint64()))
		in.returnData = ret
		return ret, ErrExecutionReverted

	case INVALID:
		return nil, ErrInvalidOpcode

	case SELFDESTRUCT:
		beneficiary := stack.Pop()
		in.evm.StateDB.Suicide(contract.Address)
		// Transfer balance to beneficiary
		balance := in.evm.StateDB.GetBalance(contract.Address)
		in.evm.StateDB.AddBalance(BigToAddress(beneficiary), balance)
		in.evm.StateDB.SubBalance(contract.Address, balance)
		return nil, nil

	default:
		return nil, ErrInvalidOpcode
	}

	return nil, nil
}

// === Gas Cost Helpers ===

func (in *Interpreter) gasCostSLoad(contract *Contract) uint64 {
	return in.evm.gasCosts.WarmAccess
}

func (in *Interpreter) gasCostSStore(contract *Contract, stack *Stack) uint64 {
	return in.evm.gasCosts.SReset
}

func (in *Interpreter) gasCostExp(stack *Stack) uint64 {
	exp := stack.Back(1)
	if exp == nil || exp.Sign() == 0 {
		return 10
	}
	return 10 + 50*uint64((exp.BitLen()+7)/8)
}

func (in *Interpreter) gasCostCopy(stack *Stack, op OpCode) uint64 {
	size := stack.Back(2)
	if size == nil {
		return 0
	}
	words := (size.Uint64() + 31) / 32
	return 3 * words
}

func (in *Interpreter) gasCostLog(stack *Stack, op OpCode) uint64 {
	size := stack.Back(1)
	if size == nil {
		return 0
	}
	return 8 * size.Uint64()
}

func (in *Interpreter) gasCostCall(contract *Contract, stack *Stack, op OpCode) uint64 {
	return in.evm.gasCosts.WarmAccess
}

func (in *Interpreter) gasCostCreate(stack *Stack, op OpCode) uint64 {
	return 32000
}

// === Helper Functions ===

func getDataBig(data []byte, start, size *big.Int) []byte {
	dlen := big.NewInt(int64(len(data)))

	s := new(big.Int).Set(start)
	if s.Cmp(dlen) >= 0 {
		return make([]byte, size.Uint64())
	}

	e := new(big.Int).Add(s, size)
	if e.Cmp(dlen) > 0 {
		e.Set(dlen)
	}

	result := make([]byte, size.Uint64())
	copy(result, data[s.Uint64():e.Uint64()])
	return result
}

func getData(data []byte, start, size uint64) []byte {
	if start >= uint64(len(data)) {
		return make([]byte, size)
	}
	end := start + size
	if end > uint64(len(data)) {
		result := make([]byte, size)
		copy(result, data[start:])
		return result
	}
	return data[start:end]
}

func keccak256Hash(data []byte) [32]byte {
	// Simplified hash - in production use crypto/sha3
	var hash [32]byte
	for i, b := range data {
		hash[i%32] ^= b
		hash[(i+1)%32] ^= b << 4
		hash[(i+7)%32] ^= b >> 3
	}
	return hash
}

// === Tracer Interface ===

// Tracer is the interface for EVM execution tracers.
type Tracer interface {
	// CaptureStart is called at the start of execution.
	CaptureStart(env *EVM, from state.Address, to state.Address, create bool, input []byte, gas uint64, value *big.Int)

	// CaptureState is called for each step of execution.
	CaptureState(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, err error)

	// CaptureEnd is called at the end of execution.
	CaptureEnd(output []byte, gasUsed uint64, err error)
}

// ScopeContext contains the execution context for tracing.
type ScopeContext struct {
	Memory   *Memory
	Stack    *Stack
	Contract *Contract
}

// JumpTable contains the execution functions for each opcode.
type JumpTable [256]func(*Interpreter, *Contract, *Stack, *Memory, uint64) ([]byte, error)
