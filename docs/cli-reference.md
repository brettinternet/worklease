# CLI reference

Run `worklease COMMAND --help` or read `worklease(1)` for every flag and
example.

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

`list` prints a compact `STATE`, `RESOURCE`, and relative `LEASE` table without
an operation-name banner. Git-backed resources collapse to provider, repository,
and item; coordination hashes use a short non-secret fingerprint. `list --full`
shows unshortened resources, claim and agent IDs, and absolute expiry timestamps,
subject to the normal bearer-shaped-value redaction policy.
On an interactive terminal, headers are bold; healthy/available states are green,
attention states are yellow, and failures are red. This styling applies to
`list`, `status`, `doctor`, garbage-collection outcomes, and error guidance.
Color is omitted when output is redirected, `TERM=dumb`, or `NO_COLOR` is set.
JSON is never colored.

Successful lifecycle mutations name the action and claim, then show only
operation-relevant fields such as revision, expiry, release reason, successor,
or guarded command result. Verification and operation inspection follow the same
summary-first format. Use `--json` for complete structured envelopes.

## Guarded and recovery operations

| Command | Purpose |
| --- | --- |
| `exec -- ARGV...` | Supervise one argv operation while local ownership remains valid. |
| `replace-file` | Verify an expected hash and atomically replace an exact claimed path. |
| `op inspect` | Inspect a redacted started/completed operation. |
| `op reconcile` | Resolve a predecessor unknown outcome with explicit evidence. |
| `history` / `events` | Read redacted authority-bound lifecycle records. |
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
