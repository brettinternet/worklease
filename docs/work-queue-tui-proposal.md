# Plan: provider-backed work queue

**Status:** Product and architecture decisions for implementation planning. The queue, source adapters, and extension protocol described here are not implemented. Existing claim guarantees remain unchanged.

## 1. Goal and decisions

Give humans and coding agents one fast, Vim-first work queue across existing task sources. Explain both provider readiness and coordination availability without moving tasks into a new tracker.

The following product choices were confirmed during proposal development. They supersede the original open questions.

| Decision | Direction |
| --- | --- |
| Initial sources | Backlog.md and GitHub Issues. Exercise both local and remote access before fixing an extension protocol. |
| Local versus server | Provider adapters run on clients initially. A remote Worklease server changes claim coordination, not task storage or provider execution. A separate source service is a later, demand-driven option. |
| Claim authority | Worklease first. Observe provider-native claims, but defer using a provider as the claim authority until its actual guarantees pass conformance tests. Never silently maintain two authorities. |
| Product scope | Queue and coordination, not an agent runtime, autonomous scheduler, or replacement issue tracker. |

The remaining design decisions in this document make those choices implementable. Performance numbers are proposed acceptance targets, not measured claims. Illustrative interfaces and command names are not frozen APIs.

### Ownership boundaries

```text
 Backlog.md / GitHub / other sources
   task content, dependencies, assignment, durable progress
                       |
              client source adapters
                       |
            shared queue application core
             /                     \
       TUI / JSON                 Worklease authority client
       disposable index           local SQLite OR remote HTTPS
       private recovery journal   claims, expiry, operations, history
```

The queue owns presentation, source access, and the authorized workflow. Sources own task truth. Worklease owns claim truth. These are separate observations, not one transaction or one universal `ready` flag.

Reuse the existing [source-provider contract](../skills/worklease-workflow/references/source-provider-contract.md), [authoring checklist](../skills/worklease-workflow/references/source-provider-authoring-checklist.md), and [generic workflow](../skills/worklease-workflow/references/contract.md). Adapters map provider semantics; they do not each implement selection, scheduling, or claim lifecycle. Extend those contracts alongside implementation rather than creating a conflicting second specification.

## 2. Local and server behavior

“Local” describes claim coordination, not necessarily the task source. Opening a GitHub source explicitly authorizes remote source reads even with a local claim authority. Merely installing Worklease or using its existing local commands must not start network access.

| Source access | Claim authority | Behavior and limit |
| --- | --- | --- |
| Local files or CLI | Local Worklease | No listener or provider login. Coordination covers cooperating processes sharing that authority, not independent databases on the same machine. |
| Remote provider API or CLI | Local Worklease | Read/write the remote provider as the local user. Claims do not exclude workers using other hosts or authorities. Display `local coordination` prominently. |
| Remote provider API or CLI | Remote Worklease | Provider calls and credentials stay on each client. Cooperating hosts must use the same authority and exact portable resource keys. |
| Local checkout | Remote Worklease | Existing host-local Backlog.md/path keys are rejected. Require an explicitly agreed portable identity and data-sharing/write policy, or disable this combination. A shared claim does not synchronize Git clones or their task files. |
| Later source service | Local or remote Worklease | A separately authenticated service may host adapters and source caches. It is not an extension of the claim server and does not acquire ownership for readers. |

Do not make source selection implicitly change the authority. Show the resolved authority ID/profile and identity scope before acquiring. Reject mismatched or ambiguous source bindings. Check server admission before offering Claim: the default remote server admits `coordination:`, so existing `github:` keys may need administrator configuration. Do not silently switch key policies to pass admission. A saved view can combine sources, but its actionable scope uses one selected authority initially; no claim transaction spans authorities.

A selected remote authority becoming unavailable never falls back to local. Read-only cached browsing can continue with an unavailable/stale badge. New claims, renewals, and writes require the relevant live services. An explicit offline view makes no provider or authority requests and cannot authorize mutations.

For the later source service, support local sources only on explicitly configured service-owned mounts or through a separately designed connector. Never treat a client's path as a path on the server. Do not add an arbitrary remote-command or filesystem API. File hosting also needs an authoritative checkout and publishing policy, not just a portable key.

## 3. Identity and interoperability

Keep four identities separate.

| Identity | Purpose |
| --- | --- |
| `Source.id` + `WorkRef.itemID` | Stable provider-qualified item identity. Include provider instance/host and project scope; retain provider-native immutable IDs when available. |
| Source locator | Current repository name, URL, checkout path, or project alias used to find that identity. A rename is not automatically a new item. |
| Exact claim resource + authority ID | The exclusion domain. Every cooperating client must derive identical bytes for the same scope. |
| Provider principal / Worklease installation / worker session | Access, audit, and ownership context. These are not interchangeable identities and do not belong in task resource keys. |

