package sampleadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxReferenceFixtureBytes = 1 << 20

type fixtureComment struct {
	OperationID string `json:"operationId"`
	Body        string `json:"body"`
	Author      string `json:"author"`
	ReceiptID   string `json:"receiptId"`
}

type fixtureWrite struct {
	OperationID     string            `json:"operationId"`
	SourceID        string            `json:"sourceId"`
	ItemID          string            `json:"itemId"`
	Operation       string            `json:"operation"`
	ExpectedVersion string            `json:"expectedVersion"`
	Patch           map[string]string `json:"patch"`
	ProviderVersion string            `json:"providerVersion"`
	DurableLocation string            `json:"durableLocation"`
	Actor           string            `json:"actor"`
}

// RunReference serves the network-free, mutable fixture adapter over the same
// bounded JSON-RPC framing as the read-only sample. resolve requires config
// containing the absolute path to a caller-owned fixture JSON file.
func RunReference(input io.Reader, output io.Writer) error {
	adapter, err := newServer(output)
	if err != nil {
		return err
	}
	adapter.writable = true
	return adapter.serve(input)
}

func (s *server) context(sourceID, state string, cursor *string) map[string]any {
	if !s.writable {
		return wireContext(sourceID, state, cursor)
	}
	principal := s.fixture.Principal
	version := referenceVersion(s.fixture.Version)
	return map[string]any{
		"principal": principal, "configurationGeneration": "reference-fixture-v1",
		"observedAt": time.Now().UTC().Format(time.RFC3339Nano), "providerVersion": version,
		"coverage": map[string]any{"state": state, "scope": sourceID, "cursor": cursor},
	}
}

func referenceVersion(version int64) string { return fmt.Sprintf("fixture-v%d", version) }

func referenceCapabilities() map[string]any {
	result := make(map[string]any)
	for _, name := range []string{"identity", "discovery", "dependencies", "state", "progress", "assignment", "mutation"} {
		capability := map[string]any{"support": "supported", "permission": "allowed", "availability": "available"}
		if name == "identity" || name == "discovery" || name == "dependencies" || name == "state" || name == "progress" || name == "assignment" || name == "mutation" {
			result[name] = capability
		}
	}
	return result
}

func readReferenceFixture(path string) (fixture, error) {
	var contents fixture
	file, err := os.Open(path)
	if err != nil {
		return contents, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxReferenceFixtureBytes+1))
	if err != nil || len(data) > maxReferenceFixtureBytes || json.Unmarshal(data, &contents) != nil || validateReferenceFixture(contents) != nil {
		return fixture{}, errors.New("invalid reference fixture")
	}
	return contents, nil
}

func validateReferenceFixture(contents fixture) error {
	if contents.Principal == "" || !validID(contents.Principal) || contents.Version < 1 || len(contents.Items) == 0 || len(contents.Items) > maxItems || len(contents.Transitions) == 0 {
		return errors.New("invalid reference fixture")
	}
	seen := make(map[string]bool, len(contents.Items))
	for _, item := range contents.Items {
		if item.ID == "" || !validText(item.ID) || seen[item.ID] || !validText(item.Title) || !validText(item.RawStatus) {
			return errors.New("invalid reference fixture item")
		}
		seen[item.ID] = true
	}
	labels := make(map[string]bool, len(contents.Transitions))
	for key, transition := range contents.Transitions {
		if !validID(key) || transition == "" || !validText(transition) || labels[transition] {
			return errors.New("invalid reference fixture transition")
		}
		labels[transition] = true
	}
	for _, write := range contents.Writes {
		if write.OperationID == "" || write.SourceID == "" || write.ItemID == "" || write.Operation == "" || write.DurableLocation == "" {
			return errors.New("invalid reference fixture write journal")
		}
	}
	return nil
}

func saveReferenceFixture(path string, contents fixture) error {
	data, err := json.MarshalIndent(contents, "", "  ")
	if err != nil || len(data) > maxReferenceFixtureBytes {
		return errors.New("reference fixture exceeds its size limit")
	}
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".worklease-reference-fixture-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	root, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Sync()
}

