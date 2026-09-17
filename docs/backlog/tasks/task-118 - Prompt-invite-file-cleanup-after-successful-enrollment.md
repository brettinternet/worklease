---
id: TASK-118
title: Prompt invite-file cleanup after successful enrollment
status: To Do
assignee: []
created_date: '2026-09-16 23:56'
updated_date: '2026-09-17 00:09'
labels: []
dependencies: []
references:
  - internal/cli/profile_commands.go
  - docs/remote-claim-authority.md
  - internal/cli/remote_commands_test.go
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
- [ ] #6 JSON success remains machine-readable with its existing envelope and no appended prose; cleanup guidance appears only in successful text output for an explicitly supplied --invite-file.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend successful text enrollment from --invite-file with consumed-invite and conditional local cleanup guidance, without printing the bearer or file contents.
2. Preserve JSON success shape and existing file/FD/prompt input behavior; never take ownership of caller files.
3. Test file contents and existence after success, definitive failure and uncertain outcome; assert no consumption guidance for failures and no file guidance for FD/prompt input. Update the enrollment example.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Validation: enrollCommand in internal/cli/profile_commands.go has separate JSON success and text success branches; inviteInputFromCommand distinguishes file, descriptor and prompt. Current text prints only profile and role. Keep guidance in the CLI text-success branch, based on the supplied --invite-file rather than merely the presence of a decoded artifact. Existing file/FD integration coverage is in internal/cli/remote_commands_test.go. No authority protocol, artifact format, or enrollment persistence change is needed.
<!-- SECTION:NOTES:END -->
