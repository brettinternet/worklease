---
id: TASK-159
title: Name the holder in remote acquire contention errors
status: done
labels: [reviewed]
rank: ab909e175ab909d74
created: 2026-10-23T22:39:00Z
updated: 2026-10-24T01:30:10Z
---

Against a remote authority, acquiring a resource held by another agent fails with a generic "remote request failed" message, and the holder object has null resources and an empty session. Local contention names the held resource and holder session. Clients such as bl must fall back to their own wording, and operators cannot see who holds the claim or which resource contended. Found while verifying bl remote claims (backlog BL-72).

## Acceptance criteria

- [x] Remote contention results report a message stating the resource is already claimed
  Evidence: Remote acquire CLI regression asserts message resource is already claimed; go test -race -count=3 -run TestRemoteAcquireContentionReportsRedactedHolderAndClearsAttempt ./internal/cli passed as part of the focused run.
- [x] The holder in remote contention results includes the contended resources and holder session, matching local results
  Evidence: Local bundle and remote CLI tests assert holder session and ordered resources; remote test exercises a 32-resource bundle with 31 maximum-length resources. All focused regressions passed three times with race detection.
- [x] Tests cover remote contention output parity with local contention
  Evidence: Extended existing authority decoding, lease contention, output projection, and remote CLI regressions. mise run lint, format-check, typecheck, test, race, and hooks all passed. Independent reviewer found error-detail cap overflow; fixed with contention-specific 256 KiB bounded budget and verified maximum bundle.

## Notes

Plan: trace remote contention error serialization, preserve public holder metadata and useful contention wording, add one parity regression, run required checks, commit and merge locally. Worktree .worktrees/task-159 was created but the required user pre-start receipt hook failed parsing empty stdin; preserve it until cleanup is authorized.

Delivered commit 8f086ca, fast-forward merged into main. Independent review found one concrete large-bundle diagnostic truncation bug; corrected and reran lint, format-check, vet, full tests, full race suite, hooks, and focused race count=3 successfully. Worktree and task-159 branch removed with explicit user approval after receipt hook failure; no associated Herdr workspace existed. Expired original claim was recovered with explicit user authorization by archiving its inactive handle after proving all commands/reviewer stopped and resource expired without competing owner, then reclaiming via bl in the same loop session. No blockers or remaining steps; nothing pushed.

Review: docs still listed four holder fields and a 16 KiB details cap. Updated cli-reference contention example, go-rewrite-contract holder field list, and protocol section 7 to document the 256 KiB already-claimed details bound. doc-test and full gates passed.
