---
id: TASK-1
title: Extract and publish provider-neutral work-lease tool
status: In Progress
assignee:
  - '@codex-main'
created_date: '2026-07-13 19:25'
updated_date: '2026-07-13 21:47'
labels:
  - architecture
  - packaging
  - release
dependencies: []
priority: high
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
### Goal and execution status
Extract a standalone Python work-lease distribution from the existing coordination helper at `/Users/brett/.dotfiles/ai/.bin/backlog-claim`. Preserve the product intent: the reusable core coordinates opaque caller-supplied resources on one host with SQLite and per-resource file locks; provider key derivation and provider-side fencing stay outside the core. This item is **available now**: the Backlog.md provider status remains `To Do`, dependencies are empty, and no human decision or unfinished repository prerequisite prevents a start.

### Target areas and reference disposition
Existing files to edit:
- `mise.toml`: retain the existing Python 3.14, latest uv, pyright, and Backlog.md tools; add reproducible `sync`, `test`, `typecheck`, `build`, and release-install tasks.
- `.gitignore`: remove the existing `uv.lock` ignore so the generated lock is committed; add only generated virtualenv, build, coverage, and local lease-state paths.

New files/directories to create:
- `pyproject.toml`: PEP 621 uv-managed distribution metadata using the `hatchling` build backend, Python `>=3.14`, `worklease` console script, and locked development configuration.
- `src/worklease/`: import package, CLI, opaque lease model/store, SQLite migration setup, POSIX file-lock wrapper, guarded executor, and adapter protocols/modules.
- `tests/`: deterministic unit and subprocess tests for every acceptance behavior; keep provider commands fake and local.
- `README.md`: development/package commands, CLI schema, same-host guarantee, security/token handling, adapter boundary, and release-install usage.
- `.github/workflows/ci.yml` and `.github/workflows/release.yml`: CI and tagged-artifact automation.
- `scripts/install-release.sh`: checksum-verifying installer invoked by a mise task.
- `uv.lock`: generated and committed by `mise exec uv -- uv lock`; never hand-edit it.

Existing external reference, read-only: `/Users/brett/.dotfiles/ai/.bin/backlog-claim` and `/Users/brett/.dotfiles/ai/tests/test_backlog_claim.py` exist and are the behavioral source for extraction. Do not edit, copy changes back to, or make runtime imports from that helper. No missing implementation path remains unresolved.

### Resolved decisions and rationale
- Use distribution/import/CLI name `worklease`. The repository project name and task title already use work-lease terminology; one name avoids packaging and install ambiguity.
- Use `src/worklease` with a standard-library runtime and `hatchling` only as the build backend. The repository has no existing package layout or runtime dependency convention; this is the smallest isolated uv project.
- Treat `resource` as a non-empty opaque string at the public API boundary. Store and compare its exact value; do not split, normalize, hash for identity, interpret paths, or infer providers. The implementation may SHA-256 the exact resource bytes solely to create a safe internal lock filename, as the reference helper does; this is storage mechanics, not provider-specific identity.
- Use `WORKLEASE_HOME` when set, otherwise `${XDG_STATE_HOME:-~/.local/state}/worklease`, with SQLite `leases.sqlite3` and a `locks/` directory. Do not read or migrate the helper's `BACKLOG_CLAIM_HOME` database in this item; a later cutover item owns compatibility.
- Preserve the helper's safety semantics: SQLite WAL plus FULL synchronous mode, one non-blocking POSIX `flock` per resource, TTL strictly above zero and at most 3600 seconds, monotonic revisions, random bearer tokens, exact claim-id/token/revision checks, and idempotent operation receipts. Persist UTC timestamps; use an injectable clock in tests.
- Scope the bundled adapters to deterministic identity/capability policy. GitHub, Backlog.md, and Markdown may derive stable local resource keys; Linear and unknown providers use deterministic `local-coordination` identities. None may claim remote/provider-side fencing without a provider conditional-write implementation; generic execution always reports same-host coordination only.
- Make JSON the default for every command, including `--version`, and include `schemaVersion: 1`, `operation`, and `ok` in every JSON response. `worklease --version` returns the packaged version in that envelope; `worklease --format text --version` may return a bare version. Plaintext is explicit `--format text`; list and status never emit bearer tokens. Acquire/heartbeat retain the token in their owner response because callers need it for conditional mutations; there is no token-recovery flag for read-only commands.
- Use exit code 0 for success, 2 for lease/capability conflicts, 3 for idempotency or version/request mismatches, 64 for invalid CLI input, and the child process status for a started guarded command. Document that these codes are stable API.
- Support POSIX hosts (Linux/macOS) for file-lock coordination. Do not claim Windows or cross-host fencing; CI covers Linux and macOS and tests skip/diagnose unsupported platforms explicitly.
- Do not hard-code a GitHub owner/repository because this checkout has no git remote. Release workflows use the Actions `GITHUB_REPOSITORY` context; local `mise run install-release VERSION=vX.Y.Z` requires an explicit `WORKLEASE_REPOSITORY=owner/name`. This is an execution-time publication input, not a prerequisite to implement or test the package.

