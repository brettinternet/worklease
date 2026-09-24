package queue

import (
	"context"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/resource"
)

type identityStatus struct {
	held map[string]bool
	fail bool
}

func (a identityStatus) Status(_ context.Context, selector lease.Selector) (lease.Status, error) {
	if a.fail {
		return lease.Status{}, context.DeadlineExceeded
	}
	result := lease.Status{}
	for _, key := range selector.Resources {
		entry := lease.ResourceStatus{Resource: key, State: "free"}
		if a.held[key] {
			entry.State = "active"
			entry.Claim = &lease.ClaimView{AuthorityID: selector.AuthorityID, Active: true}
		}
		result.Resources = append(result.Resources, entry)
	}
	return result, nil
}

func identityFixture() (ClaimSource, *fakeAdapter, ClaimAuthority, config.QueueIdentity) {
	source := ClaimSource{Source: Source{ID: "s", Adapter: "backlog-md", Locator: "/checkout"}, Policy: "generic", ClaimSource: "portable"}
	adapter := newFake()
	adapter.pages["s"] = []SummaryPage{{Items: []Summary{{Ref: Ref{"s", "1"}}}, Coverage: Coverage{State: CoverageComplete}}}
	authority := ClaimAuthority{API: identityStatus{}, ID: "authority"}
	return source, adapter, authority, IdentityInputs(source, authority.ID)
}

func TestIdentityDuplicateGuardRechecksFreshList(t *testing.T) {
	source, adapter, authority, receipt := identityFixture()
	if code, _ := IdentityGate(context.Background(), source, adapter, authority, receipt, nil, nil); code != "" {
		t.Fatal(code)
	}
	adapter.pages["s"] = []SummaryPage{{Items: []Summary{{Ref: Ref{"s", "1"}}, {Ref: Ref{"s", "1"}}}, Coverage: Coverage{State: CoverageUnknown}}}
	for i := 0; i < 2; i++ { // availability and the immediate pre-acquisition check
		if code, _ := IdentityGate(context.Background(), source, adapter, authority, receipt, nil, nil); code != "duplicate-item-id" {
			t.Fatalf("pass %d: %s", i, code)
		}
	}
}

func TestIdentityMigrationAndHeldOldKey(t *testing.T) {
	source, adapter, authority, receipt := identityFixture()
	receipt.Policy, receipt.Source = "backlog-md", "/checkout/backlog"
	receipt.ItemIDs = []string{"1"}
	if code, detail := IdentityGate(context.Background(), source, adapter, authority, receipt, nil, nil); code != "binding-migration-required" || !strings.Contains(detail, "backlog-md") || !strings.Contains(detail, "other authorities") {
		t.Fatalf("migration: %s %s", code, detail)
	}
	key, err := resource.Resolve(resource.Input{Provider: receipt.Policy, Source: receipt.Source, Item: "1"})
	if err != nil {
		t.Fatal(err)
	}
	authority.API = identityStatus{held: map[string]bool{key.Resource: true}}
	if err := PreviousKeys(context.Background(), authority, receipt, []string{"1"}); err == nil {
		t.Fatal("confirmed while old key was held")
	}
	authority.API = identityStatus{fail: true}
	if err := PreviousKeys(context.Background(), authority, receipt, []string{"1"}); err == nil {
		t.Fatal("confirmed despite unknown authority status")
	}
}

