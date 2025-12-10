package middlewares

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/tink3rlabs/magic/types"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/xeipuuv/gojsonschema"
)

type Validator struct{}

// ValidationResult holds the result of the gojsonschema validator
type ValidationResult struct {
	Result bool     `json:"result"`
	Error  []string `json:"error"`
}

// ValidateJSON validates the JSON data against the provided  JSON schema
// https://pkg.go.dev/github.com/xeipuuv/gojsonschema#section-readme

func JSONSchemaValidator(schema string, data interface{}) (ValidationResult, error) {
	schemaLoader := gojsonschema.NewStringLoader(schema)
	documentLoader := gojsonschema.NewGoLoader(data)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)

	if err != nil {
		slog.Error("gojsonschema validation function failed", slog.Any("error", err))
		return ValidationResult{}, err
	}

	if result.Valid() {
		return ValidationResult{
			Result: true,
			Error:  []string{},
		}, nil
	}

	var errors []string
	for _, desc := range result.Errors() {
		errors = append(errors, desc.String())
	}

	return ValidationResult{
		Result: false,
		Error:  errors,
	}, nil
}

// Define the JSON schemas as a map where the ctx(body, params and query) is the key and schema is the value
// Function will get the data from request and format it in the right way as required by gojsonschema validator

func (f *Validator) ValidateRequest(schemas map[string]string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var allErrors []string

		for target, schema := range schemas {
			var data interface{}

			switch target {
			case "body":
				bodyBytes, err := io.ReadAll(io.TeeReader(r.Body, &bytes.Buffer{}))
				if err != nil {
					allErrors = append(allErrors, fmt.Sprintf("Error reading body: %v", err))
					continue
				}
				// Reset body for next handler
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

				if err := json.Unmarshal(bodyBytes, &data); err != nil {
					allErrors = append(allErrors, fmt.Sprintf("Invalid JSON body: %v", err))
					continue
				}
			case "query":
				queryData := make(map[string]interface{})
				for key, values := range r.URL.Query() {
					if len(values) > 0 {
						queryData[key] = values[0]
					}
				}
				data = queryData
			case "params":
				params := make(map[string]interface{})
				routeCtx := chi.RouteContext(r.Context())
				for i, key := range routeCtx.URLParams.Keys {
					params[key] = routeCtx.URLParams.Values[i]
				}
				data = params
			}

			result, err := JSONSchemaValidator(schema, data)

			// Validate all the given schemas (body, id, and query) and return a combined error message
			// that includes errors found in each schema
			if !result.Result {
				allErrors = append(allErrors, result.Error...)
			}

			// If the gojsonschema validator function has some internal error
			if err != nil {
				serverError := types.ErrorResponse{
					Status: http.StatusText(http.StatusInternalServerError),
					Error:  "encountered an unexpected server error: " + err.Error(),
				}
				render.Status(r, http.StatusInternalServerError)
				render.JSON(w, r, serverError)
				return
			}
		}

		if len(allErrors) > 0 {
			validationError := types.ErrorResponse{
				Status:  http.StatusText(http.StatusBadRequest),
				Error:   "request validation failed",
				Details: allErrors,
			}
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, validationError)
			return
		}
		next.ServeHTTP(w, r)
	}
}
