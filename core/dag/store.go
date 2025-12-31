// Package dag provides the core DAG data structures and storage.
package dag

import (
	"errors"
	"sync"
)

var (
	// ErrVertexNotFound is returned when a vertex is not in the store.
	ErrVertexNotFound = errors.New("vertex not found")

	// ErrVertexExists is returned when trying to add a duplicate vertex.
	ErrVertexExists = errors.New("vertex already exists")

	// ErrInvalidVertex is returned when a vertex is malformed.
	ErrInvalidVertex = errors.New("invalid vertex")
)

// Store provides storage and indexing for DAG vertices.
type Store struct {
	// Primary storage: hash -> vertex
	vertices map[Hash]*Vertex

	// Indices for efficient lookups
	byHeight     map[uint64][]Hash          // height -> vertex hashes
	byValidator  map[PublicKey][]Hash       // validator pubkey -> vertex hashes
	byWave       map[uint64][]Hash          // wave -> vertex hashes
	byRound      map[uint64][]Hash          // round -> vertex hashes
	children     map[Hash][]Hash            // parent hash -> child hashes

	// Tips tracking (vertices with no children)
	tips map[Hash]struct{}

	// Genesis
	genesis *Vertex

	// Statistics
	totalVertices uint64
	maxHeight     uint64

	mu sync.RWMutex
}

// NewStore creates a new DAG store.
func NewStore() *Store {
	return &Store{
		vertices:    make(map[Hash]*Vertex),
		byHeight:    make(map[uint64][]Hash),
		byValidator: make(map[PublicKey][]Hash),
		byWave:      make(map[uint64][]Hash),
		byRound:     make(map[uint64][]Hash),
		children:    make(map[Hash][]Hash),
		tips:        make(map[Hash]struct{}),
	}
}

// Add stores a vertex in the DAG.
func (s *Store) Add(v *Vertex) error {
	if v == nil {
		return ErrInvalidVertex
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicates
	if _, exists := s.vertices[v.Hash]; exists {
		return ErrVertexExists
	}

	// Verify parents exist (except for genesis)
	if !v.IsGenesis() {
		for _, parentHash := range v.Parents {
			if _, exists := s.vertices[parentHash]; !exists {
				return ErrVertexNotFound
			}
		}
	}

	// Store vertex
	s.vertices[v.Hash] = v

	// Update indices
	s.byHeight[v.Height] = append(s.byHeight[v.Height], v.Hash)
	s.byValidator[v.ValidatorPubKey] = append(s.byValidator[v.ValidatorPubKey], v.Hash)
	s.byWave[v.Wave] = append(s.byWave[v.Wave], v.Hash)
	s.byRound[v.Round] = append(s.byRound[v.Round], v.Hash)

	// Update children index and tips
	for _, parentHash := range v.Parents {
		s.children[parentHash] = append(s.children[parentHash], v.Hash)
		// Parent is no longer a tip
		delete(s.tips, parentHash)
	}

	// New vertex is a tip (until it has children)
	s.tips[v.Hash] = struct{}{}

	// Update statistics
	s.totalVertices++
	if v.Height > s.maxHeight {
		s.maxHeight = v.Height
	}

	// Track genesis
	if v.IsGenesis() {
		s.genesis = v
	}

	return nil
}

// Get retrieves a vertex by hash.
func (s *Store) Get(hash Hash) *Vertex {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.vertices[hash]
}

// Has checks if a vertex exists in the store.
func (s *Store) Has(hash Hash) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.vertices[hash]
	return exists
}

// GetByHeight returns all vertices at a given height.
func (s *Store) GetByHeight(height uint64) []*Vertex {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hashes := s.byHeight[height]
	vertices := make([]*Vertex, 0, len(hashes))
	for _, h := range hashes {
		if v := s.vertices[h]; v != nil {
			vertices = append(vertices, v)
		}
	}
	return vertices
}

