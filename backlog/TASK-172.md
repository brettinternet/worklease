---
id: TASK-172
title: Public claim attributes for work-item associations
status: draft
type: feature
priority: low
labels: [authority, claims]
rank: af7ea712dcf7ea638
refs: [docs/claim-model.md, internal/store/schema.go, internal/instructions/instructions.go, docs/remote-authority-protocol-v1.md]
created: 2026-09-23T16:23:00Z
updated: 2026-10-23T22:58:23Z
---

Users want to find the coding-agent session (and similar context) that worked a given item. Claim epochs already retain agentId, sessionId, and workKey, and agent instructions ask harnesses to pass their own loop-run ID as the session. Two gaps remain: when no selector is given, Worklease generates the claim sessionId, so it is not the harness session; and there is nowhere to record other non-secret associations such as the harness name, transcript reference, worktree, or PR URL. The checkpoint is the wrong home: it is private recovery metadata, overwritten on every write, and redacted from public views. Task trackers such as `bl` would read these associations through the `--json` claim CLI. This changes the claims/epochs schema, Remote Authority Protocol V1, and the redaction rules in docs/claim-model.md, so it should wait for demonstrated need.

## Acceptance criteria

- [ ] Before designing, record whether requiring harnesses to supply their own session ID (closing the generated-sessionId gap) meets the need without new attributes; if so, implement only that and close this task
- [ ] If attributes are needed: a bounded set of non-secret key/value attributes can be supplied at acquire time (CLI and MCP), with documented key syntax, count, and size limits enforced by the authority
- [ ] Attributes are retained with the claim epoch and returned by status and resource history for both local and remote authorities
- [ ] docs/claim-model.md defines attributes as public, non-secret metadata, and Remote Authority Protocol V1 is updated for the new fields
- [ ] Tests cover limits, retention after release, and remote round-trip
