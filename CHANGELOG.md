# Changelog

## Unreleased (planned v0.5.0)

The existing `v0.4.0` tag points to the legacy API. The transport migration
requires a new immutable `v0.5.0` tag; do not move or reuse `v0.4.0`.

### Confidential email
- Added `Client.SendConfidentialEmail` with dedicated request, receipt and outcome
  types on the existing NotifyHub connection. Explicit provider acceptance is
  separate from ordinary queue admission and inbox delivery.
- Stable idempotency, original token expiry, bounded synchronous I/O, strict
  receipt validation, and sanitized unknown/retry/rejection outcomes fail closed
  without queue, replay, or direct-SMTP fallback.
- Reject endpoint-suffixed or credential-bearing BaseURLs with safe actionable
  diagnostics while retaining deployment prefixes and redirect protection.

### Review fixes
- Recursive startup preloading for nested message templates, including static
  reference validation and partial parsing when no message uses the engine yet.
- Template permission/read failures now propagate instead of being mistaken for
  optional file absence; a missing required subject partial cannot silently drop the subject.
- Idempotency keys must be valid UTF-8, preventing JSON from changing their identity.
- Outbox staging rejects typed nil putters instead of panicking.
- Make uses portable native cache defaults unless `GO_SHARED_CACHE_ROOT` is supplied;
  explicit cache overrides remain respected.

### Architecture
- Decoupled `gonotify` into a focused four-pillar architecture:
  1. `templates`: immutable filesystem and embeddable template engine with `html/template` auto-escaping, `text/template`, and `missingkey=error`. `NewDir` and `NewCachedDir` wrap `os.DirFS` directly without source tree magic.
  2. `transport`: provider-neutral delivery abstraction with typed error classifications, explicit durable acceptance and idempotency semantics, single-source idempotency validation (`transport.ValidateIdempotencyKey`), and capability interfaces (`ReceiptReader`, `HealthChecker`).
  3. `transport/notifyhub`: HTTP transport adapter for NotifyHub with bearer auth, loopback-only unencrypted HTTP enforcement (`AllowInsecureHTTP`), client-side idempotency key validation (8-200 bytes, no outer whitespace or ASCII control bytes), redirect blocking, subpath support, and RFC 7231 `Retry-After`.
  4. `interfaces/outbox/notifications`: outbox job processing with capability schema version 2, safe producer helper `notificationsjob.Put`, fail-fast payload validation via `transport.ValidateIdempotencyKey`, and explicit error dispositions (`Permanent`, `RetryAt`, `DeferAt` for 401 credential outages).
- Upgraded `github.com/assurrussa/outbox` to `v0.15.0`.
- Removed unused `transport/mocks` and gonotify's direct generator/tool requirements. Hand-written outbox options preserve constructor/setter signatures and reject typed nil transports. Upstream test/tool dependencies can still appear in the full module graph.
- `ProjectKey` validation now fails at construction for whitespace/control bytes; secrets are neither normalized nor exposed in errors.
- Expired notifications complete outbox jobs normally without DLQ; 401/429 schedules are capped at expiration. Direct NotifyHub submissions return the public `ErrExpired` for expired Unix-second deadlines.
- Fixed receipt-ID double escaping, rejected dot path IDs and prevented `Retry-After` duration overflow.
- Root `ValidateIdempotencyKey` is a wrapper function; all transports must honor idempotency and expiration.
- Ordinary Make targets reuse overridable shared Go caches.
- Cleaned up `.golangci.yml` depguard to reflect pure runtime dependencies.
- Removed legacy in-process delivery engines (`email/`, `telegram/`, `base/`, `NotificationManager`, Fiber middleware, SMTP pool, and rate limiters). Delivery concerns are delegated to NotifyHub.
- Removed private `goshared` dependency; all dependencies resolve through the public Go module proxy.

### Breaking Changes & Migration Guide

