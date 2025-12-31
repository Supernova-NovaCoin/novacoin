#!/bin/bash
# =============================================================================
# NovaCoin Multi-Sig Deployment Authorization Script
# =============================================================================
# Requires 4 of 7 authorized signers to approve deployment
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MULTISIG_FILE="${SCRIPT_DIR}/.multisig-approvals"
SIGNERS_FILE="${SCRIPT_DIR}/.authorized-signers"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

REQUIRED_SIGNATURES=4
TOTAL_SIGNERS=7

# =============================================================================
# INITIALIZATION
# =============================================================================

init_signers() {
    if [[ -f "${SIGNERS_FILE}" ]]; then
        echo -e "${YELLOW}Signers file already exists${NC}"
        return
    fi

    echo "Initializing authorized signers..."
    echo "Enter the public key hash for each signer (${TOTAL_SIGNERS} signers required):"

    > "${SIGNERS_FILE}"

    for i in $(seq 1 ${TOTAL_SIGNERS}); do
        read -p "Signer ${i} ID (e.g., name or key hash): " signer_id
        read -p "Signer ${i} public key: " public_key
        echo "${signer_id}:${public_key}" >> "${SIGNERS_FILE}"
    done

    chmod 600 "${SIGNERS_FILE}"
    echo -e "${GREEN}Signers initialized. File: ${SIGNERS_FILE}${NC}"
}

# =============================================================================
# APPROVAL FUNCTIONS
# =============================================================================

create_deployment_request() {
    local deployment_id=$(date +%Y%m%d-%H%M%S)-$(openssl rand -hex 4)

    echo -e "${BLUE}"
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║              Creating New Deployment Request                         ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"

    # Collect deployment details
    read -p "Enter deployment description: " description
    read -p "Enter target network (mainnet/testnet): " network
    read -p "Enter git commit hash: " commit_hash

    # Create request file
    cat > "${MULTISIG_FILE}" << EOF
# NovaCoin Multi-Sig Deployment Request
# =====================================
# Deployment ID: ${deployment_id}
# Created: $(date -u +"%Y-%m-%d %H:%M:%S UTC")

DEPLOYMENT_ID=${deployment_id}
DESCRIPTION=${description}
NETWORK=${network}
COMMIT_HASH=${commit_hash}
REQUIRED_SIGNATURES=${REQUIRED_SIGNATURES}
TOTAL_SIGNERS=${TOTAL_SIGNERS}

# Binary checksums
NOVACOIN_SHA256=$(sha256sum "${SCRIPT_DIR}/../../build/novacoin" 2>/dev/null | awk '{print $1}' || echo "NOT_BUILT")

# Approvals (format: APPROVED:<signer_id>:<timestamp>:<signature>)
# ----------------------------------------------------------------
EOF

    chmod 600 "${MULTISIG_FILE}"

    echo ""
    echo -e "${GREEN}Deployment request created: ${deployment_id}${NC}"
    echo ""
    echo "Request file: ${MULTISIG_FILE}"
    echo ""
    echo "Next steps:"
    echo "1. Share the deployment request with all signers"
    echo "2. Each signer runs: $0 approve"
    echo "3. Once ${REQUIRED_SIGNATURES}/${TOTAL_SIGNERS} signatures collected, run: $0 verify"
}

