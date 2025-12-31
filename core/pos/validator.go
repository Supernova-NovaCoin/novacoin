// Package pos implements the Proof of Stake foundation for NovaCoin.
package pos

import (
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// ValidatorStatus represents the status of a validator.
type ValidatorStatus int

const (
	ValidatorPending ValidatorStatus = iota
	ValidatorActive
	ValidatorUnbonding
	ValidatorSlashed
	ValidatorExited
)

func (s ValidatorStatus) String() string {
	switch s {
	case ValidatorPending:
		return "pending"
	case ValidatorActive:
		return "active"
	case ValidatorUnbonding:
		return "unbonding"
	case ValidatorSlashed:
		return "slashed"
	case ValidatorExited:
		return "exited"
	default:
		return "unknown"
	}
}

// Validator represents a validator in the PoS system.
type Validator struct {
	Index           uint32
	PubKey          dag.PublicKey
	Address         dag.Address
	Stake           uint64
	EffectiveStake  uint64 // Stake after slashing
	Status          ValidatorStatus
	Commission      uint16 // Basis points (100 = 1%)
	ActivationEpoch uint64
	ExitEpoch       uint64
	SlashedEpoch    uint64
	UnbondingEnd    time.Time
	WithdrawAddress dag.Address
	Delegations     uint64 // Total delegated stake
	SelfStake       uint64 // Validator's own stake
	BlocksProposed  uint64
	BlocksMissed    uint64
	LastProposal    time.Time
}

// NewValidator creates a new validator.
func NewValidator(index uint32, pubKey dag.PublicKey, address dag.Address, stake uint64) *Validator {
	return &Validator{
		Index:          index,
		PubKey:         pubKey,
		Address:        address,
		Stake:          stake,
		EffectiveStake: stake,
		SelfStake:      stake,
		Status:         ValidatorPending,
		Commission:     500, // 5% default
	}
}

// IsActive returns true if validator can participate in consensus.
func (v *Validator) IsActive() bool {
	return v.Status == ValidatorActive
}

// CanPropose returns true if validator can propose blocks.
func (v *Validator) CanPropose() bool {
	return v.Status == ValidatorActive && v.EffectiveStake > 0
}

// TotalStake returns total stake including delegations.
func (v *Validator) TotalStake() uint64 {
	return v.SelfStake + v.Delegations
}

// ValidatorRegistry manages all validators.
type ValidatorRegistry struct {
	config *RegistryConfig

	validators    map[uint32]*Validator
	byPubKey      map[dag.PublicKey]uint32
	byAddress     map[dag.Address]uint32
	nextIndex     uint32
	totalStake    uint64
	activeStake   uint64
	activeCount   int

	mu sync.RWMutex
}

// RegistryConfig holds validator registry configuration.
type RegistryConfig struct {
	MinSelfStake       uint64
	MaxValidators      int
	UnbondingPeriod    time.Duration
	ActivationDelay    uint64 // Epochs
	MaxCommission      uint16 // Basis points
	MinCommission      uint16
}

// DefaultRegistryConfig returns default configuration.
func DefaultRegistryConfig() *RegistryConfig {
	return &RegistryConfig{
		MinSelfStake:    32_000_000_000, // 32 tokens (assuming 9 decimals)
		MaxValidators:   1000,
		UnbondingPeriod: 21 * 24 * time.Hour, // 21 days
		ActivationDelay: 2,
		MaxCommission:   5000, // 50%
		MinCommission:   0,
	}
}

// NewValidatorRegistry creates a new registry.
func NewValidatorRegistry(config *RegistryConfig) *ValidatorRegistry {
	if config == nil {
		config = DefaultRegistryConfig()
	}
	return &ValidatorRegistry{
		config:     config,
		validators: make(map[uint32]*Validator),
		byPubKey:   make(map[dag.PublicKey]uint32),
		byAddress:  make(map[dag.Address]uint32),
	}
}

// Register registers a new validator.
func (r *ValidatorRegistry) Register(pubKey dag.PublicKey, address dag.Address, stake uint64) (*Validator, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byPubKey[pubKey]; exists {
		return nil, ErrValidatorExists
	}
	if stake < r.config.MinSelfStake {
		return nil, ErrInsufficientStake
	}
	if len(r.validators) >= r.config.MaxValidators {
		return nil, ErrMaxValidatorsReached
	}

	v := NewValidator(r.nextIndex, pubKey, address, stake)
	r.validators[r.nextIndex] = v
	r.byPubKey[pubKey] = r.nextIndex
	r.byAddress[address] = r.nextIndex
	r.nextIndex++
	r.totalStake += stake

	return v, nil
}

// Activate activates a pending validator.
func (r *ValidatorRegistry) Activate(index uint32, epoch uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	v := r.validators[index]
	if v == nil {
		return ErrValidatorNotFound
	}
	if v.Status != ValidatorPending {
		return ErrInvalidStatus
	}

	v.Status = ValidatorActive
	v.ActivationEpoch = epoch
	r.activeStake += v.EffectiveStake
	r.activeCount++
	return nil
}

// InitiateExit begins the exit process for a validator.
func (r *ValidatorRegistry) InitiateExit(index uint32, epoch uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	v := r.validators[index]
	if v == nil {
		return ErrValidatorNotFound
	}
	if v.Status != ValidatorActive {
		return ErrInvalidStatus
	}

	v.Status = ValidatorUnbonding
	v.ExitEpoch = epoch
	v.UnbondingEnd = time.Now().Add(r.config.UnbondingPeriod)
	r.activeStake -= v.EffectiveStake
	r.activeCount--
	return nil
}

// CompleteExit completes the exit for an unbonding validator.
func (r *ValidatorRegistry) CompleteExit(index uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	v := r.validators[index]
	if v == nil {
		return ErrValidatorNotFound
	}
	if v.Status != ValidatorUnbonding {
		return ErrInvalidStatus
	}
	if time.Now().Before(v.UnbondingEnd) {
		return ErrUnbondingNotComplete
	}

	v.Status = ValidatorExited
	r.totalStake -= v.Stake
	return nil
}

// Get returns a validator by index.
func (r *ValidatorRegistry) Get(index uint32) *Validator {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.validators[index]
}

// GetByPubKey returns a validator by public key.
func (r *ValidatorRegistry) GetByPubKey(pubKey dag.PublicKey) *Validator {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if idx, ok := r.byPubKey[pubKey]; ok {
		return r.validators[idx]
	}
	return nil
}

// GetActive returns all active validators.
func (r *ValidatorRegistry) GetActive() []*Validator {
	r.mu.RLock()
	defer r.mu.RUnlock()

	active := make([]*Validator, 0, r.activeCount)
	for _, v := range r.validators {
		if v.IsActive() {
			active = append(active, v)
		}
	}
	return active
}

// GetTotalStake returns total staked amount.
func (r *ValidatorRegistry) GetTotalStake() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.totalStake
}

