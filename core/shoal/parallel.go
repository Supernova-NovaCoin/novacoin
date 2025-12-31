// Package shoal implements the Shoal++ multi-anchor high-throughput consensus layer.
package shoal

import (
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// ParallelDAG represents one of the parallel DAGs in Shoal++.
// Multiple DAGs run concurrently to increase throughput.
type ParallelDAG struct {
	// Identity
	ID     uint8  // DAG ID (0-3 typically)
	Name   string // Human-readable name

	// Timing
	Offset       time.Duration // Stagger offset from wave start
	LastBlockAt  time.Time     // When the last block was produced
	NextSlotAt   time.Time     // When the next slot opens

	// Blocks
	Tips       []dag.Hash // Current tips of this DAG
	TipCount   int        // Number of tips
	BlockCount uint64     // Total blocks in this DAG

	// Wave tracking per DAG
	CurrentWave   uint64            // Current wave in this DAG
	WaveHeads     map[uint64][]dag.Hash // Wave -> tip hashes at wave end
	WaveBlockCount map[uint64]int    // Wave -> number of blocks

	// Performance
	BlocksPerSecond float64
	AvgPropagation  time.Duration
}

// NewParallelDAG creates a new parallel DAG.
func NewParallelDAG(id uint8, offset time.Duration) *ParallelDAG {
	return &ParallelDAG{
		ID:              id,
		Name:            parallelDAGName(id),
		Offset:          offset,
		Tips:            make([]dag.Hash, 0),
		WaveHeads:       make(map[uint64][]dag.Hash),
		WaveBlockCount:  make(map[uint64]int),
	}
}

// parallelDAGName returns a name for a parallel DAG.
func parallelDAGName(id uint8) string {
	names := []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta", "Eta", "Theta"}
	if int(id) < len(names) {
		return names[id]
	}
	return "DAG-" + string(rune('A'+id))
}

// AddBlock records a new block in this parallel DAG.
func (pd *ParallelDAG) AddBlock(hash dag.Hash, wave uint64) {
	pd.Tips = append(pd.Tips, hash)
	pd.TipCount++
	pd.BlockCount++
	pd.LastBlockAt = time.Now()
	pd.CurrentWave = wave
	pd.WaveBlockCount[wave]++
}

// RecordWaveEnd records the tips at the end of a wave.
func (pd *ParallelDAG) RecordWaveEnd(wave uint64, tips []dag.Hash) {
	pd.WaveHeads[wave] = make([]dag.Hash, len(tips))
	copy(pd.WaveHeads[wave], tips)
}

// SetTips sets the current tips of this DAG.
func (pd *ParallelDAG) SetTips(tips []dag.Hash) {
	pd.Tips = make([]dag.Hash, len(tips))
	copy(pd.Tips, tips)
	pd.TipCount = len(tips)
}

// ParallelDAGCoordinator manages multiple parallel DAGs for high throughput.
type ParallelDAGCoordinator struct {
	config *ParallelConfig

	// Parallel DAGs
	dags []*ParallelDAG

	// Cross-DAG references
	crossReferences map[dag.Hash][]dag.Hash // Hash -> referenced hashes from other DAGs

	// Merged ordering
	mergedOrder []dag.Hash // Global ordering across all DAGs

	// Timing
	waveStartTime time.Time
	currentWave   uint64

	// Callbacks
	onBlockProduced func(dagID uint8, hash dag.Hash)
	onCrossRef      func(from, to dag.Hash)

	mu sync.RWMutex
}

// ParallelConfig holds configuration for parallel DAG coordination.
type ParallelConfig struct {
	// NumDAGs is the number of parallel DAGs.
	NumDAGs int

	// StaggerOffset is the time between DAG slot starts.
	StaggerOffset time.Duration

	// SlotDuration is how long each slot lasts.
	SlotDuration time.Duration

	// MaxTipsPerDAG limits tips in each DAG to prevent bloat.
	MaxTipsPerDAG int

	// EnableCrossRef enables cross-DAG references.
	EnableCrossRef bool

	// MergeStrategy determines how blocks from different DAGs are ordered.
	MergeStrategy MergeStrategy
}

// MergeStrategy defines how blocks from parallel DAGs are merged.
type MergeStrategy int

const (
	// MergeRoundRobin alternates between DAGs in order.
	MergeRoundRobin MergeStrategy = iota
	// MergeByTimestamp orders by block timestamp.
	MergeByTimestamp
	// MergeByStake orders by proposer stake.
	MergeByStake
	// MergeByBlueScore orders by GHOSTDAG blue score.
	MergeByBlueScore
)

// DefaultParallelConfig returns default parallel DAG configuration.
func DefaultParallelConfig() *ParallelConfig {
	return &ParallelConfig{
		NumDAGs:        4,
		StaggerOffset:  125 * time.Millisecond,
		SlotDuration:   500 * time.Millisecond,
		MaxTipsPerDAG:  10,
		EnableCrossRef: true,
		MergeStrategy:  MergeByTimestamp,
	}
}

// NewParallelDAGCoordinator creates a new coordinator for parallel DAGs.
func NewParallelDAGCoordinator(config *ParallelConfig) *ParallelDAGCoordinator {
	if config == nil {
		config = DefaultParallelConfig()
	}

	coord := &ParallelDAGCoordinator{
		config:          config,
		dags:            make([]*ParallelDAG, config.NumDAGs),
		crossReferences: make(map[dag.Hash][]dag.Hash),
		mergedOrder:     make([]dag.Hash, 0),
	}

	// Initialize parallel DAGs
	for i := 0; i < config.NumDAGs; i++ {
		offset := time.Duration(i) * config.StaggerOffset
		coord.dags[i] = NewParallelDAG(uint8(i), offset)
	}

	return coord
}

// StartWave begins a new wave across all parallel DAGs.
func (c *ParallelDAGCoordinator) StartWave(waveNumber uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentWave = waveNumber
	c.waveStartTime = time.Now()

	// Reset each DAG for new wave
	for _, pd := range c.dags {
		pd.CurrentWave = waveNumber
		pd.NextSlotAt = c.waveStartTime.Add(pd.Offset)
	}
}

// EndWave ends the current wave and records tips.
func (c *ParallelDAGCoordinator) EndWave() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, pd := range c.dags {
		pd.RecordWaveEnd(c.currentWave, pd.Tips)
	}
}

