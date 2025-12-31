// Package mysticeti implements the Mysticeti 3-round commit consensus protocol.
//
// Mysticeti is a high-performance BFT consensus protocol that achieves
// fast finality (~1.5s) through an uncertified DAG approach where:
//
// 1. Blocks are broadcast without waiting for certificates
// 2. Votes are implicit through DAG parent references
// 3. Commits happen after observing 3 rounds of quorum support
// 4. A 2-round fast path is available under good conditions
//
// The protocol provides:
// - Byzantine Fault Tolerance (tolerates up to 1/3 malicious stake)
// - Fast finality (1.5s typical, 1s with fast path)
// - High throughput (inherits from underlying DAG structure)
// - Pipelining (multiple blocks can be in-flight)
package mysticeti

import (
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// Engine is the main Mysticeti consensus engine.
// It coordinates rounds, votes, and commits for fast finality.
type Engine struct {
	config *Config

	// Core components
	roundManager *RoundManager
	voteTracker  *VoteTracker
	commitRule   *CommitRule

	// DAG storage interface
	store VertexStore

	// State
	running bool

	// Channels for coordination
	blockCh      chan *dag.Vertex
	commitCh     chan *CommitCertificate
	roundEventCh chan roundEvent
	stopCh       chan struct{}

	// Callbacks
	onBlockCommit func(*dag.Vertex, *CommitCertificate)
	onRoundAdvance func(*Round)

	// Metrics
	metrics *Metrics

	mu sync.RWMutex
}

// roundEvent represents an event in round processing.
type roundEvent struct {
	eventType roundEventType
	round     *Round
}

type roundEventType int

const (
	roundEventComplete roundEventType = iota
	roundEventTimeout
)

// VertexStore is the interface for DAG storage.
type VertexStore interface {
	Get(hash dag.Hash) *dag.Vertex
	GetAll() []*dag.Vertex
	GetChildren(hash dag.Hash) []*dag.Vertex
	GetTips() []*dag.Vertex
}

// Config holds the complete Mysticeti configuration.
type Config struct {
	// Round configuration
	Round *RoundConfig

	// Vote configuration
	Vote *VoteConfig

	// Commit configuration
	Commit *CommitConfig

	// Timing
	RoundInterval    time.Duration // How often to check round progression
	CommitInterval   time.Duration // How often to check for commits

	// Finality target
	TargetFinality time.Duration // Target finality time (~1.5s)
}

// DefaultConfig returns default Mysticeti configuration.
func DefaultConfig() *Config {
	return &Config{
		Round:          DefaultRoundConfig(),
		Vote:           DefaultVoteConfig(),
		Commit:         DefaultCommitConfig(),
		RoundInterval:  50 * time.Millisecond,
		CommitInterval: 100 * time.Millisecond,
		TargetFinality: 1500 * time.Millisecond,
	}
}

// Metrics holds Mysticeti performance metrics.
type Metrics struct {
	RoundsCompleted     uint64
	RoundsTimedOut      uint64
	BlocksProposed      uint64
	BlocksCommitted     uint64
	FastPathCommits     uint64
	StandardPathCommits uint64
	AvgFinalityTime     time.Duration
	AvgRoundTime        time.Duration
	EquivocationsFound  uint64

	mu sync.RWMutex
}

// NewEngine creates a new Mysticeti engine.
func NewEngine(config *Config, store VertexStore) *Engine {
	if config == nil {
		config = DefaultConfig()
	}

	// Create vote tracker with DAG interface
	voteTracker := NewVoteTracker(
		config.Vote,
		store.Get,
		store.GetChildren,
	)

	// Create commit rule with vote tracker
	commitRule := NewCommitRule(
		config.Commit,
		voteTracker,
		store.Get,
	)

	engine := &Engine{
		config:       config,
		store:        store,
		roundManager: NewRoundManager(config.Round),
		voteTracker:  voteTracker,
		commitRule:   commitRule,
		blockCh:      make(chan *dag.Vertex, 1000),
		commitCh:     make(chan *CommitCertificate, 100),
		roundEventCh: make(chan roundEvent, 100),
		stopCh:       make(chan struct{}),
		metrics:      &Metrics{},
	}

	// Set up internal callbacks
	engine.roundManager.OnRoundComplete(func(r *Round) {
		engine.roundEventCh <- roundEvent{roundEventComplete, r}
	})
	engine.roundManager.OnRoundTimeout(func(r *Round) {
		engine.roundEventCh <- roundEvent{roundEventTimeout, r}
	})
	engine.commitRule.OnCommit(func(cert *CommitCertificate) {
		engine.commitCh <- cert
	})

	return engine
}

// Start begins the Mysticeti consensus engine.
func (e *Engine) Start() error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return ErrAlreadyRunning
	}
	e.running = true
	e.mu.Unlock()

	// Start first round
	e.roundManager.StartRound()

	// Start processing loops
	go e.roundLoop()
	go e.blockLoop()
	go e.commitLoop()
	go e.eventLoop()

	return nil
}

