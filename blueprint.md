# NovaCoin Blueprint - Ultra-Scalable PoS-DAG Hybrid

## Executive Summary

**NovaCoin** is a next-generation Layer 1 blockchain combining four breakthrough consensus innovations:

| Component | Technology | Benefit |
|-----------|------------|---------|
| Consensus Core | PoS-DAGKnight | Parameterless, 50% Byzantine tolerance |
| Finality | Mysticeti 3-Round | 0.5s theoretical minimum latency |
| Throughput | Shoal++ Multi-Anchor | 100k+ TPS, all validators are anchors |
| Privacy/Scale | ZK-Rollup Native | Recursive proofs, private transactions |
| Fairness | Encrypted Mempool | MEV-resistant, threshold encryption |

**Target Performance:**
- Block Time: 500ms adaptive
- Finality: 1.5s (3 rounds @ 500ms)
- Throughput: 100,000+ TPS
- Byzantine Tolerance: 50% (parameterless)

---

## Architecture Overview

```
                         ┌─────────────────────────────────────────────┐
                         │           APPLICATION LAYER                 │
                         │  ┌─────────┐ ┌──────────┐ ┌─────────────┐  │
                         │  │ zkEVM   │ │  DeFi    │ │   dApps     │  │
                         │  └────┬────┘ └────┬─────┘ └──────┬──────┘  │
                         └───────┼───────────┼──────────────┼─────────┘
                                 ▼           ▼              ▼
┌────────────────────────────────────────────────────────────────────────┐
│                        ZK-ROLLUP LAYER (Optional)                      │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐    │
│  │  STARK Prover   │  │   Batch Commit  │  │  Recursive Verify   │    │
│  └────────┬────────┘  └────────┬────────┘  └──────────┬──────────┘    │
└───────────┼────────────────────┼─────────────────────┼────────────────┘
            ▼                    ▼                     ▼
┌────────────────────────────────────────────────────────────────────────┐
│                     MEV RESISTANCE LAYER                               │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐    │
│  │Threshold Encrypt│  │  Fair Ordering  │  │  Blind Permutation  │    │
│  │    (BLS DKG)    │  │  (Batch-Fair)   │  │    (BlindPerm)      │    │
│  └────────┬────────┘  └────────┬────────┘  └──────────┬──────────┘    │
└───────────┼────────────────────┼─────────────────────┼────────────────┘
            ▼                    ▼                     ▼
┌────────────────────────────────────────────────────────────────────────┐
│                   CONSENSUS ORCHESTRATOR                               │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │              HybridConsensus (consensus.go)                     │  │
│  │  Coordinates: DAGKnight → Shoal++ → Mysticeti → Finality        │  │
│  └─────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────────────────┘
            │                    │                     │
            ▼                    ▼                     ▼
┌────────────────────────────────────────────────────────────────────────┐
│                   DAGKNIGHT LAYER (Parameterless)                      │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐    │
│  │ LatencyMonitor  │  │   Adaptive K    │  │ Dynamic BlockTime   │    │
│  │ (EWMA, P95)     │  │  (0.25 - 0.50)  │  │   (500ms - 10s)     │    │
│  └────────┬────────┘  └────────┬────────┘  └──────────┬──────────┘    │
└───────────┼────────────────────┼─────────────────────┼────────────────┘
            ▼                    ▼                     ▼
┌────────────────────────────────────────────────────────────────────────┐
│                   SHOAL++ LAYER (Multi-Anchor)                         │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐    │
│  │ ParallelDAGs    │  │  AnchorManager  │  │   WaveTracker       │    │
│  │ (Staggered)     │  │ (All Validators)│  │   (Completion)      │    │
│  └────────┬────────┘  └────────┬────────┘  └──────────┬──────────┘    │
└───────────┼────────────────────┼─────────────────────┼────────────────┘
            ▼                    ▼                     ▼
┌────────────────────────────────────────────────────────────────────────┐
│                   MYSTICETI LAYER (3-Round Commit)                     │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐    │
│  │  ImplicitVote   │  │  PatternDetect  │  │   CommitTracker     │    │
│  │ (Ref = Vote)    │  │  (2f+1 depth 3) │  │   (3-Round Rule)    │    │
│  └────────┬────────┘  └────────┬────────┘  └──────────┬──────────┘    │
└───────────┼────────────────────┼─────────────────────┼────────────────┘
            ▼                    ▼                     ▼
┌────────────────────────────────────────────────────────────────────────┐
│                   PoS FOUNDATION LAYER                                 │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐    │
│  │   GHOSTDAG      │  │   Staking       │  │   Slashing          │    │
│  │(Stake-Weighted) │  │   Registry      │  │   Conditions        │    │
│  └────────┬────────┘  └────────┬────────┘  └──────────┬──────────┘    │
└───────────┼────────────────────┼─────────────────────┼────────────────┘
            ▼                    ▼                     ▼
┌────────────────────────────────────────────────────────────────────────┐
│                        P2P NETWORK LAYER                               │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐    │
│  │  PeerManager    │  │  LatencyProbe   │  │   GossipProtocol    │    │
│  │  (Discovery)    │  │  (Ping/Pong)    │  │   (Reliable Bcast)  │    │
│  └─────────────────┘  └─────────────────┘  └─────────────────────┘    │
└────────────────────────────────────────────────────────────────────────┘
```

---

## File Structure

```
novacoin/
├── cmd/
│   └── novacoin/
│       └── main.go                    # Entry point, node initialization
├── core/
│   ├── consensus/
│   │   ├── hybrid.go                  # HybridConsensus orchestrator
│   │   ├── config.go                  # Unified consensus configuration
│   │   └── metrics.go                 # Consensus performance metrics
│   ├── dagknight/
│   │   ├── engine.go                  # Adaptive K, block time engine
│   │   ├── latency.go                 # Network latency estimation
│   │   ├── confidence.go              # Confidence score calculation
│   │   └── dagknight_test.go
│   ├── shoal/
│   │   ├── anchor.go                  # Multi-anchor manager
│   │   ├── wave.go                    # Wave computation & tracking
│   │   ├── parallel_dag.go            # Staggered parallel DAGs
│   │   └── shoal_test.go
│   ├── mysticeti/
│   │   ├── commit.go                  # 3-round commit rule engine
│   │   ├── implicit_vote.go           # Reference = Vote tracking
│   │   ├── pattern.go                 # Commit pattern detection
│   │   └── mysticeti_test.go
│   ├── ghostdag/
│   │   ├── ghostdag.go                # Stake-weighted GHOSTDAG
│   │   ├── coloring.go                # Blue/red set determination
│   │   └── ordering.go                # Topological ordering
│   ├── finality/
│   │   ├── finality.go                # Multi-tier finality engine
│   │   ├── checkpoint.go              # Finalized checkpoints
│   │   └── finality_test.go
│   ├── dag/
│   │   ├── vertex.go                  # Extended vertex structure
│   │   ├── store.go                   # DAG storage with indices
│   │   └── traversal.go               # DAG traversal utilities
│   ├── pos/
│   │   ├── staking.go                 # Staking registry
│   │   ├── validator.go               # Validator management
│   │   ├── slashing.go                # Slashing conditions
│   │   └── rewards.go                 # Block reward distribution
│   ├── mev/
│   │   ├── threshold_encrypt.go       # BLS threshold encryption
│   │   ├── dkg.go                     # Distributed key generation
│   │   ├── fair_order.go              # Batch-fair ordering
│   │   ├── blind_perm.go              # Blind permutation
│   │   └── mev_test.go
│   ├── zk/
│   │   ├── prover.go                  # STARK/SNARK prover interface
│   │   ├── verifier.go                # Proof verification
│   │   ├── batch.go                   # Batch proof aggregation
│   │   └── recursive.go               # Recursive proof composition
│   ├── state/
│   │   ├── state.go                   # World state management
│   │   ├── trie.go                    # Merkle Patricia Trie
│   │   └── snapshot.go                # State snapshots
│   └── vm/
│       ├── evm.go                     # EVM compatibility layer
│       └── runtime.go                 # Transaction execution
├── p2p/
│   ├── server.go                      # P2P server
│   ├── peer.go                        # Peer connection management
│   ├── message.go                     # Protocol messages
│   ├── latency_probe.go               # Ping/pong latency measurement
│   ├── gossip.go                      # Reliable broadcast
│   └── discovery.go                   # Peer discovery
├── rpc/
│   ├── server.go                      # JSON-RPC server
│   ├── handlers.go                    # RPC handlers
│   └── types.go                       # RPC types
├── crypto/
│   ├── bls.go                         # BLS signatures
│   ├── threshold.go                   # Threshold signatures
│   └── hash.go                        # Hash functions
└── config/
    └── config.go                      # Node configuration
```

---

## Implementation Plan

### Phase 1: Core Infrastructure

#### 1.1 Extended Vertex Structure (`core/dag/vertex.go`)

