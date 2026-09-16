---
id: DRAFT-7
title: Add high availability and durable backend conformance
status: Draft
assignee: []
created_date: '2026-09-16 06:03'
labels:
  - remote-authority
  - architecture
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The hosted authority is intentionally one SQLite writer on one host and object backups provide disaster recovery only. When measured throughput or durability requirements justify it, define backend conformance, replica/failover semantics, split-brain prevention, recovery evidence, and an enforcing fencing consumer before considering Postgres or highly available serving.
<!-- SECTION:DESCRIPTION:END -->
