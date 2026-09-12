# TASK-85.18 independent review

Date: 2026-09-12
Reviewer: independent `reviewer` subagent
Scope: filesystem safety, child-process cleanup, transaction/replay/pending/predecessor recovery, redaction, cancellation, Go gates/hooks/CI/release, Python retirement, and clean-checkout CLI/MCP coverage.

## Finding and resolution

### High: CI installed hooks without exercising them — resolved

The initial review found that `.github/workflows/ci.yml` ran `mise run hooks-install`, but neither CI nor `mise run ci` executed the installed pre-commit configuration. A broken `lefthook.yml` could therefore remain green.

Resolution:

- Added `hooks-all = "lefthook run pre-commit --all-files"` to `mise.toml`.
- Changed CI to run `mise run hooks-install` and `mise run hooks-all` before `mise run ci`.
- Made the test hook unset Git's transient `GIT_INDEX_FILE`; otherwise nested linked-worktree tests resolve `.git/index` relative to their temporary repositories during an actual commit.
- Executed `mise run hooks-all`: Lefthook ran both `gofmt` and `mise run test`; both passed (exit 0). The installed hook is also exercised during the final commit.

The follow-up static review confirmed that CI now installs and executes the hook. The reviewer had no shell capability for the follow-up, so the successful command output above is operator-provided resolution evidence.

## Review evidence

The reviewer inspected:

- `scripts/test-e2e.sh` and `cmd/worklease-smoke/main.go`
- CLI pending/predecessor reconciliation and redaction tests
- MCP recovery, cancellation, shutdown, and exact eleven-tool tests
- `mise.toml`, `lefthook.yml`, `.github/workflows/ci.yml`, and `.github/workflows/release.yml`
- tracked Python runtime, SDK, packaging, test, benchmark, and release surfaces

The initial review reported no other blocking defects in filesystem safety, process cleanup, recovery/replay boundaries, redaction, cancellation, release automation, or end-to-end coverage. The only reported finding was resolved and exercised.
