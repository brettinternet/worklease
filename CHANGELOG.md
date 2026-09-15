# Changelog

## Unreleased

## 2.0.0 - 2026-09-15

### Breaking

- Hosted-authority lifecycle commands now live under `worklease server`; the former `worklease hosted` command is removed.

### Added

- `worklease server init` creates a private local-only server configuration, authority, and bootstrap invite with no arguments, and `worklease serve` loads that default configuration without arguments. `WORKLEASE_SERVER_CONFIG` or `--server-config` selects another file.

## 1.3.0 - 2026-09-15

### Added

- Every `list` command accepts `ls`, including the top-level claim list and the nested policy, profile, and installation lists.
- `worklease completion bash|zsh|fish` prints deterministic, read-only shell completion derived from the visible command tree, including aliases and options.
- The experimental, opt-in self-hosted remote claim authority is documented with profile management, invite enrollment, remote CLI/MCP operation, hosted lifecycle, single-writer deployment, and restore/reopen recovery boundaries. The standard binary remains listener- and network-free unless remote profile management, a selected remote profile, or `serve` is explicitly used; local reads remain setup-free.
- Actions run [34915583460](https://github.com/brettinternet/worklease/actions/runs/34915583460) at commit `7e4cef4312d09e380d5c02cea7f23bd13ab66a9c` passed all four target archive jobs and native smoke on matching runners. Relative to v1.2.0, archive/binary deltas were linux-arm64 `+2,147,600 B`/`+5,242,880 B`, linux-x64 `+2,383,219 B`/`+5,763,072 B`, macos-arm64 `+2,218,770 B`/`+5,344,688 B`, and macos-x64 `+2,396,120 B`/`+5,856,672 B`. The dispatch did not publish, tag, or push.

## 1.2.0 - 2026-09-13

### Changed

- Compact `events` and resource `history` timelines omit synthetic row numbers, identifiers, sequence metadata, and redundant labels; `--full` retains complete non-secret diagnostics, and other collection details no longer use unstable row numbers.

## 1.1.1 - 2026-09-13

### Breaking

- Short options are now reserved for common workflows: `-j/--json`, `-H/--home`, `-h/--help`, `-v/--version`, `-r/--resource`, `-s/--session`, `-t/--ttl`, `-w/--wait`, `-a/--agent`, `-f/--full`, and `-m/--reason`. Every other option is long-only; notably, `--source` and `--work-key` no longer use `-s` and `-w`.

### Added

- Release preparation now promotes curated Unreleased notes to a validated version and date; tagged releases require and publish the exact matching changelog section.

## 1.1.0 - 2026-09-12

### Fixed

- `worklease verify` no longer panics in text mode; the claim view is rendered as a concise verification summary.
- `worklease list --resource KEY` now applies the filter instead of silently listing every claim, and rejects more than one resource.
- `gc` text prints its cutoff as an RFC3339 timestamp and its preview hint is the exact `worklease gc --apply --cutoff TIME` follow-up command.

### Changed

- Every CLI option now has operational help with a value placeholder and, where meaningful, the effective runtime default (`--ttl` 15m, `--max-duration` 1h, `--limit` 50, `--timeout` 30s, `--retention-days` 30, `--reason` released, `--agent` login user); zero-value sentinels are no longer shown. Usage lines show required inputs and alternate forms, including `exec ... -- COMMAND [ARGS...]`, `policy describe NAME`, `op inspect` selectors, both `history` projections, and the `verify --hook` form, and `[selection]` is explained in every command that accepts it. Top-level help and the manual group commands into claim lifecycle, inspection and recovery, and setup and administration.
- Compact `events` rows identify the affected resource set and omit empty claim or resource fields for authority-wide events; compact `history --resource` epochs show the agent, how ended epochs ended, and the ordered operation kinds with any non-completed state marked. Counts are pluralized correctly and sub-second times read as `now`.
- Human text never prints a bare opaque cursor: `events` and `history` text omit cursors and empty coverage fields, and `watch` text shows relative expiry and presents its resumption cursor only inside a copyable `resume: worklease watch --cursor ...` line. JSON cursor fields are unchanged.
- `worklease list` text states `no current claims` instead of printing a bare table header when nothing matches.
- `policy describe` without a name reports the available policy names instead of an unknown empty policy.
- `worklease history` without a resource now shows the bounded recent global event feed, while `history --resource RESOURCE` retains its resource epoch projection. Event cursors are emitted only in JSON output.
- Human CLI output now uses operation-specific summaries, deterministic fields, compact relative timing, labeled payload blocks, meaningful `--full` views with RFC3339 timestamps, and restrained TTY color instead of Go map or struct dumps across lifecycle, guarded, verification, inspection, reconciliation, policy, watch, GC, doctor, and error output. JSON envelopes remain unchanged; text checkpoint and transfer results add byte-count, successor-handle, and resource details.

### Added

- `worklease help --all` prints the root help and every command and subcommand once, in tree order, without touching state; `worklease help COMMAND [SUBCOMMAND]` resolves nested commands and reports unknown names as a JSON error under `--json`.

## 1.0.0 - 2026-09-12

### Fixed

- Hold deadlines now participate in exact acquire, heartbeat, and checkpoint intent identity, and MCP persists one fixed deadline across waits and recovery without constraining explicit CLI takeover.
- Guarded `exec` now waits for its own in-flight renewal before recording completion, so a child that exits during a renewal no longer fails `stale-revision` and strands a finished command as an unresolved operation; only ownership or clock renewal failures terminate the child.
- Guard failures after the started intent commits keep the exact pending request in the handle and report `commitState: unknown` instead of clearing it.
- MCP heartbeat, checkpoint, release, recovery, and automatic renewal restore the ready handle after a definitive no-commit failure, and a contended MCP acquire removes its pending grant and reports `not-committed` instead of leaving orphan pending handles behind.
- `replace-file` records a completed failure for temporary-file errors before rename instead of leaving the operation unresolved.
- `replace-file` now reads and digests at most 16 MiB of replacement content before its completing authority transaction, avoiding bulk input I/O while holding SQLite's write lock.
- Output redaction no longer destroys contextual handle paths and SHA-256 hash fields, and MCP tool results no longer report a stopped automatic renewer as `active`.
- Documented that SQLite recreates private WAL sidecars for read-only opens; a driver test covers the sidecars-absent case.

### Removed

- Retired the Python proof of concept, source-provider SDK, Python packaging, legacy JSON schemas, and Python test/build automation after the Go capability cutover.

### Changed

- Repointed generic repository gates, hooks, and CI to the Go implementation, including race, vulnerability, built-binary, documentation, and clean-checkout end-to-end checks.

### Added

- Bootstrapped the Go CLI, typed configuration, stable error/output contracts, and additive Go quality gates.
- Added deterministic, race-safe Go test helpers for clocks, identities, isolated environments, CLI invocation, and bounded subprocesses.
- Added deterministic Go resource policies, canonical keys, and key/policy inspection commands.
- Added the transactional Go singleton lease lifecycle with hashed client credentials, authenticated replay, clock safety, and guarded-operation state.
- Extended Go claims to 1–32 ordered resources with atomic overlap handling, whole-claim lifecycle mutations, and mixed-resource status projections.
- Added private contextual handles, strict credential sources, durable pending requests, and stable cross-process handle locks.
- Added redacted operation inspection, authenticated reconciliation, cursor-bound lifecycle events, and exact-resource history to the Go implementation.
- Added strict-cutoff transactional Go garbage collection and deterministic Unicode-safe read-view text output.
- Added canonical Go loop and safety instructions, staged command help polish, expiry-aware watch guidance, and read-only environment diagnostics.
- Prepared the intentionally incompatible Go cutover with four native archives, command-derived manual, built-binary smoke tests, current CLI/MCP quick starts, and optional recoverable disposal guidance for untouched Python-era state.

## 0.10.0 - 2026-09-12

### Added

- Added `worklease events`, a redacted, paginated cross-resource feed of retained lifecycle records with stable keyset cursors.

### Breaking

- A bare `worklease acquire` or `acquire-bundle` now writes a private contextual handle and omits the bearer token from output. Use `--no-lease-file` for the compatible stateless token-bearing behavior.

### Changed

- Default `history` text is now a concise chronological timeline with explicit retention and current-snapshot caveats; `--full` preserves the complete redacted diagnostic projection.
- Default `policy list` text now shows a compact five-column summary; `--full` preserves package provenance and policy contract versions.
- Garbage-collection text now highlights eligible or collected totals, compact age ranges, protected unresolved operations, and the exact safe follow-up command without exposing storage field names or null placeholders.
- Default singleton and bundle `status` text now shows a compact operational summary; `--verbose` preserves the complete redacted diagnostic projection.

## 0.9.1 - 2026-09-11

### Added

- v0.9.1 adds a concise, task-oriented `worklease(1)` manual with valid examples; release automation generates it from the current CLI, packages it in native archives, publishes a version-specific changelog asset, and uses that changelog for GitHub release notes.

## 0.9.0 - 2026-09-11

### Added

- Added an optional local stdio MCP server with seven typed lease-lifecycle tools, private restart-safe lease references, automatic bounded heartbeats, and singleton/bundle interoperability with the canonical CLI and public Python API.
- Added a published schema-v1 MCP result contract, Claude Code setup and recovery guidance, and a repeatable MCP-versus-CLI lifecycle benchmark.

### Changed

- Added public bundle checkpoint support and shared canonical agent instructions for non-CLI integrations.

## 0.8.4 - 2026-09-10

### Changed

- `worklease gc --apply` now retires expired singleton and bundle claims whose stored expiry is older than the retention cutoff, so abandoned claims no longer remain in `worklease list` indefinitely.
- GC preserves active and recently expired claims, reports old expired claims protected by unresolved operations, records truthful expired terminations, and keeps resource revisions monotonic.
- Text-mode GC dry runs with eligible records now print a copyable apply hint using the captured cutoff.

## 0.8.3 - 2026-09-09

### Added

- Added `worklease instructions loop` and `worklease instructions safety` for concise, version-matched agent coordination guidance without loading the full workflow skill.
- Added minimal `AGENTS.md` and Ralph-loop integration examples to the README, with the full skill reserved for advanced workflows.

## 0.8.2 - 2026-09-07

### Added

- Added durable local claim lifecycle history with ownership epoch boundaries, operation and reconciliation records, explicit termination reasons, current-state snapshots, and provenance/coverage metadata.
- Added `worklease history --resource RESOURCE` with human-readable and exportable JSON output for one canonical resource.

### Changed

- Reorganized and tightened the README, CLI reference, claim model, source SDK compatibility guide, distributed authority guide, and reusable workflow skill for faster scanning and clearer operational guidance.

### Fixed

- Removed unused private history projection fields and documented the history command's required resource and text coverage output.

## 0.8.1 - 2026-09-07

### Fixed

- **Mutual exclusion under a backward clock step.** 0.8.0 treated any lease whose stored expiry outlived its granted TTL as expired, so a backward clock step as small as 50 ms dispossessed a live holder and let a second agent acquire the same resource. A live holder is now never expired by a clock step; instead the next contender re-anchors an implausible stored expiry to the corrected clock, so an abandoned lease is reclaimable within one TTL rather than after the full size of the step.
- **`transfer` replay no longer returns an unrelated owner's bearer token.** Replaying a completed transfer after the resource had turned over handed the replaying predecessor whatever token currently held the resource, giving it full control of a live third-party claim. The recorded successor's token is returned only while that successor is still current.
- **`acquire` re-checks bundle membership under its lock.** The check ran before the resource lock, so a bundle committing in that window let a singleton claim overwrite a bundle member and permanently wedge the resource beyond the reach of both `acquire` and `gc`.
- **A lease handle that cannot be written no longer strands the claim.** The destination is validated before the mutation commits, and a failure afterwards emits the successful payload with its bearer token plus a `leaseFileError` field and exit `75`, instead of discarding the only copy of the token.
- **`acquire --lease-file` refuses to overwrite a handle that still holds a live claim** (`lease-file-in-use`), rather than orphaning the earlier resource for its full TTL.
- **An idempotent replay no longer rewinds a lease handle.** Replaying an earlier operation returns its recorded receipt, whose claim carries an older revision; writing that back left the handle behind the store and failed the next mutation with `stale-revision`.
- **`exec` operations recorded before `--max-duration` existed replay again.** The bound had joined the idempotency fingerprint, so upgrading turned the documented replay recovery for a pre-upgrade operation into `operation-id-request-mismatch`. Replaying with a different bound is now idempotent too.
- **Grouped short options no longer consume a guarded child's arguments.** `worklease -jH DIR exec ... /bin/echo --json` treated the cluster as one option plus an attached value, misplacing the boundary and rejecting a valid command. An unrecognized option also no longer truncates the caller's own output options away, restoring the JSON error envelope.
- **`list` measures display width correctly.** Combining marks and format characters counted as one or two columns instead of zero, so decomposed text (the normalization macOS filesystems produce) misaligned every following column; truncation could also leave a mark whose base character had been cut.
- **Credential comparison is total.** A token that is not UTF-8 encodable raised out of the ownership guard instead of failing authentication.

### Documentation

- Added [docs/cli-reference.md](docs/cli-reference.md) with the exit-code table, short option namespace, state selection, supported API surface, garbage-collection semantics, and text output grammar that the 0.8.0 README rewrite removed without relocating.
- The README lifecycle example now runs to completion when copy-pasted and releases its lease on failure.

## 0.8.0 - 2026-09-07

### Added

- Added `--lease-file`, a private mode `0600` lease handle that carries the claim token and revision across `acquire`, `heartbeat`, `checkpoint`, `exec`, `transfer`, and `release`, so lifecycle commands no longer pass bearer tokens on the command line. `transfer` accepts `--successor-lease-file`.
- Added generated lifecycle identifiers. `--claim-id`, `--session-id`, `--owner-id`, and `--operation-id` default to fresh random values, `--work-key` defaults to the resource, and `--agent-id` defaults to `WORKLEASE_AGENT_ID`. `acquire --resource R` now succeeds on its own, and every generated identifier is echoed in text and JSON output so callers can replay or transfer. Caller-supplied identifiers keep their existing idempotency semantics.
- Added `--max-duration` to `exec` and `exec-bundle`, bounding child runtime and inherited-pipe draining with a finite `3600`-second default. On expiry Worklease terminates the child's process group, records the operation as completed with reason `child-process-timeout`, and exits `124`. A grandchild that escapes the process group may survive; captured output may be truncated.
- Added the stable `invalid-token` reason (exit code 2) when a current claim ID is paired with an incorrect bearer token. This additive reason is a minor-version API change; `stale-claim` remains reserved for claim ID ownership loss.

### Fixed

- `exec` and `exec-bundle` no longer scan the child command's arguments for Worklease output options when `--` is omitted, so a child flag such as `--json` is passed through instead of being consumed.
- `status --verbose` now reports the full resource set for a bundle member and lists started `exec-bundle` operations under `unknownOperations`.
- Reclaiming an expired bundle that partially overlaps a new bundle no longer leaves a `claims` row referencing a removed bundle.
- A backward wall-clock step no longer leaves a lease held past its TTL. See the Unreleased correction above: as shipped, this expired any lease whose clock moved backward past its last renewal.
- Human-readable `list` output measures display width, so wide characters keep columns aligned.

### Changed

- Split the lease store along singleton and bundle seams and reduced per-command storage and import overhead. No behavior change.
- Rewrote the README around a complete copyable lifecycle, leaving the per-command inventory to command help.

### Breaking CLI changes

Short options now have one meaning across the complete command tree. Long options are unchanged. Update scripts as follows:

| Command | Removed short option | Use instead |
| --- | --- | --- |
| global | `-a` (`--help-all`) | `--help-all` |
| `key` | `-s` (`--source`) | `--source` |
| `acquire`, `acquire-bundle` | `-o` (`--owner-id`) | `--owner-id` |
| `gc` | `-r` (`--retention-days`) | `--retention-days` |
| `gc` | `-c` (`--cutoff`) | `--cutoff` |
| `gc` | `-a` (`--apply`) | `--apply` |
| `transfer` | `-C` (`--successor-claim-id`) | `--successor-claim-id` |
| `transfer` | `-O` (`--successor-owner-id`) | `--successor-owner-id` |
| `transfer` | `-W` (`--successor-work-key`) | `--successor-work-key` |
| `list` | `-F` (`--full`) | `--full` |
| `replace-file` | `-p` (`--path`) | `--path` |
| `replace-file` | `-e` (`--expected-sha256`) | `--expected-sha256` |
| `replace-file` | `-C` (`--content-file`) | `--content-file` |

`status --verbose` text labels now use upper snake case, matching the other text renderers (for example, `CLAIM_ID` instead of `claimId`).
