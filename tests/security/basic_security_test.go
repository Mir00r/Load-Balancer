package security

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestSecurityHeaders tests security-related HTTP headers
func TestSecurityHeaders(t *testing.T) {
	// Create backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	// Create logger
	testLogger := createBasicSecurityLogger()

	// Setup load balancer
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

	t.Run("MaliciousHeaders", func(t *testing.T) {
		maliciousHeaders := []struct {
			name  string
			key   string
			value string
		}{
			{"CRLF Injection", "X-Custom", "value\r\nX-Injected: malicious"},
			{"Large Header", "X-Large", strings.Repeat("A", 50000)},
			{"Null Byte", "X-Null", "value\x00malicious"},
			{"Control Characters", "X-Control", "value\x01\x02\x03"},
			{"Response Splitting", "X-Test", "normal\r\n\r\nHTTP/1.1 200 OK\r\n"},
		}

		for _, headerTest := range maliciousHeaders {
			t.Run("Test_"+headerTest.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", "/", nil)
				req.Header.Set(headerTest.key, headerTest.value)
				req = req.WithContext(context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req)))

				recorder := httptest.NewRecorder()
				httpHandler.ServeHTTP(recorder, req)

				// Should handle malicious headers gracefully (not crash)
				assert.NotEqual(t, 0, recorder.Code, "Should return some response code")

				// Check that response doesn't contain injected content
				responseHeaders := recorder.HeaderMap
				for _, values := range responseHeaders {
					for _, value := range values {
						assert.NotContains(t, value, "\r\n", "Response should not contain CRLF injection")
						assert.NotContains(t, value, "\x00", "Response should not contain null bytes")
					}
				}
			})
		}
	})
}

// TestHighFrequencyRequests tests protection against high-frequency attacks
func TestHighFrequencyRequests(t *testing.T) {
	// Create backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add small delay to simulate real processing
		time.Sleep(time.Millisecond * 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	// Create logger
	testLogger := createBasicSecurityLogger()

	// Setup load balancer
	backendRepo := repository.NewInMemoryBackendRepository()
	backendDomain := domain.NewBackend("test-backend", backend.URL, 1)
	require.NoError(t, backendRepo.Save(backendDomain))

	metricsService := service.NewMetrics()
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.RoundRobinStrategy,
		Timeout:  time.Second * 5,
	}

	lbService, err := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	require.NoError(t, err)

	httpHandler := handler.NewLoadBalancerHandler(lbService, metricsService, testLogger, lbConfig)

	t.Run("BurstTraffic", func(t *testing.T) {
		const (
			numRequests = 1000
			numWorkers  = 50
		)

		var successCount int64
		var errorCount int64

		var wg sync.WaitGroup
		requestCh := make(chan int, numRequests)

		// Fill request channel
		for i := 0; i < numRequests; i++ {
			requestCh <- i
		}
		close(requestCh)

		// Start workers
		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				for requestID := range requestCh {
					req := httptest.NewRequest("GET", fmt.Sprintf("/burst/%d/%d", workerID, requestID), nil)
					req.RemoteAddr = fmt.Sprintf("192.168.%d.%d:12345", workerID%255, requestID%255)
					req = req.WithContext(context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req)))

					recorder := httptest.NewRecorder()
					httpHandler.ServeHTTP(recorder, req)

					if recorder.Code == http.StatusOK {
						atomic.AddInt64(&successCount, 1)
					} else {
						atomic.AddInt64(&errorCount, 1)
					}
				}
			}(i)
		}

		wg.Wait()

		totalRequests := atomic.LoadInt64(&successCount) + atomic.LoadInt64(&errorCount)
		successRate := float64(atomic.LoadInt64(&successCount)) / float64(totalRequests) * 100

		t.Logf("Burst traffic test: %d total, %d success (%.2f%%), %d errors",
			totalRequests, atomic.LoadInt64(&successCount), successRate, atomic.LoadInt64(&errorCount))

		// Should handle most requests successfully
		assert.Equal(t, int64(numRequests), totalRequests, "Should process all requests")
		assert.Greater(t, successRate, 80.0, "Should maintain reasonable success rate during burst")
	})
}

