// Package finality implements the finality frontier for NovaCoin.
//
// The frontier tracks the boundary between finalized and pending blocks,
// allowing efficient queries for:
//   - The latest finalized block at each height
//   - Whether all ancestors of a block are finalized
//   - The minimum finalized height (for pruning)
package finality

import (
	"sort"
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// FrontierEntry represents a finalized block on the frontier.
type FrontierEntry struct {
	Hash            dag.Hash
	Height          uint64
	ValidatorIndex  uint32
	FinalizedAt     int64 // Unix timestamp
}

// Frontier tracks the finality frontier - the set of most recently
// finalized blocks that form the boundary with pending blocks.
type Frontier struct {
	// Entries by validator (each validator's latest finalized block)
	byValidator map[uint32]*FrontierEntry

	// Entries by height (multiple validators can finalize at same height)
	byHeight map[uint64][]*FrontierEntry

	// Latest entry by hash for quick lookup
	byHash map[dag.Hash]*FrontierEntry

	// Cached values
	minHeight   uint64
	maxHeight   uint64
	initialized bool

	mu sync.RWMutex
}

// NewFrontier creates a new finality frontier.
func NewFrontier() *Frontier {
	return &Frontier{
		byValidator: make(map[uint32]*FrontierEntry),
		byHeight:    make(map[uint64][]*FrontierEntry),
		byHash:      make(map[dag.Hash]*FrontierEntry),
	}
}

// Update adds or updates a block on the frontier.
func (f *Frontier) Update(vertex *dag.Vertex) {
	f.mu.Lock()
	defer f.mu.Unlock()

	entry := &FrontierEntry{
		Hash:           vertex.Hash,
		Height:         vertex.Height,
		ValidatorIndex: vertex.ValidatorIndex,
		FinalizedAt:    vertex.FinalizedAt.Unix(),
	}

	// Update by validator
	oldEntry := f.byValidator[vertex.ValidatorIndex]
	if oldEntry != nil && oldEntry.Height < vertex.Height {
		// Remove old entry from byHash
		delete(f.byHash, oldEntry.Hash)
		// Remove from byHeight
		f.removeFromHeight(oldEntry)
	}
	f.byValidator[vertex.ValidatorIndex] = entry

	// Update by height
	f.byHeight[vertex.Height] = append(f.byHeight[vertex.Height], entry)

	// Update by hash
	f.byHash[vertex.Hash] = entry

	// Update cached values
	f.updateCachedValues()
}

// removeFromHeight removes an entry from the byHeight map.
func (f *Frontier) removeFromHeight(entry *FrontierEntry) {
	entries := f.byHeight[entry.Height]
	for i, e := range entries {
		if e.Hash == entry.Hash {
			f.byHeight[entry.Height] = append(entries[:i], entries[i+1:]...)
			break
		}
	}
	if len(f.byHeight[entry.Height]) == 0 {
		delete(f.byHeight, entry.Height)
	}
}

// updateCachedValues updates min/max height cache.
func (f *Frontier) updateCachedValues() {
	if len(f.byHeight) == 0 {
		f.initialized = false
		return
	}

	f.initialized = true
	f.minHeight = ^uint64(0) // Max uint64
	f.maxHeight = 0

	for height := range f.byHeight {
		if height < f.minHeight {
			f.minHeight = height
		}
		if height > f.maxHeight {
			f.maxHeight = height
		}
	}
}

// Contains checks if a block is on the frontier.
func (f *Frontier) Contains(hash dag.Hash) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, exists := f.byHash[hash]
	return exists
}

// Get returns the frontier entry for a block.
func (f *Frontier) Get(hash dag.Hash) *FrontierEntry {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.byHash[hash]
}

// GetByValidator returns the frontier entry for a validator.
func (f *Frontier) GetByValidator(validatorIndex uint32) *FrontierEntry {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.byValidator[validatorIndex]
}

// GetAtHeight returns all frontier entries at a specific height.
func (f *Frontier) GetAtHeight(height uint64) []*FrontierEntry {
	f.mu.RLock()
	defer f.mu.RUnlock()

	entries := f.byHeight[height]
	result := make([]*FrontierEntry, len(entries))
	copy(result, entries)
	return result
}

