# worklease

`worklease` coordinates work between humans and agents on one host. A caller maps work to an opaque resource key, acquires a lease, completes the work, records a checkpoint, and releases the lease. Each lease includes a claim ID, token, revision, expiry, heartbeat, and idempotent operation receipts.

The caller's backlog or work system stays authoritative. Worklease provides local coordination state and guarded local operations.

## Install

### Local

Requires Python 3.14 or newer.

Clone the repository, then run:

```sh
uv tool install .
worklease --version
```

### Tagged releases

Tagged releases publish these exact asset names:

```text
worklease-X.Y.Z-py3-none-any.whl
worklease-X.Y.Z.tar.gz
worklease-vX.Y.Z-linux-x86_64
worklease-vX.Y.Z-linux-arm64
worklease-vX.Y.Z-macos-x86_64
worklease-vX.Y.Z-macos-arm64
checksums.txt
```

Native executables are one-file PyInstaller builds. They retain package metadata, so `worklease --version` reports the tagged version. `checksums.txt` covers every distributable asset.

### mise

Install the native release for the current platform through mise:

```sh
mise use --global 'github:brettinternet/worklease[matching=worklease,bin=worklease]'
worklease --version
```

mise selects `worklease-vX.Y.Z-{linux,macos}-{x86_64,arm64}` for the current platform and exposes it as `worklease`. Append `@vX.Y.Z` to pin a release:

For example:

```sh
mise use --global 'github:brettinternet/worklease[matching=worklease,bin=worklease]@vX.Y.Z'
```

Or install an exact release with the reproducible task:

```sh
WORKLEASE_REPOSITORY=owner/name mise run install-release VERSION=vX.Y.Z
```

The task requires a `vX.Y.Z` tag, verifies the SHA-256 manifest and `--version`, then installs to `~/.local/bin`. Set `WORKLEASE_INSTALL_DIR` to change the destination. If no native asset matches, it installs the verified `py3-none-any` wheel through `uv`. Release tests can set `WORKLEASE_RELEASE_BASE_URL` to avoid live GitHub access.

## Usage

Acquire a lease with the Python API. Use its `CLAIM_ID`, `TOKEN`, and `REVISION` for guarded operations:

```sh
export WORKLEASE_HOME=.worklease

worklease exec \
  --resource "repo:tests" \
  --claim-id "$CLAIM_ID" \
  --token "$TOKEN" \
  --revision "$REVISION" \
  --operation-id "run-tests-001" \
  -- python -m unittest discover -s tests -v
```

Replace a file only when its current SHA-256 matches the expected value:

```sh
worklease replace-file \
  --resource "repo:src/app.py" \
  --claim-id "$CLAIM_ID" \
  --token "$TOKEN" \
  --revision "$REVISION" \
  --operation-id "update-app-001" \
  --path src/app.py \
  --expected-sha256 "$CURRENT_SHA256" \
  --content-file /tmp/app.py
```

`exec` runs argv without a shell. `replace-file` writes atomically and preserves file mode.

## CLI contract

Commands emit compact JSON with `schemaVersion: 1`, `operation`, and `ok`. Failures also include a stable `error`. Use `--format text` for human-readable output. Put global options (`--format`, `--home`) before the command or before its command-specific arguments.

The command set is:

```text
worklease key --provider PROVIDER --source SOURCE --item ITEM
worklease acquire --resource RESOURCE --claim-id ID --agent-id ID \
  --session-id ID --owner-id ID --work-key WORK_KEY [--ttl SECONDS]
worklease status --resource RESOURCE
worklease list [--resource RESOURCE]
worklease heartbeat --resource RESOURCE --claim-id ID --token TOKEN \
  --revision REVISION --operation-id OPERATION_ID [--ttl SECONDS]
worklease release --resource RESOURCE --claim-id ID --token TOKEN \
  --revision REVISION --operation-id OPERATION_ID --reason REASON
worklease exec --resource RESOURCE --claim-id ID --token TOKEN \
  --revision REVISION --operation-id OPERATION_ID -- COMMAND ARG...
worklease replace-file --resource RESOURCE --claim-id ID --token TOKEN \
  --revision REVISION --operation-id OPERATION_ID --path PATH \
  --expected-sha256 SHA256 --content-file CONTENT_FILE
worklease checkpoint --resource RESOURCE --claim-id ID --token TOKEN \
  --revision REVISION --operation-id OPERATION_ID --checkpoint JSON \
  [--ttl SECONDS]
```

`status` and `list` never return bearer tokens. Tokens cannot be recovered. `acquire`, `heartbeat`, and successful `checkpoint` return the token once in their mutation response; keep it secret and out of logs. `list --format text` prints state, resource, claim, owner, and expiry columns.

### Checkpoint contract

`checkpoint` accepts exactly one JSON value. The Python API canonicalizes it
with sorted object keys, compact separators, and `allow_nan=False`, then
measures the UTF-8 bytes. The serialized checkpoint is limited to 8 KiB
(`MAX_CHECKPOINT_BYTES = 8192`); non-JSON values and larger payloads return
`invalid-checkpoint` or `checkpoint-too-large` without changing the claim.

On success, the JSON response has `schemaVersion: 1`, `operation: "checkpoint"`,
`checkpoint`, `checkpointBytes`, `operationId`, and the renewed claim with its
incremented `revision`, `heartbeatAt`, and `expiresAt`; the mutation response
includes the bearer token once. Retrying the same operation ID with the same
request returns the cached receipt, even after the lease expires; a changed
request under the same operation ID, or a new/uncached request with
stale/expired ownership, is rejected.

