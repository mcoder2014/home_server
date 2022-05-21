package errors

import "fmt"

type Error struct {
	Code    ErrorCode
	Message string
	origin  error
}

func (e Error) Error() string {
	return e.String()
}

func (e Error) String() string {
	if e.origin == nil {
		return fmt.Sprintf("error code:%v, message:%v", e.Code, e.Message)
	}
	return fmt.Sprintf("error code:%v, message:%v, origin error:%+v", e.Code, e.Message, e.origin)
}

func (e *Error) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"code": %d, "message": "%s"}`, e.Code, e.String())), nil
}

func (e *Error) Unwrap() error {
	return e.origin
}

func Wrap(err error, code ErrorCode) error {
	return &Error{
		Code:   code,
		origin: err,
	}
}

func Wrapf(err error, code ErrorCode, message string) error {
	return &Error{
		Code:    code,
		Message: message,
		origin:  err,
	}
}

func New(code ErrorCode) error {
	return &Error{
		Code: code,
	}
}

func NewWithMessage(code ErrorCode, message string) error {
	return &Error{
		Code:    code,
		Message: message,
	}
}
