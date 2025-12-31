// Package p2p implements the peer-to-peer networking layer for NovaCoin.
package p2p

import (
	"math"
	"sort"
	"sync"
	"time"
)

// LatencyMonitorConfig holds latency monitor configuration.
type LatencyMonitorConfig struct {
	PingInterval     time.Duration // How often to ping peers
	PingTimeout      time.Duration // Timeout for ping response
	SampleWindow     int           // Number of samples to keep per peer
	OutlierThreshold float64       // Z-score threshold for outliers
	NetworkSampleSize int          // Peers to sample for network estimate
}

// DefaultLatencyMonitorConfig returns default configuration.
func DefaultLatencyMonitorConfig() *LatencyMonitorConfig {
	return &LatencyMonitorConfig{
		PingInterval:      5 * time.Second,
		PingTimeout:       2 * time.Second,
		SampleWindow:      100,
		OutlierThreshold:  2.5,
		NetworkSampleSize: 20,
	}
}

// LatencySample represents a single latency measurement.
type LatencySample struct {
	RTT       time.Duration
	Timestamp time.Time
}

// PeerLatencyStats holds latency statistics for a peer.
type PeerLatencyStats struct {
	Samples      []LatencySample
	Mean         float64 // Milliseconds
	StdDev       float64
	Min          float64
	Max          float64
	Percentile50 float64
	Percentile95 float64
	Percentile99 float64
	LastUpdated  time.Time
	PendingPing  *time.Time // Time when ping was sent (nil if none pending)
	PingNonce    uint64
	Timeouts     int
}

// NetworkLatencyStats holds network-wide latency statistics.
type NetworkLatencyStats struct {
	MedianLatencyMs  float64
	MeanLatencyMs    float64
	StdDevMs         float64
	MinLatencyMs     float64
	MaxLatencyMs     float64
	Percentile95Ms   float64
	ActivePeers      int
	HealthyPeers     int // Peers with <500ms latency
	Timestamp        time.Time

	// For DAGKnight integration
	EstimatedPropagationMs float64 // Estimated message propagation time
	NetworkConfidence      uint8   // 0-100 confidence in estimates
}

// LatencyMonitor monitors peer and network latency.
type LatencyMonitor struct {
	config    *LatencyMonitorConfig
	peerStore *PeerStore

	// Per-peer stats
	peerStats map[PeerID]*PeerLatencyStats

	// Network-wide stats
	networkStats NetworkLatencyStats

	// Callbacks
	sendPing func(PeerID, uint64) error

	// For DAGKnight
	onLatencyUpdate func(NetworkLatencyStats)

	stopCh chan struct{}
	mu     sync.RWMutex
}

// NewLatencyMonitor creates a new latency monitor.
func NewLatencyMonitor(config *LatencyMonitorConfig, peerStore *PeerStore) *LatencyMonitor {
	if config == nil {
		config = DefaultLatencyMonitorConfig()
	}

	return &LatencyMonitor{
		config:    config,
		peerStore: peerStore,
		peerStats: make(map[PeerID]*PeerLatencyStats),
		stopCh:    make(chan struct{}),
	}
}

// SetSendPingFunc sets the function to send pings.
func (lm *LatencyMonitor) SetSendPingFunc(fn func(PeerID, uint64) error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.sendPing = fn
}

// SetLatencyUpdateCallback sets the callback for latency updates.
func (lm *LatencyMonitor) SetLatencyUpdateCallback(fn func(NetworkLatencyStats)) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.onLatencyUpdate = fn
}

// Start starts the latency monitor.
func (lm *LatencyMonitor) Start() {
	go lm.monitorLoop()
}

// Stop stops the latency monitor.
func (lm *LatencyMonitor) Stop() {
	close(lm.stopCh)
}

func (lm *LatencyMonitor) monitorLoop() {
	ticker := time.NewTicker(lm.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lm.pingAllPeers()
			lm.checkTimeouts()
			lm.updateNetworkStats()
		case <-lm.stopCh:
			return
		}
	}
}

func (lm *LatencyMonitor) pingAllPeers() {
	lm.mu.Lock()
	sendPing := lm.sendPing
	lm.mu.Unlock()

	if sendPing == nil {
		return
	}

	peers := lm.peerStore.GetActivePeers()
	for _, peer := range peers {
		lm.mu.Lock()
		stats, exists := lm.peerStats[peer.ID]
		if !exists {
			stats = &PeerLatencyStats{
				Samples: make([]LatencySample, 0, lm.config.SampleWindow),
			}
			lm.peerStats[peer.ID] = stats
		}

		// Don't ping if one is already pending
		if stats.PendingPing != nil {
			lm.mu.Unlock()
			continue
		}

		nonce := uint64(time.Now().UnixNano())
		now := time.Now()
		stats.PendingPing = &now
		stats.PingNonce = nonce
		lm.mu.Unlock()

		sendPing(peer.ID, nonce)
	}
}

func (lm *LatencyMonitor) checkTimeouts() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now()
	for id, stats := range lm.peerStats {
		if stats.PendingPing != nil {
			if now.Sub(*stats.PendingPing) > lm.config.PingTimeout {
				stats.PendingPing = nil
				stats.Timeouts++

				// Penalize peer for timeout
				if peer := lm.peerStore.GetPeer(id); peer != nil {
					peer.AdjustScore(-10)
				}
			}
		}
	}
}

