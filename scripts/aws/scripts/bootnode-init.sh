#!/bin/bash
# NovaCoin Bootnode Initialization Script
# This script is executed via cloud-init on first boot

set -euo pipefail

exec > >(tee /var/log/novacoin-init.log) 2>&1
echo "=== NovaCoin Bootnode Init Started: $(date) ==="

# Variables from Terraform
CHAIN_ID="${chain_id}"
ENVIRONMENT="${environment}"

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
NOVACOIN_VERSION="latest"

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
# GENERATE NODE KEY
# =============================================================================

echo ">>> Generating node key..."
# Generate a simple node key for bootnode identification
openssl rand -hex 32 > /home/novacoin/.novacoin/keys/nodekey

# =============================================================================
# CONFIGURE SYSTEMD SERVICE
# =============================================================================

echo ">>> Creating systemd service..."
cat > /etc/systemd/system/novacoin.service <<EOF
[Unit]
Description=NovaCoin Bootnode
Documentation=https://docs.novacoin.io
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=novacoin
Group=novacoin
WorkingDirectory=/home/novacoin

ExecStart=/usr/local/bin/novacoin \\
    --datadir /home/novacoin/.novacoin/data \\
    --networkid $${CHAIN_ID} \\
    --http \\
    --http.addr 0.0.0.0 \\
    --http.port 8545 \\
    --http.vhosts "*" \\
    --http.corsdomain "*" \\
    --http.api "eth,net,web3,nova" \\
    --ws \\
    --ws.addr 0.0.0.0 \\
    --ws.port 8546 \\
    --ws.origins "*" \\
    --port 30303 \\
    --metrics \\
    --metrics.addr 0.0.0.0 \\
    --metrics.port 9090 \\
    --maxpeers 100 \\
    --verbosity 3

Restart=always
RestartSec=10
TimeoutStartSec=120
TimeoutStopSec=60

LimitNOFILE=65535
LimitNPROC=65535

NoNewPrivileges=true
PrivateTmp=true

StandardOutput=journal
StandardError=journal
SyslogIdentifier=novacoin-bootnode

MemoryMax=1500M
TasksMax=1024

[Install]
WantedBy=multi-user.target
EOF

# =============================================================================
# CONFIGURE ENVIRONMENT
# =============================================================================

echo ">>> Creating environment file..."
cat > /etc/novacoin/node.env <<EOF
NETWORK_ID=$${CHAIN_ID}
NETWORK=$${ENVIRONMENT}
ROLE=bootnode
P2P_PORT=30303
HTTP_PORT=8545
WS_PORT=8546
METRICS_PORT=9090
EOF

# =============================================================================
# SYSTEM TUNING
# =============================================================================

echo ">>> Applying system tuning..."
cat >> /etc/sysctl.conf <<EOF

# NovaCoin Network Tuning
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_fin_timeout = 15
fs.file-max = 2097152
vm.swappiness = 10
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
ufw allow 8545/tcp
ufw allow 8546/tcp
ufw allow 9090/tcp
ufw --force enable

# =============================================================================
# SET PERMISSIONS
# =============================================================================

echo ">>> Setting permissions..."
chown -R novacoin:novacoin /home/novacoin/.novacoin
chmod 600 /home/novacoin/.novacoin/keys/*

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
  "role": "bootnode",
  "environment": "$${ENVIRONMENT}",
  "chain_id": $${CHAIN_ID},
  "private_ip": "$$PRIVATE_IP",
  "public_ip": "$$PUBLIC_IP",
  "p2p_port": 30303,
  "rpc_port": 8545,
  "ws_port": 8546,
  "metrics_port": 9090,
  "initialized_at": "$$(date -Iseconds)"
}
EOF
chown novacoin:novacoin /home/novacoin/.novacoin/node-info.json

echo "=== NovaCoin Bootnode Init Completed: $(date) ==="
