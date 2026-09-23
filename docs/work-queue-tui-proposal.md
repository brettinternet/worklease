# Plan: provider-backed work queue

**Status:** Accepted direction for backlog decomposition. Nothing described here is implemented. Existing claim guarantees, CLI defaults, authority wire protocol, resource policy version, and MCP tools remain unchanged. Performance numbers are acceptance targets unless marked as evidence. Command names, configuration keys, and interfaces are illustrative until the slice that ships them freezes them.

## 1. Goal

Give humans and coding agents one fast, Vim-first work queue across existing task sources. Explain both provider readiness and coordination availability without moving tasks into a new tracker.

Success means less time finding safely claimable work and fewer conflicting workers, not more tracker features. Focused operational edits and explainable dependency mapping are core capabilities. Full tracker administration, arbitrary workflow scripting, and autonomous agent scheduling are not. Lightweight launch actions are handoffs to an existing worker, not a managed agent runtime.

## 2. Decision register

Backlog items cite these IDs. Changing a decision means updating this table and the affected sections in the same commit.

| ID | Decision | Basis |
| --- | --- | --- |
| D1 | Scope is queue and coordination. Not an agent runtime, autonomous scheduler, or replacement tracker. | Confirmed |
| D2 | Initial sources are built-in Backlog.md and GitHub Issues adapters. | Confirmed |
| D3 | Source adapters run in the client. A remote Worklease authority changes claim coordination only. A separate source service waits for measured need (§15). | Confirmed |
| D4 | Each actionable scope uses one selected Worklease authority. Provider-native claims are observed, never mirrored, and never become an authority until they pass §9 admission. Neither initial provider exposes one. | Confirmed + evidence |
| D5 | Assignment is advisory and displayed beside claims. The primitive Claim action never assigns or changes provider state; Start work is a separate, explicit composition (D26). | Design |
| D6 | Progress lives in the provider. Only tested operations are enabled. A Worklease checkpoint is private recovery metadata. | Design |
| D7 | Every provider write follows intent, dispatch, receipt, read-back, then checkpoint (§8). Append-only writes carry a Worklease operation marker to locate the result after a lost response. Verification also checks the intended item, payload, and available provenance. A marker never makes a retry safe. | Design |
| D8 | Ship as `worklease queue` in the main binary. TUI and adapter dependencies live in queue packages that claim, MCP, and server code never import. Exception (D28): the MCP stdio server may import the queue core and source adapters, never the TUI, solely for the `queue_next` tool, and performs no source access until that tool is called. The authority server never imports queue packages. | User |
| D9 | Build the TUI on Bubble Tea v1 and Lip Gloss v1. The queue core publishes immutable snapshots; no I/O runs on the render loop. Pinned at tested v1.3.10 and v1.1.0 with a clean vulnerability scan of their transitive versions. | S2 spike confirmed the library choice on the D22 host; native terminal compatibility is verified by TASK-135 (§3) |
| D10 | Sources, views, identity mappings, and launch actions live in one owner-private user file, `$XDG_CONFIG_HOME/worklease/queue.yaml`, following the profiles/bindings trust model. v1 reads no repository-provided queue configuration. | Existing pattern |
| D11 | Each view names its authority profile. Claims and launches are disabled for a checkout-backed source when that checkout's profile resolution yields a different authority ID. | Design |
| D12 | A Git-tracked Backlog.md project coordinates across hosts only through an explicit portable binding: the `generic` policy with a declared source name. Existing callers must migrate to the same keys; authority agreement alone is insufficient. Duplicate task IDs block claims. | User + safety constraint |
| D13 | The Backlog.md adapter uses only `backlog … --json` commands and documented edit flags. Its MCP server is not a read path because it returns formatted text. Request bulk dependency fields upstream. Declare Git side effects from project configuration. | Evidence |
| D14 | The GitHub adapter speaks HTTPS (GraphQL and REST) directly. It uses `gh auth token --hostname H --user U` as a credential helper for an explicitly configured account, verifies the principal, and sends requests serially per host/account within each client process. Honor REST and GraphQL rate-limit headers and GraphQL `rateLimit` fields; this probe does not establish a cross-process quota coordinator. | Live github.com evidence; GHES unprobed |
| D15 | Capabilities are scoped semantic facts: support, permission, availability, semantics, and limits. Unknown never means allowed. | Design |
| D16 | External adapters arrive only after both built-ins exercise the model, as a versioned JSON-RPC 2.0 protocol over stdio. Resource policies stay static built-ins; an external adapter selects an existing policy. | Design + history |
| D17 | Other extension seams are credential helpers, declarative views, filters, status maps, keymaps, and user-configured launch actions. No executable policy plugins, widgets, lifecycle hooks, storage plugins, or event bus. | User + design |
| D18 | Launch actions run user-configured argv in an allowlisted environment carrying item references, never credentials or item content. The launched worker acquires its own claim, so Launch is unavailable while the queue itself holds that item. | User |
| D19 | The source index is a disposable per-user SQLite cache partitioned by source, principal, and generation. All queue processes share it with single-flight refresh per partition. | Design |
| D20 | Claim overlays use one namespace cursor watch per authority plus batched exact-resource status. The baseline cursor is taken before the snapshot. | Evidence |
| D21 | Support team scale: 10,000 items per source, 50,000 per view, 25 concurrent clients, and 500 active claims per authority. Stress-test browsing at 100,000. | User |
| D22 | Measure absolute budgets on an Apple M1 Max with 32 GiB. CI tracks relative regressions. | Design |
| D23 | Source-wide next-ready selection requires the complete scoped graph. A candidate is ready only with a complete, fresh, satisfied hard-prerequisite closure. A known unsatisfied prerequisite establishes blocked even while other edges are unknown; otherwise incomplete/stale/unsupported evidence means unknown. | Workflow contract + v1 fixtures |
| D24 | Reuse existing key policies. GitHub keys use the configured `owner/repo` (or `host/owner/repo`) and issue number. A detected rename, transfer, or Backlog.md ID repair disables claims until an explicit rebind. | Existing + design |
| D25 | An unavailable remote authority never falls back to local. Cached browsing continues read-only with a stale badge. | Confirmed |
| D26 | Support focused state, assignment, and progress edits, plus additional fields only for demonstrated coordination needs. Explicit Start work revalidates, claims, then performs a configured provider transition with separate outcomes. No cross-system atomicity or implicit assignment. | Product discussion |
| D27 | Interpret typed, source-qualified hard edges with named completion conditions, provenance and raw outcome alongside interpretation. Legacy `dependencies` retain terminal semantics; hierarchy, related work, and shared-resource contention are not hard prerequisites. Explain action-specific eligibility and parallel-ready groups; do not infer edges or schedule agents. | Generic contract + v1 fixtures |
| D28 | Agents select and claim in one step. `queue next --claim` (CLI) and `queue_next` with `claim: true` (MCP) walk the `selectNext` order from one snapshot. Each candidate is revalidated and then acquired with the caller's session and no wait. On contention they skip to the next candidate. They hold at most one claim, stop on an uncertain acquire, and never retry in a loop. The caller owns the resulting claim, handle, and heartbeat. `--start` / `start: true` adds the D26 Start work transition with separate outcomes. Provider status such as In Progress is never the lock. | User: concurrent agent loops lost minutes between selection and acquisition |

## 3. Evidence

Probed on 2026-09-22 on the reference machine against this repository's 102 Backlog.md tasks. Re-probe when a pinned tool version changes.

### Backlog.md 1.52.0

| Observation | Consequence |
| --- | --- |
| `task list --json` returns `{kind: task-list, schemaVersion: 1, tasks}` with id, title, status, assignees, priority, ordinal, labels, milestone, parentTaskId, acceptance-criteria counts, createdAt, updatedAt, and isReady in about 0.6 s. | One process enumerates the active project. Summaries never need a process per row. |
| List output and `search --json` omit dependencies. | Edges require `task view --json` for each task. |
| `task view --json` returns dependencies, a transitive `dependencyGraph`, `readiness` with blocking and missing dependencies, body sections, comments, and the file `path` in about 0.5 s. | One call hydrates an explicit item's closure. A full edge scan costs one process per task, about 50 s here. Per-view cost grows with project size; see the scale probe below for 10,000 tasks. |
| `task list --json --watch` re-emits the full list after each change. | Candidate long-lived invalidation source. It still carries no edges. |
| `backlog mcp start` is long-lived, but `task_view` returns formatted text. | Not a structured read path; using it would be undocumented parsing. |
| `task edit` offers `--status`, `--assignee` (replaces the whole list), `--append-notes`, `--comment`, `--check-ac <index>`, and no expected-version option. | Every write is unconditional and coordination-only. Assign to me is a read-modify-write. Checklist writes address criteria by index. |
| `updatedAt` has minute resolution. | It is not a lossless change cursor. |
| `isReady` is computed by Backlog.md with its own completion semantics. | Show it as provider-reported. The generic workflow still builds the graph. |
| Project configuration has `remote_operations`, `check_active_branches`, `auto_commit`, `bypass_git_hooks`, and `filesystem_only`. | In a scratch Git project, `task list --json` and `task view --json` with a nonresolving SSH remote did not invoke Git (no `GIT_TRACE` output) under either remote setting; do not generalize this to other reads. With `auto_commit: true`, `task edit` committed only the task file, left an unrelated staged file staged, and invoked pre-commit once. With `bypass_git_hooks: true`, the task commit still excluded the staged file and did not invoke the hook. |
| `doctor` reports and repairs duplicate IDs without JSON output. Repeated IDs are visible in list JSON. | Detect duplicates from list output. A repair renumbers a task and therefore changes its claim key. |

