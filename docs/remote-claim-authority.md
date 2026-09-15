# Experimental remote claim authority

## Status and scope

The remote claim authority is **experimental**. It is an opt-in, self-hosted
capability in the standard Worklease binary, validated only within the limits of
the acceptance evidence linked below. Worklease does not host this service and
makes no high-availability, distributed-fencing, provider-transaction, or
exactly-once execution claim.

The authority coordinates cooperating clients across hosts. The guarded child,
provider CLI/API call, and file edit always run on the client host. The remote
service never executes a provider operation or a child process.

A standard binary is inert: it permanently opens no listener and makes no
network request unless the user explicitly invokes remote profile management,
selects a remote profile, or runs `serve`. Local reads remain setup-free. A
configured profile is not a reason for an unqualified local read to contact the
network unless that profile is selected by the normal precedence rules.

## Deployment boundary

One hosted authority is one Worklease namespace, one SQLite database, and one
`worklease serve` process. It has one serialized SQLite writer and one persistent
volume. Do not put a hosted database behind a load balancer with two writable
origins, copy a live database to a second writable location, or run two servers
against the same authority ID. SQLite WAL requires a single-host filesystem;
network filesystems are unsupported.

The server takes an exclusive process-lifetime hosted OS lock on the authority
home. `serve` and every offline hosted writer take this lock before opening
SQLite. The lock file is stable while held. A paused process retains the lock;
stop it before starting a replacement. The lock protects cooperating processes
on one host, not independent writable clones.

The server configuration is deployment-owned and is read only at startup. A
configuration change (including admitted prefixes, TTL/hold bounds, rates,
listen address, or TLS files) takes effect by a deliberate **stop before start**
restart. There is no hot reload, `serve --daemon`, remote configuration route,
or schema/backend-selection flag.

A canonical configuration uses these implemented fields:

```yaml
home: /srv/worklease
listen: 127.0.0.1:8443
tlsCert: /etc/worklease/server.crt
tlsKey: /etc/worklease/server.key
admittedPrefixes:
  - "github:"
  - "coordination:"
maxTTL: 1h
maxHold: 24h
shutdownTimeout: 5s
healthRate: 60
metadataRate: 60
enrollmentRate: 20
```

`listenAddress`, `tlsCertFile`, `tlsKeyFile`, and `prefixes` are implemented
aliases for `listen`, `tlsCert`, `tlsKey`, and `admittedPrefixes`. The nested
`rateLimits: {health: N, metadata: N, enrollment: N}` form is also accepted;
do not specify an alias and its canonical field together. `home`, `listen`,
admitted prefixes, positive `maxTTL` (1s–1h), positive `maxHold` (1s–24h), and
positive health/metadata/enrollment rates are required. TLS is required unless
`--allow-insecure-http` is explicitly supplied to `serve`; cleartext HTTP
exposes credentials and claim data to the network.

An optional asynchronous SQLite backup (for example, WAL replication to object
storage) is a disaster-recovery backup, not failover, a coordinator, or a
write fence. It can lose acknowledged writes. Choose and measure a backup
cutoff and recovery downtime; backup lag does not by itself bound the lost
history interval.

## Profiles and enrollment

Profiles and bindings are owner-private user files, separate from repository
configuration. Profile selection for commands that support an authority is,
in order: explicit `--profile NAME`, `WORKLEASE_PROFILE`, an explicit
user-side checkout binding, the user default, then local. `--local` is an
explicit local override and conflicts with `--profile` or `WORKLEASE_PROFILE`.
A configured remote failure never falls back to local. Endpoint changes require
removing and adding the profile; credential-bearing redirects are refused.

The exact profile commands are:

```text
worklease profile add NAME --endpoint URL --authority-id ID [--allow-insecure-http]
worklease profile list                         # alias: profile ls
worklease profile show NAME
worklease profile remove NAME
worklease profile default NAME
worklease profile bind NAME [--cwd DIR]
worklease profile unbind [--cwd DIR]
```

`profile add` performs bounded metadata discovery and pins the supplied
`authorityId` and the discovered `restoreId` before saving. Its only command
flags are `--endpoint URL`, `--authority-id ID`, and
`--allow-insecure-http`. `profile list` is local and setup-free; profile
credentials are not printed.

An admin issues a one-time invite to an owner-private file or inherited file
descriptor, then the new installation enrolls with that invite:

```text
worklease invite issue --profile NAME --role read|write|admin \
  (--invite-file FILE|--invite-fd N) [--label TEXT] [--expires-at RFC3339] \
  [--operation-id ID] [--request-not-after RFC3339]
worklease enroll --profile NAME (--invite-file FILE|--invite-fd N) [--label TEXT]
```

`invite issue` requires exactly one invite output source. It also accepts the
long-only replay flags `--operation-id ID` and `--request-not-after TIME`.
The plaintext invite is generated before dispatch; the file form is durably
saved first, while an fd caller owns durable capture from the descriptor. Only
the invite's SHA-256 is sent to the authority. `enroll` accepts exactly one file or fd
source in non-interactive use; an interactive terminal may prompt without echo.
The installation credential is client-generated and saved privately. Neither
bearer is accepted on argv, returned in output, or stored in the repository.
Roles are exactly `read`, `write`, and `admin`: reads use `read`, claim and
operation lifecycle uses `write`, and invite, installation, claim-revocation,
GC, and recovery administration uses `admin`. A higher role includes lower
permissions.

The exact administrative commands and flags are:

```text
worklease installation list --profile NAME [--include-revoked]
worklease installation revoke --profile NAME --installation-id ID [--reason TEXT] \
  [--operation-id ID] [--request-not-after RFC3339]
worklease claim revoke --profile NAME --claim-id ID [--reason TEXT] \
  [--operation-id ID] [--request-not-after RFC3339]
worklease recovery status --profile NAME
worklease recovery reopen --profile NAME --expected-recovery-revision N \
  --attestation-file FILE [--operation-id ID] [--request-not-after RFC3339]
```

Reasons are stable machine-readable outcomes, not prose to parse. Remote
failures include `authentication-required`, `installation-revoked`,
`authorization-denied`, `authority-mismatch`, `authority-restored`,
`resource-not-enrolled`, `recovery-required`, `recovery-closed`,
`already-claimed`, `stale-claim`, `stale-revision`, `operation-request-mismatch`,
`unknown-outcome`, `operation-ambiguous`, `replay-expired`,
`operation-kind-unsupported`, `invite-invalid`, `invite-expired`, and
`invite-used`, as well as the common validation, rate-limit, storage, and clock
reasons. Exit families remain `2` ownership/contention, `3` ledger/replay or
ambiguous outcome, `64` invalid input/configuration, and `75` authority/storage
failure. Details are bounded and redacted.

## Remote CLI surface

Existing claim lifecycle, inspection, watch, and guarded-operation commands use
the selected profile: `acquire`, `status`, `list`, `heartbeat`, `checkpoint`,
`release`, `transfer`, `verify`, `exec`, `op inspect`, `op reconcile`,
`events`, `history`, `watch`, and `gc`. Use the existing flags documented in the
[CLI reference](cli-reference.md), plus global `--profile NAME` or `--local`.
Remote `--wait` remains a client loop and is capped at 60 seconds. Remote
`acquire` rejects explicit `--poll-interval`; the server owns polling. Remote
GC is apply-only: use `gc --apply` with `--cutoff TIME` or
`--retention-days N` (default 30 days); a remote GC preview is unsupported. Every remote
mutation, including `--no-handle`, records a durable exact pending request
before dispatch. A failed request after dispatch is uncertain: inspect or
replay the same request, never issue a new effect.

Remote admission accepts only configured portable prefixes. `path:`,
`backlog-md:`, and `markdown:` are host-local and always rejected remotely.
Use an agreed portable `github:` or `coordination:` resource (including a
`generic` source) on every host. Remote same-host transfer requires a named
predecessor handle and remains same-installation; cross-host transfer is not
implemented.

## Local stdio MCP

The remote authority is not an MCP endpoint. `worklease mcp` remains a local,
one-process stdio adapter. With a selected profile it keeps credentials,
opaque lease handles, and pending requests on the client host and calls the
remote HTTPS client. The eleven existing tools are exactly:

```text
key acquire status list heartbeat checkpoint verify watch events release instructions
```

The MCP command and setup remain:

```text
worklease mcp
worklease setup mcp --client claude-code|cursor
```

