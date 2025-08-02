# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Factor #2: Dependencies - Explicitly declare and isolate dependencies
# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Factor #5: Build, Release, Run - Strictly separate build and run stages
# Build the application with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o load-balancer cmd/server/main.go

# Final stage - minimal runtime image
FROM scratch

# Factor #10: Dev/Prod Parity - Keep development, staging, and production similar
# Copy CA certificates for HTTPS requests
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy binary from builder stage
COPY --from=builder /app/load-balancer /load-balancer

# Factor #6: Processes - Execute the app as one or more stateless processes
# Factor #7: Port Binding - Export services via port binding
EXPOSE 8080

# Factor #9: Disposability - Maximize robustness with fast startup and graceful shutdown
# Health check for container orchestration
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["/load-balancer", "-admin", "health-check"] || exit 1

# Factor #11: Logs - Treat logs as event streams
# Logs go to stdout/stderr, collected by container runtime

# Run the application
# Factor #3: Config - Configuration comes from environment variables
ENTRYPOINT ["/load-balancer"]
