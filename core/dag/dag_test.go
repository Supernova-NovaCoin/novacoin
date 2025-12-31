package dag

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewVertex(t *testing.T) {
	var pubKey PublicKey
	copy(pubKey[:], []byte("test-validator-public-key-48bytes"))

	parents := []Hash{{1, 2, 3}, {4, 5, 6}}
	v := NewVertex(parents, pubKey, 0, 1000)

	assert.Equal(t, parents, v.Parents)
	assert.Equal(t, pubKey, v.ValidatorPubKey)
	assert.Equal(t, uint32(0), v.ValidatorIndex)
	assert.Equal(t, uint64(1000), v.Stake)
	assert.True(t, v.IsAnchor) // All validators are anchors in Shoal++
	assert.NotNil(t, v.BlueWork)
}

func TestVertexIsGenesis(t *testing.T) {
	var pubKey PublicKey

	// Genesis has no parents
	genesis := NewVertex(nil, pubKey, 0, 1000)
	assert.True(t, genesis.IsGenesis())

	// Non-genesis has parents
	nonGenesis := NewVertex([]Hash{{1, 2, 3}}, pubKey, 0, 1000)
	assert.False(t, nonGenesis.IsGenesis())
}

func TestVertexClone(t *testing.T) {
	var pubKey PublicKey
	copy(pubKey[:], []byte("test-validator"))

	original := NewVertex([]Hash{{1, 2, 3}}, pubKey, 0, 1000)
	original.Wave = 5
	original.Round = 10
	original.IsBlue = true
	original.Transactions = [][]byte{{1, 2, 3}, {4, 5, 6}}

	clone := original.Clone()

	// Verify fields are copied
	assert.Equal(t, original.Parents, clone.Parents)
	assert.Equal(t, original.Wave, clone.Wave)
	assert.Equal(t, original.Round, clone.Round)
	assert.Equal(t, original.IsBlue, clone.IsBlue)
	assert.Equal(t, original.Transactions, clone.Transactions)

	// Verify deep copy (modifications don't affect original)
	clone.Parents[0] = Hash{9, 9, 9}
	assert.NotEqual(t, original.Parents[0], clone.Parents[0])

	clone.Transactions[0][0] = 99
	assert.NotEqual(t, original.Transactions[0][0], clone.Transactions[0][0])
}

func TestHashMethods(t *testing.T) {
	h := Hash{0x12, 0x34, 0x56, 0x78}

	assert.False(t, h.IsEmpty())
	assert.Equal(t, "12345678", h.Short())
	assert.Len(t, h.Bytes(), 32)

	empty := EmptyHash()
	assert.True(t, empty.IsEmpty())
}

func TestStoreAddAndGet(t *testing.T) {
	store := NewStore()
	var pubKey PublicKey

	// Create genesis
	genesis := NewVertex(nil, pubKey, 0, 1000)
	genesis.Hash = Hash{1}
	genesis.Height = 0

	err := store.Add(genesis)
	require.NoError(t, err)

	// Retrieve genesis
	retrieved := store.Get(genesis.Hash)
	require.NotNil(t, retrieved)
	assert.Equal(t, genesis.Hash, retrieved.Hash)

	// Check existence
	assert.True(t, store.Has(genesis.Hash))
	assert.False(t, store.Has(Hash{99}))
}

func TestStoreParentValidation(t *testing.T) {
	store := NewStore()
	var pubKey PublicKey

	// Add genesis
	genesis := NewVertex(nil, pubKey, 0, 1000)
	genesis.Hash = Hash{1}
	require.NoError(t, store.Add(genesis))

	// Add child with valid parent
	child := NewVertex([]Hash{{1}}, pubKey, 0, 1000)
	child.Hash = Hash{2}
	child.Height = 1
	require.NoError(t, store.Add(child))

	// Try to add vertex with non-existent parent
	orphan := NewVertex([]Hash{{99}}, pubKey, 0, 1000)
	orphan.Hash = Hash{3}
	err := store.Add(orphan)
	assert.Equal(t, ErrVertexNotFound, err)
}

func TestStoreDuplicateDetection(t *testing.T) {
	store := NewStore()
	var pubKey PublicKey

	genesis := NewVertex(nil, pubKey, 0, 1000)
	genesis.Hash = Hash{1}

	require.NoError(t, store.Add(genesis))
	err := store.Add(genesis)
	assert.Equal(t, ErrVertexExists, err)
}

