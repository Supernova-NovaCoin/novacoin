// Package shoal implements the Shoal++ multi-anchor high-throughput consensus layer.
package shoal

import (
	"sort"
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// AnchorState represents the state of an anchor in a wave.
type AnchorState int

const (
	// AnchorPending means the anchor is waiting to be broadcast.
	AnchorPending AnchorState = iota
	// AnchorBroadcast means the anchor has been broadcast to the network.
	AnchorBroadcast
	// AnchorReceived means the anchor has been received by other validators.
	AnchorReceived
	// AnchorCertified means the anchor has received quorum votes.
	AnchorCertified
	// AnchorCommitted means the anchor was selected and committed.
	AnchorCommitted
	// AnchorOrphaned means the anchor was not selected in its wave.
	AnchorOrphaned
)

func (s AnchorState) String() string {
	switch s {
	case AnchorPending:
		return "pending"
	case AnchorBroadcast:
		return "broadcast"
	case AnchorReceived:
		return "received"
	case AnchorCertified:
		return "certified"
	case AnchorCommitted:
		return "committed"
	case AnchorOrphaned:
		return "orphaned"
	default:
		return "unknown"
	}
}

// Anchor represents an anchor block in Shoal++.
// In Shoal++, all validators are anchors and can propose blocks.
type Anchor struct {
	// Identity
	Hash           dag.Hash     // Block hash
	ValidatorIndex uint32       // Validator who proposed this anchor
	ValidatorPK    dag.PublicKey // Validator's public key
	Stake          uint64       // Validator's stake

	// Wave info
	Wave         uint64 // Wave number this anchor belongs to
	ParallelDAG  uint8  // Which parallel DAG (0-3)
	ProposedTime time.Time

	// State
	State AnchorState

	// References
	References []dag.Hash // Other anchors this one references (DAG links)
	ReferencedBy []dag.Hash // Anchors that reference this one

	// Votes received
	VotesReceived uint64 // Total stake that voted for this anchor
	VoterIndices  []uint32 // Indices of validators who voted

	// Ordering
	OrderPosition int // Position in the wave order (-1 if not ordered)
}

// NewAnchor creates a new anchor for a validator.
func NewAnchor(
	hash dag.Hash,
	validatorIndex uint32,
	validatorPK dag.PublicKey,
	stake uint64,
	wave uint64,
	parallelDAG uint8,
) *Anchor {
	return &Anchor{
		Hash:           hash,
		ValidatorIndex: validatorIndex,
		ValidatorPK:    validatorPK,
		Stake:          stake,
		Wave:           wave,
		ParallelDAG:    parallelDAG,
		ProposedTime:   time.Now(),
		State:          AnchorPending,
		References:     make([]dag.Hash, 0),
		ReferencedBy:   make([]dag.Hash, 0),
		VoterIndices:   make([]uint32, 0),
		OrderPosition:  -1,
	}
}

// AddReference adds a reference to another anchor.
func (a *Anchor) AddReference(hash dag.Hash) {
	a.References = append(a.References, hash)
}

// AddVote records a vote from another validator.
func (a *Anchor) AddVote(validatorIndex uint32, stake uint64) {
	a.VotesReceived += stake
	a.VoterIndices = append(a.VoterIndices, validatorIndex)
}

// AnchorManager manages anchors across waves and parallel DAGs.
type AnchorManager struct {
	config *AnchorConfig

	// Anchors by hash
	anchors map[dag.Hash]*Anchor

	// Anchors by wave
	anchorsByWave map[uint64][]*Anchor

	// Anchors by validator (latest per wave)
	anchorsByValidator map[uint32]map[uint64]*Anchor

	// Anchors by parallel DAG
	anchorsByDAG map[uint8][]*Anchor

	// Validator info
	validators map[uint32]*ValidatorInfo

	// Current wave context
	currentWave uint64
	totalStake  uint64

	mu sync.RWMutex
}

// ValidatorInfo holds information about a validator anchor producer.
type ValidatorInfo struct {
	Index    uint32
	PubKey   dag.PublicKey
	Stake    uint64
	IsActive bool

	// Performance metrics
	AnchorsProposed uint64
	AnchorsCommitted uint64
	LastAnchorWave  uint64
	MissedWaves     uint64
}

// AnchorConfig holds configuration for anchor management.
type AnchorConfig struct {
	// NumParallelDAGs is the number of parallel DAGs (default 4).
	NumParallelDAGs int

	// StaggerOffset is the time offset between parallel DAGs.
	StaggerOffset time.Duration

	// MaxAnchorsPerWave is the maximum anchors per wave per validator.
	MaxAnchorsPerWave int

	// AnchorTimeout is how long to wait for an anchor to be certified.
	AnchorTimeout time.Duration

	// MaxAnchorHistory is the maximum anchors to keep in memory.
	MaxAnchorHistory int
}

// DefaultAnchorConfig returns default anchor configuration.
func DefaultAnchorConfig() *AnchorConfig {
	return &AnchorConfig{
		NumParallelDAGs:   4,
		StaggerOffset:     125 * time.Millisecond,
		MaxAnchorsPerWave: 1,
		AnchorTimeout:     2 * time.Second,
		MaxAnchorHistory:  100000,
	}
}

// NewAnchorManager creates a new anchor manager.
func NewAnchorManager(config *AnchorConfig) *AnchorManager {
	if config == nil {
		config = DefaultAnchorConfig()
	}

	return &AnchorManager{
		config:             config,
		anchors:           make(map[dag.Hash]*Anchor),
		anchorsByWave:     make(map[uint64][]*Anchor),
		anchorsByValidator: make(map[uint32]map[uint64]*Anchor),
		anchorsByDAG:      make(map[uint8][]*Anchor),
		validators:        make(map[uint32]*ValidatorInfo),
	}
}

// RegisterValidator registers a validator as an anchor producer.
func (am *AnchorManager) RegisterValidator(index uint32, pubKey dag.PublicKey, stake uint64) {
	am.mu.Lock()
	defer am.mu.Unlock()

	am.validators[index] = &ValidatorInfo{
		Index:    index,
		PubKey:   pubKey,
		Stake:    stake,
		IsActive: true,
	}

	am.anchorsByValidator[index] = make(map[uint64]*Anchor)
	am.totalStake += stake
}

// UpdateValidatorStake updates a validator's stake.
func (am *AnchorManager) UpdateValidatorStake(index uint32, stake uint64) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if v, ok := am.validators[index]; ok {
		am.totalStake -= v.Stake
		v.Stake = stake
		am.totalStake += stake
	}
}

