package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/config"
)

const externalAdapterKeyPrefix = "external:"

// ExternalSourceAdapterKey returns the registry key for one configured external source.
// Callers use it for Source.Adapter after resolving that source.
func ExternalSourceAdapterKey(sourceID string) string { return externalAdapterKeyPrefix + sourceID }

// RegisterExternalSources installs one lazy adapter per configured external source.
// Registration never starts an executable; approval is checked when Resolve starts it.
func RegisterExternalSources(registry *Registry, configured []config.QueueSource, env func(string) string) (func(), error) {
	if registry == nil {
		return nil, fmt.Errorf("external source registry is required")
	}
	type registration struct {
		key     string
		adapter *ExternalAdapter
	}
	registrations := make([]registration, 0)
	credentials := new(CredentialHelper)
	seen := make(map[string]bool)
	for _, source := range configured {
		if source.Adapter != "external" {
			continue
		}
		if !validExternalSourceID(source.ID) {
			return nil, fmt.Errorf("external source ID is invalid")
		}
		key := ExternalSourceAdapterKey(source.ID)
		if seen[key] {
			return nil, fmt.Errorf("external source %q is configured more than once", source.ID)
		}
		if _, exists := registry.Get(key); exists {
			return nil, fmt.Errorf("adapter %q already registered", key)
		}
		seen[key] = true
		copy, err := cloneExternalQueueSource(source)
		if err != nil {
			return nil, err
		}
		registrations = append(registrations, registration{key: key, adapter: &ExternalAdapter{source: copy, env: env, credentials: credentials}})
	}
	for i, registration := range registrations {
		if err := registry.Register(registration.key, registration.adapter); err != nil {
			for _, registered := range registrations[:i] {
				registered.adapter.Close()
				registry.unregister(registered.key, registered.adapter)
			}
			return nil, err
		}
	}
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			for _, registration := range registrations {
				registration.adapter.Close()
				registry.unregister(registration.key, registration.adapter)
			}
		})
	}
	return cleanup, nil
}

// ExternalAdapter adapts one supervised external process to the queue read model.
// Its process is created lazily on Resolve and is always bound to one source ID.
type ExternalAdapter struct {
	source      config.QueueSource
	env         func(string) string
	credentials *CredentialHelper

	mu                 sync.Mutex
	resolveMu          sync.Mutex
	process            *ExternalProcess
	resolved           *Source
	resolvedGeneration uint64
	listCursors        map[string]externalListBinding
}

type externalListBinding struct {
	principal               string
	configurationGeneration string
	scope                   string
}

func (a *ExternalAdapter) Close() {
	a.mu.Lock()
	process := a.process
	a.process = nil
	a.resolved = nil
	a.resolvedGeneration = 0
	a.listCursors = nil
	a.mu.Unlock()
	if process != nil {
		process.Close()
	}
}

func (a *ExternalAdapter) Resolve(ctx context.Context, options map[string]string) (Source, error) {
	for key, value := range options {
		if key != "id" || value != a.source.ID {
			return Source{}, fmt.Errorf("external adapter resolve options do not match the configured source")
		}
	}
	if err := contextError(ctx); err != nil {
		return Source{}, err
	}
	a.resolveMu.Lock()
	defer a.resolveMu.Unlock()
	process, err := a.getProcess()
	if err != nil {
		return Source{}, err
	}
	source, processGeneration, err := a.resolveProcess(ctx, process, nil)
	if err != nil {
		return Source{}, err
	}
	a.mu.Lock()
	a.resolved = &source
	a.resolvedGeneration = processGeneration
	a.listCursors = nil
	a.mu.Unlock()
	return source, nil
}

