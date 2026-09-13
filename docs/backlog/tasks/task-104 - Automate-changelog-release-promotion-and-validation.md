---
id: TASK-104
title: Automate changelog release promotion and validation
status: To Do
assignee: []
created_date: '2026-09-13 05:33'
labels: []
dependencies: []
references:
  - CHANGELOG.md
  - .github/workflows/release.yml
  - cmd/worklease-release
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
- [ ] #1 A supported release-preparation operation promotes a non-empty Unreleased section to a requested version and date while creating a fresh empty Unreleased section and preserving note categories, text, and order.
- [ ] #2 Release preparation rejects duplicate versions, empty release notes, malformed versions or dates, and attempts that would overwrite an existing release section, with actionable errors.
- [ ] #3 Tagged release automation fails clearly unless CHANGELOG.md contains exactly one matching version section at the tagged commit.
- [ ] #4 The GitHub release body is populated from the exact matching CHANGELOG.md version section instead of GitHub-generated commit notes.
- [ ] #5 Automated tests cover successful promotion, validation failures, and release-workflow integration, and release documentation explains the curated-entry and automated-promotion workflow.
<!-- AC:END -->
