package iam

import "errors"

var (
	// ErrInvalidCredentials is returned when the email/password combination fails.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrSessionNotFound indicates that the refresh token/session is missing.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionExpired indicates the stored session is no longer valid.
	ErrSessionExpired = errors.New("session expired")
	// ErrAccountFrozen signals that the user is blocked from authentication.
	ErrAccountFrozen = errors.New("account frozen")
	// ErrInvalidPairingRequest signals malformed pairing metadata.
	ErrInvalidPairingRequest = errors.New("invalid pairing request")
	// ErrPairingCodeConflict indicates the generated code already exists.
	ErrPairingCodeConflict = errors.New("pairing code conflict")
	// ErrPairingNotFound indicates a pairing record could not be located.
	ErrPairingNotFound = errors.New("pairing not found")
	// ErrPairingExpired indicates the pairing record has expired.
	ErrPairingExpired = errors.New("pairing expired")
	// ErrPairingPending signals that approval has not yet occurred.
	ErrPairingPending = errors.New("pairing not approved")
	// ErrPairingTokenMismatch signals the device token did not match.
	ErrPairingTokenMismatch = errors.New("pairing token mismatch")
	// ErrDeviceNotFound indicates the requested device does not exist or is not accessible.
	ErrDeviceNotFound = errors.New("device not found")
	// ErrInvalidHeartbeatStatus indicates unsupported heartbeat status.
	ErrInvalidHeartbeatStatus = errors.New("invalid heartbeat status")
)
