---
id: TASK-13
title: Add non-argv claim credential input
status: To Do
assignee:
  - '@codex-main'
created_date: '2026-07-14 02:34'
updated_date: '2026-07-14 03:33'
labels:
  - cli
  - security
  - credentials
dependencies: []
references:
  - src/worklease/cli.py
  - tests/test_cli.py
priority: medium
type: enhancement
ordinal: 13000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Let automation supply claim bearer tokens to mutating CLI commands without placing the secret value in process arguments or shell history. Preserve the one-time token returned by acquire and the existing token-redaction contract.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Heartbeat, release, exec, and replace-file accept documented file- and file-descriptor-based token sources with the same ownership semantics as the existing token argument.
- [ ] #2 Exactly one token source is accepted; missing, conflicting, unreadable, malformed, or unsafe credential inputs fail deterministically before state changes or child execution.
- [ ] #3 Credential values are never included in argv-derived diagnostics, JSON/text output, logs, exceptions, or child-process environments.
- [ ] #4 Existing success, stale-owner, idempotency, and exit-code behavior remains consistent across supported token sources, with automated redaction and child-isolation tests.
- [ ] #5 README lifecycle examples use a non-argv token source and explain the compatibility and security behavior of every supported source.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. **[T1] Non-argv credential resolver (AC1-AC3).** Add one shared resolver used by heartbeat, checkpoint when present, release, exec, and replace-file. Accept exactly one of legacy `--token`, `--token-file PATH`, or `--token-fd N`; read at most 4096 UTF-8 bytes once, allow one trailing newline only, and reject empty, NUL, multiline, oversized, conflicting, unreadable, symlink, non-regular, or group/other-accessible files before opening the store.
2. **[T2] Command integration and security tests (AC1-AC4).** Wire every claim-bearing command through the resolver without changing ownership/idempotency semantics. Test file/FD success, stdin FD use, stale claims, parser errors, argv/process diagnostics, exception text, emitted output, and exec child environment/FD isolation with sentinel secrets.
3. **[T3] Secure lifecycle documentation (AC5).** Update examples to prefer a 0600 token file or inherited FD, document that direct `--token` remains compatibility-only, and state precedence, one-shot reads, size/encoding rules, and shell/process-list risks.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
### Refinement snapshot

**Execution status:** available now.

**Goal and target area:** Add a shared credential input helper beside `src/worklease/cli.py`, wire existing claim-bearing commands, extend `tests/test_cli.py`, and update README examples. The helper file is an item-local creation.

**Resolved decisions:** Preserve direct `--token` for compatibility but require exactly one token source. `--token-file` uses no-follow open, requires a regular owner-only file (no group/other permission bits), and accepts at most 4096 UTF-8 bytes with one optional trailing newline. `--token-fd` accepts a non-negative inherited descriptor, reads once to EOF through a non-inheritable duplicate, and enforces the same encoding/shape/size rules; FD 0 is supported. Resolve credentials before constructing/opening LeaseStore. Errors identify only the source kind/reason, never token/path contents. Subprocess execution continues to close non-standard FDs and never adds the token to the child environment.

**Non-goals:** environment-variable/keychain/provider credential loading, token recovery, encryption at rest, changing acquire token issuance, or removing legacy argv support.

**Evidence and assumptions:** Current `_common_claim_arguments` requires `--token` for every mutation; argparse and subprocess close-fd defaults provide the existing pattern. POSIX-only file permission checks match the supported platform scope.

**Task/acceptance map:** T1→AC1-3; T2→AC1-4; T3→AC5.

**Pending verification:** every mutation command with file/FD/direct sources, 0600 and unsafe-file cases, oversized/multiline/NUL inputs, process/exception/output sentinel search, exec child isolation, full quality gates.

**Next action:** implement and unit-test the resolver before wiring command parsers.

**Refinement checkpoint:** refined: TASK-13 specification complete; provider=backlog-md; providerVersion=1.48.0; claimId=0736ab08-420f-4720-a152-75d24d4ac238; claimRevision=9; refinement: complete.
<!-- SECTION:NOTES:END -->
