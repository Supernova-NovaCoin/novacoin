// Package mev implements MEV resistance for NovaCoin.
package mev

import (
	"errors"
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
	"github.com/Supernova-NovaCoin/novacoin/crypto"
)

// EngineState represents the state of the MEV engine.
type EngineState int

const (
	EngineIdle EngineState = iota
	EngineCollecting      // Collecting encrypted transactions
	EngineOrdering        // Running fair ordering protocol
	EngineDecrypting      // Decrypting transactions
	EngineReady           // Batch ready for consensus
)

func (s EngineState) String() string {
	switch s {
	case EngineIdle:
		return "idle"
	case EngineCollecting:
		return "collecting"
	case EngineOrdering:
		return "ordering"
	case EngineDecrypting:
		return "decrypting"
	case EngineReady:
		return "ready"
	default:
		return "unknown"
	}
}

// BlockProposal represents a block ready for consensus.
type BlockProposal struct {
	EncryptedRoot dag.Hash                // Root of encrypted transactions
	DecryptedTxs  [][]byte                // Decrypted and ordered transactions
	OrderProof    *OrderingRound          // Proof of fair ordering
	DecryptProof  []byte                  // Threshold decryption proof
	Timestamp     time.Time
}

// MEVEngineConfig holds engine configuration.
type MEVEngineConfig struct {
	Threshold         *ThresholdConfig
	Ordering          *OrderingConfig
	Mempool           *MempoolConfig
	BatchInterval     time.Duration // Interval for batch creation
	DecryptionTimeout time.Duration // Timeout for decryption
}

// DefaultMEVEngineConfig returns default configuration.
func DefaultMEVEngineConfig() *MEVEngineConfig {
	return &MEVEngineConfig{
		Threshold:         DefaultThresholdConfig(),
		Ordering:          DefaultOrderingConfig(),
		Mempool:           DefaultMempoolConfig(),
		BatchInterval:     500 * time.Millisecond,
		DecryptionTimeout: 5 * time.Second,
	}
}

// MEVEngine orchestrates MEV resistance mechanisms.
type MEVEngine struct {
	config *MEVEngineConfig

	thresholdMgr *ThresholdKeyManager
	encryptor    *TransactionEncryptor
	orderer      *FairOrderer
	mempool      *EncryptedMempool

	state         EngineState
	currentBatch  *TransactionBatch
	currentKey    []byte
	currentShares []*Share

	pendingProposals map[dag.Hash]*BlockProposal

	// Callbacks
	onProposalReady func(*BlockProposal)

	stats MEVEngineStats

	running bool
	stopCh  chan struct{}
	mu      sync.RWMutex
}

// MEVEngineStats tracks engine statistics.
type MEVEngineStats struct {
	BatchesCreated    uint64
	BatchesOrdered    uint64
	BatchesDecrypted  uint64
	TotalTransactions uint64
	AverageLatency    time.Duration
	DecryptionRate    float64 // Successful decryptions / attempts
}

// NewMEVEngine creates a new MEV engine.
func NewMEVEngine(config *MEVEngineConfig) *MEVEngine {
	if config == nil {
		config = DefaultMEVEngineConfig()
	}

	thresholdMgr := NewThresholdKeyManager(config.Threshold)
	encryptor := NewTransactionEncryptor(thresholdMgr)
	orderer := NewFairOrderer(config.Ordering)
	mempool := NewEncryptedMempool(config.Mempool)

	return &MEVEngine{
		config:           config,
		thresholdMgr:     thresholdMgr,
		encryptor:        encryptor,
		orderer:          orderer,
		mempool:          mempool,
		state:            EngineIdle,
		pendingProposals: make(map[dag.Hash]*BlockProposal),
		stopCh:           make(chan struct{}),
	}
}

// Start starts the MEV engine.
func (e *MEVEngine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return ErrEngineAlreadyRunning
	}

	e.running = true
	e.state = EngineCollecting
	e.stopCh = make(chan struct{})

	go e.batchLoop()

	return nil
}

// Stop stops the MEV engine.
func (e *MEVEngine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return ErrEngineNotRunning
	}

	e.running = false
	close(e.stopCh)
	e.mempool.Stop()
	e.state = EngineIdle

	return nil
}

// batchLoop runs the batch creation loop.
func (e *MEVEngine) batchLoop() {
	ticker := time.NewTicker(e.config.BatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.tryCreateBatch()
		case <-e.stopCh:
			return
		}
	}
}

