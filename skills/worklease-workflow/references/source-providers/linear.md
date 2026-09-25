# Linear

## Source and item mapping

Resolve an explicit Linear organization and team/project scope or one caller-configured source. The caller supplies the authorized Linear integration and credentials. Confirm the proposed identities with the S8 live probe before enabling claims.

- `Source.id`: stable Linear organization ID for resource derivation; team/project restricts discovery but does not change the claim source
- `WorkRef.itemID`: issue UUID, qualified by the organization ID; the mutable issue identifier (such as `ENG-123`) is a display/lookup alias, not a claim input
- dependencies: provider-native blocking relations converted to source-qualified references
- terminal/blocked state: explicit mapping of the source's workflow states and blockers
- order: provider priority/order after dependency and explicit-source ordering

Discovery must enumerate the complete selected project/team collection and dependency closure. Resolve aliases to UUIDs within the configured organization; a team move must not silently change the claim resource. An issue identifier that can resolve in multiple teams or organizations is ambiguous.

## Worklease resource policy

The static built-in Linear key policy is coordination-only:

```sh
worklease key --provider linear --source "$organization_id" --item "$issue_uuid"
```

It derives a deterministic item resource but cannot guard the remote mutation.
Keep the Worklease guarantee `local-coordination` and
`providerMutationFenced: false` unless the caller supplies a Linear operation
that atomically rejects stale versions and returns fencing evidence.

Assignment, status, or a comment is visibility, not a claim.

## Authoritative operations

Use caller-authorized Linear reads and mutations. Refresh current issue state and provider version/updated marker before writing, preserve unrelated fields, and retain or re-read the resulting issue as the durable receipt. Pre/post reads can detect some races but do not make the mutation provider-fenced.

Resolve larger review boundaries only from an explicit provider-native project, initiative, parent, or other selector whose exact members can be returned and persisted. Archive/complete behavior must preserve the provider's distinction when one exists; unsupported archive operations return `capability` rather than deleting or shadowing an issue.
