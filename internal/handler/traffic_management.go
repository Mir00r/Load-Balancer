package handler

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/config"
	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// TrafficManagementHandler implements advanced Traefik-style traffic management
// Provides blue-green deployments, canary releases, traffic mirroring, and session stickiness
type TrafficManagementHandler struct {
	loadBalancer    domain.LoadBalancer
	metrics         domain.Metrics
	logger          *logger.Logger
	config          config.TrafficManagementConfig
	mutex           sync.RWMutex
	sessionStore    map[string]string // Session ID -> Backend ID mapping
	canaryBackends  []config.CanaryBackend
	mirrorTargets   []config.MirrorTarget
	circuitBreakers map[string]*CircuitBreaker
}

// CircuitBreaker implements circuit breaker pattern
type CircuitBreaker struct {
	State           string // "closed", "open", "half-open"
	FailureCount    int
	SuccessCount    int
	RequestCount    int
	LastFailureTime time.Time
	LastSuccessTime time.Time
	Config          config.CircuitBreakerConfig
	mutex           sync.RWMutex
}

// NewTrafficManagementHandler creates a new traffic management handler
func NewTrafficManagementHandler(lb domain.LoadBalancer, metrics domain.Metrics, logger *logger.Logger, config config.TrafficManagementConfig) *TrafficManagementHandler {
	handler := &TrafficManagementHandler{
		loadBalancer:    lb,
		metrics:         metrics,
		logger:          logger,
		config:          config,
		sessionStore:    make(map[string]string),
		circuitBreakers: make(map[string]*CircuitBreaker),
		canaryBackends:  config.CanaryDeployment.CanaryBackends,
		mirrorTargets:   config.TrafficMirroring.Targets,
	}

	// Initialize circuit breakers for each backend
	if config.CircuitBreaker.Enabled {
		for _, backend := range lb.GetBackends() {
			handler.circuitBreakers[backend.ID] = &CircuitBreaker{
				State:  "closed",
				Config: config.CircuitBreaker,
			}
		}
	}

	// Start cleanup goroutine for session store
	go handler.sessionCleanup()

	logger.WithFields(map[string]interface{}{
		"session_stickiness": config.SessionStickiness.Enabled,
		"blue_green":         config.BlueGreenDeployment.Enabled,
		"canary":             config.CanaryDeployment.Enabled,
		"mirroring":          config.TrafficMirroring.Enabled,
		"circuit_breaker":    config.CircuitBreaker.Enabled,
	}).Info("Traffic management handler initialized")

	return handler
}

