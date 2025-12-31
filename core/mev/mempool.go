// Package mev implements MEV resistance for NovaCoin.
package mev

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// MempoolEntry represents an encrypted transaction in the mempool.
type MempoolEntry struct {
	EncryptedTx *EncryptedTransaction
	ReceivedAt  time.Time
	From        dag.Address // Sender (known from signature, not content)
	GasPrice    uint64      // Committed gas price (encrypted but signed)
	Nonce       uint64      // Sender nonce (for ordering same-sender txs)
	BatchID     dag.Hash    // Batch this tx belongs to (if any)
}

// MempoolConfig holds mempool configuration.
type MempoolConfig struct {
	MaxSize           int           // Maximum entries in mempool
	MaxBatchSize      int           // Maximum transactions per batch
	BatchTimeout      time.Duration // Time to wait before forming batch
	EntryTTL          time.Duration // Time-to-live for entries
	CleanupInterval   time.Duration // Interval for cleanup
}

// DefaultMempoolConfig returns default configuration.
func DefaultMempoolConfig() *MempoolConfig {
	return &MempoolConfig{
		MaxSize:         10000,
		MaxBatchSize:    500,
		BatchTimeout:    500 * time.Millisecond,
		EntryTTL:        5 * time.Minute,
		CleanupInterval: 30 * time.Second,
	}
}

// EncryptedMempool manages encrypted transactions awaiting inclusion.
type EncryptedMempool struct {
	config *MempoolConfig

	entries    map[dag.Hash]*MempoolEntry // txHash -> entry
	byAddress  map[dag.Address][]*MempoolEntry
	pending    []*MempoolEntry            // Ordered for batch creation
	batched    map[dag.Hash]bool          // Transactions already in a batch

	stats MempoolStats

	stopCleanup chan struct{}
	mu          sync.RWMutex
}

// MempoolStats tracks mempool statistics.
type MempoolStats struct {
	TotalReceived   uint64
	TotalBatched    uint64
	TotalExpired    uint64
	TotalDropped    uint64
	CurrentSize     int
	PendingBatches  int
	AvgBatchSize    float64
}

// NewEncryptedMempool creates a new encrypted mempool.
func NewEncryptedMempool(config *MempoolConfig) *EncryptedMempool {
	if config == nil {
		config = DefaultMempoolConfig()
	}
	mp := &EncryptedMempool{
		config:      config,
		entries:     make(map[dag.Hash]*MempoolEntry),
		byAddress:   make(map[dag.Address][]*MempoolEntry),
		pending:     make([]*MempoolEntry, 0),
		batched:     make(map[dag.Hash]bool),
		stopCleanup: make(chan struct{}),
	}

	go mp.cleanupLoop()
	return mp
}

// Add adds an encrypted transaction to the mempool.
func (mp *EncryptedMempool) Add(encTx *EncryptedTransaction, from dag.Address, gasPrice, nonce uint64) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	txHash := encTx.TxHash

	// Check if already exists
	if _, exists := mp.entries[txHash]; exists {
		return ErrTxAlreadyExists
	}

	// Check size limit
	if len(mp.entries) >= mp.config.MaxSize {
		// Try to evict lowest gas price tx
		if !mp.evictLowestGasPrice(gasPrice) {
			mp.stats.TotalDropped++
			return ErrMempoolFull
		}
	}

	entry := &MempoolEntry{
		EncryptedTx: encTx,
		ReceivedAt:  time.Now(),
		From:        from,
		GasPrice:    gasPrice,
		Nonce:       nonce,
	}

	mp.entries[txHash] = entry
	mp.byAddress[from] = append(mp.byAddress[from], entry)
	mp.pending = append(mp.pending, entry)

	mp.stats.TotalReceived++
	mp.stats.CurrentSize = len(mp.entries)

	return nil
}

// evictLowestGasPrice removes the lowest gas price entry if it's lower than minGasPrice.
func (mp *EncryptedMempool) evictLowestGasPrice(minGasPrice uint64) bool {
	if len(mp.pending) == 0 {
		return false
	}

	// Find lowest gas price entry that's not batched
	var lowestEntry *MempoolEntry
	var lowestIdx int
	for i, entry := range mp.pending {
		if mp.batched[entry.EncryptedTx.TxHash] {
			continue
		}
		if lowestEntry == nil || entry.GasPrice < lowestEntry.GasPrice {
			lowestEntry = entry
			lowestIdx = i
		}
	}

	if lowestEntry == nil || lowestEntry.GasPrice >= minGasPrice {
		return false
	}

	// Remove from pending
	mp.pending = append(mp.pending[:lowestIdx], mp.pending[lowestIdx+1:]...)

	// Remove from entries
	delete(mp.entries, lowestEntry.EncryptedTx.TxHash)

	// Remove from byAddress
	mp.removeFromAddressIndex(lowestEntry)

	mp.stats.TotalDropped++
	mp.stats.CurrentSize = len(mp.entries)

	return true
}

func (mp *EncryptedMempool) removeFromAddressIndex(entry *MempoolEntry) {
	entries := mp.byAddress[entry.From]
	for i, e := range entries {
		if e.EncryptedTx.TxHash == entry.EncryptedTx.TxHash {
			mp.byAddress[entry.From] = append(entries[:i], entries[i+1:]...)
			break
		}
	}
	if len(mp.byAddress[entry.From]) == 0 {
		delete(mp.byAddress, entry.From)
	}
}

