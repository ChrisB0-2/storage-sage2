# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Install git for version info (optional)
RUN apk add --no-cache git

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with version info and static linking
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.version=${VERSION}" \
    -o storage-sage \
    ./cmd/storage-sage

# Runtime stage
FROM alpine:3.19

# Add ca-certificates for HTTPS (Loki, webhooks)
# and tzdata for proper timezone handling
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user for security
RUN adduser -D -u 1000 storagesage

# Copy binary from builder
COPY --from=builder /build/storage-sage /usr/local/bin/storage-sage

# Default config directory
RUN mkdir -p /etc/storage-sage && chown storagesage:storagesage /etc/storage-sage

# Switch to non-root user
USER storagesage

# Expose ports
# 8080 - daemon HTTP API (health, status, trigger)
# 9090 - Prometheus metrics
EXPOSE 8080 9090

# Health check for container orchestration
# Checks the daemon health endpoint every 30s
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Default entrypoint
ENTRYPOINT ["/usr/local/bin/storage-sage"]

# Default command shows help
CMD ["--help"]
