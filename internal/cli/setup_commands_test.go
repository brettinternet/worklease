package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupMCPPreviewApplyAndNewUserLifecycle(t *testing.T) {
	project, authority := t.TempDir(), filepath.Join(t.TempDir(), "authority")
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)
	var preview bytes.Buffer
	args := []string{"worklease", "--home", authority, "setup", "mcp", "--client", "claude-code", "--scope", "project", "--agent", "new-agent"}
	if err := Run(context.Background(), args, "1.2.3", "unknown", "unknown", &preview, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(project, ".mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("preview wrote config: %v", err)
	}
	resolvedProject, _ := filepath.EvalSymlinks(project)
	if !strings.Contains(preview.String(), "+++ "+filepath.Join(resolvedProject, ".mcp.json")) {
		t.Fatal(preview.String())
	}
	args = append(args, "--apply")
	if err := Run(context.Background(), args, "1.2.3", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(project, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	entry := config["mcpServers"].(map[string]any)["worklease"].(map[string]any)
	if entry["env"].(map[string]any)["WORKLEASE_AGENT_ID"] != "new-agent" {
		t.Fatalf("entry=%#v", entry)
	}
	entryArgs := entry["args"].([]any)
	if entryArgs[0] != "--home" || entryArgs[1] != authority || entryArgs[len(entryArgs)-1] != "mcp" {
		t.Fatalf("args=%v", entryArgs)
	}
	for _, command := range [][]string{{"worklease", "--home", authority, "acquire", "--resource", "onboarding"}, {"worklease", "--home", authority, "status"}, {"worklease", "--home", authority, "release"}} {
		if err := Run(context.Background(), command, "1.2.3", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
			t.Fatalf("%v: %v", command, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(project, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("MCP setup installed guard: %v", err)
	}
}

func TestSetupGuardAndInstructionsJSON(t *testing.T) {
	project := t.TempDir()
	previous, _ := os.Getwd()
	_ = os.Chdir(project)
	defer os.Chdir(previous)
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--json", "setup", "guard", "--client", "claude-code", "--coverage", "path", "--session", "loop-1", "--apply"}, "1.2.3", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["operation"] != "setup-guard" || envelope["applied"] != true {
		t.Fatalf("%s", out.String())
	}
	data, _ := os.ReadFile(filepath.Join(project, ".claude", "settings.json"))
	if strings.Contains(string(data), "Bash") || !strings.Contains(string(data), "'--coverage' 'path'") {
		t.Fatalf("%s", data)
	}
	out.Reset()
	if err := Run(context.Background(), []string{"worklease", "setup", "instructions"}, "1.2.3", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "worklease:begin v1.2.3") || !strings.Contains(out.String(), "provider remains authoritative") {
		t.Fatal(out.String())
	}
}