func (a *ExternalAdapter) Capabilities(ctx context.Context, source Source, principal string, ref *Ref) (CapabilitySet, error) {
	process, err := a.validatedProcessContext(ctx, source)
	if err != nil {
		return nil, err
	}
	if ref != nil && !a.validRef(source, *ref) {
		return nil, fmt.Errorf("external adapter reference is outside its configured source")
	}
	var wire struct {
		Context      externalWireContext        `json:"context"`
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	params := map[string]any{"sourceId": source.ID, "principal": nullableString(principal), "budget": externalBudget(100)}
	if ref != nil {
		params["ref"] = *ref
	}
	if err := process.Call(ctx, "capabilities", params, &wire); err != nil {
		return nil, err
	}
	if _, err := wire.Context.observation(); err != nil {
		return nil, err
	}
	if wire.Capabilities == nil {
		return nil, fmt.Errorf("external adapter capabilities result is invalid")
	}
	manifest, ok := process.Manifest()
	if !ok {
		return nil, fmt.Errorf("external adapter manifest is unavailable")
	}
	declared := make(map[string]bool, len(manifest.Capabilities))
	for _, name := range manifest.Capabilities {
		declared[name] = true
	}
	result := make(CapabilitySet)
	for name, raw := range wire.Capabilities {
		if !knownExternalCapability(name) || !declared[name] {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil || fields["support"] == nil || fields["permission"] == nil || fields["availability"] == nil {
			return nil, fmt.Errorf("external adapter capability %q is invalid", name)
		}
		var capability Capability
		if json.Unmarshal(raw, &capability) != nil || !validCapabilityValue(capability) {
			return nil, fmt.Errorf("external adapter capability %q has unsupported semantics", name)
		}
		result[name] = capability
	}
	return result, nil
}

func (a *ExternalAdapter) List(ctx context.Context, source Source, query Query, cursor string) (SummaryPage, error) {
	if !validExternalCursor(cursor) {
		return SummaryPage{}, fmt.Errorf("external adapter cursor is invalid")
	}
	var previousBinding *externalListBinding
	if cursor != "" {
		a.mu.Lock()
		binding, ok := a.listCursors[cursor]
		a.mu.Unlock()
		if !ok {
			return SummaryPage{}, fmt.Errorf("external adapter cursor is not bound to a prior page")
		}
		previousBinding = &binding
	}
	process, err := a.validatedProcessContext(ctx, source)
	if err != nil {
		return SummaryPage{}, err
	}
	for _, field := range query.Fields {
		if !validExternalText(field) {
			return SummaryPage{}, fmt.Errorf("external adapter requested field is invalid")
		}
	}
	states := make([]StateCategory, 0, len(query.Filters.States))
	for _, state := range query.Filters.States {
		if !validState(state) {
			return SummaryPage{}, fmt.Errorf("external adapter query contains an unsupported state")
		}
		states = append(states, state)
	}
	if query.Filters.Text != "" && !validExternalText(query.Filters.Text) {
		return SummaryPage{}, fmt.Errorf("external adapter query text is invalid")
	}
	if len(query.Filters.SourceIDs) > 0 {
		included := false
		for _, id := range query.Filters.SourceIDs {
			if id == source.ID {
				included = true
			}
		}
		if !included {
			coverage := Coverage{State: CoverageComplete, Scope: source.ID, TotalAccuracy: TotalExact}
			observation := Observation{ObservedAt: time.Now().UTC(), Coverage: coverage}
			return SummaryPage{Items: []Summary{}, Coverage: coverage, Observation: observation}, nil
		}
	}
	wireQuery := map[string]any{"states": states, "text": query.Filters.Text}
	if len(query.Filters.SourceIDs) > 0 {
		wireQuery["sourceIds"] = []string{source.ID}
	}
	var result externalListResult
	params := map[string]any{
		"sourceId": source.ID, "query": wireQuery, "cursor": nullableString(cursor),
		"fields": nonNilStrings(query.Fields), "budget": externalBudget(query.Budget),
	}
	if err := process.Call(ctx, "list", params, &result); err != nil {
		return SummaryPage{}, err
	}
	observation, err := result.Context.observation()
	if err != nil {
		return SummaryPage{}, err
	}
	binding := externalListBinding{
		principal: observation.Principal, configurationGeneration: observation.ConfigurationGeneration,
		scope: observation.Coverage.Scope,
	}
	if previousBinding != nil && *previousBinding != binding {
		return SummaryPage{}, fmt.Errorf("external adapter list page changed principal, configuration generation, or scope")
	}
	pageCoverage := observation.Coverage
	pageCoverage.Cursor = cursorValue(result.NextCursor)
	pageCoverage.TotalAccuracy = TotalUnknown
	if result.Total.Accuracy != "exact" && result.Total.Accuracy != "estimated" && result.Total.Accuracy != "unknown" {
		return SummaryPage{}, fmt.Errorf("external adapter total accuracy is unsupported")
	}
	pageCoverage.TotalAccuracy = TotalAccuracy(result.Total.Accuracy)
	if result.Total.Value != nil {
		if *result.Total.Value < 0 {
			return SummaryPage{}, fmt.Errorf("external adapter total is invalid")
		}
		pageCoverage.Total = *result.Total.Value
	} else {
		if result.Total.Accuracy == "exact" {
			return SummaryPage{}, fmt.Errorf("external adapter exact total has no value")
		}
		pageCoverage.TotalAccuracy = TotalUnknown
	}
	if result.NextCursor != nil {
		if *result.NextCursor == "" || !validExternalCursor(*result.NextCursor) {
			return SummaryPage{}, fmt.Errorf("external adapter next cursor is invalid")
		}
		pageCoverage.Cursor = *result.NextCursor
		if pageCoverage.State != CoverageUnknown {
			pageCoverage.State = CoveragePartial
		}
	}
	page := SummaryPage{Items: make([]Summary, 0, len(result.Items)), NextCursor: cursorValue(result.NextCursor), Coverage: pageCoverage, Observation: observation}
	page.Observation.Coverage = pageCoverage
	seen := make(map[string]bool, len(result.Items))
	for _, wireItem := range result.Items {
		summary, _, err := mapExternalItem(source, wireItem)
		if err != nil {
			return SummaryPage{}, err
		}
		if seen[summary.Ref.Key()] {
			return SummaryPage{}, fmt.Errorf("external adapter returned duplicate item references")
		}
		seen[summary.Ref.Key()] = true
		page.Items = append(page.Items, summary)
	}
	if err := a.updateListCursor(cursor, page.NextCursor, binding); err != nil {
		return SummaryPage{}, err
	}
	return page, nil
}

func (a *ExternalAdapter) updateListCursor(cursor, nextCursor string, binding externalListBinding) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cursor != "" {
		previous, ok := a.listCursors[cursor]
		if !ok || previous != binding {
			return fmt.Errorf("external adapter list cursor binding changed")
		}
	}
	if nextCursor != "" {
		if previous, ok := a.listCursors[nextCursor]; ok && previous != binding {
			return fmt.Errorf("external adapter next cursor is already bound to another principal, configuration generation, or scope")
		}
	}
	if cursor != "" {
		delete(a.listCursors, cursor)
	}
	if nextCursor != "" {
		if a.listCursors == nil {
			a.listCursors = make(map[string]externalListBinding)
		}
		a.listCursors[nextCursor] = binding
	}
	return nil
}

func (a *ExternalAdapter) ReadItems(ctx context.Context, source Source, refs []Ref, fields []string, budget int) []ItemOutcome {
	out := make([]ItemOutcome, len(refs))
	for i, ref := range refs {
		out[i].Ref = ref
		if !a.validRef(source, ref) {
			out[i].Kind = "failed"
			out[i].Err = fmt.Errorf("external adapter reference is outside its configured source")
		}
	}
	_, err := a.validatedProcessContext(ctx, source)
	if err != nil {
		for i := range out {
			if out[i].Err == nil {
				out[i].Kind = "failed"
				out[i].Err = err
			}
		}
		return out
	}
	for _, field := range fields {
		if !validExternalText(field) {
			err = fmt.Errorf("external adapter requested field is invalid")
			break
		}
	}
	if err != nil {
		for i := range out {
			if out[i].Err == nil {
				out[i].Kind = "failed"
				out[i].Err = err
			}
		}
		return out
	}
	limit := externalPageSize(budget)
	unique := make([]Ref, 0, min(limit, len(refs)))
	uniqueIndexes := make(map[string][]int, len(refs))
	for i, ref := range refs {
		if out[i].Err != nil {
			continue
		}
		uniqueIndexes[ref.Key()] = append(uniqueIndexes[ref.Key()], i)
		if len(uniqueIndexes[ref.Key()]) == 1 {
			unique = append(unique, ref)
		}
	}
	for start := 0; start < len(unique); start += limit {
		end := min(start+limit, len(unique))
		batch := unique[start:end]
		var result externalReadItemsResult
		params := map[string]any{"sourceId": source.ID, "refs": batch, "fields": nonNilStrings(fields), "budget": externalBudget(len(batch))}
		process, callErr := a.validatedProcessContext(ctx, source)
		if callErr == nil {
			callErr = process.Call(ctx, "readItems", params, &result)
		}
		if callErr != nil {
			for _, ref := range batch {
				for _, index := range uniqueIndexes[ref.Key()] {
					out[index].Kind, out[index].Err = "failed", callErr
				}
			}
			continue
		}
		observation, contextErr := result.Context.observation()
		if contextErr != nil {
			for _, ref := range batch {
				for _, index := range uniqueIndexes[ref.Key()] {
					out[index].Kind, out[index].Err = "failed", contextErr
				}
			}
			continue
		}
		mapped, mapErr := mapExternalOutcomes(source, batch, result.Outcomes, observation)
		if mapErr != nil {
			for _, ref := range batch {
				for _, index := range uniqueIndexes[ref.Key()] {
					out[index].Kind, out[index].Err = "failed", mapErr
				}
			}
			continue
		}
		for _, ref := range batch {
			outcome := mapped[ref.Key()]
			for _, index := range uniqueIndexes[ref.Key()] {
				out[index] = outcome
				out[index].Ref = refs[index]
			}
		}
	}
	return out
}

func (a *ExternalAdapter) ReadDependencies(ctx context.Context, source Source, ref Ref, cursor string, budget int) (DependencyPage, error) {
	process, err := a.validatedProcessContext(ctx, source)
	if err != nil {
		return DependencyPage{}, err
	}
	if !a.validRef(source, ref) || !validExternalCursor(cursor) {
		return DependencyPage{}, fmt.Errorf("external adapter dependency reference or cursor is invalid")
	}
	var result externalDependenciesResult
	params := map[string]any{"ref": ref, "cursor": nullableString(cursor), "budget": externalBudget(budget)}
	if err := process.Call(ctx, "readDependencies", params, &result); err != nil {
		return DependencyPage{}, err
	}
	observation, err := result.Context.observation()
	if err != nil {
		return DependencyPage{}, err
	}
	completeness, err := mapCoverageState(result.Completeness)
	if err != nil {
		return DependencyPage{}, err
	}
	if observation.Coverage.State == CoverageUnknown || completeness == CoverageUnknown {
		completeness = CoverageUnknown
	} else if observation.Coverage.State == CoveragePartial || completeness == CoveragePartial || result.NextCursor != nil {
		completeness = CoveragePartial
	}
	if result.NextCursor != nil && (*result.NextCursor == "" || !validExternalCursor(*result.NextCursor)) {
		return DependencyPage{}, fmt.Errorf("external adapter dependency cursor is invalid")
	}
	page := DependencyPage{Edges: make([]Relationship, 0, len(result.Edges)), NextCursor: cursorValue(result.NextCursor), Completeness: completeness, Observation: observation}
	page.Observation.Coverage.State = completeness
	page.Observation.Coverage.Cursor = page.NextCursor
	page.Observation.Coverage.ObservedEdges = len(result.Edges)
	page.Observation.Coverage.TotalAccuracy = TotalUnknown
	for _, edge := range result.Edges {
		mapped, err := mapExternalEdge(source, ref, edge)
		if err != nil {
			return DependencyPage{}, err
		}
		page.Edges = append(page.Edges, mapped)
	}
	return page, nil
}

func (a *ExternalAdapter) getProcess() (*ExternalProcess, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.process != nil {
		return a.process, nil
	}
	process, err := NewExternalProcess(a.source, a.env)
	if err != nil {
		return nil, err
	}
	if a.credentials != nil {
		process.credentials = a.credentials
	}
	a.process = process
	return process, nil
}

func (a *ExternalAdapter) resolveProcess(ctx context.Context, process *ExternalProcess, expected *Source) (Source, uint64, error) {
	manifest, err := process.Initialize(ctx)
	if err != nil {
		return Source{}, 0, err
	}
	if err := validateExternalConfig(manifest.ConfigSchema, a.source.Config); err != nil {
		return Source{}, 0, err
	}
	generation, ok := process.Generation()
	if !ok {
		return Source{}, 0, fmt.Errorf("external adapter process is unavailable")
	}
	params := map[string]any{"sourceId": a.source.ID, "config": a.source.Config}
	if a.source.CredentialRef != "" {
		params["credentialRef"] = a.source.CredentialRef
	}
	var result externalResolveResult
	if err := process.Call(ctx, "resolve", params, &result); err != nil {
		return Source{}, 0, err
	}
	observation, err := result.Context.observation()
	if err != nil {
		return Source{}, 0, err
	}
	if len(a.source.CredentialHelper) != 0 && observation.Principal != a.source.Account {
		return Source{}, 0, fmt.Errorf("credential-scope-mismatch: resolved principal differs from approved account")
	}
	if result.Source.ID != a.source.ID || !validExternalText(result.Source.Name) || !validExternalLocator(result.Source.Locator) {
		return Source{}, 0, fmt.Errorf("external adapter resolved a different or invalid source")
	}
	source := Source{ID: result.Source.ID, Name: result.Source.Name, Locator: result.Source.Locator, Adapter: ExternalSourceAdapterKey(a.source.ID)}
	if expected != nil && source != *expected {
		return Source{}, 0, fmt.Errorf("external adapter source identity changed after process restart")
	}
	currentGeneration, alive := process.Generation()
	if !alive || currentGeneration != generation {
		return Source{}, 0, fmt.Errorf("external adapter process changed during source resolution")
	}
	return source, generation, nil
}

func (a *ExternalAdapter) validatedProcess(source Source) (*ExternalProcess, error) {
	return a.validatedProcessContext(context.Background(), source)
}

func (a *ExternalAdapter) validatedProcessContext(ctx context.Context, source Source) (*ExternalProcess, error) {
	if source.ID != a.source.ID || source.Adapter != ExternalSourceAdapterKey(a.source.ID) {
		return nil, fmt.Errorf("external adapter source identity does not match its configured source")
	}
	a.resolveMu.Lock()
	defer a.resolveMu.Unlock()
	a.mu.Lock()
	process := a.process
	if process == nil || a.resolved == nil {
		a.mu.Unlock()
		return nil, fmt.Errorf("external adapter source must be resolved first")
	}
	resolved := *a.resolved
	resolvedGeneration := a.resolvedGeneration
	a.mu.Unlock()
	if source != resolved {
		return nil, fmt.Errorf("external adapter source identity changed")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if _, err := process.Initialize(ctx); err != nil {
		return nil, err
	}
	generation, alive := process.Generation()
	if !alive {
		return nil, fmt.Errorf("external adapter process is unavailable")
	}
	if generation != resolvedGeneration {
		if _, generation, err := a.resolveProcess(ctx, process, &resolved); err != nil {
			return nil, err
		} else {
			a.mu.Lock()
			if a.process != process || a.resolved == nil || *a.resolved != resolved {
				a.mu.Unlock()
				return nil, fmt.Errorf("external adapter source binding changed during process restart")
			}
			a.resolvedGeneration = generation
			a.mu.Unlock()
		}
	}
	return process, nil
}

func (a *ExternalAdapter) validRef(source Source, ref Ref) bool {
	return ref.SourceID == source.ID && ref.ItemID != "" && validExternalText(ref.ItemID)
}

type externalWireContext struct {
	Principal               *string `json:"principal"`
	ConfigurationGeneration string  `json:"configurationGeneration"`
	ObservedAt              string  `json:"observedAt"`
	ProviderVersion         *string `json:"providerVersion"`
	Coverage                struct {
		State  string  `json:"state"`
		Scope  string  `json:"scope"`
		Cursor *string `json:"cursor"`
	} `json:"coverage"`
}

func (c *externalWireContext) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return fmt.Errorf("invalid context")
	}
	for _, name := range []string{"principal", "configurationGeneration", "observedAt", "coverage", "providerVersion"} {
		if fields[name] == nil {
			return fmt.Errorf("missing context field")
		}
	}
	var coverage map[string]json.RawMessage
	if json.Unmarshal(fields["coverage"], &coverage) != nil || coverage == nil || coverage["state"] == nil || coverage["scope"] == nil {
		return fmt.Errorf("invalid coverage context")
	}
	type wireContext externalWireContext
	var decoded wireContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*c = externalWireContext(decoded)
	return nil
}

