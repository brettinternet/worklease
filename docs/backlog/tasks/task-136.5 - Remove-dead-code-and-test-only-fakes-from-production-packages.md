---
id: TASK-136.5
title: Remove dead code and test-only fakes from production packages
status: Done
assignee:
  - '@pi'
created_date: '2026-09-24 15:05'
updated_date: '2026-09-26 03:40'
labels:
  - reviewed
dependencies: []
parent_task_id: TASK-136
priority: low
type: chore
ordinal: 56000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
On 2026-09-24, `go run golang.org/x/tools/cmd/deadcode@latest -test ./...` reported functions that nothing calls, not even the tests: `cli.ValidateResourceInput` (`internal/cli/resource_input.go:80`), `gc.Service.Run` (`internal/gc/gc.go:172`), `handle.ResolveCredential` (`internal/handle/handle.go:1246`), and `reason.Wrap` (`internal/reason/reason.go:158`).

Run without `-test`, deadcode also lists production functions that only tests reach:
- `authority.FakeAuthority` and its roughly 20 methods (`internal/authority/authority.go`, lines 679–760). The only caller is `internal/queue/claims_test.go`.
- The remote smoke harness `writeCertificate` (`cmd/worklease-remote-smoke/main.go:5863`), which only its own test calls.

The rest look like deliberate test hooks and need a judgment call each: `mcp.Server.Call`/`Close`, `server.Server.Handler`/`Close`, `handle.StoreCredential`, `store.Store.db`, `store.Tx.execContext`, `lease.WrapRemoteResponse`, `lease.GenerateInstallationCredential`, `queue.Store.Subscribe`/`publish`, `quotaQueue.mutationSlot`, `reason.Names`, `cli.inviteFromCommand`, and `internal/testkit`. Keep hooks that tests in several packages use. Move a hook into a `_test.go` file when only one package uses it. Delete code that is truly unused, along with any test that exists only to exercise it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `deadcode -test ./...` reports nothing except exceptions listed in the task notes with a reason for each
- [x] #2 `authority.FakeAuthority` is gone from non-test code in `internal/authority`, and `internal/queue` tests still pass using an equivalent fake in test code
- [x] #3 Every remaining entry in `deadcode ./...` output has been moved into a `_test.go` file of its only consuming package, or is listed in the task notes with a reason to keep it
- [x] #4 `mise run lint`, `mise run format-check`, `mise run test`, and `mise run typecheck` pass
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Recheck both deadcode modes and usages against current main (baseline: 5 test-inclusive functions; 55 production-only entries).
2. Remove truly unused helpers and redundant wrappers; replace the cross-package production fake with a queue-local test double; move only-package test helpers into _test.go. Keep cross-package test APIs and runtime hooks with documented reasons.
3. Run targeted race tests, both deadcode modes, all required quality gates and staged hooks; inspect one focused review pass.
4. Commit the branch, merge into local main, update and finalize the task in primary checkout, then clean up the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Baseline on f23b59c: deadcode -test reported five unreachable functions; ordinary deadcode also flagged FakeAuthority, writeCertificate, and test hooks.

Implementation: removed five fully dead helpers and authority.FakeAuthority; queue/claims_test.go now supplies the status/list-only test double. Moved eight single-package test-only helpers into _test.go and removed two redundant test wrappers.

Verification on merged main 8c13a09: deadcode -test ./... prints nothing; deadcode ./... reports only the exceptions below. go test ./internal/queue and changed-test go test -race -count=3 (queue, store, CLI, lease, reason, queueui, remote-smoke) passed. mise run lint, format-check, test, typecheck, hooks-install, and staged hooks passed in the worktree. Independent single-pass review found no actionable defects.

Production-only deadcode exceptions retained: handle.StoreCredential is used by internal/authority tests across package boundaries; mcp.Server.Call and Close are exercised by both internal/mcp and internal/cli tests; server.Server.Handler and Close are used by internal/mcp, internal/cli, and internal/queue tests; internal/testkit is a reusable test-only package used across multiple packages, so its production compilation is intentional. No other findings remain.

Delivery: implementation commit 0f61d0b, local main merge 8c13a09; owned worktree and branch removed and associated Herdr workspace closed.

Retrospective review on main d300eb9: moved queueAuthorityForView and queueAuthorityForViewWithMetadata into CLI test code; deadcode -test ./... is empty. NewGitHubWriteAdapter remains a production-exported cross-package test API used by internal/cli and internal/queue tests; ordinary deadcode reports it by design. Focused race, full quality gates and staged hooks passed; no remaining actionable finding.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Removed unused helpers and the production FakeAuthority; relocated test-only helpers. Deadcode -test is clean, remaining production-only findings are documented shared test APIs; race checks, all four quality gates, and pre-commit hooks pass. Merged 0f61d0b via 8c13a09.
<!-- SECTION:FINAL_SUMMARY:END -->