```go
package dag

import (
    "time"
    "github.com/Supernova-NovaCoin/crypto"
)

// Vertex represents a block in the DAG with all consensus metadata
type Vertex struct {
    // === Core Identity ===
    Hash        [32]byte       // SHA3-256 of header
    Height      uint64         // DAG height (max parent height + 1)
    Timestamp   time.Time      // Block creation time

    // === DAG Structure ===
    Parents     [][32]byte     // Parent block hashes (multiple)
    Tips        [][32]byte     // DAG tips at creation time

    // === Proof of Stake ===
    ValidatorPubKey [48]byte   // BLS public key
    ValidatorIndex  uint32     // Index in validator set
    Stake           uint64     // Validator's stake at block time
    Signature       [96]byte   // BLS signature over header

    // === DAGKnight Adaptive ===
    ObservedLatencyMs uint32   // Network latency at creation (ms)
    NetworkConfidence uint8    // 0-100 confidence score
    AdaptiveK         float32  // K value used for this block

    // === Shoal++ Multi-Anchor ===
    Wave            uint64     // Wave number (increments every round)
    IsAnchor        bool       // true (all validators are anchors)
    AnchorWave      uint64     // Wave when anchored
    ParallelDAGId   uint8      // Which parallel DAG (0-3)

    // === Mysticeti 3-Round ===
    Round           uint64     // Consensus round number
    ImplicitVotes   [][32]byte // Blocks this vertex votes for (= Parents)
    CommitRound     uint64     // Round committed (0 = uncommitted)
    CommitDepth     uint8      // Depth when committed (1-3)

    // === GHOSTDAG Coloring ===
    BlueScore       uint64     // Cumulative blue score
    BlueWork        *big.Int   // Cumulative proof of stake work
    IsBlue          bool       // Blue set membership
    SelectedParent  [32]byte   // Main chain parent

    // === Finality ===
    FinalityScore   uint8      // 0-100 finality confidence
    FinalizedAt     time.Time  // When finalized (zero = not finalized)
    FinalityProof   []byte     // Aggregated signature proof

    // === MEV Resistance ===
    EncryptedTxRoot [32]byte   // Root of encrypted transactions
    DecryptionProof []byte     // Threshold decryption proof

    // === Transactions ===
    TxRoot          [32]byte   // Merkle root of transactions
    StateRoot       [32]byte   // State trie root after execution
    ReceiptRoot     [32]byte   // Receipts trie root
    TxCount         uint32     // Number of transactions
}

// VertexHeader contains fields for hash computation
type VertexHeader struct {
    Parents         [][32]byte
    Timestamp       int64
    ValidatorPubKey [48]byte
    TxRoot          [32]byte
    StateRoot       [32]byte
    Wave            uint64
    Round           uint64
    AdaptiveK       float32
}
```

#### 1.2 Latency Monitoring (`core/dagknight/latency.go`)

```go
package dagknight

import (
    "math"
    "sort"
    "sync"
    "time"
)

const (
    EWMAAlpha           = 0.2    // Smoothing factor for EWMA
    LatencySampleSize   = 100    // Samples per peer for statistics
    PingInterval        = 10 * time.Second
    PropagationTimeout  = 5 * time.Second
)

// PeerLatency tracks latency metrics for a single peer
type PeerLatency struct {
    NodeID            string
    Stake             uint64     // Weight measurements by stake

    // RTT Measurements
    RTTSamples        []float64  // Recent RTT samples (ms)
    EWMALatency       float64    // Exponentially weighted moving average
    Variance          float64    // Latency variance for jitter

    // Block Propagation
    PropagationTimes  []float64  // Block propagation delays (ms)
    EWMAPropagation   float64    // EWMA of propagation time

    // Statistics
    LastPingAt        time.Time
    LastPongAt        time.Time
    PacketLoss        float64    // 0.0 - 1.0

    mu sync.RWMutex
}

// LatencyMonitor aggregates network-wide latency metrics
type LatencyMonitor struct {
    peers map[string]*PeerLatency

    // Network-wide statistics (stake-weighted)
    MedianLatency     float64    // Median RTT across all peers
    P75Latency        float64    // 75th percentile
    P95Latency        float64    // 95th percentile
    NetworkDiameter   float64    // Estimated max propagation delay

    // Historical tracking for stability
    LatencyHistory    []float64  // Historical median values
    StabilityScore    float64    // 0-1, higher = more stable

    totalStake        uint64
    mu                sync.RWMutex
}

func NewLatencyMonitor() *LatencyMonitor {
    return &LatencyMonitor{
        peers:          make(map[string]*PeerLatency),
        LatencyHistory: make([]float64, 0, 1000),
    }
}

// RecordRTT records a round-trip time measurement
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
}

// RecordPropagation records block propagation time
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

// ComputeNetworkStats recalculates stake-weighted network statistics
func (lm *LatencyMonitor) ComputeNetworkStats() {
    lm.mu.Lock()
    defer lm.mu.Unlock()

    if len(lm.peers) == 0 {
        return
    }

    // Collect stake-weighted latencies
    type weightedLatency struct {
        latency float64
        stake   uint64
    }

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
    recent := lm.LatencyHistory[len(lm.LatencyHistory)-100:]
    if len(recent) < 10 {
        recent = lm.LatencyHistory
    }

    var sum, sumSq float64
    for _, v := range recent {
        sum += v
        sumSq += v * v
    }
    mean := sum / float64(len(recent))
    variance := sumSq/float64(len(recent)) - mean*mean
    stddev := math.Sqrt(variance)

    cv := stddev / mean // Coefficient of variation

    // Map CV to stability score (lower CV = higher stability)
    // CV < 0.1 -> stability near 1.0
    // CV > 0.5 -> stability near 0.0
    stability := 1.0 - math.Min(cv*2, 1.0)
    return stability
}

// GetNetworkState returns current network conditions for DAGKnight
func (lm *LatencyMonitor) GetNetworkState() *NetworkState {
    lm.mu.RLock()
    defer lm.mu.RUnlock()

    return &NetworkState{
        MedianLatency:     lm.MedianLatency,
        P95Latency:        lm.P95Latency,
        NetworkDiameter:   lm.NetworkDiameter,
        StabilityScore:    lm.StabilityScore,
        TotalStake:        lm.totalStake,
        ActiveValidators:  len(lm.peers),
    }
}
```

#### 1.3 DAGKnight Engine (`core/dagknight/engine.go`)

```go
package dagknight

import (
    "math"
    "sync"
    "time"
)

// DAGKnightConfig defines adaptive parameter bounds
type DAGKnightConfig struct {
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
    KAdaptationRate       float64 // How fast K changes (0.01-0.1)
    BlockTimeAdaptRate    float64 // How fast block time changes
}

// NetworkState represents current network conditions
type NetworkState struct {
    MedianLatency     float64
    P95Latency        float64
    NetworkDiameter   float64
    StabilityScore    float64
    TotalStake        uint64
    ActiveValidators  int
}

// DAGKnightEngine implements parameterless adaptive consensus
type DAGKnightEngine struct {
    config         *DAGKnightConfig
    latencyMonitor *LatencyMonitor

    // Current adaptive values
    currentK        float64
    currentBlockTime time.Duration

    // Cluster analysis for K selection
    kClusters      []float64
    clusterHistory []float64

    mu sync.RWMutex
}

func NewDAGKnightEngine(config *DAGKnightConfig, monitor *LatencyMonitor) *DAGKnightEngine {
    return &DAGKnightEngine{
        config:           config,
        latencyMonitor:   monitor,
        currentK:         config.DefaultK,
        currentBlockTime: config.TargetBlockTime,
        kClusters:        make([]float64, 0),
    }
}

// AdaptK computes optimal K based on network conditions
// This is the core DAGKnight algorithm
func (dk *DAGKnightEngine) AdaptK() float64 {
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

    return dk.currentK
}

// AdaptBlockTime computes optimal block time based on network conditions
func (dk *DAGKnightEngine) AdaptBlockTime() time.Duration {
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

    return dk.currentBlockTime
}

// CalculateConfidence returns a 0-100 confidence score for current conditions
func (dk *DAGKnightEngine) CalculateConfidence() uint8 {
    state := dk.latencyMonitor.GetNetworkState()
    if state == nil {
        return 50 // Unknown
    }

    // Factors affecting confidence:
    // 1. Latency (lower = higher confidence)
    // 2. Stability (higher = higher confidence)
    // 3. Active validators (more = higher confidence)

    latencyScore := 100.0 * (1.0 - math.Min(state.MedianLatency/1000.0, 1.0))
    stabilityScore := 100.0 * state.StabilityScore

    validatorScore := math.Min(float64(state.ActiveValidators)/100.0, 1.0) * 100.0

    // Weighted average
    confidence := latencyScore*0.4 + stabilityScore*0.4 + validatorScore*0.2

    return uint8(math.Min(100, math.Max(0, confidence)))
}

// GetCurrentK returns current adaptive K value
func (dk *DAGKnightEngine) GetCurrentK() float64 {
    dk.mu.RLock()
    defer dk.mu.RUnlock()
    return dk.currentK
}

// GetCurrentBlockTime returns current adaptive block time
func (dk *DAGKnightEngine) GetCurrentBlockTime() time.Duration {
    dk.mu.RLock()
    defer dk.mu.RUnlock()
    return dk.currentBlockTime
}
```

---

### Phase 2: Shoal++ Multi-Anchor

#### 2.1 Wave Tracker (`core/shoal/wave.go`)

