package queue

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/resource"
)

// The resource vectors are shared with CLI/MCP policy tests, not redefined by
// the protocol shim. External adapters supply inputs; the host owns the key.
func TestAdapterConformanceIdentityVectors(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "resource", "testdata", "key-vectors-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version int `json:"version"`
		Vectors []struct {
			Name     string `json:"name"`
			Provider string `json:"provider"`
			Source   string `json:"source"`
			Item     string `json:"item"`
			Resource string `json:"resource"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 {
		t.Fatalf("unexpected identity fixture version %d", fixture.Version)
	}
	tested := 0
	var linear struct{ source, item, resource string }
	for _, vector := range fixture.Vectors {
		if strings.Contains(vector.Source, "${") {
			continue
		}
		t.Run(vector.Name, func(t *testing.T) {
			derived, err := resource.Resolve(resource.Input{Provider: vector.Provider, Source: vector.Source, Item: vector.Item})
			if err != nil || derived.Resource != vector.Resource {
				t.Fatalf("resource = %q, want %q; error=%v", derived.Resource, vector.Resource, err)
			}
		})
		tested++
		if vector.Name == "linear-test-1-before-team-move" {
			linear.source, linear.item, linear.resource = vector.Source, vector.Item, vector.Resource
		}
	}
	if tested < 3 {
		t.Fatalf("too few portable identity vectors: %d", tested)
	}
	if linear.resource == "" {
		t.Fatal("Linear identity vector missing")
	}
	// Exercise the common queue derivation and remote pre-acquisition gate
	// with probe-backed organization/UUID inputs, not team or issue aliases.
	source := ClaimSource{Source: Source{ID: "linear-team", Adapter: "linear", Locator: "TEST"}, Policy: "linear", ClaimSource: linear.source}
	item := Item{Summary: Summary{Ref: Ref{SourceID: source.Source.ID, ItemID: linear.item}}, ReadOutcome: "found", Readiness: Readiness{Status: Ready}}
	prefixes := []string{"coordination:"}
	authority := ClaimAuthority{API: identityStatus{}, ID: "remote", Remote: true, AdmittedPrefixes: &prefixes}
	observed := OverlayClaims(context.Background(), []Item{item}, map[string]ClaimSource{source.Source.ID: source}, authority, config.ProfilePaths{}, nil)
	if len(observed[0].Resources) != 1 || observed[0].Resources[0] != linear.resource || observed[0].KeyInputs.Source != linear.source || observed[0].KeyInputs.Item != linear.item || !observed[0].Claim.Available {
		t.Fatalf("queue identity/admission differs from CLI vector: %+v", observed[0])
	}
	source.Source.Locator = "PDEV"
	moved := OverlayClaims(context.Background(), []Item{item}, map[string]ClaimSource{source.Source.ID: source}, authority, config.ProfilePaths{}, nil)
	if len(moved[0].Resources) != 1 || moved[0].Resources[0] != linear.resource {
		t.Fatalf("team move changed Linear claim key: %+v", moved[0])
	}
	keys, err := PreAcquireIdentity(context.Background(), source, newFake(), authority, IdentityInputs(source, authority.ID), item)
	if err != nil || len(keys) != 1 || keys[0] != linear.resource {
		t.Fatalf("remote pre-acquisition rejected Linear vector: %v %v", keys, err)
	}
	item.Ref.ItemID = ""
	observed = OverlayClaims(context.Background(), []Item{item}, map[string]ClaimSource{source.Source.ID: source}, authority, config.ProfilePaths{}, nil)
	if observed[0].Claim.Available || observed[0].Claim.Reason != "invalid-resource" {
		t.Fatalf("missing issue UUID allowed a claim: %+v", observed[0].Claim)
	}
	item.Ref.ItemID = linear.item
	item.ReadOutcome = "identity-changed"
	if _, err := PreAcquireIdentity(context.Background(), source, newFake(), authority, IdentityInputs(source, authority.ID), item); err == nil || !strings.Contains(err.Error(), "identity-changed") {
		t.Fatalf("unresolved issue identity passed gate: %v", err)
	}
	authority.AdmittedPrefixes = nil
	observed = OverlayClaims(context.Background(), []Item{item}, map[string]ClaimSource{source.Source.ID: source}, authority, config.ProfilePaths{}, nil)
	if observed[0].Claim.Available || observed[0].Claim.Reason != "admission-unknown" {
		t.Fatalf("unknown remote admission allowed a claim: %+v", observed[0].Claim)
	}
}
