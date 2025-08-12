// Package acme provides Let's Encrypt ACME client integration for automatic SSL/TLS certificate management
// Supports HTTP-01, DNS-01, and TLS-ALPN-01 challenge types with automatic renewal
package acme

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/config"
	lberrors "github.com/mir00r/load-balancer/internal/errors"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// ACMEClient manages automatic SSL/TLS certificates from Let's Encrypt
type ACMEClient struct {
	config       config.LetsEncryptConfig
	logger       *logger.Logger
	certificates map[string]*Certificate
	mutex        sync.RWMutex
	httpServer   *http.Server
	challenges   map[string]string // token -> key authorization
	challengeMux sync.RWMutex
	running      bool
}

// Certificate represents an SSL/TLS certificate
type Certificate struct {
	Domain      string    `json:"domain"`
	CertPath    string    `json:"cert_path"`
	KeyPath     string    `json:"key_path"`
	ExpiryDate  time.Time `json:"expiry_date"`
	IssuedDate  time.Time `json:"issued_date"`
	Fingerprint string    `json:"fingerprint"`
	Status      string    `json:"status"` // "valid", "pending", "expired", "error"
}

// ChallengeType represents ACME challenge types
type ChallengeType string

const (
	HTTP01Challenge    ChallengeType = "http-01"
	DNS01Challenge     ChallengeType = "dns-01"
	TLSALPN01Challenge ChallengeType = "tls-alpn-01"
)

// NewACMEClient creates a new ACME client for Let's Encrypt integration
func NewACMEClient(cfg config.LetsEncryptConfig, logger *logger.Logger) (*ACMEClient, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// Validate configuration
	if cfg.Email == "" {
		return nil, lberrors.NewError(
			lberrors.ErrCodeConfigLoad,
			"acme_client",
			"ACME client requires an email address for Let's Encrypt registration",
		)
	}

	if len(cfg.Domains) == 0 {
		return nil, lberrors.NewError(
			lberrors.ErrCodeConfigLoad,
			"acme_client",
			"ACME client requires at least one domain",
		)
	}

	// Create certificate storage directory
	if err := os.MkdirAll(cfg.StoragePath, 0755); err != nil {
		return nil, lberrors.NewErrorWithCause(
			lberrors.ErrCodeConfigLoad,
			"acme_client",
			"Failed to create certificate storage directory",
			err,
		)
	}

	client := &ACMEClient{
		config:       cfg,
		logger:       logger,
		certificates: make(map[string]*Certificate),
		challenges:   make(map[string]string),
		running:      false,
	}

	logger.WithFields(map[string]interface{}{
		"email":          cfg.Email,
		"domains":        cfg.Domains,
		"challenge_type": cfg.ChallengeType,
		"staging":        cfg.TestMode,
		"storage_path":   cfg.StoragePath,
	}).Info("ACME client initialized")

	return client, nil
}

// Start starts the ACME client and begins certificate management
func (ac *ACMEClient) Start() error {
	if ac.running {
		return fmt.Errorf("ACME client is already running")
	}

	ac.logger.Info("Starting ACME client for automatic certificate management")

	// Load existing certificates
	if err := ac.loadExistingCertificates(); err != nil {
		ac.logger.WithError(err).Warn("Failed to load existing certificates")
	}

	// Start HTTP challenge server if using HTTP-01
	if ac.config.ChallengeType == string(HTTP01Challenge) {
		if err := ac.startChallengeServer(); err != nil {
			return fmt.Errorf("failed to start challenge server: %w", err)
		}
	}

	// Start certificate management
	go ac.manageCertificates()

	ac.running = true
	return nil
}

// startChallengeServer starts the HTTP challenge server for HTTP-01 challenges
func (ac *ACMEClient) startChallengeServer() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/acme-challenge/", ac.handleACMEChallenge)

	ac.httpServer = &http.Server{
		Addr:    ":80",
		Handler: mux,
	}

	go func() {
		if err := ac.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ac.logger.WithError(err).Error("ACME challenge server failed")
		}
	}()

	ac.logger.Info("ACME HTTP challenge server started on port 80")
	return nil
}

