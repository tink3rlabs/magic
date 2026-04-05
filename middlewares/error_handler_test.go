package middlewares

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	serviceErrors "github.com/tink3rlabs/magic/errors"
	"github.com/tink3rlabs/magic/storage"
	"github.com/tink3rlabs/magic/types"
)

func TestErrorHandlerWrap_MapsKnownErrors(t *testing.T) {
	h := &ErrorHandler{}

	tests := []struct {
		name         string
		err          error
		wantStatus   int
		wantStatusTx string
		wantError    string
	}{
		{name: "not found", err: &serviceErrors.NotFound{Message: "missing"}, wantStatus: http.StatusNotFound, wantStatusTx: http.StatusText(http.StatusNotFound), wantError: "missing"},
		{name: "storage not found", err: storage.ErrNotFound, wantStatus: http.StatusNotFound, wantStatusTx: http.StatusText(http.StatusNotFound), wantError: storage.ErrNotFound.Error()},
		{name: "wrapped storage not found", err: fmt.Errorf("db: %w", storage.ErrNotFound), wantStatus: http.StatusNotFound, wantStatusTx: http.StatusText(http.StatusNotFound), wantError: "db: " + storage.ErrNotFound.Error()},
		{name: "bad request", err: &serviceErrors.BadRequest{Message: "invalid body"}, wantStatus: http.StatusBadRequest, wantStatusTx: http.StatusText(http.StatusBadRequest), wantError: "invalid body"},
		{name: "wrapped bad request", err: fmt.Errorf("validation: %w", &serviceErrors.BadRequest{Message: "invalid body"}), wantStatus: http.StatusBadRequest, wantStatusTx: http.StatusText(http.StatusBadRequest), wantError: "validation: invalid body"},
		{name: "service unavailable", err: &serviceErrors.ServiceUnavailable{Message: "down"}, wantStatus: http.StatusServiceUnavailable, wantStatusTx: http.StatusText(http.StatusServiceUnavailable), wantError: "down"},
		{name: "forbidden", err: &serviceErrors.Forbidden{Message: "denied"}, wantStatus: http.StatusForbidden, wantStatusTx: http.StatusText(http.StatusForbidden), wantError: "denied"},
		{name: "unauthorized", err: &serviceErrors.Unauthorized{Message: "no token"}, wantStatus: http.StatusUnauthorized, wantStatusTx: http.StatusText(http.StatusUnauthorized), wantError: "no token"},
		{name: "method not allowed", err: &serviceErrors.MethodNotAllowed{Message: "bad method"}, wantStatus: http.StatusMethodNotAllowed, wantStatusTx: http.StatusText(http.StatusMethodNotAllowed), wantError: "bad method"},
		{name: "conflict", err: &serviceErrors.Conflict{Message: "duplicate"}, wantStatus: http.StatusConflict, wantStatusTx: http.StatusText(http.StatusConflict), wantError: "duplicate"},
		{name: "gone", err: &serviceErrors.Gone{Message: "gone"}, wantStatus: http.StatusGone, wantStatusTx: http.StatusText(http.StatusGone), wantError: "gone"},
		{name: "unsupported media type", err: &serviceErrors.UnsupportedMediaType{Message: "unsupported"}, wantStatus: http.StatusUnsupportedMediaType, wantStatusTx: http.StatusText(http.StatusUnsupportedMediaType), wantError: "unsupported"},
		{name: "unprocessable entity", err: &serviceErrors.UnprocessableEntity{Message: "unprocessable"}, wantStatus: http.StatusUnprocessableEntity, wantStatusTx: http.StatusText(http.StatusUnprocessableEntity), wantError: "unprocessable"},
		{name: "too many requests", err: &serviceErrors.TooManyRequests{Message: "rate limited"}, wantStatus: http.StatusTooManyRequests, wantStatusTx: http.StatusText(http.StatusTooManyRequests), wantError: "rate limited"},
		{name: "internal server error", err: &serviceErrors.InternalServerError{Message: "boom"}, wantStatus: http.StatusInternalServerError, wantStatusTx: http.StatusText(http.StatusInternalServerError), wantError: "boom"},
		{name: "bad gateway", err: &serviceErrors.BadGateway{Message: "upstream"}, wantStatus: http.StatusBadGateway, wantStatusTx: http.StatusText(http.StatusBadGateway), wantError: "upstream"},
		{name: "gateway timeout", err: &serviceErrors.GatewayTimeout{Message: "timeout"}, wantStatus: http.StatusGatewayTimeout, wantStatusTx: http.StatusText(http.StatusGatewayTimeout), wantError: "timeout"},
		{name: "request timeout", err: &serviceErrors.RequestTimeout{Message: "request timed out"}, wantStatus: http.StatusRequestTimeout, wantStatusTx: http.StatusText(http.StatusRequestTimeout), wantError: "request timed out"},
		{name: "not implemented", err: &serviceErrors.NotImplemented{Message: "todo"}, wantStatus: http.StatusNotImplemented, wantStatusTx: http.StatusText(http.StatusNotImplemented), wantError: "todo"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)

			h.Wrap(func(w http.ResponseWriter, r *http.Request) error {
				return tc.err
			})(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status code mismatch: got %d, want %d", rec.Code, tc.wantStatus)
			}

			var got types.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if got.Status != tc.wantStatusTx {
				t.Fatalf("status text mismatch: got %q, want %q", got.Status, tc.wantStatusTx)
			}

			if got.Error != tc.wantError {
				t.Fatalf("error message mismatch: got %q, want %q", got.Error, tc.wantError)
			}
		})
	}
}

func TestErrorHandlerWrap_UnknownErrorFallsBackTo500(t *testing.T) {
	h := &ErrorHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	h.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		return errors.New("random failure")
	})(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status code mismatch: got %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var got types.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	wantMessage := "encountered an unexpected server error: random failure"
	if got.Status != http.StatusText(http.StatusInternalServerError) {
		t.Fatalf("status text mismatch: got %q, want %q", got.Status, http.StatusText(http.StatusInternalServerError))
	}

	if got.Error != wantMessage {
		t.Fatalf("error message mismatch: got %q, want %q", got.Error, wantMessage)
	}
}

func TestErrorHandlerWrap_NoErrorWritesNothing(t *testing.T) {
	h := &ErrorHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	h.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		return nil
	})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code mismatch: got %d, want %d", rec.Code, http.StatusOK)
	}

	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", rec.Body.String())
	}
}
