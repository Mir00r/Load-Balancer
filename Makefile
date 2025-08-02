# 12-Factor App Compliant Load Balancer Makefile
.PHONY: build run test clean docker-build docker-run docker-stop dev-setup help deps lint format security audit

# Binary name
BINARY_NAME=load-balancer
DOCKER_IMAGE=load-balancer:latest

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Factor #5: Build, Release, Run - Strictly separate build and run stages
# Build flags for optimized binary
BUILD_FLAGS=-ldflags="-w -s" -a -installsuffix cgo

# Factor #1: Codebase tracking
GIT_COMMIT=$(shell git rev-parse --short HEAD)
BUILD_TIME=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION=$(shell git describe --tags --always --dirty)

# Enhanced build flags with version info
LDFLAGS=-ldflags="-w -s -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)"

# Default target
all: build

## Development Commands

# Factor #2: Dependencies - Install and verify dependencies
deps:
	@echo "Installing dependencies..."
	$(GOMOD) download
	$(GOMOD) verify
	$(GOMOD) tidy

# Build the application
build: deps
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o build/$(BINARY_NAME) cmd/server/main.go

# Factor #9: Disposability - Quick startup for development
dev: deps
	@echo "Starting development server..."
	@if [ -f .env ]; then export $$(cat .env | xargs); fi && \
	$(GOCMD) run cmd/server/main.go

# Factor #11: Logs - View logs in development
dev-logs:
	@echo "Following application logs..."
	tail -f logs/app.log 2>/dev/null || echo "No logs found. Start the application first."

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Factor #12: Admin Processes - Run one-off admin tasks
admin-health:
	@echo "Running health check..."
	./build/$(BINARY_NAME) -admin health-check

admin-stats:
	@echo "Getting backend statistics..."
	./build/$(BINARY_NAME) -admin stats

admin-config:
	@echo "Validating configuration..."
	./build/$(BINARY_NAME) -admin validate-config

# Code quality checks
lint:
	@echo "Running linter..."
	golangci-lint run

format:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

# Security audit
security:
	@echo "Running security audit..."
	gosec ./...

audit: security
	@echo "Running dependency audit..."
	$(GOCMD) list -json -m all | nancy sleuth

## Docker Commands (Factor #10: Dev/Prod Parity)

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE) .

# Run with Docker Compose (Factor #4: Backing Services)
docker-up:
	@echo "Starting services with Docker Compose..."
	docker-compose up -d

# View Docker logs (Factor #11: Logs as event streams)
docker-logs:
	@echo "Following Docker logs..."
	docker-compose logs -f

# Stop Docker services (Factor #9: Disposability)
docker-down:
	@echo "Stopping Docker services..."
	docker-compose down

# Factor #8: Concurrency - Scale services
docker-scale:
	@echo "Scaling backend services..."
	docker-compose up -d --scale backend1=2 --scale backend2=2 --scale backend3=2

## Environment Management

# Factor #3: Config - Set up development environment
dev-setup:
	@echo "Setting up development environment..."
	@if [ ! -f .env ]; then cp .env .env.local 2>/dev/null || echo "Created .env for local development"; fi
	@mkdir -p logs build
	@echo "Development environment ready!"

# Create production environment template
prod-env:
	@echo "Creating production environment template..."
	@cat > .env.production << 'EOF'
# Production Environment Configuration
LB_SERVER_HOST=0.0.0.0
LB_SERVER_PORT=8080
LB_LOG_LEVEL=warn
LB_LOG_FORMAT=json
LB_METRICS_ENABLED=true
# Add your production backends
LB_BACKENDS=
EOF
	@echo "Production environment template created (.env.production)"

## Backend Management

# Start backend servers for testing
start-backends:
	@echo "Starting backend servers..."
	@chmod +x scripts/start-backends.sh
	@./scripts/start-backends.sh

# Stop backend servers
stop-backends:
	@echo "Stopping backend servers..."
	@chmod +x scripts/stop-backends.sh
	@./scripts/stop-backends.sh

# Run load test
load-test:
	@echo "Running load test..."
	@chmod +x scripts/load-test.sh
	@./scripts/load-test.sh

## Monitoring and Health

# Factor #9: Disposability - Health checks
health:
	@echo "Checking application health..."
	curl -f http://localhost:8080/health || echo "Health check failed"

# Check readiness (Kubernetes style)
ready:
	@echo "Checking application readiness..."
	curl -f http://localhost:8080/ready || echo "Readiness check failed"

# View metrics
metrics:
	@echo "Fetching metrics..."
	curl -s http://localhost:8080/metrics | head -20

## Cleanup

clean:
	@echo "Cleaning up..."
	$(GOCLEAN)
	rm -rf build/ coverage.out coverage.html logs/*.log

clean-docker:
	@echo "Cleaning Docker resources..."
	docker-compose down --volumes --remove-orphans
	docker rmi $(DOCKER_IMAGE) 2>/dev/null || true

## Deployment

# Factor #5: Build, Release, Run - Create release
release: clean deps test lint security build
	@echo "Creating release $(VERSION)..."
	@mkdir -p releases
	@tar -czf releases/$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz -C build $(BINARY_NAME)
	@echo "Release created: releases/$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz"

# Install binary (Factor #12: Admin processes)
install: build
	@echo "Installing $(BINARY_NAME)..."
	sudo cp build/$(BINARY_NAME) /usr/local/bin/

# Install development tools
install-tools:
	@echo "Installing development tools..."
	$(GOCMD) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GOCMD) install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest

## Documentation

# Generate documentation
docs:
	@echo "Generating documentation..."
	godoc -http=:6060 &
	@echo "Documentation server started at http://localhost:6060"

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

## Help

help:
	@echo "Load Balancer - 12-Factor App Compliant"
	@echo ""
	@echo "Development Commands:"
	@echo "  deps           Install and verify dependencies"
	@echo "  build          Build the application"
	@echo "  dev            Start development server"
	@echo "  test           Run tests with coverage"
	@echo "  lint           Run code linter"
	@echo "  format         Format code"
	@echo "  security       Run security audit"
	@echo ""
	@echo "Docker Commands:"
	@echo "  docker-build   Build Docker image"
	@echo "  docker-up      Start with Docker Compose"
	@echo "  docker-down    Stop Docker services"
	@echo "  docker-logs    View Docker logs"
	@echo "  docker-scale   Scale backend services"
	@echo ""
	@echo "Environment:"
	@echo "  dev-setup      Set up development environment"
	@echo "  prod-env       Create production environment template"
	@echo ""
	@echo "Backend Management:"
	@echo "  start-backends Start test backend servers"
	@echo "  stop-backends  Stop test backend servers"
	@echo "  load-test      Run load test"
	@echo ""
	@echo "Health & Monitoring:"
	@echo "  health         Check application health"
	@echo "  ready          Check application readiness"
	@echo "  metrics        View application metrics"
	@echo ""
	@echo "Admin Processes:"
	@echo "  admin-health   Run health check admin process"
	@echo "  admin-stats    Get backend statistics"
	@echo "  admin-config   Validate configuration"
	@echo ""
	@echo "Cleanup:"
	@echo "  clean          Clean build artifacts"
	@echo "  clean-docker   Clean Docker resources"
	@echo ""
	@echo "Deployment:"
	@echo "  release        Create production release"
	@echo "  install        Install binary to system"
	@echo "  install-tools  Install development tools"
	@echo ""
	@echo "Documentation:"
	@echo "  docs           Generate documentation"
	@echo "  bench          Run benchmarks"
