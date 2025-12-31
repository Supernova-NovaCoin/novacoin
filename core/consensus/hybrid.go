// Package consensus implements the hybrid consensus orchestrator for NovaCoin.
//
// HybridConsensus coordinates multiple consensus layers:
//   - DAGKnight: Adaptive K parameter based on network latency
//   - GHOSTDAG: Stake-weighted blue/red block coloring
//   - Shoal++: Multi-anchor, parallel DAGs for high throughput
//   - Mysticeti: 3-round commit for fast finality
//   - MEV Resistance: Threshold encryption for fair ordering
//
// The consensus flow is:
//  1. DAGKnight adapts K parameter (0.25-0.50) based on network latency
//  2. GHOSTDAG performs stake-weighted blue/red coloring
//  3. Shoal++ manages waves and anchors (all validators are anchors)
//  4. Mysticeti commits blocks after 2f+1 stake at depth 3
//  5. MEV layer decrypts and orders transactions fairly
//  6. EVM executes transactions and updates state
package consensus

import (
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
	"github.com/Supernova-NovaCoin/novacoin/core/dagknight"
	"github.com/Supernova-NovaCoin/novacoin/core/ghostdag"
	"github.com/Supernova-NovaCoin/novacoin/core/mev"
	"github.com/Supernova-NovaCoin/novacoin/core/mysticeti"
	"github.com/Supernova-NovaCoin/novacoin/core/pos"
	"github.com/Supernova-NovaCoin/novacoin/core/shoal"
	"github.com/Supernova-NovaCoin/novacoin/core/state"
)

// Common errors.
var (
	ErrNotRunning         = errors.New("consensus not running")
	ErrAlreadyRunning     = errors.New("consensus already running")
	ErrNoValidators       = errors.New("no validators registered")
	ErrInvalidBlock       = errors.New("invalid block")
	ErrBlockNotFinalized  = errors.New("block not finalized")
	ErrInsufficientStake  = errors.New("insufficient stake for quorum")
	ErrExecutionFailed    = errors.New("transaction execution failed")
)

// Config holds the hybrid consensus configuration.
type Config struct {
	// Sub-engine configurations
	DAGKnight *dagknight.Config
	GHOSTDAG  *ghostdag.Config
	Shoal     *shoal.Config
	Mysticeti *mysticeti.Config
	MEV       *mev.MEVEngineConfig

	// Block production
	BlockInterval    time.Duration // Target block interval
	MaxTxPerBlock    int           // Maximum transactions per block
	MaxBlockSize     int           // Maximum block size in bytes
	BlockGasLimit    uint64        // Block gas limit

	// Finality
	FinalityDepth    int           // Blocks needed for finality
	FinalityTimeout  time.Duration // Timeout for finality

	// Quorum
	QuorumThreshold  float64 // 2/3 for BFT

	// Chain
	ChainID *big.Int // Chain identifier
}

// DefaultConfig returns the default hybrid consensus configuration.
func DefaultConfig() *Config {
	return &Config{
		DAGKnight: dagknight.DefaultConfig(),
		GHOSTDAG:  ghostdag.DefaultConfig(),
		Shoal:     shoal.DefaultConfig(),
		Mysticeti: mysticeti.DefaultConfig(),
		MEV:       mev.DefaultMEVEngineConfig(),

		BlockInterval:   500 * time.Millisecond,
		MaxTxPerBlock:   10000,
		MaxBlockSize:    2 * 1024 * 1024, // 2MB
		BlockGasLimit:   30_000_000,

		FinalityDepth:   3,
		FinalityTimeout: 5 * time.Second,

		QuorumThreshold: 2.0 / 3.0,

		ChainID: big.NewInt(1),
	}
}

