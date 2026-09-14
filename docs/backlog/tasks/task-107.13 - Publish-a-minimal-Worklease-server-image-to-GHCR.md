---
id: TASK-107.13
title: Publish a minimal Worklease server image to GHCR
status: To Do
assignee: []
created_date: '2026-09-14 01:38'
labels:
  - remote-authority
  - release
dependencies:
  - TASK-107.7
references:
  - .github/workflows/release.yml
documentation:
  - docs/remote-claim-authority.md
parent_task_id: TASK-107
priority: medium
type: feature
ordinal: 145000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Self-hosting the remote authority should not require users to assemble their own runtime around a release archive. Ship the same statically linked Worklease server in a small, hardened container while preserving the SQLite authority’s single-host, single-writer durability model. The image is release packaging, not a second runtime or deployment control plane.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The container contains the released Worklease binary and only the runtime material it needs, has no compiler or package manager, runs as a non-root user, and introduces no container-only application behavior.
- [ ] #2 Release validation builds and smoke-tests Linux amd64 and arm64 images from the tagged commit; workflow dispatch validates without publishing, while an authorized tagged release publishes a multi-platform image to ghcr.io/brettinternet/worklease.
- [ ] #3 Published tags include an immutable release-version tag whose in-container `worklease version` matches the release, and the documented tag policy does not allow one release to overwrite another release’s version tag.
- [ ] #4 Container documentation includes a runnable `worklease serve` example and identifies one writable volume for the complete hosted home, including the SQLite database and its WAL/SHM files, hosted marker and lock, restore incarnation, installation/invite state, and every other authority file that must survive restart or upgrade.
- [ ] #5 Configuration and TLS material are documented as read-only mounts or secrets rather than baked into the image; optional backup or replication state and credentials are documented wherever that integration requires persistence.
- [ ] #6 A container restart and stop-before-start image upgrade test preserves the authority identity and durable claim/authentication state on the mounted volume. Documentation warns against multiple writable replicas and network filesystems unsupported by SQLite WAL.
- [ ] #7 The release workflow grants package-write permission only to the publication job, does not publish from pull requests or validation-only runs, and does not publish an image when required release validation fails.
<!-- AC:END -->
