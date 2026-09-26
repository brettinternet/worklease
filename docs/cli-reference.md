# CLI reference

Run `worklease COMMAND --help` or read `worklease(1)` for every flag and
example. Bare `worklease` opens the queue TUI only when both stdin and stdout
are terminals. With a pipe or redirected output it prints root help and exits
successfully; `worklease help` and `worklease -h` always print help. A configured
queue opens its first view even if a source is down. Without `queue.yaml`, it
opens Claims without creating authority or config state; run `worklease queue init`
to add sources. An invalid config is reported just as with `worklease queue`.
The empty Claims tab suggests `worklease acquire --path README.md`, `?` for help,
and `q` to quit.

`worklease help --all` prints the root help followed by every command and
subcommand once, in tree order, without touching any state. It is the one-shot
onboarding read for people and agents.

Top-level help groups commands into *Claim lifecycle*, *Inspection and recovery*,
and *Setup and administration*.

Usage lines expose required inputs and alternate forms:

```text
worklease exec [selection] ... -- COMMAND [ARGS...]
worklease policy describe NAME
worklease history [--resource RESOURCE]
worklease queue [--view NAME] init [--checkout PATH] [--adapter backlog-md|github] [--source-id ID] [--authority NAME] [--portable-claims SOURCE] [--me PRINCIPAL] [--allow-git-network] [--dry-run] [--json]
worklease queue query --view NAME [--json] [--limit N] [--cursor CURSOR] [--max-age DURATION] [--require-complete]
worklease queue adapter check --executable PATH [--adapter-config JSON | --adapter-config-file FILE] [--disposable-target ITEM] [--cancel-marker PATH] [--json]
worklease queue authority-id --json
worklease queue --view NAME identity confirm --source SOURCE --acknowledge
```

`[selection]` means the shared claim-selection options listed in that command's
help. Help shows runtime defaults, including `--ttl` 15m, `--max-duration` 1h,
`--limit` 50, `--timeout` 30s, and `--retention-days` 30.

## Global interface

```text
worklease [--json] [--home PATH] [--config PATH] COMMAND
```

Configuration precedence is flags, environment, YAML, then defaults.

Important environment variables are `WORKLEASE_HOME`, `WORKLEASE_CONFIG`,
`WORKLEASE_AGENT_ID`, and `WORKLEASE_SESSION_ID`.

For contextual handles, an explicit `--session` overrides
`WORKLEASE_SESSION_ID`. When neither resolves to a value, the selector is empty
and human output renders it as `"" (unscoped)`. `unscoped` is a display label,
not a literal selector.

