// Package metrics provides a unified Prometheus metrics toolkit for Go services.
// It includes registry management, metric builders, HTTP handlers, and middleware.
package metrics

import (
	"errors"
	"fmt"
	"math"
	"reflect"
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
	// treat as part of a collector's identity -- the collector KIND, label
	// ORDER, histogram buckets, summary objectives -- so a second
	// registration asking for a different one is reported instead of
	// silently reusing the first.
	shapes map[string]shapeRecord
}

// shapeRecord is a fingerprint together with the collector it describes.
//
// The owner matters because a descriptor can change hands without this
// package noticing: register through a builder, Unregister that collector,
// then raw-register an external one with the same descriptor and different
// buckets. The next builder request gets AlreadyRegisteredError naming the
// EXTERNAL collector, while the recorded shape still describes the departed
// one -- so a matching fingerprint would hand back the incompatible external
// collector as verified.
type shapeRecord struct {
	shape string
	owner prometheus.Collector
}

// newRegistryState allocates state for a registry this package owns.
func newRegistryState() *registryState {
	return &registryState{
		collectors: make(map[string]prometheus.Collector),
		shapes:     make(map[string]shapeRecord),
	}
}

// shapeStores maps a registry this package does NOT own to its shape records.
//
// Shape knowledge has to follow the REGISTRY, not the wrapper: two wrappers
// over one registry both register into the same place, so a shape recorded
// through one must be visible to the other. Keeping it per-wrapper left the
// second wrapper with no prior entry, so it recorded its own shape, got
// AlreadyRegisteredError, and handed back the incompatible existing collector.
//
// Only stateFor's callers need the lookup, and the only one is
// DefaultRegistry: NewRegistry and NewRegistryWithSubsystem mint a registry
// nobody else can be wrapping, so they allocate state directly. Routing those
// through here instead added a permanent entry -- keyed by the registry, which
// retains its collectors -- for every registry a test, a reload or a repeated
// middleware construction ever created, and nothing removed them.
var shapeStores sync.Map // *prometheus.Registry -> *registryState

// stateFor returns the shared state for a registry owned elsewhere, creating
// it on first use. The entry is permanent, which is why this is reserved for
// process-global registries.
func stateFor(reg *prometheus.Registry) *registryState {
	if existing, ok := shapeStores.Load(reg); ok {
		return existing.(*registryState)
	}
	actual, _ := shapeStores.LoadOrStore(reg, newRegistryState())
	return actual.(*registryState)
}

// NewRegistry creates a new Registry with the given namespace.
// The namespace is typically the service name (e.g., "herald", "stargate").
func NewRegistry(namespace string) *Registry {
	return &Registry{
		registry:  prometheus.NewRegistry(),
		namespace: namespace,
		state:     newRegistryState(),
	}
}

// NewRegistryWithSubsystem creates a new Registry with namespace and subsystem.
func NewRegistryWithSubsystem(namespace, subsystem string) *Registry {
	return &Registry{
		registry:  prometheus.NewRegistry(),
		namespace: namespace,
		subsystem: subsystem,
		state:     newRegistryState(),
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
	// The lock spans the registration. Prometheus decides who owns the
	// descriptor, and only the winner may record its shape; publishing the
	// shape first and registering afterwards got both halves wrong.
	//
	// Two goroutines building the same NEW metric both recorded a shape, then
	// one lost Register -- and, having seen no prior entry, read its own
	// AlreadyRegisteredError as "registered outside this package" and panicked
	// on a collector this package had just created. And after a genuine
	// external-collector panic the rejected shape stayed behind, so retrying
	// the same build found it "known", matched it against itself, and returned
	// the incompatible external collector the panic existed to refuse.
	//
	// Taking r.state.mu around r.registry.Register cannot deadlock: the
	// Prometheus registry's own lock is only ever taken while holding this
	// one, never the other way round.
	r.state.mu.Lock()
	defer r.state.mu.Unlock()

	if r.state.shapes == nil {
		r.state.shapes = make(map[string]shapeRecord)
	}

	if err := r.registry.Register(c); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			record, known := r.state.shapes[id]
			if known && !sameCollector(record.owner, already.ExistingCollector) {
				// The descriptor changed hands. Whatever is registered now is
				// not what this record describes, so its shape proves nothing.
				known = false
			}
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
			if record.shape != shape {
				panic(shapeConflict(id, record.shape, shape))
			}
			return already.ExistingCollector
		}
		panic(err)
	}

	// Registration succeeded, so the descriptor was free. Any shape still
	// recorded for this id described a collector that is no longer registered
	// -- the caller reached PrometheusRegistry().Unregister -- and Prometheus
	// accepts a different layout after that, so replace it rather than
	// reporting a conflict with a collector that has gone.
	r.state.shapes[id] = shapeRecord{shape: shape, owner: c}
	return c
}

