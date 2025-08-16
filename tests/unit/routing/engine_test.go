// Package routing_test provides comprehensive tests for the routing engine.
// These tests cover rule matching, performance, and edge cases following
// industry-standard testing practices.
package routing_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/mir00r/load-balancer/internal/routing"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// TestRouteEngine_AddRule tests adding routing rules
func TestRouteEngine_AddRule(t *testing.T) {
	logger, _ := logger.New(logger.Config{Level: "info", Format: "json", Output: "stdout"})
	engine := routing.NewRouteEngine(logger)

	tests := []struct {
		name    string
		rule    routing.RoutingRule
		wantErr bool
	}{
		{
			name: "valid_host_rule",
			rule: routing.RoutingRule{
				ID:       "test-1",
				Name:     "Test Host Rule",
				Priority: 100,
				Enabled:  true,
				Conditions: []routing.RuleCondition{
					{
						Type:     routing.ConditionHost,
						Operator: routing.OperatorEquals,
						Values:   []string{"api.example.com"},
					},
				},
				Actions: []routing.RuleAction{
					{
						Type:   routing.ActionRoute,
						Target: "api-backend",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid_path_rule",
			rule: routing.RoutingRule{
				ID:       "test-2",
				Name:     "Test Path Rule",
				Priority: 90,
				Enabled:  true,
				Conditions: []routing.RuleCondition{
					{
						Type:     routing.ConditionPath,
						Operator: routing.OperatorPrefix,
						Values:   []string{"/api/v1"},
					},
				},
				Actions: []routing.RuleAction{
					{
						Type:   routing.ActionRoute,
						Target: "api-v1-backend",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid_rule_no_id",
			rule: routing.RoutingRule{
				Name:     "Test Invalid Rule",
				Priority: 80,
				Enabled:  true,
				Conditions: []routing.RuleCondition{
					{
						Type:     routing.ConditionPath,
						Operator: routing.OperatorEquals,
						Values:   []string{"/test"},
					},
				},
				Actions: []routing.RuleAction{
					{
						Type:   routing.ActionRoute,
						Target: "test-backend",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid_rule_no_conditions",
			rule: routing.RoutingRule{
				ID:         "test-3",
				Name:       "Test No Conditions",
				Priority:   70,
				Enabled:    true,
				Conditions: []routing.RuleCondition{},
				Actions: []routing.RuleAction{
					{
						Type:   routing.ActionRoute,
						Target: "test-backend",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid_rule_no_actions",
			rule: routing.RoutingRule{
				ID:       "test-4",
				Name:     "Test No Actions",
				Priority: 60,
				Enabled:  true,
				Conditions: []routing.RuleCondition{
					{
						Type:     routing.ConditionPath,
						Operator: routing.OperatorEquals,
						Values:   []string{"/test"},
					},
				},
				Actions: []routing.RuleAction{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := engine.AddRule(tt.rule)
			if (err != nil) != tt.wantErr {
				t.Errorf("RouteEngine.AddRule() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestRouteEngine_RouteRequest tests request routing functionality
func TestRouteEngine_RouteRequest(t *testing.T) {
	logger, _ := logger.New(logger.Config{Level: "info", Format: "json", Output: "stdout"})
	engine := routing.NewRouteEngine(logger)

	// Add test rules
	rules := []routing.RoutingRule{
		{
			ID:       "host-rule",
			Name:     "Host Based Rule",
			Priority: 100,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{
					Type:     routing.ConditionHost,
					Operator: routing.OperatorEquals,
					Values:   []string{"api.example.com"},
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
			ID:       "path-prefix-rule",
			Name:     "Path Prefix Rule",
			Priority: 90,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{
					Type:     routing.ConditionPath,
					Operator: routing.OperatorPrefix,
					Values:   []string{"/admin/"},
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
			ID:       "method-rule",
			Name:     "HTTP Method Rule",
			Priority: 80,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{
					Type:     routing.ConditionMethod,
					Operator: routing.OperatorEquals,
					Values:   []string{"POST"},
				},
				{
					Type:     routing.ConditionPath,
					Operator: routing.OperatorPrefix,
					Values:   []string{"/api/"},
				},
			},
			Actions: []routing.RuleAction{
				{
					Type:   routing.ActionRoute,
					Target: "api-post-backend",
				},
			},
		},
		{
			ID:       "redirect-rule",
			Name:     "Redirect Rule",
			Priority: 70,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{
					Type:     routing.ConditionPath,
					Operator: routing.OperatorEquals,
					Values:   []string{"/old-path"},
				},
			},
			Actions: []routing.RuleAction{
				{
					Type:   routing.ActionRedirect,
					Target: "https://example.com/new-path",
				},
			},
		},
	}

	for _, rule := range rules {
		if err := engine.AddRule(rule); err != nil {
			t.Fatalf("Failed to add rule %s: %v", rule.ID, err)
		}
	}

	tests := []struct {
		name            string
		request         *http.Request
		expectedBackend string
		expectedAction  routing.ActionType
		shouldMatch     bool
	}{
		{
			name: "host_match",
			request: &http.Request{
				Method: "GET",
				Host:   "api.example.com",
				URL:    &url.URL{Path: "/users"},
			},
			expectedBackend: "api-backend",
			expectedAction:  routing.ActionRoute,
			shouldMatch:     true,
		},
		{
			name: "path_prefix_match",
			request: &http.Request{
				Method: "GET",
				Host:   "example.com",
				URL:    &url.URL{Path: "/admin/dashboard"},
			},
			expectedBackend: "admin-backend",
			expectedAction:  routing.ActionRoute,
			shouldMatch:     true,
		},
		{
			name: "method_and_path_match",
			request: &http.Request{
				Method: "POST",
				Host:   "example.com",
				URL:    &url.URL{Path: "/api/users"},
			},
			expectedBackend: "api-post-backend",
			expectedAction:  routing.ActionRoute,
			shouldMatch:     true,
		},
		{
			name: "redirect_match",
			request: &http.Request{
				Method: "GET",
				Host:   "example.com",
				URL:    &url.URL{Path: "/old-path"},
			},
			expectedBackend: "",
			expectedAction:  routing.ActionRedirect,
			shouldMatch:     true,
		},
		{
			name: "no_match",
			request: &http.Request{
				Method: "GET",
				Host:   "unknown.com",
				URL:    &url.URL{Path: "/unknown"},
			},
			expectedBackend: "",
			expectedAction:  "",
			shouldMatch:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.RouteRequest(tt.request)

			if tt.shouldMatch {
				if result.MatchedRule == nil {
					t.Error("Expected rule to match but no rule matched")
					return
				}

				if len(result.Actions) == 0 {
					t.Error("Expected actions but got none")
					return
				}

				action := result.Actions[0]
				if action.Type != tt.expectedAction {
					t.Errorf("Expected action type %v, got %v", tt.expectedAction, action.Type)
				}

				if tt.expectedBackend != "" && action.Target != tt.expectedBackend {
					t.Errorf("Expected backend %s, got %s", tt.expectedBackend, action.Target)
				}
			} else {
				if result.MatchedRule != nil {
					t.Error("Expected no rule to match but a rule matched")
				}
			}
		})
	}
}

// TestRouteEngine_Performance tests routing engine performance
func TestRouteEngine_Performance(t *testing.T) {
	logger, _ := logger.New(logger.Config{Level: "error", Format: "json", Output: "stdout"})
	engine := routing.NewRouteEngine(logger)

	// Add many rules to test performance
	numRules := 1000
	for i := 0; i < numRules; i++ {
		rule := routing.RoutingRule{
			ID:       formatRuleID("perf-rule", i),
			Name:     formatRuleName("Performance Rule", i),
			Priority: 100 - i, // Descending priority
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{
					Type:     routing.ConditionPath,
					Operator: routing.OperatorPrefix,
					Values:   []string{formatPath("/api/v", i)},
				},
			},
			Actions: []routing.RuleAction{
				{
					Type:   routing.ActionRoute,
					Target: formatBackend("backend", i),
				},
			},
		}

		if err := engine.AddRule(rule); err != nil {
			t.Fatalf("Failed to add rule %d: %v", i, err)
		}
	}

	// Test routing performance
	request := &http.Request{
		Method: "GET",
		Host:   "example.com",
		URL:    &url.URL{Path: "/api/v500/users"},
	}

	// Warm up
	for i := 0; i < 100; i++ {
		engine.RouteRequest(request)
	}

	// Measure performance
	numRequests := 10000
	start := time.Now()

	for i := 0; i < numRequests; i++ {
		result := engine.RouteRequest(request)
		if result.MatchedRule == nil {
			t.Error("Expected rule to match in performance test")
		}
	}

	elapsed := time.Since(start)
	avgLatency := elapsed / time.Duration(numRequests)

	t.Logf("Processed %d requests in %v (avg: %v per request)", numRequests, elapsed, avgLatency)

	// Performance requirement: average latency should be under 100 microseconds
	if avgLatency > 100*time.Microsecond {
		t.Errorf("Performance test failed: average latency %v exceeds 100µs", avgLatency)
	}

	// Check routing statistics
	stats := engine.GetStats()
	if stats.TotalRequests < int64(numRequests) {
		t.Errorf("Expected at least %d total requests, got %d", numRequests, stats.TotalRequests)
	}

	if stats.TotalRules != numRules {
		t.Errorf("Expected %d total rules, got %d", numRules, stats.TotalRules)
	}
}

// TestRouteEngine_RegexConditions tests regex-based routing conditions
func TestRouteEngine_RegexConditions(t *testing.T) {
	logger, _ := logger.New(logger.Config{Level: "info", Format: "json", Output: "stdout"})
	engine := routing.NewRouteEngine(logger)

	rule := routing.RoutingRule{
		ID:       "regex-rule",
		Name:     "Regex Rule",
		Priority: 100,
		Enabled:  true,
		Conditions: []routing.RuleCondition{
			{
				Type:     routing.ConditionPath,
				Operator: routing.OperatorRegex,
				Values:   []string{`^/api/v\d+/users/\d+$`},
			},
		},
		Actions: []routing.RuleAction{
			{
				Type:   routing.ActionRoute,
				Target: "user-api-backend",
			},
		},
	}

	if err := engine.AddRule(rule); err != nil {
		t.Fatalf("Failed to add regex rule: %v", err)
	}

	tests := []struct {
		name        string
		path        string
		shouldMatch bool
	}{
		{
			name:        "valid_user_api_v1",
			path:        "/api/v1/users/123",
			shouldMatch: true,
		},
		{
			name:        "valid_user_api_v2",
			path:        "/api/v2/users/456",
			shouldMatch: true,
		},
		{
			name:        "invalid_no_version",
			path:        "/api/users/123",
			shouldMatch: false,
		},
		{
			name:        "invalid_no_user_id",
			path:        "/api/v1/users/",
			shouldMatch: false,
		},
		{
			name:        "invalid_wrong_resource",
			path:        "/api/v1/posts/123",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &http.Request{
				Method: "GET",
				Host:   "example.com",
				URL:    &url.URL{Path: tt.path},
			}

			result := engine.RouteRequest(request)

			if tt.shouldMatch {
				if result.MatchedRule == nil {
					t.Error("Expected regex rule to match but no rule matched")
				} else if result.MatchedRule.ID != "regex-rule" {
					t.Error("Expected regex rule to match but different rule matched")
				}
			} else {
				if result.MatchedRule != nil && result.MatchedRule.ID == "regex-rule" {
					t.Error("Expected regex rule not to match but it matched")
				}
			}
		})
	}
}

// TestRouteEngine_HeaderConditions tests header-based routing conditions
func TestRouteEngine_HeaderConditions(t *testing.T) {
	logger, _ := logger.New(logger.Config{Level: "info", Format: "json", Output: "stdout"})
	engine := routing.NewRouteEngine(logger)

	rule := routing.RoutingRule{
		ID:       "header-rule",
		Name:     "Header Rule",
		Priority: 100,
		Enabled:  true,
		Conditions: []routing.RuleCondition{
			{
				Type:     routing.ConditionHeader,
				Operator: routing.OperatorEquals,
				Key:      "X-API-Version",
				Values:   []string{"v2"},
			},
		},
		Actions: []routing.RuleAction{
			{
				Type:   routing.ActionRoute,
				Target: "api-v2-backend",
			},
		},
	}

	if err := engine.AddRule(rule); err != nil {
		t.Fatalf("Failed to add header rule: %v", err)
	}

	tests := []struct {
		name        string
		headers     http.Header
		shouldMatch bool
	}{
		{
			name: "matching_header",
			headers: http.Header{
				"X-API-Version": []string{"v2"},
			},
			shouldMatch: true,
		},
		{
			name: "non_matching_header_value",
			headers: http.Header{
				"X-API-Version": []string{"v1"},
			},
			shouldMatch: false,
		},
		{
			name:        "missing_header",
			headers:     http.Header{},
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &http.Request{
				Method: "GET",
				Host:   "example.com",
				URL:    &url.URL{Path: "/api/test"},
				Header: tt.headers,
			}

			result := engine.RouteRequest(request)

			if tt.shouldMatch {
				if result.MatchedRule == nil {
					t.Error("Expected header rule to match but no rule matched")
				} else if result.MatchedRule.ID != "header-rule" {
					t.Error("Expected header rule to match but different rule matched")
				}
			} else {
				if result.MatchedRule != nil && result.MatchedRule.ID == "header-rule" {
					t.Error("Expected header rule not to match but it matched")
				}
			}
		})
	}
}

// TestRouteEngine_RulePriority tests rule priority ordering
func TestRouteEngine_RulePriority(t *testing.T) {
	logger, _ := logger.New(logger.Config{Level: "info", Format: "json", Output: "stdout"})
	engine := routing.NewRouteEngine(logger)

	// Add rules in random order to test priority sorting
	rules := []routing.RoutingRule{
		{
			ID:       "low-priority",
			Name:     "Low Priority Rule",
			Priority: 10,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{
					Type:     routing.ConditionPath,
					Operator: routing.OperatorPrefix,
					Values:   []string{"/"},
				},
			},
			Actions: []routing.RuleAction{
				{
					Type:   routing.ActionRoute,
					Target: "low-priority-backend",
				},
			},
		},
		{
			ID:       "high-priority",
			Name:     "High Priority Rule",
			Priority: 100,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{
					Type:     routing.ConditionPath,
					Operator: routing.OperatorPrefix,
					Values:   []string{"/"},
				},
			},
			Actions: []routing.RuleAction{
				{
					Type:   routing.ActionRoute,
					Target: "high-priority-backend",
				},
			},
		},
		{
			ID:       "medium-priority",
			Name:     "Medium Priority Rule",
			Priority: 50,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{
					Type:     routing.ConditionPath,
					Operator: routing.OperatorPrefix,
					Values:   []string{"/"},
				},
			},
			Actions: []routing.RuleAction{
				{
					Type:   routing.ActionRoute,
					Target: "medium-priority-backend",
				},
			},
		},
	}

	// Add rules in reverse order
	for i := len(rules) - 1; i >= 0; i-- {
		if err := engine.AddRule(rules[i]); err != nil {
			t.Fatalf("Failed to add rule %s: %v", rules[i].ID, err)
		}
	}

	request := &http.Request{
		Method: "GET",
		Host:   "example.com",
		URL:    &url.URL{Path: "/test"},
	}

	result := engine.RouteRequest(request)

	// Highest priority rule should match first
	if result.MatchedRule == nil {
		t.Fatal("Expected a rule to match but no rule matched")
	}

	if result.MatchedRule.ID != "high-priority" {
		t.Errorf("Expected high-priority rule to match, got %s", result.MatchedRule.ID)
	}

	if result.Actions[0].Target != "high-priority-backend" {
		t.Errorf("Expected high-priority-backend, got %s", result.Actions[0].Target)
	}
}

// Helper functions for test data generation

func formatRuleID(prefix string, index int) string {
	return prefix + "-" + formatNumber(index)
}

func formatRuleName(prefix string, index int) string {
	return prefix + " " + formatNumber(index)
}

func formatPath(prefix string, index int) string {
	return prefix + formatNumber(index)
}

func formatBackend(prefix string, index int) string {
	return prefix + "-" + formatNumber(index)
}

func formatNumber(n int) string {
	return string(rune('0' + n%10)) // Simple number formatting for testing
}

// Benchmark tests for performance measurement

func BenchmarkRouteEngine_AddRule(b *testing.B) {
	logger, _ := logger.New(logger.Config{Level: "error", Format: "json", Output: "stdout"})
	engine := routing.NewRouteEngine(logger)

	rule := routing.RoutingRule{
		ID:       "bench-rule",
		Name:     "Benchmark Rule",
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
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rule.ID = formatRuleID("bench-rule", i)
		if err := engine.AddRule(rule); err != nil {
			b.Fatalf("Failed to add rule: %v", err)
		}
	}
}

func BenchmarkRouteEngine_RouteRequest(b *testing.B) {
	logger, _ := logger.New(logger.Config{Level: "error", Format: "json", Output: "stdout"})
	engine := routing.NewRouteEngine(logger)

	// Add test rule
	rule := routing.RoutingRule{
		ID:       "bench-route-rule",
		Name:     "Benchmark Route Rule",
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
	}

	if err := engine.AddRule(rule); err != nil {
		b.Fatalf("Failed to add rule: %v", err)
	}

	request := &http.Request{
		Method: "GET",
		Host:   "example.com",
		URL:    &url.URL{Path: "/api/users"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := engine.RouteRequest(request)
		if result.MatchedRule == nil {
			b.Error("Expected rule to match")
		}
	}
}
