package metrics

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHandler_TimeoutReturnsServiceUnavailable(t *testing.T) {
	r, entered, release := blockingScrapeRegistry(t)
	handler := NewHandler(HandlerOpts{Registry: r, Timeout: 1})
	recorder, done := startScrape(handler)
	t.Cleanup(func() {
		release()
		waitScrape(t, done)
	})

	waitScrape(t, entered)
	waitScrape(t, done)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Equal(t, "metrics scrape timeout\n", recorder.Body.String())
}

func TestNewHandler_MaxRequestsInFlightRejectsAndRecovers(t *testing.T) {
	r, entered, release := blockingScrapeRegistry(t)
	handler := NewHandler(HandlerOpts{Registry: r, MaxRequestsInFlight: 1})
	first, done := startScrape(handler)
	t.Cleanup(func() {
		release()
		waitScrape(t, done)
	})

	waitScrape(t, entered)
	busy, busyDone := startScrape(handler)
	t.Cleanup(func() {
		release()
		waitScrape(t, busyDone)
	})
	waitScrape(t, busyDone)
	assert.Equal(t, http.StatusServiceUnavailable, busy.Code)
	assert.Contains(t, busy.Body.String(), "Limit of concurrent requests reached")

	release()
	waitScrape(t, done)
	assert.Equal(t, http.StatusOK, first.Code)
	assert.Contains(t, first.Body.String(), "blocked_scrape_value 1")
	recovered := httptest.NewRecorder()
	handler.ServeHTTP(recovered, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Equal(t, http.StatusOK, recovered.Code, "the completed scrape must release its concurrency slot")
	assert.Contains(t, recovered.Body.String(), "blocked_scrape_value 1")
}

// Block collection, not description: registration must finish before a scrape.
func blockingScrapeRegistry(t *testing.T) (*Registry, <-chan struct{}, func()) {
	t.Helper()
	r := NewRegistry("blocked_scrape")
	c := &blockingScrapeCollector{
		desc:     prometheus.NewDesc("blocked_scrape_value", "A controllably blocked scrape", nil, nil),
		entered:  make(chan struct{}),
		released: make(chan struct{}),
		finished: make(chan struct{}),
	}
	r.MustRegister(c)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(c.released) }) }
	t.Cleanup(func() {
		release()
		waitScrape(t, c.finished)
	})
	return r, c.entered, release
}

type blockingScrapeCollector struct {
	desc                        *prometheus.Desc
	entered, released, finished chan struct{}
	enterOnce, finishOnce       sync.Once
}

func (c *blockingScrapeCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }
func (c *blockingScrapeCollector) Collect(ch chan<- prometheus.Metric) {
	defer c.finishOnce.Do(func() { close(c.finished) })
	c.enterOnce.Do(func() { close(c.entered) })
	<-c.released
	ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, 1)
}

func startScrape(handler http.Handler) (*httptest.ResponseRecorder, <-chan struct{}) {
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	}()
	return recorder, done
}

func waitScrape(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("scrape did not reach the expected state")
	}
}

func TestNewHandler_OpenMetricsNegotiation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			r := handlerOptionsRegistry()
			handler := NewHandler(HandlerOpts{Registry: r, EnableOpenMetrics: enabled})
			request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			request.Header.Set("Accept", "application/openmetrics-text; version=1.0.0")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, enabled, strings.Contains(recorder.Header().Get("Content-Type"), "application/openmetrics-text"))
			assert.Equal(t, enabled, strings.HasSuffix(recorder.Body.String(), "# EOF\n"))
			assert.Contains(t, recorder.Body.String(), "handler_options_requests_total 42")
		})
	}
}

func TestNewHandler_DisableCompression(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "compressed", true: "uncompressed"}[disabled], func(t *testing.T) {
			handler := NewHandler(HandlerOpts{Registry: handlerOptionsRegistry(), DisableCompression: disabled})
			request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			request.Header.Set("Accept-Encoding", "gzip")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			assert.Equal(t, http.StatusOK, recorder.Code)
			body := recorder.Body.String()
			if disabled {
				assert.Empty(t, recorder.Header().Get("Content-Encoding"))
			} else {
				assert.Equal(t, "gzip", recorder.Header().Get("Content-Encoding"))
				reader, err := gzip.NewReader(recorder.Body)
				require.NoError(t, err)
				t.Cleanup(func() { assert.NoError(t, reader.Close()) })
				decoded, err := io.ReadAll(reader)
				require.NoError(t, err)
				body = string(decoded)
			}
			assert.Contains(t, body, "handler_options_requests_total 42")
		})
	}
}

func handlerOptionsRegistry() *Registry {
	r := NewRegistry("handler_options")
	r.Counter("requests_total").Help("Recorded requests").Build().Add(42)
	return r
}

type failingScrapeCollector struct{ desc *prometheus.Desc }

func (c failingScrapeCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }
func (c failingScrapeCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.NewInvalidMetric(c.desc, errors.New("deliberate scrape failure"))
}

func TestNewHandler_ErrorHandlingAndLogging(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mode   promhttp.HandlerErrorHandling
		status int
	}{
		{"http error", promhttp.HTTPErrorOnError, http.StatusInternalServerError},
		{"continue", promhttp.ContinueOnError, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := handlerOptionsRegistry()
			r.MustRegister(failingScrapeCollector{prometheus.NewDesc("broken_scrape", "An invalid metric", nil, nil)})
			var logged bytes.Buffer
			handler := NewHandler(HandlerOpts{
				Registry: r, ErrorHandling: tc.mode, ErrorLog: log.New(&logged, "", 0),
			})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

			assert.Equal(t, tc.status, recorder.Code)
			assert.Contains(t, logged.String(), "deliberate scrape failure")
			if tc.status == http.StatusOK {
				assert.Contains(t, recorder.Body.String(), "handler_options_requests_total 42")
			} else {
				assert.Contains(t, recorder.Body.String(), "deliberate scrape failure")
			}
		})
	}
}
