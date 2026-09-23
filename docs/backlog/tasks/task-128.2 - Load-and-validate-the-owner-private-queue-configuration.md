---
id: TASK-128.2
title: Load and validate the owner-private queue configuration
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 16:27'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-126
references:
  - internal/config/profile.go
  - internal/config/config.go
  - docs/remote-claim-authority.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-128
priority: high
type: feature
ordinal: 12000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
D10 puts sources, views, and the "me" identity mapping in one owner-private user file, `$XDG_CONFIG_HOME/worklease/queue.yaml`. It follows the trust model of profiles.yaml and bindings.yaml (internal/config/profile.go): repositories never supply queue configuration. Plan section 12 shows the intended shape. The `launch:` key is out of scope here; TASK-131.1 adds it to the schema.

This task owns the schema, loading, validation, and user documentation for the file. The adapters (TASK-128.4, TASK-128.5) and the claim overlay (TASK-128.7) consume the parsed configuration.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The queue loads `$XDG_CONFIG_HOME/worklease/queue.yaml`, falling back to ~/.config. It rejects files that are not owner-owned or are group- or world-readable, and applies the same symlink rules as profiles.yaml
- [x] #2 Schema version 1 is parsed strictly with these keys: `version`; `me` (provider accounts per host, and Backlog.md assignee strings); `sources`, where each has a unique `id` and an `adapter` of backlog-md or github; backlog-md sources require an existing `checkout` directory (with ~ expansion) and accept optional `claims` {policy: generic, source} and `allowGitNetwork` (bool, default false); github sources require `host`, `repository` in owner/repo form, and `account`; and `views`, each with a unique `name`, an `authority` that is a profile name or `local`, `sources` that name defined ids, and a `filter` from a closed key set
- [x] #3 Unknown keys, unknown adapters, duplicate ids or names, dangling source references, and unknown authority profiles fail with errors that name the offending path
- [x] #4 Configuration is never read from the current repository or working directory, or from environment variables other than XDG_CONFIG_HOME and HOME
- [x] #5 A missing file produces a structured `no-sources-configured` diagnostic with setup guidance
- [x] #6 A new docs/queue.md documents the file with a complete example and is linked from README.md
- [x] #7 Tests cover every rejection path, tilde expansion, permissions, and a valid multi-source file
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reuse private profile file loading and strict YAML parsing for queue v1, validating source/view references and trusted authority names. 2. Document owner-private setup and add rejection/valid-file tests. 3. Run focused and repository gates, review, commit and integrate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented private queue.yaml v1 loader, strict source/view validation, trusted profile lookup, and missing-file diagnostic. Focused config tests cover valid multi-source, profile, permissions, symlink, tilde and schema rejections. Reviewed path/security and missing-field handling; corrected absolute checkout validation and added fallback/permission tests. lint, format-check, test, typecheck, and hooks passed. No D1-D27 or plan section contradicted or refined. Code commit 6b06095f43b018e235fd20b25453581c4454017d merged into main at 439dc8f116ce7de8f8c68e40e40490b8b62060f4.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Private queue configuration and docs integrated on main (439dc8f); go test ./internal/config and all repository gates pass.
<!-- SECTION:FINAL_SUMMARY:END -->
