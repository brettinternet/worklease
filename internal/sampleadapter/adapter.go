// Package sampleadapter implements the static read-only JSON-RPC v1 sample adapter.
package sampleadapter

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxFrameBytes = 1 << 20
	maxItems      = 100
	maxBytes      = 786432
	maxInFlight   = 16
	maxSeenIDs    = 100000
)

//go:embed fixture.json
var fixtureFS embed.FS

type fixture struct {
	Items               []fixtureItem `json:"items"`
	InaccessibleItemIDs []string      `json:"inaccessibleItemIds"`
	Dependencies        []fixtureEdge `json:"dependencies"`
}

type fixtureItem struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	RawStatus     string   `json:"rawStatus"`
	State         string   `json:"state"`
	Order         string   `json:"order"`
	Priority      int      `json:"priority"`
	ProviderReady bool     `json:"providerReady"`
	AssignedTo    []string `json:"assignedTo"`
	NativeClaim   string   `json:"nativeClaim"`
	UpdatedAt     string   `json:"updatedAt"`
	Body          string   `json:"body"`
}

type fixtureEdge struct {
	FromID              string          `json:"fromId"`
	ToID                string          `json:"toId"`
	RelationshipType    string          `json:"relationshipType"`
	Direction           string          `json:"direction"`
	CompletionCondition *string         `json:"completionCondition"`
	RawOutcome          json.RawMessage `json:"rawOutcome"`
	Interpretation      json.RawMessage `json:"interpretation"`
	Provenance          string          `json:"provenance"`
	ProviderVersion     string          `json:"providerVersion"`
}

type rpcFailure struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    rpcFailureData `json:"data"`
}

type rpcFailureData struct {
	Diagnostic string `json:"diagnostic"`
}

type rpcError struct {
	failure rpcFailure
}

func rpcErr(code int, diagnostic, message string) *rpcError {
	return &rpcError{failure: rpcFailure{Code: code, Message: message, Data: rpcFailureData{Diagnostic: diagnostic}}}
}

func invalidParams() *rpcError {
	return rpcErr(-32602, "invalid-params", "Invalid params")
}

func unsupported() *rpcError {
	return rpcErr(-32001, "unsupported-capability", "The sample adapter does not support this operation")
}

func unavailable() *rpcError {
	return rpcErr(-32007, "unavailable-source", "The request deadline has expired")
}

type server struct {
	mu          sync.Mutex
	writeMu     sync.Mutex
	initialized bool
	resolvedID  string
	seen        map[string]struct{}
	pending     map[string]context.CancelFunc
	fixture     fixture
	output      io.Writer
	workers     sync.WaitGroup
}

func newServer(output io.Writer) (*server, error) {
	data, err := fixtureFS.ReadFile("fixture.json")
	if err != nil {
		return nil, err
	}
	var contents fixture
	if err := json.Unmarshal(data, &contents); err != nil || len(contents.Items) == 0 {
		return nil, errors.New("invalid sample fixture")
	}
	return &server{
		seen: make(map[string]struct{}), pending: make(map[string]context.CancelFunc),
		fixture: contents, output: output,
	}, nil
}

// Run serves the sample adapter protocol on the supplied streams.
func Run(input io.Reader, output io.Writer) error {
	adapter, err := newServer(output)
	if err != nil {
		return err
	}
	return adapter.serve(input)
}

func (s *server) serve(input io.Reader) error {
	reader := bufio.NewReaderSize(input, 16*1024)
	for {
		line, err := readFrame(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.workers.Wait()
				return nil
			}
			if !errors.Is(err, errIncompleteFrame) {
				_ = s.writeError(nil, rpcErr(-32700, "parse-error", "Invalid JSON-RPC frame"))
			}
			s.cancelPending()
			s.workers.Wait()
			return err
		}
		s.acceptLine(line)
	}
}

var errIncompleteFrame = errors.New("incomplete frame")
var errOversizedFrame = errors.New("oversized frame")

func readFrame(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 4096)
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > maxFrameBytes {
			return nil, errOversizedFrame
		}
		line = append(line, part...)
		if err == nil {
			return line, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			if len(line) == 0 {
				return nil, io.EOF
			}
			return nil, errIncompleteFrame
		}
		return nil, err
	}
}

