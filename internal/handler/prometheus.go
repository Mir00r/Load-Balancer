package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// PrometheusHandler provides Prometheus-compatible metrics endpoint
type PrometheusHandler struct {
	loadBalancer domain.LoadBalancer
	metrics      domain.Metrics
	logger       *logger.Logger
	startTime    time.Time
}

// NewPrometheusHandler creates a new Prometheus metrics handler
func NewPrometheusHandler(loadBalancer domain.LoadBalancer, metrics domain.Metrics, logger *logger.Logger) *PrometheusHandler {
	return &PrometheusHandler{
		loadBalancer: loadBalancer,
		metrics:      metrics,
		logger:       logger,
		startTime:    time.Now(),
	}
}

// MetricsHandler serves Prometheus-formatted metrics
func (h *PrometheusHandler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	// Get current metrics for all backends
	allBackendStats := h.metrics.GetAllBackendStats()

	// Write Prometheus metrics
	fmt.Fprintf(w, "# HELP load_balancer_requests_total Total number of requests processed\n")
	fmt.Fprintf(w, "# TYPE load_balancer_requests_total counter\n")

	fmt.Fprintf(w, "# HELP load_balancer_request_duration_seconds Request duration in seconds\n")
	fmt.Fprintf(w, "# TYPE load_balancer_request_duration_seconds histogram\n")

	fmt.Fprintf(w, "# HELP load_balancer_errors_total Total number of errors\n")
	fmt.Fprintf(w, "# TYPE load_balancer_errors_total counter\n")

	fmt.Fprintf(w, "# HELP load_balancer_backend_status Backend health status (1=healthy, 0=unhealthy)\n")
	fmt.Fprintf(w, "# TYPE load_balancer_backend_status gauge\n")

	fmt.Fprintf(w, "# HELP load_balancer_backend_success_rate Backend success rate percentage\n")
	fmt.Fprintf(w, "# TYPE load_balancer_backend_success_rate gauge\n")

	fmt.Fprintf(w, "# HELP load_balancer_uptime_seconds Load balancer uptime in seconds\n")
	fmt.Fprintf(w, "# TYPE load_balancer_uptime_seconds gauge\n")

	// Get backend information from load balancer
	allBackends := h.loadBalancer.GetBackends()
	healthyBackends := h.loadBalancer.GetHealthyBackends()

	// Create a map for quick lookup of healthy backends
	healthyBackendMap := make(map[string]bool)
	for _, backend := range healthyBackends {
		healthyBackendMap[backend.ID] = true
	}

	// Process backend stats
	for backendID, stats := range allBackendStats {
		backendIDLabel := h.sanitizeLabel(backendID)

		// Find backend URL
		var backendURL string
		for _, backend := range allBackends {
			if backend.ID == backendID {
				backendURL = backend.URL
				break
			}
		}
		backendURLLabel := h.sanitizeLabel(backendURL)

		// Extract metric values safely
		requests := h.getInt64Value(stats, "requests")
		errors := h.getInt64Value(stats, "errors")
		successRate := h.getFloat64Value(stats, "success_rate")
		avgLatencyMs := h.getFloat64Value(stats, "avg_latency_ms")

		// Request totals
		fmt.Fprintf(w, "load_balancer_requests_total{backend_id=\"%s\",backend_url=\"%s\"} %d\n",
			backendIDLabel, backendURLLabel, requests)

		// Error totals
		fmt.Fprintf(w, "load_balancer_errors_total{backend_id=\"%s\",backend_url=\"%s\"} %d\n",
			backendIDLabel, backendURLLabel, errors)

		// Backend status (1 for healthy, 0 for unhealthy)
		statusValue := 0
		if healthyBackendMap[backendID] {
			statusValue = 1
		}
		fmt.Fprintf(w, "load_balancer_backend_status{backend_id=\"%s\",backend_url=\"%s\"} %d\n",
			backendIDLabel, backendURLLabel, statusValue)

		// Success rate
		fmt.Fprintf(w, "load_balancer_backend_success_rate{backend_id=\"%s\",backend_url=\"%s\"} %.2f\n",
			backendIDLabel, backendURLLabel, successRate)

		// Average response time (convert from milliseconds to seconds)
		avgResponseTimeSeconds := avgLatencyMs / 1000.0
		fmt.Fprintf(w, "load_balancer_request_duration_seconds{backend_id=\"%s\",backend_url=\"%s\",quantile=\"0.5\"} %.6f\n",
			backendIDLabel, backendURLLabel, avgResponseTimeSeconds)
	}

	// Overall uptime
	uptime := time.Since(h.startTime).Seconds()
	fmt.Fprintf(w, "load_balancer_uptime_seconds %.2f\n", uptime)

	// Total backends
	fmt.Fprintf(w, "# HELP load_balancer_backends_total Total number of configured backends\n")
	fmt.Fprintf(w, "# TYPE load_balancer_backends_total gauge\n")
	fmt.Fprintf(w, "load_balancer_backends_total %d\n", len(allBackends))

	fmt.Fprintf(w, "# HELP load_balancer_backends_healthy Number of healthy backends\n")
	fmt.Fprintf(w, "# TYPE load_balancer_backends_healthy gauge\n")
	fmt.Fprintf(w, "load_balancer_backends_healthy %d\n", len(healthyBackends))

	// Overall metrics
	overallStats := h.metrics.GetStats()
	totalRequests := h.getInt64Value(overallStats, "total_requests")
	totalErrors := h.getInt64Value(overallStats, "total_errors")
	overallSuccessRate := h.getFloat64Value(overallStats, "overall_success_rate")

	fmt.Fprintf(w, "# HELP load_balancer_total_requests_sum Total requests across all backends\n")
	fmt.Fprintf(w, "# TYPE load_balancer_total_requests_sum counter\n")
	fmt.Fprintf(w, "load_balancer_total_requests_sum %d\n", totalRequests)

	fmt.Fprintf(w, "# HELP load_balancer_total_errors_sum Total errors across all backends\n")
	fmt.Fprintf(w, "# TYPE load_balancer_total_errors_sum counter\n")
	fmt.Fprintf(w, "load_balancer_total_errors_sum %d\n", totalErrors)

	fmt.Fprintf(w, "# HELP load_balancer_overall_success_rate Overall success rate percentage\n")
	fmt.Fprintf(w, "# TYPE load_balancer_overall_success_rate gauge\n")
	fmt.Fprintf(w, "load_balancer_overall_success_rate %.2f\n", overallSuccessRate)

	// Memory and performance metrics (if available)
	h.writeGoMetrics(w)

	h.logger.WithField("component", "prometheus").Debug("Served Prometheus metrics")
}

