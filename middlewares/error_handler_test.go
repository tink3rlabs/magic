package middlewares

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	serviceErrors "github.com/tink3rlabs/magic/errors"
	"github.com/tink3rlabs/magic/storage"
)

func TestErrorHandler_Wrap(t *testing.T) {
	tests := []struct {
		name           string
		e              *ErrorHandler
		handler        func(w http.ResponseWriter, r *http.Request) error
		wantStatusCode int
		wantBody       string
	}{
		{
			name: "handles NotFound error",
			e:    &ErrorHandler{},
			handler: func(w http.ResponseWriter, r *http.Request) error {
				return &serviceErrors.NotFound{Message: "resource not found"}
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "handles storage.ErrNotFound",
			e:    &ErrorHandler{},
			handler: func(w http.ResponseWriter, r *http.Request) error {
				return storage.ErrNotFound
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "handles BadRequest error",
			e:    &ErrorHandler{},
			handler: func(w http.ResponseWriter, r *http.Request) error {
				return &serviceErrors.BadRequest{Message: "invalid request"}
			},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "handles ServiceUnavailable error",
			e:    &ErrorHandler{},
			handler: func(w http.ResponseWriter, r *http.Request) error {
				return &serviceErrors.ServiceUnavailable{Message: "service unavailable"}
			},
			wantStatusCode: http.StatusServiceUnavailable,
		},
		{
			name: "handles Forbidden error",
			e:    &ErrorHandler{},
			handler: func(w http.ResponseWriter, r *http.Request) error {
				return &serviceErrors.Forbidden{Message: "access forbidden"}
			},
			wantStatusCode: http.StatusForbidden,
		},
		{
			name: "handles Unauthorized error",
			e:    &ErrorHandler{},
			handler: func(w http.ResponseWriter, r *http.Request) error {
				return &serviceErrors.Unauthorized{Message: "unauthorized"}
			},
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name: "handles generic error",
			e:    &ErrorHandler{},
			handler: func(w http.ResponseWriter, r *http.Request) error {
				return errors.New("generic error")
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "no error passes through",
			e:    &ErrorHandler{},
			handler: func(w http.ResponseWriter, r *http.Request) error {
				w.WriteHeader(http.StatusOK)
				return nil
			},
			wantStatusCode: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.e.Wrap(tt.handler)
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			got.ServeHTTP(w, req)
			if w.Code != tt.wantStatusCode {
				t.Errorf("ErrorHandler.Wrap() status code = %v, want %v", w.Code, tt.wantStatusCode)
			}
		})
	}
}
