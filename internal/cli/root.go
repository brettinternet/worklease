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
		Description: "Coordinate work safely on one host. Start with `worklease acquire --path README.md`, then `worklease status` and `worklease release`.\n\nExamples:\n  worklease acquire --path README.md\n  worklease version --json",
		Version:     version, HideVersion: true, Writer: stdout, ErrWriter: stderr,
		Metadata: map[string]any{jsonStateKey: state}, ExitErrHandler: func(context.Context, *urfavecli.Command, error) {},
		Flags: []urfavecli.Flag{
			&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "output one JSON envelope [$WORKLEASE_JSON]"},
			&urfavecli.StringFlag{Name: "home", Aliases: []string{"H"}, Usage: "state directory [$WORKLEASE_HOME]"},
			&urfavecli.StringFlag{Name: "config", Usage: "configuration file [$WORKLEASE_CONFIG]"},
			&urfavecli.BoolFlag{Name: "version", Aliases: []string{"v"}, Usage: "show version metadata"},
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
	_, err := fmt.Fprintf(s.writer, "worklease %s\ncommit: %s\nbuildTime: %s\ngoVersion: %s\nschemaVersion: %d\n", s.version, s.commit, s.buildTime, runtime.Version(), output.SchemaVersion)
	return err
}

// Run is the process entry point used by cmd/worklease and tests.
func Run(ctx context.Context, args []string, version, commit, buildTime string, stdout, stderr io.Writer) error {
	root := NewRootCommand(version, commit, buildTime, stdout, stderr)
	SetInvocationArgs(root, args)
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
	return root.Run(ctx, args)
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
		for _, flag := range cmd.Flags {
			if flag == nil {
				return fmt.Errorf("nil flag in %s", cmd.Name)
			}
			names := flag.Names()
			if len(names) == 0 {
				return fmt.Errorf("unnamed flag in %s", cmd.Name)
			}
			for _, name := range names {
				if previous, ok := seen[name]; ok {
					return fmt.Errorf("flag %q used by %s and %s", name, previous, cmd.Name)
				}
				seen[name] = cmd.Name
				if len(name) == 1 {
					if meaning, ok := shortMeanings[name]; ok && meaning != names[0] {
						return fmt.Errorf("short flag %q means both %s and %s", name, meaning, names[0])
					}
					shortMeanings[name] = names[0]
				}
			}
		}
		next := make(map[string]string, len(seen))
		for k, v := range seen {
			next[k] = v
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
