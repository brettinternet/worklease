---
id: doc-2
title: Go Product Contract
type: specification
created_date: '2026-09-12 03:51'
updated_date: '2026-09-13 02:46'
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
- The Go implementation is POSIX-only (Linux and macOS). V1 coordinates cooperating processes using the same authority on the same host. Arbitrary child processes and direct provider writes are not fenced. Section 20 preserves the boundary for future remote authority; remote implementation is deferred.
- Every capability below is either retained as specified or listed as removed. When the Python code does something this document does not mention, treat it as removed unless the capability inventory (TASK-85.1) says otherwise.

### 1.1 Interaction priorities

Human command-line use and AI orchestration through JSON/MCP are first-class release requirements, not optional polish. Keep the common path short; expose recovery and stateless credential machinery progressively without weakening ownership checks.

- Human CLI: a fresh installation needs no configuration file, MCP setup, hook installation, hand-generated identifiers or token handling for an ordinary local claim. In one checkout/loop, `worklease acquire --path README.md`, `worklease status`, `worklease exec -- git diff -- README.md`, and `worklease release` form the minimal journey. Root help and command examples lead with these common forms. Text results are concise; contention identifies the holder/expiry and errors explain a safe next action. Acquisition is fail-fast unless the caller explicitly requests waiting.
- Concurrent loops: set a distinct stable `WORKLEASE_SESSION_ID` once in each loop's environment, or select an explicit handle. Subsequent commands reuse it; callers need not repeat claim IDs, tokens or revisions. Do not market the unscoped checkout convenience as multi-loop isolation.
- JSON CLI: adding `--json` retains the same convenient handle-backed workflow; it does not require `--no-handle` or manual credentials. Commands never prompt for missing input, and each command result is one section 6 envelope on stdout, without progress/log text. Agents branch on structured reasons, applicable error details/commit state, holder metadata and cursors, never by parsing human messages. Bounded waiting, cancellation and pending recovery remain explicit.
- MCP: discoverable descriptions and typed schemas expose the simplest valid inputs; ordinary acquisition needs only a resource input, and later lifecycle calls reuse the opaque `lease` reference. Clients do not manage tokens or revisions. Equivalent CLI/MCP operations share domain behavior and preserve structured result/error information; transport wrappers do not flatten it into prose. Keep the eleven-tool surface in section 12; document CLI-only operations and recovery escape paths instead of implying full command parity.
- Onboarding: document and execute separate short human CLI and MCP/JSON orchestration quick starts, including contention and two isolated loops. Configuration and native guard hooks are optional advanced setup. Keep detailed safety/recovery contracts available as reference, not prerequisites to trying the common path.

TASK-85.14–85.17 own executable acceptance journeys for these priorities. They add no new command family, MCP tool, compatibility layer or remote backend.

## 2. Fixed decisions

| ID | Decision | Rationale |
| --- | --- | --- |
| D1 | Go module `github.com/brettinternet/worklease`; Go `1.27.1` pinned in `mise.toml`; binary `worklease` built from `cmd/worklease`; man-page generator `cmd/worklease-man`. | Mirrors the hum repository layout the owner already maintains. |
| D2 | Allowed runtime dependencies: `github.com/urfave/cli/v3` (v3.11.0 or later), `gopkg.in/yaml.v3`, `golang.org/x/sys`, and the SQLite driver selected by TASK-85.4 (preferred: `modernc.org/sqlite`). Tests use the standard `testing` package plus `internal/testkit`; no assertion libraries. Any other dependency requires an amendment. | Dependency-light like hum; keeps releases reproducible and audits small. |
| D3 | Targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, built with `CGO_ENABLED=0` unless the TASK-85.4 amendment records that CGO is required. No Windows support. | Cross-compilation from one runner and static binaries. |
| D4 | One claim model. A claim owns an ordered set of 1 to 32 unique resources. A "singleton" is a claim with one resource. There is no separate bundle command family, table set, or code path. | Removes the duplicated singleton/bundle implementations that drifted in Python. |
| D5 | Identity fields: `agentId` (config default, section 5), `sessionId` (stable for an agent loop when supplied through `--session` or `WORKLEASE_SESSION_ID`; generated per acquire otherwise), `workKey` (defaults to the resources joined with `,`), `claimId` (generated). The Python `ownerId` is removed; the claim ID already identifies one worker attempt. | Fewer identifiers with distinct meanings. |
| D6 | Token: 32 bytes from `crypto/rand`, encoded as 64 lowercase hex characters, generated by the local CLI/MCP adapter and persisted privately before dispatch. The authority stores only `sha256(token)` and compares in constant time. Tokens never appear on argv or in command/MCP output; explicit sources are a handle, `--token-file`, or `--token-fd`. Generated identifiers are 32 lowercase hex characters. | Client-held credentials make lost-response retries recoverable without storing recoverable bearer secrets in the authority. |
| D7 | The session-scoped contextual handle (section 9) is the default selection. `--handle PATH` selects an explicit handle. Stateless acquire uses `--no-handle` with a caller-retained `--claim-id` and token file or descriptor. Selection modes are exclusive. | Independent agent loops must not accidentally adopt each other’s claims; every retry needs recoverable identity and credentials. |
| D8 | Persistence is SQLite at `<home>/worklease.db` with `journal_mode=WAL`, `synchronous=FULL`, `busy_timeout=10000`, `foreign_keys=ON`, and `BEGIN IMMEDIATE` for every write transaction. Fresh schema version 1 via `PRAGMA user_version`. No Python database import or detection. SQLite serializes authority transitions; there are no per-resource lock files. Separate handle locks protect client credential files only (section 9). | One durable, transactional authority; fewer moving parts than flock plus SQLite. |
| D9 | Every durable lifecycle transition appends at least one row to `events` inside the same transaction. `events.seq` (AUTOINCREMENT) is the only cursor for `events`, `history`, and `watch`. | One ordering key replaces the Python composite keyset cursor. |
| D10 | Machine output is `--json` with envelope `schemaVersion: 2` and an `error` object; exit-code families in section 6. Text output is for humans and is deterministic but not a compatibility surface. | Intentionally incompatible, simpler contract. |
| D11 | Configuration precedence: command flags, then `WORKLEASE_*` environment variables, then the YAML config file, then defaults (section 5). | Same shape as hum's typed config with an optional file. |
| D12 | The first Go release is tagged `v1.0.0`. Release archives are `worklease-<version>-<os>-<arch>.tar.gz` (`os` in `linux`,`macos`; `arch` in `x64`,`arm64`) containing `bin/worklease` and `share/man/man1/worklease.1`, plus `checksums.txt` (SHA-256). The archive layout matches the current mise `github:brettinternet/worklease` installation. | Existing mise installs keep working; the version signals the incompatible rewrite. |
| D13 | MCP is served by `worklease mcp` over stdio JSON-RPC using the hum server pattern; modern protocol `2026-07-28` plus legacy `2025-11-25` interoperability (section 12); do not inherit the Python-era version list. Tools in section 12. | Proven dependency-light transport. |
| D14 | Stored timestamps and expiry use the wall clock (`time.Now().UnixMicro()` stored as INTEGER microseconds). Waits, deadlines, and heartbeat scheduling use monotonic time. Clock-regression rule in section 7.6. | Conservative expiry with deterministic tests. |
| D15 | Testing: every package passes `go test` and `go test -race`; command behavior is tested black-box through `cli.NewRootCommand` in-process and, for release smoke, through the built binary. Tests never import Python or read Python state. | Unattended loops need executable, hermetic evidence. |
| D16 | Quality gate for Go implementation subtasks until TASK-85.18 is `mise run ci-go` (section 14); documentation-only TASK-85.1 uses Backlog integrity and the repository gates available before Go bootstrap. TASK-85.18 repoints `lint`, `format-check`, `test`, and `typecheck` at the Go implementation and removes the Python tasks. | The project CLAUDE.md gates stay valid through the transition. |

## 3. Repository layout and ownership

The table assigns initial responsibility. Later feature tasks may minimally extend an earlier package API or wiring when their acceptance criteria require it; record the shared change and serialize work on overlapping files. Do not create parallel implementations to avoid an ownership restriction.

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
| `internal/lease` | 85.7 (one resource), 85.8 (many resources), 85.9 (reconciliation), 85.12 (`Verify`) | Claim lifecycle service. |
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

Claim selection uses exactly one mode: `--handle PATH` (or `WORKLEASE_HANDLE`), `--lease REF` for a private MCP handle, complete explicit credentials (`--claim-id`, exactly one token source, and `--revision` for mutations), or the contextual handle chosen by current directory and optional `--session` / `WORKLEASE_SESSION_ID`. Mixing explicit credentials with a handle/ref fails `credential-source-conflict`; explicit credentials bypass contextual discovery entirely. Handle fields are never overridden individually. Read-only `verify` accepts explicit credentials without a revision and compares it only when supplied. Public `status --claim-id ID` or `status -r R` requires no token and never proves ownership; multiple resources report each resource’s matching claim or absence without assuming one shared claim. Missing lifecycle selection fails `claim-selection-missing` (64). Resource input for `acquire` and `key` is exactly one of repeated `-r/--resource`, the provider triple `-p/--provider -s/--source -i/--item`, or `--path PATH`; mixing modes fails `resource-input-conflict` (64).

