package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
)

// LoadFromEnvironment loads configuration from environment variables
// This implements 12-Factor App methodology - Factor #3: Config
func LoadFromEnvironment() *Config {
	config := DefaultConfig()

	// Load Balancer Configuration
	if port := getEnv("LB_PORT", ""); port != "" {
		if p, err := strconv.Atoi(port); err == nil && p > 0 && p <= 65535 {
			config.LoadBalancer.Port = p
		}
	}

	if strategy := getEnv("LB_STRATEGY", ""); strategy != "" {
		config.LoadBalancer.Strategy = strategy
	}

	if maxRetries := getEnv("LB_MAX_RETRIES", ""); maxRetries != "" {
		if retries, err := strconv.Atoi(maxRetries); err == nil && retries >= 0 {
			config.LoadBalancer.MaxRetries = retries
		}
	}

	if retryDelay := getEnv("LB_RETRY_DELAY", ""); retryDelay != "" {
		if delay, err := time.ParseDuration(retryDelay); err == nil {
			config.LoadBalancer.RetryDelay = delay
		}
	}

	if timeout := getEnv("LB_TIMEOUT", ""); timeout != "" {
		if t, err := time.ParseDuration(timeout); err == nil {
			config.LoadBalancer.Timeout = t
		}
	}

	// Health Check Configuration
	if enabled := getEnv("LB_HEALTH_CHECK_ENABLED", ""); enabled != "" {
		config.LoadBalancer.HealthCheck.Enabled = strings.ToLower(enabled) == "true"
	}

	if interval := getEnv("LB_HEALTH_CHECK_INTERVAL", ""); interval != "" {
		if i, err := time.ParseDuration(interval); err == nil {
			config.LoadBalancer.HealthCheck.Interval = i
		}
	}

	if timeout := getEnv("LB_HEALTH_CHECK_TIMEOUT", ""); timeout != "" {
		if t, err := time.ParseDuration(timeout); err == nil {
			config.LoadBalancer.HealthCheck.Timeout = t
		}
	}

	if threshold := getEnv("LB_HEALTH_CHECK_HEALTHY_THRESHOLD", ""); threshold != "" {
		if t, err := strconv.Atoi(threshold); err == nil && t > 0 {
			config.LoadBalancer.HealthCheck.HealthyThreshold = t
		}
	}

	if threshold := getEnv("LB_HEALTH_CHECK_UNHEALTHY_THRESHOLD", ""); threshold != "" {
		if t, err := strconv.Atoi(threshold); err == nil && t > 0 {
			config.LoadBalancer.HealthCheck.UnhealthyThreshold = t
		}
	}

	if path := getEnv("LB_HEALTH_CHECK_PATH", ""); path != "" {
		config.LoadBalancer.HealthCheck.Path = path
	}

	// Rate Limiting Configuration
	if enabled := getEnv("LB_RATE_LIMIT_ENABLED", ""); enabled != "" {
		config.LoadBalancer.RateLimit.Enabled = strings.ToLower(enabled) == "true"
	}

	if rps := getEnv("LB_RATE_LIMIT_RPS", ""); rps != "" {
		if r, err := strconv.ParseFloat(rps, 64); err == nil && r > 0 {
			config.LoadBalancer.RateLimit.RequestsPerSecond = r
		}
	}

	if burst := getEnv("LB_RATE_LIMIT_BURST", ""); burst != "" {
		if b, err := strconv.Atoi(burst); err == nil && b > 0 {
			config.LoadBalancer.RateLimit.BurstSize = b
		}
	}

	// Circuit Breaker Configuration
	if enabled := getEnv("LB_CIRCUIT_BREAKER_ENABLED", ""); enabled != "" {
		config.LoadBalancer.CircuitBreaker.Enabled = strings.ToLower(enabled) == "true"
	}

	if threshold := getEnv("LB_CIRCUIT_BREAKER_FAILURE_THRESHOLD", ""); threshold != "" {
		if t, err := strconv.Atoi(threshold); err == nil && t > 0 {
			config.LoadBalancer.CircuitBreaker.FailureThreshold = t
		}
	}

	if timeout := getEnv("LB_CIRCUIT_BREAKER_RECOVERY_TIMEOUT", ""); timeout != "" {
		if t, err := time.ParseDuration(timeout); err == nil {
			config.LoadBalancer.CircuitBreaker.RecoveryTimeout = t
		}
	}

	if maxReq := getEnv("LB_CIRCUIT_BREAKER_MAX_REQUESTS", ""); maxReq != "" {
		if m, err := strconv.Atoi(maxReq); err == nil && m > 0 {
			config.LoadBalancer.CircuitBreaker.MaxRequests = m
		}
	}

	// Backends Configuration from Environment
	if backends := getEnv("LB_BACKENDS", ""); backends != "" {
		config.Backends = parseBackendsFromEnv(backends)
	}

	// Logging Configuration
	if level := getEnv("LB_LOG_LEVEL", ""); level != "" {
		config.Logging.Level = level
	}

	if format := getEnv("LB_LOG_FORMAT", ""); format != "" {
		config.Logging.Format = format
	}

	if output := getEnv("LB_LOG_OUTPUT", ""); output != "" {
		config.Logging.Output = output
	}

	if file := getEnv("LB_LOG_FILE", ""); file != "" {
		config.Logging.File = file
	}

	return config
}

