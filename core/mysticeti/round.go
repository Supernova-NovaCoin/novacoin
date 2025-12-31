// Package mysticeti implements the Mysticeti 3-round commit consensus protocol.
//
// Mysticeti achieves fast finality (~1.5s) through:
// - Uncertified DAG: Uses DAG structure to encode votes implicitly
// - Implicit Voting: Block references = votes (no separate voting messages)
// - 3-Round Commit: Blocks committed after reaching depth 3 with quorum support
// - 2-Round Fast Path: Optimistic commit in 2 rounds under good conditions
//
// The protocol provides Byzantine Fault Tolerance with 2f+1 stake required
// for safety and liveness, where f is the maximum Byzantine stake fraction.
package mysticeti

import (
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// RoundState represents the state of a consensus round.
type RoundState int

const (
	// RoundPending means the round hasn't started yet.
	RoundPending RoundState = iota
	// RoundProposing means validators are proposing blocks for this round.
	RoundProposing
	// RoundWaiting means waiting for blocks from other validators.
	RoundWaiting
	// RoundComplete means the round has enough participation to proceed.
	RoundComplete
	// RoundTimedOut means the round timed out waiting for blocks.
	RoundTimedOut
)

func (s RoundState) String() string {
	switch s {
	case RoundPending:
		return "pending"
	case RoundProposing:
		return "proposing"
	case RoundWaiting:
		return "waiting"
	case RoundComplete:
		return "complete"
	case RoundTimedOut:
		return "timed_out"
	default:
		return "unknown"
	}
}

// Round represents a single consensus round in Mysticeti.
type Round struct {
	// Round identity
	Number    uint64    // Round number (monotonically increasing)
	StartTime time.Time // When the round started
	EndTime   time.Time // When the round ended

	// State
	State RoundState

	// Blocks proposed in this round
	Blocks []dag.Hash // All block hashes in this round

	// Per-validator blocks (each validator proposes at most one per round)
	BlockByValidator map[uint32]dag.Hash

	// Leader for this round (anchor selection)
	LeaderIndex uint32
	LeaderBlock dag.Hash // The leader's block becomes the round anchor

	// Participation tracking
	TotalStake       uint64 // Total stake in the validator set
	ParticipatedStake uint64 // Stake that has proposed blocks
	StakeByValidator map[uint32]uint64

	// Equivocation detection
	Equivocators []uint32 // Validators who proposed multiple blocks
}

// NewRound creates a new round.
func NewRound(number uint64, leaderIndex uint32, totalStake uint64) *Round {
	return &Round{
		Number:           number,
		StartTime:        time.Now(),
		State:            RoundPending,
		Blocks:           make([]dag.Hash, 0),
		BlockByValidator: make(map[uint32]dag.Hash),
		LeaderIndex:      leaderIndex,
		TotalStake:       totalStake,
		StakeByValidator: make(map[uint32]uint64),
		Equivocators:     make([]uint32, 0),
	}
}

// AddBlock adds a block to this round from a validator.
// Returns true if this is a new block, false if duplicate/equivocation.
func (r *Round) AddBlock(hash dag.Hash, validatorIndex uint32, stake uint64) (bool, error) {
	// Check for equivocation (validator already proposed in this round)
	if existingHash, exists := r.BlockByValidator[validatorIndex]; exists {
		if existingHash != hash {
			// Equivocation detected!
			r.Equivocators = append(r.Equivocators, validatorIndex)
			return false, ErrEquivocation
		}
		// Duplicate, already have this block
		return false, nil
	}

	// Record the block
	r.Blocks = append(r.Blocks, hash)
	r.BlockByValidator[validatorIndex] = hash
	r.StakeByValidator[validatorIndex] = stake
	r.ParticipatedStake += stake

	// Check if this is the leader's block
	if validatorIndex == r.LeaderIndex {
		r.LeaderBlock = hash
	}

	return true, nil
}

// ParticipationRate returns the stake participation rate.
func (r *Round) ParticipationRate() float64 {
	if r.TotalStake == 0 {
		return 0
	}
	return float64(r.ParticipatedStake) / float64(r.TotalStake)
}

// HasQuorum returns true if the round has quorum participation (2/3+ stake).
func (r *Round) HasQuorum(threshold float64) bool {
	return r.ParticipationRate() >= threshold
}

// HasLeaderBlock returns true if the leader has proposed a block.
func (r *Round) HasLeaderBlock() bool {
	return !r.LeaderBlock.IsEmpty()
}

// Duration returns how long this round has been running.
func (r *Round) Duration() time.Duration {
	if r.EndTime.IsZero() {
		return time.Since(r.StartTime)
	}
	return r.EndTime.Sub(r.StartTime)
}

// HasEquivocation returns true if any validator equivocated.
func (r *Round) HasEquivocation() bool {
	return len(r.Equivocators) > 0
}

// RoundManager manages round progression in Mysticeti.
type RoundManager struct {
	config *RoundConfig

	// Current state
	currentRound *Round
	roundNumber  uint64

	// Round history
	rounds      map[uint64]*Round
	roundsOrder []uint64

	// Validator set
	validators   map[uint32]*ValidatorInfo
	totalStake   uint64
	leaderSchedule []uint32 // Pre-computed leader schedule

	// Synchronization
	highestSeenRound uint64 // Highest round number seen from peers

	// Callbacks
	onRoundComplete func(*Round)
	onRoundTimeout  func(*Round)

	mu sync.RWMutex
}

// ValidatorInfo holds validator information for round management.
type ValidatorInfo struct {
	Index    uint32
	PubKey   dag.PublicKey
	Stake    uint64
	IsActive bool
}

// RoundConfig holds configuration for round management.
type RoundConfig struct {
	// RoundTimeout is how long to wait for a round to complete.
	RoundTimeout time.Duration

	// MinRoundDuration is the minimum time a round must last.
	MinRoundDuration time.Duration

	// QuorumThreshold is the stake fraction required for quorum (default 2/3).
	QuorumThreshold float64

	// LeaderSelectionMode determines how leaders are selected.
	LeaderSelectionMode LeaderSelectionMode

	// MaxRoundHistory is the maximum rounds to keep in memory.
	MaxRoundHistory int

	// AllowEmptyRounds allows rounds with no leader block to complete.
	AllowEmptyRounds bool
}

// LeaderSelectionMode defines how round leaders are selected.
type LeaderSelectionMode int

const (
	// LeaderRoundRobin selects leaders in round-robin order.
	LeaderRoundRobin LeaderSelectionMode = iota
	// LeaderStakeWeighted selects leaders weighted by stake.
	LeaderStakeWeighted
	// LeaderRandom selects leaders pseudo-randomly based on round number.
	LeaderRandom
)

// DefaultRoundConfig returns default round configuration.
func DefaultRoundConfig() *RoundConfig {
	return &RoundConfig{
		RoundTimeout:        2 * time.Second,
		MinRoundDuration:    100 * time.Millisecond,
		QuorumThreshold:     2.0 / 3.0,
		LeaderSelectionMode: LeaderRoundRobin,
		MaxRoundHistory:     1000,
		AllowEmptyRounds:    false,
	}
}

// NewRoundManager creates a new round manager.
func NewRoundManager(config *RoundConfig) *RoundManager {
	if config == nil {
		config = DefaultRoundConfig()
	}

	return &RoundManager{
		config:      config,
		roundNumber: 0,
		rounds:      make(map[uint64]*Round),
		roundsOrder: make([]uint64, 0),
		validators:  make(map[uint32]*ValidatorInfo),
	}
}

// RegisterValidator registers a validator.
func (rm *RoundManager) RegisterValidator(index uint32, pubKey dag.PublicKey, stake uint64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.validators[index] = &ValidatorInfo{
		Index:    index,
		PubKey:   pubKey,
		Stake:    stake,
		IsActive: true,
	}
	rm.totalStake += stake

	// Rebuild leader schedule
	rm.rebuildLeaderSchedule()
}

// UpdateValidatorStake updates a validator's stake.
func (rm *RoundManager) UpdateValidatorStake(index uint32, stake uint64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if v, ok := rm.validators[index]; ok {
		rm.totalStake -= v.Stake
		v.Stake = stake
		rm.totalStake += stake
		rm.rebuildLeaderSchedule()
	}
}

// rebuildLeaderSchedule computes the leader schedule based on selection mode.
func (rm *RoundManager) rebuildLeaderSchedule() {
	// Collect active validator indices
	var indices []uint32
	for idx, v := range rm.validators {
		if v.IsActive {
			indices = append(indices, idx)
		}
	}

	if len(indices) == 0 {
		rm.leaderSchedule = nil
		return
	}

	switch rm.config.LeaderSelectionMode {
	case LeaderRoundRobin:
		// Simple round-robin
		rm.leaderSchedule = indices

	case LeaderStakeWeighted:
		// Repeat validators proportional to stake
		schedule := make([]uint32, 0)
		minStake := uint64(1 << 60)
		for _, v := range rm.validators {
			if v.IsActive && v.Stake < minStake {
				minStake = v.Stake
			}
		}
		if minStake == 0 {
			minStake = 1
		}

		for _, idx := range indices {
			v := rm.validators[idx]
			weight := int(v.Stake / minStake)
			if weight < 1 {
				weight = 1
			}
			if weight > 10 {
				weight = 10 // Cap to prevent extreme imbalance
			}
			for i := 0; i < weight; i++ {
				schedule = append(schedule, idx)
			}
		}
		rm.leaderSchedule = schedule

	case LeaderRandom:
		// For random, we use round number as seed in SelectLeader
		rm.leaderSchedule = indices
	}
}

// SelectLeader returns the leader for a given round.
func (rm *RoundManager) SelectLeader(roundNumber uint64) uint32 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.selectLeaderLocked(roundNumber)
}