func (s *server) acceptLine(frame []byte) {
	if len(frame) == 0 || frame[len(frame)-1] != '\n' || bytes.Contains(frame, []byte{'\r'}) {
		_ = s.writeError(nil, rpcErr(-32600, "invalid-request", "Invalid Request"))
		return
	}
	line := frame[:len(frame)-1]
	if err := validateJSON(line); err != nil {
		_ = s.writeError(nil, rpcErr(-32700, "parse-error", "Invalid JSON-RPC frame"))
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(line, &fields) != nil || fields == nil {
		_ = s.writeError(nil, rpcErr(-32600, "invalid-request", "Invalid Request"))
		return
	}
	for key := range fields {
		if key != "jsonrpc" && key != "id" && key != "method" && key != "params" {
			_ = s.writeError(nil, rpcErr(-32600, "invalid-request", "Invalid Request"))
			return
		}
	}
	var version, method string
	if json.Unmarshal(fields["jsonrpc"], &version) != nil || version != "2.0" || json.Unmarshal(fields["method"], &method) != nil || method == "" || !utf8.ValidString(method) {
		_ = s.writeError(nil, rpcErr(-32600, "invalid-request", "Invalid Request"))
		return
	}
	params, ok := fields["params"]
	var paramObject map[string]json.RawMessage
	if !ok || json.Unmarshal(params, &paramObject) != nil || paramObject == nil {
		_ = s.writeError(nil, rpcErr(-32600, "invalid-request", "Invalid Request"))
		return
	}
	if method == "$/cancelRequest" && fields["id"] == nil {
		var cancelParams struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(params, &cancelParams) == nil && validID(cancelParams.ID) {
			s.cancel(cancelParams.ID)
		}
		return
	}
	var id string
	if json.Unmarshal(fields["id"], &id) != nil || !validID(id) {
		_ = s.writeError(nil, rpcErr(-32600, "invalid-request", "Invalid Request"))
		return
	}
	if method == "$/cancelRequest" {
		_ = s.writeError(&id, rpcErr(-32600, "invalid-request", "Invalid Request"))
		return
	}

	s.mu.Lock()
	if _, exists := s.seen[id]; exists || len(s.seen) >= maxSeenIDs || len(s.pending) >= maxInFlight {
		s.mu.Unlock()
		_ = s.writeError(nil, rpcErr(-32600, "invalid-request", "Request ID is duplicate or adapter capacity is exhausted"))
		return
	}
	s.seen[id] = struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	s.pending[id] = cancel
	s.workers.Add(1)
	s.mu.Unlock()
	go s.run(ctx, cancel, id, method, params)
}

func validID(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value)
}

func (s *server) run(ctx context.Context, cancel context.CancelFunc, id, method string, params json.RawMessage) {
	defer s.workers.Done()
	defer cancel()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()
	result, budget, callErr := s.dispatch(ctx, method, params)
	if ctx.Err() != nil {
		return
	}
	if callErr != nil {
		_ = s.writeError(&id, callErr)
		return
	}
	if budget > 0 && !resultFits(result, budget) {
		_ = s.writeError(&id, unsupported())
		return
	}
	_ = s.writeResult(id, result)
}

func (s *server) dispatch(parent context.Context, method string, raw json.RawMessage) (any, int, *rpcError) {
	var params map[string]json.RawMessage
	if json.Unmarshal(raw, &params) != nil || params == nil {
		return nil, 0, invalidParams()
	}
	if method == "initialize" {
		return s.initialize(params)
	}
	if method == "changes" || method == "readReceipt" || method == "resolveReviewBoundary" || method == "archive" || method == "writeState" || method == "recordProgress" || method == "assign" {
		if _, _, err := common(params); err != nil {
			return nil, 0, err
		}
		return nil, 0, unsupported()
	}
	if method != "resolve" && method != "capabilities" && method != "list" && method != "readItems" && method != "readDependencies" && method != "readItem" && method != "resourcePolicy" {
		return nil, 0, rpcErr(-32601, "method-not-found", "Method not found")
	}
	deadline, limits, err := common(params)
	if err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	defer cancel()
	if ctx.Err() != nil {
		return nil, 0, unavailable()
	}
	if !s.isInitialized() {
		return nil, limits.MaxBytes, invalidParams()
	}
	result, callErr := s.dispatchRead(ctx, method, params, limits)
	if ctx.Err() != nil {
		return nil, limits.MaxBytes, unavailable()
	}
	return result, limits.MaxBytes, callErr
}

