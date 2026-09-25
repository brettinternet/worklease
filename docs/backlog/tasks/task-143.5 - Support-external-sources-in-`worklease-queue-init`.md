---
id: TASK-143.5
title: Support external sources in `worklease queue init`
status: To Do
assignee: []
created_date: '2026-09-25 16:38'
labels:
  - work-queue
  - external-adapter
milestone: m-1
dependencies:
  - TASK-143.4
documentation:
  - docs/queue.md
  - docs/external-adapter-protocol.md
parent_task_id: TASK-143
priority: medium
type: feature
ordinal: 77000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`worklease queue init` (TASK-137) generates queue.yaml only for Backlog.md and GitHub. Configuring an external source means hand-writing the executable, expected identity and version, config, and claim binding, then discovering the separate approval step. Reading these values from the adapter's manifest removes that manual work and its typos.

The user wants the CLI approached with API-driven design: every command added here is a stable, scriptable interface first, with `--json` output in the existing CLI envelope (`schemaVersion`, `ok`, `operation`, and `error.reason`/`exitCode`/`details`), stable diagnostic reasons, and documented exit codes, and the human output is a rendering of that same result.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease queue init --adapter external --executable PATH` starts the adapter only to read its manifest, then proposes a source with the executable path, expected adapter ID and version from the manifest, and config validated against the manifest's config schema.
- [ ] #2 Claims stay read-only unless `--portable-claims SOURCE` is given; init never enables claims, writes, or credential use implicitly.
- [ ] #3 Init never approves the adapter; its `--json` result includes the exact next command (`queue adapter approve --source ID`) and the executable digest to review.
- [ ] #4 Manifest failures, schema-invalid config, and existing sources produce stable diagnostics in the JSON envelope, and queue.yaml is left unchanged on any failure.
- [ ] #5 docs/queue.md documents the flow.
<!-- AC:END -->
