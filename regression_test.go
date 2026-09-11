package metrics

import (
	"fmt"
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

// --- Codex review follow-ups (PR #4) ---

// TestDuplicateHistogramWithDifferentBucketsIsReported is the regression test
// for AlreadyRegisteredError reuse. Bucket boundaries are not part of a
// Prometheus descriptor, so two components declaring the same histogram name,
// help and labels with different buckets are duplicates -- and handing back the
// first silently aggregated the second's observations into a layout it never
// asked for.
func TestDuplicateHistogramWithDifferentBucketsIsReported(t *testing.T) {
	r := NewRegistry("app")

	build := func(buckets []float64) {
		r.Histogram("latency_seconds").
			Help("Request latency").
			Labels("route").
			Buckets(buckets).
			BuildVec()
	}

	build([]float64{0.1, 0.5, 1})

	// The identical registration is still reused, not a panic.
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Errorf("re-registering an identical histogram panicked: %v", rec)
			}
		}()
		build([]float64{0.1, 0.5, 1})
	}()

	// A different layout is a configuration conflict.
	func() {
		defer func() {
			rec := recover()
			if rec == nil {
				t.Fatal("re-registering with different buckets was silently accepted; the second component's buckets are discarded")
			}
			if msg := fmt.Sprint(rec); !strings.Contains(msg, "different configuration") {
				t.Errorf("panic message = %q, want it to name the configuration conflict", msg)
			}
		}()
		build([]float64{1, 2, 3})
	}()
}

// TestDuplicateSummaryWithDifferentObjectivesIsReported: summaries have the
// analogous problem -- objectives are not part of the descriptor either.
func TestDuplicateSummaryWithDifferentObjectivesIsReported(t *testing.T) {
	r := NewRegistry("app")

	build := func(objectives map[float64]float64) {
		r.Summary("size_bytes").Help("Response size").Objectives(objectives).Build()
	}

	build(map[float64]float64{0.5: 0.05, 0.9: 0.01})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error("re-registering with different objectives was silently accepted")
		}
	}()
	build(map[float64]float64{0.99: 0.001})
}

// TestNilPathTransformHasAnExplicitOptOut is the regression test for nil
// PathTransformFunc having to mean two things at once. Nil now means the safe
// default; raw paths are spelled DisablePathNormalization.
func TestNilPathTransformHasAnExplicitOptOut(t *testing.T) {
	cfg := DefaultHTTPMetricsConfig()
	cfg.PathTransformFunc = nil
	if got, want := cfg.transformPath("/users/123"), "/users/:id"; got != want {
		t.Errorf("nil transform gave %q, want %q (the safe default)", got, want)
	}

	cfg.DisablePathNormalization = true
	if got, want := cfg.transformPath("/users/123"), "/users/123"; got != want {
		t.Errorf("DisablePathNormalization gave %q, want the raw %q", got, want)
	}

	// A custom function still wins over the default.
	cfg = DefaultHTTPMetricsConfig()
	cfg.PathTransformFunc = func(string) string { return "/fixed" }
	if got := cfg.transformPath("/users/123"); got != "/fixed" {
		t.Errorf("custom transform gave %q, want /fixed", got)
	}
}

// TestStaticRoutesAreNotMistakenForTokens is the regression test for the nanoid
// heuristic matching on length alone: "/forgot-password-reset" is exactly 21
// URL-safe characters, and normalising it to /:id merges an unrelated static
// route into the id bucket, corrupting its request counts and latencies.
func TestStaticRoutesAreNotMistakenForTokens(t *testing.T) {
	static := []string{
		"/forgot-password-reset",
		"/account/email-verification",
		"/subscription-management",
		"/api/password_reset_token",
	}
	for _, p := range static {
		if got := DefaultPathNormalize(p); got != p {
			t.Errorf("DefaultPathNormalize(%q) = %q, want it unchanged -- a static route is not an id", p, got)
		}
	}

	// Real generated tokens are still normalised.
	tokens := map[string]string{
		"/s/V1StGXR8_Z5jdHi6B-myT":                    "/s/:id",
		"/t/Uakgb1J5m9AI0EoMlqbP7":                    "/t/:id",
		"/users/123":                                  "/users/:id",
		"/items/550e8400-e29b-41d4-a716-446655440000": "/items/:id",
	}
	for in, want := range tokens {
		if got := DefaultPathNormalize(in); got != want {
			t.Errorf("DefaultPathNormalize(%q) = %q, want %q", in, got, want)
		}
	}
}
