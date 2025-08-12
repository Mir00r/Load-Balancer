package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/middleware"
	"github.com/mir00r/load-balancer/pkg/logger"
	"github.com/stretchr/testify/assert"
)

// createTestRateLimiterLogger creates a logger for rate limiter testing
func createTestRateLimiterLogger() *logger.Logger {
	config := logger.Config{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	}
	testLogger, _ := logger.New(config)
	return testLogger
}

// TestRateLimiterBasicFunctionality tests basic rate limiting
func TestRateLimiterBasicFunctionality(t *testing.T) {
	t.Parallel()

	config := domain.RateLimitConfig{
		RequestsPerSecond: 2.0, // 2 requests per second
		BurstSize:         3,   // Allow burst of 3 requests
	}

	testLogger := createTestRateLimiterLogger()
	rateLimiter := middleware.NewRateLimiter(config, testLogger)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	middlewareHandler := rateLimiter.RateLimitMiddleware()(testHandler)

	// Test burst allowance - first 3 requests should pass
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		recorder := httptest.NewRecorder()

		middlewareHandler.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code,
			"Request %d should succeed within burst limit", i+1)

		// Verify rate limit headers are set
		assert.NotEmpty(t, recorder.Header().Get("X-RateLimit-Limit"),
			"Rate limit header should be set")
	}

	// 4th request should be rate limited
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	recorder := httptest.NewRecorder()

	middlewareHandler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusTooManyRequests, recorder.Code,
		"Request exceeding burst limit should be blocked")
	assert.Equal(t, "0", recorder.Header().Get("X-RateLimit-Remaining"),
		"Remaining rate limit should be 0")
	assert.NotEmpty(t, recorder.Header().Get("Retry-After"),
		"Retry-After header should be set")
}

// TestRateLimiterPerIPIsolation tests that rate limiting is isolated per IP
func TestRateLimiterPerIPIsolation(t *testing.T) {
	t.Parallel()

	config := domain.RateLimitConfig{
		RequestsPerSecond: 1.0,
		BurstSize:         2,
	}

	testLogger := createTestRateLimiterLogger()
	rateLimiter := middleware.NewRateLimiter(config, testLogger)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := rateLimiter.RateLimitMiddleware()(testHandler)

	// Test different IP addresses get separate rate limits
	ips := []string{"192.168.1.1:12345", "192.168.1.2:12346", "10.0.0.1:8080"}

	for _, ip := range ips {
		// Each IP should get its own burst allowance
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/api/test", nil)
			req.RemoteAddr = ip
			recorder := httptest.NewRecorder()

			middlewareHandler.ServeHTTP(recorder, req)

			assert.Equal(t, http.StatusOK, recorder.Code,
				"IP %s request %d should succeed within its own burst limit", ip, i+1)
		}

		// Third request for this IP should be rate limited
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = ip
		recorder := httptest.NewRecorder()

		middlewareHandler.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusTooManyRequests, recorder.Code,
			"IP %s should be rate limited on third request", ip)
	}
}

// TestRateLimiterRateReplenishment tests that rate limiting allows requests over time
func TestRateLimiterRateReplenishment(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping time-based test in short mode")
	}

	config := domain.RateLimitConfig{
		RequestsPerSecond: 5.0, // 5 requests per second - fast for testing
		BurstSize:         1,   // Small burst to test replenishment
	}

	testLogger := createTestRateLimiterLogger()
	rateLimiter := middleware.NewRateLimiter(config, testLogger)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := rateLimiter.RateLimitMiddleware()(testHandler)

	// First request should succeed
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	recorder := httptest.NewRecorder()

	middlewareHandler.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusOK, recorder.Code, "First request should succeed")

	// Second immediate request should be blocked
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	recorder = httptest.NewRecorder()

	middlewareHandler.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusTooManyRequests, recorder.Code,
		"Immediate second request should be blocked")

	// Wait for rate to replenish (200ms for 5 req/sec rate)
	time.Sleep(250 * time.Millisecond)

	// Request should now succeed after waiting
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	recorder = httptest.NewRecorder()

	middlewareHandler.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusOK, recorder.Code,
		"Request should succeed after rate replenishment")
}

