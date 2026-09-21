// Fiber adapter tests, moved here from the root package together with the
// handlers and middleware they cover. External test package on purpose: they
// compile only against metrics-kit's exported API, which is what an
// out-of-tree adapter has.
//
// Every middleware test gathers from a registry it owns and asserts on the
// SAMPLES -- label sets and values -- not just on the HTTP status. A metrics
// middleware that records nothing, or records the wrong path or status, still
// serves a correct 200; asserting only the status leaves the entire point of
// the package unverified.
package fiberadapter_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metrics "github.com/soulteary/metrics-kit/v3"
	"github.com/soulteary/metrics-kit/v3/fiberadapter"
)

// --- helpers -------------------------------------------------------------

// gather returns every sample of the metric family `name` in reg, or nil when
// the family was never populated.
func gather(t *testing.T, reg *metrics.Registry, name string) []*dto.Metric {
	t.Helper()
	mfs, err := reg.Gatherer().Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf.GetMetric()
		}
	}
	return nil
}

func labelsOf(m *dto.Metric) map[string]string {
	out := make(map[string]string, len(m.GetLabel()))
	for _, l := range m.GetLabel() {
		out[l.GetName()] = l.GetValue()
	}
	return out
}

// sample returns the one sample of `name` whose labels include want, failing
// if there is not exactly one. Requiring exactly one is what makes a test
// notice a middleware that stopped normalizing paths: the extra series shows
// up here rather than passing silently.
func sample(t *testing.T, reg *metrics.Registry, name string, want map[string]string) *dto.Metric {
	t.Helper()
	var hits []*dto.Metric
	var seen []map[string]string
	for _, m := range gather(t, reg, name) {
		got := labelsOf(m)
		seen = append(seen, got)
		matched := true
		for k, v := range want {
			if got[k] != v {
				matched = false
				break
			}
		}
		if matched {
			hits = append(hits, m)
		}
	}
	require.Lenf(t, hits, 1, "want exactly one %s sample matching %v; series present: %v", name, want, seen)
	return hits[0]
}

// labelValues lists, sorted and deduplicated, the values of `label` across
// every sample of `name`. Used to assert on the whole series set at once --
// that a skipped path produced none, that two requests collapsed into one.
func labelValues(t *testing.T, reg *metrics.Registry, name, label string) []string {
	t.Helper()
	uniq := map[string]bool{}
	for _, m := range gather(t, reg, name) {
		uniq[labelsOf(m)[label]] = true
	}
	out := make([]string, 0, len(uniq))
	for v := range uniq {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func counterValue(t *testing.T, reg *metrics.Registry, name string, want map[string]string) float64 {
	t.Helper()
	return sample(t, reg, name, want).GetCounter().GetValue()
}

func histogram(t *testing.T, reg *metrics.Registry, name string, want map[string]string) (count uint64, sum float64) {
	t.Helper()
	h := sample(t, reg, name, want).GetHistogram()
	return h.GetSampleCount(), h.GetSampleSum()
}

func gaugeValue(t *testing.T, reg *metrics.Registry, name string) float64 {
	t.Helper()
	return sample(t, reg, name, nil).GetGauge().GetValue()
}

// newApp mounts the middleware for cfg on a fresh Fiber app and returns both
// the app and the registry the samples land in, so a test can assert on them.
func newApp(t *testing.T, cfg metrics.HTTPMetricsConfig) (*fiber.App, *metrics.Registry) {
	t.Helper()
	require.NotEmpty(t, cfg.Namespace, "give the config a namespace so metric names are unambiguous")
	if cfg.Registry == nil {
		cfg.Registry = metrics.NewRegistryWithSubsystem(cfg.Namespace, cfg.Subsystem)
	}
	m := metrics.NewHTTPMetrics(cfg)
	app := fiber.New()
	app.Use(fiberadapter.Middleware(m, cfg))
	return app, cfg.Registry
}

func do(t *testing.T, app *fiber.App, req *http.Request, wantStatus int) *http.Response {
	t.Helper()
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, wantStatus, resp.StatusCode)
	return resp
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return string(b)
}

// --- middleware constructors ---------------------------------------------

func TestNewFiberMiddleware(t *testing.T) {
	middleware := fiberadapter.NewMiddleware("test")
	require.NotNil(t, middleware)

	// It has to be a working handler, not merely non-nil. Its metrics land in
	// a registry NewMiddleware creates and drops, so there is nothing to
	// gather here -- see TestNewMiddlewareMetricsAreUnreachable.
	app := fiber.New()
	app.Use(middleware)
	app.Get("/api/test", func(c fiber.Ctx) error { return c.SendString("OK") })
	do(t, app, httptest.NewRequest("GET", "/api/test", nil), http.StatusOK)
}

// TestNewMiddlewareMetricsAreUnreachable pins the one thing a caller of
// NewMiddleware needs to know: it builds its own registry and keeps no
// reference to it, so the samples it records cannot be scraped. Handler()
// serves the DEFAULT registry, which is not that one.
//
// Pass a config carrying a Registry -- NewMiddlewareWithConfig, or Middleware
// over your own HTTPMetrics -- if you intend to export what you record.
func TestNewMiddlewareMetricsAreUnreachable(t *testing.T) {
	app := fiber.New()
	app.Use(fiberadapter.NewMiddleware("unreachable_ns"))
	app.Get("/api/test", func(c fiber.Ctx) error { return c.SendString("OK") })
	do(t, app, httptest.NewRequest("GET", "/api/test", nil), http.StatusOK)

	mfs, err := metrics.DefaultRegistry().Gatherer().Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		require.NotContains(t, mf.GetName(), "unreachable_ns",
			"NewMiddleware started exporting to the default registry; update its doc comment and this test")
	}
}

