package cli

import (
	"context"

	"github.com/brettinternet/worklease/internal/reason"
	urfavecli "github.com/urfave/cli/v3"
)

func newCommands(s *boundary) []*urfavecli.Command {
	flag := func(name string, aliases ...string) *urfavecli.StringFlag {
		usage := name
		switch name {
		case "handle":
			usage = "explicit private handle [$WORKLEASE_HANDLE]"
		case "session":
			usage = "stable loop session [$WORKLEASE_SESSION_ID]"
		case "agent":
			usage = "agent identity [$WORKLEASE_AGENT_ID]"
		}
		return &urfavecli.StringFlag{Name: name, Aliases: aliases, Usage: usage}
	}
	jsonless := func(name, usage, example string, flags ...urfavecli.Flag) *urfavecli.Command {
		c := &urfavecli.Command{Name: name, Usage: usage, UsageText: "worklease " + name, Description: usage + ".\n\nExamples:\n  " + example, Flags: flags}
		c.OnUsageError = func(_ context.Context, cmd *urfavecli.Command, _ error, _ bool) error {
			return s.handle(cmd, reason.New(reason.ReasonInvalidArgument, "invalid command-line arguments"))
		}
		c.Action = func(ctx context.Context, cmd *urfavecli.Command) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if name == "version" {
				return s.versionResult(cmd)
			}
			return s.handle(cmd, reason.New(reason.ReasonInternal, name+" behavior is implemented by a later task"))
		}
		return c
	}
	selection := func() []urfavecli.Flag {
		return []urfavecli.Flag{flag("handle"), flag("lease"), flag("claim-id", "c"), flag("token-file", "F"), &urfavecli.IntFlag{Name: "token-fd", Aliases: []string{"D"}, Usage: "token descriptor"}, &urfavecli.Int64Flag{Name: "revision", Aliases: []string{"R"}, Usage: "expected revision"}, flag("session")}
	}
	resource := func() []urfavecli.Flag {
		return []urfavecli.Flag{&urfavecli.StringSliceFlag{Name: "resource", Aliases: []string{"r"}, Usage: "resource"}, flag("provider", "p"), flag("source", "s"), flag("item", "i"), flag("path"), &urfavecli.BoolFlag{Name: "coordination-only", Aliases: []string{"C"}, Usage: "coordination only"}}
	}
	mutate := func() []urfavecli.Flag {
		return append(selection(), &urfavecli.DurationFlag{Name: "ttl", Aliases: []string{"T"}, Usage: "claim lifetime [$WORKLEASE_TTL]"}, flag("operation-id", "o"), flag("request-not-after"))
	}
	resources := func() urfavecli.Flag {
		return &urfavecli.StringSliceFlag{Name: "resource", Aliases: []string{"r"}, Usage: "resource"}
	}
	full := func() urfavecli.Flag {
		return &urfavecli.BoolFlag{Name: "full", Aliases: []string{"f"}, Usage: "include non-secret metadata"}
	}
	commands := []*urfavecli.Command{
		jsonless("version", "print version metadata", "worklease version --json"),
		jsonless("key", "derive a resource key", "worklease key --path README.md", resource()...),
		jsonless("acquire", "acquire a claim", "worklease acquire --path README.md", append(resource(), &urfavecli.DurationFlag{Name: "ttl", Aliases: []string{"T"}, Usage: "claim lifetime [$WORKLEASE_TTL]"}, &urfavecli.DurationFlag{Name: "wait", Aliases: []string{"W"}, Usage: "bounded wait"}, &urfavecli.DurationFlag{Name: "poll-interval", Usage: "poll interval [$WORKLEASE_POLL_INTERVAL]"}, flag("agent", "a"), flag("work-key", "w"), flag("session"), flag("handle"), flag("claim-id", "c"), flag("token-file", "F"), &urfavecli.IntFlag{Name: "token-fd", Aliases: []string{"D"}, Usage: "token descriptor"}, flag("request-not-after"), &urfavecli.BoolFlag{Name: "no-handle", Usage: "disable contextual handle"})...),
		jsonless("status", "show claim status", "worklease status", append(selection(), resources(), full())...),
		jsonless("list", "list current claims", "worklease list", resources(), full()),
		jsonless("heartbeat", "renew a claim", "worklease heartbeat --handle PATH", mutate()...),
		jsonless("checkpoint", "store recovery metadata", "worklease checkpoint --handle PATH --data JSON", append(mutate(), flag("data"), flag("data-file"))...),
		jsonless("release", "release a claim", "worklease release --handle PATH", append(mutate(), flag("reason", "m"))...),
		jsonless("transfer", "transfer a claim", "worklease transfer --handle PATH", append(mutate(), flag("to-agent"), flag("to-session"), flag("to-work-key"), flag("successor-handle"))...),
		jsonless("verify", "verify ownership", "worklease verify --handle PATH", append(selection(), resources(), flag("hook"), flag("coverage"))...),
		jsonless("exec", "run a guarded command", "worklease exec --handle PATH -- command", append(mutate(), &urfavecli.DurationFlag{Name: "max-duration", Aliases: []string{"M"}, Usage: "child limit [$WORKLEASE_MAX_DURATION]"}, flag("cwd"), &urfavecli.BoolFlag{Name: "git-primary", Usage: "primary worktree"})...),
		jsonless("replace-file", "replace one file", "worklease replace-file --path FILE", append(mutate(), flag("path"), flag("expected-sha256"), flag("content-file"))...),
		jsonless("history", "show retained history", "worklease history --resource RESOURCE", resources(), flag("cursor"), &urfavecli.IntFlag{Name: "limit", Usage: "limit"}, full()),
		jsonless("events", "show lifecycle events", "worklease events", flag("cursor"), &urfavecli.IntFlag{Name: "limit", Usage: "limit"}, full()),
		jsonless("watch", "wait for lifecycle changes", "worklease watch --cursor CURSOR", append(selection(), resources(), flag("cursor"), flag("until"), &urfavecli.DurationFlag{Name: "timeout", Usage: "timeout"})...),
		jsonless("gc", "preview or apply retention", "worklease gc", &urfavecli.Float64Flag{Name: "retention-days", Usage: "retention [$WORKLEASE_RETENTION_DAYS]"}, flag("cutoff"), &urfavecli.BoolFlag{Name: "apply", Usage: "apply"}),
		jsonless("doctor", "run diagnostics", "worklease doctor"),
	}
	group := func(name, usage, example string, commands ...*urfavecli.Command) *urfavecli.Command {
		return &urfavecli.Command{Name: name, Usage: usage, UsageText: "worklease " + name + " <command>", Description: usage + ".\n\nExamples:\n  " + example, Commands: commands}
	}
	policy := group("policy", "show built-in policies", "worklease policy list", jsonless("list", "list built-in policies", "worklease policy list"), jsonless("describe", "describe a policy", "worklease policy describe path", flag("name")))
	op := group("op", "inspect or reconcile operations", "worklease op inspect --operation-id ID", jsonless("inspect", "inspect an operation", "worklease op inspect", append(selection(), flag("operation-id", "o"), &urfavecli.BoolFlag{Name: "full", Aliases: []string{"f"}})...), jsonless("reconcile", "reconcile an operation", "worklease op reconcile", append(mutate(), flag("target-claim-id"), flag("target-operation-id"), flag("outcome"), flag("evidence"), flag("expected-request-sha256"))...))
	instructions := group("instructions", "print canonical instructions", "worklease instructions loop", jsonless("loop", "print loop instructions", "worklease instructions loop"), jsonless("safety", "print safety instructions", "worklease instructions safety"))
	setup := group("setup", "configure integrations", "worklease setup instructions", jsonless("mcp", "configure MCP", "worklease setup mcp", flag("client"), flag("scope"), &urfavecli.BoolFlag{Name: "apply"}, &urfavecli.BoolFlag{Name: "remove"}), jsonless("guard", "configure native guard", "worklease setup guard", flag("client"), flag("scope"), flag("coverage"), &urfavecli.BoolFlag{Name: "apply"}, &urfavecli.BoolFlag{Name: "remove"}), jsonless("instructions", "print setup instructions", "worklease setup instructions"))
	mcp := jsonless("mcp", "serve MCP over stdio", "worklease mcp")
	return append(commands, policy, op, instructions, setup, mcp)
}
