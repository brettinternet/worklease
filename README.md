# worklease

`worklease` coordinates generic work between humans and agents on one host. It is more than a lockfile. Each lease has a claim ID, token, revision, expiry, heartbeat, and idempotent operation receipts. It still works like a lockfile. A caller maps work to an opaque resource key, acquires it, does the work, records a checkpoint, and releases it.

The caller's backlog or work system stays authoritative. Worklease provides local coordination state and guarded local operations.

## Install

### Local

Requires Python 3.14 or newer.

Clone repo and then:

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

The native executables are one-file PyInstaller builds. They retain the
`worklease` package metadata, so `worklease --version` reports the tagged
version. `checksums.txt` covers every wheel, sdist, and native executable.

### mise

Install the native release for the current platform through mise's GitHub
backend. The releases currently published by this project are under
`brettinternet/worklease`:

```sh
mise use --global 'github:brettinternet/worklease[matching=worklease,bin=worklease]'
worklease --version
```

`brettinternet/workrelease` does not currently resolve as a GitHub repository.
If the releases are moved there, replace only the repository in the command:

```sh
mise use --global 'github:brettinternet/workrelease[matching=worklease,bin=worklease]'
```

`matching=worklease` narrows mise to this project's release assets, and its
platform detection then selects the matching native asset for the operating
system and architecture: `worklease-vX.Y.Z-{linux,macos}-{x86_64,arm64}`.
`bin=worklease` exposes the downloaded executable under the `worklease` command
name. Pin an exact release by appending `@vX.Y.Z` to the backend specification.

For example:

```sh
mise use --global 'github:brettinternet/worklease[matching=worklease,bin=worklease]@vX.Y.Z'
```

The `worklease --version` output reports the installed release version.


Install an exact release with the reproducible mise task:

```sh
WORKLEASE_REPOSITORY=owner/name mise run install-release VERSION=vX.Y.Z
```

The installer requires an exact `vX.Y.Z` tag, verifies the selected asset
against that release's SHA-256 manifest, runs its `--version` smoke test, and
installs it into `~/.local/bin` (override with `WORKLEASE_INSTALL_DIR`). It
selects the matching Linux/macOS native asset when available and otherwise
downloads and verifies the exact `py3-none-any` wheel through `uv`. Local
release tests use `WORKLEASE_RELEASE_BASE_URL`; no live GitHub access is
needed to test selection, checksum rejection, fallback, or version smoke
behavior.

## Usage

A caller first acquires a lease with the Python API. The claim receipt supplies `CLAIM_ID`, `TOKEN`, and `REVISION` for guarded operations.

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

Both commands emit JSON by default with `schemaVersion: 1`, `operation`, and `ok`. `exec` runs argv without a shell. `replace-file` writes atomically and preserves file mode. Use `--format text --version` only for bare version output. Tokens are owner credentials and are omitted from read-only status and list responses.

Success is exit code 0. Lease and capability conflicts use 2. Idempotency and version conflicts use 3. Invalid input uses 64. `exec` returns the child status.

## CLI contract

