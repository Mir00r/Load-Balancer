package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/mir00r/load-balancer/pkg/logger"
)

// ObservabilityMiddleware implements OpenTelemetry-style observability and distributed tracing
// Provides Traefik-style monitoring, tracing, and metrics collection
type ObservabilityMiddleware struct {
	config       ObservabilityConfig
	logger       *logger.Logger
	spanExporter SpanExporter
	metrics      MetricsCollector
}

// ObservabilityConfig contains observability configuration
type ObservabilityConfig struct {
	Enabled            bool                     `yaml:"enabled"`
	Tracing            TracingConfig            `yaml:"tracing"`
	Metrics            MetricsConfig            `yaml:"metrics"`
	Logging            LoggingConfig            `yaml:"logging"`
	DistributedTracing DistributedTracingConfig `yaml:"distributed_tracing"`
}

// TracingConfig configures distributed tracing
type TracingConfig struct {
	Enabled        bool              `yaml:"enabled"`
	ServiceName    string            `yaml:"service_name"`
	ServiceVersion string            `yaml:"service_version"`
	Environment    string            `yaml:"environment"`
	SampleRate     float64           `yaml:"sample_rate"` // 0.0 to 1.0
	MaxSpanEvents  int               `yaml:"max_span_events"`
	Exporters      []TracingExporter `yaml:"exporters"`
}

// TracingExporter defines tracing export configuration
type TracingExporter struct {
	Type     string                 `yaml:"type"` // "jaeger", "zipkin", "otlp"
	Endpoint string                 `yaml:"endpoint"`
	Headers  map[string]string      `yaml:"headers"`
	Config   map[string]interface{} `yaml:"config"`
}

// MetricsConfig configures metrics collection
type MetricsConfig struct {
	Enabled          bool              `yaml:"enabled"`
	Prefix           string            `yaml:"prefix"`
	Labels           map[string]string `yaml:"labels"`
	Exporters        []MetricsExporter `yaml:"exporters"`
	HistogramBuckets []float64         `yaml:"histogram_buckets"`
}

// MetricsExporter defines metrics export configuration
type MetricsExporter struct {
	Type     string                 `yaml:"type"` // "prometheus", "datadog", "influxdb"
	Endpoint string                 `yaml:"endpoint"`
	Interval time.Duration          `yaml:"interval"`
	Config   map[string]interface{} `yaml:"config"`
}

// LoggingConfig configures observability logging
type LoggingConfig struct {
	Enabled          bool              `yaml:"enabled"`
	StructuredFormat bool              `yaml:"structured_format"`
	IncludeTrace     bool              `yaml:"include_trace"`
	IncludeSpan      bool              `yaml:"include_span"`
	SensitiveHeaders []string          `yaml:"sensitive_headers"`
	Fields           map[string]string `yaml:"fields"`
}

// DistributedTracingConfig configures distributed tracing features
type DistributedTracingConfig struct {
	Enabled          bool              `yaml:"enabled"`
	TraceHeader      string            `yaml:"trace_header"`   // "X-Trace-Id"
	SpanHeader       string            `yaml:"span_header"`    // "X-Span-Id"
	BaggageHeader    string            `yaml:"baggage_header"` // "X-Baggage"
	PropagateHeaders []string          `yaml:"propagate_headers"`
	CustomTags       map[string]string `yaml:"custom_tags"`
}

// Span represents a tracing span
type Span struct {
	TraceID       string                 `json:"trace_id"`
	SpanID        string                 `json:"span_id"`
	ParentID      string                 `json:"parent_id,omitempty"`
	OperationName string                 `json:"operation_name"`
	StartTime     time.Time              `json:"start_time"`
	EndTime       time.Time              `json:"end_time"`
	Duration      time.Duration          `json:"duration"`
	Tags          map[string]interface{} `json:"tags"`
	Logs          []SpanLog              `json:"logs"`
	Status        SpanStatus             `json:"status"`
}

// SpanLog represents a span log entry
type SpanLog struct {
	Timestamp time.Time              `json:"timestamp"`
	Fields    map[string]interface{} `json:"fields"`
}

// SpanStatus represents span completion status
type SpanStatus struct {
	Code    int    `json:"code"` // 0 = OK, 1 = Error
	Message string `json:"message,omitempty"`
}

// SpanExporter interface for span export
type SpanExporter interface {
	ExportSpans(spans []Span) error
	Flush() error
	Shutdown() error
}

// MetricsCollector interface for metrics collection
type MetricsCollector interface {
	IncrementCounter(name string, tags map[string]string, value float64)
	RecordHistogram(name string, tags map[string]string, value float64)
	SetGauge(name string, tags map[string]string, value float64)
	Flush() error
}