// ServeHTTP handles requests with advanced traffic management
func (tm *TrafficManagementHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// 1. Blue-Green Deployment Check
	if tm.config.BlueGreenDeployment.Enabled {
		if backend := tm.handleBlueGreen(r); backend != nil {
			tm.forwardRequest(w, r, backend, "blue-green")
			return
		}
	}

	// 2. Canary Deployment Check
	if tm.config.CanaryDeployment.Enabled {
		if backend := tm.handleCanary(r); backend != nil {
			tm.forwardRequest(w, r, backend, "canary")
			return
		}
	}

	// 3. Session Stickiness Check
	var selectedBackend *domain.Backend
	if tm.config.SessionStickiness.Enabled {
		selectedBackend = tm.handleSessionStickiness(r)
	}

	// 4. Regular Load Balancing if no sticky session
	if selectedBackend == nil {
		selectedBackend = tm.loadBalancer.SelectBackend()
	}

	if selectedBackend == nil {
		tm.logger.Error("No available backend for request")
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	// 5. Circuit Breaker Check
	if tm.config.CircuitBreaker.Enabled {
		cb := tm.circuitBreakers[selectedBackend.ID]
		if !tm.canExecuteRequest(cb) {
			tm.logger.WithFields(map[string]interface{}{
				"backend_id": selectedBackend.ID,
				"cb_state":   cb.State,
			}).Warn("Circuit breaker prevented request")
			http.Error(w, "Service Temporarily Unavailable", http.StatusServiceUnavailable)
			return
		}
	}

	// 6. Traffic Mirroring (async)
	if tm.config.TrafficMirroring.Enabled {
		tm.mirrorTraffic(r)
	}

	// 7. Forward main request
	success := tm.forwardRequest(w, r, selectedBackend, "load-balanced")

	// 8. Update circuit breaker state
	if tm.config.CircuitBreaker.Enabled {
		cb := tm.circuitBreakers[selectedBackend.ID]
		tm.updateCircuitBreaker(cb, success)
	}

	tm.logger.WithFields(map[string]interface{}{
		"backend_id": selectedBackend.ID,
		"duration":   time.Since(startTime),
		"success":    success,
	}).Debug("Request processed by traffic management")
}

// handleBlueGreen implements blue-green deployment logic
func (tm *TrafficManagementHandler) handleBlueGreen(r *http.Request) *domain.Backend {
	config := tm.config.BlueGreenDeployment

	// Check for force switch header
	if config.SwitchHeader != "" {
		if slot := r.Header.Get(config.SwitchHeader); slot != "" {
			if slot == "blue" || slot == "green" {
				return tm.selectFromSlot(slot)
			}
		}
	}

	// Use active slot
	return tm.selectFromSlot(config.ActiveSlot)
}

// selectFromSlot selects a backend from the specified slot
func (tm *TrafficManagementHandler) selectFromSlot(slot string) *domain.Backend {
	config := tm.config.BlueGreenDeployment
	var targetBackends []string

	switch slot {
	case "blue":
		targetBackends = config.BlueBackends
	case "green":
		targetBackends = config.GreenBackends
	default:
		return nil
	}

	// Select random backend from slot
	if len(targetBackends) > 0 {
		backendID := targetBackends[rand.Intn(len(targetBackends))]
		for _, backend := range tm.loadBalancer.GetBackends() {
			if backend.ID == backendID && backend.IsHealthy() {
				return backend
			}
		}
	}

	return nil
}

// handleCanary implements canary deployment logic
func (tm *TrafficManagementHandler) handleCanary(r *http.Request) *domain.Backend {
	config := tm.config.CanaryDeployment

	// Check for force canary header
	if config.SplitHeader != "" {
		if r.Header.Get(config.SplitHeader) == "canary" {
			return tm.selectCanaryBackend()
		}
	}

	// Check traffic split percentage
	if rand.Intn(100) < config.TrafficSplit {
		return tm.selectCanaryBackend()
	}

	return nil
}

// selectCanaryBackend selects a backend from canary backends
func (tm *TrafficManagementHandler) selectCanaryBackend() *domain.Backend {
	if len(tm.canaryBackends) == 0 {
		return nil
	}

	// Weighted selection from canary backends
	totalWeight := 0
	for _, cb := range tm.canaryBackends {
		totalWeight += cb.Weight
	}

	if totalWeight == 0 {
		return nil
	}

	target := rand.Intn(totalWeight)
	current := 0

	for _, cb := range tm.canaryBackends {
		current += cb.Weight
		if target < current {
			for _, backend := range tm.loadBalancer.GetBackends() {
				if backend.ID == cb.BackendID && backend.IsHealthy() {
					return backend
				}
			}
		}
	}

	return nil
}

// handleSessionStickiness implements session affinity
func (tm *TrafficManagementHandler) handleSessionStickiness(r *http.Request) *domain.Backend {
	config := tm.config.SessionStickiness
	var sessionID string

	// Try cookie first
	if config.CookieName != "" {
		if cookie, err := r.Cookie(config.CookieName); err == nil {
			sessionID = cookie.Value
		}
	}

	// Try header if no cookie
	if sessionID == "" && config.HeaderName != "" {
		sessionID = r.Header.Get(config.HeaderName)
	}

	if sessionID == "" {
		return nil
	}

	tm.mutex.RLock()
	backendID, exists := tm.sessionStore[sessionID]
	tm.mutex.RUnlock()

	if !exists {
		return nil
	}

	// Find the backend
	for _, backend := range tm.loadBalancer.GetBackends() {
		if backend.ID == backendID && backend.IsHealthy() {
			return backend
		}
	}

	// Backend is not healthy, remove from session store
	tm.mutex.Lock()
	delete(tm.sessionStore, sessionID)
	tm.mutex.Unlock()

	return nil
}

// mirrorTraffic implements traffic mirroring
func (tm *TrafficManagementHandler) mirrorTraffic(r *http.Request) {
	config := tm.config.TrafficMirroring

	// Check sample rate
	if rand.Float64() > config.SampleRate {
		return
	}

	for _, target := range tm.mirrorTargets {
		if config.Async {
			go tm.sendMirrorRequest(r, target)
		} else {
			tm.sendMirrorRequest(r, target)
		}
	}
}

// sendMirrorRequest sends a mirrored request to target
func (tm *TrafficManagementHandler) sendMirrorRequest(r *http.Request, target config.MirrorTarget) {
	client := &http.Client{Timeout: 5 * time.Second}

	// Create mirror request URL
	mirrorURL := target.URL + r.URL.Path
	if r.URL.RawQuery != "" {
		mirrorURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequest(r.Method, mirrorURL, nil) // Don't copy body for mirrors
	if err != nil {
		tm.logger.WithError(err).WithField("target", target.Name).Warn("Failed to create mirror request")
		return
	}

	// Copy headers
	for key, values := range r.Header {
		req.Header[key] = values
	}
	req.Header.Set("X-Mirror-Request", "true")

	resp, err := client.Do(req)
	if err != nil {
		tm.logger.WithError(err).WithField("target", target.Name).Debug("Mirror request failed")
		return
	}
	defer resp.Body.Close()

	tm.logger.WithFields(map[string]interface{}{
		"target":      target.Name,
		"status_code": resp.StatusCode,
		"url":         mirrorURL,
	}).Debug("Mirror request completed")
}

// forwardRequest forwards the request to the selected backend
func (tm *TrafficManagementHandler) forwardRequest(w http.ResponseWriter, r *http.Request, backend *domain.Backend, strategy string) bool {
	// Create session if session stickiness is enabled
	if tm.config.SessionStickiness.Enabled {
		tm.createSession(w, r, backend)
	}

	// Add strategy header
	w.Header().Set("X-Load-Balance-Strategy", strategy)
	w.Header().Set("X-Backend-ID", backend.ID)

	// Forward request (simplified - in real implementation, use proper proxy)
	client := &http.Client{Timeout: 30 * time.Second}

	backendURL := backend.URL + r.URL.Path
	if r.URL.RawQuery != "" {
		backendURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequest(r.Method, backendURL, r.Body)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return false
	}

	// Copy headers
	for key, values := range r.Header {
		req.Header[key] = values
	}

	resp, err := client.Do(req)
	if err != nil {
		tm.logger.WithError(err).WithField("backend", backend.URL).Error("Request forwarding failed")
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return false
	}
	defer resp.Body.Close()

	// Copy response headers and body
	for key, values := range resp.Header {
		w.Header()[key] = values
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	tm.metrics.IncrementRequests(backend.ID)
	return resp.StatusCode < 500
}

// createSession creates a session for sticky routing
func (tm *TrafficManagementHandler) createSession(w http.ResponseWriter, r *http.Request, backend *domain.Backend) {
	config := tm.config.SessionStickiness

	// Get or create session ID
	var sessionID string
	if config.CookieName != "" {
		if cookie, err := r.Cookie(config.CookieName); err == nil {
			sessionID = cookie.Value
		}
	}

	if sessionID == "" && config.HeaderName != "" {
		sessionID = r.Header.Get(config.HeaderName)
	}

	if sessionID == "" {
		// Generate new session ID
		sessionID = tm.generateSessionID()

		// Set cookie
		if config.CookieName != "" {
			cookie := &http.Cookie{
				Name:     config.CookieName,
				Value:    sessionID,
				MaxAge:   int(config.TTL.Seconds()),
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			}
			http.SetCookie(w, cookie)
		}
	}

	// Store session mapping
	tm.mutex.Lock()
	tm.sessionStore[sessionID] = backend.ID
	tm.mutex.Unlock()
}

// generateSessionID generates a unique session ID
func (tm *TrafficManagementHandler) generateSessionID() string {
	return fmt.Sprintf("session_%d_%d", time.Now().UnixNano(), rand.Intn(10000))
}

// Circuit Breaker Methods

// canExecuteRequest checks if request can be executed based on circuit breaker state
func (tm *TrafficManagementHandler) canExecuteRequest(cb *CircuitBreaker) bool {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()

	switch cb.State {
	case "closed":
		return true
	case "open":
		if now.Sub(cb.LastFailureTime) > cb.Config.RecoveryTimeout {
			cb.State = "half-open"
			cb.SuccessCount = 0
			return true
		}
		return false
	case "half-open":
		return true
	default:
		return true
	}
}

// updateCircuitBreaker updates circuit breaker state based on request result
func (tm *TrafficManagementHandler) updateCircuitBreaker(cb *CircuitBreaker, success bool) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.RequestCount++

	if success {
		cb.SuccessCount++
		cb.LastSuccessTime = time.Now()

		if cb.State == "half-open" && cb.SuccessCount >= cb.Config.SuccessThreshold {
			cb.State = "closed"
			cb.FailureCount = 0
		}
	} else {
		cb.FailureCount++
		cb.LastFailureTime = time.Now()

		if cb.State == "closed" || cb.State == "half-open" {
			if cb.RequestCount >= cb.Config.RequestVolumeThreshold &&
				cb.FailureCount >= cb.Config.FailureThreshold {
				cb.State = "open"
			}
		}
	}
}

// sessionCleanup periodically cleans up expired sessions
func (tm *TrafficManagementHandler) sessionCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		// Simple cleanup - in production, you'd track session timestamps
		tm.mutex.Lock()
		if len(tm.sessionStore) > 1000 { // Prevent memory leaks
			// Clear half the sessions (simple eviction)
			count := 0
			for sessionID := range tm.sessionStore {
				delete(tm.sessionStore, sessionID)
				count++
				if count >= len(tm.sessionStore)/2 {
					break
				}
			}
			tm.logger.WithField("cleaned_sessions", count).Debug("Session cleanup performed")
		}
		tm.mutex.Unlock()
	}
}

