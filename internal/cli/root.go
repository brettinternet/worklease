// Package cli builds Worklease's command tree and keeps output boundaries at
// the process edge. Lease behavior is intentionally owned by later tasks.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	urfavecli "github.com/urfave/cli/v3"
)

const jsonStateKey = "worklease.json.state"

type handledError struct{ cause error }

func (e *handledError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}
func (e *handledError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}
func (e *handledError) ExitCode() int {
	if e == nil || e.cause == nil {
		return reason.ExitInternal
	}
	if classified := reason.As(e.cause); classified != nil {
		return classified.ExitCode()
	}
	return reason.ExitInternal
}

// JSONErrorHandled reports whether an error has already been rendered by the
// command's injected output boundary.
func JSONErrorHandled(err error) bool {
	var handled *handledError
	return errors.As(err, &handled)
}

type boundary struct {
	root       *urfavecli.Command
	writer     io.Writer
	errWriter  io.Writer
	version    string
	commit     string
	buildTime  string
	mu         sync.Mutex
	invocation []string
	handled    bool
}

// SetInvocationArgs supplies raw arguments for robust JSON detection when the
// parser has consumed a positional payload or rejected an option.
func SetInvocationArgs(root *urfavecli.Command, args []string) {
	if root == nil || root.Metadata == nil {
		return
	}
	if state, ok := root.Metadata[jsonStateKey].(*boundary); ok {
		state.mu.Lock()
		state.invocation = append([]string(nil), args...)
		state.mu.Unlock()
	}
}

// NewRootCommand creates a signal-aware-friendly command tree. The command
// itself never exits and all writers are injected for in-process tests.
func NewRootCommand(version, commit, buildTime string, stdout, stderr io.Writer) *urfavecli.Command {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	state := &boundary{writer: stdout, errWriter: stderr, version: version, commit: commit, buildTime: buildTime}
	root := &urfavecli.Command{
		Name: "worklease", Usage: "Coordinate local work ownership", UsageText: "worklease [global options] <command>",
		Description: "Coordinate work safely on one host. Start with `worklease acquire --path README.md`, then `worklease status` and `worklease release`. Commands that act on a claim accept the shared [selection] options described in each command's help. Run `worklease help --all` to read the whole interface once.\n\nExamples:\n  worklease acquire --path README.md\n  worklease help --all\n  worklease version --json",
		Version:     version, HideVersion: true, Writer: stdout, ErrWriter: stderr,
		EnableShellCompletion: true, ShellComplete: writeShellCompletions,
		ConfigureShellCompletionCommand: configureCompletionCommand(state),
		Metadata:                        map[string]any{jsonStateKey: state}, ExitErrHandler: func(context.Context, *urfavecli.Command, error) {},
		Flags: []urfavecli.Flag{
			&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "output one JSON envelope"},
			&urfavecli.StringFlag{Name: "home", Aliases: []string{"H"}, Usage: "state directory [$WORKLEASE_HOME]"},
			&urfavecli.StringFlag{Name: "config", Usage: "configuration file [$WORKLEASE_CONFIG]"},
			&urfavecli.StringFlag{Name: "profile", Usage: "trusted remote authority profile `NAME` [$WORKLEASE_PROFILE]"},
			&urfavecli.BoolFlag{Name: "local", Usage: "use the local authority even when a default or project profile is configured"},
			&urfavecli.BoolFlag{Name: "version", Aliases: []string{"v"}, Usage: "show version", Local: true},
		},
		Commands: newCommands(state),
		OnUsageError: func(ctx context.Context, cmd *urfavecli.Command, err error, _ bool) error {
			return state.handle(cmd, reason.New(reason.ReasonInvalidArgument, err.Error()))
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if cmd.Bool("version") {
				return state.versionResult(cmd)
			}
			if cmd.Args().Len() > 0 {
				return state.handle(cmd, reason.Invalid(fmt.Sprintf("unknown command %q", cmd.Args().First())))
			}
			return urfavecli.ShowRootCommandHelp(cmd)
		},
	}
	state.root = root
	setShellCompletionHandlers(root)
	if err := validateCLICommandTree(root); err != nil {
		panic(err)
	}
	return root
}

func (s *boundary) jsonRequested(cmd *urfavecli.Command) bool {
	if cmd != nil && cmd.Bool("json") {
		return true
	}
	s.mu.Lock()
	args := append([]string(nil), s.invocation...)
	s.mu.Unlock()
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--json" || arg == "-j" || strings.HasPrefix(arg, "--json=") {
			return true
		}
	}
	return false
}

func (s *boundary) operation(cmd *urfavecli.Command) string {
	if cmd != nil && cmd.Name != "" && cmd.Name != "worklease" {
		return cmd.Name
	}
	return "worklease"
}

