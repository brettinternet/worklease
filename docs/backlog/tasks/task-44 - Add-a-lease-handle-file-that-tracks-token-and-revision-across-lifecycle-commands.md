---
id: TASK-44
title: >-
  Add a lease handle file that tracks token and revision across lifecycle
  commands
status: Done
assignee:
  - '@codex-task-44'
created_date: '2026-09-07 03:26'
updated_date: '2026-09-07 04:49'
labels:
  - cli
  - devex
dependencies: []
references:
  - README.md
  - src/worklease/cli.py
  - src/worklease/credentials.py
modified_files:
  - README.md
  - scripts/release_artifacts.py
  - skills/worklease-workflow/SKILL.md
  - src/worklease/cli.py
  - src/worklease/lease_file.py
  - src/worklease/schemas/v1/index.json
  - src/worklease/schemas/v1/lease-file.json
  - tests/test_cli.py
  - tests/test_schemas.py
priority: high
type: feature
ordinal: 45000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Each mutation requires `--resource`, `--claim-id`, `--token`/`--token-file`, and the newest `--revision`. Tracking the revision by hand is the most error-prone part of the lifecycle for humans and agents (stale-revision was the most common failure while exercising the CLI), and passing `--token` in argv leaks the bearer secret.

Add `--lease-file PATH` (mode 0600, JSON). `acquire`, `acquire-bundle`, and `transfer` write resource(s), claim ID, token, revision, expiry, and guarantee to it and omit TOKEN from stdout when it is used. `heartbeat`, `checkpoint`, `exec`, `replace-file`, `reconcile-*`, `release`, and bundle equivalents accept `--lease-file` in place of the four identity/credential/revision flags and rewrite the revision after every successful mutation. `release` clears the file. Explicit flags still override individual fields for advanced use. Keep the file format documented and versioned; it is a caller convenience and not a claim.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 acquire --lease-file writes a 0600 file and prints no TOKEN line; the file contains resource, claimId, token, revision, expiresAt, and guarantee
- [x] #2 A full acquire, heartbeat, exec, checkpoint, release sequence works with only --lease-file and no --revision, --claim-id, --resource, or token flags
- [x] #3 Each successful mutation updates the stored revision; a concurrent stale file yields stale-revision with the same exit code as today
- [x] #4 Bundle commands and transfer support the lease file, including successor handoff to a new file
- [x] #5 README, help epilogs, and skills/worklease-workflow show the lease-file form as the primary example; schemas and tests updated
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Trace lifecycle and bundle command parsing, credential flow, outputs, schemas, help, documentation, and existing atomic file patterns.
2. Add a versioned 0600 lease-file abstraction and resolve explicit CLI fields over stored fields for lifecycle, bundle, and transfer operations.
3. Persist lease state after successful acquisition/mutations, clear it after release, suppress token output when a lease file is used, and cover stale/concurrent behavior.
4. Update schemas, README, help epilogs, and workflow skill; run focused and full quality gates, review, commit, merge to main, and clean up the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented a versioned, atomically written owner-only lease handle and wired it through singleton, bundle, transfer, reconcile, guarded execution, replacement, and release paths. Explicit CLI values override stored fields; successful mutations persist the resulting revision, release removes the handle, and lease-file output redacts tokens.

Review found that malformed or missing lease files could escape the CLI LeaseError boundary; moved lease-file resolution under normal runtime error handling and added regression coverage.

Rebased onto TASK-43 lifecycle identifier defaults, resolved overlapping help/docs, and moved generated transfer defaults after lease-file resolution so an omitted successor work key derives from the stored resource. Lease-file lifecycle and transfer tests now exercise generated operation and successor IDs.

Validation after integration: focused lease-file/default tests passed (8 tests); mise run lint, format-check, test (231 tests across core and SDK), and typecheck all passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added secure versioned lease files across singleton, bundle, transfer, and lifecycle commands. Handles suppress bearer tokens from output, advance revisions after successful mutations, preserve stale-revision behavior, and are removed after release. Updated schemas, packaged artifacts, CLI help, README, workflow guidance, and regression coverage; all repository quality gates passed.
<!-- SECTION:FINAL_SUMMARY:END -->
