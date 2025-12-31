// Package finality implements the finality tracking and proof system for NovaCoin.
//
// The finality engine provides:
//   - Finality status tracking for blocks
//   - Finality proof generation from Mysticeti commit certificates
//   - Chain reorganization detection and handling
//   - Finality frontier management for efficient queries
//
// A block becomes finalized when:
//  1. It receives a valid commit certificate from Mysticeti (2f+1 stake support)
//  2. All ancestor blocks are also finalized
//  3. No conflicting blocks at the same height are finalized
package finality

import (
	"errors"
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
	"github.com/Supernova-NovaCoin/novacoin/core/mysticeti"
)

// Common errors.
var (
	ErrNotFinalized       = errors.New("block not finalized")
	ErrAlreadyFinalized   = errors.New("block already finalized")
	ErrConflictingBlock   = errors.New("conflicting block at same height")
	ErrMissingAncestor    = errors.New("ancestor block not finalized")
	ErrInvalidProof       = errors.New("invalid finality proof")
	ErrReorgDetected      = errors.New("chain reorganization detected")
	ErrFinalityViolation  = errors.New("finality invariant violated")
)

// Config holds finality engine configuration.
type Config struct {
	// FinalityDepth is the minimum commit depth for finality (default: 3)
	FinalityDepth int

	// QuorumThreshold is the stake fraction for quorum (default: 2/3)
	QuorumThreshold float64

	// ConfirmationBlocks is additional blocks after commit for finality
	ConfirmationBlocks int

	// PruneThreshold is the number of finalized blocks to keep in memory
	PruneThreshold int

	// EnableReorgProtection enables protection against deep reorgs
	EnableReorgProtection bool

	// MaxReorgDepth is the maximum allowed reorg depth (0 = no limit)
	MaxReorgDepth int
}

// DefaultConfig returns the default finality configuration.
func DefaultConfig() *Config {
	return &Config{
		FinalityDepth:         3,
		QuorumThreshold:       2.0 / 3.0,
		ConfirmationBlocks:    0,
		PruneThreshold:        10000,
		EnableReorgProtection: true,
		MaxReorgDepth:         100,
	}
}

// FinalityStatus represents the finality state of a block.
type FinalityStatus int

const (
	// StatusPending means the block is not yet finalized.
	StatusPending FinalityStatus = iota
	// StatusCommitted means the block has a commit certificate.
	StatusCommitted
	// StatusFinalized means the block is fully finalized.
	StatusFinalized
	// StatusOrphaned means the block was orphaned by a reorg.
	StatusOrphaned
)

func (s FinalityStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusCommitted:
		return "committed"
	case StatusFinalized:
		return "finalized"
	case StatusOrphaned:
		return "orphaned"
	default:
		return "unknown"
	}
}

// FinalizedBlock contains a finalized block with its proof.
type FinalizedBlock struct {
	Hash         dag.Hash
	Height       uint64
	Status       FinalityStatus
	FinalizedAt  time.Time
	Proof        *FinalityProof
	Certificate  *mysticeti.CommitCertificate
	Confirmations int
}

// Engine is the finality tracking engine.
type Engine struct {
	config *Config

	// Block storage interface
	dagStore VertexStore

	// Finalized blocks by hash
	finalized map[dag.Hash]*FinalizedBlock

	// Finalized blocks by height (for conflict detection)
	byHeight map[uint64][]dag.Hash

	// Finality frontier (highest finalized blocks per validator)
	frontier *Frontier

	// Pending blocks with commit certificates waiting for ancestor finality
	pendingCommits map[dag.Hash]*mysticeti.CommitCertificate

	// Reorg handler
	reorgHandler *ReorgHandler

	// Statistics
	stats *Statistics

	// Callbacks
	onFinalized func(*FinalizedBlock)
	onReorg     func(*ReorgEvent)

	mu sync.RWMutex
}

// VertexStore interface for accessing blocks.
type VertexStore interface {
	Get(hash dag.Hash) *dag.Vertex
	GetByHeight(height uint64) []*dag.Vertex
	GetChildren(hash dag.Hash) []*dag.Vertex
}

// Statistics holds finality engine statistics.
type Statistics struct {
	TotalFinalized     uint64
	TotalCommitted     uint64
	TotalOrphaned      uint64
	AverageFinalityTime time.Duration
	LastFinalizedHeight uint64
	LastFinalizedTime   time.Time
	ReorgsDetected     uint64
	PendingCommits     int

	mu sync.RWMutex
}

