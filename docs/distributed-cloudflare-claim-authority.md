# Distributed Worklease claims: remote authority design

## Status and decision

Deferred design. No remote authority, HTTP client or server, authentication
setup, or deployment is part of the shipped Go release. The
[Go Product Contract](backlog/docs/go-rewrite/doc-2%20-%20Go-Product-Contract.md)
is normative for the current product; this document records the future design,
the decisions already made, and the few decisions that remain open. The file
name is historical: the first draft proposed a Cloudflare Durable Object
authority, and that alternative is evaluated and rejected below.

Resolve safety requirements in this design before remote implementation begins.
Implement the mechanisms and executable acceptance scenarios only when that
work is authorized; this design does not authorize changes to the local schema,
CLI, service, or deployment. Documentation records intended behavior, not a
shipped or verified guarantee.

**Decision (2026-09-12, owner-authorized pivot).** The remote authority is the
existing Go authority, served over authenticated HTTPS. One `worklease serve`
process runs the same `lease.Service` and SQLite store that the local CLI uses,
on one always-on host with one persistent volume. Optional continuous SQLite
replication to object storage can provide disaster-recovery backups when a
deployment's recovery targets require them; a simple `worklease serve` may rely
on its persistent SQLite volume alone. An authenticated front door such as
Cloudflare Tunnel plus Cloudflare Access terminates TLS and authenticates
installations; where compute runs is decoupled from that front door.

The single strongest reason is that nothing hard about this system is
transport. The difficulty is claim, replay, reconciliation, and garbage
collection semantics, which already exist and are tested in Go. A second
implementation of that logic in another language duplicates it forever, and its
divergence fails silently as duplicate execution or a lost started operation.
Hosting cost does not decide this architecture; comparable cost remains an
assumption until a deployment and workload are measured.

Keep one end-user `worklease` CLI. There is no requirement to preserve Python
classes, bundle commands, wire schemas, owner IDs, state files, or plugin
interfaces.

### Alternatives considered

| Option | Why not, or when |
| --- | --- |
| One SQLite-backed Cloudflare Durable Object per namespace behind a Worker | Reimplements every safety invariant in TypeScript plus a cross-language conformance suite. Input/output gates and write coalescing are a different serialization model than the tested `BEGIN IMMEDIATE` boundary. Per-object storage is a hard vendor cap that conflicts with a retention model that must never prune unknown operations. Its real advantages, near-zero patching and 30-day point-in-time recovery, do not outweigh a permanent second implementation. |
| Cloudflare Containers, or Go compiled to Wasm inside a Worker | No durable per-instance disk, so state still lives in Durable Object storage behind a JavaScript API and the store is rewritten anyway. |
| Turso or libSQL | Keeps the dialect but adds a network hop per statement and a vendor dependency without removing the single-writer server process. |
| Managed Postgres behind the Go service | The escalation path if single-host durability ever becomes the binding constraint. It costs a Postgres port of the store, not a second authority. Not needed for a private deployment. |
| SSH-forwarded authority, for example `ssh host worklease acquire ...` | Validates shared claim contention using existing commands. Handles and token files/descriptors belong to the host running the CLI; secure credential forwarding needs explicit setup. Does not provide client-local guarded execution against a remote authority. Not a product architecture. |

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
costly cross-host duplicate execution. Start with SSH-forwarded claim contention:
two runtimes on two hosts sharing one existing tracker through one authority.
That narrow experiment needs no product changes. It does not validate the full
guarded-operation recovery contract: the CLI exposes operation inspection and
reconciliation, but not separate begin/complete commands, and `exec` runs its
child where the CLI runs. Token files and descriptors are read on that host too.

If contention validation supports proceeding, separately authorize a minimal
validation harness over the existing Go service methods, with durable requests
on each client and effects running on the client hosts. Exercise a lost start
response, a crash, and a partition before treating remote recovery as validated.
An SSH child running on the authority host is not evidence for those client
failure boundaries. Measure onboarding effort, duplicate attempts avoided, time
blocked on unknown outcomes, recovery effort, and request/storage usage. Compare
against the actual scheduler or provider-native alternative. Stop if those
alternatives already solve the problem with less operational burden.

