package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ExternalWriteAdapter bridges the queue's journaled write pipeline to one
// already-resolved, source-scoped external adapter process.
type ExternalWriteAdapter struct{ *ExternalAdapter }

// ExternalWritePreview is safe to display in the parent CLI before confirmation.
type ExternalWritePreview struct {
	Operation        string         `json:"operation"`
	Patch            map[string]any `json:"patch"`
	ExpectedVersion  *string        `json:"expectedVersion"`
	ConditionalWrite bool           `json:"conditionalWrite"`
	CurrentStatus    string         `json:"currentStatus"`
	Readiness        Readiness      `json:"readiness"`
}

func NewExternalWriteAdapter(read *ExternalAdapter) *ExternalWriteAdapter {
	return &ExternalWriteAdapter{ExternalAdapter: read}
}

func (a *ExternalWriteAdapter) ValidateTransition(_ context.Context, source Source, action Action, transition string) error {
	if a == nil || a.ExternalAdapter == nil || source.ID != a.source.ID || source.Adapter != ExternalSourceAdapterKey(a.source.ID) {
		return fmt.Errorf("external adapter source identity does not match its configured source")
	}
	key := externalWorkflowKey(action)
	if key == "" || transition == "" || a.source.Workflow[key] == "" || a.source.Workflow[key] != transition {
		return fmt.Errorf("no-workflow-mapping")
	}
	return nil
}

// Prepare captures the current provider version and a canonical, explicit
// mutation patch. It never dispatches a provider mutation.
func (a *ExternalWriteAdapter) Prepare(ctx context.Context, intent WriteIntent) (WriteIntent, ExternalWritePreview, error) {
	var preview ExternalWritePreview
	prepared, err := a.prepareIntent(intent)
	if err != nil {
		return intent, preview, err
	}
	pre, item, _, err := a.inspect(ctx, prepared, false)
	if err != nil {
		return intent, preview, err
	}
	if !pre.Capability || !pre.Authorized || !pre.InScope || !pre.Fresh || !pre.NativeAvailable {
		return intent, preview, fmt.Errorf("external provider write is not authorized or available")
	}
	if prepared.Precondition != "" && prepared.Precondition != pre.Precondition {
		return intent, preview, fmt.Errorf("conflict: provider version changed before preview")
	}
	prepared.Precondition = pre.Precondition
	preview = ExternalWritePreview{
		Operation:        externalWriteMethod(prepared.Action),
		Patch:            externalWritePatch(prepared),
		ExpectedVersion:  nullableString(prepared.Precondition),
		ConditionalWrite: false,
		CurrentStatus:    item.RawStatus,
		Readiness:        item.Readiness,
	}
	return prepared, preview, nil
}

func (a *ExternalWriteAdapter) Inspect(ctx context.Context, intent WriteIntent) (WritePreflight, error) {
	prepared, err := a.prepareIntent(intent)
	if err != nil {
		return WritePreflight{}, err
	}
	pre, _, _, err := a.inspect(ctx, prepared, true)
	return pre, err
}

func (a *ExternalWriteAdapter) inspect(ctx context.Context, intent WriteIntent, checkVersion bool) (WritePreflight, Item, ExternalAdapterManifest, error) {
	var pre WritePreflight
	var root Item
	process, manifest, err := a.writeProcess(intent.Source)
	if err != nil {
		return pre, root, ExternalAdapterManifest{}, err
	}
	if err := a.validatePreparedIntent(intent); err != nil {
		return pre, root, manifest, err
	}
	method := externalWriteMethod(intent.Action)
	capabilities, err := a.actionCapabilities(ctx, process, manifest, intent, method)
	if err != nil {
		return pre, root, manifest, err
	}
	group := externalActionCapability(intent.Action)
	mutationCap, mutationOK := capabilities["mutation"]
	groupCap, groupOK := capabilities[group]
	pre.Capability = mutationOK && groupOK && externalCapabilityAvailable(mutationCap) && externalCapabilityAvailable(groupCap)
	pre.Authorized = mutationOK && groupOK && mutationCap.Permission == Allowed && groupCap.Permission == Allowed
	pre.InScope = true
	pre.NativeAvailable = true // The configured generic claim key is host-owned.
	pre.Owner = true           // Claim ownership is independently verified by WritePipeline.

	var observation Observation
	root, observation, err = a.readAuthoritativeItem(ctx, process, intent.Source, intent.Ref)
	if err != nil {
		return pre, Item{}, manifest, err
	}
	pre.Precondition = observation.ProviderVersion
	pre.Fresh = !observation.ObservedAt.IsZero() && root.Fresh
	if checkVersion && pre.Precondition != intent.Precondition {
		return pre, root, manifest, fmt.Errorf("conflict: provider version changed since preview")
	}
	items, err := a.inspectDependencyClosure(ctx, intent.Source, root)
	if err != nil {
		return pre, root, manifest, err
	}
	root = items[intent.Ref.Key()]
	pre.Ready = root.Readiness.Status == Ready
	// A configured, explicitly selected completion transition is the generic
	// provider's completion declaration; no unconfigured state is inferred.
	pre.CompletionEvidence = intent.Action == ActionComplete && intent.Transition == a.source.Workflow["complete"]
	return pre, root, manifest, nil
}

