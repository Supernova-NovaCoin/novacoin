#!/bin/bash
# =============================================================================
# NovaCoin Validator Onboarding Script
# =============================================================================
# Gradual validator onboarding for mainnet launch
# Ensures network stability during initial validator expansion
# =============================================================================

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
VALIDATORS_DIR="${SCRIPT_DIR}/validators"
REGISTRY_FILE="${VALIDATORS_DIR}/registry.json"
QUEUE_FILE="${VALIDATORS_DIR}/activation-queue.json"

# Configuration
MIN_STAKE="32000000000000000000"  # 32 NOVA in wei
MAX_VALIDATORS_PER_EPOCH=10
EPOCH_DURATION_SECONDS=21600  # 6 hours

# =============================================================================
# INITIALIZATION
# =============================================================================

init_validator_system() {
    echo -e "${BLUE}"
    echo "=========================================="
    echo "  Initializing Validator Onboarding System"
    echo "=========================================="
    echo -e "${NC}"

    mkdir -p "${VALIDATORS_DIR}"
    mkdir -p "${VALIDATORS_DIR}/keys"
    mkdir -p "${VALIDATORS_DIR}/deposits"
    mkdir -p "${VALIDATORS_DIR}/logs"

    # Initialize registry if not exists
    if [[ ! -f "${REGISTRY_FILE}" ]]; then
        cat > "${REGISTRY_FILE}" << 'EOF'
{
  "version": "1.0.0",
  "created": "",
  "validators": [],
  "stats": {
    "total_registered": 0,
    "total_active": 0,
    "total_pending": 0,
    "total_exited": 0,
    "total_slashed": 0
  }
}
EOF
        # Update created timestamp
        local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
        if command -v jq &> /dev/null; then
            jq --arg ts "${timestamp}" '.created = $ts' "${REGISTRY_FILE}" > "${REGISTRY_FILE}.tmp"
            mv "${REGISTRY_FILE}.tmp" "${REGISTRY_FILE}"
        fi
    fi

    # Initialize activation queue if not exists
    if [[ ! -f "${QUEUE_FILE}" ]]; then
        cat > "${QUEUE_FILE}" << 'EOF'
{
  "current_epoch": 0,
  "last_activation_epoch": 0,
  "pending_activations": [],
  "pending_exits": []
}
EOF
    fi

    echo -e "${GREEN}Validator system initialized${NC}"
}

# =============================================================================
# VALIDATOR REGISTRATION
# =============================================================================

