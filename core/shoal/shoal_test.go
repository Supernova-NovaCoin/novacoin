package shoal

import (
	"testing"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// mockVertexStore implements VertexStore for testing.
type mockVertexStore struct {
	vertices map[dag.Hash]*dag.Vertex
	tips     []*dag.Vertex
}

func newMockVertexStore() *mockVertexStore {
	return &mockVertexStore{
		vertices: make(map[dag.Hash]*dag.Vertex),
		tips:     make([]*dag.Vertex, 0),
	}
}

func (s *mockVertexStore) Get(hash dag.Hash) *dag.Vertex {
	return s.vertices[hash]
}

func (s *mockVertexStore) GetAll() []*dag.Vertex {
	vertices := make([]*dag.Vertex, 0, len(s.vertices))
	for _, v := range s.vertices {
		vertices = append(vertices, v)
	}
	return vertices
}

func (s *mockVertexStore) GetTips() []*dag.Vertex {
	return s.tips
}

func (s *mockVertexStore) Add(v *dag.Vertex) {
	s.vertices[v.Hash] = v
}

// Helper to create a test hash.
func testHash(id byte) dag.Hash {
	var h dag.Hash
	h[0] = id
	return h
}

// Helper to create a test public key.
func testPubKey(id byte) dag.PublicKey {
	var pk dag.PublicKey
	pk[0] = id
	return pk
}

// === Wave Tracker Tests ===

func TestNewWave(t *testing.T) {
	wave := NewWave(1)

	if wave.Number != 1 {
		t.Errorf("expected wave number 1, got %d", wave.Number)
	}

	if wave.State != WavePending {
		t.Errorf("expected state pending, got %v", wave.State)
	}

	if len(wave.Anchors) != 0 {
		t.Errorf("expected no anchors, got %d", len(wave.Anchors))
	}
}

func TestWaveAddAnchor(t *testing.T) {
	wave := NewWave(1)
	wave.TotalStake = 100

	wave.AddAnchor(testHash(1), 0, 30)
	wave.AddAnchor(testHash(2), 1, 40)
	wave.AddAnchor(testHash(3), 2, 30)

	if len(wave.Anchors) != 3 {
		t.Errorf("expected 3 anchors, got %d", len(wave.Anchors))
	}

	if wave.ParticipatedStake != 100 {
		t.Errorf("expected participated stake 100, got %d", wave.ParticipatedStake)
	}

	rate := wave.ParticipationRate()
	if rate != 1.0 {
		t.Errorf("expected participation rate 1.0, got %f", rate)
	}
}

func TestWaveVoting(t *testing.T) {
	wave := NewWave(1)
	wave.TotalStake = 100
	wave.State = WaveVoting

	anchor1 := testHash(1)
	anchor2 := testHash(2)

	wave.AddVote(anchor1, 40)
	wave.AddVote(anchor1, 25)
	wave.AddVote(anchor2, 35)

	leading, votes := wave.GetLeadingAnchor()
	if leading != anchor1 {
		t.Errorf("expected anchor1 to be leading")
	}
	if votes != 65 {
		t.Errorf("expected 65 votes, got %d", votes)
	}

	// Check quorum (2/3 = 66.67%)
	if wave.HasQuorum(2.0 / 3.0) {
		t.Error("should not have quorum with 65%")
	}

	// Add more votes
	wave.AddVote(anchor1, 5)
	if !wave.HasQuorum(2.0 / 3.0) {
		t.Error("should have quorum with 70%")
	}
}

func TestWaveTrackerBasic(t *testing.T) {
	config := DefaultWaveConfig()
	config.WaveTimeout = 1 * time.Second
	wt := NewWaveTracker(config)

	wave := wt.CurrentWave()
	if wave == nil {
		t.Fatal("expected current wave")
	}

	if wave.Number != 1 {
		t.Errorf("expected wave 1, got %d", wave.Number)
	}

	if wave.State != WaveProposing {
		t.Errorf("expected proposing state, got %v", wave.State)
	}
}

func TestWaveTrackerStateTransitions(t *testing.T) {
	config := DefaultWaveConfig()
	config.ProposingDuration = 10 * time.Millisecond
	config.VotingDuration = 10 * time.Millisecond
	wt := NewWaveTracker(config)
	wt.SetTotalStake(100)

	// Add an anchor
	err := wt.AddAnchor(testHash(1), 0, 70)
	if err != nil {
		t.Fatalf("failed to add anchor: %v", err)
	}

	// Transition to voting
	err = wt.TransitionToVoting()
	if err != nil {
		t.Fatalf("failed to transition to voting: %v", err)
	}

	if wt.CurrentWave().State != WaveVoting {
		t.Errorf("expected voting state")
	}

	// Add votes
	err = wt.AddVote(testHash(1), 70)
	if err != nil {
		t.Fatalf("failed to add vote: %v", err)
	}

	// Transition to committing
	err = wt.TransitionToCommitting()
	if err != nil {
		t.Fatalf("failed to transition to committing: %v", err)
	}

	// Commit wave
	committedWave, err := wt.CommitWave()
	if err != nil {
		t.Fatalf("failed to commit wave: %v", err)
	}

	if committedWave.State != WaveCommitted {
		t.Errorf("expected committed state")
	}

	if committedWave.CommitAnchor != testHash(1) {
		t.Errorf("expected commit anchor to be hash 1")
	}

	// New wave should have started
	if wt.CurrentWaveNumber() != 2 {
		t.Errorf("expected wave 2 after commit, got %d", wt.CurrentWaveNumber())
	}
}

// === Anchor Manager Tests ===

func TestAnchorManagerRegisterValidator(t *testing.T) {
	am := NewAnchorManager(nil)

	am.RegisterValidator(0, testPubKey(0), 100)
	am.RegisterValidator(1, testPubKey(1), 200)
	am.RegisterValidator(2, testPubKey(2), 150)

	if am.GetTotalStake() != 450 {
		t.Errorf("expected total stake 450, got %d", am.GetTotalStake())
	}

	if am.GetActiveValidatorCount() != 3 {
		t.Errorf("expected 3 active validators")
	}
}

func TestAnchorManagerCreateAnchor(t *testing.T) {
	am := NewAnchorManager(nil)
	am.RegisterValidator(0, testPubKey(0), 100)

	anchor, err := am.CreateAnchor(testHash(1), 0, 1, 0, nil)
	if err != nil {
		t.Fatalf("failed to create anchor: %v", err)
	}

	if anchor.Hash != testHash(1) {
		t.Errorf("unexpected anchor hash")
	}

	if anchor.ValidatorIndex != 0 {
		t.Errorf("unexpected validator index")
	}

	if anchor.Wave != 1 {
		t.Errorf("unexpected wave number")
	}

	if anchor.State != AnchorBroadcast {
		t.Errorf("expected broadcast state")
	}
}

func TestAnchorManagerDuplicateAnchor(t *testing.T) {
	am := NewAnchorManager(nil)
	am.RegisterValidator(0, testPubKey(0), 100)

	_, err := am.CreateAnchor(testHash(1), 0, 1, 0, nil)
	if err != nil {
		t.Fatalf("failed to create first anchor: %v", err)
	}

	// Try to create another anchor in same wave
	_, err = am.CreateAnchor(testHash(2), 0, 1, 0, nil)
	if err != ErrAlreadyAnchored {
		t.Errorf("expected ErrAlreadyAnchored, got %v", err)
	}
}

func TestAnchorManagerVoting(t *testing.T) {
	am := NewAnchorManager(nil)
	am.RegisterValidator(0, testPubKey(0), 100)
	am.RegisterValidator(1, testPubKey(1), 100)
	am.RegisterValidator(2, testPubKey(2), 100)

	anchor, _ := am.CreateAnchor(testHash(1), 0, 1, 0, nil)

	// Record votes
	_ = am.RecordVote(testHash(1), 1, 100)
	_ = am.RecordVote(testHash(1), 2, 100)

	// Check vote count
	a := am.GetAnchor(testHash(1))
	if a.VotesReceived != 200 {
		t.Errorf("expected 200 votes, got %d", a.VotesReceived)
	}

	// Should be certified (200/300 = 66.67%, just at 2/3)
	if anchor.State != AnchorCertified {
		t.Errorf("expected certified state")
	}
}

func TestAnchorManagerParallelDAGAssignment(t *testing.T) {
	am := NewAnchorManager(nil)
	am.config.NumParallelDAGs = 4

	// Verify deterministic assignment
	dag0 := am.AssignToParallelDAG(0, 1)
	dag1 := am.AssignToParallelDAG(1, 1)
	dag2 := am.AssignToParallelDAG(2, 1)
	dag3 := am.AssignToParallelDAG(3, 1)

	// Different validators should get different DAGs in same wave
	dags := map[uint8]bool{dag0: true, dag1: true, dag2: true, dag3: true}
	if len(dags) != 4 {
		t.Error("expected 4 different DAG assignments for 4 validators")
	}

	// Same validator in different waves should rotate
	dag0w1 := am.AssignToParallelDAG(0, 1)
	dag0w2 := am.AssignToParallelDAG(0, 2)
	if dag0w1 == dag0w2 {
		t.Error("expected different DAG assignment in different waves")
	}
}

// === Parallel DAG Tests ===

func TestNewParallelDAG(t *testing.T) {
	pd := NewParallelDAG(0, 0)

	if pd.ID != 0 {
		t.Errorf("expected ID 0")
	}

	if pd.Name != "Alpha" {
		t.Errorf("expected name Alpha, got %s", pd.Name)
	}
}

func TestParallelDAGAddBlock(t *testing.T) {
	pd := NewParallelDAG(0, 0)

	pd.AddBlock(testHash(1), 1)
	pd.AddBlock(testHash(2), 1)

	if pd.BlockCount != 2 {
		t.Errorf("expected 2 blocks, got %d", pd.BlockCount)
	}

	if pd.CurrentWave != 1 {
		t.Errorf("expected wave 1")
	}
}

func TestParallelDAGCoordinator(t *testing.T) {
	config := DefaultParallelConfig()
	config.NumDAGs = 4
	coord := NewParallelDAGCoordinator(config)

	// Verify 4 DAGs created
	dags := coord.GetAllDAGs()
	if len(dags) != 4 {
		t.Errorf("expected 4 DAGs, got %d", len(dags))
	}

	// Verify stagger offsets
	for i, dag := range dags {
		expectedOffset := time.Duration(i) * config.StaggerOffset
		if dag.Offset != expectedOffset {
			t.Errorf("DAG %d: expected offset %v, got %v", i, expectedOffset, dag.Offset)
		}
	}
}

func TestParallelDAGCoordinatorAddBlock(t *testing.T) {
	coord := NewParallelDAGCoordinator(nil)

	// Add blocks to different DAGs
	_ = coord.AddBlock(0, testHash(1), 1)
	_ = coord.AddBlock(1, testHash(2), 1)
	_ = coord.AddBlock(2, testHash(3), 1)
	_ = coord.AddBlock(3, testHash(4), 1)

	stats := coord.GetStatistics()
	if stats.TotalBlocks != 4 {
		t.Errorf("expected 4 total blocks, got %d", stats.TotalBlocks)
	}

	for i, count := range stats.BlocksPerDAG {
		if count != 1 {
			t.Errorf("DAG %d: expected 1 block, got %d", i, count)
		}
	}
}

func TestParallelDAGCoordinatorCrossRef(t *testing.T) {
	config := DefaultParallelConfig()
	config.EnableCrossRef = true
	coord := NewParallelDAGCoordinator(config)

	coord.AddCrossReference(testHash(1), testHash(2))
	coord.AddCrossReference(testHash(1), testHash(3))

	refs := coord.GetCrossReferences(testHash(1))
	if len(refs) != 2 {
		t.Errorf("expected 2 cross references, got %d", len(refs))
	}
}

func TestParallelDAGCoordinatorThroughput(t *testing.T) {
	config := DefaultParallelConfig()
	config.NumDAGs = 4
	config.SlotDuration = 125 * time.Millisecond
	coord := NewParallelDAGCoordinator(config)

	throughput := coord.ComputeEffectiveThroughput()
	// 4 DAGs * 8 slots/second = 32 blocks/second
	expectedMin := 30.0
	if throughput < expectedMin {
		t.Errorf("expected throughput >= %f, got %f", expectedMin, throughput)
	}
}

// === Engine Tests ===

func TestNewEngine(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	if engine == nil {
		t.Fatal("failed to create engine")
	}

	if engine.waveTracker == nil {
		t.Error("wave tracker not initialized")
	}

	if engine.anchorManager == nil {
		t.Error("anchor manager not initialized")
	}

	if engine.parallelCoord == nil {
		t.Error("parallel coordinator not initialized")
	}
}

func TestEngineStartStop(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	err := engine.Start()
	if err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}

	// Try starting again
	err = engine.Start()
	if err != ErrAlreadyRunning {
		t.Errorf("expected ErrAlreadyRunning, got %v", err)
	}

	err = engine.Stop()
	if err != nil {
		t.Fatalf("failed to stop engine: %v", err)
	}
}

