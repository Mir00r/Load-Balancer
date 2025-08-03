package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// AdminHandler provides administrative API endpoints
type AdminHandler struct {
	loadBalancer domain.LoadBalancer
	metrics      domain.Metrics
	logger       *logger.Logger
	startTime    time.Time
}

// NewAdminHandler creates a new admin handler
func NewAdminHandler(loadBalancer domain.LoadBalancer, metrics domain.Metrics, logger *logger.Logger) *AdminHandler {
	return &AdminHandler{
		loadBalancer: loadBalancer,
		metrics:      metrics,
		logger:       logger,
		startTime:    time.Now(),
	}
}

// BackendRequest represents a request to add/update a backend
type BackendRequest struct {
	ID              string `json:"id" validate:"required"`
	URL             string `json:"url" validate:"required,url"`
	Weight          int    `json:"weight,omitempty"`
	HealthCheckPath string `json:"health_check_path,omitempty"`
	MaxConnections  int    `json:"max_connections,omitempty"`
	Timeout         string `json:"timeout,omitempty"`
}

// BackendResponse represents backend information in API responses
type BackendResponse struct {
	ID                string    `json:"id"`
	URL               string    `json:"url"`
	Weight            int       `json:"weight"`
	Status            string    `json:"status"`
	HealthCheckPath   string    `json:"health_check_path"`
	MaxConnections    int       `json:"max_connections"`
	Timeout           string    `json:"timeout"`
	ActiveConnections int64     `json:"active_connections"`
	TotalRequests     int64     `json:"total_requests"`
	ErrorCount        int64     `json:"error_count"`
	LastHealthCheck   time.Time `json:"last_health_check"`
}

// HealthResponse represents health check response
type HealthResponse struct {
	Status          string    `json:"status"`
	TotalBackends   int       `json:"total_backends"`
	HealthyBackends int       `json:"healthy_backends"`
	Timestamp       time.Time `json:"timestamp"`
}

// ConfigResponse represents current configuration
type ConfigResponse struct {
	Strategy    string                   `json:"strategy"`
	Port        int                      `json:"port"`
	MaxRetries  int                      `json:"max_retries"`
	RetryDelay  string                   `json:"retry_delay"`
	Timeout     string                   `json:"timeout"`
	HealthCheck domain.HealthCheckConfig `json:"health_check"`
}

// StatsResponse represents comprehensive statistics
type StatsResponse struct {
	TotalRequests       int64                   `json:"total_requests"`
	TotalErrors         int64                   `json:"total_errors"`
	SuccessRate         float64                 `json:"success_rate"`
	AverageResponseTime float64                 `json:"average_response_time_ms"`
	Uptime              string                  `json:"uptime"`
	ActiveConnections   int                     `json:"active_connections"`
	BackendStats        map[string]BackendStats `json:"backend_stats"`
	RequestsPerSecond   float64                 `json:"requests_per_second"`
	ErrorsPerSecond     float64                 `json:"errors_per_second"`
}

// BackendStats represents individual backend statistics
type BackendStats struct {
	Requests            int64   `json:"requests"`
	Errors              int64   `json:"errors"`
	SuccessRate         float64 `json:"success_rate"`
	AverageResponseTime float64 `json:"average_response_time_ms"`
	ActiveConnections   int64   `json:"active_connections"`
}

