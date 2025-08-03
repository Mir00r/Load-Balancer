package service

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/repository"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// LoadBalancer implements the domain.LoadBalancer interface
type LoadBalancer struct {
	config        domain.LoadBalancerConfig
	backendRepo   *repository.InMemoryBackendRepository
	healthChecker *HealthChecker
	metrics       domain.Metrics
	logger        *logger.Logger
	strategy      LoadBalancingStrategy

	// Round robin state
	roundRobinIndex uint64

	// Weighted round robin state
	weightedCurrentWeights map[string]int
	weightedMu             sync.RWMutex

	mu sync.RWMutex
}

// LoadBalancingStrategy defines the interface for load balancing strategies
type LoadBalancingStrategy interface {
	SelectBackend(ctx context.Context, backends []*domain.Backend) (*domain.Backend, error)
	Name() string
}

// RoundRobinStrategy implements round-robin load balancing
type RoundRobinStrategy struct {
	index *uint64
}

// NewRoundRobinStrategy creates a new round-robin strategy
func NewRoundRobinStrategy(index *uint64) *RoundRobinStrategy {
	return &RoundRobinStrategy{index: index}
}

// SelectBackend selects the next backend using round-robin
func (s *RoundRobinStrategy) SelectBackend(ctx context.Context, backends []*domain.Backend) (*domain.Backend, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("no backends available")
	}

	// Get next index atomically
	next := atomic.AddUint64(s.index, 1)
	return backends[(next-1)%uint64(len(backends))], nil
}

// Name returns the strategy name
func (s *RoundRobinStrategy) Name() string {
	return "round_robin"
}

// WeightedRoundRobinStrategy implements weighted round-robin load balancing
type WeightedRoundRobinStrategy struct {
	currentWeights map[string]int
	mu             *sync.RWMutex
}

// NewWeightedRoundRobinStrategy creates a new weighted round-robin strategy
func NewWeightedRoundRobinStrategy(currentWeights map[string]int, mu *sync.RWMutex) *WeightedRoundRobinStrategy {
	return &WeightedRoundRobinStrategy{
		currentWeights: currentWeights,
		mu:             mu,
	}
}

// SelectBackend selects the next backend using weighted round-robin
func (s *WeightedRoundRobinStrategy) SelectBackend(ctx context.Context, backends []*domain.Backend) (*domain.Backend, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("no backends available")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Initialize current weights if needed
	totalWeight := 0
	for _, backend := range backends {
		if _, exists := s.currentWeights[backend.ID]; !exists {
			s.currentWeights[backend.ID] = 0
		}
		totalWeight += backend.Weight
	}

	if totalWeight == 0 {
		// If all weights are 0, fall back to round-robin
		return backends[rand.Intn(len(backends))], nil
	}

	// Find backend with highest current weight
	var selected *domain.Backend
	maxWeight := -1

	for _, backend := range backends {
		s.currentWeights[backend.ID] += backend.Weight
		if s.currentWeights[backend.ID] > maxWeight {
			maxWeight = s.currentWeights[backend.ID]
			selected = backend
		}
	}

	if selected != nil {
		s.currentWeights[selected.ID] -= totalWeight
	}

	return selected, nil
}

// Name returns the strategy name
func (s *WeightedRoundRobinStrategy) Name() string {
	return "weighted_round_robin"
}

// LeastConnectionsStrategy implements least connections load balancing
type LeastConnectionsStrategy struct{}

// NewLeastConnectionsStrategy creates a new least connections strategy
func NewLeastConnectionsStrategy() *LeastConnectionsStrategy {
	return &LeastConnectionsStrategy{}
}

// SelectBackend selects the backend with the least active connections
func (s *LeastConnectionsStrategy) SelectBackend(ctx context.Context, backends []*domain.Backend) (*domain.Backend, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("no backends available")
	}

	// Sort backends by active connections (ascending)
	sort.Slice(backends, func(i, j int) bool {
		connectionsI := backends[i].GetActiveConnections()
		connectionsJ := backends[j].GetActiveConnections()

		if connectionsI == connectionsJ {
			// If connections are equal, consider weights
			return backends[i].Weight > backends[j].Weight
		}
		return connectionsI < connectionsJ
	})

	return backends[0], nil
}

