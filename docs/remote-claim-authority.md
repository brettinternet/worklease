# Distributed Worklease claims: remote authority design

## Status and decision

Future design. No remote authority, HTTP client or server, authentication
setup, or deployment is part of the shipped Go release. The
[Go Product Contract](backlog/docs/go-rewrite/doc-2%20-%20Go-Product-Contract.md)
is normative for the current product; this document records the future design,
the decisions already made, and the few decisions that remain open. The first
draft proposed a Cloudflare Durable Object authority; that historical alternative
is evaluated and rejected below.

The product decision is to make an opt-in, self-hosted remote authority
available so users can experiment with cross-host coordination. Worklease will
not provision or operate that deployment and makes no high-availability claim.
Implementation still requires an explicit contract amendment and executable
acceptance evidence. Documentation records intended behavior, not a shipped or
verified guarantee.

**Decision (2026-09-12, owner-authorized pivot; refined 2026-09-13).** The remote
authority is the existing Go authority, served over authenticated HTTPS. One
`worklease serve` process initially runs the same `lease.Service` and SQLite
store that the local CLI uses, on one always-on host with one persistent volume.
Optional continuous SQLite replication to object storage can provide
disaster-recovery backups when a deployment's recovery targets require them; a
simple `worklease serve` may rely on its persistent SQLite volume alone. A
reverse proxy or tunnel may terminate TLS, but it then sees bearer secrets and
must be trusted. Restrict and encrypt an off-host origin, and do not accept
arbitrary proxy identity headers as authorization. Worklease owns installation
credentials, roles, and API authorization. Where compute runs is decoupled from
the TLS edge.

The strongest reason is to reuse the tested Go claim, replay, reconciliation,
and garbage-collection semantics. HTTP still adds durable client recovery,
authentication, cancellation, and authority-time boundaries that need their own
design and failure evidence. A second implementation of the domain semantics in
another language would duplicate them forever, and divergence could surface as
duplicate execution or a lost started operation. Hosting cost does not decide
this architecture; comparable cost remains an assumption until a deployment and
workload are measured.

Keep one end-user `worklease` binary. The shell CLI and local stdio MCP adapter
act as clients; `worklease serve` runs the remote HTTP authority. Guarded child
processes and edits always remain on the client host.

```text
shell/scripts -------- CLI -------+
                                  |
IDE/agent ----- stdio MCP --------+--> local worklease client state
                                        | handles, pending requests,
                                        | installation and claim credentials
                                        |
                                        +--- authenticated HTTPS ---> worklease serve
                                                                         |
                                                                    lease.Service
                                                                         |
                                                              SQLite + persistent volume
                                                                         |
                                                              optional async object backup
```

The initial slice is deliberately narrow so the safety core ships and
convenience follows demand. It contains: one namespace per `serve` process;
portable keys only, with admitted prefixes and TTL/hold bounds in the server
configuration file; invite-based enrollment with client-generated installation
credentials and three roles; a namespace-level recovery mode used by restore and
reopened by an operator record; the single-writer OS lock; same-host transfer;
and the local stdio MCP adapter as a remote client. Repository enrollment,
cross-host transfer, audited recovery import, admission backpressure, browser
login and a control plane, and multi-namespace serving are recorded follow-ups
with named triggers. Request incarnation binding remains the one protocol-level
dispute recorded below.

The standard release includes client and server code, but opens no listener and
performs no remote work unless a remote profile or `serve` is explicitly used.
Do not add a thin-build release matrix initially. A representative stripped
macOS arm64 probe added about 4.4 MiB uncompressed and 1.7 MiB gzip-compressed
for HTTPS client/server and RSA/X.509 primitives; omitting only `serve` while
retaining the remote client saved less than 1 MiB uncompressed. Re-measure the
implemented release on all targets and add a local-only build only for concrete
artifact, SBOM, or policy demand.

There is no requirement to preserve Python classes, bundle commands, wire
schemas, owner IDs, state files, or plugin interfaces.

### Alternatives considered

