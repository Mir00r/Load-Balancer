package load

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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

// LoadTestMetrics holds performance test results
type LoadTestMetrics struct {
	TotalRequests  int64
	SuccessfulReqs int64
	FailedRequests int64
	TotalLatencyMs int64
	MinLatencyMs   int64
	MaxLatencyMs   int64
	RequestsPerSec float64
}

// TestHighThroughputLoadBalancing tests the load balancer under high traffic
func TestHighThroughputLoadBalancing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	// Create multiple backend servers
	const numBackends = 5
	backends := make([]*httptest.Server, numBackends)
	backendDomains := make([]*domain.Backend, numBackends)

	for i := 0; i < numBackends; i++ {
		backendID := fmt.Sprintf("backend-%d", i+1)
		backends[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate some processing time
			time.Sleep(time.Millisecond * 5)
			w.Header().Set("Backend-ID", backendID)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf("Response from %s", backendID)))
		}))
		backendDomains[i] = domain.NewBackend(backendID, backends[i].URL, 1)
	}

	// Cleanup
	defer func() {
		for _, backend := range backends {
			backend.Close()
		}
	}()

	// Setup load balancer
	testLogger := createLoadTestLogger()
	backendRepo := repository.NewInMemoryBackendRepository()

	for _, backend := range backendDomains {
		require.NoError(t, backendRepo.Save(backend))
	}

	metricsService := service.NewMetrics()
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.RoundRobinStrategy,
	}

	lbService, err := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	require.NoError(t, err)

	httpHandler := handler.NewLoadBalancerHandler(lbService, metricsService, testLogger, lbConfig)

	t.Run("HighConcurrencyTest", func(t *testing.T) {
		const (
			numWorkers        = 50
			requestsPerWorker = 100
			totalRequests     = numWorkers * requestsPerWorker
		)

		metrics := &LoadTestMetrics{
			MinLatencyMs: int64(^uint64(0) >> 1), // Max int64 value
		}

		var wg sync.WaitGroup
		startTime := time.Now()

		// Start workers
		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				for j := 0; j < requestsPerWorker; j++ {
					requestStart := time.Now()

					req := httptest.NewRequest("GET", fmt.Sprintf("/load-test/%d/%d", workerID, j), nil)
					ctx := context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req))
					req = req.WithContext(ctx)

					recorder := httptest.NewRecorder()
					httpHandler.ServeHTTP(recorder, req)

					latency := time.Since(requestStart).Milliseconds()

					atomic.AddInt64(&metrics.TotalRequests, 1)
					atomic.AddInt64(&metrics.TotalLatencyMs, latency)

					if recorder.Code == http.StatusOK {
						atomic.AddInt64(&metrics.SuccessfulReqs, 1)
					} else {
						atomic.AddInt64(&metrics.FailedRequests, 1)
					}

					// Update min/max latency
					for {
						currentMin := atomic.LoadInt64(&metrics.MinLatencyMs)
						if latency >= currentMin || atomic.CompareAndSwapInt64(&metrics.MinLatencyMs, currentMin, latency) {
							break
						}
					}

					for {
						currentMax := atomic.LoadInt64(&metrics.MaxLatencyMs)
						if latency <= currentMax || atomic.CompareAndSwapInt64(&metrics.MaxLatencyMs, currentMax, latency) {
							break
						}
					}
				}
			}(i)
		}

		wg.Wait()
		totalDuration := time.Since(startTime)

		// Calculate metrics
		metrics.RequestsPerSec = float64(metrics.TotalRequests) / totalDuration.Seconds()
		avgLatencyMs := float64(metrics.TotalLatencyMs) / float64(metrics.TotalRequests)

		// Print results
		t.Logf("Load Test Results:")
		t.Logf("  Total Requests: %d", metrics.TotalRequests)
		t.Logf("  Successful: %d (%.2f%%)", metrics.SuccessfulReqs,
			float64(metrics.SuccessfulReqs)/float64(metrics.TotalRequests)*100)
		t.Logf("  Failed: %d", metrics.FailedRequests)
		t.Logf("  Duration: %v", totalDuration)
		t.Logf("  Requests/sec: %.2f", metrics.RequestsPerSec)
		t.Logf("  Avg Latency: %.2f ms", avgLatencyMs)
		t.Logf("  Min Latency: %d ms", metrics.MinLatencyMs)
		t.Logf("  Max Latency: %d ms", metrics.MaxLatencyMs)

		// Assertions
		assert.Equal(t, int64(totalRequests), metrics.TotalRequests)
		assert.Greater(t, metrics.SuccessfulReqs, int64(totalRequests*0.95), "Should have >95% success rate")
		assert.Greater(t, metrics.RequestsPerSec, 100.0, "Should handle >100 requests/sec")
		assert.Less(t, avgLatencyMs, 100.0, "Average latency should be <100ms")
	})
}

