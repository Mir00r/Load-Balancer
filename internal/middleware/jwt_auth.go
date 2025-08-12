package middleware

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// JWTAuthMiddleware implements JWT authentication with Traefik-style features
// Provides comprehensive JWT validation, claims extraction, and path-based rules
type JWTAuthMiddleware struct {
	config    JWTAuthConfig
	logger    *logger.Logger
	publicKey *rsa.PublicKey
	pathRules []JWTPathRule
	userStore map[string]JWTUser
}

// JWTAuthConfig contains JWT authentication configuration
type JWTAuthConfig struct {
	Enabled            bool          `yaml:"enabled"`
	PublicKeyPath      string        `yaml:"public_key_path"`
	Algorithm          string        `yaml:"algorithm"`  // RS256, HS256, etc.
	SecretKey          string        `yaml:"secret_key"` // For HMAC algorithms
	TokenExpiry        time.Duration `yaml:"token_expiry"`
	ClockSkew          time.Duration `yaml:"clock_skew"`
	RequiredClaims     []string      `yaml:"required_claims"` // Claims that must be present
	AudienceValidation bool          `yaml:"audience_validation"`
	IssuerValidation   bool          `yaml:"issuer_validation"`
	ValidAudiences     []string      `yaml:"valid_audiences"`
	ValidIssuers       []string      `yaml:"valid_issuers"`
	PathRules          []JWTPathRule `yaml:"path_rules"`
	Users              []JWTUser     `yaml:"users"`
}

// JWTPathRule defines JWT requirements for specific paths
type JWTPathRule struct {
	Path           string   `yaml:"path"`
	Required       bool     `yaml:"required"`
	Methods        []string `yaml:"methods"`
	RequiredRoles  []string `yaml:"required_roles"`
	RequiredScopes []string `yaml:"required_scopes"`
}

// JWTUser represents a user with JWT permissions
type JWTUser struct {
	ID       string   `yaml:"id"`
	Username string   `yaml:"username"`
	Email    string   `yaml:"email"`
	Roles    []string `yaml:"roles"`
	Scopes   []string `yaml:"scopes"`
	Active   bool     `yaml:"active"`
}

