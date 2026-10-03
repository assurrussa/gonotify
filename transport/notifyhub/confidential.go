package notifyhub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/assurrussa/gonotify/transport"
)

// ConfidentialEmailRequest is an immediate, single-recipient email operation.
// Keep the same opaque IdempotencyKey, payload and original ExpiresAt across attempts.
// Never put tokens or recipient addresses in the key or Event. This request must
// not enter the ordinary notification transport, outbox, or a fallback transport.
// ExpiresAt is required and exclusive; the Unix-second wire deadline is floored,
// so its cutoff may be less than one second earlier than the original timestamp.
//
//nolint:tagliatelle // Public request uses the established snake_case convention.
type ConfidentialEmailRequest struct {
	IdempotencyKey string                 `json:"idempotency_key"`
	Event          string                 `json:"event"`
	ExpiresAt      time.Time              `json:"expires_at"`
	Email          transport.EmailMessage `json:"email"`
}

// ConfidentialEmailReceipt describes synchronous provider handoff, not inbox delivery.
// It is separate from the durable queue Receipt. Only accepted with a nil error
// confirms provider acceptance; simulated, dispatching and unknown do not.
//
//nolint:tagliatelle // Receipt follows the established snake_case convention.
type ConfidentialEmailReceipt struct {
	ID        string         `json:"id"`
	Status    DeliveryStatus `json:"status"`
	Duplicate bool           `json:"duplicate"`
	ExpiresAt time.Time      `json:"expires_at"`
}

// StatusDispatching indicates an in-flight confidential provider operation.
const StatusDispatching DeliveryStatus = "dispatching"

// ConfidentialEmailOutcome determines whether the provider could have accepted an email.
type ConfidentialEmailOutcome string

const (
	// OutcomeNotSent means this attempt did not submit to the provider.
	OutcomeNotSent ConfidentialEmailOutcome = "not_sent"
	// OutcomeRetryable means NotifyHub explicitly confirmed a temporary rejection.
	// Reuse the original request and key if retrying before its expiration.
	OutcomeRetryable ConfidentialEmailOutcome = "retryable"
	// OutcomeRejected means a definitive terminal rejection was returned.
	OutcomeRejected ConfidentialEmailOutcome = "rejected"
	// OutcomeUnknown includes pending dispatch and ambiguous network/protocol failures.
	// Never infer non-delivery, issue a new key, or fall back to another transport.
	OutcomeUnknown ConfidentialEmailOutcome = "unknown"
	// OutcomeSimulated means no real provider handoff occurred.
	OutcomeSimulated ConfidentialEmailOutcome = "simulated"
)

var (
	// ErrConfidentialOutcomeUnknown means acceptance cannot be established safely.
	ErrConfidentialOutcomeUnknown = errors.New("confidential email outcome unknown")
	// ErrConfidentialRejected means the provider definitively rejected the email.
	ErrConfidentialRejected = errors.New("confidential email rejected")
	// ErrConfidentialSimulated means the gateway simulated a send without provider handoff.
	ErrConfidentialSimulated = errors.New("confidential email simulated")
)

// ConfidentialEmailError contains only bounded, non-sensitive machine diagnostics.
// It never includes gateway bodies, network error text, addresses, URLs or credentials.
// Outcome is authoritative: a timeout or untrusted response is unknown, not retryable.
// Unwrap preserves safe sentinels (including context cancellation/deadlines).
type ConfidentialEmailError struct {
	Outcome    ConfidentialEmailOutcome
	StatusCode int
	Code       string
	RetryAfter time.Duration
	cause      error
}

func (e *ConfidentialEmailError) Error() string {
	return fmt.Sprintf("notifyhub: confidential email %s (status %d, code %s)", e.Outcome, e.StatusCode, e.Code)
}

func (e *ConfidentialEmailError) Unwrap() error { return e.cause }

//nolint:tagliatelle // Dedicated HTTP API uses snake_case.
type confidentialPayload struct {
	Event     string                 `json:"event"`
	Email     transport.EmailMessage `json:"email"`
	ExpiresAt int64                  `json:"expires_at"`
}

//nolint:tagliatelle // Dedicated HTTP API uses snake_case.
type confidentialResponse struct {
	ID        string         `json:"id"`
	Status    DeliveryStatus `json:"status"`
	Duplicate bool           `json:"duplicate"`
	ExpiresAt int64          `json:"expires_at"`
	Code      string         `json:"code"`
	Error     string         `json:"error"`
}

