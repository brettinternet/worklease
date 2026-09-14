package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	mcpserver "github.com/brettinternet/worklease/internal/mcp"
	"github.com/brettinternet/worklease/internal/reason"
	workleaseserver "github.com/brettinternet/worklease/internal/server"
	urfavecli "github.com/urfave/cli/v3"
)

// Top-level help groups. urfave sorts categories by name, so the names are
// chosen to read in workflow order: claim first, inspect second, set up last.
const (
	categoryLifecycle  = "Claim lifecycle"
	categoryInspection = "Inspection and recovery"
	categoryAdmin      = "Setup and administration"
)

// selectionHelp explains the shared claim-selection options that every
// contextual command accepts; usage lines refer to it as [selection].
const selectionHelp = "Selection: with no selection option the command uses the private contextual handle for the current Git worktree and --session. Pass --handle PATH for an explicit handle, or --claim-id ID --revision N with --token-file FILE or --token-fd N for explicit credentials."

// flagUsage is the single source of option help for flags whose meaning is
// the same everywhere. A backquoted word becomes the value placeholder.
var flagUsage = map[string]string{
	"handle":                  "private contextual handle `PATH` [$WORKLEASE_HANDLE]",
	"lease":                   "private lease `REF` issued by the MCP server",
	"claim-id":                "claim `ID` for explicit credentials; pair with --revision and one token source",
	"token-file":              "`FILE` holding the claim credential for explicit credentials",
	"session":                 "session `NAME` that keeps concurrent loops apart [$WORKLEASE_SESSION_ID]",
	"agent":                   "agent identity `NAME` [$WORKLEASE_AGENT_ID]",
	"provider":                "built-in policy `NAME` for a provider key; pair with --source and --item (see policy list)",
	"source":                  "provider `SOURCE` such as a repository path, backlog directory, or project",
	"item":                    "provider `ITEM` such as a task, issue, or file identifier",
	"work-key":                "opaque work `KEY` recorded with the claim for humans and history",
	"request-not-after":       "RFC3339 replay deadline `TIME` for explicit-credential requests, at most 24h ahead",
	"operation-id":            "explicit 32-hex operation `ID` so a retried mutation replays exactly",
	"data":                    "inline `JSON` recovery metadata stored with the claim",
	"data-file":               "`FILE` containing JSON recovery metadata",
	"to-agent":                "successor agent identity `NAME`",
	"to-session":              "successor session `NAME`",
	"to-work-key":             "successor work `KEY`",
	"successor-handle":        "`PATH` where the successor's private handle is written (required)",
	"hook":                    "native edit-hook protocol `NAME`; claude-code reads the hook event from stdin",
	"cwd":                     "working `DIR` for the guarded command",
	"expected-sha256":         "SHA-256 `HEX` the current file must match before it is replaced",
	"content-file":            "`FILE` holding the replacement content (at most 16 MiB)",
	"cursor":                  "opaque `CURSOR` from a previous --json page or watch result",
	"until":                   "wait `CONDITION`: free (no resource is claimed) or change (any lifecycle change)",
	"cutoff":                  "RFC3339 retention cutoff `TIME`; exclusive with --retention-days",
	"target-claim-id":         "claim `ID` that owns the unresolved operation",
	"target-operation-id":     "unresolved operation `ID` to resolve",
	"outcome":                 "observed `OUTCOME`: observed-success or observed-failure",
	"evidence":                "`JSON` evidence object recorded with the reconciliation",
	"expected-request-sha256": "request `HEX` hash the target operation must match",
	"scope":                   "configuration `SCOPE`: project or user",
}

