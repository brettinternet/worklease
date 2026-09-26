package queue

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/sampleadapter"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestExternalWriteProcessHelper(t *testing.T) {
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+3 >= len(os.Args) {
		return
	}
	runExternalWriteProcessHelper(os.Args[separator+1], os.Args[separator+2], os.Args[separator+3])
}

func TestExternalWritePrepareDispatchAndReadback(t *testing.T) {
	t.Parallel()
	writer, source, logPath := newExternalWriteTestAdapter(t, "normal", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cases := []struct {
		action     Action
		transition string
		patch      map[string]string
		appendText string
		method     string
	}{
		{action: ActionStart, transition: "Doing", method: "writeState"},
		{action: ActionRecordProgress, patch: map[string]string{"append": "notes"}, appendText: "progress text", method: "recordProgress"},
		{action: ActionAssignToMe, method: "assign"},
	}
	for index, test := range cases {
		t.Run(test.method, func(t *testing.T) {
			operationID := fmt.Sprintf("%032x", index+1)
			intent := externalWriteTestIntent(source, operationID, test.action, test.transition, test.patch, test.appendText)
			prepared, preview, err := writer.Prepare(ctx, intent)
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if preview.Operation != test.method || preview.ExpectedVersion == nil || *preview.ExpectedVersion != "provider-v1" || preview.ConditionalWrite {
				t.Fatalf("preview = %+v", preview)
			}
			if test.action == ActionRecordProgress && prepared.Marker != "worklease-op:"+operationID {
				t.Fatalf("progress marker = %q", prepared.Marker)
			}
			receipt, err := writer.Write(ctx, prepared)
			if err != nil {
				t.Fatalf("write: %v", err)
			}
			if receipt.SourceID != source.ID || receipt.ItemID != "item-1" || receipt.ID != "receipt://"+operationID || receipt.Version != "provider-v2" || receipt.Actor != "alice" {
				t.Fatalf("provider receipt = %+v", receipt)
			}
			observed, err := writer.ReadReceipt(ctx, prepared, &receipt)
			if err != nil || checkWriteEvidence(prepared, &receipt, observed) != WriteVerified {
				t.Fatalf("readback = %+v error=%v", observed, err)
			}
		})
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var mutationCount int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var request struct {
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			t.Fatalf("invalid request log line: %v", err)
		}
		if request.Method != "writeState" && request.Method != "recordProgress" && request.Method != "assign" {
			continue
		}
		mutationCount++
		for _, forbidden := range []string{"claimId", "claimRevision", "resources", "sessionId", "privateHandle", "authorityId", "claimToken"} {
			if _, leaked := request.Params[forbidden]; leaked {
				t.Fatalf("mutation request leaked %q: %s", forbidden, line)
			}
		}
		var authority struct {
			AuthorizationRef string `json:"authorizationRef"`
			Scope            string `json:"scope"`
		}
		var operationID string
		_ = json.Unmarshal(request.Params["operationId"], &operationID)
		if err := json.Unmarshal(request.Params["authority"], &authority); err != nil || authority.AuthorizationRef != operationID || authority.Scope != source.ID {
			t.Fatalf("mutation authority = %+v operation=%q error=%v", authority, operationID, err)
		}
	}
	if mutationCount != len(cases) {
		t.Fatalf("mutation request count = %d, want %d", mutationCount, len(cases))
	}
}

func TestAdapterConformanceCapabilityDenials(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		mode   string
		claims bool
	}{
		{name: "user permission denied", mode: "denied", claims: true},
		{name: "manifest mutation capability missing", mode: "no-mutation", claims: true},
		{name: "generic claims missing", mode: "normal", claims: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer, source, _ := newExternalWriteTestAdapter(t, test.mode, "")
			if !test.claims {
				writer.source.Claims = nil
			}
			intent := externalWriteTestIntent(source, strings.Repeat("b", 32), ActionStart, "Doing", nil, "")
			if _, _, err := writer.Prepare(context.Background(), intent); err == nil {
				t.Fatal("unsafe external source prepared a write")
			}
		})
	}
}