**Scale probe (2026-09-23).** Apple M1 Max, 32 GiB; Backlog.md 1.52.0; five samples each on deterministic scratch projects with 102, 1,000, and 10,000 records in ten-task dependency chains. The [fixture generator](../scripts/backlog-queue-fixture.py) captures a CLI-created seed and writes only under an empty `/tmp/worklease-queue-*` directory. The [benchmark runner](../scripts/backlog-queue-benchmark.py) uses `/usr/bin/time -l` for per-process peak RSS and measures change-to-complete-JSON emission from `--watch` after changing one fixture title. `task list --json` returned exactly 102, 1,000, and 10,000 records respectively. Wall time and RSS are p50/p95; watch reports change-to-emit p50/p95 in seconds.

| Tasks | List s / MiB | View s / MiB | Watch change-to-emit s |
| ---: | ---: | ---: | ---: |
| 102 | 0.259/0.270 · 88.1/88.8 | 0.258/0.260 · 86.7/87.5 | 0.085/0.088 |
| 1,000 | 0.439/0.453 · 149.0/149.1 | 0.406/0.408 · 144.8/147.1 | 0.266/0.278 |
| 10,000 | 2.049/2.123 · 272.8/275.4 | 1.975/2.376 · 301.1/302.9 | 3.021/3.323 |

`task view --json` cost grows with project size (about 7.7× p50 from 102 to 10,000). An uncached 10,000-item edge scan at four concurrent processes projects to about 10,000 × 1.975 / 4 = 4,938 s (~82 min). This is an **illustrative projection**, not a measured bound: it extrapolates repeated views of one task and assumes ideal four-way parallelism, excluding contention, output, and retries. At most four simultaneous 10,000-item views also require roughly 1.2 GiB peak aggregate process RSS. This rules out cold source-wide next-ready at 10,000 until a bulk dependency field is available; an edge cache can serve only explicitly proven complete, fresh coverage. The watch stream emits a complete list (~3 s at 10,000), not incremental edges.

### GitHub (gh 2.101.0 and docs.github.com)

Live probe on 2026-09-23 against the user-approved `brettinternet/worklease` repository using `gh` 2.101.0. Requests were serial. Mutations used only synthetic, clearly labeled issues, one synthetic label, and one synthetic dependency edge; the edge and label were removed and the created issues closed. Sanitized requests and response evidence are recorded in TASK-126.5 notes. No existing issue content was inspected.

| Observation | Consequence |
| --- | --- |
| `gh issue list --json` exposes `blockedBy`, `blocking`, `parent`, `subIssues`, `subIssuesSummary`, `issueType`, `projectItems`, and `stateReason`. REST dependency add/remove was exercised on a synthetic edge: adding `blocked_by` advanced the blocked issue's `updated_at`; removing it did not. GraphQL `blockedBy` and `blocking` both expose `totalCount` and independent `pageInfo`. | Native dependency and hierarchy data exist on github.com. Dependency removals are not a reliable `updated_at` cursor; reconcile edges independently. Discover support per host, since GitHub Enterprise Server may differ. |
| REST lists `blocked_by` and `blocking` per issue, at most 100 per page, and adds or removes `blocked_by` with Issues read/write permission. | Dependency completeness is per item and paginated. Dependency writes stay out of scope. |
| In the tested REST issues-list representation (`state=all&sort=updated&direction=desc&per_page=1`), an unchanged `If-None-Match` returned 304. Its ETag changed after a synthetic issue edit, comment, label change, and dependency addition. GraphQL `repository.issues` accepted `filterBy: {since}`, cursor paging, and both `CREATED_AT` and `UPDATED_AT` ordering. `totalCount` changed from 2 on the first page to 3 on a later page after a synthetic issue was added between requests. `nodes(ids:)` accepted 100 IDs, rejected 101 with `ARGUMENT_LIMIT`; the tested 100-ID query cost 1. GraphQL returned `rateLimit{cost,remaining,resetAt,used}` and headers included `X-RateLimit-Limit`, `Remaining`, `Reset`, `Resource`, and `Used`. | A 304 saves primary quota for that representation, not all quota or proof of repository freshness. ETags are useful only for the exact representation. ETag behavior after a dependency removal was not measured, and a 304 on the newest-issue list cannot establish edge freshness. Counts are per response, not a multi-page snapshot. Bound `nodes(ids:)` batches at 100 and account for actual query cost. Initial issue edits remain coordination-only; enable conditional writes only with endpoint-specific evidence. |
| GitHub asks integrations to send requests serially, wait at least 1 s between mutations, honor `retry-after` and `x-ratelimit-reset`, and back off exponentially. | One serial, prioritized request queue per host and account. |
| The REST issues list includes pull requests. `gh auth token` accepts `--hostname` and `--user`. | Filter out pull requests. Resolve credentials for an explicit account, never the active one. |

GitHub's [API best practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api) also warn that updated-order pagination moves records between pages and that a 404 can mean missing permission, not deletion. The loading design must account for both. Repository rename, issue transfer, and lost-access behavior were not exercised: the user explicitly prohibited renames, transfers, and permission changes. No GHES instance was provided. These remain unknowns for TASK-128.5; preserve the configured locator, disable claims on identity ambiguity, and never interpret a 404 alone as deletion.

### Worklease authority (commit 9ca754c)

| Observation | Consequence |
| --- | --- |
| `Status` accepts many exact resources. Remote requests are capped at 1 MiB and responses at 4 MiB. | Chunked batch status serves visible rows. |
| `List` is unpaginated. A remote response over 4 MiB fails with `response-too-large`. | Never use `List` for overlays. Large namespaces need a paged read first. |
| Filtered watches accept at most 32 resources. A cursor watch without resources covers the namespace. Each watch polls the store every 50–500 ms (250 ms by default). | Use one namespace watch per authority per client. Every watching client adds a polling loop, so measure at 25 clients. |
| `Events("", n)` returns the tail and a head cursor. Cursors carry authority, restore, and gap semantics, with pages of at most 1,000. | A baseline cursor is cheap to obtain. |
| Heartbeats append `renewed` events. Expiry without a write appends nothing. | Event volume is about active claims divided by the renewal interval. Overlays must schedule their own expiry rechecks. |
| Remote admission defaults to `coordination:` and rejects host-local prefixes. `generic` keys are `coordination:generic:<sha256>`; GitHub keys are `github:…`. | Portable Backlog.md bindings are admitted by default. GitHub claims need an administrator to add `github:`. |
| Profile selection order is `--profile`, `WORKLEASE_PROFILE`, checkout binding, user default, then local. | D11 checks and launch actions reuse this resolution. |

### TUI spike (TASK-128.1, 2026-09-23)

A disposable Go module (not merged) used Bubble Tea 1.3.10, Lip Gloss 1.1.0 and `x/ansi` 0.10.1. On the D22 Apple M1 Max / 32 GiB machine, a list/detail model held 10,000 cached rows, rendered a bounded 35-row viewport, and atomically published a new immutable snapshot every 100 ms. Across 2,500 synthetic `KeyMsg` → `Update` → `View` samples, p50 was 0.138 ms, p95 0.398 ms, p99 0.725 ms (19 background publications; max 7.093 ms). This is an in-process render benchmark, **not** end-to-end terminal input latency; TASK-129.6 measures actual terminal input-to-paint against the 50 ms budget. A second run measured p95 0.156 ms / p99 0.263 ms.

The model changed from split list/detail at 120 columns to list-only at 80 and back. `lipgloss.Width("仕事 🙂")` reported seven cells; CSI, OSC title and OSC hyperlink controls in a test row were stripped before rendering, as were remaining control runes. Under `NO_COLOR=1`, a colored Lip Gloss style rendered `ready` without ANSI styling. An interactive Bubble Tea instance rendered and resized in tmux at 80×25 and 120×35; the tmux capture showed the 10,000-row list. Terminal.app, iTerm2 and Ghostty are installed but **unavailable for controlled interactive verification in this unattended run**; a Herdr-pane job requested confirmation that expired. They remain unverified, not passed. TASK-135 tests all four, plus tmux, on the shipping implementation.

With `CGO_ENABLED=0 go build -trimpath` and identical repo baseline, the synthetic linked import paths increased the binary from 24,063,730 to 25,304,802 bytes (+1,241,072). The throwaway module resolved 25 dependency modules (26 including itself); in the repo baseline `go list -m all` counted 34 modules versus 55 with the UI imports. `govulncheck ./...` on the throwaway module and `govulncheck ./cmd/worklease` on the augmented repo found no vulnerabilities after upgrading its inherited `golang.org/x/sys` to a fixed version. No prototype source or dependency change is part of the shipping tree.

### Prior art in this repository

The Python era shipped a typed source SDK and entry-point resource-policy plugins (TASK-10, TASK-11) before any real external adapter existed. The Go rewrite removed both without replacement ([Go product contract](backlog/docs/go-rewrite/doc-2%20-%20Go-Product-Contract.md), section 16). Derive the protocol from working adapters rather than designing it ahead of them.

## 4. Ownership boundaries

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

Reuse the existing [source-provider contract](../skills/worklease-workflow/references/source-provider-contract.md), [authoring checklist](../skills/worklease-workflow/references/source-provider-authoring-checklist.md), and [generic workflow](../skills/worklease-workflow/references/contract.md). Adapters map provider semantics; they do not each implement selection, scheduling, or claim lifecycle. Extend those contracts alongside implementation rather than creating a second specification. The queue's next-ready selection implements the contract's `selectNext` in one place instead of each agent reimplementing it.