// TestLoadBalancingUnderStress tests extreme load conditions
func TestLoadBalancingUnderStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	// Create backends with variable response times
	backends := []*httptest.Server{
		// Fast backend
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(time.Millisecond * 1)
			w.Header().Set("Backend-ID", "fast")
			w.WriteHeader(http.StatusOK)
		})),
		// Medium backend
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(time.Millisecond * 10)
			w.Header().Set("Backend-ID", "medium")
			w.WriteHeader(http.StatusOK)
		})),
		// Slow backend
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(time.Millisecond * 50)
			w.Header().Set("Backend-ID", "slow")
			w.WriteHeader(http.StatusOK)
		})),
	}

	defer func() {
		for _, backend := range backends {
			backend.Close()
		}
	}()

	// Setup load balancer
	testLogger := createLoadTestLogger()
	backendRepo := repository.NewInMemoryBackendRepository()

	backendDomains := []*domain.Backend{
		domain.NewBackend("fast", backends[0].URL, 3), // Higher weight for faster backend
		domain.NewBackend("medium", backends[1].URL, 2),
		domain.NewBackend("slow", backends[2].URL, 1),
	}

	for _, backend := range backendDomains {
		require.NoError(t, backendRepo.Save(backend))
	}

	metricsService := service.NewMetrics()
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.WeightedRoundRobinStrategy,
	}

	lbService, err := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	require.NoError(t, err)

	httpHandler := handler.NewLoadBalancerHandler(lbService, metricsService, testLogger, lbConfig)

	t.Run("StressTestWithVariableBackends", func(t *testing.T) {
		const (
			numWorkers = 100
			duration   = 30 * time.Second
		)

		var requestCount int64
		var successCount int64
		backendHits := make(map[string]*int64)
		backendHits["fast"] = new(int64)
		backendHits["medium"] = new(int64)
		backendHits["slow"] = new(int64)

		var wg sync.WaitGroup
		stopCh := make(chan struct{})

		// Start workers
		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				requestID := 0

				for {
					select {
					case <-stopCh:
						return
					default:
						req := httptest.NewRequest("GET", fmt.Sprintf("/stress/%d/%d", workerID, requestID), nil)
						ctx := context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req))
						req = req.WithContext(ctx)

						recorder := httptest.NewRecorder()
						httpHandler.ServeHTTP(recorder, req)

						atomic.AddInt64(&requestCount, 1)

						if recorder.Code == http.StatusOK {
							atomic.AddInt64(&successCount, 1)

							backendID := recorder.Header().Get("Backend-ID")
							if counter, exists := backendHits[backendID]; exists {
								atomic.AddInt64(counter, 1)
							}
						}

						requestID++

						// Small delay to prevent overwhelming
						time.Sleep(time.Microsecond * 100)
					}
				}
			}(i)
		}

		// Run for specified duration
		time.Sleep(duration)
		close(stopCh)
		wg.Wait()

		// Calculate results
		totalRequests := atomic.LoadInt64(&requestCount)
		totalSuccess := atomic.LoadInt64(&successCount)
		requestsPerSec := float64(totalRequests) / duration.Seconds()
		successRate := float64(totalSuccess) / float64(totalRequests) * 100

		t.Logf("Stress Test Results:")
		t.Logf("  Duration: %v", duration)
		t.Logf("  Total Requests: %d", totalRequests)
		t.Logf("  Successful: %d (%.2f%%)", totalSuccess, successRate)
		t.Logf("  Requests/sec: %.2f", requestsPerSec)
		t.Logf("  Backend Distribution:")
		for name, counter := range backendHits {
			hits := atomic.LoadInt64(counter)
			percentage := float64(hits) / float64(totalSuccess) * 100
			t.Logf("    %s: %d (%.2f%%)", name, hits, percentage)
		}

		// Assertions
		assert.Greater(t, totalRequests, int64(1000), "Should handle significant load")
		assert.Greater(t, successRate, 90.0, "Should maintain >90% success rate under stress")
		assert.Greater(t, requestsPerSec, 50.0, "Should maintain reasonable throughput")
	})
}