// MinHeight returns the minimum height on the frontier.
func (f *Frontier) MinHeight() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if !f.initialized {
		return 0
	}
	return f.minHeight
}

// MaxHeight returns the maximum height on the frontier.
func (f *Frontier) MaxHeight() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if !f.initialized {
		return 0
	}
	return f.maxHeight
}

// Size returns the number of entries on the frontier.
func (f *Frontier) Size() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.byHash)
}

// ValidatorCount returns the number of validators on the frontier.
func (f *Frontier) ValidatorCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.byValidator)
}

// GetAllEntries returns all entries on the frontier.
func (f *Frontier) GetAllEntries() []*FrontierEntry {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make([]*FrontierEntry, 0, len(f.byHash))
	for _, entry := range f.byHash {
		result = append(result, entry)
	}
	return result
}

// GetEntriesSortedByHeight returns entries sorted by height (ascending).
func (f *Frontier) GetEntriesSortedByHeight() []*FrontierEntry {
	entries := f.GetAllEntries()
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Height < entries[j].Height
	})
	return entries
}

// GetHashes returns all block hashes on the frontier.
func (f *Frontier) GetHashes() []dag.Hash {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make([]dag.Hash, 0, len(f.byHash))
	for hash := range f.byHash {
		result = append(result, hash)
	}
	return result
}

// GetHeights returns all heights on the frontier.
func (f *Frontier) GetHeights() []uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make([]uint64, 0, len(f.byHeight))
	for height := range f.byHeight {
		result = append(result, height)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i] < result[j]
	})
	return result
}

// Clone creates a deep copy of the frontier.
func (f *Frontier) Clone() *Frontier {
	f.mu.RLock()
	defer f.mu.RUnlock()

	clone := NewFrontier()
	clone.minHeight = f.minHeight
	clone.maxHeight = f.maxHeight
	clone.initialized = f.initialized

	for k, v := range f.byValidator {
		entryCopy := *v
		clone.byValidator[k] = &entryCopy
	}

	for k, entries := range f.byHeight {
		clonedEntries := make([]*FrontierEntry, len(entries))
		for i, e := range entries {
			entryCopy := *e
			clonedEntries[i] = &entryCopy
		}
		clone.byHeight[k] = clonedEntries
	}

	for k, v := range f.byHash {
		entryCopy := *v
		clone.byHash[k] = &entryCopy
	}

	return clone
}

// Clear removes all entries from the frontier.
func (f *Frontier) Clear() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.byValidator = make(map[uint32]*FrontierEntry)
	f.byHeight = make(map[uint64][]*FrontierEntry)
	f.byHash = make(map[dag.Hash]*FrontierEntry)
	f.initialized = false
	f.minHeight = 0
	f.maxHeight = 0
}

// Prune removes entries below a certain height.
func (f *Frontier) Prune(keepHeight uint64) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	pruned := 0

	// Find entries to prune
	toRemove := make([]dag.Hash, 0)
	for hash, entry := range f.byHash {
		if entry.Height < keepHeight {
			toRemove = append(toRemove, hash)
		}
	}

	// Remove entries
	for _, hash := range toRemove {
		entry := f.byHash[hash]
		delete(f.byHash, hash)

		// Remove from byValidator if this was the latest
		if v, exists := f.byValidator[entry.ValidatorIndex]; exists && v.Hash == hash {
			delete(f.byValidator, entry.ValidatorIndex)
		}

		// Remove from byHeight
		f.removeFromHeight(entry)
		pruned++
	}

	// Update cached values
	f.updateCachedValues()

	return pruned
}

// === Analysis Methods ===

// GetGap returns the difference between max and min height.
func (f *Frontier) GetGap() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if !f.initialized || f.maxHeight <= f.minHeight {
		return 0
	}
	return f.maxHeight - f.minHeight
}

// IsCompact returns true if all validators are at similar heights.
func (f *Frontier) IsCompact(maxGap uint64) bool {
	return f.GetGap() <= maxGap
}

// GetLaggingValidators returns validators below a certain height.
func (f *Frontier) GetLaggingValidators(minHeight uint64) []uint32 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make([]uint32, 0)
	for idx, entry := range f.byValidator {
		if entry.Height < minHeight {
			result = append(result, idx)
		}
	}
	return result
}

