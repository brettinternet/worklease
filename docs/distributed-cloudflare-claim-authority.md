# Distributed Worklease claims on Cloudflare

## Status and decision

Deferred design, revised with the Go product review in TASK-86. No remote
authority, HTTP client, authentication setup, Cloudflare package, or deployment
is part of TASK-85 or the first Go release. The
[Go Product Contract](backlog/docs/go-rewrite/doc-2%20-%20Go-Product-Contract.md)
is normative for the current rewrite; this document records the future design
and decisions to resolve before implementing it.

Keep one end-user `worklease` CLI. A future remote backend can coordinate
cooperating agents on different hosts; the service is a separate deployment
artifact. There is no requirement to preserve Python classes, bundle commands,
wire schemas, owner IDs, state files, or plugin interfaces.

The preferred starting architecture is one SQLite-backed Durable Object per
authority namespace behind a Worker that authenticates and routes requests.
All resources in an atomic multi-resource claim belong to that namespace.
A Durable Object supplies a stable coordination point and transactional storage,
but request handlers may interleave around external awaits. Keep claim decisions
inside storage transactions with no provider network request inside the
transaction. This design follows Cloudflare’s
[coordination guidance](https://developers.cloudflare.com/durable-objects/best-practices/rules-of-durable-objects/)
and [SQLite transaction API](https://developers.cloudflare.com/durable-objects/api/sqlite-storage-api/#transactions).

## Product value and validation

The opportunity is to keep the existing work tracker and coordinate cooperating
agents across machines. The strongest fit is independent laptop, CI, and cloud
executors, especially different agent runtimes selecting from the same backlog.
A single orchestrator or mostly human workflow usually needs less machinery.

| Alternative | What Worklease would add, if missing |
| --- | --- |
| GitHub, Linear, or another tracker | Assignment and agent sessions describe work ownership; they alone are not an atomic, expiring execution claim. Evaluate each provider's actual concurrency guarantees rather than assuming none exist. Keep task status, dependencies, and acceptance evidence in that provider. |
| One scheduler or workflow engine | Coordination across executors outside that scheduler. If all workers already obey one scheduler, use its existing mechanism. |
| Redis, SQL, or etcd coordination | A packaged work-oriented contract, portable resource conventions, atomic multi-resource claims, transfer/checkpoints, exact request replay, unresolved-operation recovery, and CLI/MCP integration. These systems already coordinate across hosts; Worklease does not invent distributed locking or offer categorically stronger guarantees. |

A transactional database workflow can reproduce this contract. The advantage
would be consistent agent integration and recovery semantics without every team
building them, not the choice of storage. All relevant executors must participate.
An unintegrated agent or two clients using different resource keys bypass the
agreement. Task claims also do not detect file overlap or prevent merge conflicts.

This is not a tracker, scheduler, durable job queue, dependency engine, remote
process runner, or exactly-once executor. Avoid adding those products to make the
claim service appear more valuable.

Before authorizing implementation, find two or three teams with recurring,
costly cross-host duplicate execution. Demonstrate two runtimes on two hosts
sharing an existing tracker, including contention, a lost response, a crash,
and a partition. Measure onboarding effort, duplicate attempts avoided, time
blocked on unknown outcomes, recovery effort, and request/storage usage. Compare
against their actual scheduler or provider-native alternative. Stop if those
alternatives already solve the problem with less operational burden.

Cheap infrastructure is useful but is not evidence of product demand. Support,
authentication, recovery, upgrades, and integrations may dominate hosting costs.
Verify current Cloudflare limits and pricing against the measured workload before
deployment. S3 conditional object writes can support coordination designs, but do
not by themselves provide the proposed multi-resource claim plus ledger
transaction. Do not build a second authority backend initially; object storage
is a possible export destination, not the preferred coordinator.

## What the Go rewrite should do now

| Current decision | Why it matters later |
| --- | --- |
| One claim over 1–32 resources and one lifecycle service | Remote atomicity does not need a second singleton/bundle model. |
| Typed service requests/results independent of CLI/MCP, SQL callbacks, and subprocesses | A future client can use the same observable contract without exporting local storage internals. |
| Immutable authorityId in receipts, requests, handles, and opaque cursors | A local path or remote URL is a locator; it must not silently retarget a credential or continuation. |
| Client-generated credentials saved before dispatch; authority retains only hashes | A lost acquire/transfer response does not require decryptable bearer tokens in server receipts. |
| Exact, bounded request replay with retained operation IDs and normalized defaults | Network retries recover the original intent and never duplicate external effects. |
| Per-claim revision for optimistic concurrency | No assumption that revision is a fence across owners. |
| Host-local versus portable identity metadata | Absolute paths and git common directories do not become cross-host repository identities. |
| Explicit operation protection and unresolved predecessor visibility | Renewals do not imply that an old process or provider request has stopped. |

Do not add a backend registry, unused transport interface, remote config flags,
fencing counters, shared wire package, or server scaffolding merely for this
proposal. Extract interfaces at actual consumer boundaries when the remote work
is authorized. V1 uses concrete local services and tests their observable
behavior; future conformance fixtures can reuse those tests’ scenarios.

## Authority and namespace

An authority identity names the coordination domain. An endpoint URL and a local
home locate it. Namespace authorization comes from trusted authenticated server
state, not an arbitrary request field. A request may identify its expected
authority/namespace for validation; that is not an access grant.

Start with one namespace for a private deployment. Use one Durable Object per
namespace and route every mutation for it to the same object. Atomic claims
cannot cross namespaces. Reject such requests before acquiring any member.

Do not shard by individual resource initially: overlapping multi-resource claims
would require distributed transactions or another coordinator. If throughput
later warrants sharding, partition by explicit tenant or repository namespace
and retain the one-namespace atomicity boundary.

SQLite is authoritative. Do not depend on process memory surviving eviction.
Evaluate expiry lazily from authority time; an alarm may perform retention work,
but expiry must not depend on a timer firing. D1 is outside the claim correctness
path; a reporting projection is a separate future decision.

Before deployment, define authority recreation, restore, endpoint changes, and
namespace deletion. Copying a database into another independently writable
service must not create two active authorities with the same identity. A
restored service must not silently reuse a cursor generation or fencing epoch
that downstream clients already observed.

## Resource identity

Contenders must send identical opaque resource bytes to the same authority.
Session, agent, host, worktree, or random invocation IDs must not become part of
the contention identity.

GitHub-style provider keys and deliberately supplied opaque keys can be portable.
The local `path`, Backlog.md and Markdown policies intentionally incorporate
host-local filesystem identity. Remote use of those policies needs an explicit
stable repository/source namespace plus a canonical relative locator. Do not
infer that namespace from whichever Git remote happens to be configured, and do
not silently rewrite an existing local key into a remote key.

Exact resource equality remains the only overlap rule. Directory claims do not
implicitly cover child files, and task claims do not cover the files touched by
that task. Broad source claims and explicit file claims are caller-selected
coordination scopes; use atomic claims when several exact resources are needed.

## Shared remote identities and configuration

A remote identity catalog and a small versioned, non-secret coordination manifest
could distribute agreement between hosts. They cannot discover that two arbitrary
names refer to the same real-world resource, or make an uncooperative client obey.
This is a proposed remote requirement, not new local configuration or a generic
remote configuration framework.

Keep the manifest in the same authoritative namespace initially. An operator
registers stable repository/source identities and vetted aliases. Aliases resolve
to canonical opaque contention keys; the claim service still compares exact
bytes. Store key-policy versions, allowed coordination scopes, and bounded
TTL/hold defaults alongside those mappings. Administrative updates require
authorization, an expected manifest revision, and an audit record.

Each installation bootstraps with an explicitly trusted endpoint, expected
authority/namespace identity, and an authentication source. The downloaded
manifest cannot change those trust anchors or supply executable commands, hooks,
provider credentials, claim tokens, or host-local absolute paths. Remote defaults
cannot weaken server-enforced bounds. Local credential handles stay local; copying
them between hosts is not ownership transfer.

For example, a laptop checkout at `/Users/dev/project` and a CI checkout at
`/workspace/project` can explicitly enroll as the same repository identity.
Their common relative locator `src/main.go` then resolves to the same portable
resource. Specify and test separator, case, Unicode, symlink, and traversal rules
before supporting file-like keys across operating systems. A task ID remains a
different resource from that file; neither implicitly covers the other.

Do not put the host, worktree, session, branch, or manifest revision into the key
for a shared logical resource. A branch/ref belongs in identity only when the
caller deliberately wants separate coordination scopes. Do not infer enrollment
from a Git remote URL. Prefer immutable resource identities so display-name and
alias changes do not change contention.

### Configuration consistency and migration

Clients persist the selected manifest revision, resolved keys, and normalized
request before dispatch. The authority validates managed resolution and policy
in the same serialized decision as admission. Reject stale or incompatible new
requests before acquiring anything. Managed clients must not bypass a rejected
manifest by silently switching to raw keys. Explicit unmanaged opaque resources
remain a separate caller-selected scope, not proof of catalog coverage; define
the admission policy for that scope before deployment.

A manifest change must not re-key an existing lease, alter its saved intent, or
invalidate otherwise valid exact replay. Authenticate and recover the original
request under its recorded policy within its replay window, without admitting
new work under retired policy. Existing lease lifecycle and hold bounds need
explicit version compatibility rules, not reinterpretation using new defaults.
A cached manifest may support resolution and diagnostics, but never offline
acquire, renewal, or ownership verification. The live authority still decides.

A key-changing migration is not a hot alias edit. Pause affected new work
admissions while permitting bounded lifecycle and recovery-only claims under the
old mapping. Drain current ownership and reconcile all unresolved predecessors
with outcome and executor-cessation evidence. Preserve old receipts and history; keep aliases
pointing at the stable identity where possible, otherwise reject retired
locators. Activate the new mapping only after contender compatibility is
established, with no interval accepting old and new keys as independent claims
for the same resource. If recovery evidence is unavailable, migration remains
blocked rather than discarding the unknown operation.

Local-to-remote and authority-to-authority migration also require an explicit
cutover with the old authority unable to admit or renew work. Do not copy live
local handles or create independently writable clones. Changing configuration
alone does not stop an old executor. The cutover procedure must establish
cessation and preserve recovery evidence before enabling the successor domain.

## Remote ledger and recovery

The ledger is useful remotely as shared operational memory across hosts within
one namespace. Keep claim state, epochs, operation records, checkpoints, and
lifecycle events in the authoritative SQLite database. Commit each corresponding
state transition and ledger record in the same transaction. There is no total
order across namespaces and no remote child-process execution implied by this.

Before a guarded effect, the client durably saves its exact request and
credentials locally and receives confirmation for that original start dispatch.
A lost start response requires inspection or exact replay, not another execution.
A retained started result remains unknown; discovering it is not permission to
execute its effect. Work still runs on the host. If completion cannot be reported after a
partition or crash, the ledger retains an unknown outcome even if the effect
succeeded. Replaying the operation never re-executes it. This does not make the
ledger and an external provider one transaction.

Recovery preserves the Go contract. A successor must cover the full transitive
resource set of overlapping unresolved operations. If that set exceeds 32,
report the recovery limitation rather than splitting ownership. Reconciliation
requires the expected request hash and evidence of both outcome and cessation of
the old executor. Evidence is a caller attestation, not proof produced by
Worklease. A checkpoint is bounded recovery context, not verified provider state.

### Access and delivery

Remote reads require namespace authorization, including redacted views called
public in the local contract. Private operation inspection additionally requires
the target epoch credential. The bounded predecessor-checkpoint recovery
exception does not grant a successor access to all historical private receipts
or evidence. Preserve CLI/MCP projection boundaries; never expose credentials,
token hashes, checkpoint bodies, argv, child output, or reconciliation evidence
in public feeds or logs. Record authenticated installation identity separately
from caller-supplied agent display labels.

Events, history, and watches retain authority/feed/filter-bound cursors and
explicit retention gaps. The remote protocol must also bind namespace and restore
generation where not already distinguished by authority identity. Consumers
resume and deduplicate using sequence/cursor identity; delivery is not exactly
once. Capture initial state and cursor coherently so the transition from snapshot
to watch cannot lose intervening changes. Gaps require an explicit resnapshot.

Watches are hints to recheck authoritative state, never permission to mutate.
Expiry remains lazy, so waiting must account for time even without a new event;
free does not mean safe while predecessors remain unresolved. Use bounded,
cancellable long polls or backoff rather than copying the local 50 ms polling
loop over the WAN. Do not hold a storage transaction while waiting. Freeze the
transport only after measuring request volume and disconnect behavior.

### Retention and capacity

Retain started unknown operations and the receipts/authentication needed for
replay and recovery. The current contiguous-prefix retention model lets one
stuck predecessor pin newer history and events indefinitely. Remote deployment
must budget for this explicitly. Do not silently prune unknown operations to
meet a quota or treat an exported record as permission to remove safety state.

Reserve capacity for renewals, inspection, completion, and reconciliation. Apply
backpressure to new admissions before storage or request exhaustion threatens
existing ownership and recovery. Exact limits, alert thresholds, and emergency
behavior remain pre-deployment decisions; no service can promise continued
renewal after its underlying storage becomes unavailable.

Read replicas, dashboards, and S3 exports may be eventually consistent diagnostic
projections. They cannot authorize claims, answer authoritative replay queries,
or establish that an external effect did not happen. This ledger is neither the
project plan nor a complete agent transcript, tamper-proof audit, or compliance
archive. Define retention/deletion policy and access to backups before storing
private recovery context remotely.

## Guarantees and process supervision

A remote lease coordinates cooperating clients across hosts. It cannot revoke
an arbitrary local file edit, kill a child whose supervisor died, or retract an
already-dispatched provider request.

For example, A starts a provider request, loses connectivity, and expires. B
acquires the next epoch. A’s request may still complete after B begins. The
authority can reject stale heartbeats while both provider writes succeed.

| Boundary | Promise |
| --- | --- |
| Claim acquire/renew/transfer/release | Atomic lifecycle within one authority namespace, with explicit coordination scope |
| Client-side exec | Supervised coordination; stop new work when renewal is uncertain, attempt bounded process-group termination |
| Native edit hook | Cooperative ownership and expected-resource check; a later edit still races |
| Direct provider mutation | providerMutationFenced=false unless that provider enforces a conditional write and supplies evidence |
| Local replace-file | Available only under its current local serialized replacement boundary |
| Remote replace-file | Disabled unless a separate authority/provider-side adapter defines and enforces it |

Unresolved started operations remain visible across expiry and replacement.
Acquisition permits a successor to inspect and recover; it does not clear the
unknown outcome. Before new guarded effects, reconcile with evidence of both
the outcome and cessation of the old executor. If a remote provider can still
finish an in-flight operation, that cessation may be impossible to establish:
keep the result unresolved rather than calling it fenced.

Do not add a fencing number without an enforcing consumer. If one is later
needed, it is monotonic per resource across ownership epochs and scoped to the
authority namespace. A many-resource grant has one value per member. Neither a
claim revision nor an event cursor is such a fence. GC, backups, restore, and
authority recreation must preserve its ordering before any provider relies on
it.

## Requests, replay, and credentials

The Go service contract, not the old Python wire schema, is the starting point.
Freeze a separate remote protocol only when this feature is implemented.
Do not reserve URLs or schema-version numbers now.

Normal lifecycle methods use bounded JSON request bodies over authenticated
HTTPS. Bind requests to authority identity and namespace. Reject unsupported
versions, unknown fields, invalid types, oversized bodies and responses, and
return non-cacheable responses. Resource keys and bearer tokens do not belong
in URLs or logs.

Clients retain generated claim/operation IDs, normalized defaults, credentials,
request hashes and retry deadlines before dispatch. Acquire and transfer send
client-generated fresh claim credentials through the authenticated channel;
the authority stores hashes and returns token-free receipts. Exact replay
authenticates the original epoch even after release/transfer and only returns
the original result. A changed request conflicts.

A timeout after dispatch has an unknown commit outcome. Inspect or retry the
exact stored request within the supported replay window; never mint a new claim
or operation because a response was lost. A pending guarded operation never
executes again on replay. Absence after retention is not proof of no effect.
Specify clock-skew tolerance and how clients obtain authority time for bounded
request deadlines before freezing the remote protocol.

Authority authentication and claim credentials are different secrets. The
former authenticates an installation and authorizes namespaces; the latter
authorizes one ownership epoch. Use separate secure credential sources and
redact both. A private deployment may use Cloudflare Access service credentials
or another authenticated front door; validate issuer/audience and namespace
mapping according to the chosen mechanism. Public multi-tenant administration,
billing, quotas and credential issuance are separate scope.

Require HTTPS outside an explicit development mode, finite timeouts,
cancellable waits, and server response validation. A configured remote failure
never falls back to local state. Renew before the deadline with a conservative
network margin; if renewal cannot be confirmed, stop new work and report the
uncertainty. Client clocks do not decide remote ownership.

## Coverage, open decisions, and release evidence

The earlier proposal already described atomic claims, exact replay, immutable
authority identity, no fallback during partitions, and the absence of provider
fencing. It acknowledged portable identity and operational recovery questions
without fully specifying shared configuration or remote ledger behavior. This
review adds those proposed requirements and the product-validation criteria.
Documentation coverage is not an implemented or tested remote guarantee.

| Area | Design coverage and remaining work |
| --- | --- |
| Adoption and integration | Validate the product need and cooperation across actual runtimes. Shared configuration distributes agreement but cannot enforce participation or discover semantic overlap. |
| Identity and manifest | Proposed enrollment, version validation, and migration rules above. Decide canonicalization vectors, managed/unmanaged admission policy, client compatibility, and operator cutover procedure before implementation. |
| Claims and replay | Existing transactional and exact-replay requirements remain. Prove lost-response, partition, clock, restart, and upgrade behavior through the remote protocol. |
| Ledger and recovery | Proposed atomic records, read authorization, cursor continuity, and capacity requirements above. Unknown provider effects can remain blocked indefinitely when cessation cannot be established. |
| Authentication and authority | Choose namespace read/write/admin permissions, installation credential rotation/revocation, historical-epoch access, and authorized cross-host transfer. Revocation must cover pending requests and existing claim use; it cannot retract an external effect. |
| Trust boundary | Start with one trusted cooperative team per namespace. Namespace access is not fine-grained resource isolation against hostile members. Separate namespaces trade away cross-boundary atomic claims; finer ACLs need their own requirement. |
| Scheduling and abuse | Decide maximum holds, hoarding response, wait/backoff limits, and starvation policy. Atomic admission is not FIFO fairness. Administrative revocation must preserve unknown operations and must not be described as executor termination. |
| Availability and cost | Measure WAN latency, renewal margins, retry/watch bursts, namespace throughput and storage growth against current provider limits. Define quotas, observability, recovery reserve, and capacity failure behavior before deployment. |
| Restore and upgrades | Define backup, rollback, schema/protocol compatibility, and disaster recovery procedures. Restoring old state must not revive stale ownership, erase unknown effects, clone an active authority, or reuse observed cursor generations. |
| Data governance | Decide data location, residency, backup access, redaction, and deletion versus replay/recovery retention. Namespace deletion cannot be an undocumented way to erase unresolved risk. |

Secure cross-host transfer needs a separately specified recipient-authentication
and credential-delivery flow. Do not solve it by syncing bearer handles or making
the authority retain decryptable claim credentials. Installation revocation and
recovery by another authorized installation must be designed together so removal
of a compromised installation does not require granting it access again.

Before private deployment, add executable scenarios covering at least:

1. Two hosts with different checkout roots resolve the same catalog identity and
   contend; deliberately separate scopes do not. Unknown aliases, stale managed
   requests, and unsafe path normalization fail without claiming another key.
2. Manifest edits during acquire do not split contention. A key migration with
   active claims or unknown predecessors is rejected. Exact replay after a
   manifest update recovers the old receipt without new effects or ownership.
3. A lost response is recovered from the same authority; a partition stops new
   client effects and never creates local fallback ownership. An old provider
   request completing after expiry remains an explicit recovery problem.
4. Namespace read/write/admin isolation, redacted feeds, historical private reads,
   credential rotation/revocation, and pending requests obey the chosen access
   policy. Cross-host transfer does not copy or expose bearer handles.
5. Snapshot/watch races, disconnect/reconnect, lazy expiry, cursor gaps, and a
   stuck predecessor pinning retention preserve recovery state. Capacity pressure
   rejects new work before consuming the defined recovery reserve.
6. Restart, rolling protocol/schema upgrades, and backup restore preserve replay
   and unknown-operation semantics. A rollback cannot silently restore old active
   claims or accepted cursor/epoch state; cloned authorities cannot both serve.

These are future acceptance scenarios, not assertions about shipped behavior.
A private service must have named operational owners and runbooks, not only low
infrastructure cost. Public hosting, billing, and multi-tenant administration
remain separate decisions after a private deployment demonstrates value.

## Deployment and implementation boundary

Keep client and service in this repository initially so protocol fixtures and
conformance changes can be reviewed together. Ship the Go CLI and Worker as
separate artifacts. Wrangler or equivalent service tooling owns deployment;
`worklease` remains the claim client, not a cloud provisioning tool.

When the feature is authorized:

1. Specify authority/namespace identity, portable keys, authentication,
   request deadlines, retention, restore, and explicit operation capabilities.
2. Extract the smallest consumer interface from the existing local services and
   run authority conformance scenarios through it.
3. Implement one namespace with atomic claims, exact replay and retained
   unknown-operation recovery; do not release a singleton-only protocol.
4. Add the Go HTTP client, explicit selection, credential handling, and
   partition/lost-response tests. Keep local execution separate.
5. Verify real cross-host contention, stale-owner rejection, server restart,
   authorization isolation, cursor gaps, and degraded connectivity before
   deploying a private service.

No remote implementation task is made ready by the Go rewrite. Resolve the
remaining operational choices when a real deployment is requested; keep
sharding, D1 reporting, provider-executed mutations, and a public hosted product
deferred until they have a concrete requirement.
