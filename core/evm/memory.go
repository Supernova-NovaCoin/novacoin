// Package evm implements the Ethereum Virtual Machine for NovaCoin.
package evm

import (
	"fmt"
	"math/big"
)

// Memory represents the EVM memory.
type Memory struct {
	store       []byte
	lastGasCost uint64
}

// NewMemory creates a new memory instance.
func NewMemory() *Memory {
	return &Memory{
		store: make([]byte, 0, 4096),
	}
}

// Set copies data to memory at the given offset.
func (m *Memory) Set(offset, size uint64, data []byte) {
	if size == 0 {
		return
	}

	// Ensure memory is large enough
	if offset+size > uint64(len(m.store)) {
		m.Resize(offset + size)
	}

	copy(m.store[offset:offset+size], data)
}

// Set32 copies a 32-byte value to memory at the given offset.
func (m *Memory) Set32(offset uint64, val *big.Int) {
	if offset+32 > uint64(len(m.store)) {
		m.Resize(offset + 32)
	}

	// Clear the 32 bytes first
	copy(m.store[offset:offset+32], make([]byte, 32))

	// Set the value (right-aligned in 32 bytes)
	bytes := val.Bytes()
	if len(bytes) > 32 {
		bytes = bytes[len(bytes)-32:]
	}
	copy(m.store[offset+32-uint64(len(bytes)):offset+32], bytes)
}

// SetByte sets a single byte at the given offset.
func (m *Memory) SetByte(offset uint64, val byte) {
	if offset >= uint64(len(m.store)) {
		m.Resize(offset + 1)
	}
	m.store[offset] = val
}

// Get returns a slice of memory.
func (m *Memory) Get(offset, size int64) []byte {
	if size == 0 {
		return nil
	}

	if offset+size > int64(len(m.store)) {
		return nil
	}

	return m.store[offset : offset+size]
}

// GetCopy returns a copy of a slice of memory.
func (m *Memory) GetCopy(offset, size int64) []byte {
	if size == 0 {
		return nil
	}

	if offset+size > int64(len(m.store)) {
		// Pad with zeros
		result := make([]byte, size)
		if offset < int64(len(m.store)) {
			copy(result, m.store[offset:])
		}
		return result
	}

	cpy := make([]byte, size)
	copy(cpy, m.store[offset:offset+size])
	return cpy
}

// GetPtr returns a pointer to memory (for reading).
func (m *Memory) GetPtr(offset, size int64) []byte {
	if size == 0 {
		return nil
	}

	if offset+size > int64(len(m.store)) {
		return nil
	}

	return m.store[offset : offset+size]
}

// Resize resizes the memory to the given size.
func (m *Memory) Resize(size uint64) {
	if uint64(len(m.store)) >= size {
		return
	}

	// Round up to 32-byte boundary
	size = (size + 31) &^ 31

	newStore := make([]byte, size)
	copy(newStore, m.store)
	m.store = newStore
}

// Len returns the length of the memory.
func (m *Memory) Len() int {
	return len(m.store)
}

// Cap returns the capacity of the memory.
func (m *Memory) Cap() int {
	return cap(m.store)
}

// Data returns the backing slice (for debugging).
func (m *Memory) Data() []byte {
	return m.store
}

// Copy creates a copy of the memory.
func (m *Memory) Copy() *Memory {
	cpy := &Memory{
		store:       make([]byte, len(m.store)),
		lastGasCost: m.lastGasCost,
	}
	copy(cpy.store, m.store)
	return cpy
}

// String returns a hex representation of memory (limited).
func (m *Memory) String() string {
	if len(m.store) == 0 {
		return "[]"
	}

	limit := len(m.store)
	if limit > 64 {
		limit = 64
	}

	str := fmt.Sprintf("[%d bytes: ", len(m.store))
	for i := 0; i < limit; i++ {
		str += fmt.Sprintf("%02x", m.store[i])
		if i < limit-1 && (i+1)%32 == 0 {
			str += " "
		}
	}
	if len(m.store) > 64 {
		str += "..."
	}
	str += "]"
	return str
}

// === Memory Gas Calculation ===

// MemoryGasCost calculates the gas cost for memory expansion.
func MemoryGasCost(mem *Memory, newSize uint64) (uint64, error) {
	if newSize == 0 {
		return 0, nil
	}

	// Round up to 32-byte words
	newSizeWords := toWordSize(newSize)
	newCost := memoryCost(newSizeWords)

	if newCost < mem.lastGasCost {
		return 0, nil
	}

	cost := newCost - mem.lastGasCost
	mem.lastGasCost = newCost

	// Check for overflow
	if newCost < cost {
		return 0, ErrGasUintOverflow
	}

	return cost, nil
}

// toWordSize returns the number of 32-byte words needed for size bytes.
func toWordSize(size uint64) uint64 {
	if size > maxUint64-31 {
		return maxUint64/32 + 1
	}
	return (size + 31) / 32
}

