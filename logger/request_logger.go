package logger

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// RequestLoggerOptions configures ChiRequestLogger behavior.
type RequestLoggerOptions struct {
	// SkipPaths suppresses logs for exact URL.Path matches.
	SkipPaths []string
	// SkipPathPrefixes suppresses logs when URL.Path has one of these prefixes.
	SkipPathPrefixes []string
	// Message overrides the request log message key.
	// Defaults to "http_request".
	Message string
}

// ChiRequestLogger returns a chi middleware that logs requests through slog,
// so log format (JSON/text) matches logger.Init configuration.
func ChiRequestLogger(opts RequestLoggerOptions) func(http.Handler) http.Handler {
	return middleware.RequestLogger(newChiSlogFormatter(opts))
}

type chiSlogFormatter struct {
	skipPaths map[string]struct{}
	prefixes  []string
	message   string
}

func newChiSlogFormatter(opts RequestLoggerOptions) *chiSlogFormatter {
	skip := make(map[string]struct{}, len(opts.SkipPaths))
	for _, p := range opts.SkipPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		skip[p] = struct{}{}
	}
	prefixes := make([]string, 0, len(opts.SkipPathPrefixes))
	for _, p := range opts.SkipPathPrefixes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		prefixes = append(prefixes, p)
	}
	msg := strings.TrimSpace(opts.Message)
	if msg == "" {
		msg = "http_request"
	}
	return &chiSlogFormatter{
		skipPaths: skip,
		prefixes:  prefixes,
		message:   msg,
	}
}

func (f *chiSlogFormatter) NewLogEntry(r *http.Request) middleware.LogEntry {
	path := r.URL.Path
	if _, ok := f.skipPaths[path]; ok {
		return noopLogEntry{}
	}
	for _, p := range f.prefixes {
		if strings.HasPrefix(path, p) {
			return noopLogEntry{}
		}
	}
	return &chiSlogEntry{
		req:     r,
		message: f.message,
	}
}

type noopLogEntry struct{}

func (noopLogEntry) Write(int, int, http.Header, time.Duration, any) {}
func (noopLogEntry) Panic(any, []byte)                               {}

type chiSlogEntry struct {
	req     *http.Request
	message string
}

func (e *chiSlogEntry) Write(status, bytes int, _ http.Header, elapsed time.Duration, _ any) {
	slog.InfoContext(e.req.Context(), e.message,
		slog.String("method", e.req.Method),
		slog.String("path", e.req.URL.Path),
		slog.String("query", e.req.URL.RawQuery),
		slog.String("remote_addr", e.req.RemoteAddr),
		slog.String("user_agent", e.req.UserAgent()),
		slog.Int("status", status),
		slog.Int("bytes", bytes),
		slog.Duration("duration", elapsed),
	)
}

func (e *chiSlogEntry) Panic(v any, stack []byte) {
	slog.ErrorContext(e.req.Context(), "http_panic",
		slog.Any("panic", v),
		slog.String("stack", string(stack)),
		slog.String("method", e.req.Method),
		slog.String("path", e.req.URL.Path),
	)
}