func TestEngineProposeAnchor(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	// Register validators
	engine.RegisterValidator(0, testPubKey(0), 100)
	engine.RegisterValidator(1, testPubKey(1), 100)
	engine.RegisterValidator(2, testPubKey(2), 100)

	err := engine.Start()
	if err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}
	defer engine.Stop()

	// Propose an anchor
	anchor, err := engine.ProposeAnchor(testHash(1), 0, nil)
	if err != nil {
		t.Fatalf("failed to propose anchor: %v", err)
	}

	if anchor.Hash != testHash(1) {
		t.Error("unexpected anchor hash")
	}

	// Verify it's in the current wave
	wave := engine.GetCurrentWave()
	if len(wave.Anchors) != 1 {
		t.Errorf("expected 1 anchor in wave, got %d", len(wave.Anchors))
	}
}

func TestEngineVoting(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	engine.RegisterValidator(0, testPubKey(0), 100)
	engine.RegisterValidator(1, testPubKey(1), 100)
	engine.RegisterValidator(2, testPubKey(2), 100)

	err := engine.Start()
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer engine.Stop()

	// Propose anchor
	_, err = engine.ProposeAnchor(testHash(1), 0, nil)
	if err != nil {
		t.Fatalf("failed to propose: %v", err)
	}

	// Transition wave to voting (manually for testing)
	engine.mu.Lock()
	engine.waveTracker.TransitionToVoting()
	engine.mu.Unlock()

	// Vote for anchor
	err = engine.VoteForAnchor(testHash(1), 1)
	if err != nil {
		t.Fatalf("failed to vote: %v", err)
	}

	err = engine.VoteForAnchor(testHash(1), 2)
	if err != nil {
		t.Fatalf("failed to vote: %v", err)
	}

	// Check votes
	anchor := engine.GetAnchor(testHash(1))
	if anchor.VotesReceived != 200 {
		t.Errorf("expected 200 votes, got %d", anchor.VotesReceived)
	}
}

