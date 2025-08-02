package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// LoggingMiddleware provides structured request logging
func LoggingMiddleware(logger *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Create request context
			requestCtx := domain.NewRequestContext(r)

			// Add request context to the request
			ctx := context.WithValue(r.Context(), "requestContext", requestCtx)
			r = r.WithContext(ctx)

			// Create response writer wrapper to capture status code
			wrappedWriter := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Create request-specific logger
			requestLogger := logger.RequestLogger(
				requestCtx.RequestID,
				requestCtx.Method,
				requestCtx.Path,
				requestCtx.RemoteAddr,
			)

			requestLogger.Info("Request started")

			// Process request
			next.ServeHTTP(wrappedWriter, r)

			// Log request completion
			duration := time.Since(start)

			logLevel := "info"
			if wrappedWriter.statusCode >= 400 {
				logLevel = "warn"
			}
			if wrappedWriter.statusCode >= 500 {
				logLevel = "error"
			}

			logEntry := requestLogger.WithFields(map[string]interface{}{
				"status_code":   wrappedWriter.statusCode,
				"duration_ms":   duration.Milliseconds(),
				"response_size": wrappedWriter.size,
			})

			switch logLevel {
			case "error":
				logEntry.Error("Request completed with error")
			case "warn":
				logEntry.Warn("Request completed with warning")
			default:
				logEntry.Info("Request completed")
			}
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture response details
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int64
}

// WriteHeader captures the status code
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write captures the response size
func (rw *responseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += int64(size)
	return size, err
}

// RecoveryMiddleware provides panic recovery with logging
func RecoveryMiddleware(logger *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log the panic
					requestCtx, _ := r.Context().Value("requestContext").(*domain.RequestContext)
					var requestID string
					if requestCtx != nil {
						requestID = requestCtx.RequestID
					}

					logger.WithFields(map[string]interface{}{
						"request_id": requestID,
						"path":       r.URL.Path,
						"method":     r.Method,
						"panic":      err,
					}).Error("Panic recovered in request handler")

					// Return internal server error
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// CORSMiddleware adds CORS headers
func CORSMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Set CORS headers
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

			// Handle preflight OPTIONS request
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeadersMiddleware adds security headers
func SecurityHeadersMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Security headers
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			next.ServeHTTP(w, r)
		})
	}
}

// TimeoutMiddleware adds request timeout handling
func TimeoutMiddleware(timeout time.Duration, logger *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create timeout context
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			// Replace request context
			r = r.WithContext(ctx)

			// Channel to signal completion
			done := make(chan struct{})

			// Start request processing in goroutine
			go func() {
				defer close(done)
				next.ServeHTTP(w, r)
			}()

			// Wait for completion or timeout
			select {
			case <-done:
				// Request completed normally
			case <-ctx.Done():
				// Request timed out
				requestCtx, _ := r.Context().Value("requestContext").(*domain.RequestContext)
				var requestID string
				if requestCtx != nil {
					requestID = requestCtx.RequestID
				}

				logger.WithFields(map[string]interface{}{
					"request_id": requestID,
					"path":       r.URL.Path,
					"method":     r.Method,
					"timeout":    timeout.String(),
				}).Warn("Request timed out")

				http.Error(w, "Request Timeout", http.StatusRequestTimeout)
			}
		})
	}
}
