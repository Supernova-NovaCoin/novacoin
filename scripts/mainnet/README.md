# NovaCoin Mainnet Deployment

This directory contains all scripts and configurations required for secure NovaCoin mainnet deployment.

## Overview

NovaCoin uses a multi-phase, multi-sig deployment process to ensure security and accountability for mainnet launches.

### Key Features

- **4-of-7 Multi-Sig Approval**: Deployment requires approval from at least 4 of 7 authorized signers
- **Security Checklist**: Comprehensive checklist covering code audit, cryptography, consensus, network, and operations
- **Gradual Validator Onboarding**: Controlled validator activation with queue management
- **Emergency Procedures**: Graceful shutdown, emergency halt, and rollback capabilities
- **Verification Scripts**: Automated pre-deployment verification

## Directory Structure

```
scripts/mainnet/
├── README.md                    # This file
├── mainnet-config.yaml          # Main configuration file
├── SECURITY-CHECKLIST.md        # Security audit checklist
├── deploy-mainnet.sh            # Main deployment preparation script
├── verify-deployment.sh         # Deployment verification script
├── launch-mainnet.sh            # Final launch sequence
├── multisig-deploy.sh           # Multi-sig approval management
├── emergency-shutdown.sh        # Emergency procedures
├── validator-onboarding.sh      # Validator management
├── genesis/                     # Genesis configuration
│   └── genesis.json             # Genesis block
├── bootstrap/                   # Bootstrap node configuration
│   └── bootstrap-config.yaml    # Bootstrap nodes
├── validators/                  # Validator data
│   ├── registry.json            # Validator registry
│   ├── activation-queue.json    # Activation queue
│   ├── deposits/                # Validator deposits
│   └── keys/                    # Validator keys (gitignored)
└── logs/                        # Deployment logs (gitignored)
```

## Deployment Process

### Phase 1: Preparation

1. **Complete Security Checklist**
   ```bash
   # Review and complete all items in SECURITY-CHECKLIST.md
   # All items must be checked [x] before deployment
   ```

2. **Initialize Multi-Sig Signers**
   ```bash
   ./multisig-deploy.sh init
   # Enter 7 authorized signer IDs and public keys
   ```

3. **Run Deployment Preparation**
   ```bash
   ./deploy-mainnet.sh
   # This will:
   # - Verify multi-sig approvals
   # - Check security checklist
   # - Build binaries
   # - Run tests
   # - Generate genesis configuration
   ```

### Phase 2: Multi-Sig Approval

1. **Create Deployment Request**
   ```bash
   ./multisig-deploy.sh request
   # Creates a new deployment request with unique ID
   ```

2. **Collect Approvals** (Each signer runs this)
   ```bash
   ./multisig-deploy.sh approve
   # Signer reviews and approves deployment
   ```

3. **Verify Approvals**
   ```bash
   ./multisig-deploy.sh verify
   # Ensures 4+ valid signatures collected
   ```

4. **Check Status**
   ```bash
   ./multisig-deploy.sh status
   # Shows current approval status
   ```

### Phase 3: Validator Onboarding

1. **Initialize Validator System**
   ```bash
   ./validator-onboarding.sh init
   ```

2. **Check Hardware Requirements**
   ```bash
   ./validator-onboarding.sh check-hardware
   ```

3. **Generate Validator Keys** (Each validator)
   ```bash
   ./validator-onboarding.sh generate-keys
   ```

4. **Register Validators**
   ```bash
   ./validator-onboarding.sh register
   ```

5. **Verify Deposits**
   ```bash
   ./validator-onboarding.sh verify-deposit <validator_id>
   ```

6. **Process Activation Queue**
   ```bash
   ./validator-onboarding.sh process-queue
   ```

### Phase 4: Verification

```bash
./verify-deployment.sh
# Verifies:
# - Binary checksums
# - Configuration validity
# - Genesis file
# - Security checklist completion
# - Multi-sig approvals
# - Network ports
# - Validator configuration
# - Monitoring setup
```

### Phase 5: Launch

```bash
./launch-mainnet.sh
# Final launch sequence:
# - Pre-launch verification
# - Bootstrap node startup
# - Genesis initialization
# - Validator activation
# - Consensus engine start
# - Network health verification
```

## Emergency Procedures

### Graceful Shutdown
```bash
./emergency-shutdown.sh graceful
# Stops accepting transactions, waits for pending, saves state
```

### Emergency Halt
```bash
./emergency-shutdown.sh halt
# Immediate stop of all processes, notifies team
```

### Pause Consensus
```bash
./emergency-shutdown.sh pause
# Pauses block production, node stays running for queries
```

### Resume Consensus
```bash
./emergency-shutdown.sh resume
# Resumes block production after pause
```

### Rollback (DANGEROUS)
```bash
./emergency-shutdown.sh rollback <block_number>
# Rolls back state to specific block - all later transactions lost!
```

### Check Status
```bash
./emergency-shutdown.sh status
# Shows current node status
```

## Configuration

### mainnet-config.yaml

Key configuration sections:

| Section | Description |
|---------|-------------|
| `network` | Chain ID, bootnodes |
| `consensus.dagknight` | Adaptive K parameters |
| `consensus.mysticeti` | Commit depth, vote threshold |
| `consensus.shoal` | Parallel DAGs, wave configuration |
| `pos.staking` | Minimum stake, unbonding period |
| `pos.slashing` | Slashing conditions and penalties |
| `mev` | Threshold encryption settings |
| `genesis` | Initial supply, allocations |

### Hardware Requirements

| Component | Minimum | Recommended |
|-----------|---------|-------------|
| CPU | 16 cores | 32 cores |
| RAM | 64 GB | 128 GB |
| Storage | 2 TB NVMe | 4 TB NVMe |
| Network | 1 Gbps | 10 Gbps |

## Security Considerations

1. **Key Management**
   - Never commit private keys
   - Use hardware wallets for multi-sig
   - Encrypt validator keys at rest

2. **Access Control**
   - SSH key-only authentication
   - Multi-factor for all admin access
   - Principle of least privilege

3. **Network Security**
   - Firewall all non-essential ports
   - Use TLS for RPC in production
   - Monitor for DDoS

4. **Operational Security**
   - Audit all deployments
   - Rotate credentials regularly
   - Test emergency procedures

## Monitoring

After launch, monitor via:

- **Prometheus**: Metrics at port 9090
- **Grafana**: Dashboards for visualization
- **Logs**: Check `logs/` directory
- **Alerts**: Configure PagerDuty/Slack integration

## Troubleshooting

### Common Issues

1. **Multi-sig approval fails**
   - Verify signer is in authorized list
   - Check signature format
   - Ensure deployment request exists

2. **Binary checksum mismatch**
   - Rebuild from clean state
   - Verify Go version matches

3. **Genesis validation fails**
   - Check JSON syntax
   - Verify chain ID is 1
   - Ensure all allocations are valid addresses

4. **Validator activation stuck**
   - Check deposit was confirmed on-chain
   - Verify stake meets minimum
   - Process activation queue manually

### Getting Help

- **Security Issues**: security@novacoin.io
- **On-Call**: oncall@novacoin.io
- **Documentation**: https://docs.novacoin.io

## License

Copyright (c) 2025 NovaCoin Foundation. All rights reserved.