| Command | Purpose | Key flags | Mutates | Owner |
| --- | --- | --- | --- | --- |
| `worklease version` | Print version, commit, build time, schema version. | | no | 85.2 |
| `worklease key` | Derive one resource and its capability metadata. | resource input, `--coordination-only` | no | 85.5 |
| `worklease policy list` / `policy describe NAME` | Show built-in policies. | `--full` | no | 85.5 |
| `worklease acquire` | Acquire a claim over 1 to 32 resources. | resource input, `--ttl`, `--wait D`, `--poll-interval D`, `--agent`, `--session`, `--work-key`, `--claim-id`, `--coordination-only`, `--handle PATH`, `--no-handle`, stateless token source | yes | 85.7 (85.8 lifts the one-resource limit; 85.10 adds handles; 85.5 adds provider input) |
| `worklease status` | Show the claim for the selected handle or resources. | `-r` repeated, claim selection, `--full` | no | 85.7, 85.11 polish |
| `worklease list` | List current claims. | `-r FILTER`, `--full` | no | 85.7, 85.11 polish |
| `worklease heartbeat` | Renew the claim. | `--ttl`, `--operation-id`, claim selection | yes | 85.7 |
| `worklease checkpoint` | Store recovery metadata and renew. | `--data JSON` or `--data-file PATH`, `--ttl`, `--operation-id`, claim selection | yes | 85.7 |
| `worklease release` | End the claim. | `--reason TEXT`, `--operation-id`, claim selection | yes | 85.7 |
| `worklease transfer` | Hand the claim to a successor identity. | `--to-agent`, `--to-session`, `--to-work-key`, required `--successor-handle PATH`, `--ttl`, `--operation-id`, claim selection | yes | 85.7 |
| `worklease verify` | Read-only ownership check. | `-r` repeated (expected resources), `--hook claude-code`, claim selection | no | 85.12 |
| `worklease exec -- ARGV...` | Run one guarded command. | `--max-duration D`, `--cwd DIR`, `--git-primary`, `--ttl`, `--operation-id`, claim selection | yes | 85.12 |
| `worklease replace-file` | Expected-hash atomic file replacement. | `--path`, `--expected-sha256`, `--content-file`, `--ttl`, `--operation-id`, claim selection | yes | 85.12 |
| `worklease op inspect` | Inspect one operation. | `--operation-id`, `-r` repeated or claim selection, `--full` | no | 85.9 |
| `worklease op reconcile` | Record the observed outcome of an unknown operation. | `--target-claim-id`, `--target-operation-id`, `--operation-id` (this reconciliation), `--outcome observed-success|observed-failure`, `--evidence JSON`, `--expected-request-sha256`, claim selection | yes | 85.9 |
| `worklease history` | Without a resource, the global lifecycle event feed; with one resource, its retained epochs. | optional `-r RESOURCE`, `--limit`, `--cursor`, `--full` | no | 85.9, 85.11 polish |
| `worklease events` | Global lifecycle event feed. | `--limit`, `--cursor`, `--full` | no | 85.9, 85.11 polish |
| `worklease watch` | Wait for a lifecycle change. | `--cursor C` or `-r` repeated with `--until free|change`, `--timeout D` | no | 85.13 |
| `worklease gc` | Preview or apply retention. | `--retention-days N` or `--cutoff RFC3339`, `--apply` | with `--apply` | 85.11 |
| `worklease doctor` | Read-only environment diagnostics. | | no | 85.14 |
| `worklease instructions loop|safety` | Print canonical agent instructions. | | no | 85.14 |
| `worklease setup mcp` | Preview or apply MCP client configuration. | `--client claude-code|cursor|generic`, `--scope project|user`, `--apply`, `--remove` | with `--apply` | 85.16 |
| `worklease setup guard` | Preview or apply a pre-mutation verification hook. | `--client claude-code|generic`, `--scope`, `--apply`, `--remove` | with `--apply` | 85.16 |
| `worklease setup instructions` | Print the AGENTS.md block. | | no | 85.16 |
| `worklease mcp` | Serve MCP over stdio. | | via tools | 85.15 |

All mutations accept the long option `--request-not-after` for explicit/stateless requests; normal handles retain it automatically. `--session`, `--handle`/`WORKLEASE_HANDLE`, and `--lease` apply wherever claim selection is supported.

Short options have exactly one meaning across the tree: `-j` json, `-H` home, `-r` resource, `-p` provider, `-s` source, `-i` item, `-a` agent, `-w` work-key, `-T` ttl, `-W` wait, `-o` operation-id, `-m` reason, `-M` max-duration, `-F` token-file, `-D` token-fd, `-R` revision, `-c` claim-id, `-C` coordination-only, `-f` full, `-h` help, `-v` version. Do not add other short options.

Help text: every command has a one-line `Usage`, a `Description` with at least one example, and flag usage strings that name the environment variable in square brackets where one applies (hum style: `state directory [$WORKLEASE_HOME]`).

## 5. Configuration

Resolution order for each key: flag, environment, YAML, default. Blank strings are treated as unset at every level.

| Key | Flag | Environment | YAML key | Default | Bounds |
| --- | --- | --- | --- | --- | --- |
| home | `--home` | `WORKLEASE_HOME` | `home` | `$XDG_STATE_HOME/worklease`, else `~/.local/state/worklease` | absolute after expansion |
| session ID | `--session` (claim selection and acquire) | `WORKLEASE_SESSION_ID` | | unset; the unscoped checkout slot is for one loop only | 1 to 128 bytes, no control characters; never a resource component |
| agent ID | `--agent` (acquire); `--to-agent` (transfer) | `WORKLEASE_AGENT_ID` | `agent_id` | current OS user name via `os/user`; if unavailable, error `agent-id-required` | 1 to 128 bytes, no control characters |
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
| 2 | ownership or contention | `already-claimed`, `stale-claim`, `invalid-token`, `stale-revision`, `claim-expired`, `wait-timeout`, `verify-failed`, `ownership-lost`, `handle-in-use`, `operation-in-progress` |
| 3 | ledger or idempotency | `operation-request-mismatch`, `unknown-outcome`, `expected-hash-mismatch`, `reconciliation-conflict`, `operation-ambiguous`, `operation-not-found`, `replay-expired` |
| 64 | invalid input or configuration | `invalid-argument`, `config-invalid`, `config-missing`, `claim-selection-missing`, `resource-input-conflict`, `invalid-resource`, `unknown-policy`, `invalid-path`, `handle-unsafe`, `handle-malformed`, `credential-unsafe`, `credential-malformed`, `credential-source-conflict`, `agent-id-required`, `unsupported-coordination-replace`, `cursor-invalid`, `setup-config-malformed` |
| 75 | authority or storage failure | `home-unsafe`, `storage-failure`, `schema-unsupported`, `schema-corrupt`, `handle-write-failed`, `authority-mismatch`, `clock-regression` |
| 124 | guarded child exceeded `--max-duration` | `child-timeout` |
| 130 | interrupted by SIGINT or SIGTERM before any durable effect | `interrupted` |
| child status | `exec` propagates the child's exit status once the child started (signals map to 128 plus the signal number) | |

Reasons are stable identifiers; messages may change. `internal/reason` holds the complete vocabulary as constants with their exit codes, and a test asserts every emitted reason is registered.

### 6.2 Committed or uncertain results

Every error after dispatch includes non-secret `claimId`, `operationId` when applicable, `commitState` (`not-committed`, `committed`, or `unknown`), and a recovery hint when known. A committed mutation whose handle update fails exits 75 with `handle-write-failed`, `ok:false`, `commitState:committed`, a redacted receipt, and the retained pending-handle path. Retry the exact pending request; never generate a fresh operation or grant. Tokens are never printed as recovery output. A crash or ambiguous storage error leaves the private pending record intact. No success envelope conceals an error exit.

For `exec`, a started child’s exit status applies only after its completion receipt is durably recorded; ownership/storage errors take precedence when completion is unknown. A known child failure is a completed operation, not `unknown-outcome`. Pre-spawn failures with proven no effect also receive completed failure receipts; only uncertain effects remain `started`.

### 6.3 Redaction

Tokens and token hashes never appear in CLI text, JSON, MCP output, logs, events, or diagnostics. They are not returned from the authority. Private handles contain the bearer; epochs retain hashes solely to authenticate replay after release or transfer. Public status, list, history, events, watch, and doctor never read or return checkpoint bodies, child output, reconciliation evidence, argv, or raw requests/receipts; `--full` expands non-secret identifiers and metadata only. The invoking checkpoint/exec/replace command may return its own payload or captured output, and credential-authenticated `op inspect --full` may return that epoch’s receipt, checkpoint, and evidence. Inspection of historical private content authenticates against the epoch hash without renewing ownership. Error text never echoes rejected credential content. MCP tools return redacted projections even when underlying receipts contain private content, and checkpoint input rejects the active bearer value or credential-like fields. The authenticated acquire recovery exception is limited to the preceding checkpoint (section 7.8); MCP returns its presence metadata only.

