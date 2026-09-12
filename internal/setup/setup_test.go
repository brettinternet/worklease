package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/reason"
)

func baseOptions(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	return Options{Kind: "mcp", Client: "claude-code", Scope: "project", Binary: filepath.Join(root, "bin", "worklease with ' quote"), ProjectDir: filepath.Join(root, "project"), UserHome: filepath.Join(root, "home"), Version: "1.2.3"}
}

func TestMCPPreviewTargetsDocumentedClientScopesWithoutWrites(t *testing.T) {
	tests := []struct{ client, scope, suffix string }{
		{"claude-code", "project", ".mcp.json"},
		{"claude-code", "user", ".claude.json"},
		{"cursor", "project", filepath.Join(".cursor", "mcp.json")},
		{"cursor", "user", filepath.Join(".cursor", "mcp.json")},
	}
	for _, test := range tests {
		t.Run(test.client+"-"+test.scope, func(t *testing.T) {
			opts := baseOptions(t)
			opts.Client, opts.Scope = test.client, test.scope
			result, err := Run(opts)
			if err != nil {
				t.Fatal(err)
			}
			base := opts.ProjectDir
			if test.scope == "user" {
				base = opts.UserHome
			}
			if result.Target != filepath.Join(base, test.suffix) {
				t.Fatalf("target=%q", result.Target)
			}
			if _, err := os.Lstat(result.Target); !os.IsNotExist(err) {
				t.Fatalf("preview wrote target: %v", err)
			}
			if !strings.Contains(result.Preview, "--- "+result.Target) || !strings.Contains(result.Preview, `"mcpServers"`) {
				t.Fatalf("preview=%s", result.Preview)
			}
		})
	}
}

func TestMCPApplyRemovePreserveUnrelatedContentAndAreIdempotent(t *testing.T) {
	opts := baseOptions(t)
	opts.Apply = true
	opts.Home = "/authority home"
	opts.Config = "/config file"
	opts.Agent = "agent-1"
	if err := os.MkdirAll(opts.ProjectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(opts.ProjectDir, ".mcp.json")
	if err := os.WriteFile(target, []byte(`{"unrelated":{"keep":true},"mcpServers":{"other":{"command":"other"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Applied || second.Changed {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	var root map[string]any
	data, _ := os.ReadFile(target)
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if root["unrelated"].(map[string]any)["keep"] != true {
		t.Fatal("unrelated content lost")
	}
	entry := root["mcpServers"].(map[string]any)["worklease"].(map[string]any)
	if entry["command"] != opts.Binary {
		t.Fatalf("entry=%#v", entry)
	}
	args := entry["args"].([]any)
	if strings.Join([]string{args[0].(string), args[1].(string), args[2].(string), args[3].(string), args[4].(string)}, "|") != "--home|/authority home|--config|/config file|mcp" {
		t.Fatalf("args=%v", args)
	}
	opts.Apply, opts.Remove = false, true
	removed, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !removed.Removed || again.Changed {
		t.Fatalf("removed=%+v again=%+v", removed, again)
	}
}

func TestSetupRejectsMalformedUnsafeAndConcurrentTargets(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		opts := baseOptions(t)
		opts.Apply = true
		_ = os.MkdirAll(opts.ProjectDir, 0o700)
		_ = os.WriteFile(filepath.Join(opts.ProjectDir, ".mcp.json"), []byte("[]"), 0o600)
		_, err := Run(opts)
		if reason.As(err).Reason != reason.ReasonSetupConfigMalformed {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		opts := baseOptions(t)
		opts.Apply = true
		_ = os.MkdirAll(opts.ProjectDir, 0o700)
		outside := filepath.Join(t.TempDir(), "outside")
		_ = os.WriteFile(outside, []byte("{}"), 0o600)
		_ = os.Symlink(outside, filepath.Join(opts.ProjectDir, ".mcp.json"))
		_, err := Run(opts)
		if reason.As(err).Reason != reason.ReasonSetupConfigMalformed {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("concurrent", func(t *testing.T) {
		opts := baseOptions(t)
		_ = os.MkdirAll(opts.ProjectDir, 0o700)
		target := filepath.Join(opts.ProjectDir, ".mcp.json")
		old := []byte("{}\n")
		_ = os.WriteFile(target, old, 0o600)
		err := atomicReplace(target, opts.ProjectDir, old, true, []byte("{\"next\":true}\n"), func() { _ = os.WriteFile(target, []byte("{\"other\":true}\n"), 0o600) })
		if reason.As(err).Reason != reason.ReasonSetupConfigMalformed || !strings.Contains(err.Error(), "concurrently") {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestGuardGenerationModesRemovalAndNoBash(t *testing.T) {
	opts := baseOptions(t)
	opts.Kind = "guard"
	opts.Apply = true
	opts.Session = "session-1"
	opts.Handle = "/private handle"
	guardTarget := filepath.Join(opts.ProjectDir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(guardTarget), 0o700); err != nil {
		t.Fatal(err)
	}
	unrelated := `{"hooks":{"PreToolUse":[{"matcher":"Write","hooks":[{"type":"command","command":"keep-me"}]}]}}`
	if err := os.WriteFile(guardTarget, []byte(unrelated), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(first.Target)
	text := string(data)
	for _, want := range []string{`"matcher": "Edit|Write|MultiEdit|NotebookEdit"`, "'verify' '--hook' 'claude-code' '--coverage' 'claim'", "'--session' 'session-1'", "'--handle' '/private handle'"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	matcher := config["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)["matcher"].(string)
	if strings.Contains(matcher, "Bash") {
		t.Fatalf("registered Bash matcher: %s", matcher)
	}
	opts.Coverage = "path"
	if _, err := Run(opts); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(first.Target)
	if !strings.Contains(string(data), "'--coverage' 'path'") {
		t.Fatalf("path mode=%s", data)
	}
	opts.Apply, opts.Remove = false, true
	if _, err := Run(opts); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(first.Target)
	if strings.Contains(string(data), "verify --hook") || strings.Contains(string(data), "'verify'") {
		t.Fatalf("guard remains: %s", data)
	}
	if !strings.Contains(string(data), "keep-me") {
		t.Fatalf("unrelated hook removed: %s", data)
	}
}

func TestGenericAndInstructionsOutput(t *testing.T) {
	opts := baseOptions(t)
	opts.Client = "generic"
	mcp, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mcp.Preview, `"mcpServers"`) {
		t.Fatal(mcp.Preview)
	}
	opts.Kind = "guard"
	guard, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(guard.Preview, "#!/bin/sh") || !strings.Contains(guard.Preview, "--handle") {
		t.Fatal(guard.Preview)
	}
	instructionsResult, err := Run(Options{Kind: "instructions", Version: "1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(instructionsResult.Preview, "<!-- worklease:begin v1.2.3 -->") || !strings.Contains(instructionsResult.Preview, "worklease:end") {
		t.Fatal(instructionsResult.Preview)
	}
}
