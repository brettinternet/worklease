package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/reason"
	urfave "github.com/urfave/cli/v3"
)

type queueClaimFixtureAdapter struct {
	source queue.Source
	items  map[string]queue.Item
	reads  int
}

func (a *queueClaimFixtureAdapter) Resolve(_ context.Context, _ map[string]string) (queue.Source, error) {
	return a.source, nil
}
func (a *queueClaimFixtureAdapter) Capabilities(context.Context, queue.Source, string, *queue.Ref) (queue.CapabilitySet, error) {
	return queue.CapabilitySet{}, nil
}
func (a *queueClaimFixtureAdapter) List(context.Context, queue.Source, queue.Query, string) (queue.SummaryPage, error) {
	return queue.SummaryPage{Coverage: queue.Coverage{State: queue.CoverageComplete}}, nil
}
func (a *queueClaimFixtureAdapter) ReadItems(_ context.Context, _ queue.Source, refs []queue.Ref, _ []string, _ int) []queue.ItemOutcome {
	a.reads++
	out := make([]queue.ItemOutcome, 0, len(refs))
	for _, ref := range refs {
		item, ok := a.items[ref.Key()]
		if !ok {
			out = append(out, queue.ItemOutcome{Ref: ref, Kind: "missing"})
			continue
		}
		item.ReadOutcome = "found"
		item.ReadPermission = queue.Allowed
		item.Fresh = true
		out = append(out, queue.ItemOutcome{Ref: ref, Kind: "found", Item: &item})
	}
	return out
}
func (a *queueClaimFixtureAdapter) ReadDependencies(_ context.Context, _ queue.Source, ref queue.Ref, cursor string, _ int) (queue.DependencyPage, error) {
	if cursor != "" {
		return queue.DependencyPage{}, nil
	}
	item := a.items[ref.Key()]
	return queue.DependencyPage{Edges: append([]queue.Relationship(nil), item.Relationships...), Completeness: queue.CoverageComplete, Observation: queue.Observation{ObservedAt: time.Now().UTC()}}, nil
}

func queueClaimItem(source, id string) queue.Item {
	return queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: source, ItemID: id}, CanonicalID: source + ":" + id, Title: "claim target", State: queue.StateOpen, Fresh: true}, TerminalKnown: true, ReadPermission: queue.Allowed}
}

