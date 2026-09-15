package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/server"
)

func main() {
	binary, err := filepath.Abs("bin/worklease")
	if err != nil {
		fatal(err)
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		fatal(err)
	}
	examples := runnableExamples(string(readme))
	for _, required := range []string{"quick-start", "json-two-loops", "mcp-discovery"} {
		body, ok := examples[required]
		if !ok {
			fatal(fmt.Errorf("README missing runnable example %s", required))
		}
		runExample(binary, required, body)
	}
	validateCurrentDocs()
	validateRemoteDocs()
	validateDocumentedExitFamilies()
	testContention(binary)
	testMCPTwoLoops(binary)
	fmt.Println("worklease documentation examples passed")
}

func runnableExamples(markdown string) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(markdown))
	name := ""
	var lines []string
	inFence := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "<!-- worklease-example:run ") {
			name = strings.TrimSuffix(strings.TrimPrefix(line, "<!-- worklease-example:run "), " -->")
			lines = nil
			continue
		}
		if name == "" {
			continue
		}
		if strings.HasPrefix(line, "```") {
			if !inFence {
				inFence = true
				continue
			}
			inFence = false
			result[name] = strings.Join(lines, "\n")
			name = ""
			continue
		}
		if inFence {
			lines = append(lines, line)
		}
	}
	return result
}

