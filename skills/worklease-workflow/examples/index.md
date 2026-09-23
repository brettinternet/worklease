# Source Workflow Examples

Choose the example by mutation guarantee, not by provider name:

- [`minimal-local-markdown.md`](minimal-local-markdown.md) shows exact local file replacement under a source claim and expected SHA-256.
- [`remote-provider-local-coordination.md`](remote-provider-local-coordination.md) shows same-host scheduling around an unfenced remote provider mutation.
- [`provider-conditional-write.md`](provider-conditional-write.md) shows a provider mutation that atomically rejects a stale provider version.

[`dependency-eligibility-v1.json`](dependency-eligibility-v1.json) is the versioned input/output graph fixture for TASK-128.3. Schema v1 uses `cases[]` with `graph.items` (source-qualified `ref: [sourceID,itemID]`), `graph.edges` (dependent `from` to prerequisite `to`, type/condition/provenance/evidence), `graph.coverage`, `candidate`, and requested `action`. `expected.readiness` is `ready`, `blocked`, or `unknown`; `expected.eligible` is action-specific; stable `reasons` explain exclusions and optional `outcome` records capability errors. Omitted typed edges with legacy `dependencies` use the terminal condition. Optional item `state` (default `open`) is the source-reported category; `resume` additionally requires `in-progress`. Legacy `dependencies` supplied beside typed edges must equal the hard-edge projection; otherwise readiness is unknown. Fixtures assert semantics, not a particular wire protocol; TASK-128.3 consumes them when implementing the core.

Every example inherits scheduling and claim-lifecycle rules from `worklease-workflow`. Provider credentials, authorized operations, source formats, and stable IDs remain caller-owned.
