package middleware

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mir00r/load-balancer/internal/middleware"
	"github.com/mir00r/load-balancer/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestWAFLogger creates a logger instance for WAF testing
func createTestWAFLogger() *logger.Logger {
	config := logger.Config{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	}
	testLogger, _ := logger.New(config)
	return testLogger
}

// TestWAFMiddlewareConfiguration tests WAF middleware configuration
func TestWAFMiddlewareConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      middleware.WAFConfig
		expectError bool
		expectNil   bool
	}{
		{
			name: "Enabled WAF with basic configuration",
			config: middleware.WAFConfig{
				Enabled:        true,
				Mode:           "block",
				MaxRequestSize: 1024 * 1024, // 1MB
				AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
				SecurityHeaders: middleware.SecurityHeaders{
					Enabled:            true,
					ContentTypeNosniff: true,
					FrameOptions:       "DENY",
					XSSProtection:      "1; mode=block",
				},
			},
			expectError: false,
			expectNil:   false,
		},
		{
			name: "Disabled WAF",
			config: middleware.WAFConfig{
				Enabled: false,
			},
			expectError: false,
			expectNil:   true,
		},
		{
			name: "WAF with custom rules",
			config: middleware.WAFConfig{
				Enabled: true,
				Mode:    "monitor",
				Rules: []middleware.WAFRule{
					{
						ID:          "custom_rule_1",
						Name:        "SQL Injection Detection",
						Description: "Detects SQL injection attempts",
						Type:        "regex",
						Target:      "body",
						Pattern:     `(?i)(union|select|insert|delete|drop|exec|script)`,
						Action:      "block",
						Severity:    "high",
						Enabled:     true,
						Methods:     []string{"POST", "PUT"},
					},
				},
			},
			expectError: false,
			expectNil:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testLogger := createTestWAFLogger()

			wafMiddleware, err := middleware.NewWAFMiddleware(tt.config, testLogger)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, wafMiddleware)
			} else {
				assert.NoError(t, err)
				if tt.expectNil {
					assert.Nil(t, wafMiddleware)
				} else {
					assert.NotNil(t, wafMiddleware)
				}
			}
		})
	}
}

// TestWAFBasicProtection tests basic WAF protection features
func TestWAFBasicProtection(t *testing.T) {
	t.Parallel()

	config := middleware.WAFConfig{
		Enabled:              true,
		Mode:                 "block",
		MaxRequestSize:       1024, // 1KB
		MaxHeaderSize:        512,  // 512 bytes
		MaxURILength:         100,  // 100 characters
		MaxQueryStringLength: 200,  // 200 characters
		AllowedMethods:       []string{"GET", "POST", "PUT", "DELETE"},
		BlockedUserAgents:    []string{"badbot", "malicious-crawler"},
		IPBlacklist:          []string{"192.168.1.100", "10.0.0.50"},
	}

	testLogger := createTestWAFLogger()
	wafMiddleware, err := middleware.NewWAFMiddleware(config, testLogger)
	require.NoError(t, err)
	require.NotNil(t, wafMiddleware)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	middlewareHandler := wafMiddleware.WAFProtection()(testHandler)

	tests := []struct {
		name           string
		setupRequest   func() *http.Request
		expectedStatus int
		description    string
	}{
		{
			name: "Valid request",
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "/api/test", nil)
			},
			expectedStatus: http.StatusOK,
			description:    "Should allow valid requests",
		},
		{
			name: "Blocked method",
			setupRequest: func() *http.Request {
				return httptest.NewRequest("TRACE", "/api/test", nil)
			},
			expectedStatus: http.StatusMethodNotAllowed,
			description:    "Should block non-allowed HTTP methods",
		},
		{
			name: "Blocked user agent",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/api/test", nil)
				req.Header.Set("User-Agent", "badbot/1.0")
				return req
			},
			expectedStatus: http.StatusForbidden,
			description:    "Should block requests from blacklisted user agents",
		},
		{
			name: "Long URI",
			setupRequest: func() *http.Request {
				longPath := "/api/" + strings.Repeat("a", 200)
				return httptest.NewRequest("GET", longPath, nil)
			},
			expectedStatus: http.StatusRequestURITooLong,
			description:    "Should block requests with URI exceeding max length",
		},
		{
			name: "Large request body",
			setupRequest: func() *http.Request {
				largeBody := bytes.NewReader(make([]byte, 2048)) // 2KB body
				return httptest.NewRequest("POST", "/api/test", largeBody)
			},
			expectedStatus: http.StatusRequestEntityTooLarge,
			description:    "Should block requests exceeding max size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupRequest()
			recorder := httptest.NewRecorder()

			middlewareHandler.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code, tt.description)
		})
	}
}

