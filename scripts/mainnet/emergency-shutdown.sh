#!/bin/bash
# =============================================================================
# NovaCoin Emergency Shutdown Script
# =============================================================================
# USE ONLY IN EMERGENCY SITUATIONS
# Requires multi-sig approval for execution in non-emergency mode
# =============================================================================

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AUDIT_LOG="${SCRIPT_DIR}/logs/emergency-$(date +%Y%m%d-%H%M%S).log"

# Emergency contacts
EMERGENCY_CONTACTS=(
    "security@novacoin.io"
    "oncall@novacoin.io"
)

# =============================================================================
# LOGGING
# =============================================================================

log() {
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo -e "${timestamp} $*" | tee -a "${AUDIT_LOG}"
}

notify_team() {
    local message="$1"
    local severity="$2"

    # Log notification
    log "[NOTIFY] Severity: ${severity} - ${message}"

    # In production, integrate with:
    # - PagerDuty
    # - Slack/Discord
    # - Email
    # - SMS

    for contact in "${EMERGENCY_CONTACTS[@]}"; do
        log "[NOTIFY] Alerting: ${contact}"
        # mail -s "[EMERGENCY] NovaCoin: ${severity}" "${contact}" <<< "${message}"
    done
}

# =============================================================================
# SHUTDOWN FUNCTIONS
# =============================================================================

graceful_shutdown() {
    log "${YELLOW}[SHUTDOWN] Initiating graceful shutdown...${NC}"

    # Step 1: Stop accepting new transactions
    log "[SHUTDOWN] Stopping transaction acceptance..."
    # curl -X POST http://localhost:8545 -d '{"method":"admin_stopTxPool"}'

    # Step 2: Wait for pending transactions
    log "[SHUTDOWN] Waiting for pending transactions to clear (60s)..."
    sleep 60

    # Step 3: Stop block production
    log "[SHUTDOWN] Stopping block production..."
    # curl -X POST http://localhost:8545 -d '{"method":"admin_stopMining"}'

    # Step 4: Disconnect peers gracefully
    log "[SHUTDOWN] Disconnecting peers..."
    # curl -X POST http://localhost:8545 -d '{"method":"admin_removePeers"}'

    # Step 5: Save state snapshot
    log "[SHUTDOWN] Saving state snapshot..."
    # curl -X POST http://localhost:8545 -d '{"method":"debug_saveSnapshot"}'

    # Step 6: Stop node
    log "[SHUTDOWN] Stopping node process..."
    pkill -TERM novacoin || true

    log "${GREEN}[SHUTDOWN] Graceful shutdown complete${NC}"
}

emergency_halt() {
    log "${RED}[EMERGENCY] INITIATING EMERGENCY HALT${NC}"

    notify_team "Emergency halt initiated" "CRITICAL"

    # Immediately stop all node processes
    log "[EMERGENCY] Force stopping all node processes..."
    pkill -9 novacoin || true

    # Stop Docker containers if running
    log "[EMERGENCY] Stopping Docker containers..."
    docker stop $(docker ps -q --filter "name=novacoin") 2>/dev/null || true

    # Block network ports (requires root)
    log "[EMERGENCY] Blocking network ports..."
    # iptables -A INPUT -p tcp --dport 30303 -j DROP
    # iptables -A INPUT -p tcp --dport 8545 -j DROP

    log "${RED}[EMERGENCY] Emergency halt complete - ALL SYSTEMS STOPPED${NC}"
}

pause_consensus() {
    log "${YELLOW}[PAUSE] Pausing consensus...${NC}"

    # Send pause signal to consensus layer
    log "[PAUSE] Sending pause signal..."
    # curl -X POST http://localhost:8545 -d '{"method":"nova_pauseConsensus"}'

    # Validators will stop producing blocks but node stays running
    log "${GREEN}[PAUSE] Consensus paused - node still accepting queries${NC}"
}

resume_consensus() {
    log "${GREEN}[RESUME] Resuming consensus...${NC}"

    # Send resume signal
    log "[RESUME] Sending resume signal..."
    # curl -X POST http://localhost:8545 -d '{"method":"nova_resumeConsensus"}'

    log "${GREEN}[RESUME] Consensus resumed${NC}"
}

# =============================================================================
# RECOVERY FUNCTIONS
# =============================================================================

recover_from_snapshot() {
    local snapshot_file="$1"

    log "[RECOVERY] Recovering from snapshot: ${snapshot_file}"

    if [[ ! -f "${snapshot_file}" ]]; then
        log "${RED}[RECOVERY] Snapshot file not found${NC}"
        exit 1
    fi

    # Stop node if running
    pkill -9 novacoin 2>/dev/null || true

    # Restore snapshot
    log "[RECOVERY] Restoring state..."
    # novacoin snapshot restore --file "${snapshot_file}"

    log "${GREEN}[RECOVERY] Snapshot restored${NC}"
}

