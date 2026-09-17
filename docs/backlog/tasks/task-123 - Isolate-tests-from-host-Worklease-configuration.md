---
id: TASK-123
title: Isolate tests from host Worklease configuration
status: Done
assignee:
  - '@pi'
created_date: '2026-09-17 00:15'
updated_date: '2026-09-17 03:10'
labels:
  - test
  - config
  - safety
dependencies: []
references:
  - internal/cli/doctor_commands_test.go
  - internal/cli/authority_context.go
  - internal/testkit/environment.go
  - internal/testkit/subprocess_unix.go
  - internal/mcp/mcp_test.go
  - internal/mcp/acceptance_regression_test.go
  - internal/config/profile.go
  - internal/doctor/doctor_test.go
  - mise.toml
  - lefthook.yml
  - 46d373f
modified_files:
  - cmd/worklease-release/isolation_test.go
  - cmd/worklease-remote-smoke/isolation_test.go
  - cmd/worklease/isolation_test.go
  - internal/authority/isolation_test.go
  - internal/cli/isolation_regression_test.go
  - internal/cli/isolation_test.go
  - internal/config/isolation_test.go
  - internal/doctor/isolation_test.go
  - internal/gc/isolation_test.go
  - internal/guard/isolation_test.go
  - internal/handle/isolation_test.go
  - internal/lease/isolation_test.go
  - internal/ledger/isolation_test.go
  - internal/mcp/isolation_test.go
  - internal/output/isolation_test.go
  - internal/reason/isolation_test.go
  - internal/release/isolation_test.go
  - internal/resource/isolation_test.go
  - internal/server/isolation_test.go
  - internal/setup/isolation_test.go
  - internal/store/isolation_test.go
  - internal/testkit/environment.go
  - internal/testkit/isolation_test.go
  - internal/testkit/testkit_test.go
  - internal/watch/isolation_test.go
priority: high
type: bug
ordinal: 165000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Go test suite can read the operator’s real Worklease configuration. clearWorkleaseEnvironment currently blanks XDG roots without replacing HOME and omits WORKLEASE_PROFILE and WORKLEASE_SERVER_CONFIG. A temporary --home or WORKLEASE_HOME isolates local state, not profile selection: authorityFor still loads user profiles and bindings, which can select a remote authority. Test subprocesses also inherit unsanitized environment entries.

Make the Go test suite (go test ./..., including test-spawned processes) independent of caller Worklease configuration and state. Preserve intentional temporary remote-server fixtures and environment/precedence tests. Never inspect, modify, or use actual operator profiles to reproduce this bug; model a hostile caller entirely in temporary directories.

