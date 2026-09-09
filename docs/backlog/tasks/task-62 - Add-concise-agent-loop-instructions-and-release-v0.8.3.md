---
id: TASK-62
title: Add concise agent loop instructions and release v0.8.3
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-09 17:27'
updated_date: '2026-09-09 17:40'
labels: []
dependencies: []
ordinal: 66000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make Worklease useful without loading the full workflow skill by exposing concise, version-matched agent instructions from the CLI and documenting minimal project and Ralph-loop discovery in the README. Publish the change as patch release v0.8.3.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 worklease instructions loop prints a concise multi-agent operating contract covering canonical resources, acquisition, conflicts, heartbeats, revalidation, durable checkpoints, release, stale claims, and secret handling
- [ ] #2 worklease instructions safety concisely states the authority, fencing, credential, and recovery boundaries
- [ ] #3 Top-level CLI help and README make the lightweight instructions discoverable and reserve the full skill for advanced workflows
- [ ] #4 Automated tests cover instruction output and existing CLI behavior remains valid
- [ ] #5 Version 0.8.3 is published with passing local and remote release checks
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a lightweight instructions command with concise loop and safety topics, integrate it into grouped help, and cover text/JSON behavior. 2. Update README agent discovery with minimal AGENTS.md and Ralph-loop snippets, while directing advanced workflows to the full skill. 3. Bump root and SDK metadata to 0.8.3, update release notes/workflow defaults, and run focused plus full quality gates. 4. Commit and push main, verify CI, tag v0.8.3, verify tagged workflows and published release assets, then finalize TASK-62.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented concise text and schema-versioned JSON output for instructions loop/safety, added top-level help and README discovery, updated CLI reference and schemas, and prepared version 0.8.3 metadata/changelog. Local lint, format-check, 277 core tests, 19 SDK tests, typechecks, root/SDK builds, wheel smoke test, and pre-commit hooks pass. Independent verifier passed acceptance criteria 1-4 with no release blocker.
<!-- SECTION:NOTES:END -->
