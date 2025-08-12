package algorithms

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoundRobinStrategy tests the round-robin load balancing algorithm
func TestRoundRobinStrategy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		backends []*domain.Backend
		requests int
		expected []string
	}{
		{
			name: "Basic round-robin with 3 backends",
			backends: []*domain.Backend{
				domain.NewBackend("backend-1", "http://localhost:8001", 1),
				domain.NewBackend("backend-2", "http://localhost:8002", 1),
				domain.NewBackend("backend-3", "http://localhost:8003", 1),
			},
			requests: 6,
			expected: []string{"backend-1", "backend-2", "backend-3", "backend-1", "backend-2", "backend-3"},
		},
		{
			name: "Single backend",
			backends: []*domain.Backend{
				domain.NewBackend("backend-1", "http://localhost:8001", 1),
			},
			requests: 3,
			expected: []string{"backend-1", "backend-1", "backend-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var index uint64
			strategy := service.NewRoundRobinStrategy(&index)

			// Mark all backends as healthy
			for _, backend := range tt.backends {
				backend.SetStatus(domain.StatusHealthy)
			}

			// Execute requests and collect results
			results := make([]string, tt.requests)
			for i := 0; i < tt.requests; i++ {
				backend, err := strategy.SelectBackend(context.Background(), tt.backends)
				require.NoError(t, err, "Failed to select backend for request %d", i)
				require.NotNil(t, backend, "Backend should not be nil")
				results[i] = backend.ID
			}

			// Verify the round-robin order
			assert.Equal(t, tt.expected, results, "Round-robin distribution should match expected order")
		})
	}
}

// TestWeightedRoundRobinStrategy tests the weighted round-robin algorithm
func TestWeightedRoundRobinStrategy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		backends []*domain.Backend
		requests int
	}{
		{
			name: "Weighted backends (2:1:1)",
			backends: []*domain.Backend{
				domain.NewBackend("backend-1", "http://localhost:8001", 2),
				domain.NewBackend("backend-2", "http://localhost:8002", 1),
				domain.NewBackend("backend-3", "http://localhost:8003", 1),
			},
			requests: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize weights map and mutex for WeightedRoundRobinStrategy
			currentWeights := make(map[string]int)
			var mu sync.RWMutex

			for _, backend := range tt.backends {
				currentWeights[backend.ID] = 0
			}

			strategy := service.NewWeightedRoundRobinStrategy(currentWeights, &mu)

			// Mark all backends as healthy
			for _, backend := range tt.backends {
				backend.SetStatus(domain.StatusHealthy)
			}

			// Execute requests and count distribution
			distribution := make(map[string]int)
			for i := 0; i < tt.requests; i++ {
				backend, err := strategy.SelectBackend(context.Background(), tt.backends)
				require.NoError(t, err, "Failed to select backend for request %d", i)
				require.NotNil(t, backend, "Backend should not be nil")
				distribution[backend.ID]++
			}

			// Verify that distribution respects weights
			totalWeight := 0
			for _, backend := range tt.backends {
				totalWeight += backend.Weight
			}

			for _, backend := range tt.backends {
				expectedRatio := float64(backend.Weight) / float64(totalWeight)
				actualCount := distribution[backend.ID]
				expectedCount := int(float64(tt.requests) * expectedRatio)

				// Allow for some variance due to rounding
				assert.InDelta(t, expectedCount, actualCount, 1.0,
					"Backend %s should receive approximately %d requests based on weight %d, got %d",
					backend.ID, expectedCount, backend.Weight, actualCount)
			}
		})
	}
}

// TestLeastConnectionsStrategy tests the least connections algorithm
func TestLeastConnectionsStrategy(t *testing.T) {
	t.Parallel()

	backends := []*domain.Backend{
		domain.NewBackend("backend-1", "http://localhost:8001", 1),
		domain.NewBackend("backend-2", "http://localhost:8002", 1),
		domain.NewBackend("backend-3", "http://localhost:8003", 1),
	}

	// Mark all backends as healthy
	for _, backend := range backends {
		backend.SetStatus(domain.StatusHealthy)
	}

	strategy := service.NewLeastConnectionsStrategy()

	// Initially all backends have 0 connections, so should select first
	backend, err := strategy.SelectBackend(context.Background(), backends)
	require.NoError(t, err)
	assert.Equal(t, "backend-1", backend.ID, "Should select first backend when all have equal connections")

	// Add connections to backends to test least connections selection
	backends[0].IncrementConnections()
	backends[0].IncrementConnections() // backend-1 now has 2 connections
	backends[1].IncrementConnections() // backend-2 now has 1 connection
	// backend-3 still has 0 connections

	backend, err = strategy.SelectBackend(context.Background(), backends)
	require.NoError(t, err)
	assert.Equal(t, "backend-3", backend.ID, "Should select backend with least connections")
}