func (c externalWireContext) observation() (Observation, error) {
	observedAt, err := time.Parse(time.RFC3339Nano, c.ObservedAt)
	if err != nil || observedAt.IsZero() || !validExternalText(c.ConfigurationGeneration) || !validExternalText(c.Coverage.Scope) {
		return Observation{}, fmt.Errorf("external adapter observation context is invalid")
	}
	coverageState, err := mapCoverageState(c.Coverage.State)
	if err != nil {
		return Observation{}, err
	}
	coverage := Coverage{State: coverageState, Scope: c.Coverage.Scope, TotalAccuracy: TotalUnknown}
	if c.Coverage.Cursor != nil {
		if *c.Coverage.Cursor == "" || !validExternalCursor(*c.Coverage.Cursor) {
			return Observation{}, fmt.Errorf("external adapter observation cursor is invalid")
		}
		coverage.Cursor = *c.Coverage.Cursor
	}
	observation := Observation{ConfigurationGeneration: c.ConfigurationGeneration, ObservedAt: observedAt.UTC(), Coverage: coverage}
	if c.Principal != nil {
		if !validExternalText(*c.Principal) {
			return Observation{}, fmt.Errorf("external adapter principal is invalid")
		}
		observation.Principal = *c.Principal
	}
	if c.ProviderVersion != nil {
		if !validExternalText(*c.ProviderVersion) {
			return Observation{}, fmt.Errorf("external adapter provider version is invalid")
		}
		observation.ProviderVersion = *c.ProviderVersion
	}
	return observation, nil
}

