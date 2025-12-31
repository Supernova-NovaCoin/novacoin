// Package evm implements the Ethereum Virtual Machine for NovaCoin.
package evm

import (
	"fmt"
	"math/big"
	"sync"
)

// StackLimit is the maximum number of items allowed on the stack.
const StackLimit = 1024

// Stack represents the EVM stack.
type Stack struct {
	data []*big.Int
}

// stackPool is a pool of stacks to reduce allocations.
var stackPool = sync.Pool{
	New: func() interface{} {
		return &Stack{
			data: make([]*big.Int, 0, 16),
		}
	},
}

// NewStack creates a new stack from the pool.
func NewStack() *Stack {
	return stackPool.Get().(*Stack)
}

// ReturnStack returns the stack to the pool.
func ReturnStack(s *Stack) {
	s.data = s.data[:0]
	stackPool.Put(s)
}

// Push pushes a value onto the stack.
func (s *Stack) Push(val *big.Int) {
	s.data = append(s.data, val)
}

// PushN pushes n items onto the stack.
func (s *Stack) PushN(vals ...*big.Int) {
	s.data = append(s.data, vals...)
}

// Pop removes and returns the top item from the stack.
func (s *Stack) Pop() *big.Int {
	if len(s.data) == 0 {
		return nil
	}
	ret := s.data[len(s.data)-1]
	s.data = s.data[:len(s.data)-1]
	return ret
}

// Peek returns the top item without removing it.
func (s *Stack) Peek() *big.Int {
	if len(s.data) == 0 {
		return nil
	}
	return s.data[len(s.data)-1]
}

// PeekN returns the nth item from the top (0 = top).
func (s *Stack) PeekN(n int) *big.Int {
	if n >= len(s.data) {
		return nil
	}
	return s.data[len(s.data)-1-n]
}

// Back returns the item at position n from the bottom.
func (s *Stack) Back(n int) *big.Int {
	if n >= len(s.data) {
		return nil
	}
	return s.data[len(s.data)-1-n]
}

// Swap swaps the top item with the item at position n.
func (s *Stack) Swap(n int) {
	if n >= len(s.data) {
		return
	}
	top := len(s.data) - 1
	s.data[top], s.data[top-n] = s.data[top-n], s.data[top]
}

// Dup duplicates the item at position n and pushes it.
func (s *Stack) Dup(n int) {
	if n > len(s.data) || n < 1 {
		return
	}
	val := new(big.Int).Set(s.data[len(s.data)-n])
	s.data = append(s.data, val)
}

// Len returns the number of items on the stack.
func (s *Stack) Len() int {
	return len(s.data)
}

// Cap returns the capacity of the stack.
func (s *Stack) Cap() int {
	return cap(s.data)
}

// Data returns the underlying stack data (for debugging).
func (s *Stack) Data() []*big.Int {
	return s.data
}

// Require checks if there are at least n items on the stack.
func (s *Stack) Require(n int) error {
	if len(s.data) < n {
		return fmt.Errorf("stack underflow: have %d, need %d", len(s.data), n)
	}
	return nil
}

// ValidSize checks if pushing n items would exceed the stack limit.
func (s *Stack) ValidSize(n int) error {
	if len(s.data)+n > StackLimit {
		return fmt.Errorf("stack overflow: have %d, adding %d, max %d", len(s.data), n, StackLimit)
	}
	return nil
}

// String returns a string representation of the stack.
func (s *Stack) String() string {
	str := "[\n"
	for i := len(s.data) - 1; i >= 0; i-- {
		str += fmt.Sprintf("  %d: %s\n", len(s.data)-1-i, s.data[i].String())
	}
	str += "]"
	return str
}

// Copy creates a deep copy of the stack.
func (s *Stack) Copy() *Stack {
	cpy := &Stack{
		data: make([]*big.Int, len(s.data)),
	}
	for i, val := range s.data {
		cpy.data[i] = new(big.Int).Set(val)
	}
	return cpy
}

// === Stack Errors ===

// StackError represents a stack error.
type StackError string

func (e StackError) Error() string {
	return string(e)
}

const (
	ErrStackUnderflow = StackError("stack underflow")
	ErrStackOverflow  = StackError("stack limit reached 1024")
)

// === Uint256 Helper Functions ===

// Uint256Max is 2^256 - 1.
var Uint256Max = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// Uint256Zero is 0.
var Uint256Zero = big.NewInt(0)

// Uint256One is 1.
var Uint256One = big.NewInt(1)

// ToU256 converts a big.Int to a 256-bit unsigned integer (modular).
func ToU256(x *big.Int) *big.Int {
	if x.Sign() < 0 || x.BitLen() > 256 {
		// Wrap around for 256-bit
		result := new(big.Int)
		result.And(x, Uint256Max)
		return result
	}
	return x
}

// FromU256 converts from unsigned 256-bit to signed interpretation.
func FromU256(x *big.Int) *big.Int {
	if x.Bit(255) == 1 {
		// Negative in two's complement
		result := new(big.Int).Sub(x, new(big.Int).Lsh(big.NewInt(1), 256))
		return result
	}
	return new(big.Int).Set(x)
}

// S256 interprets x as a signed 256-bit integer.
func S256(x *big.Int) *big.Int {
	if x.Cmp(new(big.Int).Lsh(big.NewInt(1), 255)) < 0 {
		return new(big.Int).Set(x)
	}
	return new(big.Int).Sub(x, new(big.Int).Lsh(big.NewInt(1), 256))
}

// U256 interprets x as an unsigned 256-bit integer (two's complement).
func U256(x *big.Int) *big.Int {
	return new(big.Int).And(x, Uint256Max)
}

// BigToHash converts a big.Int to a 32-byte hash.
func BigToHash(x *big.Int) [32]byte {
	var hash [32]byte
	bytes := x.Bytes()
	if len(bytes) > 32 {
		bytes = bytes[len(bytes)-32:]
	}
	copy(hash[32-len(bytes):], bytes)
	return hash
}

// HashToBig converts a 32-byte hash to a big.Int.
func HashToBig(hash [32]byte) *big.Int {
	return new(big.Int).SetBytes(hash[:])
}

// BigToAddress converts a big.Int to a 20-byte address.
func BigToAddress(x *big.Int) [20]byte {
	var addr [20]byte
	bytes := x.Bytes()
	if len(bytes) > 20 {
		bytes = bytes[len(bytes)-20:]
	}
	copy(addr[20-len(bytes):], bytes)
	return addr
}

// AddressToBig converts a 20-byte address to a big.Int.
func AddressToBig(addr [20]byte) *big.Int {
	return new(big.Int).SetBytes(addr[:])
}