func runExample(binary, name, body string) {
	root, err := os.MkdirTemp("", "worklease-doc-example-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(root)
	if output, err := exec.Command("git", "init", "--quiet", root).CombinedOutput(); err != nil {
		fatal(fmt.Errorf("%s git init: %w: %s", name, err, output))
	}
	command := exec.Command("sh", "-eu", "-c", body)
	command.Dir = root
	command.Env = append(os.Environ(), "PATH="+filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"), "WORKLEASE_HOME="+filepath.Join(root, "home"), "WORKLEASE_AGENT_ID=docs")
	if output, err := command.CombinedOutput(); err != nil {
		fatal(fmt.Errorf("runnable example %s: %w: %s", name, err, output))
	}
}

func validateCurrentDocs() {
	current := []string{"README.md", "docs/cli-reference.md", "docs/claim-model.md", "docs/mcp.md", "docs/setup.md", "skills/worklease-workflow/SKILL.md"}
	forbidden := regexp.MustCompile(`--token(?:[ =]|$)`)
	var combined strings.Builder
	for _, path := range current {
		data, err := os.ReadFile(path)
		if err != nil {
			fatal(err)
		}
		combined.Write(data)
		for _, block := range fencedBlocks(string(data)) {
			if forbidden.MatchString(block) {
				fatal(fmt.Errorf("%s contains runnable-looking argv --token", path))
			}
		}
	}
	text := combined.String()
	if !strings.Contains(text, "--token-file") || !strings.Contains(text, "--token-fd") {
		fatal(fmt.Errorf("current docs must describe file and fd credential sources"))
	}
	// Backlog and migration records are deliberately outside executable-example
	// validation.
}

func fencedBlocks(markdown string) []string {
	var blocks []string
	parts := strings.Split(markdown, "```")
	for index := 1; index < len(parts); index += 2 {
		blocks = append(blocks, parts[index])
	}
	return blocks
}

func validateRemoteDocs() {
	guides := map[string][]string{
		"README.md": {
			"experimental", "no listener", "no network request", "Local reads remain setup-free",
			"Remote failures do", "not fall back to local coordination",
		},
		"docs/remote-claim-authority.md": {
			"**experimental**", "worklease profile add NAME", "--authority-id ID", "--allow-insecure-http",
			"--invite-file FILE", "--invite-fd N", "--role read|write|admin", "--expected-recovery-revision N",
			"--attestation-file FILE", "--selected-cutoff RFC3339", "--loss-interval-start RFC3339", "gc --apply", "--cutoff TIME", "--retention-days N",
			"--loss-interval-end RFC3339", "--cutoff-unknown", "--unresolved-export FILE", "Follow-up triggers remain explicit",
			"one Worklease namespace", "one serialized SQLite writer", "hosted OS lock", "stop before start",
			"active, retired, and ephemeral", "enumerable pending-request set", "lost-tail outcomes",
			"Provider cessation and executor/process cessation", "recovery closed indefinitely",
			"fully missing completed operation", "recovery import", "completed-history journal",
			"passed all five groups", "34915583460", "7e4cef4312d09e380d5c02cea7f23bd13ab66a9c",
			"+2,147,600 B", "+5,242,880 B", "+2,383,219 B", "+5,763,072 B",
			"+2,218,770 B", "+5,344,688 B", "+2,396,120 B", "+5,856,672 B",
		},
		"docs/cli-reference.md": {
			"## Experimental remote authority", "--profile NAME", "WORKLEASE_PROFILE", "--local",
			"worklease server restore", "worklease recovery reopen", "--cutoff-unknown",
			"Remote `--wait`", "--poll-interval", "gc` requires `--apply`", "--cutoff TIME", "--retention-days N", "unsupported",
		},
		"docs/mcp.md": {
			"Experimental remote profile", "--profile NAME", "local reads remain setup-free",
			"key", "acquire", "status", "list", "heartbeat", "checkpoint", "verify", "watch", "events", "release", "instructions",
			"MCP does not add enrollment", "authentication-required", "operation-kind-unsupported",
		},
		"skills/worklease-workflow/SKILL.md": {
			"provider-neutral contract above is unchanged", "--profile NAME", "WORKLEASE_PROFILE", "--local",
			"no network request", "durable exact pending requests", "Provider execution, provider", "cessation, and file replacement remain client-local",
			"Recovery import", "HA", "Postgres", "browser control plane",
		},
	}
	for path, fragments := range guides {
		data, err := os.ReadFile(path)
		if err != nil {
			fatal(err)
		}
		text := string(data)
		for _, fragment := range fragments {
			if !strings.Contains(text, fragment) {
				fatal(fmt.Errorf("%s missing remote documentation statement %q", path, fragment))
			}
		}
	}
	remote, err := os.ReadFile("docs/remote-claim-authority.md")
	if err != nil {
		fatal(err)
	}
	for _, stale := range []string{"future remote design", "not a shipped HTTP backend", "proposal is deferred"} {
		if strings.Contains(string(remote), stale) {
			fatal(fmt.Errorf("remote guide still describes shipped capability as %q", stale))
		}
	}
	validateRemoteServerConfig(string(remote))
}

func validateRemoteServerConfig(markdown string) {
	match := regexp.MustCompile("(?s)```yaml\\n(.*?)\\n```").FindStringSubmatch(markdown)
	if len(match) != 2 {
		fatal(fmt.Errorf("remote guide missing canonical YAML server configuration"))
	}
	root, err := os.MkdirTemp("", "worklease-doc-remote-config-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(root)
	certPath, keyPath := filepath.Join(root, "server.crt"), filepath.Join(root, "server.key")
	for _, path := range []string{certPath, keyPath} {
		if err := os.WriteFile(path, []byte("documentation fixture\\n"), 0o600); err != nil {
			fatal(err)
		}
	}
	config := strings.ReplaceAll(match[1], "/srv/worklease", filepath.Join(root, "home"))
	config = strings.ReplaceAll(config, "/etc/worklease/server.crt", certPath)
	config = strings.ReplaceAll(config, "/etc/worklease/server.key", keyPath)
	configPath := filepath.Join(root, "server.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		fatal(err)
	}
	loaded, err := server.LoadConfig(configPath)
	if err != nil {
		fatal(fmt.Errorf("remote guide canonical server configuration: %w", err))
	}
	if strings.Join(loaded.Prefixes, ",") != "github:,coordination:" {
		fatal(fmt.Errorf("remote guide canonical server prefixes parsed as %q", loaded.Prefixes))
	}
}

func validateDocumentedExitFamilies() {
	guide, err := os.ReadFile("docs/cli-reference.md")
	if err != nil {
		fatal(err)
	}
	for _, fragment := range []string{"`2` ownership/contention", "`3` ledger, replay, or ambiguous outcome", "`64` invalid input/configuration", "`75`\nauthority or storage failure", "`124` child timeout", "`130` interruption"} {
		if !strings.Contains(string(guide), fragment) {
			fatal(fmt.Errorf("CLI reference missing exit-family text %q", fragment))
		}
	}
	for namedReason, want := range map[string]int{
		reason.ReasonAlreadyClaimed:     2,
		reason.ReasonOperationAmbiguous: 3,
		reason.ReasonInvalidArgument:    64,
		reason.ReasonStorageFailure:     75,
		reason.ReasonChildTimeout:       124,
		reason.ReasonInterrupted:        130,
	} {
		if got := reason.CodeFor(namedReason); got != want {
			fatal(fmt.Errorf("exit family %s=%d want=%d", namedReason, got, want))
		}
	}
}

func testContention(binary string) {
	root, err := os.MkdirTemp("", "worklease-doc-contention-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(root)
	if output, err := exec.Command("git", "init", "--quiet", root).CombinedOutput(); err != nil {
		fatal(fmt.Errorf("contention git init: %w: %s", err, output))
	}
	env := append(os.Environ(), "WORKLEASE_HOME="+filepath.Join(root, "home"), "WORKLEASE_AGENT_ID=docs")
	run := func(session string) ([]byte, error) {
		cmd := exec.Command(binary, "--json", "acquire", "--resource", "shared", "--session", session)
		cmd.Dir, cmd.Env = root, env
		return cmd.CombinedOutput()
	}
	if output, err := run("contender-a"); err != nil {
		fatal(fmt.Errorf("first contender: %w: %s", err, output))
	}
	output, err := run("contender-b")
	if err == nil {
		fatal(fmt.Errorf("second contender unexpectedly acquired resource"))
	}
	var response struct {
		Error struct {
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if json.Unmarshal(output, &response) != nil || response.Error.Reason != "already-claimed" {
		fatal(fmt.Errorf("contention was not structured already-claimed: %s", output))
	}
	cmd := exec.Command(binary, "release", "--session", "contender-a", "--reason", "done")
	cmd.Dir, cmd.Env = root, env
	if output, err := cmd.CombinedOutput(); err != nil {
		fatal(fmt.Errorf("release contender: %w: %s", err, output))
	}
}

func testMCPTwoLoops(binary string) {
	root, err := os.MkdirTemp("", "worklease-doc-mcp-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(root)
	command := exec.Command(binary, "mcp")
	command.Env = append(os.Environ(), "WORKLEASE_HOME="+filepath.Join(root, "home"), "WORKLEASE_AGENT_ID=docs-mcp")
	input, err := command.StdinPipe()
	if err != nil {
		fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		fatal(err)
	}
	scanner := bufio.NewScanner(output)
	nextID := 0
	rawCall := func(name string, arguments map[string]any) (map[string]any, bool) {
		nextID++
		request := map[string]any{"jsonrpc": "2.0", "id": nextID, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}, "_meta": map[string]any{"protocolVersion": "2026-07-28"}}
		encoded, _ := json.Marshal(request)
		if _, err := fmt.Fprintln(input, string(encoded)); err != nil || !scanner.Scan() {
			fatal(fmt.Errorf("MCP %s transport failed: %v", name, err))
		}
		var response map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			fatal(err)
		}
		result, ok := response["result"].(map[string]any)
		if !ok {
			fatal(fmt.Errorf("MCP %s protocol response: %v", name, response))
		}
		content, _ := result["structuredContent"].(map[string]any)
		isError, _ := result["isError"].(bool)
		return content, isError
	}
	call := func(name string, arguments map[string]any) map[string]any {
		content, isError := rawCall(name, arguments)
		if isError || content["ok"] != true {
			fatal(fmt.Errorf("MCP %s domain error: %v", name, content))
		}
		return content
	}
	first := call("acquire", map[string]any{"resources": []string{"mcp-a"}, "sessionId": "loop-a", "agentId": "agent-a"})
	second := call("acquire", map[string]any{"resources": []string{"mcp-b"}, "sessionId": "loop-b", "agentId": "agent-b"})
	firstLease, firstOK := first["lease"].(string)
	secondLease, secondOK := second["lease"].(string)
	if !firstOK || !secondOK || firstLease == secondLease {
		fatal(fmt.Errorf("MCP loops did not return isolated lease references: %v %v", first, second))
	}
	call("status", map[string]any{"lease": firstLease})
	call("status", map[string]any{"lease": secondLease})
	conflict, isError := rawCall("acquire", map[string]any{"resources": []string{"mcp-a"}, "sessionId": "loop-c", "agentId": "agent-c"})
	if namedReason, _ := conflict["error"].(map[string]any)["reason"].(string); !isError || namedReason != "already-claimed" {
		fatal(fmt.Errorf("MCP contention was not structured already-claimed: %v", conflict))
	}
	call("release", map[string]any{"lease": firstLease, "reason": "done"})
	call("release", map[string]any{"lease": secondLease, "reason": "done"})
	_ = input.Close()
	if err := command.Wait(); err != nil {
		fatal(fmt.Errorf("MCP two-loop server: %w", err))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