func (s *server) dispatchReferenceWrite(ctx context.Context, method string, params map[string]json.RawMessage, limits requestBudget) (any, *rpcError) {
	if ctx.Err() != nil {
		return nil, unavailable()
	}
	if method == "readReceipt" {
		s.fixtureMu.Lock()
		latest, err := readReferenceFixture(s.fixturePath)
		if err != nil {
			s.fixtureMu.Unlock()
			return nil, unavailable()
		}
		s.fixture = latest
		result, failure := s.readReferenceReceipt(params)
		s.fixtureMu.Unlock()
		if failure != nil {
			return nil, failure
		}
		if !resultFits(result, limits.MaxBytes) {
			return nil, unsupported()
		}
		return result, nil
	}
	s.fixtureMu.Lock()
	defer s.fixtureMu.Unlock()
	result, failure := s.applyReferenceWrite(method, params)
	if failure != nil {
		return nil, failure
	}
	if !resultFits(result, limits.MaxBytes) {
		// The write may already be durable; never report a capability denial.
		return nil, rpcErr(-32009, "unknown-outcome", "Fixture write response exceeds the requested budget")
	}
	return result, nil
}

func (s *server) applyReferenceWrite(method string, params map[string]json.RawMessage) (any, *rpcError) {
	latest, loadErr := readReferenceFixture(s.fixturePath)
	if loadErr != nil {
		return nil, unavailable()
	}
	s.fixture = latest
	ref, sourceID, err := s.requireRef(params)
	if err != nil {
		return nil, err
	}
	var request struct {
		OperationID     string            `json:"operationId"`
		Patch           map[string]string `json:"patch"`
		ExpectedVersion *string           `json:"expectedVersion"`
		Authority       struct {
			AuthorizationRef string `json:"authorizationRef"`
			Scope            string `json:"scope"`
		} `json:"authority"`
	}
	if json.Unmarshal(mustMarshal(params), &request) != nil || request.OperationID == "" || !validText(request.OperationID) || request.Patch == nil || request.Authority.AuthorizationRef == "" || !validText(request.Authority.AuthorizationRef) || request.Authority.Scope != sourceID {
		return nil, invalidParams()
	}
	item := s.findItem(ref.ItemID)
	if item == nil {
		return nil, invalidParams()
	}
	patch := request.Patch
	if !validReferencePatch(patch) {
		return nil, invalidParams()
	}
	if method == "recordProgress" && (len(patch) != 3 || patch["append"] != "comment" || strings.TrimSpace(patch["content"]) == "" || patch["marker"] != "worklease-op:"+request.OperationID || strings.Contains(patch["content"], "worklease-op:")) {
		return nil, invalidParams()
	}
	if method == "assign" && (len(patch) != 1 || patch["assignee"] != s.fixture.Principal) {
		return nil, invalidParams()
	}
	if method == "writeState" {
		if len(patch) != 1 || patch["status"] == "" || !configuredTransition(s.fixture.Transitions, patch["status"]) {
			return nil, invalidParams()
		}
	}
	precondition := ""
	if request.ExpectedVersion != nil {
		precondition = *request.ExpectedVersion
		if !validText(precondition) {
			return nil, invalidParams()
		}
	}
	for _, previous := range s.fixture.Writes {
		if previous.OperationID != request.OperationID {
			continue
		}
		if previous.SourceID != sourceID || previous.ItemID != ref.ItemID || previous.Operation != method || previous.ExpectedVersion != precondition || !sameStringMap(previous.Patch, patch) {
			return nil, rpcErr(-32005, "conflict", "Operation ID was already used for a different fixture write")
		}
		return s.mutationResult(sourceID, ref, method, previous), nil
	}
	if request.ExpectedVersion != nil && *request.ExpectedVersion != referenceVersion(s.fixture.Version) {
		return nil, rpcErr(-32005, "conflict", "Fixture version changed before the requested write")
	}
	contents, cloneErr := cloneReferenceFixture(s.fixture)
	if cloneErr != nil {
		return nil, rpcErr(-32603, "internal-error", "Fixture write could not be prepared")
	}
	updated := findFixtureItem(&contents, ref.ItemID)
	if updated == nil || contents.Version == int64(^uint64(0)>>1) {
		return nil, rpcErr(-32603, "internal-error", "Fixture write could not be prepared")
	}
	contents.Version++
	providerVersion := referenceVersion(contents.Version)
	durableLocation := referenceReceiptLocation(sourceID, ref.ItemID, request.OperationID)
	switch method {
	case "writeState":
		updated.RawStatus = patch["status"]
		updated.State = stateForTransition(contents.Transitions, patch["status"])
		updated.ProviderReady = updated.State != "blocked" && updated.State != "complete"
	case "recordProgress":
		updated.Comments = append(updated.Comments, fixtureComment{OperationID: request.OperationID, Body: patch["content"] + "\n\n" + patch["marker"], Author: contents.Principal, ReceiptID: durableLocation})
	case "assign":
		if !containsString(updated.AssignedTo, contents.Principal) {
			updated.AssignedTo = append(updated.AssignedTo, contents.Principal)
		}
	default:
		return nil, rpcErr(-32601, "method-not-found", "Method not found")
	}
	updated.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	write := fixtureWrite{
		OperationID: request.OperationID, SourceID: sourceID, ItemID: ref.ItemID, Operation: method,
		ExpectedVersion: precondition, Patch: cloneStringMap(patch), ProviderVersion: providerVersion,
		DurableLocation: durableLocation, Actor: contents.Principal,
	}
	contents.Writes = append(contents.Writes, write)
	if err := saveReferenceFixture(s.fixturePath, contents); err != nil {
		if persisted, reloadErr := readReferenceFixture(s.fixturePath); reloadErr == nil {
			s.fixture = persisted
		}
		return nil, rpcErr(-32009, "unknown-outcome", "Fixture persistence outcome is unknown")
	}
	s.fixture = contents
	return s.mutationResult(sourceID, ref, method, write), nil
}