func (a *ExternalWriteAdapter) actionCapabilities(ctx context.Context, process *ExternalProcess, manifest ExternalAdapterManifest, intent WriteIntent, method string) (map[string]Capability, error) {
	declared := make(map[string]bool, len(manifest.Capabilities))
	for _, name := range manifest.Capabilities {
		declared[name] = true
	}
	group := externalActionCapability(intent.Action)
	if !declared["mutation"] || !declared[group] {
		return map[string]Capability{}, nil
	}
	var result externalWriteCapabilitiesResult
	params := map[string]any{
		"sourceId":  intent.Source.ID,
		"principal": intent.Principal,
		"ref":       intent.Ref,
		"action":    method,
		"budget":    externalBudget(1),
	}
	if err := process.Call(ctx, "capabilities", params, &result); err != nil {
		return nil, err
	}
	observation, err := result.Context.observation()
	if err != nil {
		return nil, err
	}
	if observation.Principal != intent.Principal {
		return nil, fmt.Errorf("external adapter did not authorize the configured principal")
	}
	out := make(map[string]Capability, 2)
	for _, name := range []string{"mutation", group} {
		raw, ok := result.Capabilities[name]
		if !ok {
			continue
		}
		var capability Capability
		if err := json.Unmarshal(raw, &capability); err != nil || !validCapabilityValue(capability) {
			return nil, fmt.Errorf("external adapter capability %q is invalid", name)
		}
		out[name] = capability
	}
	return out, nil
}

func (a *ExternalWriteAdapter) Write(ctx context.Context, intent WriteIntent) (ProviderReceipt, error) {
	var empty ProviderReceipt
	prepared, err := a.prepareIntent(intent)
	if err != nil {
		return empty, err
	}
	pre, _, manifest, err := a.inspect(ctx, prepared, true)
	if err != nil {
		return empty, err
	}
	if !pre.Capability || !pre.Authorized || !pre.InScope || !pre.Fresh || !pre.NativeAvailable || !actionWriteEligible(prepared.Action, pre) {
		return empty, fmt.Errorf("external provider write preflight rejected")
	}
	process, _, err := a.writeProcess(prepared.Source)
	if err != nil {
		return empty, err
	}
	if !externalWriteContains(manifest.Capabilities, "mutation") {
		return empty, fmt.Errorf("external adapter manifest does not declare mutation capability")
	}
	method := externalWriteMethod(prepared.Action)
	params := map[string]any{
		"ref":             prepared.Ref,
		"operationId":     prepared.OperationID,
		"patch":           externalWritePatch(prepared),
		"expectedVersion": nullableString(prepared.Precondition),
		"authority": map[string]string{
			"authorizationRef": prepared.OperationID,
			"scope":            prepared.Source.ID,
		},
		"budget": externalBudget(1),
	}
	var result externalMutationResult
	if err := process.Call(ctx, method, params, &result); err != nil {
		return empty, err
	}
	observation, err := result.Context.observation()
	if err != nil || observation.Principal != prepared.Principal {
		return empty, fmt.Errorf("external adapter mutation response context is invalid")
	}
	wireReceipt := result.Receipt
	if wireReceipt.SourceID != prepared.Source.ID || wireReceipt.Ref == nil || *wireReceipt.Ref != prepared.Ref || wireReceipt.Operation != method {
		return empty, fmt.Errorf("external adapter mutation receipt identity mismatch")
	}
	receipt := ProviderReceipt{
		SourceID: prepared.Source.ID,
		ItemID:   prepared.Ref.ItemID,
		ID:       wireReceipt.DurableLocation,
	}
	if wireReceipt.ProviderVersion != nil {
		receipt.Version = *wireReceipt.ProviderVersion
	}
	if actor := externalObservedActor(wireReceipt.ObservedState); actor != "" {
		receipt.Actor = actor
	}
	return receipt, nil
}

