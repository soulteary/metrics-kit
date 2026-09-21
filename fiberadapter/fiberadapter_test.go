// Fiber adapter tests, moved here from the root package together with the
// handlers and middleware they cover. External test package on purpose: they
// compile only against metrics-kit's exported API, which is what an
// out-of-tree adapter has.
package fiberadapter_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metrics "github.com/soulteary/metrics-kit/v2"
	"github.com/soulteary/metrics-kit/v2/fiberadapter"
)

func TestNewFiberMiddleware(t *testing.T) {
	middleware := fiberadapter.NewMiddleware("test")
	require.NotNil(t, middleware)
}

func TestNewFiberMiddlewareWithConfig(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace:               "test",
		Subsystem:               "api",
		SkipPaths:               []string{"/health", "/metrics"},
		IncludeRequestsInFlight: true,
	}

	middleware := fiberadapter.NewMiddlewareWithConfig(cfg)
	require.NotNil(t, middleware)
}

func TestFiberMiddleware_SkipPaths(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace: "test_skip",
		Subsystem: "http",
		SkipPaths: []string{"/health", "/metrics"},
	}

	m := metrics.NewHTTPMetrics(cfg)
	middleware := fiberadapter.Middleware(m, cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/health", func(c fiber.Ctx) error {
		return c.SendString("OK")
	})
	app.Get("/api/users", func(c fiber.Ctx) error {
		return c.SendString("users")
	})

	// Test skipped path
	req := httptest.NewRequest("GET", "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Test non-skipped path
	req2 := httptest.NewRequest("GET", "/api/users", nil)
	resp2, err := app.Test(req2)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}

