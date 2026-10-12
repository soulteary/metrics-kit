package metrics

import (
	"maps"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultHTTPMetricsConfig(t *testing.T) {
	cfg := DefaultHTTPMetricsConfig()

	assert.Equal(t, "http", cfg.Subsystem)
	assert.Equal(t, HTTPDurationBuckets(), cfg.DurationBuckets)
	assert.Equal(t, BytesBuckets(), cfg.SizeBuckets)
	assert.True(t, cfg.IncludeRequestsInFlight)
	assert.False(t, cfg.IncludeRequestSize)
	assert.False(t, cfg.IncludeResponseSize)
	assert.NotNil(t, cfg.PathTransformFunc, "default config should set PathTransformFunc for cardinality safety")
}

func TestNewHTTPMetrics(t *testing.T) {
	cfg := HTTPMetricsConfig{
		Namespace: "test",
		Subsystem: "http",
	}

	m := NewHTTPMetrics(cfg)
	require.NotNil(t, m)
	require.NotNil(t, m.RequestsTotal)
	require.NotNil(t, m.RequestDuration)
}

func TestNewHTTPMetrics_WithAllOptions(t *testing.T) {
	cfg := HTTPMetricsConfig{
		Namespace:               "test",
		Subsystem:               "http",
		DurationBuckets:         []float64{0.1, 0.5, 1.0},
		SizeBuckets:             []float64{100, 1000, 10000},
		IncludeRequestSize:      true,
		IncludeResponseSize:     true,
		IncludeRequestsInFlight: true,
	}

	m := NewHTTPMetrics(cfg)
	require.NotNil(t, m)
	require.NotNil(t, m.RequestsTotal)
	require.NotNil(t, m.RequestDuration)
	require.NotNil(t, m.RequestsInFlight)
	require.NotNil(t, m.RequestSize)
	require.NotNil(t, m.ResponseSize)
}

func TestNewHTTPMetrics_WithRegistry(t *testing.T) {
	r := NewRegistry("myapp")
	cfg := HTTPMetricsConfig{
		Registry:  r,
		Namespace: "myapp",
		Subsystem: "http",
	}

	m := NewHTTPMetrics(cfg)
	require.NotNil(t, m)
	require.NotNil(t, m.RequestsTotal)
}

func TestHTTPMetrics_RecordRequest(t *testing.T) {
	cfg := HTTPMetricsConfig{
		Namespace: "test_record",
		Subsystem: "http",
	}

	m := NewHTTPMetrics(cfg)
	require.NotNil(t, m)

	// Record some requests
	m.RecordRequest("GET", "/api/users", "200", 100*time.Millisecond)
	m.RecordRequest("POST", "/api/users", "201", 200*time.Millisecond)
	m.RecordRequest("GET", "/api/users", "500", 50*time.Millisecond)

	for _, labels := range []map[string]string{
		{"method": "GET", "path": "/api/users", "status": "200"},
		{"method": "POST", "path": "/api/users", "status": "201"},
		{"method": "GET", "path": "/api/users", "status": "500"},
	} {
		assert.Equal(t, 1.0, recordedMetric(t, m.Registry, "test_record_http_requests_total", labels).GetCounter().GetValue())
	}
	assertRecordedHistogram(t, m.Registry, "test_record_http_request_duration_seconds",
		map[string]string{"method": "GET", "path": "/api/users"}, 2, 0.15)
	assertRecordedHistogram(t, m.Registry, "test_record_http_request_duration_seconds",
		map[string]string{"method": "POST", "path": "/api/users"}, 1, 0.2)
}

func TestHTTPMetrics_RecordRequestWithSize(t *testing.T) {
	cfg := HTTPMetricsConfig{
		Namespace:           "test_size",
		Subsystem:           "http",
		IncludeRequestSize:  true,
		IncludeResponseSize: true,
	}

	m := NewHTTPMetrics(cfg)
	require.NotNil(t, m)

	m.RecordRequestWithSize("POST", "/api/upload", "200", 100*time.Millisecond, 1024, 256)
	m.RecordRequestWithSize("GET", "/api/download", "200", 50*time.Millisecond, 0, 4096)

	upload := map[string]string{"method": "POST", "path": "/api/upload"}
	assertRecordedHistogram(t, m.Registry, "test_size_http_request_duration_seconds", upload, 1, 0.1)
	assertRecordedHistogram(t, m.Registry, "test_size_http_request_size_bytes", upload, 1, 1024)
	assertRecordedHistogram(t, m.Registry, "test_size_http_response_size_bytes", upload, 1, 256)
	download := map[string]string{"method": "GET", "path": "/api/download"}
	assertRecordedHistogram(t, m.Registry, "test_size_http_request_duration_seconds", download, 1, 0.05)
	assertRecordedHistogram(t, m.Registry, "test_size_http_response_size_bytes", download, 1, 4096)
}

func TestHTTPMetrics_RecordRequestWithSize_NilMetrics(t *testing.T) {
	cfg := HTTPMetricsConfig{
		Namespace:           "test_nil",
		Subsystem:           "http",
		IncludeRequestSize:  false,
		IncludeResponseSize: false,
	}

	m := NewHTTPMetrics(cfg)
	require.NotNil(t, m)
	require.Nil(t, m.RequestSize)
	require.Nil(t, m.ResponseSize)

	// Should not panic
	m.RecordRequestWithSize("POST", "/api/upload", "200", 100*time.Millisecond, 1024, 256)
	assertRecordedHistogram(t, m.Registry, "test_nil_http_request_duration_seconds",
		map[string]string{"method": "POST", "path": "/api/upload"}, 1, 0.1)
	mfs, err := m.Registry.Gatherer().Gather()
	require.NoError(t, err)
	require.Len(t, mfs, 2, "disabled size collectors must not appear in gathered metrics")
}

func TestHTTPMetrics_RecordRequestWithSize_SkipsNonPositiveSizes(t *testing.T) {
	m := NewHTTPMetrics(HTTPMetricsConfig{
		Namespace:           "skip_sizes",
		IncludeRequestSize:  true,
		IncludeResponseSize: true,
	})
	m.RecordRequestWithSize("POST", "/upload", "200", 10*time.Millisecond, 0, 256)
	m.RecordRequestWithSize("POST", "/upload", "200", 10*time.Millisecond, 1024, 0)
	m.RecordRequestWithSize("POST", "/upload", "200", 10*time.Millisecond, -1, -1)

	labels := map[string]string{"method": "POST", "path": "/upload"}
	assertRecordedHistogram(t, m.Registry, "skip_sizes_request_size_bytes", labels, 1, 1024)
	assertRecordedHistogram(t, m.Registry, "skip_sizes_response_size_bytes", labels, 1, 256)
	assertRecordedHistogram(t, m.Registry, "skip_sizes_request_duration_seconds", labels, 3, 0.03)
	assert.Equal(t, 3.0, recordedMetric(t, m.Registry, "skip_sizes_requests_total",
		map[string]string{"method": "POST", "path": "/upload", "status": "200"}).GetCounter().GetValue())
}

func TestHTTPMetrics_RecordRequestWithSize_SanitizesAllLabels(t *testing.T) {
	m := NewHTTPMetrics(HTTPMetricsConfig{
		Namespace:           "safe_labels",
		IncludeRequestSize:  true,
		IncludeResponseSize: true,
	})
	m.RecordRequestWithSize(" G\"ET\r\n ", " /api/\"upload\\file\r\n ", " 2\"00\n ", 125*time.Millisecond, 10, 20)

	labels := map[string]string{"method": "G_ET", "path": "/api/_upload_file"}
	assertRecordedHistogram(t, m.Registry, "safe_labels_request_duration_seconds", labels, 1, 0.125)
	assertRecordedHistogram(t, m.Registry, "safe_labels_request_size_bytes", labels, 1, 10)
	assertRecordedHistogram(t, m.Registry, "safe_labels_response_size_bytes", labels, 1, 20)
	assert.Equal(t, 1.0, recordedMetric(t, m.Registry, "safe_labels_requests_total",
		map[string]string{"method": "G_ET", "path": "/api/_upload_file", "status": "2_00"}).GetCounter().GetValue())
}

func recordedMetric(t *testing.T, r *Registry, name string, wantLabels map[string]string) *dto.Metric {
	t.Helper()
	mfs, err := r.Gatherer().Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, metric := range mf.GetMetric() {
			labels := make(map[string]string)
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if maps.Equal(labels, wantLabels) {
				return metric
			}
		}
	}
	require.FailNow(t, "recorded metric not found", "%s with labels %v", name, wantLabels)
	return nil
}

func assertRecordedHistogram(t *testing.T, r *Registry, name string, labels map[string]string, count uint64, sum float64) {
	t.Helper()
	h := recordedMetric(t, r, name, labels).GetHistogram()
	require.NotNil(t, h)
	assert.Equal(t, count, h.GetSampleCount(), "observation count for %s", name)
	assert.InDelta(t, sum, h.GetSampleSum(), 1e-9, "observation sum for %s", name)
}
