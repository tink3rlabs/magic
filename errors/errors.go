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