## 5. Local and server behavior

"Local" describes claim coordination, not necessarily the task source. Opening a GitHub source explicitly authorizes remote source reads even with a local claim authority. Merely installing Worklease or using its existing commands must not start network access.

| Source access | Claim authority | Behavior and limit |
| --- | --- | --- |
| Local files or CLI | Local Worklease | No listener or provider login. Coordination covers cooperating processes sharing that authority, not independent databases on the same machine. |
| Remote provider API | Local Worklease | Read and write the provider as the local user. Claims do not exclude workers on other hosts or authorities. Display `local coordination` prominently. |
| Remote provider API | Remote Worklease | Provider calls and credentials stay on each client. Cooperating hosts must use the same authority and exact portable resource keys. |
| Local checkout | Remote Worklease | Only through an explicit portable binding (D12, §6). Host-local `backlog-md:` keys are rejected remotely. A shared claim does not synchronize clones or task files. |
| Later source service | Local or remote Worklease | A separately authenticated service may host adapters and caches. It is not part of the claim server and does not acquire ownership for readers. |

Never let source selection implicitly change the authority. Show the resolved authority profile, ID, and identity scope before acquiring. Reject mismatched or ambiguous source bindings. Check server admission before offering Claim; do not silently switch key policies to pass admission. A saved view can combine sources, but its actionable scope uses one authority; no claim transaction spans authorities.

**Authority consistency (D11).** A CLI or agent working in a checkout resolves its authority from `--profile`, `WORKLEASE_PROFILE`, the checkout binding, or the default. If the view's authority differs from what that checkout resolves, a queue claim and a worker's claim land in different exclusion domains. The queue resolves each checkout-backed source's profile the same way and disables Claim and Launch with an explanation when the authority IDs differ.

**Outages (D25).** A selected remote authority becoming unavailable never falls back to local. Read-only cached browsing continues with an unavailable or stale badge. New claims, renewals, and writes require the relevant live services. An explicit offline view makes no provider or authority requests and cannot authorize mutations.

**Backlog.md Git effects.** With `remote_operations` or `check_active_branches` enabled, a Backlog.md read may contact Git remotes. The source is then treated as remote access and needs the same explicit consent as GitHub. With `auto_commit` enabled, a write creates a commit and may run hooks unless `bypass_git_hooks` is set. The mutation preview shows this effect. Test whether auto-commit captures unrelated staged changes before enabling writes in that mode.

**Branches and worktrees.** A checkout-backed source reads and writes one explicit checkout and shows its branch, HEAD, and dirty state as part of source freshness. The `backlog-md` key policy uses the Git common directory, so linked worktrees share claim identity while their task files may differ. The queue never merges task state across branches or worktrees. Progress written in a feature worktree lands on that branch and becomes visible elsewhere only through Git.

**Later source service.** Support local sources only on explicitly configured service-owned mounts or through a separately designed connector. Never treat a client path as a server path. Do not add an arbitrary remote-command or filesystem API. File hosting needs an authoritative checkout and publishing policy, not just a portable key.

## 6. Identity and interoperability

Keep four identities separate.

| Identity | Purpose |
| --- | --- |
| `Source.id` + `WorkRef.itemID` | Stable provider-qualified item identity. Include provider instance, host, and project scope; retain provider-native immutable IDs when available. |
| Source locator | Current repository name, URL, checkout path, or alias used to find that identity. A rename is not automatically a new item. |
| Exact claim resource + authority ID | The exclusion domain. Every cooperating client must derive identical bytes. |
| Provider principal / Worklease installation / worker session | Access, audit, and ownership context. Not interchangeable, and never part of task resource keys. |

Use existing versioned key policies. Do not invent queue-only keys for tasks that CLI and skill callers can already claim. Preserve source-wide loose-Markdown locking.

**GitHub.** The existing policy uses the normalized supplied locator and issue number, not a resolved immutable repository ID. Use `owner/repo` for github.com, matching existing caller examples, and an explicitly agreed `host/owner/repo` for enterprise hosts. The source binding records the exact inputs and exposes them to CLI and agent callers. The adapter compares the configured locator with the repository the API reports. On a rename or transfer (redirect, changed `nameWithOwner`, or a moved issue), claims are disabled until the binding is explicitly updated with old workers stopped and active claims resolved. Never silently replace legacy keys with immutable-ID keys.

**Backlog.md.** Default keys are host-local even when task files are Git-tracked. The D12 portable binding declares `policy: generic` and a `source` name agreed by every contender; the resource is exactly `worklease key --provider generic --source <name> --item <task-id>`. Adopting this binding is an exclusion-domain migration: stop old workers, resolve old claims and operations, and update CLI, skill, and launch callers together. A worker using the default `backlog-md` policy does not contend with the portable key, even on the same authority. The queue cannot discover or repair all independent claim domains automatically.

Before enabling claims, and again before each acquisition, the adapter rejects the binding when list output contains repeated IDs. Clones can still allocate the same ID independently before a merge. The claim layer then conservatively over-excludes, but task identity and progress references can refer to different work; do not call the overall situation safe. Suspected identity ambiguity disables actions until resolved. A later `doctor --fix` renumbers a task and changes its key. Treat a renumber as an identity migration: resolve claims on affected IDs first. No alias may create a second simultaneously writable claim domain.

**S1 identity vectors (TASK-126.4).** The versioned [shared fixture](../internal/resource/testdata/key-vectors-v1.json) pins key-policy v1 resources for resource, CLI JSON, and remote-admission tests. Host-local paths use explicit temporary checkout substitutions; `${COMMON_DIR}` is the RFC3986-encoded physical Git common directory. The repository-root Backlog.md locator is `.` (not empty), and nested `docs/backlog` remains a distinct resource. Linked worktrees with the same relative locator share a key. GitHub case and trailing `.git` normalize, but a URL-form locator is deliberately distinct from `owner/repo`. Markdown item selectors share a source-wide key. Generic portable bindings and default Backlog.md keys remain different exclusion domains; D12 and D24 are unchanged. The CLI JSON redactor had hidden the computed generic digest as `[REDACTED]`; an explicitly approved output-only fix preserves locally derived coordination digests while raw resource/source/item strings retain credential redaction. No derivation bytes or policy version changed.

Two views of the same provider item deduplicate by canonical identity. Cross-provider mirrors are separate items unless an explicit mapping says otherwise; never infer equivalence from titles or links. Resource derivation, display identity, and migration behavior need fixture vectors before any write-capable adapter ships.

## 7. Source adapter capabilities

Integrations are **source adapters**, distinct from claim authorities and database backends. An adapter must support explicit resolution, capability discovery, summary reads, and authoritative item reads. Everything else is optional.

Capabilities describe semantics, not marketing labels or bare booleans. They are evaluated at adapter, source, principal, and item/action scope. A supported operation can still be unauthorized or temporarily unavailable.

```text
Capability {
  support: supported | unsupported | unknown
  permission: allowed | denied | unknown
  availability: available | unavailable | authentication-required
  semantics: operation-specific facts
  limits: operation-specific bounds
  reason: stable diagnostic when not actionable
}
```

Unknown never means allowed. Read-only discovery never probes a capability by attempting a write. Cache discovered capabilities with an account/configuration generation and invalidate on login, scope, schema, or permission changes. The provider still authorizes every request.

| Group | Facts the adapter declares |
| --- | --- |
| Identity | Stable item IDs, instance identity, aliases, identity scope, key policy and version, rename behavior. |
| Discovery | Pagination and cursor semantics, stable sort and tie-breaker, fields, server-side filters, provider caps, totals as exact, estimated, or unknown. |
| Dependencies | Relationship types, direction, completeness, cross-source references, declared completion conditions, evidence, and terminal/blocked mapping. Hierarchy is not automatically a prerequisite. |
| State | Raw states, normalized categories per project, valid transitions, required fields, reopen/review/archive distinctions. |
| Progress | Structured progress or checklists, notes and comments, history, read-back location, append versus replace. `none` is valid. |
| Assignment | Read/write permission, user or team types, cardinality, add versus replace semantics, worker-to-account mapping. |
| Native claims | Observation and mutation separately: advisory assignment, reservation, or lease; atomicity, TTL, renewal, release, replay, owner authentication, scope, and fencing evidence. |
| Mutation | Minimal patch versus replacement, conditional writes per operation, idempotency or marker support, receipt verification, partial batch semantics. |
| Synchronization | Delta tokens, ordering, tombstones, conditional reads, webhooks, reconciliation needs, quota identity, retry hints. |
| Effects | Side effects beyond the item, such as Git fetches, commits, hooks, or notifications to watchers. |
| Authentication | Local, CLI helper, OAuth, or app options; hosts; scopes; principal identity; expiry and refresh. |

Initial declarations, from §3 evidence:

| Group | Backlog.md 1.52 | GitHub Issues (github.com) |
| --- | --- | --- |
| Identity | Task ID in one explicit checkout; host-local key or D12 binding; renumbered by duplicate repair | `owner/repo` and number; node IDs retained; rename and transfer detected |
| Discovery | Complete list in one call; no cursor; exact observed total | GraphQL cursor pages of 100; observed `totalCount`, not a multi-page snapshot; pull requests excluded |
| Dependencies | Intra-project edges per task view; closure per item; provider-reported `isReady` | `blockedBy`/`blocking` per item with totals; cross-repository references; sub-issues are hierarchy only |
| State | Configured statuses; terminal mapping from caller config | Open or closed with `stateReason`; Projects status deferred |
| Progress | Append notes or comment; criterion-index edits exist but queue writes are initially disabled (§8) | Append comment |
| Assignment | Multiple, replace-all only | Multiple, add and remove endpoints |
| Native claims | Not exposed | Not exposed |
| Mutation | Unconditional CLI edits | Unconditional; no conditional unsafe methods |
| Synchronization | Watch stream or filesystem invalidation; minute timestamps | `since` filter, conditional-GET polling; no client webhooks |
| Effects | Optional Git fetch, commit, and hooks per project config | Notifications to watchers |
| Authentication | OS file access | `gh` helper for an explicit host and account |

