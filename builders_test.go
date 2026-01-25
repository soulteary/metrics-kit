package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCounterBuilder(t *testing.T) {
	r := NewRegistry("test_counter")

	// Test building a simple counter
	counter := r.Counter("simple_counter").
		Help("A simple counter").
		Build()

	require.NotNil(t, counter)
	counter.Inc()

	metric := &dto.Metric{}
	err := counter.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())
}

func TestCounterBuilder_Vec(t *testing.T) {
	r := NewRegistry("test_counter_vec")

	// Test building a counter vec
	counterVec := r.Counter("labeled_counter").
		Help("A labeled counter").
		Labels("method", "status").
		BuildVec()

	require.NotNil(t, counterVec)

	counterVec.WithLabelValues("GET", "200").Inc()
	counterVec.WithLabelValues("POST", "201").Add(5)

	metric := &dto.Metric{}
	err := counterVec.WithLabelValues("GET", "200").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	metric2 := &dto.Metric{}
	err = counterVec.WithLabelValues("POST", "201").Write(metric2)
	require.NoError(t, err)
	assert.Equal(t, 5.0, metric2.Counter.GetValue())
}

func TestCounterBuilder_ConstLabels(t *testing.T) {
	r := NewRegistry("test_counter_const")

	counter := r.Counter("const_labeled_counter").
		Help("A counter with const labels").
		ConstLabels(prometheus.Labels{"service": "myapp"}).
		Build()

	require.NotNil(t, counter)
	counter.Inc()

	metric := &dto.Metric{}
	err := counter.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())
}

func TestHistogramBuilder(t *testing.T) {
	r := NewRegistry("test_histogram")

	// Test building a simple histogram
	histogram := r.Histogram("simple_histogram").
		Help("A simple histogram").
		Buckets([]float64{0.1, 0.5, 1.0, 5.0}).
		Build()

	require.NotNil(t, histogram)
	histogram.Observe(0.3)
	histogram.Observe(0.7)
	histogram.Observe(2.0)
}

func TestHistogramBuilder_Vec(t *testing.T) {
	r := NewRegistry("test_histogram_vec")

	// Test building a histogram vec
	histogramVec := r.Histogram("labeled_histogram").
		Help("A labeled histogram").
		Labels("operation").
		Buckets(HTTPDurationBuckets()).
		BuildVec()

	require.NotNil(t, histogramVec)

	histogramVec.WithLabelValues("read").Observe(0.05)
	histogramVec.WithLabelValues("write").Observe(0.1)
}

func TestHistogramBuilder_DefaultBuckets(t *testing.T) {
	r := NewRegistry("test_histogram_default")

	// Test with default buckets
	histogram := r.Histogram("default_buckets_histogram").
		Help("A histogram with default buckets").
		Build()

	require.NotNil(t, histogram)
	histogram.Observe(0.5)
}

func TestGaugeBuilder(t *testing.T) {
	r := NewRegistry("test_gauge")

	// Test building a simple gauge
	gauge := r.Gauge("simple_gauge").
		Help("A simple gauge").
		Build()

	require.NotNil(t, gauge)
	gauge.Set(42)

	metric := &dto.Metric{}
	err := gauge.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 42.0, metric.Gauge.GetValue())

	gauge.Inc()
	err = gauge.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 43.0, metric.Gauge.GetValue())

	gauge.Dec()
	err = gauge.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 42.0, metric.Gauge.GetValue())
}

func TestGaugeBuilder_Vec(t *testing.T) {
	r := NewRegistry("test_gauge_vec")

	// Test building a gauge vec
	gaugeVec := r.Gauge("labeled_gauge").
		Help("A labeled gauge").
		Labels("node").
		BuildVec()

	require.NotNil(t, gaugeVec)

	gaugeVec.WithLabelValues("node1").Set(100)
	gaugeVec.WithLabelValues("node2").Set(200)

	metric1 := &dto.Metric{}
	err := gaugeVec.WithLabelValues("node1").Write(metric1)
	require.NoError(t, err)
	assert.Equal(t, 100.0, metric1.Gauge.GetValue())

	metric2 := &dto.Metric{}
	err = gaugeVec.WithLabelValues("node2").Write(metric2)
	require.NoError(t, err)
	assert.Equal(t, 200.0, metric2.Gauge.GetValue())
}

func TestSummaryBuilder(t *testing.T) {
	r := NewRegistry("test_summary")

	// Test building a simple summary
	summary := r.Summary("simple_summary").
		Help("A simple summary").
		Objectives(map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001}).
		Build()

	require.NotNil(t, summary)
	for i := 0; i < 100; i++ {
		summary.Observe(float64(i) / 100.0)
	}
}

func TestSummaryBuilder_Vec(t *testing.T) {
	r := NewRegistry("test_summary_vec")

	// Test building a summary vec
	summaryVec := r.Summary("labeled_summary").
		Help("A labeled summary").
		Labels("endpoint").
		Objectives(map[float64]float64{0.5: 0.05, 0.9: 0.01}).
		BuildVec()

	require.NotNil(t, summaryVec)
	summaryVec.WithLabelValues("/api/users").Observe(0.1)
	summaryVec.WithLabelValues("/api/orders").Observe(0.2)
}

