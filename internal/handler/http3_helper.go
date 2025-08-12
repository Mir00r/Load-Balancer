// Package handler provides helper methods for HTTP/3 handler operations.
// These methods support connection management, request processing, and monitoring.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
)

// getStatus returns the current status of the HTTP/3 handler.
func (h *HTTP3Handler) getStatus() string {
	if h.IsEnabled() {
		return "running"
	}
	return "stopped"
}

// resetStats resets all statistics to initial values.
func (h *HTTP3Handler) resetStats() {
	h.statsMutex.Lock()
	defer h.statsMutex.Unlock()

	h.stats = &HTTP3Stats{
		LastStartTime:      time.Now(),
		DatagramsSupported: h.enableDatagrams,
	}
}

// connectionManager runs background connection management tasks.
// This includes connection cleanup, health monitoring, and statistics collection.
func (h *HTTP3Handler) connectionManager(ctx context.Context) {
	ticker := time.NewTicker(h.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Debug("Connection manager stopping due to context cancellation")
			return
		case <-h.stopChan:
			h.logger.Debug("Connection manager stopping due to stop signal")
			return
		case <-ticker.C:
			h.cleanupStaleConnections()
		}
	}
}

// cleanupStaleConnections removes connections that have been idle too long.
func (h *HTTP3Handler) cleanupStaleConnections() {
	h.connMutex.Lock()
	defer h.connMutex.Unlock()

	now := time.Now()
	staleConnections := make([]string, 0)

	for id, conn := range h.connections {
		if now.Sub(conn.LastActivity) > h.maxIdleTimeout {
			staleConnections = append(staleConnections, id)
		}
	}

	// Remove stale connections
	for _, id := range staleConnections {
		delete(h.connections, id)
		h.updateConnectionStats(-1, 0, 1)

		h.logger.WithFields(map[string]interface{}{
			"component":     "http3_handler",
			"connection_id": id,
			"idle_time":     now.Sub(h.connections[id].LastActivity).String(),
		}).Debug("Cleaned up stale HTTP/3 connection")
	}

	if len(staleConnections) > 0 {
		h.logger.WithFields(map[string]interface{}{
			"component":           "http3_handler",
			"cleaned_connections": len(staleConnections),
			"active_connections":  len(h.connections),
		}).Info("Cleaned up stale HTTP/3 connections")
	}
}

// statisticsCollector runs background statistics collection tasks.
func (h *HTTP3Handler) statisticsCollector(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopChan:
			return
		case <-ticker.C:
			h.updateDerivedStatistics()
		}
	}
}

// updateDerivedStatistics calculates and updates derived statistical values.
func (h *HTTP3Handler) updateDerivedStatistics() {
	h.statsMutex.Lock()
	defer h.statsMutex.Unlock()

	// Update uptime
	h.stats.Uptime = time.Since(h.stats.LastStartTime).String()

	// Calculate requests per connection
	if h.stats.ConnectionsTotal > 0 {
		h.stats.RequestsPerConnection = float64(h.stats.RequestsTotal) / float64(h.stats.ConnectionsTotal)
	}

	// Calculate average bandwidth (simplified calculation)
	uptime := time.Since(h.stats.LastStartTime)
	if uptime.Seconds() > 0 {
		totalBytes := float64(h.stats.BytesSent + h.stats.BytesReceived)
		h.stats.AverageBandwidth = (totalBytes * 8) / (uptime.Seconds() * 1000000) // Mbps
	}
}

// closeAllConnections gracefully closes all active QUIC connections.
func (h *HTTP3Handler) closeAllConnections() {
	h.connMutex.Lock()
	defer h.connMutex.Unlock()

	connectionCount := len(h.connections)
	if connectionCount == 0 {
		return
	}

	// Mark all connections as closed
	for _, conn := range h.connections {
		conn.State = "closed"
		conn.Closed = true
	}

	// Clear the connections map
	h.connections = make(map[string]*QuicConnection)

	// Update statistics
	h.updateConnectionStats(-int64(connectionCount), 0, int64(connectionCount))

	h.logger.WithFields(map[string]interface{}{
		"component":          "http3_handler",
		"closed_connections": connectionCount,
	}).Info("Closed all HTTP/3 connections")
}

// updateConnectionStats safely updates connection-related statistics.
func (h *HTTP3Handler) updateConnectionStats(activeChange, totalChange, closedChange int64) {
	h.statsMutex.Lock()
	defer h.statsMutex.Unlock()

	h.stats.ConnectionsActive += activeChange
	h.stats.ConnectionsTotal += totalChange
	h.stats.ConnectionsClosed += closedChange
}