Cheap infrastructure is useful but is not evidence of product demand. Support,
authentication, recovery, upgrades, and integrations dominate hosting costs.
Object storage is the replication and export destination, not a coordinator; S3
conditional writes do not provide the multi-resource claim plus ledger
transaction. Do not build a second authority backend.

## What the Go release already provides

| Current property | Why it matters remotely |
| --- | --- |
| One claim over 1–32 resources and one lifecycle service | Remote atomicity does not need a second singleton/bundle model. |
| Typed service requests/results independent of CLI/MCP, SQL callbacks, and subprocesses | The server binds the same methods; the client uses the same observable contract. |
| `BeginOperation`, `RenewOperation`, and `CompleteOperation` separate from the local effect callback | The server exposes the former; `RunGuardedOperation` and its effect stay on the client host. No remote process runner. |
| One pooled write connection, `BEGIN IMMEDIATE`, WAL, synchronous FULL | The single-writer serialization and durability posture a hosted authority needs already exist. |
| Immutable authorityId in receipts, requests, handles, and opaque cursors | A local path or remote URL is a locator; it must not silently retarget a credential or continuation. |
| Client-generated credentials saved before dispatch; authority retains only hashes | A lost acquire/transfer response does not require decryptable bearer tokens in server receipts. |
| Exact, bounded request replay with retained operation IDs and normalized defaults | Network retries recover the original intent and never duplicate external effects. |
| Per-claim revision for optimistic concurrency | No assumption that revision is a fence across owners. |
| Host-local versus portable identity metadata | Absolute paths and git common directories do not become cross-host repository identities. |
| Explicit operation protection and unresolved predecessor visibility | Renewals do not imply that an old process or provider request has stopped. |
| Static, cgo-free binary | The server is the same artifact as the CLI, cross-compiled for the host. |

Do not add a backend registry, unused transport interface, remote config flags,
fencing counters, or server scaffolding before the remote work is authorized.
The server needs no interface extraction: it calls the lease service directly.
Only the client needs a local/remote selector, added when the feature is built.

## Authority, namespace, and process

An authority is one SQLite database served by exactly one process. The
authority identity is the immutable random authorityId already created at
bootstrap. An endpoint URL and a database path locate it; neither is identity.
Clients validate authorityId on every response, so an endpoint can move without
retargeting credentials or continuations. Two endpoints serving the same
authorityId is the clone problem below and is prohibited.

A namespace is an authority. Start with one namespace for a private deployment.
One `serve` process may host several namespaces as separate databases with
separate authority IDs; atomic claims never cross them. Reject a cross-namespace
request before acquiring any member. Namespace authorization comes from trusted
authenticated server state, never from a request field. A request may name its
expected authority for validation; that is not an access grant.

Do not shard a namespace by resource: overlapping multi-resource claims would
require distributed transactions. If throughput later warrants partitioning,
partition by explicit tenant or repository namespace and keep the one-namespace
atomicity boundary. One serialized writer per namespace is the throughput
ceiling; measure before deciding it is a problem.

SQLite is authoritative. Nothing depends on process memory surviving a restart.
Expiry is evaluated lazily from authority time; a background task may perform
retention work, but expiry never depends on a timer firing. Never place the
authority behind a load balancer with more than one origin.

### Single writer guard

A hosted deployment introduces one new invariant: a rolling deploy, blue-green
cutover, or a hostname pointing at two machines must not create two writable
databases with the same authorityId. Deploys are stop-before-start on one
volume. The server holds an exclusive process-lifetime OS lock for the hosted
authority on that volume. All authority writers, including alternate CLI and
maintenance entry points, must respect it; they cannot bypass the server to
mutate a hosted authority. The lock remains held while a process is paused and
is released when it exits. Keep the lock file stable while held; unlinking and
recreating it must not admit a second holder. Reuse the pinned-handle technique
from `internal/handle`: open the lock file, take `flock`, then verify the opened
device and inode still match the directory entry before trusting the lock.