// SimpleSpanExporter implements a simple in-memory span exporter
type SimpleSpanExporter struct {
	spans  []Span
	config TracingConfig
	logger *logger.Logger
}

// SimpleMetricsCollector implements a simple metrics collector
type SimpleMetricsCollector struct {
	metrics map[string]MetricValue
	config  MetricsConfig
	logger  *logger.Logger
}

// MetricValue represents a metric value with metadata
type MetricValue struct {
	Type      string            `json:"type"` // "counter", "histogram", "gauge"
	Value     float64           `json:"value"`
	Tags      map[string]string `json:"tags"`
	Timestamp time.Time         `json:"timestamp"`
	Samples   []float64         `json:"samples,omitempty"` // For histograms
}

// NewObservabilityMiddleware creates a new observability middleware
func NewObservabilityMiddleware(config ObservabilityConfig, logger *logger.Logger) (*ObservabilityMiddleware, error) {
	if !config.Enabled {
		return nil, nil
	}

	middleware := &ObservabilityMiddleware{
		config: config,
		logger: logger,
	}

	// Initialize span exporter
	if config.Tracing.Enabled {
		spanExporter := &SimpleSpanExporter{
			spans:  make([]Span, 0),
			config: config.Tracing,
			logger: logger,
		}
		middleware.spanExporter = spanExporter
	}

	// Initialize metrics collector
	if config.Metrics.Enabled {
		metricsCollector := &SimpleMetricsCollector{
			metrics: make(map[string]MetricValue),
			config:  config.Metrics,
			logger:  logger,
		}
		middleware.metrics = metricsCollector
	}

	logger.WithFields(map[string]interface{}{
		"tracing_enabled": config.Tracing.Enabled,
		"metrics_enabled": config.Metrics.Enabled,
		"logging_enabled": config.Logging.Enabled,
		"service_name":    config.Tracing.ServiceName,
		"sample_rate":     config.Tracing.SampleRate,
	}).Info("Observability middleware initialized")

	return middleware, nil
}

// Observability returns the observability middleware handler
func (om *ObservabilityMiddleware) Observability() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !om.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			startTime := time.Now()

			// Create or extract trace context
			ctx, span := om.startSpan(r.Context(), r)
			r = r.WithContext(ctx)

			// Create response recorder for metrics
			recorder := &ResponseRecorder{
				ResponseWriter: w,
				StatusCode:     http.StatusOK,
			}

			// Add tracing headers to response
			if om.config.DistributedTracing.Enabled && span != nil {
				recorder.Header().Set(om.config.DistributedTracing.TraceHeader, span.TraceID)
				recorder.Header().Set(om.config.DistributedTracing.SpanHeader, span.SpanID)
			}

			// Process request
			next.ServeHTTP(recorder, r)

			// Finish span and collect metrics
			duration := time.Since(startTime)
			om.finishSpan(span, recorder, duration)
			om.collectMetrics(r, recorder, duration)
			om.logRequest(r, recorder, duration, span)
		})
	}
}

// startSpan creates a new span or continues existing trace
func (om *ObservabilityMiddleware) startSpan(ctx context.Context, r *http.Request) (context.Context, *Span) {
	if !om.config.Tracing.Enabled {
		return ctx, nil
	}

	// Check if we should sample this request
	if !om.shouldSample() {
		return ctx, nil
	}

	// Extract trace context from headers
	traceID := r.Header.Get(om.config.DistributedTracing.TraceHeader)
	parentSpanID := r.Header.Get(om.config.DistributedTracing.SpanHeader)

	// Generate new trace ID if not present
	if traceID == "" {
		traceID = om.generateTraceID()
	}

	// Create new span
	span := &Span{
		TraceID:       traceID,
		SpanID:        om.generateSpanID(),
		ParentID:      parentSpanID,
		OperationName: fmt.Sprintf("HTTP %s %s", r.Method, r.URL.Path),
		StartTime:     time.Now(),
		Tags:          make(map[string]interface{}),
		Logs:          make([]SpanLog, 0),
	}

	// Add standard tags
	span.Tags["http.method"] = r.Method
	span.Tags["http.url"] = r.URL.String()
	span.Tags["http.user_agent"] = r.UserAgent()
	span.Tags["http.remote_addr"] = r.RemoteAddr
	span.Tags["service.name"] = om.config.Tracing.ServiceName
	span.Tags["service.version"] = om.config.Tracing.ServiceVersion
	span.Tags["environment"] = om.config.Tracing.Environment

	// Add custom tags
	for key, value := range om.config.DistributedTracing.CustomTags {
		span.Tags[key] = value
	}

	// Add request-specific tags
	if r.ContentLength > 0 {
		span.Tags["http.request.size"] = r.ContentLength
	}

	// Add baggage from headers
	if baggageHeader := om.config.DistributedTracing.BaggageHeader; baggageHeader != "" {
		if baggage := r.Header.Get(baggageHeader); baggage != "" {
			span.Tags["baggage"] = baggage
		}
	}

	return ctx, span
}

