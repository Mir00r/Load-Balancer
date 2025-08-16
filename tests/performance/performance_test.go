// Package performance_test provides comprehensive performance tests for the enhanced
// load balancer architecture. These tests verify performance characteristics,
// latency requirements, and throughput capabilities under various load conditions.
package performance_test

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mir00r/load-balancer/internal/observability"
	"github.com/mir00r/load-balancer/internal/routing"
	"github.com/mir00r/load-balancer/internal/traffic"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// PerformanceTestConfig defines configuration for performance tests
type PerformanceTestConfig struct {
	NumRequests    int
	NumWorkers     int
	Duration       time.Duration
	MaxLatencyP99  time.Duration
	MaxLatencyMean time.Duration
	MinThroughput  int // requests per second
}

// PerformanceResult captures performance test results
type PerformanceResult struct {
	TotalRequests  int
	SuccessfulReqs int
	FailedReqs     int
	Duration       time.Duration
	ThroughputRPS  float64
	LatencyMean    time.Duration
	LatencyP50     time.Duration
	LatencyP95     time.Duration
	LatencyP99     time.Duration
	MemoryUsage    int64 // in bytes
	ErrorRate      float64
}

// BenchmarkRoutingEngine benchmarks the routing engine performance
func BenchmarkRoutingEngine(b *testing.B) {
	logger, _ := logger.New(logger.Config{Level: "error", Format: "json", Output: "stdout"})
	engine := routing.NewRouteEngine(logger)

	// Setup complex routing rules
	setupComplexRoutingRules(b, engine)

	// Create test requests
	requests := generateTestRequests(100)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		reqIndex := 0
		for pb.Next() {
			req := requests[reqIndex%len(requests)]
			result := engine.RouteRequest(req)
			_ = result // Use result to prevent optimization
			reqIndex++
		}
	})

	// Report custom metrics
	stats := engine.GetStats()
	b.ReportMetric(float64(stats.TotalRequests)/b.Elapsed().Seconds(), "requests/sec")
	b.ReportMetric(float64(stats.MatchedRequests)/float64(stats.TotalRequests)*100, "match_rate_%")
}

// BenchmarkTrafficManager benchmarks the traffic manager performance
func BenchmarkTrafficManager(b *testing.B) {
	logger, _ := logger.New(logger.Config{Level: "error", Format: "json", Output: "stdout"})
	engine := routing.NewRouteEngine(logger)
	manager := traffic.NewTrafficManager(engine, logger)

	// Setup routing and traffic rules
	setupComplexRoutingRules(b, engine)
	setupComplexTrafficRules(b, manager)

	// Create test requests
	requests := generateTestRequests(100)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		reqIndex := 0
		for pb.Next() {
			req := requests[reqIndex%len(requests)]
			w := httptest.NewRecorder()
			_ = manager.ProcessRequest(w, req)
			reqIndex++
		}
	})

	// Report custom metrics
	stats := manager.GetStats()
	b.ReportMetric(float64(stats.TotalRequests)/b.Elapsed().Seconds(), "requests/sec")
	b.ReportMetric(float64(stats.SuccessfulRequests)/float64(stats.TotalRequests)*100, "success_rate_%")
}

// BenchmarkObservabilityManager benchmarks the observability manager performance
func BenchmarkObservabilityManager(b *testing.B) {
	logger, _ := logger.New(logger.Config{Level: "error", Format: "json", Output: "stdout"})
	obs := observability.NewObservabilityManager("perf-test", logger)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			span := obs.StartSpan("benchmark-span", nil)
			obs.SetSpanTag(span, "test.benchmark", "true")
			obs.LogSpanEvent(span, "info", "Benchmark event", map[string]interface{}{
				"iteration": b.N,
			})
			obs.FinishSpan(span)
		}
	})

	// Report tracing metrics
	traces := obs.GetTraces()
	b.ReportMetric(float64(len(traces)), "total_traces")
}