// Stop halts the Mysticeti engine.
func (e *Engine) Stop() error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = false
	e.mu.Unlock()

	close(e.stopCh)
	return nil
}

// roundLoop manages round progression.
func (e *Engine) roundLoop() {
	ticker := time.NewTicker(e.config.RoundInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.processRound()
		}
	}
}

// processRound handles round state and transitions.
func (e *Engine) processRound() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check for timeout
	if e.roundManager.CheckTimeout() {
		_, _ = e.roundManager.TimeoutRound()
		e.metrics.mu.Lock()
		e.metrics.RoundsTimedOut++
		e.metrics.mu.Unlock()
	}

	// Check if we should advance to next round
	if e.roundManager.ShouldAdvance() {
		// Complete current round if needed
		if round := e.roundManager.CurrentRound(); round != nil {
			if round.State == RoundProposing || round.State == RoundWaiting {
				_, _ = e.roundManager.CompleteRound()
			}
		}

		// Start new round
		newRound := e.roundManager.StartRound()

		// Notify callback
		if e.onRoundAdvance != nil {
			go e.onRoundAdvance(newRound)
		}

		e.metrics.mu.Lock()
		e.metrics.RoundsCompleted++
		e.metrics.mu.Unlock()
	}
}

// blockLoop processes incoming blocks.
func (e *Engine) blockLoop() {
	for {
		select {
		case <-e.stopCh:
			return
		case vertex := <-e.blockCh:
			e.processBlock(vertex)
		}
	}
}

// processBlock handles a new block.
func (e *Engine) processBlock(vertex *dag.Vertex) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Add to round manager
	_, err := e.roundManager.AddBlock(vertex.Hash, vertex.ValidatorIndex, vertex.Round)
	if err != nil {
		if err == ErrEquivocation {
			e.metrics.mu.Lock()
			e.metrics.EquivocationsFound++
			e.metrics.mu.Unlock()
		}
		return
	}

	// Process implicit votes
	_ = e.voteTracker.ProcessBlock(vertex.Hash)

	// Add to pending commits
	e.commitRule.AddPending(vertex.Hash, vertex.Round)

	e.metrics.mu.Lock()
	e.metrics.BlocksProposed++
	e.metrics.mu.Unlock()
}

// commitLoop checks for blocks that can be committed.
func (e *Engine) commitLoop() {
	ticker := time.NewTicker(e.config.CommitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.processCommits()
		}
	}
}

// processCommits checks and processes pending commits.
func (e *Engine) processCommits() {
	e.mu.Lock()
	currentRound := e.roundManager.CurrentRoundNumber()
	e.mu.Unlock()

	// Check leader blocks for commit
	getLeaderBlock := func(round uint64) dag.Hash {
		return e.roundManager.GetLeaderBlock(round)
	}

	newCommits := e.commitRule.CheckAndCommitLeaders(currentRound, getLeaderBlock)

	// Update metrics
	for _, cert := range newCommits {
		e.metrics.mu.Lock()
		e.metrics.BlocksCommitted++
		if cert.Status == CommitFastPath {
			e.metrics.FastPathCommits++
		} else if cert.Status == CommitStandard {
			e.metrics.StandardPathCommits++
		}
		e.metrics.mu.Unlock()

		// Notify callback
		if e.onBlockCommit != nil {
			vertex := e.store.Get(cert.Block)
			if vertex != nil {
				go e.onBlockCommit(vertex, cert)
			}
		}
	}

	// Also try to commit pending blocks
	e.commitRule.ProcessPending()
}

