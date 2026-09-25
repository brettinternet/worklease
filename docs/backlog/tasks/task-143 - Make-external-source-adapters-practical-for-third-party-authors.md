---
id: TASK-143
title: Make external source adapters practical for third-party authors
status: To Do
assignee: []
created_date: '2026-09-25 16:38'
updated_date: '2026-09-25 16:38'
labels:
  - work-queue
  - external-adapter
milestone: m-1
dependencies:
  - TASK-143.1
  - TASK-143.2
  - TASK-143.3
  - TASK-143.4
  - TASK-143.5
documentation:
  - docs/external-adapter-protocol.md
  - docs/work-queue-tui-proposal.md
  - skills/worklease-workflow/references/external-adapter-authoring.md
priority: medium
type: feature
ordinal: 72000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
S7 (TASK-133) shipped the external adapter protocol: a supervised JSON-RPC 2.0 stdio process with a manifest, explicit executable approval, the static `generic` claim policy, and a host conformance suite. It is the supported way for users to add their own backends; hooks, executable policy plugins, and an event bus stay rejected (D16, D17) because they would split claim exclusion domains and make write recovery unverifiable.

In practice an outside author still cannot build one comfortably: the conformance suite runs only as a Go test in this repository against built-in fixtures, the host passes only an opaque `credentialRef` so each adapter must reinvent secret handling, the only example is read-only, v1 stability is not stated for external authors, and `queue init` cannot configure an external source. The user asked on 2026-09-25 to close these gaps so users can try supporting their own backends.

The user wants the CLI approached with API-driven design: every command added here is a stable, scriptable interface first, with `--json` output in the existing CLI envelope (`schemaVersion`, `ok`, `operation`, and `error.reason`/`exitCode`/`details`), stable diagnostic reasons, and documented exit codes, and the human output is a rendering of that same result.

SDKs and a generic MCP-bridge adapter are out of scope: the protocol is plain JSON-RPC, and MCP tools carry no pagination, capability, or receipt semantics, so each provider still needs its own mapping. Revisit SDKs only on demonstrated demand.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every subtask is Done with its acceptance criteria verified.
- [ ] #2 An author outside this repository can follow the authoring guide from an empty directory to an approved, conformance-checked, credentialed, write-capable adapter using only the shipped binary and documentation; this is exercised end to end at least once and recorded in task notes.
- [ ] #3 D16/D17 and plan §11 in docs/work-queue-tui-proposal.md reflect the shipped author tooling, and hooks, policy plugins, and SDKs remain explicitly out of scope.
<!-- AC:END -->