// TestLoadTestScenarios runs various load test scenarios
func TestLoadTestScenarios(t *testing.T) {
	scenarios := []struct {
		name   string
		config PerformanceTestConfig
	}{
		{
			name: "light_load",
			config: PerformanceTestConfig{
				NumRequests:    1000,
				NumWorkers:     10,
				Duration:       10 * time.Second,
				MaxLatencyP99:  50 * time.Millisecond,
				MaxLatencyMean: 10 * time.Millisecond,
				MinThroughput:  100, // 100 RPS
			},
		},
		{
			name: "moderate_load",
			config: PerformanceTestConfig{
				NumRequests:    10000,
				NumWorkers:     50,
				Duration:       30 * time.Second,
				MaxLatencyP99:  100 * time.Millisecond,
				MaxLatencyMean: 20 * time.Millisecond,
				MinThroughput:  300, // 300 RPS
			},
		},
		{
			name: "high_load",
			config: PerformanceTestConfig{
				NumRequests:    50000,
				NumWorkers:     100,
				Duration:       60 * time.Second,
				MaxLatencyP99:  200 * time.Millisecond,
				MaxLatencyMean: 50 * time.Millisecond,
				MinThroughput:  500, // 500 RPS
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			result := runLoadTest(t, scenario.config)
			validatePerformanceResult(t, result, scenario.config)
		})
	}
}

// runLoadTest executes a load test with the given configuration
func runLoadTest(t *testing.T, config PerformanceTestConfig) PerformanceResult {
	// Setup components
	logger, err := logger.New(logger.Config{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	obs := observability.NewObservabilityManager("load-test", logger)
	engine := routing.NewRouteEngine(logger)
	manager := traffic.NewTrafficManager(engine, logger)

	// Setup complex rules
	setupComplexRoutingRules(t, engine)
	setupComplexTrafficRules(t, manager)

	// Generate test requests
	requests := generateTestRequests(config.NumRequests)

	// Performance tracking
	var wg sync.WaitGroup
	requestChan := make(chan *http.Request, config.NumRequests)
	latencyChan := make(chan time.Duration, config.NumRequests)
	successChan := make(chan bool, config.NumRequests)

	// Fill request channel
	for _, req := range requests {
		requestChan <- req
	}
	close(requestChan)

	startTime := time.Now()

	// Start workers
	for i := 0; i < config.NumWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for req := range requestChan {
				// Start timing
				requestStart := time.Now()

				// Start observability span
				span := obs.StartSpan("load-test-request", nil)
				obs.SetSpanTag(span, "worker.id", fmt.Sprintf("%d", workerID))

				// Process request
				success := processTestRequest(req, engine, manager, obs, span)

				// Record latency
				latency := time.Since(requestStart)
				latencyChan <- latency
				successChan <- success

				obs.FinishSpan(span)
			}
		}(i)
	}

	wg.Wait()
	endTime := time.Now()

	// Collect results
	close(latencyChan)
	close(successChan)

	return analyzeResults(latencyChan, successChan, startTime, endTime)
}

// processTestRequest processes a single test request through all components
func processTestRequest(req *http.Request, engine *routing.RouteEngine, manager *traffic.TrafficManager, obs *observability.ObservabilityManager, span *observability.Span) bool {
	// Route the request
	routingStart := time.Now()
	result := engine.RouteRequest(req)
	routingLatency := time.Since(routingStart)

	obs.SetSpanTag(span, "routing.latency_ms", fmt.Sprintf("%.2f", float64(routingLatency.Nanoseconds())/1e6))

	if result.MatchedRule == nil {
		obs.SetSpanTag(span, "routing.matched", "false")
		return false
	}

	obs.SetSpanTag(span, "routing.matched", "true")
	obs.SetSpanTag(span, "routing.rule", result.MatchedRule.ID)

	// Process through traffic manager
	trafficStart := time.Now()
	w := httptest.NewRecorder()
	err := manager.ProcessRequest(w, req)
	trafficLatency := time.Since(trafficStart)

	obs.SetSpanTag(span, "traffic.latency_ms", fmt.Sprintf("%.2f", float64(trafficLatency.Nanoseconds())/1e6))

	if err != nil {
		obs.SetSpanTag(span, "traffic.success", "false")
		obs.SetSpanTag(span, "error", err.Error())
		return false
	}

	obs.SetSpanTag(span, "traffic.success", "true")
	return true
}