Use the existing versioned key policies where they fit. Do not invent new queue-only keys for tasks that existing CLI/skill callers can already claim. In particular, preserve source-wide loose-Markdown locking rather than pretending every heading can be written independently.

The existing GitHub policy uses the normalized supplied source locator and issue identifier, not an automatically resolved immutable repository ID. Use `owner/repo` and the issue number for public GitHub, matching existing caller examples; use an explicitly agreed `host/owner/repo` input for enterprise hosts. Record the exact inputs in the source binding and expose them to CLI/agent callers. Existing callers using URLs or different aliases need explicit reconciliation, not an assumed equivalent key. Test queue-versus-CLI contention, host separation, transfers, and renames before enabling claims. An unresolved mapping permits browsing only. Do not silently replace legacy keys with immutable-ID keys; changing an exclusion domain requires an explicit migration with old workers stopped, active claims resolved, and all clients updated.

Local Backlog.md identities are host-local even when their files are Git-tracked. A portable custom mapping uses an explicit existing `generic`/`coordination:` policy with documented common inputs, never an inferred Git remote. It provides coordination only and must match every other contender. No aliases may create a second simultaneously writable claim domain.

Two views of the same provider item deduplicate by canonical identity. Cross-provider mirrors are separate items unless an explicit mapping establishes otherwise; do not infer equivalence from titles or links. Resource derivation, display identity, and migration behavior need fixture vectors before any write-capable adapter ships.

## 4. Backend capabilities

Call integrations **source adapters** to distinguish them from claim authorities or database backends. A source adapter must support explicit resolution, capability discovery, paginated summary reads, and authoritative item reads. All other operations are optional.

Capabilities describe semantics, not marketing labels or just booleans. Evaluate them at adapter, source/project, authenticated principal, and item/action scope. A supported operation can still be unauthorized or temporarily unavailable.

```text
Capability {
  support: supported | unsupported | unknown
  permission: allowed | denied | unknown
  availability: available | unavailable | authentication-required
  semantics: operation-specific facts
  limits: operation-specific bounds
  reason: stable diagnostic, when not actionable
}
```

Unknown never means allowed. Read-only discovery must not probe capabilities by attempting a write. Cache capability discovery with an account/configuration generation; invalidate on login, scope, project-schema, or permission changes. The provider still authorizes every request.

| Group | Facts the adapter declares |
| --- | --- |
| Identity | Stable item IDs, instance identity, aliases, identity scope, canonical resource policy/version, rename behavior. |
| Discovery | Pagination/cursor semantics, stable sort and tie-breaker, fields, server filters/search, provider result caps, totals as exact/estimated/unknown. |
| Dependencies | Supported relationship types, direction, completeness, cross-source references, and explicit terminal/blocked mapping. Hierarchy is not automatically a prerequisite. |
| State | Raw states, per-project normalized categories, valid transitions, required fields, reopen/review/archive distinctions. |
| Progress | Structured progress/checklists, durable notes/comments, history, read-back locations, edit/append behavior. `none` is valid. |
| Assignment | Read/write permissions, user/team types, single/multiple cardinality, explicit mapping from worker to provider account. |
| Native claims | Observation and mutation separately; advisory assignment, durable reservation, or actual lease; atomicity, TTL, renewal, release, replay, ownership authentication, resource scope, and fencing evidence. |
| Mutation | Minimal patches versus full replacement, conditional writes per operation, idempotency/replay support, receipt verification, partial batch semantics. |
| Synchronization | Delta tokens, ordering/replay, tombstones, conditional reads, webhooks, reconciliation requirements, quota identity and retry hints. |
| Authentication | Local/CLI/helper/OAuth/app options, supported hosts, required scopes, principal identity, expiry/refresh behavior. |

Illustrative queue-facing operations extend the existing conceptual contract.

```text
resolve(explicitConfig) -> Source
capabilities(source, principal, ref?) -> CapabilitySet
list(source, query, cursor, fields, budget) -> SummaryPage
readItems(refs, fields, budget) -> Items + per-item outcomes
readDependencies(ref, cursor, budget) -> Edges + completeness
changes(source, cursor, budget) -> Changes + nextCursor    # optional
writeState / recordProgress / assign(...) -> ProviderReceipt # optional
readReceipt(receipt) -> verified | conflict | unknown
```

