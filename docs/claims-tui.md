# Claims TUI

Run bare `worklease` with stdin and stdout attached to a terminal. It opens the
selected authority's public claims without creating configuration or a local
store merely for browsing. Non-interactive invocation prints help instead.

Use `j`/`k` or arrows to select, Enter to open details, and Tab to switch between
Summary and History. Click to select, click again to open, and scroll with the
mouse wheel. `/` filters resource prefixes; `m`, `e`, and `s` toggle mine,
expiring, and stale filters. `r` refreshes, `?` shows help, and `q` quits.

A `*` marks a matching local contextual or MCP private handle, not merely a
matching holder identity. `u` previews renewal with the default TTL. `R` asks
for a release reason, then previews release. Enter confirms; Escape cancels.
The dispatch rechecks handle, claim revision, authority, restore identity,
expiry, and supervisor ownership. Pending handles must be recovered through
the CLI; supervised claims are managed through `worklease runs stop`.

Summary's Contact row lists where to reach the holder on this host, looked up
from the claim's session and agent each time details open, within 100 ms:
its Herdr pane and, for `claude` holders, its Claude Code peer name, each with
working, idle, or waiting status. A source that cannot answer shows why, for
example `herdr not installed`. Claims record no host, so a holder elsewhere
shows `none on this host`. Contact is never recorded, and the TUI never sends
messages. The Claude Code lookup reads only `sessionId`, `name`, `status`,
`kind`, and `peerProtocol` from `~/.claude/sessions` (or
`$CLAUDE_CONFIG_DIR/sessions`).

Local History shows retained public lifecycle events and operation states,
never credentials or checkpoint contents. Remote authorities expose only
public list fields, not other sessions' history. Poll failures retain the
last snapshot as stale and disable mutations until refreshed.

Task discovery, readiness, and provider mutations belong to the caller's task
manager. Worklease coordinates claims only.
