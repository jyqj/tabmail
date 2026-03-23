package app

import "errors"

type ErrorKind string

const (
	KindBadRequest ErrorKind = "bad_request"
	KindForbidden  ErrorKind = "forbidden"
	KindNotFound   ErrorKind = "not_found"
	KindConflict   ErrorKind = "conflict"
	KindInternal   ErrorKind = "internal"
)

type Error struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Kind)
}

func (e *Error) Unwrap() error { return e.Err }

func BadRequest(msg string) error { return &Error{Kind: KindBadRequest, Message: msg} }
func Forbidden(msg string) error  { return &Error{Kind: KindForbidden, Message: msg} }
func NotFound(msg string) error   { return &Error{Kind: KindNotFound, Message: msg} }
func Conflict(msg string) error   { return &Error{Kind: KindConflict, Message: msg} }
func Internal(err error) error {
	if err == nil {
		return nil
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return err
	}
	return &Error{Kind: KindInternal, Message: "internal server error", Err: err}
}

func As(err error) (*Error, bool) {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}
