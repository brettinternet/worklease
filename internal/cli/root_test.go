package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/reason"
	urfavecli "github.com/urfave/cli/v3"
)

func TestVersionTextAndJSON(t *testing.T) {
	for _, args := range [][]string{{"worklease", "version", "--json"}, {"worklease", "--json", "version"}, {"worklease", "--version", "--json"}} {
		var stdout, stderr bytes.Buffer
		if err := Run(context.Background(), args, "1.2.3", "abc123", "2026-09-12T00:00:00Z", &stdout, &stderr); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		var result map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("%v: %v (%q)", args, err, stdout.String())
		}
		if result["schemaVersion"] != float64(2) || result["operation"] != "version" || result["version"] != "1.2.3" || result["commit"] != "abc123" || result["buildTime"] == nil || result["goVersion"] == nil {
			t.Fatalf("%v: %#v", args, result)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q", stderr.String())
		}
	}
	for _, test := range []struct {
		name, version, commit, buildTime, want string
	}{
		{name: "release", version: "1.0.0", commit: "ca97d51936e447ef33d1eb01610543c32a345dbf", buildTime: "2026-09-13T01:08:03Z", want: "worklease 1.0.0 (ca97d51, built 2026-09-13T01:08:03Z)\n"},
		{name: "development", version: "dev", commit: "unknown", buildTime: "unknown", want: "worklease dev\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var text bytes.Buffer
			if err := Run(context.Background(), []string{"worklease", "version"}, test.version, test.commit, test.buildTime, &text, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if text.String() != test.want {
				t.Fatalf("text output = %q, want %q", text.String(), test.want)
			}
		})
	}
}

func TestParserFailuresKeepOneJSONEnvelopeAndRedact(t *testing.T) {
	token := strings.Repeat("a", 64)
	for _, args := range [][]string{{"worklease", "--json", "--unknown=" + token}, {"worklease", "version", "--json", "--unknown=" + token}, {"worklease", "--json", string([]byte{0xff})}} {
		var stdout, stderr bytes.Buffer
		err := Run(context.Background(), args, "dev", "unknown", "unknown", &stdout, &stderr)
		if err == nil {
			t.Fatalf("%v: expected error", args)
		}
		if strings.Contains(stdout.String(), token) || strings.Contains(stderr.String(), token) {
			t.Fatalf("%v leaked credential", args)
		}
		if strings.Count(stdout.String(), "\n") != 1 {
			t.Fatalf("%v emitted multiple documents: %q", args, stdout.String())
		}
		var result map[string]any
		if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
			t.Fatalf("%v: %v (%q)", args, decodeErr, stdout.String())
		}
		if result["ok"] != false {
			t.Fatalf("%v: %#v", args, result)
		}
	}
}

func TestCommandTreeRegistrationHelpAndShortOptions(t *testing.T) {
	root := NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	want := []string{"version", "key", "policy", "acquire", "status", "list", "heartbeat", "checkpoint", "release", "transfer", "verify", "exec", "replace-file", "op", "history", "events", "watch", "gc", "doctor", "instructions", "setup", "server", "serve", "mcp", "help"}
	future := []string{}
	got := map[string]bool{}
	for _, command := range root.Commands {
		got[command.Name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("command %q is not registered", name)
		}
	}
	for _, name := range future {
		if command := root.Command(name); command != nil && (command.Usage == "" || !strings.Contains(command.Description, "Examples:")) {
			t.Errorf("future command %q has incomplete staged help", name)
		}
	}
	if err := validateCLICommandTree(root); err != nil {
		t.Fatal(err)
	}
	for _, command := range root.Commands {
		if command.Usage == "" {
			t.Errorf("%s has no usage", command.Name)
		}
		if !strings.Contains(command.Description, "Examples:") {
			t.Errorf("%s has no help example", command.Name)
		}
	}
	for _, part := range []string{"[$WORKLEASE_HOME]", "[$WORKLEASE_CONFIG]"} {
		found := false
		for _, flag := range root.Flags {
			if strings.Contains(flag.String(), part) {
				found = true
			}
		}
		if !found {
			t.Errorf("root help does not reference %s", part)
		}
	}
}

func TestEventAliasShowsEventsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "event", "--help"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "worklease events [--limit N]") {
		t.Fatalf("event alias help=%q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestHistoryHelpDocumentsBothProjections(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "history", "--help"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"worklease history\n", "worklease history --resource RESOURCE", "global lifecycle event feed", "retained claim epochs for exactly one resource"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("history help missing %q: %q", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestAffectedCommandHelpDescribesTextViewsAndColor(t *testing.T) {
	root := NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	for _, path := range []string{"key", "status", "history", "events", "watch", "gc", "policy describe"} {
		command := root
		for _, name := range strings.Fields(path) {
			command = command.Command(name)
		}
		if command == nil {
			t.Fatalf("missing %s command", path)
		}
		for _, want := range []string{"Output:", "ANSI color", "NO_COLOR", "TERM=dumb", "redirection", "--json"} {
			if !strings.Contains(command.Description, want) {
				t.Errorf("%s help missing %q: %q", path, want, command.Description)
			}
		}
	}
	for _, path := range []string{"status", "history", "events", "policy describe"} {
		command := root
		for _, name := range strings.Fields(path) {
			command = command.Command(name)
		}
		if !strings.Contains(command.Description, "--full") {
			t.Errorf("%s help does not explain --full: %q", path, command.Description)
		}
	}
}

type canonicalHelpCase struct {
	path, example string
	flags         []string
}

func (test canonicalHelpCase) pathUsage() string {
	return "worklease " + test.path
}

func TestCanonicalCommandHelpPathsFlagsAndExamples(t *testing.T) {
	root := NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	tests := []canonicalHelpCase{
		{path: "version", example: "worklease version --json", flags: []string{}},
		{path: "key", example: "worklease key --path README.md", flags: []string{"resource", "provider", "source", "item", "path", "coordination-only"}},
		{path: "acquire", example: "worklease acquire --path README.md", flags: []string{"resource", "provider", "source", "item", "path", "coordination-only", "ttl", "wait", "poll-interval", "agent", "work-key", "session", "handle", "claim-id", "token-file", "token-fd", "request-not-after", "no-handle"}},
		{path: "status", example: "worklease status", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "resource", "full"}},
		{path: "list", example: "worklease list", flags: []string{"resource", "full"}},
		{path: "heartbeat", example: "worklease heartbeat", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after"}},
		{path: "checkpoint", example: "worklease checkpoint --data '{\"phase\":\"tests\"}'", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after", "data", "data-file"}},
		{path: "release", example: "worklease release", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after", "reason"}},
		{path: "exec", example: "worklease exec -- git status", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after", "max-duration", "cwd", "git-primary"}},
		{path: "transfer", example: "worklease transfer --successor-handle PATH --to-agent AGENT --to-session SESSION", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after", "to-agent", "to-session", "to-work-key", "successor-handle"}},
		{path: "replace-file", example: "worklease replace-file --path FILE --expected-sha256 SHA256 --content-file CONTENT", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after", "path", "expected-sha256", "content-file"}},
		{path: "verify", example: "worklease verify", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "resource", "hook", "coverage"}},
		{path: "history", example: "worklease history", flags: []string{"resource", "cursor", "limit", "full"}},
		{path: "events", example: "worklease events", flags: []string{"cursor", "limit", "full"}},
		{path: "watch", example: "worklease watch --resource RESOURCE --until free", flags: []string{"resource", "cursor", "until", "timeout"}},
		{path: "gc", example: "worklease gc --apply --cutoff 2026-08-14T00:00:00Z", flags: []string{"retention-days", "cutoff", "apply"}},
		{path: "doctor", example: "worklease doctor", flags: []string{}},
		{path: "policy list", example: "worklease policy list", flags: []string{"full"}},
		{path: "policy describe", example: "worklease policy describe path", flags: []string{"full"}},
		{path: "op inspect", example: "worklease op inspect --operation-id ID", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "resource", "operation-id", "full"}},
		{path: "op reconcile", example: "worklease op reconcile --target-operation-id ID --outcome observed-success --evidence '{\"outcome\":\"observed-success\",\"executorStopped\":true}'", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after", "target-claim-id", "target-operation-id", "outcome", "evidence", "expected-request-sha256"}},
		{path: "instructions loop", example: "worklease instructions loop", flags: []string{}},
		{path: "instructions safety", example: "worklease instructions safety", flags: []string{}},
		{path: "setup mcp", example: "worklease setup mcp --client claude-code --scope project", flags: []string{"client", "scope", "agent", "apply", "remove"}},
		{path: "setup guard", example: "worklease setup guard --client claude-code --coverage claim", flags: []string{"client", "scope", "coverage", "session", "handle", "lease", "apply", "remove"}},
		{path: "setup instructions", example: "worklease setup instructions", flags: []string{}},
		{path: "server init", example: "worklease server init", flags: []string{"server-config", "bootstrap-invite-file", "guided", "listen", "endpoint", "transport", "admitted-prefix", "tls-cert", "tls-key", "confirm-non-loopback", "acknowledge-cleartext-credentials"}},
		{path: "server restore", example: "worklease server restore --home DIR --from FILE", flags: []string{"from", "selected-cutoff", "loss-interval-start", "loss-interval-end", "cutoff-unknown", "bootstrap-invite-file"}},
		{path: "server bootstrap-reissue", example: "worklease server bootstrap-reissue --home DIR --bootstrap-invite-file FILE", flags: []string{"bootstrap-invite-file"}},
		{path: "server retire", example: "worklease server retire --home DIR", flags: []string{"force", "unresolved-export"}},
		{path: "serve", example: "worklease serve", flags: []string{"server-config", "allow-insecure-http"}},
		{path: "mcp", example: "worklease mcp", flags: []string{}},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			command := root
			for _, name := range strings.Fields(test.path) {
				command = command.Command(name)
				if command == nil {
					t.Fatalf("missing command")
				}
			}
			for _, line := range strings.Split(command.UsageText, "\n") {
				if !strings.HasPrefix(line, test.pathUsage()) {
					t.Fatalf("usage line %q does not start with %q", line, test.pathUsage())
				}
			}
			if !strings.Contains(command.Description, test.example) {
				t.Fatalf("description lacks executable example %q: %q", test.example, command.Description)
			}
			got := make([]string, 0, len(command.Flags))
			for _, flag := range command.Flags {
				got = append(got, flag.Names()[0])
			}
			if strings.Join(got, "\x00") != strings.Join(test.flags, "\x00") {
				t.Fatalf("flags=%v want=%v", got, test.flags)
			}
			aliases := map[string]string{"resource": "r", "session": "s", "ttl": "t", "wait": "w", "agent": "a", "reason": "m", "full": "f"}
			for _, flag := range command.Flags {
				wantNames := flag.Names()[0]
				if alias := aliases[wantNames]; alias != "" {
					wantNames += "\x00" + alias
				}
				if strings.Join(flag.Names(), "\x00") != wantNames {
					t.Fatalf("flag %q names=%v want=%q", flag.Names()[0], flag.Names(), wantNames)
				}
			}
		})
	}
}

func TestEveryListCommandAcceptsLSAlias(t *testing.T) {
	root := NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	for _, path := range []string{"list", "policy list", "profile list", "installation list"} {
		t.Run(path, func(t *testing.T) {
			parent := root
			parts := strings.Fields(path)
			for _, name := range parts[:len(parts)-1] {
				parent = parent.Command(name)
				if parent == nil {
					t.Fatalf("missing parent command %q", name)
				}
			}
			list := parent.Command("list")
			if list == nil {
				t.Fatal("missing list command")
			}
			if got := strings.Join(list.Names(), ","); got != "list,ls" {
				t.Fatalf("list command names = %q, want list,ls", got)
			}
			if parent.Command("ls") != list {
				t.Fatal("ls does not resolve to the list command")
			}
		})
	}
}

func TestSupportedShortOptionsMatchLongForms(t *testing.T) {
	tests := []struct {
		name, path, long, short, value string
		boolean                        bool
	}{
		{name: "json", long: "json", short: "j", boolean: true},
		{name: "home", long: "home", short: "H", value: "/tmp/worklease-alias-test"},
		{name: "version", long: "version", short: "v", boolean: true},
		{name: "resource", path: "key", long: "resource", short: "r", value: "resource-key"},
		{name: "session", path: "status", long: "session", short: "s", value: "session-name"},
		{name: "ttl", path: "heartbeat", long: "ttl", short: "t", value: "20m"},
		{name: "wait", path: "acquire", long: "wait", short: "w", value: "2m"},
		{name: "agent", path: "acquire", long: "agent", short: "a", value: "agent-name"},
		{name: "full", path: "list", long: "full", short: "f", boolean: true},
		{name: "reason", path: "release", long: "reason", short: "m", value: "completed"},
	}
	parse := func(test struct {
		name, path, long, short, value string
		boolean                        bool
	}, option string) string {
		t.Helper()
		var captured string
		root := NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
		target := root
		args := []string{"worklease"}
		if test.path != "" {
			target = root.Command(test.path)
			args = append(args, test.path)
		}
		target.Action = func(_ context.Context, cmd *urfavecli.Command) error {
			captured = cmd.String(test.long)
			return nil
		}
		args = append(args, option)
		if !test.boolean {
			args = append(args, test.value)
		}
		if err := root.Run(context.Background(), args); err != nil {
			t.Fatalf("%s: %v", option, err)
		}
		return captured
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			long := parse(test, "--"+test.long)
			short := parse(test, "-"+test.short)
			if short != long {
				t.Fatalf("-%s parsed as %q, --%s parsed as %q", test.short, short, test.long, long)
			}
		})
	}

	var longHelp, shortHelp bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "status", "--help"}, "dev", "unknown", "unknown", &longHelp, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), []string{"worklease", "status", "-h"}, "dev", "unknown", "unknown", &shortHelp, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if shortHelp.String() != longHelp.String() {
		t.Fatal("-h and --help produced different help")
	}
}

