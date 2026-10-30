package contact_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/brettinternet/worklease/internal/contact"
)

func put(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

// Contact must reach live holders without exposing secrets: Claude Code's key
// files stay unread, hostile handles never reach the terminal, and one slow or
// unknown source never hides another. It changes PATH, so it is not parallel.
func TestResolve(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fakes herdr with a shell script")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	put(t, filepath.Join(bin, "herdr"), `#!/bin/sh
echo '{"result":{"agents":[{"agent":"claude","agent_session":{"value":"s-1"},"agent_status":"blocked","pane_id":"w1:p2","title":"claude · work"},{"agent_session":{"value":"s-1"},"agent_status":"idle","pane_id":"bad\u001b]0;x\u0007"},{"agent_session":{"value":"other"},"agent_status":"idle","pane_id":"w1:p3"}]}}'
`, 0o755)
	claude := filepath.Join(root, "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	put(t, filepath.Join(claude, "sessions", "10.json"), `{"pid":10,"sessionId":"s-1","name":"worklease-2b","status":"busy","kind":"interactive","peerProtocol":1,"messagingSocketPath":"/tmp/sock"}`, 0o644)
	put(t, filepath.Join(claude, "sessions", "10.abc.key"), "secret", 0o000)
	put(t, filepath.Join(claude, "sessions", "11.json"), `{"sessionId":"s-2","name":"future","peerProtocol":2}`, 0o644)

	herdr := contact.Result{Resolver: "herdr", Status: "ok", Channels: []contact.Channel{{Channel: "herdr", Handle: "w1:p2", Status: "waiting", Detail: "claude · work"}}}
	for _, tc := range []struct {
		session, agent string
		want           []contact.Result
	}{
		{"s-1", "claude", []contact.Result{herdr, {Resolver: "claude-peer", Status: "ok", Channels: []contact.Channel{{Channel: "claude-peer", Handle: "worklease-2b", Status: "working", Detail: "interactive"}}}}},
		{"s-2", "claude", []contact.Result{{Resolver: "herdr", Status: "ok"}, {Resolver: "claude-peer", Status: "unavailable", Reason: "unsupported version (peerProtocol 2)"}}},
		{"s-1", "codex", []contact.Result{herdr}},
	} {
		if got := contact.Resolve(context.Background(), tc.session, tc.agent); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Resolve(%s, %s):\n got %+v\nwant %+v", tc.session, tc.agent, got, tc.want)
		}
	}

	// A hung herdr reports timeout within the budget and leaves claude-peer intact.
	put(t, filepath.Join(bin, "herdr"), "#!/bin/sh\nsleep 5\n", 0o755)
	got := contact.Resolve(context.Background(), "s-1", "claude")
	if len(got) != 2 || !reflect.DeepEqual(got[0], contact.Result{Resolver: "herdr", Status: "unavailable", Reason: "timeout"}) || got[1].Status != "ok" {
		t.Fatalf("hung herdr: %+v", got)
	}
}
