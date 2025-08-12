package config

import (
	"fmt"
	"time"

	"github.com/mir00r/load-balancer/internal/middleware"
)

// TraefikStyleConfig extends the main configuration with advanced Traefik-style features
// This provides comprehensive traffic management, security, and observability capabilities
type TraefikStyleConfig struct {
	// Traffic Management
	TrafficManagement TrafficManagementConfig `yaml:"traffic_management"`

	// Security Features
	JWT *middleware.JWTAuthConfig `yaml:"jwt,omitempty"`
	WAF *middleware.WAFConfig     `yaml:"waf,omitempty"`

	// Observability
	Observability *middleware.ObservabilityConfig `yaml:"observability,omitempty"`

	// Protocol Support
	HTTP3 *HTTP3Config `yaml:"http3,omitempty"`
	GRPC  *GRPCConfig  `yaml:"grpc,omitempty"`

	// Service Discovery
	ServiceDiscovery *ServiceDiscoveryConfig `yaml:"service_discovery,omitempty"`

	// Let's Encrypt Integration
	LetsEncrypt *LetsEncryptConfig `yaml:"lets_encrypt,omitempty"`

	// Plugin System
	Plugins *PluginConfig `yaml:"plugins,omitempty"`
}

// HTTP3Config configures HTTP/3 support
type HTTP3Config struct {
	Enabled               bool          `yaml:"enabled"`
	Port                  int           `yaml:"port"`
	CertFile              string        `yaml:"cert_file"`
	KeyFile               string        `yaml:"key_file"`
	MaxStreamTimeout      time.Duration `yaml:"max_stream_timeout"`
	MaxIdleTimeout        time.Duration `yaml:"max_idle_timeout"`
	MaxIncomingStreams    int64         `yaml:"max_incoming_streams"`
	MaxIncomingUniStreams int64         `yaml:"max_incoming_uni_streams"`
	KeepAlive             bool          `yaml:"keep_alive"`
	EnableDatagrams       bool          `yaml:"enable_datagrams"`
}

// GRPCConfig configures gRPC protocol support
type GRPCConfig struct {
	Enabled           bool          `yaml:"enabled"`
	Timeout           time.Duration `yaml:"timeout"`
	EnableH2C         bool          `yaml:"enable_h2c"`
	MaxReceiveSize    int           `yaml:"max_receive_size"`
	MaxSendSize       int           `yaml:"max_send_size"`
	EnableReflection  bool          `yaml:"enable_reflection"`
	EnableCompression bool          `yaml:"enable_compression"`
}

// ServiceDiscoveryConfig configures service discovery integration
type ServiceDiscoveryConfig struct {
	Enabled   bool                     `yaml:"enabled"`
	Provider  string                   `yaml:"provider"` // "consul", "etcd", "kubernetes"
	Endpoints []string                 `yaml:"endpoints"`
	Namespace string                   `yaml:"namespace,omitempty"`
	Interval  time.Duration            `yaml:"interval"`
	Timeout   time.Duration            `yaml:"timeout"`
	TLS       *ServiceDiscoveryTLS     `yaml:"tls,omitempty"`
	Auth      *ServiceDiscoveryAuth    `yaml:"auth,omitempty"`
	Filters   []ServiceDiscoveryFilter `yaml:"filters,omitempty"`
}

