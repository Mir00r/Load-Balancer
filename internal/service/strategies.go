package service

import (
	"context"
	"crypto/md5"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/mir00r/load-balancer/internal/domain"
)

// BaseStrategy provides common functionality for all strategies
type BaseStrategy struct {
	name    string
	stats   *StrategyStats
	filters []domain.BackendFilter
}

// StrategyStats holds thread-safe statistics for strategies
type StrategyStats struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	LastUsed        int64 // Unix timestamp
	mu              sync.RWMutex
	customStats     map[string]interface{}
}

// NewStrategyStats creates a new thread-safe statistics collector
func NewStrategyStats() *StrategyStats {
	return &StrategyStats{
		customStats: make(map[string]interface{}),
	}
}

// IncrementTotal atomically increments total request count
func (s *StrategyStats) IncrementTotal() {
	atomic.AddInt64(&s.TotalRequests, 1)
}

// IncrementSuccess atomically increments successful request count
func (s *StrategyStats) IncrementSuccess() {
	atomic.AddInt64(&s.SuccessRequests, 1)
}

// IncrementFailed atomically increments failed request count
func (s *StrategyStats) IncrementFailed() {
	atomic.AddInt64(&s.FailedRequests, 1)
}

// GetStats returns a snapshot of current statistics
func (s *StrategyStats) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]interface{}{
		"total_requests":   atomic.LoadInt64(&s.TotalRequests),
		"success_requests": atomic.LoadInt64(&s.SuccessRequests),
		"failed_requests":  atomic.LoadInt64(&s.FailedRequests),
		"last_used":        atomic.LoadInt64(&s.LastUsed),
	}

	// Add custom stats
	for k, v := range s.customStats {
		stats[k] = v
	}

	return stats
}

// SetCustomStat safely sets a custom statistic
func (s *StrategyStats) SetCustomStat(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.customStats[key] = value
}

// ThreadSafeRoundRobinStrategy implements thread-safe round-robin load balancing
type ThreadSafeRoundRobinStrategy struct {
	BaseStrategy
	index uint64
}

// NewThreadSafeRoundRobinStrategy creates a new thread-safe round-robin strategy
func NewThreadSafeRoundRobinStrategy() domain.LoadBalancingAlgorithm {
	return &ThreadSafeRoundRobinStrategy{
		BaseStrategy: BaseStrategy{
			name:  "Round Robin",
			stats: NewStrategyStats(),
			filters: []domain.BackendFilter{
				&domain.HealthyBackendFilter{},
			},
		},
	}
}

func (s *ThreadSafeRoundRobinStrategy) SelectBackend(ctx context.Context, backends []*domain.Backend, clientIP string) (*domain.Backend, error) {
	s.stats.IncrementTotal()

	// Apply filters to get available backends
	available := s.applyFilters(backends)
	if len(available) == 0 {
		s.stats.IncrementFailed()
		return nil, fmt.Errorf("no healthy backends available")
	}

	// Atomically increment and get next index
	next := atomic.AddUint64(&s.index, 1)
	selected := available[(next-1)%uint64(len(available))]

	s.stats.IncrementSuccess()
	return selected, nil
}

func (s *ThreadSafeRoundRobinStrategy) Name() string {
	return s.name
}

func (s *ThreadSafeRoundRobinStrategy) Type() domain.StrategyType {
	return domain.RoundRobinStrategyType
}

func (s *ThreadSafeRoundRobinStrategy) Reset() {
	atomic.StoreUint64(&s.index, 0)
}

func (s *ThreadSafeRoundRobinStrategy) GetStats() map[string]interface{} {
	stats := s.stats.GetStats()
	stats["current_index"] = atomic.LoadUint64(&s.index)
	return stats
}

func (s *ThreadSafeRoundRobinStrategy) applyFilters(backends []*domain.Backend) []*domain.Backend {
	result := backends
	for _, filter := range s.filters {
		result = filter.Filter(result)
	}
	return result
}

// ThreadSafeWeightedRoundRobinStrategy implements thread-safe weighted round-robin
type ThreadSafeWeightedRoundRobinStrategy struct {
	BaseStrategy
	currentWeights map[string]*int64 // Use atomic int64 pointers for thread safety
	mu             sync.RWMutex      // Protects the map structure
}

