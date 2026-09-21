# metrics-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/metrics-kit/v2.svg)](https://pkg.go.dev/github.com/soulteary/metrics-kit/v2)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/metrics-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/metrics-kit)

[中文文档](README_CN.md)

A unified Prometheus metrics toolkit for Go services. This package provides metric builders, registry management, HTTP handlers, and middleware for consistent metrics collection across services.


> **Breaking in v2.3.0 — Fiber support moved to a subpackage.**
> The Fiber handlers and middleware are now
> `github.com/soulteary/metrics-kit/v2/fiberadapter`, so importing the root
> package no longer links Fiber (and fasthttp) into binaries that never use
> it. In a net/http service that means **26 fewer linked packages, 10 fewer
> modules and a 7% smaller binary** (the rest is Prometheus, which stays).
>
> | Before | After |
> |---|---|
> | `metrics.FiberHandler()` | `fiberadapter.Handler()` |
> | `metrics.FiberHandlerFor(reg)` | `fiberadapter.HandlerFor(reg)` |
> | `metrics.FiberHandlerForGatherer(g)` | `fiberadapter.HandlerForGatherer(g)` |
> | `metrics.NewFiberHandler(opts)` | `fiberadapter.NewHandler(opts)` |
> | `metrics.NewFiberMiddleware(ns)` | `mw, reg := fiberadapter.NewMiddleware(ns)` |
> | `metrics.NewFiberMiddlewareWithConfig(cfg)` | `mw, reg := fiberadapter.NewMiddlewareWithConfig(cfg)` |
> | `m.FiberMiddleware(cfg)` | `fiberadapter.Middleware(m, cfg)` |
>
> Nothing on the net/http side changed.

## Features

- **Registry Management**: Custom Prometheus registry with namespace/subsystem support
- **Fluent Builders**: Counter, Gauge, Histogram, and Summary builders with method chaining
- **HTTP Handlers**: Standard and Fiber-compatible `/metrics` endpoint handlers (optional timeout via `HandlerOpts`)
- **HTTP Middleware**: Request metrics collection for Fiber framework, with default path normalization to limit label cardinality
- **Label Safety**: `SanitizeLabelValue` for safe label values from untrusted input; `DefaultPathNormalize` for path-based labels
- **Common Metrics**: Pre-built metric patterns for cache, rate limiting, Redis, auth, OTP, etc.
- **Bucket Presets**: Predefined histogram buckets for HTTP, Redis, external APIs, and bytes

## Installation

```bash
go get github.com/soulteary/metrics-kit/v2
```

## Usage

### Basic Registry and Counter

```go
import (
    metrics "github.com/soulteary/metrics-kit/v2"
)

// Create a registry with namespace
registry := metrics.NewRegistry("myservice")

// Build a counter with labels
requestsTotal := registry.Counter("requests_total").
    Help("Total number of requests").
    Labels("method", "status").
    BuildVec()

// Use the counter
requestsTotal.WithLabelValues("GET", "200").Inc()
requestsTotal.WithLabelValues("POST", "201").Add(5)
```

### Histogram with Custom Buckets

```go
// Build a histogram with custom buckets
latency := registry.Histogram("request_duration_seconds").
    Help("Request duration in seconds").
    Labels("endpoint").
    Buckets(metrics.HTTPDurationBuckets()).
    BuildVec()

// Record observations
latency.WithLabelValues("/api/users").Observe(0.125)
```

### Gauge

```go
// Build a gauge for tracking active connections
activeConns := registry.Gauge("active_connections").
    Help("Number of active connections").
    Build()

activeConns.Set(42)
activeConns.Inc()
activeConns.Dec()
```

### HTTP Handler

```go
import (
    "net/http"
    metrics "github.com/soulteary/metrics-kit/v2"
    "github.com/soulteary/metrics-kit/v2/fiberadapter"
)

// Standard library (uses default Prometheus registry only)
http.Handle("/metrics", metrics.Handler())

// With custom registry: use HandlerFor or NewHandler so your app's metrics are exposed
http.Handle("/metrics", metrics.HandlerFor(registry))

// For Fiber
app.Get("/metrics", fiberadapter.Handler())
app.Get("/metrics", fiberadapter.HandlerFor(registry)) // when using custom registry

// With options (e.g. custom registry + scrape timeout in seconds)
handler := metrics.NewHandler(metrics.HandlerOpts{
    Registry:          registry,
    EnableOpenMetrics: true,
    Timeout:           10,
})
http.Handle("/metrics", handler)
```

