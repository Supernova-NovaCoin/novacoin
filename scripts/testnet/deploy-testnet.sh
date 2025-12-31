#!/bin/bash
#
# NovaCoin Testnet Deployment Script
# Deploy a complete testnet environment
#

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/../.."

# Default configuration
CHAIN_ID=2
NUM_VALIDATORS=4
DATA_DIR="${ROOT_DIR}/testnet-data"
DOCKER_COMPOSE="${SCRIPT_DIR}/../docker/docker-compose.yml"

# Print banner
print_banner() {
    echo -e "${BLUE}"
    echo "╔═══════════════════════════════════════════════════════════════════╗"
    echo "║                  NovaCoin Testnet Deployment                      ║"
    echo "╚═══════════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"
}

# Log functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_step() {
    echo ""
    echo -e "${BLUE}=== $1 ===${NC}"
}

# Check prerequisites
check_prerequisites() {
    log_step "Checking Prerequisites"

    local missing=0

    # Check Docker
    if ! command -v docker &> /dev/null; then
        log_error "Docker is not installed"
        missing=1
    else
        log_info "Docker: $(docker --version | cut -d' ' -f3)"
    fi

    # Check Docker Compose
    if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
        log_error "Docker Compose is not installed"
        missing=1
    else
        log_info "Docker Compose: Available"
    fi

    # Check jq
    if ! command -v jq &> /dev/null; then
        log_error "jq is not installed"
        missing=1
    else
        log_info "jq: $(jq --version)"
    fi

    # Check if genesis script exists
    if [[ ! -f "${SCRIPT_DIR}/../genesis/generate-genesis.sh" ]]; then
        log_error "Genesis script not found"
        missing=1
    else
        log_info "Genesis script: Found"
    fi

    if [[ ${missing} -ne 0 ]]; then
        log_error "Missing prerequisites. Please install them first."
        exit 1
    fi
}

# Generate genesis block
generate_genesis() {
    log_step "Generating Testnet Genesis"

    "${SCRIPT_DIR}/../genesis/generate-genesis.sh" \
        --network testnet \
        --chain-id ${CHAIN_ID} \
        --validators ${NUM_VALIDATORS} \
        --output "${DATA_DIR}/genesis"

    log_info "Genesis generated at: ${DATA_DIR}/genesis/testnet/genesis.json"
}

# Build Docker images
build_images() {
    log_step "Building Docker Images"

    cd "${ROOT_DIR}"

    log_info "Building node image..."
    docker build -f scripts/docker/Dockerfile -t novacoin/node:testnet .

    log_info "Building validator image..."
    docker build -f scripts/docker/Dockerfile.validator -t novacoin/validator:testnet .
}

# Setup testnet directories
setup_directories() {
    log_step "Setting Up Directories"

    mkdir -p "${DATA_DIR}/node"
    mkdir -p "${DATA_DIR}/validators"

    # Copy genesis to node data
    cp "${DATA_DIR}/genesis/testnet/genesis.json" "${DATA_DIR}/node/"

    # Setup validator directories
    for i in $(seq 1 ${NUM_VALIDATORS}); do
        mkdir -p "${DATA_DIR}/validators/validator${i}/data"
        mkdir -p "${DATA_DIR}/validators/validator${i}/keys"

        # Copy genesis
        cp "${DATA_DIR}/genesis/testnet/genesis.json" "${DATA_DIR}/validators/validator${i}/data/"

        # Copy validator key
        if [[ -f "${DATA_DIR}/genesis/testnet/validators/validator_${i}.key" ]]; then
            cp "${DATA_DIR}/genesis/testnet/validators/validator_${i}.key" \
               "${DATA_DIR}/validators/validator${i}/keys/validator.key"
            chmod 600 "${DATA_DIR}/validators/validator${i}/keys/validator.key"
        fi
    done

    log_info "Directories created at: ${DATA_DIR}"
}

