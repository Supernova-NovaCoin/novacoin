// Package dagknight implements the DAGKnight parameterless consensus protocol.
// It adapts the K parameter based on observed network latency.
package dagknight

import (
	"math"
	"sort"
	"sync"
	"time"
)

const (
	// EWMAAlpha is the smoothing factor for EWMA calculations.
	EWMAAlpha = 0.2

	// LatencySampleSize is the number of samples to keep per peer.
	LatencySampleSize = 100

	// PingInterval is how often to ping peers.
	PingInterval = 10 * time.Second

	// PropagationTimeout is the max time to wait for block propagation.
	PropagationTimeout = 5 * time.Second
)

// PeerLatency tracks latency metrics for a single peer.
type PeerLatency struct {
	NodeID string
	Stake  uint64 // Weight measurements by stake

	// RTT Measurements
	RTTSamples      []float64 // Recent RTT samples (ms)
	EWMALatency     float64   // Exponentially weighted moving average
	Variance        float64   // Latency variance for jitter

	// Block Propagation
	PropagationTimes []float64 // Block propagation delays (ms)
	EWMAPropagation  float64   // EWMA of propagation time

	// Statistics
	LastPingAt  time.Time
	LastPongAt  time.Time
	PacketLoss  float64 // 0.0 - 1.0
	PingsSent   uint64
	PongsReceived uint64

	mu sync.RWMutex
}

// NetworkState represents current network conditions.
type NetworkState struct {
	MedianLatency    float64
	P75Latency       float64
	P95Latency       float64
	NetworkDiameter  float64
	StabilityScore   float64
	TotalStake       uint64
	ActiveValidators int
}

// weightedLatency is used for stake-weighted percentile calculations.
type weightedLatency struct {
	latency float64
	stake   uint64
}

// LatencyMonitor aggregates network-wide latency metrics.
type LatencyMonitor struct {
	peers map[string]*PeerLatency

	// Network-wide statistics (stake-weighted)
	MedianLatency   float64
	P75Latency      float64
	P95Latency      float64
	NetworkDiameter float64

	// Historical tracking for stability
	LatencyHistory []float64
	StabilityScore float64

	totalStake uint64
	mu         sync.RWMutex
}

// NewLatencyMonitor creates a new latency monitor.
func NewLatencyMonitor() *LatencyMonitor {
	return &LatencyMonitor{
		peers:          make(map[string]*PeerLatency),
		LatencyHistory: make([]float64, 0, 1000),
	}
}

// AddPeer registers a new peer for monitoring.
func (lm *LatencyMonitor) AddPeer(nodeID string, stake uint64) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.peers[nodeID] = &PeerLatency{
		NodeID:           nodeID,
		Stake:            stake,
		RTTSamples:       make([]float64, 0, LatencySampleSize),
		PropagationTimes: make([]float64, 0, LatencySampleSize),
	}
	lm.totalStake += stake
}

// RemovePeer removes a peer from monitoring.
func (lm *LatencyMonitor) RemovePeer(nodeID string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if peer, exists := lm.peers[nodeID]; exists {
		lm.totalStake -= peer.Stake
		delete(lm.peers, nodeID)
	}
}

// UpdatePeerStake updates a peer's stake.
func (lm *LatencyMonitor) UpdatePeerStake(nodeID string, stake uint64) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if peer, exists := lm.peers[nodeID]; exists {
		lm.totalStake = lm.totalStake - peer.Stake + stake
		peer.Stake = stake
	}
}

// RecordRTT records a round-trip time measurement.
func (lm *LatencyMonitor) RecordRTT(nodeID string, rtt time.Duration) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	peer, exists := lm.peers[nodeID]
	if !exists {
		return
	}

	rttMs := float64(rtt.Milliseconds())

	peer.mu.Lock()
	defer peer.mu.Unlock()

	// Update EWMA
	if peer.EWMALatency == 0 {
		peer.EWMALatency = rttMs
	} else {
		peer.EWMALatency = EWMAAlpha*rttMs + (1-EWMAAlpha)*peer.EWMALatency
	}

	// Update variance (for jitter detection)
	delta := rttMs - peer.EWMALatency
	peer.Variance = EWMAAlpha*delta*delta + (1-EWMAAlpha)*peer.Variance

	// Store sample
	if len(peer.RTTSamples) >= LatencySampleSize {
		peer.RTTSamples = peer.RTTSamples[1:]
	}
	peer.RTTSamples = append(peer.RTTSamples, rttMs)
	peer.LastPongAt = time.Now()
	peer.PongsReceived++
}

// RecordPingSent records when a ping was sent.
func (lm *LatencyMonitor) RecordPingSent(nodeID string) {
	lm.mu.RLock()
	peer, exists := lm.peers[nodeID]
	lm.mu.RUnlock()

	if !exists {
		return
	}

	peer.mu.Lock()
	peer.LastPingAt = time.Now()
	peer.PingsSent++
	peer.mu.Unlock()
}

// RecordPropagation records block propagation time.
func (lm *LatencyMonitor) RecordPropagation(nodeID string, delay time.Duration) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	peer, exists := lm.peers[nodeID]
	if !exists {
		return
	}

	delayMs := float64(delay.Milliseconds())

	peer.mu.Lock()
	defer peer.mu.Unlock()

	if peer.EWMAPropagation == 0 {
		peer.EWMAPropagation = delayMs
	} else {
		peer.EWMAPropagation = EWMAAlpha*delayMs + (1-EWMAAlpha)*peer.EWMAPropagation
	}

	if len(peer.PropagationTimes) >= LatencySampleSize {
		peer.PropagationTimes = peer.PropagationTimes[1:]
	}
	peer.PropagationTimes = append(peer.PropagationTimes, delayMs)
}