// RecordPong records a pong response.
func (lm *LatencyMonitor) RecordPong(peerID PeerID, nonce uint64, sentTime, recvTime int64) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	stats, exists := lm.peerStats[peerID]
	if !exists {
		return
	}

	// Verify nonce matches
	if stats.PingNonce != nonce || stats.PendingPing == nil {
		return
	}

	// Calculate RTT
	rtt := time.Since(*stats.PendingPing)
	stats.PendingPing = nil

	// Add sample
	sample := LatencySample{
		RTT:       rtt,
		Timestamp: time.Now(),
	}
	stats.Samples = append(stats.Samples, sample)

	// Keep only recent samples
	if len(stats.Samples) > lm.config.SampleWindow {
		stats.Samples = stats.Samples[1:]
	}

	// Update statistics
	lm.updatePeerStats(stats)
	stats.LastUpdated = time.Now()

	// Update peer's latency
	if peer := lm.peerStore.GetPeer(peerID); peer != nil {
		peer.UpdateLatency(uint32(rtt.Milliseconds()))
	}
}

func (lm *LatencyMonitor) updatePeerStats(stats *PeerLatencyStats) {
	if len(stats.Samples) == 0 {
		return
	}

	// Extract RTT values in milliseconds
	values := make([]float64, len(stats.Samples))
	for i, s := range stats.Samples {
		values[i] = float64(s.RTT.Milliseconds())
	}

	// Calculate mean
	var sum float64
	for _, v := range values {
		sum += v
	}
	stats.Mean = sum / float64(len(values))

	// Calculate standard deviation
	var sqDiffSum float64
	for _, v := range values {
		diff := v - stats.Mean
		sqDiffSum += diff * diff
	}
	stats.StdDev = math.Sqrt(sqDiffSum / float64(len(values)))

	// Sort for percentiles
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	stats.Min = sorted[0]
	stats.Max = sorted[len(sorted)-1]
	stats.Percentile50 = percentile(sorted, 50)
	stats.Percentile95 = percentile(sorted, 95)
	stats.Percentile99 = percentile(sorted, 99)
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	idx := (p / 100.0) * float64(len(sorted)-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}

	weight := idx - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

func (lm *LatencyMonitor) updateNetworkStats() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	activePeers := lm.peerStore.GetActivePeers()
	if len(activePeers) == 0 {
		lm.networkStats = NetworkLatencyStats{
			Timestamp:         time.Now(),
			NetworkConfidence: 0,
		}
		return
	}

	// Collect latencies from all active peers
	var latencies []float64
	healthyCount := 0

	for _, peer := range activePeers {
		if stats, exists := lm.peerStats[peer.ID]; exists && len(stats.Samples) > 0 {
			latencies = append(latencies, stats.Mean)
			if stats.Mean < 500 {
				healthyCount++
			}
		}
	}

	if len(latencies) == 0 {
		lm.networkStats = NetworkLatencyStats{
			ActivePeers:       len(activePeers),
			Timestamp:         time.Now(),
			NetworkConfidence: 10,
		}
		return
	}

	// Sort latencies
	sort.Float64s(latencies)

	// Calculate statistics
	var sum float64
	for _, l := range latencies {
		sum += l
	}
	mean := sum / float64(len(latencies))

	var sqDiffSum float64
	for _, l := range latencies {
		diff := l - mean
		sqDiffSum += diff * diff
	}
	stdDev := math.Sqrt(sqDiffSum / float64(len(latencies)))

	// Calculate confidence based on sample size and variance
	confidence := uint8(min(100, len(latencies)*10))
	if stdDev > 200 {
		confidence = uint8(float64(confidence) * 0.7)
	}

	lm.networkStats = NetworkLatencyStats{
		MedianLatencyMs:        percentile(latencies, 50),
		MeanLatencyMs:          mean,
		StdDevMs:               stdDev,
		MinLatencyMs:           latencies[0],
		MaxLatencyMs:           latencies[len(latencies)-1],
		Percentile95Ms:         percentile(latencies, 95),
		ActivePeers:            len(activePeers),
		HealthyPeers:           healthyCount,
		Timestamp:              time.Now(),
		EstimatedPropagationMs: estimatePropagation(mean, stdDev),
		NetworkConfidence:      confidence,
	}

	// Notify callback
	if lm.onLatencyUpdate != nil {
		lm.onLatencyUpdate(lm.networkStats)
	}
}

// estimatePropagation estimates message propagation time across the network.
// This accounts for multiple hops in gossip.
func estimatePropagation(meanLatency, stdDev float64) float64 {
	// Assume ~3 hops on average for full network coverage
	// Add some buffer for processing
	hops := 3.0
	processingOverhead := 10.0 // ms per hop

	// 95th percentile propagation estimate
	p95Latency := meanLatency + 1.645*stdDev
	return hops*p95Latency + hops*processingOverhead
}

// GetNetworkStats returns the current network latency statistics.
func (lm *LatencyMonitor) GetNetworkStats() NetworkLatencyStats {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.networkStats
}

// GetPeerStats returns latency statistics for a specific peer.
func (lm *LatencyMonitor) GetPeerStats(peerID PeerID) *PeerLatencyStats {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	if stats, exists := lm.peerStats[peerID]; exists {
		// Return a copy
		statsCopy := *stats
		statsCopy.Samples = make([]LatencySample, len(stats.Samples))
		copy(statsCopy.Samples, stats.Samples)
		return &statsCopy
	}
	return nil
}

// GetObservedLatencyMs returns the current observed network latency for DAGKnight.
func (lm *LatencyMonitor) GetObservedLatencyMs() uint32 {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return uint32(lm.networkStats.MedianLatencyMs)
}

// GetNetworkConfidence returns the confidence level in network measurements.
func (lm *LatencyMonitor) GetNetworkConfidence() uint8 {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.networkStats.NetworkConfidence
}

// GetEstimatedPropagationTime returns the estimated time for a message to reach all peers.
func (lm *LatencyMonitor) GetEstimatedPropagationTime() time.Duration {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return time.Duration(lm.networkStats.EstimatedPropagationMs) * time.Millisecond
}

// Helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
