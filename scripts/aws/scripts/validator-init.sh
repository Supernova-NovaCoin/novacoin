#!/bin/bash
# NovaCoin Validator Initialization Script
# This script is executed via cloud-init on first boot

set -euo pipefail

exec > >(tee /var/log/novacoin-init.log) 2>&1
echo "=== NovaCoin Validator Init Started: $(date) ==="

# Variables from Terraform
CHAIN_ID="${chain_id}"
ENVIRONMENT="${environment}"
VALIDATOR_INDEX="${validator_index}"
BOOTNODE_IP="${bootnode_ip}"

# =============================================================================
# SYSTEM SETUP
# =============================================================================

echo ">>> Updating system packages..."
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get upgrade -y

echo ">>> Installing dependencies..."
apt-get install -y \
    curl \
    wget \
    jq \
    htop \
    iotop \
    nvme-cli \
    net-tools \
    fail2ban \
    ufw

# =============================================================================
# USER SETUP
# =============================================================================

echo ">>> Creating novacoin user..."
useradd -m -s /bin/bash novacoin || true

# Create directories
mkdir -p /home/novacoin/.novacoin/{data,keys,logs}
mkdir -p /etc/novacoin

# =============================================================================
# DOWNLOAD NOVACOIN BINARY
# =============================================================================

echo ">>> Downloading NovaCoin binary..."

# Install build dependencies
apt-get install -y git build-essential

# Install Go 1.24 from official source
echo ">>> Installing Go 1.24..."
wget -q https://go.dev/dl/go1.24.0.linux-amd64.tar.gz -O /tmp/go1.24.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf /tmp/go1.24.tar.gz
export PATH=/usr/local/go/bin:$$PATH
echo 'export PATH=/usr/local/go/bin:$$PATH' >> /etc/profile.d/go.sh

# Verify Go version
/usr/local/go/bin/go version

# Build from source
cd /tmp
git clone https://github.com/Supernova-NovaCoin/novacoin.git || true
cd novacoin
/usr/local/go/bin/go build -o /usr/local/bin/novacoin ./cmd/novacoin/...
chmod +x /usr/local/bin/novacoin

# =============================================================================
# GENERATE VALIDATOR KEY
# =============================================================================

echo ">>> Generating validator key..."
# Generate BLS key for validator
openssl rand -hex 32 > /home/novacoin/.novacoin/keys/validator.key
chmod 600 /home/novacoin/.novacoin/keys/validator.key

# =============================================================================
# CONFIGURE SYSTEMD SERVICE
# =============================================================================

echo ">>> Creating systemd service..."
cat > /etc/systemd/system/novacoin.service <<EOF
[Unit]
Description=NovaCoin Validator $$VALIDATOR_INDEX
Documentation=https://docs.novacoin.io
After=network-online.target
Wants=network-online.target
ConditionPathExists=/home/novacoin/.novacoin/keys/validator.key

[Service]
Type=simple
User=novacoin
Group=novacoin
WorkingDirectory=/home/novacoin

ExecStartPre=/bin/sh -c 'test -f /home/novacoin/.novacoin/keys/validator.key || exit 1'

ExecStart=/usr/local/bin/novacoin \\
    --datadir /home/novacoin/.novacoin/data \\
    --networkid $${CHAIN_ID} \\
    --validator \\
    --validatorkey /home/novacoin/.novacoin/keys/validator.key \\
    --http \\
    --http.addr 127.0.0.1 \\
    --http.port 8545 \\
    --http.vhosts "localhost" \\
    --ws \\
    --ws.addr 127.0.0.1 \\
    --ws.port 8546 \\
    --ws.origins "localhost" \\
    --port 30303 \\
    --metrics \\
    --metrics.addr 0.0.0.0 \\
    --metrics.port 9090 \\
    --maxpeers 100 \\
    --bootnodes "enode://BOOTNODE@$${BOOTNODE_IP}:30303" \\
    --verbosity 3

