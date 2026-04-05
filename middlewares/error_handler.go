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

func writeError(w http.ResponseWriter, r *http.Request, statusCode int, message string) {
	render.Status(r, statusCode)
	render.JSON(w, r, types.ErrorResponse{
		Status: http.StatusText(statusCode),
		Error:  message,
	})
}

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
		if err == nil {
			return
		}

		statusCode := http.StatusInternalServerError
		responseError := err.Error()

		switch {
		case errors.As(err, &notFoundError), errors.Is(err, storage.ErrNotFound):
			statusCode = http.StatusNotFound
		case errors.As(err, &badRequestError):
			statusCode = http.StatusBadRequest
		case errors.As(err, &serviceUnavailable):
			statusCode = http.StatusServiceUnavailable
		case errors.As(err, &forbiddenError):
			statusCode = http.StatusForbidden
		case errors.As(err, &unauthorizedError):
			statusCode = http.StatusUnauthorized
		case errors.As(err, &methodNotAllowedError):
			statusCode = http.StatusMethodNotAllowed
		case errors.As(err, &conflictError):
			statusCode = http.StatusConflict
		case errors.As(err, &goneError):
			statusCode = http.StatusGone
		case errors.As(err, &unsupportedMediaTypeError):
			statusCode = http.StatusUnsupportedMediaType
		case errors.As(err, &unprocessableEntityError):
			statusCode = http.StatusUnprocessableEntity
		case errors.As(err, &tooManyRequestsError):
			statusCode = http.StatusTooManyRequests
		case errors.As(err, &internalServerError):
			statusCode = http.StatusInternalServerError
		case errors.As(err, &badGatewayError):
			statusCode = http.StatusBadGateway
		case errors.As(err, &gatewayTimeoutError):
			statusCode = http.StatusGatewayTimeout
		case errors.As(err, &requestTimeoutError):
			statusCode = http.StatusRequestTimeout
		case errors.As(err, &notImplementedError):
			statusCode = http.StatusNotImplemented
		default:
			responseError = "encountered an unexpected server error: " + err.Error()
		}

		writeError(w, r, statusCode, responseError)
	}
}