func TestNewFiberMiddlewareWithConfig(t *testing.T) {
	reg := metrics.NewRegistryWithSubsystem("test_cfg", "api")
	cfg := metrics.HTTPMetricsConfig{
		Registry:                reg,
		Namespace:               "test_cfg",
		Subsystem:               "api",
		SkipPaths:               []string{"/health", "/metrics"},
		IncludeRequestsInFlight: true,
	}

	middleware := fiberadapter.NewMiddlewareWithConfig(cfg)
	require.NotNil(t, middleware)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/api/users", func(c fiber.Ctx) error { return c.SendString("users") })
	app.Get("/health", func(c fiber.Ctx) error { return c.SendString("OK") })

	do(t, app, httptest.NewRequest("GET", "/api/users", nil), http.StatusOK)
	do(t, app, httptest.NewRequest("GET", "/health", nil), http.StatusOK)

	// The config reached the middleware: its registry holds the sample, and
	// its SkipPaths kept /health out.
	assert.Equal(t, []string{"/api/users"}, labelValues(t, reg, "test_cfg_api_requests_total", "path"))
}

// --- middleware behaviour -------------------------------------------------

func TestFiberMiddleware_SkipPaths(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace: "test_skip",
		Subsystem: "http",
		SkipPaths: []string{"/health", "/metrics"},
	}
	app, reg := newApp(t, cfg)
	app.Get("/health", func(c fiber.Ctx) error { return c.SendString("OK") })
	app.Get("/api/users", func(c fiber.Ctx) error { return c.SendString("users") })

	do(t, app, httptest.NewRequest("GET", "/health", nil), http.StatusOK)
	do(t, app, httptest.NewRequest("GET", "/api/users", nil), http.StatusOK)

	// A skipped path is served normally and recorded NOWHERE -- not as a
	// series with a zero count, not under a normalized name.
	assert.Equal(t, []string{"/api/users"}, labelValues(t, reg, "test_skip_http_requests_total", "path"))
	assert.Equal(t, []string{"/api/users"}, labelValues(t, reg, "test_skip_http_request_duration_seconds", "path"))
}

func TestFiberMiddleware_PathTransformFunc(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace: "test_transform",
		Subsystem: "http",
		PathTransformFunc: func(path string) string {
			if strings.HasPrefix(path, "/users/") {
				return "/users/:id"
			}
			return path
		},
	}
	app, reg := newApp(t, cfg)
	app.Get("/users/:id", func(c fiber.Ctx) error { return c.SendString("user") })

	do(t, app, httptest.NewRequest("GET", "/users/123", nil), http.StatusOK)
	do(t, app, httptest.NewRequest("GET", "/users/456", nil), http.StatusOK)

	// Both requests collapse onto the transformed label, which is the only
	// series present -- the raw paths never became labels of their own.
	assert.Equal(t, []string{"/users/:id"}, labelValues(t, reg, "test_transform_http_requests_total", "path"))
	assert.Equal(t, 2.0, counterValue(t, reg, "test_transform_http_requests_total",
		map[string]string{"method": "GET", "path": "/users/:id", "status": "200"}))
}

