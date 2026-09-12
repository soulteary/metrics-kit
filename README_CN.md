# metrics-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/metrics-kit/v2.svg)](https://pkg.go.dev/github.com/soulteary/metrics-kit/v2)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/metrics-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/metrics-kit)

[English](README.md)

统一的 Go 服务 Prometheus 指标工具包。提供指标构建器、注册表管理、HTTP 处理器和中间件，实现跨服务的一致性指标收集。

## 特性

- **注册表管理**：支持命名空间/子系统的自定义 Prometheus 注册表
- **流式构建器**：Counter、Gauge、Histogram、Summary 构建器，支持链式调用
- **HTTP 处理器**：标准库和 Fiber 兼容的 `/metrics` 端点处理器（可通过 `HandlerOpts` 设置超时）
- **HTTP 中间件**：Fiber 请求指标收集中间件，默认路径归一化以控制标签基数
- **标签安全**：`SanitizeLabelValue` 处理不可信标签值；`DefaultPathNormalize` 用于路径类标签
- **通用指标**：预置的缓存、限流、Redis、认证、OTP 等常用指标模式
- **桶预设**：HTTP、Redis、外部 API、字节大小的预定义直方图桶

## 安装

```bash
go get github.com/soulteary/metrics-kit/v2
```

## 使用

### 基础注册表和计数器

```go
import (
    metrics "github.com/soulteary/metrics-kit/v2"
)

// 创建带命名空间的注册表
registry := metrics.NewRegistry("myservice")

// 构建带标签的计数器
requestsTotal := registry.Counter("requests_total").
    Help("请求总数").
    Labels("method", "status").
    BuildVec()

// 使用计数器
requestsTotal.WithLabelValues("GET", "200").Inc()
requestsTotal.WithLabelValues("POST", "201").Add(5)
```

### 自定义桶的直方图

```go
// 构建自定义桶的直方图
latency := registry.Histogram("request_duration_seconds").
    Help("请求延迟（秒）").
    Labels("endpoint").
    Buckets(metrics.HTTPDurationBuckets()).
    BuildVec()

// 记录观测值
latency.WithLabelValues("/api/users").Observe(0.125)
```

### 仪表盘

```go
// 构建用于跟踪活跃连接数的仪表盘
activeConns := registry.Gauge("active_connections").
    Help("活跃连接数").
    Build()

activeConns.Set(42)
activeConns.Inc()
activeConns.Dec()
```

### HTTP 处理器

```go
import (
    "net/http"
    metrics "github.com/soulteary/metrics-kit/v2"
)

// 标准库（仅使用默认 Prometheus 注册表）
http.Handle("/metrics", metrics.Handler())

// 使用自定义注册表时请用 HandlerFor 或 NewHandler，否则应用内指标不会暴露
http.Handle("/metrics", metrics.HandlerFor(registry))

// Fiber 框架
app.Get("/metrics", metrics.FiberHandler())
app.Get("/metrics", metrics.FiberHandlerFor(registry)) // 使用自定义注册表时

// 使用选项（如自定义注册表 + 抓取超时秒数）
handler := metrics.NewHandler(metrics.HandlerOpts{
    Registry:          registry,
    EnableOpenMetrics: true,
    Timeout:           10,
})
http.Handle("/metrics", handler)
```

### HTTP 中间件 (Fiber)

```go
import (
    "github.com/gofiber/fiber/v3"
    metrics "github.com/soulteary/metrics-kit/v2"
)

app := fiber.New()

// 简单中间件
app.Use(metrics.NewFiberMiddleware("myservice"))

// 自定义配置（默认配置已使用 DefaultPathNormalize）
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

### 通用指标模式

```go
registry := metrics.NewRegistry("myservice")
cm := metrics.NewCommonMetrics(registry)

// 缓存指标
cache := cm.NewCacheMetrics("users")
cache.RecordHit()
cache.RecordMiss()
cache.SetSize(100)

