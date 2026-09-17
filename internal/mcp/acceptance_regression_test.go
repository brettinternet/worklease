package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestQueuedToolCallWaitsBehindEightAndEOFIsBounded(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home, AgentID: "queue-test", PollInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.Call(context.Background(), "events", nil)
	if err != nil {
		t.Fatal(err)
	}
	cursor := events["structuredContent"].(map[string]any)["nextCursor"].(string)

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- s.Serve(context.Background(), stdinReader, stdoutWriter) }()
	for id := 1; id <= maxInFlight; id++ {
		_, err = fmt.Fprintf(stdinWriter, `{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"watch","arguments":{"cursor":%q,"timeout":0.2}},"_meta":{"protocolVersion":"%s"}}`+"\n", id, cursor, ModernVersion)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = fmt.Fprintf(stdinWriter, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"instructions","arguments":{"topic":"loop"}},"_meta":{"protocolVersion":"%s"}}`+"\n", ModernVersion)
	if err != nil {
		t.Fatal(err)
	}

	scanner := bufio.NewScanner(stdoutReader)
	seen := map[int]map[string]any{}
	for len(seen) < 9 && scanner.Scan() {
		var response map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			t.Fatalf("serialized response: %v: %q", err, scanner.Text())
		}
		id := int(response["id"].(float64))
		seen[id] = response
	}
	if len(seen) != 9 {
		t.Fatalf("received %d responses", len(seen))
	}
	if protocolErr, ok := seen[9]["error"].(map[string]any); ok {
		t.Fatalf("queued call failed instead of waiting: %v", protocolErr)
	}
	_ = stdinWriter.Close()
	_ = stdoutWriter.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("EOF shutdown exceeded bound")
	}
}

func TestDuplicateIDsLegacyNegotiationAndMalformedInput(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"same","method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","id":"same","method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"initialize","params":{"protocolVersion":"unsupported"}}`,
		`not-json`,
	}, "\n") + "\n"
	var output strings.Builder
	if err := s.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	var responses []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("response is not serialized JSON: %v", err)
		}
		responses = append(responses, response)
	}
	if len(responses) != 5 {
		t.Fatalf("got %d responses: %s", len(responses), output.String())
	}
	var legacyOK, duplicateOK, listOK, unsupportedOK, malformedOK bool
	for _, response := range responses {
		if result, ok := response["result"].(map[string]any); ok {
			legacyOK = legacyOK || result["protocolVersion"] == LegacyVersion
			if tools, ok := result["tools"].([]any); ok {
				listOK = len(tools) == 11
			}
		}
		if rpcErr, ok := response["error"].(map[string]any); ok {
			duplicateOK = duplicateOK || rpcErr["message"] == "duplicate request ID"
			unsupportedOK = unsupportedOK || (rpcErr["message"] == "unsupported protocol version" && rpcErr["data"] != nil)
			malformedOK = malformedOK || rpcErr["code"] == float64(-32700)
		}
	}
	if !legacyOK || !duplicateOK || !listOK || !unsupportedOK || !malformedOK {
		t.Fatalf("protocol coverage missing legacy=%v duplicate=%v list=%v unsupported=%v malformed=%v: %s", legacyOK, duplicateOK, listOK, unsupportedOK, malformedOK, output.String())
	}
}

func TestOversizedInputAndExactElevenToolSchemas(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	oversized := strings.Repeat("x", maxMessageBytes+1) + "\n"
	if err := s.Serve(context.Background(), strings.NewReader(oversized), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "message is too large") {
		t.Fatalf("oversized input response: %s", output.String())
	}

	schemas := s.tools()
	if len(schemas) != len(toolOrder) {
		t.Fatalf("got %d tool schemas", len(schemas))
	}
	for i, name := range toolOrder {
		if schemas[i]["name"] != name {
			t.Fatalf("tool %d = %v, want %s", i, schemas[i]["name"], name)
		}
		input := schemas[i]["inputSchema"].(map[string]any)
		if input["type"] != "object" || input["additionalProperties"] != false || input["properties"] == nil {
			t.Fatalf("incomplete schema for %s: %v", name, input)
		}
	}
}

