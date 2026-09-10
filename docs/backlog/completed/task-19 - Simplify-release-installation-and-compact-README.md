---
id: TASK-19
title: Simplify release installation and compact README
status: Done
assignee:
  - '@codex-main'
created_date: '2026-07-14 21:35'
updated_date: '2026-07-14 21:43'
labels: []
dependencies: []
modified_files:
  - README.md
  - .github/workflows/release.yml
  - scripts/release_artifacts.py
  - scripts/release_installer.py
  - tests/test_release.py
priority: medium
type: enhancement
ordinal: 20000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the project easier to adopt by publishing mise-autodetectable native release archives, documenting the one-line configuration, and reducing README verbosity while preserving the essential user and agent contract. Investigate current GitHub Actions failures and record the observed cause or authentication blocker without broadening into unapproved fixes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A release publishes one standard archive per supported platform containing an executable named worklease in an autodetected binary location
- [x] #2 Users can configure `"github:brettinternet/worklease" = "latest"` without matching or bin options
- [x] #3 README installation and operational guidance is materially shorter while retaining essential lifecycle, safety, and provider-authority instructions
- [x] #4 Release policy tests and repository quality gates pass
- [x] #5 Current GitHub Actions failures are diagnosed from available evidence or explicitly recorded as blocked by authentication
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inspect README, release asset conventions, tests, and current GitHub checks. 2. Package native executables into standard per-platform archives with a bin/worklease layout and update release verification/tests. 3. Compact README around installation, core lifecycle, guarantees, and links to detailed skills/docs. 4. Run release-focused checks and all repository quality gates. 5. Independently verify acceptance criteria, record evidence, and commit the scoped changes.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Compacted README from 533 lines/3,740 words to 195 lines/1,153 words while retaining install, lifecycle, credential, revision/idempotency, unknown-outcome, provider-authority, agent-loop, compatibility, and development guidance. Replaced mise matching/bin configuration with the plain GitHub backend declaration.

Changed native releases to standard worklease-vX.Y.Z-{linux|macos}-{x64|arm64}.tar.gz assets containing executable bin/worklease. The exact installer verifies checksums, reads only that regular executable member, installs atomically, and preserves wheel fallback. Added naming, layout, executable-mode, install, checksum, and workflow regressions.

CI investigation: runs 29339209387, 29360693688, and 29369996934 consistently fail test_verbose_status_is_redacted_deterministic_and_read_only on hosted quality runners because a read-only WAL-mode SQLite connection creates transient leases.sqlite3-wal and leases.sqlite3-shm sidecars; the assertion compares the full filesystem tree and treats these bookkeeping files as logical mutation. Latest native jobs pass. No CI test fix was made because the request authorized investigation, not a fix.

Local verification: mise run lint passed; mise run format-check passed (42 files); mise run typecheck passed; mise run test passed (128 tests); mise run build passed; focused release suite passed (11 tests); git diff --check passed.

Independent verifier: AC1 PASS from four-platform archive matrix, exact executable layout, and 11 release tests; AC2 configuration is supported by mise autodetection and documented without options, with live installation of the newly packaged format deferred until the next tagged release; AC3 PASS from 69% README word reduction with required contract retained; AC4 PASS from focused gates and root full-suite evidence; AC5 PASS from recorded GitHub run diagnosis. Archive extraction safely selects only the exact regular executable member and atomically replaces the target; verifier noted only low residual risk that a checksum-valid invalid executable is smoke-tested after replacement, matching prior installer behavior.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Compacted README by 69% while preserving the user lifecycle and agent/provider safety contract. Changed native releases to mise-autodetected platform archives containing bin/worklease, updated the verified atomic installer, and added release regressions. Diagnosed three recurring CI failures as an over-strict filesystem-tree assertion against transient SQLite WAL/SHM sidecars; no unrequested CI fix was included. Verified 128 tests, 11 focused release tests, lint, formatting, typecheck, build, diff checks, and an independent acceptance review.
<!-- SECTION:FINAL_SUMMARY:END -->
