// Package finality implements tests for the finality engine.
package finality

import (
	"testing"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
	"github.com/Supernova-NovaCoin/novacoin/core/mysticeti"
)

// === Test Helpers ===

// mockVertexStore is a mock implementation of VertexStore.
type mockVertexStore struct {
	vertices map[dag.Hash]*dag.Vertex
	byHeight map[uint64][]*dag.Vertex
	children map[dag.Hash][]*dag.Vertex
}

func newMockVertexStore() *mockVertexStore {
	return &mockVertexStore{
		vertices: make(map[dag.Hash]*dag.Vertex),
		byHeight: make(map[uint64][]*dag.Vertex),
		children: make(map[dag.Hash][]*dag.Vertex),
	}
}

func (m *mockVertexStore) Add(v *dag.Vertex) {
	m.vertices[v.Hash] = v
	m.byHeight[v.Height] = append(m.byHeight[v.Height], v)
	for _, parent := range v.Parents {
		m.children[parent] = append(m.children[parent], v)
	}
}

func (m *mockVertexStore) Get(hash dag.Hash) *dag.Vertex {
	return m.vertices[hash]
}

func (m *mockVertexStore) GetByHeight(height uint64) []*dag.Vertex {
	return m.byHeight[height]
}

func (m *mockVertexStore) GetChildren(hash dag.Hash) []*dag.Vertex {
	return m.children[hash]
}

// createTestVertex creates a test vertex.
func createTestVertex(hash dag.Hash, height uint64, parents []dag.Hash, validatorIndex uint32) *dag.Vertex {
	return &dag.Vertex{
		Hash:           hash,
		Height:         height,
		Parents:        parents,
		Timestamp:      time.Now(),
		ValidatorIndex: validatorIndex,
		Stake:          1000,
	}
}

// createTestCertificate creates a test commit certificate.
func createTestCertificate(blockHash dag.Hash, round uint64, depth uint8) *mysticeti.CommitCertificate {
	return &mysticeti.CommitCertificate{
		Block:             blockHash,
		Round:             round,
		Status:            mysticeti.CommitStandard,
		CommitTime:        time.Now(),
		CommitDepth:       depth,
		SupportingBlocks:  make(map[uint64][]dag.Hash),
		SupportingStake:   make(map[uint64]uint64),
		TotalSupportStake: 700,
		TotalStake:        1000,
	}
}

// === Config Tests ===

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("DefaultConfig returned nil")
	}

	if config.FinalityDepth != 3 {
		t.Errorf("FinalityDepth = %d, want 3", config.FinalityDepth)
	}

	threshold := 2.0 / 3.0
	if config.QuorumThreshold < threshold-0.01 || config.QuorumThreshold > threshold+0.01 {
		t.Errorf("QuorumThreshold = %f, want ~0.667", config.QuorumThreshold)
	}

	if config.MaxReorgDepth != 100 {
		t.Errorf("MaxReorgDepth = %d, want 100", config.MaxReorgDepth)
	}

	if !config.EnableReorgProtection {
		t.Error("EnableReorgProtection should be true")
	}
}

// === Engine Tests ===

func TestNewEngine(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	if engine == nil {
		t.Fatal("NewEngine returned nil")
	}

	if engine.config == nil {
		t.Error("config should not be nil")
	}

	if engine.frontier == nil {
		t.Error("frontier should not be nil")
	}

	if engine.reorgHandler == nil {
		t.Error("reorgHandler should not be nil")
	}
}

func TestFinalizeGenesis(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	genesis := &dag.Vertex{
		Hash:      dag.Hash{1, 2, 3, 4},
		Height:    0,
		Parents:   []dag.Hash{},
		Timestamp: time.Now(),
	}
	store.Add(genesis)

	err := engine.FinalizeGenesis(genesis)
	if err != nil {
		t.Fatalf("FinalizeGenesis failed: %v", err)
	}

	// Check finalized
	if !engine.IsFinalized(genesis.Hash) {
		t.Error("Genesis should be finalized")
	}

	// Check status
	status := engine.GetFinalityStatus(genesis.Hash)
	if status != StatusFinalized {
		t.Errorf("Status = %v, want StatusFinalized", status)
	}

	// Check proof
	proof := engine.GetFinalityProof(genesis.Hash)
	if proof == nil {
		t.Error("Genesis should have a proof")
	}
	if !proof.IsGenesis {
		t.Error("Genesis proof should have IsGenesis = true")
	}
}

