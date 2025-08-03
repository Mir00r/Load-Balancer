package config

import (
	"fmt"
	"os"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"gopkg.in/yaml.v2"
)

// Config represents the main configuration structure
type Config struct {
	Server         ServerConfig           `yaml:"server"`
	LoadBalancer   LoadBalancerConfig     `yaml:"load_balancer"`
	Backends       []BackendConfig        `yaml:"backends"`
	Logging        LoggingConfig          `yaml:"logging"`
	TLS            *domain.TLSConfig      `yaml:"tls,omitempty"`
	Security       *domain.SecurityConfig `yaml:"security,omitempty"`
	L4             *domain.L4Config       `yaml:"l4,omitempty"`
	ConnectionPool *ConnectionPoolConfig  `yaml:"connection_pool,omitempty"`
	Metrics        MetricsConfig          `yaml:"metrics"`
	Admin          AdminConfig            `yaml:"admin"`
}

// ServerConfig contains HTTP server specific configuration
type ServerConfig struct {
	Port         int           `yaml:"port"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
}

// ConnectionPoolConfig contains connection pool configuration
type ConnectionPoolConfig struct {
	MaxIdleConns        int           `yaml:"max_idle_conns"`
	MaxActiveConns      int           `yaml:"max_active_conns"`
	MaxConnLifetime     time.Duration `yaml:"max_conn_lifetime"`
	IdleTimeout         time.Duration `yaml:"idle_timeout"`
	ConnectTimeout      time.Duration `yaml:"connect_timeout"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`
	EnableKeepalive     bool          `yaml:"enable_keepalive"`
	KeepaliveIdle       time.Duration `yaml:"keepalive_idle"`
}

// MetricsConfig contains metrics configuration
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Path    string `yaml:"path"`
}

// AdminConfig contains admin API configuration
type AdminConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Path    string `yaml:"path"`
}

// LoadBalancerConfig contains load balancer specific configuration
type LoadBalancerConfig struct {
	Strategy       string                      `yaml:"strategy"`
	Port           int                         `yaml:"port"`
	MaxRetries     int                         `yaml:"max_retries"`
	RetryDelay     time.Duration               `yaml:"retry_delay"`
	Timeout        time.Duration               `yaml:"timeout"`
	HealthCheck    domain.HealthCheckConfig    `yaml:"health_check"`
	RateLimit      domain.RateLimitConfig      `yaml:"rate_limit"`
	CircuitBreaker domain.CircuitBreakerConfig `yaml:"circuit_breaker"`
}

// BackendConfig contains backend server configuration
type BackendConfig struct {
	ID              string        `yaml:"id"`
	URL             string        `yaml:"url"`
	Weight          int           `yaml:"weight"`
	HealthCheckPath string        `yaml:"health_check_path"`
	MaxConnections  int           `yaml:"max_connections"`
	Timeout         time.Duration `yaml:"timeout"`
}

// LoggingConfig contains logging configuration
type LoggingConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	Output     string `yaml:"output"`
	File       string `yaml:"file"`
	MaxSize    int    `yaml:"max_size"`
	MaxBackups int    `yaml:"max_backups"`
	MaxAge     int    `yaml:"max_age"`
	Compress   bool   `yaml:"compress"`
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		LoadBalancer: LoadBalancerConfig{
			Strategy:   "round_robin",
			Port:       8080,
			MaxRetries: 3,
			RetryDelay: 100 * time.Millisecond,
			Timeout:    30 * time.Second,
			HealthCheck: domain.HealthCheckConfig{
				Enabled:            true,
				Interval:           30 * time.Second,
				Timeout:            5 * time.Second,
				HealthyThreshold:   2,
				UnhealthyThreshold: 3,
				Path:               "/health",
			},
			RateLimit: domain.RateLimitConfig{
				Enabled:           false,
				RequestsPerSecond: 100,
				BurstSize:         200,
			},
			CircuitBreaker: domain.CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 5,
				RecoveryTimeout:  60 * time.Second,
				MaxRequests:      10,
			},
		},
		Backends: []BackendConfig{
			{
				ID:              "backend-1",
				URL:             "http://localhost:8081",
				Weight:          1,
				HealthCheckPath: "/health",
				MaxConnections:  100,
				Timeout:         30 * time.Second,
			},
			{
				ID:              "backend-2",
				URL:             "http://localhost:8082",
				Weight:          1,
				HealthCheckPath: "/health",
				MaxConnections:  100,
				Timeout:         30 * time.Second,
			},
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
		Server: ServerConfig{
			Port:         8080,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		Metrics: MetricsConfig{
			Enabled: true,
			Port:    8080,
			Path:    "/metrics",
		},
		Admin: AdminConfig{
			Enabled: true,
			Port:    8080,
			Path:    "/admin",
		},
	}
}

// LoadFromFile loads configuration from a YAML file
func LoadFromFile(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", filename, err)
	}

	config := DefaultConfig()
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", filename, err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// LoadFromEnv loads configuration from environment variables with defaults
func LoadFromEnv() *Config {
	config := DefaultConfig()

	// Override with environment variables if they exist
	if port := os.Getenv("LB_PORT"); port != "" {
		// Parse port and set if valid
		// For simplicity, keeping defaults here
	}

	if strategy := os.Getenv("LB_STRATEGY"); strategy != "" {
		config.LoadBalancer.Strategy = strategy
	}

	if logLevel := os.Getenv("LB_LOG_LEVEL"); logLevel != "" {
		config.Logging.Level = logLevel
	}

	return config
}

// Validate validates the configuration for correctness
func (c *Config) Validate() error {
	// Validate load balancer configuration
	if c.LoadBalancer.Port <= 0 || c.LoadBalancer.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.LoadBalancer.Port)
	}

	if c.LoadBalancer.MaxRetries < 0 {
		return fmt.Errorf("max_retries cannot be negative: %d", c.LoadBalancer.MaxRetries)
	}

	if c.LoadBalancer.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive: %v", c.LoadBalancer.Timeout)
	}

	// Validate strategy
	strategy := domain.LoadBalancingStrategy(c.LoadBalancer.Strategy)
	switch strategy {
	case domain.RoundRobinStrategy, domain.WeightedRoundRobinStrategy, domain.LeastConnectionsStrategy:
		// Valid strategies
	default:
		return fmt.Errorf("unsupported load balancing strategy: %s", c.LoadBalancer.Strategy)
	}

	// Validate backends
	if len(c.Backends) == 0 {
		return fmt.Errorf("at least one backend must be configured")
	}

	backendIDs := make(map[string]bool)
	for i, backend := range c.Backends {
		if backend.ID == "" {
			return fmt.Errorf("backend[%d]: ID cannot be empty", i)
		}

		if backendIDs[backend.ID] {
			return fmt.Errorf("backend[%d]: duplicate ID '%s'", i, backend.ID)
		}
		backendIDs[backend.ID] = true

		if backend.URL == "" {
			return fmt.Errorf("backend[%d]: URL cannot be empty", i)
		}

		if backend.Weight <= 0 {
			return fmt.Errorf("backend[%d]: weight must be positive", i)
		}

		if backend.MaxConnections <= 0 {
			return fmt.Errorf("backend[%d]: max_connections must be positive", i)
		}

		if backend.Timeout <= 0 {
			return fmt.Errorf("backend[%d]: timeout must be positive", i)
		}
	}

	// Validate health check configuration
	if c.LoadBalancer.HealthCheck.Enabled {
		if c.LoadBalancer.HealthCheck.Interval <= 0 {
			return fmt.Errorf("health_check.interval must be positive")
		}
		if c.LoadBalancer.HealthCheck.Timeout <= 0 {
			return fmt.Errorf("health_check.timeout must be positive")
		}
		if c.LoadBalancer.HealthCheck.HealthyThreshold <= 0 {
			return fmt.Errorf("health_check.healthy_threshold must be positive")
		}
		if c.LoadBalancer.HealthCheck.UnhealthyThreshold <= 0 {
			return fmt.Errorf("health_check.unhealthy_threshold must be positive")
		}
	}

	// Validate rate limiting configuration
	if c.LoadBalancer.RateLimit.Enabled {
		if c.LoadBalancer.RateLimit.RequestsPerSecond <= 0 {
			return fmt.Errorf("rate_limit.requests_per_second must be positive")
		}
		if c.LoadBalancer.RateLimit.BurstSize <= 0 {
			return fmt.Errorf("rate_limit.burst_size must be positive")
		}
	}

	// Validate circuit breaker configuration
	if c.LoadBalancer.CircuitBreaker.Enabled {
		if c.LoadBalancer.CircuitBreaker.FailureThreshold <= 0 {
			return fmt.Errorf("circuit_breaker.failure_threshold must be positive")
		}
		if c.LoadBalancer.CircuitBreaker.RecoveryTimeout <= 0 {
			return fmt.Errorf("circuit_breaker.recovery_timeout must be positive")
		}
		if c.LoadBalancer.CircuitBreaker.MaxRequests <= 0 {
			return fmt.Errorf("circuit_breaker.max_requests must be positive")
		}
	}

	// Validate logging configuration
	validLevels := map[string]bool{
		"trace": true, "debug": true, "info": true,
		"warn": true, "error": true, "fatal": true, "panic": true,
	}
	if !validLevels[c.Logging.Level] {
		return fmt.Errorf("invalid log level: %s", c.Logging.Level)
	}

	validFormats := map[string]bool{"json": true, "text": true}
	if !validFormats[c.Logging.Format] {
		return fmt.Errorf("invalid log format: %s", c.Logging.Format)
	}

	validOutputs := map[string]bool{"stdout": true, "stderr": true, "file": true}
	if !validOutputs[c.Logging.Output] {
		return fmt.Errorf("invalid log output: %s", c.Logging.Output)
	}

	return nil
}

// ToLoadBalancerConfig converts to domain LoadBalancerConfig
func (c *Config) ToLoadBalancerConfig() domain.LoadBalancerConfig {
	return domain.LoadBalancerConfig{
		Strategy:       domain.LoadBalancingStrategy(c.LoadBalancer.Strategy),
		Port:           c.LoadBalancer.Port,
		MaxRetries:     c.LoadBalancer.MaxRetries,
		RetryDelay:     c.LoadBalancer.RetryDelay,
		Timeout:        c.LoadBalancer.Timeout,
		HealthCheck:    c.LoadBalancer.HealthCheck,
		RateLimit:      c.LoadBalancer.RateLimit,
		CircuitBreaker: c.LoadBalancer.CircuitBreaker,
	}
}

// ToBackends converts backend configurations to domain backends
func (c *Config) ToBackends() []*domain.Backend {
	backends := make([]*domain.Backend, len(c.Backends))
	for i, bc := range c.Backends {
		backend := domain.NewBackend(bc.ID, bc.URL, bc.Weight)
		backend.HealthCheckPath = bc.HealthCheckPath
		backend.MaxConnections = bc.MaxConnections
		backend.Timeout = bc.Timeout
		backends[i] = backend
	}
	return backends
}

// SaveToFile saves the configuration to a YAML file
func (c *Config) SaveToFile(filename string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", filename, err)
	}

	return nil
}