register_validator() {
    echo -e "${BLUE}"
    echo "=========================================="
    echo "  Register New Validator"
    echo "=========================================="
    echo -e "${NC}"

    # Collect validator information
    read -p "Enter validator public key (hex, 0x prefix): " pubkey
    read -p "Enter withdrawal address (0x...): " withdrawal_address
    read -p "Enter validator name/identifier: " validator_name
    read -p "Enter contact email: " contact_email
    read -p "Enter stake amount in NOVA (minimum 32): " stake_amount

    # Validate public key format
    if [[ ! "${pubkey}" =~ ^0x[a-fA-F0-9]{96}$ ]]; then
        echo -e "${RED}Invalid public key format. Expected 0x + 96 hex characters${NC}"
        exit 1
    fi

    # Validate withdrawal address
    if [[ ! "${withdrawal_address}" =~ ^0x[a-fA-F0-9]{40}$ ]]; then
        echo -e "${RED}Invalid withdrawal address format${NC}"
        exit 1
    fi

    # Validate stake amount
    if [[ ${stake_amount} -lt 32 ]]; then
        echo -e "${RED}Minimum stake is 32 NOVA${NC}"
        exit 1
    fi

    # Generate validator ID
    local validator_id=$(echo -n "${pubkey}${withdrawal_address}" | sha256sum | cut -c1-16)
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Check for duplicate
    if grep -q "\"pubkey\": \"${pubkey}\"" "${REGISTRY_FILE}" 2>/dev/null; then
        echo -e "${RED}Validator with this public key already registered${NC}"
        exit 1
    fi

    echo ""
    echo "Validator Registration Summary:"
    echo "================================"
    echo "ID: ${validator_id}"
    echo "Name: ${validator_name}"
    echo "Public Key: ${pubkey:0:20}...${pubkey: -10}"
    echo "Withdrawal: ${withdrawal_address}"
    echo "Stake: ${stake_amount} NOVA"
    echo ""

    read -p "Confirm registration? (yes/no): " confirm
    if [[ "${confirm}" != "yes" ]]; then
        echo "Registration cancelled"
        exit 0
    fi

    # Create validator record
    local validator_record=$(cat << EOF
{
  "id": "${validator_id}",
  "name": "${validator_name}",
  "pubkey": "${pubkey}",
  "withdrawal_address": "${withdrawal_address}",
  "stake_amount": "${stake_amount}000000000000000000",
  "contact_email": "${contact_email}",
  "status": "pending_deposit",
  "registered_at": "${timestamp}",
  "activated_at": null,
  "exit_requested_at": null,
  "exited_at": null,
  "slashed": false,
  "performance": {
    "blocks_proposed": 0,
    "blocks_missed": 0,
    "attestations": 0,
    "uptime_percentage": 0
  }
}
EOF
)

    # Save validator record
    echo "${validator_record}" > "${VALIDATORS_DIR}/deposits/${validator_id}.json"

    # Generate deposit data
    generate_deposit_data "${validator_id}" "${pubkey}" "${withdrawal_address}" "${stake_amount}"

    echo ""
    echo -e "${GREEN}Validator registered successfully!${NC}"
    echo ""
    echo "Next steps:"
    echo "1. Review deposit data in: ${VALIDATORS_DIR}/deposits/${validator_id}.json"
    echo "2. Submit deposit transaction to staking contract"
    echo "3. Run: $0 verify-deposit ${validator_id}"
}

generate_deposit_data() {
    local validator_id="$1"
    local pubkey="$2"
    local withdrawal_address="$3"
    local stake_amount="$4"

    local deposit_file="${VALIDATORS_DIR}/deposits/${validator_id}-deposit.json"

    # Calculate deposit root (simplified - in production use proper signing)
    local deposit_hash=$(echo -n "${pubkey}${withdrawal_address}${stake_amount}" | sha256sum | awk '{print $1}')

    cat > "${deposit_file}" << EOF
{
  "validator_id": "${validator_id}",
  "pubkey": "${pubkey}",
  "withdrawal_credentials": "${withdrawal_address}",
  "amount": "${stake_amount}000000000000000000",
  "deposit_data_root": "0x${deposit_hash}",
  "signature": "SIGNATURE_REQUIRED",
  "instructions": {
    "contract_address": "0x0000000000000000000000000000000000001000",
    "method": "deposit(bytes,bytes,bytes,bytes32)",
    "note": "Sign this deposit data with your validator key before submitting"
  }
}
EOF

    echo ""
    echo "Deposit data generated: ${deposit_file}"
}

# =============================================================================
# DEPOSIT VERIFICATION
# =============================================================================

verify_deposit() {
    local validator_id="$1"

    echo -e "${BLUE}Verifying deposit for validator: ${validator_id}${NC}"

    local validator_file="${VALIDATORS_DIR}/deposits/${validator_id}.json"

    if [[ ! -f "${validator_file}" ]]; then
        echo -e "${RED}Validator record not found${NC}"
        exit 1
    fi

    # In production, query the staking contract
    echo ""
    echo "Checking deposit status..."
    echo "(In production, this queries the staking contract)"
    echo ""

    read -p "Has the deposit been confirmed on-chain? (yes/no): " confirmed

    if [[ "${confirmed}" == "yes" ]]; then
        # Update validator status
        local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

        if command -v jq &> /dev/null; then
            jq --arg status "pending_activation" \
               --arg ts "${timestamp}" \
               '.status = $status | .deposit_confirmed_at = $ts' \
               "${validator_file}" > "${validator_file}.tmp"
            mv "${validator_file}.tmp" "${validator_file}"
        fi

        # Add to activation queue
        add_to_activation_queue "${validator_id}"

        echo -e "${GREEN}Deposit verified! Validator added to activation queue.${NC}"
        echo ""
        echo "Estimated activation: Next epoch (check queue position)"
    else
        echo -e "${YELLOW}Deposit not yet confirmed. Check transaction status.${NC}"
    fi
}

