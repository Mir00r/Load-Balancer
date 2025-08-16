// Package traffic provides advanced traffic management capabilities inspired by Traefik.
// This package integrates with the routing engine to provide sophisticated traffic
// shaping, load balancing, and request processing features.
package traffic

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/routing"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// LoadBalancerStrategy defines the load balancing strategy for a traffic rule
type LoadBalancerStrategy string

const (
	StrategyRoundRobin  LoadBalancerStrategy = "round_robin"
	StrategyWeighted    LoadBalancerStrategy = "weighted"
	StrategyLeastConn   LoadBalancerStrategy = "least_conn"
	StrategyIPHash      LoadBalancerStrategy = "ip_hash"
	StrategyRandom      LoadBalancerStrategy = "random"
	StrategyHealthBased LoadBalancerStrategy = "health_based"
)

// CircuitBreakerConfig defines circuit breaker configuration for traffic management
type CircuitBreakerConfig struct {
	Enabled             bool          `json:"enabled" yaml:"enabled"`
	FailureThreshold    int           `json:"failure_threshold" yaml:"failure_threshold"`
	RecoveryTimeout     time.Duration `json:"recovery_timeout" yaml:"recovery_timeout"`
	MinRequestThreshold int           `json:"min_request_threshold" yaml:"min_request_threshold"`
	SuccessThreshold    int           `json:"success_threshold" yaml:"success_threshold"`
	MonitoringWindow    time.Duration `json:"monitoring_window" yaml:"monitoring_window"`
}

// RateLimitConfig defines rate limiting configuration for traffic management
type RateLimitConfig struct {
	Enabled           bool          `json:"enabled" yaml:"enabled"`
	RequestsPerSecond float64       `json:"requests_per_second" yaml:"requests_per_second"`
	BurstSize         int           `json:"burst_size" yaml:"burst_size"`
	WindowSize        time.Duration `json:"window_size" yaml:"window_size"`
	KeyExtractor      string        `json:"key_extractor" yaml:"key_extractor"` // "ip", "header", "query"
	KeyExtractorParam string        `json:"key_extractor_param" yaml:"key_extractor_param"`
}

// RetryConfig defines retry configuration for failed requests
type RetryConfig struct {
	Enabled         bool          `json:"enabled" yaml:"enabled"`
	MaxAttempts     int           `json:"max_attempts" yaml:"max_attempts"`
	BackoffStrategy string        `json:"backoff_strategy" yaml:"backoff_strategy"` // "linear", "exponential"
	InitialDelay    time.Duration `json:"initial_delay" yaml:"initial_delay"`
	MaxDelay        time.Duration `json:"max_delay" yaml:"max_delay"`
	RetryConditions []string      `json:"retry_conditions" yaml:"retry_conditions"` // HTTP status codes
}

// TimeoutConfig defines timeout configuration for requests
type TimeoutConfig struct {
	ConnectTimeout time.Duration `json:"connect_timeout" yaml:"connect_timeout"`
	RequestTimeout time.Duration `json:"request_timeout" yaml:"request_timeout"`
	IdleTimeout    time.Duration `json:"idle_timeout" yaml:"idle_timeout"`
}

// HealthCheckConfig defines health check configuration for backends
type HealthCheckConfig struct {
	Enabled            bool          `json:"enabled" yaml:"enabled"`
	Path               string        `json:"path" yaml:"path"`
	Interval           time.Duration `json:"interval" yaml:"interval"`
	Timeout            time.Duration `json:"timeout" yaml:"timeout"`
	HealthyCode        int           `json:"healthy_code" yaml:"healthy_code"`
	UnhealthyThreshold int           `json:"unhealthy_threshold" yaml:"unhealthy_threshold"`
	HealthyThreshold   int           `json:"healthy_threshold" yaml:"healthy_threshold"`
}

// LoadBalancerConfig defines load balancer configuration for traffic rules
type LoadBalancerConfig struct {
	Strategy      LoadBalancerStrategy `json:"strategy" yaml:"strategy"`
	Backends      []string             `json:"backends" yaml:"backends"`
	Weights       map[string]int       `json:"weights,omitempty" yaml:"weights,omitempty"`
	HealthCheck   HealthCheckConfig    `json:"health_check" yaml:"health_check"`
	StickySession bool                 `json:"sticky_session" yaml:"sticky_session"`
}

