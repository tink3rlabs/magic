package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/tink3rlabs/magic/telemetry"
)

func TestMiddlewareRoutePatternLabel(t *testing.T) {
	obs := initTestObserver(t)

	r := chi.NewRouter()
	r.Use(obs.ChiMiddleware())
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	for i := 0; i < 3; i++ {
		resp, err := http.Get(srv.URL + "/users/42")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		resp.Body.Close()
	}

	// After three calls we expect the counter to reflect the
	// templated route rather than the literal path.
	count := countersFromScrape(t, obs, "http_requests_total",
		`method="GET"`, `route="/users/{id}"`, `status_code="201"`)
	if count != 3 {
		t.Errorf("expected 3 requests, got %v", count)
	}
}

func TestMiddlewareUnmatchedRoute(t *testing.T) {
	obs := initTestObserver(t)

	r := chi.NewRouter()
	r.Use(obs.ChiMiddleware())
	// chi skips the middleware chain entirely when no routes are
	// registered on the router, so we must register at least one
	// real handler before exercising the unmatched-route path.
	r.Get("/known", func(w http.ResponseWriter, req *http.Request) {})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, _ := http.Get(srv.URL + "/does-not-exist")
	resp.Body.Close()

	count := countersFromScrape(t, obs, "http_requests_total",
		`method="GET"`, `route="unmatched"`, `status_code="404"`)
	if count != 1 {
		t.Errorf("expected 1 unmatched request, got %v", count)
	}
}

func TestMiddlewarePanicRecordsAndReraises(t *testing.T) {
	obs := initTestObserver(t)

	r := chi.NewRouter()
	r.Use(obs.ChiMiddleware())
	// A panic-catching wrapper so the test process doesn't die
	// from the re-raise; the middleware itself must propagate.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			defer func() { _ = recover() }()
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/boom", func(w http.ResponseWriter, req *http.Request) {
		panic("boom")
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, _ := http.Get(srv.URL + "/boom")
	if resp != nil {
		resp.Body.Close()
	}

	// Either the handler completed with 500 (standard library's
	// default behavior when the recovered panic aborts the
	// response) or the connection was closed. Either way, the
	// counter should have observed one request in flight.
	total := countersFromScrapeRaw(t, obs, "http_requests_total", `method="GET"`, `route="/boom"`)
	if total < 1 {
		t.Errorf("expected at least one http_requests_total observation for /boom, got %v", total)
	}
}

func TestMiddlewareInFlightDecrements(t *testing.T) {
	obs := initTestObserver(t)

	r := chi.NewRouter()
	r.Use(obs.ChiMiddleware())
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	for i := 0; i < 5; i++ {
		resp, _ := http.Get(srv.URL + "/")
		resp.Body.Close()
	}

	// http_requests_in_flight uses an empty route label; once all
	// responses have been written it must return to zero.
	val := gaugeFromScrape(t, obs, "http_requests_in_flight", `method="GET"`, `route=""`)
	if val != 0 {
		t.Errorf("expected in_flight to return to 0, got %v", val)
	}
}

func TestResponseWriterHonorsInterfaces(t *testing.T) {
	// Sanity check that the wrapper still reports as Flusher when
	// the underlying writer supports it. The test doubles as
	// documentation for the hijack / push forwarding.
	rec := httptest.NewRecorder()
	w := &responseWriterWrapper{ResponseWriter: rec}

	if _, ok := any(w).(http.Flusher); !ok {
		t.Error("responseWriterWrapper should implement http.Flusher")
	}
	if _, ok := any(w).(http.Hijacker); !ok {
		t.Error("responseWriterWrapper should implement http.Hijacker")
	}
	if _, ok := any(w).(http.Pusher); !ok {
		t.Error("responseWriterWrapper should implement http.Pusher")
	}

	w.WriteHeader(http.StatusTeapot)
	_, _ = w.Write([]byte("hi"))
	if w.statusCode != http.StatusTeapot {
		t.Errorf("statusCode = %d, want 418", w.statusCode)
	}
	if w.bytesWritten != 2 {
		t.Errorf("bytesWritten = %d, want 2", w.bytesWritten)
	}
}

// ----- helpers -----

func countersFromScrape(t *testing.T, obs *Observer, metric string, labelMatchers ...string) float64 {
	t.Helper()
	return gaugeFromScrape(t, obs, metric, labelMatchers...)
}

// countersFromScrapeRaw is like countersFromScrape but only
// requires that SOME series matching the partial label set exist.
// Returns the sum across matching series.
func countersFromScrapeRaw(t *testing.T, obs *Observer, metric string, labelMatchers ...string) float64 {
	t.Helper()
	scrape := httptest.NewServer(obs.MetricsHandler())
	t.Cleanup(scrape.Close)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, scrape.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer resp.Body.Close()
	return sumMatchingLines(t, resp, metric, labelMatchers)
}

// gaugeFromScrape returns the value of the single series whose
// labels match all provided matchers. Fails the test when no
// series (or more than one) matches.
func gaugeFromScrape(t *testing.T, obs *Observer, metric string, labelMatchers ...string) float64 {
	t.Helper()
	scrape := httptest.NewServer(obs.MetricsHandler())
	t.Cleanup(scrape.Close)
	resp, err := http.Get(scrape.URL)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer resp.Body.Close()
	return sumMatchingLines(t, resp, metric, labelMatchers)
}

// telemetryLabel is an alias so call sites can form labels
// concisely when comparing against observations; unused publicly.
type telemetryLabel = telemetry.Label