## 7. Domain model and invariants

### 7.1 Resources

An opaque UTF-8 string, 1 to 1024 bytes, without NUL, CR, or LF, not starting or ending with whitespace. The authority never interprets it. Everyone contending for the same unit must present identical bytes; policies (section 7.13) exist to make that deterministic.

### 7.2 Claims and epochs

A claim has `claimId`, `resources` (ordered, unique, 1 to 32), `agentId`, `sessionId`, `workKey`, `guarantee` (`local-coordination`), `authorityId`, `localReplaceAllowed`, `revision`, `acquiredAt`, `ttl`, `heartbeatAt`, `expiresAt`, and optional `checkpoint`. A resource is `free` when no claim row references it, `active` when its claim has `now < expiresAt`, and `expired` otherwise. Exactly one claim may reference a resource at a time (primary key on `claim_resources.resource`). Expiry is lazy: reading does not mutate. The next `acquire` touching an expired resource replaces the old claim inside the same transaction, finalizing its epoch with reason `expired` effective at the stored `expiresAt` and recorded at the replacement time (event `expired-replaced`). Every claim is also an epoch row that outlives the claim and records its end.

### 7.3 Revision and credentials

`revision` starts at 1 on acquire and increments by one per committed claim-state change, including the start, internal renewals, and completion of a guarded operation. Transfer starts a successor at 1; revision is never a cross-epoch fencing number. Ordinary mutations check current claim ID, constant-time token hash equality, expiry, then expected revision, in that order, and fail `stale-claim`, `invalid-token`, `claim-expired`, or `stale-revision` without writes. A successful mutation reports the resulting revision. Replay authenticates separately as specified below and never authorizes new work against an ended epoch.

### 7.4 Operations and idempotency

Every request has a stable client-generated operation ID; acquire uses `claimId` as its operation ID. IDs are unique per claim. Before dispatch, the adapter durably retains the exact normalized request, generated defaults, credentials, and identifiers (section 9). The authority stores a request hash and a token-free receipt. Replay requires the original epoch credential even after release/transfer; `epochs.token_hash` retains that verification material for the retention window.

Hash SHA-256 over canonical JSON of all semantic request fields: operation kind, authority ID, claim ID, resources, identities, TTL, checkpoint/reconciliation data, and for exec argv, resolved cwd, and maxDuration; for replacement resolved target, expected hash, and content digest. Sort object keys recursively, preserve array order, reject duplicate JSON keys and non-finite numbers, and freeze normalized values before dispatch. Exclude only operation ID, expected revision, credentials, and local delivery options such as handle paths and output format. All changed intent, including maxDuration, conflicts. Use cross-language golden fixtures; shell environment and external state are not reproducible request inputs and replay never re-executes them.

An authenticated equal-hash replay returns the original receipt with `idempotent:true` and makes no change; a changed hash fails `operation-request-mismatch`. A `started` operation returns `unknown-outcome`, never a second external execution. Replaying acquire cannot resurrect a released/expired epoch or adopt a successor. Replay reports the recorded result and current epoch state separately; handle recovery reads the current authenticated revision and never rewinds it. An absent retained receipt is `operation-not-found` or `replay-expired`, never permission to execute an expired intent again. Within an unexpired request window, a linearizable absence read from the same healthy local authority proves the intent did not commit because retention cannot remove it yet; the adapter may dispatch the identical saved request. Adapters must inspect an uncertain request before deciding whether a not-yet-recorded request can safely be retried. Stateless mutation callers supply `--request-not-after RFC3339` and retain normalized defaults before dispatch; handle-backed calls generate and save it. Every normalized request includes `requestNotAfter`, chosen before dispatch from authority time with a maximum 24 h retry window and included in the hash. The authority rejects an expired request with `replay-expired`, including acquire. GC retains receipts and epoch authentication through that deadline; a retry never refreshes it. The deadline limits first dispatch and replay, not completion of an already-started guard under still-valid ownership. Reconciliation has a fresh deadline of its own and can resolve an older target after that target’s retry window. This bounds replay without permanent operation tombstones.

### 7.5 Guarded intent and recovery

`exec` and `replace-file` commit one `started` operation before effects, then commit `completed` afterwards. There is at most one started operation per claim (enforced by a partial unique index). While one is started, reject another guarded operation, checkpoint, transfer, or release with `operation-in-progress`; only the running guard’s internal renewals/completion and explicit reconciliation may change that claim. External heartbeat must not race the guard’s private revision. A recorded started row does not prove a process is still alive, and time passing never proves its effects ceased.

Acquire may replace an expired claim and reports retained unresolved operations touching any acquired resource. If a requested resource intersects a started predecessor operation, require the request to include every resource of that operation (including the transitive union of overlapping unresolved operations); otherwise fail `operation-in-progress` with the required resource set before any write. If that union exceeds 32, report the bounded recovery limitation rather than split it. This preserves one successor capable of reconciliation instead of stranding separate partial owners. The new owner can inspect and recover, but `verify` and new guarded effects fail `unknown-outcome-pending` until those operations are reconciled. This check follows `epoch_resources` across prior claims and survives replacement and GC.

Reconciliation has its own `operationId` plus explicit `targetClaimId` and `targetOperationId`. It requires current credentials, expected request hash, an observed outcome, and bounded caller evidence establishing both the outcome and cessation of the old executor. The current claim must cover every resource of the target operation; the target must be started and either on that claim or an ended predecessor. It updates the target ledger, records the resolving claim/agent/session, and advances only the resolver’s current revision in one transaction. It grants no old owner authority and cannot revive an expired claim. Identical replay is idempotent; changed evidence conflicts. If the outcome or cessation cannot be established, leave the operation unresolved and stop guarded work. Evidence is a caller attestation, not proof supplied by Worklease.

Resource-addressed `op inspect` searches retained epochs; duplicate operation IDs across epochs fail `operation-ambiguous` listing candidate claim IDs. Explicit claim selection disambiguates. Metadata inspection includes the non-secret requestSha256 needed for reconciliation and is public; private `--full` inspection requires that target epoch’s credential. To recover an expired pending claim, acquire its complete resource set into a new explicit handle, then reconcile the predecessor from that new current claim; do not overwrite the old pending handle to begin recovery.

### 7.6 Clocks

Authority wall time decides expiry. Waits, scheduling, and child deadlines use monotonic time. Persist an authority-wide `last_observed_at` on writes. For a backward step of at most 1 s use `max(now, last_observed_at)`; for a larger regression, fail mutations and ownership verification with `clock-regression` (75), terminate a running guard, and wait for the clock to catch up. Read-only diagnostics report the condition without rewriting any lease. Forward jumps expire normally. Never re-anchor an expired lease into a new lifetime on behalf of a contender. A reboot or clock repair does not authorize adoption of an old epoch.

### 7.7 Transfer

Requires current credentials, no pending guarded intent, and a distinct `--successor-handle PATH`. The adapter generates and durably saves the successor claim ID, credential, identities, and exact transfer request before dispatch. The authority creates the successor at revision 1 with the same resources and local execution capability, carries the checkpoint, and ends the predecessor with `successorClaimId` in one transaction, with no free interval. The predecessor handle is removed only after the successor handle is durable; a pending successor grant is recoverable with the retained request. Transfer replay authenticates the predecessor epoch and also verifies that the proposed successor credential matches the recorded successor hash; changed successor credentials conflict. It returns no bearer token. An active successor handle is never overwritten.

### 7.8 Release and checkpoint

Release requires current credentials and no pending guarded intent, accepts an optional audit reason (default `released`), removes the current claim, ends its epoch, retains the last checkpoint, and appends `released`. Preserve the supplied reason in the event and receipt independently of the structural end reason. Checkpoint accepts canonical JSON up to 8 KiB and renews TTL. New acquisition returns recovery metadata about the immediately preceding epoch per resource, including whether a checkpoint is retained; only the newly authenticated owner may retrieve that predecessor checkpoint through its acquire receipt. This is local recovery context, never evidence of provider progress.

### 7.9 Contention and waiting

Acquire fails `already-claimed` when any requested resource is held by an active claim, reporting the holder's non-secret metadata (`claimId`, `agentId`, `workKey`, `expiresAt`) for the first contended resource in caller order. With `--wait D` it retries at `poll_interval` with plus or minus 20 percent jitter until the monotonic deadline, then fails `wait-timeout` carrying the last holder metadata. Contention on a resource held as part of a larger claim reports that claim.

### 7.10 Many resources

A claim over N resources acquires all or none in one transaction. Resources are inserted in caller order; ordering is not needed for deadlock avoidance because SQLite serializes writers. Duplicate resources fail `invalid-resource`. All lifecycle commands act on the whole claim; a resource cannot be released or transferred individually.

### 7.11 Guarantee

Every claim reports `guarantee:local-coordination`, `authorityId`, `coordinationScope:same-host`, and `providerMutationFenced:false`. Identity policies do not confer execution guarantees. `exec` is supervised coordination: a stopped supervisor, escaped descendant, or in-flight provider request can outlive ownership. Renewal and process-group termination reduce that risk but do not fence arbitrary effects.