func TestFiberMiddleware_RequestResponseSize(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace:           "test_sizes",
		Subsystem:           "http",
		IncludeRequestSize:  true,
		IncludeResponseSize: true,
	}
	app, reg := newApp(t, cfg)
	app.Post("/api/data", func(c fiber.Ctx) error { return c.SendString("response data here") })

	req := httptest.NewRequest("POST", "/api/data", strings.NewReader("request body"))
	req.Header.Set("Content-Type", "text/plain")
	do(t, app, req, http.StatusOK)

	lbl := map[string]string{"method": "POST", "path": "/api/data"}
	reqCount, reqSum := histogram(t, reg, "test_sizes_http_request_size_bytes", lbl)
	assert.Equal(t, uint64(1), reqCount)
	assert.Equal(t, float64(len("request body")), reqSum, "request size is the declared Content-Length")

	respCount, respSum := histogram(t, reg, "test_sizes_http_response_size_bytes", lbl)
	assert.Equal(t, uint64(1), respCount)
	assert.Equal(t, float64(len("response data here")), respSum)
}

// TestFiberMiddleware_UnknownRequestSizeIsNotObserved covers the branch the
// implementation comment is about: fasthttp reports a negative
// Content-Length when the body length is UNKNOWN (no header at all, or a
// chunked upload). Observing that as zero would inflate the histogram's count
// while adding nothing to its sum, quietly understating request sizes, so no
// observation is made.
//
// Statement coverage cannot see this: the guarded line runs on every sized
// request, so the file reads as 100% covered with the branch never taken.
func TestFiberMiddleware_UnknownRequestSizeIsNotObserved(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace:          "test_unknown_size",
		Subsystem:          "http",
		IncludeRequestSize: true,
	}
	app, reg := newApp(t, cfg)
	app.Get("/api/test", func(c fiber.Ctx) error { return c.SendString("OK") })

	req := httptest.NewRequest("GET", "/api/test", nil)
	require.Empty(t, req.Header.Get("Content-Length"), "this test needs a request that declares no body length")
	do(t, app, req, http.StatusOK)

	// The request was counted...
	assert.Equal(t, 1.0, counterValue(t, reg, "test_unknown_size_http_requests_total",
		map[string]string{"method": "GET", "path": "/api/test", "status": "200"}))
	// ...but contributed no size observation at all, not even a zero one.
	assert.Empty(t, gather(t, reg, "test_unknown_size_http_request_size_bytes"),
		"an unknown Content-Length must not be observed as zero")
}

func TestFiberMiddleware_InFlightRequests(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace:               "test_inflight",
		Subsystem:               "http",
		IncludeRequestsInFlight: true,
	}
	app, reg := newApp(t, cfg)

	var duringRequest float64
	app.Get("/api/test", func(c fiber.Ctx) error {
		duringRequest = gaugeValue(t, reg, "test_inflight_http_requests_in_flight")
		return c.SendString("OK")
	})

	do(t, app, httptest.NewRequest("GET", "/api/test", nil), http.StatusOK)

	assert.Equal(t, 1.0, duringRequest, "gauge must be up while the handler runs")
	assert.Equal(t, 0.0, gaugeValue(t, reg, "test_inflight_http_requests_in_flight"),
		"gauge must come back down -- a leak here reads as permanent load")
}

func TestFiberMiddleware_InFlightIsDecrementedOnPanic(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace:               "test_inflight_panic",
		Subsystem:               "http",
		IncludeRequestsInFlight: true,
	}
	cfg.Registry = metrics.NewRegistryWithSubsystem(cfg.Namespace, cfg.Subsystem)
	m := metrics.NewHTTPMetrics(cfg)

	app := fiber.New()
	// Recover sits OUTSIDE the metrics middleware, so the panic unwinds
	// through it exactly as it would in production.
	app.Use(func(c fiber.Ctx) error {
		defer func() { _ = recover() }()
		return c.Next()
	})
	app.Use(fiberadapter.Middleware(m, cfg))
	app.Get("/boom", func(c fiber.Ctx) error { panic("handler exploded") })

	_, err := app.Test(httptest.NewRequest("GET", "/boom", nil))
	require.NoError(t, err)

	assert.Equal(t, 0.0, gaugeValue(t, cfg.Registry, "test_inflight_panic_http_requests_in_flight"),
		"the deferred Dec must survive a panicking handler")
}

