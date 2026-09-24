package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/resource"
)

// LaunchHandoff is a prepared process invocation. It is not evidence that a
// worker claimed the item; callers must apply the claim/authority gates before Start.
type LaunchHandoff struct {
	Argv []string
	Dir  string
	Env  []string
}

// LaunchOption contains only the public preview; environment values stay in the handoff.
type LaunchOption struct {
	Name        string      `json:"name"`
	Eligibility Eligibility `json:"eligibility"`
	Argv        []string    `json:"argv,omitempty"`
	Cwd         string      `json:"cwd,omitempty"`
	EnvNames    []string    `json:"envNames,omitempty"`
	Authority   string      `json:"authority"`
}

// LaunchResources extends the observed current key with the retired domains
// that a migrated worker must still claim atomically. Query and picker derive
// identical previews; confirmation repeats the fresh PreAcquireIdentity gate.
func LaunchResources(item Item, previous config.QueueIdentity, authority ClaimAuthority) ([]string, error) {
	if len(item.Resources) != 1 || item.KeyInputs == nil {
		return nil, fmt.Errorf("invalid-resource")
	}
	keys := append([]string(nil), item.Resources...)
	seen := map[string]bool{keys[0]: true}
	for _, domain := range previous.Retired {
		key, err := resource.Resolve(resource.Input{Provider: domain.Policy, Source: domain.Source, Item: item.Ref.ItemID})
		if err != nil {
			return nil, err
		}
		if authority.Remote && (authority.AdmittedPrefixes == nil || !lease.ResourceAdmitted(*authority.AdmittedPrefixes, key.Resource)) {
			continue // explicitly retired host-local domains cannot be claimed remotely
		}
		if !seen[key.Resource] {
			keys = append(keys, key.Resource)
			seen[key.Resource] = true
		}
	}
	if len(keys) > 32 {
		return nil, fmt.Errorf("claim domain migration exceeds atomic claim limit")
	}
	return keys, nil
}

// CheckLaunch applies the same gates to query and the interactive picker.
// The queue's own claim must be released before launching a separate worker;
// that two-step handoff has a race with other claimants.
func CheckLaunch(action config.QueueLaunch, item Item, source config.QueueSource, authority ClaimAuthority, queueSession string, parent []string) (LaunchOption, LaunchHandoff) {
	option := LaunchOption{Name: action.Name, Authority: authority.ID}
	switch {
	case item.Claim.Reason == "authority-mismatch" || item.Claim.AuthorityID != authority.ID:
		option.Eligibility = Eligibility{Reasons: []string{"authority-mismatch"}, Outcome: "capability"}
	case item.Claim.Active && queueSession != "" && item.Claim.SessionID == queueSession:
		option.Eligibility = Eligibility{Reasons: []string{"queue-holds-claim", "release or cancel, then launch; another worker may acquire in between"}, Outcome: "active-claims"}
	default:
		option.Eligibility = ClaimActions(item)[ActionLaunch]
	}
	handoff, disabled := PrepareLaunch(action, item, source, authority, parent)
	if disabled != "" && option.Eligibility.Eligible {
		option.Eligibility = Eligibility{Reasons: []string{disabled}, Outcome: "capability"}
	}
	if disabled != "" {
		return option, LaunchHandoff{}
	}
	option.Argv, option.Cwd = handoff.Argv, handoff.Dir
	for _, entry := range handoff.Env {
		name, _, _ := strings.Cut(entry, "=")
		option.EnvNames = append(option.EnvNames, name)
	}
	if !option.Eligibility.Eligible {
		return option, LaunchHandoff{}
	}
	return option, handoff
}