add_to_activation_queue() {
    local validator_id="$1"
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    if command -v jq &> /dev/null; then
        local entry=$(cat << EOF
{
  "validator_id": "${validator_id}",
  "queued_at": "${timestamp}",
  "priority": "normal"
}
EOF
)
        jq --argjson entry "${entry}" \
           '.pending_activations += [$entry]' \
           "${QUEUE_FILE}" > "${QUEUE_FILE}.tmp"
        mv "${QUEUE_FILE}.tmp" "${QUEUE_FILE}"
    fi
}

# =============================================================================
# ACTIVATION PROCESSING
# =============================================================================

process_activation_queue() {
    echo -e "${BLUE}"
    echo "=========================================="
    echo "  Processing Activation Queue"
    echo "=========================================="
    echo -e "${NC}"

    if ! command -v jq &> /dev/null; then
        echo -e "${RED}jq is required for queue processing${NC}"
        exit 1
    fi

    local pending_count=$(jq '.pending_activations | length' "${QUEUE_FILE}")
    echo "Pending activations: ${pending_count}"

    if [[ ${pending_count} -eq 0 ]]; then
        echo "No validators pending activation"
        return
    fi

    local current_epoch=$(jq '.current_epoch' "${QUEUE_FILE}")
    echo "Current epoch: ${current_epoch}"
    echo "Max activations per epoch: ${MAX_VALIDATORS_PER_EPOCH}"
    echo ""

    # Process up to MAX_VALIDATORS_PER_EPOCH
    local to_activate=${pending_count}
    if [[ ${to_activate} -gt ${MAX_VALIDATORS_PER_EPOCH} ]]; then
        to_activate=${MAX_VALIDATORS_PER_EPOCH}
    fi

    echo "Activating ${to_activate} validators..."
    echo ""

    for ((i=0; i<to_activate; i++)); do
        local validator_id=$(jq -r ".pending_activations[${i}].validator_id" "${QUEUE_FILE}")
        activate_validator "${validator_id}"
    done

    # Remove activated validators from queue
    jq --argjson count "${to_activate}" \
       '.pending_activations = .pending_activations[$count:] | .current_epoch += 1' \
       "${QUEUE_FILE}" > "${QUEUE_FILE}.tmp"
    mv "${QUEUE_FILE}.tmp" "${QUEUE_FILE}"

    echo ""
    echo -e "${GREEN}Activation processing complete${NC}"
}

activate_validator() {
    local validator_id="$1"
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    echo "Activating validator: ${validator_id}"

    local validator_file="${VALIDATORS_DIR}/deposits/${validator_id}.json"

    if [[ -f "${validator_file}" ]] && command -v jq &> /dev/null; then
        jq --arg status "active" \
           --arg ts "${timestamp}" \
           '.status = $status | .activated_at = $ts' \
           "${validator_file}" > "${validator_file}.tmp"
        mv "${validator_file}.tmp" "${validator_file}"

        # Add to registry
        local validator_data=$(cat "${validator_file}")
        jq --argjson v "${validator_data}" \
           '.validators += [$v] | .stats.total_registered += 1 | .stats.total_active += 1' \
           "${REGISTRY_FILE}" > "${REGISTRY_FILE}.tmp"
        mv "${REGISTRY_FILE}.tmp" "${REGISTRY_FILE}"
    fi

    echo -e "  ${GREEN}Activated${NC}: ${validator_id}"
}

# =============================================================================
# VALIDATOR EXIT
# =============================================================================