func newLocalQueueClaimController(t *testing.T, ttl time.Duration, items ...queue.Item) (*queueClaimController, *authorityContext, *queueClaimFixtureAdapter) {
	t.Helper()
	configRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("WORKLEASE_HOME", home)
	t.Setenv("WORKLEASE_PROFILE", "")
	registry := queue.NewRegistry()
	source := queue.Source{ID: "tasks", Adapter: "fixture", Locator: "/fixture"}
	adapter := &queueClaimFixtureAdapter{source: source, items: map[string]queue.Item{}}
	for _, item := range items {
		adapter.items[item.Ref.Key()] = item
	}
	if err := registry.Register("fixture", adapter); err != nil {
		t.Fatal(err)
	}
	backend, err := authorityForSelection(context.Background(), &urfave.Command{}, true, config.ProfileSelection{Name: config.LocalProfileName})
	if err != nil {
		t.Fatalf("local queue authority: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	backend.Config.TTL = ttl
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(config.QueueIdentityPath(os.Getenv))); err != nil {
		t.Fatalf("prepare local private queue directory: %v", err)
	}
	identity := config.QueueIdentity{Adapter: source.Adapter, Locator: source.Locator, Policy: "generic", Source: "portable", AuthorityID: backend.AuthorityID()}
	if err := config.SaveQueueIdentities(os.Getenv, config.QueueIdentities{Version: 1, Sources: map[string]config.QueueIdentity{source.ID: identity}}); err != nil {
		t.Fatalf("save local queue identity: %v", err)
	}
	claimSource := queue.ClaimSource{Source: source, Policy: identity.Policy, ClaimSource: identity.Source}
	selected := queue.ClaimAuthority{API: backend.API, LiveAPI: backend.API, ID: backend.AuthorityID(), Profile: config.LocalProfileName, LocalDefaultAuthorityID: backend.AuthorityID()}
	controller := &queueClaimController{backend: backend, registry: registry, sources: map[string]queue.Source{source.ID: source}, claimSources: map[string]queue.ClaimSource{source.ID: claimSource}, queueSession: strings.Repeat("1", 32), paths: config.UserProfilePaths(os.Getenv), current: func() (queue.ClaimAuthority, uint64) { return selected, 1 }, blocked: func() bool { return false }, profileName: config.LocalProfileName, home: backend.Config.Home}
	return controller, backend, adapter
}

func newRemoteQueueClaimController(t *testing.T, ttl time.Duration, items ...queue.Item) (*queueClaimController, *authorityContext, *queueClaimFixtureAdapter) {
	t.Helper()
	profileName, _, _ := remoteCLIFixture(t)
	home := t.TempDir()
	t.Setenv("WORKLEASE_HOME", home)
	t.Setenv("WORKLEASE_TTL", ttl.String())
	t.Setenv("WORKLEASE_PROFILE", profileName)
	registry := queue.NewRegistry()
	source := queue.Source{ID: "tasks", Adapter: "fixture", Locator: "/fixture"}
	adapter := &queueClaimFixtureAdapter{source: source, items: map[string]queue.Item{}}
	for _, item := range items {
		adapter.items[item.Ref.Key()] = item
	}
	if err := registry.Register("fixture", adapter); err != nil {
		t.Fatal(err)
	}
	backend, selected, err := queueAuthorityForClaim(context.Background(), &urfave.Command{}, profileName)
	if err != nil {
		t.Fatalf("remote queue authority: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if selected.AdmittedPrefixes == nil {
		t.Fatal("remote claim authority did not load admission metadata")
	}
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(config.QueueIdentityPath(os.Getenv))); err != nil {
		t.Fatalf("prepare remote private queue directory: %v", err)
	}
	identity := config.QueueIdentity{Adapter: source.Adapter, Locator: source.Locator, Policy: "generic", Source: "portable", AuthorityID: backend.AuthorityID()}
	if err := config.SaveQueueIdentities(os.Getenv, config.QueueIdentities{Version: 1, Sources: map[string]config.QueueIdentity{source.ID: identity}}); err != nil {
		t.Fatalf("save remote queue identity: %v", err)
	}
	claimSource := queue.ClaimSource{Source: source, Policy: identity.Policy, ClaimSource: identity.Source}
	controller := &queueClaimController{backend: backend, registry: registry, sources: map[string]queue.Source{source.ID: source}, claimSources: map[string]queue.ClaimSource{source.ID: claimSource}, queueSession: strings.Repeat("2", 32), paths: config.UserProfilePaths(os.Getenv), current: func() (queue.ClaimAuthority, uint64) { return selected, 1 }, blocked: func() bool { return false }, profile: backend.Profile, profileName: profileName, home: backend.Config.Home}
	return controller, backend, adapter
}

type unresolvedPagedClaimAdapter struct{ *queueClaimFixtureAdapter }

func (a *unresolvedPagedClaimAdapter) ReadDependencies(_ context.Context, source queue.Source, ref queue.Ref, cursor string, _ int) (queue.DependencyPage, error) {
	if cursor == "" {
		return queue.DependencyPage{Completeness: queue.CoveragePartial, NextCursor: "more"}, nil
	}
	return queue.DependencyPage{Completeness: queue.CoverageComplete}, nil
}

type changingProjectClaimAdapter struct {
	*queueClaimFixtureAdapter
	blocked        bool
	pendingBlocked bool
	refreshes      int
	fail           bool
}

func (*changingProjectClaimAdapter) ProjectStatusBound(queue.Source) bool { return true }
func (a *changingProjectClaimAdapter) RefreshProjectStatus(context.Context, queue.Source) error {
	a.refreshes++
	if a.fail {
		return fmt.Errorf("project access lost")
	}
	a.blocked = a.pendingBlocked
	return nil
}
func (a *changingProjectClaimAdapter) ReadItems(ctx context.Context, source queue.Source, refs []queue.Ref, fields []string, limit int) []queue.ItemOutcome {
	outcomes := a.queueClaimFixtureAdapter.ReadItems(ctx, source, refs, fields, limit)
	for i := range outcomes {
		if outcomes[i].Item != nil {
			outcomes[i].Item.ProjectStatusBound = true
			outcomes[i].Item.ProjectStatusKnown = true
			outcomes[i].Item.ProjectStatusState = "open"
			if a.blocked {
				outcomes[i].Item.ProjectStatusState = "blocked"
			}
		}
	}
	return outcomes
}

func TestQueueClaimRefreshesBoundProjectBeforeAction(t *testing.T) {
	t.Parallel()
	item := queueClaimItem("issues", "1")
	source := queue.Source{ID: "issues", Adapter: "project-fixture", Locator: "org/repo"}
	adapter := &changingProjectClaimAdapter{queueClaimFixtureAdapter: &queueClaimFixtureAdapter{source: source, items: map[string]queue.Item{item.Ref.Key(): item}}, pendingBlocked: true}
	registry := queue.NewRegistry()
	if err := registry.Register(source.Adapter, adapter); err != nil {
		t.Fatal(err)
	}
	fresh, err := refreshQueueActionClosure(context.Background(), registry, map[string]queue.Source{source.ID: source}, item)
	if err != nil || adapter.refreshes != 1 || fresh.Readiness.Status != queue.Blocked {
		t.Fatalf("claim used stale project status: item=%+v refreshes=%d err=%v", fresh.Readiness, adapter.refreshes, err)
	}
	adapter.fail = true
	if _, err := refreshQueueActionClosure(context.Background(), registry, map[string]queue.Source{source.ID: source}, item); err == nil {
		t.Fatal("claim proceeded after project access loss")
	}
}

func TestQueueClaimRejectsNonLinearPartialDependencyPage(t *testing.T) {
	t.Parallel()
	item := queueClaimItem("tasks", "1")
	source := queue.Source{ID: "tasks", Adapter: "external-stub", Locator: "portable"}
	adapter := &unresolvedPagedClaimAdapter{&queueClaimFixtureAdapter{source: source, items: map[string]queue.Item{item.Ref.Key(): item}}}
	registry := queue.NewRegistry()
	if err := registry.Register(source.Adapter, adapter); err != nil {
		t.Fatal(err)
	}
	if _, err := refreshQueueActionClosure(context.Background(), registry, map[string]queue.Source{source.ID: source}, item); err == nil {
		t.Fatal("partial non-Linear dependency page with a cursor was treated as complete")
	}
}

func TestQueueClaimAndCLIContendOnTheSameLocalResourceBothDirections(t *testing.T) {
	ctx := context.Background()
	item := queueClaimItem("tasks", "1")
	controller, backend, adapter := newLocalQueueClaimController(t, 30*time.Second, item)
	previewMessage := controller.Preview(ctx, item)().(queueui.ClaimPreviewMsg)
	if previewMessage.Err != nil || previewMessage.Preview == nil {
		t.Fatalf("queue preview: %+v", previewMessage)
	}
	result := controller.AcquireClaim(ctx, item, *previewMessage.Preview)().(queueui.ClaimResultMsg)
	if result.Err != nil || !result.Claim.Active || result.Claim.SessionID != controller.queueSession {
		t.Fatalf("queue acquire: %+v", result)
	}
	path, err := queueClaimHandlePath(backend.Config.Home, controller.queueSession, config.LocalProfileName, adapter.source, item.Ref)
	if err != nil {
		t.Fatal(err)
	}
	h, err := handle.Read(path)
	if err != nil || h.SessionID != controller.queueSession || h.State != "ready" || h.Token == "" || h.RestoreID != "" {
		t.Fatalf("private queue handle not retained: %+v %v", h, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("handle mode = %v %v", info, err)
	}
	_, err = runAcquireForQueueTest(backend.Config.Home, "generic", "portable", "1", false)
	if reason.As(err) == nil || reason.As(err).Reason != reason.ReasonAlreadyClaimed {
		t.Fatalf("CLI acquire did not contend with queue claim: %v", err)
	}

	second := queueClaimItem("tasks", "2")
	adapter.items[second.Ref.Key()] = second
	if _, err := runAcquireForQueueTest(backend.Config.Home, "generic", "portable", "2", false); err != nil {
		t.Fatalf("CLI first acquire: %v", err)
	}
	if _, err := controller.prepare(ctx, second); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonAlreadyClaimed {
		t.Fatalf("queue did not contend with CLI claim: %v", err)
	}
}

func TestQueueClaimAndCLIContendOnTheSameRemoteResourceBothDirections(t *testing.T) {
	ctx := context.Background()
	item := queueClaimItem("tasks", "1")
	controller, backend, adapter := newRemoteQueueClaimController(t, 30*time.Second, item)
	previewMessage := controller.Preview(ctx, item)().(queueui.ClaimPreviewMsg)
	if previewMessage.Err != nil || previewMessage.Preview == nil {
		t.Fatalf("queue preview: %+v", previewMessage)
	}
	result := controller.AcquireClaim(ctx, item, *previewMessage.Preview)().(queueui.ClaimResultMsg)
	if result.Err != nil || !result.Claim.Active || result.Claim.SessionID != controller.queueSession {
		t.Fatalf("queue acquire: %+v", result)
	}
	path, err := queueClaimHandlePath(backend.Config.Home, controller.queueSession, controller.profileName, adapter.source, item.Ref)
	if err != nil {
		t.Fatal(err)
	}
	h, err := handle.Read(path)
	if err != nil || h.SessionID != controller.queueSession || h.RestoreID != controller.profile.RestoreID || h.State != "ready" {
		t.Fatalf("remote queue handle identity was not retained: %+v %v", h, err)
	}
	if _, err := runAcquireForQueueTest(backend.Config.Home, "generic", "portable", "1", true); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonAlreadyClaimed {
		t.Fatalf("CLI acquire did not contend with remote queue claim: %v", err)
	}

	second := queueClaimItem("tasks", "2")
	adapter.items[second.Ref.Key()] = second
	if _, err := runAcquireForQueueTest(backend.Config.Home, "generic", "portable", "2", true); err != nil {
		t.Fatalf("remote CLI first acquire: %v", err)
	}
	if _, err := controller.prepare(ctx, second); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonAlreadyClaimed {
		t.Fatalf("remote queue did not contend with CLI claim: %v", err)
	}
}

func TestQueueClaimReopensPrerequisiteBeforeConfirmation(t *testing.T) {
	ctx := context.Background()
	root := queueClaimItem("tasks", "1")
	prerequisite := queueClaimItem("tasks", "2")
	prerequisite.Terminal = true
	root.Relationships = []queue.Relationship{{Type: queue.HardPrerequisite, Direction: queue.DependentToPrerequisite, From: root.Ref, To: prerequisite.Ref, Condition: "terminal", Fresh: true, Support: queue.Supported}}
	controller, backend, adapter := newLocalQueueClaimController(t, time.Minute, root, prerequisite)
	preview := controller.Preview(ctx, root)().(queueui.ClaimPreviewMsg)
	if preview.Err != nil || preview.Preview == nil {
		t.Fatalf("initial eligible preview: %+v", preview)
	}
	prerequisite.Terminal = false
	adapter.items[prerequisite.Ref.Key()] = prerequisite
	result := controller.AcquireClaim(ctx, root, *preview.Preview)().(queueui.ClaimResultMsg)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "hard-condition-unsatisfied") {
		t.Fatalf("reopened prerequisite did not stop acquisition: %v", result.Err)
	}
	status, err := backend.API.Status(ctx, lease.Selector{AuthorityID: backend.AuthorityID(), Resources: preview.Preview.Resources})
	if err != nil || len(status.Resources) != 1 || status.Resources[0].State != "free" {
		t.Fatalf("failed gate left a claim held: %+v %v", status, err)
	}

	// A reopen after confirmation's own preparation is caught by the final gate.
	prerequisite.Terminal = true
	adapter.items[prerequisite.Ref.Key()] = prerequisite
	plan, err := controller.prepare(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	prerequisite.Terminal = false
	adapter.items[prerequisite.Ref.Key()] = prerequisite
	if _, err := controller.acquire(ctx, plan); err == nil || !strings.Contains(err.Error(), "hard-condition-unsatisfied") {
		t.Fatalf("reopen before acquisition did not stop it: %v", err)
	}
	status, err = backend.API.Status(ctx, lease.Selector{AuthorityID: backend.AuthorityID(), Resources: preview.Preview.Resources})
	if err != nil || len(status.Resources) != 1 || status.Resources[0].State != "free" {
		t.Fatalf("final gate left a claim held: %+v %v", status, err)
	}
}

func TestQueueClaimReportsRemoteAdmissionRejectionAndLowerActualTTL(t *testing.T) {
	ctx := context.Background()
	item := queueClaimItem("tasks", "1")
	controller, backend, _ := newRemoteQueueClaimController(t, 2*time.Minute, item)
	preview := controller.Preview(ctx, item)().(queueui.ClaimPreviewMsg)
	if preview.Err != nil || preview.Preview == nil || preview.Preview.TTL != 2*time.Minute || preview.Preview.Hold != time.Hour || !strings.Contains(preview.Preview.CoordinationLimits, "not advertised") {
		t.Fatalf("remote limits were not previewed honestly: %+v", preview)
	}
	rejected := controller.AcquireClaim(ctx, item, *preview.Preview)().(queueui.ClaimResultMsg)
	classified := reason.As(rejected.Err)
	if classified == nil || classified.Reason != reason.ReasonInvalidArgument || classified.Details["commitState"] != "not-committed" {
		t.Fatalf("server TTL rejection not surfaced as definitive no-commit: %#v", classified)
	}
	status, err := backend.API.Status(ctx, lease.Selector{AuthorityID: backend.AuthorityID(), Resources: preview.Preview.Resources})
	if err != nil || len(status.Resources) != 1 || status.Resources[0].State != "free" {
		t.Fatalf("rejected request left a claim held: %+v %v", status, err)
	}
	controller.backend.Config.TTL = 30 * time.Second
	acceptedPreview := controller.Preview(ctx, item)().(queueui.ClaimPreviewMsg)
	if acceptedPreview.Err != nil || acceptedPreview.Preview == nil {
		t.Fatalf("lower TTL preview: %+v", acceptedPreview)
	}
	accepted := controller.AcquireClaim(ctx, item, *acceptedPreview.Preview)().(queueui.ClaimResultMsg)
	if accepted.Err != nil || accepted.GrantedTTL <= 0 || accepted.GrantedTTL > time.Minute || accepted.Claim.ExpiresAt.IsZero() {
		t.Fatalf("actual remote grant TTL/expiry: %+v", accepted)
	}
	if accepted.GrantedTTL != accepted.Claim.ExpiresAt.Sub(accepted.Claim.AcquiredAt) {
		t.Fatalf("reported TTL differs from authority grant: %+v", accepted)
	}
}

func TestQueueClaimStopsOnChangedRemoteAuthorityIdentityWithActiveHandle(t *testing.T) {
	for _, change := range []string{"restore", "authority"} {
		t.Run(change, func(t *testing.T) {
			item := queueClaimItem("tasks", "1")
			controller, _, adapter := newRemoteQueueClaimController(t, 30*time.Second, item)
			preview := controller.Preview(context.Background(), item)().(queueui.ClaimPreviewMsg)
			if preview.Err != nil || preview.Preview == nil {
				t.Fatalf("queue preview: %+v", preview)
			}
			acquired := controller.AcquireClaim(context.Background(), item, *preview.Preview)().(queueui.ClaimResultMsg)
			if acquired.Err != nil {
				t.Fatalf("queue claim: %v", acquired.Err)
			}
			path, err := queueClaimHandlePath(controller.home, controller.queueSession, controller.profileName, adapter.source, item.Ref)
			if err != nil {
				t.Fatal(err)
			}
			h, err := handle.Read(path)
			if err != nil || h.State != "ready" || h.RestoreID != controller.profile.RestoreID {
				t.Fatalf("active queue handle missing: %+v %v", h, err)
			}
			profiles, _, err := config.LoadProfiles(controller.paths)
			if err != nil {
				t.Fatal(err)
			}
			profile := profiles[controller.profileName]
			if change == "restore" {
				profile.RestoreID = strings.Repeat("f", 32)
			} else {
				profile.AuthorityID = strings.Repeat("e", 32)
			}
			if err := config.SaveProfiles(controller.paths, []config.Profile{profile}, ""); err != nil {
				t.Fatal(err)
			}
			if err := controller.profileIdentityCurrent(); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonAuthorityMismatch {
				t.Fatalf("changed profile identity did not stop actions: %v", err)
			}
			pinned := controller.profile
			controller.profile = &profile
			newAuthority := queue.ClaimAuthority{API: controller.backend.API, ID: profile.AuthorityID, Profile: controller.profileName, Remote: true}
			if err := controller.checkQueueHandles(newAuthority); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonAuthorityMismatch {
				t.Fatalf("existing active handle was silently repinned: %v", err)
			}
			controller.profile = pinned
			still, err := handle.Read(path)
			if err != nil || still.AuthorityID != controller.profile.AuthorityID || still.RestoreID != controller.profile.RestoreID {
				t.Fatalf("identity failure modified the old handle: %+v %v", still, err)
			}
		})
	}
}

func runAcquireForQueueTest(home, provider, source, item string, remote bool) (string, error) {
	var stdout, stderr bytes.Buffer
	args := []string{"worklease", "--home", home}
	if remote {
		args = append(args, "--profile", "team")
	}
	args = append(args, "acquire", "--provider", provider, "--source", source, "--item", item, "--coordination-only")
	err := Run(context.Background(), args, "test", "unknown", "unknown", &stdout, &stderr)
	if stdout.Len() == 0 && stderr.Len() > 0 {
		return stderr.String(), err
	}
	return stdout.String(), err
}

var _ queue.Adapter = (*queueClaimFixtureAdapter)(nil)
