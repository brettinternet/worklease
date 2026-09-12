package cli

import (
	"os"
	"strings"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
	urfave "github.com/urfave/cli/v3"
)

// ResourceInput is the shared normalized resource selection used by key and
// acquire. A direct resource may contain any opaque bytes accepted by policy.
type ResourceInput struct{ Keys []resource.Key }

// ResolveResourceInput enforces the exactly-one resource addressing mode and
// derives all keys before a caller can dispatch a mutation.
func ResolveResourceInput(cmd *urfave.Command) (ResourceInput, error) {
	direct := cmd.StringSlice("resource")
	provider, source, item := cmd.String("provider"), cmd.String("source"), cmd.String("item")
	path := cmd.String("path")
	tripleSet := cmd.IsSet("provider") || cmd.IsSet("source") || cmd.IsSet("item")
	pathSet := cmd.IsSet("path") || strings.TrimSpace(path) != ""
	modes := 0
	if len(direct) > 0 {
		modes++
	}
	if tripleSet {
		modes++
	}
	if pathSet {
		modes++
	}
	if modes == 0 {
		return ResourceInput{}, reason.New(reason.ReasonInvalidResource, "one resource input is required")
	}
	if modes > 1 {
		return ResourceInput{}, reason.New(reason.ReasonResourceInputConflict, "resource input modes are exclusive")
	}
	coordination := cmd.Bool("coordination-only")
	keys := make([]resource.Key, 0, len(direct))
	if len(direct) > 0 {
		seen := map[string]bool{}
		for _, raw := range direct {
			key, err := resource.Direct(raw, coordination)
			if err != nil {
				return ResourceInput{}, err
			}
			if seen[key.Resource] {
				return ResourceInput{}, reason.New(reason.ReasonInvalidResource, "duplicate resource input")
			}
			seen[key.Resource] = true
			keys = append(keys, key)
		}
		if len(keys) > 32 {
			return ResourceInput{}, reason.New(reason.ReasonInvalidResource, "at most 32 resources may be acquired")
		}
		return ResourceInput{Keys: keys}, nil
	}
	if tripleSet {
		if strings.TrimSpace(provider) == "" || strings.TrimSpace(source) == "" || strings.TrimSpace(item) == "" {
			return ResourceInput{}, reason.New(reason.ReasonResourceInputConflict, "provider input requires provider, source, and item")
		}
		key, err := resource.Resolve(resource.Input{Provider: provider, Source: source, Item: item, CoordinationOnly: coordination})
		if err != nil {
			return ResourceInput{}, err
		}
		return ResourceInput{Keys: []resource.Key{key}}, nil
	}
	cwd, _ := os.Getwd()
	key, err := resource.Resolve(resource.Input{Path: path, CoordinationOnly: coordination, WorkingDir: cwd})
	if err != nil {
		return ResourceInput{}, err
	}
	return ResourceInput{Keys: []resource.Key{key}}, nil
}

// ValidateResourceInput is retained as a cheap validation entry point for
// callers that only need to reject malformed input before mutation.
func ValidateResourceInput(cmd *urfave.Command) error {
	_, err := ResolveResourceInput(cmd)
	return err
}
