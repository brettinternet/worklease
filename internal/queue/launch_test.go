package queue

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/resource"
)

func launchFixture(t *testing.T, id string) (Item, config.QueueSource, ClaimAuthority) {
	t.Helper()
	inputs := resource.Input{Provider: "generic", Source: "project", Item: id}
	key, err := resource.Resolve(inputs)
	if err != nil {
		t.Fatal(err)
	}
	return Item{Summary: Summary{Ref: Ref{SourceID: "source", ItemID: id}, Title: "hostile title '-option' $(echo injected)\nsecond line", CanonicalID: "not-a-launch-identifier"}, Body: "hostile body with quotes and $(echo injected)\n", Resources: []string{key.Resource}, KeyInputs: &inputs, Claim: ClaimObservation{AuthorityID: "authority", SessionID: "session-secret"}}, config.QueueSource{ID: "source", Adapter: "github"}, ClaimAuthority{ID: "authority", Profile: "team"}
}

func TestLaunchGatesAndPublicPreview(t *testing.T) {
	item, source, authority := launchFixture(t, "42")
	item.Readiness.Status = Ready
	item.Fresh, item.DependenciesKnown, item.Claim.Known = true, true, true
	item.Claim.State = "free"
	item.Claim.Stale = false
	action := config.QueueLaunch{Name: "worker", Argv: []string{"echo", "--", "{itemId}"}, Cwd: t.TempDir(), PassEnv: []string{"SECRET"}}
	option, handoff := CheckLaunch(action, item, source, authority, "queue-session", []string{"SECRET=private-value"})
	if !option.Eligibility.Eligible || len(handoff.Argv) != 3 || !strings.Contains(strings.Join(option.EnvNames, ","), "SECRET") || strings.Contains(strings.Join(option.EnvNames, ","), "private-value") {
		t.Fatalf("preview: %+v handoff: %+v", option, handoff)
	}
	item.Claim.Reason = "authority-mismatch"
	option, handoff = CheckLaunch(action, item, source, authority, "queue-session", nil)
	if option.Eligibility.Reasons[0] != "authority-mismatch" || len(handoff.Argv) != 0 || len(option.Argv) != 3 {
		t.Fatalf("authority gate and preview: %+v", option)
	}
	item.Claim.Reason = ""
	item.Claim.Active, item.Claim.SessionID = true, "queue-session"
	option, handoff = CheckLaunch(action, item, source, authority, "queue-session", nil)
	if option.Eligibility.Reasons[0] != "queue-holds-claim" || !strings.Contains(strings.Join(option.Eligibility.Reasons, " "), "another worker may acquire") || len(handoff.Argv) != 0 || len(option.Argv) != 3 {
		t.Fatalf("queue gate: %+v", option)
	}
	item.Claim.Active, item.Claim.SessionID = false, ""
	option, _ = CheckLaunch(action, item, source, authority, "queue-session", nil)
	if !option.Eligibility.Eligible {
		t.Fatalf("release enables launch: %+v", option)
	}
	item.Claim.AuthorityID = "other"
	option, _ = CheckLaunch(action, item, source, authority, "queue-session", nil)
	if option.Eligibility.Reasons[0] != "authority-mismatch" {
		t.Fatalf("wrong authority: %+v", option)
	}
}

func TestLaunchProcessStartFailureHasNoClaimSideEffect(t *testing.T) {
	item, source, authority := launchFixture(t, "42")
	item.Readiness.Status, item.Claim.Known, item.Claim.State = Ready, true, "free"
	action := config.QueueLaunch{Name: "missing", Argv: []string{filepath.Join(t.TempDir(), "missing")}, Cwd: t.TempDir()}
	option, handoff := CheckLaunch(action, item, source, authority, "queue", nil)
	if !option.Eligibility.Eligible {
		t.Fatalf("expected eligible: %+v", option)
	}
	if _, err := StartLaunch(context.Background(), handoff); err == nil || item.Claim.Active {
		t.Fatalf("failed process start changed claim: %v %+v", err, item.Claim)
	}
}

