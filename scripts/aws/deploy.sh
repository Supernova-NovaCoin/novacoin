#!/bin/bash
#
# NovaCoin AWS Deployment Script
# Automated testnet/mainnet deployment
#
# Usage:
#   ./deploy.sh setup     - Configure AWS credentials
#   ./deploy.sh plan      - Preview infrastructure changes
#   ./deploy.sh apply     - Deploy infrastructure
#   ./deploy.sh destroy   - Tear down infrastructure
#   ./deploy.sh status    - Check deployment status
#   ./deploy.sh ssh       - SSH into nodes
#   ./deploy.sh logs      - View node logs
#

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEYS_DIR="${SCRIPT_DIR}/keys"

# Default values
ENVIRONMENT="${ENVIRONMENT:-testnet}"
AWS_REGION="${AWS_REGION:-ap-southeast-1}"  # Singapore
VALIDATOR_COUNT="${VALIDATOR_COUNT:-4}"  # Minimum 4 required for BFT
RPC_NODE_COUNT="${RPC_NODE_COUNT:-2}"

# =============================================================================
# HELPER FUNCTIONS
# =============================================================================

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

print_banner() {
    echo -e "${CYAN}"
    cat << 'EOF'
    _   __                  ______      _
   / | / /___ _   ______ _ / ____/___  (_)___
  /  |/ / __ \ | / / __ `// /   / __ \/ / __ \
 / /|  / /_/ / |/ / /_/ // /___/ /_/ / / / / /
/_/ |_/\____/|___/\__,_/ \____/\____/_/_/ /_/

    AWS Infrastructure Deployment
EOF
    echo -e "${NC}"
}

# =============================================================================
# SETUP
# =============================================================================

cmd_setup() {
    log_step "AWS Credentials Setup"

    echo "This will configure your AWS credentials for deployment."
    echo ""
    echo "You can provide credentials in one of these ways:"
    echo "  1. Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)"
    echo "  2. AWS CLI profile (~/.aws/credentials)"
    echo "  3. Interactive input (stored in environment)"
    echo ""

    # Check if already configured
    if aws sts get-caller-identity &>/dev/null; then
        ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
        log_info "AWS credentials already configured!"
        log_info "Account ID: $ACCOUNT_ID"
        echo ""
        read -p "Do you want to reconfigure? [y/N]: " reconfigure
        if [[ "$reconfigure" != "y" && "$reconfigure" != "Y" ]]; then
            return 0
        fi
    fi

    echo ""
    read -p "Enter AWS Access Key ID: " aws_access_key
    read -sp "Enter AWS Secret Access Key: " aws_secret_key
    echo ""
    read -p "Enter AWS Region [us-east-1]: " aws_region
    aws_region="${aws_region:-us-east-1}"

    # Export for current session
    export AWS_ACCESS_KEY_ID="$aws_access_key"
    export AWS_SECRET_ACCESS_KEY="$aws_secret_key"
    export AWS_DEFAULT_REGION="$aws_region"

    # Verify credentials
    if aws sts get-caller-identity &>/dev/null; then
        ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
        log_info "Credentials verified successfully!"
        log_info "Account ID: $ACCOUNT_ID"

        # Save to env file for persistence
        cat > "${SCRIPT_DIR}/.env" <<EOF
export AWS_ACCESS_KEY_ID="$aws_access_key"
export AWS_SECRET_ACCESS_KEY="$aws_secret_key"
export AWS_DEFAULT_REGION="$aws_region"
EOF
        chmod 600 "${SCRIPT_DIR}/.env"
        log_info "Credentials saved to .env file"
        log_warn "Remember to source .env before running other commands:"
        echo "  source ${SCRIPT_DIR}/.env"
    else
        log_error "Failed to verify AWS credentials!"
        exit 1
    fi
}

# =============================================================================
# PREREQUISITES CHECK
# =============================================================================

check_prerequisites() {
    log_step "Checking Prerequisites"

    local missing=0

    # Check Terraform
    if command -v terraform &>/dev/null; then
        TF_VERSION=$(terraform version -json | jq -r '.terraform_version')
        log_info "Terraform: v$TF_VERSION"
    else
        log_error "Terraform is not installed"
        log_info "Install from: https://www.terraform.io/downloads"
        missing=1
    fi

    # Check AWS CLI
    if command -v aws &>/dev/null; then
        AWS_VERSION=$(aws --version | cut -d' ' -f1 | cut -d'/' -f2)
        log_info "AWS CLI: v$AWS_VERSION"
    else
        log_error "AWS CLI is not installed"
        log_info "Install from: https://aws.amazon.com/cli/"
        missing=1
    fi

    # Check jq
    if command -v jq &>/dev/null; then
        log_info "jq: $(jq --version)"
    else
        log_error "jq is not installed"
        missing=1
    fi

    # Check AWS credentials
    if aws sts get-caller-identity &>/dev/null; then
        ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
        log_info "AWS Account: $ACCOUNT_ID"
    else
        log_error "AWS credentials not configured"
        log_info "Run: ./deploy.sh setup"
        missing=1
    fi

    if [[ $missing -eq 1 ]]; then
        exit 1
    fi
}

# =============================================================================
# TERRAFORM COMMANDS
# =============================================================================

terraform_init() {
    log_step "Initializing Terraform"
    cd "${SCRIPT_DIR}"
    terraform init -upgrade
}

cmd_plan() {
    check_prerequisites
    terraform_init

    log_step "Planning Infrastructure"

    terraform plan \
        -var="environment=${ENVIRONMENT}" \
        -var="aws_region=${AWS_REGION}" \
        -var="validator_count=${VALIDATOR_COUNT}" \
        -var="rpc_node_count=${RPC_NODE_COUNT}" \
        -out=tfplan

    log_info "Plan saved to: tfplan"
    log_info "Run './deploy.sh apply' to deploy"
}

cmd_apply() {
    check_prerequisites
    terraform_init

    log_step "Deploying Infrastructure"

    echo ""
    echo "Deployment Configuration:"
    echo "  Environment:    ${ENVIRONMENT}"
    echo "  Region:         ${AWS_REGION}"
    echo "  Validators:     ${VALIDATOR_COUNT}"
    echo "  RPC Nodes:      ${RPC_NODE_COUNT}"
    echo ""
    read -p "Proceed with deployment? [y/N]: " confirm
    if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
        log_warn "Deployment cancelled"
        exit 0
    fi

    # Apply
    terraform apply \
        -var="environment=${ENVIRONMENT}" \
        -var="aws_region=${AWS_REGION}" \
        -var="validator_count=${VALIDATOR_COUNT}" \
        -var="rpc_node_count=${RPC_NODE_COUNT}" \
        -auto-approve

    log_step "Deployment Complete!"

    # Show outputs
    echo ""
    echo "Infrastructure Details:"
    echo "========================"
    terraform output

    # Save outputs
    terraform output -json > "${SCRIPT_DIR}/outputs.json"
    log_info "Outputs saved to: outputs.json"

    echo ""
    log_info "SSH Key saved to: ${KEYS_DIR}/novacoin-${ENVIRONMENT}.pem"
    log_info "To SSH: ./deploy.sh ssh bootnode"
    log_info "RPC Endpoint: $(terraform output -raw rpc_endpoint)"
}

cmd_destroy() {
    check_prerequisites

    log_step "Destroying Infrastructure"

    echo ""
    log_warn "This will PERMANENTLY destroy all infrastructure!"
    echo ""
    read -p "Type '${ENVIRONMENT}' to confirm: " confirm
    if [[ "$confirm" != "${ENVIRONMENT}" ]]; then
        log_warn "Destruction cancelled"
        exit 0
    fi

    cd "${SCRIPT_DIR}"
    terraform destroy \
        -var="environment=${ENVIRONMENT}" \
        -var="aws_region=${AWS_REGION}" \
        -var="validator_count=${VALIDATOR_COUNT}" \
        -var="rpc_node_count=${RPC_NODE_COUNT}" \
        -auto-approve

    log_info "Infrastructure destroyed"
}

# =============================================================================
# STATUS & MANAGEMENT
# =============================================================================

cmd_status() {
    check_prerequisites

    log_step "Deployment Status"

    cd "${SCRIPT_DIR}"

    if [[ ! -f "terraform.tfstate" ]]; then
        log_warn "No deployment found"
        return 1
    fi

    # Get node IPs
    BOOTNODE_IP=$(terraform output -raw bootnode_public_ip 2>/dev/null || echo "N/A")
    VALIDATOR_IPS=$(terraform output -json validator_public_ips 2>/dev/null | jq -r '.[]' || echo "N/A")
    RPC_IPS=$(terraform output -json rpc_public_ips 2>/dev/null | jq -r '.[]' || echo "N/A")
    RPC_LB=$(terraform output -raw rpc_load_balancer_dns 2>/dev/null || echo "N/A")

    echo ""
    echo "Bootnode:"
    echo "  IP: ${BOOTNODE_IP}"
    if [[ "$BOOTNODE_IP" != "N/A" ]]; then
        STATUS=$(curl -sf -X POST "http://${BOOTNODE_IP}:8545" \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
            2>/dev/null | jq -r '.result // "offline"' || echo "offline")
        echo "  Block: ${STATUS}"
    fi

    echo ""
    echo "Validators:"
    i=1
    for ip in $VALIDATOR_IPS; do
        echo "  Validator $i: $ip"
        ((i++))
    done

    echo ""
    echo "RPC Nodes:"
    for ip in $RPC_IPS; do
        echo "  $ip"
    done

    echo ""
    echo "RPC Load Balancer: http://${RPC_LB}:8545"
}

cmd_ssh() {
    local target="${1:-bootnode}"

    cd "${SCRIPT_DIR}"
    KEY_FILE="${KEYS_DIR}/novacoin-${ENVIRONMENT}.pem"

    if [[ ! -f "$KEY_FILE" ]]; then
        log_error "SSH key not found: $KEY_FILE"
        exit 1
    fi

    case "$target" in
        bootnode)
            IP=$(terraform output -raw bootnode_public_ip)
            ;;
        validator-*)
            INDEX=${target#validator-}
            IP=$(terraform output -json validator_public_ips | jq -r ".[$((INDEX-1))]")
            ;;
        rpc-*)
            INDEX=${target#rpc-}
            IP=$(terraform output -json rpc_public_ips | jq -r ".[$((INDEX-1))]")
            ;;
        *)
            log_error "Unknown target: $target"
            echo "Usage: ./deploy.sh ssh [bootnode|validator-N|rpc-N]"
            exit 1
            ;;
    esac

    log_info "Connecting to $target ($IP)..."
    ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@"$IP"
}

cmd_logs() {
    local target="${1:-bootnode}"

    cd "${SCRIPT_DIR}"
    KEY_FILE="${KEYS_DIR}/novacoin-${ENVIRONMENT}.pem"

    case "$target" in
        bootnode)
            IP=$(terraform output -raw bootnode_public_ip)
            ;;
        validator-*)
            INDEX=${target#validator-}
            IP=$(terraform output -json validator_public_ips | jq -r ".[$((INDEX-1))]")
            ;;
        rpc-*)
            INDEX=${target#rpc-}
            IP=$(terraform output -json rpc_public_ips | jq -r ".[$((INDEX-1))]")
            ;;
    esac

    log_info "Fetching logs from $target ($IP)..."
    ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@"$IP" \
        "sudo journalctl -u novacoin -f --no-pager"
}

# =============================================================================
# MAIN
# =============================================================================

print_banner

# Load environment if exists
if [[ -f "${SCRIPT_DIR}/.env" ]]; then
    source "${SCRIPT_DIR}/.env"
fi

# Parse command
COMMAND="${1:-help}"
shift || true

case "$COMMAND" in
    setup)
        cmd_setup
        ;;
    plan)
        cmd_plan
        ;;
    apply)
        cmd_apply
        ;;
    destroy)
        cmd_destroy
        ;;
    status)
        cmd_status
        ;;
    ssh)
        cmd_ssh "$@"
        ;;
    logs)
        cmd_logs "$@"
        ;;
    help|*)
        echo "Usage: ./deploy.sh <command> [options]"
        echo ""
        echo "Commands:"
        echo "  setup     Configure AWS credentials"
        echo "  plan      Preview infrastructure changes"
        echo "  apply     Deploy infrastructure"
        echo "  destroy   Tear down infrastructure"
        echo "  status    Check deployment status"
        echo "  ssh       SSH into nodes (bootnode, validator-N, rpc-N)"
        echo "  logs      View node logs"
        echo ""
        echo "Environment Variables:"
        echo "  ENVIRONMENT      testnet or mainnet (default: testnet)"
        echo "  AWS_REGION       AWS region (default: us-east-1)"
        echo "  VALIDATOR_COUNT  Number of validators (default: 4)"
        echo "  RPC_NODE_COUNT   Number of RPC nodes (default: 2)"
        echo ""
        echo "Examples:"
        echo "  ./deploy.sh setup"
        echo "  ./deploy.sh plan"
        echo "  ./deploy.sh apply"
        echo "  ./deploy.sh ssh validator-1"
        echo "  VALIDATOR_COUNT=8 ./deploy.sh apply"
        ;;
esac