// tryCreateBatch attempts to create a new batch if enough transactions are pending.
func (e *MEVEngine) tryCreateBatch() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state != EngineCollecting {
		return
	}

	if e.mempool.PendingCount() == 0 {
		return
	}

	// Create batch from mempool
	encryptedTxs, txHashes := e.mempool.CreateBatch()
	if len(encryptedTxs) == 0 {
		return
	}

	// Generate encryption key and shares for this batch
	key, shares, err := e.thresholdMgr.GenerateEpochKey()
	if err != nil {
		return
	}

	e.currentKey = key
	e.currentShares = shares

	// Create transaction batch
	batch := &TransactionBatch{
		BatchID:      crypto.Hash([]byte(time.Now().String())),
		EncryptedTxs: encryptedTxs,
		MerkleRoot:   computeMerkleRoot(encryptedTxs),
	}
	e.currentBatch = batch
	e.encryptor.activeBatches[batch.BatchID] = batch

	e.state = EngineOrdering
	e.stats.BatchesCreated++

	// Start fair ordering round
	e.orderer.StartRound(txHashes)
}

// computeMerkleRoot computes the Merkle root of encrypted transactions.
func computeMerkleRoot(txs []*EncryptedTransaction) dag.Hash {
	if len(txs) == 0 {
		return dag.Hash{}
	}

	hashes := make([][32]byte, len(txs))
	for i, tx := range txs {
		hashes[i] = tx.TxHash
	}
	return crypto.MerkleRoot(hashes)
}

// SubmitEncryptedTransaction submits an encrypted transaction.
func (e *MEVEngine) SubmitEncryptedTransaction(encTx *EncryptedTransaction, from dag.Address, gasPrice, nonce uint64) error {
	return e.mempool.Add(encTx, from, gasPrice, nonce)
}

// EncryptAndSubmitTransaction encrypts and submits a transaction.
func (e *MEVEngine) EncryptAndSubmitTransaction(tx []byte, from dag.Address, gasPrice, nonce uint64) (*EncryptedTransaction, error) {
	e.mu.RLock()
	key := e.currentKey
	e.mu.RUnlock()

	if key == nil {
		// Generate a temporary key for this transaction
		// In production, this would use the current epoch key
		hash := crypto.Hash(tx)
		key = hash[:]
	}

	encTx, err := e.encryptor.EncryptTransaction(tx, key)
	if err != nil {
		return nil, err
	}

	if err := e.mempool.Add(encTx, from, gasPrice, nonce); err != nil {
		return nil, err
	}

	return encTx, nil
}

// SubmitOrderingCommit submits a commit for fair ordering.
func (e *MEVEngine) SubmitOrderingCommit(validatorIndex uint32, commitment dag.Hash) error {
	e.mu.RLock()
	state := e.state
	e.mu.RUnlock()

	if state != EngineOrdering {
		return ErrNotInOrderingPhase
	}

	return e.orderer.SubmitCommit(validatorIndex, commitment)
}

// SubmitOrderingReveal submits a reveal for fair ordering.
func (e *MEVEngine) SubmitOrderingReveal(validatorIndex uint32, seed [32]byte) error {
	e.mu.RLock()
	state := e.state
	e.mu.RUnlock()

	if state != EngineOrdering {
		return ErrNotInOrderingPhase
	}

	return e.orderer.SubmitReveal(validatorIndex, seed)
}

// FinalizeOrdering finalizes the ordering and transitions to decryption.
func (e *MEVEngine) FinalizeOrdering() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state != EngineOrdering {
		return ErrNotInOrderingPhase
	}

	// Transition ordering to reveal phase if needed
	if e.orderer.GetCurrentRound().State == OrderingCommitting {
		if err := e.orderer.TransitionToReveal(); err != nil {
			return err
		}
	}

	// Finalize ordering
	_, err := e.orderer.FinalizeOrdering()
	if err != nil {
		return err
	}

	e.state = EngineDecrypting
	e.stats.BatchesOrdered++

	// Start decryption session
	if e.currentBatch != nil {
		e.thresholdMgr.StartDecryption(e.currentBatch.BatchID)
	}

	return nil
}

// SubmitDecryptionShare submits a share for threshold decryption.
func (e *MEVEngine) SubmitDecryptionShare(validatorIndex uint32, share *Share) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state != EngineDecrypting {
		return false, ErrNotInDecryptingPhase
	}

	if e.currentBatch == nil {
		return false, ErrNoBatch
	}

	complete, key, err := e.thresholdMgr.SubmitDecryptionShare(e.currentBatch.BatchID, share)
	if err != nil {
		return false, err
	}

	if complete {
		// Decrypt all transactions
		return e.completeDecryption(key)
	}

	return false, nil
}

