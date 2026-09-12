# Go rewrite boundary review — 2026-09-12

Reviewed completed TASK-85 and TASK-88, including lease ownership, authenticated
replay/recovery, SQLite locking, handle isolation, GC/watch races, and MCP output.

## Findings

| Priority | Finding | Disposition |
| --- | --- | --- |
| High | MCP redaction skips typed structs and slices. Bearer-shaped resource, agent, and work metadata can reach text and structured output. | Fixed in TASK-94. |
| High | Handle operations re-resolve pathnames after validation and locking. Replacing the parent directory separates the held lock from subsequent handle reads/writes. The write-lock opener also lacks `O_NOFOLLOW`. | TASK-95; requires a coordinated handle filesystem change. |
| Medium | `HoldUntil` affects authority expiry but is absent from acquire/heartbeat/checkpoint request hashes and pending inputs. Changed deadlines are accepted as equal replay intent. | TASK-96; requires service and adapter recovery changes. |

### MCP redaction fix

`internal/output/output.go` deliberately skips typed values. That policy was
unsafe at the MCP boundary: `gClaim` contains a typed string slice, while status,
list, events, and verify return typed projections. A resource containing a
64-hex sentinel survived both MCP output channels before this fix.

`internal/mcp/mcp.go` now converts success and error projections to JSON values
before recursive redaction. `UseNumber` preserves integers above 2^53. Both
output channels use the redacted projection. This leaves the broader CLI
authenticated/private output policy in TASK-90.

The new tests directly marshal the returned MCP result rather than using
`jsonText`, which itself redacts and could conceal a structured-output leak.
They exercise acquire, status, list, events, and verify, plus typed private
payloads in success/error results and exact numbers/public hashes. Both tests
failed before the fix and pass afterwards.

### Handle replacement reproduction

Using `internal/handle` through a temporary Go test overlay:

1. Create a private `handles` directory and a valid handle.
2. Acquire the exclusive lock for `handles/claim.json.lock`.
3. Rename the directory to `handles-old`, then recreate private `handles`.
4. Acquire another exclusive lock for the same original pathname.
5. Write and read a replacement handle through the original pathname.

Both locks coexist, and the first lock does not protect the replacement handle.
The test reproduced this deterministically. Source inspection also confirms
that only the immediate parent is checked and the write-lock opener follows
symlinks. This is a filesystem identity/contract gap; it is not a claim that
private handles defend against arbitrary access by another process as the same UID.

### Replay reproduction

Acquire a claim, then heartbeat with a fixed operation ID, TTL, request deadline,
and a hold deadline two minutes away. Replay those inputs with the hold deadline
thirty minutes away. The replay returns the original receipt with
`Idempotent=true`; it does not reject changed intent. An independent reviewer
reproduced this with a test overlay, and the primary reviewer reran it.

TASK-96 preserves the documented direct CLI takeover exception. It does not
propose enforcing MCP's hold budget on every future authenticated CLI mutation.

## Verification and limits

`mise run lint`, `format-check`, `test`, and `typecheck` passed. Race tests passed
for MCP, handles, GC, watch, leases, store, and ledger. Go/staticcheck caches were
redirected to temporary directories because the sandbox restricts default cache
writes. Required Git hooks were installed and run before commit.

The independent authority review found no other validated ownership,
reconciliation, replay, or SQLite locking defects. Local inspection and race
tests found no additional GC/watch defect. These results are bounded review
evidence, not a proof that all interleavings are safe.

Existing TASK-89 tracks same-handle guard reconciliation; TASK-90 tracks broader
output policy; TASK-91 tracks long replacement work inside SQLite transactions.
They remain separate work and are not resolved by this MCP fix.
