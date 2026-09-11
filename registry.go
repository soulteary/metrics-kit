// Package metrics provides a unified Prometheus metrics toolkit for Go services.
// It includes registry management, metric builders, HTTP handlers, and middleware.
package metrics

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// Registry wraps prometheus.Registry with additional functionality
// for managing metrics in a service-oriented way.
type Registry struct {
	// registry is the underlying Prometheus registry
	registry *prometheus.Registry

	// namespace is the common prefix for all metrics (e.g., "herald", "stargate", "warden")
	namespace string

	// subsystem is an optional subsystem name (e.g., "otp", "auth", "cache")
	subsystem string

	// state is the mutable state shared with every WithSubsystem view.
	state *registryState
}

// registryState is the mutable state a Registry shares with the views derived
// from it.
//
// It lives behind a POINTER so a derived Registry shares the mutex along with
// the maps. WithSubsystem used to copy the Registry struct, giving each view
// its own zero-value mutex while the maps stayed shared -- two views writing
// concurrently, even in different subsystems, then raced on those maps and
// could kill the process with "concurrent map writes".
type registryState struct {
	mu sync.RWMutex

	// collectors tracks registered collectors for Reset/Unregister
	collectors map[string]prometheus.Collector

	// shapes records, per metric id, the configuration Prometheus does not
	// treat as part of a collector's identity -- label ORDER, histogram
	// buckets, summary objectives -- so a second registration asking for a
	// different one is reported instead of silently reusing the first.
	shapes map[string]string
}

// shapeStores maps an underlying *prometheus.Registry to its shape records.
//
// Shape knowledge has to follow the REGISTRY, not the wrapper: two wrappers
// over one registry (two DefaultRegistry() calls, say) both register into the
// same place, so a shape recorded through one must be visible to the other.
// Keeping it per-wrapper left the second wrapper with no prior entry, so it
// recorded its own shape, got AlreadyRegisteredError, and handed back the
// incompatible existing collector.
var shapeStores sync.Map // *prometheus.Registry -> *registryState

// stateFor returns the shared state for reg, creating it on first use.
func stateFor(reg *prometheus.Registry) *registryState {
	if existing, ok := shapeStores.Load(reg); ok {
		return existing.(*registryState)
	}
	actual, _ := shapeStores.LoadOrStore(reg, &registryState{
		collectors: make(map[string]prometheus.Collector),
		shapes:     make(map[string]string),
	})
	return actual.(*registryState)
}

// NewRegistry creates a new Registry with the given namespace.
// The namespace is typically the service name (e.g., "herald", "stargate").
func NewRegistry(namespace string) *Registry {
	reg := prometheus.NewRegistry()
	return &Registry{
		registry:  reg,
		namespace: namespace,
		state:     stateFor(reg),
	}
}

// NewRegistryWithSubsystem creates a new Registry with namespace and subsystem.
func NewRegistryWithSubsystem(namespace, subsystem string) *Registry {
	reg := prometheus.NewRegistry()
	return &Registry{
		registry:  reg,
		namespace: namespace,
		subsystem: subsystem,
		state:     stateFor(reg),
	}
}

// DefaultRegistry returns a Registry wrapping the default Prometheus registry.
func DefaultRegistry() *Registry {
	reg := prometheus.DefaultRegisterer.(*prometheus.Registry)
	return &Registry{
		registry:  reg,
		namespace: "",
		state:     stateFor(reg),
	}
}

// Namespace returns the registry's namespace.
func (r *Registry) Namespace() string {
	return r.namespace
}

// Subsystem returns the registry's subsystem.
func (r *Registry) Subsystem() string {
	return r.subsystem
}

// WithSubsystem returns a new Registry with the same underlying registry
// but a different subsystem.
func (r *Registry) WithSubsystem(subsystem string) *Registry {
	return &Registry{
		registry:  r.registry,
		namespace: r.namespace,
		subsystem: subsystem,
		// The whole state, mutex included: a derived registry writes into the
		// SAME prometheus.Registry and the same maps.
		state: r.state,
	}
}

// Register registers a collector with the registry.
// It tracks the collector for later unregistration.
func (r *Registry) Register(name string, collector prometheus.Collector) error {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()

	if err := r.registry.Register(collector); err != nil {
		return err
	}

	r.state.collectors[name] = collector
	return nil
}

// MustRegister registers collectors and panics on error.
func (r *Registry) MustRegister(collectors ...prometheus.Collector) {
	r.registry.MustRegister(collectors...)
}

