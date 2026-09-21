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

// --- error and cleanup paths -------------------------------------------

// TestRegistry_UnregisterAfterRawUnregister covers the branch where a name is
// still tracked but the collector behind it is already gone: something
// unregistered it straight through PrometheusRegistry(), which this package
// cannot observe. Unregister must report false rather than claim a removal it
// did not make.
func TestRegistry_UnregisterAfterRawUnregister(t *testing.T) {
	r := NewRegistry("unreg_raw")

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "unreg_raw",
		Name:      "counter_total",
		Help:      "A test counter",
	})
	require.NoError(t, r.Register("tracked", counter))

	// Behind this package's back.
	require.True(t, r.PrometheusRegistry().Unregister(counter))

	assert.False(t, r.Unregister("tracked"),
		"the name was tracked but the collector was already gone")
}

// TestRegistry_UnregisterCollectorDropsTheNamedEntry covers the other half of
// that bookkeeping: a collector registered under a name and then removed by
// VALUE must not leave its name behind, or a later Register under the same
// name would look occupied.
func TestRegistry_UnregisterCollectorDropsTheNamedEntry(t *testing.T) {
	r := NewRegistry("unreg_by_value")

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "unreg_by_value",
		Name:      "counter_total",
		Help:      "A test counter",
	})
	require.NoError(t, r.Register("tracked", counter))

	assert.True(t, r.UnregisterCollector(counter))
	// The name is free again, and reports nothing left to remove.
	assert.False(t, r.Unregister("tracked"))

	require.NoError(t, r.Register("tracked", counter))
	assert.True(t, r.Unregister("tracked"))
}

func TestRegistry_UnregisterCollectorThatWasNeverRegistered(t *testing.T) {
	r := NewRegistry("unreg_absent")
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "unreg_absent",
		Name:      "counter_total",
		Help:      "A test counter",
	})
	assert.False(t, r.UnregisterCollector(counter))
}

// valueCollector is a Collector whose method set is on the VALUE, so it is
// held in the interface as a struct rather than a pointer.
type valueCollector struct{ desc *prometheus.Desc }

func (v valueCollector) Describe(ch chan<- *prometheus.Desc) { ch <- v.desc }
func (v valueCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(v.desc, prometheus.GaugeValue, 1)
}

// TestRegistry_NonPointerCollectorIsNotMatchedByIdentity covers the fail-closed
// branch of sameCollector. Identity is pointer identity, because == panics on
// an interface holding a non-comparable value; a collector that cannot be
// compared that way is reported as NOT the same, so its named entry survives
// a removal it cannot be proven to be part of.
func TestRegistry_NonPointerCollectorIsNotMatchedByIdentity(t *testing.T) {
	r := NewRegistry("nonptr")
	c := valueCollector{desc: prometheus.NewDesc("nonptr_thing", "h", nil, nil)}

	require.NoError(t, r.Register("tracked", c))
	// The collector itself is removed from the Prometheus registry...
	assert.True(t, r.UnregisterCollector(c))
	// ...but identity could not be established, so the name was left alone
	// rather than deleted on a guess.
	assert.False(t, r.Unregister("tracked"),
		"the name outlives the collector; the registry no longer holds it")
}

// TestRegistry_IncompatibleDescriptorPanics covers the registration failure
// that is NOT AlreadyRegistered: Prometheus rejects a second descriptor
// sharing a fully-qualified name but differing in help or label names. There
// is no existing collector to hand back, so the build cannot continue.
func TestRegistry_IncompatibleDescriptorPanics(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ns    string
		build func(*Registry)
	}{
		{"different help", "desc_help", func(r *Registry) {
			r.Counter("thing_total").Help("first").Build()
			r.Counter("thing_total").Help("second").Build()
		}},
		{"different label names", "desc_labels", func(r *Registry) {
			r.Counter("thing_total").Help("h").Labels("a").BuildVec()
			r.Counter("thing_total").Help("h").Labels("b").BuildVec()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry(tc.ns)
			var got any
			func() {
				defer func() { got = recover() }()
				tc.build(r)
			}()
			err, ok := got.(error)
			require.Truef(t, ok, "want a panic carrying Prometheus's error, got %#v", got)
			assert.Contains(t, err.Error(), "has different label names or a different help string")
		})
	}
}
