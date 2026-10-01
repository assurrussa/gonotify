package transport

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidRequest is returned when the payload fails validation.
	ErrInvalidRequest = errors.New("invalid request")

	// ErrExpired is returned when a notification has reached its expiration deadline.
	ErrExpired = errors.New("notification expired")

	// ErrUnauthorized is returned when gateway authentication fails.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrIdempotencyConflict is returned when an idempotency key was previously used with different content.
	ErrIdempotencyConflict = errors.New("idempotency conflict")

	// ErrPayloadTooLarge is returned when the request exceeds maximum message size limits.
	ErrPayloadTooLarge = errors.New("payload too large")

	// ErrQuotaExceeded is returned when admission or rate quotas are exhausted.
	ErrQuotaExceeded = errors.New("quota exceeded")

	// ErrTemporarilyUnavailable is returned when the delivery gateway is overloaded or in maintenance.
	ErrTemporarilyUnavailable = errors.New("temporarily unavailable")

	// ErrNotFound is returned when the requested delivery or notification does not exist.
	ErrNotFound = errors.New("not found")

	// ErrInvalidResponse is returned when the gateway response violates protocol schema expectations.
	ErrInvalidResponse = errors.New("invalid gateway response")
)

// QuotaError describes admission or rate limit exhaustion with an optional RetryAfter duration.
type QuotaError struct {
	Code       string
	RetryAfter time.Duration
}

func (e *QuotaError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("quota exceeded (%s, retry after %s)", e.Code, e.RetryAfter)
	}
	return fmt.Sprintf("quota exceeded (%s)", e.Code)
}

func (e *QuotaError) Unwrap() error {
	return ErrQuotaExceeded
}

// RequestError wraps detailed HTTP status codes and error messages from the gateway.
type RequestError struct {
	StatusCode int
	Code       string
	Message    string
	Err        error
}

func (e *RequestError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("transport error (status %d, code %s): %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("transport error (status %d): %s", e.StatusCode, e.Message)
}

func (e *RequestError) Unwrap() error {
	return e.Err
}