// HybridConsensus orchestrates all consensus layers.
type HybridConsensus struct {
	config *Config

	// Sub-engines
	dagKnight *dagknight.Engine
	ghostDAG  *ghostdag.GHOSTDAGEngine
	shoal     *shoal.Engine
	mysticeti *mysticeti.Engine
	mev       *mev.MEVEngine

	// Storage
	dagStore     *dag.Store
	stateDB      *state.StateDB
	validatorSet *pos.ValidatorSetManager

	// Block production
	blockProducer *BlockProducer
	txExecutor    *TxExecutor

	// State
	running      bool
	currentEpoch uint64
	lastBlock    dag.Hash
	lastFinalized dag.Hash

	// Channels
	blockCh     chan *dag.Vertex
	finalizeCh  chan dag.Hash
	stopCh      chan struct{}

	// Callbacks
	onBlockProduced  func(*dag.Vertex)
	onBlockFinalized func(*dag.Vertex)
	onEpochChange    func(uint64)

	// Metrics
	metrics *Metrics

	mu sync.RWMutex
}

// Metrics holds consensus performance metrics.
type Metrics struct {
	BlocksProduced     uint64
	BlocksFinalized    uint64
	TxExecuted         uint64
	TxFailed           uint64
	AvgBlockTime       time.Duration
	AvgFinalityTime    time.Duration
	AvgTxPerBlock      float64
	CurrentK           float64
	CurrentWave        uint64
	CurrentRound       uint64
	TotalStake         uint64
	ActiveValidators   int

	mu sync.RWMutex
}

// NewHybridConsensus creates a new hybrid consensus engine.
func NewHybridConsensus(
	config *Config,
	dagStore *dag.Store,
	stateDB *state.StateDB,
	validatorSet *pos.ValidatorSetManager,
) *HybridConsensus {
	if config == nil {
		config = DefaultConfig()
	}

	// Create latency monitor for DAGKnight
	latencyMonitor := dagknight.NewLatencyMonitor()

	// Create vertex store adapters for Shoal++ and Mysticeti
	shoalStore := &shoalStoreAdapter{store: dagStore}
	mysticetiStore := &mysticetiStoreAdapter{store: dagStore}

	hc := &HybridConsensus{
		config:       config,
		dagStore:     dagStore,
		stateDB:      stateDB,
		validatorSet: validatorSet,

		// Initialize sub-engines
		dagKnight: dagknight.NewEngine(config.DAGKnight, latencyMonitor),
		ghostDAG:  ghostdag.NewGHOSTDAGEngine(dagStore, config.GHOSTDAG),
		shoal:     shoal.NewEngine(config.Shoal, shoalStore),
		mysticeti: mysticeti.NewEngine(config.Mysticeti, mysticetiStore),
		mev:       mev.NewMEVEngine(config.MEV),

		blockCh:    make(chan *dag.Vertex, 1000),
		finalizeCh: make(chan dag.Hash, 100),
		stopCh:     make(chan struct{}),

		metrics: &Metrics{},
	}

	// Set up callbacks between engines
	hc.setupCallbacks()

	// Create block producer and tx executor
	hc.blockProducer = NewBlockProducer(hc)
	hc.txExecutor = NewTxExecutor(hc)

	return hc
}

// setupCallbacks wires up callbacks between consensus layers.
func (hc *HybridConsensus) setupCallbacks() {
	// Mysticeti -> Block finalization
	hc.mysticeti.OnBlockCommit(func(v *dag.Vertex, cert *mysticeti.CommitCertificate) {
		hc.handleBlockCommit(v, cert)
	})

	// Mysticeti -> Round advancement
	hc.mysticeti.OnRoundAdvance(func(r *mysticeti.Round) {
		hc.handleRoundAdvance(r)
	})

	// Shoal++ -> Wave commits
	hc.shoal.OnWaveCommit(func(w *shoal.Wave) {
		hc.handleWaveCommit(w)
	})

	// Shoal++ -> Block ordering
	hc.shoal.OnBlockOrdered(func(hash dag.Hash, position int) {
		hc.handleBlockOrdered(hash, position)
	})
}

// Start begins the hybrid consensus engine.
func (hc *HybridConsensus) Start() error {
	hc.mu.Lock()
	if hc.running {
		hc.mu.Unlock()
		return ErrAlreadyRunning
	}
	hc.running = true
	hc.mu.Unlock()

	// Initialize validators in sub-engines
	hc.syncValidators()

	// Start sub-engines
	if err := hc.shoal.Start(); err != nil {
		return err
	}
	if err := hc.mysticeti.Start(); err != nil {
		hc.shoal.Stop()
		return err
	}
	if err := hc.mev.Start(); err != nil {
		hc.mysticeti.Stop()
		hc.shoal.Stop()
		return err
	}

	// Start main loops
	go hc.blockLoop()
	go hc.finalizeLoop()
	go hc.adaptLoop()

	return nil
}

