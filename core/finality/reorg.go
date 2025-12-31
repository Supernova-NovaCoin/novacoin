// Package finality implements chain reorganization handling for NovaCoin.
package finality

import (
	"errors"
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// ReorgType represents the type of chain reorganization.
type ReorgType int

const (
	// ReorgNone indicates no reorganization.
	ReorgNone ReorgType = iota
	// ReorgShallow is a shallow reorg within acceptable depth.
	ReorgShallow
	// ReorgDeep is a deep reorg that may indicate an attack.
	ReorgDeep
	// ReorgFinality is an attempted reorg of finalized blocks (rejected).
	ReorgFinality
)

func (t ReorgType) String() string {
	switch t {
	case ReorgNone:
		return "none"
	case ReorgShallow:
		return "shallow"
	case ReorgDeep:
		return "deep"
	case ReorgFinality:
		return "finality_violation"
	default:
		return "unknown"
	}
}

// ReorgEvent represents a chain reorganization event.
type ReorgEvent struct {
	// Reorg type
	Type ReorgType

	// Common ancestor of old and new chains
	CommonAncestor dag.Hash
	CommonHeight   uint64

	// Old chain (being replaced)
	OldTip    dag.Hash
	OldBlocks []dag.Hash
	OldHeight uint64

	// New chain (replacing old)
	NewTip    dag.Hash
	NewBlocks []dag.Hash
	NewHeight uint64

	// Reorg depth (number of blocks being rolled back)
	Depth int

	// Detection time
	DetectedAt time.Time

	// Was this reorg accepted or rejected?
	Accepted bool
	Reason   string
}

// ReorgHandler handles chain reorganization detection and processing.
type ReorgHandler struct {
	config *Config
	engine *Engine

	// Current chain tip
	currentTip dag.Hash
	tipHeight  uint64

	// Reorg history for analysis
	history []*ReorgEvent

	// Callbacks
	onReorgDetected func(*ReorgEvent)
	onReorgApplied  func(*ReorgEvent)
	onReorgRejected func(*ReorgEvent)

	mu sync.RWMutex
}

// NewReorgHandler creates a new reorganization handler.
func NewReorgHandler(config *Config, engine *Engine) *ReorgHandler {
	return &ReorgHandler{
		config:  config,
		engine:  engine,
		history: make([]*ReorgEvent, 0),
	}
}

// DetectReorg checks if a new block causes a chain reorganization.
func (rh *ReorgHandler) DetectReorg(newBlock *dag.Vertex) (*ReorgEvent, error) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	// If we have no current tip, this is the first block
	if rh.currentTip.IsEmpty() {
		rh.currentTip = newBlock.Hash
		rh.tipHeight = newBlock.Height
		return nil, nil
	}

	// Check if this extends the current chain
	if rh.extendsChain(newBlock) {
		rh.currentTip = newBlock.Hash
		rh.tipHeight = newBlock.Height
		return nil, nil
	}

	// Check if this is on a competing chain
	if newBlock.Height <= rh.tipHeight {
		// Competing block at same or lower height
		return rh.analyzeCompetingBlock(newBlock)
	}

	// This block is higher but doesn't extend our chain - potential reorg
	return rh.detectPotentialReorg(newBlock)
}

// extendsChain checks if the new block extends the current chain.
func (rh *ReorgHandler) extendsChain(newBlock *dag.Vertex) bool {
	for _, parent := range newBlock.Parents {
		if parent == rh.currentTip {
			return true
		}
	}
	return false
}

// analyzeCompetingBlock analyzes a competing block at the same or lower height.
func (rh *ReorgHandler) analyzeCompetingBlock(newBlock *dag.Vertex) (*ReorgEvent, error) {
	// Find common ancestor
	ancestor, oldChain, newChain := rh.findForkPoint(newBlock)

	event := &ReorgEvent{
		CommonAncestor: ancestor,
		OldTip:         rh.currentTip,
		OldBlocks:      oldChain,
		OldHeight:      rh.tipHeight,
		NewTip:         newBlock.Hash,
		NewBlocks:      newChain,
		NewHeight:      newBlock.Height,
		Depth:          len(oldChain),
		DetectedAt:     time.Now(),
	}

	// Determine reorg type
	event.Type = rh.classifyReorg(event)

	// Check if any finalized blocks would be reverted
	if rh.wouldRevertFinalized(oldChain) {
		event.Type = ReorgFinality
		event.Accepted = false
		event.Reason = "cannot revert finalized blocks"
		return event, ErrFinalityViolation
	}

	return event, nil
}