### Edge cases and failure behavior
- Concurrent acquisitions for one resource have exactly one winner; independent resources can proceed concurrently.
- Expiry permits a new claim with a new token and larger revision. The old claim cannot heartbeat, release, execute, or replace content after reclaim.
- Replaying an identical claim or operation request returns the recorded receipt without repeating side effects; reusing an ID with changed arguments fails deterministically. A released claim ID cannot be reused.
- Missing, blank, oversized, non-finite, or otherwise invalid TTLs fail before a state change. Blank release reasons fail before release.
- Guarded execution passes argv without a shell, captures stdout/stderr, renews while a long child runs, terminates the child when ownership changes, and never runs a stale or rejected request.
- Read-only list and status output omit bearer tokens in every format; tests assert tokens do not occur and that no token-recovery option exists.
- Provider adapters must reject unsupported provider commands or coordination-only attempts to claim fencing rather than silently executing them.

### Explicit non-goals
Do not modify or migrate `~/.dotfiles/ai/.bin/backlog-claim`; do not add provider SDK/network calls to the core; do not implement remote provider conditional writes, cross-host locks, Windows file-lock support, Backlog.md/GitHub/Linear workflow mutations, caller cutover, or a hosted service. Do not create a second backlog item from this refinement.

### Implementation snapshot
- Product intent: publish the reusable same-host lease engine and stable CLI first; migration of the existing dotfiles caller follows a separate release prerequisite.
- Evidence inspected: `mise.toml` declares Python 3.14/latest uv/pyright; Backlog.md CLI 1.48.0 enumerates only `TASK-1`; the reference helper and tests cover acquire/status/list/heartbeat/release, expiry/reclaim, stale ownership, idempotency, per-resource locks, guarded execution, provider capability gates, and token redaction for list. The repository has one initial commit and no git remote; release identity is therefore parameterized rather than guessed.
- Decisions above settle package naming, state location including XDG fallback, exact resource semantics versus internal lock hashing, platform guarantee, JSON/version contract, token handling, adapter responsibilities, and release identity without external product input.
- Pending verification: the implementation must run the targeted unittest suite, pyright, uv lock/build/install smoke checks, concurrent/crash/expiry/stale-token tests, and CI/release installer checks listed in the plan.
- Next action: implement `[T1] Package and CLI bootstrap`, then execute the numbered tasks in order; record each task's command and result in implementation progress.

