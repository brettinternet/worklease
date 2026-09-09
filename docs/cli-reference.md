# CLI reference

The README covers installation and a complete lifecycle. Per-command options and
examples live in `worklease <command> --help` and `worklease --help-all`. This
page holds the contracts that command help cannot express: exit codes, the short
option namespace, state selection, the supported API surface, garbage-collection
semantics, and the human-readable text grammar.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `2` | Lease or capability conflict, such as `already-claimed`, `stale-claim`, `invalid-token`, or `claim-expired`. |
| `3` | Idempotency, version, or unknown-outcome failure, such as `operation-id-request-mismatch` or `unknown-outcome`. |
| `64` | Invalid input. |
| `75` | Storage failure, or a durable mutation whose lease handle could not be written. |
| `124` | A guarded child exceeded `--max-duration` (`child-process-timeout`). |

`exec` returns the child's status once the child has started. When a mutation
commits but its `--lease-file` handle cannot be written, Worklease still emits
the successful payload, including the bearer token, adds a `leaseFileError`
field, and exits `75`; the token in that payload is the only copy.

Rely on stable `reason` values and exit codes rather than message text.

## Short option namespace

Every short option means one thing across the whole command tree. Long options
are the stable interface; short options are a convenience.

| Short | Long | Available on |
| --- | --- | --- |
| `-h` | `--help` | all commands |
| `-f` | `--format` | global and output-producing commands |
| `-j` | `--json` | global and output-producing commands |
| `-H` | `--home` | global and commands that access configuration or state |
| `-v` | `--version` | global |
| `-p` | `--provider` | `key` |
| `-i` | `--item` | `key` |
| `-C` | `--coordination-only` | `key`, `acquire`, `acquire-bundle` |
| `-n` | `--name` | `policy describe` |
| `-r` | `--resource` | claim, status, history, inspection, list, and bundle commands |
| `-c` | `--claim-id` | claim lifecycle commands |
| `-a` | `--agent-id` | `acquire`, `acquire-bundle` |
| `-s` | `--session-id` | `acquire`, `acquire-bundle` |
| `-w` | `--work-key` | `acquire`, `acquire-bundle` |
| `-W` | `--wait-timeout` | `acquire` |
| `-P` | `--poll-interval` | `acquire` |
| `-t` | `--token` | authenticated lifecycle commands |
| `-F` | `--token-file` | authenticated lifecycle commands |
| `-D` | `--token-fd` | authenticated lifecycle commands |
| `-R` | `--revision` | authenticated lifecycle commands |
| `-o` | `--operation-id` | inspection and mutating lifecycle commands |
| `-T` | `--ttl` | acquire and renewable lifecycle commands |
| `-M` | `--max-duration` | exec commands |
| `-V` | `--verbose` | `status` |
| `-I` | `--target-operation-id` | reconciliation commands |
| `-x` | `--expected-request-sha256` | reconciliation commands |
| `-O` | `--outcome` | reconciliation commands |
| `-e` | `--evidence` | reconciliation commands |
| `-k` | `--checkpoint` | `checkpoint` |
| `-A` | `--successor-agent-id` | `transfer` |
| `-S` | `--successor-session-id` | `transfer` |
| `-m` | `--reason` | release commands |
| `-d` | `--provider-directory` | exec commands |
| `-g` | `--git-primary` | exec commands |

Short options may be grouped (`-jg`). Removed aliases and their long-form
replacements are tracked in [CHANGELOG.md](../CHANGELOG.md).

## State selection

State is selected by `--home`, then `WORKLEASE_HOME`, then
`XDG_STATE_HOME/worklease`, defaulting to `~/.local/state/worklease`. Use an
absolute, private path. Never use a repository-relative state path across linked
worktrees, because each checkout would create a separate lease authority.

## Supported API surface

The supported lease API is the symbol list in `worklease.__all__`; the supported
resource-policy extension API is `worklease.adapters.__all__`. Both follow
semantic versioning. JSON responses use schema version 1; consumers must ignore
unknown fields. Published schemas live in `worklease/schemas/v1/`, and every
distribution includes those schemas and `worklease/py.typed`.

## Local history, retention, and archival

`history --resource R` is a read-only projection of the retained local epochs
for exactly `R`. It includes singleton epochs and bundle epochs containing `R`,
plus safe operation and reconciliation summaries. Each acquisition epoch has
`SOURCE epoch`; operation, reconciliation, termination, and current claim
objects have `SOURCE operation`, `SOURCE reconciliation`, `SOURCE termination`,
and `SOURCE current-claim` respectively. Acquisition is synthesized from the
epoch row, not reported as an operation, and reconciliation outcomes remain in
the reconciliation object rather than being copied into an operation.

Each epoch has exactly one derived `COMPLETENESS` value: `complete` means a
stored termination is present; `open` means a matching current claim snapshot
is present and no termination is stored; `legacy-incomplete` means the
acquisition revision is null or neither ending source is retained. `open`
describes retained epoch closure, not current-clock activity, so a past stored
expiry does not make the epoch active or complete. The command does not provide
all-resource output, provider lookups, pagination, or time filters.

