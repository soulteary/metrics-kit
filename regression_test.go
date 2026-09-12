package metrics

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
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

// --- Codex review round 2 (PR #4) ---

// TestSubsystemViewsShareTheMutex is the regression test for WithSubsystem
// copying the Registry struct: each view got its own zero-value mutex while
// the collectors and shapes maps stayed shared, so two views writing
// concurrently -- even in different subsystems -- raced and could kill the
// process with "concurrent map writes".
func TestSubsystemViewsShareTheMutex(t *testing.T) {
	r := NewRegistry("app")

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sub := r.WithSubsystem(fmt.Sprintf("sub%d", i))
			sub.Histogram("latency_seconds").
				Help("Latency").
				Labels("route").
				Buckets([]float64{0.1, 1}).
				BuildVec()
		}(i)
	}
	wg.Wait()
}

// TestDuplicateVectorWithSwappedLabelOrderIsReported: a Prometheus descriptor's
// identity is the label NAME SET, so Labels("method","path") and
// Labels("path","method") collide. AlreadyRegisteredError then returned the
// first vector with its original positional ordering, silently swapping the two
// values in the second caller's WithLabelValues calls.
func TestDuplicateVectorWithSwappedLabelOrderIsReported(t *testing.T) {
	r := NewRegistry("app")

	r.Counter("requests_total").Help("Requests").Labels("method", "path").BuildVec()

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("re-registering with a swapped label order was silently accepted; the second caller's label values would be swapped")
		}
		if msg := fmt.Sprint(rec); !strings.Contains(msg, "different configuration") {
			t.Errorf("panic message = %q, want it to name the configuration conflict", msg)
		}
	}()
	r.Counter("requests_total").Help("Requests").Labels("path", "method").BuildVec()
}

// TestDefaultBucketsCanonicalize: Prometheus substitutes DefBuckets for nil, so
// Histogram() (which seeds DefBuckets) and .Buckets(nil) describe the same
// layout and must not be reported as a conflict.
func TestDefaultBucketsCanonicalize(t *testing.T) {
	r := NewRegistry("app")

	defer func() {
		if rec := recover(); rec != nil {
			t.Errorf("nil buckets and an explicit DefBuckets were treated as a conflict: %v", rec)
		}
	}()

	r.Histogram("size_bytes").Help("Size").Build()                                // seeded DefBuckets
	r.Histogram("size_bytes").Help("Size").Buckets(nil).Build()                   // nil
	r.Histogram("size_bytes").Help("Size").Buckets(prometheus.DefBuckets).Build() // explicit
}

// TestShapeKnowledgeFollowsTheUnderlyingRegistry: two wrappers over one
// prometheus.Registry register into the same place, so a shape recorded through
// one must be visible to the other. Keeping shapes per-wrapper left the second
// with no prior entry, so it returned the incompatible existing collector.
func TestShapeKnowledgeFollowsTheUnderlyingRegistry(t *testing.T) {
	shared := prometheus.NewRegistry()
	first := &Registry{registry: shared, namespace: "app", state: stateFor(shared)}
	second := &Registry{registry: shared, namespace: "app", state: stateFor(shared)}

	first.Histogram("latency_seconds").Help("Latency").Buckets([]float64{0.1, 1}).Build()

	defer func() {
		if rec := recover(); rec == nil {
			t.Error("a differing registration through a second wrapper over the same registry was silently accepted")
		}
	}()
	second.Histogram("latency_seconds").Help("Latency").Buckets([]float64{5, 10}).Build()
}

// TestUnknownPriorShapeIsNotAssumedCompatible: when a descriptor was registered
// outside the builders, its buckets, objectives and label order cannot be read
// back through the Prometheus API, so compatibility is unverifiable and
// returning the existing collector would hide exactly the mismatch this check
// exists to catch.
func TestUnknownPriorShapeIsNotAssumedCompatible(t *testing.T) {
	r := NewRegistry("app")

	// Registered behind the builders' back.
	r.MustRegister(prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "app",
		Name:      "external_seconds",
		Help:      "External",
		Buckets:   []float64{0.1, 1},
	}))

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("reuse of an externally registered collector was silently accepted")
		}
		if msg := fmt.Sprint(rec); !strings.Contains(msg, "unknown") {
			t.Errorf("panic message = %q, want it to say the prior shape is unknown", msg)
		}
	}()
	r.Histogram("external_seconds").Help("External").Buckets([]float64{5, 10}).Build()
}

