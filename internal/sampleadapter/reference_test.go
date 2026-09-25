package sampleadapter

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReferenceAdapterWritesReceiptsAndRecoversMarkedAppend(t *testing.T) {
	t.Parallel()
	storePath := writeReferenceTestFixture(t, referenceTestFixture())
	call := startReferenceTestAdapter(t, storePath)
	ref := map[string]string{"sourceId": "reference", "itemId": "reference-1"}

	state := call("writeState", map[string]any{
		"ref": ref, "operationId": "state-op", "patch": map[string]string{"status": "Doing"},
		"expectedVersion": "fixture-v1", "authority": map[string]string{"authorizationRef": "state-op", "scope": "reference"},
	})
	assertReferenceReceipt(t, state, "writeState", "fixture-v2")
	stateReadback := call("readReceipt", map[string]any{
		"sourceId": "reference", "operation": "writeState", "operationId": "state-op", "target": ref,
		"intent": map[string]any{"payload": map[string]string{"status": "Doing"}, "expectedVersion": "fixture-v1"}, "receipt": nil,
	})
	if stateReadback["verification"] != "unknown" {
		t.Fatalf("receipt-less state attribution = %#v", stateReadback)
	}

	operationID := "0123456789abcdef0123456789abcdef"
	progressPatch := map[string]string{"append": "comment", "content": "fixture progress", "marker": "worklease-op:" + operationID}
	progress := call("recordProgress", map[string]any{
		"ref": ref, "operationId": operationID, "patch": progressPatch,
		"expectedVersion": "fixture-v2", "authority": map[string]string{"authorizationRef": operationID, "scope": "reference"},
	})
	progressReceipt := assertReferenceReceipt(t, progress, "recordProgress", "fixture-v3")
	readback := call("readReceipt", map[string]any{
		"sourceId": "reference", "operation": "recordProgress", "operationId": operationID,
		"target": ref, "intent": map[string]any{"payload": progressPatch, "expectedVersion": "fixture-v2"}, "receipt": nil,
	})
	if readback["verification"] != "verified" {
		t.Fatalf("marker recovery = %#v", readback)
	}
	evidence := readback["evidence"].(map[string]any)
	if evidence["operationId"] != operationID || evidence["markerCount"] != float64(1) || evidence["appendProof"] != true || evidence["appendContent"] != "fixture progress" || evidence["receiptId"] != progressReceipt["durableLocation"] {
		t.Fatalf("marker evidence = %#v", evidence)
	}

	assignment := call("assign", map[string]any{
		"ref": ref, "operationId": "assign-op", "patch": map[string]string{"assignee": "alice"},
		"expectedVersion": "fixture-v3", "authority": map[string]string{"authorizationRef": "assign-op", "scope": "reference"},
	})
	assignReceipt := assertReferenceReceipt(t, assignment, "assign", "fixture-v4")
	replayed := call("assign", map[string]any{
		"ref": ref, "operationId": "assign-op", "patch": map[string]string{"assignee": "alice"},
		"expectedVersion": "fixture-v3", "authority": map[string]string{"authorizationRef": "assign-op", "scope": "reference"},
	})
	if got := replayed["receipt"].(map[string]any)["durableLocation"]; got != assignReceipt["durableLocation"] {
		t.Fatalf("same operation did not return its original receipt: %#v", replayed)
	}

	contents, err := readReferenceFixture(storePath)
	if err != nil || contents.Version != 4 || len(contents.Writes) != 3 || len(contents.Items[0].Comments) != 1 || len(contents.Items[0].AssignedTo) != 1 || contents.Items[0].AssignedTo[0] != "alice" {
		t.Fatalf("fixture after writes: %+v error=%v", contents, err)
	}
}