// TestFiberMiddleware_ErrorHandling is the regression test for the status
// label. Fiber runs app.ErrorHandler after the middleware chain unwinds, so
// reading c.Response().StatusCode() right after c.Next() sees 200 for a
// request the client received a 500 for -- every failure counted as a
// success, and an alert on status=~"5.." silent forever.
func TestFiberMiddleware_ErrorHandling(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantLabel  string
	}{
		{"fiber error 500", fiber.NewError(fiber.StatusInternalServerError, "internal error"), 500, "500"},
		{"fiber error 404", fiber.NewError(fiber.StatusNotFound, "nope"), 404, "404"},
		{"wrapped fiber error", errors.Join(fiber.NewError(fiber.StatusBadGateway, "upstream")), 502, "502"},
		{"plain error", errors.New("boom"), 500, "500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := metrics.HTTPMetricsConfig{
				Namespace: "test_error_" + strings.ReplaceAll(tc.name, " ", "_"),
				Subsystem: "http",
			}
			app, reg := newApp(t, cfg)
			app.Get("/api/error", func(c fiber.Ctx) error { return tc.err })

			do(t, app, httptest.NewRequest("GET", "/api/error", nil), tc.wantStatus)

			assert.Equal(t, 1.0, counterValue(t, reg, cfg.Namespace+"_http_requests_total",
				map[string]string{"method": "GET", "path": "/api/error", "status": tc.wantLabel}),
				"the status label must be the status the client saw")
		})
	}
}

func TestFiberMiddleware_MultipleRequests(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace:               "test_multi",
		Subsystem:               "http",
		IncludeRequestsInFlight: true,
	}
	app, reg := newApp(t, cfg)
	app.Get("/api/test", func(c fiber.Ctx) error { return c.SendString("OK") })
	app.Post("/api/create", func(c fiber.Ctx) error {
		return c.Status(fiber.StatusCreated).SendString("created")
	})

	for i := 0; i < 5; i++ {
		do(t, app, httptest.NewRequest("GET", "/api/test", nil), http.StatusOK)
	}
	do(t, app, httptest.NewRequest("POST", "/api/create", nil), http.StatusCreated)

	assert.Equal(t, 5.0, counterValue(t, reg, "test_multi_http_requests_total",
		map[string]string{"method": "GET", "path": "/api/test", "status": "200"}))
	assert.Equal(t, 1.0, counterValue(t, reg, "test_multi_http_requests_total",
		map[string]string{"method": "POST", "path": "/api/create", "status": "201"}),
		"a non-default status must reach the label")

	count, _ := histogram(t, reg, "test_multi_http_request_duration_seconds",
		map[string]string{"method": "GET", "path": "/api/test"})
	assert.Equal(t, uint64(5), count, "duration is observed once per request")

	assert.Equal(t, 0.0, gaugeValue(t, reg, "test_multi_http_requests_in_flight"))
}

func TestFiberMiddleware_NoInFlightGauge(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace:               "test_no_inflight",
		Subsystem:               "http",
		IncludeRequestsInFlight: false,
	}
	cfg.Registry = metrics.NewRegistryWithSubsystem(cfg.Namespace, cfg.Subsystem)
	m := metrics.NewHTTPMetrics(cfg)
	require.Nil(t, m.RequestsInFlight)

	app := fiber.New()
	app.Use(fiberadapter.Middleware(m, cfg))
	app.Get("/api/test", func(c fiber.Ctx) error { return c.SendString("OK") })

	do(t, app, httptest.NewRequest("GET", "/api/test", nil), http.StatusOK)

	// The nil gauge is skipped, and everything else still recorded.
	assert.Empty(t, gather(t, cfg.Registry, "test_no_inflight_http_requests_in_flight"))
	assert.Equal(t, 1.0, counterValue(t, cfg.Registry, "test_no_inflight_http_requests_total",
		map[string]string{"method": "GET", "path": "/api/test", "status": "200"}))
}

