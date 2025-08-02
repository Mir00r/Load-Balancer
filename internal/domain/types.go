package domain

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"
)

// BackendStatus represents the health status of a backend server
type BackendStatus int

const (
	// StatusHealthy indicates the backend is healthy and available
	StatusHealthy BackendStatus = iota
	// StatusUnhealthy indicates the backend is unhealthy and should not receive traffic
	StatusUnhealthy
	// StatusMaintenance indicates the backend is in maintenance mode
	StatusMaintenance
)

// String returns the string representation of BackendStatus
func (s BackendStatus) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusUnhealthy:
		return "unhealthy"
	case StatusMaintenance:
		return "maintenance"
	default:
		return "unknown"
	}
}

// Backend represents a backend server with its configuration and runtime state
type Backend struct {
	ID              string        `json:"id" yaml:"id"`
	URL             string        `json:"url" yaml:"url"`
	Weight          int           `json:"weight" yaml:"weight"`
	HealthCheckPath string        `json:"health_check_path" yaml:"health_check_path"`
	MaxConnections  int           `json:"max_connections" yaml:"max_connections"`
	Timeout         time.Duration `json:"timeout" yaml:"timeout"`

	// Runtime state - thread-safe using atomic operations
	activeConnections int64
	totalRequests     int64
	failureCount      int64
	lastFailure       time.Time
	status            BackendStatus
	lastHealthCheck   time.Time
}

// NewBackend creates a new Backend instance with default values
func NewBackend(id, url string, weight int) *Backend {
	return &Backend{
		ID:              id,
		URL:             url,
		Weight:          weight,
		HealthCheckPath: "/health",
		MaxConnections:  100,
		Timeout:         30 * time.Second,
		status:          StatusHealthy,
	}
}

// IncrementConnections atomically increments the active connection count
func (b *Backend) IncrementConnections() {
	atomic.AddInt64(&b.activeConnections, 1)
}

// DecrementConnections atomically decrements the active connection count
func (b *Backend) DecrementConnections() {
	atomic.AddInt64(&b.activeConnections, -1)
}

// GetActiveConnections returns the current number of active connections
func (b *Backend) GetActiveConnections() int64 {
	return atomic.LoadInt64(&b.activeConnections)
}

// IncrementRequests atomically increments the total request count
func (b *Backend) IncrementRequests() {
	atomic.AddInt64(&b.totalRequests, 1)
}

// GetTotalRequests returns the total number of requests processed
func (b *Backend) GetTotalRequests() int64 {
	return atomic.LoadInt64(&b.totalRequests)
}

// IncrementFailures atomically increments the failure count
func (b *Backend) IncrementFailures() {
	atomic.AddInt64(&b.failureCount, 1)
	b.lastFailure = time.Now()
}

// GetFailureCount returns the current failure count
func (b *Backend) GetFailureCount() int64 {
	return atomic.LoadInt64(&b.failureCount)
}

// ResetFailures resets the failure count to zero
func (b *Backend) ResetFailures() {
	atomic.StoreInt64(&b.failureCount, 0)
}

// SetStatus updates the backend status
func (b *Backend) SetStatus(status BackendStatus) {
	b.status = status
}

// GetStatus returns the current backend status
func (b *Backend) GetStatus() BackendStatus {
	return b.status
}

// IsHealthy returns true if the backend is healthy
func (b *Backend) IsHealthy() bool {
	return b.status == StatusHealthy
}

// IsAvailable returns true if the backend can accept new connections
func (b *Backend) IsAvailable() bool {
	if !b.IsHealthy() {
		return false
	}
	if b.MaxConnections > 0 && b.GetActiveConnections() >= int64(b.MaxConnections) {
		return false
	}
	return true
}

// UpdateLastHealthCheck updates the timestamp of the last health check
func (b *Backend) UpdateLastHealthCheck() {
	b.lastHealthCheck = time.Now()
}

// GetLastHealthCheck returns the timestamp of the last health check
func (b *Backend) GetLastHealthCheck() time.Time {
	return b.lastHealthCheck
}

// LoadBalancingStrategy defines the strategy for selecting backends
type LoadBalancingStrategy string

const (
	// RoundRobinStrategy distributes requests evenly across backends
	RoundRobinStrategy LoadBalancingStrategy = "round_robin"
	// WeightedRoundRobinStrategy considers backend weights for distribution
	WeightedRoundRobinStrategy LoadBalancingStrategy = "weighted_round_robin"
	// LeastConnectionsStrategy routes to backend with fewest active connections
	LeastConnectionsStrategy LoadBalancingStrategy = "least_connections"
)