func (a *ExternalWriteAdapter) ReadReceipt(ctx context.Context, intent WriteIntent, receipt *ProviderReceipt) (ReceiptObservation, error) {
	observed := ReceiptObservation{Outcome: WriteUnknown, SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, Precondition: intent.Precondition, Patch: map[string]string{}}
	prepared, err := a.prepareIntent(intent)
	if err != nil {
		return observed, err
	}
	if err := a.validateReadSource(prepared.Source); err != nil {
		return observed, err
	}
	process, err := a.validatedProcess(prepared.Source)
	if err != nil {
		return observed, err
	}
	var wireReceipt any
	if receipt != nil {
		if receipt.SourceID != prepared.Ref.SourceID || receipt.ItemID != prepared.Ref.ItemID || receipt.ID == "" || !validExternalLocator(receipt.ID) {
			return observed, fmt.Errorf("journaled external provider receipt identity mismatch")
		}
		wireReceipt = externalReceiptForReadback(*receipt, prepared)
	}
	params := map[string]any{
		"sourceId":    prepared.Source.ID,
		"operation":   externalWriteMethod(prepared.Action),
		"operationId": prepared.OperationID,
		"target":      prepared.Ref,
		"intent": map[string]any{
			"payload":         externalWritePatch(prepared),
			"expectedVersion": nullableString(prepared.Precondition),
		},
		"receipt": wireReceipt,
		"budget":  externalBudget(1),
	}
	var result externalReadReceiptResult
	if err := process.Call(ctx, "readReceipt", params, &result); err != nil {
		return observed, err
	}
	if _, err := result.Context.observation(); err != nil {
		return observed, err
	}
	var evidence externalWriteEvidence
	if err := json.Unmarshal(result.Evidence, &evidence); err != nil {
		return observed, fmt.Errorf("external adapter receipt evidence is invalid")
	}
	switch result.Verification {
	case "verified":
		observed.Outcome = WriteVerified
	case "conflict":
		observed.Outcome = WriteConflict
	case "unknown":
		observed.Outcome = WriteUnknown
	default:
		return observed, fmt.Errorf("external adapter receipt verification is invalid")
	}
	observed.SourceID = evidence.SourceID
	observed.ItemID = evidence.ItemID
	observed.Precondition = evidence.Precondition
	observed.Patch = externalEvidencePatch(evidence.Patch)
	observed.MarkerCount = evidence.MarkerCount
	observed.AppendContent = evidence.AppendContent
	observed.AppendProof = evidence.AppendProof
	observed.ReceiptID = evidence.ReceiptID
	if observed.ReceiptID == "" {
		observed.ReceiptID = evidence.DurableLocation
	}
	if observed.ReceiptID != "" && !validExternalLocator(observed.ReceiptID) {
		return ReceiptObservation{Outcome: WriteUnknown, SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, Precondition: intent.Precondition, Patch: map[string]string{}}, fmt.Errorf("external adapter receipt evidence is invalid")
	}
	observed.OperationID = evidence.OperationID
	observed.Actor = evidence.Actor
	observed.Effects = evidence.Effects
	return observed, nil
}

