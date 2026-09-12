---
id: doc-2
title: Go Product Contract
type: specification
created_date: '2026-09-12 03:51'
updated_date: '2026-09-12 03:51'
tags:
  - go-rewrite
  - contract
  - specification
---
# Worklease Go Product Contract

This document is the normative design for TASK-85 (Rewrite Worklease in Go) and every TASK-85.x subtask. It exists so that implementing agents working unattended do not have to make product, architecture, or interface decisions. Read it completely before starting any go-rewrite task, then re-read the sections named in your task.

Fixed decisions in section 2 change only through the amendment procedure in section 15. If your task cannot be completed under this contract, follow section 15; do not improvise a different design.

## 1. Purpose and scope

- Worklease is a same-host work-lease coordinator for humans and coding agents. It grants one worker a bounded ownership epoch over one or more opaque resources, guards local operations behind that ownership, and keeps a redacted local history. The external work provider (Backlog.md, GitHub, Linear, Markdown files) remains authoritative for item state and progress.
- The Python package under `src/worklease` is a proof of concept. It is behavior evidence, not a compatibility target. Its public API, SDK, entry-point plugins, SQLite database, lease-file format, JSON schema version 1, and exact text output are all retired (section 16).
- The Go implementation is POSIX-only (Linux and macOS). Same-host coordination is the only guarantee; nothing here is cross-host exclusion or provider-side fencing.
- Every capability below is either retained as specified or listed as removed. When the Python code does something this document does not mention, treat it as removed unless the capability inventory (TASK-85.1) says otherwise.

## 2. Fixed decisions

| ID | Decision | Rationale |
| --- | --- | --- |
| D1 | Go module `github.com/brettinternet/worklease`; Go `1.27.1` pinned in `mise.toml`; binary `worklease` built from `cmd/worklease`; man-page generator `cmd/worklease-man`. | Mirrors the hum repository layout the owner already maintains. |
| D2 | Allowed runtime dependencies: `github.com/urfave/cli/v3` (v3.11.0 or later), `gopkg.in/yaml.v3`, `golang.org/x/sys`, and the SQLite driver selected by TASK-85.4 (preferred: `modernc.org/sqlite`). Tests use the standard `testing` package plus `internal/testkit`; no assertion libraries. Any other dependency requires an amendment. | Dependency-light like hum; keeps releases reproducible and audits small. |
| D3 | Targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, built with `CGO_ENABLED=0` unless the TASK-85.4 amendment records that CGO is required. No Windows support. | Cross-compilation from one runner and static binaries. |
| D4 | One claim model. A claim owns an ordered set of 1 to 32 unique resources. A "singleton" is a claim with one resource. There is no separate bundle command family, table set, or code path. | Removes the duplicated singleton/bundle implementations that drifted in Python. |
| D5 | Identity fields: `agentId` (config default, section 5), `sessionId` (generated per acquire unless supplied), `workKey` (defaults to the resources joined with `,`), `claimId` (generated). The Python `ownerId` is removed; the claim ID already identifies one worker attempt. | Fewer identifiers with distinct meanings. |
| D6 | Token: 32 bytes from `crypto/rand`, presented as 64 lowercase hex characters. The store keeps only `sha256(token)` hex and compares with `crypto/subtle.ConstantTimeCompare`. Tokens are never accepted on argv; sources are a handle file, `--token-file`, or `--token-fd`. Generated identifiers (claim, session, operation, lease reference) are 32 lowercase hex characters from `crypto/rand`. | Secret never rests in the database; no argv leakage. |
| D7 | The contextual handle (section 9) is the default claim selection for every lifecycle command. `--handle PATH` selects an explicit handle; `--no-handle` on acquire is the stateless mode that prints the token exactly once in JSON output. | Safe path by default (TASK-67 intent) without Python precedence rules. |
| D8 | Persistence is SQLite at `<home>/worklease.db` with `journal_mode=WAL`, `synchronous=FULL`, `busy_timeout=10000`, `foreign_keys=ON`, and `BEGIN IMMEDIATE` for every write transaction. Fresh schema version 1 via `PRAGMA user_version`. No Python database import or detection. SQLite is the sole serialization mechanism: there are no per-resource lock files. | One durable, transactional authority; fewer moving parts than flock plus SQLite. |
| D9 | Every durable lifecycle transition appends at least one row to `events` inside the same transaction. `events.seq` (AUTOINCREMENT) is the only cursor for `events`, `history`, and `watch`. | One ordering key replaces the Python composite keyset cursor. |
| D10 | Machine output is `--json` with envelope `schemaVersion: 2` and an `error` object; exit-code families in section 6. Text output is for humans and is deterministic but not a compatibility surface. | Intentionally incompatible, simpler contract. |
| D11 | Configuration precedence: command flags, then `WORKLEASE_*` environment variables, then the YAML config file, then defaults (section 5). | Same shape as hum's typed config with an optional file. |
| D12 | The first Go release is tagged `v1.0.0`. Release archives are `worklease-<version>-<os>-<arch>.tar.gz` (`os` in `linux`,`macos`; `arch` in `x64`,`arm64`) containing `bin/worklease` and `share/man/man1/worklease.1`, plus `checksums.txt` (SHA-256). The archive layout matches the current mise `github:brettinternet/worklease` installation. | Existing mise installs keep working; the version signals the incompatible rewrite. |
| D13 | MCP is served by `worklease mcp` over stdio JSON-RPC using the hum server pattern; protocol version `2025-06-18` with negotiation for `2024-11-05` and `2025-03-26`. Tools in section 12. | Proven dependency-light transport. |
| D14 | Stored timestamps and expiry use the wall clock (`time.Now().UnixMicro()` stored as INTEGER microseconds). Waits, deadlines, and heartbeat scheduling use monotonic time. Clock-regression rule in section 7.6. | Conservative expiry with deterministic tests. |
| D15 | Testing: every package passes `go test` and `go test -race`; command behavior is tested black-box through `cli.NewRootCommand` in-process and, for release smoke, through the built binary. Tests never import Python or read Python state. | Unattended loops need executable, hermetic evidence. |
| D16 | Quality gate for every subtask until TASK-85.18 is `mise run ci-go` (section 14). TASK-85.18 repoints `lint`, `format-check`, `test`, and `typecheck` at the Go implementation and removes the Python tasks. | The project CLAUDE.md gates stay valid through the transition. |

## 3. Repository layout and ownership

Each path has exactly one owning task. A task may create files only under the paths it owns plus the shared files listed below. Later tasks may edit earlier tasks' packages only where their acceptance criteria say so.

