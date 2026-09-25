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
	authority.Authority // Only Status and List may be called by the overlay.
	calls               [][]string
	listCalls           int
	fail                bool
	split               bool
}

func (a *statusAuthority) List(context.Context, string, *lease.RemoteActor) ([]lease.ClaimView, error) {
	a.listCalls++
	return nil, nil
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

type heldStatusAuthority struct{}

func (heldStatusAuthority) Status(_ context.Context, selector lease.Selector) (lease.Status, error) {
	return lease.Status{Resources: []lease.ResourceStatus{{Resource: selector.Resources[0], State: "active", Claim: &lease.ClaimView{AuthorityID: selector.AuthorityID, AgentID: "holder", Active: true}}}}, nil
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
	if a.listCalls != 0 {
		t.Fatalf("unexpected List calls: %d", a.listCalls)
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
func TestExternalClaimsRequireExplicitGenericBinding(t *testing.T) {
	t.Parallel()
	configured := config.QueueSource{ID: "planning", Adapter: "external"}
	resolved := Source{ID: "planning", Adapter: ExternalSourceAdapterKey("planning"), Locator: "Display Name"}
	cfg := config.QueueConfig{Sources: []config.QueueSource{configured}}
	sources := ClaimSources(cfg, []Source{resolved})
	if sources["planning"].BlockReason != "claim-binding-required" {
		t.Fatalf("unbound external source was not blocked: %+v", sources["planning"])
	}
	items := []Item{{Summary: Summary{Ref: Ref{SourceID: "planning", ItemID: "42"}}}}
	authority := &statusAuthority{}
	observed := OverlayClaims(context.Background(), items, sources, ClaimAuthority{API: authority, ID: "authority"}, config.ProfilePaths{}, nil)
	if observed[0].Claim.Reason != "claim-binding-required" || len(observed[0].Resources) != 0 || observed[0].KeyInputs != nil || len(authority.calls) != 0 {
		t.Fatalf("unbound external identity was resolved: %+v calls=%v", observed[0], authority.calls)
	}

	cfg.Sources[0].Claims = &config.QueueClaims{Policy: "generic", Source: "acme/planning"}
	sources = ClaimSources(cfg, []Source{resolved})
	if source := sources["planning"]; source.BlockReason != "" || source.Policy != "generic" || source.ClaimSource != "acme/planning" {
		t.Fatalf("explicit external binding was not preserved: %+v", source)
	}
	observed = OverlayClaims(context.Background(), items, sources, ClaimAuthority{API: authority, ID: "authority"}, config.ProfilePaths{}, nil)
	want, err := resource.Resolve(resource.Input{Provider: "generic", Source: "acme/planning", Item: "42"})
	if err != nil || len(observed[0].Resources) != 1 || observed[0].Resources[0] != want.Resource || observed[0].KeyInputs == nil || observed[0].KeyInputs.Provider != "generic" || observed[0].KeyInputs.Source != "acme/planning" || len(authority.calls) != 1 {
		t.Fatalf("explicit generic claim key was not used: %+v calls=%v err=%v", observed[0], authority.calls, err)
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

func TestDefaultBacklogKeySourceIsBacklogDirectory(t *testing.T) {
	for _, tc := range []struct {
		name, config, folder, want string
		ok                         bool
	}{
		{"configured", "backlog_directory: docs/backlog\n", "", "docs/backlog", true},
		{"root default", "project_name: x\n", "", "backlog", true},
		{"escaping value ignored", "backlog_directory: ../elsewhere\n", ".backlog", ".backlog", true},
		{"folder config", "", "backlog", "backlog", true},
		{"no project", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkout := t.TempDir()
			if tc.config != "" {
				if err := os.WriteFile(filepath.Join(checkout, "backlog.config.yml"), []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.folder != "" {
				if err := os.Mkdir(filepath.Join(checkout, tc.folder), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			cfg := config.QueueConfig{Sources: []config.QueueSource{{ID: "b", Adapter: "backlog-md", Checkout: checkout}}}
			sources := ClaimSources(cfg, []Source{{ID: "b", Adapter: "backlog-md", Locator: checkout}})
			source, ok := sources["b"]
			if ok != tc.ok || ok && source.ClaimSource != filepath.Join(checkout, tc.want) {
				t.Fatalf("key source: %+v ok=%v", source, ok)
			}
		})
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
	if out[0].Claim.Reason != "authority-mismatch" || out[0].NativeClaim != "not-exposed" || out[0].Claim.NativeState != "not-exposed" || ClaimActions(out[0])[ActionLaunch].Reasons[0] != "authority-mismatch" || len(a.calls) != 1 || len(out[0].Resources) != 1 || !strings.HasPrefix(out[0].Resources[0], "coordination:generic:") {
		t.Fatalf("mismatch: %+v", out[0])
	}
	selected.ID = profile.AuthorityID
	out = OverlayClaims(context.Background(), items, sources, selected, paths, func(string) string { return "" })
	if out[0].Claim.State != "free" || len(a.calls) != 2 {
		t.Fatalf("matching binding: %+v", out[0].Claim)
	}
}

func TestCheckoutMismatchKeepsStatusObservationAndActionDenial(t *testing.T) {
	checkout := t.TempDir()
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
	selected := ClaimAuthority{API: heldStatusAuthority{}, ID: strings.Repeat("b", 32), Profile: "view", Remote: true, AdmittedPrefixes: &prefixes}
	out := OverlayClaims(context.Background(), items, sources, selected, paths, func(string) string { return "" })
	if out[0].Claim.Reason != "authority-mismatch" || !out[0].Claim.Known || out[0].Claim.State != "held" || out[0].Claim.AgentID != "holder" {
		t.Fatalf("mismatch lost holder observation: %+v", out[0].Claim)
	}
	for _, action := range []Action{ActionClaim, ActionLaunch} {
		if got := ClaimActions(out[0])[action]; got.Eligible || len(got.Reasons) == 0 || got.Reasons[0] != "authority-mismatch" {
			t.Fatalf("%s was not denied: %+v", action, got)
		}
	}
}

func TestLocalCheckoutAuthorityMustMatchWorkerDefaultAuthority(t *testing.T) {
	checkout := t.TempDir()
	if err := os.Mkdir(filepath.Join(checkout, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	items, sources := claimFixtures(1)
	sources["s"] = ClaimSource{Source: Source{ID: "s", Adapter: "backlog-md", Locator: checkout}}
	api := &statusAuthority{}
	profileDir := t.TempDir()
	paths := config.ProfilePaths{Profiles: filepath.Join(profileDir, "profiles.yaml"), Bindings: filepath.Join(profileDir, "bindings.yaml")}
	if err := config.SaveProfiles(paths, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveBindings(paths, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	selected := ClaimAuthority{API: api, ID: "queue-home-authority", Profile: config.LocalProfileName, LocalDefaultAuthorityID: "worker-home-authority"}
	out := OverlayClaims(context.Background(), items, sources, selected, paths, func(string) string { return "" })
	if out[0].Claim.Reason != "authority-mismatch" || len(api.calls) != 1 {
		t.Fatalf("worker local authority mismatch not detected: %+v calls=%v", out[0].Claim, api.calls)
	}
	selected.ID = selected.LocalDefaultAuthorityID
	if !matchingCheckoutAuthority(checkout, selected, paths, func(string) string { return "" }) {
		t.Fatal("local checkout authority did not match")
	}
	out = OverlayClaims(context.Background(), items, sources, selected, paths, func(string) string { return "" })
	if out[0].Claim.Reason != "" || !out[0].Claim.Known {
		t.Fatalf("matching local authority rejected: %+v", out[0].Claim)
	}
}

func TestClaimActionRequiresFreshVerifiedFreeObservation(t *testing.T) {
	item := Item{Summary: Summary{Ref: Ref{SourceID: "s", ItemID: "1"}, Fresh: true}, Readiness: Readiness{Status: Ready}, Claim: ClaimObservation{Known: true, Available: true, State: "free"}}
	if got := ClaimActions(item)[ActionClaim]; !got.Eligible {
		t.Fatalf("verified free item cannot be claimed: %+v", got)
	}
	item.Claim.Stale = true
	if got := ClaimActions(item)[ActionClaim]; got.Eligible {
		t.Fatalf("stale claim observation allowed acquisition: %+v", got)
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
	if out[0].Claim.Reason != "authority-mismatch" || !out[0].Claim.Known || len(a.calls) != 1 {
		t.Fatalf("checkout: %+v", out[0].Claim)
	}
}