// SetCurrentWave sets the current wave number.
func (am *AnchorManager) SetCurrentWave(wave uint64) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.currentWave = wave
}

// CreateAnchor creates a new anchor block for a validator.
func (am *AnchorManager) CreateAnchor(
	hash dag.Hash,
	validatorIndex uint32,
	wave uint64,
	parallelDAG uint8,
	references []dag.Hash,
) (*Anchor, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	// Validate validator
	validator, ok := am.validators[validatorIndex]
	if !ok {
		return nil, ErrUnknownValidator
	}

	if !validator.IsActive {
		return nil, ErrValidatorInactive
	}

	// Check if already anchored in this wave
	if _, exists := am.anchorsByValidator[validatorIndex][wave]; exists {
		return nil, ErrAlreadyAnchored
	}

	// Create anchor
	anchor := NewAnchor(hash, validatorIndex, validator.PubKey, validator.Stake, wave, parallelDAG)
	anchor.References = references
	anchor.State = AnchorBroadcast

	// Store anchor
	am.anchors[hash] = anchor
	am.anchorsByWave[wave] = append(am.anchorsByWave[wave], anchor)
	am.anchorsByValidator[validatorIndex][wave] = anchor
	am.anchorsByDAG[parallelDAG] = append(am.anchorsByDAG[parallelDAG], anchor)

	// Update references
	for _, ref := range references {
		if refAnchor, ok := am.anchors[ref]; ok {
			refAnchor.ReferencedBy = append(refAnchor.ReferencedBy, hash)
		}
	}

	// Update validator metrics
	validator.AnchorsProposed++
	validator.LastAnchorWave = wave

	// Prune old anchors
	am.pruneAnchors()

	return anchor, nil
}

// ReceiveAnchor records that an anchor has been received from the network.
func (am *AnchorManager) ReceiveAnchor(hash dag.Hash) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	anchor, ok := am.anchors[hash]
	if !ok {
		return ErrAnchorNotFound
	}

	if anchor.State == AnchorPending {
		anchor.State = AnchorReceived
	}

	return nil
}

