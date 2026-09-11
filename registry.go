// Package metrics provides a unified Prometheus metrics toolkit for Go services.
// It includes registry management, metric builders, HTTP handlers, and middleware.
package metrics

import (
	"errors"
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

	// mu protects concurrent access to collectors map
	mu sync.RWMutex

	// collectors tracks registered collectors for Reset/Unregister
	collectors map[string]prometheus.Collector
}

// NewRegistry creates a new Registry with the given namespace.
// The namespace is typically the service name (e.g., "herald", "stargate").
func NewRegistry(namespace string) *Registry {
	return &Registry{
		registry:   prometheus.NewRegistry(),
		namespace:  namespace,
		collectors: make(map[string]prometheus.Collector),
	}
}

// NewRegistryWithSubsystem creates a new Registry with namespace and subsystem.
func NewRegistryWithSubsystem(namespace, subsystem string) *Registry {
	return &Registry{
		registry:   prometheus.NewRegistry(),
		namespace:  namespace,
		subsystem:  subsystem,
		collectors: make(map[string]prometheus.Collector),
	}
}

// DefaultRegistry returns a Registry wrapping the default Prometheus registry.
func DefaultRegistry() *Registry {
	return &Registry{
		registry:   prometheus.DefaultRegisterer.(*prometheus.Registry),
		namespace:  "",
		collectors: make(map[string]prometheus.Collector),
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
		registry:   r.registry,
		namespace:  r.namespace,
		subsystem:  subsystem,
		collectors: r.collectors,
	}
}

// Register registers a collector with the registry.
// It tracks the collector for later unregistration.
func (r *Registry) Register(name string, collector prometheus.Collector) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.registry.Register(collector); err != nil {
		return err
	}

	r.collectors[name] = collector
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
func (r *Registry) registerOrExisting(c prometheus.Collector) prometheus.Collector {
	if err := r.registry.Register(c); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			return already.ExistingCollector
		}
		panic(err)
	}
	return c
}

// Unregister removes a collector from the registry.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	collector, ok := r.collectors[name]
	if !ok {
		return false
	}

	if r.registry.Unregister(collector) {
		delete(r.collectors, name)
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
