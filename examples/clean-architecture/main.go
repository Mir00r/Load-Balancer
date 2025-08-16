// Package main demonstrates the Clean Architecture implementation
// This example shows how to use the dependency injection container
// and the various use cases to create a fully functional load balancer
// following modern software engineering principles.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mir00r/load-balancer/internal/container"
	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/ports"
)

func main() {
	// Create a context for the application
	ctx := context.Background()

	// Create container configuration
	config := &container.ContainerConfig{
		HealthCheckTimeout:      5 * time.Second,
		HealthCheckInterval:     30 * time.Second,
		DefaultStrategy:         "round_robin",
		DefaultStrategyConfig:   &ports.StrategyConfig{},
		DefaultTrafficPolicy:    &ports.TrafficPolicy{},
		MetricsRetentionPeriod:  24 * time.Hour,
		TracingEnabled:          true,
		EventBufferSize:         1000,
		BackendRepositoryConfig: make(map[string]interface{}),
		MetricsRepositoryConfig: make(map[string]interface{}),
	}

	// Create the dependency injection container
	appContainer := container.NewContainer(config)

	// Validate configuration
	if err := appContainer.ValidateConfiguration(config); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}

	// Start the container and all its services
	if err := appContainer.Start(ctx); err != nil {
		log.Fatalf("Failed to start container: %v", err)
	}
	defer func() {
		if err := appContainer.Stop(ctx); err != nil {
			log.Printf("Failed to stop container: %v", err)
		}
	}()

	// Demonstrate backend management
	if err := demonstrateBackendManagement(ctx, appContainer); err != nil {
		log.Printf("Backend management demo failed: %v", err)
	}

	// Demonstrate load balancing
	if err := demonstrateLoadBalancing(ctx, appContainer); err != nil {
		log.Printf("Load balancing demo failed: %v", err)
	}

	// Demonstrate metrics collection
	if err := demonstrateMetrics(ctx, appContainer); err != nil {
		log.Printf("Metrics demo failed: %v", err)
	}

	// Demonstrate configuration management
	if err := demonstrateConfigurationManagement(ctx, appContainer); err != nil {
		log.Printf("Configuration management demo failed: %v", err)
	}

	// Perform health check on the entire system
	if err := appContainer.HealthCheck(ctx); err != nil {
		log.Printf("System health check failed: %v", err)
	} else {
		log.Println("System health check passed")
	}

	log.Println("Clean Architecture demo completed successfully!")
}

// demonstrateBackendManagement shows how to manage backends using Clean Architecture.
func demonstrateBackendManagement(ctx context.Context, appContainer *container.Container) error {
	log.Println("=== Backend Management Demo ===")

	// Get the backend repository
	backendRepo := appContainer.GetBackendRepository()

	// Create some sample backends
	backends := []*domain.Backend{
		domain.NewBackend("backend-1", "http://localhost:8081", 1),
		domain.NewBackend("backend-2", "http://localhost:8082", 2),
		domain.NewBackend("backend-3", "http://localhost:8083", 1),
	}

	// Save backends
	for _, backend := range backends {
		if err := backendRepo.Save(ctx, backend); err != nil {
			return fmt.Errorf("failed to save backend %s: %w", backend.ID, err)
		}
		log.Printf("Saved backend: %s (%s)", backend.ID, backend.URL)
	}

	// Retrieve all backends
	allBackends, err := backendRepo.FindAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to retrieve backends: %w", err)
	}
	log.Printf("Total backends: %d", len(allBackends))

	// Get healthy backends
	healthyBackends, err := backendRepo.FindHealthy(ctx)
	if err != nil {
		return fmt.Errorf("failed to retrieve healthy backends: %w", err)
	}
	log.Printf("Healthy backends: %d", len(healthyBackends))

	// Update a backend
	backend := backends[0]
	backend.Weight = 5
	if err := backendRepo.Update(ctx, backend); err != nil {
		return fmt.Errorf("failed to update backend: %w", err)
	}
	log.Printf("Updated backend %s weight to %d", backend.ID, backend.Weight)

	return nil
}