// Helper methods to safely extract values from interface{} maps
func (h *PrometheusHandler) getInt64Value(data map[string]interface{}, key string) int64 {
	if val, ok := data[key]; ok {
		switch v := val.(type) {
		case int64:
			return v
		case int:
			return int64(v)
		case float64:
			return int64(v)
		}
	}
	return 0
}

func (h *PrometheusHandler) getFloat64Value(data map[string]interface{}, key string) float64 {
	if val, ok := data[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case int64:
			return float64(v)
		case int:
			return float64(v)
		}
	}
	return 0.0
}

// writeGoMetrics writes Go runtime metrics in Prometheus format
func (h *PrometheusHandler) writeGoMetrics(w http.ResponseWriter) {
	// These would typically come from runtime.ReadMemStats()
	// For now, we'll include basic placeholders
	fmt.Fprintf(w, "# HELP go_info Information about the Go environment\n")
	fmt.Fprintf(w, "# TYPE go_info gauge\n")
	fmt.Fprintf(w, "go_info{version=\"go1.21\"} 1\n")

	fmt.Fprintf(w, "# HELP process_start_time_seconds Start time of the process since unix epoch in seconds\n")
	fmt.Fprintf(w, "# TYPE process_start_time_seconds gauge\n")
	fmt.Fprintf(w, "process_start_time_seconds %d\n", h.startTime.Unix())
}

