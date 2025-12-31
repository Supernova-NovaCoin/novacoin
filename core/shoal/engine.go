// Package shoal implements the Shoal++ multi-anchor high-throughput consensus layer.
//
// Shoal++ achieves high throughput (100k+ TPS) through:
// - Parallel DAGs: Multiple DAGs running concurrently (default 4, staggered by 125ms)
// - Wave-based consensus: Epochs called "waves" for coordinated progress
// - Multi-anchor: All validators can propose blocks (no leader bottleneck)
// - Cross-DAG references: Links between parallel DAGs for consistency
package shoal

import (
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// Engine is the main Shoal++ consensus engine.
// It coordinates waves, anchors, and parallel DAGs for high throughput.
type Engine struct {
	config *Config

	// Core components
	waveTracker    *WaveTracker
	anchorManager  *AnchorManager
	parallelCoord  *ParallelDAGCoordinator

	// DAG storage interface
	store VertexStore

	// State
	running     bool
	currentWave uint64

	// Channels for coordination
	waveCommitCh chan *Wave
	anchorCh     chan *Anchor
	stopCh       chan struct{}

	// Callbacks
	onWaveCommit   func(*Wave)
	onAnchorCommit func(*Anchor)
	onBlockOrdered func(dag.Hash, int)

	// Metrics
	metrics *Metrics

	mu sync.RWMutex
}

// VertexStore is the interface for DAG storage.
type VertexStore interface {
	Get(hash dag.Hash) *dag.Vertex
	GetAll() []*dag.Vertex
	GetTips() []*dag.Vertex
}

// Config holds the complete Shoal++ configuration.
type Config struct {
	// Wave configuration
	Wave *WaveConfig

	// Anchor configuration
	Anchor *AnchorConfig

	// Parallel DAG configuration
	Parallel *ParallelConfig

	// Quorum threshold (2/3 for BFT)
	QuorumThreshold float64

	// Timing
	WaveInterval    time.Duration // How often to try advancing waves
	CommitTimeout   time.Duration // Timeout for commits

	// Performance
	MaxBlocksPerWave int // Maximum blocks per wave
	EnablePipelining bool // Enable wave pipelining
}

// DefaultConfig returns default Shoal++ configuration.
func DefaultConfig() *Config {
	return &Config{
		Wave:            DefaultWaveConfig(),
		Anchor:          DefaultAnchorConfig(),
		Parallel:        DefaultParallelConfig(),
		QuorumThreshold: 2.0 / 3.0,
		WaveInterval:    100 * time.Millisecond,
		CommitTimeout:   5 * time.Second,
		MaxBlocksPerWave: 10000,
		EnablePipelining: true,
	}
}

// Metrics holds Shoal++ performance metrics.
type Metrics struct {
	WavesCommitted   uint64
	WavesSkipped     uint64
	AnchorsProposed  uint64
	AnchorsCommitted uint64
	BlocksOrdered    uint64
	AvgWaveDuration  time.Duration
	AvgCommitLatency time.Duration
	Throughput       float64 // Blocks per second

	mu sync.RWMutex
}

// NewEngine creates a new Shoal++ engine.
func NewEngine(config *Config, store VertexStore) *Engine {
	if config == nil {
		config = DefaultConfig()
	}

	engine := &Engine{
		config:        config,
		store:         store,
		waveTracker:   NewWaveTracker(config.Wave),
		anchorManager: NewAnchorManager(config.Anchor),
		parallelCoord: NewParallelDAGCoordinator(config.Parallel),
		waveCommitCh:  make(chan *Wave, 100),
		anchorCh:      make(chan *Anchor, 1000),
		stopCh:        make(chan struct{}),
		metrics:       &Metrics{},
	}

	// Set up internal callbacks
	engine.waveTracker.OnWaveCommit(engine.handleWaveCommit)

	return engine
}

// Start begins the Shoal++ consensus engine.
func (e *Engine) Start() error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return ErrAlreadyRunning
	}
	e.running = true
	e.mu.Unlock()

	// Start wave progression
	go e.waveLoop()

	// Start anchor processing
	go e.anchorLoop()

	return nil
}

// Stop halts the Shoal++ engine.
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

// waveLoop manages wave progression.
func (e *Engine) waveLoop() {
	ticker := time.NewTicker(e.config.WaveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.processWave()
		}
	}
}

// processWave handles wave state transitions.
func (e *Engine) processWave() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check for timeout
	if e.waveTracker.CheckTimeout() {
		_, _ = e.waveTracker.SkipWave("timeout")
		e.metrics.mu.Lock()
		e.metrics.WavesSkipped++
		e.metrics.mu.Unlock()
		return
	}

	// Handle state transitions
	switch e.waveTracker.CurrentWave().State {
	case WaveProposing:
		if e.waveTracker.ShouldTransitionToVoting() {
			_ = e.waveTracker.TransitionToVoting()
		}

	case WaveVoting:
		if e.waveTracker.ShouldTransitionToCommitting() {
			_ = e.waveTracker.TransitionToCommitting()
		}

	case WaveCommitting:
		// Try to commit
		if e.waveTracker.CurrentWave().HasQuorum(e.config.QuorumThreshold) {
			_, _ = e.waveTracker.CommitWave()
		}
	}
}

