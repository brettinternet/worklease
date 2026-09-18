# Claim, operation, and recovery model

Worklease coordinates cooperating workers through one selected authority. The
default owner-private SQLite authority coordinates one host; the experimental
self-hosted remote authority coordinates enrolled clients across hosts. Neither
mode replaces a backlog, proves provider writes, or stops uncooperative work.

## Exact resources and claims

A resource is an opaque byte-exact string inside one authority namespace. One
claim atomically owns one to 32 ordered, unique resources. Overlap conflicts;
there is no partial acquisition. Built-in policies derive deterministic keys,
but the claim service does not interpret them.

Repository, Markdown, Backlog.md, and path identities are host-local and are
rejected by remote authorities. Remote clients use caller-selected portable
resources such as `coordination:` or `github:` keys admitted by the server; no
identity is guessed from a Git remote, login, worktree, or path.

A claim has an immutable claim ID, hashed client-held credential, current
revision, expiry, checkpoint, and agent/work metadata. MCP leases have an
absolute `maxHold` deadline; ordinary CLI claims do not. `agentId` is audit
identity, never authorization.

## Credentials and handles

Acquire and transfer generate and privately persist credentials before sending
the exact request. Grants and replays never output a token. Mutations resolve a
credential from:

1. an explicit private handle;
2. configured handle selection;
3. `--token-file` or `--token-fd` with claim/revision inputs; or
4. an authority-bound contextual handle selected by root, stable contextual
   handle selector, and authority ID.

A handle includes authority ID, resources, claim ID, revision, credential, and
pending exact request. It is written atomically with owner-only permissions and
serialized across processes.

It is convenience state, not ownership or a provider checkpoint. Two loops in one
checkout must use distinct contextual handle selectors.

The selector resolves from `--session`, then `WORKLEASE_SESSION_ID`, then the
empty value. Human output renders the empty value as `"" (unscoped)`, but
`unscoped` is not stored as the selector.

Profiles that name the same authority ID share one contextual slot regardless of
profile name, endpoint, credential path, or restore ID. Switching authorities
selects an independent slot, so a newly initialized authority cannot overwrite or
retarget the previous authority's handle.

A ready contextual handle may be replaced only after the selected authority
confirms that its claim is inactive.

The replacement is a new ownership epoch. Worklease generates a new claim ID and
credential, uses the current acquire inputs, and writes the exact request as
pending before dispatch. A validated grant alone promotes that pending handle to
ready.

Local wall-clock expiry, failed or ambiguous status, an authority mismatch, or a
concurrent handle change never authorizes replacement.

A pending request always takes precedence over ready-handle replacement. Its
original bytes, identity, deadline, credential, and authority binding are replayed
exactly.

Selecting another `--session` chooses a different contextual handle; it is not
recovery for an uncertain request. The generated claim `sessionId` is epoch
metadata, while optional `--session` participates in contextual handle selection.
With an explicit selector the values may coincide, but they retain those separate
roles.

When the resolved selector is empty, acquire still generates a non-empty claim
`sessionId`. Using that generated value as a later selector chooses another slot
rather than rediscovering the unscoped handle. Selectors never enter resource
keys, so different selectors still contend on the same exact resource.

A legacy root-and-session handle migrates only when its embedded authority ID
matches the selected authority. Mismatched legacy state remains at its original
path. If both legacy and scoped paths exist, Worklease changes neither and
reports both paths for explicit `--handle` recovery.

`worklease handle inspect` reads the selected contextual handle, or an explicit
`--handle`, entirely offline. It reports only redacted authority, claim, selector,
locally recorded lifecycle, resource, and recovery metadata.

Text renders an empty resolved selector as `"" (unscoped)` and quotes nonempty
selectors. JSON keeps `selectorSession` as the exact string and reports claim
metadata separately as `claimSessionId`.

It does not create a missing handle or authority store. A recorded expiry is
never proof that authority-side ownership or external work has ended. An
explicitly named handle cannot reveal how it was originally selected, so its
selector provenance is reported as unknown.

`worklease handle archive` sets a stale or foreign handle aside without
contacting, releasing, revoking, or otherwise mutating any authority.

