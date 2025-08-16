// Package ports defines the interfaces for the Clean Architecture implementation.
// This package contains all the interfaces that define the contracts between
// different layers of the application (domain, application, infrastructure).
//
// Following Hexagonal Architecture principles, these interfaces define the
// "ports" through which our application communicates with external systems
// and internal components.
package ports

import (
	"context"
	"net/http"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
)

// ========================================
// DOMAIN LAYER PORTS (Primary/Driving)
// ========================================

// LoadBalancerService defines the core business logic interface for load balancing operations.
// This is a primary port that defines what our application can do.
type LoadBalancerService interface {
	// SelectBackend selects an appropriate backend for the given request context
	SelectBackend(ctx context.Context, request *LoadBalancingRequest) (*domain.Backend, error)

	// GetAvailableBackends returns all healthy backends
	GetAvailableBackends(ctx context.Context) ([]*domain.Backend, error)

	// GetBackendStats returns statistics for a specific backend
	GetBackendStats(ctx context.Context, backendID string) (*BackendStats, error)

	// UpdateBackendHealth updates the health status of a backend
	UpdateBackendHealth(ctx context.Context, backendID string, isHealthy bool) error
}

// RoutingService defines the interface for request routing operations.
// This handles the logic of determining where requests should be routed.
type RoutingService interface {
	// RouteRequest determines the routing target for a given request
	RouteRequest(ctx context.Context, request *http.Request) (*RoutingResult, error)

	// AddRoute adds a new routing rule
	AddRoute(ctx context.Context, rule *RoutingRule) error

	// RemoveRoute removes a routing rule
	RemoveRoute(ctx context.Context, ruleID string) error

	// GetActiveRoutes returns all active routing rules
	GetActiveRoutes(ctx context.Context) ([]*RoutingRule, error)
}

// TrafficService defines the interface for traffic management operations.
// This handles rate limiting, circuit breaking, and other traffic policies.
type TrafficService interface {
	// ProcessRequest processes a request through traffic management policies
	ProcessRequest(ctx context.Context, request *TrafficRequest) (*TrafficResponse, error)

	// ApplyRateLimit applies rate limiting to a request
	ApplyRateLimit(ctx context.Context, request *TrafficRequest) error

	// CheckCircuitBreaker checks if circuit breaker allows the request
	CheckCircuitBreaker(ctx context.Context, backendID string) error

	// RecordRequestResult records the result of a request for traffic management
	RecordRequestResult(ctx context.Context, result *RequestResult) error
}

// ObservabilityService defines the interface for monitoring and observability.
// This handles metrics, tracing, and logging operations.
type ObservabilityService interface {
	// StartTrace starts a new trace for request tracking
	StartTrace(ctx context.Context, operationName string) (TraceContext, error)

	// FinishTrace completes a trace with the given result
	FinishTrace(ctx context.Context, trace TraceContext, result *TraceResult) error

	// RecordMetric records a metric value
	RecordMetric(ctx context.Context, metric *Metric) error

	// GetMetrics retrieves current metrics
	GetMetrics(ctx context.Context, filter *MetricFilter) (*MetricsSnapshot, error)
}

// ========================================
// INFRASTRUCTURE LAYER PORTS (Secondary/Driven)
// ========================================

// BackendRepository defines the interface for backend persistence operations.
// This is a secondary port for data persistence.
type BackendRepository interface {
	// Save persists a backend configuration
	Save(ctx context.Context, backend *domain.Backend) error

	// FindByID retrieves a backend by its ID
	FindByID(ctx context.Context, id string) (*domain.Backend, error)

	// FindAll retrieves all backends
	FindAll(ctx context.Context) ([]*domain.Backend, error)

	// Update updates an existing backend
	Update(ctx context.Context, backend *domain.Backend) error

	// Delete removes a backend
	Delete(ctx context.Context, id string) error

	// FindHealthy retrieves all healthy backends
	FindHealthy(ctx context.Context) ([]*domain.Backend, error)
}

// HealthChecker defines the interface for health checking operations.
// This is a secondary port for external health checks.
type HealthChecker interface {
	// CheckHealth performs a health check on a backend
	CheckHealth(ctx context.Context, backend *domain.Backend) (*HealthCheckResult, error)

	// StartPeriodicChecks starts periodic health checks for all backends
	StartPeriodicChecks(ctx context.Context, interval time.Duration) error

	// StopPeriodicChecks stops all periodic health checks
	StopPeriodicChecks(ctx context.Context) error

	// SetHealthCheckPolicy sets the health check policy
	SetHealthCheckPolicy(ctx context.Context, policy *HealthCheckPolicy) error
}

