package middleware

import (
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

// createTestCircuitBreakerLogger creates a logger for circuit breaker testing
func createTestCircuitBreakerLogger() *logger.Logger {
	config := logger.Config{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	}
	testLogger, _ := logger.New(config)
	return testLogger
}

// TestCircuitBreakerBasicFunctionality tests basic circuit breaker operation
func TestCircuitBreakerBasicFunctionality(t *testing.T) {
	t.Parallel()

	config := domain.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 3,
		RecoveryTimeout:  time.Millisecond * 100,
		MaxRequests:      2,
	}

	testLogger := createTestCircuitBreakerLogger()
	circuitBreaker := middleware.NewCircuitBreaker(config, testLogger)

	// Initially, circuit breaker should be closed and allow requests
	assert.True(t, circuitBreaker.Allow(), "Circuit breaker should initially allow requests")

	// Simulate failures to trigger opening
	for i := 0; i < 3; i++ {
		circuitBreaker.RecordFailure()
	}

	// Circuit breaker should now be open and reject requests
	assert.False(t, circuitBreaker.Allow(), "Circuit breaker should be open after failures")

	// Wait for recovery timeout
	time.Sleep(time.Millisecond * 150)

	// Circuit breaker should allow limited requests in half-open state
	assert.True(t, circuitBreaker.Allow(), "Circuit breaker should allow requests in half-open state")
}

// TestCircuitBreakerMiddleware tests the circuit breaker middleware
func TestCircuitBreakerMiddleware(t *testing.T) {
	t.Parallel()

	config := domain.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 2,
		RecoveryTimeout:  time.Millisecond * 50,
		MaxRequests:      1,
	}

	testLogger := createTestCircuitBreakerLogger()
	circuitBreaker := middleware.NewCircuitBreaker(config, testLogger)

	failureCount := 0
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate failures for the first few requests
		if failureCount < 2 {
			failureCount++
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Success after failures
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := middleware.CircuitBreakerMiddleware(circuitBreaker)(testHandler)

	// First two requests should pass through but fail
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		recorder := httptest.NewRecorder()

		middlewareHandler.ServeHTTP(recorder, req)
		assert.Equal(t, http.StatusInternalServerError, recorder.Code,
			"Request %d should pass through but fail", i+1)
	}

	// Next request should be blocked by open circuit
	req := httptest.NewRequest("GET", "/api/test", nil)
	recorder := httptest.NewRecorder()

	middlewareHandler.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code,
		"Request should be blocked by open circuit breaker")

	// Wait for recovery timeout
	time.Sleep(time.Millisecond * 100)

	// Circuit should be half-open and allow one request
	req = httptest.NewRequest("GET", "/api/test", nil)
	recorder = httptest.NewRecorder()

	middlewareHandler.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusOK, recorder.Code,
		"Request should succeed in half-open state")
}

// TestCircuitBreakerStates tests state transitions
func TestCircuitBreakerStates(t *testing.T) {
	t.Parallel()

	config := domain.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 2,
		RecoveryTimeout:  time.Millisecond * 50,
		MaxRequests:      3,
	}

	testLogger := createTestCircuitBreakerLogger()
	circuitBreaker := middleware.NewCircuitBreaker(config, testLogger)

	// Test initial closed state
	assert.True(t, circuitBreaker.Allow(), "Should allow requests in closed state")

	// Record failures to trigger state change
	circuitBreaker.RecordFailure()
	assert.True(t, circuitBreaker.Allow(), "Should still allow requests after one failure")

	circuitBreaker.RecordFailure()
	// After reaching failure threshold, circuit should open
	assert.False(t, circuitBreaker.Allow(), "Should block requests in open state")

	// Wait for recovery timeout
	time.Sleep(time.Millisecond * 100)

	// Should now be in half-open state
	assert.True(t, circuitBreaker.Allow(), "Should allow limited requests in half-open state")
}

// TestCircuitBreakerRecovery tests recovery scenarios
func TestCircuitBreakerRecovery(t *testing.T) {
	t.Parallel()

	config := domain.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 2,
		RecoveryTimeout:  time.Millisecond * 50,
		MaxRequests:      2,
	}

	testLogger := createTestCircuitBreakerLogger()
	circuitBreaker := middleware.NewCircuitBreaker(config, testLogger)

	// Trigger circuit opening
	circuitBreaker.RecordFailure()
	circuitBreaker.RecordFailure()
	assert.False(t, circuitBreaker.Allow(), "Circuit should be open")

	// Wait for recovery
	time.Sleep(time.Millisecond * 100)

	// Test successful recovery
	assert.True(t, circuitBreaker.Allow(), "Should allow request in half-open state")
	circuitBreaker.RecordSuccess()
	assert.True(t, circuitBreaker.Allow(), "Should allow second request in half-open state")
	circuitBreaker.RecordSuccess()

	// Circuit should remain functional after successful recovery
	assert.True(t, circuitBreaker.Allow(), "Circuit should continue to work after recovery")
}

