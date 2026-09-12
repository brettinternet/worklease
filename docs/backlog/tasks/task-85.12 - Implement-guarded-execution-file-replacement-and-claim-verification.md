---
id: TASK-85.12
title: 'Implement guarded execution, file replacement, and claim verification'
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.8
  - TASK-85.10
references:
  - src/worklease/execution.py
  - src/worklease/replacement.py
  - ../hum/internal/process
  - TASK-79
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 104000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Provide the operations that make a local lease useful around real agent work. Port guarded argv execution and expected-hash replacement using Go’s process and filesystem primitives, and add the non-mutating ownership verification requested by TASK-79 for agent tools that cannot run inside the guard. Reuse applicable POSIX process patterns from hum.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Guarded execution starts exact argv without a shell, renews and validates ownership, records intent before effects, returns the child result, and supports singleton and bundle leases.
- [ ] #2 Deadlines cover child runtime and inherited-pipe draining; timeout terminates the POSIX process group with bounded escalation and retains bounded stdout/stderr with byte and truncation metadata.
- [ ] #3 Expected-hash file replacement rejects unsafe paths and symlinks, records intent, revalidates ownership and content version, atomically renames and fsyncs the result, and replays deterministically.
- [ ] #4 A non-mutating verify operation checks current handle identity, credential, revision, expiry, and resources without changing the claim, history, checkpoint, or operation ledger, and states that it is not a fence around a later native edit.
- [ ] #5 Tests cover exit statuses, signals, escaped descendants, inherited pipes, large and invalid UTF-8 output, renewal loss, crash windows, CAS conflicts, permissions, replay, bundle execution, verification failures, and redaction.
<!-- AC:END -->
