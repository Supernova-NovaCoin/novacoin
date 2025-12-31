// Package mev implements MEV resistance for NovaCoin.
package mev

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"sync"

	"github.com/novacoin/novacoin/core/dag"
	"github.com/novacoin/novacoin/crypto"
)

// EncryptedTransaction represents an encrypted transaction.
type EncryptedTransaction struct {
	Nonce      [12]byte // AES-GCM nonce
	Ciphertext []byte   // Encrypted transaction data
	Tag        [16]byte // Authentication tag (included in ciphertext for GCM)
	TxHash     dag.Hash // Hash of encrypted tx for ordering
}

// TransactionBatch represents a batch of encrypted transactions.
type TransactionBatch struct {
	BatchID       dag.Hash                 // Unique batch identifier
	EncryptedTxs  []*EncryptedTransaction  // Encrypted transactions
	EncryptedKey  []byte                   // Threshold-encrypted symmetric key
	KeyShares     map[uint32][]byte        // Encrypted shares per validator
	MerkleRoot    dag.Hash                 // Root of encrypted tx hashes
	ValidatorSigs map[uint32][]byte        // Validator signatures on batch
	Decrypted     bool                     // Whether batch has been decrypted
}

// TransactionEncryptor handles transaction encryption/decryption.
type TransactionEncryptor struct {
	thresholdMgr *ThresholdKeyManager

	activeBatches map[dag.Hash]*TransactionBatch

	mu sync.RWMutex
}

// NewTransactionEncryptor creates a new transaction encryptor.
func NewTransactionEncryptor(thresholdMgr *ThresholdKeyManager) *TransactionEncryptor {
	return &TransactionEncryptor{
		thresholdMgr:  thresholdMgr,
		activeBatches: make(map[dag.Hash]*TransactionBatch),
	}
}

// EncryptTransaction encrypts a single transaction with the given key.
func (te *TransactionEncryptor) EncryptTransaction(tx []byte, key []byte) (*EncryptedTransaction, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Generate random nonce
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}

	// Encrypt (ciphertext includes auth tag)
	ciphertext := gcm.Seal(nil, nonce[:], tx, nil)

	// Compute hash of encrypted data for ordering
	txHash := crypto.Hash(ciphertext)

	return &EncryptedTransaction{
		Nonce:      nonce,
		Ciphertext: ciphertext,
		TxHash:     txHash,
	}, nil
}