func (a *ExternalWriteAdapter) prepareIntent(intent WriteIntent) (WriteIntent, error) {
	if err := a.validateReadSource(intent.Source); err != nil {
		return intent, err
	}
	if !validOperationID(intent.OperationID) || intent.Ref.SourceID != intent.Source.ID || !a.validRef(intent.Source, intent.Ref) || intent.Principal == "" || !validExternalText(intent.Principal) {
		return intent, fmt.Errorf("invalid external write intent")
	}
	switch intent.Action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
		if err := a.ValidateTransition(context.Background(), intent.Source, intent.Action, intent.Transition); err != nil {
			return intent, err
		}
		if len(intent.Patch) == 0 {
			intent.Patch = map[string]string{"status": intent.Transition}
		}
		if !exactStringMap(intent.Patch, map[string]string{"status": intent.Transition}) || intent.Append != "" || intent.Marker != "" {
			return intent, fmt.Errorf("external state intent must contain only its configured status")
		}
	case ActionRecordProgress:
		if len(intent.Patch) == 0 || len(intent.Patch) != 1 || intent.Patch["append"] != "notes" && intent.Patch["append"] != "comment" || strings.TrimSpace(intent.Append) == "" || !validExternalText(intent.Append) || strings.Contains(intent.Append, "worklease-op:") {
			return intent, fmt.Errorf("external progress intent requires a target and nonempty content")
		}
		marker := "worklease-op:" + intent.OperationID
		if intent.Marker != "" && intent.Marker != marker {
			return intent, fmt.Errorf("external progress marker does not match its operation")
		}
		intent.Marker = marker
	case ActionAssignToMe:
		if intent.Append != "" || intent.Marker != "" {
			return intent, fmt.Errorf("external assignment cannot include progress content")
		}
		if len(intent.Patch) == 0 {
			intent.Patch = map[string]string{"assignee": intent.Principal}
		}
		if !exactStringMap(intent.Patch, map[string]string{"assignee": intent.Principal}) {
			return intent, fmt.Errorf("external assignment must target only the configured principal")
		}
	default:
		return intent, fmt.Errorf("external adapter does not support this write action")
	}
	return intent, nil
}

func (a *ExternalWriteAdapter) validatePreparedIntent(intent WriteIntent) error {
	prepared, err := a.prepareIntent(intent)
	if err != nil {
		return err
	}
	if !exactStringMap(prepared.Patch, intent.Patch) || prepared.Marker != intent.Marker {
		return fmt.Errorf("external write intent is not prepared")
	}
	return nil
}

func (a *ExternalWriteAdapter) validateReadSource(source Source) error {
	if a == nil || a.ExternalAdapter == nil || source.ID != a.source.ID || source.Adapter != ExternalSourceAdapterKey(a.source.ID) || a.source.Adapter != "external" {
		return fmt.Errorf("external adapter source identity does not match its configured source")
	}
	return nil
}

func (a *ExternalWriteAdapter) writeProcess(source Source) (*ExternalProcess, ExternalAdapterManifest, error) {
	if err := a.validateReadSource(source); err != nil {
		return nil, ExternalAdapterManifest{}, err
	}
	if a.source.Claims == nil || a.source.Claims.Policy != "generic" || !validExternalClaimSource(a.source.Claims.Source) {
		return nil, ExternalAdapterManifest{}, fmt.Errorf("external writes require an explicit generic claim binding")
	}
	process, err := a.validatedProcess(source)
	if err != nil {
		return nil, ExternalAdapterManifest{}, err
	}
	manifest, ok := process.Manifest()
	if !ok || manifest.ResourcePolicy != "generic" || !externalWriteContains(manifest.Capabilities, "mutation") {
		return nil, ExternalAdapterManifest{}, fmt.Errorf("external adapter manifest does not declare generic mutation capability")
	}
	return process, manifest, nil
}

func (a *ExternalWriteAdapter) readAuthoritativeItem(ctx context.Context, process *ExternalProcess, source Source, ref Ref) (Item, Observation, error) {
	var empty Item
	var result externalReadItemResult
	if err := process.Call(ctx, "readItem", map[string]any{"ref": ref, "budget": externalBudget(1)}, &result); err != nil {
		return empty, Observation{}, err
	}
	observation, err := result.Context.observation()
	if err != nil {
		return empty, Observation{}, err
	}
	if result.Outcome.Ref != ref || result.Outcome.Status != "found" || len(result.Outcome.Item) == 0 || bytes.Equal(bytes.TrimSpace(result.Outcome.Item), []byte("null")) {
		return empty, Observation{}, fmt.Errorf("external adapter did not return the requested authoritative item")
	}
	_, item, err := mapExternalItem(source, result.Outcome.Item)
	if err != nil || item.Ref != ref {
		return empty, Observation{}, fmt.Errorf("external adapter authoritative item is invalid")
	}
	item.Observation = observation
	item.Coverage = observation.Coverage
	return item, observation, nil
}

