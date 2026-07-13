# worklease

`worklease` coordinates generic work between humans and agents on one host. It is more than a lockfile. Each lease has a claim ID, token, revision, expiry, heartbeat, and idempotent operation receipts. It still works like a lockfile. A caller maps work to an opaque resource key, acquires it, does the work, records a checkpoint, and releases it.

The caller's backlog or work system stays authoritative. Worklease provides local coordination state and guarded local operations.

## Install

Requires Python 3.14 or newer.

```sh
uv tool install .
worklease --version
```

## Use

A caller first acquires a lease with the Python API. The claim receipt supplies `CLAIM_ID`, `TOKEN`, and `REVISION` for guarded operations.

```sh
export WORKLEASE_HOME=.worklease

worklease exec \
  --resource "repo:tests" \
  --claim-id "$CLAIM_ID" \
  --token "$TOKEN" \
  --revision "$REVISION" \
  --operation-id "run-tests-001" \
  -- python -m pytest tests
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

Both commands emit JSON by default with `schemaVersion: 1`, `operation`, and `ok`. `exec` runs argv without a shell. `replace-file` writes atomically and preserves file mode. Use `--format text --version` only for bare version output. Tokens are owner credentials and are omitted from read-only status and list responses.

Success is exit code 0. Lease and capability conflicts use 2. Idempotency and version conflicts use 3. Invalid input uses 64. `exec` returns the child status.

## Backlog.md example

A local adapter can map a Backlog.md task to one resource:

```python
from worklease.models import AcquireRequest
from worklease.store import LeaseStore

task_id = "TASK-42"
lease = LeaseStore(".worklease").acquire(
    AcquireRequest(
        resource=f"backlog.md:{task_id}",
        claim_id="claim-42",
        agent_id="agent-42",
        session_id="session-42",
        owner_id="owner-42",
        work_key=f"implement:{task_id}",
    )
)
claim = lease["claim"]
# Pass claim["claimId"], claim["token"], and claim["revision"] to worklease exec or release.
```

The caller reads and updates the authoritative task with Backlog.md:

```sh
backlog task view TASK-42 --plain
backlog task edit TASK-42 --status "In Progress" --plain
```

Run work under the claim, save a durable checkpoint, then release the exact claim. Backlog.md is at https://github.com/MrLesk/Backlog.md.

## Limitations

The built-in store uses SQLite and POSIX locks for cooperating callers on one host. It does not provide distributed locking, cross-host exclusion, or provider-side fencing. For distributed work, use a caller-provided authority that supplies those guarantees.

## Development

```sh
mise run sync
mise run test
mise run typecheck
mise run build
```

## License

MIT. See [LICENSE](LICENSE).