// TestWAFSecurityHeaders tests security headers functionality
func TestWAFSecurityHeaders(t *testing.T) {
	t.Parallel()

	config := middleware.WAFConfig{
		Enabled: true,
		Mode:    "monitor",
		SecurityHeaders: middleware.SecurityHeaders{
			Enabled:                 true,
			ContentTypeNosniff:      true,
			FrameOptions:            "DENY",
			XSSProtection:           "1; mode=block",
			ContentSecurityPolicy:   "default-src 'self'",
			StrictTransportSecurity: "max-age=31536000; includeSubDomains",
			ReferrerPolicy:          "strict-origin-when-cross-origin",
			PermissionsPolicy:       "geolocation=(), microphone=(), camera=()",
		},
	}

	testLogger := createTestWAFLogger()
	wafMiddleware, err := middleware.NewWAFMiddleware(config, testLogger)
	require.NoError(t, err)
	require.NotNil(t, wafMiddleware)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := wafMiddleware.WAFProtection()(testHandler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	recorder := httptest.NewRecorder()

	middlewareHandler.ServeHTTP(recorder, req)

	// Verify security headers are added
	assert.Equal(t, http.StatusOK, recorder.Code, "Request should succeed")
	assert.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", recorder.Header().Get("X-Frame-Options"))
	assert.Equal(t, "1; mode=block", recorder.Header().Get("X-XSS-Protection"))
	assert.Equal(t, "default-src 'self'", recorder.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "max-age=31536000; includeSubDomains", recorder.Header().Get("Strict-Transport-Security"))
	assert.Equal(t, "strict-origin-when-cross-origin", recorder.Header().Get("Referrer-Policy"))
	assert.Equal(t, "geolocation=(), microphone=(), camera=()", recorder.Header().Get("Permissions-Policy"))
}

// TestWAFRateLimiting tests WAF rate limiting functionality
func TestWAFRateLimiting(t *testing.T) {
	t.Parallel()

	config := middleware.WAFConfig{
		Enabled: true,
		Mode:    "block",
		RateLimiting: middleware.WAFRateLimitConfig{
			Enabled:         true,
			RequestsPerMin:  5,
			WindowSize:      1 * time.Minute,
			CleanupInterval: 30 * time.Second,
		},
	}

	testLogger := createTestWAFLogger()
	wafMiddleware, err := middleware.NewWAFMiddleware(config, testLogger)
	require.NoError(t, err)
	require.NotNil(t, wafMiddleware)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := wafMiddleware.WAFProtection()(testHandler)

	// Make requests up to the limit
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		recorder := httptest.NewRecorder()

		middlewareHandler.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code,
			"Requests within limit should succeed (request %d)", i+1)
	}

	// Next request should be rate limited
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	recorder := httptest.NewRecorder()

	middlewareHandler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusTooManyRequests, recorder.Code,
		"Request exceeding rate limit should be blocked")
}

