#!/bin/bash
# =============================================================================
# NovaCoin Mainnet Deployment Script
# =============================================================================
# WARNING: This script deploys to PRODUCTION. Execute with extreme caution.
# Requires: Multi-sig approval, security checklist completion, audit sign-off
# =============================================================================

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CONFIG_FILE="${SCRIPT_DIR}/mainnet-config.yaml"
SECURITY_CHECKLIST="${SCRIPT_DIR}/SECURITY-CHECKLIST.md"
AUDIT_LOG="${SCRIPT_DIR}/logs/deployment-$(date +%Y%m%d-%H%M%S).log"
MULTISIG_FILE="${SCRIPT_DIR}/.multisig-approvals"

# Ensure log directory exists
mkdir -p "${SCRIPT_DIR}/logs"

# =============================================================================
# LOGGING FUNCTIONS
# =============================================================================

log() {
    local level="$1"
    shift
    local message="$*"
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo -e "${timestamp} [${level}] ${message}" | tee -a "${AUDIT_LOG}"
}

log_info() { log "INFO" "$*"; }
log_warn() { echo -e "${YELLOW}"; log "WARN" "$*"; echo -e "${NC}"; }
log_error() { echo -e "${RED}"; log "ERROR" "$*"; echo -e "${NC}"; }
log_success() { echo -e "${GREEN}"; log "SUCCESS" "$*"; echo -e "${NC}"; }

# =============================================================================
# SAFETY CHECKS
# =============================================================================

confirm_deployment() {
    echo -e "${RED}"
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║                    ⚠️  MAINNET DEPLOYMENT WARNING ⚠️                   ║"
    echo "╠══════════════════════════════════════════════════════════════════════╣"
    echo "║  You are about to deploy NovaCoin to PRODUCTION MAINNET.            ║"
    echo "║  This action is IRREVERSIBLE and will affect real assets.           ║"
    echo "║                                                                      ║"
    echo "║  Requirements before proceeding:                                     ║"
    echo "║  ✓ Security checklist completed and signed                          ║"
    echo "║  ✓ External security audit passed                                   ║"
    echo "║  ✓ Multi-sig approval obtained (4 of 7 signatures)                  ║"
    echo "║  ✓ Genesis validators confirmed and ready                           ║"
    echo "║  ✓ Monitoring and alerting configured                               ║"
    echo "║  ✓ Emergency shutdown procedures tested                             ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"

    read -p "Type 'DEPLOY MAINNET' to confirm: " confirmation
    if [[ "$confirmation" != "DEPLOY MAINNET" ]]; then
        log_error "Deployment cancelled by user"
        exit 1
    fi

    read -p "Enter your deployer key ID: " deployer_id
    log_info "Deployment initiated by: ${deployer_id}"
}

check_multisig_approvals() {
    log_info "Checking multi-sig approvals..."

    if [[ ! -f "${MULTISIG_FILE}" ]]; then
        log_error "Multi-sig approval file not found: ${MULTISIG_FILE}"
        log_error "Run 'multisig-deploy.sh approve' to collect signatures"
        exit 1
    fi

    local approval_count=$(grep -c "^APPROVED:" "${MULTISIG_FILE}" || echo "0")
    local required=4

    if [[ ${approval_count} -lt ${required} ]]; then
        log_error "Insufficient multi-sig approvals: ${approval_count}/${required}"
        exit 1
    fi

    log_success "Multi-sig approvals verified: ${approval_count}/${required}"
}

check_security_checklist() {
    log_info "Verifying security checklist completion..."

    if [[ ! -f "${SECURITY_CHECKLIST}" ]]; then
        log_error "Security checklist not found: ${SECURITY_CHECKLIST}"
        exit 1
    fi

    # Check for completion markers
    local unchecked=$(grep -c "\[ \]" "${SECURITY_CHECKLIST}" || echo "0")

    if [[ ${unchecked} -gt 0 ]]; then
        log_error "Security checklist incomplete: ${unchecked} items not checked"
        log_error "Complete all items in ${SECURITY_CHECKLIST}"
        exit 1
    fi

    log_success "Security checklist verified"
}

