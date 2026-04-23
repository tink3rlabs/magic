package middlewares

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
	"testing"
)

type fakeRW struct {
	header     http.Header
	body       bytes.Buffer
	status     int
	flushed    bool
	hijacked   bool
	pushed     string
	supportsFH bool
}

func newFakeRW() *fakeRW { return &fakeRW{header: http.Header{}, supportsFH: true} }

func (f *fakeRW) Header() http.Header         { return f.header }
func (f *fakeRW) Write(b []byte) (int, error) { return f.body.Write(b) }
func (f *fakeRW) WriteHeader(code int)        { f.status = code }

func (f *fakeRW) Flush() {
	if !f.supportsFH {
		return
	}
	f.flushed = true
}
func (f *fakeRW) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !f.supportsFH {
		return nil, nil, errors.New("not supported")
	}
	f.hijacked = true
	return nil, nil, nil
}
func (f *fakeRW) Push(target string, _ *http.PushOptions) error {
	if !f.supportsFH {
		return http.ErrNotSupported
	}
	f.pushed = target
	return nil
}

type bareRW struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (b *bareRW) Header() http.Header         { return b.header }
func (b *bareRW) Write(p []byte) (int, error) { return b.body.Write(p) }
func (b *bareRW) WriteHeader(code int)        { b.status = code }

func TestResponseWriterWrapperWriteHeaderOnce(t *testing.T) {
	inner := newFakeRW()
	w := &responseWriterWrapper{ResponseWriter: inner, statusCode: http.StatusOK}

	w.WriteHeader(http.StatusCreated)
	w.WriteHeader(http.StatusTeapot)

	if w.statusCode != http.StatusCreated {
		t.Errorf("statusCode = %d, want %d", w.statusCode, http.StatusCreated)
	}
	if inner.status != http.StatusCreated {
		t.Errorf("inner.status = %d, want %d (second WriteHeader should be ignored)", inner.status, http.StatusCreated)
	}
}

func TestResponseWriterWrapperWriteImplicitHeader(t *testing.T) {
	inner := newFakeRW()
	w := &responseWriterWrapper{ResponseWriter: inner, statusCode: http.StatusOK}

	n, err := w.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Errorf("Write returned (%d, %v), want (5, nil)", n, err)
	}
	if w.bytesWritten != 5 {
		t.Errorf("bytesWritten = %d, want 5", w.bytesWritten)
	}
	if !w.headerWritten {
		t.Error("Write must mark the header as written")
	}
}

func TestResponseWriterWrapperFlushForwards(t *testing.T) {
	inner := newFakeRW()
	w := &responseWriterWrapper{ResponseWriter: inner}
	w.Flush()
	if !inner.flushed {
		t.Error("Flush was not forwarded")
	}
}

func TestResponseWriterWrapperFlushNoopOnUnsupported(t *testing.T) {
	inner := &bareRW{header: http.Header{}}
	w := &responseWriterWrapper{ResponseWriter: inner}
	w.Flush()
}

func TestResponseWriterWrapperHijackForwards(t *testing.T) {
	inner := newFakeRW()
	w := &responseWriterWrapper{ResponseWriter: inner}
	_, _, err := w.Hijack()
	if err != nil {
		t.Errorf("Hijack err = %v, want nil", err)
	}
	if !inner.hijacked {
		t.Error("Hijack was not forwarded")
	}
}

func TestResponseWriterWrapperHijackErrorsWhenUnsupported(t *testing.T) {
	inner := &bareRW{header: http.Header{}}
	w := &responseWriterWrapper{ResponseWriter: inner}
	_, _, err := w.Hijack()
	if err == nil {
		t.Error("Hijack must return an error when the underlying writer does not support it")
	}
}

func TestResponseWriterWrapperPushForwards(t *testing.T) {
	inner := newFakeRW()
	w := &responseWriterWrapper{ResponseWriter: inner}
	if err := w.Push("/style.css", nil); err != nil {
		t.Errorf("Push err = %v, want nil", err)
	}
	if inner.pushed != "/style.css" {
		t.Errorf("pushed = %q, want /style.css", inner.pushed)
	}
}

func TestResponseWriterWrapperPushNotSupported(t *testing.T) {
	inner := &bareRW{header: http.Header{}}
	w := &responseWriterWrapper{ResponseWriter: inner}
	if err := w.Push("/style.css", nil); err != http.ErrNotSupported {
		t.Errorf("Push err = %v, want ErrNotSupported", err)
	}
}

func TestResponseWriterWrapperUnwrapReturnsInner(t *testing.T) {
	inner := newFakeRW()
	w := &responseWriterWrapper{ResponseWriter: inner}
	if w.Unwrap() != http.ResponseWriter(inner) {
		t.Error("Unwrap must return the embedded ResponseWriter")
	}
}

func TestPanicErrMapsAllCases(t *testing.T) {
	if got := panicErr(errors.New("boom")); got == nil || got.Error() != "boom" {
		t.Errorf("error: got %v, want 'boom'", got)
	}
	if got := panicErr("oops"); got == nil || got.Error() != "oops" {
		t.Errorf("string: got %v, want 'oops'", got)
	}
	if got := panicErr(42); got == nil || got.Error() != "panic" {
		t.Errorf("int: got %v, want generic 'panic'", got)
	}
	if got := panicErr(nil); got == nil || got.Error() != "panic" {
		t.Errorf("nil: got %v, want generic 'panic'", got)
	}
}

func TestNormalizeMethodCustomVerb(t *testing.T) {
	if got := normalizeMethod("FOO"); got != "_OTHER" {
		t.Errorf("custom verb should fold to _OTHER, got %q", got)
	}
	for _, m := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect,
		http.MethodOptions, http.MethodTrace,
	} {
		if got := normalizeMethod(m); got != m {
			t.Errorf("standard method %q should pass through, got %q", m, got)
		}
	}
}

func TestRequestSizeFromContentLength(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("abcdef")))
	if got := requestSize(r); got != 6 {
		t.Errorf("requestSize = %v, want 6", got)
	}
}

func TestRequestSizeFromHeaderFallback(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.ContentLength = 0
	r.Header.Set("Content-Length", "128")
	if got := requestSize(r); got != 128 {
		t.Errorf("requestSize from header = %v, want 128", got)
	}
}

func TestRequestSizeMissingReturnsZero(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.ContentLength = 0
	if got := requestSize(r); got != 0 {
		t.Errorf("requestSize = %v, want 0", got)
	}
}

func TestRequestSizeBadHeader(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.ContentLength = 0
	r.Header.Set("Content-Length", "not-a-number")
	if got := requestSize(r); got != 0 {
		t.Errorf("malformed CL should yield 0, got %v", got)
	}
}
