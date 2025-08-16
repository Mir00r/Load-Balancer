// Package container provides dependency injection for the Clean Architecture implementation.
// This package follows the Dependency Injection pattern to wire together all the components
// of the application while respecting the dependency inversion principle.
//
// The container:
// - Manages all service dependencies and their lifecycles
// - Provides centralized configuration of dependencies
// - Implements the Composition Root pattern
// - Ensures proper dependency injection throughout the application
// - Follows the Single Responsibility Principle for dependency management
package container

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/application"
	"github.com/mir00r/load-balancer/internal/infrastructure"
	"github.com/mir00r/load-balancer/internal/ports"
	"github.com/mir00r/load-balancer/internal/stubs"
)

// ========================================
// DEPENDENCY INJECTION CONTAINER
// ========================================

// Container manages all application dependencies following Clean Architecture.
// This implements the Composition Root pattern and provides centralized
// dependency management with proper lifecycle management.
type Container struct {
	// Infrastructure dependencies (Secondary Adapters)
	backendRepo     ports.BackendRepository
	metricsRepo     ports.MetricsRepository
	configRepo      ports.ConfigurationRepository
	healthChecker   ports.HealthChecker
	eventPublisher  ports.EventPublisher
	strategyFactory ports.StrategyFactory

	// Application services (Use Cases)
	loadBalancingUseCase     application.LoadBalancingUseCase
	routingUseCase           application.RoutingUseCase
	trafficManagementUseCase application.TrafficManagementUseCase
	healthManagementUseCase  application.HealthManagementUseCase

	// Configuration
	config *ContainerConfig

	// Lifecycle management
	mutex     sync.RWMutex
	isStarted bool
}

// ContainerConfig holds configuration for the dependency injection container.
type ContainerConfig struct {
	// Health check configuration
	HealthCheckTimeout  time.Duration `yaml:"health_check_timeout"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`

	// Load balancing configuration
	DefaultStrategy       string                `yaml:"default_strategy"`
	DefaultStrategyConfig *ports.StrategyConfig `yaml:"default_strategy_config"`

	// Traffic management configuration
	DefaultTrafficPolicy *ports.TrafficPolicy `yaml:"default_traffic_policy"`

	// Observability configuration
	MetricsRetentionPeriod time.Duration `yaml:"metrics_retention_period"`
	TracingEnabled         bool          `yaml:"tracing_enabled"`

	// Event publishing configuration
	EventBufferSize int `yaml:"event_buffer_size"`

	// Repository configurations
	BackendRepositoryConfig map[string]interface{} `yaml:"backend_repository_config"`
	MetricsRepositoryConfig map[string]interface{} `yaml:"metrics_repository_config"`
}