func TestFiberMiddleware_NoSizeHistograms(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace: "test_no_sizes",
		Subsystem: "http",
	}
	cfg.Registry = metrics.NewRegistryWithSubsystem(cfg.Namespace, cfg.Subsystem)
	m := metrics.NewHTTPMetrics(cfg)
	require.Nil(t, m.RequestSize)
	require.Nil(t, m.ResponseSize)

	app := fiber.New()
	app.Use(fiberadapter.Middleware(m, cfg))
	app.Post("/api/data", func(c fiber.Ctx) error { return c.SendString("body") })

	do(t, app, httptest.NewRequest("POST", "/api/data", strings.NewReader("in")), http.StatusOK)

	assert.Empty(t, gather(t, cfg.Registry, "test_no_sizes_http_request_size_bytes"))
	assert.Empty(t, gather(t, cfg.Registry, "test_no_sizes_http_response_size_bytes"))
	assert.Equal(t, 1.0, counterValue(t, cfg.Registry, "test_no_sizes_http_requests_total",
		map[string]string{"method": "POST", "path": "/api/data", "status": "200"}))
}

func TestFiberMiddleware_DefaultPathNormalize(t *testing.T) {
	cfg := metrics.DefaultHTTPMetricsConfig()
	cfg.Namespace = "test_default_norm"
	cfg.Subsystem = "http"
	app, reg := newApp(t, cfg)
	app.Get("/users/:id", func(c fiber.Ctx) error { return c.SendString("ok") })

	do(t, app, httptest.NewRequest("GET", "/users/1", nil), http.StatusOK)
	do(t, app, httptest.NewRequest("GET", "/users/2", nil), http.StatusOK)

	// The default config normalizes without being asked: two distinct IDs are
	// ONE series. Losing this is how a label set becomes unbounded.
	assert.Equal(t, []string{"/users/:id"}, labelValues(t, reg, "test_default_norm_http_requests_total", "path"))
	assert.Equal(t, 2.0, counterValue(t, reg, "test_default_norm_http_requests_total",
		map[string]string{"method": "GET", "path": "/users/:id", "status": "200"}))
}

func TestFiberMiddleware_DisablePathNormalization(t *testing.T) {
	cfg := metrics.DefaultHTTPMetricsConfig()
	cfg.Namespace = "test_raw_path"
	cfg.Subsystem = "http"
	cfg.PathTransformFunc = nil
	cfg.DisablePathNormalization = true
	app, reg := newApp(t, cfg)
	app.Get("/users/:id", func(c fiber.Ctx) error { return c.SendString("ok") })

	do(t, app, httptest.NewRequest("GET", "/users/1", nil), http.StatusOK)
	do(t, app, httptest.NewRequest("GET", "/users/2", nil), http.StatusOK)

	// The opt-out reaches the adapter: raw paths, one series each.
	assert.Equal(t, []string{"/users/1", "/users/2"}, labelValues(t, reg, "test_raw_path_http_requests_total", "path"))
}

// TestFiberMiddleware_LabelsSurviveBufferReuse is the regression test for
// label values aliasing fasthttp's pooled request buffer.
//
// c.Path() is an unsafe view valid only until the handler returns, but a
// Prometheus label is kept forever. Recording the view let request N+1
// rewrite request N's already-stored label in place: two vec entries reported
// the same label set, Gather() failed with a duplicate, and /metrics answered
// 500 for EVERY metric in the registry from then on.
//
// The raw-path opt-out is the shortest way to reach it -- nothing downstream
// allocates a fresh string -- but any PathTransformFunc that returns its
// argument unchanged does the same.
func TestFiberMiddleware_LabelsSurviveBufferReuse(t *testing.T) {
	for _, tc := range []struct {
		name string
		ns   string
		tune func(*metrics.HTTPMetricsConfig)
	}{
		{"raw paths", "test_alias_raw", func(c *metrics.HTTPMetricsConfig) {
			c.PathTransformFunc = nil
			c.DisablePathNormalization = true
		}},
		{"identity transform", "test_alias_identity", func(c *metrics.HTTPMetricsConfig) {
			c.PathTransformFunc = func(p string) string { return p }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := metrics.DefaultHTTPMetricsConfig()
			cfg.Namespace, cfg.Subsystem = tc.ns, "http"
			tc.tune(&cfg)
			app, reg := newApp(t, cfg)
			app.Get("/*", func(c fiber.Ctx) error { return c.SendString("ok") })

			want := []string{"/aaa", "/bbb", "/ccc", "/ddd"}
			for _, p := range want {
				do(t, app, httptest.NewRequest("GET", p, nil), http.StatusOK)
			}

			// Gathering at all is half the assertion: a corrupted label set
			// makes Gather return a duplicate-series error, which is what
			// takes the endpoint down.
			_, err := reg.Gatherer().Gather()
			require.NoError(t, err, "a reused request buffer rewrote a recorded label")

			assert.Equal(t, want, labelValues(t, reg, tc.ns+"_http_requests_total", "path"),
				"every path must still read back as the one that was requested")
			for _, p := range want {
				assert.Equal(t, 1.0, counterValue(t, reg, tc.ns+"_http_requests_total",
					map[string]string{"method": "GET", "path": p, "status": "200"}))
			}
		})
	}
}