func newCommands(s *boundary) []*urfavecli.Command {
	flag := func(name string, aliases ...string) *urfavecli.StringFlag {
		usage, ok := flagUsage[name]
		if !ok {
			panic("missing help for flag " + name)
		}
		return &urfavecli.StringFlag{Name: name, Aliases: aliases, Usage: usage}
	}
	ttlFlag := func() urfavecli.Flag {
		return &urfavecli.DurationFlag{Name: "ttl", Aliases: []string{"t"}, Usage: "claim lifetime `DURATION` [$WORKLEASE_TTL]", DefaultText: durationText(config.DefaultTTL)}
	}
	tokenFDFlag := func() urfavecli.Flag {
		return &urfavecli.IntFlag{Name: "token-fd", Usage: "inherited file descriptor `N` holding the claim credential for explicit credentials", HideDefault: true}
	}
	jsonless := func(name, usage, example string, flags ...urfavecli.Flag) *urfavecli.Command {
		c := &urfavecli.Command{Name: name, Usage: usage, UsageText: "worklease " + name, Description: usage + ".\n\nExamples:\n  " + example, Flags: flags}
		c.OnUsageError = func(_ context.Context, cmd *urfavecli.Command, _ error, _ bool) error {
			return s.handle(cmd, reason.New(reason.ReasonInvalidArgument, "invalid command-line arguments"))
		}
		return c
	}
	// detail replaces the one-line summary at the top of a description with
	// fuller guidance while preserving the trailing examples.
	detail := func(command *urfavecli.Command, text string) {
		index := strings.Index(command.Description, "\n\nExamples:")
		command.Description = text + command.Description[index:]
	}
	usageText := func(command *urfavecli.Command, lines ...string) {
		command.UsageText = strings.Join(lines, "\n")
	}
	selection := func() []urfavecli.Flag {
		return []urfavecli.Flag{flag("handle"), flag("lease"), flag("claim-id"), flag("token-file"), tokenFDFlag(), &urfavecli.Int64Flag{Name: "revision", Usage: "expected claim revision `N` for explicit credentials; required for mutations", HideDefault: true}, flag("session", "s")}
	}
	resourceFlag := func(usage string) *urfavecli.StringSliceFlag {
		return &urfavecli.StringSliceFlag{Name: "resource", Aliases: []string{"r"}, Usage: usage}
	}
	resource := func() []urfavecli.Flag {
		return []urfavecli.Flag{resourceFlag("exact resource `KEY`; repeat for up to 32 resources in one claim"), flag("provider"), flag("source"), flag("item"), &urfavecli.StringFlag{Name: "path", Usage: "file `PATH` resolved to a repository-relative path key"}, &urfavecli.BoolFlag{Name: "coordination-only", Usage: "coordinate on the key without permitting guarded local replacement"}}
	}
	mutate := func() []urfavecli.Flag {
		return append(selection(), ttlFlag(), flag("operation-id"), flag("request-not-after"))
	}
	full := func(usage ...string) urfavecli.Flag {
		description := "include non-secret metadata"
		if len(usage) > 0 {
			description = usage[0]
		}
		return &urfavecli.BoolFlag{Name: "full", Aliases: []string{"f"}, Usage: description}
	}
	limitFlag := func() urfavecli.Flag {
		return &urfavecli.IntFlag{Name: "limit", Usage: "maximum records `N` per page (1-1000)", DefaultText: "50"}
	}
	textOutput := func(command *urfavecli.Command, description string) {
		command.Description = strings.Replace(command.Description, ".\n\nExamples:", ".\n\nOutput: "+description+" ANSI color is used only on interactive terminals and is disabled by NO_COLOR, TERM=dumb, redirection, and --json.\n\nExamples:", 1)
	}
	resourceInputUsage := "(--path FILE | --resource KEY... | --provider NAME --source SOURCE --item ITEM)"

	keyCommand := jsonless("key", "derive a resource key", "worklease key --path README.md\n  worklease key --provider backlog-md --source docs/backlog --item TASK-1", resource()...)
	keyCommand.Action = keyAction(s)
	usageText(keyCommand, "worklease key "+resourceInputUsage+" [--coordination-only]")
	detail(keyCommand, "Derive the exact resource key a claim would use without touching the authority. Exactly one input form is accepted.")
	textOutput(keyCommand, "The text view starts with the derived-key outcome and deterministic lowerCamelCase fields.")

	acquireCommand := jsonless("acquire", "acquire a claim", "worklease acquire --path README.md\n  worklease acquire --resource KEY1 --resource KEY2 --ttl 30m\n  worklease acquire --path README.md --wait 2m",
		append(resource(),
			ttlFlag(),
			&urfavecli.DurationFlag{Name: "wait", Aliases: []string{"w"}, Usage: "wait up to `DURATION` for a contended resource instead of failing immediately", HideDefault: true},
			&urfavecli.DurationFlag{Name: "poll-interval", Usage: "`DURATION` between contention polls while waiting [$WORKLEASE_POLL_INTERVAL]", DefaultText: durationText(config.DefaultPollInterval)},
			&urfavecli.StringFlag{Name: "agent", Aliases: []string{"a"}, Usage: flagUsage["agent"], DefaultText: "login user"},
			flag("work-key"), flag("session", "s"), flag("handle"), flag("claim-id"), flag("token-file"), tokenFDFlag(), flag("request-not-after"),
			&urfavecli.BoolFlag{Name: "no-handle", Usage: "stateless acquire without a contextual handle; requires --claim-id, --session, and one token source"},
		)...)
	acquireCommand.Action = acquireActionReal(s)
	usageText(acquireCommand, "worklease acquire "+resourceInputUsage+" [options]", "worklease acquire "+resourceInputUsage+" --no-handle --claim-id ID --session NAME (--token-file FILE | --token-fd N)")
	detail(acquireCommand, "Atomically claim one to 32 ordered resources and write a private contextual handle for later commands. A second form acquires statelessly with explicit credentials and never writes a handle.")

	statusCommand := jsonless("status", "show claim status", "worklease status\n  worklease status --resource KEY --full", append(selection(), resourceFlag("public status for exact resource `KEY`; repeatable"), full("show complete identifiers, metadata, and absolute timestamps"))...)
	statusCommand.Action = statusActionReal(s)
	usageText(statusCommand, "worklease status [selection] [--full]", "worklease status --resource KEY... [--full]")
	detail(statusCommand, "Show the selected claim, or the public holder state of exact resources when --resource is given.\n\n"+selectionHelp)
	textOutput(statusCommand, "The default view shortens identifiers and shows relative expiry; --full shows complete non-secret metadata with RFC3339 timestamps.")

	listCommand := jsonless("list", "list current claims", "worklease list\n  worklease list --resource KEY --full", resourceFlag("only claims covering exact resource `KEY`"), full())
	listCommand.Action = listActionReal(s)
	usageText(listCommand, "worklease list [--resource KEY] [--full]")
	detail(listCommand, "List every current claim in the local authority, optionally filtered to one exact resource.")
	textOutput(listCommand, "The default view is a compact STATE, RESOURCE, and relative LEASE table and states plainly when there are no current claims; --full adds claim and agent IDs with RFC3339 expiry.")

	heartbeatCommand := jsonless("heartbeat", "renew the contextual claim", "worklease heartbeat\n  worklease heartbeat --ttl 30m", mutate()...)
	heartbeatCommand.Action = heartbeatActionReal(s)
	usageText(heartbeatCommand, "worklease heartbeat [selection] [--ttl DURATION]")
	detail(heartbeatCommand, "Renew the selected claim's expiry and advance its revision.\n\n"+selectionHelp)

	checkpointCommand := jsonless("checkpoint", "store recovery metadata", "worklease checkpoint --data '{\"phase\":\"tests\"}'\n  worklease checkpoint --data-file progress.json", append(mutate(), flag("data"), flag("data-file"))...)
	checkpointCommand.Action = checkpointActionReal(s)
	usageText(checkpointCommand, "worklease checkpoint (--data JSON | --data-file FILE) [selection]")
	detail(checkpointCommand, "Persist bounded JSON recovery metadata with the selected claim and renew it in the same transaction.\n\n"+selectionHelp)

	releaseCommand := jsonless("release", "release the contextual claim", "worklease release\n  worklease release --reason done", append(mutate(), &urfavecli.StringFlag{Name: "reason", Aliases: []string{"m"}, Usage: "release `REASON` recorded in history", DefaultText: "released"})...)
	releaseCommand.Action = releaseActionReal(s)
	usageText(releaseCommand, "worklease release [--reason REASON] [selection]")
	detail(releaseCommand, "End the selected claim so other agents can acquire its resources.\n\n"+selectionHelp)

	transferCommand := jsonless("transfer", "transfer a claim", "worklease transfer --successor-handle PATH --to-agent AGENT --to-session SESSION", append(mutate(), flag("to-agent"), flag("to-session"), flag("to-work-key"), flag("successor-handle"))...)
	transferCommand.Action = transferActionReal(s)
	usageText(transferCommand, "worklease transfer --successor-handle PATH [--to-agent NAME] [--to-session NAME] [--to-work-key KEY] [selection]")
	detail(transferCommand, "Atomically hand the selected claim's resources to a new successor claim whose private handle is written to --successor-handle.\n\n"+selectionHelp)

	verifyCommand := jsonless("verify", "verify contextual ownership", "worklease verify\n  worklease verify --resource KEY\n  worklease verify --hook claude-code --coverage path < hook-event.json", append(selection(), resourceFlag("exact resource `KEY` the claim must cover; repeatable"), flag("hook"), &urfavecli.StringFlag{Name: "coverage", Usage: "hook coverage `MODE`: claim (any edit while the claim is held) or path (edited file must be a claimed path)", DefaultText: "claim"})...)
	verifyCommand.Action = verifyAction(s)
	usageText(verifyCommand, "worklease verify [--resource KEY...] [selection]", "worklease verify --hook claude-code [--coverage claim|path] [selection] < hook-event.json")
	detail(verifyCommand, "Verify that the selected claim is active and, when resources are given, covers each of them. The hook form reads a native editor event from stdin and exits 2 to block the edit when verification fails.\n\n"+selectionHelp)

	execCommand := jsonless("exec", "run a guarded contextual command", "worklease exec -- git status\n  worklease exec --max-duration 10m -- mise run test", append(mutate(), &urfavecli.DurationFlag{Name: "max-duration", Usage: "kill the child after `DURATION` [$WORKLEASE_MAX_DURATION]", DefaultText: durationText(config.DefaultMaxDuration)}, flag("cwd"), &urfavecli.BoolFlag{Name: "git-primary", Usage: "run in the primary Git worktree of the current repository; exclusive with --cwd"})...)
	execCommand.Action = execAction(s)
	usageText(execCommand, "worklease exec [selection] [--max-duration DURATION] [--cwd DIR | --git-primary] -- COMMAND [ARGS...]")
	detail(execCommand, "Run COMMAND while the selected claim stays valid; everything after -- is the child argv. Ownership loss stops the child and the operation stays inspectable.\n\n"+selectionHelp)

	replaceCommand := jsonless("replace-file", "replace one file", "worklease replace-file --path FILE --expected-sha256 SHA256 --content-file CONTENT", append(mutate(), &urfavecli.StringFlag{Name: "path", Usage: "claimed file `PATH` to replace"}, flag("expected-sha256"), flag("content-file"))...)
	replaceCommand.Action = replaceFileAction(s)
	usageText(replaceCommand, "worklease replace-file --path FILE --content-file FILE [--expected-sha256 HEX] [selection]")
	detail(replaceCommand, "Atomically replace one claimed file with the content of --content-file, optionally only when the current content matches --expected-sha256.\n\n"+selectionHelp)

	historyCommand := jsonless("history", "show recent lifecycle events or retained resource history", "worklease history\n  worklease history --resource RESOURCE", resourceFlag("retained claim epochs for exactly one exact resource `KEY`"), flag("cursor"), limitFlag(), full("show complete identifiers, operation metadata, and absolute timestamps"))
	historyCommand.Action = historyAction(s)
	usageText(historyCommand, "worklease history [--limit N] [--full]", "worklease history --resource KEY [--limit N] [--cursor CURSOR] [--full]")
	textOutput(historyCommand, "Without a resource the command shows the global lifecycle event feed; --resource shows retained claim epochs for exactly one resource, including how each ended and its operation activity. --full adds complete non-secret metadata and RFC3339 timestamps. Cursors appear only in --json.")

	eventsCommand := jsonless("events", "show lifecycle events", "worklease events\n  worklease events --limit 20 --full", flag("cursor"), limitFlag(), full("show complete identifiers, event details, and absolute timestamps"))
	eventsCommand.Aliases = []string{"event"}
	eventsCommand.Action = eventsAction(s)
	usageText(eventsCommand, "worklease events [--limit N] [--cursor CURSOR] [--full]")
	textOutput(eventsCommand, "The default view shows compact events with their resources and relative timing; --full adds complete non-secret event metadata and RFC3339 timestamps. Cursors appear only in --json.")

	gcCommand := jsonless("gc", "preview or apply retention", "worklease gc\n  worklease gc --apply --cutoff 2026-08-14T00:00:00Z", &urfavecli.Float64Flag{Name: "retention-days", Usage: "collect records older than `DAYS` [$WORKLEASE_RETENTION_DAYS]", DefaultText: fmt.Sprint(config.DefaultRetention)}, flag("cutoff"), &urfavecli.BoolFlag{Name: "apply", Usage: "apply retention; without it the command only previews"})
	gcCommand.Action = gcAction(s)
	usageText(gcCommand, "worklease gc [--retention-days DAYS | --cutoff TIME] [--apply]")
	detail(gcCommand, "Preview, or with --apply collect, retained lifecycle records older than the cutoff while protecting active claims and unresolved operations.")
	textOutput(gcCommand, "The text view starts with a preview or applied outcome, deterministic lowerCamelCase fields, and the exact follow-up command for a preview.")

	watchCommand := jsonless("watch", "wait for lifecycle changes", "worklease watch --resource RESOURCE --until free\n  worklease watch --cursor CURSOR --timeout 5m", resourceFlag("exact resource `KEY` to watch; repeatable"), flag("cursor"), flag("until"), &urfavecli.DurationFlag{Name: "timeout", Usage: "give up after `DURATION`, at most 1h", DefaultText: "30s"})
	watchCommand.Action = watchAction(s)
	usageText(watchCommand, "worklease watch --resource KEY... --until (free|change) [--timeout DURATION]", "worklease watch --cursor CURSOR [--timeout DURATION]")
	detail(watchCommand, "Block until the watched resources reach --until, or until any lifecycle event lands after --cursor. A timeout is a normal outcome, not an error.")
	textOutput(watchCommand, "The text view starts with the observed outcome, omits routine false booleans, shows relative expiry and actionable gap guidance, and includes the next cursor only in a copyable resume command.")

	doctorCommand := jsonless("doctor", "run read-only diagnostics", "worklease doctor\n  worklease doctor --json")
	doctorCommand.Action = doctorAction(s)
	detail(doctorCommand, "Check configuration, authority home safety, database schema, handles, clocks, Git, and MCP availability without writing anything. Exit status is non-zero when a check fails.")

	versionCommand := jsonless("version", "print the version", "worklease version\n  worklease version --json")
	versionCommand.Action = func(ctx context.Context, cmd *urfavecli.Command) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return s.versionResult(cmd)
	}
	commands := []*urfavecli.Command{
		versionCommand, keyCommand, acquireCommand,
		statusCommand, listCommand, heartbeatCommand, checkpointCommand, releaseCommand, transferCommand,
		verifyCommand, execCommand, replaceCommand,
		historyCommand, eventsCommand, watchCommand, gcCommand, doctorCommand,
	}
	group := func(name, usage, example string, commands ...*urfavecli.Command) *urfavecli.Command {
		for _, child := range commands {
			if child != nil && child.UsageText == "worklease "+child.Name {
				child.UsageText = "worklease " + name + " " + child.Name
			}
		}
		return &urfavecli.Command{
			Name: name, Usage: usage, UsageText: "worklease " + name + " <command>",
			Description: usage + ".\n\nExamples:\n  " + example, Commands: commands,
			OnUsageError: func(_ context.Context, cmd *urfavecli.Command, err error, _ bool) error {
				return s.handle(cmd, reason.Invalid(err.Error()))
			},
			Action: func(_ context.Context, cmd *urfavecli.Command) error {
				if cmd.Args().Len() > 0 {
					return s.handle(cmd, reason.Invalid(fmt.Sprintf("unknown command %q", cmd.Args().First())))
				}
				if s.jsonRequested(cmd) {
					return s.handle(cmd, reason.Invalid(fmt.Sprintf("%s requires a subcommand", name)))
				}
				return urfavecli.ShowSubcommandHelp(cmd)
			},
		}
	}
	policyList := jsonless("list", "list built-in policies", "worklease policy list\n  worklease policy list --full", full("add local-replace and provider-fencing columns"))
	policyList.Action = policyListAction(s)
	policyDescribe := jsonless("describe", "describe a policy", "worklease policy describe path\n  worklease policy describe backlog-md --full", full("show contract versions and fencing guarantees"))
	policyDescribe.Action = policyDescribeAction(s)
	usageText(policyDescribe, "worklease policy describe NAME [--full]")
	detail(policyDescribe, "Describe one built-in resource policy by NAME (see policy list).")
	textOutput(policyDescribe, "The default view shows identity and capability fields; --full adds contract versions and fencing guarantees.")
	policy := group("policy", "show built-in policies", "worklease policy list", policyList, policyDescribe)

	inspectCommand := jsonless("inspect", "inspect an operation", "worklease op inspect --operation-id ID\n  worklease op inspect --resource KEY\n  worklease op inspect --operation-id ID --full", append(selection(), resourceFlag("latest operation for exact resource `KEY`"), flag("operation-id"), full("include authenticated request metadata; requires the owning claim's handle or credentials"))...)
	inspectCommand.Action = inspectAction(s)
	usageText(inspectCommand, "worklease op inspect (--operation-id ID | --claim-id ID | --resource KEY | [selection])", "worklease op inspect --operation-id ID --full [selection]")
	detail(inspectCommand, "Inspect a started or completed guarded operation. The public form redacts private payloads; --full authenticates with the owning claim and shows request metadata.\n\n"+selectionHelp)

	reconcileCommand := jsonless("reconcile", "reconcile an operation", "worklease op reconcile --target-operation-id ID --outcome observed-success --evidence '{\"outcome\":\"observed-success\",\"executorStopped\":true}'", append(mutate(), flag("target-claim-id"), flag("target-operation-id"), flag("outcome"), flag("evidence"), flag("expected-request-sha256"))...)
	reconcileCommand.Action = reconcileAction(s)
	usageText(reconcileCommand, "worklease op reconcile --target-operation-id ID --outcome OUTCOME --evidence JSON [--target-claim-id ID] [--expected-request-sha256 HEX] [selection]")
	detail(reconcileCommand, "Resolve a predecessor operation whose outcome is unknown by recording what was observed, so blocked resources become claimable again.\n\n"+selectionHelp)
	op := group("op", "inspect or reconcile operations", "worklease op inspect --operation-id ID", inspectCommand, reconcileCommand)

	loopCommand := jsonless("loop", "print loop instructions", "worklease instructions loop")
	loopCommand.Action = instructionsAction(s, "loop")
	detail(loopCommand, "Print the canonical acquire, verify, checkpoint, release loop that agents should follow.")
	safetyCommand := jsonless("safety", "print safety instructions", "worklease instructions safety")
	safetyCommand.Action = instructionsAction(s, "safety")
	detail(safetyCommand, "Print the canonical safety rules for credentials, guarded mutation, and recovery.")
	instructions := group("instructions", "print canonical instructions", "worklease instructions loop", loopCommand, safetyCommand)

	setupMCP := jsonless("mcp", "preview optional MCP client configuration; write only with --apply or --remove", "worklease setup mcp --client claude-code --scope project\n  worklease setup mcp --client cursor --apply",
		&urfavecli.StringFlag{Name: "client", Usage: "MCP `CLIENT`: claude-code, cursor, or generic (print only)", DefaultText: "claude-code"},
		&urfavecli.StringFlag{Name: "scope", Usage: flagUsage["scope"], DefaultText: "project"},
		flag("agent", "a"),
		&urfavecli.BoolFlag{Name: "apply", Usage: "write the configuration atomically"},
		&urfavecli.BoolFlag{Name: "remove", Usage: "remove the configuration"})
	setupMCP.Action = setupAction(s, "mcp")
	usageText(setupMCP, "worklease setup mcp [--client CLIENT] [--scope SCOPE] [--agent NAME] [--apply | --remove]")
	setupGuard := jsonless("guard", "preview an optional native edit guard; write only with --apply or --remove", "worklease setup guard --client claude-code --coverage claim\n  worklease setup guard --coverage path --apply",
		&urfavecli.StringFlag{Name: "client", Usage: "hook `CLIENT`: claude-code or generic (print only)", DefaultText: "claude-code"},
		&urfavecli.StringFlag{Name: "scope", Usage: flagUsage["scope"], DefaultText: "project"},
		&urfavecli.StringFlag{Name: "coverage", Usage: "hook coverage `MODE`: claim or path", DefaultText: "claim"},
		flag("session", "s"), flag("handle"), flag("lease"),
		&urfavecli.BoolFlag{Name: "apply", Usage: "write the hook configuration atomically"},
		&urfavecli.BoolFlag{Name: "remove", Usage: "remove the hook configuration"})
	setupGuard.Action = setupAction(s, "guard")
	usageText(setupGuard, "worklease setup guard [--client CLIENT] [--scope SCOPE] [--coverage claim|path] [--session NAME | --handle PATH] [--apply | --remove]")
	setupInstructions := jsonless("instructions", "print the managed AGENTS.md block", "worklease setup instructions")
	setupInstructions.Action = setupAction(s, "instructions")
	setup := group("setup", "configure optional integrations", "worklease setup instructions", setupMCP, setupGuard, setupInstructions)

	mcp := jsonless("mcp", "serve MCP over stdio", "worklease mcp")
	detail(mcp, "Serve the Model Context Protocol on stdin and stdout for a coding-agent client configured by setup mcp.")
	mcp.Action = func(ctx context.Context, cmd *urfavecli.Command) error {
		cfg, err := config.Load(config.Input{Flags: map[string]string{"home": cmd.String("home"), "agent": cmd.String("agent"), "config": cmd.String("config"), "ttl": cmd.String("ttl"), "poll_interval": cmd.String("poll-interval")}})
		if err != nil {
			return err
		}
		server, err := mcpserver.NewServer(mcpserver.Options{Home: cfg.Home, AgentID: cfg.AgentID, SessionID: cfg.SessionID, TTL: cfg.TTL, PollInterval: cfg.PollInterval})
		if err != nil {
			return err
		}
		return server.Serve(ctx, os.Stdin, s.writer)
	}

	hosted := hostedCommands(s)
	serve := jsonless("serve", "serve a hosted remote authority", "worklease serve --server-config server.yaml [--dev-http]",
		&urfavecli.StringFlag{Name: "server-config", Usage: "deployment server configuration `FILE`"},
		&urfavecli.BoolFlag{Name: "dev-http", Usage: "allow cleartext HTTP on a loopback address for development only"})
	serve.Action = func(ctx context.Context, cmd *urfavecli.Command) error {
		path := strings.TrimSpace(cmd.String("server-config"))
		if path == "" {
			return s.handle(cmd, reason.New(reason.ReasonConfigMissing, "--server-config is required"))
		}
		cfg, err := workleaseserver.LoadConfig(path)
		if err != nil {
			return s.handle(cmd, err)
		}
		srv, err := workleaseserver.New(ctx, cfg, cmd.Bool("dev-http"), nil)
		if err != nil {
			return s.handle(cmd, err)
		}
		return srv.Serve(ctx, cmd.Bool("dev-http"))
	}
	usageText(serve, "worklease serve --server-config FILE [--dev-http]")
	detail(serve, "Serve one marked hosted authority over the frozen Worklease HTTP protocol. TLS is required unless --dev-http is explicitly used on loopback.")
	all := append(commands, policy, op, instructions, setup, hosted, serve, mcp, helpCommand(s))
	for _, command := range all {
		switch command.Name {
		case "key", "acquire", "status", "list", "heartbeat", "checkpoint", "release", "transfer", "verify", "exec", "replace-file":
			command.Category = categoryLifecycle
		case "history", "events", "watch", "op", "doctor":
			command.Category = categoryInspection
		default:
			command.Category = categoryAdmin
		}
	}
	return all
}

