// Package setup generates optional client configuration without hiding writes.
package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/brettinternet/worklease/internal/reason"
)

// Options describes one setup preview, apply, or removal.
type Options struct {
	Kind, Client, Scope, Coverage string
	Apply, Remove                 bool
	Binary, ProjectDir, UserHome  string
	Home, Config, Session         string
	Handle, Lease, Agent, Version string
}

// Result is safe to render in either text or JSON output.
type Result struct {
	Target  string `json:"target,omitempty"`
	Preview string `json:"preview"`
	Changed bool   `json:"changed"`
	Applied bool   `json:"applied"`
	Removed bool   `json:"removed"`
}

// Run renders or atomically applies one setup operation.
func Run(opts Options) (Result, error) {
	if opts.Apply && opts.Remove {
		return Result{}, reason.Invalid("--apply and --remove are mutually exclusive")
	}
	if opts.Kind == "instructions" {
		return instructionsResult(opts.Version)
	}
	client := strings.TrimSpace(opts.Client)
	if client == "" {
		client = "claude-code"
	}
	scope := strings.TrimSpace(opts.Scope)
	if scope == "" {
		scope = "project"
	}
	if scope != "project" && scope != "user" {
		return Result{}, reason.Invalid("scope must be project or user")
	}
	if opts.Kind == "guard" && coverage(opts.Coverage) != "claim" && coverage(opts.Coverage) != "path" {
		return Result{}, reason.Invalid("coverage must be claim or path")
	}
	if client == "generic" && (opts.Apply || opts.Remove) {
		return Result{}, reason.Invalid("generic setup only prints configuration")
	}
	binary, err := filepath.Abs(opts.Binary)
	if err != nil || strings.TrimSpace(opts.Binary) == "" {
		return Result{}, reason.Invalid("running binary path is unavailable")
	}
	if opts.Kind == "mcp" && client == "generic" {
		entry := mcpEntry(binary, opts)
		data, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{"worklease": entry}}, "", "  ")
		return Result{Preview: string(data) + "\n"}, nil
	}
	if opts.Kind == "guard" && client == "generic" {
		return Result{Preview: genericGuard(binary, opts)}, nil
	}
	target, base, err := targetPath(opts.Kind, client, scope, opts.ProjectDir, opts.UserHome)
	if err != nil {
		return Result{}, err
	}
	old, existed, err := readSafe(target, base)
	if err != nil {
		return Result{}, err
	}
	root := map[string]any{}
	if existed && len(bytes.TrimSpace(old)) > 0 {
		if err := json.Unmarshal(old, &root); err != nil || root == nil {
			return Result{}, malformed(target)
		}
	}
	managed, err := hasManaged(root, opts.Kind)
	if err != nil {
		return Result{}, err
	}
	var newData []byte
	if opts.Remove && !managed {
		newData = append([]byte(nil), old...)
	} else {
		if err := mutateRoot(root, opts.Kind, client, binary, opts); err != nil {
			return Result{}, err
		}
		newData, err = json.MarshalIndent(root, "", "  ")
		if err != nil {
			return Result{}, malformed(target)
		}
		newData = append(newData, '\n')
	}
	changed := !bytes.Equal(old, newData)
	preview := unified(target, old, newData)
	result := Result{Target: target, Preview: preview, Changed: changed, Applied: opts.Apply && changed, Removed: opts.Remove && changed}
	if !opts.Apply && !opts.Remove {
		return result, nil
	}
	if !changed {
		return result, nil
	}
	if err := atomicReplace(target, base, old, existed, newData, nil); err != nil {
		return Result{}, err
	}
	return result, nil
}

func targetPath(kind, client, scope, project, home string) (string, string, error) {
	if kind != "mcp" && kind != "guard" {
		return "", "", reason.Invalid("unknown setup kind")
	}
	if kind == "guard" && client != "claude-code" {
		return "", "", reason.Invalid("guard client must be claude-code or generic")
	}
	if kind == "mcp" && client != "claude-code" && client != "cursor" {
		return "", "", reason.Invalid("MCP client must be claude-code, cursor, or generic")
	}
	base := project
	if scope == "user" {
		base = home
	}
	if strings.TrimSpace(base) == "" {
		return "", "", reason.Invalid("setup base directory is unavailable")
	}
	base, err := filepath.Abs(base)
	if err != nil {
		return "", "", reason.Invalid("setup base directory is unavailable")
	}
	var relative string
	switch {
	case kind == "guard":
		relative = filepath.Join(".claude", "settings.json")
	case client == "claude-code" && scope == "project":
		relative = ".mcp.json"
	case client == "claude-code":
		relative = ".claude.json"
	case client == "cursor":
		relative = filepath.Join(".cursor", "mcp.json")
	}
	return filepath.Join(base, relative), base, nil
}

