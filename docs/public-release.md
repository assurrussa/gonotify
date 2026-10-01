# Public release readiness

## Scope

The planned `v0.5.0` release completes the transition of `gonotify` from a legacy direct delivery
runtime into a focused library: immutable template engine (`templates`), transport contract
(`transport`), NotifyHub gateway adapter (`transport/notifyhub`), and outbox job processing
(`interfaces/outbox/notifications`).

The existing `v0.4.0` tag identifies the legacy API at commit
`578712051c2712882861586206e9ba1050d0134c`; it does not contain the transport
packages. Preserve that immutable tag. The new API requires a fresh minor
version, planned as `v0.5.0`, after all publication and history-review gates pass.

Dependencies (`godi` and `outbox`) resolve anonymously through the public module proxy.
No private modules or replacement shared libraries are required.

Repository visibility and tags are operator decisions. Passing a local source
check does not mean the repository or a tag is publicly downloadable.

## Compatibility & Migration

- **Outbox Schema Version 2**: `interfaces/outbox/notifications.Job` implements `outbox.VersionedJob`
  with `SchemaVersion = 2`. Producers must write with `outboxService.PutVersioned(ctx, notificationsjob.JobName, notificationsjob.SchemaVersion, payload, availableAt)`.
  Pause legacy producers, drain all pending and running v1 tasks with the old
  workers, and resolve any v1 DLQ tasks that may be replayed before upgrading
  worker nodes to v0.5.0. Do not let old workers consume v2 payloads.
- **Templates**: Replaces old mutable notifications with `templates.Renderer` and `RenderedContent`.
  Both HTML and text templates enforce `missingkey=error` to prevent silent delivery of corrupt messages.
- **Transport**: Replaces direct SMTP/Telegram senders with `transport.Transport`.
- **NotifyHub Client**: Enforces redirect blocking (`http.ErrUseLastResponse`) to protect project keys,
  supports subpath prefixes, per-operation timeouts, and validates gateway responses.

### Host upgrade order

1. Resolve the exact host module graph. Select outbox root `v0.15.0` and
   independently versioned backends implementing `DeferJobsRepository`. For
   PostgreSQL, `github.com/assurrussa/outbox/backends/pgsql v0.15.0` declares
   root outbox `v0.15.0`; verify the implementation and host integration tests.
   A dependency such as gouploads `v0.10.0` selecting backend `v0.12.0` does not
   become compatible merely because gonotify raises the root module version.
2. Verify NotifyHub durable acceptance, idempotency, expiration and receipt
   behavior in a test environment using test recipients and credentials.
3. Migrate every consumer of removed legacy packages and test with the exact
   candidate sources. Local replacements prove source compatibility only.
4. After publication is approved, verify the new immutable gonotify tag with
   no replacements, then pin it in goadmin and the host. Complete the same
   published-version verification for every other library before host release.
5. At the separately approved runtime cutover, pause v1 producers and drain
   v1 work as described above. Stop old workers before enabling v2 workers
   and producers. Do not roll back workers while v2 tasks remain queued.

## Gates

Use the toolchain declared in `go.mod` (currently `go1.27.1`); the anonymous
probe deliberately uses `GOTOOLCHAIN=local`. It will fail rather than silently
choose another compiler. Bash, a C compiler for race tests, and public internet
access to the Go module proxy and checksum database are required.

```sh
make ci-check
make public-probe-test
make public-consumer-local
make publish-readiness
```

`ci-check` is the minimal non-mutating gate: module tidiness, offline probe
safeguards, and ordinary repository tests. It does not replace the full local
gate required before merge.

`public-probe-test` is an offline shell test with a fake Go executable. It tests
environment isolation, argument validation, exact-version checks, rejection of
unexpected replacements, read-only cache cleanup, and propagation of failures.
It is NOT a library build or evidence that any module is public.

`public-consumer-local` creates a temporary external module and a new HOME,
GOPATH, module cache, and build cache. It clears the inherited environment,
Go configuration/workspaces, Git configuration and credentials, and disables
Go authentication. Only `proxy.golang.org` is allowed; there is no direct VCS
fallback and checksum verification remains enabled. The maintained supported
import manifest and public-API smoke test are copied into the consumer.

Only gonotify itself is replaced with the checkout. The declared dependency
graph is downloaded before consumer tidy, then verified. The probe checks the
selected version and replacement set, and runs the external consumer's race
tests. This proves anonymous dependency resolution for that source candidate,
not publication of gonotify. It does not replace the repository test suite.
The temporary module cache is removed with `go clean -modcache` under the same
isolated environment. Cleanup preserves a prior probe error; a cleanup error
also fails the command. The final success message follows successful cleanup.

`publish-readiness` runs the full, mutating `make check` workflow, including
trimpath/race and anonymous consumer checks, and requires tracked files to remain
unchanged afterwards.
Review and commit generated/formatting changes before running it again.

After the repository is public and a new immutable tag is available:

```sh
make public-consumer-published VERSION=<published-tag>
make release-readiness VERSION=<published-tag>
```

The published check allows no replacements and requires Go to select exactly
the requested tag. It does not pin alternative versions of gonotify's own
dependencies. An inaccessible module, missing tag, checksum failure, unexpected
version, replacement, or failed test fails the check. Do not work around this
with credentials, disabled checksum checks, a warm cache, or retagging.

The GitHub workflow uses the project toolchain, does not persist checkout
credentials, and does not need a `GH_PAT` secret for dependencies. Reading a
private checkout itself still requires GitHub's authorized checkout token.
The workflow runs one ten-minute-bounded job calling `make ci-check` for
non-draft pull requests targeting `master`, or on manual dispatch. It cancels
superseded runs and has no push-trigger duplicate. Lint, repeated race tests,
trimpath, coverage, generation and anonymous consumer gates run locally.
Published-tag verification uses the local commands above. The workflow never
changes visibility, creates a tag, merges a PR, or deploys an application.

## Release sequence

1. Obtain a passing full local gate on the project toolchain, including
   `go mod tidy -diff`, and a successful minimal GitHub CI run before merge.
   Review tidy changes rather than guessing checksums. If GitHub cannot start
   or complete a run, the release sequence is blocked.
2. Complete the required source and history review while the repository remains
   private. Merge the approved release-preparation PR into `master` after human
   code review and passing checks. Repository visibility changes require
   separate operator approval.
3. Confirm the target version is still unused, then push an annotated tag `v0.5.0` matching the target version.
4. Verify publication with `make release-readiness VERSION=v0.5.0`.
