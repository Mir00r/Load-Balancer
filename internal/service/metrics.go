package service

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics implements the domain.Metrics interface
type Metrics struct {
	// Global counters
	totalRequests int64
	totalErrors   int64

	// Per-backend metrics
	backendMetrics map[string]*BackendMetrics
	mu             sync.RWMutex

	// Request latency tracking
	latencyBuckets map[string]*LatencyBuckets
}

// BackendMetrics holds metrics for a specific backend
type BackendMetrics struct {
	Requests     int64     `json:"requests"`
	Errors       int64     `json:"errors"`
	TotalLatency int64     `json:"total_latency_ms"`
	MinLatency   int64     `json:"min_latency_ms"`
	MaxLatency   int64     `json:"max_latency_ms"`
	LastRequest  time.Time `json:"last_request"`
	SuccessRate  float64   `json:"success_rate"`
}

// LatencyBuckets holds latency distribution data
type LatencyBuckets struct {
	Under10ms   int64 `json:"under_10ms"`
	Under50ms   int64 `json:"under_50ms"`
	Under100ms  int64 `json:"under_100ms"`
	Under500ms  int64 `json:"under_500ms"`
	Under1000ms int64 `json:"under_1000ms"`
	Over1000ms  int64 `json:"over_1000ms"`
}

// NewMetrics creates a new metrics instance
func NewMetrics() *Metrics {
	return &Metrics{
		backendMetrics: make(map[string]*BackendMetrics),
		latencyBuckets: make(map[string]*LatencyBuckets),
	}
}

// IncrementRequests increments the total request count for a backend
func (m *Metrics) IncrementRequests(backendID string) {
	atomic.AddInt64(&m.totalRequests, 1)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.backendMetrics[backendID] == nil {
		m.backendMetrics[backendID] = &BackendMetrics{
			MinLatency: int64(^uint64(0) >> 1), // Max int64 value
		}
	}

	atomic.AddInt64(&m.backendMetrics[backendID].Requests, 1)
	m.backendMetrics[backendID].LastRequest = time.Now()
}

// IncrementErrors increments the error count for a backend
func (m *Metrics) IncrementErrors(backendID string) {
	atomic.AddInt64(&m.totalErrors, 1)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.backendMetrics[backendID] == nil {
		m.backendMetrics[backendID] = &BackendMetrics{
			MinLatency: int64(^uint64(0) >> 1), // Max int64 value
		}
	}

	atomic.AddInt64(&m.backendMetrics[backendID].Errors, 1)

	// Update success rate
	requests := atomic.LoadInt64(&m.backendMetrics[backendID].Requests)
	errors := atomic.LoadInt64(&m.backendMetrics[backendID].Errors)
	if requests > 0 {
		m.backendMetrics[backendID].SuccessRate = float64(requests-errors) / float64(requests) * 100
	}
}

// RecordLatency records request latency for a backend
func (m *Metrics) RecordLatency(backendID string, duration time.Duration) {
	latencyMs := duration.Milliseconds()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Initialize backend metrics if not exists
	if m.backendMetrics[backendID] == nil {
		m.backendMetrics[backendID] = &BackendMetrics{
			MinLatency: latencyMs,
			MaxLatency: latencyMs,
		}
	}

	// Initialize latency buckets if not exists
	if m.latencyBuckets[backendID] == nil {
		m.latencyBuckets[backendID] = &LatencyBuckets{}
	}

	backend := m.backendMetrics[backendID]
	buckets := m.latencyBuckets[backendID]

	// Update total latency
	atomic.AddInt64(&backend.TotalLatency, latencyMs)

	// Update min/max latency
	if latencyMs < backend.MinLatency {
		backend.MinLatency = latencyMs
	}
	if latencyMs > backend.MaxLatency {
		backend.MaxLatency = latencyMs
	}

	// Update latency buckets
	switch {
	case latencyMs < 10:
		atomic.AddInt64(&buckets.Under10ms, 1)
	case latencyMs < 50:
		atomic.AddInt64(&buckets.Under50ms, 1)
	case latencyMs < 100:
		atomic.AddInt64(&buckets.Under100ms, 1)
	case latencyMs < 500:
		atomic.AddInt64(&buckets.Under500ms, 1)
	case latencyMs < 1000:
		atomic.AddInt64(&buckets.Under1000ms, 1)
	default:
		atomic.AddInt64(&buckets.Over1000ms, 1)
	}
}

