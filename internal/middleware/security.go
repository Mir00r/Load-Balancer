package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// SecurityMiddleware provides security features including IP filtering and DDoS protection
type SecurityMiddleware struct {
	config domain.SecurityConfig
	logger *logger.Logger

	// Connection tracking for DDoS protection
	connectionTracker map[string]*connectionInfo
	mutex             sync.RWMutex

	// IP networks for whitelist/blacklist
	whitelistNets []*net.IPNet
	blacklistNets []*net.IPNet
}

// connectionInfo tracks connection details per IP
type connectionInfo struct {
	count       int
	lastRequest time.Time
	blocked     bool
	blockUntil  time.Time
}

// NewSecurityMiddleware creates a new security middleware instance
func NewSecurityMiddleware(config domain.SecurityConfig, logger *logger.Logger) (*SecurityMiddleware, error) {
	sm := &SecurityMiddleware{
		config:            config,
		logger:            logger,
		connectionTracker: make(map[string]*connectionInfo),
	}

	// Parse IP whitelist
	for _, ip := range config.IPWhitelist {
		if net, err := parseIPOrCIDR(ip); err == nil {
			sm.whitelistNets = append(sm.whitelistNets, net)
		} else {
			return nil, fmt.Errorf("invalid whitelist IP/CIDR: %s: %w", ip, err)
		}
	}

	// Parse IP blacklist
	for _, ip := range config.IPBlacklist {
		if net, err := parseIPOrCIDR(ip); err == nil {
			sm.blacklistNets = append(sm.blacklistNets, net)
		} else {
			return nil, fmt.Errorf("invalid blacklist IP/CIDR: %s: %w", ip, err)
		}
	}

	// Start cleanup goroutine if DDoS protection is enabled
	if config.DDoSProtection {
		go sm.cleanupRoutine()
	}

	return sm, nil
}

// SecurityMiddleware returns the HTTP middleware function
func (sm *SecurityMiddleware) SecurityMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := sm.getClientIP(r)

			log := sm.logger.WithFields(map[string]interface{}{
				"component": "security",
				"client_ip": clientIP,
				"path":      r.URL.Path,
				"method":    r.Method,
			})

			// Check request size limit
			if sm.config.RequestSizeLimit > 0 && r.ContentLength > sm.config.RequestSizeLimit {
				log.Warn("Request size limit exceeded")
				http.Error(w, "Request too large", http.StatusRequestEntityTooLarge)
				return
			}

			// Check IP blacklist first
			if sm.isBlacklisted(clientIP) {
				log.Warn("Request from blacklisted IP")
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Check IP whitelist (if configured)
			if len(sm.whitelistNets) > 0 && !sm.isWhitelisted(clientIP) {
				log.Warn("Request from non-whitelisted IP")
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// DDoS protection
			if sm.config.DDoSProtection && !sm.checkDDoSLimits(clientIP) {
				log.Warn("DDoS protection triggered")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			// Update connection tracking
			sm.updateConnectionTracker(clientIP)

			// Add security headers
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the real client IP from request headers
func (sm *SecurityMiddleware) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for reverse proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isWhitelisted checks if an IP is in the whitelist
func (sm *SecurityMiddleware) isWhitelisted(ip string) bool {
	clientIP := net.ParseIP(ip)
	if clientIP == nil {
		return false
	}

	for _, ipNet := range sm.whitelistNets {
		if ipNet.Contains(clientIP) {
			return true
		}
	}
	return false
}

// isBlacklisted checks if an IP is in the blacklist
func (sm *SecurityMiddleware) isBlacklisted(ip string) bool {
	clientIP := net.ParseIP(ip)
	if clientIP == nil {
		return false
	}

	for _, ipNet := range sm.blacklistNets {
		if ipNet.Contains(clientIP) {
			return true
		}
	}
	return false
}

// checkDDoSLimits implements basic DDoS protection
func (sm *SecurityMiddleware) checkDDoSLimits(ip string) bool {
	if sm.config.MaxConnPerIP <= 0 {
		return true // No limit configured
	}

	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	info, exists := sm.connectionTracker[ip]
	if !exists {
		return true // First request from this IP
	}

	now := time.Now()

	// Check if IP is currently blocked
	if info.blocked && now.Before(info.blockUntil) {
		return false
	}

	// Reset block status if block period has expired
	if info.blocked && now.After(info.blockUntil) {
		info.blocked = false
		info.count = 0
	}

	// Check connection limit
	if info.count >= sm.config.MaxConnPerIP {
		// Block for 5 minutes
		info.blocked = true
		info.blockUntil = now.Add(5 * time.Minute)
		sm.logger.WithField("client_ip", ip).Warn("IP blocked due to excessive connections")
		return false
	}

	return true
}

// updateConnectionTracker updates connection tracking for an IP
func (sm *SecurityMiddleware) updateConnectionTracker(ip string) {
	if !sm.config.DDoSProtection {
		return
	}

	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	now := time.Now()
	info, exists := sm.connectionTracker[ip]
	if !exists {
		sm.connectionTracker[ip] = &connectionInfo{
			count:       1,
			lastRequest: now,
		}
		return
	}

	// Increment count if not blocked
	if !info.blocked {
		info.count++
	}
	info.lastRequest = now
}

// cleanupRoutine periodically cleans up old connection tracking data
func (sm *SecurityMiddleware) cleanupRoutine() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sm.cleanup()
	}
}

// cleanup removes old connection tracking data
func (sm *SecurityMiddleware) cleanup() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	now := time.Now()
	cutoff := now.Add(-30 * time.Minute) // Remove entries older than 30 minutes

	for ip, info := range sm.connectionTracker {
		if info.lastRequest.Before(cutoff) && !info.blocked {
			delete(sm.connectionTracker, ip)
		}
	}
}

// parseIPOrCIDR parses an IP address or CIDR notation
func parseIPOrCIDR(s string) (*net.IPNet, error) {
	if strings.Contains(s, "/") {
		_, ipNet, err := net.ParseCIDR(s)
		return ipNet, err
	}

	ip := net.ParseIP(s)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", s)
	}

	// Create a /32 (IPv4) or /128 (IPv6) network for single IP
	var mask net.IPMask
	if ip.To4() != nil {
		mask = net.CIDRMask(32, 32)
	} else {
		mask = net.CIDRMask(128, 128)
	}

	return &net.IPNet{IP: ip, Mask: mask}, nil
}

// GetStats returns security middleware statistics
func (sm *SecurityMiddleware) GetStats() map[string]interface{} {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	stats := map[string]interface{}{
		"whitelist_rules": len(sm.whitelistNets),
		"blacklist_rules": len(sm.blacklistNets),
		"tracked_ips":     len(sm.connectionTracker),
		"ddos_protection": sm.config.DDoSProtection,
	}

	if sm.config.DDoSProtection {
		blockedCount := 0
		for _, info := range sm.connectionTracker {
			if info.blocked {
				blockedCount++
			}
		}
		stats["blocked_ips"] = blockedCount
	}

	return stats
}
