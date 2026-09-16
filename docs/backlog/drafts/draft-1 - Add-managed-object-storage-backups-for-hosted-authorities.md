---
id: DRAFT-1
title: Add managed object-storage backups for hosted authorities
status: Draft
assignee: []
created_date: '2026-09-16 06:03'
labels:
  - remote-authority
  - backup
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Hosted authorities can restore an owner-supplied SQLite backup, but Worklease does not create, schedule, upload, retain, verify, or select production backups. Define an operator-facing online SQLite backup integration, initially for S3-compatible object storage, that preserves WAL consistency, records durable cutoffs, protects credentials, verifies artifacts, and feeds only explicitly selected backups into the existing recovery-mode restore workflow. This remains disaster recovery rather than high availability.
<!-- SECTION:DESCRIPTION:END -->