type externalResolveResult struct {
	Context externalWireContext `json:"context"`
	Source  struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Locator string `json:"locator"`
	} `json:"source"`
}

func (r *externalResolveResult) UnmarshalJSON(data []byte) error {
	if err := requireExternalJSONFields(data, "context", "source"); err != nil {
		return err
	}
	var envelope map[string]json.RawMessage
	var source map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil || json.Unmarshal(envelope["source"], &source) != nil || source == nil || source["id"] == nil || source["name"] == nil || source["locator"] == nil {
		return fmt.Errorf("invalid resolved source")
	}
	type resolveResult externalResolveResult
	var decoded resolveResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = externalResolveResult(decoded)
	return nil
}

type externalListResult struct {
	Context    externalWireContext `json:"context"`
	Items      []json.RawMessage   `json:"items"`
	NextCursor *string             `json:"nextCursor"`
	Total      struct {
		Value    *int   `json:"value"`
		Accuracy string `json:"accuracy"`
	} `json:"total"`
}

func (r *externalListResult) UnmarshalJSON(data []byte) error {
	if err := requireExternalJSONFields(data, "context", "items", "nextCursor", "total"); err != nil {
		return err
	}
	var envelope map[string]json.RawMessage
	var total map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil || json.Unmarshal(envelope["total"], &total) != nil || total == nil || total["value"] == nil || total["accuracy"] == nil {
		return fmt.Errorf("invalid list total")
	}
	type listResult externalListResult
	var decoded listResult
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Items == nil {
		return fmt.Errorf("invalid list items")
	}
	*r = externalListResult(decoded)
	return nil
}

type externalReadItemsResult struct {
	Context  externalWireContext `json:"context"`
	Outcomes []json.RawMessage   `json:"outcomes"`
}

func (r *externalReadItemsResult) UnmarshalJSON(data []byte) error {
	if err := requireExternalJSONFields(data, "context", "outcomes"); err != nil {
		return err
	}
	type itemsResult externalReadItemsResult
	var decoded itemsResult
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Outcomes == nil {
		return fmt.Errorf("invalid item outcomes")
	}
	*r = externalReadItemsResult(decoded)
	return nil
}

type externalDependenciesResult struct {
	Context      externalWireContext `json:"context"`
	Edges        []json.RawMessage   `json:"edges"`
	NextCursor   *string             `json:"nextCursor"`
	Completeness string              `json:"completeness"`
}

func (r *externalDependenciesResult) UnmarshalJSON(data []byte) error {
	if err := requireExternalJSONFields(data, "context", "edges", "nextCursor", "completeness"); err != nil {
		return err
	}
	type dependenciesResult externalDependenciesResult
	var decoded dependenciesResult
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Edges == nil {
		return fmt.Errorf("invalid dependency edges")
	}
	*r = externalDependenciesResult(decoded)
	return nil
}

func requireExternalJSONFields(data []byte, names ...string) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return fmt.Errorf("invalid result object")
	}
	for _, name := range names {
		if fields[name] == nil {
			return fmt.Errorf("missing result field")
		}
	}
	return nil
}

type externalItem struct {
	Ref             Ref           `json:"ref"`
	Title           string        `json:"title"`
	RawStatus       string        `json:"rawStatus"`
	State           StateCategory `json:"state"`
	Order           string        `json:"order"`
	Priority        int           `json:"priority"`
	CanonicalID     string        `json:"canonicalId"`
	ProviderReady   *bool         `json:"providerReady"`
	AssignedTo      []string      `json:"assignedTo"`
	NativeClaim     string        `json:"nativeClaim"`
	UpdatedAt       time.Time     `json:"updatedAt"`
	Body            string        `json:"body"`
	Terminal        *bool         `json:"terminal"`
	ProviderBlocked *bool         `json:"providerBlocked"`
}

