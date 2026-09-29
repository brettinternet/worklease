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
func mcpQueueNext(home, profile string, pinnedCheckout ...string) func(context.Context, string, bool, bool, bool, string, float64, string, string, *config.Profile, func(context.Context, string, string, *config.Profile, []string) (map[string]any, error), func(string) string) (map[string]any, error) {
	checkout := ""
	if len(pinnedCheckout) > 0 {
		checkout = pinnedCheckout[0]
	}
	return func(ctx context.Context, view string, allProjects, claim, start bool, session string, ttl float64, authorityID, authorityProfile string, pinnedProfile *config.Profile, acquire func(context.Context, string, string, *config.Profile, []string) (map[string]any, error), handlePath func(string) string) (map[string]any, error) {
		if checkout != "" {
			ctx = context.WithValue(ctx, queueProjectRootKey{}, checkout)
		}
		args := []string{"worklease", "--json", "--home", home}
		if profile != "" {
			args = append(args, "--profile", profile)
		}
		args = append(args, "queue", "next", "--view", view)
		if allProjects {
			args = append(args, "--all-projects")
		}
		if claim {
			args = append(args, "--claim", "--session", session, "--ttl", time.Duration(ttl*float64(time.Second)).String())
			ctx = context.WithValue(ctx, queueMCPAcquireKey{}, queueMCPAcquire{authorityID: authorityID, profile: authorityProfile, pinnedProfile: pinnedProfile, acquire: acquire, handlePath: func(grant map[string]any) string {
				ref, _ := grant["lease"].(string)
				if ref == "" {
					return ""
				}
				return handlePath(ref)
			}})
		}
		if start {
			args = append(args, "--start")
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
