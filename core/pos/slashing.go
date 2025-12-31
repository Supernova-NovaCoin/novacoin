// Package pos implements the Proof of Stake foundation for NovaCoin.
package pos

import (
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// SlashReason represents the reason for slashing.
type SlashReason int

const (
	SlashEquivocation SlashReason = iota // Double-signing
	SlashDowntime                        // Liveness failure
	SlashInvalidBlock                    // Produced invalid block
)

func (r SlashReason) String() string {
	switch r {
	case SlashEquivocation:
		return "equivocation"
	case SlashDowntime:
		return "downtime"
	case SlashInvalidBlock:
		return "invalid_block"
	default:
		return "unknown"
	}
}

// SlashEvent records a slashing event.
type SlashEvent struct {
	ValidatorIndex uint32
	Reason         SlashReason
	Amount         uint64
	Epoch          uint64
	Timestamp      time.Time
	Evidence       []byte
	BlockHash      dag.Hash
}

// SlashingEngine handles validator slashing.
type SlashingEngine struct {
	config   *SlashingConfig
	registry *ValidatorRegistry

	events         []*SlashEvent
	slashedInEpoch map[uint64]map[uint32]bool // epoch -> validator -> slashed
	downtimeCount  map[uint32]uint64          // validator -> missed blocks

	totalSlashed uint64
	mu           sync.RWMutex
}

// SlashingConfig holds slashing configuration.
type SlashingConfig struct {
	EquivocationSlashRate uint16 // Basis points (500 = 5%)
	DowntimeSlashRate     uint16
	InvalidBlockSlashRate uint16
	DowntimeThreshold     uint64 // Missed blocks before slashing
	DowntimeWindow        uint64 // Blocks to check for downtime
	SlashCooldown         uint64 // Epochs between slashes
	JailDuration          time.Duration
}

// DefaultSlashingConfig returns default configuration.
func DefaultSlashingConfig() *SlashingConfig {
	return &SlashingConfig{
		EquivocationSlashRate: 500,  // 5%
		DowntimeSlashRate:     100,  // 1%
		InvalidBlockSlashRate: 1000, // 10%
		DowntimeThreshold:     100,
		DowntimeWindow:        1000,
		SlashCooldown:         10,
		JailDuration:          7 * 24 * time.Hour,
	}
}

// NewSlashingEngine creates a new slashing engine.
func NewSlashingEngine(config *SlashingConfig, registry *ValidatorRegistry) *SlashingEngine {
	if config == nil {
		config = DefaultSlashingConfig()
	}
	return &SlashingEngine{
		config:         config,
		registry:       registry,
		events:         make([]*SlashEvent, 0),
		slashedInEpoch: make(map[uint64]map[uint32]bool),
		downtimeCount:  make(map[uint32]uint64),
	}
}

// SlashEquivocation slashes a validator for double-signing.
func (se *SlashingEngine) SlashEquivocation(validatorIndex uint32, epoch uint64, evidence []byte, blockHash dag.Hash) (*SlashEvent, error) {
	return se.slash(validatorIndex, SlashEquivocation, epoch, evidence, blockHash)
}

// SlashDowntime slashes a validator for downtime.
func (se *SlashingEngine) SlashDowntime(validatorIndex uint32, epoch uint64) (*SlashEvent, error) {
	return se.slash(validatorIndex, SlashDowntime, epoch, nil, dag.Hash{})
}

// SlashInvalidBlock slashes a validator for producing an invalid block.
func (se *SlashingEngine) SlashInvalidBlock(validatorIndex uint32, epoch uint64, blockHash dag.Hash) (*SlashEvent, error) {
	return se.slash(validatorIndex, SlashInvalidBlock, epoch, nil, blockHash)
}

func (se *SlashingEngine) slash(validatorIndex uint32, reason SlashReason, epoch uint64, evidence []byte, blockHash dag.Hash) (*SlashEvent, error) {
	se.mu.Lock()
	defer se.mu.Unlock()

	v := se.registry.Get(validatorIndex)
	if v == nil {
		return nil, ErrValidatorNotFound
	}

	// Check cooldown
	if se.slashedInEpoch[epoch] == nil {
		se.slashedInEpoch[epoch] = make(map[uint32]bool)
	}
	if se.slashedInEpoch[epoch][validatorIndex] {
		return nil, ErrAlreadySlashed
	}

	// Calculate slash amount
	var rate uint16
	switch reason {
	case SlashEquivocation:
		rate = se.config.EquivocationSlashRate
	case SlashDowntime:
		rate = se.config.DowntimeSlashRate
	case SlashInvalidBlock:
		rate = se.config.InvalidBlockSlashRate
	}

	slashAmount := (v.EffectiveStake * uint64(rate)) / 10000

	// Apply slash
	se.registry.mu.Lock()
	v.EffectiveStake -= slashAmount
	v.Status = ValidatorSlashed
	v.SlashedEpoch = epoch
	se.registry.activeStake -= slashAmount
	se.registry.mu.Unlock()

	// Record event
	event := &SlashEvent{
		ValidatorIndex: validatorIndex,
		Reason:         reason,
		Amount:         slashAmount,
		Epoch:          epoch,
		Timestamp:      time.Now(),
		Evidence:       evidence,
		BlockHash:      blockHash,
	}
	se.events = append(se.events, event)
	se.slashedInEpoch[epoch][validatorIndex] = true
	se.totalSlashed += slashAmount

	return event, nil
}

// RecordMissedBlock records a missed block for downtime tracking.
func (se *SlashingEngine) RecordMissedBlock(validatorIndex uint32) bool {
	se.mu.Lock()
	defer se.mu.Unlock()

	se.downtimeCount[validatorIndex]++
	return se.downtimeCount[validatorIndex] >= se.config.DowntimeThreshold
}

// ResetDowntimeCount resets the missed block counter.
func (se *SlashingEngine) ResetDowntimeCount(validatorIndex uint32) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.downtimeCount[validatorIndex] = 0
}

// GetDowntimeCount returns the current downtime count.
func (se *SlashingEngine) GetDowntimeCount(validatorIndex uint32) uint64 {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.downtimeCount[validatorIndex]
}

// GetSlashEvents returns all slash events.
func (se *SlashingEngine) GetSlashEvents() []*SlashEvent {
	se.mu.RLock()
	defer se.mu.RUnlock()
	events := make([]*SlashEvent, len(se.events))
	copy(events, se.events)
	return events
}

// GetValidatorSlashEvents returns slash events for a validator.
func (se *SlashingEngine) GetValidatorSlashEvents(validatorIndex uint32) []*SlashEvent {
	se.mu.RLock()
	defer se.mu.RUnlock()

	var events []*SlashEvent
	for _, e := range se.events {
		if e.ValidatorIndex == validatorIndex {
			events = append(events, e)
		}
	}
	return events
}

// GetTotalSlashed returns total slashed amount.
func (se *SlashingEngine) GetTotalSlashed() uint64 {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.totalSlashed
}

// Error types
var (
	ErrAlreadySlashed = posError("validator already slashed in this epoch")
)
