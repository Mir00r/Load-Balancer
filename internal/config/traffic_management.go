package config

import "time"

// TrafficManagementConfig contains advanced traffic management configuration
type TrafficManagementConfig struct {
	// Session Stickiness
	SessionStickiness SessionStickinessConfig `yaml:"session_stickiness"`

	// Blue-Green Deployment
	BlueGreenDeployment BlueGreenConfig `yaml:"blue_green"`

	// Canary Deployment
	CanaryDeployment CanaryConfig `yaml:"canary"`

	// Traffic Mirroring
	TrafficMirroring MirroringConfig `yaml:"traffic_mirroring"`

	// Circuit Breaker
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker"`
}

// SessionStickinessConfig configures session affinity
type SessionStickinessConfig struct {
	Enabled    bool          `yaml:"enabled"`
	CookieName string        `yaml:"cookie_name"`
	HeaderName string        `yaml:"header_name"`
	TTL        time.Duration `yaml:"ttl"`
}

// BlueGreenConfig configures blue-green deployment strategy
type BlueGreenConfig struct {
	Enabled       bool     `yaml:"enabled"`
	BlueBackends  []string `yaml:"blue_backends"`
	GreenBackends []string `yaml:"green_backends"`
	ActiveSlot    string   `yaml:"active_slot"`   // "blue" or "green"
	SwitchHeader  string   `yaml:"switch_header"` // Header to override active slot
}

// CanaryConfig configures canary deployment strategy
type CanaryConfig struct {
	Enabled        bool            `yaml:"enabled"`
	CanaryBackends []CanaryBackend `yaml:"canary_backends"`
	TrafficSplit   int             `yaml:"traffic_split"` // Percentage to canary (0-100)
	SplitHeader    string          `yaml:"split_header"`  // Header to force canary routing
}

// CanaryBackend represents a canary backend with weight
type CanaryBackend struct {
	URL       string `yaml:"url"`
	BackendID string `yaml:"backend_id"`
	Weight    int    `yaml:"weight"`
}

// MirroringConfig configures traffic mirroring (shadowing)
type MirroringConfig struct {
	Enabled    bool           `yaml:"enabled"`
	Targets    []MirrorTarget `yaml:"targets"`
	Async      bool           `yaml:"async"`       // Mirror requests asynchronously
	SampleRate float64        `yaml:"sample_rate"` // Rate of requests to mirror (0.0-1.0)
}

// MirrorTarget represents a traffic mirror destination
type MirrorTarget struct {
	URL        string            `yaml:"url"`
	Name       string            `yaml:"name"`
	Headers    map[string]string `yaml:"headers,omitempty"`
	Timeout    time.Duration     `yaml:"timeout"`
	IgnoreBody bool              `yaml:"ignore_body"` // Don't send request body
}

// CircuitBreakerConfig configures circuit breaker pattern
type CircuitBreakerConfig struct {
	Enabled                bool          `yaml:"enabled"`
	FailureThreshold       int           `yaml:"failure_threshold"`        // Number of failures to open circuit
	SuccessThreshold       int           `yaml:"success_threshold"`        // Number of successes to close circuit
	RecoveryTimeout        time.Duration `yaml:"recovery_timeout"`         // Time to wait before trying again
	RequestVolumeThreshold int           `yaml:"request_volume_threshold"` // Minimum requests before circuit can trip
}

// CircuitBreakerState represents the circuit breaker states
type CircuitBreakerState string

const (
	CircuitBreakerClosed   CircuitBreakerState = "closed"
	CircuitBreakerOpen     CircuitBreakerState = "open"
	CircuitBreakerHalfOpen CircuitBreakerState = "half-open"
)

// CircuitBreakerStats tracks circuit breaker statistics
type CircuitBreakerStats struct {
	State            CircuitBreakerState `json:"state"`
	Failures         int                 `json:"failures"`
	Successes        int                 `json:"successes"`
	Requests         int                 `json:"requests"`
	LastFailureTime  time.Time           `json:"last_failure_time"`
	LastRecoveryTime time.Time           `json:"last_recovery_time"`
	NextRetryTime    time.Time           `json:"next_retry_time"`
}
