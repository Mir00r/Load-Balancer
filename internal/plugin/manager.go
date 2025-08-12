// Package plugin provides an extensible plugin system with WASM support for runtime loading and execution
// Supports HTTP request/response processing, authentication, rate limiting, and custom middleware
package plugin

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/config"
	lberrors "github.com/mir00r/load-balancer/internal/errors"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// PluginType defines the type of plugin
type PluginType int

const (
	PluginTypeHTTP PluginType = iota
	PluginTypeAuth
	PluginTypeRateLimit
	PluginTypeMetrics
	PluginTypeCustom
)

// String returns string representation of plugin type
func (pt PluginType) String() string {
	switch pt {
	case PluginTypeHTTP:
		return "http"
	case PluginTypeAuth:
		return "auth"
	case PluginTypeRateLimit:
		return "rate_limit"
	case PluginTypeMetrics:
		return "metrics"
	case PluginTypeCustom:
		return "custom"
	default:
		return "unknown"
	}
}

// PluginPhase defines when the plugin should be executed
type PluginPhase int

const (
	PluginPhaseRequest PluginPhase = iota
	PluginPhaseResponse
	PluginPhaseError
	PluginPhaseMetrics
)

// String returns string representation of plugin phase
func (pp PluginPhase) String() string {
	switch pp {
	case PluginPhaseRequest:
		return "request"
	case PluginPhaseResponse:
		return "response"
	case PluginPhaseError:
		return "error"
	case PluginPhaseMetrics:
		return "metrics"
	default:
		return "unknown"
	}
}

// PluginContext provides context and data to plugins
type PluginContext struct {
	RequestID  string
	ClientIP   string
	UserAgent  string
	Path       string
	Method     string
	Headers    map[string][]string
	Body       []byte
	Metadata   map[string]interface{}
	StartTime  time.Time
	BackendID  string
	StatusCode int
	Error      error
}

// PluginResult represents the result of plugin execution
type PluginResult struct {
	Continue     bool                   // Whether to continue processing
	ModifyHeader map[string]string      // Headers to add/modify
	SetStatus    int                    // HTTP status to set (0 = no change)
	SetBody      []byte                 // Response body to set
	Metadata     map[string]interface{} // Metadata to pass to next plugin
	Error        error                  // Error that occurred
}

// Plugin interface defines the contract for all plugins
type Plugin interface {
	// Name returns the plugin name
	Name() string

	// Version returns the plugin version
	Version() string

	// Type returns the plugin type
	Type() PluginType

	// Priority returns execution priority (higher = earlier)
	Priority() int

	// Init initializes the plugin with configuration
	Init(config map[string]interface{}) error

	// Execute runs the plugin with given context
	Execute(ctx context.Context, pluginCtx *PluginContext) (*PluginResult, error)

	// Cleanup performs any necessary cleanup
	Cleanup() error

	// CanHandle returns true if plugin can handle the request
	CanHandle(method, path string) bool
}

// PluginManager manages plugin lifecycle and execution
type PluginManager struct {
	plugins        map[string]Plugin
	pluginsByPhase map[PluginPhase][]Plugin
	wasmPlugins    map[string]*WASMPlugin
	config         *config.PluginConfig
	logger         *logger.Logger
	mutex          sync.RWMutex
	enabled        bool
}

// NewPluginManager creates a new plugin manager
func NewPluginManager(cfg *config.PluginConfig, logger *logger.Logger) (*PluginManager, error) {
	if cfg == nil || !cfg.Enabled {
		return &PluginManager{
			plugins:        make(map[string]Plugin),
			pluginsByPhase: make(map[PluginPhase][]Plugin),
			wasmPlugins:    make(map[string]*WASMPlugin),
			logger:         logger,
			enabled:        false,
		}, nil
	}

	pm := &PluginManager{
		plugins:        make(map[string]Plugin),
		pluginsByPhase: make(map[PluginPhase][]Plugin),
		wasmPlugins:    make(map[string]*WASMPlugin),
		config:         cfg,
		logger:         logger,
		enabled:        true,
	}

	if err := pm.loadPlugins(); err != nil {
		return nil, lberrors.NewErrorWithCause(
			lberrors.ErrCodeConfigLoad,
			"plugin_manager",
			"Failed to load plugins",
			err,
		)
	}

	logger.WithFields(map[string]interface{}{
		"total_plugins": len(pm.plugins),
		"wasm_plugins":  len(pm.wasmPlugins),
		"enabled":       pm.enabled,
	}).Info("Plugin manager initialized")

	return pm, nil
}

