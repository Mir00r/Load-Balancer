package handler

import (
	"encoding/json"
	"net/http"
	"time"
)

// HealthHandler provides application health check endpoint
// This implements 12-Factor App methodology - Factor #9: Disposability
type HealthHandler struct {
	startTime time.Time
	version   string
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(version string) *HealthHandler {
	return &HealthHandler{
		startTime: time.Now(),
		version:   version,
	}
}

// ReadinessHandler checks if the application is ready to serve traffic
func (h *HealthHandler) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":    "ready",
		"timestamp": time.Now().UTC(),
		"version":   h.version,
		"uptime":    time.Since(h.startTime).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// LivenessHandler checks if the application is alive
func (h *HealthHandler) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":    "alive",
		"timestamp": time.Now().UTC(),
		"version":   h.version,
		"uptime":    time.Since(h.startTime).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