func (a *ExternalWriteAdapter) inspectDependencyClosure(ctx context.Context, source Source, root Item) (map[string]Item, error) {
	items := map[string]Item{root.Ref.Key(): root}
	queue := []Ref{root.Ref}
	visited := make(map[string]bool)
	totalEdges := 0
	for next := 0; next < len(queue); next++ {
		ref := queue[next]
		if visited[ref.Key()] {
			continue
		}
		visited[ref.Key()] = true
		cursor := ""
		var edges []Relationship
		cursorSeen := make(map[string]bool)
		complete := false
		for pageCount := 0; pageCount < externalMaxItems; pageCount++ {
			page, err := a.ReadDependencies(ctx, source, ref, cursor, externalMaxItems)
			if err != nil {
				return nil, err
			}
			if page.Completeness == CoverageUnknown {
				return nil, fmt.Errorf("external dependency conditions are incomplete")
			}
			edges = append(edges, page.Edges...)
			totalEdges += len(page.Edges)
			if len(edges) > externalMaxItems || totalEdges > externalMaxItems {
				return nil, fmt.Errorf("external dependency closure exceeds the item limit")
			}
			if page.NextCursor == "" {
				complete = page.Completeness == CoverageComplete
				break
			}
			if cursorSeen[page.NextCursor] {
				return nil, fmt.Errorf("external dependency cursor repeated")
			}
			cursorSeen[page.NextCursor] = true
			cursor = page.NextCursor
		}
		if !complete {
			return nil, fmt.Errorf("external dependency closure is incomplete")
		}
		item := items[ref.Key()]
		item.Relationships = append([]Relationship{}, edges...)
		item.Dependencies = nil
		item.DependenciesKnown = true
		item.Closure = CoverageComplete
		items[ref.Key()] = item

		missing := make([]Ref, 0)
		missingSeen := make(map[string]bool)
		for _, edge := range edges {
			if edge.From != ref || edge.Type != HardPrerequisite {
				continue
			}
			if _, ok := items[edge.To.Key()]; !ok && !missingSeen[edge.To.Key()] {
				missingSeen[edge.To.Key()] = true
				missing = append(missing, edge.To)
			}
		}
		if len(items)+len(missing) > externalMaxItems {
			return nil, fmt.Errorf("external dependency closure exceeds the item limit")
		}
		if len(missing) == 0 {
			continue
		}
		outcomes := a.ReadItems(ctx, source, missing, []string{"state", "terminal", "providerBlocked"}, len(missing))
		for index, outcome := range outcomes {
			if outcome.Ref != missing[index] || outcome.Kind != "found" || outcome.Item == nil || outcome.Item.ReadPermission != Allowed || !outcome.Item.Fresh {
				return nil, fmt.Errorf("external prerequisite could not be refreshed")
			}
			items[missing[index].Key()] = *outcome.Item
			queue = append(queue, missing[index])
		}
	}
	return Recompute(items, CoverageComplete), nil
}

func externalWorkflowKey(action Action) string {
	switch action {
	case ActionStart, ActionResume:
		return "start"
	case ActionReportBlocked:
		return "blocked"
	case ActionRequestReview:
		return "review"
	case ActionComplete:
		return "complete"
	case ActionReopen:
		return "reopen"
	default:
		return ""
	}
}

func externalActionCapability(action Action) string {
	switch action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
		return "state"
	case ActionRecordProgress:
		return "progress"
	case ActionAssignToMe:
		return "assignment"
	default:
		return ""
	}
}

func externalWriteMethod(action Action) string {
	switch action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
		return "writeState"
	case ActionRecordProgress:
		return "recordProgress"
	case ActionAssignToMe:
		return "assign"
	default:
		return ""
	}
}