// loadPlugins loads all configured plugins
func (pm *PluginManager) loadPlugins() error {
	// Load WASM plugins
	for _, wasmCfg := range pm.config.WASMPlugins {
		if err := pm.loadWASMPlugin(wasmCfg); err != nil {
			pm.logger.WithFields(map[string]interface{}{
				"plugin_name": wasmCfg.Name,
				"plugin_path": wasmCfg.Path,
				"error":       err.Error(),
			}).Error("Failed to load WASM plugin")
			return err
		}
	}

	// Load built-in plugins
	builtInPlugins := []Plugin{
		NewLoggingPlugin(pm.logger),
		NewMetricsPlugin(),
		NewCORSPlugin(),
		NewCompressionPlugin(),
	}

	for _, plugin := range builtInPlugins {
		if err := pm.RegisterPlugin(plugin); err != nil {
			pm.logger.WithFields(map[string]interface{}{
				"plugin_name": plugin.Name(),
				"error":       err.Error(),
			}).Error("Failed to register built-in plugin")
			return err
		}
	}

	return nil
}

// loadWASMPlugin loads a WASM plugin from file
func (pm *PluginManager) loadWASMPlugin(cfg config.WASMPlugin) error {
	wasmPlugin, err := NewWASMPlugin(cfg, pm.logger)
	if err != nil {
		return err
	}

	pm.wasmPlugins[cfg.Name] = wasmPlugin
	return pm.RegisterPlugin(wasmPlugin)
}

// RegisterPlugin registers a plugin with the manager
func (pm *PluginManager) RegisterPlugin(plugin Plugin) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	name := plugin.Name()
	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}

	pm.plugins[name] = plugin
	pm.organizePluginsByPhase()

	pm.logger.WithFields(map[string]interface{}{
		"plugin_name":    name,
		"plugin_type":    plugin.Type().String(),
		"plugin_version": plugin.Version(),
		"priority":       plugin.Priority(),
	}).Info("Plugin registered")

	return nil
}

// UnregisterPlugin removes a plugin from the manager
func (pm *PluginManager) UnregisterPlugin(name string) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	// Cleanup plugin
	if err := plugin.Cleanup(); err != nil {
		pm.logger.WithFields(map[string]interface{}{
			"plugin_name": name,
			"error":       err.Error(),
		}).Warn("Plugin cleanup failed")
	}

	delete(pm.plugins, name)
	pm.organizePluginsByPhase()

	pm.logger.WithFields(map[string]interface{}{
		"plugin_name": name,
	}).Info("Plugin unregistered")

	return nil
}

// organizePluginsByPhase organizes plugins by their execution phase and priority
func (pm *PluginManager) organizePluginsByPhase() {
	pm.pluginsByPhase = make(map[PluginPhase][]Plugin)

	for _, plugin := range pm.plugins {
		// For now, assume all plugins run on request phase
		// In a real implementation, plugins would specify their phases
		phase := PluginPhaseRequest
		pm.pluginsByPhase[phase] = append(pm.pluginsByPhase[phase], plugin)
	}

	// Sort plugins by priority (higher priority first)
	for phase := range pm.pluginsByPhase {
		sort.Slice(pm.pluginsByPhase[phase], func(i, j int) bool {
			return pm.pluginsByPhase[phase][i].Priority() > pm.pluginsByPhase[phase][j].Priority()
		})
	}
}

