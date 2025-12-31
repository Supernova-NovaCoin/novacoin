#!/bin/bash
# NovaCoin RPC Node Initialization Script
# This script is executed via cloud-init on first boot

set -euo pipefail

exec > >(tee /var/log/novacoin-init.log) 2>&1
echo "=== NovaCoin RPC Node Init Started: $$(date) ==="

# Variables from Terraform
CHAIN_ID="${chain_id}"
ENVIRONMENT="${environment}"
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
    ufw \
    nginx

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
# CONFIGURE SYSTEMD SERVICE
# =============================================================================

echo ">>> Creating systemd service..."
cat > /etc/systemd/system/novacoin.service <<EOF
[Unit]
Description=NovaCoin RPC Node
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
    --http.api "eth,net,web3,nova,txpool,debug" \\
    --ws \\
    --ws.addr 0.0.0.0 \\
    --ws.port 8546 \\
    --ws.origins "*" \\
    --ws.api "eth,net,web3,nova" \\
    --port 30303 \\
    --metrics \\
    --metrics.addr 0.0.0.0 \\
    --metrics.port 9090 \\
    --maxpeers 200 \\
    --bootnodes "enode://BOOTNODE@$${BOOTNODE_IP}:30303" \\
    --cache 4096 \\
    --txpool.slots 8192 \\
    --rpc.txfeecap 0 \\
    --verbosity 3

Restart=always
RestartSec=10
TimeoutStartSec=120
TimeoutStopSec=60

LimitNOFILE=131072
LimitNPROC=131072

NoNewPrivileges=true
PrivateTmp=true

StandardOutput=journal
StandardError=journal
SyslogIdentifier=novacoin-rpc

MemoryMax=1500M
TasksMax=1024

[Install]
WantedBy=multi-user.target
EOF

# =============================================================================
# CONFIGURE NGINX REVERSE PROXY
# =============================================================================

echo ">>> Configuring nginx..."
cat > /etc/nginx/sites-available/novacoin-rpc <<EOF
upstream novacoin_http {
    server 127.0.0.1:8545;
    keepalive 64;
}

upstream novacoin_ws {
    server 127.0.0.1:8546;
    keepalive 64;
}

server {
    listen 80;
    server_name _;

    # Health check
    location /health {
        return 200 'OK';
        add_header Content-Type text/plain;
    }

    # HTTP RPC
    location / {
        proxy_pass http://novacoin_http;
        proxy_http_version 1.1;
        proxy_set_header Host $$host;
        proxy_set_header X-Real-IP $$remote_addr;
        proxy_set_header X-Forwarded-For $$proxy_add_x_forwarded_for;
        proxy_set_header Connection "";

        # Rate limiting
        limit_req zone=rpc burst=50 nodelay;
        limit_req_status 429;
    }
}

# Rate limiting zone
limit_req_zone $$binary_remote_addr zone=rpc:10m rate=100r/s;
EOF

ln -sf /etc/nginx/sites-available/novacoin-rpc /etc/nginx/sites-enabled/
rm -f /etc/nginx/sites-enabled/default
nginx -t && systemctl restart nginx

# =============================================================================
# CONFIGURE ENVIRONMENT
# =============================================================================

echo ">>> Creating environment file..."
cat > /etc/novacoin/node.env <<EOF
NETWORK_ID=$${CHAIN_ID}
NETWORK=$${ENVIRONMENT}
ROLE=rpc
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

# NovaCoin RPC Network Tuning
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_keepalive_time = 300
net.ipv4.tcp_keepalive_intvl = 60
net.ipv4.tcp_keepalive_probes = 5
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
www-data soft nofile 65535
www-data hard nofile 65535
EOF

# =============================================================================
# FIREWALL
# =============================================================================

echo ">>> Configuring firewall..."
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 30303/tcp
ufw allow 30303/udp
ufw allow 8545/tcp
ufw allow 8546/tcp
ufw allow from 10.0.0.0/16 to any port 9090
ufw --force enable

# =============================================================================
# SET PERMISSIONS
# =============================================================================

echo ">>> Setting permissions..."
chown -R novacoin:novacoin /home/novacoin/.novacoin

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
  "role": "rpc",
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

echo "=== NovaCoin RPC Node Init Completed: $$(date) ==="
