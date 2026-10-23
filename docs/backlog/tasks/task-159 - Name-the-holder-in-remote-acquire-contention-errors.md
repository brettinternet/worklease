---
id: TASK-159
title: Name the holder in remote acquire contention errors
status: To Do
assignee: []
created_date: '2026-10-23 22:39'
labels: []
dependencies: []
ordinal: 102000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Against a remote authority, acquiring a resource held by another agent fails with a generic "remote request failed" message, and the holder object has null resources and an empty session. Local contention names the held resource and holder session. Clients such as bl must fall back to their own wording, and operators cannot see who holds the claim or which resource contended. Found while verifying bl remote claims (backlog BL-72).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Remote contention results report a message stating the resource is already claimed
- [ ] #2 The holder in remote contention results includes the contended resources and holder session, matching local results
- [ ] #3 Tests cover remote contention output parity with local contention
<!-- AC:END -->