// TestCircuitBreakerConcurrency tests circuit breaker under concurrent load
func TestCircuitBreakerConcurrency(t *testing.T) {
	t.Parallel()

	config := domain.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 5,
		RecoveryTimeout:  time.Millisecond * 100,
		MaxRequests:      3,
	}

	testLogger := createTestCircuitBreakerLogger()
	circuitBreaker := middleware.NewCircuitBreaker(config, testLogger)

	const numGoroutines = 20
	const requestsPerGoroutine = 10

	var wg sync.WaitGroup
	results := make(chan bool, numGoroutines*requestsPerGoroutine)

	// Launch concurrent requests
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				allowed := circuitBreaker.Allow()
				results <- allowed

				// Simulate some processing time
				time.Sleep(time.Microsecond * 100)

				// Record success for allowed requests
				if allowed {
					circuitBreaker.RecordSuccess()
				}
			}
		}()
	}

	wg.Wait()
	close(results)

	// Count allowed vs blocked requests
	allowedCount := 0
	totalRequests := 0
	for allowed := range results {
		if allowed {
			allowedCount++
		}
		totalRequests++
	}

	expectedTotal := numGoroutines * requestsPerGoroutine
	assert.Equal(t, expectedTotal, totalRequests,
		"Should process all concurrent requests")
	assert.Greater(t, allowedCount, 0,
		"Some requests should be allowed")
}

// TestCircuitBreakerDisabled tests behavior when circuit breaker is disabled
func TestCircuitBreakerDisabled(t *testing.T) {
	t.Parallel()

	config := domain.CircuitBreakerConfig{
		Enabled:          false, // Disabled
		FailureThreshold: 1,
		RecoveryTimeout:  time.Millisecond * 10,
		MaxRequests:      1,
	}

	testLogger := createTestCircuitBreakerLogger()
	circuitBreaker := middleware.NewCircuitBreaker(config, testLogger)

	// Even with failures, disabled circuit breaker should always allow requests
	for i := 0; i < 10; i++ {
		assert.True(t, circuitBreaker.Allow(),
			"Disabled circuit breaker should always allow requests")
		circuitBreaker.RecordFailure()
	}
}

// TestCircuitBreakerConfiguration tests different configurations
func TestCircuitBreakerConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      domain.CircuitBreakerConfig
		failures    int
		expectOpen  bool
		description string
	}{
		{
			name: "Low failure threshold",
			config: domain.CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 1,
				RecoveryTimeout:  time.Millisecond * 50,
				MaxRequests:      1,
			},
			failures:    1,
			expectOpen:  true,
			description: "Should open after single failure with low threshold",
		},
		{
			name: "High failure threshold",
			config: domain.CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 10,
				RecoveryTimeout:  time.Millisecond * 50,
				MaxRequests:      1,
			},
			failures:    5,
			expectOpen:  false,
			description: "Should remain closed with failures below high threshold",
		},
		{
			name: "Zero failure threshold",
			config: domain.CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 0,
				RecoveryTimeout:  time.Millisecond * 50,
				MaxRequests:      1,
			},
			failures:    0,
			expectOpen:  false,
			description: "Should handle zero failure threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testLogger := createTestCircuitBreakerLogger()
			circuitBreaker := middleware.NewCircuitBreaker(tt.config, testLogger)

			// Record specified number of failures
			for i := 0; i < tt.failures; i++ {
				circuitBreaker.RecordFailure()
			}

			allowed := circuitBreaker.Allow()
			if tt.expectOpen {
				assert.False(t, allowed, tt.description)
			} else {
				assert.True(t, allowed, tt.description)
			}
		})
	}
}

