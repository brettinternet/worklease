# Local MCP server

The MCP interface is optional and local-only. Install a tagged release with its
extra, for example:

```sh
uv tool install 'worklease[mcp] @ git+https://github.com/brettinternet/worklease@v0.9.0'
```

A Claude Code configuration can start it over stdio:

```json
{
  "mcpServers": {
    "worklease": {
      "command": "worklease-mcp",
      "args": [],
      "env": {"WORKLEASE_AGENT_ID": "claude-code"}
    }
  }
}
```

The server exposes exactly `key`, `acquire`, `status`, `list`, `heartbeat`,
`checkpoint`, and `release`. It has no HTTP transport and never launches the
CLI. `acquire` and `status` take an ordered `resources` list (one to 32
members); one member uses the singleton API and multiple members use the
bundle API. `wait_timeout` is capped at 60 seconds and is intended only as a
short contention backoff: select another ready item instead of blocking an
agent for a long time.

## Safe lifecycle

1. Resolve the provider key with `key` and inspect its authoritative eligibility.
2. Call `acquire` before delegation or edits. Save the returned opaque `lease`
   reference; do not copy a lease file or attempt to inspect it.
3. Keep provider progress authoritative. The server renews automatically before
   half the TTL by default, but `max_hold` (four hours by default) bounds a
   forgotten live session. A result reports `autoHeartbeat` as `active`,
   `stopped`, or `disabled`.
4. Call `checkpoint` for bounded local recovery metadata after verifying that
   it contains no credentials. Call `status` before durable work and release.
5. Verify provider state, then call `release` with an audit reason.

References are capabilities, not identities. Handles are private mode `0600`
files in `$WORKLEASE_HOME/mcp-leases/`, in a mode `0700` directory. The server
uses the same home precedence as the CLI: `--home`, `WORKLEASE_HOME`,
`XDG_STATE_HOME/worklease`, then `~/.local/state/worklease`. Tokens and
revisions stay in the handle and are never in structured results, text, logs,
or checkpoints. The filename is the returned reference plus `.lease`.

The guarantee covers only cooperating callers using the same authority and
exact resource. Only guarded local operations are fenced; coordination-only
claims, provider writes, and other external effects are not provider-fenced.
The PyInstaller/native `worklease` binary remains CLI-only; install the Python
package with the `mcp` extra to run the server.

## Recovery matrix

| Situation | Operator action |
| --- | --- |
| Client disconnects (singleton) | Reconnect with the saved reference; if unavailable, run `worklease status --verbose --resource R`, then `worklease release --lease-file "$WORKLEASE_HOME/mcp-leases/<lease-reference>.lease" --reason 'operator recovery'`. |
| Client disconnects (bundle) | Check every member with `worklease status --verbose --resource R` and `R2`; release atomically with `worklease release-bundle --lease-file "$WORKLEASE_HOME/mcp-leases/<lease-reference>.lease" --reason 'operator recovery'`. |
| Server restarts | Present the saved reference to `heartbeat`, `checkpoint`, or `release`; never adopt by agent identity. Use `worklease release` for a singleton handle or `worklease release-bundle` for a bundle handle. |
| Stale revision or `stale-claim` | Stop mutating, run singleton `worklease status --verbose --resource R` or bundle `worklease status-bundle --resource R --resource R2`, then acquire a fresh reference; use the matching `release`/`release-bundle --lease-file` command for final release. |
| Expiry | Stop work, run the matching singleton `status --verbose` or bundle `status-bundle`, and acquire a fresh reference; release with the matching CLI lease-file command only if the claim is still current. |
| Unknown outcome | Inspect provider state. Use `worklease inspect-operation --resource R --operation-id ID` (or `inspect-operation-bundle` with every `--resource`) and then the matching `worklease reconcile-operation --lease-file ...` or `reconcile-operation-bundle --lease-file ...`; do not blindly replay. |

For operator recovery, release directly with the persisted handle path:

```sh
# Singleton handle
worklease release \
  --lease-file "$WORKLEASE_HOME/mcp-leases/<lease-reference>.lease" \
  --reason "operator recovery after MCP disconnect"

# Bundle handle (all members are released atomically)
worklease release-bundle \
  --lease-file "$WORKLEASE_HOME/mcp-leases/<lease-reference>.lease" \
  --reason "operator recovery after MCP disconnect"
```

MCP intentionally excludes execution, file replacement, transfer, history,
garbage collection, operation inspection/reconciliation, policy and provider
discovery or writes, dependency scheduling, and every HTTP transport. Use the
canonical CLI and its `--json --lease-file` mode for those operations and for
unknown-outcome reconciliation.

The benchmark can be run repeatedly as a context-cost and process-count
comparison (it makes no performance promise):

```sh
uv run python benchmarks/mcp_lifecycle.py
```
