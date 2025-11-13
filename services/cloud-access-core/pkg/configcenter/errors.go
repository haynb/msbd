package configcenter

import "errors"

var (
	// ErrProfileNotFound indicates the requested client profile does not exist.
	ErrProfileNotFound = errors.New("profile not found")
	// ErrInvalidRequest indicates malformed input.
	ErrInvalidRequest = errors.New("invalid config request")
)
