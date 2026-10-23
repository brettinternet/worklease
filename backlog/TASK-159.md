---
id: TASK-159
title: Name the holder in remote acquire contention errors
status: todo
rank: ab909e175ab909d74
created: 2026-10-23T22:39:00Z
updated: 2026-10-23T22:39:00Z
---

Against a remote authority, acquiring a resource held by another agent fails with a generic "remote request failed" message, and the holder object has null resources and an empty session. Local contention names the held resource and holder session. Clients such as bl must fall back to their own wording, and operators cannot see who holds the claim or which resource contended. Found while verifying bl remote claims (backlog BL-72).

## Acceptance criteria

- [ ] Remote contention results report a message stating the resource is already claimed
- [ ] The holder in remote contention results includes the contended resources and holder session, matching local results
- [ ] Tests cover remote contention output parity with local contention
