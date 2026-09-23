# Provider Authoring Checklist

Complete this checklist for each source kind. A missing answer is an unsupported capability, not an invitation to guess.

## Identity and resolution

- What explicit locator identifies one source?
- Which caller configuration may derive a source when none is explicit?
- How are ambiguous sources reported without probing arbitrary providers?
- Are item IDs source-local? If so, how are they qualified as `WorkRef`?
- How are duplicate exact selectors rejected?

## Discovery and state

- How does `discover` enumerate the complete selected collection, including pagination?
- How are dependencies read and source-qualified?
- How are missing dependencies and provider blockers preserved?
- Which durable states map to `isTerminal` and `isBlocked`?
- Which provider values supply priority and stable order without overriding dependencies?
- Which fields must remain opaque metadata?

## Worklease identity and capability

- Which built-in key policy fits the provider, or why is a custom resource policy required?
- Is the claim scope one item or the whole source?
- Does the resource remain identical across worktrees, sessions, agents, and processes?
- Can two unrelated sources or items collide?
- What exact local operation, if any, executes within the Worklease guard?
- Is coordination-only mode required for direct provider mutations?

## Capability, queue reads, and freshness

- Are capabilities reported with support (`supported|unsupported|unknown`), permission (`allowed|denied|unknown`), availability (`available|unavailable|authentication-required`), semantics, limits, and reason?
- Are capability scopes distinguished at adapter, source, principal, and item/action levels? Does unknown remain non-actionable?
- Do read-only checks avoid probing support by attempting writes?
- Does every response carry principal, configuration generation, observation time, coverage, and opaque provider version when one exists?
- Are claim revision, provider version, provider update timestamp, and sync cursor kept distinct? Is a read ETag/timestamp kept separate from conditional-write support?
- Does summary listing expose pagination/cursor, coverage, and total accuracy (`exact|estimated|unknown`)?
- Do batched item reads report a result per reference and bound any per-item fallback instead of hiding unbounded fan-out?
- Do dependency reads declare completeness and retain typed relationship, direction, source-qualified endpoints, observation/version, completion condition, raw outcome, and configured interpretation?
- Is an optional change feed explicit about cursor ordering, retention, and gap/reset behavior?
- Are all capability groups declared: Identity, Discovery, Dependencies, State, Progress, Assignment, Native claims, Mutation, Synchronization, Effects, and Authentication? Do Effects include Git fetch/commit/hooks/notifications and Authentication include principal and credential lifecycle?
- Are unsupported capability, authentication, authorization, conflict, rate-limit/retry-time, unavailable-source, incomplete-graph, and unknown-outcome diagnostics structured and mapped to `contract.md` outcomes?

## Provider mutation

- Which caller-authorized operation performs each state/progress/review/archive write?
- How is the item refreshed immediately before mutation?
- Which provider version, ETag, transaction revision, or conditional predicate is supplied?
- Does the provider enforce that condition in the same durable mutation?
- Which unrelated fields must the adapter preserve?
- How are conflict, permission denial, unavailable tooling, and ambiguous outcomes represented?

## Durable checkpoint

- What provider-native state proves progress or completion?
- Which mutation response identifies the durable source/version?
- When must the adapter re-read provider state to verify the checkpoint?
- Can another caller discover the checkpoint without local files or handoff text?
- What blocks release when the receipt or resulting state is uncertain?

## Review and archive

- What is the default one-item review boundary?
- Which explicit provider-native selector can define a larger boundary?
- Can every requested member be resolved and persisted durably?
- What does archive, close, move, or complete mean for this provider?
- Which operation is unsupported rather than approximated?

## Security and guarantees

- Where do provider credentials come from, and which operations are authorized?
- Are provider secrets, Worklease tokens, or sensitive item content excluded from logs and checkpoints?
- Is `providerMutationFenced` initialized to `false`?
- What concrete provider conditional-write evidence, if any, permits setting it to `true`?
- Does documentation distinguish same-host local guarding from provider-side and cross-host fencing?

## Acceptance probes

Before publishing an adapter, demonstrate:

1. two sources containing the same item ID produce distinct `WorkRef` and resource values;
2. the same target in two worktrees produces the same resource;
3. complete discovery includes dependency closure and does not select work itself;
4. an unsupported write returns `capability` without creating a local shadow;
5. a stale provider version produces `conflict` and no completion checkpoint;
6. an ambiguous provider response blocks release until verified;
7. read-only claim output and all examples omit bearer tokens; and
8. provider fencing remains false unless a stale provider writer is rejected atomically by the provider.
