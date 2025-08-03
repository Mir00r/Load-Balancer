package handler

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// L4Handler provides Layer 4 (TCP/UDP) load balancing capabilities
type L4Handler struct {
	loadBalancer domain.LoadBalancer
	config       *L4Config
	logger       *logger.Logger
	connections  map[string]*ConnectionTracker
	connMutex    sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
}

// ConnectionTracker tracks active Layer 4 connections
type ConnectionTracker struct {
	ClientConn   net.Conn
	BackendConn  net.Conn
	BackendID    string
	StartTime    time.Time
	BytesRead    int64
	BytesWritten int64
	Active       bool
	mutex        sync.RWMutex
}

// L4Config holds Layer 4 load balancer configuration
type L4Config struct {
	Protocol        string        `json:"protocol" yaml:"protocol"`             // "tcp" or "udp"
	ListenAddress   string        `json:"listen_address" yaml:"listen_address"` // e.g., ":8080"
	IdleTimeout     time.Duration `json:"idle_timeout" yaml:"idle_timeout"`
	ConnectTimeout  time.Duration `json:"connect_timeout" yaml:"connect_timeout"`
	MaxConnections  int           `json:"max_connections" yaml:"max_connections"`
	BufferSize      int           `json:"buffer_size" yaml:"buffer_size"`
	EnableKeepalive bool          `json:"enable_keepalive" yaml:"enable_keepalive"`
	KeepaliveIdle   time.Duration `json:"keepalive_idle" yaml:"keepalive_idle"`
}

// NewL4Handler creates a new Layer 4 load balancer handler
func NewL4Handler(loadBalancer domain.LoadBalancer, config *L4Config, logger *logger.Logger) *L4Handler {
	ctx, cancel := context.WithCancel(context.Background())

	if config == nil {
		config = &L4Config{
			Protocol:        "tcp",
			ListenAddress:   ":8080",
			IdleTimeout:     5 * time.Minute,
			ConnectTimeout:  10 * time.Second,
			MaxConnections:  1000,
			BufferSize:      32 * 1024, // 32KB
			EnableKeepalive: true,
			KeepaliveIdle:   5 * time.Minute,
		}
	}

	return &L4Handler{
		loadBalancer: loadBalancer,
		config:       config,
		logger:       logger,
		connections:  make(map[string]*ConnectionTracker),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start starts the Layer 4 load balancer listener
func (h *L4Handler) Start() error {
	switch h.config.Protocol {
	case "tcp":
		return h.startTCPListener()
	case "udp":
		return h.startUDPListener()
	default:
		return fmt.Errorf("unsupported protocol: %s", h.config.Protocol)
	}
}

// Stop gracefully stops the Layer 4 load balancer
func (h *L4Handler) Stop() error {
	h.logger.Info("Stopping Layer 4 load balancer")
	h.cancel()

	// Close all active connections
	h.connMutex.Lock()
	defer h.connMutex.Unlock()

	for connID, tracker := range h.connections {
		h.closeConnection(connID, tracker)
	}

	return nil
}

// startTCPListener starts the TCP listener and handles connections
func (h *L4Handler) startTCPListener() error {
	listener, err := net.Listen("tcp", h.config.ListenAddress)
	if err != nil {
		return fmt.Errorf("failed to start TCP listener: %w", err)
	}
	defer listener.Close()

	h.logger.WithField("address", h.config.ListenAddress).Info("Layer 4 TCP load balancer started")

	for {
		select {
		case <-h.ctx.Done():
			return nil
		default:
			conn, err := listener.Accept()
			if err != nil {
				h.logger.WithError(err).Error("Failed to accept TCP connection")
				continue
			}

			// Check connection limits
			if h.getActiveConnectionCount() >= h.config.MaxConnections {
				h.logger.Warn("Maximum connections reached, rejecting new connection")
				conn.Close()
				continue
			}

			go h.handleTCPConnection(conn)
		}
	}
}

// startUDPListener starts the UDP listener and handles packets
func (h *L4Handler) startUDPListener() error {
	addr, err := net.ResolveUDPAddr("udp", h.config.ListenAddress)
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to start UDP listener: %w", err)
	}
	defer conn.Close()

	h.logger.WithField("address", h.config.ListenAddress).Info("Layer 4 UDP load balancer started")

	buffer := make([]byte, h.config.BufferSize)

	for {
		select {
		case <-h.ctx.Done():
			return nil
		default:
			n, clientAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				h.logger.WithError(err).Error("Failed to read UDP packet")
				continue
			}

			go h.handleUDPPacket(conn, clientAddr, buffer[:n])
		}
	}
}