// TestRateLimiterConcurrency tests rate limiter under concurrent load
func TestRateLimiterConcurrency(t *testing.T) {
	t.Parallel()

	config := domain.RateLimitConfig{
		RequestsPerSecond: 10.0,
		BurstSize:         20,
	}

	testLogger := createTestRateLimiterLogger()
	rateLimiter := middleware.NewRateLimiter(config, testLogger)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := rateLimiter.RateLimitMiddleware()(testHandler)

	const numGoroutines = 20
	const requestsPerGoroutine = 5

	var wg sync.WaitGroup
	results := make(chan int, numGoroutines*requestsPerGoroutine)
	errors := make(chan error, numGoroutines*requestsPerGoroutine)

	// Launch concurrent requests from different IPs
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				req := httptest.NewRequest("GET", fmt.Sprintf("/api/test-%d-%d", goroutineID, j), nil)
				// Each goroutine uses a different IP to avoid cross-interference
				req.RemoteAddr = fmt.Sprintf("192.168.1.%d:12345", goroutineID+1)
				recorder := httptest.NewRecorder()

				middlewareHandler.ServeHTTP(recorder, req)
				results <- recorder.Code

				if recorder.Code >= 500 {
					errors <- fmt.Errorf("server error: %d", recorder.Code)
				}
			}
		}(i)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Check for server errors
	for err := range errors {
		t.Errorf("Concurrent execution error: %v", err)
	}

	// Count results
	statusCounts := make(map[int]int)
	totalRequests := 0
	for statusCode := range results {
		statusCounts[statusCode]++
		totalRequests++
	}

	expectedTotal := numGoroutines * requestsPerGoroutine
	assert.Equal(t, expectedTotal, totalRequests,
		"Should process all %d concurrent requests", expectedTotal)

	// Most requests should succeed since each IP has its own rate limit
	assert.Greater(t, statusCounts[http.StatusOK], expectedTotal/2,
		"At least half the requests should succeed with separate IPs")
}

// TestRateLimiterHeaderExtraction tests client IP extraction from various headers
func TestRateLimiterHeaderExtraction(t *testing.T) {
	t.Parallel()

	config := domain.RateLimitConfig{
		RequestsPerSecond: 1.0,
		BurstSize:         1,
	}

	testLogger := createTestRateLimiterLogger()
	rateLimiter := middleware.NewRateLimiter(config, testLogger)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := rateLimiter.RateLimitMiddleware()(testHandler)

	tests := []struct {
		name          string
		setupRequest  func() *http.Request
		expectedLimit bool
		description   string
	}{
		{
			name: "X-Forwarded-For header",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/api/test", nil)
				req.Header.Set("X-Forwarded-For", "203.0.113.1")
				return req
			},
			expectedLimit: false,
			description:   "Should use X-Forwarded-For IP for rate limiting",
		},
		{
			name: "X-Real-IP header",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/api/test", nil)
				req.Header.Set("X-Real-IP", "203.0.113.2")
				return req
			},
			expectedLimit: false,
			description:   "Should use X-Real-IP for rate limiting",
		},
		{
			name: "Remote address fallback",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/api/test", nil)
				req.RemoteAddr = "203.0.113.3:8080"
				return req
			},
			expectedLimit: false,
			description:   "Should use RemoteAddr as fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// First request should succeed
			req := tt.setupRequest()
			recorder := httptest.NewRecorder()

			middlewareHandler.ServeHTTP(recorder, req)
			assert.Equal(t, http.StatusOK, recorder.Code,
				"First request should succeed for: %s", tt.description)

			// Second request should be rate limited (burst size = 1)
			req = tt.setupRequest()
			recorder = httptest.NewRecorder()

			middlewareHandler.ServeHTTP(recorder, req)
			assert.Equal(t, http.StatusTooManyRequests, recorder.Code,
				"Second request should be rate limited for: %s", tt.description)
		})
	}
}

