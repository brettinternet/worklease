# Work queue configuration

Start in a Backlog.md checkout or a checkout with a GitHub `origin` remote:

```sh
worklease queue init            # write owner-private configuration and confirm safe initial identity
worklease queue init --dry-run  # preview detected facts, their origins, and exact YAML without writing
```

Use `--checkout PATH` for another repository, `--adapter backlog-md|github` to override detection, `--source-id ID` to choose an ID, `--portable-claims SOURCE` to opt into cross-host claims, and `--authority NAME` for a trusted remote profile. Backlog.md `me` defaults to its single `defaultAssignee`, then `@` plus the OS login; `--me @name` overrides the default or adds a name to an existing list. `--allow-git-network` explicitly consents when the Backlog.md project enables remote Git operations or active-branch checks; otherwise init refuses to write. Init checks the same adapter prerequisites as queue: Git, Backlog.md CLI 1.52.x or an authenticated `gh` account (`gh auth login --hostname HOST`). A bare `backlog/` folder is not enough to detect Backlog.md. If both Backlog.md and a GitHub origin exist, init selects Backlog.md and prints the explicit command to add GitHub. `worklease queue --view NAME init` selects a different view. Re-running adds a source to the selected view or reports an already configured checkout. Only new host-local sources on the local authority are automatically identity-confirmed; for other bindings, complete the migration checklist below and run the printed `queue identity confirm` command.

## Hand-written YAML reference

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
    workflow:
      start: In Progress
      complete: Done
      reopen: To Do
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

`checkout` must exist and `~` expands from HOME. Omit `claims` for host-local Backlog.md keys; a portable `generic` source must be agreed by all claimants before use. `allowGitNetwork` defaults to false. GitHub repositories use `owner/repo` and require an explicit host and account. View authorities must be `local` or a name in the trusted `profiles.yaml`; source IDs must be defined above and must not contain `:`, which separates `SOURCE:ITEM` refs. Filter keys are limited to `readiness`, `claim`, and `assigned`. Missing configuration is reported as `no-sources-configured` with this setup guidance.

For forthcoming built-in `linear` and `jira-cloud` sources, owner-private `queue.yaml` accepts `account: EXPECTED_PRINCIPAL` and `credentialHelper: [/absolute/path/to/helper, arg]`. The helper prints **one token line** to stdout; put credentials in the helper's private store, not in YAML or argv. Execution has a 15-second timeout and a 4 KiB limit on each output stream, terminates its process group on exit, inherits only process plumbing and the user's private configuration directory variables, and discards stderr. The queue resolves each credential serially and returns a token only after the adapter verifies its authenticated principal against `account`; failures and mismatches disable writes without echoing secrets. GitHub continues using `gh auth token` unchanged. These two adapters are not yet shipped: configuring one now does not enable reads, claims, or writes, and `claims`/`workflow` are rejected until their adapters are implemented.

