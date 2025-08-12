// Package plugin provides built-in plugins for common functionality like logging, CORS, compression, and metrics
// These plugins demonstrate the plugin interface and provide essential load balancer features
package plugin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mir00r/load-balancer/pkg/logger"
)

// LoggingPlugin provides request logging functionality
type LoggingPlugin struct {
	name    string
	version string
	logger  *logger.Logger
	enabled bool
}

// NewLoggingPlugin creates a new logging plugin
func NewLoggingPlugin(logger *logger.Logger) *LoggingPlugin {
	return &LoggingPlugin{
		name:    "request-logger",
		version: "1.0.0",
		logger:  logger,
		enabled: true,
	}
}

// Name returns the plugin name
func (lp *LoggingPlugin) Name() string {
	return lp.name
}

// Version returns the plugin version
func (lp *LoggingPlugin) Version() string {
	return lp.version
}

// Type returns the plugin type
func (lp *LoggingPlugin) Type() PluginType {
	return PluginTypeHTTP
}

// Priority returns execution priority
func (lp *LoggingPlugin) Priority() int {
	return 50 // Medium priority
}

// Init initializes the plugin
func (lp *LoggingPlugin) Init(config map[string]interface{}) error {
	lp.logger.Debug("Logging plugin initialized")
	return nil
}

// Execute runs the logging plugin
func (lp *LoggingPlugin) Execute(ctx context.Context, pluginCtx *PluginContext) (*PluginResult, error) {
	if !lp.enabled {
		return &PluginResult{Continue: true}, nil
	}

	lp.logger.WithFields(map[string]interface{}{
		"request_id": pluginCtx.RequestID,
		"method":     pluginCtx.Method,
		"path":       pluginCtx.Path,
		"client_ip":  pluginCtx.ClientIP,
		"user_agent": pluginCtx.UserAgent,
	}).Info("Request processed by logging plugin")

	return &PluginResult{
		Continue:     true,
		ModifyHeader: map[string]string{"X-Request-Logged": "true"},
		Metadata:     map[string]interface{}{"logged_at": time.Now()},
	}, nil
}

// Cleanup performs cleanup
func (lp *LoggingPlugin) Cleanup() error {
	lp.enabled = false
	return nil
}

// CanHandle returns true if plugin can handle the request
func (lp *LoggingPlugin) CanHandle(method, path string) bool {
	return lp.enabled // Log all requests
}

// MetricsPlugin provides request metrics collection
type MetricsPlugin struct {
	name    string
	version string
	enabled bool
}

// NewMetricsPlugin creates a new metrics plugin
func NewMetricsPlugin() *MetricsPlugin {
	return &MetricsPlugin{
		name:    "request-metrics",
		version: "1.0.0",
		enabled: true,
	}
}

// Name returns the plugin name
func (mp *MetricsPlugin) Name() string {
	return mp.name
}

// Version returns the plugin version
func (mp *MetricsPlugin) Version() string {
	return mp.version
}

// Type returns the plugin type
func (mp *MetricsPlugin) Type() PluginType {
	return PluginTypeMetrics
}

// Priority returns execution priority
func (mp *MetricsPlugin) Priority() int {
	return 10 // Low priority (run last)
}

// Init initializes the plugin
func (mp *MetricsPlugin) Init(config map[string]interface{}) error {
	return nil
}

// Execute runs the metrics plugin
func (mp *MetricsPlugin) Execute(ctx context.Context, pluginCtx *PluginContext) (*PluginResult, error) {
	if !mp.enabled {
		return &PluginResult{Continue: true}, nil
	}

	// Mock metrics collection - in reality, this would update Prometheus metrics
	return &PluginResult{
		Continue:     true,
		ModifyHeader: map[string]string{"X-Metrics-Collected": "true"},
		Metadata: map[string]interface{}{
			"metrics_plugin": mp.name,
			"processed_at":   time.Now(),
		},
	}, nil
}

// Cleanup performs cleanup
func (mp *MetricsPlugin) Cleanup() error {
	mp.enabled = false
	return nil
}

// CanHandle returns true if plugin can handle the request
func (mp *MetricsPlugin) CanHandle(method, path string) bool {
	return mp.enabled // Collect metrics for all requests
}

// CORSPlugin provides Cross-Origin Resource Sharing support
type CORSPlugin struct {
	name           string
	version        string
	enabled        bool
	allowedOrigins []string
	allowedMethods []string
	allowedHeaders []string
	maxAge         int
}

// NewCORSPlugin creates a new CORS plugin
func NewCORSPlugin() *CORSPlugin {
	return &CORSPlugin{
		name:           "cors-handler",
		version:        "1.0.0",
		enabled:        true,
		allowedOrigins: []string{"*"},
		allowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD"},
		allowedHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With"},
		maxAge:         3600, // 1 hour
	}
}

// Name returns the plugin name
func (cp *CORSPlugin) Name() string {
	return cp.name
}

// Version returns the plugin version
func (cp *CORSPlugin) Version() string {
	return cp.version
}

// Type returns the plugin type
func (cp *CORSPlugin) Type() PluginType {
	return PluginTypeHTTP
}

// Priority returns execution priority
func (cp *CORSPlugin) Priority() int {
	return 90 // High priority (run early)
}

