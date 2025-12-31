# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

NovaCoin is a next-generation Layer 1 blockchain combining four consensus innovations:
- **PoS-DAGKnight**: Parameterless consensus with 50% Byzantine tolerance
- **Mysticeti 3-Round**: Fast finality (~1.5s) via uncertified DAG commits
- **Shoal++ Multi-Anchor**: High throughput (100k+ TPS) with parallel DAGs
- **ZK-Rollup Native**: STARK proofs for scalability and privacy
- **Encrypted Mempool**: MEV resistance via threshold encryption

## Architecture Layers (Top to Bottom)

```
Application Layer (zkEVM, DeFi, dApps)
    ↓
ZK-Rollup Layer (STARK Prover, Batch Commit, Recursive Verify)
    ↓
MEV Resistance Layer (Threshold Encrypt, Fair Ordering, BlindPerm)
    ↓
Consensus Orchestrator (HybridConsensus - coordinates all engines)
    ↓
DAGKnight Layer (Adaptive K, Dynamic BlockTime, Latency Monitor)
    ↓
Shoal++ Layer (Parallel DAGs, Anchor Manager, Wave Tracker)
    ↓
Mysticeti Layer (Implicit Vote, Pattern Detect, 3-Round Commit)
    ↓
PoS Foundation (Stake-Weighted GHOSTDAG, Staking, Slashing)
    ↓
P2P Network Layer
```

## Key Technical Concepts

### Consensus Flow
1. **DAGKnight** adapts K parameter (0.25-0.50) based on network latency
2. **GHOSTDAG** performs stake-weighted blue/red coloring
3. **Shoal++** manages waves and anchors (all validators are anchors)
4. **Mysticeti** commits blocks after 2f+1 stake at depth 3

### Critical Data Structures
- `Vertex` (core/dag/vertex.go): Extended block with consensus metadata including AdaptiveK, Wave, Round, BlueScore, ImplicitVotes
- `HybridConsensus` (core/consensus/hybrid.go): Main orchestrator coordinating all engines

### MEV Resistance
- Transactions encrypted with BLS threshold encryption
- Commit-reveal with blind permutation (BlindPerm) for fair ordering
- Decryption only after block finalization

### ZK Proofs
- STARK proofs for batch execution verification
- Recursive proofs for aggregation
- Poseidon hash function for STARK-friendliness

## Directory Structure

```
novacoin/
├── cmd/novacoin/main.go          # Entry point
├── core/
│   ├── consensus/hybrid.go       # Main consensus orchestrator
│   ├── dagknight/                # Adaptive K, latency monitoring
│   ├── shoal/                    # Multi-anchor, waves, parallel DAGs
│   ├── mysticeti/                # 3-round commit, implicit voting
│   ├── ghostdag/                 # Stake-weighted DAG coloring
│   ├── mev/                      # Threshold encryption, fair ordering
│   ├── zk/                       # STARK prover, zkEVM executor
│   ├── pos/                      # Staking, slashing, rewards
│   └── dag/                      # Vertex structure, storage
├── p2p/                          # Networking, gossip, latency probes
├── crypto/                       # BLS, threshold signatures
└── config/                       # Node configuration
```

## Default Configuration Values

- Block Time: 500ms (adaptive range: 500ms - 10s)
- K Parameter: 0.33 default (range: 0.25 - 0.50)
- Quorum Threshold: 2/3 stake (BFT)
- Parallel DAGs: 4 (staggered by 125ms)
- MEV Threshold: 67-of-100 validators

## Implementation Language

Go (Golang) - all core components use Go idioms and standard library patterns.