func (s *server) mutationResult(sourceID string, ref workRef, method string, write fixtureWrite) any {
	version := write.ProviderVersion
	return map[string]any{
		"context": s.context(sourceID, "complete", nil),
		"receipt": map[string]any{
			"sourceId": sourceID, "ref": ref, "operation": method,
			"providerVersion": &version, "durableLocation": write.DurableLocation,
			"observedState":    map[string]string{"actor": write.Actor},
			"conditionalWrite": false, "fencingEvidence": nil,
		},
	}
}

func (s *server) readReferenceReceipt(params map[string]json.RawMessage) (any, *rpcError) {
	sourceID, ok := stringField(params, "sourceId")
	if !ok || !validID(sourceID) {
		return nil, invalidParams()
	}
	if _, err := s.requireSource(map[string]json.RawMessage{"sourceId": mustMarshal(sourceID)}); err != nil {
		return nil, err
	}
	operation, ok := stringField(params, "operation")
	if !ok || operation != "writeState" && operation != "recordProgress" && operation != "assign" {
		return nil, invalidParams()
	}
	operationID, ok := stringField(params, "operationId")
	if !ok || operationID == "" || !validText(operationID) {
		return nil, invalidParams()
	}
	var target workRef
	targetRaw := params["target"]
	if json.Unmarshal(targetRaw, &target) != nil || target.SourceID == "" {
		var wrapped struct {
			Ref workRef `json:"ref"`
		}
		if json.Unmarshal(targetRaw, &wrapped) != nil {
			return nil, invalidParams()
		}
		target = wrapped.Ref
	}
	if target.SourceID != sourceID || target.ItemID == "" || !validText(target.ItemID) {
		return nil, invalidParams()
	}
	var intent struct {
		Payload         map[string]json.RawMessage `json:"payload"`
		ExpectedVersion *string                    `json:"expectedVersion"`
	}
	if json.Unmarshal(params["intent"], &intent) != nil || intent.Payload == nil {
		return nil, invalidParams()
	}
	patch := make(map[string]string, len(intent.Payload))
	for key, raw := range intent.Payload {
		var value string
		if json.Unmarshal(raw, &value) != nil || !validText(key) || !validText(value) {
			return nil, invalidParams()
		}
		patch[key] = value
	}
	precondition := ""
	if intent.ExpectedVersion != nil {
		precondition = *intent.ExpectedVersion
		if !validText(precondition) {
			return nil, invalidParams()
		}
	}
	item := s.findItem(target.ItemID)
	verification := "unknown"
	markerCount, appendProof := 0, false
	appendContent, receiptID, actor := "", "", ""
	evidencePatch := map[string]string{}
	if item != nil && operation == "recordProgress" && len(patch) == 3 && patch["append"] == "comment" && patch["marker"] == "worklease-op:"+operationID && strings.TrimSpace(patch["content"]) != "" {
		marker := "worklease-op:" + operationID
		for _, comment := range item.Comments {
			markerCount += strings.Count(comment.Body, marker)
			if strings.HasSuffix(comment.Body, "\n\n"+marker) {
				content := strings.TrimSuffix(comment.Body, "\n\n"+marker)
				if comment.OperationID == operationID && content == patch["content"] && comment.Author == s.fixture.Principal {
					for _, write := range s.fixture.Writes {
						if write.OperationID == operationID && write.SourceID == sourceID && write.ItemID == target.ItemID && write.Operation == operation && write.ExpectedVersion == precondition && sameStringMap(write.Patch, patch) && write.DurableLocation == comment.ReceiptID {
							appendProof, appendContent, receiptID, actor = true, content, comment.ReceiptID, comment.Author
							evidencePatch = map[string]string{"append": "comment"}
						}
					}
				}
			}
		}
		if markerCount == 1 && appendProof {
			verification = "verified"
		} else {
			appendProof = false
		}
	} else if item != nil && operation != "recordProgress" && len(params["receipt"]) > 0 && string(params["receipt"]) != "null" {
		for _, write := range s.fixture.Writes {
			if write.OperationID != operationID || write.SourceID != sourceID || write.ItemID != target.ItemID || write.Operation != operation || write.ExpectedVersion != precondition || !sameStringMap(write.Patch, patch) {
				continue
			}
			if operation == "writeState" && item.RawStatus == patch["status"] || operation == "assign" && containsString(item.AssignedTo, patch["assignee"]) {
				verification, evidencePatch, receiptID, actor = "verified", cloneStringMap(write.Patch), write.DurableLocation, write.Actor
			} else {
				verification, evidencePatch, receiptID, actor = "conflict", cloneStringMap(write.Patch), write.DurableLocation, write.Actor
			}
			break
		}
	}
	if rawReceipt := params["receipt"]; len(rawReceipt) > 0 && string(rawReceipt) != "null" {
		var receipt struct {
			SourceID        string   `json:"sourceId"`
			Ref             *workRef `json:"ref"`
			Operation       string   `json:"operation"`
			DurableLocation string   `json:"durableLocation"`
		}
		if json.Unmarshal(rawReceipt, &receipt) != nil || receipt.SourceID != sourceID || receipt.Ref == nil || *receipt.Ref != target || receipt.Operation != operation || receipt.DurableLocation == "" || receipt.DurableLocation != receiptID {
			verification = "unknown"
			appendProof = false
		}
	}
	var expectedVersion any
	if intent.ExpectedVersion != nil {
		expectedVersion = precondition
	}
	evidence := map[string]any{
		"sourceId": sourceID, "itemId": target.ItemID, "precondition": expectedVersion,
		"patch": evidencePatch, "markerCount": markerCount, "appendContent": appendContent,
		"appendProof": appendProof, "receiptId": receiptID, "durableLocation": receiptID,
		"operationId": operationID, "actor": actor, "effects": map[string]bool{},
	}
	return map[string]any{"context": s.context(sourceID, "complete", nil), "verification": verification, "evidence": evidence}, nil
}