The `COVERAGE` block reports nullable `EARLIEST_RETAINED_ACQUISITION_REVISION`,
nullable `RESOURCE_REVISION_WATERMARK`, and `LEGACY_INCOMPLETE_COUNT`. The
earliest value is the minimum non-null acquisition revision among retained
singleton and bundle-member epochs for `R`; the watermark is the monotonic
`resources` revision, including its retained tombstone. Revision coverage can
show that stamped acquisition history starts after earlier activity, but cannot
identify missing events: garbage collection and pre-migration history are
indistinguishable, and non-acquisition mutations also consume revisions. A
never-seen resource has both revisions null and a zero count; after all epochs
are collected, earliest is null while the watermark remains when its tombstone
is retained.

Where an end bound is stored, consumers may project the half-open held-at
interval `acquiredAt <= T < termination.effectiveAt` for a terminated epoch or
`acquiredAt <= T < currentClaim.expiresAt` for an open epoch. A
legacy-incomplete epoch without either end source has an unknown upper bound.
This is a consumer rule over retained local rows, not an audit fact. The
command intentionally has no `--at` or current-clock input.

`--json` is a deterministic, positive-allowlist diagnostic export suitable for
redirecting one resource before collection:

```sh
worklease history --resource local:formatter --json > formatter-history.json
```

The export is sanitized, not a complete archive. It never includes bearer
tokens or token hashes, checkpoint bodies, request or receipt blobs,
reconciliation or provider evidence, argv, stdout, stderr, or file contents;
termination exposes only whether a checkpoint was present. JSON contains no
export-time timestamp or clock-derived active state, and repeated reads of
unchanged SQLite state are byte-identical. History is bounded by the same
local retention policy as the underlying records; rows already removed by
`gc --apply` cannot be recovered.

For a complete local archive, make a private SQLite backup before collection
(and preserve the private state-directory permissions):

```sh
sqlite3 "${WORKLEASE_HOME:-${XDG_STATE_HOME:-$HOME/.local/state}/worklease}/leases.sqlite3" \
  ".backup '$HOME/worklease-archive.sqlite3'"
chmod 600 "$HOME/worklease-archive.sqlite3"
```

The backup contains the retained local coordination database, including
secret-bearing lifecycle material. Treat it as private state; do not publish
or redirect it as a history export.

`gc` is a read-only dry run unless `--apply` is supplied. The default retention
window is 30 days.

```sh
worklease gc
worklease gc --retention-days 90
worklease gc --cutoff 2026-01-01T00:00:00Z --apply
```

The result reports deterministic counts, oldest eligible timestamp, and newest
eligible timestamp for each category:

| Category |
| --- |
| epochs |
| bundle epochs |
| operations |
| releases |
| reconciliations |
| resource metadata |

Records strictly older than the captured cutoff are eligible. GC always
protects:

- active claims;
- expired but unreclaimed claims;
- current ownership;
- unresolved started operations;
- records inside the retention window.

Applying GC uses one immediate SQLite transaction. Interruption leaves either
the state from before collection or the committed state after collection.
Resource revision tombstones preserve monotonic revisions after old metadata is
removed.

Use an explicit cutoff for repeatable maintenance. Back up the normal state
database before applying a destructive collection.

Invalid cutoffs, unsupported retention values, storage conflicts, and
protected-record conflicts fail without partial deletion. GC does not reconcile
unknown operations or provide verbose diagnostics. Use the dedicated inspection
and diagnostic commands for those cases.

## Human-readable text grammar

Text is the stable default for people. Automation must explicitly request
schema-versioned JSON with `--json` or `--format json`.

Output is UTF-8, newline-delimited, and deterministic:

- Fields use the order documented below.
- Tabs separate columns.
- Scalar values use compact JSON-compatible escaping without spaces.
- Printable Unicode remains readable.
- Control characters, C1 characters, and DEL are escaped as `\u0000` through
  `\u001f`, `\u007f` through `\u009f`.
- Escaping prevents values from creating lines or columns.

Successful operations normally begin with `OK <operation>`. Failures begin with
`ERROR <operation>: <reason>` and may contain only these diagnostic fields:

```text
RESOURCE
OPERATION_ID
TARGET_OPERATION_ID
PROVIDER
FIELD
CLAIM_ID
revision bounds
STATE
GUARANTEE
PROVIDER_FENCING
EXPECTED_REQUEST_SHA256
AVAILABLE
```

Parser failures use `ERROR <command>: invalid-arguments`. When no command is
identifiable, they use `ERROR parse: invalid-arguments`. Parser failures keep
the parser exit status and may include a safe command-specific `HINT` with an
example or valid values. Hints never echo rejected argument values.

### Command output

