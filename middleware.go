package metrics

import (
	"time"

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
	//
	// Nil means DefaultPathNormalize. It used to mean "use the raw path", but
	// a config built as a struct literal also leaves it nil, and a raw URL
	// path as a label value lets anyone requesting random URLs mint a new time
	// series per request. Use DisablePathNormalization to ask for raw paths.
	PathTransformFunc func(path string) string

	// DisablePathNormalization uses the raw request path as the label value.
	//
	// This is the explicit spelling of what PathTransformFunc: nil used to
	// mean. Only safe where every path is already bounded -- a router that
	// reports its route pattern rather than the request URL, say. Otherwise
	// each distinct ID becomes its own time series.
	DisablePathNormalization bool

	// IncludeRequestSize enables request size tracking
	IncludeRequestSize bool

	// IncludeResponseSize enables response size tracking
	IncludeResponseSize bool

	// IncludeRequestsInFlight enables in-flight request tracking
	IncludeRequestsInFlight bool
}

// DefaultHTTPMetricsConfig returns the default configuration for HTTP metrics.
// PathTransformFunc is set to DefaultPathNormalize to avoid label cardinality
// explosion in production; set a custom function for different normalization,
// or DisablePathNormalization for raw paths.
func DefaultHTTPMetricsConfig() HTTPMetricsConfig {
	return HTTPMetricsConfig{
		Subsystem:               "http",
		DurationBuckets:         HTTPDurationBuckets(),
		SizeBuckets:             BytesBuckets(),
		IncludeRequestsInFlight: true,
		PathTransformFunc:       DefaultPathNormalize,
	}
}

// TransformPath applies this config's path normalization: SkipPaths are the
// caller's business, but everything else -- a custom PathTransformFunc, or
// DefaultPathNormalize collapsing /users/123 to /users/:id -- runs here.
//
// A nil PathTransformFunc means DefaultPathNormalize. It cannot also mean
// "raw path": a config built as a struct literal leaves the field nil without
// intending anything by it, and a raw URL path as a label value lets anyone
// requesting random URLs mint a new time series per request. Raw paths are
// spelled DisablePathNormalization.
//
// Exported so a framework adapter normalizes labels exactly the way the rest
// of the package does; label cardinality is the whole point of this step, and
// an adapter that got it subtly wrong would blow up the series count.
func (c HTTPMetricsConfig) TransformPath(path string) string {
	if c.DisablePathNormalization {
		return path
	}
	if c.PathTransformFunc != nil {
		return c.PathTransformFunc(path)
	}
	return DefaultPathNormalize(path)
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

// RecordRequest records a single HTTP request metric.
// This is useful for manual metric recording outside of middleware.
// Method, path, and status are sanitized for safe use as label values.
func (m *HTTPMetrics) RecordRequest(method, path, status string, duration time.Duration) {
	method = SanitizeLabelValue(method, DefaultLabelValueMaxLength)
	path = SanitizeLabelValue(path, DefaultLabelValueMaxLength)
	status = SanitizeLabelValue(status, DefaultLabelValueMaxLength)
	m.RequestsTotal.WithLabelValues(method, path, status).Inc()
	m.RequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

// RecordRequestWithSize records an HTTP request with size information.
func (m *HTTPMetrics) RecordRequestWithSize(method, path, status string, duration time.Duration, reqSize, respSize int) {
	method = SanitizeLabelValue(method, DefaultLabelValueMaxLength)
	path = SanitizeLabelValue(path, DefaultLabelValueMaxLength)
	m.RecordRequest(method, path, status, duration)

	if m.RequestSize != nil && reqSize > 0 {
		m.RequestSize.WithLabelValues(method, path).Observe(float64(reqSize))
	}

	if m.ResponseSize != nil && respSize > 0 {
		m.ResponseSize.WithLabelValues(method, path).Observe(float64(respSize))
	}
}