// anchorLoop processes anchor events.
func (e *Engine) anchorLoop() {
	for {
		select {
		case <-e.stopCh:
			return
		case anchor := <-e.anchorCh:
			e.processAnchor(anchor)
		}
	}
}

// processAnchor handles a new anchor.
func (e *Engine) processAnchor(anchor *Anchor) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Record the anchor
	if e.onAnchorCommit != nil {
		e.onAnchorCommit(anchor)
	}

	e.metrics.mu.Lock()
	e.metrics.AnchorsProposed++
	e.metrics.mu.Unlock()
}

// handleWaveCommit is called when a wave is committed.
func (e *Engine) handleWaveCommit(wave *Wave) {
	e.metrics.mu.Lock()
	e.metrics.WavesCommitted++
	e.metrics.mu.Unlock()

	// Compute block ordering for this wave
	e.orderWaveBlocks(wave)

	// End wave in parallel DAG coordinator
	e.parallelCoord.EndWave()

	// Start new wave in parallel DAGs
	e.parallelCoord.StartWave(wave.Number + 1)

	// Notify callback
	if e.onWaveCommit != nil {
		e.onWaveCommit(wave)
	}

	// Update metrics
	e.updateMetrics(wave)
}

// orderWaveBlocks computes the deterministic ordering of blocks in a wave.
func (e *Engine) orderWaveBlocks(wave *Wave) {
	// Get vertex getter function
	getVertex := func(hash dag.Hash) *dag.Vertex {
		return e.store.Get(hash)
	}

	// Compute wave order
	order := ComputeWaveOrder(wave, getVertex)

	// Mark anchors with their position
	for i, hash := range order.Blocks {
		_ = e.anchorManager.CommitAnchor(hash, i)

		if e.onBlockOrdered != nil {
			e.onBlockOrdered(hash, i)
		}

		e.metrics.mu.Lock()
		e.metrics.BlocksOrdered++
		e.metrics.mu.Unlock()
	}

	// Mark non-ordered anchors as orphaned
	for _, anchor := range e.anchorManager.GetAnchorsForWave(wave.Number) {
		if anchor.OrderPosition < 0 {
			_ = e.anchorManager.OrphanAnchor(anchor.Hash)
		}
	}
}

// updateMetrics updates performance metrics.
func (e *Engine) updateMetrics(wave *Wave) {
	e.metrics.mu.Lock()
	defer e.metrics.mu.Unlock()

	// Update average wave duration
	duration := wave.Duration()
	if e.metrics.AvgWaveDuration == 0 {
		e.metrics.AvgWaveDuration = duration
	} else {
		// Exponential moving average
		e.metrics.AvgWaveDuration = (e.metrics.AvgWaveDuration*9 + duration) / 10
	}

	// Compute throughput
	if e.metrics.AvgWaveDuration > 0 {
		blocksPerWave := float64(len(wave.Anchors))
		e.metrics.Throughput = blocksPerWave / e.metrics.AvgWaveDuration.Seconds()
	}
}

// === Public API ===

// ProposeAnchor proposes a new anchor block for the current wave.
func (e *Engine) ProposeAnchor(
	hash dag.Hash,
	validatorIndex uint32,
	references []dag.Hash,
) (*Anchor, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil, ErrNotRunning
	}

	wave := e.waveTracker.CurrentWave()
	if wave == nil {
		return nil, ErrNoActiveWave
	}

	// Assign to parallel DAG
	dagID := e.anchorManager.AssignToParallelDAG(validatorIndex, wave.Number)

	// Create anchor
	anchor, err := e.anchorManager.CreateAnchor(hash, validatorIndex, wave.Number, dagID, references)
	if err != nil {
		return nil, err
	}

	// Add to wave tracker
	validator := e.anchorManager.GetValidatorInfo(validatorIndex)
	if validator != nil {
		_ = e.waveTracker.AddAnchor(hash, validatorIndex, validator.Stake)
	}

	// Add to parallel DAG
	_ = e.parallelCoord.AddBlock(dagID, hash, wave.Number)

	// Notify processing
	select {
	case e.anchorCh <- anchor:
	default:
		// Channel full, skip notification
	}

	return anchor, nil
}

// VoteForAnchor records a vote for an anchor.
func (e *Engine) VoteForAnchor(anchorHash dag.Hash, voterIndex uint32) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	validator := e.anchorManager.GetValidatorInfo(voterIndex)
	if validator == nil {
		return ErrUnknownValidator
	}

	// Record in anchor manager
	if err := e.anchorManager.RecordVote(anchorHash, voterIndex, validator.Stake); err != nil {
		return err
	}

	// Record in wave tracker
	return e.waveTracker.AddVote(anchorHash, validator.Stake)
}

