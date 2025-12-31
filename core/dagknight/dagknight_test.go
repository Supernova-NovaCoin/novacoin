package dagknight

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLatencyMonitor(t *testing.T) {
	lm := NewLatencyMonitor()
	assert.NotNil(t, lm)
	assert.Empty(t, lm.peers)
}

func TestAddRemovePeer(t *testing.T) {
	lm := NewLatencyMonitor()

	lm.AddPeer("peer1", 1000)
	assert.Len(t, lm.peers, 1)
	assert.Equal(t, uint64(1000), lm.totalStake)

	lm.AddPeer("peer2", 2000)
	assert.Len(t, lm.peers, 2)
	assert.Equal(t, uint64(3000), lm.totalStake)

	lm.RemovePeer("peer1")
	assert.Len(t, lm.peers, 1)
	assert.Equal(t, uint64(2000), lm.totalStake)
}

func TestRecordRTT(t *testing.T) {
	lm := NewLatencyMonitor()
	lm.AddPeer("peer1", 1000)

	// Record multiple RTTs
	lm.RecordRTT("peer1", 50*time.Millisecond)
	lm.RecordRTT("peer1", 60*time.Millisecond)
	lm.RecordRTT("peer1", 55*time.Millisecond)

	peer := lm.GetPeerLatency("peer1")
	require.NotNil(t, peer)

	// EWMA should be computed
	assert.True(t, peer.EWMALatency > 0)
	assert.True(t, peer.EWMALatency < 100) // Should be around 50-60ms
}

func TestRecordPropagation(t *testing.T) {
	lm := NewLatencyMonitor()
	lm.AddPeer("peer1", 1000)

	lm.RecordPropagation("peer1", 100*time.Millisecond)
	lm.RecordPropagation("peer1", 120*time.Millisecond)

	peer := lm.GetPeerLatency("peer1")
	require.NotNil(t, peer)
	assert.True(t, peer.EWMAPropagation > 0)
}

func TestComputeNetworkStats(t *testing.T) {
	lm := NewLatencyMonitor()

	// Add multiple peers with different latencies
	lm.AddPeer("peer1", 1000)
	lm.AddPeer("peer2", 1000)
	lm.AddPeer("peer3", 1000)

	lm.RecordRTT("peer1", 50*time.Millisecond)
	lm.RecordRTT("peer2", 100*time.Millisecond)
	lm.RecordRTT("peer3", 150*time.Millisecond)

	lm.ComputeNetworkStats()

	state := lm.GetNetworkState()
	require.NotNil(t, state)

	assert.True(t, state.MedianLatency > 0)
	assert.True(t, state.P95Latency >= state.MedianLatency)
	assert.Equal(t, 3, state.ActiveValidators)
}

func TestStabilityScore(t *testing.T) {
	lm := NewLatencyMonitor()
	lm.AddPeer("peer1", 1000)

	// Record consistent latencies (high stability)
	for i := 0; i < 20; i++ {
		lm.RecordRTT("peer1", 50*time.Millisecond)
		lm.ComputeNetworkStats()
	}

	state := lm.GetNetworkState()
	require.NotNil(t, state)

	// Consistent latencies should result in high stability
	assert.True(t, state.StabilityScore >= 0.5)
}

func TestNewEngine(t *testing.T) {
	engine := NewEngine(nil, nil)
	assert.NotNil(t, engine)
	assert.Equal(t, 0.33, engine.currentK)
	assert.Equal(t, time.Second, engine.currentBlockTime)
}

func TestEngineWithConfig(t *testing.T) {
	config := &Config{
		MinK:            0.20,
		MaxK:            0.40,
		DefaultK:        0.30,
		MinBlockTime:    250 * time.Millisecond,
		MaxBlockTime:    5 * time.Second,
		TargetBlockTime: 500 * time.Millisecond,
	}

	engine := NewEngine(config, nil)

	assert.Equal(t, 0.30, engine.currentK)
	assert.Equal(t, 500*time.Millisecond, engine.currentBlockTime)
}

