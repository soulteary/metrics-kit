package metrics

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeLabelValue_Empty(t *testing.T) {
	assert.Equal(t, "", SanitizeLabelValue("", 0))
	assert.Equal(t, "", SanitizeLabelValue("", 10))
}

func TestSanitizeLabelValue_NewlinesAndBackslash(t *testing.T) {
	// Newlines and backslash can break Prometheus exposition format
	in := "a\nb\rc\\d"
	out := SanitizeLabelValue(in, 0)
	assert.NotContains(t, out, "\n")
	assert.NotContains(t, out, "\r")
	assert.NotContains(t, out, "\\")
	assert.Equal(t, "a b c_d", out)
}

func TestSanitizeLabelValue_TrimSpace(t *testing.T) {
	assert.Equal(t, "x", SanitizeLabelValue("  x  ", 0))
}

func TestSanitizeLabelValue_Truncate(t *testing.T) {
	s := "hello世界"
	out := SanitizeLabelValue(s, 5)
	assert.Equal(t, 5, len([]rune(out)))
	assert.Equal(t, "hello", out)

	out0 := SanitizeLabelValue(s, 0)
	assert.Equal(t, s, out0)
}

func TestSanitizeLabelValue_NoTruncateWhenUnderLimit(t *testing.T) {
	s := "short"
	assert.Equal(t, s, SanitizeLabelValue(s, 10))
}

func TestSanitizeLabelValue_SafeForExposition(t *testing.T) {
	// Values that could inject extra lines must be sanitized
	malicious := []string{"metric\nvalue 1", "label\r\ninjection", "back\\slash"}
	for _, s := range malicious {
		out := SanitizeLabelValue(s, 0)
		assert.False(t, strings.ContainsAny(out, "\n\r"), "output should not contain newline or carriage return: %q", out)
	}
}

func TestDefaultPathNormalize_EmptyAndRoot(t *testing.T) {
	assert.Equal(t, "", DefaultPathNormalize(""))
	assert.Equal(t, "/", DefaultPathNormalize("/"))
}

func TestDefaultPathNormalize_NumericSegment(t *testing.T) {
	assert.Equal(t, "/users/:id", DefaultPathNormalize("/users/123"))
	assert.Equal(t, "/users/:id/profile", DefaultPathNormalize("/users/123/profile"))
	assert.Equal(t, "/api/v1/items/:id", DefaultPathNormalize("/api/v1/items/999"))
}

func TestDefaultPathNormalize_UUIDSegment(t *testing.T) {
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	assert.Equal(t, "/items/:id", DefaultPathNormalize("/items/"+uuid))
	assert.Equal(t, "/orders/:id/items", DefaultPathNormalize("/orders/"+uuid+"/items"))
}

func TestDefaultPathNormalize_NonIDSegmentsUnchanged(t *testing.T) {
	assert.Equal(t, "/api/users", DefaultPathNormalize("/api/users"))
	assert.Equal(t, "/health", DefaultPathNormalize("/health"))
	assert.Equal(t, "/users/me", DefaultPathNormalize("/users/me"))
}

func TestDefaultPathNormalize_MultipleIDs(t *testing.T) {
	assert.Equal(t, "/users/:id/orders/:id", DefaultPathNormalize("/users/1/orders/42"))
}

func TestDefaultPathNormalize_OutputUsableAsLabel(t *testing.T) {
	// Ensure normalized path can be used in WithLabelValues without breaking exposition
	r := NewRegistry("test_labels")
	counter := r.Counter("path_hits_total").Help("Hits").Labels("path").BuildVec()
	paths := []string{
		DefaultPathNormalize("/users/123"),
		DefaultPathNormalize("/users/456"),
	}
	for _, p := range paths {
		counter.WithLabelValues(p).Inc()
	}
	mfs, err := r.PrometheusRegistry().Gather()
	require.NoError(t, err)
	require.NotEmpty(t, mfs)
}
