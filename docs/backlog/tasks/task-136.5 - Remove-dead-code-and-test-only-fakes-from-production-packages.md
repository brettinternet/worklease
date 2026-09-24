---
id: TASK-136.5
title: Remove dead code and test-only fakes from production packages
status: To Do
assignee: []
created_date: '2026-09-24 15:05'
labels: []
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
- [ ] #1 `deadcode -test ./...` reports nothing except exceptions listed in the task notes with a reason for each
- [ ] #2 `authority.FakeAuthority` is gone from non-test code in `internal/authority`, and `internal/queue` tests still pass using an equivalent fake in test code
- [ ] #3 Every remaining entry in `deadcode ./...` output has been moved into a `_test.go` file of its only consuming package, or is listed in the task notes with a reason to keep it
- [ ] #4 `mise run lint`, `mise run format-check`, `mise run test`, and `mise run typecheck` pass
<!-- AC:END -->
