package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mir00r/load-balancer/internal/config"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// HTTPProvider implements service discovery using HTTP endpoints
type HTTPProvider struct {
	config   config.ServiceDiscoveryConfig
	logger   *logger.Logger
	client   *http.Client
	endpoint string
}

// HTTPServiceResponse represents the expected HTTP response format
type HTTPServiceResponse struct {
	Services []*Service `json:"services"`
}

// NewHTTPProvider creates a new HTTP service discovery provider
func NewHTTPProvider(cfg config.ServiceDiscoveryConfig, logger *logger.Logger) (*HTTPProvider, error) {
	if len(cfg.Endpoints) == 0 {
		return nil, fmt.Errorf("no HTTP endpoints configured")
	}

	client := &http.Client{
		Timeout: cfg.Timeout,
	}

	return &HTTPProvider{
		config:   cfg,
		logger:   logger,
		client:   client,
		endpoint: cfg.Endpoints[0],
	}, nil
}

// Name returns the provider name
func (h *HTTPProvider) Name() string {
	return "http"
}

// Connect establishes connection to HTTP endpoint
func (h *HTTPProvider) Connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", h.endpoint+"/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP connection request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to HTTP endpoint: %w", err)
	}
	defer resp.Body.Close()

	h.logger.WithField("endpoint", h.endpoint).Info("Connected to HTTP service discovery")
	return nil
}

// DiscoverServices discovers services from HTTP endpoint
func (h *HTTPProvider) DiscoverServices(ctx context.Context) ([]*Service, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", h.endpoint+"/services", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create services request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query services: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("services query failed with status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response HTTPServiceResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse services: %w", err)
	}

	return response.Services, nil
}

// Subscribe subscribes to service changes
func (h *HTTPProvider) Subscribe(ctx context.Context, callback func([]*Service)) error {
	// Simple polling implementation
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				services, err := h.DiscoverServices(ctx)
				if err != nil {
					h.logger.WithError(err).Error("Failed to discover services")
					continue
				}
				callback(services)
			}
		}
	}()

	return nil
}

// Health checks the health of the HTTP endpoint
func (h *HTTPProvider) Health() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", h.endpoint+"/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %s", resp.Status)
	}

	return nil
}

// Disconnect closes the connection
func (h *HTTPProvider) Disconnect() error {
	h.logger.Info("Disconnected from HTTP service discovery")
	return nil
}
