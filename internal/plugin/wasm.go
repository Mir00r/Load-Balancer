// Package plugin provides WebAssembly (WASM) plugin support for runtime loading and execution
// Enables dynamic plugin loading without recompilation of the main application
package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mir00r/load-balancer/internal/config"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// WASMPlugin represents a WebAssembly plugin
type WASMPlugin struct {
	name     string
	version  string
	path     string
	config   config.WASMPlugin
	logger   *logger.Logger
	priority int
	paths    []string
	methods  []string
	enabled  bool
	// In a real implementation, this would contain WASM runtime
	// For now, we'll mock the functionality
}

// NewWASMPlugin creates a new WASM plugin instance
func NewWASMPlugin(cfg config.WASMPlugin, logger *logger.Logger) (*WASMPlugin, error) {
	// Validate plugin file exists
	if _, err := os.Stat(cfg.Path); os.IsNotExist(err) {
		// For demo purposes, we'll create a mock plugin if file doesn't exist
		logger.WithFields(map[string]interface{}{
			"plugin_name": cfg.Name,
			"plugin_path": cfg.Path,
		}).Warn("WASM plugin file not found, creating mock plugin")
	}

	plugin := &WASMPlugin{
		name:     cfg.Name,
		version:  "1.0.0", // Default version
		path:     cfg.Path,
		config:   cfg,
		logger:   logger,
		priority: cfg.Priority,
		paths:    cfg.Paths,
		methods:  cfg.Methods,
		enabled:  true,
	}

	// Initialize plugin configuration
	if err := plugin.Init(cfg.Config); err != nil {
		return nil, fmt.Errorf("failed to initialize WASM plugin %s: %w", cfg.Name, err)
	}

	logger.WithFields(map[string]interface{}{
		"plugin_name": cfg.Name,
		"plugin_path": cfg.Path,
		"priority":    cfg.Priority,
		"paths":       cfg.Paths,
		"methods":     cfg.Methods,
	}).Info("WASM plugin loaded")

	return plugin, nil
}

// Name returns the plugin name
func (wp *WASMPlugin) Name() string {
	return wp.name
}

// Version returns the plugin version
func (wp *WASMPlugin) Version() string {
	return wp.version
}

// Type returns the plugin type (always Custom for WASM plugins)
func (wp *WASMPlugin) Type() PluginType {
	return PluginTypeCustom
}

// Priority returns execution priority
func (wp *WASMPlugin) Priority() int {
	return wp.priority
}

// Init initializes the plugin with configuration
func (wp *WASMPlugin) Init(config map[string]interface{}) error {
	wp.logger.WithFields(map[string]interface{}{
		"plugin_name": wp.name,
		"config":      config,
	}).Debug("Initializing WASM plugin")

	// In a real implementation, this would:
	// 1. Load the WASM module
	// 2. Instantiate the WASM runtime
	// 3. Call the plugin's init function with config

	// For demo purposes, just validate config
	if config != nil {
		wp.logger.WithFields(map[string]interface{}{
			"plugin_name": wp.name,
			"config_keys": len(config),
		}).Info("WASM plugin configuration applied")
	}

	return nil
}

// Execute runs the plugin with given context
func (wp *WASMPlugin) Execute(ctx context.Context, pluginCtx *PluginContext) (*PluginResult, error) {
	if !wp.enabled {
		return &PluginResult{Continue: true}, nil
	}

	wp.logger.WithFields(map[string]interface{}{
		"plugin_name": wp.name,
		"request_id":  pluginCtx.RequestID,
		"method":      pluginCtx.Method,
		"path":        pluginCtx.Path,
		"client_ip":   pluginCtx.ClientIP,
	}).Debug("Executing WASM plugin")

	// Mock implementation - in reality, this would:
	// 1. Prepare plugin context data for WASM
	// 2. Call the WASM plugin's execute function
	// 3. Parse the result from WASM
	// 4. Return structured plugin result

	result := &PluginResult{
		Continue:     true,
		ModifyHeader: make(map[string]string),
		Metadata:     make(map[string]interface{}),
	}

	// Demo plugin behavior based on plugin name
	switch wp.name {
	case "custom-auth":
		result = wp.mockAuthPlugin(pluginCtx)
	case "request-logger":
		result = wp.mockLoggingPlugin(pluginCtx)
	case "rate-limiter":
		result = wp.mockRateLimitPlugin(pluginCtx)
	default:
		// Default behavior: add plugin execution header
		result.ModifyHeader["X-Plugin-Executed"] = wp.name
		result.Metadata["plugin_executed"] = wp.name
	}

	wp.logger.WithFields(map[string]interface{}{
		"plugin_name": wp.name,
		"request_id":  pluginCtx.RequestID,
		"continue":    result.Continue,
		"headers":     len(result.ModifyHeader),
		"metadata":    len(result.Metadata),
	}).Debug("WASM plugin execution completed")

	return result, nil
}