// sameCollector reports whether a and b are the same collector instance.
//
// By pointer identity rather than ==, which panics on an interface holding a
// non-comparable value and a caller may well register one. Anything not
// comparable this way is reported as NOT the same, which fails closed: the
// recorded shape is then treated as unknown and the reuse refused.
func sameCollector(a, b prometheus.Collector) bool {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	if av.Kind() != reflect.Pointer || bv.Kind() != reflect.Pointer {
		return false
	}
	return av.Pointer() == bv.Pointer()
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
// It mirrors what Prometheus treats as a descriptor's identity: the
// fully-qualified name, the variable label NAME SET, and the constant labels.
//
// Labels are SORTED, because a Prometheus descriptor's identity is the label
// NAME SET, not its order: Labels("method","path") and Labels("path","method")
// collide. Sorting here makes them share an id so the differing order shows up
// as a shape conflict; keying on the given order instead hid it, and
// AlreadyRegisteredError then handed back the first vector, silently swapping
// the two label values in the second caller's WithLabelValues calls.
func (r *Registry) metricID(name string, labels []string, constLabels prometheus.Labels) string {
	sorted := append([]string(nil), labels...)
	sort.Strings(sorted)
	return prometheus.BuildFQName(r.namespace, r.subsystem, name) +
		"{" + strings.Join(sorted, ",") + "}" + constLabelID(constLabels)
}

// constLabelID renders the constant labels, which ARE part of a descriptor's
// identity: Prometheus registers two collectors that differ only there side by
// side. Leaving them out of the id filed both under one shape record, so a
// perfectly valid pair of histograms -- same name and variable labels, one per
// const-label value -- panicked as a conflict the moment their buckets
// differed, before Prometheus ever saw the second registration.
func constLabelID(constLabels prometheus.Labels) string {
	if len(constLabels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(constLabels))
	for k := range constLabels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + strconv.Quote(constLabels[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// kindShape fingerprints the collector kind AND its form.
//
// Prometheus does not distinguish kinds in a descriptor, and Go's interfaces
// do not distinguish them either: a gauge structurally implements Inc and Add,
// so prometheus.Counter accepts one, and a summary implements Observe, so
// prometheus.Histogram accepts one. Reusing across kinds therefore type-
// asserted cleanly and handed back the wrong instrument -- a gauge exported
// as a counter, accepting negative Add calls.
//
// Scalar and vector are separate kinds here ("counter" vs "counter_vec"),
// because Build() and BuildVec() with no labels produce the same metric id
// AND the same label shape. The second registration was handed the first
// collector, and THAT assertion does fail -- a prometheus.Counter is not a
// *prometheus.CounterVec -- so the reuse this whole mechanism exists to make
// safe reintroduced the startup panic it was meant to remove. Now it is
// reported as the configuration conflict it is.
func kindShape(kind string) string {
	return "kind=" + kind + ";"
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
	if len(buckets) == 0 {
		return "buckets=default"
	}
	// A terminal +Inf is implicit: Prometheus appends that bucket itself and
	// strips a supplied one, so a declaration carrying it and one omitting it
	// are the same histogram -- and reporting them as a conflict rejected a
	// perfectly valid second registration.
	//
	// Stripped AFTER the empty check, never before: []float64{+Inf} is not an
	// empty slice to Prometheus either. Empty means DefBuckets; a lone +Inf
	// means no finite bounds at all, which is a different histogram.
	if math.IsInf(buckets[len(buckets)-1], +1) {
		buckets = buckets[:len(buckets)-1]
	}
	if slices.Equal(buckets, prometheus.DefBuckets) {
		return "buckets=default"
	}
	parts := make([]string, len(buckets))
	for i, b := range buckets {
		parts[i] = strconv.FormatFloat(b, 'g', -1, 64)
	}
	return "buckets=[" + strings.Join(parts, ",") + "]"
}

// objectiveShape fingerprints summary objectives.
//
// No canonicalisation here, deliberately, and NOT by analogy with
// bucketShape. Empty buckets really are prometheus.DefBuckets -- the package
// declares that variable and newHistogram assigns it -- so the two spellings
// build the same histogram and must fingerprint alike. Summaries are the
// opposite: client_golang has no DefObjectives any more, and empty objectives
// build a noObjectivesSummary, a summary carrying NO quantiles. That is a
// different layout from any explicit quantile map, so collapsing them would
// let two genuinely different summaries pass the conflict check.
//
// Hence "none" rather than "default": the empty case is an absence of
// quantiles, not a default set of them.
func objectiveShape(objectives map[float64]float64) string {
	if len(objectives) == 0 {
		return "objectives=none"
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
