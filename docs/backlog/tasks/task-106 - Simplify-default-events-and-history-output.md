---
id: TASK-106
title: Remove synthetic row numbers and simplify lifecycle timelines
status: To Do
assignee: []
created_date: '2026-09-13 06:51'
updated_date: '2026-09-13 06:51'
labels: []
dependencies: []
priority: medium
type: enhancement
ordinal: 131000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Human-readable collection output still reads like serialized records rather than a compact view. Synthetic row numbers are not referenced elsewhere, collection headers already establish row context, and identifiers and sequence metadata make routine timeline scanning noisy. Remove synthetic numbering wherever it adds no stable identity, and keep complete lifecycle diagnostics available through `--full` while making default events and history output comparable in density to `worklease list`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Default `worklease events` text rows omit synthetic row numbers, sequence values, claim IDs, and redundant field labels while retaining event kind, summarized resources when present, and relative time.
- [ ] #2 Default resource-specific `worklease history` text rows omit synthetic row numbers, claim IDs, and redundant field labels while retaining agent, status, acquisition and end timing, end reason, and the compact operation summary.
- [ ] #3 `--full` retains complete non-secret event and history metadata, including event sequence, complete claim identifiers, operation metadata, and absolute timestamps, without adding synthetic row numbers.
- [ ] #4 Other human-readable collection views, including full resource status and acquire recovery details, omit synthetic row numbers when rows have no stable user-facing identity.
- [ ] #5 Gap and pruning notices remain visible in compact text because they affect interpretation of the timeline.
- [ ] #6 JSON output and cursor behavior remain unchanged.
- [ ] #7 Tests cover compact and full events and history output, resource-less authority events, and every other collection view changed by the numbering rule.
<!-- AC:END -->