func TestFinalizeGenesisNonGenesis(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	nonGenesis := &dag.Vertex{
		Hash:      dag.Hash{5, 6, 7, 8},
		Height:    1,
		Parents:   []dag.Hash{{1, 2, 3, 4}},
		Timestamp: time.Now(),
	}

	err := engine.FinalizeGenesis(nonGenesis)
	if err == nil {
		t.Error("FinalizeGenesis should fail for non-genesis block")
	}
}

func TestProcessCommitCertificate(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	// Create genesis
	genesis := &dag.Vertex{
		Hash:      dag.Hash{1, 2, 3, 4},
		Height:    0,
		Parents:   []dag.Hash{},
		Timestamp: time.Now(),
	}
	store.Add(genesis)
	engine.FinalizeGenesis(genesis)

	// Create block
	block := createTestVertex(dag.Hash{5, 6, 7, 8}, 1, []dag.Hash{genesis.Hash}, 0)
	store.Add(block)

	// Create certificate
	cert := createTestCertificate(block.Hash, 10, 3)

	// Process certificate
	finBlock, err := engine.ProcessCommitCertificate(cert)
	if err != nil {
		t.Fatalf("ProcessCommitCertificate failed: %v", err)
	}

	if finBlock == nil {
		t.Fatal("ProcessCommitCertificate returned nil")
	}

	// Check finalized
	if !engine.IsFinalized(block.Hash) {
		t.Error("Block should be finalized")
	}

	// Check proof
	proof := engine.GetFinalityProof(block.Hash)
	if proof == nil {
		t.Error("Block should have a proof")
	}
}

func TestProcessCommitCertificateMissingAncestor(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	// Create block without finalized parent
	block := createTestVertex(dag.Hash{5, 6, 7, 8}, 1, []dag.Hash{{1, 2, 3, 4}}, 0)
	store.Add(block)

	// Create certificate
	cert := createTestCertificate(block.Hash, 10, 3)

	// Process certificate - should queue for later
	finBlock, err := engine.ProcessCommitCertificate(cert)
	if err != nil {
		t.Fatalf("ProcessCommitCertificate failed: %v", err)
	}

	if finBlock != nil {
		t.Error("Block should not be finalized without ancestor")
	}

	// Check pending
	stats := engine.GetStatistics()
	if stats.PendingCommits != 1 {
		t.Errorf("PendingCommits = %d, want 1", stats.PendingCommits)
	}
}

func TestProcessCommitCertificateAlreadyFinalized(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	// Create and finalize genesis
	genesis := &dag.Vertex{
		Hash:      dag.Hash{1, 2, 3, 4},
		Height:    0,
		Parents:   []dag.Hash{},
		Timestamp: time.Now(),
	}
	store.Add(genesis)
	engine.FinalizeGenesis(genesis)

	// Try to finalize again
	cert := createTestCertificate(genesis.Hash, 10, 3)
	_, err := engine.ProcessCommitCertificate(cert)

	if err != ErrAlreadyFinalized {
		t.Errorf("Expected ErrAlreadyFinalized, got %v", err)
	}
}

// === Query Tests ===

func TestGetFinalizedBlock(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	// Create and finalize genesis
	genesis := &dag.Vertex{
		Hash:      dag.Hash{1, 2, 3, 4},
		Height:    0,
		Parents:   []dag.Hash{},
		Timestamp: time.Now(),
	}
	store.Add(genesis)
	engine.FinalizeGenesis(genesis)

	// Get finalized block
	finBlock := engine.GetFinalizedBlock(genesis.Hash)
	if finBlock == nil {
		t.Fatal("GetFinalizedBlock returned nil")
	}

	if finBlock.Hash != genesis.Hash {
		t.Error("Hash mismatch")
	}

	if finBlock.Height != 0 {
		t.Errorf("Height = %d, want 0", finBlock.Height)
	}
}

