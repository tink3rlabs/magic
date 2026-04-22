package middlewares

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/tink3rlabs/magic/observability"
)

func testObserver(t *testing.T) *observability.Observer {
	t.Helper()
	cfg := observability.DefaultConfig()
	cfg.ServiceName = "middlewares-test"
	cfg.MetricsMode = observability.MetricsModePrometheus
	cfg.EnableTracing = false

	obs, err := observability.Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		_ = obs.Shutdown(context.Background())
	})
	return obs
}

func TestObservabilityNilIsIdentity(t *testing.T) {
	mw := Observability(nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusNoContent)
	}
}

func TestObservabilityMiddlewareRecordsHTTPMetrics(t *testing.T) {
	obs := testObserver(t)

	r := chi.NewRouter()
	r.Use(Observability(obs))
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	for i := 0; i < 2; i++ {
		resp, err := http.Get(srv.URL + "/users/42")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		resp.Body.Close()
	}

	scrape := httptest.NewServer(obs.MetricsHandler())
	t.Cleanup(scrape.Close)
	resp, err := http.Get(scrape.URL)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read scrape: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `http_requests_total{method="GET",route="/users/{id}",status_code="201"} 2`) {
		t.Fatalf("expected route-labeled http_requests_total for /users/{id}; scrape:\n%s", s)
	}
}

func TestObservabilityMiddlewareUnmatchedRoute(t *testing.T) {
	obs := testObserver(t)

	r := chi.NewRouter()
	r.Use(Observability(obs))
	r.Get("/known", func(w http.ResponseWriter, req *http.Request) {})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, _ := http.Get(srv.URL + "/does-not-exist")
	if resp != nil {
		resp.Body.Close()
	}

	count := countersFromScrape(t, obs, observability.HTTPRequestsTotal,
		`method="GET"`, `route="unmatched"`, `status_code="404"`)
	if count != 1 {
		t.Errorf("expected 1 unmatched request, got %v", count)
	}
}

func TestObservabilityMiddlewarePanicRecordsAndReraises(t *testing.T) {
	obs := testObserver(t)

	r := chi.NewRouter()
	r.Use(Observability(obs))
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

	total := countersFromScrapeRaw(t, obs, observability.HTTPRequestsTotal,
		`method="GET"`, `route="/boom"`)
	if total < 1 {
		t.Errorf("expected at least one request observation for /boom, got %v", total)
	}
}

func TestObservabilityMiddlewareInFlightDecrements(t *testing.T) {
	obs := testObserver(t)

	r := chi.NewRouter()
	r.Use(Observability(obs))
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	for i := 0; i < 5; i++ {
		resp, _ := http.Get(srv.URL + "/")
		if resp != nil {
			resp.Body.Close()
		}
	}

	val := gaugeFromScrape(t, obs, observability.HTTPRequestsInFlight,
		`method="GET"`, `route=""`)
	if val != 0 {
		t.Errorf("expected in_flight to return to 0, got %v", val)
	}
}

func TestResponseWriterWrapperHonorsInterfaces(t *testing.T) {
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

func countersFromScrape(t *testing.T, obs *observability.Observer, metric string, labelMatchers ...string) float64 {
	t.Helper()
	return gaugeFromScrape(t, obs, metric, labelMatchers...)
}

func countersFromScrapeRaw(t *testing.T, obs *observability.Observer, metric string, labelMatchers ...string) float64 {
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

func gaugeFromScrape(t *testing.T, obs *observability.Observer, metric string, labelMatchers ...string) float64 {
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

func sumMatchingLines(t *testing.T, resp *http.Response, metric string, labelMatchers []string) float64 {
	t.Helper()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var total float64
	prefixed := metric + "{"
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if !strings.HasPrefix(line, prefixed) && !strings.HasPrefix(line, metric+" ") {
			continue
		}
		matched := true
		for _, m := range labelMatchers {
			if !strings.Contains(line, m) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		idx := strings.LastIndex(line, " ")
		if idx < 0 {
			continue
		}
		v, err := strconv.ParseFloat(line[idx+1:], 64)
		if err != nil {
			continue
		}
		total += v
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan scrape: %v", err)
	}
	return total
}