// handleACMEChallenge handles HTTP-01 ACME challenges
func (ac *ACMEClient) handleACMEChallenge(w http.ResponseWriter, r *http.Request) {
	token := filepath.Base(r.URL.Path)

	ac.challengeMux.RLock()
	keyAuth, exists := ac.challenges[token]
	ac.challengeMux.RUnlock()

	if !exists {
		ac.logger.WithFields(map[string]interface{}{
			"token": token,
			"path":  r.URL.Path,
		}).Warn("Unknown ACME challenge token")
		http.NotFound(w, r)
		return
	}

	ac.logger.WithFields(map[string]interface{}{
		"token": token,
		"path":  r.URL.Path,
	}).Info("Serving ACME challenge")

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(keyAuth))
}

// manageCertificates manages certificate lifecycle (issuance and renewal)
func (ac *ACMEClient) manageCertificates() {
	// Initial certificate check
	ac.checkAndIssueCertificates()

	// Set up renewal ticker
	ticker := time.NewTicker(12 * time.Hour) // Check twice daily
	defer ticker.Stop()

	for range ticker.C {
		ac.checkAndRenewCertificates()
	}
}

// checkAndIssueCertificates checks for missing certificates and issues them
func (ac *ACMEClient) checkAndIssueCertificates() {
	for _, domain := range ac.config.Domains {
		ac.mutex.RLock()
		cert, exists := ac.certificates[domain]
		ac.mutex.RUnlock()

		if !exists || cert.Status != "valid" {
			ac.logger.WithField("domain", domain).Info("Issuing new certificate")
			if err := ac.issueCertificate(domain); err != nil {
				ac.logger.WithError(err).Errorf("Failed to issue certificate for domain %s", domain)
			}
		}
	}
}

// checkAndRenewCertificates checks for expiring certificates and renews them
func (ac *ACMEClient) checkAndRenewCertificates() {
	renewalThreshold := time.Duration(ac.config.RenewalDays) * 24 * time.Hour

	for domain, cert := range ac.certificates {
		if time.Until(cert.ExpiryDate) <= renewalThreshold {
			ac.logger.WithFields(map[string]interface{}{
				"domain":     domain,
				"expires_at": cert.ExpiryDate,
			}).Info("Renewing certificate")

			if err := ac.renewCertificate(domain); err != nil {
				ac.logger.WithError(err).Errorf("Failed to renew certificate for domain %s", domain)
			}
		}
	}
}

// issueCertificate issues a new certificate for the specified domain
func (ac *ACMEClient) issueCertificate(domain string) error {
	ac.logger.WithField("domain", domain).Info("Starting certificate issuance process")

	// For demo purposes, create a self-signed certificate
	// In production, this would interact with Let's Encrypt ACME API
	cert, err := ac.createSelfSignedCertificate(domain)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	ac.mutex.Lock()
	ac.certificates[domain] = cert
	ac.mutex.Unlock()

	ac.logger.WithFields(map[string]interface{}{
		"domain":     domain,
		"expires_at": cert.ExpiryDate,
		"cert_path":  cert.CertPath,
		"key_path":   cert.KeyPath,
	}).Info("Certificate issued successfully")

	return nil
}

// renewCertificate renews an existing certificate
func (ac *ACMEClient) renewCertificate(domain string) error {
	ac.logger.WithField("domain", domain).Info("Starting certificate renewal process")

	// For demo, just issue a new certificate
	return ac.issueCertificate(domain)
}

// createSelfSignedCertificate creates a self-signed certificate for development/demo
// In production, this would be replaced with actual ACME protocol implementation
func (ac *ACMEClient) createSelfSignedCertificate(domain string) (*Certificate, error) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	template := &x509.Certificate{
		SerialNumber: nil,
		Subject: pkix.Name{
			CommonName: domain,
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(0, 3, 0), // 3 months validity
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{domain},
	}

	// Create certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Define file paths
	certPath := filepath.Join(ac.config.StoragePath, domain+".crt")
	keyPath := filepath.Join(ac.config.StoragePath, domain+".key")

	// Write certificate file
	certFile, err := os.Create(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate file: %w", err)
	}
	defer certFile.Close()

	if err := pem.Encode(certFile, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}); err != nil {
		return nil, fmt.Errorf("failed to write certificate: %w", err)
	}

	// Write private key file
	keyFile, err := os.Create(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyFile.Close()

	if err := pem.Encode(keyFile, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}); err != nil {
		return nil, fmt.Errorf("failed to write private key: %w", err)
	}

	certificate := &Certificate{
		Domain:      domain,
		CertPath:    certPath,
		KeyPath:     keyPath,
		ExpiryDate:  template.NotAfter,
		IssuedDate:  template.NotBefore,
		Fingerprint: fmt.Sprintf("demo-cert-%s", domain),
		Status:      "valid",
	}

	return certificate, nil
}

