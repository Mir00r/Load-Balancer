package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/pkg/logger"
	"golang.org/x/time/rate"
)

// RateLimitConfig defines rate limiting configuration
type RateLimitConfig struct {
	Enabled         bool          `yaml:"enabled"`
	RequestsPerSec  float64       `yaml:"requests_per_sec"`
	BurstSize       int           `yaml:"burst_size"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
	MaxClients      int           `yaml:"max_clients"`
	// IP-based rules
	WhitelistedIPs []string `yaml:"whitelisted_ips"`
	BlacklistedIPs []string `yaml:"blacklisted_ips"`
	// Path-based rules
	PathRules map[string]PathRateLimit `yaml:"path_rules"`
}

// PathRateLimit defines rate limiting per path
type PathRateLimit struct {
	RequestsPerSec float64 `yaml:"requests_per_sec"`
	BurstSize      int     `yaml:"burst_size"`
}

// ClientLimiter holds rate limiter for a specific client
type ClientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimitMiddleware implements NGINX-style rate limiting
type RateLimitMiddleware struct {
	config       RateLimitConfig
	clients      map[string]*ClientLimiter
	mutex        sync.RWMutex
	logger       *logger.Logger
	whitelisted  map[string]bool
	blacklisted  map[string]bool
	cleanupTimer *time.Timer
}

// NewRateLimitMiddleware creates a new rate limiting middleware
func NewRateLimitMiddleware(config RateLimitConfig, logger *logger.Logger) *RateLimitMiddleware {
	rlm := &RateLimitMiddleware{
		config:      config,
		clients:     make(map[string]*ClientLimiter),
		logger:      logger,
		whitelisted: make(map[string]bool),
		blacklisted: make(map[string]bool),
	}

	// Populate IP lists
	for _, ip := range config.WhitelistedIPs {
		rlm.whitelisted[ip] = true
	}
	for _, ip := range config.BlacklistedIPs {
		rlm.blacklisted[ip] = true
	}

	// Start cleanup routine
	if config.CleanupInterval > 0 {
		rlm.startCleanup()
	}

	return rlm
}

// RateLimit returns the rate limiting middleware
func (rlm *RateLimitMiddleware) RateLimit() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rlm.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			clientIP := rlm.getClientIP(r)

			// Check blacklist first
			if rlm.blacklisted[clientIP] {
				rlm.logger.WithFields(map[string]interface{}{
					"client_ip": clientIP,
					"path":      r.URL.Path,
				}).Warn("Request blocked: IP in blacklist")
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Skip rate limiting for whitelisted IPs
			if rlm.whitelisted[clientIP] {
				next.ServeHTTP(w, r)
				return
			}

			// Get or create rate limiter for client
			limiter := rlm.getLimiter(clientIP, r.URL.Path)

			if !limiter.Allow() {
				rlm.logger.WithFields(map[string]interface{}{
					"client_ip": clientIP,
					"path":      r.URL.Path,
				}).Warn("Request rate limited")

				// Set rate limit headers (NGINX-style)
				w.Header().Set("X-RateLimit-Limit", strconv.FormatFloat(rlm.config.RequestsPerSec, 'f', 0, 64))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
				w.Header().Set("Retry-After", "1")

				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			// Add rate limit headers to successful requests
			remaining := int(limiter.Tokens())
			w.Header().Set("X-RateLimit-Limit", strconv.FormatFloat(rlm.config.RequestsPerSec, 'f', 0, 64))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			next.ServeHTTP(w, r)
		})
	}
}

// getLimiter gets or creates a rate limiter for a client
func (rlm *RateLimitMiddleware) getLimiter(clientIP, path string) *rate.Limiter {
	rlm.mutex.Lock()
	defer rlm.mutex.Unlock()

	key := clientIP + ":" + path

	// Check if we have a path-specific rule
	requestsPerSec := rlm.config.RequestsPerSec
	burstSize := rlm.config.BurstSize

	for pathPattern, rule := range rlm.config.PathRules {
		if matched, _ := matchPath(pathPattern, path); matched {
			requestsPerSec = rule.RequestsPerSec
			burstSize = rule.BurstSize
			break
		}
	}

	client, exists := rlm.clients[key]
	if !exists {
		// Check max clients limit
		if len(rlm.clients) >= rlm.config.MaxClients {
			// Remove oldest client
			rlm.removeOldestClient()
		}

		client = &ClientLimiter{
			limiter:  rate.NewLimiter(rate.Limit(requestsPerSec), burstSize),
			lastSeen: time.Now(),
		}
		rlm.clients[key] = client
	} else {
		client.lastSeen = time.Now()
	}

	return client.limiter
}

// getClientIP extracts the client IP address
func (rlm *RateLimitMiddleware) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (for proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// removeOldestClient removes the oldest client from the map
func (rlm *RateLimitMiddleware) removeOldestClient() {
	var oldestKey string
	var oldestTime time.Time

	for key, client := range rlm.clients {
		if oldestKey == "" || client.lastSeen.Before(oldestTime) {
			oldestKey = key
			oldestTime = client.lastSeen
		}
	}

	if oldestKey != "" {
		delete(rlm.clients, oldestKey)
	}
}

// startCleanup starts the cleanup routine for expired clients
func (rlm *RateLimitMiddleware) startCleanup() {
	rlm.cleanupTimer = time.AfterFunc(rlm.config.CleanupInterval, func() {
		rlm.cleanup()
		rlm.startCleanup() // Reschedule
	})
}

// cleanup removes expired clients
func (rlm *RateLimitMiddleware) cleanup() {
	rlm.mutex.Lock()
	defer rlm.mutex.Unlock()

	now := time.Now()
	for key, client := range rlm.clients {
		if now.Sub(client.lastSeen) > rlm.config.CleanupInterval {
			delete(rlm.clients, key)
		}
	}

	rlm.logger.WithFields(map[string]interface{}{
		"active_clients": len(rlm.clients),
	}).Debug("Cleaned up expired rate limit clients")
}

// matchPath checks if a path matches a pattern (simple glob-style matching)
func matchPath(pattern, path string) (bool, error) {
	// Simple wildcard matching - can be enhanced with regex
	if pattern == "*" {
		return true, nil
	}

	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(path, prefix), nil
	}

	return pattern == path, nil
}

// GetStats returns rate limiting statistics
func (rlm *RateLimitMiddleware) GetStats() map[string]interface{} {
	rlm.mutex.RLock()
	defer rlm.mutex.RUnlock()

	return map[string]interface{}{
		"enabled":          rlm.config.Enabled,
		"active_clients":   len(rlm.clients),
		"max_clients":      rlm.config.MaxClients,
		"requests_per_sec": rlm.config.RequestsPerSec,
		"burst_size":       rlm.config.BurstSize,
		"whitelisted_ips":  len(rlm.whitelisted),
		"blacklisted_ips":  len(rlm.blacklisted),
	}
}

// Stop stops the cleanup timer
func (rlm *RateLimitMiddleware) Stop() {
	if rlm.cleanupTimer != nil {
		rlm.cleanupTimer.Stop()
	}
}
