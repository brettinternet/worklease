---
id: TASK-13
title: Add non-argv claim credential input
status: Done
assignee:
  - '@codex-main'
created_date: '2026-07-14 02:34'
updated_date: '2026-07-14 19:01'
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
- [x] #1 Heartbeat, release, exec, and replace-file accept documented file- and file-descriptor-based token sources with the same ownership semantics as the existing token argument.
- [x] #2 Exactly one token source is accepted; missing, conflicting, unreadable, malformed, or unsafe credential inputs fail deterministically before state changes or child execution.
- [x] #3 Credential values are never included in argv-derived diagnostics, JSON/text output, logs, exceptions, or child-process environments.
- [x] #4 Existing success, stale-owner, idempotency, and exit-code behavior remains consistent across supported token sources, with automated redaction and child-isolation tests.
- [x] #5 README lifecycle examples use a non-argv token source and explain the compatibility and security behavior of every supported source.
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

Implementation checkpoint (T1): commit 06db8774f547193663639056a77c91520c32160b; added the shared credential resolver with direct, owner-only no-follow file, and duplicated non-inheritable descriptor sources, bounded UTF-8 validation, source exclusivity, and redacted deterministic errors. Verification: mise run lint, mise run format-check, mise run test (115 tests), mise run typecheck, mise run hooks all passed. Next task: T2 command integration and security tests. Remaining acceptance criteria: #1-#5.

Implementation checkpoint (T2): commit 8a5e5c6f2d98703ab728f443d4b96a01972a4d57 adds shared CLI credential-source resolution across claim-bearing commands, deterministic source validation before LeaseStore creation, file/FD lifecycle coverage, missing/conflicting source no-mutation checks, and non-argv exec child redaction coverage. Verification: mise run lint passed; mise run format-check passed; mise run test passed; mise run typecheck passed; mise run hooks passed. Next task: T3 secure lifecycle documentation. Remaining acceptance criteria: #5.

Implementation follow-up (T2 coverage): commit 8ac85983f7d1970be6781bcc42049f722d84f428 adds file-source replace-file integration coverage and token redaction assertion. Verification: mise run hooks passed (format, lint, full tests). T2 remains complete; next task is T3 secure lifecycle documentation; remaining acceptance criterion: #5.

Implementation pass T3 claimed under claim EF39F87E-B0F6-4246-9BA8-5DA4D64B8D39; canonical in-progress checkpoint refreshed.

Implementation checkpoint (T3): commit 751ef2d integrated into canonical main as 751ef2d. Updated README lifecycle examples and command reference to prefer --token-file/--token-fd, documented exact source exclusivity, 0600/no-follow/regular owner-only file rules, bounded UTF-8/newline validation, one-shot reads, FD 0 support, compatibility risks, and redaction/child-isolation guarantees. Verification: mise run lint, mise run format-check, mise run test (115 tests), mise run typecheck, mise run hooks all passed; README contains no lifecycle --token secret examples. All implementation tasks complete; next pass is exact-item review; remaining acceptance criteria: #1-#5.

Review checkpoint: reviewed: implementation commits 46fcc08a0000d39d24c6e30096d454330a3f66eb, bcee32d0959956efbe872edabff9a28adb89ec03, fcbbcb19de5fb83b98ca0187b73e394a8baaba8d, 751ef2d8ae5d526aa06728161150a28e0181d6a1; review-fix commits: none. Full security/compatibility review found no actionable findings. Verified targeted credential and CLI tests plus mise run lint, mise run format-check, mise run test, mise run typecheck, and mise run hooks.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented non-argv claim credential sources with shared validation, secure file/FD handling, CLI integration, redaction and child-isolation coverage, and lifecycle documentation. Reviewed the complete accumulated implementation at the exact commits above with no fixes required; targeted tests and all repository quality gates passed.
<!-- SECTION:FINAL_SUMMARY:END -->
