package handler

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// ConnectionPool manages a pool of persistent connections to backend servers
type ConnectionPool struct {
	pools  map[string]*BackendPool
	config *ConnectionPoolConfig
	logger *logger.Logger
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

// BackendPool holds connections for a specific backend
type BackendPool struct {
	backend     *domain.Backend
	connections chan *PooledConnection
	activeConns int64
	totalConns  int64
	mu          sync.RWMutex
	lastUsed    time.Time
}

// PooledConnection represents a connection in the pool
type PooledConnection struct {
	Conn       net.Conn
	CreatedAt  time.Time
	LastUsed   time.Time
	UsageCount int64
	IsHealthy  bool
	BackendID  string
}

// ConnectionPoolConfig holds connection pool configuration
type ConnectionPoolConfig struct {
	// MaxIdleConns is the maximum number of idle connections per backend
	MaxIdleConns int `json:"max_idle_conns" yaml:"max_idle_conns"`
	// MaxActiveConns is the maximum number of active connections per backend
	MaxActiveConns int `json:"max_active_conns" yaml:"max_active_conns"`
	// MaxConnLifetime is the maximum lifetime of a connection
	MaxConnLifetime time.Duration `json:"max_conn_lifetime" yaml:"max_conn_lifetime"`
	// IdleTimeout is the timeout for idle connections
	IdleTimeout time.Duration `json:"idle_timeout" yaml:"idle_timeout"`
	// ConnectTimeout is the timeout for establishing new connections
	ConnectTimeout time.Duration `json:"connect_timeout" yaml:"connect_timeout"`
	// HealthCheckInterval is the interval for health checking pooled connections
	HealthCheckInterval time.Duration `json:"health_check_interval" yaml:"health_check_interval"`
	// EnableKeepalive enables TCP keepalive on connections
	EnableKeepalive bool `json:"enable_keepalive" yaml:"enable_keepalive"`
	// KeepaliveIdle is the time before sending keepalive probes
	KeepaliveIdle time.Duration `json:"keepalive_idle" yaml:"keepalive_idle"`
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(config *ConnectionPoolConfig, logger *logger.Logger) *ConnectionPool {
	if config == nil {
		config = &ConnectionPoolConfig{
			MaxIdleConns:        10,
			MaxActiveConns:      100,
			MaxConnLifetime:     30 * time.Minute,
			IdleTimeout:         5 * time.Minute,
			ConnectTimeout:      10 * time.Second,
			HealthCheckInterval: 30 * time.Second,
			EnableKeepalive:     true,
			KeepaliveIdle:       5 * time.Minute,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	pool := &ConnectionPool{
		pools:  make(map[string]*BackendPool),
		config: config,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}

	// Start background tasks
	go pool.cleanupWorker()
	go pool.healthCheckWorker()

	return pool
}

// GetConnection retrieves a connection from the pool or creates a new one
func (cp *ConnectionPool) GetConnection(backend *domain.Backend) (*PooledConnection, error) {
	cp.mu.RLock()
	pool, exists := cp.pools[backend.ID]
	cp.mu.RUnlock()

	if !exists {
		cp.mu.Lock()
		// Double-check after acquiring write lock
		if pool, exists = cp.pools[backend.ID]; !exists {
			pool = cp.createBackendPool(backend)
			cp.pools[backend.ID] = pool
		}
		cp.mu.Unlock()
	}

	return pool.getConnection(cp.config, cp.logger)
}

// ReturnConnection returns a connection to the pool
func (cp *ConnectionPool) ReturnConnection(conn *PooledConnection) {
	if conn == nil || !conn.IsHealthy {
		if conn != nil && conn.Conn != nil {
			conn.Conn.Close()
		}
		return
	}

	cp.mu.RLock()
	pool, exists := cp.pools[conn.BackendID]
	cp.mu.RUnlock()

	if exists {
		pool.returnConnection(conn)
	} else {
		// Pool doesn't exist anymore, close the connection
		conn.Conn.Close()
	}
}

// RemoveBackend removes all connections for a backend
func (cp *ConnectionPool) RemoveBackend(backendID string) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if pool, exists := cp.pools[backendID]; exists {
		pool.closeAll()
		delete(cp.pools, backendID)
		cp.logger.WithField("backend_id", backendID).Info("Removed backend from connection pool")
	}
}

// Close closes all connections and stops background workers
func (cp *ConnectionPool) Close() error {
	cp.cancel()

	cp.mu.Lock()
	defer cp.mu.Unlock()

	for backendID, pool := range cp.pools {
		pool.closeAll()
		delete(cp.pools, backendID)
	}

	cp.logger.Info("Connection pool closed")
	return nil
}

// GetStats returns connection pool statistics
func (cp *ConnectionPool) GetStats() map[string]interface{} {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	stats := make(map[string]interface{})
	backendStats := make(map[string]interface{})

	totalIdle := 0
	totalActive := 0

	for backendID, pool := range cp.pools {
		pool.mu.RLock()
		idle := len(pool.connections)
		active := int(pool.activeConns)
		total := int(pool.totalConns)
		lastUsed := pool.lastUsed
		pool.mu.RUnlock()

		backendStats[backendID] = map[string]interface{}{
			"idle_connections":   idle,
			"active_connections": active,
			"total_connections":  total,
			"last_used":          lastUsed,
		}

		totalIdle += idle
		totalActive += active
	}

	stats["backends"] = backendStats
	stats["total_idle_connections"] = totalIdle
	stats["total_active_connections"] = totalActive
	stats["total_backends"] = len(cp.pools)

	return stats
}

// createBackendPool creates a new backend pool
func (cp *ConnectionPool) createBackendPool(backend *domain.Backend) *BackendPool {
	return &BackendPool{
		backend:     backend,
		connections: make(chan *PooledConnection, cp.config.MaxIdleConns),
		lastUsed:    time.Now(),
	}
}

// getConnection gets a connection from the backend pool
func (bp *BackendPool) getConnection(config *ConnectionPoolConfig, logger *logger.Logger) (*PooledConnection, error) {
	bp.mu.Lock()
	bp.lastUsed = time.Now()

	// Check if we've exceeded the maximum active connections
	if bp.activeConns >= int64(config.MaxActiveConns) {
		bp.mu.Unlock()
		return nil, &ConnectionPoolError{
			Message: "maximum active connections reached",
			Code:    "MAX_ACTIVE_EXCEEDED",
		}
	}

	bp.activeConns++
	bp.mu.Unlock()

	// Try to get an existing connection from the pool
	select {
	case conn := <-bp.connections:
		// Check if connection is still valid
		if bp.isConnectionValid(conn, config) {
			conn.LastUsed = time.Now()
			conn.UsageCount++
			return conn, nil
		}
		// Connection is invalid, close it and create a new one
		conn.Conn.Close()
	default:
		// No idle connections available
	}

	// Create a new connection
	return bp.createNewConnection(config, logger)
}

// returnConnection returns a connection to the pool
func (bp *BackendPool) returnConnection(conn *PooledConnection) {
	bp.mu.Lock()
	bp.activeConns--
	bp.mu.Unlock()

	// Try to return the connection to the pool
	select {
	case bp.connections <- conn:
		// Successfully returned to pool
	default:
		// Pool is full, close the connection
		conn.Conn.Close()
	}
}

// createNewConnection creates a new connection to the backend
func (bp *BackendPool) createNewConnection(config *ConnectionPoolConfig, logger *logger.Logger) (*PooledConnection, error) {
	// Set connection timeout
	dialer := &net.Dialer{
		Timeout: config.ConnectTimeout,
	}

	conn, err := dialer.Dial("tcp", bp.backend.URL)
	if err != nil {
		bp.mu.Lock()
		bp.activeConns-- // Decrement on failure
		bp.mu.Unlock()
		return nil, err
	}

	// Configure TCP keepalive if enabled
	if config.EnableKeepalive {
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			if config.KeepaliveIdle > 0 {
				tcpConn.SetKeepAlivePeriod(config.KeepaliveIdle)
			}
		}
	}

	pooledConn := &PooledConnection{
		Conn:      conn,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
		IsHealthy: true,
		BackendID: bp.backend.ID,
	}

	bp.mu.Lock()
	bp.totalConns++
	bp.mu.Unlock()

	logger.WithField("backend_id", bp.backend.ID).Debug("Created new pooled connection")
	return pooledConn, nil
}

// isConnectionValid checks if a connection is still valid and usable
func (bp *BackendPool) isConnectionValid(conn *PooledConnection, config *ConnectionPoolConfig) bool {
	now := time.Now()

	// Check connection age
	if now.Sub(conn.CreatedAt) > config.MaxConnLifetime {
		return false
	}

	// Check idle timeout
	if now.Sub(conn.LastUsed) > config.IdleTimeout {
		return false
	}

	// Check if connection is still healthy
	if !conn.IsHealthy {
		return false
	}

	// Try to detect broken connections by setting a short deadline
	conn.Conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
	buffer := make([]byte, 1)
	_, err := conn.Conn.Read(buffer)
	conn.Conn.SetReadDeadline(time.Time{}) // Reset deadline

	// If we get an error other than timeout, the connection might be broken
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			// Timeout is expected, connection is likely fine
			return true
		}
		// Other errors indicate a broken connection
		return false
	}

	return true
}

