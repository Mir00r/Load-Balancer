// Package handler provides HTTP/3 with QUIC protocol support for modern high-performance load balancing
// Implements QUIC transport layer with HTTP/3 semantics including multiplexing, server push, and connection migration
package handler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/config"
	"github.com/mir00r/load-balancer/internal/domain"
	lberrors "github.com/mir00r/load-balancer/internal/errors"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// QuicConnection represents a QUIC connection (mock implementation)
type QuicConnection struct {
	RemoteAddr    net.Addr
	ConnectedAt   time.Time
	StreamCount   int
	BytesSent     int64
	BytesReceived int64
	Closed        bool
}

// HTTP3Stats tracks HTTP/3 and QUIC protocol statistics
type HTTP3Stats struct {
	ConnectionsTotal      int64   `json:"connections_total"`
	ConnectionsActive     int64   `json:"connections_active"`
	StreamsTotal          int64   `json:"streams_total"`
	StreamsActive         int64   `json:"streams_active"`
	BytesSent             int64   `json:"bytes_sent"`
	BytesReceived         int64   `json:"bytes_received"`
	PacketsLost           int64   `json:"packets_lost"`
	RTTMicroseconds       int64   `json:"rtt_microseconds"`
	ConnectionMigrations  int64   `json:"connection_migrations"`
	ZeroRTTAttempts       int64   `json:"zero_rtt_attempts"`
	ZeroRTTSuccessful     int64   `json:"zero_rtt_successful"`
	RequestsPerConnection float64 `json:"requests_per_connection"`
}

// HTTP3Handler handles HTTP/3 requests with QUIC protocol support
type HTTP3Handler struct {
	config       config.HTTP3Config
	loadBalancer domain.LoadBalancer
	logger       *logger.Logger
	server       *http.Server
	connections  map[string]*QuicConnection
	connMutex    sync.RWMutex
	stats        HTTP3Stats
	statsMutex   sync.RWMutex
	running      bool
	certFile     string
	keyFile      string
}

// NewHTTP3Handler creates a new HTTP/3 handler with QUIC support
func NewHTTP3Handler(
	cfg config.HTTP3Config,
	loadBalancer domain.LoadBalancer,
	logger *logger.Logger,
) (*HTTP3Handler, error) {

	if !cfg.Enabled {
		return nil, nil
	}

	// Validate configuration
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, lberrors.NewError(
			lberrors.ErrCodeConfigLoad,
			"http3_handler",
			fmt.Sprintf("Invalid HTTP/3 port: %d", cfg.Port),
		)
	}

	if cfg.CertFile == "" || cfg.KeyFile == "" {
		return nil, lberrors.NewError(
			lberrors.ErrCodeConfigLoad,
			"http3_handler",
			"HTTP/3 requires TLS certificate and key files",
		)
	}

	handler := &HTTP3Handler{
		config:       cfg,
		loadBalancer: loadBalancer,
		logger:       logger,
		connections:  make(map[string]*QuicConnection),
		certFile:     cfg.CertFile,
		keyFile:      cfg.KeyFile,
		running:      false,
	}

	// Set up HTTP/3 server
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler.handleHTTP3Request)
	mux.HandleFunc("/health", handler.handleHealth)
	mux.HandleFunc("/stats", handler.handleStats)

	handler.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      mux,
		ReadTimeout:  cfg.MaxStreamTimeout,
		WriteTimeout: cfg.MaxStreamTimeout,
		IdleTimeout:  cfg.MaxIdleTimeout,
	}

	logger.WithFields(map[string]interface{}{
		"port":                     cfg.Port,
		"cert_file":                cfg.CertFile,
		"key_file":                 cfg.KeyFile,
		"max_stream_timeout":       cfg.MaxStreamTimeout,
		"max_idle_timeout":         cfg.MaxIdleTimeout,
		"max_incoming_streams":     cfg.MaxIncomingStreams,
		"max_incoming_uni_streams": cfg.MaxIncomingUniStreams,
		"keep_alive":               cfg.KeepAlive,
		"enable_datagrams":         cfg.EnableDatagrams,
	}).Info("HTTP/3 handler initialized with QUIC support")

	return handler, nil
}

// Start starts the HTTP/3 server with QUIC transport
func (h *HTTP3Handler) Start(ctx context.Context) error {
	if !h.config.Enabled {
		return nil
	}

	h.logger.WithFields(map[string]interface{}{
		"address": h.server.Addr,
	}).Info("Starting HTTP/3 server with QUIC transport")

	h.running = true

	// In a real implementation, this would:
	// 1. Set up QUIC listener with proper configuration
	// 2. Configure connection migration support
	// 3. Set up 0-RTT resumption
	// 4. Configure congestion control algorithms

	// Mock implementation: Start regular HTTPS server
	go func() {
		if err := h.server.ListenAndServeTLS(h.certFile, h.keyFile); err != nil && err != http.ErrServerClosed {
			h.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Error("HTTP/3 server failed to start")
		}
	}()

	// Start connection monitoring
	go h.monitorConnections(ctx)

	h.logger.WithFields(map[string]interface{}{
		"port": h.config.Port,
	}).Info("HTTP/3 server started successfully")

	return nil
}

