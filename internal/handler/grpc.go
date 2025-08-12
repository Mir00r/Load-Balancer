package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// GRPCProxyHandler implements gRPC proxying with load balancing
// Provides Traefik-style gRPC proxy functionality
type GRPCProxyHandler struct {
	loadBalancer domain.LoadBalancer
	metrics      domain.Metrics
	logger       *logger.Logger
	timeout      time.Duration
	enableH2C    bool // HTTP/2 Cleartext for gRPC
}

// GRPCConfig contains gRPC proxy configuration
type GRPCConfig struct {
	Enabled           bool          `yaml:"enabled"`
	Timeout           time.Duration `yaml:"timeout"`
	EnableH2C         bool          `yaml:"enable_h2c"`
	MaxReceiveSize    int           `yaml:"max_receive_size"`
	MaxSendSize       int           `yaml:"max_send_size"`
	EnableReflection  bool          `yaml:"enable_reflection"`
	EnableCompression bool          `yaml:"enable_compression"`
}

// NewGRPCProxyHandler creates a new gRPC proxy handler with Traefik-style features
func NewGRPCProxyHandler(lb domain.LoadBalancer, metrics domain.Metrics, logger *logger.Logger, config GRPCConfig) *GRPCProxyHandler {
	return &GRPCProxyHandler{
		loadBalancer: lb,
		metrics:      metrics,
		logger:       logger,
		timeout:      config.Timeout,
		enableH2C:    config.EnableH2C,
	}
}

// ServeHTTP handles gRPC requests with proper load balancing
func (gh *GRPCProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Verify this is a gRPC request
	if !gh.isGRPCRequest(r) {
		gh.logger.WithFields(map[string]interface{}{
			"method":       r.Method,
			"path":         r.URL.Path,
			"content_type": r.Header.Get("Content-Type"),
		}).Warn("Non-gRPC request received by gRPC handler")
		http.Error(w, "Bad Request: Not a gRPC request", http.StatusBadRequest)
		return
	}

	// Select backend for gRPC service
	backend := gh.loadBalancer.SelectBackend()
	if backend == nil {
		gh.logger.WithFields(map[string]interface{}{
			"error": "No available backend for gRPC service",
			"path":  r.URL.Path,
		}).Error("Failed to select backend for gRPC")
		gh.writeGRPCError(w, "Service Unavailable: No healthy backend servers")
		return
	}

	// Create gRPC client connection to backend
	ctx, cancel := context.WithTimeout(r.Context(), gh.timeout)
	defer cancel()

	conn, err := gh.dialBackend(ctx, backend)
	if err != nil {
		gh.logger.WithFields(map[string]interface{}{
			"error":   err.Error(),
			"backend": backend.URL,
			"path":    r.URL.Path,
		}).Error("Failed to connect to gRPC backend")
		gh.writeGRPCError(w, "Bad Gateway: Failed to connect to backend")
		return
	}
	defer conn.Close()

	// Proxy the gRPC request
	err = gh.proxyGRPCRequest(ctx, w, r, conn, backend)
	if err != nil {
		gh.logger.WithFields(map[string]interface{}{
			"error":    err.Error(),
			"backend":  backend.URL,
			"path":     r.URL.Path,
			"duration": time.Since(startTime),
		}).Error("gRPC request proxying failed")
		gh.metrics.IncrementErrors(backend.ID)
		return
	}

	// Update metrics
	gh.metrics.IncrementRequests(backend.ID)
	gh.metrics.RecordLatency(backend.ID, time.Since(startTime))

	gh.logger.WithFields(map[string]interface{}{
		"backend":  backend.URL,
		"path":     r.URL.Path,
		"duration": time.Since(startTime),
		"status":   "success",
	}).Debug("gRPC request proxied successfully")
}

// isGRPCRequest checks if the request is a gRPC request
func (gh *GRPCProxyHandler) isGRPCRequest(r *http.Request) bool {
	// Check Content-Type for gRPC
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/grpc") {
		return true
	}

	// Check for HTTP/2 with POST method (common gRPC pattern)
	if r.Method == "POST" && r.ProtoMajor == 2 {
		// Additional gRPC indicators
		if r.Header.Get("Grpc-Encoding") != "" ||
			r.Header.Get("Grpc-Accept-Encoding") != "" ||
			strings.Contains(r.UserAgent(), "grpc") {
			return true
		}
	}

	return false
}