// handleTCPConnection handles a single TCP connection
func (h *L4Handler) handleTCPConnection(clientConn net.Conn) {
	defer clientConn.Close()

	// Set connection timeouts
	if h.config.IdleTimeout > 0 {
		clientConn.SetDeadline(time.Now().Add(h.config.IdleTimeout))
	}

	// Enable TCP keepalive if configured
	if h.config.EnableKeepalive {
		if tcpConn, ok := clientConn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			if h.config.KeepaliveIdle > 0 {
				tcpConn.SetKeepAlivePeriod(h.config.KeepaliveIdle)
			}
		}
	}

	// Select a backend
	backend := h.loadBalancer.SelectBackend()
	if backend == nil {
		h.logger.Error("No healthy backends available")
		return
	}

	// Connect to backend
	backendConn, err := h.connectToBackend(backend)
	if err != nil {
		h.logger.WithError(err).WithField("backend", backend.URL).Error("Failed to connect to backend")
		return
	}
	defer backendConn.Close()

	// Create connection tracker
	connID := fmt.Sprintf("%s-%s", clientConn.RemoteAddr().String(), backend.ID)
	tracker := &ConnectionTracker{
		ClientConn:  clientConn,
		BackendConn: backendConn,
		BackendID:   backend.ID,
		StartTime:   time.Now(),
		Active:      true,
	}

	h.connMutex.Lock()
	h.connections[connID] = tracker
	h.connMutex.Unlock()

	defer func() {
		h.connMutex.Lock()
		h.closeConnection(connID, tracker)
		delete(h.connections, connID)
		h.connMutex.Unlock()
	}()

	h.logger.WithFields(map[string]interface{}{
		"client":  clientConn.RemoteAddr().String(),
		"backend": backend.URL,
		"conn_id": connID,
	}).Debug("TCP connection established")

	// Start bidirectional data transfer
	var wg sync.WaitGroup
	wg.Add(2)

	// Client to backend
	go func() {
		defer wg.Done()
		bytes, err := h.copyData(clientConn, backendConn, "client->backend")
		tracker.mutex.Lock()
		tracker.BytesRead += bytes
		tracker.mutex.Unlock()
		if err != nil && err != io.EOF {
			h.logger.WithError(err).Debug("Error copying data from client to backend")
		}
	}()

	// Backend to client
	go func() {
		defer wg.Done()
		bytes, err := h.copyData(backendConn, clientConn, "backend->client")
		tracker.mutex.Lock()
		tracker.BytesWritten += bytes
		tracker.mutex.Unlock()
		if err != nil && err != io.EOF {
			h.logger.WithError(err).Debug("Error copying data from backend to client")
		}
	}()

	wg.Wait()

	duration := time.Since(tracker.StartTime)
	h.logger.WithFields(map[string]interface{}{
		"client":        clientConn.RemoteAddr().String(),
		"backend":       backend.URL,
		"duration":      duration,
		"bytes_read":    tracker.BytesRead,
		"bytes_written": tracker.BytesWritten,
	}).Debug("TCP connection closed")
}

