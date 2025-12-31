// Package ghostdag implements stake-weighted GHOSTDAG consensus.
// GHOSTDAG determines which blocks are "blue" (honest majority) and "red" (adversarial).
package ghostdag

import (
	"math/big"
	"sort"
	"sync"

	"github.com/novacoin/novacoin/core/dag"
)

// GHOSTDAGEngine implements the stake-weighted GHOSTDAG protocol.
type GHOSTDAGEngine struct {
	store  *dag.Store
	config *Config

	// Cache for computed values
	blueScoreCache    map[dag.Hash]uint64
	selectedParentCache map[dag.Hash]dag.Hash

	mu sync.RWMutex
}

// Config holds GHOSTDAG configuration.
type Config struct {
	// K parameter controls the width of the DAG (anticone size limit)
	// In DAGKnight, this is adaptive; here we use a fixed or provided value
	K int

	// MaxParents is the maximum number of parents a block can reference
	MaxParents int

	// PruningDepth is how deep to keep the DAG before pruning
	PruningDepth uint64
}

// DefaultConfig returns default GHOSTDAG configuration.
func DefaultConfig() *Config {
	return &Config{
		K:            18, // Default K for ~10 parents
		MaxParents:   10,
		PruningDepth: 100000,
	}
}

// NewGHOSTDAGEngine creates a new GHOSTDAG engine.
func NewGHOSTDAGEngine(store *dag.Store, config *Config) *GHOSTDAGEngine {
	if config == nil {
		config = DefaultConfig()
	}
	return &GHOSTDAGEngine{
		store:               store,
		config:              config,
		blueScoreCache:      make(map[dag.Hash]uint64),
		selectedParentCache: make(map[dag.Hash]dag.Hash),
	}
}

// ProcessVertex processes a new vertex and determines its blue/red coloring.
func (g *GHOSTDAGEngine) ProcessVertex(v *dag.Vertex, adaptiveK float64) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Step 1: Select the parent with highest blue score (selected parent)
	selectedParent := g.selectParent(v)
	v.SelectedParent = selectedParent

	// Step 2: Compute blue set for this block
	blueSet := g.computeBlueSet(v, adaptiveK)

	// Step 3: Determine if this block is blue
	v.IsBlue = g.isBlue(v, blueSet, adaptiveK)

	// Step 4: Compute blue score (cumulative)
	v.BlueScore = g.computeBlueScore(v, blueSet)

	// Step 5: Compute blue work (stake-weighted)
	v.BlueWork = g.computeBlueWork(v)

	// Cache results
	g.blueScoreCache[v.Hash] = v.BlueScore
	g.selectedParentCache[v.Hash] = selectedParent

	return nil
}

// selectParent selects the parent with the highest blue score.
// This becomes the "selected parent" or main chain parent.
func (g *GHOSTDAGEngine) selectParent(v *dag.Vertex) dag.Hash {
	if len(v.Parents) == 0 {
		return dag.Hash{} // Genesis
	}

	var bestParent dag.Hash
	var bestScore uint64
	var bestWork *big.Int

	for _, parentHash := range v.Parents {
		parent := g.store.Get(parentHash)
		if parent == nil {
			continue
		}

		// Primary: highest blue score
		// Tiebreaker: highest blue work (stake-weighted)
		if parent.BlueScore > bestScore ||
			(parent.BlueScore == bestScore && parent.BlueWork != nil &&
			 (bestWork == nil || parent.BlueWork.Cmp(bestWork) > 0)) {
			bestScore = parent.BlueScore
			bestWork = parent.BlueWork
			bestParent = parentHash
		}
	}

	return bestParent
}

