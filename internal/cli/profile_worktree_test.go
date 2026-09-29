package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/testkit"
	urfave "github.com/urfave/cli/v3"
)

func createProfileWorktrees(t *testing.T) (string, string) {
	t.Helper()
	main := filepath.Join(t.TempDir(), "main")
	if err := os.Mkdir(main, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", main}, {"-C", main, "config", "user.name", "test"}, {"-C", main, "config", "user.email", "test@example.invalid"}} {
		if output, err := testkit.GitCommand(args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(main, "tracked"), []byte("worktree fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-C", main, "add", "tracked"}, {"-C", main, "commit", "-m", "initial"}} {
		if output, err := testkit.GitCommand(args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	linked := filepath.Join(t.TempDir(), "linked")
	if output, err := testkit.GitCommand("-C", main, "worktree", "add", "--detach", linked).CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, output)
	}
	return main, linked
}

func TestProfileBindingSelectionAndMutationsSpanLinkedWorktrees(t *testing.T) {
	main, linked := createProfileWorktrees(t)
	saveTestProfiles(t, []config.Profile{testProfile("team"), testProfile("other")}, "")
	paths := config.UserProfilePaths(nil)
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(main); err != nil {
		t.Fatal(err)
	}
	if _, err := runProfileCLI(t, "profile", "bind", "team"); err != nil {
		t.Fatal(err)
	}
	mainRoot, err := handle.ContextRoot(main, nil)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := config.LoadBindings(paths)
	if err != nil || bindings[mainRoot] != "team" {
		t.Fatalf("main binding=%v err=%v", bindings, err)
	}

	if err := os.Chdir(linked); err != nil {
		t.Fatal(err)
	}
	out, err := runProfileCLI(t, "profile", "show")
	if err != nil || out != "team (selected by binding)\n" {
		t.Fatalf("linked profile show=%q err=%v", out, err)
	}
	selected, err := profileSelection(&urfave.Command{})
	if err != nil || selected.Name != "team" || selected.Source != "binding" {
		t.Fatalf("linked selection=%+v err=%v", selected, err)
	}
	backend, err := authorityFor(context.Background(), &urfave.Command{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !backend.Remote || backend.ProfileName != "team" {
		backend.Close()
		t.Fatalf("linked authority route: remote=%t profile=%q", backend.Remote, backend.ProfileName)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}

	linkedRoot, err := handle.ContextRoot(linked, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.SaveBindings(paths, map[string]string{mainRoot: "team", linkedRoot: "other"}); err != nil {
		t.Fatal(err)
	}
	_, err = runProfileCLI(t, "--json", "profile", "show")
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonProfileBindingConflict {
		t.Fatalf("legacy conflict error=%v", err)
	}

	// An explicit bind from this worktree updates the repository entry and
	// removes only this worktree's legacy override.
	if _, err := runProfileCLI(t, "profile", "bind", "team"); err != nil {
		t.Fatal(err)
	}
	bindings, err = config.LoadBindings(paths)
	if err != nil || len(bindings) != 1 || bindings[mainRoot] != "team" {
		t.Fatalf("reconciled bindings=%v err=%v", bindings, err)
	}
	if _, err := runProfileCLI(t, "profile", "bind", "other"); err != nil {
		t.Fatal(err)
	}
	bindings, err = config.LoadBindings(paths)
	if err != nil || len(bindings) != 1 || bindings[mainRoot] != "other" {
		t.Fatalf("linked bind did not change repository binding: %v err=%v", bindings, err)
	}
	if _, err := runProfileCLI(t, "profile", "unbind"); err != nil {
		t.Fatal(err)
	}
	bindings, err = config.LoadBindings(paths)
	if err != nil || len(bindings) != 0 {
		t.Fatalf("linked unbind left bindings=%v err=%v", bindings, err)
	}

}