// DefaultContainerConfig returns a default configuration for the container.
func DefaultContainerConfig() *ContainerConfig {
	return &ContainerConfig{
		HealthCheckTimeout:      10 * time.Second,
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
}

// NewContainer creates a new dependency injection container.
// This is the main composition root where all dependencies are wired together.
func NewContainer(config *ContainerConfig) *Container {
	if config == nil {
		config = DefaultContainerConfig()
	}

	container := &Container{
		config: config,
	}

	// Initialize the container with all dependencies
	container.initializeInfrastructure()
	container.initializeApplicationServices()

	return container
}

// ========================================
// INFRASTRUCTURE INITIALIZATION
// ========================================

// initializeInfrastructure sets up all infrastructure dependencies (secondary adapters).
func (c *Container) initializeInfrastructure() {
	// Initialize repositories
	c.backendRepo = infrastructure.NewInMemoryBackendRepository()
	c.metricsRepo = infrastructure.NewInMemoryMetricsRepository()
	c.configRepo = infrastructure.NewInMemoryConfigurationRepository()

	// Initialize health checker
	c.healthChecker = infrastructure.NewHTTPHealthChecker(c.config.HealthCheckTimeout)

	// Initialize event publisher
	c.eventPublisher = infrastructure.NewInMemoryEventPublisher()

	// Initialize strategy factory
	c.strategyFactory = infrastructure.NewDefaultStrategyFactory()
}

// ========================================
// APPLICATION SERVICES INITIALIZATION
// ========================================

// initializeApplicationServices sets up all application services (use cases).
func (c *Container) initializeApplicationServices() {
	// Initialize stub services for demonstration
	routingService := stubs.NewStubRoutingService()
	trafficService := stubs.NewStubTrafficService()
	observabilityService := stubs.NewStubObservabilityService()

	// Load balancing configuration
	loadBalancingConfig := &application.LoadBalancingConfig{
		Strategy:       c.config.DefaultStrategy,
		StrategyConfig: c.config.DefaultStrategyConfig,
		TrafficPolicy:  c.config.DefaultTrafficPolicy,
	}

	// Initialize load balancing use case with all its dependencies
	c.loadBalancingUseCase = application.NewLoadBalancingService(
		c.backendRepo,
		routingService,
		trafficService,
		observabilityService,
		c.strategyFactory,
		c.eventPublisher,
		loadBalancingConfig,
	)

	// Initialize routing use case
	c.routingUseCase = application.NewRoutingService(
		routingService,
		c.configRepo,
		c.eventPublisher,
		observabilityService,
	)

	// TODO: Initialize traffic management and health management use cases
	// These would require additional service implementations
}

// ========================================
// PUBLIC INTERFACE METHODS
// ========================================

// GetLoadBalancingUseCase returns the load balancing use case.
func (c *Container) GetLoadBalancingUseCase() application.LoadBalancingUseCase {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.loadBalancingUseCase
}

// GetRoutingUseCase returns the routing use case.
func (c *Container) GetRoutingUseCase() application.RoutingUseCase {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.routingUseCase
}

// GetTrafficManagementUseCase returns the traffic management use case.
func (c *Container) GetTrafficManagementUseCase() application.TrafficManagementUseCase {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.trafficManagementUseCase
}

// GetHealthManagementUseCase returns the health management use case.
func (c *Container) GetHealthManagementUseCase() application.HealthManagementUseCase {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.healthManagementUseCase
}

// ========================================
// REPOSITORY ACCESS METHODS
// ========================================

// GetBackendRepository returns the backend repository.
func (c *Container) GetBackendRepository() ports.BackendRepository {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.backendRepo
}

// GetMetricsRepository returns the metrics repository.
func (c *Container) GetMetricsRepository() ports.MetricsRepository {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.metricsRepo
}

// GetConfigurationRepository returns the configuration repository.
func (c *Container) GetConfigurationRepository() ports.ConfigurationRepository {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.configRepo
}

// GetHealthChecker returns the health checker.
func (c *Container) GetHealthChecker() ports.HealthChecker {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.healthChecker
}

// GetEventPublisher returns the event publisher.
func (c *Container) GetEventPublisher() ports.EventPublisher {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.eventPublisher
}

// GetStrategyFactory returns the strategy factory.
func (c *Container) GetStrategyFactory() ports.StrategyFactory {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.strategyFactory
}

// ========================================
// LIFECYCLE MANAGEMENT
// ========================================

// Start starts all the services and background processes.
// This method implements the Application Lifecycle pattern.
func (c *Container) Start(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.isStarted {
		return fmt.Errorf("container is already started")
	}

	// Start health checker
	if err := c.healthChecker.StartPeriodicChecks(ctx, c.config.HealthCheckInterval); err != nil {
		return fmt.Errorf("failed to start health checker: %w", err)
	}

	// Load initial configuration
	if err := c.loadInitialConfiguration(ctx); err != nil {
		return fmt.Errorf("failed to load initial configuration: %w", err)
	}

	// TODO: Start other background services as needed

	c.isStarted = true
	return nil
}

// Stop stops all the services and cleans up resources.
func (c *Container) Stop(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.isStarted {
		return nil // Already stopped
	}

	// Stop health checker
	if err := c.healthChecker.StopPeriodicChecks(ctx); err != nil {
		return fmt.Errorf("failed to stop health checker: %w", err)
	}

	// TODO: Stop other services and clean up resources

	c.isStarted = false
	return nil
}

// IsStarted returns whether the container is started.
func (c *Container) IsStarted() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.isStarted
}

// ========================================
// CONFIGURATION MANAGEMENT
// ========================================

// loadInitialConfiguration loads the initial application configuration.
func (c *Container) loadInitialConfiguration(ctx context.Context) error {
	// Create default configuration
	defaultConfig := &ports.Configuration{
		Backends: []*ports.BackendConfig{},
		LoadBalancing: &ports.LoadBalancingConfig{
			"strategy": c.config.DefaultStrategy,
			"config":   c.config.DefaultStrategyConfig,
		},
		Traffic: &ports.TrafficConfig{
			"policies": []interface{}{c.config.DefaultTrafficPolicy},
		},
		Observability: &ports.ObservabilityConfig{
			"metrics_retention": c.config.MetricsRetentionPeriod.String(),
			"tracing_enabled":   c.config.TracingEnabled,
		},
		Metadata: map[string]any{
			"created_at": time.Now(),
			"version":    "1.0.0",
		},
	}

	// Save default configuration
	return c.configRepo.SaveConfiguration(ctx, defaultConfig)
}

