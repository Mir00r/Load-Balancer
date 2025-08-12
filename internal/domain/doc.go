/*
Package domain contains the core business entities and interfaces for the Load Balancer.

This package implements the Domain layer of Clean Architecture, providing:
- Core business entities (Backend, LoadBalancer)
- Business interfaces (LoadBalancer, BackendRepository, Metrics)
- Domain services and value objects
- Business rules and domain logic

The domain package is independent of external frameworks and infrastructure,
ensuring the business logic remains testable and maintainable.

Key Components:

Backend Entity:
Backend represents a server instance that can handle requests. It maintains
its own state including health status, connection counts, and failure tracking.
All state modifications are thread-safe using atomic operations.

	backend := domain.NewBackend("api-1", "http://api1.example.com", 100)
	backend.IncrementActiveConnections()
	if backend.IsHealthy() && backend.IsAvailable() {
		// Route traffic to this backend
	}

Load Balancing Strategies:
The package defines interfaces for different load balancing algorithms:
- Round Robin: Distributes requests evenly across backends
- Weighted Round Robin: Considers backend weights for distribution
- Least Connections: Routes to backend with fewest active connections
- IP Hash: Provides session affinity based on client IP

Strategy Configuration:
Load balancing strategies can be configured using the AlgorithmConfig:

	config := domain.AlgorithmConfig{
		Name: "weighted-api-servers",
		Type: domain.WeightedRoundRobinStrategyType,
		Weights: map[string]int{
			"api-1": 3,
			"api-2": 2,
			"api-3": 1,
		},
	}

Backend Filtering:
The package provides flexible backend filtering capabilities:

	healthyFilter := &domain.HealthyBackendFilter{}
	weightedFilter := &domain.WeightedBackendFilter{MinWeight: 50}
	compositeFilter := domain.NewCompositeBackendFilter(healthyFilter, weightedFilter)

	availableBackends := compositeFilter.Filter(allBackends)

Health Checking:
Backend health is managed through configurable health check policies:

	healthConfig := domain.HealthCheckConfig{
		Enabled:            true,
		Interval:           30 * time.Second,
		Timeout:            5 * time.Second,
		Path:               "/health",
		HealthyThreshold:   2,
		UnhealthyThreshold: 3,
	}

Thread Safety:
All domain entities are designed to be thread-safe for concurrent access:
- Atomic operations for counters and state changes
- Read-write mutexes for complex state management
- Immutable value objects where possible

Error Handling:
The package uses structured error types for better error handling:

	backend, err := loadBalancer.GetBackend(ctx)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrNoBackendsAvailable):
			// Handle no backends scenario
		case errors.Is(err, domain.ErrAllBackendsUnhealthy):
			// Handle unhealthy backends scenario
		}
	}

Configuration Management:
Domain entities support configuration through structured config objects:

	lbConfig := domain.LoadBalancerConfig{
		Strategy:       domain.RoundRobinStrategy,
		MaxRetries:     3,
		RetryDelay:     100 * time.Millisecond,
		Timeout:        30 * time.Second,
		HealthCheck:    healthConfig,
		RateLimit:      rateLimitConfig,
		CircuitBreaker: circuitBreakerConfig,
	}

Metrics and Observability:
The domain layer defines interfaces for metrics collection:

	type Metrics interface {
		IncrementRequests(backendID string)
		RecordLatency(backendID string, duration time.Duration)
		IncrementErrors(backendID string, errorType string)
		GetMetrics() map[string]interface{}
	}

This design allows for pluggable metrics implementations while keeping
the domain logic independent of specific metrics frameworks.

Best Practices:

1. Immutability: Prefer immutable value objects for configuration
2. Interface Segregation: Keep interfaces focused and cohesive
3. Dependency Inversion: Depend on abstractions, not concretions
4. Single Responsibility: Each entity has a single, well-defined purpose
5. Open/Closed Principle: Open for extension, closed for modification

Testing:
The domain package is designed for comprehensive testing:

	func TestBackendHealthManagement(t *testing.T) {
		backend := domain.NewBackend("test", "http://test.com", 100)

		// Test health transitions
		backend.SetStatus(domain.StatusUnhealthy)
		assert.False(t, backend.IsHealthy())

		// Test thread safety
		go backend.IncrementActiveConnections()
		go backend.DecrementActiveConnections()

		// Verify final state
		assert.Equal(t, int64(0), backend.GetActiveConnections())
	}

Package Structure:
- types.go: Core domain entities and value objects
- strategy.go: Load balancing strategy interfaces and implementations
- interfaces.go: Domain service interfaces
- errors.go: Domain-specific error types
- events.go: Domain events for observability

This package serves as the foundation for the entire load balancer system,
providing a stable, well-tested core that other layers can depend upon.
*/
package domain