check_system_requirements() {
    log_info "Checking system requirements..."

    # Check Go version
    local go_version=$(go version 2>/dev/null | grep -oP 'go\K[0-9]+\.[0-9]+' || echo "0")
    if [[ $(echo "${go_version} < 1.21" | bc -l) -eq 1 ]]; then
        log_error "Go version 1.21+ required, found: ${go_version}"
        exit 1
    fi

    # Check available memory
    local mem_gb=$(free -g | awk '/^Mem:/{print $2}')
    if [[ ${mem_gb} -lt 32 ]]; then
        log_warn "Recommended RAM: 32GB, found: ${mem_gb}GB"
    fi

    # Check disk space
    local disk_gb=$(df -BG "${PROJECT_ROOT}" | awk 'NR==2{print $4}' | tr -d 'G')
    if [[ ${disk_gb} -lt 500 ]]; then
        log_error "Insufficient disk space: ${disk_gb}GB (500GB required)"
        exit 1
    fi

    # Check required ports
    for port in 30303 8545 8546 9090; do
        if netstat -tuln 2>/dev/null | grep -q ":${port} "; then
            log_error "Port ${port} is already in use"
            exit 1
        fi
    done

    log_success "System requirements verified"
}

# =============================================================================
# BUILD FUNCTIONS
# =============================================================================

build_binaries() {
    log_info "Building NovaCoin binaries..."

    cd "${PROJECT_ROOT}"

    # Clean previous builds
    rm -rf build/
    mkdir -p build/

    # Build with production flags
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
        -ldflags="-s -w -X main.Version=1.0.0 -X main.GitCommit=$(git rev-parse HEAD)" \
        -o build/novacoin \
        ./cmd/novacoin

    # Verify binary
    if [[ ! -f "build/novacoin" ]]; then
        log_error "Build failed: novacoin binary not found"
        exit 1
    fi

    # Calculate checksum
    local checksum=$(sha256sum build/novacoin | awk '{print $1}')
    echo "${checksum}" > build/novacoin.sha256
    log_info "Binary checksum: ${checksum}"

    log_success "Binaries built successfully"
}

run_tests() {
    log_info "Running test suite..."

    cd "${PROJECT_ROOT}"

    # Run all tests
    if ! go test ./... -count=1 -race -timeout=10m; then
        log_error "Tests failed"
        exit 1
    fi

    log_success "All tests passed"
}

# =============================================================================
# GENESIS GENERATION
# =============================================================================

generate_genesis() {
    log_info "Generating genesis configuration..."

    local genesis_dir="${SCRIPT_DIR}/genesis"
    mkdir -p "${genesis_dir}"

    # Generate genesis timestamp
    local genesis_timestamp=$(date +%s)

    # Create genesis.json
    cat > "${genesis_dir}/genesis.json" << EOF
{
  "config": {
    "chainId": 1,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "berlinBlock": 0,
    "londonBlock": 0,
    "novacoin": {
      "dagknight": {
        "initialK": 0.33,
        "minK": 0.25,
        "maxK": 0.50
      },
      "shoal": {
        "parallelDAGs": 4,
        "dagStaggerMs": 125
      },
      "mysticeti": {
        "commitDepth": 3,
        "voteThreshold": 0.67
      },
      "mev": {
        "enabled": true,
        "threshold": 67,
        "totalShares": 100
      }
    }
  },
  "timestamp": "${genesis_timestamp}",
  "extraData": "0x4e6f7661436f696e204d61696e6e6574204c61756e6368",
  "gasLimit": "30000000",
  "difficulty": "0x1",
  "alloc": {}
}
EOF

    log_info "Genesis file created at: ${genesis_dir}/genesis.json"
    log_warn "IMPORTANT: Update 'alloc' with actual addresses before deployment"

    log_success "Genesis configuration generated"
}