func validReferencePatch(patch map[string]string) bool {
	if len(patch) == 0 {
		return false
	}
	for key, value := range patch {
		if key == "" || !validText(key) || !validText(value) {
			return false
		}
	}
	return true
}

func configuredTransition(transitions map[string]string, value string) bool {
	for _, configured := range transitions {
		if configured == value {
			return true
		}
	}
	return false
}

func stateForTransition(transitions map[string]string, status string) string {
	for action, label := range transitions {
		if label != status {
			continue
		}
		switch action {
		case "complete":
			return "complete"
		case "blocked":
			return "blocked"
		case "reopen":
			return "open"
		default:
			return "in-progress"
		}
	}
	return "open"
}

func referenceReceiptLocation(sourceID, itemID, operationID string) string {
	location := url.URL{Scheme: "fixture", Host: sourceID, Path: "/" + itemID + "/" + operationID}
	return location.String()
}

func findFixtureItem(contents *fixture, id string) *fixtureItem {
	for index := range contents.Items {
		if contents.Items[index].ID == id {
			return &contents.Items[index]
		}
	}
	return nil
}

func cloneReferenceFixture(contents fixture) (fixture, error) {
	data, err := json.Marshal(contents)
	if err != nil {
		return fixture{}, err
	}
	var cloned fixture
	if err := json.Unmarshal(data, &cloned); err != nil {
		return fixture{}, err
	}
	return cloned, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func sameStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
