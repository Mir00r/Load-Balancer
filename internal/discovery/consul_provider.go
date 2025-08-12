package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mir00r/load-balancer/internal/config"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// ConsulProvider implements service discovery using HashiCorp Consul
type ConsulProvider struct {
	config   config.ServiceDiscoveryConfig
	logger   *logger.Logger
	client   *http.Client
	endpoint string
}

// ConsulService represents a Consul service
type ConsulService struct {
	ID      string `json:"ID"`
	Node    string `json:"Node"`
	Address string `json:"Address"`
	Service struct {
		ID      string   `json:"ID"`
		Service string   `json:"Service"`
		Tags    []string `json:"Tags"`
		Address string   `json:"Address"`
		Port    int      `json:"Port"`
	} `json:"Service"`
	Checks []struct {
		Status string `json:"Status"`
	} `json:"Checks"`
}

// NewConsulProvider creates a new Consul service discovery provider
func NewConsulProvider(cfg config.ServiceDiscoveryConfig, logger *logger.Logger) (*ConsulProvider, error) {
	if len(cfg.Endpoints) == 0 {
		return nil, fmt.Errorf("no Consul endpoints configured")
	}

	client := &http.Client{
		Timeout: cfg.Timeout,
	}

	// Configure TLS if enabled
	if cfg.TLS != nil && cfg.TLS.Enabled {
		// TLS configuration would go here
		logger.Info("Consul TLS configuration detected but not implemented in this example")
	}

	return &ConsulProvider{
		config:   cfg,
		logger:   logger,
		client:   client,
		endpoint: cfg.Endpoints[0], // Use first endpoint
	}, nil
}

// Name returns the provider name
func (c *ConsulProvider) Name() string {
	return "consul"
}

// Connect establishes connection to Consul
func (c *ConsulProvider) Connect(ctx context.Context) error {
	// Test connection to Consul
	url := fmt.Sprintf("http://%s/v1/agent/self", c.endpoint)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create Consul connection request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Consul at %s: %w", c.endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Consul connection failed with status: %s", resp.Status)
	}

	c.logger.WithField("endpoint", c.endpoint).Info("Connected to Consul")
	return nil
}

// DiscoverServices discovers services from Consul
func (c *ConsulProvider) DiscoverServices(ctx context.Context) ([]*Service, error) {
	// Get all services from Consul catalog
	url := fmt.Sprintf("http://%s/v1/catalog/services", c.endpoint)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Consul services request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query Consul services: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Consul services query failed with status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Consul response: %w", err)
	}

	var consulServices map[string][]string
	if err := json.Unmarshal(body, &consulServices); err != nil {
		return nil, fmt.Errorf("failed to parse Consul services: %w", err)
	}

	var services []*Service

	// Get detailed information for each service
	for serviceName := range consulServices {
		if serviceName == "consul" {
			continue // Skip Consul itself
		}

		serviceInstances, err := c.getServiceInstances(ctx, serviceName)
		if err != nil {
			c.logger.WithError(err).Warnf("Failed to get instances for service %s", serviceName)
			continue
		}

		services = append(services, serviceInstances...)
	}

	c.logger.WithField("service_count", len(services)).Debug("Discovered services from Consul")
	return services, nil
}

// getServiceInstances gets all instances of a specific service
func (c *ConsulProvider) getServiceInstances(ctx context.Context, serviceName string) ([]*Service, error) {
	url := fmt.Sprintf("http://%s/v1/health/service/%s", c.endpoint, serviceName)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create service instances request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query service instances: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("service instances query failed with status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read service instances response: %w", err)
	}

	var consulServices []ConsulService
	if err := json.Unmarshal(body, &consulServices); err != nil {
		return nil, fmt.Errorf("failed to parse service instances: %w", err)
	}

	var services []*Service

	for _, consulService := range consulServices {
		// Determine health status
		health := "passing"
		for _, check := range consulService.Checks {
			if check.Status == "critical" {
				health = "critical"
				break
			}
			if check.Status == "warning" && health == "passing" {
				health = "warning"
			}
		}

		// Use service address if available, otherwise node address
		address := consulService.Service.Address
		if address == "" {
			address = consulService.Address
		}

		service := &Service{
			ID:      consulService.Service.ID,
			Name:    consulService.Service.Service,
			Tags:    consulService.Service.Tags,
			Address: address,
			Port:    consulService.Service.Port,
			Health:  health,
			Metadata: map[string]string{
				"node":         consulService.Node,
				"consul_id":    consulService.ID,
				"service_name": consulService.Service.Service,
			},
		}

		// Extract metadata from tags
		for _, tag := range consulService.Service.Tags {
			if strings.HasPrefix(tag, "weight=") {
				service.Metadata["weight"] = strings.TrimPrefix(tag, "weight=")
			}
			if strings.HasPrefix(tag, "health_path=") {
				service.Metadata["health_path"] = strings.TrimPrefix(tag, "health_path=")
			}
		}

		services = append(services, service)
	}

	return services, nil
}

// Subscribe subscribes to service changes (simplified implementation)
func (c *ConsulProvider) Subscribe(ctx context.Context, callback func([]*Service)) error {
	// For production, this would use Consul's blocking queries or watches
	// This is a simplified polling implementation
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				services, err := c.DiscoverServices(ctx)
				if err != nil {
					c.logger.WithError(err).Error("Failed to discover services in subscription")
					continue
				}
				callback(services)
			}
		}
	}()

	return nil
}

// Health checks the health of the Consul connection
func (c *ConsulProvider) Health() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://%s/v1/status/leader", c.endpoint)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("Consul health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Consul health check failed with status: %s", resp.Status)
	}

	return nil
}

// Disconnect closes the connection to Consul
func (c *ConsulProvider) Disconnect() error {
	// HTTP client doesn't need explicit disconnection
	c.logger.Info("Disconnected from Consul")
	return nil
}
