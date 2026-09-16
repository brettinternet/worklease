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

## Output conventions

| Surface | Convention |
| --- | --- |
| Text | Outcome sentences or tables; lower camel case labels and upper snake case headers. |
| JSON | Stable schema-version 2 envelopes; never colored. |
| Color | Interactive semantic states only; disabled for redirected output, `TERM=dumb`, or `NO_COLOR`. |
| `--full` | Complete non-secret metadata, full resources and IDs, and RFC3339 timestamps. |
| Redaction | Credentials and private operation payloads stay hidden unless an authenticated command explicitly allows them. |

Common inspection forms:

| Form | Output |
| --- | --- |
| `list` | `STATE`, `RESOURCE`, and relative `LEASE`; Git resources collapse to provider/repository/item. |
| `list --full` | Full resources, claim and agent IDs, and RFC3339 expiry. |
| `list --resource KEY` | One exact resource, or `no current claims`. |
| `events` | Kind, resource set when present, and relative time. |
| `history --resource KEY` | Agent, status, acquire/end times, end reason, and operation kinds. |

`status` uses relative expiry; `status --full` adds complete non-secret metadata.
Lifecycle results show only fields relevant to the operation. Checkpoint reports
persisted bytes, transfer shows the successor handle and resources, and verify
keeps full unresolved operation IDs.

`history` without a resource aliases `events`. Text views omit cursors; page with
`--json` and `--cursor`. `watch` prints a cursor only as a runnable
`resume: worklease watch --cursor ...` command. A GC preview likewise ends with
the exact apply command.

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
| `doctor` | Run read-only local diagnostics, or verify a selected remote profile's transport, identity, credential, role, recovery state, and advertised prefixes. Use `--resource KEY` to check remote admission for one key. |
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

| Input error | Reason | Message |
| --- | --- | --- |
| No contextual claim for `status`, `verify`, `heartbeat`, `checkpoint`, or `release` | `claim-selection-missing` | `no contextual claim is available; run worklease acquire --path FILE` |
| `checkpoint` without `--data` or `--data-file` | `invalid-argument` | `checkpoint requires --data JSON or --data-file FILE; for example: worklease checkpoint --data '{"phase":"tests"}'` |

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

The standard binary uses the network only when you manage or select a remote
profile, or run `serve`. Remote failures never fall back to local.

The local journey uses the paths printed by state-producing commands:

```sh
worklease server init
worklease serve
worklease enroll --invite-file PATH_PRINTED_BY_INIT
worklease invite issue
worklease enroll --invite-file PATH_PRINTED_BY_INVITE_ISSUE
worklease acquire --resource coordination:demo
worklease list
worklease heartbeat
worklease release
```

The defaults are pinned TLS at `https://127.0.0.1:8443`, the
`coordination:` prefix, and owner-private XDG paths. A LAN setup adds
`--listen 0.0.0.0:8443 --endpoint https://HOST:8443 --confirm-non-loopback`.

Customize with setup flags first, then `WORKLEASE_SERVER_CONFIG`, then the
matching `server.yaml` keys. `--guided` is a no-op compatibility alias.
Cleartext additionally requires
`--transport http --acknowledge-cleartext-credentials`.

| Task | Commands |
| --- | --- |
| Select an authority | `profile add|list|show|remove|default|bind|unbind`, `--profile`, `--local` |
| Enroll a client | `invite issue`, `enroll` |
| Issue an invite with defaults | `invite issue` defaults to write/profile-label/15-minute expiry, writes an owner-private artifact, and prints its path plus exact enroll command without the bearer. |
| Administer access | `installation list|revoke`, `claim revoke` |
| Recover an authority | `server restore`, `recovery status|reopen` |
| Run, reset, or retire a server | `server init|bootstrap-reissue|reset|retire`, `serve` |

A selected profile applies to lifecycle, inspection, guarded operations, events,
watches, and GC. Remote authorities accept portable resources only; `path:`,
`backlog-md:`, and `markdown:` are rejected.

The service is one namespace and one SQLite writer on a single-host filesystem.
It does not provide HA, provider fencing, or exactly-once execution. See the
[remote authority guide](remote-claim-authority.md) for command forms,
enrollment, deployment, recovery evidence, and unsupported operations.
