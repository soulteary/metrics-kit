package metrics_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	metrics "github.com/soulteary/metrics-kit/v3"
)

// Build a registry, declare a metric on it, and serve it on /metrics.
func Example() {
	reg := metrics.NewRegistry("myservice")

	requests := reg.Counter("requests_total").
		Help("Requests handled, by route").
		Labels("route").
		BuildVec()

	requests.WithLabelValues("/healthz").Add(3)

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.HandlerFor(reg))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, "myservice_requests_total{") {
			fmt.Println(line)
		}
	}
	// Output:
	// myservice_requests_total{route="/healthz"} 3
}

// The builders are fluent and end in Build (one collector) or BuildVec (a
// labelled one). The namespace comes from the registry, so metric names are
// prefixed without repeating it at every call site.
func ExampleRegistry_Counter() {
	reg := metrics.NewRegistry("shop")

	orders := reg.Counter("orders_total").
		Help("Orders placed, by channel").
		Labels("channel").
		BuildVec()

	orders.WithLabelValues("web").Inc()
	orders.WithLabelValues("web").Inc()
	orders.WithLabelValues("app").Inc()

	mfs, err := reg.Gatherer().Gather()
	if err != nil {
		panic(err)
	}
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			fmt.Printf("%s{%s=%q} %v\n",
				mf.GetName(),
				m.GetLabel()[0].GetName(), m.GetLabel()[0].GetValue(),
				m.GetCounter().GetValue())
		}
	}
	// Output:
	// shop_orders_total{channel="app"} 1
	// shop_orders_total{channel="web"} 2
}

// RegisterHTTPHandler mounts the default registry on an existing mux.
func ExampleRegisterHTTPHandler() {
	mux := http.NewServeMux()
	metrics.RegisterHTTPHandler(mux, "/metrics")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	fmt.Println(rec.Code)
	// Output: 200
}

// This package has no net/http middleware: you record each request yourself,
// from wherever you already have the method, path, status and duration.
//
// Normalize the path first. A raw URL path as a label value lets anyone
// requesting random URLs mint a new time series per request.
func ExampleHTTPMetrics_RecordRequest() {
	cfg := metrics.DefaultHTTPMetricsConfig()
	cfg.Namespace = "api"

	// With no Registry in the config, NewHTTPMetrics builds one -- and
	// m.Registry is the only handle to it. Serve it with HandlerFor(m.Registry).
	m := metrics.NewHTTPMetrics(cfg)

	path := cfg.TransformPath("/users/42") // -> /users/:id
	m.RecordRequest(http.MethodGet, path, "200", 120*time.Millisecond)
	m.RecordRequest(http.MethodGet, path, "200", 80*time.Millisecond)
	m.RecordRequest(http.MethodGet, path, "500", 5*time.Millisecond)

	mfs, err := m.Registry.Gatherer().Gather()
	if err != nil {
		panic(err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "api_http_requests_total" {
			continue
		}
		for _, s := range mf.GetMetric() {
			labels := map[string]string{}
			for _, l := range s.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			fmt.Printf("%s %s %v\n", labels["path"], labels["status"], s.GetCounter().GetValue())
		}
	}
	// Output:
	// /users/:id 200 2
	// /users/:id 500 1
}

// TransformPath applies the config's normalization. It is what keeps the path
// label bounded, and is exported so an out-of-tree adapter can apply the same
// rule instead of restating it.
func ExampleHTTPMetricsConfig_TransformPath() {
	cfg := metrics.DefaultHTTPMetricsConfig()
	fmt.Println(cfg.TransformPath("/users/42"))

	// Raw paths are an explicit opt-out, safe only where every path is
	// already bounded -- a router reporting its route pattern, say.
	cfg.DisablePathNormalization = true
	fmt.Println(cfg.TransformPath("/users/42"))
	// Output:
	// /users/:id
	// /users/42
}

// DefaultPathNormalize collapses only unambiguous id shapes: all-digit
// segments, UUIDs, long hex strings and ULIDs. Route names are left alone.
func ExampleDefaultPathNormalize() {
	for _, p := range []string{
		"/users/123/orders/456",
		"/users/018f3a2b-4c5d-7e8f-9a0b-1c2d3e4f5a6b",
		"/files/a1b2c3d4e5f6a7b8",
		"/api/health",
	} {
		fmt.Println(metrics.DefaultPathNormalize(p))
	}
	// Output:
	// /users/:id/orders/:id
	// /users/:id
	// /files/:id
	// /api/health
}

// Label values taken from user or external input must be sanitized, or a
// newline or quote breaks the exposition format for the whole registry.
func ExampleSanitizeLabelValue() {
	fmt.Printf("%q\n", metrics.SanitizeLabelValue("/api/foo\nbar\"baz", metrics.DefaultLabelValueMaxLength))
	// Output: "/api/foo bar_baz"
}
