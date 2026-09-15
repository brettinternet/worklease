---
id: TASK-107.13
title: Publish a minimal Worklease server image to GHCR
status: Done
assignee:
  - '@brett'
created_date: '2026-09-14 01:38'
updated_date: '2026-09-15 01:05'
labels:
  - remote-authority
  - release
dependencies:
  - TASK-107.7
references:
  - .github/workflows/release.yml
documentation:
  - docs/remote-claim-authority.md
modified_files:
  - .dockerignore
  - Dockerfile
  - .github/workflows/release.yml
  - README.md
  - cmd/worklease-release/main_test.go
  - docs/container.md
  - scripts/test-container-image.sh
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
- [x] #1 The container contains the released Worklease binary and only the runtime material it needs, has no compiler or package manager, runs as a non-root user, and introduces no container-only application behavior.
- [x] #2 Release validation builds and smoke-tests Linux amd64 and arm64 images from the tagged commit; workflow dispatch validates without publishing, while an authorized tagged release publishes a multi-platform image to ghcr.io/brettinternet/worklease.
- [x] #3 Published tags include an immutable release-version tag whose in-container `worklease version` matches the release, and the documented tag policy does not allow one release to overwrite another release’s version tag.
- [x] #4 Container documentation includes a runnable `worklease serve` example and identifies one writable volume for the complete hosted home, including the SQLite database and its WAL/SHM files, hosted marker and lock, restore incarnation, installation/invite state, and every other authority file that must survive restart or upgrade.
- [x] #5 Configuration and TLS material are documented as read-only mounts or secrets rather than baked into the image; optional backup or replication state and credentials are documented wherever that integration requires persistence.
- [x] #6 A container restart and stop-before-start image upgrade test preserves the authority identity and durable claim/authentication state on the mounted volume. Documentation warns against multiple writable replicas and network filesystems unsupported by SQLite WAL.
- [x] #7 The release workflow grants package-write permission only to the publication job, does not publish from pull requests or validation-only runs, and does not publish an image when required release validation fails.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a pinned minimal non-root container definition that packages each released Linux binary without compilers, package managers, or container-specific application behavior.
2. Add an executable container smoke test that verifies image version/minimality and preserves authority identity plus durable authentication and claim state across a stop-before-start restart/upgrade on one mounted hosted-home volume.
3. Extend release automation to validate amd64 and arm64 images on dispatch/tag builds, then publish an immutable versioned multi-platform GHCR manifest only after required release validation, with package write limited to that job.
4. Document runnable initialization/serve usage, the complete writable hosted-home volume, read-only config/TLS/secrets, optional backup persistence, immutable tag policy, and SQLite single-writer/filesystem constraints.
5. Run focused container/workflow checks, all repository quality gates, independent review, and final acceptance verification before integrating.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented scratch-based non-root images assembled from the released Linux archive binaries. Added native amd64/arm64 validation and restart/stop-before-start replacement smoke coverage, an environment-gated GHCR publication job with isolated package-write permission and immutable-tag refusal, release workflow assertions, and container operations documentation.

Verification: local mise run lint, format-check, test, and typecheck passed after rebasing onto main; mise run hooks, actionlint, shellcheck, documentation examples, and git diff checks passed. Validation-only Release run 34915583460 passed at integrated commit 7e4cef4 for all four archives, both native container architectures (including version, scratch filesystem, non-root config, restart, replacement, durable auth/claim state), and all mise installs; release and GHCR publication jobs were skipped as required. Independent review found and fixed an invalid YAML prefix fixture; final review had no findings. Independent acceptance verification passed after explicit restart coverage was added.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Published minimal Worklease server container packaging and release automation. The scratch image contains only the released binary and runs non-root; tagged releases are gated and publish one immutable multi-platform GHCR version tag. Added container lifecycle smoke coverage and complete hosted-home/TLS/backup/operator documentation. Verified by local repository gates, independent review/acceptance checks, and successful validation-only Release run 34915583460 at integrated commit 7e4cef4.
<!-- SECTION:FINAL_SUMMARY:END -->