Do not use a startup-only expiring database lease: a paused server can resume
after another server adopts it. SQLite transaction serialization alone does not
enforce one serving process. The OS lock is a local safety check for one volume,
not protection against independent writable clones or a substitute for the
deployment rule. Stop-before-start upgrades must respect the same lock.

### Restore incarnation and quarantine

Replication is asynchronous, so losing the host loses the tail of committed
writes, including recently committed started operations. Restore therefore is
authority recreation, never resumption. Add a cryptographically random
`restoreId` to `meta` at bootstrap and bind it into receipts, handles, requests,
and cursors alongside authorityId. Every restore from a replica, database import,
or recreation generates a fresh value before serving requests; an ordinary
restart preserves it. Compare incarnations for equality, not ordering. A counter
inside the backup is insufficient: restoring the same generation-1 backup twice
would produce generation 2 twice and accept requests from rolled-back state.

Asynchronous backup provides disaster recovery, not seamless failover or zero
acknowledged-write loss. For example, [Litestream's replication](https://litestream.io/how-it-works/)
copies WAL changes asynchronously. Set acceptable data loss and recovery downtime
before choosing the deployment, and measure replication lag and restore drills
against those targets. Safe reopening can remain blocked longer than a target
if outcome or executor-cessation evidence is unavailable.

After a restore the authority:

1. ends every active claim with reason `restored`, effective at restore time,
   and leaves every started operation unresolved;
2. rejects requests, replays, and cursors bound to another incarnation with
   `authority-restored`, returning the current restoreId so the client fails
   closed and preserves its old requests as recovery evidence;
3. quarantines the namespace: permits authorized inspection, audited recovery
   import, and bounded recovery-only claims and reconciliation, but rejects
   normal admissions and new guarded effects until explicitly reopened.

The restored ledger cannot enumerate started operations lost from the backup
tail. A client may have received start confirmation and dispatched a provider
mutation before that start record was replicated; an offline client will not
immediately report it. A clean restored ledger alone is not reopening evidence.

Reopening requires an audited administrative action recording cessation of the
old authority, an installation inventory and revocation state verified
independently of the restored snapshot, and recovery evidence from those
installations or equivalent independent evidence. Account for outstanding work,
including confirmed starts, using saved requests and provider observations.
An unavailable installation or missing evidence keeps the namespace quarantined
unless equivalent evidence establishes its outcomes and executor cessation.

Define audited recovery import as an explicit remote service extension. An admin
request bound to the current incarnation records a missing operation as unknown,
preserving its original authority/incarnation, claim and operation IDs, request
hash, full resource set, and evidence provenance. Imports are idempotent for that
original identity; conflicting evidence fails closed. They do not fabricate a
successful receipt, revive old credentials, or authorize redispatch. Retain them
as unresolved safety state and make them visible to the same overlap/recovery
checks as restored started operations. Reconcile under current recovery-only
ownership, with outcome and executor-cessation evidence. The current Go
reconciliation method rejects missing targets, so import is required new work.
Reopen normal admissions only after all inventoried uncertainty is reconciled.

Within one incarnation, an unexpired request's absence from the same healthy
authority establishes absence at that read, not cancellation of an in-flight
dispatch. Only the identical saved request may be retried under exact replay.
Across incarnations, absence cannot exclude a lost commit; recover rather than
redispatch. Copying a live database to a second writable location is never a
supported operation; a copy is usable only through the quarantined restore path
under a fresh incarnation.

### Namespace deletion and cutover

Namespace deletion refuses while active claims or unresolved started operations
exist. An operator may force deletion only after exporting the redacted list of
unresolved operations, which is recorded in the deployment's administrative log
outside the deleted database. Deletion is never an undocumented way to erase
unresolved risk.

Local-to-remote and authority-to-authority migration require an explicit cutover
with the old authority unable to admit or renew work. Do not copy live local
handles or create independently writable clones. Changing configuration alone
does not stop an old executor; the cutover procedure establishes cessation and
preserves recovery evidence before enabling the successor domain.

## Resource identity

Contenders must send identical opaque resource bytes to the same authority.
Session, agent, host, worktree, or random invocation IDs must not become part of
the contention identity. The authority never normalizes or interprets a key.

Host-local policy keys, that is `path`, `backlog-md`, and `markdown`, embed an
absolute host path and are always rejected by a remote authority with
`resource-not-enrolled`. Remote use of those policies requires an enrolled
repository identity plus a canonical relative locator (below). Do not infer that
identity from whichever Git remote happens to be configured, and do not silently
rewrite an existing local key into a remote key.

Exact resource equality remains the only overlap rule. Directory claims do not
implicitly cover child files, and task claims do not cover the files touched by
that task. Broad source claims and explicit file claims are caller-selected
coordination scopes; use atomic claims when several exact resources are needed.

### Canonical relative locator

A portable file-like key is `<policy>:<repositoryId>:<locator>` with each
component percent-encoded as the local policies already do. The locator rules
are fixed:

- the path bytes exactly as Git records them in the index or tree, so every
  checkout of the enrolled repository agrees without filesystem-specific
  normalization; for a file not yet tracked, the bytes the client will hand to
  Git;
- UTF-8, no NUL, CR, or LF, `/` separators only, no backslash, no empty, `.`,
  or `..` segments, no leading or trailing `/`;
- byte-exact, case-sensitive comparison, no Unicode normalization: NFC and
  NFD spellings are different keys, and a team on a case-insensitive or
  NFD-normalizing filesystem still agrees because Git's recorded bytes are the
  source of truth;
- symlinks are resolved inside the repository before enrollment, mirroring the
  local `path` policy; a target outside the repository is rejected.

Share these rules as cross-platform conformance vectors before supporting
file-like keys remotely. A task ID remains a different resource from that file;
neither implicitly covers the other.

## Shared remote identities and configuration

A remote identity catalog and a small versioned, non-secret coordination manifest
distribute agreement between hosts. They cannot discover that two arbitrary
names refer to the same real-world resource, or make an uncooperative client obey.
This is a remote requirement, not new local configuration or a generic remote
configuration framework.

The manifest lives in the same authoritative namespace database. An operator
with the `admin` role registers stable repository/source identities and vetted
aliases. Aliases resolve to canonical opaque contention keys; the claim service
still compares exact bytes. The manifest also stores key-policy versions, the
admission policy below, and TTL/hold bounds. Administrative updates require an
expected manifest revision and produce an audit record.

Each installation bootstraps with an explicitly trusted endpoint, the expected
authorityId, and an authentication source. The downloaded manifest cannot change
those trust anchors or supply executable commands, hooks, provider credentials,
claim tokens, or host-local absolute paths. Remote defaults cannot weaken
server-enforced bounds. Local credential handles stay local; copying them between
hosts is not ownership transfer.

For example, a laptop checkout at `/Users/dev/project` and a CI checkout at
`/workspace/project` explicitly enroll as the same repository identity. Their
common locator `src/main.go` then resolves to the same portable resource.

Do not put the host, worktree, session, branch, or manifest revision into the key
for a shared logical resource. A branch/ref belongs in identity only when the
caller deliberately wants separate coordination scopes. Prefer immutable resource
identities so display-name and alias changes do not change contention.

### Admission policy

A resource is admitted when it resolves through an enrolled identity, or when
its policy prefix is one the manifest lists as an allowed portable policy, for
example `github:` or `coordination:`. Everything else fails
`resource-not-enrolled` before any write. Managed clients must not bypass a
rejected manifest by switching to raw keys. TTL or hold values above the
namespace bounds are rejected, not clamped: clamping would change the hashed
intent and break exact replay.

### Configuration consistency and migration

Clients persist the selected manifest revision, resolved keys, and normalized
request before dispatch. The authority validates managed resolution and policy
in the same serialized decision as admission. Reject stale or incompatible new
requests before acquiring anything.

A manifest change must not re-key an existing lease, alter its saved intent, or
invalidate otherwise valid exact replay. Recover the original request under its
recorded policy within its replay window, without admitting new work under
retired policy. Existing lease lifecycle and hold bounds keep the version they
were admitted under. A cached manifest may support resolution and diagnostics,
but never offline acquire, renewal, or ownership verification.

A key-changing migration is not a hot alias edit. Pause affected new work
admissions while permitting bounded lifecycle and recovery-only claims under the
old mapping. Drain current ownership and reconcile all unresolved predecessors
with outcome and executor-cessation evidence. Preserve old receipts and history;
keep aliases pointing at the stable identity where possible, otherwise reject
retired locators. Activate the new mapping only after contender compatibility is
established, with no interval accepting old and new keys as independent claims
for the same resource. If recovery evidence is unavailable, migration remains
blocked rather than discarding the unknown operation.

## Remote ledger and recovery

The ledger is shared operational memory across hosts within one namespace. Claim
state, epochs, operation records, checkpoints, and lifecycle events stay in the
authoritative SQLite database, and each state transition commits with its ledger
record in one transaction, exactly as today. There is no total order across
namespaces and no remote child-process execution.

Before a guarded effect, the client durably saves its exact request and
credentials locally and receives confirmation for that original start dispatch.
A lost start response requires inspection or exact replay, not another execution.
A retained started result remains unknown; discovering it is not permission to
execute its effect. Work still runs on the host. If completion cannot be reported
after a partition or crash, the ledger retains an unknown outcome even if the
effect succeeded. Replaying the operation never re-executes it. This does not make
the ledger and an external provider one transaction.

Recovery preserves the Go contract. A successor must cover the full transitive
resource set of overlapping unresolved operations. If that set exceeds 32,
report the recovery limitation rather than splitting ownership. Reconciliation
requires the expected request hash and evidence of both outcome and cessation of
the old executor. Evidence is a caller attestation, not proof produced by
Worklease. A checkpoint is bounded recovery context, not verified provider state.

Recovery-only claims are an explicit service extension used during restore,
key migration, and capacity pressure. They cover the full required resource set
under the same atomic ownership rules and 32-resource limit. Admission requires
an unresolved recovery target, bounded TTL/hold limits, and reserved capacity.
The authority rejects new guarded effects under these claims, preserves the
restriction across renewal and transfer, and never upgrades them in place.
Normal work requires a fresh ordinary claim after recovery and admission reopen.

### Access and delivery

Remote reads require namespace authorization, including redacted views called
public in the local contract. Private operation inspection additionally requires
the target epoch credential, or the `admin` role as defined below. The bounded
predecessor-checkpoint recovery exception does not grant a successor access to
all historical private receipts or evidence. Preserve CLI/MCP projection
boundaries; never expose credentials, token hashes, checkpoint bodies, argv,
child output, or reconciliation evidence in public feeds or logs. Record the
authenticated installation identity on every mutation, separately from the
caller-supplied agent display label.

Events, history, and watches retain authority/feed/filter-bound cursors and
explicit retention gaps, extended with the restore incarnation. Consumers resume
and deduplicate using sequence/cursor identity; delivery is not exactly once.
Capture initial state and cursor coherently so the transition from snapshot to
watch cannot lose intervening changes. Gaps require an explicit resnapshot.

Watches are hints to recheck authoritative state, never permission to mutate.
Expiry remains lazy, so waiting accounts for time even without a new event; free
does not mean safe while predecessors remain unresolved. The remote watch is a
bounded long poll of at most 30 s per request, matching the local default watch
timeout, with client-side backoff and jitter on errors. The local adaptive
50–500 ms poll stays inside the server. WebSockets are deferred until request
volume and disconnect behavior are measured. The server never holds a storage
transaction while waiting.

The authority never blocks an acquire. `--wait` is a client loop of acquire
attempts paced by watch long polls with jitter. There is no server-side queue,
and atomic admission is not FIFO fairness.

### Retention and capacity

Retain started unknown operations and the receipts/authentication needed for
replay and recovery. The contiguous-prefix retention model lets one stuck
predecessor pin newer history and events indefinitely. Because the binding
constraint is now volume size rather than a vendor cap, the deployment budgets
for this explicitly: a disk usage alert wired to the retention model, and a
volume that can grow. Never prune unknown operations to meet a quota or treat an
exported record as permission to remove safety state.

Backpressure protects existing ownership and recovery before storage or request
exhaustion threatens them. Above an admission ceiling, new ordinary acquisitions
and operation starts fail `capacity-exhausted`; existing ownership lifecycle,
inspection, operation completion, and reconciliation remain available. Reserve
bounded capacity for recovery-only acquisitions and recovery imports too: an
expired predecessor cannot be reconciled without a current resolver claim.
Authenticate and validate authority/incarnation before recognizing exact replay;
recover an existing receipt before applying new-admission checks. Crossing a
capacity threshold must not turn a committed acquire into a failed new attempt.

Above a higher hard threshold, new checkpoint writes are also rejected. Reserved
headroom supports bounded recovery and essential lifecycle writes, not unlimited
renewals or imports. Include WAL growth, pinned history, and backup lag in volume
monitoring; measure operation sizes and bound recovery request/storage usage.
The two thresholds have provisional defaults of 80 and 95 percent of the volume,
to be validated under load. If the reserve cannot support safe recovery, expand
storage and keep normal admissions closed. No service can promise continued
renewal after its underlying storage becomes unavailable.

Read replicas, dashboards, and object-storage exports are eventually consistent
diagnostic projections. They cannot authorize claims, answer authoritative replay
queries, or establish that an external effect did not happen. This ledger is
neither the project plan nor a complete agent transcript, tamper-proof audit, or
compliance archive.

## Guarantees and process supervision

A remote lease coordinates cooperating clients across hosts. It cannot revoke
an arbitrary local file edit, kill a child whose supervisor died, or retract an
already-dispatched provider request.

For example, A starts a provider request, loses connectivity, and expires. B
acquires the next epoch. A's request may still complete after B begins. The
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

## Requests, replay, and time

The Go service contract is the protocol's starting point. Freeze the remote
protocol only when this feature is implemented; do not reserve URLs or
schema-version numbers now.

Lifecycle methods use bounded JSON request bodies over authenticated HTTPS,
mapped onto the typed service requests. Recovery import, recovery-only admission,
and transfer preparation require explicit extensions to that service contract;
their transactional invariants cannot be implemented by independent HTTP writes.
Bind requests to authorityId and restoreId. Reject unsupported versions, unknown
fields, invalid types, oversized bodies and responses, and return non-cacheable
responses. Resource keys and bearer tokens do not belong in URLs or logs.

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

### Authority time

Client wall clocks never decide remote ownership, so no skew tolerance is
needed. Every response carries `authorityTime`. A client that has not yet
observed authority time performs a read first. Clients express every deadline
relative to authority time and advance it with the local monotonic clock
measured from dispatch, not from receipt, which is conservative by one round
trip. `requestNotAfter` is chosen from the last observed authority time plus
monotonic elapsed time, with the same 24 h maximum window as the local contract;
the authority rejects a value that is in the past or beyond that window.

Renewal is scheduled when half the granted TTL has elapsed in authority time. If
a renewal is not confirmed by the time three quarters has elapsed, the client
stops starting new guarded work and reports the uncertainty. A configured
remote failure never falls back to local state. Require HTTPS outside an
explicit development mode, finite timeouts, cancellable waits, and server
response validation.

## Authentication, roles, and revocation

Authority authentication and claim credentials are different secrets. The
former authenticates an installation and authorizes a namespace role; the
latter authorizes one ownership epoch. Use separate secure credential sources and
redact both. Public multi-tenant administration, billing, quotas and credential
issuance are separate scope.

A private deployment authenticates installations at the front door with
Cloudflare Access service tokens, or an equivalent authenticated reverse proxy.
The server validates the front door's identity assertion: for Access, verify the
JWT signature against trusted Access keys, expected issuer and application
audience, token type, and expiry. Map the verified service-token `common_name`
(Client ID), not `sub`, to an installation and its namespace role in the database.
Cloudflare documents these claims in its [application token contract](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/authorization-cookie/application-token/).
Check installation revocation on every request, including replay. The server
accepts no unauthenticated request path except health.

| Role | Grants |
| --- | --- |
| `read` | Redacted status, list, events, history, and watch for the namespace. |
| `write` | `read` plus acquire, renew, checkpoint, transfer, release, operation begin/renew/complete, inspection of epochs it holds credentials for, and reconciliation. |
| `admin` | `write` plus manifest and enrollment edits, installation enrollment and revocation, audited private inspection of ended epochs in the namespace, administrative claim revocation, GC apply, audited recovery import and restore reopening, and namespace deletion. |

Namespace access is not fine-grained resource isolation against hostile
members: start with one trusted cooperative team per namespace. Separate
namespaces trade away cross-boundary atomic claims; finer ACLs need their own
requirement.

Credential rotation overlaps: enroll the new installation credential before
revoking the old one. Revocation of an installation takes effect on its next
request, including pending exact replays, which fail `installation-revoked`.
Claims held by a revoked installation are not force-released, because that
would erase unknown-outcome state; they expire lazily or an admin revokes them
explicitly. Administrative claim revocation ends the epoch with reason
`revoked`, leaves started operations unresolved, and appends a public event. It
is not executor termination and must never be described as one. Another
`write` installation then recovers through ordinary acquire-after-expiry plus
reconciliation, using the public request hash and, where needed, an admin's
audited private inspection. Removing a compromised installation therefore never
requires granting it access again.

### Cross-host transfer

Transfer never moves a bearer credential between hosts. It is two steps:

1. The recipient installation calls `transfer-prepare`, generating the
   successor claim ID and credential locally, saving them in a new handle, and
   registering only the successor hash with the authority. The registration is
   bound to the recipient installation and expires with its own
   `requestNotAfter` if unused.
2. The current holder calls `transfer` with its credentials, naming the
   prepared successor claim ID. The authority verifies the registration, creates
   the successor at revision 1, carries the checkpoint, and ends the predecessor
   in one transaction with no free interval, exactly like local transfer.

Replay of either step follows the local transfer rules; changed successor
credentials conflict. Unused registrations are retired lazily. Copying a handle
between hosts remains unsupported and is not ownership transfer.

## Data governance

Choose the hosting region at namespace creation and treat it as immutable. If
the front door is Cloudflare, its logs record request metadata outside the
region. Retention and deletion of private recovery context follow the same GC
rules as the local authority; deletion beyond GC requires an `admin` action and
is refused for unresolved operations. Replicas in object storage inherit the
namespace's access controls and are readable only by the restore procedure.
Backups are therefore covered by the restore incarnation rule and never by a
second live authority. Object-storage replication is optional: disabling it
removes replica-based disaster recovery but does not change the single-writer,
identity, restart, or claim-safety rules. Any later database import still enters
the quarantined restore path under a fresh incarnation.

## Possible follow-ups

These are separate opportunities, not requirements for the initial remote
authority:

- **Optional managed backup.** Package continuous replication and restore drills
  for operators whose recovery targets justify them. Keep plain `worklease
  serve` usable with only a persistent SQLite volume, with the absence of a
  replica and its data-loss consequences explicit.
- **Claim-scoped agent coordination.** Evaluate bounded structured handoff notes,
  release or transfer requests, recovery-required notices, and annotations tied
  to claim or operation IDs. Reuse checkpoints, events, and watches where their
  existing safety and privacy boundaries fit.

Do not turn the second follow-up into general chat, a durable message queue, task
dispatch, remote commands, or transcript storage. Those belong in an external
messaging or orchestration system, which can carry Worklease IDs for correlation
while Worklease remains the authority for ownership and recovery.

## Remaining decisions and release evidence

The architecture and safety requirements above are design decisions. Their
mechanisms and failure behavior still require implementation and executable
evidence when remote work is authorized. Sharding, fencing counters, and public
hosting are explicit deferrals, not decisions blocking a private implementation.
The following questions still require measurements or operating-team decisions:

| Area | Open decision and what resolves it |
| --- | --- |
| Whether to build at all | The demand validation above. Two or three teams with measured cross-host duplicate execution that a scheduler or provider does not already solve. |
| Recovery targets | The operating team sets acceptable acknowledged-write loss and recovery downtime before choosing deployment details. Async backup is not high availability; missing recovery evidence can block reopening indefinitely. |
| Capacity and cost | Measured WAN latency, renewal margins, retry/watch bursts, per-namespace write throughput, replication lag, and recovery storage demand. These validate thresholds, reserves, quotas, and cost assumptions. |
| Hosting region and provider | An operating-team choice constrained by recovery targets, one always-on host, and one persistent volume. Object-storage replication is optional and selected when those targets require replica-based recovery. |
| Operational ownership | Named owners and runbooks for patching, restore, and reopening after restore. A private service does not ship without them. |

Before private deployment, add executable scenarios covering at least:

1. Two hosts with different checkout roots resolve the same enrolled identity and
   contend; deliberately separate scopes do not. Unknown aliases, host-local
   keys, stale managed requests, and unsafe locators fail with
   `resource-not-enrolled` without claiming another key.
2. Manifest edits during acquire do not split contention. A key migration with
   active claims or unknown predecessors is rejected. Exact replay after a
   manifest update recovers the old receipt without new effects or ownership.
   Over-bound TTL is rejected, not clamped.
3. A lost response is recovered from the same authority; a partition stops new
   client effects and never creates local fallback ownership. An old provider
   request completing after expiry remains an explicit recovery problem.
   Renewal scheduling uses authority time and is conservative by one round trip.
4. Role isolation, redacted feeds, admin private reads, credential rotation,
   installation revocation with pending replays, and administrative claim
   revocation obey the roles table. Two-step transfer succeeds without a bearer
   crossing hosts; an unprepared or expired successor is rejected.
5. Snapshot/watch races, disconnect/reconnect, lazy expiry, cursor gaps, and a
   stuck predecessor pinning retention preserve recovery state. Capacity
   pressure rejects new admissions at the ceiling while renewals, release, and
   reconciliation continue. Start with an expired unresolved predecessor and no
   resolver claim: recovery-only acquisition still succeeds within its reserve
   and cannot start new effects, including after transfer. Replay an acquire
   committed below the ceiling after crossing it; recover the original receipt.
   Exhaust the recovery reserve and fail closed without pruning unknown state.
6. Restart, rolling protocol/schema upgrades, and restore from a replica preserve
   replay and unknown-operation semantics. Restart preserves restoreId; restore
   creates a fresh one, ends active claims as `restored`, and rejects old
   incarnation cursors, handles, and replays. Restore the same backup twice and
   reject requests from the first restored incarnation at the second. Lose a
   confirmed start from the backup tail while its client is offline: quarantine
   remains closed until independent evidence or client recovery accounts for it.
   Exercise idempotent/conflicting imports, recovery-only claims before normal
   reopening, and restored revocation-state verification. A paused server keeps
   its OS lock: refuse a second server and alternate CLI writer, resume the first
   safely, then permit takeover after exit. Verify stable lock-file identity.

These are future acceptance scenarios, not assertions about shipped behavior.

## Deployment and implementation boundary

Client and server live in this repository because they are one binary in two
modes, not because a second implementation needs a shared conformance suite.
Deployment tooling owns the host, volume, replication, front door, and secrets;
`worklease` remains the claim client and authority, not a provisioning tool.

Do not add preparatory remote code to the local release. When the feature is
authorized:

1. Amend the contract's remote section under its amendment procedure and freeze
   the protocol over the typed service requests and explicit recovery/transfer
   extensions, with authority time, restoreId, and the error reasons named above.
2. Add restoreId to `meta` and bind it into requests, receipts, handles, and
   cursors. Implement the process-lifetime lock and quarantined restore path,
   including audited unknown-operation import and reopening prerequisites.
3. Implement `worklease serve`: HTTP handlers over the existing service methods,
   front-door identity validation and role mapping, bounded bodies,
   non-cacheable responses, no resource keys or tokens in URLs or logs, and the
   admission ceiling with exact replay and bounded recovery reservations.
4. Add the client-side local/remote selector, credential handling, two-step
   transfer, and the partition and lost-response tests. Keep guarded effects
   strictly local.
5. Run the six scenario groups against a real two-host deployment behind the
   front door, including a replica restore, before offering the service to a
   second team.

Remote implementation is not made ready by this document. Sharding, a fencing
counter, provider-executed mutations, managed Postgres, and a public hosted
product stay deferred until each has a concrete requirement.
