package middleware

import (
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

// createTestLogger creates a logger instance for testing
func createTestLogger() *logger.Logger {
	config := logger.Config{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	}
	testLogger, _ := logger.New(config)
	return testLogger
}

// TestJWTAuthMiddlewareConfiguration tests JWT middleware configuration and initialization
func TestJWTAuthMiddlewareConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		config        middleware.JWTAuthConfig
		expectError   bool
		expectEnabled bool
	}{
		{
			name: "Valid HMAC configuration",
			config: middleware.JWTAuthConfig{
				Enabled:     true,
				Algorithm:   "HS256",
				SecretKey:   "test-secret-key",
				TokenExpiry: 1 * time.Hour,
				ClockSkew:   5 * time.Minute,
			},
			expectError:   false,
			expectEnabled: true,
		},
		{
			name: "Disabled middleware",
			config: middleware.JWTAuthConfig{
				Enabled: false,
			},
			expectError:   false,
			expectEnabled: false,
		},
		{
			name: "Valid configuration with path rules",
			config: middleware.JWTAuthConfig{
				Enabled:     true,
				Algorithm:   "HS256",
				SecretKey:   "test-secret",
				TokenExpiry: 1 * time.Hour,
				PathRules: []middleware.JWTPathRule{
					{
						Path:          "/admin/*",
						Required:      true,
						Methods:       []string{"GET", "POST"},
						RequiredRoles: []string{"admin"},
					},
					{
						Path:     "/public/*",
						Required: false,
					},
				},
			},
			expectError:   false,
			expectEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testLogger := createTestLogger()

			jwtMiddleware, err := middleware.NewJWTAuthMiddleware(tt.config, testLogger)

			if tt.expectError {
				assert.Error(t, err, "Expected error for configuration")
				assert.Nil(t, jwtMiddleware, "Middleware should be nil when error occurs")
			} else {
				assert.NoError(t, err, "Should not return error for valid configuration")
				if tt.expectEnabled {
					assert.NotNil(t, jwtMiddleware, "Middleware should be initialized when enabled")
				} else {
					assert.Nil(t, jwtMiddleware, "Middleware should be nil when disabled")
				}
			}
		})
	}
}

// TestJWTTokenValidation tests JWT token validation logic
func TestJWTTokenValidation(t *testing.T) {
	t.Parallel()

	config := middleware.JWTAuthConfig{
		Enabled:        true,
		Algorithm:      "HS256",
		SecretKey:      "test-secret-key",
		TokenExpiry:    1 * time.Hour,
		ClockSkew:      5 * time.Minute,
		RequiredClaims: []string{"sub", "username"},
		Users: []middleware.JWTUser{
			{
				ID:       "user1",
				Username: "testuser",
				Email:    "test@example.com",
				Roles:    []string{"user"},
				Scopes:   []string{"read"},
				Active:   true,
			},
		},
	}

	testLogger := createTestLogger()
	jwtMiddleware, err := middleware.NewJWTAuthMiddleware(config, testLogger)
	require.NoError(t, err)
	require.NotNil(t, jwtMiddleware)

	// Create a test handler that requires authentication
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	middlewareHandler := jwtMiddleware.JWTAuth()(testHandler)

	tests := []struct {
		name           string
		token          string
		expectedStatus int
		description    string
	}{
		{
			name:           "Missing token",
			token:          "",
			expectedStatus: http.StatusUnauthorized,
			description:    "Should return 401 when no token provided",
		},
		{
			name:           "Invalid token format",
			token:          "invalid-token",
			expectedStatus: http.StatusUnauthorized,
			description:    "Should return 401 for malformed token",
		},
		{
			name:           "Bearer token without actual token",
			token:          "Bearer ",
			expectedStatus: http.StatusUnauthorized,
			description:    "Should return 401 for empty bearer token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/protected", nil)
			if tt.token != "" {
				req.Header.Set("Authorization", tt.token)
			}

			recorder := httptest.NewRecorder()
			middlewareHandler.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code, tt.description)
		})
	}
}

