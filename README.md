# gonotify

[![Go Reference](https://pkg.go.dev/badge/github.com/assurrussa/gonotify.svg)](https://pkg.go.dev/github.com/assurrussa/gonotify)
[![Go Report Card](https://goreportcard.com/badge/github.com/assurrussa/gonotify)](https://goreportcard.com/report/github.com/assurrussa/gonotify)
[![Go](https://github.com/assurrussa/gonotify/actions/workflows/go.yml/badge.svg)](https://github.com/assurrussa/gonotify/actions/workflows/go.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

English | [Русский](docs/README.ru.md)

A Go notification library providing an immutable template engine, transport
contracts, a NotifyHub delivery gateway client, and outbox job processing.

## Features

- **Immutable templates**: HTML/text rendering with auto-escaping, layouts, partials, caching, and preloading via `templates`.
- **Unified transport**: Provider-neutral `transport.Transport` contract with typed errors (`QuotaError`, `RequestError`, etc.) and optional capability interfaces (`ReceiptReader`, `HealthChecker`).
- **NotifyHub client**: NotifyHub HTTP client adapter (`transport/notifyhub`) with bearer auth, idempotency keys, no-redirect security policy, and RFC 7231 `Retry-After`.
- **Confidential email**: `Client.SendConfidentialEmail` uses the same gateway client and project key for immediate, metadata-only, single-recipient provider handoff, with explicit unknown/retry/rejection outcomes and no queue fallback. See [confidential email](docs/confidential-email.md).
- **Retry-safe outbox (v2)**: Deferred job handling (`interfaces/outbox/notifications`) supporting pre-rendered transport requests with schema version 2, downstream delivery idempotency keys, and typed outbox dispositions (`Permanent`, `RetryAt`, `DeferAt`).
- **Dependency injection**: Clean wiring through `godi` via `di.ModuleBootstrap()`.

## Installation

Use the Go version and toolchain declared in [go.mod](go.mod): Go `1.27.0`
with toolchain `go1.27.1`.

```bash
go get github.com/assurrussa/gonotify
```

## Supported packages

The stable consumer surface is defined in
[reference/externalconsumer/packages.go](reference/externalconsumer/packages.go):

- `github.com/assurrussa/gonotify` — primary type aliases and errors.
- `github.com/assurrussa/gonotify/templates` — immutable template engine (`Renderer`, `PreloadableRenderer`, `RenderedContent`).
- `github.com/assurrussa/gonotify/transport` — transport interface, message models, and typed delivery errors.
- `github.com/assurrussa/gonotify/transport/notifyhub` — NotifyHub gateway HTTP transport, dedicated confidential-email requests/outcomes, and delivery statuses.
- `github.com/assurrussa/gonotify/di` — host wiring through `godi`.
- `github.com/assurrussa/gonotify/interfaces/outbox/notifications` — deferred notification jobs, schema version 2, and payloads.

Other packages are internal implementation details.

From this repository, check the documented `../site` host with all consumer roots:

```bash
go run ./cmd/importpolicy --repo-root ../site --consumers backend,goadmin,goauth,fixtures
```

For another host, list every consumer root; omitted directories are not checked.
See the [project overview](docs/project-overview.md) for the package map and contracts.

## Quick Start: Templates & NotifyHub

Decouple notification formatting from delivery:
1. Render notification content immutably using `templates.Renderer`.
2. Dispatch via NotifyHub gateway using `transport/notifyhub.Client`, or enqueue to outbox via `interfaces/outbox/notifications`.

A complete compilable example is maintained in [examples/quickstart/main.go](examples/quickstart/main.go).

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/assurrussa/gonotify/templates"
	"github.com/assurrussa/gonotify/transport"
	"github.com/assurrussa/gonotify/transport/notifyhub"
)

func main() {
	ctx := context.Background()

	// 1. Immutable template rendering with layout, auto-escaping, and caching
	renderer := templates.NewCached(os.DirFS("templates/email"))

	rendered, err := renderer.Render("welcome", map[string]any{
		"UserName": "Alice",
		"Year":     2026,
	})
	if err != nil {
		log.Fatal(err)
	}

	// 2. Transport client for NotifyHub gateway
	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    os.Getenv("NOTIFYHUB_URL"),
		ProjectKey: os.Getenv("NOTIFYHUB_PROJECT_KEY"),
	})
	if err != nil {
		log.Fatalf("client init failed: %v", err)
	}

	receipt, err := client.Submit(ctx, transport.Request{
		IdempotencyKey: "signup-user-123",
		Event:          "user.welcome",
		Email: &transport.EmailMessage{
			From:    "noreply@example.com",
			To:      []string{"alice@example.com"},
			Subject: rendered.Subject,
			HTML:    rendered.HTML,
			Text:    rendered.Text,
		},
	})
	if err != nil {
		log.Fatalf("delivery failed: %v", err)
	}

	log.Printf("submitted notification: id=%s duplicate=%t deliveries=%d",
		receipt.ID, receipt.Duplicate, len(receipt.Deliveries))
}
```

> [!NOTE]
`Config.BaseURL` is the gateway origin or deployment prefix (for example, `https://notify.example/prefix`), without `/v1/notifications`, user information, query parameters, or a fragment. Endpoint URLs are rejected with an actionable error instead of duplicating the API path.

> **Receipt Semantics**: A successful `Submit` (`err == nil`) confirms that NotifyHub has accepted and durably persisted the notification request. It does not mean the message has already been delivered to the recipient or accepted by upstream SMTP/Telegram providers. Delivery progression (`queued`, `sending`, `accepted`, `delivered`, `bounced`) is reflected in `Receipt.Deliveries` and can be queried via `ReceiptReader.Get(ctx, id)`.

> [!SECURITY]
> **Transport Security**: Plain HTTP is only permitted automatically for loopback addresses (`localhost`, `127.0.0.1`, `[::1]`). Remote HTTP connections are rejected by default to prevent sending `ProjectKey` bearer credentials unencrypted over the network. To permit remote HTTP on trusted private networks, set `AllowInsecureHTTP: true` in `notifyhub.Config`.

`ProjectKey` is used exactly as supplied. The constructor rejects surrounding whitespace,
embedded spaces and ASCII control bytes without including the secret in errors.

Every `Transport` must honor `Request.IdempotencyKey`: equivalent submissions with the
same key reuse one logical notification; detectable content conflicts return
`ErrIdempotencyConflict`. Keys are valid UTF-8, 8–200 bytes, with no surrounding whitespace or ASCII
control bytes. ASCII keys such as `registration:01J...` are recommended.

`Request.ExpiresAt` is an exclusive submission and delivery deadline; nil means no
expiration. NotifyHub uses whole Unix seconds (fractional seconds are truncated), and
the client returns `ErrExpired` before making an HTTP request once that wire deadline
is reached. The gateway must enforce expiration for already admitted notifications.

## Template Engine

The `templates` package parses templates from any `fs.FS` (`os.DirFS` or `embed.FS`).
HTML templates use `html/template` with automatic context-aware escaping for XSS protection,
while plain text and email subjects use `text/template`. Both engines are configured with
`missingkey=error` to fail fast on omitted variables in data maps.

A template directory can contain:

```text
templates/email/
├── layout.html.tmpl       # optional outer layout for HTML
├── layout.txt.tmpl        # optional outer layout for text
├── partials/              # optional *.html.tmpl / *.txt.tmpl
└── welcome/
    ├── html.tmpl          # HTML body
    ├── txt.tmpl           # plain text fallback
    └── subject.tmpl       # optional subject template
```

### Constructors

- `templates.New(root fs.FS) Renderer`: direct rendering on each call without caching.
- `templates.NewCached(root fs.FS) PreloadableRenderer`: thread-safe cached renderer for production.
- `templates.NewDir(dir string) Renderer`: convenience constructor for disk filesystem paths (`os.DirFS`).
- `templates.NewCachedDir(dir string) PreloadableRenderer`: cached renderer for disk filesystem paths (`os.DirFS`).

Call `.Preload()` on a `PreloadableRenderer` at application startup to eagerly parse and validate all templates:

Preloading walks nested message directories (for example `account/welcome`), parses
root layouts and partials, and rejects undefined template references before execution.
Runtime data validation still occurs during rendering. Only a missing optional file is
ignored; filesystem permission and I/O errors are returned to the caller.

```go
//go:embed templates/email/*
var emailFS embed.FS

renderer := templates.NewCached(emailFS)
if err := renderer.Preload(); err != nil {
	log.Fatalf("template preload failed: %v", err)
}
```

## Deferred Delivery with Outbox

Import the supported job package:

```go
import (
	notificationsjob "github.com/assurrussa/gonotify/interfaces/outbox/notifications"
	"github.com/assurrussa/gonotify/transport"
	"github.com/assurrussa/gonotify/transport/notifyhub"
)
```

### Producer Side

#### Recommended Helper: `notificationsjob.Put`

The package provides a safe producer helper `notificationsjob.Put` that validates the request, serializes it to schema v2, and stages it directly via `outbox.VersionedPutter` without requiring the host to remember job names or schema versions:

```go
req := transport.Request{
	IdempotencyKey: "signup-user-123",
	Event:          "user.welcome",
	Email: &transport.EmailMessage{
		From:    "noreply@example.com",
		To:      []string{"john@example.com"},
		Subject: rendered.Subject,
		HTML:    rendered.HTML,
		Text:    rendered.Text,
	},
}

jobID, err := notificationsjob.Put(ctx, outboxService, req, time.Now())
if err != nil {
	log.Fatalf("failed to stage outbox notification: %v", err)
}
```

`notificationsjob.Put` ensures retry-safe downstream delivery idempotency: the required `IdempotencyKey` guarantees that the delivery gateway will reject or deduplicate redundant delivery attempts upon worker retries. If your application also requires deduplication at the outbox storage layer across concurrent producers, stage jobs using `outboxService.PutVersionedUnique(...)` directly.

#### Manual Payload Serialization

You can also serialize the payload manually:

```go
payload, err := notificationsjob.MarshalPayload(notificationsjob.Payload{
	Request: req,
})
if err != nil {
	log.Fatal(err)
}

outboxService.PutVersioned(
	ctx,
	notificationsjob.JobName,       // "notifications_send"
	notificationsjob.SchemaVersion, // 2
	payload,
	time.Now(),
)
```

`MarshalPayload` performs fail-fast structural validation on the request (validates that the idempotency key meets the `transport.ValidateIdempotencyKey` contract of 8 to 200 bytes without outer whitespace or ASCII control bytes, and that at least one destination payload is specified). Expiration is evaluated at worker execution time, so the payload remains serializable after its deadline.

### Consumer / Worker Side

Register the outbox worker job using the transport client:

```go
client, err := notifyhub.New(notifyhub.Config{
	BaseURL:    os.Getenv("NOTIFYHUB_URL"),
	ProjectKey: os.Getenv("NOTIFYHUB_PROJECT_KEY"),
})
if err != nil {
	log.Fatal(err)
}

job := notificationsjob.Must(notificationsjob.NewOptions(client))
// Register job with your outbox worker runner...
```

The job implements `outbox.VersionedJob` and automatically classifies transport errors:
- **Permanent failure** (`outbox.Permanent`): client validation errors (`400 ErrInvalidRequest`), idempotency conflicts (`409 ErrIdempotencyConflict`), oversized payloads (`413 ErrPayloadTooLarge`), and malformed payloads directly transition to DLQ.
- **Rate limiting / Quota** (`outbox.RetryAt`): `429 QuotaError` with `Retry-After` reschedules execution to the requested timestamp, capped at `ExpiresAt` when set.
- **Authentication failure** (`outbox.DeferAt`): `401 ErrUnauthorized` is postponed using `outbox.DeferAt` without consuming retry attempts, allowing operators to rotate credentials during configuration outages without exhausting job attempt budgets.
- **Transient failures**: `503 ErrTemporarilyUnavailable` and network transport errors use standard outbox retry policy.
- **Expiration**: a structurally valid request at or past `ExpiresAt`, or a transport returning `ErrExpired`, completes with `nil` (ack/drop), without retry or DLQ. Already expired tasks are not submitted. Errors from a submission that finishes after the deadline also complete normally. Invalid payloads still go to DLQ. The 401 defer timestamp is capped at `ExpiresAt`, so credential outages cannot keep expired tasks indefinitely pending once a worker runs them.

> [!NOTE]
> **Outbox Backend Requirement**: `Job.Handle` uses `outbox.DeferAt` for 401 Unauthorized errors to postpone retry attempts without burning the attempt budget. Use backend module versions that implement `DeferJobsRepository` and are compatible with outbox v0.15. PostgreSQL, MySQL, SQLite and Picodata backends are separate Go modules; upgrading the root outbox module does not upgrade them automatically. Verify the host's selected modules with `go list -m all` before upgrading workers.

## Dependency Injection (godi)

For applications using `godi`, `di.ModuleBootstrap()` registers providers for:
- `transport.Transport` via `di.ProvideNotifyHubClient` (key: `di.KeyNotifyHubClient`).
- `*notificationsjob.Job` via `di.ProvideOutboxJob` (key: `di.KeyNotificationsJob`).

## Development and Verification

Run commands from the repository root:

```bash
make check
make ci-check
make test-trimpath
make public-probe-test
make public-consumer-local
```

- [Project overview](docs/project-overview.md) — package map and runtime contracts.
- [Development](docs/development.md) — tooling, generation, and verification commands.
- [Public release readiness](docs/public-release.md) — anonymous consumers and publication gates.

## License

[MIT](LICENSE).
