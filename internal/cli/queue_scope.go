package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
)

type queueProjectRootKey struct{}

type queueScope struct {
	Kind     string `json:"kind"`
	Checkout string `json:"checkout,omitempty"`
}

func (s queueScope) label() string {
	if s.Kind == "project" {
		return "project " + s.Checkout
	}
	return "all projects"
}

func queueWorkingDirectory(ctx context.Context) string {
	if pinned, ok := ctx.Value(queueProjectRootKey{}).(string); ok && pinned != "" {
		return pinned
	}
	cwd, _ := os.Getwd()
	return cwd
}

// queueProjectSources resolves checkout ownership through BindingRoots so a
// linked worktree belongs to its main checkout. Sources without a repository
// checkout or matching GitHub origin need an owner-private projectCheckout.
func queueProjectSources(ctx context.Context, cfg config.QueueConfig, allProjects bool) (queueScope, []config.QueueSource, error) {
	if allProjects {
		return queueScope{Kind: "all-projects"}, append([]config.QueueSource(nil), cfg.Sources...), nil
	}
	cwd := queueWorkingDirectory(ctx)
	_, projectRoot, err := handle.BindingRoots(cwd, nil)
	if err != nil {
		return queueScope{}, nil, err
	}
	projectRoot, err = filepath.Abs(projectRoot)
	if err != nil {
		return queueScope{}, nil, err
	}
	origin := ""
	if remote, remoteErr := queueInitCommandOutput(ctx, projectRoot, "git", "remote", "get-url", "origin"); remoteErr == nil {
		origin = remote
	}
	originHost, originRepository, hasOrigin := queueInitRemote(origin)

	matched := make(map[string]bool)
	for _, source := range cfg.Sources {
		if source.Adapter == "backlog-md" || source.Adapter == "beads" {
			if root, ok := queueCheckoutRoot(source.Checkout); ok && root == projectRoot {
				matched[source.ID] = true
			}
		}
		if source.ProjectCheckout != "" {
			if root, ok := queueCheckoutRoot(source.ProjectCheckout); ok && root == projectRoot {
				matched[source.ID] = true
			}
		}
		if source.Adapter == "github" && hasOrigin && strings.EqualFold(source.Host, originHost) && strings.EqualFold(source.Repository, originRepository) {
			matched[source.ID] = true
		}
	}
	if len(matched) == 0 {
		return queueScope{Kind: "all-projects"}, append([]config.QueueSource(nil), cfg.Sources...), nil
	}
	sources := make([]config.QueueSource, 0, len(matched))
	for _, source := range cfg.Sources {
		if matched[source.ID] {
			sources = append(sources, source)
		}
	}
	return queueScope{Kind: "project", Checkout: projectRoot}, sources, nil
}

func queueCheckoutRoot(checkout string) (string, bool) {
	if checkout == "" {
		return "", false
	}
	_, root, err := handle.BindingRoots(checkout, nil)
	if err != nil {
		return "", false
	}
	return root, true
}

// queueViewSourceIDs intersects the view's optional legacy source restriction
// with the effective project scope. An empty list means filter-only view.
func queueViewSourceIDs(view config.QueueView, scoped []config.QueueSource) []string {
	allowed := make(map[string]bool, len(scoped))
	for _, source := range scoped {
		allowed[source.ID] = true
	}
	ids := make([]string, 0, len(scoped))
	if len(view.Sources) == 0 {
		for _, source := range scoped {
			ids = append(ids, source.ID)
		}
		return ids
	}
	for _, id := range view.Sources {
		if allowed[id] {
			ids = append(ids, id)
		}
	}
	return ids
}

func queueSourcesByIDs(cfg config.QueueConfig, ids []string) []config.QueueSource {
	byID := make(map[string]config.QueueSource, len(cfg.Sources))
	for _, source := range cfg.Sources {
		byID[source.ID] = source
	}
	result := make([]config.QueueSource, 0, len(ids))
	for _, id := range ids {
		if source, ok := byID[id]; ok {
			result = append(result, source)
		}
	}
	return result
}