Only `replace-file` can report `mutationProtection:local-serialized-replace`: its expected-hash check and atomic replacement share the local authority write transaction, excluding other cooperating replacements and ownership transitions during that boundary. It does not fence editors, other authorities, or remote providers. A claim has `localReplaceAllowed` (true for direct resource input and local policies; false for `--coordination-only` and coordination policies). This is permission to request that specific guard, not a claim-wide fencing promise.

### 7.12 Events

Kinds: `acquired`, `renewed`, `checkpointed`, `exec-started`, `exec-completed`, `replace-started`, `replace-completed`, `reconciled`, `transferred`, `released`, `expired-replaced`, `expired-retired`, `gc-applied`. Each row carries `at`, `kind`, `claimId`, `resources` (JSON array), `operationId` (nullable), `revision` (nullable), `agentId`, and a non-secret `detail` JSON object (for example exit status, reason text, counts). Events never contain tokens, hashes, checkpoints, output, or evidence.

### 7.13 Resource policies

Built-in policies and exact derivations. Percent-encode each interpolated identity component’s UTF-8 bytes using RFC 3986 unreserved characters only; in particular encode `%`, `:`, `#`, and `/` so delimiter-containing inputs cannot collide. Use conformance vectors for colons in source paths and item IDs. The table shows decoded components for readability. `<common>` is the absolute, symlink-resolved `git rev-parse --git-common-dir` of the repository containing the source path (so linked worktrees agree); `<locator>` is the source path relative to `git rev-parse --show-toplevel` in POSIX form; outside Git, `<common>` and `<locator>` are both the absolute resolved source path. Git is probed with `GIT_*` environment variables removed.

| Policy | Input | Resource | Capability | Scope |
| --- | --- | --- | --- | --- |
| `backlog-md` | source = backlog directory, item = task ID | `backlog-md:<common>:<locator>:<item>` | `item-claim` | `item` |
| `markdown` | source = Markdown file or directory; item is accepted but not part of the identity | `markdown:<common>:<locator>:__source__` | `source-claim` | `source` |
| `github` | source = `owner/repo` (lowercased, `.git` stripped), item = number | `github:<source>#<item>` | `local-coordination` | `item` |
| `linear` | source = team or project key, item = issue key | `coordination:linear:<sha256 of canonical JSON {item,provider,source}>` | `local-coordination` | `item` |
| `generic` | any source and item | `coordination:generic:<sha256 of canonical JSON {item,provider,source}>` | `local-coordination` | `item` |
| `path` | `--path P` inside a Git repository | `path:<common>:<repo-relative POSIX path>` | `item-claim` | `path` |

`path` resolves symlinks through the nearest existing ancestor (including a not-yet-created leaf), maps aliases to one canonical repository-relative identity, and rejects a resolved target outside the repository. It rejects paths outside the repository, `..` traversal after normalization, and non-Git directories (`invalid-path` with a hint), and claims exact paths only: no parent, child, or glob overlap is implied. Every key result reports `provider`, `capability`, `scope`, `localReplaceAllowed`, `providerFencing: false`, `identityScope` (`host-local` for filesystem policies, `portable` for provider keys), and `resource`. Policies are registered in a static Go map; unknown names fail `unknown-policy` listing the available names. No dynamic plugins, entry points, network, or credentials.

## 8. Storage

Home layout, created only by write operations with owner-only creation modes:

```
<home>/                          0700  owned by the effective user
<home>/worklease.db              0600  plus -wal and -shm
<home>/handles/                  0700
<home>/handles/ctx-<sha256>.json 0600  contextual handles
<home>/handles/mcp-<ref>.json    0600  MCP handles
```

Read-only commands never create a home, database, or handles and never chmod anything; a missing home is empty authority state, while verify reports a missing claim. For an existing WAL database, select a driver-supported read-only mode that still sees committed WAL data rather than ignoring WAL. Limit (TASK-88): SQLite itself creates absent `-wal`/`-shm` sidecars for a read-only WAL open when the directory is writable and modernc exposes no way to refuse; they inherit the database's private 0600 mode, the main database is not modified, and a driver test documents this rather than claiming sidecar absence. Write opens perform safety checks: `<home>` and `<home>/handles` must be directories owned by the effective UID with no group or other permission bits (fix by `chmod 0700` if owned; fail `home-unsafe` if foreign-owned or a symlink); the database path is checked with `lstat` and must be a regular file or absent; all SQLite main/WAL/SHM files and handle/lock files must be owner-only, regular, not hard-linked, and opened without following symlinks where the opener is under our control. Handle, lock, and replacement files are opened by our code through pinned directory descriptors with no-follow semantics. The SQLite driver opens the database by path, so TASK-85.4 must document exactly what the selected driver can enforce (no-follow, mode, or neither) and TASK-85.6 must close the remaining gap as far as possible: verify the opened database's device and inode against the `lstat` result after opening, keep the home directory descriptor pinned, reject unsafe ancestors, and cover the residual pre-open race with a test that documents it rather than a claim that it is closed. Set private modes at creation without a process-global umask change in a concurrent MCP server. Symlinked or foreign files fail `home-unsafe` (75).

TASK-85.4 selected `modernc.org/sqlite v1.58.0` as the pure-Go driver. Write connections use `_txlock=immediate`; read connections use `mode=ro` with `_txlock=deferred`; both use one pooled connection and the D8 pragmas. Executable tests prove WAL-visible read-only observation without modifying the main database; SQLite may update shared-memory coordination words and recreates absent sidecars with the database's private mode, cross-process writer serialization, bounded cancellation, rollback, AUTOINCREMENT non-reuse, and killed-process durability. The driver performs private no-follow pre-creation and pre-open `lstat` validation for the main/WAL/SHM paths, but modernc opens SQLite files by pathname and cannot accept a pinned `O_NOFOLLOW` descriptor. Therefore the final lstat-to-driver-open race remains: TASK-85.6 must keep the trusted home directory descriptor pinned and compare the opened database device/inode. A commit error is classified only by an independent bounded fresh read as committed, not-committed, or unknown; a post-commit safety-check failure is explicitly committed and must not be retried.

Schema version 1 (`PRAGMA user_version = 1`), created inside one `BEGIN IMMEDIATE` transaction:

```sql
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
-- keys: created_at, authority_id (random 32-hex, immutable), last_observed_at,
-- last_event_seq (never reduced), pruned_through_seq (both integer text, default 0)

CREATE TABLE claims (
  claim_id      TEXT PRIMARY KEY,
  token_hash    TEXT NOT NULL,
  revision      INTEGER NOT NULL,
  agent_id      TEXT NOT NULL,
  session_id    TEXT NOT NULL,
  work_key      TEXT NOT NULL,
  guarantee     TEXT NOT NULL CHECK (guarantee = 'local-coordination'),
  local_replace_allowed INTEGER NOT NULL CHECK (local_replace_allowed IN (0,1)),
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
  token_hash          TEXT NOT NULL,     -- replay authentication, never returned
  agent_id            TEXT NOT NULL,
  session_id          TEXT NOT NULL,
  work_key            TEXT NOT NULL,
  guarantee           TEXT NOT NULL,
  local_replace_allowed INTEGER NOT NULL,
  acquired_at         INTEGER NOT NULL,
  acquired_seq        INTEGER NOT NULL,
  ended_at            INTEGER,           -- effective end time
  ended_seq           INTEGER,           -- event seq that recorded the end
  ended_recorded_at    INTEGER,           -- retention starts at observation, not expiry
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
  request_not_after INTEGER NOT NULL,   -- bounded exact-replay deadline
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
CREATE UNIQUE INDEX one_started_per_claim ON operations(claim_id) WHERE state = 'started';

CREATE TABLE reconciliations (
  claim_id                 TEXT NOT NULL,
  operation_id             TEXT NOT NULL,
  outcome                  TEXT NOT NULL CHECK (outcome IN ('observed-success','observed-failure')),
  evidence                 TEXT NOT NULL,   -- JSON
  request_hash             TEXT NOT NULL,
  reconcile_operation_id   TEXT NOT NULL,
  resolver_claim_id        TEXT NOT NULL,
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

Transaction API: `store.Write(ctx, fn)` opens `BEGIN IMMEDIATE`, runs `fn(tx)`, commits or rolls back; `store.Read(ctx, fn)` opens a deferred transaction. `tx.AppendEvent(Event) (seq, error)` is the only way to write events. Context cancellation before commit rolls back; an error during commit must be classified by durable read-back as committed, not-committed, or unknown, never assumed to have rolled back. The store uses `SetMaxOpenConns(1)` for its write connection.

## 9. Handles and credentials

Context root: use resolved `git rev-parse --show-toplevel` with all `GIT_*` variables removed, otherwise resolved cwd. Linked worktrees have distinct contexts; subdirectories share their root. Context ID is SHA-256 of canonical JSON `[root, sessionSelector]`, where the selector is explicit `--session`, `WORKLEASE_SESSION_ID`, or the empty string. A generated acquire session ID is metadata; it does not silently change the context selector. Independent loops in one checkout must supply distinct stable session selectors or explicit handles. The unscoped checkout slot is a convenience for one loop, and OS login agent ID is a label, not isolation or authentication.

Handle path: `<home>/handles/ctx-<contextId>.json`; MCP uses `mcp-<ref>.json`. Handle JSON has schemaVersion 1, authorityId, claimId, token, revision, resources, expiresAt, agentId, sessionId, localReplaceAllowed, state (`pending` or `ready`), and optional pendingRequest plus recoveryRequest. Pending grants may omit revision/expiry until committed. A pending request retains normalized inputs, generated defaults/IDs, request hash and requestNotAfter, and for transfer its successor credential. Limit the entire handle to 64 KiB; reject an oversized pending request before dispatch. A recoveryRequest is the separately durable exact reconciliation request (its operation ID, target IDs, evidence, hash and deadline); it coexists with the original pending external request. Recover this request first after a crash. On a definitive no-commit reconciliation failure (including a wrong expected request hash), clear `recoveryRequest` only and preserve the original `pendingRequest`, allowing a corrected reconciliation with a fresh operation ID. On an uncertain reconciliation outcome keep both unchanged for exact recovery. Only confirmed reconciliation atomically synchronizes the current handle and clears both records. The two bounded slots share the 64 KiB limit. Large file contents and child output are never stored in it. Ready and pending MCP handles also persist `holdUntil` and the automatic-renewal owner metadata; initial acquire expiry and every MCP renewal are capped at holdUntil, including explicit calls after restart.

Use a stable sibling `<handle>.lock` with POSIX advisory locking across processes. Never unlink that lock during ordinary lifecycle cleanup (otherwise two lock inodes can coexist). Hold it from selection/read through authority dispatch and durable handle update/removal; reload after acquiring it. Lock waits are bounded and cancellable; contention returns `handle-in-use` without a mutation. For transfer, acquire predecessor and successor handle locks in sorted canonical-path order; reject the same destination. Locks protect local credential files only and cannot fence resources or a future remote authority.

Files are regular, owner-only 0600 or stricter, owned by the effective UID, not hard-linked or symlinked, in trusted directories. Reject unsafe paths with `handle-unsafe`, oversized/invalid JSON or versions with `handle-malformed`, and name only the path in errors. Writes use a same-directory temporary file, fsync, rename, then directory fsync. Do not overwrite an active or pending grant. A stale ready handle may be replaced after checking the same authority; a pending handle must first be recovered.

Before acquire, create a durable pending handle containing the client-generated claim ID/token and exact request. Before a lifecycle mutation, persist its pending request while preserving the usable credential. Then dispatch; on success atomically replace it with the current ready handle. A crash at any boundary leaves enough state to inspect/retry that exact request. A mutating invocation with a pending handle first inspects/retries that exact request, rejects changed intent, and never invents a new operation ID. `op reconcile` is the explicit exception: it may select credentials from a pending handle to resolve that started target under section 7.5; it must not replay the external effect. For ordinary mutations without a recoveryRequest, definitive no-commit validation/contention failure clears only the pending request (and removes a failed pending grant), while an uncertain result remains pending. After requestNotAfter, report replay-expired and require inspection/reconciliation or verified stale-grant disposal; never resubmit with a refreshed deadline. A completed release removes the handle only after read-back; transfer preserves predecessor recovery until the successor handle is durable. A completed replay may synchronize a stale handle using authenticated current state; a replay never rewinds or recreates an ended claim.

Every use compares handle authorityId with the opened authority; mismatch fails `authority-mismatch` before mutation. Moving a handle to another home does not retarget it. `--no-handle` acquire requires caller-retained claimId and exactly one token source; stateless callers retain all normalized defaults, operation IDs and requestNotAfter for retries themselves. No mode prints tokens.

Credential file/descriptor reads are bounded to 4096 bytes; a file is regular, owner-only, effective-user-owned, not hard-linked or symlinked. Descriptor input duplicates with CLOEXEC. Accept exactly 64 lowercase hex characters with one optional trailing newline; otherwise `credential-malformed`. Unsafe files fail `credential-unsafe`; conflicting selectors or sources fail `credential-source-conflict`.

## 10. Guarded operations

### 10.1 exec

Runs exactly the argv after `--` without a shell, in a new process group (`Setpgid`), stdin from `/dev/null`, stdout and stderr captured up to 1 MiB each (the receipt records `stdoutBytes`, `stderrBytes`, `stdoutTruncated`, `stderrTruncated`, and the captured text with invalid UTF-8 replaced). Working directory: caller directory by default, `--cwd DIR`, or `--git-primary` (the primary worktree of the repository containing the caller directory, resolved through `git rev-parse --git-common-dir`; fails `invalid-path` when the caller is not in a repository or the primary worktree is missing). When `--cwd` or `--git-primary` is used, `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_COMMON_DIR`, `GIT_PREFIX`, and `GIT_OBJECT_DIRECTORY` are removed from the child environment. The child receives `WORKLEASE_CLAIM_ID` and `WORKLEASE_OPERATION_ID`; never the token.

Sequence: validate input and credentials; write `started` (event `exec-started`) and renew the claim; spawn; while the child runs, heartbeat at `ttl/2`; if renewal cannot be confirmed before the last known lease deadline, or a heartbeat fails with an ownership/clock reason, send SIGTERM to the process group, wait 2 s, SIGKILL the group, and fail `ownership-lost` (2); the operation stays `started`, visible across successor epochs until reconciliation; when `--max-duration` elapses (monotonic, covering pipe draining), terminate the same way, stop draining, and exit 124 with `timedOut: true`; otherwise record `completed` with the exit status and exit with the child's status. Replay returns the stored receipt without spawning. Storage failure after spawn terminates the group and leaves the operation `started`. The supervisor does not hold a database transaction while a child runs. A second guard on this claim is rejected even with a fresh revision. A killed supervisor or escaped descendant is a documented limit, not a fencing success. Hold the handle lock during exec; internal renewals use service credentials and do not reacquire that lock.

### 10.2 replace-file

Inputs: `--path`, `--expected-sha256` (64 hex), `--content-file`. Requires `localReplaceAllowed`, otherwise `unsupported-coordination-replace` (64). It replaces an existing regular file; creation/deletion is outside this command. Reject symlinked or hard-linked targets/content and unsafe parent traversal. Derive the canonical `path` resource and require it in the claim. A task resource alone does not authorize replacement of arbitrary files; acquire the task and path resources together. Replacement content is limited to 16 MiB; reject a larger file with `invalid-argument` before persisting the operation intent.

Before the completing write transaction, safely open, bound, read, and digest the content into an immutable in-process snapshot, then bind the request to that digest. Persist started intent. In the completing local write transaction recheck credentials and unresolved intents, compare target identity and expected hash, re-verify the prepared content digest, write a temporary file with the target mode, fsync, rename relative to the pinned target directory, fsync the directory, and record completion. Recheck the deadline before rename; the transaction serializes cooperating authority transitions throughout the replacement. A mismatch or pre-rename error with proven no effect records a completed failure, freeing the started slot. A rename/commit uncertainty remains started.

Do not automatically complete a started request merely because the target has the new hash: another writer could have produced those bytes, and a paused original writer may still act. Explicit reconciliation must establish outcome and cessation. Completed replay returns the original receipt without reading changed content or renaming again. The receipt states `mutationProtection:local-serialized-replace` and `providerMutationFenced:false`.

### 10.3 verify

Read-only. Select and read credentials using a shared lock on an existing handle lock file when present, without creating a lock file, validate authority identity, current claim ID, token, expiry/clock, optional expected revision, expected resource membership, then unresolved operations touching any member across retained epochs. Success reports claim ID, resources, revision, expiresAt and expiresIn; it grants no authority beyond that instant. Ordinary ownership failures exit 2 as `verify-failed` with `details.cause` such as missing-handle, stale-claim, invalid-token, claim-expired, stale-revision, resource-mismatch, or unknown-outcome-pending. Invalid inputs and authority/storage failures retain their 64/75 families; hooks map every failure to exit 2. A handle with a pending request is not ready to authorize a native edit.

`--hook claude-code` parses bounded PreToolUse JSON from stdin, uses its cwd (fallback process cwd), and resolves the selected session/explicit handle or `--lease` reference. It has two coverage modes. The default, `--coverage claim`, passes when that handle holds a current, verified claim in this authority with no pending request; it does not inspect which file is being edited, so a task-scoped claim authorizes the agent's edits for that task (the TASK-80 intent). `--coverage path` additionally derives the canonical `path` resource for each target in Edit/Write/MultiEdit/NotebookEdit and fails resource-mismatch unless every target is a member of the claim; native creation may use the non-existing-leaf path derivation. Help and docs state that claim coverage proves only that the agent holds a claim, not that the claim covers the file. Malformed or unsupported input fails closed with hook-input-invalid in both modes. Success is silent exit 0; failure is a one-line stderr block reason and exit 2.

Do not parse Bash shell text or allow commands by first word. V1’s generated guard matches native file-edit tools only; shell/provider actions require the normal cooperation workflow. A command starting `worklease` or `backlog` can still contain `;`, substitutions, or a mutating provider action. This is a cooperative check, with an unavoidable check-to-edit race, not a fence. MCP references do not automatically become contextual handles: clients using MCP and native hooks must explicitly bind the returned lease reference (or private handle path) to the hook invocation.

## 11. Events, history, watch, and garbage collection

Cursors are opaque, versioned base64url JSON containing authorityId, feed (`events` or `history`), exact filter, and a decimal-string sequence. Structural validation precedes opening storage; authority/feed/filter mismatches fail cursor-invalid, never silently restart. Empty feeds preserve the durable `meta.last_event_seq`, not MAX of retained rows. Sequence values are strings in public JSON to avoid JavaScript precision loss.

`events` returns rows after the cursor ascending, up to limit (default 50, maximum 1000). Without a cursor it returns the latest limit rows ascending. nextCursor is the last returned position, or the supplied position on an empty continuation. A cursor below pruned_through_seq returns gap:true, no rows, and a bound reset cursor; consumers explicitly resnapshot before resuming. `history` without a resource is exactly this bounded global event feed: text uses the event view and JSON uses the canonical events envelope with `operation` equal to `events`. `history --resource RESOURCE` instead uses the same default and maximum limits with history cursors bound to that exact resource and acquired_seq positions. Without a cursor it returns the latest retained epochs ascending; with a cursor it returns later epochs ascending. The resource projection includes public operation summaries, coverage, and statuses open, expired-open, complete. It is retained diagnostic state, not a snapshot feed of subsequent changes to older epochs; use events for changes. Public --full never exposes private payloads.

`watch --cursor C` returns the first matching event, filtered by resource intersection if given. Capture the initial cursor and current resource state in one read snapshot so a concurrent release is not missed. Poll adaptively at 50–500 ms without retaining a transaction between polls; advance the inspected sequence only through rows actually scanned. A timeout returns a cursor through the last scanned position, never an unexamined later MAX that might hide a matching event.

`watch -r ... --until free` means every requested resource is acquirable by expiry/ownership rules (absent or expired). Recheck authority time and state on every poll and at the nearest known expiry, even when no event is appended; expiry is lazy. Transfer or immediate reacquisition keeps waiting. Report unresolved predecessor operations separately so free does not imply safe to mutate. `--until change` also detects an active-to-expired change without inventing a durable event. Timeouts default to 30s, maximum 1h (MCP 60s); gaps return immediately, cancellation is bounded, and watches never mutate or leak goroutines.

`gc` previews by default. Use a strict cutoff at or before now. Active claims and any epoch with a started operation are protected. First retire sufficiently old expired claims, recording ended_at at expiry but ended_recorded_at at collection time. Retain ended epochs and all their operations/authentication for the later of the recorded end/reconciliation retention window and requestNotAfter deadlines. Newly retired epochs therefore cannot be collected in that same run. Delete epoch/history/receipt records only as an eligible contiguous prefix in acquired_seq order, stopping at the first retained/protected epoch. This aligns history gaps with event-prefix retention: an old protected epoch also pins newer history, intentionally trading disk space for a simple honest continuation contract.

Prune events only as one contiguous prefix older than cutoff, below every retained epoch’s acquired_seq, and outside retained replay/recovery needs. Never delete an ended claim’s scattered events out of the middle of the global feed. Raise pruned_through_seq only to the actual removed prefix end; preserve last_event_seq even if no event remains. This can retain extra history behind a long-lived claim, an explicit v1 space tradeoff. Apply re-derives eligibility inside one write transaction, updates all projections atomically, appends gc-applied, and rolls back fully on pre-commit failure. Clock regression blocks apply; it never prunes using uncertain time.

## 12. MCP server

`worklease mcp` reads newline-delimited JSON-RPC 2.0 from stdin and writes responses to stdout; stderr carries redacted logs. Protocol baseline: [MCP 2026-07-28 versioning](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning) and [stdio](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio), with a [2025-11-25 legacy lifecycle](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle) for initialization-based clients. Modern requests carry per-request version metadata and support server/discover; unsupported modern versions return the specified error and supported-version list. Legacy initialize negotiates a supported legacy version and waits for initialized; do not reuse modern error/handshake semantics for legacy sessions. Tests cover both before claiming client interoperability. Implementation pattern for transport primitives only: hum `internal/mcp/server.go` (request registry with duplicate-ID rejection, per-request cancellation via `notifications/cancelled`, serialized response writer, 4 MiB message limit, EOF and parent-context shutdown). Concurrency limit: 8 in-flight tool calls. Shutdown waits up to 5 s for in-flight calls.

Tools (`tools/list` returns these names, descriptions, and JSON Schemas; domain failures are tool results with `isError: true` and structured `{ok:false,error:{reason,exitCode,message,details?}}`; preserve the applicable section 6 domain fields, including commit state and recovery/holder metadata, rather than discarding them in the transport adapter. Protocol failures are JSON-RPC errors):

| Tool | Input | Output |
| --- | --- | --- |
| `key` | `{provider, source, item}` or `{path}`, optional `coordinationOnly` | key result (section 7.13) |
| `acquire` | `{resources[]}` or provider triple or `{path}`; optional `ttl`, `wait` (at most 60 s), `workKey`, `agentId`, `sessionId`, `coordinationOnly`, `autoHeartbeat` (default true), `maxHold` (default 4h) | `{lease, authorityId, handlePath, claim (no token), autoHeartbeat, holdUntil}` |
| `status` | `{lease}` or `{resources[]}` | claim without token, `unknownOperations` |
| `list` | optional `{resource}` | claims without tokens |
| `heartbeat` | `{lease, ttl?}` | receipt |
| `checkpoint` | `{lease, data, ttl?}` | receipt |
| `verify` | `{lease, resources?}` | verify result |
| `watch` | `{cursor}` or `{resources[], until}`, `timeout` (at most 60 s) | watch result |
| `events` | `{cursor?, limit?}` | events page |
| `release` | `{lease, reason?}` | receipt |
| `instructions` | `{topic: loop|safety}` | text lines |

`lease` is a 32-hex random reference whose handle lives at `<home>/handles/mcp-<ref>.json` (0600). The client never sees tokens; revisions are non-secret diagnostics and may be returned, but callers do not need to manage them. Automatic heartbeat runs per lease before `ttl/2`, uses the same cross-process handle lock and pending-request recovery as the CLI, and stops at a persisted absolute `holdUntil` (acquire time + maxHold, never extended on restart or ordinary calls), on expiry or ownership failure, on `release`, and on shutdown, and never releases the claim itself. Results report `autoHeartbeat` as `active`, `stopped`, or `disabled`. Bound maxHold to 1 minute–24 hours, persist it/holdUntil in the MCP handle, and clamp both the initial acquire expiry and every MCP renewal expiry to holdUntil; reject explicit or automatic MCP renewal at or after that deadline. Direct CLI takeover under the private handle is an explicit user-owned lifecycle action outside the MCP automatic-hold budget; document it as such. Restart preserves lease references but does not silently resume renewal or reset holdUntil. A server schedules only leases it acquired in this process. A second server using that reference may make explicit mutations under the handle lock but does not become another automatic renewer. Verification must not race a renewal into false stale-revision errors. Stop automatic renewal on uncertain dispatch/handle persistence until recovery. Explicit calls queued behind the 8-call cap are cancellable and do not block the input reader from processing cancellation or EOF. On stdin EOF the server stops heartbeats and exits without releasing anything. The server uses the same config resolution as the CLI (section 5).

## 13. Setup, guard, doctor, and instructions

`setup mcp --client claude-code` targets `.mcp.json` in the current project (scope `project`) or `~/.claude.json` (scope `user`), inserting `mcpServers.worklease = {"command": "<absolute path of the running worklease binary>", "args": ["mcp"]}` (with `env.WORKLEASE_AGENT_ID` only when `--agent` is given). `--client cursor` targets `.cursor/mcp.json` or `~/.cursor/mcp.json` with the same shape. `--client generic` prints the JSON snippet only. Default is a preview showing the target path and a unified diff; `--apply` writes atomically (temporary file plus rename), preserving all unrelated keys semantically (formatting is normalized to two-space indentation); a non-object root or invalid JSON fails `setup-config-malformed` (64) without writing. `--remove` deletes only `mcpServers.worklease`. Apply and remove are idempotent.

`setup guard --client claude-code` targets `.claude/settings.json` (project) or `~/.claude/settings.json` (user) with matcher `Edit|Write|MultiEdit|NotebookEdit` and an absolute, safely quoted running-binary path invoking `verify --hook claude-code` (claim coverage by default); `setup guard --coverage path` generates `verify --hook claude-code --coverage path` for per-file enforcement, and the managed entry is identified for removal by the `verify --hook claude-code` command prefix in either form. There is no `--include-bash` in v1. `--client generic` prints a POSIX wrapper for an explicitly selected claim and expected path resources. Preserve unrelated configuration, refuse symlinked/unsafe files and concurrent edits, and remove only the exact managed entry. User-scoped MCP configuration uses the client’s documented global configuration shape, verified against current client docs by TASK-85.16; do not assume it is identical to project configuration. Generated configuration preserves explicit home/config/session selection so CLI, MCP and hooks do not silently use different authorities. Document native-hook resource binding, MCP lease selection, unsupported tools, and check-to-edit/provider fencing limits.

`setup instructions` prints an AGENTS.md block containing the `worklease instructions loop` and `safety` text between `<!-- worklease:begin vX.Y.Z -->` and `<!-- worklease:end -->` markers. `instructions loop|safety` returns the canonical text held in `internal/instructions`, adapted from `src/worklease/instructions.py` to the Go command names.

`doctor` runs read-only checks and prints one line per check: `id`, `status` (`ok`, `warn`, `fail`, `unknown`), `detail`, and optional `hint`. Check IDs: `config.sources`, `home.path`, `home.permissions`, `db.open` (read-only open), `db.schema`, `context.root`, `handle.present`, `handle.permissions`, `agent.identity`, `git.available`, `clock.monotonic`, `clock.authority`, `authority.identity`, `mcp.available`, and `state.python-era`, which warns when the home still contains Python-era files (`leases.sqlite3`, `locks/`, `context-leases/`, `mcp-leases/`) and points to the disposal instructions; the Go store never reads those files. It creates no directories, handles, or database files (SQLite may recreate private WAL sidecars for its read-only open), never reads token fields, and states that it cannot verify other hosts or provider-side fencing. Exit 0 when no check is `fail`, otherwise 1.

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

CI: a Go job matrix on `ubuntu-latest`, `ubuntu-24.04-arm`, `macos-15-intel`, and `macos-14` runs `mise run ci-go`; the Python jobs remain until TASK-85.18 removes them. Release (TASK-85.17): on `v*` tags, verify CI passed for the commit, build the four archives with `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.buildVersion=<version> -X main.buildCommit=<sha> -X main.buildTime=<iso>"`, generate the man page, write `checksums.txt`, install every archive in a clean directory, run `worklease version --json`, `worklease key`, an acquire/verify/release lifecycle against a temporary home, and modern MCP `server/discover`/`tools/list` plus legacy `initialize`/`tools/list` round trips, then, only under explicit owner release authorization, publish with `gh release create`. Preparing and smoke-testing artifacts is part of the rewrite; pushing, creating tags/PRs, dispatching publication, and publishing releases require separate authorization and are not inferred from an unattended task loop.

