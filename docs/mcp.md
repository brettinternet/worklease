# MCP and JSON orchestration

`worklease mcp` is a one-process stdio server. Stdout is protocol-only; logs and
diagnostics use stderr. It supports MCP `2026-07-28` discovery and the
`2025-11-25` initialize lifecycle for interoperability.

## Quick start

Configure a client with `worklease setup mcp --client claude-code|cursor` or use:

```json
{"mcpServers":{"worklease":{"command":"/absolute/path/to/worklease","args":["mcp"]}}}
```

The server inherits `WORKLEASE_SESSION_ID` from whatever launches it and uses it
as the default `acquire.sessionId`, so the launcher is the right place to set
session identity; the agent never has to copy it into a tool argument:

```json
{"mcpServers":{"worklease":{"command":"/absolute/path/to/worklease","args":["mcp"],"env":{"WORKLEASE_SESSION_ID":"${PI_SESSION_ID}"}}}}
```

```sh
WORKLEASE_SESSION_ID="$LOOP_RUN_ID" pi   # per loop run, inherited by MCP and CLI
```

Discover the modern server and list typed tools:

```text
{"jsonrpc":"2.0","id":1,"method":"server/discover"}
{"jsonrpc":"2.0","id":2,"method":"tools/list","_meta":{"protocolVersion":"2026-07-28"}}
```

Legacy clients send `initialize` with protocol version `2025-11-25`, then
`notifications/initialized`, then `tools/list`.

## Tool boundary

The eleven tools are `key`, `acquire`, `status`, `list`, `heartbeat`,
`checkpoint`, `release`, `verify`, `watch`, `events`, and `instructions`.
Schemas reject unknown inputs and return schema-version 2 domain envelopes.

The `instructions` tool accepts `topic: setup|remote|server|loop|safety` and
returns the same guidance as the CLI. These are read-only instructions, not MCP
tools for enrollment or server administration. `server` describes the remote
claim authority, not this stdio server.

MCP intentionally is not CLI parity. `exec`, `replace-file`, transfer, operation
inspection/reconciliation, history, garbage collection, doctor, setup, and
policy inspection are CLI-only. Use the CLI explicitly for those local operator
boundaries.

## Two isolated loops

Each MCP `acquire` may carry a stable `sessionId`; it defaults to
`WORKLEASE_SESSION_ID`, then a fresh value per acquire. The server keeps a
private authority-bound handle under that selector and returns an opaque `lease`
reference; later lifecycle calls use the reference, not the session selector.

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"acquire","arguments":{"resources":["task:a"],"sessionId":"loop-a","agentId":"agent-a"}},"_meta":{"protocolVersion":"2026-07-28"}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"acquire","arguments":{"resources":["task:b"],"sessionId":"loop-b","agentId":"agent-b"}},"_meta":{"protocolVersion":"2026-07-28"}}
```

Use the returned reference for the rest of the lifecycle:

| Step | Input or output |
| --- | --- |
| `acquire` | Capture `structuredContent.lease`. |
| `status`, `heartbeat`, `checkpoint`, `verify`, `release` | Pass `{"lease":"REFERENCE"}`. |

Sessions isolate private handles; lease references select them. Contention
returns structured `already-claimed` holder/expiry details. Tokens never appear
in results, errors, logs, checkpoints, or schemas.

## Renewal, cancellation, and recovery

The server can renew an active claim before half its TTL while a tool call is in
flight. Every MCP heartbeat, automatic or explicit, is capped by the original
absolute `maxHold` deadline.

The server keeps reading stdin so cancellation and EOF are prompt. Restarting
does not auto-renew an old handle.

Mutations save exact pending requests before dispatch:

- If an uncertain-outcome error returns a lease reference, retry with that
  reference. Only the identical operation replays within its bounded window.
- If acquire fails definitively, no lease reference remains; submit a fresh
  `acquire`.
- Changed intent conflicts.

MCP exposes no guarded exec tools. Use the CLI for inspection, cessation
evidence, and reconciliation; see [the claim model](claim-model.md).

MCP errors preserve the CLI's stable `reason`, `exitCode`, and `details` instead
of flattening contention or stale ownership into prose. Unknown methods, invalid
protocol transitions, duplicate in-flight IDs, and overload are protocol errors;
domain failures remain tool results.

## Security boundary

The local SQLite authority and handle directory are owner-private. Authority IDs
bind handles and cursors. Resource keys may be host-local. MCP does not discover
provider work, perform provider writes, or prove provider-side fencing. A client
must verify its authoritative provider checkpoint before release.

### Experimental remote profile

The experimental remote authority is never an MCP endpoint. `worklease mcp`
stays local and uses HTTPS only when a profile is selected. Credentials,
handles, and pending requests remain on the client host.

| Selection | Behavior |
| --- | --- |
| `--profile NAME`, environment, binding, or default | Use the selected remote profile. |
| `--local` | Use local SQLite; conflicts with remote selection. |
| No selected profile | Make no network request; local reads remain setup-free. |

The same eleven tools remain available. Remote acquire accepts only configured
portable prefixes; `path`, `backlog-md`, and `markdown` keys are rejected.
`wait` is a client loop capped at 60 seconds, while the server owns polling.

Enrollment, administration, transfer, exec, replacement, reconciliation, history,
profiles, and recovery remain outside MCP. Enroll with:

```text
worklease enroll --profile NAME (--invite-file FILE|--invite-fd N) [--label TEXT]
```

Remote failures retain stable reasons such as `authentication-required`,
`installation-revoked`, `already-claimed`, `authority-restored`,
`unknown-outcome`, and `operation-kind-unsupported`; they are not flattened to
prose. See the [experimental remote authority guide](remote-claim-authority.md)
for hosted deployment, recovery, and unsupported boundaries.

Native editor guards are optional and separate; see [setup](setup.md).
