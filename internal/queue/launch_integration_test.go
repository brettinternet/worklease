package queue

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/resource"
)

func TestReferenceLauncherClaimsExactQueueHandoff(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binaryDir := t.TempDir()
	binary := filepath.Join(binaryDir, "worklease")
	build := exec.Command("go", "build", "-o", binary, "./cmd/worklease")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %s: %v", out, err)
	}
	for _, provider := range []string{"github", "generic"} {
		t.Run(provider, func(t *testing.T) {
			workDir := t.TempDir()
			home := filepath.Join(workDir, "state")
			base := []string{"PATH=" + binaryDir + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + workDir, "WORKLEASE_HOME=" + home}
			seed := exec.Command(binary, "acquire", "--resource", "coordination:generic:launcher-seed", "--session", "launcher-seed")
			seed.Dir, seed.Env = workDir, append(base, "WORKLEASE_PROFILE=local")
			if data, err := seed.CombinedOutput(); err != nil {
				t.Fatalf("seed authority: %s %v", data, err)
			}
			defer func() {
				cleanup := exec.Command(binary, "release", "--session", "launcher-seed", "--reason", "test no-effect seed")
				cleanup.Dir, cleanup.Env = workDir, append(base, "WORKLEASE_PROFILE=local")
				if data, err := cleanup.CombinedOutput(); err != nil {
					t.Errorf("seed release: %s %v", data, err)
				}
			}()
			check := exec.Command(binary, "queue", "authority-id", "--json")
			check.Dir, check.Env = workDir, append(base, "WORKLEASE_PROFILE=local")
			data, err := check.Output()
			if err != nil {
				t.Fatalf("authority: %v", err)
			}
			var resolved struct {
				AuthorityID string `json:"authorityId"`
			}
			if err := json.Unmarshal(data, &resolved); err != nil || resolved.AuthorityID == "" {
				t.Fatalf("authority response: %s %v", data, err)
			}
			inputs := resource.Input{Provider: provider, Source: "example/project", Item: "42"}
			if provider == "generic" {
				inputs.Source = "portable-backlog-binding"
			}
			key, err := resource.Resolve(inputs)
			if err != nil {
				t.Fatal(err)
			}
			resources := []string{key.Resource}
			if provider == "generic" {
				legacy, err := resource.Resolve(resource.Input{Provider: "generic", Source: "retired-backlog-binding", Item: "42"})
				if err != nil {
					t.Fatal(err)
				}
				resources = append(resources, legacy.Resource)
			}
			item := Item{Summary: Summary{Ref: Ref{SourceID: "s", ItemID: "42"}}, KeyInputs: &inputs, Resources: resources, Claim: ClaimObservation{AuthorityID: resolved.AuthorityID, Known: true, State: "free"}, Readiness: Readiness{Status: Ready}}
			source := config.QueueSource{ID: "s", Adapter: provider, Checkout: workDir}
			if provider == "generic" {
				source.Adapter = "backlog-md"
			}
			handoff, disabled := PrepareLaunch(config.QueueLaunch{Name: "reference", Argv: []string{"python3", filepath.Join(root, "scripts", "queue-launch-worker.py")}, Cwd: workDir, PassEnv: []string{"WORKLEASE_HOME"}}, item, source, ClaimAuthority{ID: resolved.AuthorityID, Profile: "local"}, base)
			if disabled != "" {
				t.Fatal(disabled)
			}
			cmd := exec.Command(handoff.Argv[0], handoff.Argv[1:]...)
			cmd.Dir, cmd.Env = handoff.Dir, handoff.Env
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("launcher: %s: %v", output, err)
			}
			var launched struct {
				AuthorityID string   `json:"authorityId"`
				Resources   []string `json:"resources"`
				SessionID   string   `json:"sessionId"`
			}
			if err := json.Unmarshal(output, &launched); err != nil || launched.AuthorityID != resolved.AuthorityID || !reflect.DeepEqual(launched.Resources, item.Resources) || launched.SessionID == "" {
				t.Fatalf("handoff mismatch: %s %v", output, err)
			}
			status := exec.Command(binary, "status", "--json", "--resource", key.Resource)
			status.Dir, status.Env = workDir, append(base, "WORKLEASE_PROFILE=local")
			statusData, err := status.Output()
			if err != nil || !strings.Contains(string(statusData), launched.SessionID) {
				t.Fatalf("worker claim absent: %s %v", statusData, err)
			}
			for _, key := range resources {
				verify := exec.Command(binary, "verify", "--session", launched.SessionID, "--resource", key)
				verify.Dir, verify.Env = workDir, append(base, "WORKLEASE_PROFILE=local")
				if data, err := verify.CombinedOutput(); err != nil {
					t.Fatalf("worker missing claim resource: %s %v", data, err)
				}
			}
			cleanup := exec.Command(binary, "release", "--session", launched.SessionID, "--reason", "test no-effect worker claim")
			cleanup.Dir, cleanup.Env = workDir, append(base, "WORKLEASE_PROFILE=local")
			if data, err := cleanup.CombinedOutput(); err != nil {
				t.Fatalf("release: %s %v", data, err)
			}
			// Starting a process that exits without acquiring does not create a
			// worker claim; only the authority's later observation can do that.
			noClaim, disabled := PrepareLaunch(config.QueueLaunch{Name: "no-claim", Argv: []string{"python3", "-c", "pass"}, Cwd: workDir}, item, source, ClaimAuthority{ID: resolved.AuthorityID, Profile: "local"}, base)
			if disabled != "" {
				t.Fatal(disabled)
			}
			child, err := StartLaunch(context.Background(), noClaim)
			if err != nil {
				t.Fatal(err)
			}
			if err := child.Wait(); err != nil {
				t.Fatal(err)
			}
			free := exec.Command(binary, "status", "--json", "--resource", key.Resource)
			free.Dir, free.Env = workDir, append(base, "WORKLEASE_PROFILE=local")
			freeData, err := free.Output()
			if err != nil {
				t.Fatal(err)
			}
			var availability struct {
				Resources []struct {
					State string `json:"state"`
				} `json:"resources"`
			}
			if err := json.Unmarshal(freeData, &availability); err != nil || len(availability.Resources) != 1 || availability.Resources[0].State != "free" {
				t.Fatalf("unclaimed launcher created worker claim: %s %v", freeData, err)
			}
			// A mismatched handoff cannot acquire; no process owns the resource.
			wrong := append([]string(nil), handoff.Env...)
			for i, entry := range wrong {
				if strings.HasPrefix(entry, "WORKLEASE_QUEUE_AUTHORITY_ID=") {
					wrong[i] = "WORKLEASE_QUEUE_AUTHORITY_ID=wrong"
				}
			}
			bad := exec.Command("python3", filepath.Join(root, "scripts", "queue-launch-worker.py"))
			bad.Dir, bad.Env = workDir, wrong
			if err := bad.Run(); err == nil {
				t.Fatal("mismatched authority was accepted")
			}
		})
	}
}
