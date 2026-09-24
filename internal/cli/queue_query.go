package cli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
	"github.com/brettinternet/worklease/internal/reason"
	urfavecli "github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

type queueQueryEnvelope struct {
	SchemaVersion int                `json:"schemaVersion"`
	View          string             `json:"view"`
	Authority     queueAuthorityJSON `json:"authority"`
	Sources       []queueSourceJSON  `json:"sources"`
	Items         []queueQueryItem   `json:"items"`
	NextCursor    string             `json:"nextCursor,omitempty"`
	Incomplete    bool               `json:"incomplete"`
}
type queueAuthorityJSON struct {
	Profile string `json:"profile"`
	ID      string `json:"authorityId"`
	Scope   string `json:"scope"`
}
type queueSourceJSON struct {
	ID              string         `json:"id"`
	Coverage        queue.Coverage `json:"coverage"`
	Freshness       string         `json:"freshness"`
	ObservedAt      time.Time      `json:"observedAt,omitempty"`
	ServedFromIndex bool           `json:"servedFromIndex"`
	Diagnostics     []string       `json:"diagnostics,omitempty"`
}
type queueQueryItem struct {
	queue.Item
	DisplayID string                       `json:"displayId"`
	Resources []string                     `json:"resources"`
	Actions   map[string]queue.Eligibility `json:"actions"`
}
type queueCursor struct {
	Fingerprint string `json:"fingerprint"`
	Offset      int    `json:"offset"`
}

func completedRefreshObserved(previous, current time.Time, coverage queue.Coverage, fresh bool) bool {
	return coverage.State == queue.CoverageComplete && ((!current.IsZero() && current.After(previous)) || fresh)
}

func queueQueryAction(s *boundary) func(context.Context, *urfavecli.Command) error {
	return queueQueryActionWithLoader(s, queue.NewLoader)
}

func queueQueryActionWithLoader(s *boundary, newLoader func(*queue.Registry) *queue.Loader) func(context.Context, *urfavecli.Command) error {
	return queueQueryActionWithRegistry(s, queue.NewRegistry, newLoader)
}

func queueQueryActionWithRegistry(s *boundary, newRegistry func() *queue.Registry, newLoader func(*queue.Registry) *queue.Loader) func(context.Context, *urfavecli.Command) error {
	return queueQueryActionWithSelection(s, newRegistry, newLoader, nil)
}

// The selector runs over the same unpaginated, overlaid snapshot as query.
// It runs before any query cursor or page limit can hide part of the scope.
type queueSnapshotSelector func(context.Context, *urfavecli.Command, config.QueueConfig, *config.QueueView, *queue.Registry, []queue.Source, *authorityContext, queue.ClaimAuthority, []queue.Item, []queue.Item, []queueSourceJSON, bool) error

