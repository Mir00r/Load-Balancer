package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/service"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// LoadBalancerHandler handles HTTP requests and forwards them to backend servers
type LoadBalancerHandler struct {
	loadBalancer *service.LoadBalancer
	metrics      domain.Metrics
	logger       *logger.Logger
	config       domain.LoadBalancerConfig
}

// NewLoadBalancerHandler creates a new load balancer handler
func NewLoadBalancerHandler(
	loadBalancer *service.LoadBalancer,
	metrics domain.Metrics,
	logger *logger.Logger,
	config domain.LoadBalancerConfig,
) *LoadBalancerHandler {
	return &LoadBalancerHandler{
		loadBalancer: loadBalancer,
		metrics:      metrics,
		logger:       logger,
		config:       config,
	}
}

// ServeHTTP handles incoming HTTP requests
func (h *LoadBalancerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Get request context
	requestCtx, ok := r.Context().Value("requestContext").(*domain.RequestContext)
	if !ok {
		requestCtx = domain.NewRequestContext(r)
	}

	log := h.logger.RequestLogger(
		requestCtx.RequestID,
		requestCtx.Method,
		requestCtx.Path,
		requestCtx.RemoteAddr,
	)

	// Try to get a backend with retries
	var backend *domain.Backend
	var err error

	for attempt := 0; attempt <= h.config.MaxRetries; attempt++ {
		backend, err = h.loadBalancer.GetBackend(r.Context())
		if err != nil {
			if attempt == h.config.MaxRetries {
				log.WithError(err).Error("Failed to get backend after retries")
				h.metrics.IncrementErrors("load_balancer")
				http.Error(w, "No healthy backends available", http.StatusServiceUnavailable)
				return
			}

			log.WithFields(map[string]interface{}{
				"attempt": attempt + 1,
				"error":   err.Error(),
			}).Warn("Failed to get backend, retrying")

			// Wait before retry
			time.Sleep(h.config.RetryDelay)
			continue
		}
		break
	}

	requestCtx.BackendID = backend.ID
	requestCtx.Retries = 0 // Reset for backend-specific retries

	log = log.BackendLogger(backend.ID, backend.URL)
	log.Debug("Selected backend for request")

	// Increment connection count
	backend.IncrementConnections()
	defer backend.DecrementConnections()

	// Increment request count
	backend.IncrementRequests()
	h.metrics.IncrementRequests(backend.ID)

	// Forward request with retries
	success := false
	for attempt := 0; attempt <= h.config.MaxRetries; attempt++ {
		if h.forwardRequest(w, r, backend, log) {
			success = true
			break
		}

		requestCtx.Retries = attempt + 1

		if attempt < h.config.MaxRetries {
			log.WithField("attempt", attempt+1).Warn("Request failed, retrying")
			time.Sleep(h.config.RetryDelay)
		}
	}

	duration := time.Since(start)
	h.metrics.RecordLatency(backend.ID, duration)

	if !success {
		log.Error("Request failed after all retries")
		backend.IncrementFailures()
		h.metrics.IncrementErrors(backend.ID)
		http.Error(w, "Backend request failed", http.StatusBadGateway)
	}
}

// forwardRequest forwards a request to a specific backend
func (h *LoadBalancerHandler) forwardRequest(w http.ResponseWriter, r *http.Request, backend *domain.Backend, log *logger.Logger) bool {
	// Parse backend URL
	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		log.WithError(err).Error("Failed to parse backend URL")
		return false
	}

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(backendURL)

	// Customize the proxy director
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Add load balancer headers
		req.Header.Set("X-Forwarded-By", "LoadBalancer/1.0")
		req.Header.Set("X-Backend-ID", backend.ID)

		// Preserve original host if needed
		if req.Header.Get("X-Forwarded-Host") == "" {
			req.Header.Set("X-Forwarded-Host", req.Host)
		}

		log.WithFields(map[string]interface{}{
			"target_url":     req.URL.String(),
			"method":         req.Method,
			"content_length": req.ContentLength,
		}).Debug("Forwarding request to backend")
	}

	// Customize error handling
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.WithError(err).Error("Backend request failed")
		backend.IncrementFailures()
		h.metrics.IncrementErrors(backend.ID)

		// Don't write error response here, let the caller handle retries
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(r.Context(), backend.Timeout)
	defer cancel()

	// Update request with timeout context
	r = r.WithContext(ctx)

	// Capture response for error checking
	responseRecorder := &responseRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	// Forward the request
	proxy.ServeHTTP(responseRecorder, r)

	// Check if request was successful
	if responseRecorder.statusCode >= 400 {
		log.WithField("status_code", responseRecorder.statusCode).
			Warn("Backend returned error response")

		if responseRecorder.statusCode >= 500 {
			backend.IncrementFailures()
			h.metrics.IncrementErrors(backend.ID)
			return false
		}
	}

	log.WithField("status_code", responseRecorder.statusCode).
		Debug("Request forwarded successfully")

	return true
}

