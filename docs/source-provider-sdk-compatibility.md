# Source Provider SDK Compatibility

The `worklease-source-sdk` distribution publishes the provider boundary from
[`source-provider-contract.md`](../skills/worklease-workflow/references/source-provider-contract.md).
It is deliberately separate from the `worklease` lease core: providers own
credentials, network clients, discovery, authoritative writes, receipts, and
provider versions. The source workflow owns dependency scheduling and claim
lifecycle.

## Contract versions

| Contract | Version | Declared by | Compatibility rule |
| --- | ---: | --- | --- |
| Source-provider SDK | 1 | `SourceProvider.contract_version` and `CONTRACT_VERSION` | Providers targeting SDK 1 may depend on additive 1.x model fields, but must not require a newer major contract. |
| Resource policy | 1 | `ResourcePolicyDescriptor.contract_version` | Policies must use the `worklease.resource_policies` entry-point group and declare the exact policy contract version. |
| Resource key policy | 1 | `ResourcePolicyDescriptor.key_policy_version` | A policy upgrade that changes canonicalization, scope, or collision behavior requires a new key-policy version and a migration plan. |

A provider package should declare a compatible SDK range in its distribution
metadata and expose the exact SDK and resource-policy versions it tested. A
major contract mismatch is an installation or loading error, not a fallback to
another provider or policy.

## Provider implementation boundary

A `SourceProvider` should:

1. Return typed, source-qualified `Source`, `WorkRef`, and `WorkItem` values.
2. Let `discover` return the complete source and dependency closure.
3. Let `read_item` read authoritative provider state.
4. Accept caller-supplied authority for mutations.
5. Return a `ProviderReceipt` with the post-write version, durable location,
   observed state, and conditional-write or fencing evidence.
6. Return an explicit `CapabilityResult` for unsupported operations.

The SDK does not define scheduling, claim lifecycle, or provider credentials.
Never put bearer tokens in models, receipts, diagnostics, or checkpoints.

Set `provider_fencing=True` only when the provider itself rejects stale writes
conditionally and returns evidence. A local Worklease claim is not provider
fencing evidence.

## Resource-policy composition

A provider may compose a TASK-10 resource policy without adding provider code
to `worklease` core modules. Register a `ResourcePolicyRegistration` through
`worklease.resource_policies`; declare origin, policy versions, scope,
capability, generic execution guarantee, and `provider_fencing_supported`.
Resource policy code selects a deterministic local resource only. It does not
resolve work or perform provider writes.

The test-only package at
[`packages/worklease-source-sdk/examples/source-provider-plugin`](../packages/worklease-source-sdk/examples/source-provider-plugin)
shows this composition. Its entry point is:

```toml
[project.entry-points."worklease.resource_policies"]
example-source = "source_provider_example.policy:registration"
```

The example depends on `worklease-source-sdk`, implements the provider
protocol, and uses only the public TASK-10 policy types (`ProviderAdapter`,
`ResourceKey`, `ResourcePolicyDescriptor`, and `ResourcePolicyRegistration`).

## Installation and release checks

Build and test the SDK and example independently from the lease core:

```sh
uv build --project packages/worklease-source-sdk
uv build --project packages/worklease-source-sdk/examples/source-provider-plugin
PYTHONPATH=packages/worklease-source-sdk/src:packages/worklease-source-sdk/examples/source-provider-plugin/src \
  python -m unittest discover -s packages/worklease-source-sdk/tests -v
```

Tagged repository releases attach the SDK wheel and source distribution beside
the lease-core Python and native assets. Install the SDK artifact from the same
tag as the core before installing a provider package that depends on it.

Wheel and editable installs discover external resource-policy entry points
lazily. Frozen standalone `worklease` executables expose built-in policies only
and do not discover entry points from the build environment. Provider packages
must therefore document their installation mode and must not assume that a
frozen executable can load them.