// GetLeadingValidators returns validators above a certain height.
func (f *Frontier) GetLeadingValidators(maxHeight uint64) []uint32 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make([]uint32, 0)
	for idx, entry := range f.byValidator {
		if entry.Height > maxHeight {
			result = append(result, idx)
		}
	}
	return result
}

// GetMedianHeight returns the median height across all validators.
func (f *Frontier) GetMedianHeight() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if len(f.byValidator) == 0 {
		return 0
	}

	heights := make([]uint64, 0, len(f.byValidator))
	for _, entry := range f.byValidator {
		heights = append(heights, entry.Height)
	}

	sort.Slice(heights, func(i, j int) bool {
		return heights[i] < heights[j]
	})

	mid := len(heights) / 2
	if len(heights)%2 == 0 {
		return (heights[mid-1] + heights[mid]) / 2
	}
	return heights[mid]
}

// GetHeightDistribution returns a histogram of heights.
func (f *Frontier) GetHeightDistribution() map[uint64]int {
	f.mu.RLock()
	defer f.mu.RUnlock()

	dist := make(map[uint64]int)
	for _, entry := range f.byValidator {
		dist[entry.Height]++
	}
	return dist
}

// === Serialization ===

// FrontierSnapshot is a serializable snapshot of the frontier.
type FrontierSnapshot struct {
	Entries   []*FrontierEntry
	MinHeight uint64
	MaxHeight uint64
}

// Snapshot creates a snapshot of the frontier.
func (f *Frontier) Snapshot() *FrontierSnapshot {
	f.mu.RLock()
	defer f.mu.RUnlock()

	entries := make([]*FrontierEntry, 0, len(f.byHash))
	for _, entry := range f.byHash {
		entryCopy := *entry
		entries = append(entries, &entryCopy)
	}

	return &FrontierSnapshot{
		Entries:   entries,
		MinHeight: f.minHeight,
		MaxHeight: f.maxHeight,
	}
}

// RestoreFromSnapshot restores the frontier from a snapshot.
func (f *Frontier) RestoreFromSnapshot(snap *FrontierSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.byValidator = make(map[uint32]*FrontierEntry)
	f.byHeight = make(map[uint64][]*FrontierEntry)
	f.byHash = make(map[dag.Hash]*FrontierEntry)

	for _, entry := range snap.Entries {
		entryCopy := *entry
		f.byValidator[entry.ValidatorIndex] = &entryCopy
		f.byHeight[entry.Height] = append(f.byHeight[entry.Height], &entryCopy)
		f.byHash[entry.Hash] = &entryCopy
	}

	f.minHeight = snap.MinHeight
	f.maxHeight = snap.MaxHeight
	f.initialized = len(snap.Entries) > 0
}

// === Checkpoint Support ===

// Checkpoint represents a finality checkpoint.
type Checkpoint struct {
	// Block at the checkpoint
	BlockHash   dag.Hash
	BlockHeight uint64

	// Frontier state at checkpoint
	FrontierSnapshot *FrontierSnapshot

	// Stake that confirmed the checkpoint
	ConfirmingStake uint64
	TotalStake      uint64

	// Creation time
	CreatedAt int64
}

// NewCheckpoint creates a new checkpoint from the current frontier.
func (f *Frontier) NewCheckpoint(blockHash dag.Hash, blockHeight uint64, confirmingStake, totalStake uint64) *Checkpoint {
	return &Checkpoint{
		BlockHash:        blockHash,
		BlockHeight:      blockHeight,
		FrontierSnapshot: f.Snapshot(),
		ConfirmingStake:  confirmingStake,
		TotalStake:       totalStake,
		CreatedAt:        nowUnix(),
	}
}

// AdvanceTo moves the frontier to a new checkpoint.
func (f *Frontier) AdvanceTo(checkpoint *Checkpoint) {
	f.RestoreFromSnapshot(checkpoint.FrontierSnapshot)
}

// nowUnix returns current Unix timestamp.
func nowUnix() int64 {
	return time.Now().Unix()
}