// 限流指标
rateLimit := cm.NewRateLimitMetrics()
rateLimit.RecordHit("user")
rateLimit.RecordHit("ip")

// Redis 指标
redis := cm.NewRedisMetrics()
redis.RecordSuccess("get", 5*time.Millisecond)
redis.RecordFailure("set", 100*time.Millisecond)
redis.SetActiveConnections(10)

// 外部服务指标（如 Warden、Herald）
warden := cm.NewExternalServiceMetrics("warden")
warden.RecordSuccess("check_user", 50*time.Millisecond)
warden.RecordFailure("get_info", 200*time.Millisecond)

// 后台任务指标
tasks := cm.NewBackgroundTaskMetrics()
tasks.IncRunning()
tasks.RecordSuccess("sync", 5*time.Second)
tasks.DecRunning()

// 认证指标
auth := cm.NewAuthMetrics()
auth.RecordSuccess("password")
auth.RecordSessionCreated()

// OTP 指标
otp := cm.NewOTPMetrics()
otp.RecordChallengeCreated("email", "login", "success")
otp.RecordSend("email", "smtp", "success", 100*time.Millisecond)
otp.RecordVerification("success", "")
```

### 路径归一化

把路径用作指标标签是一种基数风险：每个不同取值都会产生一条时间序列。
`DefaultPathNormalize` 会折叠那些**无歧义**的 ID 形态：

| 形态 | 示例 | 归一化为 |
|------|------|----------|
| 纯数字段 | `/users/123` | `/users/:id` |
| UUID | `/orders/3f1a…` | `/orders/:id` |
| 16 位及以上十六进制 | `/t/9f86d081884c7d65` | `/t/:id` |
| ULID（大小写均可） | `/events/01ARZ3ND…` | `/events/:id` |

无论你是否设置 `PathTransformFunc`，中间件都会应用它，因此用结构体字面量构造的配置
同样会得到归一化。

```go
cfg := metrics.DefaultHTTPMetricsConfig()
cfg.PathTransformFunc = metrics.PathNormalizeWithTokens // 范围更宽，见下文
cfg.SkipPaths = []string{"/healthz", "/metrics"}
cfg.DisablePathNormalization = true                     // 原始路径；先看下面的警告
```

**nanoid 和 base64url 形态的段是特意不处理的。** 没有任何特征能把一个 21 字符的随机
token 与一个 21 字符的路由名（比如 `/oauth2CallbackHandler`）区分开。
`PathNormalizeWithTokens` 纯按形状猜——**任何** 21–22 字符的 URL-safe 段都算，包括
`/forgot-password-reset`——而猜错会把一个真实端点静默地合并进 `/:id`。启用之前请先
检查你的路由表里有没有这个长度的段；当你的 ID 有确定的精确形式时，请优先用自定义的
`PathTransformFunc`。

`DisablePathNormalization` 会记录原始路径。只在路由集合封闭且已知的场景使用；否则
请求随机 URL 会每个请求产生一条新的时间序列，这是耗尽 Prometheus 服务器的廉价手段。

### 桶预设

```go
// HTTP 请求延迟桶
// 1ms, 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s
metrics.HTTPDurationBuckets()

// Redis 操作延迟桶
// 0.5ms, 1ms, 2.5ms, 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s
metrics.RedisDurationBuckets()

// 外部 API 调用延迟桶
// 10ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s, 30s
metrics.ExternalAPIDurationBuckets()

// 请求/响应大小桶
// 100B, 1KB, 10KB, 100KB, 1MB, 10MB
metrics.BytesBuckets()

