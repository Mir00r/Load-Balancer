// Package infrastructure contains the infrastructure layer of the Clean Architecture.
// This layer implements the secondary ports (adapters) and provides concrete
// implementations for external systems like databases, message queues, HTTP clients, etc.
//
// The infrastructure layer:
// - Implements secondary ports (outbound adapters)
// - Handles external system integrations
// - Contains framework-specific code
// - Implements persistence, messaging, and external service adapters
// - Should not contain business logic
package infrastructure

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/ports"
)

// ========================================
// BACKEND REPOSITORY ADAPTER
// ========================================

// InMemoryBackendRepository implements BackendRepository using in-memory storage.
// This is a simple implementation for demonstration/testing purposes.
// In production, this would be replaced with a database adapter.
type InMemoryBackendRepository struct {
	backends map[string]*domain.Backend
	mutex    sync.RWMutex
}

// NewInMemoryBackendRepository creates a new in-memory backend repository.
func NewInMemoryBackendRepository() ports.BackendRepository {
	return &InMemoryBackendRepository{
		backends: make(map[string]*domain.Backend),
	}
}

// FindAll returns all backends.
func (r *InMemoryBackendRepository) FindAll(ctx context.Context) ([]*domain.Backend, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	backends := make([]*domain.Backend, 0, len(r.backends))
	for _, backend := range r.backends {
		backends = append(backends, backend)
	}
	return backends, nil
}

// FindByID finds a backend by ID.
func (r *InMemoryBackendRepository) FindByID(ctx context.Context, id string) (*domain.Backend, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	backend, exists := r.backends[id]
	if !exists {
		return nil, fmt.Errorf("backend with ID %s not found", id)
	}
	return backend, nil
}

// FindHealthy returns all healthy backends.
func (r *InMemoryBackendRepository) FindHealthy(ctx context.Context) ([]*domain.Backend, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var healthyBackends []*domain.Backend
	for _, backend := range r.backends {
		if backend.IsHealthy() {
			healthyBackends = append(healthyBackends, backend)
		}
	}
	return healthyBackends, nil
}

// Save saves a backend.
func (r *InMemoryBackendRepository) Save(ctx context.Context, backend *domain.Backend) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.backends[backend.ID] = backend
	return nil
}

// Update updates a backend.
func (r *InMemoryBackendRepository) Update(ctx context.Context, backend *domain.Backend) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.backends[backend.ID]; !exists {
		return fmt.Errorf("backend with ID %s not found", backend.ID)
	}
	r.backends[backend.ID] = backend
	return nil
}

// Delete deletes a backend.
func (r *InMemoryBackendRepository) Delete(ctx context.Context, id string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.backends[id]; !exists {
		return fmt.Errorf("backend with ID %s not found", id)
	}
	delete(r.backends, id)
	return nil
}

// GetStats returns backend statistics.
func (r *InMemoryBackendRepository) GetStats(ctx context.Context, id string) (*ports.BackendStats, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	backend, exists := r.backends[id]
	if !exists {
		return nil, fmt.Errorf("backend with ID %s not found", id)
	}

	return &ports.BackendStats{
		BackendID:         backend.ID,
		IsHealthy:         backend.IsHealthy(),
		ActiveConnections: int(backend.GetActiveConnections()),
		RequestCount:      backend.GetTotalRequests(),
		SuccessCount:      backend.GetTotalRequests() - backend.GetFailureCount(),
		ErrorCount:        backend.GetFailureCount(),
		AverageLatency:    0, // TODO: Track in a separate metrics system
		LastHealthCheck:   backend.GetLastHealthCheck(),
	}, nil
}

// ========================================
// HEALTH CHECKER ADAPTER
// ========================================

// HTTPHealthChecker implements HealthChecker using HTTP health checks.
type HTTPHealthChecker struct {
	client    *http.Client
	timeout   time.Duration
	interval  time.Duration
	isRunning bool
	stopChan  chan struct{}
	backends  []*domain.Backend
	mutex     sync.RWMutex
}

// NewHTTPHealthChecker creates a new HTTP health checker.
func NewHTTPHealthChecker(timeout time.Duration) ports.HealthChecker {
	return &HTTPHealthChecker{
		client: &http.Client{
			Timeout: timeout,
		},
		timeout:  timeout,
		stopChan: make(chan struct{}),
	}
}

