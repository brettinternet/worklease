# Optional agent setup

A fresh local Worklease install needs no client configuration. Use `worklease acquire`,
`status`, and `release` directly. For agent-led installation, local/remote selection,
and verification, start with the [agent setup guide](agent-setup.md) or
`worklease instructions setup`. MCP and native edit guards are separate,
optional integrations.

## MCP

Preview the documented project target without writing it:

```sh
worklease setup mcp --client claude-code --scope project
worklease setup mcp --client cursor --scope project
```

Use `--scope user` for Claude Code's `~/.claude.json` or Cursor's
`~/.cursor/mcp.json`. Add `--apply` to write atomically, or `--remove` to remove
only the generated `mcpServers.worklease` entry. Existing unrelated JSON keys
are retained. `--client generic` prints a snippet; copy it into the client's MCP
server configuration yourself.

The generated server command uses the absolute running Worklease binary.

An explicit global `--home` or `--config` is copied into its argument list, and
`--agent` adds `WORKLEASE_AGENT_ID` to the server environment. This keeps the CLI
and MCP server on the same local authority without putting credentials in
configuration.

MCP returns an opaque lease reference. CLI contextual handles and MCP lease
references are not selected implicitly across interfaces. To use an MCP claim
with a native hook, bind it explicitly when generating the hook:

```sh
worklease setup guard --client claude-code --lease LEASE_REFERENCE
```

## Optional native edit guard

Preview the Claude Code `PreToolUse` entry, then apply it explicitly:

```sh
worklease setup guard --client claude-code
worklease setup guard --client claude-code --apply
```

Use exact path coverage when each edited path must appear in the claim:

```sh
worklease setup guard --client claude-code --coverage path --apply
```

| Setting | Behavior |
| --- | --- |
| Default coverage | Requires a current claim for the selected context/session. |
| `--coverage path` | Also requires each edited path resource. |
| `--home`, `--config`, `--session`, `--handle`, `--lease` | Embeds the explicit selection in the quoted hook command. |
| `--remove` | Removes only the Worklease-managed hook, in either coverage mode. |
| `--client generic` | Prints a POSIX wrapper requiring `WORKLEASE_HANDLE` and `WORKLEASE_RESOURCE`. |

The Claude Code hook covers `Edit`, `Write`, `MultiEdit`, and `NotebookEdit`,
not Bash. It is a cooperative pre-edit check, not a filesystem fence: another
editor can race it. It cannot cover shell or provider writes, unsupported tools,
other hosts, or another authority.

Client shapes and hook behavior were checked against current documentation:

- [Claude Code settings](https://docs.anthropic.com/en/docs/claude-code/settings)
- [Claude Code hooks](https://docs.anthropic.com/en/docs/claude-code/hooks)
- [Cursor MCP](https://cursor.com/docs/context/mcp/install-links)
- [Cursor hooks](https://cursor.com/docs/agent/hooks)

Claude Code command hooks receive JSON on stdin, including `cwd`, `tool_name`,
and `tool_input`; exit code 2 blocks the tool call. Cursor's user hook shape is
`~/.cursor/hooks.json`, but Worklease v1 does not generate a Cursor native edit
guard.

## Agent instructions

`worklease setup instructions` prints a short project template inside versioned
`<!-- worklease:begin ... -->` / `<!-- worklease:end -->` markers. It does not
edit files.

Fill in the verified non-secret authority selection, work source, and exact
resource convention. Merge it into `AGENTS.md` without replacing unrelated
instructions or duplicating an existing block.

The template points agents to `worklease instructions loop` and
`worklease instructions safety` instead of embedding the full guides. Never put
credentials or invitations in it.