// registerOrExisting registers a collector, returning the already-registered
// collector when one with the same fully-qualified name and labels exists.
//
// Builders used MustRegister, so a duplicate metric name -- two components
// declaring the same counter, or a package initialised twice in a test binary
// -- crashed the process at startup. A name collision is a programming
// mistake, but taking the service down for it is a poor trade when the
// existing collector is exactly what the caller wanted.
//
// id identifies the metric and shape fingerprints the configuration that
// Prometheus does NOT consider part of a collector's identity: histogram
// bucket boundaries and summary objectives. Two registrations agreeing on
// name, help and labels but differing there are duplicates as far as
// Prometheus is concerned, so handing back the first would silently discard
// the second caller's configuration and aggregate its observations into a
// layout it never asked for. Reuse is therefore limited to collectors whose
// complete configuration matches; a genuine conflict is reported rather than
// hidden. shape is empty for counters and gauges, which carry no such
// configuration.
func (r *Registry) registerOrExisting(c prometheus.Collector, id, shape string) prometheus.Collector {
	r.state.mu.Lock()
	if r.state.shapes == nil {
		r.state.shapes = make(map[string]string)
	}
	previous, known := r.state.shapes[id]
	if known && previous != shape {
		r.state.mu.Unlock()
		panic(shapeConflict(id, previous, shape))
	}
	r.state.shapes[id] = shape
	r.state.mu.Unlock()

	if err := r.registry.Register(c); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if !known {
				// Prometheus says this descriptor is taken, but nothing
				// recorded its shape -- it was registered outside the
				// builders, through MustRegister or a raw Registerer. The
				// existing collector's buckets, objectives and label order
				// cannot be read back through the Prometheus API, so
				// compatibility is unverifiable. Returning it anyway is how
				// the mismatch this check exists to catch slips through.
				panic(shapeConflict(id, "unknown (registered outside this package)", shape))
			}
			return already.ExistingCollector
		}
		panic(err)
	}
	return c
}

// shapeConflict is the panic message for an incompatible re-registration.
func shapeConflict(id, previous, requested string) string {
	return fmt.Sprintf(
		"metrics: %q is already registered with a different configuration.\n"+
			"  registered: %s\n  requested:  %s\n"+
			"Prometheus treats these as the same collector, so reusing it would record "+
			"observations into a layout this caller did not ask for.", id, previous, requested)
}

// metricID is the identity registerOrExisting keys its shape records by.
//
// Labels are SORTED, because a Prometheus descriptor's identity is the label
// NAME SET, not its order: Labels("method","path") and Labels("path","method")
// collide. Sorting here makes them share an id so the differing order shows up
// as a shape conflict; keying on the given order instead hid it, and
// AlreadyRegisteredError then handed back the first vector, silently swapping
// the two label values in the second caller's WithLabelValues calls.
func (r *Registry) metricID(name string, labels []string) string {
	sorted := append([]string(nil), labels...)
	sort.Strings(sorted)
	return prometheus.BuildFQName(r.namespace, r.subsystem, name) + "{" + strings.Join(sorted, ",") + "}"
}

// labelShape fingerprints the label ORDER, which the id deliberately discards.
func labelShape(labels []string) string {
	return "labels=[" + strings.Join(labels, ",") + "]"
}

// bucketShape fingerprints histogram bucket boundaries.
//
// Nil, empty and an explicit prometheus.DefBuckets all mean the same layout --
// Prometheus substitutes DefBuckets for nil -- so they must produce the same
// fingerprint. Histogram() seeds b.buckets with DefBuckets while .Buckets(nil)
// leaves it empty, and treating those as different made a perfectly valid
// second registration panic as a conflict.
func bucketShape(buckets []float64) string {
	if len(buckets) == 0 || slices.Equal(buckets, prometheus.DefBuckets) {
		return "buckets=default"
	}
	parts := make([]string, len(buckets))
	for i, b := range buckets {
		parts[i] = strconv.FormatFloat(b, 'g', -1, 64)
	}
	return "buckets=[" + strings.Join(parts, ",") + "]"
}

// objectiveShape fingerprints summary objectives.
func objectiveShape(objectives map[float64]float64) string {
	if len(objectives) == 0 {
		return "objectives=default"
	}
	quantiles := make([]float64, 0, len(objectives))
	for q := range objectives {
		quantiles = append(quantiles, q)
	}
	sort.Float64s(quantiles)

	parts := make([]string, len(quantiles))
	for i, q := range quantiles {
		parts[i] = strconv.FormatFloat(q, 'g', -1, 64) + ":" + strconv.FormatFloat(objectives[q], 'g', -1, 64)
	}
	return "objectives={" + strings.Join(parts, ",") + "}"
}

// Unregister removes a collector from the registry.
func (r *Registry) Unregister(name string) bool {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()

	collector, ok := r.state.collectors[name]
	if !ok {
		return false
	}

	if r.registry.Unregister(collector) {
		delete(r.state.collectors, name)
		return true
	}
	return false
}

// Gatherer returns the underlying prometheus.Gatherer interface.
func (r *Registry) Gatherer() prometheus.Gatherer {
	return r.registry
}

// Registerer returns the underlying prometheus.Registerer interface.
func (r *Registry) Registerer() prometheus.Registerer {
	return r.registry
}

// PrometheusRegistry returns the underlying *prometheus.Registry.
func (r *Registry) PrometheusRegistry() *prometheus.Registry {
	return r.registry
}

// buildFQName creates a fully qualified metric name.
func (r *Registry) buildFQName(name string) string {
	return prometheus.BuildFQName(r.namespace, r.subsystem, name)
}