`workflow` is an optional per-source map from `start`, `blocked`, `review`, `complete`, and `reopen` to real provider transitions. Configure only transitions that the provider supports; the write adapter validates each mapping before a write. An absent mapping reports `no-workflow-mapping` rather than inventing a provider status. The configured account is the external adapter's principal for capability checks and assignment; it does not alter the source-scoped generic claim identity. The TUI's `s` (configured state), `p` (progress note), and `a` (assign to me) actions require an existing verified queue-owned claim. Each shows the exact provider effect, claim, races, side effects, and marker before confirmation; the claim alone never changes provider state. No status mapping is invented for an unsupported provider. Pending write intents are stored under `$XDG_STATE_HOME/worklease/queue-recovery/` (default `~/.local/state/worklease/queue-recovery/`), apart from the disposable index. Do not delete unresolved records when clearing the cache or logging out. `worklease queue recovery --json` lists unresolved records across sources with the operation ID, item, intended effect, claim, dispatch time, last read-back outcome, and permitted recovery steps; it reads only owner-private state and does not need a queue view or live provider. In the TUI, the Recovery view lists the same records and the item detail's Recovery tab filters them by item. A lost write response is never success or a reason to release the claim: `worklease queue recovery retry --operation-id ID --handle PRIVATE_HANDLE` retries read-back and any exact checkpoint replay without redispatching the provider write. If read-back fails, its error reports `commitState: unknown` until the outcome is proven; an absent recovery ID reports `operation-not-found`, not proof that a provider write did not commit. Select the original authority with `--profile NAME` if it is not the default. For a verified provider effect with a pending checkpoint, `retry` checks the original authority first: a committed checkpoint resolves normally, but after `CheckpointNotAfter` it will not replay an absent checkpoint. After confirming no executor can still commit it, run `worklease queue recovery checkpoint-missing --operation-id ID --handle PRIVATE_HANDLE --evidence 'what was checked' --provider-verified --checkpoint-absent --executor-ceased` against the original authority. It rechecks checkpoint status, requires durable verified provider read-back and an expired deadline, and records the operator identity and typed evidence in the terminal `checkpoint-missing` journal record. In the TUI Recovery view, press `e` on a checkpoint-pending record and type `PROVIDER VERIFIED; CHECKPOINT ABSENT; EXECUTOR STOPPED: ` followed by provider and authority audit evidence. Neither path redispatches the provider write or replays an expired checkpoint. The provider effect happened, the Worklease checkpoint did not; the claim remains held and cannot be cancelled as no-effect. This terminal outcome no longer blocks later writes on the same item. If authority status is unavailable or the executor may still finish, leave the record unresolved. Once the original executor has stopped and a provider audit proves no write committed, `worklease queue recovery reconcile --operation-id ID --evidence 'what was checked' --no-commit --executor-ceased` records the OS operator identity and typed evidence in the private journal. Reconciliation is refused while dispatch is still in progress or a provider receipt/verified read-back exists; it does not checkpoint or release the claim.

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

## External adapters and executable approval

External sources name an absolute executable and the adapter identity/version expected from its manifest:

```yaml
sources:
  - id: planning
    adapter: external
    executable: /opt/worklease-adapters/planning
    expectedAdapterId: example.planning
    expectedVersion: 1.2.3
    config: {}
    account: brett
    workflow:
      start: In Progress
      complete: Done
    claims: {policy: generic, source: acme/planning}
```

External sources without `claims` remain read-only: they can be queried, but claim observation and claim/write actions fail closed. To enable claims, explicitly configure `claims: {policy: generic, source: STABLE_AGREED_IDENTITY}`. The source identity must be non-empty, exact (no surrounding whitespace or control characters), and agreed by all claimants. Only the static `generic` key policy is supported; the adapter manifest must independently declare `resourcePolicy: generic`. Writes also require an explicit safe `account` principal; provider-state actions require their corresponding `workflow` transition mapping. The adapter ID, display locator, executable path, and other resolved values are never implicit claim-key inputs.

Approval is explicit and source-specific. Preview the configured executable path, expected adapter ID/version, and current SHA-256 digest with the command below; inspect the exact `claims`, `account`, and `workflow` bindings in `queue.yaml` before acknowledging it:

```sh
worklease queue adapter approve --source planning --json
```

Review the preview, then repeat with `--acknowledge` to record it. Approval hashes the existing executable and does not execute or download it. External sources may set `account` to the explicit non-empty provider principal and `workflow` to provider transitions for `start`, `blocked`, `review`, `complete`, and `reopen`; host, repository, and checkout fields remain invalid. Writes additionally require `claims: {policy: generic, source: ...}`. The owner-private approval binds the source ID, path, expected identity/version, exact claims binding (including the absence of one), account, workflow map, executable digest, and a hash of source config and credential reference (without storing their raw values); changing any binding requires approval again. The host launches only a private copy whose bytes match the approved digest. An unapproved or changed executable/binding is not started. Queue query reports that source as incomplete while continuing to read independent configured sources.

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
