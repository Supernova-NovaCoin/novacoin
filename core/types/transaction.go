// Package types implements core blockchain types for NovaCoin.
package types

import (
	"encoding/binary"
	"errors"
	"math/big"
	"sync/atomic"

	"github.com/novacoin/novacoin/core/state"
	"github.com/novacoin/novacoin/crypto"
)

// TxType represents the transaction type.
type TxType uint8

const (
	TxTypeLegacy     TxType = 0   // Legacy transaction (pre-EIP-2718)
	TxTypeAccessList TxType = 1   // EIP-2930 access list transaction
	TxTypeDynamicFee TxType = 2   // EIP-1559 dynamic fee transaction
	TxTypeBlob       TxType = 3   // EIP-4844 blob transaction (future)
)

// Transaction represents a blockchain transaction.
type Transaction struct {
	inner TxData

	// Cached values
	hash  atomic.Value // *state.Hash
	size  atomic.Value // uint64
	from  atomic.Value // *state.Address
}

// TxData is the interface for transaction data.
type TxData interface {
	txType() TxType
	copy() TxData

	chainID() *big.Int
	nonce() uint64
	gasPrice() *big.Int
	gasTipCap() *big.Int
	gasFeeCap() *big.Int
	gas() uint64
	to() *state.Address
	value() *big.Int
	data() []byte
	accessList() AccessList

	rawSignatureValues() (v, r, s *big.Int)
	setSignatureValues(chainID, v, r, s *big.Int)
}

// AccessList is a list of addresses and storage keys for EIP-2930.
type AccessList []AccessTuple

// AccessTuple represents an address and its accessed storage keys.
type AccessTuple struct {
	Address     state.Address
	StorageKeys []state.Hash
}

// StorageKeys returns the total number of storage keys in the access list.
func (al AccessList) StorageKeys() int {
	sum := 0
	for _, tuple := range al {
		sum += len(tuple.StorageKeys)
	}
	return sum
}

// LegacyTx is the transaction data for legacy (pre-EIP-2718) transactions.
type LegacyTx struct {
	Nonce    uint64
	GasPrice *big.Int
	Gas      uint64
	To       *state.Address // nil means contract creation
	Value    *big.Int
	Data     []byte
	V, R, S  *big.Int // Signature values
}

func (tx *LegacyTx) txType() TxType      { return TxTypeLegacy }
func (tx *LegacyTx) chainID() *big.Int   { return deriveChainID(tx.V) }
func (tx *LegacyTx) nonce() uint64       { return tx.Nonce }
func (tx *LegacyTx) gasPrice() *big.Int  { return tx.GasPrice }
func (tx *LegacyTx) gasTipCap() *big.Int { return tx.GasPrice }
func (tx *LegacyTx) gasFeeCap() *big.Int { return tx.GasPrice }
func (tx *LegacyTx) gas() uint64         { return tx.Gas }
func (tx *LegacyTx) to() *state.Address  { return tx.To }
func (tx *LegacyTx) value() *big.Int     { return tx.Value }
func (tx *LegacyTx) data() []byte        { return tx.Data }
func (tx *LegacyTx) accessList() AccessList { return nil }

func (tx *LegacyTx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.V, tx.R, tx.S
}

func (tx *LegacyTx) setSignatureValues(chainID, v, r, s *big.Int) {
	tx.V, tx.R, tx.S = v, r, s
}

func (tx *LegacyTx) copy() TxData {
	cpy := &LegacyTx{
		Nonce: tx.Nonce,
		Gas:   tx.Gas,
		To:    copyAddress(tx.To),
		Data:  copyBytes(tx.Data),
	}
	if tx.GasPrice != nil {
		cpy.GasPrice = new(big.Int).Set(tx.GasPrice)
	}
	if tx.Value != nil {
		cpy.Value = new(big.Int).Set(tx.Value)
	}
	if tx.V != nil {
		cpy.V = new(big.Int).Set(tx.V)
	}
	if tx.R != nil {
		cpy.R = new(big.Int).Set(tx.R)
	}
	if tx.S != nil {
		cpy.S = new(big.Int).Set(tx.S)
	}
	return cpy
}

