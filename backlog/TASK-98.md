---
id: TASK-98
title: Release Go rewrite as v1.0.0
status: done
type: chore
priority: high
assignees: ['@brett']
rank: acbe6d9601cbe6ce2
refs: [.github/workflows/ci.yml, .github/workflows/release.yml]
files: [.github/workflows/release.yml, README.md, internal/guard/guard.go, internal/guard/guard_test.go, internal/store/driver.go]
created: 2026-09-13T00:56:00Z
updated: 2026-09-13T01:35:00Z
---

The Go rewrite is complete locally, but the remote CI matrix is failing and the first stable Go release has not been published. The release must preserve the existing mise GitHub backend contract so users can continue selecting latest without changing their tool declaration.

## Acceptance criteria

- [x] CI passes on all configured Linux and macOS runners for the release commit
- [x] Release validation builds and smoke-tests mise-compatible Go archives for linux and macOS on x64 and arm64
- [x] GitHub release v1.0.0 publishes checksummed Go archives from the release commit
- [x] A clean mise installation using github:brettinternet/worklease = "latest" installs worklease v1.0.0

## Plan

1. Reproduce and correct the cross-platform CI failures without weakening coverage.
2. Add an end-to-end mise installation check to release validation while preserving the established archive naming/layout contract.
3. Run all repository quality gates and release validation locally.
4. Push main, wait for green CI, tag v1.0.0, then verify the release workflow, assets, checksums, version metadata, and a clean latest mise install.

## Notes

Confirmed the existing release archive names and bin/worklease layout install successfully through mise GitHub backend at v0.10.0. Fixed guarded exec output capture so readers drain owned pipes after the leader exits, and normalized deadline-bounded SQLite contention. Added post-publication latest-mise checks on all four release platforms and documented the unchanged mise declaration.

The first post-publication check exposed mise's intentional minimum-release-age filter: a clean latest lookup selected v0.9.1 immediately after publication. The release artifact itself installed successfully when that delay was disabled. Updated release verification to set MISE_MINIMUM_RELEASE_AGE=0 and to run on workflow_dispatch as well as tagged publication, allowing immediate post-release validation without changing users' simple latest declaration.

Verification: local mise run ci passed all format, staticcheck, vet, unit, race, vulnerability, end-to-end, and manual-generation gates. Main CI runs 34729583689 (release commit ca97d51) and 34729975577 (pipeline follow-up d81e4bd) passed all four OS/architecture jobs. Release v1.0.0 publishes four archives plus checksums from ca97d51. Release validation run 34729978282 passed archive build/smoke/manual checks and clean latest mise installation on linux-x64, linux-arm64, macos-x64, and macos-arm64; its first attempt hit a transient macOS artifact-upload DNS failure and the failed attempt passed on rerun. GitHub latest resolves to v1.0.0.

Final bookkeeping CI exposed one remaining macOS-only test race: the signal helper could call os.Exit(0) before its self-sent SIGTERM was delivered. Reopened to make the helper wait for signal delivery and restore a green final main commit.

CI run 34730446348 then exposed that the guarded-exec fixture used a one-second lease while the race detector under shared runner load took 1.86 seconds to start and finish a trivial helper. Increased only the test fixture lease/budget to ten seconds; production timeout behavior remains covered separately.

Final CI verification: main CI run 34730733395 for commit ea66c25 passed all four jobs (ubuntu-latest, ubuntu-24.04-arm, macos-14, and macos-15-intel), including repository hooks, quality gates, race tests, end-to-end smoke, and generated-manual validation.

## Summary

Released the Go rewrite as v1.0.0 with checksummed cross-platform archives, fixed the CI flakes that blocked the release, preserved and documented the simple mise latest declaration, and added four-platform post-publication mise installation checks. Local gates, release validation, clean latest mise installs, and final main CI run 34730733395 on all four configured runners pass.
