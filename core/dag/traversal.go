// Package dag provides DAG traversal utilities.
package dag

import (
	"sort"
)

// TraversalOrder defines the order for DAG traversal.
type TraversalOrder int

const (
	// TopologicalOrder traverses from genesis to tips.
	TopologicalOrder TraversalOrder = iota
	// ReverseTopologicalOrder traverses from tips to genesis.
	ReverseTopologicalOrder
	// BreadthFirst traverses level by level.
	BreadthFirst
	// DepthFirst traverses branch by branch.
	DepthFirst
)

// Traversal provides DAG traversal utilities.
type Traversal struct {
	store *Store
}

// NewTraversal creates a new traversal utility.
func NewTraversal(store *Store) *Traversal {
	return &Traversal{store: store}
}

// Ancestors returns all ancestors of a vertex up to the given depth.
// If depth is 0, returns all ancestors up to genesis.
func (t *Traversal) Ancestors(hash Hash, depth int) []*Vertex {
	visited := make(map[Hash]bool)
	var result []*Vertex

	t.ancestorsHelper(hash, depth, 0, visited, &result)

	return result
}

func (t *Traversal) ancestorsHelper(hash Hash, maxDepth, currentDepth int, visited map[Hash]bool, result *[]*Vertex) {
	if visited[hash] {
		return
	}
	if maxDepth > 0 && currentDepth >= maxDepth {
		return
	}

	v := t.store.Get(hash)
	if v == nil {
		return
	}

	visited[hash] = true

	for _, parentHash := range v.Parents {
		parent := t.store.Get(parentHash)
		if parent != nil && !visited[parentHash] {
			*result = append(*result, parent)
			t.ancestorsHelper(parentHash, maxDepth, currentDepth+1, visited, result)
		}
	}
}

// Descendants returns all descendants of a vertex up to the given depth.
// If depth is 0, returns all descendants up to tips.
func (t *Traversal) Descendants(hash Hash, depth int) []*Vertex {
	visited := make(map[Hash]bool)
	var result []*Vertex

	t.descendantsHelper(hash, depth, 0, visited, &result)

	return result
}

func (t *Traversal) descendantsHelper(hash Hash, maxDepth, currentDepth int, visited map[Hash]bool, result *[]*Vertex) {
	if visited[hash] {
		return
	}
	if maxDepth > 0 && currentDepth >= maxDepth {
		return
	}

	visited[hash] = true

	children := t.store.GetChildren(hash)
	for _, child := range children {
		if !visited[child.Hash] {
			*result = append(*result, child)
			t.descendantsHelper(child.Hash, maxDepth, currentDepth+1, visited, result)
		}
	}
}

// TopologicalSort returns vertices in topological order (parents before children).
func (t *Traversal) TopologicalSort() []*Vertex {
	vertices := t.store.GetAll()
	if len(vertices) == 0 {
		return vertices
	}

	// Build in-degree map
	inDegree := make(map[Hash]int)
	for _, v := range vertices {
		if _, exists := inDegree[v.Hash]; !exists {
			inDegree[v.Hash] = 0
		}
		for _, parentHash := range v.Parents {
			inDegree[v.Hash]++
			_ = parentHash // Ensure parent is counted
		}
	}

	// Find all vertices with no dependencies (genesis and orphans)
	var queue []*Vertex
	for _, v := range vertices {
		if inDegree[v.Hash] == 0 {
			queue = append(queue, v)
		}
	}

	// Process queue
	var result []*Vertex
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		result = append(result, v)

		// Reduce in-degree for children
		children := t.store.GetChildren(v.Hash)
		for _, child := range children {
			inDegree[child.Hash]--
			if inDegree[child.Hash] == 0 {
				queue = append(queue, child)
			}
		}
	}

	return result
}

// PathTo finds a path from source to target.
// Returns nil if no path exists.
func (t *Traversal) PathTo(source, target Hash) []*Vertex {
	if source == target {
		v := t.store.Get(source)
		if v != nil {
			return []*Vertex{v}
		}
		return nil
	}

	// BFS to find shortest path
	visited := make(map[Hash]bool)
	parent := make(map[Hash]Hash)
	queue := []Hash{source}
	visited[source] = true

	found := false
	for len(queue) > 0 && !found {
		current := queue[0]
		queue = queue[1:]

		children := t.store.GetChildren(current)
		for _, child := range children {
			if !visited[child.Hash] {
				visited[child.Hash] = true
				parent[child.Hash] = current
				queue = append(queue, child.Hash)

				if child.Hash == target {
					found = true
					break
				}
			}
		}
	}

	if !found {
		return nil
	}

	// Reconstruct path
	var path []*Vertex
	current := target
	for current != source {
		v := t.store.Get(current)
		if v != nil {
			path = append([]*Vertex{v}, path...)
		}
		current = parent[current]
	}
	if v := t.store.Get(source); v != nil {
		path = append([]*Vertex{v}, path...)
	}

	return path
}