The exact owner-private record is durably copied to no-overwrite archive storage
before the original is removed. Pending or recovery state requires
`--acknowledge-pending-recovery`; refusal leaves the source unchanged.

A successful archive prints an explicit `--handle` recovery path. The archive is
never selected as a contextual handle, and the underlying claim may remain
active.

## Revisions, replay, and unknown outcomes

Every mutation names an operation ID and exact normalized request with a bounded
`requestNotAfter`. Omit the operation ID for ordinary work.

Reuse it only to replay the same request after losing a response. A changed
resource set, cwd, content digest, duration, checkpoint, or other protected intent
conflicts.

A committed response can remain unknown to the client. The pending request in
the handle permits authenticated replay during the recovery window. Acquire and
transfer replays remain authenticated even after the original epoch ends; no
public replay can recover a credential.

Guarded operations record intent before side effects. If ownership is lost, the
operation remains started.

A predecessor started operation blocks new guarded work until an authorized
operator checks the authoritative effect, proves the old executor has ceased, and
records explicit reconciliation. A file already matching a proposed hash is not
enough evidence that an old executor cannot still write.

## Guarantees

| Operation | Honest guarantee |
| --- | --- |
| Local-authority claim lifecycle | Lease exclusion among cooperating callers on one host |
| Remote-authority claim lifecycle | Lease exclusion among enrolled, cooperating clients across hosts |
| `exec` or a provider CLI/API | `local-coordination` for the client-local effect, even when the claim authority is remote |
| `replace-file` with exact path membership and expected hash | `local-serialized-replace` for that local replacement; unsupported remotely |
| Provider mutation with its own conditional write/fence | Provider's separately evidenced guarantee |

Expiry ends authorization but does not stop arbitrary child or remote work.
Guarded exec supervises its process group and records uncertain outcomes instead
of claiming impossible fencing. Default native-hook claim coverage verifies a
current claim only; opt-in path coverage requires exact claimed membership.

## Output redaction policy

All output passes through JSON normalization and recursive redaction. The same
rules apply to structs, typed collections, and loose JSON:

- Always redact keys named `token`, `tokenHash`, `bearer`, `password`, `secret`,
  `credential`, or `credentials`.
- Redact token-shaped values except documented SHA-256 and path fields.
- Redact token-shaped member names; preserve collisions with deterministic
  suffixes.
- Allow `argv`, `rawRequest`, `rawReceipt`, `checkpoint`, `evidence`, and
  `output` only in the invoking checkpoint, exec, or replace-file result, or in
  credential-authenticated `op inspect --full`.

Public CLI views, errors, and MCP output redact operation-private keys. Public
producers must still avoid private columns; redaction is not authorization.

## Events, cursors, and retention

The authority appends one ordered lifecycle event sequence. Events and history
are non-secret projections. They exclude credentials, raw requests, command
output, replacement contents, checkpoint values, reconciliation evidence, and
provider payloads.

Watch cursors carry authority identity and the last scanned sequence. Watches
poll time/state as well as events so expiry without a write is observable. A
timeout returns only the last fully scanned cursor.

Garbage collection retires eligible claims and removes only a safe contiguous
history prefix. Active claims, unresolved effects, authenticated replay windows,
and recently recorded endings pin required evidence. An ancient claim newly
retired today is retained from that recorded end, not its old expiry.

## Authority identity and copies

Each authority creates one immutable random authority ID. Handles, requests,
receipts, and cursors bind to it. A local home path or remote profile is only a
locator. Copying an active database to create another independent authority with
the same ID is unsafe and unsupported.

## Legacy Python state

Worklease does not import or delete Python-era state. Migrate it manually:

1. Stop every old Worklease process.
2. Move `leases.sqlite3`, `locks/`, `context-leases/`, and `mcp-leases/` from
   `WORKLEASE_HOME` into a private backup.
3. Keep the backup until rollback and historical inspection are unnecessary.

See [CLI reference](cli-reference.md) for commands,
[MCP and JSON](mcp.md) for agent orchestration, and the
[experimental remote authority guide](remote-claim-authority.md) for cross-host
setup, admission, and recovery.