| Path | Owner | Contents |
| --- | --- | --- |
| `go.mod`, `go.sum` | 85.2 (85.4 adds the driver) | Module definition. |
| `cmd/worklease/main.go` | 85.2 | Signal-aware entry point, exit-code mapping. |
| `cmd/worklease-man/main.go` | 85.17 | Man page generator (hum `cmd/hum-man`). |
| `internal/config` | 85.2 | Typed configuration, precedence, validation. |
| `internal/reason` | 85.2 | `Error` type, reason vocabulary, exit codes. |
| `internal/output` | 85.2 | JSON envelope, text rendering helpers, redaction helpers. |
| `internal/cli` | 85.2 skeleton; every feature task adds its own `<command>.go` and `<command>_test.go`; 85.14 completes and polishes | urfave/cli v3 command tree. |
| `internal/testkit` | 85.3 | Shared test helpers. |
| `internal/store` | 85.4 (driver proof) and 85.6 (schema, open, transactions, permissions, event append) | Authority storage. |
| `internal/resource` | 85.5 | Resource policies and key derivation. |
| `internal/lease` | 85.7 (one resource), 85.8 (many resources), 85.12 (`Verify`) | Claim lifecycle service. |
| `internal/ledger` | 85.9 | Operation inspection and reconciliation, events feed, history read model. |
| `internal/handle` | 85.10 | Context root, handle files, credential sources, claim selection. |
| `internal/gc` | 85.11 | Garbage collection. |
| `internal/guard` | 85.12 | Guarded exec and expected-hash replacement. |
| `internal/watch` | 85.13 | Cursor and resource watches. |
| `internal/doctor` | 85.14 | Read-only diagnostics. |
| `internal/instructions` | 85.14 | Canonical agent instruction text. |
| `internal/mcp` | 85.15 | stdio MCP server. |
| `internal/setup` | 85.16 | Setup generators and guard hooks. |
| `.github/workflows`, `scripts/`, release tasks in `mise.toml` | 85.17 (85.2 adds the Go CI jobs) | CI and release. |
| `README.md`, `docs/*.md`, `skills/`, `CHANGELOG.md`, `AGENTS.md`/`CLAUDE.md` | 85.17 rewrites; every task appends its own `CHANGELOG.md` Unreleased line | Documentation. |

Shared files any task may edit minimally: `internal/cli/commands.go` (command registration list), `mise.toml`, `go.mod`/`go.sum`, `CHANGELOG.md` under `## Unreleased`. Do not reformat or reorganize code you do not own. Implementation tasks do not rewrite the human documentation; they keep command help, Go doc comments, and the changelog current, and TASK-85.17 rewrites the documents.

## 4. Command tree

Global flags (every command): `--json`/`-j`, `--home DIR`/`-H`, `--config PATH`, `--help`/`-h`, `--version`/`-v` (root only).

Claim selection for lifecycle commands, evaluated in this order: `--handle PATH`; otherwise the contextual handle for the current directory if it exists; otherwise explicit `--claim-id ID --revision N` with exactly one of `--token-file PATH` or `--token-fd N`. Explicit `--claim-id`, `--revision`, `--token-file`, and `--token-fd` override the corresponding handle fields individually. If nothing selects a claim the command fails with `claim-selection-missing` (exit 64). Resource input for `acquire` and `key` is exactly one of: repeated `-r/--resource`, the provider triple `-p/--provider -s/--source -i/--item`, or `--path PATH`; mixing modes fails with `resource-input-conflict` (64).

| Command | Purpose | Key flags | Mutates | Owner |
| --- | --- | --- | --- | --- |
| `worklease version` | Print version, commit, build time, schema version. | | no | 85.2 |
| `worklease key` | Derive one resource and its capability metadata. | resource input, `--coordination-only` | no | 85.5 |
| `worklease policy list` / `policy describe NAME` | Show built-in policies. | `--full` | no | 85.5 |
| `worklease acquire` | Acquire a claim over 1 to 32 resources. | resource input, `--ttl`, `--wait D`, `--poll-interval D`, `--agent`, `--session`, `--work-key`, `--claim-id`, `--coordination-only`, `--handle PATH`, `--no-handle` | yes | 85.7 (85.8 lifts the one-resource limit; 85.10 adds handles; 85.5 adds provider input) |
| `worklease status` | Show the claim for the selected handle or resources. | `-r` repeated, claim selection, `--full` | no | 85.7, 85.11 polish |
| `worklease list` | List current claims. | `-r FILTER`, `--full` | no | 85.7, 85.11 polish |
| `worklease heartbeat` | Renew the claim. | `--ttl`, `--operation-id`, claim selection | yes | 85.7 |
| `worklease checkpoint` | Store recovery metadata and renew. | `--data JSON` or `--data-file PATH`, `--ttl`, `--operation-id`, claim selection | yes | 85.7 |
| `worklease release` | End the claim. | `--reason TEXT`, `--operation-id`, claim selection | yes | 85.7 |
| `worklease transfer` | Hand the claim to a successor identity. | `--to-agent`, `--to-session`, `--to-work-key`, `--successor-handle PATH`, `--ttl`, `--operation-id`, claim selection | yes | 85.7 |
| `worklease verify` | Read-only ownership check. | `-r` repeated (expected resources), `--hook claude-code`, claim selection | no | 85.12 |
| `worklease exec -- ARGV...` | Run one guarded command. | `--max-duration D`, `--cwd DIR`, `--git-primary`, `--ttl`, `--operation-id`, claim selection | yes | 85.12 |
| `worklease replace-file` | Expected-hash atomic file replacement. | `--path`, `--expected-sha256`, `--content-file`, `--ttl`, `--operation-id`, claim selection | yes | 85.12 |
| `worklease op inspect` | Inspect one operation. | `--operation-id`, `-r` repeated or claim selection, `--full` | no | 85.9 |
| `worklease op reconcile` | Record the observed outcome of an unknown operation. | `--operation-id`, `--outcome observed-success|observed-failure`, `--evidence JSON`, `--expected-request-sha256`, claim selection | yes | 85.9 |
| `worklease history` | Retained epochs for one resource. | `-r RESOURCE`, `--limit`, `--cursor`, `--full` | no | 85.9, 85.11 polish |
| `worklease events` | Global lifecycle event feed. | `--limit`, `--cursor`, `--full` | no | 85.9, 85.11 polish |
| `worklease watch` | Wait for a lifecycle change. | `--cursor C` or `-r` repeated with `--until free|change`, `--timeout D` | no | 85.13 |
| `worklease gc` | Preview or apply retention. | `--retention-days N` or `--cutoff RFC3339`, `--apply` | with `--apply` | 85.11 |
| `worklease doctor` | Read-only environment diagnostics. | | no | 85.14 |
| `worklease instructions loop|safety` | Print canonical agent instructions. | | no | 85.14 |
| `worklease setup mcp` | Preview or apply MCP client configuration. | `--client claude-code|cursor|generic`, `--scope project|user`, `--apply`, `--remove` | with `--apply` | 85.16 |
| `worklease setup guard` | Preview or apply a pre-mutation verification hook. | `--client claude-code|generic`, `--scope`, `--apply`, `--remove` | with `--apply` | 85.16 |
| `worklease setup instructions` | Print the AGENTS.md block. | | no | 85.16 |
| `worklease mcp` | Serve MCP over stdio. | | via tools | 85.15 |