// MetricsRepository defines the interface for metrics persistence.
// This is a secondary port for metrics storage.
type MetricsRepository interface {
	// Store stores metric data
	Store(ctx context.Context, metrics []*Metric) error

	// Query retrieves metrics based on a query
	Query(ctx context.Context, query *MetricQuery) (*MetricsSnapshot, error)

	// Aggregate performs aggregation on metrics
	Aggregate(ctx context.Context, aggregation *MetricAggregation) (*AggregationResult, error)
}

// ConfigurationRepository defines the interface for configuration management.
// This is a secondary port for configuration persistence.
type ConfigurationRepository interface {
	// LoadConfiguration loads the application configuration
	LoadConfiguration(ctx context.Context) (*Configuration, error)

	// SaveConfiguration saves the application configuration
	SaveConfiguration(ctx context.Context, config *Configuration) error

	// WatchConfiguration watches for configuration changes
	WatchConfiguration(ctx context.Context) (<-chan *ConfigurationEvent, error)
}

// EventPublisher defines the interface for event publishing.
// This is a secondary port for event-driven communication.
type EventPublisher interface {
	// Publish publishes an event
	Publish(ctx context.Context, event *Event) error

	// Subscribe subscribes to events of a specific type
	Subscribe(ctx context.Context, eventType string, handler EventHandler) error

	// Unsubscribe unsubscribes from events
	Unsubscribe(ctx context.Context, eventType string, handlerID string) error
}

// ========================================
// STRATEGY INTERFACES (Strategy Pattern)
// ========================================

// LoadBalancingStrategy defines the interface for load balancing algorithms.
// This follows the Strategy pattern for different load balancing algorithms.
type LoadBalancingStrategy interface {
	// SelectBackend selects a backend using the specific algorithm
	SelectBackend(ctx context.Context, request *LoadBalancingRequest, backends []*domain.Backend) (*domain.Backend, error)

	// Name returns the name of the strategy
	Name() string

	// Type returns the type of the strategy
	Type() StrategyType

	// Reset resets the strategy state
	Reset() error

	// GetStats returns strategy-specific statistics
	GetStats() map[string]interface{}
}

// StrategyFactory creates load balancing strategies.
// This follows the Factory pattern for strategy creation.
type StrategyFactory interface {
	// CreateStrategy creates a strategy of the specified type
	CreateStrategy(strategyType StrategyType, config *StrategyConfig) (LoadBalancingStrategy, error)

	// GetAvailableStrategies returns all available strategy types
	GetAvailableStrategies() []StrategyType

	// ValidateConfig validates a strategy configuration
	ValidateConfig(strategyType StrategyType, config *StrategyConfig) error
}

// ========================================
// DATA TRANSFER OBJECTS
// ========================================