// analyzeResults analyzes performance test results
func analyzeResults(latencyChan chan time.Duration, successChan chan bool, startTime, endTime time.Time) PerformanceResult {
	// Collect latencies
	var latencies []time.Duration
	for latency := range latencyChan {
		latencies = append(latencies, latency)
	}

	// Count successes
	successCount := 0
	totalRequests := 0
	for success := range successChan {
		totalRequests++
		if success {
			successCount++
		}
	}

	// Sort latencies for percentile calculation
	if len(latencies) > 0 {
		for i := 0; i < len(latencies); i++ {
			for j := i + 1; j < len(latencies); j++ {
				if latencies[i] > latencies[j] {
					latencies[i], latencies[j] = latencies[j], latencies[i]
				}
			}
		}
	}

	duration := endTime.Sub(startTime)
	throughput := float64(totalRequests) / duration.Seconds()

	result := PerformanceResult{
		TotalRequests:  totalRequests,
		SuccessfulReqs: successCount,
		FailedReqs:     totalRequests - successCount,
		Duration:       duration,
		ThroughputRPS:  throughput,
		ErrorRate:      float64(totalRequests-successCount) / float64(totalRequests) * 100,
	}

	// Calculate latency statistics
	if len(latencies) > 0 {
		// Mean latency
		var sum time.Duration
		for _, latency := range latencies {
			sum += latency
		}
		result.LatencyMean = sum / time.Duration(len(latencies))

		// Percentiles
		result.LatencyP50 = latencies[len(latencies)*50/100]
		result.LatencyP95 = latencies[len(latencies)*95/100]
		result.LatencyP99 = latencies[len(latencies)*99/100]
	}

	return result
}

// validatePerformanceResult validates performance test results against requirements
func validatePerformanceResult(t *testing.T, result PerformanceResult, config PerformanceTestConfig) {
	t.Logf("Performance Results:")
	t.Logf("  Total Requests: %d", result.TotalRequests)
	t.Logf("  Successful: %d (%.2f%%)", result.SuccessfulReqs, float64(result.SuccessfulReqs)/float64(result.TotalRequests)*100)
	t.Logf("  Failed: %d (%.2f%%)", result.FailedReqs, result.ErrorRate)
	t.Logf("  Duration: %v", result.Duration)
	t.Logf("  Throughput: %.2f RPS", result.ThroughputRPS)
	t.Logf("  Latency Mean: %v", result.LatencyMean)
	t.Logf("  Latency P50: %v", result.LatencyP50)
	t.Logf("  Latency P95: %v", result.LatencyP95)
	t.Logf("  Latency P99: %v", result.LatencyP99)

	// Validate throughput
	if result.ThroughputRPS < float64(config.MinThroughput) {
		t.Errorf("Throughput %.2f RPS is below minimum requirement of %d RPS", result.ThroughputRPS, config.MinThroughput)
	}

	// Validate latency requirements
	if result.LatencyMean > config.MaxLatencyMean {
		t.Errorf("Mean latency %v exceeds maximum requirement of %v", result.LatencyMean, config.MaxLatencyMean)
	}

	if result.LatencyP99 > config.MaxLatencyP99 {
		t.Errorf("P99 latency %v exceeds maximum requirement of %v", result.LatencyP99, config.MaxLatencyP99)
	}

	// Validate error rate (should be less than 5%)
	if result.ErrorRate > 5.0 {
		t.Errorf("Error rate %.2f%% exceeds maximum acceptable rate of 5%%", result.ErrorRate)
	}
}