Text output is for humans; `--json` emits one schema-version 2 envelope. `worklease queue query --view NAME --json` places its queue schema v1 projection in the `query` field; see [queue configuration and query schema](queue.md#read-only-query).

Bearer credentials are accepted only through a private contextual/explicit
handle, `--token-file`, or `--token-fd`. An argv `--token` option is deliberately
unsupported. Acquire persists a client-generated credential before dispatch and
never returns it in text or JSON.

## External adapter conformance

`queue adapter check` launches only the explicitly named canonical absolute executable through the production supervised host. It snapshots its bytes but never reads/writes queue.yaml, adapter approvals, the queue index, or Worklease authority. The configuration is a JSON object supplied inline with `--adapter-config` or in a file with `--adapter-config-file`; it must satisfy the negotiated manifest schema. The process has user privileges, not a sandbox. Inspect it before running. Without `--disposable-target ITEM` no mutation is dispatched. With the flag, the checker may append a marked progress note **to that existing disposable item**; do not point it at real work. A read-only adapter does not write even when a target is supplied.

`--json` uses the usual schema-version 2 envelope, operation `queue-adapter-check`. Successful results contain `verdict: "pass"`, a bounded manifest summary (`id`, `version`, `protocolMajor`), and `checks: [{id,status,reason,detail}]`. `status` is `pass`, `fail`, or `skip`; skips include a reason and do not count as passes. Details never contain provider payloads or stderr. A conformance failure returns `ok: false`, `error.reason: "adapter-conformance-failed"`, `error.exitCode: 65`, and `error.details.verdict` and `error.details.checks` in the same shape. An invalid flag, unreadable executable, or configuration error exits 64 (`invalid-argument`); success exits 0. Output is one JSON document per invocation. Human text renders the same check list.

The checker exercises negotiation, configuration, source binding, capability/list/read/dependency semantics, continuation and budget, static resource-policy inputs, host frame/collection/deadline guards, and (only with an explicit target and declared supported mutation) a receipt and read-back without redispatch. The host tests crash restart and a redaction sentinel. To check adapter cancellation, configure a disposable fixture that blocks a `list` query with text `__worklease_conformance_cancel__`, writes `PATH.request` when it begins, handles `$/cancelRequest`, and writes `PATH.done` after aborting. Pass a fresh `--cancel-marker PATH` and the corresponding fixture config. Without that marker the check is `skip`, **not** certification of cancellation behavior. The checker only reads the marker files; the adapter creates them. Check those failure modes in your adapter's own tests before shipping.

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
options.

Generation and completion requests do not inspect or mutate claim state. The
framework's completion protocol remains hidden from suggestions. Unsupported
shell names fail with an `invalid-argument` error listing the three supported
shells.

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

## Work queue

`worklease queue [--view NAME] [--high-contrast]` (or `worklease q [-v NAME]`)
opens the configured view from the owner's private `queue.yaml` in a Bubble Tea
terminal. `--high-contrast` replaces faint text and color with bold, underline,
and reverse video; `NO_COLOR` removes styling entirely.
Root-level `worklease -v` still shows the version. The queue shows source
coverage, readiness, assignment, native occupancy, authority-scoped Worklease
claims, and lazy claim history. `j`/`k` move, Enter opens detail, Tab changes detail tabs, `/` filters
loaded rows, `r` refreshes, `o` opens a GitHub issue URL explicitly, `c` previews
Claim for me, and `x` previews configured launch actions. Launch shows argv, cwd,
environment variable names, and authority; Enter starts the selected process,
not a coordinated worker. The worker acquires its own claim; the queue observes
it only after it appears in the claim overlay. Assignment, progress, and state
writes remain unavailable. `worklease queue authority-id --json` exposes the
invoking worker's selected authority ID for launchers to compare before acquire.
`queue init` writes owner-private configuration directly; `--dry-run` previews facts, origins, and exact YAML without writing. Backlog.md identity defaults to `@` plus the OS login if no single default assignee exists. Init preflights the real adapter, including Backlog.md CLI 1.52.x and explicit `--allow-git-network` consent for project Git network effects. Source setup and configured views are described in `docs/queue.md`.

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

Use `--session NAME` for each concurrent loop.

Without an explicit handle, Worklease selects an authority-bound contextual
handle by Git worktree root (or resolved current directory), contextual handle
selector, and authority ID. The selector resolves from `--session`, then
`WORKLEASE_SESSION_ID`, then the empty unscoped value.

Profiles for the same authority share the slot even if their name, endpoint,
credential path, or restore ID changes. Switching to another authority selects an
independent slot; switching back resumes the matching handle.

A handle is convenience state, not the claim or an authoritative provider
checkpoint.

The selector and claim metadata are distinct. `--session` chooses a contextual
handle, while the claim's `sessionId` identifies that ownership epoch. With an
explicit selector the two values may coincide, but they retain those separate
roles.

When acquire uses the unscoped slot it still generates a non-empty claim
`sessionId`; that generated value cannot select or recover the unscoped handle.
Explicit `--handle`, `WORKLEASE_HANDLE`, stateless credentials, and MCP lease
references keep their existing path selection and are not remapped.

Re-running remote `acquire` on a ready contextual handle replaces it only after
the authority confirms the old claim is inactive.

Worklease stages a fresh claim ID, credential, and the current acquire inputs as
a pending exact request, then publishes ready state only after validating the
grant. Active claims, status failures, authority mismatches, and concurrent
handle changes fail closed.

An existing pending request is replayed exactly instead. Changing `--session`
selects another handle and does not recover an uncertain request.

Legacy root-and-session handles migrate only when their embedded authority ID
matches. Mismatches remain recoverable at the legacy path. A legacy/scoped path
conflict changes neither file and reports explicit `--handle` recovery choices.

One claim covers all `--resource` values atomically. Resources contend by exact
bytes and are never silently normalized. Contextual handle selectors only
separate private handles: another selector cannot bypass contention on the same
resource.

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
Human lifecycle output labels claim metadata as `claimSessionId`; JSON retains
the stable `sessionId` field. Neither is presented as a contextual handle
selector.

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
| `handle inspect` | Validate and inspect a selected or explicit private handle offline. |
| `handle archive` | Preserve a selected or explicit private handle aside without authority mutation. |
| `gc` | Preview or apply contiguous-prefix retention. |

`handle inspect` reports only redacted authority, claim, selector, locally
recorded lifecycle, resource, and recovery metadata. It does not contact an
authority or create a missing handle/store. Explicit paths report selector
provenance as unknown, and recorded expiry does not prove inactivity.

`handle archive` uses owner-private durable no-overwrite storage and prints the
archive path plus an explicit-handle inspection command. It never releases,
revokes, or contacts the authority, so the claim may remain active.

Pending or recovery state requires `--acknowledge-pending-recovery`; refusal
leaves the source untouched. Use `--destination PATH` only with an existing
owner-private directory; the contextual handle directory is rejected as a
destination.

Archives are not contextual slots and are never selected automatically.

Guarded-operation guarantees:

- `exec` reports `local-coordination`, not provider fencing.
- `replace-file` reads and digests at most 16 MiB before its transaction. Only a
  successful expected-hash replacement reports `local-serialized-replace`.
- A started operation blocks another until outcome and process cessation are
  established.
- Replay requires the same operation ID and normalized request within the
  recovery window; changed intent is rejected.

### Guarded exec and takeover example

The normal path claims a resource, runs a supervised command, records its
completion, and releases the claim:

```sh
worklease acquire --resource coordination:deploy --session worker-a
worklease exec --session worker-a -- ./deploy.sh
worklease release --session worker-a --reason done
```

`exec` records the operation as started before spawning the child. While it
runs, Worklease renews the claim and supervises the local process group. A
normal completion records the argv, execution directory, exit code, and bounded
stdout and stderr.

If worker A disappears after the operation starts, the claim eventually
expires but the operation remains unresolved. Worker B can acquire the same
resource and inspect the operation reported in `unknownOperations`:

```sh
worklease --json acquire \
  --resource coordination:deploy \
  --session worker-b
worklease --json op inspect --operation-id OPERATION_ID
```

Worker B owns the resource, but another guarded command remains blocked because
Worklease cannot tell whether `deploy.sh` did nothing, partly ran, or completed
without recording its receipt. Before continuing, the worker must inspect the
actual target system and establish that the old executor and any asynchronous
effect have stopped.

After determining the outcome, worker B records that evidence against the exact
predecessor operation:

```sh
worklease op reconcile \
  --session worker-b \
  --target-claim-id OLD_CLAIM_ID \
  --target-operation-id OPERATION_ID \
  --expected-request-sha256 REQUEST_HASH \
  --outcome observed-success \
  --evidence '{"outcome":"observed-success","executorStopped":true,"checked":"deployment API"}'
```

Use `observed-failure` only when inspection establishes failure. The claim ID,
operation ID, and request hash are available in the structured acquire and
inspection results. Reconciliation does not discover the outcome; it records
the operator's independently established outcome and cessation evidence. Once
it succeeds, guarded work can continue:

```sh
worklease exec --session worker-b -- ./next-step.sh
worklease release --session worker-b --reason done
```

For a guarded local file update, claim the exact path and require its current
content hash. The replacement fails without changing the file if another writer
changed it first:

```sh
worklease acquire --path config.json --session config-update
worklease replace-file \
  --session config-update \
  --path config.json \
  --expected-sha256 CURRENT_SHA256 \
  --content-file new-config.json
worklease release --session config-update --reason done
```

Worklease is not a complete activity log. Public history contains redacted
lifecycle metadata, completed exec output is bounded, and a started operation
may have no completion receipt. Use checkpoints, commits, and the selected work
provider for human or agent handoff context.

## Policy and administration

| Command | Purpose |
| --- | --- |
| `policy list` / `policy describe` | Inspect static built-in policies. |
| `doctor` | Run read-only local diagnostics, or verify a selected remote profile's transport, identity, credential, role, recovery state, and advertised prefixes. Use `--resource KEY` to check remote admission for one key. |
| `instructions [setup|remote|server|loop|safety]` | List topics, or print version-matched agent guidance: setup and verification, joining a remote authority, hosting an authority (not MCP), lifecycle, or safety. Instruction topics support `--json`; the bare topic directory is text help. |
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
{"schemaVersion":2,"ok":false,"operation":"acquire","error":{"reason":"already-claimed","exitCode":2,"message":"resource is already claimed","details":{"resource":"coordination:busy","requestClaimId":"…attempted…","operationId":"…operation…","holder":{"claimId":"…holder…","agentId":"agent","workKey":"work","expiresAt":"2026-09-12T00:00:00.000000Z"},"commitState":"not-committed"}}}
```

Exit families are:

- `0` success; `1` internal failure.
- `2` ownership/contention; `3` ledger, replay, or ambiguous outcome.
- `64` invalid input/configuration; `75`
authority or storage failure.
- `124` child timeout; `130` interruption.

Domain details include safe holder or recovery metadata. They never include
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
Concurrent loops need distinct contextual handle selectors even in one checkout.
The selector is not part of a resource key and does not create an independent
resource-lock namespace.

Stable explicit-selector lifecycle:

```sh
worklease acquire --session loop-a --resource coordination:demo
worklease heartbeat --session loop-a
worklease status --session loop-a
worklease release --session loop-a --reason done
```

`WORKLEASE_SESSION_ID` supplies the selector when the flag is absent, and an
explicit flag wins:

```sh
export WORKLEASE_SESSION_ID=loop-a
worklease status                       # selects loop-a
worklease status --session loop-b      # selects loop-b instead
worklease doctor --session loop-b      # diagnoses the explicit override
```

Any unique string works as the selector, so the process that knows the identity
should export it once and let every `worklease` and `worklease mcp` child
inherit it:

```sh
WORKLEASE_SESSION_ID="$LOOP_RUN_ID" pi   # one loop run
WORKLEASE_SESSION_ID=jane pi             # any unique name
```

Two selectors still contend on one exact resource:

```sh
worklease acquire --session loop-a --resource coordination:shared
worklease acquire --session loop-b --resource coordination:shared # already claimed
```

Authority selection is another independent part of the contextual slot.

Selection precedence is `--profile`, `WORKLEASE_PROFILE`, checkout binding,
configured user default, then implicit local.

The exact case-sensitive name `local` is a built-in selection at every layer and
never a persisted remote profile.

`profile default local` records an explicit local user default without changing
bindings. `profile bind local` (alias `profile use local`) overrides the remote
default for that checkout. `profile unbind` (alias `profile unuse`) removes that
override so normal fallback resumes.

`profile default` reports configured default versus unset, while `profile show`
reports the effective selection and source. `profile list` always shows `local`
separately from remote profiles, and `profile show local` inspects it without
network access.

A remote profile named `local` from an older store is a compatibility collision;
selection fails closed with migration guidance.

Manually rename that remote profile and its default/checkout-binding references
while retaining the existing credential path. `local` cannot be added, enrolled,
or removed as a remote profile.

Remote-only administration commands fail clearly when local is selected.
`--local` remains the forced, network-free bypass of bindings, defaults, and
profile-store loading. It conflicts with any nonempty `--profile` or
`WORKLEASE_PROFILE`, including `local`.

A profile switch selects the slot for that authority, and switching back resumes
the original authority's slot:

```sh
worklease --profile team acquire --session loop-a --resource coordination:team
worklease --local acquire --session loop-a --resource coordination:local
worklease --profile team status --session loop-a # resumes the team-authority slot
```

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
| Select an authority | `profile add|list|show|remove|default|bind|unbind` (`use`/`unuse` alias checkout binding), `--profile`, `--local` |
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
