package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
	urfave "github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

type queueInitFact struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Origin string `json:"origin"`
}

type queueInitResult struct {
	Path         string          `json:"path"`
	Outcome      string          `json:"outcome"`
	SourceID     string          `json:"sourceId"`
	Facts        []queueInitFact `json:"facts"`
	YAML         string          `json:"yaml"`
	Applied      bool            `json:"applied"`
	Identity     string          `json:"identity"`
	Checklist    string          `json:"checklist,omitempty"`
	Unmapped     []string        `json:"unmapped,omitempty"`
	NextCommands []string        `json:"nextCommands"`
}

func queueInitCommand(s *boundary) *urfave.Command {
	return &urfave.Command{Name: "init", Usage: "preview or create owner-private queue.yaml from a checkout",
		UsageText:   "worklease queue [--view NAME] init [--checkout PATH] [--adapter backlog-md|github] [--source-id ID] [--authority NAME] [--portable-claims SOURCE] [--me PRINCIPAL] [--apply] [--json]",
		Description: "Detect provider facts on explicit invocation; preview the exact YAML without writing. Re-run to add another checkout.\n\nExamples:\n  worklease queue init\n  worklease queue init --apply\n  worklease queue --view Team init --checkout /path/to/repo --authority shared --apply",
		Flags: []urfave.Flag{
			&urfave.StringFlag{Name: "checkout", Usage: "git checkout `PATH` (default: current checkout)"},
			&urfave.StringFlag{Name: "adapter", Usage: "source adapter: backlog-md or github"},
			&urfave.StringFlag{Name: "source-id", Usage: "new source `ID`"},
			&urfave.StringFlag{Name: "authority", Value: "local", Usage: "trusted authority `NAME`"},
			&urfave.StringFlag{Name: "portable-claims", Usage: "generic cross-host claim source `SOURCE` (Backlog.md only)"},
			&urfave.StringFlag{Name: "me", Usage: "provider principal `PRINCIPAL`"},
			&urfave.BoolFlag{Name: "apply", Usage: "write queue.yaml and confirm a safe initial local identity"},
		},
		Action: func(ctx context.Context, cmd *urfave.Command) error {
			result, err := prepareQueueInit(ctx, cmd)
			if err != nil {
				return s.handle(cmd, err)
			}
			if cmd.Bool("apply") && result.Outcome != "unchanged" {
				if result.Identity == "me-required" {
					return s.handle(cmd, reason.New(reason.ReasonMeRequired, "--me @name is required before applying queue.yaml"))
				}
				if err := handle.EnsureOwnerPrivateDir(filepath.Dir(result.Path)); err != nil {
					return s.handle(cmd, err)
				}
				lock, err := handle.AcquireLock(ctx, result.Path+".lock")
				if err != nil {
					return s.handle(cmd, err)
				}
				defer lock.Close()
				// Re-read under the writer lock: concurrent init calls must merge, not overwrite.
				result, err = prepareQueueInit(ctx, cmd)
				if err != nil {
					return s.handle(cmd, err)
				}
				if result.Identity == "me-required" {
					return s.handle(cmd, reason.New(reason.ReasonMeRequired, "--me @name is required before applying queue.yaml"))
				}
				if result.Outcome != "unchanged" {
					if err := handle.WriteOwnerPrivate(result.Path, []byte(result.YAML), 1<<20); err != nil {
						return s.handle(cmd, err)
					}
					result.Applied = true
				}
				if result.Applied && result.Identity == "automatic" {
					view := cmd.String("view")
					if view == "" {
						view = "Ready"
					}
					if err := confirmQueueIdentity(ctx, cmd, view, result.SourceID); err != nil {
						result.Identity = "confirmation-failed"
						if reason.As(err) == nil {
							if strings.HasPrefix(err.Error(), reason.ReasonBindingMigrationRequired+":") {
								err = reason.New(reason.ReasonBindingMigrationRequired, err.Error())
							} else {
								err = reason.New(reason.ReasonConfigInvalid, err.Error())
							}
						}
						classified := reason.As(err)
						classified.With("applied", true).With("nextCommands", result.NextCommands).With("checklist", queue.MigrationChecklist)
						if !s.jsonRequested(cmd) {
							fmt.Fprintf(s.writer, "queue.yaml written; identity confirmation failed: %v\n%s\n", err, strings.Join(result.NextCommands, "\n"))
						}
						return s.handle(cmd, err)
					}
					result.Identity = "confirmed"
					result.Checklist = ""
					result.NextCommands = []string{"worklease queue --view " + queueInitQuote(view)}
				}
			}
			if s.jsonRequested(cmd) {
				return output.WriteSuccess(s.writer, "queue-init", map[string]any{"path": result.Path, "outcome": result.Outcome, "sourceId": result.SourceID, "facts": result.Facts, "yaml": result.YAML, "applied": result.Applied, "identity": result.Identity, "unmapped": result.Unmapped, "checklist": result.Checklist, "nextCommands": result.NextCommands})
			}
			fmt.Fprintf(s.writer, "Path: %s\nOutcome: %s\n", result.Path, result.Outcome)
			for _, fact := range result.Facts {
				fmt.Fprintf(s.writer, "%s: %s (from %s)\n", fact.Name, fact.Value, fact.Origin)
			}
			if len(result.Unmapped) > 0 {
				fmt.Fprintf(s.writer, "Unmapped intents: %s\n", strings.Join(result.Unmapped, ", "))
			}
			fmt.Fprintf(s.writer, "Identity: %s\nYAML:\n%s", result.Identity, result.YAML)
			if result.Checklist != "" {
				fmt.Fprintf(s.writer, "Migration checklist: %s\n", result.Checklist)
			}
			fmt.Fprintln(s.writer, strings.Join(result.NextCommands, "\n"))
			return nil
		},
	}
}