// ErrorResponse represents error responses
type ErrorResponse struct {
	Error     string    `json:"error"`
	Code      int       `json:"code"`
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"request_id,omitempty"`
}

// ListBackendsHandler handles GET /admin/backends
func (h *AdminHandler) ListBackendsHandler(w http.ResponseWriter, r *http.Request) {
	backends := h.loadBalancer.GetBackends()
	healthyBackends := h.loadBalancer.GetHealthyBackends()

	// Create a map for quick lookup of healthy backends
	healthyMap := make(map[string]bool)
	for _, backend := range healthyBackends {
		healthyMap[backend.ID] = true
	}

	var response []BackendResponse
	for _, backend := range backends {
		// Get backend stats
		stats := h.metrics.GetBackendStats(backend.ID)

		var totalRequests, errorCount int64
		// Since stats is already map[string]interface{}, no need for type assertion
		if req, exists := stats["requests"]; exists {
			if reqInt, ok := req.(int64); ok {
				totalRequests = reqInt
			}
		}
		if err, exists := stats["errors"]; exists {
			if errInt, ok := err.(int64); ok {
				errorCount = errInt
			}
		}

		status := "unhealthy"
		if healthyMap[backend.ID] {
			status = "healthy"
		}

		response = append(response, BackendResponse{
			ID:                backend.ID,
			URL:               backend.URL,
			Weight:            backend.Weight,
			Status:            status,
			HealthCheckPath:   backend.HealthCheckPath,
			MaxConnections:    backend.MaxConnections,
			Timeout:           backend.Timeout.String(),
			ActiveConnections: backend.GetActiveConnections(),
			TotalRequests:     totalRequests,
			ErrorCount:        errorCount,
			LastHealthCheck:   time.Now(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)

	h.logger.WithFields(map[string]interface{}{
		"component": "admin_api",
		"action":    "list_backends",
		"count":     len(response),
	}).Info("Listed backends")
}

// AddBackendHandler handles POST /admin/backends
func (h *AdminHandler) AddBackendHandler(w http.ResponseWriter, r *http.Request) {
	var req BackendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorResponse(w, "Invalid JSON body", http.StatusBadRequest, "")
		return
	}

	// Validate required fields
	if req.ID == "" || req.URL == "" {
		h.writeErrorResponse(w, "ID and URL are required", http.StatusBadRequest, "")
		return
	}

	// Set defaults
	if req.Weight <= 0 {
		req.Weight = 1
	}
	if req.HealthCheckPath == "" {
		req.HealthCheckPath = "/health"
	}
	if req.MaxConnections <= 0 {
		req.MaxConnections = 100
	}

	timeout := 30 * time.Second
	if req.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(req.Timeout); err == nil {
			timeout = parsedTimeout
		}
	}

	// Create backend
	backend := &domain.Backend{
		ID:              req.ID,
		URL:             req.URL,
		Weight:          req.Weight,
		HealthCheckPath: req.HealthCheckPath,
		MaxConnections:  req.MaxConnections,
		Timeout:         timeout,
	}
	backend.SetStatus(domain.StatusHealthy)

	// Add to load balancer
	if err := h.loadBalancer.AddBackend(backend); err != nil {
		if err.Error() == fmt.Sprintf("backend with ID '%s' already exists", req.ID) {
			h.writeErrorResponse(w, err.Error(), http.StatusConflict, "")
		} else {
			h.writeErrorResponse(w, err.Error(), http.StatusInternalServerError, "")
		}
		return
	}

	response := BackendResponse{
		ID:              backend.ID,
		URL:             backend.URL,
		Weight:          backend.Weight,
		Status:          "healthy",
		HealthCheckPath: backend.HealthCheckPath,
		MaxConnections:  backend.MaxConnections,
		Timeout:         backend.Timeout.String(),
		LastHealthCheck: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)

	h.logger.WithFields(map[string]interface{}{
		"component":   "admin_api",
		"action":      "add_backend",
		"backend_id":  req.ID,
		"backend_url": req.URL,
	}).Info("Added backend")
}

// DeleteBackendHandler handles DELETE /admin/backends/{id}
func (h *AdminHandler) DeleteBackendHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	backendID := vars["id"]

	if err := h.loadBalancer.RemoveBackend(backendID); err != nil {
		if err.Error() == fmt.Sprintf("backend with ID '%s' not found", backendID) {
			h.writeErrorResponse(w, err.Error(), http.StatusNotFound, "")
		} else {
			h.writeErrorResponse(w, err.Error(), http.StatusInternalServerError, "")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)

	h.logger.WithFields(map[string]interface{}{
		"component":  "admin_api",
		"action":     "delete_backend",
		"backend_id": backendID,
	}).Info("Deleted backend")
}

// GetConfigHandler handles GET /admin/config
func (h *AdminHandler) GetConfigHandler(w http.ResponseWriter, r *http.Request) {
	response := ConfigResponse{
		Strategy:   "round_robin",
		Port:       8080,
		MaxRetries: 3,
		RetryDelay: "100ms",
		Timeout:    "30s",
		HealthCheck: domain.HealthCheckConfig{
			Enabled:            true,
			Interval:           30 * time.Second,
			Timeout:            5 * time.Second,
			HealthyThreshold:   2,
			UnhealthyThreshold: 3,
			Path:               "/health",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetStatsHandler handles GET /admin/stats
func (h *AdminHandler) GetStatsHandler(w http.ResponseWriter, r *http.Request) {
	overallStats := h.metrics.GetStats()
	allBackendStats := h.metrics.GetAllBackendStats()

	// Parse overall stats - no type assertion needed since it's already map[string]interface{}
	var totalRequests, totalErrors int64
	var successRate float64

	if req, exists := overallStats["total_requests"]; exists {
		if reqInt, ok := req.(int64); ok {
			totalRequests = reqInt
		}
	}
	if err, exists := overallStats["total_errors"]; exists {
		if errInt, ok := err.(int64); ok {
			totalErrors = errInt
		}
	}
	if sr, exists := overallStats["overall_success_rate"]; exists {
		if srFloat, ok := sr.(float64); ok {
			successRate = srFloat
		}
	}

	// Calculate uptime
	uptime := time.Since(h.startTime)

	// Calculate rates (per second)
	uptimeSeconds := uptime.Seconds()
	requestsPerSecond := float64(totalRequests) / uptimeSeconds
	errorsPerSecond := float64(totalErrors) / uptimeSeconds

	// Process backend stats
	backendStats := make(map[string]BackendStats)
	totalActiveConnections := 0

	for backendID, stats := range allBackendStats {
		var requests, errors int64
		var avgLatency float64

		if req, exists := stats["requests"]; exists {
			if reqInt, ok := req.(int64); ok {
				requests = reqInt
			}
		}
		if err, exists := stats["errors"]; exists {
			if errInt, ok := err.(int64); ok {
				errors = errInt
			}
		}
		if lat, exists := stats["avg_latency_ms"]; exists {
			if latFloat, ok := lat.(float64); ok {
				avgLatency = latFloat
			}
		}

		// Calculate backend success rate
		backendSuccessRate := 100.0
		if requests > 0 {
			backendSuccessRate = float64(requests-errors) / float64(requests) * 100
		}

		// Get active connections - we'll estimate from backends
		backends := h.loadBalancer.GetBackends()
		var activeConnections int64
		for _, backend := range backends {
			if backend.ID == backendID {
				activeConnections = backend.GetActiveConnections()
				totalActiveConnections += int(activeConnections)
				break
			}
		}

		backendStats[backendID] = BackendStats{
			Requests:            requests,
			Errors:              errors,
			SuccessRate:         backendSuccessRate,
			AverageResponseTime: avgLatency,
			ActiveConnections:   activeConnections,
		}
	}

	response := StatsResponse{
		TotalRequests:       totalRequests,
		TotalErrors:         totalErrors,
		SuccessRate:         successRate,
		AverageResponseTime: 0, // Would calculate from all backends
		Uptime:              uptime.String(),
		ActiveConnections:   totalActiveConnections,
		BackendStats:        backendStats,
		RequestsPerSecond:   requestsPerSecond,
		ErrorsPerSecond:     errorsPerSecond,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// writeErrorResponse writes a standardized error response
func (h *AdminHandler) writeErrorResponse(w http.ResponseWriter, message string, code int, requestID string) {
	response := ErrorResponse{
		Error:     message,
		Code:      code,
		Timestamp: time.Now(),
		RequestID: requestID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(response)

	h.logger.WithFields(map[string]interface{}{
		"component":  "admin_api",
		"error":      message,
		"code":       code,
		"request_id": requestID,
	}).Error("API error response")
}