func hasManaged(root map[string]any, kind string) (bool, error) {
	key := "mcpServers"
	if kind == "guard" {
		key = "hooks"
	}
	value, ok := root[key]
	if !ok {
		return false, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return false, malformedField(key)
	}
	if kind == "mcp" {
		_, ok = object["worklease"]
		return ok, nil
	}
	value, ok = object["PreToolUse"]
	if !ok {
		return false, nil
	}
	entries, ok := value.([]any)
	if !ok {
		return false, malformedField("hooks.PreToolUse")
	}
	for _, entry := range entries {
		if managedGuard(entry) {
			return true, nil
		}
	}
	return false, nil
}

func mutateRoot(root map[string]any, kind, client, binary string, opts Options) error {
	if kind == "mcp" {
		servers, err := objectAt(root, "mcpServers")
		if err != nil {
			return err
		}
		if opts.Remove {
			delete(servers, "worklease")
		} else {
			servers["worklease"] = mcpEntry(binary, opts)
		}
		return nil
	}
	hooks, err := objectAt(root, "hooks")
	if err != nil {
		return err
	}
	var entries []any
	if existing, ok := hooks["PreToolUse"]; ok {
		entries, ok = existing.([]any)
		if !ok {
			return malformedField("hooks.PreToolUse")
		}
	}
	kept := make([]any, 0, len(entries)+1)
	for _, raw := range entries {
		if remaining, keep := withoutManagedGuard(raw); keep {
			kept = append(kept, remaining)
		}
	}
	if !opts.Remove {
		kept = append(kept, map[string]any{"matcher": "Edit|Write|MultiEdit|NotebookEdit", "hooks": []any{map[string]any{"type": "command", "command": guardCommand(binary, opts)}}})
	}
	if len(kept) == 0 {
		delete(hooks, "PreToolUse")
	} else {
		hooks["PreToolUse"] = kept
	}
	return nil
}

func objectAt(root map[string]any, key string) (map[string]any, error) {
	value, ok := root[key]
	if !ok {
		value = map[string]any{}
		root[key] = value
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, malformedField(key)
	}
	return object, nil
}

func mcpEntry(binary string, opts Options) map[string]any {
	args := globalArgs(opts)
	args = append(args, "mcp")
	entry := map[string]any{"command": binary, "args": args}
	if strings.TrimSpace(opts.Agent) != "" {
		entry["env"] = map[string]any{"WORKLEASE_AGENT_ID": opts.Agent}
	}
	return entry
}

func guardCommand(binary string, opts Options) string {
	args := append(globalArgs(opts), "verify", "--hook", "claude-code", "--coverage", coverage(opts.Coverage))
	if opts.Session != "" {
		args = append(args, "--session", opts.Session)
	}
	if opts.Handle != "" {
		args = append(args, "--handle", opts.Handle)
	}
	if opts.Lease != "" {
		args = append(args, "--lease", opts.Lease)
	}
	quoted := []string{shellQuote(binary)}
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func globalArgs(opts Options) []string {
	args := []string{}
	if opts.Home != "" {
		args = append(args, "--home", opts.Home)
	}
	if opts.Config != "" {
		args = append(args, "--config", opts.Config)
	}
	return args
}

func coverage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "claim"
	}
	return value
}

func managedGuard(raw any) bool {
	entry, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	handlers, ok := entry["hooks"].([]any)
	if !ok {
		return false
	}
	for _, handler := range handlers {
		if isManagedHandler(handler) {
			return true
		}
	}
	return false
}

func withoutManagedGuard(raw any) (any, bool) {
	entry, ok := raw.(map[string]any)
	if !ok {
		return raw, true
	}
	handlers, ok := entry["hooks"].([]any)
	if !ok {
		return raw, true
	}
	kept := make([]any, 0, len(handlers))
	for _, rawHandler := range handlers {
		if !isManagedHandler(rawHandler) {
			kept = append(kept, rawHandler)
		}
	}
	if len(kept) == len(handlers) {
		return raw, true
	}
	if len(kept) == 0 {
		return nil, false
	}
	copyEntry := make(map[string]any, len(entry))
	for key, value := range entry {
		copyEntry[key] = value
	}
	copyEntry["hooks"] = kept
	return copyEntry, true
}

