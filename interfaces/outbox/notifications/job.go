package gonotifyjob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/assurrussa/outbox/outbox"
	sharedjob "github.com/assurrussa/outbox/shared/job"

	"github.com/assurrussa/gonotify/transport"
)

const (
	JobName                                            = "notifications_send"
	SchemaVersion                 outbox.SchemaVersion = 2
	defaultUnauthorizedDeferDelay                      = 5 * time.Minute
)

var (
	_ outbox.Job          = (*Job)(nil)
	_ outbox.VersionedJob = (*Job)(nil)
)

type Job struct {
	sharedjob.DefaultJob
	Options
}

func Must(opts Options) *Job {
	j, err := New(opts)
	if err != nil {
		panic(err)
	}

	return j
}

func New(opts Options) (*Job, error) {
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("validate job options: %w", err)
	}

	return &Job{
		Options: opts,
	}, nil
}

func (j *Job) Name() string { return JobName }

func (j *Job) SchemaVersion() outbox.SchemaVersion { return SchemaVersion }

func (j *Job) Handle(ctx context.Context, payload string) error {
	p, err := UnmarshalPayload(payload)
	if err != nil {
		return outbox.Permanent(err)
	}

	if err := ValidatePayload(p); err != nil {
		return outbox.Permanent(err)
	}
	if p.Request.ExpiresAt != nil && !p.Request.ExpiresAt.After(time.Now()) {
		return nil // Expiration is a normal completion, without submission or DLQ.
	}

	_, err = j.transport.Submit(ctx, p.Request)
	if err == nil || errors.Is(err, transport.ErrExpired) {
		return nil
	}
	now := time.Now()
	if p.Request.ExpiresAt != nil && !p.Request.ExpiresAt.After(now) {
		return nil // Do not retry a request that expired during submission.
	}
	if errors.Is(err, transport.ErrInvalidRequest) ||
		errors.Is(err, transport.ErrIdempotencyConflict) ||
		errors.Is(err, transport.ErrPayloadTooLarge) {
		return outbox.Permanent(fmt.Errorf("submit transport request: %w", err))
	}

	var qErr *transport.QuotaError
	if errors.As(err, &qErr) && qErr.RetryAfter > 0 {
		return outbox.RetryAt(fmt.Errorf("submit transport request: %w", err), nextAttemptAt(p.Request, now, qErr.RetryAfter))
	}

	if errors.Is(err, transport.ErrUnauthorized) {
		return outbox.DeferAt(fmt.Errorf("submit transport request: %w", err),
			nextAttemptAt(p.Request, now, defaultUnauthorizedDeferDelay))
	}

	return fmt.Errorf("submit transport request: %w", err)
}

func nextAttemptAt(req transport.Request, now time.Time, delay time.Duration) time.Time {
	at := now.Add(delay)
	if req.ExpiresAt != nil && req.ExpiresAt.Before(at) {
		return *req.ExpiresAt
	}
	return at
}