func externalWritePatch(intent WriteIntent) map[string]any {
	patch := make(map[string]any, len(intent.Patch)+2)
	for key, value := range intent.Patch {
		patch[key] = value
	}
	if intent.Action == ActionRecordProgress {
		patch["content"] = intent.Append
		patch["marker"] = intent.Marker
	}
	return patch
}

func externalCapabilityAvailable(capability Capability) bool {
	return capability.Support == Supported && capability.Availability == Available
}

func externalReceiptForReadback(receipt ProviderReceipt, intent WriteIntent) map[string]any {
	var version *string
	if receipt.Version != "" {
		version = &receipt.Version
	}
	observedState := map[string]any{}
	if receipt.Actor != "" {
		observedState["actor"] = receipt.Actor
	}
	return map[string]any{
		"sourceId":         receipt.SourceID,
		"ref":              intent.Ref,
		"operation":        externalWriteMethod(intent.Action),
		"providerVersion":  version,
		"durableLocation":  receipt.ID,
		"observedState":    observedState,
		"conditionalWrite": false,
		"fencingEvidence":  nil,
	}
}

func externalObservedActor(raw json.RawMessage) string {
	var state struct {
		Actor string `json:"actor"`
	}
	if json.Unmarshal(raw, &state) != nil || !validExternalText(state.Actor) {
		return ""
	}
	return state.Actor
}

func externalEvidencePatch(raw json.RawMessage) map[string]string {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return map[string]string{}
	}
	patch := make(map[string]string, len(values))
	for key, value := range values {
		var text string
		if json.Unmarshal(value, &text) == nil {
			patch[key] = text
		}
	}
	return patch
}

func externalWriteContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

type externalWriteCapabilitiesResult struct {
	Context      externalWireContext        `json:"context"`
	Capabilities map[string]json.RawMessage `json:"capabilities"`
}

