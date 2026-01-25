package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// CommonMetrics provides frequently used metric patterns for services.
type CommonMetrics struct {
	registry *Registry
}

// NewCommonMetrics creates a new CommonMetrics instance.
func NewCommonMetrics(registry *Registry) *CommonMetrics {
	return &CommonMetrics{registry: registry}
}

// CacheMetrics holds cache-related metrics.
type CacheMetrics struct {
	Hits   prometheus.Counter
	Misses prometheus.Counter
	Size   prometheus.Gauge
	Errors prometheus.Counter
}

// NewCacheMetrics creates cache metrics with the given name prefix.
func (cm *CommonMetrics) NewCacheMetrics(name string) *CacheMetrics {
	r := cm.registry.WithSubsystem(name)
	return &CacheMetrics{
		Hits: r.Counter("cache_hits_total").
			Help("Total number of cache hits").
			Build(),
		Misses: r.Counter("cache_misses_total").
			Help("Total number of cache misses").
			Build(),
		Size: r.Gauge("cache_size").
			Help("Current number of items in cache").
			Build(),
		Errors: r.Counter("cache_errors_total").
			Help("Total number of cache errors").
			Build(),
	}
}

// RecordHit records a cache hit.
func (m *CacheMetrics) RecordHit() {
	m.Hits.Inc()
}

// RecordMiss records a cache miss.
func (m *CacheMetrics) RecordMiss() {
	m.Misses.Inc()
}

// SetSize sets the current cache size.
func (m *CacheMetrics) SetSize(size float64) {
	m.Size.Set(size)
}

// RecordError records a cache error.
func (m *CacheMetrics) RecordError() {
	m.Errors.Inc()
}

// RateLimitMetrics holds rate limiting metrics.
type RateLimitMetrics struct {
	Hits *prometheus.CounterVec
}

// NewRateLimitMetrics creates rate limit metrics.
func (cm *CommonMetrics) NewRateLimitMetrics() *RateLimitMetrics {
	return &RateLimitMetrics{
		Hits: cm.registry.Counter("rate_limit_hits_total").
			Help("Total number of rate limit hits").
			Labels("scope").
			BuildVec(),
	}
}

// RecordHit records a rate limit hit for the given scope.
// Common scopes: "user", "ip", "destination", "resend_cooldown"
func (m *RateLimitMetrics) RecordHit(scope string) {
	m.Hits.WithLabelValues(scope).Inc()
}

// RedisMetrics holds Redis operation metrics.
type RedisMetrics struct {
	OperationsTotal   *prometheus.CounterVec
	OperationDuration *prometheus.HistogramVec
	ConnectionsActive prometheus.Gauge
	ConnectionErrors  prometheus.Counter
}

// NewRedisMetrics creates Redis operation metrics.
func (cm *CommonMetrics) NewRedisMetrics() *RedisMetrics {
	r := cm.registry.WithSubsystem("redis")
	return &RedisMetrics{
		OperationsTotal: r.Counter("operations_total").
			Help("Total number of Redis operations").
			Labels("operation", "result").
			BuildVec(),
		OperationDuration: r.Histogram("operation_duration_seconds").
			Help("Redis operation duration in seconds").
			Labels("operation").
			Buckets(RedisDurationBuckets()).
			BuildVec(),
		ConnectionsActive: r.Gauge("connections_active").
			Help("Number of active Redis connections").
			Build(),
		ConnectionErrors: r.Counter("connection_errors_total").
			Help("Total number of Redis connection errors").
			Build(),
	}
}