// CheckHealth checks the health of a backend.
func (h *HTTPHealthChecker) CheckHealth(ctx context.Context, backend *domain.Backend) (*ports.HealthCheckResult, error) {
	healthURL := backend.URL + backend.HealthCheckPath
	if backend.HealthCheckPath == "" {
		healthURL = backend.URL + "/health" // Default health check path
	}

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return &ports.HealthCheckResult{
			BackendID: backend.ID,
			IsHealthy: false,
			Error:     fmt.Sprintf("Failed to create request: %v", err),
			Timestamp: time.Now(),
		}, nil
	}

	start := time.Now()
	resp, err := h.client.Do(req)
	duration := time.Since(start)

	if err != nil {
		return &ports.HealthCheckResult{
			BackendID: backend.ID,
			IsHealthy: false,
			Error:     fmt.Sprintf("Request failed: %v", err),
			Latency:   duration,
			Timestamp: time.Now(),
		}, nil
	}
	defer resp.Body.Close()

	isHealthy := resp.StatusCode >= 200 && resp.StatusCode < 300

	return &ports.HealthCheckResult{
		BackendID:  backend.ID,
		IsHealthy:  isHealthy,
		StatusCode: resp.StatusCode,
		Latency:    duration,
		Timestamp:  time.Now(),
	}, nil
}

// StartPeriodicChecks starts periodic health checks.
func (h *HTTPHealthChecker) StartPeriodicChecks(ctx context.Context, interval time.Duration) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if h.isRunning {
		return fmt.Errorf("periodic health checks are already running")
	}

	h.interval = interval
	h.isRunning = true

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				h.mutex.Lock()
				h.isRunning = false
				h.mutex.Unlock()
				return
			case <-h.stopChan:
				h.mutex.Lock()
				h.isRunning = false
				h.mutex.Unlock()
				return
			case <-ticker.C:
				h.mutex.RLock()
				backends := h.backends
				h.mutex.RUnlock()

				for _, backend := range backends {
					go func(b *domain.Backend) {
						result, err := h.CheckHealth(ctx, b)
						if err != nil {
							log.Printf("Health check failed for backend %s: %v", b.ID, err)
							return
						}

						// Update backend status based on health check result
						if result.IsHealthy {
							b.SetStatus(domain.StatusHealthy)
						} else {
							b.SetStatus(domain.StatusUnhealthy)
						}
						b.UpdateLastHealthCheck()
					}(backend)
				}
			}
		}
	}()

	return nil
}

// StopPeriodicChecks stops all periodic health checks.
func (h *HTTPHealthChecker) StopPeriodicChecks(ctx context.Context) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if !h.isRunning {
		return nil
	}

	close(h.stopChan)
	h.stopChan = make(chan struct{})
	return nil
}

// SetHealthCheckPolicy sets the health check policy.
func (h *HTTPHealthChecker) SetHealthCheckPolicy(ctx context.Context, policy *ports.HealthCheckPolicy) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.timeout = policy.Timeout
	h.client.Timeout = policy.Timeout
	h.interval = policy.Interval

	return nil
}

// SetBackends sets the backends to be health checked.
func (h *HTTPHealthChecker) SetBackends(backends []*domain.Backend) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.backends = backends
}

// ========================================
// METRICS REPOSITORY ADAPTER
// ========================================

// InMemoryMetricsRepository implements MetricsRepository using in-memory storage.
type InMemoryMetricsRepository struct {
	metrics map[string][]*ports.Metric
	mutex   sync.RWMutex
}

// NewInMemoryMetricsRepository creates a new in-memory metrics repository.
func NewInMemoryMetricsRepository() ports.MetricsRepository {
	return &InMemoryMetricsRepository{
		metrics: make(map[string][]*ports.Metric),
	}
}

// Store stores metric data.
func (r *InMemoryMetricsRepository) Store(ctx context.Context, metrics []*ports.Metric) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	for _, metric := range metrics {
		metricType := (*metric)["name"].(string)
		r.metrics[metricType] = append(r.metrics[metricType], metric)
	}
	return nil
}

// Query retrieves metrics based on a query.
func (r *InMemoryMetricsRepository) Query(ctx context.Context, query *ports.MetricQuery) (*ports.MetricsSnapshot, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// For simplicity, return all metrics. In a real implementation,
	// this would filter based on the provided query criteria.
	snapshot := &ports.MetricsSnapshot{
		"metrics":   r.metrics,
		"timestamp": time.Now(),
		"query":     query,
	}
	return snapshot, nil
}