### HTTP Middleware (Fiber)

```go
import (
    "github.com/gofiber/fiber/v3"
    metrics "github.com/soulteary/metrics-kit/v2"
    "github.com/soulteary/metrics-kit/v2/fiberadapter"
)

app := fiber.New()

// Simple middleware. The second return is the registry the metrics went
// into -- without it nothing can scrape them.
mw, reg := fiberadapter.NewMiddleware("myservice")
app.Use(mw)
app.Get("/metrics", fiberadapter.HandlerFor(reg))

// With custom configuration (default config already uses DefaultPathNormalize)
cfg := metrics.HTTPMetricsConfig{
    Namespace:               "myservice",
    Subsystem:               "api",
    SkipPaths:               []string{"/health", "/metrics"},
    IncludeRequestSize:      true,
    IncludeResponseSize:     true,
    IncludeRequestsInFlight: true,
    PathTransformFunc:       metrics.DefaultPathNormalize, // /users/123 -> /users/:id
}
mw, reg = fiberadapter.NewMiddlewareWithConfig(cfg)
app.Use(mw)
```

Set `cfg.Registry` to put the HTTP metrics into a registry you already have --
see [Herald](#herald-otp-service) for one `/metrics` serving both.

### Common Metrics Patterns

```go
registry := metrics.NewRegistry("myservice")
cm := metrics.NewCommonMetrics(registry)

// Cache metrics
cache := cm.NewCacheMetrics("users")
cache.RecordHit()
cache.RecordMiss()
cache.SetSize(100)

// Rate limit metrics
rateLimit := cm.NewRateLimitMetrics()
rateLimit.RecordHit("user")
rateLimit.RecordHit("ip")

// Redis metrics
redis := cm.NewRedisMetrics()
redis.RecordSuccess("get", 5*time.Millisecond)
redis.RecordFailure("set", 100*time.Millisecond)
redis.SetActiveConnections(10)

// External service metrics (e.g., Warden, Herald)
warden := cm.NewExternalServiceMetrics("warden")
warden.RecordSuccess("check_user", 50*time.Millisecond)
warden.RecordFailure("get_info", 200*time.Millisecond)

// Background task metrics
tasks := cm.NewBackgroundTaskMetrics()
tasks.IncRunning()
tasks.RecordSuccess("sync", 5*time.Second)
tasks.DecRunning()

// Auth metrics
auth := cm.NewAuthMetrics()
auth.RecordSuccess("password")
auth.RecordSessionCreated()

// OTP metrics
otp := cm.NewOTPMetrics()
otp.RecordChallengeCreated("email", "login", "success")
otp.RecordSend("email", "smtp", "success", 100*time.Millisecond)
otp.RecordVerification("success", "")
```

### Path Normalization

A path used as a metric label is a cardinality risk: one time series per distinct
value. `DefaultPathNormalize` collapses the **unambiguous** id shapes:

| Shape | Example | Becomes |
|-------|---------|---------|
| All-digit segment | `/users/123` | `/users/:id` |
| UUID | `/orders/3f1a…` | `/orders/:id` |
| Hex, 16 or more chars | `/t/9f86d081884c7d65` | `/t/:id` |
| ULID (either case) | `/events/01ARZ3ND…` | `/events/:id` |

It is applied by the middleware whether or not you set `PathTransformFunc`, so a
config built as a struct literal still gets it.

```go
cfg := metrics.DefaultHTTPMetricsConfig()
cfg.PathTransformFunc = metrics.PathNormalizeWithTokens // wider, see below
cfg.SkipPaths = []string{"/healthz", "/metrics"}
cfg.DisablePathNormalization = true                     // raw paths; see the warning
```

**Nanoid- and base64url-shaped segments are deliberately left alone.** Nothing
distinguishes a 21-character random token from a 21-character route name such as
`/oauth2CallbackHandler`. `PathNormalizeWithTokens` guesses at them by shape
alone — **any** 21–22 character URL-safe segment, `/forgot-password-reset`
included — and a wrong guess silently merges a real endpoint into `/:id`. Check
your route table for a segment of that length before enabling it, and prefer a
custom `PathTransformFunc` when your ids have a known exact form.

`DisablePathNormalization` logs the raw path. Use it only where the route set is
closed and known; otherwise requesting random URLs mints a new time series per
request, which is a cheap way to exhaust a Prometheus server.

### Bucket Presets

```go
// HTTP request duration buckets
// 1ms, 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s
metrics.HTTPDurationBuckets()

// Redis operation duration buckets
// 0.5ms, 1ms, 2.5ms, 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s
metrics.RedisDurationBuckets()

// External API call duration buckets
// 10ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s, 30s
metrics.ExternalAPIDurationBuckets()

// Request/response size buckets
// 100B, 1KB, 10KB, 100KB, 1MB, 10MB
metrics.BytesBuckets()

// Default Prometheus buckets
metrics.DefaultBuckets()
```

## Project Structure

```
metrics-kit/
├── registry.go       # Registry management with namespace/subsystem
├── builders.go       # Fluent metric builders (Counter, Histogram, Gauge, Summary)
├── labels.go         # Label safety: SanitizeLabelValue, DefaultPathNormalize
├── http.go           # HTTP handlers for /metrics endpoint
├── middleware.go     # net/http metric types (Fiber middleware lives in fiberadapter/)
├── common.go         # Common metric patterns (cache, redis, auth, OTP, etc.)
└── *_test.go         # Comprehensive tests
```

## Integration Example

### Herald (OTP Service)

```go
package main

import (
    "github.com/gofiber/fiber/v3"
    metrics "github.com/soulteary/metrics-kit/v2"
    "github.com/soulteary/metrics-kit/v2/fiberadapter"
)

func main() {
    registry := metrics.NewRegistry("herald")
    cm := metrics.NewCommonMetrics(registry)
    
    // Create OTP metrics
    otp := cm.NewOTPMetrics()
    redis := cm.NewRedisMetrics()
    rateLimit := cm.NewRateLimitMetrics()
    
    app := fiber.New()
    
    // Add metrics middleware, into the SAME registry the endpoint serves.
    // Without cfg.Registry it builds its own, and these HTTP metrics would
    // be missing from /metrics.
    cfg := metrics.DefaultHTTPMetricsConfig()
    cfg.Registry = registry.WithSubsystem("http")
    mw, _ := fiberadapter.NewMiddlewareWithConfig(cfg)
    app.Use(mw)
    
    // Add metrics endpoint
    app.Get("/metrics", fiberadapter.HandlerFor(registry))
    
    // Use metrics in handlers
    app.Post("/v1/otp/challenges", func(c fiber.Ctx) error {
        // ... create challenge logic ...
        otp.RecordChallengeCreated("email", "login", "success")
        return c.JSON(response)
    })
    
    app.Listen(":8080")
}
```

### Stargate (Auth Gateway)

```go
package main

import (
    metrics "github.com/soulteary/metrics-kit/v2"
)

func main() {
    registry := metrics.NewRegistry("stargate")
    cm := metrics.NewCommonMetrics(registry)
    
    auth := cm.NewAuthMetrics()
    heraldCalls := cm.NewExternalServiceMetrics("herald")
    wardenCalls := cm.NewExternalServiceMetrics("warden")
    
    // Use in auth flow
    auth.RecordSuccess("warden_otp")
    auth.RecordSessionCreated()
    
    // Track external service calls
    heraldCalls.RecordSuccess("create_challenge", 150*time.Millisecond)
    wardenCalls.RecordSuccess("check_user", 50*time.Millisecond)
}
```

## API Reference

### Registries

```go
registry := metrics.NewRegistry("myapp")
registry = metrics.NewRegistryWithSubsystem("myapp", "http")
registry = metrics.DefaultRegistry()            // process-wide default

registry.Namespace()
registry.Gatherer()                             // prometheus.Gatherer
registry.PrometheusRegistry()                   // the underlying *prometheus.Registry

registry.Register("name", collector)            // tracked by name
registry.Unregister("name")
registry.MustRegister(collectors...)
registry.UnregisterCollector(collector)         // for builder-created collectors
```

### Builders

```go
registry.Counter("requests_total").Help("…").Labels("method").BuildVec()
registry.Gauge("queue_depth").Help("…").Build()
registry.Histogram("duration_seconds").Buckets(metrics.HTTPDurationBuckets()).BuildVec()
registry.Summary("payload_bytes").Build()
```

Declaring the same metric name twice **reuses the existing collector** instead of
panicking, so two components reaching for the same counter both get it.

### Exposition Handlers

```go
http.Handle("/metrics", metrics.Handler())                      // DefaultRegistry
http.Handle("/metrics", metrics.HandlerFor(registry))
http.Handle("/metrics", metrics.HandlerForGatherer(gatherer))
http.Handle("/metrics", metrics.NewHandler(metrics.DefaultHandlerOpts()))

metrics.RegisterHTTPHandler(mux, "/metrics")
metrics.RegisterHTTPHandlerFor(mux, "/metrics", registry)

app.Get("/metrics", fiberadapter.Handler())
app.Get("/metrics", fiberadapter.HandlerFor(registry))
app.Get("/metrics", fiberadapter.HandlerForGatherer(gatherer))
app.Get("/metrics", fiberadapter.NewHandler(metrics.DefaultHandlerOpts()))
```

None of these authenticate. See [Security and Deployment](#security-and-deployment).

### HTTP Metrics

```go
cfg := metrics.DefaultHTTPMetricsConfig()
m := metrics.NewHTTPMetrics(cfg)                 // the collectors, to drive yourself
                                                 // m.Registry is where they landed

mw, reg := fiberadapter.NewMiddleware("myapp")   // or NewMiddlewareWithConfig(cfg)
app.Use(mw)
app.Get("/metrics", fiberadapter.HandlerFor(reg))
```

| Option | Default | Notes |
|--------|---------|-------|
| `Namespace` / `Subsystem` | from the registry | metric name prefix |
| `Registry` | a new isolated registry | where the collectors land; `m.Registry` and the middleware constructors report which |
| `PathTransformFunc` | `DefaultPathNormalize` | applied by the middleware regardless |
| `DisablePathNormalization` | `false` | log raw paths — read the warning first |
| `SkipPaths` | none | paths to record nothing for |
| `DurationBuckets` | `HTTPDurationBuckets()` | |
| `SizeBuckets` | `DefaultBuckets()` | |
| `IncludeRequestSize` | | from `Content-Length`, not the body |
| `IncludeResponseSize` | | |
| `IncludeRequestsInFlight` | | |

### Common Metrics

`metrics.NewCommonMetrics(registry)` returns a `*CommonMetrics` bundling the
ready-made groups: `AuthMetrics`, `OTPMetrics`, `CacheMetrics`, `RedisMetrics`,
`RateLimitMetrics`, `ExternalServiceMetrics` and `BackgroundTaskMetrics`.

## Security and Deployment

- **Protect the `/metrics` endpoint.** The handlers returned by `Handler()`, `HandlerFor()`, etc. do not perform authentication. Do not expose `/metrics` to the public internet. Prefer one or more of: a dedicated admin port, network policies, reverse-proxy authentication, or IP allowlisting so only your monitoring stack can scrape.
- **Path label cardinality.** The default HTTP metrics config uses `DefaultPathNormalize` so paths like `/users/123` become `/users/:id`. In production, always use path normalization (or skip certain paths) to avoid unbounded time series and potential DoS from high cardinality. `DefaultPathNormalize` replaces only **unambiguous** id shapes -- all-digit segments, UUIDs, long hex strings and ULIDs. Nanoid- and base64url-shaped segments are left alone, because nothing distinguishes a 21-character random token from a 21-character route name such as `/oauth2CallbackHandler`; `PathNormalizeWithTokens` guesses at them by shape alone -- **any** 21-22 character URL-safe segment, `/forgot-password-reset` included -- and a wrong guess silently merges a real endpoint into `/:id`. Check your route table for a segment of that length before enabling it, and use a custom `PathTransformFunc` when your ids have a known exact form.
- **Label values from untrusted input.** For metrics that use labels from user or external input (e.g. in `CommonMetrics` such as scope, operation, provider), either pass only controlled enum-like values or sanitize with `SanitizeLabelValue(s, metrics.DefaultLabelValueMaxLength)` to avoid breaking the exposition format (e.g. newlines in label values). Example: `scope := metrics.SanitizeLabelValue(userInput, metrics.DefaultLabelValueMaxLength)` before calling `rateLimit.RecordHit(scope)`.

## Registry and Unregister

- `Unregister(name)` only affects collectors registered with `Register(name, collector)`. Metrics created via the builders (`Build()` / `BuildVec()`) are not tracked by name, so `Unregister` cannot reach them.
- To remove a builder-created collector, keep its reference and call **`registry.UnregisterCollector(collector)`**. As well as unregistering it, this releases the shape record that describes it -- the record holds the collector strongly, so dynamically creating and removing uniquely named vectors otherwise retains every one of them, label children included, for the registry's lifetime.
- Calling `registry.PrometheusRegistry().Unregister(collector)` directly still works, but this package cannot observe that call, so the shape record is left behind. Prefer `UnregisterCollector`.
- Note that re-registering the same metric name with **different label names** panics inside Prometheus whatever you do: `client_golang` keeps its `dimHashesByName` for the life of the process on purpose.

## Upgrade Notes (v2.2.0)

One field and two functions were added; nothing was removed. The first item can
change a crash into normal operation, which is the point.

- **A duplicate metric name no longer kills the process.** Every builder
  registered through `MustRegister`, so declaring the same name twice — two
  components reaching for the same counter, or a package initialised twice in a
  test binary — panicked at startup and took the service down. Builders now reuse
  the collector that `prometheus.AlreadyRegisteredError` hands back. Note that
  re-registering a name with **different label names** still panics inside
  Prometheus whatever you do: `client_golang` keeps its `dimHashesByName` for the
  life of the process on purpose.
- **Raw paths no longer reach label values by accident.** The middleware applied
  `PathTransformFunc` only when non-nil. `DefaultMiddlewareConfig` sets it, but a
  config built as a struct literal — `HTTPMetricsConfig{SkipPaths: …}` — left it
  nil, and the raw URL path became the label: requesting random URLs minted a new
  time series per request. The default is now applied in the middleware rather
  than assumed from the constructor. **Expect fewer, coarser path labels** if you
  were building the config as a literal.
- **`DefaultPathNormalize` recognises more id shapes.** It matched digits, UUIDs
  and hex of 24+ characters, so 16-character hex ids, ULIDs and nanoids leaked
  into labels anyway. 16+ hex and ULIDs in **either case** are matched now — the
  encoding is case-insensitive and libraries emit both.
- **Request size comes from `Content-Length`.** The middleware called `c.Body()`,
  materialising the whole body for every request — including ones the handler
  streams or never reads — and ran before any body-size limit further down the
  chain.
- **`SanitizeLabelValue` escapes the double quote.** It escaped backslashes and
  newlines but not `"`, and a label is written `name="value"` — the quote breaks
  out of the field exactly as the others do.
- **`UnregisterCollector` is new**, and is how to remove a builder-created
  collector. `Unregister(name)` only reaches collectors registered with
  `Register(name, collector)`. Going through `PrometheusRegistry().Unregister`
  still works but leaves behind a shape record that holds the collector strongly —
  so dynamically creating and removing uniquely named vectors retained every one
  of them, label children included, for the registry's lifetime.
- **`PathNormalizeWithTokens` and `DisablePathNormalization` are new.** See
  [Path Normalization](#path-normalization) — the first is an opt-in guess that
  can merge real endpoints, the second turns normalization off entirely.
- **Named registrations are per-wrapper.** Two `DefaultRegistry()` wrappers over
  one registry shared the named-registration map, so the second's
  `Register("x", c2)` overwrote the first's entry and the first's
  `Unregister("x")` removed the *second's* collector.
- **Requirements said Go 1.26**; `go.mod` requires `1.27.0`.

## Requirements

- **Go 1.27+** (`go.mod` declares `go 1.27.0`)
- github.com/prometheus/client_golang v1.22.0+
- github.com/gofiber/fiber/v3 v3.4.0+ (for Fiber middleware)

This v2 module line targets Fiber v3. Applications that still use Fiber v2 should remain on `github.com/soulteary/metrics-kit` v1.

## Test Coverage

Run tests:

```bash
go test ./... -v

# With coverage
go test ./... -coverprofile=coverage.out -covermode=atomic
go tool cover -html=coverage.out -o coverage.html
go tool cover -func=coverage.out
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

See [LICENSE](LICENSE) file for details.
