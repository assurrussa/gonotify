# Project Overview

## Purpose

`gonotify` is a Go notification library providing an immutable template engine,
transport contracts, a NotifyHub delivery gateway client, and outbox job processing.

The module path is `github.com/assurrussa/gonotify`.

## Stable Packages

The supported external consumer packages are defined in
`reference/externalconsumer/packages.go` and compile-checked through
`reference/externalconsumer/imports.go`.

Stable packages:

- `github.com/assurrussa/gonotify` for primary type aliases and errors.
- `github.com/assurrussa/gonotify/templates` for immutable template rendering.
- `github.com/assurrussa/gonotify/transport` for transport contracts and message models.
- `github.com/assurrussa/gonotify/transport/notifyhub` for the NotifyHub gateway client and delivery statuses.
- `github.com/assurrussa/gonotify/di` for `godi` host wiring.
- `github.com/assurrussa/gonotify/interfaces/outbox/notifications` for
  deferred notification jobs (schema version 2).

Everything else is internal implementation detail.

## Package Map

- Root package `gonotify`: primary type aliases, transport models, and error re-exports.
- `templates`: modern filesystem/embedded template engine (`Renderer`, `PreloadableRenderer`) supporting
  `html/template` auto-escaping, `text/template`, layouts, partials, caching (`NewCached`), and preloading (`Preload()`).
- `transport`: unified delivery interface (`Transport`) with optional capabilities (`ReceiptReader`, `HealthChecker`)
  and immutable message structs (`Request`, `EmailMessage`, `TelegramMessage`, `Receipt`) with typed errors.
- `transport/notifyhub`: HTTP transport adapter for the NotifyHub notification gateway with security redirect policy and loopback-only unencrypted HTTP enforcement.
- `di`: `godi` providers for `transport.Transport` and outbox `Job`.
- `interfaces/outbox/notifications`: outbox job supporting pre-rendered `transport.Request` with schema version 2,
  safe producer helper (`Put`), typed error classifications (`Permanent`, `RetryAt`, `DeferAt`), payload validation, and options.
- `reference/externalconsumer`: source of truth for stable host import policy.
- `cmd/importpolicy`: CLI that scans host repos for unsupported `gonotify` imports.

## Template Contract

Templates are loaded from any `fs.FS` (`os.DirFS` or `embed.FS`):
- HTML files parsed using `html/template` with automatic contextual escaping.
- Plain text and subject files parsed using `text/template`.
- Both engines enforce `missingkey=error` to avoid silent data truncation.
- Optional outer layouts (`layout.html.tmpl`, `layout.txt.tmpl`).
- Optional partials (`partials/*.html.tmpl`, `partials/*.txt.tmpl`).
- Direct rendering via `templates.New` / `templates.NewDir`.
- Thread-safe caching via `templates.NewCached` / `templates.NewCachedDir`.
- Startup preloading via `Preload()` recursively parses nested message directories,
  root layouts and partials and validates static template references. Data-dependent
  errors remain render-time checks. Filesystem errors propagate; only optional file
  absence is ignored.

## Outbox Contract

The outbox job name is `notifications_send` with capability schema version `2`.

Payload structure:
- `request`: complete `transport.Request` containing mandatory `idempotency_key` (8-200 bytes) and pre-rendered `email` and/or `telegram` messages.

Safe Enqueue & Idempotency:
- Use `notificationsjob.Put(ctx, outboxService, req, time.Now())` for early validation and automatic schema v2 staging via `outbox.VersionedPutter.PutVersioned`.
- Downstream idempotency: the mandatory stable `idempotency_key` ensures downstream delivery is retry-safe; retries or duplicate gateway submissions will not produce duplicate recipient notifications. Storage-level enqueue deduplication across concurrent producers requires unique putters (`PutVersionedUnique`).

The job executes `transport.Submit(ctx, req.Request)` and translates errors into outbox dispositions:
- `outbox.Permanent`: invalid requests (400), idempotency conflicts (409), oversized payloads (413), and malformed payloads transition directly to DLQ.
- `outbox.RetryAt`: rate quota exhaustion (429) reschedules according to RFC 7231 `Retry-After`, capped at `ExpiresAt` when set.
- `outbox.DeferAt`: unauthorized failures (401) postpone without consuming retry attempts, preventing DLQ drops during credential rotation.
- Standard retry: temporary unavailability (503) and network failures retry via the worker runner.
- Expiration: structurally valid requests at or past `ExpiresAt`, transport `ErrExpired`, and errors completing after expiration return `nil` (ack/drop), without retries or DLQ. Already expired requests are never submitted; malformed payloads remain permanent errors. The 401 defer time is capped at expiration too.

Every transport must honor idempotency and prevent admission/delivery after expiration.
Keys require valid UTF-8, use byte bounds, reject surrounding whitespace and ASCII control bytes, and should
prefer ASCII. NotifyHub rejects invalid `ProjectKey` values at construction without
normalizing or logging secrets. Its expiration wire format uses whole Unix seconds
(fractional seconds truncated); an expired wire deadline returns `ErrExpired` locally.

Outbox Backend Compatibility:
- `outbox.DeferAt` requires backend module versions implementing `DeferJobsRepository` and compatible with outbox v0.15. PostgreSQL, MySQL, SQLite and Picodata backends have independent module versions; updating the root module does not update them automatically. Verify selected host versions with `go list -m all` before deploying worker nodes.

## Shared Wiki Alignment

The shared platform pages describe `gonotify` as the notification client library
and link deferred delivery to `outbox` and `notifyhub`. Keep shared pages concise and
contract-focused. Local docs and code win when there is drift.
