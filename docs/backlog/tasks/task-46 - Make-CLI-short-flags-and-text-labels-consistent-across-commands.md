---
id: TASK-46
title: Make CLI short flags and text labels consistent across commands
status: To Do
assignee: []
created_date: '2026-09-07 03:27'
labels:
  - cli
  - devex
dependencies: []
references:
  - src/worklease/cli.py
priority: medium
type: enhancement
ordinal: 47000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Short flags mean different things per subcommand: `-r`/`-c`/`-a` are --resource/--claim-id/--agent-id everywhere except `gc` (--retention-days/--cutoff/--apply) and the top level (`-a` = --help-all); `-F` is --token-file except on `list` (--full); `-C` is --coordination-only on acquire but --successor-claim-id on transfer and --content-file on replace-file; `-W`/`-O` differ between acquire, transfer, and reconcile. Scripts that reuse a flag across commands silently change meaning. Also: `status --verbose` prints raw camelCase claim keys while every other renderer uses `_text_label`, and `--json --help`/`--help-all` print text.

Decide on one flag namespace (likely: reserve -r/-c/-a/-s/-o/-w/-t/-F/-D/-R/-T for the shared claim vocabulary and give command-specific options distinct letters or long-only forms), treat removals as a breaking CLI change with a documented migration, and normalize verbose labels.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every short flag has one meaning across all subcommands, documented in a table in README
- [ ] #2 status --verbose labels use the same upper-snake convention as other renderers; tests updated
- [ ] #3 Removed or changed short flags are listed in release notes; tests assert no letter is bound to two meanings
<!-- AC:END -->
