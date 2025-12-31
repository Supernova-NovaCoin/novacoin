package evm

import (
	"math/big"
	"testing"

	"github.com/novacoin/novacoin/core/state"
)

// === Stack Tests ===

func TestStackBasicOperations(t *testing.T) {
	stack := NewStack()
	defer ReturnStack(stack)

	// Push
	stack.Push(big.NewInt(10))
	stack.Push(big.NewInt(20))
	stack.Push(big.NewInt(30))

	if stack.Len() != 3 {
		t.Errorf("expected len 3, got %d", stack.Len())
	}

	// Peek
	top := stack.Peek()
	if top.Cmp(big.NewInt(30)) != 0 {
		t.Errorf("expected 30, got %s", top.String())
	}

	// Pop
	val := stack.Pop()
	if val.Cmp(big.NewInt(30)) != 0 {
		t.Errorf("expected 30, got %s", val.String())
	}

	if stack.Len() != 2 {
		t.Errorf("expected len 2, got %d", stack.Len())
	}
}

func TestStackSwap(t *testing.T) {
	stack := NewStack()
	defer ReturnStack(stack)

	stack.Push(big.NewInt(1))
	stack.Push(big.NewInt(2))
	stack.Push(big.NewInt(3))

	// Swap top with second
	stack.Swap(1)

	top := stack.Pop()
	second := stack.Pop()

	if top.Cmp(big.NewInt(2)) != 0 {
		t.Errorf("expected 2 at top, got %s", top.String())
	}
	if second.Cmp(big.NewInt(3)) != 0 {
		t.Errorf("expected 3 at second, got %s", second.String())
	}
}

func TestStackDup(t *testing.T) {
	stack := NewStack()
	defer ReturnStack(stack)

	stack.Push(big.NewInt(1))
	stack.Push(big.NewInt(2))
	stack.Push(big.NewInt(3))

	// Dup the top (position 1)
	stack.Dup(1)

	if stack.Len() != 4 {
		t.Errorf("expected len 4, got %d", stack.Len())
	}

	top := stack.Pop()
	if top.Cmp(big.NewInt(3)) != 0 {
		t.Errorf("expected 3, got %s", top.String())
	}
}

func TestStackRequire(t *testing.T) {
	stack := NewStack()
	defer ReturnStack(stack)

	stack.Push(big.NewInt(1))

	if err := stack.Require(1); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if err := stack.Require(2); err == nil {
		t.Error("expected underflow error")
	}
}

// === Memory Tests ===

func TestMemoryBasicOperations(t *testing.T) {
	mem := NewMemory()

	// Set 32 bytes
	mem.Set32(0, big.NewInt(0x1234567890))

	// Read back
	data := mem.GetCopy(0, 32)
	val := new(big.Int).SetBytes(data)

	if val.Cmp(big.NewInt(0x1234567890)) != 0 {
		t.Errorf("expected 0x1234567890, got %s", val.String())
	}
}

func TestMemoryResize(t *testing.T) {
	mem := NewMemory()

	if mem.Len() != 0 {
		t.Errorf("expected initial len 0, got %d", mem.Len())
	}

	mem.Resize(100)

	// Should be rounded up to 32-byte boundary
	if mem.Len() != 128 {
		t.Errorf("expected len 128, got %d", mem.Len())
	}
}

func TestMemorySetByte(t *testing.T) {
	mem := NewMemory()

	mem.SetByte(10, 0xAB)

	data := mem.Get(10, 1)
	if data[0] != 0xAB {
		t.Errorf("expected 0xAB, got 0x%02x", data[0])
	}
}

