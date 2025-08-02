package middleware

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
	"golang.org/x/time/rate"
)

// RateLimiter manages rate limiting for clients
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	rate     rate.Limit
	burst    int
	logger   *logger.Logger
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config domain.RateLimitConfig, logger *logger.Logger) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     rate.Limit(config.RequestsPerSecond),
		burst:    config.BurstSize,
		logger:   logger.MiddlewareLogger("rate_limiter"),
	}
}

// getLimiter gets or creates a rate limiter for a client IP
func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.limiters[ip]
	if !exists {
		limiter = rate.NewLimiter(rl.rate, rl.burst)
		rl.limiters[ip] = limiter
	}

	return limiter
}

// cleanupLimiters removes old limiters to prevent memory leaks
func (rl *RateLimiter) cleanupLimiters() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// In a production system, you'd want to track last access time
	// and remove limiters that haven't been used recently
	if len(rl.limiters) > 10000 { // Simple cleanup when too many limiters
		rl.limiters = make(map[string]*rate.Limiter)
		rl.logger.Info("Cleaned up rate limiter cache")
	}
}

// RateLimitMiddleware provides rate limiting functionality
func (rl *RateLimiter) RateLimitMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract client IP
			clientIP := getClientIP(r)

			// Get rate limiter for this client
			limiter := rl.getLimiter(clientIP)

			// Check if request is allowed
			if !limiter.Allow() {
				rl.logger.WithFields(map[string]interface{}{
					"client_ip": clientIP,
					"path":      r.URL.Path,
					"method":    r.Method,
				}).Warn("Rate limit exceeded")

				// Set rate limit headers
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.2f", float64(rl.rate)))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("Retry-After", "1")

				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			// Set rate limit headers for successful requests
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.2f", float64(rl.rate)))

			// Periodically cleanup limiters
			go rl.cleanupLimiters()

			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the client IP address from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP if multiple are present
		if firstIP := xff; firstIP != "" {
			return firstIP
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}

// GetStats returns rate limiter statistics
func (rl *RateLimiter) GetStats() map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	return map[string]interface{}{
		"rate_limit":     float64(rl.rate),
		"burst_size":     rl.burst,
		"active_clients": len(rl.limiters),
	}
}