`readItems` may have a bounded fallback where provider batching is absent; it must not conceal an unbounded request fan-out. Every response carries source/principal generation, observation time, coverage, and applicable opaque provider version. Claim revision, provider version, update timestamp, and synchronization cursor are distinct values. An update timestamp or read ETag does not imply a conditional-write capability.

Preserve raw provider status alongside normalized state. Keep readiness, assignment, claim availability, freshness, and dependency completeness separate. A source with no dependency model cannot be presented as dependency-verified merely because no edges were fetched. Explicitly declared “this workflow has no prerequisites” differs from unsupported or incomplete discovery.

Use stable structured diagnostics for unsupported capability, authentication, authorization, conflict, rate limiting with retry time, unavailable source, incomplete graph, and unknown outcome. Map these to the existing workflow result vocabulary rather than replacing it with text parsing. TUI and JSON expose the same reasons and disabled-action explanations.

## 5. Progress, assignment, and write recovery

### Progress is source-owned

Prefer native structured state/progress fields. Use a provider-native note or comment only when that operation is supported and the caller explicitly authorizes it. Do not invent a percentage from elapsed lease time, status, or heartbeat count. A checklist fraction is labeled as checklist completion, not total project progress.

The initial Backlog.md adapter uses supported CLI/API operations for reads and writes, including project-specific state and progress. It does not directly edit or use undocumented parsing of Backlog records as a substitute for that interface. If efficient bulk reads are unavailable, improve the provider-facing API or declare the limitation rather than spawning a process per row.

The initial GitHub adapter uses `gh` JSON/API operations with explicit host and repository selection. Issue state, assignees, comments, and any supported project state remain distinct. Do not promise an In Progress field, dependency model, project workflow, or conditional write without testing the actual endpoint, account, and repository capabilities. Pin/test supported CLI versions and select explicit fields rather than parsing human output.

If no durable progress write exists, the queue remains useful for browsing and claims, but disables Record progress. A Worklease checkpoint is private recovery metadata, not a provider update or a public substitute backlog. Do not silently create sidecar task files. Adding Markdown as a separately configured source is an explicit product choice, not a fallback write path.

Read-only adapters can still support claims, display externally recorded progress, and verify existing provider checkpoints. Unsupported writes remain disabled; an explicit non-completion release is available when no effect is unresolved. Neither path may manufacture a completion checkpoint.

Separate actions are Claim, Assign, Change state, Record progress, and Release. Claim never marks In Progress or assigns a user automatically. A later convenience action may compose these only with an explicit step preview and independently reported outcomes; it cannot claim atomicity across systems.

### Assignment is advisory

Display assignees next to, not instead of, claim holders. Worklease continues to provide local or cross-host exclusion even when the provider supports only user assignment. Assignment to a person can outlive many short worker leases.

Default eligibility policy excludes work assigned to someone else from suggested work. A deliberate user selection may override that policy without silently reassigning the task. Assignment to me is not permission to adopt another session's claim. Multiple workers using one provider account still require distinct Worklease sessions.

An authorized Assign to me operation preserves unrelated assignees according to the provider's cardinality and explicit requested intent. Release does not clear assignment or status. Never advertise assignment mirroring as a distributed lock, even if a conditional update can avoid one lost update; it does not necessarily supply lease expiry, owner-authenticated renewal, or resource exclusion.

### Every write has a recoverable boundary

1. Check declared capability, caller authority, exact scope, and the current claim. Refresh the item and required dependency closure from the provider; recheck native occupancy and policy eligibility. Preserve unrelated fields.
2. Persist exact mutation intent in an owner-private recovery journal before dispatch, including operation ID, source/principal identity, requested patch, precondition, and claim/operation references. Use existing Worklease guarded-operation and pending-handle machinery for what it already owns; do not duplicate its claim journal.
3. Use provider conditional writes and idempotency keys only where genuinely supported. Without them, report coordination-only and allow only the supported minimal operation; pre/post reads do not eliminate the race with external writers.
4. Obtain a durable provider receipt and independently read back the authoritative result. Verify the intended fields or operation-specific receipt, not merely exit status or HTTP success. Eventual consistency produces a bounded pending-verification state, not an optimistic completion.
5. Only after verification, record any Worklease checkpoint and resolve the guarded operation. Release as completed only after the required provider checkpoint and operation outcome are established.

A matching comment body alone cannot prove which attempt created it. Retain provider operation IDs or permitted idempotency markers where available. If a lost response cannot be disambiguated, surface Unknown outcome and require inspection/reconciliation. Do not retry non-idempotent writes under a new ID, automatically reverse a possibly successful write, or reacquire to bypass recovery.