func TestGetFinalizedAtHeight(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	// Create and finalize genesis
	genesis := &dag.Vertex{
		Hash:      dag.Hash{1, 2, 3, 4},
		Height:    0,
		Parents:   []dag.Hash{},
		Timestamp: time.Now(),
	}
	store.Add(genesis)
	engine.FinalizeGenesis(genesis)

	// Get at height 0
	blocks := engine.GetFinalizedAtHeight(0)
	if len(blocks) != 1 {
		t.Errorf("len(blocks) = %d, want 1", len(blocks))
	}

	// Get at height 1 (empty)
	blocks = engine.GetFinalizedAtHeight(1)
	if len(blocks) != 0 {
		t.Errorf("len(blocks) = %d, want 0", len(blocks))
	}
}

func TestGetLastFinalizedBlock(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	// Initially nil
	last := engine.GetLastFinalizedBlock()
	if last != nil {
		t.Error("Expected nil initially")
	}

	// Create and finalize genesis
	genesis := &dag.Vertex{
		Hash:      dag.Hash{1, 2, 3, 4},
		Height:    0,
		Parents:   []dag.Hash{},
		Timestamp: time.Now(),
	}
	store.Add(genesis)
	engine.FinalizeGenesis(genesis)

	last = engine.GetLastFinalizedBlock()
	if last == nil {
		t.Fatal("GetLastFinalizedBlock returned nil")
	}
	if last.Hash != genesis.Hash {
		t.Error("Hash mismatch")
	}
}

func TestVerifyFinality(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	// Create and finalize genesis
	genesis := &dag.Vertex{
		Hash:      dag.Hash{1, 2, 3, 4},
		Height:    0,
		Parents:   []dag.Hash{},
		Timestamp: time.Now(),
	}
	store.Add(genesis)
	engine.FinalizeGenesis(genesis)

	// Verify genesis
	valid, err := engine.VerifyFinality(genesis.Hash)
	if err != nil {
		t.Fatalf("VerifyFinality failed: %v", err)
	}
	if !valid {
		t.Error("Genesis should be valid")
	}

	// Verify non-existent block
	_, err = engine.VerifyFinality(dag.Hash{9, 9, 9})
	if err != ErrNotFinalized {
		t.Errorf("Expected ErrNotFinalized, got %v", err)
	}
}

// === Callback Tests ===

func TestOnFinalizedCallback(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	callbackCalled := make(chan *FinalizedBlock, 1)
	engine.OnFinalized(func(fb *FinalizedBlock) {
		callbackCalled <- fb
	})

	// Create and finalize genesis
	genesis := &dag.Vertex{
		Hash:      dag.Hash{1, 2, 3, 4},
		Height:    0,
		Parents:   []dag.Hash{},
		Timestamp: time.Now(),
	}
	store.Add(genesis)
	engine.FinalizeGenesis(genesis)

	// Wait for callback
	select {
	case fb := <-callbackCalled:
		if fb.Hash != genesis.Hash {
			t.Error("Hash mismatch in callback")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Callback not called")
	}
}

// === Proof Tests ===

func TestFinalityProofSerialize(t *testing.T) {
	proof := NewFinalityProof(
		dag.Hash{1, 2, 3},
		100,
		50,
		3,
		700,
		1000,
		[]byte("signature"),
	)

	// Serialize
	data := proof.Serialize()
	if len(data) == 0 {
		t.Fatal("Serialize returned empty data")
	}

	// Deserialize
	restored, err := DeserializeFinalityProof(data)
	if err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}

	// Check fields
	if restored.BlockHash != proof.BlockHash {
		t.Error("BlockHash mismatch")
	}
	if restored.BlockHeight != proof.BlockHeight {
		t.Errorf("BlockHeight = %d, want %d", restored.BlockHeight, proof.BlockHeight)
	}
	if restored.CommitRound != proof.CommitRound {
		t.Errorf("CommitRound = %d, want %d", restored.CommitRound, proof.CommitRound)
	}
	if restored.CommitDepth != proof.CommitDepth {
		t.Errorf("CommitDepth = %d, want %d", restored.CommitDepth, proof.CommitDepth)
	}
	if restored.SupportingStake != proof.SupportingStake {
		t.Errorf("SupportingStake = %d, want %d", restored.SupportingStake, proof.SupportingStake)
	}
	if restored.TotalStake != proof.TotalStake {
		t.Errorf("TotalStake = %d, want %d", restored.TotalStake, proof.TotalStake)
	}
}