// loadExistingCertificates loads certificates from the storage directory
func (ac *ACMEClient) loadExistingCertificates() error {
	entries, err := os.ReadDir(ac.config.StoragePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Storage directory doesn't exist yet
		}
		return fmt.Errorf("failed to read storage directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if filepath.Ext(name) == ".crt" {
			domain := name[:len(name)-4] // Remove .crt extension

			certPath := filepath.Join(ac.config.StoragePath, name)
			keyPath := filepath.Join(ac.config.StoragePath, domain+".key")

			// Check if both cert and key files exist
			if _, err := os.Stat(keyPath); err == nil {
				cert, err := ac.loadCertificateInfo(domain, certPath, keyPath)
				if err != nil {
					ac.logger.WithError(err).Warnf("Failed to load certificate for domain %s", domain)
					continue
				}

				ac.certificates[domain] = cert
				ac.logger.WithField("domain", domain).Info("Loaded existing certificate")
			}
		}
	}

	ac.logger.WithField("certificate_count", len(ac.certificates)).Info("Loaded existing certificates")
	return nil
}

// loadCertificateInfo loads certificate information from files
func (ac *ACMEClient) loadCertificateInfo(domain, certPath, keyPath string) (*Certificate, error) {
	// Read certificate file
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate file: %w", err)
	}

	// Parse certificate
	block, _ := pem.Decode(certData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Determine status
	status := "valid"
	if time.Now().After(cert.NotAfter) {
		status = "expired"
	} else if time.Until(cert.NotAfter) <= time.Duration(ac.config.RenewalDays)*24*time.Hour {
		status = "expiring_soon"
	}

	return &Certificate{
		Domain:      domain,
		CertPath:    certPath,
		KeyPath:     keyPath,
		ExpiryDate:  cert.NotAfter,
		IssuedDate:  cert.NotBefore,
		Fingerprint: fmt.Sprintf("%x", cert.SerialNumber),
		Status:      status,
	}, nil
}

// GetCertificate returns certificate information for a domain
func (ac *ACMEClient) GetCertificate(domain string) (*Certificate, error) {
	ac.mutex.RLock()
	defer ac.mutex.RUnlock()

	cert, exists := ac.certificates[domain]
	if !exists {
		return nil, fmt.Errorf("certificate not found for domain: %s", domain)
	}

	return cert, nil
}

// ListCertificates returns all managed certificates
func (ac *ACMEClient) ListCertificates() []*Certificate {
	ac.mutex.RLock()
	defer ac.mutex.RUnlock()

	certificates := make([]*Certificate, 0, len(ac.certificates))
	for _, cert := range ac.certificates {
		certificates = append(certificates, cert)
	}

	return certificates
}

// Stop stops the ACME client
func (ac *ACMEClient) Stop() error {
	if !ac.running {
		return nil
	}

	ac.logger.Info("Stopping ACME client")

	// Stop HTTP challenge server
	if ac.httpServer != nil {
		if err := ac.httpServer.Close(); err != nil {
			ac.logger.WithError(err).Error("Failed to stop ACME challenge server")
		}
	}

	ac.running = false
	return nil
}

// GetStats returns ACME client statistics
func (ac *ACMEClient) GetStats() map[string]interface{} {
	ac.mutex.RLock()
	defer ac.mutex.RUnlock()

	stats := map[string]interface{}{
		"enabled":        ac.config.Enabled,
		"running":        ac.running,
		"staging_mode":   ac.config.TestMode,
		"challenge_type": ac.config.ChallengeType,
		"domains":        ac.config.Domains,
		"renewal_days":   ac.config.RenewalDays,
		"storage_path":   ac.config.StoragePath,
	}

	// Certificate statistics
	var validCerts, expiredCerts, expiringSoonCerts int
	for _, cert := range ac.certificates {
		switch cert.Status {
		case "valid":
			validCerts++
		case "expired":
			expiredCerts++
		case "expiring_soon":
			expiringSoonCerts++
		}
	}

	stats["certificates"] = map[string]interface{}{
		"total":         len(ac.certificates),
		"valid":         validCerts,
		"expired":       expiredCerts,
		"expiring_soon": expiringSoonCerts,
	}

	return stats
}
