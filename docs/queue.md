# Work queue configuration

The read-only queue uses owner-private `$XDG_CONFIG_HOME/worklease/queue.yaml` (or `~/.config/worklease/queue.yaml` when XDG_CONFIG_HOME is unset). Create the `worklease` directory owner-private (`0700`) and the file owner-private (`0600`). Symlinks and files owned by another user are rejected. No repository configuration is read. The queue reads trusted remote authority names from the sibling `profiles.yaml`; `local` selects the built-in local authority.

```yaml
version: 1
me:
  github.com: brett
  backlog-md: ["@brett"]
sources:
  - id: worklease
    adapter: backlog-md
    checkout: ~/dev/me/worklease
    allowGitNetwork: false
    claims:
      policy: generic
      source: brettinternet/worklease/backlog
  - id: acme-api
    adapter: github
    host: github.com
    repository: acme/api
    account: brett
views:
  - name: Ready
    authority: local
    sources: [worklease, acme-api]
    filter:
      readiness: ready
      claim: free
      assigned: [me, nobody]
```

`checkout` must exist and `~` expands from HOME. Omit `claims` for host-local Backlog.md keys; a portable `generic` source must be agreed by all claimants before use. `allowGitNetwork` defaults to false. GitHub repositories use `owner/repo` and require an explicit host and account. View authorities must be `local` or a name in the trusted `profiles.yaml`; source IDs must be defined above. Filter keys are limited to `readiness`, `claim`, and `assigned`. Missing configuration is reported as `no-sources-configured` with this setup guidance.

## Launch handoffs

Owner-private `queue.yaml` can define named launch actions (unique names, non-empty argv, optional cwd and passEnv):

```yaml
launch:
  - name: worker
    argv: [/usr/local/bin/start-worker, --ref, "{ref}"]
    cwd: "{checkout}"
    passEnv: [GH_TOKEN] # only when the worker actually needs it
```

Only `{ref}`, `{sourceId}`, `{itemId}`, and `{checkout}` substitute, within individual argv elements or cwd. Unknown placeholders and keys are errors. `{checkout}` is available for configured local checkouts; API-only sources need an explicit absolute cwd, and an unavailable placeholder disables the action with `unresolved-placeholder`. Execution starts the configured binary directly, without a shell. **Argv substitution prevents shell expansion, not the target program's option parsing**: use explicit value arguments (`--ref={ref}` when supported) or terminate options (`-- "{itemId}"`, according to that program's syntax). An item ID beginning with `-` stays one argument; the launcher must handle it safely.

The child receives only existing `PATH`, `HOME`, `USER`, `LOGNAME`, `SHELL`, `TERM`, `LANG`, `LC_*`, `TMPDIR`, and `XDG_*_HOME`, explicitly named `passEnv` variables, plus `WORKLEASE_PROFILE`, `WORKLEASE_QUEUE_AUTHORITY_ID`, `WORKLEASE_QUEUE_REF`, and `WORKLEASE_QUEUE_RESOURCES` (JSON array of exact claim resources, including required retired binding keys). `GH_TOKEN` and other secrets are excluded unless explicitly passed. Titles, bodies, and session IDs are not supplied; the launched worker creates its own session, checks the authority ID, and acquires the exact resource set. The reference `scripts/queue-launch-worker.py` compares `worklease queue authority-id --json` before acquire and verifies exact keys through the worker's private handle. If the worker uses a non-default `WORKLEASE_HOME`, add it to `passEnv`. This is a **trusted executable** with the user's privileges, not a sandbox. A successful process start is not a claim or proof of coordination. The queue cannot ensure an arbitrary launcher consumes this handoff. Press `x` to inspect the action picker (argv, cwd, environment variable names and authority); Enter starts the action only after a fresh identity/claim gate. Launch is unavailable on authority mismatch or while the queue holds a claim; releasing then launching is two steps, and another worker can acquire in between. The queue observes a worker claim only after it appears in the overlay and never renews or releases it.

## Claim identity and migration

The first use of a source, and every subsequent change to its configured claim inputs (including a portable Backlog.md binding, repository locator, or view authority), requires explicit confirmation before claim actions become available. Queue reads remain read-only: confirmation alone writes the owner-private `queue-identities.json` beside `queue.yaml`, not the backing provider. A missing, unsafe, or mismatched identity record fails closed. This record is **not** part of the disposable queue index; keep it when clearing the cache. No alias is introduced for the old key.

Before confirming: stop workers using the old domain; resolve old claims and pending operations; update CLI, skill, and launch callers to derive exactly the configured policy/source/item; update `queue.yaml` to the new locator. Verify the view authority is shared by all claimants. The queue checks old keys in that authority and refuses confirmation while any is active or unknown, but **cannot detect claims in other authorities or prove that old workers and operations have stopped**. The acknowledgement is an operator assertion of those external checks:

```sh
worklease queue --view Ready identity confirm --source worklease --acknowledge
```