func TestMemoryGasCost(t *testing.T) {
	mem := NewMemory()

	cost, err := MemoryGasCost(mem, 32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First 32 bytes: 1 word, cost = 1/512 + 3 = 3
	if cost != 3 {
		t.Errorf("expected cost 3, got %d", cost)
	}

	// Expanding to 64 bytes
	cost2, err := MemoryGasCost(mem, 64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2 words: cost = 4/512 + 6 = 6, delta = 6 - 3 = 3
	if cost2 != 3 {
		t.Errorf("expected cost 3, got %d", cost2)
	}
}

// === Opcode Tests ===

func TestOpcodeString(t *testing.T) {
	if ADD.String() != "ADD" {
		t.Errorf("expected ADD, got %s", ADD.String())
	}

	if PUSH32.String() != "PUSH32" {
		t.Errorf("expected PUSH32, got %s", PUSH32.String())
	}
}

func TestOpcodeIsPush(t *testing.T) {
	if !PUSH1.IsPush() {
		t.Error("PUSH1 should be push")
	}
	if !PUSH32.IsPush() {
		t.Error("PUSH32 should be push")
	}
	if ADD.IsPush() {
		t.Error("ADD should not be push")
	}
}

func TestOpcodePushBytes(t *testing.T) {
	if PUSH1.PushBytes() != 1 {
		t.Errorf("expected 1, got %d", PUSH1.PushBytes())
	}
	if PUSH32.PushBytes() != 32 {
		t.Errorf("expected 32, got %d", PUSH32.PushBytes())
	}
	if ADD.PushBytes() != 0 {
		t.Errorf("expected 0, got %d", ADD.PushBytes())
	}
}

func TestOpcodeIsDup(t *testing.T) {
	if !DUP1.IsDup() {
		t.Error("DUP1 should be dup")
	}
	if !DUP16.IsDup() {
		t.Error("DUP16 should be dup")
	}
	if ADD.IsDup() {
		t.Error("ADD should not be dup")
	}
}

func TestOpcodeIsSwap(t *testing.T) {
	if !SWAP1.IsSwap() {
		t.Error("SWAP1 should be swap")
	}
	if !SWAP16.IsSwap() {
		t.Error("SWAP16 should be swap")
	}
	if ADD.IsSwap() {
		t.Error("ADD should not be swap")
	}
}

func TestOpcodeLogTopics(t *testing.T) {
	if LOG0.LogTopics() != 0 {
		t.Errorf("expected 0, got %d", LOG0.LogTopics())
	}
	if LOG4.LogTopics() != 4 {
		t.Errorf("expected 4, got %d", LOG4.LogTopics())
	}
}

func TestOpcodeIsStaticViolation(t *testing.T) {
	if !SSTORE.IsStaticViolation() {
		t.Error("SSTORE should be static violation")
	}
	if !LOG0.IsStaticViolation() {
		t.Error("LOG0 should be static violation")
	}
	if SLOAD.IsStaticViolation() {
		t.Error("SLOAD should not be static violation")
	}
}

// === Code Tests ===

func TestCodeGetOp(t *testing.T) {
	code := Code{byte(PUSH1), 0x42, byte(PUSH1), 0x43, byte(ADD)}

	if code.GetOp(0) != PUSH1 {
		t.Errorf("expected PUSH1, got %v", code.GetOp(0))
	}
	if code.GetOp(4) != ADD {
		t.Errorf("expected ADD, got %v", code.GetOp(4))
	}
	if code.GetOp(100) != STOP {
		t.Errorf("expected STOP for out of bounds, got %v", code.GetOp(100))
	}
}

func TestCodeValidJumpDest(t *testing.T) {
	// PUSH1 0x05 JUMP INVALID INVALID JUMPDEST STOP
	code := Code{byte(PUSH1), 0x05, byte(JUMP), byte(INVALID), byte(INVALID), byte(JUMPDEST), byte(STOP)}

	if !code.ValidJumpDest(5) {
		t.Error("position 5 should be valid JUMPDEST")
	}
	if code.ValidJumpDest(0) {
		t.Error("position 0 should not be valid JUMPDEST")
	}
	if code.ValidJumpDest(1) {
		t.Error("position 1 (in PUSH data) should not be valid JUMPDEST")
	}
}

func TestJumpDestAnalysis(t *testing.T) {
	code := Code{byte(JUMPDEST), byte(PUSH1), 0x5b, byte(JUMPDEST)}

	jda := NewJumpDestAnalysis(code)

	if !jda.Valid(0) {
		t.Error("position 0 should be valid")
	}
	if jda.Valid(2) {
		t.Error("position 2 (0x5b in PUSH data) should not be valid")
	}
	if !jda.Valid(3) {
		t.Error("position 3 should be valid")
	}
}

// === Return Data Tests ===

func TestReturnData(t *testing.T) {
	rd := NewReturnData()

	rd.Set([]byte{0x01, 0x02, 0x03, 0x04})

	if rd.Len() != 4 {
		t.Errorf("expected len 4, got %d", rd.Len())
	}

	data, err := rd.Copy(1, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(data) != 2 || data[0] != 0x02 || data[1] != 0x03 {
		t.Errorf("expected [0x02, 0x03], got %v", data)
	}

	// Out of bounds
	_, err = rd.Copy(3, 5)
	if err == nil {
		t.Error("expected out of bounds error")
	}
}

// === Contract Tests ===

func TestContract(t *testing.T) {
	caller := state.Address{1}
	addr := state.Address{2}

	contract := NewContract(caller, addr, big.NewInt(100), 50000)

	if contract.Caller() != caller {
		t.Error("caller mismatch")
	}
	if contract.Self() != addr {
		t.Error("address mismatch")
	}
	if contract.Value.Cmp(big.NewInt(100)) != 0 {
		t.Error("value mismatch")
	}
}

func TestContractUseGas(t *testing.T) {
	contract := NewContract(state.Address{}, state.Address{}, big.NewInt(0), 1000)

	if !contract.UseGas(500) {
		t.Error("should be able to use 500 gas")
	}
	if contract.Gas != 500 {
		t.Errorf("expected 500 gas remaining, got %d", contract.Gas)
	}

	if contract.UseGas(600) {
		t.Error("should not be able to use 600 gas")
	}
	if contract.Gas != 500 {
		t.Error("gas should not change on failed deduction")
	}
}

func TestContractSetCode(t *testing.T) {
	contract := NewContract(state.Address{}, state.Address{}, big.NewInt(0), 1000)

	code := Code{byte(PUSH1), 0x42, byte(STOP)}
	hash := state.Hash{1, 2, 3}

	contract.SetCode(hash, code)

	if len(contract.Code) != 3 {
		t.Errorf("expected code len 3, got %d", len(contract.Code))
	}
	if contract.CodeHash != hash {
		t.Error("code hash mismatch")
	}
}

// === U256 Helper Tests ===

func TestU256(t *testing.T) {
	// Positive value stays positive
	val := big.NewInt(100)
	result := U256(val)
	if result.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("expected 100, got %s", result.String())
	}

	// Large value is masked
	large := new(big.Int).Lsh(big.NewInt(1), 257) // 2^257
	result = U256(large)
	if result.BitLen() > 256 {
		t.Error("result should fit in 256 bits")
	}
}

func TestS256(t *testing.T) {
	// Positive value
	val := big.NewInt(100)
	result := S256(val)
	if result.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("expected 100, got %s", result.String())
	}

	// Value with high bit set (negative in two's complement)
	highBit := new(big.Int).Lsh(big.NewInt(1), 255)
	result = S256(highBit)
	if result.Sign() >= 0 {
		t.Error("should be negative")
	}
}

func TestBigToHash(t *testing.T) {
	val := big.NewInt(0x12345678)
	hash := BigToHash(val)

	// Should be right-aligned
	if hash[31] != 0x78 || hash[30] != 0x56 || hash[29] != 0x34 || hash[28] != 0x12 {
		t.Errorf("hash mismatch: %x", hash)
	}
}

func TestBigToAddress(t *testing.T) {
	val := big.NewInt(0x12345678)
	addr := BigToAddress(val)

	// Should be right-aligned
	if addr[19] != 0x78 || addr[18] != 0x56 || addr[17] != 0x34 || addr[16] != 0x12 {
		t.Errorf("address mismatch: %x", addr)
	}
}

// === Gas Costs Tests ===

func TestDefaultGasCosts(t *testing.T) {
	costs := DefaultGasCosts()

	if costs.Base != 2 {
		t.Errorf("expected base cost 2, got %d", costs.Base)
	}
	if costs.VeryLow != 3 {
		t.Errorf("expected very low cost 3, got %d", costs.VeryLow)
	}
	if costs.Create != 32000 {
		t.Errorf("expected create cost 32000, got %d", costs.Create)
	}
}

// === EVM Config Tests ===

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.MaxCallDepth != 1024 {
		t.Errorf("expected max call depth 1024, got %d", config.MaxCallDepth)
	}
	if config.MaxCodeSize != 24576 {
		t.Errorf("expected max code size 24576, got %d", config.MaxCodeSize)
	}
}

