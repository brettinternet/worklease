---
id: TASK-20
title: Make Worklease skills portable across agents
status: Done
assignee:
  - '@codex-skill-portability'
created_date: '2026-07-14 22:12'
updated_date: '2026-07-14 22:22'
labels: []
dependencies: []
modified_files:
  - AGENTS.md
  - README.md
  - docs/source-provider-sdk-compatibility.md
  - skills/AGENTS.md
  - skills/worklease-workflow
  - skills/worklease-source-workflow
  - tests/test_skills.py
priority: medium
type: docs
ordinal: 21000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Publish agent-directed installation guidance and make the Worklease skill bundle self-contained for Agent Skills clients instead of relying on a sibling skill filesystem reference.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 skills/AGENTS.md tells agents how to install the complete Worklease skill bundle for a user-selected Agent Skills location without assuming one agent product
- [x] #2 Installed skill references stay within the installed skill root and preserve one normative workflow contract
- [x] #3 Skill metadata and links pass repository validation
- [x] #4 README points users and agents to the portable installation guidance
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Restructure the source/provider guidance under the worklease-workflow skill so every installed reference stays within one skill root, while retaining a compatibility pointer for existing repository links. 2. Add skills/AGENTS.md with product-neutral agent installation instructions and update README discovery guidance. 3. Add focused validation for standard frontmatter and root-contained links, run skill validation and repository quality gates, independently verify, then finalize and commit only TASK-20 files.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Consolidated the source workflow, provider mappings, and examples under one self-contained worklease-workflow skill; added product-neutral skills/AGENTS.md installation guidance, bundled MIT notice, README entry point, portable frontmatter, and focused root-contained link tests. Focused unittest: 4/4 passed. Official quick_validate.py: Skill is valid.

Final verification: official skill quick validator passed; focused portable-skill tests passed 4/4; repository mise run lint, format-check, test (132 tests), and typecheck passed; staged mise run hooks passed Ruff and 132 tests. Independent verifier PASS covered all four acceptance criteria and confirmed all 20 local Markdown links stay within the 17-file skill bundle. Its future-coupling test finding was fixed and independently rechecked PASS.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Published one self-contained, product-neutral Worklease Agent Skill with agent-directed installation guidance, source-provider mappings and examples inside the skill root, portable frontmatter, bundled license, README/SDK links, and regression tests. Verified with the official skill validator, all repository quality gates, staged hooks, and independent acceptance review.
<!-- SECTION:FINAL_SUMMARY:END -->
