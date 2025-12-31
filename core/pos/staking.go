// Package pos implements the Proof of Stake foundation for NovaCoin.
package pos

import (
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// Delegation represents a stake delegation to a validator.
type Delegation struct {
	Delegator       dag.Address
	ValidatorIndex  uint32
	Amount          uint64
	StartEpoch      uint64
	UnbondingEpoch  uint64
	UnbondingEnd    time.Time
	IsUnbonding     bool
	RewardsClaimed  uint64
	PendingRewards  uint64
}

// StakingManager handles staking operations.
type StakingManager struct {
	config   *StakingConfig
	registry *ValidatorRegistry

	delegations    map[dag.Address][]*Delegation
	byValidator    map[uint32][]*Delegation
	pendingDeposits []pendingDeposit
	pendingWithdrawals []pendingWithdrawal

	totalDelegated uint64
	currentEpoch   uint64

	mu sync.RWMutex
}

type pendingDeposit struct {
	delegator      dag.Address
	validatorIndex uint32
	amount         uint64
	effectiveEpoch uint64
}

type pendingWithdrawal struct {
	delegator      dag.Address
	validatorIndex uint32
	amount         uint64
	availableTime  time.Time
}

// StakingConfig holds staking configuration.
type StakingConfig struct {
	MinDelegation      uint64
	UnbondingPeriod    time.Duration
	MaxDelegationsPerValidator int
	DepositDelay       uint64 // Epochs before deposit becomes active
	RewardClaimDelay   uint64 // Epochs before rewards can be claimed
}

// DefaultStakingConfig returns default configuration.
func DefaultStakingConfig() *StakingConfig {
	return &StakingConfig{
		MinDelegation:      1_000_000_000, // 1 token
		UnbondingPeriod:    21 * 24 * time.Hour,
		MaxDelegationsPerValidator: 10000,
		DepositDelay:       1,
		RewardClaimDelay:   1,
	}
}

// NewStakingManager creates a new staking manager.
func NewStakingManager(config *StakingConfig, registry *ValidatorRegistry) *StakingManager {
	if config == nil {
		config = DefaultStakingConfig()
	}
	return &StakingManager{
		config:      config,
		registry:    registry,
		delegations: make(map[dag.Address][]*Delegation),
		byValidator: make(map[uint32][]*Delegation),
	}
}

// Delegate stakes tokens to a validator.
func (sm *StakingManager) Delegate(delegator dag.Address, validatorIndex uint32, amount uint64) (*Delegation, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if amount < sm.config.MinDelegation {
		return nil, ErrDelegationTooSmall
	}

	v := sm.registry.Get(validatorIndex)
	if v == nil {
		return nil, ErrValidatorNotFound
	}
	if !v.IsActive() && v.Status != ValidatorPending {
		return nil, ErrValidatorNotActive
	}

	if len(sm.byValidator[validatorIndex]) >= sm.config.MaxDelegationsPerValidator {
		return nil, ErrTooManyDelegations
	}

	d := &Delegation{
		Delegator:      delegator,
		ValidatorIndex: validatorIndex,
		Amount:         amount,
		StartEpoch:     sm.currentEpoch + sm.config.DepositDelay,
	}

	sm.delegations[delegator] = append(sm.delegations[delegator], d)
	sm.byValidator[validatorIndex] = append(sm.byValidator[validatorIndex], d)
	sm.totalDelegated += amount

	// Update validator delegations
	sm.registry.mu.Lock()
	v.Delegations += amount
	v.EffectiveStake = v.SelfStake + v.Delegations
	sm.registry.mu.Unlock()

	return d, nil
}

// Undelegate initiates withdrawal of delegated tokens.
func (sm *StakingManager) Undelegate(delegator dag.Address, validatorIndex uint32, amount uint64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	dels := sm.delegations[delegator]
	var target *Delegation
	for _, d := range dels {
		if d.ValidatorIndex == validatorIndex && !d.IsUnbonding && d.Amount >= amount {
			target = d
			break
		}
	}

	if target == nil {
		return ErrDelegationNotFound
	}

	target.IsUnbonding = true
	target.UnbondingEpoch = sm.currentEpoch
	target.UnbondingEnd = time.Now().Add(sm.config.UnbondingPeriod)

	// Update validator
	v := sm.registry.Get(validatorIndex)
	if v != nil {
		sm.registry.mu.Lock()
		v.Delegations -= amount
		v.EffectiveStake = v.SelfStake + v.Delegations
		sm.registry.mu.Unlock()
	}

	return nil
}

// CompleteUnbonding completes unbonding for ready delegations.
func (sm *StakingManager) CompleteUnbonding(delegator dag.Address) (uint64, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var totalWithdrawn uint64
	dels := sm.delegations[delegator]
	remaining := make([]*Delegation, 0)

	for _, d := range dels {
		if d.IsUnbonding && time.Now().After(d.UnbondingEnd) {
			totalWithdrawn += d.Amount
			sm.totalDelegated -= d.Amount
			// Remove from validator list
			sm.removeDelegationFromValidator(d)
		} else {
			remaining = append(remaining, d)
		}
	}

	sm.delegations[delegator] = remaining
	return totalWithdrawn, nil
}

func (sm *StakingManager) removeDelegationFromValidator(d *Delegation) {
	dels := sm.byValidator[d.ValidatorIndex]
	for i, del := range dels {
		if del == d {
			sm.byValidator[d.ValidatorIndex] = append(dels[:i], dels[i+1:]...)
			break
		}
	}
}

// GetDelegations returns all delegations for an address.
func (sm *StakingManager) GetDelegations(delegator dag.Address) []*Delegation {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	dels := sm.delegations[delegator]
	result := make([]*Delegation, len(dels))
	copy(result, dels)
	return result
}

// GetValidatorDelegations returns all delegations to a validator.
func (sm *StakingManager) GetValidatorDelegations(validatorIndex uint32) []*Delegation {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	dels := sm.byValidator[validatorIndex]
	result := make([]*Delegation, len(dels))
	copy(result, dels)
	return result
}

// GetTotalDelegated returns total delegated stake.
func (sm *StakingManager) GetTotalDelegated() uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.totalDelegated
}

// SetEpoch updates the current epoch.
func (sm *StakingManager) SetEpoch(epoch uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.currentEpoch = epoch
}

// AddPendingRewards adds pending rewards to a delegation.
func (sm *StakingManager) AddPendingRewards(delegator dag.Address, validatorIndex uint32, amount uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, d := range sm.delegations[delegator] {
		if d.ValidatorIndex == validatorIndex && !d.IsUnbonding {
			d.PendingRewards += amount
			break
		}
	}
}

// ClaimRewards claims pending rewards for a delegation.
func (sm *StakingManager) ClaimRewards(delegator dag.Address, validatorIndex uint32) (uint64, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, d := range sm.delegations[delegator] {
		if d.ValidatorIndex == validatorIndex {
			rewards := d.PendingRewards
			d.RewardsClaimed += rewards
			d.PendingRewards = 0
			return rewards, nil
		}
	}
	return 0, ErrDelegationNotFound
}

// Error types
var (
	ErrDelegationTooSmall  = posError("delegation amount too small")
	ErrValidatorNotActive  = posError("validator not active")
	ErrTooManyDelegations  = posError("too many delegations to validator")
	ErrDelegationNotFound  = posError("delegation not found")
)