func TestReferenceAdapterReceiptUnknownWhenMarkerAttributionIsAmbiguous(t *testing.T) {
	t.Parallel()
	contents := referenceTestFixture()
	contents.Items[0].Comments = []fixtureComment{
		{OperationID: "ambiguous-op", Body: "progress\n\nworklease-op:ambiguous-op", Author: "alice", ReceiptID: "fixture://reference/reference-1/ambiguous-op"},
		{OperationID: "other-op", Body: "copied progress\n\nworklease-op:ambiguous-op", Author: "bob", ReceiptID: "fixture://reference/reference-1/other-op"},
	}
	storePath := writeReferenceTestFixture(t, contents)
	call := startReferenceTestAdapter(t, storePath)
	result := call("readReceipt", map[string]any{
		"sourceId": "reference", "operation": "recordProgress", "operationId": "ambiguous-op",
		"target":  map[string]string{"sourceId": "reference", "itemId": "reference-1"},
		"intent":  map[string]any{"payload": map[string]string{"append": "comment", "content": "progress", "marker": "worklease-op:ambiguous-op"}, "expectedVersion": nil},
		"receipt": nil,
	})
	if result["verification"] != "unknown" {
		t.Fatalf("ambiguous marker verification = %#v", result)
	}
	evidence := result["evidence"].(map[string]any)
	if evidence["markerCount"] != float64(2) || evidence["appendProof"] != false {
		t.Fatalf("ambiguous marker evidence = %#v", evidence)
	}
}

func TestReferenceAdapterCustomCompletionIsTerminal(t *testing.T) {
	t.Parallel()
	contents := referenceTestFixture()
	contents.Transitions["complete"] = "Shipped"
	call := startReferenceTestAdapter(t, writeReferenceTestFixture(t, contents))
	ref := map[string]string{"sourceId": "reference", "itemId": "reference-1"}
	call("writeState", map[string]any{"ref": ref, "operationId": "ship-op", "patch": map[string]string{"status": "Shipped"}, "expectedVersion": "fixture-v1", "authority": map[string]string{"authorizationRef": "ship-op", "scope": "reference"}})
	outcome := call("readItem", map[string]any{"ref": ref})["outcome"].(map[string]any)
	item := outcome["item"].(map[string]any)
	if item["state"] != "complete" || item["terminal"] != true {
		t.Fatalf("configured completion = %#v", item)
	}
}

