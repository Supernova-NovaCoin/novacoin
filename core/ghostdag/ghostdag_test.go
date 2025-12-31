package ghostdag

import (
	"math/big"
	"testing"

	"github.com/novacoin/novacoin/core/dag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestVertex(hash dag.Hash, parents []dag.Hash, height uint64, stake uint64) *dag.Vertex {
	var pubKey dag.PublicKey
	v := dag.NewVertex(parents, pubKey, 0, stake)
	v.Hash = hash
	v.Height = height
	v.BlueWork = big.NewInt(0)
	return v
}

func TestNewGHOSTDAGEngine(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)

	assert.NotNil(t, engine)
	assert.NotNil(t, engine.config)
	assert.Equal(t, 18, engine.config.K)
	assert.Equal(t, 10, engine.config.MaxParents)
}

func TestProcessGenesisVertex(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)

	// Create genesis
	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))

	// Process genesis
	err := engine.ProcessVertex(genesis, 0.33)
	require.NoError(t, err)

	assert.True(t, genesis.IsBlue)
	assert.Equal(t, uint64(1), genesis.BlueScore)
	assert.True(t, genesis.SelectedParent.IsEmpty())
}

func TestProcessLinearChain(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)

	// Create linear chain: genesis -> v1 -> v2 -> v3
	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))
	require.NoError(t, engine.ProcessVertex(genesis, 0.33))

	v1 := createTestVertex(dag.Hash{2}, []dag.Hash{{1}}, 1, 1000)
	require.NoError(t, store.Add(v1))
	require.NoError(t, engine.ProcessVertex(v1, 0.33))

	v2 := createTestVertex(dag.Hash{3}, []dag.Hash{{2}}, 2, 1000)
	require.NoError(t, store.Add(v2))
	require.NoError(t, engine.ProcessVertex(v2, 0.33))

	v3 := createTestVertex(dag.Hash{4}, []dag.Hash{{3}}, 3, 1000)
	require.NoError(t, store.Add(v3))
	require.NoError(t, engine.ProcessVertex(v3, 0.33))

	// All blocks should be blue in a linear chain
	assert.True(t, genesis.IsBlue)
	assert.True(t, v1.IsBlue)
	assert.True(t, v2.IsBlue)
	assert.True(t, v3.IsBlue)

	// Blue scores should increase
	assert.Equal(t, uint64(1), genesis.BlueScore)
	assert.True(t, v1.BlueScore > genesis.BlueScore)
	assert.True(t, v2.BlueScore > v1.BlueScore)
	assert.True(t, v3.BlueScore > v2.BlueScore)

	// Selected parents should form chain
	assert.Equal(t, dag.Hash{1}, v1.SelectedParent)
	assert.Equal(t, dag.Hash{2}, v2.SelectedParent)
	assert.Equal(t, dag.Hash{3}, v3.SelectedParent)
}

func TestProcessDAGWithMerge(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)

	// Create DAG with merge:
	//     genesis
	//     /    \
	//    v1    v2
	//     \    /
	//       v3

	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))
	require.NoError(t, engine.ProcessVertex(genesis, 10.0)) // High K for testing

	v1 := createTestVertex(dag.Hash{2}, []dag.Hash{{1}}, 1, 1000)
	require.NoError(t, store.Add(v1))
	require.NoError(t, engine.ProcessVertex(v1, 10.0))

	v2 := createTestVertex(dag.Hash{3}, []dag.Hash{{1}}, 1, 1000)
	require.NoError(t, store.Add(v2))
	require.NoError(t, engine.ProcessVertex(v2, 10.0))

	v3 := createTestVertex(dag.Hash{4}, []dag.Hash{{2}, {3}}, 2, 1000)
	require.NoError(t, store.Add(v3))
	require.NoError(t, engine.ProcessVertex(v3, 10.0))

	// All should be blue with high K
	assert.True(t, genesis.IsBlue)
	assert.True(t, v1.IsBlue)
	assert.True(t, v2.IsBlue)
	assert.True(t, v3.IsBlue)

	// v3 merges both branches
	assert.Len(t, v3.Parents, 2)
}