// TestIPHashStrategy tests the IP hash algorithm
func TestIPHashStrategy(t *testing.T) {
	t.Parallel()

	backends := []*domain.Backend{
		domain.NewBackend("backend-1", "http://localhost:8001", 1),
		domain.NewBackend("backend-2", "http://localhost:8002", 1),
		domain.NewBackend("backend-3", "http://localhost:8003", 1),
	}

	// Mark all backends as healthy
	for _, backend := range backends {
		backend.SetStatus(domain.StatusHealthy)
	}

	strategy := service.NewIPHashStrategy()

	// Test that same IP consistently maps to same backend
	testIPs := []string{"192.168.1.1", "10.0.0.1", "172.16.0.1", "127.0.0.1"}

	for _, ip := range testIPs {
		ctx := context.WithValue(context.Background(), "client_ip", ip)

		// Select backend multiple times for same IP
		var selectedBackend string
		for i := 0; i < 5; i++ {
			backend, err := strategy.SelectBackend(ctx, backends)
			require.NoError(t, err, "Failed to select backend for IP %s", ip)
			require.NotNil(t, backend, "Backend should not be nil")

			if i == 0 {
				selectedBackend = backend.ID
			} else {
				assert.Equal(t, selectedBackend, backend.ID,
					"Same IP %s should consistently map to same backend", ip)
			}
		}
	}
}

// TestStrategiesConcurrency tests all algorithms under concurrent load
func TestStrategiesConcurrency(t *testing.T) {
	t.Parallel()

	backends := []*domain.Backend{
		domain.NewBackend("backend-1", "http://localhost:8001", 1),
		domain.NewBackend("backend-2", "http://localhost:8002", 1),
		domain.NewBackend("backend-3", "http://localhost:8003", 1),
	}

	// Mark all backends as healthy
	for _, backend := range backends {
		backend.SetStatus(domain.StatusHealthy)
	}

	strategies := make(map[string]service.LoadBalancingStrategy)

	// Initialize each strategy with proper parameters
	var index uint64
	strategies["round-robin"] = service.NewRoundRobinStrategy(&index)
	strategies["least-connections"] = service.NewLeastConnectionsStrategy()
	strategies["ip-hash"] = service.NewIPHashStrategy()

	// Add weighted round-robin with proper parameters
	currentWeights := make(map[string]int)
	var mu sync.RWMutex
	for _, backend := range backends {
		currentWeights[backend.ID] = 0
	}
	strategies["weighted"] = service.NewWeightedRoundRobinStrategy(currentWeights, &mu)

	for strategyName, strategy := range strategies {
		t.Run(strategyName, func(t *testing.T) {
			const numGoroutines = 10
			const requestsPerGoroutine = 20

			var wg sync.WaitGroup
			results := make(chan string, numGoroutines*requestsPerGoroutine)
			errors := make(chan error, numGoroutines*requestsPerGoroutine)

			// Launch concurrent goroutines
			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func(goroutineID int) {
					defer wg.Done()

					for j := 0; j < requestsPerGoroutine; j++ {
						ctx := context.WithValue(context.Background(), "client_ip",
							fmt.Sprintf("192.168.1.%d", goroutineID+1))

						backend, err := strategy.SelectBackend(ctx, backends)
						if err != nil {
							errors <- err
							return
						}
						if backend == nil {
							errors <- fmt.Errorf("backend is nil")
							return
						}
						results <- backend.ID
					}
				}(i)
			}

			wg.Wait()
			close(results)
			close(errors)

			// Check for errors
			for err := range errors {
				t.Errorf("Error in concurrent execution: %v", err)
			}

			// Verify all requests were processed
			var resultCount int
			distribution := make(map[string]int)
			for result := range results {
				resultCount++
				distribution[result]++
			}

			expectedTotal := numGoroutines * requestsPerGoroutine
			assert.Equal(t, expectedTotal, resultCount,
				"Should process all %d requests", expectedTotal)

			// Verify that all backends received some requests (except for IP hash which might be skewed)
			if strategyName != "ip-hash" {
				for _, backend := range backends {
					assert.Greater(t, distribution[backend.ID], 0,
						"Backend %s should have received at least one request", backend.ID)
				}
			}
		})
	}
}

