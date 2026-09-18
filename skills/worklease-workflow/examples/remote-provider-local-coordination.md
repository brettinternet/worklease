# Example: Remote Provider with Local Coordination

Use this shape for Linear, Jira, GitHub Issues, or another remote provider when
Worklease excludes cooperating callers on one host but the provider mutation
does not share a provider fence.

1. Resolve provider/source/item and read the authoritative version.
2. Normalize source, dependencies, state, and provider ordering.
3. Select eligible work through the generic workflow.
4. Derive the deterministic exact item resource and acquire it.
5. Refresh dependencies, claim, and provider state immediately before mutation.
6. Perform only the caller-authorized provider mutation.
7. Re-read and verify the provider checkpoint.
8. Checkpoint locally, then release.

Set `$key_policy` to `linear` or `github` for those built-ins. Use `generic` for
Jira or a custom provider.

```sh
worklease key --provider "$key_policy" --source "$source_locator" \
  --item "$item_id" --coordination-only --json
worklease acquire --resource "$resource" --session "$session"
# provider read/write/re-read occurs through caller-authorized capabilities
worklease checkpoint --session "$session" --data '{"providerVerified":true}'
worklease release --session "$session" --reason "provider checkpoint verified"
```

Record:

```text
guarantee: local-coordination
guaranteeScope: cooperating callers sharing this authority on one host
providerMutationFenced: false
```

Pre/post provider reads can detect a changed version or wrong result but do not
exclude another host or direct writer.

On version mismatch, ownership loss, permission failure, or ambiguous response,
stop and reconcile the provider checkpoint or allow expiry.

Assignment, status, comments, local command success, and Worklease receipts do
not replace the provider receipt.