func (s *boundary) handle(cmd *urfavecli.Command, err error) error {
	if err == nil || JSONErrorHandled(err) {
		return err
	}
	if !s.jsonRequested(cmd) {
		return err
	}
	s.mu.Lock()
	if s.handled {
		s.mu.Unlock()
		return &handledError{cause: err}
	}
	s.handled = true
	s.mu.Unlock()
	_ = output.WriteError(s.writer, s.operation(cmd), err)
	return &handledError{cause: err}
}

func (s *boundary) versionResult(cmd *urfavecli.Command) error {
	fields := map[string]any{"version": s.version, "commit": s.commit, "buildTime": s.buildTime, "goVersion": runtime.Version()}
	if s.jsonRequested(cmd) {
		return output.WriteSuccess(s.writer, "version", fields)
	}
	details := make([]string, 0, 2)
	if s.commit != "" && s.commit != "unknown" {
		commit := s.commit
		if len(commit) > 7 {
			commit = commit[:7]
		}
		details = append(details, commit)
	}
	if s.buildTime != "" && s.buildTime != "unknown" {
		details = append(details, "built "+s.buildTime)
	}
	if len(details) == 0 {
		_, err := fmt.Fprintf(s.writer, "worklease %s\n", s.version)
		return err
	}
	_, err := fmt.Fprintf(s.writer, "worklease %s (%s)\n", s.version, strings.Join(details, ", "))
	return err
}

// Run is the process entry point used by cmd/worklease and tests.
func Run(ctx context.Context, args []string, version, commit, buildTime string, stdout, stderr io.Writer) error {
	root := NewRootCommand(version, commit, buildTime, stdout, stderr)
	for _, arg := range args {
		if !utf8.ValidString(arg) {
			err := reason.Invalid("arguments must be valid UTF-8")
			if hasJSON(args) {
				_ = output.WriteError(stdout, "worklease", err)
				return &handledError{cause: err}
			}
			return err
		}
	}
	args = safeCompletionArgs(args)
	SetInvocationArgs(root, args)
	return root.Run(ctx, args)
}

// safeCompletionArgs keeps the framework's internal completion marker from
// becoming an argument to a command after a positional `--` separator. Shell
// completion must never dispatch the command being completed.
func safeCompletionArgs(args []string) []string {
	marker := len(args) - 1
	if marker < 0 || args[marker] != "--generate-shell-completion" {
		return args
	}
	for index := 1; index < marker-1; index++ {
		if args[index] == "--" {
			safe := append([]string(nil), args[:index]...)
			return append(safe, "--generate-shell-completion")
		}
	}
	return args
}

func hasJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" || arg == "-j" || strings.HasPrefix(arg, "--json=") {
			return true
		}
	}
	return false
}

func validateCLICommandTree(root *urfavecli.Command) error {
	shortMeanings := make(map[string]string)
	var visit func(*urfavecli.Command, map[string]string) error
	visit = func(cmd *urfavecli.Command, inherited map[string]string) error {
		seen := make(map[string]string, len(inherited))
		for k, v := range inherited {
			seen[k] = v
		}
		next := make(map[string]string, len(inherited))
		for k, v := range inherited {
			next[k] = v
		}
		for _, flag := range cmd.Flags {
			if flag == nil {
				return fmt.Errorf("nil flag in %s", cmd.Name)
			}
			names := flag.Names()
			if len(names) == 0 {
				return fmt.Errorf("unnamed flag in %s", cmd.Name)
			}
			if doc, ok := flag.(urfavecli.DocGenerationFlag); ok && names[0] != "help" {
				usage := strings.ReplaceAll(strings.TrimSpace(doc.GetUsage()), "`", "")
				if usage == "" || strings.EqualFold(usage, names[0]) || strings.EqualFold(usage, strings.ReplaceAll(names[0], "-", " ")) {
					return fmt.Errorf("flag --%s in %s has no operational help", names[0], cmd.Name)
				}
				if rendered := flag.String(); strings.Contains(rendered, "(default: 0") {
					return fmt.Errorf("flag --%s in %s exposes a zero-value sentinel default", names[0], cmd.Name)
				}
			}
			for _, name := range names {
				if previous, ok := seen[name]; ok {
					return fmt.Errorf("flag %q used by %s and %s", name, previous, cmd.Name)
				}
				seen[name] = cmd.Name
				// Local flags belong only to this command, so descendants may reuse their aliases.
				if local, ok := flag.(interface{ IsLocal() bool }); ok && local.IsLocal() {
					continue
				}
				next[name] = cmd.Name
				if len(name) == 1 {
					if meaning, ok := shortMeanings[name]; ok && meaning != names[0] {
						return fmt.Errorf("short flag %q means both %s and %s", name, meaning, names[0])
					}
					shortMeanings[name] = names[0]
				}
			}
		}
		for _, child := range cmd.Commands {
			if err := visit(child, next); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(root, nil)
}
