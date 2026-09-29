package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	osuser "os/user"
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

type queueInitSourceResult struct {
	ID       string          `json:"id"`
	Adapter  string          `json:"adapter"`
	Outcome  string          `json:"outcome"`
	Identity string          `json:"identity"`
	Facts    []queueInitFact `json:"facts"`
	Reports  []string        `json:"reports,omitempty"`
}

type queueInitResult struct {
	Sources          []queueInitSourceResult `json:"sources,omitempty"`
	Reports          []string                `json:"reports,omitempty"`
	Path             string                  `json:"path"`
	ExecutableSHA256 string                  `json:"executableSHA256,omitempty"`
	Outcome          string                  `json:"outcome"`
	SourceID         string                  `json:"sourceId"`
	Adapter          string                  `json:"-"`
	Me               string                  `json:"-"`
	Mapped           []string                `json:"-"`
	DefaultView      bool                    `json:"-"`
	Facts            []queueInitFact         `json:"facts"`
	YAML             string                  `json:"yaml"`
	Applied          bool                    `json:"applied"`
	Identity         string                  `json:"identity"`
	Checklist        string                  `json:"checklist,omitempty"`
	Unmapped         []string                `json:"unmapped,omitempty"`
	NextCommands     []string                `json:"nextCommands"`
}

