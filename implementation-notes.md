# Implementation Notes

## 2026-10-01 PR 3 Recursive Preload and Independent Review

Goal: resolve PR comments 4154781256 (recursive preload) and 4154781246
(portable cache defaults), then review the notification boundaries again.
Review lens: Go/backend, filesystem error handling, identity preservation and
API compatibility. Starting HEAD: 311a5a4; clean checkout; original hosted CI passed.

Confirmed failures reproduced before changes:
- Nested template syntax/undefined references and unused bad partials were missed
  by Preload. Permission failures were silently treated as optional file absence.
- A missing required partial could make RenderSubject return an empty success.
- Invalid UTF-8 keys passed validation but JSON serialization could change them.
- Passing a typed nil VersionedPutter to Put panicked.

Decisions:
- Walk message directories recursively, excluding the root partials subtree.
  Load flat global partial sets and root layouts; statically validate references
  before caching, without executing with fake data or changing public signatures.
- Propagate filesystem failures; optional subject absence is checked against its
  own file, rather than any nested ErrNotExist.
- Preserve key identity with UTF-8 validation and reject typed nil interface inputs.
- Public Make defaults use native caches; local checks explicitly supply the
  shared cache root required by the parent workspace instructions.
- Update local/shared contract docs after validation, then commit/push the fix
  to the existing PR. No merge or tag publication is requested.

Evidence and validation:
- Regression tests first reproduced nested preload omissions, hidden filesystem
  errors, optional-subject misclassification, UTF-8 identity loss and typed-nil panic.
- Affected package tests passed after the fixes. Final make check passed: vet,
  lint (0 issues), all tests, race -count=5, trimpath/race, coverage, offline probe
  safeguards and anonymous local consumer (including nested cached rendering).
- Cache behavior probes passed with HOME unset, a custom shared root, and explicit
  per-cache overrides. git diff --check and the public-doc machine-path scan passed.
- gopls could not resolve package metadata for the new validation file; compiler,
  lint, tests and race checks validate that file. No dependency changes were made.
- Final diff reviewed for public signatures, filesystem handling and cache races.
  Local/shared contract docs are synchronized; hosted CI for the pushed fix is
  checked separately. Local gates are not publication or delivery evidence.
- The portable Make decision above supersedes the earlier shared-default decision.

## 2026-10-01 Remaining Review Findings and Additional Code Review

Goal: resolve the review of `refactor/transport-contract` at `3dca7c8` and fix
additional confirmed correctness issues. Review lens: Go/backend, delivery
reliability and public API compatibility. Starting worktree was clean.

Current decisions (supersede earlier generator and backend-version statements):
- Reject malformed ProjectKey values without trimming or revealing the secret.
- Root idempotency validator is a function; keys are 8–200 bytes and reject all
  ASCII controls. Every transport must honor idempotency and expiration.
- Valid expired jobs complete with nil (ack/drop), without DLQ. Invalid payloads
  remain permanent. 401/429 timestamps stop at expiration; failures completing
  after expiration also terminate. ErrExpired distinguishes expiration from
  validation errors, including NotifyHub's truncated Unix-second wire deadline.
- Replace generated options while preserving exported signatures and typed-nil
  rejection. Remove unsupported, unused transport/mocks and direct generator/tool
  requirements. Upstream module graphs still select gomock/validator through outbox; retained direct runtime versions are unchanged, while removed indirect
  overrides allow upstream-selected transitive versions.
- Fix receipt ID double escaping and Retry-After duration overflow. Independently
  versioned outbox backends must implement DeferJobsRepository; the root version
  alone does not prove backend compatibility.
- Make uses shared cache defaults and respects explicit environment overrides.

Evidence and validation:
- Initial targeted package tests passed at the original HEAD.
- New regression tests cover malformed secrets/controls, UTF-8 byte boundaries,
  expiration boundaries and deferred cycles using synctest, typed nil options,
  escaped receipt IDs, overflow and root external-consumer contracts.
- go mod tidy removed gonotify's generator/tool requirements. go list -m all and
  go mod graph distinguish the remaining upstream dependencies from runtime imports.
- Targeted transport, job and externalconsumer tests passed after implementation.
- Final make check passed: vet, lint (0 issues), tests, race -count=5, trimpath,
  coverage, offline probe safeguards and anonymous local external consumer.
