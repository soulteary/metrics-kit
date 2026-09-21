package fiberadapter_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gofiber/fiber/v3"

	metrics "github.com/soulteary/metrics-kit/v3"
	"github.com/soulteary/metrics-kit/v3/fiberadapter"
)

// Instrument a Fiber app and serve what it collects.
//
// The constructor returns the registry alongside the handler: with no Registry
// in the config it builds one, and that return is the only handle to it.
// Handler() serves the DEFAULT registry, which is not that one.
func Example() {
	cfg := metrics.DefaultHTTPMetricsConfig()
	cfg.Namespace = "shop"
	cfg.SkipPaths = []string{"/metrics"} // don't instrument the scrape itself

	mw, reg := fiberadapter.NewMiddlewareWithConfig(cfg)

	app := fiber.New()
	app.Use(mw)
	app.Get("/users/:id", func(c fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/metrics", fiberadapter.HandlerFor(reg))

	// Two different ids collapse to one series: the default config
	// normalizes the path before it becomes a label.
	for _, p := range []string{"/users/1", "/users/2"} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, p, nil))
		if err != nil {
			panic(err)
		}
		resp.Body.Close()
	}

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "shop_http_requests_total{") {
			fmt.Println(line)
		}
	}
	// Output:
	// shop_http_requests_total{method="GET",path="/users/:id",status="200"} 2
}

// Middleware takes metrics you built yourself, so application metrics and HTTP
// metrics can share one registry and one /metrics endpoint.
func ExampleMiddleware() {
	reg := metrics.NewRegistry("herald")

	signups := reg.Counter("signups_total").Help("Signups completed").Build()
	signups.Inc()

	cfg := metrics.DefaultHTTPMetricsConfig()
	cfg.Registry = reg.WithSubsystem("http") // same underlying registry
	cfg.SkipPaths = []string{"/metrics"}

	app := fiber.New()
	app.Use(fiberadapter.Middleware(metrics.NewHTTPMetrics(cfg), cfg))
	app.Get("/signup", func(c fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/metrics", fiberadapter.HandlerFor(reg))

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/signup", nil))
	if err != nil {
		panic(err)
	}
	resp.Body.Close()

	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "herald_signups_total ") ||
			strings.HasPrefix(line, "herald_http_requests_total{") {
			fmt.Println(line)
		}
	}
	// Output:
	// herald_http_requests_total{method="GET",path="/signup",status="200"} 1
	// herald_signups_total 1
}
