# CLI reference

Run `worklease COMMAND --help` or read `worklease(1)` for every flag and
example. `worklease help --all` prints the root help followed by every
command and subcommand once, in tree order, without touching any state; it is
the one-shot onboarding read for people and agents. Top-level help groups
commands into *Claim lifecycle*, *Inspection and recovery*, and *Setup and
administration*.

Usage lines expose required inputs and alternate forms:

```text
worklease exec [selection] ... -- COMMAND [ARGS...]
worklease policy describe NAME
worklease history [--resource RESOURCE]
```

`[selection]` means the shared claim-selection options listed in that command's
help. Help shows runtime defaults, including `--ttl` 15m, `--max-duration` 1h,
`--limit` 50, `--timeout` 30s, and `--retention-days` 30.

## Global interface

```text
worklease [--json] [--home PATH] [--config PATH] COMMAND
```

Configuration precedence is flags, environment, YAML, then defaults. Important
environment variables are `WORKLEASE_HOME`, `WORKLEASE_CONFIG`,
`WORKLEASE_AGENT_ID`, and `WORKLEASE_SESSION_ID`. Text output is for humans;
`--json` emits one schema-version 2 envelope.

Bearer credentials are accepted only through a private contextual/explicit
handle, `--token-file`, or `--token-fd`. An argv `--token` option is deliberately
unsupported. Acquire persists a client-generated credential before dispatch and
never returns it in text or JSON.

## Short options

Short options have exactly one meaning across the command tree and are available
wherever the corresponding long option is supported:

| Short | Long | Short | Long |
| --- | --- | --- | --- |
| `-j` | `--json` | `-H` | `--home` |
| `-h` | `--help` | `-v` | `--version` |
| `-r` | `--resource` | `-s` | `--session` |
| `-t` | `--ttl` | `-w` | `--wait` |
| `-a` | `--agent` | `-f` | `--full` |
| `-m` | `--reason` | | |

No other option has a short alias. In particular, `--source`, `--work-key`, the
provider triple, handles and leases, explicit credentials, replay and polling
controls, coordination-only mode, and guarded-operation tuning are long-only.

Every command named `list` also accepts the conventional `ls` alias: `worklease
ls`, `policy ls`, `profile ls`, and `installation ls`. The canonical names remain
`list` in help, documentation, and structured operation names.

## Shell completion

`worklease completion (bash|zsh|fish)` prints a deterministic, ANSI-free shell
completion script derived from the registered command tree. It includes visible
root and nested commands, command aliases, global options, and command-local
options. Generation and completion requests do not inspect or mutate claim
state; the framework's completion protocol remains hidden from suggestions.
Unsupported shell names fail with an `invalid-argument` error listing the three
supported shells.

Enable completion with the matching shell setup:

```bash
source <(worklease completion bash)
```

```zsh
source <(worklease completion zsh)
```

```fish
mkdir -p ~/.config/fish/completions
worklease completion fish > ~/.config/fish/completions/worklease.fish
```

## Common lifecycle

| Command | Purpose |
| --- | --- |
| `version` | Print a concise version, short commit, and build time. Use `--json` for full build and output-schema metadata. |
| `key` | Derive an exact built-in resource key. |
| `acquire` | Atomically claim one to 32 ordered, unique resources. |
| `status` / `list` | Read current non-secret claim state. |
| `heartbeat` | Renew the current ownership epoch. |
| `checkpoint` | Persist bounded local recovery metadata and renew. |
| `release` | End the exact current claim. |
| `transfer` | Atomically create a client-credentialled successor claim. |
| `verify` | Verify current ownership, optionally with exact path coverage. |

Use `--session NAME` for each concurrent loop. Without an explicit handle,
Worklease selects an authority-bound contextual handle by Git worktree root (or
resolved current directory) and session. A handle is convenience state, not the
claim or an authoritative provider checkpoint.

One claim covers all `--resource` values atomically. Resources contend by exact
bytes and are never silently normalized.

Human-readable output uses outcome sentences or table headers rather than a
standalone command-name banner. Key/value labels use lower camel case aligned
with the JSON vocabulary (`claimId`, `expiresAt`, `nextCursor`); table headers
use upper snake case (`CLAIM_ID`, `EXPIRES_AT`). JSON field names and envelopes
are unchanged.

`list` has three useful forms:

| Form | Output |
| --- | --- |
| `list` | Compact `STATE`, `RESOURCE`, and relative `LEASE` table; Git resources collapse to provider/repository/item. |
| `list --full` | Full resources, claim and agent IDs, and RFC3339 expiry, with normal redaction. |
| `list --resource KEY` | One exact resource, or `no current claims`. |

An absent, empty, or filtered-out authority also prints `no current claims`.
Coordination hashes appear as short non-secret fingerprints. `status` uses
relative expiry; `status --full` adds complete non-secret metadata and RFC3339
timestamps.

