# Load Balancer

A production-ready load balancer implementation in Go with advanced features including multiple load balancing strategies, health checking, circuit breaker pattern, and comprehensive monitoring.

## Features

### Load Balancing Strategies
- **Round Robin**: Distributes requests evenly across backends
- **Weighted Round Robin**: Considers backend weights for proportional distribution  
- **Least Connections**: Routes to backend with fewest active connections

### Advanced Health Checking
- Configurable health check endpoints
- Customizable intervals, timeouts, and thresholds
- Automatic backend recovery and status monitoring
- Concurrent health checks for better performance

### Production-Ready Features
- Graceful shutdown handling
- Comprehensive structured logging
- Rate limiting middleware
- Circuit breaker pattern
- Request metrics and monitoring
- Docker support with multi-stage builds

## Architecture

```
├── cmd/server/          # Application entry point
├── internal/
│   ├── config/         # Configuration management
│   ├── domain/         # Business entities & interfaces
│   ├── handler/        # HTTP request handlers
│   ├── middleware/     # HTTP middleware components
│   ├── repository/     # Data access layer
│   └── service/        # Business logic & strategies
├── pkg/logger/         # Shared logging package
├── examples/           # Test backend servers
└── scripts/           # Testing and deployment scripts
```

## Design Patterns Applied

- **Strategy Pattern**: For load balancing algorithms
- **Repository Pattern**: For backend data management
- **Dependency Injection**: For loose coupling
- **Circuit Breaker**: For fault tolerance
- **Clean Architecture**: Clear separation of concerns

## Code Quality Features

- Comprehensive error handling
- Thread-safe operations with proper mutex usage
- Interface-based design for extensibility
- Detailed code comments and documentation
- Industry-standard naming conventions

## Quick Start

1. **Configure backends in config.yaml**
2. **Choose load balancing strategy**
3. **Set health check parameters**
4. **Run with `make run`**

## Usage Examples

### Basic Setup
```bash
# Configure backends in config.yaml
# Choose load balancing strategy
# Set health check parameters
make run
```

### Testing
```bash
# Start test backends
make start-backends

# Run load tests
make load-test

# Docker deployment
make docker-up
```

### Monitoring
- Health endpoint: `GET /health`
- Structured JSON logging
- Request timing and metrics
- Backend status tracking

## Architecture & Design Patterns

- **Single Responsibility Principle**: Each struct has a clear, single purpose
- **Dependency Injection**: Components are properly initialized and dependencies injected
- **Factory Pattern**: `NewBackend()` and `NewLoadBalancer()` constructors
- **Strategy Pattern**: Easily extensible for different load balancing algorithms

## Thread Safety & Concurrency

- **RWMutex Usage**: Proper read/write locks for shared state
- **Atomic Operations**: Lock-free counter increments for performance
- **Context Propagation**: Clean request-scoped data handling
- **Goroutine Safety**: All operations are thread-safe

## Production Features

- **Health Checks**: Concurrent health checking with configurable intervals
- **Error Handling**: Comprehensive error handling with proper logging
- **Retry Logic**: Exponential backoff with configurable retry limits
- **Graceful Shutdown**: Proper server shutdown handling
- **Resource Management**: Proper connection cleanup and resource disposal

## Performance Optimizations

- **Efficient Round-Robin**: O(1) next backend selection
- **Concurrent Health Checks**: Parallel health check execution
- **Connection Pooling**: Leverages Go's built-in HTTP connection pooling
- **Minimal Locking**: Reduced lock contention with atomic operations

## Reliability & Resilience

- **Circuit Breaker Pattern**: Backends marked down after failures
- **Request Timeouts**: Configurable timeouts for all operations
- **Failure Isolation**: Failed backends don't affect others
- **Automatic Recovery**: Health checks restore failed backends

## Code Quality

- **Comprehensive Comments**: Every function and important section documented
- **Consistent Naming**: Clear, descriptive variable and function names
- **Error Wrapping**: Proper error context with `fmt.Errorf`
- **Configuration Constants**: All magic numbers replaced with named constants
