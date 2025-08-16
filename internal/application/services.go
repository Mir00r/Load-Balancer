// Package application contains the application layer of the Clean Architecture.
// This layer orchestrates the domain logic and coordinates between different
// domain services. It implements use cases and application-specific business logic.
//
// The application layer:
// - Orchestrates domain objects to perform application-specific tasks
// - Coordinates between multiple domain services
// - Handles transaction boundaries
// - Implements use cases and workflows
// - Does not contain business rules (those belong in the domain)
package application

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/ports"
)

// ========================================
// USE CASE INTERFACES
// ========================================

// LoadBalancingUseCase defines the interface for load balancing use cases.
// This interface represents the primary application workflows for load balancing.
type LoadBalancingUseCase interface {
	// ProcessRequest processes an incoming request through the entire load balancing pipeline
	ProcessRequest(ctx context.Context, request *ports.LoadBalancingRequest) (*LoadBalancingResult, error)

	// GetBackendStatus retrieves the current status of all backends
	GetBackendStatus(ctx context.Context) (*BackendStatusReport, error)

	// UpdateBackendConfiguration updates backend configuration
	UpdateBackendConfiguration(ctx context.Context, config *BackendConfigUpdate) error

	// GetLoadBalancingMetrics retrieves load balancing metrics
	GetLoadBalancingMetrics(ctx context.Context, filter *MetricsFilter) (*LoadBalancingMetrics, error)
}

// RoutingUseCase defines the interface for routing use cases.
type RoutingUseCase interface {
	// CreateRoutingRule creates a new routing rule
	CreateRoutingRule(ctx context.Context, rule *ports.RoutingRule) error

	// UpdateRoutingRule updates an existing routing rule
	UpdateRoutingRule(ctx context.Context, ruleID string, updates *RoutingRuleUpdate) error

	// DeleteRoutingRule deletes a routing rule
	DeleteRoutingRule(ctx context.Context, ruleID string) error

	// GetRoutingConfiguration retrieves current routing configuration
	GetRoutingConfiguration(ctx context.Context) (*RoutingConfiguration, error)

	// ValidateRoutingConfiguration validates a routing configuration
	ValidateRoutingConfiguration(ctx context.Context, config *RoutingConfiguration) (*ValidationResult, error)
}

// TrafficManagementUseCase defines the interface for traffic management use cases.
type TrafficManagementUseCase interface {
	// ApplyTrafficPolicies applies traffic management policies to a request
	ApplyTrafficPolicies(ctx context.Context, request *ports.TrafficRequest) (*TrafficDecision, error)

	// UpdateTrafficPolicy updates a traffic management policy
	UpdateTrafficPolicy(ctx context.Context, policyID string, policy *ports.TrafficPolicy) error

	// GetTrafficMetrics retrieves traffic management metrics
	GetTrafficMetrics(ctx context.Context, filter *MetricsFilter) (*TrafficMetrics, error)

	// GetCircuitBreakerStatus retrieves circuit breaker status for all backends
	GetCircuitBreakerStatus(ctx context.Context) (*CircuitBreakerStatusReport, error)
}

// HealthManagementUseCase defines the interface for health management use cases.
type HealthManagementUseCase interface {
	// StartHealthChecks starts health checking for all backends
	StartHealthChecks(ctx context.Context) error

	// StopHealthChecks stops health checking
	StopHealthChecks(ctx context.Context) error

	// TriggerHealthCheck triggers an immediate health check for a specific backend
	TriggerHealthCheck(ctx context.Context, backendID string) (*ports.HealthCheckResult, error)

	// UpdateHealthCheckPolicy updates the health check policy
	UpdateHealthCheckPolicy(ctx context.Context, policy *ports.HealthCheckPolicy) error

	// GetHealthReport generates a comprehensive health report
	GetHealthReport(ctx context.Context) (*HealthReport, error)
}

// ========================================
// USE CASE IMPLEMENTATIONS
// ========================================

// LoadBalancingService implements LoadBalancingUseCase.
// This service orchestrates the entire load balancing workflow.
type LoadBalancingService struct {
	// Dependencies (injected via constructor)
	backendRepo     ports.BackendRepository
	routingService  ports.RoutingService
	trafficService  ports.TrafficService
	observability   ports.ObservabilityService
	strategyFactory ports.StrategyFactory
	eventPublisher  ports.EventPublisher

	// Configuration
	config *LoadBalancingConfig
}

