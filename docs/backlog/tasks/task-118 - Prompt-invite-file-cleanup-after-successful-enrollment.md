---
id: TASK-118
title: Prompt invite-file cleanup after successful enrollment
status: To Do
assignee: []
created_date: '2026-09-16 23:56'
labels: []
dependencies: []
references:
  - internal/cli/profile_commands.go
  - docs/remote-claim-authority.md
priority: low
type: enhancement
ordinal: 160000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Successful enrollment currently reports only the profile and role. The invite is single-use at the authority, but the caller-owned artifact remains on disk and the immediate command output does not remind the operator to remove it. Automatically deleting an input file would be surprising and unsafe for canonical bootstrap artifacts, read-only mounts, or files managed by another secret-transfer system. Provide timely cleanup guidance without taking ownership of the file or expanding the command with automatic deletion behavior.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 After a successful text-mode enrollment from `--invite-file`, output states that the invite was consumed and advises removing the local artifact when it is no longer needed.
- [ ] #2 Enrollment never deletes or modifies the caller-provided invite file, whether enrollment succeeds, fails definitively, or has an uncertain outcome.
- [ ] #3 Failure and uncertain-outcome output does not claim that the invite was consumed, because exact replay may still require the artifact.
- [ ] #4 Enrollment from `--invite-fd` or the hidden prompt does not claim that a local invite file exists.
- [ ] #5 The cleanup guidance and tests do not expose the invite bearer or weaken existing output redaction.
<!-- AC:END -->