// GetCurrentSlotDAG returns the DAG whose slot is currently active.
func (c *ParallelDAGCoordinator) GetCurrentSlotDAG() *ParallelDAG {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()
	elapsed := now.Sub(c.waveStartTime)

	// Find which DAG's slot is active
	slotCycle := c.config.SlotDuration
	cyclePosition := elapsed % (slotCycle * time.Duration(c.config.NumDAGs))

	dagIndex := int(cyclePosition / slotCycle)
	if dagIndex >= len(c.dags) {
		dagIndex = len(c.dags) - 1
	}

	return c.dags[dagIndex]
}

// GetDAG returns a specific parallel DAG.
func (c *ParallelDAGCoordinator) GetDAG(id uint8) *ParallelDAG {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if int(id) >= len(c.dags) {
		return nil
	}
	return c.dags[id]
}

// GetAllDAGs returns all parallel DAGs.
func (c *ParallelDAGCoordinator) GetAllDAGs() []*ParallelDAG {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*ParallelDAG, len(c.dags))
	copy(result, c.dags)
	return result
}

// AddBlock adds a block to the specified parallel DAG.
func (c *ParallelDAGCoordinator) AddBlock(dagID uint8, hash dag.Hash, wave uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if int(dagID) >= len(c.dags) {
		return ErrInvalidDAGID
	}

	pd := c.dags[dagID]
	pd.AddBlock(hash, wave)

	// Prune tips if too many
	if pd.TipCount > c.config.MaxTipsPerDAG {
		c.pruneTips(pd)
	}

	// Notify callback
	if c.onBlockProduced != nil {
		go c.onBlockProduced(dagID, hash)
	}

	return nil
}

