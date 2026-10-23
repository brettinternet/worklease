---
id: TASK-20
title: Make Worklease skills portable across agents
status: done
type: docs
priority: medium
assignees: ['@codex-skill-portability']
rank: a24d3fc68324d3fa6
files: [AGENTS.md, README.md, docs/source-provider-sdk-compatibility.md, skills/AGENTS.md, skills/worklease-workflow, skills/worklease-source-workflow, tests/test_skills.py]
created: 2026-07-14T22:12:00Z
updated: 2026-07-14T22:22:00Z
---

Publish agent-directed installation guidance and make the Worklease skill bundle self-contained for Agent Skills clients instead of relying on a sibling skill filesystem reference.

## Acceptance criteria

- [x] skills/AGENTS.md tells agents how to install the complete Worklease skill bundle for a user-selected Agent Skills location without assuming one agent product
- [x] Installed skill references stay within the installed skill root and preserve one normative workflow contract
- [x] Skill metadata and links pass repository validation
- [x] README points users and agents to the portable installation guidance

## Plan

1. Restructure the source/provider guidance under the worklease-workflow skill so every installed reference stays within one skill root, while retaining a compatibility pointer for existing repository links. 2. Add skills/AGENTS.md with product-neutral agent installation instructions and update README discovery guidance. 3. Add focused validation for standard frontmatter and root-contained links, run skill validation and repository quality gates, independently verify, then finalize and commit only TASK-20 files.

## Notes

Consolidated the source workflow, provider mappings, and examples under one self-contained worklease-workflow skill; added product-neutral skills/AGENTS.md installation guidance, bundled MIT notice, README entry point, portable frontmatter, and focused root-contained link tests. Focused unittest: 4/4 passed. Official quick_validate.py: Skill is valid.

Final verification: official skill quick validator passed; focused portable-skill tests passed 4/4; repository mise run lint, format-check, test (132 tests), and typecheck passed; staged mise run hooks passed Ruff and 132 tests. Independent verifier PASS covered all four acceptance criteria and confirmed all 20 local Markdown links stay within the 17-file skill bundle. Its future-coupling test finding was fixed and independently rechecked PASS.

## Summary

Published one self-contained, product-neutral Worklease Agent Skill with agent-directed installation guidance, source-provider mappings and examples inside the skill root, portable frontmatter, bundled license, README/SDK links, and regression tests. Verified with the official skill validator, all repository quality gates, staged hooks, and independent acceptance review.
