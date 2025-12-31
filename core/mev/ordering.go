// Package mev implements MEV resistance for NovaCoin.
package mev

import (
	"crypto/rand"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
	"github.com/Supernova-NovaCoin/novacoin/crypto"
)

// OrderingState represents the state of a fair ordering round.
type OrderingState int

const (
	OrderingCollecting OrderingState = iota // Collecting transactions
	OrderingCommitting                      // Collecting commit messages
	OrderingRevealing                       // Collecting reveal messages
	OrderingComplete                        // Ordering determined
	OrderingFailed                          // Round failed (timeout/insufficient reveals)
)

func (s OrderingState) String() string {
	switch s {
	case OrderingCollecting:
		return "collecting"
	case OrderingCommitting:
		return "committing"
	case OrderingRevealing:
		return "revealing"
	case OrderingComplete:
		return "complete"
	case OrderingFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// CommitMessage represents a validator's commitment.
type CommitMessage struct {
	ValidatorIndex uint32
	Commitment     dag.Hash  // Hash(seed || validator_index)
	Timestamp      time.Time
}

// RevealMessage represents a validator's reveal.
type RevealMessage struct {
	ValidatorIndex uint32
	Seed           [32]byte
	Timestamp      time.Time
}

// OrderingRound tracks a single ordering round.
type OrderingRound struct {
	RoundID        dag.Hash
	State          OrderingState
	Transactions   []dag.Hash           // Encrypted tx hashes
	Commits        map[uint32]*CommitMessage
	Reveals        map[uint32]*RevealMessage
	CombinedSeed   [32]byte             // Combined from all reveals
	FinalOrder     []int                // Permutation indices
	StartTime      time.Time
	CommitDeadline time.Time
	RevealDeadline time.Time

	requiredCommits int
}

// OrderingConfig holds ordering configuration.
type OrderingConfig struct {
	CommitTimeout    time.Duration // Time allowed for commits
	RevealTimeout    time.Duration // Time allowed for reveals
	MinCommits       int           // Minimum commits required
	MinReveals       int           // Minimum reveals required (should be >= MinCommits)
}

// DefaultOrderingConfig returns default configuration.
func DefaultOrderingConfig() *OrderingConfig {
	return &OrderingConfig{
		CommitTimeout: 2 * time.Second,
		RevealTimeout: 2 * time.Second,
		MinCommits:    67, // 2/3 of 100
		MinReveals:    67,
	}
}

// FairOrderer implements fair transaction ordering.
type FairOrderer struct {
	config *OrderingConfig

	currentRound   *OrderingRound
	historicalRounds map[dag.Hash]*OrderingRound

	mu sync.RWMutex
}

// NewFairOrderer creates a new fair orderer.
func NewFairOrderer(config *OrderingConfig) *FairOrderer {
	if config == nil {
		config = DefaultOrderingConfig()
	}
	return &FairOrderer{
		config:           config,
		historicalRounds: make(map[dag.Hash]*OrderingRound),
	}
}

// StartRound starts a new ordering round.
func (fo *FairOrderer) StartRound(transactions []dag.Hash) (*OrderingRound, error) {
	fo.mu.Lock()
	defer fo.mu.Unlock()

	if fo.currentRound != nil && fo.currentRound.State != OrderingComplete && fo.currentRound.State != OrderingFailed {
		return nil, ErrRoundInProgress
	}

	now := time.Now()
	roundID := crypto.HashMultiple(
		[]byte("ordering_round"),
		[]byte(now.String()),
	)

	round := &OrderingRound{
		RoundID:         roundID,
		State:           OrderingCollecting,
		Transactions:    transactions,
		Commits:         make(map[uint32]*CommitMessage),
		Reveals:         make(map[uint32]*RevealMessage),
		StartTime:       now,
		CommitDeadline:  now.Add(fo.config.CommitTimeout),
		RevealDeadline:  now.Add(fo.config.CommitTimeout + fo.config.RevealTimeout),
		requiredCommits: fo.config.MinCommits,
	}

	fo.currentRound = round
	fo.historicalRounds[roundID] = round

	return round, nil
}

// GenerateSeed generates a random seed for commitment.
func (fo *FairOrderer) GenerateSeed() ([32]byte, error) {
	var seed [32]byte
	_, err := rand.Read(seed[:])
	if err != nil {
		return seed, err
	}
	return seed, nil
}

// CreateCommitment creates a commitment from a seed and validator index.
func (fo *FairOrderer) CreateCommitment(seed [32]byte, validatorIndex uint32) dag.Hash {
	// Commitment = Hash(seed || validator_index)
	indexBytes := []byte{
		byte(validatorIndex >> 24),
		byte(validatorIndex >> 16),
		byte(validatorIndex >> 8),
		byte(validatorIndex),
	}
	return crypto.HashMultiple(seed[:], indexBytes)
}

// SubmitCommit submits a commitment for the current round.
func (fo *FairOrderer) SubmitCommit(validatorIndex uint32, commitment dag.Hash) error {
	fo.mu.Lock()
	defer fo.mu.Unlock()

	if fo.currentRound == nil {
		return ErrNoActiveRound
	}

	round := fo.currentRound
	if round.State != OrderingCollecting && round.State != OrderingCommitting {
		return ErrInvalidState
	}

	if time.Now().After(round.CommitDeadline) {
		return ErrCommitDeadlinePassed
	}

	if _, exists := round.Commits[validatorIndex]; exists {
		return ErrAlreadyCommitted
	}

	round.Commits[validatorIndex] = &CommitMessage{
		ValidatorIndex: validatorIndex,
		Commitment:     commitment,
		Timestamp:      time.Now(),
	}

	// Transition to committing state if we have first commit
	if round.State == OrderingCollecting {
		round.State = OrderingCommitting
	}

	return nil
}

// TransitionToReveal transitions the round to reveal phase.
func (fo *FairOrderer) TransitionToReveal() error {
	fo.mu.Lock()
	defer fo.mu.Unlock()

	if fo.currentRound == nil {
		return ErrNoActiveRound
	}

	round := fo.currentRound
	if round.State != OrderingCommitting {
		return ErrInvalidState
	}

	if len(round.Commits) < fo.config.MinCommits {
		round.State = OrderingFailed
		return ErrInsufficientCommits
	}

	round.State = OrderingRevealing
	return nil
}

// SubmitReveal submits a reveal for the current round.
func (fo *FairOrderer) SubmitReveal(validatorIndex uint32, seed [32]byte) error {
	fo.mu.Lock()
	defer fo.mu.Unlock()

	if fo.currentRound == nil {
		return ErrNoActiveRound
	}

	round := fo.currentRound
	if round.State != OrderingRevealing {
		return ErrInvalidState
	}

	if time.Now().After(round.RevealDeadline) {
		return ErrRevealDeadlinePassed
	}

	// Verify the reveal matches the commitment
	commit, exists := round.Commits[validatorIndex]
	if !exists {
		return ErrNoCommitment
	}

	expectedCommitment := fo.CreateCommitment(seed, validatorIndex)
	if expectedCommitment != commit.Commitment {
		return ErrInvalidReveal
	}

	if _, exists := round.Reveals[validatorIndex]; exists {
		return ErrAlreadyRevealed
	}

	round.Reveals[validatorIndex] = &RevealMessage{
		ValidatorIndex: validatorIndex,
		Seed:           seed,
		Timestamp:      time.Now(),
	}

	return nil
}

// FinalizeOrdering finalizes the ordering using BlindPerm.
func (fo *FairOrderer) FinalizeOrdering() ([]int, error) {
	fo.mu.Lock()
	defer fo.mu.Unlock()

	if fo.currentRound == nil {
		return nil, ErrNoActiveRound
	}

	round := fo.currentRound
	if round.State != OrderingRevealing {
		return nil, ErrInvalidState
	}

	if len(round.Reveals) < fo.config.MinReveals {
		round.State = OrderingFailed
		return nil, ErrInsufficientReveals
	}

	// Combine all revealed seeds
	combinedSeed := fo.combineSeeds(round.Reveals)
	round.CombinedSeed = combinedSeed

	// Generate permutation using BlindPerm
	permutation := fo.blindPerm(len(round.Transactions), combinedSeed)
	round.FinalOrder = permutation
	round.State = OrderingComplete

	return permutation, nil
}

// combineSeeds combines all revealed seeds deterministically.
func (fo *FairOrderer) combineSeeds(reveals map[uint32]*RevealMessage) [32]byte {
	// Sort by validator index for determinism
	indices := make([]uint32, 0, len(reveals))
	for idx := range reveals {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool {
		return indices[i] < indices[j]
	})

	// XOR all seeds together, then hash
	var combined [32]byte
	for _, idx := range indices {
		seed := reveals[idx].Seed
		for i := 0; i < 32; i++ {
			combined[i] ^= seed[i]
		}
	}

	// Final hash to ensure uniform distribution
	return crypto.Hash(combined[:])
}

// blindPerm generates a deterministic permutation from a seed.
// Uses Fisher-Yates shuffle with the seed as RNG source.
func (fo *FairOrderer) blindPerm(n int, seed [32]byte) []int {
	if n == 0 {
		return []int{}
	}

	// Initialize identity permutation
	perm := make([]int, n)
	for i := 0; i < n; i++ {
		perm[i] = i
	}

	// Seed-based deterministic RNG
	rngState := seed
	getRandom := func(max int) int {
		// Hash state to get next value
		rngState = crypto.Hash(rngState[:])
		// Use first 8 bytes as uint64
		val := uint64(rngState[0])<<56 | uint64(rngState[1])<<48 |
			uint64(rngState[2])<<40 | uint64(rngState[3])<<32 |
			uint64(rngState[4])<<24 | uint64(rngState[5])<<16 |
			uint64(rngState[6])<<8 | uint64(rngState[7])
		return int(val % uint64(max))
	}

	// Fisher-Yates shuffle
	for i := n - 1; i > 0; i-- {
		j := getRandom(i + 1)
		perm[i], perm[j] = perm[j], perm[i]
	}

	return perm
}

// ApplyOrdering applies a permutation to transactions.
func (fo *FairOrderer) ApplyOrdering(transactions []dag.Hash, perm []int) []dag.Hash {
	if len(transactions) != len(perm) {
		return transactions
	}

	result := make([]dag.Hash, len(transactions))
	for i, idx := range perm {
		result[i] = transactions[idx]
	}
	return result
}

// GetCurrentRound returns the current ordering round.
func (fo *FairOrderer) GetCurrentRound() *OrderingRound {
	fo.mu.RLock()
	defer fo.mu.RUnlock()
	return fo.currentRound
}

// GetRound returns a round by ID.
func (fo *FairOrderer) GetRound(roundID dag.Hash) *OrderingRound {
	fo.mu.RLock()
	defer fo.mu.RUnlock()
	return fo.historicalRounds[roundID]
}

// GetCommitCount returns the number of commits in the current round.
func (fo *FairOrderer) GetCommitCount() int {
	fo.mu.RLock()
	defer fo.mu.RUnlock()
	if fo.currentRound == nil {
		return 0
	}
	return len(fo.currentRound.Commits)
}

// GetRevealCount returns the number of reveals in the current round.
func (fo *FairOrderer) GetRevealCount() int {
	fo.mu.RLock()
	defer fo.mu.RUnlock()
	if fo.currentRound == nil {
		return 0
	}
	return len(fo.currentRound.Reveals)
}

// Error types
var (
	ErrRoundInProgress       = errors.New("ordering round in progress")
	ErrNoActiveRound         = errors.New("no active ordering round")
	ErrInvalidState          = errors.New("invalid ordering state")
	ErrCommitDeadlinePassed  = errors.New("commit deadline passed")
	ErrRevealDeadlinePassed  = errors.New("reveal deadline passed")
	ErrAlreadyCommitted      = errors.New("validator already committed")
	ErrAlreadyRevealed       = errors.New("validator already revealed")
	ErrNoCommitment          = errors.New("no commitment found for validator")
	ErrInvalidReveal         = errors.New("reveal does not match commitment")
	ErrInsufficientCommits   = errors.New("insufficient commits")
	ErrInsufficientReveals   = errors.New("insufficient reveals")
)
