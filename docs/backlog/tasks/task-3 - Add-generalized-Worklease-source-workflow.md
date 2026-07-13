---
id: TASK-3
title: Add generalized Worklease source workflow
status: In Progress
assignee:
  - '@codex-main'
created_date: '2026-07-13 22:29'
updated_date: '2026-07-13 22:37'
labels:
  - architecture
  - documentation
  - skills
dependencies: []
modified_files:
  - skills/worklease-source-workflow/
  - README.md
priority: medium
type: docs
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Publish a reusable source/provider composition skill so users can connect Backlog.md, loose Markdown, GitHub Issues, Linear, Jira, and unknown work sources to the provider-neutral Worklease workflow without copying repository-specific automation. Keep provider SDKs, network operations, and provider-side mutations outside the Worklease core.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A worklease-source-workflow skill loads the worklease-workflow contract first and leaves dependency scheduling, ownership epochs, heartbeat/release sequencing, checkpoint ordering, and generic result semantics in that normative contract.
- [ ] #2 The skill supplies a provider capability contract and authoring checklist for explicit source resolution, Source/WorkRef/WorkItem mapping, Worklease resource policy selection, authoritative reads and writes, durable receipts, review/archive boundaries, capability failures, and provider fencing evidence.
- [ ] #3 Provider references cover Backlog.md, loose Markdown, GitHub Issues, Linear, Jira, and unknown providers without importing personal dotfiles commands, environment variables, or provider SDK/network behavior into the core.
- [ ] #4 Examples show local Markdown replacement, remote-provider local coordination, and a provider conditional-write path while preserving providerMutationFenced false unless concrete provider fencing evidence exists.
- [ ] #5 README links the source workflow skill and gives succinct instructions for implementing a custom source adapter.
- [ ] #6 Focused checks verify skill metadata, links, provider coverage, and the boundary between generic scheduling, bundled key adapters, and provider workflow adapters.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a progressive-loading worklease-source-workflow skill that inherits the normative worklease-workflow contract and defines the source/provider composition boundary.
2. Add the provider capability contract, authoring checklist, provider references, and three guarantee-focused examples without provider SDK or personal-dotfiles assumptions.
3. Link the skill from README with a concise custom-adapter procedure, run focused link/coverage/boundary checks plus repository quality gates, independently verify acceptance, and finalize the task.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented the generalized worklease-source-workflow skill, provider contract/checklist, six provider references, and three guarantee-focused examples. README now links both skills and gives a six-step custom-source procedure. Focused documentation smoke passed for skill metadata, 14 Markdown documents, all relative links, provider coverage, guarantee vocabulary, and exclusion of legacy backlog-claim assumptions.
<!-- SECTION:NOTES:END -->
