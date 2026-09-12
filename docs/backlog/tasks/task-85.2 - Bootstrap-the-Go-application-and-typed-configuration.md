---
id: TASK-85.2
title: Bootstrap the Go application and typed configuration
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies: []
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - ../hum/cmd/hum/main.go
  - ../hum/internal/config/config.go
  - ../hum/internal/cli/root.go
  - ../hum/mise.toml
  - ../hum/.taskfiles/cli.yaml
  - ../hum/.github/workflows/ci.yaml
  - src/worklease/cli.py
  - tests/test_cli.py
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 94000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Nothing in Go exists yet. Every later task needs the module, the entry point, the typed config, the error and exit-code registry, the JSON envelope, and the CI gate to exist and to be shaped the same way; otherwise each task invents its own. This task creates that foundation exactly as the Go Product Contract specifies, using the sibling hum repository as the pattern source. It adds no lease behavior.

Read first: contract sections 2 (D1, D2, D3, D10, D11, D15, D16), 3, 4 (global flags and the short-option table), 5, 6, 14, and 18 (reason, config, and cli sketches).

Patterns to mirror (read them before coding): `../hum/cmd/hum/main.go` (signal.NotifyContext, exitCode mapping, injected writers), `../hum/internal/config/config.go` (Input/Config/New with firstNonEmpty precedence and validation errors naming the key), `../hum/internal/cli/root.go` NewRootCommand (Writer/ErrWriter injection, no-op ExitErrHandler, OnUsageError, validateCLICommandTree, JSON error boundary), `../hum/mise.toml` and `../hum/.taskfiles/cli.yaml` (task shapes), `../hum/.github/workflows/ci.yaml` (matrix and Go cache steps).

Python evidence (behavior only, do not port structure): `src/worklease/cli.py` lines 60-80 and 455-480 (env names and help wording), `docs/cli-reference.md` sections "Exit codes" and "Short option namespace" (superseded by contract section 6 and 4), and these tests in `tests/test_cli.py`: test_short_flags_have_one_meaning_across_parser_tree, test_option_abbreviations_are_rejected, test_json_and_format_conflicts_are_order_independent, test_unrecognized_option_keeps_the_json_error_envelope, test_non_utf8_arguments_fail_as_invalid_arguments.

Deliver:

- `go.mod` (module `github.com/brettinternet/worklease`, go 1.27) and `go.sum`; `cmd/worklease/main.go` with `buildVersion`, `buildCommit`, `buildTime` ldflags variables, SIGINT/SIGTERM/SIGHUP context cancellation, and exit-code mapping through `reason.Error` and urfave `ExitCoder`.
- `internal/reason`: the `Error` type, the complete reason registry from contract 6.1 with exit codes, `CodeFor`, and a `Registered(reason)` helper other packages' tests use to assert every emitted reason is known.
- `internal/config`: `Load` implementing contract section 5 with per-value source tracking; YAML via `gopkg.in/yaml.v3` with `KnownFields(true)`.
- `internal/output`: JSON success and error envelope writers (schemaVersion 2, one document, stable key order via structs), text helpers (summary line, `key: value` lines, control-character escaping), and an `AssertNoSecret`-style redaction helper for tests.
- `internal/cli`: `NewRootCommand(version, commit, buildTime, stdout, stderr)` with global flags, the `version` command (text and JSON: version, commit, buildTime, schemaVersion, goVersion), `commands.go` registration list, a tree validator enforcing the contract short-option table, and a JSON error boundary so every failure in `--json` mode is exactly one error envelope on stdout.
- `mise.toml`: tools `go = "1.27.1"`, `staticcheck`, `"go:golang.org/x/vuln/cmd/govulncheck"`, and tasks `go-build`, `go-fmt`, `go-fmt-check`, `go-vet`, `go-test`, `go-race`, `go-vuln`, `ci-go` per contract 14. Leave every existing Python task untouched.
- `.github/workflows/ci.yml`: add a `go` job matrix (`ubuntu-latest`, `ubuntu-24.04-arm`, `macos-15-intel`, `macos-14`) running `mise run ci-go` alongside the existing Python jobs.
- `CHANGELOG.md` Unreleased: one line announcing the Go rewrite foundation.

Owned paths: `go.mod`, `go.sum`, `cmd/worklease`, `internal/config`, `internal/reason`, `internal/output`, `internal/cli` (skeleton), additions to `mise.toml` and `.github/workflows/ci.yml`, one `CHANGELOG.md` line. Out of scope: any lease, store, resource, or MCP behavior; removing Python tooling; documentation rewrites.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise run go-build` produces bin/worklease on Linux and macOS, and `bin/worklease version --json` emits exactly one JSON document with schemaVersion 2, operation "version", ok true, and the ldflags-injected version, commit, and buildTime; text mode prints one line.
- [ ] #2 internal/config tests prove precedence flag over env over YAML over default for every key in contract section 5, source tracking for each resolved value, bare-integer and Go duration parsing, blank values treated as unset, and failures with config-invalid for unknown YAML keys, wrong types, out-of-bounds values, and symlinked config files, plus config-missing for an explicit missing path while a missing default path is not an error.
- [ ] #3 internal/reason registers every reason string from contract section 6.1 with its exit code and a test compares the registry against a golden list; internal/output tests prove the success and error envelopes, that stdout carries exactly one JSON document in --json mode for a usage error, an unknown command, and an internal error, and that text-mode failures go to stderr as "error: <reason>: <message>".
- [ ] #4 internal/cli tests prove --help lists the command groups, unknown commands and abbreviated options fail invalid-argument (exit 64) in both modes, non-UTF-8 arguments fail invalid-argument, the short-option validator rejects a test command that reuses a short flag with a different long name, SIGTERM delivered to a running test command cancels its context and exits 130 with reason interrupted, and stdout and stderr are routed through the injected writers.
- [ ] #5 `mise run ci-go` passes locally and the new CI Go jobs pass on all four runners without modifying or removing any existing Python job or mise task.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