// closeAll closes all connections in the pool
func (bp *BackendPool) closeAll() {
	close(bp.connections)
	for conn := range bp.connections {
		if conn.Conn != nil {
			conn.Conn.Close()
		}
	}
}

// cleanupWorker periodically cleans up expired connections
func (cp *ConnectionPool) cleanupWorker() {
	ticker := time.NewTicker(cp.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cp.ctx.Done():
			return
		case <-ticker.C:
			cp.cleanup()
		}
	}
}

// cleanup removes expired connections from all pools
func (cp *ConnectionPool) cleanup() {
	cp.mu.RLock()
	pools := make([]*BackendPool, 0, len(cp.pools))
	for _, pool := range cp.pools {
		pools = append(pools, pool)
	}
	cp.mu.RUnlock()

	for _, pool := range pools {
		pool.cleanupExpiredConnections(cp.config)
	}
}

// cleanupExpiredConnections removes expired connections from a backend pool
func (bp *BackendPool) cleanupExpiredConnections(config *ConnectionPoolConfig) {
	expiredConns := make([]*PooledConnection, 0)

	// Collect expired connections
	for {
		select {
		case conn := <-bp.connections:
			if !bp.isConnectionValid(conn, config) {
				expiredConns = append(expiredConns, conn)
			} else {
				// Return valid connection back to pool
				select {
				case bp.connections <- conn:
				default:
					// Pool is full, close the connection
					expiredConns = append(expiredConns, conn)
				}
			}
		default:
			// No more connections to check
			goto cleanup
		}
	}

cleanup:
	// Close expired connections
	for _, conn := range expiredConns {
		if conn.Conn != nil {
			conn.Conn.Close()
		}
	}

	if len(expiredConns) > 0 {
		bp.mu.Lock()
		bp.totalConns -= int64(len(expiredConns))
		bp.mu.Unlock()
	}
}

