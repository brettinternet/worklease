---
id: TASK-118
title: Prompt invite-file cleanup after successful enrollment
status: Done
assignee:
  - '@brett'
created_date: '2026-09-16 23:56'
updated_date: '2026-09-17 06:06'
labels: []
dependencies: []
references:
  - internal/cli/profile_commands.go
  - docs/remote-claim-authority.md
  - internal/cli/remote_commands_test.go
modified_files:
  - internal/cli/profile_commands.go
  - internal/cli/remote_commands_test.go
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
- [x] #1 After a successful text-mode enrollment from `--invite-file`, output states that the invite was consumed and advises removing the local artifact when it is no longer needed.
- [x] #2 Enrollment never deletes or modifies the caller-provided invite file, whether enrollment succeeds, fails definitively, or has an uncertain outcome.
- [x] #3 Failure and uncertain-outcome output does not claim that the invite was consumed, because exact replay may still require the artifact.
- [x] #4 Enrollment from `--invite-fd` or the hidden prompt does not claim that a local invite file exists.
- [x] #5 The cleanup guidance and tests do not expose the invite bearer or weaken existing output redaction.
- [x] #6 JSON success remains machine-readable with its existing envelope and no appended prose; cleanup guidance appears only in successful text output for an explicitly supplied --invite-file.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add successful text-mode cleanup guidance only when --invite-file was explicitly supplied.
2. Extend enrollment tests to prove success guidance, file preservation across success/failure/uncertain outcomes, and no guidance for FD/prompt or JSON paths.
3. Update the remote enrollment documentation example and run focused plus repository quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Validation: enrollCommand in internal/cli/profile_commands.go has separate JSON success and text success branches; inviteInputFromCommand distinguishes file, descriptor and prompt. Current text prints only profile and role. Keep guidance in the CLI text-success branch, based on the supplied --invite-file rather than merely the presence of a decoded artifact. Existing file/FD integration coverage is in internal/cli/remote_commands_test.go. No authority protocol, artifact format, or enrollment persistence change is needed.

Implemented conditional text-only cleanup guidance, preserved JSON/FD/prompt behavior, added success/definitive-failure/uncertain-outcome file-preservation tests, and updated the remote enrollment guide. Focused CLI tests pass.

Validation passed on merged main: mise run lint, mise run format-check, mise run test, and mise run typecheck. Independent verifier passed all six acceptance criteria, including repeated focused enrollment tests and cached-diff checks. One unrelated MCP timing test failed once before passing alone and in the full rerun.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added successful text-mode invite-file cleanup guidance without taking ownership of caller artifacts. Covered file preservation and output behavior for success, definitive failure, uncertain outcome, FD, prompt, and JSON enrollment; updated remote enrollment documentation. Verified with repository quality gates and independent acceptance review.
<!-- SECTION:FINAL_SUMMARY:END -->
