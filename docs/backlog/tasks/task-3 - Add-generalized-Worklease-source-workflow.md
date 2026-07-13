---
id: TASK-3
title: Add generalized Worklease source workflow
status: To Do
assignee: []
created_date: '2026-07-13 22:29'
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
