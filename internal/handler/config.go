package handler

import (
	"encoding/json"
	"net/http"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// ConfigHandler handles configuration reload operations
type ConfigHandler struct {
	loadBalancer domain.LoadBalancer
	logger       *logger.Logger
}

// NewConfigHandler creates a new configuration handler
func NewConfigHandler(loadBalancer domain.LoadBalancer, logger *logger.Logger) *ConfigHandler {
	return &ConfigHandler{
		loadBalancer: loadBalancer,
		logger:       logger,
	}
}

// ReloadConfigHandler handles configuration reload requests
func (ch *ConfigHandler) ReloadConfigHandler(w http.ResponseWriter, r *http.Request) {
	ch.logger.Info("Configuration reload requested")

	// For now, just return success
	// In a full implementation, this would reload from file or accept new config via POST
	response := map[string]interface{}{
		"status":           "success",
		"message":          "Configuration reload not implemented in this version",
		"current_backends": len(ch.loadBalancer.GetBackends()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetConfigHandler returns current configuration status
func (ch *ConfigHandler) GetConfigHandler(w http.ResponseWriter, r *http.Request) {
	backends := ch.loadBalancer.GetBackends()
	healthyBackends := ch.loadBalancer.GetHealthyBackends()

	response := map[string]interface{}{
		"total_backends":   len(backends),
		"healthy_backends": len(healthyBackends),
		"backends":         backends,
		"reload_available": false, // Will be true when dynamic reload is implemented
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