If acquiring succeeded but a subsequent pre-dispatch check fails, report Claim held / source unchanged and offer a safe release when no operation is unresolved. If a provider write succeeded but checkpointing failed, retain its receipt and finish recovery without repeating the provider write. Provider failure does not undo the claim, and claim expiry does not undo a provider effect. A provider outage stops further writes, not necessarily the worker's bounded heartbeat/recovery effort.

The source index is disposable. Pending operation intents and receipts are not. Store them separately with restrictive permissions and bounded retention only after resolution. Logout/cache eviction must not destroy unresolved recovery records. A read-back version proves an observed result, not that no future writer will change it. A CAS on an item version protects that mutation but is not evidence that the provider enforces Worklease lease ownership.

## 6. Distributed and provider-native claims

### Initial policy

Use one selected Worklease authority for all queue acquisitions. Remote coordination works only when all cooperating workers use that authority and identical resources. Neither local nor remote Worklease excludes arbitrary provider writers or workers using independent native claim systems.

Where the provider exposes native claims, show the holder, scope, observation age, and reported semantics. Fresh native occupancy blocks suggested acquisition by default. Unavailable/stale native occupancy must not be rendered as free. If the provider exposes no claim capability, show `not exposed`, not `unclaimed`; this does not disable Worklease-only coordination. Observing an empty native claim then acquiring Worklease is not a transaction with the provider; a native-only worker can race it. Deployments mixing these workers need a shared coordination policy, not a green “globally exclusive” badge.

Do not acquire a native claim merely to mirror Worklease. Two leases introduce partial acquisition, two renewals, expiry disagreement, and recovery deadlocks. Assignment or status mirroring, when requested, remains a provider mutation rather than a second authority.

### Admission criteria for a later native authority

Native claim support is a separate authority integration, not a source adapter method casually substituted for `acquire`. First test a real provider and document its observable contract.

- Atomically acquire-if-free across the intended hosts, with authenticated ownership and defined expiry/renewal/release rules.
- Identify the authority and ownership epoch, reject stale owner operations, and define clock, network partition, lost-response, replay, restart, and takeover behavior.
- Expose scope and bundle guarantees. A single-item provider claim cannot emulate Worklease's atomic one-to-32-resource bundle or cover files/ports/deployments. Reject unsupported workflows instead of splitting a bundle into independent claims.
- Expose operation/recovery and fencing limitations honestly. Provider task reservation alone is not a conforming replacement for the full guarded-operation API.
- Pass contention, stale-owner, uncertain-acquire, expiry, recovery, and security tests. Capabilities must be evidenced, not inferred from a command called `claim`.

A provider offering only an indefinite reservation remains a reservation, not a renewable lease. If useful, a future mode can expose that narrower workflow explicitly; it cannot inherit Worklease guarantees by implementing methods with similar names.

