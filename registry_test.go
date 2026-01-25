package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry("myservice")
	assert.NotNil(t, r)
	assert.Equal(t, "myservice", r.Namespace())
	assert.Equal(t, "", r.Subsystem())
}

func TestNewRegistryWithSubsystem(t *testing.T) {
	r := NewRegistryWithSubsystem("myservice", "http")
	assert.NotNil(t, r)
	assert.Equal(t, "myservice", r.Namespace())
	assert.Equal(t, "http", r.Subsystem())
}

func TestRegistry_WithSubsystem(t *testing.T) {
	r := NewRegistry("myservice")
	sub := r.WithSubsystem("cache")

	assert.Equal(t, "myservice", sub.Namespace())
	assert.Equal(t, "cache", sub.Subsystem())
	// Should share the same underlying registry
	assert.Same(t, r.registry, sub.registry)
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry("test")

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_counter",
		Help: "A test counter",
	})

	err := r.Register("my_counter", counter)
	require.NoError(t, err)

	// Registering the same name should fail
	counter2 := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_counter",
		Help: "A duplicate counter",
	})
	err = r.Register("my_counter_2", counter2)
	assert.Error(t, err)
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry("test")

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "unregister_test_counter",
		Help: "A test counter",
	})

	err := r.Register("my_counter", counter)
	require.NoError(t, err)

	// Unregister should succeed
	ok := r.Unregister("my_counter")
	assert.True(t, ok)

	// Unregister non-existent should fail
	ok = r.Unregister("non_existent")
	assert.False(t, ok)
}

func TestRegistry_MustRegister(t *testing.T) {
	r := NewRegistry("test")

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "must_register_test_counter",
		Help: "A test counter",
	})

	// Should not panic
	assert.NotPanics(t, func() {
		r.MustRegister(counter)
	})
}

func TestRegistry_Gatherer(t *testing.T) {
	r := NewRegistry("test")
	assert.NotNil(t, r.Gatherer())
}

func TestRegistry_Registerer(t *testing.T) {
	r := NewRegistry("test")
	assert.NotNil(t, r.Registerer())
}

func TestRegistry_PrometheusRegistry(t *testing.T) {
	r := NewRegistry("test")
	assert.NotNil(t, r.PrometheusRegistry())
}

func TestDefaultRegistry(t *testing.T) {
	r := DefaultRegistry()
	assert.NotNil(t, r)
	assert.Equal(t, "", r.Namespace())
}

func TestRegistry_buildFQName(t *testing.T) {
	tests := []struct {
		name       string
		namespace  string
		subsystem  string
		metricName string
		expected   string
	}{
		{
			name:       "with namespace only",
			namespace:  "myservice",
			subsystem:  "",
			metricName: "requests_total",
			expected:   "myservice_requests_total",
		},
		{
			name:       "with namespace and subsystem",
			namespace:  "myservice",
			subsystem:  "http",
			metricName: "requests_total",
			expected:   "myservice_http_requests_total",
		},
		{
			name:       "without namespace",
			namespace:  "",
			subsystem:  "",
			metricName: "requests_total",
			expected:   "requests_total",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistryWithSubsystem(tt.namespace, tt.subsystem)
			result := r.buildFQName(tt.metricName)
			assert.Equal(t, tt.expected, result)
		})
	}
}