request_exit() {
    read -p "Enter validator ID to exit: " validator_id

    echo -e "${YELLOW}"
    echo "WARNING: Exiting a validator is a significant action."
    echo "After exit, your stake will be locked for the unbonding period."
    echo -e "${NC}"

    read -p "Type 'I WANT TO EXIT' to confirm: " confirmation
    if [[ "${confirmation}" != "I WANT TO EXIT" ]]; then
        echo "Exit cancelled"
        exit 0
    fi

    local validator_file="${VALIDATORS_DIR}/deposits/${validator_id}.json"
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    if [[ -f "${validator_file}" ]] && command -v jq &> /dev/null; then
        jq --arg status "exit_requested" \
           --arg ts "${timestamp}" \
           '.status = $status | .exit_requested_at = $ts' \
           "${validator_file}" > "${validator_file}.tmp"
        mv "${validator_file}.tmp" "${validator_file}"

        # Add to exit queue
        local entry=$(cat << EOF
{
  "validator_id": "${validator_id}",
  "requested_at": "${timestamp}"
}
EOF
)
        jq --argjson entry "${entry}" \
           '.pending_exits += [$entry]' \
           "${QUEUE_FILE}" > "${QUEUE_FILE}.tmp"
        mv "${QUEUE_FILE}.tmp" "${QUEUE_FILE}"

        echo -e "${GREEN}Exit request submitted${NC}"
        echo "Your validator will exit at the next epoch boundary."
        echo "Unbonding period: 7 days"
    else
        echo -e "${RED}Validator not found or jq not installed${NC}"
        exit 1
    fi
}

# =============================================================================
# STATUS AND REPORTING
# =============================================================================

show_status() {
    echo -e "${BLUE}"
    echo "=========================================="
    echo "  Validator System Status"
    echo "=========================================="
    echo -e "${NC}"

    if ! command -v jq &> /dev/null; then
        echo "Install jq for detailed statistics"
        echo ""
        echo "Registered validators:"
        ls -1 "${VALIDATORS_DIR}/deposits/"*.json 2>/dev/null | wc -l
        return
    fi

    # Registry stats
    echo "Registry Statistics:"
    echo "===================="
    jq -r '.stats | "Total Registered: \(.total_registered)\nActive: \(.total_active)\nPending: \(.total_pending)\nExited: \(.total_exited)\nSlashed: \(.total_slashed)"' "${REGISTRY_FILE}" 2>/dev/null || echo "No registry data"

    echo ""
    echo "Activation Queue:"
    echo "================="
    jq -r '"Current Epoch: \(.current_epoch)\nPending Activations: \(.pending_activations | length)\nPending Exits: \(.pending_exits | length)"' "${QUEUE_FILE}" 2>/dev/null || echo "No queue data"

    echo ""
    echo "Recent Validators:"
    echo "=================="
    for file in "${VALIDATORS_DIR}/deposits/"*.json; do
        if [[ -f "${file}" ]] && [[ "${file}" != *"-deposit.json" ]]; then
            local name=$(jq -r '.name // "Unknown"' "${file}")
            local status=$(jq -r '.status // "unknown"' "${file}")
            local id=$(jq -r '.id // "?"' "${file}")
            printf "  %-16s %-20s %s\n" "${id}" "${name}" "${status}"
        fi
    done 2>/dev/null || echo "  No validators registered"
}

list_validators() {
    echo -e "${BLUE}=== Registered Validators ===${NC}"
    echo ""

    if ! command -v jq &> /dev/null; then
        echo "Install jq for detailed listing"
        return
    fi

    local filter="${1:-all}"

    printf "%-16s %-20s %-20s %-10s\n" "ID" "NAME" "STATUS" "STAKE"
    printf "%-16s %-20s %-20s %-10s\n" "----------------" "--------------------" "--------------------" "----------"

    for file in "${VALIDATORS_DIR}/deposits/"*.json; do
        if [[ -f "${file}" ]] && [[ "${file}" != *"-deposit.json" ]]; then
            local id=$(jq -r '.id // "?"' "${file}")
            local name=$(jq -r '.name // "Unknown"' "${file}" | cut -c1-18)
            local status=$(jq -r '.status // "unknown"' "${file}")
            local stake=$(jq -r '.stake_amount // "0"' "${file}" | cut -c1-5)

            if [[ "${filter}" == "all" ]] || [[ "${status}" == "${filter}" ]]; then
                printf "%-16s %-20s %-20s %-10s\n" "${id}" "${name}" "${status}" "${stake}..."
            fi
        fi
    done 2>/dev/null
}