func queueInitCommandOutput(ctx context.Context, checkout string, args ...string) (string, error) {
	probe, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(probe, args[0], args[1:]...)
	command.Dir = checkout
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name == "BACKLOG_CWD" || strings.HasPrefix(name, "GIT_") {
			continue
		}
		switch name {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN":
			if args[0] == "gh" {
				continue
			}
		}
		command.Env = append(command.Env, entry)
	}
	data, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func queueInitQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func queueInitApplyCommand(cmd *urfave.Command, checkout, adapter, id, me string) string {
	command := "worklease queue"
	if view := cmd.String("view"); view != "" && view != "Ready" {
		command += " --view " + queueInitQuote(view)
	}
	command += " init --checkout " + queueInitQuote(checkout) + " --adapter " + queueInitQuote(adapter) + " --source-id " + queueInitQuote(id)
	if authority := cmd.String("authority"); authority != "local" {
		command += " --authority " + queueInitQuote(authority)
	}
	if claims := cmd.String("portable-claims"); claims != "" {
		command += " --portable-claims " + queueInitQuote(claims)
	}
	if me != "" {
		command += " --me " + queueInitQuote(me)
	}
	return command + " --apply"
}

func queueInitRemote(remote string) (string, string, bool) {
	var host, repository string
	if strings.HasPrefix(remote, "git@") {
		address := strings.TrimPrefix(remote, "git@")
		host, repository, _ = strings.Cut(address, ":")
	} else if u, err := url.Parse(remote); err == nil && (u.Scheme == "https" || u.Scheme == "ssh") {
		host, repository = u.Hostname(), strings.TrimPrefix(u.Path, "/")
	}
	host = strings.ToLower(host)
	repository = strings.TrimSuffix(repository, ".git")
	parts := strings.Split(repository, "/")
	if host == "" || strings.ContainsAny(host, "/\\ :@?#\t\r\n") || len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(repository, " \\:@?#\t\r\n") {
		return "", "", false
	}
	return host, repository, true
}

func initNode(value any) (*yaml.Node, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc.Content[0], nil
}