// computeBlueSet determines which blocks in the anticone are blue.
func (g *GHOSTDAGEngine) computeBlueSet(v *dag.Vertex, k float64) []dag.Hash {
	if len(v.Parents) == 0 {
		return nil // Genesis has no blue set to compute
	}

	// Get the blue set from selected parent
	selectedParent := g.store.Get(v.SelectedParent)
	if selectedParent == nil {
		return nil
	}

	// Start with the blue set inherited from selected parent
	blueSet := make(map[dag.Hash]bool)
	g.collectBlueAncestors(selectedParent.Hash, blueSet)

	// Add the selected parent itself
	if selectedParent.IsBlue {
		blueSet[selectedParent.Hash] = true
	}

	// Consider other parents - they're blue if their anticone wrt current blue set is small
	for _, parentHash := range v.Parents {
		if parentHash == v.SelectedParent {
			continue
		}

		parent := g.store.Get(parentHash)
		if parent == nil {
			continue
		}

		// Check if adding this parent keeps anticone size under k
		anticoneSize := g.anticoneSize(parentHash, blueSet)
		if float64(anticoneSize) <= k {
			blueSet[parentHash] = true
		}
	}

	// Convert to slice
	result := make([]dag.Hash, 0, len(blueSet))
	for hash := range blueSet {
		result = append(result, hash)
	}

	return result
}

// collectBlueAncestors collects all blue ancestors of a vertex.
func (g *GHOSTDAGEngine) collectBlueAncestors(hash dag.Hash, blueSet map[dag.Hash]bool) {
	v := g.store.Get(hash)
	if v == nil {
		return
	}

	for _, parentHash := range v.Parents {
		parent := g.store.Get(parentHash)
		if parent == nil {
			continue
		}

		if parent.IsBlue && !blueSet[parentHash] {
			blueSet[parentHash] = true
			g.collectBlueAncestors(parentHash, blueSet)
		}
	}
}

// anticoneSize computes the anticone size of a vertex relative to a blue set.
func (g *GHOSTDAGEngine) anticoneSize(hash dag.Hash, blueSet map[dag.Hash]bool) int {
	// Anticone = blocks that are neither ancestors nor descendants
	// For GHOSTDAG, we count blue blocks in the anticone

	v := g.store.Get(hash)
	if v == nil {
		return 0
	}

	// Collect ancestors
	ancestors := make(map[dag.Hash]bool)
	g.collectAncestors(hash, ancestors)

	// Count blue blocks not in ancestors and not descendants
	count := 0
	for blueHash := range blueSet {
		if ancestors[blueHash] {
			continue // Ancestor, not in anticone
		}
		if hash == blueHash {
			continue // Self
		}

		// Check if blueHash is a descendant of hash
		if g.isDescendant(blueHash, hash) {
			continue
		}

		count++
	}

	return count
}

// collectAncestors collects all ancestors of a vertex.
func (g *GHOSTDAGEngine) collectAncestors(hash dag.Hash, ancestors map[dag.Hash]bool) {
	v := g.store.Get(hash)
	if v == nil {
		return
	}

	for _, parentHash := range v.Parents {
		if !ancestors[parentHash] {
			ancestors[parentHash] = true
			g.collectAncestors(parentHash, ancestors)
		}
	}
}

// isDescendant checks if 'potential' is a descendant of 'of'.
func (g *GHOSTDAGEngine) isDescendant(potential, of dag.Hash) bool {
	children := g.store.GetChildren(of)
	for _, child := range children {
		if child.Hash == potential {
			return true
		}
		if g.isDescendant(potential, child.Hash) {
			return true
		}
	}
	return false
}

// isBlue determines if a vertex should be colored blue.
func (g *GHOSTDAGEngine) isBlue(v *dag.Vertex, blueSet []dag.Hash, k float64) bool {
	if v.IsGenesis() {
		return true // Genesis is always blue
	}

	// A block is blue if its anticone (relative to the blue set) is at most k
	blueMap := make(map[dag.Hash]bool)
	for _, h := range blueSet {
		blueMap[h] = true
	}

	anticone := g.anticoneSize(v.Hash, blueMap)
	return float64(anticone) <= k
}