// UpdateConfiguration updates the container configuration.
func (c *Container) UpdateConfiguration(ctx context.Context, updates *ContainerConfigUpdates) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Apply configuration updates
	if updates.HealthCheckTimeout != nil {
		c.config.HealthCheckTimeout = *updates.HealthCheckTimeout
	}
	if updates.HealthCheckInterval != nil {
		c.config.HealthCheckInterval = *updates.HealthCheckInterval
	}
	if updates.DefaultStrategy != nil {
		c.config.DefaultStrategy = *updates.DefaultStrategy
	}
	if updates.DefaultStrategyConfig != nil {
		c.config.DefaultStrategyConfig = updates.DefaultStrategyConfig
	}

	// Update health checker policy if needed
	if updates.HealthCheckTimeout != nil || updates.HealthCheckInterval != nil {
		policy := &ports.HealthCheckPolicy{
			Timeout:  c.config.HealthCheckTimeout,
			Interval: c.config.HealthCheckInterval,
		}
		return c.healthChecker.SetHealthCheckPolicy(ctx, policy)
	}

	return nil
}

// GetConfiguration returns the current container configuration.
func (c *Container) GetConfiguration() *ContainerConfig {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// Return a copy to prevent external modifications
	configCopy := *c.config
	return &configCopy
}

// ========================================
// CONFIGURATION UPDATE TYPES
// ========================================

// ContainerConfigUpdates represents partial updates to container configuration.
type ContainerConfigUpdates struct {
	HealthCheckTimeout    *time.Duration        `yaml:"health_check_timeout,omitempty"`
	HealthCheckInterval   *time.Duration        `yaml:"health_check_interval,omitempty"`
	DefaultStrategy       *string               `yaml:"default_strategy,omitempty"`
	DefaultStrategyConfig *ports.StrategyConfig `yaml:"default_strategy_config,omitempty"`
	DefaultTrafficPolicy  *ports.TrafficPolicy  `yaml:"default_traffic_policy,omitempty"`
}

// ========================================
// VALIDATION METHODS
// ========================================

// ValidateConfiguration validates the container configuration.
func (c *Container) ValidateConfiguration(config *ContainerConfig) error {
	if config.HealthCheckTimeout <= 0 {
		return fmt.Errorf("health check timeout must be positive")
	}
	if config.HealthCheckInterval <= 0 {
		return fmt.Errorf("health check interval must be positive")
	}
	if config.DefaultStrategy == "" {
		return fmt.Errorf("default strategy must be specified")
	}
	if config.MetricsRetentionPeriod <= 0 {
		return fmt.Errorf("metrics retention period must be positive")
	}
	if config.EventBufferSize <= 0 {
		return fmt.Errorf("event buffer size must be positive")
	}

	// Validate strategy configuration
	strategyType := ports.StrategyType(config.DefaultStrategy)
	return c.strategyFactory.ValidateConfig(strategyType, config.DefaultStrategyConfig)
}

// ========================================
// UTILITY METHODS
// ========================================

// Reset resets the container to its initial state.
// This is primarily useful for testing scenarios.
func (c *Container) Reset(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Stop if running
	if c.isStarted {
		if err := c.Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop container: %w", err)
		}
	}

	// Reinitialize all dependencies
	c.initializeInfrastructure()
	c.initializeApplicationServices()

	return nil
}

// HealthCheck performs a health check on the container and all its dependencies.
func (c *Container) HealthCheck(ctx context.Context) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if !c.isStarted {
		return fmt.Errorf("container is not started")
	}

	// Check if all required dependencies are available
	if c.backendRepo == nil {
		return fmt.Errorf("backend repository is not initialized")
	}
	if c.metricsRepo == nil {
		return fmt.Errorf("metrics repository is not initialized")
	}
	if c.configRepo == nil {
		return fmt.Errorf("configuration repository is not initialized")
	}
	if c.healthChecker == nil {
		return fmt.Errorf("health checker is not initialized")
	}
	if c.eventPublisher == nil {
		return fmt.Errorf("event publisher is not initialized")
	}
	if c.strategyFactory == nil {
		return fmt.Errorf("strategy factory is not initialized")
	}

	// TODO: Perform deeper health checks on each component

	return nil
}

// GetStats returns statistics about the container and its components.
func (c *Container) GetStats() map[string]interface{} {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return map[string]interface{}{
		"is_started":           c.isStarted,
		"config":               c.config,
		"available_strategies": c.strategyFactory.GetAvailableStrategies(),
		"timestamp":            time.Now(),
	}
}