# =============================================================================
# DEPLOYMENT FUNCTIONS
# =============================================================================

deploy_bootstrap_nodes() {
    log_info "Deploying bootstrap nodes..."

    # This would typically deploy to cloud infrastructure
    # For now, we'll create the configuration

    local bootstrap_dir="${SCRIPT_DIR}/bootstrap"
    mkdir -p "${bootstrap_dir}"

    cat > "${bootstrap_dir}/bootstrap-config.yaml" << EOF
# Bootstrap Node Configuration
# Deploy these nodes first before mainnet launch

nodes:
  - id: bootstrap-1
    region: us-east-1
    instance_type: m5.2xlarge
    enode: ""  # Generated during deployment

  - id: bootstrap-2
    region: eu-west-1
    instance_type: m5.2xlarge
    enode: ""

  - id: bootstrap-3
    region: ap-northeast-1
    instance_type: m5.2xlarge
    enode: ""

  - id: bootstrap-4
    region: us-west-2
    instance_type: m5.2xlarge
    enode: ""
EOF

    log_success "Bootstrap node configuration created"
}

deploy_genesis_validators() {
    log_info "Preparing genesis validator deployment..."

    local validator_dir="${SCRIPT_DIR}/validators"
    mkdir -p "${validator_dir}"

    cat > "${validator_dir}/genesis-validators.yaml" << EOF
# Genesis Validators
# These validators will be active at network launch

validators:
  # Minimum 100 validators required for mainnet launch
  # Each validator must:
  # - Pass KYC/AML verification (if required)
  # - Stake minimum 32 NOVA
  # - Meet hardware requirements
  # - Sign validator agreement

  count: 0  # Update with actual count

  requirements:
    min_stake: "32000000000000000000"  # 32 NOVA
    hardware:
      cpu_cores: 16
      ram_gb: 64
      storage_gb: 4000
      storage_type: "nvme_ssd"

  onboarding:
    deadline: ""  # Set deadline for validator registration
    verification_required: true
EOF

    log_info "Validator configuration created at: ${validator_dir}/"
    log_warn "IMPORTANT: Onboard genesis validators before launch"

    log_success "Genesis validator preparation complete"
}

# =============================================================================
# VERIFICATION FUNCTIONS
# =============================================================================

verify_deployment() {
    log_info "Running deployment verification..."

    "${SCRIPT_DIR}/verify-deployment.sh"

    log_success "Deployment verification passed"
}

# =============================================================================
# MAIN EXECUTION
# =============================================================================

main() {
    echo -e "${BLUE}"
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║              NovaCoin Mainnet Deployment Script v1.0.0               ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"

    log_info "Starting mainnet deployment process..."
    log_info "Audit log: ${AUDIT_LOG}"

    # Pre-deployment checks
    confirm_deployment
    check_multisig_approvals
    check_security_checklist
    check_system_requirements

    # Build phase
    build_binaries
    run_tests

    # Genesis preparation
    generate_genesis

    # Infrastructure deployment
    deploy_bootstrap_nodes
    deploy_genesis_validators

    # Post-deployment verification
    verify_deployment

    echo -e "${GREEN}"
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║                    ✅ DEPLOYMENT PREPARATION COMPLETE                 ║"
    echo "╠══════════════════════════════════════════════════════════════════════╣"
    echo "║  Next steps:                                                         ║"
    echo "║  1. Review generated configurations in ${SCRIPT_DIR}/                ║"
    echo "║  2. Update genesis.json with actual addresses                        ║"
    echo "║  3. Deploy bootstrap nodes to cloud infrastructure                   ║"
    echo "║  4. Onboard genesis validators                                       ║"
    echo "║  5. Schedule mainnet launch with all stakeholders                    ║"
    echo "║  6. Execute final launch with 'launch-mainnet.sh'                    ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"

    log_success "Mainnet deployment preparation completed"
}

# Run main function
main "$@"