// mockAuthPlugin simulates an authentication plugin
func (wp *WASMPlugin) mockAuthPlugin(pluginCtx *PluginContext) *PluginResult {
	result := &PluginResult{
		Continue:     true,
		ModifyHeader: make(map[string]string),
		Metadata:     make(map[string]interface{}),
	}

	// Check for authorization header
	authHeaders, exists := pluginCtx.Headers["Authorization"]
	if !exists || len(authHeaders) == 0 {
		wp.logger.WithFields(map[string]interface{}{
			"plugin_name": wp.name,
			"request_id":  pluginCtx.RequestID,
			"path":        pluginCtx.Path,
		}).Warn("Missing authorization header")

		result.Continue = false
		result.SetStatus = 401
		result.SetBody = []byte(`{"error": "Authorization header required"}`)
		return result
	}

	// Mock token validation
	token := authHeaders[0]
	if token != "Bearer valid-token" {
		wp.logger.WithFields(map[string]interface{}{
			"plugin_name": wp.name,
			"request_id":  pluginCtx.RequestID,
			"token":       token,
		}).Warn("Invalid authorization token")

		result.Continue = false
		result.SetStatus = 403
		result.SetBody = []byte(`{"error": "Invalid authorization token"}`)
		return result
	}

	// Authentication successful
	result.ModifyHeader["X-Authenticated-User"] = "demo-user"
	result.Metadata["user_id"] = "demo-user"
	result.Metadata["auth_plugin"] = wp.name

	wp.logger.WithFields(map[string]interface{}{
		"plugin_name": wp.name,
		"request_id":  pluginCtx.RequestID,
		"user_id":     "demo-user",
	}).Debug("Authentication successful")

	return result
}

// mockLoggingPlugin simulates a request logging plugin
func (wp *WASMPlugin) mockLoggingPlugin(pluginCtx *PluginContext) *PluginResult {
	wp.logger.WithFields(map[string]interface{}{
		"plugin_name": wp.name,
		"request_id":  pluginCtx.RequestID,
		"method":      pluginCtx.Method,
		"path":        pluginCtx.Path,
		"client_ip":   pluginCtx.ClientIP,
		"user_agent":  pluginCtx.UserAgent,
		"timestamp":   pluginCtx.StartTime,
	}).Info("Request logged by WASM plugin")

	return &PluginResult{
		Continue:     true,
		ModifyHeader: map[string]string{"X-Request-Logged": "true"},
		Metadata:     map[string]interface{}{"logged_by": wp.name},
	}
}

// mockRateLimitPlugin simulates a rate limiting plugin
func (wp *WASMPlugin) mockRateLimitPlugin(pluginCtx *PluginContext) *PluginResult {
	result := &PluginResult{
		Continue:     true,
		ModifyHeader: make(map[string]string),
		Metadata:     make(map[string]interface{}),
	}

	// Mock rate limiting based on client IP
	// In reality, this would maintain state in the WASM plugin
	clientIP := pluginCtx.ClientIP

	// Simple demo: reject requests from "blocked.ip"
	if clientIP == "blocked.ip" {
		wp.logger.WithFields(map[string]interface{}{
			"plugin_name": wp.name,
			"request_id":  pluginCtx.RequestID,
			"client_ip":   clientIP,
		}).Warn("Request rate limited by WASM plugin")

		result.Continue = false
		result.SetStatus = 429
		result.SetBody = []byte(`{"error": "Rate limit exceeded"}`)
		result.ModifyHeader["X-RateLimit-Limit"] = "100"
		result.ModifyHeader["X-RateLimit-Remaining"] = "0"
		return result
	}

	// Allow request with rate limit headers
	result.ModifyHeader["X-RateLimit-Limit"] = "100"
	result.ModifyHeader["X-RateLimit-Remaining"] = "99"
	result.Metadata["rate_limited"] = false
	result.Metadata["rate_limit_plugin"] = wp.name

	return result
}

// Cleanup performs any necessary cleanup
func (wp *WASMPlugin) Cleanup() error {
	wp.logger.WithFields(map[string]interface{}{
		"plugin_name": wp.name,
	}).Info("Cleaning up WASM plugin")

	// In a real implementation, this would:
	// 1. Call the plugin's cleanup function
	// 2. Dispose of the WASM runtime
	// 3. Free allocated memory

	wp.enabled = false
	return nil
}

// CanHandle returns true if plugin can handle the request
func (wp *WASMPlugin) CanHandle(method, path string) bool {
	// Check if plugin is enabled
	if !wp.enabled {
		return false
	}

	// Check method filter
	if len(wp.methods) > 0 {
		methodMatch := false
		for _, m := range wp.methods {
			if m == method || m == "*" {
				methodMatch = true
				break
			}
		}
		if !methodMatch {
			return false
		}
	}

	// Check path filter
	if len(wp.paths) > 0 {
		pathMatch := false
		for _, p := range wp.paths {
			if p == "*" {
				pathMatch = true
				break
			}
			// Simple pattern matching - in reality, use proper glob/regex
			matched, err := filepath.Match(p, path)
			if err == nil && matched {
				pathMatch = true
				break
			}
		}
		if !pathMatch {
			return false
		}
	}

	return true
}

// Enable enables the plugin
func (wp *WASMPlugin) Enable() {
	wp.enabled = true
	wp.logger.WithFields(map[string]interface{}{
		"plugin_name": wp.name,
	}).Info("WASM plugin enabled")
}

// Disable disables the plugin
func (wp *WASMPlugin) Disable() {
	wp.enabled = false
	wp.logger.WithFields(map[string]interface{}{
		"plugin_name": wp.name,
	}).Info("WASM plugin disabled")
}

// IsEnabled returns true if plugin is enabled
func (wp *WASMPlugin) IsEnabled() bool {
	return wp.enabled
}

// GetPath returns the plugin file path
func (wp *WASMPlugin) GetPath() string {
	return wp.path
}

// GetConfig returns the plugin configuration
func (wp *WASMPlugin) GetConfig() config.WASMPlugin {
	return wp.config
}
