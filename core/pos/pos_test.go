package pos

import (
	"testing"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

func testPubKey(index uint32) dag.PublicKey {
	var pk dag.PublicKey
	pk[0] = byte(index)
	pk[1] = byte(index >> 8)
	return pk
}

func testAddress(index uint32) dag.Address {
	var addr dag.Address
	addr[0] = byte(index)
	addr[1] = byte(index >> 8)
	return addr
}

func testHash(index uint32) dag.Hash {
	var h dag.Hash
	h[0] = byte(index)
	h[1] = byte(index >> 8)
	return h
}

// === Validator Registry Tests ===

func TestValidatorRegistry_Register(t *testing.T) {
	registry := NewValidatorRegistry(nil)

	v, err := registry.Register(testPubKey(1), testAddress(1), 50_000_000_000)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if v.Index != 0 {
		t.Errorf("Expected index 0, got %d", v.Index)
	}
	if v.Status != ValidatorPending {
		t.Errorf("Expected pending status")
	}
	if v.EffectiveStake != 50_000_000_000 {
		t.Errorf("Expected effective stake 50B, got %d", v.EffectiveStake)
	}
}

func TestValidatorRegistry_InsufficientStake(t *testing.T) {
	registry := NewValidatorRegistry(nil)

	_, err := registry.Register(testPubKey(1), testAddress(1), 1_000_000) // Too small
	if err != ErrInsufficientStake {
		t.Errorf("Expected ErrInsufficientStake, got %v", err)
	}
}

func TestValidatorRegistry_DuplicateValidator(t *testing.T) {
	registry := NewValidatorRegistry(nil)

	registry.Register(testPubKey(1), testAddress(1), 50_000_000_000)
	_, err := registry.Register(testPubKey(1), testAddress(2), 50_000_000_000)
	if err != ErrValidatorExists {
		t.Errorf("Expected ErrValidatorExists, got %v", err)
	}
}

func TestValidatorRegistry_Activate(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	v, _ := registry.Register(testPubKey(1), testAddress(1), 50_000_000_000)

	err := registry.Activate(v.Index, 1)
	if err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	v = registry.Get(v.Index)
	if v.Status != ValidatorActive {
		t.Errorf("Expected active status")
	}
	if v.ActivationEpoch != 1 {
		t.Errorf("Expected activation epoch 1")
	}
}

func TestValidatorRegistry_Exit(t *testing.T) {
	config := &RegistryConfig{
		MinSelfStake:    32_000_000_000,
		MaxValidators:   1000,
		UnbondingPeriod: 100 * time.Millisecond, // Short for testing
		ActivationDelay: 2,
		MaxCommission:   5000,
	}
	registry := NewValidatorRegistry(config)
	v, _ := registry.Register(testPubKey(1), testAddress(1), 50_000_000_000)
	registry.Activate(v.Index, 1)

	err := registry.InitiateExit(v.Index, 10)
	if err != nil {
		t.Fatalf("InitiateExit failed: %v", err)
	}

	v = registry.Get(v.Index)
	if v.Status != ValidatorUnbonding {
		t.Errorf("Expected unbonding status")
	}

	// Complete exit after unbonding
	time.Sleep(150 * time.Millisecond)
	err = registry.CompleteExit(v.Index)
	if err != nil {
		t.Fatalf("CompleteExit failed: %v", err)
	}

	v = registry.Get(v.Index)
	if v.Status != ValidatorExited {
		t.Errorf("Expected exited status")
	}
}

func TestValidatorRegistry_GetActive(t *testing.T) {
	registry := NewValidatorRegistry(nil)

	for i := uint32(0); i < 5; i++ {
		v, _ := registry.Register(testPubKey(i), testAddress(i), 50_000_000_000)
		if i < 3 {
			registry.Activate(v.Index, 1)
		}
	}

	active := registry.GetActive()
	if len(active) != 3 {
		t.Errorf("Expected 3 active validators, got %d", len(active))
	}
}

// === Staking Manager Tests ===

func TestStakingManager_Delegate(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	v, _ := registry.Register(testPubKey(1), testAddress(1), 50_000_000_000)
	registry.Activate(v.Index, 1)

	staking := NewStakingManager(nil, registry)

	delegator := testAddress(100)
	d, err := staking.Delegate(delegator, v.Index, 5_000_000_000)
	if err != nil {
		t.Fatalf("Delegate failed: %v", err)
	}

	if d.Amount != 5_000_000_000 {
		t.Errorf("Expected delegation amount 5B")
	}

	v = registry.Get(v.Index)
	if v.Delegations != 5_000_000_000 {
		t.Errorf("Expected validator delegations 5B, got %d", v.Delegations)
	}
}

func TestStakingManager_DelegateTooSmall(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	v, _ := registry.Register(testPubKey(1), testAddress(1), 50_000_000_000)
	registry.Activate(v.Index, 1)

	staking := NewStakingManager(nil, registry)

	_, err := staking.Delegate(testAddress(100), v.Index, 100) // Too small
	if err != ErrDelegationTooSmall {
		t.Errorf("Expected ErrDelegationTooSmall, got %v", err)
	}
}

func TestStakingManager_Undelegate(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	v, _ := registry.Register(testPubKey(1), testAddress(1), 50_000_000_000)
	registry.Activate(v.Index, 1)

	config := &StakingConfig{
		MinDelegation:              1_000_000_000,
		UnbondingPeriod:            100 * time.Millisecond,
		MaxDelegationsPerValidator: 10000,
		DepositDelay:               1,
		RewardClaimDelay:           1,
	}
	staking := NewStakingManager(config, registry)

	delegator := testAddress(100)
	staking.Delegate(delegator, v.Index, 5_000_000_000)

	err := staking.Undelegate(delegator, v.Index, 5_000_000_000)
	if err != nil {
		t.Fatalf("Undelegate failed: %v", err)
	}

	// Check unbonding state
	dels := staking.GetDelegations(delegator)
	if len(dels) != 1 || !dels[0].IsUnbonding {
		t.Errorf("Expected delegation in unbonding state")
	}

	// Complete unbonding
	time.Sleep(150 * time.Millisecond)
	withdrawn, err := staking.CompleteUnbonding(delegator)
	if err != nil {
		t.Fatalf("CompleteUnbonding failed: %v", err)
	}
	if withdrawn != 5_000_000_000 {
		t.Errorf("Expected withdrawn 5B, got %d", withdrawn)
	}
}

func TestStakingManager_Rewards(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	v, _ := registry.Register(testPubKey(1), testAddress(1), 50_000_000_000)
	registry.Activate(v.Index, 1)

	staking := NewStakingManager(nil, registry)
	delegator := testAddress(100)
	staking.Delegate(delegator, v.Index, 5_000_000_000)

	staking.AddPendingRewards(delegator, v.Index, 1_000_000)

	rewards, err := staking.ClaimRewards(delegator, v.Index)
	if err != nil {
		t.Fatalf("ClaimRewards failed: %v", err)
	}
	if rewards != 1_000_000 {
		t.Errorf("Expected rewards 1M, got %d", rewards)
	}
}

// === Slashing Engine Tests ===

func TestSlashingEngine_Equivocation(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	v, _ := registry.Register(testPubKey(1), testAddress(1), 100_000_000_000)
	registry.Activate(v.Index, 1)

	slashing := NewSlashingEngine(nil, registry)

	event, err := slashing.SlashEquivocation(v.Index, 5, []byte("evidence"), testHash(1))
	if err != nil {
		t.Fatalf("SlashEquivocation failed: %v", err)
	}

	// 5% slash rate on 100B stake = 5B slashed
	if event.Amount != 5_000_000_000 {
		t.Errorf("Expected slash amount 5B, got %d", event.Amount)
	}
	if event.Reason != SlashEquivocation {
		t.Errorf("Expected equivocation reason")
	}

	v = registry.Get(v.Index)
	if v.Status != ValidatorSlashed {
		t.Errorf("Expected slashed status")
	}
	if v.EffectiveStake != 95_000_000_000 {
		t.Errorf("Expected effective stake 95B, got %d", v.EffectiveStake)
	}
}

func TestSlashingEngine_DoubleSlash(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	v, _ := registry.Register(testPubKey(1), testAddress(1), 100_000_000_000)
	registry.Activate(v.Index, 1)

	slashing := NewSlashingEngine(nil, registry)
	slashing.SlashEquivocation(v.Index, 5, nil, testHash(1))

	_, err := slashing.SlashEquivocation(v.Index, 5, nil, testHash(2))
	if err != ErrAlreadySlashed {
		t.Errorf("Expected ErrAlreadySlashed, got %v", err)
	}
}

func TestSlashingEngine_Downtime(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	v, _ := registry.Register(testPubKey(1), testAddress(1), 100_000_000_000)
	registry.Activate(v.Index, 1)

	config := &SlashingConfig{
		EquivocationSlashRate: 500,
		DowntimeSlashRate:     100,
		InvalidBlockSlashRate: 1000,
		DowntimeThreshold:     5,
		DowntimeWindow:        100,
		SlashCooldown:         10,
		JailDuration:          time.Hour,
	}
	slashing := NewSlashingEngine(config, registry)

	// Record missed blocks until threshold
	for i := 0; i < 4; i++ {
		shouldSlash := slashing.RecordMissedBlock(v.Index)
		if shouldSlash {
			t.Errorf("Should not slash yet at count %d", i+1)
		}
	}

	// This should trigger threshold
	shouldSlash := slashing.RecordMissedBlock(v.Index)
	if !shouldSlash {
		t.Errorf("Should trigger slash at threshold")
	}

	// Now slash for downtime
	event, err := slashing.SlashDowntime(v.Index, 10)
	if err != nil {
		t.Fatalf("SlashDowntime failed: %v", err)
	}

	// 1% slash rate on 100B = 1B
	if event.Amount != 1_000_000_000 {
		t.Errorf("Expected slash amount 1B, got %d", event.Amount)
	}
}

// === Rewards Distributor Tests ===

func TestRewardsDistributor_BlockReward(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	v, _ := registry.Register(testPubKey(1), testAddress(1), 100_000_000_000)
	registry.Activate(v.Index, 1)

	rewards := NewRewardsDistributor(nil, registry, nil)

	err := rewards.DistributeBlockReward(v.Index, testHash(1), 1_000_000_000)
	if err != nil {
		t.Fatalf("DistributeBlockReward failed: %v", err)
	}

	pending := rewards.GetPendingRewards(v.Index)
	if pending == 0 {
		t.Errorf("Expected pending rewards > 0")
	}

	claimed, err := rewards.ClaimRewards(v.Index)
	if err != nil {
		t.Fatalf("ClaimRewards failed: %v", err)
	}
	if claimed != pending {
		t.Errorf("Expected claimed %d, got %d", pending, claimed)
	}

	// Should be zero after claiming
	if rewards.GetPendingRewards(v.Index) != 0 {
		t.Errorf("Expected 0 pending after claim")
	}
}

func TestRewardsDistributor_WithDelegation(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	v, _ := registry.Register(testPubKey(1), testAddress(1), 50_000_000_000)
	registry.Activate(v.Index, 1)

	staking := NewStakingManager(nil, registry)
	delegator := testAddress(100)
	staking.Delegate(delegator, v.Index, 50_000_000_000)

	rewards := NewRewardsDistributor(nil, registry, staking)

	err := rewards.DistributeBlockReward(v.Index, testHash(1), 0)
	if err != nil {
		t.Fatalf("DistributeBlockReward failed: %v", err)
	}

	// Delegator should have pending rewards
	dels := staking.GetDelegations(delegator)
	if len(dels) == 0 || dels[0].PendingRewards == 0 {
		t.Errorf("Expected delegator to have pending rewards")
	}
}

func TestRewardsDistributor_ProtocolTreasury(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	v, _ := registry.Register(testPubKey(1), testAddress(1), 100_000_000_000)
	registry.Activate(v.Index, 1)

	rewards := NewRewardsDistributor(nil, registry, nil)

	// Fees with protocol share
	fees := uint64(10_000_000_000) // 10 tokens
	rewards.DistributeBlockReward(v.Index, testHash(1), fees)

	// 10% of fees should go to treasury
	treasury := rewards.GetProtocolTreasury()
	expectedTreasury := (fees * 1000) / 10000 // 10%
	if treasury != expectedTreasury {
		t.Errorf("Expected treasury %d, got %d", expectedTreasury, treasury)
	}
}

// === Validator Set Manager Tests ===

func TestValidatorSetManager_Initialize(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	for i := uint32(0); i < 5; i++ {
		v, _ := registry.Register(testPubKey(i), testAddress(i), 50_000_000_000+uint64(i)*10_000_000_000)
		registry.Activate(v.Index, 1)
	}

	vsm := NewValidatorSetManager(nil, registry)
	err := vsm.InitializeSet(1)
	if err != nil {
		t.Fatalf("InitializeSet failed: %v", err)
	}

	set := vsm.GetCurrentSet()
	if set == nil {
		t.Fatal("Expected current set")
	}
	if len(set.Validators) != 5 {
		t.Errorf("Expected 5 validators, got %d", len(set.Validators))
	}

	// Validators should be sorted by stake descending
	for i := 0; i < len(set.Validators)-1; i++ {
		if set.Validators[i].EffectiveStake < set.Validators[i+1].EffectiveStake {
			t.Errorf("Validators not sorted by stake")
		}
	}
}

func TestValidatorSetManager_HasQuorum(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	// Need at least 4 validators (default MinActiveValidators)
	for i := uint32(0); i < 4; i++ {
		v, _ := registry.Register(testPubKey(i), testAddress(i), 100_000_000_000)
		registry.Activate(v.Index, 1)
	}

	vsm := NewValidatorSetManager(nil, registry)
	err := vsm.InitializeSet(1)
	if err != nil {
		t.Fatalf("InitializeSet failed: %v", err)
	}

	// Get the actual threshold from the manager
	threshold := vsm.GetQuorumThreshold()
	if threshold == 0 {
		t.Fatal("Threshold should not be 0")
	}

	// Below threshold should not pass
	if vsm.HasQuorum(threshold - 1) {
		t.Errorf("Should not have quorum below threshold")
	}

	// At or above threshold should pass
	if !vsm.HasQuorum(threshold) {
		t.Errorf("Should have quorum at threshold")
	}
	if !vsm.HasQuorum(threshold + 1_000_000_000) {
		t.Errorf("Should have quorum above threshold")
	}
}

func TestValidatorSetManager_EpochTransition(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	for i := uint32(0); i < 5; i++ {
		v, _ := registry.Register(testPubKey(i), testAddress(i), 50_000_000_000)
		registry.Activate(v.Index, 1)
	}

	vsm := NewValidatorSetManager(nil, registry)
	vsm.InitializeSet(1)

	// Queue new validator for activation
	newV, _ := registry.Register(testPubKey(10), testAddress(10), 50_000_000_000)
	vsm.QueueActivation(newV.Index)

	// Process transition
	transition, err := vsm.ProcessEpochTransition(2)
	if err != nil {
		t.Fatalf("ProcessEpochTransition failed: %v", err)
	}

	if len(transition.Activated) != 1 {
		t.Errorf("Expected 1 activation, got %d", len(transition.Activated))
	}

	// New set should have 6 validators
	set := vsm.GetCurrentSet()
	if len(set.Validators) != 6 {
		t.Errorf("Expected 6 validators, got %d", len(set.Validators))
	}
}

func TestValidatorSetManager_SelectProposer(t *testing.T) {
	registry := NewValidatorRegistry(nil)
	// Use different stakes to ensure different proposers get selected
	// Need at least 4 validators (default MinActiveValidators)
	stakes := []uint64{50_000_000_000, 75_000_000_000, 100_000_000_000, 150_000_000_000}
	for i := uint32(0); i < 4; i++ {
		v, _ := registry.Register(testPubKey(i), testAddress(i), stakes[i])
		registry.Activate(v.Index, 1)
	}

	vsm := NewValidatorSetManager(nil, registry)
	err := vsm.InitializeSet(1)
	if err != nil {
		t.Fatalf("InitializeSet failed: %v", err)
	}

	// Different slots should select different proposers based on stake-weighting
	proposers := make(map[uint32]int)
	for slot := uint64(0); slot < 400; slot++ {
		p := vsm.SelectProposer(slot)
		proposers[p]++
	}

	// With varying stake, should select at least 2 different proposers
	if len(proposers) < 2 {
		t.Errorf("Expected multiple proposers to be selected, got %d", len(proposers))
	}
}

// === Integration Test ===

func TestPoSIntegration(t *testing.T) {
	// Setup
	registry := NewValidatorRegistry(nil)
	stakingConfig := &StakingConfig{
		MinDelegation:              1_000_000_000,
		UnbondingPeriod:            100 * time.Millisecond,
		MaxDelegationsPerValidator: 10000,
		DepositDelay:               0,
		RewardClaimDelay:           0,
	}
	staking := NewStakingManager(stakingConfig, registry)
	slashing := NewSlashingEngine(nil, registry)
	rewards := NewRewardsDistributor(nil, registry, staking)
	vsm := NewValidatorSetManager(nil, registry)

	// Register validators
	for i := uint32(0); i < 4; i++ {
		v, _ := registry.Register(testPubKey(i), testAddress(i), 50_000_000_000)
		registry.Activate(v.Index, 1)
	}

	// Initialize validator set
	vsm.InitializeSet(1)

	// Delegate to validator 0
	delegator := testAddress(100)
	staking.Delegate(delegator, 0, 10_000_000_000)

	// Distribute block reward
	rewards.DistributeBlockReward(0, testHash(1), 1_000_000_000)

	// Verify delegator has rewards
	dels := staking.GetDelegations(delegator)
	if len(dels) == 0 || dels[0].PendingRewards == 0 {
		t.Errorf("Expected delegator rewards")
	}

	// Slash validator 1 for equivocation
	slashing.SlashEquivocation(1, 1, nil, testHash(10))
	v := registry.Get(1)
	if v.Status != ValidatorSlashed {
		t.Errorf("Expected validator 1 to be slashed")
	}

	// Epoch transition (slashed validator removed from active set)
	vsm.ProcessEpochTransition(2)

	// Verify quorum still works with 3 active validators
	if !vsm.HasQuorum(150_000_000_000) { // 3 * 50B = 150B total, need 2/3
		t.Errorf("Should still have quorum")
	}

	// Undelegate
	staking.Undelegate(delegator, 0, 10_000_000_000)
	time.Sleep(150 * time.Millisecond)
	withdrawn, _ := staking.CompleteUnbonding(delegator)
	if withdrawn != 10_000_000_000 {
		t.Errorf("Expected full withdrawal")
	}
}
