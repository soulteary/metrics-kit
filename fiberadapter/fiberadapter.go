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
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus"

	metrics "github.com/soulteary/metrics-kit/v2"
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

// NewMiddleware creates a Fiber middleware with default configuration.
func NewMiddleware(namespace string) fiber.Handler {
	cfg := metrics.DefaultHTTPMetricsConfig()
	cfg.Namespace = namespace
	return Middleware(metrics.NewHTTPMetrics(cfg), cfg)
}

// NewMiddlewareWithConfig creates a Fiber middleware with custom configuration.
func NewMiddlewareWithConfig(cfg metrics.HTTPMetricsConfig) fiber.Handler {
	return Middleware(metrics.NewHTTPMetrics(cfg), cfg)
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

		path = cfg.TransformPath(path)
		path = metrics.SanitizeLabelValue(path, metrics.DefaultLabelValueMaxLength)
		method := metrics.SanitizeLabelValue(c.Method(), metrics.DefaultLabelValueMaxLength)

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
		status := metrics.SanitizeLabelValue(strconv.Itoa(c.Response().StatusCode()), metrics.DefaultLabelValueMaxLength)

		m.RequestsTotal.WithLabelValues(method, path, status).Inc()
		m.RequestDuration.WithLabelValues(method, path).Observe(duration)

		// Record response size
		if m.ResponseSize != nil {
			m.ResponseSize.WithLabelValues(method, path).Observe(float64(len(c.Response().Body())))
		}

		return err
	}
}
