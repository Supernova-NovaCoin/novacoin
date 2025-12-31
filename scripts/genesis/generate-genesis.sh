#!/bin/bash
#
# NovaCoin Genesis Block Generator
# Generates genesis.json from template with actual addresses and keys
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE_FILE="${SCRIPT_DIR}/genesis-template.json"

# Default values
NETWORK="mainnet"
CHAIN_ID=1
OUTPUT_DIR="${SCRIPT_DIR}/../../genesis"
NUM_VALIDATORS=4

# Print banner
print_banner() {
    echo -e "${BLUE}"
    echo "╔═══════════════════════════════════════════════════════════════════╗"
    echo "║                  NovaCoin Genesis Generator                       ║"
    echo "╚═══════════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"
}

# Print usage
usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -n, --network NAME      Network name (mainnet, testnet, devnet) [default: mainnet]"
    echo "  -c, --chain-id ID       Chain ID [default: 1]"
    echo "  -v, --validators NUM    Number of initial validators [default: 4]"
    echo "  -o, --output DIR        Output directory [default: ../../genesis]"
    echo "  -h, --help              Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 --network testnet --chain-id 2 --validators 4"
    echo "  $0 -n devnet -c 1337 -v 1"
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

# Generate a new keypair
generate_keypair() {
    local name=$1
    local output_dir=$2

    log_info "Generating keypair for ${name}..."

    # Create directory
    mkdir -p "${output_dir}/keys"

    # Generate private key (32 bytes hex)
    local privkey=$(openssl rand -hex 32)

    # For real implementation, derive public key and address from private key
    # This is a placeholder - in production, use proper ECDSA/BLS derivation
    local pubkey="0x04$(openssl rand -hex 64)"
    local address="0x$(echo -n "${privkey}" | sha256sum | cut -c 1-40)"

    # Save private key securely
    echo "${privkey}" > "${output_dir}/keys/${name}.key"
    chmod 600 "${output_dir}/keys/${name}.key"

    # Return address and pubkey
    echo "${address}|${pubkey}"
}

# Generate BLS keypair for validator
generate_validator_key() {
    local index=$1
    local output_dir=$2

    log_info "Generating validator key ${index}..."

    mkdir -p "${output_dir}/validators"

    # Generate BLS private key (placeholder - use proper BLS in production)
    local privkey=$(openssl rand -hex 32)
    local pubkey="0x$(openssl rand -hex 48)"  # BLS public key is 48 bytes
    local address="0x$(echo -n "${privkey}" | sha256sum | cut -c 1-40)"

    # Save validator key
    echo "${privkey}" > "${output_dir}/validators/validator_${index}.key"
    chmod 600 "${output_dir}/validators/validator_${index}.key"

    echo "${address}|${pubkey}"
}

