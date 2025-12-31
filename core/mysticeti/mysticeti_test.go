package mysticeti

import (
	"testing"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// mockVertexStore implements VertexStore for testing.
type mockVertexStore struct {
	vertices map[dag.Hash]*dag.Vertex
	children map[dag.Hash][]*dag.Vertex
	tips     []*dag.Vertex
}

func newMockVertexStore() *mockVertexStore {
	return &mockVertexStore{
		vertices: make(map[dag.Hash]*dag.Vertex),
		children: make(map[dag.Hash][]*dag.Vertex),
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

func (s *mockVertexStore) GetChildren(hash dag.Hash) []*dag.Vertex {
	return s.children[hash]
}

func (s *mockVertexStore) GetTips() []*dag.Vertex {
	return s.tips
}

func (s *mockVertexStore) Add(v *dag.Vertex) {
	s.vertices[v.Hash] = v
	// Update children links
	for _, parentHash := range v.Parents {
		s.children[parentHash] = append(s.children[parentHash], v)
	}
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

// Helper to create a test vertex.
func testVertex(id byte, round uint64, validatorIndex uint32, stake uint64, parents []dag.Hash) *dag.Vertex {
	v := &dag.Vertex{
		Hash:           testHash(id),
		Round:          round,
		ValidatorIndex: validatorIndex,
		Stake:          stake,
		Parents:        parents,
		ImplicitVotes:  parents,
		Timestamp:      time.Now(),
	}
	return v
}

// === Round Manager Tests ===

func TestNewRound(t *testing.T) {
	round := NewRound(1, 0, 100)

	if round.Number != 1 {
		t.Errorf("expected round number 1, got %d", round.Number)
	}

	if round.LeaderIndex != 0 {
		t.Errorf("expected leader index 0, got %d", round.LeaderIndex)
	}

	if round.TotalStake != 100 {
		t.Errorf("expected total stake 100, got %d", round.TotalStake)
	}

	if round.State != RoundPending {
		t.Errorf("expected pending state, got %v", round.State)
	}
}

func TestRoundAddBlock(t *testing.T) {
	round := NewRound(1, 0, 300)

	// Add first block
	isNew, err := round.AddBlock(testHash(1), 0, 100)
	if err != nil || !isNew {
		t.Fatalf("failed to add first block: %v", err)
	}

	// Add second block from different validator
	isNew, err = round.AddBlock(testHash(2), 1, 100)
	if err != nil || !isNew {
		t.Fatalf("failed to add second block: %v", err)
	}

	// Add duplicate block (same hash)
	isNew, err = round.AddBlock(testHash(1), 0, 100)
	if err != nil || isNew {
		t.Error("duplicate block should not be new")
	}

	// Check state
	if len(round.Blocks) != 2 {
		t.Errorf("expected 2 blocks, got %d", len(round.Blocks))
	}

	if round.ParticipatedStake != 200 {
		t.Errorf("expected participated stake 200, got %d", round.ParticipatedStake)
	}
}

func TestRoundEquivocation(t *testing.T) {
	round := NewRound(1, 0, 100)

	// Add first block from validator 0
	_, _ = round.AddBlock(testHash(1), 0, 100)

	// Try to add different block from same validator (equivocation)
	_, err := round.AddBlock(testHash(2), 0, 100)
	if err != ErrEquivocation {
		t.Errorf("expected ErrEquivocation, got %v", err)
	}

	if !round.HasEquivocation() {
		t.Error("expected equivocation to be detected")
	}

	if len(round.Equivocators) != 1 || round.Equivocators[0] != 0 {
		t.Error("expected validator 0 to be in equivocators list")
	}
}

func TestRoundQuorum(t *testing.T) {
	round := NewRound(1, 0, 300)

	// Add blocks for 2/3 quorum
	_, _ = round.AddBlock(testHash(1), 0, 100)
	if round.HasQuorum(2.0 / 3.0) {
		t.Error("should not have quorum with 1/3 stake")
	}

	_, _ = round.AddBlock(testHash(2), 1, 100)
	// 200/300 = 2/3 exactly, which meets >= 2/3 threshold
	if !round.HasQuorum(2.0 / 3.0) {
		t.Error("should have quorum with 2/3 stake exactly")
	}

	_, _ = round.AddBlock(testHash(3), 2, 100)
	if !round.HasQuorum(2.0 / 3.0) {
		t.Error("should have quorum with 100% stake")
	}
}

func TestRoundManagerBasic(t *testing.T) {
	rm := NewRoundManager(nil)

	// Register validators
	rm.RegisterValidator(0, testPubKey(0), 100)
	rm.RegisterValidator(1, testPubKey(1), 100)
	rm.RegisterValidator(2, testPubKey(2), 100)

	// Start first round
	round := rm.StartRound()
	if round.Number != 1 {
		t.Errorf("expected round 1, got %d", round.Number)
	}

	if round.State != RoundProposing {
		t.Errorf("expected proposing state")
	}
}

func TestRoundManagerLeaderSelection(t *testing.T) {
	rm := NewRoundManager(nil)

	rm.RegisterValidator(0, testPubKey(0), 100)
	rm.RegisterValidator(1, testPubKey(1), 100)
	rm.RegisterValidator(2, testPubKey(2), 100)

	// Verify deterministic leader selection
	leader1 := rm.SelectLeader(1)
	leader2 := rm.SelectLeader(2)
	leader3 := rm.SelectLeader(3)

	// Leaders should cycle through validators
	leaders := map[uint32]bool{leader1: true, leader2: true, leader3: true}
	if len(leaders) != 3 {
		t.Error("expected 3 different leaders for 3 rounds")
	}

	// Same round should always give same leader
	if rm.SelectLeader(1) != leader1 {
		t.Error("leader selection should be deterministic")
	}
}

func TestRoundManagerAddBlock(t *testing.T) {
	rm := NewRoundManager(nil)

	rm.RegisterValidator(0, testPubKey(0), 100)
	rm.RegisterValidator(1, testPubKey(1), 100)

	rm.StartRound()

	// Add block to current round
	isNew, err := rm.AddBlock(testHash(1), 0, 1)
	if err != nil || !isNew {
		t.Fatalf("failed to add block: %v", err)
	}

	// Try adding to future round
	_, err = rm.AddBlock(testHash(2), 1, 10)
	if err != ErrFutureRound {
		t.Errorf("expected ErrFutureRound, got %v", err)
	}
}

// === Vote Tracker Tests ===

func TestVoteTrackerBasic(t *testing.T) {
	store := newMockVertexStore()

	vt := NewVoteTracker(nil, store.Get, store.GetChildren)

	// Register validators
	vt.RegisterValidator(0, 100)
	vt.RegisterValidator(1, 100)
	vt.RegisterValidator(2, 100)

	if vt.GetTotalStake() != 300 {
		t.Errorf("expected total stake 300, got %d", vt.GetTotalStake())
	}
}

func TestVoteTrackerImplicitVotes(t *testing.T) {
	store := newMockVertexStore()

	// Create parent block (round 1)
	parent := testVertex(1, 1, 0, 100, nil)
	store.Add(parent)

	// Create child blocks that vote for parent (round 2)
	child1 := testVertex(2, 2, 1, 100, []dag.Hash{parent.Hash})
	child2 := testVertex(3, 2, 2, 100, []dag.Hash{parent.Hash})
	store.Add(child1)
	store.Add(child2)

	vt := NewVoteTracker(nil, store.Get, store.GetChildren)
	vt.RegisterValidator(0, 100)
	vt.RegisterValidator(1, 100)
	vt.RegisterValidator(2, 100)

	// Process child blocks
	_ = vt.ProcessBlock(child1.Hash)
	_ = vt.ProcessBlock(child2.Hash)

	// Check vote tally for parent
	tally := vt.GetVoteTally(parent.Hash)
	if tally == nil {
		t.Fatal("expected vote tally for parent")
	}

	if len(tally.DirectVotes) != 2 {
		t.Errorf("expected 2 direct votes, got %d", len(tally.DirectVotes))
	}

	if tally.DirectVoteStake != 200 {
		t.Errorf("expected direct vote stake 200, got %d", tally.DirectVoteStake)
	}
}

func TestVoteTrackerQuorumCheck(t *testing.T) {
	store := newMockVertexStore()

	// Build a chain of blocks with votes at each level
	genesis := testVertex(0, 0, 0, 100, nil)
	store.Add(genesis)

	block1 := testVertex(1, 1, 0, 100, []dag.Hash{genesis.Hash})
	store.Add(block1)

	// Round 2 blocks voting for block1
	block2a := testVertex(2, 2, 0, 100, []dag.Hash{block1.Hash})
	block2b := testVertex(3, 2, 1, 100, []dag.Hash{block1.Hash})
	block2c := testVertex(4, 2, 2, 100, []dag.Hash{block1.Hash})
	store.Add(block2a)
	store.Add(block2b)
	store.Add(block2c)

	vt := NewVoteTracker(nil, store.Get, store.GetChildren)
	vt.RegisterValidator(0, 100)
	vt.RegisterValidator(1, 100)
	vt.RegisterValidator(2, 100)

	// Process blocks
	_ = vt.ProcessBlock(block2a.Hash)
	_ = vt.ProcessBlock(block2b.Hash)
	_ = vt.ProcessBlock(block2c.Hash)

	// Check if block1 has quorum at depth 1
	tally := vt.GetVoteTally(block1.Hash)
	if tally == nil {
		t.Fatal("expected vote tally")
	}

	// Check stake at round 2 (depth 1 from block1's round 1)
	stakeAtDepth1 := tally.VotesByRound[2]
	if stakeAtDepth1 != 300 {
		t.Errorf("expected 300 stake at depth 1, got %d", stakeAtDepth1)
	}

	// Check HasQuorumAtRound
	if !tally.HasQuorumAtRound(2, 300, 2.0/3.0) {
		t.Error("expected quorum at round 2")
	}
}

// === Commit Rule Tests ===

func TestCommitCertificate(t *testing.T) {
	cert := NewCommitCertificate(testHash(1), 1, CommitFastPath, 2)

	if cert.Block != testHash(1) {
		t.Error("unexpected block hash")
	}

	if cert.Status != CommitFastPath {
		t.Errorf("expected fast path status")
	}

	if cert.CommitDepth != 2 {
		t.Errorf("expected commit depth 2")
	}
}

func TestCommitRuleStandardPath(t *testing.T) {
	store := newMockVertexStore()

	// Create a chain with 4 rounds
	genesis := testVertex(0, 0, 0, 100, nil)
	store.Add(genesis)

	block1 := testVertex(1, 1, 0, 100, []dag.Hash{genesis.Hash})
	store.Add(block1)

	// Round 2 (depth 1)
	block2a := testVertex(2, 2, 0, 100, []dag.Hash{block1.Hash})
	block2b := testVertex(3, 2, 1, 100, []dag.Hash{block1.Hash})
	block2c := testVertex(4, 2, 2, 100, []dag.Hash{block1.Hash})
	store.Add(block2a)
	store.Add(block2b)
	store.Add(block2c)

	// Round 3 (depth 2)
	block3a := testVertex(5, 3, 0, 100, []dag.Hash{block2a.Hash})
	block3b := testVertex(6, 3, 1, 100, []dag.Hash{block2b.Hash})
	block3c := testVertex(7, 3, 2, 100, []dag.Hash{block2c.Hash})
	store.Add(block3a)
	store.Add(block3b)
	store.Add(block3c)

	// Round 4 (depth 3)
	block4a := testVertex(8, 4, 0, 100, []dag.Hash{block3a.Hash})
	block4b := testVertex(9, 4, 1, 100, []dag.Hash{block3b.Hash})
	block4c := testVertex(10, 4, 2, 100, []dag.Hash{block3c.Hash})
	store.Add(block4a)
	store.Add(block4b)
	store.Add(block4c)

	vt := NewVoteTracker(nil, store.Get, store.GetChildren)
	vt.RegisterValidator(0, 100)
	vt.RegisterValidator(1, 100)
	vt.RegisterValidator(2, 100)

	// Process all blocks to register votes
	for _, v := range store.GetAll() {
		_ = vt.ProcessBlock(v.Hash)
	}

	// Create commit rule
	config := DefaultCommitConfig()
	config.EnableFastPath = false // Test standard path only
	cr := NewCommitRule(config, vt, store.Get)

	// Try to commit block1
	cert := cr.TryCommit(block1.Hash)
	if cert == nil {
		t.Fatal("expected block1 to be committed")
	}

	if cert.Status != CommitStandard {
		t.Errorf("expected standard commit, got %v", cert.Status)
	}

	if cert.CommitDepth != 3 {
		t.Errorf("expected depth 3, got %d", cert.CommitDepth)
	}

	// Verify block is now committed
	if !cr.IsCommitted(block1.Hash) {
		t.Error("block1 should be committed")
	}
}

func TestCommitRuleFastPath(t *testing.T) {
	store := newMockVertexStore()

	// Create a chain with 3 rounds (sufficient for fast path)
	genesis := testVertex(0, 0, 0, 100, nil)
	store.Add(genesis)

	block1 := testVertex(1, 1, 0, 100, []dag.Hash{genesis.Hash})
	store.Add(block1)

	// Round 2 (depth 1)
	block2a := testVertex(2, 2, 0, 100, []dag.Hash{block1.Hash})
	block2b := testVertex(3, 2, 1, 100, []dag.Hash{block1.Hash})
	block2c := testVertex(4, 2, 2, 100, []dag.Hash{block1.Hash})
	store.Add(block2a)
	store.Add(block2b)
	store.Add(block2c)

	// Round 3 (depth 2)
	block3a := testVertex(5, 3, 0, 100, []dag.Hash{block2a.Hash})
	block3b := testVertex(6, 3, 1, 100, []dag.Hash{block2b.Hash})
	block3c := testVertex(7, 3, 2, 100, []dag.Hash{block2c.Hash})
	store.Add(block3a)
	store.Add(block3b)
	store.Add(block3c)

	vt := NewVoteTracker(nil, store.Get, store.GetChildren)
	vt.RegisterValidator(0, 100)
	vt.RegisterValidator(1, 100)
	vt.RegisterValidator(2, 100)

	// Process all blocks
	for _, v := range store.GetAll() {
		_ = vt.ProcessBlock(v.Hash)
	}

	// Create commit rule with fast path enabled
	config := DefaultCommitConfig()
	config.EnableFastPath = true
	cr := NewCommitRule(config, vt, store.Get)

	// Try to commit block1
	cert := cr.TryCommit(block1.Hash)
	if cert == nil {
		t.Fatal("expected block1 to be committed")
	}

	if cert.Status != CommitFastPath {
		t.Errorf("expected fast path commit, got %v", cert.Status)
	}

	if cert.CommitDepth != 2 {
		t.Errorf("expected depth 2, got %d", cert.CommitDepth)
	}
}

// === Engine Tests ===

func TestNewEngine(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	if engine == nil {
		t.Fatal("failed to create engine")
	}

	if engine.roundManager == nil {
		t.Error("round manager not initialized")
	}

	if engine.voteTracker == nil {
		t.Error("vote tracker not initialized")
	}

	if engine.commitRule == nil {
		t.Error("commit rule not initialized")
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

func TestEngineProposeBlock(t *testing.T) {
	store := newMockVertexStore()
	config := DefaultConfig()
	config.RoundInterval = 10 * time.Millisecond
	engine := NewEngine(config, store)

	// Register validators
	engine.RegisterValidator(0, testPubKey(0), 100)
	engine.RegisterValidator(1, testPubKey(1), 100)
	engine.RegisterValidator(2, testPubKey(2), 100)

	_ = engine.Start()
	defer engine.Stop()

	// Create and propose a block
	vertex := testVertex(1, 0, 0, 100, nil)
	store.Add(vertex)

	err := engine.ProposeBlock(vertex)
	if err != nil {
		t.Fatalf("failed to propose block: %v", err)
	}

	// Give time for processing
	time.Sleep(50 * time.Millisecond)

	// Check round has the block
	round := engine.GetCurrentRound()
	if round == nil {
		t.Fatal("expected current round")
	}
}

func TestEngineRoundProgression(t *testing.T) {
	store := newMockVertexStore()
	config := DefaultConfig()
	config.Round.RoundTimeout = 50 * time.Millisecond
	config.Round.MinRoundDuration = 10 * time.Millisecond
	config.RoundInterval = 10 * time.Millisecond
	engine := NewEngine(config, store)

	engine.RegisterValidator(0, testPubKey(0), 100)
	engine.RegisterValidator(1, testPubKey(1), 100)
	engine.RegisterValidator(2, testPubKey(2), 100)

	_ = engine.Start()
	defer engine.Stop()

	initialRound := engine.GetCurrentRoundNumber()

	// Wait for a few rounds to pass
	time.Sleep(200 * time.Millisecond)

	currentRound := engine.GetCurrentRoundNumber()
	if currentRound <= initialRound {
		t.Errorf("expected round progression, got %d -> %d", initialRound, currentRound)
	}
}

func TestEngineMetrics(t *testing.T) {
	store := newMockVertexStore()
	engine := NewEngine(nil, store)

	engine.RegisterValidator(0, testPubKey(0), 100)

	_ = engine.Start()
	defer engine.Stop()

	// Propose some blocks
	for i := byte(1); i <= 3; i++ {
		vertex := testVertex(i, 0, 0, 100, nil)
		store.Add(vertex)
		_ = engine.ProposeBlock(vertex)
	}

	// Give time for processing
	time.Sleep(50 * time.Millisecond)

	metrics := engine.GetMetrics()
	if metrics.BlocksProposed == 0 {
		t.Error("expected blocks to be proposed")
	}
}

func TestEngineCallbacks(t *testing.T) {
	store := newMockVertexStore()
	config := DefaultConfig()
	config.Round.RoundTimeout = 50 * time.Millisecond
	config.Round.MinRoundDuration = 10 * time.Millisecond
	config.RoundInterval = 10 * time.Millisecond
	engine := NewEngine(config, store)

	engine.RegisterValidator(0, testPubKey(0), 100)

	var roundAdvances int
	engine.OnRoundAdvance(func(r *Round) {
		roundAdvances++
	})

	_ = engine.Start()
	defer engine.Stop()

	// Wait for rounds to advance
	time.Sleep(150 * time.Millisecond)

	if roundAdvances == 0 {
		t.Error("expected round advance callbacks")
	}
}

// === State String Tests ===

func TestRoundStateString(t *testing.T) {
	tests := []struct {
		state    RoundState
		expected string
	}{
		{RoundPending, "pending"},
		{RoundProposing, "proposing"},
		{RoundWaiting, "waiting"},
		{RoundComplete, "complete"},
		{RoundTimedOut, "timed_out"},
		{RoundState(99), "unknown"},
	}

	for _, test := range tests {
		result := test.state.String()
		if result != test.expected {
			t.Errorf("state %d: expected %s, got %s", test.state, test.expected, result)
		}
	}
}

func TestCommitStatusString(t *testing.T) {
	tests := []struct {
		status   CommitStatus
		expected string
	}{
		{CommitPending, "pending"},
		{CommitFastPath, "fast_path"},
		{CommitStandard, "standard"},
		{CommitSkipped, "skipped"},
		{CommitStatus(99), "unknown"},
	}

	for _, test := range tests {
		result := test.status.String()
		if result != test.expected {
			t.Errorf("status %d: expected %s, got %s", test.status, test.expected, result)
		}
	}
}

// === Benchmark Tests ===

func BenchmarkRoundAddBlock(b *testing.B) {
	round := NewRound(1, 0, 10000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hash := testHash(byte(i % 256))
		validatorIndex := uint32(i % 100)
		round.BlockByValidator[validatorIndex] = dag.Hash{} // Reset for benchmark
		_, _ = round.AddBlock(hash, validatorIndex, 100)
	}
}

func BenchmarkVoteTrackerProcess(b *testing.B) {
	store := newMockVertexStore()

	// Create parent
	parent := testVertex(0, 1, 0, 100, nil)
	store.Add(parent)

	vt := NewVoteTracker(nil, store.Get, store.GetChildren)
	for i := uint32(0); i < 100; i++ {
		vt.RegisterValidator(i, 100)
	}

	// Create many child blocks
	for i := byte(1); i < 100; i++ {
		child := testVertex(i, 2, uint32(i), 100, []dag.Hash{parent.Hash})
		store.Add(child)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hash := testHash(byte(i%99) + 1)
		_ = vt.ProcessBlock(hash)
	}
}

func BenchmarkCommitCheck(b *testing.B) {
	store := newMockVertexStore()

	// Create chain for commit
	genesis := testVertex(0, 0, 0, 100, nil)
	store.Add(genesis)

	block1 := testVertex(1, 1, 0, 100, []dag.Hash{genesis.Hash})
	store.Add(block1)

	for round := uint64(2); round <= 4; round++ {
		for v := byte(0); v < 3; v++ {
			id := byte(round*10 + uint64(v))
			prevRound := round - 1
			prevBlocks := []dag.Hash{testHash(byte(prevRound*10 + uint64(v)))}
			block := testVertex(id, round, uint32(v), 100, prevBlocks)
			store.Add(block)
		}
	}

	vt := NewVoteTracker(nil, store.Get, store.GetChildren)
	for i := uint32(0); i < 3; i++ {
		vt.RegisterValidator(i, 100)
	}

	for _, v := range store.GetAll() {
		_ = vt.ProcessBlock(v.Hash)
	}

	cr := NewCommitRule(nil, vt, store.Get)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cr.TryCommit(block1.Hash)
		// Reset for next iteration
		delete(cr.committed, block1.Hash)
	}
}