// AccessListTx is the data for EIP-2930 access list transactions.
type AccessListTx struct {
	ChainID    *big.Int
	Nonce      uint64
	GasPrice   *big.Int
	Gas        uint64
	To         *state.Address
	Value      *big.Int
	Data       []byte
	AccessList AccessList
	V, R, S    *big.Int
}

func (tx *AccessListTx) txType() TxType      { return TxTypeAccessList }
func (tx *AccessListTx) chainID() *big.Int   { return tx.ChainID }
func (tx *AccessListTx) nonce() uint64       { return tx.Nonce }
func (tx *AccessListTx) gasPrice() *big.Int  { return tx.GasPrice }
func (tx *AccessListTx) gasTipCap() *big.Int { return tx.GasPrice }
func (tx *AccessListTx) gasFeeCap() *big.Int { return tx.GasPrice }
func (tx *AccessListTx) gas() uint64         { return tx.Gas }
func (tx *AccessListTx) to() *state.Address  { return tx.To }
func (tx *AccessListTx) value() *big.Int     { return tx.Value }
func (tx *AccessListTx) data() []byte        { return tx.Data }
func (tx *AccessListTx) accessList() AccessList { return tx.AccessList }

func (tx *AccessListTx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.V, tx.R, tx.S
}

func (tx *AccessListTx) setSignatureValues(chainID, v, r, s *big.Int) {
	tx.ChainID, tx.V, tx.R, tx.S = chainID, v, r, s
}

func (tx *AccessListTx) copy() TxData {
	cpy := &AccessListTx{
		Nonce:      tx.Nonce,
		Gas:        tx.Gas,
		To:         copyAddress(tx.To),
		Data:       copyBytes(tx.Data),
		AccessList: copyAccessList(tx.AccessList),
	}
	if tx.ChainID != nil {
		cpy.ChainID = new(big.Int).Set(tx.ChainID)
	}
	if tx.GasPrice != nil {
		cpy.GasPrice = new(big.Int).Set(tx.GasPrice)
	}
	if tx.Value != nil {
		cpy.Value = new(big.Int).Set(tx.Value)
	}
	if tx.V != nil {
		cpy.V = new(big.Int).Set(tx.V)
	}
	if tx.R != nil {
		cpy.R = new(big.Int).Set(tx.R)
	}
	if tx.S != nil {
		cpy.S = new(big.Int).Set(tx.S)
	}
	return cpy
}

// DynamicFeeTx is the data for EIP-1559 dynamic fee transactions.
type DynamicFeeTx struct {
	ChainID    *big.Int
	Nonce      uint64
	GasTipCap  *big.Int // Max priority fee per gas (tip)
	GasFeeCap  *big.Int // Max total fee per gas
	Gas        uint64
	To         *state.Address
	Value      *big.Int
	Data       []byte
	AccessList AccessList
	V, R, S    *big.Int
}

func (tx *DynamicFeeTx) txType() TxType    { return TxTypeDynamicFee }
func (tx *DynamicFeeTx) chainID() *big.Int { return tx.ChainID }
func (tx *DynamicFeeTx) nonce() uint64     { return tx.Nonce }
func (tx *DynamicFeeTx) gas() uint64       { return tx.Gas }
func (tx *DynamicFeeTx) to() *state.Address { return tx.To }
func (tx *DynamicFeeTx) value() *big.Int   { return tx.Value }
func (tx *DynamicFeeTx) data() []byte      { return tx.Data }
func (tx *DynamicFeeTx) accessList() AccessList { return tx.AccessList }
func (tx *DynamicFeeTx) gasTipCap() *big.Int    { return tx.GasTipCap }
func (tx *DynamicFeeTx) gasFeeCap() *big.Int    { return tx.GasFeeCap }

func (tx *DynamicFeeTx) gasPrice() *big.Int {
	// For compatibility, return gas fee cap
	return tx.GasFeeCap
}

func (tx *DynamicFeeTx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.V, tx.R, tx.S
}

