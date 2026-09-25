package queue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	}
	if tested < 3 {
		t.Fatalf("too few portable identity vectors: %d", tested)
	}
}