func TestEngineMetrics(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	engine.RegisterValidator(0, testPubKey(0), 100)

	_ = engine.Start()
	defer engine.Stop()

	_, _ = engine.ProposeAnchor(testHash(1), 0, nil)

	// Give the anchor loop time to process
	time.Sleep(20 * time.Millisecond)

	metrics := engine.GetMetrics()
	if metrics.AnchorsProposed != 1 {
		t.Errorf("expected 1 anchor proposed, got %d", metrics.AnchorsProposed)
	}
}

func TestEngineTheoreticalThroughput(t *testing.T) {
	store := newMockVertexStore()
	config := DefaultConfig()
	config.Parallel.NumDAGs = 4
	config.Parallel.SlotDuration = 125 * time.Millisecond
	engine := NewEngine(config, store)

	// Register 100 validators
	for i := uint32(0); i < 100; i++ {
		engine.RegisterValidator(i, testPubKey(byte(i)), 100)
	}

	throughput := engine.ComputeTheoreticalThroughput()
	// Should be substantial with 100 validators and 4 DAGs
	if throughput < 1000 {
		t.Errorf("expected throughput > 1000, got %f", throughput)
	}
}

// === Integration Tests ===

func TestFullWaveCommit(t *testing.T) {
	// Test wave commit flow using WaveTracker directly
	config := DefaultWaveConfig()
	config.ProposingDuration = 10 * time.Millisecond
	config.VotingDuration = 10 * time.Millisecond
	wt := NewWaveTracker(config)
	wt.SetTotalStake(300)

	// Track committed waves
	var committedWaves []*Wave
	wt.OnWaveCommit(func(w *Wave) {
		committedWaves = append(committedWaves, w)
	})

	// Add anchor
	err := wt.AddAnchor(testHash(1), 0, 100)
	if err != nil {
		t.Fatalf("failed to add anchor: %v", err)
	}

	// Transition to voting
	err = wt.TransitionToVoting()
	if err != nil {
		t.Fatalf("failed to transition to voting: %v", err)
	}

	// Add votes for quorum (need 2/3 = 200 stake)
	_ = wt.AddVote(testHash(1), 100)
	_ = wt.AddVote(testHash(1), 100)
	_ = wt.AddVote(testHash(1), 100) // 300 total = 100%

	// Transition to committing
	err = wt.TransitionToCommitting()
	if err != nil {
		t.Fatalf("failed to transition to committing: %v", err)
	}

	// Commit wave
	wave, err := wt.CommitWave()
	if err != nil {
		t.Fatalf("failed to commit wave: %v", err)
	}

	if wave.State != WaveCommitted {
		t.Errorf("expected committed state, got %v", wave.State)
	}

	if wave.CommitAnchor != testHash(1) {
		t.Error("expected commit anchor to be hash 1")
	}

	// Give callback time to execute
	time.Sleep(10 * time.Millisecond)

	if len(committedWaves) == 0 {
		t.Error("expected at least one committed wave callback")
	}

	// New wave should have started
	if wt.CurrentWaveNumber() != 2 {
		t.Errorf("expected wave 2 after commit, got %d", wt.CurrentWaveNumber())
	}
}