# Main genesis generation
generate_genesis() {
    local output_dir=$1
    local network=$2
    local chain_id=$3
    local num_validators=$4

    log_info "Generating genesis for ${network} (Chain ID: ${chain_id})"

    # Create output directory
    mkdir -p "${output_dir}"

    # Generate timestamp
    local timestamp=$(date +%s)
    local timestamp_hex="0x$(printf '%x' ${timestamp})"

    # Read template
    local genesis=$(cat "${TEMPLATE_FILE}")

    # Update basic config
    genesis=$(echo "${genesis}" | jq --arg ts "${timestamp_hex}" '.timestamp = $ts')
    genesis=$(echo "${genesis}" | jq --argjson cid "${chain_id}" '.config.chainId = $cid')
    genesis=$(echo "${genesis}" | jq --arg name "${network^} Network" '.config.networkName = $name')

    # Generate allocation addresses
    log_info "Generating allocation addresses..."

    local foundation=$(generate_keypair "foundation" "${output_dir}")
    local foundation_addr=$(echo "${foundation}" | cut -d'|' -f1)

    local ecosystem=$(generate_keypair "ecosystem" "${output_dir}")
    local ecosystem_addr=$(echo "${ecosystem}" | cut -d'|' -f1)

    local community=$(generate_keypair "community" "${output_dir}")
    local community_addr=$(echo "${community}" | cut -d'|' -f1)

    local team=$(generate_keypair "team_vesting" "${output_dir}")
    local team_addr=$(echo "${team}" | cut -d'|' -f1)

    local public=$(generate_keypair "public_launch" "${output_dir}")
    local public_addr=$(echo "${public}" | cut -d'|' -f1)

    # Update allocations in genesis
    local alloc_json=$(cat <<EOF
{
    "${foundation_addr}": {
        "balance": "150000000000000000000000000",
        "comment": "Foundation Reserve (15%)"
    },
    "${ecosystem_addr}": {
        "balance": "250000000000000000000000000",
        "comment": "Ecosystem Development (25%)"
    },
    "${community_addr}": {
        "balance": "400000000000000000000000000",
        "comment": "Community & Staking Rewards (40%)"
    },
    "${team_addr}": {
        "balance": "120000000000000000000000000",
        "comment": "Core Contributors (12%) - 4-year vesting"
    },
    "${public_addr}": {
        "balance": "80000000000000000000000000",
        "comment": "Public Launch (8%)"
    }
}
EOF
)
    genesis=$(echo "${genesis}" | jq --argjson alloc "${alloc_json}" '.alloc = $alloc')

    # Generate validators
    log_info "Generating ${num_validators} validator keys..."
    local validators="[]"

    for i in $(seq 1 ${num_validators}); do
        local val_data=$(generate_validator_key "${i}" "${output_dir}")
        local val_addr=$(echo "${val_data}" | cut -d'|' -f1)
        local val_pubkey=$(echo "${val_data}" | cut -d'|' -f2)

        local val_json=$(cat <<EOF
{
    "address": "${val_addr}",
    "pubkey": "${val_pubkey}",
    "stake": "1000000000000000000000000",
    "name": "Genesis Validator ${i}"
}
EOF
)
        validators=$(echo "${validators}" | jq --argjson val "${val_json}" '. + [$val]')
    done

    genesis=$(echo "${genesis}" | jq --argjson vals "${validators}" '.validators = $vals')

    # Generate seed nodes placeholder
    local seeds='["enode://SEED1@seed1.novacoin.io:30303", "enode://SEED2@seed2.novacoin.io:30303", "enode://SEED3@seed3.novacoin.io:30303"]'
    genesis=$(echo "${genesis}" | jq --argjson seeds "${seeds}" '.seeds = $seeds')

    # Write genesis file
    local genesis_file="${output_dir}/genesis.json"
    echo "${genesis}" | jq '.' > "${genesis_file}"

    log_info "Genesis file created: ${genesis_file}"

    # Create address summary
    cat > "${output_dir}/addresses.txt" <<EOF
NovaCoin Genesis Addresses
Generated: $(date)
Network: ${network}
Chain ID: ${chain_id}

=== Token Allocations ===
Foundation (15%):    ${foundation_addr}
Ecosystem (25%):     ${ecosystem_addr}
Community (40%):     ${community_addr}
Team Vesting (12%):  ${team_addr}
Public Launch (8%):  ${public_addr}

=== Initial Validators ===
$(for i in $(seq 1 ${num_validators}); do
    echo "Validator ${i}: $(cat "${output_dir}/validators/validator_${i}.key" 2>/dev/null | sha256sum | cut -c 1-40 | sed 's/^/0x/')"
done)

IMPORTANT: Secure all private keys in ${output_dir}/keys/ and ${output_dir}/validators/
EOF

    log_info "Address summary: ${output_dir}/addresses.txt"

    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                    Genesis Generation Complete                     ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${YELLOW}SECURITY NOTICE:${NC}"
    echo "  - Private keys are stored in: ${output_dir}/keys/"
    echo "  - Validator keys are stored in: ${output_dir}/validators/"
    echo "  - Secure these files immediately!"
    echo "  - Never commit private keys to version control!"
    echo ""
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--network)
            NETWORK="$2"
            shift 2
            ;;
        -c|--chain-id)
            CHAIN_ID="$2"
            shift 2
            ;;
        -v|--validators)
            NUM_VALIDATORS="$2"
            shift 2
            ;;
        -o|--output)
            OUTPUT_DIR="$2"
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
case "${NETWORK}" in
    mainnet)
        CHAIN_ID=${CHAIN_ID:-1}
        ;;
    testnet)
        CHAIN_ID=${CHAIN_ID:-2}
        ;;
    devnet)
        CHAIN_ID=${CHAIN_ID:-1337}
        ;;
    *)
        log_warn "Custom network: ${NETWORK}"
        ;;
esac

# Check dependencies
if ! command -v jq &> /dev/null; then
    log_error "jq is required but not installed. Install with: brew install jq"
    exit 1
fi

if ! command -v openssl &> /dev/null; then
    log_error "openssl is required but not installed."
    exit 1
fi

# Run
print_banner
generate_genesis "${OUTPUT_DIR}/${NETWORK}" "${NETWORK}" "${CHAIN_ID}" "${NUM_VALIDATORS}"