// detectPotentialReorg detects a potential reorg when a higher block doesn't extend our chain.
func (rh *ReorgHandler) detectPotentialReorg(newBlock *dag.Vertex) (*ReorgEvent, error) {
	// Find common ancestor
	ancestor, oldChain, newChain := rh.findForkPoint(newBlock)

	event := &ReorgEvent{
		CommonAncestor: ancestor,
		OldTip:         rh.currentTip,
		OldBlocks:      oldChain,
		OldHeight:      rh.tipHeight,
		NewTip:         newBlock.Hash,
		NewBlocks:      newChain,
		NewHeight:      newBlock.Height,
		Depth:          len(oldChain),
		DetectedAt:     time.Now(),
	}

	// Determine reorg type
	event.Type = rh.classifyReorg(event)

	// Check for finality violations
	if rh.wouldRevertFinalized(oldChain) {
		event.Type = ReorgFinality
		event.Accepted = false
		event.Reason = "cannot revert finalized blocks"
		return event, ErrFinalityViolation
	}

	// Check depth limit
	if rh.config.EnableReorgProtection && rh.config.MaxReorgDepth > 0 {
		if event.Depth > rh.config.MaxReorgDepth {
			event.Type = ReorgDeep
			event.Accepted = false
			event.Reason = "reorg depth exceeds limit"
			return event, ErrReorgDetected
		}
	}

	return event, nil
}

// findForkPoint finds the common ancestor and diverging chains.
func (rh *ReorgHandler) findForkPoint(newBlock *dag.Vertex) (dag.Hash, []dag.Hash, []dag.Hash) {
	oldChain := make([]dag.Hash, 0)
	newChain := make([]dag.Hash, 0)

	// Start with current tip and new block
	oldHash := rh.currentTip
	newHash := newBlock.Hash
	newChain = append(newChain, newHash)

	// Get heights
	oldVertex := rh.engine.dagStore.Get(oldHash)
	if oldVertex == nil {
		return dag.Hash{}, oldChain, newChain
	}
	oldHeight := oldVertex.Height
	newHeight := newBlock.Height

	// Walk back both chains until we find common ancestor
	for oldHash != newHash {
		if oldHeight >= newHeight && !oldHash.IsEmpty() {
			oldChain = append(oldChain, oldHash)
			// Move old chain back
			oldVertex = rh.engine.dagStore.Get(oldHash)
			if oldVertex != nil && len(oldVertex.Parents) > 0 {
				oldHash = oldVertex.SelectedParent
				if oldHash.IsEmpty() {
					oldHash = oldVertex.Parents[0]
				}
				oldHeight--
			} else {
				break
			}
		} else if !newHash.IsEmpty() {
			// Move new chain back
			newVertex := rh.engine.dagStore.Get(newHash)
			if newVertex != nil && len(newVertex.Parents) > 0 {
				newHash = newVertex.SelectedParent
				if newHash.IsEmpty() {
					newHash = newVertex.Parents[0]
				}
				newChain = append(newChain, newHash)
				newHeight--
			} else {
				break
			}
		} else {
			break
		}

		// Safety: prevent infinite loop
		if len(oldChain) > 10000 || len(newChain) > 10000 {
			break
		}
	}

	// The common ancestor is where they meet
	ancestor := oldHash
	if oldHash != newHash {
		ancestor = dag.Hash{} // No common ancestor found
	}

	// Remove the common ancestor from new chain if present
	if len(newChain) > 0 && newChain[len(newChain)-1] == ancestor {
		newChain = newChain[:len(newChain)-1]
	}

	return ancestor, oldChain, newChain
}

