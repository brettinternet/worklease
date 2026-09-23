# Work queue configuration

The read-only queue uses one owner-private file, `$XDG_CONFIG_HOME/worklease/queue.yaml` (or `~/.config/worklease/queue.yaml` when XDG_CONFIG_HOME is unset). Create the `worklease` directory owner-private (`0700`) and the file owner-private (`0600`). Symlinks and files owned by another user are rejected. No repository configuration is read. The queue reads trusted remote authority names from the sibling `profiles.yaml`; `local` selects the built-in local authority.

```yaml
version: 1
me:
  github.com: brett
  backlog-md: ["@brett"]
sources:
  - id: worklease
    adapter: backlog-md
    checkout: ~/dev/me/worklease
    allowGitNetwork: false
    claims:
      policy: generic
      source: brettinternet/worklease/backlog
  - id: acme-api
    adapter: github
    host: github.com
    repository: acme/api
    account: brett
views:
  - name: Ready
    authority: local
    sources: [worklease, acme-api]
    filter:
      readiness: ready
      claim: free
      assigned: [me, nobody]
```

`checkout` must exist and `~` expands from HOME. Omit `claims` for host-local Backlog.md keys; a portable `generic` source must be agreed by all claimants before use. `allowGitNetwork` defaults to false. GitHub repositories use `owner/repo` and require an explicit host and account. View authorities must be `local` or a name in the trusted `profiles.yaml`; source IDs must be defined above. Filter keys are limited to `readiness`, `claim`, and `assigned`. The `launch` section is not supported in this slice. Missing configuration is reported as `no-sources-configured` with this setup guidance.

## Read-only query

`worklease queue query --view Ready --json` emits the queue query schema v1 inside the normal CLI schema-version 2 envelope. The `query` object contains the view name, authority profile/id/scope, per-source coverage and freshness, an `incomplete` flag, items, and an optional opaque `nextCursor`. Items carry `ref`, display/provider IDs, title, raw/normalized state, readiness and reasons, provider readiness, assignment, claim observation/native state, exact `resources` and `keyInputs` from the claim identity rules, and an `actions` map whose start/claim/launch/resume/report-blocked/record-progress/complete entries include `eligible`, `reasons`, `requires`, and `outcome`. Unknown action capabilities remain unavailable with reasons. The source rows contain coverage and freshness; provider-specific diagnostics are included when available. Schema v1 is the `query.schemaVersion` contract and is independent of the outer Worklease envelope version.

Use `--limit N` (default 50, range 1–1000) and pass `--cursor` from `nextCursor` for a bounded page. A cursor is valid only for the same view, filters, sources, authority, observed principals/configuration generations, and full source snapshot (including items hidden by the view filter); a changed snapshot returns `cursor-invalid`. `--require-complete` fails with one structured `incomplete` error envelope when source coverage or dependency closure is incomplete. Text output is a compact ID/state/readiness/claim/title table. The disposable index and `--max-age` are not part of this slice.