type requestBudget struct {
	MaxItems int `json:"maxItems"`
	MaxBytes int `json:"maxBytes"`
}

func common(params map[string]json.RawMessage) (time.Time, requestBudget, *rpcError) {
	var deadlineValue string
	if json.Unmarshal(params["deadline"], &deadlineValue) != nil || !strings.HasSuffix(deadlineValue, "Z") {
		return time.Time{}, requestBudget{}, invalidParams()
	}
	deadline, err := time.Parse(time.RFC3339Nano, deadlineValue)
	if err != nil {
		return time.Time{}, requestBudget{}, invalidParams()
	}
	if !deadline.After(time.Now()) {
		return time.Time{}, requestBudget{}, unavailable()
	}
	var limits requestBudget
	if json.Unmarshal(params["budget"], &limits) != nil || limits.MaxItems < 1 || limits.MaxItems > maxItems || limits.MaxBytes < 1 || limits.MaxBytes > maxBytes {
		return time.Time{}, requestBudget{}, invalidParams()
	}
	return deadline, limits, nil
}

func (s *server) initialize(params map[string]json.RawMessage) (any, int, *rpcError) {
	var request struct {
		ProtocolMajors []int    `json:"protocolMajors"`
		HostFeatures   []string `json:"hostFeatures"`
	}
	if json.Unmarshal(mustMarshal(params), &request) != nil || len(request.ProtocolMajors) == 0 || len(request.ProtocolMajors) > 16 || len(request.HostFeatures) > 100 {
		return nil, 0, invalidParams()
	}
	versionOne := false
	seen := make(map[int]bool, len(request.ProtocolMajors))
	for _, major := range request.ProtocolMajors {
		if major < 1 || seen[major] {
			return nil, 0, invalidParams()
		}
		seen[major] = true
		if major == 1 {
			versionOne = true
		}
	}
	for _, feature := range request.HostFeatures {
		if !validID(feature) {
			return nil, 0, invalidParams()
		}
	}
	if !versionOne {
		return nil, 0, unsupported()
	}
	s.mu.Lock()
	if s.initialized {
		s.mu.Unlock()
		return nil, 0, rpcErr(-32600, "invalid-request", "Adapter is already initialized")
	}
	s.initialized = true
	s.mu.Unlock()
	manifest := map[string]any{
		"id": "worklease.sample.static", "version": "1.0.0",
		"protocol":       map[string]int{"minMajor": 1, "maxMajor": 1},
		"configSchema":   map[string]any{"type": "object", "additionalProperties": false},
		"authentication": []string{}, "resourcePolicy": "generic",
		"capabilities":     []string{"identity", "discovery", "dependencies", "state", "progress", "assignment", "native-claims", "mutation", "synchronization", "effects", "authentication"},
		"requiredFeatures": []string{},
	}
	return map[string]any{"protocolVersion": 1, "manifest": manifest}, 0, nil
}