func TestStoreTips(t *testing.T) {
	store := NewStore()
	var pubKey PublicKey

	// Genesis is initially a tip
	genesis := NewVertex(nil, pubKey, 0, 1000)
	genesis.Hash = Hash{1}
	require.NoError(t, store.Add(genesis))

	tips := store.GetTips()
	require.Len(t, tips, 1)
	assert.Equal(t, genesis.Hash, tips[0].Hash)

	// Add child - genesis is no longer a tip
	child := NewVertex([]Hash{{1}}, pubKey, 0, 1000)
	child.Hash = Hash{2}
	child.Height = 1
	require.NoError(t, store.Add(child))

	tips = store.GetTips()
	require.Len(t, tips, 1)
	assert.Equal(t, child.Hash, tips[0].Hash)

	// Add another child of genesis - now we have 2 tips
	child2 := NewVertex([]Hash{{1}}, pubKey, 1, 1000)
	child2.Hash = Hash{3}
	child2.Height = 1
	require.NoError(t, store.Add(child2))

	tips = store.GetTips()
	assert.Len(t, tips, 2)
}

func TestStoreIndices(t *testing.T) {
	store := NewStore()

	var pubKey1, pubKey2 PublicKey
	copy(pubKey1[:], []byte("validator1"))
	copy(pubKey2[:], []byte("validator2"))

	// Add genesis
	genesis := NewVertex(nil, pubKey1, 0, 1000)
	genesis.Hash = Hash{1}
	genesis.Height = 0
	genesis.Wave = 0
	genesis.Round = 0
	require.NoError(t, store.Add(genesis))

	// Add vertices from different validators
	v1 := NewVertex([]Hash{{1}}, pubKey1, 0, 1000)
	v1.Hash = Hash{2}
	v1.Height = 1
	v1.Wave = 1
	v1.Round = 1
	require.NoError(t, store.Add(v1))

	v2 := NewVertex([]Hash{{1}}, pubKey2, 1, 2000)
	v2.Hash = Hash{3}
	v2.Height = 1
	v2.Wave = 1
	v2.Round = 1
	require.NoError(t, store.Add(v2))

	// Test by height
	heightOne := store.GetByHeight(1)
	assert.Len(t, heightOne, 2)

	// Test by validator
	validator1Vertices := store.GetByValidator(pubKey1)
	assert.Len(t, validator1Vertices, 2) // genesis + v1

	validator2Vertices := store.GetByValidator(pubKey2)
	assert.Len(t, validator2Vertices, 1) // v2 only

	// Test by wave
	waveOne := store.GetByWave(1)
	assert.Len(t, waveOne, 2)

	// Test by round
	roundOne := store.GetByRound(1)
	assert.Len(t, roundOne, 2)
}

func TestStoreStatistics(t *testing.T) {
	store := NewStore()
	var pubKey PublicKey

	assert.Equal(t, uint64(0), store.Size())
	assert.Equal(t, uint64(0), store.MaxHeight())
	assert.Equal(t, 0, store.TipCount())

	// Add genesis
	genesis := NewVertex(nil, pubKey, 0, 1000)
	genesis.Hash = Hash{1}
	genesis.Height = 0
	require.NoError(t, store.Add(genesis))

	assert.Equal(t, uint64(1), store.Size())
	assert.Equal(t, uint64(0), store.MaxHeight())
	assert.Equal(t, 1, store.TipCount())

	// Add child
	child := NewVertex([]Hash{{1}}, pubKey, 0, 1000)
	child.Hash = Hash{2}
	child.Height = 5
	require.NoError(t, store.Add(child))

	assert.Equal(t, uint64(2), store.Size())
	assert.Equal(t, uint64(5), store.MaxHeight())
}

func TestTraversalAncestors(t *testing.T) {
	store := NewStore()
	var pubKey PublicKey

	// Build a simple chain: genesis -> v1 -> v2 -> v3
	genesis := NewVertex(nil, pubKey, 0, 1000)
	genesis.Hash = Hash{1}
	require.NoError(t, store.Add(genesis))

	v1 := NewVertex([]Hash{{1}}, pubKey, 0, 1000)
	v1.Hash = Hash{2}
	v1.Height = 1
	require.NoError(t, store.Add(v1))

	v2 := NewVertex([]Hash{{2}}, pubKey, 0, 1000)
	v2.Hash = Hash{3}
	v2.Height = 2
	require.NoError(t, store.Add(v2))

	v3 := NewVertex([]Hash{{3}}, pubKey, 0, 1000)
	v3.Hash = Hash{4}
	v3.Height = 3
	require.NoError(t, store.Add(v3))

	traversal := NewTraversal(store)

	// All ancestors
	ancestors := traversal.Ancestors(Hash{4}, 0)
	assert.Len(t, ancestors, 3)

	// Limited depth
	ancestors = traversal.Ancestors(Hash{4}, 2)
	assert.Len(t, ancestors, 2)
}

