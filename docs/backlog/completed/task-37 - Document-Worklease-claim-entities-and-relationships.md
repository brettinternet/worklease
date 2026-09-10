---
id: TASK-37
title: Document Worklease claim entities and relationships
status: Done
assignee:
  - '@codex'
created_date: '2026-07-16 14:42'
updated_date: '2026-07-16 14:54'
labels: []
dependencies: []
documentation:
  - docs/claim-model.md
modified_files:
  - README.md
  - docs/claim-model.md
priority: medium
type: docs
ordinal: 38000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add an approachable conceptual map of Worklease provider identity, resource derivation, ownership epochs, mutation credentials, lifecycle state, and required CLI values. Keep the README compact and link to a deeper guide.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 README links readers to the claim entity deep dive
- [x] #2 The deep-dive guide diagrams provider/source/item, resource, claim identity, credentials, lifecycle, and provider state boundaries
- [x] #3 The guide distinguishes required values by operation and separates claim revision from provider version
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a compact claim-model entry point to README.md. 2. Add docs/claim-model.md with relationship and lifecycle diagrams, field semantics, and operation requirements. 3. Verify links, rendered Markdown structure, and project quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Added a compact README entry point and docs/claim-model.md with entity, lifecycle, expiry, state, and provider-boundary diagrams. Independent verifier passed all acceptance criteria after checking links, Mermaid fence balance, CLI operation coverage, replace-file requirements, and claim-revision/provider-version semantics. Validation: git diff --check, mise run lint, mise run format-check, mise run test (191 core + 19 SDK tests), and mise run typecheck all passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Documented how provider/source/item derive a resource, how identities and credentials form a bounded claim, and which values each singleton and bundle operation requires. README links to the guide. Verified all links and semantics independently; all repository quality gates pass.
<!-- SECTION:FINAL_SUMMARY:END -->