func mustMarshal(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

func (s *server) dispatchRead(ctx context.Context, method string, params map[string]json.RawMessage, limits requestBudget) (any, *rpcError) {
	if ctx.Err() != nil {
		return nil, unavailable()
	}
	switch method {
	case "resolve":
		return s.resolve(params)
	case "capabilities":
		id, err := s.requireSource(params)
		if err != nil {
			return nil, err
		}
		return map[string]any{"context": wireContext(id, "complete", nil), "capabilities": sampleCapabilities()}, nil
	case "list":
		return s.list(params, limits)
	case "readItems":
		return s.readItems(params, limits)
	case "readDependencies":
		return s.readDependencies(params, limits)
	case "readItem":
		return s.readItem(params, limits)
	case "resourcePolicy":
		return s.resourcePolicy(params)
	default:
		return nil, rpcErr(-32601, "method-not-found", "Method not found")
	}
}

func (s *server) resolve(params map[string]json.RawMessage) (any, *rpcError) {
	id, ok := stringField(params, "sourceId")
	if !ok || !validID(id) {
		return nil, invalidParams()
	}
	configValue, ok := params["config"]
	var config map[string]json.RawMessage
	if !ok || json.Unmarshal(configValue, &config) != nil || config == nil || len(config) != 0 {
		return nil, invalidParams()
	}
	s.mu.Lock()
	if s.resolvedID != "" && s.resolvedID != id {
		s.mu.Unlock()
		return nil, invalidParams()
	}
	s.resolvedID = id
	s.mu.Unlock()
	return map[string]any{
		"context": wireContext(id, "complete", nil),
		"source":  map[string]string{"id": id, "name": "Sample adapter fixture", "locator": "memory://sample-fixture"},
	}, nil
}

func sampleCapabilities() map[string]any {
	result := make(map[string]any)
	for _, name := range []string{"identity", "discovery", "dependencies", "state", "progress", "assignment", "native-claims", "mutation", "synchronization", "effects", "authentication"} {
		capability := map[string]any{"support": "unsupported", "permission": "unknown", "availability": "unavailable", "reason": "static sample adapter is read-only"}
		if name == "identity" || name == "discovery" || name == "dependencies" || name == "state" {
			capability = map[string]any{"support": "supported", "permission": "allowed", "availability": "available"}
		}
		result[name] = capability
	}
	return result
}

func (s *server) list(params map[string]json.RawMessage, limits requestBudget) (any, *rpcError) {
	id, err := s.requireSource(params)
	if err != nil {
		return nil, err
	}
	var request struct {
		Cursor *string `json:"cursor"`
		Query  struct {
			States []string `json:"states"`
			Text   string   `json:"text"`
		} `json:"query"`
	}
	if json.Unmarshal(mustMarshal(params), &request) != nil || params["query"] == nil {
		return nil, invalidParams()
	}
	items := make([]fixtureItem, 0, len(s.fixture.Items))
	stateFilters := make(map[string]bool, len(request.Query.States))
	for _, state := range request.Query.States {
		stateFilters[state] = true
	}
	text := strings.ToLower(request.Query.Text)
	for _, item := range s.fixture.Items {
		if len(stateFilters) != 0 && !stateFilters[item.State] {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(item.Title+" "+item.RawStatus+" "+item.Body), text) {
			continue
		}
		items = append(items, item)
	}
	start, cursorErr := parseCursor(request.Cursor, "list", id)
	if cursorErr != nil || start > len(items) {
		return nil, invalidParams()
	}
	end := min(start+limits.MaxItems, len(items))
	page := make([]any, 0, end-start)
	for index := start; index < end; index++ {
		page = append(page, s.wireItem(id, items[index]))
	}
	for {
		next := nextCursor("list", id, start+len(page), len(items))
		contextState := "complete"
		if next != nil {
			contextState = "partial"
		}
		result := map[string]any{
			"context": wireContext(id, contextState, next), "items": page, "nextCursor": next,
			"total": map[string]any{"value": len(items), "accuracy": "exact"},
		}
		if resultFits(result, limits.MaxBytes) {
			return result, nil
		}
		if len(page) == 0 {
			return nil, unsupported()
		}
		page = page[:len(page)-1]
	}
}

func (s *server) readItems(params map[string]json.RawMessage, limits requestBudget) (any, *rpcError) {
	id, err := s.requireSource(params)
	if err != nil {
		return nil, err
	}
	var request struct {
		Refs []struct {
			SourceID string `json:"sourceId"`
			ItemID   string `json:"itemId"`
		} `json:"refs"`
	}
	if json.Unmarshal(mustMarshal(params), &request) != nil || len(request.Refs) == 0 || len(request.Refs) > maxItems || len(request.Refs) > limits.MaxItems {
		return nil, invalidParams()
	}
	outcomes := make([]any, 0, len(request.Refs))
	for _, ref := range request.Refs {
		if ref.SourceID != id || !validText(ref.ItemID) || ref.ItemID == "" {
			return nil, invalidParams()
		}
		outcomes = append(outcomes, s.wireOutcome(id, ref.ItemID))
	}
	result := map[string]any{"context": wireContext(id, "complete", nil), "outcomes": outcomes}
	for index := len(outcomes) - 1; !resultFits(result, limits.MaxBytes) && index >= 0; index-- {
		outcome := outcomes[index].(map[string]any)
		if outcome["status"] == "found" {
			outcomes[index] = map[string]any{"ref": refValue(id, request.Refs[index].ItemID), "status": "failed", "diagnostic": "item exceeds requested byte budget"}
		}
	}
	if !resultFits(result, limits.MaxBytes) {
		return nil, unsupported()
	}
	return result, nil
}

func (s *server) readDependencies(params map[string]json.RawMessage, limits requestBudget) (any, *rpcError) {
	ref, id, err := s.requireRef(params)
	if err != nil {
		return nil, err
	}
	var request struct {
		Cursor *string `json:"cursor"`
	}
	if json.Unmarshal(mustMarshal(params), &request) != nil {
		return nil, invalidParams()
	}
	edges := make([]fixtureEdge, 0, len(s.fixture.Dependencies))
	for _, edge := range s.fixture.Dependencies {
		if edge.FromID == ref.ItemID || edge.RelationshipType != "dependency" && edge.ToID == ref.ItemID {
			edges = append(edges, edge)
		}
	}
	start, cursorErr := parseCursor(request.Cursor, "deps", id)
	if cursorErr != nil || start > len(edges) {
		return nil, invalidParams()
	}
	end := min(start+limits.MaxItems, len(edges))
	page := make([]any, 0, end-start)
	for index := start; index < end; index++ {
		edge := edges[index]
		page = append(page, map[string]any{
			"from": refValue(id, edge.FromID), "to": refValue(id, edge.ToID),
			"relationshipType": edge.RelationshipType, "direction": edge.Direction,
			"completionCondition": edge.CompletionCondition, "rawOutcome": edge.RawOutcome,
			"interpretation": edge.Interpretation, "provenance": edge.Provenance,
			"providerVersion": edge.ProviderVersion,
		})
	}
	for {
		next := nextCursor("deps", id, start+len(page), len(edges))
		completeness := "complete"
		if next != nil {
			completeness = "partial"
		}
		result := map[string]any{
			"context": wireContext(id, completeness, next), "edges": page,
			"nextCursor": next, "completeness": completeness,
		}
		if resultFits(result, limits.MaxBytes) {
			return result, nil
		}
		if len(page) == 0 {
			return nil, unsupported()
		}
		page = page[:len(page)-1]
	}
}

func (s *server) readItem(params map[string]json.RawMessage, limits requestBudget) (any, *rpcError) {
	ref, id, err := s.requireRef(params)
	if err != nil {
		return nil, err
	}
	outcome := s.wireOutcome(id, ref.ItemID)
	result := map[string]any{"context": wireContext(id, "complete", nil), "outcome": outcome}
	if !resultFits(result, limits.MaxBytes) && outcome["status"] == "found" {
		result["outcome"] = map[string]any{"ref": refValue(id, ref.ItemID), "status": "failed", "diagnostic": "item exceeds requested byte budget"}
	}
	if !resultFits(result, limits.MaxBytes) {
		return nil, unsupported()
	}
	return result, nil
}

func (s *server) resourcePolicy(params map[string]json.RawMessage) (any, *rpcError) {
	ref, id, err := s.requireRef(params)
	if err != nil {
		return nil, err
	}
	workKey, ok := stringField(params, "workKey")
	if !ok || !validID(workKey) {
		return nil, invalidParams()
	}
	return map[string]any{
		"context": wireContext(id, "complete", nil), "policy": "generic",
		"source": id, "item": ref.ItemID, "scope": "item",
	}, nil
}

type workRef struct {
	SourceID string `json:"sourceId"`
	ItemID   string `json:"itemId"`
}

func (s *server) requireSource(params map[string]json.RawMessage) (string, *rpcError) {
	id, ok := stringField(params, "sourceId")
	if !ok || !validID(id) {
		return "", invalidParams()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resolvedID == "" || s.resolvedID != id {
		return "", invalidParams()
	}
	return id, nil
}

func (s *server) requireRef(params map[string]json.RawMessage) (workRef, string, *rpcError) {
	var ref workRef
	if json.Unmarshal(params["ref"], &ref) != nil || !validText(ref.ItemID) || ref.ItemID == "" {
		return workRef{}, "", invalidParams()
	}
	id, err := s.requireSource(map[string]json.RawMessage{"sourceId": mustMarshal(ref.SourceID)})
	if err != nil {
		return workRef{}, "", err
	}
	return ref, id, nil
}

func stringField(fields map[string]json.RawMessage, name string) (string, bool) {
	var value string
	raw, exists := fields[name]
	if !exists || json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func wireContext(sourceID, state string, cursor *string) map[string]any {
	return map[string]any{
		"principal": nil, "configurationGeneration": "sample-fixture-v1",
		"observedAt": time.Now().UTC().Format(time.RFC3339Nano), "providerVersion": "sample-fixture-v1",
		"coverage": map[string]any{"state": state, "scope": sourceID, "cursor": cursor},
	}
}

func refValue(sourceID, itemID string) workRef { return workRef{SourceID: sourceID, ItemID: itemID} }

func (s *server) wireOutcome(sourceID, itemID string) map[string]any {
	ref := refValue(sourceID, itemID)
	if item := s.findItem(itemID); item != nil {
		return map[string]any{"ref": ref, "status": "found", "item": s.wireItem(sourceID, *item)}
	}
	for _, inaccessible := range s.fixture.InaccessibleItemIDs {
		if inaccessible == itemID {
			return map[string]any{"ref": ref, "status": "inaccessible"}
		}
	}
	return map[string]any{"ref": ref, "status": "missing"}
}

func (s *server) wireItem(sourceID string, item fixtureItem) map[string]any {
	terminal := item.State == "complete"
	blocked := item.State == "blocked"
	return map[string]any{
		"ref": refValue(sourceID, item.ID), "title": item.Title, "rawStatus": item.RawStatus,
		"state": item.State, "order": item.Order, "priority": item.Priority,
		"canonicalId": sourceID + ":" + item.ID, "providerReady": item.ProviderReady,
		"assignedTo": item.AssignedTo, "nativeClaim": item.NativeClaim,
		"updatedAt": item.UpdatedAt, "body": item.Body,
		"terminal": terminal, "providerBlocked": blocked,
	}
}

func (s *server) findItem(id string) *fixtureItem {
	for i := range s.fixture.Items {
		if s.fixture.Items[i].ID == id {
			return &s.fixture.Items[i]
		}
	}
	return nil
}

func nextCursor(kind, sourceID string, offset, total int) *string {
	if offset >= total {
		return nil
	}
	value := kind + ":" + base64.RawURLEncoding.EncodeToString([]byte(sourceID)) + ":" + strconv.Itoa(offset)
	return &value
}

func parseCursor(cursor *string, kind, sourceID string) (int, error) {
	if cursor == nil {
		return 0, nil
	}
	parts := strings.Split(*cursor, ":")
	if len(parts) != 3 || parts[0] != kind {
		return 0, errors.New("invalid cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || string(decoded) != sourceID {
		return 0, errors.New("invalid cursor")
	}
	offset, err := strconv.Atoi(parts[2])
	if err != nil || offset < 0 {
		return 0, errors.New("invalid cursor")
	}
	return offset, nil
}

func resultFits(result any, limit int) bool {
	data, err := json.Marshal(result)
	return err == nil && len(data) <= limit
}

func (s *server) writeResult(id string, result any) error {
	return s.writeEnvelope(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *server) writeError(id *string, failure *rpcError) error {
	var responseID any
	if id != nil {
		responseID = *id
	}
	return s.writeEnvelope(map[string]any{"jsonrpc": "2.0", "id": responseID, "error": failure.failure})
}

func (s *server) writeEnvelope(response any) error {
	data, err := json.Marshal(response)
	if err != nil || len(data)+1 > maxFrameBytes {
		return errors.New("response exceeds frame limit")
	}
	data = append(data, '\n')
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	for len(data) != 0 {
		written, writeErr := s.output.Write(data)
		if writeErr != nil {
			return writeErr
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func (s *server) isInitialized() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initialized
}

func (s *server) cancel(id string) {
	s.mu.Lock()
	cancel := s.pending[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *server) cancelPending() {
	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.pending))
	for _, cancel := range s.pending {
		cancels = append(cancels, cancel)
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func validText(value string) bool { return len(value) <= 4096 && utf8.ValidString(value) }

func validateJSON(data []byte) error {
	if !json.Valid(data) {
		return errors.New("invalid JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]bool)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || keys[key] {
				return errors.New("duplicate or invalid object key")
			}
			keys[key] = true
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("invalid JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	return nil
}