// Remove removes a transaction from the mempool.
func (mp *EncryptedMempool) Remove(txHash dag.Hash) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	entry, exists := mp.entries[txHash]
	if !exists {
		return
	}

	delete(mp.entries, txHash)
	delete(mp.batched, txHash)
	mp.removeFromAddressIndex(entry)

	// Remove from pending
	for i, e := range mp.pending {
		if e.EncryptedTx.TxHash == txHash {
			mp.pending = append(mp.pending[:i], mp.pending[i+1:]...)
			break
		}
	}

	mp.stats.CurrentSize = len(mp.entries)
}

// CreateBatch creates a batch of transactions for inclusion.
func (mp *EncryptedMempool) CreateBatch() ([]*EncryptedTransaction, []dag.Hash) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Sort pending by gas price (descending)
	sort.Slice(mp.pending, func(i, j int) bool {
		return mp.pending[i].GasPrice > mp.pending[j].GasPrice
	})

	batch := make([]*EncryptedTransaction, 0, mp.config.MaxBatchSize)
	hashes := make([]dag.Hash, 0, mp.config.MaxBatchSize)

	for _, entry := range mp.pending {
		if mp.batched[entry.EncryptedTx.TxHash] {
			continue
		}
		if len(batch) >= mp.config.MaxBatchSize {
			break
		}

		batch = append(batch, entry.EncryptedTx)
		hashes = append(hashes, entry.EncryptedTx.TxHash)
		mp.batched[entry.EncryptedTx.TxHash] = true
	}

	mp.stats.TotalBatched += uint64(len(batch))
	if len(batch) > 0 {
		mp.stats.PendingBatches++
		// Update average batch size
		mp.stats.AvgBatchSize = (mp.stats.AvgBatchSize*float64(mp.stats.PendingBatches-1) + float64(len(batch))) / float64(mp.stats.PendingBatches)
	}

	return batch, hashes
}

// MarkBatchComplete marks transactions in a batch as complete and removes them.
func (mp *EncryptedMempool) MarkBatchComplete(txHashes []dag.Hash) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	for _, hash := range txHashes {
		entry, exists := mp.entries[hash]
		if !exists {
			continue
		}

		delete(mp.entries, hash)
		delete(mp.batched, hash)
		mp.removeFromAddressIndex(entry)

		// Remove from pending
		for i, e := range mp.pending {
			if e.EncryptedTx.TxHash == hash {
				mp.pending = append(mp.pending[:i], mp.pending[i+1:]...)
				break
			}
		}
	}

	mp.stats.CurrentSize = len(mp.entries)
}

// MarkBatchFailed marks a batch as failed, allowing transactions to be re-batched.
func (mp *EncryptedMempool) MarkBatchFailed(txHashes []dag.Hash) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	for _, hash := range txHashes {
		delete(mp.batched, hash)
	}
}

// Get returns an entry by transaction hash.
func (mp *EncryptedMempool) Get(txHash dag.Hash) *MempoolEntry {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.entries[txHash]
}

// GetByAddress returns all entries from an address.
func (mp *EncryptedMempool) GetByAddress(from dag.Address) []*MempoolEntry {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	entries := mp.byAddress[from]
	result := make([]*MempoolEntry, len(entries))
	copy(result, entries)
	return result
}

// Size returns the current mempool size.
func (mp *EncryptedMempool) Size() int {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return len(mp.entries)
}

// PendingCount returns the number of pending (not batched) transactions.
func (mp *EncryptedMempool) PendingCount() int {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	count := 0
	for _, entry := range mp.pending {
		if !mp.batched[entry.EncryptedTx.TxHash] {
			count++
		}
	}
	return count
}

// GetStats returns mempool statistics.
func (mp *EncryptedMempool) GetStats() MempoolStats {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.stats
}

// cleanupLoop periodically removes expired entries.
func (mp *EncryptedMempool) cleanupLoop() {
	ticker := time.NewTicker(mp.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mp.cleanup()
		case <-mp.stopCleanup:
			return
		}
	}
}

func (mp *EncryptedMempool) cleanup() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	now := time.Now()
	var toRemove []dag.Hash

	for hash, entry := range mp.entries {
		if now.Sub(entry.ReceivedAt) > mp.config.EntryTTL {
			toRemove = append(toRemove, hash)
		}
	}

	for _, hash := range toRemove {
		entry := mp.entries[hash]
		delete(mp.entries, hash)
		delete(mp.batched, hash)
		mp.removeFromAddressIndex(entry)

		// Remove from pending
		for i, e := range mp.pending {
			if e.EncryptedTx.TxHash == hash {
				mp.pending = append(mp.pending[:i], mp.pending[i+1:]...)
				break
			}
		}
	}

	mp.stats.TotalExpired += uint64(len(toRemove))
	mp.stats.CurrentSize = len(mp.entries)
}

// Stop stops the cleanup goroutine.
func (mp *EncryptedMempool) Stop() {
	close(mp.stopCleanup)
}

// Clear clears all entries from the mempool.
func (mp *EncryptedMempool) Clear() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	mp.entries = make(map[dag.Hash]*MempoolEntry)
	mp.byAddress = make(map[dag.Address][]*MempoolEntry)
	mp.pending = make([]*MempoolEntry, 0)
	mp.batched = make(map[dag.Hash]bool)
	mp.stats.CurrentSize = 0
}

// Error types
var (
	ErrTxAlreadyExists = errors.New("transaction already in mempool")
	ErrMempoolFull     = errors.New("mempool is full")
	ErrTxNotFound      = errors.New("transaction not found in mempool")
)
