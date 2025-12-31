// Package state implements the blockchain state management layer for NovaCoin.
// It provides a Merkle Patricia Trie-based state database for storing account
// balances, nonces, contract code, and storage.
package state

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/novacoin/novacoin/crypto"
)

// Address represents a 20-byte account address (EVM compatible).
type Address [20]byte

// EmptyAddress is the zero address.
var EmptyAddress = Address{}

// Bytes returns the address as a byte slice.
func (a Address) Bytes() []byte {
	return a[:]
}

// String returns hex representation of the address.
func (a Address) String() string {
	hex := "0123456789abcdef"
	result := make([]byte, 42)
	result[0] = '0'
	result[1] = 'x'
	for i, b := range a {
		result[2+i*2] = hex[b>>4]
		result[2+i*2+1] = hex[b&0x0f]
	}
	return string(result)
}

// AddressFromBytes creates an address from bytes.
func AddressFromBytes(b []byte) Address {
	var addr Address
	if len(b) >= 20 {
		copy(addr[:], b[:20])
	} else {
		copy(addr[20-len(b):], b)
	}
	return addr
}

// AddressFromPublicKey derives address from a public key (last 20 bytes of hash).
func AddressFromPublicKey(pubKey []byte) Address {
	hash := crypto.Hash(pubKey)
	var addr Address
	copy(addr[:], hash[12:32])
	return addr
}

// Hash represents a 32-byte hash.
type Hash [32]byte

// EmptyHash is the zero hash.
var EmptyHash = Hash{}

// EmptyCodeHash is the hash of empty code (Keccak256 of empty bytes).
var EmptyCodeHash = crypto.Hash(nil)

// EmptyRootHash is the hash of an empty trie.
var EmptyRootHash = Hash{
	0x56, 0xe8, 0x1f, 0x17, 0x1b, 0xcc, 0x55, 0xa6,
	0xff, 0x83, 0x45, 0xe6, 0x92, 0xc0, 0xf8, 0x6e,
	0x5b, 0x48, 0xe0, 0x1b, 0x99, 0x6c, 0xad, 0xc0,
	0x01, 0x62, 0x2f, 0xb5, 0xe3, 0x63, 0xb4, 0x21,
}

// Bytes returns the hash as a byte slice.
func (h Hash) Bytes() []byte {
	return h[:]
}

// IsEmpty returns true if the hash is zero.
func (h Hash) IsEmpty() bool {
	return h == EmptyHash
}

// HashFromBytes creates a hash from bytes.
func HashFromBytes(b []byte) Hash {
	var hash Hash
	if len(b) >= 32 {
		copy(hash[:], b[:32])
	} else {
		copy(hash[32-len(b):], b)
	}
	return hash
}

// Account represents the state of an account in the blockchain.
// This is EVM-compatible: balance, nonce, code hash, and storage root.
type Account struct {
	// Nonce is the number of transactions sent from this account.
	// For contract accounts, it's the number of contract creations.
	Nonce uint64

	// Balance is the account's balance in the smallest denomination.
	Balance *big.Int

	// StorageRoot is the root hash of the account's storage trie.
	// For EOAs without storage, this is EmptyRootHash.
	StorageRoot Hash

	// CodeHash is the hash of the account's EVM bytecode.
	// For EOAs, this is EmptyCodeHash.
	CodeHash Hash
}

// NewAccount creates a new empty account.
func NewAccount() *Account {
	return &Account{
		Nonce:       0,
		Balance:     new(big.Int),
		StorageRoot: EmptyRootHash,
		CodeHash:    HashFromBytes(EmptyCodeHash[:]),
	}
}

// NewAccountWithBalance creates an account with initial balance.
func NewAccountWithBalance(balance *big.Int) *Account {
	acc := NewAccount()
	if balance != nil {
		acc.Balance = new(big.Int).Set(balance)
	}
	return acc
}

// IsEmpty returns true if the account is considered empty.
// An account is empty if it has zero nonce, zero balance, and no code.
func (a *Account) IsEmpty() bool {
	return a.Nonce == 0 &&
		(a.Balance == nil || a.Balance.Sign() == 0) &&
		a.CodeHash == HashFromBytes(EmptyCodeHash[:])
}

// IsContract returns true if the account has code (is a contract).
func (a *Account) IsContract() bool {
	return a.CodeHash != HashFromBytes(EmptyCodeHash[:])
}

// Copy creates a deep copy of the account.
func (a *Account) Copy() *Account {
	cpy := &Account{
		Nonce:       a.Nonce,
		StorageRoot: a.StorageRoot,
		CodeHash:    a.CodeHash,
	}
	if a.Balance != nil {
		cpy.Balance = new(big.Int).Set(a.Balance)
	} else {
		cpy.Balance = new(big.Int)
	}
	return cpy
}

// Serialize serializes the account for storage.
// Format: nonce(8) + balance_len(4) + balance + storage_root(32) + code_hash(32)
func (a *Account) Serialize() []byte {
	balanceBytes := []byte{}
	if a.Balance != nil && a.Balance.Sign() != 0 {
		balanceBytes = a.Balance.Bytes()
	}

	size := 8 + 4 + len(balanceBytes) + 32 + 32
	data := make([]byte, size)

	offset := 0

	// Nonce
	binary.BigEndian.PutUint64(data[offset:], a.Nonce)
	offset += 8

	// Balance length and bytes
	binary.BigEndian.PutUint32(data[offset:], uint32(len(balanceBytes)))
	offset += 4
	copy(data[offset:], balanceBytes)
	offset += len(balanceBytes)

	// Storage root
	copy(data[offset:], a.StorageRoot[:])
	offset += 32

	// Code hash
	copy(data[offset:], a.CodeHash[:])

	return data
}

// DeserializeAccount deserializes an account from bytes.
func DeserializeAccount(data []byte) (*Account, error) {
	if len(data) < 8+4+32+32 {
		return nil, ErrInvalidAccountData
	}

	offset := 0

	// Nonce
	nonce := binary.BigEndian.Uint64(data[offset:])
	offset += 8

	// Balance
	balanceLen := binary.BigEndian.Uint32(data[offset:])
	offset += 4

	if len(data) < offset+int(balanceLen)+64 {
		return nil, ErrInvalidAccountData
	}

	balance := new(big.Int)
	if balanceLen > 0 {
		balance.SetBytes(data[offset : offset+int(balanceLen)])
	}
	offset += int(balanceLen)

	// Storage root
	var storageRoot Hash
	copy(storageRoot[:], data[offset:offset+32])
	offset += 32

	// Code hash
	var codeHash Hash
	copy(codeHash[:], data[offset:offset+32])

	return &Account{
		Nonce:       nonce,
		Balance:     balance,
		StorageRoot: storageRoot,
		CodeHash:    codeHash,
	}, nil
}

// AccountChange represents a change to an account for journaling.
type AccountChange struct {
	Address     Address
	PrevAccount *Account // nil if account didn't exist
	NewAccount  *Account // nil if account was deleted
}

// StorageChange represents a change to storage for journaling.
type StorageChange struct {
	Address  Address
	Key      Hash
	PrevValue Hash
	NewValue  Hash
}

// CodeChange represents a change to contract code.
type CodeChange struct {
	Address  Address
	PrevCode []byte
	NewCode  []byte
}

// Error types
var (
	ErrInvalidAccountData = errors.New("invalid account data")
	ErrAccountNotFound    = errors.New("account not found")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrNonceOverflow      = errors.New("nonce overflow")
)