func TestFinalityProofVerify(t *testing.T) {
	proof := NewFinalityProof(
		dag.Hash{1, 2, 3},
		100,
		50,
		3,
		700,
		1000,
		[]byte("signature"),
	)

	// Verify
	valid, err := proof.Verify(2.0 / 3.0)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !valid {
		t.Error("Proof should be valid")
	}

	// Test insufficient stake
	proof.SupportingStake = 500
	valid, err = proof.Verify(2.0 / 3.0)
	if err == nil {
		t.Error("Expected error for insufficient stake")
	}
}

func TestFinalityProofGetters(t *testing.T) {
	proof := NewFinalityProof(
		dag.Hash{1, 2, 3},
		100,
		50,
		2,
		700,
		1000,
		nil,
	)

	// Test stake ratio
	ratio := proof.GetStakeRatio()
	if ratio < 0.69 || ratio > 0.71 {
		t.Errorf("StakeRatio = %f, want ~0.70", ratio)
	}

	// Test fast path
	if !proof.IsFastPath() {
		t.Error("Should be fast path")
	}

	proof.CommitDepth = 3
	if !proof.IsStandardPath() {
		t.Error("Should be standard path")
	}
}

func TestProofBatch(t *testing.T) {
	proofs := make([]*FinalityProof, 3)
	for i := 0; i < 3; i++ {
		proofs[i] = NewFinalityProof(
			dag.Hash{byte(i)},
			uint64(i),
			uint64(i * 10),
			3,
			700,
			1000,
			nil,
		)
	}

	batch := NewProofBatch(proofs)
	if batch.Root.IsEmpty() {
		t.Error("Batch root should not be empty")
	}

	// Verify batch
	valid, errs := batch.VerifyBatch(2.0 / 3.0)
	if !valid {
		t.Errorf("Batch verification failed: %v", errs)
	}

	// Serialize
	data := batch.Serialize()
	if len(data) == 0 {
		t.Fatal("Serialize returned empty data")
	}

	// Deserialize
	restored, err := DeserializeProofBatch(data)
	if err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}

	if len(restored.Proofs) != 3 {
		t.Errorf("len(Proofs) = %d, want 3", len(restored.Proofs))
	}
}

func TestLightProof(t *testing.T) {
	fullProof := NewFinalityProof(
		dag.Hash{1, 2, 3},
		100,
		50,
		3,
		700,
		1000,
		[]byte("sig"),
	)

	anchorHash := dag.Hash{9, 9, 9}
	lightProof := NewLightProof(fullProof, anchorHash)

	if lightProof.BlockHash != fullProof.BlockHash {
		t.Error("BlockHash mismatch")
	}

	// Serialize
	data := lightProof.Serialize()
	if len(data) == 0 {
		t.Fatal("Serialize returned empty data")
	}

	// Deserialize
	restored, err := DeserializeLightProof(data)
	if err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}

	if restored.BlockHeight != lightProof.BlockHeight {
		t.Error("BlockHeight mismatch")
	}
}

// === Frontier Tests ===

func TestFrontierNew(t *testing.T) {
	frontier := NewFrontier()

	if frontier == nil {
		t.Fatal("NewFrontier returned nil")
	}

	if frontier.Size() != 0 {
		t.Errorf("Size = %d, want 0", frontier.Size())
	}

	if frontier.ValidatorCount() != 0 {
		t.Errorf("ValidatorCount = %d, want 0", frontier.ValidatorCount())
	}
}