// GetStats returns current statistics
func (m *Metrics) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Calculate overall success rate
	totalRequests := atomic.LoadInt64(&m.totalRequests)
	totalErrors := atomic.LoadInt64(&m.totalErrors)

	var overallSuccessRate float64
	if totalRequests > 0 {
		overallSuccessRate = float64(totalRequests-totalErrors) / float64(totalRequests) * 100
	}

	// Prepare backend stats
	backendStats := make(map[string]interface{})
	for backendID, metrics := range m.backendMetrics {
		requests := atomic.LoadInt64(&metrics.Requests)
		errors := atomic.LoadInt64(&metrics.Errors)
		totalLatency := atomic.LoadInt64(&metrics.TotalLatency)

		var avgLatency float64
		if requests > 0 {
			avgLatency = float64(totalLatency) / float64(requests)
		}

		backendStats[backendID] = map[string]interface{}{
			"requests":             requests,
			"errors":               errors,
			"success_rate":         metrics.SuccessRate,
			"avg_latency_ms":       avgLatency,
			"min_latency_ms":       metrics.MinLatency,
			"max_latency_ms":       metrics.MaxLatency,
			"last_request":         metrics.LastRequest,
			"latency_distribution": m.latencyBuckets[backendID],
		}
	}

	return map[string]interface{}{
		"total_requests":       totalRequests,
		"total_errors":         totalErrors,
		"overall_success_rate": overallSuccessRate,
		"backends":             backendStats,
		"uptime":               time.Now(), // This would be calculated from start time in production
	}
}

// GetBackendStats returns statistics for a specific backend
func (m *Metrics) GetBackendStats(backendID string) map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics, exists := m.backendMetrics[backendID]
	if !exists {
		return map[string]interface{}{
			"requests":       0,
			"errors":         0,
			"success_rate":   0.0,
			"avg_latency_ms": 0.0,
			"min_latency_ms": 0,
			"max_latency_ms": 0,
		}
	}

	requests := atomic.LoadInt64(&metrics.Requests)
	errors := atomic.LoadInt64(&metrics.Errors)
	totalLatency := atomic.LoadInt64(&metrics.TotalLatency)

	var avgLatency float64
	if requests > 0 {
		avgLatency = float64(totalLatency) / float64(requests)
	}

	return map[string]interface{}{
		"requests":             requests,
		"errors":               errors,
		"success_rate":         metrics.SuccessRate,
		"avg_latency_ms":       avgLatency,
		"min_latency_ms":       metrics.MinLatency,
		"max_latency_ms":       metrics.MaxLatency,
		"last_request":         metrics.LastRequest,
		"latency_distribution": m.latencyBuckets[backendID],
	}
}

// GetAllBackendStats returns statistics for all backends
func (m *Metrics) GetAllBackendStats() map[string]map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]map[string]interface{})

	for backendID, metrics := range m.backendMetrics {
		requests := atomic.LoadInt64(&metrics.Requests)
		errors := atomic.LoadInt64(&metrics.Errors)
		totalLatency := atomic.LoadInt64(&metrics.TotalLatency)

		var avgLatency float64
		if requests > 0 {
			avgLatency = float64(totalLatency) / float64(requests)
		}

		result[backendID] = map[string]interface{}{
			"requests":             requests,
			"errors":               errors,
			"success_rate":         metrics.SuccessRate,
			"avg_latency_ms":       avgLatency,
			"min_latency_ms":       metrics.MinLatency,
			"max_latency_ms":       metrics.MaxLatency,
			"last_request":         metrics.LastRequest,
			"latency_distribution": m.latencyBuckets[backendID],
		}
	}

	return result
}

// Reset resets all metrics
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	atomic.StoreInt64(&m.totalRequests, 0)
	atomic.StoreInt64(&m.totalErrors, 0)

	m.backendMetrics = make(map[string]*BackendMetrics)
	m.latencyBuckets = make(map[string]*LatencyBuckets)
}

// ResetBackend resets metrics for a specific backend
func (m *Metrics) ResetBackend(backendID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.backendMetrics, backendID)
	delete(m.latencyBuckets, backendID)
}

// GetTotalRequests returns the total number of requests processed
func (m *Metrics) GetTotalRequests() int64 {
	return atomic.LoadInt64(&m.totalRequests)
}

// GetTotalErrors returns the total number of errors encountered
func (m *Metrics) GetTotalErrors() int64 {
	return atomic.LoadInt64(&m.totalErrors)
}

// GetOverallSuccessRate returns the overall success rate
func (m *Metrics) GetOverallSuccessRate() float64 {
	totalRequests := atomic.LoadInt64(&m.totalRequests)
	totalErrors := atomic.LoadInt64(&m.totalErrors)

	if totalRequests == 0 {
		return 0.0
	}

	return float64(totalRequests-totalErrors) / float64(totalRequests) * 100
}