// TestFiberMiddleware_SanitizeLabelValue drives the sanitizer through a
// PathTransformFunc, because fasthttp hands c.Path() the raw percent-encoded
// target and a newline cannot survive the request line -- the transform is
// the vector that can actually produce one.
//
// An unsanitized newline in a label value would break the exposition format
// for the whole registry, not just this series.
func TestFiberMiddleware_SanitizeLabelValue(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{
		Namespace:         "test_sanitize",
		Subsystem:         "http",
		PathTransformFunc: func(string) string { return "/api\nfoo\"bar\\baz" },
	}
	app, reg := newApp(t, cfg)
	app.Get("/*", func(c fiber.Ctx) error { return c.SendString("ok") })

	do(t, app, httptest.NewRequest("GET", "/api/foo", nil), http.StatusOK)

	got := labelValues(t, reg, "test_sanitize_http_requests_total", "path")
	require.Len(t, got, 1)
	assert.Equal(t, "/api foo_bar_baz", got[0])
	assert.NotContains(t, got[0], "\n")
	assert.NotContains(t, got[0], `"`)
}

// TestFiberMiddleware_LongPathIsTruncated pins the other half of sanitizing:
// a long path is cut to DefaultLabelValueMaxLength, so a pathological URL
// cannot blow the label up.
func TestFiberMiddleware_LongPathIsTruncated(t *testing.T) {
	long := "/" + strings.Repeat("a", metrics.DefaultLabelValueMaxLength*2)
	cfg := metrics.HTTPMetricsConfig{
		Namespace:         "test_longpath",
		Subsystem:         "http",
		PathTransformFunc: func(string) string { return long },
	}
	app, reg := newApp(t, cfg)
	app.Get("/*", func(c fiber.Ctx) error { return c.SendString("ok") })

	do(t, app, httptest.NewRequest("GET", "/whatever", nil), http.StatusOK)

	got := labelValues(t, reg, "test_longpath_http_requests_total", "path")
	require.Len(t, got, 1)
	assert.Len(t, got[0], metrics.DefaultLabelValueMaxLength)
	assert.Equal(t, long[:metrics.DefaultLabelValueMaxLength], got[0])
}

func TestFiberMiddleware_MethodLabel(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{Namespace: "test_method", Subsystem: "http"}
	app, reg := newApp(t, cfg)
	app.All("/api/test", func(c fiber.Ctx) error { return c.SendString("ok") })

	for _, m := range []string{"GET", "POST", "PUT", "DELETE"} {
		do(t, app, httptest.NewRequest(m, "/api/test", nil), http.StatusOK)
	}

	assert.Equal(t, []string{"DELETE", "GET", "POST", "PUT"},
		labelValues(t, reg, "test_method_http_requests_total", "method"))
}