// classifyReorg determines the type of reorganization.
func (rh *ReorgHandler) classifyReorg(event *ReorgEvent) ReorgType {
	if event.Depth == 0 {
		return ReorgNone
	}

	// Check if any finalized blocks are affected
	for _, hash := range event.OldBlocks {
		if rh.engine.IsFinalized(hash) {
			return ReorgFinality
		}
	}

	// Check depth
	if event.Depth > rh.config.MaxReorgDepth && rh.config.MaxReorgDepth > 0 {
		return ReorgDeep
	}

	return ReorgShallow
}

// wouldRevertFinalized checks if the reorg would revert finalized blocks.
func (rh *ReorgHandler) wouldRevertFinalized(blocks []dag.Hash) bool {
	for _, hash := range blocks {
		if rh.engine.IsFinalized(hash) {
			return true
		}
	}
	return false
}

// HandleReorg processes a chain reorganization.
func (rh *ReorgHandler) HandleReorg(event *ReorgEvent) error {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	// Check if reorg should be accepted
	switch event.Type {
	case ReorgNone:
		return nil

	case ReorgFinality:
		// Never accept finality violations
		event.Accepted = false
		if event.Reason == "" {
			event.Reason = "finality violation"
		}
		rh.recordReorg(event)

		if rh.onReorgRejected != nil {
			go rh.onReorgRejected(event)
		}
		return ErrFinalityViolation

	case ReorgDeep:
		// Reject deep reorgs if protection is enabled
		if rh.config.EnableReorgProtection {
			event.Accepted = false
			if event.Reason == "" {
				event.Reason = "deep reorg rejected"
			}
			rh.recordReorg(event)

			if rh.onReorgRejected != nil {
				go rh.onReorgRejected(event)
			}
			return ErrReorgDetected
		}
		// Fall through to apply if protection disabled
		fallthrough

	case ReorgShallow:
		// Accept shallow reorgs
		event.Accepted = true
		rh.recordReorg(event)

		// Update chain tip
		rh.currentTip = event.NewTip
		rh.tipHeight = event.NewHeight

		// Update engine statistics
		rh.engine.stats.mu.Lock()
		rh.engine.stats.ReorgsDetected++
		rh.engine.stats.mu.Unlock()

		if rh.onReorgApplied != nil {
			go rh.onReorgApplied(event)
		}
		return nil
	}

	return nil
}

// recordReorg records a reorg event in history.
func (rh *ReorgHandler) recordReorg(event *ReorgEvent) {
	rh.history = append(rh.history, event)

	// Limit history size
	if len(rh.history) > 1000 {
		rh.history = rh.history[len(rh.history)-1000:]
	}
}

// SetCurrentTip sets the current chain tip.
func (rh *ReorgHandler) SetCurrentTip(hash dag.Hash, height uint64) {
	rh.mu.Lock()
	defer rh.mu.Unlock()
	rh.currentTip = hash
	rh.tipHeight = height
}

// GetCurrentTip returns the current chain tip.
func (rh *ReorgHandler) GetCurrentTip() (dag.Hash, uint64) {
	rh.mu.RLock()
	defer rh.mu.RUnlock()
	return rh.currentTip, rh.tipHeight
}

// GetReorgHistory returns the reorg history.
func (rh *ReorgHandler) GetReorgHistory() []*ReorgEvent {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	result := make([]*ReorgEvent, len(rh.history))
	copy(result, rh.history)
	return result
}

// GetRecentReorgs returns reorgs in the last duration.
func (rh *ReorgHandler) GetRecentReorgs(duration time.Duration) []*ReorgEvent {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	result := make([]*ReorgEvent, 0)

	for _, event := range rh.history {
		if event.DetectedAt.After(cutoff) {
			result = append(result, event)
		}
	}

	return result
}

