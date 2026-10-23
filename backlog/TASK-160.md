---
id: TASK-160
title: Add managed object-storage backups for hosted authorities
status: draft
labels: [remote-authority, backup]
rank: af452a37b0f452960
created: 2026-09-16T06:03:00Z
updated: 2026-09-16T06:03:00Z
---

Hosted authorities can restore an owner-supplied SQLite backup, but Worklease does not create, schedule, upload, retain, verify, or select production backups. Define an operator-facing online SQLite backup integration, initially for S3-compatible object storage, that preserves WAL consistency, records durable cutoffs, protects credentials, verifies artifacts, and feeds only explicitly selected backups into the existing recovery-mode restore workflow. This remains disaster recovery rather than high availability.
