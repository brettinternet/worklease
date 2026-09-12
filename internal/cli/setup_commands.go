package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/brettinternet/worklease/internal/output"
	setupgen "github.com/brettinternet/worklease/internal/setup"
	urfave "github.com/urfave/cli/v3"
)

func setupAction(s *boundary, kind string) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		binary, err := os.Executable()
		if err != nil {
			return s.handle(cmd, err)
		}
		binary, err = filepath.Abs(binary)
		if err != nil {
			return s.handle(cmd, err)
		}
		cwd, err := os.Getwd()
		if err != nil {
			return s.handle(cmd, err)
		}
		userHome, err := os.UserHomeDir()
		if err != nil {
			return s.handle(cmd, err)
		}
		if kind == "guard" {
			if _, selectionErr := Select(SelectionInput{Handle: explicitValue(cmd, "handle", "WORKLEASE_HANDLE"), Lease: strings.TrimSpace(cmd.String("lease")), Session: explicitValue(cmd, "session", "WORKLEASE_SESSION_ID")}, false); selectionErr != nil {
				return s.handle(cmd, selectionErr)
			}
		}
		result, err := setupgen.Run(setupgen.Options{
			Kind: kind, Client: cmd.String("client"), Scope: cmd.String("scope"), Coverage: cmd.String("coverage"),
			Apply: cmd.Bool("apply"), Remove: cmd.Bool("remove"), Binary: binary, ProjectDir: cwd, UserHome: userHome,
			Home: explicitValue(cmd, "home", "WORKLEASE_HOME"), Config: explicitValue(cmd, "config", "WORKLEASE_CONFIG"),
			Session: explicitValue(cmd, "session", "WORKLEASE_SESSION_ID"), Handle: explicitValue(cmd, "handle", "WORKLEASE_HANDLE"),
			Lease: strings.TrimSpace(cmd.String("lease")), Agent: strings.TrimSpace(cmd.String("agent")), Version: s.version,
		})
		if err != nil {
			return s.handle(cmd, err)
		}
		if s.jsonRequested(cmd) {
			return output.WriteSuccess(s.writer, "setup-"+kind, map[string]any{"target": result.Target, "preview": result.Preview, "changed": result.Changed, "applied": result.Applied, "removed": result.Removed})
		}
		_, err = s.writer.Write([]byte(result.Preview))
		return err
	}
}

func explicitValue(cmd *urfave.Command, flag, environment string) string {
	if cmd.IsSet(flag) {
		return strings.TrimSpace(cmd.String(flag))
	}
	return strings.TrimSpace(os.Getenv(environment))
}
