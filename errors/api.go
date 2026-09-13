package errors

import "net/http"

// APIError is the shared HTTP error category. Message is safe for callers;
// wrapped database/filesystem errors remain internal and are never serialized.
type APIError struct {
	Code    ErrorCode
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e == nil {
		return "service unavailable"
	}
	return e.Message
}

func (e *APIError) Is(target error) bool {
	other, ok := target.(*APIError)
	if !ok || e == nil || other == nil {
		return false
	}
	return e.Code == other.Code
}

var (
	ErrInvalid       = &APIError{ErrorCodeParamInvalid, http.StatusBadRequest, "invalid request"}
	ErrUnauthorized  = &APIError{ErrorCodeUserNotLogin, http.StatusUnauthorized, "authentication required"}
	ErrForbidden     = &APIError{ErrorCodeForbidden, http.StatusForbidden, "forbidden"}
	ErrNotFound      = &APIError{ErrorCodeNotFound, http.StatusNotFound, "not found"}
	ErrConflict      = &APIError{ErrorCodeConflict, http.StatusConflict, "revision or idempotency conflict"}
	ErrTooLarge      = &APIError{ErrorCodeTooLarge, http.StatusRequestEntityTooLarge, "request is too large"}
	ErrUnsupported   = &APIError{ErrorCodeUnsupported, http.StatusUnsupportedMediaType, "unsupported request"}
	ErrRateLimited   = &APIError{ErrorCodeRateLimited, http.StatusTooManyRequests, "limit reached"}
	ErrUnprocessable = &APIError{ErrorCodeUnprocessable, http.StatusUnprocessableEntity, "invalid content"}
	ErrDependency    = &APIError{ErrorCodeDependency, http.StatusServiceUnavailable, "service unavailable"}
)

// WithMessage adds a caller-safe explanation without changing the category.
// Do not pass raw SQL, filesystem errors, tokens or secret material here.
func WithMessage(category *APIError, message string) *APIError {
	if category == nil {
		category = ErrDependency
	}
	result := *category
	result.Message = message
	return &result
}

// PublicMessage supplies a stable, safe fallback for legacy error codes.
func PublicMessage(code ErrorCode) string {
	switch code {
	case ErrorCodeSuccess:
		return "success"
	case ErrorCodeParamInvalid:
		return ErrInvalid.Message
	case ErrorCodeUserNameOrPasswdWrong:
		return "username or password is incorrect"
	case ErrorCodeUserNotLogin, ErrorCodeUserLoginExpire:
		return "authentication required"
	case ErrorCodeBookNotFound, ErrorCodeStorageNotFount:
		return "not found"
	case ErrorCodeStorageHasExist:
		return "resource already exists"
	case ErrorCodePreCheckFailed:
		return "precondition failed"
	default:
		return "service unavailable"
	}
}
