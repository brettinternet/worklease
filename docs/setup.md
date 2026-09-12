# Optional agent setup

A fresh Worklease install needs no client configuration. Use `worklease acquire`,
`status`, and `release` directly. MCP and native edit guards are separate,
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

The generated server command uses the absolute running Worklease binary. An
explicit global `--home` or `--config` is copied into its argument list, and
`--agent` adds `WORKLEASE_AGENT_ID` to the server environment. This keeps the
CLI and MCP server on the same local authority without putting credentials in
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

The hook matches only `Edit`, `Write`, `MultiEdit`, and `NotebookEdit`. It does
not register Bash. Default claim coverage allows those edits while the selected
context/session has a current valid claim. Stricter path coverage requires the
claim to contain each edited path resource:

```sh
worklease setup guard --client claude-code --coverage path --apply
```

Explicit `--home`, `--config`, `--session`, `--handle`, or `--lease` selections
are embedded in the safely quoted hook command. `--remove` recognizes either
coverage form and removes only the Worklease-managed hook. `--client generic`
prints a POSIX wrapper which requires `WORKLEASE_HANDLE` and
`WORKLEASE_RESOURCE`; install and invoke that wrapper in the client yourself.

The native hook is a cooperative check immediately before an edit, not a fence
around the later filesystem write. Another editor can race it. It does not
cover shell commands, provider writes, unsupported client tools, other hosts,
or callers using another authority. A Worklease claim never turns an external
provider mutation into a provider-fenced operation.

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

`worklease setup instructions` prints the canonical loop and safety text inside
versioned `<!-- worklease:begin ... -->` / `<!-- worklease:end -->` markers for
an `AGENTS.md` integration.
