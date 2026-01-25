package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler(t *testing.T) {
	handler := Handler()
	require.NotNil(t, handler)

	req, err := http.NewRequest("GET", "/metrics", http.NoBody)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "# HELP")
	assert.Contains(t, rr.Body.String(), "# TYPE")
}

func TestHandlerFor(t *testing.T) {
	r := NewRegistry("test_handler")

	// Register a test metric
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "test_handler",
		Name:      "requests_total",
		Help:      "Total requests",
	})
	r.MustRegister(counter)
	counter.Inc()

	handler := HandlerFor(r)
	require.NotNil(t, handler)

	req, err := http.NewRequest("GET", "/metrics", http.NoBody)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "test_handler_requests_total")
}

func TestHandlerForGatherer(t *testing.T) {
	r := prometheus.NewRegistry()

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "gatherer_test_counter",
		Help: "A test counter",
	})
	r.MustRegister(counter)
	counter.Add(5)

	handler := HandlerForGatherer(r)
	require.NotNil(t, handler)

	req, err := http.NewRequest("GET", "/metrics", http.NoBody)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "gatherer_test_counter 5")
}

func TestHandler_MultipleRequests(t *testing.T) {
	handler := Handler()
	require.NotNil(t, handler)

	for i := 0; i < 5; i++ {
		req, err := http.NewRequest("GET", "/metrics", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	}
}

func TestHandler_ConcurrentRequests(t *testing.T) {
	handler := Handler()
	require.NotNil(t, handler)

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			req, err := http.NewRequest("GET", "/metrics", http.NoBody)
			if err != nil {
				done <- false
				return
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			done <- rr.Code == http.StatusOK
		}()
	}

	successCount := 0
	for i := 0; i < 10; i++ {
		if <-done {
			successCount++
		}
	}

	assert.Equal(t, 10, successCount)
}

func TestHandler_ResponseHeaders(t *testing.T) {
	handler := Handler()
	require.NotNil(t, handler)

	req, err := http.NewRequest("GET", "/metrics", http.NoBody)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	contentType := rr.Header().Get("Content-Type")
	// Prometheus metrics endpoint can return various content types
	assert.True(t,
		contentType == "text/plain; version=0.0.4; charset=utf-8" ||
			contentType == "text/plain; charset=utf-8" ||
			contentType == "application/openmetrics-text; version=1.0.0; charset=utf-8" ||
			strings.Contains(contentType, "text/plain") ||
			strings.Contains(contentType, "openmetrics"),
		"Unexpected Content-Type: %s", contentType)
}

func TestDefaultHandlerOpts(t *testing.T) {
	opts := DefaultHandlerOpts()
	assert.True(t, opts.EnableOpenMetrics)
	assert.Nil(t, opts.Registry)
}

func TestNewHandler(t *testing.T) {
	opts := DefaultHandlerOpts()
	handler := NewHandler(opts)
	require.NotNil(t, handler)

	req, err := http.NewRequest("GET", "/metrics", http.NoBody)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestNewHandler_WithRegistry(t *testing.T) {
	r := NewRegistry("test_new_handler")
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "test_new_handler",
		Name:      "custom_counter",
		Help:      "A custom counter",
	})
	r.MustRegister(counter)
	counter.Add(42)

	opts := HandlerOpts{
		Registry:          r,
		EnableOpenMetrics: true,
	}

	handler := NewHandler(opts)
	require.NotNil(t, handler)

	req, err := http.NewRequest("GET", "/metrics", http.NoBody)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "test_new_handler_custom_counter 42")
}

func TestRegisterHTTPHandler(t *testing.T) {
	mux := http.NewServeMux()
	RegisterHTTPHandler(mux, "/metrics")

	req, err := http.NewRequest("GET", "/metrics", http.NoBody)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRegisterHTTPHandlerFor(t *testing.T) {
	r := NewRegistry("test_register")
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "test_register",
		Name:      "test_metric",
		Help:      "A test metric",
	})
	r.MustRegister(counter)
	counter.Inc()

	mux := http.NewServeMux()
	RegisterHTTPHandlerFor(mux, "/metrics", r)

	req, err := http.NewRequest("GET", "/metrics", http.NoBody)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "test_register_test_metric 1")
}

func TestFiberHandler(t *testing.T) {
	handler := FiberHandler()
	require.NotNil(t, handler)

	app := fiber.New()
	app.Get("/metrics", handler)

	req := httptest.NewRequest("GET", "/metrics", http.NoBody)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFiberHandlerFor(t *testing.T) {
	r := NewRegistry("test_fiber_handler_for")
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "test_fiber_handler_for",
		Name:      "requests_total",
		Help:      "Total requests",
	})
	r.MustRegister(counter)
	counter.Inc()

	handler := FiberHandlerFor(r)
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

	handler := FiberHandlerForGatherer(r)
	require.NotNil(t, handler)

	app := fiber.New()
	app.Get("/metrics", handler)

	req := httptest.NewRequest("GET", "/metrics", http.NoBody)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestNewFiberHandler(t *testing.T) {
	opts := DefaultHandlerOpts()
	handler := NewFiberHandler(opts)
	require.NotNil(t, handler)

	app := fiber.New()
	app.Get("/metrics", handler)

	req := httptest.NewRequest("GET", "/metrics", http.NoBody)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestNewFiberHandler_WithRegistry(t *testing.T) {
	r := NewRegistry("test_fiber_new_handler")
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "test_fiber_new_handler",
		Name:      "custom_counter",
		Help:      "A custom counter",
	})
	r.MustRegister(counter)
	counter.Add(42)

	opts := HandlerOpts{
		Registry:          r,
		EnableOpenMetrics: true,
	}

	handler := NewFiberHandler(opts)
	require.NotNil(t, handler)

	app := fiber.New()
	app.Get("/metrics", handler)

	req := httptest.NewRequest("GET", "/metrics", http.NoBody)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
