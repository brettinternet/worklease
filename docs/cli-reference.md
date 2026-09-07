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
plus safe operation and reconciliation summaries. Acquisition is synthesized
from the epoch row, not reported as an operation. The command does not provide
all-resource output, provider lookups, pagination, or time filters. An open
row remains open when its stored expiry is past; a read does not reclaim it or
invent a termination. Legacy rows with missing acquisition or ending evidence
are marked `legacy-incomplete` and cannot be reconstructed from this view.

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

`gc` is a read-only dry run unless `--apply` is supplied. It uses a 30-day
retention window by default and reports deterministic counts plus oldest and
newest eligible timestamps for epochs, bundle epochs, operations, releases,
reconciliations, and resource metadata:

```sh
worklease gc
worklease gc --retention-days 90
worklease gc --cutoff 2026-01-01T00:00:00Z --apply
```

Records strictly older than the captured cutoff are eligible. Active claims,
expired-but-unreclaimed claims, current ownership, unresolved started
operations, and records inside the retention window are always protected.
Applying a collection uses one immediate SQLite transaction; interruption leaves
either the pre-collection or the committed post-collection state. Resource
revision tombstones preserve monotonic revisions after historical metadata is
removed.

Use an explicit cutoff for repeatable maintenance, and take the normal state
database backup before applying a destructive collection. Invalid cutoffs,
unsupported retention values, storage conflicts, and protected-record conflicts
fail without partial deletion. Garbage collection does not reconcile unknown
operations or provide verbose diagnostics; use the dedicated inspection and
diagnostics commands for those concerns.

## Human-readable text grammar

Text is the stable default display format for people. Automation must explicitly
request schema-versioned JSON with `--json` or `--format json`.

Text output is UTF-8, newline-delimited, and deterministic: fields appear in the
order documented below, and tab (`\t`) separates columns. Scalar values use
compact JSON-compatible escaping without spaces. Printable Unicode remains
readable; control characters, C1 characters, and DEL are escaped
(`\u0000` through `\u001f`, `\u007f` through `\u009f`) so values cannot create
lines or columns.

Successful operations normally begin with `OK <operation>`. Failures begin with
`ERROR <operation>: <reason>`, followed by only allowlisted diagnostic fields
(`RESOURCE`, `OPERATION_ID`, `TARGET_OPERATION_ID`, `PROVIDER`, `FIELD`,
`CLAIM_ID`, revision bounds, `STATE`, `GUARANTEE`, `PROVIDER_FENCING`,
`EXPECTED_REQUEST_SHA256`, or `AVAILABLE`). Parser failures use `ERROR
<command>: invalid-arguments` (or `ERROR parse: invalid-arguments` when no
command is identifiable), preserve the parser exit status, and may include a
safe `HINT` with a command-specific example or valid values. Hints never echo
rejected argument values.

The command grammars are:

- `version`: the version alone on success.
- `key`: `OK key`, then `PROVIDER`, `RESOURCE`, `SCOPE`, `CAPABILITY`,
  `GENERIC_EXECUTION_GUARANTEE`, `FENCED_MUTATIONS`, and `PROVIDER_FENCING`.
- `policy list`: one tab-separated header (`NAME`, `ORIGIN`, `ORIGIN_VERSION`,
  `CONTRACT_VERSION`, `KEY_POLICY_VERSION`, `SCOPE`, `CAPABILITY`,
  `GENERIC_EXECUTION_GUARANTEE`, `PROVIDER_FENCING_SUPPORTED`), followed by one
  row per policy. An empty list emits the header only.
- `policy describe`: one `FIELD: value` line for each policy field.
- `list`: a fixed-width, space-padded table with columns `STATE`, `RESOURCE`,
  `CLAIM_ID`, `OWNER_ID`, and `EXPIRES_AT`, followed by one `active` or
  `expired` row per claim. Column starts remain aligned across all rows, and
  widths are measured in terminal columns, so East Asian wide characters count
  as two and combining marks as zero. Default text output bounds `RESOURCE` to
  52 columns, `CLAIM_ID` to 18, `OWNER_ID` to 24, and `EXPIRES_AT` to 16. Long
  path-like resource values keep their leading component, a useful path
  component when space permits, and the longest suffix beginning at a
  separator. This keeps recognizable repository, source, and item boundaries;
  other long values keep a prefix and suffix around an ellipsis. Active expiry
  values are approximate relative durations such as `1h 2m`; expired values are
  labeled such as `expired 3m`. `worklease list --full` shows the complete
  resource, identifiers, and absolute expiry timestamps. `--json` and
  `--format json` always preserve the complete underlying values. Tokens are
  never listed.