// NewLoadBalancingService creates a new load balancing service.
// This constructor follows Dependency Injection principles.
func NewLoadBalancingService(
	backendRepo ports.BackendRepository,
	routingService ports.RoutingService,
	trafficService ports.TrafficService,
	observability ports.ObservabilityService,
	strategyFactory ports.StrategyFactory,
	eventPublisher ports.EventPublisher,
	config *LoadBalancingConfig,
) LoadBalancingUseCase {
	return &LoadBalancingService{
		backendRepo:     backendRepo,
		routingService:  routingService,
		trafficService:  trafficService,
		observability:   observability,
		strategyFactory: strategyFactory,
		eventPublisher:  eventPublisher,
		config:          config,
	}
}

// ProcessRequest implements the main load balancing workflow.
// This method orchestrates the entire request processing pipeline.
func (s *LoadBalancingService) ProcessRequest(ctx context.Context, request *ports.LoadBalancingRequest) (*LoadBalancingResult, error) {
	// Start tracing for observability
	trace, err := s.observability.StartTrace(ctx, "load_balancing.process_request")
	if err != nil {
		return nil, fmt.Errorf("failed to start trace: %w", err)
	}
	defer func() {
		_ = s.observability.FinishTrace(ctx, trace, &ports.TraceResult{
			"request_id": request.RequestID,
			"timestamp":  time.Now(),
		})
	}()

	// Step 1: Route the request to determine target criteria
	routingResult, err := s.routingService.RouteRequest(ctx, &http.Request{
		Method: request.Method,
		URL:    &url.URL{Path: request.Path},
		Header: convertToHTTPHeaders(request.Headers),
	})
	if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}

	// Step 2: Get available backends
	backends, err := s.backendRepo.FindHealthy(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy backends: %w", err)
	}

	if len(backends) == 0 {
		return nil, fmt.Errorf("no healthy backends available")
	}

	// Step 3: Apply load balancing strategy
	strategy, err := s.strategyFactory.CreateStrategy(
		ports.StrategyType(s.config.Strategy),
		s.config.StrategyConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create strategy: %w", err)
	}

	selectedBackend, err := strategy.SelectBackend(ctx, request, backends)
	if err != nil {
		return nil, fmt.Errorf("backend selection failed: %w", err)
	}

	// Step 4: Apply traffic management policies
	trafficRequest := &ports.TrafficRequest{
		Request:       request,
		TargetBackend: selectedBackend,
		Policy:        s.config.TrafficPolicy,
	}

	trafficResponse, err := s.trafficService.ProcessRequest(ctx, trafficRequest)
	if err != nil {
		return nil, fmt.Errorf("traffic management failed: %w", err)
	}

	if !trafficResponse.Allowed {
		return &LoadBalancingResult{
			Backend:  nil,
			Decision: "rejected",
			Reason:   trafficResponse.Reason,
			Metadata: trafficResponse.Metadata,
		}, nil
	}

	// Step 5: Record metrics
	err = s.observability.RecordMetric(ctx, &ports.Metric{
		"name":      "request_processed",
		"backend":   selectedBackend.ID,
		"strategy":  strategy.Name(),
		"timestamp": time.Now(),
	})
	if err != nil {
		// Log error but don't fail the request
	}

	// Step 6: Publish event
	event := &ports.Event{
		ID:     request.RequestID,
		Type:   "request_processed",
		Source: "load_balancer",
		Data: map[string]any{
			"backend_id": selectedBackend.ID,
			"strategy":   strategy.Name(),
			"routing":    routingResult,
			"client_ip":  request.ClientIP,
		},
		Timestamp: time.Now(),
	}

	_ = s.eventPublisher.Publish(ctx, event)

	return &LoadBalancingResult{
		Backend:     selectedBackend,
		Decision:    "accepted",
		Reason:      "request processed successfully",
		Strategy:    strategy.Name(),
		RoutingRule: routingResult.RoutingRule,
		Metadata: map[string]any{
			"processing_time": time.Since(request.Timestamp),
			"trace_id":        trace,
		},
	}, nil
}

