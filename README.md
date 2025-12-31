# NovaCoin

Next-generation Layer 1 blockchain combining four consensus innovations for maximum security, throughput, and finality.

## Features

- **PoS-DAGKnight**: Parameterless consensus with 50% Byzantine tolerance
- **Mysticeti 3-Round**: Fast finality (~1.5s) via uncertified DAG commits
- **Shoal++ Multi-Anchor**: High throughput (100k+ TPS) with parallel DAGs
- **ZK-Rollup Native**: STARK proofs for scalability and privacy
- **Encrypted Mempool**: MEV resistance via threshold encryption

## Architecture

```
Application Layer (zkEVM, DeFi, dApps)
    ↓
ZK-Rollup Layer (STARK Prover, Batch Commit, Recursive Verify)
    ↓
MEV Resistance Layer (Threshold Encrypt, Fair Ordering, BlindPerm)
    ↓
Consensus Orchestrator (HybridConsensus)
    ↓
DAGKnight → Shoal++ → Mysticeti → GHOSTDAG
    ↓
PoS Foundation (Stake-Weighted GHOSTDAG, Staking, Slashing)
    ↓
P2P Network Layer
```

## Directory Structure

```
novacoin/
├── cmd/novacoin/         # Node entry point
├── core/
│   ├── consensus/        # Hybrid consensus orchestrator
│   ├── dagknight/        # Adaptive K, latency monitoring
│   ├── shoal/            # Multi-anchor, waves, parallel DAGs
│   ├── mysticeti/        # 3-round commit, implicit voting
│   ├── ghostdag/         # Stake-weighted DAG coloring
│   ├── mev/              # Threshold encryption, fair ordering
│   ├── zk/               # STARK prover, zkEVM executor
│   ├── pos/              # Staking, slashing, rewards
│   ├── evm/              # EVM interpreter
│   ├── state/            # State management
│   └── dag/              # Vertex structure, storage
├── p2p/                  # Networking, gossip, latency probes
├── rpc/                  # JSON-RPC server
├── crypto/               # BLS, threshold signatures
├── config/               # Node configuration
└── scripts/              # Deployment scripts
```

## Quick Start

### Build

```bash
go build -o novacoin ./cmd/novacoin
```

### Run Node

```bash
./novacoin --datadir ~/.novacoin --networkid 1 --rpc --ws
```

### Run Validator

```bash
./novacoin --datadir ~/.novacoin --validator --validator-key /path/to/key
```

## Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| Block Time | 500ms | Adaptive range: 500ms - 10s |
| K Parameter | 0.33 | DAGKnight adaptive range: 0.25 - 0.50 |
| Quorum | 2/3 stake | BFT quorum threshold |
| Parallel DAGs | 4 | Staggered by 125ms |
| MEV Threshold | 67-of-100 | Threshold encryption validators |

## Related Repositories

- [novacoin-sdk-ts](https://github.com/Supernova-NovaCoin/novacoin-sdk-ts) - TypeScript SDK
- [novacoin-wallet-web](https://github.com/Supernova-NovaCoin/novacoin-wallet-web) - Web Wallet
- [novacoin-wallet-cli](https://github.com/Supernova-NovaCoin/novacoin-wallet-cli) - CLI Wallet
- [novacoin-explorer](https://github.com/Supernova-NovaCoin/novacoin-explorer) - Block Explorer
- [novacoin-dashboard-node](https://github.com/Supernova-NovaCoin/novacoin-dashboard-node) - Node Dashboard
- [novacoin-dashboard-tokens](https://github.com/Supernova-NovaCoin/novacoin-dashboard-tokens) - Token Dashboard
- [novacoin-docs](https://github.com/Supernova-NovaCoin/novacoin-docs) - Documentation

## Testing

```bash
go test ./... -count=1
```

## License

MIT License - see [LICENSE](LICENSE) for details.