// pruneTips removes old tips from a DAG.
func (c *ParallelDAGCoordinator) pruneTips(pd *ParallelDAG) {
	// Keep only the most recent tips
	if len(pd.Tips) <= c.config.MaxTipsPerDAG {
		return
	}
	pd.Tips = pd.Tips[len(pd.Tips)-c.config.MaxTipsPerDAG:]
	pd.TipCount = len(pd.Tips)
}

// AddCrossReference records a cross-DAG reference.
func (c *ParallelDAGCoordinator) AddCrossReference(fromHash, toHash dag.Hash) {
	if !c.config.EnableCrossRef {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.crossReferences[fromHash] = append(c.crossReferences[fromHash], toHash)

	if c.onCrossRef != nil {
		go c.onCrossRef(fromHash, toHash)
	}
}

// GetCrossReferences returns all cross-DAG references for a block.
func (c *ParallelDAGCoordinator) GetCrossReferences(hash dag.Hash) []dag.Hash {
	c.mu.RLock()
	defer c.mu.RUnlock()

	refs := c.crossReferences[hash]
	result := make([]dag.Hash, len(refs))
	copy(result, refs)
	return result
}

// blockInfo holds information about a block for ordering purposes.
type blockInfo struct {
	hash      dag.Hash
	dagID     uint8
	timestamp time.Time
	stake     uint64
	blueScore uint64
}

// MergeDAGOrder computes a global ordering of blocks from all parallel DAGs.
func (c *ParallelDAGCoordinator) MergeDAGOrder(
	wave uint64,
	getVertex func(dag.Hash) *dag.Vertex,
) []dag.Hash {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Collect all blocks from this wave across all DAGs
	var blocks []blockInfo
	for _, pd := range c.dags {
		// Get blocks from this wave
		for hash := range pd.WaveBlockCount {
			if hash == wave {
				for _, tip := range pd.WaveHeads[wave] {
					v := getVertex(tip)
					if v != nil {
						blocks = append(blocks, blockInfo{
							hash:      tip,
							dagID:     pd.ID,
							timestamp: v.Timestamp,
							stake:     v.Stake,
							blueScore: v.BlueScore,
						})
					}
				}
			}
		}
	}

	// Sort based on merge strategy
	switch c.config.MergeStrategy {
	case MergeRoundRobin:
		// Sort by DAG ID, then timestamp within DAG
		c.sortRoundRobin(blocks)
	case MergeByTimestamp:
		// Sort purely by timestamp
		c.sortByTimestamp(blocks)
	case MergeByStake:
		// Sort by stake (descending)
		c.sortByStake(blocks)
	case MergeByBlueScore:
		// Sort by blue score (descending)
		c.sortByBlueScore(blocks)
	}

	// Extract ordered hashes
	c.mergedOrder = make([]dag.Hash, len(blocks))
	for i, b := range blocks {
		c.mergedOrder[i] = b.hash
	}

	return c.mergedOrder
}

func (c *ParallelDAGCoordinator) sortRoundRobin(blocks []blockInfo) {
	// Group by DAG, then interleave
	byDAG := make(map[uint8][]int)
	for i, b := range blocks {
		byDAG[b.dagID] = append(byDAG[b.dagID], i)
	}

	// Round-robin merge
	merged := make([]int, 0, len(blocks))
	maxLen := 0
	for _, indices := range byDAG {
		if len(indices) > maxLen {
			maxLen = len(indices)
		}
	}

	for i := 0; i < maxLen; i++ {
		for dagID := uint8(0); dagID < uint8(c.config.NumDAGs); dagID++ {
			if indices, ok := byDAG[dagID]; ok && i < len(indices) {
				merged = append(merged, indices[i])
			}
		}
	}

	// Reorder blocks
	reordered := make([]blockInfo, len(blocks))
	for i, idx := range merged {
		reordered[i] = blocks[idx]
	}
	copy(blocks, reordered)
}

func (c *ParallelDAGCoordinator) sortByTimestamp(blocks []blockInfo) {
	// Simple insertion sort for small slices
	for i := 1; i < len(blocks); i++ {
		j := i
		for j > 0 && blocks[j-1].timestamp.After(blocks[j].timestamp) {
			blocks[j-1], blocks[j] = blocks[j], blocks[j-1]
			j--
		}
	}
}

func (c *ParallelDAGCoordinator) sortByStake(blocks []blockInfo) {
	for i := 1; i < len(blocks); i++ {
		j := i
		for j > 0 && blocks[j-1].stake < blocks[j].stake {
			blocks[j-1], blocks[j] = blocks[j], blocks[j-1]
			j--
		}
	}
}

func (c *ParallelDAGCoordinator) sortByBlueScore(blocks []blockInfo) {
	for i := 1; i < len(blocks); i++ {
		j := i
		for j > 0 && blocks[j-1].blueScore < blocks[j].blueScore {
			blocks[j-1], blocks[j] = blocks[j], blocks[j-1]
			j--
		}
	}
}

// GetTipsAcrossDAGs returns all tips from all parallel DAGs.
func (c *ParallelDAGCoordinator) GetTipsAcrossDAGs() []dag.Hash {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var allTips []dag.Hash
	for _, pd := range c.dags {
		allTips = append(allTips, pd.Tips...)
	}
	return allTips
}

// SetTipsForDAG sets the tips for a specific DAG.
func (c *ParallelDAGCoordinator) SetTipsForDAG(dagID uint8, tips []dag.Hash) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if int(dagID) >= len(c.dags) {
		return ErrInvalidDAGID
	}

	c.dags[dagID].SetTips(tips)
	return nil
}