// Name returns the strategy name
func (s *LeastConnectionsStrategy) Name() string {
	return "least_connections"
}

// IPHashStrategy implements IP hash-based load balancing for session persistence
type IPHashStrategy struct{}

// NewIPHashStrategy creates a new IP hash strategy
func NewIPHashStrategy() *IPHashStrategy {
	return &IPHashStrategy{}
}

// SelectBackend selects a backend based on client IP hash for session persistence
func (s *IPHashStrategy) SelectBackend(ctx context.Context, backends []*domain.Backend) (*domain.Backend, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("no backends available")
	}

	// Get client IP from context
	clientIP := s.getClientIP(ctx)
	if clientIP == "" {
		// Fallback to round-robin if no IP available
		return backends[0], nil
	}

	// Use a simple hash function to determine backend
	hash := s.hashString(clientIP)
	index := hash % uint32(len(backends))

	return backends[index], nil
}

// Name returns the strategy name
func (s *IPHashStrategy) Name() string {
	return "ip_hash"
}

// getClientIP extracts client IP from context
func (s *IPHashStrategy) getClientIP(ctx context.Context) string {
	if reqCtx, ok := ctx.Value("requestContext").(*domain.RequestContext); ok {
		return reqCtx.RemoteAddr
	}
	return ""
}

// hashString implements a simple hash function for IP addresses
func (s *IPHashStrategy) hashString(s2 string) uint32 {
	h := uint32(2166136261)
	for _, c := range s2 {
		h = (h ^ uint32(c)) * 16777619
	}
	return h
}

// NewLoadBalancer creates a new load balancer instance
func NewLoadBalancer(
	config domain.LoadBalancerConfig,
	backendRepo *repository.InMemoryBackendRepository,
	healthChecker *HealthChecker,
	metrics domain.Metrics,
	logger *logger.Logger,
) (*LoadBalancer, error) {

	lb := &LoadBalancer{
		config:                 config,
		backendRepo:            backendRepo,
		healthChecker:          healthChecker,
		metrics:                metrics,
		logger:                 logger.LoadBalancerLogger(),
		weightedCurrentWeights: make(map[string]int),
	}

	// Initialize strategy based on configuration
	if err := lb.setStrategy(config.Strategy); err != nil {
		return nil, fmt.Errorf("failed to set load balancing strategy: %w", err)
	}

	return lb, nil
}

// setStrategy sets the load balancing strategy
func (lb *LoadBalancer) setStrategy(strategy domain.LoadBalancingStrategy) error {
	switch strategy {
	case domain.RoundRobinStrategy:
		lb.strategy = NewRoundRobinStrategy(&lb.roundRobinIndex)
	case domain.WeightedRoundRobinStrategy:
		lb.strategy = NewWeightedRoundRobinStrategy(lb.weightedCurrentWeights, &lb.weightedMu)
	case domain.LeastConnectionsStrategy:
		lb.strategy = NewLeastConnectionsStrategy()
	case domain.IPHashStrategy:
		lb.strategy = NewIPHashStrategy()
	default:
		return fmt.Errorf("unsupported load balancing strategy: %s", strategy)
	}

	lb.logger.Infof("Load balancing strategy set to: %s", lb.strategy.Name())
	return nil
}

// GetBackend selects the next available backend based on the configured strategy
func (lb *LoadBalancer) GetBackend(ctx context.Context) (*domain.Backend, error) {
	// Get available backends
	backends, err := lb.backendRepo.GetAvailable()
	if err != nil {
		return nil, fmt.Errorf("failed to get available backends: %w", err)
	}

	if len(backends) == 0 {
		return nil, fmt.Errorf("no healthy backends available")
	}

	// Use strategy to select backend
	backend, err := lb.strategy.SelectBackend(ctx, backends)
	if err != nil {
		return nil, fmt.Errorf("failed to select backend: %w", err)
	}

	lb.logger.WithField("backend_id", backend.ID).
		WithField("strategy", lb.strategy.Name()).
		Debug("Selected backend for request")

	return backend, nil
}

