package mcp

import (
	"context"

	"github.com/brettinternet/worklease/internal/config"

	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
)

// queueNext delegates snapshot selection and fresh candidate validation to the
// CLI queue core, while acquisition uses this server's ordinary MCP lease path.
func (s *Server) queueNext(ctx context.Context, a map[string]any) (any, error) {
	if s.options.QueueNext == nil {
		return nil, reason.New(reason.ReasonConfigInvalid, "queue integration is unavailable")
	}
	claim, err := argBool(a, "claim", false)
	if err != nil {
		return nil, err
	}
	start, err := argBool(a, "start", false)
	if err != nil {
		return nil, err
	}
	allProjects, err := argBool(a, "allProjects", false)
	if err != nil {
		return nil, err
	}
	if start && !claim {
		return nil, reason.Invalid("start requires claim")
	}
	session := valueString(a, "sessionId")
	if session == "" {
		session = s.options.SessionID
	}
	ttl, err := argNumber(a, "ttl", s.options.TTL.Seconds())
	if err != nil || ttl <= 0 || ttl > 3600 {
		return nil, reason.Invalid("ttl must be between 1s and 1h")
	}
	authorityID, profile := "", ""
	if claim {
		bundle, err := s.open(ctx, false)
		if err != nil {
			return nil, err
		}
		authorityID = bundle.id
		bundle.Close()
		profile = s.options.ProfileName
		if s.profile != nil && profile == "" {
			profile = s.profile.Name
		}
		if profile == "" {
			profile = config.LocalProfileName
		}
	}
	result, err := s.options.QueueNext(ctx, valueString(a, "view"), allProjects, claim, start, session, ttl, authorityID, profile, s.profile, func(ctx context.Context, targetID, targetProfile string, target *config.Profile, resources []string) (map[string]any, error) {
		bundle, err := s.openNamedAuthority(ctx, targetProfile, target, true)
		if err != nil {
			return nil, err
		}
		defer bundle.Close()
		if bundle.id != targetID {
			return nil, reason.New(reason.ReasonAuthorityMismatch, "source claim authority changed before acquisition")
		}
		acquire := map[string]any{"resources": resources, "sessionId": session, "ttl": ttl, "coordinationOnly": true, "wait": float64(0)}
		for _, key := range []string{"agentId", "maxHold", "autoHeartbeat"} {
			if value, ok := a[key]; ok {
				acquire[key] = value
			}
		}
		value, err := s.acquireWithBundle(ctx, acquire, targetID, bundle, resources)
		if err != nil {
			return nil, err
		}
		grant, ok := value.(map[string]any)
		if !ok {
			return nil, reason.New(reason.ReasonUnknownOutcome, "acquire receipt is invalid")
		}
		return grant, nil
	}, s.handlePath)
	if err != nil {
		return nil, err
	}
	// The CLI's generated resource keys are public claim identities, not bearer
	// tokens. Preserve their exact digests through MCP's public projection so
	// read-only candidates remain usable for manual acquisition.
	if next, ok := result["next"].(map[string]any); ok {
		if candidates, ok := next["candidates"].([]any); ok {
			for _, candidate := range candidates {
				row, ok := candidate.(map[string]any)
				if !ok {
					continue
				}
				if resources, ok := row["resources"].([]any); ok {
					for index, value := range resources {
						if key, ok := value.(string); ok {
							resources[index] = output.PublicDigest(key)
						}
					}
				}
			}
		}
	}
	return result, nil
}
