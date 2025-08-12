package middleware

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/pkg/logger"
)

// WAFMiddleware implements Web Application Firewall functionality with Traefik-style security
// Provides comprehensive protection against common web attacks and vulnerabilities
type WAFMiddleware struct {
	config        WAFConfig
	logger        *logger.Logger
	rules         []WAFRule
	ipWhitelist   map[string]bool
	ipBlacklist   map[string]bool
	rateLimiter   map[string]*WAFRateLimiter
	mutex         sync.RWMutex
	compiledRules []CompiledWAFRule
}

// WAFConfig contains Web Application Firewall configuration
type WAFConfig struct {
	Enabled              bool               `yaml:"enabled"`
	Mode                 string             `yaml:"mode"`             // "block", "monitor", "off"
	MaxRequestSize       int64              `yaml:"max_request_size"` // Maximum request size in bytes
	MaxHeaderSize        int                `yaml:"max_header_size"`  // Maximum header size
	MaxURILength         int                `yaml:"max_uri_length"`   // Maximum URI length
	MaxQueryStringLength int                `yaml:"max_query_string_length"`
	BlockedUserAgents    []string           `yaml:"blocked_user_agents"`
	AllowedMethods       []string           `yaml:"allowed_methods"`
	IPWhitelist          []string           `yaml:"ip_whitelist"`
	IPBlacklist          []string           `yaml:"ip_blacklist"`
	RateLimiting         WAFRateLimitConfig `yaml:"rate_limiting"`
	SecurityHeaders      SecurityHeaders    `yaml:"security_headers"`
	Rules                []WAFRule          `yaml:"rules"`
	GeoblockCountries    []string           `yaml:"geoblock_countries"`
	EnableBotDetection   bool               `yaml:"enable_bot_detection"`
}

// WAFRule defines a WAF protection rule
type WAFRule struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Type        string   `yaml:"type"`     // "regex", "string_match", "size_limit", "rate_limit"
	Target      string   `yaml:"target"`   // "header", "body", "uri", "query", "cookie", "user_agent"
	Pattern     string   `yaml:"pattern"`  // Regex pattern or string to match
	Action      string   `yaml:"action"`   // "block", "monitor", "rate_limit"
	Severity    string   `yaml:"severity"` // "low", "medium", "high", "critical"
	Enabled     bool     `yaml:"enabled"`
	Methods     []string `yaml:"methods"` // HTTP methods this rule applies to
}

// CompiledWAFRule contains compiled regex patterns for performance
type CompiledWAFRule struct {
	Rule  WAFRule
	Regex *regexp.Regexp
}

