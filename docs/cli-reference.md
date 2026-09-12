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
| `-V` | `--verbose` | singleton and bundle status commands |
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

`worklease events` is a read-only, newest-first projection of retained lifecycle rows
across all resources. It includes one redacted record for each retained epoch (including
bundle epochs), operation, reconciliation, and termination row. Use `--limit 1..1000`
(default 100) and follow the opaque `--cursor`/`nextCursor` keyset chain; the cursor is
not tied to the page size. Ordering is by descending `at`, then source order
`termination`, `reconciliation`, `operation`, `epoch`, then normalized resource key,
claim ID, operation ID, and kind. `--full` preserves complete resources and identifiers
in text output. The feed is retention-bounded: rows collected by `gc --apply` between
pages disappear, and there is no cross-invocation snapshot. Newer rows recorded after a
page are excluded from its continuation; explicitly backdated rows older than the cursor
may appear.

`history --resource R` is a read-only chronological summary of the retained
local epochs for exactly `R`. It includes singleton epochs and bundle epochs
containing `R`, plus safe operation and reconciliation summaries. Default text
shows one concise resource label, a coverage sentence, and one acquisition row
per epoch with agent, work key, kind, completeness, and operation and
reconciliation counts. Nested operation and reconciliation rows use labeled
fields. A `CURRENT_SNAPSHOT` row is explicitly retained evidence, not proof of
an active lease. Use `--full` for the complete redacted diagnostic field dump;
`--json` is unchanged and always complete. Each acquisition epoch has
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
expiry does not make the epoch active or complete. The command does not provide provider lookups or time filters. Use `events` for
all-resource output and keyset pagination.

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

JSON reports deterministic counts, oldest eligible timestamp, and newest
eligible timestamp for each category:

| Category |
| --- |
| expired singleton claims |
| expired bundle claims |
| epochs |
| bundle epochs |
| operations |
| releases |
| reconciliations |
| resource metadata |

Records strictly older than the captured cutoff are eligible. For current
claims, age is measured from `expiresAt`; an expiry exactly at the cutoff is
retained. Text output states whether the run changed anything, shows the
retention window, exact cutoff, and total eligible or collected count, and lists
only nonzero groups with readable labels and compact oldest/newest ages such as
`62d ago`. When a dry run finds eligible records, it prints a copyable `HINT`
that reuses the exact captured cutoff with `--apply`; empty dry runs omit the
hint. A successful apply reports either the collected total or an explicit
`0 records (no changes)`. JSON output remains unchanged and retains every group
and absolute timestamp.

GC always protects active claims, expired claims inside the retention window,
and claims with unresolved started operations. The `protected` JSON object and
text `PROTECTED` block report old expired singleton or bundle claims retained
because of those unknown outcomes. Reconcile the operation before collecting
them; GC never guesses whether external work succeeded.