// getEnv gets environment variable with fallback to default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// parseBackendsFromEnv parses backends from environment variable
// Format: "id1=url1=weight1,id2=url2=weight2"
// Example: "backend-1=http://localhost:8081=1,backend-2=http://localhost:8082=2"
func parseBackendsFromEnv(backends string) []BackendConfig {
	var backendConfigs []BackendConfig

	if backends == "" {
		return backendConfigs
	}

	backendSpecs := strings.Split(backends, ",")
	for _, spec := range backendSpecs {
		parts := strings.Split(strings.TrimSpace(spec), "=")
		if len(parts) >= 2 {
			weight := 1
			if len(parts) >= 3 {
				if w, err := strconv.Atoi(parts[2]); err == nil && w > 0 {
					weight = w
				}
			}

			backendConfig := BackendConfig{
				ID:              parts[0],
				URL:             parts[1],
				Weight:          weight,
				HealthCheckPath: getEnv("LB_BACKEND_HEALTH_PATH", "/health"),
				MaxConnections:  getEnvInt("LB_BACKEND_MAX_CONNECTIONS", 100),
				Timeout:         getEnvDuration("LB_BACKEND_TIMEOUT", 30*time.Second),
			}

			backendConfigs = append(backendConfigs, backendConfig)
		}
	}

	return backendConfigs
}

