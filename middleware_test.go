package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
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

func TestNewFiberMiddleware(t *testing.T) {
	middleware := NewFiberMiddleware("test")
	require.NotNil(t, middleware)
}

func TestNewFiberMiddlewareWithConfig(t *testing.T) {
	cfg := HTTPMetricsConfig{
		Namespace:               "test",
		Subsystem:               "api",
		SkipPaths:               []string{"/health", "/metrics"},
		IncludeRequestsInFlight: true,
	}

	middleware := NewFiberMiddlewareWithConfig(cfg)
	require.NotNil(t, middleware)
}

func TestFiberMiddleware_SkipPaths(t *testing.T) {
	cfg := HTTPMetricsConfig{
		Namespace: "test_skip",
		Subsystem: "http",
		SkipPaths: []string{"/health", "/metrics"},
	}

	m := NewHTTPMetrics(cfg)
	middleware := m.FiberMiddleware(cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})
	app.Get("/api/users", func(c *fiber.Ctx) error {
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
	cfg := HTTPMetricsConfig{
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

	m := NewHTTPMetrics(cfg)
	middleware := m.FiberMiddleware(cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/users/:id", func(c *fiber.Ctx) error {
		return c.SendString("user")
	})

	req := httptest.NewRequest("GET", "/users/123", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFiberMiddleware_RequestResponseSize(t *testing.T) {
	cfg := HTTPMetricsConfig{
		Namespace:           "test_sizes",
		Subsystem:           "http",
		IncludeRequestSize:  true,
		IncludeResponseSize: true,
	}

	m := NewHTTPMetrics(cfg)
	middleware := m.FiberMiddleware(cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Post("/api/data", func(c *fiber.Ctx) error {
		return c.SendString("response data here")
	})

	req := httptest.NewRequest("POST", "/api/data", strings.NewReader("request body"))
	req.Header.Set("Content-Type", "text/plain")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFiberMiddleware_InFlightRequests(t *testing.T) {
	cfg := HTTPMetricsConfig{
		Namespace:               "test_inflight",
		Subsystem:               "http",
		IncludeRequestsInFlight: true,
	}

	m := NewHTTPMetrics(cfg)
	require.NotNil(t, m.RequestsInFlight)

	middleware := m.FiberMiddleware(cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/api/test", func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFiberMiddleware_ErrorHandling(t *testing.T) {
	cfg := HTTPMetricsConfig{
		Namespace: "test_error",
		Subsystem: "http",
	}

	m := NewHTTPMetrics(cfg)
	middleware := m.FiberMiddleware(cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/api/error", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusInternalServerError, "internal error")
	})

	req := httptest.NewRequest("GET", "/api/error", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestFiberMiddleware_MultipleRequests(t *testing.T) {
	cfg := HTTPMetricsConfig{
		Namespace:               "test_multi",
		Subsystem:               "http",
		IncludeRequestsInFlight: true,
	}

	m := NewHTTPMetrics(cfg)
	middleware := m.FiberMiddleware(cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/api/test", func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})
	app.Post("/api/create", func(c *fiber.Ctx) error {
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
	cfg := HTTPMetricsConfig{
		Namespace:               "test_no_inflight",
		Subsystem:               "http",
		IncludeRequestsInFlight: false,
	}

	m := NewHTTPMetrics(cfg)
	require.Nil(t, m.RequestsInFlight)

	middleware := m.FiberMiddleware(cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/api/test", func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFiberMiddleware_DefaultPathNormalize(t *testing.T) {
	// Default config uses DefaultPathNormalize: /users/1 and /users/2 map to same label /users/:id
	cfg := DefaultHTTPMetricsConfig()
	cfg.Namespace = "test_default_norm"
	cfg.Subsystem = "http"
	m := NewHTTPMetrics(cfg)
	middleware := m.FiberMiddleware(cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/users/:id", func(c *fiber.Ctx) error {
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
	cfg := HTTPMetricsConfig{
		Namespace:         "test_sanitize",
		Subsystem:         "http",
		PathTransformFunc: func(p string) string { return p },
	}
	m := NewHTTPMetrics(cfg)
	middleware := m.FiberMiddleware(cfg)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/*", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/api/foo%0abar", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
