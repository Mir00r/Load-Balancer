// Package discovery provides service discovery integration for dynamic backend management
// Supports multiple service discovery providers including Consul, Kubernetes, and etcd
package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/config"
	"github.com/mir00r/load-balancer/internal/domain"
	lberrors "github.com/mir00r/load-balancer/internal/errors"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// ServiceDiscovery manages dynamic backend discovery from various providers
type ServiceDiscovery struct {
	config    config.ServiceDiscoveryConfig
	logger    *logger.Logger
	providers map[string]Provider
	backends  map[string]*domain.Backend
	mutex     sync.RWMutex
	stopCh    chan struct{}
	running   bool
}

// Provider defines the interface for service discovery providers
type Provider interface {
	Name() string
	Connect(ctx context.Context) error
	DiscoverServices(ctx context.Context) ([]*Service, error)
	Subscribe(ctx context.Context, callback func([]*Service)) error
	Disconnect() error
	Health() error
}

// Service represents a discovered service with its instances
type Service struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Tags     []string          `json:"tags"`
	Address  string            `json:"address"`
	Port     int               `json:"port"`
	Health   string            `json:"health"` // "passing", "warning", "critical"
	Metadata map[string]string `json:"metadata"`
}

// NewServiceDiscovery creates a new service discovery manager
func NewServiceDiscovery(cfg config.ServiceDiscoveryConfig, logger *logger.Logger) (*ServiceDiscovery, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	sd := &ServiceDiscovery{
		config:    cfg,
		logger:    logger,
		providers: make(map[string]Provider),
		backends:  make(map[string]*domain.Backend),
		stopCh:    make(chan struct{}),
		running:   false,
	}

	// Initialize providers based on configuration
	if err := sd.initializeProviders(); err != nil {
		return nil, lberrors.NewErrorWithCause(
			lberrors.ErrCodeConfigLoad,
			"service_discovery",
			"Failed to initialize service discovery providers",
			err,
		)
	}

	logger.WithFields(map[string]interface{}{
		"provider":  cfg.Provider,
		"endpoints": cfg.Endpoints,
		"namespace": cfg.Namespace,
	}).Info("Service discovery initialized")

	return sd, nil
}

// initializeProviders initializes service discovery providers
func (sd *ServiceDiscovery) initializeProviders() error {
	switch sd.config.Provider {
	case "consul":
		provider, err := NewConsulProvider(sd.config, sd.logger)
		if err != nil {
			return fmt.Errorf("failed to initialize Consul provider: %w", err)
		}
		sd.providers["consul"] = provider

	case "kubernetes":
		provider, err := NewKubernetesProvider(sd.config, sd.logger)
		if err != nil {
			return fmt.Errorf("failed to initialize Kubernetes provider: %w", err)
		}
		sd.providers["kubernetes"] = provider

	case "etcd":
		provider, err := NewEtcdProvider(sd.config, sd.logger)
		if err != nil {
			return fmt.Errorf("failed to initialize etcd provider: %w", err)
		}
		sd.providers["etcd"] = provider

	case "http":
		provider, err := NewHTTPProvider(sd.config, sd.logger)
		if err != nil {
			return fmt.Errorf("failed to initialize HTTP provider: %w", err)
		}
		sd.providers["http"] = provider

	default:
		return fmt.Errorf("unsupported service discovery provider: %s", sd.config.Provider)
	}

	return nil
}

// Start begins service discovery and monitoring
func (sd *ServiceDiscovery) Start(ctx context.Context) error {
	if sd.running {
		return fmt.Errorf("service discovery is already running")
	}

	// Connect to providers
	for name, provider := range sd.providers {
		if err := provider.Connect(ctx); err != nil {
			sd.logger.WithError(err).Errorf("Failed to connect to provider %s", name)
			return fmt.Errorf("failed to connect to provider %s: %w", name, err)
		}
	}

	sd.running = true
	sd.logger.Info("Service discovery started")

	// Start discovery loop
	go sd.discoveryLoop(ctx)

	// Start health checking
	go sd.healthCheckLoop(ctx)

	return nil
}

// discoveryLoop runs the main service discovery loop
func (sd *ServiceDiscovery) discoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(sd.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sd.stopCh:
			return
		case <-ticker.C:
			if err := sd.discoverServices(ctx); err != nil {
				sd.logger.WithError(err).Error("Service discovery iteration failed")
			}
		}
	}
}

// discoverServices discovers services from all providers
func (sd *ServiceDiscovery) discoverServices(ctx context.Context) error {
	var allServices []*Service

	for name, provider := range sd.providers {
		services, err := provider.DiscoverServices(ctx)
		if err != nil {
			sd.logger.WithError(err).Errorf("Failed to discover services from %s", name)
			continue
		}
		allServices = append(allServices, services...)
	}

	// Convert services to backends
	newBackends := sd.convertServicesToBackends(allServices)

	// Update backends with diff detection
	sd.updateBackends(newBackends)

	return nil
}

