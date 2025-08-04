package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/mir00r/load-balancer/pkg/logger"
	"golang.org/x/net/http2"
)

// TLSConfig defines TLS/SSL configuration
type TLSConfig struct {
	Enabled    bool     `yaml:"enabled"`
	CertFile   string   `yaml:"cert_file"`
	KeyFile    string   `yaml:"key_file"`
	MinVersion string   `yaml:"min_version"` // "1.0", "1.1", "1.2", "1.3"
	MaxVersion string   `yaml:"max_version"`
	Ciphers    []string `yaml:"ciphers"`
	// HTTP/2 support
	HTTP2Enabled bool `yaml:"http2_enabled"`
	// Auto-redirect HTTP to HTTPS
	RedirectHTTP bool `yaml:"redirect_http"`
	HTTPPort     int  `yaml:"http_port"`
	HTTPSPort    int  `yaml:"https_port"`
}

// EnhancedServer provides HTTP/2 and TLS support
type EnhancedServer struct {
	config      TLSConfig
	logger      *logger.Logger
	httpServer  *http.Server
	httpsServer *http.Server
}

// NewEnhancedServer creates a new enhanced server with TLS and HTTP/2 support
func NewEnhancedServer(config TLSConfig, handler http.Handler, logger *logger.Logger) *EnhancedServer {
	return &EnhancedServer{
		config: config,
		logger: logger,
	}
}

// Start starts the enhanced server with TLS and HTTP/2 support
func (s *EnhancedServer) Start(handler http.Handler) error {
	if s.config.Enabled {
		return s.startHTTPS(handler)
	}
	return s.startHTTP(handler)
}

// startHTTPS starts HTTPS server with optional HTTP/2
func (s *EnhancedServer) startHTTPS(handler http.Handler) error {
	// Create TLS configuration
	tlsConfig := &tls.Config{
		MinVersion: s.getTLSVersion(s.config.MinVersion),
		MaxVersion: s.getTLSVersion(s.config.MaxVersion),
	}

	// Configure cipher suites if specified
	if len(s.config.Ciphers) > 0 {
		tlsConfig.CipherSuites = s.getCipherSuites()
	}

	// Configure HTTPS server
	s.httpsServer = &http.Server{
		Addr:      fmt.Sprintf(":%d", s.config.HTTPSPort),
		Handler:   handler,
		TLSConfig: tlsConfig,
		// Timeouts
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Enable HTTP/2 if configured
	if s.config.HTTP2Enabled {
		if err := http2.ConfigureServer(s.httpsServer, &http2.Server{
			MaxConcurrentStreams: 1000,
			MaxReadFrameSize:     1048576, // 1MB
			IdleTimeout:          300 * time.Second,
		}); err != nil {
			s.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Error("Failed to configure HTTP/2")
			return err
		}
		s.logger.Info("HTTP/2 support enabled")
	}

	// Start HTTP redirect server if configured
	if s.config.RedirectHTTP {
		go s.startHTTPRedirect()
	}

	s.logger.WithFields(map[string]interface{}{
		"port":          s.config.HTTPSPort,
		"tls_enabled":   true,
		"http2_enabled": s.config.HTTP2Enabled,
		"cert_file":     s.config.CertFile,
	}).Info("Starting HTTPS server")

	return s.httpsServer.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile)
}

// startHTTP starts regular HTTP server
func (s *EnhancedServer) startHTTP(handler http.Handler) error {
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.config.HTTPPort),
		Handler: handler,
		// Timeouts
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.logger.WithFields(map[string]interface{}{
		"port":        s.config.HTTPPort,
		"tls_enabled": false,
	}).Info("Starting HTTP server")

	return s.httpServer.ListenAndServe()
}

// startHTTPRedirect starts HTTP server that redirects to HTTPS
func (s *EnhancedServer) startHTTPRedirect() {
	redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpsURL := fmt.Sprintf("https://%s:%d%s",
			r.Host, s.config.HTTPSPort, r.RequestURI)

		s.logger.WithFields(map[string]interface{}{
			"from": r.URL.String(),
			"to":   httpsURL,
		}).Debug("Redirecting HTTP to HTTPS")

		http.Redirect(w, r, httpsURL, http.StatusMovedPermanently)
	})

	redirectServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.HTTPPort),
		Handler:      redirectHandler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	s.logger.WithFields(map[string]interface{}{
		"port":        s.config.HTTPPort,
		"redirect_to": s.config.HTTPSPort,
	}).Info("Starting HTTP redirect server")

	if err := redirectServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		s.logger.WithFields(map[string]interface{}{
			"error": err.Error(),
		}).Error("HTTP redirect server failed")
	}
}

// Shutdown gracefully shuts down the servers
func (s *EnhancedServer) Shutdown(ctx context.Context) error {
	var err error

	if s.httpsServer != nil {
		if shutdownErr := s.httpsServer.Shutdown(ctx); shutdownErr != nil {
			s.logger.WithFields(map[string]interface{}{
				"error": shutdownErr.Error(),
			}).Error("Failed to shutdown HTTPS server")
			err = shutdownErr
		}
	}

	if s.httpServer != nil {
		if shutdownErr := s.httpServer.Shutdown(ctx); shutdownErr != nil {
			s.logger.WithFields(map[string]interface{}{
				"error": shutdownErr.Error(),
			}).Error("Failed to shutdown HTTP server")
			err = shutdownErr
		}
	}

	return err
}

// getTLSVersion converts string version to TLS constant
func (s *EnhancedServer) getTLSVersion(version string) uint16 {
	switch version {
	case "1.0":
		return tls.VersionTLS10
	case "1.1":
		return tls.VersionTLS11
	case "1.2":
		return tls.VersionTLS12
	case "1.3":
		return tls.VersionTLS13
	default:
		return tls.VersionTLS12 // Default to TLS 1.2
	}
}

// getCipherSuites converts cipher names to TLS constants
func (s *EnhancedServer) getCipherSuites() []uint16 {
	cipherMap := map[string]uint16{
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":         tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":         tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256":       tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384":       tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256":   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256": tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	}

	var ciphers []uint16
	for _, cipherName := range s.config.Ciphers {
		if cipher, exists := cipherMap[cipherName]; exists {
			ciphers = append(ciphers, cipher)
		} else {
			s.logger.WithFields(map[string]interface{}{
				"cipher": cipherName,
			}).Warn("Unknown cipher suite")
		}
	}

	return ciphers
}

// GenerateSelfSignedCert generates a self-signed certificate for testing
func GenerateSelfSignedCert(certFile, keyFile string) error {
	// This would contain certificate generation logic
	// For production, use proper CA-signed certificates
	return fmt.Errorf("certificate generation not implemented - use openssl or similar tools")
}