Short options have exactly one meaning across the tree: `-j` json, `-H` home, `-r` resource, `-p` provider, `-s` source, `-i` item, `-a` agent, `-w` work-key, `-T` ttl, `-W` wait, `-o` operation-id, `-m` reason, `-M` max-duration, `-F` token-file, `-D` token-fd, `-R` revision, `-c` claim-id, `-C` coordination-only, `-f` full, `-h` help, `-v` version. Do not add other short options.

Help text: every command has a one-line `Usage`, a `Description` with at least one example, and flag usage strings that name the environment variable in square brackets where one applies (hum style: `state directory [$WORKLEASE_HOME]`).

## 5. Configuration

Resolution order for each key: flag, environment, YAML, default. Blank strings are treated as unset at every level.

| Key | Flag | Environment | YAML key | Default | Bounds |
| --- | --- | --- | --- | --- | --- |
| home | `--home` | `WORKLEASE_HOME` | `home` | `$XDG_STATE_HOME/worklease`, else `~/.local/state/worklease` | absolute after expansion |
| agent ID | `--agent` (acquire, transfer) | `WORKLEASE_AGENT_ID` | `agent_id` | current OS user name via `os/user`; if unavailable, error `agent-id-required` | 1 to 128 bytes, no control characters |
| ttl | `--ttl` | `WORKLEASE_TTL` | `ttl` | `15m` | `1s` to `1h` |
| max duration | `--max-duration` | `WORKLEASE_MAX_DURATION` | `max_duration` | `1h` | `1s` to `24h` |
| retention | `--retention-days` | `WORKLEASE_RETENTION_DAYS` | `retention_days` | `30` | greater than 0 |
| poll interval | `--poll-interval` | `WORKLEASE_POLL_INTERVAL` | `poll_interval` | `250ms` | `10ms` to `30s` |
| config path | `--config` | `WORKLEASE_CONFIG` | | `$XDG_CONFIG_HOME/worklease/config.yaml`, else `~/.config/worklease/config.yaml` | regular file |

Durations accept Go `time.ParseDuration` syntax and bare integers meaning seconds. Unknown YAML keys, wrong types, or out-of-bounds values fail with `config-invalid` (64) naming the key and source. A missing default config file is not an error; a missing file named by `--config` or `WORKLEASE_CONFIG` is `config-missing` (64). The config file may not contain secrets and needs no special permissions, but it must be a regular file (no symlink). `config.Config` records the source of every resolved value (`flag`, `env`, `file`, `default`) for `doctor`.

## 6. Output, errors, and exit codes

JSON success: `{"schemaVersion":2,"operation":"<command>","ok":true, ...fields}`. JSON failure: `{"schemaVersion":2,"operation":"<command>","ok":false,"error":{"reason":"<kebab-case>","exitCode":N,"message":"<human text>","details":{...}}}`. One JSON document per invocation on stdout; nothing else on stdout in JSON mode. Timestamps are RFC 3339 with microseconds in UTC. Durations are seconds as JSON numbers. Fields never carry secrets (section 6.3).

Text mode: a first line summarizing the outcome (for example `acquired 1 resource as claim 3f9c... (expires in 15m)`), then `key: value` lines relevant to that operation, deterministic ordering, UTF-8, control characters escaped. Failures print `error: <reason>: <message>` and optional `hint:` lines to stderr. Text is designed for people; machine consumers must use `--json` and exit codes.

### 6.1 Exit codes

| Code | Family | Example reasons |
| --- | --- | --- |
| 0 | success | |
| 1 | internal failure (bug or unexpected I/O) | `internal` |
| 2 | ownership or contention | `already-claimed`, `stale-claim`, `invalid-token`, `stale-revision`, `claim-expired`, `wait-timeout`, `verify-failed`, `ownership-lost`, `handle-in-use` |
| 3 | ledger or idempotency | `operation-request-mismatch`, `unknown-outcome`, `expected-hash-mismatch`, `gc-protected-record`, `reconciliation-conflict`, `operation-ambiguous` |
| 64 | invalid input or configuration | `invalid-argument`, `config-invalid`, `config-missing`, `claim-selection-missing`, `resource-input-conflict`, `invalid-resource`, `unknown-policy`, `invalid-path`, `handle-unsafe`, `handle-malformed`, `credential-unsafe`, `credential-malformed`, `credential-source-conflict`, `agent-id-required`, `unsupported-coordination-replace`, `cursor-invalid`, `setup-config-malformed` |
| 75 | authority or storage failure | `home-unsafe`, `storage-failure`, `schema-unsupported`, `schema-corrupt`, `handle-write-failed` |
| 124 | guarded child exceeded `--max-duration` | `child-timeout` |
| 130 | interrupted by SIGINT or SIGTERM before any durable effect | `interrupted` |
| child status | `exec` propagates the child's exit status once the child started (signals map to 128 plus the signal number) | |

Reasons are stable identifiers; messages may change. `internal/reason` holds the complete vocabulary as constants with their exit codes, and a test asserts every emitted reason is registered.

### 6.2 Partial success

A mutation that commits but cannot write its handle exits 75 with reason `handle-write-failed` and still emits the successful claim payload, including the token (the only copy), plus `handleError`. Text mode prints the same payload to stdout and a recovery hint to stderr. No other command has partial success.

### 6.3 Redaction

Never emitted anywhere except where stated: tokens (only in acquire and transfer output when no handle is written, and in the `handle-write-failed` payload), token hashes (never), handle file contents (never), checkpoint bodies (only in `status --full` for the selected claim and in `history --full`), child stdout and stderr (only in the `exec` receipt of the invoking command and `op inspect --full`), reconciliation evidence (only in `op inspect --full`), request and receipt blobs (never in `list`, `events`, default `history`, `doctor`, or MCP logs). Error messages never echo rejected credential or handle content.

## 7. Domain model and invariants

### 7.1 Resources

An opaque UTF-8 string, 1 to 1024 bytes, without NUL, CR, or LF, not starting or ending with whitespace. The authority never interprets it. Everyone contending for the same unit must present identical bytes; policies (section 7.13) exist to make that deterministic.

### 7.2 Claims and epochs