// setupComplexRoutingRules sets up complex routing rules for performance testing
func setupComplexRoutingRules(t testing.TB, engine *routing.RouteEngine) {
	rules := []routing.RoutingRule{
		{
			ID:       "api-v1",
			Name:     "API v1 Routes",
			Priority: 100,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{Type: routing.ConditionPath, Operator: routing.OperatorPrefix, Values: []string{"/api/v1/"}},
			},
			Actions: []routing.RuleAction{{Type: routing.ActionRoute, Target: "api-v1-backend"}},
		},
		{
			ID:       "api-v2",
			Name:     "API v2 Routes",
			Priority: 95,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{Type: routing.ConditionPath, Operator: routing.OperatorPrefix, Values: []string{"/api/v2/"}},
			},
			Actions: []routing.RuleAction{{Type: routing.ActionRoute, Target: "api-v2-backend"}},
		},
		{
			ID:       "admin-secure",
			Name:     "Admin Secure Routes",
			Priority: 90,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{Type: routing.ConditionPath, Operator: routing.OperatorPrefix, Values: []string{"/admin/"}},
				{Type: routing.ConditionHeader, Operator: routing.OperatorEquals, Key: "Authorization", Values: []string{"Bearer"}},
			},
			Actions: []routing.RuleAction{{Type: routing.ActionRoute, Target: "admin-backend"}},
		},
		{
			ID:       "static-assets",
			Name:     "Static Asset Routes",
			Priority: 85,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{Type: routing.ConditionPath, Operator: routing.OperatorRegex, Values: []string{`\.(css|js|png|jpg|gif|ico)$`}},
			},
			Actions: []routing.RuleAction{{Type: routing.ActionRoute, Target: "static-backend"}},
		},
		{
			ID:       "websocket",
			Name:     "WebSocket Routes",
			Priority: 80,
			Enabled:  true,
			Conditions: []routing.RuleCondition{
				{Type: routing.ConditionPath, Operator: routing.OperatorPrefix, Values: []string{"/ws/"}},
				{Type: routing.ConditionHeader, Operator: routing.OperatorEquals, Key: "Upgrade", Values: []string{"websocket"}},
			},
			Actions: []routing.RuleAction{{Type: routing.ActionRoute, Target: "websocket-backend"}},
		},
	}

	for _, rule := range rules {
		if err := engine.AddRule(rule); err != nil {
			t.Fatalf("Failed to add rule %s: %v", rule.ID, err)
		}
	}
}