show_validator() {
    local validator_id="$1"
    local validator_file="${VALIDATORS_DIR}/deposits/${validator_id}.json"

    if [[ ! -f "${validator_file}" ]]; then
        echo -e "${RED}Validator not found: ${validator_id}${NC}"
        exit 1
    fi

    echo -e "${BLUE}=== Validator Details ===${NC}"
    echo ""

    if command -v jq &> /dev/null; then
        jq '.' "${validator_file}"
    else
        cat "${validator_file}"
    fi
}

# =============================================================================
# HARDWARE REQUIREMENTS CHECK
# =============================================================================

check_hardware() {
    echo -e "${BLUE}"
    echo "=========================================="
    echo "  Validator Hardware Requirements Check"
    echo "=========================================="
    echo -e "${NC}"

    local passed=0
    local failed=0

    # CPU check
    local cpu_cores=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo "0")
    if [[ ${cpu_cores} -ge 16 ]]; then
        echo -e "${GREEN}[PASS]${NC} CPU: ${cpu_cores} cores (minimum: 16)"
        ((passed++))
    else
        echo -e "${RED}[FAIL]${NC} CPU: ${cpu_cores} cores (minimum: 16)"
        ((failed++))
    fi

    # RAM check
    local ram_gb
    if [[ -f /proc/meminfo ]]; then
        ram_gb=$(awk '/MemTotal/ {printf "%.0f", $2/1024/1024}' /proc/meminfo)
    else
        ram_gb=$(sysctl -n hw.memsize 2>/dev/null | awk '{printf "%.0f", $1/1024/1024/1024}')
    fi

    if [[ ${ram_gb} -ge 64 ]]; then
        echo -e "${GREEN}[PASS]${NC} RAM: ${ram_gb}GB (minimum: 64GB)"
        ((passed++))
    else
        echo -e "${RED}[FAIL]${NC} RAM: ${ram_gb}GB (minimum: 64GB)"
        ((failed++))
    fi

    # Disk check
    local disk_gb=$(df -BG "${PROJECT_ROOT}" 2>/dev/null | awk 'NR==2{print $4}' | tr -d 'G' || echo "0")
    if [[ ${disk_gb} -ge 4000 ]]; then
        echo -e "${GREEN}[PASS]${NC} Disk: ${disk_gb}GB available (minimum: 4TB)"
        ((passed++))
    elif [[ ${disk_gb} -ge 2000 ]]; then
        echo -e "${YELLOW}[WARN]${NC} Disk: ${disk_gb}GB available (recommended: 4TB)"
        ((passed++))
    else
        echo -e "${RED}[FAIL]${NC} Disk: ${disk_gb}GB available (minimum: 2TB)"
        ((failed++))
    fi

    # Network check (basic)
    if ping -c 1 8.8.8.8 &> /dev/null; then
        echo -e "${GREEN}[PASS]${NC} Network: Connected"
        ((passed++))
    else
        echo -e "${RED}[FAIL]${NC} Network: No connectivity"
        ((failed++))
    fi

    # Port availability
    local ports_open=true
    for port in 30303 8545; do
        if netstat -tuln 2>/dev/null | grep -q ":${port} "; then
            echo -e "${RED}[FAIL]${NC} Port ${port}: In use"
            ports_open=false
            ((failed++))
        else
            echo -e "${GREEN}[PASS]${NC} Port ${port}: Available"
            ((passed++))
        fi
    done

    echo ""
    echo "=========================================="
    echo "Results: ${passed} passed, ${failed} failed"
    echo "=========================================="

    if [[ ${failed} -gt 0 ]]; then
        echo -e "${RED}Hardware requirements not met${NC}"
        return 1
    else
        echo -e "${GREEN}Hardware requirements satisfied${NC}"
        return 0
    fi
}

# =============================================================================
# GENERATE VALIDATOR KEYS
# =============================================================================

