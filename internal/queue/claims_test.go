package queue

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/authority"
	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
)

type statusAuthority struct {
	authority.FakeAuthority
	calls [][]string
	fail  bool
	split bool
}

func (a *statusAuthority) Status(_ context.Context, sel lease.Selector) (lease.Status, error) {
	a.calls = append(a.calls, append([]string(nil), sel.Resources...))
	if a.fail {
		return lease.Status{}, reason.New(reason.ReasonResponseTooLarge, "unavailable")
	}
	if a.split && len(sel.Resources) > 1 {
		return lease.Status{}, reason.New(reason.ReasonResponseTooLarge, "large")
	}
	result := lease.Status{}
	for _, key := range sel.Resources {
		state := lease.ResourceStatus{Resource: key, State: "free"}
		if len(sel.Resources) > 1 && key == sel.Resources[0] {
			state.State = "active"
			state.Claim = &lease.ClaimView{AuthorityID: sel.AuthorityID, AgentID: "worker", SessionID: "session", Active: true, ExpiresAt: time.Now().Add(time.Minute)}
		}
		if len(sel.Resources) > 1 && key == sel.Resources[1] {
			state.State = "expired"
			state.Claim = &lease.ClaimView{AuthorityID: sel.AuthorityID, AgentID: "old", Active: false, ExpiresAt: time.Now().Add(-time.Minute)}
		}
		result.Resources = append(result.Resources, state)
	}
	return result, nil
}
func claimFixtures(count int) ([]Item, map[string]ClaimSource) {
	items := make([]Item, count)
	sources := map[string]ClaimSource{"s": {Source: Source{ID: "s", Adapter: "generic", Locator: "project"}}}
	for i := range items {
		items[i].Ref = Ref{SourceID: "s", ItemID: fmt.Sprintf("%d", i)}
		items[i].Readiness = Readiness{Status: Ready}
	}
	return items, sources
}
func TestClaimOverlayBatchesAndSplitsWithoutList(t *testing.T) {
	items, sources := claimFixtures(64)
	a := &statusAuthority{split: true}
	prefixes := []string{"coordination:"}
	out := OverlayClaims(context.Background(), items, sources, ClaimAuthority{API: a, ID: "authority", Profile: "remote", Remote: true, AdmittedPrefixes: &prefixes}, config.ProfilePaths{}, nil)
	if len(a.calls) < 65 || len(a.calls[0]) != 32 || len(a.calls[1]) != 16 {
		t.Fatalf("unexpected batch schedule: %v", a.calls[:3])
	}
	for i, item := range out {
		if item.Claim.State != "free" || item.Claim.Stale || !item.Claim.Available || item.Claim.ObservedAt.IsZero() {
			t.Fatalf("item %d: %+v", i, item.Claim)
		}
		key, err := resource.Resolve(resource.Input{Provider: "generic", Source: "project", Item: item.Ref.ItemID})
		if err != nil || len(item.Resources) != 1 || item.Resources[0] != key.Resource || item.KeyInputs.Source != "project" {
			t.Fatalf("item %d key: %+v %v", i, item, err)
		}
	}
	if len(a.FakeAuthority.Calls) != 0 {
		t.Fatalf("unexpected API calls: %v", a.FakeAuthority.Calls)
	}
}
func TestClaimOverlayStatesAndOutage(t *testing.T) {
	items, sources := claimFixtures(2)
	items[0].Ref.ItemID = "held"
	items[1].Ref.ItemID = "expired"
	a := &statusAuthority{}
	prefixes := []string{"coordination:"}
	selected := ClaimAuthority{API: a, ID: "authority", Profile: "remote", Remote: true, AdmittedPrefixes: &prefixes}
	out := OverlayClaims(context.Background(), items, sources, selected, config.ProfilePaths{}, nil)
	if out[0].Claim.State != "held" || out[0].Claim.AgentID != "worker" || out[0].Claim.SessionID != "session" || out[0].Claim.ExpiresAt.IsZero() || out[0].Claim.Available {
		t.Fatalf("held: %+v", out[0].Claim)
	}
	if out[1].Claim.State != "expired" || !out[1].Claim.Available {
		t.Fatalf("expired: %+v", out[1].Claim)
	}
	a.fail = true
	out = OverlayClaims(context.Background(), items, sources, selected, config.ProfilePaths{}, nil)
	for _, item := range out {
		if item.Claim.State != "unknown" || !item.Claim.Stale || item.Claim.Available || ClaimActions(item)[ActionStart].Eligible {
			t.Fatalf("outage became free: %+v", item.Claim)
		}
	}
}
func TestConfiguredSourcesPreserveKeyInputsAndNativeClaim(t *testing.T) {
	cfg := config.QueueConfig{Sources: []config.QueueSource{
		{ID: "github", Adapter: "github", Host: "github.com", Repository: "Owner/Repository"},
		{ID: "enterprise", Adapter: "github", Host: "ghe.example.com", Repository: "Team/Project"},
		{ID: "backlog", Adapter: "backlog-md", Checkout: "/checkout", Claims: &config.QueueClaims{Policy: "generic", Source: "portable"}},
	}}
	resolved := []Source{{ID: "github", Adapter: "github", Locator: "Owner/Repository"}, {ID: "enterprise", Adapter: "github", Locator: "Team/Project"}, {ID: "backlog", Adapter: "backlog-md", Locator: "/checkout"}}
	sources := ClaimSources(cfg, resolved)
	for id, expected := range map[string]string{"github": "Owner/Repository", "enterprise": "ghe.example.com/Team/Project", "backlog": "portable"} {
		if sources[id].ClaimSource != expected {
			t.Fatalf("%s key source: %q", id, sources[id].ClaimSource)
		}
	}
	items := []Item{{Summary: Summary{Ref: Ref{SourceID: "github", ItemID: "126"}}}, {Summary: Summary{Ref: Ref{SourceID: "enterprise", ItemID: "126"}}}}
	prefixes := []string{"github:"}
	observed := OverlayClaims(context.Background(), items, sources, ClaimAuthority{API: &statusAuthority{}, ID: "authority", Remote: true, AdmittedPrefixes: &prefixes}, config.ProfilePaths{}, nil)
	for i, resourceKey := range []string{"github:owner%2Frepository#126", "github:ghe.example.com%2Fteam%2Fproject#126"} {
		if len(observed[i].Resources) != 1 || observed[i].Resources[0] != resourceKey || observed[i].NativeClaim != "not-exposed" || observed[i].Claim.NativeState != "not-exposed" {
			t.Fatalf("source key/native state: %+v", observed[i])
		}
	}
}