Every command emits compact JSON by default. Successful responses contain
`schemaVersion: 1`, `operation`, and `ok: true`; failures retain the same
envelope with `ok: false` and a stable `error` value. Use `--format text`
explicitly when a human-readable form is needed. The global options
(`--format`, `--home`) may appear before a command, and the same options may
appear after a command before its command-specific arguments.

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
```

`status` and `list` are read-only and never return bearer tokens, including
when `--format text` is selected. There is no token-recovery option. Acquire
and heartbeat return the token once because the owner must supply it to
conditional operations; store it as a secret and do not put it in logs.
`list --format text` prints a tabular state/resource/claim/owner/expiry view.
`exec` returns the child process status when a child starts. Exit codes are
stable: `0` success, `2` lease or capability conflict, `3` idempotency or
version/request mismatch, `64` invalid CLI input, and the child status for
`exec`.

## Backlog.md example

A local adapter can map a [Backlog.md](https://github.com/MrLesk/Backlog.md) task to one resource:

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

The caller reads and updates the authoritative task with Backlog.md:

```sh
backlog task view TASK-42 --plain
backlog task edit TASK-42 --status "In Progress" --plain
```

The CLI version runs an authoritative task command under the same derived
resource and claim with `exec`:

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

The `--` marks the boundary between `worklease exec` options and the `backlog task` argv.

Run work under the claim, save a durable checkpoint, then release the exact claim.

## Adapter boundary

The bundled adapters are lazy, deterministic identity policies. GitHub and Backlog.md can derive helper-fenced item keys; Markdown derives one helper-fenced source key for every item; Linear and unknown providers derive `local-coordination` keys. None of these adapters performs provider writes or claims remote/provider-side fencing. Generic `exec` always reports and provides only local coordination.

Markdown source updates use the core atomic `replace-file` operation with an expected SHA-256, symlink rejection, mode preservation, and an atomic fsync/rename. A Markdown adapter rejects coordination-only claims before delegating to that operation.

```python
from worklease.adapters import key_result

key_result("markdown", "docs/backlog.md", "ITEM-42")
# {"capability": "source-claim", "scope": "source", ...}
```

Provider-side execution remains unavailable until a provider adapter supplies a real conditional-write check. A same-host helper claim is never upgraded into that stronger guarantee.

## Guarantees

The built-in store's only guarantee is same-host SQLite plus POSIX file-lock coordination for cooperating callers. It is not distributed locking, cross-host exclusion, or provider-side fencing.

## Agent workflow

Agents first read the [Worklease workflow skill](skills/worklease-workflow/SKILL.md). To connect a concrete backlog, issue system, document, or custom source, load the [Worklease source workflow skill](skills/worklease-source-workflow/SKILL.md) after it. The normative workflow tells agents to:

- let the caller or provider adapter discover the full scope, dependencies, and one exact opaque claim resource
- use `LeaseStore` for same-host claim inspection, acquisition, heartbeat, and release
- use `worklease exec` or `worklease replace-file` only for the local operation they actually guard
- treat a provider CLI/API mutation as local coordination unless the provider itself supplies conditional-write fencing
- verify a durable provider checkpoint before releasing the local claim

The skill does not choose providers or schedule work. After the caller selects a provider, source, and item, a bundled adapter may derive the deterministic local resource and capability; neither the skill nor those key adapters performs provider discovery, provider writes, or provider-side claims. The caller's backlog remains authoritative.

### Connect your work source

1. Define explicit source resolution and source-qualified `Source`, `WorkRef`, and `WorkItem` mappings.
2. Use a bundled `worklease.adapters` key policy, or document one exact stable resource policy for the claim scope.
3. Implement authorized provider reads, writes, durable receipts, review boundaries, and archive behavior; return `capability` for unsupported operations.
4. Keep graph construction and selection in `worklease-workflow`; do not duplicate them in the provider adapter.
5. Default `providerMutationFenced` to `false` unless the provider write itself atomically rejects stale writers and returns evidence.
6. Validate the adapter with the [provider contract](skills/worklease-source-workflow/references/provider-contract.md), [authoring checklist](skills/worklease-source-workflow/references/provider-authoring-checklist.md), and the [matching guarantee example](skills/worklease-source-workflow/examples/index.md).

Provider-specific mappings for Backlog.md, loose Markdown, GitHub Issues, Linear, Jira, and unknown systems are indexed in the [provider references](skills/worklease-source-workflow/references/providers/index.md).

## Limitations

The built-in store does not provide distributed locking, cross-host exclusion, or provider-side fencing. For stronger guarantees, the caller must use its provider's conditional mutation authority. The provider remains authoritative for discovery, status, progress, review, and completion.

## Development

Run the reproducible development environment with the repository's Python 3.14
and latest uv/pyright toolchain:

```sh
uv sync --locked
uv run worklease --version
uv run python -m unittest discover -s tests -v
mise exec -- pyright src/worklease tests
uv build
```

The mise tasks are equivalent convenience wrappers:

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