func TestShortOptionNamespaceIsExactAndRemovedAliasesFail(t *testing.T) {
	root := NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	if err := root.Run(context.Background(), []string{"worklease", "--help"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"j": "json", "H": "home", "h": "help", "v": "version", "r": "resource", "s": "session", "t": "ttl", "w": "wait", "a": "agent", "f": "full", "m": "reason"}
	got := map[string]string{}
	collect := func(flags []urfavecli.Flag) {
		for _, flag := range flags {
			for _, name := range flag.Names()[1:] {
				if len(name) == 1 {
					got[name] = flag.Names()[0]
				}
			}
		}
	}
	collect(root.VisibleFlags())
	for _, entry := range commandTree(root) {
		collect(entry.command.VisibleFlags())
	}
	for short, long := range want {
		if got[short] != long {
			t.Errorf("-%s means %q, want %q", short, got[short], long)
		}
	}
	for short, long := range got {
		if want[short] != long {
			t.Errorf("unexpected -%s/--%s", short, long)
		}
	}

	removed := [][]string{
		{"worklease", "key", "-p", "generic"},
		{"worklease", "key", "-i", "item"},
		{"worklease", "key", "-C"},
		{"worklease", "key", "-s", "source"},
		{"worklease", "heartbeat", "-c", "claim"},
		{"worklease", "heartbeat", "-F", "token"},
		{"worklease", "heartbeat", "-D", "3"},
		{"worklease", "heartbeat", "-R", "1"},
		{"worklease", "heartbeat", "-o", strings.Repeat("1", 32)},
		{"worklease", "exec", "-M", "1m", "--", "true"},
		{"worklease", "acquire", "-T", "1m", "--resource", "resource"},
		{"worklease", "acquire", "-W", "1m", "--resource", "resource"},
		{"worklease", "acquire", "--resource", "resource", "-w", "old-work-key"},
	}
	for _, args := range removed {
		var stdout, stderr bytes.Buffer
		if err := Run(context.Background(), args, "dev", "unknown", "unknown", &stdout, &stderr); err == nil {
			t.Errorf("%v: removed alias was accepted", args)
		}
	}
}

func TestRunHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, []string{"worklease", "version"}, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
}