// OnBlockProduced sets a callback for when a block is produced.
func (c *ParallelDAGCoordinator) OnBlockProduced(callback func(dagID uint8, hash dag.Hash)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onBlockProduced = callback
}

// OnCrossReference sets a callback for cross-DAG references.
func (c *ParallelDAGCoordinator) OnCrossReference(callback func(from, to dag.Hash)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onCrossRef = callback
}

// ParallelStatistics holds statistics for parallel DAGs.
type ParallelStatistics struct {
	NumDAGs           int
	TotalBlocks       uint64
	TotalTips         int
	CrossReferences   int
	BlocksPerDAG      []uint64
	AvgBlocksPerWave  float64
}

// GetStatistics returns parallel DAG statistics.
func (c *ParallelDAGCoordinator) GetStatistics() *ParallelStatistics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := &ParallelStatistics{
		NumDAGs:         len(c.dags),
		CrossReferences: len(c.crossReferences),
		BlocksPerDAG:    make([]uint64, len(c.dags)),
	}

	for i, pd := range c.dags {
		stats.BlocksPerDAG[i] = pd.BlockCount
		stats.TotalBlocks += pd.BlockCount
		stats.TotalTips += pd.TipCount
	}

	if c.currentWave > 0 {
		stats.AvgBlocksPerWave = float64(stats.TotalBlocks) / float64(c.currentWave)
	}

	return stats
}

// ComputeEffectiveThroughput estimates the effective throughput in blocks per second.
func (c *ParallelDAGCoordinator) ComputeEffectiveThroughput() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Each DAG can produce at its slot rate
	slotsPerSecond := float64(time.Second) / float64(c.config.SlotDuration)
	return slotsPerSecond * float64(len(c.dags))
}

// Error types for parallel DAG operations.
var (
	ErrInvalidDAGID = parallelError("invalid parallel DAG ID")
)

type parallelError string

func (e parallelError) Error() string {
	return string(e)
}