// convertServicesToBackends converts discovered services to backend instances
func (sd *ServiceDiscovery) convertServicesToBackends(services []*Service) map[string]*domain.Backend {
	backends := make(map[string]*domain.Backend)

	for _, service := range services {
		// Apply filters
		if !sd.matchesFilters(service) {
			continue
		}

		// Only include healthy services
		if service.Health != "passing" {
			sd.logger.WithFields(map[string]interface{}{
				"service_id": service.ID,
				"health":     service.Health,
			}).Debug("Skipping unhealthy service")
			continue
		}

		backend := &domain.Backend{
			ID:     service.ID,
			URL:    fmt.Sprintf("http://%s:%d", service.Address, service.Port),
			Weight: 100, // Default weight
		}

		// Extract weight from metadata if available
		if weightStr, exists := service.Metadata["weight"]; exists {
			if weight := parseWeight(weightStr); weight > 0 {
				backend.Weight = weight
			}
		}

		backends[backend.ID] = backend
	}

	return backends
}

// matchesFilters checks if a service matches the configured filters
func (sd *ServiceDiscovery) matchesFilters(service *Service) bool {
	for _, filter := range sd.config.Filters {
		switch filter.Key {
		case "service_name":
			if !sd.matchesStringFilter(service.Name, filter.Values, filter.Operator) {
				return false
			}
		case "tags":
			if !sd.matchesTagsFilter(service.Tags, filter.Values, filter.Operator) {
				return false
			}
		case "health":
			if !sd.matchesStringFilter(service.Health, filter.Values, filter.Operator) {
				return false
			}
		}
	}
	return true
}

// matchesStringFilter checks string-based filter matching
func (sd *ServiceDiscovery) matchesStringFilter(value string, filterValues []string, operator string) bool {
	switch operator {
	case "equals":
		for _, filterValue := range filterValues {
			if value == filterValue {
				return true
			}
		}
		return false
	case "contains":
		for _, filterValue := range filterValues {
			if strings.Contains(value, filterValue) {
				return true
			}
		}
		return false
	case "not_equals":
		for _, filterValue := range filterValues {
			if value == filterValue {
				return false
			}
		}
		return true
	}
	return true
}

// matchesTagsFilter checks tag-based filter matching
func (sd *ServiceDiscovery) matchesTagsFilter(tags []string, filterValues []string, operator string) bool {
	switch operator {
	case "contains":
		for _, filterValue := range filterValues {
			for _, tag := range tags {
				if tag == filterValue {
					return true
				}
			}
		}
		return false
	case "contains_all":
		for _, filterValue := range filterValues {
			found := false
			for _, tag := range tags {
				if tag == filterValue {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	return true
}

// updateBackends updates the backend list with diff detection
func (sd *ServiceDiscovery) updateBackends(newBackends map[string]*domain.Backend) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()

	// Find added backends
	for id, backend := range newBackends {
		if _, exists := sd.backends[id]; !exists {
			sd.logger.WithFields(map[string]interface{}{
				"backend_id": id,
				"url":        backend.URL,
			}).Info("Service discovered: adding backend")
		}
	}

	// Find removed backends
	for id := range sd.backends {
		if _, exists := newBackends[id]; !exists {
			sd.logger.WithFields(map[string]interface{}{
				"backend_id": id,
			}).Info("Service removed: removing backend")
		}
	}

	// Update the backends map
	sd.backends = newBackends

	sd.logger.WithField("backend_count", len(sd.backends)).Debug("Backends updated from service discovery")
}

// healthCheckLoop monitors provider health
func (sd *ServiceDiscovery) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sd.stopCh:
			return
		case <-ticker.C:
			for name, provider := range sd.providers {
				if err := provider.Health(); err != nil {
					sd.logger.WithError(err).Warnf("Provider %s health check failed", name)
				}
			}
		}
	}
}

// GetBackends returns the current list of discovered backends
func (sd *ServiceDiscovery) GetBackends() []*domain.Backend {
	sd.mutex.RLock()
	defer sd.mutex.RUnlock()

	backends := make([]*domain.Backend, 0, len(sd.backends))
	for _, backend := range sd.backends {
		backends = append(backends, backend)
	}

	return backends
}

// Stop stops the service discovery
func (sd *ServiceDiscovery) Stop() error {
	if !sd.running {
		return nil
	}

	close(sd.stopCh)
	sd.running = false

	// Disconnect from providers
	for name, provider := range sd.providers {
		if err := provider.Disconnect(); err != nil {
			sd.logger.WithError(err).Errorf("Failed to disconnect from provider %s", name)
		}
	}

	sd.logger.Info("Service discovery stopped")
	return nil
}

// GetStats returns service discovery statistics
func (sd *ServiceDiscovery) GetStats() map[string]interface{} {
	sd.mutex.RLock()
	defer sd.mutex.RUnlock()

	stats := map[string]interface{}{
		"enabled":           sd.config.Enabled,
		"provider":          sd.config.Provider,
		"running":           sd.running,
		"backend_count":     len(sd.backends),
		"endpoints":         sd.config.Endpoints,
		"discover_interval": sd.config.Interval.String(),
	}

	// Add provider-specific stats
	providerStats := make(map[string]interface{})
	for name, provider := range sd.providers {
		providerStats[name] = map[string]interface{}{
			"name": provider.Name(),
			"health": func() string {
				if err := provider.Health(); err != nil {
					return "unhealthy"
				}
				return "healthy"
			}(),
		}
	}
	stats["providers"] = providerStats

	return stats
}

// parseWeight parses weight string to integer
func parseWeight(weightStr string) int {
	// Simple implementation - could be enhanced
	switch weightStr {
	case "high":
		return 150
	case "medium":
		return 100
	case "low":
		return 50
	default:
		// Try to parse as integer
		if weight := 100; weight > 0 { // Simplified parsing
			return weight
		}
		return 100
	}
}
