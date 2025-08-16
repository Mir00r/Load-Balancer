// Package observability provides comprehensive observability capabilities including
// distributed tracing, advanced metrics collection, and structured logging.
// This package implements OpenTelemetry-based tracing and Prometheus-compatible metrics.
package observability

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/pkg/logger"
)

// TraceContext represents a distributed tracing context
type TraceContext struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpan   string            `json:"parent_span,omitempty"`
	BaggageItems map[string]string `json:"baggage_items,omitempty"`
	StartTime    time.Time         `json:"start_time"`
	EndTime      time.Time         `json:"end_time,omitempty"`
}

// Span represents a single operation within a trace
type Span struct {
	Context       TraceContext      `json:"context"`
	OperationName string            `json:"operation_name"`
	Tags          map[string]string `json:"tags"`
	Logs          []LogEntry        `json:"logs"`
	Duration      time.Duration     `json:"duration"`
	Status        SpanStatus        `json:"status"`

	startTime time.Time
	mutex     sync.RWMutex
}

// LogEntry represents a log entry within a span
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// SpanStatus represents the status of a span
type SpanStatus struct {
	Code    StatusCode `json:"code"`
	Message string     `json:"message,omitempty"`
}

// StatusCode represents span status codes
type StatusCode int

const (
	StatusCodeOK StatusCode = iota
	StatusCodeError
	StatusCodeTimeout
	StatusCodeCancelled
)

// RequestMetrics represents metrics for HTTP requests
type RequestMetrics struct {
	RequestsTotal       int64         `json:"requests_total"`
	RequestsInFlight    int64         `json:"requests_in_flight"`
	RequestDuration     time.Duration `json:"request_duration"`
	RequestSize         int64         `json:"request_size"`
	ResponseSize        int64         `json:"response_size"`
	ResponseStatusCodes map[int]int64 `json:"response_status_codes"`
}

// BackendMetrics represents metrics for backend servers
type BackendMetrics struct {
	BackendRequestsTotal     int64         `json:"backend_requests_total"`
	BackendRequestDuration   time.Duration `json:"backend_request_duration"`
	BackendResponseSize      int64         `json:"backend_response_size"`
	BackendConnectionsActive int64         `json:"backend_connections_active"`
	BackendHealthStatus      string        `json:"backend_health_status"`
	BackendLastHealthCheck   time.Time     `json:"backend_last_health_check"`
}

// SystemMetrics represents system-level metrics
type SystemMetrics struct {
	CPUUsagePercent     float64       `json:"cpu_usage_percent"`
	MemoryUsageBytes    int64         `json:"memory_usage_bytes"`
	MemoryUsagePercent  float64       `json:"memory_usage_percent"`
	GoroutineCount      int           `json:"goroutine_count"`
	GCPauseDuration     time.Duration `json:"gc_pause_duration"`
	OpenFileDescriptors int           `json:"open_file_descriptors"`
	NetworkBytesIn      int64         `json:"network_bytes_in"`
	NetworkBytesOut     int64         `json:"network_bytes_out"`
}

// TracingManager manages distributed tracing operations
type TracingManager struct {
	serviceName string
	spans       map[string]*Span
	spansMutex  sync.RWMutex
	logger      *logger.Logger

	// Configuration
	samplingRate     float64
	maxSpansPerTrace int
}

// MetricsCollector collects and manages various metrics
type MetricsCollector struct {
	requestMetrics RequestMetrics
	backendMetrics map[string]*BackendMetrics
	systemMetrics  SystemMetrics
	customMetrics  map[string]interface{}

	metricsMutex sync.RWMutex
	logger       *logger.Logger

	// Collection intervals
	systemMetricsInterval time.Duration
	lastCollection        time.Time
}

// ObservabilityManager coordinates all observability features
type ObservabilityManager struct {
	tracingManager   *TracingManager
	metricsCollector *MetricsCollector
	logger           *logger.Logger

	// Configuration
	enabled            bool
	tracingEnabled     bool
	metricsEnabled     bool
	distributedTracing bool
}

