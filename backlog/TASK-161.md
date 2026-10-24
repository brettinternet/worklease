---
id: TASK-161
title: Support remote file replacement
status: draft
type: feature
priority: low
labels: [remote-authority]
rank: af8d0649a7f8d056e
refs: [docs/claim-model.md, docs/remote-authority-protocol-v1.md, docs/go-rewrite-contract.md]
created: 2026-09-16T06:03:00Z
updated: 2026-10-24T04:09:36Z
---

Remote profiles currently reject replace-file because the authority is remote while the filesystem effect is client-local. Define a safe client-executed replacement protocol that preserves claim ownership, exact pending recovery, bounded execution, local path validation, and the existing rule that the server never executes filesystem effects.

## Acceptance criteria

- [ ] Before design, the item records a concrete remote workflow that needs guarded file replacement

- [ ] Remote replace-file runs on the client under a valid remote claim, and the server performs no filesystem effect

- [ ] An unknown-outcome replacement recovers from its exact pending request without applying twice

- [ ] docs/claim-model.md and Remote Authority Protocol V1 state the mutation protection remote replacement reports; it does not claim local-serialized-replace