A claim has `claimId`, `resources` (ordered, unique, 1 to 32), `agentId`, `sessionId`, `workKey`, `guarantee` (`fenced` or `local-coordination`), `revision`, `acquiredAt`, `ttl`, `heartbeatAt`, `expiresAt`, and optional `checkpoint`. A resource is `free` when no claim row references it, `active` when its claim has `now < expiresAt`, and `expired` otherwise. Exactly one claim may reference a resource at a time (primary key on `claim_resources.resource`). Expiry is lazy: reading does not mutate. The next `acquire` touching an expired resource replaces the old claim inside the same transaction, finalizing its epoch with reason `expired` effective at the stored `expiresAt` and recorded at the replacement time (event `expired-replaced`). Every claim is also an epoch row that outlives the claim and records its end.

### 7.3 Revision and credentials

`revision` starts at 1 on acquire and increments by exactly one per successful mutation (heartbeat, checkpoint, exec, replace-file, reconcile; transfer starts the successor at 1). A mutation supplies `claimId`, `token`, and expected `revision`. Checks run in this order and stop at the first failure: claim ID not current → `stale-claim`; token hash mismatch → `invalid-token`; claim expired → `claim-expired`; revision mismatch → `stale-revision` with `expectedRevision` and `suppliedRevision` in details. Failed checks never mutate.

### 7.4 Operations and idempotency

Every mutation carries an `operationId` (caller-supplied or generated). The request hash is SHA-256 over canonical JSON (sorted keys, no whitespace) of the request with `operationId`, `revision`, and `ttl` removed. Replay of the same `operationId` on the same claim with an equal hash returns the stored receipt with `idempotent: true` and changes nothing; a different hash fails `operation-request-mismatch`; a `started` operation fails `unknown-outcome` until reconciled. Replay never repeats external effects. Operation IDs are unique per claim; reuse across kinds is a mismatch.

### 7.5 Guarded intent

`exec` and `replace-file` write an operation row in state `started` in one transaction before producing any external effect, then write `completed` with the receipt afterwards. A crash between the two leaves `started`, which `status`, `op inspect`, and `verify` report as an unknown outcome. `op reconcile` records exactly one observed outcome with caller evidence under current credentials, sets state `reconciled`, increments the revision, and appends `reconciled`. Reconciling twice with identical input is idempotent; with different input it fails `reconciliation-conflict`.

### 7.6 Clocks

Heartbeat and checkpoint set `heartbeatAt = now` and `expiresAt = now + ttl`. Acquire sets `acquiredAt = heartbeatAt = now`. Forward jumps expire claims normally. Backward regression: if a reader observes `now < heartbeatAt - 1s`, the claim is treated as active; if that reader is an acquire attempt on the same resource, it re-anchors `heartbeatAt = now` and `expiresAt = now + (stored expiresAt - stored heartbeatAt)` in its transaction and appends `clock-regression`, so the claim still expires eventually. Waits and child deadlines use monotonic time only.

### 7.7 Transfer

Requires current credentials. Creates a successor claim (new `claimId`, new token, `revision = 1`, same resources, same guarantee, successor identity from `--to-*` flags with the same defaults as acquire, checkpoint carried over) and ends the predecessor epoch with reason `transferred` and `successorClaimId`, in one transaction with no free interval. The successor token is written to `--successor-handle PATH` when given; otherwise it is printed once in the JSON payload (text mode prints it with a warning line). Transfer replay follows section 7.4.

### 7.8 Release and checkpoint

Release requires current credentials, accepts an optional `--reason` (default `released`), removes the claim rows, ends the epoch with reason `released`, retains the last checkpoint on the epoch, and appends `released`. Checkpoint data is JSON up to 8 KiB, stored on the claim, renews the TTL, and is returned only as described in 6.3.

### 7.9 Contention and waiting

Acquire fails `already-claimed` when any requested resource is held by an active claim, reporting the holder's non-secret metadata (`claimId`, `agentId`, `workKey`, `expiresAt`) for the first contended resource in caller order. With `--wait D` it retries at `poll_interval` with plus or minus 20 percent jitter until the monotonic deadline, then fails `wait-timeout` carrying the last holder metadata. Contention on a resource held as part of a larger claim reports that claim.

### 7.10 Many resources

A claim over N resources acquires all or none in one transaction. Resources are inserted in caller order; ordering is not needed for deadlock avoidance because SQLite serializes writers. Duplicate resources fail `invalid-resource`. All lifecycle commands act on the whole claim; a resource cannot be released or transferred individually.

### 7.11 Guarantee

`fenced` means same-host cooperative fencing of guarded local operations among callers sharing one authority; it never means provider fencing. Direct `-r` input and policies with capability `item-claim` or `source-claim` yield `fenced`; `--coordination-only` or a policy with capability `local-coordination` yields `local-coordination`. `replace-file` requires `fenced`.

### 7.12 Events

Kinds: `acquired`, `renewed`, `checkpointed`, `exec-started`, `exec-completed`, `replace-started`, `replace-completed`, `reconciled`, `transferred`, `released`, `expired-replaced`, `expired-retired`, `clock-regression`, `gc-applied`. Each row carries `at`, `kind`, `claimId`, `resources` (JSON array), `operationId` (nullable), `revision` (nullable), `agentId`, and a non-secret `detail` JSON object (for example exit status, reason text, counts). Events never contain tokens, hashes, checkpoints, output, or evidence.

### 7.13 Resource policies

Built-in policies and exact derivations. `<common>` is the absolute, symlink-resolved `git rev-parse --git-common-dir` of the repository containing the source path (so linked worktrees agree); `<locator>` is the source path relative to `git rev-parse --show-toplevel` in POSIX form; outside Git, `<common>` and `<locator>` are both the absolute resolved source path. Git is probed with `GIT_*` environment variables removed.

| Policy | Input | Resource | Capability | Scope |
| --- | --- | --- | --- | --- |
| `backlog-md` | source = backlog directory, item = task ID | `backlog-md:<common>:<locator>:<item>` | `item-claim` | `item` |
| `markdown` | source = Markdown file or directory; item is accepted but not part of the identity | `markdown:<common>:<locator>:__source__` | `source-claim` | `source` |
| `github` | source = `owner/repo` (lowercased, `.git` stripped), item = number | `github:<source>#<item>` | `local-coordination` | `item` |
| `linear` | source = team or project key, item = issue key | `coordination:linear:<sha256 of canonical JSON {item,provider,source}>` | `local-coordination` | `item` |
| `generic` | any source and item | `coordination:generic:<sha256 of canonical JSON {item,provider,source}>` | `local-coordination` | `item` |
| `path` | `--path P` inside a Git repository | `path:<common>:<repo-relative POSIX path>` | `item-claim` | `path` |