#### Migrating to v0.5.0:
1. **Outbox Schema Version 2**:
   - `interfaces/outbox/notifications.Job` now implements `outbox.VersionedJob` with `SchemaVersion = 2`.
   - The payload format is updated: `Payload` contains `Request transport.Request`, where `IdempotencyKey` resides solely in `Request.IdempotencyKey`.
   - Recommended producer enqueue: `notificationsjob.Put(ctx, outboxService, req, time.Now())` which performs structural validation and stages via `outbox.VersionedPutter`.
   - Alternatively, manual publish via `outboxService.PutVersioned(ctx, notificationsjob.JobName, notificationsjob.SchemaVersion, payload, availableAt)`.
   - 401 Unauthorized uses `outbox.DeferAt` to postpone jobs without burning retry attempt limits.
   - **Migration action**: Drain all in-flight schema v1 outbox tasks before upgrading worker services to v0.5.0.
2. **Template Rendering**:
   - Replace mutable `notification.Notification` methods with `templates.New(fs)` or `templates.NewCached(fs)`.
   - Result is returned as an immutable `templates.RenderedContent` (`Subject`, `HTML`, `Text`).
   - `missingkey=error` is enabled by default to prevent delivering notifications with missing variables.
   - `NewDir` / `NewCachedDir` construct directly from disk paths without ancestor scanning.
3. **Transport & Delivery**:
   - Direct SMTP and Telegram configurations are replaced by `transport.Transport`.
   - For NotifyHub delivery, configure `transport/notifyhub.New(notifyhub.Config{ BaseURL: ..., ProjectKey: ... })`. Remote plain HTTP requires `AllowInsecureHTTP: true`.
   - Successful `Submit` indicates gateway persistence, not final recipient delivery.
   - Handle `ErrExpired` as normal expiration. The outbox job acknowledges expired tasks without DLQ. NotifyHub deadlines use whole Unix seconds with fractional seconds truncated.
   - `transport/mocks` is removed (outside the stable consumer surface); host tests should provide their own transport doubles.
   - Errors from `Submit()` are typed: `ErrInvalidRequest` (400), `ErrUnauthorized` (401), `ErrIdempotencyConflict` (409), `ErrPayloadTooLarge` (413), `QuotaError` (429), and `ErrTemporarilyUnavailable` (503).
4. **Dependency Injection**:
   - `di.ModuleBootstrap()` now registers `ProvideNotifyHubClient` and `ProvideOutboxJob` (the latter requires only `transport.Transport`, removing `gologger`).
5. **Outbox Backend Compatibility**:
   - `Job.Handle` returns `outbox.DeferAt` on HTTP 401 (`ErrUnauthorized`) to postpone jobs without burning attempt budgets.
   - Use backend module versions implementing `DeferJobsRepository` and compatible with outbox v0.15. PostgreSQL, MySQL, SQLite and Picodata backends are independently versioned; upgrading the root module does not upgrade them automatically. Verify selected host versions with `go list -m all`.
   - Hosts using custom outbox backends or decoupled repository modules must verify or update their repository implementation before upgrading to `v0.5.0`.

## v0.3.6 - 2026-03-03

### Fixed
- Fixed `TemplatesDir` path resolution for `email/templates` to support predictable behavior in all common scenarios:
  - absolute path (e.g. `/app/templates/email`)
  - relative path from current working directory (e.g. `templates/email`)
  - empty value (`TemplatesDir=""`) with backward-compatible fallback behavior.
- Removed the `DirFS with empty root` failure mode for non-empty `TemplatesDir`.
- Invalid template directories now surface diagnostic filesystem errors (for example: `no such file or directory`) instead of `DirFS with empty root`.

### Changed
- Updated `resolveDir` strategy in `email/templates/embed.go`:
  - trim input
  - absolute path handling
  - relative path lookup via `cwd` first
  - fallback via `filecaller.FindFileDir`
  - return original input when not found to preserve clear OS errors.
- Added an extra invariant in renderer initialization to prevent `os.DirFS("")` when `TemplatesDir` is non-empty.

### Tests
- Added and updated unit tests for:
  - empty `TemplatesDir`
  - valid relative/absolute paths
  - missing relative/absolute paths
  - `NewFSRenderer` and `NewCachedFSRenderer` behavior
  - service preload error path to ensure `not exists` diagnostics and no `empty root` regression.
