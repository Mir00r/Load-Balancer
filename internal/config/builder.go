package config

import (
	"fmt"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/errors"
)

// ConfigBuilder provides a fluent interface for building configurations
type ConfigBuilder struct {
	config *Config
	errors []error
}

// NewConfigBuilder creates a new configuration builder with sensible defaults
func NewConfigBuilder() *ConfigBuilder {
	return &ConfigBuilder{
		config: &Config{
			Server: ServerConfig{
				Port:         8080,
				ReadTimeout:  30 * time.Second,
				WriteTimeout: 30 * time.Second,
				IdleTimeout:  120 * time.Second,
			},
			LoadBalancer: LoadBalancerConfig{
				Strategy:   "round_robin",
				MaxRetries: 3,
				RetryDelay: 100 * time.Millisecond,
				Timeout:    30 * time.Second,
			},
			Logging: LoggingConfig{
				Level:      "info",
				Format:     "json",
				Output:     "stdout",
				MaxSize:    100,
				MaxBackups: 3,
				MaxAge:     28,
				Compress:   true,
			},
			Metrics: MetricsConfig{
				Enabled: true,
				Path:    "/metrics",
				Port:    9090,
			},
			Admin: AdminConfig{
				Enabled: true,
				Port:    8081,
				Path:    "/admin",
			},
		},
	}
}

// WithServer configures the HTTP server settings
func (b *ConfigBuilder) WithServer(port int, readTimeout, writeTimeout, idleTimeout time.Duration) *ConfigBuilder {
	if port <= 0 || port > 65535 {
		b.errors = append(b.errors, fmt.Errorf("invalid port number: %d", port))
		return b
	}

	b.config.Server.Port = port
	b.config.Server.ReadTimeout = readTimeout
	b.config.Server.WriteTimeout = writeTimeout
	b.config.Server.IdleTimeout = idleTimeout
	return b
}

// WithLoadBalancer configures load balancer settings
func (b *ConfigBuilder) WithLoadBalancer(strategy string, maxRetries int, retryDelay, timeout time.Duration) *ConfigBuilder {
	validStrategies := map[string]bool{
		"round_robin":          true,
		"weighted_round_robin": true,
		"least_connections":    true,
		"ip_hash":              true,
	}

	if !validStrategies[strategy] {
		b.errors = append(b.errors, fmt.Errorf("invalid load balancer strategy: %s", strategy))
		return b
	}

	if maxRetries < 0 {
		b.errors = append(b.errors, fmt.Errorf("maxRetries cannot be negative: %d", maxRetries))
		return b
	}

	b.config.LoadBalancer.Strategy = strategy
	b.config.LoadBalancer.MaxRetries = maxRetries
	b.config.LoadBalancer.RetryDelay = retryDelay
	b.config.LoadBalancer.Timeout = timeout
	return b
}

// WithBackend adds a backend to the configuration
func (b *ConfigBuilder) WithBackend(id, url string, weight int, healthCheckPath string) *ConfigBuilder {
	if id == "" {
		b.errors = append(b.errors, fmt.Errorf("backend ID cannot be empty"))
		return b
	}

	if url == "" {
		b.errors = append(b.errors, fmt.Errorf("backend URL cannot be empty"))
		return b
	}

	if weight < 0 {
		b.errors = append(b.errors, fmt.Errorf("backend weight cannot be negative: %d", weight))
		return b
	}

	if healthCheckPath == "" {
		healthCheckPath = "/health" // default health check path
	}

	backend := BackendConfig{
		ID:              id,
		URL:             url,
		Weight:          weight,
		HealthCheckPath: healthCheckPath,
		MaxConnections:  100,              // default
		Timeout:         30 * time.Second, // default
	}

	b.config.Backends = append(b.config.Backends, backend)
	return b
}

// WithLogging configures logging settings
func (b *ConfigBuilder) WithLogging(level, format, output string) *ConfigBuilder {
	validLevels := map[string]bool{
		"trace": true, "debug": true, "info": true,
		"warn": true, "error": true, "fatal": true, "panic": true,
	}

	validFormats := map[string]bool{
		"json": true, "text": true, "logfmt": true,
	}

	if !validLevels[level] {
		b.errors = append(b.errors, fmt.Errorf("invalid log level: %s", level))
		return b
	}

	if !validFormats[format] {
		b.errors = append(b.errors, fmt.Errorf("invalid log format: %s", format))
		return b
	}

	b.config.Logging.Level = level
	b.config.Logging.Format = format
	b.config.Logging.Output = output
	return b
}