// PrepareLaunch uses only a validated item reference and the exact observed
// claim resources. It never copies provider content or a worker session ID.
// A non-empty reason disables the action without starting a process.
func PrepareLaunch(action config.QueueLaunch, item Item, source config.QueueSource, authority ClaimAuthority, parent []string) (LaunchHandoff, string) {
	if source.ID != item.Ref.SourceID || item.KeyInputs == nil || len(item.Resources) == 0 || authority.ID == "" || authority.Profile == "" {
		return LaunchHandoff{}, "invalid-resource"
	}
	if _, err := resource.ValidateIdentity("sourceId", item.Ref.SourceID); err != nil {
		return LaunchHandoff{}, "invalid-resource"
	}
	key, err := resource.Resolve(*item.KeyInputs)
	if err != nil || key.Item != item.Ref.ItemID || key.Resource != item.Resources[0] {
		return LaunchHandoff{}, "invalid-resource"
	}
	values := map[string]string{
		"ref": item.Ref.String(), "sourceId": item.Ref.SourceID,
		"itemId": item.Ref.ItemID, "checkout": source.Checkout,
	}
	resolve := func(template string) (string, bool) {
		if err := config.ValidateLaunchTemplate(template); err != nil {
			return "", false
		}
		for name := range values {
			if values[name] == "" && strings.Contains(template, "{"+name+"}") {
				return "", false
			}
		}
		replacements := make([]string, 0, len(values)*2)
		for _, name := range []string{"ref", "sourceId", "itemId", "checkout"} {
			replacements = append(replacements, "{"+name+"}", values[name])
		}
		return strings.NewReplacer(replacements...).Replace(template), true
	}
	if len(action.Argv) == 0 {
		return LaunchHandoff{}, "unresolved-placeholder"
	}
	handoff := LaunchHandoff{Argv: make([]string, len(action.Argv))}
	for i, arg := range action.Argv {
		value, ok := resolve(arg)
		if !ok || i == 0 && value == "" {
			return LaunchHandoff{}, "unresolved-placeholder"
		}
		handoff.Argv[i] = value
	}
	cwd := action.Cwd
	if cwd == "" {
		cwd = source.Checkout
	}
	var ok bool
	handoff.Dir, ok = resolve(cwd)
	if !ok || !filepath.IsAbs(handoff.Dir) {
		return LaunchHandoff{}, "unresolved-placeholder"
	}
	if stat, err := os.Stat(handoff.Dir); err != nil || !stat.IsDir() {
		return LaunchHandoff{}, "unresolved-placeholder"
	}
	encoded, err := json.Marshal(item.Resources)
	if err != nil {
		return LaunchHandoff{}, "invalid-resource"
	}
	allowed := map[string]bool{}
	for _, name := range action.PassEnv {
		allowed[name] = true
	}
	vars := map[string]string{}
	for _, entry := range parent {
		name, value, found := strings.Cut(entry, "=")
		if found && (launchBaseEnv(name) || allowed[name]) {
			vars[name] = value
		}
	}
	vars["WORKLEASE_PROFILE"] = authority.Profile
	vars["WORKLEASE_QUEUE_AUTHORITY_ID"] = authority.ID
	vars["WORKLEASE_QUEUE_REF"] = item.Ref.String()
	vars["WORKLEASE_QUEUE_RESOURCES"] = string(encoded)
	for name, value := range vars {
		handoff.Env = append(handoff.Env, name+"="+value)
	}
	sort.Strings(handoff.Env)
	return handoff, ""
}

func launchBaseEnv(name string) bool {
	switch name {
	case "PATH", "HOME", "USER", "LOGNAME", "SHELL", "TERM", "LANG", "TMPDIR":
		return true
	}
	return strings.HasPrefix(name, "LC_") || strings.HasPrefix(name, "XDG_") && strings.HasSuffix(name, "_HOME")
}

// StartLaunch uses exec semantics: no shell or argument re-parsing is involved.
// The child is independent of the queue's context and owns its own lifecycle.
func StartLaunch(_ context.Context, handoff LaunchHandoff) (*exec.Cmd, error) {
	if len(handoff.Argv) == 0 || handoff.Dir == "" {
		return nil, fmt.Errorf("launch handoff is incomplete")
	}
	cmd := exec.Command(handoff.Argv[0], handoff.Argv[1:]...)
	cmd.Dir, cmd.Env = handoff.Dir, handoff.Env
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}