// NewEngine creates a new finality engine.
func NewEngine(config *Config, dagStore VertexStore) *Engine {
	if config == nil {
		config = DefaultConfig()
	}

	e := &Engine{
		config:         config,
		dagStore:       dagStore,
		finalized:      make(map[dag.Hash]*FinalizedBlock),
		byHeight:       make(map[uint64][]dag.Hash),
		frontier:       NewFrontier(),
		pendingCommits: make(map[dag.Hash]*mysticeti.CommitCertificate),
		stats:          &Statistics{},
	}

	// Initialize reorg handler
	e.reorgHandler = NewReorgHandler(config, e)

	return e
}

// ProcessCommitCertificate processes a commit certificate from Mysticeti.
// Returns the finalized block if all conditions are met.
func (e *Engine) ProcessCommitCertificate(cert *mysticeti.CommitCertificate) (*FinalizedBlock, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if already finalized
	if _, exists := e.finalized[cert.Block]; exists {
		return nil, ErrAlreadyFinalized
	}

	// Get the block
	vertex := e.dagStore.Get(cert.Block)
	if vertex == nil {
		// Block not found, queue for later
		e.pendingCommits[cert.Block] = cert
		return nil, nil
	}

	// Check if all ancestors are finalized
	if !e.allAncestorsFinalized(vertex) {
		// Queue for later processing
		e.pendingCommits[cert.Block] = cert
		e.stats.mu.Lock()
		e.stats.PendingCommits = len(e.pendingCommits)
		e.stats.mu.Unlock()
		return nil, nil
	}

	// Check for conflicts (same height already finalized)
	if err := e.checkConflicts(vertex); err != nil {
		return nil, err
	}

	// Create finality proof
	proof := e.createFinalityProof(vertex, cert)

	// Create finalized block
	finBlock := &FinalizedBlock{
		Hash:         cert.Block,
		Height:       vertex.Height,
		Status:       StatusFinalized,
		FinalizedAt:  time.Now(),
		Proof:        proof,
		Certificate:  cert,
		Confirmations: 0,
	}

	// Store finalized block
	e.finalized[cert.Block] = finBlock
	e.byHeight[vertex.Height] = append(e.byHeight[vertex.Height], cert.Block)

	// Update frontier
	e.frontier.Update(vertex)

	// Update vertex
	vertex.FinalityScore = 100
	vertex.FinalizedAt = finBlock.FinalizedAt
	vertex.FinalityProof = proof.Serialize()

	// Update statistics
	e.updateStats(finBlock)

	// Process any pending commits that may now be ready
	e.processPendingCommits()

	// Notify callback
	if e.onFinalized != nil {
		go e.onFinalized(finBlock)
	}

	return finBlock, nil
}

// allAncestorsFinalized checks if all ancestors are finalized.
func (e *Engine) allAncestorsFinalized(vertex *dag.Vertex) bool {
	if vertex.IsGenesis() {
		return true
	}

	for _, parentHash := range vertex.Parents {
		if _, exists := e.finalized[parentHash]; !exists {
			return false
		}
	}

	return true
}

// checkConflicts checks for conflicting finalized blocks at the same height.
func (e *Engine) checkConflicts(vertex *dag.Vertex) error {
	existing := e.byHeight[vertex.Height]
	for _, hash := range existing {
		if hash != vertex.Hash {
			// Different block at same height - conflict!
			return ErrConflictingBlock
		}
	}
	return nil
}

// createFinalityProof creates a finality proof from the commit certificate.
func (e *Engine) createFinalityProof(vertex *dag.Vertex, cert *mysticeti.CommitCertificate) *FinalityProof {
	return NewFinalityProof(
		vertex.Hash,
		vertex.Height,
		cert.Round,
		cert.CommitDepth,
		cert.TotalSupportStake,
		cert.TotalStake,
		cert.AggregatedSignature,
	)
}

