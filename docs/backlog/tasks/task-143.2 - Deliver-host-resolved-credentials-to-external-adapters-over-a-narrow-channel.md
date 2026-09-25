---
id: TASK-143.2
title: Deliver host-resolved credentials to external adapters over a narrow channel
status: To Do
assignee: []
created_date: '2026-09-25 16:38'
updated_date: '2026-09-25 16:38'
labels:
  - work-queue
  - external-adapter
milestone: m-1
dependencies:
  - TASK-142.2
documentation:
  - docs/external-adapter-protocol.md
  - docs/external-adapter-protocol.schema.json
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-143
priority: medium
type: feature
ordinal: 74000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
External adapters receive only an opaque `credentialRef`, and the host strips provider tokens from their environment, so each adapter must implement its own keychain or secret lookup. That duplicates security-sensitive code in every adapter and bypasses the host's principal verification.

The Linear work in TASK-134 adds a user-configured credential helper for built-in adapters, and DRAFT-14 plans OAuth. This task makes the same host-side credential resolution available to external adapters, so helpers and later OAuth serve built-in and external sources alike. It depends on the Linear credential-helper subtask; add that dependency once the subtask exists.

The user wants the CLI approached with API-driven design: every command added here is a stable, scriptable interface first, with `--json` output in the existing CLI envelope (`schemaVersion`, `ok`, `operation`, and `error.reason`/`exitCode`/`details`), stable diagnostic reasons, and documented exit codes, and the human output is a rendering of that same result.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 An external source can name a host-side credential helper in queue.yaml; the host runs it and delivers the resulting secret to the adapter over one documented protocol channel, never on argv, in the environment, in source config, or in any logged or journaled message.
- [ ] #2 The channel is a protocol v1-compatible optional feature negotiated through the manifest; adapters that do not declare it keep working unchanged with the opaque `credentialRef`.
- [ ] #3 The credential is bound to the approved source, origin, and principal; changing the helper, its scope, or the principal requires reapproval, and a mismatch is refused with a stable diagnostic.
- [ ] #4 Expiry and refresh are supported: the host can deliver a replacement credential without restarting the adapter, and refresh is serialized per credential.
- [ ] #5 The protocol spec and JSON schema define the channel and its failure diagnostics, and the conformance check verifies that the credential never appears in adapter stdout, stderr, diagnostics, or host logs.
- [ ] #6 The authoring guide shows an adapter consuming the credential.
<!-- AC:END -->