MCP uses the existing lease arguments: `resources`, selected provider/source/item
or `path`, `ttl`, `wait` (0–60 seconds), `workKey`, `agentId`, `sessionId`,
`coordinationOnly`, `autoHeartbeat`, `maxHold`, an opaque `lease` reference,
`data`, `reason`, `cursor`, `limit`, `until`, and `timeout`. It does not add
profile, enrollment, administration, transfer, exec, replacement,
reconciliation, history, recovery, or remote-server tools. MCP never redeems
an invite. `key` and instructions are client-local, and remote host-local keys
are rejected by remote admission.

## Hosted operator commands

All hosted commands are offline-only, never use a remote profile, and take the
hosted lock before opening SQLite:

```text
worklease hosted init --home DIR --server-config FILE --bootstrap-invite-file FILE
worklease hosted restore --home DIR --from FILE --selected-cutoff RFC3339 \
  --loss-interval-start RFC3339 --loss-interval-end RFC3339 \
  --bootstrap-invite-file FILE [--cutoff-unknown]
worklease hosted bootstrap-reissue --home DIR --bootstrap-invite-file FILE
worklease hosted retire --home DIR [--force --unresolved-export FILE]
worklease serve --server-config FILE [--allow-insecure-http]
```

`hosted init` stages the bootstrap secret before the authority transaction and
writes a one-time admin invite. `bootstrap-reissue` replaces only that active
invite. `serve` uses only the server configuration file for its listen address,
TLS, admission bounds, rates, and home; `--allow-insecure-http` is its only
network override.

Configuration changes are therefore: stop `serve`, edit the deployment-owned
file, verify the hosted home and backup, then start exactly one `serve` process.
Do not hot-edit a running configuration or start a second process. Keep the
hosted home, lock, database, WAL sidecars, readiness marker, and private
secrets on the same trusted single-host filesystem.

## Restore, recovery, and reopening

Restore is authority recreation, never resumption. `hosted restore` installs an
owner-private backup under the hosted lock, creates a fresh random `restoreId`,
ends active claims with reason `restored`, revokes old installation credentials
and invites, leaves started operations unresolved, and enters namespace recovery
mode. Old handles and cursors fail closed (`stale-claim` or
`authority-restored`); an absent old credential is `authentication-required` and
a retained revoked credential is `installation-revoked`.

The operator records the selected durable backup cutoff. If it is unknown, pass
`--cutoff-unknown`; the lost-history bound is then unknown. Always record the
loss interval start and end through cessation of the old authority, not merely
observed backup lag. A delayed provider request, escaped descendant, or client
whose response was lost can outlive a terminal receipt, an expired claim, an
empty pending directory, or the old authority process. Recovery must account
for it.

Recovery mode refuses new `BeginOperation` and ordinary unrelated acquires.
Renewal, checkpoint, release, completion, reconciliation, inspection, and
same-host transfer remain available. A recovery acquire must cover the full
transitive resource closure of an unresolved predecessor and still fit the
1–32-resource limit. It does not grant permission to redispatch an effect.
Resolve retained operations by exact replay or reconciliation with explicit
outcome and cessation evidence; a retained started operation remains
`unknown-outcome` until resolved.

Before reopening, the admin attestation and private evidence references must
cover all of the following:

1. An independent inventory of every active, retired, and ephemeral/CI
   installation that may have acted since the selected backup.
2. An enumerable pending-request set for each installation, or equivalent
   independently verified evidence proving exhaustive pending coverage.
3. Both retained outcomes and the lost-tail outcomes: every retained started
   operation is reconciled, and work that may exist only outside the restored
   ledger is accounted for.
4. Provider cessation and executor/process cessation for every known in-flight
   operation, including operations with terminal receipts and cases where a
   pending set is empty. Completion is not cessation and an empty pending set
   is not provider history.
5. The selected durable cutoff and the interval from that cutoff through old
   authority cessation. If the selected cutoff or interval bounds are unknown,
   say so explicitly; an unknown bound never waives any other required coverage.
6. Namespace-wide cessation coverage, including delayed provider effects after
   the old authority stops. Missing inventory, pending-set, outcome, or cessation
   coverage keeps recovery closed indefinitely.

`recovery reopen` reads the owner-private bounded JSON attestation from
`--attestation-file`. The attestation carries `inventoryComplete`,
`pendingSetsComplete`, `retainedOutcomesComplete`,
`namespaceCessationEstablished`, and private `evidenceReferences`; Worklease
validates structure and retained rows but cannot verify external truth. Never
put credentials, argv, provider payloads, checkpoints, receipts, or evidence
dumps in public events, logs, MCP results, or the attestation itself.