// selectLeaderLocked returns the leader without acquiring a lock.
// Caller must hold rm.mu.
func (rm *RoundManager) selectLeaderLocked(roundNumber uint64) uint32 {
	if len(rm.leaderSchedule) == 0 {
		return 0
	}

	switch rm.config.LeaderSelectionMode {
	case LeaderRandom:
		// Use round number as pseudo-random seed
		idx := (roundNumber * 2654435761) % uint64(len(rm.leaderSchedule))
		return rm.leaderSchedule[idx]
	default:
		// Round-robin or stake-weighted (both use schedule)
		idx := roundNumber % uint64(len(rm.leaderSchedule))
		return rm.leaderSchedule[idx]
	}
}

// StartRound starts a new round.
func (rm *RoundManager) StartRound() *Round {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.roundNumber++
	leader := rm.selectLeaderLocked(rm.roundNumber)

	round := NewRound(rm.roundNumber, leader, rm.totalStake)
	round.State = RoundProposing

	rm.currentRound = round
	rm.rounds[rm.roundNumber] = round
	rm.roundsOrder = append(rm.roundsOrder, rm.roundNumber)

	// Prune old rounds
	rm.pruneRounds()

	return round
}

// pruneRounds removes old rounds beyond history limit.
func (rm *RoundManager) pruneRounds() {
	if len(rm.roundsOrder) <= rm.config.MaxRoundHistory {
		return
	}

	toRemove := len(rm.roundsOrder) - rm.config.MaxRoundHistory
	for i := 0; i < toRemove; i++ {
		delete(rm.rounds, rm.roundsOrder[i])
	}
	rm.roundsOrder = rm.roundsOrder[toRemove:]
}

