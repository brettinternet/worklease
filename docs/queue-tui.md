# TUI

Bare `worklease` opens the TUI in a terminal. It shows the views in [queue.yaml](queue.md) plus Recovery and Claims tabs, and opens on Claims when `queue.yaml` is absent. `worklease queue` opens it on the views too. Queue rows default to sources associated with the current project checkout (the main checkout when launched from a linked worktree); outside a configured project, all sources are included. The header names the effective project scope. Press `X` to toggle between that project and all projects; the TUI refreshes the source set before showing the widened scope. Listed sources in legacy views remain an additional restriction. Press `?` for every key.

```text
worklease  authority local 1cc38a84…  me @brett                                      synced now
 Ready 4   Mine 2   Claimed 2   Recovery 0   Claims 3
  ID        Title                                 Status       Ready                Assigned
  TASK-130  Add a Jira Cloud source adapter       To Do        ready                —
> TASK-131  Retry busy adapter snapshot startup   In Progress  mine                 @brett
  TASK-132  Decode resource keys in TUI displays  To Do        pi-agent@brett-mbp   —
  TASK-135  Document remote authority bootstrap   Blocked      blocked              —
12 done hidden
```

**Ready** says whether you can pick an item up:

| Ready | Meaning |
| --- | --- |
| `ready` | Free to claim |
| `mine` | This queue holds the claim |
| `agent@host` | Another session holds the claim |
| `blocked` | A prerequisite is open |
| `assigned to others` | Assigned only to other people; the Assigned column and detail name them |
| `ready ⠋` | Last known state from the cache; being reread now |
| `ready (stale)` | Last known readiness; the reread failed, so `r` retries |
| `deps unknown`, `claim unknown`, `stale` | Not known yet; `r` refreshes |
| `done` | Finished; shown only after `d` |

The queue opens with the rows cached from the last run, even after a branch switch or a new commit, and rereads them in the background. `⠋ syncing` replaces `synced` in the header until the reread finishes. Open items are read first; a done item is read when you open it.

The footer shows only what needs attention, such as hidden done items, a filter, partial loading, or stale data.

## Item detail

`enter` opens the selected item. The detail sits beside the list at 120 columns or wider and replaces it when the terminal is narrower. When you go back, the selection stays on the item you opened, even if an action removed it from the view.

```text
TASK-131 Retry busy adapter snapshot startup
s status  p note  a assign me  o open  i view claim
 Summary   Dependencies   Activity   Claims   Recovery
Status   In Progress
Ready    mine
Assigned @brett
Claim    mine · expires in 9m

## Description
…
```

The action line lists only actions available now. Each one opens a preview; `enter` confirms and `esc` cancels.

| Key | Action |
| --- | --- |
| `S` | Start: claim, then move to the configured start status |
| `c` | Claim without changing provider status |
| `s`, `p`, `a`, `A` | Change status, add a progress note, assign to me, unassign me |
| `R` | Release a claim when no operation has started |
| `x`, `o` | Launch a worker, open in the provider |
| `i` | Jump to the item's claim in Claims |
| `m` | Load more comments or claim history |

## Recovery

Recovery lists provider writes whose outcome is unknown: `r` retries read-back, `e` attests with audit evidence, `u` reloads the journal. See [queue configuration](queue.md) for the matching `worklease queue recovery` commands.

## Claims

The Claims tab lists every claim on the authority, including claims held by other queues and agents, newest acquisition first. Renewals do not change the order. A claim on a loaded item shows that item's title. `i` jumps back to the item.

```text
  ID        Claimed                                Holder              Expires
> TASK-131  Retry busy adapter snapshot startup    mine                9m
  TASK-132  Decode resource keys in TUI displays   pi-agent@brett-mbp  1m19s
            worklease TASK-126                     brett               expired
```

`m` shows claims with your agent identity (not proof of ownership), `e` shows claims about to expire, `s` shows stale claims, `/` filters by resource prefix, and `esc` clears the filters. An eligible contextual or MCP handle under the current Worklease home marks its claim in the list; detail shows its kind and path. Browse and filter without sending private credentials to the authority.

`u` renews an eligible active claim for the default TTL; `R` releases it with a reason (default `released`). Both show a preview of the authority, claim, resources, handle, expiry, and effect. Only explicit confirmation dispatches. Dismissing the preview, navigating, or refreshing never mutates a claim. Release frees the resources and removes the handle; renewal updates the displayed expiry immediately.

Actions are unavailable when the claim is not held by a discoverable handle here, the authority or remote restore ID differs, or the handle is pending/recovering. Use `worklease heartbeat --handle PATH` or `worklease release --handle PATH` for manual recovery and for handles outside the discoverable contextual/MCP directory; explicitly supplied token files remain CLI-only. Queue-owned handles under `queue-handles` must be managed from the item (`i`) or `worklease queue` because their lifecycle also updates the provider. For live supervised runs, use `worklease runs stop` so the supervisor owns heartbeat and release. Public holder identity alone never makes a claim eligible. If an outcome is uncertain, the handle stays pending; recover with the exact CLI command shown by the TUI.

## Keys

| Key | Action |
| --- | --- |
| `j`/`k`, `gg`/`G` | Move, first, last |
| `enter`, `esc` | Open, back |
| `tab` | Next detail section |
| `v`/`V`, `1`-`9` | Next or previous view, jump to view |
| `X` | Toggle current-project / all-projects scope and refresh |
| `/`, `esc` | Filter loaded rows, clear |
| `d` | Show or hide done items |
| `r`, `:`, `q` | Refresh, command palette, quit |
| `u`, `R` (Claims) | Preview renewal or release of an eligible private-handle claim |

The mouse works too: click a row to select it, click it again to open it, click a tab to switch, and use the wheel to scroll.
