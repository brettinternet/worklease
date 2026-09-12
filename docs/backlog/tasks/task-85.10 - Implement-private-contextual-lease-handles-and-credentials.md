---
id: TASK-85.10
title: Implement contextual handles and credential sources
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.7
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/lease_context.py
  - src/worklease/lease_file.py
  - src/worklease/credentials.py
  - tests/test_lease_context.py
  - tests/test_credentials.py
  - tests/test_cli.py
  - TASK-67
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 102000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Copying tokens, claim ids, and revisions between commands is the main way agents leak secrets or act on stale state. The contract makes an owner-only contextual handle the default claim selection (D7, section 9) so ordinary commands need no identity flags, while explicit handles and file or descriptor credentials remain for concurrent and automated use. TASK-67 explored this in Python; this task implements the Go rules directly.

Read first: contract sections 2 (D6, D7), 4 (claim selection precedence and acquire flags), 6.2, 9, 18 (handle sketch). Python evidence: `src/worklease/lease_context.py` (context root through git rev-parse with GIT_* stripped, sha256 of the root), `lease_file.py` (size cap, atomic write, permission checks), `credentials.py` (file and descriptor rules); all tests in `tests/test_lease_context.py` and `tests/test_credentials.py`; `tests/test_cli.py`: test_unwritable_lease_file_fails_before_the_claim_commits, test_late_lease_file_failure_still_returns_the_claim_token, test_acquire_refuses_to_clobber_a_handle_holding_a_live_claim, test_idempotent_replay_does_not_rewind_the_lease_handle, test_lease_file_singleton_lifecycle_tracks_revision_and_clears, test_lease_file_bundle_and_transfer_handoff, test_non_argv_token_sources_cover_lifecycle_and_prevent_state_changes. Pattern: `../hum/internal/cli/project_dir_test.go` for working-directory-dependent tests.

Deliver in `internal/handle`: `ContextRoot`, `ContextualPath`, `Read`, `Write`, `Remove` with the section 9 safety rules and atomic writes; credential readers for `--token-file` and `--token-fd`; `Select` implementing the claim-selection precedence with per-field overrides and the errors claim-selection-missing, credential-source-conflict, handle-unsafe, handle-malformed. Wire into `internal/cli`: `acquire` writes the contextual handle by default (handle-in-use check before the authority write; destination directory prepared and validated before the transaction; token omitted from output when a handle is written; handle-write-failed partial success per contract 6.2); `heartbeat`, `checkpoint`, `release`, and `transfer` resolve the claim through `Select`; mutations rewrite the handle after commit without rewinding on idempotent replay; `release` removes it; `transfer` removes it and honors `--successor-handle`; `status` with no arguments shows the contextual claim. Remove the temporary `--no-handle` requirement from TASK-85.7.

Owned paths: `internal/handle`, claim-selection wiring in the `internal/cli` lifecycle commands. Out of scope: MCP handles (TASK-85.15 reuses Read and Write), guard commands (TASK-85.12 reuses Select).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Context tests prove a Git root, a nested subdirectory, and a path through a symlink resolve to the same context id even with GIT_DIR and GIT_WORK_TREE set, a linked worktree has a different id, a non-Git directory uses its resolved path, and two distinct non-Git directories differ.
- [ ] #2 Handle safety tests prove reads fail handle-unsafe for a symlink, a directory, a foreign-owned file (injected stat), and mode 0644, and handle-malformed for 65 KiB content, non-JSON, schemaVersion 2, and a missing token field; error messages contain the path but never file contents; writes produce mode 0600 through a temp file plus rename and leave no temp file behind on failure.
- [ ] #3 Lifecycle CLI tests prove `acquire -r R` with no handle flags writes ctx-<id>.json and omits the token from text and JSON, a second acquire in the same context while that claim is active fails handle-in-use without touching the authority (event count unchanged), after release the handle is gone and a new acquire succeeds, heartbeat, checkpoint, release, transfer with --successor-handle, and status need no identity flags, an idempotent heartbeat replay leaves the handle revision unchanged, and `--handle PATH` supports two concurrent claims in one context.
- [ ] #4 Credential tests prove explicit --claim-id and --revision with --token-file (0600 regular file) or --token-fd (including stdin as fd 0) work, a 0644 token file fails credential-unsafe, both sources together fail credential-source-conflict, oversized or multi-line content fails credential-malformed, and an explicit --revision overrides the handle revision while the handle supplies the other fields.
- [ ] #5 Partial-success tests prove a mutation whose handle rewrite fails (directory made read-only after commit) exits 75 with handle-write-failed, emits the claim with the token and handleError, and the authority holds the committed state; `mise run ci-go` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
