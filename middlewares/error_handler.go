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
		var methodNotAllowedError *serviceErrors.MethodNotAllowed
		var conflictError *serviceErrors.Conflict
		var goneError *serviceErrors.Gone
		var unsupportedMediaTypeError *serviceErrors.UnsupportedMediaType
		var unprocessableEntityError *serviceErrors.UnprocessableEntity
		var tooManyRequestsError *serviceErrors.TooManyRequests
		var internalServerError *serviceErrors.InternalServerError
		var badGatewayError *serviceErrors.BadGateway
		var gatewayTimeoutError *serviceErrors.GatewayTimeout
		var requestTimeoutError *serviceErrors.RequestTimeout
		var notImplementedError *serviceErrors.NotImplemented

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

		if errors.As(err, &methodNotAllowedError) {
			render.Status(r, http.StatusMethodNotAllowed)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusMethodNotAllowed),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &conflictError) {
			render.Status(r, http.StatusConflict)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusConflict),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &goneError) {
			render.Status(r, http.StatusGone)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusGone),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &unsupportedMediaTypeError) {
			render.Status(r, http.StatusUnsupportedMediaType)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusUnsupportedMediaType),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &unprocessableEntityError) {
			render.Status(r, http.StatusUnprocessableEntity)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusUnprocessableEntity),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &tooManyRequestsError) {
			render.Status(r, http.StatusTooManyRequests)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusTooManyRequests),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &internalServerError) {
			render.Status(r, http.StatusInternalServerError)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusInternalServerError),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &badGatewayError) {
			render.Status(r, http.StatusBadGateway)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusBadGateway),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &gatewayTimeoutError) {
			render.Status(r, http.StatusGatewayTimeout)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusGatewayTimeout),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &requestTimeoutError) {
			render.Status(r, http.StatusRequestTimeout)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusRequestTimeout),
				Error:  err.Error(),
			}
			render.JSON(w, r, response)
			return
		}

		if errors.As(err, &notImplementedError) {
			render.Status(r, http.StatusNotImplemented)
			response := types.ErrorResponse{
				Status: http.StatusText(http.StatusNotImplemented),
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
