---
id: TASK-145
title: Add an authority-wide Claims tab to the queue TUI
status: To Do
assignee: []
created_date: '2026-09-26 06:03'
labels: []
dependencies: []
references:
  - internal/queueui/model.go
  - internal/queueui/view.go
  - internal/cli/queue_command.go
  - internal/watch/watch.go
priority: medium
type: feature
ordinal: 79000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The queue TUI shows claims only through the claim overlay (`queue.RunClaimOverlay`), which joins claims onto queue items. Claims whose resources do not map to a loaded item (plain `worklease acquire` on files or branches, claims from other sources or views) are invisible, even to queue users. `worklease queue` also fails outright when `queue.yaml` is absent or has no views, so users with no task provider, or who only care about claims, have no live view at all. `worklease list` and `status` are one-shot, and `internal/watch` has the live ledger cursor but no UI.

The queue TUI already has a precedent for a non-item view: the built-in Recovery tab (`RecoveryViewID`) lives in the same tab bar, switches with `v`/`V`/digits and mouse clicks, and reuses the header, palette (including high contrast), footer bindings, and help overlay. A Claims tab should follow that precedent so moving between claims and queue lists is one keystroke and the two feel like one tool, rather than shipping a separate TUI with its own look.

Naming: the existing Claimed view lists queue items that carry a claim; the new tab lists authority claims regardless of items. Keep the distinction clear in labels and help text.

Keep claims state and rendering in their own files inside `internal/queueui` rather than growing `model.go` (about 1,900 lines) further; share the list, detail, tab bar, and palette primitives instead of copying them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The queue TUI tab bar includes a built-in Claims tab that lists current claims from the selected authority, including claims whose resources match no queue item
- [ ] #2 Switching between the Claims tab and queue views uses the existing `v`/`V`, digit, and tab-click bindings, and each view keeps its selection when the user returns to it
- [ ] #3 The Claims tab reuses the queue TUI header, tab bar, palette (normal and high contrast), list and detail layout, footer bindings, and help overlay; help text distinguishes Claims from the Claimed view
- [ ] #4 Claim detail shows resources, holder and session, expiry countdown, checkpoints or progress, and recent lifecycle history
- [ ] #5 The claims list updates live from the ledger cursor without restarting, and marks stale or expiring claims
- [ ] #6 The list can be filtered to mine vs all, by resource prefix, and to expiring or stale claims
- [ ] #7 When claim resources match a loaded queue item, the row shows the item title and a key jumps to that item in a queue view; otherwise raw resource keys are shown
- [ ] #8 A dedicated entry point opens the TUI directly on the Claims tab, and with no `queue.yaml` the TUI runs claims-only with a visible notice instead of failing; malformed queue config still fails
- [ ] #9 Against a remote authority, the tab shows no more about other sessions than `worklease status` and `worklease list` already expose to the caller
- [ ] #10 The first version is read-only; renew and release actions are out of scope
- [ ] #11 Tests cover tab switching, claims-only startup, live updates, item correlation, and remote visibility, each at the lowest layer that reproduces it
<!-- AC:END -->