func TestReferencesCrossServerPendingRecoveryAndRestartHold(t *testing.T) {
	home, _ := testkit.Home(t)
	cliPath := filepath.Join(t.TempDir(), "worklease")
	buildCommand := exec.Command("go", "build", "-o", cliPath, "../../cmd/worklease")
	buildCommand.Dir = "."
	if buildOutput, err := buildCommand.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, buildOutput)
	}
	first, err := NewServer(Options{Home: home, AgentID: "first", SessionID: "session-one"})
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

	second, err := NewServer(Options{Home: home, AgentID: "second", SessionID: "session-two"})
	if err != nil {
		t.Fatal(err)
	}
	if status, err := second.Call(context.Background(), "status", map[string]any{"lease": ref}); err != nil || status["isError"] == true {
		t.Fatalf("second server could not use reference: %v %v", status, err)
	}
	verifyCommand := exec.Command(cliPath, "--home", home, "--json", "verify", "--handle", path)
	verifyCommand.Dir = "."
	verifyOutput, err := verifyCommand.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI verify of MCP handle: %v: %s", err, verifyOutput)
	}
	if strings.Contains(string(verifyOutput), `"token"`) || strings.Contains(string(verifyOutput), `"tokenHash"`) {
		t.Fatalf("CLI verify leaked private state: %s", verifyOutput)
	}
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(time.Hour)
	heartbeatID := strings.Repeat("1", 32)
	heartbeatInputs := map[string]any{"kind": "heartbeat", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": int64(time.Hour / time.Microsecond), "requestNotAfter": deadline.UnixMicro()}
	h.State = "pending"
	h.PendingRequest = &handle.PendingRequest{OperationID: heartbeatID, Kind: "heartbeat", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: hashValue(heartbeatInputs), RequestNotAfter: deadline, Inputs: heartbeatInputs}
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

	releaseID := strings.Repeat("2", 32)
	releaseInputs := map[string]any{"kind": "release", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "reason": "recovered release", "requestNotAfter": deadline.UnixMicro()}
	h.State = "pending"
	h.PendingRequest = &handle.PendingRequest{OperationID: releaseID, Kind: "release", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: hashValue(releaseInputs), RequestNotAfter: deadline, Inputs: releaseInputs}
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

func TestAutomaticHeartbeatHasOneOwnerAndDoesNotResumeAfterRestart(t *testing.T) {
	home, _ := testkit.Home(t)
	first, err := NewServer(Options{Home: home, AgentID: "renew-owner"})
	if err != nil {
		t.Fatal(err)
	}
	acquired, err := first.Call(context.Background(), "acquire", map[string]any{"resources": []any{"renewed-resource"}, "ttl": float64(1), "maxHold": float64(60)})
	if err != nil {
		t.Fatal(err)
	}
	content := acquired["structuredContent"].(map[string]any)
	ref, path := content["lease"].(string), content["handlePath"].(string)
	initial, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var renewed handle.Handle
	for time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
		renewed, err = handle.Read(path)
		if err == nil && renewed.Revision > initial.Revision {
			break
		}
	}
	if renewed.Revision <= initial.Revision || renewed.ExpiresAt.After(renewed.HoldUntil) {
		t.Fatalf("automatic heartbeat did not renew within hold: initial=%d current=%d expires=%s hold=%s", initial.Revision, renewed.Revision, renewed.ExpiresAt, renewed.HoldUntil)
	}
	first.Close()
	time.Sleep(100 * time.Millisecond)
	stopped, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewServer(Options{Home: home, AgentID: "restart"})
	if err != nil {
		t.Fatal(err)
	}
	if status, err := second.Call(context.Background(), "status", map[string]any{"lease": ref}); err != nil || status["isError"] == true {
		t.Fatalf("restart could not read reference: %v %v", status, err)
	}
	time.Sleep(650 * time.Millisecond)
	afterRestart, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if afterRestart.Revision != stopped.Revision {
		t.Fatalf("restart resumed automatic renewal: %d -> %d", stopped.Revision, afterRestart.Revision)
	}
	second.Close()
}

