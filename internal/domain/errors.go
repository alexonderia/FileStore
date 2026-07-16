package domain

import "errors"

var (
	ErrInvalid      = errors.New("invalid input")
	ErrUnauthorized = errors.New("authentication required")
	ErrForbidden    = errors.New("operation is not allowed")
	ErrNotFound     = errors.New("resource was not found")
	ErrConflict     = errors.New("resource conflicts with current state")
	ErrTooLarge     = errors.New("payload is too large")
	ErrLocked       = errors.New("resource is locked")
)
