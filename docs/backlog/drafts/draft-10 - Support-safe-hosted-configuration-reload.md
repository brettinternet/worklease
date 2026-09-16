---
id: DRAFT-10
title: Support safe hosted configuration reload
status: Draft
assignee: []
created_date: '2026-09-16 06:03'
labels:
  - remote-authority
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Hosted configuration changes currently require stop-before-start. Define a fail-closed reload boundary for settings that can change safely, explicitly retaining restart requirements for authority identity, storage, TLS trust, admitted-prefix, or limit changes whose in-flight semantics cannot be preserved.
<!-- SECTION:DESCRIPTION:END -->
