package observability

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

	"github.com/tink3rlabs/magic/telemetry"
)

// ChiMiddleware returns an HTTP middleware that:
//
//  1. Extracts a W3C trace context from incoming headers and
//     starts a server span for the request.
//  2. Wraps the http.ResponseWriter so the terminating handler's
//     status code and response size can be observed.
//  3. Records built-in HTTP metrics after the downstream handler
//     returns, using the chi route pattern (not the raw URL path)
//     to keep cardinality bounded.
//
// The middleware must be installed on the chi router via
// router.Use(obs.ChiMiddleware()) so that chi has already parsed
// the URL and attached a RouteContext to the request by the time
// the deferred recording block runs.
//
// Panic policy: the middleware records the panic on the span,
// marks the span as errored, ends it, and re-raises the panic.
// Application-level recovery middleware is the appropriate place
// to decide whether to respond to or propagate the panic.
func (o *Observer) ChiMiddleware() func(http.Handler) http.Handler {
	if o == nil || o.telem == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	tracer := o.telem.Tracer
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			method := normalizeMethod(r.Method)

			propagator := otel.GetTextMapPropagator()
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			ctx, span := tracer.Start(ctx, "HTTP "+method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(method),
					semconv.URLPath(r.URL.Path),
					semconv.ServerAddress(r.Host),
				),
			)

			ww := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}
			rCtx := r.WithContext(ctx)

			methodLabel := telemetry.Label{Key: LabelHTTPMethod, Value: method}
			preRouteLabel := telemetry.Label{Key: LabelHTTPRoute, Value: ""}

			// Increment in-flight before serving using an empty
			// route label; chi has not yet resolved the pattern.
			// This counter decrement pairs with the same labels
			// in the deferred block so we never leave stale
			// series incrementing unbalanced.
			o.httpRequestsInFlight.Add(1, methodLabel, preRouteLabel)

			defer func() {
				o.httpRequestsInFlight.Add(-1, methodLabel, preRouteLabel)

				route := routePatternFromReq(rCtx)
				routeLabel := telemetry.Label{Key: LabelHTTPRoute, Value: route}
				status := ww.statusCode
				statusLabel := telemetry.Label{Key: LabelHTTPStatusCode, Value: strconv.Itoa(status)}

				duration := time.Since(start).Seconds()
				o.httpRequestsTotal.Add(1, methodLabel, routeLabel, statusLabel)
				o.httpRequestDuration.Observe(duration, methodLabel, routeLabel, statusLabel)
				o.httpRequestSize.Observe(requestSize(r), methodLabel, routeLabel)
				o.httpResponseSize.Observe(float64(ww.bytesWritten), methodLabel, routeLabel, statusLabel)

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

// normalizeMethod returns the uppercase HTTP method or folds
// unrecognized methods to "_OTHER" so custom verbs do not blow up
// the cardinality of the method label.
func normalizeMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions,
		http.MethodTrace:
		return m
	}
	return "_OTHER"
}

// routePatternFromReq returns the chi route pattern, or
// "unmatched" when the request did not hit any registered route
// (typically a 404 from chi.NotFound or a method not allowed
// response).
func routePatternFromReq(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}

// requestSize returns the Content-Length of r as a float64, or 0
// when the header is missing or malformed. The middleware never
// buffers the body — streaming handlers would be broken otherwise.
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

// panicErr converts an arbitrary panic value to an error so it
// can be recorded on a span via trace.Span.RecordError.
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

// ----- responseWriterWrapper -----

// responseWriterWrapper captures the status code and bytes written
// by the downstream handler while forwarding optional interfaces
// (Flusher, Hijacker, Pusher) so streaming, SSE, and websocket
// handlers keep working.
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
	return nil, nil, errors.New("observability: underlying ResponseWriter does not support Hijack")
}

func (w *responseWriterWrapper) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Unwrap exposes the wrapped ResponseWriter so Go's
// http.ResponseController can reach through to the underlying
// connection (useful for ReadDeadline, SetWriteDeadline, etc.).
func (w *responseWriterWrapper) Unwrap() http.ResponseWriter { return w.ResponseWriter }