// SelectBackend selects a backend without context (for Layer 4 and other non-HTTP use cases)
func (lb *LoadBalancer) SelectBackend() *domain.Backend {
	ctx := context.Background()
	backend, err := lb.GetBackend(ctx)
	if err != nil {
		lb.logger.WithError(err).Error("Failed to select backend")
		return nil
	}
	return backend
}

// AddBackend adds a new backend to the pool
func (lb *LoadBalancer) AddBackend(backend *domain.Backend) error {
	if backend == nil {
		return fmt.Errorf("backend cannot be nil")
	}

	if err := lb.backendRepo.Save(backend); err != nil {
		return fmt.Errorf("failed to save backend: %w", err)
	}

	// Add to health checking if it's running
	if lb.healthChecker.IsRunning() {
		lb.healthChecker.AddBackend(context.Background(), backend)
	}

	lb.logger.WithField("backend_id", backend.ID).
		WithField("backend_url", backend.URL).
		Info("Added new backend")

	return nil
}

// RemoveBackend removes a backend from the pool
func (lb *LoadBalancer) RemoveBackend(id string) error {
	if err := lb.backendRepo.Delete(id); err != nil {
		return fmt.Errorf("failed to delete backend: %w", err)
	}

	// Clean up weighted round robin state
	lb.weightedMu.Lock()
	delete(lb.weightedCurrentWeights, id)
	lb.weightedMu.Unlock()

	lb.logger.WithField("backend_id", id).Info("Removed backend")
	return nil
}

// GetBackends returns all backends
func (lb *LoadBalancer) GetBackends() []*domain.Backend {
	backends, err := lb.backendRepo.GetAll()
	if err != nil {
		lb.logger.WithError(err).Error("Failed to get all backends")
		return []*domain.Backend{}
	}
	return backends
}

// GetHealthyBackends returns only healthy backends
func (lb *LoadBalancer) GetHealthyBackends() []*domain.Backend {
	backends, err := lb.backendRepo.GetHealthy()
	if err != nil {
		lb.logger.WithError(err).Error("Failed to get healthy backends")
		return []*domain.Backend{}
	}
	return backends
}

// Start starts the load balancer and health checking
func (lb *LoadBalancer) Start(ctx context.Context) error {
	lb.logger.Info("Starting load balancer")

	// Get all backends
	backends, err := lb.backendRepo.GetAll()
	if err != nil {
		return fmt.Errorf("failed to get backends: %w", err)
	}

	if len(backends) == 0 {
		return fmt.Errorf("no backends configured")
	}

	// Start health checking
	if err := lb.healthChecker.StartChecking(ctx, backends); err != nil {
		return fmt.Errorf("failed to start health checking: %w", err)
	}

	lb.logger.Infof("Load balancer started with %d backends", len(backends))
	return nil
}

// Stop gracefully stops the load balancer
func (lb *LoadBalancer) Stop(ctx context.Context) error {
	lb.logger.Info("Stopping load balancer")

	// Stop health checking
	if err := lb.healthChecker.StopChecking(); err != nil {
		lb.logger.WithError(err).Error("Failed to stop health checker")
	}

	lb.logger.Info("Load balancer stopped")
	return nil
}

// GetStats returns load balancer statistics
func (lb *LoadBalancer) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"strategy":       lb.strategy.Name(),
		"max_retries":    lb.config.MaxRetries,
		"timeout":        lb.config.Timeout.String(),
		"backend_stats":  lb.backendRepo.GetStats(),
		"health_checker": lb.healthChecker.GetStats(),
	}

	if lb.metrics != nil {
		stats["metrics"] = lb.metrics.GetStats()
	}

	return stats
}

// SetStrategy changes the load balancing strategy
func (lb *LoadBalancer) SetStrategy(strategy domain.LoadBalancingStrategy) error {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if err := lb.setStrategy(strategy); err != nil {
		return err
	}

	lb.config.Strategy = strategy
	return nil
}

// GetConfig returns the current configuration
func (lb *LoadBalancer) GetConfig() domain.LoadBalancerConfig {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return lb.config
}
