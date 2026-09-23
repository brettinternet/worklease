---
id: DRAFT-13
title: Public claim attributes for work-item associations
status: Draft
assignee: []
created_date: '2026-09-23 16:23'
labels:
  - authority
  - claims
dependencies: []
references:
  - docs/claim-model.md
  - internal/store/schema.go
  - internal/instructions/instructions.go
documentation:
  - docs/backlog/docs/remote-authority/doc-4 - Remote-Authority-Protocol-V1.md
priority: low
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Users want to find the coding-agent session (and similar context) that worked a given item. Upstream providers have no place for this, so Worklease is the natural home. Claim epochs already retain agentId, sessionId, and workKey, and agent instructions ask harnesses to pass their own loop-run ID as the session. Two gaps remain: when no selector is given, Worklease generates the claim sessionId, so it is not the harness session; and there is nowhere to record other non-secret associations such as the harness name, transcript reference, worktree, or PR URL. The checkpoint is the wrong home: it is private recovery metadata, overwritten on every write, and redacted from public views. This changes the claims/epochs schema, Remote Authority Protocol V1, and the redaction rules in docs/claim-model.md, so it should wait for demonstrated need rather than join the work-queue slices.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Before designing, record whether requiring harnesses to supply their own session ID (closing the generated-sessionId gap) meets the need without new attributes; if so, implement only that and close this task
- [ ] #2 If attributes are needed: a bounded set of non-secret key/value attributes can be supplied at acquire time (CLI and MCP), with documented key syntax, count, and size limits enforced by the authority
- [ ] #3 Attributes are retained with the claim epoch and returned by status and resource history for both local and remote authorities
- [ ] #4 docs/claim-model.md defines attributes as public, non-secret metadata, and Remote Authority Protocol V1 is updated for the new fields
- [ ] #5 Tests cover limits, retention after release, and remote round-trip
<!-- AC:END -->