// finishSpan completes the span with response information
func (om *ObservabilityMiddleware) finishSpan(span *Span, recorder *ResponseRecorder, duration time.Duration) {
	if span == nil {
		return
	}

	span.EndTime = time.Now()
	span.Duration = duration

	// Add response tags
	span.Tags["http.status_code"] = recorder.StatusCode
	span.Tags["http.response.size"] = recorder.BytesWritten

	// Set span status based on HTTP status code
	if recorder.StatusCode >= 400 {
		span.Status.Code = 1 // Error
		span.Status.Message = http.StatusText(recorder.StatusCode)
		span.Tags["error"] = true
	} else {
		span.Status.Code = 0 // OK
	}

	// Add duration-based tags
	if duration > 5*time.Second {
		span.Tags["slow_request"] = true
	}

	// Export span
	if om.spanExporter != nil {
		om.spanExporter.ExportSpans([]Span{*span})
	}
}

// collectMetrics records metrics for the request
func (om *ObservabilityMiddleware) collectMetrics(r *http.Request, recorder *ResponseRecorder, duration time.Duration) {
	if !om.config.Metrics.Enabled || om.metrics == nil {
		return
	}

	// Create tags
	tags := map[string]string{
		"method":      r.Method,
		"status_code": strconv.Itoa(recorder.StatusCode),
		"handler":     "load_balancer",
	}

	// Add custom labels
	for key, value := range om.config.Metrics.Labels {
		tags[key] = value
	}

	// Record metrics
	om.metrics.IncrementCounter("http_requests_total", tags, 1)
	om.metrics.RecordHistogram("http_request_duration_seconds", tags, duration.Seconds())
	om.metrics.IncrementCounter("http_request_size_bytes", tags, float64(r.ContentLength))
	om.metrics.IncrementCounter("http_response_size_bytes", tags, float64(recorder.BytesWritten))

	// Record error metrics
	if recorder.StatusCode >= 400 {
		errorTags := map[string]string{
			"method":      r.Method,
			"status_code": strconv.Itoa(recorder.StatusCode),
		}
		om.metrics.IncrementCounter("http_errors_total", errorTags, 1)
	}
}

// logRequest logs the request with observability context
func (om *ObservabilityMiddleware) logRequest(r *http.Request, recorder *ResponseRecorder, duration time.Duration, span *Span) {
	if !om.config.Logging.Enabled {
		return
	}

	fields := map[string]interface{}{
		"method":        r.Method,
		"path":          r.URL.Path,
		"status_code":   recorder.StatusCode,
		"duration_ms":   duration.Milliseconds(),
		"remote_addr":   r.RemoteAddr,
		"user_agent":    r.UserAgent(),
		"request_size":  r.ContentLength,
		"response_size": recorder.BytesWritten,
	}

	// Add custom fields
	for key, value := range om.config.Logging.Fields {
		fields[key] = value
	}

	// Add trace context if available
	if om.config.Logging.IncludeTrace && span != nil {
		fields["trace_id"] = span.TraceID
		if om.config.Logging.IncludeSpan {
			fields["span_id"] = span.SpanID
			fields["parent_span_id"] = span.ParentID
		}
	}

	// Filter sensitive headers
	headers := make(map[string]string)
	for name, values := range r.Header {
		isSensitive := false
		for _, sensitive := range om.config.Logging.SensitiveHeaders {
			if name == sensitive {
				isSensitive = true
				break
			}
		}
		if !isSensitive && len(values) > 0 {
			headers[name] = values[0]
		}
	}
	fields["headers"] = headers

	// Log level based on status code
	if recorder.StatusCode >= 500 {
		om.logger.WithFields(fields).Error("HTTP request completed with server error")
	} else if recorder.StatusCode >= 400 {
		om.logger.WithFields(fields).Warn("HTTP request completed with client error")
	} else {
		om.logger.WithFields(fields).Info("HTTP request completed successfully")
	}
}

// Helper methods