Applying GC uses one immediate SQLite transaction. Eligible expired bundles are
retired as whole ownership units. Collection records an `expired` termination
at the maintenance transaction time, removes the current claim projection so it
no longer appears in `list`, and intentionally ends checkpoint recovery from
that abandoned claim. The newly recorded termination and its epoch remain for a
fresh retention window. Interruption leaves either the state from before
collection or the committed state after collection. Resource revision
tombstones preserve monotonic revisions after old metadata is removed.

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
| `policy list` | Fixed-width summary columns `NAME`, `SCOPE`, `CAPABILITY`, `EXECUTION`, `FENCING`, then rows; `--full` adds package provenance and policy contract versions |
| `policy describe` | One `FIELD: value` line per policy field |
| `list` | Fixed-width summary columns `STATE`, `RESOURCE`, `LEASE`, then rows; `--full` adds lifecycle identifiers and absolute expiry |
| `history` | `OK history`, one concise `RESOURCE`, a retention-bounded local `COVERAGE` summary, `EPOCHS`, then chronological acquisition rows with labeled operation, reconciliation, termination, or current-snapshot details; `--full` restores all redacted diagnostic fields |
| `events` | `OK events`, `EVENTS`, retained `EVENT` blocks, and `NEXT_CURSOR` plus a continuation `HINT` only when another page exists |
| `status`, `status-bundle`, `bundle-status`, `inspect-bundle` | `OK`, one compact `RESOURCE` or ordered `RESOURCES` value, `STATE`, then `AGENT_ID`, `WORK_KEY`, relative `LEASE`, and `REVISION` for a claim |
| Status commands with `--verbose` | Resource and state, full redacted `CLAIM`, `UNKNOWN_OPERATIONS`, `RELEASE`, and optional `GUIDANCE` |
| `inspect-operation`, `inspect-operation-bundle` | `OK`, identity, kind, state, outcome, hashes, and reconciliation timestamps when present |
| `gc` | `OK gc`; `DRY_RUN` plus `RETENTION`, `CUTOFF`, and total `ELIGIBLE`, or total `COLLECTED`; nonzero readable group rows with compact oldest/newest ages; an exact-cutoff apply `HINT` when useful; then nonzero `PROTECTED` rows naming unresolved operations |
| Claim mutations and guarded commands | `OK`, operation and mutation fields, then `CLAIM` with resource(s), `CLAIM_ID`, `AGENT_ID`, `SESSION_ID`, `OWNER_ID`, `WORK_KEY`, revision, expiry, and guarantee |

`list` uses a fixed-width, space-padded summary table. Widths follow terminal
columns: East Asian wide characters count as two, and combining marks count as
zero. Git-backed resource keys are summarized as provider, repository, and
item (for example, `backlog-md:worklease:TASK-68`). Other long resources keep
recognizable boundaries or a prefix and suffix around an ellipsis within 52
columns. Active lease values use approximate durations such as `1h 2m left`;
expired values use elapsed durations such as `3m ago`.

`worklease list --full` shows `STATE`, `RESOURCE`, `CLAIM_ID`, `OWNER_ID`, and
`EXPIRES_AT` with complete values. JSON also remains complete. The default
`policy list` table uses concise `yes`/`no` fencing values and fits every built-in
policy within 80 terminal columns. `worklease policy list --full` restores
`ORIGIN`, `ORIGIN_VERSION`, `CONTRACT_VERSION`, `KEY_POLICY_VERSION`, and the
complete existing field set. Policy-list JSON remains complete with or without
`--full`; an empty policy list emits the selected header only. Default status
output uses the same compact resource and relative lease conventions as `list`;
an unclaimed status ends after `STATE free` without an empty claim block.
`--verbose` restores complete redacted lifecycle identifiers, timestamps, unknown
operations, release data, and guidance for singleton and bundle status commands.
Tokens are never listed.

`history` emits `OK history`, one concise `RESOURCE`, a `COVERAGE` sentence
that counts complete, open, and legacy-incomplete epochs while stating that the
view is retention-bounded local history rather than a provider audit, and
`EPOCHS`. Each chronological `EPOCH` row labels acquired time, agent, work key,
kind, per-epoch completeness, and operation and reconciliation counts. An
indented termination row keeps its distinct reason so migration gaps remain
visible even for terminated epochs. Operation, reconciliation, and current-snapshot
rows use labeled fields. Epochs sort by acquisition revision, with acquired
time and stable IDs only for legacy fallback; operations and reconciliations
retain their deterministic JSON order.

`history --full` preserves every redacted diagnostic field previously shown by
default: source provenance, resource membership, lifecycle identifiers,
revisions, completeness, operation and reconciliation identities, all retained
termination fields and timestamps, and current snapshots. JSON output is
schema-compatible and complete with or without `--full`.

`complete`, `open`, and `legacy-incomplete` are projection labels, not
provider state. A `CURRENT_SNAPSHOT` line says explicitly that retained state
is not proof of an active lease. This history is retention-bounded local
diagnostic state: migration-era nulls and `gc --apply` removal are not
recoverable, and it is not append-only audit or provider history.

For status commands with `--verbose`, bundle claims use ordered `RESOURCES`,
including the JSON claim's `resources` array. Unknown operations include started
bundle operations such as `exec-bundle`. A missing release emits `RELEASE <none>`.
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
