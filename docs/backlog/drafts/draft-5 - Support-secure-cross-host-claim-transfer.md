---
id: DRAFT-5
title: Support secure cross-host claim transfer
status: Draft
assignee: []
created_date: '2026-09-16 06:03'
labels:
  - remote-authority
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Claim transfer is limited to one host because copying bearer credentials between installations would weaken the installation trust boundary. Define an authenticated cross-host handoff that preserves revision fencing, exact replay, installation attribution, pending recovery, and explicit recipient acceptance without exposing or copying the predecessor credential.
<!-- SECTION:DESCRIPTION:END -->