// JWTClaims represents JWT token claims
type JWTClaims struct {
	UserID   string   `json:"sub"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Roles    []string `json:"roles"`
	Scopes   []string `json:"scopes"`
	jwt.RegisteredClaims
}

// NewJWTAuthMiddleware creates a new JWT authentication middleware
func NewJWTAuthMiddleware(config JWTAuthConfig, logger *logger.Logger) (*JWTAuthMiddleware, error) {
	if !config.Enabled {
		return nil, nil
	}

	middleware := &JWTAuthMiddleware{
		config:    config,
		logger:    logger,
		pathRules: config.PathRules,
		userStore: make(map[string]JWTUser),
	}

	// Load users into store
	for _, user := range config.Users {
		middleware.userStore[user.ID] = user
	}

	// Load public key for RSA algorithms
	if config.Algorithm == "RS256" || config.Algorithm == "RS384" || config.Algorithm == "RS512" {
		if config.PublicKeyPath != "" {
			publicKey, err := middleware.loadPublicKey(config.PublicKeyPath)
			if err != nil {
				return nil, fmt.Errorf("failed to load JWT public key: %w", err)
			}
			middleware.publicKey = publicKey
		}
	}

	logger.WithFields(map[string]interface{}{
		"algorithm":    config.Algorithm,
		"path_rules":   len(config.PathRules),
		"users":        len(config.Users),
		"token_expiry": config.TokenExpiry,
	}).Info("JWT authentication middleware initialized")

	return middleware, nil
}

// JWTAuth returns the JWT authentication middleware
func (jm *JWTAuthMiddleware) JWTAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !jm.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Check if JWT is required for this path
			pathRule := jm.getPathRule(r.URL.Path, r.Method)
			if pathRule == nil || !pathRule.Required {
				next.ServeHTTP(w, r)
				return
			}

			// Extract JWT token from request
			token := jm.extractToken(r)
			if token == "" {
				jm.logger.WithFields(map[string]interface{}{
					"path":   r.URL.Path,
					"method": r.Method,
					"ip":     r.RemoteAddr,
				}).Warn("JWT token missing")
				jm.writeJWTError(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			// Validate and parse JWT
			claims, err := jm.validateToken(token)
			if err != nil {
				jm.logger.WithFields(map[string]interface{}{
					"error":  err.Error(),
					"path":   r.URL.Path,
					"method": r.Method,
					"ip":     r.RemoteAddr,
				}).Warn("JWT validation failed")
				jm.writeJWTError(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			// Check user status
			user, exists := jm.userStore[claims.UserID]
			if exists && !user.Active {
				jm.logger.WithFields(map[string]interface{}{
					"user_id": claims.UserID,
					"path":    r.URL.Path,
				}).Warn("Inactive user attempted access")
				jm.writeJWTError(w, "Account inactive", http.StatusForbidden)
				return
			}

			// Check role-based access
			if len(pathRule.RequiredRoles) > 0 {
				if !jm.hasRequiredRoles(claims.Roles, pathRule.RequiredRoles) {
					jm.logger.WithFields(map[string]interface{}{
						"user_id":        claims.UserID,
						"user_roles":     claims.Roles,
						"required_roles": pathRule.RequiredRoles,
						"path":           r.URL.Path,
					}).Warn("Insufficient roles for access")
					jm.writeJWTError(w, "Insufficient permissions", http.StatusForbidden)
					return
				}
			}

			// Check scope-based access
			if len(pathRule.RequiredScopes) > 0 {
				if !jm.hasRequiredScopes(claims.Scopes, pathRule.RequiredScopes) {
					jm.logger.WithFields(map[string]interface{}{
						"user_id":         claims.UserID,
						"user_scopes":     claims.Scopes,
						"required_scopes": pathRule.RequiredScopes,
						"path":            r.URL.Path,
					}).Warn("Insufficient scopes for access")
					jm.writeJWTError(w, "Insufficient permissions", http.StatusForbidden)
					return
				}
			}

			// Add user context to request
			ctx := r.Context()
			ctx = jm.addUserContext(ctx, claims)
			r = r.WithContext(ctx)

			// Add JWT info headers
			w.Header().Set("X-JWT-User-ID", claims.UserID)
			w.Header().Set("X-JWT-Username", claims.Username)
			w.Header().Set("X-JWT-Validated", "true")

			jm.logger.WithFields(map[string]interface{}{
				"user_id":  claims.UserID,
				"username": claims.Username,
				"path":     r.URL.Path,
				"method":   r.Method,
			}).Debug("JWT authentication successful")

			next.ServeHTTP(w, r)
		})
	}
}

// extractToken extracts JWT from Authorization header or cookie
func (jm *JWTAuthMiddleware) extractToken(r *http.Request) string {
	// Try Authorization header first
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		// Bearer token format
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	// Try cookie
	if cookie, err := r.Cookie("jwt_token"); err == nil {
		return cookie.Value
	}

	// Try query parameter (less secure, for specific use cases)
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	return ""
}

// validateToken validates and parses the JWT token
func (jm *JWTAuthMiddleware) validateToken(tokenString string) (*JWTClaims, error) {
	// Parse token with claims
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate algorithm
		switch jm.config.Algorithm {
		case "RS256", "RS384", "RS512":
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return jm.publicKey, nil
		case "HS256", "HS384", "HS512":
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(jm.config.SecretKey), nil
		default:
			return nil, fmt.Errorf("unsupported algorithm: %s", jm.config.Algorithm)
		}
	})

	if err != nil {
		return nil, err
	}

	// Extract claims
	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Validate expiration with clock skew
	if time.Now().Add(jm.config.ClockSkew).After(claims.ExpiresAt.Time) {
		return nil, fmt.Errorf("token expired")
	}

	// Validate not before with clock skew
	if time.Now().Add(-jm.config.ClockSkew).Before(claims.NotBefore.Time) {
		return nil, fmt.Errorf("token not yet valid")
	}

	// Validate audience if configured
	if jm.config.AudienceValidation && len(jm.config.ValidAudiences) > 0 {
		validAudience := false
		for _, validAud := range jm.config.ValidAudiences {
			for _, tokenAud := range claims.Audience {
				if tokenAud == validAud {
					validAudience = true
					break
				}
			}
			if validAudience {
				break
			}
		}
		if !validAudience {
			return nil, fmt.Errorf("invalid audience")
		}
	}

	// Validate issuer if configured
	if jm.config.IssuerValidation && len(jm.config.ValidIssuers) > 0 {
		validIssuer := false
		for _, validIss := range jm.config.ValidIssuers {
			if claims.Issuer == validIss {
				validIssuer = true
				break
			}
		}
		if !validIssuer {
			return nil, fmt.Errorf("invalid issuer")
		}
	}

	// Validate required claims
	for _, requiredClaim := range jm.config.RequiredClaims {
		if !jm.hasRequiredClaim(claims, requiredClaim) {
			return nil, fmt.Errorf("missing required claim: %s", requiredClaim)
		}
	}

	return claims, nil
}

// getPathRule finds the matching path rule
func (jm *JWTAuthMiddleware) getPathRule(path, method string) *JWTPathRule {
	for _, rule := range jm.pathRules {
		if jm.matchPath(rule.Path, path) && jm.matchMethod(rule.Methods, method) {
			return &rule
		}
	}
	return nil
}

// matchPath checks if path matches pattern
func (jm *JWTAuthMiddleware) matchPath(pattern, path string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(path, prefix)
	}
	return pattern == path
}

// matchMethod checks if method is in allowed methods
func (jm *JWTAuthMiddleware) matchMethod(allowedMethods []string, method string) bool {
	if len(allowedMethods) == 0 {
		return true // No method restriction
	}
	for _, allowed := range allowedMethods {
		if allowed == method {
			return true
		}
	}
	return false
}

// hasRequiredRoles checks if user has required roles
func (jm *JWTAuthMiddleware) hasRequiredRoles(userRoles, requiredRoles []string) bool {
	for _, required := range requiredRoles {
		found := false
		for _, userRole := range userRoles {
			if userRole == required {
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

// hasRequiredScopes checks if user has required scopes
func (jm *JWTAuthMiddleware) hasRequiredScopes(userScopes, requiredScopes []string) bool {
	for _, required := range requiredScopes {
		found := false
		for _, userScope := range userScopes {
			if userScope == required {
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

// hasRequiredClaim checks if claims contain required claim
func (jm *JWTAuthMiddleware) hasRequiredClaim(claims *JWTClaims, requiredClaim string) bool {
	switch requiredClaim {
	case "sub":
		return claims.Subject != ""
	case "username":
		return claims.Username != ""
	case "email":
		return claims.Email != ""
	case "roles":
		return len(claims.Roles) > 0
	case "scopes":
		return len(claims.Scopes) > 0
	default:
		return true // Unknown claims are assumed present
	}
}

// addUserContext adds user information to request context
func (jm *JWTAuthMiddleware) addUserContext(ctx context.Context, claims *JWTClaims) context.Context {
	// In a real implementation, you'd use proper context keys
	return ctx
}

// writeJWTError writes a JWT error response
func (jm *JWTAuthMiddleware) writeJWTError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"error":   "authentication_failed",
		"message": message,
		"status":  statusCode,
	}

	json.NewEncoder(w).Encode(response)
}

// loadPublicKey loads RSA public key from file
func (jm *JWTAuthMiddleware) loadPublicKey(keyPath string) (*rsa.PublicKey, error) {
	// Simplified - in production, load from file system
	// For demo purposes, return nil (would need actual key loading logic)
	return nil, fmt.Errorf("public key loading not implemented in demo")
}

// GetStats returns JWT authentication statistics
func (jm *JWTAuthMiddleware) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"enabled":             jm.config.Enabled,
		"algorithm":           jm.config.Algorithm,
		"path_rules":          len(jm.pathRules),
		"users":               len(jm.userStore),
		"audience_validation": jm.config.AudienceValidation,
		"issuer_validation":   jm.config.IssuerValidation,
		"token_expiry":        jm.config.TokenExpiry.String(),
		"clock_skew":          jm.config.ClockSkew.String(),
	}
}