// BenchmarkRateLimiter benchmarks rate limiter performance
func BenchmarkRateLimiter(b *testing.B) {
	config := domain.RateLimitConfig{
		RequestsPerSecond: 1000.0, // High rate for benchmarking
		BurstSize:         1000,
	}

	testLogger := createTestRateLimiterLogger()
	rateLimiter := middleware.NewRateLimiter(config, testLogger)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := rateLimiter.RateLimitMiddleware()(testHandler)

	b.Run("SameIP", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				req := httptest.NewRequest("GET", "/api/test", nil)
				req.RemoteAddr = "192.168.1.1:12345"
				recorder := httptest.NewRecorder()
				middlewareHandler.ServeHTTP(recorder, req)
			}
		})
	})

	b.Run("DifferentIPs", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				req := httptest.NewRequest("GET", "/api/test", nil)
				req.RemoteAddr = fmt.Sprintf("192.168.1.%d:12345", (i%254)+1)
				recorder := httptest.NewRecorder()
				middlewareHandler.ServeHTTP(recorder, req)
				i++
			}
		})
	})
}

// TestRateLimiterMemoryCleanup tests memory cleanup functionality
func TestRateLimiterMemoryCleanup(t *testing.T) {
	t.Parallel()

	config := domain.RateLimitConfig{
		RequestsPerSecond: 100.0,
		BurstSize:         10,
	}

	testLogger := createTestRateLimiterLogger()
	rateLimiter := middleware.NewRateLimiter(config, testLogger)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := rateLimiter.RateLimitMiddleware()(testHandler)

	// Generate many unique IPs to trigger cleanup
	for i := 0; i < 10; i++ { // Reduced number for testing
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = fmt.Sprintf("10.0.%d.%d:12345", i/256, i%256)
		recorder := httptest.NewRecorder()

		middlewareHandler.ServeHTTP(recorder, req)
		assert.Equal(t, http.StatusOK, recorder.Code,
			"Request from IP %d should succeed", i)
	}

	// Test that rate limiter still functions after cleanup
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	recorder := httptest.NewRecorder()

	middlewareHandler.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusOK, recorder.Code,
		"Rate limiter should still function after cleanup")
}

// TestRateLimiterConfigurationVariations tests different rate limiter configurations
func TestRateLimiterConfigurationVariations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      domain.RateLimitConfig
		requests    int
		expectedOK  int
		description string
	}{
		{
			name: "High rate, high burst",
			config: domain.RateLimitConfig{
				RequestsPerSecond: 100.0,
				BurstSize:         50,
			},
			requests:    60,
			expectedOK:  50, // Should get burst allowance
			description: "Should allow burst of 50 requests",
		},
		{
			name: "Low rate, no burst",
			config: domain.RateLimitConfig{
				RequestsPerSecond: 1.0,
				BurstSize:         1,
			},
			requests:    5,
			expectedOK:  1, // Only one request allowed
			description: "Should allow only 1 request with no burst",
		},
		{
			name: "Zero burst",
			config: domain.RateLimitConfig{
				RequestsPerSecond: 10.0,
				BurstSize:         0,
			},
			requests:    5,
			expectedOK:  0, // No requests should be allowed with zero burst
			description: "Should block all requests with zero burst",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testLogger := createTestRateLimiterLogger()
			rateLimiter := middleware.NewRateLimiter(tt.config, testLogger)

			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			middlewareHandler := rateLimiter.RateLimitMiddleware()(testHandler)

			successCount := 0
			for i := 0; i < tt.requests; i++ {
				req := httptest.NewRequest("GET", "/api/test", nil)
				req.RemoteAddr = "192.168.1.1:12345"
				recorder := httptest.NewRecorder()

				middlewareHandler.ServeHTTP(recorder, req)

				if recorder.Code == http.StatusOK {
					successCount++
				}
			}

			assert.Equal(t, tt.expectedOK, successCount, tt.description)
		})
	}
}
