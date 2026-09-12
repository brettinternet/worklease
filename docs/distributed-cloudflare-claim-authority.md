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