// getEnvInt gets environment variable as integer with fallback
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvDuration gets environment variable as duration with fallback
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// LoadConfig loads configuration with priority: env vars > config file > defaults
// This implements 12-Factor App methodology - Factor #3: Config
func LoadConfig() (*Config, error) {
	var config *Config

	// First, try to load from file if CONFIG_FILE is specified
	configFile := getEnv("CONFIG_FILE", "config.yaml")
	if configFile != "" {
		if _, err := os.Stat(configFile); err == nil {
			config, err = LoadFromFile(configFile)
			if err != nil {
				// Log warning but don't fail - use environment instead
				fmt.Printf("Warning: Failed to load config from file %s: %v\n", configFile, err)
				config = DefaultConfig()
			}
		} else {
			// Config file doesn't exist, use defaults
			config = DefaultConfig()
		}
	} else {
		config = DefaultConfig()
	}

	// Override with environment variables (highest priority)
	envConfig := LoadFromEnvironment()

	// Merge configurations (env takes precedence)
	mergeConfigs(config, envConfig)

	// Validate final configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// mergeConfigs merges environment config into base config
func mergeConfigs(base, env *Config) {
	// Load balancer settings
	if env.LoadBalancer.Port != 8080 { // Only override if different from default
		base.LoadBalancer.Port = env.LoadBalancer.Port
	}
	if env.LoadBalancer.Strategy != "round_robin" {
		base.LoadBalancer.Strategy = env.LoadBalancer.Strategy
	}
	if env.LoadBalancer.MaxRetries != 3 {
		base.LoadBalancer.MaxRetries = env.LoadBalancer.MaxRetries
	}
	if env.LoadBalancer.RetryDelay != 100*time.Millisecond {
		base.LoadBalancer.RetryDelay = env.LoadBalancer.RetryDelay
	}
	if env.LoadBalancer.Timeout != 30*time.Second {
		base.LoadBalancer.Timeout = env.LoadBalancer.Timeout
	}

	// Health check settings
	mergeHealthCheckConfig(&base.LoadBalancer.HealthCheck, &env.LoadBalancer.HealthCheck)

	// Rate limit settings
	mergeRateLimitConfig(&base.LoadBalancer.RateLimit, &env.LoadBalancer.RateLimit)

	// Circuit breaker settings
	mergeCircuitBreakerConfig(&base.LoadBalancer.CircuitBreaker, &env.LoadBalancer.CircuitBreaker)

	// Backends - env overrides file completely if specified
	if len(env.Backends) > 0 {
		base.Backends = env.Backends
	}

	// Logging settings
	if env.Logging.Level != "info" {
		base.Logging.Level = env.Logging.Level
	}
	if env.Logging.Format != "json" {
		base.Logging.Format = env.Logging.Format
	}
	if env.Logging.Output != "stdout" {
		base.Logging.Output = env.Logging.Output
	}
	if env.Logging.File != "" {
		base.Logging.File = env.Logging.File
	}
}

func mergeHealthCheckConfig(base, env *domain.HealthCheckConfig) {
	// Only merge non-default values from environment
	if os.Getenv("LB_HEALTH_CHECK_ENABLED") != "" {
		base.Enabled = env.Enabled
	}
	if os.Getenv("LB_HEALTH_CHECK_INTERVAL") != "" {
		base.Interval = env.Interval
	}
	if os.Getenv("LB_HEALTH_CHECK_TIMEOUT") != "" {
		base.Timeout = env.Timeout
	}
	if os.Getenv("LB_HEALTH_CHECK_HEALTHY_THRESHOLD") != "" {
		base.HealthyThreshold = env.HealthyThreshold
	}
	if os.Getenv("LB_HEALTH_CHECK_UNHEALTHY_THRESHOLD") != "" {
		base.UnhealthyThreshold = env.UnhealthyThreshold
	}
	if os.Getenv("LB_HEALTH_CHECK_PATH") != "" {
		base.Path = env.Path
	}
}

func mergeRateLimitConfig(base, env *domain.RateLimitConfig) {
	if os.Getenv("LB_RATE_LIMIT_ENABLED") != "" {
		base.Enabled = env.Enabled
	}
	if os.Getenv("LB_RATE_LIMIT_RPS") != "" {
		base.RequestsPerSecond = env.RequestsPerSecond
	}
	if os.Getenv("LB_RATE_LIMIT_BURST") != "" {
		base.BurstSize = env.BurstSize
	}
}

func mergeCircuitBreakerConfig(base, env *domain.CircuitBreakerConfig) {
	if os.Getenv("LB_CIRCUIT_BREAKER_ENABLED") != "" {
		base.Enabled = env.Enabled
	}
	if os.Getenv("LB_CIRCUIT_BREAKER_FAILURE_THRESHOLD") != "" {
		base.FailureThreshold = env.FailureThreshold
	}
	if os.Getenv("LB_CIRCUIT_BREAKER_RECOVERY_TIMEOUT") != "" {
		base.RecoveryTimeout = env.RecoveryTimeout
	}
	if os.Getenv("LB_CIRCUIT_BREAKER_MAX_REQUESTS") != "" {
		base.MaxRequests = env.MaxRequests
	}
}