// === Mock StateDB for Interpreter Tests ===

type mockStateDB struct {
	accounts map[state.Address]*mockAccount
	storage  map[state.Address]map[state.Hash]state.Hash
	code     map[state.Address][]byte
	refund   uint64
}

type mockAccount struct {
	balance *big.Int
	nonce   uint64
}

func newMockStateDB() *mockStateDB {
	return &mockStateDB{
		accounts: make(map[state.Address]*mockAccount),
		storage:  make(map[state.Address]map[state.Hash]state.Hash),
		code:     make(map[state.Address][]byte),
	}
}

func (m *mockStateDB) CreateAccount(addr state.Address) {
	m.accounts[addr] = &mockAccount{balance: big.NewInt(0)}
}
func (m *mockStateDB) SubBalance(addr state.Address, amount *big.Int) {
	if acc := m.accounts[addr]; acc != nil {
		acc.balance.Sub(acc.balance, amount)
	}
}
func (m *mockStateDB) AddBalance(addr state.Address, amount *big.Int) {
	if acc := m.accounts[addr]; acc != nil {
		acc.balance.Add(acc.balance, amount)
	} else {
		m.accounts[addr] = &mockAccount{balance: new(big.Int).Set(amount)}
	}
}
func (m *mockStateDB) GetBalance(addr state.Address) *big.Int {
	if acc := m.accounts[addr]; acc != nil {
		return new(big.Int).Set(acc.balance)
	}
	return big.NewInt(0)
}
func (m *mockStateDB) GetNonce(addr state.Address) uint64 {
	if acc := m.accounts[addr]; acc != nil {
		return acc.nonce
	}
	return 0
}
func (m *mockStateDB) SetNonce(addr state.Address, nonce uint64) {
	if acc := m.accounts[addr]; acc != nil {
		acc.nonce = nonce
	}
}
func (m *mockStateDB) GetCode(addr state.Address) []byte { return m.code[addr] }
func (m *mockStateDB) GetCodeSize(addr state.Address) int { return len(m.code[addr]) }
func (m *mockStateDB) GetCodeHash(addr state.Address) state.Hash {
	code := m.code[addr]
	return Code(code).Hash()
}
func (m *mockStateDB) SetCode(addr state.Address, code []byte) { m.code[addr] = code }
func (m *mockStateDB) GetState(addr state.Address, key state.Hash) state.Hash {
	if s := m.storage[addr]; s != nil {
		return s[key]
	}
	return state.Hash{}
}
func (m *mockStateDB) SetState(addr state.Address, key, val state.Hash) {
	if m.storage[addr] == nil {
		m.storage[addr] = make(map[state.Hash]state.Hash)
	}
	m.storage[addr][key] = val
}
func (m *mockStateDB) GetCommittedState(addr state.Address, key state.Hash) state.Hash {
	return m.GetState(addr, key)
}
func (m *mockStateDB) GetTransientState(state.Address, state.Hash) state.Hash {
	return state.Hash{}
}
func (m *mockStateDB) SetTransientState(state.Address, state.Hash, state.Hash) {}
func (m *mockStateDB) HasSuicided(state.Address) bool                          { return false }
func (m *mockStateDB) Suicide(state.Address) bool                              { return true }
func (m *mockStateDB) Exist(addr state.Address) bool                           { return m.accounts[addr] != nil }
func (m *mockStateDB) Empty(addr state.Address) bool {
	acc := m.accounts[addr]
	return acc == nil || (acc.nonce == 0 && acc.balance.Sign() == 0 && len(m.code[addr]) == 0)
}
func (m *mockStateDB) AddAddressToAccessList(state.Address)                         {}
func (m *mockStateDB) AddSlotToAccessList(state.Address, state.Hash)                {}
func (m *mockStateDB) AddressInAccessList(state.Address) bool                       { return true }
func (m *mockStateDB) SlotInAccessList(state.Address, state.Hash) (bool, bool)      { return true, true }
func (m *mockStateDB) Snapshot() int                                                { return 0 }
func (m *mockStateDB) RevertToSnapshot(int)                                         {}
func (m *mockStateDB) AddRefund(gas uint64)                                         { m.refund += gas }
func (m *mockStateDB) SubRefund(gas uint64)                                         { m.refund -= gas }
func (m *mockStateDB) GetRefund() uint64                                            { return m.refund }

