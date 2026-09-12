package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type envelope struct {
	OK          bool   `json:"ok"`
	Version     string `json:"version"`
	Resource    string `json:"resource"`
	ClaimID     string `json:"claimId"`
	OperationID string `json:"operationId"`
	NextCursor  string `json:"nextCursor"`
	Inspection  struct {
		RequestSHA256 string `json:"requestSha256"`
	} `json:"inspection"`
}

func main() {
	binary := flag.String("binary", "bin/worklease", "built worklease binary")
	version := flag.String("version", "dev", "expected binary version")
	flag.Parse()
	root, err := os.MkdirTemp("", "worklease-smoke-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(root)
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		fatal(err)
	}
	git := exec.Command("git", "init", "--quiet", project)
	if output, err := git.CombinedOutput(); err != nil {
		fatal(fmt.Errorf("initialize smoke repository: %w: %s", err, output))
	}
	target := filepath.Join(project, "owned.txt")
	content := filepath.Join(root, "replacement.txt")
	mustWrite(target, "old\n")
	mustWrite(content, "new\n")
	env := append(os.Environ(), "WORKLEASE_HOME="+home, "WORKLEASE_AGENT_ID=smoke", "WORKLEASE_SESSION_ID=smoke-session")

	result := runJSON(env, *binary, "version")
	if result.Version != *version {
		fatal(fmt.Errorf("version=%q want=%q", result.Version, *version))
	}
	key := runJSON(env, *binary, "key", "--path", target)
	if key.Resource == "" {
		fatal(fmt.Errorf("key returned no resource"))
	}
	acquired := runJSON(env, *binary, "acquire", "--resource", key.Resource, "--resource", "smoke-extra")
	runJSON(env, *binary, "status")
	runJSON(env, *binary, "list", "--resource", key.Resource)
	runJSON(env, *binary, "verify", "--coverage", "path", "--resource", key.Resource)
	runJSON(env, *binary, "exec", "--", *binary, "version")

	// Recover a same-claim guard whose started intent advanced the authority
	// beyond the revision retained by its pending contextual handle.
	timedOutOperation := strings.Repeat("e", 32)
	runFailure(env, *binary, "exec", "--operation-id", timedOutOperation, "--max-duration", "100ms", "--", "sleep", "5")
	inspection := runJSON(env, *binary, "op", "inspect", "--operation-id", timedOutOperation)
	if inspection.Inspection.RequestSHA256 == "" {
		fatal(fmt.Errorf("timed-out operation inspection returned no request hash"))
	}
	runJSON(env, *binary, "op", "reconcile", "--target-claim-id", acquired.ClaimID, "--target-operation-id", timedOutOperation, "--expected-request-sha256", inspection.Inspection.RequestSHA256, "--outcome", "observed-failure", "--evidence", `{"outcome":"observed-failure","executorStopped":true}`)
	runJSON(env, *binary, "heartbeat")
	runJSON(env, *binary, "checkpoint", "--data", `{"phase":"recovered"}`)
	runJSON(env, *binary, "exec", "--", *binary, "version")
	if acquired.OperationID != "" {
		runJSON(env, *binary, "op", "inspect", "--operation-id", acquired.OperationID)
	}
	runJSON(env, *binary, "history", "--resource", key.Resource)
	events := runJSON(env, *binary, "events", "--limit", "100")
	if events.NextCursor != "" {
		runJSON(env, *binary, "watch", "--cursor", events.NextCursor, "--timeout", "10ms")
	}
	runJSON(env, *binary, "gc")
	runJSON(env, *binary, "doctor")
	runJSON(env, *binary, "policy", "list")
	runJSON(env, *binary, "policy", "describe", "path")
	runJSON(env, *binary, "setup", "instructions")
	hash := sha256File(target)
	runJSON(env, *binary, "replace-file", "--path", target, "--expected-sha256", hash, "--content-file", content)
	runJSON(env, *binary, "release", "--reason", "smoke complete")
	if got, err := os.ReadFile(target); err != nil || string(got) != "new\n" {
		fatal(fmt.Errorf("replacement result=%q err=%v", got, err))
	}

	modern := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"server/discover\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\",\"_meta\":{\"protocolVersion\":\"2026-07-28\"}}\n"
	legacy := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-11-25\"}}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n"
	for name, input := range map[string]string{"modern": modern, "legacy": legacy} {
		cmd := exec.Command(*binary, "mcp")
		cmd.Env = env
		cmd.Stdin = strings.NewReader(input)
		output, err := cmd.CombinedOutput()
		if err != nil || !bytes.Contains(output, []byte(`"tools"`)) {
			fatal(fmt.Errorf("%s MCP smoke: %w: %s", name, err, output))
		}
	}
	fmt.Println("worklease built-binary smoke passed")
}

func runFailure(env []string, binary string, args ...string) {
	cmd := exec.Command(binary, append([]string{"--json"}, args...)...)
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err == nil {
		fatal(fmt.Errorf("%s %s unexpectedly succeeded: %s", binary, strings.Join(args, " "), output))
	}
}

func runJSON(env []string, binary string, args ...string) envelope {
	cmd := exec.Command(binary, append([]string{"--json"}, args...)...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		fatal(fmt.Errorf("%s %s: %w: %s", binary, strings.Join(args, " "), err, output))
	}
	var result envelope
	if err := json.Unmarshal(output, &result); err != nil || !result.OK {
		fatal(fmt.Errorf("%s %s invalid response: %w: %s", binary, strings.Join(args, " "), err, output))
	}
	return result
}

func sha256File(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func mustWrite(path, value string) {
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