func mapExternalItem(source Source, raw json.RawMessage) (Summary, Item, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil || fields["ref"] == nil {
		return Summary{}, Item{}, fmt.Errorf("external adapter item is invalid")
	}
	var wire externalItem
	if json.Unmarshal(raw, &wire) != nil || wire.Ref.SourceID != source.ID || !validExternalText(wire.Ref.ItemID) {
		return Summary{}, Item{}, fmt.Errorf("external adapter item reference escaped its configured source")
	}
	for _, value := range []string{wire.Title, wire.RawStatus, string(wire.State), wire.Order, wire.CanonicalID, wire.NativeClaim} {
		if !validExternalText(value) {
			return Summary{}, Item{}, fmt.Errorf("external adapter item text is invalid")
		}
	}
	if wire.Body != "" && !utf8.ValidString(wire.Body) {
		return Summary{}, Item{}, fmt.Errorf("external adapter item body is invalid")
	}
	if wire.State != "" && !validState(wire.State) {
		wire.State = StateUnknown
	}
	if wire.State == "" {
		wire.State = StateUnknown
	}
	terminalKnown := wire.State != StateUnknown
	terminal := wire.State == StateComplete
	if wire.Terminal != nil {
		if terminalKnown && terminal != *wire.Terminal {
			return Summary{}, Item{}, fmt.Errorf("external adapter item terminal semantics conflict with its state")
		}
		terminal, terminalKnown = *wire.Terminal, true
	}
	providerBlocked := wire.State == StateBlocked
	if wire.ProviderBlocked != nil {
		if wire.State == StateBlocked && !*wire.ProviderBlocked {
			return Summary{}, Item{}, fmt.Errorf("external adapter item blocked semantics conflict with its state")
		}
		providerBlocked = *wire.ProviderBlocked
	}
	state := wire.State
	if terminal && state == StateUnknown {
		state = StateComplete
	} else if !terminalKnown {
		state = StateUnknown
	}
	if state == StateComplete {
		terminal, terminalKnown = true, true
	}
	if _, exists := fields["assignedTo"]; exists {
		if fields["assignedTo"] == nil || string(fields["assignedTo"]) == "null" {
			return Summary{}, Item{}, fmt.Errorf("external adapter assignee list is invalid")
		}
		if wire.AssignedTo == nil {
			return Summary{}, Item{}, fmt.Errorf("external adapter assignee list is invalid")
		}
		for _, owner := range wire.AssignedTo {
			if !validExternalText(owner) {
				return Summary{}, Item{}, fmt.Errorf("external adapter assignee is invalid")
			}
		}
	}
	summary := Summary{
		Ref: wire.Ref, Title: wire.Title, RawStatus: wire.RawStatus, State: state, Order: wire.Order,
		Priority: wire.Priority, CanonicalID: wire.CanonicalID, ProviderReady: wire.ProviderReady,
		AssignedTo: append([]string(nil), wire.AssignedTo...), NativeClaim: wire.NativeClaim,
		UpdatedAt: wire.UpdatedAt, Fresh: true, Terminal: terminal, ProviderBlocked: providerBlocked,
	}
	item := Item{Summary: summary, Body: wire.Body, TerminalKnown: terminalKnown, ReadOutcome: "found", ReadPermission: Allowed}
	if _, exists := fields["assignedTo"]; exists {
		item.Assignment = Assignment{Owners: append([]string(nil), wire.AssignedTo...), Known: true, Assigned: len(wire.AssignedTo) > 0}
	}
	if state == StateUnknown {
		item.ReadPermission = PermissionUnknown
	}
	return summary, item, nil
}

func mapExternalOutcomes(source Source, requested []Ref, rawOutcomes []json.RawMessage, observation Observation) (map[string]ItemOutcome, error) {
	if len(rawOutcomes) != len(requested) {
		return nil, fmt.Errorf("external adapter did not return one outcome per requested reference")
	}
	requestedByKey := make(map[string]Ref, len(requested))
	for _, ref := range requested {
		requestedByKey[ref.Key()] = ref
	}
	mapped := make(map[string]ItemOutcome, len(requested))
	for _, raw := range rawOutcomes {
		var fields map[string]json.RawMessage
		var outcome struct {
			Ref    Ref             `json:"ref"`
			Status string          `json:"status"`
			Item   json.RawMessage `json:"item"`
		}
		if json.Unmarshal(raw, &fields) != nil || fields == nil || fields["ref"] == nil || fields["status"] == nil || json.Unmarshal(raw, &outcome) != nil {
			return nil, fmt.Errorf("external adapter returned an invalid item outcome")
		}
		ref, requestedRef := requestedByKey[outcome.Ref.Key()]
		if !requestedRef || outcome.Ref != ref || outcome.Ref.SourceID != source.ID || mapped[ref.Key()].Ref != (Ref{}) {
			return nil, fmt.Errorf("external adapter item outcomes do not match the exact requested references")
		}
		mappedOutcome := ItemOutcome{Ref: ref, Kind: outcome.Status, Observation: observation}
		switch outcome.Status {
		case "found":
			if len(outcome.Item) == 0 || bytes.Equal(outcome.Item, []byte("null")) {
				return nil, fmt.Errorf("external adapter found outcome has no item")
			}
			summary, item, err := mapExternalItem(source, outcome.Item)
			if err != nil || summary.Ref != ref {
				return nil, fmt.Errorf("external adapter found outcome returned a different item reference")
			}
			item.Observation = observation
			item.Coverage = observation.Coverage
			mappedOutcome.Item = &item
		case "missing":
			if len(outcome.Item) != 0 {
				return nil, fmt.Errorf("external adapter missing outcome unexpectedly contains an item")
			}
		case "inaccessible":
			if len(outcome.Item) != 0 {
				return nil, fmt.Errorf("external adapter inaccessible outcome unexpectedly contains an item")
			}
		case "failed":
			if len(outcome.Item) != 0 {
				return nil, fmt.Errorf("external adapter failed outcome unexpectedly contains an item")
			}
			mappedOutcome.Err = fmt.Errorf("external adapter item read failed")
		default:
			return nil, fmt.Errorf("external adapter returned an unsupported item outcome")
		}
		mapped[ref.Key()] = mappedOutcome
	}
	if len(mapped) != len(requested) {
		return nil, fmt.Errorf("external adapter omitted a requested item outcome")
	}
	return mapped, nil
}

