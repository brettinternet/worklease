# worklease

`worklease` coordinates work between humans and agents on one host. A caller maps work to an opaque resource key, acquires a lease, performs guarded work, verifies the caller's durable checkpoint, and releases the lease. Each ownership epoch has a claim ID, bearer token, revision, expiry, heartbeat, and idempotent operation receipts.

The caller's backlog or work system stays authoritative. Worklease stores local coordination state and guards local operations; it does not write provider state or turn a same-host lease into provider-side fencing.

## Install

### Local

Requires Python 3.14 or newer.

```sh
uv tool install .
worklease --version
```

### Tagged releases

See [GitHub Releases](https://github.com/brettinternet/worklease/releases) for tagged builds and checksums.

### mise

Install the native release for the current platform through mise:

```sh
mise use --global 'github:brettinternet/worklease[matching=worklease,bin=worklease]'
worklease --version
```

Pin a release by appending `@vX.Y.Z`, or install an exact release with:

```sh
WORKLEASE_REPOSITORY=owner/name mise run install-release VERSION=vX.Y.Z
```

The release task verifies the SHA-256 manifest and `--version` before installing. Set `WORKLEASE_INSTALL_DIR` to change the destination. If no native asset matches, it installs the verified `py3-none-any` wheel through `uv`.

## Usage

The CLI emits compact JSON with `schemaVersion: 1`, `operation`, and `ok`. Use `--format text` for human-readable output. Global options (`--format` and `--home`) go before the command or before its command-specific arguments.

### 1. Derive one exact resource

The caller chooses the provider, source, and item. Every contender for the same logical work must derive the same resource string:

```sh
RESOURCE_JSON="$(worklease key \
  --provider backlog-md \
  --source docs/backlog \
  --item TASK-42)"
RESOURCE="$(printf '%s\n' "$RESOURCE_JSON" | python -c \
  'import json, sys; print(json.load(sys.stdin)["resource"])')"
printf 'resource=%s\n' "$RESOURCE"
```

The key response also reports `capability`, `scope`, `fencedMutations`, `providerFencing`, and `genericExecutionGuarantee`. These describe the selected local policy; `providerFencing` is `false` unless the provider mutation itself supplies conditional-write evidence. Exact source and item identity are caller-owned and are never expanded by Worklease.

Use `--coordination-only` with `key` when a provider write will occur outside a Worklease-guarded local operation. The resulting claim coordinates cooperating local workers only.

### 2. Acquire an ownership epoch

Generate fresh, unique IDs for every new attempt. Keep the bearer token private; it is shown only in successful mutation responses and is not returned by read-only commands.

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

The successful response contains the claim's `claimId`, `token`, `revision`, `expiresAt`, `heartbeatAt`, `workKey`, and `guarantee`. Save the returned token and revision for the next mutation. An active claim is one ownership epoch, not an assignment or status marker. An acquire of an expired epoch creates a fresh claim ID and token and may include its prior coordination checkpoint as recovery metadata.

The default TTL is 900 seconds and the maximum is 3600 seconds. Heartbeat before half the lease elapses and around long operations. Never adopt or renew an unexpired claim merely because the agent or session identity is unchanged.

### 3. Inspect and keep the lease alive

Read-only status and list calls never expose bearer tokens:

```sh
worklease status --resource "$RESOURCE"
worklease list --resource "$RESOURCE"
worklease heartbeat \
  --resource "$RESOURCE" \
  --claim-id "$CLAIM_ID" \
  --token "$TOKEN" \
  --revision "$REVISION" \
  --operation-id "heartbeat-TASK-42-001" \
  --ttl 900
```

A successful heartbeat atomically renews the lease and advances the active `revision`; replace `REVISION` with the returned value. Stale, expired, wrong-token, and conflicting requests fail without changing ownership. Operation IDs are idempotency keys: replay the exact same request to recover a lost response, and never reuse one for changed inputs.

### 4. Run one guarded command

`exec` passes argv directly without a shell. It renews the claim while the child runs, records the child result, and advances the revision when the operation completes:

```sh
worklease exec \
  --resource "$RESOURCE" \
  --claim-id "$CLAIM_ID" \
  --token "$TOKEN" \
  --revision "$REVISION" \
  --operation-id "run-tests-TASK-42-001" \
  -- python -m unittest discover -s tests -v
```

Use the returned claim revision for the next operation. A non-zero child status is reported as `child-process-failed`; an uncertain start or storage outcome is reported as `unknown-outcome` and must be reconciled before retrying external work. A successful replay of the same operation ID and identical request returns the cached receipt without running the command again.

If `exec` reports `unknown-outcome`, do not replay the external command automatically. Inspect the durable local operation first:

```sh
worklease inspect-operation \
  --resource "$RESOURCE" \
  --operation-id "run-tests-TASK-42-001"
```

Inspection returns only the operation kind, expected revision, creation time, and a SHA-256 fingerprint of the persisted request. It reports `unknown-outcome`, `completed`, or `reconciled`; it never returns the request, receipt, evidence, or bearer token. A reconciled `observed-success` means the caller may continue from the provider-verified result. A reconciled `observed-failure` means the caller must stop or issue a new operation ID for an explicitly approved retry; it must never reuse the original operation ID to rerun the external command.

Only the current claimant may record an observed result. Supply the inspected fingerprint and bounded strict JSON evidence from the provider or other authoritative observer:

```sh
worklease reconcile-operation \
  --resource "$RESOURCE" \
  --claim-id "$CLAIM_ID" \
  --token "$TOKEN" \
  --revision "$REVISION" \
  --operation-id "reconcile-TASK-42-001" \
  --target-operation-id "run-tests-TASK-42-001" \
  --expected-request-sha256 "$REQUEST_SHA256" \
  --outcome observed-success \
  --evidence '{"providerReceipt":"receipt-123","observedAt":"2026-07-13T12:00:00Z"}'
```

The target operation remains immutable; reconciliation appends an audit record containing the resolver identity, outcome, fingerprint, evidence, and timestamps. Evidence is canonical JSON and limited to 8192 UTF-8 bytes. Replaying the exact reconciliation request returns its cached receipt; changed evidence, outcome, fingerprint, claim, or revision fails without another external side effect. A storage failure leaves the claim revision and unknown operation unchanged.

### 5. Replace a Markdown source by expected hash

`replace-file` is a fenced, atomic compare-and-swap for one source file. Derive a **source-wide** Markdown resource and acquire that resource before replacing the file; do not reuse an item-scoped resource:

```sh
MARKDOWN_KEY_JSON="$(worklease key \
  --provider markdown \
  --source docs/backlog.md \
  --item SOURCE)"
MARKDOWN_RESOURCE="$(printf '%s\n' "$MARKDOWN_KEY_JSON" | python -c \
  'import json, sys; print(json.load(sys.stdin)["resource"])')"
# Acquire MARKDOWN_RESOURCE using the lifecycle above and retain:
# MARKDOWN_CLAIM_ID, MARKDOWN_TOKEN, and MARKDOWN_REVISION.
```

Compute the expected SHA-256 from the current file, prepare replacement content separately, and use the returned revision afterward:

```sh
CURRENT_SHA256="$(shasum -a 256 docs/backlog.md | cut -d ' ' -f 1)"
worklease replace-file \
  --resource "$MARKDOWN_RESOURCE" \
  --claim-id "$MARKDOWN_CLAIM_ID" \
  --token "$MARKDOWN_TOKEN" \
  --revision "$MARKDOWN_REVISION" \
  --operation-id "replace-backlog-001" \
  --path docs/backlog.md \
  --expected-sha256 "$CURRENT_SHA256" \
  --content-file /tmp/backlog.md
```

The expected hash prevents an old source snapshot from being overwritten. Symlink targets are rejected, the file mode is preserved, and the replacement is atomic. This operation advances the active revision on success. A coordination-only claim cannot call `replace-file` because the source mutation requires the fenced claim path.

### 6. Record and verify checkpoints

Worklease has a `checkpoint` command, but its bounded JSON value is **coordination metadata**, not provider progress:

```sh
worklease checkpoint \
  --resource "$RESOURCE" \
  --claim-id "$CLAIM_ID" \
  --token "$TOKEN" \
  --revision "$REVISION" \
  --operation-id "checkpoint-TASK-42-001" \
  --checkpoint '{"phase":"tests","case":42}' \
  --ttl 900
```

Checkpoint JSON is canonicalized with sorted keys and compact separators, rejects NaN and non-JSON values, and is limited to 8192 UTF-8 bytes. A successful write renews the lease, advances the revision, and returns the token once. The latest value survives clean release and expiry recovery until an explicit retention or garbage-collection operation removes it. Read-only status/list output redacts it as appropriate and never returns the bearer token.

A Worklease operation receipt or coordination checkpoint is not an authoritative provider checkpoint. The caller must update or verify its backlog/provider record separately, retain the provider receipt or reread the source, and confirm the expected version/state before release. Worklease does not provide a provider-durable checkpoint command, exactly-once external effects, or cross-host fencing.

### 7. Release the exact current epoch

Release only after the caller's durable provider checkpoint has been verified. Use the latest active revision, including any advances from heartbeat, checkpoint, `exec`, or `replace-file`:

```sh
worklease release \
  --resource "$RESOURCE" \
  --claim-id "$CLAIM_ID" \
  --token "$TOKEN" \
  --revision "$REVISION" \
  --operation-id "release-TASK-42-001" \
  --reason "provider checkpoint verified"
```

Release validates and consumes the latest revision; it does not advance the revision. Its receipt records the released claim and reason. A missing, stale, or ambiguous provider checkpoint is not fixed by releasing: stop, reconcile the source, and retain the claim until ownership is known. A failed or uncertain release must not be retried with changed inputs; reuse the same operation ID only to recover that exact request.

## Recipes

### Singleton command

Derive one stable local resource (for example, `local:formatter`), acquire it with a unique `work-key`, run exactly one argv through `exec`, then release the returned current revision. Do not use a shell string; pass executable and arguments after `--`.

### Scarce local resource

Use one shared exact resource for the scarce object, such as `local:port:8080` or `local:gpu:0`. All cooperating callers must use the same identity. If acquire reports an active claim, inspect its non-secret owner and expiry or wait for expiry; do not steal it and do not select a dependent task merely to avoid waiting.

### Local-agent item ownership

For a local backlog item, derive the item resource and use the item ID in the work key:

```sh
worklease key --provider backlog-md --source docs/backlog --item TASK-42
backlog task view TASK-42 --plain
backlog task edit TASK-42 -s 'In Progress' -a @agent --append-notes 'claimed under Worklease'
```

Run the authoritative edit through the same fenced claim authority when the provider path supports it. Assignment and status are visibility only; neither is a claim. Verify the task's resulting provider state before release.

### Source-wide Markdown replacement

The Markdown adapter uses one source claim for every item in a file. Derive it from the source path (the item argument is still required by the generic CLI), acquire it once, then use `replace-file` with the source's current expected hash:

```sh
worklease key --provider markdown --source docs/backlog.md --item SOURCE
```

Do not use a coordination-only key for `replace-file`; its compare-and-swap requires a fenced source claim.

### Idempotent replay

Persist each operation ID with its exact request. If the response is lost, retry the same command with the same resource, claim ID, token, revision, operation ID, TTL, and arguments. The cached receipt is returned and the guarded command is not repeated. A changed request under the same operation ID returns `operation-id-request-mismatch`.

### Locally coordinated remote-provider write

When a provider has no conditional-write fence, derive and acquire with `--coordination-only`. Before each direct provider API/CLI mutation, validate the exact local claim, provider eligibility, and provider version; perform the provider write; then reread both the claim and provider state. Report this pass as `local-coordination`, retain the provider receipt, and stop on claim loss, assignee conflict, provider change, failure, or ambiguity. Do not run an unfenced remote write through `exec` and do not describe local coordination as provider-side fencing.

## CLI contract

The singleton commands used in the lifecycle are:

```text
worklease key --provider PROVIDER --source SOURCE --item ITEM [--coordination-only]
worklease acquire --resource RESOURCE --claim-id ID --agent-id ID \
  --session-id ID --owner-id ID --work-key WORK_KEY [--ttl SECONDS]
worklease status --resource RESOURCE
worklease list [--resource RESOURCE]
worklease inspect-operation --resource RESOURCE --operation-id OPERATION_ID
worklease reconcile-operation --resource RESOURCE --claim-id ID --token TOKEN \
  --revision REVISION --operation-id OPERATION_ID \
  --target-operation-id TARGET_OPERATION_ID \
  --expected-request-sha256 SHA256 \
  --outcome observed-success|observed-failure --evidence JSON
worklease heartbeat --resource RESOURCE --claim-id ID --token TOKEN \
  --revision REVISION --operation-id OPERATION_ID [--ttl SECONDS]
worklease checkpoint --resource RESOURCE --claim-id ID --token TOKEN \
  --revision REVISION --operation-id OPERATION_ID --checkpoint JSON [--ttl SECONDS]
worklease exec --resource RESOURCE --claim-id ID --token TOKEN \
  --revision REVISION --operation-id OPERATION_ID -- COMMAND ARG...
worklease replace-file --resource RESOURCE --claim-id ID --token TOKEN \
  --revision REVISION --operation-id OPERATION_ID --path PATH \
  --expected-sha256 SHA256 --content-file CONTENT_FILE
worklease release --resource RESOURCE --claim-id ID --token TOKEN \
  --revision REVISION --operation-id OPERATION_ID --reason REASON
```

Exit codes are `0` for success, `2` for lease or capability conflicts, `3` for idempotency/version or unknown-outcome failures, `64` for invalid input, and `75` for storage failure. `exec` returns the child status after the child starts.

The state directory is selected by `--home`, then `WORKLEASE_HOME`, then `XDG_STATE_HOME/worklease`, defaulting to `~/.local/state/worklease`. Keep it private and use a separate home for isolated tests or independent coordination domains.

## Bundle claims

Use a bundle when one operation must own several exact resources together:

```sh
worklease acquire-bundle \
  --resource "repo:file-a" --resource "repo:file-b" \
  --claim-id "bundle-42" --agent-id "agent-42" \
  --session-id "session-42" --owner-id "owner-42" \
  --work-key "implement:bundle-42"
```

Bundles contain 1–32 non-empty opaque resources. Exact duplicates are rejected. The
receipt preserves the caller's resource order; locking uses a deterministic internal
order so overlapping bundles cannot deadlock. Acquisition, including expiry reclaim,
is all-or-nothing: a conflict or storage failure leaves no partial member claims.

Carry the one bundle `claimId`, `token`, and `revision` into `heartbeat-bundle`,
`exec-bundle`, and `release-bundle`, repeating the complete resource list in its
original order. These operations advance or consume one shared revision for every
member; a single-resource lifecycle command cannot mutate a bundle member. Status
is read-only and redacts the shared token. Reuse the same operation ID and request
for an idempotent replay; changed requests, stale revisions, and stale owners fail
without changing the bundle.

Bundle claims use the same same-host coordination boundary as single-resource
claims. They exclude cooperating local callers only; they do not provide
distributed locking, cross-host exclusion, or provider-side fencing. Provider
discovery, writes, receipts, and durable checkpoints remain caller/provider-owned.

## Resource-policy and source-provider boundary

Bundled resource policies derive deterministic local identities and declare
claim scope and capability. Backlog.md and GitHub use helper-fenced item keys;
Markdown uses one helper-fenced source key; Linear and explicitly selected
`generic` use local coordination. An unknown provider name fails with
`resource-policy-not-found`; callers must opt into `generic` when they
intentionally need a coordination-only custom identity:

```sh
worklease key --provider generic --source ACCOUNT --item WORK
```

Resource policies do not discover work, authenticate, execute provider writes,
or prove provider-side fencing. A source-provider adapter owns resolution,
complete discovery, authoritative reads and writes, receipts, review
boundaries, and archive behavior. It may compose a resource policy, but the
provider-neutral source workflow still owns dependency scheduling and claim
lifecycle.

Installed Python distributions can register policies through the
`worklease.resource_policies` entry-point group. The contract version is 1;
each registration declares its policy name, origin, key-policy version, claim
scope, capability, generic execution guarantee, and provider-fencing support.
Wheel and editable installs discover external registrations lazily when the
selected policy is used. Frozen standalone executables expose built-in
policies only and do not discover entry points from the build environment.

Inspect the available registrations without loading policy implementations:

```sh
worklease policy list
worklease policy describe --name generic
```

`key` is a policy decision, not a provider mutation. `exec` and
`replace-file` guard local operations only. A provider mutation is
provider-fenced only when the provider itself atomically rejects stale writers
and returns evidence; a Worklease local claim alone never implies that
guarantee.

The Worklease source workflow defines the provider-neutral boundary. See the
[source provider contract](skills/worklease-source-workflow/references/provider-contract.md)
and [provider references](skills/worklease-source-workflow/references/providers/index.md)
for source-specific mapping rules.

## Guarantees

The built-in store coordinates cooperating callers on one host through SQLite and POSIX file locks. It does not provide distributed locking, cross-host exclusion, or provider-side fencing. Claims are exact ownership epochs with bearer credentials and compare-and-set revisions. Tokens are accepted only by claim mutations and guarded operations; never put them in logs, comments, checkpoints, or handoffs.

The provider remains authoritative for discovery, status, dependencies, progress, review, completion, and durable checkpoints. Stop on ownership loss, provider-version conflict, missing receipts, or unknown outcomes. Never substitute an assignee, status, comment, branch, worktree, ordinary lock file, local cache, or command exit status for a claim or verified provider state.

## Agent workflow

Read the [Worklease workflow skill](skills/worklease-workflow/SKILL.md) before designing a loop. To connect a backlog, issue system, document, or custom source, then read the [source workflow skill](skills/worklease-source-workflow/SKILL.md).

The caller or adapter must:

1. Resolve sources and selectors in caller order.
2. Discover the complete dependency graph and select only ready, unblocked work.
3. Supply one exact resource and acquire a fresh ownership epoch before isolation or edits.
4. Revalidate dependencies, ownership, guarantee scope, and provider state before every durable mutation.
5. Verify the authoritative provider checkpoint before release, review, handoff, or archive.

The skills and key adapters do not select providers, schedule work, discover provider items, write provider progress, establish review boundaries, or claim provider-side fencing.

## Development

Use the repository's Python 3.14 and locked toolchain:

```sh
uv sync --locked
uv run worklease --version
uv run python -m unittest discover -s tests -v
pyright src/worklease tests
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