// Init initializes the plugin
func (cp *CORSPlugin) Init(config map[string]interface{}) error {
	if origins, ok := config["allowed_origins"].([]string); ok {
		cp.allowedOrigins = origins
	}
	if methods, ok := config["allowed_methods"].([]string); ok {
		cp.allowedMethods = methods
	}
	if headers, ok := config["allowed_headers"].([]string); ok {
		cp.allowedHeaders = headers
	}
	if maxAge, ok := config["max_age"].(int); ok {
		cp.maxAge = maxAge
	}
	return nil
}

// Execute runs the CORS plugin
func (cp *CORSPlugin) Execute(ctx context.Context, pluginCtx *PluginContext) (*PluginResult, error) {
	if !cp.enabled {
		return &PluginResult{Continue: true}, nil
	}

	headers := make(map[string]string)

	// Set CORS headers
	if len(cp.allowedOrigins) > 0 {
		if cp.allowedOrigins[0] == "*" {
			headers["Access-Control-Allow-Origin"] = "*"
		} else {
			// Check if origin is allowed
			origin := ""
			if origins, exists := pluginCtx.Headers["Origin"]; exists && len(origins) > 0 {
				origin = origins[0]
			}

			for _, allowedOrigin := range cp.allowedOrigins {
				if allowedOrigin == origin {
					headers["Access-Control-Allow-Origin"] = origin
					break
				}
			}
		}
	}

	if len(cp.allowedMethods) > 0 {
		headers["Access-Control-Allow-Methods"] = strings.Join(cp.allowedMethods, ", ")
	}

	if len(cp.allowedHeaders) > 0 {
		headers["Access-Control-Allow-Headers"] = strings.Join(cp.allowedHeaders, ", ")
	}

	if cp.maxAge > 0 {
		headers["Access-Control-Max-Age"] = fmt.Sprintf("%d", cp.maxAge)
	}

	// Handle preflight requests
	result := &PluginResult{
		Continue:     true,
		ModifyHeader: headers,
		Metadata:     map[string]interface{}{"cors_processed": true},
	}

	if pluginCtx.Method == "OPTIONS" {
		// This is a preflight request
		result.SetStatus = 200
		result.SetBody = []byte("")
		// Continue processing to allow other plugins to handle OPTIONS
	}

	return result, nil
}

// Cleanup performs cleanup
func (cp *CORSPlugin) Cleanup() error {
	cp.enabled = false
	return nil
}

// CanHandle returns true if plugin can handle the request
func (cp *CORSPlugin) CanHandle(method, path string) bool {
	return cp.enabled // Handle all requests for CORS
}

// CompressionPlugin provides response compression
type CompressionPlugin struct {
	name         string
	version      string
	enabled      bool
	minSize      int
	contentTypes []string
}

// NewCompressionPlugin creates a new compression plugin
func NewCompressionPlugin() *CompressionPlugin {
	return &CompressionPlugin{
		name:    "response-compression",
		version: "1.0.0",
		enabled: true,
		minSize: 1024, // Only compress responses > 1KB
		contentTypes: []string{
			"text/html",
			"text/css",
			"text/javascript",
			"application/javascript",
			"application/json",
			"text/xml",
			"application/xml",
		},
	}
}

// Name returns the plugin name
func (cmp *CompressionPlugin) Name() string {
	return cmp.name
}

// Version returns the plugin version
func (cmp *CompressionPlugin) Version() string {
	return cmp.version
}

// Type returns the plugin type
func (cmp *CompressionPlugin) Type() PluginType {
	return PluginTypeHTTP
}

// Priority returns execution priority
func (cmp *CompressionPlugin) Priority() int {
	return 20 // Low priority (run towards the end)
}

// Init initializes the plugin
func (cmp *CompressionPlugin) Init(config map[string]interface{}) error {
	if minSize, ok := config["min_size"].(int); ok {
		cmp.minSize = minSize
	}
	if contentTypes, ok := config["content_types"].([]string); ok {
		cmp.contentTypes = contentTypes
	}
	return nil
}

// Execute runs the compression plugin
func (cmp *CompressionPlugin) Execute(ctx context.Context, pluginCtx *PluginContext) (*PluginResult, error) {
	if !cmp.enabled {
		return &PluginResult{Continue: true}, nil
	}

	// Check if client accepts compression
	acceptsGzip := false
	if acceptEncoding, exists := pluginCtx.Headers["Accept-Encoding"]; exists {
		for _, encoding := range acceptEncoding {
			if strings.Contains(strings.ToLower(encoding), "gzip") {
				acceptsGzip = true
				break
			}
		}
	}

	headers := make(map[string]string)
	metadata := make(map[string]interface{})

	if acceptsGzip {
		// Mark for compression (actual compression would happen in response phase)
		headers["Content-Encoding"] = "gzip"
		headers["Vary"] = "Accept-Encoding"
		metadata["compress_response"] = true
		metadata["compression_type"] = "gzip"
	}

	metadata["compression_plugin"] = cmp.name

	return &PluginResult{
		Continue:     true,
		ModifyHeader: headers,
		Metadata:     metadata,
	}, nil
}

// Cleanup performs cleanup
func (cmp *CompressionPlugin) Cleanup() error {
	cmp.enabled = false
	return nil
}

// CanHandle returns true if plugin can handle the request
func (cmp *CompressionPlugin) CanHandle(method, path string) bool {
	// Only compress GET requests typically
	return cmp.enabled && (method == "GET" || method == "POST")
}
