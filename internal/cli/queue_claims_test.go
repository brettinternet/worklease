package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/testkit"
	"github.com/creack/pty"
	urfave "github.com/urfave/cli/v3"
)

func TestQueueClaimsOnlyFallbackForMissingAndViewlessConfig(t *testing.T) {
	t.Parallel()
	missing := reason.New(reason.ReasonNoSourcesConfigured, "queue config missing")
	notice, onlyClaims, err := queueClaimsOnlyFallback(config.QueueConfig{}, missing)
	if err != nil || !onlyClaims || !strings.Contains(notice, "worklease queue init") {
		t.Fatalf("missing config fallback=(%q,%t,%v)", notice, onlyClaims, err)
	}
	notice, onlyClaims, err = queueClaimsOnlyFallback(config.QueueConfig{Sources: []config.QueueSource{{ID: "source"}}}, nil)
	if err != nil || !onlyClaims || !strings.Contains(notice, "no views") {
		t.Fatalf("viewless config fallback=(%q,%t,%v)", notice, onlyClaims, err)
	}
	configured := config.QueueConfig{Views: []config.QueueView{{Name: "First"}, {Name: "Second"}}}
	if notice, onlyClaims, err = queueClaimsOnlyFallback(configured, nil); err != nil || onlyClaims || notice != "" {
		t.Fatalf("configured views must keep their queue entry point: (%q,%t,%v)", notice, onlyClaims, err)
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
	if view := model.View(); !strings.Contains(view, "Claims") || !strings.Contains(view, "queue.yaml is not configured") || !strings.Contains(view, "worklease acquire --path README.md") || !strings.Contains(view, "? for help") || !strings.Contains(view, "q to quit") {
		t.Fatalf("claims-only startup notice not visible: %s", view)
	}
}

func TestBareInteractiveFreshHomeShowsClaimsWithoutState(t *testing.T) {
	if testkit.RunIsolatedTest(t) {
		return
	}
	home, env := testkit.Home(t)
	for key, value := range env {
		t.Setenv(key, value)
	}
	runBarePTY(t, "worklease acquire --path README.md", "worklease queue init", "Claims")
	for _, path := range []string{home, env["XDG_CONFIG_HOME"], env["XDG_STATE_HOME"]} {
		entries, err := os.ReadDir(path)
		if err != nil || len(entries) != 0 {
			t.Fatalf("bare Claims TUI created state at %s: %v, %v", path, entries, err)
		}
	}
}

func TestBareInteractiveKeepsConfiguredViewWhenSourceFails(t *testing.T) {
	if testkit.RunIsolatedTest(t) {
		return
	}
	_, env := testkit.Home(t)
	for key, value := range env {
		t.Setenv(key, value)
	}
	configDir := filepath.Join(env["XDG_CONFIG_HOME"], "worklease")
	if err := handle.EnsureOwnerPrivateDir(configDir); err != nil {
		t.Fatal(err)
	}
	content := "version: 1\nme: {backlog-md: ['@test']}\nsources:\n  - id: missing\n    adapter: backlog-md\n    checkout: " + env["HOME"] + "\nviews:\n  - name: Ready\n    authority: local\n    sources: [missing]\n    filter: {}\n"
	if err := handle.WriteOwnerPrivate(filepath.Join(configDir, "queue.yaml"), []byte(content), 1<<20); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if _, claimsOnly, err := queueClaimsOnlyFallback(cfg, nil); err != nil || claimsOnly {
		t.Fatalf("configured source must use the queue view: %t %v", claimsOnly, err)
	}
	model := queueui.New(queue.Snapshot{Items: map[string]queue.Item{}, Sources: map[string]queue.Coverage{}})
	model.Views = []string{cfg.Views[0].Name, queueui.ClaimsViewID}
	model.ViewName = cfg.Views[0].Name
	model.Sources = []queue.Source{{ID: "missing", Name: "missing"}}
	model.SourceErrors = map[string]string{"missing": "source-read-failed"}
	if view := model.View(); !strings.Contains(view, "Ready") || !strings.Contains(view, "missing: source-read-failed") || model.ViewName == queueui.ClaimsViewID {
		t.Fatalf("source outage hid the configured view: %s", view)
	}
	runBarePTY(t, "Ready", "Sources missing:")
}

// Exercise the real root dispatch and Bubble Tea startup on a pseudo-terminal.
func runBarePTY(t *testing.T, visible ...string) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 35, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	originalInput := os.Stdin
	os.Stdin = slave
	defer func() { os.Stdin = originalInput }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- Run(ctx, []string{"worklease"}, "test", "unknown", "unknown", slave, slave) }()
	chunks := make(chan string, 16)
	readErrors := make(chan error, 1)
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := master.Read(buffer)
			if err != nil {
				readErrors <- err
				return
			}
			chunks <- string(buffer[:n])
		}
	}()
	var screen strings.Builder
	for {
		shown := screen.String()
		allVisible := true
		for _, text := range visible {
			allVisible = allVisible && strings.Contains(shown, text)
		}
		if allVisible {
			break
		}
		select {
		case chunk := <-chunks:
			screen.WriteString(chunk)
		case err := <-readErrors:
			cancel()
			<-finished
			t.Fatalf("TUI missing %v: %v (%s)", visible, err, shown)
		case <-ctx.Done():
			<-finished
			t.Fatalf("TUI missing %v: %v (%s)", visible, ctx.Err(), shown)
		}
	}
	if _, err := master.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("TUI did not quit on q")
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
	queueErr := Run(context.Background(), []string{"worklease", "queue"}, "test", "unknown", "unknown", &stdout, &stderr)
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	originalInput := os.Stdin
	os.Stdin = slave
	defer func() { os.Stdin = originalInput }()
	bareErr := Run(context.Background(), []string{"worklease"}, "test", "unknown", "unknown", slave, slave)
	if queueErr == nil || bareErr == nil || bareErr.Error() != queueErr.Error() {
		t.Fatalf("bare invocation must report the same config error as queue: bare=%v queue=%v", bareErr, queueErr)
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
