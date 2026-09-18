# Example: Local Markdown Source Replacement

Use this shape when one established Markdown file is authoritative and all items
share one source-wide mutation boundary.

1. Resolve and validate the explicit source path.
2. Parse the complete established format into source-qualified work items.
3. Select work through the generic workflow; the provider adapter does not
   select.
4. Derive and acquire the exact source resource with a private session handle.
5. Build candidate content without modifying the source.
6. Hash the current source and use guarded replacement.
7. Re-read/hash the provider file as the durable checkpoint, checkpoint the
   claim, then release.

```sh
worklease --json key --provider markdown --source "$source" --item __source__
# Read the envelope's exact resource into $resource.
worklease acquire --resource "$resource" --session "$session"
worklease replace-file --session "$session" --path "$source" \
  --expected-sha256 "$expected_sha256" --content-file "$candidate"
worklease checkpoint --session "$session" --data '{"providerVerified":true}'
worklease release --session "$session" --reason "provider checkpoint verified"
```

The caller extracts the exact resource string from the JSON key envelope and
owns IDs, paths, candidate content, and provider verification.

For this exact expected-hash replacement, the provider is the local file and
`local-serialized-replace` describes only that mutation. Direct edits, arbitrary
commands, and moves remain unfenced.

Credentials stay in the private handle and never appear in provider state or
output.
