// Package integration_test provides comprehensive integration tests for the enhanced
// load balancer architecture. These tests verify the interaction between routing engine,
// traffic manager, and observability components.
package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mir00r/load-balancer/internal/observability"
	"github.com/mir00r/load-balancer/internal/routing"
	"github.com/mir00r/load-balancer/internal/traffic"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// TestEnhancedArchitectureIntegration tests the integration of all enhanced components
func TestEnhancedArchitectureIntegration(t *testing.T) {
	// Create test logger
	logger, err := logger.New(logger.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Initialize components
	observabilityManager := observability.NewObservabilityManager("test-service", logger)
	routingEngine := routing.NewRouteEngine(logger)
	trafficManager := traffic.NewTrafficManager(routingEngine, logger)

	// Setup test routing rules
	setupTestRoutes(t, routingEngine)

	// Setup test traffic rules
	setupTestTrafficRules(t, trafficManager)

	// Test scenarios
	t.Run("basic_routing", func(t *testing.T) {
		testBasicRouting(t, routingEngine, observabilityManager)
	})

	t.Run("traffic_management", func(t *testing.T) {
		testTrafficManagement(t, trafficManager, observabilityManager)
	})

	t.Run("observability_integration", func(t *testing.T) {
		testObservabilityIntegration(t, observabilityManager, routingEngine)
	})

	t.Run("end_to_end_flow", func(t *testing.T) {
		testEndToEndFlow(t, routingEngine, trafficManager, observabilityManager)
	})
}

// setupTestRoutes configures test routing rules
func setupTestRoutes(t *testing.T, engine *routing.RouteEngine) {
	rules := []routing.RoutingRule{
		{
			ID:       "api-route",
			Name:     "API Route",
			Priority: 100,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{
					Type:     routing.ConditionPath,
					Operator: routing.OperatorPrefix,
					Values:   []string{"/api/"},
				},
			},
			Actions: []routing.RuleAction{
				{
					Type:   routing.ActionRoute,
					Target: "api-backend",
				},
			},
		},
		{
			ID:       "admin-route",
			Name:     "Admin Route",
			Priority: 90,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{
					Type:     routing.ConditionPath,
					Operator: routing.OperatorPrefix,
					Values:   []string{"/admin/"},
				},
				{
					Type:     routing.ConditionHeader,
					Operator: routing.OperatorEquals,
					Key:      "Authorization",
					Values:   []string{"Bearer admin-token"},
				},
			},
			Actions: []routing.RuleAction{
				{
					Type:   routing.ActionRoute,
					Target: "admin-backend",
				},
			},
		},
		{
			ID:       "redirect-route",
			Name:     "Redirect Route",
			Priority: 80,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{
					Type:     routing.ConditionPath,
					Operator: routing.OperatorEquals,
					Values:   []string{"/old"},
				},
			},
			Actions: []routing.RuleAction{
				{
					Type:   routing.ActionRedirect,
					Target: "/new",
					Params: map[string]string{
						"code": "301",
					},
				},
			},
		},
	}

	for _, rule := range rules {
		if err := engine.AddRule(rule); err != nil {
			t.Fatalf("Failed to add rule %s: %v", rule.ID, err)
		}
	}
}

// setupTestTrafficRules configures test traffic management rules
func setupTestTrafficRules(t *testing.T, manager *traffic.TrafficManager) {
	rules := []*traffic.TrafficRule{
		{
			ID:      "api-traffic",
			Name:    "API Traffic Management",
			RouteID: "api-route",
			Enabled: true,
			LoadBalancer: traffic.LoadBalancerConfig{
				Strategy: traffic.StrategyRoundRobin,
				Backends: []string{"api-backend-1", "api-backend-2"},
			},
			RateLimit: traffic.RateLimitConfig{
				Enabled:           true,
				RequestsPerSecond: 100,
				BurstSize:         200,
				KeyExtractor:      "ip",
			},
			CircuitBreaker: traffic.CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 5,
				RecoveryTimeout:  30 * time.Second,
			},
			Retry: traffic.RetryConfig{
				Enabled:         true,
				MaxAttempts:     3,
				BackoffStrategy: "exponential",
				InitialDelay:    100 * time.Millisecond,
				MaxDelay:        1 * time.Second,
			},
		},
		{
			ID:      "admin-traffic",
			Name:    "Admin Traffic Management",
			RouteID: "admin-route",
			Enabled: true,
			LoadBalancer: traffic.LoadBalancerConfig{
				Strategy: traffic.StrategyLeastConn,
				Backends: []string{"admin-backend-1"},
			},
			RateLimit: traffic.RateLimitConfig{
				Enabled:           true,
				RequestsPerSecond: 10,
				BurstSize:         20,
				KeyExtractor:      "header",
				KeyExtractorParam: "Authorization",
			},
		},
	}

	for _, rule := range rules {
		if err := manager.AddTrafficRule(rule); err != nil {
			t.Fatalf("Failed to add traffic rule %s: %v", rule.ID, err)
		}
	}
}