func TestReferenceAdapterRejectedRebindKeepsOriginalStore(t *testing.T) {
	t.Parallel()
	first := writeReferenceTestFixture(t, referenceTestFixture())
	second := writeReferenceTestFixture(t, referenceTestFixture())
	server, err := newServer(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	server.writable = true
	if _, failure := server.resolve(map[string]json.RawMessage{"sourceId": mustMarshal("reference"), "config": mustMarshal(map[string]string{"fixturePath": first})}); failure != nil {
		t.Fatalf("initial resolve: %+v", failure)
	}
	for _, attempt := range []struct{ source, path string }{{"other", second}, {"reference", second}} {
		if _, failure := server.resolve(map[string]json.RawMessage{"sourceId": mustMarshal(attempt.source), "config": mustMarshal(map[string]string{"fixturePath": attempt.path})}); failure == nil {
			t.Fatalf("accepted rebind to %+v", attempt)
		}
	}
	if server.fixturePath != first || server.resolvedID != "reference" {
		t.Fatalf("rejected rebind altered store: path=%q source=%q", server.fixturePath, server.resolvedID)
	}
}

func TestReferenceAdapterRejectsSymlinkStore(t *testing.T) {
	t.Parallel()
	original := writeReferenceTestFixture(t, referenceTestFixture())
	linked := filepath.Join(t.TempDir(), "linked.json")
	if err := os.Symlink(original, linked); err != nil {
		t.Fatal(err)
	}
	server, err := newServer(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	server.writable = true
	if _, failure := server.resolve(map[string]json.RawMessage{"sourceId": mustMarshal("reference"), "config": mustMarshal(map[string]string{"fixturePath": linked})}); failure == nil {
		t.Fatal("accepted a symlink-backed store whose link would be replaced on write")
	}
}

func TestReferenceAdapterSmallBudgetDoesNotDenyDurableWrite(t *testing.T) {
	t.Parallel()
	path := writeReferenceTestFixture(t, referenceTestFixture())
	server, err := newServer(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	server.writable, server.fixturePath, server.resolvedID = true, path, "reference"
	operationID := "budget-op"
	result, failure := server.dispatchReferenceWrite(context.Background(), "recordProgress", map[string]json.RawMessage{
		"ref": mustMarshal(workRef{"reference", "reference-1"}), "operationId": mustMarshal(operationID),
		"patch":     mustMarshal(map[string]string{"append": "comment", "content": "budget test", "marker": "worklease-op:" + operationID}),
		"authority": mustMarshal(map[string]string{"authorizationRef": operationID, "scope": "reference"}),
	}, requestBudget{MaxItems: 1, MaxBytes: 1})
	if result != nil || failure == nil || failure.failure.Data.Diagnostic != "unknown-outcome" {
		t.Fatalf("post-write response = %#v, failure=%+v", result, failure)
	}
	contents, err := readReferenceFixture(path)
	if err != nil || len(contents.Writes) != 1 || len(contents.Items[0].Comments) != 1 {
		t.Fatalf("durable effect = %+v, error=%v", contents, err)
	}
}

func referenceTestFixture() fixture {
	return fixture{
		Principal: "alice", Version: 1,
		Transitions: map[string]string{"start": "Doing", "blocked": "Blocked", "review": "Review", "complete": "Done", "reopen": "Open"},
		Items: []fixtureItem{{
			ID: "reference-1", Title: "Reference item", RawStatus: "Open", State: "open", Order: "1", Priority: 1,
			ProviderReady: true, AssignedTo: []string{}, UpdatedAt: "2025-01-02T03:04:05Z", Body: "fixture body",
		}},
		InaccessibleItemIDs: []string{}, Dependencies: []fixtureEdge{}, Writes: []fixtureWrite{},
	}
}

func writeReferenceTestFixture(t *testing.T, contents fixture) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := saveReferenceFixture(path, contents); err != nil {
		t.Fatal(err)
	}
	return path
}

func startReferenceTestAdapter(t *testing.T, storePath string) func(string, map[string]any) map[string]any {
	t.Helper()
	client, host := net.Pipe()
	adapter, err := newServer(host)
	if err != nil {
		t.Fatal(err)
	}
	adapter.writable = true
	serveDone := make(chan error, 1)
	go func() { serveDone <- adapter.serve(host) }()
	t.Cleanup(func() {
		_ = client.Close()
		if err := <-serveDone; err != nil {
			t.Errorf("serve reference adapter: %v", err)
		}
	})
	reader := bufio.NewReader(client)
	call := func(method string, values map[string]any) map[string]any {
		t.Helper()
		params := values
		if method != "initialize" {
			params["deadline"] = time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
			params["budget"] = map[string]int{"maxItems": 10, "maxBytes": maxBytes}
		}
		request := map[string]any{"jsonrpc": "2.0", "id": method + "-" + time.Now().Format("150405.000000000"), "method": method, "params": params}
		frame, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Write(append(frame, '\n')); err != nil {
			t.Fatal(err)
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var response map[string]any
		if err := json.Unmarshal(line, &response); err != nil {
			t.Fatalf("decode response %q: %v", line, err)
		}
		if response["error"] != nil {
			t.Fatalf("%s error response: %#v", method, response["error"])
		}
		if result, ok := response["result"].(map[string]any); ok {
			return result
		}
		t.Fatalf("%s response has no result: %#v", method, response)
		return nil
	}
	call("initialize", map[string]any{"protocolMajors": []int{1}, "hostFeatures": []string{}})
	call("resolve", map[string]any{"sourceId": "reference", "config": map[string]string{"fixturePath": storePath}})
	return call
}

func assertReferenceReceipt(t *testing.T, result map[string]any, operation, version string) map[string]any {
	t.Helper()
	receipt, ok := result["receipt"].(map[string]any)
	if !ok || receipt["operation"] != operation || receipt["sourceId"] != "reference" || receipt["providerVersion"] != version || receipt["conditionalWrite"] != false || receipt["fencingEvidence"] != nil || receipt["durableLocation"] == "" {
		t.Fatalf("%s receipt = %#v", operation, result)
	}
	return receipt
}

func TestReferenceFixtureAtomicReplacementKeepsPrivateMode(t *testing.T) {
	t.Parallel()
	path := writeReferenceTestFixture(t, referenceTestFixture())
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	contents, err := readReferenceFixture(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveReferenceFixture(path, contents); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("fixture mode = %v error=%v", info.Mode().Perm(), err)
	}
}