// TestFiberMiddleware_PropagatesHandlerError checks the middleware stays
// transparent: it records the failure AND hands the error onward, rather than
// swallowing it the way Fiber's logger middleware does.
func TestFiberMiddleware_PropagatesHandlerError(t *testing.T) {
	cfg := metrics.HTTPMetricsConfig{Namespace: "test_propagate", Subsystem: "http"}
	cfg.Registry = metrics.NewRegistryWithSubsystem(cfg.Namespace, cfg.Subsystem)
	m := metrics.NewHTTPMetrics(cfg)

	var seenByOuter error
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		err := c.Next()
		seenByOuter = err
		return err
	})
	app.Use(fiberadapter.Middleware(m, cfg))

	want := fiber.NewError(fiber.StatusTeapot, "nope")
	app.Get("/api/error", func(c fiber.Ctx) error { return want })

	do(t, app, httptest.NewRequest("GET", "/api/error", nil), fiber.StatusTeapot)

	assert.Equal(t, want, seenByOuter, "the error must reach middleware wrapping this one")
	assert.Equal(t, 1.0, counterValue(t, cfg.Registry, "test_propagate_http_requests_total",
		map[string]string{"method": "GET", "path": "/api/error", "status": "418"}))
}

// --- /metrics handlers ----------------------------------------------------

func TestFiberHandler(t *testing.T) {
	handler := fiberadapter.Handler()
	require.NotNil(t, handler)

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "fiber_default_registry_probe_total",
		Help: "A probe registered on the default registry",
	})
	prometheus.MustRegister(counter)
	t.Cleanup(func() { prometheus.Unregister(counter) })
	counter.Add(7)

	app := fiber.New()
	app.Get("/metrics", handler)

	resp := do(t, app, httptest.NewRequest("GET", "/metrics", http.NoBody), http.StatusOK)
	// It serves the DEFAULT registry, and serves real exposition text.
	assert.Contains(t, body(t, resp), "fiber_default_registry_probe_total 7")
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

	resp := do(t, app, httptest.NewRequest("GET", "/metrics", http.NoBody), http.StatusOK)
	out := body(t, resp)
	assert.Contains(t, out, "test_fiber_handler_for_requests_total 1")
	// It serves THAT registry, not the default one: no process collectors.
	assert.NotContains(t, out, "go_goroutines")
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

	resp := do(t, app, httptest.NewRequest("GET", "/metrics", http.NoBody), http.StatusOK)
	out := body(t, resp)
	assert.Contains(t, out, "fiber_gatherer_test_counter 10")
	assert.NotContains(t, out, "go_goroutines")
}

func TestNewFiberHandler(t *testing.T) {
	opts := metrics.DefaultHandlerOpts()
	handler := fiberadapter.NewHandler(opts)
	require.NotNil(t, handler)

	app := fiber.New()
	app.Get("/metrics", handler)

	resp := do(t, app, httptest.NewRequest("GET", "/metrics", http.NoBody), http.StatusOK)
	assert.Contains(t, body(t, resp), "# HELP")
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

	resp := do(t, app, httptest.NewRequest("GET", "/metrics", http.NoBody), http.StatusOK)
	assert.Contains(t, body(t, resp), "test_fiber_new_handler_custom_counter 42")
}

// TestHandlersRouteThroughTheirOwnRegistry keeps the four constructors from
// collapsing into each other: each must serve the source it was given.
func TestHandlersRouteThroughTheirOwnRegistry(t *testing.T) {
	a := metrics.NewRegistry("reg_a")
	ca := prometheus.NewCounter(prometheus.CounterOpts{Namespace: "reg_a", Name: "hits_total", Help: "h"})
	a.MustRegister(ca)
	ca.Add(1)

	b := metrics.NewRegistry("reg_b")
	cb := prometheus.NewCounter(prometheus.CounterOpts{Namespace: "reg_b", Name: "hits_total", Help: "h"})
	b.MustRegister(cb)
	cb.Add(2)

	app := fiber.New()
	app.Get("/a", fiberadapter.HandlerFor(a))
	app.Get("/b", fiberadapter.NewHandler(metrics.HandlerOpts{Registry: b}))

	outA := body(t, do(t, app, httptest.NewRequest("GET", "/a", http.NoBody), http.StatusOK))
	outB := body(t, do(t, app, httptest.NewRequest("GET", "/b", http.NoBody), http.StatusOK))

	assert.Contains(t, outA, "reg_a_hits_total 1")
	assert.NotContains(t, outA, "reg_b_hits_total")
	assert.Contains(t, outB, "reg_b_hits_total 2")
	assert.NotContains(t, outB, "reg_a_hits_total")
}