On an interactive terminal, headers are bold; healthy/available states and event
kinds are green, attention states are yellow, and failures are red. Styling is
limited to semantic headers, states, kinds, garbage-collection outcomes, and
error guidance; identifiers and timestamps are never colored. Color is omitted
when output is redirected, `TERM=dumb`, or `NO_COLOR` is set. JSON is never
colored.

Successful lifecycle mutations name the action and claim, then show only
operation-relevant fields. In text, checkpoint reports persisted byte size and
transfer confirms the successor handle and complete resource set. These additions
do not change their stable JSON envelopes. Verification preserves full unresolved
operation IDs needed for recovery.

Inspection output favors compact timelines:

| View | Compact output | `--full` adds |
| --- | --- | --- |
| `events` | Kind, resource set when present, relative time | Complete non-secret event metadata |
| `history --resource` | Agent, status, relative acquire/end times, end reason, ordered operation kinds | IDs, per-operation rows, RFC3339 timestamps |
| Guarded/authenticated payloads | Indented labeled blocks | Complete allowed payload |

Non-completed operations include state, such as `exec:started`. Authority-wide
events such as `gc-applied` omit resource placeholders. Timelines and recovery
rows have no synthetic row numbers.

`history` without a resource aliases `events`; its JSON envelope therefore uses
`"operation":"events"`. Resource-scoped history has its own envelope.
`policy describe --full` adds contract versions and fencing guarantees. `key`,
`watch`, and `gc` use fixed field order. A GC preview ends with the exact apply
command. Use `--json` for complete structured output.

Cursor policy:

- `events` and `history` text omit cursors; page with `--json` and `--cursor`.
- `watch` omits false booleans, uses relative expiry, and reports retention gaps
  only when present.
- `watch` exposes a cursor only as `resume: worklease watch --cursor ...`.
- JSON cursor fields are unchanged.

## Guarded and recovery operations

| Command | Purpose |
| --- | --- |
| `exec -- ARGV...` | Supervise one argv operation while local ownership remains valid. |
| `replace-file` | Verify an expected hash and atomically replace an exact claimed path. |
| `op inspect` | Inspect a redacted started/completed operation. |
| `op reconcile` | Resolve a predecessor unknown outcome with explicit evidence. |
| `history` / `events` | Show the latest global lifecycle events; add `history --resource RESOURCE` for retained epochs of one resource. |
| `watch` | Wait for a resource state or event cursor change. |
| `gc` | Preview or apply contiguous-prefix retention. |

Guarded-operation guarantees:

- `exec` reports `local-coordination`, not provider fencing.
- `replace-file` reads and digests at most 16 MiB before its transaction. Only a
  successful expected-hash replacement reports `local-serialized-replace`.
- A started operation blocks another until outcome and process cessation are
  established.
- Replay requires the same operation ID and normalized request within the
  recovery window; changed intent is rejected.

## Policy and administration

| Command | Purpose |
| --- | --- |
| `policy list` / `policy describe` | Inspect static built-in policies. |
| `doctor` | Run read-only configuration, permissions, authority, handle, clock, MCP, Git, and Python-era-state diagnostics. |
| `instructions loop|safety` | Print canonical agent instructions. |
| `setup mcp` | Preview/apply/remove Claude Code, Cursor, or generic MCP setup. |
| `setup guard` | Preview/apply/remove optional native edit-hook setup. |
| `setup instructions` | Generate the managed AGENTS.md block. |
| `completion bash|zsh|fish` | Print the completion script for a supported shell. |
| `mcp` | Serve Model Context Protocol on stdin/stdout. |

Built-in resource policies are `backlog-md`, `markdown`, `github`, `linear`,
`generic`, and `path`. Repository/path keys are explicitly host-local. Provider
keys remain opaque; deriving one does not authenticate or mutate a provider.

Native edit hooks default to claim coverage. `--coverage path` opts into exact
path membership. Hooks do not intercept shell/provider writes and do not turn
local coordination into cross-host or provider-side fencing. See
[setup](setup.md) for client-specific binding and removal.

## Errors and exit codes

JSON failures use:

```json
{"schemaVersion":2,"ok":false,"operation":"acquire","error":{"reason":"already-claimed","exitCode":2,"message":"resource is already claimed","details":{}}}
```

Exit families are: `0` success, `1` internal failure, `2` ownership/contention,
`3` ledger, replay, or ambiguous outcome, `64` invalid input/configuration, `75`
authority or storage failure, `124` child timeout, and `130` interruption. Domain details include safe holder or recovery metadata, never
credentials, requests, command output, file contents, checkpoints, or provider
payloads.

## Selection and concurrency

Selection precedence is explicit handle, configured handle, exact credentials,
then contextual session handle. Credential sources are mutually exclusive.
Handles and cursors include authority identity and fail closed against another
authority.

A claim contains one immutable claim ID, one credential, one revision stream,
and one to 32 resources. All lifecycle mutations apply to the whole claim.
MCP leases have an absolute `maxHold` deadline; ordinary CLI claims do not.
Concurrent sessions need distinct selectors even in one checkout.

