# CLI zero-flag audit

This table is the checked-in contract for every leaf command. A blank option
means the command either performs a safe contextual read, uses its documented
local default, or reports the missing required input without guessing.

| Command | Zero-flag behavior | Safety / guidance |
| --- | --- | --- |
| `version` | Print build version | Read-only |
| `key` | Report missing resource input | No mutation |
| `acquire` | Report missing resource input | Acquire requires an explicit resource |
| `status` | Show current contextual claim | If none, acquire first |
| `list` | List current claims | Says `no current claims` when empty |
| `heartbeat` | Use contextual handle and default TTL | Acquire first if no claim |
| `checkpoint` | Report missing data and claim selection | Supply `--data` or `--data-file`; acquire first |
| `release` | Use contextual handle and default reason | Acquire first if no claim |
| `transfer` | Report missing successor handle | Requires explicit successor destination |
| `verify` | Verify contextual claim | Acquire first if no claim |
| `exec` | Report missing child command | Requires `-- COMMAND` |
| `replace-file` | Report missing path/content | Requires explicit replacement inputs |
| `history` | Show global lifecycle history | Read-only |
| `events` | Show lifecycle events | Read-only |
| `watch` | Report missing wait condition/resource or cursor | Requires explicit wait target |
| `gc` | Preview retention | Never applies without `--apply` |
| `doctor` | Run read-only diagnostics | No state mutation |
| `queue init` | Preflight the detected adapter and write owner-private queue configuration | `--dry-run` previews without writing; Backlog.md `me` falls back to OS login and network effects require `--allow-git-network` |
| `queue claims` | Open the authority-wide Claims TUI | Read-only public claim list and event feed; independent of `queue.yaml`; no renew or release actions |
| `queue query` | Report missing required view | Requires a configured view; read-only |
| `queue next` | Report missing required view | Requires a configured view and complete scope; selects without acquiring |
| `queue recovery retry` | Reject missing operation ID or original handle | Reads the provider result without dispatching another write |
| `queue adapter approve` | Reject missing explicit source | Previews approval only; records it only with `--acknowledge` |
| `queue adapter check` | Reject missing explicit executable | Runs only the named executable; writes a disposable provider item only with `--disposable-target` |
| `queue adapter protocol` | Report the host's supported majors without starting an adapter | `--manifest-file FILE` also reports the saved manifest's range and compatibility; never approves or starts it |
| `queue recovery reconcile` | Reject missing operation ID, evidence, or attestations | Requires proof the provider write did not commit and the executor ceased; claim remains held |
| `queue recovery checkpoint-missing` | Reject missing operation ID, original handle, evidence, or attestations | Requires verified provider effect, absent expired checkpoint, and ceased executor; claim remains held |
| `queue authority-id` | Show the invoking worker's selected authority ID | Read-only; launchers compare it to the queue handoff before acquiring |
| `queue identity confirm` | Reject missing view, source, or acknowledgement | Confirm only after stopping old workers and resolving old claims and operations |
| `policy list` | List built-in policies | Read-only |
| `policy describe` | Report missing policy name | Read-only |
| `op inspect` | Inspect contextual operation | Read-only; acquire first for private context |
| `op reconcile` | Report missing reconciliation inputs | Requires explicit evidence and target |
| `handle inspect` | Inspect the selected contextual handle offline | Read-only; does not contact an authority or create missing state |
| `handle archive` | Archive the selected contextual handle offline | Pending/recovery state requires explicit acknowledgement; never mutates an authority |
| `instructions setup` | Print setup and verification guidance | Read-only |
| `instructions remote` | Print guidance for joining an existing authority | Read-only |
| `instructions server` | Print claim-authority hosting guidance (not MCP) | Read-only |
| `instructions loop` | Print lifecycle guidance | Read-only |
| `instructions safety` | Print safety guidance | Read-only |
| `setup mcp` | Preview integration changes | Writes only with `--apply` |
| `setup guard` | Preview guard changes | Writes only with `--apply` |
| `setup instructions` | Print managed instructions | Read-only |
| `profile add` | Report missing profile name/trust inputs | Performs bounded metadata discovery only when inputs are supplied |
| `profile list` | List trusted profiles | Setup-free and local |
| `profile show` | Show the selected contextual/default profile | Pass a name only to override profile selection |
| `profile remove` | Report missing profile name | Destructive only with explicit name |
| `profile default` | Show the current default profile | Changes default only with an explicit name |
| `profile bind` | Report missing profile name | Binds current checkout only with explicit name |
| `profile unbind` | Remove current checkout binding | Explicitly scoped to current checkout |
| `enroll` | Prompt privately in a terminal; otherwise report missing invite | Bearer never enters argv/output |
| `invite issue` | Issue write invite for selected profile; owner-private default artifact, profile label, 15-minute expiry | Prints artifact path and exact enroll command, never bearer |
| `installation list` | List installations for selected remote profile | Read-only |
| `installation revoke` | Report missing installation ID | Destructive only with explicit ID |
| `claim revoke` | Report missing claim ID | Destructive only with explicit ID |
| `recovery status` | Show remote recovery state | Read-only |
| `recovery reopen` | Report missing attestation | Requires explicit private evidence |
| `server init` | Secure TLS loopback at `127.0.0.1:8443`, HTTPS endpoint, generated pin, coordination namespace, XDG paths | `--guided` is only an alias; LAN requires endpoint and consent; HTTP requires acknowledgement |
| `server restore` | Report missing backup/recovery inputs | Destructive state replacement requires explicit inputs |
| `server bootstrap-reissue` | Resolve server config/home and default bootstrap artifact | Names the resolved authority on errors |
| `server reset` | Resolve server config/home and default bootstrap artifact, then refuse mutation without `--confirm-reset` | Always refuses active claims; unresolved operations require an external redacted export |
| `server retire` | Resolve server config/home, name it, and refuse mutation without `--confirm-retire` | Explicitly destructive; output names resolved home |
| `serve` | Resolve and serve configured authority | TLS by default; HTTP requires explicit allowance |
| `mcp` | Serve MCP over stdio | Network only when selected by profile |
| `help` | Show command help | Read-only |