// eventLoop handles round events.
func (e *Engine) eventLoop() {
	for {
		select {
		case <-e.stopCh:
			return
		case event := <-e.roundEventCh:
			switch event.eventType {
			case roundEventComplete:
				e.handleRoundComplete(event.round)
			case roundEventTimeout:
				e.handleRoundTimeout(event.round)
			}
		}
	}
}

// handleRoundComplete handles a completed round.
func (e *Engine) handleRoundComplete(round *Round) {
	// Update metrics
	e.metrics.mu.Lock()
	if e.metrics.AvgRoundTime == 0 {
		e.metrics.AvgRoundTime = round.Duration()
	} else {
		e.metrics.AvgRoundTime = (e.metrics.AvgRoundTime*9 + round.Duration()) / 10
	}
	e.metrics.mu.Unlock()
}

// handleRoundTimeout handles a timed out round.
func (e *Engine) handleRoundTimeout(round *Round) {
	// Log or handle timeout
	// Could trigger view change or recovery in some implementations
}

// === Public API ===

// ProposeBlock proposes a new block for the current round.
func (e *Engine) ProposeBlock(vertex *dag.Vertex) error {
	e.mu.RLock()
	if !e.running {
		e.mu.RUnlock()
		return ErrNotRunning
	}
	e.mu.RUnlock()

	// Set round number to current round
	vertex.Round = e.roundManager.CurrentRoundNumber()

	// Set implicit votes (parents)
	vertex.ImplicitVotes = vertex.Parents

	// Submit for processing
	select {
	case e.blockCh <- vertex:
		return nil
	default:
		return ErrBlockChannelFull
	}
}

// ReceiveBlock processes a block received from the network.
func (e *Engine) ReceiveBlock(vertex *dag.Vertex) error {
	e.mu.RLock()
	if !e.running {
		e.mu.RUnlock()
		return ErrNotRunning
	}
	e.mu.RUnlock()

	// Submit for processing
	select {
	case e.blockCh <- vertex:
		return nil
	default:
		return ErrBlockChannelFull
	}
}

// RegisterValidator registers a validator for consensus.
func (e *Engine) RegisterValidator(index uint32, pubKey dag.PublicKey, stake uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.roundManager.RegisterValidator(index, pubKey, stake)
	e.voteTracker.RegisterValidator(index, stake)
}

// UpdateValidatorStake updates a validator's stake.
func (e *Engine) UpdateValidatorStake(index uint32, stake uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.roundManager.UpdateValidatorStake(index, stake)
	e.voteTracker.RegisterValidator(index, stake)
}

// GetCurrentRound returns the current round.
func (e *Engine) GetCurrentRound() *Round {
	return e.roundManager.CurrentRound()
}

// GetCurrentRoundNumber returns the current round number.
func (e *Engine) GetCurrentRoundNumber() uint64 {
	return e.roundManager.CurrentRoundNumber()
}

// GetRound returns a round by number.
func (e *Engine) GetRound(number uint64) *Round {
	return e.roundManager.GetRound(number)
}

// IsCommitted returns true if a block has been committed.
func (e *Engine) IsCommitted(hash dag.Hash) bool {
	return e.commitRule.IsCommitted(hash)
}

// GetCommitCertificate returns the commit certificate for a block.
func (e *Engine) GetCommitCertificate(hash dag.Hash) *CommitCertificate {
	return e.commitRule.GetCertificate(hash)
}

// GetCommitOrder returns the order of committed blocks.
func (e *Engine) GetCommitOrder() []dag.Hash {
	return e.commitRule.GetCommitOrder()
}

// GetLastCommit returns the most recently committed block.
func (e *Engine) GetLastCommit() dag.Hash {
	return e.commitRule.GetLastCommit()
}

// GetVoteTally returns the vote tally for a block.
func (e *Engine) GetVoteTally(hash dag.Hash) *VoteTally {
	return e.voteTracker.GetVoteTally(hash)
}

// GetQuorumDepth returns the maximum quorum depth for a block.
func (e *Engine) GetQuorumDepth(hash dag.Hash) int {
	return e.voteTracker.GetQuorumDepth(hash)
}

// === Callbacks ===

// OnBlockCommit sets a callback for when a block is committed.
func (e *Engine) OnBlockCommit(callback func(*dag.Vertex, *CommitCertificate)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onBlockCommit = callback
}