// GetReorgStats returns reorg statistics.
func (rh *ReorgHandler) GetReorgStats() ReorgStats {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	stats := ReorgStats{
		TotalReorgs: len(rh.history),
	}

	for _, event := range rh.history {
		switch event.Type {
		case ReorgShallow:
			stats.ShallowReorgs++
		case ReorgDeep:
			stats.DeepReorgs++
		case ReorgFinality:
			stats.FinalityViolations++
		}

		if event.Accepted {
			stats.AcceptedReorgs++
			stats.TotalDepth += event.Depth
		} else {
			stats.RejectedReorgs++
		}
	}

	if stats.AcceptedReorgs > 0 {
		stats.AverageDepth = float64(stats.TotalDepth) / float64(stats.AcceptedReorgs)
	}

	return stats
}

// ReorgStats contains reorg statistics.
type ReorgStats struct {
	TotalReorgs        int
	ShallowReorgs      int
	DeepReorgs         int
	FinalityViolations int
	AcceptedReorgs     int
	RejectedReorgs     int
	TotalDepth         int
	AverageDepth       float64
}

// === Callbacks ===

// OnReorgDetected sets callback for reorg detection.
func (rh *ReorgHandler) OnReorgDetected(callback func(*ReorgEvent)) {
	rh.mu.Lock()
	defer rh.mu.Unlock()
	rh.onReorgDetected = callback
}

// OnReorgApplied sets callback for applied reorgs.
func (rh *ReorgHandler) OnReorgApplied(callback func(*ReorgEvent)) {
	rh.mu.Lock()
	defer rh.mu.Unlock()
	rh.onReorgApplied = callback
}

// OnReorgRejected sets callback for rejected reorgs.
func (rh *ReorgHandler) OnReorgRejected(callback func(*ReorgEvent)) {
	rh.mu.Lock()
	defer rh.mu.Unlock()
	rh.onReorgRejected = callback
}

// === Rollback Support ===

// RollbackState represents the state needed to rollback a reorg.
type RollbackState struct {
	// Blocks to remove
	BlocksToRemove []dag.Hash

	// Blocks to restore
	BlocksToRestore []dag.Hash

	// State roots before and after
	OldStateRoot dag.Hash
	NewStateRoot dag.Hash

	// Transaction hashes to revert
	TxsToRevert []dag.Hash

	// Transaction hashes to reapply
	TxsToReapply []dag.Hash
}

// ComputeRollbackState computes the state needed to apply a reorg.
func (rh *ReorgHandler) ComputeRollbackState(event *ReorgEvent) (*RollbackState, error) {
	if event.Type == ReorgFinality {
		return nil, ErrFinalityViolation
	}

	state := &RollbackState{
		BlocksToRemove:  event.OldBlocks,
		BlocksToRestore: event.NewBlocks,
	}

	// Collect transactions from blocks being removed
	for _, hash := range event.OldBlocks {
		block := rh.engine.dagStore.Get(hash)
		if block != nil {
			// Would collect transaction hashes here
			_ = block
		}
	}

	// Collect transactions from blocks being added
	for _, hash := range event.NewBlocks {
		block := rh.engine.dagStore.Get(hash)
		if block != nil {
			// Would collect transaction hashes here
			_ = block
		}
	}

	return state, nil
}

// CanReorg checks if a reorg can be applied.
func (rh *ReorgHandler) CanReorg(depth int) bool {
	if !rh.config.EnableReorgProtection {
		return true
	}

	if rh.config.MaxReorgDepth == 0 {
		return true
	}

	return depth <= rh.config.MaxReorgDepth
}

// ValidateReorg validates that a reorg is safe to apply.
func (rh *ReorgHandler) ValidateReorg(event *ReorgEvent) error {
	// Check for finality violations
	if rh.wouldRevertFinalized(event.OldBlocks) {
		return ErrFinalityViolation
	}

	// Check depth limit
	if !rh.CanReorg(event.Depth) {
		return errors.New("reorg depth exceeds limit")
	}

	// Verify common ancestor exists
	if event.CommonAncestor.IsEmpty() && len(event.OldBlocks) > 0 {
		return errors.New("no common ancestor found")
	}

	return nil
}