// NewThreadSafeWeightedRoundRobinStrategy creates a new thread-safe weighted round-robin strategy
func NewThreadSafeWeightedRoundRobinStrategy() domain.LoadBalancingAlgorithm {
	return &ThreadSafeWeightedRoundRobinStrategy{
		BaseStrategy: BaseStrategy{
			name:  "Weighted Round Robin",
			stats: NewStrategyStats(),
			filters: []domain.BackendFilter{
				&domain.HealthyBackendFilter{},
			},
		},
		currentWeights: make(map[string]*int64),
	}
}

func (s *ThreadSafeWeightedRoundRobinStrategy) SelectBackend(ctx context.Context, backends []*domain.Backend, clientIP string) (*domain.Backend, error) {
	s.stats.IncrementTotal()

	// Apply filters to get available backends
	available := s.applyFilters(backends)
	if len(available) == 0 {
		s.stats.IncrementFailed()
		return nil, fmt.Errorf("no healthy backends available")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Initialize weights for new backends
	totalWeight := 0
	for _, backend := range available {
		if _, exists := s.currentWeights[backend.ID]; !exists {
			s.currentWeights[backend.ID] = new(int64)
		}
		totalWeight += backend.Weight
	}

	if totalWeight == 0 {
		// Fall back to round-robin if all weights are zero
		s.stats.IncrementSuccess()
		return available[len(available)%2], nil // Simple fallback
	}

	// Find backend with highest current weight
	var selected *domain.Backend
	maxWeight := int64(-1)

	for _, backend := range available {
		currentWeight := atomic.AddInt64(s.currentWeights[backend.ID], int64(backend.Weight))
		if currentWeight > maxWeight {
			maxWeight = currentWeight
			selected = backend
		}
	}

	// Reduce selected backend's weight by total weight
	if selected != nil {
		atomic.AddInt64(s.currentWeights[selected.ID], int64(-totalWeight))
		s.stats.IncrementSuccess()
		return selected, nil
	}

	s.stats.IncrementFailed()
	return nil, fmt.Errorf("failed to select backend")
}

func (s *ThreadSafeWeightedRoundRobinStrategy) Name() string {
	return s.name
}

func (s *ThreadSafeWeightedRoundRobinStrategy) Type() domain.StrategyType {
	return domain.WeightedRoundRobinStrategyType
}

func (s *ThreadSafeWeightedRoundRobinStrategy) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, weight := range s.currentWeights {
		atomic.StoreInt64(weight, 0)
	}
}

func (s *ThreadSafeWeightedRoundRobinStrategy) GetStats() map[string]interface{} {
	stats := s.stats.GetStats()

	s.mu.RLock()
	weights := make(map[string]int64)
	for id, weight := range s.currentWeights {
		weights[id] = atomic.LoadInt64(weight)
	}
	s.mu.RUnlock()

	stats["current_weights"] = weights
	return stats
}

func (s *ThreadSafeWeightedRoundRobinStrategy) applyFilters(backends []*domain.Backend) []*domain.Backend {
	result := backends
	for _, filter := range s.filters {
		result = filter.Filter(result)
	}
	return result
}

// ThreadSafeLeastConnectionsStrategy implements thread-safe least connections balancing
type ThreadSafeLeastConnectionsStrategy struct {
	BaseStrategy
}

// NewThreadSafeLeastConnectionsStrategy creates a new thread-safe least connections strategy
func NewThreadSafeLeastConnectionsStrategy() domain.LoadBalancingAlgorithm {
	return &ThreadSafeLeastConnectionsStrategy{
		BaseStrategy: BaseStrategy{
			name:  "Least Connections",
			stats: NewStrategyStats(),
			filters: []domain.BackendFilter{
				&domain.HealthyBackendFilter{},
			},
		},
	}
}

func (s *ThreadSafeLeastConnectionsStrategy) SelectBackend(ctx context.Context, backends []*domain.Backend, clientIP string) (*domain.Backend, error) {
	s.stats.IncrementTotal()

	// Apply filters to get available backends
	available := s.applyFilters(backends)
	if len(available) == 0 {
		s.stats.IncrementFailed()
		return nil, fmt.Errorf("no healthy backends available")
	}

	// Sort by active connections (ascending), then by weight (descending)
	sort.Slice(available, func(i, j int) bool {
		connectionsI := available[i].GetActiveConnections()
		connectionsJ := available[j].GetActiveConnections()

		if connectionsI == connectionsJ {
			// If connections are equal, prefer higher weight
			return available[i].Weight > available[j].Weight
		}
		return connectionsI < connectionsJ
	})

	s.stats.IncrementSuccess()
	return available[0], nil
}

func (s *ThreadSafeLeastConnectionsStrategy) Name() string {
	return s.name
}

