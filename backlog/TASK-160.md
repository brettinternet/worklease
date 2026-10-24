---
id: TASK-160
title: Document and drill hosted authority backups
status: draft
type: feature
priority: medium
labels: [remote-authority, backup]
related: [TASK-163, TASK-166]
rank: af452a37b0f452960
refs: [docs/container.md, docs/remote-claim-authority.md]
created: 2026-09-16T06:03:00Z
updated: 2026-10-24T04:09:36Z
---

Hosted authorities can restore an owner-supplied SQLite backup through `server restore`, but Worklease ships no verified way to create one. docs/container.md only says to use "a supported online SQLite backup or replication integration" and does not name or test one. An operator who copies the live database file without its WAL can end up with a backup that cannot be restored.

Start with the simplest option: document and drill one external online-replication tool (for example Litestream) against S3-compatible storage, feeding the existing recovery-mode restore. Build backup support into Worklease only for gaps the drill records, such as scheduling, retention, artifact verification, or recording the durable cutoff. This is disaster recovery, not high availability (see TASK-166).

## Acceptance criteria

- [ ] docs/container.md documents one tested online-backup recipe for S3-compatible storage that keeps credentials and replica state outside the authority home

- [ ] A drill restores that backup through worklease server restore, enters recovery mode, and records the selected durable cutoff; the drill evidence is retained

- [ ] The item records which gaps (scheduling, retention, verification, cutoff recording) the external recipe leaves; built-in backup support is added only for those gaps