func TestPreAcquireIdentityUsesFreshListAndBothClaimDomains(t *testing.T) {
	source, adapter, authority, receipt := identityFixture()
	receipt.Retired = []config.ClaimDomain{{Policy: "generic", Source: "old-portable"}}
	item := Item{Summary: Summary{Ref: Ref{"s", "1"}}}
	keys, err := PreAcquireIdentity(context.Background(), source, adapter, authority, receipt, item)
	if err != nil || len(keys) != 2 {
		t.Fatalf("old/new atomic claim: %v %v", keys, err)
	}
	if keys[0] == keys[1] || !strings.HasPrefix(keys[0], "coordination:generic:") || !strings.HasPrefix(keys[1], "coordination:generic:") {
		t.Fatalf("wrong exclusion domains: %v", keys)
	}
	prefixes := []string{"github:"}
	authority.Remote, authority.AdmittedPrefixes = true, &prefixes
	if _, err := PreAcquireIdentity(context.Background(), source, adapter, authority, receipt, item); err == nil {
		t.Fatal("unadmitted retired domain passed pre-acquisition gate")
	}
	authority.Remote = false
	authority.API = identityStatus{held: map[string]bool{keys[1]: true}}
	if _, err := PreAcquireIdentity(context.Background(), source, adapter, authority, receipt, item); err == nil {
		t.Fatal("held old key passed pre-acquisition gate")
	}
	authority.API = identityStatus{}
	adapter.pages["s"] = []SummaryPage{{Coverage: Coverage{State: CoverageComplete}}}
	if _, err := PreAcquireIdentity(context.Background(), source, adapter, authority, receipt, item); err == nil {
		t.Fatal("vanished item passed pre-acquisition re-read")
	}
	adapter.pages["s"] = []SummaryPage{{Items: []Summary{{Ref: Ref{"s", "1"}}, {Ref: Ref{"s", "1"}}}, Coverage: Coverage{State: CoverageUnknown}}}
	if _, err := PreAcquireIdentity(context.Background(), source, adapter, authority, receipt, item); err == nil {
		t.Fatal("new duplicate passed pre-acquisition re-read")
	}
}

func TestIdentityRenameTransferRenumberAndRebind(t *testing.T) {
	source, adapter, authority, receipt := identityFixture()
	receipt.ItemIDs = []string{"1"}
	key, err := resource.Resolve(resource.Input{Provider: receipt.Policy, Source: receipt.Source, Item: "1"})
	if err != nil {
		t.Fatal(err)
	}
	authority.API = identityStatus{held: map[string]bool{key.Resource: true}}
	adapter.pages["s"] = []SummaryPage{{Coverage: Coverage{State: CoverageComplete}}}
	stale := []Item{{Summary: Summary{Ref: Ref{"s", "1"}}}}
	if code, _ := IdentityGate(context.Background(), source, adapter, authority, receipt, stale, nil); code != "identity-changed" {
		t.Fatalf("renumber: %s", code)
	}
	item := Item{Summary: Summary{Ref: Ref{"s", "1"}}, ReadOutcome: "identity-changed"}
	if code, _ := IdentityGate(context.Background(), source, adapter, authority, receipt, []Item{item}, nil); code != "identity-changed" {
		t.Fatalf("transfer: %s", code)
	}
	github := ClaimSource{Source: Source{ID: "g", Adapter: "github", Locator: "new/repo"}, ClaimSource: "new/repo"}
	old := config.QueueIdentity{Adapter: "github", Locator: "old/repo", Policy: "github", Source: "old/repo", AuthorityID: authority.ID}
	if code, detail := IdentityGate(context.Background(), github, adapter, authority, old, nil, nil); code != "binding-migration-required" || !strings.Contains(detail, "old/repo") || !strings.Contains(detail, "new/repo") {
		t.Fatalf("rebind: %s %s", code, detail)
	}
	github.Source.Locator = "old/repo"
	github.ClaimSource = "old/repo"
	if code, _ := IdentityGate(context.Background(), github, adapter, authority, IdentityInputs(github, authority.ID), []Item{{Summary: Summary{Ref: Ref{"g", "2"}}, ReadOutcome: "identity-changed"}}, nil); code != "identity-changed" {
		t.Fatalf("rename: %s", code)
	}
}

func TestIdentityPartialObservationIsNotRenumber(t *testing.T) {
	t.Parallel()
	source := ClaimSource{Source: Source{ID: "g", Adapter: "github", Locator: "o/r"}, Policy: "github", ClaimSource: "github.com/o/r"}
	receipt := IdentityInputs(source, "authority")
	receipt.ItemIDs = []string{"1", "2"}
	key, err := resource.Resolve(resource.Input{Provider: receipt.Policy, Source: receipt.Source, Item: "2"})
	if err != nil {
		t.Fatal(err)
	}
	authority := ClaimAuthority{API: identityStatus{held: map[string]bool{key.Resource: true}}, ID: "authority"}
	if _, err := PreAcquireIdentity(context.Background(), source, newFake(), authority, receipt, Item{Summary: Summary{Ref: Ref{"g", "1"}}}); err != nil {
		t.Fatalf("another held item blocked the claim: %v", err)
	}
	if code, _ := IdentityGate(context.Background(), source, newFake(), authority, receipt, nil, []Ref{{"g", "2"}}); code != "identity-changed" {
		t.Fatalf("held deleted item: %s", code)
	}
}

