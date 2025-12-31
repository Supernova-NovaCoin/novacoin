// Package dagknight implements the DAGKnight parameterless consensus protocol.
package dagknight

import (
	"math"
	"sync"
	"time"
)

// Config defines adaptive parameter bounds for DAGKnight.
type Config struct {
	// K parameter bounds (Byzantine tolerance)
	MinK     float64 // 0.25 - tolerate 25% Byzantine
	MaxK     float64 // 0.50 - tolerate 50% Byzantine (theoretical max)
	DefaultK float64 // 0.33 - default 1/3 Byzantine

	// Block time bounds
	MinBlockTime    time.Duration // 500ms floor
	MaxBlockTime    time.Duration // 10s ceiling
	TargetBlockTime time.Duration // 1s target

	// Confidence thresholds
	HighConfidenceLatency float64 // 50ms - high confidence threshold
	LowConfidenceLatency  float64 // 1000ms - low confidence threshold

	// Adaptation rates
	KAdaptationRate    float64 // How fast K changes (0.01-0.1)
	BlockTimeAdaptRate float64 // How fast block time changes
}

// DefaultConfig returns default DAGKnight configuration.
func DefaultConfig() *Config {
	return &Config{
		MinK:                  0.25,
		MaxK:                  0.50,
		DefaultK:              0.33,
		MinBlockTime:          500 * time.Millisecond,
		MaxBlockTime:          10 * time.Second,
		TargetBlockTime:       1 * time.Second,
		HighConfidenceLatency: 50,
		LowConfidenceLatency:  1000,
		KAdaptationRate:       0.02,
		BlockTimeAdaptRate:    0.1,
	}
}

// Engine implements the DAGKnight parameterless adaptive consensus.
type Engine struct {
	config         *Config
	latencyMonitor *LatencyMonitor

	// Current adaptive values
	currentK         float64
	currentBlockTime time.Duration

	// K-cluster analysis
	kClusters      []float64
	clusterHistory []float64

	// Last adaptation timestamps
	lastKAdapt         time.Time
	lastBlockTimeAdapt time.Time

	mu sync.RWMutex
}

// NewEngine creates a new DAGKnight engine.
func NewEngine(config *Config, monitor *LatencyMonitor) *Engine {
	if config == nil {
		config = DefaultConfig()
	}
	if monitor == nil {
		monitor = NewLatencyMonitor()
	}

	return &Engine{
		config:           config,
		latencyMonitor:   monitor,
		currentK:         config.DefaultK,
		currentBlockTime: config.TargetBlockTime,
		kClusters:        make([]float64, 0),
	}
}

// AdaptK computes optimal K based on network conditions.
// This is the core DAGKnight algorithm.
func (dk *Engine) AdaptK() float64 {
	dk.mu.Lock()
	defer dk.mu.Unlock()

	state := dk.latencyMonitor.GetNetworkState()
	if state == nil || state.ActiveValidators < 4 {
		return dk.config.DefaultK // Not enough data
	}

	// === DAGKnight K-Cluster Analysis ===
	// The protocol analyzes groups of blocks (k-clusters) to find
	// the optimal K that covers 50% of honest network stake

	// Step 1: Estimate network delay from observed latencies
	estimatedDelay := state.NetworkDiameter
	if estimatedDelay == 0 {
		estimatedDelay = state.P95Latency * 2
	}

	// Step 2: Compute K based on delay tolerance
	// Higher delay -> need higher K for safety
	// Lower delay -> can use lower K for faster finality

	// Normalize delay to [0, 1] range
	delayNormalized := math.Min(estimatedDelay/2000.0, 1.0) // Cap at 2000ms

	// Map to K range based on delay
	// Low delay (fast network) -> lower K (closer to 0.25)
	// High delay (slow network) -> higher K (closer to 0.50)
	targetK := dk.config.MinK + delayNormalized*(dk.config.MaxK-dk.config.MinK)

	// Step 3: Adjust for network stability
	// Unstable network -> more conservative (higher K)
	stabilityAdjustment := (1.0 - state.StabilityScore) * 0.1
	targetK += stabilityAdjustment

	// Step 4: Smooth transition (don't change K too rapidly)
	adaptRate := dk.config.KAdaptationRate
	if math.Abs(targetK-dk.currentK) < adaptRate {
		dk.currentK = targetK
	} else if targetK > dk.currentK {
		dk.currentK += adaptRate
	} else {
		dk.currentK -= adaptRate
	}

	// Clamp to bounds
	dk.currentK = math.Max(dk.config.MinK, math.Min(dk.config.MaxK, dk.currentK))

	dk.lastKAdapt = time.Now()

	return dk.currentK
}