func TestFrontierUpdate(t *testing.T) {
	frontier := NewFrontier()

	vertex := &dag.Vertex{
		Hash:           dag.Hash{1, 2, 3},
		Height:         10,
		ValidatorIndex: 0,
		FinalizedAt:    time.Now(),
	}

	frontier.Update(vertex)

	if frontier.Size() != 1 {
		t.Errorf("Size = %d, want 1", frontier.Size())
	}

	if frontier.ValidatorCount() != 1 {
		t.Errorf("ValidatorCount = %d, want 1", frontier.ValidatorCount())
	}

	if frontier.MinHeight() != 10 {
		t.Errorf("MinHeight = %d, want 10", frontier.MinHeight())
	}

	if frontier.MaxHeight() != 10 {
		t.Errorf("MaxHeight = %d, want 10", frontier.MaxHeight())
	}
}

func TestFrontierContains(t *testing.T) {
	frontier := NewFrontier()

	hash := dag.Hash{1, 2, 3}
	vertex := &dag.Vertex{
		Hash:           hash,
		Height:         10,
		ValidatorIndex: 0,
		FinalizedAt:    time.Now(),
	}

	frontier.Update(vertex)

	if !frontier.Contains(hash) {
		t.Error("Frontier should contain hash")
	}

	if frontier.Contains(dag.Hash{9, 9, 9}) {
		t.Error("Frontier should not contain unknown hash")
	}
}

func TestFrontierGetByValidator(t *testing.T) {
	frontier := NewFrontier()

	vertex := &dag.Vertex{
		Hash:           dag.Hash{1, 2, 3},
		Height:         10,
		ValidatorIndex: 5,
		FinalizedAt:    time.Now(),
	}

	frontier.Update(vertex)

	entry := frontier.GetByValidator(5)
	if entry == nil {
		t.Fatal("GetByValidator returned nil")
	}

	if entry.Height != 10 {
		t.Errorf("Height = %d, want 10", entry.Height)
	}

	if entry := frontier.GetByValidator(99); entry != nil {
		t.Error("Should return nil for unknown validator")
	}
}

func TestFrontierPrune(t *testing.T) {
	frontier := NewFrontier()

	// Add multiple entries
	for i := 0; i < 5; i++ {
		vertex := &dag.Vertex{
			Hash:           dag.Hash{byte(i)},
			Height:         uint64(i * 10),
			ValidatorIndex: uint32(i),
			FinalizedAt:    time.Now(),
		}
		frontier.Update(vertex)
	}

	if frontier.Size() != 5 {
		t.Errorf("Size = %d, want 5", frontier.Size())
	}

	// Prune below height 20
	pruned := frontier.Prune(20)
	if pruned != 2 {
		t.Errorf("pruned = %d, want 2", pruned)
	}

	if frontier.Size() != 3 {
		t.Errorf("Size after prune = %d, want 3", frontier.Size())
	}
}

func TestFrontierClone(t *testing.T) {
	frontier := NewFrontier()

	vertex := &dag.Vertex{
		Hash:           dag.Hash{1, 2, 3},
		Height:         10,
		ValidatorIndex: 0,
		FinalizedAt:    time.Now(),
	}
	frontier.Update(vertex)

	clone := frontier.Clone()

	if clone.Size() != frontier.Size() {
		t.Error("Clone size mismatch")
	}

	// Modify original
	vertex2 := &dag.Vertex{
		Hash:           dag.Hash{4, 5, 6},
		Height:         20,
		ValidatorIndex: 0,
		FinalizedAt:    time.Now(),
	}
	frontier.Update(vertex2)

	// Clone should be unchanged
	if clone.MaxHeight() != 10 {
		t.Error("Clone should not be affected by original")
	}
}

