package metrics

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
)

// HTTPMetrics holds the metrics collectors for HTTP request monitoring.
type HTTPMetrics struct {
	// RequestsTotal counts total HTTP requests by method, path, and status
	RequestsTotal *prometheus.CounterVec

	// RequestDuration observes request latency by method and path
	RequestDuration *prometheus.HistogramVec

	// RequestsInFlight tracks currently processing requests
	RequestsInFlight prometheus.Gauge

	// RequestSize observes request body size
	RequestSize *prometheus.HistogramVec

	// ResponseSize observes response body size
	ResponseSize *prometheus.HistogramVec
}

// HTTPMetricsConfig configures the HTTP metrics middleware.
type HTTPMetricsConfig struct {
	// Registry is the metrics registry to use. If nil, creates a new one.
	Registry *Registry

	// Namespace for metrics (e.g., "myservice")
	Namespace string

	// Subsystem for metrics (default: "http")
	Subsystem string

	// DurationBuckets for the request duration histogram
	DurationBuckets []float64

	// SizeBuckets for request/response size histograms
	SizeBuckets []float64

	// SkipPaths is a list of paths to skip metrics collection
	SkipPaths []string

	// PathTransformFunc transforms the request path before using it as a label.
	// This is useful for normalizing paths with parameters (e.g., /users/123 -> /users/:id)
	PathTransformFunc func(path string) string

	// IncludeRequestSize enables request size tracking
	IncludeRequestSize bool

	// IncludeResponseSize enables response size tracking
	IncludeResponseSize bool

	// IncludeRequestsInFlight enables in-flight request tracking
	IncludeRequestsInFlight bool
}

// DefaultHTTPMetricsConfig returns the default configuration for HTTP metrics.
func DefaultHTTPMetricsConfig() HTTPMetricsConfig {
	return HTTPMetricsConfig{
		Subsystem:               "http",
		DurationBuckets:         HTTPDurationBuckets(),
		SizeBuckets:             BytesBuckets(),
		IncludeRequestsInFlight: true,
	}
}

// NewHTTPMetrics creates a new HTTPMetrics with the given configuration.
func NewHTTPMetrics(cfg HTTPMetricsConfig) *HTTPMetrics {
	registry := cfg.Registry
	if registry == nil {
		registry = NewRegistryWithSubsystem(cfg.Namespace, cfg.Subsystem)
	}

	if cfg.DurationBuckets == nil {
		cfg.DurationBuckets = HTTPDurationBuckets()
	}

	if cfg.SizeBuckets == nil {
		cfg.SizeBuckets = BytesBuckets()
	}

	m := &HTTPMetrics{
		RequestsTotal: registry.Counter("requests_total").
			Help("Total number of HTTP requests").
			Labels("method", "path", "status").
			BuildVec(),

		RequestDuration: registry.Histogram("request_duration_seconds").
			Help("HTTP request duration in seconds").
			Labels("method", "path").
			Buckets(cfg.DurationBuckets).
			BuildVec(),
	}

	if cfg.IncludeRequestsInFlight {
		m.RequestsInFlight = registry.Gauge("requests_in_flight").
			Help("Number of HTTP requests currently being processed").
			Build()
	}

	if cfg.IncludeRequestSize {
		m.RequestSize = registry.Histogram("request_size_bytes").
			Help("HTTP request body size in bytes").
			Labels("method", "path").
			Buckets(cfg.SizeBuckets).
			BuildVec()
	}

	if cfg.IncludeResponseSize {
		m.ResponseSize = registry.Histogram("response_size_bytes").
			Help("HTTP response body size in bytes").
			Labels("method", "path").
			Buckets(cfg.SizeBuckets).
			BuildVec()
	}

	return m
}

// FiberMiddleware returns a Fiber middleware that collects HTTP metrics.
func (m *HTTPMetrics) FiberMiddleware(cfg HTTPMetricsConfig) fiber.Handler {
	skipPathMap := make(map[string]bool)
	for _, p := range cfg.SkipPaths {
		skipPathMap[p] = true
	}

	return func(c *fiber.Ctx) error {
		path := c.Path()

		// Skip metrics collection for specified paths
		if skipPathMap[path] {
			return c.Next()
		}

		// Transform path if a transform function is provided
		if cfg.PathTransformFunc != nil {
			path = cfg.PathTransformFunc(path)
		}

		method := c.Method()

		// Track in-flight requests
		if m.RequestsInFlight != nil {
			m.RequestsInFlight.Inc()
			defer m.RequestsInFlight.Dec()
		}

		// Record request size
		if m.RequestSize != nil {
			m.RequestSize.WithLabelValues(method, path).Observe(float64(len(c.Body())))
		}

		start := time.Now()

		// Process request
		err := c.Next()

		// Record metrics
		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Response().StatusCode())

		m.RequestsTotal.WithLabelValues(method, path, status).Inc()
		m.RequestDuration.WithLabelValues(method, path).Observe(duration)

		// Record response size
		if m.ResponseSize != nil {
			m.ResponseSize.WithLabelValues(method, path).Observe(float64(len(c.Response().Body())))
		}

		return err
	}
}

// NewFiberMiddleware creates a Fiber middleware with default configuration.
func NewFiberMiddleware(namespace string) fiber.Handler {
	cfg := DefaultHTTPMetricsConfig()
	cfg.Namespace = namespace
	m := NewHTTPMetrics(cfg)
	return m.FiberMiddleware(cfg)
}

// NewFiberMiddlewareWithConfig creates a Fiber middleware with custom configuration.
func NewFiberMiddlewareWithConfig(cfg HTTPMetricsConfig) fiber.Handler {
	m := NewHTTPMetrics(cfg)
	return m.FiberMiddleware(cfg)
}

// RecordRequest records a single HTTP request metric.
// This is useful for manual metric recording outside of middleware.
func (m *HTTPMetrics) RecordRequest(method, path, status string, duration time.Duration) {
	m.RequestsTotal.WithLabelValues(method, path, status).Inc()
	m.RequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

// RecordRequestWithSize records an HTTP request with size information.
func (m *HTTPMetrics) RecordRequestWithSize(method, path, status string, duration time.Duration, reqSize, respSize int) {
	m.RecordRequest(method, path, status, duration)

	if m.RequestSize != nil && reqSize > 0 {
		m.RequestSize.WithLabelValues(method, path).Observe(float64(reqSize))
	}

	if m.ResponseSize != nil && respSize > 0 {
		m.ResponseSize.WithLabelValues(method, path).Observe(float64(respSize))
	}
}
