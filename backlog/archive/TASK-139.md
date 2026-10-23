---
id: TASK-139
title: Add a built-in Jira Cloud source adapter with API-token auth
status: todo
type: feature
priority: medium
labels: [work-queue, deferred]
milestone: m-1
blocked_by: [TASK-142.2]
rank: a6d9601cbe6d95fbc
refs: [docs/work-queue-tui-proposal.md, skills/worklease-workflow/references/source-providers/jira.md]
created: 2026-09-25T16:21:00Z
updated: 2026-10-22T01:23:00Z
---

The user asked for Jira support on 2026-09-25 during TASK-134 (S8 intake), making Jira an accepted S8 candidate alongside Linear. Teams that plan in Jira cannot see its issues in `worklease queue` today, so agents and humans there get no readiness, dependency, or claim view without leaving the tool.

Scope is Jira Cloud authenticated with Atlassian API tokens (email plus token over Basic auth, or scoped API tokens), supplied through the user-configured credential helper introduced with the Linear adapter. OAuth is explored separately in DRAFT-14. Jira Data Center differs in user identity (username vs accountId), comment format (wiki markup vs ADF), search endpoints, and auth; it is out of scope here and needs its own intake decision.

Build it as a built-in adapter, following the Linear decision in TASK-134, and structure it like the Linear parent (probe, identity vectors, read adapter, sync, claims, then focused writes); split into subtasks when picked up.

Known Jira constraints to account for (verify with the probe):
- Issue search moved to `POST /rest/api/3/search/jql` with `nextPageToken`/`isLast` and no exact total; `/rest/api/3/search` with `startAt`/`total` is deprecated and being removed. `approximate-count` only estimates.
- Issue keys change when an issue moves projects; the numeric issue ID does not. The skill's jira.md currently puts the project/filter locator in the `generic` source, which would give a moved issue a new claim key; resolve that explicitly.
- Workflows, statuses, and link-type names are configured per site and project. `statusCategory` (new/indeterminate/done) and `resolution` are the normalized signals; a done status with a non-success resolution is not successful completion (D27).
- JQL `updated` has minute resolution, deleted issues disappear from search, and webhooks need admin or app installation.
- Comments are ADF (no hidden HTML comment for the D7 operation marker); assignment is single-valued by accountId; writes are unconditional.

## Acceptance criteria

- [ ] docs/work-queue-tui-proposal.md records the Jira decision in §2 and §16, a Jira Cloud evidence subsection in §3 from a live probe on a user-approved test project (synthetic issues only, cleaned up), and a Jira column in the §7 declarations table.
- [ ] The probe establishes search pagination and ordering, whether link add/remove changes `updated` on either issue, project-move identity behavior, deleted and permission-lost visibility, transition field requirements, rate-limit responses and headers, ADF comment round-trip for the operation marker, and absence of native claims.
- [ ] A Jira Cloud source is configured in queue.yaml with an explicit site, JQL scope (project or saved filter), account, and credential helper; the token is held only in memory, never persisted or logged, and the authenticated accountId is verified against the configured account before every write and whenever credentials change.
- [ ] Claim keys use the existing static `generic` policy with a site-qualified source and a stable issue identity chosen from probe evidence; identity vectors pin the bytes, queue- and CLI-derived keys are byte-equal, and the skill's jira.md matches the chosen form.
- [ ] A project move, key change, or site mismatch is detected and handled per §6: claims stay correct or are disabled until an explicit rebind, never silently rekeyed.
- [ ] The read adapter lists the full configured scope with coverage that reports totals as estimated or unknown, hydrates items and links, maps user-configured link types to hard prerequisites (hierarchy and other links informational), and keeps raw status beside the normalized category and resolution.
- [ ] Incremental sync and reconciliation work with the queue index and per-quota scheduler; a partial scan never advances the watermark or proves deletion, and a missing issue is not treated as deleted without reconciliation.
- [ ] Claim for me, the D11 check, the identity gate, `queue next --claim`, and MCP `queue_next` work on Jira sources, and remote admission accepts the keys under `coordination:`.
- [ ] Focused writes ship after reads are proven: configured transitions (including Start work) using only transitions discovered for that issue, a comment carrying a verifiable operation marker, and assign-to-me with declared single-assignee replace semantics and explicit confirmation when someone else is assigned; each follows the §8 recovery pipeline with read-back.
- [ ] The adapter passes the shared adapter conformance suite, and tests cover pagination interruption, rate limiting with retry time, permission loss, project moves, non-success resolutions, and secret redaction.
- [ ] User-facing queue docs describe Jira Cloud setup, the required token scopes, and the Data Center exclusion.

## Plan

1. Split the Jira Cloud work into a live probe, identity, read, sync, claim, and focused-write subtasks with explicit dependencies. 2. Run the synthetic-issue probe on a user-approved Jira Cloud test project and update §3/D30/§7 from observed evidence before settling API behavior. 3. Implement identity, read/sync, claims and recovery-backed writes in dependency order, with conformance and security tests; update the skill and queue setup docs. 4. Run repository gates, commit and merge from the owned worktree, then clean it up only after every acceptance criterion is verified.

## Notes

Selected after TASK-143 and all five children were confirmed Done and merged. Created isolated worktree task-139-jira-cloud. Asked for a user-approved Jira Cloud test project; user confirmed none is available. AC1–2 require a live synthetic-issue probe, so API-dependent implementation and completion must await approved site/project and credentials; do not substitute documentation assumptions for observation.

Blocked after planning: no Jira Cloud test project is available (explicit user answer). Resume at TASK-139.1 only after the user supplies an approved Jira Cloud site URL, disposable project key(s) permitting issue creation, linking, movement, transitions and deletion, a test accountId and a private credential helper for an API token with sufficient scopes. Never request the token in chat or create synthetic issues outside that approved scope. No adapter code, live probe, gates or acceptance checks were performed; TASK-139.1–139.6 record the ordered implementation handoff. Keep parent To Do and unassigned until access is supplied.

## Comments

### 2026-10-22T01:23:00Z unknown

Superseded on 2026-10-21: the work queue leaves Worklease (TASK-156). Jira Cloud support moves to bl as BL-40 (probe, owner-gated) and BL-41 (adapter) in github.com/brettinternet/backlog.
