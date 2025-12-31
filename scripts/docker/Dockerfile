# NovaCoin Node Dockerfile
# Multi-stage build for minimal image size

# ============================================
# Stage 1: Builder
# ============================================
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache \
    make \
    gcc \
    musl-dev \
    linux-headers \
    git

# Set working directory
WORKDIR /build

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the node binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.Version=$(git describe --tags --always 2>/dev/null || echo 'dev')" \
    -o novacoin \
    ./cmd/novacoin/...

# ============================================
# Stage 2: Runtime
# ============================================
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    jq

# Create non-root user for security
RUN addgroup -g 1000 novacoin && \
    adduser -u 1000 -G novacoin -s /bin/sh -D novacoin

# Create necessary directories
RUN mkdir -p /home/novacoin/.novacoin/data && \
    mkdir -p /home/novacoin/.novacoin/keys && \
    mkdir -p /home/novacoin/.novacoin/logs && \
    chown -R novacoin:novacoin /home/novacoin/.novacoin

# Copy binary from builder
COPY --from=builder /build/novacoin /usr/local/bin/novacoin

# Set permissions
RUN chmod +x /usr/local/bin/novacoin

# Switch to non-root user
USER novacoin
WORKDIR /home/novacoin

# Environment variables
ENV NOVACOIN_DATADIR=/home/novacoin/.novacoin/data
ENV NOVACOIN_NETWORK=mainnet

# Expose ports
# P2P networking
EXPOSE 30303/tcp
EXPOSE 30303/udp
# HTTP RPC
EXPOSE 8545/tcp
# WebSocket RPC
EXPOSE 8546/tcp
# Metrics (Prometheus)
EXPOSE 9090/tcp

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -sf http://localhost:8545 -X POST \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
        || exit 1

# Default command
ENTRYPOINT ["novacoin"]
CMD [ \
    "--datadir", "/home/novacoin/.novacoin/data", \
    "--networkid", "1", \
    "--http", \
    "--http.addr", "0.0.0.0", \
    "--http.port", "8545", \
    "--http.vhosts", "*", \
    "--http.corsdomain", "*", \
    "--ws", \
    "--ws.addr", "0.0.0.0", \
    "--ws.port", "8546", \
    "--ws.origins", "*", \
    "--port", "30303", \
    "--metrics" \
]
