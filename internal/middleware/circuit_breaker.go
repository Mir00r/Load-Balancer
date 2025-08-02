package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	// StateClosed - Circuit breaker is closed, requests pass through
	StateClosed CircuitBreakerState = iota
	// StateOpen - Circuit breaker is open, requests are rejected
	StateOpen
	// StateHalfOpen - Circuit breaker allows limited requests to test recovery
	StateHalfOpen
)

// String returns the string representation of CircuitBreakerState
func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	config domain.CircuitBreakerConfig
	logger *logger.Logger

	// State management
	state           CircuitBreakerState
	failures        int
	requests        int
	successCount    int
	lastFailureTime time.Time
	nextAttempt     time.Time

	mu sync.RWMutex
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config domain.CircuitBreakerConfig, logger *logger.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		config: config,
		logger: logger.MiddlewareLogger("circuit_breaker"),
		state:  StateClosed,
	}
}

// Allow checks if a request should be allowed through the circuit breaker
func (cb *CircuitBreaker) Allow() bool {
	if !cb.config.Enabled {
		return true
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case StateClosed:
		// Circuit is closed, allow request
		return true

	case StateOpen:
		// Check if we should transition to half-open
		if now.After(cb.nextAttempt) {
			cb.state = StateHalfOpen
			cb.requests = 0
			cb.successCount = 0
			cb.logger.Info("Circuit breaker transitioning to half-open state")
			return true
		}
		// Circuit is still open, reject request
		return false

	case StateHalfOpen:
		// Allow limited requests in half-open state
		if cb.requests < cb.config.MaxRequests {
			return true
		}
		return false

	default:
		return false
	}
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	if !cb.config.Enabled {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		// Reset failure count on success
		cb.failures = 0

	case StateHalfOpen:
		cb.successCount++
		cb.requests++

		// If we have enough successful requests, close the circuit
		if cb.successCount >= cb.config.MaxRequests {
			cb.state = StateClosed
			cb.failures = 0
			cb.requests = 0
			cb.successCount = 0
			cb.logger.Info("Circuit breaker closing after successful recovery")
		}
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	if !cb.config.Enabled {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		// Check if we should open the circuit
		if cb.failures >= cb.config.FailureThreshold {
			cb.state = StateOpen
			cb.nextAttempt = time.Now().Add(cb.config.RecoveryTimeout)
			cb.logger.WithFields(map[string]interface{}{
				"failures":          cb.failures,
				"failure_threshold": cb.config.FailureThreshold,
				"next_attempt":      cb.nextAttempt,
			}).Warn("Circuit breaker opening due to failures")
		}

	case StateHalfOpen:
		// Go back to open state on failure
		cb.state = StateOpen
		cb.nextAttempt = time.Now().Add(cb.config.RecoveryTimeout)
		cb.requests = 0
		cb.successCount = 0
		cb.logger.Info("Circuit breaker opening again after failure in half-open state")
	}
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"enabled":           cb.config.Enabled,
		"state":             cb.state.String(),
		"failures":          cb.failures,
		"failure_threshold": cb.config.FailureThreshold,
		"last_failure_time": cb.lastFailureTime,
		"next_attempt":      cb.nextAttempt,
		"recovery_timeout":  cb.config.RecoveryTimeout.String(),
		"max_requests":      cb.config.MaxRequests,
	}
}

// CircuitBreakerMiddleware provides circuit breaker functionality for HTTP handlers
func CircuitBreakerMiddleware(cb *CircuitBreaker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if request is allowed
			if !cb.Allow() {
				cb.logger.WithFields(map[string]interface{}{
					"path":   r.URL.Path,
					"method": r.Method,
					"state":  cb.GetState().String(),
				}).Warn("Request rejected by circuit breaker")

				w.Header().Set("X-Circuit-Breaker-State", cb.GetState().String())
				http.Error(w, "Service Unavailable - Circuit Breaker Open", http.StatusServiceUnavailable)
				return
			}

			// Create a response writer wrapper to capture the status code
			wrapper := &circuitBreakerResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Process the request
			next.ServeHTTP(wrapper, r)

			// Record success or failure based on status code
			if wrapper.statusCode >= 500 {
				cb.RecordFailure()
			} else {
				cb.RecordSuccess()
			}

			// Add circuit breaker state header
			wrapper.Header().Set("X-Circuit-Breaker-State", cb.GetState().String())
		})
	}
}

// circuitBreakerResponseWriter wraps http.ResponseWriter to capture status code
type circuitBreakerResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code
func (w *circuitBreakerResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Reset resets the circuit breaker to its initial state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.failures = 0
	cb.requests = 0
	cb.successCount = 0
	cb.lastFailureTime = time.Time{}
	cb.nextAttempt = time.Time{}

	cb.logger.Info("Circuit breaker reset to closed state")
}
