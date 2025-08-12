// Additional methods for the HTTP/3 handler
// These methods complete the HTTP/3 handler functionality

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
func (h *HTTP3Handler) connectionManager(ctx context.Context) {
	ticker := time.NewTicker(h.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopChan:
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
	}

	if len(staleConnections) > 0 {
		h.logger.WithFields(map[string]interface{}{
			"component":           "http3_handler",
			"cleaned_connections": len(staleConnections),
		}).Debug("Cleaned up stale HTTP/3 connections")
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

	// Calculate average bandwidth
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
	for _, conn := range h.connections {
		conn.State = "closed"
		conn.Closed = true
	}

	h.connections = make(map[string]*QuicConnection)
	h.updateConnectionStats(-int64(connectionCount), 0, int64(connectionCount))
}

// updateConnectionStats safely updates connection-related statistics.
func (h *HTTP3Handler) updateConnectionStats(activeChange, totalChange, closedChange int64) {
	h.statsMutex.Lock()
	defer h.statsMutex.Unlock()

	h.stats.ConnectionsActive += activeChange
	h.stats.ConnectionsTotal += totalChange
	h.stats.ConnectionsClosed += closedChange
}

// handleHTTP3Request processes incoming HTTP/3 requests.
func (h *HTTP3Handler) handleHTTP3Request(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Increment active requests
	h.statsMutex.Lock()
	h.stats.RequestsActive++
	h.stats.RequestsTotal++
	h.statsMutex.Unlock()

	// Decrement when done
	defer func() {
		h.statsMutex.Lock()
		h.stats.RequestsActive--
		h.statsMutex.Unlock()
	}()

	// Add HTTP/3 specific headers
	w.Header().Set("Alt-Svc", fmt.Sprintf("h3=\":%d\"; ma=%d", h.config.Port, HTTP3AltSvcMaxAge))
	w.Header().Set("Server", "LoadBalancer-HTTP3/1.0")

	// Track connection
	connID := h.getConnectionID(r)
	h.trackConnection(connID, r)

	// Get backend from load balancer
	backend := h.loadBalancer.SelectBackend()
	if backend == nil {
		h.handleRequestError(w, r, fmt.Errorf("no backend available"), startTime)
		return
	}

	// Process the request
	h.processHTTP3Request(w, r, backend, startTime)
}

// getConnectionID generates a connection ID from the request.
func (h *HTTP3Handler) getConnectionID(r *http.Request) string {
	return fmt.Sprintf("%s-%d", r.RemoteAddr, time.Now().UnixNano()%1000000)
}

// trackConnection tracks a QUIC connection for monitoring.
func (h *HTTP3Handler) trackConnection(connID string, r *http.Request) {
	h.connMutex.Lock()
	defer h.connMutex.Unlock()

	if _, exists := h.connections[connID]; !exists {
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
	} else {
		h.connections[connID].LastActivity = time.Now()
		h.connections[connID].StreamCount++
	}
}

// processHTTP3Request processes an HTTP/3 request.
func (h *HTTP3Handler) processHTTP3Request(w http.ResponseWriter, r *http.Request, backend *domain.Backend, startTime time.Time) {
	// Simulate processing
	time.Sleep(25 * time.Millisecond)

	latency := time.Since(startTime)
	responseBody := h.generateHTTP3Response(backend, latency)
	responseSize := int64(len(responseBody))

	h.updateRequestStats(true, responseSize, 0, latency)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(responseBody))
}

// generateHTTP3Response creates a JSON response.
func (h *HTTP3Handler) generateHTTP3Response(backend *domain.Backend, latency time.Duration) string {
	return fmt.Sprintf(`{
  "status": "success",
  "protocol": "HTTP/3",
  "transport": "QUIC",
  "backend": "%s",
  "latency_ms": %d,
  "timestamp": "%s",
  "features": {
    "stream_multiplexing": true,
    "connection_migration": %t,
    "0rtt_resumption": %t,
    "datagram_support": %t
  }
}`, backend.URL, latency.Milliseconds(), time.Now().UTC().Format(time.RFC3339),
		h.enableMigration, h.enable0RTT, h.enableDatagrams)
}