Selecting native authority would be explicit per actionable scope and visible in every claim/receipt. It cannot silently activate on discovery or outage. Migration from Worklease requires a quiescent, audited cutover. Never infer release or executor cessation from TTL expiry, a changed assignee, or a missing claim row. See [claim guarantees](claim-model.md#guarantees).

### Who renews a claim?

Browsing never renews someone else's claim. A worker that uses the CLI/MCP owns its existing handle, session, and heartbeat lifecycle; the TUI observes it. The first handoff path passes the item and resource/authority references to a worker, which acquires its own claim. It does not pass bearer credentials or launch an agent.

For an explicit human Claim for me, the queue owns a distinct, persisted full-UUID session and renews while that work session is active, before half the TTL with jitter and a safe deadline margin. Display the next renewal and last result. Suspend/resume or loss of ownership requires authoritative verification before further work. Network uncertainty disables writes even if a local countdown is positive.

On exit with owned claims, show the exact consequence. Offer verified release when safe, or stop renewing and leave the remaining lease/recovery state explicitly visible. Never release an unresolved operation as completed, silently create a renewal daemon, or promise continued ownership after closing. Reopening requires the private handle and live verification, not a matching username. A future handoff must use supported transfer semantics; current remote cross-host transfer is not implemented.

## 7. Authentication and trust

Provider authentication is separate from Worklease enrollment. A Worklease installation's write role grants no GitHub, Linear, or Jira permission, and a provider OAuth token grants no claim ownership.

The current remote authority's read role can inspect namespace claim metadata; it does not enforce source-specific provider ACLs. Treat an authority as a shared coordination trust domain. Do not publish task titles, descriptions, or private progress in public claim metadata. Resource keys can still reveal source/item existence, and hashing is not access control. Deployments requiring stronger separation need separately administered authorities or a separately designed authorization change, not a filtered TUI pretending to provide security.

| Deployment | Credential behavior |
| --- | --- |
| Local Backlog.md / local CLI | Use OS access and the supported provider interface. No new login. Filesystem access still needs a trusted source root. |
| Personal GitHub source | Reuse the selected host/account's `gh` login. Show the resolved provider principal before writes; never rely on whichever ambient account happens to answer first. |
| Other personal integrations | Prefer the provider's existing credential helper; otherwise use a supported native-app OAuth flow or explicit scoped token input. No embedded application secret in a distributed CLI. |
| Headless/team automation | Use provider-supported app/service identities, short-lived scoped credentials, or a caller-provided helper. Record both initiating worker and actual provider actor. |
| Later source service | Server-managed secret store and credential refresh; separate queue-client authentication and per-source authorization. Choose explicitly between per-user delegation and a documented service identity. |

Bind credential references to provider origin, tenant/project scope, and principal. Keep secrets in an OS keychain/helper or protected secret store; a supported private file/fd path is a fallback for headless use. Do not put secrets on argv, in repository configuration, keys, receipts, task comments, traces, or logs. Source configuration contains references, not tokens. Sanitize CLI stderr as well as successful JSON.

Offer read-only setup first. Request additional write scopes only for authorized operations and re-discover capabilities afterward. Serialize token refresh for each credential to avoid refresh races. Distinguish expired credentials, revoked scopes, permission denial, SSO requirements, quota exhaustion, and provider downtime. Never prompt for interactive login from an unattended agent operation; return an actionable structured error.

A project file may suggest a source but cannot auto-install/execute an adapter, select a secret, or authorize a new endpoint. Require user trust before executing configured binaries or contacting newly introduced origins. Provider URLs, redirect destinations, local roots, and custom hosts need validation. A later service additionally needs SSRF protections and explicit host/mount allowlists.

Partition caches and indexes by source instance, tenant, principal/access scope, and configuration generation. Shared service caches must not expose one user's private tasks, search matches, dependency names, counts, or existence to another. Initially, do not share payloads across principals. Client-private cached data has explicit retention and offline-use consent; revocation cannot erase copies already read while disconnected. Hide/purge inaccessible cached projections after revalidation, and invalidate all derived indexes. Preserve only protected recovery material needed to resolve outstanding writes.

## 8. Extensibility without a framework

Expose the optional UI as `worklease queue`, with a proposed `worklease queue query --json` agent path. Keep both thin clients of the same queue core. These names are planning choices, not implemented commands. Keep existing CLI defaults, authority wire protocol, resource policy version, and MCP tool surface compatible. Decide any new MCP query tools separately after the JSON contract is exercised.

Start with small internal Go interfaces and built-in Backlog.md/GitHub adapters. Reuse the authority client and safe handle/lifecycle code rather than opening its SQLite tables from the TUI. Do not spawn a new Worklease process for every visible row; versioned JSON is useful for integration but not an excuse for unbounded process overhead.

After both sources exercise the model, add explicitly installed out-of-process adapters using a versioned JSON protocol over stdio. Any language can implement it. Use the same conformance fixtures for built-in and external adapters.

The extension boundary needs:

- A manifest with adapter identity/version, protocol range, config schema, supported authentication methods, and declared capabilities. Negotiate major protocol compatibility; unknown optional fields may be ignored, unknown required semantics may not.
- Request IDs, cancellation, deadlines, bounded message/collection sizes, pagination, and backpressure. Protocol output belongs on stdout; bounded redacted diagnostics go on stderr. Prefer a supervised long-lived process over per-item spawning.
- Explicit executable/version selection and user approval. No automatic plugin download or execution from repository metadata. A crash isolates the source; a crash after dispatch leaves the mutation uncertain.
- Source-scoped credential references or a narrowly scoped helper channel. Do not send Worklease bearer credentials to source adapters. Minimize inherited environment and accessible configuration.
- A fake provider, golden protocol fixtures, identity vectors, and tests for pagination, stale writes, capability denials, cancellation, quota handling, malformed output, uncertain outcomes, and secret leakage.

Process isolation is not a sandbox. An installed executable runs with the user's OS privileges unless a separate sandbox is explicitly provided. Document that trust rather than promising credential isolation that stdio cannot enforce.

Other useful seams are credential helpers and declarative saved views, filters, status mappings, ordering, and resource-policy bindings. Keep readiness and resource derivation inspectable and deterministic. Do not introduce executable policy plugins, arbitrary TUI widgets, lifecycle hooks, an agent scheduler, storage plugins, or a general event bus without a concrete requirement. A later notification integration should consume a bounded redacted public stream, not arbitrary callbacks inside claim transactions.

## 9. TUI and agent experience

Default to a dense list/detail layout. A board hides too much about dependencies, authority, freshness, and partial loading; it can be considered later as another view of the same model.

```text
Worklease  Queue   authority: local [host-local]   sources: 2/2   synced 8s ago
 Sources / Views       Work queue                          Details
 > All                 ID       State   Ready   Claim      GH #184 Retry backoff
   Backlog             TASK-42  Todo    yes     free       Assignee: brett
   GitHub              GH #184  Open    blocked alice 4m   Source: GitHub / api
   Mine                GH #190  Open    unknown unknown    Needs: #177 (open)
   Ready                                                   Claim: alice / loop-7
   Claimed by me       50 loaded / total unknown            Expires: 4m [observed]
   Needs recovery      More pages available                 Native claim: not exposed
                                                           Summary / dependencies
 / filter   Enter details   c claim   a assign   p progress   r refresh   ? help
```

The example is illustrative, not a guarantee that a provider supplies every column. Show assignment, provider-native occupancy, and Worklease ownership independently. A claim badge always includes authority/scope in its detail view. `Mine` distinguishes assigned to my provider account from held by my worker session.

- Use `j/k`, `gg/G`, `/`, `n/N`, Enter, Escape, and consistent pane navigation. Provide arrow-key equivalents and a command palette for less common actions. Explain whether search covers loaded rows, the local index, or the remote source.
- Keep selection anchored by canonical item identity during refresh. Do not reorder beneath an open confirmation or steal focus when a detail response arrives late. Preserve scroll position; signal new rows instead of jumping.
- Offer detail tabs for Summary, Dependencies, Activity, Claims, and Recovery. Lazy-load long bodies and comments. Show raw workflow state and why a transition/action is unavailable.
- Distinguish blocked, occupied, assigned elsewhere, unknown dependencies, stale, permission denied, offline, rate-limited, and recovery-required. Never collapse them into “no work.”
- Before a mutation, show source, provider actor, authority, exact scope/resources, requested change, and guarantee limitation. Unsupported actions stay discoverable with an explanation. No partial-success toast that conceals a held claim or uncertain write.
- On narrow terminals, use list/detail switching rather than unreadable squeezed columns. Support no-color and high-contrast modes, resize, Unicode width, and text labels instead of color-only state. Plain JSON/text remains available for agents and accessibility.
- Treat descriptions as untrusted content. Strip terminal control/OSC sequences, bound Markdown rendering, and never execute embedded commands. Opening a URL or external editor requires an explicit user action and safe argument passing.

The JSON path reports identical items, coverage, freshness, authority, capability denials, and action outcomes. It needs bounded pagination and stable schema versioning. No workflow should require screen scraping or be safer in the TUI than in automation.

## 10. Remote loading and performance

### Fast first view, honest partial knowledge

Render a bounded page of cached summaries immediately, then revalidate asynchronously. A cold source fetches one summary page; bodies, comments, expanded dependencies, and history load only on demand. Slow or failed sources cannot block healthy ones. Loading completion order must not alter configured source order or workflow selection order.

Store summaries and searchable fields in a disposable local SQLite index, separate from the authority database and recovery journal. Use indexed queries/FTS where appropriate, bounded detail caches, and explicit schema generations. Fetching every body to enable search is opt-in background indexing, not startup work. Label body-search coverage when only some bodies are indexed.

Each query reports source coverage, scope, observation time, page cursor, exact/estimated/unknown totals, and reason for incompleteness. Distinguish these statements:

- “No match in the loaded/indexed subset.”
- “No match in the complete selected provider scope.”
- “This exact item has a fully checked prerequisite closure.”
- “There is no eligible work anywhere in the complete selected scope.”

Only the last requires full scope enumeration and the required complete graph. Paginated browsing does not relax the generic workflow's complete-graph requirement before selecting work. An explicit item action can hydrate and verify its authorized selection plus prerequisite closure; a Next ready/source-wide selection cannot silently choose from the first page. Cycles, inaccessible/missing prerequisites, exhausted graph budgets, and unsupported dependency semantics block readiness proof. Dependency reads do not authorize writes outside the selected scope.

Filtering, cross-source sorting, and provider search may have incompatible order/cap semantics. Push down only filters with equivalent meaning; evaluate residual filters explicitly. Preserve stable per-source order and deterministic tie-breakers. Do not claim a globally complete top-N ranking from independently truncated source searches. Cursors bind query, sort, source, principal, and generation; filter changes or expired cursors restart the appropriate scan.

### Bounded work and incremental synchronization

Use one request scheduler per client process with shared budgets for the actual provider quota identity, not just each repository. Start with conservative bounded concurrency, provider-sized pages, cancellation of superseded reads, deduplicated in-flight fetches, and batched reads where available. Reserve capacity for authoritative action checks. Heartbeats use a separate control path and cannot sit behind source refresh or full-text indexing. Lifecycle requests still serialize through the existing per-handle revision/pending-request rules; reserved capacity must not bypass an unresolved request or race two mutations of the same claim.

Debounce search, prioritize visible summaries and selected details, and bound prefetch to adjacent pages. Never perform a network call per row during rendering. Cap pending jobs, item/body sizes, decoded responses, dependency traversal, and memory. If a provider has no batch interface, enforce the same budget on individual requests and show progress rather than hiding a large fan-out.

Honor provider retry hints and rate headers with exponential backoff and jitter; distinguish rate-limited from offline. Retry safe reads, not uncertain writes. Stop background hydration before spending capacity needed for an explicit action. Within a process, share polling by source/principal/query; across many clients, measure duplicated traffic before adding a daemon or service.

Prefer provider deltas and conditional reads when supported. Treat webhooks as invalidation hints unless the provider proves a complete ordered stream. Handle duplicates, out-of-order updates, tombstones, changed permissions, expired cursors, and fields disappearing. Reconcile periodically with a bounded authoritative scan. Do not delete missing rows from an incomplete scan; use a completed scan generation or explicit deletion evidence. Apply each page and its checkpoint atomically in the disposable cache so crashes cannot skip uncommitted data.

For mutable offset pagination or weak update timestamps, deduplicate by stable identity, overlap incremental windows where appropriate, and retain an incomplete/syncing label until reconciliation. A timestamp alone is not a lossless change cursor. Local filesystem watches similarly invalidate provider reads, with overflow recovery and periodic reconciliation; they are not a substitute provider parser.

### Efficient claim overlays

Use the existing exact-resource batch `status` operation for visible/actionable items, plus events/watch to invalidate and refresh a bounded claim projection per authority. Do not poll each row or load all historical claims on every refresh. Keep provider sync and claim freshness independent. Subscribe using a baseline/cursor sequence that cannot miss changes between snapshot and watch; reconcile on gaps or authority restore and refresh before acting. Expiry needs authoritative rechecks even when no new lifecycle event arrives; a local countdown is display only.

The current authority interface has an unpaginated `List`, optionally filtered to one exact resource; resource-filtered watches are limited to 32 resources. Event cursors already carry identity and gap semantics. Do not assume these are a scalable bulk-subscription API. Benchmark them, cap supported working sets, and introduce provider-neutral bounded snapshot/filter or paging support only if required. Never expand the watch filter limit merely to attach the entire queue. Baseline/event consistency and cursor recovery need explicit tests before an event-derived availability badge can be trusted.

### Proposed acceptance budgets

Measure on a documented reference machine with fixed fixtures and injected network latency. Include p50/p95/p99, process count, API request count, transferred bytes, RSS, disk/index size, and quota cost. Do not present provider-controlled latency as a Worklease guarantee.

| Workload | Initial target |
| --- | --- |
| Warm first view, 10,000 summaries | p95 within 500 ms; no network dependency before rendering. |
| Navigation and scrolling | p95 input-to-render within 50 ms with cached rows, including concurrent refresh. |
| Indexed filter/search, 10,000 summaries | p95 within 100 ms; no full corpus parse or external process per keystroke. |
| Cold remote view | Render within 150 ms of receiving the first summary page; never wait for full enumeration. Track provider latency separately. |
| Large browsing corpus, 100,000 summaries | Remain navigable with bounded windows/caches and a provisional 256 MiB RSS budget, excluding external provider processes, which are reported separately. |
| Failure/load scenario | Rate limiting, a hung adapter, a large graph, and full refresh cannot starve owned-claim renewal or freeze input. Measure renewal deadline margin. |

These numbers are prototype exit criteria to validate or revise with recorded measurements before implementation scope expands, not a reason to weaken safety. Test a larger stress corpus to find the failure boundary rather than promising unlimited scale.

## 11. Scaling limits and overlooked failure cases

| Risk | Plan now; escalate only with evidence |
| --- | --- |
| Many clients polling the same account | Per-quota scheduling and jitter now. A permission-aware source service later can coalesce reads, own webhooks, and isolate tenants. A shared service identity does not inherit each viewer's provider permissions. |
| Many claims and renewals | Estimate load as active claims / renewal interval plus acquisitions, watches, history, and recovery. Benchmark the existing single-writer authority separately from source throughput. Bound background reads so renewal latency stays safe. |
| SQLite/WAL growth | Separate cache from authority state. Track disk headroom, write latency, checkpointing, pinned unresolved history, and recovery journal growth. Cache eviction must not erase authority evidence. Never put the authority WAL on a network filesystem. |
| Multiple authority replicas | Existing remote mode is one writer, not HA. No writable clones, lease failover, or source-hub substitution. Storage/HA work needs separate conformance and restore evidence. |
| Large dependency graphs | Incremental closure reads and graph indexes, cycle detection, traversal bounds, and explicit incomplete outcomes. Source-wide scheduling may legitimately require more data than browsing. |
| Hot items and contention | Reuse bounded claim waits and jitter. No new FIFO/fairness guarantee or server-side work queue. Show contention rather than repeatedly acquiring in a tight loop. |
| Tenant and ACL changes | Permission-aware partitioning and invalidation across summaries, FTS, bodies, graphs, and counts. Do not leak hidden dependency identifiers through explanations. |
| Cache/reset/upgrade | Schema-versioned disposable rebuilds; durable recovery migrations separately. Invalid provider cursor, cache loss, plugin upgrade, authority restore, and principal change are different reset causes. |
| Sleep, clock skew, and disconnect | Use authority time/lease verification, not wall-clock UI countdowns. Stop writes on uncertainty and retain recovery evidence. |
| Source disappearance | Distinguish deleted, moved, archived, inaccessible, and temporarily unavailable. Preserve claim/recovery references even if the source no longer resolves. |
| Bulk operations | Defer initially. Later report per-item results and cancellation boundaries; no implied cross-source atomicity or rollback. |
| Telemetry and privacy | Opt-in diagnostics with bounded identifiers and no task bodies/tokens. Measure queue wait, provider latency, cache lag, graph coverage, renewal margin, and unknown outcomes. |

Also test concurrent human edits, required transition fields, changes to workflow schemas, read-only filesystems, CLI version drift, interrupted login, revoked service installations, duplicate webhooks, terminal escape injection, enormous descriptions, and adapter crashes after a write. These are acceptance scenarios, not just exception text.

## 12. Implementation slices and decision checkpoints

Create backlog items from these slices after this plan is accepted. This document does not create or complete backlog work.

1. **Contract and identity fixtures.** Extend the existing source contract with capability, pagination, coverage, freshness, and principal semantics. Correct the generated Backlog Worklease guide through the Backlog CLI: its claim that remote authority is deferred is stale; the current [experimental remote contract](remote-claim-authority.md) governs this plan. Verify GitHub canonical inputs and migration limits. Demonstrate identical queue/CLI claim resources, host separation, source-wide Markdown scope, and structured unsupported results. Keep protocol names internal.
2. **Read-only vertical slice.** Shared query core, Backlog.md and GitHub adapters, explicit auth/source setup, summary/detail TUI, JSON query, and claim visibility. Prove incomplete graphs, unreadable sources, stale cache, and local-versus-remote authority scope are visible. No provider writes or new claim ownership yet.
3. **Performance and synchronization.** Add the disposable index, bounded scheduler, lazy hydration, reconciliation, and claim projection. Publish benchmark fixtures/results and pagination/cursor correctness tests. Resolve any authority snapshot/paging gap needed for the supported scale before claiming live availability.
4. **Claim lifecycle.** Explicit exact resources, acquisition revalidation, contention, private session ownership, independent heartbeat, exit/suspend behavior, and CLI worker handoff. Prove remote outages never change authority and native occupancy never becomes a false global-exclusion promise.
5. **Provider progress and assignment.** Add only tested operations with explicit permission, durable intent, receipt read-back, conflict handling, and recovery UI. Inject lost responses and partial failures at each boundary. Verify assignment-only and progress-unsupported sources still behave honestly.
6. **External source adapters.** Stabilize a small versioned out-of-process protocol from the two implementations. Publish an authoring guide, sample adapter, compatibility policy, and shared conformance suite. Require install/trust approval and test crashes, malformed output, cancellation, and secret redaction.
7. **Evidence-driven additions.** Add Beads, Linear, Jira, GitLab, or another source based on actual demand. Native-authority mode requires a real provider conformance study. A source service requires measured duplicated traffic/latency, an authorization/cache design, and a deployment need. Neither is a prerequisite for the initial useful queue.

Before splitting implementation work, retain explicit decisions/evidence for the supported corpus and client count, reference benchmark hardware, Backlog.md bulk-read availability, GitHub identity/endpoint capability vectors, and authority snapshot/watch consistency. Investigations may narrow supported scope; they must not silently change claims, data authority, or authorization guarantees.