func TestGetMainChain(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)

	// Create chain
	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))
	require.NoError(t, engine.ProcessVertex(genesis, 0.33))

	v1 := createTestVertex(dag.Hash{2}, []dag.Hash{{1}}, 1, 1000)
	require.NoError(t, store.Add(v1))
	require.NoError(t, engine.ProcessVertex(v1, 0.33))

	v2 := createTestVertex(dag.Hash{3}, []dag.Hash{{2}}, 2, 1000)
	require.NoError(t, store.Add(v2))
	require.NoError(t, engine.ProcessVertex(v2, 0.33))

	// Get main chain from tip
	chain := engine.GetMainChain(dag.Hash{3})

	require.Len(t, chain, 3)
	assert.Equal(t, dag.Hash{1}, chain[0].Hash)
	assert.Equal(t, dag.Hash{2}, chain[1].Hash)
	assert.Equal(t, dag.Hash{3}, chain[2].Hash)
}

func TestGetBlueScore(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)

	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))
	require.NoError(t, engine.ProcessVertex(genesis, 0.33))

	v1 := createTestVertex(dag.Hash{2}, []dag.Hash{{1}}, 1, 1000)
	require.NoError(t, store.Add(v1))
	require.NoError(t, engine.ProcessVertex(v1, 0.33))

	// Test cached blue score
	assert.Equal(t, genesis.BlueScore, engine.GetBlueScore(dag.Hash{1}))
	assert.Equal(t, v1.BlueScore, engine.GetBlueScore(dag.Hash{2}))

	// Non-existent vertex
	assert.Equal(t, uint64(0), engine.GetBlueScore(dag.Hash{99}))
}

func TestGetStatistics(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)

	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))
	require.NoError(t, engine.ProcessVertex(genesis, 0.33))

	v1 := createTestVertex(dag.Hash{2}, []dag.Hash{{1}}, 1, 1000)
	require.NoError(t, store.Add(v1))
	require.NoError(t, engine.ProcessVertex(v1, 0.33))

	stats := engine.GetStatistics()

	assert.Equal(t, uint64(2), stats.TotalBlocks)
	assert.Equal(t, uint64(2), stats.BlueBlocks)
	assert.Equal(t, uint64(0), stats.RedBlocks)
	assert.True(t, stats.MaxBlueScore > 0)
}

func TestColoring(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)
	coloring := NewColoring(engine)

	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))
	require.NoError(t, engine.ProcessVertex(genesis, 0.33))

	v1 := createTestVertex(dag.Hash{2}, []dag.Hash{{1}}, 1, 1000)
	require.NoError(t, store.Add(v1))

	result := coloring.ColorVertex(v1, 10.0)

	assert.NotNil(t, result)
	assert.True(t, result.IsBlue)
	assert.Equal(t, dag.Hash{1}, result.SelectedParent)
}

func TestColorDistribution(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)
	coloring := NewColoring(engine)

	// Add some blocks
	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))
	require.NoError(t, engine.ProcessVertex(genesis, 10.0))

	v1 := createTestVertex(dag.Hash{2}, []dag.Hash{{1}}, 1, 1000)
	require.NoError(t, store.Add(v1))
	require.NoError(t, engine.ProcessVertex(v1, 10.0))

	dist := coloring.GetColorDistribution()

	assert.Equal(t, 2, dist.Total)
	assert.Equal(t, 2, dist.Blue)
	assert.Equal(t, 0, dist.Red)
	assert.Equal(t, 1.0, dist.BlueRatio)
}

func TestOrdering(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)
	ordering := NewOrdering(engine)

	// Create linear chain
	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))
	require.NoError(t, engine.ProcessVertex(genesis, 0.33))

	v1 := createTestVertex(dag.Hash{2}, []dag.Hash{{1}}, 1, 1000)
	require.NoError(t, store.Add(v1))
	require.NoError(t, engine.ProcessVertex(v1, 0.33))

	v2 := createTestVertex(dag.Hash{3}, []dag.Hash{{2}}, 2, 1000)
	require.NoError(t, store.Add(v2))
	require.NoError(t, engine.ProcessVertex(v2, 0.33))

	// Get order from tip
	order := ordering.GetOrder(dag.Hash{3})

	require.Len(t, order, 3)
	// Should be in order from genesis to tip
	assert.Equal(t, dag.Hash{1}, order[0].Hash)
}

