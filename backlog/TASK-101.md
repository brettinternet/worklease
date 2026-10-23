---
id: TASK-101
title: Make lifecycle timeline text operationally informative
status: done
type: enhancement
priority: medium
assignees: ['@brett']
labels: [cli, ux]
rank: ace98b3a62ce98a84
refs: [internal/cli/text.go, internal/cli/watch_commands.go, internal/cli/text_test.go, internal/cli/watch_commands_test.go, docs/cli-reference.md]
created: 2026-09-13T04:08:00Z
updated: 2026-09-13T04:30:00Z
---

The compact lifecycle views are internally consistent but still omit context needed to understand what happened. Global events do not identify the affected resource, resource history reduces completed epochs to a generic status, watch uses an absolute timestamp in an otherwise compact view, and text cursor behavior conflicts with the documented JSON-only policy. Human output should remain concise while making each row understandable and continuation behavior explicit.

## Acceptance criteria

- [x] Default text for events and bare history identifies the affected resource or compact resource set for every event.
- [x] Compact resource history communicates how completed epochs ended and includes enough concise actor or work context and operation activity to understand each epoch without --full.
- [x] Human event, history, and watch output follows one documented cursor policy: it never dumps an unexplained opaque cursor, and any text continuation token is presented only as part of a copyable continuation instruction; JSON cursor fields remain unchanged.
- [x] Watch text uses concise relative expiry while JSON retains the exact timestamp.
- [x] Fixed-time tests cover singleton and multi-resource events, open and ended history epochs, cursor continuation or omission, timeout, and watch expiry; CLI help, CLI reference, and changelog describe the resulting behavior.

## Plan

1. Compact events and bare history rows add a compact resource set; singular/plural counts.
2. Compact resource history epochs show agent, end reason and relative end time for ended epochs, and an operation activity summary (count and kinds) without --full.
3. One cursor policy: events and history text never print cursors or blank coverage fields; watch text presents its resumption cursor only inside a copyable 'worklease watch --cursor ...' line; JSON unchanged.
4. Watch text shows relative expiry; JSON keeps exact timestamps.
5. Fixed-time tests for singleton/multi-resource events, open/ended epochs, cursor omission and continuation, timeout and expiry; update help text, cli-reference, CHANGELOG.

## Notes

Compact events rows add resources=<compact set> after kind and omit empty claimId/resources for authority-wide events (gc-applied). Compact history epochs add agentId, ended=<reason> <relative> for ended epochs, and operations=<ordered kinds, non-completed marked kind:state>; full keeps per-operation rows and adds prunedThroughSequence. Cursor policy: events/history text print no cursor and no blank coverage line (compact shows 'pruned: events through sequence N were collected' only when non-zero); watch text drops nextCursor and ends with 'resume: worklease watch --cursor CURSOR'; JSON unchanged. Watch text shows '(expires in 14m)' relative expiry. Counts pluralize (1 event / 2 events) and sub-second deltas read 'now'. Verified with TestCompactTimelineTextIsOperationallyInformative, TestStatusHistoryAndEventsFullTextExpandsMetadata, TestHistoryAndEventsColorOnlySemanticValues, TestHistoryWithoutResourceShowsRecentEventsAndTextOmitsCursor, TestWatchTextIncludesExpiryGuidance, TestWatchTextPresentsCursorOnlyAsResumeCommand, TestWatchTextTimeoutIncludesDurableCursor, TestWatchJSONRedactsEventDetails, smoke (events JSON nextCursor -> watch --cursor), and the full gates. Help Output: text for events, history, and watch, docs/cli-reference.md, and CHANGELOG describe the behavior.

## Summary

Compact events and history text now identify resources, actors, how epochs ended, and operation activity; text never prints a bare cursor and watch shows relative expiry with a copyable resume command. Verified by fixed-time unit tests, CLI-level tests, smoke, and the quality gates; docs and changelog updated.
