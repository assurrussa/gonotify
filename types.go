package gonotify

import (
	"time"

	"github.com/assurrussa/gonotify/templates"
	"github.com/assurrussa/gonotify/transport"
)

// Primary type aliases from templates and transport.
type (
	Transport       = transport.Transport
	ReceiptReader   = transport.ReceiptReader
	HealthChecker   = transport.HealthChecker
	Request         = transport.Request
	Priority        = transport.Priority
	EmailMessage    = transport.EmailMessage
	TelegramMessage = transport.TelegramMessage
	Receipt         = transport.Receipt
	DeliveryReceipt = transport.DeliveryReceipt

	Renderer            = templates.Renderer
	PreloadableRenderer = templates.PreloadableRenderer
	RenderedContent     = templates.RenderedContent
)

// Typed transport error aliases.
type (
	QuotaError   = transport.QuotaError
	RequestError = transport.RequestError
)

// Sentinel error values from transport.
var (
	ErrInvalidRequest         = transport.ErrInvalidRequest
	ErrExpired                = transport.ErrExpired
	ErrUnauthorized           = transport.ErrUnauthorized
	ErrIdempotencyConflict    = transport.ErrIdempotencyConflict
	ErrPayloadTooLarge        = transport.ErrPayloadTooLarge
	ErrQuotaExceeded          = transport.ErrQuotaExceeded
	ErrTemporarilyUnavailable = transport.ErrTemporarilyUnavailable
	ErrNotFound               = transport.ErrNotFound
	ErrInvalidResponse        = transport.ErrInvalidResponse
)

// Idempotency key bounds from transport.
const (
	MinIdempotencyKeyLength = transport.MinIdempotencyKeyLength
	MaxIdempotencyKeyLength = transport.MaxIdempotencyKeyLength
)

// ValidateIdempotencyKey validates that key meets the gonotify transport contract.
func ValidateIdempotencyKey(key string) error {
	return transport.ValidateIdempotencyKey(key)
}

// Scheduling priorities from transport. High priority requires Request.ExpiresAt.
const (
	PriorityNormal = transport.PriorityNormal
	PriorityHigh   = transport.PriorityHigh
)

// ValidatePriority checks supported priority values and the high-priority deadline requirement.
func ValidatePriority(priority Priority, expiresAt *time.Time) error {
	return transport.ValidatePriority(priority, expiresAt)
}
