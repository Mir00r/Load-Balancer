package handler

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/mir00r/load-balancer/pkg/logger"
)

// RewriteRule represents a URL rewrite rule
type RewriteRule struct {
	Pattern     *regexp.Regexp
	Replacement string
	Flags       []string
	Conditions  []RewriteCondition
}

// RewriteCondition represents a condition for URL rewriting
type RewriteCondition struct {
	TestString string
	Pattern    *regexp.Regexp
	Flags      []string
}

// URLRewriteHandler handles URL rewriting and advanced routing
type URLRewriteHandler struct {
	logger      *logger.Logger
	rules       []*RewriteRule
	nextHandler http.Handler
}

// NewURLRewriteHandler creates a new URL rewrite handler
func NewURLRewriteHandler(logger *logger.Logger, nextHandler http.Handler) *URLRewriteHandler {
	return &URLRewriteHandler{
		logger:      logger,
		rules:       make([]*RewriteRule, 0),
		nextHandler: nextHandler,
	}
}

// AddRule adds a rewrite rule
func (urh *URLRewriteHandler) AddRule(pattern, replacement string, flags []string) error {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}

	rule := &RewriteRule{
		Pattern:     regex,
		Replacement: replacement,
		Flags:       flags,
		Conditions:  make([]RewriteCondition, 0),
	}

	urh.rules = append(urh.rules, rule)
	return nil
}

// AddCondition adds a condition to the last rule
func (urh *URLRewriteHandler) AddCondition(testString, pattern string, flags []string) error {
	if len(urh.rules) == 0 {
		urh.logger.Warn("No rules available to add condition to")
		return nil
	}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}

	condition := RewriteCondition{
		TestString: testString,
		Pattern:    regex,
		Flags:      flags,
	}

	lastRuleIndex := len(urh.rules) - 1
	urh.rules[lastRuleIndex].Conditions = append(urh.rules[lastRuleIndex].Conditions, condition)
	return nil
}

// ServeHTTP processes URL rewriting
func (urh *URLRewriteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	originalURL := r.URL.Path

	for _, rule := range urh.rules {
		// Check conditions first
		if !urh.checkConditions(rule.Conditions, r) {
			continue
		}

		// Apply the rule
		if rule.Pattern.MatchString(r.URL.Path) {
			newPath := rule.Pattern.ReplaceAllString(r.URL.Path, rule.Replacement)

			// Handle different flags
			for _, flag := range rule.Flags {
				switch flag {
				case "L": // Last rule
					r.URL.Path = newPath
					urh.logRewrite(originalURL, newPath, "rewrite")
					urh.nextHandler.ServeHTTP(w, r)
					return
				case "R": // Redirect (302)
					http.Redirect(w, r, newPath, http.StatusFound)
					urh.logRewrite(originalURL, newPath, "redirect")
					return
				case "R=301": // Permanent redirect
					http.Redirect(w, r, newPath, http.StatusMovedPermanently)
					urh.logRewrite(originalURL, newPath, "redirect_permanent")
					return
				case "F": // Forbidden
					http.Error(w, "Forbidden", http.StatusForbidden)
					urh.logRewrite(originalURL, newPath, "forbidden")
					return
				case "G": // Gone
					http.Error(w, "Gone", http.StatusGone)
					urh.logRewrite(originalURL, newPath, "gone")
					return
				}
			}

			// Default behavior: rewrite and continue
			r.URL.Path = newPath
			urh.logRewrite(originalURL, newPath, "rewrite")
		}
	}

	// Continue to next handler
	urh.nextHandler.ServeHTTP(w, r)
}

// checkConditions checks if all conditions are met
func (urh *URLRewriteHandler) checkConditions(conditions []RewriteCondition, r *http.Request) bool {
	for _, condition := range conditions {
		testValue := urh.getTestValue(condition.TestString, r)
		if !condition.Pattern.MatchString(testValue) {
			return false
		}
	}
	return true
}

// getTestValue gets the actual value to test based on the test string
func (urh *URLRewriteHandler) getTestValue(testString string, r *http.Request) string {
	switch {
	case strings.HasPrefix(testString, "%{HTTP_"):
		headerName := strings.TrimSuffix(strings.TrimPrefix(testString, "%{HTTP_"), "}")
		return r.Header.Get(headerName)
	case testString == "%{REQUEST_METHOD}":
		return r.Method
	case testString == "%{REQUEST_URI}":
		return r.RequestURI
	case testString == "%{QUERY_STRING}":
		return r.URL.RawQuery
	case testString == "%{REMOTE_ADDR}":
		return r.RemoteAddr
	case testString == "%{REQUEST_FILENAME}":
		return r.URL.Path
	case strings.HasPrefix(testString, "%{ENV:"):
		// Environment variable (would need implementation)
		return ""
	default:
		return testString
	}
}