| Commands | Ordered output |
| --- | --- |
| `version` | Version only |
| `instructions loop`, `instructions safety` | One concise instruction per line |
| `key` | `OK key`, `PROVIDER`, `RESOURCE`, `SCOPE`, `CAPABILITY`, `GENERIC_EXECUTION_GUARANTEE`, `FENCED_MUTATIONS`, `PROVIDER_FENCING` |
| `policy list` | Header `NAME`, `ORIGIN`, `ORIGIN_VERSION`, `CONTRACT_VERSION`, `KEY_POLICY_VERSION`, `SCOPE`, `CAPABILITY`, `GENERIC_EXECUTION_GUARANTEE`, `PROVIDER_FENCING_SUPPORTED`, then rows |
| `policy describe` | One `FIELD: value` line per policy field |
| `list` | Fixed-width `STATE`, `RESOURCE`, `CLAIM_ID`, `OWNER_ID`, `EXPIRES_AT`, then rows |
| `history` | `OK history`, `RESOURCE`, a `COVERAGE` block, `EPOCHS`, then retained `EPOCH` blocks with identity, operations, reconciliations, termination, and current snapshots |
| `status`, `status-bundle`, `bundle-status`, `inspect-bundle` | `OK`, optional `RESOURCE` or `RESOURCES`, `STATE`, then `CLAIM` fields `RESOURCE` or `RESOURCES`, `CLAIM_ID`, `AGENT_ID`, `SESSION_ID`, `OWNER_ID`, `WORK_KEY`, `REVISION`, `EXPIRES_AT`, `GUARANTEE` |
| `status --verbose` | Resource and state, full redacted `CLAIM`, `UNKNOWN_OPERATIONS`, `RELEASE`, and optional `GUIDANCE` |
| `inspect-operation`, `inspect-operation-bundle` | `OK`, identity, kind, state, outcome, hashes, and reconciliation timestamps when present |
| `gc` | `OK gc`, retention fields, then sorted `ELIGIBLE` rows with count, oldest, and newest timestamps |
| Claim mutations and guarded commands | `OK`, operation and mutation fields, then `CLAIM` with resource(s), `CLAIM_ID`, `AGENT_ID`, `SESSION_ID`, `OWNER_ID`, `WORK_KEY`, revision, expiry, and guarantee |

`list` uses a fixed-width, space-padded table. Widths follow terminal columns:
East Asian wide characters count as two, and combining marks count as zero.

| Field | Default width |
| --- | ---: |
| `RESOURCE` | 52 |
| `CLAIM_ID` | 18 |
| `OWNER_ID` | 24 |
| `EXPIRES_AT` | 16 |

Long paths keep recognizable repository, source, and item boundaries. Other
long values keep a prefix and suffix around an ellipsis. Active expiry values
use approximate durations such as `1h 2m`; expired values use labels such as
`expired 3m`.

`worklease list --full` and JSON preserve complete values. An empty policy list
emits its header only. An unclaimed status emits `CLAIM <none>`. Tokens are
never listed.

`history` emits `OK history`, `RESOURCE`, a `COVERAGE` block, and `EPOCHS`.
Each `EPOCH` includes `SOURCE`, acquisition identity, acquired time,
acquisition revision, and `COMPLETENESS`. It then emits an `OPERATIONS` count
and ordered safe operation summaries whose first column is `SOURCE operation`,
followed by `RECONCILIATIONS` rows whose first column is `SOURCE
reconciliation`. `TERMINATION` and `CURRENT_CLAIM` each contain their stored
snapshot and source or `<none>`. Epochs sort by acquisition revision, with
acquired time and stable IDs only for legacy fallback; operations sort by
expected revision, created time, operation ID, kind, claim ID, and resource.

`complete`, `open`, and `legacy-incomplete` are projection labels, not
provider state. The current snapshot is not an active-state assertion. This
history is retention-bounded local diagnostic state: migration-era nulls and
`gc --apply` removal are not recoverable, and it is not append-only audit or
provider history.

For `status --verbose`, bundle claims use ordered `RESOURCES`, including the
JSON claim's `resources` array. Unknown operations include started bundle
operations such as `exec-bundle`. A missing release emits `RELEASE <none>`.
Field labels use upper snake case.

Successful `acquire`, `heartbeat`, `checkpoint`, and `transfer` may include
`TOKEN` for the next lifecycle step. Other mutation output and all failures
omit bearer tokens. With `--lease-file`, the token is written to the handle
instead. Guarded child results append a `COMMAND` block in this order when
present:

```text
RETURNCODE
EXECUTION_DIRECTORY
STDOUT_BYTES
STDOUT_TRUNCATED
STDERR_BYTES
STDERR_TRUNCATED
STDOUT
STDERR
```

`STDOUT` and `STDERR` are escaped scalar values, not raw streams.

## Development

Use the locked Python 3.14 toolchain:

```sh
mise run sync
mise run lint
mise run format-check
mise run test
mise run typecheck
mise run build
```
