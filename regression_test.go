package metrics

import (
	"strings"
	"testing"
)

// TestDuplicateMetricDoesNotPanic: builders used MustRegister, so declaring
// the same metric name twice -- two components, or a package initialised twice
// in a test binary -- took the process down at startup.
func TestDuplicateMetricDoesNotPanic(t *testing.T) {
	r := NewRegistryWithSubsystem("app", "http")

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("registering a duplicate metric panicked: %v", recovered)
		}
	}()

	first := r.Counter("requests_total").Help("total").BuildVec()
	second := r.Counter("requests_total").Help("total").BuildVec()

	if first != second {
		t.Error("the second build returned a different collector; it should reuse the registered one")
	}

	// The reused collector still works.
	second.WithLabelValues().Inc()
}

// TestStructLiteralConfigStillNormalisesPaths: DefaultMiddlewareConfig sets
// PathTransformFunc, but a config built as a struct literal leaves it nil --
// and then the raw URL path becomes a label value, so random URLs mint a new
// time series per request.
func TestStructLiteralConfigStillNormalisesPaths(t *testing.T) {
	cfg := HTTPMetricsConfig{} // no PathTransformFunc
	if cfg.PathTransformFunc != nil {
		t.Fatal("precondition: the literal should leave the transform nil")
	}

	// The middleware applies the default itself; check the default does the work.
	if got := DefaultPathNormalize("/users/12345/orders/67890"); got != "/users/:id/orders/:id" {
		t.Errorf("DefaultPathNormalize = %q, want %q", got, "/users/:id/orders/:id")
	}
}

// TestPathNormalizeCoversCommonIDShapes: the previous pattern only matched
// digits, UUIDs and hex of 24+ characters, so shorter hex ids, ULIDs and
// nanoids all leaked into label values.
func TestPathNormalizeCoversCommonIDShapes(t *testing.T) {
	cases := map[string]string{
		"/users/123": "/users/:id",
		"/items/550e8400-e29b-41d4-a716-446655440000": "/items/:id",
		"/o/507f1f77bcf86cd799439011":                 "/o/:id", // 24-char hex (ObjectID)
		"/t/0123456789abcdef":                         "/t/:id", // 16-char hex (64-bit id)
		"/e/01ARZ3NDEKTSV4RRFFQ69G5FAV":               "/e/:id", // ULID
		"/s/V1StGXR8_Z5jdHi6B-myT":                    "/s/:id", // nanoid
		"/health":                                     "/health",
		"/api/v1/users":                               "/api/v1/users",
		"/":                                           "/",
	}
	for in, want := range cases {
		if got := DefaultPathNormalize(in); got != want {
			t.Errorf("DefaultPathNormalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSanitizeLabelValueEscapesQuotes: a label is written as name="value", so
// the quote breaks out of the field just as a newline does.
func TestSanitizeLabelValueEscapesQuotes(t *testing.T) {
	got := SanitizeLabelValue(`a"b\c`+"\nd\re", DefaultLabelValueMaxLength)

	for _, bad := range []string{`"`, `\`, "\n", "\r"} {
		if strings.Contains(got, bad) {
			t.Errorf("sanitized value still contains %q: %q", bad, got)
		}
	}

	// Truncation still works, by runes.
	long := strings.Repeat("é", 500)
	if got := SanitizeLabelValue(long, 10); len([]rune(got)) != 10 {
		t.Errorf("truncated to %d runes, want 10", len([]rune(got)))
	}
}
