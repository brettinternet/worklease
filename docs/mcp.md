# MCP and JSON orchestration

`worklease mcp` is a one-process stdio server. Stdout is protocol-only; logs and
diagnostics use stderr. It supports MCP `2026-07-28` discovery and the
`2025-11-25` initialize lifecycle for interoperability.

## Quick start

Configure a client with `worklease setup mcp --client claude-code|cursor` or use:

```json
{"mcpServers":{"worklease":{"command":"/absolute/path/to/worklease","args":["mcp"]}}}
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

MCP intentionally is not CLI parity. `exec`, `replace-file`, transfer, operation
inspection/reconciliation, history, garbage collection, doctor, setup, and
policy inspection are CLI-only. Use the CLI explicitly for those local operator
boundaries.

## Two isolated loops

Each MCP `acquire` may carry a stable `sessionId`. The server keeps a private
authority-bound handle under that selector and returns an opaque `lease`
reference; later lifecycle calls use the reference, not the session selector.

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"acquire","arguments":{"resources":["task:a"],"sessionId":"loop-a","agentId":"agent-a"}},"_meta":{"protocolVersion":"2026-07-28"}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"acquire","arguments":{"resources":["task:b"],"sessionId":"loop-b","agentId":"agent-b"}},"_meta":{"protocolVersion":"2026-07-28"}}
```

Capture each successful result's opaque `structuredContent.lease`. Subsequent
`status`, `heartbeat`, `checkpoint`, `verify`, and `release` tool calls pass
`{"lease":"REFERENCE"}` rather than a session selector. The sessions isolate
the private handle files; the lease references select them. If both loops
acquire `task:a`, the loser receives a structured `already-claimed` result with
safe holder/expiry details and can watch or select other work. No token appears
in results, errors, logs, checkpoints, or tool schemas.

## Renewal, cancellation, and recovery

The server can renew an active claim before half its TTL while a tool call is in
flight. Every MCP heartbeat, automatic or explicit, is capped by the original
absolute `maxHold` deadline. The server keeps reading stdin so cancellation and
EOF are prompt. Restarting does not auto-renew an old handle.

Mutations persist exact pending requests before dispatch. Retry by opaque lease
reference only when the outcome is uncertain and the error returns that
reference; this replays only the identical operation during its bounded window.
A definitive acquire failure removes the pending grant and returns no lease
reference, so retry with a fresh `acquire` request. Changed intent conflicts.
Started guarded effects are not exposed as MCP exec tools; advanced inspection,
cessation evidence, and reconciliation remain explicit CLI operations described
in [the claim model](claim-model.md).

MCP errors preserve the CLI's stable `reason`, `exitCode`, and `details` instead
of flattening contention or stale ownership into prose. Unknown methods, invalid
protocol transitions, duplicate in-flight IDs, and overload are protocol errors;
domain failures remain tool results.

## Security boundary

The local SQLite authority and handle directory are owner-private. Authority IDs
bind handles and cursors. Resource keys may be host-local. MCP does not discover
provider work, perform provider writes, or prove provider-side fencing. A client
must verify its authoritative provider checkpoint before release.

Native editor guards are optional and separate; see [setup](setup.md). The
deferred remote authority document is design evidence, not an available MCP
transport.