// TestConcurrentBuildOfOneNewMetricDoesNotPanic is the regression test for
// publishing the shape before Register decided who owns the descriptor. Both
// goroutines recorded a shape, then the loser of the Prometheus registration
// race read its own AlreadyRegisteredError against a shapes entry it had seen
// as absent -- and panicked with "registered outside this package" over a
// collector this very package had just built.
func TestConcurrentBuildOfOneNewMetricDoesNotPanic(t *testing.T) {
	const goroutines = 64

	for attempt := 0; attempt < 300; attempt++ {
		r := NewRegistry("app")

		var start sync.WaitGroup
		start.Add(1)
		var done sync.WaitGroup
		panics := make(chan any, goroutines)
		built := make(chan prometheus.Histogram, goroutines)

		for i := 0; i < goroutines; i++ {
			done.Add(1)
			go func() {
				defer done.Done()
				defer func() {
					if rec := recover(); rec != nil {
						panics <- rec
					}
				}()
				start.Wait()
				built <- r.Histogram("latency_seconds").Help("Latency").Buckets([]float64{0.1, 1}).Build()
			}()
		}

		start.Done()
		done.Wait()
		close(panics)
		close(built)

		if rec, ok := <-panics; ok {
			t.Fatalf("concurrent build of one new metric panicked: %v", rec)
		}

		var first prometheus.Histogram
		n := 0
		for h := range built {
			n++
			if first == nil {
				first = h
			} else if h != first {
				t.Fatal("concurrent builds of one metric returned different collectors")
			}
		}
		if n != goroutines {
			t.Fatalf("%d builds completed, want %d", n, goroutines)
		}
	}
}

// TestRejectedExternalShapeIsNotRemembered is the regression test for leaving
// the requested shape behind after an external-collector panic. The retry then
// found its own unverified entry, matched it against itself, and returned the
// incompatible external collector the first attempt had refused.
func TestRejectedExternalShapeIsNotRemembered(t *testing.T) {
	r := NewRegistry("app")

	r.MustRegister(prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "app",
		Name:      "external_seconds",
		Help:      "External",
		Buckets:   []float64{0.1, 1},
	}))

	build := func() (rec any) {
		defer func() { rec = recover() }()
		r.Histogram("external_seconds").Help("External").Buckets([]float64{5, 10}).Build()
		return nil
	}

	if build() == nil {
		t.Fatal("the first attempt did not refuse the externally registered collector")
	}
	second := build()
	if second == nil {
		t.Fatal("the retry reused the external collector; the refused shape was remembered as trusted")
	}
	if msg := fmt.Sprint(second); !strings.Contains(msg, "unknown") {
		t.Errorf("retry panic = %q, want it to still report the prior shape as unknown", msg)
	}
}

// TestShapeIsReplaceableAfterUnregister is the regression test for comparing
// against a collector that is gone. Prometheus accepts a different layout once
// the old collector is unregistered, but the shape record outlived it and
// refused the valid replacement -- breaking metric replacement during reloads
// and tests.
func TestShapeIsReplaceableAfterUnregister(t *testing.T) {
	r := NewRegistry("app")

	first := r.Histogram("latency_seconds").Help("Latency").Buckets([]float64{0.1, 1}).Build()
	if !r.PrometheusRegistry().Unregister(first) {
		t.Fatal("Unregister reported the collector was not registered")
	}

	defer func() {
		if rec := recover(); rec != nil {
			t.Errorf("re-registering after Unregister panicked: %v", rec)
		}
	}()
	second := r.Histogram("latency_seconds").Help("Latency").Buckets([]float64{5, 10}).Build()
	if second == first {
		t.Error("re-registration returned the unregistered collector")
	}
}

// TestConstLabelsSeparateMetricIdentities is the regression test for keying
// shapes on name and variable labels alone. Prometheus registers two
// collectors differing only in constant-label VALUES side by side, but they
// shared one shape record here, so a valid pair -- one histogram per tenant,
// say, with different buckets -- panicked as a conflict before Prometheus ever
// saw the second registration.
func TestConstLabelsSeparateMetricIdentities(t *testing.T) {
	r := NewRegistry("app")

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("two collectors distinguished by their const labels were reported as a conflict: %v", rec)
		}
	}()

	a := r.Histogram("latency_seconds").Help("Latency").
		ConstLabels(prometheus.Labels{"tenant": "a"}).Buckets([]float64{0.1, 1}).Build()
	b := r.Histogram("latency_seconds").Help("Latency").
		ConstLabels(prometheus.Labels{"tenant": "b"}).Buckets([]float64{5, 10}).Build()

	if a == b {
		t.Error("the two const-label variants returned the same collector")
	}

	// The guard still applies WITHIN one const-label identity.
	conflict := func() (rec any) {
		defer func() { rec = recover() }()
		r.Histogram("latency_seconds").Help("Latency").
			ConstLabels(prometheus.Labels{"tenant": "a"}).Buckets([]float64{99}).Build()
		return nil
	}()
	if conflict == nil {
		t.Error("differing buckets under the same const labels were silently accepted")
	}
}