type externalWireEdge struct {
	From                Ref             `json:"from"`
	To                  Ref             `json:"to"`
	RelationshipType    string          `json:"relationshipType"`
	Direction           string          `json:"direction"`
	CompletionCondition *string         `json:"completionCondition"`
	RawOutcome          json.RawMessage `json:"rawOutcome"`
	Interpretation      json.RawMessage `json:"interpretation"`
	DeclaredProvenance  string          `json:"provenance"`
	ProviderVersion     string          `json:"providerVersion"`
}

func mapExternalEdge(source Source, requested Ref, raw json.RawMessage) (Relationship, error) {
	var fields map[string]json.RawMessage
	var wire externalWireEdge
	if json.Unmarshal(raw, &fields) != nil || fields == nil || fields["from"] == nil || fields["to"] == nil ||
		fields["relationshipType"] == nil || fields["direction"] == nil || fields["completionCondition"] == nil ||
		fields["rawOutcome"] == nil || fields["interpretation"] == nil || json.Unmarshal(raw, &wire) != nil {
		return Relationship{}, fmt.Errorf("external adapter dependency edge is invalid")
	}
	if wire.From.SourceID != source.ID || !validExternalText(wire.From.ItemID) || wire.To.SourceID != source.ID || !validExternalText(wire.To.ItemID) ||
		wire.From != requested && wire.To != requested {
		return Relationship{}, fmt.Errorf("external adapter dependency edge escaped its requested source or reference")
	}
	var interpretationFields map[string]json.RawMessage
	if json.Unmarshal(wire.Interpretation, &interpretationFields) != nil || interpretationFields == nil {
		return Relationship{}, fmt.Errorf("external adapter dependency interpretation is invalid")
	}
	if !validExternalText(wire.RelationshipType) || !validExternalText(wire.DeclaredProvenance) || !validExternalText(wire.ProviderVersion) {
		return Relationship{}, fmt.Errorf("external adapter dependency edge text is invalid")
	}
	condition := ""
	if wire.CompletionCondition != nil {
		if !validExternalText(*wire.CompletionCondition) {
			return Relationship{}, fmt.Errorf("external adapter dependency condition is invalid")
		}
		condition = *wire.CompletionCondition
	}
	rawOutcome := ""
	if len(wire.RawOutcome) != 0 && string(wire.RawOutcome) != "null" {
		rawOutcome = string(wire.RawOutcome)
	}
	interpretation, interpretationKnown := externalInterpretation(wire.Interpretation)
	from, to := wire.From, wire.To
	direction := UnknownDirection
	support := Supported
	typeOfRelationship := Related
	switch wire.RelationshipType {
	case "dependency", "prerequisite", "hard-prerequisite":
		switch wire.Direction {
		case "prerequisite":
			direction, typeOfRelationship = DependentToPrerequisite, HardPrerequisite
		case "dependent":
			direction, typeOfRelationship = DependentToPrerequisite, HardPrerequisite
			from, to = wire.To, wire.From
		case "non-blocking":
			direction, typeOfRelationship = NonBlockingDirection, Related
		case "unknown":
			direction, typeOfRelationship, support = UnknownDirection, HardPrerequisite, SupportUnknown
			if wire.From != requested {
				from, to = requested, wire.From
			} else {
				from, to = requested, wire.To
			}
		default:
			return Relationship{}, fmt.Errorf("external adapter dependency direction is unsupported")
		}
		if typeOfRelationship == HardPrerequisite && condition == "" {
			support = SupportUnknown
		}
		if typeOfRelationship == HardPrerequisite && condition != "terminal" && !interpretationKnown {
			support = SupportUnknown
		}
	case "parent-child", "hierarchy":
		typeOfRelationship = ParentChild
		switch wire.Direction {
		case "prerequisite", "dependent":
			direction = ParentToChild
		case "non-blocking":
			direction = NonBlockingDirection
		case "unknown":
			direction, support = UnknownDirection, SupportUnknown
		default:
			return Relationship{}, fmt.Errorf("external adapter hierarchy direction is unsupported")
		}
		interpretation = "hierarchy only"
	case "related":
		typeOfRelationship = Related
		switch wire.Direction {
		case "non-blocking":
			direction = NonBlockingDirection
		case "unknown":
			direction, support = UnknownDirection, SupportUnknown
		case "prerequisite", "dependent":
			direction, support = NonBlockingDirection, SupportUnknown
		default:
			return Relationship{}, fmt.Errorf("external adapter related-work direction is unsupported")
		}
	default:
		switch wire.Direction {
		case "prerequisite":
			direction, typeOfRelationship, support = DependentToPrerequisite, HardPrerequisite, SupportUnknown
		case "dependent":
			direction, typeOfRelationship, support = DependentToPrerequisite, HardPrerequisite, SupportUnknown
			from, to = wire.To, wire.From
		case "non-blocking":
			direction, typeOfRelationship, support = NonBlockingDirection, Related, SupportUnknown
		case "unknown":
			direction, typeOfRelationship, support = UnknownDirection, HardPrerequisite, SupportUnknown
			if wire.From != requested {
				from, to = requested, wire.From
			} else {
				from, to = requested, wire.To
			}
		default:
			return Relationship{}, fmt.Errorf("external adapter dependency direction is unsupported")
		}
		interpretation = string(wire.Interpretation)
	}
	if typeOfRelationship == HardPrerequisite && from != requested {
		return Relationship{}, fmt.Errorf("external adapter prerequisite direction does not match the requested item")
	}
	provenance := wire.DeclaredProvenance
	if provenance == "" {
		provenance = "external-adapter." + wire.RelationshipType
	}
	return Relationship{Type: typeOfRelationship, Direction: direction, From: from, To: to, Provenance: provenance,
		Condition: condition, RawOutcome: rawOutcome, Interpretation: interpretation, Fresh: true, Support: support}, nil
}

func externalInterpretation(raw json.RawMessage) (string, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return "", false
	}
	for _, name := range []string{"result", "status", "outcome"} {
		if value := object[name]; len(value) > 0 {
			var text string
			if json.Unmarshal(value, &text) != nil {
				return "", false
			}
			switch text {
			case "satisfied", "unsatisfied":
				return text, true
			case "unknown":
				return "", false
			default:
				return "", false
			}
		}
	}
	return "", false
}