// responseRecorder captures response details
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

// WriteHeader captures the status code
func (rr *responseRecorder) WriteHeader(code int) {
	if !rr.written {
		rr.statusCode = code
		rr.written = true
		rr.ResponseWriter.WriteHeader(code)
	}
}

// Write ensures WriteHeader is called
func (rr *responseRecorder) Write(b []byte) (int, error) {
	if !rr.written {
		rr.WriteHeader(http.StatusOK)
	}
	return rr.ResponseWriter.Write(b)
}

// HealthHandler provides health check endpoint
func (h *LoadBalancerHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	backends := h.loadBalancer.GetBackends()
	healthyBackends := h.loadBalancer.GetHealthyBackends()

	status := "healthy"
	statusCode := http.StatusOK

	if len(healthyBackends) == 0 {
		status = "unhealthy"
		statusCode = http.StatusServiceUnavailable
	} else if len(healthyBackends) < len(backends)/2 {
		status = "degraded"
		statusCode = http.StatusOK // Still operational but degraded
	}

	response := map[string]interface{}{
		"status":           status,
		"timestamp":        time.Now().UTC(),
		"total_backends":   len(backends),
		"healthy_backends": len(healthyBackends),
		"load_balancer":    h.loadBalancer.GetStats(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// MetricsHandler provides metrics endpoint
func (h *LoadBalancerHandler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	metrics := h.metrics.GetStats()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(metrics)
}

// BackendsHandler provides backend management endpoint
func (h *LoadBalancerHandler) BackendsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listBackends(w, r)
	case http.MethodPost:
		h.addBackend(w, r)
	case http.MethodPut:
		h.updateBackend(w, r)
	case http.MethodDelete:
		h.removeBackend(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listBackends returns all backends
func (h *LoadBalancerHandler) listBackends(w http.ResponseWriter, r *http.Request) {
	backends := h.loadBalancer.GetBackends()

	backendDetails := make([]map[string]interface{}, len(backends))
	for i, backend := range backends {
		backendDetails[i] = map[string]interface{}{
			"id":                 backend.ID,
			"url":                backend.URL,
			"weight":             backend.Weight,
			"status":             backend.GetStatus().String(),
			"active_connections": backend.GetActiveConnections(),
			"total_requests":     backend.GetTotalRequests(),
			"failure_count":      backend.GetFailureCount(),
			"last_health_check":  backend.GetLastHealthCheck(),
			"metrics":            h.metrics.GetBackendStats(backend.ID),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(backendDetails)
}

// addBackend adds a new backend
func (h *LoadBalancerHandler) addBackend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID              string        `json:"id"`
		URL             string        `json:"url"`
		Weight          int           `json:"weight"`
		HealthCheckPath string        `json:"health_check_path"`
		MaxConnections  int           `json:"max_connections"`
		Timeout         time.Duration `json:"timeout"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	backend := domain.NewBackend(req.ID, req.URL, req.Weight)
	if req.HealthCheckPath != "" {
		backend.HealthCheckPath = req.HealthCheckPath
	}
	if req.MaxConnections > 0 {
		backend.MaxConnections = req.MaxConnections
	}
	if req.Timeout > 0 {
		backend.Timeout = req.Timeout
	}

	if err := h.loadBalancer.AddBackend(backend); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "backend added"})
}

// removeBackend removes a backend
func (h *LoadBalancerHandler) removeBackend(w http.ResponseWriter, r *http.Request) {
	// Extract backend ID from URL path or query parameter
	path := strings.TrimPrefix(r.URL.Path, "/backends/")
	backendID := strings.Split(path, "/")[0]

	if backendID == "" {
		backendID = r.URL.Query().Get("id")
	}

	if backendID == "" {
		http.Error(w, "Backend ID required", http.StatusBadRequest)
		return
	}

	if err := h.loadBalancer.RemoveBackend(backendID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "backend removed"})
}

// updateBackend updates a backend configuration
func (h *LoadBalancerHandler) updateBackend(w http.ResponseWriter, r *http.Request) {
	// For simplicity, this would be implemented similar to addBackend
	// but would update existing backend configuration
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

// StatusHandler provides a simple status endpoint
func (h *LoadBalancerHandler) StatusHandler(w http.ResponseWriter, r *http.Request) {
	stats := h.loadBalancer.GetStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "running",
		"timestamp": time.Now().UTC(),
		"stats":     stats,
	})
}