// HealthCheckConfig defines configuration for health checking
type HealthCheckConfig struct {
	Enabled            bool          `json:"enabled" yaml:"enabled"`
	Interval           time.Duration `json:"interval" yaml:"interval"`
	Timeout            time.Duration `json:"timeout" yaml:"timeout"`
	HealthyThreshold   int           `json:"healthy_threshold" yaml:"healthy_threshold"`
	UnhealthyThreshold int           `json:"unhealthy_threshold" yaml:"unhealthy_threshold"`
	Path               string        `json:"path" yaml:"path"`
}

// LoadBalancerConfig defines the configuration for the load balancer
type LoadBalancerConfig struct {
	Strategy       LoadBalancingStrategy `json:"strategy" yaml:"strategy"`
	Port           int                   `json:"port" yaml:"port"`
	MaxRetries     int                   `json:"max_retries" yaml:"max_retries"`
	RetryDelay     time.Duration         `json:"retry_delay" yaml:"retry_delay"`
	Timeout        time.Duration         `json:"timeout" yaml:"timeout"`
	HealthCheck    HealthCheckConfig     `json:"health_check" yaml:"health_check"`
	RateLimit      RateLimitConfig       `json:"rate_limit" yaml:"rate_limit"`
	CircuitBreaker CircuitBreakerConfig  `json:"circuit_breaker" yaml:"circuit_breaker"`
}

// RateLimitConfig defines configuration for rate limiting
type RateLimitConfig struct {
	Enabled           bool    `json:"enabled" yaml:"enabled"`
	RequestsPerSecond float64 `json:"requests_per_second" yaml:"requests_per_second"`
	BurstSize         int     `json:"burst_size" yaml:"burst_size"`
}

// CircuitBreakerConfig defines configuration for circuit breaker
type CircuitBreakerConfig struct {
	Enabled          bool          `json:"enabled" yaml:"enabled"`
	FailureThreshold int           `json:"failure_threshold" yaml:"failure_threshold"`
	RecoveryTimeout  time.Duration `json:"recovery_timeout" yaml:"recovery_timeout"`
	MaxRequests      int           `json:"max_requests" yaml:"max_requests"`
}

// LoadBalancer defines the interface for load balancer implementations
type LoadBalancer interface {
	// GetBackend selects the next available backend based on the configured strategy
	GetBackend(ctx context.Context) (*Backend, error)
	// AddBackend adds a new backend to the pool
	AddBackend(backend *Backend) error
	// RemoveBackend removes a backend from the pool
	RemoveBackend(id string) error
	// GetBackends returns all backends
	GetBackends() []*Backend
	// GetHealthyBackends returns only healthy backends
	GetHealthyBackends() []*Backend
	// Start starts the load balancer and health checking
	Start(ctx context.Context) error
	// Stop gracefully stops the load balancer
	Stop(ctx context.Context) error
}

// BackendRepository defines the interface for backend data management
type BackendRepository interface {
	// GetAll returns all backends
	GetAll() ([]*Backend, error)
	// GetByID returns a backend by its ID
	GetByID(id string) (*Backend, error)
	// Save persists a backend
	Save(backend *Backend) error
	// Delete removes a backend
	Delete(id string) error
	// GetHealthy returns only healthy backends
	GetHealthy() ([]*Backend, error)
}

// HealthChecker defines the interface for health checking
type HealthChecker interface {
	// Check performs a health check on a backend
	Check(ctx context.Context, backend *Backend) error
	// StartChecking starts periodic health checking
	StartChecking(ctx context.Context, backends []*Backend) error
	// StopChecking stops health checking
	StopChecking() error
}

// Metrics defines the interface for collecting and reporting metrics
type Metrics interface {
	// IncrementRequests increments the total request count
	IncrementRequests(backendID string)
	// IncrementErrors increments the error count
	IncrementErrors(backendID string)
	// RecordLatency records request latency
	RecordLatency(backendID string, duration time.Duration)
	// GetStats returns current statistics
	GetStats() map[string]interface{}
	// GetBackendStats returns statistics for a specific backend
	GetBackendStats(backendID string) map[string]interface{}
}

// RequestContext contains request-specific information
type RequestContext struct {
	RequestID  string
	RemoteAddr string
	UserAgent  string
	Method     string
	Path       string
	StartTime  time.Time
	BackendID  string
	Retries    int
}

// NewRequestContext creates a new RequestContext from an HTTP request
func NewRequestContext(r *http.Request) *RequestContext {
	return &RequestContext{
		RequestID:  generateRequestID(),
		RemoteAddr: r.RemoteAddr,
		UserAgent:  r.UserAgent(),
		Method:     r.Method,
		Path:       r.URL.Path,
		StartTime:  time.Now(),
	}
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	// Simple implementation - in production, use UUID or similar
	return time.Now().Format("20060102150405.000")
}
