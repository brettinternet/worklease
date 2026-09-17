# Set up Worklease with an agent

Use this guide when a user asks you to set up Worklease. It is consumer setup
guidance, not the repository's contributor instructions. Inspect existing setup
before changing it, preserve unrelated configuration, and ask only for missing
consequential decisions.

## Install, then use version-matched instructions

Check `worklease version`. If it is not installed, follow the
[installation instructions](../README.md#install), using the user's preferred
installation method and scope. Installing Worklease does not require cloning
this repository or installing its contributor tools.

Once installed, run:

```sh
worklease instructions
worklease instructions setup
```

The first command lists topics; the second guides configuration and verification.
Instruction commands only print guidance. Follow the installed binary's
instructions and matching release documentation rather than assuming the latest
repository documentation describes an older binary. If the installed version
lacks these topics, ask before upgrading and use its matching setup reference.

## Choose the intended topology

| User intent | Route |
| --- | --- |
| Coordinate workers on one host | Follow `instructions setup`; workers must share the same owner-private local authority/state home, not merely have Worklease installed. |
| Use an existing remote authority | Follow `instructions remote`; obtain an administrator-issued invite privately. |
| Host a new remote authority | Follow `instructions server`; confirm deployment and network exposure before making changes. |

Remote authority is experimental. Hosting a claim authority with `worklease serve`
is separate from the optional local MCP stdio server, `worklease mcp`.
Do not create a server when the user only asked to join one. Do not fall back to
local coordination when remote setup fails.

Also resolve the authoritative work source, exact resource convention, and
configuration scope. Distinct sessions separate handles, not resource namespaces.
Remote resources must be admitted cross-host keys; host-local path, Backlog.md,
and Markdown keys cannot simply be reused remotely. Do not invent a mapping
without agreement from the other contenders.

Use the CLI by default. Configure [MCP or native hooks](setup.md) only when
requested or needed; preview before applying. Do not change user-wide defaults
for a checkout-only request. Inspect `worklease profile list` and
`worklease profile default` before enrollment: when no user default exists,
enrollment sets one. Obtain approval for that broader scope before proceeding;
existing defaults are preserved. Verify the resulting selection explicitly.

## Verify before declaring completion

Follow the checks in `worklease instructions setup`:

- Run `doctor` against the intended authority; check remote admission with
  `--resource KEY`.
- On an agreed disposable resource, acquire with one unique session and confirm
  a second independent session receives contention. Release the test claim.
  For remote workers, test from the intended hosts where possible.
- Confirm all intended workers select the same authority and exact resource
  convention. A single successful acquisition does not prove shared coordination.
- Keep invitations, credentials, and private handles outside checkouts, logs,
  chat, and project instructions.

Report failed or unavailable checks as blockers, not completed setup. Never
release an unrelated claim or reset authority state to make a check pass.

## Leave a small project nudge

Run `worklease setup instructions` to print a version-marked project block.
Fill its authority, work-source, and resource-convention placeholders with the
verified non-secret choices, then merge it into the project's agent instructions.
Preserve unrelated content and replace an existing Worklease block rather than
appending duplicates. This command prints a template; it does not edit files.

Keep installation steps and operator runbooks out of the persistent block. Future
agents need the project choices and pointers to `instructions loop` and
`instructions safety`, not the entire setup guide. Use the
[workflow skill](../skills/worklease-workflow/SKILL.md) only when dependency-aware
work selection, provider progress, review, or handoff requires its fuller contract.