- go mod tidy -diff and git diff --check passed. Focused gopls production/job
  diagnostics passed; gopls cannot resolve metadata for the new NotifyHub test file,
  which the full compiler/test/race/lint gates do check. go_vulncheck returned no findings.
- Final diff reviewed; no unrelated source changes. Documentation checked for
  machine-local paths and current contract consistency.
- Shared platform contract and sanitized local-branch snapshot updated;
  bash scripts/lint-streams.sh passed (pre-existing freshness warnings).
  Publication, deployment and hosted CI are outside this verification.

## 2026-10-01 v0.4.0 Review Hardening: Unified Idempotency & Contract Guarantees

Goal: resolve remaining findings from PR pre-merge review (single-source idempotency validation, formal transport contract invariants, outbox backend documentation, and P3 cleanup).

Key decisions and changes:
- **Unified Single-Source Idempotency Validation (P2)**:
  - Extracted `transport.ValidateIdempotencyKey(key string) error` as the canonical source of truth for both `interfaces/outbox/notifications` and `transport/notifyhub`.
  - Defined `MinIdempotencyKeyLength = 8` and `MaxIdempotencyKeyLength = 200` in package `transport`, making key length part of the public `gonotify` transport contract.
  - Enforced that idempotency keys must not contain leading or trailing whitespace (`key != strings.TrimSpace(key)`). This eliminates divergence where `TrimSpace` at producer time previously hid length violations from the transport layer.
  - Re-exported constants and `ValidateIdempotencyKey` in the root `gonotify` package.
- **Formalized Generic `Transport` Contract Semantics (P2)**:
  - Augmented GoDoc on `transport.Transport` to formally specify:
    1. Durable acceptance semantics: `Submit` requires durable gateway persistence; raw fire-and-forget transports (e.g. direct unbuffered SMTP/Telegram) do not satisfy this interface.
    2. Downstream idempotency: implementations used for durable/retryable delivery MUST honor `Request.IdempotencyKey`, preventing duplicate logical deliveries on retry.
- **Outbox Backend Compatibility (`DeferJobsRepository`) Documentation (P2)**:
  - Documented that `Job.Handle` returning `outbox.DeferAt` on 401 Unauthorized requires an outbox backend implementing `DeferJobsRepository` (supported by all built-in backends in `outbox v0.15.0+`).
  - Added explicit notes to `README.md`, `docs/README.ru.md`, `docs/project-overview.md`, and `CHANGELOG.md` migration guide.
- **Outbox Downstream Idempotency vs Enqueue Deduplication Clarified (P2)**:
  - Clarified across documentation that `notificationsjob.Put` ensures retry-safe downstream delivery idempotency (resilient to worker retries), while storage-level duplicate enqueue protection across multiple producers requires `PutVersionedUnique`.
- **P3 Documentation & Polish Cleanup**:
  - Corrected `docs/project-overview.md` package reference from `notifications.Put` to `notificationsjob.Put`.
  - Fixed sentence completion in `AGENTS.md` Outbox Contract section.
  - Moved release notes in `CHANGELOG.md` under `## Unreleased` until formal release tagging.
  - Adjusted README "Production-ready HTTP client adapter" wording to "NotifyHub HTTP client adapter".

## 2026-10-01 v0.4.0 Final Pre-Merge Hardening & Review Resolution

Goal: resolve remaining review blockers for `v0.4.0` release readiness.

Key decisions and changes:
- **NotifyHub Client Remote Insecure HTTP Blocking (P1 Security)**:
  - Added `AllowInsecureHTTP bool` to `notifyhub.Config`.
  - Enforced that plain `http://` is only permitted automatically for loopback addresses (`localhost`, `127.0.0.0/8`, `::1`).
  - Remote plain HTTP requires explicit opt-in (`AllowInsecureHTTP: true`), preventing accidental transmission of the bearer `ProjectKey` in plaintext across untrusted networks.
- **401 Unauthorized Outbox Semantics via DeferAt (P1 Correctness)**:
  - Upgraded `github.com/assurrussa/outbox` to `v0.15.0`.
  - Replaced `outbox.RetryAt` with `outbox.DeferAt` for `ErrUnauthorized` in `Job.Handle()`.
  - Preserves the invariant that credential rotation outages postpone the job without exhausting worker attempt budgets (`MaxAttempts = 30`), preventing false DLQ transitions.
