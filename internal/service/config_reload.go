package service

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/config"
	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
	"gopkg.in/yaml.v2"
)

// ConfigReloadService handles dynamic configuration reloading
type ConfigReloadService struct {
	config          *config.Config
	loadBalancer    *LoadBalancer // Use concrete type to access SetStrategy
	configFilePath  string
	logger          *logger.Logger
	mutex           sync.RWMutex
	reloadCallbacks []func(*config.Config) error
	watcherStop     chan struct{}
	lastModTime     time.Time
}

// NewConfigReloadService creates a new configuration reload service
func NewConfigReloadService(
	cfg *config.Config,
	loadBalancer *LoadBalancer, // Use concrete type
	configFilePath string,
	logger *logger.Logger,
) *ConfigReloadService {
	return &ConfigReloadService{
		config:          cfg,
		loadBalancer:    loadBalancer,
		configFilePath:  configFilePath,
		logger:          logger,
		reloadCallbacks: make([]func(*config.Config) error, 0),
		watcherStop:     make(chan struct{}),
	}
}

// RegisterReloadCallback registers a callback to be called when config is reloaded
func (crs *ConfigReloadService) RegisterReloadCallback(callback func(*config.Config) error) {
	crs.mutex.Lock()
	defer crs.mutex.Unlock()
	crs.reloadCallbacks = append(crs.reloadCallbacks, callback)
}

// StartWatcher starts the configuration file watcher
func (crs *ConfigReloadService) StartWatcher() error {
	// Check if config file exists
	_, err := ioutil.ReadFile(crs.configFilePath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Simple file watching - check modification time periodically
	go crs.watchConfigFile()

	crs.logger.WithFields(map[string]interface{}{
		"config_file": crs.configFilePath,
	}).Info("Started configuration file watcher")

	return nil
}

// StopWatcher stops the configuration file watcher
func (crs *ConfigReloadService) StopWatcher() {
	close(crs.watcherStop)
	crs.logger.Info("Stopped configuration file watcher")
}

// watchConfigFile watches for configuration file changes
func (crs *ConfigReloadService) watchConfigFile() {
	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := crs.checkConfigFileModification(); err != nil {
				crs.logger.WithFields(map[string]interface{}{
					"error": err.Error(),
				}).Error("Failed to check config file modification")
			}
		case <-crs.watcherStop:
			return
		}
	}
}