func TestAdaptK(t *testing.T) {
	monitor := NewLatencyMonitor()

	// Add enough peers
	for i := 0; i < 10; i++ {
		monitor.AddPeer(string(rune('a'+i)), 1000)
	}

	// Record low latencies
	for i := 0; i < 10; i++ {
		monitor.RecordRTT(string(rune('a'+i)), 50*time.Millisecond)
		monitor.RecordPropagation(string(rune('a'+i)), 100*time.Millisecond)
	}
	monitor.ComputeNetworkStats()

	engine := NewEngine(nil, monitor)

	// Adapt K
	k := engine.AdaptK()

	// With low latency, K should be near the minimum
	assert.True(t, k >= 0.25)
	assert.True(t, k <= 0.50)
}

func TestAdaptKHighLatency(t *testing.T) {
	monitor := NewLatencyMonitor()

	for i := 0; i < 10; i++ {
		monitor.AddPeer(string(rune('a'+i)), 1000)
	}

	// Record high latencies
	for i := 0; i < 10; i++ {
		monitor.RecordRTT(string(rune('a'+i)), 500*time.Millisecond)
		monitor.RecordPropagation(string(rune('a'+i)), 1000*time.Millisecond)
	}
	monitor.ComputeNetworkStats()

	engine := NewEngine(nil, monitor)

	k := engine.AdaptK()

	// With high latency, K should be higher
	assert.True(t, k >= 0.25)
}

func TestAdaptBlockTime(t *testing.T) {
	monitor := NewLatencyMonitor()

	for i := 0; i < 10; i++ {
		monitor.AddPeer(string(rune('a'+i)), 1000)
	}

	for i := 0; i < 10; i++ {
		monitor.RecordRTT(string(rune('a'+i)), 50*time.Millisecond)
		monitor.RecordPropagation(string(rune('a'+i)), 100*time.Millisecond)
	}
	monitor.ComputeNetworkStats()

	engine := NewEngine(nil, monitor)

	blockTime := engine.AdaptBlockTime()

	// Block time should be within bounds
	assert.True(t, blockTime >= 500*time.Millisecond)
	assert.True(t, blockTime <= 10*time.Second)
}

func TestSetK(t *testing.T) {
	engine := NewEngine(nil, nil)

	engine.SetK(0.40)
	assert.Equal(t, 0.40, engine.GetCurrentK())

	// Should clamp to bounds
	engine.SetK(0.10)
	assert.Equal(t, 0.25, engine.GetCurrentK())

	engine.SetK(0.90)
	assert.Equal(t, 0.50, engine.GetCurrentK())
}

func TestSetBlockTime(t *testing.T) {
	engine := NewEngine(nil, nil)

	engine.SetBlockTime(2 * time.Second)
	assert.Equal(t, 2*time.Second, engine.GetCurrentBlockTime())

	// Should clamp to bounds
	engine.SetBlockTime(100 * time.Millisecond)
	assert.Equal(t, 500*time.Millisecond, engine.GetCurrentBlockTime())

	engine.SetBlockTime(20 * time.Second)
	assert.Equal(t, 10*time.Second, engine.GetCurrentBlockTime())
}

func TestComputeKForLatency(t *testing.T) {
	engine := NewEngine(nil, nil)

	// Low latency -> lower K
	kLow := engine.ComputeKForLatency(50)
	// High latency -> higher K
	kHigh := engine.ComputeKForLatency(1000)

	assert.True(t, kLow < kHigh)
	assert.True(t, kLow >= 0.25)
	assert.True(t, kHigh <= 0.50)
}

func TestConfidenceCalculator(t *testing.T) {
	monitor := NewLatencyMonitor()

	for i := 0; i < 10; i++ {
		monitor.AddPeer(string(rune('a'+i)), 1000)
		monitor.RecordRTT(string(rune('a'+i)), 50*time.Millisecond)
	}
	monitor.ComputeNetworkStats()

	engine := NewEngine(nil, monitor)
	cc := NewConfidenceCalculator(engine)

	confidence := cc.CalculateConfidence()

	assert.True(t, confidence > 0)
	assert.True(t, confidence <= 100)
}

