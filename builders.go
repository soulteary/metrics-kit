package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// CounterBuilder provides a fluent interface for building Counter metrics.
type CounterBuilder struct {
	registry    *Registry
	name        string
	help        string
	labels      []string
	constLabels prometheus.Labels
}

// HistogramBuilder provides a fluent interface for building Histogram metrics.
type HistogramBuilder struct {
	registry    *Registry
	name        string
	help        string
	labels      []string
	buckets     []float64
	constLabels prometheus.Labels
}

// GaugeBuilder provides a fluent interface for building Gauge metrics.
type GaugeBuilder struct {
	registry    *Registry
	name        string
	help        string
	labels      []string
	constLabels prometheus.Labels
}

// SummaryBuilder provides a fluent interface for building Summary metrics.
type SummaryBuilder struct {
	registry    *Registry
	name        string
	help        string
	labels      []string
	objectives  map[float64]float64
	constLabels prometheus.Labels
}

// Counter creates a new CounterBuilder.
func (r *Registry) Counter(name string) *CounterBuilder {
	return &CounterBuilder{
		registry: r,
		name:     name,
	}
}

// Histogram creates a new HistogramBuilder.
func (r *Registry) Histogram(name string) *HistogramBuilder {
	return &HistogramBuilder{
		registry: r,
		name:     name,
		buckets:  prometheus.DefBuckets,
	}
}

// Gauge creates a new GaugeBuilder.
func (r *Registry) Gauge(name string) *GaugeBuilder {
	return &GaugeBuilder{
		registry: r,
		name:     name,
	}
}

// Summary creates a new SummaryBuilder.
func (r *Registry) Summary(name string) *SummaryBuilder {
	return &SummaryBuilder{
		registry: r,
		name:     name,
	}
}

// --- CounterBuilder methods ---

// Help sets the help text for the counter.
func (b *CounterBuilder) Help(help string) *CounterBuilder {
	b.help = help
	return b
}

// Labels sets the label names for the counter.
func (b *CounterBuilder) Labels(labels ...string) *CounterBuilder {
	b.labels = labels
	return b
}

// ConstLabels sets constant labels for the counter.
func (b *CounterBuilder) ConstLabels(labels prometheus.Labels) *CounterBuilder {
	b.constLabels = labels
	return b
}

// Build creates and registers a Counter (without labels).
func (b *CounterBuilder) Build() prometheus.Counter {
	opts := prometheus.CounterOpts{
		Namespace:   b.registry.namespace,
		Subsystem:   b.registry.subsystem,
		Name:        b.name,
		Help:        b.help,
		ConstLabels: b.constLabels,
	}
	counter := prometheus.NewCounter(opts)
	return b.registry.registerOrExisting(counter, b.registry.metricID(b.name, nil), labelShape(nil)).(prometheus.Counter)
}

// BuildVec creates and registers a CounterVec (with labels).
func (b *CounterBuilder) BuildVec() *prometheus.CounterVec {
	opts := prometheus.CounterOpts{
		Namespace:   b.registry.namespace,
		Subsystem:   b.registry.subsystem,
		Name:        b.name,
		Help:        b.help,
		ConstLabels: b.constLabels,
	}
	counterVec := prometheus.NewCounterVec(opts, b.labels)
	return b.registry.registerOrExisting(counterVec, b.registry.metricID(b.name, b.labels), labelShape(b.labels)).(*prometheus.CounterVec)
}

// --- HistogramBuilder methods ---

// Help sets the help text for the histogram.
func (b *HistogramBuilder) Help(help string) *HistogramBuilder {
	b.help = help
	return b
}

// Labels sets the label names for the histogram.
func (b *HistogramBuilder) Labels(labels ...string) *HistogramBuilder {
	b.labels = labels
	return b
}

// Buckets sets the bucket boundaries for the histogram.
func (b *HistogramBuilder) Buckets(buckets []float64) *HistogramBuilder {
	b.buckets = buckets
	return b
}

// ConstLabels sets constant labels for the histogram.
func (b *HistogramBuilder) ConstLabels(labels prometheus.Labels) *HistogramBuilder {
	b.constLabels = labels
	return b
}

// Build creates and registers a Histogram (without labels).
func (b *HistogramBuilder) Build() prometheus.Histogram {
	opts := prometheus.HistogramOpts{
		Namespace:   b.registry.namespace,
		Subsystem:   b.registry.subsystem,
		Name:        b.name,
		Help:        b.help,
		Buckets:     b.buckets,
		ConstLabels: b.constLabels,
	}
	histogram := prometheus.NewHistogram(opts)
	return b.registry.registerOrExisting(histogram, b.registry.metricID(b.name, nil), labelShape(nil)+";"+bucketShape(b.buckets)).(prometheus.Histogram)
}

// BuildVec creates and registers a HistogramVec (with labels).
func (b *HistogramBuilder) BuildVec() *prometheus.HistogramVec {
	opts := prometheus.HistogramOpts{
		Namespace:   b.registry.namespace,
		Subsystem:   b.registry.subsystem,
		Name:        b.name,
		Help:        b.help,
		Buckets:     b.buckets,
		ConstLabels: b.constLabels,
	}
	histogramVec := prometheus.NewHistogramVec(opts, b.labels)
	return b.registry.registerOrExisting(histogramVec, b.registry.metricID(b.name, b.labels), labelShape(b.labels)+";"+bucketShape(b.buckets)).(*prometheus.HistogramVec)
}