`path` rejects paths outside the repository, `..` traversal after normalization, and non-Git directories (`invalid-path` with a hint), and claims exact paths only: no parent, child, or glob overlap is implied. Every key result reports `provider`, `capability`, `scope`, `fencedMutations`, `providerFencing: false`, and `resource`. Policies are registered in a static Go map; unknown names fail `unknown-policy` listing the available names. No dynamic plugins, entry points, network, or credentials.

## 8. Storage

Home layout, created on first use with a `0077` umask applied by the process for these paths:

```
<home>/                          0700  owned by the effective user
<home>/worklease.db              0600  plus -wal and -shm
<home>/handles/                  0700
<home>/handles/ctx-<sha256>.json 0600  contextual handles
<home>/handles/mcp-<ref>.json    0600  MCP handles
```

Safety checks on every open: `<home>` and `<home>/handles` must be directories owned by the effective UID with no group or other permission bits (fix by `chmod 0700` if owned; fail `home-unsafe` if foreign-owned or a symlink); the database path is checked with `lstat` and must be a regular file or absent; files are opened with `O_NOFOLLOW|O_CLOEXEC`. Symlinked or foreign files fail `home-unsafe` (75).

Schema version 1 (`PRAGMA user_version = 1`), created inside one `BEGIN IMMEDIATE` transaction:

```sql
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
-- keys: created_at, pruned_through_seq (integer as text, default 0)

CREATE TABLE claims (
  claim_id      TEXT PRIMARY KEY,
  token_hash    TEXT NOT NULL,
  revision      INTEGER NOT NULL,
  agent_id      TEXT NOT NULL,
  session_id    TEXT NOT NULL,
  work_key      TEXT NOT NULL,
  guarantee     TEXT NOT NULL CHECK (guarantee IN ('fenced','local-coordination')),
  acquired_at   INTEGER NOT NULL,   -- unix microseconds
  ttl_us        INTEGER NOT NULL,
  heartbeat_at  INTEGER NOT NULL,
  expires_at    INTEGER NOT NULL,
  checkpoint    TEXT                -- JSON or NULL
);

CREATE TABLE claim_resources (
  resource  TEXT PRIMARY KEY,
  claim_id  TEXT NOT NULL REFERENCES claims(claim_id) ON DELETE CASCADE,
  position  INTEGER NOT NULL
);
CREATE INDEX claim_resources_by_claim ON claim_resources(claim_id);

CREATE TABLE epochs (
  claim_id            TEXT PRIMARY KEY,
  agent_id            TEXT NOT NULL,
  session_id          TEXT NOT NULL,
  work_key            TEXT NOT NULL,
  guarantee           TEXT NOT NULL,
  acquired_at         INTEGER NOT NULL,
  acquired_seq        INTEGER NOT NULL,
  ended_at            INTEGER,           -- effective end time
  ended_seq           INTEGER,           -- event seq that recorded the end
  end_reason          TEXT CHECK (end_reason IN ('released','transferred','expired')),
  final_revision      INTEGER,
  successor_claim_id  TEXT,
  checkpoint          TEXT               -- last checkpoint retained at end
);
CREATE INDEX epochs_by_acquired_seq ON epochs(acquired_seq);

CREATE TABLE epoch_resources (
  claim_id  TEXT NOT NULL REFERENCES epochs(claim_id) ON DELETE CASCADE,
  resource  TEXT NOT NULL,
  position  INTEGER NOT NULL,
  PRIMARY KEY (claim_id, position)
);
CREATE INDEX epoch_resources_by_resource ON epoch_resources(resource, claim_id);

CREATE TABLE operations (
  claim_id          TEXT NOT NULL,
  operation_id      TEXT NOT NULL,
  kind              TEXT NOT NULL,
  request_hash      TEXT NOT NULL,
  expected_revision INTEGER NOT NULL,
  state             TEXT NOT NULL CHECK (state IN ('started','completed','reconciled')),
  receipt           TEXT,               -- JSON; non-secret except exec output
  started_at        INTEGER NOT NULL,
  started_seq       INTEGER NOT NULL,
  completed_at      INTEGER,
  completed_seq     INTEGER,
  PRIMARY KEY (claim_id, operation_id)
);
CREATE INDEX operations_by_state ON operations(state);

CREATE TABLE reconciliations (
  claim_id                 TEXT NOT NULL,
  operation_id             TEXT NOT NULL,
  outcome                  TEXT NOT NULL CHECK (outcome IN ('observed-success','observed-failure')),
  evidence                 TEXT NOT NULL,   -- JSON
  request_hash             TEXT NOT NULL,
  reconcile_operation_id   TEXT NOT NULL,
  resolver_agent_id        TEXT NOT NULL,
  resolver_session_id      TEXT NOT NULL,
  recorded_at              INTEGER NOT NULL,
  recorded_seq             INTEGER NOT NULL,
  PRIMARY KEY (claim_id, operation_id)
);

CREATE TABLE events (
  seq           INTEGER PRIMARY KEY AUTOINCREMENT,
  at            INTEGER NOT NULL,
  kind          TEXT NOT NULL,
  claim_id      TEXT,
  resources     TEXT NOT NULL,          -- JSON array
  operation_id  TEXT,
  revision      INTEGER,
  agent_id      TEXT,
  detail        TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX events_by_claim ON events(claim_id, seq);
CREATE INDEX events_by_at ON events(at);
```

Open procedure: ensure home safety; open with the pragmas from D8; read `user_version`; `0` on an empty database → create the schema and set `1`; `1` → verify the expected tables exist (`schema-corrupt` if not); anything else → `schema-unsupported`. Until `v1.0.0` ships, tasks may change schema version 1 in place (no migrations between pre-release states; tests always start from empty homes). After `v1.0.0`, changes add numbered migrations applied in one serialized transaction.

Transaction API: `store.Write(ctx, fn)` opens `BEGIN IMMEDIATE`, runs `fn(tx)`, commits or rolls back; `store.Read(ctx, fn)` opens a deferred transaction. `tx.AppendEvent(Event) (seq, error)` is the only way to write events. Context cancellation aborts the statement and rolls back. The store uses `SetMaxOpenConns(1)` for its write connection.

## 9. Handles and credentials

Context root: run `git -C <cwd> rev-parse --show-toplevel` with `GIT_*` variables removed from the environment; if it succeeds use the resolved path, otherwise the resolved current directory. Linked worktrees therefore have distinct contexts (one checkout, one handle); subdirectories share the root. Context ID is the lowercase hex SHA-256 of the root path bytes. Contextual handle path: `<home>/handles/ctx-<contextId>.json`.

Handle file: JSON `{"schemaVersion":1,"claimId":"...","token":"...","revision":N,"resources":["..."],"expiresAt":"RFC3339","guarantee":"...","agentId":"..."}`, at most 64 KiB, mode 0600. Writes go to a temporary file in the same directory, `fsync`, `rename`, then `fsync` the directory. Reads fail `handle-unsafe` (64) for symlinks, non-regular files, foreign owners, or group/other permission bits, and `handle-malformed` (64) for oversized, non-JSON, wrong-version, or missing-field content. Error messages name the path only.

