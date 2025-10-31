package middlewares

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestJSONSchemaValidator(t *testing.T) {
	type args struct {
		schema string
		data   interface{}
	}
	tests := []struct {
		name    string
		args    args
		want    ValidationResult
		wantErr bool
	}{
		{
			name: "valid data passes validation",
			args: args{
				schema: `{
					"type": "object",
					"properties": {
						"name": {"type": "string"}
					},
					"required": ["name"]
				}`,
				data: map[string]interface{}{
					"name": "test",
				},
			},
			want: ValidationResult{
				Result: true,
				Error:  []string{},
			},
			wantErr: false,
		},
		{
			name: "invalid data fails validation",
			args: args{
				schema: `{
					"type": "object",
					"properties": {
						"name": {"type": "string"}
					},
					"required": ["name"]
				}`,
				data: map[string]interface{}{},
			},
			want: ValidationResult{
				Result: false,
				Error:  []string{}, // Will be populated by validator
			},
			wantErr: false,
		},
		{
			name: "invalid schema returns error",
			args: args{
				schema: `{invalid json}`,
				data:   map[string]interface{}{},
			},
			wantErr: true,
		},
		{
			name: "valid number passes validation",
			args: args{
				schema: `{
					"type": "object",
					"properties": {
						"age": {"type": "number"}
					}
				}`,
				data: map[string]interface{}{
					"age": 25,
				},
			},
			want: ValidationResult{
				Result: true,
				Error:  []string{},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := JSONSchemaValidator(tt.args.schema, tt.args.data)
			if (err != nil) != tt.wantErr {
				t.Fatalf("JSONSchemaValidator() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Result != tt.want.Result {
				t.Errorf("JSONSchemaValidator() Result = %v, want %v", got.Result, tt.want.Result)
			}
			if !got.Result && len(got.Error) == 0 {
				t.Error("JSONSchemaValidator() should return errors when Result is false")
			}
		})
	}
}

func TestValidator_ValidateRequest(t *testing.T) {
	tests := []struct {
		name           string
		f              *Validator
		schemas        map[string]string
		next           http.HandlerFunc
		requestSetup   func(*http.Request)
		wantStatusCode int
	}{
		{
			name: "valid body passes through",
			f:    &Validator{},
			schemas: map[string]string{
				"body": `{
					"type": "object",
					"properties": {
						"name": {"type": "string"}
					},
					"required": ["name"]
				}`,
			},
			next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			requestSetup: func(r *http.Request) {
				r.Body = io.NopCloser(strings.NewReader(`{"name": "test"}`))
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name: "invalid body returns bad request",
			f:    &Validator{},
			schemas: map[string]string{
				"body": `{
					"type": "object",
					"properties": {
						"name": {"type": "string"}
					},
					"required": ["name"]
				}`,
			},
			next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("next handler should not be called")
			}),
			requestSetup: func(r *http.Request) {
				r.Body = io.NopCloser(strings.NewReader(`{}`))
			},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "valid query passes through",
			f:    &Validator{},
			schemas: map[string]string{
				"query": `{
					"type": "object",
					"properties": {
						"limit": {"type": "string"}
					}
				}`,
			},
			next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			requestSetup: func(r *http.Request) {
				// Query params would be set via URL
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name: "valid params passes through",
			f:    &Validator{},
			schemas: map[string]string{
				"params": `{
					"type": "object",
					"properties": {
						"id": {"type": "string"}
					}
				}`,
			},
			next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			requestSetup: func(r *http.Request) {
				rctx := chi.NewRouteContext()
				rctx.URLParams.Add("id", "123")
				ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
				*r = *r.WithContext(ctx)
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name: "invalid schema returns internal server error",
			f:    &Validator{},
			schemas: map[string]string{
				"body": `{invalid json}`,
			},
			next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("next handler should not be called")
			}),
			requestSetup: func(r *http.Request) {
				r.Body = io.NopCloser(strings.NewReader(`{"name": "test"}`))
			},
			wantStatusCode: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.f.ValidateRequest(tt.schemas, tt.next)
			req := httptest.NewRequest("GET", "/", nil)
			if tt.requestSetup != nil {
				tt.requestSetup(req)
			}
			w := httptest.NewRecorder()
			got.ServeHTTP(w, req)
			if w.Code != tt.wantStatusCode {
				t.Errorf("Validator.ValidateRequest() status code = %v, want %v", w.Code, tt.wantStatusCode)
			}
		})
	}
}