- **Unified Receipt Decoding & Enhanced Validation (P2)**:
  - Extracted shared `decodeReceipt(respBytes)` function used by both `Submit` and `Get`.
  - Guaranteed `Get` validates delivery items and IDs as defensively as `Submit`.
  - Bounded response reads to `maxResponseBytes + 1` to reject truncated responses exceeding 1 MiB with `ErrInvalidResponse`.
  - Made `truncateDiagnostic` rune-safe to avoid slicing multi-byte UTF-8 sequences.
- **Outbox Safe Producer Helper & Early Validation (P2)**:
  - Added `notificationsjob.Put(ctx, putter, req, availableAt)` helper to automate early structural validation and schema v2 `PutVersioned` staging.
  - Added `ValidateRequest` and `ValidatePayload` enforcing idempotency key length (8..200), control character absence, and destination presence.
  - `MarshalPayload` validates before serialization so invalid payloads fail fast before staging in database transactions.
  - Exported `type JobID = types.JobID`.
- **NotifyHub Client Idempotency Key Validation (P2)**:
  - `client.Submit` validates `8 <= len(IdempotencyKey) <= 200` and checks for CR/LF/NUL before network transmission, returning `ErrInvalidRequest` immediately.
- **Legacy Source Tree Resolution Cleanup (P2)**:
  - Removed legacy `findTemplateDir`, `resolveDir`, `newRootFS`, `runtime.Caller(0)`, and `GOPATH` ancestor searching from `templates`.
  - Simplified `templates.NewDir` and `templates.NewCachedDir` to wrap `os.DirFS(dir)` cleanly.
- **Receipt Persistence vs Delivery Semantics (P2)**:
  - Documented in code and documentation that a successful `Submit` confirms gateway durable persistence, not downstream delivery or SMTP provider acceptance.
  - Updated test fixture in `TestSubmit_Success_202Accepted` from `"accepted"` to `"queued"`.
- **Tooling & Dependency Cleanup (P3)**:
  - Declared `options-gen v0.58.0` and `mockgen v0.6.0` as tool dependencies in `go.mod` using Go 1.24+ `tool` directives.
  - Updated `//go:generate` directives to use `go tool options-gen` and `go tool mockgen`.
  - Removed empty `interfaces/outbox/notifications/mocks` generated mock and directive.
  - Cleaned up `.golangci.yml` depguard allowlist by removing unused legacy packages (`gologger`, `fiber/v3`, `go-json`).

## 2026-10-01 v0.4.0 Transport Contract, Outbox Schema v2 & Release Hardening

Goal: address code review blockers for `refactor/transport-contract` to prepare for `v0.4.0` release.

Decisions and changes implemented:
- **Outbox Schema Version 2**: `interfaces/outbox/notifications.Job` implements `outbox.VersionedJob`
  with `SchemaVersion = 2`. Removed duplicate `Payload.IdempotencyKey` in favor of single
  source of truth in `Payload.Request.IdempotencyKey`. Enforced non-empty idempotency key for durable retries.
- **Outbox Typed Error Semantics**: `Job.Handle` maps `ErrInvalidRequest` (400), `ErrIdempotencyConflict` (409),
  `ErrPayloadTooLarge` (413), and malformed payloads to `outbox.Permanent(err)` (DLQ). `QuotaError` (429)
  with `RetryAfter > 0` reschedules via `outbox.RetryAt`. `ErrUnauthorized` (401) is given a 1-minute
  backoff delay via `RetryAt` to avoid burning attempt budgets. Network and 503 errors perform standard retry.
- **NotifyHub Client Contract**:
  - Enforced `http.ErrUseLastResponse` on HTTPClient to prevent credential leakage across redirects.
  - Validated BaseURL scheme (`http`/`https`) and host.
  - Supported URL subpaths correctly when composing endpoints.
  - Added per-operation timeout via `context.WithTimeout(ctx, c.timeout)`.
  - Added response validation to prevent returning empty `Receipt` with `nil` error (`ErrInvalidResponse`).
  - Unified error mapping between `Submit()` and `Get()`, truncating diagnostic message bodies to 256 bytes.
  - Defined `DeliveryStatus` typed constants matching NotifyHub gateway specifications.
- **Template Portability & Safety**:
  - Replaced `filepath.Join` with `path.Join` for all `fs.FS` path operations to prevent backslash issues on Windows.
  - Configured `missingkey=error` on all HTML and text template instances to fail fast on missing fields.