Acquire behavior: without `--handle` or `--no-handle`, write the contextual handle; if a handle already exists at the destination and its claim is still current and active, fail `handle-in-use` (2) before touching the authority; if it is stale (released, expired, or unknown claim), replace it. `--handle PATH` writes there with the same rules; the parent directory must exist. Heartbeat, checkpoint, exec, replace-file, and reconcile rewrite the handle with the new revision (and expiry) after commit. Release removes the handle after commit. Transfer removes the predecessor handle after commit and writes the successor handle only when `--successor-handle` is given. Idempotent replays never rewind a handle's revision.

Credential sources for explicit selection: `--token-file PATH` must be a regular, owner-only (`0600` or stricter), non-symlink file of at most 4096 bytes; `--token-fd N` duplicates the descriptor with `CLOEXEC` and reads at most 4096 bytes. The value is one line; a single trailing newline is stripped; empty, multi-line, NUL-containing, or non-UTF-8 content is `credential-malformed`. Supplying both sources is `credential-source-conflict`.

## 10. Guarded operations

### 10.1 exec

Runs exactly the argv after `--` without a shell, in a new process group (`Setpgid`), stdin from `/dev/null`, stdout and stderr captured up to 1 MiB each (the receipt records `stdoutBytes`, `stderrBytes`, `stdoutTruncated`, `stderrTruncated`, and the captured text with invalid UTF-8 replaced). Working directory: caller directory by default, `--cwd DIR`, or `--git-primary` (the primary worktree of the repository containing the caller directory, resolved through `git rev-parse --git-common-dir`; fails `invalid-path` when the caller is not in a repository or the primary worktree is missing). When `--cwd` or `--git-primary` is used, `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_COMMON_DIR`, `GIT_PREFIX`, and `GIT_OBJECT_DIRECTORY` are removed from the child environment. The child receives `WORKLEASE_CLAIM_ID` and `WORKLEASE_OPERATION_ID`; never the token.

Sequence: validate input and credentials; write `started` (event `exec-started`) and renew the claim; spawn; while the child runs, heartbeat at `ttl/2`; if a heartbeat fails with an ownership reason, send SIGTERM to the process group, wait 2 s, SIGKILL the group, and fail `ownership-lost` (2) after recording `completed` with `terminated: "ownership-lost"`; when `--max-duration` elapses (monotonic, covering pipe draining), terminate the same way, stop draining, and exit 124 with `timedOut: true`; otherwise record `completed` with the exit status and exit with the child's status. Replay returns the stored receipt without spawning. Storage failure after spawn terminates the group and leaves the operation `started`.

### 10.2 replace-file

Inputs: `--path` (target, absolute or relative to the caller directory), `--expected-sha256` (64 hex), `--content-file`. Rejects symlink or non-regular target and content files, and `local-coordination` claims (`unsupported-coordination-replace`, 64). Sequence: compute the content hash; write `started` (event `replace-started`); inside the completing transaction re-verify credentials, then read the target and compare with the expected hash (`expected-hash-mismatch`, 3, with `actualSha256`); write a temporary file in the target directory with the target's mode, `fsync`, `rename`, `fsync` the directory; write `completed` with `previousSha256` and `sha256`. Replay with the same request returns the receipt; if the target already has the new hash and the operation is `started` (crash after rename), the replay completes it idempotently.

### 10.3 verify

Read-only. Selects a claim (section 4), then checks in order: handle or credentials readable; claim ID current; token matches; not expired; revision equals the handle's revision (skipped for explicit credentials without `--revision`); when `-r` is given, resources equal the claim's resources in order; no `started` operation is pending. Success prints `ok: true`, `claimId`, `resources`, `revision`, `expiresAt`, `expiresIn`. Failure exits 2 with reason `verify-failed` and `details.cause` naming the failed check (`missing-handle`, `stale-claim`, `invalid-token`, `claim-expired`, `stale-revision`, `resource-mismatch`, `unknown-outcome-pending`). `verify` never writes the database, events, operations, or the handle. `--hook claude-code` reads and discards the hook JSON on stdin, prints a one-line block reason to stderr on failure, and uses exit 2 to block and 0 to allow. Documentation and help state that verify is a cooperative precondition, not a fence around a later native edit.

## 11. Events, history, watch, and garbage collection

Cursors are opaque strings; the internal form is the decimal `seq`. `events` returns rows with `seq > cursor` in ascending order up to `--limit` (default 50, maximum 1000) and `nextCursor` = last returned `seq`; without `--cursor` it returns the most recent `limit` rows ascending and `nextCursor` = maximum `seq`. If `cursor < meta.pruned_through_seq`, the result is `{"ok":true,"gap":true,"prunedThrough":N,"nextCursor":"N","events":[]}` so callers never skip silently into a false continuation. Malformed cursors fail `cursor-invalid` (64) without opening the database.

`history -r R` lists epochs whose `epoch_resources` include R, ordered by `acquired_seq`, paginated by `acquired_seq` with the same cursor rules, each with `status` in `open` (current and active), `expired-open` (current but past expiry), or `complete` (ended with reason), its operations (non-secret summaries), and reconciliations (outcome only). It reports `coverage: {prunedThroughSeq, retainedFromSeq}` and never claims completeness beyond retained rows. `--full` adds checkpoint bodies and receipts of the epochs.

`watch --cursor C` returns the first event with `seq > C` whose `resources` intersect `-r` (or any event when no `-r`), or `{"ok":true,"timedOut":true,"cursor":"<max seq>"}` after `--timeout` (default 30s, maximum 1h; MCP maximum 60s). `watch -r R --until free` returns immediately if R is free, otherwise waits for an event ending the holding epoch (`released`, `transferred`, `expired-replaced`, `expired-retired`) and re-checks freeness. `--until change` returns on any event touching R. Gap cursors return the gap result immediately. Waiting polls `SELECT MAX(seq) FROM events` on an adaptive interval from 50 ms to 500 ms, holds no transaction between polls, honors context cancellation, and leaks no goroutines.

`gc` computes `cutoff = now - retention_days` or the explicit `--cutoff` (must be at or before now). Eligible: epochs with `ended_at < cutoff` (with their operations, reconciliations, and events up to `ended_seq`), and current claims with `expires_at < cutoff` and no `started` operations, which are retired by writing an `expired` end and `expired-retired` event. Protected and reported: active claims, and epochs or claims with unresolved `started` operations. Events are pruned only below the minimum retained `acquired_seq`, and `meta.pruned_through_seq` is raised to the highest pruned `seq`. Dry run is the default and reports counts and oldest/newest per category; `--apply` performs everything in one `BEGIN IMMEDIATE` transaction and appends `gc-applied` with the counts.