func TestPreAcquireIdentityRejectsHeldRenumberUnderDefaultBacklogPolicy(t *testing.T) {
	t.Parallel()
	source := ClaimSource{Source: Source{ID: "s", Adapter: "backlog-md", Locator: "/checkout"}, ClaimSource: "/checkout/.git"}
	receipt := IdentityInputs(source, "authority")
	receipt.ItemIDs = []string{"A"}
	key, err := resource.Resolve(resource.Input{Provider: receipt.Policy, Source: receipt.Source, Item: "A"})
	if err != nil {
		t.Fatal(err)
	}
	authority := ClaimAuthority{API: identityStatus{held: map[string]bool{key.Resource: true}}, ID: "authority"}
	adapter := newFake()
	adapter.pages["s"] = []SummaryPage{{Items: []Summary{{Ref: Ref{"s", "B"}}}, Coverage: Coverage{State: CoverageComplete}}}
	b := Item{Summary: Summary{Ref: Ref{"s", "B"}}}
	if _, err := PreAcquireIdentity(context.Background(), source, adapter, authority, receipt, b); err == nil || !strings.Contains(err.Error(), "identity-changed") {
		t.Fatalf("held renumbered task passed the final gate: %v", err)
	}
	adapter.pages["s"] = []SummaryPage{{Items: []Summary{{Ref: Ref{"s", "A"}}, {Ref: Ref{"s", "B"}}}, Coverage: Coverage{State: CoverageComplete}}}
	if _, err := PreAcquireIdentity(context.Background(), source, adapter, authority, receipt, b); err != nil {
		t.Fatalf("held task still present blocked another claim: %v", err)
	}
}

func TestPreAcquireIdentityRejectsDriftLatchedAfterItemRead(t *testing.T) {
	t.Parallel()
	adapter := NewGitHubAdapter()
	adapter.bindings["g"] = &githubBinding{host: "github.com", repository: "o/r"}
	source := ClaimSource{Source: Source{ID: "g", Adapter: "github", Locator: "o/r"}, Policy: "github", ClaimSource: "github.com/o/r"}
	authority := ClaimAuthority{API: identityStatus{}, ID: "authority"}
	receipt := IdentityInputs(source, authority.ID)
	item := Item{Summary: Summary{Ref: Ref{"g", "1"}}, ReadOutcome: "found"}
	adapter.drift(adapter.bindings["g"])
	if _, err := PreAcquireIdentity(context.Background(), source, adapter, authority, receipt, item); err == nil || !strings.Contains(err.Error(), "identity-changed") {
		t.Fatalf("latched drift passed the final gate: %v", err)
	}
}

func TestGitHubIdentityChangeIncludesOldAndNewLocators(t *testing.T) {
	adapter := NewGitHubAdapter()
	adapter.bindings["g"] = &githubBinding{host: "github.com", repository: "old/repo"}
	adapter.driftTo(adapter.bindings["g"], "new/repo")
	if detail := adapter.IdentityChange(Source{ID: "g"}); !strings.Contains(detail, "old/repo") || !strings.Contains(detail, "new/repo") {
		t.Fatalf("rename locators missing: %s", detail)
	}
}

func TestIdentityOverlayKeepsConfiguredResourceAndBlocksAction(t *testing.T) {
	source, _, authority, _ := identityFixture()
	source.Source.Adapter = "github" // exercise the gate without checkout authority policy
	source.BlockReason, source.BlockDetail = "binding-migration-required", MigrationChecklist
	items := []Item{{Summary: Summary{Ref: Ref{"s", "1"}}, Readiness: Readiness{Status: Ready}}}
	observed := OverlayClaims(context.Background(), items, map[string]ClaimSource{"s": source}, authority, config.ProfilePaths{}, nil)
	key, err := resource.Resolve(resource.Input{Provider: "generic", Source: "portable", Item: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if observed[0].Resources[0] != key.Resource || observed[0].KeyInputs.Source != "portable" || observed[0].Claim.Reason != "binding-migration-required" || ClaimActions(observed[0])[ActionClaim].Reasons[0] != "binding-migration-required" {
		t.Fatalf("overlay: %+v", observed[0])
	}
}