// checkConfigFileModification checks if the config file has been modified
func (crs *ConfigReloadService) checkConfigFileModification() error {
	// Check if file exists and can be read
	_, err := ioutil.ReadFile(crs.configFilePath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// For simplicity, we'll reload if file content changed
	// In production, use proper file system watching
	newConfig, err := crs.loadConfigFromFile()
	if err != nil {
		return fmt.Errorf("failed to load new config: %w", err)
	}

	// Compare configs (simplified comparison)
	if crs.configChanged(newConfig) {
		crs.logger.Info("Configuration file changed, reloading...")
		return crs.ReloadConfig(newConfig)
	}

	return nil
}

// loadConfigFromFile loads configuration from file
func (crs *ConfigReloadService) loadConfigFromFile() (*config.Config, error) {
	data, err := ioutil.ReadFile(crs.configFilePath)
	if err != nil {
		return nil, err
	}

	var newConfig config.Config
	if err := yaml.Unmarshal(data, &newConfig); err != nil {
		return nil, err
	}

	return &newConfig, nil
}

// configChanged checks if configuration has changed
func (crs *ConfigReloadService) configChanged(newConfig *config.Config) bool {
	crs.mutex.RLock()
	defer crs.mutex.RUnlock()

	// Simple comparison - serialize both configs and compare
	oldData, _ := json.Marshal(crs.config)
	newData, _ := json.Marshal(newConfig)

	return string(oldData) != string(newData)
}

// ReloadConfig reloads the configuration
func (crs *ConfigReloadService) ReloadConfig(newConfig *config.Config) error {
	crs.mutex.Lock()
	defer crs.mutex.Unlock()

	crs.logger.WithFields(map[string]interface{}{
		"old_backends": len(crs.config.Backends),
		"new_backends": len(newConfig.Backends),
	}).Info("Reloading configuration")

	// Validate new configuration
	if err := crs.validateConfig(newConfig); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Store old config for rollback
	oldConfig := crs.config

	// Update backend configuration
	if err := crs.updateBackends(newConfig); err != nil {
		crs.logger.WithFields(map[string]interface{}{
			"error": err.Error(),
		}).Error("Failed to update backends, rolling back")
		return err
	}

	// Update load balancer strategy if changed
	if oldConfig.LoadBalancer.Strategy != newConfig.LoadBalancer.Strategy {
		strategy := domain.LoadBalancingStrategy(newConfig.LoadBalancer.Strategy)
		if err := crs.loadBalancer.SetStrategy(strategy); err != nil {
			crs.logger.WithFields(map[string]interface{}{
				"error":    err.Error(),
				"strategy": newConfig.LoadBalancer.Strategy,
			}).Error("Failed to update load balancing strategy")
			return err
		}
		crs.logger.WithFields(map[string]interface{}{
			"old_strategy": oldConfig.LoadBalancer.Strategy,
			"new_strategy": newConfig.LoadBalancer.Strategy,
		}).Info("Updated load balancing strategy")
	}

	// Call registered callbacks
	for _, callback := range crs.reloadCallbacks {
		if err := callback(newConfig); err != nil {
			crs.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Error("Config reload callback failed")
			return err
		}
	}

	// Update stored configuration
	crs.config = newConfig

	crs.logger.Info("Configuration reloaded successfully")
	return nil
}

// updateBackends updates the backend configuration
func (crs *ConfigReloadService) updateBackends(newConfig *config.Config) error {
	// Get current backends
	currentBackends := crs.loadBalancer.GetBackends()
	currentBackendMap := make(map[string]*domain.Backend)
	for _, backend := range currentBackends {
		currentBackendMap[backend.ID] = backend
	}

	// Create map of new backends
	newBackendMap := make(map[string]*domain.Backend)
	for _, backendConfig := range newConfig.Backends {
		backend := &domain.Backend{
			ID:              backendConfig.ID,
			URL:             backendConfig.URL,
			Weight:          backendConfig.Weight,
			HealthCheckPath: backendConfig.HealthCheckPath,
			MaxConnections:  backendConfig.MaxConnections,
			Timeout:         backendConfig.Timeout,
		}
		backend.SetStatus(domain.StatusHealthy)
		newBackendMap[backend.ID] = backend
	}

	// Remove backends that are no longer in the configuration
	for id, backend := range currentBackendMap {
		if _, exists := newBackendMap[id]; !exists {
			if err := crs.loadBalancer.RemoveBackend(id); err != nil {
				crs.logger.WithFields(map[string]interface{}{
					"backend_id": id,
					"error":      err.Error(),
				}).Error("Failed to remove backend")
				return err
			}
			crs.logger.WithFields(map[string]interface{}{
				"backend_id": id,
				"url":        backend.URL,
			}).Info("Removed backend")
		}
	}

	// Add or update backends
	for id, backend := range newBackendMap {
		if currentBackend, exists := currentBackendMap[id]; exists {
			// Update existing backend if URL changed
			if currentBackend.URL != backend.URL {
				if err := crs.loadBalancer.RemoveBackend(id); err != nil {
					return err
				}
				if err := crs.loadBalancer.AddBackend(backend); err != nil {
					return err
				}
				crs.logger.WithFields(map[string]interface{}{
					"backend_id": id,
					"old_url":    currentBackend.URL,
					"new_url":    backend.URL,
				}).Info("Updated backend URL")
			}
		} else {
			// Add new backend
			if err := crs.loadBalancer.AddBackend(backend); err != nil {
				crs.logger.WithFields(map[string]interface{}{
					"backend_id": id,
					"error":      err.Error(),
				}).Error("Failed to add backend")
				return err
			}
			crs.logger.WithFields(map[string]interface{}{
				"backend_id": id,
				"url":        backend.URL,
			}).Info("Added new backend")
		}
	}

	return nil
}

// validateConfig validates the new configuration
func (crs *ConfigReloadService) validateConfig(config *config.Config) error {
	if len(config.Backends) == 0 {
		return fmt.Errorf("at least one backend must be configured")
	}

	// Validate load balancing strategy
	validStrategies := []string{"round_robin", "ip_hash", "weighted"}
	strategyValid := false
	for _, valid := range validStrategies {
		if config.LoadBalancer.Strategy == valid {
			strategyValid = true
			break
		}
	}
	if !strategyValid {
		return fmt.Errorf("invalid load balancing strategy: %s", config.LoadBalancer.Strategy)
	}

	// Validate backend configurations
	for _, backend := range config.Backends {
		if backend.ID == "" {
			return fmt.Errorf("backend ID cannot be empty")
		}
		if backend.URL == "" {
			return fmt.Errorf("backend URL cannot be empty")
		}
		if backend.Weight <= 0 {
			return fmt.Errorf("backend weight must be positive")
		}
	}

	return nil
}

// GetCurrentConfig returns the current configuration
func (crs *ConfigReloadService) GetCurrentConfig() *config.Config {
	crs.mutex.RLock()
	defer crs.mutex.RUnlock()
	return crs.config
}

// ReloadFromAPI reloads configuration from API request
func (crs *ConfigReloadService) ReloadFromAPI(newConfigData []byte) error {
	var newConfig config.Config
	if err := yaml.Unmarshal(newConfigData, &newConfig); err != nil {
		return fmt.Errorf("invalid YAML configuration: %w", err)
	}

	return crs.ReloadConfig(&newConfig)
}

// GetReloadStats returns reload statistics
func (crs *ConfigReloadService) GetReloadStats() map[string]interface{} {
	crs.mutex.RLock()
	defer crs.mutex.RUnlock()

	return map[string]interface{}{
		"config_file":      crs.configFilePath,
		"watcher_active":   true,
		"callbacks_count":  len(crs.reloadCallbacks),
		"current_backends": len(crs.config.Backends),
		"last_modified":    crs.lastModTime,
	}
}