## Experimental remote authority

The standard binary opens no listener and makes no network request unless you:

- manage a remote profile;
- select a remote profile; or
- run `serve`.

Local reads need no setup, and remote failures never fall back to local. The
experimental, self-hosted authority supports one namespace, process, and SQLite
writer on one single-host filesystem—not HA, provider fencing, or exactly-once
execution. See [`remote-claim-authority.md`](remote-claim-authority.md).

### Profiles and enrollment

Profiles are owner-private and selected in this order: `--profile NAME`,
`WORKLEASE_PROFILE`, explicit user-side checkout binding, user default, then
local. `--local` overrides local selection and conflicts with `--profile` or
`WORKLEASE_PROFILE`. Endpoint changes require remove/add.

```text
worklease profile add NAME --endpoint URL --authority-id ID [--allow-insecure-http]
worklease profile list
worklease profile ls
worklease profile show NAME
worklease profile remove NAME
worklease profile default NAME
worklease profile bind NAME [--cwd DIR]
worklease profile unbind [--cwd DIR]
worklease enroll --profile NAME (--invite-file FILE|--invite-fd N) [--label TEXT]
```

`profile add` performs metadata discovery and pins authority/incarnation before
saving. `enroll` never accepts an invite or installation credential on argv.
An admin issues an invite with exactly one output source:

```text
worklease invite issue --profile NAME --role read|write|admin \
  (--invite-file FILE|--invite-fd N) [--label TEXT] [--expires-at RFC3339] \
  [--operation-id ID] [--request-not-after RFC3339]
```

The invite is generated before dispatch; the file form is durably saved first,
while an fd caller owns durable capture. Only its hash crosses the network. Roles are `read`, `write`, and `admin`. Stable remote reasons include
`authentication-required`, `installation-revoked`, `authorization-denied`,
`authority-restored`, `resource-not-enrolled`, `recovery-required`,
`recovery-closed`, `unknown-outcome`, `operation-ambiguous`,
`operation-request-mismatch`, and `operation-kind-unsupported`.

### Remote and hosted commands

The selected profile applies to lifecycle, inspection, guarded operations,
events, watches, and GC. Remote differences:

- `--wait` is a client loop capped at 60 seconds; `--poll-interval` is local-only.
- GC requires `--apply` plus `--cutoff TIME` or `--retention-days N` (default 30).
- Every mutation, including `--no-handle`, saves its exact pending request first.
- `path:`, `backlog-md:`, and `markdown:` resources are rejected.
- Same-host transfer requires a named predecessor handle.

Remote administration is:

```text
worklease installation list --profile NAME [--include-revoked]
worklease installation revoke --profile NAME --installation-id ID [--reason TEXT] [--operation-id ID] [--request-not-after RFC3339]
worklease claim revoke --profile NAME --claim-id ID [--reason TEXT] [--operation-id ID] [--request-not-after RFC3339]
worklease recovery status --profile NAME
worklease recovery reopen --profile NAME --expected-recovery-revision N --attestation-file FILE [--operation-id ID] [--request-not-after RFC3339]
```

Server lifecycle operations are offline-only and take the hosted lock before opening
SQLite:

```text
worklease server init [--server-config FILE] [--bootstrap-invite-file FILE]
worklease server restore --home DIR --from FILE --selected-cutoff RFC3339 --loss-interval-start RFC3339 --loss-interval-end RFC3339 --bootstrap-invite-file FILE [--cutoff-unknown]
worklease server bootstrap-reissue --home DIR --bootstrap-invite-file FILE
worklease server retire --home DIR [--force --unresolved-export FILE]
worklease serve [--server-config FILE] [--allow-insecure-http]
```

With no arguments, `server init` creates a local-only configuration, authority,
and bootstrap invite in the user's XDG directories. `serve` resolves config in
this order:

1. `--server-config`
2. `WORKLEASE_SERVER_CONFIG`
3. `$XDG_CONFIG_HOME/worklease/server.yaml`

Settings load only at startup; changes require stop-before-start. Restore:

- creates a fresh incarnation;
- ends active claims as `restored` and revokes old credentials;
- retains started operations as unresolved; and
- blocks ordinary admission until an admin attests to installation inventory,
  pending requests, retained and lost-tail outcomes, executor/provider
  cessation, and the cutoff-to-cessation loss interval.

Unknown bounds must be explicit and waive nothing. Any coverage gap keeps
recovery closed. A missing completed operation also needs independent
no-residual-effect evidence. Recovery import and completed-history journals are
unsupported.

`replace-file`, provider execution, recovery import, completed-history
journaling, cross-host transfer, repository enrollment, HA, Postgres,
multi-namespace serving, admission backpressure, browser control plane, and
remote retirement are unsupported. Public publication, tags, pushes, and
release execution are separate owner-authorized actions.
