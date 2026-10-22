package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/testkit"
	"github.com/creack/pty"
)

func TestClaimsOnlyModelStartsOnAuthorityClaimsWithoutSetupNotice(t *testing.T) {
	t.Parallel()
	model := newClaimsOnlyModel("remote authority-1", "remote", true)
	if !model.HighContrast || !model.Claims.Loading || model.Claims.Notice != "" {
		t.Fatalf("claims-only model startup state=%+v", model.Claims)
	}
	if view := model.View(); !strings.Contains(view, "Claims") || !strings.Contains(view, "worklease acquire --path README.md") || !strings.Contains(view, "? for help") || !strings.Contains(view, "q to quit") {
		t.Fatalf("claims-only startup should show claims guidance without a setup notice: %s", view)
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
	runBarePTY(t, "worklease acquire --path README.md", "? for help", "Claims")
	for _, path := range []string{home, env["XDG_CONFIG_HOME"], env["XDG_STATE_HOME"]} {
		entries, err := os.ReadDir(path)
		if err != nil || len(entries) != 0 {
			t.Fatalf("bare Claims TUI created state at %s: %v, %v", path, entries, err)
		}
	}
}

func TestClaimsOnlyTUISeesAuthorityCreatedAfterStartup(t *testing.T) {
	if testkit.RunIsolatedTest(t) {
		return
	}
	home, env := testkit.Home(t)
	for key, value := range env {
		t.Setenv(key, value)
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- Run(ctx, []string{"worklease"}, "test", "unknown", "unknown", slave, slave)
	}()
	chunks := make(chan string, 32)
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := master.Read(buffer)
			if err != nil {
				return
			}
			select {
			case chunks <- string(buffer[:n]):
			case <-ctx.Done():
				return
			}
		}
	}()
	var screen strings.Builder
	for !strings.Contains(screen.String(), "worklease acquire --path README.md") {
		select {
		case chunk := <-chunks:
			screen.WriteString(chunk)
		case err := <-finished:
			t.Fatalf("Claims TUI ended before startup: %v", err)
		case <-ctx.Done():
			t.Fatalf("Claims TUI startup: %v", ctx.Err())
		}
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("browsing created state: %v %v", entries, err)
	}
	var out, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "acquire", "--local", "--home", home, "--resource", "coordination:new-after-start"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("acquire after startup: %v %s", err, stderr.String())
	}
	if _, err := master.Write([]byte("r")); err != nil {
		t.Fatal(err)
	}
	for !strings.Contains(screen.String(), "coordination:new-after-start") {
		select {
		case chunk := <-chunks:
			screen.WriteString(chunk)
		case err := <-finished:
			t.Fatalf("Claims TUI ended before new claim: %v", err)
		case <-ctx.Done():
			t.Fatalf("new claim missing after refresh: %v", ctx.Err())
		}
	}
	if _, err := master.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("Claims TUI: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("Claims TUI did not quit: %v", ctx.Err())
	}
}

func TestClaimsOnlyTUIOpensWritableAuthorityOnConfirmedMutation(t *testing.T) {
	_, env := testkit.Home(t)
	for key, value := range env {
		t.Setenv(key, value)
	}
	home := t.TempDir()
	t.Setenv("WORKLEASE_HOME", home)
	var out, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "acquire", "--local", "--home", home, "--resource", "coordination:claims-tui"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("acquire: %v %s", err, stderr.String())
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- Run(ctx, []string{"worklease"}, "test", "unknown", "unknown", slave, slave)
	}()
	chunks := make(chan string, 32)
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := master.Read(buffer)
			if err != nil {
				return
			}
			select {
			case chunks <- string(buffer[:n]):
			case <-ctx.Done():
				return
			}
		}
	}()
	var screen strings.Builder
	await := func(want string) {
		t.Helper()
		for !strings.Contains(screen.String(), want) {
			select {
			case chunk := <-chunks:
				screen.WriteString(chunk)
			case err := <-finished:
				t.Fatalf("Claims TUI ended before %q: %v (%s)", want, err, screen.String())
			case <-ctx.Done():
				t.Fatalf("Claims TUI missing %q: %v (%s)", want, ctx.Err(), screen.String())
			}
		}
	}
	send := func(key string) {
		t.Helper()
		if _, err := master.Write([]byte(key)); err != nil {
			t.Fatal(err)
		}
	}
	await("coordination:claims-tui")
	send("u")
	await("Confirm claim renewal")
	send("\r")
	await("Claim renewed")
	send("R")
	await("Release reason")
	send("\r")
	await("Confirm claim release")
	send("\r")
	await("Claim released")
	send("q")
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("Claims TUI: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("Claims TUI did not quit: %v", ctx.Err())
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "list", "--local", "--home", home, "--json"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("list after TUI release: %v %s", err, stderr.String())
	}
	var listed struct {
		Claims []lease.ClaimView `json:"claims"`
	}
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil || len(listed.Claims) != 0 {
		t.Fatalf("claim still held after TUI release: %v %+v %s", err, listed.Claims, out.String())
	}
}

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
