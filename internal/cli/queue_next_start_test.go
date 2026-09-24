package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/mcp"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/testkit"
)

func nextStartFixture(t *testing.T) (string, string) {
	t.Helper()
	controller, _, root := queueStartFixture(t)
	for _, args := range [][]string{{"add", "docs/backlog"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture"}} {
		command := testkit.GitCommand(args...)
		command.Dir = root
		if data, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v %s", err, data)
		}
	}
	// The installed Backlog.md list omits dependency fields; supply its known
	// empty edge list so the read-only queue can select this isolated fixture.
	binary, err := exec.LookPath("backlog")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := t.TempDir()
	script := `#!/bin/sh
if [ -n "$QUEUE_FAIL_AFTER_EDIT" ] && [ -f "$QUEUE_FAIL_AFTER_EDIT" ] && [ "$1 $2" = "task view" ]; then exit 2; fi
if [ -n "$QUEUE_FAIL_AFTER_EDIT" ] && [ "$1 $2" = "task edit" ]; then
  "$REAL_BACKLOG" "$@" || exit $?
  printf 'edited' > "$QUEUE_FAIL_AFTER_EDIT"
  exit 0
fi
if [ "$1 $2 $3" = "task list --json" ]; then
  exec python3 -c 'import json,os,subprocess; data=json.loads(subprocess.check_output([os.environ["REAL_BACKLOG"],"task","list","--json"])); [row.setdefault("dependencies",[]) for row in data["tasks"]]; print(json.dumps(data))'
fi
exec "$REAL_BACKLOG" "$@"
`
	if err := os.WriteFile(filepath.Join(wrapper, "backlog"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REAL_BACKLOG", binary)
	t.Setenv("PATH", wrapper+string(os.PathListSeparator)+os.Getenv("PATH"))
	return controller.claim.backend.Config.Home, root
}

func nextStartCLI(t *testing.T, home string, args ...string) map[string]any {
	t.Helper()
	argv := append([]string{"worklease", "--home", home, "queue", "next", "--view", "Ready", "--json"}, args...)
	var output bytes.Buffer
	if err := Run(context.Background(), argv, "", "", "", &output, &output); err != nil {
		t.Fatalf("queue next: %v: %s", err, output.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope["next"].(map[string]any)
}

func TestQueueNextStartAppliesBacklogTransition(t *testing.T) {
	home, root := nextStartFixture(t)
	result := nextStartCLI(t, home, "--claim", "--start", "--session", "cli-worker")
	if result["claimOutcome"] != "applied" || result["transition"].(map[string]any)["outcome"] != "applied" {
		t.Fatalf("start outcome: %#v", result)
	}
	var renewed bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--home", home, "heartbeat", "--session", "cli-worker", "--json"}, "", "", "", &renewed, &renewed); err != nil {
		t.Fatalf("worker cannot renew its normal contextual handle: %v %s", err, renewed.String())
	}
	view := exec.Command("backlog", "task", "view", "TASK-1", "--json")
	view.Dir = root
	data, err := view.Output()
	if err != nil || !strings.Contains(string(data), `"status": "In Progress"`) {
		t.Fatalf("status: %v %s", err, data)
	}
}

func TestMCPQueueNextStartAppliesBacklogTransition(t *testing.T) {
	home, _ := nextStartFixture(t)
	s, err := mcp.NewServer(mcp.Options{Home: home, ProfileName: config.LocalProfileName, TTL: 30 * time.Second, QueueNext: mcpQueueNext(home, config.LocalProfileName)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	result, err := s.Call(context.Background(), "queue_next", map[string]any{"view": "Ready", "claim": true, "start": true, "sessionId": "mcp-worker", "ttl": float64(120), "maxHold": float64(300)})
	if err != nil || result["isError"] == true {
		t.Fatalf("MCP start: %v %#v", err, result)
	}
	next := result["structuredContent"].(map[string]any)["next"].(map[string]any)
	if next["claimOutcome"] != "applied" || next["transition"].(map[string]any)["outcome"] != "applied" {
		t.Fatalf("MCP outcome: %#v", next)
	}
	grant := next["claim"].(map[string]any)
	leaseRef := grant["lease"].(string)
	private, err := handle.Read(grant["handlePath"].(string))
	if err != nil || private.HoldUntil.IsZero() || private.AutoRenewOwner == "" {
		t.Fatalf("MCP hold/automatic renewal lost after checkpoint: %+v %v", private, err)
	}
	verified, err := s.Call(context.Background(), "verify", map[string]any{"lease": leaseRef})
	if err != nil || verified["isError"] == true {
		t.Fatalf("claim not verified after start: %v %#v", err, verified)
	}
	before := private.Revision
	beat, err := s.Call(context.Background(), "heartbeat", map[string]any{"lease": leaseRef})
	if err != nil || beat["isError"] == true {
		t.Fatalf("MCP renewal failed after start: %v %#v", err, beat)
	}
	private, err = handle.Read(grant["handlePath"].(string))
	if err != nil || private.Revision <= before || private.HoldUntil.IsZero() {
		t.Fatalf("renewal did not advance within hold budget: %+v %v", private, err)
	}
}

func TestQueueNextStartOutcomes(t *testing.T) {
	for _, mode := range []string{"cli", "mcp"} {
		for _, scenario := range []struct {
			name, replacement, outcome string
			uncertain                  bool
		}{
			{"missing-mapping", "", "not attempted", false},
			{"invalid-transition", "start: Missing", "rejected", false},
			{"unknown-readback", "start: In Progress", "unknown", true},
		} {
			t.Run(mode+"/"+scenario.name, func(t *testing.T) {
				home, root := nextStartFixture(t)
				if !scenario.uncertain {
					path := config.QueuePath(os.Getenv)
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					replacement := ""
					if scenario.replacement != "" {
						replacement = "    workflow:\n      " + scenario.replacement + "\n"
					}
					if err := handle.WriteOwnerPrivate(path, []byte(strings.Replace(string(data), "    workflow:\n      start: In Progress\n", replacement, 1)), 1<<20); err != nil {
						t.Fatal(err)
					}
				} else {
					t.Setenv("QUEUE_FAIL_AFTER_EDIT", filepath.Join(t.TempDir(), "dispatched"))
				}
				var next map[string]any
				if mode == "cli" {
					next = nextStartCLI(t, home, "--claim", "--start", "--session", "worker")
				} else {
					s, err := mcp.NewServer(mcp.Options{Home: home, ProfileName: config.LocalProfileName, TTL: 30 * time.Second, QueueNext: mcpQueueNext(home, config.LocalProfileName)})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(s.Close)
					result, err := s.Call(context.Background(), "queue_next", map[string]any{"view": "Ready", "claim": true, "start": true, "sessionId": "worker", "autoHeartbeat": false})
					if err != nil || result["isError"] == true {
						t.Fatalf("MCP start: %v %#v", err, result)
					}
					next = result["structuredContent"].(map[string]any)["next"].(map[string]any)
				}
				transition := next["transition"].(map[string]any)
				if next["claimOutcome"] != "applied" || transition["outcome"] != scenario.outcome {
					t.Fatalf("claim must remain held with %s transition: %#v", scenario.outcome, next)
				}
				if scenario.outcome == "rejected" && transition["message"] != "Claim acquired; status unchanged" {
					t.Fatalf("rejection message: %#v", transition)
				}
				if scenario.uncertain {
					if transition["operationId"] == nil {
						t.Fatalf("missing recovery operation: %#v", transition)
					}
					journal, err := queueRecoveryJournal()
					if err != nil {
						t.Fatal(err)
					}
					entries, err := journal.Recovery()
					if err != nil || len(entries) != 1 {
						t.Fatalf("unknown write not journaled: %+v %v", entries, err)
					}
				}
				view := exec.Command(os.Getenv("REAL_BACKLOG"), "task", "view", "TASK-1", "--json")
				view.Dir = root
				data, err := view.Output()
				if err != nil {
					t.Fatal(err)
				}
				if scenario.uncertain == !strings.Contains(string(data), `"status": "In Progress"`) {
					t.Fatalf("unexpected provider status: %s", data)
				}
			})
		}
	}
}

func TestQueueNextStartPlainReportsBothSteps(t *testing.T) {
	home, _ := nextStartFixture(t)
	path := config.QueuePath(os.Getenv)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.WriteOwnerPrivate(path, []byte(strings.Replace(string(data), "start: In Progress", "start: Missing", 1)), 1<<20); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--home", home, "queue", "next", "--view", "Ready", "--claim", "--start", "--session", "worker"}, "", "", "", &output, &output); err != nil || !strings.Contains(output.String(), "Claim acquired; status unchanged") || !strings.Contains(output.String(), "claim: applied; transition: rejected") {
		t.Fatalf("plain result: %v %s", err, output.String())
	}
}

func TestQueueNextStartRejectsChangedActorAfterClaim(t *testing.T) {
	controller, item, root := queueStartFixture(t)
	ctx := context.Background()
	plan, err := controller.claim.prepare(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.claim.acquire(ctx, plan); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	path := config.QueuePath(os.Getenv)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.WriteOwnerPrivate(path, []byte(strings.Replace(string(data), "'@bob'", "'@alice'", 1)), 1<<20); err != nil {
		t.Fatal(err)
	}
	auth, _ := controller.claim.current()
	got := queueNextStart(ctx, cfg, item, controller.claim.registry, []queue.Source{controller.claim.sources[item.Ref.SourceID]}, controller.claim.backend, auth, controller.claim.queueSession, plan.handle)
	if got["outcome"] != "rejected" || !strings.Contains(got["reason"].(string), "actor changed") {
		t.Fatalf("stale provider actor wrote: %#v", got)
	}
	view := exec.Command("backlog", "task", "view", "TASK-1", "--json")
	view.Dir = root
	read, err := view.Output()
	if err != nil || !strings.Contains(string(read), `"status": "To Do"`) {
		t.Fatalf("provider changed: %v %s", err, read)
	}
}

func TestQueueNextStartGitHubHasNoStatusMapping(t *testing.T) {
	cfg := config.QueueConfig{Sources: []config.QueueSource{{ID: "issues", Adapter: "github", Workflow: map[string]string{"start": "in-progress"}}}}
	item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "issues", ItemID: "1"}}}
	got := queueNextStart(context.Background(), cfg, item, nil, nil, nil, queue.ClaimAuthority{}, "worker", "private-handle")
	if got["outcome"] != "not attempted" || !strings.Contains(got["reason"].(string), "mapping") {
		t.Fatalf("GitHub issue state was invented: %#v", got)
	}
}

func TestQueueNextStartRequiresClaim(t *testing.T) {
	home, _ := nextStartFixture(t)
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--home", home, "queue", "next", "--view", "Ready", "--start", "--json"}, "", "", "", &output, &output); err == nil || !strings.Contains(output.String(), "--start requires --claim") {
		t.Fatalf("CLI accepted start without claim: %s", output.String())
	}
	s, err := mcp.NewServer(mcp.Options{Home: home, QueueNext: mcpQueueNext(home, config.LocalProfileName)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	result, err := s.Call(context.Background(), "queue_next", map[string]any{"view": "Ready", "start": true})
	if err != nil || result["isError"] != true {
		t.Fatalf("MCP accepted start without claim: %v %#v", err, result)
	}
}