// 默认 Prometheus 桶
metrics.DefaultBuckets()
```

## 项目结构

```
metrics-kit/
├── registry.go       # 带命名空间/子系统的注册表管理
├── builders.go       # 流式指标构建器（Counter、Histogram、Gauge、Summary）
├── labels.go         # 标签安全：SanitizeLabelValue、DefaultPathNormalize
├── http.go           # /metrics 端点的 HTTP 处理器
├── middleware.go     # Fiber HTTP 中间件
├── common.go         # 通用指标模式（缓存、Redis、认证、OTP 等）
└── *_test.go         # 完整测试
```

## 集成示例

### Herald（OTP 服务）

```go
package main

import (
    "github.com/gofiber/fiber/v3"
    metrics "github.com/soulteary/metrics-kit/v2"
)

func main() {
    registry := metrics.NewRegistry("herald")
    cm := metrics.NewCommonMetrics(registry)
    
    // 创建 OTP 指标
    otp := cm.NewOTPMetrics()
    redis := cm.NewRedisMetrics()
    rateLimit := cm.NewRateLimitMetrics()
    
    app := fiber.New()
    
    // 添加指标中间件
    app.Use(metrics.NewFiberMiddleware("herald"))
    
    // 添加指标端点
    app.Get("/metrics", metrics.FiberHandlerFor(registry))
    
    // 在处理器中使用指标
    app.Post("/v1/otp/challenges", func(c fiber.Ctx) error {
        // ... 创建 challenge 逻辑 ...
        otp.RecordChallengeCreated("email", "login", "success")
        return c.JSON(response)
    })
    
    app.Listen(":8080")
}
```

### Stargate（认证网关）

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
    
    // 在认证流程中使用
    auth.RecordSuccess("warden_otp")
    auth.RecordSessionCreated()
    
    // 跟踪外部服务调用
    heraldCalls.RecordSuccess("create_challenge", 150*time.Millisecond)
    wardenCalls.RecordSuccess("check_user", 50*time.Millisecond)
}
```

## API 参考

### 注册表

```go
registry := metrics.NewRegistry("myapp")
registry = metrics.NewRegistryWithSubsystem("myapp", "http")
registry = metrics.DefaultRegistry()            // 进程级默认注册表

registry.Namespace()
registry.Gatherer()                             // prometheus.Gatherer
registry.PrometheusRegistry()                   // 底层 *prometheus.Registry

registry.Register("name", collector)            // 按名称追踪
registry.Unregister("name")
registry.MustRegister(collectors...)
registry.UnregisterCollector(collector)         // 用于构建器创建的采集器
```

### 构建器

```go
registry.Counter("requests_total").Help("…").Labels("method").BuildVec()
registry.Gauge("queue_depth").Help("…").Build()
registry.Histogram("duration_seconds").Buckets(metrics.HTTPDurationBuckets()).BuildVec()
registry.Summary("payload_bytes").Build()
```

同一个指标名声明两次会**复用已有的采集器**而不是 panic，因此两个组件去取同一个计数器
都能拿到。

### 暴露端点处理器

```go
http.Handle("/metrics", metrics.Handler())                      // DefaultRegistry
http.Handle("/metrics", metrics.HandlerFor(registry))
http.Handle("/metrics", metrics.HandlerForGatherer(gatherer))
http.Handle("/metrics", metrics.NewHandler(metrics.DefaultHandlerOpts()))

metrics.RegisterHTTPHandler(mux, "/metrics")
metrics.RegisterHTTPHandlerFor(mux, "/metrics", registry)

app.Get("/metrics", metrics.FiberHandler())
app.Get("/metrics", metrics.FiberHandlerFor(registry))
app.Get("/metrics", metrics.FiberHandlerForGatherer(gatherer))
app.Get("/metrics", metrics.NewFiberHandler(metrics.DefaultHandlerOpts()))
```