func (tx *DynamicFeeTx) setSignatureValues(chainID, v, r, s *big.Int) {
	tx.ChainID, tx.V, tx.R, tx.S = chainID, v, r, s
}

func (tx *DynamicFeeTx) copy() TxData {
	cpy := &DynamicFeeTx{
		Nonce:      tx.Nonce,
		Gas:        tx.Gas,
		To:         copyAddress(tx.To),
		Data:       copyBytes(tx.Data),
		AccessList: copyAccessList(tx.AccessList),
	}
	if tx.ChainID != nil {
		cpy.ChainID = new(big.Int).Set(tx.ChainID)
	}
	if tx.GasTipCap != nil {
		cpy.GasTipCap = new(big.Int).Set(tx.GasTipCap)
	}
	if tx.GasFeeCap != nil {
		cpy.GasFeeCap = new(big.Int).Set(tx.GasFeeCap)
	}
	if tx.Value != nil {
		cpy.Value = new(big.Int).Set(tx.Value)
	}
	if tx.V != nil {
		cpy.V = new(big.Int).Set(tx.V)
	}
	if tx.R != nil {
		cpy.R = new(big.Int).Set(tx.R)
	}
	if tx.S != nil {
		cpy.S = new(big.Int).Set(tx.S)
	}
	return cpy
}