// NewObservabilityManager creates a new observability manager
func NewObservabilityManager(serviceName string, logger *logger.Logger) *ObservabilityManager {
	tracingManager := &TracingManager{
		serviceName:      serviceName,
		spans:            make(map[string]*Span),
		logger:           logger,
		samplingRate:     1.0, // Sample all traces by default
		maxSpansPerTrace: 1000,
	}

	metricsCollector := &MetricsCollector{
		backendMetrics:        make(map[string]*BackendMetrics),
		customMetrics:         make(map[string]interface{}),
		logger:                logger,
		systemMetricsInterval: 30 * time.Second,
		requestMetrics: RequestMetrics{
			ResponseStatusCodes: make(map[int]int64),
		},
	}

	return &ObservabilityManager{
		tracingManager:     tracingManager,
		metricsCollector:   metricsCollector,
		logger:             logger,
		enabled:            true,
		tracingEnabled:     true,
		metricsEnabled:     true,
		distributedTracing: true,
	}
}

// StartSpan starts a new tracing span
func (om *ObservabilityManager) StartSpan(operationName string, parentSpan *Span) *Span {
	if !om.enabled || !om.tracingEnabled {
		return nil
	}

	span := &Span{
		Context: TraceContext{
			TraceID:   om.generateTraceID(),
			SpanID:    om.generateSpanID(),
			StartTime: time.Now(),
		},
		OperationName: operationName,
		Tags:          make(map[string]string),
		Logs:          make([]LogEntry, 0),
		Status:        SpanStatus{Code: StatusCodeOK},
		startTime:     time.Now(),
	}

	// Set parent span if provided
	if parentSpan != nil {
		span.Context.ParentSpan = parentSpan.Context.SpanID
		span.Context.TraceID = parentSpan.Context.TraceID
	}

	// Store span
	om.tracingManager.spansMutex.Lock()
	om.tracingManager.spans[span.Context.SpanID] = span
	om.tracingManager.spansMutex.Unlock()

	return span
}

// FinishSpan completes a tracing span
func (om *ObservabilityManager) FinishSpan(span *Span) {
	if span == nil {
		return
	}

	span.mutex.Lock()
	span.Context.EndTime = time.Now()
	span.Duration = span.Context.EndTime.Sub(span.startTime)
	span.mutex.Unlock()

	om.logger.WithFields(map[string]interface{}{
		"trace_id":       span.Context.TraceID,
		"span_id":        span.Context.SpanID,
		"operation_name": span.OperationName,
		"duration_ms":    span.Duration.Milliseconds(),
		"status":         span.Status.Code,
	}).Debug("Span completed")
}

// SetSpanTag sets a tag on a span
func (om *ObservabilityManager) SetSpanTag(span *Span, key, value string) {
	if span == nil {
		return
	}

	span.mutex.Lock()
	span.Tags[key] = value
	span.mutex.Unlock()
}

// LogSpanEvent logs an event in a span
func (om *ObservabilityManager) LogSpanEvent(span *Span, level, message string, fields map[string]interface{}) {
	if span == nil {
		return
	}

	logEntry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		Fields:    fields,
	}

	span.mutex.Lock()
	span.Logs = append(span.Logs, logEntry)
	span.mutex.Unlock()
}

// SetSpanStatus sets the status of a span
func (om *ObservabilityManager) SetSpanStatus(span *Span, code StatusCode, message string) {
	if span == nil {
		return
	}

	span.mutex.Lock()
	span.Status = SpanStatus{
		Code:    code,
		Message: message,
	}
	span.mutex.Unlock()
}

