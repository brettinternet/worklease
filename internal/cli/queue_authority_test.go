package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/store"
	urfave "github.com/urfave/cli/v3"
)

func queueAuthorityForView(ctx context.Context, cmd *urfave.Command, name string) (*authorityContext, queue.ClaimAuthority, error) {
	return queueAuthorityForViewWithMetadata(ctx, cmd, name, true)
}

func queueAuthorityForViewWithMetadata(ctx context.Context, cmd *urfave.Command, name string, fetchMetadata bool) (*authorityContext, queue.ClaimAuthority, error) {
	return queueAuthorityForViewMode(ctx, cmd, name, fetchMetadata, false)
}

func TestQueueAuthoritySetRoutesMixedSourceBindings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("WORKLEASE_HOME", home)
	configFile, err := config.Load(config.Input{})
	if err != nil {
		t.Fatal(err)
	}
	local, err := store.Open(context.Background(), configFile.Home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	localID := local.AuthorityID()
	if err := local.Close(); err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{Name: "shared", Endpoint: "http://127.0.0.1:1", AllowInsecureHTTP: true, AuthorityID: strings.Repeat("a", 32), Credential: config.CredentialDescriptor{Path: filepath.Join(t.TempDir(), "credential")}}
	if err := config.SaveProfiles(config.UserProfilePaths(os.Getenv), []config.Profile{profile}, ""); err != nil {
		t.Fatal(err)
	}
	cfg := config.QueueConfig{
		Sources: []config.QueueSource{{ID: "personal", Authority: config.LocalProfileName}, {ID: "team", Authority: profile.Name}},
		Views:   []config.QueueView{{Name: "Ready", Sources: []string{"personal", "team"}}},
	}
	set, err := queueAuthoritiesForSources(context.Background(), &urfave.Command{}, cfg, []string{"personal", "team"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	localBackend, localAuthority, localOK := set.ForSource("personal")
	teamBackend, teamAuthority, teamOK := set.ForSource("team")
	if !localOK || !teamOK || localBackend == teamBackend || localAuthority.ID != localID || teamAuthority.ID != profile.AuthorityID || set.SourceNames["personal"] != config.LocalProfileName || set.SourceNames["team"] != profile.Name {
		t.Fatalf("mixed authority routing: local=(%+v,%+v,%v) team=(%+v,%+v,%v) names=%v", localBackend, localAuthority, localOK, teamBackend, teamAuthority, teamOK, set.SourceNames)
	}
	if summary := queueAuthoritySummary(set, []string{"personal", "team"}); summary.Profile != "mixed" || summary.Scope != "mixed" || len(summary.Authorities) != 2 {
		t.Fatalf("mixed authority summary: %+v", summary)
	}
}

func TestQueueUnavailableRemoteNeverFallsBackToLocal(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("WORKLEASE_HOME", t.TempDir())
	paths := config.UserProfilePaths(os.Getenv)
	profile := config.Profile{Name: "offline", Endpoint: "http://127.0.0.1:1", AllowInsecureHTTP: true, AuthorityID: strings.Repeat("a", 32), Credential: config.CredentialDescriptor{Path: filepath.Join(filepath.Dir(paths.Profiles), "missing-credential")}}
	if err := config.SaveProfiles(paths, []config.Profile{profile}, ""); err != nil {
		t.Fatal(err)
	}
	backend, overlay, err := queueAuthorityForView(context.Background(), &urfave.Command{}, "offline")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if !backend.Remote || backend.Store != nil || !overlay.Remote || overlay.ID != profile.AuthorityID || overlay.AdmittedPrefixes != nil {
		t.Fatalf("remote outage fell back: backend=%+v overlay=%+v", backend, overlay)
	}
}
