package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/resource"
	"github.com/brettinternet/worklease/internal/runs"
)

func TestQueueLaunchRunsWorkerUnderSupervisedClaim(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binaryDir := t.TempDir()
	binary := filepath.Join(binaryDir, "worklease")
	// Re-execute the current test binary as the CLI without a compile in the test.
	if err := os.Symlink(os.Args[0], binary); err != nil {
		t.Fatal(err)
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
			item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "s", ItemID: "42"}}, KeyInputs: &inputs, Resources: resources, Claim: queue.ClaimObservation{AuthorityID: resolved.AuthorityID, Known: true, State: "free"}, Readiness: queue.Readiness{Status: queue.Ready}}
			source := config.QueueSource{ID: "s", Adapter: provider, Checkout: workDir}
			if provider == "generic" {
				source.Adapter = "backlog-md"
			}
			action := config.QueueLaunch{Name: "reference", Argv: []string{"python3", filepath.Join(root, "scripts", "queue-launch-worker.py")}, Cwd: workDir}
			authority := queue.ClaimAuthority{ID: resolved.AuthorityID, Profile: "local"}
			handoff, disabled := queue.PrepareLaunch(action, item, source, authority, base)
			if disabled != "" {
				t.Fatal(disabled)
			}
			started, err := startQueueRun(handoff, authority, home)
			if err != nil {
				t.Fatalf("start run: %+v %v", started, err)
			}
			if started.Claim == nil || started.Claim.AuthorityID != resolved.AuthorityID || len(started.Claim.Resources) != len(resources) {
				t.Fatalf("run did not claim the handoff: %+v", started.Claim)
			}
			wait := exec.Command(binary, "--json", "runs", "wait", started.ID, "--timeout", "1m")
			wait.Dir, wait.Env = workDir, base
			data, err = wait.Output()
			if err != nil {
				t.Fatalf("wait: %s %v", data, err)
			}
			var finished struct {
				Run runs.Record `json:"run"`
			}
			if err := json.Unmarshal(data, &finished); err != nil {
				t.Fatal(err)
			}
			record := finished.Run
			if record.ExitCode == nil || *record.ExitCode != 0 || record.Result == nil || record.Result.Outcome != "done" || record.Claim == nil || record.Claim.State != runs.ClaimReleased {
				t.Fatalf("worker did not verify the inherited claim: %s", data)
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
				t.Fatalf("run did not release the claim: %s %v", freeData, err)
			}
			// A handoff for another authority is refused before any claim.
			if record, err := startQueueRun(handoff, queue.ClaimAuthority{ID: "wrong", Profile: "local"}, home); err == nil || record.Claim != nil {
				t.Fatalf("mismatched authority was accepted: %+v %v", record, err)
			}
			// The reference worker refuses to run outside a supervised run.
			bare := exec.Command(handoff.Argv[0], handoff.Argv[1:]...)
			bare.Dir, bare.Env = handoff.Dir, handoff.Env
			if err := bare.Run(); err == nil {
				t.Fatal("reference worker ran without an inherited claim")
			}
		})
	}
}