// Stop halts the hybrid consensus engine.
func (hc *HybridConsensus) Stop() error {
	hc.mu.Lock()
	if !hc.running {
		hc.mu.Unlock()
		return nil
	}
	hc.running = false
	hc.mu.Unlock()

	close(hc.stopCh)

	// Stop sub-engines
	hc.mev.Stop()
	hc.mysticeti.Stop()
	hc.shoal.Stop()

	return nil
}

// blockLoop processes incoming blocks.
func (hc *HybridConsensus) blockLoop() {
	for {
		select {
		case <-hc.stopCh:
			return
		case vertex := <-hc.blockCh:
			hc.processBlock(vertex)
		}
	}
}

// processBlock handles a new block through all consensus layers.
func (hc *HybridConsensus) processBlock(vertex *dag.Vertex) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	// 1. Apply DAGKnight adaptive parameters
	vertex.AdaptiveK = float32(hc.dagKnight.GetCurrentK())
	vertex.ObservedLatencyMs = uint32(hc.getNetworkLatency())

	// 2. Run GHOSTDAG coloring
	hc.ghostDAG.ProcessVertex(vertex, float64(vertex.AdaptiveK))

	// 3. Add to Shoal++ for wave tracking
	_, err := hc.shoal.ProposeAnchor(
		vertex.Hash,
		vertex.ValidatorIndex,
		vertex.Parents,
	)
	if err != nil {
		// Log error but continue
	}

	// 4. Submit to Mysticeti for commit
	hc.mysticeti.ReceiveBlock(vertex)

	// Update metrics
	hc.metrics.mu.Lock()
	hc.metrics.BlocksProduced++
	hc.metrics.CurrentK = hc.dagKnight.GetCurrentK()
	hc.metrics.CurrentWave = hc.shoal.GetCurrentWaveNumber()
	hc.metrics.CurrentRound = hc.mysticeti.GetCurrentRoundNumber()
	hc.metrics.mu.Unlock()

	// Notify callback
	if hc.onBlockProduced != nil {
		go hc.onBlockProduced(vertex)
	}
}

// finalizeLoop handles block finalization.
func (hc *HybridConsensus) finalizeLoop() {
	for {
		select {
		case <-hc.stopCh:
			return
		case hash := <-hc.finalizeCh:
			hc.finalizeBlock(hash)
		}
	}
}

// finalizeBlock marks a block as finalized.
func (hc *HybridConsensus) finalizeBlock(hash dag.Hash) {
	vertex := hc.dagStore.Get(hash)
	if vertex == nil {
		return
	}

	// Update finality fields
	vertex.FinalizedAt = time.Now()
	vertex.FinalityScore = 100

	// Update state
	hc.mu.Lock()
	hc.lastFinalized = hash
	hc.mu.Unlock()

	// Update metrics
	hc.metrics.mu.Lock()
	hc.metrics.BlocksFinalized++
	hc.metrics.mu.Unlock()

	// Notify callback
	if hc.onBlockFinalized != nil {
		go hc.onBlockFinalized(vertex)
	}
}

// adaptLoop periodically adapts consensus parameters.
func (hc *HybridConsensus) adaptLoop() {
	ticker := time.NewTicker(hc.config.BlockInterval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.stopCh:
			return
		case <-ticker.C:
			hc.adaptParameters()
		}
	}
}

// adaptParameters adjusts consensus parameters based on network conditions.
func (hc *HybridConsensus) adaptParameters() {
	// Adapt K parameter
	if hc.dagKnight.ShouldAdaptK() {
		hc.dagKnight.AdaptK()
	}

	// Adapt block time
	if hc.dagKnight.ShouldAdaptBlockTime() {
		hc.dagKnight.AdaptBlockTime()
	}
}

// === Callback Handlers ===