func TestFrontierSnapshot(t *testing.T) {
	frontier := NewFrontier()

	for i := 0; i < 3; i++ {
		vertex := &dag.Vertex{
			Hash:           dag.Hash{byte(i)},
			Height:         uint64(i * 10),
			ValidatorIndex: uint32(i),
			FinalizedAt:    time.Now(),
		}
		frontier.Update(vertex)
	}

	// Create snapshot
	snapshot := frontier.Snapshot()
	if len(snapshot.Entries) != 3 {
		t.Errorf("Entries = %d, want 3", len(snapshot.Entries))
	}

	// Clear and restore
	frontier.Clear()
	if frontier.Size() != 0 {
		t.Error("Size should be 0 after Clear")
	}

	frontier.RestoreFromSnapshot(snapshot)
	if frontier.Size() != 3 {
		t.Errorf("Size after restore = %d, want 3", frontier.Size())
	}
}

func TestFrontierMedianHeight(t *testing.T) {
	frontier := NewFrontier()

	heights := []uint64{10, 20, 30, 40, 50}
	for i, h := range heights {
		vertex := &dag.Vertex{
			Hash:           dag.Hash{byte(i)},
			Height:         h,
			ValidatorIndex: uint32(i),
			FinalizedAt:    time.Now(),
		}
		frontier.Update(vertex)
	}

	median := frontier.GetMedianHeight()
	if median != 30 {
		t.Errorf("MedianHeight = %d, want 30", median)
	}
}

// === Reorg Tests ===

func TestReorgHandlerNew(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	handler := engine.reorgHandler
	if handler == nil {
		t.Fatal("reorgHandler should not be nil")
	}
}

func TestReorgHandlerSetCurrentTip(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	hash := dag.Hash{1, 2, 3}
	engine.reorgHandler.SetCurrentTip(hash, 100)

	tip, height := engine.reorgHandler.GetCurrentTip()
	if tip != hash {
		t.Error("Tip hash mismatch")
	}
	if height != 100 {
		t.Errorf("Height = %d, want 100", height)
	}
}

func TestReorgHandlerDetectNoReorg(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	// Set current tip
	genesis := &dag.Vertex{
		Hash:   dag.Hash{1, 2, 3, 4},
		Height: 0,
	}
	store.Add(genesis)
	engine.reorgHandler.SetCurrentTip(genesis.Hash, genesis.Height)

	// Add extending block
	block := &dag.Vertex{
		Hash:    dag.Hash{5, 6, 7, 8},
		Height:  1,
		Parents: []dag.Hash{genesis.Hash},
	}
	store.Add(block)

	// Detect reorg (should be none)
	event, err := engine.reorgHandler.DetectReorg(block)
	if err != nil {
		t.Fatalf("DetectReorg failed: %v", err)
	}

	if event != nil {
		t.Error("Expected no reorg event for extending block")
	}
}

func TestReorgHandlerCanReorg(t *testing.T) {
	config := DefaultConfig()
	config.MaxReorgDepth = 10

	store := newMockVertexStore()
	engine := NewEngine(config, store)

	// Can reorg at depth 5
	if !engine.reorgHandler.CanReorg(5) {
		t.Error("Should allow reorg at depth 5")
	}

	// Cannot reorg at depth 15
	if engine.reorgHandler.CanReorg(15) {
		t.Error("Should not allow reorg at depth 15")
	}
}

func TestReorgStats(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	stats := engine.reorgHandler.GetReorgStats()

	if stats.TotalReorgs != 0 {
		t.Errorf("TotalReorgs = %d, want 0", stats.TotalReorgs)
	}
}

// === Statistics Tests ===

func TestEngineStatistics(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	stats := engine.GetStatistics()

	if stats.TotalFinalized != 0 {
		t.Errorf("TotalFinalized = %d, want 0", stats.TotalFinalized)
	}

	// Finalize genesis
	genesis := &dag.Vertex{
		Hash:      dag.Hash{1, 2, 3, 4},
		Height:    0,
		Parents:   []dag.Hash{},
		Timestamp: time.Now(),
	}
	store.Add(genesis)
	engine.FinalizeGenesis(genesis)

	stats = engine.GetStatistics()
	if stats.TotalFinalized != 1 {
		t.Errorf("TotalFinalized = %d, want 1", stats.TotalFinalized)
	}

	if stats.LastFinalizedHeight != 0 {
		t.Errorf("LastFinalizedHeight = %d, want 0", stats.LastFinalizedHeight)
	}
}