// === Interpreter Tests ===

func TestInterpreterSimpleAdd(t *testing.T) {
	// Code: PUSH1 0x02 PUSH1 0x03 ADD PUSH1 0x00 MSTORE PUSH1 0x20 PUSH1 0x00 RETURN
	code := Code{
		byte(PUSH1), 0x02, // Push 2
		byte(PUSH1), 0x03, // Push 3
		byte(ADD),             // 2 + 3 = 5
		byte(PUSH1), 0x00,     // Push 0 (memory offset)
		byte(MSTORE),          // Store result at offset 0
		byte(PUSH1), 0x20,     // Push 32 (size)
		byte(PUSH1), 0x00,     // Push 0 (offset)
		byte(RETURN), // Return 32 bytes from offset 0
	}

	stateDB := newMockStateDB()
	ctx := NewCallContext(state.Address{1}, big.NewInt(1))
	ctx.BlockNumber = big.NewInt(1)
	ctx.BaseFee = big.NewInt(1)
	ctx.ChainID = big.NewInt(1)

	evm := NewEVM(ctx, stateDB, nil)
	interpreter := NewInterpreter(evm)

	contract := NewContract(state.Address{1}, state.Address{2}, big.NewInt(0), 100000)
	contract.SetCode(state.Hash{}, code)

	result, err := interpreter.Run(contract, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should be 32 bytes with value 5 at the end
	if len(result) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(result))
	}

	val := new(big.Int).SetBytes(result)
	if val.Cmp(big.NewInt(5)) != 0 {
		t.Errorf("expected 5, got %s", val.String())
	}
}

