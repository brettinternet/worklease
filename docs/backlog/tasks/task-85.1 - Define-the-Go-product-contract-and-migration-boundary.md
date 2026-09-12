---
id: TASK-85.1
title: Define the Go product contract and migration boundary
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
labels:
  - go-rewrite
milestone: m-0
dependencies: []
references:
  - README.md
  - docs/claim-model.md
  - docs/cli-reference.md
  - ../hum/internal/cli/root.go
  - ../hum/internal/config/config.go
parent_task_id: TASK-85
priority: high
type: spike
ordinal: 93000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Python implementation is a proof of concept rather than a compatibility target, but it contains hard-won safety behavior mixed with unused APIs and speculative extensibility. Establish the intended Go product surface before agents reproduce accidental complexity or delete important guarantees. The contract should favor the owner’s current workflows, POSIX semantics, urfave/cli v3, typed configuration, and clean extension seams without preserving Python packaging, persistence, or exact output.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A durable design document inventories each current CLI, MCP, storage, execution, history, maintenance, resource-policy, setup, and workflow capability as retain, redesign, defer, or remove, with rationale.
- [ ] #2 The document defines the new command tree, machine-output contract, error model, configuration precedence including flags, environment and optional YAML, and the POSIX platform boundary.
- [ ] #3 Security and correctness invariants cover ownership epochs, expiry, stale-owner rejection, idempotency, unknown outcomes, bundles, secret handling, filesystem safety, guarded child termination, and the limit of same-host coordination.
- [ ] #4 The document explicitly retires the Python core API, source-provider SDK, Python entry-point plugins, old SQLite and lease-file compatibility, and identifies any genuinely useful extension seam retained in Go.
- [ ] #5 SQLite driver and MCP implementation choices have bounded proof criteria, and unresolved choices are converted into explicit dependent spikes rather than left to implementers.
<!-- AC:END -->