// GetBackendStatus retrieves the current status of all backends.
func (s *LoadBalancingService) GetBackendStatus(ctx context.Context) (*BackendStatusReport, error) {
	backends, err := s.backendRepo.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get backends: %w", err)
	}

	var backendStatuses []*ports.BackendStats
	for _, backend := range backends {
		stats, err := s.getBackendStats(ctx, backend.ID)
		if err != nil {
			// Log error but continue with other backends
			continue
		}
		backendStatuses = append(backendStatuses, stats)
	}

	return &BackendStatusReport{
		Backends:  backendStatuses,
		Timestamp: time.Now(),
		Total:     len(backends),
		Healthy:   s.countHealthyBackends(backendStatuses),
	}, nil
}

// UpdateBackendConfiguration updates backend configuration.
func (s *LoadBalancingService) UpdateBackendConfiguration(ctx context.Context, config *BackendConfigUpdate) error {
	// Validate the configuration
	if err := s.validateBackendConfig(config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Get existing backend
	backend, err := s.backendRepo.FindByID(ctx, config.BackendID)
	if err != nil {
		return fmt.Errorf("backend not found: %w", err)
	}

	// Apply updates
	s.applyBackendUpdates(backend, config)

	// Save updated backend
	if err := s.backendRepo.Update(ctx, backend); err != nil {
		return fmt.Errorf("failed to update backend: %w", err)
	}

	// Publish configuration change event
	event := &ports.Event{
		Type:   "backend_configuration_updated",
		Source: "load_balancer",
		Data: map[string]any{
			"backend_id": config.BackendID,
			"changes":    config.Changes,
		},
		Timestamp: time.Now(),
	}

	_ = s.eventPublisher.Publish(ctx, event)

	return nil
}

// GetLoadBalancingMetrics retrieves load balancing metrics.
func (s *LoadBalancingService) GetLoadBalancingMetrics(ctx context.Context, filter *MetricsFilter) (*LoadBalancingMetrics, error) {
	metrics, err := s.observability.GetMetrics(ctx, &ports.MetricFilter{
		"component": "load_balancer",
		"from":      filter.From,
		"to":        filter.To,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}

	return &LoadBalancingMetrics{
		TotalRequests:      extractMetricValue(metrics, "total_requests"),
		SuccessfulRequests: extractMetricValue(metrics, "successful_requests"),
		FailedRequests:     extractMetricValue(metrics, "failed_requests"),
		AverageLatency:     extractDurationMetric(metrics, "average_latency"),
		Backends:           extractBackendMetrics(metrics),
		Timestamp:          time.Now(),
	}, nil
}

// ========================================
// ROUTING USE CASE IMPLEMENTATION
// ========================================

// RoutingService implements RoutingUseCase.
type RoutingService struct {
	routingService ports.RoutingService
	configRepo     ports.ConfigurationRepository
	eventPublisher ports.EventPublisher
	observability  ports.ObservabilityService
}

// NewRoutingService creates a new routing service.
func NewRoutingService(
	routingService ports.RoutingService,
	configRepo ports.ConfigurationRepository,
	eventPublisher ports.EventPublisher,
	observability ports.ObservabilityService,
) RoutingUseCase {
	return &RoutingService{
		routingService: routingService,
		configRepo:     configRepo,
		eventPublisher: eventPublisher,
		observability:  observability,
	}
}

// CreateRoutingRule creates a new routing rule.
func (s *RoutingService) CreateRoutingRule(ctx context.Context, rule *ports.RoutingRule) error {
	// Validate the rule
	if err := s.validateRoutingRule(rule); err != nil {
		return fmt.Errorf("invalid routing rule: %w", err)
	}

	// Add the rule
	if err := s.routingService.AddRoute(ctx, rule); err != nil {
		return fmt.Errorf("failed to add routing rule: %w", err)
	}

	// Publish event
	event := &ports.Event{
		Type:   "routing_rule_created",
		Source: "routing_service",
		Data: map[string]any{
			"rule_id": rule.ID,
			"rule":    rule,
		},
		Timestamp: time.Now(),
	}

	_ = s.eventPublisher.Publish(ctx, event)

	return nil
}

// UpdateRoutingRule updates an existing routing rule.
func (s *RoutingService) UpdateRoutingRule(ctx context.Context, ruleID string, updates *RoutingRuleUpdate) error {
	// Get existing rules to find the one to update
	rules, err := s.routingService.GetActiveRoutes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get routing rules: %w", err)
	}

	var existingRule *ports.RoutingRule
	for _, rule := range rules {
		if rule.ID == ruleID {
			existingRule = rule
			break
		}
	}

	if existingRule == nil {
		return fmt.Errorf("routing rule with ID %s not found", ruleID)
	}

	// Apply updates
	s.applyRoutingRuleUpdates(existingRule, updates)

	// Validate updated rule
	if err := s.validateRoutingRule(existingRule); err != nil {
		return fmt.Errorf("invalid updated routing rule: %w", err)
	}

	// Remove old rule and add updated one
	if err := s.routingService.RemoveRoute(ctx, ruleID); err != nil {
		return fmt.Errorf("failed to remove old routing rule: %w", err)
	}

	if err := s.routingService.AddRoute(ctx, existingRule); err != nil {
		return fmt.Errorf("failed to add updated routing rule: %w", err)
	}

	// Publish event
	event := &ports.Event{
		Type:   "routing_rule_updated",
		Source: "routing_service",
		Data: map[string]any{
			"rule_id": ruleID,
			"updates": updates,
		},
		Timestamp: time.Now(),
	}

	_ = s.eventPublisher.Publish(ctx, event)

	return nil
}

// DeleteRoutingRule deletes a routing rule.
func (s *RoutingService) DeleteRoutingRule(ctx context.Context, ruleID string) error {
	// Check if rule exists
	rules, err := s.routingService.GetActiveRoutes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get routing rules: %w", err)
	}

	found := false
	for _, rule := range rules {
		if rule.ID == ruleID {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("routing rule with ID %s not found", ruleID)
	}

	// Delete the rule
	if err := s.routingService.RemoveRoute(ctx, ruleID); err != nil {
		return fmt.Errorf("failed to delete routing rule: %w", err)
	}

	// Publish event
	event := &ports.Event{
		Type:   "routing_rule_deleted",
		Source: "routing_service",
		Data: map[string]any{
			"rule_id": ruleID,
		},
		Timestamp: time.Now(),
	}

	_ = s.eventPublisher.Publish(ctx, event)

	return nil
}

// GetRoutingConfiguration retrieves current routing configuration.
func (s *RoutingService) GetRoutingConfiguration(ctx context.Context) (*RoutingConfiguration, error) {
	rules, err := s.routingService.GetActiveRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get routing rules: %w", err)
	}

	return &RoutingConfiguration{
		Rules: rules,
	}, nil
}

