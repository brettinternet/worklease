# Backlog.md

## Source and item mapping

Resolve an explicit Backlog.md project path or caller-configured project. Use supported Backlog.md operations for discovery and reads; do not parse or edit task Markdown files as a substitute for the provider interface.

- `Source.id`: stable canonical project identity supplied by the caller
- `WorkRef.itemID`: exact task ID, qualified by `Source.id`
- `WorkItem.dependencies`: task dependencies returned by Backlog.md, converted to source-qualified references
- terminal/blocked state: caller-declared mapping of the project's configured statuses and blockers
- order: provider ordinal/priority after dependency and explicit-source ordering

Source-only discovery enumerates the complete project. An explicit task selector does not authorize mutation of its dependency closure.

## Initial capability declaration (Backlog.md 1.52.0)

Capabilities are evaluated per configured project and caller principal; these
observations do not imply write authorization beyond the named operation.

| Group | Declaration |
| --- | --- |
| Identity | Supported: task ID within one explicit checkout; host-local key or explicit D12 portable binding. Duplicate repair can renumber IDs. |
| Discovery | Supported: complete project list in one call, no cursor, exact observed total. |
| Dependencies | Supported: intra-project edges from task views; per-item closure; Backlog-reported `isReady` retained as provider evidence, not generic scheduling. |
| State | Supported when caller maps configured project statuses and blockers to normalized state. |
| Progress | Supported: append notes or comments; criterion-index mutation exists but is initially disabled by queue policy. |
| Assignment | Supported: multiple assignees; CLI edit replaces the full list, requiring read-modify-write. |
| Native claims | Unsupported: not exposed. |
| Mutation | Supported only as unconditional CLI edits; no provider compare-and-set or conditional write. Permission and availability remain caller/project-specific. |
| Synchronization | Supported: task-list watch or filesystem invalidation; `updatedAt` has minute resolution and is not a lossless cursor. |
| Effects | Project configuration may enable Git fetches (`remote_operations`/`check_active_branches`), commits (`auto_commit`), and hooks (unless bypassed). Expose these effects before authorized writes. |
| Authentication | Supported through local OS access to an explicitly configured checkout and the supported Backlog CLI; no provider login. |

## Worklease resource policy

Without a portable binding, use the static built-in Backlog.md key policy with
the project's backlog directory (for example `docs/backlog`, from
`backlog_directory` in the root `backlog.config.yml`), not the checkout root:

```sh
worklease key --provider backlog-md --source "$backlog_directory" --item "$task_id"
```

When the project declares a D12 portable binding, use exactly its agreed source
name instead; adopting it migrates every CLI, skill, and queue caller together:

```sh
worklease key --provider generic --source "$binding_source" --item "$task_id"
```

The two policies are distinct exclusion domains and never contend. The default
produces an item-scoped local key. Its local guarded-operation capability
does not make a Backlog.md CLI, MCP, SDK, or remote write provider-fenced.

Normalize direct provider mutations as `local-coordination` and set
`providerMutationFenced: false` unless the provider operation itself returns
conditional-write evidence.

## Authoritative operations

The caller supplies authorized Backlog.md reads and writes. Refresh the task and
dependency state before mutation. Preserve fields outside the requested patch.

A durable receipt is the resulting task ID plus provider state/version that can
be read again from the project. A Worklease `exec` receipt or command exit status
alone is insufficient.

Use Backlog.md's documented status, progress, review, document, and archive operations when authorized. If the installed interface cannot perform or verify a requested operation, return `capability`; never edit `docs/backlog/` records directly or maintain a writable local shadow.
