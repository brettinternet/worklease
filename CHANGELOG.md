# Changelog

## Unreleased

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
