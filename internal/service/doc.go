/*
Package service implements the Application Service layer of the Load Balancer.

This package contains the business logic and orchestration for load balancing operations,
sitting between the domain layer and the infrastructure layers. It implements use cases
and application workflows while delegating domain logic to domain entities.

Key Components:

LoadBalancer Service:
The main orchestrator that coordinates backend selection, health checking, and metrics collection.

	loadBalancer := service.NewLoadBalancer(
		config,
		backendRepository,
		healthChecker,
		metrics,
		logger,
	)

	// Start the load balancer with health checking
	ctx := context.Background()
	if err := loadBalancer.Start(ctx); err != nil {
		log.Fatal("Failed to start load balancer:", err)
	}

Thread-Safe Strategy Implementations:
The service package provides production-ready, thread-safe implementations of load balancing strategies:

Round Robin Strategy:

	strategy := service.NewThreadSafeRoundRobinStrategy()
	backend, err := strategy.SelectBackend(ctx, backends, clientIP)

Weighted Round Robin Strategy:

	strategy := service.NewThreadSafeWeightedRoundRobinStrategy()
	backend, err := strategy.SelectBackend(ctx, backends, clientIP)

Least Connections Strategy:

	strategy := service.NewThreadSafeLeastConnectionsStrategy()
	backend, err := strategy.SelectBackend(ctx, backends, clientIP)

IP Hash Strategy:

	strategy := service.NewThreadSafeIPHashStrategy()
	backend, err := strategy.SelectBackend(ctx, backends, clientIP)

Health Checker Service:
Manages backend health monitoring with configurable policies:

	healthChecker := service.NewHealthChecker(
		domain.HealthCheckConfig{
			Enabled:            true,
			Interval:           30 * time.Second,
			Timeout:            5 * time.Second,
			Path:               "/health",
			HealthyThreshold:   2,
			UnhealthyThreshold: 3,
		},
		logger,
	)

Strategy Factory:
Creates and manages load balancing strategy instances:

	factory := service.NewDefaultStrategyFactory()
	strategy, err := factory.CreateAlgorithm(
		domain.RoundRobinStrategyType,
		domain.AlgorithmConfig{
			Name: "primary-strategy",
			Type: domain.RoundRobinStrategyType,
		},
	)

Strategy Statistics:
All strategies provide detailed statistics for monitoring and debugging:

	stats := strategy.GetStats()
	// Returns:
	// {
	//   "total_requests": 1000,
	//   "success_requests": 950,
	//   "failed_requests": 50,
	//   "last_used": 1638360000,
	//   "current_index": 5  // for round-robin
	// }

Metrics Service:
Collects and aggregates performance metrics:

	metrics := service.NewMetrics()
	metrics.IncrementRequests("backend-1")
	metrics.RecordLatency("backend-1", 150*time.Millisecond)
	metrics.IncrementErrors("backend-1", "timeout")

Thread Safety:
All service implementations are designed for high-concurrency environments:

Atomic Operations:
- Request counters use atomic.AddInt64() for thread-safe increments
- Index management uses atomic operations to prevent race conditions

Read-Write Mutexes:
- Protect complex data structures during updates
- Allow concurrent reads while ensuring exclusive writes

Lock-Free Algorithms:
- IP hash strategy uses no locks for maximum performance
- Statistics collection minimizes lock contention

Configuration Reload:
The config reload service enables hot configuration updates:

	reloadService := service.NewConfigReloadService(
		configPath,
		loadBalancer,
		logger,
	)

	// Watch for configuration changes
	go reloadService.WatchAndReload(ctx)

Error Handling:
Services use structured error handling with context:

	backend, err := loadBalancer.GetBackend(ctx)
	if err != nil {
		var lbErr *errors.LoadBalancerError
		if errors.As(err, &lbErr) {
			logger.WithFields(logrus.Fields{
				"error_code": lbErr.Code,
				"component":  lbErr.Component,
				"request_id": lbErr.RequestID,
			}).Error("Backend selection failed")
		}
	}

Performance Considerations:

Backend Selection Optimization:
- Pre-filtered backend lists to reduce iteration overhead
- Cached calculations for weighted strategies
- Efficient data structures for fast lookups

Memory Management:
- Object pooling for frequently allocated objects
- Careful management of goroutines and channels
- Periodic cleanup of unused resources

Monitoring Integration:
Services provide comprehensive observability:

Request Metrics:
- Total requests per backend
- Success/failure rates
- Response time percentiles
- Error categorization

Health Metrics:
- Backend availability statistics
- Health check success rates
- Recovery time tracking
- Circuit breaker state changes

Strategy Metrics:
- Algorithm performance characteristics
- Selection distribution fairness
- Load balancing effectiveness
- Failover behavior analysis

Best Practices:

1. Context Propagation: Always pass context for cancellation and timeouts
2. Graceful Degradation: Provide fallback behavior when components fail
3. Resource Cleanup: Ensure proper cleanup of goroutines and connections
4. Error Wrapping: Wrap errors with context for better debugging
5. Configuration Validation: Validate configuration at startup

Testing Strategies:

Unit Testing:

	func TestRoundRobinSelection(t *testing.T) {
		strategy := service.NewThreadSafeRoundRobinStrategy()
		backends := createTestBackends(3)

		// Test fair distribution
		selections := make(map[string]int)
		for i := 0; i < 300; i++ {
			backend, err := strategy.SelectBackend(ctx, backends, "")
			require.NoError(t, err)
			selections[backend.ID]++
		}

		// Verify even distribution
		assert.Equal(t, 100, selections["backend-1"])
		assert.Equal(t, 100, selections["backend-2"])
		assert.Equal(t, 100, selections["backend-3"])
	}

Concurrency Testing:

	func TestStrategyConcurrency(t *testing.T) {
		strategy := service.NewThreadSafeRoundRobinStrategy()
		backends := createTestBackends(3)

		var wg sync.WaitGroup
		const numGoroutines = 100
		const requestsPerGoroutine = 10

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < requestsPerGoroutine; j++ {
					_, err := strategy.SelectBackend(ctx, backends, "")
					assert.NoError(t, err)
				}
			}()
		}

		wg.Wait()

		stats := strategy.GetStats()
		assert.Equal(t, int64(1000), stats["total_requests"])
	}

Integration Testing:

	func TestLoadBalancerIntegration(t *testing.T) {
		// Setup complete load balancer with real components
		config := createTestConfig()
		repo := repository.NewInMemoryBackendRepository()
		healthChecker := service.NewHealthChecker(config.HealthCheck, logger)
		metrics := service.NewMetrics()

		lb := service.NewLoadBalancer(config, repo, healthChecker, metrics, logger)

		// Add backends
		backends := createTestBackends(3)
		for _, backend := range backends {
			require.NoError(t, lb.AddBackend(backend))
		}

		// Start load balancer
		ctx := context.Background()
		require.NoError(t, lb.Start(ctx))
		defer lb.Stop(ctx)

		// Test backend selection
		for i := 0; i < 100; i++ {
			backend, err := lb.GetBackend(ctx)
			require.NoError(t, err)
			assert.NotNil(t, backend)
		}
	}

Package Structure:
- load_balancer.go: Main load balancer service implementation
- strategies.go: Thread-safe strategy implementations
- health_checker.go: Backend health monitoring service
- metrics.go: Metrics collection and aggregation
- config_reload.go: Configuration hot-reload functionality

This package serves as the application core, orchestrating domain logic
and providing the primary interfaces for the handler and infrastructure layers.
*/
package service