// handleHTTP3Request processes incoming HTTP/3 requests with QUIC-specific optimizations.
func (h *HTTP3Handler) handleHTTP3Request(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Increment active requests
	h.statsMutex.Lock()
	h.stats.RequestsActive++
	h.stats.RequestsTotal++
	h.statsMutex.Unlock()

	// Ensure we decrement active requests when done
	defer func() {
		h.statsMutex.Lock()
		h.stats.RequestsActive--
		h.statsMutex.Unlock()
	}()

	// Add HTTP/3 specific headers for protocol advertisement
	w.Header().Set("Alt-Svc", fmt.Sprintf("h3=\":%d\"; ma=%d", h.config.Port, HTTP3AltSvcMaxAge))
	w.Header().Set("Server", "LoadBalancer-HTTP3/1.0")
	w.Header().Set("Vary", "Accept-Encoding")

	h.logger.WithFields(map[string]interface{}{
		"component":      "http3_handler",
		"method":         r.Method,
		"path":           r.URL.Path,
		"remote_addr":    r.RemoteAddr,
		"user_agent":     r.Header.Get("User-Agent"),
		"protocol":       r.Proto,
		"content_length": r.ContentLength,
	}).Debug("Processing HTTP/3 request")

	// Track connection if not already tracked
	connID := h.getConnectionID(r)
	h.trackConnection(connID, r)

	// Get backend from load balancer
	backend, err := h.loadBalancer.GetNextBackend()
	if err != nil {
		h.handleRequestError(w, r, err, startTime)
		return
	}

	// Process the request (in production, this would proxy to backend)
	h.processHTTP3Request(w, r, backend, startTime)
}

// getConnectionID extracts or generates a connection ID from the request.
func (h *HTTP3Handler) getConnectionID(r *http.Request) string {
	// In a real QUIC implementation, this would be the actual QUIC connection ID
	// For simulation, we use remote address and some request properties
	return fmt.Sprintf("%s-%d", r.RemoteAddr, rand.Int31n(1000000))
}

// trackConnection tracks a QUIC connection for monitoring and management.
func (h *HTTP3Handler) trackConnection(connID string, r *http.Request) {
	h.connMutex.Lock()
	defer h.connMutex.Unlock()

	if _, exists := h.connections[connID]; !exists {
		// Create new connection tracking
		conn := &QuicConnection{
			ID:                connID,
			RemoteAddr:        parseAddr(r.RemoteAddr),
			LocalAddr:         parseAddr(fmt.Sprintf(":%d", h.config.Port)),
			ConnectedAt:       time.Now(),
			LastActivity:      time.Now(),
			MaxStreams:        int(h.maxStreams),
			State:             "active",
			SupportsDatagrams: h.enableDatagrams,
			Supports0RTT:      h.enable0RTT,
		}

		h.connections[connID] = conn
		h.updateConnectionStats(1, 1, 0)

		h.logger.WithFields(map[string]interface{}{
			"component":     "http3_handler",
			"connection_id": connID,
			"remote_addr":   r.RemoteAddr,
		}).Debug("New HTTP/3 connection tracked")
	} else {
		// Update existing connection
		h.connections[connID].LastActivity = time.Now()
		h.connections[connID].StreamCount++
	}
}

// processHTTP3Request processes an HTTP/3 request with QUIC optimizations.
func (h *HTTP3Handler) processHTTP3Request(w http.ResponseWriter, r *http.Request, backend *domain.Backend, startTime time.Time) {
	// Simulate QUIC-specific processing optimizations
	processingTime := time.Millisecond * 25 // Faster than traditional HTTP due to QUIC benefits
	time.Sleep(processingTime)

	// Calculate response metrics
	latency := time.Since(startTime)
	responseBody := h.generateHTTP3Response(backend, latency)
	responseSize := int64(len(responseBody))

	// Update statistics
	h.updateRequestStats(true, responseSize, 0, latency)

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.FormatInt(responseSize, 10))
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	// Send response
	w.Write([]byte(responseBody))

	h.logger.WithFields(map[string]interface{}{
		"component":     "http3_handler",
		"method":        r.Method,
		"path":          r.URL.Path,
		"backend":       backend.URL,
		"latency_ms":    latency.Milliseconds(),
		"response_size": responseSize,
		"status":        "success",
	}).Debug("HTTP/3 request completed successfully")
}

// generateHTTP3Response creates a JSON response for HTTP/3 requests.
func (h *HTTP3Handler) generateHTTP3Response(backend *domain.Backend, latency time.Duration) string {
	response := map[string]interface{}{
		"status":     "success",
		"protocol":   "HTTP/3",
		"transport":  "QUIC",
		"backend":    backend.URL,
		"latency_ms": latency.Milliseconds(),
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"features": map[string]bool{
			"stream_multiplexing":  true,
			"server_push":          true,
			"connection_migration": h.enableMigration,
			"0rtt_resumption":      h.enable0RTT,
			"datagram_support":     h.enableDatagrams,
		},
		"connection_info": map[string]interface{}{
			"quic_version": "1.0",
			"tls_version":  "1.3",
			"cipher_suite": "TLS_AES_256_GCM_SHA384",
		},
	}

	jsonBytes, _ := json.MarshalIndent(response, "", "  ")
	return string(jsonBytes)
}