// RecordVote records a vote for an anchor.
func (am *AnchorManager) RecordVote(anchorHash dag.Hash, voterIndex uint32, stake uint64) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	anchor, ok := am.anchors[anchorHash]
	if !ok {
		return ErrAnchorNotFound
	}

	// Check if already voted
	for _, idx := range anchor.VoterIndices {
		if idx == voterIndex {
			return ErrAlreadyVoted
		}
	}

	anchor.AddVote(voterIndex, stake)

	// Check if certified (quorum reached)
	if float64(anchor.VotesReceived)/float64(am.totalStake) >= 2.0/3.0 {
		anchor.State = AnchorCertified
	}

	return nil
}

// CommitAnchor marks an anchor as committed.
func (am *AnchorManager) CommitAnchor(hash dag.Hash, orderPosition int) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	anchor, ok := am.anchors[hash]
	if !ok {
		return ErrAnchorNotFound
	}

	anchor.State = AnchorCommitted
	anchor.OrderPosition = orderPosition

	// Update validator metrics
	if v, ok := am.validators[anchor.ValidatorIndex]; ok {
		v.AnchorsCommitted++
	}

	return nil
}

// OrphanAnchor marks an anchor as orphaned.
func (am *AnchorManager) OrphanAnchor(hash dag.Hash) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	anchor, ok := am.anchors[hash]
	if !ok {
		return ErrAnchorNotFound
	}

	anchor.State = AnchorOrphaned
	return nil
}

// GetAnchor returns an anchor by hash.
func (am *AnchorManager) GetAnchor(hash dag.Hash) *Anchor {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.anchors[hash]
}

// GetAnchorsForWave returns all anchors for a wave.
func (am *AnchorManager) GetAnchorsForWave(wave uint64) []*Anchor {
	am.mu.RLock()
	defer am.mu.RUnlock()

	anchors := am.anchorsByWave[wave]
	result := make([]*Anchor, len(anchors))
	copy(result, anchors)
	return result
}

// GetAnchorsForDAG returns all anchors for a parallel DAG.
func (am *AnchorManager) GetAnchorsForDAG(dagID uint8) []*Anchor {
	am.mu.RLock()
	defer am.mu.RUnlock()

	anchors := am.anchorsByDAG[dagID]
	result := make([]*Anchor, len(anchors))
	copy(result, anchors)
	return result
}

// GetValidatorAnchor returns a validator's anchor for a specific wave.
func (am *AnchorManager) GetValidatorAnchor(validatorIndex uint32, wave uint64) *Anchor {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if anchors, ok := am.anchorsByValidator[validatorIndex]; ok {
		return anchors[wave]
	}
	return nil
}

// GetCertifiedAnchors returns all certified anchors for a wave.
func (am *AnchorManager) GetCertifiedAnchors(wave uint64) []*Anchor {
	am.mu.RLock()
	defer am.mu.RUnlock()

	var certified []*Anchor
	for _, anchor := range am.anchorsByWave[wave] {
		if anchor.State >= AnchorCertified {
			certified = append(certified, anchor)
		}
	}
	return certified
}

// GetMissingValidators returns validators who haven't anchored in the current wave.
func (am *AnchorManager) GetMissingValidators(wave uint64) []uint32 {
	am.mu.RLock()
	defer am.mu.RUnlock()

	var missing []uint32
	for idx, v := range am.validators {
		if !v.IsActive {
			continue
		}
		if _, ok := am.anchorsByValidator[idx][wave]; !ok {
			missing = append(missing, idx)
		}
	}
	return missing
}