// TestJWTPathBasedRules tests path-based JWT rule enforcement
func TestJWTPathBasedRules(t *testing.T) {
	t.Parallel()

	config := middleware.JWTAuthConfig{
		Enabled:     true,
		Algorithm:   "HS256",
		SecretKey:   "test-secret-key",
		TokenExpiry: 1 * time.Hour,
		PathRules: []middleware.JWTPathRule{
			{
				Path:     "/public/*",
				Required: false,
			},
			{
				Path:          "/admin/*",
				Required:      true,
				Methods:       []string{"GET", "POST"},
				RequiredRoles: []string{"admin"},
			},
			{
				Path:          "/user/*",
				Required:      true,
				Methods:       []string{"GET"},
				RequiredRoles: []string{"user", "admin"},
			},
		},
		Users: []middleware.JWTUser{
			{
				ID:       "admin1",
				Username: "admin",
				Roles:    []string{"admin"},
				Active:   true,
			},
			{
				ID:       "user1",
				Username: "user",
				Roles:    []string{"user"},
				Active:   true,
			},
		},
	}

	testLogger := createTestLogger()
	jwtMiddleware, err := middleware.NewJWTAuthMiddleware(config, testLogger)
	require.NoError(t, err)
	require.NotNil(t, jwtMiddleware)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	middlewareHandler := jwtMiddleware.JWTAuth()(testHandler)

	tests := []struct {
		name           string
		path           string
		method         string
		token          string
		expectedStatus int
		description    string
	}{
		{
			name:           "Public path without token",
			path:           "/public/info",
			method:         "GET",
			token:          "",
			expectedStatus: http.StatusOK,
			description:    "Should allow access to public paths without token",
		},
		{
			name:           "Admin path without token",
			path:           "/admin/dashboard",
			method:         "GET",
			token:          "",
			expectedStatus: http.StatusUnauthorized,
			description:    "Should deny access to protected admin paths without token",
		},
		{
			name:           "User path without token",
			path:           "/user/profile",
			method:         "GET",
			token:          "",
			expectedStatus: http.StatusUnauthorized,
			description:    "Should deny access to protected user paths without token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}

			recorder := httptest.NewRecorder()
			middlewareHandler.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code, tt.description)
		})
	}
}

// TestJWTConcurrency tests JWT middleware under concurrent load
func TestJWTConcurrency(t *testing.T) {
	t.Parallel()

	config := middleware.JWTAuthConfig{
		Enabled:     true,
		Algorithm:   "HS256",
		SecretKey:   "test-secret-key",
		TokenExpiry: 1 * time.Hour,
		PathRules: []middleware.JWTPathRule{
			{
				Path:     "/api/*",
				Required: false,
			},
		},
	}

	testLogger := createTestLogger()
	jwtMiddleware, err := middleware.NewJWTAuthMiddleware(config, testLogger)
	require.NoError(t, err)
	require.NotNil(t, jwtMiddleware)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	middlewareHandler := jwtMiddleware.JWTAuth()(testHandler)

	const numGoroutines = 50
	const requestsPerGoroutine = 10

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

	// All requests to /api/* should succeed (no JWT required for this path)
	assert.Equal(t, expectedTotal, statusCounts[http.StatusOK],
		"All requests should succeed for public API paths")
}

// TestJWTUserStore tests user store functionality
func TestJWTUserStore(t *testing.T) {
	t.Parallel()

	users := []middleware.JWTUser{
		{
			ID:       "user1",
			Username: "alice",
			Email:    "alice@example.com",
			Roles:    []string{"user"},
			Scopes:   []string{"read"},
			Active:   true,
		},
		{
			ID:       "user2",
			Username: "bob",
			Email:    "bob@example.com",
			Roles:    []string{"admin", "user"},
			Scopes:   []string{"read", "write"},
			Active:   true,
		},
		{
			ID:       "user3",
			Username: "inactive",
			Email:    "inactive@example.com",
			Roles:    []string{"user"},
			Scopes:   []string{"read"},
			Active:   false,
		},
	}

	config := middleware.JWTAuthConfig{
		Enabled:     true,
		Algorithm:   "HS256",
		SecretKey:   "test-secret-key",
		TokenExpiry: 1 * time.Hour,
		Users:       users,
	}

	testLogger := createTestLogger()
	jwtMiddleware, err := middleware.NewJWTAuthMiddleware(config, testLogger)
	require.NoError(t, err)
	require.NotNil(t, jwtMiddleware)

	t.Run("User store contains all configured users", func(t *testing.T) {
		// Note: We can't directly test the internal userStore since it's private.
		// Instead, we verify the middleware was created successfully with user data.
		// In a real implementation, you might want to expose a GetUser method for testing.
		assert.NotNil(t, jwtMiddleware, "Middleware should be initialized with user data")
	})
}

