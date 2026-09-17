---
id: TASK-124
title: Make local a first-class profile selection
status: To Do
assignee: []
created_date: '2026-09-17 23:00'
labels:
  - ergonomics
  - remote-authority
dependencies: []
references:
  - docs/remote-claim-authority.md
  - docs/cli-reference.md
  - internal/config/profile.go
  - internal/cli/profile_commands.go
priority: medium
type: enhancement
ordinal: 166000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Remote enrollment commonly makes a remote profile the user default, but returning to local authority while retaining that profile requires manually editing profiles.yaml or passing `--local` forever. Users already understand authority choice through profile names, so local should be a reserved built-in selection rather than a fake persisted remote profile. This must preserve the existing selection precedence and keep enrolled remote profiles and credentials available for later explicit use.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease profile list` exposes `local` as a reserved built-in selection, clearly distinguished from persisted remote profiles; adding or enrolling a remote profile under that name is rejected safely.
- [ ] #2 `worklease profile default local` makes local the user-wide default without deleting persisted remote profiles or credentials, and `worklease --profile NAME` can still select a retained remote profile.
- [ ] #3 `worklease profile bind local [--cwd DIR]` records an explicit checkout-local selection that overrides a user remote default; unbinding restores normal fallback behavior.
- [ ] #4 Commands that inspect or explicitly select profiles, including text and JSON output, consistently accept/report the built-in local selection and its selection source; help, user documentation, migration compatibility, and focused precedence tests cover local default, local binding, retained remote selection, and `--local` behavior.
<!-- AC:END -->