// ValidateRoutingConfiguration validates a routing configuration.
func (s *RoutingService) ValidateRoutingConfiguration(ctx context.Context, config *RoutingConfiguration) (*ValidationResult, error) {
	var errors []string

	// Validate each rule
	for _, rule := range config.Rules {
		if err := s.validateRoutingRule(rule); err != nil {
			errors = append(errors, fmt.Sprintf("rule %s: %v", rule.ID, err))
		}
	}

	// Check for conflicting rules
	if conflictErrs := s.checkRuleConflicts(config.Rules); len(conflictErrs) > 0 {
		errors = append(errors, conflictErrs...)
	}

	return &ValidationResult{
		Valid:  len(errors) == 0,
		Errors: errors,
	}, nil
}

// ========================================
// DATA TRANSFER OBJECTS
// ========================================

// LoadBalancingResult represents the result of load balancing.
type LoadBalancingResult struct {
	Backend     *domain.Backend    `json:"backend"`
	Decision    string             `json:"decision"`
	Reason      string             `json:"reason"`
	Strategy    string             `json:"strategy"`
	RoutingRule *ports.RoutingRule `json:"routing_rule,omitempty"`
	Metadata    map[string]any     `json:"metadata,omitempty"`
}

// BackendStatusReport represents the status of all backends.
type BackendStatusReport struct {
	Backends  []*ports.BackendStats `json:"backends"`
	Timestamp time.Time             `json:"timestamp"`
	Total     int                   `json:"total"`
	Healthy   int                   `json:"healthy"`
}

// BackendConfigUpdate represents a backend configuration update.
type BackendConfigUpdate struct {
	BackendID string                 `json:"backend_id"`
	Changes   map[string]interface{} `json:"changes"`
}

