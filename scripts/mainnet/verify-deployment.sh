#!/bin/bash
# =============================================================================
# NovaCoin Deployment Verification Script
# =============================================================================
# Verifies that all deployment prerequisites and configurations are correct
# =============================================================================

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

PASSED=0
FAILED=0
WARNINGS=0

# =============================================================================
# TEST FUNCTIONS
# =============================================================================

test_pass() {
    echo -e "${GREEN}✓${NC} $1"
    ((PASSED++))
}

test_fail() {
    echo -e "${RED}✗${NC} $1"
    ((FAILED++))
}

test_warn() {
    echo -e "${YELLOW}⚠${NC} $1"
    ((WARNINGS++))
}

# =============================================================================
# VERIFICATION CHECKS
# =============================================================================

verify_binaries() {
    echo ""
    echo "=== Binary Verification ==="

    local binary="${PROJECT_ROOT}/build/novacoin"

    if [[ -f "${binary}" ]]; then
        test_pass "NovaCoin binary exists"

        # Check if executable
        if [[ -x "${binary}" ]]; then
            test_pass "Binary is executable"
        else
            test_fail "Binary is not executable"
        fi

        # Verify checksum
        if [[ -f "${binary}.sha256" ]]; then
            local expected=$(cat "${binary}.sha256")
            local actual=$(sha256sum "${binary}" | awk '{print $1}')
            if [[ "${expected}" == "${actual}" ]]; then
                test_pass "Binary checksum verified"
            else
                test_fail "Binary checksum mismatch"
            fi
        else
            test_warn "No checksum file found"
        fi

        # Check binary version
        local version=$("${binary}" version 2>/dev/null || echo "unknown")
        if [[ "${version}" != "unknown" ]]; then
            test_pass "Binary version: ${version}"
        else
            test_warn "Could not determine binary version"
        fi
    else
        test_fail "NovaCoin binary not found at ${binary}"
    fi
}

verify_configuration() {
    echo ""
    echo "=== Configuration Verification ==="

    local config="${SCRIPT_DIR}/mainnet-config.yaml"

    if [[ -f "${config}" ]]; then
        test_pass "Mainnet config exists"

        # Check required fields
        if grep -q "chain_id: 1" "${config}"; then
            test_pass "Chain ID is 1 (mainnet)"
        else
            test_fail "Chain ID is not set to 1"
        fi

        if grep -q "min_stake:" "${config}"; then
            test_pass "Minimum stake configured"
        else
            test_fail "Minimum stake not configured"
        fi

        if grep -q "genesis_timestamp:" "${config}"; then
            test_pass "Genesis timestamp field present"
        else
            test_warn "Genesis timestamp not set"
        fi
    else
        test_fail "Mainnet config not found"
    fi
}

verify_genesis() {
    echo ""
    echo "=== Genesis Verification ==="

    local genesis="${SCRIPT_DIR}/genesis/genesis.json"

    if [[ -f "${genesis}" ]]; then
        test_pass "Genesis file exists"

        # Validate JSON
        if jq empty "${genesis}" 2>/dev/null; then
            test_pass "Genesis JSON is valid"
        else
            test_fail "Genesis JSON is invalid"
        fi

        # Check chain ID
        local chain_id=$(jq -r '.config.chainId' "${genesis}" 2>/dev/null)
        if [[ "${chain_id}" == "1" ]]; then
            test_pass "Genesis chain ID is 1"
        else
            test_fail "Genesis chain ID is not 1: ${chain_id}"
        fi

        # Check allocations
        local alloc_count=$(jq '.alloc | length' "${genesis}" 2>/dev/null)
        if [[ "${alloc_count}" -gt 0 ]]; then
            test_pass "Genesis has ${alloc_count} allocations"
        else
            test_warn "Genesis has no allocations (addresses need to be added)"
        fi
    else
        test_warn "Genesis file not yet generated"
    fi
}

verify_security() {
    echo ""
    echo "=== Security Verification ==="

    local checklist="${SCRIPT_DIR}/SECURITY-CHECKLIST.md"

    if [[ -f "${checklist}" ]]; then
        test_pass "Security checklist exists"

        local unchecked=$(grep -c "\[ \]" "${checklist}" || echo "0")
        local checked=$(grep -c "\[x\]" "${checklist}" || echo "0")

        if [[ ${unchecked} -eq 0 ]]; then
            test_pass "All security items checked (${checked} items)"
        else
            test_fail "${unchecked} security items not completed"
        fi
    else
        test_fail "Security checklist not found"
    fi

    # Check for sensitive files
    if [[ -f "${SCRIPT_DIR}/.env" ]]; then
        test_warn "Found .env file - ensure no secrets are committed"
    fi

    if find "${SCRIPT_DIR}" -name "*.key" -o -name "*.pem" 2>/dev/null | grep -q .; then
        test_warn "Found key/pem files - ensure they are not committed"
    fi
}

