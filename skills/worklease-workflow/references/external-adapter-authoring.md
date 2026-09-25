# External adapter authoring

Use the [v1 protocol](../../../docs/external-adapter-protocol.md) and its
[normative schema](../../../docs/external-adapter-protocol.schema.json) for the
wire contract. This guide covers the standalone sample at
`cmd/worklease-sample-adapter`; it is a read-only teaching fixture, not a
provider integration or a production adapter.

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

## Run and verify

From the repository root:

```sh
go build -o /absolute/path/worklease-sample-adapter ./cmd/worklease-sample-adapter
go test ./cmd/worklease-sample-adapter
# Run after the sample has been built and explicitly approved:
go test ./internal/queue -run TestAdapterConformance
```

The last command is the host conformance suite. It exercises the adapter
through the production host rather than treating a successful process launch as
proof of protocol conformance. The in-process sample tests also launch a
one-purpose test-binary shim that calls the same exported `Run` function, so
they do not build executables from within Go tests.

Before publishing a real source adapter, complete the
[provider authoring checklist](source-provider-authoring-checklist.md). In
particular, document authoritative reads, dependency meaning, durable write
receipts, credentials, resource identity, and the exact guarantees that are not
provided. Do not infer mutation support or provider fencing from this sample.