// sanitizeLabel sanitizes metric label values for Prometheus
func (h *PrometheusHandler) sanitizeLabel(value string) string {
	// Escape quotes and backslashes
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}

// HealthCheckMetricsHandler provides detailed health check metrics
func (h *PrometheusHandler) HealthCheckMetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	allBackendStats := h.metrics.GetAllBackendStats()

	fmt.Fprintf(w, "# HELP load_balancer_health_check_success_total Total successful health checks\n")
	fmt.Fprintf(w, "# TYPE load_balancer_health_check_success_total counter\n")

	fmt.Fprintf(w, "# HELP load_balancer_health_check_failure_total Total failed health checks\n")
	fmt.Fprintf(w, "# TYPE load_balancer_health_check_failure_total counter\n")

	fmt.Fprintf(w, "# HELP load_balancer_health_check_duration_seconds Health check duration in seconds\n")
	fmt.Fprintf(w, "# TYPE load_balancer_health_check_duration_seconds gauge\n")

	// Get backend information from load balancer
	allBackends := h.loadBalancer.GetBackends()

	for backendID, stats := range allBackendStats {
		backendIDLabel := h.sanitizeLabel(backendID)

		// Find backend URL
		var backendURL string
		for _, backend := range allBackends {
			if backend.ID == backendID {
				backendURL = backend.URL
				break
			}
		}
		backendURLLabel := h.sanitizeLabel(backendURL)

		// Extract metric values safely
		requests := h.getInt64Value(stats, "requests")
		errors := h.getInt64Value(stats, "errors")
		avgLatencyMs := h.getFloat64Value(stats, "avg_latency_ms")

		// Health check success count (derived from request count - error count)
		successCount := requests - errors
		fmt.Fprintf(w, "load_balancer_health_check_success_total{backend_id=\"%s\",backend_url=\"%s\"} %d\n",
			backendIDLabel, backendURLLabel, successCount)

		// Health check failure count
		fmt.Fprintf(w, "load_balancer_health_check_failure_total{backend_id=\"%s\",backend_url=\"%s\"} %d\n",
			backendIDLabel, backendURLLabel, errors)

		// Last health check duration (convert from milliseconds to seconds)
		lastCheckDurationSeconds := avgLatencyMs / 1000.0
		fmt.Fprintf(w, "load_balancer_health_check_duration_seconds{backend_id=\"%s\",backend_url=\"%s\"} %.6f\n",
			backendIDLabel, backendURLLabel, lastCheckDurationSeconds)
	}
} // CustomMetricsHandler allows adding custom application metrics
func (h *PrometheusHandler) CustomMetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	// Custom business metrics
	fmt.Fprintf(w, "# HELP load_balancer_custom_metric_example Example custom metric\n")
	fmt.Fprintf(w, "# TYPE load_balancer_custom_metric_example gauge\n")
	fmt.Fprintf(w, "load_balancer_custom_metric_example 42\n")

	// Add more custom metrics as needed
}

// ScrapeConfigHandler provides a Prometheus scrape configuration
func (h *PrometheusHandler) ScrapeConfigHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/yaml")

	config := `
# Prometheus scrape configuration for Load Balancer
scrape_configs:
  - job_name: 'load-balancer'
    static_configs:
      - targets: ['localhost:8080']
    scrape_interval: 15s
    metrics_path: /metrics
    scrape_timeout: 10s
    
  - job_name: 'load-balancer-health'
    static_configs:
      - targets: ['localhost:8080']
    scrape_interval: 30s
    metrics_path: /metrics/health
    scrape_timeout: 10s
`

	fmt.Fprint(w, config)
}