### Task-to-criterion map
`[T1]` covers AC1. `[T2]` covers the state-machine portion of AC2. `[T3]` covers guarded execution and the remaining AC2 behavior. `[T4]` covers AC3 and AC4. `[T5]` covers AC5 and the CLI/documentation portion of AC3. `[T6]` covers AC6 and AC7. All criteria require their named tests and generated artifacts to pass.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 [T1] Package bootstrap: `mise exec uv -- uv sync --locked` installs the `worklease` distribution from `pyproject.toml`; default `mise exec uv -- uv run worklease --version` returns a JSON object containing the packaged version, while `--format text --version` returns the bare version; README and mise tasks document development, test, typecheck, build, and package-install commands using the existing Python 3.14/latest uv/pyright toolchain.
- [ ] #2 [T2, T3] Provider-neutral behavior: opaque resource strings are stored and compared exactly (with any hashing limited to internal lock filenames), and targeted tests prove one-winner concurrency, independent-resource concurrency, SQLite/file-lock serialization, TTL expiry and reclaim, crash recovery, heartbeat, release, monotonically increasing revisions, random token replacement, idempotent retries, operation/request mismatch rejection, stale-token/revision rejection, durable crash-during-exec `unknown-outcome` handling without automatic rerun, and guarded process behavior without provider imports.
- [ ] #3 [T4, T5] README and CLI output explicitly state the only built-in guarantee is same-host SQLite plus POSIX file-lock coordination, including the Markdown expected-hash replacement path; bundled adapters cannot claim remote/provider-side fencing unless they perform provider-side conditional checks, and generic execution reports local coordination rather than provider fencing.
- [ ] #4 [T4] Optional GitHub, Backlog.md, Markdown, and Linear adapter modules implement only provider-specific identity/capability policy behind protocols; Markdown source claims expose the core atomic expected-SHA-256 `replace-file` operation, Linear/unknown providers remain local-coordination, and tests prove generic execution cannot claim provider fencing.
- [ ] #5 [T5] Every command, including `--version`, emits schema-versioned JSON by default; `--format text` is explicit; list/status never expose bearer tokens in JSON or plaintext, and tests cover stable fields, exit codes, redaction, rejected token-recovery flags, malformed input, and child-command failures.
- [ ] #6 [T6] CI runs the targeted concurrency, crash/expiry, stale-ownership, installation, pyright, build, and release-validation checks on supported POSIX runners; a clean checkout can reproduce them through mise/uv without relying on the external helper.
- [ ] #7 [T6] Tagged `vX.Y.Z` releases publish wheel and sdist assets plus one-file PyInstaller executables for the supported POSIX Linux/macOS architecture matrix, with OS/architecture names and a SHA-256 `checksums.txt` covering every asset. The PyInstaller build preserves `worklease` package metadata so `worklease --version` works from the frozen executable. The tested `mise run install-release VERSION=vX.Y.Z` path downloads the exact matching native asset when available, verifies its checksum, installs and executes it, and falls back to the exact versioned wheel with checksum verification when no matching native asset is available; local runs require `WORKLEASE_REPOSITORY=owner/name`. Tests cover asset selection, checksum validation, fallback behavior, and the downloaded artifact's `worklease --version` smoke test.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. **[T1] Package and CLI bootstrap (AC1).** Create `pyproject.toml`, `src/worklease/__init__.py`, the initial `src/worklease/cli.py`, `README.md`, and `tests/test_package_smoke.py`; add only the required mise tasks, remove the `uv.lock` ignore, and commit generated `uv.lock`. Expose `worklease --version` from one package version with the default JSON envelope and explicit plaintext form, make `uv sync --locked` and an editable local CLI install reproducible, and document the exact `mise exec uv -- uv ...` commands. Verification: `mise exec uv -- uv lock`, `mise exec uv -- uv sync --locked`, `mise exec uv -- uv run worklease --version`, and `mise exec uv -- uv run worklease --format text --version`.
2. **[T2] Core lease lifecycle (AC2).** Implement `src/worklease/models.py`, `store.py`, `sqlite.py`, and `locking.py` around an opaque `str` resource. Add schema creation/migrations, WAL/FULL SQLite settings, per-resource non-blocking POSIX locks whose filenames hash exact resource bytes only for storage, acquire/status/list/heartbeat/release, bounded TTL, expiry/reclaim, crash-safe restart, token/revision CAS, operation receipts, and exact idempotency. Bundle `tests/test_store.py` and subprocess/thread contention tests with this behavior; no adapter imports. Verification: `mise exec uv -- uv run python -m unittest tests.test_store -v` plus the crash/reclaim scenarios.
3. **[T3] Guarded local operations (AC2).** Implement `src/worklease/execution.py` and `src/worklease/replacement.py`, wiring `exec` and `replace-file` into the CLI. `exec` must persist an operation intent before spawning an argv-only child, capture output, renew during long commands, terminate the process group on ownership loss where possible, persist a receipt on completion, replay identical completed requests without a second side effect, and return `unknown-outcome` without rerunning an intent left started after a parent crash. `replace-file` must reject symlinks, require an expected SHA-256 of the target, atomically write/fsync/rename while held by the opaque lease, preserve mode, and use the same receipt/stale-claim rules. Add `tests/test_execution.py` and `tests/test_replacement.py`, including stale requests, crash-during-exec recovery, exactly-once-after-receipt, and expected-hash conflicts. Verification: `mise exec uv -- uv run python -m unittest tests.test_execution tests.test_replacement -v`.
4. **[T4] Adapter capability isolation (AC3, AC4).** Add `src/worklease/adapters/protocol.py`, `github.py`, `backlog_md.py`, `markdown.py`, and `linear.py` with lazy optional loading and deterministic key/capability results. Match the reference helper's canonical local-key behavior where applicable, make Markdown item inputs map to one source identity and delegate its expected-hash replacement to T3, represent Linear/unknown providers as local-coordination, and reject provider-fenced execution unless an adapter supplies a provider-side conditional check. Add adapter, import-boundary, and Markdown replacement integration tests; keep all network/workflow writes out of scope. Verification: `mise exec uv -- uv run python -m unittest tests.test_adapters -v`.
5. **[T5] Stable CLI contract and documentation (AC3, AC5).** Finish `src/worklease/cli.py` output/error formatting and README sections for schema version 1, JSON default including `--version`, explicit plaintext, exit-code mapping, token redaction with no read-only recovery flag, same-host/POSIX limits, opaque resources, expected-hash file replacement, and adapter guarantees. Add integration tests for acquire/status/list/heartbeat/release/key/exec/replace-file/version, malformed input, rejected token-recovery flags, redaction, unknown-outcome recovery, and deterministic errors. Verification: `mise exec uv -- uv run python -m unittest tests.test_cli -v`.
6. **[T6] CI and release validation (AC6, AC7).** Add `.github/workflows/ci.yml` for supported Linux/macOS mise/uv installation, unittest, pyright, wheel/sdist build and installation, native executable builds, artifact checksums, and release validation. Add `.github/workflows/release.yml` for `v*` tags that publishes wheel/sdist assets plus one-file PyInstaller executables for the supported POSIX Linux/macOS architecture matrix; preserve the existing no-Windows scope and use explicit OS/architecture asset names. The PyInstaller build must preserve package metadata with `--copy-metadata worklease` so the frozen CLI's `importlib.metadata.version('worklease')` lookup works. Publish a SHA-256 `checksums.txt` covering every release asset. Update `scripts/install-release.sh` and the `mise run install-release VERSION=...` path to require `WORKLEASE_REPOSITORY` outside Actions, select the exact matching native asset on supported targets, verify its checksum, install it, and smoke-test `worklease --version`; fall back to the exact versioned wheel with the same checksum verification when no matching native asset is available. Test native artifact selection, checksum validation, wheel fallback, and execution of the downloaded binary against a release fixture and in the tagged workflow. Verification: `mise exec -- pyright`, `mise exec uv -- uv run python -m unittest discover -s tests -v`, `mise exec uv -- uv build`, native binary smoke tests on each target runner, and the release-install smoke test.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement evidence and status:
- Execution status: available now. Provider workflow status remains `To Do`; no dependency, blocker, or active claim prevented refinement.
- Source: whole Backlog.md project `docs/backlog`; discovery through `mise exec -- backlog task list --plain --sort id` returned exactly `TASK-1` with `dependencies: []`.
- Provider: Backlog.md CLI 1.48.0; normal-mode fenced item claim acquired for `refine:TASK-1:1.48.0` with claim ID `B4F73C5F-1C41-4589-BED0-379CFDE0A0E6`; owner visibility was projected as `@codex-main` through the guarded CLI.
- Reference evidence: `/Users/brett/.dotfiles/ai/.bin/backlog-claim` and `/Users/brett/.dotfiles/ai/tests/test_backlog_claim.py` are existing read-only inputs. The helper's test suite establishes the behavior copied into the task map; the helper remains unchanged.
- Repository evidence: only the initial commit exists; there is no git remote. The release workflow therefore uses the runtime `GITHUB_REPOSITORY` context and the local installer requires explicit `WORKLEASE_REPOSITORY`; this avoids inventing an owner/repository and does not block package implementation.
- Oracle challenge was completed once, in one batch. Accepted findings: preserve XDG_STATE_HOME fallback, hash only exact opaque bytes for internal lock filenames, never expose/recover bearer tokens from status, remove the existing `uv.lock` ignore before committing the generated lock, emit JSON for default `--version`, include Markdown expected-hash replacement, and define crash-during-exec `unknown-outcome` rather than promising impossible exactly-once side effects. These choices are recorded above and checked against the helper/repository evidence.
- Missing-reference disposition: no missing implementation path remains. New package/CI/script paths are explicitly item-local creation; `uv.lock` is generated by uv; the dotfiles helper is an external supplied prerequisite/reference, not a file to edit or migrate.
- Final durable checkpoint: `refined: TASK-1 specification complete; provider=backlog-md; providerVersion=1.48.0; claimId=B4F73C5F-1C41-4589-BED0-379CFDE0A0E6; claimRevision=10; correctedSpecificationOperation=49B86452-DD1A-4056-8F7E-C811BFA92D09; notesCorrectionOperation=2545CC51-B860-4CB9-93E3-91623878ABF4; refinement: complete`.
- Keep status, dependencies, labels, priority, and completion state unchanged.

