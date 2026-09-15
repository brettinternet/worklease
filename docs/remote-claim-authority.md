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

Each authority has these deployment invariants:

| Invariant | Required practice | Not supported |
| --- | --- | --- |
| One writer | Run one `worklease serve` process against one SQLite database and persistent volume. | Multiple writable origins or copies |
| Local WAL | Keep the complete authority home on one single-host filesystem. | Network filesystems |
| Process lock | Stop the lock holder before starting its replacement; a paused process still holds the lock. | Using the hosted lock to protect independent clones |
| Startup configuration | Stop, edit, then start. The deployment-owned file is read once at startup. | Hot reload, remote configuration, or `serve --daemon` |

`serve` and every offline hosted writer take the process-lifetime hosted lock
before opening SQLite.

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

Configuration rules:

| Canonical field | Accepted alias | Constraint |
| --- | --- | --- |
| `listen` | `listenAddress` | Required |
| `tlsCert` | `tlsCertFile` | Required unless insecure HTTP is explicit |
| `tlsKey` | `tlsKeyFile` | Required unless insecure HTTP is explicit |
| `admittedPrefixes` | `prefixes` | Required |
| `maxTTL` | — | 1s–1h |
| `maxHold` | — | 1s–24h |
| `healthRate`, `metadataRate`, `enrollmentRate` | nested `rateLimits` | Positive |

`home` is also required. Do not combine a canonical field with its alias.
Cleartext HTTP requires `allowInsecureHTTP: true` or
`serve --allow-insecure-http` and exposes credentials and claim data.

An optional asynchronous SQLite backup (for example, WAL replication to object
storage) is a disaster-recovery backup, not failover, a coordinator, or a
write fence. It can lose acknowledged writes. Choose and measure a backup
cutoff and recovery downtime; backup lag does not by itself bound the lost
history interval.

## Profiles and enrollment

Profiles and bindings are owner-private user files, never repository config.
Selection order is:

1. `--profile NAME`
2. `WORKLEASE_PROFILE`
3. user-side checkout binding
4. user default
5. local authority

`--local` overrides bindings/defaults and conflicts with explicit profile
selection. Remote failures never fall back to local. To change an endpoint,
remove and re-add the profile. Credential-bearing redirects are refused.

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

Invite and enrollment rules:

- `invite issue` requires one output source and supports the long-only replay
  flags shown above.
- File output is durable before dispatch; fd callers own durable capture.
- Only the invite SHA-256 reaches the authority.
- Non-interactive enrollment requires one file or fd; terminals may prompt
  without echo.
- Installation credentials are client-generated and private.
- Neither bearer appears on argv, in output, or in the repository.

| Role | Permission |
| --- | --- |
| `read` | Read state |
| `write` | Read plus claim and operation lifecycle |
| `admin` | Write plus invites, installations, revocation, GC, and recovery |

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

Remote reasons are stable values, not prose to parse:

```text
authentication-required  installation-revoked  authorization-denied
authority-mismatch       authority-restored     resource-not-enrolled
recovery-required        recovery-closed        already-claimed
stale-claim              stale-revision         operation-request-mismatch
unknown-outcome          operation-ambiguous    replay-expired
operation-kind-unsupported  invite-invalid      invite-expired  invite-used
```

Common validation, rate-limit, storage, and clock reasons also apply. Exit
families remain `2` ownership/contention, `3` ledger/replay/ambiguity, `64`
invalid input/configuration, and `75` authority/storage. Details are bounded and
redacted.

## Remote CLI surface

Lifecycle, inspection, guarded-operation, event, watch, and GC commands use the
selected profile. See the [CLI reference](cli-reference.md) for shared flags.
Remote differences:

- `--wait` is a client loop capped at 60 seconds.
- `acquire --poll-interval` is rejected; the server owns polling.
- GC is apply-only: use `gc --apply` with `--cutoff TIME` or
  `--retention-days N` (default 30).
- Every mutation, including `--no-handle`, records its exact pending request
  before dispatch.
- After an uncertain dispatch, inspect or replay the same request; never issue a
  new effect.

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

MCP keeps its existing lease arguments and adds no profile, enrollment,
administration, transfer, exec, replacement, reconciliation, history, recovery,
or server tools. It never redeems invites. `key` and instructions remain
client-local; remote admission rejects host-local keys.

## Server operator commands

All server lifecycle commands are offline-only, never use a remote profile, and take the
hosted lock before opening SQLite:

```text
worklease server init [--server-config FILE] [--bootstrap-invite-file FILE]
worklease server restore --home DIR --from FILE --selected-cutoff RFC3339 \
  --loss-interval-start RFC3339 --loss-interval-end RFC3339 \
  --bootstrap-invite-file FILE [--cutoff-unknown]
worklease server bootstrap-reissue --home DIR --bootstrap-invite-file FILE
worklease server retire --home DIR [--force --unresolved-export FILE]
worklease serve [--server-config FILE] [--allow-insecure-http]
```

With no arguments, `server init` creates this local-only setup:

