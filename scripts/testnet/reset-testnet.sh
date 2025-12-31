#!/bin/bash
#
# NovaCoin Testnet Reset Script
# Completely reset testnet to fresh state
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
DATA_DIR="${ROOT_DIR}/testnet-data"

# Print banner
print_banner() {
    echo -e "${RED}"
    echo "╔═══════════════════════════════════════════════════════════════════╗"
    echo "║                  NovaCoin Testnet Reset                           ║"
    echo "║                                                                   ║"
    echo "║  WARNING: This will delete ALL testnet data!                      ║"
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

# Confirmation prompt
confirm_reset() {
    echo ""
    echo -e "${YELLOW}This will:${NC}"
    echo "  1. Stop all testnet containers"
    echo "  2. Remove all testnet Docker volumes"
    echo "  3. Delete all testnet data in ${DATA_DIR}"
    echo "  4. Generate new genesis and validator keys"
    echo ""
    read -p "Are you sure you want to reset? Type 'yes' to confirm: " response

    if [[ "${response}" != "yes" ]]; then
        log_warn "Reset cancelled"
        exit 0
    fi
}

# Stop containers
stop_containers() {
    log_step "Stopping Testnet Containers"

    cd "${SCRIPT_DIR}/../docker"

    export COMPOSE_PROJECT_NAME=novacoin-testnet

    # Check if override exists
    if [[ -f "${DATA_DIR}/docker-compose.override.yml" ]]; then
        docker-compose \
            -f docker-compose.yml \
            -f "${DATA_DIR}/docker-compose.override.yml" \
            down -v --remove-orphans 2>/dev/null || true
    fi

    # Also stop any containers by name pattern
    local containers=$(docker ps -a --filter "name=novacoin-testnet" -q)
    if [[ -n "${containers}" ]]; then
        docker rm -f ${containers} 2>/dev/null || true
    fi

    log_info "Containers stopped and removed"
}

# Remove Docker volumes
remove_volumes() {
    log_step "Removing Docker Volumes"

    # List testnet volumes
    local volumes=$(docker volume ls --filter "name=novacoin-testnet" -q)

    if [[ -n "${volumes}" ]]; then
        docker volume rm ${volumes} 2>/dev/null || true
        log_info "Docker volumes removed"
    else
        log_info "No Docker volumes to remove"
    fi
}

# Delete data directory
delete_data() {
    log_step "Deleting Data Directory"

    if [[ -d "${DATA_DIR}" ]]; then
        rm -rf "${DATA_DIR}"
        log_info "Deleted: ${DATA_DIR}"
    else
        log_info "Data directory not found"
    fi
}

# Regenerate genesis
regenerate_genesis() {
    log_step "Regenerating Genesis"

    # Ask if user wants to regenerate
    read -p "Generate new genesis and keys? [Y/n]: " response
    response=${response:-Y}

    if [[ "${response}" =~ ^[Yy] ]]; then
        # Create fresh data directory
        mkdir -p "${DATA_DIR}"

        # Generate new genesis
        "${SCRIPT_DIR}/../genesis/generate-genesis.sh" \
            --network testnet \
            --chain-id 2 \
            --validators 4 \
            --output "${DATA_DIR}/genesis"

        log_info "New genesis generated"
    else
        log_info "Skipping genesis generation"
    fi
}

# Redeploy option
offer_redeploy() {
    echo ""
    read -p "Deploy fresh testnet now? [Y/n]: " response
    response=${response:-Y}

    if [[ "${response}" =~ ^[Yy] ]]; then
        "${SCRIPT_DIR}/deploy-testnet.sh" deploy
    else
        echo ""
        echo "To deploy later, run:"
        echo "  ${SCRIPT_DIR}/deploy-testnet.sh deploy"
    fi
}

# Main
main() {
    print_banner

    # Check for force flag
    if [[ "${1:-}" == "--force" || "${1:-}" == "-f" ]]; then
        log_warn "Force mode - skipping confirmation"
    else
        confirm_reset
    fi

    stop_containers
    remove_volumes
    delete_data

    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                    Testnet Reset Complete                         ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    regenerate_genesis
    offer_redeploy
}

main "$@"
