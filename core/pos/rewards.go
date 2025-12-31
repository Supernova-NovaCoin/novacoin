// Package pos implements the Proof of Stake foundation for NovaCoin.
package pos

import (
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// RewardType represents the type of reward.
type RewardType int

const (
	RewardBlock RewardType = iota // Block production reward
	RewardAttestation             // Attestation/voting reward
	RewardProposal                // Proposal reward
	RewardFee                     // Transaction fee share
)

func (r RewardType) String() string {
	switch r {
	case RewardBlock:
		return "block"
	case RewardAttestation:
		return "attestation"
	case RewardProposal:
		return "proposal"
	case RewardFee:
		return "fee"
	default:
		return "unknown"
	}
}

// RewardEvent records a reward distribution event.
type RewardEvent struct {
	ValidatorIndex uint32
	Type           RewardType
	Amount         uint64
	Epoch          uint64
	BlockHash      dag.Hash
	Timestamp      time.Time
}

// EpochRewards tracks rewards for an epoch.
type EpochRewards struct {
	Epoch            uint64
	TotalRewards     uint64
	BlockRewards     uint64
	AttestRewards    uint64
	FeeRewards       uint64
	ValidatorRewards map[uint32]uint64
	DelegatorRewards map[dag.Address]uint64
	Finalized        bool
}

// RewardsConfig holds rewards configuration.
type RewardsConfig struct {
	BaseBlockReward      uint64  // Base reward per block
	AttestationReward    uint64  // Reward for attestation
	ProposerBonus        uint16  // Basis points bonus for proposer (100 = 1%)
	FeeRecipientShare    uint16  // Basis points of fees to block producer
	ProtocolFeeShare     uint16  // Basis points to protocol treasury
	InitialInflationRate uint16  // Annual inflation in basis points (300 = 3%)
	MinInflationRate     uint16  // Minimum inflation rate
	InflationDecayRate   uint16  // Annual decay in basis points
	EpochsPerYear        uint64  // Number of epochs per year
	BlocksPerEpoch       uint64  // Blocks per epoch for calculations
}

// DefaultRewardsConfig returns default configuration.
func DefaultRewardsConfig() *RewardsConfig {
	return &RewardsConfig{
		BaseBlockReward:      2_000_000_000, // 2 tokens per block
		AttestationReward:    100_000_000,   // 0.1 token per attestation
		ProposerBonus:        100,           // 1% proposer bonus
		FeeRecipientShare:    8000,          // 80% of fees to block producer
		ProtocolFeeShare:     1000,          // 10% to protocol
		InitialInflationRate: 300,           // 3% annual inflation
		MinInflationRate:     50,            // 0.5% minimum
		InflationDecayRate:   50,            // 0.5% decay per year
		EpochsPerYear:        365 * 24 * 6,  // ~52560 epochs (10 min epochs)
		BlocksPerEpoch:       32,
	}
}

// RewardsDistributor handles reward distribution.
type RewardsDistributor struct {
	config   *RewardsConfig
	registry *ValidatorRegistry
	staking  *StakingManager

	currentEpoch     uint64
	totalDistributed uint64
	totalSupply      uint64 // Track for inflation calculation

	epochRewards   map[uint64]*EpochRewards
	pendingRewards map[uint32]uint64      // validator -> pending
	events         []*RewardEvent

	protocolTreasury uint64 // Accumulated protocol fees

	mu sync.RWMutex
}

// NewRewardsDistributor creates a new rewards distributor.
func NewRewardsDistributor(config *RewardsConfig, registry *ValidatorRegistry, staking *StakingManager) *RewardsDistributor {
	if config == nil {
		config = DefaultRewardsConfig()
	}
	return &RewardsDistributor{
		config:         config,
		registry:       registry,
		staking:        staking,
		epochRewards:   make(map[uint64]*EpochRewards),
		pendingRewards: make(map[uint32]uint64),
		events:         make([]*RewardEvent, 0),
		totalSupply:    1_000_000_000_000_000_000, // 1 billion initial supply
	}
}

// DistributeBlockReward distributes rewards for a block.
func (rd *RewardsDistributor) DistributeBlockReward(proposerIndex uint32, blockHash dag.Hash, fees uint64) error {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	v := rd.registry.Get(proposerIndex)
	if v == nil {
		return ErrValidatorNotFound
	}

	// Calculate block reward with inflation adjustment
	blockReward := rd.calculateBlockReward()

	// Proposer bonus
	proposerBonus := (blockReward * uint64(rd.config.ProposerBonus)) / 10000

	// Fee distribution
	proposerFees := (fees * uint64(rd.config.FeeRecipientShare)) / 10000
	protocolFees := (fees * uint64(rd.config.ProtocolFeeShare)) / 10000
	remainingFees := fees - proposerFees - protocolFees

	// Total proposer reward
	totalProposerReward := blockReward + proposerBonus + proposerFees

	// Distribute to proposer (respecting commission for delegated stake)
	rd.distributeToValidator(proposerIndex, totalProposerReward, RewardBlock, blockHash)

	// Protocol treasury
	rd.protocolTreasury += protocolFees

	// Remaining fees distributed to attesters (handled separately)
	if remainingFees > 0 {
		rd.distributeAttestationPool(remainingFees, blockHash)
	}

	return nil
}

// DistributeAttestationReward rewards a validator for attestation.
func (rd *RewardsDistributor) DistributeAttestationReward(validatorIndex uint32, blockHash dag.Hash) error {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	v := rd.registry.Get(validatorIndex)
	if v == nil {
		return ErrValidatorNotFound
	}
	if !v.IsActive() {
		return ErrValidatorNotActive
	}

	// Stake-weighted attestation reward
	activeStake := rd.registry.GetActiveStake()
	if activeStake == 0 {
		return nil
	}

	// Reward proportional to stake
	reward := (rd.config.AttestationReward * v.EffectiveStake) / activeStake
	rd.distributeToValidator(validatorIndex, reward, RewardAttestation, blockHash)

	return nil
}

func (rd *RewardsDistributor) distributeToValidator(validatorIndex uint32, amount uint64, rewardType RewardType, blockHash dag.Hash) {
	v := rd.registry.Get(validatorIndex)
	if v == nil {
		return
	}

	// Calculate commission for delegator rewards
	commission := v.Commission
	totalStake := v.EffectiveStake
	if totalStake == 0 {
		return
	}

	// Self-stake portion goes entirely to validator
	selfStakeShare := (amount * v.SelfStake) / totalStake

	// Delegated stake portion: commission to validator, rest to delegators
	delegatedShare := amount - selfStakeShare
	commissionAmount := (delegatedShare * uint64(commission)) / 10000
	delegatorShare := delegatedShare - commissionAmount

	// Validator gets self-stake reward + commission
	validatorReward := selfStakeShare + commissionAmount
	rd.pendingRewards[validatorIndex] += validatorReward

	// Distribute to delegators proportionally
	if rd.staking != nil && delegatorShare > 0 {
		rd.distributeToDelegators(validatorIndex, delegatorShare)
	}

	// Record event
	event := &RewardEvent{
		ValidatorIndex: validatorIndex,
		Type:           rewardType,
		Amount:         amount,
		Epoch:          rd.currentEpoch,
		BlockHash:      blockHash,
		Timestamp:      time.Now(),
	}
	rd.events = append(rd.events, event)
	rd.totalDistributed += amount

	// Update epoch tracking
	rd.ensureEpochRewards(rd.currentEpoch)
	rd.epochRewards[rd.currentEpoch].TotalRewards += amount
	rd.epochRewards[rd.currentEpoch].ValidatorRewards[validatorIndex] += validatorReward
}

func (rd *RewardsDistributor) distributeToDelegators(validatorIndex uint32, amount uint64) {
	delegations := rd.staking.GetValidatorDelegations(validatorIndex)
	if len(delegations) == 0 {
		return
	}

	var totalDelegated uint64
	for _, d := range delegations {
		if !d.IsUnbonding {
			totalDelegated += d.Amount
		}
	}

	if totalDelegated == 0 {
		return
	}

	for _, d := range delegations {
		if d.IsUnbonding {
			continue
		}
		delegatorReward := (amount * d.Amount) / totalDelegated
		rd.staking.AddPendingRewards(d.Delegator, validatorIndex, delegatorReward)

		rd.ensureEpochRewards(rd.currentEpoch)
		rd.epochRewards[rd.currentEpoch].DelegatorRewards[d.Delegator] += delegatorReward
	}
}

func (rd *RewardsDistributor) distributeAttestationPool(amount uint64, blockHash dag.Hash) {
	activeValidators := rd.registry.GetActive()
	if len(activeValidators) == 0 {
		return
	}

	var totalStake uint64
	for _, v := range activeValidators {
		totalStake += v.EffectiveStake
	}

	if totalStake == 0 {
		return
	}

	for _, v := range activeValidators {
		share := (amount * v.EffectiveStake) / totalStake
		if share > 0 {
			rd.distributeToValidator(v.Index, share, RewardFee, blockHash)
		}
	}
}

func (rd *RewardsDistributor) calculateBlockReward() uint64 {
	// Apply inflation decay based on time
	yearsElapsed := rd.currentEpoch / rd.config.EpochsPerYear
	currentRate := rd.config.InitialInflationRate

	for i := uint64(0); i < yearsElapsed; i++ {
		decay := (uint64(currentRate) * uint64(rd.config.InflationDecayRate)) / 10000
		if currentRate <= rd.config.MinInflationRate+uint16(decay) {
			currentRate = rd.config.MinInflationRate
			break
		}
		currentRate -= uint16(decay)
	}

	// Calculate per-block reward from annual inflation
	annualReward := (rd.totalSupply * uint64(currentRate)) / 10000
	blocksPerYear := rd.config.EpochsPerYear * rd.config.BlocksPerEpoch
	blockReward := annualReward / blocksPerYear

	// Minimum reward floor
	if blockReward < rd.config.BaseBlockReward/10 {
		blockReward = rd.config.BaseBlockReward / 10
	}

	return blockReward
}

func (rd *RewardsDistributor) ensureEpochRewards(epoch uint64) {
	if rd.epochRewards[epoch] == nil {
		rd.epochRewards[epoch] = &EpochRewards{
			Epoch:            epoch,
			ValidatorRewards: make(map[uint32]uint64),
			DelegatorRewards: make(map[dag.Address]uint64),
		}
	}
}

// ClaimRewards claims pending rewards for a validator.
func (rd *RewardsDistributor) ClaimRewards(validatorIndex uint32) (uint64, error) {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	v := rd.registry.Get(validatorIndex)
	if v == nil {
		return 0, ErrValidatorNotFound
	}

	rewards := rd.pendingRewards[validatorIndex]
	rd.pendingRewards[validatorIndex] = 0
	return rewards, nil
}

// GetPendingRewards returns pending rewards for a validator.
func (rd *RewardsDistributor) GetPendingRewards(validatorIndex uint32) uint64 {
	rd.mu.RLock()
	defer rd.mu.RUnlock()
	return rd.pendingRewards[validatorIndex]
}

// GetEpochRewards returns rewards for an epoch.
func (rd *RewardsDistributor) GetEpochRewards(epoch uint64) *EpochRewards {
	rd.mu.RLock()
	defer rd.mu.RUnlock()
	return rd.epochRewards[epoch]
}

// GetTotalDistributed returns total distributed rewards.
func (rd *RewardsDistributor) GetTotalDistributed() uint64 {
	rd.mu.RLock()
	defer rd.mu.RUnlock()
	return rd.totalDistributed
}

// GetProtocolTreasury returns accumulated protocol fees.
func (rd *RewardsDistributor) GetProtocolTreasury() uint64 {
	rd.mu.RLock()
	defer rd.mu.RUnlock()
	return rd.protocolTreasury
}

// SetEpoch updates the current epoch.
func (rd *RewardsDistributor) SetEpoch(epoch uint64) {
	rd.mu.Lock()
	defer rd.mu.Unlock()
	rd.currentEpoch = epoch
}

// SetTotalSupply sets the total supply for inflation calculations.
func (rd *RewardsDistributor) SetTotalSupply(supply uint64) {
	rd.mu.Lock()
	defer rd.mu.Unlock()
	rd.totalSupply = supply
}

// FinalizeEpoch finalizes rewards for an epoch.
func (rd *RewardsDistributor) FinalizeEpoch(epoch uint64) error {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	er := rd.epochRewards[epoch]
	if er == nil {
		return ErrEpochNotFound
	}
	if er.Finalized {
		return ErrEpochAlreadyFinalized
	}

	er.Finalized = true
	return nil
}

// GetRewardEvents returns all reward events.
func (rd *RewardsDistributor) GetRewardEvents() []*RewardEvent {
	rd.mu.RLock()
	defer rd.mu.RUnlock()
	events := make([]*RewardEvent, len(rd.events))
	copy(events, rd.events)
	return events
}

// GetValidatorRewardEvents returns reward events for a validator.
func (rd *RewardsDistributor) GetValidatorRewardEvents(validatorIndex uint32) []*RewardEvent {
	rd.mu.RLock()
	defer rd.mu.RUnlock()

	var events []*RewardEvent
	for _, e := range rd.events {
		if e.ValidatorIndex == validatorIndex {
			events = append(events, e)
		}
	}
	return events
}

// Error types
var (
	ErrEpochNotFound        = posError("epoch not found")
	ErrEpochAlreadyFinalized = posError("epoch already finalized")
)
