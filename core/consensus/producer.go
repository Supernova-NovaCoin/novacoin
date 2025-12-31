// Package consensus implements the hybrid consensus orchestrator for NovaCoin.
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// BlockProducer handles block creation.
type BlockProducer struct {
	consensus *HybridConsensus

	// Block production state
	lastProducedTime time.Time
	lastProducedHash dag.Hash
	blockCount       uint64

	// Transaction queue
	pendingTxs [][]byte

	mu sync.Mutex
}

// NewBlockProducer creates a new block producer.
func NewBlockProducer(consensus *HybridConsensus) *BlockProducer {
	return &BlockProducer{
		consensus:  consensus,
		pendingTxs: make([][]byte, 0, 1000),
	}
}

// ProduceBlock creates a new block with the given transactions.
func (bp *BlockProducer) ProduceBlock(
	validatorIndex uint32,
	pubKey dag.PublicKey,
	stake uint64,
	transactions [][]byte,
) (*dag.Vertex, error) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	// Get parent blocks (DAG tips)
	parents := bp.selectParents()
	if len(parents) == 0 {
		return nil, errors.New("no parent blocks available")
	}

	// Create vertex
	vertex := dag.NewVertex(parents, pubKey, validatorIndex, stake)

	// Set consensus parameters
	vertex.AdaptiveK = float32(bp.consensus.dagKnight.GetCurrentK())
	vertex.Wave = bp.consensus.shoal.GetCurrentWaveNumber()
	vertex.Round = bp.consensus.mysticeti.GetCurrentRoundNumber()

	// Set transactions
	vertex.Transactions = bp.selectTransactions(transactions)
	vertex.TxCount = uint32(len(vertex.Transactions))

	// Calculate transaction root
	vertex.TxRoot = bp.computeTxRoot(vertex.Transactions)

	// Compute block hash
	vertex.Hash = bp.computeBlockHash(vertex)

	// Compute height
	vertex.Height = bp.computeHeight(parents)

	// Set implicit votes (parents serve as votes in Mysticeti)
	vertex.ImplicitVotes = parents

	// All validators are anchors in Shoal++
	vertex.IsAnchor = true
	vertex.AnchorWave = vertex.Wave

	// Assign parallel DAG
	vertex.ParallelDAGId = bp.assignParallelDAG(validatorIndex)

	// Store in DAG
	if err := bp.consensus.dagStore.Add(vertex); err != nil {
		return nil, err
	}

	// Update state
	bp.lastProducedTime = time.Now()
	bp.lastProducedHash = vertex.Hash
	bp.blockCount++

	return vertex, nil
}

// selectParents selects parent blocks for the new block.
func (bp *BlockProducer) selectParents() []dag.Hash {
	tips := bp.consensus.dagStore.GetTipHashes()

	// Limit parents to reasonable number
	maxParents := 8
	if len(tips) > maxParents {
		tips = tips[:maxParents]
	}

	return tips
}

// selectTransactions selects transactions for the block.
func (bp *BlockProducer) selectTransactions(provided [][]byte) [][]byte {
	// Combine provided transactions with pending
	all := append(provided, bp.pendingTxs...)

	// Limit by count
	maxTx := bp.consensus.config.MaxTxPerBlock
	if len(all) > maxTx {
		// Keep remaining as pending
		bp.pendingTxs = all[maxTx:]
		all = all[:maxTx]
	} else {
		bp.pendingTxs = nil
	}

	// Limit by size
	totalSize := 0
	maxSize := bp.consensus.config.MaxBlockSize
	result := make([][]byte, 0, len(all))

	for _, tx := range all {
		if totalSize+len(tx) > maxSize {
			break
		}
		result = append(result, tx)
		totalSize += len(tx)
	}

	return result
}

// computeTxRoot computes the Merkle root of transactions.
func (bp *BlockProducer) computeTxRoot(txs [][]byte) dag.Hash {
	if len(txs) == 0 {
		return dag.EmptyHash()
	}

	// Simple Merkle tree implementation
	leaves := make([]dag.Hash, len(txs))
	for i, tx := range txs {
		h := sha256.Sum256(tx)
		copy(leaves[i][:], h[:])
	}

	for len(leaves) > 1 {
		var nextLevel []dag.Hash
		for i := 0; i < len(leaves); i += 2 {
			if i+1 < len(leaves) {
				combined := append(leaves[i][:], leaves[i+1][:]...)
				h := sha256.Sum256(combined)
				var hash dag.Hash
				copy(hash[:], h[:])
				nextLevel = append(nextLevel, hash)
			} else {
				nextLevel = append(nextLevel, leaves[i])
			}
		}
		leaves = nextLevel
	}

	return leaves[0]
}

// computeBlockHash computes the block hash.
func (bp *BlockProducer) computeBlockHash(vertex *dag.Vertex) dag.Hash {
	h := sha256.New()

	// Hash header fields
	for _, parent := range vertex.Parents {
		h.Write(parent[:])
	}

	// Timestamp
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(vertex.Timestamp.UnixMilli()))
	h.Write(ts)

	// Validator
	h.Write(vertex.ValidatorPubKey[:])

	// Transaction root
	h.Write(vertex.TxRoot[:])

	// State root
	h.Write(vertex.StateRoot[:])

	// Wave and round
	wave := make([]byte, 8)
	binary.BigEndian.PutUint64(wave, vertex.Wave)
	h.Write(wave)

	round := make([]byte, 8)
	binary.BigEndian.PutUint64(round, vertex.Round)
	h.Write(round)

	// Compute hash
	sum := h.Sum(nil)
	var hash dag.Hash
	copy(hash[:], sum)

	return hash
}

// computeHeight computes the block height.
func (bp *BlockProducer) computeHeight(parents []dag.Hash) uint64 {
	maxHeight := uint64(0)

	for _, parentHash := range parents {
		parent := bp.consensus.dagStore.Get(parentHash)
		if parent != nil && parent.Height >= maxHeight {
			maxHeight = parent.Height + 1
		}
	}

	return maxHeight
}

// assignParallelDAG assigns the block to a parallel DAG.
func (bp *BlockProducer) assignParallelDAG(validatorIndex uint32) uint8 {
	// Simple round-robin based on validator index
	numDAGs := uint8(4) // Default 4 parallel DAGs
	return uint8(validatorIndex % uint32(numDAGs))
}

// AddPendingTransaction adds a transaction to the pending queue.
func (bp *BlockProducer) AddPendingTransaction(tx []byte) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.pendingTxs = append(bp.pendingTxs, tx)
}

// GetPendingCount returns the number of pending transactions.
func (bp *BlockProducer) GetPendingCount() int {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return len(bp.pendingTxs)
}

// GetBlockCount returns the number of blocks produced.
func (bp *BlockProducer) GetBlockCount() uint64 {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return bp.blockCount
}

// GetLastProducedTime returns the time of the last produced block.
func (bp *BlockProducer) GetLastProducedTime() time.Time {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return bp.lastProducedTime
}

// ClearPending clears all pending transactions.
func (bp *BlockProducer) ClearPending() {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.pendingTxs = nil
}