func TestEndToEndDiscoveredClientUsesOnlyLeaseReference(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home, AgentID: "e2e-client", PollInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient(t, s)
	defer client.close(t)

	discovery := client.request("server/discover", nil, false)
	if discovery["result"].(map[string]any)["protocolVersion"] != ModernVersion {
		t.Fatalf("discovery: %v", discovery)
	}
	listed := client.request("tools/list", nil, true)
	if len(listed["result"].(map[string]any)["tools"].([]any)) != 11 {
		t.Fatalf("tools/list: %v", listed)
	}
	acquire := client.tool("acquire", map[string]any{"resources": []any{"e2e-resource"}})
	leaseRef := acquire["lease"].(string)
	if len(leaseRef) != 32 {
		t.Fatalf("lease reference: %q", leaseRef)
	}
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"status", map[string]any{"lease": leaseRef}},
		{"verify", map[string]any{"lease": leaseRef}},
		{"checkpoint", map[string]any{"lease": leaseRef, "data": map[string]any{"phase": "e2e"}}},
	} {
		client.tool(call.name, call.args)
	}
	events := client.tool("events", map[string]any{"limit": float64(10)})
	cursor := events["nextCursor"].(string)
	client.tool("watch", map[string]any{"cursor": cursor, "timeout": float64(0.01)})
	client.tool("release", map[string]any{"lease": leaseRef})
}

type testClient struct {
	t      *testing.T
	input  *io.PipeWriter
	output *bufio.Scanner
	done   chan error
	nextID int
}

func newTestClient(t *testing.T, server *Server) *testClient {
	t.Helper()
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), inputReader, outputWriter) }()
	return &testClient{t: t, input: inputWriter, output: bufio.NewScanner(outputReader), done: done}
}

func (c *testClient) request(method string, params any, modern bool) map[string]any {
	c.t.Helper()
	c.nextID++
	request := map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method}
	if params != nil {
		request["params"] = params
	}
	if modern {
		request["_meta"] = map[string]any{"protocolVersion": ModernVersion}
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := c.input.Write(append(encoded, '\n')); err != nil {
		c.t.Fatal(err)
	}
	if !c.output.Scan() {
		c.t.Fatal("MCP server closed before response")
	}
	var response map[string]any
	if err := json.Unmarshal(c.output.Bytes(), &response); err != nil {
		c.t.Fatal(err)
	}
	if response["error"] != nil {
		c.t.Fatalf("protocol error for %s: %v", method, response["error"])
	}
	return response
}

func (c *testClient) tool(name string, arguments map[string]any) map[string]any {
	c.t.Helper()
	response := c.request("tools/call", map[string]any{"name": name, "arguments": arguments}, true)
	result := response["result"].(map[string]any)
	if result["isError"] == true {
		c.t.Fatalf("tool %s failed: %v", name, result)
	}
	return result["structuredContent"].(map[string]any)
}

func (c *testClient) close(t *testing.T) {
	t.Helper()
	_ = c.input.Close()
	select {
	case err := <-c.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("MCP server did not stop on EOF")
	}
}

func TestRejectedCheckpointDoesNotStrandLease(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home, AgentID: "checkpoint-test"})
	if err != nil {
		t.Fatal(err)
	}
	acquired, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []any{"checkpoint-resource"}, "autoHeartbeat": false})
	if err != nil {
		t.Fatal(err)
	}
	content := acquired["structuredContent"].(map[string]any)
	ref := content["lease"].(string)
	path := content["handlePath"].(string)
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := s.Call(context.Background(), "checkpoint", map[string]any{"lease": ref, "data": map[string]any{"value": h.Token}})
	if err != nil {
		t.Fatal(err)
	}
	if bad["isError"] != true || strings.Contains(jsonText(bad), h.Token) {
		t.Fatalf("checkpoint rejection missing or leaked credential: %v", bad)
	}
	after, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "ready" || after.PendingRequest != nil {
		t.Fatalf("rejected input stranded handle: state=%s pending=%v", after.State, after.PendingRequest)
	}
	released, err := s.Call(context.Background(), "release", map[string]any{"lease": ref})
	if err != nil || released["isError"] == true {
		t.Fatalf("release after rejected checkpoint: %v %v", released, err)
	}
}