// WithHealthCheck configures health check settings
func (b *ConfigBuilder) WithHealthCheck(interval, timeout time.Duration, path string, thresholdHealthy, thresholdUnhealthy int) *ConfigBuilder {
	if interval <= 0 {
		b.errors = append(b.errors, fmt.Errorf("health check interval must be positive"))
		return b
	}

	if timeout <= 0 {
		b.errors = append(b.errors, fmt.Errorf("health check timeout must be positive"))
		return b
	}

	if timeout >= interval {
		b.errors = append(b.errors, fmt.Errorf("health check timeout must be less than interval"))
		return b
	}

	if thresholdHealthy <= 0 {
		b.errors = append(b.errors, fmt.Errorf("healthy threshold must be positive"))
		return b
	}

	if thresholdUnhealthy <= 0 {
		b.errors = append(b.errors, fmt.Errorf("unhealthy threshold must be positive"))
		return b
	}

	healthCheck := domain.HealthCheckConfig{
		Enabled:            true,
		Interval:           interval,
		Timeout:            timeout,
		Path:               path,
		HealthyThreshold:   thresholdHealthy,
		UnhealthyThreshold: thresholdUnhealthy,
	}

	b.config.LoadBalancer.HealthCheck = healthCheck
	return b
}

// WithRateLimit configures rate limiting
func (b *ConfigBuilder) WithRateLimit(enabled bool, requestsPerSecond float64, burst int) *ConfigBuilder {
	if requestsPerSecond <= 0 {
		b.errors = append(b.errors, fmt.Errorf("requests per second must be positive"))
		return b
	}

	if burst <= 0 {
		b.errors = append(b.errors, fmt.Errorf("burst size must be positive"))
		return b
	}

	rateLimit := &RateLimitConfig{
		Enabled:         enabled,
		RequestsPerSec:  requestsPerSecond,
		BurstSize:       burst,
		CleanupInterval: time.Minute,
	}

	b.config.RateLimit = rateLimit
	return b
}

// WithTLS configures TLS settings
func (b *ConfigBuilder) WithTLS(enabled bool, certFile, keyFile string, minVersion, maxVersion string) *ConfigBuilder {
	if enabled && (certFile == "" || keyFile == "") {
		b.errors = append(b.errors, fmt.Errorf("TLS cert and key files must be specified when TLS is enabled"))
		return b
	}

	validVersions := map[string]bool{
		"1.0": true, "1.1": true, "1.2": true, "1.3": true,
	}

	if minVersion != "" && !validVersions[minVersion] {
		b.errors = append(b.errors, fmt.Errorf("invalid TLS min version: %s", minVersion))
		return b
	}

	if maxVersion != "" && !validVersions[maxVersion] {
		b.errors = append(b.errors, fmt.Errorf("invalid TLS max version: %s", maxVersion))
		return b
	}

	tls := &domain.TLSConfig{
		Enabled:    enabled,
		CertFile:   certFile,
		KeyFile:    keyFile,
		MinVersion: minVersion,
		MaxVersion: maxVersion,
	}

	b.config.TLS = tls
	return b
}

// WithCircuitBreaker configures circuit breaker settings
func (b *ConfigBuilder) WithCircuitBreaker(enabled bool, threshold int, timeout, resetTimeout time.Duration) *ConfigBuilder {
	if threshold <= 0 {
		b.errors = append(b.errors, fmt.Errorf("circuit breaker failure threshold must be positive"))
		return b
	}

	if timeout <= 0 {
		b.errors = append(b.errors, fmt.Errorf("circuit breaker timeout must be positive"))
		return b
	}

	if resetTimeout <= 0 {
		b.errors = append(b.errors, fmt.Errorf("circuit breaker reset timeout must be positive"))
		return b
	}

	circuitBreaker := domain.CircuitBreakerConfig{
		Enabled:          enabled,
		FailureThreshold: threshold,
		RecoveryTimeout:  resetTimeout,
		MaxRequests:      10, // default max requests during half-open state
	}

	b.config.LoadBalancer.CircuitBreaker = circuitBreaker
	return b
}

// WithMetrics configures metrics settings
func (b *ConfigBuilder) WithMetrics(enabled bool, path string, port int) *ConfigBuilder {
	if port <= 0 || port > 65535 {
		b.errors = append(b.errors, fmt.Errorf("invalid metrics port: %d", port))
		return b
	}

	if path == "" {
		path = "/metrics"
	}

	b.config.Metrics.Enabled = enabled
	b.config.Metrics.Path = path
	b.config.Metrics.Port = port
	return b
}

// WithAdmin configures admin API settings
func (b *ConfigBuilder) WithAdmin(enabled bool, path string, port int) *ConfigBuilder {
	if port <= 0 || port > 65535 {
		b.errors = append(b.errors, fmt.Errorf("invalid admin port: %d", port))
		return b
	}

	if path == "" {
		path = "/admin"
	}

	b.config.Admin.Enabled = enabled
	b.config.Admin.Path = path
	b.config.Admin.Port = port
	return b
}