Implementation progress:
- T1 Package and CLI bootstrap complete in commit cd7c328 (rebased onto current main).
- Added pyproject.toml (hatchling, Python >=3.14, worklease console script), src/worklease package/CLI, README.md, tests/test_package_smoke.py, and uv.lock.
- Verification: mise exec uv -- uv lock (pass); mise exec uv -- uv sync --locked (pass, installed worklease 0.1.0); mise exec uv -- uv run worklease --version (pass, JSON {schemaVersion:1, operation:version, ok:true, version:0.1.0}); mise exec uv -- uv run worklease --format text --version (pass, 0.1.0); mise exec uv -- uv run python -m unittest tests.test_package_smoke -v (pass, 3 tests).
- Next implementation task: [T2] Core lease lifecycle.
- Remaining acceptance criteria: #2, #3, #4, #5, #6, #7.

Implementation progress checkpoint:
- [T2] Core lease lifecycle complete in commit 7358658 (integrated into local main).
- Added opaque-resource models, SQLite WAL/FULL schema, migration metadata, monotonic revisions, random bearer tokens, exact claim/token/revision checks, idempotent heartbeat/release receipts, TTL validation/expiry reclaim, crash recovery, and non-blocking POSIX per-resource locks.
- Verification: `mise exec uv -- uv run python -m unittest discover -s tests -v` passed (12 tests); `mise exec -- pyright src/worklease tests` passed (0 errors).
- Next implementation task: [T3] Guarded local operations.
- Remaining acceptance criteria: #2 (T3 portion), #3, #4, #5, #6, #7.