// TestMaliciousPayloads tests handling of potentially malicious request payloads
func TestMaliciousPayloads(t *testing.T) {
	// Create backend server that echoes request info
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("Method: %s, Path: %s", r.Method, r.URL.Path)))
	}))
	defer backend.Close()

	// Create logger
	testLogger := createBasicSecurityLogger()

	// Setup load balancer
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

	maliciousRequests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"SQL in URL", "GET", "/search?q=' OR 1=1--", ""},
		{"XSS in URL", "GET", "/profile?name=<script>alert('xss')</script>", ""},
		{"Path Traversal", "GET", "/../../../etc/passwd", ""},
		{"Command Injection", "GET", "/ping?host=; rm -rf /", ""},
		{"Long URL", "GET", "/" + strings.Repeat("a", 10000), ""},
		{"Unicode Bypass", "GET", "/admin\u202e", ""},
		{"Double Encoding", "GET", "/%252e%252e%252f", ""},
	}

	for _, maliciousReq := range maliciousRequests {
		t.Run("Handle_"+maliciousReq.name, func(t *testing.T) {
			var body *strings.Reader
			if maliciousReq.body != "" {
				body = strings.NewReader(maliciousReq.body)
			}

			req := httptest.NewRequest(maliciousReq.method, maliciousReq.path, body)
			req = req.WithContext(context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req)))

			recorder := httptest.NewRecorder()
			httpHandler.ServeHTTP(recorder, req)

			// Should handle requests without crashing
			assert.NotEqual(t, 0, recorder.Code, "Should return some response code for: %s", maliciousReq.name)

			// Response shouldn't contain obvious injection results
			responseBody := recorder.Body.String()
			assert.NotContains(t, responseBody, "<script>", "Response should not contain unescaped script tags")
		})
	}
}

// TestConnectionExhaustion tests protection against connection exhaustion attacks
func TestConnectionExhaustion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping connection exhaustion test in short mode")
	}

	// Create backend server with processing delay
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond * 100) // Simulate processing time
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	// Create logger
	testLogger := createBasicSecurityLogger()

	// Setup load balancer
	backendRepo := repository.NewInMemoryBackendRepository()
	backendDomain := domain.NewBackend("test-backend", backend.URL, 1)
	backendDomain.MaxConnections = 20 // Limit connections for testing
	require.NoError(t, backendRepo.Save(backendDomain))

	metricsService := service.NewMetrics()
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.RoundRobinStrategy,
		Timeout:  time.Second * 5,
	}

	lbService, err := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	require.NoError(t, err)

	httpHandler := handler.NewLoadBalancerHandler(lbService, metricsService, testLogger, lbConfig)

	t.Run("ManySlowConnections", func(t *testing.T) {
		const numSlowConnections = 30 // More than backend limit

		var wg sync.WaitGroup
		results := make(chan int, numSlowConnections)

		// Create many slow connections simultaneously
		for i := 0; i < numSlowConnections; i++ {
			wg.Add(1)
			go func(connID int) {
				defer wg.Done()

				req := httptest.NewRequest("GET", fmt.Sprintf("/slow-%d", connID), nil)
				req = req.WithContext(context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req)))

				recorder := httptest.NewRecorder()
				httpHandler.ServeHTTP(recorder, req)

				results <- recorder.Code
			}(i)
		}

		// Also test that we can still serve legitimate quick requests
		quickSuccess := 0
		for i := 0; i < 5; i++ {
			req := httptest.NewRequest("GET", fmt.Sprintf("/quick-%d", i), nil)
			req = req.WithContext(context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req)))

			recorder := httptest.NewRecorder()
			httpHandler.ServeHTTP(recorder, req)

			if recorder.Code == http.StatusOK {
				quickSuccess++
			}
		}

		wg.Wait()
		close(results)

		successCount := 0
		errorCount := 0
		for statusCode := range results {
			if statusCode == http.StatusOK {
				successCount++
			} else {
				errorCount++
			}
		}

		t.Logf("Connection exhaustion test: %d success, %d errors, %d quick success",
			successCount, errorCount, quickSuccess)

		// Should handle connections appropriately, not hang indefinitely
		assert.Equal(t, numSlowConnections, successCount+errorCount, "All connections should be handled")

		// Should still be able to serve some quick requests
		assert.Greater(t, quickSuccess, 0, "Should still serve some quick requests during slow connection attack")
	})
}

// TestResourceExhaustion tests handling of resource-intensive requests
func TestResourceExhaustion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping resource exhaustion test in short mode")
	}

	// Create backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	// Create logger
	testLogger := createBasicSecurityLogger()

	// Setup load balancer
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

	t.Run("LargeRequests", func(t *testing.T) {
		// Test handling of large request bodies
		largeBody := strings.Repeat("X", 1024*1024) // 1MB body

		req := httptest.NewRequest("POST", "/upload", strings.NewReader(largeBody))
		req.Header.Set("Content-Type", "text/plain")
		req = req.WithContext(context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req)))

		recorder := httptest.NewRecorder()
		httpHandler.ServeHTTP(recorder, req)

		// Should handle large requests without crashing
		assert.NotEqual(t, 0, recorder.Code, "Should handle large request")

		t.Logf("Large request test: Status %d", recorder.Code)
	})

	t.Run("ManyHeadersRequest", func(t *testing.T) {
		// Test request with many headers
		req := httptest.NewRequest("GET", "/", nil)

		// Add many custom headers
		for i := 0; i < 1000; i++ {
			req.Header.Set(fmt.Sprintf("X-Custom-%d", i), fmt.Sprintf("value-%d", i))
		}

		req = req.WithContext(context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req)))

		recorder := httptest.NewRecorder()
		httpHandler.ServeHTTP(recorder, req)

		// Should handle request with many headers
		assert.NotEqual(t, 0, recorder.Code, "Should handle request with many headers")

		t.Logf("Many headers test: Status %d", recorder.Code)
	})
}

