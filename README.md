# worklease

`worklease` coordinates humans and agents working on the same host. A caller derives an opaque resource key, acquires a time-limited ownership epoch, performs guarded work, verifies the authoritative provider checkpoint, and releases the lease.

Worklease guards local operations. The caller's backlog or work system remains authoritative for discovery, dependencies, progress, review, and completion. A same-host lease is not distributed locking or provider-side fencing.

## Install

Requires Python 3.14 or newer for source installs.

```sh
uv tool install .
worklease --version
```

Tagged releases include checksummed Python packages and native executables. With [mise](https://mise.jdx.dev/), add this to `mise.toml`:

```toml
[tools]
"github:brettinternet/worklease" = "latest"
```

Then run `mise install`. Pin a version by replacing `latest` with `vX.Y.Z`.

### Install the agent skill

Ask your agent to follow [`skills/AGENTS.md`](skills/AGENTS.md). The portable
Agent Skills bundle is [`skills/worklease-workflow/`](skills/worklease-workflow/);
install that complete directory at the same Git tag as the CLI. The agent must
use its documented skill installer or discovery directory rather than assuming
a product-specific path. Skill installation does not install the CLI.

For example, tell your agent: “Read
`https://github.com/brettinternet/worklease/blob/vX.Y.Z/skills/AGENTS.md` and
install the Worklease skill for this agent.”

For a verified direct install, use:

```sh
WORKLEASE_REPOSITORY=brettinternet/worklease \
  mise run install-release VERSION=vX.Y.Z
```

This verifies the SHA-256 manifest and `--version`, preferring a native asset and falling back to the universal wheel through `uv`. Set `WORKLEASE_INSTALL_DIR` to change the destination. See [GitHub Releases](https://github.com/brettinternet/worklease/releases) for all assets.

## Core lifecycle

The CLI emits compact schema-versioned JSON. Add `--format text` for human-readable output; global options such as `--format` and `--home` must precede command-specific arguments.

### 1. Derive one exact resource

Every contender for the same logical work must use the same provider, source, and item identity:

```sh
worklease key \
  --provider backlog-md \
  --source docs/backlog \
  --item TASK-42
```

The response declares the claim scope and guarantee. Use `--coordination-only` when the provider write occurs outside a Worklease-guarded local operation. Never describe local coordination as provider-side fencing.

### 2. Acquire a fresh ownership epoch

Generate new claim, agent, session, and owner IDs for each attempt:

```sh
worklease acquire \
  --resource "$RESOURCE" \
  --claim-id "$CLAIM_ID" \
  --agent-id "$AGENT_ID" \
  --session-id "$SESSION_ID" \
  --owner-id "$OWNER_ID" \
  --work-key "implement:TASK-42" \
  --ttl 900
```

Save the returned token and revision. The token appears only in successful mutation responses. Prefer a mode-0600 `--token-file` or inherited `--token-fd`; direct `--token` is supported but exposes the bearer secret in argv. Never put a token in logs, comments, checkpoints, or handoffs.

Heartbeat before half the lease elapses and around long operations. Every successful mutation advances the revision; always use the newest returned value.

```sh
worklease heartbeat \
  --resource "$RESOURCE" \
  --claim-id "$CLAIM_ID" \
  --token-file "$TOKEN_FILE" \
  --revision "$REVISION" \
  --operation-id "heartbeat-TASK-42-001" \
  --ttl 900
```

### 3. Inspect or execute

Read-only commands never expose bearer tokens:

```sh
worklease status --resource "$RESOURCE"
worklease status --resource "$RESOURCE" --verbose
worklease list --resource "$RESOURCE"
```

Run guarded commands as argv, without a shell string:

```sh
worklease exec \
  --resource "$RESOURCE" \
  --claim-id "$CLAIM_ID" \
  --token-file "$TOKEN_FILE" \
  --revision "$REVISION" \
  --operation-id "test-TASK-42-001" \
  --git-primary \
  -- python -m unittest discover -s tests -v
```

Use `--provider-directory DIR` instead of `--git-primary` for an explicit checkout. Provider execution strips Git repository-routing variables while preserving identity, configuration, and credentials. The resolved directory is part of the idempotent request.

Operation IDs are idempotency keys. Replay only the identical request to recover a lost response; never reuse an ID for changed inputs. If an operation reports `unknown-outcome`, inspect it and verify the authoritative external result before recording reconciliation:

```sh
worklease inspect-operation \
  --resource "$RESOURCE" \
  --operation-id "test-TASK-42-001"
```

Do not automatically rerun an uncertain external command.

### 4. Checkpoint and release

`checkpoint` stores bounded coordination metadata; it is not provider progress:

```sh
worklease checkpoint \
  --resource "$RESOURCE" \
  --claim-id "$CLAIM_ID" \
  --token-file "$TOKEN_FILE" \
  --revision "$REVISION" \
  --operation-id "checkpoint-TASK-42-001" \
  --checkpoint '{"phase":"tests","result":"passed"}'
```

Before release, update or reread the authoritative provider and verify its expected version and state. Then release the exact current epoch with the newest revision:

```sh
worklease release \
  --resource "$RESOURCE" \
  --claim-id "$CLAIM_ID" \
  --token-file "$TOKEN_FILE" \
  --revision "$REVISION" \
  --operation-id "release-TASK-42-001" \
  --reason "provider checkpoint verified"
```

Stop on ownership loss, provider-version conflict, missing receipts, or unknown outcomes. An assignee, status, comment, branch, worktree, lock file, local cache, or command exit status is not a claim or a verified provider checkpoint.

## Common patterns

Singleton command: derive one stable local resource such as `local:formatter`, acquire it, run one argv through `exec`, and release the returned revision.

Scarce resource: use one shared identity such as `local:port:8080` or `local:gpu:0`. Wait for the current claim or its expiry; do not steal it.

Source-wide Markdown update: derive a `markdown` key for the file, acquire the source claim, and use `replace-file` with the current SHA-256. The expected hash and atomic replacement fence that one local file mutation. A coordination-only claim cannot call `replace-file`.

Multi-resource operation: use `acquire-bundle`, `heartbeat-bundle`, `exec-bundle`, and `release-bundle` for 1–32 exact resources. Bundle acquisition and revision changes are all-or-nothing, but retain the same same-host boundary as singleton claims.

Remote provider without conditional writes: acquire with `--coordination-only`; revalidate the claim, provider eligibility, and provider version before each direct API/CLI mutation; perform the write; then reread both claim and provider state. Retain the provider receipt and stop on conflict or ambiguity.

## Boundaries and agent contract

Built-in resource policies derive deterministic local identities and declare scope and capability. They do not discover work, authenticate to providers, schedule dependencies, write progress, establish review boundaries, or prove provider fencing. External Python distributions can add policies through the `worklease.resource_policies` entry-point group; frozen executables expose built-ins only.

```sh
worklease policy list
worklease policy describe --name generic
```

Agents should follow this sequence:

1. Resolve caller-selected sources and discover the complete dependency graph.
2. Select only ready, unblocked work.
3. Acquire one exact resource before isolation or edits.
4. Revalidate dependencies, ownership, guarantee scope, and provider state before every durable mutation.
5. Verify the authoritative provider checkpoint before release, review, handoff, or archive.

Read the [portable Worklease workflow skill](skills/worklease-workflow/SKILL.md)
before implementing an agent loop. It contains the provider-neutral contract
and source mappings for Backlog.md, loose Markdown, GitHub Issues, Linear,
Jira, and custom sources. The
[source-provider contract](skills/worklease-workflow/references/source-provider-contract.md)
and [SDK compatibility guide](docs/source-provider-sdk-compatibility.md) define
adapter and receipt requirements.

## CLI and compatibility

Run `worklease COMMAND --help` for the complete command surface. Singleton lifecycle commands include `key`, `acquire`, `status`, `list`, `heartbeat`, `checkpoint`, `exec`, `replace-file`, and `release`; uncertain-operation handling uses `inspect-operation` and `reconcile-operation`; multi-resource equivalents use the `*-bundle` commands.

Exit codes are `0` for success, `2` for lease or capability conflicts, `3` for idempotency/version or unknown-outcome failures, `64` for invalid input, and `75` for storage failure. `exec` returns the child status after the child starts.

State is selected by `--home`, then `WORKLEASE_HOME`, then `XDG_STATE_HOME/worklease`, defaulting to `~/.local/state/worklease`. Use an absolute, private path. Never use a repository-relative state path across linked worktrees because each checkout would create a separate lease authority.

The supported Python API is the symbol list in `worklease.__all__` and follows semantic versioning. JSON responses use schema version 1; consumers must ignore unknown fields and rely on stable `reason` values and exit codes rather than message text. Published schemas live in `worklease/schemas/v1/`. Every distribution includes those schemas and `worklease/py.typed`.

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
