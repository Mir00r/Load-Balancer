# 12-Factor App Compliance

This document outlines how the Load Balancer implementation follows the [12-Factor App](https://12factor.net/) methodology for building modern, scalable applications.

## I. Codebase
**One codebase tracked in revision control, many deploys**

✅ **Implemented**: 
- Single Git repository contains the entire application
- Version tracking with Git commits, tags, and build metadata
- Build flags include version, git commit, and build time
- Same codebase deployed to development, staging, and production environments

**Implementation Details**:
- `Makefile` includes version tracking: `GIT_COMMIT`, `BUILD_TIME`, `VERSION`
- Docker images tagged with versions
- Release process creates versioned artifacts

## II. Dependencies
**Explicitly declare and isolate dependencies**

✅ **Implemented**:
- `go.mod` and `go.sum` explicitly declare all dependencies
- Dependencies are vendored and verified
- No system-wide packages assumed
- Container images use multi-stage builds to isolate dependencies

**Implementation Details**:
- `go mod download && go mod verify` in build process
- Docker multi-stage build with explicit dependency installation
- No reliance on implicit system dependencies

## III. Config
**Store config in the environment**

✅ **Implemented**:
- All configuration via environment variables
- No hardcoded configuration in code
- Environment-specific configuration files (`.env`, `.env.production`)
- Default values provided with environment override capability

**Implementation Details**:
- `internal/config/environment.go` - Comprehensive environment configuration
- Docker Compose uses environment variables
- `.env` file for local development
- Production configuration template (`.env.production`)

**Environment Variables**:
```bash
# Server Configuration
LB_SERVER_HOST=0.0.0.0
LB_SERVER_PORT=8080
LB_SERVER_READ_TIMEOUT=30s
LB_SERVER_WRITE_TIMEOUT=30s
LB_SERVER_IDLE_TIMEOUT=60s

# Load Balancer Configuration
LB_ALGORITHM=round_robin
LB_HEALTH_CHECK_ENABLED=true
LB_HEALTH_CHECK_INTERVAL=30s
LB_HEALTH_CHECK_TIMEOUT=10s
LB_HEALTH_CHECK_RETRIES=3
LB_HEALTH_CHECK_PATH=/health

# Rate Limiting
LB_RATE_LIMIT_ENABLED=true
LB_RATE_LIMIT_REQUESTS=100
LB_RATE_LIMIT_BURST=10

# Circuit Breaker
LB_CIRCUIT_BREAKER_ENABLED=true
LB_CIRCUIT_BREAKER_THRESHOLD=5
LB_CIRCUIT_BREAKER_TIMEOUT=60s

# Logging
LB_LOG_LEVEL=info
LB_LOG_FORMAT=json

# Backend Servers
LB_BACKENDS=http://backend1:8081,http://backend2:8082
```

## IV. Backing Services
**Treat backing services as attached resources**

✅ **Implemented**:
- Backend servers configured via environment variables
- Services can be swapped without code changes
- Abstracted through repository and service interfaces
- Health checking treats backends as potentially unreliable resources

**Implementation Details**:
- Backend URLs configurable via `LB_BACKENDS` environment variable
- Repository pattern abstracts backend management
- Health checker monitors backend availability
- Circuit breaker protects against failing backends

## V. Build, Release, Run
**Strictly separate build and run stages**

✅ **Implemented**:
- Clear separation of build, release, and run stages
- Immutable releases with version tracking
- Runtime cannot modify code
- Docker multi-stage builds enforce separation

**Implementation Details**:
- **Build Stage**: `make build` creates optimized binary
- **Release Stage**: `make release` creates versioned artifacts
- **Run Stage**: Application runs from immutable binary
- Docker images built once, run multiple times

**Build Process**:
```bash
# Build stage
make deps      # Install dependencies
make test      # Run tests
make build     # Create binary

# Release stage
make release   # Create versioned release package

# Run stage
./load-balancer  # Run immutable binary
```

## VI. Processes
**Execute the app as one or more stateless processes**

✅ **Implemented**:
- Application runs as stateless processes
- No sticky sessions or local state storage
- Process-shared state managed through external services
- Horizontal scaling ready

**Implementation Details**:
- `cmd/server/process.go` - Process management utilities
- No local file system state (except logs, which are streams)
- Session state would be stored in external services (Redis, etc.)
- Each process instance is independent and replaceable

## VII. Port Binding
**Export services via port binding**

✅ **Implemented**:
- Application self-contained with built-in HTTP server
- No reliance on external web server
- Port configurable via environment variables
- Container exposes services via port binding

**Implementation Details**:
- Built-in HTTP server using Go's `net/http`
- Port configured via `LB_SERVER_PORT` environment variable
- Docker containers expose ports: `EXPOSE 8080`
- Service discovery through port binding

## VIII. Concurrency
**Scale out via the process model**

✅ **Implemented**:
- Designed for horizontal scaling
- Process-based concurrency model
- No shared state between processes
- Ready for container orchestration

**Implementation Details**:
- `cmd/server/process.go` - Process management
- Docker Compose scaling: `docker-compose up --scale backend1=3`
- Kubernetes ready with proper resource definitions
- Load balancer itself can be scaled horizontally

**Scaling Examples**:
```bash
# Docker Compose scaling
docker-compose up --scale load-balancer=3

# Kubernetes scaling
kubectl scale deployment load-balancer --replicas=3
```

## IX. Disposability
**Maximize robustness with fast startup and graceful shutdown**

✅ **Implemented**:
- Fast startup (< 5 seconds)
- Graceful shutdown handling
- SIGTERM signal handling
- Health checks for orchestration

**Implementation Details**:
- `internal/handler/health.go` - Health and readiness endpoints
- Graceful shutdown in `main.go` with context cancellation
- Docker health checks and readiness probes
- Quick startup with minimal initialization

**Health Endpoints**:
- `/health` - Liveness probe
- `/ready` - Readiness probe
- `/metrics` - Monitoring endpoint

## X. Dev/Prod Parity
**Keep development, staging, and production as similar as possible**

✅ **Implemented**:
- Same container images across environments
- Environment-based configuration only
- Same backing services (containerized)
- Minimal gaps between environments

**Implementation Details**:
- Docker ensures consistent runtime environments
- Environment variables control behavior differences
- Same database/service containers in development and production
- Continuous deployment with same artifacts

**Environment Parity**:
```bash
# Development
make dev-setup
make docker-up

# Production (same containers, different config)
docker-compose -f docker-compose.prod.yml up
```

## XI. Logs
**Treat logs as event streams**

✅ **Implemented**:
- Logs written to stdout/stderr
- Structured logging (JSON format option)
- No log file management in application
- Container runtime handles log aggregation

**Implementation Details**:
- `pkg/logger` - Structured logging package
- Configurable log format (text/json) via `LB_LOG_FORMAT`
- Docker Compose log configuration
- Ready for centralized logging systems (ELK, Fluentd, etc.)

**Log Configuration**:
```yaml
# docker-compose.yml
logging:
  driver: "json-file"
  options:
    max-size: "10m"
    max-file: "3"
```

## XII. Admin Processes
**Run admin/management tasks as one-off processes**

✅ **Implemented**:
- Admin processes run in same environment
- One-off commands for maintenance tasks
- Same codebase for admin and web processes
- Built-in admin command support

**Implementation Details**:
- `cmd/server/admin.go` - Admin process runner
- Admin flags: `-admin health-check`, `-admin stats`, `-admin validate-config`
- Same container image for admin tasks
- Database migrations and maintenance through admin processes

**Admin Commands**:
```bash
# Health check
./load-balancer -admin health-check

# Backend statistics
./load-balancer -admin stats

# Configuration validation
./load-balancer -admin validate-config

# Database migrations (if implemented)
./load-balancer -admin migrate

# Cleanup tasks
./load-balancer -admin cleanup
```

## Compliance Summary

| Factor | Status | Implementation |
|--------|--------|----------------|
| I. Codebase | ✅ | Git repository with version tracking |
| II. Dependencies | ✅ | go.mod with explicit dependencies |
| III. Config | ✅ | Environment variables only |
| IV. Backing Services | ✅ | Configurable backend services |
| V. Build, Release, Run | ✅ | Separate build/release/run stages |
| VI. Processes | ✅ | Stateless process design |
| VII. Port Binding | ✅ | Self-contained HTTP server |
| VIII. Concurrency | ✅ | Horizontal scaling ready |
| IX. Disposability | ✅ | Fast startup, graceful shutdown |
| X. Dev/Prod Parity | ✅ | Container-based consistency |
| XI. Logs | ✅ | Stdout/stderr event streams |
| XII. Admin Processes | ✅ | Built-in admin commands |

## Kubernetes Deployment Example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: load-balancer
spec:
  replicas: 3
  selector:
    matchLabels:
      app: load-balancer
  template:
    metadata:
      labels:
        app: load-balancer
    spec:
      containers:
      - name: load-balancer
        image: load-balancer:latest
        ports:
        - containerPort: 8080
        env:
        - name: LB_SERVER_PORT
          value: "8080"
        - name: LB_BACKENDS
          value: "http://backend1:8081,http://backend2:8082"
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          limits:
            cpu: 1000m
            memory: 512Mi
          requests:
            cpu: 500m
            memory: 256Mi
```

This implementation fully complies with the 12-Factor App methodology, making it suitable for modern cloud-native deployments.