// === Prune Tests ===

func TestEnginePrune(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	// Create and finalize genesis
	genesis := &dag.Vertex{
		Hash:      dag.Hash{1, 2, 3, 4},
		Height:    0,
		Parents:   []dag.Hash{},
		Timestamp: time.Now(),
	}
	store.Add(genesis)
	engine.FinalizeGenesis(genesis)

	// Create and finalize more blocks
	prevHash := genesis.Hash
	for i := uint64(1); i <= 5; i++ {
		block := createTestVertex(dag.Hash{byte(i + 10)}, i, []dag.Hash{prevHash}, 0)
		store.Add(block)

		cert := createTestCertificate(block.Hash, i*10, 3)
		engine.ProcessCommitCertificate(cert)
		prevHash = block.Hash
	}

	// Prune below height 3
	pruned := engine.Prune(3)
	if pruned < 3 {
		t.Errorf("pruned = %d, want >= 3", pruned)
	}
}

// === Integration Tests ===

func TestFinalityChain(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	// Create chain: genesis -> block1 -> block2 -> block3
	genesis := &dag.Vertex{
		Hash:      dag.Hash{0},
		Height:    0,
		Parents:   []dag.Hash{},
		Timestamp: time.Now(),
	}
	store.Add(genesis)
	engine.FinalizeGenesis(genesis)

	prevHash := genesis.Hash
	for i := uint64(1); i <= 3; i++ {
		block := createTestVertex(dag.Hash{byte(i)}, i, []dag.Hash{prevHash}, 0)
		store.Add(block)

		cert := createTestCertificate(block.Hash, i*10, 3)
		finBlock, err := engine.ProcessCommitCertificate(cert)
		if err != nil {
			t.Fatalf("Failed to finalize block %d: %v", i, err)
		}
		if finBlock == nil {
			t.Fatalf("Block %d not finalized", i)
		}

		prevHash = block.Hash
	}

	// Check all finalized
	for i := uint64(0); i <= 3; i++ {
		blocks := engine.GetFinalizedAtHeight(i)
		if len(blocks) != 1 {
			t.Errorf("Height %d: len(blocks) = %d, want 1", i, len(blocks))
		}
	}

	// Check frontier - minHeight is the lowest among validators
	// Since we use validator 0 for all blocks, only the latest (height 3) is tracked
	if engine.GetFrontierHeight() != 3 {
		t.Errorf("FrontierHeight = %d, want 3", engine.GetFrontierHeight())
	}
}

// === Benchmark Tests ===

func BenchmarkProcessCommitCertificate(b *testing.B) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	genesis := &dag.Vertex{
		Hash:      dag.Hash{0},
		Height:    0,
		Parents:   []dag.Hash{},
		Timestamp: time.Now(),
	}
	store.Add(genesis)
	engine.FinalizeGenesis(genesis)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		block := createTestVertex(dag.Hash{byte(i % 255), byte(i / 255)}, uint64(i+1), []dag.Hash{genesis.Hash}, 0)
		store.Add(block)

		cert := createTestCertificate(block.Hash, uint64(i*10), 3)
		engine.ProcessCommitCertificate(cert)
	}
}

func BenchmarkFrontierUpdate(b *testing.B) {
	frontier := NewFrontier()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vertex := &dag.Vertex{
			Hash:           dag.Hash{byte(i % 255)},
			Height:         uint64(i),
			ValidatorIndex: uint32(i % 100),
			FinalizedAt:    time.Now(),
		}
		frontier.Update(vertex)
	}
}

func BenchmarkProofSerialize(b *testing.B) {
	proof := NewFinalityProof(
		dag.Hash{1, 2, 3},
		100,
		50,
		3,
		700,
		1000,
		make([]byte, 96),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proof.Serialize()
	}
}
