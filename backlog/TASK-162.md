---
id: TASK-162
title: Coordinate remote provider execution and fencing
status: draft
type: feature
priority: low
labels: [remote-authority, architecture]
related: [TASK-166]
rank: af9b658072f9b64a4
refs: [docs/remote-claim-authority.md, docs/claim-model.md]
created: 2026-09-16T06:03:00Z
updated: 2026-10-24T04:09:36Z
---

The remote authority coordinates claims but does not execute provider operations, prove provider cessation, or enforce provider-side fencing. Define an opt-in client/provider integration that carries fencing evidence to an enforcing consumer, retains exact outcome evidence, and does not turn the authority server into a general remote process runner.

## Acceptance criteria

- [ ] Before design, the item names a provider or consumer that can enforce a claim-revision fencing token

- [ ] That consumer rejects a stale holder's write in a test that uses a superseded claim revision

- [ ] The authority server still executes no provider operations