func TestBucketHelpers(t *testing.T) {
	t.Run("DefaultBuckets", func(t *testing.T) {
		buckets := DefaultBuckets()
		assert.Equal(t, prometheus.DefBuckets, buckets)
	})

	t.Run("HTTPDurationBuckets", func(t *testing.T) {
		buckets := HTTPDurationBuckets()
		assert.Len(t, buckets, 12)
		assert.Equal(t, 0.001, buckets[0]) // 1ms
		assert.Equal(t, 10.0, buckets[11]) // 10s
	})

	t.Run("RedisDurationBuckets", func(t *testing.T) {
		buckets := RedisDurationBuckets()
		assert.Len(t, buckets, 11)
		assert.Equal(t, 0.0005, buckets[0]) // 0.5ms
		assert.Equal(t, 1.0, buckets[10])   // 1s
	})

	t.Run("ExternalAPIDurationBuckets", func(t *testing.T) {
		buckets := ExternalAPIDurationBuckets()
		assert.Len(t, buckets, 10)
		assert.Equal(t, 0.01, buckets[0]) // 10ms
		assert.Equal(t, 30.0, buckets[9]) // 30s
	})

	t.Run("BytesBuckets", func(t *testing.T) {
		buckets := BytesBuckets()
		assert.Len(t, buckets, 6)
		assert.Equal(t, 100.0, buckets[0])      // 100B
		assert.Equal(t, 10485760.0, buckets[5]) // 10MB
	})
}

func TestHistogramBuilder_ConstLabels(t *testing.T) {
	r := NewRegistry("test_histogram_const")

	histogram := r.Histogram("const_labeled_histogram").
		Help("A histogram with const labels").
		ConstLabels(prometheus.Labels{"service": "myapp", "version": "v1"}).
		Buckets([]float64{0.1, 0.5, 1.0}).
		Build()

	require.NotNil(t, histogram)
	histogram.Observe(0.3)
	histogram.Observe(0.7)
}

func TestHistogramBuilder_ConstLabels_Vec(t *testing.T) {
	r := NewRegistry("test_histogram_const_vec")

	histogramVec := r.Histogram("const_labeled_histogram_vec").
		Help("A histogram vec with const labels").
		Labels("operation").
		ConstLabels(prometheus.Labels{"service": "myapp"}).
		Buckets([]float64{0.1, 0.5, 1.0}).
		BuildVec()

	require.NotNil(t, histogramVec)
	histogramVec.WithLabelValues("read").Observe(0.3)
	histogramVec.WithLabelValues("write").Observe(0.7)
}

func TestGaugeBuilder_ConstLabels(t *testing.T) {
	r := NewRegistry("test_gauge_const")

	gauge := r.Gauge("const_labeled_gauge").
		Help("A gauge with const labels").
		ConstLabels(prometheus.Labels{"service": "myapp", "env": "prod"}).
		Build()

	require.NotNil(t, gauge)
	gauge.Set(100)

	metric := &dto.Metric{}
	err := gauge.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 100.0, metric.Gauge.GetValue())
}

func TestGaugeBuilder_ConstLabels_Vec(t *testing.T) {
	r := NewRegistry("test_gauge_const_vec")

	gaugeVec := r.Gauge("const_labeled_gauge_vec").
		Help("A gauge vec with const labels").
		Labels("node").
		ConstLabels(prometheus.Labels{"service": "myapp"}).
		BuildVec()

	require.NotNil(t, gaugeVec)
	gaugeVec.WithLabelValues("node1").Set(100)
	gaugeVec.WithLabelValues("node2").Set(200)

	metric1 := &dto.Metric{}
	err := gaugeVec.WithLabelValues("node1").Write(metric1)
	require.NoError(t, err)
	assert.Equal(t, 100.0, metric1.Gauge.GetValue())
}

func TestSummaryBuilder_ConstLabels(t *testing.T) {
	r := NewRegistry("test_summary_const")

	summary := r.Summary("const_labeled_summary").
		Help("A summary with const labels").
		ConstLabels(prometheus.Labels{"service": "myapp", "region": "us-west"}).
		Objectives(map[float64]float64{0.5: 0.05, 0.9: 0.01}).
		Build()

	require.NotNil(t, summary)
	for i := 0; i < 100; i++ {
		summary.Observe(float64(i) / 100.0)
	}
}

func TestSummaryBuilder_ConstLabels_Vec(t *testing.T) {
	r := NewRegistry("test_summary_const_vec")

	summaryVec := r.Summary("const_labeled_summary_vec").
		Help("A summary vec with const labels").
		Labels("endpoint").
		ConstLabels(prometheus.Labels{"service": "myapp"}).
		Objectives(map[float64]float64{0.5: 0.05, 0.9: 0.01}).
		BuildVec()

	require.NotNil(t, summaryVec)
	summaryVec.WithLabelValues("/api/users").Observe(0.1)
	summaryVec.WithLabelValues("/api/orders").Observe(0.2)
}
