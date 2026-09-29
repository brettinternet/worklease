package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestQueueProjectSourcesMatchLinkedCheckoutOriginAndPrivateAssociations(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := testkit.GitCommand("init", "-b", "main", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := testkit.GitCommand("-C", root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
	if out, err := testkit.GitCommand("-C", root, "remote", "add", "origin", "git@github.com:Acme/Repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	if out, err := testkit.GitCommand("-C", root, "worktree", "add", "--detach", linked, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, out)
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := config.QueueConfig{Sources: []config.QueueSource{
		{ID: "backlog", Adapter: "backlog-md", Checkout: root},
		{ID: "beads", Adapter: "beads", Checkout: root},
		{ID: "github", Adapter: "github", Host: "github.com", Repository: "acme/repo"},
		{ID: "linear", Adapter: "linear", ProjectCheckout: root},
		{ID: "external", Adapter: "external", ProjectCheckout: root},
		{ID: "orphan", Adapter: "external"},
		{ID: "elsewhere", Adapter: "backlog-md", Checkout: other},
	}}
	ctx := context.WithValue(context.Background(), queueProjectRootKey{}, linked)
	scope, sources, err := queueProjectSources(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	_, expectedRoot, err := handle.BindingRoots(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.ID)
	}
	if scope.Kind != "project" || scope.Checkout != expectedRoot || !reflect.DeepEqual(ids, []string{"backlog", "beads", "github", "linear", "external"}) {
		t.Fatalf("linked-worktree project scope: scope=%+v sources=%v", scope, ids)
	}
	if got := queueViewSourceIDs(config.QueueView{Name: "All"}, sources); !reflect.DeepEqual(got, ids) {
		t.Fatalf("filter-only view sources = %v, want %v", got, ids)
	}
	if got := queueViewSourceIDs(config.QueueView{Name: "Legacy", Sources: []string{"elsewhere", "github", "orphan"}}, sources); !reflect.DeepEqual(got, []string{"github"}) {
		t.Fatalf("legacy restriction escaped scope intersection: %v", got)
	}
	allScope, allSources, err := queueProjectSources(ctx, cfg, true)
	if err != nil || allScope.Kind != "all-projects" || len(allSources) != len(cfg.Sources) {
		t.Fatalf("explicit cross-project scope: %+v %v %v", allScope, allSources, err)
	}
}

func TestQueueProjectSourcesDefaultToAllOutsideConfiguredProject(t *testing.T) {
	t.Parallel()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := config.QueueConfig{Sources: []config.QueueSource{{ID: "project", Adapter: "backlog-md", Checkout: filepath.Join(t.TempDir(), "configured")}, {ID: "unassociated", Adapter: "external"}}}
	scope, sources, err := queueProjectSources(context.WithValue(context.Background(), queueProjectRootKey{}, outside), cfg, false)
	if err != nil || scope.Kind != "all-projects" || len(sources) != len(cfg.Sources) {
		t.Fatalf("outside-project default: scope=%+v sources=%v err=%v", scope, sources, err)
	}
}
