package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type githubTestTransport func(*http.Request) (*http.Response, error)

func (f githubTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGitHubEnterpriseUsesHostAPI(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/graphql" {
			t.Errorf("enterprise API path: %s", r.URL.Path)
		}
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if strings.Contains(request.Query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
		} else {
			fmt.Fprint(w, `{"data":{"repository":{"id":"R_123","nameWithOwner":"org/repo"}}}`)
		}
	}))
	defer server.Close()
	binary := filepath.Join(t.TempDir(), "gh")
	script := "#!/bin/sh\n[ \"$1 $2 $3 $4 $5 $6\" = \"auth token --hostname enterprise.example --user tester\" ] || exit 2\nprintf 'canary-enterprise-token\\n'\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	a := NewGitHubAdapter()
	a.Binary = binary
	a.Client = &http.Client{Transport: githubTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://enterprise.example/api/graphql" {
			t.Errorf("wrong enterprise origin: %s", req.URL)
		}
		forwarded := req.Clone(req.Context())
		forwarded.URL, _ = url.Parse(server.URL + req.URL.Path)
		forwarded.Host = forwarded.URL.Host
		return server.Client().Do(forwarded)
	})}
	source, err := a.Resolve(context.Background(), map[string]string{"host": "enterprise.example", "repository": "org/repo", "account": "tester"})
	if err != nil || source.Adapter != "github" {
		t.Fatalf("enterprise resolve: %+v %v", source, err)
	}
}
