package metrics

import (
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommonMetrics(t *testing.T) {
	r := NewRegistry("test")
	cm := NewCommonMetrics(r)
	require.NotNil(t, cm)
}

func TestCacheMetrics(t *testing.T) {
	r := NewRegistry("test_cache")
	cm := NewCommonMetrics(r)

	m := cm.NewCacheMetrics("users")
	require.NotNil(t, m)
	require.NotNil(t, m.Hits)
	require.NotNil(t, m.Misses)
	require.NotNil(t, m.Size)
	require.NotNil(t, m.Errors)

	// Test RecordHit
	m.RecordHit()
	metric := &dto.Metric{}
	err := m.Hits.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test RecordMiss
	m.RecordMiss()
	m.RecordMiss()
	err = m.Misses.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 2.0, metric.Counter.GetValue())

	// Test SetSize
	m.SetSize(100)
	err = m.Size.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 100.0, metric.Gauge.GetValue())

	// Test RecordError
	m.RecordError()
	err = m.Errors.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())
}

func TestRateLimitMetrics(t *testing.T) {
	r := NewRegistry("test_ratelimit")
	cm := NewCommonMetrics(r)

	m := cm.NewRateLimitMetrics()
	require.NotNil(t, m)
	require.NotNil(t, m.Hits)

	// Test RecordHit for different scopes
	scopes := []string{"user", "ip", "destination", "resend_cooldown"}
	for _, scope := range scopes {
		m.RecordHit(scope)

		metric := &dto.Metric{}
		err := m.Hits.WithLabelValues(scope).Write(metric)
		require.NoError(t, err)
		assert.Equal(t, 1.0, metric.Counter.GetValue())
	}
}

func TestRedisMetrics(t *testing.T) {
	r := NewRegistry("test_redis")
	cm := NewCommonMetrics(r)

	m := cm.NewRedisMetrics()
	require.NotNil(t, m)
	require.NotNil(t, m.OperationsTotal)
	require.NotNil(t, m.OperationDuration)
	require.NotNil(t, m.ConnectionsActive)
	require.NotNil(t, m.ConnectionErrors)

	// Test RecordOperation
	m.RecordOperation("get", "success", 5*time.Millisecond)
	metric := &dto.Metric{}
	err := m.OperationsTotal.WithLabelValues("get", "success").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test RecordSuccess
	m.RecordSuccess("set", 3*time.Millisecond)
	err = m.OperationsTotal.WithLabelValues("set", "success").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test RecordFailure
	m.RecordFailure("del", 10*time.Millisecond)
	err = m.OperationsTotal.WithLabelValues("del", "failure").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test SetActiveConnections
	m.SetActiveConnections(5)
	err = m.ConnectionsActive.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 5.0, metric.Gauge.GetValue())

	// Test RecordConnectionError
	m.RecordConnectionError()
	err = m.ConnectionErrors.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())
}

func TestExternalServiceMetrics(t *testing.T) {
	r := NewRegistry("test_external")
	cm := NewCommonMetrics(r)

	m := cm.NewExternalServiceMetrics("warden")
	require.NotNil(t, m)
	require.NotNil(t, m.CallsTotal)
	require.NotNil(t, m.CallDuration)

	// Test RecordCall
	m.RecordCall("check_user", "success", 100*time.Millisecond)
	metric := &dto.Metric{}
	err := m.CallsTotal.WithLabelValues("check_user", "success").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test RecordSuccess
	m.RecordSuccess("get_info", 50*time.Millisecond)
	err = m.CallsTotal.WithLabelValues("get_info", "success").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test RecordFailure
	m.RecordFailure("update_user", 200*time.Millisecond)
	err = m.CallsTotal.WithLabelValues("update_user", "failure").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())
}

func TestBackgroundTaskMetrics(t *testing.T) {
	r := NewRegistry("test_bg")
	cm := NewCommonMetrics(r)

	m := cm.NewBackgroundTaskMetrics()
	require.NotNil(t, m)
	require.NotNil(t, m.ExecutionsTotal)
	require.NotNil(t, m.Duration)
	require.NotNil(t, m.ErrorsTotal)
	require.NotNil(t, m.Running)

	// Test RecordExecution
	m.RecordExecution("sync", "success", 5*time.Second)
	metric := &dto.Metric{}
	err := m.ExecutionsTotal.WithLabelValues("sync", "success").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test RecordSuccess
	m.RecordSuccess("cleanup", 2*time.Second)
	err = m.ExecutionsTotal.WithLabelValues("cleanup", "success").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test RecordFailure (also increments errors)
	m.RecordFailure("backup", 10*time.Second)
	err = m.ExecutionsTotal.WithLabelValues("backup", "failure").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	err = m.ErrorsTotal.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test IncRunning/DecRunning
	m.IncRunning()
	m.IncRunning()
	err = m.Running.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 2.0, metric.Gauge.GetValue())

	m.DecRunning()
	err = m.Running.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Gauge.GetValue())
}

func TestAuthMetrics(t *testing.T) {
	r := NewRegistry("test_auth")
	cm := NewCommonMetrics(r)

	m := cm.NewAuthMetrics()
	require.NotNil(t, m)
	require.NotNil(t, m.RequestsTotal)
	require.NotNil(t, m.SessionsCreated)
	require.NotNil(t, m.SessionsDestroyed)

	// Test RecordAuthRequest
	m.RecordAuthRequest("password", "success")
	metric := &dto.Metric{}
	err := m.RequestsTotal.WithLabelValues("password", "success").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test RecordSuccess
	m.RecordSuccess("otp")
	err = m.RequestsTotal.WithLabelValues("otp", "success").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test RecordFailure
	m.RecordFailure("password")
	err = m.RequestsTotal.WithLabelValues("password", "failure").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test RecordSessionCreated
	m.RecordSessionCreated()
	m.RecordSessionCreated()
	err = m.SessionsCreated.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 2.0, metric.Counter.GetValue())

	// Test RecordSessionDestroyed
	m.RecordSessionDestroyed()
	err = m.SessionsDestroyed.Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())
}

func TestOTPMetrics(t *testing.T) {
	r := NewRegistry("test_otp")
	cm := NewCommonMetrics(r)

	m := cm.NewOTPMetrics()
	require.NotNil(t, m)
	require.NotNil(t, m.ChallengesTotal)
	require.NotNil(t, m.SendsTotal)
	require.NotNil(t, m.SendDuration)
	require.NotNil(t, m.VerificationsTotal)

	// Test RecordChallengeCreated
	m.RecordChallengeCreated("email", "login", "success")
	metric := &dto.Metric{}
	err := m.ChallengesTotal.WithLabelValues("email", "login", "success").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	m.RecordChallengeCreated("sms", "login", "success")
	err = m.ChallengesTotal.WithLabelValues("sms", "login", "success").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test RecordSend
	m.RecordSend("email", "smtp", "success", 100*time.Millisecond)
	err = m.SendsTotal.WithLabelValues("email", "smtp", "success").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	m.RecordSend("sms", "twilio", "failure", 500*time.Millisecond)
	err = m.SendsTotal.WithLabelValues("sms", "twilio", "failure").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	// Test RecordVerification
	m.RecordVerification("success", "")
	err = m.VerificationsTotal.WithLabelValues("success", "").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	m.RecordVerification("failure", "expired")
	err = m.VerificationsTotal.WithLabelValues("failure", "expired").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())

	m.RecordVerification("failure", "invalid")
	err = m.VerificationsTotal.WithLabelValues("failure", "invalid").Write(metric)
	require.NoError(t, err)
	assert.Equal(t, 1.0, metric.Counter.GetValue())
}
