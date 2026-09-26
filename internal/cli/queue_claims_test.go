package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/reason"
	urfave "github.com/urfave/cli/v3"
)

func TestQueueClaimsOnlyFallbackForMissingAndViewlessConfig(t *testing.T) {
	t.Parallel()
	missing := reason.New(reason.ReasonNoSourcesConfigured, "queue config missing")
	notice, onlyClaims, err := queueClaimsOnlyFallback(config.QueueConfig{}, missing)
	if err != nil || !onlyClaims || !strings.Contains(notice, "not configured") {
		t.Fatalf("missing config fallback=(%q,%t,%v)", notice, onlyClaims, err)
	}
	notice, onlyClaims, err = queueClaimsOnlyFallback(config.QueueConfig{Sources: []config.QueueSource{{ID: "source"}}}, nil)
	if err != nil || !onlyClaims || !strings.Contains(notice, "no views") {
		t.Fatalf("viewless config fallback=(%q,%t,%v)", notice, onlyClaims, err)
	}
	malformed := errors.New("malformed queue config")
	if notice, onlyClaims, err = queueClaimsOnlyFallback(config.QueueConfig{}, malformed); !errors.Is(err, malformed) || onlyClaims || notice != "" {
		t.Fatalf("malformed config was hidden: (%q,%t,%v)", notice, onlyClaims, err)
	}
}

func TestClaimsOnlyModelStartsOnAuthorityClaimsWithNotice(t *testing.T) {
	t.Parallel()
	model := newClaimsOnlyModel("remote authority-1", "remote", true, "queue.yaml is not configured")
	if model.ViewName != queueui.ClaimsViewID || len(model.Views) != 1 || model.Views[0] != queueui.ClaimsViewID || !model.HighContrast || !model.Claims.Loading || model.Claims.Notice == "" {
		t.Fatalf("claims-only model startup state=%+v views=%v", model.Claims, model.Views)
	}
	if view := model.View(); !strings.Contains(view, "Claims") || !strings.Contains(view, "queue.yaml is not configured") {
		t.Fatalf("claims-only startup notice not visible: %s", view)
	}
}

func TestQueueClaimsEntryPointRejectsMalformedConfig(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "worklease")
	if err := handle.EnsureOwnerPrivateDir(configDir); err != nil {
		t.Fatal(err)
	}
	if err := handle.WriteOwnerPrivate(filepath.Join(configDir, "queue.yaml"), []byte("views: [invalid"), 1<<20); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "queue", "claims"}, "test", "unknown", "unknown", &stdout, &stderr); err == nil {
		t.Fatalf("malformed queue.yaml must fail, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(configDir, "profiles.yaml")); !os.IsNotExist(err) {
		t.Fatalf("unexpected state created while rejecting config: %v", err)
	}
}

func TestQueueClaimsEntryPointIsRegistered(t *testing.T) {
	t.Parallel()
	root := NewRootCommand("test", "unknown", "unknown", nil, nil)
	var claims *urfave.Command
	var find func(*urfave.Command, string)
	find = func(command *urfave.Command, prefix string) {
		path := strings.TrimSpace(prefix + " " + command.Name)
		if path == "worklease queue claims" {
			claims = command
		}
		for _, child := range command.Commands {
			find(child, path)
		}
	}
	for _, command := range root.Commands {
		find(command, "worklease")
	}
	if claims == nil || !strings.Contains(claims.UsageText, "queue claims") {
		t.Fatal("dedicated queue claims entry point is not registered")
	}
}