// ExecutePlugins executes plugins for a given phase
func (pm *PluginManager) ExecutePlugins(ctx context.Context, phase PluginPhase, pluginCtx *PluginContext) (*PluginResult, error) {
	if !pm.enabled {
		return &PluginResult{Continue: true}, nil
	}

	pm.mutex.RLock()
	plugins := pm.pluginsByPhase[phase]
	pm.mutex.RUnlock()

	result := &PluginResult{
		Continue:     true,
		ModifyHeader: make(map[string]string),
		Metadata:     make(map[string]interface{}),
	}

	for _, plugin := range plugins {
		// Check if plugin can handle this request
		if !plugin.CanHandle(pluginCtx.Method, pluginCtx.Path) {
			continue
		}

		// Execute plugin
		pluginResult, err := plugin.Execute(ctx, pluginCtx)
		if err != nil {
			pm.logger.WithFields(map[string]interface{}{
				"plugin_name": plugin.Name(),
				"phase":       phase.String(),
				"error":       err.Error(),
				"request_id":  pluginCtx.RequestID,
			}).Error("Plugin execution failed")

			result.Error = err
			continue
		}

		// Merge plugin result
		if pluginResult != nil {
			if !pluginResult.Continue {
				result.Continue = false
			}

			// Merge headers
			for key, value := range pluginResult.ModifyHeader {
				result.ModifyHeader[key] = value
			}

			// Update status if set
			if pluginResult.SetStatus > 0 {
				result.SetStatus = pluginResult.SetStatus
			}

			// Update body if set
			if pluginResult.SetBody != nil {
				result.SetBody = pluginResult.SetBody
			}

			// Merge metadata
			for key, value := range pluginResult.Metadata {
				result.Metadata[key] = value
				pluginCtx.Metadata[key] = value // Update context for next plugin
			}

			// If plugin says don't continue, stop execution
			if !pluginResult.Continue {
				break
			}
		}
	}

	return result, nil
}

// ExecuteRequestPlugins executes plugins for request phase
func (pm *PluginManager) ExecuteRequestPlugins(ctx context.Context, r *http.Request) (*PluginResult, error) {
	pluginCtx := &PluginContext{
		RequestID: getRequestID(r),
		ClientIP:  getClientIP(r),
		UserAgent: r.UserAgent(),
		Path:      r.URL.Path,
		Method:    r.Method,
		Headers:   map[string][]string(r.Header),
		Metadata:  make(map[string]interface{}),
		StartTime: time.Now(),
	}

	return pm.ExecutePlugins(ctx, PluginPhaseRequest, pluginCtx)
}

// ExecuteResponsePlugins executes plugins for response phase
func (pm *PluginManager) ExecuteResponsePlugins(ctx context.Context, r *http.Request, w http.ResponseWriter, statusCode int, backendID string) (*PluginResult, error) {
	pluginCtx := &PluginContext{
		RequestID:  getRequestID(r),
		ClientIP:   getClientIP(r),
		UserAgent:  r.UserAgent(),
		Path:       r.URL.Path,
		Method:     r.Method,
		Headers:    map[string][]string(r.Header),
		Metadata:   make(map[string]interface{}),
		StartTime:  time.Now(),
		BackendID:  backendID,
		StatusCode: statusCode,
	}

	return pm.ExecutePlugins(ctx, PluginPhaseResponse, pluginCtx)
}

// GetPluginInfo returns information about all loaded plugins
func (pm *PluginManager) GetPluginInfo() map[string]interface{} {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	plugins := make([]map[string]interface{}, 0, len(pm.plugins))
	for _, plugin := range pm.plugins {
		plugins = append(plugins, map[string]interface{}{
			"name":     plugin.Name(),
			"version":  plugin.Version(),
			"type":     plugin.Type().String(),
			"priority": plugin.Priority(),
		})
	}

	return map[string]interface{}{
		"enabled":       pm.enabled,
		"total_plugins": len(pm.plugins),
		"wasm_plugins":  len(pm.wasmPlugins),
		"plugins":       plugins,
	}
}

// Shutdown gracefully shuts down the plugin manager
func (pm *PluginManager) Shutdown(ctx context.Context) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.logger.Info("Shutting down plugin manager")

	// Cleanup all plugins
	for name, plugin := range pm.plugins {
		if err := plugin.Cleanup(); err != nil {
			pm.logger.WithFields(map[string]interface{}{
				"plugin_name": name,
				"error":       err.Error(),
			}).Warn("Plugin cleanup failed during shutdown")
		}
	}

	// Cleanup WASM plugins
	for name, wasmPlugin := range pm.wasmPlugins {
		if err := wasmPlugin.Cleanup(); err != nil {
			pm.logger.WithFields(map[string]interface{}{
				"wasm_plugin_name": name,
				"error":            err.Error(),
			}).Warn("WASM plugin cleanup failed during shutdown")
		}
	}

	pm.enabled = false
	return nil
}

// Helper functions

// getRequestID extracts or generates a request ID
func getRequestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	if id := r.Header.Get("X-Correlation-ID"); id != "" {
		return id
	}
	// Generate a simple request ID (in production, use a proper UUID library)
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

// getClientIP extracts the real client IP address
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to remote address
	return r.RemoteAddr
}
