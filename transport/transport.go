package transport

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Transport defines the delivery transport contract.
//
// Semantics & Invariants:
//
// 1. Durable Acceptance:
// A successful Submit (err == nil) confirms that the delivery gateway has
// accepted and durably persisted the notification request. It does NOT guarantee
// immediate delivery or downstream provider acceptance. Downstream delivery progression
// (e.g. queued, sending, accepted, delivered, bounced) is tracked via Receipt.Deliveries
// or through ReceiptReader.Get.
// A Transport implementation MUST provide durable acceptance semantics. A direct
// fire-and-forget or transient provider client (e.g. unbuffered SMTP or raw Telegram HTTP)
// does not satisfy this contract.
//
// 2. Idempotency:
// Every Transport implementation MUST honor Request.IdempotencyKey.
// Re-submitting the same idempotency key with equivalent content MUST NOT create a second
// logical notification and MUST return a receipt indicating duplicate submission if supported
// (or the original submission receipt).
// Reusing the same idempotency key with different content MUST return ErrIdempotencyConflict
// when the transport can detect the conflict.
//
// 3. Expiration:
// When Request.ExpiresAt is set, the transport MUST prevent submission or downstream
// delivery at or after that instant. Gateways must enforce expiration after admission too.
// A transport rejecting an expired request returns ErrExpired.
type Transport interface {
	Submit(ctx context.Context, req Request) (Receipt, error)
}

const (
	// MinIdempotencyKeyLength is the minimum allowed byte length for an idempotency key.
	MinIdempotencyKeyLength = 8
	// MaxIdempotencyKeyLength is the maximum allowed byte length for an idempotency key.
	MaxIdempotencyKeyLength = 200
)

// ValidateIdempotencyKey validates that key conforms to the gonotify transport contract:
// Valid UTF-8, 8 to 200 bytes, no leading or trailing whitespace, and no ASCII control characters.
// ASCII keys are recommended for interoperability across transports.
func ValidateIdempotencyKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: missing idempotency_key in request", ErrInvalidRequest)
	}
	if !utf8.ValidString(key) {
		return fmt.Errorf("%w: idempotency_key must be valid UTF-8", ErrInvalidRequest)
	}
	for i := range len(key) {
		if key[i] < 0x20 || key[i] == 0x7f {
			return fmt.Errorf("%w: idempotency_key contains invalid control characters", ErrInvalidRequest)
		}
	}
	if key != strings.TrimSpace(key) {
		return fmt.Errorf("%w: idempotency_key must not contain leading or trailing whitespace", ErrInvalidRequest)
	}
	if len(key) < MinIdempotencyKeyLength || len(key) > MaxIdempotencyKeyLength {
		return fmt.Errorf("%w: idempotency_key length must be between %d and %d, got %d",
			ErrInvalidRequest, MinIdempotencyKeyLength, MaxIdempotencyKeyLength, len(key))
	}
	return nil
}

// ReceiptReader is an optional transport capability for querying previously submitted receipts.
type ReceiptReader interface {
	Get(ctx context.Context, id string) (Receipt, error)
}

// HealthChecker is an optional transport capability for checking gateway availability.
type HealthChecker interface {
	Ping(ctx context.Context) error
}

// Request is the immutable notification delivery request.
//
//nolint:tagliatelle // Serialization contract uses snake_case for external compatibility.
type Request struct {
	Event          string `json:"event"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	// ExpiresAt is the exclusive submission and delivery deadline; nil means no expiration.
	ExpiresAt *time.Time       `json:"expires_at,omitempty"`
	Email     *EmailMessage    `json:"email,omitempty"`
	Telegram  *TelegramMessage `json:"telegram,omitempty"`
}

// EmailMessage contains the rendered email payload.
type EmailMessage struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text,omitempty"`
	HTML    string   `json:"html,omitempty"`
}

// TelegramMessage contains the rendered Telegram payload.
//
//nolint:tagliatelle // Serialization contract uses snake_case for external compatibility.
type TelegramMessage struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

// Receipt represents the submission confirmation from the delivery gateway.
type Receipt struct {
	ID         string            `json:"id"`
	Duplicate  bool              `json:"duplicate"`
	Deliveries []DeliveryReceipt `json:"deliveries,omitempty"`
}

// DeliveryReceipt represents a single downstream delivery item.
type DeliveryReceipt struct {
	ID        string `json:"id"`
	Channel   string `json:"channel"`
	Recipient string `json:"recipient"`
	Status    string `json:"status"`
}