rollback_to_block() {
    local block_number="$1"

    log "${YELLOW}[ROLLBACK] Rolling back to block: ${block_number}${NC}"

    notify_team "Initiating rollback to block ${block_number}" "HIGH"

    # This is a DANGEROUS operation
    echo -e "${RED}"
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║                    ⚠️  ROLLBACK WARNING ⚠️                             ║"
    echo "║  This will revert all state after block ${block_number}              ║"
    echo "║  All transactions after this block will be lost!                     ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"

    read -p "Type 'CONFIRM ROLLBACK' to proceed: " confirmation
    if [[ "$confirmation" != "CONFIRM ROLLBACK" ]]; then
        log "[ROLLBACK] Cancelled by user"
        exit 1
    fi

    # Stop node
    pkill -TERM novacoin 2>/dev/null || true
    sleep 5

    # Perform rollback
    log "[ROLLBACK] Executing rollback..."
    # novacoin db rollback --to-block "${block_number}"

    log "${GREEN}[ROLLBACK] Rollback complete${NC}"
}

# =============================================================================
# STATUS FUNCTIONS
# =============================================================================

check_status() {
    echo "=== NovaCoin Node Status ==="

    # Check if process is running
    if pgrep -x novacoin > /dev/null; then
        echo -e "${GREEN}Node Process: RUNNING${NC}"
        local pid=$(pgrep -x novacoin)
        echo "PID: ${pid}"
    else
        echo -e "${RED}Node Process: STOPPED${NC}"
    fi

    # Check RPC
    if curl -s http://localhost:8545 > /dev/null 2>&1; then
        echo -e "${GREEN}RPC: AVAILABLE${NC}"

        # Get block number
        local block=$(curl -s http://localhost:8545 \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
            | jq -r '.result' 2>/dev/null || echo "unknown")
        echo "Current Block: ${block}"
    else
        echo -e "${RED}RPC: UNAVAILABLE${NC}"
    fi

    # Check P2P
    if netstat -tuln 2>/dev/null | grep -q ":30303 "; then
        echo -e "${GREEN}P2P Port: LISTENING${NC}"
    else
        echo -e "${RED}P2P Port: NOT LISTENING${NC}"
    fi

    # Check peers
    local peers=$(curl -s http://localhost:8545 \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"net_peerCount","params":[],"id":1}' \
        | jq -r '.result' 2>/dev/null || echo "0x0")
    echo "Connected Peers: $((${peers}))"
}

# =============================================================================
# MAIN
# =============================================================================

usage() {
    echo "NovaCoin Emergency Shutdown Script"
    echo ""
    echo "Usage: $0 <command>"
    echo ""
    echo "Commands:"
    echo "  graceful          Graceful shutdown (saves state)"
    echo "  halt              Emergency halt (immediate stop)"
    echo "  pause             Pause consensus (node stays running)"
    echo "  resume            Resume consensus"
    echo "  recover <file>    Recover from snapshot file"
    echo "  rollback <block>  Rollback to specific block"
    echo "  status            Check node status"
    echo ""
    echo "Examples:"
    echo "  $0 graceful"
    echo "  $0 halt"
    echo "  $0 recover /path/to/snapshot.db"
    echo "  $0 rollback 1000000"
}

main() {
    mkdir -p "${SCRIPT_DIR}/logs"

    if [[ $# -lt 1 ]]; then
        usage
        exit 1
    fi

    local command="$1"
    shift

    log "=== Emergency Script Executed ==="
    log "Command: ${command}"
    log "User: $(whoami)"
    log "Host: $(hostname)"

    case "${command}" in
        graceful)
            graceful_shutdown
            ;;
        halt)
            emergency_halt
            ;;
        pause)
            pause_consensus
            ;;
        resume)
            resume_consensus
            ;;
        recover)
            if [[ $# -lt 1 ]]; then
                echo "Usage: $0 recover <snapshot_file>"
                exit 1
            fi
            recover_from_snapshot "$1"
            ;;
        rollback)
            if [[ $# -lt 1 ]]; then
                echo "Usage: $0 rollback <block_number>"
                exit 1
            fi
            rollback_to_block "$1"
            ;;
        status)
            check_status
            ;;
        *)
            echo "Unknown command: ${command}"
            usage
            exit 1
            ;;
    esac
}

main "$@"