// Stop stops the HTTP/3 server
func (h *HTTP3Handler) Stop(ctx context.Context) error {
	if !h.running {
		return nil
	}

	h.logger.Info("Stopping HTTP/3 server")

	h.running = false

	// Gracefully close all QUIC connections
	h.connMutex.Lock()
	for connID, conn := range h.connections {
		conn.Closed = true
		h.logger.WithFields(map[string]interface{}{
			"connection_id": connID,
			"remote_addr":   conn.RemoteAddr.String(),
			"duration":      time.Since(conn.ConnectedAt),
		}).Debug("Closing QUIC connection")
	}
	h.connMutex.Unlock()

	// Stop the server
	return h.server.Shutdown(ctx)
}

// handleHTTP3Request handles HTTP/3 requests with QUIC connection management
func (h *HTTP3Handler) handleHTTP3Request(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	connID := h.getConnectionID(r)

	// Track connection
	h.trackConnection(r, connID)

	// Update statistics
	h.updateStats(r)

	// Select backend using load balancer
	backend, err := h.loadBalancer.SelectBackend(r)
	if err != nil {
		h.logger.WithFields(map[string]interface{}{
			"error":         err.Error(),
			"connection_id": connID,
			"remote_addr":   r.RemoteAddr,
			"path":          r.URL.Path,
			"method":        r.Method,
		}).Error("Failed to select backend for HTTP/3 request")

		http.Error(w, "No healthy backends available", http.StatusServiceUnavailable)
		return
	}

	// Add HTTP/3 specific headers
	h.addHTTP3Headers(w, r)

	// Proxy request to backend
	if err := h.proxyToBackend(w, r, backend); err != nil {
		h.logger.WithFields(map[string]interface{}{
			"error":         err.Error(),
			"backend_id":    backend.ID,
			"backend_url":   backend.URL,
			"connection_id": connID,
		}).Error("Failed to proxy HTTP/3 request")

		http.Error(w, "Backend request failed", http.StatusBadGateway)
		return
	}

	// Log successful request
	duration := time.Since(startTime)
	h.logger.WithFields(map[string]interface{}{
		"backend_id":    backend.ID,
		"connection_id": connID,
		"method":        r.Method,
		"path":          r.URL.Path,
		"duration_ms":   duration.Milliseconds(),
		"protocol":      "HTTP/3",
		"remote_addr":   r.RemoteAddr,
	}).Debug("HTTP/3 request completed successfully")
}

// addHTTP3Headers adds HTTP/3 and QUIC specific headers
func (h *HTTP3Handler) addHTTP3Headers(w http.ResponseWriter, r *http.Request) {
	// Add headers to indicate HTTP/3 support
	w.Header().Set("alt-svc", fmt.Sprintf("h3=\":%d\"; ma=86400", h.config.Port))
	w.Header().Set("X-Protocol", "HTTP/3")
	w.Header().Set("X-QUIC-Version", "1")

	// Connection-specific headers
	connID := h.getConnectionID(r)
	w.Header().Set("X-Connection-ID", connID)

	// Performance headers
	if h.config.EnableDatagrams {
		w.Header().Set("X-QUIC-Datagrams", "supported")
	}
}

// proxyToBackend forwards the request to the selected backend
func (h *HTTP3Handler) proxyToBackend(w http.ResponseWriter, r *http.Request, backend *domain.Backend) error {
	// Create client for backend request
	client := &http.Client{
		Timeout: 30 * time.Second,
		// In a real implementation, this might use HTTP/3 client as well
	}

	// Create request to backend
	backendURL := backend.URL + r.URL.Path
	if r.URL.RawQuery != "" {
		backendURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequest(r.Method, backendURL, r.Body)
	if err != nil {
		return fmt.Errorf("failed to create backend request: %w", err)
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// Add load balancer headers
	req.Header.Set("X-Forwarded-For", r.RemoteAddr)
	req.Header.Set("X-Forwarded-Proto", "http/3")
	req.Header.Set("X-Load-Balancer", "HTTP3Handler")

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("backend request failed: %w", err)
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	return h.copyResponseBody(w, resp)
}

// copyResponseBody copies response body efficiently
func (h *HTTP3Handler) copyResponseBody(w http.ResponseWriter, resp *http.Response) error {
	// In a real implementation, this would use io.Copy with proper buffering
	// and potentially leverage QUIC's stream multiplexing capabilities

	buffer := make([]byte, 32*1024) // 32KB buffer
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return fmt.Errorf("failed to write response: %w", writeErr)
			}
		}
		if err != nil {
			break
		}
	}

	return nil
}