func (s *ThreadSafeLeastConnectionsStrategy) Type() domain.StrategyType {
	return domain.LeastConnectionsStrategyType
}

func (s *ThreadSafeLeastConnectionsStrategy) Reset() {
	// No state to reset for least connections
}

func (s *ThreadSafeLeastConnectionsStrategy) GetStats() map[string]interface{} {
	return s.stats.GetStats()
}

func (s *ThreadSafeLeastConnectionsStrategy) applyFilters(backends []*domain.Backend) []*domain.Backend {
	result := backends
	for _, filter := range s.filters {
		result = filter.Filter(result)
	}
	return result
}

// ThreadSafeIPHashStrategy implements thread-safe IP hash-based load balancing
type ThreadSafeIPHashStrategy struct {
	BaseStrategy
	hashFunc func(string) uint32
}

// NewThreadSafeIPHashStrategy creates a new thread-safe IP hash strategy
func NewThreadSafeIPHashStrategy() domain.LoadBalancingAlgorithm {
	return &ThreadSafeIPHashStrategy{
		BaseStrategy: BaseStrategy{
			name:  "IP Hash",
			stats: NewStrategyStats(),
			filters: []domain.BackendFilter{
				&domain.HealthyBackendFilter{},
			},
		},
		hashFunc: fnvHash,
	}
}

func (s *ThreadSafeIPHashStrategy) SelectBackend(ctx context.Context, backends []*domain.Backend, clientIP string) (*domain.Backend, error) {
	s.stats.IncrementTotal()

	// Apply filters to get available backends
	available := s.applyFilters(backends)
	if len(available) == 0 {
		s.stats.IncrementFailed()
		return nil, fmt.Errorf("no healthy backends available")
	}

	if clientIP == "" {
		// Fall back to round-robin if no client IP
		s.stats.IncrementSuccess()
		return available[0], nil
	}

	// Calculate hash and select backend
	hash := s.hashFunc(clientIP)
	index := hash % uint32(len(available))

	s.stats.IncrementSuccess()
	return available[index], nil
}

func (s *ThreadSafeIPHashStrategy) Name() string {
	return s.name
}

func (s *ThreadSafeIPHashStrategy) Type() domain.StrategyType {
	return domain.IPHashStrategyType
}

func (s *ThreadSafeIPHashStrategy) Reset() {
	// No state to reset for IP hash
}

func (s *ThreadSafeIPHashStrategy) GetStats() map[string]interface{} {
	return s.stats.GetStats()
}

func (s *ThreadSafeIPHashStrategy) applyFilters(backends []*domain.Backend) []*domain.Backend {
	result := backends
	for _, filter := range s.filters {
		result = filter.Filter(result)
	}
	return result
}

// Hash functions

// fnvHash implements FNV-1a hash algorithm
func fnvHash(input string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(input))
	return h.Sum32()
}

// md5Hash implements MD5 hash algorithm (for compatibility)
func md5Hash(input string) uint32 {
	hasher := md5.New()
	hasher.Write([]byte(input))
	hash := hasher.Sum(nil)
	return uint32(hash[0])<<24 | uint32(hash[1])<<16 | uint32(hash[2])<<8 | uint32(hash[3])
}

// DefaultStrategyFactory implements the AlgorithmFactory interface
type DefaultStrategyFactory struct{}

// NewDefaultStrategyFactory creates a new strategy factory
func NewDefaultStrategyFactory() domain.AlgorithmFactory {
	return &DefaultStrategyFactory{}
}

func (f *DefaultStrategyFactory) CreateAlgorithm(strategyType domain.StrategyType, config domain.AlgorithmConfig) (domain.LoadBalancingAlgorithm, error) {
	switch strategyType {
	case domain.RoundRobinStrategyType:
		return NewThreadSafeRoundRobinStrategy(), nil
	case domain.WeightedRoundRobinStrategyType:
		return NewThreadSafeWeightedRoundRobinStrategy(), nil
	case domain.LeastConnectionsStrategyType:
		return NewThreadSafeLeastConnectionsStrategy(), nil
	case domain.IPHashStrategyType:
		return NewThreadSafeIPHashStrategy(), nil
	default:
		return nil, fmt.Errorf("unsupported strategy type: %s", strategyType)
	}
}

func (f *DefaultStrategyFactory) GetAvailableAlgorithms() []domain.StrategyType {
	return []domain.StrategyType{
		domain.RoundRobinStrategyType,
		domain.WeightedRoundRobinStrategyType,
		domain.LeastConnectionsStrategyType,
		domain.IPHashStrategyType,
	}
}