```go
package shoal

import (
    "sync"

    "github.com/Supernova-NovaCoin/core/dag"
)

// WaveState tracks the progress of a single wave
type WaveState struct {
    Wave           uint64
    StartTime      int64

    // Validator participation
    Validators     map[[48]byte]bool    // pubkey -> participated
    StakeCommitted uint64               // Cumulative stake committed
    TotalStake     uint64               // Total stake at wave start

    // Vertices in this wave
    Vertices       [][32]byte           // All vertex hashes in wave
    AnchorVertices [][32]byte           // Anchor vertices (all in Shoal++)

    // Completion status
    IsComplete     bool
    CompletedAt    int64
}

// WaveTracker manages wave progression and completion
type WaveTracker struct {
    waves          map[uint64]*WaveState
    currentWave    uint64
    completedWaves []uint64

    // Configuration
    quorumThreshold float64 // 2/3 stake required for completion

    mu sync.RWMutex
}

func NewWaveTracker(quorumThreshold float64) *WaveTracker {
    return &WaveTracker{
        waves:           make(map[uint64]*WaveState),
        completedWaves:  make([]uint64, 0),
        quorumThreshold: quorumThreshold,
    }
}

// ComputeWave determines wave number from vertex's parents
func (wt *WaveTracker) ComputeWave(v *dag.Vertex, getVertex func([32]byte) *dag.Vertex) uint64 {
    if len(v.Parents) == 0 {
        return 0 // Genesis
    }

    // Wave = max(parent waves) + 1 if this is a new round
    // In Shoal++, wave increments when all parallel DAGs advance

    maxParentWave := uint64(0)
    for _, parentHash := range v.Parents {
        parent := getVertex(parentHash)
        if parent != nil && parent.Wave > maxParentWave {
            maxParentWave = parent.Wave
        }
    }

    // New wave if we're starting a new round
    if v.Round > 0 && v.Round%4 == 0 { // Every 4 rounds = new wave
        return maxParentWave + 1
    }

    return maxParentWave
}

// ProcessVertex updates wave state when new vertex arrives
func (wt *WaveTracker) ProcessVertex(v *dag.Vertex, totalStake uint64) error {
    wt.mu.Lock()
    defer wt.mu.Unlock()

    wave := v.Wave

    // Initialize wave if new
    if _, exists := wt.waves[wave]; !exists {
        wt.waves[wave] = &WaveState{
            Wave:       wave,
            StartTime:  v.Timestamp.UnixMilli(),
            Validators: make(map[[48]byte]bool),
            TotalStake: totalStake,
        }

        if wave > wt.currentWave {
            wt.currentWave = wave
        }
    }

    ws := wt.waves[wave]

    // Record validator participation
    if !ws.Validators[v.ValidatorPubKey] {
        ws.Validators[v.ValidatorPubKey] = true
        ws.StakeCommitted += v.Stake
        ws.Vertices = append(ws.Vertices, v.Hash)

        // In Shoal++, every vertex is an anchor
        ws.AnchorVertices = append(ws.AnchorVertices, v.Hash)
    }

    // Check wave completion
    if !ws.IsComplete && float64(ws.StakeCommitted)/float64(ws.TotalStake) >= wt.quorumThreshold {
        ws.IsComplete = true
        ws.CompletedAt = v.Timestamp.UnixMilli()
        wt.completedWaves = append(wt.completedWaves, wave)
    }

    return nil
}

// IsWaveComplete checks if a wave has achieved quorum
func (wt *WaveTracker) IsWaveComplete(wave uint64) bool {
    wt.mu.RLock()
    defer wt.mu.RUnlock()

    ws, exists := wt.waves[wave]
    if !exists {
        return false
    }
    return ws.IsComplete
}

// GetCompletedWaves returns all completed wave numbers
func (wt *WaveTracker) GetCompletedWaves() []uint64 {
    wt.mu.RLock()
    defer wt.mu.RUnlock()

    result := make([]uint64, len(wt.completedWaves))
    copy(result, wt.completedWaves)
    return result
}
```

#### 2.2 Anchor Manager (`core/shoal/anchor.go`)

```go
package shoal

import (
    "sync"
    "time"

    "github.com/Supernova-NovaCoin/core/dag"
)

// AnchorManager implements Shoal++ multi-anchor consensus
// In Shoal++, EVERY validator is an anchor (not just leaders)
type AnchorManager struct {
    // Wave -> Validator -> Anchor vertex hash
    anchors map[uint64]map[[48]byte][32]byte

    // Track certification (validators that referenced each anchor)
    certifications map[[32]byte]*AnchorCertification

    waveTracker *WaveTracker

    mu sync.RWMutex
}

// AnchorCertification tracks which validators certified an anchor
type AnchorCertification struct {
    AnchorHash     [32]byte
    AnchorWave     uint64
    ValidatorPK    [48]byte           // Anchor creator

    CertifiedBy    map[[48]byte]bool  // Validators that referenced this
    StakeCertified uint64             // Cumulative certifying stake
    TotalStake     uint64             // Total stake for percentage

    FirstCertified time.Time
    QuorumReached  time.Time          // When 2f+1 reached
}

func NewAnchorManager(waveTracker *WaveTracker) *AnchorManager {
    return &AnchorManager{
        anchors:        make(map[uint64]map[[48]byte][32]byte),
        certifications: make(map[[32]byte]*AnchorCertification),
        waveTracker:    waveTracker,
    }
}

// ProcessAnchor records a new anchor vertex
func (am *AnchorManager) ProcessAnchor(v *dag.Vertex, totalStake uint64) error {
    am.mu.Lock()
    defer am.mu.Unlock()

    wave := v.Wave

    // Initialize wave anchors map
    if am.anchors[wave] == nil {
        am.anchors[wave] = make(map[[48]byte][32]byte)
    }

    // One anchor per validator per wave
    if _, exists := am.anchors[wave][v.ValidatorPubKey]; exists {
        return nil // Already has anchor this wave
    }

    // Record anchor
    am.anchors[wave][v.ValidatorPubKey] = v.Hash

    // Initialize certification tracking
    am.certifications[v.Hash] = &AnchorCertification{
        AnchorHash:     v.Hash,
        AnchorWave:     wave,
        ValidatorPK:    v.ValidatorPubKey,
        CertifiedBy:    make(map[[48]byte]bool),
        TotalStake:     totalStake,
        FirstCertified: time.Now(),
    }

    // Self-certify
    am.certifications[v.Hash].CertifiedBy[v.ValidatorPubKey] = true
    am.certifications[v.Hash].StakeCertified = v.Stake

    return nil
}

// ProcessCertification updates anchor certification when a vertex references it
func (am *AnchorManager) ProcessCertification(
    v *dag.Vertex,
    getVertex func([32]byte) *dag.Vertex,
) {
    am.mu.Lock()
    defer am.mu.Unlock()

    // Each parent reference acts as an implicit certification
    for _, parentHash := range v.Parents {
        cert, exists := am.certifications[parentHash]
        if !exists {
            continue
        }

        // Skip if already certified by this validator
        if cert.CertifiedBy[v.ValidatorPubKey] {
            continue
        }

        // Add certification
        cert.CertifiedBy[v.ValidatorPubKey] = true
        cert.StakeCertified += v.Stake

        // Check if quorum reached (2f+1 = 2/3 stake)
        if cert.QuorumReached.IsZero() &&
           float64(cert.StakeCertified)/float64(cert.TotalStake) >= 2.0/3.0 {
            cert.QuorumReached = time.Now()
        }
    }
}

// GetCertifiedAnchors returns anchors that achieved quorum in a wave
func (am *AnchorManager) GetCertifiedAnchors(wave uint64) [][32]byte {
    am.mu.RLock()
    defer am.mu.RUnlock()

    var result [][32]byte

    anchors, exists := am.anchors[wave]
    if !exists {
        return result
    }

    for _, hash := range anchors {
        cert := am.certifications[hash]
        if cert != nil && !cert.QuorumReached.IsZero() {
            result = append(result, hash)
        }
    }

    return result
}
```

#### 2.3 Parallel DAG Manager (`core/shoal/parallel_dag.go`)

```go
package shoal

import (
    "sync"
    "time"
)

const (
    NumParallelDAGs = 4        // Shoal++ uses 4 parallel DAGs
    StaggerOffset   = 125      // ms offset between DAGs (500ms / 4)
)

// ParallelDAGManager coordinates multiple staggered DAGs
// This reduces queuing latency from 1.5 to 0.5 message delays
type ParallelDAGManager struct {
    dags       [NumParallelDAGs]*DAGInstance
    roundRobin uint8

    mu sync.RWMutex
}

// DAGInstance represents a single parallel DAG
type DAGInstance struct {
    ID            uint8
    CurrentRound  uint64
    LastBlockTime time.Time
    Tips          [][32]byte
}

func NewParallelDAGManager() *ParallelDAGManager {
    pm := &ParallelDAGManager{}

    for i := 0; i < NumParallelDAGs; i++ {
        pm.dags[i] = &DAGInstance{
            ID:   uint8(i),
            Tips: make([][32]byte, 0),
        }
    }

    return pm
}

// SelectDAG chooses optimal DAG for new block based on timing
func (pm *ParallelDAGManager) SelectDAG() uint8 {
    pm.mu.Lock()
    defer pm.mu.Unlock()

    now := time.Now()

    // Find DAG with oldest last block (most ready for new block)
    var oldestDAG uint8
    var oldestTime time.Time

    for i, dag := range pm.dags {
        if oldestTime.IsZero() || dag.LastBlockTime.Before(oldestTime) {
            oldestTime = dag.LastBlockTime
            oldestDAG = uint8(i)
        }
    }

    // Alternatively, use stagger-based selection
    msInCycle := now.UnixMilli() % 500 // 500ms block time
    staggerDAG := uint8(msInCycle / StaggerOffset)

    // Use round-robin if DAGs are balanced
    pm.roundRobin = (pm.roundRobin + 1) % NumParallelDAGs

    return staggerDAG
}

// RecordBlock updates DAG state when block is produced
func (pm *ParallelDAGManager) RecordBlock(dagID uint8, blockHash [32]byte, round uint64) {
    pm.mu.Lock()
    defer pm.mu.Unlock()

    if dagID >= NumParallelDAGs {
        return
    }

    dag := pm.dags[dagID]
    dag.CurrentRound = round
    dag.LastBlockTime = time.Now()
    dag.Tips = append(dag.Tips, blockHash)

    // Keep only recent tips
    if len(dag.Tips) > 100 {
        dag.Tips = dag.Tips[len(dag.Tips)-100:]
    }
}

// GetTipsForDAG returns current tips for a specific DAG
func (pm *ParallelDAGManager) GetTipsForDAG(dagID uint8) [][32]byte {
    pm.mu.RLock()
    defer pm.mu.RUnlock()

    if dagID >= NumParallelDAGs {
        return nil
    }

    tips := make([][32]byte, len(pm.dags[dagID].Tips))
    copy(tips, pm.dags[dagID].Tips)
    return tips
}

// GetAllTips returns tips from all parallel DAGs
func (pm *ParallelDAGManager) GetAllTips() [][32]byte {
    pm.mu.RLock()
    defer pm.mu.RUnlock()

    var allTips [][32]byte
    for _, dag := range pm.dags {
        allTips = append(allTips, dag.Tips...)
    }
    return allTips
}
```