// TrafficRule defines comprehensive traffic management for a specific route
type TrafficRule struct {
	ID             string               `json:"id" yaml:"id"`
	Name           string               `json:"name" yaml:"name"`
	RouteID        string               `json:"route_id" yaml:"route_id"`
	Enabled        bool                 `json:"enabled" yaml:"enabled"`
	LoadBalancer   LoadBalancerConfig   `json:"load_balancer" yaml:"load_balancer"`
	CircuitBreaker CircuitBreakerConfig `json:"circuit_breaker" yaml:"circuit_breaker"`
	RateLimit      RateLimitConfig      `json:"rate_limit" yaml:"rate_limit"`
	Retry          RetryConfig          `json:"retry" yaml:"retry"`
	Timeout        TimeoutConfig        `json:"timeout" yaml:"timeout"`

	// Metadata
	CreatedAt time.Time         `json:"created_at" yaml:"created_at"`
	UpdatedAt time.Time         `json:"updated_at" yaml:"updated_at"`
	Tags      map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`

	// Statistics
	RequestCount int64     `json:"request_count"`
	ErrorCount   int64     `json:"error_count"`
	LastUsed     time.Time `json:"last_used,omitempty"`
}

// TrafficStats provides comprehensive traffic management statistics
type TrafficStats struct {
	TotalRequests       int64         `json:"total_requests"`
	SuccessfulRequests  int64         `json:"successful_requests"`
	FailedRequests      int64         `json:"failed_requests"`
	RetryAttempts       int64         `json:"retry_attempts"`
	CircuitBreakerTrips int64         `json:"circuit_breaker_trips"`
	RateLimitExceeded   int64         `json:"rate_limit_exceeded"`
	AverageResponseTime time.Duration `json:"average_response_time"`

	// Backend statistics
	BackendStats map[string]*BackendStats `json:"backend_stats"`
}

// BackendStats provides statistics for individual backends
type BackendStats struct {
	RequestCount        int64         `json:"request_count"`
	ErrorCount          int64         `json:"error_count"`
	AverageLatency      time.Duration `json:"average_latency"`
	HealthStatus        string        `json:"health_status"`
	LastHealthCheck     time.Time     `json:"last_health_check"`
	CircuitBreakerState string        `json:"circuit_breaker_state"`
}

// TrafficManager manages advanced traffic routing and processing
type TrafficManager struct {
	routingEngine *routing.RouteEngine

	// Traffic rules and load balancers
	trafficRules  map[string]*TrafficRule
	loadBalancers map[string]domain.LoadBalancer
	rulesMutex    sync.RWMutex

	// Circuit breakers and rate limiters
	circuitBreakers map[string]*CircuitBreaker
	rateLimiters    map[string]*RateLimiter
	breakersMutex   sync.RWMutex

	// Statistics
	stats      TrafficStats
	statsMutex sync.RWMutex

	logger *logger.Logger
}

// CircuitBreaker implements the circuit breaker pattern for fault tolerance
type CircuitBreaker struct {
	config       CircuitBreakerConfig
	state        string // "closed", "open", "half-open"
	failures     int
	requests     int
	lastFailTime time.Time
	mutex        sync.RWMutex
}

// RateLimiter implements rate limiting for request throttling
type RateLimiter struct {
	config       RateLimitConfig
	buckets      map[string]*TokenBucket
	bucketsMutex sync.RWMutex
}

// TokenBucket implements a token bucket for rate limiting
type TokenBucket struct {
	capacity   int
	tokens     int
	refillRate float64
	lastRefill time.Time
	mutex      sync.Mutex
}

// NewTrafficManager creates a new traffic manager with the provided routing engine
func NewTrafficManager(routingEngine *routing.RouteEngine, logger *logger.Logger) *TrafficManager {
	return &TrafficManager{
		routingEngine:   routingEngine,
		trafficRules:    make(map[string]*TrafficRule),
		loadBalancers:   make(map[string]domain.LoadBalancer),
		circuitBreakers: make(map[string]*CircuitBreaker),
		rateLimiters:    make(map[string]*RateLimiter),
		logger:          logger,
		stats: TrafficStats{
			BackendStats: make(map[string]*BackendStats),
		},
	}
}

// AddTrafficRule adds a new traffic management rule
func (tm *TrafficManager) AddTrafficRule(rule *TrafficRule) error {
	tm.rulesMutex.Lock()
	defer tm.rulesMutex.Unlock()

	// Validate rule
	if err := tm.validateTrafficRule(rule); err != nil {
		return fmt.Errorf("invalid traffic rule: %w", err)
	}

	// Set metadata
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()

	// Store rule
	tm.trafficRules[rule.ID] = rule

	// Initialize circuit breaker if enabled
	if rule.CircuitBreaker.Enabled {
		tm.breakersMutex.Lock()
		tm.circuitBreakers[rule.ID] = &CircuitBreaker{
			config: rule.CircuitBreaker,
			state:  "closed",
		}
		tm.breakersMutex.Unlock()
	}

	// Initialize rate limiter if enabled
	if rule.RateLimit.Enabled {
		tm.breakersMutex.Lock()
		tm.rateLimiters[rule.ID] = &RateLimiter{
			config:  rule.RateLimit,
			buckets: make(map[string]*TokenBucket),
		}
		tm.breakersMutex.Unlock()
	}

	tm.logger.WithFields(map[string]interface{}{
		"rule_id":   rule.ID,
		"rule_name": rule.Name,
		"route_id":  rule.RouteID,
	}).Info("Traffic rule added")

	return nil
}

// ProcessRequest processes an incoming request through the traffic management pipeline
func (tm *TrafficManager) ProcessRequest(w http.ResponseWriter, r *http.Request) error {
	startTime := time.Now()

	// Update request statistics
	tm.updateRequestStats(1, 0)

	// Route the request
	routingResult := tm.routingEngine.RouteRequest(r)
	if routingResult.MatchedRule == nil {
		// No routing rule matched, use default processing
		return tm.processDefaultRequest(w, r)
	}

	// Find traffic rule for the matched route
	tm.rulesMutex.RLock()
	trafficRule, exists := tm.trafficRules[routingResult.MatchedRule.ID]
	tm.rulesMutex.RUnlock()

	if !exists || !trafficRule.Enabled {
		// No traffic rule or disabled, use default processing
		return tm.processDefaultRequest(w, r)
	}

	// Process through traffic management pipeline
	if err := tm.processTrafficRule(w, r, trafficRule, routingResult); err != nil {
		tm.updateRequestStats(0, 1)
		return err
	}

	// Update statistics
	processingTime := time.Since(startTime)
	tm.updateProcessingTime(processingTime)
	tm.updateRequestStats(0, 0)

	return nil
}

// processTrafficRule processes a request through a specific traffic rule
func (tm *TrafficManager) processTrafficRule(w http.ResponseWriter, r *http.Request, rule *TrafficRule, routingResult *routing.RoutingResult) error {
	// Apply rate limiting
	if rule.RateLimit.Enabled {
		if !tm.checkRateLimit(r, rule) {
			tm.statsMutex.Lock()
			tm.stats.RateLimitExceeded++
			tm.statsMutex.Unlock()

			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return fmt.Errorf("rate limit exceeded")
		}
	}

	// Check circuit breaker
	if rule.CircuitBreaker.Enabled {
		if !tm.checkCircuitBreaker(rule.ID) {
			tm.statsMutex.Lock()
			tm.stats.CircuitBreakerTrips++
			tm.statsMutex.Unlock()

			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			return fmt.Errorf("circuit breaker open")
		}
	}

	// Apply custom headers from routing result
	if routingResult.Headers != nil {
		for key, values := range routingResult.Headers {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
	}

	// Handle different action types
	for _, action := range routingResult.Actions {
		switch action.Type {
		case routing.ActionRedirect:
			if routingResult.StatusCode > 0 {
				w.WriteHeader(routingResult.StatusCode)
			}
			return nil
		case routing.ActionBlock:
			http.Error(w, "Request blocked", http.StatusForbidden)
			return fmt.Errorf("request blocked")
		case routing.ActionRoute:
			return tm.processRouteAction(w, r, rule, action.Target)
		}
	}

	return nil
}

// processRouteAction processes a route action through load balancing
func (tm *TrafficManager) processRouteAction(w http.ResponseWriter, r *http.Request, rule *TrafficRule, backend string) error {
	// Get or create load balancer for this rule
	loadBalancer, err := tm.getLoadBalancer(rule)
	if err != nil {
		return fmt.Errorf("failed to get load balancer: %w", err)
	}

	// Select backend
	selectedBackend := loadBalancer.SelectBackend()
	if selectedBackend == nil {
		http.Error(w, "No backend available", http.StatusServiceUnavailable)
		return fmt.Errorf("no backend available")
	}

	// Process request with retry logic
	if rule.Retry.Enabled {
		return tm.processWithRetry(w, r, rule, selectedBackend)
	}

	// Process request directly
	return tm.forwardToBackend(w, r, selectedBackend)
}

// processDefaultRequest processes a request without specific traffic management
func (tm *TrafficManager) processDefaultRequest(w http.ResponseWriter, r *http.Request) error {
	// This would integrate with the existing load balancer logic
	// For now, return an error indicating no handler
	http.Error(w, "No route found", http.StatusNotFound)
	return fmt.Errorf("no route found")
}

// checkRateLimit checks if a request should be rate limited
func (tm *TrafficManager) checkRateLimit(r *http.Request, rule *TrafficRule) bool {
	tm.breakersMutex.RLock()
	rateLimiter, exists := tm.rateLimiters[rule.ID]
	tm.breakersMutex.RUnlock()

	if !exists {
		return true // No rate limiter, allow request
	}

	// Extract rate limiting key
	key := tm.extractRateLimitKey(r, rule.RateLimit)

	// Check token bucket
	rateLimiter.bucketsMutex.RLock()
	bucket, exists := rateLimiter.buckets[key]
	rateLimiter.bucketsMutex.RUnlock()

	if !exists {
		// Create new bucket
		bucket = &TokenBucket{
			capacity:   rule.RateLimit.BurstSize,
			tokens:     rule.RateLimit.BurstSize,
			refillRate: rule.RateLimit.RequestsPerSecond,
			lastRefill: time.Now(),
		}
		rateLimiter.bucketsMutex.Lock()
		rateLimiter.buckets[key] = bucket
		rateLimiter.bucketsMutex.Unlock()
	}

	return bucket.consume(1)
}

// checkCircuitBreaker checks if the circuit breaker allows the request
func (tm *TrafficManager) checkCircuitBreaker(ruleID string) bool {
	tm.breakersMutex.RLock()
	breaker, exists := tm.circuitBreakers[ruleID]
	tm.breakersMutex.RUnlock()

	if !exists {
		return true // No circuit breaker, allow request
	}

	breaker.mutex.Lock()
	defer breaker.mutex.Unlock()

	now := time.Now()

	switch breaker.state {
	case "closed":
		return true
	case "open":
		if now.Sub(breaker.lastFailTime) > breaker.config.RecoveryTimeout {
			breaker.state = "half-open"
			breaker.requests = 0
			breaker.failures = 0
			return true
		}
		return false
	case "half-open":
		return true
	}

	return false
}

// extractRateLimitKey extracts the rate limiting key from the request
func (tm *TrafficManager) extractRateLimitKey(r *http.Request, config RateLimitConfig) string {
	switch config.KeyExtractor {
	case "ip":
		return r.RemoteAddr
	case "header":
		return r.Header.Get(config.KeyExtractorParam)
	case "query":
		return r.URL.Query().Get(config.KeyExtractorParam)
	default:
		return "default"
	}
}

// getLoadBalancer gets or creates a load balancer for a traffic rule
func (tm *TrafficManager) getLoadBalancer(rule *TrafficRule) (domain.LoadBalancer, error) {
	tm.rulesMutex.RLock()
	lb, exists := tm.loadBalancers[rule.ID]
	tm.rulesMutex.RUnlock()

	if exists {
		return lb, nil
	}

	// This would create a load balancer based on the rule configuration
	// For now, return an error
	return nil, fmt.Errorf("load balancer not implemented")
}

// processWithRetry processes a request with retry logic
func (tm *TrafficManager) processWithRetry(w http.ResponseWriter, r *http.Request, rule *TrafficRule, backend *domain.Backend) error {
	var lastError error

	for attempt := 1; attempt <= rule.Retry.MaxAttempts; attempt++ {
		err := tm.forwardToBackend(w, r, backend)
		if err == nil {
			return nil // Success
		}

		lastError = err
		tm.statsMutex.Lock()
		tm.stats.RetryAttempts++
		tm.statsMutex.Unlock()

		// Calculate backoff delay
		if attempt < rule.Retry.MaxAttempts {
			delay := tm.calculateBackoffDelay(rule.Retry, attempt)
			time.Sleep(delay)
		}
	}

	return lastError
}

// forwardToBackend forwards the request to the selected backend
func (tm *TrafficManager) forwardToBackend(w http.ResponseWriter, r *http.Request, backend *domain.Backend) error {
	// This would implement the actual request forwarding
	// For now, return a placeholder implementation
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("Forwarded to backend: %s", backend.URL)))
	return nil
}

// calculateBackoffDelay calculates the backoff delay for retry attempts
func (tm *TrafficManager) calculateBackoffDelay(config RetryConfig, attempt int) time.Duration {
	switch config.BackoffStrategy {
	case "exponential":
		delay := config.InitialDelay * time.Duration(1<<uint(attempt-1))
		if delay > config.MaxDelay {
			return config.MaxDelay
		}
		return delay
	case "linear":
		delay := config.InitialDelay * time.Duration(attempt)
		if delay > config.MaxDelay {
			return config.MaxDelay
		}
		return delay
	default:
		return config.InitialDelay
	}
}

// validateTrafficRule validates a traffic rule configuration
func (tm *TrafficManager) validateTrafficRule(rule *TrafficRule) error {
	if rule.ID == "" {
		return fmt.Errorf("traffic rule ID is required")
	}

	if rule.Name == "" {
		return fmt.Errorf("traffic rule name is required")
	}

	if rule.RouteID == "" {
		return fmt.Errorf("route ID is required")
	}

	// Validate load balancer configuration
	if len(rule.LoadBalancer.Backends) == 0 {
		return fmt.Errorf("at least one backend is required")
	}

	// Validate rate limiting configuration
	if rule.RateLimit.Enabled {
		if rule.RateLimit.RequestsPerSecond <= 0 {
			return fmt.Errorf("requests per second must be positive")
		}
		if rule.RateLimit.BurstSize <= 0 {
			return fmt.Errorf("burst size must be positive")
		}
	}

	// Validate circuit breaker configuration
	if rule.CircuitBreaker.Enabled {
		if rule.CircuitBreaker.FailureThreshold <= 0 {
			return fmt.Errorf("failure threshold must be positive")
		}
		if rule.CircuitBreaker.RecoveryTimeout <= 0 {
			return fmt.Errorf("recovery timeout must be positive")
		}
	}

	// Validate retry configuration
	if rule.Retry.Enabled {
		if rule.Retry.MaxAttempts <= 0 {
			return fmt.Errorf("max attempts must be positive")
		}
		if rule.Retry.InitialDelay <= 0 {
			return fmt.Errorf("initial delay must be positive")
		}
	}

	return nil
}

// consume consumes tokens from the token bucket
func (tb *TokenBucket) consume(tokens int) bool {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)

	// Refill tokens
	tokensToAdd := int(elapsed.Seconds() * tb.refillRate)
	tb.tokens = min(tb.capacity, tb.tokens+tokensToAdd)
	tb.lastRefill = now

	// Check if we have enough tokens
	if tb.tokens >= tokens {
		tb.tokens -= tokens
		return true
	}

	return false
}

// updateRequestStats updates request statistics
func (tm *TrafficManager) updateRequestStats(successIncrement, errorIncrement int64) {
	tm.statsMutex.Lock()
	defer tm.statsMutex.Unlock()

	tm.stats.TotalRequests += successIncrement + errorIncrement
	tm.stats.SuccessfulRequests += successIncrement
	tm.stats.FailedRequests += errorIncrement
}

// updateProcessingTime updates processing time statistics
func (tm *TrafficManager) updateProcessingTime(processingTime time.Duration) {
	tm.statsMutex.Lock()
	defer tm.statsMutex.Unlock()

	// Calculate moving average (simplified)
	if tm.stats.TotalRequests > 0 {
		tm.stats.AverageResponseTime = time.Duration(
			(int64(tm.stats.AverageResponseTime)*int64(tm.stats.TotalRequests-1) + int64(processingTime)) / int64(tm.stats.TotalRequests),
		)
	} else {
		tm.stats.AverageResponseTime = processingTime
	}
}

// GetStats returns traffic management statistics
func (tm *TrafficManager) GetStats() TrafficStats {
	tm.statsMutex.RLock()
	defer tm.statsMutex.RUnlock()

	return tm.stats
}

// GetTrafficRules returns all traffic rules
func (tm *TrafficManager) GetTrafficRules() map[string]*TrafficRule {
	tm.rulesMutex.RLock()
	defer tm.rulesMutex.RUnlock()

	// Return a copy to prevent external modification
	rules := make(map[string]*TrafficRule)
	for k, v := range tm.trafficRules {
		rules[k] = v
	}
	return rules
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