// ServiceDiscoveryTLS configures TLS for service discovery
type ServiceDiscoveryTLS struct {
	Enabled            bool   `yaml:"enabled"`
	CertFile           string `yaml:"cert_file"`
	KeyFile            string `yaml:"key_file"`
	CAFile             string `yaml:"ca_file"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

// ServiceDiscoveryAuth configures authentication for service discovery
type ServiceDiscoveryAuth struct {
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	Token    string `yaml:"token,omitempty"`
}

// ServiceDiscoveryFilter configures service filtering
type ServiceDiscoveryFilter struct {
	Key      string   `yaml:"key"`
	Values   []string `yaml:"values"`
	Operator string   `yaml:"operator"` // "equals", "contains", "regex"
}

// LetsEncryptConfig configures Let's Encrypt automatic HTTPS
type LetsEncryptConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Email         string   `yaml:"email"`
	Domains       []string `yaml:"domains"`
	ChallengeType string   `yaml:"challenge_type"` // "http", "dns", "tls"
	DNSProvider   string   `yaml:"dns_provider,omitempty"`
	StoragePath   string   `yaml:"storage_path"`
	KeyType       string   `yaml:"key_type"` // "rsa2048", "rsa4096", "ec256", "ec384"
	RenewalDays   int      `yaml:"renewal_days"`
	TestMode      bool     `yaml:"test_mode"` // Use staging environment
}

// PluginConfig configures the plugin system
type PluginConfig struct {
	Enabled     bool         `yaml:"enabled"`
	Directory   string       `yaml:"directory"`
	WASMPlugins []WASMPlugin `yaml:"wasm_plugins,omitempty"`
	HTTPPlugins []HTTPPlugin `yaml:"http_plugins,omitempty"`
}

// WASMPlugin represents a WebAssembly plugin
type WASMPlugin struct {
	Name     string                 `yaml:"name"`
	Path     string                 `yaml:"path"`
	Enabled  bool                   `yaml:"enabled"`
	Config   map[string]interface{} `yaml:"config,omitempty"`
	Priority int                    `yaml:"priority"`
	Paths    []string               `yaml:"paths,omitempty"`   // Specific paths to apply plugin
	Methods  []string               `yaml:"methods,omitempty"` // Specific methods to apply plugin
}

// HTTPPlugin represents an HTTP-based plugin
type HTTPPlugin struct {
	Name     string                 `yaml:"name"`
	URL      string                 `yaml:"url"`
	Enabled  bool                   `yaml:"enabled"`
	Timeout  time.Duration          `yaml:"timeout"`
	Config   map[string]interface{} `yaml:"config,omitempty"`
	Priority int                    `yaml:"priority"`
	Headers  map[string]string      `yaml:"headers,omitempty"`
}

// DefaultTraefikStyleConfig returns a default Traefik-style configuration
func DefaultTraefikStyleConfig() TraefikStyleConfig {
	return TraefikStyleConfig{
		// Traffic Management
		TrafficManagement: TrafficManagementConfig{
			SessionStickiness: SessionStickinessConfig{
				Enabled:    false,
				CookieName: "lb_session",
				HeaderName: "X-Session-ID",
				TTL:        time.Hour,
			},
			BlueGreenDeployment: BlueGreenConfig{
				Enabled:       false,
				BlueBackends:  []string{},
				GreenBackends: []string{},
				ActiveSlot:    "blue",
				SwitchHeader:  "X-Deployment-Slot",
			},
			CanaryDeployment: CanaryConfig{
				Enabled:        false,
				CanaryBackends: []CanaryBackend{},
				TrafficSplit:   10,
				SplitHeader:    "X-Canary",
			},
			TrafficMirroring: MirroringConfig{
				Enabled:    false,
				Targets:    []MirrorTarget{},
				Async:      true,
				SampleRate: 0.1,
			},
			CircuitBreaker: CircuitBreakerConfig{
				Enabled:                true,
				FailureThreshold:       5,
				SuccessThreshold:       3,
				RecoveryTimeout:        30 * time.Second,
				RequestVolumeThreshold: 20,
			},
		},

		// JWT Authentication
		JWT: &middleware.JWTAuthConfig{
			Enabled:            false,
			Algorithm:          "RS256",
			TokenExpiry:        24 * time.Hour,
			ClockSkew:          5 * time.Minute,
			RequiredClaims:     []string{"sub", "exp"},
			AudienceValidation: false,
			IssuerValidation:   false,
			PathRules:          []middleware.JWTPathRule{},
			Users:              []middleware.JWTUser{},
		},

		// Web Application Firewall
		WAF: &middleware.WAFConfig{
			Enabled:              false,
			Mode:                 "monitor",
			MaxRequestSize:       10 * 1024 * 1024, // 10MB
			MaxHeaderSize:        8192,
			MaxURILength:         2048,
			MaxQueryStringLength: 1024,
			BlockedUserAgents:    []string{"bot", "crawler", "scanner"},
			AllowedMethods:       []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
			RateLimiting: middleware.WAFRateLimitConfig{
				Enabled:         true,
				RequestsPerMin:  100,
				WindowSize:      time.Minute,
				CleanupInterval: 5 * time.Minute,
			},
			SecurityHeaders: middleware.SecurityHeaders{
				Enabled:                 true,
				ContentTypeNosniff:      true,
				FrameOptions:            "SAMEORIGIN",
				XSSProtection:           "1; mode=block",
				ContentSecurityPolicy:   "default-src 'self'",
				StrictTransportSecurity: "max-age=31536000; includeSubDomains",
				ReferrerPolicy:          "strict-origin-when-cross-origin",
			},
			Rules:              []middleware.WAFRule{},
			EnableBotDetection: true,
		},

		// Observability
		Observability: &middleware.ObservabilityConfig{
			Enabled: true,
			Tracing: middleware.TracingConfig{
				Enabled:        true,
				ServiceName:    "traefik-style-load-balancer",
				ServiceVersion: "1.0.0",
				Environment:    "production",
				SampleRate:     0.1,
				MaxSpanEvents:  128,
				Exporters:      []middleware.TracingExporter{},
			},
			Metrics: middleware.MetricsConfig{
				Enabled:          true,
				Prefix:           "traefik_lb",
				Labels:           map[string]string{"service": "load_balancer"},
				Exporters:        []middleware.MetricsExporter{},
				HistogramBuckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			Logging: middleware.LoggingConfig{
				Enabled:          true,
				StructuredFormat: true,
				IncludeTrace:     true,
				IncludeSpan:      true,
				SensitiveHeaders: []string{"Authorization", "X-API-Key", "Cookie"},
				Fields:           map[string]string{"service": "load_balancer"},
			},
			DistributedTracing: middleware.DistributedTracingConfig{
				Enabled:          true,
				TraceHeader:      "X-Trace-ID",
				SpanHeader:       "X-Span-ID",
				BaggageHeader:    "X-Baggage",
				PropagateHeaders: []string{"X-Request-ID", "X-Correlation-ID"},
				CustomTags:       map[string]string{"version": "1.0.0"},
			},
		},

		// HTTP/3 Support
		HTTP3: &HTTP3Config{
			Enabled:               false,
			Port:                  8443,
			MaxStreamTimeout:      30 * time.Second,
			MaxIdleTimeout:        5 * time.Minute,
			MaxIncomingStreams:    100,
			MaxIncomingUniStreams: 100,
			KeepAlive:             true,
			EnableDatagrams:       false,
		},

		// gRPC Support
		GRPC: &GRPCConfig{
			Enabled:           false,
			Timeout:           30 * time.Second,
			EnableH2C:         false,
			MaxReceiveSize:    4 * 1024 * 1024, // 4MB
			MaxSendSize:       4 * 1024 * 1024, // 4MB
			EnableReflection:  false,
			EnableCompression: true,
		},

		// Service Discovery
		ServiceDiscovery: &ServiceDiscoveryConfig{
			Enabled:   false,
			Provider:  "consul",
			Endpoints: []string{"localhost:8500"},
			Interval:  30 * time.Second,
			Timeout:   10 * time.Second,
			Filters:   []ServiceDiscoveryFilter{},
		},

		// Let's Encrypt
		LetsEncrypt: &LetsEncryptConfig{
			Enabled:       false,
			ChallengeType: "http",
			KeyType:       "ec256",
			RenewalDays:   30,
			TestMode:      true,
		},

		// Plugin System
		Plugins: &PluginConfig{
			Enabled:     false,
			Directory:   "./plugins",
			WASMPlugins: []WASMPlugin{},
			HTTPPlugins: []HTTPPlugin{},
		},
	}
}

// Validate validates the Traefik-style configuration
func (config *TraefikStyleConfig) Validate() error {
	// Validate traffic management
	if config.TrafficManagement.CanaryDeployment.Enabled {
		if config.TrafficManagement.CanaryDeployment.TrafficSplit < 0 ||
			config.TrafficManagement.CanaryDeployment.TrafficSplit > 100 {
			return fmt.Errorf("canary traffic split must be between 0 and 100")
		}
	}

	if config.TrafficManagement.TrafficMirroring.Enabled {
		if config.TrafficManagement.TrafficMirroring.SampleRate < 0 ||
			config.TrafficManagement.TrafficMirroring.SampleRate > 1 {
			return fmt.Errorf("traffic mirroring sample rate must be between 0.0 and 1.0")
		}
	}

	// Validate JWT configuration
	if config.JWT != nil && config.JWT.Enabled {
		if config.JWT.Algorithm == "" {
			return fmt.Errorf("JWT algorithm is required when JWT is enabled")
		}

		if config.JWT.Algorithm == "HS256" && config.JWT.SecretKey == "" {
			return fmt.Errorf("JWT secret key is required for HMAC algorithms")
		}

		if (config.JWT.Algorithm == "RS256" || config.JWT.Algorithm == "RS384" ||
			config.JWT.Algorithm == "RS512") && config.JWT.PublicKeyPath == "" {
			return fmt.Errorf("JWT public key path is required for RSA algorithms")
		}
	}

	// Validate WAF configuration
	if config.WAF != nil && config.WAF.Enabled {
		validModes := []string{"block", "monitor", "off"}
		validMode := false
		for _, mode := range validModes {
			if config.WAF.Mode == mode {
				validMode = true
				break
			}
		}
		if !validMode {
			return fmt.Errorf("WAF mode must be one of: %v", validModes)
		}

		if config.WAF.MaxRequestSize <= 0 {
			return fmt.Errorf("WAF max request size must be positive")
		}
	}

	// Validate observability configuration
	if config.Observability != nil && config.Observability.Enabled {
		if config.Observability.Tracing.Enabled {
			if config.Observability.Tracing.ServiceName == "" {
				return fmt.Errorf("tracing service name is required when tracing is enabled")
			}

			if config.Observability.Tracing.SampleRate < 0 ||
				config.Observability.Tracing.SampleRate > 1 {
				return fmt.Errorf("tracing sample rate must be between 0.0 and 1.0")
			}
		}
	}

	// Validate HTTP/3 configuration
	if config.HTTP3 != nil && config.HTTP3.Enabled {
		if config.HTTP3.Port <= 0 || config.HTTP3.Port > 65535 {
			return fmt.Errorf("HTTP/3 port must be between 1 and 65535")
		}

		if config.HTTP3.CertFile == "" || config.HTTP3.KeyFile == "" {
			return fmt.Errorf("HTTP/3 requires certificate and key files")
		}
	}

	// Validate service discovery configuration
	if config.ServiceDiscovery != nil && config.ServiceDiscovery.Enabled {
		validProviders := []string{"consul", "etcd", "kubernetes"}
		validProvider := false
		for _, provider := range validProviders {
			if config.ServiceDiscovery.Provider == provider {
				validProvider = true
				break
			}
		}
		if !validProvider {
			return fmt.Errorf("service discovery provider must be one of: %v", validProviders)
		}

		if len(config.ServiceDiscovery.Endpoints) == 0 {
			return fmt.Errorf("service discovery requires at least one endpoint")
		}
	}

	// Validate Let's Encrypt configuration
	if config.LetsEncrypt != nil && config.LetsEncrypt.Enabled {
		if config.LetsEncrypt.Email == "" {
			return fmt.Errorf("Let's Encrypt email is required")
		}

		if len(config.LetsEncrypt.Domains) == 0 {
			return fmt.Errorf("Let's Encrypt requires at least one domain")
		}

		validChallenges := []string{"http", "dns", "tls"}
		validChallenge := false
		for _, challenge := range validChallenges {
			if config.LetsEncrypt.ChallengeType == challenge {
				validChallenge = true
				break
			}
		}
		if !validChallenge {
			return fmt.Errorf("Let's Encrypt challenge type must be one of: %v", validChallenges)
		}
	}

	return nil
}

// GetFeatureSummary returns a summary of enabled features
func (config *TraefikStyleConfig) GetFeatureSummary() map[string]interface{} {
	return map[string]interface{}{
		"traffic_management": map[string]bool{
			"session_stickiness": config.TrafficManagement.SessionStickiness.Enabled,
			"blue_green":         config.TrafficManagement.BlueGreenDeployment.Enabled,
			"canary":             config.TrafficManagement.CanaryDeployment.Enabled,
			"traffic_mirroring":  config.TrafficManagement.TrafficMirroring.Enabled,
			"circuit_breaker":    config.TrafficManagement.CircuitBreaker.Enabled,
		},
		"security": map[string]bool{
			"jwt_enabled": config.JWT != nil && config.JWT.Enabled,
			"waf_enabled": config.WAF != nil && config.WAF.Enabled,
		},
		"protocols": map[string]bool{
			"http3_enabled": config.HTTP3 != nil && config.HTTP3.Enabled,
			"grpc_enabled":  config.GRPC != nil && config.GRPC.Enabled,
		},
		"observability": map[string]bool{
			"tracing_enabled": config.Observability != nil && config.Observability.Tracing.Enabled,
			"metrics_enabled": config.Observability != nil && config.Observability.Metrics.Enabled,
		},
		"integrations": map[string]bool{
			"service_discovery": config.ServiceDiscovery != nil && config.ServiceDiscovery.Enabled,
			"lets_encrypt":      config.LetsEncrypt != nil && config.LetsEncrypt.Enabled,
			"plugins":           config.Plugins != nil && config.Plugins.Enabled,
		},
	}
}
