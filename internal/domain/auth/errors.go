package auth

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	ErrorCodeValidation         ErrorCode = "VALIDATION_FAILED"
	ErrorCodeNotFound           ErrorCode = "NOT_FOUND"
	ErrorCodeInvalidCredentials ErrorCode = "INVALID_CREDENTIALS"
	ErrorCodeForbidden          ErrorCode = "FORBIDDEN"
	ErrorCodeExpiredToken       ErrorCode = "EXPIRED_TOKEN"
	ErrorCodeRevokedToken       ErrorCode = "REVOKED_TOKEN"
	ErrorCodeDisabledUser       ErrorCode = "DISABLED_USER"
	ErrorCodeBootstrapRequired  ErrorCode = "BOOTSTRAP_REQUIRED"
	ErrorCodeConflict           ErrorCode = "CONFLICT"
)

type Error struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}

	return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func IsCode(err error, code ErrorCode) bool {
	var authErr *Error
	if !errors.As(err, &authErr) {
		return false
	}

	return authErr.Code == code
}

func NewValidationError(message string) error {
	return &Error{Code: ErrorCodeValidation, Message: message}
}

func NewNotFoundError(kind, value string) error {
	return &Error{Code: ErrorCodeNotFound, Message: fmt.Sprintf("%s %q was not found", kind, value)}
}

func NewInvalidCredentialsError() error {
	return &Error{Code: ErrorCodeInvalidCredentials, Message: "invalid credentials"}
}

func NewForbiddenError(message string) error {
	return &Error{Code: ErrorCodeForbidden, Message: message}
}

func NewExpiredTokenError(accessor string) error {
	return &Error{Code: ErrorCodeExpiredToken, Message: fmt.Sprintf("token %q has expired", accessor)}
}

func NewRevokedTokenError(accessor string) error {
	return &Error{Code: ErrorCodeRevokedToken, Message: fmt.Sprintf("token %q has been revoked", accessor)}
}

func NewDisabledUserError(username string) error {
	return &Error{Code: ErrorCodeDisabledUser, Message: fmt.Sprintf("user %q is disabled", username)}
}

func NewBootstrapRequiredError() error {
	return &Error{Code: ErrorCodeBootstrapRequired, Message: "auth enabled requires an active global admin; run `registry bootstrap-admin`"}
}

func NewConflictError(message string) error {
	return &Error{Code: ErrorCodeConflict, Message: message}
}
