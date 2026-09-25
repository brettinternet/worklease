# External adapter authoring

Use the [v1 protocol](../../../docs/external-adapter-protocol.md) and its
[normative schema](../../../docs/external-adapter-protocol.schema.json) for the
wire contract. This guide covers the standalone read-only sample at
`cmd/worklease-sample-adapter` and the writable local fixture at
`cmd/worklease-reference-adapter`. Both are teaching examples, not provider
integrations or production adapters.

## Manifest and protocol

The sample negotiates protocol major 1 and identifies as
`worklease.sample.static` version `1.0.0`. Its config schema accepts only an
empty object, it requires no authentication, and it declares the static
`generic` resource policy. Identity, discovery, dependencies, and state reads
are supported; other declared capability groups are unsupported. It implements
`resolve`, `capabilities`, `list`, `readItems`, `readDependencies`, `readItem`,
and `resourcePolicy`. Mutations and other unimplemented operations return the
stable `unsupported-capability` diagnostic without changing data.

The fixture is embedded from `internal/sampleadapter/fixture.json` and
contains three fixed items and two typed relationships. Requests after
initialization must include a future UTC deadline and a budget within the v1
limits. The sample bounds frames and in-flight work, paginates list and
relationship reads, checks source-qualified references, and accepts
`$/cancelRequest` to stop outstanding work. Standard output is reserved for
one compact JSON-RPC response per line; diagnostics do not include request
contents or secrets.

## Static resource policy

The manifest selects `generic`; the sample's `resourcePolicy` response supplies
the configured source ID, the exact requested item ID, and item scope. Worklease
applies its own static key policy to those inputs. The adapter never chooses or
returns an arbitrary claim key. Generic keys provide local coordination only;
they do not establish provider identity, execute provider work, or fence remote
writers. Keep this distinction explicit when adapting the example.

## Writable local fixture

`cmd/worklease-reference-adapter` reuses the sample's bounded JSON-RPC framing
and read model, but requires an explicit `fixturePath` configuration pointing
at a writable regular local JSON file (not a symlink). Start from
`internal/sampleadapter/reference-fixture.json` and copy it to a disposable
location before running write checks:

```sh
cp internal/sampleadapter/reference-fixture.json /tmp/worklease-reference-fixture.json

go build -o /absolute/path/worklease-reference-adapter ./cmd/worklease-reference-adapter
worklease queue adapter check \
  --executable /absolute/path/worklease-reference-adapter \
  --adapter-config '{"fixturePath":"/tmp/worklease-reference-fixture.json"}' \
  --disposable-target reference-1 --json
```

A queue source using the same local store binds its actor, transitions, and
generic claim identity explicitly:

```yaml
sources:
  - id: reference
    adapter: external
    executable: /absolute/path/worklease-reference-adapter
    expectedAdapterId: worklease.reference.local-fixture
    expectedVersion: 1.0.0
    account: alice
    config: {fixturePath: /absolute/path/worklease-reference-fixture.json}
    workflow: {start: Doing, blocked: Blocked, review: Review, complete: Done, reopen: Open}
    claims: {policy: generic, source: fixture/planning}
```

The store contains the configured fixture principal, an initial provider
version, allowed transition labels, items, and a durable write journal. Configure
the adapter's source workflow with those exact labels. Use `account` equal to
the store's `principal` so observations, action-scoped capabilities, and receipts
identify the same actor.

The adapter supports three writes. `writeState` accepts only a status present
among the fixture's configured `transitions` values. `recordProgress` appends a
comment record containing the exact `worklease-op:<operationId>` marker, content,
author, and provider receipt location. `assign` adds the configured principal
to the item's assignees. Each writes the JSON store and returns its source,
reference, operation, actor, incremented provider version, and durable fixture
location. Reusing the same operation ID with the same intent returns the
original receipt without applying the effect again; a different intent with an
already-used ID conflicts.

`readReceipt` never dispatches a write. For a progress append it uses the
journaled intent and the uniquely matching comment marker plus stored append
provenance to verify recovery without a receipt. A missing marker, duplicate
marker, mismatched content, or ambiguous attribution remains `unknown`. State
and assignment read-back require their provider receipt and current matching
state; matching state alone cannot attribute a lost response.

The receipt truthfully reports `conditionalWrite: false` and
`fencingEvidence: null`. The version check is only a pre-write fixture check;
it is not an atomic provider compare-and-swap. Atomic local file replacement
is not cross-process locking, provider fencing, or protection against another
actor editing the JSON concurrently. The adapter has no network access,
provider authentication, or provider-side guarantees. Use only a disposable
copy: the conformance check mutates its explicitly named target.

## Approval and trust

Build the executable and configure its absolute path, expected adapter ID and
version, and empty config in the selected queue source. For example:

```yaml
sources:
  - id: sample
    adapter: external
    executable: /absolute/path/worklease-sample-adapter
    expectedAdapterId: worklease.sample.static
    expectedVersion: 1.0.0
    config: {}
```

Before adding the source to queue.yaml or approving it, check the built executable:

```sh
worklease queue adapter check --executable /absolute/path/worklease-sample-adapter --adapter-config '{}' --json
```

The checker reports one pass/fail/skip entry per check. Skipped probes are not
passes: a normal read-only sample cannot demonstrate write recovery, and
blocking cancellation fixtures must be tested separately. For adapters declaring `host-credential-v1`, the checker sends a disposable secret canary and fails if the adapter echoes it on stdout, stderr, or in diagnostics. To exercise cancellation, configure a fixture that blocks the `__worklease_conformance_cancel__` list query, writes `PATH.request` when entered and `PATH.done` after handling `$/cancelRequest`, and pass a fresh `--cancel-marker PATH`. An ignored cancellation fails the check; without a marker it is skipped. The result
uses the CLI schema-version 2 envelope, `queue-adapter-check` operation, and
`verdict`, `manifest`, and `checks` fields. A failed check exits 65 with
`adapter-conformance-failed` and the checks in `error.details`; invalid inputs
exit 64; success exits 0. For an adapter that declares and supports mutations,
`--disposable-target ITEM` explicitly opts into a marked progress write and
receipt read-back against **only a disposable provider item**. Omit it for
production items. Config may instead be loaded with `--adapter-config-file FILE`.

Preview and inspect the exact source binding, executable path, identity/version,
and digest before acknowledging owner approval:

```sh
worklease queue adapter approve --source sample --json
worklease queue adapter approve --source sample --acknowledge --json
```

Approval is source-specific and must be repeated after a bound executable or
configuration changes. Do not add a credential reference; the sample does not
use credentials. An adapter runs with the user's privileges and is **not a
sandbox**. A malicious approved executable can access files and network
resources available to that user. Keep approval limited to reviewed binaries,
use the host's restricted environment, and treat provider titles and bodies as
data rather than instructions.

## Add host-managed credentials to your adapter

The sample above remains read-only and unauthenticated; the local fixture also
has no provider credentials. For a provider adapter, configure a private helper
and approved scope (not a literal token):

```yaml
sources:
  - id: provider
    adapter: external
    executable: /absolute/path/provider-adapter
    expectedAdapterId: example.provider
    expectedVersion: 1.0.0
    account: alice
    credentialHelper: [/absolute/path/provider-token-helper]
    config: {origin: "https://api.provider.example", tenant: team-one}
```

Your helper prints one token line on stdout and obtains it from an owner-private
store. In `initialize`, inspect `hostFeatures` for `host-credential-v1`; declare
it in `manifest.authentication` (and `requiredFeatures` if required to run).
Handle the host's `credential` request **before** `resolve`: read the token only
from `params.credential`, query the authenticated provider identity at
`params.origin`, check the token's provider scope, then return
`{"sourceId":"provider","origin":"https://api.provider.example","principal":"alice","expiresAt":"2027-01-01T00:00:00Z"}`
with the same source ID/origin and actual verified principal. `expiresAt` is
optional; never report an expired credential as valid. Replace the prior token
on subsequent `credential` calls; the host refreshes before each operation
without restarting the process. Never echo tokens in results, errors, stdout,
stderr, logs or receipts. The host compares the result with the approved
account and origin and blocks mismatches; it cannot independently verify your
provider API, so your adapter must perform that verification honestly.
Reapprove after changing helper, origin, tenant or account. An adapter without
this feature continues using its opaque `credentialRef` on `resolve`.

## Run and verify

From the repository root:

```sh
go build -o /absolute/path/worklease-sample-adapter ./cmd/worklease-sample-adapter
go test ./internal/sampleadapter
worklease queue adapter check --executable /absolute/path/worklease-sample-adapter --json

go build -o /absolute/path/worklease-reference-adapter ./cmd/worklease-reference-adapter
go test ./internal/queue -run TestReferenceAdapter
# In-repo fixtures exercise the same check implementation:
go test ./internal/queue -run TestAdapterConformance
```

Both checks exercise the production host rather than treating a successful
process launch as proof of protocol conformance. The in-process sample tests also launch a
one-purpose test-binary shim that calls the same exported `Run` function, so
they do not build executables from within Go tests.

Before publishing a real source adapter, complete the
[provider authoring checklist](source-provider-authoring-checklist.md). In
particular, document authoritative reads, dependency meaning, durable write
receipts, credentials, resource identity, and the exact guarantees that are not
provided. Do not infer mutation support or provider fencing from this sample.