// --- Codex review round 4 (PR #4) ---

// TestGaugeIsNotReusedAsACounter is the regression test for leaving the
// collector KIND out of the shape.
//
// Prometheus does not distinguish kinds in a descriptor, and neither do Go's
// interfaces: a gauge structurally implements Inc and Add, so
// prometheus.Counter accepts one. The type assertion therefore succeeded and
// handed back a gauge as a counter -- exported as a gauge, and accepting the
// negative Add calls a counter must refuse.
func TestGaugeIsNotReusedAsACounter(t *testing.T) {
	r := NewRegistry("app")
	r.Gauge("requests").Help("Requests").Build()

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("a gauge was returned as a counter")
		}
		if msg := fmt.Sprint(rec); !strings.Contains(msg, "kind=") {
			t.Errorf("panic = %q, want it to report the kind mismatch", msg)
		}
	}()
	r.Counter("requests").Help("Requests").Build()
}

// TestSummaryIsNotReusedAsAHistogram pins the same property for Observe: a
// summary satisfies prometheus.Histogram, since both carry it.
//
// Unlike the gauge/counter case this was ALREADY refused before kindShape --
// their fingerprints differ anyway, one ending in objectives= and the other
// in buckets= -- so this is a guard against that incidental protection being
// refactored away, not a regression test. It passes with or without the kind
// tag.
func TestSummaryIsNotReusedAsAHistogram(t *testing.T) {
	r := NewRegistry("app")
	r.Summary("latency_seconds").Help("Latency").Build()

	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("a summary was returned as a histogram")
		}
	}()
	r.Histogram("latency_seconds").Help("Latency").Build()
}

// TestShapeIsNotTrustedForAnotherCollector is the regression test for keying
// the shape on the metric id alone. A descriptor can change hands without this
// package noticing -- builder registration, Unregister, then a raw external
// registration with the same descriptor and different buckets -- and the
// recorded shape still described the DEPARTED collector, so a matching
// fingerprint handed back the incompatible external one as verified.
func TestShapeIsNotTrustedForAnotherCollector(t *testing.T) {
	r := NewRegistry("app")

	mine := r.Histogram("latency_seconds").Help("Latency").Buckets([]float64{0.1, 1}).Build()
	if !r.PrometheusRegistry().Unregister(mine) {
		t.Fatal("Unregister reported the collector was not registered")
	}

	// Someone registers their own collector for the same descriptor, with a
	// different layout, behind the builders' back.
	external := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "app",
		Name:      "latency_seconds",
		Help:      "Latency",
		Buckets:   []float64{5, 10},
	})
	r.MustRegister(external)

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("the external collector was reused on a shape recorded for a different one")
		}
		if msg := fmt.Sprint(rec); !strings.Contains(msg, "unknown") {
			t.Errorf("panic = %q, want it to report the prior shape as unknown", msg)
		}
	}()
	// The SAME buckets the record holds: a shape comparison alone would match.
	r.Histogram("latency_seconds").Help("Latency").Buckets([]float64{0.1, 1}).Build()
}

// TestTerminalInfinityBucketCanonicalizes is the regression test for keeping an
// explicit trailing +Inf in the fingerprint. Prometheus appends that bucket
// itself and strips a supplied one, so the two declarations are the same
// histogram and the second registration was refused as a conflict.
func TestTerminalInfinityBucketCanonicalizes(t *testing.T) {
	r := NewRegistry("app")

	first := r.Histogram("latency_seconds").Help("Latency").
		Buckets([]float64{0.1, 1}).Build()

	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("an explicit +Inf was reported as a conflict: %v", rec)
			}
		}()
		if second := r.Histogram("latency_seconds").Help("Latency").
			Buckets([]float64{0.1, 1, math.Inf(1)}).Build(); second != first {
			t.Error("the two declarations returned different collectors")
		}
	}()

	// DefBuckets plus an explicit +Inf is still the default layout.
	if got, want := bucketShape(append(append([]float64(nil), prometheus.DefBuckets...), math.Inf(1))),
		bucketShape(nil); got != want {
		t.Errorf("bucketShape(DefBuckets+Inf) = %q, want %q", got, want)
	}

	// A LONE +Inf is not the default layout: empty means DefBuckets, a lone
	// +Inf means no finite bounds at all.
	if bucketShape([]float64{math.Inf(1)}) == bucketShape(nil) {
		t.Error("a lone +Inf was conflated with the default buckets")
	}
}
