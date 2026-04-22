package logger

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChiRequestLoggerWritesJSONWithGlobalLogger(t *testing.T) {
	withRestoredDefaultLogger(t)

	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))

	r := http.NewServeMux()
	r.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	})

	h := ChiRequestLogger(RequestLoggerOptions{})(r)
	req := httptest.NewRequest(http.MethodGet, "/ok?a=b", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	out := buf.String()
	if !strings.Contains(out, `"msg":"http_request"`) {
		t.Fatalf("expected request log message, got %q", out)
	}
	if !strings.Contains(out, `"path":"/ok"`) {
		t.Fatalf("expected path in log, got %q", out)
	}
	if !strings.Contains(out, `"status":201`) {
		t.Fatalf("expected status in log, got %q", out)
	}
}

func TestChiRequestLoggerSkipsExactPath(t *testing.T) {
	withRestoredDefaultLogger(t)

	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))

	r := http.NewServeMux()
	r.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := ChiRequestLogger(RequestLoggerOptions{
		SkipPaths: []string{"/metrics"},
	})(r)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if out := strings.TrimSpace(buf.String()); out != "" {
		t.Fatalf("expected no request log, got %q", out)
	}
}

func TestChiRequestLoggerSkipsPathPrefix(t *testing.T) {
	withRestoredDefaultLogger(t)

	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))

	r := http.NewServeMux()
	r.HandleFunc("/health/readiness", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := ChiRequestLogger(RequestLoggerOptions{
		SkipPathPrefixes: []string{"/health"},
	})(r)
	req := httptest.NewRequest(http.MethodGet, "/health/readiness", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if out := strings.TrimSpace(buf.String()); out != "" {
		t.Fatalf("expected no request log, got %q", out)
	}
}