- `history`: `OK history`, a `RESOURCE` line, an `EPOCHS` count, then one
  `EPOCH` block per retained epoch. Each block contains identity, acquired time,
  acquisition revision, `STATE`, and `LEGACY_INCOMPLETE` fields, followed by an
  `OPERATIONS` count and fixed columns
  (`OPERATION_ID`, `KIND`, `STATE`, `EXPECTED_REVISION`, `CREATED_AT`,
  `OUTCOME`, `RECONCILIATION_OPERATION_ID`, `RECONCILED_AT`) for each safe
  operation summary. `RECONCILIATIONS` rows contain target claim and operation
  IDs, reconciliation operation ID, kind, outcome, and recorded time. A
  `TERMINATION` block contains its reason and stored final snapshot, or
  `TERMINATION <none>`; a `CURRENT` block contains the stored current snapshot,
  or `CURRENT <none>`. Values use the same JSON-compatible escaping as other
  text output. Epochs sort by acquisition revision, with acquired time and
  stable IDs only for legacy rows; operations sort by expected revision,
  created time, operation ID, kind, claim ID, and resource.
- `status`, `status-bundle`, `bundle-status`, and `inspect-bundle`:
  `OK <operation>`, optional `RESOURCE` or `RESOURCES`, `STATE`, then a `CLAIM`
  block containing `RESOURCE`, `CLAIM_ID`, `AGENT_ID`, `SESSION_ID`,
  `OWNER_ID`, `WORK_KEY`, `REVISION`, `EXPIRES_AT`, and `GUARANTEE`; an
  unclaimed resource emits `CLAIM <none>`.
- `status --verbose`: resource and state lines, a full diagnostic `CLAIM` block
  without its token, `UNKNOWN_OPERATIONS` and `UNKNOWN` rows, a `RELEASE` block
  or `RELEASE <none>`, and optional `GUIDANCE`. For a bundle member, the claim
  block uses `RESOURCES` with the ordered bundle resources instead of
  `RESOURCE`, and unknown operations include started bundle operations such as
  `exec-bundle`. JSON output likewise uses the claim's `resources` array. Field
  labels use the same upper-snake convention as all other text renderers.
- `inspect-operation` and `inspect-operation-bundle`: `OK <operation>`, followed
  by singleton or ordered-bundle identity, kind, state, outcome, hashes, and
  reconciliation timestamps when present.
- `gc`: `OK gc`, retention fields, then an `ELIGIBLE` section with sorted
  record-type rows containing count, oldest, and newest timestamps.
- `acquire`, `acquire-bundle`, `bundle-acquire`, `heartbeat`, `checkpoint`,
  `heartbeat-bundle`, `bundle-heartbeat`, `transfer`, `release`,
  `release-bundle`, `bundle-release`, `exec`, `exec-bundle`, `bundle-exec`,
  `replace-file`, `reconcile-operation`, and `reconcile-operation-bundle`:
  `OK <operation>`, operation and mutation fields, then a `CLAIM` block. The
  block includes `CLAIM_ID`, `AGENT_ID`, `SESSION_ID`, `OWNER_ID`, and
  `WORK_KEY` along with the resource, revision, expiry, and guarantee. A
  successful acquire, heartbeat, checkpoint, or transfer may include `TOKEN`
  because the current or successor owner needs it for the next lifecycle step;
  other mutation and all failure output omit bearer tokens. When `--lease-file`
  is used the token is written to the handle instead of the output.

Guarded child results add a `COMMAND` block with `RETURNCODE`,
`EXECUTION_DIRECTORY`, `STDOUT_BYTES`, `STDOUT_TRUNCATED`, `STDERR_BYTES`,
`STDERR_TRUNCATED`, `STDOUT`, and `STDERR` in that order when those fields
exist. `STDOUT` and `STDERR` are values, not raw appended streams, so their
escaping rules are identical to every other scalar. Status and list output
expose no bearer tokens or secret claim material; mutation output exposes only
the minimum owner fields required to continue the lifecycle.

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
