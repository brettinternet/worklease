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
	"github.com/brettinternet/worklease/internal/resource"
)

// LaunchHandoff is a prepared process invocation. It is not evidence that a
// worker claimed the item; callers must apply the claim/authority gates before Start.
type LaunchHandoff struct {
	Argv []string
	Dir  string
	Env  []string
}

// PrepareLaunch uses only a validated item reference and the exact observed
// claim resources. It never copies provider content or a worker session ID.
// A non-empty reason disables the action without starting a process.
func PrepareLaunch(action config.QueueLaunch, item Item, source config.QueueSource, authority ClaimAuthority, parent []string) (LaunchHandoff, string) {
	if source.ID != item.Ref.SourceID || item.KeyInputs == nil || len(item.Resources) == 0 || item.Claim.AuthorityID != authority.ID || authority.ID == "" || authority.Profile == "" {
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
func StartLaunch(ctx context.Context, handoff LaunchHandoff) (*exec.Cmd, error) {
	if len(handoff.Argv) == 0 || handoff.Dir == "" {
		return nil, fmt.Errorf("launch handoff is incomplete")
	}
	cmd := exec.CommandContext(ctx, handoff.Argv[0], handoff.Argv[1:]...)
	cmd.Dir, cmd.Env = handoff.Dir, handoff.Env
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}