- **Dependency & Code Cleanup**:
  - Simplified `interfaces/outbox/notifications.Options` to require only `transport.Transport`, removing unused `gologger`.
  - Simplified `di.ProvideOutboxJob`.
  - Added optional transport capability interfaces `ReceiptReader` and `HealthChecker`.
- **Release Documentation & Quickstart**:
  - Synchronized `README.md`, `docs/README.ru.md`, `docs/project-overview.md`, `docs/development.md`, and `docs/public-release.md`.
  - Documented `v0.4.0` breaking release and migration guide in `CHANGELOG.md`.
  - Created compilable, verified `examples/quickstart/main.go`.

## 2026-09-29 Bilingual README

Goal: use English for the root README and keep a Russian contract guide in
`docs/README.ru.md`, with navigation in both directions and links to canonical
code examples.

Current constraints and decisions:

- PR #1 is merged as `7b742ed`; this docs branch starts from that master.
- The owner deferred GitHub CI and history/secret checks. They are outside this
  documentation task and are not claimed as completed.
- Keep runtime code, dependencies, supported imports and CI configuration
  unchanged. No publication or provider delivery is part of this task.
- Correct both languages against the current API: service constructor context
  and logger, notification argument order, explicit host configuration loading,
  delivery results, Fiber v3, and real development commands.
- Remove the old nonexistent package tree/test example and unsupported Gin
  claim. Keep Russian explanations aligned with the documented contracts.
- PR #2 review corrected two initial choices: restore the full canonical
  `backend,goadmin,goauth,fixtures` import-policy roots in both guides, and keep
  shared code/configuration examples only in the English README. The initial
  duplicated Russian examples are superseded by links to canonical sections.
  Narrowing application roots to avoid local caches can hide unsupported imports;
  cache handling must not redefine the documented consumer coverage.

Initial verification on `8f55d70` (before replacing duplicate examples):

- Passed 20 local link/anchor checks, English language placement, matching
  section counts and Markdown fence checks using the ignored local helper
  `tmp/verification/check-readmes.py`.
- All 14 Go blocks compiled in temporary consumer modules: complete quick
  starts and fragments wrapped in functions with their documented dependencies.
  Import-only blocks were checked together with their payload examples.
  Each language passed `GOTOOLCHAIN=local GOWORK=off GOPROXY=off
  go test -mod=readonly -count=1 ./...` with a writable temporary build cache.
  Temporary module paths are recorded in
  `tmp/verification/readme-examples-work.txt` (ignored).
- `git diff --check` passed; edited public docs contain no machine-local paths.
- Delivery functions were not executed, no hosted job was dispatched, and
  unchanged runtime gates were not rerun for this docs-only change.

Current review validation:

- `python3 tmp/verification/check-pr2-review.py` passed 32 local link/anchor
  checks, fences, canonical consumer roots and language placement. The Russian
  guide has no duplicate Go/configuration blocks; its only code block is the
  required canonical checker command.
- All seven English Go blocks are byte-for-byte unchanged from `8f55d70`;
  reuse their previous compilation evidence. Runtime code, dependencies and
  workflow configuration also remain unchanged.
- The documented checker command, with a temporary host path and Go's readonly
  module flag, accepted supported imports and detected unsupported imports in
  all four roots, including `backend/other`. Log:
  `tmp/verification/pr2-import-roots.log` (ignored). Real hosts were not changed.
- `git diff --check` passed. No manual GitHub dispatch or history/secret check
  was performed; the owner's deferred checks remain outside this task.

## 2026-09-28 PR #1 Review Fixes

Goal: finish the existing public-dependency PR with four confirmed fixes and
bounded CI. Keep the supported packages, notification delivery and outbox
payload contracts intact. Repository visibility and tags remain separate.

Accepted decisions:

- Empty template paths require an available source directory; missing or
  non-directory source paths use the existing cwd fallback. Non-empty paths
  preserve ancestor lookup and the GOPATH boundary.
- Path regression tests compare filesystem identity and cover symlink cwd.
- Anonymous probes clean their isolated module cache through Go, preserve the
  original failure status, and emit success only after cleanup succeeds.
- One minimal CI job runs tidy-diff, offline shell safeguards and ordinary
  tests. Non-draft PRs and manual dispatch are the only runner triggers;
  superseded runs are cancelled and each job is bounded to ten minutes.