// GetByValidator returns all vertices created by a validator.
func (s *Store) GetByValidator(pk PublicKey) []*Vertex {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hashes := s.byValidator[pk]
	vertices := make([]*Vertex, 0, len(hashes))
	for _, h := range hashes {
		if v := s.vertices[h]; v != nil {
			vertices = append(vertices, v)
		}
	}
	return vertices
}

// GetByWave returns all vertices in a given wave.
func (s *Store) GetByWave(wave uint64) []*Vertex {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hashes := s.byWave[wave]
	vertices := make([]*Vertex, 0, len(hashes))
	for _, h := range hashes {
		if v := s.vertices[h]; v != nil {
			vertices = append(vertices, v)
		}
	}
	return vertices
}

// GetByRound returns all vertices in a given round.
func (s *Store) GetByRound(round uint64) []*Vertex {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hashes := s.byRound[round]
	vertices := make([]*Vertex, 0, len(hashes))
	for _, h := range hashes {
		if v := s.vertices[h]; v != nil {
			vertices = append(vertices, v)
		}
	}
	return vertices
}

// GetChildren returns all vertices that reference the given vertex as a parent.
func (s *Store) GetChildren(hash Hash) []*Vertex {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hashes := s.children[hash]
	vertices := make([]*Vertex, 0, len(hashes))
	for _, h := range hashes {
		if v := s.vertices[h]; v != nil {
			vertices = append(vertices, v)
		}
	}
	return vertices
}

// GetParents returns all parent vertices of the given vertex.
func (s *Store) GetParents(hash Hash) []*Vertex {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v := s.vertices[hash]
	if v == nil {
		return nil
	}

	parents := make([]*Vertex, 0, len(v.Parents))
	for _, parentHash := range v.Parents {
		if parent := s.vertices[parentHash]; parent != nil {
			parents = append(parents, parent)
		}
	}
	return parents
}

// GetTips returns all current DAG tips (vertices with no children).
func (s *Store) GetTips() []*Vertex {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tips := make([]*Vertex, 0, len(s.tips))
	for hash := range s.tips {
		if v := s.vertices[hash]; v != nil {
			tips = append(tips, v)
		}
	}
	return tips
}

// GetTipHashes returns the hashes of all current tips.
func (s *Store) GetTipHashes() []Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tips := make([]Hash, 0, len(s.tips))
	for hash := range s.tips {
		tips = append(tips, hash)
	}
	return tips
}

// GetGenesis returns the genesis vertex.
func (s *Store) GetGenesis() *Vertex {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.genesis
}

// Size returns the total number of vertices in the store.
func (s *Store) Size() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalVertices
}

// MaxHeight returns the maximum height in the DAG.
func (s *Store) MaxHeight() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxHeight
}

// TipCount returns the number of current tips.
func (s *Store) TipCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tips)
}

// GetAll returns all vertices in the store.
func (s *Store) GetAll() []*Vertex {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vertices := make([]*Vertex, 0, len(s.vertices))
	for _, v := range s.vertices {
		vertices = append(vertices, v)
	}
	return vertices
}

// GetHashesByHeight returns vertex hashes at a given height.
func (s *Store) GetHashesByHeight(height uint64) []Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hashes := s.byHeight[height]
	result := make([]Hash, len(hashes))
	copy(result, hashes)
	return result
}

// Iterator provides iteration over vertices in height order.
type Iterator struct {
	store   *Store
	height  uint64
	index   int
	current *Vertex
}

// NewIterator creates an iterator starting from the genesis.
func (s *Store) NewIterator() *Iterator {
	return &Iterator{
		store:  s,
		height: 0,
		index:  -1,
	}
}

// Next advances to the next vertex.
func (it *Iterator) Next() bool {
	it.store.mu.RLock()
	defer it.store.mu.RUnlock()

	for it.height <= it.store.maxHeight {
		hashes := it.store.byHeight[it.height]
		it.index++

		if it.index < len(hashes) {
			it.current = it.store.vertices[hashes[it.index]]
			return true
		}

		it.height++
		it.index = -1
	}

	it.current = nil
	return false
}

// Vertex returns the current vertex.
func (it *Iterator) Vertex() *Vertex {
	return it.current
}
