package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/instructions"
	"github.com/brettinternet/worklease/internal/lease"

	"github.com/brettinternet/worklease/internal/testkit"
)

func TestCallLifecycleAndRedaction(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home, AgentID: "test-agent"})
	if err != nil {
		t.Fatal(err)
	}
	acquired, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []any{"mcp-resource"}, "ttl": float64(2), "maxHold": float64(60), "autoHeartbeat": false})
	if err != nil {
		t.Fatal(err)
	}
	if acquired["structuredContent"] == nil {
		t.Fatal("missing structured content")
	}
	content := acquired["structuredContent"].(map[string]any)
	ref := content["lease"].(string)
	if len(ref) != 32 || strings.Contains(strings.ToLower(jsonText(acquired)), "token") {
		t.Fatal("private credential leaked")
	}
	status, err := s.Call(context.Background(), "status", map[string]any{"lease": ref})
	if err != nil || status["structuredContent"] == nil {
		t.Fatalf("status: %v", err)
	}
	if _, err = s.Call(context.Background(), "checkpoint", map[string]any{"lease": ref, "data": map[string]any{"step": float64(1)}}); err != nil {
		t.Fatal(err)
	}
	released, err := s.Call(context.Background(), "release", map[string]any{"lease": ref, "reason": "complete"})
	if err != nil {
		t.Fatal(err)
	}
	if released["isError"] == true {
		t.Fatalf("release failed: %s", jsonText(released))
	}
}

