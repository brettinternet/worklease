---
id: TASK-165
title: Support repository enrollment for host-local resources
status: draft
type: feature
priority: low
labels: [remote-authority]
rank: afc68324d3fc68246
refs: [docs/remote-claim-authority.md, docs/claim-model.md]
created: 2026-09-16T06:03:00Z
updated: 2026-10-24T04:09:37Z
---

Remote enrollment currently excludes host-local path, backlog-md, and markdown resource identities because different checkout roots may not identify the same object. Define repository enrollment and portable locator vectors that map equivalent repositories and resources across supported platforms without allowing repository content to select profiles or redirect trust.

## Acceptance criteria

- [ ] Before design, the item records a workflow that needs host-local resource keys shared across checkout roots

- [ ] Locator vectors map equivalent repositories and resources on Linux and macOS

- [ ] A test shows repository content cannot select a profile or redirect trust
