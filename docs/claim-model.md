# Claim, operation, and recovery model

Worklease coordinates cooperating processes through one owner-private SQLite
authority. It does not replace a backlog, prove provider writes, or stop remote
work.

## Exact resources and claims

A resource is an opaque byte-exact string inside one authority namespace. One
claim atomically owns one to 32 ordered, unique resources. Overlap conflicts;
there is no partial acquisition. Built-in policies derive deterministic keys,
but the claim service does not interpret them.

Repository, Markdown, Backlog.md, and path identities are host-local. A future
remote namespace must be caller-selected rather than guessed from a Git remote,
login, worktree, or path. Portable provider keys may be used today without
claiming cross-host exclusion.

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
4. an authority-bound contextual handle selected by root and stable session.

A handle includes authority ID, resources, claim ID, revision, credential, and
pending exact request. It is written atomically with owner-only permissions and
serialized across processes. It is convenience state, not ownership or a
provider checkpoint. Two loops in one checkout must use distinct sessions.

## Revisions, replay, and unknown outcomes

Every mutation names an operation ID and exact normalized request with a bounded
`requestNotAfter`. Omit the operation ID for ordinary work. Reuse it only to
replay the same request after losing a response. A changed resource set, cwd,
content digest, duration, checkpoint, or other protected intent conflicts.

A committed response can remain unknown to the client. The pending request in
the handle permits authenticated replay during the recovery window. Acquire and
transfer replays remain authenticated even after the original epoch ends; no
public replay can recover a credential.

Guarded operations record intent before side effects. If ownership is lost,
the operation remains started. A predecessor started operation blocks new
guarded work until an authorized operator checks the authoritative effect,
proves the old executor has ceased, and records explicit reconciliation. A file
already matching a proposed hash is not enough evidence that an old executor
cannot still write.

## Guarantees

| Operation | Honest guarantee |
| --- | --- |
| Claim lifecycle, `exec`, or a provider CLI/API invoked locally | `local-coordination` among cooperating callers on this host |
| `replace-file` with exact path membership and expected hash | `local-serialized-replace` for that local replacement |
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

Each local authority creates one immutable random authority ID. Handles,
requests, receipts, and cursors bind to it. A home path is only a locator.
Copying an active database to create another independent authority with the same
ID is unsafe and unsupported.

## Legacy Python state

Worklease does not import or delete Python-era state. Migrate it manually:

1. Stop every old Worklease process.
2. Move `leases.sqlite3`, `locks/`, `context-leases/`, and `mcp-leases/` from
   `WORKLEASE_HOME` into a private backup.
3. Keep the backup until rollback and historical inspection are unnecessary.

See [CLI reference](cli-reference.md) for commands and
[MCP and JSON](mcp.md) for agent orchestration. The remote authority design
document is explicitly deferred.