Restart=always
RestartSec=5
TimeoutStartSec=180
TimeoutStopSec=120
WatchdogSec=300

LimitNOFILE=131072
LimitNPROC=131072

NoNewPrivileges=true
PrivateTmp=true

StandardOutput=journal
StandardError=journal
SyslogIdentifier=novacoin-validator

MemoryMax=3G
TasksMax=2048
CPUWeight=90
Nice=-10

[Install]
WantedBy=multi-user.target
EOF

# =============================================================================
# CONFIGURE ENVIRONMENT
# =============================================================================

echo ">>> Creating environment file..."
cat > /etc/novacoin/validator.env <<EOF
NETWORK_ID=$${CHAIN_ID}
NETWORK=$${ENVIRONMENT}
ROLE=validator
VALIDATOR_INDEX=$${VALIDATOR_INDEX}
P2P_PORT=30303
HTTP_PORT=8545
WS_PORT=8546
METRICS_PORT=9090
BOOTNODE_IP=$${BOOTNODE_IP}
EOF

# =============================================================================
# SYSTEM TUNING
# =============================================================================

echo ">>> Applying system tuning..."
cat >> /etc/sysctl.conf <<EOF

# NovaCoin Validator Network Tuning
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_fin_timeout = 15
fs.file-max = 2097152
vm.swappiness = 10
vm.dirty_ratio = 10
vm.dirty_background_ratio = 5
EOF
sysctl -p

# Configure limits
cat >> /etc/security/limits.conf <<EOF
novacoin soft nofile 131072
novacoin hard nofile 131072
novacoin soft nproc 131072
novacoin hard nproc 131072
EOF

# =============================================================================
# FIREWALL
# =============================================================================

echo ">>> Configuring firewall..."
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 30303/tcp
ufw allow 30303/udp
ufw allow from 10.0.0.0/16 to any port 8545
ufw allow from 10.0.0.0/16 to any port 8546
ufw allow from 10.0.0.0/16 to any port 9090
ufw --force enable

# =============================================================================
# SET PERMISSIONS
# =============================================================================

echo ">>> Setting permissions..."
chown -R novacoin:novacoin /home/novacoin/.novacoin
chmod 600 /home/novacoin/.novacoin/keys/*

# =============================================================================
# WAIT FOR BOOTNODE
# =============================================================================

echo ">>> Waiting for bootnode to be ready..."
for i in {1..30}; do
    if nc -z $${BOOTNODE_IP} 30303 2>/dev/null; then
        echo "Bootnode is ready!"
        break
    fi
    echo "Waiting for bootnode... ($i/30)"
    sleep 10
done

# =============================================================================
# START SERVICE
# =============================================================================

echo ">>> Starting NovaCoin service..."
systemctl daemon-reload
systemctl enable novacoin
systemctl start novacoin

# =============================================================================
# SAVE NODE INFO
# =============================================================================

echo ">>> Saving node info..."
PRIVATE_IP=$$(curl -s http://169.254.169.254/latest/meta-data/local-ipv4)
PUBLIC_IP=$$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)

cat > /home/novacoin/.novacoin/node-info.json <<EOF
{
  "role": "validator",
  "validator_index": $${VALIDATOR_INDEX},
  "environment": "$${ENVIRONMENT}",
  "chain_id": $${CHAIN_ID},
  "private_ip": "$$PRIVATE_IP",
  "public_ip": "$$PUBLIC_IP",
  "bootnode_ip": "$${BOOTNODE_IP}",
  "p2p_port": 30303,
  "rpc_port": 8545,
  "ws_port": 8546,
  "metrics_port": 9090,
  "initialized_at": "$$(date -Iseconds)"
}
EOF
chown novacoin:novacoin /home/novacoin/.novacoin/node-info.json

echo "=== NovaCoin Validator Init Completed: $(date) ==="