func TestFiberMiddleware_PathTransformFunc(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace: "test_transform",
		Subsystem: "http",
		PathTransformFunc: func(path string) string {
			// Normalize /users/123 to /users/:id
			if len(path) > 7 && path[:7] == "/users/" {
				return "/users/:id"
			}
			return path
		},
	}

	m := metrics.NewHTTPMetrics(cfg)
	middleware := fiberadapter.Middleware(m, cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/users/:id", func(c fiber.Ctx) error {
		return c.SendString("user")
	})

	req := httptest.NewRequest("GET", "/users/123", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFiberMiddleware_RequestResponseSize(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace:           "test_sizes",
		Subsystem:           "http",
		IncludeRequestSize:  true,
		IncludeResponseSize: true,
	}

	m := metrics.NewHTTPMetrics(cfg)
	middleware := fiberadapter.Middleware(m, cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Post("/api/data", func(c fiber.Ctx) error {
		return c.SendString("response data here")
	})

	req := httptest.NewRequest("POST", "/api/data", strings.NewReader("request body"))
	req.Header.Set("Content-Type", "text/plain")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFiberMiddleware_InFlightRequests(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace:               "test_inflight",
		Subsystem:               "http",
		IncludeRequestsInFlight: true,
	}

	m := metrics.NewHTTPMetrics(cfg)
	require.NotNil(t, m.RequestsInFlight)

	middleware := fiberadapter.Middleware(m, cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/api/test", func(c fiber.Ctx) error {
		return c.SendString("OK")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFiberMiddleware_ErrorHandling(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace: "test_error",
		Subsystem: "http",
	}

	m := metrics.NewHTTPMetrics(cfg)
	middleware := fiberadapter.Middleware(m, cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/api/error", func(c fiber.Ctx) error {
		return fiber.NewError(fiber.StatusInternalServerError, "internal error")
	})

	req := httptest.NewRequest("GET", "/api/error", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestFiberMiddleware_MultipleRequests(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace:               "test_multi",
		Subsystem:               "http",
		IncludeRequestsInFlight: true,
	}

	m := metrics.NewHTTPMetrics(cfg)
	middleware := fiberadapter.Middleware(m, cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/api/test", func(c fiber.Ctx) error {
		return c.SendString("OK")
	})
	app.Post("/api/create", func(c fiber.Ctx) error {
		return c.Status(fiber.StatusCreated).SendString("created")
	})

	// Multiple GET requests
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// POST request
	req := httptest.NewRequest("POST", "/api/create", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestFiberMiddleware_NoInFlightGauge(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace:               "test_no_inflight",
		Subsystem:               "http",
		IncludeRequestsInFlight: false,
	}

	m := metrics.NewHTTPMetrics(cfg)
	require.Nil(t, m.RequestsInFlight)

	middleware := fiberadapter.Middleware(m, cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/api/test", func(c fiber.Ctx) error {
		return c.SendString("OK")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFiberMiddleware_DefaultPathNormalize(t *testing.T) {
	// Default config uses DefaultPathNormalize: /users/1 and /users/2 map to same label /users/:id
	cfg := metrics.DefaultHTTPMetricsConfig()
	cfg.Namespace = "test_default_norm"
	cfg.Subsystem = "http"
	m := metrics.NewHTTPMetrics(cfg)
	middleware := fiberadapter.Middleware(m, cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/users/:id", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req1 := httptest.NewRequest("GET", "/users/1", nil)
	resp1, err := app.Test(req1)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp1.StatusCode)

	req2 := httptest.NewRequest("GET", "/users/2", nil)
	resp2, err := app.Test(req2)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}

func TestFiberMiddleware_SanitizeLabelValue(t *testing.T) {
	// Path with newline should be sanitized so exposition format is not broken
	cfg := metrics.HTTPMetricsConfig{
		Namespace:         "test_sanitize",
		Subsystem:         "http",
		PathTransformFunc: func(p string) string { return p },
	}
	m := metrics.NewHTTPMetrics(cfg)
	middleware := fiberadapter.Middleware(m, cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/*", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/api/foo%0abar", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFiberHandler(t *testing.T) {
	handler := fiberadapter.Handler()
	require.NotNil(t, handler)

	app := fiber.New()
	app.Get("/metrics", handler)

	req := httptest.NewRequest("GET", "/metrics", http.NoBody)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFiberHandlerFor(t *testing.T) {
	r := metrics.NewRegistry("test_fiber_handler_for")
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "test_fiber_handler_for",
		Name:      "requests_total",
		Help:      "Total requests",
	})
	r.MustRegister(counter)
	counter.Inc()

	handler := fiberadapter.HandlerFor(r)
	require.NotNil(t, handler)

	app := fiber.New()
	app.Get("/metrics", handler)

	req := httptest.NewRequest("GET", "/metrics", http.NoBody)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFiberHandlerForGatherer(t *testing.T) {
	r := prometheus.NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "fiber_gatherer_test_counter",
		Help: "A test counter",
	})
	r.MustRegister(counter)
	counter.Add(10)

	handler := fiberadapter.HandlerForGatherer(r)
	require.NotNil(t, handler)

	app := fiber.New()
	app.Get("/metrics", handler)

	req := httptest.NewRequest("GET", "/metrics", http.NoBody)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestNewFiberHandler(t *testing.T) {
	opts := metrics.DefaultHandlerOpts()
	handler := fiberadapter.NewHandler(opts)
	require.NotNil(t, handler)

	app := fiber.New()
	app.Get("/metrics", handler)

	req := httptest.NewRequest("GET", "/metrics", http.NoBody)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestNewFiberHandler_WithRegistry(t *testing.T) {
	r := metrics.NewRegistry("test_fiber_new_handler")
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "test_fiber_new_handler",
		Name:      "custom_counter",
		Help:      "A custom counter",
	})
	r.MustRegister(counter)
	counter.Add(42)

	opts := metrics.HandlerOpts{
		Registry:          r,
		EnableOpenMetrics: true,
	}

	handler := fiberadapter.NewHandler(opts)
	require.NotNil(t, handler)

	app := fiber.New()
	app.Get("/metrics", handler)

	req := httptest.NewRequest("GET", "/metrics", http.NoBody)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