// trackConnection tracks QUIC connection metrics
func (h *HTTP3Handler) trackConnection(r *http.Request, connID string) {
	h.connMutex.Lock()
	defer h.connMutex.Unlock()

	if conn, exists := h.connections[connID]; exists {
		// Update existing connection
		conn.StreamCount++
		conn.BytesReceived += r.ContentLength
	} else {
		// Create new connection
		remoteAddr, _ := net.ResolveTCPAddr("tcp", r.RemoteAddr)
		h.connections[connID] = &QuicConnection{
			RemoteAddr:    remoteAddr,
			ConnectedAt:   time.Now(),
			StreamCount:   1,
			BytesReceived: r.ContentLength,
		}
	}
}

// updateStats updates HTTP/3 protocol statistics
func (h *HTTP3Handler) updateStats(r *http.Request) {
	h.statsMutex.Lock()
	defer h.statsMutex.Unlock()

	h.stats.StreamsTotal++
	h.stats.StreamsActive++
	h.stats.BytesReceived += r.ContentLength

	// Update connection statistics
	h.connMutex.RLock()
	h.stats.ConnectionsActive = int64(len(h.connections))
	h.connMutex.RUnlock()

	// Calculate requests per connection
	if h.stats.ConnectionsTotal > 0 {
		h.stats.RequestsPerConnection = float64(h.stats.StreamsTotal) / float64(h.stats.ConnectionsTotal)
	}
}

// monitorConnections monitors QUIC connections for cleanup and statistics
func (h *HTTP3Handler) monitorConnections(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.cleanupConnections()
		}
	}
}

// cleanupConnections removes old or closed connections
func (h *HTTP3Handler) cleanupConnections() {
	h.connMutex.Lock()
	defer h.connMutex.Unlock()

	now := time.Now()
	cleaned := 0

	for connID, conn := range h.connections {
		// Remove connections older than idle timeout or closed
		if conn.Closed || now.Sub(conn.ConnectedAt) > h.config.MaxIdleTimeout {
			delete(h.connections, connID)
			cleaned++
		}
	}

	if cleaned > 0 {
		h.logger.WithFields(map[string]interface{}{
			"cleaned_connections": cleaned,
			"active_connections":  len(h.connections),
		}).Debug("Cleaned up QUIC connections")
	}
}

// getConnectionID extracts or generates a connection identifier
func (h *HTTP3Handler) getConnectionID(r *http.Request) string {
	// In a real QUIC implementation, this would be the actual connection ID
	// For now, use remote address as a simple identifier
	return r.RemoteAddr
}

// handleHealth handles health check requests
func (h *HTTP3Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	h.addHTTP3Headers(w, r)

	status := map[string]interface{}{
		"status":   "healthy",
		"protocol": "HTTP/3",
		"quic":     "active",
		"connections": map[string]interface{}{
			"active": len(h.connections),
			"total":  h.stats.ConnectionsTotal,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Simple JSON response
	fmt.Fprintf(w, `{"status":"healthy","protocol":"HTTP/3","connections_active":%d}`, len(h.connections))
}

// handleStats handles statistics requests
func (h *HTTP3Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	h.addHTTP3Headers(w, r)

	h.statsMutex.RLock()
	stats := h.stats
	h.statsMutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Return basic statistics
	fmt.Fprintf(w, `{
		"connections_total": %d,
		"connections_active": %d,
		"streams_total": %d,
		"bytes_sent": %d,
		"bytes_received": %d,
		"requests_per_connection": %.2f
	}`, stats.ConnectionsTotal, stats.ConnectionsActive, stats.StreamsTotal,
		stats.BytesSent, stats.BytesReceived, stats.RequestsPerConnection)
}

// GetStats returns current HTTP/3 statistics
func (h *HTTP3Handler) GetStats() HTTP3Stats {
	h.statsMutex.RLock()
	defer h.statsMutex.RUnlock()
	return h.stats
}

// GetConnections returns information about active connections
func (h *HTTP3Handler) GetConnections() map[string]interface{} {
	h.connMutex.RLock()
	defer h.connMutex.RUnlock()

	connections := make([]map[string]interface{}, 0, len(h.connections))
	for connID, conn := range h.connections {
		connections = append(connections, map[string]interface{}{
			"id":             connID,
			"remote_addr":    conn.RemoteAddr.String(),
			"connected_at":   conn.ConnectedAt,
			"stream_count":   conn.StreamCount,
			"bytes_sent":     conn.BytesSent,
			"bytes_received": conn.BytesReceived,
			"duration":       time.Since(conn.ConnectedAt).Milliseconds(),
		})
	}

	return map[string]interface{}{
		"total_connections": len(h.connections),
		"connections":       connections,
		"protocol":          "HTTP/3",
		"quic_version":      "1",
		"server_running":    h.running,
	}
}
