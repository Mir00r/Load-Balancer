// Package stubs provides stub implementations for services that are not yet fully implemented.
// These stubs allow the Clean Architecture demonstration to work while maintaining
// the proper interfaces and dependency injection patterns.
package stubs

import (
	"context"
	"net/http"
	"time"

	"github.com/mir00r/load-balancer/internal/ports"
)

// ========================================
// ROUTING SERVICE STUB
// ========================================

// StubRoutingService provides a minimal implementation of RoutingService.
type StubRoutingService struct{}

// NewStubRoutingService creates a new stub routing service.
func NewStubRoutingService() ports.RoutingService {
	return &StubRoutingService{}
}

// RouteRequest returns a default routing result.
func (s *StubRoutingService) RouteRequest(ctx context.Context, request *http.Request) (*ports.RoutingResult, error) {
	return &ports.RoutingResult{
		RoutingRule: &ports.RoutingRule{
			ID:       "default",
			Name:     "default-route",
			Priority: 1,
			Enabled:  true,
		},
		Metadata: map[string]any{
			"stub": true,
			"path": request.URL.Path,
		},
	}, nil
}

// AddRoute adds a routing rule (stub implementation).
func (s *StubRoutingService) AddRoute(ctx context.Context, rule *ports.RoutingRule) error {
	// Stub implementation - just return success
	return nil
}

// RemoveRoute removes a routing rule (stub implementation).
func (s *StubRoutingService) RemoveRoute(ctx context.Context, ruleID string) error {
	// Stub implementation - just return success
	return nil
}

// GetActiveRoutes returns empty active routes (stub implementation).
func (s *StubRoutingService) GetActiveRoutes(ctx context.Context) ([]*ports.RoutingRule, error) {
	// Return a default route
	return []*ports.RoutingRule{
		{
			ID:       "default",
			Name:     "default-route",
			Priority: 1,
			Enabled:  true,
		},
	}, nil
}

// ========================================
// TRAFFIC SERVICE STUB
// ========================================

// StubTrafficService provides a minimal implementation of TrafficService.
type StubTrafficService struct{}

// NewStubTrafficService creates a new stub traffic service.
func NewStubTrafficService() ports.TrafficService {
	return &StubTrafficService{}
}

// ProcessRequest allows all requests (stub implementation).
func (s *StubTrafficService) ProcessRequest(ctx context.Context, request *ports.TrafficRequest) (*ports.TrafficResponse, error) {
	return &ports.TrafficResponse{
		Allowed: true,
		Reason:  "allowed by stub",
		Metadata: map[string]any{
			"stub": true,
		},
	}, nil
}

// ApplyRateLimit applies rate limiting (stub implementation).
func (s *StubTrafficService) ApplyRateLimit(ctx context.Context, request *ports.TrafficRequest) error {
	// Stub implementation - just return success (no rate limiting)
	return nil
}

// CheckCircuitBreaker checks circuit breaker (stub implementation).
func (s *StubTrafficService) CheckCircuitBreaker(ctx context.Context, backendID string) error {
	// Stub implementation - circuit breaker always allows
	return nil
}

// RecordRequestResult records request result (stub implementation).
func (s *StubTrafficService) RecordRequestResult(ctx context.Context, result *ports.RequestResult) error {
	// Stub implementation - just return success
	return nil
}

// UpdatePolicy updates a traffic policy (stub implementation).
func (s *StubTrafficService) UpdatePolicy(ctx context.Context, policy *ports.TrafficPolicy) error {
	// Stub implementation - just return success
	return nil
}

// GetPolicies returns empty policies (stub implementation).
func (s *StubTrafficService) GetPolicies(ctx context.Context) ([]*ports.TrafficPolicy, error) {
	return []*ports.TrafficPolicy{}, nil
}

// ========================================
// OBSERVABILITY SERVICE STUB
// ========================================

// StubObservabilityService provides a minimal implementation of ObservabilityService.
type StubObservabilityService struct{}

// NewStubObservabilityService creates a new stub observability service.
func NewStubObservabilityService() ports.ObservabilityService {
	return &StubObservabilityService{}
}

// StartTrace starts a trace (stub implementation).
func (s *StubObservabilityService) StartTrace(ctx context.Context, operation string) (ports.TraceContext, error) {
	return ports.TraceContext("stub-trace-" + operation), nil
}

// FinishTrace finishes a trace (stub implementation).
func (s *StubObservabilityService) FinishTrace(ctx context.Context, trace ports.TraceContext, result *ports.TraceResult) error {
	// Stub implementation - just return success
	return nil
}

// RecordMetric records a metric (stub implementation).
func (s *StubObservabilityService) RecordMetric(ctx context.Context, metric *ports.Metric) error {
	// Stub implementation - just return success
	return nil
}

// GetMetrics retrieves metrics (stub implementation).
func (s *StubObservabilityService) GetMetrics(ctx context.Context, filter *ports.MetricFilter) (*ports.MetricsSnapshot, error) {
	return &ports.MetricsSnapshot{
		"stub":      true,
		"timestamp": time.Now(),
	}, nil
}
