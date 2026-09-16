---
id: DRAFT-8
title: Serve multiple isolated authority namespaces
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
One server process currently serves one namespace. Define multi-namespace routing only when deployment demand justifies isolated configuration, locks, credentials, admission limits, recovery state, observability, and lifecycle operations without allowing identity or replay state to cross namespace boundaries.
<!-- SECTION:DESCRIPTION:END -->
