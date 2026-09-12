---
id: TASK-93
title: Omit zero requestNotAfter from public op inspect and history output
status: To Do
assignee:
  - '@brett'
created_date: '2026-09-12 22:39'
labels:
  - go-rewrite
dependencies: []
references:
  - internal/ledger/ledger.go
priority: low
type: chore
ordinal: 118000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
ledger.Operation uses a time.Time for RequestNotAfter with omitempty, which never omits a zero time. Public op inspect and history therefore print "requestNotAfter":"0001-01-01T00:00:00Z" for every operation, while the real deadline is only filled in for authenticated --full inspection. Machine consumers cannot tell "not disclosed" from a real value.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Public op inspect and history omit requestNotAfter entirely instead of emitting the zero time
- [ ] #2 Authenticated op inspect --full still returns the recorded deadline and a test covers both projections
<!-- AC:END -->