// SendConfidentialEmail performs exactly one synchronous HTTP attempt with the
// existing gateway connection and project credential. It never queues or falls
// back. Only HTTP 200 with status accepted is successful. Repeat an identical
// operation using its stable key to recover the gateway's metadata-only outcome;
// an unknown outcome must not trigger a blind resend with a new key.
func (c *Client) SendConfidentialEmail(ctx context.Context, req ConfidentialEmailRequest) (ConfidentialEmailReceipt, error) {
	if err := validateConfidentialRequest(req); err != nil {
		return ConfidentialEmailReceipt{}, confidentialError(OutcomeNotSent, 0, "invalid_request", err)
	}
	if err := ctx.Err(); err != nil {
		return ConfidentialEmailReceipt{}, confidentialError(OutcomeNotSent, 0, "context_done", err)
	}
	deadline := time.Unix(req.ExpiresAt.Unix(), 0)
	if !time.Now().Before(deadline) {
		return ConfidentialEmailReceipt{}, confidentialError(OutcomeNotSent, 0, "expired", transport.ErrExpired)
	}
	payload, err := json.Marshal(confidentialPayload{Event: req.Event, Email: req.Email, ExpiresAt: deadline.Unix()})
	if err != nil {
		return ConfidentialEmailReceipt{}, confidentialError(OutcomeNotSent, 0, "invalid_request", transport.ErrInvalidRequest)
	}
	opCtx, stopExpiry := context.WithDeadline(ctx, deadline)
	defer stopExpiry()
	opCtx, stopTimeout := context.WithTimeout(opCtx, c.timeout)
	defer stopTimeout()
	endpoint := c.endpoint("/v1/confidential-email")
	httpReq, err := http.NewRequestWithContext(opCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ConfidentialEmailReceipt{}, confidentialError(OutcomeNotSent, 0, "invalid_request", transport.ErrInvalidRequest)
	}
	// Prevent net/http from replaying a POST on a reused connection after an ambiguous failure.
	httpReq.GetBody = nil
	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpReq.Header.Set("Authorization", "Bearer "+c.projectKey)
	httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)
	if err := opCtx.Err(); err != nil {
		return ConfidentialEmailReceipt{}, confidentialError(OutcomeNotSent, 0, "context_done", err)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ConfidentialEmailReceipt{}, unknownConfidentialError(0, "request_failed", safeContextCause(opCtx, err))
	}
	defer resp.Body.Close()
	return readConfidentialResponse(opCtx, resp, deadline)
}

func validateConfidentialRequest(req ConfidentialEmailRequest) error {
	if err := transport.ValidateIdempotencyKey(req.IdempotencyKey); err != nil {
		return err
	}
	if req.ExpiresAt.IsZero() || len(req.Email.To) != 1 || strings.TrimSpace(req.Email.Subject) == "" ||
		strings.ContainsAny(req.Email.Subject, "\r\n") || (req.Email.Text == "" && req.Email.HTML == "") {
		return transport.ErrInvalidRequest
	}
	for _, address := range []string{req.Email.From, req.Email.To[0]} {
		if strings.ContainsAny(address, "\r\n") {
			return transport.ErrInvalidRequest
		}
		if _, err := mail.ParseAddress(address); err != nil {
			return transport.ErrInvalidRequest
		}
	}
	return nil
}

func readConfidentialResponse(ctx context.Context, resp *http.Response, expiry time.Time) (ConfidentialEmailReceipt, error) {
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	var result confidentialResponse
	valid := readErr == nil && len(body) <= maxResponseBytes && json.Unmarshal(body, &result) == nil
	// Pre-admission HTTP rejections are definitive even if an optional body is unreadable.
	if err := confidentialAdmissionError(resp.StatusCode, resp.Header); err != nil {
		return ConfidentialEmailReceipt{}, err
	}
	if !valid {
		return ConfidentialEmailReceipt{}, unknownConfidentialError(resp.StatusCode, "invalid_response",
			errors.Join(transport.ErrInvalidResponse, safeContextCause(ctx, readErr)))
	}
	if resp.StatusCode == http.StatusConflict && result.Error == "idempotency_conflict" && result.Status == "" {
		return ConfidentialEmailReceipt{}, confidentialError(OutcomeNotSent, resp.StatusCode,
			"idempotency_conflict", transport.ErrIdempotencyConflict)
	}
	if strings.TrimSpace(result.ID) == "" || result.ExpiresAt != expiry.Unix() {
		return ConfidentialEmailReceipt{}, unknownConfidentialError(resp.StatusCode, "invalid_response", transport.ErrInvalidResponse)
	}
	receipt := ConfidentialEmailReceipt{
		ID: result.ID, Status: result.Status, Duplicate: result.Duplicate, ExpiresAt: time.Unix(result.ExpiresAt, 0),
	}
	return classifyConfidentialResponse(resp, result, receipt)
}