// --- GaugeBuilder methods ---

// Help sets the help text for the gauge.
func (b *GaugeBuilder) Help(help string) *GaugeBuilder {
	b.help = help
	return b
}

// Labels sets the label names for the gauge.
func (b *GaugeBuilder) Labels(labels ...string) *GaugeBuilder {
	b.labels = labels
	return b
}

// ConstLabels sets constant labels for the gauge.
func (b *GaugeBuilder) ConstLabels(labels prometheus.Labels) *GaugeBuilder {
	b.constLabels = labels
	return b
}

// Build creates and registers a Gauge (without labels).
func (b *GaugeBuilder) Build() prometheus.Gauge {
	opts := prometheus.GaugeOpts{
		Namespace:   b.registry.namespace,
		Subsystem:   b.registry.subsystem,
		Name:        b.name,
		Help:        b.help,
		ConstLabels: b.constLabels,
	}
	gauge := prometheus.NewGauge(opts)
	return b.registry.registerOrExisting(gauge, b.registry.metricID(b.name, nil), labelShape(nil)).(prometheus.Gauge)
}

// BuildVec creates and registers a GaugeVec (with labels).
func (b *GaugeBuilder) BuildVec() *prometheus.GaugeVec {
	opts := prometheus.GaugeOpts{
		Namespace:   b.registry.namespace,
		Subsystem:   b.registry.subsystem,
		Name:        b.name,
		Help:        b.help,
		ConstLabels: b.constLabels,
	}
	gaugeVec := prometheus.NewGaugeVec(opts, b.labels)
	return b.registry.registerOrExisting(gaugeVec, b.registry.metricID(b.name, b.labels), labelShape(b.labels)).(*prometheus.GaugeVec)
}

// --- SummaryBuilder methods ---

// Help sets the help text for the summary.
func (b *SummaryBuilder) Help(help string) *SummaryBuilder {
	b.help = help
	return b
}

// Labels sets the label names for the summary.
func (b *SummaryBuilder) Labels(labels ...string) *SummaryBuilder {
	b.labels = labels
	return b
}

// Objectives sets the quantile objectives for the summary.
func (b *SummaryBuilder) Objectives(objectives map[float64]float64) *SummaryBuilder {
	b.objectives = objectives
	return b
}

// ConstLabels sets constant labels for the summary.
func (b *SummaryBuilder) ConstLabels(labels prometheus.Labels) *SummaryBuilder {
	b.constLabels = labels
	return b
}

// Build creates and registers a Summary (without labels).
func (b *SummaryBuilder) Build() prometheus.Summary {
	opts := prometheus.SummaryOpts{
		Namespace:   b.registry.namespace,
		Subsystem:   b.registry.subsystem,
		Name:        b.name,
		Help:        b.help,
		Objectives:  b.objectives,
		ConstLabels: b.constLabels,
	}
	summary := prometheus.NewSummary(opts)
	return b.registry.registerOrExisting(summary, b.registry.metricID(b.name, nil), labelShape(nil)+";"+objectiveShape(b.objectives)).(prometheus.Summary)
}

// BuildVec creates and registers a SummaryVec (with labels).
func (b *SummaryBuilder) BuildVec() *prometheus.SummaryVec {
	opts := prometheus.SummaryOpts{
		Namespace:   b.registry.namespace,
		Subsystem:   b.registry.subsystem,
		Name:        b.name,
		Help:        b.help,
		Objectives:  b.objectives,
		ConstLabels: b.constLabels,
	}
	summaryVec := prometheus.NewSummaryVec(opts, b.labels)
	return b.registry.registerOrExisting(summaryVec, b.registry.metricID(b.name, b.labels), labelShape(b.labels)+";"+objectiveShape(b.objectives)).(*prometheus.SummaryVec)
}

// --- Predefined bucket configurations ---

// DefaultBuckets returns the default Prometheus histogram buckets.
func DefaultBuckets() []float64 {
	return prometheus.DefBuckets
}

// HTTPDurationBuckets returns buckets suitable for HTTP request duration tracking.
// Values: 1ms, 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s
func HTTPDurationBuckets() []float64 {
	return []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0}
}

// RedisDurationBuckets returns buckets suitable for Redis operation duration tracking.
// Values: 0.5ms, 1ms, 2.5ms, 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s
func RedisDurationBuckets() []float64 {
	return []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0}
}

// ExternalAPIDurationBuckets returns buckets suitable for external API call duration tracking.
// Values: 10ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s, 30s
func ExternalAPIDurationBuckets() []float64 {
	return []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0}
}

// BytesBuckets returns buckets suitable for request/response size tracking.
// Values: 100B, 1KB, 10KB, 100KB, 1MB, 10MB
func BytesBuckets() []float64 {
	return []float64{100, 1024, 10240, 102400, 1048576, 10485760}
}