---

### Phase 3: Mysticeti 3-Round Commit

#### 3.1 Implicit Vote Tracker (`core/mysticeti/implicit_vote.go`)

```go
package mysticeti

import (
    "sync"

    "github.com/Supernova-NovaCoin/core/dag"
)

// In Mysticeti, a reference (parent link) IS a vote
// No separate voting messages needed - this is key to 3-round latency

// Vote represents an implicit vote via parent reference
type Vote struct {
    VoterPubKey    [48]byte
    VoterStake     uint64
    VotedFor       [32]byte  // The block being voted for
    VotingBlock    [32]byte  // The block containing the vote (reference)
    Round          uint64    // Round of the voting block
    Depth          uint8     // 1 = direct, 2 = indirect, 3 = transitive
}

// ImplicitVoteTracker tracks votes implied by DAG references
type ImplicitVoteTracker struct {
    // Block hash -> all votes for it
    votesFor map[[32]byte][]*Vote

    // Block hash -> stake at each depth
    stakeAtDepth map[[32]byte]map[uint8]uint64

    mu sync.RWMutex
}

func NewImplicitVoteTracker() *ImplicitVoteTracker {
    return &ImplicitVoteTracker{
        votesFor:     make(map[[32]byte][]*Vote),
        stakeAtDepth: make(map[[32]byte]map[uint8]uint64),
    }
}

// ProcessVertex extracts implicit votes from a new vertex
func (ivt *ImplicitVoteTracker) ProcessVertex(
    v *dag.Vertex,
    getVertex func([32]byte) *dag.Vertex,
) {
    ivt.mu.Lock()
    defer ivt.mu.Unlock()

    // Direct votes (depth 1): parent references
    for _, parentHash := range v.Parents {
        ivt.recordVote(v, parentHash, 1)
    }

    // Indirect votes (depth 2): grandparents
    for _, parentHash := range v.Parents {
        parent := getVertex(parentHash)
        if parent == nil {
            continue
        }
        for _, grandparentHash := range parent.Parents {
            ivt.recordVote(v, grandparentHash, 2)
        }
    }

    // Transitive votes (depth 3): great-grandparents
    for _, parentHash := range v.Parents {
        parent := getVertex(parentHash)
        if parent == nil {
            continue
        }
        for _, grandparentHash := range parent.Parents {
            grandparent := getVertex(grandparentHash)
            if grandparent == nil {
                continue
            }
            for _, ggHash := range grandparent.Parents {
                ivt.recordVote(v, ggHash, 3)
            }
        }
    }
}

func (ivt *ImplicitVoteTracker) recordVote(v *dag.Vertex, votedFor [32]byte, depth uint8) {
    vote := &Vote{
        VoterPubKey: v.ValidatorPubKey,
        VoterStake:  v.Stake,
        VotedFor:    votedFor,
        VotingBlock: v.Hash,
        Round:       v.Round,
        Depth:       depth,
    }

    // Add to votes list
    ivt.votesFor[votedFor] = append(ivt.votesFor[votedFor], vote)

    // Update stake at depth
    if ivt.stakeAtDepth[votedFor] == nil {
        ivt.stakeAtDepth[votedFor] = make(map[uint8]uint64)
    }
    ivt.stakeAtDepth[votedFor][depth] += v.Stake
}

// GetVotingStake returns stake voting for a block at each depth
func (ivt *ImplicitVoteTracker) GetVotingStake(blockHash [32]byte) (depth1, depth2, depth3 uint64) {
    ivt.mu.RLock()
    defer ivt.mu.RUnlock()

    stakes := ivt.stakeAtDepth[blockHash]
    if stakes == nil {
        return 0, 0, 0
    }

    return stakes[1], stakes[2], stakes[3]
}

// GetAllVotes returns all votes for a block
func (ivt *ImplicitVoteTracker) GetAllVotes(blockHash [32]byte) []*Vote {
    ivt.mu.RLock()
    defer ivt.mu.RUnlock()

    votes := ivt.votesFor[blockHash]
    result := make([]*Vote, len(votes))
    copy(result, votes)
    return result
}
```

#### 3.2 Commit Rule Engine (`core/mysticeti/commit.go`)

```go
package mysticeti

import (
    "sync"
    "time"

    "github.com/Supernova-NovaCoin/core/dag"
)

// CommitCandidate represents a block being evaluated for commit
type CommitCandidate struct {
    Vertex         *dag.Vertex
    Round          uint64

    // Stake at each depth
    DirectStake    uint64  // Round 1: direct references
    IndirectStake  uint64  // Round 2: depth 2 references
    TransitiveStake uint64 // Round 3: depth 3 references

    // Total stake for quorum calculation
    TotalStake     uint64

    // Commit status
    CommitReady    bool
    CommitRound    uint64
    CommitTime     time.Time
}

// MysticetiEngine implements 3-round commit rule
type MysticetiEngine struct {
    voteTracker  *ImplicitVoteTracker
    candidates   map[[32]byte]*CommitCandidate
    committed    map[[32]byte]bool

    // Configuration
    quorumThreshold float64 // 2f+1 = 2/3 for BFT

    mu sync.RWMutex
}

func NewMysticetiEngine(voteTracker *ImplicitVoteTracker) *MysticetiEngine {
    return &MysticetiEngine{
        voteTracker:     voteTracker,
        candidates:      make(map[[32]byte]*CommitCandidate),
        committed:       make(map[[32]byte]bool),
        quorumThreshold: 2.0 / 3.0,
    }
}

// ProcessVertex evaluates commit candidates when new vertex arrives
func (me *MysticetiEngine) ProcessVertex(v *dag.Vertex, totalStake uint64) []*CommitCandidate {
    me.mu.Lock()
    defer me.mu.Unlock()

    var newlyCommitted []*CommitCandidate

    // Add as commit candidate if not already
    if _, exists := me.candidates[v.Hash]; !exists {
        me.candidates[v.Hash] = &CommitCandidate{
            Vertex:     v,
            Round:      v.Round,
            TotalStake: totalStake,
        }
    }

    // Update vote counts for all candidates
    for hash, candidate := range me.candidates {
        if me.committed[hash] {
            continue
        }

        // Get current voting stake at each depth
        d1, d2, d3 := me.voteTracker.GetVotingStake(hash)
        candidate.DirectStake = d1
        candidate.IndirectStake = d2
        candidate.TransitiveStake = d3

        // === Mysticeti 3-Round Commit Rule ===
        // Commit if we have 2f+1 stake at depth 3 (3 message rounds)
        if me.checkCommitRule(candidate) {
            candidate.CommitReady = true
            candidate.CommitRound = v.Round
            candidate.CommitTime = time.Now()
            me.committed[hash] = true
            newlyCommitted = append(newlyCommitted, candidate)
        }
    }

    return newlyCommitted
}

// checkCommitRule implements Mysticeti's uncertified DAG commit rule
func (me *MysticetiEngine) checkCommitRule(c *CommitCandidate) bool {
    threshold := uint64(float64(c.TotalStake) * me.quorumThreshold)

    // === Primary Rule: 3-Round Commit ===
    // Block commits when 2f+1 stake has transitively referenced it
    // This corresponds to 3 message rounds:
    // Round 1: Block proposed
    // Round 2: Validators reference block (direct vote)
    // Round 3: Validators reference round-2 blocks (indirect vote confirms)

    if c.TransitiveStake >= threshold {
        return true
    }

    // === Fast Path: 2-Round Commit (for good network conditions) ===
    // If > 2f+1 stake directly referenced AND indirectly confirmed
    // AND no equivocation detected, can commit in 2 rounds
    if c.DirectStake >= threshold && c.IndirectStake >= threshold {
        // Additional check: no conflicting blocks from same validator
        // (equivocation detection would go here)
        return true
    }

    return false
}

// IsCommitted checks if a block has been committed
func (me *MysticetiEngine) IsCommitted(hash [32]byte) bool {
    me.mu.RLock()
    defer me.mu.RUnlock()
    return me.committed[hash]
}

// GetCommitCandidate returns commit status for a block
func (me *MysticetiEngine) GetCommitCandidate(hash [32]byte) *CommitCandidate {
    me.mu.RLock()
    defer me.mu.RUnlock()

    c := me.candidates[hash]
    if c == nil {
        return nil
    }

    // Return copy
    copy := *c
    return &copy
}

// GetPendingCommits returns all blocks awaiting commit
func (me *MysticetiEngine) GetPendingCommits() []*CommitCandidate {
    me.mu.RLock()
    defer me.mu.RUnlock()

    var pending []*CommitCandidate
    for hash, candidate := range me.candidates {
        if !me.committed[hash] {
            copy := *candidate
            pending = append(pending, &copy)
        }
    }
    return pending
}
```