// Aggregate performs aggregation on metrics.
func (r *InMemoryMetricsRepository) Aggregate(ctx context.Context, aggregation *ports.MetricAggregation) (*ports.AggregationResult, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// TODO: Implement actual aggregation logic
	result := &ports.AggregationResult{
		"aggregated": true,
		"timestamp":  time.Now(),
		"type":       "aggregation",
	}
	return result, nil
}

// ========================================
// LOAD BALANCING STRATEGY FACTORY
// ========================================

// DefaultStrategyFactory implements StrategyFactory.
type DefaultStrategyFactory struct {
	strategies map[ports.StrategyType]func(*ports.StrategyConfig) ports.LoadBalancingStrategy
}

// NewDefaultStrategyFactory creates a new strategy factory.
func NewDefaultStrategyFactory() ports.StrategyFactory {
	factory := &DefaultStrategyFactory{
		strategies: make(map[ports.StrategyType]func(*ports.StrategyConfig) ports.LoadBalancingStrategy),
	}

	// Register built-in strategies
	factory.RegisterStrategy(ports.RoundRobinStrategy, func(config *ports.StrategyConfig) ports.LoadBalancingStrategy {
		return NewRoundRobinStrategy()
	})

	factory.RegisterStrategy(ports.WeightedRoundRobinStrategy, func(config *ports.StrategyConfig) ports.LoadBalancingStrategy {
		return NewWeightedRoundRobinStrategy()
	})

	factory.RegisterStrategy(ports.LeastConnectionsStrategy, func(config *ports.StrategyConfig) ports.LoadBalancingStrategy {
		return NewLeastConnectionsStrategy()
	})

	factory.RegisterStrategy(ports.IPHashStrategy, func(config *ports.StrategyConfig) ports.LoadBalancingStrategy {
		return NewIPHashStrategy()
	})

	return factory
}

// CreateStrategy creates a load balancing strategy.
func (f *DefaultStrategyFactory) CreateStrategy(strategyType ports.StrategyType, config *ports.StrategyConfig) (ports.LoadBalancingStrategy, error) {
	creator, exists := f.strategies[strategyType]
	if !exists {
		return nil, fmt.Errorf("unsupported strategy type: %s", strategyType)
	}
	return creator(config), nil
}

// RegisterStrategy registers a new strategy.
func (f *DefaultStrategyFactory) RegisterStrategy(strategyType ports.StrategyType, creator func(*ports.StrategyConfig) ports.LoadBalancingStrategy) {
	f.strategies[strategyType] = creator
}

// GetAvailableStrategies returns all available strategies.
func (f *DefaultStrategyFactory) GetAvailableStrategies() []ports.StrategyType {
	strategies := make([]ports.StrategyType, 0, len(f.strategies))
	for strategyType := range f.strategies {
		strategies = append(strategies, strategyType)
	}
	return strategies
}

// ValidateConfig validates a strategy configuration.
func (f *DefaultStrategyFactory) ValidateConfig(strategyType ports.StrategyType, config *ports.StrategyConfig) error {
	_, exists := f.strategies[strategyType]
	if !exists {
		return fmt.Errorf("unsupported strategy type: %s", strategyType)
	}

	// Basic validation - can be extended based on strategy type
	if config == nil {
		return fmt.Errorf("strategy configuration cannot be nil")
	}

	// Add strategy-specific validation here if needed
	switch strategyType {
	case ports.WeightedRoundRobinStrategy:
		// Validate weights if provided
		if weights, ok := (*config)["weights"]; ok {
			if _, ok := weights.([]int); !ok {
				return fmt.Errorf("weights must be an array of integers")
			}
		}
	case ports.IPHashStrategy:
		// Validate hash configuration if provided
		if hashKey, ok := (*config)["hash_key"]; ok {
			if _, ok := hashKey.(string); !ok {
				return fmt.Errorf("hash_key must be a string")
			}
		}
	}

	return nil
}

// ========================================
// EVENT PUBLISHER ADAPTER
// ========================================

// InMemoryEventPublisher implements EventPublisher using in-memory channels.
type InMemoryEventPublisher struct {
	subscribers map[string][]ports.EventHandler
	mutex       sync.RWMutex
}

