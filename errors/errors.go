package errors

import "net/http"

type StatusCoder interface {
	error
	StatusCode() int
}

// HTTPError is a generic composable error for arbitrary HTTP status codes.
type HTTPError struct {
	Message string
	Code    int
}

func (e *HTTPError) Error() string {
	return e.Message
}

func (e *HTTPError) StatusCode() int {
	if e.Code == 0 {
		return http.StatusInternalServerError
	}

	return e.Code
}

func NewHTTPError(code int, message string) *HTTPError {
	return &HTTPError{Message: message, Code: code}
}

type BadRequest struct {
	Message string
}

func (e *BadRequest) Error() string {
	return e.Message
}

func (e *BadRequest) StatusCode() int {
	return http.StatusBadRequest
}

type NotFound struct {
	Message string
}

func (e *NotFound) Error() string {
	return e.Message
}

func (e *NotFound) StatusCode() int {
	return http.StatusNotFound
}

type ServiceUnavailable struct {
	Message string
}

func (e *ServiceUnavailable) Error() string {
	return e.Message
}

func (e *ServiceUnavailable) StatusCode() int {
	return http.StatusServiceUnavailable
}

type Forbidden struct {
	Message string
}

func (e *Forbidden) Error() string {
	return e.Message
}

func (e *Forbidden) StatusCode() int {
	return http.StatusForbidden
}

type Unauthorized struct {
	Message string
}

func (e *Unauthorized) Error() string {
	return e.Message
}

func (e *Unauthorized) StatusCode() int {
	return http.StatusUnauthorized
}

type MethodNotAllowed struct {
	Message string
}

func (e *MethodNotAllowed) Error() string {
	return e.Message
}

func (e *MethodNotAllowed) StatusCode() int {
	return http.StatusMethodNotAllowed
}

type Conflict struct {
	Message string
}

func (e *Conflict) Error() string {
	return e.Message
}

func (e *Conflict) StatusCode() int {
	return http.StatusConflict
}

type Gone struct {
	Message string
}

func (e *Gone) Error() string {
	return e.Message
}

func (e *Gone) StatusCode() int {
	return http.StatusGone
}

type UnsupportedMediaType struct {
	Message string
}

func (e *UnsupportedMediaType) Error() string {
	return e.Message
}

func (e *UnsupportedMediaType) StatusCode() int {
	return http.StatusUnsupportedMediaType
}

type UnprocessableEntity struct {
	Message string
}

func (e *UnprocessableEntity) Error() string {
	return e.Message
}

func (e *UnprocessableEntity) StatusCode() int {
	return http.StatusUnprocessableEntity
}

type TooManyRequests struct {
	Message string
}

func (e *TooManyRequests) Error() string {
	return e.Message
}

func (e *TooManyRequests) StatusCode() int {
	return http.StatusTooManyRequests
}

type InternalServerError struct {
	Message string
}

func (e *InternalServerError) Error() string {
	return e.Message
}

func (e *InternalServerError) StatusCode() int {
	return http.StatusInternalServerError
}

type BadGateway struct {
	Message string
}

func (e *BadGateway) Error() string {
	return e.Message
}

func (e *BadGateway) StatusCode() int {
	return http.StatusBadGateway
}

type GatewayTimeout struct {
	Message string
}

func (e *GatewayTimeout) Error() string {
	return e.Message
}

func (e *GatewayTimeout) StatusCode() int {
	return http.StatusGatewayTimeout
}

type RequestTimeout struct {
	Message string
}

func (e *RequestTimeout) Error() string {
	return e.Message
}

func (e *RequestTimeout) StatusCode() int {
	return http.StatusRequestTimeout
}

type NotImplemented struct {
	Message string
}

func (e *NotImplemented) Error() string {
	return e.Message
}

func (e *NotImplemented) StatusCode() int {
	return http.StatusNotImplemented
}
