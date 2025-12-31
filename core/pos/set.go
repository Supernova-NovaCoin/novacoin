// Package pos implements the Proof of Stake foundation for NovaCoin.
package pos

import (
	"sort"
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// ValidatorSetEntry represents a validator in the active set.
type ValidatorSetEntry struct {
	Index          uint32
	PubKey         dag.PublicKey
	EffectiveStake uint64
	VotingPower    uint64 // Normalized voting power (0-10000)
}

// ValidatorSet represents the active validator set for an epoch.
type ValidatorSet struct {
	Epoch        uint64
	Validators   []*ValidatorSetEntry
	TotalStake   uint64
	ValidatorMap map[uint32]*ValidatorSetEntry
	Finalized    bool
	CreatedAt    time.Time
}

// EpochTransition records an epoch transition.
type EpochTransition struct {
	FromEpoch   uint64
	ToEpoch     uint64
	Activated   []uint32 // Validators activated
	Exited      []uint32 // Validators exited
	Slashed     []uint32 // Validators slashed
	StakeChange int64    // Net stake change
	Timestamp   time.Time
}

// ValidatorSetConfig holds validator set configuration.
type ValidatorSetConfig struct {
	MaxActiveValidators int    // Maximum validators in active set
	MinActiveValidators int    // Minimum validators required
	EpochDuration       uint64 // Blocks per epoch
	ActivationQueue     int    // Max activations per epoch
	ExitQueue           int    // Max exits per epoch
	ChurnLimit          uint16 // Basis points of set that can change per epoch
	QuorumThreshold     uint16 // Basis points required for quorum (6667 = 2/3)
}

// DefaultValidatorSetConfig returns default configuration.
func DefaultValidatorSetConfig() *ValidatorSetConfig {
	return &ValidatorSetConfig{
		MaxActiveValidators: 1000,
		MinActiveValidators: 4,
		EpochDuration:       32,
		ActivationQueue:     8,
		ExitQueue:           8,
		ChurnLimit:          400, // 4% per epoch max churn
		QuorumThreshold:     6667, // 2/3 + 1 basis point
	}
}

// ValidatorSetManager manages the active validator set.
type ValidatorSetManager struct {
	config   *ValidatorSetConfig
	registry *ValidatorRegistry

	currentSet    *ValidatorSet
	historicalSets map[uint64]*ValidatorSet
	transitions   []*EpochTransition

	pendingActivations []uint32
	pendingExits       []uint32

	currentEpoch uint64

	mu sync.RWMutex
}

// NewValidatorSetManager creates a new validator set manager.
func NewValidatorSetManager(config *ValidatorSetConfig, registry *ValidatorRegistry) *ValidatorSetManager {
	if config == nil {
		config = DefaultValidatorSetConfig()
	}
	return &ValidatorSetManager{
		config:         config,
		registry:       registry,
		historicalSets: make(map[uint64]*ValidatorSet),
	}
}

// InitializeSet creates the initial validator set.
func (vsm *ValidatorSetManager) InitializeSet(epoch uint64) error {
	vsm.mu.Lock()
	defer vsm.mu.Unlock()

	activeValidators := vsm.registry.GetActive()
	if len(activeValidators) < vsm.config.MinActiveValidators {
		return ErrInsufficientValidators
	}

	set := vsm.buildValidatorSet(epoch, activeValidators)
	vsm.currentSet = set
	vsm.historicalSets[epoch] = set
	vsm.currentEpoch = epoch

	return nil
}

// ProcessEpochTransition handles epoch transition.
func (vsm *ValidatorSetManager) ProcessEpochTransition(newEpoch uint64) (*EpochTransition, error) {
	vsm.mu.Lock()
	defer vsm.mu.Unlock()

	if newEpoch <= vsm.currentEpoch {
		return nil, ErrInvalidEpoch
	}

	oldSet := vsm.currentSet
	if oldSet == nil {
		return nil, ErrNoActiveSet
	}

	// Calculate churn limit
	maxChurn := (len(oldSet.Validators) * int(vsm.config.ChurnLimit)) / 10000
	if maxChurn < 1 {
		maxChurn = 1
	}

	// Process activations (up to limit)
	activated := make([]uint32, 0)
	activationLimit := min(len(vsm.pendingActivations), vsm.config.ActivationQueue, maxChurn)
	for i := 0; i < activationLimit; i++ {
		idx := vsm.pendingActivations[i]
		if err := vsm.registry.Activate(idx, newEpoch); err == nil {
			activated = append(activated, idx)
		}
	}
	vsm.pendingActivations = vsm.pendingActivations[activationLimit:]

	// Process exits (up to limit)
	exited := make([]uint32, 0)
	exitLimit := min(len(vsm.pendingExits), vsm.config.ExitQueue, maxChurn-len(activated))
	if exitLimit < 0 {
		exitLimit = 0
	}
	for i := 0; i < exitLimit; i++ {
		idx := vsm.pendingExits[i]
		if err := vsm.registry.InitiateExit(idx, newEpoch); err == nil {
			exited = append(exited, idx)
		}
	}
	vsm.pendingExits = vsm.pendingExits[exitLimit:]

	// Check for slashed validators
	slashed := make([]uint32, 0)
	for _, entry := range oldSet.Validators {
		v := vsm.registry.Get(entry.Index)
		if v != nil && v.Status == ValidatorSlashed {
			slashed = append(slashed, entry.Index)
		}
	}

	// Build new validator set
	activeValidators := vsm.registry.GetActive()
	if len(activeValidators) < vsm.config.MinActiveValidators {
		return nil, ErrInsufficientValidators
	}

	newSet := vsm.buildValidatorSet(newEpoch, activeValidators)

	// Calculate stake change
	stakeChange := int64(newSet.TotalStake) - int64(oldSet.TotalStake)

	// Record transition
	transition := &EpochTransition{
		FromEpoch:   vsm.currentEpoch,
		ToEpoch:     newEpoch,
		Activated:   activated,
		Exited:      exited,
		Slashed:     slashed,
		StakeChange: stakeChange,
		Timestamp:   time.Now(),
	}
	vsm.transitions = append(vsm.transitions, transition)

	// Finalize old set
	oldSet.Finalized = true

	// Update current
	vsm.currentSet = newSet
	vsm.historicalSets[newEpoch] = newSet
	vsm.currentEpoch = newEpoch

	return transition, nil
}

func (vsm *ValidatorSetManager) buildValidatorSet(epoch uint64, validators []*Validator) *ValidatorSet {
	// Sort by stake descending
	sort.Slice(validators, func(i, j int) bool {
		return validators[i].EffectiveStake > validators[j].EffectiveStake
	})

	// Limit to max active
	if len(validators) > vsm.config.MaxActiveValidators {
		validators = validators[:vsm.config.MaxActiveValidators]
	}

	var totalStake uint64
	entries := make([]*ValidatorSetEntry, len(validators))
	validatorMap := make(map[uint32]*ValidatorSetEntry)

	for i, v := range validators {
		entry := &ValidatorSetEntry{
			Index:          v.Index,
			PubKey:         v.PubKey,
			EffectiveStake: v.EffectiveStake,
		}
		entries[i] = entry
		validatorMap[v.Index] = entry
		totalStake += v.EffectiveStake
	}

	// Calculate voting power (normalized to 10000)
	if totalStake > 0 {
		for _, entry := range entries {
			entry.VotingPower = (entry.EffectiveStake * 10000) / totalStake
		}
	}

	return &ValidatorSet{
		Epoch:        epoch,
		Validators:   entries,
		TotalStake:   totalStake,
		ValidatorMap: validatorMap,
		CreatedAt:    time.Now(),
	}
}

// QueueActivation queues a validator for activation.
func (vsm *ValidatorSetManager) QueueActivation(validatorIndex uint32) error {
	vsm.mu.Lock()
	defer vsm.mu.Unlock()

	v := vsm.registry.Get(validatorIndex)
	if v == nil {
		return ErrValidatorNotFound
	}
	if v.Status != ValidatorPending {
		return ErrInvalidStatus
	}

	// Check if already queued
	for _, idx := range vsm.pendingActivations {
		if idx == validatorIndex {
			return ErrAlreadyQueued
		}
	}

	vsm.pendingActivations = append(vsm.pendingActivations, validatorIndex)
	return nil
}

// QueueExit queues a validator for exit.
func (vsm *ValidatorSetManager) QueueExit(validatorIndex uint32) error {
	vsm.mu.Lock()
	defer vsm.mu.Unlock()

	v := vsm.registry.Get(validatorIndex)
	if v == nil {
		return ErrValidatorNotFound
	}
	if v.Status != ValidatorActive {
		return ErrInvalidStatus
	}

	// Check if already queued
	for _, idx := range vsm.pendingExits {
		if idx == validatorIndex {
			return ErrAlreadyQueued
		}
	}

	vsm.pendingExits = append(vsm.pendingExits, validatorIndex)
	return nil
}

// GetCurrentSet returns the current validator set.
func (vsm *ValidatorSetManager) GetCurrentSet() *ValidatorSet {
	vsm.mu.RLock()
	defer vsm.mu.RUnlock()
	return vsm.currentSet
}

// GetSetForEpoch returns the validator set for an epoch.
func (vsm *ValidatorSetManager) GetSetForEpoch(epoch uint64) *ValidatorSet {
	vsm.mu.RLock()
	defer vsm.mu.RUnlock()
	return vsm.historicalSets[epoch]
}

// GetValidator returns a validator entry from the current set.
func (vsm *ValidatorSetManager) GetValidator(index uint32) *ValidatorSetEntry {
	vsm.mu.RLock()
	defer vsm.mu.RUnlock()
	if vsm.currentSet == nil {
		return nil
	}
	return vsm.currentSet.ValidatorMap[index]
}

// HasQuorum checks if given stake represents a quorum.
func (vsm *ValidatorSetManager) HasQuorum(stake uint64) bool {
	vsm.mu.RLock()
	defer vsm.mu.RUnlock()
	if vsm.currentSet == nil || vsm.currentSet.TotalStake == 0 {
		return false
	}
	threshold := (vsm.currentSet.TotalStake * uint64(vsm.config.QuorumThreshold)) / 10000
	return stake >= threshold
}

// GetQuorumThreshold returns the stake needed for quorum.
func (vsm *ValidatorSetManager) GetQuorumThreshold() uint64 {
	vsm.mu.RLock()
	defer vsm.mu.RUnlock()
	if vsm.currentSet == nil {
		return 0
	}
	return (vsm.currentSet.TotalStake * uint64(vsm.config.QuorumThreshold)) / 10000
}

// IsInActiveSet checks if validator is in the active set.
func (vsm *ValidatorSetManager) IsInActiveSet(validatorIndex uint32) bool {
	vsm.mu.RLock()
	defer vsm.mu.RUnlock()
	if vsm.currentSet == nil {
		return false
	}
	_, exists := vsm.currentSet.ValidatorMap[validatorIndex]
	return exists
}

// GetPendingActivations returns validators pending activation.
func (vsm *ValidatorSetManager) GetPendingActivations() []uint32 {
	vsm.mu.RLock()
	defer vsm.mu.RUnlock()
	result := make([]uint32, len(vsm.pendingActivations))
	copy(result, vsm.pendingActivations)
	return result
}

// GetPendingExits returns validators pending exit.
func (vsm *ValidatorSetManager) GetPendingExits() []uint32 {
	vsm.mu.RLock()
	defer vsm.mu.RUnlock()
	result := make([]uint32, len(vsm.pendingExits))
	copy(result, vsm.pendingExits)
	return result
}

// GetTransitions returns all epoch transitions.
func (vsm *ValidatorSetManager) GetTransitions() []*EpochTransition {
	vsm.mu.RLock()
	defer vsm.mu.RUnlock()
	result := make([]*EpochTransition, len(vsm.transitions))
	copy(result, vsm.transitions)
	return result
}

// GetCurrentEpoch returns the current epoch.
func (vsm *ValidatorSetManager) GetCurrentEpoch() uint64 {
	vsm.mu.RLock()
	defer vsm.mu.RUnlock()
	return vsm.currentEpoch
}

// SelectProposer selects the block proposer for a slot.
func (vsm *ValidatorSetManager) SelectProposer(slot uint64) uint32 {
	vsm.mu.RLock()
	defer vsm.mu.RUnlock()

	if vsm.currentSet == nil || len(vsm.currentSet.Validators) == 0 {
		return 0
	}

	numValidators := uint64(len(vsm.currentSet.Validators))
	if numValidators == 0 {
		return 0
	}

	// Stake-weighted selection using slot as seed
	totalStake := vsm.currentSet.TotalStake
	if totalStake == 0 {
		return vsm.currentSet.Validators[0].Index
	}

	// Scale slot to stake range for proper distribution
	// Use slot modulo validator count, then weight by stake within that "round"
	round := slot / numValidators
	position := slot % numValidators

	// Mix position with round for deterministic but distributed selection
	// This ensures different slots map to different stake ranges
	scaledPosition := (position*totalStake/numValidators + round) % totalStake

	var cumulative uint64
	for _, entry := range vsm.currentSet.Validators {
		cumulative += entry.EffectiveStake
		if scaledPosition < cumulative {
			return entry.Index
		}
	}

	return vsm.currentSet.Validators[0].Index
}

// Error types
var (
	ErrInsufficientValidators = posError("insufficient validators for set")
	ErrInvalidEpoch           = posError("invalid epoch number")
	ErrNoActiveSet            = posError("no active validator set")
	ErrAlreadyQueued          = posError("validator already queued")
)

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