func (om *ObservabilityMiddleware) shouldSample() bool {
	// Simple sampling based on rate
	// In production, use more sophisticated sampling strategies
	return om.config.Tracing.SampleRate >= 1.0 ||
		(om.config.Tracing.SampleRate > 0 &&
			time.Now().UnixNano()%1000 < int64(om.config.Tracing.SampleRate*1000))
}

func (om *ObservabilityMiddleware) generateTraceID() string {
	return fmt.Sprintf("trace_%d_%d", time.Now().UnixNano(), time.Now().UnixNano()%10000)
}

func (om *ObservabilityMiddleware) generateSpanID() string {
	return fmt.Sprintf("span_%d_%d", time.Now().UnixNano(), time.Now().UnixNano()%10000)
}

// ResponseRecorder captures response information
type ResponseRecorder struct {
	http.ResponseWriter
	StatusCode   int
	BytesWritten int64
}

func (rr *ResponseRecorder) WriteHeader(statusCode int) {
	rr.StatusCode = statusCode
	rr.ResponseWriter.WriteHeader(statusCode)
}

func (rr *ResponseRecorder) Write(data []byte) (int, error) {
	rr.BytesWritten += int64(len(data))
	return rr.ResponseWriter.Write(data)
}

// Implementation of SpanExporter interface

func (se *SimpleSpanExporter) ExportSpans(spans []Span) error {
	se.spans = append(se.spans, spans...)

	// Log spans for debugging
	for _, span := range spans {
		se.logger.WithFields(map[string]interface{}{
			"trace_id":       span.TraceID,
			"span_id":        span.SpanID,
			"operation_name": span.OperationName,
			"duration_ms":    span.Duration.Milliseconds(),
			"status_code":    span.Status.Code,
		}).Debug("Span exported")
	}

	return nil
}

func (se *SimpleSpanExporter) Flush() error {
	se.logger.WithField("spans_count", len(se.spans)).Debug("Span exporter flushed")
	return nil
}

func (se *SimpleSpanExporter) Shutdown() error {
	se.logger.Info("Span exporter shutdown")
	return nil
}

// Implementation of MetricsCollector interface

func (mc *SimpleMetricsCollector) IncrementCounter(name string, tags map[string]string, value float64) {
	key := mc.makeMetricKey(name, tags)
	if existing, ok := mc.metrics[key]; ok {
		existing.Value += value
		mc.metrics[key] = existing
	} else {
		mc.metrics[key] = MetricValue{
			Type:      "counter",
			Value:     value,
			Tags:      tags,
			Timestamp: time.Now(),
		}
	}
}

func (mc *SimpleMetricsCollector) RecordHistogram(name string, tags map[string]string, value float64) {
	key := mc.makeMetricKey(name, tags)
	if existing, ok := mc.metrics[key]; ok {
		existing.Samples = append(existing.Samples, value)
		mc.metrics[key] = existing
	} else {
		mc.metrics[key] = MetricValue{
			Type:      "histogram",
			Value:     value,
			Tags:      tags,
			Timestamp: time.Now(),
			Samples:   []float64{value},
		}
	}
}

func (mc *SimpleMetricsCollector) SetGauge(name string, tags map[string]string, value float64) {
	key := mc.makeMetricKey(name, tags)
	mc.metrics[key] = MetricValue{
		Type:      "gauge",
		Value:     value,
		Tags:      tags,
		Timestamp: time.Now(),
	}
}

func (mc *SimpleMetricsCollector) Flush() error {
	mc.logger.WithField("metrics_count", len(mc.metrics)).Debug("Metrics collector flushed")
	return nil
}

func (mc *SimpleMetricsCollector) makeMetricKey(name string, tags map[string]string) string {
	key := name
	for k, v := range tags {
		key += fmt.Sprintf(",%s=%s", k, v)
	}
	return key
}

// GetStats returns observability middleware statistics
func (om *ObservabilityMiddleware) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"enabled":         om.config.Enabled,
		"tracing_enabled": om.config.Tracing.Enabled,
		"metrics_enabled": om.config.Metrics.Enabled,
		"logging_enabled": om.config.Logging.Enabled,
		"service_name":    om.config.Tracing.ServiceName,
		"sample_rate":     om.config.Tracing.SampleRate,
	}

	// Add span statistics
	if se, ok := om.spanExporter.(*SimpleSpanExporter); ok {
		stats["spans_exported"] = len(se.spans)
	}

	// Add metrics statistics
	if mc, ok := om.metrics.(*SimpleMetricsCollector); ok {
		stats["metrics_collected"] = len(mc.metrics)
	}

	return stats
}