// NewTransaction creates a new transaction.
func NewTransaction(nonce uint64, to state.Address, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *Transaction {
	return NewTx(&LegacyTx{
		Nonce:    nonce,
		To:       &to,
		Value:    amount,
		Gas:      gasLimit,
		GasPrice: gasPrice,
		Data:     data,
	})
}

// NewContractCreation creates a new contract creation transaction.
func NewContractCreation(nonce uint64, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *Transaction {
	return NewTx(&LegacyTx{
		Nonce:    nonce,
		Value:    amount,
		Gas:      gasLimit,
		GasPrice: gasPrice,
		Data:     data,
	})
}

// NewTx creates a new transaction from TxData.
func NewTx(inner TxData) *Transaction {
	tx := new(Transaction)
	tx.setDecoded(inner.copy(), 0)
	return tx
}

func (tx *Transaction) setDecoded(inner TxData, size uint64) {
	tx.inner = inner
	if size > 0 {
		tx.size.Store(size)
	}
}

// Type returns the transaction type.
func (tx *Transaction) Type() TxType {
	return tx.inner.txType()
}

// ChainID returns the chain ID of the transaction.
func (tx *Transaction) ChainID() *big.Int {
	return tx.inner.chainID()
}

// Nonce returns the nonce of the transaction.
func (tx *Transaction) Nonce() uint64 {
	return tx.inner.nonce()
}

// GasPrice returns the gas price of the transaction.
func (tx *Transaction) GasPrice() *big.Int {
	return new(big.Int).Set(tx.inner.gasPrice())
}

// GasTipCap returns the gas tip cap (max priority fee).
func (tx *Transaction) GasTipCap() *big.Int {
	return new(big.Int).Set(tx.inner.gasTipCap())
}

// GasFeeCap returns the gas fee cap (max total fee).
func (tx *Transaction) GasFeeCap() *big.Int {
	return new(big.Int).Set(tx.inner.gasFeeCap())
}

// Gas returns the gas limit.
func (tx *Transaction) Gas() uint64 {
	return tx.inner.gas()
}

// To returns the recipient address.
func (tx *Transaction) To() *state.Address {
	return copyAddress(tx.inner.to())
}

// Value returns the value being transferred.
func (tx *Transaction) Value() *big.Int {
	return new(big.Int).Set(tx.inner.value())
}

// Data returns the transaction data.
func (tx *Transaction) Data() []byte {
	return copyBytes(tx.inner.data())
}

// AccessList returns the access list.
func (tx *Transaction) AccessList() AccessList {
	return tx.inner.accessList()
}

// RawSignatureValues returns the signature values.
func (tx *Transaction) RawSignatureValues() (v, r, s *big.Int) {
	return tx.inner.rawSignatureValues()
}

// Hash returns the transaction hash.
func (tx *Transaction) Hash() state.Hash {
	if hash := tx.hash.Load(); hash != nil {
		return *hash.(*state.Hash)
	}

	var h state.Hash
	data := tx.serialize()
	hashBytes := crypto.Hash(data)
	copy(h[:], hashBytes[:])

	tx.hash.Store(&h)
	return h
}

// serialize serializes the transaction for hashing.
func (tx *Transaction) serialize() []byte {
	// Simple serialization for hashing
	// In production, use RLP encoding
	inner := tx.inner

	size := 1 + 8 + 8 + 8 // type + nonce + gas + value_len
	if inner.value() != nil {
		size += len(inner.value().Bytes())
	}
	if inner.to() != nil {
		size += 20
	}
	size += 4 + len(inner.data()) // data_len + data
	if inner.gasPrice() != nil {
		size += len(inner.gasPrice().Bytes())
	}

	data := make([]byte, size)
	offset := 0

	data[offset] = byte(inner.txType())
	offset++

	binary.BigEndian.PutUint64(data[offset:], inner.nonce())
	offset += 8

	binary.BigEndian.PutUint64(data[offset:], inner.gas())
	offset += 8

	if inner.to() != nil {
		copy(data[offset:], inner.to()[:])
		offset += 20
	}

	if inner.value() != nil {
		valBytes := inner.value().Bytes()
		binary.BigEndian.PutUint64(data[offset:], uint64(len(valBytes)))
		offset += 8
		copy(data[offset:], valBytes)
		offset += len(valBytes)
	}

	binary.BigEndian.PutUint32(data[offset:], uint32(len(inner.data())))
	offset += 4
	copy(data[offset:], inner.data())

	return data
}

// Cost returns the total cost of the transaction (gas * price + value).
func (tx *Transaction) Cost() *big.Int {
	total := new(big.Int).Mul(tx.GasPrice(), new(big.Int).SetUint64(tx.Gas()))
	total.Add(total, tx.Value())
	return total
}

// EffectiveGasTip returns the effective gas tip given a base fee.
func (tx *Transaction) EffectiveGasTip(baseFee *big.Int) *big.Int {
	if baseFee == nil {
		return tx.GasTipCap()
	}

	// tip = min(gasTipCap, gasFeeCap - baseFee)
	tip := new(big.Int).Sub(tx.GasFeeCap(), baseFee)
	if tip.Cmp(tx.GasTipCap()) > 0 {
		tip = tx.GasTipCap()
	}
	if tip.Sign() < 0 {
		return big.NewInt(0)
	}
	return tip
}

// EffectiveGasPrice returns the effective gas price given a base fee.
func (tx *Transaction) EffectiveGasPrice(baseFee *big.Int) *big.Int {
	if baseFee == nil {
		return tx.GasPrice()
	}
	tip := tx.EffectiveGasTip(baseFee)
	return new(big.Int).Add(tip, baseFee)
}

// IsContractCreation returns true if this is a contract creation.
func (tx *Transaction) IsContractCreation() bool {
	return tx.inner.to() == nil
}

// Copy creates a deep copy of the transaction.
func (tx *Transaction) Copy() *Transaction {
	return &Transaction{
		inner: tx.inner.copy(),
	}
}

// WithSignature returns a new transaction with the given signature.
func (tx *Transaction) WithSignature(signer Signer, sig []byte) (*Transaction, error) {
	r, s, v, err := signer.SignatureValues(tx, sig)
	if err != nil {
		return nil, err
	}

	cpy := tx.Copy()
	cpy.inner.setSignatureValues(signer.ChainID(), v, r, s)
	return cpy, nil
}

// Signer is the interface for transaction signing.
type Signer interface {
	// Sender returns the sender address.
	Sender(tx *Transaction) (state.Address, error)

	// SignatureValues returns the signature values.
	SignatureValues(tx *Transaction, sig []byte) (r, s, v *big.Int, err error)

	// ChainID returns the chain ID.
	ChainID() *big.Int

	// Hash returns the hash to be signed.
	Hash(tx *Transaction) state.Hash

	// Equal returns true if signers are equivalent.
	Equal(Signer) bool
}

// LatestSigner returns the signer for the latest transaction types.
func LatestSigner(chainID *big.Int) Signer {
	return NewLondonSigner(chainID)
}

// LondonSigner is a signer for EIP-1559 transactions.
type LondonSigner struct {
	chainID *big.Int
}

// NewLondonSigner creates a new London signer.
func NewLondonSigner(chainID *big.Int) *LondonSigner {
	return &LondonSigner{chainID: chainID}
}

func (s *LondonSigner) ChainID() *big.Int {
	return s.chainID
}

func (s *LondonSigner) Equal(other Signer) bool {
	if ls, ok := other.(*LondonSigner); ok {
		return s.chainID.Cmp(ls.chainID) == 0
	}
	return false
}

func (s *LondonSigner) Sender(tx *Transaction) (state.Address, error) {
	if tx.Type() == TxTypeLegacy {
		v, r, _ := tx.RawSignatureValues()
		if v == nil || r == nil {
			return state.Address{}, ErrInvalidSignature
		}
	}

	// For now, return a placeholder
	// In production, recover the public key from signature
	v, r, ss := tx.RawSignatureValues()
	if v == nil || r == nil || ss == nil {
		return state.Address{}, ErrInvalidSignature
	}

	// Simplified sender recovery (placeholder)
	hash := s.Hash(tx)
	return state.AddressFromBytes(hash[:20]), nil
}

func (s *LondonSigner) SignatureValues(tx *Transaction, sig []byte) (r, s2, v *big.Int, err error) {
	if len(sig) != 65 {
		return nil, nil, nil, ErrInvalidSignatureLength
	}

	r = new(big.Int).SetBytes(sig[:32])
	s2 = new(big.Int).SetBytes(sig[32:64])
	v = new(big.Int).SetBytes([]byte{sig[64]})

	switch tx.Type() {
	case TxTypeLegacy:
		// EIP-155: v = chainID * 2 + 35 + recovery_id
		v = new(big.Int).Add(v, new(big.Int).Mul(s.chainID, big.NewInt(2)))
		v.Add(v, big.NewInt(35))
	case TxTypeAccessList, TxTypeDynamicFee:
		// EIP-2930/EIP-1559: v is just recovery_id (0 or 1)
	}

	return r, s2, v, nil
}

func (s *LondonSigner) Hash(tx *Transaction) state.Hash {
	return tx.Hash()
}

// Helper functions

func copyAddress(addr *state.Address) *state.Address {
	if addr == nil {
		return nil
	}
	cpy := *addr
	return &cpy
}

func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	cpy := make([]byte, len(b))
	copy(cpy, b)
	return cpy
}

func copyAccessList(al AccessList) AccessList {
	if al == nil {
		return nil
	}
	cpy := make(AccessList, len(al))
	for i, tuple := range al {
		cpy[i].Address = tuple.Address
		if tuple.StorageKeys != nil {
			cpy[i].StorageKeys = make([]state.Hash, len(tuple.StorageKeys))
			copy(cpy[i].StorageKeys, tuple.StorageKeys)
		}
	}
	return cpy
}

func deriveChainID(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	if v.BitLen() <= 8 {
		vv := v.Uint64()
		if vv == 27 || vv == 28 {
			return nil // Pre-EIP-155
		}
		// EIP-155: v = chainID * 2 + 35 + recovery_id
		return new(big.Int).SetUint64((vv - 35) / 2)
	}
	// Large v
	v = new(big.Int).Sub(v, big.NewInt(35))
	return v.Div(v, big.NewInt(2))
}

// Transactions is a slice of transactions.
type Transactions []*Transaction

// Len returns the number of transactions.
func (txs Transactions) Len() int { return len(txs) }

// Error types
var (
	ErrInvalidSignature       = errors.New("invalid signature")
	ErrInvalidSignatureLength = errors.New("invalid signature length")
	ErrInvalidChainID         = errors.New("invalid chain id")
	ErrTxTypeNotSupported     = errors.New("transaction type not supported")
)