Test conventions: tests create isolated homes with `t.TempDir()` and `WORKLEASE_HOME`; clocks are injected (`testkit.Clock`); identifiers and tokens come from an injected generator; concurrency tests bound every wait with a deadline and fail with diagnostics rather than hanging; multi-process tests run the test binary itself as a subprocess (`os.Args[0]` with a `WORKLEASE_TEST_HELPER` marker) so they need no separate build step.

## 15. Amendment procedure

Amend only when: (a) a spike (TASK-85.4) records a required deviation; (b) an implementation task proves a fixed decision impossible or unsafe with executable evidence; (c) the repository owner requests a change. Never amend for preference. To amend: add an entry to section 17 of this document (`backlog doc update <doc-id> --content ...` after reading the current content) with the date, task ID, the decision ID or section changed, the new text, and the evidence; then add a comment on TASK-85 summarizing it. Implementation continues under the amended text. Tasks must not carry contradictory local designs in notes or code comments instead of amending.

## 16. Retired Python surfaces

Removed at TASK-85.18 with no replacement: the Python public API (`worklease.__all__`), the `worklease-mcp` entry point, the `worklease_source_sdk` package and example plugin, entry-point resource-policy plugins, PyInstaller and `uv` packaging, JSON schema version 1 files, the Python lease-file format and the `context-leases` and `mcp-leases` directories, the `owner ID` identity, `--token` on argv, bundle-specific commands (`acquire-bundle`, `status-bundle`, `heartbeat-bundle`, `release-bundle`, `exec-bundle`, `inspect-operation-bundle`, `reconcile-operation-bundle`) and their aliases, `--provider-directory` (renamed `--cwd`), the `--format` flag (only `--json` remains), the MCP lifecycle benchmark, `docs/source-provider-sdk-compatibility.md`, and the Python-era SQLite schema versions 1 to 3 (no import). Python-era state directories are left untouched on disk; installation docs describe optional recoverable disposal. Keep `docs/distributed-cloudflare-claim-authority.md` as deferred Go-oriented design evidence; it is neither a v1 compatibility surface nor a shipped remote feature.