// logRewrite logs URL rewrite operations
func (urh *URLRewriteHandler) logRewrite(original, rewritten, action string) {
	urh.logger.WithFields(map[string]interface{}{
		"original_url":  original,
		"rewritten_url": rewritten,
		"action":        action,
	}).Info("URL rewrite applied")
}

// Common rewrite patterns

// AddSPARewrite adds Single Page Application rewrite rule
func (urh *URLRewriteHandler) AddSPARewrite() error {
	// Rewrite all non-file requests to index.html for SPA
	return urh.AddRule(`^/(?!api/|static/|assets/).*$`, "/index.html", []string{"L"})
}

// AddWWWRedirect adds www to non-www redirect
func (urh *URLRewriteHandler) AddWWWRedirect() error {
	// Would need host header checking in conditions
	return urh.AddRule(`^(.*)$`, "http://www.example.com$1", []string{"R=301"})
}

// AddHTTPSRedirect adds HTTP to HTTPS redirect
func (urh *URLRewriteHandler) AddHTTPSRedirect() error {
	// Would need scheme checking in conditions
	return urh.AddRule(`^(.*)$`, "https://example.com$1", []string{"R=301"})
}

// AddAPIVersioning adds API versioning rewrite
func (urh *URLRewriteHandler) AddAPIVersioning() error {
	// Rewrite /api/users to /api/v1/users if no version specified
	return urh.AddRule(`^/api/(?!v\d+/)(.*)$`, "/api/v1/$1", []string{"L"})
}

// AddTrailingSlashRedirect adds trailing slash handling
func (urh *URLRewriteHandler) AddTrailingSlashRedirect() error {
	// Redirect directories without trailing slash to with trailing slash
	return urh.AddRule(`^([^.]*[^/])$`, "$1/", []string{"R=301"})
}

// ProxyPassHandler handles proxy_pass style routing
type ProxyPassHandler struct {
	logger      *logger.Logger
	routes      map[string]string // path -> backend URL
	stripPrefix map[string]string // path -> prefix to strip
}

// NewProxyPassHandler creates a new proxy pass handler
func NewProxyPassHandler(logger *logger.Logger) *ProxyPassHandler {
	return &ProxyPassHandler{
		logger:      logger,
		routes:      make(map[string]string),
		stripPrefix: make(map[string]string),
	}
}

// AddRoute adds a proxy pass route
func (pph *ProxyPassHandler) AddRoute(path, backend string, stripPrefix string) {
	pph.routes[path] = backend
	if stripPrefix != "" {
		pph.stripPrefix[path] = stripPrefix
	}
}

// ServeHTTP handles proxy pass routing
func (pph *ProxyPassHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	originalPath := r.URL.Path

	// Find the longest matching route
	var matchedPath, backend string
	for path, backendURL := range pph.routes {
		if strings.HasPrefix(originalPath, path) && len(path) > len(matchedPath) {
			matchedPath = path
			backend = backendURL
		}
	}

	if backend == "" {
		http.NotFound(w, r)
		return
	}

	// Strip prefix if configured
	if prefix, exists := pph.stripPrefix[matchedPath]; exists {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
	}

	// Parse backend URL
	backendURL, err := url.Parse(backend)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		pph.logger.WithFields(map[string]interface{}{
			"error":   err.Error(),
			"backend": backend,
		}).Error("Failed to parse backend URL")
		return
	}

	// Set up proxy request
	r.URL.Scheme = backendURL.Scheme
	r.URL.Host = backendURL.Host
	r.URL.Path = strings.TrimSuffix(backendURL.Path, "/") + "/" + strings.TrimPrefix(r.URL.Path, "/")

	// Log the proxy pass
	pph.logger.WithFields(map[string]interface{}{
		"original_path": originalPath,
		"backend":       backend,
		"final_url":     r.URL.String(),
	}).Info("Proxy pass routing")

	// This would need actual proxy implementation
	// For now, just return success
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Proxied to: " + r.URL.String()))
}