// processPendingCommits processes queued commits that may now be ready.
func (e *Engine) processPendingCommits() {
	processed := make([]dag.Hash, 0)

	for hash, cert := range e.pendingCommits {
		vertex := e.dagStore.Get(hash)
		if vertex == nil {
			continue
		}

		if e.allAncestorsFinalized(vertex) {
			// Can now finalize this block
			if err := e.checkConflicts(vertex); err != nil {
				// Conflict - orphan this block
				e.orphanBlock(hash)
				processed = append(processed, hash)
				continue
			}

			// Create finality proof
			proof := e.createFinalityProof(vertex, cert)

			finBlock := &FinalizedBlock{
				Hash:        hash,
				Height:      vertex.Height,
				Status:      StatusFinalized,
				FinalizedAt: time.Now(),
				Proof:       proof,
				Certificate: cert,
			}

			e.finalized[hash] = finBlock
			e.byHeight[vertex.Height] = append(e.byHeight[vertex.Height], hash)
			e.frontier.Update(vertex)

			vertex.FinalityScore = 100
			vertex.FinalizedAt = finBlock.FinalizedAt
			vertex.FinalityProof = proof.Serialize()

			e.updateStats(finBlock)
			processed = append(processed, hash)

			if e.onFinalized != nil {
				go e.onFinalized(finBlock)
			}
		}
	}

	// Remove processed commits
	for _, hash := range processed {
		delete(e.pendingCommits, hash)
	}

	e.stats.mu.Lock()
	e.stats.PendingCommits = len(e.pendingCommits)
	e.stats.mu.Unlock()
}

// orphanBlock marks a block as orphaned.
func (e *Engine) orphanBlock(hash dag.Hash) {
	e.stats.mu.Lock()
	e.stats.TotalOrphaned++
	e.stats.mu.Unlock()

	// Could store orphaned blocks for analysis
}

// updateStats updates finality statistics.
func (e *Engine) updateStats(finBlock *FinalizedBlock) {
	e.stats.mu.Lock()
	defer e.stats.mu.Unlock()

	e.stats.TotalFinalized++
	e.stats.LastFinalizedHeight = finBlock.Height
	e.stats.LastFinalizedTime = finBlock.FinalizedAt

	// Update average finality time (simplified)
	vertex := e.dagStore.Get(finBlock.Hash)
	if vertex != nil {
		finalityDuration := finBlock.FinalizedAt.Sub(vertex.Timestamp)
		if e.stats.AverageFinalityTime == 0 {
			e.stats.AverageFinalityTime = finalityDuration
		} else {
			// Exponential moving average
			e.stats.AverageFinalityTime = (e.stats.AverageFinalityTime*9 + finalityDuration) / 10
		}
	}
}

// === Query Methods ===

// IsFinalized returns true if the block is finalized.
func (e *Engine) IsFinalized(hash dag.Hash) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	finBlock, exists := e.finalized[hash]
	return exists && finBlock.Status == StatusFinalized
}

// GetFinalizedBlock returns the finalized block info.
func (e *Engine) GetFinalizedBlock(hash dag.Hash) *FinalizedBlock {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.finalized[hash]
}

// GetFinalizedAtHeight returns finalized blocks at a specific height.
func (e *Engine) GetFinalizedAtHeight(height uint64) []*FinalizedBlock {
	e.mu.RLock()
	defer e.mu.RUnlock()

	hashes := e.byHeight[height]
	result := make([]*FinalizedBlock, 0, len(hashes))

	for _, hash := range hashes {
		if fb := e.finalized[hash]; fb != nil {
			result = append(result, fb)
		}
	}

	return result
}

// GetFinalityStatus returns the finality status of a block.
func (e *Engine) GetFinalityStatus(hash dag.Hash) FinalityStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if fb, exists := e.finalized[hash]; exists {
		return fb.Status
	}

	if _, pending := e.pendingCommits[hash]; pending {
		return StatusCommitted
	}

	return StatusPending
}

// GetFinalityProof returns the finality proof for a block.
func (e *Engine) GetFinalityProof(hash dag.Hash) *FinalityProof {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if fb := e.finalized[hash]; fb != nil {
		return fb.Proof
	}

	return nil
}

// GetLastFinalizedBlock returns the most recently finalized block.
func (e *Engine) GetLastFinalizedBlock() *FinalizedBlock {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var latest *FinalizedBlock
	for _, fb := range e.finalized {
		if latest == nil || fb.Height > latest.Height {
			latest = fb
		}
	}

	return latest
}

