package service

import "github.com/alexonderia/filestore/internal/domain"

var (
	ErrInvalid      = domain.ErrInvalid
	ErrUnauthorized = domain.ErrUnauthorized
	ErrForbidden    = domain.ErrForbidden
	ErrNotFound     = domain.ErrNotFound
	ErrConflict     = domain.ErrConflict
	ErrTooLarge     = domain.ErrTooLarge
	ErrLocked       = domain.ErrLocked
)