func knownExternalCapability(name string) bool {
	switch name {
	case "identity", "discovery", "dependencies", "state", "progress", "assignment", "native-claims", "mutation", "synchronization", "effects", "authentication":
		return true
	default:
		return false
	}
}

func validCapabilityValue(capability Capability) bool {
	switch capability.Support {
	case Supported, Unsupported, SupportUnknown:
	default:
		return false
	}
	switch capability.Permission {
	case Allowed, Denied, PermissionUnknown:
	default:
		return false
	}
	switch capability.Availability {
	case Available, Unavailable, AuthenticationRequired:
	default:
		return false
	}
	for key, value := range capability.Semantics {
		if !validExternalText(key) || !validExternalText(value) {
			return false
		}
	}
	for key, value := range capability.Limits {
		if !validExternalText(key) || value < 0 {
			return false
		}
	}
	return validExternalText(capability.Reason)
}

func validExternalLocator(value string) bool {
	if !validExternalText(value) {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return !strings.Contains(value, "://")
	}
	if parsed.User != nil {
		return false
	}
	for key := range parsed.Query() {
		normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "").Replace(key))
		if normalized == "credentialref" || isExternalCredentialField(key) {
			return false
		}
	}
	return true
}

func validExternalSourceID(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}

func validExternalText(value string) bool   { return len(value) <= 4096 && utf8.ValidString(value) }
func validExternalCursor(value string) bool { return len(value) <= 4096 && utf8.ValidString(value) }

func validState(state StateCategory) bool {
	switch state {
	case StateOpen, StateInProgress, StateBlocked, StateComplete, StateUnknown:
		return true
	default:
		return false
	}
}

func mapCoverageState(value string) (CoverageState, error) {
	switch CoverageState(value) {
	case CoverageComplete, CoveragePartial, CoverageUnknown:
		return CoverageState(value), nil
	default:
		return CoverageUnknown, fmt.Errorf("external adapter coverage state is unsupported")
	}
}

func externalPageSize(requested int) int {
	if requested <= 0 || requested > externalMaxItems {
		return externalMaxItems
	}
	return requested
}

func externalBudget(maxItems int) map[string]int {
	return map[string]int{"maxItems": externalPageSize(maxItems), "maxBytes": externalResultLimit}
}

func cursorValue(cursor *string) string {
	if cursor == nil {
		return ""
	}
	return *cursor
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func contextError(ctx context.Context) error {
	if ctx != nil {
		return ctx.Err()
	}
	return nil
}

func cloneExternalQueueSource(source config.QueueSource) (config.QueueSource, error) {
	encoded, err := json.Marshal(source.Config)
	if err != nil {
		return config.QueueSource{}, fmt.Errorf("external adapter source configuration is invalid")
	}
	var copied map[string]any
	if len(source.Config) == 0 {
		copied = map[string]any{}
	} else if err := json.Unmarshal(encoded, &copied); err != nil {
		return config.QueueSource{}, fmt.Errorf("external adapter source configuration is invalid")
	}
	source.Config = copied
	source.CredentialHelper = append([]string(nil), source.CredentialHelper...)
	return source, nil
}

func validateExternalConfig(rawSchema json.RawMessage, configuration map[string]any) error {
	var schema map[string]any
	decoder := json.NewDecoder(bytes.NewReader(rawSchema))
	decoder.UseNumber()
	if err := decoder.Decode(&schema); err != nil || schema == nil {
		return fmt.Errorf("external adapter configuration schema is invalid")
	}
	if err := validateExternalSchemaStructure(schema); err != nil {
		return fmt.Errorf("external adapter configuration schema is unsupported: %w", err)
	}
	configurationJSON, err := json.Marshal(configuration)
	if err != nil {
		return fmt.Errorf("external adapter source configuration is invalid")
	}
	var value any
	valueDecoder := json.NewDecoder(bytes.NewReader(configurationJSON))
	valueDecoder.UseNumber()
	if err := valueDecoder.Decode(&value); err != nil {
		return fmt.Errorf("external adapter source configuration is invalid")
	}
	if err := validateExternalSchemaValue(schema, value); err != nil {
		return fmt.Errorf("external adapter source configuration does not match its manifest schema: %w", err)
	}
	return nil
}

func validateExternalSchemaStructure(schema map[string]any) error {
	allowed := map[string]bool{
		"$schema": true, "$id": true, "$comment": true, "title": true, "description": true,
		"type": true, "properties": true, "required": true, "additionalProperties": true, "items": true,
		"enum": true, "const": true, "minLength": true, "maxLength": true, "minimum": true, "maximum": true,
		"minItems": true, "maxItems": true, "minProperties": true, "maxProperties": true, "pattern": true,
	}
	for keyword, value := range schema {
		if !allowed[keyword] {
			return fmt.Errorf("unknown schema keyword %q", keyword)
		}
		switch keyword {
		case "$schema", "$id", "$comment", "title", "description":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("invalid %s annotation", keyword)
			}
		case "type":
			if !externalSchemaTypeDeclarationValid(value) {
				return fmt.Errorf("unsupported type declaration")
			}
		case "properties":
			properties, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid properties schema")
			}
			for name, rawChild := range properties {
				child, ok := rawChild.(map[string]any)
				if !ok {
					return fmt.Errorf("unsupported schema for property %q", name)
				}
				if err := validateExternalSchemaStructure(child); err != nil {
					return fmt.Errorf("property %q: %w", name, err)
				}
			}
		case "required":
			required, ok := value.([]any)
			if !ok {
				return fmt.Errorf("invalid required schema")
			}
			seen := make(map[string]bool, len(required))
			for _, rawName := range required {
				name, ok := rawName.(string)
				if !ok || seen[name] {
					return fmt.Errorf("invalid required property name")
				}
				seen[name] = true
			}
		case "additionalProperties":
			if child, ok := value.(map[string]any); ok {
				if err := validateExternalSchemaStructure(child); err != nil {
					return fmt.Errorf("additional properties: %w", err)
				}
			} else if _, ok := value.(bool); !ok {
				return fmt.Errorf("invalid additionalProperties schema")
			}
		case "items":
			child, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("unsupported items schema")
			}
			if err := validateExternalSchemaStructure(child); err != nil {
				return fmt.Errorf("items: %w", err)
			}
		case "enum":
			values, ok := value.([]any)
			if !ok || len(values) == 0 {
				return fmt.Errorf("invalid enum schema")
			}
		case "pattern":
			pattern, ok := value.(string)
			if !ok {
				return fmt.Errorf("invalid pattern schema")
			}
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("invalid pattern schema")
			}
		case "minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties":
			number, ok := externalSchemaNumber(value)
			if !ok || number < 0 || math.Trunc(number) != number {
				return fmt.Errorf("invalid %s bound", keyword)
			}
		case "minimum", "maximum":
			if _, ok := externalSchemaNumber(value); !ok {
				return fmt.Errorf("invalid %s bound", keyword)
			}
		}
	}
	return nil
}

