# GitHub Issues

## Source and item mapping

Resolve an explicit repository locator or one caller-configured repository. The caller supplies the authorized GitHub integration; this reference does not choose a CLI, SDK, MCP server, or credential source.

- `Source.id`: canonical repository identity
- `WorkRef.itemID`: issue number or stable node ID, always qualified by `Source.id`
- dependencies: provider-native sub-issue/blocked relationships or one caller-documented relation; unsupported dependency semantics remain empty/`capability`, not inferred from prose
- terminal/blocked state: explicit mapping of issue state and caller-owned blockers
- order: caller-documented priority/labels/order after dependency and explicit-source ordering

Discovery must paginate the complete selected collection. A bare issue number is ambiguous without a resolved repository.

## Initial capability declaration (GitHub Issues on github.com)

Capabilities are scoped to the configured repository, verified account, and
requested item/action. The following provider evidence does not itself grant
caller permission; permission and availability must be checked for the principal
and operation.

| Group | Declaration |
| --- | --- |
| Identity | Supported: configured `owner/repo` (github.com) or `host/owner/repo` (Enterprise) and issue number; retain node IDs. Detect repository rename/transfer. |
| Discovery | Supported: GraphQL cursor pages of 100; observed `totalCount` is not a multi-page snapshot. Exclude pull requests. |
| Dependencies | Supported where provider exposes blocked-by/blocking relationships; paginate per-item relationships and retain cross-repository references. Sub-issues are hierarchy, not prerequisites. |
| State | Supported: issue open/closed and `stateReason`; Projects status is unsupported/deferred. |
| Progress | Supported: append issue comments. |
| Assignment | Supported: add/remove assignees; multiple principals. |
| Native claims | Unsupported: not exposed. |
| Mutation | Issue edits are unconditional; unsafe methods do not support conditional requests absent endpoint-specific evidence. |
| Synchronization | Supported: `since` filtering and conditional-GET polling; no client webhooks. Cursor/filter semantics and page coverage remain explicit. |
| Effects | Mutations notify watchers; disclose this as a side effect. |
| Authentication | Use `gh auth token` for the explicitly configured host/account; verify the principal. Credentials, scopes, and quota context are account/host-scoped. |

## Worklease resource policy

Use the bundled GitHub key policy after repository and issue resolution.
`$repository` is `owner/repo` on github.com and the exact configured
`host/owner/repo` on an Enterprise host; a URL form derives a different key.

```sh
worklease key --provider github --source "$repository" --item "$issue_number"
```

This creates an item-scoped local key. A locally guarded GitHub command is not a
provider-fenced issue mutation; other hosts and direct writers remain possible.

Default to `providerMutationFenced: false` and normalize the mutation guarantee
as `local-coordination` unless the selected GitHub operation atomically enforces
a supplied provider version and returns evidence.

## Authoritative operations

Refresh the issue immediately before mutation and preserve fields not named by the requested patch. The durable receipt contains the repository-qualified issue identity, resulting state, and provider version or updated marker that can be re-read. A local command receipt alone is insufficient.

Review boundaries larger than one issue require an explicit caller selector with exact members. Closing or otherwise archiving an issue is allowed only when the caller defines that provider operation as the requested archive behavior; never infer source-wide closure from source-only discovery.
