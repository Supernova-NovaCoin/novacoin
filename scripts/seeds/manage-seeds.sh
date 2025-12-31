#!/bin/bash
#
# NovaCoin Seed Node Manager
# Manage seed nodes for mainnet and testnet
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

# Default values
NETWORK="mainnet"
ACTION=""

# Print banner
print_banner() {
    echo -e "${BLUE}"
    echo "╔═══════════════════════════════════════════════════════════════════╗"
    echo "║                  NovaCoin Seed Node Manager                       ║"
    echo "╚═══════════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"
}

# Usage
usage() {
    echo "Usage: $0 <action> [OPTIONS]"
    echo ""
    echo "Actions:"
    echo "  list                    List all seed nodes"
    echo "  add <enode>             Add a new seed node"
    echo "  remove <enode>          Remove a seed node"
    echo "  check                   Check seed node connectivity"
    echo "  generate                Generate enode for this node"
    echo ""
    echo "Options:"
    echo "  -n, --network NAME      Network (mainnet, testnet) [default: mainnet]"
    echo "  -h, --help              Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 list --network mainnet"
    echo "  $0 add 'enode://abc...@1.2.3.4:30303' --network testnet"
    echo "  $0 check"
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

# Get seeds file for network
get_seeds_file() {
    echo "${SCRIPT_DIR}/seeds-${NETWORK}.txt"
}

# List seed nodes
list_seeds() {
    local seeds_file=$(get_seeds_file)

    if [[ ! -f "${seeds_file}" ]]; then
        log_error "Seeds file not found: ${seeds_file}"
        exit 1
    fi

    echo ""
    echo -e "${BLUE}=== ${NETWORK^} Seed Nodes ===${NC}"
    echo ""

    local count=0
    while IFS= read -r line; do
        # Skip comments and empty lines
        [[ -z "${line}" || "${line}" =~ ^# ]] && continue

        count=$((count + 1))
        # Parse enode
        if [[ "${line}" =~ enode://([a-f0-9]+)@([^:]+):([0-9]+) ]]; then
            local pubkey="${BASH_REMATCH[1]:0:16}..."
            local host="${BASH_REMATCH[2]}"
            local port="${BASH_REMATCH[3]}"
            echo -e "  ${count}. ${GREEN}${host}${NC}:${port}"
            echo -e "     Pubkey: ${pubkey}"
        else
            echo -e "  ${count}. ${line}"
        fi
    done < "${seeds_file}"

    echo ""
    echo "Total: ${count} seed nodes"
}

# Add seed node
add_seed() {
    local enode=$1
    local seeds_file=$(get_seeds_file)

    # Validate enode format
    if [[ ! "${enode}" =~ ^enode://[a-f0-9]{128}@[^:]+:[0-9]+$ ]]; then
        log_error "Invalid enode format"
        echo "Expected: enode://<128-char-pubkey>@<host>:<port>"
        exit 1
    fi

    # Check if already exists
    if grep -q "${enode}" "${seeds_file}" 2>/dev/null; then
        log_warn "Seed node already exists"
        exit 0
    fi

    # Add to file
    echo "" >> "${seeds_file}"
    echo "# Added $(date)" >> "${seeds_file}"
    echo "${enode}" >> "${seeds_file}"

    log_info "Added seed node to ${NETWORK}"
}

# Remove seed node
remove_seed() {
    local enode=$1
    local seeds_file=$(get_seeds_file)
    local temp_file="${seeds_file}.tmp"

    # Check if exists
    if ! grep -q "${enode}" "${seeds_file}" 2>/dev/null; then
        log_error "Seed node not found"
        exit 1
    fi

    # Remove line
    grep -v "${enode}" "${seeds_file}" > "${temp_file}"
    mv "${temp_file}" "${seeds_file}"

    log_info "Removed seed node from ${NETWORK}"
}

# Check seed connectivity
check_seeds() {
    local seeds_file=$(get_seeds_file)

    if [[ ! -f "${seeds_file}" ]]; then
        log_error "Seeds file not found: ${seeds_file}"
        exit 1
    fi

    echo ""
    echo -e "${BLUE}=== Checking ${NETWORK^} Seed Connectivity ===${NC}"
    echo ""

    local online=0
    local offline=0

    while IFS= read -r line; do
        # Skip comments and empty lines
        [[ -z "${line}" || "${line}" =~ ^# ]] && continue

        # Parse host and port
        if [[ "${line}" =~ @([^:]+):([0-9]+) ]]; then
            local host="${BASH_REMATCH[1]}"
            local port="${BASH_REMATCH[2]}"

            # Check connectivity with timeout
            printf "  Checking %s:%s ... " "${host}" "${port}"

            if timeout 5 bash -c "echo >/dev/tcp/${host}/${port}" 2>/dev/null; then
                echo -e "${GREEN}ONLINE${NC}"
                online=$((online + 1))
            else
                echo -e "${RED}OFFLINE${NC}"
                offline=$((offline + 1))
            fi
        fi
    done < "${seeds_file}"

    echo ""
    echo -e "Online: ${GREEN}${online}${NC}  Offline: ${RED}${offline}${NC}"

    if [[ ${offline} -gt 0 ]]; then
        log_warn "Some seed nodes are offline"
        exit 1
    fi
}

# Generate enode for this node
generate_enode() {
    log_info "Generating enode for this node..."

    # Check if novacoin binary exists
    if ! command -v novacoin &> /dev/null; then
        log_error "novacoin binary not found in PATH"
        echo "Build it with: go build ./cmd/novacoin/..."
        exit 1
    fi

    # Generate nodekey if not exists
    local nodekey_file="/tmp/novacoin-nodekey"
    if [[ ! -f "${nodekey_file}" ]]; then
        openssl rand -hex 32 > "${nodekey_file}"
    fi

    # Get public key (placeholder - actual implementation would use proper key derivation)
    local privkey=$(cat "${nodekey_file}")
    local pubkey=$(echo -n "${privkey}" | sha256sum | cut -c 1-64)$(echo -n "${privkey}x" | sha256sum | cut -c 1-64)

    # Get public IP
    local public_ip
    public_ip=$(curl -s ifconfig.me 2>/dev/null || echo "YOUR_PUBLIC_IP")

    echo ""
    echo -e "${GREEN}Your enode:${NC}"
    echo ""
    echo "enode://${pubkey}@${public_ip}:30303"
    echo ""
    echo "Add this to seeds file with:"
    echo "  $0 add 'enode://${pubkey}@${public_ip}:30303' --network ${NETWORK}"
    echo ""

    # Clean up
    rm -f "${nodekey_file}"
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        list|add|remove|check|generate)
            ACTION="$1"
            shift
            # Get argument for add/remove
            if [[ "${ACTION}" == "add" || "${ACTION}" == "remove" ]] && [[ $# -gt 0 && ! "$1" =~ ^- ]]; then
                ENODE="$1"
                shift
            fi
            ;;
        -n|--network)
            NETWORK="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Validate network
if [[ "${NETWORK}" != "mainnet" && "${NETWORK}" != "testnet" ]]; then
    log_error "Invalid network: ${NETWORK}"
    echo "Valid networks: mainnet, testnet"
    exit 1
fi

# Execute action
print_banner

case "${ACTION}" in
    list)
        list_seeds
        ;;
    add)
        if [[ -z "${ENODE:-}" ]]; then
            log_error "Enode required for add action"
            usage
            exit 1
        fi
        add_seed "${ENODE}"
        ;;
    remove)
        if [[ -z "${ENODE:-}" ]]; then
            log_error "Enode required for remove action"
            usage
            exit 1
        fi
        remove_seed "${ENODE}"
        ;;
    check)
        check_seeds
        ;;
    generate)
        generate_enode
        ;;
    *)
        log_error "Action required"
        usage
        exit 1
        ;;
esac