// WithConnectionPool configures connection pool settings
func (b *ConfigBuilder) WithConnectionPool(maxIdle, maxActive int, maxLifetime, idleTimeout time.Duration) *ConfigBuilder {
	if maxIdle < 0 {
		b.errors = append(b.errors, fmt.Errorf("max idle connections cannot be negative"))
		return b
	}

	if maxActive < 0 {
		b.errors = append(b.errors, fmt.Errorf("max active connections cannot be negative"))
		return b
	}

	if maxActive > 0 && maxIdle > maxActive {
		b.errors = append(b.errors, fmt.Errorf("max idle connections cannot exceed max active connections"))
		return b
	}

	connectionPool := &ConnectionPoolConfig{
		MaxIdleConns:        maxIdle,
		MaxActiveConns:      maxActive,
		MaxConnLifetime:     maxLifetime,
		IdleTimeout:         idleTimeout,
		ConnectTimeout:      30 * time.Second, // default
		HealthCheckInterval: 30 * time.Second, // default
		EnableKeepalive:     true,             // default
		KeepaliveIdle:       30 * time.Second, // default
	}

	b.config.ConnectionPool = connectionPool
	return b
}

// Build validates and returns the final configuration
func (b *ConfigBuilder) Build() (*Config, error) {
	// Check for any configuration errors
	if len(b.errors) > 0 {
		return nil, errors.NewError(
			errors.ErrCodeConfigLoad,
			"config_builder",
			fmt.Sprintf("Configuration validation failed with %d errors", len(b.errors)),
		).WithMetadata("errors", b.errors)
	}

	// Validate the complete configuration
	if err := b.validateConfig(); err != nil {
		return nil, err
	}

	return b.config, nil
}

// validateConfig performs final validation on the complete configuration
func (b *ConfigBuilder) validateConfig() error {
	// Validate that we have at least one backend
	if len(b.config.Backends) == 0 {
		return errors.NewError(
			errors.ErrCodeConfigLoad,
			"config_builder",
			"At least one backend must be configured",
		)
	}

	// Validate port conflicts
	usedPorts := make(map[int]string)

	if _, exists := usedPorts[b.config.Server.Port]; exists {
		return errors.NewError(
			errors.ErrCodeConfigLoad,
			"config_builder",
			fmt.Sprintf("Port conflict: server port %d is already in use", b.config.Server.Port),
		)
	}
	usedPorts[b.config.Server.Port] = "server"

	if b.config.Metrics.Enabled {
		if _, exists := usedPorts[b.config.Metrics.Port]; exists {
			return errors.NewError(
				errors.ErrCodeConfigLoad,
				"config_builder",
				fmt.Sprintf("Port conflict: metrics port %d is already in use", b.config.Metrics.Port),
			)
		}
		usedPorts[b.config.Metrics.Port] = "metrics"
	}

	if b.config.Admin.Enabled {
		if _, exists := usedPorts[b.config.Admin.Port]; exists {
			return errors.NewError(
				errors.ErrCodeConfigLoad,
				"config_builder",
				fmt.Sprintf("Port conflict: admin port %d is already in use", b.config.Admin.Port),
			)
		}
		usedPorts[b.config.Admin.Port] = "admin"
	}

	// Validate backend IDs are unique
	backendIDs := make(map[string]bool)
	for _, backend := range b.config.Backends {
		if backendIDs[backend.ID] {
			return errors.NewError(
				errors.ErrCodeConfigLoad,
				"config_builder",
				fmt.Sprintf("Duplicate backend ID: %s", backend.ID),
			)
		}
		backendIDs[backend.ID] = true
	}

	return nil
}

// BuildFromFile loads configuration from a file and returns a builder
func BuildFromFile(filename string) (*ConfigBuilder, error) {
	cfg, err := LoadFromFile(filename)
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeConfigLoad, "config_builder", "Failed to load configuration from file")
	}

	return &ConfigBuilder{config: cfg}, nil
}

// Clone creates a copy of the current configuration for modification
func (b *ConfigBuilder) Clone() *ConfigBuilder {
	// Deep copy the configuration
	newConfig := *b.config

	// Copy slices
	if b.config.Backends != nil {
		newConfig.Backends = make([]BackendConfig, len(b.config.Backends))
		copy(newConfig.Backends, b.config.Backends)
	}

	// Copy pointers
	if b.config.TLS != nil {
		tls := *b.config.TLS
		newConfig.TLS = &tls
	}

	if b.config.Security != nil {
		security := *b.config.Security
		newConfig.Security = &security
	}

	if b.config.L4 != nil {
		l4 := *b.config.L4
		newConfig.L4 = &l4
	}

	if b.config.ConnectionPool != nil {
		cp := *b.config.ConnectionPool
		newConfig.ConnectionPool = &cp
	}

	if b.config.RateLimit != nil {
		rl := *b.config.RateLimit
		newConfig.RateLimit = &rl
	}

	if b.config.Auth != nil {
		auth := *b.config.Auth
		newConfig.Auth = &auth
	}

	if b.config.SSL != nil {
		ssl := *b.config.SSL
		newConfig.SSL = &ssl
	}

	if b.config.TraefikStyle != nil {
		ts := *b.config.TraefikStyle
		newConfig.TraefikStyle = &ts
	}

	return &ConfigBuilder{
		config: &newConfig,
		errors: make([]error, len(b.errors)),
	}
}

// GetConfig returns the current configuration (read-only)
func (b *ConfigBuilder) GetConfig() *Config {
	return b.config
}