// ComputeAnchorOrder computes the deterministic order of anchors in a wave.
// Order is: certified first, then by stake (desc), then by hash.
func (am *AnchorManager) ComputeAnchorOrder(wave uint64) []dag.Hash {
	am.mu.RLock()
	defer am.mu.RUnlock()

	anchors := am.anchorsByWave[wave]
	if len(anchors) == 0 {
		return nil
	}

	// Sort anchors
	sorted := make([]*Anchor, len(anchors))
	copy(sorted, anchors)

	sort.Slice(sorted, func(i, j int) bool {
		// Certified anchors first
		iCertified := sorted[i].State >= AnchorCertified
		jCertified := sorted[j].State >= AnchorCertified
		if iCertified != jCertified {
			return iCertified
		}

		// Higher stake first
		if sorted[i].Stake != sorted[j].Stake {
			return sorted[i].Stake > sorted[j].Stake
		}

		// Hash tiebreaker
		for k := 0; k < 32; k++ {
			if sorted[i].Hash[k] != sorted[j].Hash[k] {
				return sorted[i].Hash[k] < sorted[j].Hash[k]
			}
		}
		return false
	})

	order := make([]dag.Hash, len(sorted))
	for i, a := range sorted {
		order[i] = a.Hash
	}

	return order
}

// GetTotalStake returns the total stake in the anchor manager.
func (am *AnchorManager) GetTotalStake() uint64 {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.totalStake
}

// GetActiveValidatorCount returns the number of active validators.
func (am *AnchorManager) GetActiveValidatorCount() int {
	am.mu.RLock()
	defer am.mu.RUnlock()

	count := 0
	for _, v := range am.validators {
		if v.IsActive {
			count++
		}
	}
	return count
}

// GetValidatorInfo returns validator info by index.
func (am *AnchorManager) GetValidatorInfo(index uint32) *ValidatorInfo {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.validators[index]
}

// pruneAnchors removes old anchors beyond the history limit.
func (am *AnchorManager) pruneAnchors() {
	if len(am.anchors) <= am.config.MaxAnchorHistory {
		return
	}

	// Find oldest wave with anchors
	var oldestWave uint64 = ^uint64(0)
	for wave := range am.anchorsByWave {
		if wave < oldestWave {
			oldestWave = wave
		}
	}

	// Remove anchors from oldest wave
	for _, anchor := range am.anchorsByWave[oldestWave] {
		delete(am.anchors, anchor.Hash)
		delete(am.anchorsByValidator[anchor.ValidatorIndex], oldestWave)
	}
	delete(am.anchorsByWave, oldestWave)
}

// AssignToParallelDAG determines which parallel DAG a validator should use.
// Uses round-robin based on validator index for deterministic assignment.
func (am *AnchorManager) AssignToParallelDAG(validatorIndex uint32, wave uint64) uint8 {
	// Round-robin across parallel DAGs, shifted by wave for variation
	return uint8((uint64(validatorIndex) + wave) % uint64(am.config.NumParallelDAGs))
}

// GetParallelDAGOffset returns the time offset for a parallel DAG.
func (am *AnchorManager) GetParallelDAGOffset(dagID uint8) time.Duration {
	return time.Duration(dagID) * am.config.StaggerOffset
}

// AnchorStatistics holds statistics about anchors.
type AnchorStatistics struct {
	TotalAnchors     int
	CertifiedAnchors int
	CommittedAnchors int
	OrphanedAnchors  int
	ActiveValidators int
	TotalStake       uint64
	AvgVotesReceived float64
}

// GetStatistics returns anchor statistics.
func (am *AnchorManager) GetStatistics() *AnchorStatistics {
	am.mu.RLock()
	defer am.mu.RUnlock()

	stats := &AnchorStatistics{
		TotalAnchors: len(am.anchors),
		TotalStake:   am.totalStake,
	}

	var totalVotes uint64
	for _, anchor := range am.anchors {
		switch anchor.State {
		case AnchorCertified:
			stats.CertifiedAnchors++
		case AnchorCommitted:
			stats.CommittedAnchors++
		case AnchorOrphaned:
			stats.OrphanedAnchors++
		}
		totalVotes += anchor.VotesReceived
	}

	for _, v := range am.validators {
		if v.IsActive {
			stats.ActiveValidators++
		}
	}

	if stats.TotalAnchors > 0 {
		stats.AvgVotesReceived = float64(totalVotes) / float64(stats.TotalAnchors)
	}

	return stats
}

// Error types for anchor operations.
var (
	ErrUnknownValidator  = anchorError("unknown validator")
	ErrValidatorInactive = anchorError("validator is inactive")
	ErrAlreadyAnchored   = anchorError("already anchored in this wave")
	ErrAnchorNotFound    = anchorError("anchor not found")
	ErrAlreadyVoted      = anchorError("already voted for this anchor")
)

type anchorError string

func (e anchorError) Error() string {
	return string(e)
}