func TestLaunchHandoffIncludesRetiredBindingKeys(t *testing.T) {
	source, adapter, authority, receipt := identityFixture()
	authority.Profile = "local"
	receipt.Retired = []config.ClaimDomain{{Policy: "generic", Source: "old-portable"}}
	current, err := resource.Resolve(resource.Input{Provider: "generic", Source: "portable", Item: "1"})
	if err != nil {
		t.Fatal(err)
	}
	inputs := resource.Input{Provider: "generic", Source: "portable", Item: "1"}
	item := Item{Summary: Summary{Ref: Ref{SourceID: "s", ItemID: "1"}}, KeyInputs: &inputs, Resources: []string{current.Resource}, Readiness: Readiness{Status: Ready}, Claim: ClaimObservation{AuthorityID: authority.ID, Known: true, State: "free"}}
	keys, err := LaunchResources(item, receipt, authority)
	if err != nil || len(keys) != 2 {
		t.Fatalf("launch keys: %v %v", keys, err)
	}
	fresh, err := PreAcquireIdentity(context.Background(), source, adapter, authority, receipt, item)
	if err != nil || !reflect.DeepEqual(keys, fresh) {
		t.Fatalf("launch differs from atomic claim: %v %v %v", keys, fresh, err)
	}
	item.Resources = keys
	action := config.QueueLaunch{Name: "worker", Argv: []string{"echo", "{itemId}"}, Cwd: t.TempDir()}
	option, handoff := CheckLaunch(action, item, config.QueueSource{ID: "s", Adapter: "backlog-md"}, authority, "queue", nil)
	if !option.Eligibility.Eligible || !strings.Contains(strings.Join(handoff.Env, "\n"), "WORKLEASE_QUEUE_RESOURCES=[") {
		t.Fatalf("migrated handoff: %+v %+v", option, handoff)
	}
	var passed []string
	for _, entry := range handoff.Env {
		if strings.HasPrefix(entry, "WORKLEASE_QUEUE_RESOURCES=") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(entry, "WORKLEASE_QUEUE_RESOURCES=")), &passed); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !reflect.DeepEqual(passed, keys) {
		t.Fatalf("retired key missing from handoff: %v", passed)
	}
	authority.API = identityStatus{held: map[string]bool{keys[1]: true}}
	if _, err := PreAcquireIdentity(context.Background(), source, adapter, authority, receipt, item); err == nil {
		t.Fatal("held retired key did not refuse launch confirmation")
	}
}

func TestLaunchCarriesExactGitHubIdentityVector(t *testing.T) {
	inputs := resource.Input{Provider: "github", Source: "Owner/Repo.GIT", Item: "ITEM:1"}
	key, err := resource.Resolve(inputs)
	if err != nil || key.Resource != "github:owner%2Frepo#ITEM%3A1" {
		t.Fatalf("identity vector: %+v %v", key, err)
	}
	item := Item{Summary: Summary{Ref: Ref{SourceID: "source", ItemID: inputs.Item}}, KeyInputs: &inputs, Resources: []string{key.Resource}, Claim: ClaimObservation{AuthorityID: "authority"}}
	source := config.QueueSource{ID: "source", Adapter: "github"}
	handoff, disabled := PrepareLaunch(config.QueueLaunch{Argv: []string{"echo", "{ref}"}, Cwd: t.TempDir()}, item, source, ClaimAuthority{ID: "authority", Profile: "local"}, nil)
	if disabled != "" || !reflect.DeepEqual(handoff.Argv, []string{"echo", "source:ITEM:1"}) || !strings.Contains(strings.Join(handoff.Env, "\n"), "WORKLEASE_QUEUE_RESOURCES=[\"github:owner%2Frepo#ITEM%3A1\"]") {
		t.Fatalf("handoff vector: %+v %s", handoff, disabled)
	}
}

func TestPrepareLaunchUnresolvedAndInvalidIdentity(t *testing.T) {
	item, source, authority := launchFixture(t, "-option")
	action := config.QueueLaunch{Name: "agent", Argv: []string{"echo", "{checkout}"}, Cwd: t.TempDir()}
	if _, reason := PrepareLaunch(action, item, source, authority, nil); reason != "unresolved-placeholder" {
		t.Fatalf("missing checkout: %q", reason)
	}
	action.Argv = []string{"echo", "{itemId}"}
	action.Cwd = ""
	if _, reason := PrepareLaunch(action, item, source, authority, nil); reason != "unresolved-placeholder" {
		t.Fatalf("API source without cwd: %q", reason)
	}
	action.Cwd = t.TempDir()
	item.Resources = []string{"substituted-resource"}
	if _, reason := PrepareLaunch(action, item, source, authority, nil); reason != "invalid-resource" {
		t.Fatalf("forged resource: %q", reason)
	}
	item, source, authority = launchFixture(t, "-option")
	item.Ref.ItemID = "bad\noption"
	if _, reason := PrepareLaunch(action, item, source, authority, nil); reason != "invalid-resource" {
		t.Fatalf("invalid adapter ID: %q", reason)
	}
}