func isManagedHandler(raw any) bool {
	handler, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	command, _ := handler["command"].(string)
	return strings.Contains(command, " verify --hook claude-code") || strings.Contains(command, "'verify' '--hook' 'claude-code'")
}

func genericGuard(binary string, opts Options) string {
	args := append(globalArgs(opts), "verify")
	quoted := []string{shellQuote(binary)}
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return "#!/bin/sh\n" +
		": \"${WORKLEASE_HANDLE:?set WORKLEASE_HANDLE to a private claim handle}\"\n" +
		": \"${WORKLEASE_RESOURCE:?set WORKLEASE_RESOURCE to the expected path resource}\"\n" +
		"exec " + strings.Join(quoted, " ") + " --handle \"$WORKLEASE_HANDLE\" --resource \"$WORKLEASE_RESOURCE\"\n"
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func readSafe(path, base string) ([]byte, bool, error) {
	if err := safePath(path, base); err != nil {
		return nil, false, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return nil, false, reason.New(reason.ReasonSetupConfigMalformed, "setup target is unsafe").With("path", path)
	}
	data, err := os.ReadFile(path)
	return data, true, err
}

func safePath(path, base string) error {
	if info, err := os.Lstat(base); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return reason.New(reason.ReasonSetupConfigMalformed, "setup scope is unsafe").With("path", base)
		}
	} else if !os.IsNotExist(err) {
		return reason.New(reason.ReasonSetupConfigMalformed, "setup scope is unsafe").With("path", base)
	}
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return reason.New(reason.ReasonSetupConfigMalformed, "setup target escapes its scope")
	}
	current := base
	parts := strings.Split(filepath.Dir(rel), string(filepath.Separator))
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			break
		}
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return reason.New(reason.ReasonSetupConfigMalformed, "setup target has an unsafe parent").With("path", current)
		}
	}
	return nil
}

func atomicReplace(path, base string, old []byte, existed bool, data []byte, before func()) error {
	if err := safePath(path, base); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if before != nil {
		before()
	}
	current, currentExists, err := readSafe(path, base)
	if err != nil {
		return err
	}
	if currentExists != existed || !bytes.Equal(current, old) {
		return reason.New(reason.ReasonSetupConfigMalformed, "setup target changed concurrently").With("path", path)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".worklease-setup-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	mode := os.FileMode(0o600)
	if existed {
		if info, e := os.Stat(path); e == nil {
			mode = info.Mode().Perm()
		}
	}
	if err = temp.Chmod(mode); err == nil {
		_, err = temp.Write(data)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	latest, latestExists, readErr := readSafe(path, base)
	if readErr != nil {
		return readErr
	}
	if latestExists != existed || !bytes.Equal(latest, old) {
		return reason.New(reason.ReasonSetupConfigMalformed, "setup target changed concurrently").With("path", path)
	}
	if err = os.Rename(tempName, path); err != nil {
		return err
	}
	if dir, e := os.Open(filepath.Dir(path)); e == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func unified(path string, old, next []byte) string {
	oldLines, newLines := bytes.Count(old, []byte("\n")), bytes.Count(next, []byte("\n"))
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n@@ -1,%d +1,%d @@\n", path, path, oldLines, newLines)
	for _, line := range strings.Split(strings.TrimSuffix(string(old), "\n"), "\n") {
		if len(old) > 0 {
			b.WriteString("-")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(next), "\n"), "\n") {
		b.WriteString("+")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func instructionsResult(version string) (Result, error) {
	if strings.TrimSpace(version) == "" {
		version = "dev"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- worklease:begin v%s -->\n", version)
	b.WriteString(`# Worklease coordination

Authority selection: <verified local state home or remote profile selection>
Work source: <authoritative source for eligibility and progress>
Resource convention: <exact shared canonical keys; include admitted prefixes for remote>

Fill these non-secret settings before using this block; do not guess missing values.
Read worklease instructions loop and worklease instructions safety before work.
Use the recorded authority and exact resources across all contenders, with distinct sessions.
Claim before work, heartbeat before half the TTL, stop on ownership loss, and release after verified progress.
Keep credentials, invitations, and private handles out of project instructions and logs.
`)
	b.WriteString("<!-- worklease:end -->\n")
	return Result{Preview: b.String()}, nil
}

func malformed(path string) error {
	return reason.New(reason.ReasonSetupConfigMalformed, "setup configuration must be a JSON object").With("path", path)
}
func malformedField(field string) error {
	return reason.New(reason.ReasonSetupConfigMalformed, "setup configuration field has the wrong type").With("field", field)
}
