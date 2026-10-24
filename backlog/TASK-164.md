---
id: TASK-164
title: Support secure cross-host claim transfer
status: draft
type: feature
priority: low
labels: [remote-authority]
rank: afb823ee08fb82310
refs: [docs/remote-claim-authority.md, docs/remote-authority-protocol-v1.md]
created: 2026-09-16T06:03:00Z
updated: 2026-10-24T04:09:37Z
---

Claim transfer is limited to one host because copying bearer credentials between installations would weaken the installation trust boundary. Define an authenticated cross-host handoff that preserves revision fencing, exact replay, installation attribution, pending recovery, and explicit recipient acceptance without exposing or copying the predecessor credential.

## Acceptance criteria

- [ ] Before design, the item records a workflow that needs a claim handed to another installation

- [ ] A claim moves between two installations only after the recipient explicitly accepts, and the predecessor credential is never copied or exposed

- [ ] Tests across two installations cover revision fencing, exact replay, installation attribution, and pending recovery