// The helper deliberately rejects option-looking identifiers before --.
func TestLaunchChildHelper(t *testing.T) {
	if os.Getenv("LAUNCH_HELPER") != "yes" {
		return
	}
	args := os.Args
	separator := -1
	for i, arg := range args {
		if arg == "--" {
			separator = i
			break
		}
		if strings.HasPrefix(arg, "-option") {
			os.Exit(9)
		}
	}
	if separator < 0 || separator+1 >= len(args) {
		os.Exit(10)
	}
	data, err := json.Marshal(struct{ Args, Env []string }{args[separator+1:], os.Environ()})
	if err != nil || os.WriteFile(os.Getenv("LAUNCH_CAPTURE"), data, 0600) != nil {
		os.Exit(11)
	}
}

func TestLaunchExecHandoffIsolatedEnvironment(t *testing.T) {
	id := "-option 'quoted' $(echo injected)"
	item, source, authority := launchFixture(t, id)
	capture := filepath.Join(t.TempDir(), "capture.json")
	action := config.QueueLaunch{Name: "agent", Argv: []string{os.Args[0], "-test.run=^TestLaunchChildHelper$", "--", "{itemId}", "{ref}"}, Cwd: t.TempDir(), PassEnv: []string{"LAUNCH_HELPER", "LAUNCH_CAPTURE", "GH_TOKEN", "AWS_SESSION_TOKEN"}}
	parent := []string{"PATH=/bin", "HOME=/private/home", "LANG=en_US.UTF-8", "LC_ALL=C", "XDG_CACHE_HOME=/cache", "XDG_OTHER=drop", "GH_TOKEN=explicit", "AWS_SESSION_TOKEN=explicit-temporary-credential", "GITHUB_TOKEN=secret", "CANARY_SECRET=secret", "LAUNCH_HELPER=yes", "LAUNCH_CAPTURE=" + capture, "WORKLEASE_QUEUE_RESOURCES=forged", "WORKLEASE_PROFILE=forged", "WORKLEASE_SESSION_ID=secret"}
	handoff, reason := PrepareLaunch(action, item, source, authority, parent)
	if reason != "" {
		t.Fatal(reason)
	}
	withoutCredential := action
	withoutCredential.PassEnv = []string{"LAUNCH_HELPER", "LAUNCH_CAPTURE", "GH_TOKEN"}
	isolated, disabled := PrepareLaunch(withoutCredential, item, source, authority, parent)
	if disabled != "" || strings.Contains(strings.Join(isolated.Env, "\n"), "AWS_SESSION_TOKEN") {
		t.Fatalf("unnamed credential leaked: %q %s", isolated.Env, disabled)
	}
	cmd, err := StartLaunch(context.Background(), handoff)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Args, Env []string }
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Args, []string{id, item.Ref.String()}) {
		t.Fatalf("arguments changed: %q", got.Args)
	}
	vars := map[string]string{}
	for _, entry := range got.Env {
		key, value, _ := strings.Cut(entry, "=")
		vars[key] = value
	}
	for _, name := range []string{"GITHUB_TOKEN", "CANARY_SECRET", "WORKLEASE_SESSION_ID", "XDG_OTHER"} {
		if _, ok := vars[name]; ok {
			t.Fatalf("leaked %s", name)
		}
	}
	if vars["GH_TOKEN"] != "explicit" || vars["AWS_SESSION_TOKEN"] != "explicit-temporary-credential" || vars["WORKLEASE_PROFILE"] != "team" || vars["WORKLEASE_QUEUE_AUTHORITY_ID"] != "authority" || vars["WORKLEASE_QUEUE_REF"] != item.Ref.String() || vars["WORKLEASE_QUEUE_RESOURCES"] != `["`+item.Resources[0]+`"]` {
		t.Fatalf("incorrect handoff: %v", vars)
	}
	for _, value := range append(got.Args, got.Env...) {
		if strings.Contains(value, item.Title) || strings.Contains(value, item.Body) || strings.Contains(value, "session-secret") {
			t.Fatalf("provider text or session leaked: %q", value)
		}
	}
}