- Full lint/race/trimpath/generation/coverage/anonymous-consumer checks remain
  local. A successful executed minimal GitHub job remains required before merge.
- Ran real module tidy on Go 1.27.1; it updates kr/pretty checksum entries to
  the selected v0.3.0 without changing direct dependency versions or toolchain.
- Committed generator/formatter output: the options-gen v0.55.6 header and
  local import grouping in two generated files. Signatures and bodies did not
  change.

Current verification (earlier sections retain historical attempts):

- Bash syntax and `make public-probe-test` pass, including read-only cache,
  download/test/cleanup failures and preservation of the original exit status.
- Gopls reports no parse/build errors in the edited Go files.
- Focused template race tests, `make ci-check`, non-fixing repository lint
  (zero issues), and actionlint v1.7.12 pass on Go 1.27.1.
- Independent review of the complete PR candidate found no actionable defects;
  it also checked focused path regressions, shell guards and workflow syntax.
  A separate follow-up review confirmed that the generated diff changes only
  comments and import grouping.
- `GOTOOLCHAIN=local GOWORK=off make publish-readiness` passed with writable
  temporary build/lint caches on source commit `e0cbc4b`: tidy, generation,
  formatting, vet, lint (zero issues), unit tests, race tests repeated five
  times, trimpath/race, coverage, offline safeguards, and the real anonymous
  consumer. The final tracked-file diff check passed. The first run passed all
  execution checks but failed that final check until generated output was
  committed. Log: `tmp/verification/publish-readiness-final.log` (ignored).
- Isolated host comparisons passed against both base
  `f26b5e03bf32e64805b8a98a19c9e83c11ac2141` and candidate source:
  goadmin `4152e0a490e786bd25e1623477b6ab41f05c79f0` and site
  `4614ee60e0839c4fb25f5a020301721844ba5a36`. Hosts were copied from Git HEAD;
  site's unrelated uncommitted auth changes were excluded and preserved.
  Candidate checks were repeated after generation. Each temporary module
  replaced only gonotify, used `GOWORK=off`, `GOTOOLCHAIN=local`, `GOPROXY=off`,
  and ran `go test -mod=readonly -count=1` with these package sets:
  - goadmin: `./host ./infrastructure/notify/... ./outbox/notifications`
  - site/backend: `./internal/app ./internal/infrastructure/services/notify/... ./internal/auth`
  Logs: `tmp/verification/host-compatibility/` (ignored). These checks prove
  checkout compatibility, not compatibility of an unpublished tag or live
  SMTP/Telegram delivery.
- GitHub previously refused to start CI due to account billing/spending limits;
  the PR stays draft. A skipped draft job cannot satisfy the successful-CI
  criterion. No tag, visibility change, merge or deployment was performed.
- Host import-policy baseline: scan site source roots instead of its local
  module cache. Goadmin's existing testsupport imports gonotify/mocks, outside
  the stable package manifest; this PR does not expand that manifest.

## 2026-09-28 Public Dependency Preparation

Task: prepare gonotify for standalone public consumption in a new branch,
without changing repository visibility, publishing a tag, or modifying hosts.

Decisions:

- Removed the two identified goshared uses: template source-path lookup and the
  outbox test fixture. Kept public notification and outbox interfaces unchanged.
- Preserved the legacy source-ancestor search and GOPATH boundary using local
  standard-library code. Empty paths use `.` only when source lookup cannot
  resolve a directory, including trimpath builds. Production needs an explicit
  template directory; source lookup is a compatibility aid.
- Kept the existing pinned Go/toolchain and all other dependency versions. Only
  the goshared requirement and its two checksum lines were removed; further
  tidy changes require a real project-toolchain run.
- Added distinct local-source and exact-published-tag anonymous consumers, with
  fresh caches, disabled inherited authentication/workspaces, public proxy only,
  and exact selected-version/replacement validation. No dependency overrides
  are accepted in published mode.
- Removed CI's private credentials requirement and documented the gates. The
  existing mutating make check behavior is preserved, not silently redesigned.
- Did not change shared wiki pages: no shared wiki checkout was available in
  this execution. Local docs describe the maintained contract.

Verification during implementation:

- Passed five repeated race runs of the modified template production file and
  the new path tests as an isolated standard-library package:
  `GO111MODULE=off GOTOOLCHAIN=local go test -race -count=5 embed.go path_internal_test.go`.
- Passed the same isolated tests with `-trimpath -race -count=1`, and `go vet`
  against those explicit files. The local compiler was Go 1.23.2; these narrow
  checks are not a claim that the full Go 1.27 project suite passed.
- Passed gofmt checks on the changed Go files and bash syntax checks on both
  scripts. Passed `make public-probe-test`: these are offline fake-Go safeguard
  tests, not anonymous network builds.
- Attempted `make public-consumer-local`: failed before package tests because
  the project requires Go 1.27.0 and the available local toolchain is 1.23.2.
  Direct repository/network access from the execution container also failed DNS
  resolution. The source was read and changes were written through the GitHub
  connector, not through a successful local git clone.
- Full module tidy, generation, lint, repository tests, real consumer builds,
  provider integration, and host regressions remain required before release.
  No published-tag check or repository-history secret review was completed.

Next: obtain passing project-toolchain CI and real host checks; review history
and publication suitability; explicitly approve public visibility and a new
immutable tag; pass the exact anonymous published check; update goadmin in a
separate change. See `docs/public-release.md`.

## 2026-06-04 Project Initialization

Task: analyze the repository, fill `AGENTS.md`, and create focused project
documentation when useful.

Decisions:

- Kept `AGENTS.md` as the agent entrypoint and moved detailed project facts to
  `docs/project-overview.md` and `docs/development.md`.
- Preserved the shared wiki routing instructions, but made local source order
  and stable consumer boundaries explicit.
- Treated `reference/externalconsumer/packages.go` as the source of truth for
  host-safe imports. Although `email`, `telegram`, `base`, `init`, and `shared`
  are physical Go packages, they are not documented as stable external consumer
  packages until the manifest is intentionally expanded.
- Documented `shared/builder` as useful but not stable for external consumers
  because it is absent from `reference/externalconsumer`.
- Did not update the shared wiki: the current `platforms/gonotify.md` and
  `platforms/outbox.md` pages already align with the verified local stable
  surface and outbox dependency.

Verification notes:

- `go list ./...` with default caches failed because the sandbox could not write
  to the host's default Go build cache.
- Retried with local caches:
  `GOCACHE=$PWD/tmp/gocache GOMODCACHE=$PWD/tmp/gomodcache go list ./...`.
  This listed repository packages but failed dependency resolution at
  `github.com/assurrussa/goshared v1.0.0` with `unknown revision v1.0.0`.
- Retried tests with local caches:
  `GOCACHE=$PWD/tmp/gocache GOMODCACHE=$PWD/tmp/gomodcache go test ./...`.
  The command failed at the same private dependency resolution boundary before
  package tests could run.
- Because this was a docs-only initialization and dependency resolution is
  blocked by the private module/tag state, no code behavior was changed.

## 2026-06-04 Lint Release Prep

Task: fix reported `goconst` lint errors, then commit, push, and tag only if
`make` passes.

Decisions:

- Added constants for repeated test/template strings instead of suppressing
  `goconst`.
- Left pre-existing worktree changes in `.golangci.yml`, `go.mod`, `go.sum`,
  and `telegram/service.go` intact. They were present before this lint fix and
  appear related to the current `make` baseline, including private dependency
  resolution through `goshared v1.0.2`.

Verification:

- Initial sandboxed `make` failed at `go mod tidy` because Go could not write to
  the host's default Go build cache.
- Escalated `make` first reached lint and found one additional `goconst`
  occurrence for `"Тест"` in template tests.
- Final escalated `make` passed fully: tidy, generate, formatting, vet,
  golangci-lint with 0 issues, `go test ./...`, `go test -race -count=5 ./...`,
  and coverage HTML generation.

## 2026-09-30: Shared Go cache defaults

Ordinary local Go build/test/lint commands reuse shared caches outside checkout
and worktree. `GO_SHARED_CACHE_ROOT` and individual cache overrides remain
configurable; intentional disposable consumer/release caches retain isolation.
Verified cache defaults, alternate root and explicit build-cache override with
Make/Task environment probes; YAML graphs, shell syntax and diff checks passed.

## 2026-10-01: Branch refactor/transport-contract (Templates & NotifyHub Transport)