T2 verifier-fix checkpoint:
- Corrected status read redaction so both status and list omit bearer tokens; added regression coverage.
- Added schema migration compatibility for legacy claims tables missing acquire_ttl and coordination_only, with defaults and regression coverage.
- Fix commit: 22f67bb (integrated into local main); T2 implementation commits are 7358658 and 22f67bb, plus durable checkpoint commit 6405653.
- Verification: `mise exec uv -- uv run python -m unittest discover -s tests -v` passed (13 tests); `mise exec -- pyright src/worklease tests` passed (0 errors).
- Independent verifier findings resolved; no T2 criteria remain open.
- Next implementation task remains: [T3] Guarded local operations.

Release scope update: TASK-1 T6/AC7 now includes standalone PyInstaller executables for the existing POSIX Linux/macOS architecture scope, metadata preservation, native-asset checksums, installer selection with wheel fallback, and downloaded-binary smoke tests. No Windows support is added.

Implementation progress checkpoint:
- [T3] Guarded local operations complete in commit b8c6272 (isolated worktree task-1-t3; verified and ready for local integration).
- Added durable operation intent/completion state with legacy SQLite migration; argv-only guarded exec with stdout/stderr capture, periodic heartbeats, process-group termination on ownership loss, idempotent completed receipts, and unknown-outcome handling for started intents after parent interruption.
- Added atomic expected-SHA-256 file replacement with symlink rejection, fsync/rename, mode preservation, stale/version checks, and idempotent receipts; wired exec and replace-file through the CLI.
- Added tests/test_execution.py covering argv behavior, child failures, heartbeat renewal, ownership-loss termination, crash/unknown-outcome recovery, idempotent replay, expected-hash conflicts, symlink rejection, mode preservation, and CLI wiring.
- Verification: mise exec uv -- uv run python -m unittest discover -s tests -v passed (21 tests); mise exec -- pyright src/worklease tests passed (0 errors).
- Independent T3 verifier review is pending before claim release.
- Next implementation task: [T4] Adapter capability isolation. Remaining acceptance criteria: #2 (T4-independent portions), #3, #4, #5, #6, #7.