// completeDecryption completes the decryption process.
func (e *MEVEngine) completeDecryption(key []byte) (bool, error) {
	if e.currentBatch == nil {
		return false, ErrNoBatch
	}

	// Get ordering
	round := e.orderer.GetCurrentRound()
	if round == nil || round.State != OrderingComplete {
		return false, ErrOrderingNotComplete
	}

	// Decrypt transactions in order
	orderedTxs := e.orderer.ApplyOrdering(round.Transactions, round.FinalOrder)
	decryptedTxs := make([][]byte, 0, len(orderedTxs))

	for _, txHash := range orderedTxs {
		// Find encrypted tx by hash
		var encTx *EncryptedTransaction
		for _, tx := range e.currentBatch.EncryptedTxs {
			if tx.TxHash == txHash {
				encTx = tx
				break
			}
		}
		if encTx == nil {
			continue
		}

		// Decrypt
		plaintext, err := e.encryptor.DecryptTransaction(encTx, key)
		if err != nil {
			// Skip invalid transactions
			continue
		}
		decryptedTxs = append(decryptedTxs, plaintext)
	}

	// Create block proposal
	proposal := &BlockProposal{
		EncryptedRoot: e.currentBatch.MerkleRoot,
		DecryptedTxs:  decryptedTxs,
		OrderProof:    round,
		Timestamp:     time.Now(),
	}

	e.pendingProposals[e.currentBatch.BatchID] = proposal
	e.state = EngineReady

	e.stats.BatchesDecrypted++
	e.stats.TotalTransactions += uint64(len(decryptedTxs))

	// Notify callback
	if e.onProposalReady != nil {
		e.onProposalReady(proposal)
	}

	// Mark batch complete in mempool
	txHashes := make([]dag.Hash, len(round.Transactions))
	copy(txHashes, round.Transactions)
	e.mempool.MarkBatchComplete(txHashes)

	return true, nil
}

// GetProposal returns a pending block proposal.
func (e *MEVEngine) GetProposal(batchID dag.Hash) *BlockProposal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.pendingProposals[batchID]
}

// ConsumeProposal consumes a proposal after block creation.
func (e *MEVEngine) ConsumeProposal(batchID dag.Hash) *BlockProposal {
	e.mu.Lock()
	defer e.mu.Unlock()

	proposal := e.pendingProposals[batchID]
	delete(e.pendingProposals, batchID)

	// Reset to collecting state
	if e.state == EngineReady {
		e.state = EngineCollecting
		e.currentBatch = nil
		e.currentKey = nil
	}

	return proposal
}

// SetProposalCallback sets the callback for when a proposal is ready.
func (e *MEVEngine) SetProposalCallback(callback func(*BlockProposal)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onProposalReady = callback
}

// GetState returns the current engine state.
func (e *MEVEngine) GetState() EngineState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

// GetStats returns engine statistics.
func (e *MEVEngine) GetStats() MEVEngineStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.stats
}

// GetMempool returns the mempool for direct access.
func (e *MEVEngine) GetMempool() *EncryptedMempool {
	return e.mempool
}

// GetThresholdManager returns the threshold key manager.
func (e *MEVEngine) GetThresholdManager() *ThresholdKeyManager {
	return e.thresholdMgr
}

// GetOrderer returns the fair orderer.
func (e *MEVEngine) GetOrderer() *FairOrderer {
	return e.orderer
}

// GetEncryptor returns the transaction encryptor.
func (e *MEVEngine) GetEncryptor() *TransactionEncryptor {
	return e.encryptor
}

// GetShare returns the current epoch share for a validator.
func (e *MEVEngine) GetShare(validatorIndex uint32) *Share {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.currentShares == nil || int(validatorIndex) > len(e.currentShares) {
		return nil
	}
	// Share indices are 1-based
	for _, share := range e.currentShares {
		if share.Index == validatorIndex {
			return share
		}
	}
	return nil
}

// GenerateCommitment generates a commit for a validator.
func (e *MEVEngine) GenerateCommitment(validatorIndex uint32) ([32]byte, dag.Hash, error) {
	seed, err := e.orderer.GenerateSeed()
	if err != nil {
		return [32]byte{}, dag.Hash{}, err
	}
	commitment := e.orderer.CreateCommitment(seed, validatorIndex)
	return seed, commitment, nil
}

// Error types
var (
	ErrEngineAlreadyRunning  = errors.New("engine already running")
	ErrEngineNotRunning      = errors.New("engine not running")
	ErrNotInOrderingPhase    = errors.New("not in ordering phase")
	ErrNotInDecryptingPhase  = errors.New("not in decrypting phase")
	ErrNoBatch               = errors.New("no active batch")
	ErrOrderingNotComplete   = errors.New("ordering not complete")
)
