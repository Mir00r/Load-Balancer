# 🏗️ Load Balancer Architecture Overview

## 📁 Project Structure

```
Load-Balancer/
├── cmd/
│   └── server/
│       └── main.go                    # Application entry point
├── internal/
│   ├── config/
│   │   └── config.go                  # Configuration management
│   ├── domain/
│   │   └── types.go                   # Business entities & interfaces
│   ├── handler/
│   │   └── load_balancer.go           # HTTP request handlers
│   ├── middleware/
│   │   ├── circuit_breaker.go         # Circuit breaker implementation
│   │   ├── common.go                  # Common middleware (logging, CORS, etc.)
│   │   └── rate_limiter.go            # Rate limiting middleware
│   ├── repository/
│   │   └── backend_repository.go      # Backend data management
│   └── service/
│       ├── health_checker.go          # Health checking service
│       ├── load_balancer.go           # Load balancing strategies
│       ├── load_balancer_test.go      # Unit tests
│       └── metrics.go                 # Metrics collection
├── pkg/
│   └── logger/
│       └── logger.go                  # Structured logging
├── examples/
│   └── backend/
│       ├── main.go                    # Test backend server
│       └── Dockerfile                 # Backend Docker image
├── scripts/
│   ├── start-backends.sh              # Start test backends
│   ├── stop-backends.sh               # Stop test backends
│   └── load-test.sh                   # Load testing script
├── config.yaml                        # Default configuration
├── docker-compose.yml                 # Docker Compose setup
├── Dockerfile                         # Load balancer Docker image
├── Makefile                          # Build and development tasks
├── go.mod                            # Go module definition
├── README.md                         # Main documentation
├── USAGE.md                          # Usage guide
└── .gitignore                        # Git ignore patterns
```

## 🏛️ Architecture Components

### 1. Domain Layer (`internal/domain/`)
- **Purpose**: Defines business entities and interfaces
- **Key Components**:
  - `Backend`: Represents a backend server with health status and metrics
  - `LoadBalancer` interface: Defines load balancing operations
  - `HealthChecker` interface: Defines health checking operations
  - `Metrics` interface: Defines metrics collection operations
  - Configuration types and enums

### 2. Service Layer (`internal/service/`)
- **Purpose**: Implements business logic and strategies
- **Key Components**:
  - `LoadBalancer`: Main orchestrator with pluggable strategies
  - `HealthChecker`: Concurrent health monitoring
  - `Metrics`: Real-time metrics collection and aggregation
  - Load balancing strategies (Round Robin, Weighted, Least Connections)

### 3. Repository Layer (`internal/repository/`)
- **Purpose**: Data access and persistence abstraction
- **Key Components**:
  - `InMemoryBackendRepository`: Thread-safe in-memory storage
  - Backend lifecycle management (CRUD operations)
  - Query methods for filtering backends by status

### 4. Handler Layer (`internal/handler/`)
- **Purpose**: HTTP request handling and API endpoints
- **Key Components**:
  - `LoadBalancerHandler`: Main request proxying logic
  - Management endpoints (health, metrics, backends)
  - Request retry logic with exponential backoff

### 5. Middleware Layer (`internal/middleware/`)
- **Purpose**: Cross-cutting concerns and request/response processing
- **Key Components**:
  - `RateLimiter`: Token bucket rate limiting per client IP
  - `CircuitBreaker`: Fault tolerance with state management
  - Common middleware (logging, recovery, CORS, security headers)

### 6. Configuration Layer (`internal/config/`)
- **Purpose**: Configuration management and validation
- **Key Components**:
  - YAML-based configuration with defaults
  - Environment variable overrides
  - Comprehensive validation with error reporting

### 7. Logging Package (`pkg/logger/`)
- **Purpose**: Structured logging with context
- **Key Components**:
  - JSON/text formatters
  - Multiple output destinations
  - Component-specific logger creation

## 🔄 Request Flow

```
1. HTTP Request → Middleware Chain
   ↓
2. Rate Limiter → Check client IP limits
   ↓
3. Circuit Breaker → Check service health
   ↓
4. Load Balancer Handler → Select backend
   ↓
5. Backend Selection Strategy → Round Robin/Weighted/Least Connections
   ↓
6. Reverse Proxy → Forward to backend
   ↓
7. Response → Back through middleware chain
   ↓
8. Metrics Collection → Record latency, success/failure
```