// handleRequestError processes HTTP/3 request errors.
func (h *HTTP3Handler) handleRequestError(w http.ResponseWriter, r *http.Request, err error, startTime time.Time) {
	latency := time.Since(startTime)

	// Update error statistics
	h.updateRequestStats(false, 0, 1, latency)

	// Generate error response
	errorResponse := map[string]interface{}{
		"status":     "error",
		"error":      err.Error(),
		"protocol":   "HTTP/3",
		"transport":  "QUIC",
		"latency_ms": latency.Milliseconds(),
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}

	jsonBytes, _ := json.MarshalIndent(errorResponse, "", "  ")
	responseSize := int64(len(jsonBytes))

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.FormatInt(responseSize, 10))
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write(jsonBytes)

	h.logger.WithFields(map[string]interface{}{
		"component":  "http3_handler",
		"method":     r.Method,
		"path":       r.URL.Path,
		"error":      err.Error(),
		"latency_ms": latency.Milliseconds(),
		"status":     "error",
	}).Error("HTTP/3 request failed")
}

// updateRequestStats safely updates request-related statistics.
func (h *HTTP3Handler) updateRequestStats(success bool, bytesSent, bytesFailed int64, latency time.Duration) {
	h.statsMutex.Lock()
	defer h.statsMutex.Unlock()

	if success {
		h.stats.RequestsSuccessful++
		h.stats.BytesSent += bytesSent
	} else {
		h.stats.RequestsFailed++
	}

	// Update average latency (exponential moving average)
	if h.stats.RequestsTotal > 0 {
		alpha := 0.1 // Smoothing factor
		currentLatency := float64(latency.Milliseconds())
		h.stats.AverageLatency = alpha*currentLatency + (1-alpha)*h.stats.AverageLatency
	} else {
		h.stats.AverageLatency = float64(latency.Milliseconds())
	}
}

// handleHealth provides HTTP/3 server health check endpoint.
func (h *HTTP3Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":      "healthy",
		"protocol":    "HTTP/3",
		"transport":   "QUIC",
		"enabled":     h.IsEnabled(),
		"port":        h.config.Port,
		"connections": len(h.connections),
		"uptime":      time.Since(h.stats.LastStartTime).String(),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"features": map[string]bool{
			"stream_multiplexing":  true,
			"server_push":          true,
			"connection_migration": h.enableMigration,
			"0rtt_resumption":      h.enable0RTT,
			"datagram_support":     h.enableDatagrams,
		},
	}

	jsonBytes, _ := json.MarshalIndent(health, "", "  ")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(jsonBytes)))
	w.WriteHeader(http.StatusOK)
	w.Write(jsonBytes)
}

// handleStats provides detailed HTTP/3 statistics endpoint.
func (h *HTTP3Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := h.GetStats()
	jsonBytes, _ := json.MarshalIndent(stats, "", "  ")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(jsonBytes)))
	w.WriteHeader(http.StatusOK)
	w.Write(jsonBytes)
}

// handleConnections provides detailed connection information endpoint.
func (h *HTTP3Handler) handleConnections(w http.ResponseWriter, r *http.Request) {
	h.connMutex.RLock()
	connections := make([]*QuicConnection, 0, len(h.connections))
	for _, conn := range h.connections {
		connections = append(connections, conn)
	}
	h.connMutex.RUnlock()

	response := map[string]interface{}{
		"total_connections":  len(connections),
		"active_connections": h.stats.ConnectionsActive,
		"connections":        connections,
		"timestamp":          time.Now().UTC().Format(time.RFC3339),
	}

	jsonBytes, _ := json.MarshalIndent(response, "", "  ")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(jsonBytes)))
	w.WriteHeader(http.StatusOK)
	w.Write(jsonBytes)
}

// parseAddr parses a network address string into a net.Addr.
// This is a simplified implementation for demonstration purposes.
func parseAddr(addr string) net.Addr {
	if strings.Contains(addr, ":") {
		parts := strings.Split(addr, ":")
		if len(parts) >= 2 {
			return &net.TCPAddr{
				IP:   net.ParseIP(parts[0]),
				Port: parsePort(parts[1]),
			}
		}
	}
	return &net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 443,
	}
}

// parsePort parses a port string into an integer.
func parsePort(portStr string) int {
	if port, err := strconv.Atoi(portStr); err == nil {
		return port
	}
	return 443 // Default HTTPS port
}