// LoadBalancingRequest represents a request for load balancing.
type LoadBalancingRequest struct {
	RequestID string            `json:"request_id"`
	ClientIP  string            `json:"client_ip"`
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Headers   map[string]string `json:"headers"`
	Protocol  string            `json:"protocol"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

// RoutingResult represents the result of a routing decision.
type RoutingResult struct {
	TargetBackend *domain.Backend `json:"target_backend"`
	RoutingRule   *RoutingRule    `json:"routing_rule"`
	Metadata      map[string]any  `json:"metadata,omitempty"`
}

// RoutingRule represents a routing rule configuration.
type RoutingRule struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Priority   int                 `json:"priority"`
	Conditions []*RoutingCondition `json:"conditions"`
	Actions    []*RoutingAction    `json:"actions"`
	Enabled    bool                `json:"enabled"`
	Metadata   map[string]any      `json:"metadata,omitempty"`
}

// RoutingCondition represents a condition for routing.
type RoutingCondition struct {
	Type     ConditionType `json:"type"`
	Field    string        `json:"field"`
	Operator string        `json:"operator"`
	Value    string        `json:"value"`
	Negate   bool          `json:"negate,omitempty"`
}

// RoutingAction represents an action to take when routing conditions match.
type RoutingAction struct {
	Type       ActionType     `json:"type"`
	Target     string         `json:"target"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// TrafficRequest represents a request for traffic management.
type TrafficRequest struct {
	Request       *LoadBalancingRequest `json:"request"`
	TargetBackend *domain.Backend       `json:"target_backend"`
	Policy        *TrafficPolicy        `json:"policy,omitempty"`
}

// TrafficResponse represents the response from traffic management.
type TrafficResponse struct {
	Allowed    bool           `json:"allowed"`
	Reason     string         `json:"reason,omitempty"`
	RetryAfter *time.Duration `json:"retry_after,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// TrafficPolicy represents traffic management policies.
type TrafficPolicy struct {
	RateLimit      *RateLimitPolicy      `json:"rate_limit,omitempty"`
	CircuitBreaker *CircuitBreakerPolicy `json:"circuit_breaker,omitempty"`
	Retry          *RetryPolicy          `json:"retry,omitempty"`
	Timeout        *TimeoutPolicy        `json:"timeout,omitempty"`
}

// BackendStats represents statistics for a backend.
type BackendStats struct {
	BackendID         string        `json:"backend_id"`
	RequestCount      int64         `json:"request_count"`
	SuccessCount      int64         `json:"success_count"`
	ErrorCount        int64         `json:"error_count"`
	AverageLatency    time.Duration `json:"average_latency"`
	ActiveConnections int           `json:"active_connections"`
	LastHealthCheck   time.Time     `json:"last_health_check"`
	IsHealthy         bool          `json:"is_healthy"`
}

// HealthCheckResult represents the result of a health check.
type HealthCheckResult struct {
	BackendID  string        `json:"backend_id"`
	IsHealthy  bool          `json:"is_healthy"`
	Latency    time.Duration `json:"latency"`
	StatusCode int           `json:"status_code,omitempty"`
	Error      string        `json:"error,omitempty"`
	Timestamp  time.Time     `json:"timestamp"`
}

// HealthCheckPolicy defines health check configuration.
type HealthCheckPolicy struct {
	Interval           time.Duration `json:"interval"`
	Timeout            time.Duration `json:"timeout"`
	HealthyThreshold   int           `json:"healthy_threshold"`
	UnhealthyThreshold int           `json:"unhealthy_threshold"`
	Path               string        `json:"path"`
	Method             string        `json:"method"`
	ExpectedStatus     []int         `json:"expected_status"`
}

// ========================================
// ENUMS AND TYPES
// ========================================

// StrategyType represents the type of load balancing strategy.
type StrategyType string

const (
	RoundRobinStrategy         StrategyType = "round_robin"
	WeightedRoundRobinStrategy StrategyType = "weighted_round_robin"
	LeastConnectionsStrategy   StrategyType = "least_connections"
	IPHashStrategy             StrategyType = "ip_hash"
	ConsistentHashStrategy     StrategyType = "consistent_hash"
	ResourceBasedStrategy      StrategyType = "resource_based"
)

// ConditionType represents the type of routing condition.
type ConditionType string

const (
	PathCondition   ConditionType = "path"
	HeaderCondition ConditionType = "header"
	MethodCondition ConditionType = "method"
	HostCondition   ConditionType = "host"
	QueryCondition  ConditionType = "query"
)

// ActionType represents the type of routing action.
type ActionType string

const (
	RouteAction    ActionType = "route"
	RedirectAction ActionType = "redirect"
	RewriteAction  ActionType = "rewrite"
	RejectAction   ActionType = "reject"
)

// ========================================
// ADDITIONAL INTERFACES
// ========================================

// EventHandler handles published events.
type EventHandler interface {
	Handle(ctx context.Context, event *Event) error
	ID() string
}

// Event represents a domain event.
type Event struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Source    string         `json:"source"`
	Data      map[string]any `json:"data"`
	Timestamp time.Time      `json:"timestamp"`
	Version   string         `json:"version"`
}

// ConfigurationEvent represents a configuration change event.
type ConfigurationEvent struct {
	Type          string                 `json:"type"`
	Configuration *Configuration         `json:"configuration"`
	Changes       map[string]interface{} `json:"changes"`
	Timestamp     time.Time              `json:"timestamp"`
}

// Configuration represents the application configuration.
type Configuration struct {
	Backends      []*BackendConfig     `json:"backends"`
	LoadBalancing *LoadBalancingConfig `json:"load_balancing"`
	Traffic       *TrafficConfig       `json:"traffic"`
	Observability *ObservabilityConfig `json:"observability"`
	Metadata      map[string]any       `json:"metadata,omitempty"`
}

// Generic configuration types
type (
	BackendConfig        map[string]interface{}
	LoadBalancingConfig  map[string]interface{}
	TrafficConfig        map[string]interface{}
	ObservabilityConfig  map[string]interface{}
	StrategyConfig       map[string]interface{}
	RateLimitPolicy      map[string]interface{}
	CircuitBreakerPolicy map[string]interface{}
	RetryPolicy          map[string]interface{}
	TimeoutPolicy        map[string]interface{}
)

// Additional types for completeness
type (
	TraceContext      interface{}
	TraceResult       map[string]interface{}
	Metric            map[string]interface{}
	MetricFilter      map[string]interface{}
	MetricsSnapshot   map[string]interface{}
	MetricQuery       map[string]interface{}
	MetricAggregation map[string]interface{}
	AggregationResult map[string]interface{}
	RequestResult     map[string]interface{}
)
