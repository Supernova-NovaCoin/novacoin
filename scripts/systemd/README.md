# NovaCoin Systemd Services

## Installation

### 1. Create novacoin user

```bash
sudo useradd -r -m -s /bin/bash novacoin
sudo mkdir -p /home/novacoin/.novacoin/{data,keys,logs}
sudo chown -R novacoin:novacoin /home/novacoin/.novacoin
sudo chmod 700 /home/novacoin/.novacoin/keys
```

### 2. Install NovaCoin binary

```bash
sudo cp novacoin /usr/local/bin/
sudo chmod +x /usr/local/bin/novacoin
```

### 3. Install service file

**For Full Node:**
```bash
sudo cp novacoin-node.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable novacoin-node
sudo systemctl start novacoin-node
```

**For Validator:**
```bash
# First, generate or copy validator key
sudo cp validator.key /home/novacoin/.novacoin/keys/
sudo chown novacoin:novacoin /home/novacoin/.novacoin/keys/validator.key
sudo chmod 600 /home/novacoin/.novacoin/keys/validator.key

# Then install service
sudo cp novacoin-validator.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable novacoin-validator
sudo systemctl start novacoin-validator
```

## Configuration

Create environment file for custom settings:

```bash
# /etc/novacoin/node.env
NETWORK_ID=1
```

```bash
# /etc/novacoin/validator.env
NETWORK_ID=1
```

## Commands

```bash
# Check status
sudo systemctl status novacoin-node
sudo systemctl status novacoin-validator

# View logs
sudo journalctl -u novacoin-node -f
sudo journalctl -u novacoin-validator -f

# Restart
sudo systemctl restart novacoin-node
sudo systemctl restart novacoin-validator

# Stop
sudo systemctl stop novacoin-node
sudo systemctl stop novacoin-validator
```

## Monitoring

The metrics endpoint is available at:
- Full Node: `http://127.0.0.1:9090/metrics`
- Validator: `http://0.0.0.0:9090/metrics` (exposed for Prometheus scraping)

## Security Notes

1. **Validator keys** are stored with restricted permissions (600)
2. **RPC endpoints** are restricted to localhost for validators
3. **SystemD hardening** is enabled (NoNewPrivileges, PrivateTmp, ProtectSystem)
4. **Resource limits** are set to prevent resource exhaustion
