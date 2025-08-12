package domain

import (
	"context"
	"fmt"
)

// StrategyType represents the type of load balancing strategy
type StrategyType string

const (
	RoundRobinStrategyType         StrategyType = "round_robin"
	WeightedRoundRobinStrategyType StrategyType = "weighted_round_robin"
	LeastConnectionsStrategyType   StrategyType = "least_connections"
	IPHashStrategyType             StrategyType = "ip_hash"
)

// LoadBalancingAlgorithm defines the interface for load balancing algorithms
// This interface ensures thread-safe backend selection with proper error handling
type LoadBalancingAlgorithm interface {
	// SelectBackend selects the next backend based on the strategy algorithm
	// Returns error if no suitable backend is available
	SelectBackend(ctx context.Context, backends []*Backend, clientIP string) (*Backend, error)

	// Name returns the human-readable name of the strategy
	Name() string

	// Type returns the strategy type for configuration matching
	Type() StrategyType

	// Reset resets the internal state of the strategy
	Reset()

	// GetStats returns strategy-specific statistics
	GetStats() map[string]interface{}
}

// AlgorithmFactory creates load balancing strategies
type AlgorithmFactory interface {
	CreateAlgorithm(strategyType StrategyType, config AlgorithmConfig) (LoadBalancingAlgorithm, error)
	GetAvailableAlgorithms() []StrategyType
}

// AlgorithmConfig holds configuration for strategy creation
type AlgorithmConfig struct {
	// Common configuration
	Name    string                 `json:"name" yaml:"name"`
	Type    StrategyType           `json:"type" yaml:"type"`
	Options map[string]interface{} `json:"options" yaml:"options"`

	// Strategy-specific configuration
	Weights     map[string]int `json:"weights,omitempty" yaml:"weights,omitempty"`
	HashKey     string         `json:"hash_key,omitempty" yaml:"hash_key,omitempty"`
	Consistency bool           `json:"consistency,omitempty" yaml:"consistency,omitempty"`
}

// Validate validates the strategy configuration
func (ac *AlgorithmConfig) Validate() error {
	if ac.Name == "" {
		return fmt.Errorf("strategy name cannot be empty")
	}

	if ac.Type == "" {
		return fmt.Errorf("strategy type cannot be empty")
	}

	// Validate type-specific configuration
	switch ac.Type {
	case WeightedRoundRobinStrategyType:
		if len(ac.Weights) == 0 {
			return fmt.Errorf("weighted round robin strategy requires weights configuration")
		}
	case IPHashStrategyType:
		if ac.HashKey == "" {
			ac.HashKey = "client_ip" // default hash key
		}
	}

	return nil
}

// DefaultAlgorithmFactory is a concrete implementation of AlgorithmFactory
type DefaultAlgorithmFactory struct{}

// NewAlgorithmFactory creates a new default algorithm factory
func NewAlgorithmFactory() AlgorithmFactory {
	return &DefaultAlgorithmFactory{}
}

// CreateAlgorithm creates a load balancing strategy based on the provided configuration
func (f *DefaultAlgorithmFactory) CreateAlgorithm(strategyType StrategyType, config AlgorithmConfig) (LoadBalancingAlgorithm, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid strategy configuration: %w", err)
	}

	// Note: This would create instances from the new thread-safe implementations
	// For now, return an error to indicate the strategy needs to be implemented
	return nil, fmt.Errorf("strategy creation not yet implemented for type: %s", strategyType)
}

// GetAvailableAlgorithms returns all supported strategy types
func (f *DefaultAlgorithmFactory) GetAvailableAlgorithms() []StrategyType {
	return []StrategyType{
		RoundRobinStrategyType,
		WeightedRoundRobinStrategyType,
		LeastConnectionsStrategyType,
		IPHashStrategyType,
	}
}

// BackendFilter defines the interface for filtering backends
type BackendFilter interface {
	Filter(backends []*Backend) []*Backend
	Name() string
}

// HealthyBackendFilter filters only healthy backends
type HealthyBackendFilter struct{}

func (f *HealthyBackendFilter) Filter(backends []*Backend) []*Backend {
	var healthy []*Backend
	for _, backend := range backends {
		if backend.IsHealthy() && backend.IsAvailable() {
			healthy = append(healthy, backend)
		}
	}
	return healthy
}

func (f *HealthyBackendFilter) Name() string {
	return "healthy_backends"
}

// WeightedBackendFilter filters backends based on weight constraints
type WeightedBackendFilter struct {
	MinWeight int
	MaxWeight int
}

func (f *WeightedBackendFilter) Filter(backends []*Backend) []*Backend {
	var filtered []*Backend
	for _, backend := range backends {
		if backend.Weight >= f.MinWeight && (f.MaxWeight == 0 || backend.Weight <= f.MaxWeight) {
			filtered = append(filtered, backend)
		}
	}
	return filtered
}

func (f *WeightedBackendFilter) Name() string {
	return "weighted_backends"
}

// CompositeBackendFilter applies multiple filters in sequence
type CompositeBackendFilter struct {
	filters []BackendFilter
}

func NewCompositeBackendFilter(filters ...BackendFilter) *CompositeBackendFilter {
	return &CompositeBackendFilter{filters: filters}
}

func (f *CompositeBackendFilter) Filter(backends []*Backend) []*Backend {
	result := backends
	for _, filter := range f.filters {
		result = filter.Filter(result)
		if len(result) == 0 {
			break // No need to continue if no backends remain
		}
	}
	return result
}

func (f *CompositeBackendFilter) Name() string {
	return "composite_filter"
}

func (f *CompositeBackendFilter) AddFilter(filter BackendFilter) {
	f.filters = append(f.filters, filter)
}