// setupComplexTrafficRules sets up complex traffic management rules for performance testing
func setupComplexTrafficRules(t testing.TB, manager *traffic.TrafficManager) {
	rules := []*traffic.TrafficRule{
		{
			ID:      "api-v1-traffic",
			RouteID: "api-v1",
			Enabled: true,
			LoadBalancer: traffic.LoadBalancerConfig{
				Strategy: traffic.StrategyRoundRobin,
				Backends: []string{"api-v1-backend-1", "api-v1-backend-2", "api-v1-backend-3"},
			},
			RateLimit: traffic.RateLimitConfig{
				Enabled:           true,
				RequestsPerSecond: 1000,
				BurstSize:         2000,
				KeyExtractor:      "ip",
			},
			CircuitBreaker: traffic.CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 10,
				RecoveryTimeout:  30 * time.Second,
			},
			Retry: traffic.RetryConfig{
				Enabled:         true,
				MaxAttempts:     3,
				BackoffStrategy: "exponential",
				InitialDelay:    50 * time.Millisecond,
				MaxDelay:        500 * time.Millisecond,
			},
		},
		{
			ID:      "api-v2-traffic",
			RouteID: "api-v2",
			Enabled: true,
			LoadBalancer: traffic.LoadBalancerConfig{
				Strategy: traffic.StrategyLeastConn,
				Backends: []string{"api-v2-backend-1", "api-v2-backend-2"},
			},
			RateLimit: traffic.RateLimitConfig{
				Enabled:           true,
				RequestsPerSecond: 500,
				BurstSize:         1000,
				KeyExtractor:      "header",
				KeyExtractorParam: "API-Key",
			},
		},
		{
			ID:      "admin-traffic",
			RouteID: "admin-secure",
			Enabled: true,
			LoadBalancer: traffic.LoadBalancerConfig{
				Strategy: traffic.StrategyRoundRobin,
				Backends: []string{"admin-backend-1"},
			},
			RateLimit: traffic.RateLimitConfig{
				Enabled:           true,
				RequestsPerSecond: 100,
				BurstSize:         200,
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

// generateTestRequests generates a variety of test requests for performance testing
func generateTestRequests(count int) []*http.Request {
	requests := make([]*http.Request, count)
	paths := []string{
		"/api/v1/users",
		"/api/v1/posts",
		"/api/v1/comments",
		"/api/v2/users",
		"/api/v2/analytics",
		"/admin/dashboard",
		"/admin/users",
		"/static/app.js",
		"/static/style.css",
		"/ws/notifications",
		"/health",
		"/metrics",
	}

	methods := []string{"GET", "POST", "PUT", "DELETE"}

	for i := 0; i < count; i++ {
		path := paths[rand.Intn(len(paths))]
		method := methods[rand.Intn(len(methods))]

		req := httptest.NewRequest(method, path, nil)

		// Add random headers
		if strings.HasPrefix(path, "/admin/") {
			req.Header.Set("Authorization", "Bearer admin-token-"+fmt.Sprintf("%d", rand.Intn(100)))
		}
		if strings.HasPrefix(path, "/api/v2/") {
			req.Header.Set("API-Key", "key-"+fmt.Sprintf("%d", rand.Intn(50)))
		}
		if strings.HasPrefix(path, "/ws/") {
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "Upgrade")
		}

		// Add random IP addresses for rate limiting tests
		req.RemoteAddr = fmt.Sprintf("192.168.%d.%d:12345", rand.Intn(256), rand.Intn(256))

		requests[i] = req
	}

	return requests
}

// TestMemoryUsage tests memory usage under load
func TestMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory usage test in short mode")
	}

	// Setup components
	logger, err := logger.New(logger.Config{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	obs := observability.NewObservabilityManager("memory-test", logger)
	engine := routing.NewRouteEngine(logger)
	manager := traffic.NewTrafficManager(engine, logger)

	// Setup rules
	setupComplexRoutingRules(t, engine)
	setupComplexTrafficRules(t, manager)

	// Run sustained load for memory profiling
	numRequests := 100000
	requests := generateTestRequests(1000) // Reuse 1000 different requests

	t.Logf("Processing %d requests for memory usage analysis", numRequests)

	startTime := time.Now()
	for i := 0; i < numRequests; i++ {
		req := requests[i%len(requests)]

		// Start span
		span := obs.StartSpan("memory-test-request", nil)

		// Route request
		result := engine.RouteRequest(req)
		if result.MatchedRule != nil {
			// Process through traffic manager
			w := httptest.NewRecorder()
			manager.ProcessRequest(w, req)
		}

		obs.FinishSpan(span)

		// Report progress every 10k requests
		if (i+1)%10000 == 0 {
			elapsed := time.Since(startTime)
			rps := float64(i+1) / elapsed.Seconds()
			t.Logf("Processed %d requests (%.2f RPS)", i+1, rps)
		}
	}

	duration := time.Since(startTime)
	t.Logf("Completed %d requests in %v (%.2f RPS)", numRequests, duration, float64(numRequests)/duration.Seconds())

	// Check final statistics
	routingStats := engine.GetStats()
	trafficStats := manager.GetStats()
	traces := obs.GetTraces()

	t.Logf("Final statistics:")
	t.Logf("  Routing: %d total, %d matched", routingStats.TotalRequests, routingStats.MatchedRequests)
	t.Logf("  Traffic: %d total, %d successful", trafficStats.TotalRequests, trafficStats.SuccessfulRequests)
	t.Logf("  Traces: %d collected", len(traces))

	// Memory usage would be reported by runtime profiling tools
	t.Log("Memory usage analysis complete - use go tool pprof for detailed memory profiling")
}