func (hc *HybridConsensus) handleBlockCommit(v *dag.Vertex, cert *mysticeti.CommitCertificate) {
	// Mark as committed
	v.CommitRound = cert.Round
	v.CommitDepth = cert.CommitDepth

	// Check if fast path
	if cert.Status == mysticeti.CommitFastPath {
		v.CommitDepth = 2
	}

	// Send for finalization
	select {
	case hc.finalizeCh <- v.Hash:
	default:
	}
}

func (hc *HybridConsensus) handleRoundAdvance(r *mysticeti.Round) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	// Update metrics
	hc.metrics.mu.Lock()
	hc.metrics.CurrentRound = r.Number
	hc.metrics.mu.Unlock()
}

func (hc *HybridConsensus) handleWaveCommit(w *shoal.Wave) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	// Update metrics
	hc.metrics.mu.Lock()
	hc.metrics.CurrentWave = w.Number
	hc.metrics.mu.Unlock()
}

func (hc *HybridConsensus) handleBlockOrdered(hash dag.Hash, position int) {
	// Block ordering determined by Shoal++
	// This is used for MEV-resistant transaction ordering
}

// === Public API ===

// ProposeBlock creates and proposes a new block.
func (hc *HybridConsensus) ProposeBlock(
	validatorIndex uint32,
	pubKey dag.PublicKey,
	stake uint64,
	transactions [][]byte,
) (*dag.Vertex, error) {
	hc.mu.RLock()
	if !hc.running {
		hc.mu.RUnlock()
		return nil, ErrNotRunning
	}
	hc.mu.RUnlock()

	return hc.blockProducer.ProduceBlock(validatorIndex, pubKey, stake, transactions)
}

// ReceiveBlock processes a block received from the network.
func (hc *HybridConsensus) ReceiveBlock(vertex *dag.Vertex) error {
	hc.mu.RLock()
	if !hc.running {
		hc.mu.RUnlock()
		return ErrNotRunning
	}
	hc.mu.RUnlock()

	// Validate block
	if err := hc.validateBlock(vertex); err != nil {
		return err
	}

	// Store in DAG
	hc.dagStore.Add(vertex)

	// Submit for processing
	select {
	case hc.blockCh <- vertex:
		return nil
	default:
		return errors.New("block channel full")
	}
}

// validateBlock validates a block before processing.
func (hc *HybridConsensus) validateBlock(vertex *dag.Vertex) error {
	// Check timestamp
	if vertex.Timestamp.After(time.Now().Add(10 * time.Second)) {
		return ErrInvalidBlock
	}

	// Check parents exist
	for _, parent := range vertex.Parents {
		if hc.dagStore.Get(parent) == nil {
			return ErrInvalidBlock
		}
	}

	// Check validator
	validator := hc.validatorSet.GetValidator(vertex.ValidatorIndex)
	if validator == nil {
		return ErrInvalidBlock
	}

	return nil
}

// ExecuteTransactions executes transactions for a block.
func (hc *HybridConsensus) ExecuteTransactions(vertex *dag.Vertex) (*ExecutionResult, error) {
	return hc.txExecutor.Execute(vertex)
}

// GetFinalizedBlock returns the last finalized block.
func (hc *HybridConsensus) GetFinalizedBlock() *dag.Vertex {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.dagStore.Get(hc.lastFinalized)
}

// GetLastBlock returns the last processed block.
func (hc *HybridConsensus) GetLastBlock() *dag.Vertex {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.dagStore.Get(hc.lastBlock)
}

// IsFinalized checks if a block is finalized.
func (hc *HybridConsensus) IsFinalized(hash dag.Hash) bool {
	vertex := hc.dagStore.Get(hash)
	return vertex != nil && vertex.IsFinalized()
}

// GetCurrentK returns the current adaptive K parameter.
func (hc *HybridConsensus) GetCurrentK() float64 {
	return hc.dagKnight.GetCurrentK()
}

// GetCurrentBlockTime returns the current adaptive block time.
func (hc *HybridConsensus) GetCurrentBlockTime() time.Duration {
	return hc.dagKnight.GetCurrentBlockTime()
}

// GetCurrentWave returns the current Shoal++ wave.
func (hc *HybridConsensus) GetCurrentWave() *shoal.Wave {
	return hc.shoal.GetCurrentWave()
}

