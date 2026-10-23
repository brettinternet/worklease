---
id: TASK-167
title: Serve multiple isolated authority namespaces
status: draft
labels: [remote-authority, architecture]
rank: afe3419269fe340b2
created: 2026-09-16T06:03:00Z
updated: 2026-09-16T06:03:00Z
---

One server process currently serves one namespace. Define multi-namespace routing only when deployment demand justifies isolated configuration, locks, credentials, admission limits, recovery state, observability, and lifecycle operations without allowing identity or replay state to cross namespace boundaries.
