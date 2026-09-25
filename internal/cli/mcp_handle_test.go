package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/mcp"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestReferencesCrossServerPendingRecoveryAndRestartHold(t *testing.T) {
	t.Parallel()
	home, _ := testkit.Home(t)
	first, err := mcp.NewServer(mcp.Options{Home: home, AgentID: "first", SessionID: "session-one"})
	if err != nil {
		t.Fatal(err)
	}
	acquired, err := first.Call(context.Background(), "acquire", map[string]any{"resources": []any{"shared-reference"}, "ttl": float64(10), "maxHold": float64(60), "autoHeartbeat": false})
	if err != nil {
		t.Fatal(err)
	}
	content := acquired["structuredContent"].(map[string]any)
	ref, path := content["lease"].(string), content["handlePath"].(string)
	first.Close()

	second, err := mcp.NewServer(mcp.Options{Home: home, AgentID: "second", SessionID: "session-two"})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if status, err := second.Call(context.Background(), "status", map[string]any{"lease": ref}); err != nil || status["isError"] == true {
		t.Fatalf("second server could not use reference: %v %v", status, err)
	}
	verified := testkit.RunCLI(context.Background(), []string{"worklease", "--home", home, "--json", "verify", "--handle", path}, func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		return Run(ctx, args, "dev", "unknown", "unknown", stdout, stderr)
	})
	if verified.Err != nil {
		t.Fatalf("CLI verify of MCP handle: %v: %s", verified.Err, verified.Stdout)
	}
	if strings.Contains(string(verified.Stdout), `"token"`) || strings.Contains(string(verified.Stdout), `"tokenHash"`) {
		t.Fatalf("CLI verify leaked private state: %s", verified.Stdout)
	}
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(time.Hour)
	heartbeatInputs := map[string]any{"kind": "heartbeat", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": int64(time.Hour / time.Microsecond), "requestNotAfter": deadline.UnixMicro()}
	h.State = "pending"
	h.PendingRequest = &handle.PendingRequest{OperationID: strings.Repeat("1", 32), Kind: "heartbeat", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: mcpHandleHash(heartbeatInputs), RequestNotAfter: deadline, Inputs: heartbeatInputs}
	if err := handle.Write(path, h); err != nil {
		t.Fatal(err)
	}
	heartbeat, err := second.Call(context.Background(), "heartbeat", map[string]any{"lease": ref})
	if err != nil || heartbeat["isError"] == true {
		t.Fatalf("pending heartbeat recovery: %v %v", heartbeat, err)
	}
	h, err = handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.State != "ready" || h.PendingRequest != nil || h.ExpiresAt.After(h.HoldUntil) {
		t.Fatalf("heartbeat recovery state=%s pending=%v expires=%s hold=%s", h.State, h.PendingRequest, h.ExpiresAt, h.HoldUntil)
	}

	releaseInputs := map[string]any{"kind": "release", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "reason": "recovered release", "requestNotAfter": deadline.UnixMicro()}
	h.State = "pending"
	h.PendingRequest = &handle.PendingRequest{OperationID: strings.Repeat("2", 32), Kind: "release", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: mcpHandleHash(releaseInputs), RequestNotAfter: deadline, Inputs: releaseInputs}
	if err := handle.Write(path, h); err != nil {
		t.Fatal(err)
	}
	released, err := second.Call(context.Background(), "release", map[string]any{"lease": ref})
	if err != nil || released["isError"] == true {
		t.Fatalf("pending release recovery: %v %v", released, err)
	}
	if _, err := handle.Read(path); err == nil {
		t.Fatal("released MCP handle still exists")
	}
}

func TestSubprocessStdioLifecycle(t *testing.T) {
	t.Parallel()
	home, _ := testkit.Home(t)
	binary := filepath.Join(t.TempDir(), "worklease")
	if err := os.Symlink(os.Args[0], binary); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "mcp")
	cmd.Env = append(os.Environ(), "WORKLEASE_HOME="+home, "WORKLEASE_AGENT_ID=subprocess")
	cmd.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"server/discover\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("subprocess: %v stderr=%s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	var responses []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		var value map[string]any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, value)
	}
	if len(responses) != 2 {
		t.Fatalf("responses: %s", stdout.String())
	}
}

func mcpHandleHash(value any) string {
	encoded, _ := json.Marshal(value)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}
