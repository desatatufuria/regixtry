package regixtry

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	ErrorCodeInvalidDigest     ErrorCode = "INVALID_DIGEST"
	ErrorCodeDigestMismatch    ErrorCode = "DIGEST_MISMATCH"
	ErrorCodeInvalidRepository ErrorCode = "INVALID_REPOSITORY"
	ErrorCodeInvalidManifest   ErrorCode = "INVALID_MANIFEST"
	ErrorCodeValidation        ErrorCode = "VALIDATION_FAILED"
	ErrorCodeNotFound          ErrorCode = "NOT_FOUND"
	ErrorCodeConflict          ErrorCode = "CONFLICT"
	ErrorCodeUnauthorized      ErrorCode = "UNAUTHORIZED"
	ErrorCodePolicyViolation   ErrorCode = "POLICY_VIOLATION"
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
	var registryErr *Error
	if !errors.As(err, &registryErr) {
		return false
	}

	return registryErr.Code == code
}

func NewInvalidDigestError(value string) error {
	return &Error{Code: ErrorCodeInvalidDigest, Message: fmt.Sprintf("invalid digest %q", value)}
}

func NewDigestMismatchError(expected, actual Digest) error {
	return &Error{Code: ErrorCodeDigestMismatch, Message: fmt.Sprintf("digest mismatch: expected %s, got %s", expected, actual)}
}

func NewInvalidRepositoryError(value string) error {
	return &Error{Code: ErrorCodeInvalidRepository, Message: fmt.Sprintf("invalid repository %q", value)}
}

func NewInvalidManifestError(message string) error {
	return &Error{Code: ErrorCodeInvalidManifest, Message: message}
}

func NewValidationError(message string) error {
	return &Error{Code: ErrorCodeValidation, Message: message}
}

func NewNotFoundError(kind, value string) error {
	return &Error{Code: ErrorCodeNotFound, Message: fmt.Sprintf("%s %q was not found", kind, value)}
}

func NewConflictError(message string) error {
	return &Error{Code: ErrorCodeConflict, Message: message}
}

func NewUnauthorizedError(message string) error {
	return &Error{Code: ErrorCodeUnauthorized, Message: message}
}

func NewPolicyViolationError(message string) error {
	return &Error{Code: ErrorCodePolicyViolation, Message: message}
}