// CommonAncestors finds all common ancestors of two vertices.
func (t *Traversal) CommonAncestors(hash1, hash2 Hash) []*Vertex {
	ancestors1 := make(map[Hash]bool)
	t.collectAllAncestors(hash1, ancestors1)
	ancestors1[hash1] = true

	var common []*Vertex
	visited := make(map[Hash]bool)
	t.findCommonAncestors(hash2, ancestors1, visited, &common)

	if ancestors1[hash2] {
		if v := t.store.Get(hash2); v != nil {
			common = append(common, v)
		}
	}

	return common
}

func (t *Traversal) collectAllAncestors(hash Hash, ancestors map[Hash]bool) {
	v := t.store.Get(hash)
	if v == nil {
		return
	}

	for _, parentHash := range v.Parents {
		if !ancestors[parentHash] {
			ancestors[parentHash] = true
			t.collectAllAncestors(parentHash, ancestors)
		}
	}
}

func (t *Traversal) findCommonAncestors(hash Hash, ancestors1 map[Hash]bool, visited map[Hash]bool, common *[]*Vertex) {
	if visited[hash] {
		return
	}
	visited[hash] = true

	v := t.store.Get(hash)
	if v == nil {
		return
	}

	for _, parentHash := range v.Parents {
		if ancestors1[parentHash] {
			if parent := t.store.Get(parentHash); parent != nil {
				*common = append(*common, parent)
			}
		}
		t.findCommonAncestors(parentHash, ancestors1, visited, common)
	}
}

// LowestCommonAncestor finds the lowest common ancestor of two vertices.
// Returns nil if no common ancestor exists.
func (t *Traversal) LowestCommonAncestor(hash1, hash2 Hash) *Vertex {
	common := t.CommonAncestors(hash1, hash2)
	if len(common) == 0 {
		return nil
	}

	// Find the one with highest height (lowest in the DAG means highest height)
	sort.Slice(common, func(i, j int) bool {
		return common[i].Height > common[j].Height
	})

	return common[0]
}

// IsAncestor checks if potentialAncestor is an ancestor of vertex.
func (t *Traversal) IsAncestor(vertex, potentialAncestor Hash) bool {
	if vertex == potentialAncestor {
		return false
	}

	visited := make(map[Hash]bool)
	return t.isAncestorHelper(vertex, potentialAncestor, visited)
}

func (t *Traversal) isAncestorHelper(current, target Hash, visited map[Hash]bool) bool {
	if visited[current] {
		return false
	}
	visited[current] = true

	v := t.store.Get(current)
	if v == nil {
		return false
	}

	for _, parentHash := range v.Parents {
		if parentHash == target {
			return true
		}
		if t.isAncestorHelper(parentHash, target, visited) {
			return true
		}
	}

	return false
}

// IsDescendant checks if potentialDescendant is a descendant of vertex.
func (t *Traversal) IsDescendant(vertex, potentialDescendant Hash) bool {
	return t.IsAncestor(potentialDescendant, vertex)
}

// AnticoneOf returns the anticone of a vertex (vertices that are neither ancestors nor descendants).
func (t *Traversal) AnticoneOf(hash Hash) []*Vertex {
	// Get ancestors and descendants
	ancestors := make(map[Hash]bool)
	t.collectAllAncestors(hash, ancestors)
	ancestors[hash] = true

	descendants := make(map[Hash]bool)
	for _, v := range t.Descendants(hash, 0) {
		descendants[v.Hash] = true
	}
	descendants[hash] = true

	// Anticone = all vertices - ancestors - descendants
	var anticone []*Vertex
	for _, v := range t.store.GetAll() {
		if !ancestors[v.Hash] && !descendants[v.Hash] {
			anticone = append(anticone, v)
		}
	}

	return anticone
}

// PastOf returns all ancestors of a vertex (the "past" in DAG terminology).
func (t *Traversal) PastOf(hash Hash) []*Vertex {
	return t.Ancestors(hash, 0)
}

// FutureOf returns all descendants of a vertex (the "future" in DAG terminology).
func (t *Traversal) FutureOf(hash Hash) []*Vertex {
	return t.Descendants(hash, 0)
}

// GetHeight computes the height of a vertex (max parent height + 1).
func (t *Traversal) GetHeight(hash Hash) uint64 {
	v := t.store.Get(hash)
	if v == nil {
		return 0
	}
	return v.Height
}

// ComputeHeight computes the height for a new vertex based on its parents.
func (t *Traversal) ComputeHeight(parents []Hash) uint64 {
	if len(parents) == 0 {
		return 0
	}

	var maxHeight uint64
	for _, parentHash := range parents {
		parent := t.store.Get(parentHash)
		if parent != nil && parent.Height >= maxHeight {
			maxHeight = parent.Height + 1
		}
	}

	return maxHeight
}

// BlueAnticone returns vertices in the anticone that are in the blue set.
func (t *Traversal) BlueAnticone(hash Hash) []*Vertex {
	anticone := t.AnticoneOf(hash)
	var blue []*Vertex
	for _, v := range anticone {
		if v.IsBlue {
			blue = append(blue, v)
		}
	}
	return blue
}
