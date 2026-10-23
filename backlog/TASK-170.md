---
id: TASK-170
title: Add an authenticated browser control plane
status: draft
labels: [remote-authority, control-plane]
rank: af61e8a546f61e7cc
created: 2026-09-16T06:03:00Z
updated: 2026-09-16T06:03:00Z
---

Remote administration is CLI-only with installation credentials and no browser or OAuth surface. Add a browser control plane only when deployment demand justifies a separately authenticated and authorized interface with CSRF protection, secret-safe enrollment, role enforcement, auditability, and no trust in arbitrary proxy identity headers.