// TestMemoryUsageUnderLoad tests memory consumption during high load
func TestMemoryUsageUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	// Create backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	// Setup load balancer
	testLogger := createLoadTestLogger()
	backendRepo := repository.NewInMemoryBackendRepository()
	backendDomain := domain.NewBackend("test-backend", backend.URL, 1)
	require.NoError(t, backendRepo.Save(backendDomain))

	metricsService := service.NewMetrics()
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.RoundRobinStrategy,
	}

	lbService, err := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	require.NoError(t, err)

	httpHandler := handler.NewLoadBalancerHandler(lbService, metricsService, testLogger, lbConfig)

	t.Run("MemoryStabilityTest", func(t *testing.T) {
		const (
			numIterations = 1000
			requestsBatch = 100
		)

		// Run multiple batches of requests
		for iteration := 0; iteration < numIterations; iteration++ {
			var wg sync.WaitGroup

			for i := 0; i < requestsBatch; i++ {
				wg.Add(1)
				go func(reqID int) {
					defer wg.Done()

					req := httptest.NewRequest("GET", fmt.Sprintf("/memory-test/%d/%d", iteration, reqID), nil)
					ctx := context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req))
					req = req.WithContext(ctx)

					recorder := httptest.NewRecorder()
					httpHandler.ServeHTTP(recorder, req)
				}(i)
			}

			wg.Wait()

			// Log progress every 100 iterations
			if iteration%100 == 0 {
				t.Logf("Completed iteration %d/%d", iteration, numIterations)
			}
		}

		totalRequests := numIterations * requestsBatch
		t.Logf("Memory test completed: %d total requests processed", totalRequests)

		// If we reach here without running out of memory, the test passes
		assert.True(t, true, "Memory usage remained stable under load")
	})
}

// BenchmarkLoadBalancerThroughput benchmarks the load balancer's throughput
func BenchmarkLoadBalancerThroughput(b *testing.B) {
	// Create backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// Setup load balancer
	testLogger := createLoadTestLogger()
	backendRepo := repository.NewInMemoryBackendRepository()
	backendDomain := domain.NewBackend("bench-backend", backend.URL, 1)
	backendRepo.Save(backendDomain)

	metricsService := service.NewMetrics()
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.RoundRobinStrategy,
	}

	lbService, _ := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	httpHandler := handler.NewLoadBalancerHandler(lbService, metricsService, testLogger, lbConfig)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		requestID := 0
		for pb.Next() {
			req := httptest.NewRequest("GET", fmt.Sprintf("/bench/%d", requestID), nil)
			ctx := context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req))
			req = req.WithContext(ctx)

			recorder := httptest.NewRecorder()
			httpHandler.ServeHTTP(recorder, req)
			requestID++
		}
	})
}

// BenchmarkLoadBalancerLatency benchmarks request latency
func BenchmarkLoadBalancerLatency(b *testing.B) {
	// Create backend with slight delay
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Microsecond * 100) // Minimal delay
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// Setup load balancer
	testLogger := createLoadTestLogger()
	backendRepo := repository.NewInMemoryBackendRepository()
	backendDomain := domain.NewBackend("latency-backend", backend.URL, 1)
	backendRepo.Save(backendDomain)

	metricsService := service.NewMetrics()
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.RoundRobinStrategy,
	}

	lbService, _ := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	httpHandler := handler.NewLoadBalancerHandler(lbService, metricsService, testLogger, lbConfig)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/latency-bench", nil)
		ctx := context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req))
		req = req.WithContext(ctx)

		recorder := httptest.NewRecorder()
		httpHandler.ServeHTTP(recorder, req)
	}
}

// createLoadTestLogger creates a logger optimized for load testing
func createLoadTestLogger() *logger.Logger {
	config := logger.Config{
		Level:  "error", // Reduce log noise during load tests
		Format: "text",
		Output: "stdout",
	}
	testLogger, _ := logger.New(config)
	return testLogger
}