func initField(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func initSet(mapping *yaml.Node, key string, value *yaml.Node) {
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func prepareQueueInit(ctx context.Context, cmd *urfave.Command) (queueInitResult, error) {
	result := queueInitResult{Path: config.QueuePath(os.Getenv), Facts: []queueInitFact{}}
	if !filepath.IsAbs(result.Path) {
		return result, reason.Invalid("queue.yaml path must be absolute")
	}
	checkout := cmd.String("checkout")
	if checkout == "" {
		checkout = "."
	}
	root, err := queueInitCommandOutput(ctx, checkout, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return result, reason.Invalid("--checkout must be a git checkout")
	}
	checkout, err = filepath.EvalSymlinks(root)
	if err != nil {
		return result, reason.Invalid("--checkout must resolve to an existing git checkout")
	}
	result.Facts = append(result.Facts, queueInitFact{"checkout", checkout, "git rev-parse --show-toplevel"})
	view := cmd.String("view")
	if view == "" {
		view = "Ready"
	}
	authority := cmd.String("authority")
	profiles, _, err := config.LoadProfiles(config.ProfilePaths{Profiles: filepath.Join(filepath.Dir(result.Path), "profiles.yaml")})
	if err != nil {
		return result, err
	}
	if authority != "local" {
		if _, ok := profiles[authority]; !ok {
			return result, reason.Invalid("unknown authority profile: " + authority)
		}
	}
	data, err := handle.ReadOwnerPrivate(result.Path, 1<<20)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, reason.New(reason.ReasonConfigInvalid, "queue.yaml cannot be read safely: "+err.Error())
	}
	existing := err == nil
	var cfg config.QueueConfig
	var doc yaml.Node
	if existing {
		cfg, err = config.LoadQueue(os.Getenv)
		if err != nil {
			return result, err
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return result, reason.New(reason.ReasonConfigInvalid, err.Error())
		}
	} else {
		if err := yaml.Unmarshal([]byte("version: 1\nme: {}\nsources: []\nviews: []\n"), &doc); err != nil {
			return result, err
		}
	}
	mapping := doc.Content[0]
	remote, remoteErr := queueInitCommandOutput(ctx, checkout, "git", "remote", "get-url", "origin")
	host, repository, github := queueInitRemote(remote)
	backlogDir, backlogErr := queue.BacklogDirectory(checkout)
	adapter := cmd.String("adapter")
	if adapter == "" {
		switch {
		case backlogErr == nil:
			adapter = "backlog-md"
		case remoteErr == nil && github && (host == "github.com" || strings.HasPrefix(host, "github.")):
			adapter = "github"
		default:
			return result, reason.Invalid("cannot detect source; specify --adapter backlog-md|github")
		}
	}
	if adapter != "backlog-md" && adapter != "github" {
		return result, reason.Invalid("--adapter must be backlog-md or github")
	}
	if adapter == "backlog-md" && backlogErr != nil {
		return result, reason.Invalid("--checkout has no Backlog.md project")
	}
	if adapter == "github" && !github {
		return result, reason.Invalid("--checkout origin is not a GitHub remote")
	}
	if adapter == "github" && cmd.String("portable-claims") != "" {
		return result, reason.Invalid("--portable-claims requires backlog-md")
	}
	result.Facts = append(result.Facts, queueInitFact{"adapter", adapter, "Backlog.md project / git origin"}, queueInitFact{"view", view, "queue --view or Ready default"}, queueInitFact{"authority", authority, "--authority or local default"})
	if adapter == "backlog-md" {
		result.Facts = append(result.Facts, queueInitFact{"backlogDirectory", backlogDir, "queue.BacklogDirectory"})
	} else {
		result.Facts = append(result.Facts, queueInitFact{"host", host, "git origin"}, queueInitFact{"repository", repository, "git origin"})
	}
	for _, source := range cfg.Sources {
		resolvedCheckout, _ := filepath.EvalSymlinks(source.Checkout)
		if adapter == "backlog-md" && source.Adapter == adapter && resolvedCheckout == checkout || adapter == "github" && source.Adapter == adapter && strings.EqualFold(source.Host, host) && strings.EqualFold(source.Repository, repository) {
			if me := cmd.String("me"); me != "" {
				key := "backlog-md"
				if adapter == "github" {
					key = host
				}
				configured := cfg.Me[key]
				if adapter == "backlog-md" {
					var names []string
					_ = configured.Decode(&names)
					if !slices.Contains(names, me) {
						return result, reason.Invalid("--me conflicts with existing me.backlog-md")
					}
				} else if configured.Value != me || source.Account != me {
					return result, reason.Invalid("--me conflicts with existing GitHub account")
				}
			}
			if id := cmd.String("source-id"); id != "" && id != source.ID {
				return result, reason.Invalid("checkout already configured as source " + source.ID)
			}
			result.SourceID, result.Outcome = source.ID, "unchanged"
			result.YAML = string(data)
			result.Identity = "unchanged"
			result.NextCommands = []string{"worklease queue --view " + queueInitQuote(view)}
			return result, nil
		}
	}
	id := cmd.String("source-id")
	if id == "" {
		id = strings.ReplaceAll(filepath.Base(checkout), ":", "")
		base := id
		for n := 2; slices.ContainsFunc(cfg.Sources, func(s config.QueueSource) bool { return s.ID == id }); n++ {
			id = base + "-" + strconv.Itoa(n)
		}
	}
	if id == "" || strings.Contains(id, ":") {
		return result, reason.Invalid("--source-id must be nonempty and contain no colon")
	}
	for _, source := range cfg.Sources {
		if source.ID == id {
			return result, reason.Invalid("source ID already configured: " + id)
		}
	}
	result.SourceID = id
	result.Facts = append(result.Facts, queueInitFact{"sourceId", id, "--source-id or checkout basename (unique suffix)"})
	result.Outcome = "created"
	if existing {
		result.Outcome = "merged"
	}
	var selected *yaml.Node
	for i, v := range cfg.Views {
		if v.Name == view {
			selected = initField(initField(mapping, "views").Content[i], "sources")
			if v.Authority != authority {
				return result, reason.Invalid("view authority differs from --authority")
			}
			break
		}
	}
	if selected == nil {
		viewNode, err := initNode(config.QueueView{Name: view, Authority: authority, Sources: []string{id}, Filter: config.QueueFilter{Readiness: "ready", Claim: "free", Assigned: []string{"me", "nobody"}}})
		if err != nil {
			return result, err
		}
		initField(mapping, "views").Content = append(initField(mapping, "views").Content, viewNode)
	} else {
		item, _ := initNode(id)
		selected.Content = append(selected.Content, item)
	}
	source := config.QueueSource{ID: id, Adapter: adapter, AllowGitNetwork: false}
	me := cmd.String("me")
	meNode := initField(mapping, "me")
	if adapter == "backlog-md" {
		source.Checkout = checkout
		if claim := cmd.String("portable-claims"); claim != "" {
			source.Claims = &config.QueueClaims{Policy: "generic", Source: claim}
		}
		statuses, e := queueInitCommandOutput(ctx, checkout, "backlog", "config", "get", "statuses")
		if e != nil {
			return result, reason.New(reason.ReasonConfigInvalid, "cannot read Backlog.md statuses")
		}
		defaultStatus, e := queueInitCommandOutput(ctx, checkout, "backlog", "config", "get", "defaultStatus")
		if e != nil {
			return result, reason.New(reason.ReasonConfigInvalid, "cannot read Backlog.md default status")
		}
		assignee, e := queueInitCommandOutput(ctx, checkout, "backlog", "config", "get", "defaultAssignee")
		if e != nil {
			return result, reason.New(reason.ReasonConfigInvalid, "cannot read Backlog.md default assignee")
		}
		result.Facts = append(result.Facts, queueInitFact{"statuses", statuses, "backlog config get statuses"}, queueInitFact{"defaultStatus", defaultStatus, "backlog config get defaultStatus"}, queueInitFact{"defaultAssignee", assignee, "backlog config get defaultAssignee"})
		source.Workflow = map[string]string{}
		available := strings.Split(statuses, ",")
		for i := range available {
			available[i] = strings.TrimSpace(available[i])
		}
		for _, pair := range []struct{ intent, status string }{{"start", "In Progress"}, {"complete", "Done"}, {"reopen", defaultStatus}} {
			if slices.Contains(available, pair.status) && pair.status != "" {
				source.Workflow[pair.intent] = pair.status
			} else {
				result.Unmapped = append(result.Unmapped, pair.intent)
			}
		}
		result.Unmapped = append(result.Unmapped, "blocked", "review")
		if meNodeExisting := initField(meNode, "backlog-md"); meNodeExisting != nil {
			var existingMe []string
			if err := meNodeExisting.Decode(&existingMe); err != nil {
				return result, reason.New(reason.ReasonConfigInvalid, "invalid me.backlog-md")
			}
			if me != "" && !slices.Contains(existingMe, me) {
				return result, reason.Invalid("--me conflicts with existing me.backlog-md")
			}
		} else {
			if me == "" {
				candidates := strings.Split(assignee, ",")
				if len(candidates) == 1 {
					me = strings.TrimSpace(candidates[0])
				}
			}
			if me == "" {
				result.Identity = "me-required"
			} else {
				if !strings.HasPrefix(me, "@") || len(me) < 2 || strings.ContainsAny(me, " ,\t\n") {
					return result, reason.Invalid("--me must be one @name")
				}
				value, _ := initNode([]string{me})
				initSet(meNode, "backlog-md", value)
			}
		}
	} else {
		source.Host, source.Repository = host, repository
		account, e := queueInitCommandOutput(ctx, checkout, "gh", "api", "--hostname", host, "user", "--jq", ".login")
		if e != nil || account == "" {
			return result, reason.New(reason.ReasonAuthenticationRequired, "authenticate with gh auth login --hostname "+host)
		}
		result.Facts = append(result.Facts, queueInitFact{"account", account, "gh api --hostname " + host + " user"})
		if me != "" && me != account {
			return result, reason.Invalid("--me conflicts with authenticated GitHub account")
		}
		source.Account = account
		source.Workflow = map[string]string{"complete": "closed", "reopen": "open"}
		if node := initField(meNode, host); node != nil {
			if node.Value != account {
				return result, reason.Invalid("authenticated account conflicts with existing me." + host)
			}
		} else {
			value, _ := initNode(account)
			initSet(meNode, host, value)
		}
		result.Unmapped = []string{"start", "blocked", "review"}
	}
	sourceNode, err := initNode(source)
	if err != nil {
		return result, err
	}
	// Omit adapter-specific zero fields; an explicit false documents the network boundary.
	sourceNode.Content = nil
	for _, field := range []struct {
		key   string
		value any
	}{{"id", id}, {"adapter", adapter}} {
		node, _ := initNode(field.value)
		initSet(sourceNode, field.key, node)
	}
	if adapter == "backlog-md" {
		node, _ := initNode(checkout)
		initSet(sourceNode, "checkout", node)
		if source.Claims != nil {
			node, _ = initNode(source.Claims)
			initSet(sourceNode, "claims", node)
		}
	} else {
		for _, field := range []struct{ key, value string }{{"host", host}, {"repository", repository}, {"account", source.Account}} {
			node, _ := initNode(field.value)
			initSet(sourceNode, field.key, node)
		}
	}
	if len(source.Workflow) > 0 {
		node, _ := initNode(source.Workflow)
		initSet(sourceNode, "workflow", node)
	}
	node, _ := initNode(false)
	initSet(sourceNode, "allowGitNetwork", node)
	initField(mapping, "sources").Content = append(initField(mapping, "sources").Content, sourceNode)
	var rendered bytes.Buffer
	encoder := yaml.NewEncoder(&rendered)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return result, err
	}
	if err := encoder.Close(); err != nil {
		return result, err
	}
	result.YAML = rendered.String()
	if result.Identity != "me-required" {
		if _, err := config.ValidateQueue(rendered.Bytes(), os.Getenv); err != nil {
			return result, reason.New(reason.ReasonConfigInvalid, err.Error())
		}
	}
	result.NextCommands = []string{queueInitApplyCommand(cmd, checkout, adapter, id, me)}
	if result.Identity == "me-required" {
		result.NextCommands = []string{queueInitApplyCommand(cmd, checkout, adapter, id, "@name")}
	}
	if cmd.Bool("apply") && result.Identity != "me-required" {
		identities, err := config.LoadQueueIdentities(os.Getenv)
		if err != nil {
			return result, err
		}
		_, existingIdentity := identities.Sources[id]
		if source.Claims == nil && authority == "local" && !existingIdentity {
			result.Identity = "automatic"
		} else {
			result.Identity = "confirmation-required"
		}
		result.NextCommands = []string{fmt.Sprintf("worklease queue --view %s identity confirm --source %s --acknowledge", queueInitQuote(view), queueInitQuote(id))}
		result.Checklist = queue.MigrationChecklist
	} else if result.Identity == "" {
		result.Identity = "pending"
	}
	return result, nil
}