func TestInterpreterStop(t *testing.T) {
	code := Code{byte(STOP)}

	stateDB := newMockStateDB()
	ctx := NewCallContext(state.Address{}, big.NewInt(1))
	ctx.BlockNumber = big.NewInt(1)
	ctx.BaseFee = big.NewInt(1)
	ctx.ChainID = big.NewInt(1)

	evm := NewEVM(ctx, stateDB, nil)
	interpreter := NewInterpreter(evm)

	contract := NewContract(state.Address{}, state.Address{}, big.NewInt(0), 100000)
	contract.SetCode(state.Hash{}, code)

	result, err := interpreter.Run(contract, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for STOP")
	}
}

func TestInterpreterRevert(t *testing.T) {
	// PUSH1 0x00 PUSH1 0x00 REVERT
	code := Code{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(REVERT),
	}

	stateDB := newMockStateDB()
	ctx := NewCallContext(state.Address{}, big.NewInt(1))
	ctx.BlockNumber = big.NewInt(1)
	ctx.BaseFee = big.NewInt(1)
	ctx.ChainID = big.NewInt(1)

	evm := NewEVM(ctx, stateDB, nil)
	interpreter := NewInterpreter(evm)

	contract := NewContract(state.Address{}, state.Address{}, big.NewInt(0), 100000)
	contract.SetCode(state.Hash{}, code)

	_, err := interpreter.Run(contract, nil, false)
	if err != ErrExecutionReverted {
		t.Errorf("expected ErrExecutionReverted, got %v", err)
	}
}

