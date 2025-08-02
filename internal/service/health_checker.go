package service

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// HealthChecker implements the domain.HealthChecker interface
type HealthChecker struct {
	config    domain.HealthCheckConfig
	client    *http.Client
	logger    *logger.Logger
	stopChan  chan struct{}
	wg        sync.WaitGroup
	isRunning bool
	mu        sync.RWMutex
}

// NewHealthChecker creates a new health checker instance
func NewHealthChecker(config domain.HealthCheckConfig, logger *logger.Logger) *HealthChecker {
	return &HealthChecker{
		config: config,
		client: &http.Client{
			Timeout: config.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  true,
				MaxIdleConnsPerHost: 2,
			},
		},
		logger:   logger.HealthCheckLogger(),
		stopChan: make(chan struct{}),
	}
}

// Check performs a health check on a backend
func (hc *HealthChecker) Check(ctx context.Context, backend *domain.Backend) error {
	if !hc.config.Enabled {
		return nil
	}

	healthURL := backend.URL + backend.HealthCheckPath
	log := hc.logger.BackendLogger(backend.ID, backend.URL)

	log.Debugf("Performing health check for backend %s at %s", backend.ID, healthURL)

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		log.WithError(err).Error("Failed to create health check request")
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	// Set headers
	req.Header.Set("User-Agent", "LoadBalancer-HealthChecker/1.0")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	// Perform the request
	start := time.Now()
	resp, err := hc.client.Do(req)
	duration := time.Since(start)

	if err != nil {
		log.WithError(err).WithField("duration_ms", duration.Milliseconds()).
			Warn("Health check request failed")
		hc.handleHealthCheckFailure(backend)
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()

	log.WithField("status_code", resp.StatusCode).
		WithField("duration_ms", duration.Milliseconds()).
		Debug("Health check request completed")

	// Update last health check timestamp
	backend.UpdateLastHealthCheck()

	// Check response status
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		hc.handleHealthCheckSuccess(backend)
		log.Debug("Backend health check passed")
		return nil
	}

	// Handle unhealthy response
	log.WithField("status_code", resp.StatusCode).
		Warn("Backend health check failed with non-2xx status")
	hc.handleHealthCheckFailure(backend)
	return fmt.Errorf("health check failed with status %d", resp.StatusCode)
}

// StartChecking starts periodic health checking for all backends
func (hc *HealthChecker) StartChecking(ctx context.Context, backends []*domain.Backend) error {
	if !hc.config.Enabled {
		hc.logger.Info("Health checking is disabled")
		return nil
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	if hc.isRunning {
		return fmt.Errorf("health checker is already running")
	}

	hc.isRunning = true
	hc.logger.Infof("Starting health checker with interval %v", hc.config.Interval)

	// Start health checking for each backend in a separate goroutine
	for _, backend := range backends {
		hc.wg.Add(1)
		go hc.healthCheckLoop(ctx, backend)
	}

	return nil
}

// StopChecking stops health checking
func (hc *HealthChecker) StopChecking() error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if !hc.isRunning {
		return nil
	}

	hc.logger.Info("Stopping health checker")
	close(hc.stopChan)
	hc.wg.Wait()
	hc.isRunning = false
	hc.stopChan = make(chan struct{})

	hc.logger.Info("Health checker stopped")
	return nil
}

// AddBackend adds a backend to health checking
func (hc *HealthChecker) AddBackend(ctx context.Context, backend *domain.Backend) {
	if !hc.config.Enabled {
		return
	}

	hc.mu.RLock()
	isRunning := hc.isRunning
	hc.mu.RUnlock()

	if isRunning {
		hc.wg.Add(1)
		go hc.healthCheckLoop(ctx, backend)
		hc.logger.Infof("Added backend %s to health checking", backend.ID)
	}
}

// healthCheckLoop runs the health check loop for a specific backend
func (hc *HealthChecker) healthCheckLoop(ctx context.Context, backend *domain.Backend) {
	defer hc.wg.Done()

	ticker := time.NewTicker(hc.config.Interval)
	defer ticker.Stop()

	log := hc.logger.BackendLogger(backend.ID, backend.URL)
	log.Debug("Starting health check loop")

	// Perform initial health check
	if err := hc.Check(ctx, backend); err != nil {
		log.WithError(err).Warn("Initial health check failed")
	}

	for {
		select {
		case <-ctx.Done():
			log.Debug("Health check loop stopped due to context cancellation")
			return
		case <-hc.stopChan:
			log.Debug("Health check loop stopped")
			return
		case <-ticker.C:
			// Create a timeout context for this health check
			checkCtx, cancel := context.WithTimeout(ctx, hc.config.Timeout)

			if err := hc.Check(checkCtx, backend); err != nil {
				log.WithError(err).Debug("Health check failed")
			}

			cancel()
		}
	}
}

// handleHealthCheckSuccess handles a successful health check
func (hc *HealthChecker) handleHealthCheckSuccess(backend *domain.Backend) {
	// Reset failure count on successful health check
	if backend.GetFailureCount() > 0 {
		backend.ResetFailures()
	}

	// Mark backend as healthy if it was unhealthy
	if backend.GetStatus() != domain.StatusHealthy {
		backend.SetStatus(domain.StatusHealthy)
		hc.logger.BackendLogger(backend.ID, backend.URL).
			Info("Backend recovered and marked as healthy")
	}
}

// handleHealthCheckFailure handles a failed health check
func (hc *HealthChecker) handleHealthCheckFailure(backend *domain.Backend) {
	backend.IncrementFailures()

	log := hc.logger.BackendLogger(backend.ID, backend.URL).
		WithField("failure_count", backend.GetFailureCount())

	// Mark backend as unhealthy if failure threshold is reached
	if backend.GetFailureCount() >= int64(hc.config.UnhealthyThreshold) {
		if backend.GetStatus() == domain.StatusHealthy {
			backend.SetStatus(domain.StatusUnhealthy)
			log.Warn("Backend marked as unhealthy due to repeated failures")
		}
	} else {
		log.Debug("Health check failed but threshold not reached")
	}
}

// GetStats returns health checker statistics
func (hc *HealthChecker) GetStats() map[string]interface{} {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	return map[string]interface{}{
		"enabled":             hc.config.Enabled,
		"running":             hc.isRunning,
		"interval":            hc.config.Interval.String(),
		"timeout":             hc.config.Timeout.String(),
		"healthy_threshold":   hc.config.HealthyThreshold,
		"unhealthy_threshold": hc.config.UnhealthyThreshold,
		"check_path":          hc.config.Path,
	}
}

// IsRunning returns true if health checking is currently running
func (hc *HealthChecker) IsRunning() bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.isRunning
}
