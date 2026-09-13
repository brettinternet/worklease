---
id: TASK-104
title: Automate changelog release promotion and validation
status: Done
assignee:
  - '@brett'
created_date: '2026-09-13 05:33'
updated_date: '2026-09-13 06:12'
labels: []
dependencies: []
references:
  - CHANGELOG.md
  - .github/workflows/release.yml
  - cmd/worklease-release
modified_files:
  - .github/workflows/release.yml
  - CHANGELOG.md
  - README.md
  - cmd/worklease-release/main.go
  - cmd/worklease-release/main_test.go
  - internal/release/changelog.go
  - internal/release/changelog_test.go
priority: medium
type: chore
ordinal: 129000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Go release cutover removed the previous changelog-aware release automation. As a result, v1.0.0 was tagged while its notes remained under Unreleased, and v1.1.0 used GitHub-generated commit notes rather than the curated changelog. Keep changelog prose curated by humans or agents, but automate the mechanical promotion and enforce alignment so a tag cannot silently publish with missing or misplaced notes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A supported release-preparation operation promotes a non-empty Unreleased section to a requested version and date while creating a fresh empty Unreleased section and preserving note categories, text, and order.
- [x] #2 Release preparation rejects duplicate versions, empty release notes, malformed versions or dates, and attempts that would overwrite an existing release section, with actionable errors.
- [x] #3 Tagged release automation fails clearly unless CHANGELOG.md contains exactly one matching version section at the tagged commit.
- [x] #4 The GitHub release body is populated from the exact matching CHANGELOG.md version section instead of GitHub-generated commit notes.
- [x] #5 Automated tests cover successful promotion, validation failures, and release-workflow integration, and release documentation explains the curated-entry and automated-promotion workflow.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add strict changelog parsing, promotion, and exact-version note extraction to the release package, preserving the Unreleased body verbatim and rejecting invalid or ambiguous state.
2. Expose changelog preparation and note extraction modes through cmd/worklease-release while preserving archive-build behavior.
3. Update the tagged release workflow to validate/extract the tagged version and publish that exact file as the GitHub release body.
4. Add unit and workflow integration tests, document the curated release process, then run focused and repository quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented strict changelog promotion and exact release-note extraction, exposed both through cmd/worklease-release, wired dispatch/tag validation and curated GitHub release bodies, added tests and release documentation. Initial independent review found seven edge cases; fixed source/output alias protection, fenced-code heading parsing, malformed duplicate detection, tab heading emptiness, explicit empty mode flags, workflow-dispatch validation, and concurrent main changelog preservation. Verification after fixes: focused release tests passed; mise run lint, format-check, test, and typecheck passed; release.yml parsed as YAML; promotion and extraction command smokes passed.

Independent reviewer re-review: PASS with no remaining findings. Rebased implementation onto current main and preserved TASK-105 Unreleased notes. Implementation commit: c320c1b.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added validated changelog promotion and exact-version note extraction to worklease-release, including duplicate/empty/malformed safeguards and source overwrite protection. Tagged and manual release validation now checks curated notes, and GitHub publication uses the exact changelog body. Verified with focused promotion/validation/workflow tests, command smokes, YAML parsing, mise lint, format-check, test, typecheck, staged hooks, and an independent review pass.
<!-- SECTION:FINAL_SUMMARY:END -->
