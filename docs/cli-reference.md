# CLI reference

Run `worklease COMMAND --help` or read `worklease(1)` for every flag and
example. `worklease help --all` prints the root help followed by every
command and subcommand once, in tree order, without touching any state; it is
the one-shot onboarding read for people and agents. Top-level help groups
commands into *Claim lifecycle*, *Inspection and recovery*, and *Setup and
administration*.

Usage lines show required inputs and alternate forms, for example
`worklease exec [selection] ... -- COMMAND [ARGS...]`, `worklease policy describe
NAME`, and the two `history` projections. `[selection]` stands for the shared
claim-selection options that every contextual command accepts; each such
command's help explains them. Option help shows effective runtime defaults
(`--ttl` 15m, `--max-duration` 1h, `--limit` 50, `--timeout` 30s,
`--retention-days` 30) and never a zero-value sentinel.

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

`list` prints a compact `STATE`, `RESOURCE`, and relative `LEASE` table, or the
single line `no current claims` when nothing matches (missing authority, empty
authority, or a `--resource` filter with no match). Git-backed
resources collapse to provider, repository, and item; coordination hashes use a
short non-secret fingerprint. `list --full` shows unshortened resources, claim
and agent IDs, and absolute expiry timestamps, subject to the normal
bearer-shaped-value redaction policy. `list --resource KEY` accepts exactly one
exact resource. `status` similarly uses relative expiry in
its compact view; `status --full` adds complete non-secret claim metadata and
RFC3339 timestamps.

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

Guarded command streams and authenticated inspection payloads use indented,
labeled blocks instead of escaped single lines. Compact `events` rows show only
the event kind, compact resource set when present, and relative time; authority-
wide events such as `gc-applied` omit resource placeholders. Compact
`history --resource` epochs show the agent, status, relative acquire and end
times, end reason, and ordered operation kinds; a non-completed operation
includes its state (`exec:started`). `--full` views add complete non-secret
identifiers and metadata, per-operation rows, and absolute RFC3339 timestamps.
Timeline rows, full resource-status rows, and acquire-recovery rows do not use
synthetic row numbers. `history` without a resource is an alias for the
bounded global event feed. Its `--json` output is the canonical events envelope,
including `operation: "events"`; `history --resource RESOURCE --json` instead
returns the resource-scoped history envelope. `policy describe --full` adds
contract versions and fencing guarantees. `key`, `watch`, and `gc` use fixed
command-specific field ordering; a `gc` preview ends with the exact
`worklease gc --apply --cutoff TIME` follow-up. Use `--json` as the canonical
complete structured output.

Cursor policy: text views never print a bare opaque cursor. `events` and
`history` text omit cursors entirely; page with `--json` and `--cursor`. `watch`
text omits routine false booleans, shows relative expiry per resource, reports a
retention gap only when one exists, and presents its resumption cursor only inside
a copyable `resume: worklease watch --cursor ...` line. JSON cursor fields are
unchanged.

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

`exec` reports `local-coordination`; it is not provider fencing. Replacement
content is limited to 16 MiB and is read and digested before the guarded
transaction. Only successful expected-hash replacement reports
`local-serialized-replace`. A started guarded
operation blocks another until its outcome and process cessation are established.
Exact replay accepts the same operation ID and normalized request during its
bounded recovery window; changed intent is rejected.

## Policy and administration

| Command | Purpose |
| --- | --- |
| `policy list` / `policy describe` | Inspect static built-in policies. |
| `doctor` | Run read-only configuration, permissions, authority, handle, clock, MCP, Git, and Python-era-state diagnostics. |
| `instructions loop|safety` | Print canonical agent instructions. |
| `setup mcp` | Preview/apply/remove Claude Code, Cursor, or generic MCP setup. |
| `setup guard` | Preview/apply/remove optional native edit-hook setup. |
| `setup instructions` | Generate the managed AGENTS.md block. |
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

## Deferred authority

[`distributed-cloudflare-claim-authority.md`](distributed-cloudflare-claim-authority.md)
is a proposal, not a shipped HTTP backend. Worklease uses one local SQLite
authority. It has no remote fallback.