approve_deployment() {
    if [[ ! -f "${MULTISIG_FILE}" ]]; then
        echo -e "${RED}No pending deployment request found${NC}"
        echo "Create one with: $0 request"
        exit 1
    fi

    # Display current request
    echo -e "${BLUE}"
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║              Deployment Request Details                              ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"

    grep "^DEPLOYMENT_ID=" "${MULTISIG_FILE}" | cut -d= -f2
    echo "Description: $(grep "^DESCRIPTION=" "${MULTISIG_FILE}" | cut -d= -f2)"
    echo "Network: $(grep "^NETWORK=" "${MULTISIG_FILE}" | cut -d= -f2)"
    echo "Commit: $(grep "^COMMIT_HASH=" "${MULTISIG_FILE}" | cut -d= -f2)"
    echo "Binary SHA256: $(grep "^NOVACOIN_SHA256=" "${MULTISIG_FILE}" | cut -d= -f2)"
    echo ""

    # Show current approvals
    local current=$(grep -c "^APPROVED:" "${MULTISIG_FILE}" || echo "0")
    echo "Current approvals: ${current}/${REQUIRED_SIGNATURES}"
    echo ""

    # Collect signer info
    read -p "Enter your signer ID: " signer_id

    # Check if already approved
    if grep -q "^APPROVED:${signer_id}:" "${MULTISIG_FILE}"; then
        echo -e "${YELLOW}You have already approved this deployment${NC}"
        exit 0
    fi

    # Verify signer is authorized
    if [[ -f "${SIGNERS_FILE}" ]] && ! grep -q "^${signer_id}:" "${SIGNERS_FILE}"; then
        echo -e "${RED}Signer ID not found in authorized signers${NC}"
        exit 1
    fi

    echo ""
    echo -e "${YELLOW}By approving, you confirm:${NC}"
    echo "  1. You have reviewed the deployment request"
    echo "  2. You have verified the binary checksum"
    echo "  3. You authorize this deployment to proceed"
    echo ""

    read -p "Type 'I APPROVE' to sign: " confirmation
    if [[ "$confirmation" != "I APPROVE" ]]; then
        echo "Approval cancelled"
        exit 1
    fi

    # Generate signature (in production, use hardware signing)
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    local deployment_id=$(grep "^DEPLOYMENT_ID=" "${MULTISIG_FILE}" | cut -d= -f2)
    local message="${deployment_id}:${signer_id}:${timestamp}"

    # Simulate signature (in production, use GPG or hardware wallet)
    local signature=$(echo -n "${message}" | sha256sum | awk '{print $1}')

    # Add approval
    echo "APPROVED:${signer_id}:${timestamp}:${signature}" >> "${MULTISIG_FILE}"

    echo ""
    echo -e "${GREEN}Approval recorded${NC}"
    echo "Signer: ${signer_id}"
    echo "Timestamp: ${timestamp}"
    echo "Signature: ${signature:0:16}..."

    # Check if we have enough signatures
    local new_count=$(grep -c "^APPROVED:" "${MULTISIG_FILE}" || echo "0")
    echo ""
    echo "Total approvals: ${new_count}/${REQUIRED_SIGNATURES}"

    if [[ ${new_count} -ge ${REQUIRED_SIGNATURES} ]]; then
        echo ""
        echo -e "${GREEN}✓ Sufficient approvals collected!${NC}"
        echo "Run '$0 verify' to verify all signatures"
        echo "Then run 'deploy-mainnet.sh' to proceed with deployment"
    fi
}

verify_approvals() {
    if [[ ! -f "${MULTISIG_FILE}" ]]; then
        echo -e "${RED}No deployment request found${NC}"
        exit 1
    fi

    echo -e "${BLUE}"
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║              Verifying Multi-Sig Approvals                           ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"

    local deployment_id=$(grep "^DEPLOYMENT_ID=" "${MULTISIG_FILE}" | cut -d= -f2)
    echo "Deployment ID: ${deployment_id}"
    echo ""

    local valid=0
    local invalid=0

    while IFS=: read -r prefix signer_id timestamp signature; do
        if [[ "${prefix}" != "APPROVED" ]]; then
            continue
        fi

        # Verify signature (simplified - in production, verify cryptographically)
        local expected_message="${deployment_id}:${signer_id}:${timestamp}"
        local expected_sig=$(echo -n "${expected_message}" | sha256sum | awk '{print $1}')

        if [[ "${signature}" == "${expected_sig}" ]]; then
            echo -e "${GREEN}✓${NC} ${signer_id} (${timestamp})"
            ((valid++))
        else
            echo -e "${RED}✗${NC} ${signer_id} - INVALID SIGNATURE"
            ((invalid++))
        fi
    done < "${MULTISIG_FILE}"

    echo ""
    echo "Valid signatures: ${valid}"
    echo "Invalid signatures: ${invalid}"
    echo "Required: ${REQUIRED_SIGNATURES}"
    echo ""

    if [[ ${invalid} -gt 0 ]]; then
        echo -e "${RED}VERIFICATION FAILED: Invalid signatures detected${NC}"
        exit 1
    fi

    if [[ ${valid} -ge ${REQUIRED_SIGNATURES} ]]; then
        echo -e "${GREEN}VERIFICATION PASSED: ${valid}/${REQUIRED_SIGNATURES} valid signatures${NC}"
        echo ""
        echo "Deployment is authorized to proceed."
        exit 0
    else
        echo -e "${YELLOW}INSUFFICIENT SIGNATURES: ${valid}/${REQUIRED_SIGNATURES}${NC}"
        echo ""
        echo "Need $((REQUIRED_SIGNATURES - valid)) more signature(s)"
        exit 1
    fi
}