generate_keys() {
    echo -e "${BLUE}"
    echo "=========================================="
    echo "  Generate Validator Keys"
    echo "=========================================="
    echo -e "${NC}"

    echo -e "${YELLOW}WARNING: Key generation should be done on an air-gapped machine${NC}"
    echo ""

    read -p "Enter validator name/identifier: " validator_name
    read -p "Enter withdrawal address (0x...): " withdrawal_address

    local key_dir="${VALIDATORS_DIR}/keys/${validator_name}"
    mkdir -p "${key_dir}"

    echo ""
    echo "Generating keys..."

    # Generate BLS key pair (simplified - in production use proper BLS library)
    local private_key=$(openssl rand -hex 32)
    local public_key=$(echo -n "${private_key}" | sha256sum | awk '{print $1}')

    # Save encrypted private key
    echo -e "${YELLOW}Enter a strong password to encrypt your validator key:${NC}"
    read -s password
    echo ""

    # Encrypt with openssl (simplified - in production use proper key derivation)
    echo "${private_key}" | openssl enc -aes-256-cbc -pbkdf2 -salt -pass pass:"${password}" -out "${key_dir}/validator.key.enc"

    # Save public key
    cat > "${key_dir}/validator_pubkey.json" << EOF
{
  "name": "${validator_name}",
  "pubkey": "0x${public_key}${public_key}${public_key:0:32}",
  "withdrawal_address": "${withdrawal_address}",
  "generated_at": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
}
EOF

    echo ""
    echo -e "${GREEN}Keys generated successfully!${NC}"
    echo ""
    echo "Files created:"
    echo "  - ${key_dir}/validator.key.enc (encrypted private key)"
    echo "  - ${key_dir}/validator_pubkey.json (public key info)"
    echo ""
    echo -e "${RED}IMPORTANT:${NC}"
    echo "  1. Back up your encrypted key securely"
    echo "  2. Never share your private key"
    echo "  3. Remember your encryption password"
    echo ""
    echo "To register this validator, run:"
    echo "  $0 register"
    echo "And use pubkey from: ${key_dir}/validator_pubkey.json"
}

# =============================================================================
# MAIN
# =============================================================================

usage() {
    echo "NovaCoin Validator Onboarding System"
    echo ""
    echo "Usage: $0 <command> [options]"
    echo ""
    echo "Commands:"
    echo "  init                Initialize validator system"
    echo "  generate-keys       Generate new validator keys"
    echo "  register            Register a new validator"
    echo "  verify-deposit <id> Verify deposit for validator"
    echo "  process-queue       Process activation queue"
    echo "  request-exit        Request validator exit"
    echo "  status              Show system status"
    echo "  list [filter]       List validators (filter: all/active/pending)"
    echo "  show <id>           Show validator details"
    echo "  check-hardware      Check hardware requirements"
    echo ""
    echo "Examples:"
    echo "  $0 init"
    echo "  $0 generate-keys"
    echo "  $0 register"
    echo "  $0 verify-deposit abc123"
    echo "  $0 list active"
}

main() {
    if [[ $# -lt 1 ]]; then
        usage
        exit 1
    fi

    local command="$1"
    shift

    case "${command}" in
        init)
            init_validator_system
            ;;
        generate-keys)
            generate_keys
            ;;
        register)
            init_validator_system
            register_validator
            ;;
        verify-deposit)
            if [[ $# -lt 1 ]]; then
                echo "Usage: $0 verify-deposit <validator_id>"
                exit 1
            fi
            verify_deposit "$1"
            ;;
        process-queue)
            process_activation_queue
            ;;
        request-exit)
            request_exit
            ;;
        status)
            show_status
            ;;
        list)
            list_validators "${1:-all}"
            ;;
        show)
            if [[ $# -lt 1 ]]; then
                echo "Usage: $0 show <validator_id>"
                exit 1
            fi
            show_validator "$1"
            ;;
        check-hardware)
            check_hardware
            ;;
        *)
            echo "Unknown command: ${command}"
            usage
            exit 1
            ;;
    esac
}

main "$@"