---

### Phase 4: MEV Resistance Layer

#### 4.1 Threshold Encryption (`core/mev/threshold_encrypt.go`)

```go
package mev

import (
    "crypto/rand"
    "sync"

    bls "github.com/herumi/bls-eth-go-binary/bls"
)

// ThresholdEncryption implements BLS-based threshold encryption
// for encrypted mempool (MEV resistance)

// EncryptedTransaction is a transaction encrypted until block finalization
type EncryptedTransaction struct {
    Ciphertext     []byte     // AES-GCM encrypted transaction
    EncryptionKey  []byte     // Key encrypted with threshold pubkey
    CommitHash     [32]byte   // Hash commitment for ordering
    Timestamp      int64      // Submission time
    SubmitterProof []byte     // Proof of valid submission
}

// ThresholdKeyShare represents a validator's share of decryption key
type ThresholdKeyShare struct {
    ValidatorPK    [48]byte
    ShareIndex     uint32
    Share          *bls.SecretKey
}

// DecryptionShare is a partial decryption from one validator
type DecryptionShare struct {
    ValidatorPK    [48]byte
    ShareIndex     uint32
    PartialDecrypt []byte
    Signature      [96]byte   // Signature over partial decrypt
}

// ThresholdEncryptor manages threshold encryption for MEV resistance
type ThresholdEncryptor struct {
    // DKG output
    publicKey      *bls.PublicKey    // Aggregate public key
    threshold      int               // t-of-n threshold
    totalShares    int               // n validators

    // Local share (if this node is a validator)
    localShare     *ThresholdKeyShare

    // Pending encrypted transactions
    encryptedPool  map[[32]byte]*EncryptedTransaction

    // Decryption shares collected
    decryptShares  map[[32]byte]map[uint32]*DecryptionShare

    mu sync.RWMutex
}

func NewThresholdEncryptor(threshold, total int) *ThresholdEncryptor {
    return &ThresholdEncryptor{
        threshold:     threshold,
        totalShares:   total,
        encryptedPool: make(map[[32]byte]*EncryptedTransaction),
        decryptShares: make(map[[32]byte]map[uint32]*DecryptionShare),
    }
}

// EncryptTransaction encrypts a transaction for the encrypted mempool
func (te *ThresholdEncryptor) EncryptTransaction(txData []byte) (*EncryptedTransaction, error) {
    // Generate random symmetric key
    symKey := make([]byte, 32)
    if _, err := rand.Read(symKey); err != nil {
        return nil, err
    }

    // Encrypt transaction with symmetric key (AES-GCM)
    ciphertext, err := aesGCMEncrypt(symKey, txData)
    if err != nil {
        return nil, err
    }

    // Encrypt symmetric key with threshold public key
    encryptedKey, err := te.encryptKeyForThreshold(symKey)
    if err != nil {
        return nil, err
    }

    // Compute commit hash for ordering
    commitHash := computeCommitHash(ciphertext, encryptedKey)

    etx := &EncryptedTransaction{
        Ciphertext:    ciphertext,
        EncryptionKey: encryptedKey,
        CommitHash:    commitHash,
        Timestamp:     timeNowMs(),
    }

    te.mu.Lock()
    te.encryptedPool[commitHash] = etx
    te.mu.Unlock()

    return etx, nil
}

// SubmitDecryptionShare submits a partial decryption share
func (te *ThresholdEncryptor) SubmitDecryptionShare(
    commitHash [32]byte,
    share *DecryptionShare,
) ([]byte, bool) {
    te.mu.Lock()
    defer te.mu.Unlock()

    // Initialize shares map for this transaction
    if te.decryptShares[commitHash] == nil {
        te.decryptShares[commitHash] = make(map[uint32]*DecryptionShare)
    }

    // Add share
    te.decryptShares[commitHash][share.ShareIndex] = share

    // Check if we have threshold shares
    if len(te.decryptShares[commitHash]) < te.threshold {
        return nil, false // Not enough shares yet
    }

    // Attempt decryption
    etx := te.encryptedPool[commitHash]
    if etx == nil {
        return nil, false
    }

    // Combine shares to recover symmetric key
    symKey, err := te.combineShares(commitHash, etx.EncryptionKey)
    if err != nil {
        return nil, false
    }

    // Decrypt transaction
    txData, err := aesGCMDecrypt(symKey, etx.Ciphertext)
    if err != nil {
        return nil, false
    }

    return txData, true
}

// encryptKeyForThreshold encrypts key using threshold public key
func (te *ThresholdEncryptor) encryptKeyForThreshold(key []byte) ([]byte, error) {
    // Uses Identity-Based Encryption (IBE) with BLS
    // Simplified: encrypt with aggregate public key
    // In production, use proper threshold encryption scheme

    if te.publicKey == nil {
        return nil, ErrNoPublicKey
    }

    // Placeholder: actual implementation uses pairing-based crypto
    return encryptWithBLSPubkey(te.publicKey, key)
}

// combineShares recovers the decryption key from threshold shares
func (te *ThresholdEncryptor) combineShares(
    commitHash [32]byte,
    encryptedKey []byte,
) ([]byte, error) {
    shares := te.decryptShares[commitHash]

    // Lagrange interpolation to recover secret
    // Combines t-of-n shares

    var shareList []*DecryptionShare
    for _, s := range shares {
        shareList = append(shareList, s)
        if len(shareList) >= te.threshold {
            break
        }
    }

    return lagrangeInterpolate(shareList, encryptedKey)
}
```

#### 4.2 Fair Ordering (`core/mev/fair_order.go`)

```go
package mev

import (
    "crypto/sha256"
    "sort"
    "sync"
)

// FairOrderer implements batch-fair transaction ordering
// Combines commit-reveal with blind permutation (BlindPerm)

// OrderedBatch represents a fairly-ordered batch of transactions
type OrderedBatch struct {
    BatchID        uint64
    Transactions   [][32]byte  // Ordered transaction hashes
    OrderingProof  []byte      // ZK proof of fair ordering
    Timestamp      int64
}

// FairOrderer manages fair transaction ordering
type FairOrderer struct {
    // Commit phase: collect encrypted transaction commitments
    commitments map[[32]byte]int64  // commit hash -> timestamp

    // Reveal phase: collect revealed transactions
    revealed map[[32]byte][]byte   // commit hash -> revealed tx

    // Configuration
    batchSize      int
    commitWindow   int64  // ms
    revealWindow   int64  // ms

    mu sync.RWMutex
}

func NewFairOrderer(batchSize int, commitMs, revealMs int64) *FairOrderer {
    return &FairOrderer{
        commitments:  make(map[[32]byte]int64),
        revealed:     make(map[[32]byte][]byte),
        batchSize:    batchSize,
        commitWindow: commitMs,
        revealWindow: revealMs,
    }
}

// SubmitCommitment records a transaction commitment
func (fo *FairOrderer) SubmitCommitment(commitHash [32]byte, timestamp int64) {
    fo.mu.Lock()
    defer fo.mu.Unlock()

    fo.commitments[commitHash] = timestamp
}

// SubmitReveal records a revealed transaction
func (fo *FairOrderer) SubmitReveal(commitHash [32]byte, txData []byte) error {
    fo.mu.Lock()
    defer fo.mu.Unlock()

    // Verify commitment exists
    if _, exists := fo.commitments[commitHash]; !exists {
        return ErrNoCommitment
    }

    // Verify reveal matches commitment
    actualHash := sha256.Sum256(txData)
    if actualHash != commitHash {
        return ErrRevealMismatch
    }

    fo.revealed[commitHash] = txData
    return nil
}

// CreateOrderedBatch creates a fairly-ordered batch
func (fo *FairOrderer) CreateOrderedBatch(batchID uint64, seed []byte) *OrderedBatch {
    fo.mu.Lock()
    defer fo.mu.Unlock()

    // Collect revealed transactions
    var txHashes [][32]byte
    for hash := range fo.revealed {
        txHashes = append(txHashes, hash)
    }

    if len(txHashes) == 0 {
        return nil
    }

    // === Batch-Fair Ordering ===
    // Step 1: Sort by commit timestamp (receive-order fairness)
    sort.Slice(txHashes, func(i, j int) bool {
        return fo.commitments[txHashes[i]] < fo.commitments[txHashes[j]]
    })

    // Step 2: Apply blind permutation (BlindPerm)
    // Randomized shuffle using consensus-derived seed
    // This prevents block producer from controlling exact order
    permuted := fo.blindPermute(txHashes, seed)

    batch := &OrderedBatch{
        BatchID:      batchID,
        Transactions: permuted,
        Timestamp:    timeNowMs(),
    }

    // Generate ordering proof (ZK proof of fair permutation)
    batch.OrderingProof = fo.generateOrderingProof(txHashes, permuted, seed)

    // Clear processed transactions
    for _, hash := range txHashes {
        delete(fo.commitments, hash)
        delete(fo.revealed, hash)
    }

    return batch
}

// blindPermute applies deterministic but unpredictable permutation
func (fo *FairOrderer) blindPermute(hashes [][32]byte, seed []byte) [][32]byte {
    if len(hashes) <= 1 {
        return hashes
    }

    // Fisher-Yates shuffle with seed-derived randomness
    result := make([][32]byte, len(hashes))
    copy(result, hashes)

    // Derive permutation indices from seed
    for i := len(result) - 1; i > 0; i-- {
        // Hash seed with index to get deterministic random
        indexSeed := sha256.Sum256(append(seed, byte(i)))
        j := int(indexSeed[0])<<8 + int(indexSeed[1])
        j = j % (i + 1)

        result[i], result[j] = result[j], result[i]
    }

    return result
}

// generateOrderingProof creates ZK proof of fair permutation
func (fo *FairOrderer) generateOrderingProof(
    original, permuted [][32]byte,
    seed []byte,
) []byte {
    // In production: ZK-SNARK proof that:
    // 1. permuted is a valid permutation of original
    // 2. permutation was derived correctly from seed
    // 3. No transactions were added/removed/modified

    // Placeholder: hash-based commitment
    proof := sha256.Sum256(append(
        flattenHashes(original),
        append(flattenHashes(permuted), seed...)...,
    ))

    return proof[:]
}
```