// memoryCost calculates the quadratic memory cost for a given number of words.
func memoryCost(words uint64) uint64 {
	// memory_cost = (words^2) / 512 + 3 * words
	return (words*words)/512 + 3*words
}

const maxUint64 = ^uint64(0)

// ErrGasUintOverflow is returned when gas calculation overflows.
var ErrGasUintOverflow = fmt.Errorf("gas uint64 overflow")

// CalcMemorySize calculates required memory size for an operation.
func CalcMemorySize(offset, size *big.Int) (uint64, bool) {
	if size.Sign() == 0 {
		return 0, true
	}

	// Check if offset + size overflows
	if offset.BitLen() > 64 {
		return 0, false
	}
	if size.BitLen() > 64 {
		return 0, false
	}

	off := offset.Uint64()
	sz := size.Uint64()

	// Check for overflow
	if off > maxUint64-sz {
		return 0, false
	}

	return off + sz, true
}

// === Return Data Buffer ===

// ReturnData holds the return data from the last call.
type ReturnData struct {
	data []byte
}

// NewReturnData creates a new return data buffer.
func NewReturnData() *ReturnData {
	return &ReturnData{}
}

// Set sets the return data.
func (rd *ReturnData) Set(data []byte) {
	if data == nil {
		rd.data = nil
		return
	}
	rd.data = make([]byte, len(data))
	copy(rd.data, data)
}

// Get returns the return data.
func (rd *ReturnData) Get() []byte {
	return rd.data
}

// Len returns the length of return data.
func (rd *ReturnData) Len() int {
	return len(rd.data)
}

// Copy returns a copy of the return data at the given offset and size.
func (rd *ReturnData) Copy(offset, size uint64) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}

	// Check bounds
	if offset > uint64(len(rd.data)) {
		return nil, fmt.Errorf("return data out of bounds: offset %d, len %d", offset, len(rd.data))
	}

	end := offset + size
	if end > uint64(len(rd.data)) {
		return nil, fmt.Errorf("return data out of bounds: end %d, len %d", end, len(rd.data))
	}

	result := make([]byte, size)
	copy(result, rd.data[offset:end])
	return result, nil
}

// === Code Buffer ===

// Code represents contract bytecode.
type Code []byte

// GetOp returns the opcode at the given position.
func (c Code) GetOp(n uint64) OpCode {
	if n >= uint64(len(c)) {
		return STOP
	}
	return OpCode(c[n])
}

// GetData returns a slice of code for PUSH operations.
func (c Code) GetData(start, size uint64) []byte {
	if start >= uint64(len(c)) {
		return make([]byte, size)
	}

	end := start + size
	if end > uint64(len(c)) {
		// Pad with zeros
		result := make([]byte, size)
		copy(result, c[start:])
		return result
	}

	return c[start:end]
}

// Len returns the length of the code.
func (c Code) Len() int {
	return len(c)
}

// Hash computes the code hash.
func (c Code) Hash() [32]byte {
	// Simple hash implementation - in production would use Keccak256
	var hash [32]byte
	for i, b := range c {
		hash[i%32] ^= b
		hash[(i+1)%32] ^= b << 4
		hash[(i+7)%32] ^= b >> 3
	}
	return hash
}

// ValidJumpDest checks if a position is a valid JUMPDEST.
func (c Code) ValidJumpDest(dest uint64) bool {
	if dest >= uint64(len(c)) {
		return false
	}

	// Must be JUMPDEST opcode
	if c[dest] != byte(JUMPDEST) {
		return false
	}

	// Check it's not in the middle of a PUSH
	// Scan from the beginning to check
	for i := uint64(0); i < dest; {
		op := OpCode(c[i])
		if op.IsPush() {
			// Skip the push data
			pushBytes := op.PushBytes()
			if i+1+uint64(pushBytes) > dest && dest > i {
				// dest is in the middle of push data
				return false
			}
			i += 1 + uint64(pushBytes)
		} else {
			i++
		}
	}

	return true
}

// JumpDestAnalysis returns a bitmap of valid jump destinations.
type JumpDestAnalysis struct {
	bitmap []byte
}

// NewJumpDestAnalysis analyzes code for valid jump destinations.
func NewJumpDestAnalysis(code Code) *JumpDestAnalysis {
	jda := &JumpDestAnalysis{
		bitmap: make([]byte, (len(code)+7)/8),
	}

	for i := 0; i < len(code); {
		op := OpCode(code[i])
		if op == JUMPDEST {
			jda.bitmap[i/8] |= 1 << (i % 8)
		}
		if op.IsPush() {
			i += 1 + op.PushBytes()
		} else {
			i++
		}
	}

	return jda
}

// Valid checks if a position is a valid jump destination.
func (jda *JumpDestAnalysis) Valid(dest uint64) bool {
	if dest >= uint64(len(jda.bitmap)*8) {
		return false
	}
	return jda.bitmap[dest/8]&(1<<(dest%8)) != 0
}