The confirmation re-reads the entire source, rejects duplicate task IDs and partial lists, checks old-key claims, and stores the configured claim inputs, observed IDs, and retired claim domains. The pre-acquisition gate must re-read identity and atomically acquire the configured key with all retired item keys admitted by the same authority. A retired host-local key cannot be admitted by a remote authority: only the operator's explicit acknowledgement that legacy workers and operations stopped permits that transition; the queue cannot fence or discover claims in separate authorities. Callers must fail closed if the current key is not admitted. A portable Backlog.md source re-reads its list again for availability and immediately before each future acquisition (the acquisition implementation is TASK-130.1); duplicates disable claim actions with `duplicate-item-id`. A vanished ID with a held old key, a GitHub rename/transfer, or a rebind reports `identity-changed` or `binding-migration-required` with the old/new locator where observed. On GitHub, edit `queue.yaml` to the actual new repository before confirming; an unresolved redirect cannot be confirmed. Resolved immutable repository IDs are for observation only, never substituted into claim keys.

## Select next work

`worklease queue next --view Ready --json` examines the entire configured source scope and dependency graph before selecting one item. `--group N` (1–32) returns a bounded wave without overlapping exact claim resources, including exclusions and their reasons. Repeat `--item SOURCE:ITEM` to limit selection to exact refs in that order; this deliberate choice may override advisory assignment (including an assignment view filter), but never readiness or claim checks. Otherwise configured source order, provider priority/order, and stable ref break ties. This is a **read-only observation**, not a reservation: `acquired` is always false. Results distinguish `ready`, `complete-and-empty`, `blocked`, `active-claims`, `assigned-elsewhere`, `ineligible`, and `incomplete`; the latter includes source coverage and item exclusion reasons. Assignment to another person is excluded by default, even when a view does not filter assignments. An unclaimed in-progress item can be resumed. Neither command launches a worker.

For a manual claim, take the candidate's exact resource from `next`, acquire it through the same authority, and re-query on contention (another worker may win the race):

```sh
worklease queue next --view Ready --json
worklease acquire --resource 'RESOURCE_FROM_NEXT' --session 'WORKER_SESSION'
# On contention: worklease queue next --view Ready --json again.
worklease heartbeat --session 'WORKER_SESSION'
# Persist and verify provider progress before release.
worklease release --session 'WORKER_SESSION' --reason 'provider checkpoint verified'
```

For agent loops, use `worklease queue next --view Ready --claim --session 'WORKER_SESSION' --json` instead of the manual race-prone pair. It revalidates each candidate against fresh provider evidence and the identity gate, skips contended candidates without waiting, and returns the first worker-owned claim with its receipt and skipped candidates. When none remain it reports `active-claims`; on uncertain acquisition it stops and reports the pending handle for recovery. `--claim` requires a session, cannot be combined with `--group` or cached/paginated selection, and does not change provider status. Use the normal `worklease verify`, `heartbeat`, and `release` with that session. The worker owns renewal and release; the queue never renews it.

MCP-only loops use `queue_next` with `{ "view": "Ready", "claim": true, "sessionId": "WORKER_SESSION" }` instead. The tool runs the same selection and fresh identity checks, returning the same `next` object (candidate, exclusions, skipped contenders, and no-work result). A successful `next.claim.lease` is an opaque MCP reference: persist it and pass it to `verify`, `heartbeat`, `checkpoint`, and `release`. Optional `ttl` and `maxHold` are seconds; `autoHeartbeat` defaults to true and renews only while this MCP server process lives, so verify ownership after reconnecting or a wait. An uncertain acquire returns a lease reference in the structured error; recover it with `acquire` using only `{ "lease": "REFERENCE" }`, never a fresh `queue_next` attempt. A definitive failure has no lease to replay. With `claim` absent or false the MCP tool is read-only. It loads the queue configuration only when called; missing or invalid configuration is a tool error, not a server startup failure. The view's full authority profile (including endpoint and restore ID) must match the MCP server's startup profile before acquisition; restart the server after profile migration.

## Read-only query

`worklease queue query --view Ready --json` emits the queue query schema v1 inside the normal CLI schema-version 2 envelope. The `query` object contains the view name, authority profile/id/scope, per-source coverage and freshness, an `incomplete` flag, items, and an optional opaque `nextCursor`. Items carry `ref`, display/provider IDs, title, raw/normalized state, readiness and reasons, provider readiness, assignment, claim observation/native state, exact `resources` and `keyInputs` from the claim identity rules, an `actions` map whose start/claim/launch/resume/report-blocked/record-progress/complete entries include `eligible`, `reasons`, `requires`, and `outcome`, and a `launches` array with each named action's gate and safe preview. Unknown action capabilities remain unavailable with reasons. The source rows contain coverage and freshness; provider-specific diagnostics are included when available. Schema v1 is the `query.schemaVersion` contract and is independent of the outer Worklease envelope version.

Use `--limit N` (default 50, range 1–1000) and pass `--cursor` from `nextCursor` for a bounded page. A cursor is valid only for the same view, filters, sources, authority, observed principals/configuration generations, and full source snapshot (including items hidden by the view filter); a changed snapshot returns `cursor-invalid`. `--require-complete` fails with one structured `incomplete` error envelope when source coverage or dependency closure is incomplete. Text output is a compact ID/state/readiness/claim/title table. `--max-age DURATION` reuses a complete source snapshot observed within that age; source rows include `observedAt` and `servedFromIndex`. Without it, the query revalidates sources. The disposable, owner-private WAL index lives at `$XDG_CACHE_HOME/worklease/queue/` (default `~/.cache/worklease/queue/`); deleting it is safe. It partitions rows by source, principal, access scope, and configuration generation. Sources without a provable access-scope identity (currently GitHub) bypass persistent caching. A failed refresh may show stale cached rows with incomplete coverage, never as ready-to-act evidence. The TUI displays cached rows before refreshing in the background.