func (r *externalWriteCapabilitiesResult) UnmarshalJSON(data []byte) error {
	if err := requireExternalJSONFields(data, "context", "capabilities"); err != nil {
		return err
	}
	var raw struct {
		Context      externalWireContext        `json:"context"`
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || raw.Capabilities == nil {
		return fmt.Errorf("invalid external write capabilities")
	}
	*r = externalWriteCapabilitiesResult(raw)
	return nil
}

type externalReadItemOutcome struct {
	Ref    Ref             `json:"ref"`
	Status string          `json:"status"`
	Item   json.RawMessage `json:"item"`
}

type externalReadItemResult struct {
	Context externalWireContext     `json:"context"`
	Outcome externalReadItemOutcome `json:"outcome"`
}

func (r *externalReadItemResult) UnmarshalJSON(data []byte) error {
	if err := requireExternalJSONFields(data, "context", "outcome"); err != nil {
		return err
	}
	var envelope map[string]json.RawMessage
	var outcomeFields map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil || json.Unmarshal(envelope["outcome"], &outcomeFields) != nil || outcomeFields == nil || outcomeFields["ref"] == nil || outcomeFields["status"] == nil {
		return fmt.Errorf("invalid external authoritative item result")
	}
	var decoded struct {
		Context externalWireContext     `json:"context"`
		Outcome externalReadItemOutcome `json:"outcome"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	switch decoded.Outcome.Status {
	case "found":
		if outcomeFields["item"] == nil || bytes.Equal(bytes.TrimSpace(decoded.Outcome.Item), []byte("null")) {
			return fmt.Errorf("found external item result has no item")
		}
	case "missing", "inaccessible", "failed":
		if outcomeFields["item"] != nil {
			return fmt.Errorf("non-found external item result contains an item")
		}
	default:
		return fmt.Errorf("unsupported external item result")
	}
	*r = externalReadItemResult(decoded)
	return nil
}

type externalWireProviderReceipt struct {
	SourceID         string          `json:"sourceId"`
	Ref              *Ref            `json:"ref"`
	Operation        string          `json:"operation"`
	ProviderVersion  *string         `json:"providerVersion"`
	DurableLocation  string          `json:"durableLocation"`
	ObservedState    json.RawMessage `json:"observedState"`
	ConditionalWrite bool            `json:"conditionalWrite"`
	FencingEvidence  json.RawMessage `json:"fencingEvidence"`
}

type externalMutationResult struct {
	Context externalWireContext         `json:"context"`
	Receipt externalWireProviderReceipt `json:"receipt"`
}

func (r *externalMutationResult) UnmarshalJSON(data []byte) error {
	if err := requireExternalJSONFields(data, "context", "receipt"); err != nil {
		return err
	}
	var envelope map[string]json.RawMessage
	var receiptFields map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil || json.Unmarshal(envelope["receipt"], &receiptFields) != nil || receiptFields == nil {
		return fmt.Errorf("invalid external provider receipt")
	}
	for _, field := range []string{"sourceId", "ref", "operation", "providerVersion", "durableLocation", "observedState", "conditionalWrite", "fencingEvidence"} {
		if receiptFields[field] == nil {
			return fmt.Errorf("external provider receipt is missing a required field")
		}
	}
	type mutationResult externalMutationResult
	var decoded mutationResult
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Receipt.SourceID == "" || decoded.Receipt.Operation == "" || !validExternalLocator(decoded.Receipt.DurableLocation) || decoded.Receipt.DurableLocation == "" {
		return fmt.Errorf("invalid external provider receipt")
	}
	if _, err := decoded.Context.observation(); err != nil {
		return fmt.Errorf("invalid external mutation response context")
	}
	if decoded.Receipt.Ref == nil || decoded.Receipt.Ref.SourceID == "" || decoded.Receipt.Ref.ItemID == "" {
		return fmt.Errorf("external provider receipt reference is invalid")
	}
	if decoded.Receipt.ProviderVersion != nil && !validExternalText(*decoded.Receipt.ProviderVersion) {
		return fmt.Errorf("external provider receipt version is invalid")
	}
	var state map[string]json.RawMessage
	if json.Unmarshal(decoded.Receipt.ObservedState, &state) != nil || state == nil {
		return fmt.Errorf("external provider receipt observed state is invalid")
	}
	var conditional bool
	if json.Unmarshal(receiptFields["conditionalWrite"], &conditional) != nil {
		return fmt.Errorf("external provider receipt conditional-write value is invalid")
	}
	if !bytes.Equal(bytes.TrimSpace(decoded.Receipt.FencingEvidence), []byte("null")) {
		var fencing map[string]json.RawMessage
		if json.Unmarshal(decoded.Receipt.FencingEvidence, &fencing) != nil || fencing == nil {
			return fmt.Errorf("external provider receipt fencing evidence is invalid")
		}
	}
	*r = externalMutationResult(decoded)
	return nil
}

type externalWriteEvidence struct {
	SourceID        string          `json:"sourceId"`
	ItemID          string          `json:"itemId"`
	Precondition    string          `json:"precondition"`
	Patch           json.RawMessage `json:"patch"`
	MarkerCount     int             `json:"markerCount"`
	AppendContent   string          `json:"appendContent"`
	AppendProof     bool            `json:"appendProof"`
	ReceiptID       string          `json:"receiptId"`
	DurableLocation string          `json:"durableLocation"`
	OperationID     string          `json:"operationId"`
	Actor           string          `json:"actor"`
	Effects         map[string]bool `json:"effects"`
}

type externalReadReceiptResult struct {
	Context      externalWireContext `json:"context"`
	Verification string              `json:"verification"`
	Evidence     json.RawMessage     `json:"evidence"`
}

func (r *externalReadReceiptResult) UnmarshalJSON(data []byte) error {
	if err := requireExternalJSONFields(data, "context", "verification", "evidence"); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil || fields["evidence"] == nil {
		return fmt.Errorf("invalid external read-receipt result")
	}
	type readReceiptResult externalReadReceiptResult
	var decoded readReceiptResult
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Verification != "verified" && decoded.Verification != "conflict" && decoded.Verification != "unknown" {
		return fmt.Errorf("invalid external read-receipt result")
	}
	var evidence map[string]json.RawMessage
	if json.Unmarshal(decoded.Evidence, &evidence) != nil || evidence == nil {
		return fmt.Errorf("invalid external read-receipt evidence")
	}
	*r = externalReadReceiptResult(decoded)
	return nil
}

var _ WriteAdapter = (*ExternalWriteAdapter)(nil)