// TestStrategiesWithUnhealthyBackends tests behavior when some backends are unhealthy
func TestStrategiesWithUnhealthyBackends(t *testing.T) {
	t.Parallel()

	backends := []*domain.Backend{
		domain.NewBackend("backend-1", "http://localhost:8001", 1),
		domain.NewBackend("backend-2", "http://localhost:8002", 1),
		domain.NewBackend("backend-3", "http://localhost:8003", 1),
	}

	// Mark only some backends as healthy
	backends[0].SetStatus(domain.StatusHealthy)
	backends[1].SetStatus(domain.StatusUnhealthy)
	backends[2].SetStatus(domain.StatusHealthy)

	strategies := make(map[string]service.LoadBalancingStrategy)

	// Initialize each strategy with proper parameters
	var index uint64
	strategies["round-robin"] = service.NewRoundRobinStrategy(&index)
	strategies["least-connections"] = service.NewLeastConnectionsStrategy()
	strategies["ip-hash"] = service.NewIPHashStrategy()

	// Add weighted round-robin with proper parameters
	currentWeights := make(map[string]int)
	var mu sync.RWMutex
	for _, backend := range backends {
		currentWeights[backend.ID] = 0
	}
	strategies["weighted"] = service.NewWeightedRoundRobinStrategy(currentWeights, &mu)

	for strategyName, strategy := range strategies {
		t.Run(strategyName, func(t *testing.T) {
			// Test multiple requests to ensure unhealthy backends are avoided
			for i := 0; i < 10; i++ {
				ctx := context.WithValue(context.Background(), "client_ip", "192.168.1.1")
				backend, err := strategy.SelectBackend(ctx, backends)

				require.NoError(t, err, "Should successfully select a backend")
				require.NotNil(t, backend, "Backend should not be nil")

				// Should only select healthy backends
				assert.Contains(t, []string{"backend-1", "backend-3"}, backend.ID,
					"Should only select healthy backends, got %s", backend.ID)
			}
		})
	}
}

// BenchmarkLoadBalancingStrategies benchmarks all algorithms
func BenchmarkLoadBalancingStrategies(b *testing.B) {
	backends := make([]*domain.Backend, 10)
	for i := 0; i < 10; i++ {
		backends[i] = domain.NewBackend(fmt.Sprintf("backend-%d", i+1),
			fmt.Sprintf("http://localhost:80%02d", i+1), 1)
		backends[i].SetStatus(domain.StatusHealthy)
	}

	strategies := make(map[string]service.LoadBalancingStrategy)

	// Initialize each strategy with proper parameters
	var index uint64
	strategies["round-robin"] = service.NewRoundRobinStrategy(&index)
	strategies["least-connections"] = service.NewLeastConnectionsStrategy()
	strategies["ip-hash"] = service.NewIPHashStrategy()

	// Add weighted round-robin
	currentWeights := make(map[string]int)
	var mu sync.RWMutex
	for _, backend := range backends {
		currentWeights[backend.ID] = 0
	}
	strategies["weighted"] = service.NewWeightedRoundRobinStrategy(currentWeights, &mu)

	for strategyName, strategy := range strategies {
		b.Run(strategyName, func(b *testing.B) {
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					ctx := context.WithValue(context.Background(), "client_ip",
						fmt.Sprintf("192.168.1.%d", i%255+1))
					_, err := strategy.SelectBackend(ctx, backends)
					if err != nil {
						b.Fatal(err)
					}
					i++
				}
			})
		})
	}
}

// TestNoAvailableBackendsScenarios tests behavior when no backends are available
func TestNoAvailableBackendsScenarios(t *testing.T) {
	t.Parallel()

	// Test with empty backend list
	emptyBackends := []*domain.Backend{}

	// Test with all unhealthy backends
	unhealthyBackends := []*domain.Backend{
		domain.NewBackend("backend-1", "http://localhost:8001", 1),
		domain.NewBackend("backend-2", "http://localhost:8002", 1),
	}
	for _, backend := range unhealthyBackends {
		backend.SetStatus(domain.StatusUnhealthy)
	}

	strategies := make(map[string]service.LoadBalancingStrategy)

	// Initialize each strategy with proper parameters
	var index uint64
	strategies["round-robin"] = service.NewRoundRobinStrategy(&index)
	strategies["least-connections"] = service.NewLeastConnectionsStrategy()
	strategies["ip-hash"] = service.NewIPHashStrategy()

	// Add weighted round-robin
	currentWeights := make(map[string]int)
	var mu sync.RWMutex
	strategies["weighted"] = service.NewWeightedRoundRobinStrategy(currentWeights, &mu)

	testCases := []struct {
		name     string
		backends []*domain.Backend
	}{
		{"empty-backends", emptyBackends},
		{"all-unhealthy", unhealthyBackends},
	}

	for _, tc := range testCases {
		for strategyName, strategy := range strategies {
			t.Run(fmt.Sprintf("%s-%s", strategyName, tc.name), func(t *testing.T) {
				backend, err := strategy.SelectBackend(context.Background(), tc.backends)
				assert.Error(t, err, "Should return error when no backends available")
				assert.Nil(t, backend, "Backend should be nil when error occurs")
			})
		}
	}
}