func TestLargeResourceStatusSplitsOnResponseLimit(t *testing.T) {
	items := make([]Item, 32)
	indexes := make(map[string][]int)
	keys := make([]string, 32)
	for i := range keys {
		keys[i] = fmt.Sprintf("%02d%s", i, strings.Repeat("x", 970))
		indexes[keys[i]] = []int{i}
	}
	a := &statusAuthority{split: true}
	overlayBatch(context.Background(), items, indexes, keys, ClaimAuthority{API: a, ID: "authority"})
	if len(a.calls) != 63 || len(a.calls[0]) != 32 {
		t.Fatalf("expected binary splitting of near-1KiB keys: %d calls", len(a.calls))
	}
	for _, item := range items {
		if item.Claim.State != "free" {
			t.Fatalf("missing status after split: %+v", item.Claim)
		}
	}
}

func TestCheckoutBindingMismatchWithPortableKey(t *testing.T) {
	checkout, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	private := t.TempDir()
	paths := config.ProfilePaths{Profiles: filepath.Join(private, "profiles.yaml"), Bindings: filepath.Join(private, "bindings.yaml")}
	profile := config.Profile{Name: "worker", Endpoint: "http://127.0.0.1:8888", AuthorityID: strings.Repeat("a", 32), AllowInsecureHTTP: true, Credential: config.CredentialDescriptor{Path: filepath.Join(private, "credential")}}
	if err := config.SaveProfiles(paths, []config.Profile{profile}, ""); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveBindings(paths, map[string]string{checkout: "worker"}); err != nil {
		t.Fatal(err)
	}
	items, sources := claimFixtures(1)
	sources["s"] = ClaimSource{Source: Source{ID: "s", Adapter: "backlog-md", Locator: checkout}, Policy: "generic", ClaimSource: "portable"}
	prefixes := []string{"coordination:"}
	a := &statusAuthority{}
	selected := ClaimAuthority{API: a, ID: strings.Repeat("b", 32), Profile: "view", Remote: true, AdmittedPrefixes: &prefixes}
	out := OverlayClaims(context.Background(), items, sources, selected, paths, func(key string) string {
		if key == "WORKLEASE_PROFILE" {
			return ""
		}
		return os.Getenv(key)
	})
	if out[0].Claim.Reason != "authority-mismatch" || out[0].NativeClaim != "not-exposed" || out[0].Claim.NativeState != "not-exposed" || ClaimActions(out[0])[ActionLaunch].Reasons[0] != "authority-mismatch" || len(a.calls) != 0 || len(out[0].Resources) != 1 || !strings.HasPrefix(out[0].Resources[0], "coordination:generic:") {
		t.Fatalf("mismatch: %+v", out[0])
	}
	selected.ID = profile.AuthorityID
	out = OverlayClaims(context.Background(), items, sources, selected, paths, func(string) string { return "" })
	if out[0].Claim.State != "free" || len(a.calls) != 1 {
		t.Fatalf("matching binding: %+v", out[0].Claim)
	}
}

func TestClaimOverlayAdmissionAndCheckoutAuthority(t *testing.T) {
	items, sources := claimFixtures(1)
	prefixes := []string{"github:"}
	a := &statusAuthority{}
	selected := ClaimAuthority{API: a, ID: "authority", Profile: "remote", Remote: true, AdmittedPrefixes: &prefixes}
	out := OverlayClaims(context.Background(), items, sources, selected, config.ProfilePaths{}, nil)
	if out[0].Claim.Reason != "resource-not-admitted" || len(a.calls) != 0 || ClaimActions(out[0])[ActionClaim].Reasons[0] != "resource-not-admitted" {
		t.Fatalf("admission: %+v", out[0])
	}
	sources["s"] = ClaimSource{Source: Source{ID: "s", Adapter: "backlog-md", Locator: t.TempDir()}}
	prefixes = []string{"backlog-md:"}
	selected.Remote = false
	out = OverlayClaims(context.Background(), items, sources, selected, config.ProfilePaths{}, nil)
	if out[0].Claim.Reason != "authority-mismatch" || len(a.calls) != 0 {
		t.Fatalf("checkout: %+v", out[0].Claim)
	}
}