| Option | Why not, or when |
| --- | --- |
| One SQLite-backed Cloudflare Durable Object per namespace behind a Worker | Reimplements every safety invariant in TypeScript plus a cross-language conformance suite. Input/output gates and write coalescing are a different serialization model than the tested `BEGIN IMMEDIATE` boundary. The [per-object storage limit](https://developers.cloudflare.com/durable-objects/platform/limits/) is an operational retention constraint, as every finite store has one. The principal objection is the permanent second implementation. Near-zero patching and [30-day point-in-time recovery](https://developers.cloudflare.com/durable-objects/api/sqlite-storage-api/) do not outweigh that cost. |
| Cloudflare Containers, or Go compiled to Wasm inside a Worker | [Container disk is ephemeral](https://developers.cloudflare.com/containers/concepts/architecture/). Persistence requires either a redesigned store or an external database; Durable Objects are one option. FUSE or object storage has not demonstrated the filesystem and WAL durability this authority requires. |
| Turso or libSQL | Keeps much of the SQLite model, and its [HTTP API supports batched and pipelined requests](https://docs.turso.tech/sdk/http/reference), so it need not add one network round trip per statement. A remote database still needs transaction, ambiguity, time, driver, and failure conformance, and it does not remove the remote Worklease service or namespace serialization. |
| Managed Postgres behind the Go service | The planned escalation path when measured write throughput, managed-database durability, or stateless server replicas become requirements. It is a second storage implementation of the same authority contract, not a second claim service. Do not pay its abstraction and conformance cost before one of those triggers exists. |
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

Making the capability available for experimentation is itself an accepted
product goal; prior adoption by two or three teams is no longer an implementation
gate. Demand validation still determines promotion, support promises, and later
investment. An SSH-forwarded authority remains a cheap discovery experiment,
but it does not validate the full client failure boundary because handles,
credentials, and guarded effects execute on the authority host.

Before presenting remote recovery as reliable, run a two-host harness over the
HTTP service with durable requests and effects on the client hosts. Exercise a
lost start response, a crash, and a partition. Measure onboarding effort,
duplicate attempts avoided, time blocked on unknown outcomes, recovery effort,
WAN latency, write throughput, and request/storage usage. Compare against the
actual scheduler or provider-native alternative; the feature remains useful
only where cooperating executors need coordination outside those systems.

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
| `BeginOperation`, `RenewOperation`, and `CompleteOperation` separate from the local effect callback | The server exposes these lifecycle methods. The exec supervisor stays on the client, and `RunGuardedOperation` remains a local replacement helper rather than remote execution orchestration. |
| One pooled write connection, `BEGIN IMMEDIATE`, WAL, synchronous FULL | The single-writer serialization and durability posture a hosted authority needs already exist. |
| Immutable authorityId in receipts, requests, handles, and opaque cursors | A local path or remote URL is a locator; it must not silently retarget a credential or continuation. |
| Client-generated credentials saved before dispatch; authority retains only hashes | A lost acquire/transfer response does not require decryptable bearer tokens in server receipts. |
| Exact, bounded request replay with retained operation IDs and normalized defaults | Network retries recover the original intent and never duplicate external effects. |
| Per-claim revision for optimistic concurrency | No assumption that revision is a fence across owners. |
| Host-local versus portable identity metadata | Absolute paths and git common directories do not become cross-host repository identities. |
| Explicit operation protection and unresolved predecessor visibility | Renewals do not imply that an old process or provider request has stopped. |
| Static, cgo-free binary | The server is the same artifact as the CLI, cross-compiled for the host. |

Do not add a generic backend registry or storage interface solely in anticipation
of Postgres. The HTTP server calls the typed lease service, and only the client
needs a local/remote authority selector. Keep HTTP, authentication, MCP, and
configuration code from depending on SQLite types. When a second storage backend
is authorized, extract the narrow transaction/repository boundary from the two
real implementations and run one behavioral conformance suite against both.
Fencing counters remain deferred until an enforcing consumer exists.

## Authority, namespace, and process

An initial authority is one SQLite database served by exactly one process. The
authority identity is the immutable random authorityId already created at
bootstrap; it is independent of database engine. An endpoint URL and a database
path or DSN locate it; none is identity. Clients validate authorityId on every
response, so an endpoint can move without retargeting credentials or
continuations. Two independently writable stores serving the same authorityId
is the clone problem below and is prohibited.

A namespace is an authority, and one `serve` process serves exactly one
namespace from one database. A team that needs isolated coordination runs its
own process; a hostname router in front of several processes is a deployment
concern. Atomic claims never cross namespaces, and there is no cross-namespace
request to reject because a process cannot see another namespace. Hosting
several namespaces in one process is a follow-up. It also requires per-namespace
lock ownership, credential isolation, and load isolation in addition to routing.
Namespace authorization comes from trusted authenticated server state, never
from a request field. A request may name its expected authority for validation;
that is not an access grant.

Do not shard a namespace by resource: overlapping multi-resource claims would
require distributed transactions. If throughput later warrants partitioning,
partition by explicit tenant or repository namespace and keep the one-namespace
atomicity boundary. One serialized writer per namespace is the throughput
ceiling; measure before deciding it is a problem.

The configured database is authoritative. Nothing depends on process memory
surviving a restart. Expiry is evaluated lazily from authority time; a background
task may perform retention work, but expiry never depends on a timer firing. A
SQLite authority must never sit behind a load balancer with more than one origin.

### Single writer guard

A hosted deployment introduces one new invariant: a rolling deploy, blue-green
cutover, or a hostname pointing at two machines must not create two writable
databases with the same authorityId. Deploys are stop-before-start on one
volume. The server holds an exclusive process-lifetime OS lock for the hosted
authority on that volume. All writers to that hosted authority, including
alternate CLI and maintenance entry points, must respect it; they cannot bypass
the server. Ordinary local CLI authorities retain their existing concurrency
model. The hosted lock remains held while a process is paused and
is released when it exits. Keep the lock file stable while held; unlinking and
recreating it must not admit a second holder. Reuse the pinned-handle technique
from `internal/handle`: open the lock file, take `flock`, then verify the opened
device and inode still match the directory entry before trusting the lock.

Do not use a startup-only expiring database lease: a paused server can resume
after another server adopts it. SQLite transaction serialization alone does not
enforce one serving process. The advisory OS lock protects cooperating processes
that open the same stable inode. The pinned device/inode check verifies
acquisition; every maintenance writer must also avoid unlinking or replacing the
lock file or its parent while the lock is held. It does not protect independent
writable clones. SQLite WAL requires a single-host filesystem and is [not
supported on a network filesystem](https://sqlite.org/wal.html).
Stop-before-start upgrades must respect the same lock.

### Storage backend and Postgres evolution

SQLite is the initial and default backend. Authority mutations are short, and
one serialized writer is also the easiest way to preserve atomic multi-resource
claims, exact replay, ledger projection updates, and admission decisions. The
expected private and small-team workload is unlikely to make SQLite the first
bottleneck; measure transactions per second, p95/p99 write latency, WAL growth,
and long-poll/read pressure before changing engines.

Cluster operators may nevertheless prefer managed Postgres for operational
reasons before raw throughput requires it: durable managed storage, automated
backups, rolling application deploys, and several stateless `serve` replicas.
Postgres is therefore an intended future backend, but not part of the initial
remote release.

Do not create a lowest-common-denominator SQL abstraction now. The current
service/store code uses real SQLite transaction behavior, and a speculative
interface would hide rather than solve the differences in locking, commit
ambiguity, sequences, time, and restore. Preserve the future path by keeping the
wire protocol and typed domain requests/results storage-neutral, keeping new
HTTP/authentication code outside `internal/store`, and centralizing backend SQL
and transaction mechanics below the service boundary. Extract an
operation-oriented store interface only while implementing Postgres, from the
needs demonstrated by both backends.

A Postgres backend must preserve the same observable authority contract. At a
minimum it must:

- serialize every mutation for one namespace, for example by locking one
  authority row or taking a transaction-scoped namespace advisory lock;
- atomically update claims, resources, operations, replay records, ledger
  projections, and recovery-mode state;
- classify uncertain commits through durable request/result read-back;
- use authoritative database time consistently and retain authorityId and
  restoreId across ordinary restarts;
- regenerate restoreId and enter recovery mode after any restore, import, or
  promotion/failover that can lose acknowledged writes; transparent failover is
  permitted only with demonstrated synchronous no-acknowledged-write-loss
  durability and split-brain prevention;
- keep connection capacity available for ownership lifecycle and recovery under
  load;
- run the same backend-neutral failure and replay scenarios as SQLite.

Only a shared transactional Postgres database can authorize multiple stateless
`serve` replicas. Application replicas must not use process memory, local locks,
or notifications as correctness state; `LISTEN/NOTIFY` may only accelerate a
recheck. Replicas do not remove the namespace serialization boundary, so they do
not by themselves increase mutation throughput. Multi-replica service is a
separate release claim requiring concurrent and failover evidence. Do not expose
a public backend registry or promise DSN compatibility until Postgres exists.

### Restore incarnation and recovery mode

Replication is asynchronous, so losing the host loses the tail of committed
writes, including recently committed started operations. Restore therefore is
authority recreation, never resumption. Add a cryptographically random
`restoreId` to `meta` at bootstrap. Every restore from a replica, database
import, or recreation generates a fresh value before serving requests; an
ordinary restart preserves it. Compare incarnations for equality, not ordering.
A counter inside the backup is insufficient: restoring the same generation-1
backup twice would produce generation 2 twice and accept requests from
rolled-back state.

The current draft detects an incarnation change from Worklease application
responses rather than request input. This decision remains disputed in the
architecture recommendations below and must be resolved before the protocol is
frozen. Every Worklease application response carries the current `restoreId`,
and cursors embed it so the authority can distinguish an old-incarnation
sequence from a future one. The client keeps one last-observed value per profile;
when a response carries a different value it fails closed with
`authority-restored`, stops dispatching for that profile, and preserves saved
recovery evidence. Proxy and network failures do not carry this envelope.
Receipts may record the issuing incarnation as provenance. A stateless
`--no-handle` caller has no profile state and therefore may see `stale-claim`,
`installation-revoked`, or `authentication-required`, depending on which rows
survived the restore.

Asynchronous backup provides disaster recovery, not seamless failover or zero
acknowledged-write loss. For example, [Litestream's replication](https://litestream.io/how-it-works/)
copies WAL changes asynchronously. Set acceptable data loss and recovery downtime
before choosing the deployment, and measure replication lag and restore drills
against those targets. Safe reopening can remain blocked longer than a target
if outcome or executor-cessation evidence is unavailable.

After a restore the authority:

1. ends every active claim with reason `restored`, effective at restore time,
   and leaves every started operation unresolved, so an old handle fails
   `stale-claim`;
2. revokes every retained installation credential and invite from the old
   incarnation, so a retained old bearer fails `installation-revoked`; a bearer
   whose row was lost fails `authentication-required`, and an old-incarnation
   cursor fails `authority-restored`;
3. enters recovery mode (below), so ordinary admission and new guarded effects
   are refused until explicitly reopened while authorized inspection and
   reconciliation continue.

Recovery mode is a namespace state, not a claim type. While it is set, the
authority refuses every new `BeginOperation` and admits an acquire only when it
covers the full transitive resource closure of an unresolved predecessor, under
the same atomic ownership rules and 32-resource limit. Resolve retained replay
before applying the new-start check. A completed operation returns its receipt;
an operation still recorded as started returns `unknown-outcome`, never renewed
permission to execute. Renewal, checkpoint, same-host transfer, release,
completion, and reconciliation remain available. A claim admitted in recovery
mode is an ordinary claim and may start effects after the namespace reopens.
Entering recovery mode cannot stop effects already dispatched on client hosts.
There is no recovery-only claim attribute to carry through renewal and transfer.

The restored ledger and client pending requests cannot enumerate the lost tail.
A start, its effect, and its completion may all occur after the backup cutoff;
the client can then clear its pending request while the backup contains no row.
Installation enrollment or revocation can be lost in the same interval. A clean
restored ledger and an empty pending set are therefore not reopening evidence.

Reopening requires independently retained operator, provider, or executor
records covering the chosen restore horizon from the restored durable cutoff to
cessation of the old authority. The evidence must account for potentially lost
dispatched and completed work, installation enrollment and revocation, outcomes,
and executor cessation. Missing evidence keeps the namespace in recovery mode.
This is initially an operator procedure and does not silently add a client
journal. The bounded private reopening record contains the attestation and
private references to external evidence, never credentials, argv, or evidence
dumps in public feeds. The authority serializes reopening against current
recovery state, verifies that every retained unresolved operation is reconciled
and the operator attestation is complete, then atomically records the action and
opens ordinary admission. The attestation records responsibility; it cannot
mechanically prove external truth.

Audited recovery import remains a follow-up triggered by a restore drill in
which manual accounting proves insufficient. It can make an already identified
lost-tail operation participate in overlap checks, but cannot discover missing
operations or replace external evidence. If added, an import is an admin request
bound to the current incarnation that preserves original identity, request hash,
full resource set, and evidence provenance; is idempotent and fails closed on
conflict; never fabricates success, revives credentials, or authorizes
redispatch; and is reconciled under recovery ownership. The current Go
reconciliation method rejects missing targets, so import is new work.

Within one incarnation, an unexpired request's absence from the same healthy
authority establishes absence at that read, not cancellation of an in-flight
dispatch. Only the identical saved request may be retried under exact replay.
Across incarnations, absence cannot exclude a lost commit; recover rather than
redispatch. Copying a live database to a second writable location is never a
supported operation; a copy is usable only through the restore procedure under
a fresh incarnation.

### Namespace deletion and cutover

Deleting a namespace means stopping its process and retiring its database.
`worklease serve` refuses to retire a database while active claims or
unresolved started operations exist. An operator may force it only after
exporting the redacted list of unresolved operations, which is recorded in the
deployment's administrative log outside the retired database. Deletion is never
an undocumented way to erase unresolved risk.

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
`resource-not-enrolled`. The initial remote release admits only portable keys:
the `github:` and `coordination:` prefixes that the `github`, `linear`, and
`generic` policies already derive, plus any prefix the operator lists for raw
`--resource` input. A team coordinating a Backlog.md or file-based backlog
across hosts meanwhile uses the `generic` policy with an agreed source name,
for example `--provider generic --source my-repo --item TASK-123`, which every
host hashes to the same `coordination:generic:<sha256>` key. Remote use of the
host-local policies is a follow-up that requires an enrolled repository identity
plus the canonical relative locator below. Do not infer that identity
from whichever Git remote happens to be configured, and do not silently rewrite
an existing local key into a remote key.

Exact resource equality remains the only overlap rule. Directory claims do not
implicitly cover child files, and task claims do not cover the files touched by
that task. Broad source claims and explicit file claims are caller-selected
coordination scopes; use atomic claims when several exact resources are needed.

### Canonical relative locator

Recorded design for the repository-enrollment follow-up. A portable file-like
key is `<policy>:<repositoryId>:<locator>` with each component percent-encoded
as the local policies already do. The locator rules are fixed:

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

## Client configuration and admission

Each installation trusts an explicitly recorded endpoint and expected
authorityId supplied by the invite issuer. The client compares both before
activating a profile. Credential-bearing requests do not follow redirects, and
endpoint changes require an explicit configuration action. User configuration
stores named authority profiles and the default profile, but never a bearer
credential. Explicit `--profile`, then
`WORKLEASE_PROFILE`, then a user-side project binding, then the user default
chooses the profile; with none of these the authority is local. A project
binding maps the canonical local project identity to a profile and is written
only by an explicit user command run in that checkout. There is no
repository-committed project file and no machine-policy layer initially: a
cloned repository cannot propose, select, or redirect a profile because nothing
in the repository is read for authority selection. Server configuration is a
separate deployment-owned file and never reads project configuration. Both
layers are follow-ups if a deployment demonstrates the need; the ordering above
leaves room for them without changing how a profile is bound.

Installation credentials belong in the OS credential store, with an
owner-private file or descriptor source for headless environments; they never
belong in YAML, a repository, argv, MCP configuration, or logs. Local claim
handles stay local; copying them between hosts is not ownership transfer.

The server configuration file lists delimiter-terminated admitted prefixes,
such as `github:` and `coordination:generic:`, plus namespace TTL and hold
bounds. The known host-local prefixes `path:`, `backlog-md:`, and `markdown:` are
reserved and always rejected remotely, even if a raw key or mistaken allowlist
names them. Admission validates only the prefix; it cannot prove that an opaque
raw key is portable. Contention still uses byte equality.

Prefix withdrawal blocks new ordinary claims. It never strands lifecycle,
same-host transfer, recovery under previously admitted keys, or exact replay.
At admission the authority persists the granted maximum TTL and absolute hold
deadline. Every expiry extension, including `BeginOperation`,
`RenewOperation`, and same-host transfer, enforces those persisted limits. A
caller cannot raise or reset them, and same-host transfer inherits the
predecessor's absolute deadline. A fresh recovery acquire receives a fresh hold
budget under current bounds. A new admission requesting TTL or maximum hold
above current bounds is rejected without rewriting hashed intent; resulting
expiry is capped at the admitted deadline as a separate calculation. Existing
leases retain their admitted limits when configuration changes.

Do not put the host, worktree, session, or branch into the key for a shared
logical resource. A branch/ref belongs in identity only when the caller
deliberately wants separate coordination scopes. Prefer immutable resource
identities so display-name changes do not change contention.

### Follow-up: repository enrollment and aliases

Remote use of `path`, `backlog-md`, and `markdown` needs a shared catalog in the
namespace database: an `admin` registers stable repository/source identities and
vetted aliases, each resolving to a canonical opaque key that the claim service
still compares byte for byte. For example, a laptop checkout at
`/Users/dev/project` and a CI checkout at `/workspace/project` enroll as the
same repository identity, and their common locator `src/main.go` resolves to the
same resource. Catalog edits require an expected revision and produce an audit
record. Clients persist the catalog revision and resolved keys with the
normalized request before dispatch, and the authority validates resolution in
the same serialized decision as admission. A cached catalog may support
resolution and diagnostics, never offline acquire, renewal, or ownership
verification. The catalog cannot change trust anchors or supply executable
commands, hooks, provider credentials, claim tokens, or host-local paths.

A catalog change must never re-key an existing lease or invalidate its exact
replay. A key-changing migration therefore is not a hot alias edit: put the
namespace into recovery mode, drain current ownership, reconcile every
unresolved predecessor with outcome and executor-cessation evidence, preserve old
receipts, and activate the new mapping only when no interval accepts old and new
keys as independent claims for the same resource. Missing recovery evidence
blocks the migration rather than discarding the unknown operation. Share the
locator rules as cross-platform conformance vectors before shipping this.

## Remote ledger and recovery

The ledger is shared operational memory across hosts within one namespace. Claim
state, epochs, operation records, checkpoints, and lifecycle events stay in the
authoritative database, and each state transition commits with its ledger record
in one transaction, exactly as today. SQLite is the initial implementation;
future backends preserve this observable atomicity. There is no total order
across namespaces and no remote child-process execution.

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

Recovery during restore and key migration uses the namespace recovery mode
defined above: acquires must cover an unresolved predecessor and no claim may
start a guarded effect until the namespace reopens. There is no recovery-only
claim type and no reserved recovery capacity.

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
attempts paced by watch long polls with jitter, bounded by the same 60 s maximum
as the local contract. There is no server-side queue, and atomic admission is
not FIFO fairness.

### Local MCP adapter

Keep `worklease mcp` as a local stdio server. It resolves the same trusted
project/profile configuration as the CLI and calls either the local service or
the remote HTTPS client. The remote authority is not initially an MCP endpoint.
This keeps compatibility independent of each MCP host's remote transport and
authentication support.

The adapter keeps installation credentials, claim credentials, handles, and
pending requests on the client host; MCP exposes only opaque lease references.
It renews claims and performs watch long polls without putting secrets in MCP
configuration or model-visible results. It never enrolls or redeems an invite
from a tool call. A missing or revoked installation credential returns the
distinct `authentication-required` or `installation-revoked` reason with
guidance to enroll the installation outside MCP.

Project-scoped MCP setup binds the project root or explicit profile when the MCP
host cannot provide a trustworthy workspace root. A user-global MCP process must
not guess a repository from arbitrary request content. Guarded execution and
native edit checks remain client-local; remote `replace-file` remains disabled.

### Retention and capacity

Retain started unknown operations and the receipts/authentication needed for
replay and recovery. The contiguous-prefix retention model lets one stuck
predecessor pin newer history and events indefinitely. Because the binding
constraint is now volume size rather than a vendor cap, the deployment budgets
for this explicitly: a disk usage alert wired to the retention model, and a
volume that can grow. Never prune unknown operations to meet a quota or treat an
exported record as permission to remove safety state.

There is no admission ceiling or reserved recovery capacity initially. A full
volume fails writes with `storage-failure`, exactly as the local authority
does, and the operator expands storage; no service can promise continued
renewal after its underlying storage becomes unavailable. Include WAL growth,
pinned history, and backup lag in the disk alert, and measure operation sizes
under real load. Admission backpressure that keeps lifecycle, completion, and
reconciliation available while refusing new ordinary work is a follow-up
triggered by those measurements. If added, it follows the same ordered replay
and admission checks but remains separate from explicit recovery mode. Pressure
subsiding must not reopen recovery or require restore accounting. It must
recognize exact replay of a committed acquire before applying a new-admission
check, so crossing a threshold never turns a committed acquire into a failed
new attempt.

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
mapped field by field onto an explicit allowed subset of the typed service
requests. Local compatibility fields such as `LegacyRequestHash` are never
client-selected remote input. Recovery mode is a namespace state the
existing admission and `BeginOperation` paths consult; it is not a new method.
The initial administrative service also covers invites, installation and claim
revocation, restore reopening, bounded private inspection, GC apply, and database
retirement. Recovery import and cross-host transfer preparation are follow-up
extensions whose transactional invariants cannot be implemented by independent
HTTP writes. Bind requests to authorityId; every Worklease application response
carries `restoreId` and `authorityTime`. Reject unsupported versions, unknown
fields, invalid types, oversized bodies and responses, and return non-cacheable
responses. Resource keys and bearer credentials do not belong in URLs or logs.

Every authenticated remote mutation validates the current installation, role,
and authority before exact replay. Claim-operation replay also authenticates the
original epoch credential. Only a request that is not a replay proceeds to
new-admission checks for recovery state, prefix, and bounds. Recheck current
installation revocation, namespace policy, and recovery state in the same
serialized mutation transaction, rather than relying on HTTP middleware.
Current revocation may refuse replay; otherwise resolve retained replay before
new-admission checks even after policy changes. A completed operation returns
its receipt, while an operation retained as started returns `unknown-outcome`
and never renewed permission to execute. Recovery mode refuses a new start.
Enrollment has its separate atomic invite-redemption replay rules below.

Clients retain generated claim/operation IDs, normalized defaults, credentials,
request hashes and retry deadlines before dispatch. Acquire and initial
same-host transfer send client-generated fresh claim credentials through the
authenticated channel; the authority stores hashes and returns token-free
receipts. Exact replay authenticates the original epoch even after release or
same-host transfer. Exact replay of acquire and same-host transfer returns the
original result; operation replay follows the retained state described above. A
changed request conflicts.

A timeout after dispatch has an unknown commit outcome. Inspect or retry the
exact stored request within the supported replay window; never mint a new claim
or operation because a response was lost. A pending guarded operation never
executes again on replay. Absence after retention is not proof of no effect.

### Authority time

Client wall clocks never decide remote ownership. Sample `authorityTime` freshly
for every response, including replay; a historical receipt is returned inside a
fresh response envelope. A client without a usable sample performs a read first.
For an authority sample `A` generated between monotonic send `S` and receive
`R`, estimate authority time at monotonic `N` with two bounds:

| Use | Conservative estimate |
| --- | --- |
| Expiry and renewal | upper bound `A + (N - S)` |
| Latest valid `requestNotAfter` | lower bound `A + (N - R) + 24h` |

For example, asymmetric outbound delay increases the upper estimate but must not
extend the 24-hour request window. A delayed short window may expire; the client
must not rewrite a saved deadline. Refresh the sample after process restart,
suspend, or monotonic-clock discontinuity. The authority clock still needs
operational discipline even though client wall-clock offset is not an ownership
source. These scheduling estimates assume stable clock progress and include a
drift margin; server checks remain authoritative for ownership. A late original
start response received after the conservative expiry must not dispatch its
effect.

Renewal is scheduled when half the granted TTL has elapsed in authority time. If
a renewal is not confirmed by the time three quarters has elapsed, the client
stops starting new guarded work and reports the uncertainty. A configured
remote failure never falls back to local state. Require HTTPS outside an
explicit development mode, finite timeouts, cancellable waits, and server
response validation.

## Authentication, roles, and revocation

Authority authentication and claim credentials are different secrets. An
installation credential authenticates one installation, that is one machine and
user or one CI identity, and authorizes a namespace role; a claim credential
authorizes one ownership epoch. Store, rotate, and redact them independently.
Worklease owns this implementation in this repository and has no runtime,
identity, protocol, or compatibility dependency on an external identity
provider or OAuth server.

Enrollment uses cryptographically unguessable one-shot bearer invites of at
least 128 bits with a short expiry and a fixed role. They grant that role in
full, so distribute and store them as secrets. Do not place invite or
installation credentials in argv or logs.

1. The admin client generates and durably saves an invite code before dispatch,
   then sends its hash, role, nonunique diagnostic label, bounded request ID,
   and deadline. An identical retry returns the same grant without creating a
   second invite. The authority never stores the code.
2. The redeemer supplies the code through a hidden prompt, `--invite-file`, or
   `--invite-fd`. It also supplies the trusted HTTPS endpoint and expected
   authorityId received from the issuer. The client generates and durably stores
   an installation credential before dispatch; a descriptor can read an already
   persisted secret but is not durable storage.
3. The authority atomically burns the invite, inserts an immutable installation
   ID and credential hash, and stores the immutable redemption replay result.
   The client finalizes the profile only after confirmed or recovered enrollment
   and preserves pending enrollment plus older recovery evidence until then.
4. Every later request sends the installation bearer to the authority over
   trusted TLS. The authority looks up its hash, checks revocation, and applies
   the role.

Exact redemption replay with the same invite and installation credential can
return the original result after invite expiry within an explicit bounded replay
window no longer than the existing 24-hour request maximum. A different
credential conflicts; installation revocation overrides replay. After replay
retention, a burned invite can never enroll again. There is no access/refresh
split, token endpoint, browser page, or session. Rotate by enrolling a new
immutable installation ID and revoking the old ID. Labels are nonunique
diagnostics, never revocation selectors.

First-start initialization under the lock durably creates one admin bootstrap
invite before serving. It writes the invite to an owner-private file and prints
only the authority identity and file path, never the secret in daemon logs.
Headless setup uses invite and credential files protected by its secret manager.
The MCP adapter never enrolls from a tool call. Except for health and `enroll`,
API routes require authentication. Apply rate limits and bounded bodies to the
unauthenticated routes.

Human identity is deliberately outside the authority. The authority knows
installations, roles, invites, and who issued each invite; it does not know
people. A future control plane that verifies a person by email, OIDC, or
payment, provisions a namespace, and hands out its bootstrap invite, or a policy
that auto-issues `write` invites to a verified domain, sits in front of this
contract without changing it. Letting a `write` installation issue invites at or
below its own role, so a person can enroll a second machine without an admin,
is a one-rule policy knob for later. Browser device authorization is likewise a
follow-up if manual invite distribution proves painful.

| Role | Grants |
| --- | --- |
| `read` | Redacted status, list, events, history, and watch for the namespace. |
| `write` | `read` plus acquire, renew, checkpoint, transfer, release, operation begin/renew/complete, inspection of epochs it holds credentials for, and reconciliation. |
| `admin` | `write` plus invite issuance and installation revocation, audited private inspection of ended epochs in the namespace, administrative claim revocation, GC apply, restore reopening, and database retirement. |

An invite carries its role; the redeemer does not choose one. Two machines used
by the same person are separate installations so either can be audited and
revoked independently. Namespace access is not fine-grained resource isolation
against hostile members: start with one trusted cooperative team per namespace.
Separate namespaces trade away cross-boundary atomic claims; finer ACLs need
their own requirement.

Credential rotation overlaps: redeem a new invite and durably store the new
credential before revoking the old installation ID. Revocation takes
effect on its next request, including pending exact replays, which fail
`installation-revoked`. Claims held by a revoked installation are not
force-released. Installation identity and claim ownership have separate
lifecycles; claims expire lazily or an admin revokes them explicitly.

Administrative claim revocation ends the epoch with reason `revoked`, leaves
started operations unresolved, and appends a public event. It is not executor
termination and must never be described as one. Another `write` installation
may acquire immediately under the normal contention checks and reconcile, using
the public request hash and, where needed, an admin's audited private inspection.
Removing a compromised installation therefore never requires granting it access
again.

An ordinary restart preserves installations and invites. A restored or imported
authority changes `restoreId`, revokes every retained old installation and
invite, and enters recovery mode. Ordinary enrollment and API invite issuance
are closed. The sole enrollment exception redeems the current-incarnation
offline bootstrap admin invite. That admin can inspect, acquire recovery
ownership, reconcile, collect old installation evidence out of band, and reopen;
normal re-invitation starts only after reopening.

The restore command runs with the server stopped and holds the single-writer
lock while it rotates `restoreId`, ends claims, revokes retained credentials,
sets recovery mode, and durably writes the bootstrap invite. An offline reissue
for a lost or expired bootstrap holds the same lock, invalidates its predecessor,
and leaves `restoreId`, claims, and history unchanged. Restoring rolled-back rows
must never resurrect revoked access.

### Transfer

Same-host transfer reuses local successor credential generation and the atomic
ownership transition. It also enforces the admitted remote hold limits described
above. Cross-host transfer is a follow-up because a claim bearer must never be
shared between executor installations; normal client-to-authority bearer
transmission over trusted TLS remains required. Until it lands, a handoff between
machines is release followed by acquire, with the free interval that implies.
The recorded design is two steps: the recipient calls
`transfer-prepare`, generating the successor claim ID and credential locally and
registering only the hash, bound to its installation and expiring with its own
`requestNotAfter` if unused; the holder then calls `transfer` naming that
successor, and the authority creates it at revision 1, carries the checkpoint,
and ends the predecessor in one transaction with no free interval. Its replay
and authorization contract remains unresolved. Before shipping, it must support
predecessor-authorized token-free replay and recipient outcome inspection after
a lost response without sharing either claim credential between installations.
Copying a handle between hosts remains unsupported and is not ownership transfer.

## Data governance

Choose the hosting region at namespace creation and treat it as immutable. A
managed edge such as a tunnel, CDN, or hosted proxy may process request metadata
outside the region, depending on its deployment. Retention and deletion of
private recovery context follow the same GC rules as the local authority;
deletion beyond GC requires an `admin` action and is refused for unresolved
operations. The operator must configure object-backup IAM, encryption, region,
and restore access explicitly; API roles do not carry over to object storage.
Backups are therefore covered by the restore incarnation rule and never by a
second live authority. Object-storage replication is optional: disabling it
removes replica-based disaster recovery but does not change the single-writer,
identity, restart, or claim-safety rules. Any later database import still enters
the restore procedure under a fresh incarnation.

## Possible follow-ups

These are separate opportunities, not requirements for the initial remote
authority. Each has a named trigger so it is pulled in by evidence, not by
preference:

- **Repository enrollment and aliases.** Remote `path`, `backlog-md`, and
  `markdown` keys through an admin-maintained catalog with the canonical
  relative locator and the migration rules recorded above. Trigger: a team for
  whom an agreed `coordination:` convention is not enough.
- **Cross-host transfer.** The two-step `transfer-prepare` design recorded
  above. Trigger: release-then-acquire handoffs between machines lose real
  contention races. Evidence must cover lost prepare and transfer responses and
  the unresolved token-free authorization and outcome-inspection contract.
- **Audited recovery import.** Recording lost-tail operations inside the
  restored ledger. Trigger: a restore drill in which operator accounting alone
  is insufficient.
- **Admission backpressure.** A storage or request ceiling that refuses new
  ordinary work while keeping lifecycle, completion, and reconciliation open,
  with pressure state separate from explicit recovery mode. Trigger: measured
  volume or write pressure.
- **Device authorization and a control plane.** A browser or code-approval
  login, self-service signups that provision a namespace and hand out its
  bootstrap invite, domain-based auto-invites, and `write`-issued invites. All
  sit in front of the invite contract. Trigger: manual invite distribution or
  namespace provisioning becomes the onboarding bottleneck.
- **Multi-namespace `serve`.** One process routing several databases. Trigger:
  process-per-namespace becomes an operational burden.
- **Optional managed backup.** Package continuous replication and restore drills
  for operators whose recovery targets justify them. Keep plain `worklease
  serve` usable with only a persistent SQLite volume, with the absence of a
  replica and its data-loss consequences explicit.
- **Managed Postgres backend.** Add it when measurements or deployment demand
  require managed durability, stateless replicas, or more write throughput.
  Extract the storage interface from SQLite and Postgres together, and require
  both to pass the same authority conformance scenarios.
- **Claim-scoped agent coordination.** Evaluate bounded structured handoff notes,
  release or transfer requests, recovery-required notices, and annotations tied
  to claim or operation IDs. Reuse checkpoints, events, and watches where their
  existing safety and privacy boundaries fit.

Do not turn the last follow-up into general chat, a durable message queue, task
dispatch, remote commands, or transcript storage. Those belong in an external
messaging or orchestration system, which can carry Worklease IDs for correlation
while Worklease remains the authority for ownership and recovery.

## Architecture review recommendations

The narrow core is sound. Reuse the Go service, keep one SQLite writer and one
namespace per server, admit explicit portable prefixes, enroll installations by
invite, and use ordinary claims under namespace recovery mode. Do not add
initial capacity tiers, recovery import, or cross-host transfer.

Two decisions remain disputed and must be resolved before the remote protocol is
frozen.

1. **Request incarnation binding.** Prefer an `expectedRestoreId` in the
   immutable saved remote request envelope and in handle or pending-request
   origin metadata. The server checks it before replay or mutation and binds it
   to the stored replay result or its digest. Responses and cursors continue to
   carry `restoreId`; secret formats do not change. Otherwise, an unacknowledged
   acquire saved under restore R0 can be absent from the restored snapshot, then
   sent after re-enrollment under R1 with the new installation bearer. The
   server has no R0 row to recognize and can commit after reopening, or during
   recovery if it covers a surviving unknown. Its R1 response matches the
   profile, so response-only detection misses the stale request. The alternative
   is response-only detection with every old request and handle bound to an
   immutable installation credential generation that cannot be reused with a
   fresh credential; the profile's latest value alone is insufficient. Choose
   one design, not both. Neither design terminates an old executor or prevents a
   delayed successful response, so cessation evidence remains required.
2. **Restore evidence.** If routine reopening is intended, prefer durable,
   independently retained history of dispatched and completed work for the
   supported backup horizon. The minimal initial alternative is manual external
   operator or provider evidence, with an explicit inability to reopen when
   exhaustive enumeration, outcome, or cessation evidence is absent. A client
   journal or synchronous acknowledged-write durability is a future option, not
   an implicit initial requirement. Recovery import cannot supply missing
   evidence. A fully completed operation lost from the backup tail demonstrates
   the evidence gap even when it creates no concurrent effect by itself.

## Remaining decisions and release evidence

The architecture and safety requirements above are design decisions. Their
mechanisms and failure behavior still require implementation and executable
evidence. The product goal—an opt-in self-hosted authority for experimentation—is
decided. Sharding, fencing counters, managed hosting, and Postgres implementation
remain explicit deferrals. The following questions still require measurements or
operator choices:

| Area | Open decision and what resolves it |
| --- | --- |
| Recovery targets | The operator sets acceptable acknowledged-write loss and recovery downtime before choosing deployment details. Async backup is not high availability; missing recovery evidence can block reopening indefinitely. |
| SQLite to Postgres trigger | Measured write latency/throughput, a requirement for managed-database durability, or a requirement for stateless `serve` replicas. Preference alone does not create a speculative abstraction, but cluster deployments are expected to be the strongest trigger. |
| Capacity and cost | Measured WAN latency, renewal margins, retry/watch bursts, per-namespace write throughput, replication lag, and storage growth under pinned history. These decide whether admission backpressure is needed and validate cost assumptions. |
| Hosting region and provider | An operator choice constrained by recovery targets. SQLite requires one always-on host and one persistent volume; object-storage replication is optional. |
| Operational ownership | Deployment owners supply runbooks for patching, invite issuance and credential rotation, restore, and reopening. Worklease ships the capability, not an operating service or SLA. |

Before private deployment, add executable scenarios covering at least:

1. Two hosts with different checkout roots and explicit user-side project
   bindings to the same profile contend on the same portable key; deliberately
   separate scopes do not. An unbound checkout uses local authority only when no
   explicit flag, environment setting, or user default selects a profile;
   repository content cannot select or redirect one. Delimiter boundaries are
   enforced, and reserved host-local prefixes fail `resource-not-enrolled` even
   through raw input or a bad allowlist. Prefix withdrawal and bound changes
   reject new admissions while lifecycle, recovery, same-host transfer, and
   exact replay continue. Over-bound requests are rejected without rewriting
   intent. Persisted limits constrain every expiry extension, including
   `BeginOperation`, `RenewOperation`, and same-host transfer.
2. A lost response is recovered from the same authority; a partition stops new
   client effects and never creates local fallback ownership. An old provider
   request completing after expiry remains an explicit recovery problem.
   Race revocation and policy changes against mutations and verify the
   transaction recheck plus authentication, authority, replay, and admission
   order. Verify that replay wraps historical results with fresh authority time.
   Exercise asymmetric request latency, drift margin, the 24-hour lower-bound
   formula, required initial reads, restart and suspend resampling, an expired
   short window, and a late successful start response that must not dispatch.
3. First-start initialization durably writes one protected admin invite file.
   Test hidden, file, and descriptor input without argv or log disclosure;
   trusted endpoint and authority matching; no credential-bearing redirect; and
   final profile activation only after recovered enrollment. Drop invite
   issuance and redemption responses, including across expiry, replay retention,
   mismatch, and revocation. Verify atomic burn, immutable installation IDs,
   rotation and revocation by ID, role isolation, and distinct
   `authentication-required` and `installation-revoked` MCP guidance.
4. Snapshot/watch races, disconnect/reconnect, lazy expiry, cursor gaps, and a
   stuck predecessor pinning retention preserve recovery state. A full volume
   fails writes with `storage-failure` and prunes nothing.
5. Restart, rolling protocol/schema upgrades, and restore from a replica preserve
   replay and unknown-operation semantics. Restart preserves `restoreId`;
   restore creates a fresh one, ends active claims as `restored`, revokes retained
   old rows, rejects old cursors, and returns `authentication-required` for a
   credential whose row was lost. Restore the same backup twice and reject the
   first restored incarnation at the second. Exercise the selected
   request-incarnation design with an R0 pending acquire sent after R1
   re-enrollment. Lose both a
   confirmed start and a fully completed operation from the backup tail and keep
   recovery closed until evidence covers the full cutoff-to-cessation interval.
   Verify transitive recovery closure, new-start refusal, and retained replay:
   completed operations return receipts while still-started operations return
   `unknown-outcome` and never permission to execute. Verify the sole bootstrap
   enrollment exception, lost-bootstrap reissue without another restore, and
   atomic reopening. A paused SQLite server
   keeps its stable inode lock and refuses a second server and alternate hosted
   writer, then permits takeover after exit. Ordinary concurrent local CLI and
   watch behavior remains intact; do not add a blanket process-lifetime lock or
   generic lock registry. When Postgres is added, run these backend-neutral
   scenarios against both implementations; lose an acknowledged start during
   standby promotion and require a fresh `restoreId` plus recovery mode, while
   transparent failover passes only under proven synchronous durability and
   split-brain prevention.

These are future acceptance scenarios, not assertions about shipped behavior.

## Deployment and implementation boundary

Client and server live in this repository because they are one binary in two
modes, not because a second implementation needs a shared conformance suite.
Deployment tooling owns the host, volume, replication, front door, and secrets;
`worklease` remains the claim client and authority, not a provisioning tool.

Do not add preparatory remote code to the local release outside an authorized
implementation slice. When implementation begins:

1. Resolve request-incarnation binding, then amend the contract's section 20
   under its amendment procedure. Replace the "front door such as Cloudflare
   Tunnel plus Access" wording because Worklease owns API authorization and a
   trusted edge terminates TLS. Do not declare the existing `restoreId` request,
   receipt, handle, and cursor binding superseded before choosing one disputed
   alternative above. Freeze an explicit allowed field mapping over the typed
   service requests plus the administrative surface named here, excluding local
   compatibility fields such as `LegacyRequestHash`, with `authorityTime`,
   `restoreId`, and the error reasons named here:
   `authority-restored`, `installation-revoked`, `authentication-required`,
   `resource-not-enrolled`, and the `restored` and `revoked` end reasons.
2. Add `restoreId` to `meta`, return it and `authorityTime` on every Worklease
   application response, embed it in cursors, and implement the selected request
   binding. Implement the hosted SQLite process-lifetime lock without changing
   normal concurrent local CLI behavior; the restore procedure; transitive
   recovery admission; serialized mutation rechecks and ordering; persisted TTL
   and hold limits on every extension path; and atomic reopening.
   The new `meta` row and the `restored` and `revoked` epoch end reasons change
   the shared SQLite schema: `epochs.end_reason` is CHECK-constrained to the
   three local reasons today. Bump `store.SchemaVersion` with a one-way
   in-place migration so the new binary upgrades a local home once and an older
   binary refuses it with `schema-unsupported`; the local release shares this
   store and cannot be exempted.
3. Implement `worklease serve`: HTTP handlers over the existing service methods,
   the `installations` and `invites` tables, the rate-limited `enroll` route,
   offline bootstrap and reissue, invite issuance and redemption replay, immutable
   installation IDs, role mapping, bounded bodies, non-cacheable responses, no
   resource keys or credentials in URLs or logs, and delimiter-terminated policy
   prefixes plus TTL/hold bounds from the server configuration file. Keep
   transport and authentication independent of SQLite implementation types.
4. Add authority profiles, user-side project bindings, OS credential storage
   with file and descriptor sources, hidden invite input plus `--invite-file` and
   `--invite-fd`, the client-side local/remote selector, authority-time bounds,
   and partition/lost-response tests. Keep
   guarded effects strictly local and never fall back from a configured remote
   profile.
5. Adapt `worklease mcp` as the local stdio adapter over that same client,
   preserving opaque lease references and local pending-request durability. Add
   distinct `authentication-required` and `installation-revoked` guidance
   without enrolling from a tool call.
6. Run the five scenario groups against a real two-host SQLite deployment behind
   its TLS edge, including a replica restore, before promoting the experimental
   capability.

Remote implementation is not made ready by this document. Sharding, a fencing
counter, provider-executed mutations, managed Postgres implementation,
multi-replica serving, browser login, a control plane, and a public hosted
product stay deferred until each has a concrete requirement.