## 12. MCP server

`worklease mcp` reads newline-delimited JSON-RPC 2.0 from stdin and writes responses to stdout; stderr carries redacted logs. Implementation pattern: hum `internal/mcp/server.go` (request registry with duplicate-ID rejection, per-request cancellation via `notifications/cancelled`, serialized response writer, 4 MiB message limit, EOF and parent-context shutdown). Concurrency limit: 8 in-flight tool calls. Shutdown waits up to 5 s for in-flight calls.

Tools (`tools/list` returns these names, descriptions, and JSON Schemas; domain failures are tool results with `isError: true` and structured `{ok:false,error:{reason,exitCode,message}}`; protocol failures are JSON-RPC errors):

| Tool | Input | Output |
| --- | --- | --- |
| `key` | `{provider, source, item}` or `{path}`, optional `coordinationOnly` | key result (section 7.13) |
| `acquire` | `{resources[]}` or provider triple or `{path}`; optional `ttl`, `wait` (at most 60 s), `workKey`, `agentId`, `coordinationOnly`, `autoHeartbeat` (default true), `maxHold` (default 4h) | `{lease, claim (no token), autoHeartbeat}` |
| `status` | `{lease}` or `{resources[]}` | claim without token, `unknownOperations` |
| `list` | optional `{resource}` | claims without tokens |
| `heartbeat` | `{lease, ttl?}` | receipt |
| `checkpoint` | `{lease, data, ttl?}` | receipt |
| `verify` | `{lease, resources?}` | verify result |
| `watch` | `{cursor}` or `{resources[], until}`, `timeout` (at most 60 s) | watch result |
| `events` | `{cursor?, limit?}` | events page |
| `release` | `{lease, reason?}` | receipt |
| `instructions` | `{topic: loop|safety}` | text lines |

`lease` is a 32-hex random reference whose handle lives at `<home>/handles/mcp-<ref>.json` (0600). The client never sees tokens or revisions. Automatic heartbeat runs per lease at `ttl/2`, is serialized with explicit mutations through a per-lease mutex, stops after `maxHold`, on expiry or ownership failure, on `release`, and on shutdown, and never releases the claim itself. Results report `autoHeartbeat` as `active`, `stopped`, or `disabled`. On stdin EOF the server stops heartbeats and exits without releasing anything. The server uses the same config resolution as the CLI (section 5).

## 13. Setup, guard, doctor, and instructions

`setup mcp --client claude-code` targets `.mcp.json` in the current project (scope `project`) or `~/.claude.json` (scope `user`), inserting `mcpServers.worklease = {"command": "<absolute path of the running worklease binary>", "args": ["mcp"]}` (with `env.WORKLEASE_AGENT_ID` only when `--agent` is given). `--client cursor` targets `.cursor/mcp.json` or `~/.cursor/mcp.json` with the same shape. `--client generic` prints the JSON snippet only. Default is a preview showing the target path and a unified diff; `--apply` writes atomically (temporary file plus rename), preserving all unrelated keys semantically (formatting is normalized to two-space indentation); a non-object root or invalid JSON fails `setup-config-malformed` (64) without writing. `--remove` deletes only `mcpServers.worklease`. Apply and remove are idempotent.

`setup guard --client claude-code` targets `.claude/settings.json` (project) or `~/.claude/settings.json` (user) and inserts one `hooks.PreToolUse` entry with matcher `Edit|Write|MultiEdit|NotebookEdit|Bash` and command `worklease verify --hook claude-code`, identified for removal by that command string. `setup guard --client generic` prints a POSIX shell wrapper that runs `worklease verify` before a mutating command. Documentation states the boundaries: hooks are cooperative, unsupported tools bypass them, a mutation after verification is not fenced (time-of-check versus time-of-use), direct provider writes are never fenced, and coordination is same-host only.

`setup instructions` prints an AGENTS.md block containing the `worklease instructions loop` and `safety` text between `<!-- worklease:begin vX.Y.Z -->` and `<!-- worklease:end -->` markers. `instructions loop|safety` returns the canonical text held in `internal/instructions`, adapted from `src/worklease/instructions.py` to the Go command names.

`doctor` runs read-only checks and prints one line per check: `id`, `status` (`ok`, `warn`, `fail`, `unknown`), `detail`, and optional `hint`. Check IDs: `config.sources`, `home.path`, `home.permissions`, `db.open` (read-only open), `db.schema`, `context.root`, `handle.present`, `handle.permissions`, `agent.identity`, `git.available`, `clock.monotonic`, `mcp.available`. It creates no directories or files, never reads token fields, and states that it cannot verify other hosts or provider-side fencing. Exit 0 when no check is `fail`, otherwise 1.

## 14. Build, test, and release

`mise.toml` tools: `go = "1.27.1"`, `staticcheck`, `"go:golang.org/x/vuln/cmd/govulncheck"`, `lefthook`, plus the existing Backlog.md tool. Mise tasks added by TASK-85.2 and kept until TASK-85.18 renames them:

| Task | Command |
| --- | --- |
| `go-build` | `go build -o bin/worklease ./cmd/worklease` |
| `go-fmt` / `go-fmt-check` | `gofmt -w` / `test -z "$(gofmt -l .)"` over Go files |
| `go-vet` | `go vet ./...` and `staticcheck ./...` |
| `go-test` | `go test ./...` |
| `go-race` | `go test -race ./...` |
| `go-vuln` | `govulncheck ./...` |
| `go-man` | `go run ./cmd/worklease-man ... > dist/worklease.1` (85.17) |
| `go-smoke` | build, then run the built-binary smoke test (85.17) |
| `ci-go` | `go-fmt-check`, `go-vet`, `go-test`, `go-race`, `go-vuln` |

CI: a Go job matrix on `ubuntu-latest`, `ubuntu-24.04-arm`, `macos-15-intel`, and `macos-14` runs `mise run ci-go`; the Python jobs remain until TASK-85.18 removes them. Release (TASK-85.17): on `v*` tags, verify CI passed for the commit, build the four archives with `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.buildVersion=<version> -X main.buildCommit=<sha> -X main.buildTime=<iso>"`, generate the man page, write `checksums.txt`, install every archive in a clean directory, run `worklease version --json`, `worklease key`, an acquire/verify/release lifecycle against a temporary home, and an MCP `initialize` plus `tools/list` round trip, then publish with `gh release create`.