// BenchmarkCircuitBreaker benchmarks circuit breaker performance
func BenchmarkCircuitBreaker(b *testing.B) {
	config := domain.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 100,
		RecoveryTimeout:  time.Second,
		MaxRequests:      10,
	}

	testLogger := createTestCircuitBreakerLogger()
	circuitBreaker := middleware.NewCircuitBreaker(config, testLogger)

	b.Run("Allow", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				circuitBreaker.Allow()
			}
		})
	})

	b.Run("RecordSuccess", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				circuitBreaker.RecordSuccess()
			}
		})
	})

	b.Run("RecordFailure", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				circuitBreaker.RecordFailure()
			}
		})
	})
}

// TestCircuitBreakerMiddlewareIntegration tests full middleware integration
func TestCircuitBreakerMiddlewareIntegration(t *testing.T) {
	t.Parallel()

	config := domain.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 3,
		RecoveryTimeout:  time.Millisecond * 100,
		MaxRequests:      2,
	}

	testLogger := createTestCircuitBreakerLogger()
	circuitBreaker := middleware.NewCircuitBreaker(config, testLogger)

	requestCount := 0
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		// Fail first 3 requests, then succeed
		if requestCount <= 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	})

	middlewareHandler := middleware.CircuitBreakerMiddleware(circuitBreaker)(testHandler)

	scenarios := []struct {
		name           string
		delay          time.Duration
		expectedStatus int
		description    string
	}{
		{
			name:           "First failure",
			delay:          0,
			expectedStatus: http.StatusInternalServerError,
			description:    "Should pass through first failure",
		},
		{
			name:           "Second failure",
			delay:          0,
			expectedStatus: http.StatusInternalServerError,
			description:    "Should pass through second failure",
		},
		{
			name:           "Third failure - triggers opening",
			delay:          0,
			expectedStatus: http.StatusInternalServerError,
			description:    "Should pass through third failure before opening",
		},
		{
			name:           "Circuit open - blocked request",
			delay:          0,
			expectedStatus: http.StatusServiceUnavailable,
			description:    "Should block request when circuit is open",
		},
		{
			name:           "After recovery timeout - half open",
			delay:          time.Millisecond * 150,
			expectedStatus: http.StatusOK,
			description:    "Should allow request in half-open state and succeed",
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			if scenario.delay > 0 {
				time.Sleep(scenario.delay)
			}

			req := httptest.NewRequest("GET", "/api/test", nil)
			recorder := httptest.NewRecorder()

			middlewareHandler.ServeHTTP(recorder, req)

			assert.Equal(t, scenario.expectedStatus, recorder.Code, scenario.description)
		})
	}
}

// TestCircuitBreakerTimeout tests recovery timeout behavior
func TestCircuitBreakerTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping time-based test in short mode")
	}

	config := domain.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 1,
		RecoveryTimeout:  time.Millisecond * 200,
		MaxRequests:      1,
	}

	testLogger := createTestCircuitBreakerLogger()
	circuitBreaker := middleware.NewCircuitBreaker(config, testLogger)

	// Trigger circuit opening
	circuitBreaker.RecordFailure()
	assert.False(t, circuitBreaker.Allow(), "Circuit should be open immediately after failure")

	// Check that circuit remains open before timeout
	time.Sleep(time.Millisecond * 100) // Half the recovery timeout
	assert.False(t, circuitBreaker.Allow(), "Circuit should remain open before recovery timeout")

	// Wait for full recovery timeout
	time.Sleep(time.Millisecond * 150) // Total wait > recovery timeout
	assert.True(t, circuitBreaker.Allow(), "Circuit should allow requests after recovery timeout")

	// Test circuit breaker state retrieval
	state := circuitBreaker.GetState()
	assert.NotNil(t, state, "Should be able to get circuit breaker state")

	// Test circuit breaker stats
	stats := circuitBreaker.GetStats()
	assert.NotNil(t, stats, "Should be able to get circuit breaker stats")
	assert.Contains(t, stats, "state", "Stats should contain state information")
}

// TestCircuitBreakerReset tests the reset functionality
func TestCircuitBreakerReset(t *testing.T) {
	t.Parallel()

	config := domain.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 2,
		RecoveryTimeout:  time.Second, // Long timeout
		MaxRequests:      1,
	}

	testLogger := createTestCircuitBreakerLogger()
	circuitBreaker := middleware.NewCircuitBreaker(config, testLogger)

	// Trigger failures to open circuit
	circuitBreaker.RecordFailure()
	circuitBreaker.RecordFailure()
	assert.False(t, circuitBreaker.Allow(), "Circuit should be open")

	// Reset circuit breaker
	circuitBreaker.Reset()

	// Should be closed again
	assert.True(t, circuitBreaker.Allow(), "Circuit should be closed after reset")
}