func TestTraversalDescendants(t *testing.T) {
	store := NewStore()
	var pubKey PublicKey

	// Build a chain
	genesis := NewVertex(nil, pubKey, 0, 1000)
	genesis.Hash = Hash{1}
	require.NoError(t, store.Add(genesis))

	v1 := NewVertex([]Hash{{1}}, pubKey, 0, 1000)
	v1.Hash = Hash{2}
	v1.Height = 1
	require.NoError(t, store.Add(v1))

	v2 := NewVertex([]Hash{{2}}, pubKey, 0, 1000)
	v2.Hash = Hash{3}
	v2.Height = 2
	require.NoError(t, store.Add(v2))

	traversal := NewTraversal(store)

	descendants := traversal.Descendants(Hash{1}, 0)
	assert.Len(t, descendants, 2)
}

func TestTraversalIsAncestor(t *testing.T) {
	store := NewStore()
	var pubKey PublicKey

	genesis := NewVertex(nil, pubKey, 0, 1000)
	genesis.Hash = Hash{1}
	require.NoError(t, store.Add(genesis))

	v1 := NewVertex([]Hash{{1}}, pubKey, 0, 1000)
	v1.Hash = Hash{2}
	require.NoError(t, store.Add(v1))

	v2 := NewVertex([]Hash{{2}}, pubKey, 0, 1000)
	v2.Hash = Hash{3}
	require.NoError(t, store.Add(v2))

	traversal := NewTraversal(store)

	assert.True(t, traversal.IsAncestor(Hash{3}, Hash{1}))
	assert.True(t, traversal.IsAncestor(Hash{3}, Hash{2}))
	assert.False(t, traversal.IsAncestor(Hash{1}, Hash{3}))
	assert.False(t, traversal.IsAncestor(Hash{1}, Hash{1})) // Not ancestor of itself
}

func TestTraversalComputeHeight(t *testing.T) {
	store := NewStore()
	var pubKey PublicKey

	genesis := NewVertex(nil, pubKey, 0, 1000)
	genesis.Hash = Hash{1}
	genesis.Height = 0
	require.NoError(t, store.Add(genesis))

	v1 := NewVertex([]Hash{{1}}, pubKey, 0, 1000)
	v1.Hash = Hash{2}
	v1.Height = 1
	require.NoError(t, store.Add(v1))

	v2 := NewVertex([]Hash{{1}}, pubKey, 0, 1000)
	v2.Hash = Hash{3}
	v2.Height = 1
	require.NoError(t, store.Add(v2))

	traversal := NewTraversal(store)

	// New vertex with both v1 and v2 as parents
	newHeight := traversal.ComputeHeight([]Hash{{2}, {3}})
	assert.Equal(t, uint64(2), newHeight)
}

func TestVertexFinality(t *testing.T) {
	var pubKey PublicKey
	v := NewVertex(nil, pubKey, 0, 1000)

	assert.False(t, v.IsFinalized())
	assert.False(t, v.IsCommitted())

	v.FinalizedAt = time.Now()
	assert.True(t, v.IsFinalized())

	v.CommitRound = 5
	assert.True(t, v.IsCommitted())
}

func TestStoreIterator(t *testing.T) {
	store := NewStore()
	var pubKey PublicKey

	// Add vertices at different heights
	for i := 0; i < 5; i++ {
		var parents []Hash
		if i > 0 {
			parents = []Hash{{byte(i)}}
		}
		v := NewVertex(parents, pubKey, 0, 1000)
		v.Hash = Hash{byte(i + 1)}
		v.Height = uint64(i)
		require.NoError(t, store.Add(v))
	}

	// Iterate
	it := store.NewIterator()
	var heights []uint64
	for it.Next() {
		heights = append(heights, it.Vertex().Height)
	}

	// Should be in height order
	assert.Equal(t, []uint64{0, 1, 2, 3, 4}, heights)
}