Queue-facing operations extend the existing conceptual contract:

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

`readItems` may fall back to bounded individual reads where batching is absent; it must not hide an unbounded fan-out. Every response carries source/principal generation, observation time, coverage, and opaque provider version where one exists. Claim revision, provider version, update timestamp, and sync cursor are distinct values. An update timestamp or read ETag does not imply a conditional-write capability.

Keep raw provider status beside normalized state. Keep readiness, assignment, claim availability, freshness, and dependency completeness separate. A source with no dependency model is never shown as dependency-verified because no edges were fetched; an explicit "this workflow has no prerequisites" declaration differs from unsupported or incomplete discovery.

Use stable structured diagnostics for unsupported capability, authentication, authorization, conflict, rate limiting with retry time, unavailable source, incomplete graph, and unknown outcome. Map them to the existing workflow result vocabulary. TUI and JSON expose the same reasons and disabled-action explanations.

### Dependency interpretation (D27)

The queue's intelligence comes from accurate, explainable relationships, not guesses from task prose. Preserve the provider relationship, its direction, source-qualified endpoints, observation/version, and interpretation alongside normalized edges. Resolving a reference never authorizes a new source, endpoint, or credential scope; inaccessible or unconfigured dependencies remain explicit diagnostics.

| Relationship | Coordination meaning |
| --- | --- |
| Hard prerequisite | Blocks eligibility until its declared completion condition is evidenced. Initially, all hard prerequisites must be satisfied. |
| Parent/child hierarchy | Navigation and grouping only unless an explicit provider or caller policy defines a blocking relationship. |
| Related work | Informational; never a hidden prerequisite. |
| Cross-source prerequisite | Resolve an explicit provider reference or caller-supplied mapping to a stable `WorkRef`. Record its provenance; never infer links from titles or coincident item numbers. |
| Shared resource | Separate claim contention. Compute parallel availability from exact resource overlap, not by inventing a dependency edge. |
| Missing, inaccessible, or cyclic prerequisite | Prevents readiness proof and explains the affected scope without leaking inaccessible item details. |

The initial hard-edge condition is the existing contract's caller-declared terminal state. A closed issue, completed implementation, approved review, and merged change are not interchangeable evidence. Expose the raw outcome and configured interpretation; cancellation, rejection, or `not_planned` must not silently be described as successful implementation. A workflow needing success, merge, or review conditions must declare them explicitly and provide a source that can verify them.

The generic contract now adds optional typed `relationships` alongside the legacy `dependencies: WorkRef[]`. Legacy-only callers keep the caller-declared terminal condition; when both are present, dependencies are the hard-edge projection, not a second graph. Each hard edge carries a named condition and raw outcome beside its configured interpretation. Start with named, evidenced conditions when a real workflow needs them, not arbitrary predicates or executable workflow scripts. A source unable to verify the requested stronger condition yields `capability`/`unknown`, never fallback to “closed means satisfied.” The [v1 dependency eligibility fixtures](../skills/worklease-workflow/examples/dependency-eligibility-v1.json) anchor the core implementation in TASK-128.3.

Apply readiness to each candidate. A current known unsatisfied prerequisite establishes `blocked`, even with other edges still loading. With no established blocker but incomplete/stale evidence, show `unknown`. `ready` requires the complete relevant closure, satisfied hard prerequisites, and no provider blocker. Scope completeness remains a separate fact: an explicitly selected item's closure can be complete while source-wide `next` remains unavailable. Assignment policy and claim occupancy are separate from dependency readiness.

Recompute affected dependents when an edge, prerequisite state, completion condition, or permission changes, including reopen events. Do not cache a transitive graph solely under the selected item's version. Refresh the relevant closure before acquisition and again before provider mutations; this reduces stale decisions but is not a transaction across sources.

Use the generic contract's deterministic ordering for next-ready and, when exposed, parallel-ready groups. Explain each candidate's prerequisites and resource conflicts. A group is a current observation, not reserved capacity or permission to launch a worker wave. Dependency editing, automatic edge inference, OR/conditional workflows, critical-path scheduling, and agent execution stay out of the initial scope.

## 8. Progress, assignment, and write recovery

### Focused operational editing (D26)

The queue should let a worker make the small updates needed to coordinate: enter In Progress, report Blocked, request review, mark completion, assign, or record progress/evidence. Those are workflow intents, not a universal status enum. Present the provider's actual valid transitions and required fields using explicit per-source mappings. GitHub Issues without a configured, supported Projects/label mapping does not gain an invented In Progress state.

State, assignment, and progress are the first write capabilities. Add priority, labels, or other fields only when a concrete coordination workflow needs them and the adapter can preserve unrelated data and verify the result. Full issue-body editing, project schema administration, arbitrary field forms, and bulk/dependency editing are not initial goals. Unsupported fields link back to the provider instead of creating local shadow state.

Eligibility is action-specific. Starting or resuming implementation requires a fresh complete hard-prerequisite closure and dependency readiness. A verified current owner must still be able to report Blocked or record progress when prerequisites change; do not make documenting a blocker impossible because work is blocked. Maintenance still requires action-specific permission, a verified claim, and a provider receipt; it never makes the item start-eligible. Completion requires its declared evidence, not simply a terminal label when a stronger condition is declared. The S1 contract and fixtures define these rules; TASK-128.3 implements them.

### Progress is source-owned

Prefer native structured state and progress fields. Use a note or comment only when that operation is supported and the caller authorizes it. Never invent a percentage from lease time, status, or heartbeat count. A checklist fraction is labeled checklist completion, not project progress.

| Operation | Backlog.md | GitHub | Initial availability |
| --- | --- | --- | --- |
| Change state | `task edit --status` with a configured status | Close or reopen with a reason | Slice 6, after transition tests |
| Record progress | `--append-notes` or `--comment` with marker | Issue comment with marker | Slice 6 |
| Check a criterion | `--check-ac N` targets a mutable index | Body task lists | Read-only initially. Backlog.md needs stable criterion targeting or an atomic precondition; pre/post reads can detect, not prevent, checking the wrong criterion after a reorder. GitHub body replacement lacks compare-and-set. |
| Assign to me | Read-modify-write with `--assignee`; the race is declared | Add-assignees endpoint | Slice 6 |
| Release | No provider write | No provider write | Slice 4 |

If no durable progress write exists, the queue stays useful for browsing and claims but disables Record progress. Never create sidecar task files. Adding Markdown as a separately configured source is an explicit choice, not a fallback write path. Read-only adapters can still support claims, show externally recorded progress, and verify existing provider checkpoints. Nothing may manufacture a completion checkpoint. A human who claims and then abandons an item without work needs a **cancellation**: a release with a non-completion reason, allowed only when the queue started no guarded operation or provider write under that claim. The generic contract now defines this no-effect cancellation without a provider checkpoint. Every other release still requires a verified provider checkpoint.

Claim, Assign, Change state, Record progress, and Release remain separate actions. Claim never marks In Progress or assigns.

### Explicit Start work

Once provider writes ship, offer Start work as an explicit composition, not a new claim primitive:

1. Preview the source, provider actor, authority/resources, configured transition, and any required fields. If no In Progress mapping is supported, offer Claim alone; never invent a status or label.
2. Revalidate the selected item's prerequisites, eligibility, and permissions, then acquire the exact Worklease resources. Contention stops the action before any provider write.
3. Refresh provider state and verify ownership again, then perform the configured transition through the journaled write path below. Assignment remains a separate opt-in action, not a side effect of starting.
4. Read back the result and report each step as applied, rejected, not attempted, or unknown. Retain the claim for the human work session and its normal heartbeat lifecycle.

If a transition fails before any effect, show “Claim acquired; status unchanged” and offer correction or safe cancellation when eligible. An uncertain transition enters recovery and is not retried or automatically rolled back. Resuming uses the same verified private handle and recorded intent, not a new claim or mutation ID. Start work is available only when its component capabilities and authorization are available; it never makes cross-system operations atomic.

### Assignment is advisory

Display assignees beside claim holders. Worklease still provides exclusion when a provider supports only assignment; assignment to a person can outlive many short worker leases. Backlog.md agents in this repository already assign session-named identities such as `@pi-01a094d6`; show them as assignees, never as claims.

By default, suggested work excludes items assigned to someone else. A deliberate selection may override that without reassigning. Assignment to me never permits adopting another session's claim. Multiple workers sharing one provider account still need distinct Worklease sessions. The configured identity mapping (§12) defines which provider accounts and assignee strings mean "me".

Assign to me preserves unrelated assignees. On GitHub that is the add endpoint; on Backlog.md it is a declared read-modify-write race. Release never clears assignment or status. Never advertise assignment mirroring as a distributed lock.

### Every write has a recoverable boundary