Task: begin architectural transition of `gonotify` towards a lightweight
`templates + transport contract` client:
1. Created branch `refactor/transport-contract`.
2. Extracted standalone `templates` package:
   - Returns immutable `RenderedContent{Subject, HTML, Text}` without mutating input notifications.
   - HTML templates parse with `html/template` (context-aware auto-escaping for XSS defense), while text and subject templates parse with `text/template`.
   - Direct support for `fs.FS` (`embed.FS` and `os.DirFS`), with both direct and thread-safe cached/preloadable renderers.
   - Comprehensive test suite covering layouts, partials, embed.FS, escaping, concurrency, and error handling.
3. Defined core `transport` interface and data models:
   - `Transport` interface with `Submit(ctx, Request) (Receipt, error)`.
   - Strongly-typed `Request`, `EmailMessage`, `TelegramMessage`, and `Receipt`.
   - Typed error hierarchy (`QuotaError` with `RetryAfter`, `RequestError`, `ErrInvalidRequest`, `ErrUnauthorized`, `ErrIdempotencyConflict`, `ErrPayloadTooLarge`, `ErrQuotaExceeded`, `ErrTemporarilyUnavailable`, `ErrNotFound`).
4. Implemented `transport/notifyhub` client:
   - Targets the NotifyHub HTTP API (`POST /v1/notifications`, `GET /v1/notifications/{id}`, `GET /healthz`).
   - Supports Bearer token authorization, `Idempotency-Key` header, RFC 7231 `Retry-After` parsing, and bounded response body reading (`io.LimitReader`).
   - Full test coverage with `httptest.Server`.
5. Updated `reference/externalconsumer/packages.go` and `imports.go` to include the new packages in the stable public consumer surface.
6. Adapted Outbox contract (`interfaces/outbox/notifications`):
   - Added pre-rendered `transport.Request` and `IdempotencyKey` to `Payload` while retaining legacy compatibility.
   - Fixed silent failure bug in `Job.Handle`: delivery results with `Success == false` now correctly return an error for proper outbox worker retries.
   - Fixed recipient name loss: single recipients now preserve `Payload.Name` so templates receive `.UserName`.
   - Added `WithTransport`, `NewTransportJob`, and `MustTransport` constructors to execute outbox jobs directly via `transport.Transport`.
7. Formally deprecated legacy direct delivery mechanisms:
   - `email.Service` (direct SMTP sender and pool).
   - `email.AntiSpamService` (in-library keyword filter).
   - `telegram.Service` (direct Telegram client).
   - `middleware.go` (Fiber error middleware).
   - `NotificationService`, `NotificationManager`, and `NotificationContract`.

## 2026-10-01: Complete Removal of Legacy Direct Delivery Subsystems

Decision: accelerate transition by completely removing obsolete direct-delivery components
instead of keeping a prolonged deprecation cycle, as consumers are migrating directly to NotifyHub:
1. Removed legacy packages and files: `email/`, `telegram/`, `base/`, `init/`, `shared/`, `middleware.go`, `service.go`, `service_test.go`, `manager.go`, `config.go`, `helpers.go`, and `mocks/types_mock.gen.go`.
2. Cleaned `go.mod`: removed `gofiber/fiber/v3`, `fasthttp`, `bytebufferpool`, and unused indirect dependencies.
3. Simplified root package `gonotify`: re-exports primary types and typed errors from `templates` and `transport`.
4. Modernized `interfaces/outbox/notifications`: `Job` only requires `transport.Transport` and `logger.Logger`; `Payload` encapsulates `transport.Request` with `IdempotencyKey`.
5. Modernized `di`: provides `ProvideNotifyHubClient` and `ProvideOutboxJob` through `godi`.
6. Updated `Makefile`: `test-trimpath` tests `./templates`.
7. Updated documentation and guidelines across `README.md`, `docs/README.ru.md`, `docs/project-overview.md`, `AGENTS.md`, and shared wiki `platforms/gonotify.md`.



## 2026-10-01 immutable release version correction

The earlier v0.4.0 planning references above are historical. GitHub inspection
confirmed that tag already identifies legacy commit 578712051c2712882861586206e9ba1050d0134c.
The transport API release is now planned as v0.5.0; no existing tag may move.
Updated active release and migration instructions, including the independently
versioned PostgreSQL backend and a producer-pause/v1-drain cutover. Publication
and history cleanup remain operator gates. This is a documentation-only change.