// AdaptBlockTime computes optimal block time based on network conditions.
func (dk *Engine) AdaptBlockTime() time.Duration {
	dk.mu.Lock()
	defer dk.mu.Unlock()

	state := dk.latencyMonitor.GetNetworkState()
	if state == nil {
		return dk.config.TargetBlockTime
	}

	// Block time should be > network diameter to ensure propagation
	// but not too long to maximize throughput

	minSafeBlockTime := time.Duration(state.NetworkDiameter*1.5) * time.Millisecond

	// Ensure minimum floor
	if minSafeBlockTime < dk.config.MinBlockTime {
		minSafeBlockTime = dk.config.MinBlockTime
	}

	// Target slightly above minimum for safety margin
	targetBlockTime := time.Duration(float64(minSafeBlockTime) * 1.2)

	// Clamp to bounds
	if targetBlockTime < dk.config.MinBlockTime {
		targetBlockTime = dk.config.MinBlockTime
	}
	if targetBlockTime > dk.config.MaxBlockTime {
		targetBlockTime = dk.config.MaxBlockTime
	}

	// Smooth transition
	currentMs := float64(dk.currentBlockTime.Milliseconds())
	targetMs := float64(targetBlockTime.Milliseconds())
	adaptRate := dk.config.BlockTimeAdaptRate

	if math.Abs(targetMs-currentMs) < adaptRate*currentMs {
		dk.currentBlockTime = targetBlockTime
	} else if targetMs > currentMs {
		dk.currentBlockTime = time.Duration(currentMs*(1+adaptRate)) * time.Millisecond
	} else {
		dk.currentBlockTime = time.Duration(currentMs*(1-adaptRate)) * time.Millisecond
	}

	dk.lastBlockTimeAdapt = time.Now()

	return dk.currentBlockTime
}

// GetCurrentK returns current adaptive K value.
func (dk *Engine) GetCurrentK() float64 {
	dk.mu.RLock()
	defer dk.mu.RUnlock()
	return dk.currentK
}

// GetCurrentBlockTime returns current adaptive block time.
func (dk *Engine) GetCurrentBlockTime() time.Duration {
	dk.mu.RLock()
	defer dk.mu.RUnlock()
	return dk.currentBlockTime
}

// SetK manually sets the K parameter (for testing or override).
func (dk *Engine) SetK(k float64) {
	dk.mu.Lock()
	defer dk.mu.Unlock()
	dk.currentK = math.Max(dk.config.MinK, math.Min(dk.config.MaxK, k))
}

// SetBlockTime manually sets the block time (for testing or override).
func (dk *Engine) SetBlockTime(d time.Duration) {
	dk.mu.Lock()
	defer dk.mu.Unlock()
	if d < dk.config.MinBlockTime {
		d = dk.config.MinBlockTime
	}
	if d > dk.config.MaxBlockTime {
		d = dk.config.MaxBlockTime
	}
	dk.currentBlockTime = d
}

// GetLatencyMonitor returns the latency monitor for external updates.
func (dk *Engine) GetLatencyMonitor() *LatencyMonitor {
	return dk.latencyMonitor
}

// GetConfig returns the configuration.
func (dk *Engine) GetConfig() *Config {
	return dk.config
}

// AdaptiveState contains the current state of adaptive parameters.
type AdaptiveState struct {
	K                 float64
	BlockTime         time.Duration
	NetworkState      *NetworkState
	LastKAdapt        time.Time
	LastBlockTimeAdapt time.Time
}

// GetAdaptiveState returns the complete adaptive state.
func (dk *Engine) GetAdaptiveState() *AdaptiveState {
	dk.mu.RLock()
	defer dk.mu.RUnlock()

	return &AdaptiveState{
		K:                  dk.currentK,
		BlockTime:          dk.currentBlockTime,
		NetworkState:       dk.latencyMonitor.GetNetworkState(),
		LastKAdapt:         dk.lastKAdapt,
		LastBlockTimeAdapt: dk.lastBlockTimeAdapt,
	}
}

// ComputeKForLatency computes what K would be for a given latency.
// Useful for predictions and simulations.
func (dk *Engine) ComputeKForLatency(latencyMs float64) float64 {
	// Same formula as AdaptK but without state changes
	delayNormalized := math.Min(latencyMs/2000.0, 1.0)
	k := dk.config.MinK + delayNormalized*(dk.config.MaxK-dk.config.MinK)
	return math.Max(dk.config.MinK, math.Min(dk.config.MaxK, k))
}

// ComputeBlockTimeForLatency computes block time for a given network diameter.
func (dk *Engine) ComputeBlockTimeForLatency(diameterMs float64) time.Duration {
	minSafe := time.Duration(diameterMs*1.5) * time.Millisecond
	if minSafe < dk.config.MinBlockTime {
		minSafe = dk.config.MinBlockTime
	}

	target := time.Duration(float64(minSafe) * 1.2)
	if target > dk.config.MaxBlockTime {
		target = dk.config.MaxBlockTime
	}

	return target
}

// ShouldAdaptK returns true if enough time has passed since last K adaptation.
func (dk *Engine) ShouldAdaptK() bool {
	dk.mu.RLock()
	defer dk.mu.RUnlock()

	// Adapt at most once per block time
	return time.Since(dk.lastKAdapt) >= dk.currentBlockTime
}

// ShouldAdaptBlockTime returns true if enough time has passed since last block time adaptation.
func (dk *Engine) ShouldAdaptBlockTime() bool {
	dk.mu.RLock()
	defer dk.mu.RUnlock()

	// Adapt at most once every 10 block times
	return time.Since(dk.lastBlockTimeAdapt) >= dk.currentBlockTime*10
}
