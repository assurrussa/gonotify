# Development

## Prerequisites

Use the Go version and toolchain declared in `go.mod` (currently Go `1.27.0`
and `go1.27.1`). Do not downgrade them to accommodate a local sandbox.

Dependencies must resolve anonymously through the public Go module proxy.
`GH_PAT`, a global `GOPRIVATE` override, or a populated private module cache
must not be prerequisites for building the library. Access to a private source
checkout itself remains separate from dependency access. A missing or private
tag is a release blocker, not a reason to bypass checksum verification.

Formatting and linting expect these tools:

- `gofumpt`
- `gci`
- `golangci-lint`

## Main Commands

Full local gate:

```bash
make check
```

The full gate runs tidy, generate, format, vet, lint, tests, repeated race tests,
trimpath/race, HTML coverage generation, offline probe safeguards, and the
anonymous local consumer.
The existing preparation/formatting steps are mutating; review their changes.

Narrow commands:

```bash
make ci-check
go mod tidy -diff
go generate ./...
go fmt ./...
go vet ./...
go test ./...
go test -race -count=5 ./...
make test-trimpath
golangci-lint run -v --fix --timeout=5m ./...
go test -bench=. -benchmem ./...
make public-probe-test
make public-consumer-local
make public-consumer-published VERSION=<published-tag>
make publish-readiness
make release-readiness VERSION=<published-tag>
```

See [Public release readiness](public-release.md) for the exact meaning and
limitations of each gate. The anonymous probe uses fresh temporary caches and
never inherits credentials, a workspace, or local dependency overrides.

Ordinary Make targets use Go's and the linter's portable shared cache locations.
Set `GO_SHARED_CACHE_ROOT` to choose a shared build/module/lint root; explicit `GOCACHE`,
`GOMODCACHE` and `GOLANGCI_LINT_CACHE` values take precedence. In a sandbox,
obtain access to the shared cache rather than creating per-task caches.
Anonymous consumer probes keep their disposable isolation and cleanup.

## Generation

There are currently no generated Go files or generator dependencies. `Options`
is maintained manually, preserving `NewOptions`, `OptOptionsSetter` and
`(*Options).Validate`. Tests use small hand-written transports. `go generate ./...`
remains in the local gate as a no-op for compatibility with the development workflow.
Upstream outbox test or tool requirements can still appear in `go list -m all`
(including gomock and validator). This does not mean gonotify's runtime imports them.

## Import Policy

Use this checker when validating a host repository:

```bash
go run ./cmd/importpolicy --repo-root ../site --consumers backend,goadmin,goauth,fixtures
```

The checker scans selected host roots for `github.com/assurrussa/gonotify`
imports and fails on packages not listed in
`reference/externalconsumer/packages.go`.

When expanding the stable consumer surface, update:

- `reference/externalconsumer/packages.go`
- `reference/externalconsumer/imports.go`
- `README.md`
- `docs/project-overview.md`
- `AGENTS.md`
- shared wiki page `platforms/gonotify.md` after verification

## CI

GitHub Actions runs a single `build` job for non-draft pull requests targeting
`master`, including transition to ready for review, and manual dispatch. Draft
jobs are skipped; converting to draft also cancels an older active run. There
are no push-trigger duplicates, matrices, or uploaded artifacts. Superseded runs
are cancelled and the job has a ten-minute timeout.

The workflow uses the toolchain from `go.mod`, does not persist checkout
credentials, and downloads dependencies through the public proxy without
`GH_PAT`. It calls only `make ci-check`: `go mod tidy -diff`, offline shell
safeguards and `go test -mod=readonly -count=1 ./...`. Lint, repeated race tests,
trimpath, generation, coverage and anonymous consumer checks remain mandatory
locally through `make publish-readiness`.

Merge requires a successful executed CI job as well as the full local gate.
An account billing/limit failure or a skipped draft job is not passing CI.

## Test Focus Areas

Keep these areas covered when changing behavior:

- template rendering, auto-escaping, layouts, and `missingkey=error` in `templates/templates_test.go`
- transport gateway client, protocol validation, redirect security, and error mapping in `transport/notifyhub/client_test.go`
- outbox payload handling, schema version 2, and error classifications in `interfaces/outbox/notifications/job_test.go` and `payload_test.go`
- stable import manifest in `reference/externalconsumer/manifest_test.go`
- external runtime/payload smoke tests in `reference/externalconsumer/consumer_test.go`
- offline isolation/failure tests in `scripts/test-public-consumer.sh`

## Documentation Discipline

Keep durable project contracts in `docs/`. Use `implementation-notes.md` for
task-specific decisions, tradeoffs, verification attempts, and blockers.