func TestGetBlueOrder(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)
	ordering := NewOrdering(engine)

	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))
	require.NoError(t, engine.ProcessVertex(genesis, 0.33))

	v1 := createTestVertex(dag.Hash{2}, []dag.Hash{{1}}, 1, 1000)
	require.NoError(t, store.Add(v1))
	require.NoError(t, engine.ProcessVertex(v1, 0.33))

	blueOrder := ordering.GetBlueOrder(dag.Hash{2})

	// All should be blue in linear chain
	assert.Len(t, blueOrder, 2)
	for _, v := range blueOrder {
		assert.True(t, v.IsBlue)
	}
}

func TestConfirmationScore(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)
	ordering := NewOrdering(engine)

	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))
	require.NoError(t, engine.ProcessVertex(genesis, 0.33))

	v1 := createTestVertex(dag.Hash{2}, []dag.Hash{{1}}, 1, 1000)
	require.NoError(t, store.Add(v1))
	require.NoError(t, engine.ProcessVertex(v1, 0.33))

	v2 := createTestVertex(dag.Hash{3}, []dag.Hash{{2}}, 2, 1000)
	require.NoError(t, store.Add(v2))
	require.NoError(t, engine.ProcessVertex(v2, 0.33))

	// Genesis should have highest confirmation score
	genesisScore := ordering.GetConfirmationScore(dag.Hash{1})
	v1Score := ordering.GetConfirmationScore(dag.Hash{2})
	v2Score := ordering.GetConfirmationScore(dag.Hash{3})

	assert.True(t, genesisScore > v1Score)
	assert.True(t, v1Score > v2Score)
	assert.Equal(t, uint64(0), v2Score) // Tip has 0 confirmations
}

func TestIsInMainChain(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)
	ordering := NewOrdering(engine)

	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))
	require.NoError(t, engine.ProcessVertex(genesis, 0.33))

	v1 := createTestVertex(dag.Hash{2}, []dag.Hash{{1}}, 1, 1000)
	require.NoError(t, store.Add(v1))
	require.NoError(t, engine.ProcessVertex(v1, 0.33))

	// Both should be in main chain
	assert.True(t, ordering.IsInMainChain(dag.Hash{1}, dag.Hash{2}))
	assert.True(t, ordering.IsInMainChain(dag.Hash{2}, dag.Hash{2}))

	// Non-existent should not be
	assert.False(t, ordering.IsInMainChain(dag.Hash{99}, dag.Hash{2}))
}

func TestBlueWork(t *testing.T) {
	store := dag.NewStore()
	engine := NewGHOSTDAGEngine(store, nil)

	// Create chain with different stakes
	genesis := createTestVertex(dag.Hash{1}, nil, 0, 1000)
	require.NoError(t, store.Add(genesis))
	require.NoError(t, engine.ProcessVertex(genesis, 0.33))

	v1 := createTestVertex(dag.Hash{2}, []dag.Hash{{1}}, 1, 2000)
	require.NoError(t, store.Add(v1))
	require.NoError(t, engine.ProcessVertex(v1, 0.33))

	v2 := createTestVertex(dag.Hash{3}, []dag.Hash{{2}}, 2, 3000)
	require.NoError(t, store.Add(v2))
	require.NoError(t, engine.ProcessVertex(v2, 0.33))

	// Blue work should accumulate
	assert.Equal(t, int64(1000), genesis.BlueWork.Int64())
	assert.Equal(t, int64(3000), v1.BlueWork.Int64()) // 1000 + 2000
	assert.Equal(t, int64(6000), v2.BlueWork.Int64()) // 1000 + 2000 + 3000
}