The Python API is `LeaseStore.checkpoint(MutationRequest(...), value)`. It
atomically writes the checkpoint, advances the revision, and renews the lease.
The latest value remains on the active claim. A clean `release` copies it to
release history; a later `acquire` reports `recovery: "clean-handoff"` and the
checkpoint. If the claim expires first, the next acquire reports
`recovery: "expired-recovery"` with the last checkpoint. A first acquire has
no recovery marker. These records are local coordination metadata only; the
backlog/provider remains authoritative for real progress, and the feature
does not provide provider-side fencing, cross-host exclusion, or exactly-once
external effects. Checkpoint retention is not lease-TTL-limited: the latest
value survives clean release and expiry recovery until a future explicit
retention or garbage-collection operation removes it.

Exit codes: `0` success, `2` lease or capability conflict, `3` idempotency or version/request mismatch, and `64` invalid input. `exec` returns the child status after the child starts.

## Backlog.md example

Map a [Backlog.md](https://github.com/MrLesk/Backlog.md) task to one resource:

```python
from worklease.adapters import key
from worklease.models import AcquireRequest
from worklease.store import LeaseStore

task_id = "TASK-42"
project_path = "docs/backlog"
resource = key("backlog-md", project_path, task_id).resource
lease = LeaseStore(".worklease").acquire(
    AcquireRequest(
        resource=resource,
        claim_id="claim-42",
        agent_id="agent-42",
        session_id="session-42",
        owner_id="owner-42",
        work_key=f"implement:{task_id}",
    )
)
claim = lease["claim"]
# Export resource, claim["claimId"], claim["token"], and claim["revision"]
# for the matching worklease exec or release command.
```

Read and update the authoritative task through Backlog.md:

```sh
backlog task view TASK-42 --plain
backlog task edit TASK-42 --status "In Progress" --plain
```

Run a task command under the same resource and claim:

```sh
export RESOURCE="$(worklease key --provider backlog-md \
  --source docs/backlog --item TASK-42 | python -c \
  'import json,sys; print(json.load(sys.stdin)["resource"])')"
worklease exec \
  --resource "$RESOURCE" \
  --claim-id "$CLAIM_ID" \
  --token "$TOKEN" \
  --revision "$REVISION" \
  --operation-id "backlog-task-edit-42" \
  -- backlog task edit TASK-42 --status "In Progress" --plain
```

`--` separates `worklease exec` options from the child command.

Run work under the claim, save a durable checkpoint, then release the exact claim.

## Adapter boundary

Bundled adapters provide deterministic identities. GitHub and Backlog.md derive helper-fenced item keys. Markdown derives one helper-fenced source key for all items. Linear and unknown providers derive `local-coordination` keys. Adapters do not write to providers or claim provider-side fencing. Generic `exec` provides only local coordination.

Markdown updates use `replace-file`: expected SHA-256, symlink rejection, mode preservation, and atomic fsync/rename. The Markdown adapter rejects coordination-only claims first.

```python
from worklease.adapters import key_result

key_result("markdown", "docs/backlog.md", "ITEM-42")
# {"capability": "source-claim", "scope": "source", ...}
```

Provider-side execution requires an adapter with a real conditional-write check. A same-host claim never implies that guarantee.

## Guarantees

The built-in store coordinates cooperating callers on one host through SQLite and POSIX file locks. It does not provide distributed locking, cross-host exclusion, or provider-side fencing. For stronger guarantees, use the provider's conditional mutation authority. The provider remains authoritative for discovery, status, progress, review, and completion.

## Agent workflow

First read the [Worklease workflow skill](skills/worklease-workflow/SKILL.md). To connect a backlog, issue system, document, or custom source, then read the [source workflow skill](skills/worklease-source-workflow/SKILL.md).

- Let the caller or adapter discover scope, dependencies, and one opaque claim resource.
- Use `LeaseStore` to inspect, acquire, heartbeat, and release same-host claims.
- Use `worklease exec` or `worklease replace-file` only for the operation being guarded.
- Treat provider mutations as local coordination unless the provider supplies conditional-write fencing.
- Verify a durable provider checkpoint before releasing the claim.

The skills and key adapters do not select providers, schedule work, discover provider items, write to providers, or claim provider-side fencing. The caller selects the provider, source, and item. Its backlog remains authoritative.

### Connect your work source

1. Define source resolution and source-qualified `Source`, `WorkRef`, and `WorkItem` mappings.
2. Use a bundled key policy or document one stable resource policy for the claim scope.
3. Implement authorized reads, writes, durable receipts, review boundaries, and archive behavior. Return `capability` for unsupported operations.
4. Keep graph construction and selection in `worklease-workflow`.
5. Default `providerMutationFenced` to `false`. Set it to `true` only when provider writes atomically reject stale writers and return evidence.
6. Validate against the [provider contract](skills/worklease-source-workflow/references/provider-contract.md), [authoring checklist](skills/worklease-source-workflow/references/provider-authoring-checklist.md), and [guarantee examples](skills/worklease-source-workflow/examples/index.md).

See [provider references](skills/worklease-source-workflow/references/providers/index.md) for Backlog.md, Markdown, GitHub Issues, Linear, Jira, and unknown systems.

## Development

Use the repository's Python 3.14 and locked toolchain:

```sh
uv sync --locked
uv run worklease --version
uv run python -m unittest discover -s tests -v
mise exec -- pyright src/worklease tests
uv build
```

Equivalent mise tasks:

```sh
mise run sync
mise run cli -- --version
mise run test
mise run typecheck
mise run build
```

Install the local package as a CLI tool:

```sh
uv tool install .
worklease --version
```