// handleRequestError processes HTTP/3 request errors.
func (h *HTTP3Handler) handleRequestError(w http.ResponseWriter, r *http.Request, err error, startTime time.Time) {
	latency := time.Since(startTime)
	h.updateRequestStats(false, 0, 1, latency)

	errorResponse := fmt.Sprintf(`{
  "status": "error",
  "error": "%s",
  "protocol": "HTTP/3",
  "latency_ms": %d,
  "timestamp": "%s"
}`, err.Error(), latency.Milliseconds(), time.Now().UTC().Format(time.RFC3339))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte(errorResponse))
}

// updateRequestStats safely updates request statistics.
func (h *HTTP3Handler) updateRequestStats(success bool, bytesSent, bytesFailed int64, latency time.Duration) {
	h.statsMutex.Lock()
	defer h.statsMutex.Unlock()

	if success {
		h.stats.RequestsSuccessful++
		h.stats.BytesSent += bytesSent
	} else {
		h.stats.RequestsFailed++
	}

	// Update average latency
	if h.stats.RequestsTotal > 0 {
		alpha := 0.1
		currentLatency := float64(latency.Milliseconds())
		h.stats.AverageLatency = alpha*currentLatency + (1-alpha)*h.stats.AverageLatency
	} else {
		h.stats.AverageLatency = float64(latency.Milliseconds())
	}
}

// handleHealth provides HTTP/3 server health check endpoint.
func (h *HTTP3Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	healthResponse := fmt.Sprintf(`{
  "status": "healthy",
  "protocol": "HTTP/3",
  "enabled": %t,
  "port": %d,
  "connections": %d,
  "timestamp": "%s"
}`, h.IsEnabled(), h.config.Port, len(h.connections), time.Now().UTC().Format(time.RFC3339))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(healthResponse))
}

// handleStats provides detailed HTTP/3 statistics endpoint.
func (h *HTTP3Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := h.GetStats()

	statsResponse := fmt.Sprintf(`{
  "enabled": %t,
  "status": "%s",
  "port": %d,
  "uptime": "%s",
  "requests_total": %d,
  "requests_successful": %d,
  "requests_failed": %d,
  "connections_total": %d,
  "connections_active": %d,
  "average_latency_ms": %.2f,
  "quic_version": "1.0"
}`, stats["enabled"], stats["status"], stats["port"], stats["uptime"],
		stats["requests_total"], stats["requests_successful"], stats["requests_failed"],
		stats["connections_total"], stats["connections_active"], stats["average_latency_ms"])

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(statsResponse))
}

// handleConnections provides detailed connection information endpoint.
func (h *HTTP3Handler) handleConnections(w http.ResponseWriter, r *http.Request) {
	h.connMutex.RLock()
	connectionCount := len(h.connections)
	h.connMutex.RUnlock()

	connectionsResponse := fmt.Sprintf(`{
  "total_connections": %d,
  "active_connections": %d,
  "timestamp": "%s"
}`, connectionCount, h.stats.ConnectionsActive, time.Now().UTC().Format(time.RFC3339))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(connectionsResponse))
}

// parseAddr parses a network address string into a net.Addr.
func parseAddr(addr string) net.Addr {
	parts := strings.Split(addr, ":")
	if len(parts) >= 2 {
		if port, err := parsePort(parts[1]); err == nil {
			return &net.TCPAddr{
				IP:   net.ParseIP(parts[0]),
				Port: port,
			}
		}
	}
	return &net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 443,
	}
}

// parsePort parses a port string into an integer.
func parsePort(portStr string) (int, error) {
	var port int
	_, err := fmt.Sscanf(portStr, "%d", &port)
	if err != nil {
		return 443, err
	}
	return port, nil
}