func externalSchemaTypeDeclarationValid(value any) bool {
	validName := func(name string) bool {
		switch name {
		case "object", "array", "string", "boolean", "number", "integer", "null":
			return true
		default:
			return false
		}
	}
	if name, ok := value.(string); ok {
		return validName(name)
	}
	types, ok := value.([]any)
	if !ok || len(types) == 0 {
		return false
	}
	seen := make(map[string]bool, len(types))
	for _, rawType := range types {
		name, ok := rawType.(string)
		if !ok || !validName(name) || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}

func validateExternalSchemaValue(schema map[string]any, value any) error {
	allowed := map[string]bool{
		"$schema": true, "$id": true, "$comment": true, "title": true, "description": true,
		"type": true, "properties": true, "required": true, "additionalProperties": true, "items": true,
		"enum": true, "const": true, "minLength": true, "maxLength": true, "minimum": true, "maximum": true,
		"minItems": true, "maxItems": true, "minProperties": true, "maxProperties": true, "pattern": true,
	}
	for keyword := range schema {
		if !allowed[keyword] {
			return fmt.Errorf("unsupported schema keyword %q", keyword)
		}
	}
	if rawType, exists := schema["type"]; exists {
		if !externalSchemaTypeMatches(rawType, value) {
			return fmt.Errorf("value has the wrong type")
		}
	}
	if enum, exists := schema["enum"]; exists {
		choices, ok := enum.([]any)
		if !ok {
			return fmt.Errorf("invalid enum schema")
		}
		matched := false
		for _, choice := range choices {
			if externalJSONEqual(choice, value) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("value is not an allowed enum member")
		}
	}
	if constant, exists := schema["const"]; exists && !externalJSONEqual(constant, value) {
		return fmt.Errorf("value does not match the required constant")
	}
	if object, ok := value.(map[string]any); ok {
		properties, _ := schema["properties"].(map[string]any)
		if requiredRaw, exists := schema["required"]; exists {
			required, ok := requiredRaw.([]any)
			if !ok {
				return fmt.Errorf("invalid required schema")
			}
			for _, field := range required {
				name, ok := field.(string)
				if !ok {
					return fmt.Errorf("invalid required schema")
				}
				if _, exists := object[name]; !exists {
					return fmt.Errorf("required property %q is missing", name)
				}
			}
		}
		if !externalSchemaCountValid(schema, "minProperties", len(object), true) || !externalSchemaCountValid(schema, "maxProperties", len(object), false) {
			return fmt.Errorf("object property count is outside the declared bounds")
		}
		additional := schema["additionalProperties"]
		for name, child := range object {
			childSchema, declared := properties[name].(map[string]any)
			if !declared {
				switch value := additional.(type) {
				case bool:
					if !value {
						return fmt.Errorf("unknown property %q", name)
					}
				case map[string]any:
					childSchema = value
				default:
					continue
				}
			}
			if err := validateExternalSchemaValue(childSchema, child); err != nil {
				return fmt.Errorf("property %q: %w", name, err)
			}
		}
	}
	if array, ok := value.([]any); ok {
		if !externalSchemaCountValid(schema, "minItems", len(array), true) || !externalSchemaCountValid(schema, "maxItems", len(array), false) {
			return fmt.Errorf("array item count is outside the declared bounds")
		}
		if itemSchema, exists := schema["items"]; exists {
			childSchema, ok := itemSchema.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid items schema")
			}
			for i, child := range array {
				if err := validateExternalSchemaValue(childSchema, child); err != nil {
					return fmt.Errorf("array item %d: %w", i, err)
				}
			}
		}
	}
	if text, ok := value.(string); ok {
		length := utf8.RuneCountInString(text)
		if !externalSchemaCountValid(schema, "minLength", length, true) || !externalSchemaCountValid(schema, "maxLength", length, false) {
			return fmt.Errorf("string length is outside the declared bounds")
		}
		if pattern, exists := schema["pattern"]; exists {
			patternText, ok := pattern.(string)
			if !ok {
				return fmt.Errorf("invalid pattern schema")
			}
			compiled, err := regexp.Compile(patternText)
			if err != nil || !compiled.MatchString(text) {
				return fmt.Errorf("string does not match the declared pattern")
			}
		}
	}
	if number, ok := externalSchemaNumber(value); ok {
		if minimum, exists := schema["minimum"]; exists {
			bound, valid := externalSchemaNumber(minimum)
			if !valid || number < bound {
				return fmt.Errorf("number is below the declared minimum")
			}
		}
		if maximum, exists := schema["maximum"]; exists {
			bound, valid := externalSchemaNumber(maximum)
			if !valid || number > bound {
				return fmt.Errorf("number exceeds the declared maximum")
			}
		}
	}
	return nil
}

func externalSchemaTypeMatches(schemaType any, value any) bool {
	if types, ok := schemaType.([]any); ok {
		for _, candidate := range types {
			if name, ok := candidate.(string); ok && externalSchemaTypeMatches(name, value) {
				return true
			}
		}
		return false
	}
	name, ok := schemaType.(string)
	if !ok {
		return false
	}
	matches := false
	switch name {
	case "object":
		_, matches = value.(map[string]any)
	case "array":
		_, matches = value.([]any)
	case "string":
		_, matches = value.(string)
	case "boolean":
		_, matches = value.(bool)
	case "number":
		_, matches = externalSchemaNumber(value)
	case "integer":
		var number float64
		number, matches = externalSchemaNumber(value)
		matches = matches && math.Trunc(number) == number
	case "null":
		matches = value == nil
	default:
		return false
	}
	return matches
}

func externalSchemaCountValid(schema map[string]any, keyword string, count int, minimum bool) bool {
	raw, exists := schema[keyword]
	if !exists {
		return true
	}
	number, ok := externalSchemaNumber(raw)
	if !ok || math.Trunc(number) != number || number < 0 {
		return false
	}
	if minimum {
		return float64(count) >= number
	}
	return float64(count) <= number
}

func externalSchemaNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	case float64:
		return number, true
	case int:
		return float64(number), true
	default:
		return 0, false
	}
}

func externalJSONEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

var _ Adapter = (*ExternalAdapter)(nil)