func TestSubprocessStdioLifecycle(t *testing.T) {
	home, _ := testkit.Home(t)
	cmd := exec.Command("go", "run", "../../cmd/worklease", "mcp")
	cmd.Dir = "."
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

func TestMCPArgumentTypesHoldCeilingAndCanonicalInstructions(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home, AgentID: "test-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(serverInstructions, "uncertain outcome") || !strings.Contains(serverInstructions, "definitive failure") {
		t.Fatalf("server acquire recovery instructions are incomplete: %q", serverInstructions)
	}
	acquireDescription := ""
	var acquireSchema map[string]any
	for _, tool := range s.tools() {
		if tool["name"] == "acquire" {
			acquireDescription, _ = tool["description"].(string)
			acquireSchema, _ = tool["inputSchema"].(map[string]any)
		}
	}
	if !strings.Contains(acquireDescription, "Retry by reference only after an uncertain outcome") || !strings.Contains(acquireDescription, "definitive failure returns no reference") {
		t.Fatalf("acquire tool recovery description is incomplete: %q", acquireDescription)
	}
	leaseReplay := acquireSchema["oneOf"].([]any)[0].(map[string]any)
	if leaseReplay["maxProperties"] != 1 {
		t.Fatalf("acquire lease replay schema accepts changed inputs: %v", leaseReplay)
	}
	bad, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []any{"r"}, "path": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if bad["isError"] != true {
		t.Fatalf("mixed resource modes accepted: %v", bad)
	}
	bad, err = s.Call(context.Background(), "acquire", map[string]any{"resources": []any{"r"}, "autoHeartbeat": "false"})
	if err != nil {
		t.Fatal(err)
	}
	if bad["isError"] != true {
		t.Fatalf("invalid boolean accepted: %v", bad)
	}
	acquired, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []any{"r"}, "ttl": float64(2), "maxHold": float64(60), "autoHeartbeat": false})
	if err != nil {
		t.Fatal(err)
	}
	content := acquired["structuredContent"].(map[string]any)
	h, err := handle.Read(content["handlePath"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if h.ExpiresAt.After(h.HoldUntil) {
		t.Fatalf("expiry exceeds hold: %s > %s", h.ExpiresAt, h.HoldUntil)
	}
	for _, topic := range instructions.Topics() {
		got, err := s.Call(context.Background(), "instructions", map[string]any{"topic": topic})
		if err != nil {
			t.Fatal(err)
		}
		lines := got["structuredContent"].(map[string]any)["lines"]
		want, _ := instructions.For(topic)
		if fmt.Sprint(lines) != fmt.Sprint(want) {
			t.Fatalf("instructions %s differ: %v != %v", topic, lines, want)
		}
	}

	bundle, err := s.open(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	authority := bundle.st.AuthorityID()
	ref, claim, token := strings.Repeat("f", 32), strings.Repeat("e", 32), strings.Repeat("a", 64)
	deadline := time.Now().UTC().Add(time.Hour)
	legacyGrant, err := bundle.svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: authority, ClaimID: claim, Token: token, Resources: []string{"recovery-resource"}, AgentID: "agent", SessionID: "session", WorkKey: "recovery-resource", TTL: 2 * time.Second, RequestNotAfter: deadline, LocalReplaceAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = bundle.st.Close()
	inputs := map[string]any{"kind": "acquire", "authorityId": authority, "claimId": claim, "resources": []string{"recovery-resource"}, "agentId": "agent", "sessionId": "session", "workKey": "recovery-resource", "ttl": int64(2 * time.Second / time.Microsecond), "wait": int64(0), "requestNotAfter": deadline.UnixMicro(), "coordinationOnly": false, "localReplaceAllowed": true}
	pending := handle.Handle{SchemaVersion: 1, AuthorityID: authority, ClaimID: claim, Token: token, Resources: []string{"recovery-resource"}, AgentID: "agent", SessionID: "session", LocalReplaceAllowed: true, HoldUntil: time.Now().UTC().Add(time.Minute), State: "pending", PendingRequest: &handle.PendingRequest{OperationID: claim, Kind: "acquire", AuthorityID: authority, ClaimID: claim, RequestHash: legacyGrant.Receipt.RequestHash, RequestNotAfter: deadline, Inputs: inputs}}
	if err := handle.Write(s.handlePath(ref), pending); err != nil {
		t.Fatal(err)
	}
	changed, err := s.Call(context.Background(), "acquire", map[string]any{"lease": ref, "ttl": float64(60)})
	if err != nil {
		t.Fatal(err)
	}
	if changed["isError"] != true {
		t.Fatalf("pending acquire replay accepted changed ttl: %v", changed)
	}
	replayed, err := s.Call(context.Background(), "acquire", map[string]any{"lease": ref})
	if err != nil || replayed["isError"] == true {
		t.Fatalf("pending acquire recovery: %v %v", replayed, err)
	}
}

func TestMCPAcquireFixesHoldDeadlineBeforeWaiting(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home, AgentID: "waiter"})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := s.open(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(time.Hour)
	_, err = bundle.svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: bundle.st.AuthorityID(), ClaimID: strings.Repeat("1", 32), Token: strings.Repeat("a", 64), Resources: []string{"waited-resource"}, AgentID: "holder", SessionID: "holder", TTL: time.Second, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	_ = bundle.st.Close()

	started := time.Now().UTC()
	acquired, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []any{"waited-resource"}, "ttl": float64(2), "wait": float64(2), "maxHold": float64(60), "autoHeartbeat": false})
	if err != nil || acquired["isError"] == true {
		t.Fatalf("waited acquire: %v %v", acquired, err)
	}
	content := acquired["structuredContent"].(map[string]any)
	h, err := handle.Read(content["handlePath"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if h.HoldUntil.After(started.Add(60*time.Second + 500*time.Millisecond)) {
		t.Fatalf("hold deadline moved with wait: started=%s hold=%s", started, h.HoldUntil)
	}
}

func TestServeCancellationClosesIdleInputAndBrokenOutput(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, reader, io.Discard) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after cancellation")
	}
	_ = writer.Close()

	broken, err := NewServer(Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	var input strings.Reader = *strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"server/discover\"}\n")
	if err := broken.Serve(context.Background(), &input, failWriter{}); err != nil {
		t.Fatal(err)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestStdioNegotiationSurfaceAndProtocolErrors(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"server/discover"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","_meta":{"protocolVersion":"2026-07-28"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"nope"}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("responses: %q", out.String())
	}
	responses := map[string]map[string]any{}
	for _, line := range lines {
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatal(err)
		}
		responses[fmt.Sprint(response["id"])] = response
	}
	discover := responses["1"]
	if discover["error"] != nil {
		t.Fatalf("discover failed: %v", discover)
	}
	list := responses["2"]
	result := list["result"].(map[string]any)
	if len(result["tools"].([]any)) != 11 {
		t.Fatalf("tool count: %v", result["tools"])
	}
	if responses["3"]["error"] == nil {
		t.Fatal("unknown method was not rejected")
	}
}