// CurrentRound returns the current round.
func (rm *RoundManager) CurrentRound() *Round {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.currentRound
}

// CurrentRoundNumber returns the current round number.
func (rm *RoundManager) CurrentRoundNumber() uint64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.roundNumber
}

// GetRound returns a round by number.
func (rm *RoundManager) GetRound(number uint64) *Round {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.rounds[number]
}

// AddBlock adds a block to the current round.
func (rm *RoundManager) AddBlock(hash dag.Hash, validatorIndex uint32, roundNumber uint64) (bool, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Get the target round
	round := rm.rounds[roundNumber]
	if round == nil {
		// Round doesn't exist yet, might be from the future
		if roundNumber > rm.roundNumber {
			rm.highestSeenRound = max(rm.highestSeenRound, roundNumber)
			return false, ErrFutureRound
		}
		return false, ErrRoundNotFound
	}

	// Get validator stake
	validator := rm.validators[validatorIndex]
	if validator == nil {
		return false, ErrUnknownValidator
	}

	// Add block to round
	isNew, err := round.AddBlock(hash, validatorIndex, validator.Stake)
	if err != nil {
		return false, err
	}

	// Check if round should complete
	if round.HasQuorum(rm.config.QuorumThreshold) && round.State == RoundProposing {
		round.State = RoundWaiting
	}

	return isNew, nil
}

// CompleteRound marks the current round as complete.
func (rm *RoundManager) CompleteRound() (*Round, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.currentRound == nil {
		return nil, ErrNoActiveRound
	}

	round := rm.currentRound

	// Check completion conditions
	if !rm.config.AllowEmptyRounds && !round.HasLeaderBlock() {
		return nil, ErrNoLeaderBlock
	}

	if round.Duration() < rm.config.MinRoundDuration {
		return nil, ErrRoundTooShort
	}

	// Mark complete
	round.State = RoundComplete
	round.EndTime = time.Now()

	// Notify callback
	if rm.onRoundComplete != nil {
		go rm.onRoundComplete(round)
	}

	return round, nil
}