// OnRoundAdvance sets a callback for when a new round starts.
func (e *Engine) OnRoundAdvance(callback func(*Round)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onRoundAdvance = callback
}

// === Statistics ===

// GetMetrics returns current metrics.
func (e *Engine) GetMetrics() *Metrics {
	e.metrics.mu.RLock()
	defer e.metrics.mu.RUnlock()

	return &Metrics{
		RoundsCompleted:     e.metrics.RoundsCompleted,
		RoundsTimedOut:      e.metrics.RoundsTimedOut,
		BlocksProposed:      e.metrics.BlocksProposed,
		BlocksCommitted:     e.metrics.BlocksCommitted,
		FastPathCommits:     e.metrics.FastPathCommits,
		StandardPathCommits: e.metrics.StandardPathCommits,
		AvgFinalityTime:     e.metrics.AvgFinalityTime,
		AvgRoundTime:        e.metrics.AvgRoundTime,
		EquivocationsFound:  e.metrics.EquivocationsFound,
	}
}

// GetRoundStatistics returns round manager statistics.
func (e *Engine) GetRoundStatistics() *RoundStatistics {
	return e.roundManager.GetStatistics()
}

// GetVoteStatistics returns vote tracker statistics.
func (e *Engine) GetVoteStatistics() *VoteStatistics {
	return e.voteTracker.GetStatistics()
}

// GetCommitStatistics returns commit rule statistics.
func (e *Engine) GetCommitStatistics() *CommitStatistics {
	return e.commitRule.GetStatistics()
}

// ComputeExpectedFinality estimates the expected finality time.
func (e *Engine) ComputeExpectedFinality() time.Duration {
	stats := e.commitRule.GetStatistics()

	// Base finality is standard depth * round time
	avgRoundTime := e.metrics.AvgRoundTime
	if avgRoundTime == 0 {
		avgRoundTime = e.config.Round.RoundTimeout / 2
	}

	// If fast path is working well, use fast path depth
	if stats.FastPathRate > 0.5 {
		return avgRoundTime * time.Duration(e.config.Commit.FastPathDepth)
	}

	return avgRoundTime * time.Duration(e.config.Commit.StandardDepth)
}

// Error types for engine operations.
var (
	ErrAlreadyRunning   = engineError("engine already running")
	ErrNotRunning       = engineError("engine not running")
	ErrBlockChannelFull = engineError("block channel is full")
)

type engineError string

func (e engineError) Error() string {
	return string(e)
}

// FinalityStatus represents the finality status of a block.
type FinalityStatus struct {
	Block           dag.Hash
	Round           uint64
	IsCommitted     bool
	CommitStatus    CommitStatus
	QuorumDepth     int
	DirectVoteStake uint64
	TotalVoteStake  uint64
	TotalStake      uint64
	EstimatedFinality time.Duration
}

// GetFinalityStatus returns the finality status of a block.
func (e *Engine) GetFinalityStatus(hash dag.Hash) *FinalityStatus {
	vertex := e.store.Get(hash)
	if vertex == nil {
		return nil
	}

	status := &FinalityStatus{
		Block:      hash,
		Round:      vertex.Round,
		TotalStake: e.voteTracker.GetTotalStake(),
	}

	// Check if committed
	cert := e.commitRule.GetCertificate(hash)
	if cert != nil {
		status.IsCommitted = cert.Status == CommitFastPath || cert.Status == CommitStandard
		status.CommitStatus = cert.Status
	}

	// Get vote info
	tally := e.voteTracker.GetVoteTally(hash)
	if tally != nil {
		status.DirectVoteStake = tally.DirectVoteStake
		status.TotalVoteStake = tally.TotalVoteStake
	}

	status.QuorumDepth = e.voteTracker.GetQuorumDepth(hash)

	// Estimate remaining finality time
	if !status.IsCommitted {
		currentRound := e.roundManager.CurrentRoundNumber()
		roundsNeeded := e.config.Commit.StandardDepth - int(currentRound-vertex.Round)
		if roundsNeeded < 0 {
			roundsNeeded = 0
		}
		avgRoundTime := e.metrics.AvgRoundTime
		if avgRoundTime == 0 {
			avgRoundTime = 500 * time.Millisecond
		}
		status.EstimatedFinality = avgRoundTime * time.Duration(roundsNeeded)
	}

	return status
}