1. Check capability, caller authority, exact scope, and the current claim. Refresh the item and required dependency closure; recheck native occupancy and eligibility. Preserve unrelated fields.
2. Persist the exact mutation intent in an owner-private recovery journal before dispatch: operation ID, source and principal, patch, precondition, and claim/operation references. Reuse Worklease guarded-operation and pending-handle machinery for what it owns; do not duplicate its journal.
3. Use conditional writes and idempotency keys only where genuinely supported. Neither initial provider has them, so both run coordination-only with minimal operations. Pre/post reads do not remove the race with external writers.
4. Obtain the provider receipt and independently read back the result. Verify the intended fields, not exit status or HTTP success. Eventual consistency produces bounded pending verification, not optimistic completion.
5. Only after verification, record any Worklease checkpoint and resolve the guarded operation. Release as completed only after the provider checkpoint and operation outcome are established.

**Operation markers (D7).** Append-only writes include `worklease-op:<operationID>`: an HTML comment in GitHub Markdown and a trailing line in Backlog.md notes or comments. The preview discloses the marker; it reveals Worklease use to readers but is random and non-secret. After a lost response, use it to locate a candidate result on the exact intended source/item. Verify the intended appended content against the journal, provider receipt identity and actor where available, and every declared effect required for success. A copied marker, changed content, duplicate match, or unverifiable Git side effect is not proof of the intended complete operation.

If no matching result is visible, the outcome stays `unknown`: read-back can lag a committed write. The marker is a correlation aid, not authentication, an idempotency key, or evidence that the executor has ceased. Never re-dispatch an unresolved write, even with the same marker, and never re-begin its guarded operation. Resolve only after the effect and required cessation evidence are established, or through explicit reconciliation. Never retry under a new ID, auto-reverse, or reacquire to bypass recovery.

If acquiring succeeded but a pre-dispatch check fails, report Claim held / source unchanged and offer a safe release. If a provider write succeeded but checkpointing failed, keep the receipt and finish recovery without repeating the write. Provider failure does not undo a claim, and claim expiry does not undo a provider effect. A provider outage stops further writes, not the worker's bounded heartbeat and recovery.

The source index is disposable; pending intents and receipts are not. Store them separately with restrictive permissions, and retain them only for a bounded period after resolution. Logout and cache eviction never destroy unresolved recovery records. A CAS on an item version, where one exists, protects that mutation but is not evidence that the provider enforces Worklease ownership.

## 9. Distributed and provider-native claims

### Initial policy

All queue acquisitions use the view's selected Worklease authority. Remote coordination works only when every cooperating worker uses that authority and identical resources. Neither local nor remote Worklease excludes arbitrary provider writers or independent native claim systems.

Where a provider exposes native claims, show holder, scope, observation age, and semantics. Fresh native occupancy blocks suggested acquisition by default; unavailable or stale occupancy is never rendered as free. Without the capability, show `not exposed`, not `unclaimed`; Worklease-only coordination still works. Observing an empty native claim and then acquiring in Worklease is not a transaction; a native-only worker can race it. Mixed deployments need a shared coordination policy, not a "globally exclusive" badge.

Never acquire a native claim to mirror Worklease. Two leases add partial acquisition, double renewal, expiry disagreement, and recovery deadlocks.

### Admission criteria for a later native authority

Native claim support is a separate authority integration, not an adapter method substituted for `acquire`. It requires a real provider study and documented contract.

- Atomic acquire-if-free across intended hosts, authenticated ownership, and defined expiry, renewal, and release.
- Authority identity and ownership epoch; stale-owner rejection; defined clock, partition, lost-response, replay, restart, and takeover behavior.
- Scope and bundle guarantees. A single-item provider claim cannot emulate the atomic 1-to-32-resource bundle or cover files, ports, or deployments. Reject unsupported workflows rather than splitting a bundle.
- Honest operation, recovery, and fencing limits. A task reservation alone does not replace the guarded-operation API.
- Passing contention, stale-owner, uncertain-acquire, expiry, recovery, and security tests. Evidence, not a command named `claim`.