// TimeoutRound marks the current round as timed out.
func (rm *RoundManager) TimeoutRound() (*Round, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.currentRound == nil {
		return nil, ErrNoActiveRound
	}

	round := rm.currentRound
	round.State = RoundTimedOut
	round.EndTime = time.Now()

	// Notify callback
	if rm.onRoundTimeout != nil {
		go rm.onRoundTimeout(round)
	}

	return round, nil
}

// CheckTimeout returns true if the current round has timed out.
func (rm *RoundManager) CheckTimeout() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if rm.currentRound == nil {
		return false
	}

	return rm.currentRound.Duration() >= rm.config.RoundTimeout
}

// ShouldAdvance returns true if we should advance to the next round.
func (rm *RoundManager) ShouldAdvance() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if rm.currentRound == nil {
		return true // No round, should start one
	}

	round := rm.currentRound

	// Advance if round is complete or timed out
	if round.State == RoundComplete || round.State == RoundTimedOut {
		return true
	}

	// Advance if we have quorum and minimum duration has passed
	if round.HasQuorum(rm.config.QuorumThreshold) &&
		round.Duration() >= rm.config.MinRoundDuration {
		return true
	}

	return false
}

// GetBlocksForRound returns all blocks in a specific round.
func (rm *RoundManager) GetBlocksForRound(roundNumber uint64) []dag.Hash {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	round := rm.rounds[roundNumber]
	if round == nil {
		return nil
	}

	blocks := make([]dag.Hash, len(round.Blocks))
	copy(blocks, round.Blocks)
	return blocks
}

// GetLeaderBlock returns the leader's block for a round.
func (rm *RoundManager) GetLeaderBlock(roundNumber uint64) dag.Hash {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	round := rm.rounds[roundNumber]
	if round == nil {
		return dag.Hash{}
	}

	return round.LeaderBlock
}

// OnRoundComplete sets a callback for round completion.
func (rm *RoundManager) OnRoundComplete(callback func(*Round)) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onRoundComplete = callback
}

// OnRoundTimeout sets a callback for round timeout.
func (rm *RoundManager) OnRoundTimeout(callback func(*Round)) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onRoundTimeout = callback
}

// GetHighestSeenRound returns the highest round number seen from peers.
func (rm *RoundManager) GetHighestSeenRound() uint64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.highestSeenRound
}

// IsBehind returns true if we're behind the highest seen round.
func (rm *RoundManager) IsBehind() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.roundNumber < rm.highestSeenRound
}

// RoundStatistics holds round manager statistics.
type RoundStatistics struct {
	CurrentRound       uint64
	CompletedRounds    uint64
	TimedOutRounds     uint64
	TotalBlocks        uint64
	AvgParticipation   float64
	AvgRoundDuration   time.Duration
	EquivocationCount  int
}

// GetStatistics returns round manager statistics.
func (rm *RoundManager) GetStatistics() *RoundStatistics {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	stats := &RoundStatistics{
		CurrentRound: rm.roundNumber,
	}

	var totalParticipation float64
	var totalDuration time.Duration
	var participationCount int

	for _, round := range rm.rounds {
		stats.TotalBlocks += uint64(len(round.Blocks))
		stats.EquivocationCount += len(round.Equivocators)

		if round.State == RoundComplete {
			stats.CompletedRounds++
			totalDuration += round.Duration()
		} else if round.State == RoundTimedOut {
			stats.TimedOutRounds++
		}

		if round.ParticipationRate() > 0 {
			totalParticipation += round.ParticipationRate()
			participationCount++
		}
	}

	if stats.CompletedRounds > 0 {
		stats.AvgRoundDuration = totalDuration / time.Duration(stats.CompletedRounds)
	}
	if participationCount > 0 {
		stats.AvgParticipation = totalParticipation / float64(participationCount)
	}

	return stats
}

// Error types for round operations.
var (
	ErrEquivocation     = roundError("equivocation detected: validator proposed multiple blocks")
	ErrFutureRound      = roundError("block is from a future round")
	ErrRoundNotFound    = roundError("round not found")
	ErrUnknownValidator = roundError("unknown validator")
	ErrNoActiveRound    = roundError("no active round")
	ErrNoLeaderBlock    = roundError("no leader block in round")
	ErrRoundTooShort    = roundError("round duration too short")
)

type roundError string

func (e roundError) Error() string {
	return string(e)
}

// max returns the maximum of two uint64 values.
func max(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
