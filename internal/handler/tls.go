package handler

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// TLSHandler manages TLS termination and configuration
type TLSHandler struct {
	config domain.TLSConfig
	logger *logger.Logger
}

// NewTLSHandler creates a new TLS handler
func NewTLSHandler(config domain.TLSConfig, logger *logger.Logger) *TLSHandler {
	return &TLSHandler{
		config: config,
		logger: logger,
	}
}

// ConfigureTLS configures TLS settings for the HTTP server
func (h *TLSHandler) ConfigureTLS() (*tls.Config, error) {
	if !h.config.Enabled {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12, // Default to TLS 1.2
		MaxVersion: tls.VersionTLS13, // Default to TLS 1.3
	}

	// Configure minimum TLS version
	if h.config.MinVersion != "" {
		minVersion, err := h.parseTLSVersion(h.config.MinVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid min TLS version: %w", err)
		}
		tlsConfig.MinVersion = minVersion
	}

	// Configure maximum TLS version
	if h.config.MaxVersion != "" {
		maxVersion, err := h.parseTLSVersion(h.config.MaxVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid max TLS version: %w", err)
		}
		tlsConfig.MaxVersion = maxVersion
	}

	// Configure cipher suites if specified
	if len(h.config.CipherSuites) > 0 {
		cipherSuites, err := h.parseCipherSuites(h.config.CipherSuites)
		if err != nil {
			return nil, fmt.Errorf("invalid cipher suites: %w", err)
		}
		tlsConfig.CipherSuites = cipherSuites
	}

	// Security best practices
	tlsConfig.PreferServerCipherSuites = true
	tlsConfig.CurvePreferences = []tls.CurveID{
		tls.X25519,
		tls.CurveP384,
		tls.CurveP256,
	}

	h.logger.WithFields(map[string]interface{}{
		"component":    "tls",
		"min_version":  h.formatTLSVersion(tlsConfig.MinVersion),
		"max_version":  h.formatTLSVersion(tlsConfig.MaxVersion),
		"cipher_count": len(tlsConfig.CipherSuites),
	}).Info("TLS configuration loaded")

	return tlsConfig, nil
}

// RedirectHTTP creates a handler that redirects HTTP traffic to HTTPS
func (h *TLSHandler) RedirectHTTP(httpsPort int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.Host

		// Remove port if present
		if colonIndex := strings.LastIndex(host, ":"); colonIndex != -1 {
			host = host[:colonIndex]
		}

		// Add HTTPS port if not default
		if httpsPort != 443 {
			host = fmt.Sprintf("%s:%d", host, httpsPort)
		}

		httpsURL := fmt.Sprintf("https://%s%s", host, r.RequestURI)

		h.logger.WithFields(map[string]interface{}{
			"component":    "tls",
			"original_url": r.URL.String(),
			"redirect_url": httpsURL,
		}).Debug("Redirecting HTTP to HTTPS")

		http.Redirect(w, r, httpsURL, http.StatusMovedPermanently)
	}
}

// parseTLSVersion parses TLS version string to tls constant
func (h *TLSHandler) parseTLSVersion(version string) (uint16, error) {
	switch strings.ToUpper(version) {
	case "1.0", "TLS1.0", "TLSV1.0":
		return tls.VersionTLS10, nil
	case "1.1", "TLS1.1", "TLSV1.1":
		return tls.VersionTLS11, nil
	case "1.2", "TLS1.2", "TLSV1.2":
		return tls.VersionTLS12, nil
	case "1.3", "TLS1.3", "TLSV1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported TLS version: %s", version)
	}
}

// parseCipherSuites parses cipher suite names to tls constants
func (h *TLSHandler) parseCipherSuites(suites []string) ([]uint16, error) {
	var cipherSuites []uint16

	// Map of cipher suite names to constants
	cipherMap := map[string]uint16{
		"TLS_AES_128_GCM_SHA256":                  tls.TLS_AES_128_GCM_SHA256,
		"TLS_AES_256_GCM_SHA384":                  tls.TLS_AES_256_GCM_SHA384,
		"TLS_CHACHA20_POLY1305_SHA256":            tls.TLS_CHACHA20_POLY1305_SHA256,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256": tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384": tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305":  tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":   tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":   tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305":    tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
	}

	for _, suite := range suites {
		if cipher, exists := cipherMap[strings.ToUpper(suite)]; exists {
			cipherSuites = append(cipherSuites, cipher)
		} else {
			// Try to parse as numeric value
			if val, err := strconv.ParseUint(suite, 0, 16); err == nil {
				cipherSuites = append(cipherSuites, uint16(val))
			} else {
				return nil, fmt.Errorf("unknown cipher suite: %s", suite)
			}
		}
	}

	return cipherSuites, nil
}

// formatTLSVersion converts TLS version constant to readable string
func (h *TLSHandler) formatTLSVersion(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("Unknown (%d)", version)
	}
}

// GetTLSInfo returns information about TLS configuration
func (h *TLSHandler) GetTLSInfo(r *http.Request) map[string]interface{} {
	info := map[string]interface{}{
		"tls_enabled": h.config.Enabled,
	}

	if r.TLS != nil {
		info["tls_version"] = h.formatTLSVersion(r.TLS.Version)
		info["cipher_suite"] = tls.CipherSuiteName(r.TLS.CipherSuite)
		info["server_name"] = r.TLS.ServerName

		if len(r.TLS.PeerCertificates) > 0 {
			cert := r.TLS.PeerCertificates[0]
			info["client_cert"] = map[string]interface{}{
				"subject": cert.Subject.String(),
				"issuer":  cert.Issuer.String(),
			}
		}
	}

	return info
}