---

### Phase 5: Hybrid Consensus Orchestrator

#### 5.1 Consensus Coordinator (`core/consensus/hybrid.go`)

```go
package consensus

import (
    "sync"
    "time"

    "github.com/Supernova-NovaCoin/core/dag"
    "github.com/Supernova-NovaCoin/core/dagknight"
    "github.com/Supernova-NovaCoin/core/ghostdag"
    "github.com/Supernova-NovaCoin/core/mysticeti"
    "github.com/Supernova-NovaCoin/core/shoal"
    "github.com/Supernova-NovaCoin/core/finality"
)

// HybridConsensus orchestrates all consensus layers
type HybridConsensus struct {
    // Consensus engines
    dagknight  *dagknight.DAGKnightEngine
    ghostdag   *ghostdag.GHOSTDAGEngine
    shoal      *shoal.AnchorManager
    waveTracker *shoal.WaveTracker
    parallelDAG *shoal.ParallelDAGManager
    mysticeti  *mysticeti.MysticetiEngine

    // Supporting infrastructure
    latency    *dagknight.LatencyMonitor
    finality   *finality.FinalityEngine

    // State
    dagStore   *dag.Store
    totalStake uint64

    // Metrics
    metrics    *ConsensusMetrics

    mu sync.RWMutex
}

// ConsensusMetrics tracks performance
type ConsensusMetrics struct {
    BlocksProcessed  uint64
    AverageFinality  time.Duration
    CurrentK         float64
    CurrentBlockTime time.Duration
    CommitRate       float64  // Commits per second
}

func NewHybridConsensus(config *Config) *HybridConsensus {
    latency := dagknight.NewLatencyMonitor()
    waveTracker := shoal.NewWaveTracker(2.0 / 3.0)
    voteTracker := mysticeti.NewImplicitVoteTracker()

    return &HybridConsensus{
        dagknight:   dagknight.NewDAGKnightEngine(config.DAGKnight, latency),
        ghostdag:    ghostdag.NewGHOSTDAGEngine(config.GHOSTDAG),
        shoal:       shoal.NewAnchorManager(waveTracker),
        waveTracker: waveTracker,
        parallelDAG: shoal.NewParallelDAGManager(),
        mysticeti:   mysticeti.NewMysticetiEngine(voteTracker),
        latency:     latency,
        finality:    finality.NewFinalityEngine(),
        dagStore:    dag.NewStore(),
        metrics:     &ConsensusMetrics{},
    }
}

// ProcessBlock is the main entry point for new blocks
func (hc *HybridConsensus) ProcessBlock(v *dag.Vertex) error {
    hc.mu.Lock()
    defer hc.mu.Unlock()

    startTime := time.Now()

    // === Step 1: DAGKnight Adaptation ===
    // Compute current adaptive K based on network conditions
    adaptiveK := hc.dagknight.AdaptK()
    v.AdaptiveK = float32(adaptiveK)
    v.NetworkConfidence = hc.dagknight.CalculateConfidence()

    // === Step 2: Compute Wave (Shoal++) ===
    v.Wave = hc.waveTracker.ComputeWave(v, hc.dagStore.Get)

    // === Step 3: Process GHOSTDAG (stake-weighted coloring) ===
    if err := hc.ghostdag.ProcessVertex(v, adaptiveK); err != nil {
        return err
    }

    // === Step 4: Store in DAG ===
    if err := hc.dagStore.Add(v); err != nil {
        return err
    }

    // === Step 5: Process Anchor (Shoal++) ===
    // In Shoal++, every validator is an anchor
    v.IsAnchor = true
    if err := hc.shoal.ProcessAnchor(v, hc.totalStake); err != nil {
        return err
    }

    // Process certifications (references from this block to others)
    hc.shoal.ProcessCertification(v, hc.dagStore.Get)

    // Update wave tracker
    if err := hc.waveTracker.ProcessVertex(v, hc.totalStake); err != nil {
        return err
    }

    // Update parallel DAG
    hc.parallelDAG.RecordBlock(v.ParallelDAGId, v.Hash, v.Round)

    // === Step 6: Mysticeti 3-Round Commit ===
    newCommits := hc.mysticeti.ProcessVertex(v, hc.totalStake)

    // === Step 7: Update Finality ===
    for _, commit := range newCommits {
        hc.finality.MarkFinalized(commit.Vertex.Hash)
        commit.Vertex.FinalizedAt = time.Now()
    }

    // === Step 8: Update Metrics ===
    hc.metrics.BlocksProcessed++
    hc.metrics.CurrentK = adaptiveK
    hc.metrics.CurrentBlockTime = hc.dagknight.GetCurrentBlockTime()

    if len(newCommits) > 0 {
        elapsed := time.Since(startTime)
        // Update rolling average finality time
        hc.metrics.AverageFinality = (hc.metrics.AverageFinality + elapsed) / 2
    }

    return nil
}

// GetBlockTime returns current adaptive block time
func (hc *HybridConsensus) GetBlockTime() time.Duration {
    return hc.dagknight.AdaptBlockTime()
}

// GetTips returns current DAG tips for block production
func (hc *HybridConsensus) GetTips() [][32]byte {
    // Get tips from optimal parallel DAG
    dagID := hc.parallelDAG.SelectDAG()
    return hc.parallelDAG.GetTipsForDAG(dagID)
}

// IsFinalized checks if a block is finalized
func (hc *HybridConsensus) IsFinalized(hash [32]byte) bool {
    return hc.mysticeti.IsCommitted(hash)
}

// GetMetrics returns current consensus metrics
func (hc *HybridConsensus) GetMetrics() *ConsensusMetrics {
    hc.mu.RLock()
    defer hc.mu.RUnlock()

    m := *hc.metrics
    return &m
}
```

---

### Phase 6: Configuration

#### 6.1 Unified Config (`core/consensus/config.go`)

```go
package consensus

import (
    "time"

    "github.com/Supernova-NovaCoin/core/dagknight"
)

// Config holds all consensus configuration
type Config struct {
    // Mode selection
    Mode string // "legacy", "dagknight", "hybrid"

    // DAGKnight configuration
    DAGKnight *dagknight.DAGKnightConfig

    // GHOSTDAG configuration
    GHOSTDAG *GHOSTDAGConfig

    // Shoal++ configuration
    Shoal *ShoalConfig

    // Mysticeti configuration
    Mysticeti *MysticetiConfig

    // MEV resistance configuration
    MEV *MEVConfig
}

type GHOSTDAGConfig struct {
    MaxParents         int     // Maximum parent references (default: 10)
    PruningDepth       uint64  // DAG pruning depth
}

type ShoalConfig struct {
    QuorumThreshold    float64       // 2/3 for BFT
    WaveTimeout        time.Duration // Max time per wave
    NumParallelDAGs    int           // Number of parallel DAGs (default: 4)
}

type MysticetiConfig struct {
    QuorumThreshold    float64       // 2/3 for BFT
    MaxPendingCommits  int           // Max commits in flight
    EnableFastPath     bool          // 2-round fast path
}

type MEVConfig struct {
    Enabled            bool
    ThresholdN         int           // Total validators
    ThresholdT         int           // Threshold (t-of-n)
    CommitWindowMs     int64         // Commit phase duration
    RevealWindowMs     int64         // Reveal phase duration
    BatchSize          int           // Transactions per batch
}

// DefaultConfig returns production-ready defaults
func DefaultConfig() *Config {
    return &Config{
        Mode: "hybrid",

        DAGKnight: &dagknight.DAGKnightConfig{
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
        },

        GHOSTDAG: &GHOSTDAGConfig{
            MaxParents:   10,
            PruningDepth: 100000,
        },

        Shoal: &ShoalConfig{
            QuorumThreshold: 2.0 / 3.0,
            WaveTimeout:     5 * time.Second,
            NumParallelDAGs: 4,
        },

        Mysticeti: &MysticetiConfig{
            QuorumThreshold:   2.0 / 3.0,
            MaxPendingCommits: 10000,
            EnableFastPath:    true,
        },

        MEV: &MEVConfig{
            Enabled:        true,
            ThresholdN:     100,
            ThresholdT:     67, // 2/3 + 1
            CommitWindowMs: 500,
            RevealWindowMs: 500,
            BatchSize:      1000,
        },
    }
}
```

