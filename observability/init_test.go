package observability

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/tink3rlabs/magic/telemetry"
)

// initTestObserver produces an Observer in Prometheus mode so no
// external OTLP endpoint is required. Caller is responsible for
// Shutdown.
func initTestObserver(t *testing.T) *Observer {
	t.Helper()
	cfg := DefaultConfig()
	cfg.ServiceName = "obs-test"
	cfg.MetricsMode = MetricsModePrometheus
	// Tracing uses a noop TracerProvider (no endpoint).
	cfg.EnableTracing = false

	obs, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		_ = obs.Shutdown(context.Background())
	})
	return obs
}

func TestInitSetsGlobalTelemetry(t *testing.T) {
	obs := initTestObserver(t)
	if telemetry.Global() != obs.telem {
		t.Error("telemetry.Global() must return the installed Telemetry after Init")
	}
}

func TestInitRegistersBuiltinHTTPMetrics(t *testing.T) {
	obs := initTestObserver(t)

	// Prometheus omits HELP lines for series with zero samples,
	// so fire one request through the middleware first. That
	// exercises every built-in HTTP instrument.
	r := chi.NewRouter()
	r.Use(obs.ChiMiddleware())
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	scrape := httptest.NewServer(obs.MetricsHandler())
	t.Cleanup(scrape.Close)

	sResp, err := http.Get(scrape.URL)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer sResp.Body.Close()
	body, _ := io.ReadAll(sResp.Body)

	for _, want := range []string{
		HTTPRequestsTotal,
		HTTPRequestDurationSeconds,
		HTTPRequestSizeBytes,
		HTTPResponseSizeBytes,
		HTTPRequestsInFlight,
	} {
		if !strings.Contains(string(body), "# HELP "+want) {
			t.Errorf("expected HELP for %q in scrape output", want)
		}
	}
}

func TestInitPrometheusRegistersRuntimeMetrics(t *testing.T) {
	obs := initTestObserver(t)

	scrape := httptest.NewServer(obs.MetricsHandler())
	t.Cleanup(scrape.Close)

	resp, err := http.Get(scrape.URL)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// At least one go_ runtime metric and one process_ metric
	// should be present when the collectors are registered.
	if !strings.Contains(string(body), "go_goroutines") {
		t.Error("expected go_goroutines in runtime metrics output")
	}
	if !strings.Contains(string(body), "process_") {
		t.Error("expected process_* metric in output")
	}
}

func TestInitChiMiddlewareRecordsRequest(t *testing.T) {
	obs := initTestObserver(t)

	r := chi.NewRouter()
	r.Use(obs.ChiMiddleware())
	r.Get("/hello/{name}", func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(w, "hi")
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	httpResp, err := http.Get(srv.URL + "/hello/alice")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	httpResp.Body.Close()

	scrape := httptest.NewServer(obs.MetricsHandler())
	t.Cleanup(scrape.Close)

	mResp, err := http.Get(scrape.URL)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer mResp.Body.Close()
	mb, _ := io.ReadAll(mResp.Body)
	body := string(mb)

	if !strings.Contains(body, `http_requests_total{method="GET",route="/hello/{name}",status_code="200"} 1`) {
		t.Errorf("expected 1 GET /hello/{name} 200 request, got:\n%s", body)
	}
}

func TestInitOTLPMetricsHandlerReturns404(t *testing.T) {
	// Requesting OTLP mode without an endpoint fails Init; skip
	// the exporter and instead install a throwaway Observer by
	// hand to validate the 404 handler contract.
	obs := &Observer{cfg: Config{MetricsMode: MetricsModeOTLP}}
	w := httptest.NewRecorder()
	obs.MetricsHandler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("OTLP mode /metrics must return 404, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type must be JSON, got %q", ct)
	}
}

func TestInitShutdownIsIdempotent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ServiceName = "svc"
	cfg.MetricsMode = MetricsModePrometheus
	obs, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := obs.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := obs.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown must be no-op: %v", err)
	}
}

func TestInitShutdownResetsGlobal(t *testing.T) {
	obs := initTestObserver(t)
	before := telemetry.Global()
	if before != obs.telem {
		t.Fatal("global not installed by Init")
	}
	_ = obs.Shutdown(context.Background())
	if telemetry.Global() == obs.telem {
		t.Error("Shutdown should reset the global")
	}
}
