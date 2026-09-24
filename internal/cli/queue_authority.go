package cli

import (
	"context"
	"fmt"

	"github.com/brettinternet/worklease/internal/output"
	urfave "github.com/urfave/cli/v3"
)

// queue authority-id resolves the invoking worker's authority, not the view's.
// Launchers compare it to the queue's handoff before trying to acquire.
func queueAuthorityIDCommand(s *boundary) *urfave.Command {
	return &urfave.Command{Name: "authority-id", Usage: "show the invoking worker's selected authority ID", Action: func(ctx context.Context, cmd *urfave.Command) error {
		backend, err := authorityFor(ctx, cmd, false)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer backend.Close()
		if backend.AuthorityID() == "" {
			return s.handle(cmd, fmt.Errorf("selected authority is not initialized"))
		}
		if cmd.Bool("json") {
			return output.WriteSuccess(s.writer, "queue-authority-id", map[string]any{"authorityId": backend.AuthorityID(), "profile": backend.ProfileName})
		}
		_, err = fmt.Fprintln(s.writer, backend.AuthorityID())
		return err
	}}
}