---

## Expected Performance

| Metric | Target | How Achieved |
|--------|--------|--------------|
| Block Time | 500ms adaptive | DAGKnight dynamic adjustment |
| Finality | 1.5s (3 rounds) | Mysticeti uncertified DAG |
| Throughput | 100k+ TPS | Shoal++ parallel DAGs |
| Byzantine Tolerance | 50% | DAGKnight parameterless |
| MEV Resistance | Full | Threshold encryption + fair ordering |
| Message Rounds | 3 (optimal) | Mysticeti commit rule |
| Queuing Latency | 0.5 rounds | Parallel staggered DAGs |

---

## Security Considerations

| Threat | Mitigation |
|--------|------------|
| Latency spoofing | Stake-weighted measurements; cross-validate with propagation |
| Wave stuffing | One anchor per validator per wave |
| Adaptive K manipulation | Only measure staked peers; smooth transitions |
| Nothing-at-stake | Slashing for equivocation |
| MEV front-running | Threshold encryption until finalization |
| Sandwich attacks | Blind permutation ordering |
| Long-range attacks | Checkpointing + weak subjectivity |

---

## zkEVM Layer (Phase 7)

#### 7.1 zkEVM Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         zkEVM LAYER                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────────┐    │
│  │   Sequencer  │────▶│   Executor   │────▶│   STARK Prover   │    │
│  │  (Batch Txs) │     │  (Run EVM)   │     │ (Generate Proof) │    │
│  └──────────────┘     └──────────────┘     └──────────────────┘    │
│         │                    │                      │              │
│         ▼                    ▼                      ▼              │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────────┐    │
│  │ Transaction  │     │    State     │     │  Proof Verifier  │    │
│  │    Pool      │     │   Manager    │     │   (On-Chain)     │    │
│  └──────────────┘     └──────────────┘     └──────────────────┘    │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

#### 7.2 zkEVM Executor (`core/zk/executor.go`)

```go
package zk

import (
    "math/big"
    "sync"

    "github.com/Supernova-NovaCoin/core/state"
    "github.com/Supernova-NovaCoin/core/vm"
)

// ExecutionTrace captures all state changes for proof generation
type ExecutionTrace struct {
    TxHash          [32]byte
    From            [20]byte
    To              [20]byte
    Input           []byte
    GasUsed         uint64

    // State access patterns (for STARK witness)
    StorageReads    []StorageAccess
    StorageWrites   []StorageAccess
    MemoryOps       []MemoryOp
    StackOps        []StackOp

    // Execution steps
    Steps           []ExecutionStep
    Success         bool
    ReturnData      []byte
}

type ExecutionStep struct {
    PC              uint64
    OpCode          byte
    Gas             uint64
    GasCost         uint64
    Stack           []*big.Int
    Memory          []byte
    Storage         map[[32]byte][32]byte
}

type StorageAccess struct {
    Address [20]byte
    Key     [32]byte
    Value   [32]byte
    IsWrite bool
}

// zkEVMExecutor runs EVM and captures execution trace
type zkEVMExecutor struct {
    stateDB     *state.StateDB
    chainConfig *vm.ChainConfig

    // Batch execution
    traces      []*ExecutionTrace
    batchRoot   [32]byte

    mu sync.Mutex
}

func NewzkEVMExecutor(stateDB *state.StateDB, config *vm.ChainConfig) *zkEVMExecutor {
    return &zkEVMExecutor{
        stateDB:     stateDB,
        chainConfig: config,
        traces:      make([]*ExecutionTrace, 0),
    }
}

// ExecuteTx runs a transaction and captures trace for ZK proof
func (e *zkEVMExecutor) ExecuteTx(tx *Transaction) (*ExecutionTrace, error) {
    e.mu.Lock()
    defer e.mu.Unlock()

    trace := &ExecutionTrace{
        TxHash: tx.Hash(),
        From:   tx.From(),
        To:     tx.To(),
        Input:  tx.Data(),
    }

    // Create EVM context with trace hooks
    evm := vm.NewEVM(e.createContext(tx), e.stateDB, e.chainConfig)
    evm.SetTracer(&zkTracer{trace: trace})

    // Execute transaction
    result, err := evm.Call(
        vm.AccountRef(tx.From()),
        tx.To(),
        tx.Data(),
        tx.Gas(),
        tx.Value(),
    )

    trace.GasUsed = tx.Gas() - result.GasLeft
    trace.Success = err == nil
    trace.ReturnData = result.ReturnData

    e.traces = append(e.traces, trace)

    return trace, err
}

// BatchExecute processes a batch of transactions
func (e *zkEVMExecutor) BatchExecute(txs []*Transaction) ([]*ExecutionTrace, error) {
    traces := make([]*ExecutionTrace, 0, len(txs))

    for _, tx := range txs {
        trace, err := e.ExecuteTx(tx)
        if err != nil {
            trace.Success = false
        }
        traces = append(traces, trace)
    }

    return traces, nil
}

// GetBatchWitness returns witness data for STARK prover
func (e *zkEVMExecutor) GetBatchWitness() *BatchWitness {
    e.mu.Lock()
    defer e.mu.Unlock()

    return &BatchWitness{
        Traces:         e.traces,
        PreStateRoot:   e.stateDB.GetPreRoot(),
        PostStateRoot:  e.stateDB.Root(),
        TxCount:        len(e.traces),
    }
}
```

#### 7.3 STARK Prover Interface (`core/zk/prover.go`)

```go
package zk

import (
    "context"
)

// ProverConfig configures the STARK prover
type ProverConfig struct {
    SecurityLevel   int    // 128 bits default
    BlowupFactor    int    // 8x default
    NumQueries      int    // 80 default
    HashFunction    string // "poseidon" or "keccak"
}

// Proof represents a STARK proof
type Proof struct {
    // Commitment phase
    TraceCommitment     [32]byte
    ConstraintCommitment [32]byte

    // FRI layers (for polynomial commitment)
    FRILayers           []FRILayer

    // Query responses
    Queries             []QueryResponse

    // Public inputs
    PublicInputs        []byte
}

type FRILayer struct {
    Commitment [32]byte
    Polynomial []byte
}

type QueryResponse struct {
    Index      uint64
    Value      []byte
    AuthPath   [][32]byte
}

// BatchWitness contains all data needed to generate proof
type BatchWitness struct {
    Traces         []*ExecutionTrace
    PreStateRoot   [32]byte
    PostStateRoot  [32]byte
    TxCount        int
}

// STARKProver interface for proof generation
type STARKProver interface {
    // Setup generates proving/verifying keys (one-time)
    Setup(config *ProverConfig) error

    // Prove generates a STARK proof for a batch
    Prove(ctx context.Context, witness *BatchWitness) (*Proof, error)

    // Verify checks a proof (fast, for on-chain verification)
    Verify(proof *Proof, publicInputs []byte) (bool, error)

    // EstimateGas estimates verification gas cost
    EstimateGas(proof *Proof) uint64
}

// RecursiveProver wraps proofs for aggregation
type RecursiveProver interface {
    STARKProver

    // AggregateProofs combines multiple proofs into one
    AggregateProofs(proofs []*Proof) (*Proof, error)

    // ProofOfProof creates a proof that verifies other proofs
    ProofOfProof(innerProofs []*Proof) (*Proof, error)
}

// DefaultSTARKProver implements STARKProver using STARK-friendly hash
type DefaultSTARKProver struct {
    config *ProverConfig
}

func NewSTARKProver(config *ProverConfig) *DefaultSTARKProver {
    if config == nil {
        config = &ProverConfig{
            SecurityLevel: 128,
            BlowupFactor:  8,
            NumQueries:    80,
            HashFunction:  "poseidon",
        }
    }
    return &DefaultSTARKProver{config: config}
}

func (p *DefaultSTARKProver) Prove(ctx context.Context, witness *BatchWitness) (*Proof, error) {
    // 1. Build execution trace polynomial
    tracePoly := buildTracePolynomial(witness.Traces)

    // 2. Build constraint polynomial (AIR constraints)
    constraintPoly := buildConstraintPolynomial(tracePoly)

    // 3. Commit to trace using Merkle tree
    traceCommit := merkleCommit(tracePoly)

    // 4. Generate FRI proof for polynomial commitment
    friProof := generateFRI(constraintPoly, p.config)

    // 5. Generate query responses
    queries := generateQueries(tracePoly, constraintPoly, p.config.NumQueries)

    return &Proof{
        TraceCommitment:      traceCommit,
        ConstraintCommitment: merkleCommit(constraintPoly),
        FRILayers:           friProof,
        Queries:             queries,
        PublicInputs:        encodePublicInputs(witness),
    }, nil
}

func (p *DefaultSTARKProver) Verify(proof *Proof, publicInputs []byte) (bool, error) {
    // 1. Verify FRI layers
    if !verifyFRI(proof.FRILayers) {
        return false, nil
    }

    // 2. Verify query responses against commitments
    for _, query := range proof.Queries {
        if !verifyQuery(query, proof.TraceCommitment) {
            return false, nil
        }
    }

    // 3. Check constraint satisfaction
    if !checkConstraints(proof, publicInputs) {
        return false, nil
    }

    return true, nil
}

func (p *DefaultSTARKProver) EstimateGas(proof *Proof) uint64 {
    // Base cost + per-query cost + FRI layer costs
    base := uint64(100000)
    queryGas := uint64(len(proof.Queries)) * 2000
    friGas := uint64(len(proof.FRILayers)) * 5000

    return base + queryGas + friGas
}
```