// handleUDPPacket handles a single UDP packet
func (h *L4Handler) handleUDPPacket(serverConn *net.UDPConn, clientAddr *net.UDPAddr, data []byte) {
	// Select a backend
	backend := h.loadBalancer.SelectBackend()
	if backend == nil {
		h.logger.Error("No healthy backends available for UDP packet")
		return
	}

	// Parse backend address
	backendAddr, err := net.ResolveUDPAddr("udp", backend.URL)
	if err != nil {
		h.logger.WithError(err).WithField("backend", backend.URL).Error("Failed to resolve backend UDP address")
		return
	}

	// Connect to backend
	backendConn, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		h.logger.WithError(err).WithField("backend", backend.URL).Error("Failed to connect to backend UDP")
		return
	}
	defer backendConn.Close()

	// Set timeouts
	if h.config.ConnectTimeout > 0 {
		backendConn.SetDeadline(time.Now().Add(h.config.ConnectTimeout))
	}

	// Forward packet to backend
	_, err = backendConn.Write(data)
	if err != nil {
		h.logger.WithError(err).Error("Failed to write UDP packet to backend")
		return
	}

	// Read response from backend
	response := make([]byte, h.config.BufferSize)
	n, err := backendConn.Read(response)
	if err != nil {
		h.logger.WithError(err).Error("Failed to read UDP response from backend")
		return
	}

	// Forward response back to client
	_, err = serverConn.WriteToUDP(response[:n], clientAddr)
	if err != nil {
		h.logger.WithError(err).Error("Failed to write UDP response to client")
		return
	}

	h.logger.WithFields(map[string]interface{}{
		"client":        clientAddr.String(),
		"backend":       backend.URL,
		"request_size":  len(data),
		"response_size": n,
	}).Debug("UDP packet processed")
}

// connectToBackend establishes a connection to a backend server
func (h *L4Handler) connectToBackend(backend *domain.Backend) (net.Conn, error) {
	// Set connection timeout
	timeout := h.config.ConnectTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	conn, err := net.DialTimeout(h.config.Protocol, backend.URL, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to backend %s: %w", backend.URL, err)
	}

	// Configure TCP-specific options
	if h.config.Protocol == "tcp" && h.config.EnableKeepalive {
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			if h.config.KeepaliveIdle > 0 {
				tcpConn.SetKeepAlivePeriod(h.config.KeepaliveIdle)
			}
		}
	}

	return conn, nil
}

// copyData copies data from source to destination and returns bytes copied
func (h *L4Handler) copyData(src, dst net.Conn, direction string) (int64, error) {
	buffer := make([]byte, h.config.BufferSize)
	totalBytes := int64(0)

	for {
		// Set read timeout
		if h.config.IdleTimeout > 0 {
			src.SetReadDeadline(time.Now().Add(h.config.IdleTimeout))
		}

		n, err := src.Read(buffer)
		if n > 0 {
			// Set write timeout
			if h.config.IdleTimeout > 0 {
				dst.SetWriteDeadline(time.Now().Add(h.config.IdleTimeout))
			}

			written, writeErr := dst.Write(buffer[:n])
			totalBytes += int64(written)

			if writeErr != nil {
				return totalBytes, writeErr
			}
		}

		if err != nil {
			if err != io.EOF {
				h.logger.WithError(err).WithField("direction", direction).Debug("Connection read error")
			}
			return totalBytes, err
		}
	}
}

// closeConnection safely closes a connection and updates its state
func (h *L4Handler) closeConnection(connID string, tracker *ConnectionTracker) {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()

	if !tracker.Active {
		return
	}

	tracker.Active = false

	if tracker.ClientConn != nil {
		tracker.ClientConn.Close()
	}

	if tracker.BackendConn != nil {
		tracker.BackendConn.Close()
	}
}

// getActiveConnectionCount returns the number of active connections
func (h *L4Handler) getActiveConnectionCount() int {
	h.connMutex.RLock()
	defer h.connMutex.RUnlock()

	activeCount := 0
	for _, tracker := range h.connections {
		tracker.mutex.RLock()
		if tracker.Active {
			activeCount++
		}
		tracker.mutex.RUnlock()
	}

	return activeCount
}

// GetConnectionStats returns statistics about active connections
func (h *L4Handler) GetConnectionStats() map[string]interface{} {
	h.connMutex.RLock()
	defer h.connMutex.RUnlock()

	stats := map[string]interface{}{
		"total_connections":  len(h.connections),
		"active_connections": h.getActiveConnectionCount(),
		"protocol":           h.config.Protocol,
		"listen_address":     h.config.ListenAddress,
	}

	// Per-backend connection counts
	backendCounts := make(map[string]int)
	for _, tracker := range h.connections {
		tracker.mutex.RLock()
		if tracker.Active {
			backendCounts[tracker.BackendID]++
		}
		tracker.mutex.RUnlock()
	}

	stats["backend_connections"] = backendCounts

	return stats
}
