---
id: TASK-166
title: Add high availability and durable backend conformance
status: draft
labels: [remote-authority, architecture]
rank: afd4e25b9efd4e17c
created: 2026-09-16T06:03:00Z
updated: 2026-09-16T06:03:00Z
---

The hosted authority is intentionally one SQLite writer on one host and object backups provide disaster recovery only. When measured throughput or durability requirements justify it, define backend conformance, replica/failover semantics, split-brain prevention, recovery evidence, and an enforcing fencing consumer before considering Postgres or highly available serving.
