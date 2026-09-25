package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/testkit"
)

// A cancellation must reach the built-in provider operation, not merely stop
// the host's wait for a protocol response.
func TestAdapterConformanceBuiltInCancellation(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"backlog-md", "github"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			_, paths := testkit.Home(t)
			env := externalTestEnvironment(paths)
			started := filepath.Join(t.TempDir(), "started")
			done := filepath.Join(t.TempDir(), "done")
			fixture := map[string]any{"cancelMarker": done}
			var githubStarted chan struct{}
			if kind == "backlog-md" {
				root, binary := fakeBacklog(t)
				contents, err := os.ReadFile(binary)
				if err != nil {
					t.Fatal(err)
				}
				replaced := strings.Replace(string(contents), "/bin/cat backlog-list.json", fmt.Sprintf("printf started > %s; sleep 30", shellQuote(started)), 1)
				if err := os.WriteFile(binary, []byte(replaced), 0700); err != nil {
					t.Fatal(err)
				}
				fixture["checkout"], fixture["binary"] = root, binary
			} else {
				githubStarted = make(chan struct{}, 1)
				built, server := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
					var request struct {
						Query string `json:"query"`
					}
					_ = json.NewDecoder(r.Body).Decode(&request)
					if strings.Contains(request.Query, "viewer") {
						_, _ = fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
						return
					}
					githubStarted <- struct{}{}
					<-r.Context().Done()
				})
				fixture["binary"], fixture["apiBase"] = built.Binary, server.URL
			}
			executable := filepath.Join(t.TempDir(), "cancellable-shim")
			script := fmt.Sprintf("#!/bin/sh\nexec %s -test.run='^TestAdapterConformanceProcessHelper$' -- %s\n", shellQuote(os.Args[0]), shellQuote(kind))
			if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			executable, err := filepath.EvalSymlinks(executable)
			if err != nil {
				t.Fatal(err)
			}
			configured := config.QueueSource{ID: "cancel-fixture", Adapter: "external", Executable: executable, ExpectedAdapterID: "conformance.shim", ExpectedVersion: "1.0.0", Config: fixture}
			if err := config.ApproveQueueAdapter(context.Background(), env, configured); err != nil {
				t.Fatal(err)
			}
			adapter := &ExternalAdapter{source: configured, env: env}
			t.Cleanup(adapter.Close)
			source, err := adapter.Resolve(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := adapter.List(ctx, source, Query{Budget: 1}, ""); result <- err }()
			if kind == "backlog-md" {
				waitForFile(t, started)
			} else {
				select {
				case <-githubStarted:
				case <-time.After(5 * time.Second):
					t.Fatal("GitHub provider request never started")
				}
			}
			cancel()
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("cancelled list succeeded")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cancelled host request did not return")
			}
			waitForFile(t, done+".done")
		})
	}
}

func TestAdapterConformanceRejectsEarlyCancellationDoneMarker(t *testing.T) {
	t.Parallel()
	root, binary := fakeBacklog(t)
	marker := filepath.Join(t.TempDir(), "early-cancel")
	executable := filepath.Join(t.TempDir(), "early-done-shim")
	script := fmt.Sprintf("#!/bin/sh\nexec %s -test.run='^TestAdapterConformanceProcessHelper$' -- backlog-md\n", shellQuote(os.Args[0]))
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	report, err := CheckExternalAdapter(context.Background(), AdapterCheckOptions{
		Executable: executable, Config: map[string]any{"checkout": root, "binary": binary, "cancelMarker": marker, "doneAtEntry": "true"},
		CancelMarker: marker,
	})
	if err != nil || report.Verdict != "fail" {
		t.Fatalf("early cancellation completion marker was accepted: %+v %v", report, err)
	}
	for _, check := range report.Checks {
		if check.ID == "cancel-notification" && check.Status == "fail" && check.Reason == "done-before-cancellation" {
			return
		}
	}
	t.Fatalf("missing early completion failure: %+v", report.Checks)
}
