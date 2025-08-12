package errors

import (
	"errors"
	"fmt"
	"runtime"
	"time"
)

// ErrorCode represents a specific error type for better error handling
type ErrorCode string

const (
	// Infrastructure errors
	ErrCodeConfigLoad         ErrorCode = "CONFIG_LOAD_FAILED"
	ErrCodeBackendUnavailable ErrorCode = "BACKEND_UNAVAILABLE"
	ErrCodeBackendTimeout     ErrorCode = "BACKEND_TIMEOUT"
	ErrCodeConnectionPoolFull ErrorCode = "CONNECTION_POOL_FULL"
	ErrCodeHealthCheckFailed  ErrorCode = "HEALTH_CHECK_FAILED"

	// Load balancing errors
	ErrCodeNoBackends       ErrorCode = "NO_BACKENDS_AVAILABLE"
	ErrCodeInvalidStrategy  ErrorCode = "INVALID_STRATEGY"
	ErrCodeStrategyFailed   ErrorCode = "STRATEGY_FAILED"
	ErrCodeBackendSelection ErrorCode = "BACKEND_SELECTION_FAILED"

	// Request processing errors
	ErrCodeInvalidRequest       ErrorCode = "INVALID_REQUEST"
	ErrCodeAuthenticationFailed ErrorCode = "AUTHENTICATION_FAILED"
	ErrCodeAuthorizationFailed  ErrorCode = "AUTHORIZATION_FAILED"
	ErrCodeRateLimitExceeded    ErrorCode = "RATE_LIMIT_EXCEEDED"
	ErrCodeRequestTimeout       ErrorCode = "REQUEST_TIMEOUT"
	ErrCodeCircuitBreakerOpen   ErrorCode = "CIRCUIT_BREAKER_OPEN"

	// Security errors
	ErrCodeSecurityViolation ErrorCode = "SECURITY_VIOLATION"
	ErrCodeInvalidToken      ErrorCode = "INVALID_TOKEN"
	ErrCodeTokenExpired      ErrorCode = "TOKEN_EXPIRED"
	ErrCodeAccessDenied      ErrorCode = "ACCESS_DENIED"

	// Internal errors
	ErrCodeInternalError      ErrorCode = "INTERNAL_ERROR"
	ErrCodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"
	ErrCodeResourceExhausted  ErrorCode = "RESOURCE_EXHAUSTED"
)

// LoadBalancerError represents a structured error with context
type LoadBalancerError struct {
	Code       ErrorCode              `json:"code"`
	Message    string                 `json:"message"`
	Details    string                 `json:"details,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	RequestID  string                 `json:"request_id,omitempty"`
	Component  string                 `json:"component,omitempty"`
	StackTrace string                 `json:"stack_trace,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	Cause      error                  `json:"-"` // Original error
}

// Error implements the error interface
func (e *LoadBalancerError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("[%s][%s] %s: %s", e.RequestID, e.Code, e.Component, e.Message)
	}
	return fmt.Sprintf("[%s] %s: %s", e.Code, e.Component, e.Message)
}

// Unwrap returns the underlying error
func (e *LoadBalancerError) Unwrap() error {
	return e.Cause
}

// Is checks if this error matches the target error code
func (e *LoadBalancerError) Is(target error) bool {
	if t, ok := target.(*LoadBalancerError); ok {
		return e.Code == t.Code
	}
	return false
}

// WithMetadata adds metadata to the error
func (e *LoadBalancerError) WithMetadata(key string, value interface{}) *LoadBalancerError {
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	e.Metadata[key] = value
	return e
}

// WithRequestID adds request ID to the error
func (e *LoadBalancerError) WithRequestID(requestID string) *LoadBalancerError {
	e.RequestID = requestID
	return e
}

// WithStackTrace adds stack trace to the error
func (e *LoadBalancerError) WithStackTrace() *LoadBalancerError {
	e.StackTrace = getStackTrace()
	return e
}

// IsRetryable returns true if the error might be resolved by retrying
func (e *LoadBalancerError) IsRetryable() bool {
	switch e.Code {
	case ErrCodeBackendTimeout, ErrCodeConnectionPoolFull, ErrCodeServiceUnavailable:
		return true
	default:
		return false
	}
}

// HTTPStatusCode returns the appropriate HTTP status code for this error
func (e *LoadBalancerError) HTTPStatusCode() int {
	switch e.Code {
	case ErrCodeInvalidRequest:
		return 400
	case ErrCodeAuthenticationFailed, ErrCodeInvalidToken, ErrCodeTokenExpired:
		return 401
	case ErrCodeAuthorizationFailed, ErrCodeAccessDenied:
		return 403
	case ErrCodeRateLimitExceeded:
		return 429
	case ErrCodeBackendUnavailable, ErrCodeNoBackends, ErrCodeServiceUnavailable:
		return 503
	case ErrCodeRequestTimeout, ErrCodeBackendTimeout:
		return 504
	case ErrCodeInternalError, ErrCodeStrategyFailed:
		return 500
	default:
		return 500
	}
}