// WAFRateLimitConfig configures WAF-specific rate limiting
type WAFRateLimitConfig struct {
	Enabled         bool          `yaml:"enabled"`
	RequestsPerMin  int           `yaml:"requests_per_min"`
	WindowSize      time.Duration `yaml:"window_size"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
}

// WAFRateLimiter tracks requests per IP for WAF rate limiting
type WAFRateLimiter struct {
	Requests  []time.Time
	Blocked   bool
	BlockedAt time.Time
}

// SecurityHeaders defines security headers to add
type SecurityHeaders struct {
	Enabled                 bool   `yaml:"enabled"`
	ContentTypeNosniff      bool   `yaml:"content_type_nosniff"`
	FrameOptions            string `yaml:"frame_options"`  // "DENY", "SAMEORIGIN", "ALLOW-FROM"
	XSSProtection           string `yaml:"xss_protection"` // "1; mode=block"
	ContentSecurityPolicy   string `yaml:"content_security_policy"`
	StrictTransportSecurity string `yaml:"strict_transport_security"`
	ReferrerPolicy          string `yaml:"referrer_policy"`
	PermissionsPolicy       string `yaml:"permissions_policy"`
}

// AttackVector represents different types of attacks the WAF can detect
type AttackVector struct {
	Type        string
	Pattern     string
	Description string
	Severity    string
}

// NewWAFMiddleware creates a new WAF middleware with comprehensive protection
func NewWAFMiddleware(config WAFConfig, logger *logger.Logger) (*WAFMiddleware, error) {
	if !config.Enabled {
		return nil, nil
	}

	waf := &WAFMiddleware{
		config:      config,
		logger:      logger,
		rules:       config.Rules,
		ipWhitelist: make(map[string]bool),
		ipBlacklist: make(map[string]bool),
		rateLimiter: make(map[string]*WAFRateLimiter),
	}

	// Populate IP lists
	for _, ip := range config.IPWhitelist {
		waf.ipWhitelist[ip] = true
	}
	for _, ip := range config.IPBlacklist {
		waf.ipBlacklist[ip] = true
	}

	// Compile regex rules for performance
	err := waf.compileRules()
	if err != nil {
		return nil, fmt.Errorf("failed to compile WAF rules: %w", err)
	}

	// Add default attack vector rules
	waf.addDefaultRules()

	// Start cleanup goroutine for rate limiting
	if config.RateLimiting.Enabled {
		go waf.rateLimitCleanup()
	}

	logger.WithFields(map[string]interface{}{
		"mode":             config.Mode,
		"rules":            len(waf.rules),
		"max_request_size": config.MaxRequestSize,
		"rate_limiting":    config.RateLimiting.Enabled,
		"security_headers": config.SecurityHeaders.Enabled,
		"bot_detection":    config.EnableBotDetection,
	}).Info("WAF middleware initialized")

	return waf, nil
}

// WAFProtection returns the WAF middleware handler
func (waf *WAFMiddleware) WAFProtection() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !waf.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Add security headers
			if waf.config.SecurityHeaders.Enabled {
				waf.addSecurityHeaders(w)
			}

			// Check IP whitelist/blacklist
			clientIP := waf.getClientIP(r)
			if waf.ipBlacklist[clientIP] {
				waf.logSecurity("ip_blocked", clientIP, r, "Client IP in blacklist")
				waf.blockRequest(w, "Forbidden", http.StatusForbidden)
				return
			}

			if len(waf.ipWhitelist) > 0 && !waf.ipWhitelist[clientIP] {
				waf.logSecurity("ip_not_whitelisted", clientIP, r, "Client IP not in whitelist")
				waf.blockRequest(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Rate limiting check
			if waf.config.RateLimiting.Enabled {
				if waf.checkRateLimit(clientIP) {
					waf.logSecurity("rate_limit_exceeded", clientIP, r, "Rate limit exceeded")
					waf.blockRequest(w, "Too Many Requests", http.StatusTooManyRequests)
					return
				}
			}

			// Request size validation
			if r.ContentLength > waf.config.MaxRequestSize {
				waf.logSecurity("request_too_large", clientIP, r, fmt.Sprintf("Request size %d exceeds limit %d", r.ContentLength, waf.config.MaxRequestSize))
				waf.blockRequest(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
				return
			}

			// URI length validation
			if len(r.RequestURI) > waf.config.MaxURILength {
				waf.logSecurity("uri_too_long", clientIP, r, fmt.Sprintf("URI length %d exceeds limit %d", len(r.RequestURI), waf.config.MaxURILength))
				waf.blockRequest(w, "URI Too Long", http.StatusRequestURITooLong)
				return
			}

			// Query string length validation
			if len(r.URL.RawQuery) > waf.config.MaxQueryStringLength {
				waf.logSecurity("query_string_too_long", clientIP, r, fmt.Sprintf("Query string length %d exceeds limit %d", len(r.URL.RawQuery), waf.config.MaxQueryStringLength))
				waf.blockRequest(w, "Bad Request", http.StatusBadRequest)
				return
			}

			// HTTP method validation
			if len(waf.config.AllowedMethods) > 0 && !waf.isMethodAllowed(r.Method) {
				waf.logSecurity("method_not_allowed", clientIP, r, fmt.Sprintf("HTTP method %s not allowed", r.Method))
				waf.blockRequest(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}

			// User agent validation
			if waf.isBlockedUserAgent(r.UserAgent()) {
				waf.logSecurity("blocked_user_agent", clientIP, r, fmt.Sprintf("Blocked user agent: %s", r.UserAgent()))
				waf.blockRequest(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Bot detection
			if waf.config.EnableBotDetection && waf.detectBot(r) {
				waf.logSecurity("bot_detected", clientIP, r, "Bot detected")
				// For bots, we might want to apply different rules or rate limits
				w.Header().Set("X-Bot-Detected", "true")
			}

			// Apply WAF rules
			blocked, rule := waf.evaluateRules(r)
			if blocked {
				waf.logSecurity("waf_rule_triggered", clientIP, r, fmt.Sprintf("WAF rule triggered: %s", rule.Name))
				if waf.config.Mode == "block" {
					waf.blockRequest(w, "Forbidden", http.StatusForbidden)
					return
				}
				// In monitor mode, log but don't block
				w.Header().Set("X-WAF-Rule-Triggered", rule.ID)
			}

			// Request passed all checks
			waf.logger.WithFields(map[string]interface{}{
				"client_ip":  clientIP,
				"method":     r.Method,
				"path":       r.URL.Path,
				"user_agent": r.UserAgent(),
				"status":     "allowed",
			}).Debug("Request passed WAF checks")

			next.ServeHTTP(w, r)
		})
	}
}

// compileRules compiles regex patterns in WAF rules for better performance
func (waf *WAFMiddleware) compileRules() error {
	waf.compiledRules = make([]CompiledWAFRule, 0, len(waf.rules))

	for _, rule := range waf.rules {
		if !rule.Enabled {
			continue
		}

		compiled := CompiledWAFRule{Rule: rule}

		if rule.Type == "regex" && rule.Pattern != "" {
			regex, err := regexp.Compile(rule.Pattern)
			if err != nil {
				return fmt.Errorf("failed to compile regex for rule %s: %w", rule.ID, err)
			}
			compiled.Regex = regex
		}

		waf.compiledRules = append(waf.compiledRules, compiled)
	}

	return nil
}

// addDefaultRules adds common attack vector detection rules
func (waf *WAFMiddleware) addDefaultRules() {
	defaultAttackVectors := []AttackVector{
		{"sql_injection", `(?i)(union\s+select|insert\s+into|delete\s+from|drop\s+table|exec\s*\(|script\s*>)`, "SQL Injection Attempt", "high"},
		{"xss", `(?i)(<script|javascript:|on\w+\s*=|<iframe|<object|<embed)`, "Cross-Site Scripting Attempt", "high"},
		{"lfi", `(?i)(\.\.\/|\.\.\\|etc\/passwd|boot\.ini|windows\/system32)`, "Local File Inclusion Attempt", "medium"},
		{"rfi", `(?i)(http:\/\/|https:\/\/|ftp:\/\/).*\.(php|asp|jsp)`, "Remote File Inclusion Attempt", "medium"},
		{"command_injection", `(?i)(\|\s*\w+|\;\s*\w+|\x60\s*\w+|\$\(|\${)`, "Command Injection Attempt", "high"},
		{"directory_traversal", `(?i)(\.\.\/){3,}`, "Directory Traversal Attempt", "medium"},
		{"php_injection", `(?i)(php:\/\/|php_|eval\s*\(|base64_decode|gzinflate)`, "PHP Code Injection Attempt", "high"},
	}

	for _, vector := range defaultAttackVectors {
		rule := WAFRule{
			ID:          "default_" + vector.Type,
			Name:        vector.Description,
			Description: "Default protection rule for " + vector.Type,
			Type:        "regex",
			Target:      "uri,query,body",
			Pattern:     vector.Pattern,
			Action:      "block",
			Severity:    vector.Severity,
			Enabled:     true,
			Methods:     []string{"GET", "POST", "PUT", "PATCH"},
		}

		waf.rules = append(waf.rules, rule)

		// Compile the new rule
		if regex, err := regexp.Compile(rule.Pattern); err == nil {
			waf.compiledRules = append(waf.compiledRules, CompiledWAFRule{
				Rule:  rule,
				Regex: regex,
			})
		}
	}

	waf.logger.WithField("default_rules", len(defaultAttackVectors)).Info("Added default WAF attack vector rules")
}

// evaluateRules checks request against all WAF rules
func (waf *WAFMiddleware) evaluateRules(r *http.Request) (bool, WAFRule) {
	// Read request body if present
	var body []byte
	if r.Body != nil && r.ContentLength > 0 && r.ContentLength < waf.config.MaxRequestSize {
		body, _ = io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewBuffer(body))
	}

	for _, compiled := range waf.compiledRules {
		rule := compiled.Rule

		// Check if rule applies to this method
		if len(rule.Methods) > 0 && !waf.stringInSlice(r.Method, rule.Methods) {
			continue
		}

		// Extract content based on target
		content := waf.extractContent(r, body, rule.Target)

		// Evaluate rule
		matched := false
		switch rule.Type {
		case "regex":
			if compiled.Regex != nil {
				matched = compiled.Regex.MatchString(content)
			}
		case "string_match":
			matched = strings.Contains(strings.ToLower(content), strings.ToLower(rule.Pattern))
		case "size_limit":
			if limit, err := strconv.Atoi(rule.Pattern); err == nil {
				matched = len(content) > limit
			}
		}

		if matched {
			waf.logger.WithFields(map[string]interface{}{
				"rule_id":   rule.ID,
				"rule_name": rule.Name,
				"target":    rule.Target,
				"content":   content[:min(len(content), 100)], // Log first 100 chars
				"severity":  rule.Severity,
				"client_ip": waf.getClientIP(r),
			}).Warn("WAF rule matched")

			return rule.Action == "block", rule
		}
	}

	return false, WAFRule{}
}

// extractContent extracts content from request based on target
func (waf *WAFMiddleware) extractContent(r *http.Request, body []byte, target string) string {
	var content strings.Builder
	targets := strings.Split(target, ",")

	for _, t := range targets {
		switch strings.TrimSpace(t) {
		case "uri":
			content.WriteString(r.RequestURI)
		case "query":
			content.WriteString(r.URL.RawQuery)
		case "body":
			content.Write(body)
		case "user_agent":
			content.WriteString(r.UserAgent())
		case "headers":
			for name, values := range r.Header {
				content.WriteString(name + ":" + strings.Join(values, ","))
			}
		case "cookies":
			for _, cookie := range r.Cookies() {
				content.WriteString(cookie.Name + "=" + cookie.Value)
			}
		}
		content.WriteString(" ")
	}

	return content.String()
}

// addSecurityHeaders adds security headers to response
func (waf *WAFMiddleware) addSecurityHeaders(w http.ResponseWriter) {
	headers := waf.config.SecurityHeaders

	if headers.ContentTypeNosniff {
		w.Header().Set("X-Content-Type-Options", "nosniff")
	}

	if headers.FrameOptions != "" {
		w.Header().Set("X-Frame-Options", headers.FrameOptions)
	}

	if headers.XSSProtection != "" {
		w.Header().Set("X-XSS-Protection", headers.XSSProtection)
	}

	if headers.ContentSecurityPolicy != "" {
		w.Header().Set("Content-Security-Policy", headers.ContentSecurityPolicy)
	}

	if headers.StrictTransportSecurity != "" {
		w.Header().Set("Strict-Transport-Security", headers.StrictTransportSecurity)
	}

	if headers.ReferrerPolicy != "" {
		w.Header().Set("Referrer-Policy", headers.ReferrerPolicy)
	}

	if headers.PermissionsPolicy != "" {
		w.Header().Set("Permissions-Policy", headers.PermissionsPolicy)
	}
}

// checkRateLimit checks if client has exceeded rate limit
func (waf *WAFMiddleware) checkRateLimit(clientIP string) bool {
	waf.mutex.Lock()
	defer waf.mutex.Unlock()

	now := time.Now()
	limiter, exists := waf.rateLimiter[clientIP]

	if !exists {
		limiter = &WAFRateLimiter{
			Requests: make([]time.Time, 0),
		}
		waf.rateLimiter[clientIP] = limiter
	}

	// Clean old requests outside window
	windowStart := now.Add(-waf.config.RateLimiting.WindowSize)
	validRequests := make([]time.Time, 0)
	for _, reqTime := range limiter.Requests {
		if reqTime.After(windowStart) {
			validRequests = append(validRequests, reqTime)
		}
	}
	limiter.Requests = validRequests

	// Add current request
	limiter.Requests = append(limiter.Requests, now)

	// Check if limit exceeded
	if len(limiter.Requests) > waf.config.RateLimiting.RequestsPerMin {
		limiter.Blocked = true
		limiter.BlockedAt = now
		return true
	}

	return false
}

// detectBot detects if request is from a bot
func (waf *WAFMiddleware) detectBot(r *http.Request) bool {
	userAgent := strings.ToLower(r.UserAgent())

	// Common bot indicators
	botIndicators := []string{
		"bot", "crawler", "spider", "scraper", "curl", "wget", "python-requests",
		"java/", "go-http-client", "okhttp", "apache-httpclient",
	}

	for _, indicator := range botIndicators {
		if strings.Contains(userAgent, indicator) {
			return true
		}
	}

	// Check for missing common headers (simple bot detection)
	if r.Header.Get("Accept") == "" || r.Header.Get("Accept-Language") == "" {
		return true
	}

	return false
}

// isMethodAllowed checks if HTTP method is allowed
func (waf *WAFMiddleware) isMethodAllowed(method string) bool {
	for _, allowed := range waf.config.AllowedMethods {
		if allowed == method {
			return true
		}
	}
	return false
}

// isBlockedUserAgent checks if user agent is blocked
func (waf *WAFMiddleware) isBlockedUserAgent(userAgent string) bool {
	userAgentLower := strings.ToLower(userAgent)
	for _, blocked := range waf.config.BlockedUserAgents {
		if strings.Contains(userAgentLower, strings.ToLower(blocked)) {
			return true
		}
	}
	return false
}

// getClientIP extracts client IP from request
func (waf *WAFMiddleware) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
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

	// Extract from RemoteAddr
	if ip := strings.Split(r.RemoteAddr, ":")[0]; ip != "" {
		return ip
	}

	return r.RemoteAddr
}

// blockRequest sends a blocked request response
func (waf *WAFMiddleware) blockRequest(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-WAF-Blocked", "true")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"error":     "request_blocked",
		"message":   message,
		"status":    statusCode,
		"timestamp": time.Now().UTC(),
	}

	// Simple JSON encoding without external dependencies
	w.Write([]byte(fmt.Sprintf(`{"error":"%s","message":"%s","status":%d,"timestamp":"%s"}`,
		response["error"], response["message"], response["status"], response["timestamp"])))
}

// logSecurity logs security events
func (waf *WAFMiddleware) logSecurity(eventType, clientIP string, r *http.Request, message string) {
	waf.logger.WithFields(map[string]interface{}{
		"event_type":     eventType,
		"client_ip":      clientIP,
		"method":         r.Method,
		"path":           r.URL.Path,
		"user_agent":     r.UserAgent(),
		"referer":        r.Referer(),
		"content_length": r.ContentLength,
		"message":        message,
		"timestamp":      time.Now().UTC(),
	}).Warn("WAF Security Event")
}

// rateLimitCleanup periodically cleans up old rate limit data
func (waf *WAFMiddleware) rateLimitCleanup() {
	ticker := time.NewTicker(waf.config.RateLimiting.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		waf.mutex.Lock()
		now := time.Now()
		cleanupCount := 0

		for ip, limiter := range waf.rateLimiter {
			// Remove old requests
			windowStart := now.Add(-waf.config.RateLimiting.WindowSize)
			validRequests := make([]time.Time, 0)
			for _, reqTime := range limiter.Requests {
				if reqTime.After(windowStart) {
					validRequests = append(validRequests, reqTime)
				}
			}
			limiter.Requests = validRequests

			// Remove empty limiters
			if len(limiter.Requests) == 0 && limiter.BlockedAt.Add(time.Hour).Before(now) {
				delete(waf.rateLimiter, ip)
				cleanupCount++
			}
		}

		waf.mutex.Unlock()

		if cleanupCount > 0 {
			waf.logger.WithField("cleaned_ips", cleanupCount).Debug("WAF rate limiter cleanup completed")
		}
	}
}

// Helper functions

func (waf *WAFMiddleware) stringInSlice(str string, slice []string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetStats returns WAF statistics
func (waf *WAFMiddleware) GetStats() map[string]interface{} {
	waf.mutex.RLock()
	defer waf.mutex.RUnlock()

	return map[string]interface{}{
		"enabled":          waf.config.Enabled,
		"mode":             waf.config.Mode,
		"rules":            len(waf.rules),
		"compiled_rules":   len(waf.compiledRules),
		"ip_whitelist":     len(waf.ipWhitelist),
		"ip_blacklist":     len(waf.ipBlacklist),
		"rate_limiters":    len(waf.rateLimiter),
		"max_request_size": waf.config.MaxRequestSize,
		"max_uri_length":   waf.config.MaxURILength,
		"security_headers": waf.config.SecurityHeaders.Enabled,
		"bot_detection":    waf.config.EnableBotDetection,
		"rate_limiting":    waf.config.RateLimiting.Enabled,
	}
}
