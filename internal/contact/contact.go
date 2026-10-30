// Package contact looks up how to reach a claim holder through the channels
// already running beside it on this host. It is copied from bl's
// internal/contact built-in resolvers. Contact is resolved when a claim is
// viewed and never recorded, and Worklease never sends messages itself.
package contact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Budget bounds one Resolve call. A resolver still running when it ends
// reports unavailable: timeout.
const Budget = 100 * time.Millisecond

// outputLimit bounds what is read from one resolver or session file.
const outputLimit = 64 << 10

// Result is one resolver's answer: ok with channels, or unavailable with a
// reason.
type Result struct {
	Resolver string
	Status   string
	Reason   string
	Channels []Channel
}

// Channel is one way to reach the holder. Status is working, idle, waiting,
// or unknown.
type Channel struct {
	Channel string
	Handle  string
	Status  string
	Detail  string
}

// Resolve returns one result per applicable resolver for the holder of a
// harness session. Claims record no host, so a holder on another host matches
// no local channel. claude-peer runs only for the claude agent kind.
func Resolve(ctx context.Context, sessionID, agent string) []Result {
	if sessionID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, Budget)
	defer cancel()
	type resolver struct {
		name string
		run  func() Result
	}
	resolvers := []resolver{{"herdr", func() Result { return herdrResult(ctx, sessionID) }}}
	if agent == "claude" {
		resolvers = append(resolvers, resolver{"claude-peer", func() Result { return peerResult(sessionID) }})
	}
	type answer struct {
		index  int
		result Result
	}
	answers := make(chan answer, len(resolvers))
	for i, r := range resolvers {
		go func() { answers <- answer{i, r.run()} }()
	}
	out := make([]Result, len(resolvers))
	done := make([]bool, len(resolvers))
	for range resolvers {
		select {
		case a := <-answers:
			out[a.index], done[a.index] = a.result, true
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	for i, r := range resolvers {
		if !done[i] {
			out[i] = unavailable("timeout")
		}
		out[i].Resolver = r.name
	}
	return out
}

func unavailable(reason string) Result {
	return Result{Status: "unavailable", Reason: reason}
}

func ok(channels []Channel) Result {
	return Result{Status: "ok", Channels: channels}
}

type herdrAgent struct {
	AgentSession struct {
		Value string `json:"value"`
	} `json:"agent_session"`
	Status string `json:"agent_status"`
	PaneID string `json:"pane_id"`
	Title  string `json:"title"`
}

func herdrResult(ctx context.Context, sessionID string) Result {
	path, err := exec.LookPath("herdr")
	if err != nil {
		return unavailable("herdr not installed")
	}
	// Start in a minimal environment plus the socket of the Herdr session
	// Worklease runs in.
	cmd := exec.CommandContext(ctx, path, "agent", "list")
	for _, key := range []string{"PATH", "HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "HERDR_SOCKET_PATH"} {
		if value, set := os.LookupEnv(key); set {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	data, err := output(cmd)
	if ctx.Err() != nil {
		return unavailable("timeout")
	}
	if err != nil {
		return unavailable("herdr not running")
	}
	var listed struct {
		Result struct {
			Agents []herdrAgent `json:"agents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &listed); err != nil {
		return unavailable("unreadable herdr agent list")
	}
	var channels []Channel
	for _, a := range listed.Result.Agents {
		if a.AgentSession.Value == sessionID && a.PaneID != "" {
			status := map[string]string{"working": "working", "idle": "idle", "done": "idle", "blocked": "waiting"}[a.Status]
			channels = appendChannel(channels, Channel{Channel: "herdr", Handle: a.PaneID, Status: status, Detail: a.Title})
		}
	}
	return ok(channels)
}

// claudePeer holds the only fields read from Claude Code's undocumented
// session files. Key files and messaging sockets are never opened.
type claudePeer struct {
	SessionID    string `json:"sessionId"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Kind         string `json:"kind"`
	PeerProtocol int    `json:"peerProtocol"`
}

// peerProtocol is the Claude Code peer protocol version this resolver knows.
const peerProtocol = 1

func peerResult(sessionID string) Result {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return unavailable("claude sessions unreadable")
		}
		dir = filepath.Join(home, ".claude")
	}
	files, err := filepath.Glob(filepath.Join(dir, "sessions", "*.json"))
	if err != nil {
		return unavailable("claude sessions unreadable")
	}
	var channels []Channel
	for _, file := range files {
		data, err := readLimited(file)
		if err != nil {
			continue // the session exited while being listed
		}
		var p claudePeer
		if json.Unmarshal(data, &p) != nil || p.SessionID != sessionID {
			continue
		}
		if p.PeerProtocol != peerProtocol {
			return unavailable("unsupported version (peerProtocol " + strconv.Itoa(p.PeerProtocol) + ")")
		}
		if p.Name != "" {
			status := map[string]string{"busy": "working", "shell": "working", "idle": "idle", "waiting": "waiting"}[p.Status]
			channels = appendChannel(channels, Channel{Channel: "claude-peer", Handle: p.Name, Status: status, Detail: p.Kind})
		}
	}
	return ok(channels)
}

// appendChannel normalizes status and drops a channel whose fields could
// smuggle terminal control or are too long to be a handle.
func appendChannel(channels []Channel, c Channel) []Channel {
	for _, v := range []string{c.Channel, c.Handle, c.Detail} {
		if len(v) > 1024 || strings.ContainsFunc(v, unicode.IsControl) {
			return channels
		}
	}
	if c.Handle == "" {
		return channels
	}
	switch c.Status {
	case "working", "idle", "waiting":
	default:
		c.Status = "unknown"
	}
	return append(channels, c)
}

func output(cmd *exec.Cmd) ([]byte, error) {
	var stdout bytes.Buffer
	cmd.Stdout = &limitWriter{w: &stdout, n: outputLimit}
	// A backgrounded grandchild can hold stdout open past the deadline.
	cmd.WaitDelay = 10 * time.Millisecond
	err := cmd.Run()
	return stdout.Bytes(), err
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, outputLimit))
}

type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if len(p) > l.n {
		return 0, errors.New("output exceeds 64 KiB")
	}
	l.n -= len(p)
	return l.w.Write(p)
}