// MetricsFilter represents a filter for metrics queries.
type MetricsFilter struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// LoadBalancingMetrics represents load balancing metrics.
type LoadBalancingMetrics struct {
	TotalRequests      int64                      `json:"total_requests"`
	SuccessfulRequests int64                      `json:"successful_requests"`
	FailedRequests     int64                      `json:"failed_requests"`
	AverageLatency     time.Duration              `json:"average_latency"`
	Backends           map[string]*BackendMetrics `json:"backends"`
	Timestamp          time.Time                  `json:"timestamp"`
}

// BackendMetrics represents metrics for a specific backend.
type BackendMetrics struct {
	RequestCount   int64         `json:"request_count"`
	SuccessRate    float64       `json:"success_rate"`
	AverageLatency time.Duration `json:"average_latency"`
	ErrorRate      float64       `json:"error_rate"`
}

// LoadBalancingConfig represents load balancing configuration.
type LoadBalancingConfig struct {
	Strategy       string                `json:"strategy"`
	StrategyConfig *ports.StrategyConfig `json:"strategy_config"`
	TrafficPolicy  *ports.TrafficPolicy  `json:"traffic_policy"`
}

// RoutingConfiguration represents routing configuration.
type RoutingConfiguration struct {
	Rules []*ports.RoutingRule `json:"rules"`
}

// RoutingRuleUpdate represents an update to a routing rule.
type RoutingRuleUpdate struct {
	Name       *string                   `json:"name,omitempty"`
	Priority   *int                      `json:"priority,omitempty"`
	Conditions []*ports.RoutingCondition `json:"conditions,omitempty"`
	Actions    []*ports.RoutingAction    `json:"actions,omitempty"`
	Enabled    *bool                     `json:"enabled,omitempty"`
	Metadata   map[string]any            `json:"metadata,omitempty"`
}

// ValidationResult represents the result of validation.
type ValidationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

