# metrics-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/metrics-kit.svg)](https://pkg.go.dev/github.com/soulteary/metrics-kit)
[![Go Report Card](https://goreportcard.com/badge/github.com/soulteary/metrics-kit)](https://goreportcard.com/report/github.com/soulteary/metrics-kit)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/metrics-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/metrics-kit)

[English](README.md)

统一的 Go 服务 Prometheus 指标工具包。提供指标构建器、注册表管理、HTTP 处理器和中间件，实现跨服务的一致性指标收集。

## 特性

- **注册表管理**：支持命名空间/子系统的自定义 Prometheus 注册表
- **流式构建器**：Counter、Gauge、Histogram、Summary 构建器，支持链式调用
- **HTTP 处理器**：标准库和 Fiber 兼容的 `/metrics` 端点处理器
- **HTTP 中间件**：Fiber 框架的请求指标收集中间件
- **通用指标**：预置的缓存、限流、Redis、认证、OTP 等常用指标模式
- **桶预设**：HTTP、Redis、外部 API、字节大小的预定义直方图桶

## 安装

```bash
go get github.com/soulteary/metrics-kit
```

## 使用

### 基础注册表和计数器

```go
import (
    metrics "github.com/soulteary/metrics-kit"
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
    metrics "github.com/soulteary/metrics-kit"
)

// 标准库
http.Handle("/metrics", metrics.Handler())

// 使用自定义注册表
http.Handle("/metrics", metrics.HandlerFor(registry))

// Fiber 框架
app.Get("/metrics", metrics.FiberHandler())
```

### HTTP 中间件 (Fiber)

```go
import (
    "github.com/gofiber/fiber/v2"
    metrics "github.com/soulteary/metrics-kit"
)

app := fiber.New()

// 简单中间件
app.Use(metrics.NewFiberMiddleware("myservice"))

// 自定义配置
cfg := metrics.HTTPMetricsConfig{
    Namespace:               "myservice",
    Subsystem:               "api",
    SkipPaths:               []string{"/health", "/metrics"},
    IncludeRequestSize:      true,
    IncludeResponseSize:     true,
    IncludeRequestsInFlight: true,
    PathTransformFunc: func(path string) string {
        // 规范化带 ID 的路径
        // /users/123 -> /users/:id
        return path
    },
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
    "github.com/gofiber/fiber/v2"
    metrics "github.com/soulteary/metrics-kit"
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
    app.Post("/v1/otp/challenges", func(c *fiber.Ctx) error {
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
    metrics "github.com/soulteary/metrics-kit"
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

## 要求

- Go 1.25 或更高版本
- github.com/prometheus/client_golang v1.22.0+
- github.com/gofiber/fiber/v2 v2.52.6+（用于 Fiber 中间件）

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

详见 [LICENSE](LICENSE) 文件。
