package middlewares

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/tink3rlabs/magic/observability"
)

func benchObserver(b *testing.B, tracing bool) *observability.Observer {
	b.Helper()
	cfg := observability.DefaultConfig()
	cfg.ServiceName = "bench"
	cfg.MetricsMode = observability.MetricsModePrometheus
	cfg.EnableTracing = tracing
	if tracing {
		cfg.TracesOTLPEndpoint = "localhost:4317"
		cfg.TracesOTLPInsecure = true
	}

	obs, err := observability.Init(context.Background(), cfg)
	if err != nil {
		b.Fatalf("Init: %v", err)
	}
	b.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = obs.Shutdown(ctx)
	})

	if tracing {
		if _, ok := obs.TracerProvider().(*sdktrace.TracerProvider); !ok {
			b.Fatalf("expected SDK tracer provider with tracing enabled; got %T", obs.TracerProvider())
		}
	}
	return obs
}

func benchRouter(obs *observability.Observer) http.Handler {
	r := chi.NewRouter()
	r.Use(Observability(obs))
	r.Get("/bench", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	return r
}

func benchBaseRouter() http.Handler {
	r := chi.NewRouter()
	r.Get("/bench", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	return r
}

func BenchmarkChiRouterBaseline(b *testing.B) {
	handler := benchBaseRouter()
	req := httptest.NewRequest(http.MethodGet, "/bench", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

func BenchmarkChiMiddlewareNoTracing(b *testing.B) {
	obs := benchObserver(b, false)
	handler := benchRouter(obs)
	req := httptest.NewRequest(http.MethodGet, "/bench", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

func BenchmarkChiMiddlewareWithTracing(b *testing.B) {
	obs := benchObserver(b, true)
	handler := benchRouter(obs)
	req := httptest.NewRequest(http.MethodGet, "/bench", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

func BenchmarkChiMiddlewareParallel(b *testing.B) {
	obs := benchObserver(b, false)
	handler := benchRouter(obs)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		req := httptest.NewRequest(http.MethodGet, "/bench", nil)
		for pb.Next() {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})
}