// NewInMemoryEventPublisher creates a new in-memory event publisher.
func NewInMemoryEventPublisher() ports.EventPublisher {
	return &InMemoryEventPublisher{
		subscribers: make(map[string][]ports.EventHandler),
	}
}

// Publish publishes an event.
func (p *InMemoryEventPublisher) Publish(ctx context.Context, event *ports.Event) error {
	p.mutex.RLock()
	handlers := p.subscribers[event.Type]
	p.mutex.RUnlock()

	for _, handler := range handlers {
		go func(h ports.EventHandler) {
			if err := h.Handle(ctx, event); err != nil {
				log.Printf("Event handler error: %v", err)
			}
		}(handler)
	}

	return nil
}

// Subscribe subscribes to events of a specific type.
func (p *InMemoryEventPublisher) Subscribe(ctx context.Context, eventType string, handler ports.EventHandler) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.subscribers[eventType] = append(p.subscribers[eventType], handler)
	return nil
}

// Unsubscribe unsubscribes from events.
func (p *InMemoryEventPublisher) Unsubscribe(ctx context.Context, eventType string, handlerID string) error {
	// For simplicity, this implementation doesn't track handler IDs
	// In a real implementation, you would track handlers by ID and remove specific ones
	return nil
}

// ========================================
// CONFIGURATION REPOSITORY ADAPTER
// ========================================

// InMemoryConfigurationRepository implements ConfigurationRepository using in-memory storage.
type InMemoryConfigurationRepository struct {
	config *ports.Configuration
	mutex  sync.RWMutex
}

// NewInMemoryConfigurationRepository creates a new in-memory configuration repository.
func NewInMemoryConfigurationRepository() ports.ConfigurationRepository {
	return &InMemoryConfigurationRepository{
		config: &ports.Configuration{
			Metadata: map[string]any{
				"last_updated": time.Now(),
			},
		},
	}
}

// LoadConfiguration loads the application configuration.
func (r *InMemoryConfigurationRepository) LoadConfiguration(ctx context.Context) (*ports.Configuration, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return r.config, nil
}

// SaveConfiguration saves the application configuration.
func (r *InMemoryConfigurationRepository) SaveConfiguration(ctx context.Context, config *ports.Configuration) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.config = config
	if r.config.Metadata == nil {
		r.config.Metadata = make(map[string]any)
	}
	r.config.Metadata["last_updated"] = time.Now()
	return nil
}

// WatchConfiguration watches for configuration changes.
func (r *InMemoryConfigurationRepository) WatchConfiguration(ctx context.Context) (<-chan *ports.ConfigurationEvent, error) {
	// For simplicity, return a channel that never sends events
	// In a real implementation, this would watch for file changes, database updates, etc.
	ch := make(chan *ports.ConfigurationEvent)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

// ========================================
// CONCRETE LOAD BALANCING STRATEGIES
// ========================================

// RoundRobinStrategy implements round-robin load balancing.
type RoundRobinStrategy struct {
	current int
	mutex   sync.Mutex
}

// NewRoundRobinStrategy creates a new round-robin strategy.
func NewRoundRobinStrategy() ports.LoadBalancingStrategy {
	return &RoundRobinStrategy{}
}

// SelectBackend selects a backend using round-robin algorithm.
func (s *RoundRobinStrategy) SelectBackend(ctx context.Context, request *ports.LoadBalancingRequest, backends []*domain.Backend) (*domain.Backend, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("no backends available")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	backend := backends[s.current%len(backends)]
	s.current++
	return backend, nil
}

// Name returns the strategy name.
func (s *RoundRobinStrategy) Name() string {
	return "round_robin"
}

// Type returns the strategy type.
func (s *RoundRobinStrategy) Type() ports.StrategyType {
	return ports.RoundRobinStrategy
}

// Reset resets the strategy state.
func (s *RoundRobinStrategy) Reset() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.current = 0
	return nil
}

// GetStats returns strategy-specific statistics.
func (s *RoundRobinStrategy) GetStats() map[string]interface{} {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return map[string]interface{}{
		"current_index": s.current,
		"strategy":      "round_robin",
	}
}

// WeightedRoundRobinStrategy implements weighted round-robin load balancing.
type WeightedRoundRobinStrategy struct {
	weights []int
	current int
	mutex   sync.Mutex
}

// NewWeightedRoundRobinStrategy creates a new weighted round-robin strategy.
func NewWeightedRoundRobinStrategy() ports.LoadBalancingStrategy {
	return &WeightedRoundRobinStrategy{}
}

