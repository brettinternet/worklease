---
id: TASK-169
title: Support safe hosted configuration reload
status: draft
labels: [remote-authority]
rank: af53896e7bf538896
created: 2026-09-16T06:03:00Z
updated: 2026-09-16T06:03:00Z
---

Hosted configuration changes currently require stop-before-start. Define a fail-closed reload boundary for settings that can change safely, explicitly retaining restart requirements for authority identity, storage, TLS trust, admitted-prefix, or limit changes whose in-flight semantics cannot be preserved.