// TestConcurrentSecurityScenarios tests various security scenarios under concurrent load
func TestConcurrentSecurityScenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent security test in short mode")
	}

	// Create backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond * 10) // Simulate processing
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	// Create logger
	testLogger := createBasicSecurityLogger()

	// Setup load balancer
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

	t.Run("MixedTrafficUnderLoad", func(t *testing.T) {
		const (
			numWorkers        = 20
			requestsPerWorker = 50
			testDuration      = time.Second * 10
		)

		var totalRequests int64
		var successfulRequests int64

		ctx, cancel := context.WithTimeout(context.Background(), testDuration)
		defer cancel()

		var wg sync.WaitGroup

		// Simulate mixed legitimate and potentially malicious traffic
		attackPatterns := []string{
			"/normal/endpoint",
			"/search?q=<script>alert('xss')</script>",
			"/api/users?filter=' OR 1=1--",
			"/admin/../../../etc/passwd",
			"/upload",
			"/" + strings.Repeat("long", 1000),
		}

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				requestCount := 0
				for {
					select {
					case <-ctx.Done():
						return
					default:
						// Pick a pattern (mix of normal and potentially malicious)
						pattern := attackPatterns[requestCount%len(attackPatterns)]

						req := httptest.NewRequest("GET", pattern, nil)
						req.RemoteAddr = fmt.Sprintf("10.0.%d.%d:12345", workerID, requestCount%255)
						req = req.WithContext(context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req)))

						recorder := httptest.NewRecorder()
						httpHandler.ServeHTTP(recorder, req)

						atomic.AddInt64(&totalRequests, 1)
						if recorder.Code < 500 { // Accept any non-server-error response
							atomic.AddInt64(&successfulRequests, 1)
						}

						requestCount++
						if requestCount >= requestsPerWorker {
							return
						}

						time.Sleep(time.Millisecond * 50)
					}
				}
			}(i)
		}

		wg.Wait()

		total := atomic.LoadInt64(&totalRequests)
		successful := atomic.LoadInt64(&successfulRequests)
		successRate := float64(successful) / float64(total) * 100

		t.Logf("Mixed traffic test: %d total, %d successful (%.2f%%)", total, successful, successRate)

		// Should handle mixed traffic gracefully
		assert.Greater(t, total, int64(100), "Should process significant number of requests")
		assert.Greater(t, successRate, 80.0, "Should maintain reasonable success rate under mixed load")
	})
}

// TestSecurityResilience tests overall security resilience
func TestSecurityResilience(t *testing.T) {
	// Create backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Secure backend response"))
	}))
	defer backend.Close()

	// Create logger
	testLogger := createBasicSecurityLogger()

	// Setup load balancer
	backendRepo := repository.NewInMemoryBackendRepository()
	backendDomain := domain.NewBackend("secure-backend", backend.URL, 1)
	require.NoError(t, backendRepo.Save(backendDomain))

	metricsService := service.NewMetrics()
	lbConfig := domain.LoadBalancerConfig{
		Strategy: domain.RoundRobinStrategy,
	}

	lbService, err := service.NewLoadBalancer(lbConfig, backendRepo, nil, metricsService, testLogger)
	require.NoError(t, err)

	httpHandler := handler.NewLoadBalancerHandler(lbService, metricsService, testLogger, lbConfig)

	t.Run("OverallSecurityTest", func(t *testing.T) {
		// Test various security scenarios
		testCases := []struct {
			name        string
			method      string
			path        string
			headers     map[string]string
			expectError bool
		}{
			{"Normal Request", "GET", "/api/data", nil, false},
			{"Long URL", "GET", "/" + strings.Repeat("test/", 1000), nil, false},
			{"Many Query Params", "GET", "/?" + strings.Repeat("param=value&", 100), nil, false},
			{"Special Characters", "GET", "/api/test%20with%20spaces", nil, false},
			{"Unicode Path", "GET", "/api/测试", nil, false},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(tc.method, tc.path, nil)

				// Add custom headers if specified
				for key, value := range tc.headers {
					req.Header.Set(key, value)
				}

				req = req.WithContext(context.WithValue(req.Context(), "requestContext", domain.NewRequestContext(req)))

				recorder := httptest.NewRecorder()
				httpHandler.ServeHTTP(recorder, req)

				// Should handle all requests gracefully
				assert.NotEqual(t, 0, recorder.Code, "Should return some response for: %s", tc.name)

				if !tc.expectError {
					assert.Less(t, recorder.Code, 500, "Should not return server error for legitimate request: %s", tc.name)
				}
			})
		}
	})
}

// createBasicSecurityLogger creates a logger optimized for security testing
func createBasicSecurityLogger() *logger.Logger {
	config := logger.Config{
		Level:  "warn", // Reduce noise but capture security events
		Format: "text",
		Output: "stdout",
	}
	testLogger, _ := logger.New(config)
	return testLogger
}
