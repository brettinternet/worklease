package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/reason"
)

// mcpQueueNext invokes the same unpaginated selector and candidate revalidation
// as the CLI. Only the final acquire is substituted with MCP's private lease.
func mcpQueueNext(home, profile string) func(context.Context, string, bool, string, float64, string, string, *config.Profile, func(context.Context, []string) (map[string]any, error)) (map[string]any, error) {
	return func(ctx context.Context, view string, claim bool, session string, ttl float64, authorityID, authorityProfile string, pinnedProfile *config.Profile, acquire func(context.Context, []string) (map[string]any, error)) (map[string]any, error) {
		args := []string{"worklease", "--json", "--home", home}
		if profile != "" {
			args = append(args, "--profile", profile)
		}
		args = append(args, "queue", "next", "--view", view)
		if claim {
			args = append(args, "--claim", "--session", session, "--ttl", time.Duration(ttl*float64(time.Second)).String())
			ctx = context.WithValue(ctx, queueMCPAcquireKey{}, queueMCPAcquire{authorityID: authorityID, profile: authorityProfile, pinnedProfile: pinnedProfile, acquire: acquire})
		}
		var result bytes.Buffer
		err := Run(ctx, args, "", "", "", &result, &result)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			Next map[string]any `json:"next"`
		}
		if json.Unmarshal(result.Bytes(), &envelope) != nil || envelope.Next == nil {
			return nil, reason.New(reason.ReasonUnknownOutcome, "queue next result could not be decoded")
		}
		return map[string]any{"next": envelope.Next}, nil
	}
}