revoke_approval() {
    if [[ ! -f "${MULTISIG_FILE}" ]]; then
        echo -e "${RED}No deployment request found${NC}"
        exit 1
    fi

    read -p "Enter your signer ID to revoke: " signer_id

    if ! grep -q "^APPROVED:${signer_id}:" "${MULTISIG_FILE}"; then
        echo "No approval found for signer: ${signer_id}"
        exit 1
    fi

    # Remove approval (create temp file without the approval)
    grep -v "^APPROVED:${signer_id}:" "${MULTISIG_FILE}" > "${MULTISIG_FILE}.tmp"
    mv "${MULTISIG_FILE}.tmp" "${MULTISIG_FILE}"

    echo -e "${YELLOW}Approval revoked for: ${signer_id}${NC}"
}

show_status() {
    if [[ ! -f "${MULTISIG_FILE}" ]]; then
        echo "No pending deployment request"
        exit 0
    fi

    echo -e "${BLUE}=== Deployment Request Status ===${NC}"
    echo ""
    echo "Deployment ID: $(grep "^DEPLOYMENT_ID=" "${MULTISIG_FILE}" | cut -d= -f2)"
    echo "Description: $(grep "^DESCRIPTION=" "${MULTISIG_FILE}" | cut -d= -f2)"
    echo "Network: $(grep "^NETWORK=" "${MULTISIG_FILE}" | cut -d= -f2)"
    echo "Commit: $(grep "^COMMIT_HASH=" "${MULTISIG_FILE}" | cut -d= -f2)"
    echo ""

    local count=$(grep -c "^APPROVED:" "${MULTISIG_FILE}" || echo "0")
    echo "Approvals: ${count}/${REQUIRED_SIGNATURES}"
    echo ""

    if [[ ${count} -gt 0 ]]; then
        echo "Signers who approved:"
        grep "^APPROVED:" "${MULTISIG_FILE}" | while IFS=: read -r prefix signer timestamp sig; do
            echo "  - ${signer} (${timestamp})"
        done
    fi

    if [[ ${count} -ge ${REQUIRED_SIGNATURES} ]]; then
        echo ""
        echo -e "${GREEN}✓ Ready for deployment${NC}"
    else
        echo ""
        echo -e "${YELLOW}Waiting for $((REQUIRED_SIGNATURES - count)) more signature(s)${NC}"
    fi
}

cancel_request() {
    if [[ ! -f "${MULTISIG_FILE}" ]]; then
        echo "No pending deployment request"
        exit 0
    fi

    read -p "Are you sure you want to cancel the deployment request? (yes/no): " confirm
    if [[ "${confirm}" == "yes" ]]; then
        rm "${MULTISIG_FILE}"
        echo -e "${GREEN}Deployment request cancelled${NC}"
    else
        echo "Cancelled"
    fi
}

# =============================================================================
# MAIN
# =============================================================================

usage() {
    echo "NovaCoin Multi-Sig Deployment Authorization"
    echo ""
    echo "Usage: $0 <command>"
    echo ""
    echo "Commands:"
    echo "  init      Initialize authorized signers"
    echo "  request   Create new deployment request"
    echo "  approve   Approve pending deployment"
    echo "  revoke    Revoke your approval"
    echo "  verify    Verify all approvals"
    echo "  status    Show current status"
    echo "  cancel    Cancel deployment request"
}

main() {
    if [[ $# -lt 1 ]]; then
        usage
        exit 1
    fi

    case "$1" in
        init)
            init_signers
            ;;
        request)
            create_deployment_request
            ;;
        approve)
            approve_deployment
            ;;
        revoke)
            revoke_approval
            ;;
        verify)
            verify_approvals
            ;;
        status)
            show_status
            ;;
        cancel)
            cancel_request
            ;;
        *)
            echo "Unknown command: $1"
            usage
            exit 1
            ;;
    esac
}

main "$@"
