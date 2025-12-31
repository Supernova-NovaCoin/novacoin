// Package ghostdag implements topological ordering for DAG blocks.
package ghostdag

import (
	"sort"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// Ordering provides topological ordering of DAG blocks.
type Ordering struct {
	engine *GHOSTDAGEngine
}

// NewOrdering creates a new ordering instance.
func NewOrdering(engine *GHOSTDAGEngine) *Ordering {
	return &Ordering{engine: engine}
}

// GetOrder returns blocks in GHOSTDAG order.
// This is the canonical ordering used for transaction execution.
func (o *Ordering) GetOrder(tipHash dag.Hash) []*dag.Vertex {
	// Start from tip and walk back to genesis via selected parents
	mainChain := o.getMainChain(tipHash)

	if len(mainChain) == 0 {
		return nil
	}

	// For each block in main chain, collect its merge set in order
	var ordered []*dag.Vertex
	visited := make(map[dag.Hash]bool)

	for _, block := range mainChain {
		// First, add blocks from merge set (in blue-score order)
		mergeSetOrdered := o.orderMergeSet(block, visited)
		ordered = append(ordered, mergeSetOrdered...)

		// Then add the block itself
		if !visited[block.Hash] {
			ordered = append(ordered, block)
			visited[block.Hash] = true
		}
	}

	return ordered
}

// getMainChain returns the main chain (selected parent chain) from tip to genesis.
func (o *Ordering) getMainChain(tipHash dag.Hash) []*dag.Vertex {
	var chain []*dag.Vertex
	current := tipHash

	for {
		v := o.engine.store.Get(current)
		if v == nil {
			break
		}

		chain = append([]*dag.Vertex{v}, chain...)

		if v.IsGenesis() {
			break
		}

		current = v.SelectedParent
	}

	return chain
}

// orderMergeSet orders the merge set of a block.
func (o *Ordering) orderMergeSet(block *dag.Vertex, visited map[dag.Hash]bool) []*dag.Vertex {
	// Merge set = parents (except selected parent) and their unvisited ancestors
	mergeSet := make(map[dag.Hash]*dag.Vertex)

	for _, parentHash := range block.Parents {
		if parentHash == block.SelectedParent {
			continue
		}
		if visited[parentHash] {
			continue
		}

		parent := o.engine.store.Get(parentHash)
		if parent != nil {
			mergeSet[parentHash] = parent
			o.collectUnvisitedAncestors(parentHash, visited, mergeSet)
		}
	}

	// Convert to slice and sort by blue score (higher first), then by hash for determinism
	var blocks []*dag.Vertex
	for _, v := range mergeSet {
		blocks = append(blocks, v)
	}

	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].BlueScore != blocks[j].BlueScore {
			return blocks[i].BlueScore > blocks[j].BlueScore
		}
		// Tiebreaker: lexicographic hash comparison
		for k := 0; k < 32; k++ {
			if blocks[i].Hash[k] != blocks[j].Hash[k] {
				return blocks[i].Hash[k] < blocks[j].Hash[k]
			}
		}
		return false
	})

	// Mark as visited
	for _, b := range blocks {
		visited[b.Hash] = true
	}

	return blocks
}

// collectUnvisitedAncestors collects ancestors not yet visited.
func (o *Ordering) collectUnvisitedAncestors(hash dag.Hash, visited map[dag.Hash]bool, mergeSet map[dag.Hash]*dag.Vertex) {
	v := o.engine.store.Get(hash)
	if v == nil {
		return
	}

	for _, parentHash := range v.Parents {
		if visited[parentHash] || mergeSet[parentHash] != nil {
			continue
		}

		parent := o.engine.store.Get(parentHash)
		if parent != nil {
			mergeSet[parentHash] = parent
			o.collectUnvisitedAncestors(parentHash, visited, mergeSet)
		}
	}
}