## 17. Amendments

Entries are chronological. A later entry supersedes any earlier entry it contradicts; the body of this document always reflects the latest state. In particular the TASK-86 entries supersede the request-hash, Bash-allowlist, `--include-bash`, and token-on-stdout text of the two earlier entries.

- 2026-09-12, owner-requested before the loop started (no task): section 10.3 `--hook claude-code` now parses the hook JSON (`cwd`, `tool_name`, `tool_input.command`) and allowlists Bash commands whose first word is `worklease` or `backlog`; section 13 default guard matcher excludes `Bash`, with `--include-bash` as opt-in; section 13 `doctor` gains the `state.python-era` check; section 19 records the absolute hum path, the `Blocked` label convention, and the closure of the Python-era tasks. Evidence: a blanket Bash matcher would have blocked `worklease acquire` itself, and worktree checkouts cannot resolve `../hum`.
- 2026-09-12, adversarial contract review before the loop started (no task): section 7.4 keeps `ttl` in the request hash and drops `maxDuration`, and defines acquire idempotency (key `claimId`, operations row with `operation_id = claimId`, replay returns the claim without the token); section 10.1 leaves the operation `started` on ownership loss instead of writing `completed`; section 4 makes `--revision` optional for the read-only `verify` and `status`; section 7.3 states that guarded operations raise the revision by more than one and that the handle receives the final revision; section 6.1 drops the never-emitted `gc-protected-record` reason; sections 7.5 and 9 name the triggers of `operation-ambiguous` and `credential-unsafe`. Evidence: the earlier text contradicted TASK-85.7 acceptance criterion 3, the Python fingerprint in `operations.py` (keeps ttl, drops maxDuration), and `execution.py`, which re-raises on renewal failure and leaves the operation started.

- 2026-09-12, TASK-86, owner-requested product review: D5–D8 and sections 4–13, 16, 18–20 supersede prior Python-derived choices. Introduce session-scoped exclusive handle selection, pre-dispatch durable client credentials and exact requests, authenticated bounded replay, authority-bound handles/cursors, predecessor reconciliation, explicit local replacement protection, expiry-aware watches and contiguous-prefix GC. Remove claim-wide fenced promises, token recovery on stdout, hash-only automatic replacement reconciliation, Bash first-word bypass, and MCP revision secrecy. Preserve deferred remote design and make the inventory gate implementation. Evidence and counterexamples are recorded in section 21; the owner explicitly waived Python compatibility and requested anticipation of remote support. Go 1.27.1 was confirmed available at https://go.dev/dl/?mode=json during this review. D13/section 12 now target the current 2026-07-28 MCP spec plus 2025-11-25 legacy interoperability, based on the official versioning/stdio/lifecycle sources linked there, rather than freezing the Python-era protocol list.

- 2026-09-12, TASK-86, owner-requested ergonomics priority: section 1.1 makes setup-free human commands and handle-backed JSON/typed MCP orchestration release requirements; section 12 preserves structured domain error details across MCP. TASK-85.14–85.17 gain executable common-path, contention, session-isolation and onboarding acceptance journeys. Evidence: the owner explicitly prioritized quick human commands and ergonomic MCP/JSON support for agent orchestration; the prior MCP error sketch dropped section 6 details needed for machine decisions. No new commands, MCP tools or remote implementation are introduced.

- 2026-09-12, owner-accepted review of the TASK-86 refinement (no task): section 10.3 and 13 native guard defaults to `--coverage claim` (a current valid claim in the selected context or session authorizes native edits) with `--coverage path` as the opt-in per-file mode, because exact path coverage with a 32-resource claim cap made the guard unusable for task-scoped work; section 8 no longer requires the SQLite driver itself to enforce no-follow opens and instead requires TASK-85.4 to document driver capability and TASK-85.6 to verify device and inode after open; this section gained the supersession rule; every TASK-85.x description regained an "Evidence and patterns" paragraph naming the Python tests and hum files that the TASK-86 rewrite had compressed away. Owning tasks 85.12 and 85.16 were updated to match.

- 2026-09-12, owner-authorized remote architecture pivot (no task): section 20 replaces the recommended Cloudflare Durable Object reimplementation with serving the existing Go authority over authenticated HTTPS from one host, and names the restore generation and single-writer guard as pre-release remote invariants. No current decision, command, or shipped behavior changes; remote implementation remains deferred. Evidence: `internal/lease`, `internal/store`, `internal/ledger`, and `internal/gc` already implement the semantics a second authority would have to reproduce, `internal/store/driver.go` already provides the single-writer `BEGIN IMMEDIATE`, WAL, synchronous FULL posture, and `lease.Service` already separates `BeginOperation`/`RenewOperation`/`CompleteOperation` from the local effect callback; the owner accepted the pivot in review. Full rationale and alternatives are in `docs/distributed-cloudflare-claim-authority.md`.