它们都不做认证，详见[安全与部署](#安全与部署)。

### HTTP 指标

```go
cfg := metrics.DefaultHTTPMetricsConfig()
m := metrics.NewHTTPMetrics(cfg)                 // 拿到采集器自己驱动

app.Use(metrics.NewFiberMiddleware("myapp"))     // 或者
app.Use(metrics.NewFiberMiddlewareWithConfig(cfg))
```

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `Namespace` / `Subsystem` | 取自注册表 | 指标名前缀 |
| `Registry` | `DefaultRegistry()` | 采集器注册到哪里 |
| `PathTransformFunc` | `DefaultPathNormalize` | 中间件无条件应用 |
| `DisablePathNormalization` | `false` | 记录原始路径——先读警告 |
| `SkipPaths` | 无 | 这些路径不记录任何指标 |
| `DurationBuckets` | `HTTPDurationBuckets()` | |
| `SizeBuckets` | `DefaultBuckets()` | |
| `IncludeRequestSize` | | 取自 `Content-Length`，不读请求体 |
| `IncludeResponseSize` | | |
| `IncludeRequestsInFlight` | | |

### 通用指标

`metrics.NewCommonMetrics(registry)` 返回一个 `*CommonMetrics`，打包了现成的几组指标：
`AuthMetrics`、`OTPMetrics`、`CacheMetrics`、`RedisMetrics`、`RateLimitMetrics`、
`ExternalServiceMetrics` 和 `BackgroundTaskMetrics`。

## 安全与部署

- **保护 `/metrics` 端点**：`Handler()`、`HandlerFor()` 等返回的处理器不包含鉴权。请勿将 `/metrics` 暴露到公网。建议使用独立管理端口、网络策略、反向代理鉴权或 IP 白名单，仅允许监控系统抓取。
- **路径标签基数**：默认 HTTP 指标配置使用 `DefaultPathNormalize`，将 `/users/123` 规范为 `/users/:id`。生产环境务必做路径归一化或跳过部分路径，避免时间序列基数爆炸和 DoS 风险。`DefaultPathNormalize` 只替换**无歧义**的 id 形态：纯数字、UUID、长十六进制串与 ULID。nanoid 与 base64url 形态的分段不再替换——21 个字符的随机 token 与同长度的路由名（例如 `/oauth2CallbackHandler`）无法区分；`PathNormalizeWithTokens` 仅按形态去猜——**任意** 21-22 个字符的 URL-safe 分段，包括 `/forgot-password-reset`——而猜错会把真实端点静默并入 `/:id`。启用前请检查路由表中是否存在该长度的分段；若 id 有确定的精确形态，请改用自定义 `PathTransformFunc`。
- **不可信输入的标签值**：对来自用户或外部的标签值（如 CommonMetrics 的 scope、operation、provider），应只传入受控的枚举值，或使用 `SanitizeLabelValue(s, metrics.DefaultLabelValueMaxLength)` 做清洗，避免破坏 exposition 格式（如换行符）。示例：在调用 `rateLimit.RecordHit(scope)` 前执行 `scope := metrics.SanitizeLabelValue(userInput, metrics.DefaultLabelValueMaxLength)`。

## 注册表与 Unregister

- `Unregister(name)` 仅对通过 `Register(name, collector)` 注册的采集器生效。通过构建器 `Build()`/`BuildVec()` 创建的指标不按名称追踪，`Unregister` 无法处理它们。
- 移除构建器创建的采集器请保留其引用并调用 **`registry.UnregisterCollector(collector)`**。它在注销之外还会释放描述该采集器的 shape 记录——该记录强引用采集器，否则动态创建并移除大量唯一命名的向量会在注册表的整个生命周期内保留它们（连同其标签子项）。
- 直接调用 `registry.PrometheusRegistry().Unregister(collector)` 仍然可行，但本包无法感知该调用，shape 记录会被遗留。请优先使用 `UnregisterCollector`。
- 另外请注意：用**不同的标签名**重新注册同一指标名，无论如何都会在 Prometheus 内部 panic——`client_golang` 有意在整个进程生命周期内保留 `dimHashesByName`。

## 升级说明（v2.2.0）

新增一个字段和两个函数，没有删除任何东西。第一条会把崩溃变成正常运行——这正是目的。

- **重名指标不再让进程挂掉。** 所有构建器都经由 `MustRegister` 注册，因此同一个名字
  声明两次——两个组件去取同一个计数器，或者某个包在测试二进制里被初始化两次——会在启动
  时 panic 并带走整个服务。现在构建器会复用 `prometheus.AlreadyRegisteredError` 交回的
  采集器。注意：用**不同标签名**重新注册同一个名字，无论如何仍会在 Prometheus 内部
  panic——`client_golang` 有意在整个进程生命周期内保留 `dimHashesByName`。
- **原始路径不再会意外进入标签值。** 中间件此前只在 `PathTransformFunc` 非 nil 时才
  应用它。`DefaultMiddlewareConfig` 会设置它，但用结构体字面量构造的配置——
  `HTTPMetricsConfig{SkipPaths: …}`——会把它留成 nil，于是原始 URL 路径成了标签：
  请求随机 URL 会每个请求产生一条新时间序列。现在默认值由中间件应用，而不是假定来自
  构造函数。如果你此前是用字面量构造配置，**请预期路径标签变少、变粗**。
- **`DefaultPathNormalize` 识别更多 ID 形态。** 它此前只匹配数字、UUID 和 24 位及以上
  的十六进制，于是 16 位十六进制 ID、ULID 和 nanoid 还是漏进了标签。现在 16 位及以上
  十六进制和**大小写任意**的 ULID 都会被匹配——该编码本身是大小写无关的，各个库两种都
  会输出。
- **请求大小取自 `Content-Length`。** 中间件此前调用 `c.Body()`，会为每个请求物化整个
  请求体——包括处理器用流式读取或根本不读的那些——而且发生在链路更下游的任何体积限制
  之前。
- **`SanitizeLabelValue` 会转义双引号。** 它此前转义反斜杠和换行，但没转义 `"`，而标签
  是写成 `name="value"` 的——双引号和那两者一样能突破字段边界。
- **新增 `UnregisterCollector`**，这是移除构建器创建的采集器的方式。
  `Unregister(name)` 只能处理通过 `Register(name, collector)` 注册的采集器。走
  `PrometheusRegistry().Unregister` 仍然可行，但会遗留一条强引用该采集器的 shape 记录
  ——于是动态创建并移除大量唯一命名的向量，会在注册表的整个生命周期内保留它们全部
  （连同标签子项）。
- **新增 `PathNormalizeWithTokens` 和 `DisablePathNormalization`。** 详见
  [路径归一化](#路径归一化)——前者是一个可能合并真实端点的可选猜测，后者完全关闭归一化。
- **按名称的注册归各自的 wrapper 所有。** 同一个注册表上的两个 `DefaultRegistry()`
  wrapper 此前共享按名注册的映射，于是第二个的 `Register("x", c2)` 会覆盖第一个的条目，
  而第一个的 `Unregister("x")` 会移除*第二个*的采集器。
- **要求里写的是 Go 1.26**；`go.mod` 需要 `1.27.0`。

## 要求

- **Go 1.27+**（`go.mod` 声明 `go 1.27.0`）
- github.com/prometheus/client_golang v1.22.0+
- github.com/gofiber/fiber/v3 v3.4.0+（用于 Fiber 中间件）

此 v2 模块版本面向 Fiber v3。仍使用 Fiber v2 的应用应继续使用 `github.com/soulteary/metrics-kit` v1。

## 测试覆盖率

运行测试：

```bash
go test ./... -v

# 带覆盖率
go test ./... -coverprofile=coverage.out -covermode=atomic
go tool cover -html=coverage.out -o coverage.html
go tool cover -func=coverage.out
```

## 贡献

1. Fork 本仓库
2. 创建功能分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add some amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 提交 Pull Request

## 许可证

Apache License 2.0 —— 详见 [LICENSE](LICENSE)。