#### 7.4 zkEVM State Manager (`core/zk/state.go`)

```go
package zk

import (
    "sync"

    "github.com/Supernova-NovaCoin/core/state"
)

// zkStateManager handles state for zkEVM with proof-friendly structure
type zkStateManager struct {
    // Current state
    stateDB     *state.StateDB

    // State transitions for batching
    transitions []*StateTransition

    // Pending batches
    pendingBatch *Batch

    // Committed batches (with proofs)
    committed    map[[32]byte]*CommittedBatch

    mu sync.RWMutex
}

// StateTransition represents a single state change
type StateTransition struct {
    TxHash      [32]byte
    Address     [20]byte
    Key         [32]byte
    PrevValue   [32]byte
    NewValue    [32]byte
    Type        TransitionType
}

type TransitionType uint8

const (
    StorageWrite TransitionType = iota
    BalanceChange
    NonceIncrement
    ContractCreate
    ContractDestroy
)

// Batch represents a batch of transactions to be proven
type Batch struct {
    BatchNumber     uint64
    Transactions    []*Transaction
    Transitions     []*StateTransition
    PreStateRoot    [32]byte
    PostStateRoot   [32]byte
    Timestamp       int64
}

// CommittedBatch is a batch with its STARK proof
type CommittedBatch struct {
    Batch    *Batch
    Proof    *Proof
    Verified bool
}

func NewzkStateManager(stateDB *state.StateDB) *zkStateManager {
    return &zkStateManager{
        stateDB:   stateDB,
        committed: make(map[[32]byte]*CommittedBatch),
    }
}

// StartBatch begins a new batch
func (m *zkStateManager) StartBatch(batchNumber uint64) {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.pendingBatch = &Batch{
        BatchNumber:  batchNumber,
        PreStateRoot: m.stateDB.Root(),
        Timestamp:    timeNowMs(),
    }
    m.transitions = make([]*StateTransition, 0)
}

// RecordTransition records a state transition
func (m *zkStateManager) RecordTransition(t *StateTransition) {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.transitions = append(m.transitions, t)
}

// FinalizeBatch completes a batch and generates witness
func (m *zkStateManager) FinalizeBatch() *BatchWitness {
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.pendingBatch == nil {
        return nil
    }

    m.pendingBatch.Transitions = m.transitions
    m.pendingBatch.PostStateRoot = m.stateDB.Root()

    witness := &BatchWitness{
        PreStateRoot:  m.pendingBatch.PreStateRoot,
        PostStateRoot: m.pendingBatch.PostStateRoot,
        TxCount:       len(m.pendingBatch.Transactions),
    }

    return witness
}

// CommitBatch stores a proven batch
func (m *zkStateManager) CommitBatch(batch *Batch, proof *Proof) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.committed[batch.PostStateRoot] = &CommittedBatch{
        Batch:    batch,
        Proof:    proof,
        Verified: true,
    }

    return nil
}

// GetProofForState returns proof that a state root is valid
func (m *zkStateManager) GetProofForState(stateRoot [32]byte) *Proof {
    m.mu.RLock()
    defer m.mu.RUnlock()

    if cb := m.committed[stateRoot]; cb != nil {
        return cb.Proof
    }
    return nil
}
```

#### 7.5 File Structure Update for zkEVM

```
core/zk/
├── executor.go          # zkEVM executor with tracing
├── prover.go            # STARK prover interface
├── verifier.go          # On-chain verifier
├── state.go             # zkEVM state manager
├── batch.go             # Batch management
├── recursive.go         # Recursive proof aggregation
├── air/
│   ├── constraints.go   # AIR constraints for EVM
│   ├── trace.go         # Execution trace builder
│   └── polynomial.go    # Polynomial operations
├── fri/
│   ├── fri.go           # FRI protocol
│   ├── commit.go        # Polynomial commitment
│   └── query.go         # Query generation
└── hash/
    ├── poseidon.go      # Poseidon hash (STARK-friendly)
    └── rescue.go        # Rescue hash alternative
```

---

## Implementation Order

1. **Core Infrastructure** (Week 1-2)
   - `core/dag/vertex.go` - Extended vertex structure
   - `core/dagknight/latency.go` - Latency monitoring
   - `core/dagknight/engine.go` - Adaptive K engine

2. **GHOSTDAG Layer** (Week 2-3)
   - `core/ghostdag/ghostdag.go` - Stake-weighted coloring
   - `core/ghostdag/ordering.go` - Topological ordering

3. **Shoal++ Layer** (Week 3-4)
   - `core/shoal/wave.go` - Wave computation
   - `core/shoal/anchor.go` - Multi-anchor manager
   - `core/shoal/parallel_dag.go` - Parallel DAGs

4. **Mysticeti Layer** (Week 4-5)
   - `core/mysticeti/implicit_vote.go` - Vote tracking
   - `core/mysticeti/commit.go` - 3-round commit

5. **MEV Layer** (Week 5-6)
   - `core/mev/threshold_encrypt.go` - Threshold encryption
   - `core/mev/fair_order.go` - Fair ordering

6. **Integration** (Week 6-7)
   - `core/consensus/hybrid.go` - Orchestrator
   - `core/consensus/config.go` - Configuration
   - `p2p/server.go` - Wire up consensus

7. **zkEVM Layer** (Week 7-8)
   - `core/zk/executor.go` - zkEVM with execution tracing
   - `core/zk/prover.go` - STARK prover interface
   - `core/zk/state.go` - Proof-friendly state management

8. **Testing & Benchmarks** (Week 8-9)
   - Unit tests for each component
   - Integration tests
   - Stress tests + benchmarks
   - ZK proof generation benchmarks

---

## Files to Create

| File | Purpose | Lines Est. |
|------|---------|------------|
| `core/dag/vertex.go` | Extended vertex structure | 200 |
| `core/dagknight/latency.go` | Latency monitoring | 300 |
| `core/dagknight/engine.go` | Adaptive K engine | 250 |
| `core/dagknight/confidence.go` | Confidence scoring | 100 |
| `core/ghostdag/ghostdag.go` | Stake-weighted GHOSTDAG | 400 |
| `core/ghostdag/coloring.go` | Blue/red coloring | 200 |
| `core/ghostdag/ordering.go` | Topological ordering | 150 |
| `core/shoal/wave.go` | Wave computation | 200 |
| `core/shoal/anchor.go` | Multi-anchor manager | 250 |
| `core/shoal/parallel_dag.go` | Parallel DAG manager | 150 |
| `core/mysticeti/implicit_vote.go` | Vote tracking | 200 |
| `core/mysticeti/commit.go` | 3-round commit engine | 250 |
| `core/finality/finality.go` | Multi-tier finality | 200 |
| `core/mev/threshold_encrypt.go` | Threshold encryption | 300 |
| `core/mev/dkg.go` | Distributed key generation | 250 |
| `core/mev/fair_order.go` | Fair ordering | 200 |
| `core/consensus/hybrid.go` | Orchestrator | 300 |
| `core/consensus/config.go` | Configuration | 150 |
| `p2p/latency_probe.go` | Ping/pong probing | 150 |
| `core/zk/executor.go` | zkEVM executor | 350 |
| `core/zk/prover.go` | STARK prover | 400 |
| `core/zk/verifier.go` | On-chain verifier | 200 |
| `core/zk/state.go` | ZK state manager | 250 |
| `core/zk/batch.go` | Batch management | 150 |
| `core/zk/recursive.go` | Recursive proofs | 200 |
| `core/zk/air/constraints.go` | AIR constraints | 300 |
| `core/zk/fri/fri.go` | FRI protocol | 250 |
| `core/zk/hash/poseidon.go` | STARK-friendly hash | 150 |
| **Total** | | **~6,300** |

---

## User Configuration Summary

| Setting | Choice |
|---------|--------|
| Language | **Go** |
| Priority | **Throughput (100k+ TPS)** |
| Smart Contracts | **zkEVM (Solidity compatible)** |
| MEV Resistance | **Full (threshold encryption + fair ordering)** |

---

## Research Sources

- [DAGKnight Protocol Paper](https://eprint.iacr.org/2022/1494.pdf)
- [Mysticeti: Uncertified DAGs](https://arxiv.org/abs/2310.14821)
- [Shoal++: High Throughput DAG BFT](https://arxiv.org/abs/2405.20488)
- [Sui Mysticeti Documentation](https://sui.io/mysticeti)
- [Kaspa DAGKnight Implementation](https://kaspa.org/the-dag-knight-protocol-elevating-kaspa/)
- [Ethereum zkEVM Architecture](https://ethereum.org/developers/docs/scaling/zk-rollups/)
- [Encrypted Mempool EIP](https://docs.shutter.network/docs/shutter/research/)