// testBasicRouting tests basic routing functionality
func testBasicRouting(t *testing.T, engine *routing.RouteEngine, obs *observability.ObservabilityManager) {
	tests := []struct {
		name            string
		method          string
		path            string
		headers         map[string]string
		expectedBackend string
		expectedAction  routing.ActionType
	}{
		{
			name:            "api_route_match",
			method:          "GET",
			path:            "/api/users",
			expectedBackend: "api-backend",
			expectedAction:  routing.ActionRoute,
		},
		{
			name:   "admin_route_match",
			method: "GET",
			path:   "/admin/dashboard",
			headers: map[string]string{
				"Authorization": "Bearer admin-token",
			},
			expectedBackend: "admin-backend",
			expectedAction:  routing.ActionRoute,
		},
		{
			name:            "redirect_route_match",
			method:          "GET",
			path:            "/old",
			expectedBackend: "",
			expectedAction:  routing.ActionRedirect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test request
			req := httptest.NewRequest(tt.method, tt.path, nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			// Start observability span
			span := obs.StartSpan("test-request", nil)
			obs.SetSpanTag(span, "test.case", tt.name)

			// Route request
			result := engine.RouteRequest(req)

			// Verify routing result
			if result.MatchedRule == nil {
				t.Error("Expected routing rule to match")
				obs.SetSpanStatus(span, observability.StatusCodeError, "No rule matched")
			} else {
				if len(result.Actions) == 0 {
					t.Error("Expected routing actions")
				} else {
					action := result.Actions[0]
					if action.Type != tt.expectedAction {
						t.Errorf("Expected action type %v, got %v", tt.expectedAction, action.Type)
					}
					if tt.expectedBackend != "" && action.Target != tt.expectedBackend {
						t.Errorf("Expected backend %s, got %s", tt.expectedBackend, action.Target)
					}
				}
				obs.SetSpanTag(span, "routing.rule", result.MatchedRule.ID)
				obs.SetSpanTag(span, "routing.backend", result.Backend)
			}

			// Finish span
			obs.FinishSpan(span)
		})
	}

	// Check routing statistics
	stats := engine.GetStats()
	if stats.TotalRequests < int64(len(tests)) {
		t.Errorf("Expected at least %d requests processed, got %d", len(tests), stats.TotalRequests)
	}
}