// TestWAFCustomRules tests custom WAF rule evaluation
func TestWAFCustomRules(t *testing.T) {
	t.Parallel()

	config := middleware.WAFConfig{
		Enabled: true,
		Mode:    "block",
		Rules: []middleware.WAFRule{
			{
				ID:          "sql_injection_test",
				Name:        "SQL Injection Detection",
				Description: "Detects basic SQL injection patterns",
				Type:        "regex",
				Target:      "body",
				Pattern:     `(?i)(union.*select|insert.*into|drop.*table)`,
				Action:      "block",
				Severity:    "high",
				Enabled:     true,
				Methods:     []string{"POST", "PUT"},
			},
			{
				ID:          "xss_test",
				Name:        "XSS Detection",
				Description: "Detects basic XSS patterns",
				Type:        "regex",
				Target:      "query",
				Pattern:     `(?i)(<script|javascript:|on\w+\s*=)`,
				Action:      "block",
				Severity:    "medium",
				Enabled:     true,
			},
		},
	}

	testLogger := createTestWAFLogger()
	wafMiddleware, err := middleware.NewWAFMiddleware(config, testLogger)
	require.NoError(t, err)
	require.NotNil(t, wafMiddleware)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := wafMiddleware.WAFProtection()(testHandler)

	tests := []struct {
		name           string
		setupRequest   func() *http.Request
		expectedStatus int
		description    string
	}{
		{
			name: "Safe request",
			setupRequest: func() *http.Request {
				return httptest.NewRequest("POST", "/api/test",
					bytes.NewReader([]byte(`{"name": "John", "age": 30}`)))
			},
			expectedStatus: http.StatusOK,
			description:    "Should allow safe requests",
		},
		{
			name: "SQL injection in body",
			setupRequest: func() *http.Request {
				return httptest.NewRequest("POST", "/api/test",
					bytes.NewReader([]byte(`{"query": "SELECT * FROM users UNION SELECT password FROM admin"}`)))
			},
			expectedStatus: http.StatusForbidden,
			description:    "Should block SQL injection attempts in request body",
		},
		{
			name: "XSS in query parameter",
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "/api/test?search=<script>alert('xss')</script>", nil)
			},
			expectedStatus: http.StatusForbidden,
			description:    "Should block XSS attempts in query parameters",
		},
		{
			name: "SQL injection rule not applied to GET",
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "/api/test",
					bytes.NewReader([]byte(`{"query": "SELECT * FROM users UNION SELECT password FROM admin"}`)))
			},
			expectedStatus: http.StatusOK,
			description:    "SQL injection rule should not apply to GET requests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupRequest()
			recorder := httptest.NewRecorder()

			middlewareHandler.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code, tt.description)
		})
	}
}

// TestWAFIPFiltering tests IP whitelist and blacklist functionality
func TestWAFIPFiltering(t *testing.T) {
	t.Parallel()

	config := middleware.WAFConfig{
		Enabled:     true,
		Mode:        "block",
		IPWhitelist: []string{"127.0.0.1", "192.168.1.10"},
		IPBlacklist: []string{"10.0.0.1", "172.16.0.100"},
	}

	testLogger := createTestWAFLogger()
	wafMiddleware, err := middleware.NewWAFMiddleware(config, testLogger)
	require.NoError(t, err)
	require.NotNil(t, wafMiddleware)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := wafMiddleware.WAFProtection()(testHandler)

	tests := []struct {
		name           string
		clientIP       string
		expectedStatus int
		description    string
	}{
		{
			name:           "Whitelisted IP",
			clientIP:       "127.0.0.1:12345",
			expectedStatus: http.StatusOK,
			description:    "Should allow whitelisted IPs",
		},
		{
			name:           "Blacklisted IP",
			clientIP:       "10.0.0.1:54321",
			expectedStatus: http.StatusForbidden,
			description:    "Should block blacklisted IPs",
		},
		{
			name:           "Regular IP",
			clientIP:       "203.0.113.1:8080",
			expectedStatus: http.StatusOK,
			description:    "Should allow IPs not in whitelist or blacklist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/test", nil)
			req.RemoteAddr = tt.clientIP

			recorder := httptest.NewRecorder()
			middlewareHandler.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code, tt.description)
		})
	}
}

