package handler

import (
	"bufio"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// WebSocketProxyHandler handles WebSocket connections with load balancing
type WebSocketProxyHandler struct {
	loadBalancer domain.LoadBalancer
	metrics      domain.Metrics
	logger       *logger.Logger
	timeout      time.Duration
}

// NewWebSocketProxyHandler creates a new WebSocket proxy handler
func NewWebSocketProxyHandler(lb domain.LoadBalancer, metrics domain.Metrics, logger *logger.Logger) *WebSocketProxyHandler {
	return &WebSocketProxyHandler{
		loadBalancer: lb,
		metrics:      metrics,
		logger:       logger,
		timeout:      30 * time.Second,
	}
}

// ServeHTTP handles WebSocket proxy requests
func (wph *WebSocketProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if this is a WebSocket upgrade request
	if !wph.isWebSocketUpgrade(r) {
		http.Error(w, "Bad Request: Not a WebSocket upgrade", http.StatusBadRequest)
		return
	}

	// Select backend
	backend := wph.loadBalancer.SelectBackend()
	if backend == nil {
		wph.logger.WithFields(map[string]interface{}{
			"error": "No available backend",
		}).Error("Failed to select backend for WebSocket")
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	// Parse backend URL
	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		wph.logger.WithFields(map[string]interface{}{
			"error":   err.Error(),
			"backend": backend.URL,
		}).Error("Invalid backend URL")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Create connection to backend
	backendConn, err := wph.dialBackend(backendURL, r)
	if err != nil {
		wph.logger.WithFields(map[string]interface{}{
			"error":   err.Error(),
			"backend": backend.URL,
		}).Error("Failed to connect to backend")
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer backendConn.Close()

	// Send the upgrade request to backend
	err = r.Write(backendConn)
	if err != nil {
		wph.logger.WithFields(map[string]interface{}{
			"error":   err.Error(),
			"backend": backend.URL,
		}).Error("Failed to forward request to backend")
		return
	}

	// Read response from backend
	backendReader := bufio.NewReader(backendConn)
	resp, err := http.ReadResponse(backendReader, r)
	if err != nil {
		wph.logger.WithFields(map[string]interface{}{
			"error":   err.Error(),
			"backend": backend.URL,
		}).Error("Failed to read response from backend")
		return
	}

	// Check if backend accepted the WebSocket upgrade
	if resp.StatusCode != http.StatusSwitchingProtocols {
		wph.logger.WithFields(map[string]interface{}{
			"status_code": resp.StatusCode,
			"backend":     backend.URL,
		}).Warn("Backend rejected WebSocket upgrade")

		// Forward the error response
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		return
	}

	// Hijack the client connection
	clientConn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		wph.logger.WithFields(map[string]interface{}{
			"error": err.Error(),
		}).Error("Failed to hijack client connection")
		return
	}
	defer clientConn.Close()

	// Send the successful upgrade response to client
	err = resp.Write(clientConn)
	if err != nil {
		wph.logger.WithFields(map[string]interface{}{
			"error": err.Error(),
		}).Error("Failed to send upgrade response to client")
		return
	}

	// Start proxying data between client and backend
	wph.logger.WithFields(map[string]interface{}{
		"backend": backend.URL,
		"client":  r.RemoteAddr,
	}).Info("WebSocket connection established")

	// Update metrics
	backend.IncrementConnections()
	wph.metrics.IncrementRequests(backend.ID)
	defer func() {
		backend.DecrementConnections()
	}()

	// Proxy data bidirectionally
	wph.proxyData(clientConn, backendConn, backend.ID)
}

// isWebSocketUpgrade checks if the request is a WebSocket upgrade
func (wph *WebSocketProxyHandler) isWebSocketUpgrade(r *http.Request) bool {
	return strings.ToLower(r.Header.Get("Connection")) == "upgrade" &&
		strings.ToLower(r.Header.Get("Upgrade")) == "websocket"
}

// dialBackend creates a connection to the backend server
func (wph *WebSocketProxyHandler) dialBackend(backendURL *url.URL, r *http.Request) (net.Conn, error) {
	address := backendURL.Host
	if backendURL.Port() == "" {
		if backendURL.Scheme == "wss" || backendURL.Scheme == "https" {
			address += ":443"
		} else {
			address += ":80"
		}
	}

	// Set up dialer with timeout
	dialer := &net.Dialer{
		Timeout: wph.timeout,
	}

	// Handle TLS connections
	if backendURL.Scheme == "wss" || backendURL.Scheme == "https" {
		tlsConfig := &tls.Config{
			ServerName: backendURL.Hostname(),
		}
		return tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	}

	return dialer.Dial("tcp", address)
}

// proxyData handles bidirectional data proxying between client and backend
func (wph *WebSocketProxyHandler) proxyData(clientConn, backendConn net.Conn, backendID string) {
	// Channel to signal when either direction is done
	done := make(chan struct{}, 2)

	// Proxy client -> backend
	go func() {
		defer func() { done <- struct{}{} }()
		wph.copyData(clientConn, backendConn, "client->backend", backendID)
	}()

	// Proxy backend -> client
	go func() {
		defer func() { done <- struct{}{} }()
		wph.copyData(backendConn, clientConn, "backend->client", backendID)
	}()

	// Wait for either direction to complete
	<-done

	wph.logger.WithFields(map[string]interface{}{
		"backend_id": backendID,
	}).Info("WebSocket connection closed")
}

// copyData copies data from src to dst with logging
func (wph *WebSocketProxyHandler) copyData(dst, src net.Conn, direction, backendID string) {
	buffer := make([]byte, 32*1024) // 32KB buffer
	totalBytes := int64(0)

	for {
		// Set read timeout
		src.SetReadDeadline(time.Now().Add(wph.timeout))

		n, err := src.Read(buffer)
		if n > 0 {
			totalBytes += int64(n)

			// Set write timeout
			dst.SetWriteDeadline(time.Now().Add(wph.timeout))
			_, writeErr := dst.Write(buffer[:n])
			if writeErr != nil {
				wph.logger.WithFields(map[string]interface{}{
					"error":             writeErr.Error(),
					"direction":         direction,
					"backend_id":        backendID,
					"bytes_transferred": totalBytes,
				}).Error("WebSocket write error")
				return
			}
		}

		if err != nil {
			wph.logger.WithFields(map[string]interface{}{
				"error":             err.Error(),
				"direction":         direction,
				"backend_id":        backendID,
				"bytes_transferred": totalBytes,
			}).Debug("WebSocket connection ended")
			return
		}
	}
}

// TCPProxyHandler handles raw TCP proxying for Layer 4 load balancing
type TCPProxyHandler struct {
	loadBalancer domain.LoadBalancer
	metrics      domain.Metrics
	logger       *logger.Logger
	timeout      time.Duration
}

// NewTCPProxyHandler creates a new TCP proxy handler
func NewTCPProxyHandler(lb domain.LoadBalancer, metrics domain.Metrics, logger *logger.Logger) *TCPProxyHandler {
	return &TCPProxyHandler{
		loadBalancer: lb,
		metrics:      metrics,
		logger:       logger,
		timeout:      30 * time.Second,
	}
}

// HandleConnection handles a TCP connection
func (tph *TCPProxyHandler) HandleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	// Select backend
	backend := tph.loadBalancer.SelectBackend()
	if backend == nil {
		tph.logger.WithFields(map[string]interface{}{
			"error":  "No available backend",
			"client": clientConn.RemoteAddr(),
		}).Error("Failed to select backend for TCP connection")
		return
	}

	// Parse backend URL
	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		tph.logger.WithFields(map[string]interface{}{
			"error":   err.Error(),
			"backend": backend.URL,
		}).Error("Invalid backend URL for TCP")
		return
	}

	// Connect to backend
	backendConn, err := net.DialTimeout("tcp", backendURL.Host, tph.timeout)
	if err != nil {
		tph.logger.WithFields(map[string]interface{}{
			"error":   err.Error(),
			"backend": backend.URL,
			"client":  clientConn.RemoteAddr(),
		}).Error("Failed to connect to TCP backend")
		return
	}
	defer backendConn.Close()

	tph.logger.WithFields(map[string]interface{}{
		"backend": backend.URL,
		"client":  clientConn.RemoteAddr(),
	}).Info("TCP connection established")

	// Update metrics
	backend.IncrementConnections()
	tph.metrics.IncrementRequests(backend.ID)
	defer func() {
		backend.DecrementConnections()
	}()

	// Proxy data bidirectionally
	tph.proxyTCPData(clientConn, backendConn, backend.ID)
}