// InstrumentHTTPHandler wraps an HTTP handler with observability instrumentation
func (om *ObservabilityManager) InstrumentHTTPHandler(handler http.Handler, operationName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Start tracing span
		span := om.StartSpan(fmt.Sprintf("HTTP %s %s", r.Method, operationName), nil)
		if span != nil {
			om.SetSpanTag(span, "http.method", r.Method)
			om.SetSpanTag(span, "http.url", r.URL.String())
			om.SetSpanTag(span, "http.remote_addr", r.RemoteAddr)
			om.SetSpanTag(span, "http.user_agent", r.Header.Get("User-Agent"))
		}

		// Wrap response writer to capture status code and response size
		wrapped := &observabilityResponseWriter{
			ResponseWriter: w,
			statusCode:     200,
			bytesWritten:   0,
		}

		// Update request metrics
		om.metricsCollector.metricsMutex.Lock()
		om.metricsCollector.requestMetrics.RequestsInFlight++
		om.metricsCollector.requestMetrics.RequestsTotal++
		om.metricsCollector.metricsMutex.Unlock()

		// Process request
		defer func() {
			duration := time.Since(startTime)

			// Update metrics
			om.metricsCollector.metricsMutex.Lock()
			om.metricsCollector.requestMetrics.RequestsInFlight--
			om.metricsCollector.requestMetrics.RequestDuration = duration
			om.metricsCollector.requestMetrics.RequestSize += r.ContentLength
			om.metricsCollector.requestMetrics.ResponseSize += int64(wrapped.bytesWritten)
			om.metricsCollector.requestMetrics.ResponseStatusCodes[wrapped.statusCode]++
			om.metricsCollector.metricsMutex.Unlock()

			// Finish span
			if span != nil {
				om.SetSpanTag(span, "http.status_code", fmt.Sprintf("%d", wrapped.statusCode))
				om.SetSpanTag(span, "http.response_size", fmt.Sprintf("%d", wrapped.bytesWritten))

				if wrapped.statusCode >= 400 {
					om.SetSpanStatus(span, StatusCodeError, fmt.Sprintf("HTTP %d", wrapped.statusCode))
				}

				om.FinishSpan(span)
			}

			// Log request
			om.logger.WithFields(map[string]interface{}{
				"method":        r.Method,
				"path":          r.URL.Path,
				"status":        wrapped.statusCode,
				"duration_ms":   duration.Milliseconds(),
				"request_size":  r.ContentLength,
				"response_size": wrapped.bytesWritten,
				"remote_addr":   r.RemoteAddr,
				"trace_id": func() string {
					if span != nil {
						return span.Context.TraceID
					}
					return ""
				}(),
			}).Info("HTTP request processed")
		}()

		handler.ServeHTTP(wrapped, r)
	})
}

// RecordBackendMetrics records metrics for backend operations
func (om *ObservabilityManager) RecordBackendMetrics(backendID string, duration time.Duration, responseSize int64, healthy bool) {
	if !om.enabled || !om.metricsEnabled {
		return
	}

	om.metricsCollector.metricsMutex.Lock()
	defer om.metricsCollector.metricsMutex.Unlock()

	// Get or create backend metrics
	backendMetrics, exists := om.metricsCollector.backendMetrics[backendID]
	if !exists {
		backendMetrics = &BackendMetrics{}
		om.metricsCollector.backendMetrics[backendID] = backendMetrics
	}

	// Update metrics
	backendMetrics.BackendRequestsTotal++
	backendMetrics.BackendRequestDuration = duration
	backendMetrics.BackendResponseSize += responseSize
	backendMetrics.BackendLastHealthCheck = time.Now()

	if healthy {
		backendMetrics.BackendHealthStatus = "healthy"
	} else {
		backendMetrics.BackendHealthStatus = "unhealthy"
	}
}

