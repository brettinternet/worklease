package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	urfave "github.com/urfave/cli/v3"
)

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
