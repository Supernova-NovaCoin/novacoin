// Package ghostdag implements DAG coloring algorithms.
package ghostdag

import (
	"sort"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// ColoringResult contains the result of coloring a vertex.
type ColoringResult struct {
	Vertex         *dag.Vertex
	IsBlue         bool
	BlueScore      uint64
	SelectedParent dag.Hash
	BlueSet        []dag.Hash
	RedSet         []dag.Hash
	MergeSet       []dag.Hash
}

// Coloring handles the blue/red coloring of DAG vertices.
type Coloring struct {
	engine *GHOSTDAGEngine
}

// NewColoring creates a new coloring instance.
func NewColoring(engine *GHOSTDAGEngine) *Coloring {
	return &Coloring{engine: engine}
}

// ColorVertex performs full coloring analysis on a vertex.
func (c *Coloring) ColorVertex(v *dag.Vertex, k float64) *ColoringResult {
	result := &ColoringResult{
		Vertex: v,
	}

	if v.IsGenesis() {
		result.IsBlue = true
		result.BlueScore = 1
		return result
	}

	// Get merge set (all blocks being merged by this vertex)
	result.MergeSet = c.computeMergeSet(v)

	// Separate into blue and red
	result.BlueSet, result.RedSet = c.partitionByColor(result.MergeSet, k)

	// Select parent with highest blue score
	result.SelectedParent = c.selectBestParent(v)

	// Determine if this vertex is blue
	result.IsBlue = c.determineColor(v, result.BlueSet, k)

	// Compute blue score
	result.BlueScore = c.computeBlueScoreFromResult(v, result)

	return result
}

// computeMergeSet computes the set of blocks being merged by this vertex.
// The merge set includes all blocks in the past of v that are not in the past
// of the selected parent (the "new" blocks being added to the main chain).
func (c *Coloring) computeMergeSet(v *dag.Vertex) []dag.Hash {
	if len(v.Parents) <= 1 {
		// No merge happening - single parent or genesis
		return v.Parents
	}

	// Find selected parent's past
	selectedParent := c.selectBestParent(v)
	selectedPast := make(map[dag.Hash]bool)
	c.collectPast(selectedParent, selectedPast)

	// Merge set = parents and their ancestors not in selected parent's past
	mergeSet := make(map[dag.Hash]bool)
	for _, parentHash := range v.Parents {
		if parentHash == selectedParent {
			continue
		}
		if !selectedPast[parentHash] {
			mergeSet[parentHash] = true
		}
		// Include ancestors not in selected past
		c.collectMergeAncestors(parentHash, selectedPast, mergeSet)
	}

	result := make([]dag.Hash, 0, len(mergeSet))
	for hash := range mergeSet {
		result = append(result, hash)
	}

	return result
}

// collectPast collects all ancestors of a vertex.
func (c *Coloring) collectPast(hash dag.Hash, past map[dag.Hash]bool) {
	if past[hash] {
		return
	}
	past[hash] = true

	v := c.engine.store.Get(hash)
	if v == nil {
		return
	}

	for _, parentHash := range v.Parents {
		c.collectPast(parentHash, past)
	}
}

// collectMergeAncestors collects ancestors that should be in the merge set.
func (c *Coloring) collectMergeAncestors(hash dag.Hash, selectedPast, mergeSet map[dag.Hash]bool) {
	v := c.engine.store.Get(hash)
	if v == nil {
		return
	}

	for _, parentHash := range v.Parents {
		if selectedPast[parentHash] || mergeSet[parentHash] {
			continue
		}
		mergeSet[parentHash] = true
		c.collectMergeAncestors(parentHash, selectedPast, mergeSet)
	}
}

// partitionByColor separates the merge set into blue and red blocks.
func (c *Coloring) partitionByColor(mergeSet []dag.Hash, k float64) (blue, red []dag.Hash) {
	// Sort by blue score (blocks with higher scores are more likely blue)
	sorted := make([]dag.Hash, len(mergeSet))
	copy(sorted, mergeSet)

	sort.Slice(sorted, func(i, j int) bool {
		vi := c.engine.store.Get(sorted[i])
		vj := c.engine.store.Get(sorted[j])
		if vi == nil || vj == nil {
			return false
		}
		return vi.BlueScore > vj.BlueScore
	})

	// Greedily add blocks to blue set while anticone constraint is satisfied
	blueSet := make(map[dag.Hash]bool)

	for _, hash := range sorted {
		v := c.engine.store.Get(hash)
		if v == nil {
			continue
		}

		// Check if adding this block violates the anticone constraint
		anticoneCount := c.countAnticoneBlue(hash, blueSet)
		if float64(anticoneCount) <= k {
			blueSet[hash] = true
			blue = append(blue, hash)
		} else {
			red = append(red, hash)
		}
	}

	return blue, red
}

// countAnticoneBlue counts blue blocks in the anticone of a vertex.
func (c *Coloring) countAnticoneBlue(hash dag.Hash, blueSet map[dag.Hash]bool) int {
	v := c.engine.store.Get(hash)
	if v == nil {
		return 0
	}

	// Collect past of this vertex
	past := make(map[dag.Hash]bool)
	c.collectPast(hash, past)

	// Collect future of this vertex
	future := make(map[dag.Hash]bool)
	c.collectFuture(hash, future)

	// Count blue blocks not in past or future
	count := 0
	for blueHash := range blueSet {
		if !past[blueHash] && !future[blueHash] && blueHash != hash {
			count++
		}
	}

	return count
}

// collectFuture collects all descendants of a vertex.
func (c *Coloring) collectFuture(hash dag.Hash, future map[dag.Hash]bool) {
	children := c.engine.store.GetChildren(hash)
	for _, child := range children {
		if !future[child.Hash] {
			future[child.Hash] = true
			c.collectFuture(child.Hash, future)
		}
	}
}

// selectBestParent selects the parent with highest blue score.
func (c *Coloring) selectBestParent(v *dag.Vertex) dag.Hash {
	if len(v.Parents) == 0 {
		return dag.Hash{}
	}
	if len(v.Parents) == 1 {
		return v.Parents[0]
	}

	var best dag.Hash
	var bestScore uint64

	for _, parentHash := range v.Parents {
		parent := c.engine.store.Get(parentHash)
		if parent == nil {
			continue
		}

		if parent.BlueScore >= bestScore {
			bestScore = parent.BlueScore
			best = parentHash
		}
	}

	return best
}

// determineColor determines if a vertex should be blue.
func (c *Coloring) determineColor(v *dag.Vertex, blueSet []dag.Hash, k float64) bool {
	if v.IsGenesis() {
		return true
	}

	blueMap := make(map[dag.Hash]bool)
	for _, h := range blueSet {
		blueMap[h] = true
	}

	// Count anticone relative to current blue set
	anticone := c.countAnticoneBlue(v.Hash, blueMap)

	return float64(anticone) <= k
}

// computeBlueScoreFromResult computes blue score from coloring result.
func (c *Coloring) computeBlueScoreFromResult(v *dag.Vertex, result *ColoringResult) uint64 {
	if v.IsGenesis() {
		return 1
	}

	// Start with selected parent's score
	selectedParent := c.engine.store.Get(result.SelectedParent)
	if selectedParent == nil {
		return uint64(len(result.BlueSet))
	}

	score := selectedParent.BlueScore

	// Add blue blocks from merge set
	score += uint64(len(result.BlueSet))

	// Add self if blue
	if result.IsBlue {
		score++
	}

	return score
}

// RecolorDAG re-colors the entire DAG from genesis.
// This is useful after K parameter changes.
func (c *Coloring) RecolorDAG(k float64) {
	// Get all vertices in topological order
	vertices := c.engine.store.GetAll()

	// Sort by height
	sort.Slice(vertices, func(i, j int) bool {
		return vertices[i].Height < vertices[j].Height
	})

	// Re-color each vertex
	for _, v := range vertices {
		result := c.ColorVertex(v, k)
		v.IsBlue = result.IsBlue
		v.BlueScore = result.BlueScore
		v.SelectedParent = result.SelectedParent
	}
}

// GetColorDistribution returns the distribution of blue vs red blocks.
type ColorDistribution struct {
	Total     int
	Blue      int
	Red       int
	BlueRatio float64
}

// GetColorDistribution returns color distribution statistics.
func (c *Coloring) GetColorDistribution() *ColorDistribution {
	vertices := c.engine.store.GetAll()

	dist := &ColorDistribution{
		Total: len(vertices),
	}

	for _, v := range vertices {
		if v.IsBlue {
			dist.Blue++
		} else {
			dist.Red++
		}
	}

	if dist.Total > 0 {
		dist.BlueRatio = float64(dist.Blue) / float64(dist.Total)
	}

	return dist
}