// GetCurrentRound returns the current Mysticeti round.
func (hc *HybridConsensus) GetCurrentRound() *mysticeti.Round {
	return hc.mysticeti.GetCurrentRound()
}

// GetMetrics returns consensus metrics.
func (hc *HybridConsensus) GetMetrics() *Metrics {
	hc.metrics.mu.RLock()
	defer hc.metrics.mu.RUnlock()

	return &Metrics{
		BlocksProduced:   hc.metrics.BlocksProduced,
		BlocksFinalized:  hc.metrics.BlocksFinalized,
		TxExecuted:       hc.metrics.TxExecuted,
		TxFailed:         hc.metrics.TxFailed,
		AvgBlockTime:     hc.metrics.AvgBlockTime,
		AvgFinalityTime:  hc.metrics.AvgFinalityTime,
		AvgTxPerBlock:    hc.metrics.AvgTxPerBlock,
		CurrentK:         hc.metrics.CurrentK,
		CurrentWave:      hc.metrics.CurrentWave,
		CurrentRound:     hc.metrics.CurrentRound,
		TotalStake:       hc.metrics.TotalStake,
		ActiveValidators: hc.metrics.ActiveValidators,
	}
}

// === Validator Management ===

// syncValidators synchronizes validators across all engines.
func (hc *HybridConsensus) syncValidators() {
	currentSet := hc.validatorSet.GetCurrentSet()
	if currentSet == nil {
		return
	}

	validators := currentSet.Validators

	for _, v := range validators {
		hc.shoal.RegisterValidator(v.Index, v.PubKey, v.EffectiveStake)
		hc.mysticeti.RegisterValidator(v.Index, v.PubKey, v.EffectiveStake)
	}

	hc.metrics.mu.Lock()
	hc.metrics.TotalStake = currentSet.TotalStake
	hc.metrics.ActiveValidators = len(validators)
	hc.metrics.mu.Unlock()
}

// UpdateValidator updates a validator's stake.
func (hc *HybridConsensus) UpdateValidator(index uint32, stake uint64) {
	hc.shoal.UpdateValidatorStake(index, stake)
	hc.mysticeti.UpdateValidatorStake(index, stake)
}

// === Callbacks ===

// OnBlockProduced sets a callback for block production.
func (hc *HybridConsensus) OnBlockProduced(callback func(*dag.Vertex)) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.onBlockProduced = callback
}

// OnBlockFinalized sets a callback for block finalization.
func (hc *HybridConsensus) OnBlockFinalized(callback func(*dag.Vertex)) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.onBlockFinalized = callback
}

// OnEpochChange sets a callback for epoch changes.
func (hc *HybridConsensus) OnEpochChange(callback func(uint64)) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.onEpochChange = callback
}

// === Helper Methods ===

func (hc *HybridConsensus) getNetworkLatency() float64 {
	state := hc.dagKnight.GetLatencyMonitor().GetNetworkState()
	if state == nil {
		return 100 // Default 100ms
	}
	return state.MedianLatency
}

// === Store Adapters ===

// shoalStoreAdapter adapts dag.Store to shoal.VertexStore.
type shoalStoreAdapter struct {
	store *dag.Store
}

func (a *shoalStoreAdapter) Get(hash dag.Hash) *dag.Vertex {
	return a.store.Get(hash)
}

func (a *shoalStoreAdapter) GetAll() []*dag.Vertex {
	return a.store.GetAll()
}

func (a *shoalStoreAdapter) GetTips() []*dag.Vertex {
	return a.store.GetTips()
}

// mysticetiStoreAdapter adapts dag.Store to mysticeti.VertexStore.
type mysticetiStoreAdapter struct {
	store *dag.Store
}

func (a *mysticetiStoreAdapter) Get(hash dag.Hash) *dag.Vertex {
	return a.store.Get(hash)
}

func (a *mysticetiStoreAdapter) GetAll() []*dag.Vertex {
	return a.store.GetAll()
}

func (a *mysticetiStoreAdapter) GetChildren(hash dag.Hash) []*dag.Vertex {
	return a.store.GetChildren(hash)
}

func (a *mysticetiStoreAdapter) GetTips() []*dag.Vertex {
	return a.store.GetTips()
}