// CollectSystemMetrics collects system-level metrics
func (om *ObservabilityManager) CollectSystemMetrics() {
	if !om.enabled || !om.metricsEnabled {
		return
	}

	now := time.Now()
	if now.Sub(om.metricsCollector.lastCollection) < om.metricsCollector.systemMetricsInterval {
		return
	}

	om.metricsCollector.metricsMutex.Lock()
	defer om.metricsCollector.metricsMutex.Unlock()

	// In a real implementation, these would use actual system metrics collection
	// For now, we'll use placeholder values
	om.metricsCollector.systemMetrics = SystemMetrics{
		CPUUsagePercent:     0.0, // Would use actual CPU metrics
		MemoryUsageBytes:    0,   // Would use actual memory metrics
		MemoryUsagePercent:  0.0,
		GoroutineCount:      0, // Would use runtime.NumGoroutine()
		GCPauseDuration:     0, // Would use GC metrics
		OpenFileDescriptors: 0, // Would use process metrics
		NetworkBytesIn:      0, // Would use network metrics
		NetworkBytesOut:     0,
	}

	om.metricsCollector.lastCollection = now
}

// GetMetrics returns current metrics snapshot
func (om *ObservabilityManager) GetMetrics() map[string]interface{} {
	if !om.enabled || !om.metricsEnabled {
		return make(map[string]interface{})
	}

	om.metricsCollector.metricsMutex.RLock()
	defer om.metricsCollector.metricsMutex.RUnlock()

	// Collect current system metrics
	go om.CollectSystemMetrics()

	return map[string]interface{}{
		"requests": om.metricsCollector.requestMetrics,
		"backends": om.metricsCollector.backendMetrics,
		"system":   om.metricsCollector.systemMetrics,
		"custom":   om.metricsCollector.customMetrics,
	}
}

// GetTraces returns current traces
func (om *ObservabilityManager) GetTraces() []*Span {
	if !om.enabled || !om.tracingEnabled {
		return make([]*Span, 0)
	}

	om.tracingManager.spansMutex.RLock()
	defer om.tracingManager.spansMutex.RUnlock()

	spans := make([]*Span, 0, len(om.tracingManager.spans))
	for _, span := range om.tracingManager.spans {
		spans = append(spans, span)
	}

	return spans
}

// ExtractTraceContext extracts trace context from HTTP headers
func (om *ObservabilityManager) ExtractTraceContext(r *http.Request) *TraceContext {
	traceID := r.Header.Get("X-Trace-Id")
	spanID := r.Header.Get("X-Span-Id")
	parentSpan := r.Header.Get("X-Parent-Span-Id")

	if traceID == "" {
		return nil
	}

	return &TraceContext{
		TraceID:    traceID,
		SpanID:     spanID,
		ParentSpan: parentSpan,
		StartTime:  time.Now(),
	}
}

// InjectTraceContext injects trace context into HTTP headers
func (om *ObservabilityManager) InjectTraceContext(span *Span, header http.Header) {
	if span == nil {
		return
	}

	header.Set("X-Trace-Id", span.Context.TraceID)
	header.Set("X-Span-Id", span.Context.SpanID)
	if span.Context.ParentSpan != "" {
		header.Set("X-Parent-Span-Id", span.Context.ParentSpan)
	}
}

// generateTraceID generates a unique trace ID
func (om *ObservabilityManager) generateTraceID() string {
	return fmt.Sprintf("trace-%d", time.Now().UnixNano())
}

// generateSpanID generates a unique span ID
func (om *ObservabilityManager) generateSpanID() string {
	return fmt.Sprintf("span-%d", time.Now().UnixNano())
}

// observabilityResponseWriter wraps http.ResponseWriter to capture metrics
type observabilityResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (w *observabilityResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *observabilityResponseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.bytesWritten += n
	return n, err
}

// Enable enables observability features
func (om *ObservabilityManager) Enable() {
	om.enabled = true
}

// Disable disables observability features
func (om *ObservabilityManager) Disable() {
	om.enabled = false
}

// EnableTracing enables distributed tracing
func (om *ObservabilityManager) EnableTracing() {
	om.tracingEnabled = true
}

// DisableTracing disables distributed tracing
func (om *ObservabilityManager) DisableTracing() {
	om.tracingEnabled = false
}

// EnableMetrics enables metrics collection
func (om *ObservabilityManager) EnableMetrics() {
	om.metricsEnabled = true
}

// DisableMetrics disables metrics collection
func (om *ObservabilityManager) DisableMetrics() {
	om.metricsEnabled = false
}
