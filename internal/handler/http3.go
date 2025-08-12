package handler

import (
	"github.com/mir00r/load-balancer/internal/config"
	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// HTTP3Handler implements HTTP/3 with QUIC protocol support
// This handler provides modern HTTP/3 capabilities with enhanced performance
// Currently disabled due to dependency issues - to enable, install QUIC library
type HTTP3Handler struct {
	config       config.HTTP3Config
	logger       *logger.Logger
	loadBalancer domain.LoadBalancer
	metrics      domain.Metrics
	enabled      bool
}

// NewHTTP3Handler creates a new HTTP/3 handler
func NewHTTP3Handler(cfg config.HTTP3Config, lb domain.LoadBalancer, metrics domain.Metrics, logger *logger.Logger) *HTTP3Handler {
	logger.Warn("HTTP/3 support is currently disabled due to missing QUIC library dependencies")
	logger.Info("To enable HTTP/3, install: go get github.com/quic-go/quic-go github.com/quic-go/quic-go/http3")

	return &HTTP3Handler{
		config:       cfg,
		logger:       logger,
		loadBalancer: lb,
		metrics:      metrics,
		enabled:      false,
	}
}

// Start starts the HTTP/3 server (currently disabled)
func (h *HTTP3Handler) Start() error {
	h.logger.Info("HTTP/3 server start skipped - feature currently disabled")
	return nil
}

// Stop stops the HTTP/3 server
func (h *HTTP3Handler) Stop() error {
	return nil
}

// IsEnabled returns whether HTTP/3 is enabled
func (h *HTTP3Handler) IsEnabled() bool {
	return h.enabled
}

// GetStats returns HTTP/3 handler statistics
func (h *HTTP3Handler) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"enabled":         false,
		"status":          "disabled",
		"reason":          "missing_quic_library",
		"install_command": "go get github.com/quic-go/quic-go github.com/quic-go/quic-go/http3",
		"configured_port": h.config.Port,
	}
}