Updated README with the same-host work-lease model, guarded exec and replace-file examples, Backlog.md usage, explicit local limitations, and MIT licensing. Verified documented CLI flags with uv run worklease exec --help and replace-file --help. Focused test suite passed with mise run test.
T3 verifier-fix checkpoint:
- Independent verifier PASS for T3 implementation range b8c6272, 030cd61, 805d99b, and 4e4b8a0; no findings remain.
- Added same-resource lock-held operation execution/replacement, ownership validation before side effects, SIGTERM/SIGKILL process-group escalation including exited leaders and SIGTERM-resistant descendants, and replay validation before loading deleted inputs.
- Added exact candidate-hash mismatch checks, raw symlink rejection on every replacement invocation, and regression coverage for contender exclusion, expiry during atomic replacement, descendant cleanup, changed replay inputs, and missing replay inputs.
- Final verification under MISE_CONFIG_FILE=/Users/brett/dev/me/worklease/.worktrees/task-1-t3/mise.toml: mise exec uv -- uv run python -m unittest discover -s tests -v passed (23 tests); mise exec -- pyright src/worklease tests passed (0 errors). Focused T3 suite passed (10 tests). Independent verifier manual process-group, stale replacement, migration, and CLI replace-file checks passed.
- Implementation commits: b8c6272, 030cd61, 805d99b, 4e4b8a0; all task-related and clean in task-1-t3 worktree.
- Next implementation task: [T4] Adapter capability isolation. Remaining acceptance criteria: #2 (T4-independent portions), #3, #4, #5, #6, #7.

Resolved the task record merge state through Backlog.md after integrating the documentation update.

Added README guidance linking skills/worklease-workflow/SKILL.md. Documented the agent sequence to discover dependencies, claim through the authoritative source, use worklease for same-host coordination, heartbeat, checkpoint, and release without implying provider-side fencing.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: @codex-main
created: 2026-07-13 21:04
---
Expanded the in-progress release scope before T6: keep wheel/sdist, add POSIX Linux/macOS PyInstaller assets, preserve worklease metadata, verify checksums for every asset, select native binaries with wheel fallback, and execute the downloaded artifact in smoke tests.
---
<!-- COMMENTS:END -->