func queueQueryActionWithSelection(s *boundary, newRegistry func() *queue.Registry, newLoader func(*queue.Registry) *queue.Loader, selector queueSnapshotSelector) func(context.Context, *urfavecli.Command) error {
	return func(ctx context.Context, cmd *urfavecli.Command) error {
		if cmd.String("view") == "" {
			return s.handle(cmd, reason.Invalid("--view is required for queue query"))
		}
		cfg, err := config.LoadQueue(nil)
		if err != nil {
			return s.handle(cmd, err)
		}
		var view *config.QueueView
		for i := range cfg.Views {
			if cfg.Views[i].Name == cmd.String("view") {
				view = &cfg.Views[i]
				break
			}
		}
		if view == nil {
			return s.handle(cmd, reason.Invalid("unknown queue view"))
		}
		registry := newRegistry()
		sources := make([]queue.Source, 0, len(view.Sources))
		resolveErrors := make(map[string]string)
		for _, configured := range cfg.Sources {
			included := false
			for _, id := range view.Sources {
				if id == configured.ID {
					included = true
					break
				}
			}
			if !included {
				continue
			}
			adapter, ok := registry.Get(configured.Adapter)
			if !ok {
				return s.handle(cmd, reason.New(reason.ReasonConfigInvalid, "queue adapter unavailable"))
			}
			opts := map[string]string{"id": configured.ID, "checkout": configured.Checkout, "host": configured.Host, "repository": configured.Repository, "account": configured.Account, "allowGitNetwork": fmt.Sprint(configured.AllowGitNetwork)}
			source, e := adapter.Resolve(ctx, opts)
			if e != nil {
				resolveErrors[configured.ID] = "source-resolve-failed"
				continue
			}
			source.ID, source.Adapter = configured.ID, configured.Adapter
			sources = append(sources, source)
		}
		loader := newLoader(registry)
		cacheDir, cacheErr := queueindex.CacheDir(os.Getenv, "")
		if cacheErr != nil {
			return s.handle(cmd, cacheErr)
		}
		index, cacheErr := queueindex.Open(ctx, cacheDir)
		if cacheErr != nil {
			return s.handle(cmd, cacheErr)
		}
		defer index.Close()
		loader.GitHubSync = queueindex.GitHubSyncStore{Index: index, Registry: registry}
		maxAge := time.Duration(0)
		if cmd.IsSet("max-age") {
			maxAge = cmd.Duration("max-age")
			if maxAge < 0 {
				return s.handle(cmd, reason.Invalid("--max-age must not be negative"))
			}
		}
		partitions := make(map[string]queueindex.Partition)
		observationTimes := make(map[string]time.Time)
		servedFromIndex := make(map[string]bool)
		cached := queue.Snapshot{Items: map[string]queue.Item{}, Sources: map[string]queue.Coverage{}}
		refreshSources := make([]queue.Source, 0, len(sources))
		releases := make([]func(), 0, len(sources))
		defer func() {
			for _, release := range releases {
				release()
			}
		}()
		// All processes acquire partition locks in the same order, regardless of view order.
		lockOrder := append([]queue.Source(nil), sources...)
		sort.Slice(lockOrder, func(i, j int) bool { return lockOrder[i].ID < lockOrder[j].ID })
		for _, source := range lockOrder {
			adapter, _ := registry.Get(source.Adapter)
			partition, cacheable := queueindex.ForSource(adapter, source)
			if !cacheable {
				refreshSources = append(refreshSources, source)
				continue
			}
			partitions[source.ID] = partition
			candidate, observed, fresh, readErr := index.Read(ctx, partition, maxAge)
			if readErr != nil {
				return s.handle(cmd, readErr)
			}
			observationTimes[source.ID] = observed
			wantCache := cmd.IsSet("max-age") && fresh
			initialObservation := observed
			if !wantCache {
				release, acquired, lockErr := index.TryRefreshLock(partition)
				if lockErr != nil {
					return s.handle(cmd, lockErr)
				}
				if !acquired {
					release, lockErr = index.WaitRefreshLock(ctx, partition)
					if lockErr != nil {
						return s.handle(cmd, lockErr)
					}
					candidate, observed, fresh, readErr = index.Read(ctx, partition, maxAge)
					if readErr != nil {
						release()
						return s.handle(cmd, readErr)
					}
					observationTimes[source.ID] = observed
					completedWhileWaiting := completedRefreshObserved(initialObservation, observed, candidate.Sources[source.ID], fresh)
					wantCache = (cmd.IsSet("max-age") && fresh || completedWhileWaiting) && !(selector != nil && cmd.Bool("claim"))
				}
				if wantCache {
					release()
				} else {
					releases = append(releases, release)
					refreshSources = append(refreshSources, source)
				}
			}
			if len(candidate.Sources) > 0 {
				cached.Items = mergeItems(cached.Items, candidate.Items)
				cached.Sources[source.ID] = candidate.Sources[source.ID]
			}
			if wantCache {
				servedFromIndex[source.ID] = true
			}
		}
		loader.Store.SeedSnapshot(cached)
		updates := loader.Refresh(ctx, refreshSources)
		for range updates {
		}
		snapshot := loader.Store.Current()
		for _, source := range refreshSources {
			if partition, ok := partitions[source.ID]; ok {
				complete := snapshot.Sources[source.ID].State == queue.CoverageComplete
				if writeErr := replaceQueueIndexSnapshot(ctx, index, partition, source.ID, snapshot); writeErr != nil {
					return s.handle(cmd, writeErr)
				}
				if complete {
					observationTimes[source.ID] = time.Now()
				}
			}
		}
		viewFilters := queue.Filters{}
		switch strings.ToLower(view.Filter.Readiness) {
		case "ready": /* readiness post-filter */
		}
		items := queue.EvaluateView(snapshot.Items, queue.View{SourceOrder: view.Sources, Filters: viewFilters})
		selected, auth, e := queueAuthorityForViewMode(ctx, cmd, view.Authority, true, selector != nil && cmd.Bool("claim"))
		if e != nil {
			return s.handle(cmd, e)
		}
		defer selected.Close()
		identities, e := config.LoadQueueIdentities(nil)
		if e != nil {
			return s.handle(cmd, e)
		}
		claimSources := queue.GuardClaimSources(ctx, queue.ClaimSources(cfg, sources), registry, auth, identities, snapshot)
		items = queue.OverlayClaims(ctx, items, claimSources, auth, config.UserProfilePaths(nil), nil)
		cursorItems := append([]queue.Item(nil), items...)
		readiness := strings.ToLower(view.Filter.Readiness)
		claim := strings.ToLower(view.Filter.Claim)
		assigned := map[string]bool{}
		for _, a := range view.Filter.Assigned {
			assigned[strings.ToLower(a)] = true
		}
		filtered := items[:0]
		for _, item := range items {
			if readiness == "ready" && item.Readiness.Status != queue.Ready || readiness == "blocked" && item.Readiness.Status != queue.Blocked {
				continue
			}
			if claim == "free" && !(item.Claim.Known && !item.Claim.Active) || claim == "held" && !item.Claim.Active {
				continue
			}
			if len(assigned) > 0 {
				ok := false
				for _, owner := range item.AssignedTo {
					if assigned[strings.ToLower(owner)] || assigned["me"] && isQueueMe(cfg, item, owner) {
						ok = true
					}
				}
				if assigned["nobody"] && len(item.AssignedTo) == 0 {
					ok = true
				}
				if !ok {
					// A deliberate source-qualified next selector overrides only
					// advisory assignment, not readiness or claim checks.
					selected := false
					if selector != nil {
						for _, ref := range cmd.StringSlice("item") {
							selected = selected || ref == item.Ref.String()
						}
					}
					if !selected {
						continue
					}
				}
			}
			filtered = append(filtered, item)
		}
		items = filtered
		actions := []queue.Action{queue.ActionStart, queue.ActionClaim, queue.ActionLaunch, queue.ActionResume, queue.ActionReportBlocked, queue.ActionRecordProgress, queue.ActionComplete}
		// Bind pagination to the entire snapshot, including items excluded by the view filter.
		cursorCoverage := make(map[string]queue.Coverage, len(snapshot.Sources)+len(resolveErrors))
		for id, coverage := range snapshot.Sources {
			cursorCoverage[id] = coverage
		}
		for id, failure := range resolveErrors {
			cursorCoverage[id] = queue.Coverage{State: queue.CoverageUnknown, TotalAccuracy: queue.TotalUnknown, Reason: failure}
		}
		generations := make(map[string]string, len(sources))
		for _, source := range sources {
			if adapter, ok := registry.Get(source.Adapter); ok {
				if observed, ok := adapter.(interface{ ConfigurationGeneration(queue.Source) string }); ok {
					generations[source.ID] = observed.ConfigurationGeneration(source)
				}
			}
		}
		fingerprint := queueQueryFingerprint(view, configuredQueueSources(cfg.Sources, view.Sources), cfg.Me, generations, queueAuthorityJSON{Profile: auth.Profile, ID: auth.ID, Scope: scopeLabel(auth.Remote)}, cursorCoverage, cursorItems)
		cursor := queueCursor{Fingerprint: fingerprint}
		if encoded := cmd.String("cursor"); encoded != "" {
			raw, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
			if decodeErr != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Fingerprint != fingerprint || cursor.Offset < 0 || cursor.Offset > len(items) {
				return s.handle(cmd, reason.New(reason.ReasonCursorInvalid, "cursor mismatch or stale generation").With("cursor", "invalid"))
			}
		}
		limit := cmd.Int("limit")
		if !cmd.IsSet("limit") {
			limit = 50
		}
		if limit < 1 || limit > 1000 {
			return s.handle(cmd, reason.Invalid("limit must be between 1 and 1000"))
		}
		end := cursor.Offset + limit
		if end > len(items) {
			end = len(items)
		}
		page := make([]queueQueryItem, 0, end-cursor.Offset)
		for _, item := range items[cursor.Offset:end] {
			available := make(map[string]queue.Eligibility, len(actions))
			for action, eligibility := range queue.ClaimActions(item) {
				available[string(action)] = eligibility
			}
			page = append(page, queueQueryItem{Item: item, DisplayID: item.Ref.ItemID, Resources: item.Resources, Actions: available})
		}
		incomplete := false
		sourceRows := make([]queueSourceJSON, 0, len(view.Sources))
		for _, id := range view.Sources {
			coverage, ok := snapshot.Sources[id]
			if failure := resolveErrors[id]; failure != "" {
				coverage = queue.Coverage{State: queue.CoverageUnknown, TotalAccuracy: queue.TotalUnknown, Reason: failure}
			} else if !ok {
				coverage = queue.Coverage{State: queue.CoverageUnknown, TotalAccuracy: queue.TotalUnknown, Reason: "not-observed"}
			}
			freshness := "fresh"
			if coverage.State != queue.CoverageComplete {
				freshness = "unknown"
				incomplete = true
			}
			for _, item := range cursorItems {
				if item.Ref.SourceID == id && !item.Fresh {
					freshness = "stale"
					incomplete = true
					break
				}
			}
			diagnostics := queueSourceDiagnostics(registry, sourceForID(sources, id))
			if failure := resolveErrors[id]; failure != "" {
				diagnostics = []string{failure}
			}
			if source := claimSources[id]; source.BlockReason != "" {
				diagnostics = append(diagnostics, source.BlockReason+": "+source.BlockDetail)
			}
			sourceRows = append(sourceRows, queueSourceJSON{ID: id, Coverage: coverage, Freshness: freshness, ObservedAt: observationTimes[id], ServedFromIndex: servedFromIndex[id], Diagnostics: diagnostics})
		}
		for _, item := range cursorItems {
			if !item.DependenciesKnown || item.Closure != queue.CoverageComplete {
				incomplete = true
			}
		}
		if selector != nil {
			return selector(ctx, cmd, cfg, view, registry, sources, selected, auth, cursorItems, items, sourceRows, incomplete)
		}
		if incomplete && cmd.Bool("require-complete") {
			fields := queueQueryEnvelope{SchemaVersion: 1, View: view.Name, Authority: queueAuthorityJSON{Profile: auth.Profile, ID: auth.ID, Scope: scopeLabel(auth.Remote)}, Sources: sourceRows, Items: page, Incomplete: true}
			return s.handle(cmd, reason.New(reason.ReasonQueueIncomplete, "queue query is incomplete").With("result", "incomplete").With("query", normalizedQueueEnvelope(fields)))
		}
		envelope := queueQueryEnvelope{SchemaVersion: 1, View: view.Name, Authority: queueAuthorityJSON{Profile: auth.Profile, ID: auth.ID, Scope: scopeLabel(auth.Remote)}, Sources: sourceRows, Items: page, Incomplete: incomplete}
		if end < len(items) {
			next := queueCursor{Fingerprint: fingerprint, Offset: end}
			raw, _ := json.Marshal(next)
			envelope.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		if cmd.Bool("json") {
			return output.WriteSuccess(s.writer, "queue-query", map[string]any{"query": normalizedQueueEnvelope(envelope)})
		}
		if _, err := fmt.Fprintln(s.writer, "ID\tSTATE\tREADY\tCLAIM\tTITLE"); err != nil {
			return err
		}
		for _, item := range page {
			if _, err := fmt.Fprintf(s.writer, "%s\t%s\t%s\t%s\t%s\n", safeQueueCell(item.Ref.String()), safeQueueCell(string(item.State)), safeQueueCell(string(item.Readiness.Status)), safeQueueCell(item.Claim.State), safeQueueCell(item.Title)); err != nil {
				return err
			}
		}
		return nil
	}
}
func mergeItems(dst, src map[string]queue.Item) map[string]queue.Item {
	for key, item := range src {
		dst[key] = item
	}
	return dst
}

func normalizedQueueEnvelope(envelope queueQueryEnvelope) map[string]any {
	data, _ := json.Marshal(envelope)
	var projected map[string]any
	_ = json.Unmarshal(data, &projected)
	items, _ := projected["items"].([]any)
	for index, value := range items {
		item, _ := value.(map[string]any)
		if item == nil || index >= len(envelope.Items) {
			continue
		}
		resources := make([]any, len(envelope.Items[index].Resources))
		for i, resource := range envelope.Items[index].Resources {
			if key := envelope.Items[index].KeyInputs; key != nil && (key.Provider == "generic" || key.Provider == "linear") {
				resources[i] = output.PublicDigest(resource)
			} else {
				resources[i] = resource
			}
		}
		item["resources"] = resources
	}
	return projected
}
func queueQueryFingerprint(view *config.QueueView, bindings []config.QueueSource, me map[string]yaml.Node, generations map[string]string, authority queueAuthorityJSON, coverage map[string]queue.Coverage, items []queue.Item) string {
	stableCoverage := make(map[string]queue.Coverage, len(coverage))
	for id, value := range coverage {
		if value.Reason == "cached-index" {
			value.Reason = ""
		}
		// The index records coverage state, but not the refresh's auxiliary metadata.
		value.Scope = ""
		value.Cursor = ""
		value.Total = 0
		value.TotalAccuracy = queue.TotalUnknown
		stableCoverage[id] = value
	}
	stableItems := append([]queue.Item(nil), items...)
	for i := range stableItems {
		stableItems[i].Observation.ObservedAt = time.Time{}
		stableItems[i].Claim.ObservedAt = time.Time{}
	}
	sort.Slice(stableItems, func(i, j int) bool { return stableItems[i].Ref.Less(stableItems[j].Ref) })
	fingerprintData, _ := json.Marshal(struct {
		View        string
		Filters     config.QueueFilter
		SourceIDs   []string
		Bindings    []config.QueueSource
		Me          map[string]yaml.Node
		Generations map[string]string
		Authority   queueAuthorityJSON
		Coverage    map[string]queue.Coverage
		Items       []queue.Item
	}{view.Name, view.Filter, view.Sources, bindings, me, generations, authority, stableCoverage, stableItems})
	digest := sha256.Sum256(fingerprintData)
	return hex.EncodeToString(digest[:])
}
func configuredQueueSources(configured []config.QueueSource, ids []string) []config.QueueSource {
	byID := make(map[string]config.QueueSource, len(configured))
	for _, source := range configured {
		byID[source.ID] = source
	}
	result := make([]config.QueueSource, 0, len(ids))
	for _, id := range ids {
		if source, ok := byID[id]; ok {
			result = append(result, source)
		}
	}
	return result
}
func safeQueueCell(value string) string {
	value = output.RedactString(value)
	var result strings.Builder
	for _, r := range value {
		if r >= 0x20 && r != 0x7f && !(r >= 0x80 && r <= 0x9f) {
			result.WriteRune(r)
		}
	}
	return result.String()
}
func sourceForID(sources []queue.Source, id string) queue.Source {
	for _, source := range sources {
		if source.ID == id {
			return source
		}
	}
	return queue.Source{}
}
func queueSourceDiagnostics(registry *queue.Registry, source queue.Source) []string {
	adapter, ok := registry.Get(source.Adapter)
	if !ok {
		return []string{"adapter-unavailable"}
	}
	backlog, ok := adapter.(*queue.BacklogAdapter)
	if !ok {
		return nil
	}
	d := backlog.Diagnostics(source)
	values := []string{}
	if d.NetworkEffects {
		values = append(values, "network-effects")
	}
	if d.CommitEffects {
		values = append(values, "commit-effects")
	}
	if d.HookEffects {
		values = append(values, "hook-effects")
	}
	if d.Dirty {
		values = append(values, "checkout-dirty")
	}
	if len(d.DuplicateIDs) > 0 {
		values = append(values, "duplicate-item-ids")
	}
	return values
}
func scopeLabel(remote bool) string {
	if remote {
		return "remote"
	}
	return "local"
}
func isQueueMe(cfg config.QueueConfig, item queue.Item, owner string) bool {
	for _, source := range cfg.Sources {
		if source.ID != item.Ref.SourceID {
			continue
		}
		if source.Adapter == "github" {
			var account string
			if value := cfg.Me[source.Host]; value.Decode(&account) == nil {
				return strings.EqualFold(account, owner)
			}
		}
		if source.Adapter == "backlog-md" {
			var names []string
			if value := cfg.Me["backlog-md"]; value.Decode(&names) == nil {
				for _, name := range names {
					if strings.EqualFold(name, owner) {
						return true
					}
				}
			}
		}
	}
	return false
}