func classifyConfidentialResponse(
	resp *http.Response, result confidentialResponse, receipt ConfidentialEmailReceipt,
) (ConfidentialEmailReceipt, error) {
	switch {
	case resp.StatusCode == http.StatusOK && result.Status == StatusAccepted:
		return receipt, nil
	case resp.StatusCode == http.StatusOK && result.Status == StatusSimulated:
		return receipt, confidentialError(OutcomeSimulated, resp.StatusCode,
			confidentialCode(result.Code, "simulated"), ErrConfidentialSimulated)
	case resp.StatusCode == http.StatusAccepted && result.Status == StatusDispatching:
		return receipt, unknownConfidentialError(resp.StatusCode, confidentialCode(result.Code, "dispatching"), nil)
	case resp.StatusCode == http.StatusConflict && result.Status == StatusUnknown:
		return receipt, unknownConfidentialError(resp.StatusCode, confidentialCode(result.Code, "unknown"), nil)
	case resp.StatusCode == http.StatusServiceUnavailable && result.Status == StatusRetry:
		err := confidentialError(OutcomeRetryable, resp.StatusCode,
			confidentialCode(result.Code, "retry"), transport.ErrTemporarilyUnavailable)
		err.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		return receipt, err
	case resp.StatusCode == http.StatusUnprocessableEntity && result.Status == StatusFailed:
		return receipt, confidentialError(OutcomeRejected, resp.StatusCode,
			confidentialCode(result.Code, "failed"), ErrConfidentialRejected)
	default:
		return ConfidentialEmailReceipt{}, unknownConfidentialError(resp.StatusCode, "invalid_response", transport.ErrInvalidResponse)
	}
}

func confidentialAdmissionError(status int, header http.Header) *ConfidentialEmailError {
	var code string
	var cause error
	switch status {
	case http.StatusBadRequest:
		code, cause = "invalid_request", transport.ErrInvalidRequest
	case http.StatusUnauthorized, http.StatusForbidden:
		code, cause = "unauthorized", transport.ErrUnauthorized
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		code, cause = "endpoint_unavailable", transport.ErrNotFound
	case http.StatusGone:
		code, cause = "expired", transport.ErrExpired
	case http.StatusRequestEntityTooLarge:
		code, cause = "request_too_large", transport.ErrPayloadTooLarge
	case http.StatusTooManyRequests:
		code, cause = "quota_exceeded", transport.ErrQuotaExceeded
	default:
		return nil
	}
	err := confidentialError(OutcomeNotSent, status, code, cause)
	if status == http.StatusTooManyRequests {
		err.RetryAfter = parseRetryAfter(header.Get("Retry-After"))
	}
	return err
}

func confidentialError(outcome ConfidentialEmailOutcome, status int, code string, cause error) *ConfidentialEmailError {
	return &ConfidentialEmailError{Outcome: outcome, StatusCode: status, Code: code, cause: cause}
}

func unknownConfidentialError(status int, code string, cause error) *ConfidentialEmailError {
	return confidentialError(OutcomeUnknown, status, code, errors.Join(ErrConfidentialOutcomeUnknown, cause))
}

func safeContextCause(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

// Accept only fixed documented diagnostics; arbitrary identifiers may themselves be tokens.
func confidentialCode(code, fallback string) string {
	switch code {
	case "provider_rejected_temporary", "provider_rejected", "dispatch_outcome_unknown", "dispatch_in_progress",
		"dry_run_no_external_delivery", "recipient_suppressed", "retry_budget_exhausted", "expired":
		return code
	default:
		return fallback
	}
}