- 2026-09-12, TASK-85.4 SQLite driver proof: D2/D3/D8 and section 8 select `modernc.org/sqlite v1.58.0` with `CGO_ENABLED=0`; `_txlock=immediate` writes, deferred read-only observations, one pooled connection, WAL, synchronous FULL, busy timeout 10000, and foreign keys ON. Tests prove pragma read-back, cross-process serialization, rollback, bounded cancellation, AUTOINCREMENT non-reuse, killed uncommitted versus durable committed writes, private main/WAL/SHM handling, and WAL-visible read-only access without modifying the main database; TASK-88 later proved that SQLite may update shared-memory coordination words and that SQLite recreates absent owner-private `-wal`/`-shm` sidecars in a writable directory. The driver cannot open SQLite through a caller-supplied no-follow descriptor, so pre-open path checks do not close the final replacement race; TASK-85.6 retains the pinned-directory and opened device/inode verification obligation. Commit errors require independent durable read-back and may remain unknown. Evidence: `internal/store/driver_test.go`, four linux/darwin amd64/arm64 CGO-disabled builds, `mise run ci-go`, zero `govulncheck` findings, and the dependency's BSD-3-Clause/SQLite license files.

## 18. Internal API sketches

These names locate responsibilities; signatures must carry the fields required by the amended contract. Prefer existing typed requests/results over a generic backend framework. Consumer-specific interfaces may be extracted when there is an actual consumer need; no remote implementation or placeholder backend is required.

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
type Key struct { Provider, Source, Item, Resource, Capability, Scope, IdentityScope string; LocalReplaceAllowed bool }
type Policy interface { Name() string; Describe() Descriptor; Key(in Input) (Key, error) }
func Lookup(name string) (Policy, error)
func Names() []string

// internal/lease
type Clock interface { Now() time.Time; Monotonic() time.Duration }
type Credentials struct { AuthorityID, ClaimID, Token string; Revision int64 }
type Service struct { /* store, clock, id generator, defaults */ }
func New(st *store.Store, clock Clock, ids IDGenerator, defaults Defaults) *Service
func (s *Service) Acquire(ctx context.Context, req AcquireRequest) (Grant, error)   // AcquireRequest carries client-held credential; Grant never returns a token
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
func ContextualPath(home, root, sessionSelector string) string
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

- Select only tasks labeled `go-rewrite` (milestone `m-0`) and only when every dependency is `Done`. Use the live dependency graph as the sole readiness order; there is no separate wave list. TASK-85.1 precedes bootstrap, TASK-85.4 needs the test foundation, and recovery/handle APIs precede their guard and MCP consumers. Parallel work is allowed only on distinct owned surfaces.
- Before coding: `backlog task view TASK-85.N --plain`, read this document, read the Python files listed in the task as evidence, and the hum files listed as patterns. Record the plan with `backlog task edit --plan`.
- Path references: tasks cite hum patterns as `../hum/...`, which resolves only from the primary checkout at `/Users/brett/dev/me/worklease`. From a worktree use the absolute path `/Users/brett/dev/me/hum`. Python evidence paths are repository-relative and resolve from any checkout until TASK-85.18 deletes them; after that, read them with `git show <sha>:<path>` using the SHA recorded in the capability inventory.
- Python-era tasks TASK-74 through TASK-84 were closed as superseded on 2026-09-12 with final summaries naming their Go owners, and TASK-67 shipped in Python before the cutover. Do not reopen them; TASK-85.18 verifies that no nonterminal Python-era task remains.
- Own only the paths in section 3. Register new commands in `internal/cli/commands.go`. Append one `CHANGELOG.md` Unreleased line per user-visible change.
- Gate: documentation-only TASK-85.1 uses Backlog integrity and the existing repository gates; implementation tasks run `mise run ci-go`. Do not require a Go gate before it exists or weaken a failing check.
- Finalize per `backlog instructions task-finalization`: check each acceptance criterion with named test functions or command output as evidence, write the final summary, set `Done`, and commit with a concise imperative message (for example `Add Go lease service`).
- If a fixed decision blocks you, follow section 15. If the owner must decide, record the exact question as a task comment, finish everything else, leave the task `In Progress` with the blocker in notes, and add the `Blocked` label (`backlog task edit TASK-85.N --add-label Blocked`) so `backlog-list` surfaces it; remove the label when unblocked.

## 20. Remote authority boundary (feature deferred)

V1 ships only the local SQLite authority. Keep its typed claim/operation service independent of CLI/MCP encoding, local handle paths, and provider APIs. SQLite transaction callbacks and process supervision stay local implementation details; do not design a remote protocol around SQL callbacks or a remote process runner. Reuse those observable service contracts in future conformance tests. Do not build an HTTP client, backend registry, Worker, auth configuration, or deployment tooling now.

Create immutable random authorityId in local meta at bootstrap; include it in non-secret receipts and bind it into requests, handles, and cursors. A home path/URL is a locator, not authority identity. Do not copy an active authority database to create an independent authority with the same ID. Restore and remote namespace identity need explicit operational rules before remote release; local guarantees never extend across copied databases or hosts.

Resources contend by exact bytes inside one authority namespace. One multi-resource claim must remain atomic within that namespace. Local path/backlog/Markdown keys are explicitly host-local: absolute git common directories are not portable repository identity. Future remote filesystem coordination needs a caller-selected stable repository/source namespace plus relative locator, not a silently guessed Git remote URL, login, session, or worktree ID. Portable opaque/provider keys remain usable today without adding namespace configuration.

The deferred design in `docs/distributed-cloudflare-claim-authority.md` (historical file name) recommends serving this same Go authority (`lease.Service` plus the SQLite store) over authenticated HTTPS from one always-on host with one volume, asynchronous replication to object storage, and an authenticated front door such as Cloudflare Tunnel plus Access. A Durable Object reimplementation was rejected because it duplicates every safety invariant in a second language. Future authority authentication is separate from each claim credential; configured remote failures must never fall back to local claims. Multi-host transport must preserve exact request/replay and unknown-outcome semantics and explicitly handle network partitions. A hosted deployment adds two invariants before release: a random restore incarnation identifier (`restoreId`) in `meta`, regenerated on every restore and bound into requests, receipts, handles, and cursors so a restore fails closed into a quarantined recovery state, and an exclusive process-lifetime OS lock so one database is never served by two live processes. Neither exists today and neither is added before the feature is authorized.

Do not add a fencing counter now. A future provider-enforced fence must increase across epochs per resource, survive retention/restore, and be scoped to the authority namespace; the current per-claim revision and diagnostic event seq are neither such a fence nor provider evidence. Remote client-side exec remains coordination only. Remote replacement must be disabled unless a separately specified authority/provider-side adapter can enforce that operation’s boundary.

## 21. Review counterexamples and acceptance ownership

| Counterexample | Required decision / acceptance owner |
| --- | --- |
| Two loops share cwd/login but work on different resources; two processes both see an absent handle. | Stable session selectors and a cross-process handle lock prevent credential overwrite/adoption; 85.10, 85.15. |
| Acquire or transfer commits, then the client dies before saving its bearer; a public replay fetches a private receipt. | Persist client credentials before dispatch; authenticate replay even after epoch end; never emit tokens; 85.7, 85.10. |
| An exec request is replayed with a different cwd, content digest, or duration. | Exact normalized intent and bounded requestNotAfter; changed intent conflicts; 85.7, 85.12. |
| Guard A starts, guard B uses the latest revision, or A’s supervisor dies and its child survives expiry. | One started slot, predecessor unknowns block new guards, explicit quiescence evidence, honest coordination guarantee; 85.7, 85.9, 85.12. |
| A copied handle or cursor is used against another authority; a local repo path is used from another host. | Authority binding and explicit host-local resource metadata, no silent fallback/normalization; 85.5, 85.6, 85.10, 85.13. |
| A file has the proposed new hash but an old executor may still run; a handle for file A is used to edit B. | Hash equality is not reconciliation; `--coverage path` enforces exact path membership when enabled, and the default claim coverage documents that it does not; 85.12, 85.16. |
| A lease expires without a write; a matching event arrives between a watch’s last scan and timeout. | Poll time/state without events; return only the last scanned cursor; 85.13. |
| One old claim pins history while newer ended claims are GC eligible; an ancient expired claim is retired today. | Prune only a contiguous prefix and retain from recorded end, never delete fresh recovery evidence; 85.11. |
| Eight long MCP calls block stdin processing; restarting renewal resets a four-hour maxHold. | Cancellation/EOF remain readable; persist absolute holdUntil and do not auto-resume; 85.15. |
| A command begins with backlog but also contains a second shell mutation. | No shell-string allowlist; native path-aware guards only; 85.12, 85.16. |

These are documentation acceptance scenarios for implementation, not claims that Go code already passes them. Existing Python is evidence only. TASK-85.1 inventories capability families and named safety tests; it is not a requirement to port every Python test or implementation detail.
