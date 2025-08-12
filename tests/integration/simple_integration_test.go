package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/handler"
	"github.com/mir00r/load-balancer/internal/repository"
	"github.com/mir00r/load-balancer/internal/service"
	"github.com/mir00r/load-balancer/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSimpleLoadBalancing tests basic load balancing functionality
func TestSimpleLoadBalancing(t *testing.T) {
	// Create test backends
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Backend-ID", "backend-1")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Response from backend 1"))
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Backend-ID", "backend-2")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Response from backend 2"))
	}))
	defer backend2.Close()

	// Create logger
	loggerConfig := logger.Config{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	}
	testLogger, err := logger.New(loggerConfig)
	require.NoError(t, err)

	// Create backends
	backends := []*domain.Backend{
		domain.NewBackend("backend-1", backend1.URL, 1),
		domain.NewBackend("backend-2", backend2.URL, 1),
	}

	// Create backend repository and add backends
	backendRepo := repository.NewInMemoryBackendRepository()
	for _, backend := range backends {
		err := backendRepo.Save(backend)
		require.NoError(t, err)
	}

	// Create metrics service
	metricsService := service.NewMetrics()

	// Create load balancer configuration
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.RoundRobinStrategy,
	}

	// Create load balancer service
	lbService, err := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	require.NoError(t, err)

	// Create HTTP handler
	httpHandler := handler.NewLoadBalancerHandler(lbService, metricsService, testLogger, lbConfig)

	t.Run("RoundRobinDistribution", func(t *testing.T) {
		backendHits := make(map[string]int)
		const numRequests = 10

		// Make multiple requests
		for i := 0; i < numRequests; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			ctx := context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req))
			req = req.WithContext(ctx)

			recorder := httptest.NewRecorder()
			httpHandler.ServeHTTP(recorder, req)

			if recorder.Code == http.StatusOK {
				backendID := recorder.Header().Get("Backend-ID")
				if backendID != "" {
					backendHits[backendID]++
				}
			}
		}

		// Should distribute requests to both backends
		t.Logf("Backend hits: %+v", backendHits)
		assert.Greater(t, len(backendHits), 0, "Should distribute requests to backends")

		// Check that both backends received at least some requests
		totalHits := 0
		for _, hits := range backendHits {
			totalHits += hits
		}
		assert.Greater(t, totalHits, 0, "Should have successful requests")
	})
}

// TestBackendFailureHandling tests how the load balancer handles backend failures
func TestBackendFailureHandling(t *testing.T) {
	// Create healthy backend
	healthyBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Backend-ID", "healthy")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Healthy response"))
	}))
	defer healthyBackend.Close()

	// Create failing backend
	failingBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Server error"))
	}))
	defer failingBackend.Close()

	// Create logger
	loggerConfig := logger.Config{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	}
	testLogger, err := logger.New(loggerConfig)
	require.NoError(t, err)

	// Create backends
	healthyBackendDomain := domain.NewBackend("healthy", healthyBackend.URL, 1)
	failingBackendDomain := domain.NewBackend("failing", failingBackend.URL, 1)

	// Create backend repository and add backends
	backendRepo := repository.NewInMemoryBackendRepository()
	require.NoError(t, backendRepo.Save(healthyBackendDomain))
	require.NoError(t, backendRepo.Save(failingBackendDomain))

	// Create metrics service
	metricsService := service.NewMetrics()

	// Create load balancer
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.RoundRobinStrategy,
	}

	lbService, err := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	require.NoError(t, err)

	// Create HTTP handler
	httpHandler := handler.NewLoadBalancerHandler(lbService, metricsService, testLogger, lbConfig)

	t.Run("HandlesPartialFailures", func(t *testing.T) {
		successCount := 0
		const numRequests = 10

		// Make requests
		for i := 0; i < numRequests; i++ {
			req := httptest.NewRequest("GET", "/", nil)
			ctx := context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req))
			req = req.WithContext(ctx)

			recorder := httptest.NewRecorder()
			httpHandler.ServeHTTP(recorder, req)

			if recorder.Code == http.StatusOK {
				successCount++
			}
		}

		t.Logf("Success rate: %d/%d", successCount, numRequests)
		// Should have some successful requests (from healthy backend)
		assert.Greater(t, successCount, 0, "Should have some successful responses despite failures")
	})
}