A fully missing completed operation may remain an attested history gap only when
independent evidence establishes that no residual provider or executor effect
exists. Never redispatch a missing row. Recovery import and a completed-history
journal are unsupported: they remain deferred until a recovery drill shows that
manual accounting lacks required evidence or a recovery target cannot be
established. An export, read replica, or backup is not permission to remove
unknown state or to reopen without the coverage above.

## Retirement and unsupported boundaries

`hosted retire` refuses active claims or unresolved started operations. Forced
retirement requires `--force --unresolved-export FILE`, writes a redacted export
outside the hosted home, and records only safe active/unresolved metadata. It is
not a recovery import and does not erase unresolved risk. Retirement is offline;
there is no HTTP retirement route.

The experimental release explicitly does **not** support:

- remote `replace-file` or remote file replacement;
- remote provider execution, provider cessation, or provider-side fencing;
- recovery import or a completed-history journal;
- cross-host transfer, repository enrollment, or host-local `path`, `backlog-md`,
  and `markdown` enrollment;
- high availability, multi-writer serving, multi-namespace serving, or a
  Postgres backend;
- server-side acquire queues, admission backpressure, or FIFO fairness;
- hot configuration reload;
- browser/OAuth login or a browser control plane;
- credential-bearing redirects, arbitrary proxy identity headers, or a
  repository-committed profile/enrollment configuration.

These are not hidden fallbacks. Unsupported operations return a structured
capability or validation reason, and configured remote failures never silently
switch to local coordination.

Follow-up triggers remain explicit:

- add repository enrollment and aliases only after deployments need host-local
  policy keys across checkout roots and the canonical locator vectors pass on
  every supported platform;
- add cross-host transfer only when ownership handoff between installations is a
  demonstrated workflow, with a protocol that cannot copy bearer credentials;
- add recovery import when a restore drill shows that manual accounting cannot
  represent already-identified lost-tail operations;
- add a completed-history client journal when a drill cannot establish required
  coverage or meet its selected recovery target; the journal still cannot prove
  provider or executor cessation;
- add admission backpressure only after measured disk, WAL, pinned-history, or
  request-size pressure shows lifecycle capacity needs protection;
- add browser login or a control plane only after deployment demand justifies a
  separate authenticated management surface;
- add multi-namespace serving only after demand justifies per-namespace routing,
  locks, credentials, and load isolation;
- add Postgres or multiple service replicas only after measured throughput or
  managed-durability requirements justify a second backend and its conformance
  and failover evidence; add fencing counters only with an enforcing consumer.

## Evidence and release artifacts

The [TASK-107.11 acceptance record](backlog/tasks/task-107.11%20-%20Build-the-two-host-acceptance-harness-and-run-scenario-groups-1-to-5.md)
records the retained reports at
`dist/remote-acceptance/task-107-11-final-local-reviewed-6/report.json` and
`dist/remote-acceptance/vm-20260915T041350Z/report.json`. The unchanged harness
passed all five groups locally and in the repository-managed Lima VM; the VM run
put the authority and one client across SSH with target-architecture binaries.
This is measured acceptance evidence, not proof of HA, fencing, provider
cessation, cross-host transfer, or recovery reliability beyond the scenarios
and limits recorded in those reports.

Release artifact evidence is Actions run
[34915583460](https://github.com/brettinternet/worklease/actions/runs/34915583460),
commit `7e4cef4312d09e380d5c02cea7f23bd13ab66a9c`. All four target archive jobs
and native smoke passed, and artifacts were executed only on their matching
runners:

| Target | Archive delta vs v1.2.0 | Binary delta vs v1.2.0 |
| --- | ---: | ---: |
| linux-arm64 | +2,147,600 B | +5,242,880 B |
| linux-x64 | +2,383,219 B | +5,763,072 B |
| macos-arm64 | +2,218,770 B | +5,344,688 B |
| macos-x64 | +2,396,120 B | +5,856,672 B |

The workflow dispatched artifact jobs only; it did not publish, tag, or push.
Public publication, tagging, pushing, and release execution require separate
owner authorization. Promotion from experimental status likewise requires
additional evidence and an explicit product decision.
