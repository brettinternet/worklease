package cli

import (
	"os"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/runs"
)

func queueLaunchOption(action config.QueueLaunch, item queue.Item, source config.QueueSource, authority queue.ClaimAuthority, session string, previous config.QueueIdentity, env []string) (queue.LaunchOption, queue.LaunchHandoff) {
	keys, err := queue.LaunchResources(item, previous, authority)
	if err != nil {
		return queue.LaunchOption{Name: action.Name, Authority: authority.ID, Eligibility: queue.Eligibility{Reasons: []string{"invalid-resource"}, Outcome: "capability"}}, queue.LaunchHandoff{}
	}
	item.Resources = keys
	return queue.CheckLaunch(action, item, source, authority, session, env)
}

// startQueueRun hands a gated launch to a detached `worklease run`, which
// claims the exact resources for the worker, supervises it, and releases the
// claim when it exits. It returns once the worker is running or the run failed.
func startQueueRun(handoff queue.LaunchHandoff, authority queue.ClaimAuthority, home string) (runs.Record, error) {
	executable, err := os.Executable()
	if err != nil {
		return runs.Record{}, reason.New(reason.ReasonInternal, "worklease executable cannot be resolved")
	}
	profile := config.LocalProfileName
	if authority.Remote {
		profile = authority.Profile
	}
	args := []string{"--home", home, "--profile", profile, "run", "--name", handoff.Name, "--ref", handoff.Ref, "--expect-authority", authority.ID}
	for _, key := range handoff.Resources {
		args = append(args, "--resource", key)
	}
	args = append(append(args, "--"), handoff.Argv...)
	record, _, err := runs.StartDetached(executable, args, handoff.Dir, handoff.Env, runs.NewID(time.Now(), randomHex(4)))
	if err != nil {
		return runs.Record{}, err
	}
	if record.State == runs.StateFailed {
		return record, reason.New(reason.ReasonInternal, "run "+record.ID+" failed: "+record.Error)
	}
	return record, nil
}
