package errors

type BadRequest struct {
	Message string
}

func (e *BadRequest) Error() string {
	return e.Message
}

type NotFound struct {
	Message string
}

func (e *NotFound) Error() string {
	return e.Message
}

type ServiceUnavailable struct {
	Message string
}

func (e *ServiceUnavailable) Error() string {
	return e.Message
}

type Forbidden struct {
	Message string
}

func (e *Forbidden) Error() string {
	return e.Message
}

type Unauthorized struct {
	Message string
}

func (e *Unauthorized) Error() string {
	return e.Message
}

type MethodNotAllowed struct {
	Message string
}

func (e *MethodNotAllowed) Error() string {
	return e.Message
}

type Conflict struct {
	Message string
}

func (e *Conflict) Error() string {
	return e.Message
}

type Gone struct {
	Message string
}

func (e *Gone) Error() string {
	return e.Message
}

type UnsupportedMediaType struct {
	Message string
}

func (e *UnsupportedMediaType) Error() string {
	return e.Message
}

type UnprocessableEntity struct {
	Message string
}

func (e *UnprocessableEntity) Error() string {
	return e.Message
}

type TooManyRequests struct {
	Message string
}

func (e *TooManyRequests) Error() string {
	return e.Message
}

type InternalServerError struct {
	Message string
}

func (e *InternalServerError) Error() string {
	return e.Message
}

type BadGateway struct {
	Message string
}

func (e *BadGateway) Error() string {
	return e.Message
}

type GatewayTimeout struct {
	Message string
}

func (e *GatewayTimeout) Error() string {
	return e.Message
}

type RequestTimeout struct {
	Message string
}

func (e *RequestTimeout) Error() string {
	return e.Message
}

type NotImplemented struct {
	Message string
}

func (e *NotImplemented) Error() string {
	return e.Message
}