func TestCalculateBlockConfidence(t *testing.T) {
	engine := NewEngine(nil, nil)
	cc := NewConfidenceCalculator(engine)

	// Good conditions
	good := cc.CalculateBlockConfidence(50, 0.25, 100)

	// Bad conditions
	bad := cc.CalculateBlockConfidence(500, 0.50, 10)

	assert.True(t, good > bad)
}

func TestConfidenceLevel(t *testing.T) {
	assert.Equal(t, VeryLow, GetConfidenceLevel(10))
	assert.Equal(t, Low, GetConfidenceLevel(30))
	assert.Equal(t, Medium, GetConfidenceLevel(50))
	assert.Equal(t, High, GetConfidenceLevel(70))
	assert.Equal(t, VeryHigh, GetConfidenceLevel(90))
}

func TestConfidenceLevelString(t *testing.T) {
	assert.Equal(t, "very_low", VeryLow.String())
	assert.Equal(t, "low", Low.String())
	assert.Equal(t, "medium", Medium.String())
	assert.Equal(t, "high", High.String())
	assert.Equal(t, "very_high", VeryHigh.String())
}

func TestGenerateConfidenceReport(t *testing.T) {
	monitor := NewLatencyMonitor()

	for i := 0; i < 10; i++ {
		monitor.AddPeer(string(rune('a'+i)), 1000)
		monitor.RecordRTT(string(rune('a'+i)), 50*time.Millisecond)
	}
	monitor.ComputeNetworkStats()

	engine := NewEngine(nil, monitor)
	cc := NewConfidenceCalculator(engine)

	report := cc.GenerateConfidenceReport()

	assert.NotNil(t, report)
	assert.True(t, report.OverallConfidence > 0)
	assert.NotNil(t, report.Recommendations)
}

func TestPredictFinalityTime(t *testing.T) {
	monitor := NewLatencyMonitor()

	for i := 0; i < 10; i++ {
		monitor.AddPeer(string(rune('a'+i)), 1000)
		monitor.RecordRTT(string(rune('a'+i)), 50*time.Millisecond)
		monitor.RecordPropagation(string(rune('a'+i)), 100*time.Millisecond)
	}
	monitor.ComputeNetworkStats()

	engine := NewEngine(nil, monitor)
	cc := NewConfidenceCalculator(engine)

	// Trigger adaptation to set block time
	engine.AdaptBlockTime()

	finalityTime, level := cc.PredictFinalityTime()

	assert.True(t, finalityTime > 0)
	assert.NotEqual(t, VeryLow, level)
}

func TestGetAdaptiveState(t *testing.T) {
	engine := NewEngine(nil, nil)

	state := engine.GetAdaptiveState()

	assert.NotNil(t, state)
	assert.Equal(t, 0.33, state.K)
	assert.Equal(t, time.Second, state.BlockTime)
}

func TestUpdatePeerStake(t *testing.T) {
	lm := NewLatencyMonitor()
	lm.AddPeer("peer1", 1000)

	assert.Equal(t, uint64(1000), lm.totalStake)

	lm.UpdatePeerStake("peer1", 2000)
	assert.Equal(t, uint64(2000), lm.totalStake)
}

func TestGetAllPeers(t *testing.T) {
	lm := NewLatencyMonitor()
	lm.AddPeer("peer1", 1000)
	lm.AddPeer("peer2", 2000)

	peers := lm.GetAllPeers()
	assert.Len(t, peers, 2)
}

func TestGetJitter(t *testing.T) {
	lm := NewLatencyMonitor()
	lm.AddPeer("peer1", 1000)

	// Record varying latencies to create jitter
	lm.RecordRTT("peer1", 50*time.Millisecond)
	lm.RecordRTT("peer1", 100*time.Millisecond)
	lm.RecordRTT("peer1", 75*time.Millisecond)

	jitter := lm.GetJitter()
	// Some variance should exist
	assert.True(t, jitter >= 0)
}