# Create docker-compose override for testnet
create_compose_override() {
    log_step "Creating Docker Compose Override"

    cat > "${DATA_DIR}/docker-compose.override.yml" <<EOF
version: '3.8'

services:
  novacoin-node:
    image: novacoin/node:testnet
    volumes:
      - ${DATA_DIR}/node:/home/novacoin/.novacoin/data
    environment:
      - NOVACOIN_NETWORK=testnet
    command: >
      --datadir /home/novacoin/.novacoin/data
      --networkid ${CHAIN_ID}
      --http
      --http.addr 0.0.0.0
      --http.port 8545
      --http.vhosts "*"
      --http.corsdomain "*"
      --http.api "eth,net,web3,nova"
      --ws
      --ws.addr 0.0.0.0
      --ws.port 8546
      --ws.origins "*"
      --port 30303
      --metrics
      --verbosity 4

EOF

    # Add validators
    for i in $(seq 1 ${NUM_VALIDATORS}); do
        local port_offset=$((i * 10))
        cat >> "${DATA_DIR}/docker-compose.override.yml" <<EOF
  novacoin-validator-${i}:
    image: novacoin/validator:testnet
    container_name: novacoin-testnet-validator-${i}
    volumes:
      - ${DATA_DIR}/validators/validator${i}/data:/home/novacoin/.novacoin/data
      - ${DATA_DIR}/validators/validator${i}/keys:/home/novacoin/.novacoin/keys:ro
    environment:
      - NOVACOIN_NETWORK=testnet
      - NOVACOIN_ROLE=validator
    ports:
      - "$((30303 + port_offset)):30303/tcp"
      - "$((30303 + port_offset)):30303/udp"
      - "$((8545 + port_offset)):8545/tcp"
      - "$((9090 + port_offset)):9090/tcp"
    networks:
      - novacoin-network
    depends_on:
      - novacoin-node
    command: >
      --datadir /home/novacoin/.novacoin/data
      --networkid ${CHAIN_ID}
      --validator
      --validatorkey /home/novacoin/.novacoin/keys/validator.key
      --http
      --http.addr 0.0.0.0
      --http.port 8545
      --ws
      --ws.addr 0.0.0.0
      --ws.port 8546
      --port 30303
      --metrics
      --bootnodes "enode://BOOTNODE@novacoin-node:30303"
      --verbosity 4

EOF
    done

    log_info "Override file created: ${DATA_DIR}/docker-compose.override.yml"
}

# Start testnet
start_testnet() {
    log_step "Starting Testnet"

    cd "${SCRIPT_DIR}/../docker"

    # Export environment for docker-compose
    export NETWORK=testnet
    export COMPOSE_PROJECT_NAME=novacoin-testnet

    # Start services
    docker-compose \
        -f docker-compose.yml \
        -f "${DATA_DIR}/docker-compose.override.yml" \
        up -d novacoin-node

    log_info "Waiting for node to start..."
    sleep 10

    # Start validators
    for i in $(seq 1 ${NUM_VALIDATORS}); do
        docker-compose \
            -f docker-compose.yml \
            -f "${DATA_DIR}/docker-compose.override.yml" \
            up -d "novacoin-validator-${i}"
        sleep 2
    done

    log_info "Testnet started!"
}

# Show status
show_status() {
    log_step "Testnet Status"

    echo ""
    echo "Services:"
    docker ps --filter "name=novacoin-testnet" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

    echo ""
    echo "RPC Endpoints:"
    echo "  Node:       http://localhost:8545"
    for i in $(seq 1 ${NUM_VALIDATORS}); do
        local port_offset=$((i * 10))
        echo "  Validator ${i}: http://localhost:$((8545 + port_offset))"
    done

    echo ""
    echo "Commands:"
    echo "  View logs:  docker logs -f novacoin-testnet-node"
    echo "  Stop:       $0 stop"
    echo "  Reset:      ${SCRIPT_DIR}/reset-testnet.sh"
}

# Stop testnet
stop_testnet() {
    log_step "Stopping Testnet"

    cd "${SCRIPT_DIR}/../docker"

    export COMPOSE_PROJECT_NAME=novacoin-testnet

    docker-compose \
        -f docker-compose.yml \
        -f "${DATA_DIR}/docker-compose.override.yml" \
        down

    log_info "Testnet stopped"
}

# Main function
main() {
    local action="${1:-deploy}"

    print_banner

    case "${action}" in
        deploy)
            check_prerequisites
            generate_genesis
            build_images
            setup_directories
            create_compose_override
            start_testnet
            show_status
            ;;
        start)
            start_testnet
            show_status
            ;;
        stop)
            stop_testnet
            ;;
        status)
            show_status
            ;;
        *)
            echo "Usage: $0 [deploy|start|stop|status]"
            exit 1
            ;;
    esac
}

main "$@"