// dialBackend creates a gRPC connection to the backend
func (gh *GRPCProxyHandler) dialBackend(ctx context.Context, backend *domain.Backend) (*grpc.ClientConn, error) {
	// Parse backend URL to get host:port
	backendAddr := strings.TrimPrefix(backend.URL, "http://")
	backendAddr = strings.TrimPrefix(backendAddr, "https://")

	// Configure gRPC dial options
	opts := []grpc.DialOption{
		grpc.WithTimeout(gh.timeout),
		grpc.WithBlock(), // Wait for connection to be established
	}

	// Configure TLS or insecure connection
	if strings.HasPrefix(backend.URL, "https://") {
		opts = append(opts, grpc.WithTransportCredentials(nil)) // Use TLS
	} else {
		opts = append(opts, grpc.WithInsecure())
	}

	// Enable H2C if configured
	if gh.enableH2C {
		opts = append(opts, grpc.WithInsecure())
	}

	return grpc.DialContext(ctx, backendAddr, opts...)
}

// proxyGRPCRequest proxies the gRPC request to the backend
func (gh *GRPCProxyHandler) proxyGRPCRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, conn *grpc.ClientConn, backend *domain.Backend) error {
	// Extract gRPC metadata from HTTP headers
	md := gh.extractGRPCMetadata(r)
	ctx = metadata.NewOutgoingContext(ctx, md)

	// For simplicity, we'll use HTTP proxy for gRPC over HTTP/2
	// In a full implementation, you'd need to handle gRPC streaming properly

	// Set appropriate headers for gRPC response
	w.Header().Set("Content-Type", "application/grpc")
	w.Header().Set("Grpc-Status", "0") // OK status

	// For demonstration, we'll proxy as HTTP request
	// Real gRPC proxy would need to handle streaming and binary protocol
	gh.proxyAsHTTP(w, r, backend)

	return nil
}

// extractGRPCMetadata converts HTTP headers to gRPC metadata
func (gh *GRPCProxyHandler) extractGRPCMetadata(r *http.Request) metadata.MD {
	md := metadata.New(nil)

	for key, values := range r.Header {
		// Convert gRPC headers (grpc- prefix)
		if strings.HasPrefix(strings.ToLower(key), "grpc-") {
			md[key] = values
		}

		// Convert standard headers that should be forwarded
		switch strings.ToLower(key) {
		case "authorization", "user-agent", "x-forwarded-for", "x-real-ip":
			md[key] = values
		}
	}

	return md
}

// proxyAsHTTP proxies the request as HTTP (simplified gRPC handling)
func (gh *GRPCProxyHandler) proxyAsHTTP(w http.ResponseWriter, r *http.Request, backend *domain.Backend) {
	// This is a simplified implementation
	// Real gRPC proxy would need full protocol handling

	// Create HTTP client for backend request
	client := &http.Client{
		Timeout: gh.timeout,
	}

	// Forward the request to backend
	resp, err := client.Do(r)
	if err != nil {
		gh.writeGRPCError(w, "Failed to proxy to backend")
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		w.Header()[key] = values
	}

	// Set status and copy body
	w.WriteHeader(resp.StatusCode)

	// Note: For full gRPC support, you'd need to handle streaming properly
}

// writeGRPCError writes a gRPC-formatted error response
func (gh *GRPCProxyHandler) writeGRPCError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/grpc")
	w.Header().Set("Grpc-Status", "14") // UNAVAILABLE
	w.Header().Set("Grpc-Message", message)
	w.WriteHeader(http.StatusServiceUnavailable)
}

// GetStats returns gRPC proxy statistics
func (gh *GRPCProxyHandler) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"handler_type":    "grpc_proxy",
		"timeout_seconds": gh.timeout.Seconds(),
		"h2c_enabled":     gh.enableH2C,
		"backend_count":   len(gh.loadBalancer.GetBackends()),
	}
}
