package gonotifyjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/assurrussa/outbox/outbox"
	"github.com/assurrussa/outbox/shared/types"

	"github.com/assurrussa/gonotify/transport"
)

// JobID represents an outbox job identifier.
type JobID = types.JobID

// Payload defines the notification sending task payload for schema version 2.
// It encapsulates pre-rendered transport requests with a mandatory stable idempotency key.
type Payload struct {
	Request transport.Request `json:"request"`
}

// ValidateRequest checks structural invariants for a transport request before outbox staging.
func ValidateRequest(req transport.Request) error {
	if err := transport.ValidateIdempotencyKey(req.IdempotencyKey); err != nil {
		return fmt.Errorf("cannot process request: %w", err)
	}
	if req.Email == nil && req.Telegram == nil {
		return fmt.Errorf(
			"cannot process request: %w: missing message payload (email or telegram) in request",
			transport.ErrInvalidRequest,
		)
	}
	return nil
}

// ValidatePayload validates the notification payload.
func ValidatePayload(p Payload) error {
	return ValidateRequest(p.Request)
}

// MarshalPayload validates and serializes the notification payload.
func MarshalPayload(p Payload) (string, error) {
	if err := ValidatePayload(p); err != nil {
		return "", fmt.Errorf("invalid payload: %w", err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	return string(b), nil
}

// UnmarshalPayload deserializes the notification payload JSON string.
func UnmarshalPayload(data string) (Payload, error) {
	var p Payload
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return Payload{}, fmt.Errorf("unmarshal payload: %w", err)
	}
	return p, nil
}

// Put stages a notification request into the outbox with schema version 2.
// It validates the request and marshals the payload before calling outbox.PutVersioned.
func Put(
	ctx context.Context,
	putter outbox.VersionedPutter,
	req transport.Request,
	availableAt time.Time,
) (JobID, error) {
	if isNil(putter) {
		return JobID{}, errors.New("cannot put notification job: outbox putter is nil")
	}

	payload, err := MarshalPayload(Payload{Request: req})
	if err != nil {
		return JobID{}, fmt.Errorf("prepare notification payload: %w", err)
	}

	jobID, err := putter.PutVersioned(ctx, JobName, SchemaVersion, payload, availableAt)
	if err != nil {
		return JobID{}, fmt.Errorf("put notification job: %w", err)
	}

	return jobID, nil
}