func TestExclusiveSelectionAndResourceBoundaries(t *testing.T) {
	revision, fd := int64(1), 3
	if _, err := Select(SelectionInput{Handle: "a", Lease: "b"}, true); reason.As(err).Reason != reason.ReasonCredentialSourceConflict {
		t.Fatalf("handle/lease conflict = %v", err)
	}
	if _, err := Select(SelectionInput{ClaimID: "id", TokenFile: "token", TokenFD: &fd, Revision: &revision}, true); reason.As(err).Reason != reason.ReasonCredentialSourceConflict {
		t.Fatalf("token conflict = %v", err)
	}
	if _, err := Select(SelectionInput{ClaimID: "id", TokenFile: "token"}, true); reason.As(err).Reason != reason.ReasonInvalidArgument {
		t.Fatalf("missing revision = %v", err)
	}
	if selected, err := Select(SelectionInput{Session: "stable"}, true); err != nil || selected.Mode != "context" || selected.Session != "stable" {
		t.Fatalf("context selection = %#v, %v", selected, err)
	}
}

func TestEveryOptionHasOperationalHelpAndNoSentinelDefaults(t *testing.T) {
	root := NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	checked := 0
	for _, entry := range commandTree(root) {
		for _, flag := range entry.command.VisibleFlags() {
			name := flag.Names()[0]
			if name == "help" {
				continue
			}
			rendered := flag.String()
			usage := strings.TrimSpace(rendered[strings.Index(rendered, "\t")+1:])
			path := strings.Join(entry.path, " ")
			if usage == "" || strings.EqualFold(usage, name) || strings.EqualFold(usage, strings.ReplaceAll(name, "-", " ")) {
				t.Errorf("%s --%s has no operational help: %q", path, name, rendered)
			}
			if strings.Contains(rendered, "(default: 0") || strings.Contains(rendered, "`") {
				t.Errorf("%s --%s exposes a sentinel default or raw placeholder quote: %q", path, name, rendered)
			}
			checked++
		}
	}
	if checked < 100 {
		t.Fatalf("checked only %d flags", checked)
	}
	root = NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	acquire := root.Command("acquire")
	for _, want := range []string{"--ttl DURATION, -t DURATION\tclaim lifetime DURATION [$WORKLEASE_TTL] (default: 15m)", "--wait DURATION, -w DURATION\twait up to DURATION for a contended resource instead of failing immediately", "--session NAME, -s NAME\tsession NAME that keeps concurrent loops apart [$WORKLEASE_SESSION_ID]", "--poll-interval DURATION\tDURATION between contention polls while waiting [$WORKLEASE_POLL_INTERVAL] (default: 250ms)", "--agent NAME, -a NAME\tagent identity NAME [$WORKLEASE_AGENT_ID] (default: login user)"} {
		found := false
		for _, flag := range acquire.Flags {
			if flag.String() == want {
				found = true
			}
		}
		if !found {
			t.Errorf("acquire help lacks %q", want)
		}
	}
	for path, want := range map[string]string{"exec": "(default: 1h)", "release": "(default: released)", "watch": "(default: 30s)", "events": "(default: 50)", "gc": "(default: 30)"} {
		found := false
		for _, flag := range root.Command(path).Flags {
			if strings.Contains(flag.String(), want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s help lacks effective default %q", path, want)
		}
	}
}

func TestUsageLinesShowPositionalsAndAlternateForms(t *testing.T) {
	root := NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	find := func(path string) *urfavecli.Command {
		command := root
		for _, name := range strings.Fields(path) {
			command = command.Command(name)
		}
		return command
	}
	for path, wants := range map[string][]string{
		"exec":            {"-- COMMAND [ARGS...]", "[selection]"},
		"acquire":         {"(--path FILE | --resource KEY... | --provider NAME --source SOURCE --item ITEM)", "--no-handle --claim-id ID"},
		"key":             {"(--path FILE | --resource KEY... | --provider NAME --source SOURCE --item ITEM)"},
		"policy describe": {"worklease policy describe NAME"},
		"op inspect":      {"(--operation-id ID | --claim-id ID | --resource KEY | [selection])", "--full [selection]"},
		"history":         {"worklease history [--limit N] [--full]\nworklease history --resource KEY"},
		"verify":          {"worklease verify [--resource KEY...] [selection]", "--hook claude-code [--coverage claim|path]"},
		"checkpoint":      {"(--data JSON | --data-file FILE)"},
		"watch":           {"--until (free|change)", "worklease watch --cursor CURSOR"},
		"transfer":        {"--successor-handle PATH"},
		"help":            {"worklease help [COMMAND [SUBCOMMAND]]\nworklease help --all"},
	} {
		command := find(path)
		for _, want := range wants {
			if !strings.Contains(command.UsageText, want) {
				t.Errorf("%s usage %q lacks %q", path, command.UsageText, want)
			}
		}
	}
	for _, entry := range commandTree(root) {
		if strings.Contains(entry.command.UsageText, "[selection]") && !strings.Contains(entry.command.Description, "Selection:") {
			t.Errorf("%s uses [selection] without explaining it", strings.Join(entry.path, " "))
		}
	}
}

func TestRootHelpGroupsCommandsInWorkflowOrder(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--help"}, "dev", "unknown", "unknown", &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	text := stdout.String()
	lifecycle, inspection, admin := strings.Index(text, "Claim lifecycle:"), strings.Index(text, "Inspection and recovery:"), strings.Index(text, "Setup and administration:")
	if lifecycle < 0 || inspection < lifecycle || admin < inspection {
		t.Fatalf("categories are missing or out of order: %q", text)
	}
	within := func(name string, start, end int) bool {
		index := strings.Index(text, "\n     "+name+" ")
		return index > start && (end < 0 || index < end)
	}
	if !within("acquire", lifecycle, inspection) || !within("replace-file", lifecycle, inspection) || !within("history", inspection, admin) || !within("doctor", inspection, admin) || !within("setup", admin, -1) || !within("help, h", admin, -1) {
		t.Fatalf("commands are not grouped as expected: %q", text)
	}
	if strings.Contains(text, "\x1b") {
		t.Fatal("root help contains ANSI sequences")
	}
}

func TestHelpAllCoversEveryCommandOnceReadOnly(t *testing.T) {
	home := t.TempDir() + "/never-created"
	render := func() string {
		var stdout, stderr bytes.Buffer
		if err := Run(context.Background(), []string{"worklease", "--home", home, "help", "--all"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr=%q", stderr.String())
		}
		return stdout.String()
	}
	first := render()
	if first != render() {
		t.Fatal("aggregate help is not deterministic")
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("help --all touched the authority home: %v", err)
	}
	if strings.Contains(first, "\x1b") {
		t.Fatal("aggregate help contains ANSI sequences")
	}
	root := NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	if err := root.Run(context.Background(), []string{"worklease", "--help"}); err != nil {
		t.Fatal(err)
	}
	sections := 0
	for _, entry := range commandTree(root) {
		if entry.command.Name == "help" {
			continue
		}
		header := "NAME:\n   " + strings.Join(entry.path, " ") + " - " + entry.command.Usage + "\n"
		if count := strings.Count(first, header); count != 1 {
			t.Errorf("%q appears %d times", header, count)
		}
		sections++
	}
	if got := strings.Count(first, "\nNAME:\n"); got != sections {
		t.Fatalf("aggregate help has %d command sections, want %d", got, sections)
	}
	if !strings.HasPrefix(first, "NAME:\n   worklease - ") {
		t.Fatalf("aggregate help does not start with root help: %q", first[:80])
	}
}

func TestHelpCommandResolvesNestedPathsAndReportsUnknownInJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "help", "op", "inspect"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout.String(), "NAME:\n   worklease op inspect - inspect an operation\n") || !strings.Contains(stdout.String(), "worklease op inspect (--operation-id ID") {
		t.Fatalf("nested help=%q", stdout.String())
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"worklease", "help", "policy"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "COMMANDS:") || !strings.Contains(stdout.String(), "describe") {
		t.Fatalf("group help=%q", stdout.String())
	}
	stdout.Reset()
	err := Run(context.Background(), []string{"worklease", "help", "bogus", "--json"}, "dev", "unknown", "unknown", &stdout, &stderr)
	if err == nil {
		t.Fatal("expected unknown command error")
	}
	var envelope map[string]any
	if decodeErr := json.Unmarshal(stdout.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("%v (%q)", decodeErr, stdout.String())
	}
	if envelope["ok"] != false || envelope["operation"] != "help" || reason.As(err).ExitCode() != reason.ExitInvalid {
		t.Fatalf("envelope=%#v err=%v", envelope, err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
}
