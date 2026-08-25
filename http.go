package metrics

import (
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns a standard http.Handler for the /metrics endpoint
// using the default Prometheus registry.
func Handler() http.Handler {
	return promhttp.Handler()
}

// HandlerFor returns an http.Handler for the given registry.
func HandlerFor(registry *Registry) http.Handler {
	return promhttp.HandlerFor(registry.Gatherer(), promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// HandlerForGatherer returns an http.Handler for the given Gatherer.
func HandlerForGatherer(gatherer prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// FiberHandler returns a Fiber-compatible handler for the /metrics endpoint
// using the default Prometheus registry.
func FiberHandler() fiber.Handler {
	return adaptor.HTTPHandler(promhttp.Handler())
}

// FiberHandlerFor returns a Fiber-compatible handler for the given registry.
func FiberHandlerFor(registry *Registry) fiber.Handler {
	return adaptor.HTTPHandler(promhttp.HandlerFor(registry.Gatherer(), promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))
}

// FiberHandlerForGatherer returns a Fiber-compatible handler for the given Gatherer.
func FiberHandlerForGatherer(gatherer prometheus.Gatherer) fiber.Handler {
	return adaptor.HTTPHandler(promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))
}

// HandlerOpts provides options for configuring metrics handlers.
type HandlerOpts struct {
	// Registry is the custom registry to use. If nil, uses default registry.
	Registry *Registry

	// ErrorHandling defines how errors are handled.
	// See promhttp.HandlerOpts for details.
	ErrorHandling promhttp.HandlerErrorHandling

	// ErrorLog is an optional logger for errors.
	ErrorLog promhttp.Logger

	// EnableOpenMetrics enables OpenMetrics format in addition to standard format.
	EnableOpenMetrics bool

	// DisableCompression disables response compression.
	DisableCompression bool

	// MaxRequestsInFlight limits concurrent requests. 0 means no limit.
	MaxRequestsInFlight int

	// Timeout specifies the maximum time for a request in seconds.
	// When > 0, the handler is wrapped with http.TimeoutHandler; 0 means no timeout.
	Timeout int
}

// DefaultHandlerOpts returns the default handler options.
func DefaultHandlerOpts() HandlerOpts {
	return HandlerOpts{
		EnableOpenMetrics: true,
	}
}

// NewHandler creates a new metrics HTTP handler with the given options.
// When Timeout is greater than 0, the handler is wrapped with http.TimeoutHandler;
// timed-out requests respond with 503 Service Unavailable.
func NewHandler(opts HandlerOpts) http.Handler {
	promOpts := promhttp.HandlerOpts{
		ErrorHandling:       opts.ErrorHandling,
		ErrorLog:            opts.ErrorLog,
		EnableOpenMetrics:   opts.EnableOpenMetrics,
		DisableCompression:  opts.DisableCompression,
		MaxRequestsInFlight: opts.MaxRequestsInFlight,
	}

	var h http.Handler
	if opts.Registry != nil {
		h = promhttp.HandlerFor(opts.Registry.Gatherer(), promOpts)
	} else {
		h = promhttp.HandlerFor(prometheus.DefaultGatherer, promOpts)
	}
	if opts.Timeout > 0 {
		h = http.TimeoutHandler(h, time.Duration(opts.Timeout)*time.Second, "metrics scrape timeout\n")
	}
	return h
}

// NewFiberHandler creates a new Fiber metrics handler with the given options.
func NewFiberHandler(opts HandlerOpts) fiber.Handler {
	return adaptor.HTTPHandler(NewHandler(opts))
}

// RegisterHTTPHandler registers the metrics handler on an http.ServeMux.
func RegisterHTTPHandler(mux *http.ServeMux, path string) {
	mux.Handle(path, Handler())
}

// RegisterHTTPHandlerFor registers a metrics handler for the given registry.
func RegisterHTTPHandlerFor(mux *http.ServeMux, path string, registry *Registry) {
	mux.Handle(path, HandlerFor(registry))
}