verify_multisig() {
    echo ""
    echo "=== Multi-Sig Verification ==="

    local multisig="${SCRIPT_DIR}/.multisig-approvals"

    if [[ -f "${multisig}" ]]; then
        local approvals=$(grep -c "^APPROVED:" "${multisig}" || echo "0")

        if [[ ${approvals} -ge 4 ]]; then
            test_pass "Multi-sig approvals: ${approvals}/7 (minimum 4 required)"
        else
            test_fail "Insufficient multi-sig approvals: ${approvals}/7"
        fi
    else
        test_warn "Multi-sig approval file not found"
    fi
}

verify_network() {
    echo ""
    echo "=== Network Verification ==="

    # Check if ports are available
    for port in 30303 8545 8546 9090; do
        if ! netstat -tuln 2>/dev/null | grep -q ":${port} "; then
            test_pass "Port ${port} is available"
        else
            test_fail "Port ${port} is in use"
        fi
    done

    # Check DNS (if configured)
    local domains=("rpc.novacoin.io" "ws.novacoin.io" "explorer.novacoin.io")
    for domain in "${domains[@]}"; do
        if host "${domain}" >/dev/null 2>&1; then
            test_pass "DNS resolves: ${domain}"
        else
            test_warn "DNS not configured: ${domain}"
        fi
    done
}

verify_validators() {
    echo ""
    echo "=== Validator Verification ==="

    local validator_config="${SCRIPT_DIR}/validators/genesis-validators.yaml"

    if [[ -f "${validator_config}" ]]; then
        test_pass "Validator configuration exists"

        local count=$(grep "count:" "${validator_config}" | awk '{print $2}')
        if [[ "${count}" -ge 100 ]]; then
            test_pass "Genesis validators: ${count} (minimum 100)"
        else
            test_warn "Genesis validators: ${count} (need minimum 100)"
        fi
    else
        test_warn "Validator configuration not found"
    fi
}

verify_monitoring() {
    echo ""
    echo "=== Monitoring Verification ==="

    # Check for monitoring configuration
    if [[ -f "${PROJECT_ROOT}/scripts/docker/prometheus.yml" ]]; then
        test_pass "Prometheus configuration exists"
    else
        test_warn "Prometheus configuration not found"
    fi

    # Check if Grafana dashboards exist
    if [[ -d "${PROJECT_ROOT}/scripts/docker/grafana" ]]; then
        test_pass "Grafana dashboards directory exists"
    else
        test_warn "Grafana dashboards not configured"
    fi
}

# =============================================================================
# MAIN EXECUTION
# =============================================================================

main() {
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║           NovaCoin Deployment Verification Script v1.0.0             ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"

    verify_binaries
    verify_configuration
    verify_genesis
    verify_security
    verify_multisig
    verify_network
    verify_validators
    verify_monitoring

    echo ""
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║                        VERIFICATION SUMMARY                          ║"
    echo "╠══════════════════════════════════════════════════════════════════════╣"
    printf "║  ${GREEN}Passed${NC}:   %-56d ║\n" ${PASSED}
    printf "║  ${RED}Failed${NC}:   %-56d ║\n" ${FAILED}
    printf "║  ${YELLOW}Warnings${NC}: %-56d ║\n" ${WARNINGS}
    echo "╚══════════════════════════════════════════════════════════════════════╝"

    if [[ ${FAILED} -gt 0 ]]; then
        echo ""
        echo -e "${RED}VERIFICATION FAILED: ${FAILED} critical issues found${NC}"
        exit 1
    elif [[ ${WARNINGS} -gt 0 ]]; then
        echo ""
        echo -e "${YELLOW}VERIFICATION PASSED WITH WARNINGS: Review ${WARNINGS} items${NC}"
        exit 0
    else
        echo ""
        echo -e "${GREEN}VERIFICATION PASSED: All checks successful${NC}"
        exit 0
    fi
}

main "$@"