// TestWAFConcurrency tests WAF middleware under concurrent load
func TestWAFConcurrency(t *testing.T) {
	t.Parallel()

	config := middleware.WAFConfig{
		Enabled:        true,
		Mode:           "block",
		MaxRequestSize: 1024 * 1024, // 1MB
		AllowedMethods: []string{"GET", "POST"},
		RateLimiting: middleware.WAFRateLimitConfig{
			Enabled:         true,
			RequestsPerMin:  100,
			WindowSize:      1 * time.Minute,
			CleanupInterval: 30 * time.Second,
		},
	}

	testLogger := createTestWAFLogger()
	wafMiddleware, err := middleware.NewWAFMiddleware(config, testLogger)
	require.NoError(t, err)
	require.NotNil(t, wafMiddleware)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := wafMiddleware.WAFProtection()(testHandler)

	const numGoroutines = 20
	const requestsPerGoroutine = 5

	var wg sync.WaitGroup
	results := make(chan int, numGoroutines*requestsPerGoroutine)
	errors := make(chan error, numGoroutines*requestsPerGoroutine)

	// Launch concurrent requests
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				req := httptest.NewRequest("GET", fmt.Sprintf("/api/test-%d-%d", goroutineID, j), nil)
				req.RemoteAddr = fmt.Sprintf("192.168.1.%d:12345", goroutineID+1)
				recorder := httptest.NewRecorder()

				middlewareHandler.ServeHTTP(recorder, req)
				results <- recorder.Code

				if recorder.Code >= 500 {
					errors <- fmt.Errorf("server error: %d", recorder.Code)
				}
			}
		}(i)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Verify no server errors occurred
	for err := range errors {
		t.Errorf("Concurrent execution error: %v", err)
	}

	// Count results
	statusCounts := make(map[int]int)
	totalRequests := 0
	for statusCode := range results {
		statusCounts[statusCode]++
		totalRequests++
	}

	expectedTotal := numGoroutines * requestsPerGoroutine
	assert.Equal(t, expectedTotal, totalRequests,
		"Should process all %d concurrent requests", expectedTotal)

	// Most requests should succeed (some might be rate limited)
	assert.Greater(t, statusCounts[http.StatusOK], 0,
		"At least some requests should succeed")
}

// BenchmarkWAFMiddleware benchmarks WAF middleware performance
func BenchmarkWAFMiddleware(b *testing.B) {
	config := middleware.WAFConfig{
		Enabled:        true,
		Mode:           "block",
		MaxRequestSize: 1024 * 1024,
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
		Rules: []middleware.WAFRule{
			{
				ID:      "test_rule",
				Type:    "regex",
				Target:  "uri",
				Pattern: `(?i)/(admin|private)/`,
				Action:  "block",
				Enabled: true,
			},
		},
	}

	testLogger := createTestWAFLogger()
	wafMiddleware, err := middleware.NewWAFMiddleware(config, testLogger)
	require.NoError(b, err)
	require.NotNil(b, wafMiddleware)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := wafMiddleware.WAFProtection()(testHandler)

	b.Run("ValidRequest", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				req := httptest.NewRequest("GET", "/api/test", nil)
				recorder := httptest.NewRecorder()
				middlewareHandler.ServeHTTP(recorder, req)
			}
		})
	})

	b.Run("BlockedRequest", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				req := httptest.NewRequest("GET", "/admin/dashboard", nil)
				recorder := httptest.NewRecorder()
				middlewareHandler.ServeHTTP(recorder, req)
			}
		})
	})
}

// TestWAFMonitorMode tests WAF in monitor mode
func TestWAFMonitorMode(t *testing.T) {
	t.Parallel()

	config := middleware.WAFConfig{
		Enabled: true,
		Mode:    "monitor", // Monitor mode should not block requests
		Rules: []middleware.WAFRule{
			{
				ID:      "monitor_test",
				Type:    "regex",
				Target:  "uri",
				Pattern: `(?i)/dangerous/`,
				Action:  "block",
				Enabled: true,
			},
		},
	}

	testLogger := createTestWAFLogger()
	wafMiddleware, err := middleware.NewWAFMiddleware(config, testLogger)
	require.NoError(t, err)
	require.NotNil(t, wafMiddleware)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := wafMiddleware.WAFProtection()(testHandler)

	// In monitor mode, even dangerous requests should pass through
	req := httptest.NewRequest("GET", "/dangerous/path", nil)
	recorder := httptest.NewRecorder()

	middlewareHandler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code,
		"Monitor mode should allow requests while logging them")
}
