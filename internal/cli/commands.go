package cli

import (
	"context"
	"fmt"
	"log"
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
const selectionHelp = "Selection: with no selection option the command uses the private contextual handle for the current Git worktree and resolved --session selector. An empty resolved selector uses the unscoped slot. A claim sessionId is ownership-epoch metadata, not a resource namespace; an explicit selector may give it the same value, but a generated unscoped claim sessionId cannot select that handle. If no claim is selected, acquire one first. Pass --handle PATH for an explicit handle, or --claim-id ID --revision N with --token-file FILE or --token-fd N for explicit credentials."

// flagUsage is the single source of option help for flags whose meaning is
// the same everywhere. A backquoted word becomes the value placeholder.
var flagUsage = map[string]string{
	"handle":                  "private contextual handle `PATH` [$WORKLEASE_HANDLE]",
	"lease":                   "private lease `REF` issued by the MCP server",
	"claim-id":                "claim `ID` for explicit credentials; pair with --revision and one token source",
	"token-file":              "`FILE` holding the claim credential for explicit credentials",
	"session":                 "contextual handle selector `NAME` that keeps concurrent loops apart [$WORKLEASE_SESSION_ID]",
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
	detail(acquireCommand, "Atomically claim one to 32 ordered resources and write a private contextual handle for later commands. --session selects that handle; when the resolved selector is empty, Worklease uses the unscoped slot and generates a separate claim sessionId as ownership-epoch metadata. A second form acquires statelessly with explicit credentials and never writes a handle.")

	statusCommand := jsonless("status", "show claim status", "worklease status\n  worklease status --resource KEY --full", append(selection(), resourceFlag("public status for exact resource `KEY`; repeatable"), full("show complete identifiers, metadata, and absolute timestamps"))...)
	statusCommand.Action = statusActionReal(s)
	usageText(statusCommand, "worklease status [selection] [--full]", "worklease status --resource KEY... [--full]")
	detail(statusCommand, "Show the selected claim, or the public holder state of exact resources when --resource is given.\n\n"+selectionHelp)
	textOutput(statusCommand, "The default view shortens identifiers and shows relative expiry; --full shows complete non-secret metadata with RFC3339 timestamps.")

	listCommand := jsonless("list", "list current claims", "worklease list\n  worklease list --resource KEY --full", resourceFlag("only claims covering exact resource `KEY`"), full())
	listCommand.Aliases = []string{"ls"}
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
	detail(checkpointCommand, "Persist bounded JSON recovery metadata with the selected claim and renew it in the same transaction. Supply exactly one --data JSON or --data-file FILE; if no claim is selected, acquire one first.\n\n"+selectionHelp)

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

	doctorCommand := jsonless("doctor", "run read-only diagnostics", "worklease doctor\n  worklease doctor --session NAME\n  worklease doctor --profile NAME --resource KEY\n  worklease doctor --json", resourceFlag("optional remote resource `KEY` to evaluate against advertised prefixes"), flag("session", "s"))
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
	policyList.Aliases = []string{"ls"}
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

	handleInspectCommand := jsonless("inspect", "inspect a private handle offline", "worklease handle inspect\n  worklease handle inspect --handle PATH", flag("handle"), flag("session", "s"))
	handleInspectCommand.Action = handleInspectAction(s)
	usageText(handleInspectCommand, "worklease handle inspect [--session NAME | --handle PATH]")
	detail(handleInspectCommand, "Read and validate the selected handle without contacting an authority or creating missing state. Output contains only redacted claim, selector, locally recorded lifecycle, resource, and recovery metadata. Recorded expiry never proves that a claim is inactive.")
	handleArchiveCommand := jsonless("archive", "archive a private handle offline", "worklease handle archive\n  worklease handle archive --handle PATH --destination ARCHIVE", flag("handle"), flag("session", "s"), &urfavecli.StringFlag{Name: "destination", Usage: "owner-private no-overwrite archive `PATH`"}, &urfavecli.BoolFlag{Name: "acknowledge-pending-recovery", Usage: "acknowledge that pending or recovery state and a possibly active claim are only being preserved aside"})
	handleArchiveCommand.Action = handleArchiveAction(s)
	usageText(handleArchiveCommand, "worklease handle archive [--session NAME | --handle PATH] [--destination PATH] [--acknowledge-pending-recovery]")
	detail(handleArchiveCommand, "Durably preserve the exact selected handle in owner-private no-overwrite storage before removing the source. This command never contacts, releases, or mutates an authority; the claim may remain active. Pending or recovery state requires --acknowledge-pending-recovery.")
	handleCommand := group("handle", "inspect or archive private handles offline", "worklease handle inspect", handleInspectCommand, handleArchiveCommand)

	setupGuide := jsonless("setup", "set up and verify agent coordination", "worklease instructions setup")
	setupGuide.Action = instructionsAction(s, "setup")
	remoteGuide := jsonless("remote", "join an existing remote claim authority", "worklease instructions remote")
	remoteGuide.Action = instructionsAction(s, "remote")
	serverGuide := jsonless("server", "host a remote claim authority (not MCP)", "worklease instructions server")
	serverGuide.Action = instructionsAction(s, "server")
	loopCommand := jsonless("loop", "print loop instructions", "worklease instructions loop")
	loopCommand.Action = instructionsAction(s, "loop")
	detail(loopCommand, "Print the canonical acquire, verify, checkpoint, release loop that agents should follow.")
	safetyCommand := jsonless("safety", "print safety instructions", "worklease instructions safety")
	safetyCommand.Action = instructionsAction(s, "safety")
	detail(safetyCommand, "Print the canonical safety rules for credentials, guarded mutation, and recovery.")
	instructions := group("instructions", "print canonical instructions", "worklease instructions setup", setupGuide, remoteGuide, serverGuide, loopCommand, safetyCommand)

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
		selected, err := profileSelection(cmd)
		if err != nil {
			return err
		}
		server, err := mcpserver.NewServer(mcpserver.Options{Home: cfg.Home, AgentID: cfg.AgentID, SessionID: cfg.SessionID, TTL: cfg.TTL, PollInterval: cfg.PollInterval, Profile: selected.Profile, ProfileName: selected.Name, QueueNext: mcpQueueNext(cfg.Home, selected.Name)})
		if err != nil {
			return err
		}
		return server.Serve(ctx, os.Stdin, s.writer)
	}

	server := serverCommands(s)
	serve := jsonless("serve", "serve a hosted remote authority", "worklease serve\n  worklease serve --server-config server.yaml",
		&urfavecli.StringFlag{Name: "server-config", Usage: "deployment server configuration `FILE` [$WORKLEASE_SERVER_CONFIG]"},
		&urfavecli.BoolFlag{Name: "allow-insecure-http", Usage: "allow cleartext HTTP on the configured listen address"})
	serve.Action = func(ctx context.Context, cmd *urfavecli.Command) error {
		path, err := serverConfigPath(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		if _, statErr := os.Lstat(path); os.IsNotExist(statErr) {
			return s.handle(cmd, reason.New(reason.ReasonConfigMissing, fmt.Sprintf("server configuration not found at %s; run worklease server init", path)))
		}
		cfg, err := workleaseserver.LoadConfig(path)
		if err != nil {
			return s.handle(cmd, err)
		}
		allowInsecureHTTP := cmd.Bool("allow-insecure-http") || cfg.AllowInsecureHTTP
		if allowInsecureHTTP {
			if _, err := fmt.Fprintln(s.errWriter, insecureHTTPWarning); err != nil {
				return err
			}
		}
		srv, err := workleaseserver.New(ctx, cfg, allowInsecureHTTP, log.New(s.errWriter, "", 0))
		if err != nil {
			return s.handle(cmd, err)
		}
		return srv.Serve(ctx, allowInsecureHTTP)
	}
	usageText(serve, "worklease serve [--server-config FILE] [--allow-insecure-http]")
	detail(serve, "Serve one marked hosted authority using --server-config, WORKLEASE_SERVER_CONFIG, or the default user configuration. TLS is required unless insecure HTTP is explicitly enabled by the file or flag.")
	queueQuery := jsonless("query", "query a configured work queue", "worklease queue query --view Ready --json", &urfavecli.IntFlag{Name: "limit", Usage: "maximum items `N` per page (1-1000)", DefaultText: "50"}, &urfavecli.StringFlag{Name: "cursor", Usage: "opaque `CURSOR` from the previous page"}, &urfavecli.DurationFlag{Name: "max-age", Usage: "serve the index when observations are younger than `DURATION`", HideDefault: true}, &urfavecli.BoolFlag{Name: "require-complete", Usage: "fail if source coverage or dependency edges are incomplete"})
	queueQuery.Action = queueQueryAction(s)
	usageText(queueQuery, "worklease queue query --view NAME [--json] [--limit N] [--cursor CURSOR] [--max-age DURATION] [--require-complete]")
	detail(queueQuery, "Read one configured queue view. Query is read-only; source coverage and dependency completeness are reported explicitly.")
	queueBrowse := queueCommand(s)
	queueNext := jsonless("next", "select ready work or claim it for an agent loop", "worklease queue next --view Ready --claim --session WORKER --json", &urfavecli.IntFlag{Name: "group", Value: 1, Usage: "return up to `N` independent ready items (1-32); read-only only"}, &urfavecli.StringSliceFlag{Name: "item", Usage: "select exact `SOURCE:ITEM` in explicit order (repeatable)"}, &urfavecli.BoolFlag{Name: "claim", Usage: "acquire the first available candidate for this worker without waiting"}, flag("session", "s"), flag("handle"), ttlFlag(), flag("agent", "a"))
	queueNext.Action = queueNextAction(s)
	usageText(queueNext, "worklease queue next --view NAME [--claim --session SESSION] [--json] [--group N] [--item SOURCE:ITEM ...]")
	detail(queueNext, "Select from a complete view and dependency graph. Plain next never acquires; --claim acquires one worker-owned claim with the regular contextual handle and no wait.")
	queueBrowse.Commands = []*urfavecli.Command{queueQuery, queueNext, queueIdentityCommand(s)}
	all := append(commands, queueBrowse, policy, op, handleCommand, instructions, setup)
	all = append(all, profileCommands(s)...)
	all = append(all, remoteAdminCommands(s)...)
	all = append(all, server, serve, mcp, helpCommand(s))
	for _, command := range all {
		switch command.Name {
		case "key", "acquire", "status", "list", "heartbeat", "checkpoint", "release", "transfer", "verify", "exec", "replace-file":
			command.Category = categoryLifecycle
		case "history", "events", "watch", "op", "handle", "doctor":
			command.Category = categoryInspection
		default:
			command.Category = categoryAdmin
		}
	}
	return all
}

func setShellCompletionHandlers(command *urfavecli.Command) {
	command.ShellComplete = writeShellCompletions
	for _, child := range command.Commands {
		setShellCompletionHandlers(child)
	}
}

// writeShellCompletions derives suggestions only from the registered CLI tree.
func writeShellCompletions(_ context.Context, command *urfavecli.Command) {
	last := ""
	if state, ok := command.Root().Metadata[jsonStateKey].(*boundary); ok {
		state.mu.Lock()
		args := append([]string(nil), state.invocation...)
		state.mu.Unlock()
		for index, arg := range args {
			if arg == "--generate-shell-completion" && index > 0 {
				last = args[index-1]
				break
			}
		}
	}
	if last == "" {
		args := command.Args().Slice()
		for index := len(args) - 1; index >= 0; index-- {
			if args[index] != "--generate-shell-completion" {
				last = args[index]
				break
			}
		}
	}
	if strings.HasPrefix(last, "-") {
		seen := make(map[string]bool)
		flags := append(command.VisibleFlags(), command.VisiblePersistentFlags()...)
		for _, flag := range flags {
			doc, _ := flag.(urfavecli.DocGenerationFlag)
			usage := ""
			if doc != nil {
				usage = strings.ReplaceAll(doc.GetUsage(), "`", "")
			}
			for _, name := range flag.Names() {
				token := "--" + name
				if len(name) == 1 {
					token = "-" + name
				}
				if !seen[token] {
					fmt.Fprintf(command.Root().Writer, "%s:%s\n", token, usage)
					seen[token] = true
				}
			}
		}
		return
	}
	children := command.VisibleCommands()
	if command == command.Root() {
		if help := command.Command("help"); help != nil && !help.Hidden {
			children = append(children, help)
		}
	}
	for _, child := range children {
		for _, name := range child.Names() {
			fmt.Fprintf(command.Root().Writer, "%s:%s\n", name, child.Usage)
		}
	}
}

const bashCompletionDependencyBlock = `  if declare -F _comp_initialize >/dev/null 2>&1; then
    _comp_initialize "$@"
  else
    _get_comp_words_by_ref "$@" cur prev words cword
  fi`

const bashCompletionFallbackBlock = `  if declare -F _comp_initialize >/dev/null 2>&1; then
    _comp_initialize "$@"
  elif declare -F _get_comp_words_by_ref >/dev/null 2>&1; then
    _get_comp_words_by_ref "$@" cur prev words cword
  else
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev=""
    if (( COMP_CWORD > 0 )); then
      prev="${COMP_WORDS[COMP_CWORD-1]}"
    fi
    words=("${COMP_WORDS[@]}")
    cword="${COMP_CWORD}"
  fi`

const bashCompletionUnsafeRequestBlock = `__worklease_build_completion_request() {
  local -a words_before_cursor=("${COMP_WORDS[@]:0:${COMP_CWORD}}")
  local current_word="${COMP_WORDS[COMP_CWORD]}"

  if [[ "${current_word}" == "-"* ]]; then
    printf '%s %s --generate-shell-completion' "${words_before_cursor[*]}" "${current_word}"
  else
    printf '%s --generate-shell-completion' "${words_before_cursor[*]}"
  fi
}`

const bashCompletionSafeRequestBlock = `__worklease_build_completion_request() {
  __worklease_completion_request=("${COMP_WORDS[@]:0:${COMP_CWORD}}")
  local current_word="${COMP_WORDS[COMP_CWORD]}"

  if [[ "${current_word}" == "-"* ]]; then
    __worklease_completion_request+=("${current_word}")
  fi
  __worklease_completion_request+=("--generate-shell-completion")
}`

const bashCompletionUnsafeInvocation = `    local request_comp

    COMPREPLY=()
    cur="${words[$cword]}"

    __worklease_init_completion -n "=:" || return

    request_comp="$(__worklease_build_completion_request)"
    opts=$(eval "${request_comp}" 2>/dev/null)`

const bashCompletionSafeInvocation = `    local -a __worklease_completion_request

    COMPREPLY=()
    cur="${words[$cword]}"

    __worklease_init_completion -n "=:" || return

    __worklease_build_completion_request
    opts=$("${__worklease_completion_request[@]}" 2>/dev/null)`

func safeBashCompletion(script string) (string, error) {
	for _, replacement := range []struct{ old, new string }{
		{bashCompletionDependencyBlock, bashCompletionFallbackBlock},
		{bashCompletionUnsafeRequestBlock, bashCompletionSafeRequestBlock},
		{bashCompletionUnsafeInvocation, bashCompletionSafeInvocation},
	} {
		if !strings.Contains(script, replacement.old) {
			return "", fmt.Errorf("unexpected Bash completion template")
		}
		script = strings.Replace(script, replacement.old, replacement.new, 1)
	}
	return script, nil
}

// configureCompletionCommand exposes urfave's generated completion scripts
// for the shells Worklease supports while keeping its runtime protocol hidden.
func configureCompletionCommand(s *boundary) urfavecli.ConfigureShellCompletionCommand {
	return func(command *urfavecli.Command) {
		command.Hidden = false
		command.Category = categoryAdmin
		command.Usage = "generate shell completion for Bash, Zsh, or Fish"
		command.UsageText = "worklease completion (bash|zsh|fish)"
		command.Description = "Print a deterministic completion script derived from the registered command tree. Generation and completion requests are read-only and do not inspect claim state.\n\nExamples:\n  worklease completion bash\n  worklease completion zsh\n  worklease completion fish"
		supported := map[string]bool{"bash": true, "zsh": true, "fish": true}
		commands := make([]*urfavecli.Command, 0, len(supported))
		for _, child := range command.Commands {
			if !supported[child.Name] {
				continue
			}
			child.UsageText = "worklease completion " + child.Name
			child.Description = fmt.Sprintf("Print the %s completion script.\n\nExamples:\n  worklease completion %s", child.Name, child.Name)
			if child.Name == "bash" {
				generate := child.Action
				child.Action = func(ctx context.Context, cmd *urfavecli.Command) error {
					root := cmd.Root()
					writer := root.Writer
					var generated strings.Builder
					root.Writer = &generated
					err := generate(ctx, cmd)
					root.Writer = writer
					if err != nil {
						return err
					}
					script, err := safeBashCompletion(generated.String())
					if err != nil {
						return err
					}
					_, err = writer.Write([]byte(script))
					return err
				}
			}
			commands = append(commands, child)
		}
		command.Commands = commands
		setShellCompletionHandlers(command)
		command.Action = func(_ context.Context, cmd *urfavecli.Command) error {
			if cmd.Args().Len() > 0 {
				return s.handle(cmd, reason.Invalid(fmt.Sprintf("unsupported shell %q; supported shells: bash, zsh, fish", cmd.Args().First())))
			}
			return urfavecli.ShowSubcommandHelp(cmd)
		}
	}
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
