---
id: TASK-157
title: Report symlinked ancestors accurately in owner-private path checks
status: done
rank: ab57213c2eb57209c
created: 2026-10-23T22:39:00Z
updated: 2026-10-23T23:12:36Z
---

On macOS, /tmp and /var are symlinks to /private/... . `worklease server init` (and enroll/invite artifact writes) under such a path fails with "output directory must be owner-private" even when the directory itself is 0700 and owned by the caller. The real cause is the symlinked ancestor, so operators chase permissions that are already correct. Found while building the bl remote-authority test (backlog BL-72), which had to EvalSymlinks every home.

## Acceptance criteria

- [x] When an owner-private check fails because an ancestor is a symlink, the error names the symlinked component and suggests the resolved path
  Evidence: go test -race -count=3 -run 'TestValidateHostedHomeDistinguishesSymlinkAndPermissions|TestWriteHostedExportRefusesReplacementAndSymlinkedParents' ./internal/store passed. Direct go run ./cmd/worklease server init --json with XDG_CONFIG_HOME beneath a symlink returned home-unsafe, named the alias component, suggested the resolved output directory, and created no configuration.
- [x] The permission-mode error is reported only when the mode or owner is actually wrong
  Evidence: TestValidateHostedHomeDistinguishesSymlinkAndPermissions passed: resolved 0700 directory accepted; symlink rejected as symlink, not permissions; chmod 0755 rejected with cause home permissions are not private. Hosted output callers preserve the actual validation failure.
- [x] Tests cover a 0700 directory beneath a symlinked ancestor
  Evidence: Focused regression tests passed three times under the race detector, including a 0700 private directory below a symlinked ancestor and a missing suffix. Full mise run test passed before and after merge.

## Notes

Plan: preserve existing symlink rejection, add component-specific diagnostics with resolved-path guidance in secureHome, propagate the actual validation error through hosted output callers, and verify private-directory and permission failures. Implement in .worktrees/fix-task-157; Worktrunk receipt hook failed on absent JSON stdin; user explicitly approved cleanup of this exact checkout and branch after merge.

Delivered source commit 325bac25ae4ca6561987a2c4dca30ce6bb836eef, merged into main as 8e4859c883164f13ff2a15acf0ed741eeaf6e5b7. Verification: mise run lint, format-check, typecheck, test all passed in worktree and integrated main; staged mise run hooks passed; focused race tests passed count=3; direct server init JSON execution confirmed actionable symlink guidance and no config write. One general diff review completed; no additional item-scoped defects. Existing primary changes preserved byte-for-byte through merge. User-approved Worktrunk cleanup removed fix/task-157 branch and .worktrees/fix-task-157; exact Herdr workspace wNQ verified absent afterward. Receipt hook failure remains personal setup, not a repo defect. All criteria and delivery obligations satisfied; no blocker or next implementation step remains.