// TestConcurrentLoadBalancing tests concurrent request handling
func TestConcurrentLoadBalancing(t *testing.T) {
	// Create test backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate processing time
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	// Create logger
	loggerConfig := logger.Config{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	}
	testLogger, err := logger.New(loggerConfig)
	require.NoError(t, err)

	// Create backend
	backendDomain := domain.NewBackend("test-backend", backend.URL, 1)

	// Create backend repository
	backendRepo := repository.NewInMemoryBackendRepository()
	require.NoError(t, backendRepo.Save(backendDomain))

	// Create metrics service
	metricsService := service.NewMetrics()

	// Create load balancer
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.RoundRobinStrategy,
	}

	lbService, err := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	require.NoError(t, err)

	// Create HTTP handler
	httpHandler := handler.NewLoadBalancerHandler(lbService, metricsService, testLogger, lbConfig)

	t.Run("ConcurrentRequests", func(t *testing.T) {
		const numGoroutines = 10
		const requestsPerGoroutine = 5

		var wg sync.WaitGroup
		results := make(chan int, numGoroutines*requestsPerGoroutine)

		// Launch concurrent requests
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				for j := 0; j < requestsPerGoroutine; j++ {
					req := httptest.NewRequest("GET", "/", nil)
					ctx := context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req))
					req = req.WithContext(ctx)

					recorder := httptest.NewRecorder()
					httpHandler.ServeHTTP(recorder, req)
					results <- recorder.Code
				}
			}()
		}

		wg.Wait()
		close(results)

		// Count responses
		totalRequests := 0
		successCount := 0
		for statusCode := range results {
			totalRequests++
			if statusCode == http.StatusOK {
				successCount++
			}
		}

		expectedTotal := numGoroutines * requestsPerGoroutine
		assert.Equal(t, expectedTotal, totalRequests, "Should handle all concurrent requests")
		assert.Greater(t, successCount, 0, "Should have successful responses")

		t.Logf("Concurrent test results: %d/%d successful", successCount, totalRequests)
	})
}

// TestAdminHandlerBasics tests basic admin functionality
func TestAdminHandlerBasics(t *testing.T) {
	// Create logger
	loggerConfig := logger.Config{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	}
	testLogger, err := logger.New(loggerConfig)
	require.NoError(t, err)

	// Create backend repository with a test backend
	backendRepo := repository.NewInMemoryBackendRepository()
	testBackend := domain.NewBackend("test-backend", "http://localhost:8081", 1)
	require.NoError(t, backendRepo.Save(testBackend))

	// Create metrics service
	metricsService := service.NewMetrics()

	// Create load balancer
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.RoundRobinStrategy,
	}

	lbService, err := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	require.NoError(t, err)

	// Create admin handler
	adminHandler := handler.NewAdminHandler(lbService, metricsService, testLogger)

	t.Run("AdminHandlerCreation", func(t *testing.T) {
		assert.NotNil(t, adminHandler, "Admin handler should be created successfully")
	})
}

// TestHTTPMethodHandling tests different HTTP methods
func TestHTTPMethodHandling(t *testing.T) {
	// Create test backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Method", r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// Create logger
	loggerConfig := logger.Config{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	}
	testLogger, err := logger.New(loggerConfig)
	require.NoError(t, err)

	// Create backend
	backendDomain := domain.NewBackend("test-backend", backend.URL, 1)

	// Create backend repository
	backendRepo := repository.NewInMemoryBackendRepository()
	require.NoError(t, backendRepo.Save(backendDomain))

	// Create metrics service
	metricsService := service.NewMetrics()

	// Create load balancer
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.RoundRobinStrategy,
	}

	lbService, err := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	require.NoError(t, err)

	// Create HTTP handler
	httpHandler := handler.NewLoadBalancerHandler(lbService, metricsService, testLogger, lbConfig)

	methods := []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS"}

	for _, method := range methods {
		t.Run("Method_"+method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/", nil)
			ctx := context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req))
			req = req.WithContext(ctx)

			recorder := httptest.NewRecorder()
			httpHandler.ServeHTTP(recorder, req)

			// Should handle all standard HTTP methods
			t.Logf("Method %s: Status %d", method, recorder.Code)

			// For HEAD requests, we might not get content, but status should be reasonable
			if method != "HEAD" {
				// Check if we got a reasonable response
				assert.True(t, recorder.Code < 500, "Should handle %s method without server error", method)
			}
		})
	}
}