// RegisterValidator registers a validator for anchor production.
func (e *Engine) RegisterValidator(index uint32, pubKey dag.PublicKey, stake uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.anchorManager.RegisterValidator(index, pubKey, stake)
	e.waveTracker.SetTotalStake(e.anchorManager.GetTotalStake())
}

// UpdateValidatorStake updates a validator's stake.
func (e *Engine) UpdateValidatorStake(index uint32, stake uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.anchorManager.UpdateValidatorStake(index, stake)
	e.waveTracker.SetTotalStake(e.anchorManager.GetTotalStake())
}

// GetCurrentWave returns the current wave.
func (e *Engine) GetCurrentWave() *Wave {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.waveTracker.CurrentWave()
}

// GetCurrentWaveNumber returns the current wave number.
func (e *Engine) GetCurrentWaveNumber() uint64 {
	return e.waveTracker.CurrentWaveNumber()
}

// GetWave returns a wave by number.
func (e *Engine) GetWave(number uint64) *Wave {
	return e.waveTracker.GetWave(number)
}

// GetAnchor returns an anchor by hash.
func (e *Engine) GetAnchor(hash dag.Hash) *Anchor {
	return e.anchorManager.GetAnchor(hash)
}

// GetAnchorsForWave returns all anchors for a wave.
func (e *Engine) GetAnchorsForWave(wave uint64) []*Anchor {
	return e.anchorManager.GetAnchorsForWave(wave)
}

// GetParallelDAG returns a specific parallel DAG.
func (e *Engine) GetParallelDAG(id uint8) *ParallelDAG {
	return e.parallelCoord.GetDAG(id)
}

// GetAllParallelDAGs returns all parallel DAGs.
func (e *Engine) GetAllParallelDAGs() []*ParallelDAG {
	return e.parallelCoord.GetAllDAGs()
}

// GetTipsAcrossDAGs returns all tips from all parallel DAGs.
func (e *Engine) GetTipsAcrossDAGs() []dag.Hash {
	return e.parallelCoord.GetTipsAcrossDAGs()
}

// AddCrossReference records a cross-DAG reference.
func (e *Engine) AddCrossReference(fromHash, toHash dag.Hash) {
	e.parallelCoord.AddCrossReference(fromHash, toHash)
}

// === Callbacks ===

// OnWaveCommit sets a callback for wave commits.
func (e *Engine) OnWaveCommit(callback func(*Wave)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onWaveCommit = callback
}

// OnAnchorCommit sets a callback for anchor commits.
func (e *Engine) OnAnchorCommit(callback func(*Anchor)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onAnchorCommit = callback
}

// OnBlockOrdered sets a callback for when a block is ordered.
func (e *Engine) OnBlockOrdered(callback func(dag.Hash, int)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onBlockOrdered = callback
}

// === Statistics ===

// GetMetrics returns current metrics.
func (e *Engine) GetMetrics() *Metrics {
	e.metrics.mu.RLock()
	defer e.metrics.mu.RUnlock()

	return &Metrics{
		WavesCommitted:   e.metrics.WavesCommitted,
		WavesSkipped:     e.metrics.WavesSkipped,
		AnchorsProposed:  e.metrics.AnchorsProposed,
		AnchorsCommitted: e.metrics.AnchorsCommitted,
		BlocksOrdered:    e.metrics.BlocksOrdered,
		AvgWaveDuration:  e.metrics.AvgWaveDuration,
		AvgCommitLatency: e.metrics.AvgCommitLatency,
		Throughput:       e.metrics.Throughput,
	}
}

// GetWaveStatistics returns wave tracker statistics.
func (e *Engine) GetWaveStatistics() *WaveStatistics {
	return e.waveTracker.GetStatistics()
}

// GetAnchorStatistics returns anchor manager statistics.
func (e *Engine) GetAnchorStatistics() *AnchorStatistics {
	return e.anchorManager.GetStatistics()
}

// GetParallelStatistics returns parallel DAG statistics.
func (e *Engine) GetParallelStatistics() *ParallelStatistics {
	return e.parallelCoord.GetStatistics()
}

// ComputeTheoreticalThroughput estimates maximum possible throughput.
func (e *Engine) ComputeTheoreticalThroughput() float64 {
	// Each parallel DAG can produce at its slot rate
	// Combined with all validators being anchors
	numDAGs := float64(e.config.Parallel.NumDAGs)
	slotDuration := e.config.Parallel.SlotDuration.Seconds()
	validatorCount := float64(e.anchorManager.GetActiveValidatorCount())

	// Each wave can have up to validatorCount anchors
	// Waves complete at the wave interval rate
	waveInterval := e.config.WaveInterval.Seconds()

	// Throughput = (validators per wave * DAG multiplier) / wave duration
	return (validatorCount * numDAGs) / (slotDuration * waveInterval)
}

// Error types for engine operations.
var (
	ErrAlreadyRunning = engineError("engine already running")
	ErrNotRunning     = engineError("engine not running")
)

type engineError string

func (e engineError) Error() string {
	return string(e)
}