// ComputeNetworkStats recalculates stake-weighted network statistics.
func (lm *LatencyMonitor) ComputeNetworkStats() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if len(lm.peers) == 0 {
		return
	}

	// Collect stake-weighted latencies
	var latencies []weightedLatency
	var totalStake uint64
	var maxPropagation float64

	for _, peer := range lm.peers {
		peer.mu.RLock()
		if peer.EWMALatency > 0 && peer.Stake > 0 {
			latencies = append(latencies, weightedLatency{
				latency: peer.EWMALatency,
				stake:   peer.Stake,
			})
			totalStake += peer.Stake

			if peer.EWMAPropagation > maxPropagation {
				maxPropagation = peer.EWMAPropagation
			}
		}
		peer.mu.RUnlock()
	}

	if len(latencies) == 0 || totalStake == 0 {
		return
	}

	// Sort by latency for percentile calculation
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i].latency < latencies[j].latency
	})

	// Compute stake-weighted percentiles
	lm.MedianLatency = lm.computeWeightedPercentile(latencies, totalStake, 0.50)
	lm.P75Latency = lm.computeWeightedPercentile(latencies, totalStake, 0.75)
	lm.P95Latency = lm.computeWeightedPercentile(latencies, totalStake, 0.95)
	lm.NetworkDiameter = maxPropagation * 2 // Round trip estimate
	lm.totalStake = totalStake

	// Track stability
	lm.LatencyHistory = append(lm.LatencyHistory, lm.MedianLatency)
	if len(lm.LatencyHistory) > 1000 {
		lm.LatencyHistory = lm.LatencyHistory[1:]
	}
	lm.StabilityScore = lm.computeStability()
}

func (lm *LatencyMonitor) computeWeightedPercentile(
	latencies []weightedLatency,
	totalStake uint64,
	percentile float64,
) float64 {
	targetStake := uint64(float64(totalStake) * percentile)
	var cumStake uint64

	for _, wl := range latencies {
		cumStake += wl.stake
		if cumStake >= targetStake {
			return wl.latency
		}
	}
	return latencies[len(latencies)-1].latency
}

func (lm *LatencyMonitor) computeStability() float64 {
	if len(lm.LatencyHistory) < 10 {
		return 0.5 // Unknown stability
	}

	// Compute coefficient of variation over recent history
	recent := lm.LatencyHistory
	if len(recent) > 100 {
		recent = lm.LatencyHistory[len(lm.LatencyHistory)-100:]
	}

	var sum, sumSq float64
	for _, v := range recent {
		sum += v
		sumSq += v * v
	}
	mean := sum / float64(len(recent))
	if mean == 0 {
		return 1.0
	}

	variance := sumSq/float64(len(recent)) - mean*mean
	if variance < 0 {
		variance = 0
	}
	stddev := math.Sqrt(variance)

	cv := stddev / mean // Coefficient of variation

	// Map CV to stability score (lower CV = higher stability)
	// CV < 0.1 -> stability near 1.0
	// CV > 0.5 -> stability near 0.0
	stability := 1.0 - math.Min(cv*2, 1.0)
	return stability
}

// GetNetworkState returns current network conditions for DAGKnight.
func (lm *LatencyMonitor) GetNetworkState() *NetworkState {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	return &NetworkState{
		MedianLatency:    lm.MedianLatency,
		P75Latency:       lm.P75Latency,
		P95Latency:       lm.P95Latency,
		NetworkDiameter:  lm.NetworkDiameter,
		StabilityScore:   lm.StabilityScore,
		TotalStake:       lm.totalStake,
		ActiveValidators: len(lm.peers),
	}
}

// GetPeerLatency returns latency info for a specific peer.
func (lm *LatencyMonitor) GetPeerLatency(nodeID string) *PeerLatency {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	peer, exists := lm.peers[nodeID]
	if !exists {
		return nil
	}

	// Return a copy
	peer.mu.RLock()
	defer peer.mu.RUnlock()

	return &PeerLatency{
		NodeID:          peer.NodeID,
		Stake:           peer.Stake,
		EWMALatency:     peer.EWMALatency,
		Variance:        peer.Variance,
		EWMAPropagation: peer.EWMAPropagation,
		LastPingAt:      peer.LastPingAt,
		LastPongAt:      peer.LastPongAt,
		PacketLoss:      peer.PacketLoss,
	}
}

// GetAllPeers returns all monitored peers.
func (lm *LatencyMonitor) GetAllPeers() []*PeerLatency {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	peers := make([]*PeerLatency, 0, len(lm.peers))
	for _, peer := range lm.peers {
		peer.mu.RLock()
		peers = append(peers, &PeerLatency{
			NodeID:          peer.NodeID,
			Stake:           peer.Stake,
			EWMALatency:     peer.EWMALatency,
			Variance:        peer.Variance,
			EWMAPropagation: peer.EWMAPropagation,
		})
		peer.mu.RUnlock()
	}

	return peers
}

// GetJitter returns the average jitter (variance) across all peers.
func (lm *LatencyMonitor) GetJitter() float64 {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	if len(lm.peers) == 0 {
		return 0
	}

	var totalVariance float64
	var count int

	for _, peer := range lm.peers {
		peer.mu.RLock()
		if peer.Variance > 0 {
			totalVariance += peer.Variance
			count++
		}
		peer.mu.RUnlock()
	}

	if count == 0 {
		return 0
	}

	return math.Sqrt(totalVariance / float64(count))
}