// TrafficDecision represents a traffic management decision.
type TrafficDecision struct {
	Allowed    bool           `json:"allowed"`
	Reason     string         `json:"reason"`
	RetryAfter *time.Duration `json:"retry_after,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// TrafficMetrics represents traffic management metrics.
type TrafficMetrics struct {
	RateLimitedRequests int64     `json:"rate_limited_requests"`
	CircuitBreakerTrips int64     `json:"circuit_breaker_trips"`
	RetryAttempts       int64     `json:"retry_attempts"`
	Timestamp           time.Time `json:"timestamp"`
}

// CircuitBreakerStatusReport represents circuit breaker status.
type CircuitBreakerStatusReport struct {
	Backends  map[string]*CircuitBreakerStatus `json:"backends"`
	Timestamp time.Time                        `json:"timestamp"`
}

// CircuitBreakerStatus represents the status of a circuit breaker.
type CircuitBreakerStatus struct {
	State       string     `json:"state"`
	FailureRate float64    `json:"failure_rate"`
	LastTrip    *time.Time `json:"last_trip,omitempty"`
}

// HealthReport represents a comprehensive health report.
type HealthReport struct {
	Overall   HealthStatus              `json:"overall"`
	Backends  map[string]*BackendHealth `json:"backends"`
	Timestamp time.Time                 `json:"timestamp"`
}

// HealthStatus represents overall health status.
type HealthStatus struct {
	Status      string `json:"status"`
	Healthy     int    `json:"healthy"`
	Unhealthy   int    `json:"unhealthy"`
	Maintenance int    `json:"maintenance"`
}

// BackendHealth represents the health of a backend.
type BackendHealth struct {
	Status           string        `json:"status"`
	LastCheck        time.Time     `json:"last_check"`
	ResponseTime     time.Duration `json:"response_time"`
	ConsecutiveFails int           `json:"consecutive_fails"`
}

// ========================================
// HELPER METHODS
// ========================================

// Helper methods for the load balancing service
func (s *LoadBalancingService) getBackendStats(ctx context.Context, backendID string) (*ports.BackendStats, error) {
	// TODO: Implement backend stats retrieval
	return &ports.BackendStats{
		BackendID: backendID,
	}, nil
}

func (s *LoadBalancingService) countHealthyBackends(stats []*ports.BackendStats) int {
	count := 0
	for _, stat := range stats {
		if stat.IsHealthy {
			count++
		}
	}
	return count
}

func (s *LoadBalancingService) validateBackendConfig(config *BackendConfigUpdate) error {
	if config.BackendID == "" {
		return fmt.Errorf("backend ID is required")
	}
	return nil
}

func (s *LoadBalancingService) applyBackendUpdates(backend *domain.Backend, config *BackendConfigUpdate) {
	// Apply configuration changes to the backend
	for key, value := range config.Changes {
		switch key {
		case "weight":
			if weight, ok := value.(int); ok {
				backend.Weight = weight
			}
		case "max_connections":
			if maxConn, ok := value.(int); ok {
				backend.MaxConnections = maxConn
			}
			// Add more fields as needed
		}
	}
}

func (s *RoutingService) validateRoutingRule(rule *ports.RoutingRule) error {
	if rule.ID == "" {
		return fmt.Errorf("rule ID is required")
	}
	if rule.Name == "" {
		return fmt.Errorf("rule name is required")
	}
	if len(rule.Conditions) == 0 {
		return fmt.Errorf("at least one condition is required")
	}
	if len(rule.Actions) == 0 {
		return fmt.Errorf("at least one action is required")
	}
	return nil
}

// applyRoutingRuleUpdates applies updates to a routing rule
func (s *RoutingService) applyRoutingRuleUpdates(rule *ports.RoutingRule, updates *RoutingRuleUpdate) {
	if updates.Name != nil {
		rule.Name = *updates.Name
	}
	if updates.Priority != nil {
		rule.Priority = *updates.Priority
	}
	if updates.Conditions != nil {
		rule.Conditions = updates.Conditions
	}
	if updates.Actions != nil {
		rule.Actions = updates.Actions
	}
	if updates.Enabled != nil {
		rule.Enabled = *updates.Enabled
	}
	if updates.Metadata != nil {
		if rule.Metadata == nil {
			rule.Metadata = make(map[string]any)
		}
		for k, v := range updates.Metadata {
			rule.Metadata[k] = v
		}
	}
}

// checkRuleConflicts checks for conflicts between routing rules
func (s *RoutingService) checkRuleConflicts(rules []*ports.RoutingRule) []string {
	var conflicts []string

	// Check for duplicate IDs
	idMap := make(map[string]bool)
	for _, rule := range rules {
		if idMap[rule.ID] {
			conflicts = append(conflicts, fmt.Sprintf("duplicate rule ID: %s", rule.ID))
		}
		idMap[rule.ID] = true
	}

	// Check for duplicate names
	nameMap := make(map[string]bool)
	for _, rule := range rules {
		if nameMap[rule.Name] {
			conflicts = append(conflicts, fmt.Sprintf("duplicate rule name: %s", rule.Name))
		}
		nameMap[rule.Name] = true
	}

	// Check for priority conflicts (rules with same priority)
	priorityMap := make(map[int][]*ports.RoutingRule)
	for _, rule := range rules {
		priorityMap[rule.Priority] = append(priorityMap[rule.Priority], rule)
	}

	for priority, rulesWithPriority := range priorityMap {
		if len(rulesWithPriority) > 1 {
			ruleNames := make([]string, len(rulesWithPriority))
			for i, rule := range rulesWithPriority {
				ruleNames[i] = rule.Name
			}
			conflicts = append(conflicts, fmt.Sprintf("multiple rules with same priority %d: %v", priority, ruleNames))
		}
	}

	return conflicts
}

// Helper functions for metrics
func extractMetricValue(metrics *ports.MetricsSnapshot, key string) int64 {
	// TODO: Implement metric value extraction
	return 0
}

func extractDurationMetric(metrics *ports.MetricsSnapshot, key string) time.Duration {
	// TODO: Implement duration metric extraction
	return 0
}

func extractBackendMetrics(metrics *ports.MetricsSnapshot) map[string]*BackendMetrics {
	// TODO: Implement backend metrics extraction
	return make(map[string]*BackendMetrics)
}

func convertToHTTPHeaders(headers map[string]string) http.Header {
	httpHeaders := make(http.Header)
	for k, v := range headers {
		httpHeaders.Set(k, v)
	}
	return httpHeaders
}