// DecryptTransaction decrypts a single transaction.
func (te *TransactionEncryptor) DecryptTransaction(encTx *EncryptedTransaction, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plaintext, err := gcm.Open(nil, encTx.Nonce[:], encTx.Ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// CreateBatch creates a new encrypted transaction batch.
func (te *TransactionEncryptor) CreateBatch(txs [][]byte, validatorPubKeys map[uint32]dag.PublicKey) (*TransactionBatch, error) {
	te.mu.Lock()
	defer te.mu.Unlock()

	// Generate batch encryption key
	key, shares, err := te.thresholdMgr.GenerateEpochKey()
	if err != nil {
		return nil, err
	}

	// Encrypt all transactions
	encryptedTxs := make([]*EncryptedTransaction, len(txs))
	txHashes := make([][32]byte, len(txs))

	for i, tx := range txs {
		encTx, err := te.EncryptTransaction(tx, key)
		if err != nil {
			return nil, err
		}
		encryptedTxs[i] = encTx
		txHashes[i] = encTx.TxHash
	}

	// Compute Merkle root of encrypted tx hashes
	merkleRoot := crypto.MerkleRoot(txHashes)

	// Generate batch ID
	batchID := crypto.Hash(merkleRoot[:])

	// Encrypt shares for each validator
	// In production, this would use the validator's public key
	// For now, we'll store shares directly (simplified)
	keyShares := make(map[uint32][]byte)
	for _, share := range shares {
		// Serialize share: index (4 bytes) + value (32 bytes)
		shareData := make([]byte, 36)
		shareData[0] = byte(share.Index >> 24)
		shareData[1] = byte(share.Index >> 16)
		shareData[2] = byte(share.Index >> 8)
		shareData[3] = byte(share.Index)
		valueBytes := share.Value.Bytes()
		copy(shareData[36-len(valueBytes):], valueBytes)
		keyShares[share.Index] = shareData
	}

	batch := &TransactionBatch{
		BatchID:       batchID,
		EncryptedTxs:  encryptedTxs,
		KeyShares:     keyShares,
		MerkleRoot:    merkleRoot,
		ValidatorSigs: make(map[uint32][]byte),
	}

	te.activeBatches[batchID] = batch
	return batch, nil
}

// DecryptBatch decrypts all transactions in a batch.
func (te *TransactionEncryptor) DecryptBatch(batchID dag.Hash, key []byte) ([][]byte, error) {
	te.mu.Lock()
	defer te.mu.Unlock()

	batch, exists := te.activeBatches[batchID]
	if !exists {
		return nil, ErrBatchNotFound
	}

	if batch.Decrypted {
		return nil, ErrAlreadyDecrypted
	}

	txs := make([][]byte, len(batch.EncryptedTxs))
	for i, encTx := range batch.EncryptedTxs {
		tx, err := te.DecryptTransaction(encTx, key)
		if err != nil {
			return nil, err
		}
		txs[i] = tx
	}

	batch.Decrypted = true
	return txs, nil
}

// GetBatch returns a batch by ID.
func (te *TransactionEncryptor) GetBatch(batchID dag.Hash) *TransactionBatch {
	te.mu.RLock()
	defer te.mu.RUnlock()
	return te.activeBatches[batchID]
}

// GetBatchShare returns the encrypted share for a validator.
func (te *TransactionEncryptor) GetBatchShare(batchID dag.Hash, validatorIndex uint32) ([]byte, error) {
	te.mu.RLock()
	defer te.mu.RUnlock()

	batch, exists := te.activeBatches[batchID]
	if !exists {
		return nil, ErrBatchNotFound
	}

	share, exists := batch.KeyShares[validatorIndex]
	if !exists {
		return nil, ErrShareNotFound
	}

	return share, nil
}

// AddValidatorSignature adds a validator signature to a batch.
func (te *TransactionEncryptor) AddValidatorSignature(batchID dag.Hash, validatorIndex uint32, sig []byte) error {
	te.mu.Lock()
	defer te.mu.Unlock()

	batch, exists := te.activeBatches[batchID]
	if !exists {
		return ErrBatchNotFound
	}

	batch.ValidatorSigs[validatorIndex] = sig
	return nil
}

// GetSignatureCount returns the number of validator signatures.
func (te *TransactionEncryptor) GetSignatureCount(batchID dag.Hash) int {
	te.mu.RLock()
	defer te.mu.RUnlock()

	batch, exists := te.activeBatches[batchID]
	if !exists {
		return 0
	}
	return len(batch.ValidatorSigs)
}

// RemoveBatch removes a batch from active tracking.
func (te *TransactionEncryptor) RemoveBatch(batchID dag.Hash) {
	te.mu.Lock()
	defer te.mu.Unlock()
	delete(te.activeBatches, batchID)
}

// VerifyEncryptedTxRoot verifies the Merkle root of encrypted transactions.
func (te *TransactionEncryptor) VerifyEncryptedTxRoot(encTxs []*EncryptedTransaction, root dag.Hash) bool {
	if len(encTxs) == 0 {
		return root == dag.Hash{}
	}

	txHashes := make([][32]byte, len(encTxs))
	for i, encTx := range encTxs {
		txHashes[i] = encTx.TxHash
	}

	computed := crypto.MerkleRoot(txHashes)
	return computed == root
}

// Error types
var (
	ErrInvalidKeyLength  = errors.New("invalid key length (must be 32 bytes)")
	ErrDecryptionFailed  = errors.New("decryption failed (authentication error)")
	ErrBatchNotFound     = errors.New("batch not found")
	ErrShareNotFound     = errors.New("share not found for validator")
	ErrAlreadyDecrypted  = errors.New("batch already decrypted")
)