// demonstrateLoadBalancing shows how to use the load balancing use case.
func demonstrateLoadBalancing(ctx context.Context, appContainer *container.Container) error {
	log.Println("=== Load Balancing Demo ===")

	// Get the load balancing use case
	loadBalancingUseCase := appContainer.GetLoadBalancingUseCase()

	// Create sample requests
	requests := []*ports.LoadBalancingRequest{
		{
			RequestID: "req-1",
			Method:    "GET",
			Path:      "/api/users",
			Headers:   map[string]string{"Content-Type": "application/json"},
			ClientIP:  "192.168.1.100",
			Timestamp: time.Now(),
		},
		{
			RequestID: "req-2",
			Method:    "POST",
			Path:      "/api/orders",
			Headers:   map[string]string{"Content-Type": "application/json"},
			ClientIP:  "192.168.1.101",
			Timestamp: time.Now(),
		},
		{
			RequestID: "req-3",
			Method:    "GET",
			Path:      "/api/products",
			Headers:   map[string]string{"Content-Type": "application/json"},
			ClientIP:  "192.168.1.102",
			Timestamp: time.Now(),
		},
	}

	// Process requests through load balancer
	for _, request := range requests {
		result, err := loadBalancingUseCase.ProcessRequest(ctx, request)
		if err != nil {
			log.Printf("Failed to process request %s: %v", request.RequestID, err)
			continue
		}

		if result.Backend != nil {
			log.Printf("Request %s -> Backend %s (Strategy: %s, Decision: %s)",
				request.RequestID, result.Backend.ID, result.Strategy, result.Decision)
		} else {
			log.Printf("Request %s rejected: %s", request.RequestID, result.Reason)
		}
	}

	// Get backend status
	statusReport, err := loadBalancingUseCase.GetBackendStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get backend status: %w", err)
	}
	log.Printf("Backend status: Total=%d, Healthy=%d", statusReport.Total, statusReport.Healthy)

	return nil
}

// demonstrateMetrics shows how to collect and query metrics.
func demonstrateMetrics(ctx context.Context, appContainer *container.Container) error {
	log.Println("=== Metrics Demo ===")

	// Get the metrics repository
	metricsRepo := appContainer.GetMetricsRepository()

	// Create sample metrics
	metrics := []*ports.Metric{
		{
			"name":      "request_count",
			"value":     100,
			"backend":   "backend-1",
			"timestamp": time.Now(),
		},
		{
			"name":      "response_time",
			"value":     250.5,
			"backend":   "backend-2",
			"timestamp": time.Now(),
		},
		{
			"name":      "error_count",
			"value":     5,
			"backend":   "backend-3",
			"timestamp": time.Now(),
		},
	}

	// Store metrics
	if err := metricsRepo.Store(ctx, metrics); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}
	log.Printf("Stored %d metrics", len(metrics))

	// Query metrics
	query := &ports.MetricQuery{
		"metric_name": "request_count",
		"from":        time.Now().Add(-1 * time.Hour),
		"to":          time.Now(),
	}

	snapshot, err := metricsRepo.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query metrics: %w", err)
	}
	log.Printf("Queried metrics: %v", (*snapshot)["timestamp"])

	// Aggregate metrics
	aggregation := &ports.MetricAggregation{
		"type":   "sum",
		"metric": "request_count",
		"period": "1h",
	}

	result, err := metricsRepo.Aggregate(ctx, aggregation)
	if err != nil {
		return fmt.Errorf("failed to aggregate metrics: %w", err)
	}
	log.Printf("Aggregated metrics: %v", (*result)["timestamp"])

	return nil
}

// demonstrateConfigurationManagement shows how to manage configuration.
func demonstrateConfigurationManagement(ctx context.Context, appContainer *container.Container) error {
	log.Println("=== Configuration Management Demo ===")

	// Get the configuration repository
	configRepo := appContainer.GetConfigurationRepository()

	// Load current configuration
	config, err := configRepo.LoadConfiguration(ctx)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	log.Printf("Current configuration loaded, metadata: %v", config.Metadata)

	// Update configuration
	config.Metadata["updated_at"] = time.Now()
	config.Metadata["version"] = "1.0.1"

	if err := configRepo.SaveConfiguration(ctx, config); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}
	log.Println("Configuration updated successfully")

	// Update container configuration
	updates := &container.ContainerConfigUpdates{
		HealthCheckTimeout:  timePtr(10 * time.Second),
		HealthCheckInterval: timePtr(60 * time.Second),
	}

	if err := appContainer.UpdateConfiguration(ctx, updates); err != nil {
		return fmt.Errorf("failed to update container configuration: %w", err)
	}
	log.Println("Container configuration updated")

	// Get current container configuration
	containerConfig := appContainer.GetConfiguration()
	log.Printf("Health check timeout: %v", containerConfig.HealthCheckTimeout)
	log.Printf("Health check interval: %v", containerConfig.HealthCheckInterval)

	return nil
}

// demonstrateStrategyFactory shows how to use different load balancing strategies.
func demonstrateStrategyFactory(ctx context.Context, appContainer *container.Container) error {
	log.Println("=== Strategy Factory Demo ===")

	// Get the strategy factory
	strategyFactory := appContainer.GetStrategyFactory()

	// Get available strategies
	strategies := strategyFactory.GetAvailableStrategies()
	log.Printf("Available strategies: %v", strategies)

	// Create different strategies
	for _, strategyType := range strategies {
		strategy, err := strategyFactory.CreateStrategy(strategyType, &ports.StrategyConfig{})
		if err != nil {
			log.Printf("Failed to create strategy %s: %v", strategyType, err)
			continue
		}

		log.Printf("Created strategy: %s (%s)", strategy.Name(), strategy.Type())

		// Get strategy stats
		stats := strategy.GetStats()
		log.Printf("Strategy stats: %v", stats)
	}

	return nil
}

// Helper function to create a pointer to time.Duration
func timePtr(d time.Duration) *time.Duration {
	return &d
}