func queueInitCommand(s *boundary) *urfave.Command {
	return &urfave.Command{Name: "init", Usage: "create owner-private queue.yaml from a checkout or external adapter manifest",
		UsageText:   "worklease queue [--view NAME] init [--checkout PATH] [--adapter backlog-md|github|external] [--executable PATH] [--adapter-config JSON | --adapter-config-file FILE] [--source-id ID] [--authority NAME] [--portable-claims SOURCE] [--me PRINCIPAL] [--allow-git-network] [--ignore-proposal] [--enroll-contract] [--dry-run] [--json]",
		Description: "Detect provider facts and write queue.yaml directly. Use --dry-run to preview without writing. Re-run to add another checkout. Use --enroll-contract only after reviewing the committed source contract; --dry-run previews the owner-private enrollment.\n\nExamples:\n  worklease queue init\n  worklease queue init --dry-run\n  worklease queue init --enroll-contract --dry-run\n  worklease queue --view Team init --authority shared --allow-git-network",
		OnUsageError: func(_ context.Context, cmd *urfave.Command, _ error, _ bool) error {
			return queueInitError(s, cmd, reason.Invalid("invalid command-line arguments"))
		},
		Flags: []urfave.Flag{
			&urfave.StringFlag{Name: "checkout", Usage: "git checkout `PATH` (default: current checkout)"},
			&urfave.StringFlag{Name: "adapter", Usage: "source adapter: backlog-md, github or external"},
			&urfave.StringFlag{Name: "executable", Usage: "absolute canonical external adapter executable `PATH`"},
			&urfave.StringFlag{Name: "adapter-config", Usage: "external adapter configuration as a JSON object"},
			&urfave.StringFlag{Name: "adapter-config-file", Usage: "path to external adapter JSON configuration object"},
			&urfave.StringFlag{Name: "source-id", Usage: "new source `ID`"},
			&urfave.StringFlag{Name: "authority", Value: "local", Usage: "trusted authority `NAME`"},
			&urfave.StringFlag{Name: "portable-claims", Usage: "explicit generic cross-host claim source `SOURCE` (Backlog.md or external)"},
			&urfave.StringFlag{Name: "me", Usage: "provider principal `PRINCIPAL`"},
			&urfave.BoolFlag{Name: "dry-run", Usage: "preview detected facts and exact YAML without writing"},
			&urfave.BoolFlag{Name: "ignore-proposal", Usage: "ignore checkout queue-sources.yaml and detect the provider"},
			&urfave.BoolFlag{Name: "enroll-contract", Usage: "explicitly use the reviewed checkout source contract at runtime"},
			&urfave.BoolFlag{Name: "allow-git-network", Usage: "consent to Backlog.md project Git network effects"},
		},
		Action: func(ctx context.Context, cmd *urfave.Command) error {
			result, err := prepareQueueInit(ctx, cmd)
			if err != nil {
				return queueInitError(s, cmd, err)
			}
			if !cmd.Bool("dry-run") && result.Outcome != "unchanged" {
				if err := handle.EnsureOwnerPrivateDir(filepath.Dir(result.Path)); err != nil {
					return queueInitError(s, cmd, err)
				}
				lock, err := handle.AcquireLock(ctx, result.Path+".lock")
				if err != nil {
					return queueInitError(s, cmd, err)
				}
				defer lock.Close()
				// Re-read under the writer lock: concurrent init calls must merge, not overwrite.
				result, err = prepareQueueInit(ctx, cmd)
				if err != nil {
					return queueInitError(s, cmd, err)
				}
				if result.Outcome != "unchanged" {
					if err := handle.WriteOwnerPrivate(result.Path, []byte(result.YAML), 1<<20); err != nil {
						return queueInitError(s, cmd, err)
					}
					result.Applied = true
				}
				if result.Applied {
					toConfirm := []string{}
					if result.Identity == "automatic" {
						toConfirm = append(toConfirm, result.SourceID)
					}
					if len(result.Sources) > 0 {
						toConfirm = nil
						for _, source := range result.Sources {
							if source.Identity == "automatic" {
								toConfirm = append(toConfirm, source.ID)
							}
						}
					}
					view := cmd.String("view")
					if view == "" {
						view = "Ready"
					}
					for _, sourceID := range toConfirm {
						if err := confirmQueueIdentity(ctx, cmd, view, sourceID); err != nil {
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
							return queueInitError(s, cmd, err)
						}
					}
					if len(toConfirm) > 0 {
						result.Identity = "confirmed"
						for i := range result.Sources {
							if slices.Contains(toConfirm, result.Sources[i].ID) {
								result.Sources[i].Identity = "confirmed"
							}
						}
						if slices.ContainsFunc(result.Sources, func(source queueInitSourceResult) bool { return source.Identity == "confirmation-required" }) {
							result.Identity = "confirmation-required"
						} else {
							result.Checklist = ""
						}
						result.NextCommands = slices.DeleteFunc(result.NextCommands, func(command string) bool {
							return slices.ContainsFunc(toConfirm, func(id string) bool {
								return strings.Contains(command, " identity confirm --source "+queueInitQuote(id)+" --acknowledge")
							})
						})
						if result.Checklist == "" {
							result.NextCommands = append(result.NextCommands, queueInitQueueCommand(view, result.DefaultView))
						}
					}
				}
			}
			if s.jsonRequested(cmd) {
				return output.WriteSuccess(s.writer, "queue-init", map[string]any{"path": result.Path, "outcome": result.Outcome, "sourceId": result.SourceID, "executableSHA256": result.ExecutableSHA256, "facts": result.Facts, "yaml": result.YAML, "applied": result.Applied, "identity": result.Identity, "unmapped": result.Unmapped, "checklist": result.Checklist, "nextCommands": result.NextCommands, "sources": result.Sources, "reports": result.Reports})
			}
			if cmd.Bool("dry-run") {
				for _, fact := range result.Facts {
					fmt.Fprintf(s.writer, "%s: %s (from %s)\n", fact.Name, fact.Value, fact.Origin)
				}
				fmt.Fprintf(s.writer, "YAML:\n%s", result.YAML)
			} else {
				fmt.Fprintf(s.writer, "Path: %s\nOutcome: %s\nSource: %s (%s)\nMe: %s\nMapped intents: %s\nUnmapped intents: %s\nIdentity: %s\n", result.Path, result.Outcome, result.SourceID, result.Adapter, result.Me, strings.Join(result.Mapped, ", "), strings.Join(result.Unmapped, ", "), result.Identity)
				if result.ExecutableSHA256 != "" {
					fmt.Fprintf(s.writer, "Executable SHA-256: %s\n", result.ExecutableSHA256)
				}
			}
			for _, report := range result.Reports {
				fmt.Fprintln(s.writer, report)
			}
			if result.Checklist != "" {
				fmt.Fprintf(s.writer, "Migration checklist: %s\n", result.Checklist)
			}
			fmt.Fprintln(s.writer, strings.Join(result.NextCommands, "\n"))
			return nil
		},
	}
}