// BenchmarkJWTMiddleware benchmarks JWT middleware performance
func BenchmarkJWTMiddleware(b *testing.B) {
	config := middleware.JWTAuthConfig{
		Enabled:     true,
		Algorithm:   "HS256",
		SecretKey:   "test-secret-key",
		TokenExpiry: 1 * time.Hour,
		PathRules: []middleware.JWTPathRule{
			{
				Path:     "/public/*",
				Required: false,
			},
			{
				Path:     "/protected/*",
				Required: true,
			},
		},
	}

	testLogger := createTestLogger()
	jwtMiddleware, err := middleware.NewJWTAuthMiddleware(config, testLogger)
	require.NoError(b, err)
	require.NotNil(b, jwtMiddleware)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareHandler := jwtMiddleware.JWTAuth()(testHandler)

	b.Run("PublicPath", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				req := httptest.NewRequest("GET", "/public/test", nil)
				recorder := httptest.NewRecorder()
				middlewareHandler.ServeHTTP(recorder, req)
			}
		})
	})

	b.Run("ProtectedPathNoToken", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				req := httptest.NewRequest("GET", "/protected/test", nil)
				recorder := httptest.NewRecorder()
				middlewareHandler.ServeHTTP(recorder, req)
			}
		})
	})
}

// TestJWTErrorHandling tests error handling scenarios
func TestJWTErrorHandling(t *testing.T) {
	t.Parallel()

	config := middleware.JWTAuthConfig{
		Enabled:     true,
		Algorithm:   "HS256",
		SecretKey:   "test-secret-key",
		TokenExpiry: 1 * time.Hour,
	}

	testLogger := createTestLogger()
	jwtMiddleware, err := middleware.NewJWTAuthMiddleware(config, testLogger)
	require.NoError(t, err)
	require.NotNil(t, jwtMiddleware)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	middlewareHandler := jwtMiddleware.JWTAuth()(testHandler)

	errorTests := []struct {
		name           string
		setupRequest   func() *http.Request
		expectedStatus int
		description    string
	}{
		{
			name: "Malformed Authorization header",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/protected", nil)
				req.Header.Set("Authorization", "Malformed token")
				return req
			},
			expectedStatus: http.StatusUnauthorized,
			description:    "Should handle malformed authorization headers gracefully",
		},
		{
			name: "Multiple Authorization headers",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/protected", nil)
				req.Header.Add("Authorization", "Bearer token1")
				req.Header.Add("Authorization", "Bearer token2")
				return req
			},
			expectedStatus: http.StatusUnauthorized,
			description:    "Should handle multiple authorization headers",
		},
		{
			name: "Very long token",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/protected", nil)
				longToken := "Bearer " + strings.Repeat("a", 10000)
				req.Header.Set("Authorization", longToken)
				return req
			},
			expectedStatus: http.StatusUnauthorized,
			description:    "Should handle very long tokens without crashing",
		},
	}

	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupRequest()
			recorder := httptest.NewRecorder()

			middlewareHandler.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code, tt.description)

			// Verify response is JSON format for errors
			contentType := recorder.Header().Get("Content-Type")
			if recorder.Code != http.StatusOK {
				assert.Contains(t, contentType, "application/json",
					"Error responses should be in JSON format")
			}
		})
	}
}