Scope: shared test utilities, affected Go package tests, and existing mise/hook entry points as needed. Do not change production configuration precedence, force --local globally, disable remote tests, or build a general process sandbox. Standalone smoke/doc-test/VM harness hardening is not required here unless a Go test invokes that path; record adjacent gaps separately.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 All tests and spawned test subprocesses use temporary HOME, XDG_CONFIG_HOME, and XDG_STATE_HOME directories rather than host paths
- [x] #2 Test setup clears or overrides every Worklease configuration variable, including remote profile and server configuration selection
- [x] #3 A regression test seeds sentinel host configuration and verifies that tests neither read it nor attempt network access to its endpoint
- [x] #4 Test isolation applies consistently through mise run test and repository hooks without requiring callers to sanitize their shell
- [x] #5 Focused tests and the full test suite pass when the real user configuration contains a selected remote profile
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Establish the failing boundary safely. Inventory configuration consumers and all test child-process launches under internal/ and cmd/. Start with clearWorkleaseEnvironment (internal/cli/doctor_commands_test.go), clearDoctorEnvironment, internal/mcp/mcp_test.go and acceptance_regression_test.go, internal/config/config_test.go, and internal/testkit/environment.go. Record which packages need process-level protection, per-test fixtures, or explicit child environments; pure tests with injected inputs need no redundant setup.
2. Extend existing internal/testkit environment utilities rather than introducing a separate framework. Provide an owner-private temporary HOME plus nonempty XDG_CONFIG_HOME and XDG_STATE_HOME. Remove inherited WORKLEASE_* entries before applying explicit test overrides (including profile/server-config, handles, identity, sessions, durations, and acceptance fault switches). Keep Worklease state distinct from HOME. Preserve Environment’s deterministic output and Git-variable filtering and Home’s no-process-mutation contract; inspect all callers before changing either contract.
3. Install isolation before application configuration is resolved. Prefer package TestMain setup for packages requiring ambient protection, with cleanup before os.Exit, plus a shared testing.TB setup helper for tests that need fresh per-test defaults. Replace partial clearing helpers so they cannot restore host fallback. For parallel tests, use injected environment maps or startup isolation rather than t.Setenv. Tests specifically covering unset/blank XDG fallback must retain temporary HOME. Avoid global WORKLEASE_HOME defaults that mask default-path and config-source assertions.
4. Audit and repair child boundaries, not just parent setup: CLI helper re-execs, MCP go-run processes, store/lease contention helpers, guard/handle children, and testkit.RunTestProcess. Build child environments from sanitized roots and add explicit fixture overrides last. Re-executed test binaries must preserve deliberately supplied fixture roots and required helper-mode/coordination values; blindly clearing WORKLEASE_* again in TestMain can break helpers or recurse. Preserve PATH and needed Go build/module cache settings so replacing HOME does not cause unnecessary downloads or tool discovery failures. Add focused tests for duplicate-key replacement, cleanup, explicit overrides, and child/grandchild inheritance.
5. Add bounded hostile-caller regression coverage using only temporary directories. Seed owner-private profiles.yaml, bindings.yaml, config.yaml, server.yaml, credentials/handle/state sentinels, and a selected profile pointing to a local request-counting HTTP server. Cover inherited WORKLEASE_PROFILE and WORKLEASE_SERVER_CONFIG, default profile and project binding, explicit hostile XDG roots, and empty/unset XDG fallback to fake HOME. Exercise representative local CLI and MCP operations both in-process and through a child after isolation. Assert resolved paths remain in test-owned roots, local behavior succeeds, sentinels remain unchanged, and the hostile endpoint receives zero requests. Also use invalid/poisoned configuration to prove files are not consumed: unchanged files and zero requests alone do not prove absence of reads. Verify the fixture would select the hostile profile without isolation using configuration resolution only, never a real remote operation. Keep explicit temporary remote tests working.
6. Verify entry points and scope. mise.toml currently runs go test ./... directly; lefthook.yml delegates Go changes to mise run test. Prefer fixing tests themselves so direct go test works too; add a wrapper only if a demonstrated gap remains. Force fresh runs with -count=1 (or GOFLAGS=-count=1 for mise/hooks). Run focused testkit/config/cli/mcp/doctor tests, the full suite, and race tests under synthetic poisoned caller settings. Exercise the hook test job explicitly even when staged task-only files would normally skip it. Never use the actual user configuration as a test fixture.
7. Before delivery run mise run lint, mise run format-check, mise run test, and mise run typecheck; stage intended files, install hooks with mise run hooks-install, and run mise run hooks. Record commands, hostile-fixture cases, subprocess coverage, and any residual harness gaps in this task. Read the finalization guide before checking acceptance criteria. Keep implementation and evidence in one bounded task; no prerequisite task is currently identified.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement only: implementation has not started and all acceptance criteria remain unchecked. AC #1 refers to ambient Worklease configuration/state paths for the Go test suite and its child processes, not forbidding Go toolchain/cache access. AC #2 excludes intentional test-authored overrides after sanitization. AC #3 must prove configuration is not consumed as well as no requests/mutations. AC #5 must be verified with a synthetic equivalent of a selected user remote profile, never by accessing or editing real operator configuration. Existing remote fixtures remain authorized network traffic; only the hostile sentinel endpoint must see zero requests.

Claimed for implementation under local Worklease claim b55e8ea1ff1…cf5c6237ef6e (session 01a0ad43-b587-708f-9f93-d92635e17f41). Provider writes are locally coordinated, not provider-fenced.

Implemented shared process isolation with owner-private HOME/XDG roots and complete WORKLEASE_* sanitization across every Go package that has tests. Recognized re-exec helpers preserve only the variables for their explicitly selected helper test. Added a synthetic hostile selected-profile/config regression covering in-process and child CLI execution, poisoned config non-consumption, and zero endpoint requests.

Verification passed: focused go tests for testkit/cli/config/mcp/doctor; mise run lint; mise run format-check; mise run test; mise run typecheck; hostile HOME/XDG/WORKLEASE_* full go test ./... -count=1; mise run hooks; commit hooks. go test -race ./... exposed the pre-existing timing-sensitive internal/mcp claim-expiry failure noted during implementation; the ordinary full suite and focused MCP suite passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Isolated every Go test package and its re-exec helpers from caller HOME, XDG, and Worklease configuration. Added hostile remote-profile regression coverage for in-process and child execution. Merged commit 46d373f; required checks, hooks, focused tests, and a full synthetic-hostile-environment suite passed.
<!-- SECTION:FINAL_SUMMARY:END -->