// computeBlueScore calculates the cumulative blue score.
func (g *GHOSTDAGEngine) computeBlueScore(v *dag.Vertex, blueSet []dag.Hash) uint64 {
	if v.IsGenesis() {
		if v.IsBlue {
			return 1
		}
		return 0
	}

	// Blue score = selected parent's blue score + count of new blue blocks
	selectedParent := g.store.Get(v.SelectedParent)
	if selectedParent == nil {
		return uint64(len(blueSet))
	}

	baseScore := selectedParent.BlueScore

	// Add 1 for each blue block in the merge set (including self if blue)
	additionalBlue := uint64(0)
	for _, parentHash := range v.Parents {
		parent := g.store.Get(parentHash)
		if parent != nil && parent.IsBlue {
			if parentHash != v.SelectedParent {
				additionalBlue++
			}
		}
	}

	if v.IsBlue {
		additionalBlue++
	}

	return baseScore + additionalBlue
}

// computeBlueWork calculates stake-weighted cumulative work.
func (g *GHOSTDAGEngine) computeBlueWork(v *dag.Vertex) *big.Int {
	work := big.NewInt(0)

	// Get parent's work
	selectedParent := g.store.Get(v.SelectedParent)
	if selectedParent != nil && selectedParent.BlueWork != nil {
		work.Set(selectedParent.BlueWork)
	}

	// Add this block's stake if blue
	if v.IsBlue {
		work.Add(work, big.NewInt(int64(v.Stake)))
	}

	return work
}

// GetBlueScore returns the blue score for a vertex.
func (g *GHOSTDAGEngine) GetBlueScore(hash dag.Hash) uint64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if score, ok := g.blueScoreCache[hash]; ok {
		return score
	}

	v := g.store.Get(hash)
	if v == nil {
		return 0
	}
	return v.BlueScore
}

// GetSelectedParent returns the selected parent for a vertex.
func (g *GHOSTDAGEngine) GetSelectedParent(hash dag.Hash) dag.Hash {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if parent, ok := g.selectedParentCache[hash]; ok {
		return parent
	}

	v := g.store.Get(hash)
	if v == nil {
		return dag.Hash{}
	}
	return v.SelectedParent
}

// GetMainChain returns the main chain (selected parent chain) from a tip.
func (g *GHOSTDAGEngine) GetMainChain(tipHash dag.Hash) []*dag.Vertex {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var chain []*dag.Vertex
	current := tipHash

	for {
		v := g.store.Get(current)
		if v == nil {
			break
		}

		chain = append(chain, v)

		if v.IsGenesis() {
			break
		}

		current = v.SelectedParent
	}

	// Reverse to get genesis-first order
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	return chain
}

// GetBlueBlocks returns all blue blocks in the DAG.
func (g *GHOSTDAGEngine) GetBlueBlocks() []*dag.Vertex {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var blues []*dag.Vertex
	for _, v := range g.store.GetAll() {
		if v.IsBlue {
			blues = append(blues, v)
		}
	}

	// Sort by blue score
	sort.Slice(blues, func(i, j int) bool {
		return blues[i].BlueScore < blues[j].BlueScore
	})

	return blues
}

// GetRedBlocks returns all red blocks in the DAG.
func (g *GHOSTDAGEngine) GetRedBlocks() []*dag.Vertex {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var reds []*dag.Vertex
	for _, v := range g.store.GetAll() {
		if !v.IsBlue {
			reds = append(reds, v)
		}
	}

	return reds
}

// Statistics returns GHOSTDAG statistics.
type Statistics struct {
	TotalBlocks  uint64
	BlueBlocks   uint64
	RedBlocks    uint64
	MaxBlueScore uint64
	TotalWork    *big.Int
}

// GetStatistics returns current GHOSTDAG statistics.
func (g *GHOSTDAGEngine) GetStatistics() *Statistics {
	g.mu.RLock()
	defer g.mu.RUnlock()

	stats := &Statistics{
		TotalWork: big.NewInt(0),
	}

	for _, v := range g.store.GetAll() {
		stats.TotalBlocks++
		if v.IsBlue {
			stats.BlueBlocks++
		} else {
			stats.RedBlocks++
		}
		if v.BlueScore > stats.MaxBlueScore {
			stats.MaxBlueScore = v.BlueScore
		}
		if v.BlueWork != nil && v.BlueWork.Cmp(stats.TotalWork) > 0 {
			stats.TotalWork.Set(v.BlueWork)
		}
	}

	return stats
}