## 🧠 Design Patterns Applied

### 1. **Strategy Pattern**
- **Location**: Load balancing algorithms
- **Benefit**: Easy to add new load balancing strategies
- **Implementation**: `LoadBalancingStrategy` interface with multiple implementations

### 2. **Repository Pattern**
- **Location**: Backend data management
- **Benefit**: Abstracts data storage, enables testing
- **Implementation**: `BackendRepository` interface with in-memory implementation

### 3. **Dependency Injection**
- **Location**: Service initialization
- **Benefit**: Loose coupling, easier testing
- **Implementation**: Constructor injection in all services

### 4. **Circuit Breaker Pattern**
- **Location**: Fault tolerance middleware
- **Benefit**: Prevents cascade failures
- **Implementation**: State machine with configurable thresholds

### 5. **Observer Pattern**
- **Location**: Health checking
- **Benefit**: Reactive health status updates
- **Implementation**: Concurrent health checkers updating backend status

### 6. **Factory Pattern**
- **Location**: Component creation
- **Benefit**: Centralized object creation
- **Implementation**: `New*` constructors with validation

## 🔧 Key Features

### 1. **Load Balancing Strategies**
- **Round Robin**: O(1) selection with atomic counters
- **Weighted Round Robin**: Proportional distribution based on weights
- **Least Connections**: Dynamic selection based on active connections

### 2. **Health Checking**
- **Concurrent**: Each backend monitored independently
- **Configurable**: Customizable intervals, timeouts, thresholds
- **Automatic Recovery**: Failed backends automatically restored

### 3. **Fault Tolerance**
- **Circuit Breaker**: Prevents requests to failed backends
- **Retry Logic**: Exponential backoff with configurable limits
- **Graceful Degradation**: Continues operating with remaining healthy backends

### 4. **Observability**
- **Structured Logging**: JSON logs with request tracing
- **Metrics Collection**: Request counts, latencies, error rates
- **Health Endpoints**: Real-time status monitoring

### 5. **Performance**
- **Thread Safety**: Lock-free operations where possible
- **Connection Pooling**: Efficient HTTP client reuse
- **Minimal Latency**: Direct reverse proxy forwarding

## 🚀 Production Features

### 1. **Scalability**
- **Horizontal**: Add/remove backends dynamically
- **Vertical**: Efficient resource utilization
- **Load Distribution**: Multiple balancing algorithms

### 2. **Reliability**
- **Zero-Downtime**: Graceful shutdown handling
- **Error Isolation**: Failed backends don't affect others
- **Self-Healing**: Automatic backend recovery

### 3. **Security**
- **Rate Limiting**: Prevent abuse and DoS attacks
- **Security Headers**: CORS, XSS protection, etc.
- **Input Validation**: Comprehensive request validation

### 4. **Monitoring**
- **Real-time Metrics**: Live performance data
- **Health Dashboards**: Status visualization
- **Alerting Ready**: Structured data for monitoring systems

### 5. **Configuration**
- **Hot Reload**: Configuration updates without restart
- **Environment Overrides**: Flexible deployment options
- **Validation**: Prevent misconfigurations

## 🧪 Testing Strategy

### 1. **Unit Tests**
- Strategy pattern implementations
- Repository operations
- Metrics collection
- Configuration validation

### 2. **Integration Tests**
- Health checker with real backends
- Load balancer with multiple strategies
- Middleware chain processing

### 3. **Load Tests**
- Performance under high concurrency
- Resource utilization monitoring
- Failure scenario testing

### 4. **Manual Tests**
- API endpoint validation
- Docker deployment verification
- Configuration testing

## 📊 Metrics and Monitoring

### 1. **Request Metrics**
- Total requests and errors
- Response time distributions
- Success rates per backend

### 2. **Backend Metrics**
- Health status
- Active connections
- Request counts and latencies

### 3. **System Metrics**
- Circuit breaker states
- Rate limiter statistics
- Memory and CPU usage

### 4. **Business Metrics**
- Load distribution fairness
- Backend utilization rates
- Error rate trends

This architecture provides a production-ready, scalable, and maintainable load balancer implementation with comprehensive features for modern distributed systems.
