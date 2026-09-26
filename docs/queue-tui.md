# TUI

Bare `worklease` opens the TUI in a terminal. It shows the views in [queue.yaml](queue.md) and a Claims tab, and opens on Claims when `queue.yaml` is absent. `worklease queue` opens it on the views too. Press `?` for every key.

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
| `elsewhere` | Assigned to someone else |
| `ready ⠋` | Last known state from the cache; being reread now |
| `ready (stale)` | Last known state; the reread failed, so `r` retries |
| `deps unknown`, `claim unknown`, `stale` | Not known yet; `r` refreshes |
| `done` | Finished; shown only after `d` |

The queue opens with the rows cached from the last run, even after a branch switch or a new commit, and rereads them in the background. `⠋ syncing` replaces `synced` in the header until the reread finishes.

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
| `s`, `p`, `a` | Change status, add a progress note, assign to me |
| `R` | Release a claim when no operation has started |
| `x`, `o` | Launch a worker, open in the provider |
| `i` | Jump to the item's claim in Claims |

## Claims

The Claims tab lists every claim on the authority, including claims held by other queues and agents. A claim on a loaded item shows that item's title. `i` jumps back to the item.

```text
  ID        Claimed                                Holder              Expires
> TASK-131  Retry busy adapter snapshot startup    mine                9m
  TASK-132  Decode resource keys in TUI displays   pi-agent@brett-mbp  1m19s
            worklease TASK-126                     brett               expired
```

`m` shows only your claims, `e` shows only claims about to expire, `/` filters by resource prefix, and `esc` clears the filters.

## Keys

| Key | Action |
| --- | --- |
| `j`/`k`, `gg`/`G` | Move, first, last |
| `enter`, `esc` | Open, back |
| `tab` | Next detail section |
| `v`/`V`, `1`-`9` | Next or previous view, jump to view |
| `/`, `esc` | Filter loaded rows, clear |
| `d` | Show or hide done items |
| `r`, `:`, `q` | Refresh, command palette, quit |

The mouse works too: click a row to select it, click it again to open it, click a tab to switch, and use the wheel to scroll.