func TestWaveStateString(t *testing.T) {
	tests := []struct {
		state    WaveState
		expected string
	}{
		{WavePending, "pending"},
		{WaveProposing, "proposing"},
		{WaveVoting, "voting"},
		{WaveCommitting, "committing"},
		{WaveCommitted, "committed"},
		{WaveSkipped, "skipped"},
		{WaveState(99), "unknown"},
	}

	for _, test := range tests {
		result := test.state.String()
		if result != test.expected {
			t.Errorf("state %d: expected %s, got %s", test.state, test.expected, result)
		}
	}
}

func TestAnchorStateString(t *testing.T) {
	tests := []struct {
		state    AnchorState
		expected string
	}{
		{AnchorPending, "pending"},
		{AnchorBroadcast, "broadcast"},
		{AnchorReceived, "received"},
		{AnchorCertified, "certified"},
		{AnchorCommitted, "committed"},
		{AnchorOrphaned, "orphaned"},
		{AnchorState(99), "unknown"},
	}

	for _, test := range tests {
		result := test.state.String()
		if result != test.expected {
			t.Errorf("state %d: expected %s, got %s", test.state, test.expected, result)
		}
	}
}

// Benchmark tests

func BenchmarkProposeAnchor(b *testing.B) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	for i := uint32(0); i < 100; i++ {
		engine.RegisterValidator(i, testPubKey(byte(i)), 100)
	}

	_ = engine.Start()
	defer engine.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hash := testHash(byte(i % 256))
		validatorIndex := uint32(i % 100)
		// Reset wave state for testing
		engine.mu.Lock()
		delete(engine.anchorManager.anchorsByValidator[validatorIndex], engine.waveTracker.CurrentWaveNumber())
		engine.mu.Unlock()
		_, _ = engine.ProposeAnchor(hash, validatorIndex, nil)
	}
}

func BenchmarkWaveTrackerAddAnchor(b *testing.B) {
	wt := NewWaveTracker(nil)
	wt.SetTotalStake(10000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wt.mu.Lock()
		wt.currentWave.AddAnchor(testHash(byte(i%256)), uint32(i%100), 100)
		wt.mu.Unlock()
	}
}

func BenchmarkAnchorManagerCreate(b *testing.B) {
	am := NewAnchorManager(nil)

	for i := uint32(0); i < 1000; i++ {
		am.RegisterValidator(i, testPubKey(byte(i%256)), 100)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wave := uint64(i)
		validatorIndex := uint32(i % 1000)
		_, _ = am.CreateAnchor(testHash(byte(i%256)), validatorIndex, wave, 0, nil)
	}
}
