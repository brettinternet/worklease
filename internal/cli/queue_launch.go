package cli

import (
	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
)

func queueLaunchOption(action config.QueueLaunch, item queue.Item, source config.QueueSource, authority queue.ClaimAuthority, session string, previous config.QueueIdentity, env []string) (queue.LaunchOption, queue.LaunchHandoff) {
	keys, err := queue.LaunchResources(item, previous, authority)
	if err != nil {
		return queue.LaunchOption{Name: action.Name, Authority: authority.ID, Eligibility: queue.Eligibility{Reasons: []string{"invalid-resource"}, Outcome: "capability"}}, queue.LaunchHandoff{}
	}
	item.Resources = keys
	return queue.CheckLaunch(action, item, source, authority, session, env)
}
