package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/mir00r/load-balancer/pkg/logger"
)

// AuthConfig defines authentication configuration
type AuthConfig struct {
	Enabled bool              `yaml:"enabled"`
	Type    string            `yaml:"type"`  // "basic", "bearer", "jwt"
	Users   map[string]string `yaml:"users"` // username -> password hash
	Realm   string            `yaml:"realm"`
	// Path-based auth rules
	PathRules map[string]PathAuth `yaml:"path_rules"`
}

// PathAuth defines authentication rules per path
type PathAuth struct {
	Required bool              `yaml:"required"`
	Users    map[string]string `yaml:"users"`
	Methods  []string          `yaml:"methods"`
}

// AuthMiddleware implements NGINX-style authentication
type AuthMiddleware struct {
	config AuthConfig
	logger *logger.Logger
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(config AuthConfig, logger *logger.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		config: config,
		logger: logger,
	}
}

// BasicAuth returns the basic authentication middleware
func (am *AuthMiddleware) BasicAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !am.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Check if authentication is required for this path
			authRequired, users := am.isAuthRequired(r.URL.Path, r.Method)
			if !authRequired {
				next.ServeHTTP(w, r)
				return
			}

			// Get Authorization header
			auth := r.Header.Get("Authorization")
			if auth == "" {
				am.requireAuth(w, "Authorization required")
				return
			}

			// Check Basic Auth
			if !strings.HasPrefix(auth, "Basic ") {
				am.requireAuth(w, "Basic authentication required")
				return
			}

			// Decode credentials
			encoded := strings.TrimPrefix(auth, "Basic ")
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				am.logger.WithFields(map[string]interface{}{
					"error": err.Error(),
					"path":  r.URL.Path,
				}).Warn("Failed to decode basic auth credentials")
				am.requireAuth(w, "Invalid credentials format")
				return
			}

			// Parse username:password
			credentials := string(decoded)
			parts := strings.SplitN(credentials, ":", 2)
			if len(parts) != 2 {
				am.requireAuth(w, "Invalid credentials format")
				return
			}

			username, password := parts[0], parts[1]

			// Validate credentials
			if !am.validateCredentials(username, password, users) {
				am.logger.WithFields(map[string]interface{}{
					"username":  username,
					"path":      r.URL.Path,
					"client_ip": am.getClientIP(r),
				}).Warn("Authentication failed")
				am.requireAuth(w, "Invalid credentials")
				return
			}

			am.logger.WithFields(map[string]interface{}{
				"username": username,
				"path":     r.URL.Path,
			}).Info("Authentication successful")

			// Add user info to request context if needed
			r.Header.Set("X-Authenticated-User", username)

			next.ServeHTTP(w, r)
		})
	}
}

// isAuthRequired checks if authentication is required for a path and method
func (am *AuthMiddleware) isAuthRequired(path, method string) (bool, map[string]string) {
	// Check path-specific rules first
	for pathPattern, rule := range am.config.PathRules {
		if matched, _ := matchPath(pathPattern, path); matched {
			if !rule.Required {
				return false, nil
			}
			// Check if method is in allowed methods
			if len(rule.Methods) > 0 {
				methodAllowed := false
				for _, allowedMethod := range rule.Methods {
					if strings.EqualFold(allowedMethod, method) {
						methodAllowed = true
						break
					}
				}
				if !methodAllowed {
					return false, nil
				}
			}
			// Use path-specific users if available, otherwise global users
			if len(rule.Users) > 0 {
				return true, rule.Users
			}
			return true, am.config.Users
		}
	}

	// Default to global auth settings
	return am.config.Enabled, am.config.Users
}

// validateCredentials validates username and password
func (am *AuthMiddleware) validateCredentials(username, password string, users map[string]string) bool {
	if users == nil {
		users = am.config.Users
	}

	storedHash, exists := users[username]
	if !exists {
		return false
	}

	// Simple hash comparison (in production, use proper password hashing)
	passwordHash := am.hashPassword(password)

	// Use constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare([]byte(passwordHash), []byte(storedHash)) == 1
}

// hashPassword creates a simple hash of the password
// In production, use bcrypt or argon2
func (am *AuthMiddleware) hashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return base64.StdEncoding.EncodeToString(hash[:])
}

// requireAuth sends a 401 Unauthorized response
func (am *AuthMiddleware) requireAuth(w http.ResponseWriter, message string) {
	realm := am.config.Realm
	if realm == "" {
		realm = "Load Balancer"
	}

	w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
	w.Header().Set("Content-Type", "application/json")
	http.Error(w, `{"error": "`+message+`"}`, http.StatusUnauthorized)
}

// getClientIP extracts the client IP address
func (am *AuthMiddleware) getClientIP(r *http.Request) string {
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
	if ip := strings.Split(r.RemoteAddr, ":"); len(ip) > 0 {
		return ip[0]
	}
	return r.RemoteAddr
}

// BearerAuth returns the bearer token authentication middleware
func (am *AuthMiddleware) BearerAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !am.config.Enabled || am.config.Type != "bearer" {
				next.ServeHTTP(w, r)
				return
			}

			// Check if authentication is required for this path
			authRequired, _ := am.isAuthRequired(r.URL.Path, r.Method)
			if !authRequired {
				next.ServeHTTP(w, r)
				return
			}

			// Get Authorization header
			auth := r.Header.Get("Authorization")
			if auth == "" {
				am.requireBearerAuth(w, "Bearer token required")
				return
			}

			// Check Bearer token
			if !strings.HasPrefix(auth, "Bearer ") {
				am.requireBearerAuth(w, "Bearer token required")
				return
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			if !am.validateBearerToken(token) {
				am.logger.WithFields(map[string]interface{}{
					"path":      r.URL.Path,
					"client_ip": am.getClientIP(r),
				}).Warn("Bearer token validation failed")
				am.requireBearerAuth(w, "Invalid token")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// validateBearerToken validates a bearer token
func (am *AuthMiddleware) validateBearerToken(token string) bool {
	// Simple token validation - in production, validate JWT or check against database
	// For now, check if token exists in users map as a valid token
	for _, storedToken := range am.config.Users {
		if subtle.ConstantTimeCompare([]byte(token), []byte(storedToken)) == 1 {
			return true
		}
	}
	return false
}

// requireBearerAuth sends a 401 Unauthorized response for bearer auth
func (am *AuthMiddleware) requireBearerAuth(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.Header().Set("Content-Type", "application/json")
	http.Error(w, `{"error": "`+message+`"}`, http.StatusUnauthorized)
}

// CreatePasswordHash creates a password hash for configuration
func CreatePasswordHash(password string) string {
	hash := sha256.Sum256([]byte(password))
	return base64.StdEncoding.EncodeToString(hash[:])
}

// GetStats returns authentication statistics
func (am *AuthMiddleware) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"enabled":     am.config.Enabled,
		"type":        am.config.Type,
		"realm":       am.config.Realm,
		"users_count": len(am.config.Users),
		"path_rules":  len(am.config.PathRules),
	}
}
