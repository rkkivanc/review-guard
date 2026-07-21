package domain

import "errors"

// Sentinel errors mapped to HTTP by the transport layer.
var (
	ErrNotFound           = errors.New("not found")
	ErrConflict           = errors.New("conflict")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrValidation         = errors.New("validation")
	ErrForbidden          = errors.New("forbidden")
)
