package middlewares

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/tink3rlabs/magic/observability"
	"github.com/tink3rlabs/magic/telemetry"
)

// Observability returns the HTTP middleware that emits the
// built-in tracing and metrics signals from an initialized
// observability.Observer.
//
// Passing nil is allowed and returns an identity middleware, so
// callers can wire the stack conditionally without extra guards.
func Observability(obs *observability.Observer) func(http.Handler) http.Handler {
	state := obs.HTTPMiddlewareState()
	if state == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	propagator := otel.GetTextMapPropagator()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			method := normalizeMethod(r.Method)

			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			ctx, span := state.Tracer.Start(ctx, "HTTP "+method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(method),
					semconv.URLPath(r.URL.Path),
					semconv.ServerAddress(r.Host),
				),
			)

			ww := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}
			rCtx := r.WithContext(ctx)

			methodLabel := telemetry.Label{Key: observability.LabelHTTPMethod, Value: method}
			preRouteLabel := telemetry.Label{Key: observability.LabelHTTPRoute, Value: ""}
			state.RequestsInFlight.Add(1, methodLabel, preRouteLabel)

			defer func() {
				state.RequestsInFlight.Add(-1, methodLabel, preRouteLabel)

				route := routePatternFromReq(rCtx)
				routeLabel := telemetry.Label{Key: observability.LabelHTTPRoute, Value: route}
				status := ww.statusCode
				statusLabel := telemetry.Label{Key: observability.LabelHTTPStatusCode, Value: strconv.Itoa(status)}

				duration := time.Since(start).Seconds()
				state.RequestsTotal.Add(1, methodLabel, routeLabel, statusLabel)
				state.RequestDuration.Observe(duration, methodLabel, routeLabel, statusLabel)
				state.RequestSize.Observe(requestSize(r), methodLabel, routeLabel)
				state.ResponseSize.Observe(float64(ww.bytesWritten), methodLabel, routeLabel, statusLabel)

				span.SetAttributes(
					attribute.String("http.route", route),
					semconv.HTTPResponseStatusCodeKey.Int(status),
				)
				if status >= 500 {
					span.SetStatus(codes.Error, http.StatusText(status))
				}

				if rec := recover(); rec != nil {
					span.RecordError(panicErr(rec))
					span.SetStatus(codes.Error, "panic")
					span.End()
					panic(rec)
				}
				span.End()
			}()

			next.ServeHTTP(ww, rCtx)
		})
	}
}

func normalizeMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions,
		http.MethodTrace:
		return m
	}
	return "_OTHER"
}

func routePatternFromReq(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}

func requestSize(r *http.Request) float64 {
	if r.ContentLength > 0 {
		return float64(r.ContentLength)
	}
	if cl := r.Header.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseFloat(cl, 64); err == nil && n >= 0 {
			return n
		}
	}
	return 0
}

func panicErr(v any) error {
	switch x := v.(type) {
	case error:
		return x
	case string:
		return errors.New(x)
	default:
		return errors.New("panic")
	}
}

type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode    int
	bytesWritten  int64
	headerWritten bool
}

func (w *responseWriterWrapper) WriteHeader(code int) {
	if w.headerWritten {
		return
	}
	w.statusCode = code
	w.headerWritten = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	if !w.headerWritten {
		w.headerWritten = true
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += int64(n)
	return n, err
}

func (w *responseWriterWrapper) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *responseWriterWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("middlewares: underlying ResponseWriter does not support Hijack")
}

func (w *responseWriterWrapper) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (w *responseWriterWrapper) Unwrap() http.ResponseWriter { return w.ResponseWriter }