// NewError creates a new LoadBalancerError
func NewError(code ErrorCode, component, message string) *LoadBalancerError {
	return &LoadBalancerError{
		Code:      code,
		Component: component,
		Message:   message,
		Timestamp: time.Now(),
	}
}

// NewErrorWithCause creates a new LoadBalancerError with an underlying cause
func NewErrorWithCause(code ErrorCode, component, message string, cause error) *LoadBalancerError {
	return &LoadBalancerError{
		Code:      code,
		Component: component,
		Message:   message,
		Timestamp: time.Now(),
		Cause:     cause,
		Details:   cause.Error(),
	}
}

// WrapError wraps an existing error with LoadBalancerError structure
func WrapError(err error, code ErrorCode, component, message string) *LoadBalancerError {
	if err == nil {
		return nil
	}

	return &LoadBalancerError{
		Code:      code,
		Component: component,
		Message:   message,
		Timestamp: time.Now(),
		Cause:     err,
		Details:   err.Error(),
	}
}

// Common error constructors for frequently used errors

// NewBackendUnavailableError creates an error for unavailable backend
func NewBackendUnavailableError(backendID string, cause error) *LoadBalancerError {
	return NewErrorWithCause(
		ErrCodeBackendUnavailable,
		"load_balancer",
		fmt.Sprintf("Backend %s is unavailable", backendID),
		cause,
	).WithMetadata("backend_id", backendID)
}

// NewNoBackendsError creates an error when no backends are available
func NewNoBackendsError() *LoadBalancerError {
	return NewError(
		ErrCodeNoBackends,
		"load_balancer",
		"No healthy backends available for request",
	)
}

// NewStrategyError creates an error for strategy-related issues
func NewStrategyError(strategy string, cause error) *LoadBalancerError {
	return NewErrorWithCause(
		ErrCodeStrategyFailed,
		"strategy",
		fmt.Sprintf("Load balancing strategy '%s' failed", strategy),
		cause,
	).WithMetadata("strategy", strategy)
}

// NewRateLimitError creates an error for rate limiting
func NewRateLimitError(clientIP string, limit int) *LoadBalancerError {
	return NewError(
		ErrCodeRateLimitExceeded,
		"rate_limiter",
		fmt.Sprintf("Rate limit exceeded for client %s (limit: %d)", clientIP, limit),
	).WithMetadata("client_ip", clientIP).WithMetadata("limit", limit)
}

// NewAuthenticationError creates an authentication error
func NewAuthenticationError(reason string) *LoadBalancerError {
	return NewError(
		ErrCodeAuthenticationFailed,
		"auth",
		fmt.Sprintf("Authentication failed: %s", reason),
	).WithMetadata("reason", reason)
}

// NewCircuitBreakerError creates a circuit breaker error
func NewCircuitBreakerError(backendID string) *LoadBalancerError {
	return NewError(
		ErrCodeCircuitBreakerOpen,
		"circuit_breaker",
		fmt.Sprintf("Circuit breaker is open for backend %s", backendID),
	).WithMetadata("backend_id", backendID)
}

// Helper functions

// getStackTrace captures the current stack trace
func getStackTrace() string {
	buf := make([]byte, 1024)
	for {
		n := runtime.Stack(buf, false)
		if n < len(buf) {
			return string(buf[:n])
		}
		buf = make([]byte, 2*len(buf))
	}
}

// IsLoadBalancerError checks if an error is a LoadBalancerError
func IsLoadBalancerError(err error) bool {
	var lbErr *LoadBalancerError
	return errors.As(err, &lbErr)
}

// GetErrorCode extracts the error code from an error
func GetErrorCode(err error) ErrorCode {
	var lbErr *LoadBalancerError
	if errors.As(err, &lbErr) {
		return lbErr.Code
	}
	return ErrCodeInternalError
}

// IsRetryable checks if an error is retryable
func IsRetryable(err error) bool {
	var lbErr *LoadBalancerError
	if errors.As(err, &lbErr) {
		return lbErr.IsRetryable()
	}
	return false
}

// GetHTTPStatusCode gets the appropriate HTTP status code for an error
func GetHTTPStatusCode(err error) int {
	var lbErr *LoadBalancerError
	if errors.As(err, &lbErr) {
		return lbErr.HTTPStatusCode()
	}
	return 500
}