func TestExternalWriteRefreshesDependencyConditions(t *testing.T) {
	t.Parallel()
	writer, source, logPath := newExternalWriteTestAdapter(t, "blocked-dependency", "")
	intent := externalWriteTestIntent(source, strings.Repeat("d", 32), ActionStart, "Doing", nil, "")
	prepared, preview, err := writer.Prepare(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Readiness.Status != Blocked {
		t.Fatalf("preview did not refresh blocking prerequisite: %+v", preview.Readiness)
	}
	pre, err := writer.Inspect(context.Background(), prepared)
	if err != nil || pre.Ready {
		t.Fatalf("preflight readiness = %+v error=%v", pre, err)
	}
	if _, err := writer.Write(context.Background(), prepared); err == nil {
		t.Fatal("write dispatched despite an unsatisfied prerequisite")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var request struct {
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(line), &request) == nil && (request.Method == "writeState" || request.Method == "recordProgress" || request.Method == "assign") {
			t.Fatalf("blocked write was dispatched: %s", line)
		}
	}
}

func TestAdapterConformanceJournalUnknownWithoutRedispatch(t *testing.T) {
	t.Parallel()
	writer, source, logPath := newExternalWriteTestAdapter(t, "crash-write", "")
	_, paths := testkit.Home(t)
	journal, err := NewWriteJournal(filepath.Join(paths["XDG_STATE_HOME"], "worklease", "queue-recovery"), filepath.Join(paths["HOME"], ".cache", "worklease", "queue"))
	if err != nil {
		t.Fatal(err)
	}
	intent := externalWriteTestIntent(source, strings.Repeat("c", 32), ActionStart, "Doing", nil, "")
	intent.AuthorityID, intent.ClaimID, intent.ClaimRevision = "authority-private", "claim-private", 1
	intent.Resources = []string{"resource:private"}
	intent.CheckpointTTL = time.Minute
	intent.CheckpointNotAfter = time.Now().Add(time.Hour)
	prepared, _, err := writer.Prepare(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	claim := &writeFixture{}
	pipeline := WritePipeline{Adapter: writer, Claim: claim, Journal: journal, Workflow: map[string]string{"start": "Doing"}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := pipeline.Start(ctx, prepared)
	if err == nil || result.Outcome != WriteUnknown {
		t.Fatalf("crashed write result=%+v error=%v", result, err)
	}
	record, err := journal.Read(prepared.OperationID)
	if err != nil || record.Status != "unknown" || record.Receipt != nil {
		t.Fatalf("journal after lost response = %+v error=%v", record, err)
	}
	result, err = pipeline.Recover(ctx, prepared.OperationID)
	if err != nil || result.Outcome != WriteUnknown {
		t.Fatalf("recovery after crash = %+v error=%v", result, err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var dispatches int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var request struct {
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(line), &request) == nil && request.Method == "writeState" {
			dispatches++
		}
	}
	if dispatches != 1 {
		t.Fatalf("journal recovery redispatched provider write %d times", dispatches)
	}
}

func TestExternalWriteTestProcessHarnessIsBounded(t *testing.T) {
	crashed, err := testkit.RunTestProcess("external-crash", time.Second)
	if err == nil || crashed.ExitCode != 23 {
		t.Fatalf("crash helper result=%+v error=%v", crashed, err)
	}
}

func externalWriteTestIntent(source Source, operationID string, action Action, transition string, patch map[string]string, appendText string) WriteIntent {
	return WriteIntent{
		OperationID:   operationID,
		Source:        source,
		Ref:           Ref{SourceID: source.ID, ItemID: "item-1"},
		Principal:     "alice",
		Patch:         patch,
		AuthorityID:   "must-not-be-sent",
		ClaimID:       "claim-must-not-be-sent",
		ClaimRevision: 4,
		Resources:     []string{"resource:must-not-be-sent"},
		Action:        action,
		Transition:    transition,
		Append:        appendText,
	}
}

func newExternalWriteTestAdapter(t *testing.T, mode, logPath string) (*ExternalWriteAdapter, Source, string) {
	t.Helper()
	_, paths := testkit.Home(t)
	env := externalTestEnvironment(paths)
	if logPath == "" {
		logPath = filepath.Join(t.TempDir(), "requests.jsonl")
	}
	source := config.QueueSource{
		ID: "external-write", Adapter: "external",
		Executable:        writeExternalWriteAdapterScript(t, "external-write", mode, logPath),
		ExpectedAdapterID: "example.write", ExpectedVersion: "1.0.0",
		Claims:   &config.QueueClaims{Policy: "generic", Source: "acme/planning"},
		Workflow: map[string]string{"start": "Doing", "blocked": "Blocked", "review": "Review", "complete": "Done", "reopen": "Open"},
		Config:   map[string]any{},
	}
	if err := config.ApproveQueueAdapter(context.Background(), env, source); err != nil {
		t.Fatalf("approve external test adapter: %v", err)
	}
	read := &ExternalAdapter{source: source, env: env}
	resolved, err := read.Resolve(context.Background(), map[string]string{"id": source.ID})
	if err != nil {
		t.Fatalf("resolve external test adapter: %v", err)
	}
	t.Cleanup(read.Close)
	return NewExternalWriteAdapter(read), resolved, logPath
}

func writeExternalWriteAdapterScript(t *testing.T, sourceID, mode, logPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "external-write-adapter")
	command := fmt.Sprintf("#!/bin/sh\nexec %s -test.run='^TestExternalWriteProcessHelper$' -- %s %s %s\n", shellQuote(os.Args[0]), shellQuote(sourceID), shellQuote(mode), shellQuote(logPath))
	if err := os.WriteFile(path, []byte(command), 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func runExternalWriteProcessHelper(expectedSource, mode, logPath string) {
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			os.Exit(0)
		}
		var request struct {
			ID     string                     `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &request) != nil {
			os.Exit(31)
		}
		if logPath != "" {
			if file, openErr := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); openErr == nil {
				_, _ = file.Write(line)
				_ = file.Close()
			}
		}
		var result any
		switch request.Method {
		case "initialize":
			caps := []string{"mutation", "state", "progress", "assignment"}
			if mode == "no-mutation" {
				caps = []string{"state", "progress", "assignment"}
			}
			result = map[string]any{"protocolVersion": 1, "manifest": map[string]any{
				"id": "example.write", "version": "1.0.0", "protocol": map[string]int{"minMajor": 1, "maxMajor": 1},
				"configSchema": map[string]any{"type": "object"}, "authentication": []string{}, "resourcePolicy": "generic",
				"capabilities": caps, "requiredFeatures": []string{},
			}}
		case "resolve":
			id := writeRequestSource(request.Params)
			if id != expectedSource {
				os.Exit(32)
			}
			result = map[string]any{"context": writeWireContext(id, "adapter-user"), "source": map[string]string{"id": id, "name": id, "locator": "memory://" + id}}
		case "capabilities":
			id := writeRequestSource(request.Params)
			var principal string
			_ = json.Unmarshal(request.Params["principal"], &principal)
			permission := "allowed"
			if mode == "denied" {
				permission = "denied"
			}
			caps := map[string]any{}
			for _, name := range []string{"mutation", "state", "progress", "assignment"} {
				caps[name] = map[string]any{"support": "supported", "permission": permission, "availability": "available"}
			}
			result = map[string]any{"context": writeWireContext(id, principal), "capabilities": caps}
		case "readItem":
			ref := writeRequestRef(request.Params)
			result = map[string]any{"context": writeWireContext(ref.SourceID, "adapter-user"), "outcome": map[string]any{"ref": ref, "status": "found", "item": writeWireItem(ref)}}
		case "readDependencies":
			ref := writeRequestRef(request.Params)
			edges := []any{}
			if mode == "blocked-dependency" && ref.ItemID == "item-1" {
				edges = append(edges, writeWireEdge(ref, Ref{SourceID: ref.SourceID, ItemID: "prerequisite"}))
			}
			result = map[string]any{"context": writeWireContext(ref.SourceID, "adapter-user"), "edges": edges, "nextCursor": nil, "completeness": "complete"}
		case "readItems":
			var refs []Ref
			_ = json.Unmarshal(request.Params["refs"], &refs)
			outcomes := make([]any, 0, len(refs))
			for _, ref := range refs {
				outcomes = append(outcomes, map[string]any{"ref": ref, "status": "found", "item": writeWireItem(ref)})
			}
			result = map[string]any{"context": writeWireContext(writeRequestSource(request.Params), "adapter-user"), "outcomes": outcomes}
		case "writeState", "recordProgress", "assign":
			if mode == "crash-write" {
				os.Exit(23)
			}
			ref := writeRequestRef(request.Params)
			var operationID string
			_ = json.Unmarshal(request.Params["operationId"], &operationID)
			result = map[string]any{"context": writeWireContext(ref.SourceID, "alice"), "receipt": map[string]any{
				"sourceId": ref.SourceID, "ref": ref, "operation": request.Method, "providerVersion": "provider-v2",
				"durableLocation": "receipt://" + operationID, "observedState": map[string]any{"actor": "alice"},
				"conditionalWrite": false, "fencingEvidence": nil,
			}}
		case "readReceipt":
			var operation string
			var operationID string
			_ = json.Unmarshal(request.Params["operation"], &operation)
			_ = json.Unmarshal(request.Params["operationId"], &operationID)
			var target Ref
			_ = json.Unmarshal(request.Params["target"], &target)
			var intent struct {
				Payload         map[string]any `json:"payload"`
				ExpectedVersion *string        `json:"expectedVersion"`
			}
			_ = json.Unmarshal(request.Params["intent"], &intent)
			var receipt struct {
				DurableLocation string `json:"durableLocation"`
			}
			_ = json.Unmarshal(request.Params["receipt"], &receipt)
			verification := "verified"
			patch := map[string]string{}
			markerCount, appendProof := 0, false
			appendContent, receiptID := "", receipt.DurableLocation
			if mode == "crash-write" {
				verification = "unknown"
				receiptID = ""
			} else {
				for key, value := range intent.Payload {
					if text, ok := value.(string); ok && (key == "status" || key == "append" || key == "assignee") {
						patch[key] = text
					}
				}
				if operation == "recordProgress" {
					markerCount, appendProof = 1, true
					appendContent, _ = intent.Payload["content"].(string)
				}
			}
			var expectedVersion any
			if intent.ExpectedVersion != nil {
				expectedVersion = *intent.ExpectedVersion
			}
			result = map[string]any{"context": writeWireContext(target.SourceID, "adapter-user"), "verification": verification, "evidence": map[string]any{
				"sourceId": target.SourceID, "itemId": target.ItemID, "precondition": expectedVersion, "patch": patch,
				"markerCount": markerCount, "appendContent": appendContent, "appendProof": appendProof,
				"receiptId": receiptID, "operationId": operationID, "actor": "alice",
			}}
		default:
			os.Exit(33)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			os.Exit(34)
		}
		response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": json.RawMessage(encoded)})
		if err != nil {
			os.Exit(35)
		}
		if _, err = os.Stdout.Write(append(response, '\n')); err != nil {
			os.Exit(36)
		}
	}
}

func writeRequestSource(params map[string]json.RawMessage) string {
	var sourceID string
	_ = json.Unmarshal(params["sourceId"], &sourceID)
	if sourceID == "" {
		sourceID = writeRequestRef(params).SourceID
	}
	return sourceID
}

func writeRequestRef(params map[string]json.RawMessage) Ref {
	var ref Ref
	_ = json.Unmarshal(params["ref"], &ref)
	return ref
}

func writeWireContext(sourceID, principal string) map[string]any {
	return map[string]any{
		"principal": principal, "configurationGeneration": "generation-" + sourceID,
		"observedAt": time.Now().UTC().Format(time.RFC3339Nano), "providerVersion": "provider-v1",
		"coverage": map[string]any{"state": "complete", "scope": sourceID, "cursor": nil},
	}
}

func writeWireEdge(from, to Ref) map[string]any {
	return map[string]any{
		"from": from, "to": to, "relationshipType": "dependency", "direction": "prerequisite",
		"completionCondition": "terminal", "rawOutcome": nil, "interpretation": map[string]any{"result": "unknown"},
		"provenance": "external-write-test", "providerVersion": "provider-v1",
	}
}

func writeWireItem(ref Ref) map[string]any {
	return map[string]any{
		"ref": ref, "title": "Write fixture", "rawStatus": "Open", "state": "open", "order": "1", "priority": 0,
		"canonicalId": ref.SourceID + ":" + ref.ItemID, "providerReady": true, "assignedTo": []string{}, "nativeClaim": "none",
		"updatedAt": time.Now().UTC(), "body": "", "terminal": false, "providerBlocked": false,
	}
}

func TestReferenceAdapterThroughHostWritesConfiguredActions(t *testing.T) {
	t.Parallel()
	writer, source, storePath := newReferenceExternalWriteAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cases := []struct {
		action     Action
		transition string
		patch      map[string]string
		appendText string
	}{
		{action: ActionStart, transition: "Doing", patch: map[string]string{"status": "Doing"}},
		{action: ActionRecordProgress, patch: map[string]string{"append": "comment"}, appendText: "reference progress"},
		{action: ActionAssignToMe},
	}
	for index, test := range cases {
		operationID := fmt.Sprintf("%032x", index+1)
		intent := WriteIntent{
			OperationID: operationID, Source: source, Ref: Ref{SourceID: source.ID, ItemID: "reference-1"},
			Principal: "alice", Action: test.action, Transition: test.transition,
			Patch: test.patch, Append: test.appendText,
		}
		prepared, preview, err := writer.Prepare(ctx, intent)
		if err != nil || preview.ConditionalWrite || preview.ExpectedVersion == nil {
			t.Fatalf("prepare %s: preview=%+v error=%v", externalWriteMethod(test.action), preview, err)
		}
		receipt, err := writer.Write(ctx, prepared)
		if err != nil || receipt.SourceID != source.ID || receipt.ItemID != intent.Ref.ItemID || receipt.Actor != "alice" || receipt.Version == "" || receipt.ID == "" {
			t.Fatalf("write %s: receipt=%+v error=%v", externalWriteMethod(test.action), receipt, err)
		}
		observation, err := writer.ReadReceipt(ctx, prepared, &receipt)
		if err != nil || checkWriteEvidence(prepared, &receipt, observation) != WriteVerified {
			t.Fatalf("readback %s: observation=%+v error=%v", externalWriteMethod(test.action), observation, err)
		}
	}
	var store struct {
		Version int64 `json:"version"`
		Items   []struct {
			AssignedTo []string `json:"assignedTo"`
			Comments   []any    `json:"comments"`
		} `json:"items"`
		Writes []json.RawMessage `json:"writes"`
	}
	data, err := os.ReadFile(storePath)
	if err != nil || json.Unmarshal(data, &store) != nil || store.Version != 4 || len(store.Writes) != 3 || len(store.Items) != 1 || len(store.Items[0].Comments) != 1 || len(store.Items[0].AssignedTo) != 1 || store.Items[0].AssignedTo[0] != "alice" {
		t.Fatalf("fixture effects: %+v error=%v", store, err)
	}
}

func TestReferenceAdapterLostResponseRecoversWithClaimAndNoRedispatch(t *testing.T) {
	t.Parallel()
	writer, source, storePath := newReferenceExternalWriteAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	intent := WriteIntent{
		OperationID: strings.Repeat("e", 32), Source: source, Ref: Ref{SourceID: source.ID, ItemID: "reference-1"},
		Principal: "alice", Action: ActionRecordProgress, Patch: map[string]string{"append": "comment"}, Append: "lost response progress",
		AuthorityID: "authority-ref", ClaimID: "claim-ref", ClaimRevision: 2, Resources: []string{"generic:fixture/reference/reference-1"}, CheckpointTTL: time.Minute,
	}
	prepared, _, err := writer.Prepare(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	_, paths := testkit.Home(t)
	journal, err := NewWriteJournal(filepath.Join(paths["XDG_STATE_HOME"], "worklease", "queue-recovery"), filepath.Join(paths["HOME"], ".cache", "worklease", "queue"))
	if err != nil {
		t.Fatal(err)
	}
	claim := &referencePipelineClaim{}
	pipeline := WritePipeline{Adapter: lostReferenceExternalWriteResponse{writer}, Claim: claim, Journal: journal}
	result, err := pipeline.Start(ctx, prepared)
	if err == nil || result.Outcome != WriteUnknown || !result.ClaimHeld {
		t.Fatalf("lost response result=%+v error=%v", result, err)
	}
	record, err := journal.Read(prepared.OperationID)
	if err != nil || record.Status != "unknown" || record.Receipt != nil {
		t.Fatalf("unknown journal record=%+v error=%v", record, err)
	}
	result, err = pipeline.Recover(ctx, prepared.OperationID)
	if err != nil || result.Outcome != WriteVerified || !result.ClaimHeld || claim.verifyCalls != 4 || claim.checkpointCalls != 1 {
		t.Fatalf("recovery result=%+v error=%v claim=%+v", result, err, claim)
	}
	data, err := os.ReadFile(storePath)
	var store struct {
		Writes []json.RawMessage `json:"writes"`
		Items  []struct {
			Comments []json.RawMessage `json:"comments"`
		} `json:"items"`
	}
	if err != nil || json.Unmarshal(data, &store) != nil || len(store.Writes) != 1 || len(store.Items) != 1 || len(store.Items[0].Comments) != 1 {
		t.Fatalf("recovery redispatched the provider write: store=%+v error=%v", store, err)
	}
}

type lostReferenceExternalWriteResponse struct{ *ExternalWriteAdapter }

func (a lostReferenceExternalWriteResponse) Write(ctx context.Context, intent WriteIntent) (ProviderReceipt, error) {
	if _, err := a.ExternalWriteAdapter.Write(ctx, intent); err != nil {
		return ProviderReceipt{}, err
	}
	return ProviderReceipt{}, fmt.Errorf("response lost after the fixture write was applied")
}

type referencePipelineClaim struct {
	verifyCalls     int
	checkpointCalls int
	checkpointed    bool
}

func (c *referencePipelineClaim) Verify(context.Context, WriteIntent) error {
	c.verifyCalls++
	return nil
}

func (c *referencePipelineClaim) Checkpoint(context.Context, WriteIntent, ProviderReceipt) error {
	c.checkpointCalls++
	c.checkpointed = true
	return nil
}

func (c *referencePipelineClaim) CheckpointStatus(context.Context, WriteIntent, ProviderReceipt) (WriteVerification, error) {
	if c.checkpointed {
		return WriteVerified, nil
	}
	return WriteUnknown, nil
}

func TestReferenceAdapterHostCredentialedWrite(t *testing.T) {
	writer, source, storePath := newReferenceExternalWriteAdapter(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	intent := WriteIntent{OperationID: "00000000000000000000000000000001", Source: source, Ref: Ref{SourceID: source.ID, ItemID: "reference-1"}, Principal: "alice", Action: ActionStart, Transition: "Doing", Patch: map[string]string{"status": "Doing"}}
	prepared, _, err := writer.Prepare(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := writer.Write(ctx, prepared)
	if err != nil {
		t.Fatalf("credentialed write: %v", err)
	}
	observation, err := writer.ReadReceipt(ctx, prepared, &receipt)
	if err != nil || checkWriteEvidence(prepared, &receipt, observation) != WriteVerified {
		t.Fatalf("credentialed receipt: %+v %v", observation, err)
	}
	data, err := os.ReadFile(storePath)
	if err != nil || !strings.Contains(string(data), `"rawStatus": "Doing"`) {
		t.Fatalf("fixture was not updated: %v %s", err, data)
	}
}

func newReferenceExternalWriteAdapter(t *testing.T, credentialed ...bool) (*ExternalWriteAdapter, Source, string) {
	t.Helper()
	storePath := filepath.Join(t.TempDir(), "reference-store.json")
	store := map[string]any{
		"principal": "alice", "version": 1,
		"transitions": map[string]string{"start": "Doing", "blocked": "Blocked", "review": "Review", "complete": "Done", "reopen": "Open"},
		"items": []any{map[string]any{
			"id": "reference-1", "title": "Disposable reference item", "rawStatus": "Open", "state": "open", "order": "1", "priority": 1,
			"providerReady": true, "assignedTo": []string{}, "nativeClaim": "", "updatedAt": "2025-01-02T03:04:05Z", "body": "fixture",
		}},
		"inaccessibleItemIds": []string{}, "dependencies": []any{}, "writes": []any{},
	}
	data, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, paths := testkit.Home(t)
	env := externalTestEnvironment(paths)
	if len(credentialed) > 0 && credentialed[0] {
		for name, value := range paths {
			t.Setenv(name, value)
		}
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "reference-adapter")
	script := fmt.Sprintf("#!/bin/sh\nexec %s -test.run='^TestReferenceAdapterProcessHelper$' -- reference\n", shellQuote(testBinary))
	if len(credentialed) > 0 && credentialed[0] {
		script = fmt.Sprintf("#!/bin/sh\nexec %s -test.run='^TestReferenceAdapterProcessHelper$' -- reference %s\n", shellQuote(testBinary), shellQuote(paths["XDG_CONFIG_HOME"]))
	}
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	configured := config.QueueSource{
		ID: "reference-host", Adapter: "external", Executable: canonical,
		ExpectedAdapterID: "worklease.reference.local-fixture", ExpectedVersion: "1.0.0", Account: "alice",
		Claims:   &config.QueueClaims{Policy: "generic", Source: "fixture/reference"},
		Workflow: map[string]string{"start": "Doing", "blocked": "Blocked", "review": "Review", "complete": "Done", "reopen": "Open"},
		Config:   map[string]any{"fixturePath": storePath},
	}
	if len(credentialed) > 0 && credentialed[0] {
		const token = "disposable-fixture-credential"
		const origin = "https://reference.invalid"
		directory := filepath.Join(paths["XDG_CONFIG_HOME"], "worklease")
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(token))
		data, _ := json.Marshal(map[string]string{"sourceId": configured.ID, "origin": origin, "principal": "alice", "sha256": fmt.Sprintf("%x", digest)})
		if err := os.WriteFile(filepath.Join(directory, "reference-credential.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		helper := filepath.Join(t.TempDir(), "credential-helper")
		if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf '%s\\n' '"+token+"'\n"), 0700); err != nil {
			t.Fatal(err)
		}
		configured.CredentialHelper = []string{helper}
		configured.Config["origin"] = origin
	}
	if err := config.ApproveQueueAdapter(context.Background(), env, configured); err != nil {
		t.Fatalf("approve reference adapter: %v", err)
	}
	read := &ExternalAdapter{source: configured, env: env}
	t.Cleanup(read.Close)
	source, err := read.Resolve(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolve reference adapter: %v", err)
	}
	return NewExternalWriteAdapter(read), source, storePath
}

func TestReferenceAdapterProcessHelper(t *testing.T) {
	for index, argument := range os.Args {
		if argument != "--" || index+1 >= len(os.Args) || os.Args[index+1] != "reference" {
			continue
		}
		if index+2 < len(os.Args) {
			t.Setenv("XDG_CONFIG_HOME", os.Args[index+2])
		}
		if err := sampleadapter.RunReference(os.Stdin, os.Stdout); err != nil {
			t.Fatalf("serve reference adapter: %v", err)
		}
		return
	}
}
