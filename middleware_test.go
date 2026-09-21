package metrics

import (
	"testing"
	"time"

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
}