// GetFinalizedChain returns the finalized chain up to the given height.
func (e *Engine) GetFinalizedChain(maxHeight uint64) []*FinalizedBlock {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]*FinalizedBlock, 0)

	for height := uint64(0); height <= maxHeight; height++ {
		blocks := e.GetFinalizedAtHeight(height)
		result = append(result, blocks...)
	}

	return result
}

// VerifyFinality verifies a block's finality proof.
func (e *Engine) VerifyFinality(hash dag.Hash) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	fb := e.finalized[hash]
	if fb == nil {
		return false, ErrNotFinalized
	}

	if fb.Proof == nil {
		return false, ErrInvalidProof
	}

	// Verify the proof
	return fb.Proof.Verify(e.config.QuorumThreshold)
}

// === Frontier Methods ===

// GetFrontier returns the current finality frontier.
func (e *Engine) GetFrontier() *Frontier {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.frontier.Clone()
}

// GetFrontierHeight returns the minimum height of the frontier.
func (e *Engine) GetFrontierHeight() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.frontier.MinHeight()
}

// === Reorg Methods ===

// DetectReorg checks for potential chain reorganization.
func (e *Engine) DetectReorg(newBlock *dag.Vertex) (*ReorgEvent, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.reorgHandler.DetectReorg(newBlock)
}

// HandleReorg processes a chain reorganization.
func (e *Engine) HandleReorg(event *ReorgEvent) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.reorgHandler.HandleReorg(event); err != nil {
		return err
	}

	// Notify callback
	if e.onReorg != nil {
		go e.onReorg(event)
	}

	return nil
}

// === Callbacks ===

// OnFinalized sets a callback for when a block is finalized.
func (e *Engine) OnFinalized(callback func(*FinalizedBlock)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onFinalized = callback
}

// OnReorg sets a callback for chain reorganizations.
func (e *Engine) OnReorg(callback func(*ReorgEvent)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onReorg = callback
}

// === Statistics ===

// GetStatistics returns finality statistics.
func (e *Engine) GetStatistics() *Statistics {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return &Statistics{
		TotalFinalized:      e.stats.TotalFinalized,
		TotalCommitted:      e.stats.TotalCommitted,
		TotalOrphaned:       e.stats.TotalOrphaned,
		AverageFinalityTime: e.stats.AverageFinalityTime,
		LastFinalizedHeight: e.stats.LastFinalizedHeight,
		LastFinalizedTime:   e.stats.LastFinalizedTime,
		ReorgsDetected:      e.stats.ReorgsDetected,
		PendingCommits:      e.stats.PendingCommits,
	}
}

// === Pruning ===

// Prune removes old finalized blocks from memory.
func (e *Engine) Prune(keepHeight uint64) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	pruned := 0

	for hash, fb := range e.finalized {
		if fb.Height < keepHeight {
			delete(e.finalized, hash)
			pruned++
		}
	}

	for height := range e.byHeight {
		if height < keepHeight {
			delete(e.byHeight, height)
		}
	}

	return pruned
}

// === Genesis ===

// FinalizeGenesis marks the genesis block as finalized.
func (e *Engine) FinalizeGenesis(genesis *dag.Vertex) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !genesis.IsGenesis() {
		return errors.New("not a genesis block")
	}

	proof := &FinalityProof{
		BlockHash:   genesis.Hash,
		BlockHeight: 0,
		CommitRound: 0,
		CommitDepth: 0,
		IsGenesis:   true,
	}

	finBlock := &FinalizedBlock{
		Hash:        genesis.Hash,
		Height:      0,
		Status:      StatusFinalized,
		FinalizedAt: genesis.Timestamp,
		Proof:       proof,
	}

	e.finalized[genesis.Hash] = finBlock
	e.byHeight[0] = []dag.Hash{genesis.Hash}
	e.frontier.Update(genesis)

	genesis.FinalityScore = 100
	genesis.FinalizedAt = genesis.Timestamp
	genesis.FinalityProof = proof.Serialize()

	// Update statistics
	e.stats.mu.Lock()
	e.stats.TotalFinalized++
	e.stats.LastFinalizedHeight = 0
	e.stats.LastFinalizedTime = genesis.Timestamp
	e.stats.mu.Unlock()

	// Notify callback
	if e.onFinalized != nil {
		go e.onFinalized(finBlock)
	}

	return nil
}