Test conventions: tests create isolated homes with `t.TempDir()` and `WORKLEASE_HOME`; clocks are injected (`testkit.Clock`); identifiers and tokens come from an injected generator; concurrency tests bound every wait with a deadline and fail with diagnostics rather than hanging; multi-process tests run the test binary itself as a subprocess (`os.Args[0]` with a `WORKLEASE_TEST_HELPER` marker) so they need no separate build step.

## 15. Amendment procedure

Amend only when: (a) a spike (TASK-85.4) records a required deviation; (b) an implementation task proves a fixed decision impossible or unsafe with executable evidence; (c) the repository owner requests a change. Never amend for preference. To amend: add an entry to section 17 of this document (`backlog doc update <doc-id> --content ...` after reading the current content) with the date, task ID, the decision ID or section changed, the new text, and the evidence; then add a comment on TASK-85 summarizing it. Implementation continues under the amended text. Tasks must not carry contradictory local designs in notes or code comments instead of amending.

## 16. Retired Python surfaces

Removed at TASK-85.18 with no replacement: the Python public API (`worklease.__all__`), the `worklease-mcp` entry point, the `worklease_source_sdk` package and example plugin, entry-point resource-policy plugins, PyInstaller and `uv` packaging, JSON schema version 1 files, the Python lease-file format and the `context-leases` and `mcp-leases` directories, the `owner ID` identity, `--token` on argv, bundle-specific commands (`acquire-bundle`, `status-bundle`, `heartbeat-bundle`, `release-bundle`, `exec-bundle`, `inspect-operation-bundle`, `reconcile-operation-bundle`) and their aliases, `--provider-directory` (renamed `--cwd`), the `--format` flag (only `--json` remains), the MCP lifecycle benchmark, `docs/source-provider-sdk-compatibility.md`, `docs/distributed-cloudflare-claim-authority.md`, and the Python-era SQLite schema versions 1 to 3 (no import). Python-era state directories are left untouched on disk; installation docs tell users to delete them.

## 17. Amendments

None yet.

## 18. Internal API sketches

Names below are binding; signatures may gain fields. Owning tasks implement them; later tasks depend on them.

```go
// internal/reason
type Error struct { Reason string; Code int; Message string; Details map[string]any }
func New(reason string, msg string) *Error            // code looked up from the registry
func (e *Error) Error() string
func (e *Error) With(key string, value any) *Error
func CodeFor(reason string) int

// internal/config
type Input struct { Flags map[string]string; Env func(string) string; Home, ConfigPath string }
type Config struct { Home, AgentID, ConfigPath string; TTL, MaxDuration, PollInterval time.Duration; RetentionDays float64; Sources map[string]string }
func Load(in Input) (Config, error)

// internal/store
type Options struct { ReadOnly bool }
func Open(ctx context.Context, home string, opts Options) (*Store, error)
func (s *Store) Close() error
func (s *Store) Write(ctx context.Context, fn func(*Tx) error) error   // BEGIN IMMEDIATE
func (s *Store) Read(ctx context.Context, fn func(*Tx) error) error
type Event struct { At time.Time; Kind string; ClaimID string; Resources []string; OperationID string; Revision *int64; AgentID string; Detail map[string]any }
func (tx *Tx) AppendEvent(ev Event) (int64, error)

// internal/resource
type Key struct { Provider, Source, Item, Resource, Capability, Scope string; FencedMutations bool }
type Policy interface { Name() string; Describe() Descriptor; Key(in Input) (Key, error) }
func Lookup(name string) (Policy, error)
func Names() []string

// internal/lease
type Clock interface { Now() time.Time; Monotonic() time.Duration }
type Credentials struct { ClaimID, Token string; Revision int64 }
type Service struct { /* store, clock, id generator, defaults */ }
func New(st *store.Store, clock Clock, ids IDGenerator, defaults Defaults) *Service
func (s *Service) Acquire(ctx context.Context, req AcquireRequest) (Grant, error)   // Grant.Token is the only place the token appears
func (s *Service) Status(ctx context.Context, sel Selector) (Status, error)
func (s *Service) List(ctx context.Context, filter string) ([]ClaimView, error)
func (s *Service) Heartbeat(ctx context.Context, creds Credentials, req Renew) (Receipt, error)
func (s *Service) Checkpoint(ctx context.Context, creds Credentials, req CheckpointRequest) (Receipt, error)
func (s *Service) Release(ctx context.Context, creds Credentials, req ReleaseRequest) (Receipt, error)
func (s *Service) Transfer(ctx context.Context, creds Credentials, req TransferRequest) (Grant, error)
func (s *Service) Verify(ctx context.Context, creds Credentials, expected []string) (Verification, error)   // 85.12
func (s *Service) BeginOperation(ctx context.Context, creds Credentials, op OperationIntent) (Started, error)     // 85.7; used by guard
func (s *Service) CompleteOperation(ctx context.Context, creds Credentials, id string, receipt map[string]any) (Receipt, error)

// internal/handle
func ContextRoot(cwd string, run GitRunner) (string, error)
func ContextualPath(home, root string) string
func Read(path string) (Handle, error)
func Write(path string, h Handle) error
func Remove(path string) error
func Select(in SelectionInput) (Selection, error)     // implements the section 4 precedence

// internal/guard
func Exec(ctx context.Context, svc *lease.Service, creds lease.Credentials, req ExecRequest) (ExecResult, error)
func ReplaceFile(ctx context.Context, svc *lease.Service, creds lease.Credentials, req ReplaceRequest) (ReplaceResult, error)

// internal/watch
func Wait(ctx context.Context, st *store.Store, req Request) (Result, error)

// internal/cli
func NewRootCommand(version, commit, buildTime string, stdout, stderr io.Writer) *cli.Command
```

## 19. Working this plan unattended

- Select only tasks labeled `go-rewrite` (milestone `m-0`) and only when every dependency is `Done`. Waves that may run in parallel: {85.1, 85.2} → {85.3, 85.4} → {85.5, 85.6} → {85.7} → {85.8, 85.10} → {85.9, 85.12} → {85.11, 85.13} → {85.14, 85.15} → {85.16} → {85.17} → {85.18} → {85}.
- Before coding: `backlog task view TASK-85.N --plain`, read this document, read the Python files listed in the task as evidence, and the hum files listed as patterns. Record the plan with `backlog task edit --plan`.
- Own only the paths in section 3. Register new commands in `internal/cli/commands.go`. Append one `CHANGELOG.md` Unreleased line per user-visible change.
- Gate: `mise run ci-go` must pass before finalization. Do not weaken or skip tests.
- Finalize per `backlog instructions task-finalization`: check each acceptance criterion with named test functions or command output as evidence, write the final summary, set `Done`, and commit with a concise imperative message (for example `Add Go lease service`).
- If a fixed decision blocks you, follow section 15. If the owner must decide, record the exact question as a task comment, finish everything else, and leave the task `In Progress` with the blocker in notes.