func queueInitError(s *boundary, cmd *urfave.Command, err error) error {
	if !s.jsonRequested(cmd) {
		return err
	}
	if writeErr := output.WriteError(s.writer, "queue-init", err); writeErr != nil {
		return writeErr
	}
	return &handledError{cause: err}
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
	if value != "" && !strings.ContainsAny(value, " \t\n\r'\"\\$`;&|()<>*?[]{}!#") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func queueInitQueueCommand(view string, isDefault bool) string {
	if isDefault {
		return "worklease queue"
	}
	return "worklease queue --view " + queueInitQuote(view)
}

func queueInitCheckoutArg(ctx context.Context, cmd *urfave.Command, checkout string) string {
	if cmd.String("checkout") == "" {
		return ""
	}
	current, err := queueInitCommandOutput(ctx, ".", "git", "rev-parse", "--show-toplevel")
	if err == nil {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil && resolved == checkout {
			return ""
		}
	}
	return " --checkout " + queueInitQuote(checkout)
}

func queueInitApplyCommand(ctx context.Context, cmd *urfave.Command, checkout, adapter, detectedAdapter, id, defaultID, me, defaultMe string) string {
	command := "worklease queue"
	if view := cmd.String("view"); view != "" {
		command += " --view " + queueInitQuote(view)
	}
	command += " init"
	command += queueInitCheckoutArg(ctx, cmd, checkout)
	if adapter != detectedAdapter {
		command += " --adapter " + queueInitQuote(adapter)
	}
	if id != defaultID {
		command += " --source-id " + queueInitQuote(id)
	}
	if authority := cmd.String("authority"); authority != "local" {
		command += " --authority " + queueInitQuote(authority)
	}
	if claims := cmd.String("portable-claims"); claims != "" {
		command += " --portable-claims " + queueInitQuote(claims)
	}
	if cmd.String("me") != "" && me != "" && me != defaultMe {
		command += " --me " + queueInitQuote(me)
	}
	if cmd.Bool("allow-git-network") {
		command += " --allow-git-network"
	}
	if cmd.Bool("ignore-proposal") {
		command += " --ignore-proposal"
	}
	if cmd.Bool("enroll-contract") {
		command += " --enroll-contract"
	}
	return command
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
	initBlockStyle(doc.Content[0])
	return doc.Content[0], nil
}

func initBlockStyle(node *yaml.Node) {
	node.Style &^= yaml.FlowStyle
	for _, child := range node.Content {
		initBlockStyle(child)
	}
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

func queueInitSetContractCheckout(mapping *yaml.Node, sourceID, checkout string) error {
	sources := initField(mapping, "sources")
	if sources == nil || sources.Kind != yaml.SequenceNode {
		return reason.New(reason.ReasonConfigInvalid, "queue sources are unavailable")
	}
	for _, source := range sources.Content {
		id := initField(source, "id")
		if id == nil || id.Value != sourceID {
			continue
		}
		value, err := initNode(checkout)
		if err != nil {
			return err
		}
		if field := initField(source, "contractCheckout"); field == nil {
			initSet(source, "contractCheckout", value)
		} else {
			field.Value = checkout
			field.Tag = "!!str"
		}
		return nil
	}
	return reason.New(reason.ReasonConfigInvalid, "queue source could not be enrolled")
}

var queueInitGitHubAdapter = queue.NewGitHubAdapter

func queueInitPreflight(ctx context.Context, source config.QueueSource) error {
	options := map[string]string{"id": source.ID, "checkout": source.Checkout, "host": source.Host, "repository": source.Repository, "account": source.Account, "allowGitNetwork": strconv.FormatBool(source.AllowGitNetwork)}
	if source.Adapter == "backlog-md" {
		_, err := queue.NewBacklogAdapter().Resolve(ctx, options)
		if diagnostic, ok := err.(queue.BacklogDiagnostic); ok {
			switch diagnostic.Code {
			case "cli-missing":
				return reason.Invalid("cli-missing: install the backlog CLI (Backlog.md 1.52.x required)")
			case "unsupported-version":
				found, _ := queueInitCommandOutput(ctx, source.Checkout, "backlog", "--version")
				return reason.Invalid("unsupported-version: found Backlog.md " + found + "; required 1.52.x")
			case "git-missing":
				return reason.Invalid("git-missing: install Git")
			case "git-network-consent":
				return reason.Invalid("git-network-consent: this project needs --allow-git-network")
			}
		}
		return err
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return reason.Invalid("cli-missing: install the gh CLI")
	}
	_, err := queueInitGitHubAdapter().Resolve(ctx, options)
	if diagnostic, ok := err.(queue.GitHubDiagnostic); ok && diagnostic.Code == "authentication" {
		return reason.New(reason.ReasonAuthenticationRequired, "authenticate with gh auth login --hostname "+source.Host)
	}
	return err
}

func queueInitOSUser() string {
	if user, err := osuser.Current(); err == nil && user.Username != "" {
		return user.Username
	}
	return os.Getenv("USER")
}

func prepareQueueInit(ctx context.Context, cmd *urfave.Command) (queueInitResult, error) {
	proposal, checkout, err := readQueueProposal(ctx, cmd)
	if err != nil {
		return queueInitResult{}, err
	}
	if cmd.Bool("enroll-contract") && proposal == nil {
		return queueInitResult{}, reason.Invalid("--enroll-contract requires a valid .config/worklease/queue-sources.yaml; do not combine it with --ignore-proposal")
	}
	if proposal != nil {
		return prepareQueueInitProposal(ctx, cmd, proposal, checkout)
	}
	return prepareQueueInitOne(ctx, cmd, nil, nil)
}

func prepareQueueInitOne(ctx context.Context, cmd *urfave.Command, proposed *queueProposalSource, snapshot []byte) (queueInitResult, error) {
	result := queueInitResult{Path: config.QueuePath(os.Getenv), Facts: []queueInitFact{}}
	if !filepath.IsAbs(result.Path) {
		return result, reason.Invalid("queue.yaml path must be absolute")
	}
	if cmd.String("adapter") == "external" {
		return prepareQueueInitExternal(ctx, cmd, result)
	}
	if cmd.IsSet("executable") || cmd.IsSet("adapter-config") || cmd.IsSet("adapter-config-file") {
		return result, reason.Invalid("--executable and --adapter-config require --adapter external")
	}
	checkout := cmd.String("checkout")
	if checkout == "" {
		checkout = "."
	}
	if _, err := exec.LookPath("git"); err != nil {
		return result, reason.Invalid("git-missing: install Git")
	}
	root, err := queueInitCommandOutput(ctx, checkout, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return result, reason.Invalid("--checkout must be a git checkout")
	}
	checkout, err = filepath.EvalSymlinks(root)
	if err != nil {
		return result, reason.Invalid("--checkout must resolve to an existing git checkout")
	}
	if cmd.Bool("enroll-contract") {
		_, checkout, err = handle.BindingRoots(checkout, nil)
		if err != nil {
			return result, reason.Invalid("--enroll-contract requires a resolvable Git checkout")
		}
	}
	result.Facts = append(result.Facts, queueInitFact{"checkout", checkout, "git rev-parse --show-toplevel"})
	contractCheckout := ""
	if cmd.Bool("enroll-contract") {
		contractCheckout, err = enrolledQueueContractRoot(ctx, checkout)
		if err != nil {
			return result, reason.Invalid("--enroll-contract requires an existing Git checkout")
		}
		result.Facts = append(result.Facts, queueInitFact{"contractCheckout", contractCheckout, "--enroll-contract"})
	}
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
	if snapshot != nil {
		data, err = snapshot, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, reason.New(reason.ReasonConfigInvalid, "queue.yaml cannot be read safely: "+err.Error())
	}
	existing := err == nil
	var cfg config.QueueConfig
	var doc yaml.Node
	if existing {
		cfg, err = config.ValidateQueue(data, os.Getenv)
		if err != nil {
			return result, reason.New(reason.ReasonConfigInvalid, err.Error())
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return result, reason.New(reason.ReasonConfigInvalid, err.Error())
		}
	} else {
		if err := yaml.Unmarshal([]byte("version: 1\nme:\n  {}\nsources:\n  []\nviews:\n  []\n"), &doc); err != nil {
			return result, err
		}
	}
	result.DefaultView = len(cfg.Views) == 0 || cfg.Views[0].Name == view
	mapping := doc.Content[0]
	if !existing {
		initBlockStyle(mapping)
	}
	remote, remoteErr := queueInitCommandOutput(ctx, checkout, "git", "remote", "get-url", "origin")
	host, repository, github := queueInitRemote(remote)
	rootConfig, rootConfigErr := os.Stat(filepath.Join(checkout, "backlog.config.yml"))
	folderConfig, folderConfigErr := os.Stat(filepath.Join(checkout, "backlog", "config.yml"))
	backlogDetected := rootConfigErr == nil && !rootConfig.IsDir() || folderConfigErr == nil && !folderConfig.IsDir()
	detectedAdapter := ""
	switch {
	case backlogDetected:
		detectedAdapter = "backlog-md"
	case remoteErr == nil && github && (host == "github.com" || strings.HasPrefix(host, "github.")):
		detectedAdapter = "github"
	}
	adapter := cmd.String("adapter")
	if proposed != nil {
		adapter = proposed.Adapter
		detectedAdapter = adapter
	}
	if adapter == "" {
		adapter = detectedAdapter
		if adapter == "" {
			return result, reason.Invalid("cannot detect source; specify --adapter backlog-md|github")
		}
	}
	if adapter != "backlog-md" && adapter != "github" {
		return result, reason.Invalid("--adapter must be backlog-md or github")
	}
	if adapter == "backlog-md" && !backlogDetected {
		return result, reason.Invalid("--checkout has no Backlog.md project")
	}
	if adapter == "github" && (!github || host != "github.com" && !strings.HasPrefix(host, "github.")) {
		return result, reason.Invalid("--checkout origin is not a GitHub remote")
	}
	if adapter == "github" && cmd.String("portable-claims") != "" {
		return result, reason.Invalid("--portable-claims requires backlog-md")
	}
	result.Adapter = adapter
	adapterOrigin := "--adapter"
	if proposed != nil {
		adapterOrigin = queueProposalFile
	}
	if cmd.String("adapter") == "" && proposed == nil {
		adapterOrigin = "git origin"
		if adapter == "backlog-md" {
			adapterOrigin = "Backlog.md config"
		}
	}
	viewOrigin := "--view"
	if cmd.String("view") == "" {
		viewOrigin = "default"
	}
	authorityOrigin := "--authority"
	if !cmd.IsSet("authority") {
		authorityOrigin = "default"
	}
	result.Facts = append(result.Facts, queueInitFact{"adapter", adapter, adapterOrigin}, queueInitFact{"view", view, viewOrigin}, queueInitFact{"authority", authority, authorityOrigin})
	if adapter == "backlog-md" {
		backlogDir, err := queue.BacklogDirectory(checkout)
		if err != nil {
			return result, err
		}
		result.Facts = append(result.Facts, queueInitFact{"backlogDirectory", backlogDir, "Backlog.md config"})
		if proposed == nil && github && remoteErr == nil && (host == "github.com" || strings.HasPrefix(host, "github.")) {
			other := "worklease queue"
			if cmd.String("view") != "" {
				other += " --view " + queueInitQuote(view)
			}
			other += " init"
			other += queueInitCheckoutArg(ctx, cmd, checkout)
			result.NextCommands = []string{"Also detected GitHub: " + other + " --adapter github"}
		}
	} else {
		result.Facts = append(result.Facts, queueInitFact{"host", host, "git origin"}, queueInitFact{"repository", repository, "git origin"})
	}
	for _, source := range cfg.Sources {
		resolvedCheckout, _ := filepath.EvalSymlinks(source.Checkout)
		if adapter == "backlog-md" && source.Adapter == adapter && resolvedCheckout == checkout || adapter == "github" && source.Adapter == adapter && strings.EqualFold(source.Host, host) && strings.EqualFold(source.Repository, repository) {
			contractEnrollmentChanged := false
			identitySourceID := source.ID
			if cmd.Bool("enroll-contract") && proposed != nil && proposed.ID != "" {
				identitySourceID = proposed.ID
			}
			if cmd.Bool("enroll-contract") && source.ContractCheckout != contractCheckout {
				if err := queueInitSetContractCheckout(mapping, source.ID, contractCheckout); err != nil {
					return result, err
				}
				source.ContractCheckout = contractCheckout
				contractEnrollmentChanged = true
			}
			if proposed != nil {
				result.Facts = append(result.Facts, queueInitFact{"proposedSourceId", proposed.ID, queueProposalFile})
				if proposed.Workflow != nil {
					for intent, transition := range proposed.Workflow {
						result.Facts = append(result.Facts, queueInitFact{"workflow." + intent, transition, queueProposalFile})
					}
				}
				if proposed.Claims != nil {
					result.Facts = append(result.Facts, queueInitFact{"claims", proposed.Claims.Policy + ":" + proposed.Claims.Source, queueProposalFile})
				}
				if proposed.Workflow != nil {
					if diff := proposalDifference("workflow", source.Workflow, proposed.Workflow); diff != "" {
						result.Reports = append(result.Reports, "source "+source.ID+": "+diff)
					}
				}
				if diff := proposalClaimReport(source.Claims, proposed.Claims); diff != "" {
					result.Reports = append(result.Reports, "source "+source.ID+": "+diff)
				}
			}
			if id := cmd.String("source-id"); id != "" && id != source.ID {
				return result, reason.Invalid("checkout already configured as source " + source.ID)
			}
			if cmd.IsSet("authority") {
				sourceAuthorities, authorityErr := config.QueueSourceAuthorities(cfg)
				if authorityErr != nil {
					return result, authorityErr
				}
				if sourceAuthorities[source.ID] != authority {
					return result, reason.Invalid("source authority differs from --authority")
				}
			}
			if claims := cmd.String("portable-claims"); claims != "" && (source.Claims == nil || source.Claims.Policy != "generic" || source.Claims.Source != claims) {
				return result, reason.Invalid("source claim domain differs from --portable-claims")
			}
			if (proposed == nil || adapter == "backlog-md") && cmd.IsSet("allow-git-network") && cmd.Bool("allow-git-network") != source.AllowGitNetwork {
				return result, reason.Invalid("source allowGitNetwork differs from --allow-git-network")
			}
			if err := queueInitPreflight(ctx, source); err != nil {
				return result, err
			}
			result.SourceID, result.Outcome = source.ID, "unchanged"
			if contractEnrollmentChanged {
				result.Outcome = "merged"
			}
			result.Identity = "unchanged"
			meNode := initField(mapping, "me")
			if adapter == "backlog-md" {
				configured := initField(meNode, "backlog-md")
				var names []string
				if err := configured.Decode(&names); err != nil {
					return result, reason.New(reason.ReasonConfigInvalid, "invalid me.backlog-md")
				}
				if len(names) > 0 {
					result.Me = names[0]
				}
				if me := cmd.String("me"); me != "" {
					if !queueInitValidMe(me) {
						return result, reason.Invalid("--me must be one @name")
					}
					if !slices.Contains(names, me) {
						entry, _ := initNode(me)
						configured.Content = append(configured.Content, entry)
						initBlockStyle(configured)
						result.Me, result.Outcome = me, "merged"
					}
				}
			} else {
				account, err := queueInitCommandOutput(ctx, checkout, "gh", "api", "--hostname", host, "user", "--jq", ".login")
				if err != nil || account == "" {
					return result, reason.New(reason.ReasonAuthenticationRequired, "authenticate with gh auth login --hostname "+host)
				}
				if account != source.Account || cmd.String("me") != "" && cmd.String("me") != account {
					return result, reason.Invalid("--me conflicts with authenticated GitHub account")
				}
				result.Me = account
			}
			meOrigin := "existing me.backlog-md"
			if adapter == "github" {
				meOrigin = "gh api --hostname " + host + " user"
			} else if result.Outcome == "merged" {
				meOrigin = "--me"
			}
			result.Facts = append(result.Facts, queueInitFact{"sourceId", source.ID, "existing source"}, queueInitFact{"me", result.Me, meOrigin})
			for _, intent := range []string{"start", "blocked", "review", "complete", "reopen"} {
				if source.Workflow[intent] != "" {
					result.Mapped = append(result.Mapped, intent)
				} else {
					result.Unmapped = append(result.Unmapped, intent)
				}
			}
			if result.Outcome == "unchanged" {
				result.YAML = string(data)
				identities, err := config.LoadQueueIdentities(os.Getenv)
				if err != nil {
					return result, err
				}
				if _, confirmed := identities.Sources[source.ID]; !confirmed {
					result.Identity = "confirmation-required"
					result.Checklist = queue.MigrationChecklist
					result.NextCommands = append(result.NextCommands, fmt.Sprintf("worklease queue --view %s identity confirm --source %s --acknowledge", queueInitQuote(view), queueInitQuote(identitySourceID)))
					return result, nil
				}
				result.NextCommands = append(result.NextCommands, queueInitQueueCommand(view, result.DefaultView))
				return result, nil
			}
			if cmd.Bool("dry-run") {
				result.Identity = "pending"
				result.NextCommands = append(result.NextCommands, queueInitApplyCommand(ctx, cmd, checkout, adapter, detectedAdapter, source.ID, source.ID, cmd.String("me"), ""))
			} else {
				result.NextCommands = append(result.NextCommands, queueInitQueueCommand(view, result.DefaultView))
			}
			if contractEnrollmentChanged {
				result.Checklist = queue.MigrationChecklist
				if cmd.Bool("dry-run") {
					result.Identity = "pending"
				} else {
					result.Identity = "confirmation-required"
					result.NextCommands = append(result.NextCommands, fmt.Sprintf("worklease queue --view %s identity confirm --source %s --acknowledge", queueInitQuote(view), queueInitQuote(identitySourceID)))
				}
			}
			result.YAML, err = queueInitRender(&doc)
			if err != nil {
				return result, err
			}
			if _, err := config.ValidateQueue([]byte(result.YAML), os.Getenv); err != nil {
				return result, reason.New(reason.ReasonConfigInvalid, err.Error())
			}
			return result, nil
		}
	}
	defaultID := strings.ReplaceAll(filepath.Base(checkout), ":", "")
	if proposed != nil && proposed.ID != "" {
		defaultID = proposed.ID
	}
	base := defaultID
	for n := 2; slices.ContainsFunc(cfg.Sources, func(s config.QueueSource) bool { return s.ID == defaultID }); n++ {
		defaultID = base + "-" + strconv.Itoa(n)
	}
	id := cmd.String("source-id")
	if id == "" {
		id = defaultID
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
	idOrigin := "--source-id"
	if cmd.String("source-id") == "" {
		idOrigin = "checkout basename"
		if proposed != nil && proposed.ID != "" {
			idOrigin = queueProposalFile
		}
	}
	result.Facts = append(result.Facts, queueInitFact{"sourceId", id, idOrigin})
	result.Outcome = "created"
	if existing {
		result.Outcome = "merged"
	}
	if err := queueInitAddViews(mapping, cfg, existing, cmd.String("view"), authority, id); err != nil {
		return result, err
	}
	source := config.QueueSource{ID: id, Authority: authority, Adapter: adapter, ContractCheckout: contractCheckout, AllowGitNetwork: adapter == "backlog-md" && cmd.Bool("allow-git-network")}
	networkOrigin := "default"
	if source.AllowGitNetwork {
		networkOrigin = "--allow-git-network"
	}
	result.Facts = append(result.Facts, queueInitFact{"allowGitNetwork", strconv.FormatBool(source.AllowGitNetwork), networkOrigin})
	me := cmd.String("me")
	defaultMe := ""
	meNode := initField(mapping, "me")
	if adapter == "backlog-md" {
		source.Checkout = checkout
		if err := queueInitPreflight(ctx, source); err != nil {
			return result, err
		}
		if claim := cmd.String("portable-claims"); claim != "" {
			source.Claims = &config.QueueClaims{Policy: "generic", Source: claim}
		}
		if proposed != nil {
			source.Claims = proposed.Claims
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
		if proposed != nil && proposed.Workflow != nil {
			source.Workflow = proposed.Workflow
			result.Unmapped = nil
			for intent, transition := range source.Workflow {
				result.Facts = append(result.Facts, queueInitFact{"workflow." + intent, transition, queueProposalFile})
			}
		}
		if proposed != nil && source.Claims != nil {
			result.Facts = append(result.Facts, queueInitFact{"claims", source.Claims.Policy + ":" + source.Claims.Source, queueProposalFile})
		}
		candidates := strings.Split(assignee, ",")
		if len(candidates) == 1 {
			defaultMe = strings.TrimSpace(candidates[0])
		}
		if defaultMe == "" {
			defaultMe = "@" + queueInitOSUser()
		}
		meOrigin := "--me"
		if existingMe := initField(meNode, "backlog-md"); existingMe != nil {
			var names []string
			if err := existingMe.Decode(&names); err != nil || len(names) == 0 {
				return result, reason.New(reason.ReasonConfigInvalid, "invalid me.backlog-md")
			}
			defaultMe = names[0]
			if me != "" && !queueInitValidMe(me) {
				return result, reason.Invalid("--me must be one @name")
			}
			if me != "" && !slices.Contains(names, me) {
				entry, _ := initNode(me)
				existingMe.Content = append(existingMe.Content, entry)
				initBlockStyle(existingMe)
			} else {
				me = names[0]
				meOrigin = "existing me.backlog-md"
			}
		} else {
			if me == "" {
				me = defaultMe
				meOrigin = "backlog config get defaultAssignee"
				if len(candidates) != 1 || strings.TrimSpace(candidates[0]) == "" {
					meOrigin = "OS user"
				}
			}
			if !queueInitValidMe(me) {
				return result, reason.New(reason.ReasonMeRequired, "cannot determine a valid @name; supply --me @name")
			}
			value, _ := initNode([]string{me})
			initSet(meNode, "backlog-md", value)
		}
		result.Me = me
		result.Facts = append(result.Facts, queueInitFact{"me", me, meOrigin})
	} else {
		source.Host, source.Repository = host, repository
		if _, err := exec.LookPath("gh"); err != nil {
			return result, reason.Invalid("cli-missing: install the gh CLI")
		}
		account, e := queueInitCommandOutput(ctx, checkout, "gh", "api", "--hostname", host, "user", "--jq", ".login")
		if e != nil || account == "" {
			return result, reason.New(reason.ReasonAuthenticationRequired, "authenticate with gh auth login --hostname "+host)
		}
		result.Facts = append(result.Facts, queueInitFact{"account", account, "gh api --hostname " + host + " user"})
		if me != "" && me != account {
			return result, reason.Invalid("--me conflicts with authenticated GitHub account")
		}
		source.Account = account
		defaultMe = account
		result.Me = account
		result.Facts = append(result.Facts, queueInitFact{"me", account, "gh api --hostname " + host + " user"})
		source.Workflow = map[string]string{"complete": "closed", "reopen": "open"}
		if proposed != nil && proposed.Workflow != nil {
			source.Workflow = proposed.Workflow
			for intent, transition := range source.Workflow {
				result.Facts = append(result.Facts, queueInitFact{"workflow." + intent, transition, queueProposalFile})
			}
		}
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
	}{{"id", id}, {"authority", authority}, {"adapter", adapter}} {
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
	if source.ContractCheckout != "" {
		node, _ := initNode(source.ContractCheckout)
		initSet(sourceNode, "contractCheckout", node)
	}
	node, _ := initNode(source.AllowGitNetwork)
	initSet(sourceNode, "allowGitNetwork", node)
	sourcesNode := initField(mapping, "sources")
	sourcesNode.Content = append(sourcesNode.Content, sourceNode)
	sourcesNode.Style &^= yaml.FlowStyle
	for intent := range source.Workflow {
		result.Mapped = append(result.Mapped, intent)
	}
	slices.Sort(result.Mapped)
	if err := queueInitPreflight(ctx, source); err != nil {
		return result, err
	}
	result.YAML, err = queueInitRender(&doc)
	if err != nil {
		return result, err
	}
	if _, err := config.ValidateQueue([]byte(result.YAML), os.Getenv); err != nil {
		return result, reason.New(reason.ReasonConfigInvalid, err.Error())
	}
	if !cmd.Bool("dry-run") {
		identities, err := config.LoadQueueIdentities(os.Getenv)
		if err != nil {
			return result, err
		}
		_, existingIdentity := identities.Sources[id]
		if source.Claims == nil && authority == "local" && !existingIdentity && !cmd.Bool("enroll-contract") {
			result.Identity = "automatic"
		} else {
			result.Identity = "confirmation-required"
		}
		result.NextCommands = append(result.NextCommands, fmt.Sprintf("worklease queue --view %s identity confirm --source %s --acknowledge", queueInitQuote(view), queueInitQuote(id)))
		result.Checklist = queue.MigrationChecklist
	} else {
		result.Identity = "pending"
		if proposed != nil {
			result.NextCommands = append(result.NextCommands, queueInitApplyCommand(ctx, cmd, checkout, adapter, detectedAdapter, id, id, me, defaultMe))
		} else {
			result.NextCommands = append(result.NextCommands, queueInitApplyCommand(ctx, cmd, checkout, adapter, detectedAdapter, id, defaultID, me, defaultMe))
		}
	}
	return result, nil
}

// queueInitViews mirrors the TUI's built-in filters. Source membership comes
// from project scope, not from the views.
var queueInitViews = []config.QueueView{
	{Name: "Ready", Filter: config.QueueFilter{Readiness: "ready", Claim: "free", Assigned: []string{"me", "nobody"}}},
	{Name: "Mine", Filter: config.QueueFilter{Assigned: []string{"me"}}},
	{Name: "Claimed", Filter: config.QueueFilter{Claim: "held"}},
	{Name: "All", Filter: config.QueueFilter{Readiness: "all"}},
}

// queueInitAddViews creates filter-only defaults. Existing source lists remain
// untouched, so legacy views continue to impose their explicit restrictions.
func queueInitAddViews(mapping *yaml.Node, cfg config.QueueConfig, existing bool, view, _ string, id string) error {
	wanted := []config.QueueView{{Name: view, Filter: queueInitViews[0].Filter}}
	if view == "" {
		wanted = queueInitViews
	}
	viewsNode := initField(mapping, "views")
	for n, want := range wanted {
		primary := n == 0
		index := slices.IndexFunc(cfg.Views, func(v config.QueueView) bool { return v.Name == want.Name })
		switch {
		case index >= 0:
			// Preserve legacy explicit source restrictions unchanged.
		case primary || !existing:
			viewNode, err := initNode(want)
			if err != nil {
				return err
			}
			viewsNode.Content = append(viewsNode.Content, viewNode)
			viewsNode.Style &^= yaml.FlowStyle
		}
	}
	return nil
}

func queueInitValidMe(me string) bool {
	return strings.HasPrefix(me, "@") && len(me) > 1 && !strings.ContainsAny(me, " ,\t\n\r")
}

func queueInitRender(doc *yaml.Node) (string, error) {
	var rendered bytes.Buffer
	encoder := yaml.NewEncoder(&rendered)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	return rendered.String(), nil
}