// GetStats returns traffic management statistics
func (tm *TrafficManagementHandler) GetStats() map[string]interface{} {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	stats := map[string]interface{}{
		"session_stickiness": map[string]interface{}{
			"enabled":         tm.config.SessionStickiness.Enabled,
			"active_sessions": len(tm.sessionStore),
		},
		"blue_green": map[string]interface{}{
			"enabled":     tm.config.BlueGreenDeployment.Enabled,
			"active_slot": tm.config.BlueGreenDeployment.ActiveSlot,
		},
		"canary": map[string]interface{}{
			"enabled":       tm.config.CanaryDeployment.Enabled,
			"traffic_split": tm.config.CanaryDeployment.TrafficSplit,
			"backends":      len(tm.canaryBackends),
		},
		"mirroring": map[string]interface{}{
			"enabled":     tm.config.TrafficMirroring.Enabled,
			"targets":     len(tm.mirrorTargets),
			"sample_rate": tm.config.TrafficMirroring.SampleRate,
		},
	}

	// Circuit breaker stats
	if tm.config.CircuitBreaker.Enabled {
		cbStats := make(map[string]interface{})
		for backendID, cb := range tm.circuitBreakers {
			cb.mutex.RLock()
			cbStats[backendID] = map[string]interface{}{
				"state":         cb.State,
				"failure_count": cb.FailureCount,
				"success_count": cb.SuccessCount,
				"request_count": cb.RequestCount,
			}
			cb.mutex.RUnlock()
		}
		stats["circuit_breakers"] = cbStats
	}

	return stats
}