// proxyTCPData handles bidirectional TCP data proxying
func (tph *TCPProxyHandler) proxyTCPData(clientConn, backendConn net.Conn, backendID string) {
	done := make(chan struct{}, 2)

	// Client -> Backend
	go func() {
		defer func() { done <- struct{}{} }()
		tph.copyTCPData(clientConn, backendConn, "client->backend", backendID)
	}()

	// Backend -> Client
	go func() {
		defer func() { done <- struct{}{} }()
		tph.copyTCPData(backendConn, clientConn, "backend->client", backendID)
	}()

	// Wait for either direction to complete
	<-done

	tph.logger.WithFields(map[string]interface{}{
		"backend_id": backendID,
	}).Info("TCP connection closed")
}

// copyTCPData copies TCP data from src to dst
func (tph *TCPProxyHandler) copyTCPData(dst, src net.Conn, direction, backendID string) {
	buffer := make([]byte, 32*1024)
	totalBytes := int64(0)

	for {
		src.SetReadDeadline(time.Now().Add(tph.timeout))
		n, err := src.Read(buffer)
		if n > 0 {
			totalBytes += int64(n)
			dst.SetWriteDeadline(time.Now().Add(tph.timeout))
			_, writeErr := dst.Write(buffer[:n])
			if writeErr != nil {
				tph.logger.WithFields(map[string]interface{}{
					"error":             writeErr.Error(),
					"direction":         direction,
					"backend_id":        backendID,
					"bytes_transferred": totalBytes,
				}).Error("TCP write error")
				return
			}
		}

		if err != nil {
			tph.logger.WithFields(map[string]interface{}{
				"direction":         direction,
				"backend_id":        backendID,
				"bytes_transferred": totalBytes,
			}).Debug("TCP connection ended")
			return
		}
	}
}