// GetActiveStake returns active validators' stake.
func (r *ValidatorRegistry) GetActiveStake() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeStake
}

// UpdateStake updates a validator's stake.
func (r *ValidatorRegistry) UpdateStake(index uint32, newStake uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	v := r.validators[index]
	if v == nil {
		return ErrValidatorNotFound
	}

	oldStake := v.Stake
	v.Stake = newStake
	v.EffectiveStake = newStake
	v.SelfStake = newStake - v.Delegations

	r.totalStake = r.totalStake - oldStake + newStake
	if v.IsActive() {
		r.activeStake = r.activeStake - oldStake + newStake
	}
	return nil
}

// RecordProposal records that a validator proposed a block.
func (r *ValidatorRegistry) RecordProposal(index uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v := r.validators[index]; v != nil {
		v.BlocksProposed++
		v.LastProposal = time.Now()
	}
}

// RecordMissedBlock records that a validator missed a block.
func (r *ValidatorRegistry) RecordMissedBlock(index uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v := r.validators[index]; v != nil {
		v.BlocksMissed++
	}
}

// Error types
var (
	ErrValidatorExists      = posError("validator already exists")
	ErrValidatorNotFound    = posError("validator not found")
	ErrInsufficientStake    = posError("insufficient stake")
	ErrMaxValidatorsReached = posError("maximum validators reached")
	ErrInvalidStatus        = posError("invalid validator status")
	ErrUnbondingNotComplete = posError("unbonding period not complete")
)

type posError string

func (e posError) Error() string { return string(e) }
