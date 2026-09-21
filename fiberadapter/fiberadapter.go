// Package fiberadapter exposes metrics-kit over Fiber v3.
//
// It lives in its own package so that importing the root package does not drag
// Fiber -- and with it fasthttp -- into binaries that never use it. A service
// on net/http, Echo, Gin or chi pays nothing for Fiber support existing; only
// importing this package links it in.
//
// The /metrics handlers here are the root package's handlers put through
// Fiber's adaptor; the middleware reads its label rules (path normalization,
// value sanitizing) from the root package rather than restating them, because
// label cardinality is the whole point of that step.
package fiberadapter

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus"

	metrics "github.com/soulteary/metrics-kit/v3"
)

// Handler returns a Fiber handler for the /metrics endpoint using the default
// Prometheus registry. It is the Fiber counterpart of metrics.Handler.
func Handler() fiber.Handler {
	return adaptor.HTTPHandler(metrics.Handler())
}

// HandlerFor returns a Fiber handler for the given registry.
func HandlerFor(registry *metrics.Registry) fiber.Handler {
	return adaptor.HTTPHandler(metrics.HandlerFor(registry))
}

// HandlerForGatherer returns a Fiber handler for the given Gatherer.
func HandlerForGatherer(gatherer prometheus.Gatherer) fiber.Handler {
	return adaptor.HTTPHandler(metrics.HandlerForGatherer(gatherer))
}

// NewHandler creates a Fiber metrics handler with the given options.
func NewHandler(opts metrics.HandlerOpts) fiber.Handler {
	return adaptor.HTTPHandler(metrics.NewHandler(opts))
}

// NewMiddleware creates a Fiber middleware with default configuration, and
// returns the registry its metrics were recorded into.
//
// The registry is half the return value, not a convenience. These
// constructors build the HTTPMetrics themselves, and with no Registry in the
// config NewHTTPMetrics makes one; returning only the handler dropped the
// only reference to it, so what the middleware recorded could be scraped by
// nobody -- Handler() serves the DEFAULT registry, which is not that one.
// Serve what this middleware collects with HandlerFor(reg).
//
// Passing a config that names a Registry hands that same one back.
func NewMiddleware(namespace string) (fiber.Handler, *metrics.Registry) {
	cfg := metrics.DefaultHTTPMetricsConfig()
	cfg.Namespace = namespace
	return NewMiddlewareWithConfig(cfg)
}

// NewMiddlewareWithConfig creates a Fiber middleware with custom
// configuration, and returns the registry its metrics were recorded into.
// See NewMiddleware for why the registry comes back with the handler.
func NewMiddlewareWithConfig(cfg metrics.HTTPMetricsConfig) (fiber.Handler, *metrics.Registry) {
	m := metrics.NewHTTPMetrics(cfg)
	return Middleware(m, cfg), m.Registry
}

// Middleware returns a Fiber middleware that collects HTTP metrics around
// every request.
func Middleware(m *metrics.HTTPMetrics, cfg metrics.HTTPMetricsConfig) fiber.Handler {
	skipPathMap := make(map[string]bool)
	for _, p := range cfg.SkipPaths {
		skipPathMap[p] = true
	}

	return func(c fiber.Ctx) error {
		path := c.Path()

		// Skip metrics collection for specified paths
		if skipPathMap[path] {
			return c.Next()
		}

		// Copy before the strings outlive the request.
		//
		// c.Path() and c.Method() are unsafe views onto fasthttp's pooled
		// request buffer -- Fiber only guarantees them until the handler
		// returns, which is what its Immutable config option is for. A
		// Prometheus label value is retained for the life of the process, so
		// keeping the view lets the NEXT request rewrite an already-recorded
		// label in place: two vec entries then report the same label set,
		// Gather() fails with a duplicate, and promhttp answers /metrics with
		// a 500 from then on -- the whole endpoint, not just these series.
		//
		// It only shows when nothing downstream happens to allocate.
		// DefaultPathNormalize rebuilds the path and hides it; the raw-path
		// opt-out and any PathTransformFunc returning its argument unchanged
		// do not. Cloning here rather than after the transform also spares a
		// user-supplied PathTransformFunc a string it must not keep.
		//
		// After the skip check, so a skipped path still costs no allocation.
		path = cfg.TransformPath(strings.Clone(path))
		path = metrics.SanitizeLabelValue(path, metrics.DefaultLabelValueMaxLength)
		method := metrics.SanitizeLabelValue(strings.Clone(c.Method()), metrics.DefaultLabelValueMaxLength)

		// Track in-flight requests
		if m.RequestsInFlight != nil {
			m.RequestsInFlight.Inc()
			defer m.RequestsInFlight.Dec()
		}

		// Record request size
		if m.RequestSize != nil {
			// Content-Length rather than len(c.Body()): reading the body here
			// materialises it in full for every request, including ones the
			// handler streams or never reads, and runs before any body-size
			// limit further down the chain.
			// A negative Content-Length means the length is UNKNOWN --
			// a streamed or chunked upload. Recording it as zero increments
			// the histogram count while contributing nothing to its sum and
			// lowest bucket, systematically understating request sizes, so
			// the observation is skipped instead.
			if size := c.Request().Header.ContentLength(); size >= 0 {
				m.RequestSize.WithLabelValues(method, path).Observe(float64(size))
			}
		}

		start := time.Now()

		// Process request
		err := c.Next()

		// Record metrics
		duration := time.Since(start).Seconds()
		status := metrics.SanitizeLabelValue(strconv.Itoa(statusCode(c, err)), metrics.DefaultLabelValueMaxLength)

		m.RequestsTotal.WithLabelValues(method, path, status).Inc()
		m.RequestDuration.WithLabelValues(method, path).Observe(duration)

		// Record response size
		if m.ResponseSize != nil {
			m.ResponseSize.WithLabelValues(method, path).Observe(float64(len(c.Response().Body())))
		}

		return err
	}
}

// statusCode is the status the client will actually see.
//
// Fiber runs app.ErrorHandler AFTER the whole middleware chain unwinds, so at
// this point a handler that returned an error has not set a status yet and
// c.Response().StatusCode() still reads 200 -- every failed request would be
// counted as a success, and an alert on requests_total{status=~"5.."} would
// never fire.
//
// The error's own code is used instead, the way Fiber's DefaultErrorHandler
// derives it. Running app.ErrorHandler here to get the definitive status (what
// Fiber's logger middleware does) is deliberately NOT done: the handler would
// then run twice for anyone pairing this with that logger.
//
// The residue is a custom ErrorHandler mapping a non-fiber.Error to something
// other than 500 -- recorded as 500 rather than its real code. Still an error,
// which is the distinction the metric exists to make.
func statusCode(c fiber.Ctx, err error) int {
	if err == nil {
		return c.Response().StatusCode()
	}
	var fe *fiber.Error
	if errors.As(err, &fe) {
		return fe.Code
	}
	return fiber.StatusInternalServerError
}