// testTrafficManagement tests traffic management functionality
func testTrafficManagement(t *testing.T, manager *traffic.TrafficManager, obs *observability.ObservabilityManager) {
	// Create test requests
	requests := []*http.Request{
		httptest.NewRequest("GET", "/api/users", nil),
		httptest.NewRequest("GET", "/api/posts", nil),
		httptest.NewRequest("POST", "/api/users", nil),
	}

	// Set different IPs for rate limiting tests
	requests[0].RemoteAddr = "192.168.1.1:12345"
	requests[1].RemoteAddr = "192.168.1.2:12346"
	requests[2].RemoteAddr = "192.168.1.3:12347"

	for i, req := range requests {
		// Start observability span
		span := obs.StartSpan("traffic-test", nil)
		obs.SetSpanTag(span, "request.index", string(rune('0'+i)))
		obs.SetSpanTag(span, "request.path", req.URL.Path)

		// Create mock response writer
		w := httptest.NewRecorder()

		// Process request through traffic manager
		err := manager.ProcessRequest(w, req)

		if err != nil {
			obs.SetSpanStatus(span, observability.StatusCodeError, err.Error())
			obs.LogSpanEvent(span, "error", "Traffic processing failed", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			obs.SetSpanTag(span, "response.status", string(rune('0'+w.Code/100)))
		}

		// Finish span
		obs.FinishSpan(span)
	}

	// Check traffic management statistics
	stats := manager.GetStats()
	if stats.TotalRequests < int64(len(requests)) {
		t.Logf("Traffic manager processed %d requests (expected at least %d)", stats.TotalRequests, len(requests))
	}
}

// testObservabilityIntegration tests observability integration
func testObservabilityIntegration(t *testing.T, obs *observability.ObservabilityManager, engine *routing.RouteEngine) {
	// Test tracing functionality
	parentSpan := obs.StartSpan("integration-test", nil)
	obs.SetSpanTag(parentSpan, "test.type", "integration")

	// Create child spans
	childSpan1 := obs.StartSpan("routing-operation", parentSpan)
	obs.SetSpanTag(childSpan1, "operation.type", "routing")

	// Simulate routing operation
	req := httptest.NewRequest("GET", "/api/test", nil)
	result := engine.RouteRequest(req)

	if result.MatchedRule != nil {
		obs.SetSpanTag(childSpan1, "routing.rule", result.MatchedRule.ID)
		obs.LogSpanEvent(childSpan1, "info", "Rule matched successfully", map[string]interface{}{
			"rule_id": result.MatchedRule.ID,
		})
	}

	obs.FinishSpan(childSpan1)

	// Create another child span
	childSpan2 := obs.StartSpan("backend-operation", parentSpan)
	obs.SetSpanTag(childSpan2, "operation.type", "backend")
	obs.LogSpanEvent(childSpan2, "info", "Backend processing", nil)

	// Simulate backend operation delay
	time.Sleep(10 * time.Millisecond)

	obs.FinishSpan(childSpan2)
	obs.FinishSpan(parentSpan)

	// Verify traces were created
	traces := obs.GetTraces()
	if len(traces) < 3 { // parent + 2 children
		t.Errorf("Expected at least 3 traces, got %d", len(traces))
	}

	// Test metrics collection
	obs.RecordBackendMetrics("test-backend", 50*time.Millisecond, 1024, true)

	metrics := obs.GetMetrics()
	if metrics == nil {
		t.Error("Expected metrics to be available")
	}

	// Test HTTP instrumentation
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	instrumentedHandler := obs.InstrumentHTTPHandler(testHandler, "test-handler")

	req = httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	instrumentedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify request was instrumented
	updatedMetrics := obs.GetMetrics()
	if updatedMetrics == nil {
		t.Error("Expected updated metrics after instrumented request")
	}
}

// testEndToEndFlow tests complete request flow through all components
func testEndToEndFlow(t *testing.T, engine *routing.RouteEngine, manager *traffic.TrafficManager, obs *observability.ObservabilityManager) {
	// Create a complete request handler that uses all components
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Start tracing
		span := obs.StartSpan("end-to-end-request", nil)
		defer obs.FinishSpan(span)

		obs.SetSpanTag(span, "http.method", r.Method)
		obs.SetSpanTag(span, "http.path", r.URL.Path)

		// Route the request
		routingSpan := obs.StartSpan("routing", span)
		result := engine.RouteRequest(r)
		obs.FinishSpan(routingSpan)

		if result.MatchedRule == nil {
			obs.SetSpanStatus(span, observability.StatusCodeError, "No routing rule matched")
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		obs.SetSpanTag(span, "routing.rule", result.MatchedRule.ID)

		// Process through traffic management
		trafficSpan := obs.StartSpan("traffic-management", span)
		err := manager.ProcessRequest(w, r)
		obs.FinishSpan(trafficSpan)

		if err != nil {
			obs.SetSpanStatus(span, observability.StatusCodeError, err.Error())
			if w.Header().Get("Content-Type") == "" {
				http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			}
			return
		}

		// If we get here, the request was processed successfully
		obs.SetSpanStatus(span, observability.StatusCodeOK, "Request processed successfully")
	})

	// Instrument the handler
	instrumentedHandler := obs.InstrumentHTTPHandler(handler, "end-to-end")

	// Test different request scenarios
	tests := []struct {
		name           string
		method         string
		path           string
		headers        map[string]string
		expectedStatus int
	}{
		{
			name:           "api_request",
			method:         "GET",
			path:           "/api/users",
			expectedStatus: http.StatusOK,
		},
		{
			name:   "admin_request",
			method: "GET",
			path:   "/admin/dashboard",
			headers: map[string]string{
				"Authorization": "Bearer admin-token",
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "not_found",
			method:         "GET",
			path:           "/nonexistent",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			w := httptest.NewRecorder()
			instrumentedHandler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}

	// Verify final statistics
	routingStats := engine.GetStats()
	trafficStats := manager.GetStats()
	observabilityMetrics := obs.GetMetrics()

	t.Logf("Final Statistics:")
	t.Logf("  Routing: %d total requests, %d matched", routingStats.TotalRequests, routingStats.MatchedRequests)
	t.Logf("  Traffic: %d total requests, %d successful", trafficStats.TotalRequests, trafficStats.SuccessfulRequests)
	t.Logf("  Traces: %d traces collected", len(obs.GetTraces()))

	// Verify that all components processed requests
	if routingStats.TotalRequests < int64(len(tests)) {
		t.Errorf("Routing engine should have processed at least %d requests", len(tests))
	}

	if observabilityMetrics == nil {
		t.Error("Observability metrics should be available")
	}
}

// TestConcurrentRequestProcessing tests concurrent request processing
func TestConcurrentRequestProcessing(t *testing.T) {
	// Setup components
	logger, err := logger.New(logger.Config{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	obs := observability.NewObservabilityManager("concurrent-test", logger)
	engine := routing.NewRouteEngine(logger)
	manager := traffic.NewTrafficManager(engine, logger)

	// Setup test routes
	setupTestRoutes(t, engine)

	// Number of concurrent requests
	numRequests := 100
	numWorkers := 10

	// Create channels for coordinating test
	requestChan := make(chan *http.Request, numRequests)
	resultChan := make(chan bool, numRequests)

	// Generate test requests
	for i := 0; i < numRequests; i++ {
		path := "/api/test"
		if i%3 == 0 {
			path = "/admin/test"
		}
		req := httptest.NewRequest("GET", path, nil)
		if path == "/admin/test" {
			req.Header.Set("Authorization", "Bearer admin-token")
		}
		req.RemoteAddr = "192.168.1." + string(rune('0'+i%10)) + ":12345"
		requestChan <- req
	}
	close(requestChan)

	// Start concurrent workers
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			for req := range requestChan {
				// Start tracing
				span := obs.StartSpan("concurrent-request", nil)
				obs.SetSpanTag(span, "worker.id", string(rune('0'+workerID)))

				// Route request
				result := engine.RouteRequest(req)
				success := result.MatchedRule != nil

				if success {
					// Process through traffic manager
					w := httptest.NewRecorder()
					err := manager.ProcessRequest(w, req)
					success = err == nil
				}

				obs.SetSpanStatus(span, observability.StatusCodeOK, "Processed")
				obs.FinishSpan(span)

				resultChan <- success
			}
		}(i)
	}

	// Collect results
	successCount := 0
	for i := 0; i < numRequests; i++ {
		if <-resultChan {
			successCount++
		}
	}

	t.Logf("Successfully processed %d out of %d concurrent requests", successCount, numRequests)

	// Verify statistics
	stats := engine.GetStats()
	if stats.TotalRequests != int64(numRequests) {
		t.Errorf("Expected %d total requests, got %d", numRequests, stats.TotalRequests)
	}

	traces := obs.GetTraces()
	if len(traces) < numRequests {
		t.Logf("Created %d traces for %d requests", len(traces), numRequests)
	}
}

// TestComponentFailureRecovery tests recovery from component failures
func TestComponentFailureRecovery(t *testing.T) {
	logger, err := logger.New(logger.Config{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	obs := observability.NewObservabilityManager("failure-test", logger)
	engine := routing.NewRouteEngine(logger)
	manager := traffic.NewTrafficManager(engine, logger)

	// Test observability failure handling
	t.Run("observability_disabled", func(t *testing.T) {
		obs.Disable()

		span := obs.StartSpan("test-span", nil)

		// Should handle gracefully when disabled
		if span != nil {
			t.Error("Expected nil span when observability is disabled")
		}

		obs.Enable()
	})

	// Test routing engine with invalid rules
	t.Run("invalid_routing_rules", func(t *testing.T) {
		invalidRule := routing.RoutingRule{
			// Missing required fields
			Priority: 100,
		}

		err := engine.AddRule(invalidRule)
		if err == nil {
			t.Error("Expected error when adding invalid rule")
		}

		// Engine should still be functional
		req := httptest.NewRequest("GET", "/test", nil)
		result := engine.RouteRequest(req)

		// Should return default result for unmatched requests
		if result == nil {
			t.Error("Expected routing result even for unmatched requests")
		}
	})

	// Test traffic manager resilience
	t.Run("traffic_manager_resilience", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/unknown", nil)
		w := httptest.NewRecorder()

		// Should handle requests even without matching rules
		err := manager.ProcessRequest(w, req)

		// May return error but shouldn't panic
		if err == nil {
			t.Log("Traffic manager handled unknown route gracefully")
		} else {
			t.Logf("Traffic manager returned expected error: %v", err)
		}
	})
}

// Helper function to create test context with timeout
func createTestContext(t *testing.T, timeout time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}