// RecordOperation records a Redis operation.
func (m *RedisMetrics) RecordOperation(operation, result string, duration time.Duration) {
	m.OperationsTotal.WithLabelValues(operation, result).Inc()
	m.OperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// RecordSuccess records a successful Redis operation.
func (m *RedisMetrics) RecordSuccess(operation string, duration time.Duration) {
	m.RecordOperation(operation, "success", duration)
}

// RecordFailure records a failed Redis operation.
func (m *RedisMetrics) RecordFailure(operation string, duration time.Duration) {
	m.RecordOperation(operation, "failure", duration)
}

// SetActiveConnections sets the number of active connections.
func (m *RedisMetrics) SetActiveConnections(count float64) {
	m.ConnectionsActive.Set(count)
}

// RecordConnectionError records a connection error.
func (m *RedisMetrics) RecordConnectionError() {
	m.ConnectionErrors.Inc()
}

// ExternalServiceMetrics holds metrics for external service calls.
type ExternalServiceMetrics struct {
	CallsTotal   *prometheus.CounterVec
	CallDuration *prometheus.HistogramVec
}

// NewExternalServiceMetrics creates metrics for an external service.
func (cm *CommonMetrics) NewExternalServiceMetrics(serviceName string) *ExternalServiceMetrics {
	r := cm.registry.WithSubsystem(serviceName)
	return &ExternalServiceMetrics{
		CallsTotal: r.Counter("calls_total").
			Help("Total number of external service calls").
			Labels("operation", "result").
			BuildVec(),
		CallDuration: r.Histogram("call_duration_seconds").
			Help("External service call duration in seconds").
			Labels("operation").
			Buckets(ExternalAPIDurationBuckets()).
			BuildVec(),
	}
}

// RecordCall records an external service call.
func (m *ExternalServiceMetrics) RecordCall(operation, result string, duration time.Duration) {
	m.CallsTotal.WithLabelValues(operation, result).Inc()
	m.CallDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// RecordSuccess records a successful external service call.
func (m *ExternalServiceMetrics) RecordSuccess(operation string, duration time.Duration) {
	m.RecordCall(operation, "success", duration)
}

// RecordFailure records a failed external service call.
func (m *ExternalServiceMetrics) RecordFailure(operation string, duration time.Duration) {
	m.RecordCall(operation, "failure", duration)
}

// BackgroundTaskMetrics holds metrics for background tasks.
type BackgroundTaskMetrics struct {
	ExecutionsTotal *prometheus.CounterVec
	Duration        *prometheus.HistogramVec
	ErrorsTotal     prometheus.Counter
	Running         prometheus.Gauge
}

// NewBackgroundTaskMetrics creates background task metrics.
func (cm *CommonMetrics) NewBackgroundTaskMetrics() *BackgroundTaskMetrics {
	r := cm.registry.WithSubsystem("background_task")
	return &BackgroundTaskMetrics{
		ExecutionsTotal: r.Counter("executions_total").
			Help("Total number of background task executions").
			Labels("task", "result").
			BuildVec(),
		Duration: r.Histogram("duration_seconds").
			Help("Background task duration in seconds").
			Labels("task").
			Buckets(DefaultBuckets()).
			BuildVec(),
		ErrorsTotal: r.Counter("errors_total").
			Help("Total number of background task errors").
			Build(),
		Running: r.Gauge("running").
			Help("Number of currently running background tasks").
			Build(),
	}
}

// RecordExecution records a background task execution.
func (m *BackgroundTaskMetrics) RecordExecution(task, result string, duration time.Duration) {
	m.ExecutionsTotal.WithLabelValues(task, result).Inc()
	m.Duration.WithLabelValues(task).Observe(duration.Seconds())
	if result == "failure" {
		m.ErrorsTotal.Inc()
	}
}

// RecordSuccess records a successful task execution.
func (m *BackgroundTaskMetrics) RecordSuccess(task string, duration time.Duration) {
	m.RecordExecution(task, "success", duration)
}

// RecordFailure records a failed task execution.
func (m *BackgroundTaskMetrics) RecordFailure(task string, duration time.Duration) {
	m.RecordExecution(task, "failure", duration)
}

// IncRunning increments the running task count.
func (m *BackgroundTaskMetrics) IncRunning() {
	m.Running.Inc()
}

// DecRunning decrements the running task count.
func (m *BackgroundTaskMetrics) DecRunning() {
	m.Running.Dec()
}

// AuthMetrics holds authentication metrics.
type AuthMetrics struct {
	RequestsTotal     *prometheus.CounterVec
	SessionsCreated   prometheus.Counter
	SessionsDestroyed prometheus.Counter
}

// NewAuthMetrics creates authentication metrics.
func (cm *CommonMetrics) NewAuthMetrics() *AuthMetrics {
	r := cm.registry.WithSubsystem("auth")
	return &AuthMetrics{
		RequestsTotal: r.Counter("requests_total").
			Help("Total number of authentication requests").
			Labels("method", "result").
			BuildVec(),
		SessionsCreated: r.Counter("sessions_created_total").
			Help("Total number of sessions created").
			Build(),
		SessionsDestroyed: r.Counter("sessions_destroyed_total").
			Help("Total number of sessions destroyed").
			Build(),
	}
}

// RecordAuthRequest records an authentication request.
func (m *AuthMetrics) RecordAuthRequest(method, result string) {
	m.RequestsTotal.WithLabelValues(method, result).Inc()
}

// RecordSuccess records a successful authentication.
func (m *AuthMetrics) RecordSuccess(method string) {
	m.RecordAuthRequest(method, "success")
}

// RecordFailure records a failed authentication.
func (m *AuthMetrics) RecordFailure(method string) {
	m.RecordAuthRequest(method, "failure")
}

// RecordSessionCreated records a session creation.
func (m *AuthMetrics) RecordSessionCreated() {
	m.SessionsCreated.Inc()
}

// RecordSessionDestroyed records a session destruction.
func (m *AuthMetrics) RecordSessionDestroyed() {
	m.SessionsDestroyed.Inc()
}

// OTPMetrics holds OTP/verification code metrics.
type OTPMetrics struct {
	ChallengesTotal    *prometheus.CounterVec
	SendsTotal         *prometheus.CounterVec
	SendDuration       *prometheus.HistogramVec
	VerificationsTotal *prometheus.CounterVec
}

// NewOTPMetrics creates OTP metrics.
func (cm *CommonMetrics) NewOTPMetrics() *OTPMetrics {
	r := cm.registry.WithSubsystem("otp")
	return &OTPMetrics{
		ChallengesTotal: r.Counter("challenges_total").
			Help("Total number of OTP challenges created").
			Labels("channel", "purpose", "result").
			BuildVec(),
		SendsTotal: r.Counter("sends_total").
			Help("Total number of OTP sends via providers").
			Labels("channel", "provider", "result").
			BuildVec(),
		SendDuration: r.Histogram("send_duration_seconds").
			Help("Duration of OTP send operations in seconds").
			Labels("provider").
			Buckets(ExternalAPIDurationBuckets()).
			BuildVec(),
		VerificationsTotal: r.Counter("verifications_total").
			Help("Total number of OTP verifications").
			Labels("result", "reason").
			BuildVec(),
	}
}

// RecordChallengeCreated records a challenge creation event.
func (m *OTPMetrics) RecordChallengeCreated(channel, purpose, result string) {
	m.ChallengesTotal.WithLabelValues(channel, purpose, result).Inc()
}

// RecordSend records an OTP send event.
func (m *OTPMetrics) RecordSend(channel, provider, result string, duration time.Duration) {
	m.SendsTotal.WithLabelValues(channel, provider, result).Inc()
	m.SendDuration.WithLabelValues(provider).Observe(duration.Seconds())
}

// RecordVerification records a verification event.
func (m *OTPMetrics) RecordVerification(result, reason string) {
	m.VerificationsTotal.WithLabelValues(result, reason).Inc()
}