func TestInterpreterEmptyCode(t *testing.T) {
	code := Code{}

	stateDB := newMockStateDB()
	ctx := NewCallContext(state.Address{}, big.NewInt(1))

	evm := NewEVM(ctx, stateDB, nil)
	interpreter := NewInterpreter(evm)

	contract := NewContract(state.Address{}, state.Address{}, big.NewInt(0), 100000)
	contract.SetCode(state.Hash{}, code)

	result, err := interpreter.Run(contract, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for empty code")
	}
}

func TestInterpreterInvalidOpcode(t *testing.T) {
	code := Code{byte(INVALID)}

	stateDB := newMockStateDB()
	ctx := NewCallContext(state.Address{}, big.NewInt(1))

	evm := NewEVM(ctx, stateDB, nil)
	interpreter := NewInterpreter(evm)

	contract := NewContract(state.Address{}, state.Address{}, big.NewInt(0), 100000)
	contract.SetCode(state.Hash{}, code)

	_, err := interpreter.Run(contract, nil, false)
	if err != ErrInvalidOpcode {
		t.Errorf("expected ErrInvalidOpcode, got %v", err)
	}
}

func TestInterpreterStackUnderflow(t *testing.T) {
	// ADD without any values on stack
	code := Code{byte(ADD)}

	stateDB := newMockStateDB()
	ctx := NewCallContext(state.Address{}, big.NewInt(1))

	evm := NewEVM(ctx, stateDB, nil)
	interpreter := NewInterpreter(evm)

	contract := NewContract(state.Address{}, state.Address{}, big.NewInt(0), 100000)
	contract.SetCode(state.Hash{}, code)

	_, err := interpreter.Run(contract, nil, false)
	if err != ErrStackUnderflow_ {
		t.Errorf("expected stack underflow, got %v", err)
	}
}

