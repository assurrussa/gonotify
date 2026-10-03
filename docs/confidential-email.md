# Confidential synchronous email

Use `(*notifyhub.Client).SendConfidentialEmail` for rendered single-recipient,
short-lived credential mail. Reuse the same `Client`, `Config.BaseURL` and
`Config.ProjectKey` used by ordinary notifications. No second connection or
credential is needed. The server must support `POST /v1/confidential-email`;
missing or incompatible servers fail closed. There is no ordinary queue, outbox,
automatic retry or direct-provider fallback in this operation.

This is deliberately separate from `transport.Transport.Submit`, whose success
means durable queue acceptance. A confidential success means the provider has
accepted responsibility for the message. It does **not** mean inbox delivery.
HTTP 202 queue admission is never a successful confidential send.

## Request and lifetime

`ConfidentialEmailRequest` contains `Event`, `IdempotencyKey`, required
`ExpiresAt time.Time`, and one rendered `transport.EmailMessage`. The email must
have a sender, exactly one recipient, a subject, and text and/or HTML. The stable
opaque key is sent in `Idempotency-Key`; the existing project key is sent as
Bearer authorization. Do not put tokens, addresses or URLs in the event or key.

The wire payload is `{event,email,expires_at}` with `expires_at` in Unix seconds.
The original token expiry is floored, never extended, so the effective cutoff
can be less than one second earlier. Expired wire deadlines fail before I/O.
The earlier of the caller deadline, configured timeout, and token cutoff bounds
the operation. Repeat attempts must preserve the original key, content, event
and expiry; never recompute expiry as `now + TTL`.

The dedicated gateway contract retains only outcome metadata and a protected
payload fingerprint needed for idempotency, not the message body, recipient,
auth URL or token. This endpoint must bypass generic notification persistence,
ordinary payload queues, raw diagnostics and request-body logging. Transport
security, provider policy and provider retention remain deployment concerns.

## Results

`ConfidentialEmailReceipt` exposes `ID`, `Status`, `Duplicate` and effective
`ExpiresAt`. `ConfidentialEmailError` exposes safe bounded `Code`, `StatusCode`,
`Outcome` and `RetryAfter`. `errors.Is` preserves documented sentinels and safe
context cancellation/deadline causes. Raw gateway bodies, arbitrary gateway
codes and network diagnostics are never included in these errors.

| Result | Meaning |
| --- | --- |
| HTTP 200, `accepted`, nil error | Provider handoff confirmed; duplicates reuse the same outcome |
| `OutcomeSimulated` | No external provider handoff; `ErrConfidentialSimulated` |
| `OutcomeRetryable` | Explicit HTTP 503 `retry` receipt confirms temporary rejection; same operation may be retried before expiry |
| `OutcomeRejected` | HTTP 422 `failed` receipt confirms terminal rejection; `ErrConfidentialRejected` |
| `OutcomeNotSent` | Validation, pre-I/O cancellation/expiry, or definitive pre-admission HTTP rejection; check wrapped transport/context sentinel |
| `OutcomeUnknown` | Dispatch pending, stored unknown, network ambiguity, timeout, invalid receipt, mismatched expiry, or untrusted response; `ErrConfidentialOutcomeUnknown` |

A retryable response is not permission to reset the token lifetime or replace
the idempotency key. Respect `RetryAfter` and stop at the original token cutoff.
Quota errors expose `transport.ErrQuotaExceeded`; expired operations expose
`transport.ErrExpired`; mismatched-key content exposes
`transport.ErrIdempotencyConflict`. Authentication/configuration failures must
be corrected rather than retried blindly.

An unknown result can have reached the provider. Never infer failure, create a
new key, revoke an otherwise valid token on that basis, or fall back to another
channel. An identical request/key may recover known gateway metadata, but a
stored unknown remains unknown and must not dispatch again. There is no
exactly-once inbox delivery guarantee.

## Connection safety

Use the gateway origin or deployment prefix as `Config.BaseURL`, without an
API endpoint suffix. An exact `/v1/notifications` suffix is rejected with an
instruction to remove it; deployment prefixes remain supported. Userinfo,
query parameters and fragments are rejected. Remote HTTP is rejected unless
explicitly enabled; loopback HTTP supports local tests. Redirects stay disabled,
and the supplied `http.Client` is cloned rather than mutated. The confidential
POST body is not replayable by the standard HTTP transport.

Tests use local `httptest` servers and synthetic transports only. They cover
wire/auth/prefix contracts, accepted duplicates, unchanged retry/unknown
requests, conflicts, expiration, timeout/cancellation, rejected/malformed
receipts, response limits, redirect protection and sanitized diagnostics.