An indefinite reservation remains a reservation, not a renewable lease. Selecting native authority is explicit per actionable scope, visible in every receipt, and never activated by discovery or outage. Migration requires a quiescent, audited cutover. Never infer release or executor cessation from TTL expiry, a changed assignee, or a missing row. See [claim guarantees](claim-model.md#guarantees).

### Who renews a claim?

Browsing never renews anyone's claim. A worker using the CLI or MCP owns its handle, session, and heartbeat; the queue observes it. That includes claims acquired through `queue next --claim` or MCP `queue_next` (D28): they use the caller's session and the same contextual handle or lease reference as `worklease acquire`, and the queue never renews them. Launch actions (D18) pass references so the worker acquires its own claim; they never pass bearer credentials.

For an explicit human Claim for me, the queue owns a distinct persisted full-UUID session and renews while that queue process runs: before half the TTL, with jitter and a safe deadline margin, on a control path that source refresh cannot delay. The requested TTL and hold respect the authority's admitted `maxTTL` and `maxHold`. The detail view shows next renewal and last result. After suspend or resume, loss of ownership requires authoritative verification before further work. Network uncertainty disables writes even while a local countdown looks positive.

On exit with owned claims, show the exact consequence. Offer verified release when safe, or stop renewing and leave the remaining lease and recovery state visible. Never release an unresolved operation as completed, create a renewal daemon, or promise ownership after closing. Reopening requires the private handle and live verification, not a matching username. Cross-host transfer remains unimplemented.

## 10. Authentication and trust

Provider authentication is separate from Worklease enrollment. A Worklease write role grants no provider permission, and a provider token grants no claim ownership.

The remote authority's read role can inspect namespace claim metadata; it does not enforce provider ACLs. Treat an authority as a shared coordination trust domain. Never publish titles, descriptions, or private progress in claim metadata. Resource keys can still reveal item existence, and hashing is not access control. Stronger separation needs separately administered authorities or a separately designed authorization change.

| Deployment | Credential behavior |
| --- | --- |
| Local Backlog.md | OS access and the supported CLI. No login. A trusted, explicitly configured checkout. |
| Personal GitHub | `gh auth token --hostname H --user U` for the configured account, run with ambient `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, and `GITHUB_ENTERPRISE_TOKEN` removed unless the source explicitly selects one. Verify `viewer.login` equals the configured account before any write and after every credential change. The token stays in process memory; Worklease never persists it. |
| Other personal integrations | The provider's credential helper, otherwise a native-app OAuth flow or explicit scoped token input. No embedded application secret. |
| Headless or team automation | Provider app or service identities, short-lived scoped credentials, or a caller helper. Record both the initiating worker and the provider actor. |
| Later source service | Server-managed secrets and refresh, separate client authentication, and per-source authorization. Choose explicitly between per-user delegation and a documented service identity. |

Bind credential references to provider origin, tenant or project scope, and principal. Secrets stay in an OS keychain, helper, or protected store; a private file or fd is a headless fallback. Never put secrets on argv or in configuration, keys, receipts, comments, traces, or logs. Sanitize provider stderr as well as JSON.

Offer read-only setup first. Request write scopes only for authorized operations and rediscover capabilities afterward. Serialize token refresh per credential. Distinguish expired credentials, revoked scopes, permission denial, SSO/SAML enforcement, quota exhaustion, and provider downtime. Never prompt for interactive login from an unattended operation; return a structured, actionable error.

Only the user's queue file can install adapters, select secrets, authorize endpoints, or define launch actions. Validate provider URLs, redirects, checkout roots, and custom hosts. A later service also needs SSRF protection and explicit host and mount allowlists.

Partition caches by source instance, tenant, principal/access scope, and configuration generation. Never share payloads across principals. Cached data has explicit retention, and offline use requires consent; revocation cannot erase copies read while disconnected. After revalidation, purge inaccessible projections from every derived index. Keep only the protected recovery material needed for outstanding writes.

**Untrusted content.** Issue titles, bodies, and comments, especially in public repositories, are attacker-controllable input to any agent that reads them. The TUI shows each item's author and whether that author is outside the repository's collaborators, where the provider exposes it. Launch actions pass references, never content, so the worker's own safety policy applies when it reads the item.

## 11. Extensibility

| Seam | Decision |
| --- | --- |
| Source adapters | Built-in Go interfaces first. After slice 6, a versioned out-of-process protocol (D16). |
| Resource policies | Static built-ins only. They define exclusion domains, and divergent plugin-derived keys would silently split them. External adapters select an existing policy, usually `generic`. |
| Claim authorities | Local and remote only. Native provider authority requires §9 admission. No storage plugins. |
| Credential helpers | Built-in `gh`. A generic helper protocol, modeled on Git credential helpers, when a third provider needs it. |
| Views, filters, status maps, keymaps | Declarative user configuration. Readiness and resource derivation stay inspectable and deterministic. |
| Launch actions | User-configured argv templates (D18, §12). |
| Notifications | Later: consume the existing public events and watch stream from outside. No callbacks inside claim transactions. |
| MCP | Existing tools unchanged. Add one `queue_next` tool mirroring `queue next --json`, including `claim` and `start` (D28), once the CLI contract is exercised. Other queue MCP tools wait for demonstrated need. |
| Executable policy, widgets, lifecycle hooks, event bus | Rejected until a concrete requirement exists. |

**External adapter protocol.** JSON-RPC 2.0 over stdio with newline-delimited messages. This is the same framing as MCP stdio, so existing libraries work, but it uses a Worklease-specific method set that mirrors §7. MCP tools are not used as the adapter protocol because they carry no standard pagination, capability, coverage, or receipt semantics; mapping them is exactly the per-provider adapter work. An external adapter may itself call a provider's MCP server or API. The boundary needs:

- A manifest with adapter identity and version, protocol range, config schema, authentication methods, resource policy selection, and declared capabilities. Negotiate the major version. Unknown optional fields may be ignored; unknown required semantics may not.
- Request IDs, cancellation, deadlines, bounded message and collection sizes, pagination, and backpressure. Protocol messages go on stdout, bounded redacted diagnostics on stderr. Use a supervised long-lived process, never one per item.
- Explicit executable and version selection with user approval. No automatic download or execution. A crash isolates its source; a crash after dispatch leaves the mutation uncertain.
- Source-scoped credential references or a narrow helper channel. Never send Worklease bearer credentials. Minimize inherited environment and configuration.
- A fake provider, golden fixtures, identity vectors, and tests for pagination, stale writes, capability denials, cancellation, quota handling, malformed output, uncertain outcomes, and secret leakage. The same suite runs against the built-ins.

Process isolation is not a sandbox. An installed adapter runs with the user's privileges. Document that trust instead of promising credential isolation that stdio cannot enforce.

## 12. Configuration

One owner-private file (D10), strictly parsed like `profiles.yaml`:

```yaml
# $XDG_CONFIG_HOME/worklease/queue.yaml (never read from a repository)
version: 1
me:
  github.com: brett                     # provider account per host
  backlog-md: ["@brett"]                # assignee strings that mean me
sources:
  - id: worklease
    adapter: backlog-md
    checkout: ~/dev/me/worklease
    claims:                             # omit for host-local backlog-md keys
      policy: generic                   # D12 portable binding
      source: brettinternet/worklease/backlog
  - id: acme-api
    adapter: github
    host: github.com
    repository: acme/api
    account: brett
views:
  - name: Ready
    authority: team                     # profile name or local
    sources: [worklease, acme-api]
    filter: {readiness: ready, claim: free, assigned: [me, nobody]}
launch:
  - name: agent
    argv: [/usr/local/bin/start-agent, --ref, "{ref}"]
    cwd: "{checkout}"
```

Launch placeholders are limited to adapter-validated identifiers: `{ref}`, `{sourceId}`, `{itemId}`, and `{checkout}`. Each substitutes into exactly one argv element; no shell runs. The child environment is built from scratch, never inherited wholesale. It contains a fixed allowlist (`PATH`, `HOME`, `USER`, `LOGNAME`, `SHELL`, `TERM`, `LANG`, `LC_*`, `TMPDIR`, and `XDG_*_HOME`), any variables the action names in an explicit `passEnv` list, and `WORKLEASE_PROFILE`, `WORKLEASE_QUEUE_AUTHORITY_ID`, `WORKLEASE_QUEUE_REF`, and `WORKLEASE_QUEUE_RESOURCES` (a JSON array). Variables such as `GH_TOKEN` from the queue's own environment never reach the child unless `passEnv` names them. The environment never includes titles, bodies, or a session ID; the worker generates its own session.

Launch is disabled when the D11 check fails, and while the queue holds a claim on any of the item's resources. In that case the launched worker's own acquisition would fail as already claimed. The user can release (or cancel, §8) and then launch, which is two explicit steps: another worker may acquire in between. The queue shows the worker's claim once it appears and neither supervises nor retries the worker.

The configured launcher must have a tested handoff contract that consumes the exact resources and verifies the authority ID before work. Passing `WORKLEASE_QUEUE_RESOURCES` does not change the existing CLI's key policy automatically; arbitrary commands may ignore it. Do not report a coordinated worker merely because process launch succeeded. API-only sources need an explicitly configured working directory when `{checkout}` is unavailable; unresolved placeholders disable the action. Argv substitution prevents shell expansion, not every target program's option parsing: use explicit value arguments or `--` where appropriate and test option-like identifiers.

Project-suggested sources are deferred. When added, a repository file may only propose sources, which the user copies into this file after a preview. It never proposes adapters, executables, credentials, or launch actions.

## 13. TUI and agent experience

Default to a dense list/detail layout. A board hides dependencies, authority, freshness, and partial loading; it can come later as another view of the same model.

```text
worklease queue  view: All  authority: team (remote 3f9c…)  me: brett  sources 2/2  refresh 8s ago
 Views         ID        Title                    State  Ready    Assigned  Claim      | acme/api#184
   Ready    3  TASK-42   Cache provider pages     To Do  ready    -         free       | Retry backoff for sync
   Mine     5  #184      Retry backoff for sync   Open   ready    brett     me 4m      | Open, by alice (outside)
   Claimed  1  #190      Split queue index        Open   unknown  -         -          | Ready     2/2 prereqs closed
   Recovery 0                                                                          | Assigned  brett
 > All    214                                                                          | Claim     me, queue-7f3c, 4m
 Sources                                                                               |           renews 1m12s, ok
   backlog  ok                                                                         | Native    not exposed
   github   stale 3m                                                                   | Resource  github:acme%2Fapi#184
               3 of 214  total exact  edges 214/214                                    | [Summary] Deps Activity Claims
 j/k move  / filter  enter open  c claim  a assign  p progress  x launch  : command  ? help
```

The mockup is illustrative; not every provider supplies every column. Assignment, native occupancy, and Worklease ownership remain independent facts. A claim badge's detail always includes authority and scope. `Mine` distinguishes assigned to my account from held by my session. Session labels are shortened for display only; stored session identities remain full UUIDs.

Every user-initiated claim/provider action opens a preview first. Heartbeats follow the already authorized session policy without repeated prompts:

```text
 Claim acme/api#184 for me
   Authority  team, remote 3f9c…, cross-host among enrolled clients
   Resource   github:acme%2Fapi#184
   Session    queue-7f3c (this window), TTL 10m, renews while open
   Provider   unchanged: no assignment or state change
   Limits     does not stop writers outside this authority
                                             enter confirm   esc cancel
```

| Keys | Action |
| --- | --- |
| `j`/`k`, arrows, `gg`/`G` | Move, top, bottom |
| `h`/`l`, Tab, Enter, Esc | Pane focus, open, back |
| `/`, `n`/`N` | Filter; next or previous match |
| `c`, `R` | Claim for me; release (capital to avoid accidents) |
| `a`, `s`, `p` | Assign to me, change state, record progress |
| `x`, `o` | Launch action picker; open provider URL |
| `r`, `:`, `?` | Refresh, command palette, help |

Keymaps are remappable (D17). Start work appears in the command palette and detail actions with its separate claim/transition preview; `c` continues to mean Claim only. Dependency details show relationship type, required outcome, observed evidence, coverage/freshness, and the exact reason for ready, blocked, or unknown.

- Search states whether it covers loaded rows, the local index, or the remote source.
- Selection stays anchored by canonical identity during refresh. Never reorder beneath an open preview or steal focus when a late detail response arrives. Preserve scroll position and signal new rows instead of jumping.
- Detail tabs: Summary, Dependencies, Activity, Claims, and Recovery. Long bodies and comments load lazily. Show raw workflow state and why an action is unavailable.
- Distinguish blocked, occupied, assigned elsewhere, unknown dependencies, stale, permission denied, offline, rate-limited, and recovery-required. Never collapse them into "no work".
- No partial-success toast may conceal a held claim or uncertain write.
- Below about 100 columns, switch between list and detail instead of squeezing columns. Support no-color and high-contrast modes, resize, Unicode width, and text labels rather than color-only state. Test Terminal.app, iTerm2, Ghostty, tmux, and Herdr panes.
- Strip terminal control and OSC sequences from provider content, render bodies as bounded, sanitized text with minimal Markdown styling, and never execute embedded commands. Opening a URL or editor requires an explicit action with safe argument passing. The queue never opens raw Backlog.md files for editing, because writes go through the CLI.

**JSON path.** `worklease queue query --view NAME --json` returns the same items, coverage, freshness, authority, capability denials, and action availability as the TUI, with a stable schema version and bounded pagination. Each item carries its exact claim `resources` and `keyInputs` so CLI callers derive identical keys. `--max-age DURATION` serves the index when it is fresh enough and otherwise joins or starts a single-flight refresh. `--require-complete` fails with a structured `incomplete` result instead of returning partial coverage. `worklease queue next --view NAME --json` runs the contract's `selectNext` over a complete scope and returns one candidate or a structured no-work reason. Without flags it does not acquire; the caller claims, and on contention it queries again. For agent loops, `--claim --session ID` selects and acquires in one step (D28). Every candidate it tries is revalidated (prerequisite closure, eligibility, and the identity gate) and acquired with no wait. A contended candidate is recorded with its holder and expiry, and the next one is tried. The first success returns the candidate plus the claim receipt. If contention exhausts the snapshot's ready candidates, the result is the structured `active-claims` outcome. An uncertain acquire stops and is surfaced for pending-request recovery; the command never tries another candidate while a claim might be held. `--start` then runs the Start work transition (§8) and reports claim and transition outcomes separately. MCP `queue_next` exposes the same result schema. No workflow requires screen scraping, and none is safer in the TUI than in automation.

## 14. Remote loading and performance

### Fast first view, honest partial knowledge

Render a bounded page of authorized cached summaries immediately, then revalidate asynchronously. Backlog.md can seed this cache; GitHub payloads remain withheld across processes until live provider revalidation. A cold source fetches one summary page; bodies, comments, expanded dependencies, and history load on demand. Slow or failed sources never block healthy ones, and completion order never changes configured source order or selection order.

The index (D19) is SQLite in WAL mode under the user cache directory, separate from the authority database and recovery journal, with FTS for searchable fields, bounded detail caches, and a schema generation. Rebuilding it is always safe. Each source partition has one refresher at a time across all queue processes, enforced by an advisory file lock. Other processes read the index and wait for or report the in-flight refresh. This keeps agents that call `queue query` in loops from multiplying provider traffic. Indexing every body for search is opt-in background work, and body-search coverage is labeled.

Each query reports coverage, scope, observation time, cursor, exact/estimated/unknown totals, and the reason for incompleteness. Distinguish:

- "No match in the loaded or indexed subset."
- "No match in the complete selected provider scope."
- "This item has a fully checked prerequisite closure."
- "There is no eligible work anywhere in the complete selected scope."

Only the last requires full enumeration plus the complete graph (D23). An explicit item action can hydrate and verify that item's closure; next-ready never chooses from the first page. Cycles, inaccessible or missing prerequisites, exhausted graph budgets, and unsupported dependency semantics block readiness proof. Dependency reads never authorize writes outside the selected scope.

Push down only filters whose meaning is equivalent; evaluate residual filters explicitly. Preserve stable per-source order and deterministic tie-breakers. Never claim a globally complete top-N from independently truncated searches. Cursors bind query, sort, source, principal, and generation.

### Backlog.md loading

- **Summaries:** one `task list --json` per invalidation gives the complete project in one process. Summaries never need a process per row.
- **Invalidation:** use a debounced filesystem watch on the configured checkout's Backlog task directories and Git/config location, then re-list through the existing single-flight refresh. At 10,000 tasks the measured long-lived `task list --json --watch` emits an entire JSON list ~3.021 s p50 after a title change (§3), versus ~2.049 s p50 for a fresh list once a filesystem event arrives; the stream also lacks edges, so keeping it hot duplicates a large parse. A watcher error/overflow or lost channel invalidates all edge observations and triggers re-listing before restart; periodic one-minute reconciliation does the same even without events. Filesystem events are hints, not proof of unchanged content. One-shot queue queries list directly rather than depend on a process-local watch.
- **Edges:** `task view --json` per task, with at most 4 concurrent processes, prioritized as selected-item closure, then visible rows, then background. Partition observations by the explicit checkout and configuration generation. Task ID, `updatedAt`, path, mtime, and size are invalidation hints, not authoritative content versions. Track prerequisite observations separately; an unchanged task file does not mean its dependencies are unchanged. Watch loss, branch/HEAD changes, or uncertain invalidation require provider re-reads, not reusing metadata tuples as proof. Action checks bypass the display cache and do not coalesce with an in-flight background view; the selected closure hydrates newly discovered prerequisites before visible and background rows. Next-ready is available only once the current required graph is complete. Meanwhile show coverage such as "edges 812/10,000", preserve evidenced blockers, and use `unknown` for candidates whose closure is incomplete.
- **Upstream:** [Backlog.md issue #1035](https://github.com/MrLesk/Backlog.md/issues/1035) requests `dependencies` and a per-task content version in `task list --json` and `search --json`. Backlog.md 1.52.0 still omits both fields. The measured 10,000-item view scan is at least ~82 minutes even at ideal four-process throughput (§3); cold source-wide next-ready therefore waits for a bulk dependency field. A warm edge cache can support next-ready only after proving the complete relevant graph and freshness; it cannot turn incomplete observations into readiness. The list itself costs ~2 s and the watch change-to-emit ~3 s at 10,000 items; the initial filesystem-watch choice is based on those measurements, with realistic edit cadence remaining a benchmark input for TASK-129.6.

### GitHub loading

- **Enumeration:** GraphQL `repository.issues` with cursor pages of up to 100. Prefer creation order for full scans so ordinary updates do not continually move records between pages. Select summary fields and bounded `blockedBy` connections with their own pagination/completeness. `totalCount` is exact for that observed response, not an immutable count across a live multi-page scan. Deduplicate and reconcile concurrent additions, removals, and transfers; no provider snapshot is implied.
- **Incremental sync:** `repository.issues(filterBy: {since})` and cursor pagination were confirmed on github.com. Use supported `since` filtering with an overlap window and a fixed scan-start watermark. Persist each fetched page and its resume cursor atomically, but advance the committed synchronization watermark only after the complete window has been scanned. A partial newest-first page must not advance past older unseen changes. Use newest-first updated ordering so an issue edited ahead of the current cursor is revisited by the next overlapping window; stable creation ordering remains the choice for full reconciliation. Dedupe by node ID and revisit overlapping windows. Live probing showed a dependency addition advances the issue's `updatedAt`, but removing the edge did not; updated timestamps and conditional ETags are therefore not a lossless dependency-change feed. Reconcile dependency connections independently and refresh the selected item's closure before action.
- **Change polling:** a conditional GET is valid only for its exact origin, principal, query, page, and representation. The probed newest-issue representation returned 304 unchanged and changed ETags after issue edit, comment, label change, and dependency addition. A newest-issue 304 cannot prove older issues, dependency removals, deletion, or permissions are unchanged. Use it only as an optional hint, never to suppress periodic incremental and full reconciliation. Authorized 304s save primary REST quota, but still cost latency, transport, and potentially secondary-limit capacity. Dependency addition updated `updatedAt`; removal did not. Edge freshness therefore relies on reconciliation plus closure refresh before action.
- **Reconciliation:** use periodic bounded full scans to reconcile membership and visibility. A completed scan can retire rows from the current accessible projection but does not prove deletion. A 404 may mean permission loss; withhold stale content and classify deleted, moved, inaccessible, or unknown only as the evidence permits. Preserve identity and recovery records. No partial scan may retire absent rows. Persist GitHub sync metadata and per-page checkpoints in private index tables keyed by origin, immutable repository identity, principal, and configuration generation, but keep GitHub out of the generic offline cache: cached issue payloads are not exposed without a live access check for the same principal and repository. A fresh process without a verified in-memory projection starts or resumes a bounded full reconciliation before relying on incremental windows; this adds cold-start provider cost (measure in TASK-129.6) rather than silently dropping older unchanged issues.
- **Details:** batched GraphQL `nodes(ids:)` for visible rows; comments lazily.
- **Scheduling:** one serial request queue per host and account within each client process, with priorities action checks, then selected detail, then visible page, then background. Mutations are spaced at least 1 s apart. Honor `retry-after` and rate headers; distinguish rate-limited from offline. Per-source single-flight refresh does not serialize different partitions or hosts sharing an account; measure that aggregate load and use jitter/backoff rather than claiming a global quota coordinator.

### Bounded work

One scheduler per process shares budgets per provider quota identity, cancels superseded reads, deduplicates in-flight fetches, and reserves capacity for authoritative action checks. GitHub serializes per host/account and Backlog.md bounds subprocesses to four per source. This coordination does not extend across processes; independent clients use jitter and backoff, with aggregate load measured in TASK-129.6. Heartbeats run on a separate control path that neither refresh nor indexing can delay. Lifecycle requests still serialize through the per-handle revision and pending-request rules; reserved capacity never bypasses an unresolved request or races two mutations of the same claim.

Debounce search, prioritize visible summaries and the selected detail, and prefetch only adjacent pages. Never make a network call per row while rendering. Cap pending jobs, item and body sizes, decoded responses, dependency traversal, and memory. Retry safe reads, never uncertain writes. Stop background hydration before it spends capacity an explicit action needs.

Treat webhooks, when a later service owns them, as invalidation hints unless the provider proves a complete ordered stream. Handle duplicates, out-of-order updates, tombstones, permission changes, expired cursors, and disappearing fields. Never delete rows missing from an incomplete scan.

### Claim overlay (D20)

1. Read the head cursor with `Events("", 1)`.
2. Batch `Status` the visible and actionable resources. Size chunks for the 4 MiB response cap as well as the request cap, because each claim appears in both `claims` and `resources` and bundles carry up to 32 resources of up to 1 KiB each. On `response-too-large`, split the chunk and retry, since reads are safe to retry.
3. Watch the namespace from the head cursor. Events between the cursor and the snapshot replay harmlessly, so nothing is missed.
4. When an event touches a displayed resource, re-status just those resources.
5. Schedule a re-status at each displayed claim's authority-time `expiresAt`, because expiry appends no event.
6. On a history gap, discard the projection and restart from step 1. On restore or authority change, also stop claim-dependent actions and follow the existing profile identity and recovery procedures before resubscribing. Never silently repin an authority/restore ID or treat snapshot rebuilding as recovery for active handles.

Never use `List` or filtered watches for overlays, and never widen the 32-resource filter limit to attach a whole queue. Provider sync and claim freshness stay independent. Always re-verify before acting; a local countdown is display only. The overlay's restore fail-closed behavior is verified in S3; the active queue-owned handle regression is verified in S4 when Claim for me introduces that handle (TASK-130.1).

### Acceptance budgets

Measure on the D22 reference machine with fixed fixtures and injected network latency. Report p50, p95, and p99, process count, API request count, bytes transferred, RSS, disk and index size, and quota cost. Provider-controlled latency is never a Worklease guarantee.

| Workload | Target |
| --- | --- |
| Warm first view, 10,000 summaries | p95 within 500 ms, with no network dependency before rendering. |
| Navigation and scrolling | p95 input-to-render within 50 ms on cached rows, including during a concurrent refresh. |
| Indexed filter or search, 10,000 summaries | p95 within 100 ms, with no corpus parse or external process per keystroke. |
| Cold remote view | Render within 150 ms of the first summary page arriving, never waiting for full enumeration. |
| View of 50,000 summaries across sources | Navigable, within a provisional 256 MiB RSS budget excluding provider processes, which are reported separately. |
| 100,000-summary browsing stress | Remains navigable with bounded windows. Record the failure boundary. |
| 25 clients and 500 active claims on one remote authority | At a recorded 10-minute TTL and renewal cadence before half-TTL, renewal p99 leaves at least 50% of the TTL margin. Also measure burst load and shorter-TTL saturation; the claim count alone is not a capacity guarantee. Namespace watches do not saturate the store. |
| Rate limiting, a hung adapter, a large graph, and a full refresh together | None of them starves owned-claim renewal or freezes input. Measure the renewal deadline margin. |

These are prototype exit criteria to validate or revise with recorded measurements before scope expands, never a reason to weaken safety.

## 15. Scaling limits and overlooked failure cases

| Risk | Plan now; escalate only with evidence |
| --- | --- |
| Agents calling `queue query` in loops | Single-flight refresh per partition, `--max-age`, and index reads. This is the dominant local traffic multiplier. |
| Backlog.md edge hydration at 10,000 tasks | Edge cache, bounded concurrency, and the upstream bulk field. If view cost scales with project size, next-ready on large projects waits for upstream. |
| Many clients polling one provider account | A per-account serial scheduler, conditional GETs, and jitter. A later permission-aware source service can coalesce reads and own webhooks. A shared service identity does not inherit each viewer's permissions. |
| Namespace watch cost | Each watching client polls the store every 250 ms. Measure 25 clients. If it saturates the store, add server-side watch coalescing before raising client count. |
| Remote `List` response cap | Overlays avoid `List`. Add provider-neutral paging before any feature enumerates claims at scale. |
| Claims and renewals | Load is roughly active claims divided by the renewal interval, plus acquisitions, watches, history, and recovery. Benchmark the single-writer authority separately from source throughput. |
| SQLite and WAL growth | Keep the cache separate from authority state. Track disk headroom, checkpointing, pinned history, and journal growth. Never put the authority WAL on a network filesystem. |
| Authority replicas | Remote mode is one writer, not HA. HA needs separate conformance and restore evidence. |
| Large dependency graphs | Incremental closure reads, cycle detection, traversal bounds, and explicit incomplete outcomes. |
| Hot items and contention | Bounded claim waits and jitter. No FIFO guarantee or server-side work queue. Show contention instead of re-acquiring in a tight loop. |
| Authority split brain | The D11 check. Launch actions pass `WORKLEASE_PROFILE`. |
| Clone-local Backlog.md IDs | Duplicate-ID guard and renumber-as-migration (§6). |
| Backlog.md Git side effects | Declared effects, previews, and consent (§5). |
| Prompt injection through launched agents | References only, provenance display, and the worker's own policy (§10). |
| Tenant and ACL changes | Permission-aware partitioning and invalidation across summaries, FTS, bodies, graphs, and counts. Never leak hidden dependency identifiers through explanations. |
| Cache, reset, and upgrade | Schema-versioned disposable rebuilds; durable recovery migrations separately. Invalid provider cursors, cache loss, plugin upgrades, authority restores, and principal changes are distinct reset causes. |
| Sleep, clock skew, and disconnect | Authority time and lease verification, not wall-clock countdowns. Stop writes on uncertainty and keep recovery evidence. |
| Source disappearance | Distinguish deleted, moved, archived, inaccessible, and temporarily unavailable. Keep claim and recovery references even when the source no longer resolves. |
| Bulk operations | Deferred. Later, per-item results and cancellation boundaries, with no implied cross-source atomicity. |
| Telemetry and privacy | Opt-in diagnostics with bounded identifiers and no bodies or tokens. Measure queue wait, provider latency, cache lag, graph coverage, renewal margin, and unknown outcomes. |

Also test concurrent human edits, required transition fields, workflow schema changes, read-only filesystems, CLI version drift, interrupted login, revoked installations, terminal escape injection, enormous descriptions, and adapter crashes after a write. These are acceptance scenarios, not just error messages.

## 16. Implementation slices

Each slice becomes one parent backlog task with subtasks. Exit criteria become acceptance criteria.

Backlog milestone `m-1` tracks the slices: S1 `TASK-126`, S2 `TASK-128`, S3 `TASK-129`, S4 `TASK-130`, S5 `TASK-131`, S6 `TASK-132`, S7 `TASK-133`, and S8 `TASK-134`. Subtask numbers follow slice numbering; for example, S2.3 is `TASK-128.3`. Each parent depends on its subtasks, and each slice's first subtasks depend on the previous parent. The S2 Bubble Tea spike is the exception and can start at any time.

Preserve existing claim and authorization guarantees. Planned generic workflow extensions must be explicit, documented, and tested with existing callers; they cannot silently weaken safety. Investigations may narrow supported scope.

| Slice | Depends on | Scope | Exit criteria |
| --- | --- | --- | --- |
| S1 Contract, identity, probes | None | Extend the source-provider contract with capabilities, pagination, coverage, freshness, principal, effects, and typed relationship evidence (§7). Define the compatible generic-contract extension for named dependency completion conditions and action-specific eligibility; initially retain terminal-state behavior and reject unsupported stronger conditions. Amend the generic contract with the no-effect cancellation rule (§8). Correct the stale remote-authority statement in the generated Worklease guide through the Backlog CLI. Add identity fixture vectors: `backlog-md` host-local keys, D12 `generic` bindings, GitHub `owner/repo` and `host/owner/repo`, linked worktrees, and source-wide Markdown. Run live probes: whether GitHub dependency edits bump `updatedAt`, ETag behavior, Enterprise field availability where accessible, Backlog.md list and view cost on a generated 10,000-task project, and auto-commit with staged changes. File the upstream Backlog.md request. | Vectors run in tests. Queue-derived and CLI-derived keys are byte-equal. Probe results are recorded in §3 and any affected decisions updated. Fixtures distinguish hard edges from hierarchy, cross-source references, cycles, partial graphs with known blockers, and unsupported completion conditions. |
| S2 Read-only vertical slice | S1 | Bubble Tea spike first, covering input latency, resize, Unicode width, and no-color (confirms D9). Then the queue core snapshot model, `queue.yaml` loading, Backlog.md and GitHub read adapters, the list/detail TUI, `queue query --json` schema v1, claim visibility by batch status, and the D11 check. | Incomplete graphs, unreadable sources, stale cache, and local versus remote scope are visible in both TUI and JSON. No provider writes and no claim ownership. No network access when only local sources and the local authority are selected and Backlog.md remote operations are off. A remote-authority view contacts only the selected authority for coordination and explicitly configured sources for provider reads. |
| S3 Index, sync, overlay | S2 | SQLite index with FTS, single-flight refresh, the per-quota scheduler, GitHub GraphQL enumeration and conditional polling, the Backlog.md edge cache and invalidation choice, reconciliation generations, the D20 overlay, and benchmark fixtures. | §14 budgets met, or revised with recorded measurements. Pagination and cursor tests cover interrupted multi-page sync, changing sort order, newest-page 304 with older changes, and permission loss. A partial scan never advances the committed watermark or proves deletion. Injected-race tests show the overlay never misses a change. The 25-client, 500-claim authority test has recorded results. |
| S4 Claim lifecycle | S3 | Claim for me with a private session, heartbeat on the control path, exit and suspend behavior, admission checks, the D12 binding with its duplicate-ID and migration checks, contention UX, `queue next` with explainable dependency reasons, `queue next --claim` (D28), and the MCP `queue_next` tool. | Remote outages never change the authority. The queue and the CLI contend on identical resources. The renewal margin holds under S3 load scenarios. Native occupancy never renders as global exclusion. |
| S5 Launch actions | S4 | Argv templates, the allowlisted environment, cwd, the D11 and held-claim gates, and display of the worker's claim once it appears. | With secret variables set in the queue's environment, none reach the child unless named in `passEnv`. Hostile titles and option-like IDs cannot inject arguments. Claim then Launch and unresolved cwd are refused with explanations. A tested launcher consumes the handoff, and the worker verifies the same authority and exact resources, including portable Backlog bindings. |
| S6 Provider writes | S4 | Focused state, progress, and assignment operations plus explicit Start work per §8 (including `queue next --claim --start` and MCP `start: true`, D28), with the recovery journal, markers, read-back, cancellation, and recovery UI. Keep unsafe checklist/body writes disabled. | Lost responses and partial failures are injected at each boundary. Start work never writes after contention, never invents a status, and reports claim/transition outcomes separately. A newly blocked owner can report the blocker without authorizing further implementation. A marker with wrong content or duplicate matches is not a verified result. Lagging read-back stays unresolved and never re-dispatches. Cancellation is refused once any write or guarded operation has started. Assignment-only and progress-unsupported sources behave honestly. |
| S7 External adapter protocol | S6 | A JSON-RPC 2.0 stdio protocol derived from both built-ins, plus a manifest, authoring guide, sample adapter, compatibility policy, and shared conformance suite. | The built-ins pass the suite. Tests cover crashes, malformed output, cancellation, and secret redaction. Installation requires explicit approval. |
| S8 Evidence-driven additions | S7 or measured need | Beads, Linear, Jira, or GitLab by demand. A native-authority study per §9. A source service once duplicated traffic, latency, and authorization needs are measured. | Each is its own decision, with evidence recorded here first. |

The upstream Backlog.md bulk-dependency request (`TASK-127`) runs in parallel from S1. When it lands, S3's edge cache becomes a fallback for older versions.

## 17. Open questions

These do not block S1 or S2. Each needs an answer recorded here before the named slice starts.

- Whether namespace watch polling holds at 25 clients or needs server-side coalescing (S3 measurement).
- The shape of provider-neutral claim paging, if any feature needs to enumerate claims (before S4 if `next` requires it).
- Whether to map GitHub Projects v2 status fields; this needs the `project` scope and per-project field discovery (before S6).
- A headless GitHub identity: GitHub App installation versus fine-grained tokens (before any unattended write).