// helpCommand replaces urfave's built-in help so that one deterministic,
// read-only invocation can print the whole command tree for onboarding.
func helpCommand(s *boundary) *urfavecli.Command {
	c := &urfavecli.Command{
		Name: "help", Aliases: []string{"h"}, Usage: "show help for one command, or the whole tree with --all",
		UsageText:   "worklease help [COMMAND [SUBCOMMAND]]\nworklease help --all",
		Description: "Print help for the root, one command, or one nested command. With --all, print every command once in tree order so a person or agent can read the complete interface in one pass.\n\nExamples:\n  worklease help acquire\n  worklease help op inspect\n  worklease help --all",
		Flags:       []urfavecli.Flag{&urfavecli.BoolFlag{Name: "all", Usage: "print help for every command and subcommand once, in tree order"}},
	}
	c.OnUsageError = func(_ context.Context, cmd *urfavecli.Command, _ error, _ bool) error {
		return s.handle(cmd, reason.New(reason.ReasonInvalidArgument, "invalid command-line arguments"))
	}
	c.Action = func(ctx context.Context, cmd *urfavecli.Command) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		root := s.root
		if cmd.Bool("all") {
			if cmd.Args().Len() > 0 {
				return s.handle(cmd, reason.Invalid("--all does not take a command"))
			}
			return writeAggregateHelp(ctx, root)
		}
		parent, target := root, root
		for _, name := range cmd.Args().Slice() {
			next := target.Command(name)
			if next == nil || next.Hidden {
				return s.handle(cmd, reason.Invalid(fmt.Sprintf("unknown command %q", name)))
			}
			parent, target = target, next
		}
		if target == root {
			return urfavecli.ShowRootCommandHelp(root)
		}
		return urfavecli.ShowCommandHelp(ctx, parent, target.Name)
	}
	return c
}

