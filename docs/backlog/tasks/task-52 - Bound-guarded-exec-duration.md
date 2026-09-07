---
id: TASK-52
title: Bound guarded exec duration
status: To Do
assignee: []
created_date: '2026-09-07 03:28'
labels:
  - exec
dependencies: []
references:
  - src/worklease/execution.py
priority: medium
type: enhancement
ordinal: 53000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`exec` drains child output to EOF with no upper bound while renewing the lease indefinitely and holding the non-blocking resource flock, so every other command on that resource fails `resource-guarded` for as long as the child (or a grandchild holding the inherited pipes) runs. Add an explicit maximum duration (flag with a documented default or opt-in), terminate the process group on expiry, record the operation as failed or unknown-outcome as appropriate, and document the caveat about grandchildren inheriting pipes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 exec and exec-bundle accept a maximum duration; exceeding it terminates the child process group and returns a documented reason and exit code
- [ ] #2 The operation ledger records the timed-out operation so inspect-operation shows its state
- [ ] #3 README and help text document the bound and the grandchild pipe caveat; tests cover a child that outlives the bound
<!-- AC:END -->