| Item | Default |
| --- | --- |
| Configuration | `$XDG_CONFIG_HOME/worklease/server.yaml` |
| Authority | `$XDG_STATE_HOME/worklease/server` |
| Bootstrap invite | Beside the configuration |
| Listener | `127.0.0.1:8443` |
| Admission | `coordination:` resources |
| Transport | Explicit insecure HTTP |

Files are owner-private. Before exposing the server to another host, edit the
configuration and enable TLS.

`server init` stages the bootstrap secret before the authority transaction and
writes a one-time admin invite. `bootstrap-reissue` replaces only that invite.
`serve` resolves configuration in this order:

1. `--server-config`
2. `WORKLEASE_SERVER_CONFIG`
3. `$XDG_CONFIG_HOME/worklease/server.yaml`

The file defines the listener, TLS, admission bounds, rates, home, and optional
insecure HTTP. Apply changes with the stop-before-start procedure in
[Deployment boundary](#deployment-boundary).

## Restore, recovery, and reopening

Restore recreates an authority; it never resumes one.

| Phase | Behavior |
| --- | --- |
| `server restore` | Installs the private backup under lock, creates a new `restoreId`, ends claims as `restored`, revokes credentials/invites, and preserves started operations as unresolved. |
| Recovery mode | Rejects `BeginOperation` and unrelated acquires; permits renewal, checkpoint, release, completion, reconciliation, inspection, and same-host transfer. |
| Resolution | Requires exact replay or reconciliation with outcome and cessation evidence. Started work remains `unknown-outcome` until resolved. |
| `recovery reopen` | Reopens admission only after complete attested coverage. |

Recover in this order:

1. Record the durable backup cutoff and loss interval through old-authority
   cessation. Use `--cutoff-unknown` when needed; backup lag is not the bound.
2. Enumerate the private evidence below.
3. Replay or reconcile every retained started operation. Never redispatch a
   missing row.
4. Write the bounded, owner-private JSON attestation.
5. Run `recovery reopen`; any evidence gap keeps recovery closed.

| Evidence | Required coverage |
| --- | --- |
| Installation inventory | Every active, retired, ephemeral, and CI installation that may have acted since the backup. |
| Pending requests | An enumerable set per installation, or an independently verified exhaustive equivalent. |
| Outcomes | Retained started operations and lost-tail work absent from the restored ledger. |
| Cessation | Provider and executor cessation for every in-flight operation. Completion is not cessation; an empty pending set is not provider history. |
| Loss window | The durable cutoff through old-authority cessation. Unknown bounds waive nothing. |
| Namespace | Delayed provider effects after old-authority shutdown. |

The attestation contains `inventoryComplete`, `pendingSetsComplete`,
`retainedOutcomesComplete`, `namespaceCessationEstablished`, and private
`evidenceReferences`. Worklease validates its structure and retained rows, not
external truth. Keep credentials, argv, payloads, checkpoints, receipts, and
evidence dumps out of the attestation and public output.

Old handles and cursors fail closed as `stale-claim` or `authority-restored`.
Missing old credentials return `authentication-required`; retained revoked ones
return `installation-revoked`. A recovery acquire must cover its predecessor's
full transitive resource closure within the 1–32-resource limit and never
authorizes redispatch.

A missing completed operation needs independent evidence of no residual effect.
Exports, replicas, and backups cannot remove unknown state or substitute for
full reopening evidence. Recovery import and a completed-history journal remain
deferred.

## Retirement and unsupported boundaries

`server retire` refuses active claims or unresolved started operations. Forced
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

Future capabilities need demonstrated demand or evidence:

| Capability | Trigger |
| --- | --- |
| Repository enrollment | Cross-root host-local keys are needed and locator vectors pass on every platform. |
| Cross-host transfer | Installation handoff is demonstrated without copying bearer credentials. |
| Recovery import | A drill proves manual accounting cannot represent known lost-tail work. |
| Completed-history journal | A drill cannot establish coverage or meet its target; a journal still cannot prove cessation. |
| Admission backpressure | Measured disk, WAL, pinned-history, or request-size pressure threatens lifecycle capacity. |
| Browser/control plane | Deployment demand justifies a separate authenticated surface. |
| Multi-namespace serving | Demand justifies isolated routing, locks, credentials, and load. |
| Postgres or replicas | Measured throughput/durability needs justify backend conformance and failover evidence. Fencing counters also need an enforcing consumer. |

## Evidence and release artifacts

The [TASK-107.11 acceptance record](backlog/tasks/task-107.11%20-%20Build-the-two-host-acceptance-harness-and-run-scenario-groups-1-to-5.md)
links two retained reports:

- `dist/remote-acceptance/task-107-11-final-local-reviewed-6/report.json`
- `dist/remote-acceptance/vm-20260915T041350Z/report.json`

The same harness passed all five groups locally and in the repository-managed
Lima VM. The VM placed the authority and one client across SSH using native
binaries. This evidence proves only those recorded scenarios—not HA, fencing,
provider cessation, cross-host transfer, or broader recovery reliability.

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