func TestInterpreterPushAndPop(t *testing.T) {
	// PUSH2 0x1234 POP STOP
	code := Code{
		byte(PUSH2), 0x12, 0x34,
		byte(POP),
		byte(STOP),
	}

	stateDB := newMockStateDB()
	ctx := NewCallContext(state.Address{}, big.NewInt(1))

	evm := NewEVM(ctx, stateDB, nil)
	interpreter := NewInterpreter(evm)

	contract := NewContract(state.Address{}, state.Address{}, big.NewInt(0), 100000)
	contract.SetCode(state.Hash{}, code)

	_, err := interpreter.Run(contract, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInterpreterDupAndSwap(t *testing.T) {
	// PUSH1 0x01 PUSH1 0x02 DUP2 SWAP1 ADD STOP
	// Stack: [1] -> [1,2] -> [1,2,1] -> [1,1,2] -> [1,3]
	code := Code{
		byte(PUSH1), 0x01,
		byte(PUSH1), 0x02,
		byte(DUP2),
		byte(SWAP1),
		byte(ADD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}

	stateDB := newMockStateDB()
	ctx := NewCallContext(state.Address{}, big.NewInt(1))
	ctx.BlockNumber = big.NewInt(1)
	ctx.BaseFee = big.NewInt(1)
	ctx.ChainID = big.NewInt(1)

	evm := NewEVM(ctx, stateDB, nil)
	interpreter := NewInterpreter(evm)

	contract := NewContract(state.Address{}, state.Address{}, big.NewInt(0), 100000)
	contract.SetCode(state.Hash{}, code)

	result, err := interpreter.Run(contract, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	val := new(big.Int).SetBytes(result)
	if val.Cmp(big.NewInt(3)) != 0 {
		t.Errorf("expected 3, got %s", val.String())
	}
}

func TestInterpreterComparison(t *testing.T) {
	// PUSH1 0x05 PUSH1 0x03 LT -> 1 (3 < 5)
	code := Code{
		byte(PUSH1), 0x05,
		byte(PUSH1), 0x03,
		byte(LT),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}

	stateDB := newMockStateDB()
	ctx := NewCallContext(state.Address{}, big.NewInt(1))
	ctx.BlockNumber = big.NewInt(1)
	ctx.BaseFee = big.NewInt(1)
	ctx.ChainID = big.NewInt(1)

	evm := NewEVM(ctx, stateDB, nil)
	interpreter := NewInterpreter(evm)

	contract := NewContract(state.Address{}, state.Address{}, big.NewInt(0), 100000)
	contract.SetCode(state.Hash{}, code)

	result, err := interpreter.Run(contract, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	val := new(big.Int).SetBytes(result)
	if val.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("expected 1, got %s", val.String())
	}
}

func TestInterpreterBitwise(t *testing.T) {
	// PUSH1 0xFF PUSH1 0x0F AND -> 0x0F
	code := Code{
		byte(PUSH1), 0xFF,
		byte(PUSH1), 0x0F,
		byte(AND),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}

	stateDB := newMockStateDB()
	ctx := NewCallContext(state.Address{}, big.NewInt(1))
	ctx.BlockNumber = big.NewInt(1)
	ctx.BaseFee = big.NewInt(1)
	ctx.ChainID = big.NewInt(1)

	evm := NewEVM(ctx, stateDB, nil)
	interpreter := NewInterpreter(evm)

	contract := NewContract(state.Address{}, state.Address{}, big.NewInt(0), 100000)
	contract.SetCode(state.Hash{}, code)

	result, err := interpreter.Run(contract, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	val := new(big.Int).SetBytes(result)
	if val.Cmp(big.NewInt(0x0F)) != 0 {
		t.Errorf("expected 0x0F, got %s", val.String())
	}
}

// === Execution Result Tests ===

func TestExecutionResult(t *testing.T) {
	result := &ExecutionResult{
		UsedGas:    21000,
		Err:        nil,
		ReturnData: []byte{0x01, 0x02},
	}

	if result.Failed() {
		t.Error("should not be failed")
	}
	if result.Revert() {
		t.Error("should not be reverted")
	}

	data, err := result.Unwrap()
	if err != nil {
		t.Error("should have no error")
	}
	if len(data) != 2 {
		t.Errorf("expected 2 bytes, got %d", len(data))
	}
}

func TestExecutionResultRevert(t *testing.T) {
	result := &ExecutionResult{
		Err: ErrExecutionReverted,
	}

	if !result.Failed() {
		t.Error("should be failed")
	}
	if !result.Revert() {
		t.Error("should be reverted")
	}
}

func TestEVMCancel(t *testing.T) {
	stateDB := newMockStateDB()
	ctx := NewCallContext(state.Address{}, big.NewInt(1))

	evm := NewEVM(ctx, stateDB, nil)

	if evm.Cancelled() {
		t.Error("should not be cancelled initially")
	}

	evm.Cancel()

	if !evm.Cancelled() {
		t.Error("should be cancelled after Cancel()")
	}
}
