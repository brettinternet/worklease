---
id: TASK-67
title: Add a minimal Worklease GIF demo
status: Done
assignee:
  - '@brett'
created_date: '2026-09-11 21:33'
updated_date: '2026-09-11 21:53'
labels: []
dependencies: []
modified_files:
  - README.md
  - docs/simple-demo.tape
  - docs/simple-demo.gif
type: docs
ordinal: 71000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add a second, deliberately minimal terminal demo that shows the basic acquire-and-release lifecycle with fewer flags than the existing two-worker contention demo, so first-time readers can understand Worklease quickly.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A reproducible VHS tape renders the simplest useful single-worker acquire-and-release scenario
- [x] #2 The rendered GIF uses only the flags needed to make the lifecycle clear
- [x] #3 The README presents the minimal demo without removing access to the existing contention demo
- [x] #4 The rendered demo is visually verified and project quality gates pass
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a compact single-pane VHS tape that hides setup and visibly runs only acquire and release against one resource.
2. Render the new GIF and present it immediately above the existing two-worker demo in the README introduction.
3. Inspect representative GIF frames and run all repository quality gates.

4. Remove output-truncation pipes from the visible lifecycle commands, enlarge the terminal for complete output, and rerender the GIF.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Added docs/simple-demo.tape and rendered docs/simple-demo.gif as an 11-second, 1200×800 single-pane acquire/work/release lifecycle. The visible acquire uses only resource and lease-file arguments; release uses lease-file and reason. Both commands display their complete native output without truncation pipes. The lease file remains because it is the simplest safe way to retain the acquisition credential and revision for the later release; omitting it would require exposing and passing token, claim ID, and revision separately. README presents the minimal demo immediately above the original two-worker contention demo.

Validation: VHS 0.11.0 rendered the GIF successfully. A four-frame contact sheet at three-second intervals visually confirmed the complete command, output, work, and release sequence without clipping. ffprobe reported duration=11.000000 and size=167314. mise run lint, mise run format-check, mise run test (321 tests), mise run typecheck, and git diff --check passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added a concise single-worker GIF immediately above the existing contention demo. The demo now shows unpiped native acquire and release output while retaining the lease file required for a simple safe release. Verified visually and passed all project quality gates.
<!-- SECTION:FINAL_SUMMARY:END -->
