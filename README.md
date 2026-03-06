# metrics-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/metrics-kit.svg)](https://pkg.go.dev/github.com/soulteary/metrics-kit)
[![Go Report Card](https://goreportcard.com/badge/github.com/soulteary/metrics-kit)](https://goreportcard.com/report/github.com/soulteary/metrics-kit)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/metrics-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/metrics-kit)

[中文文档](README_CN.md)

A unified Prometheus metrics toolkit for Go services. This package provides metric builders, registry management, HTTP handlers, and middleware for consistent metrics collection across services.

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
go get github.com/soulteary/metrics-kit
```

## Usage

### Basic Registry and Counter

```go
import (
    metrics "github.com/soulteary/metrics-kit"
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
    metrics "github.com/soulteary/metrics-kit"
)

// Standard library (uses default Prometheus registry only)
http.Handle("/metrics", metrics.Handler())

// With custom registry: use HandlerFor or NewHandler so your app's metrics are exposed
http.Handle("/metrics", metrics.HandlerFor(registry))

// For Fiber
app.Get("/metrics", metrics.FiberHandler())
app.Get("/metrics", metrics.FiberHandlerFor(registry)) // when using custom registry

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
    "github.com/gofiber/fiber/v2"
    metrics "github.com/soulteary/metrics-kit"
)

app := fiber.New()

// Simple middleware
app.Use(metrics.NewFiberMiddleware("myservice"))

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
app.Use(metrics.NewFiberMiddlewareWithConfig(cfg))
```

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
├── middleware.go     # Fiber HTTP middleware
├── common.go         # Common metric patterns (cache, redis, auth, OTP, etc.)
└── *_test.go         # Comprehensive tests
```

## Integration Example

### Herald (OTP Service)

```go
package main

import (
    "github.com/gofiber/fiber/v2"
    metrics "github.com/soulteary/metrics-kit"
)

func main() {
    registry := metrics.NewRegistry("herald")
    cm := metrics.NewCommonMetrics(registry)
    
    // Create OTP metrics
    otp := cm.NewOTPMetrics()
    redis := cm.NewRedisMetrics()
    rateLimit := cm.NewRateLimitMetrics()
    
    app := fiber.New()
    
    // Add metrics middleware
    app.Use(metrics.NewFiberMiddleware("herald"))
    
    // Add metrics endpoint
    app.Get("/metrics", metrics.FiberHandlerFor(registry))
    
    // Use metrics in handlers
    app.Post("/v1/otp/challenges", func(c *fiber.Ctx) error {
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
    metrics "github.com/soulteary/metrics-kit"
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

## Security and Deployment

- **Protect the `/metrics` endpoint.** The handlers returned by `Handler()`, `HandlerFor()`, etc. do not perform authentication. Do not expose `/metrics` to the public internet. Prefer one or more of: a dedicated admin port, network policies, reverse-proxy authentication, or IP allowlisting so only your monitoring stack can scrape.
- **Path label cardinality.** The default HTTP metrics config uses `DefaultPathNormalize` so paths like `/users/123` become `/users/:id`. In production, always use path normalization (or skip certain paths) to avoid unbounded time series and potential DoS from high cardinality.
- **Label values from untrusted input.** For metrics that use labels from user or external input (e.g. in `CommonMetrics` such as scope, operation, provider), either pass only controlled enum-like values or sanitize with `SanitizeLabelValue(s, metrics.DefaultLabelValueMaxLength)` to avoid breaking the exposition format (e.g. newlines in label values). Example: `scope := metrics.SanitizeLabelValue(userInput, metrics.DefaultLabelValueMaxLength)` before calling `rateLimit.RecordHit(scope)`.

## Registry and Unregister

- `Unregister(name)` only affects collectors that were registered with `Register(name, collector)`. Metrics created via the builders (`Build()` / `BuildVec()`) are registered with `MustRegister` and are not tracked by name; to remove them, keep the collector reference and call the underlying `registry.PrometheusRegistry().Unregister(collector)`.

## Requirements

- Go 1.26 or later
- github.com/prometheus/client_golang v1.22.0+
- github.com/gofiber/fiber/v2 v2.52.6+ (for Fiber middleware)

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