// writeAggregateHelp prints the root help followed by every visible command
// exactly once in depth-first registration order.
func writeAggregateHelp(ctx context.Context, root *urfavecli.Command) error {
	if err := urfavecli.ShowRootCommandHelp(root); err != nil {
		return err
	}
	for _, entry := range commandTree(root) {
		if entry.command.Name == "help" {
			continue
		}
		if _, err := fmt.Fprintln(root.Writer); err != nil {
			return err
		}
		if err := urfavecli.ShowCommandHelp(ctx, entry.parent, entry.command.Name); err != nil {
			return err
		}
	}
	return nil
}

type commandEntry struct {
	parent, command *urfavecli.Command
	path            []string
}

// durationText renders an effective default the way people type it (15m, 1h)
// instead of Go's zero-padded form (15m0s, 1h0m0s).
func durationText(value time.Duration) string {
	switch {
	case value >= time.Hour && value%time.Hour == 0:
		return fmt.Sprintf("%dh", int(value/time.Hour))
	case value >= time.Minute && value%time.Minute == 0:
		return fmt.Sprintf("%dm", int(value/time.Minute))
	default:
		return value.String()
	}
}

func commandTree(root *urfavecli.Command) []commandEntry {
	var result []commandEntry
	var visit func(*urfavecli.Command, []string)
	visit = func(parent *urfavecli.Command, prefix []string) {
		for _, command := range parent.VisibleCommands() {
			path := append(append([]string(nil), prefix...), command.Name)
			result = append(result, commandEntry{parent: parent, command: command, path: path})
			visit(command, path)
		}
	}
	visit(root, []string{root.Name})
	return result
}