// healthCheckWorker periodically health checks pooled connections
func (cp *ConnectionPool) healthCheckWorker() {
	ticker := time.NewTicker(cp.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cp.ctx.Done():
			return
		case <-ticker.C:
			cp.healthCheckConnections()
		}
	}
}

// healthCheckConnections performs health checks on pooled connections
func (cp *ConnectionPool) healthCheckConnections() {
	cp.mu.RLock()
	pools := make([]*BackendPool, 0, len(cp.pools))
	for _, pool := range cp.pools {
		pools = append(pools, pool)
	}
	cp.mu.RUnlock()

	for _, pool := range pools {
		pool.healthCheckPool(cp.config)
	}
}

// healthCheckPool performs health checks on connections in a backend pool
func (bp *BackendPool) healthCheckPool(config *ConnectionPoolConfig) {
	// For now, we rely on the isConnectionValid method
	// In a more sophisticated implementation, we could send actual health check requests
	bp.cleanupExpiredConnections(config)
}

// ConnectionPoolError represents a connection pool error
type ConnectionPoolError struct {
	Message string
	Code    string
}

// Error returns the error message
func (e *ConnectionPoolError) Error() string {
	return e.Message
}

// PoolingTransport is an http.RoundTripper that uses connection pooling
type PoolingTransport struct {
	pool   *ConnectionPool
	logger *logger.Logger
}

// NewPoolingTransport creates a new pooling transport
func NewPoolingTransport(pool *ConnectionPool, logger *logger.Logger) *PoolingTransport {
	return &PoolingTransport{
		pool:   pool,
		logger: logger,
	}
}

// RoundTrip implements http.RoundTripper interface
func (pt *PoolingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// This is a simplified implementation
	// In a full implementation, you would need to handle HTTP over the pooled connections
	return http.DefaultTransport.RoundTrip(req)
}
