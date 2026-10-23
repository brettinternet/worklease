---
id: TASK-164
title: Support secure cross-host claim transfer
status: draft
labels: [remote-authority]
rank: afb823ee08fb82310
created: 2026-09-16T06:03:00Z
updated: 2026-09-16T06:03:00Z
---

Claim transfer is limited to one host because copying bearer credentials between installations would weaken the installation trust boundary. Define an authenticated cross-host handoff that preserves revision fencing, exact replay, installation attribution, pending recovery, and explicit recipient acceptance without exposing or copying the predecessor credential.