// SelectBackend selects a backend using weighted round-robin algorithm.
func (s *WeightedRoundRobinStrategy) SelectBackend(ctx context.Context, request *ports.LoadBalancingRequest, backends []*domain.Backend) (*domain.Backend, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("no backends available")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Build weighted list if not already built
	if len(s.weights) != len(backends) {
		s.weights = make([]int, len(backends))
		for i, backend := range backends {
			weight := backend.Weight
			if weight <= 0 {
				weight = 1 // Default weight
			}
			s.weights[i] = weight
		}
	}

	// Simple weighted selection (can be optimized)
	totalWeight := 0
	for _, weight := range s.weights {
		totalWeight += weight
	}

	target := s.current % totalWeight
	s.current++

	currentWeight := 0
	for i, weight := range s.weights {
		currentWeight += weight
		if target < currentWeight {
			return backends[i], nil
		}
	}

	// Fallback to first backend
	return backends[0], nil
}

// Name returns the strategy name.
func (s *WeightedRoundRobinStrategy) Name() string {
	return "weighted_round_robin"
}

// Type returns the strategy type.
func (s *WeightedRoundRobinStrategy) Type() ports.StrategyType {
	return ports.WeightedRoundRobinStrategy
}

// Reset resets the strategy state.
func (s *WeightedRoundRobinStrategy) Reset() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.current = 0
	s.weights = nil
	return nil
}

// GetStats returns strategy-specific statistics.
func (s *WeightedRoundRobinStrategy) GetStats() map[string]interface{} {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return map[string]interface{}{
		"current_index": s.current,
		"weights":       s.weights,
		"strategy":      "weighted_round_robin",
	}
}

// LeastConnectionsStrategy implements least connections load balancing.
type LeastConnectionsStrategy struct{}

// NewLeastConnectionsStrategy creates a new least connections strategy.
func NewLeastConnectionsStrategy() ports.LoadBalancingStrategy {
	return &LeastConnectionsStrategy{}
}

// SelectBackend selects a backend with the least connections.
func (s *LeastConnectionsStrategy) SelectBackend(ctx context.Context, request *ports.LoadBalancingRequest, backends []*domain.Backend) (*domain.Backend, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("no backends available")
	}

	var selectedBackend *domain.Backend
	minConnections := int64(^uint64(0) >> 1) // Max int64

	for _, backend := range backends {
		if backend.GetActiveConnections() < minConnections {
			minConnections = backend.GetActiveConnections()
			selectedBackend = backend
		}
	}

	return selectedBackend, nil
}

// Name returns the strategy name.
func (s *LeastConnectionsStrategy) Name() string {
	return "least_connections"
}

// Type returns the strategy type.
func (s *LeastConnectionsStrategy) Type() ports.StrategyType {
	return ports.LeastConnectionsStrategy
}

// Reset resets the strategy state.
func (s *LeastConnectionsStrategy) Reset() error {
	// No state to reset for least connections
	return nil
}

// GetStats returns strategy-specific statistics.
func (s *LeastConnectionsStrategy) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"strategy": "least_connections",
	}
}

// IPHashStrategy implements IP hash load balancing.
type IPHashStrategy struct{}

// NewIPHashStrategy creates a new IP hash strategy.
func NewIPHashStrategy() ports.LoadBalancingStrategy {
	return &IPHashStrategy{}
}

// SelectBackend selects a backend based on client IP hash.
func (s *IPHashStrategy) SelectBackend(ctx context.Context, request *ports.LoadBalancingRequest, backends []*domain.Backend) (*domain.Backend, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("no backends available")
	}

	// Simple hash function based on client IP
	hash := 0
	for _, char := range request.ClientIP {
		hash = hash*31 + int(char)
	}

	index := hash % len(backends)
	if index < 0 {
		index = -index
	}

	return backends[index], nil
}

// Name returns the strategy name.
func (s *IPHashStrategy) Name() string {
	return "ip_hash"
}

// Type returns the strategy type.
func (s *IPHashStrategy) Type() ports.StrategyType {
	return ports.IPHashStrategy
}

// Reset resets the strategy state.
func (s *IPHashStrategy) Reset() error {
	// No state to reset for IP hash
	return nil
}

// GetStats returns strategy-specific statistics.
func (s *IPHashStrategy) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"strategy": "ip_hash",
	}
}
