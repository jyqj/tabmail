package app

import (
	"errors"

	"tabmail/internal/authz"
)

type ErrorKind string

const (
	KindBadRequest ErrorKind = "bad_request"
	KindForbidden  ErrorKind = "forbidden"
	KindNotFound   ErrorKind = "not_found"
	KindConflict   ErrorKind = "conflict"
	KindInternal   ErrorKind = "internal"
	// KindQuotaExceeded marks a rate/quota limitation. It has no HTTP number
	// baked in; the transport layer decides how to render it.
	KindQuotaExceeded ErrorKind = "quota_exceeded"
)

type Error struct {
	Kind    ErrorKind
	Message string
	Err     error
	// Data optionally carries machine-readable payload alongside the error
	// (e.g. the current revision on a draft conflict). The transport layer
	// decides whether and how to surface it; domain code only fills it in.
	Data map[string]any
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

// ConflictWithData builds a conflict that additionally reports the current
// domain state (e.g. the live revision) so clients can resynchronize.
func ConflictWithData(msg string, data map[string]any) error {
	return &Error{Kind: KindConflict, Message: msg, Data: data}
}

// QuotaExceeded reports that a usage limit has been reached.
func QuotaExceeded(msg string) error { return &Error{Kind: KindQuotaExceeded, Message: msg} }

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

// FromAuthz maps an authorization result into an app error: an authz denial
// becomes Forbidden (preserving the message), any other error becomes Internal,
// and nil stays nil. It is the single converter shared by the service
// authorize() wrappers.
func FromAuthz(err error) error {
	if err == nil {
		return nil
	}
	if authz.IsAuthzError(err) {
		return Forbidden(err.Error())
	}
	return Internal(err)
}
