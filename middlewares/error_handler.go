package middlewares

import (
	"errors"
	"net/http"

	"github.com/go-chi/render"

	serviceErrors "github.com/tink3rlabs/magic/errors"
	"github.com/tink3rlabs/magic/storage"
	"github.com/tink3rlabs/magic/types"
)

type ErrorHandler struct{}

func (e *ErrorHandler) Wrap(handler func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var notFoundError *serviceErrors.NotFound
		var badRequestError *serviceErrors.BadRequest
		var serviceUnavailable *serviceErrors.ServiceUnavailable
		var forbiddenError *serviceErrors.Forbidden
		var unauthorizedError *serviceErrors.Unauthorized

		err := handler(w, r)

		if (errors.As(err, &notFoundError)) || (errors.Is(err, storage.ErrNotFound)) {
			render.Status(r, http.StatusNotFound)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusNotFound),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &badRequestError) {
			render.Status(r, http.StatusBadRequest)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusBadRequest),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &serviceUnavailable) {
			render.Status(r, http.StatusServiceUnavailable)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusServiceUnavailable),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &forbiddenError) {
			render.Status(r, http.StatusForbidden)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusForbidden),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &unauthorizedError) {
			render.Status(r, http.StatusUnauthorized)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusUnauthorized),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusInternalServerError),
				Error:  "encountered an unexpected server error: " + err.Error(),
			}
			render.JSON(w, r, response)
			return
		}
	}
}