// GetBlueOrder returns only blue blocks in order.
func (o *Ordering) GetBlueOrder(tipHash dag.Hash) []*dag.Vertex {
	allOrdered := o.GetOrder(tipHash)

	var blueOrdered []*dag.Vertex
	for _, v := range allOrdered {
		if v.IsBlue {
			blueOrdered = append(blueOrdered, v)
		}
	}

	return blueOrdered
}

// GetTransactionOrder returns the order in which transactions should be executed.
// This includes transactions from blue blocks only (red block transactions are ignored).
func (o *Ordering) GetTransactionOrder(tipHash dag.Hash) [][]byte {
	blueBlocks := o.GetBlueOrder(tipHash)

	var txs [][]byte
	for _, block := range blueBlocks {
		txs = append(txs, block.Transactions...)
	}

	return txs
}

// GetConfirmationScore returns how "confirmed" a block is.
// Higher score = more confirmations = more confidence.
func (o *Ordering) GetConfirmationScore(blockHash dag.Hash) uint64 {
	block := o.engine.store.Get(blockHash)
	if block == nil {
		return 0
	}

	// Find the tip with highest blue score
	tips := o.engine.store.GetTips()
	if len(tips) == 0 {
		return 0
	}

	var maxTipScore uint64
	for _, tip := range tips {
		if tip.BlueScore > maxTipScore {
			maxTipScore = tip.BlueScore
		}
	}

	// Confirmation score = tip's blue score - block's blue score
	// This represents how many blue blocks have been built on top
	if maxTipScore >= block.BlueScore {
		return maxTipScore - block.BlueScore
	}

	return 0
}

// IsInMainChain checks if a block is in the main chain.
func (o *Ordering) IsInMainChain(blockHash, tipHash dag.Hash) bool {
	current := tipHash

	for {
		if current == blockHash {
			return true
		}

		v := o.engine.store.Get(current)
		if v == nil || v.IsGenesis() {
			break
		}

		current = v.SelectedParent
	}

	return current == blockHash
}

// GetChainDiff returns blocks that differ between two chain tips.
// Returns (blocksOnlyInA, blocksOnlyInB, commonAncestor)
func (o *Ordering) GetChainDiff(tipA, tipB dag.Hash) ([]dag.Hash, []dag.Hash, dag.Hash) {
	// Walk both chains back until we find common ancestor
	chainA := make(map[dag.Hash]bool)
	chainB := make(map[dag.Hash]bool)

	currentA := tipA
	currentB := tipB

	for {
		if currentA == currentB {
			return o.getChainUpTo(tipA, currentA), o.getChainUpTo(tipB, currentB), currentA
		}

		vA := o.engine.store.Get(currentA)
		vB := o.engine.store.Get(currentB)

		if vA == nil && vB == nil {
			break
		}

		if vA != nil {
			chainA[currentA] = true
			if chainB[currentA] {
				return o.getChainUpTo(tipA, currentA), o.getChainUpTo(tipB, currentA), currentA
			}
			if !vA.IsGenesis() {
				currentA = vA.SelectedParent
			}
		}

		if vB != nil {
			chainB[currentB] = true
			if chainA[currentB] {
				return o.getChainUpTo(tipA, currentB), o.getChainUpTo(tipB, currentB), currentB
			}
			if !vB.IsGenesis() {
				currentB = vB.SelectedParent
			}
		}
	}

	return nil, nil, dag.Hash{}
}

// getChainUpTo returns blocks from tip down to (but not including) ancestor.
func (o *Ordering) getChainUpTo(tip, ancestor dag.Hash) []dag.Hash {
	var chain []dag.Hash
	current := tip

	for current != ancestor {
		chain = append(chain, current)
		v := o.engine.store.Get(current)
		if v == nil || v.IsGenesis() {
			break
		}
		current = v.SelectedParent
	}

	return chain
}

// ReorgDepth calculates the reorganization depth if switching from one tip to another.
func (o *Ordering) ReorgDepth(currentTip, newTip dag.Hash) uint64 {
	blocksToRevert, _, _ := o.GetChainDiff(currentTip, newTip)
	return uint64(len(blocksToRevert))
}
