package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/repository"
	"github.com/mir00r/load-balancer/pkg/logger"
)

func TestRoundRobinStrategy(t *testing.T) {
	// Setup
	backends := []*domain.Backend{
		domain.NewBackend("backend-1", "http://localhost:8081", 1),
		domain.NewBackend("backend-2", "http://localhost:8082", 1),
		domain.NewBackend("backend-3", "http://localhost:8083", 1),
	}

	var index uint64
	strategy := NewRoundRobinStrategy(&index)

	// Test round-robin selection
	selectedBackends := make([]string, 6)
	for i := 0; i < 6; i++ {
		backend, err := strategy.SelectBackend(context.Background(), backends)
		if err != nil {
			t.Fatalf("Failed to select backend: %v", err)
		}
		selectedBackends[i] = backend.ID
	}

	// Verify round-robin pattern
	expected := []string{
		"backend-1", "backend-2", "backend-3",
		"backend-1", "backend-2", "backend-3",
	}

	for i, expected := range expected {
		if selectedBackends[i] != expected {
			t.Errorf("Expected backend %s at position %d, got %s", expected, i, selectedBackends[i])
		}
	}
}

func TestWeightedRoundRobinStrategy(t *testing.T) {
	// Setup
	backends := []*domain.Backend{
		domain.NewBackend("backend-1", "http://localhost:8081", 1),
		domain.NewBackend("backend-2", "http://localhost:8082", 2),
		domain.NewBackend("backend-3", "http://localhost:8083", 1),
	}

	currentWeights := make(map[string]int)
	var mu sync.RWMutex
	strategy := NewWeightedRoundRobinStrategy(currentWeights, &mu)

	// Test weighted selection
	selections := make(map[string]int)
	for i := 0; i < 12; i++ {
		backend, err := strategy.SelectBackend(context.Background(), backends)
		if err != nil {
			t.Fatalf("Failed to select backend: %v", err)
		}
		selections[backend.ID]++
	}

	// Verify weighted distribution (backend-2 should have twice as many selections)
	if selections["backend-2"] != 6 {
		t.Errorf("Expected backend-2 to be selected 6 times, got %d", selections["backend-2"])
	}
	if selections["backend-1"] != 3 {
		t.Errorf("Expected backend-1 to be selected 3 times, got %d", selections["backend-1"])
	}
	if selections["backend-3"] != 3 {
		t.Errorf("Expected backend-3 to be selected 3 times, got %d", selections["backend-3"])
	}
}

func TestLeastConnectionsStrategy(t *testing.T) {
	// Setup
	backends := []*domain.Backend{
		domain.NewBackend("backend-1", "http://localhost:8081", 1),
		domain.NewBackend("backend-2", "http://localhost:8082", 1),
		domain.NewBackend("backend-3", "http://localhost:8083", 1),
	}

	// Simulate different connection counts
	backends[0].IncrementConnections()
	backends[0].IncrementConnections()
	backends[1].IncrementConnections()
	// backend-3 has 0 connections

	strategy := NewLeastConnectionsStrategy()

	// Test least connections selection
	backend, err := strategy.SelectBackend(context.Background(), backends)
	if err != nil {
		t.Fatalf("Failed to select backend: %v", err)
	}

	// Should select backend-3 (0 connections)
	if backend.ID != "backend-3" {
		t.Errorf("Expected backend-3 to be selected, got %s", backend.ID)
	}
}

func TestLoadBalancer(t *testing.T) {
	// Setup
	log, _ := logger.New(logger.Config{
		Level:  "error",
		Format: "json",
		Output: "stderr",
	})

	repo := repository.NewInMemoryBackendRepository()
	healthChecker := NewHealthChecker(domain.HealthCheckConfig{
		Enabled: false, // Disable for testing
	}, log)
	metrics := NewMetrics()

	config := domain.LoadBalancerConfig{
		Strategy:   domain.RoundRobinStrategy,
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
		Timeout:    30 * time.Second,
	}

	lb, err := NewLoadBalancer(config, repo, healthChecker, metrics, log)
	if err != nil {
		t.Fatalf("Failed to create load balancer: %v", err)
	}

	// Add backends
	backends := []*domain.Backend{
		domain.NewBackend("backend-1", "http://localhost:8081", 1),
		domain.NewBackend("backend-2", "http://localhost:8082", 1),
	}

	for _, backend := range backends {
		if err := lb.AddBackend(backend); err != nil {
			t.Fatalf("Failed to add backend: %v", err)
		}
	}

	// Test backend selection
	ctx := context.Background()
	backend1, err := lb.GetBackend(ctx)
	if err != nil {
		t.Fatalf("Failed to get backend: %v", err)
	}

	backend2, err := lb.GetBackend(ctx)
	if err != nil {
		t.Fatalf("Failed to get backend: %v", err)
	}

	// Should alternate between backends
	if backend1.ID == backend2.ID {
		t.Error("Expected different backends to be selected")
	}
}

func TestMetrics(t *testing.T) {
	metrics := NewMetrics()

	// Test request counting
	metrics.IncrementRequests("backend-1")
	metrics.IncrementRequests("backend-1")
	metrics.IncrementRequests("backend-2")

	// Test error counting
	metrics.IncrementErrors("backend-1")

	// Test latency recording
	metrics.RecordLatency("backend-1", 50*time.Millisecond)
	metrics.RecordLatency("backend-1", 100*time.Millisecond)

	// Verify stats
	stats := metrics.GetStats()

	totalRequests, ok := stats["total_requests"].(int64)
	if !ok || totalRequests != 3 {
		t.Errorf("Expected 3 total requests, got %v", totalRequests)
	}

	totalErrors, ok := stats["total_errors"].(int64)
	if !ok || totalErrors != 1 {
		t.Errorf("Expected 1 total error, got %v", totalErrors)
	}
}
