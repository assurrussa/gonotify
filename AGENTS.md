# Repository Guidelines

## Source Order

Use `$project-context-router` for repository work that needs cross-project
context or the shared wiki.

Do not hard-code machine-local absolute paths in this public repository.

Local docs and code in this repository are the source of truth for commands,
public APIs, config keys, supported imports, runtime behavior, and release
gates. Read `AGENTS.md`, `README.md`, `docs/`, `reference/`, code, tests,
configs, and generated contracts before shared wiki pages.

Expose the shared wiki root through `AGENT_CONTEXT_ROOT` or let `$project-context-router` resolve it for the current session.

When shared context is needed, follow `streams/AGENTS.md` and its query route.
Reuse already loaded root rules, PII policy and glossary. Open the known hub
and only the topic relevant to the task:

- `streams/wiki/platforms/gonotify.md`

For integration work, open only the affected neighbour hub:

- `streams/wiki/platforms/outbox.md`

Use `streams/wiki/index.md` only to locate an unknown area or answer an overview
question. This is a task router, not a mandatory list of wiki pages.

If verified local docs or code conflict with the shared wiki, treat the wiki as
stale. When documentation upkeep covers public contracts or integration boundaries, update
the matching shared platform page with concise contract wording after local
verification. Do not copy whole README sections into shared wiki pages.

## Project Shape

`gonotify` is a Go notification library providing an immutable template engine
(`templates`), transport contracts (`transport`), a NotifyHub client adapter
(`transport/notifyhub`), `godi` dependency injection wiring (`di`), and outbox
job processing (`interfaces/outbox/notifications`).

Use `docs/project-overview.md` for the package map, runtime contracts, and
integration boundaries. Use `docs/development.md` for local commands,
generation, CI, and release-surface maintenance.

## Stable Consumer Surface

The external consumer contract is defined in
`reference/externalconsumer/packages.go` for host repositories:

- `github.com/assurrussa/gonotify`
- `github.com/assurrussa/gonotify/templates`
- `github.com/assurrussa/gonotify/transport`
- `github.com/assurrussa/gonotify/transport/notifyhub`
- `github.com/assurrussa/gonotify/di`
- `github.com/assurrussa/gonotify/interfaces/outbox/notifications`

Do not introduce host imports from `internal` or `cmd` unless the task
explicitly expands the consumer contract. If the stable surface changes, update
`reference/externalconsumer/packages.go`,
`reference/externalconsumer/imports.go`, README stable-surface text, local docs,
and the shared `platforms/gonotify.md` page.

Use the import policy checker against host repos:

```bash
go run ./cmd/importpolicy --repo-root ../site --consumers backend,goadmin,goauth,fixtures
```

## Runtime Contracts

`templates.Renderer` parses templates from an `fs.FS` (`os.DirFS` or `embed.FS`).
HTML templates use `html/template` with automatic contextual escaping for XSS protection,
while plain text and email subjects use `text/template`. Thread-safe caching is
provided by `templates.NewCached`, and startup preloading via `.Preload()`. Both
engines enforce `missingkey=error` to prevent delivering notifications with missing variables.

`transport.Transport` defines `Submit(ctx, Request) (Receipt, error)`.
`transport/notifyhub.New` creates an HTTP client for the NotifyHub delivery gateway
with bearer authorization, idempotency keys, redirect blocking, and RFC 7231 `Retry-After` support.
`Client.SendConfidentialEmail` is a separate synchronous, single-recipient operation
using that same connection, never the durable transport/outbox. Only explicit
provider acceptance succeeds; see `docs/confidential-email.md` for fail-closed
outcomes, stable retries, expiry precision, and the metadata-only gateway contract.

`di.ModuleBootstrap` wires `transport.Transport` and outbox `Job` through `godi`.

## Outbox Contract

The outbox job name is `notifications_send` with capability schema version `2`.
Payloads are JSON encoded through `interfaces/outbox/notifications.MarshalPayload` and
decoded by `Handle`. Payloads encapsulate pre-rendered transport.Request values with stable
idempotency keys.

Structurally valid expired requests complete without submission or DLQ. `ErrExpired`
also completes normally; 401/429 delays are capped at `Request.ExpiresAt`.
The job classifies other errors: permanent failures (`Permanent`) transition to DLQ,
quota limits (`RetryAt`) are delayed, authentication errors (`DeferAt`) are postponed
without consuming attempts, and transient failures retry.

## Development Commands

The default local gate is:

```bash
make check
```

Narrow checks:

```bash
go generate ./...
go fmt ./...
go vet ./...
go test ./...
go test -race -count=5 ./...
make ci-check
make test-trimpath
golangci-lint run -v --fix --timeout=5m ./...
make public-probe-test
make public-consumer-local
make public-consumer-published VERSION=<published-tag>
```

`make fmt` also expects `gofumpt` and `gci`. There are currently no generator
dependencies; `go generate ./...` is a no-op.

Dependencies must resolve without private credentials. CI uses the project
toolchain and public module proxy; do not restore `GH_PAT` URL rewrites or
private-module bypasses. The anonymous consumer uses fresh isolated caches
and distinguishes a local source check from a published-tag check. For ordinary
local/sandboxed checks, reuse shared caches outside checkout/worktree. Obtain
permitted shared-cache access when sandboxed; keep isolated consumer caches
disposable. Respect explicit cache overrides.

## Documentation Notes

Keep docs contract-focused and avoid copying README examples wholesale. For
large or interruptible work, keep `implementation-notes.md` updated with
non-obvious decisions, tradeoffs, verification commands, and blockers.
