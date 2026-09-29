package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
	urfave "github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

const queueProposalFile = ".config/worklease/queue-sources.yaml"

type queueProposalSource struct {
	ID       string              `yaml:"id"`
	Adapter  string              `yaml:"adapter"`
	Workflow map[string]string   `yaml:"workflow"`
	Claims   *config.QueueClaims `yaml:"claims"`
}

type queueProposal struct {
	Version int                   `yaml:"version"`
	Sources []queueProposalSource `yaml:"sources"`
}

// A project proposal is read only by init, after resolving the selected checkout.
// Never decode it as QueueConfig: that schema contains executable and credential fields.
func readQueueProposal(ctx context.Context, cmd *urfave.Command) (*queueProposal, string, error) {
	if cmd.Bool("ignore-proposal") || cmd.String("adapter") == "external" {
		return nil, "", nil
	}
	checkout := cmd.String("checkout")
	if checkout == "" {
		checkout = "."
	}
	root, err := queueInitCommandOutput(ctx, checkout, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, "", nil // ordinary init will report the checkout error
	}
	if cmd.Bool("enroll-contract") {
		_, root, err = handle.BindingRoots(root, nil)
		if err != nil {
			return nil, "", reason.Invalid("--enroll-contract requires a resolvable Git checkout")
		}
	}
	return readQueueProposalAt(root)
}

func readQueueProposalAt(root string) (*queueProposal, string, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, "", reason.Invalid("checkout must resolve to an existing git checkout")
	}
	for _, directory := range []string{".config", filepath.Join(".config", "worklease")} {
		info, err := os.Lstat(filepath.Join(root, directory))
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", nil
		}
		if err != nil {
			return nil, "", err
		}
		if !info.IsDir() {
			return nil, "", reason.Invalid(queueProposalFile + ": parent must be a directory, not a symlink")
		}
	}
	path := filepath.Join(root, queueProposalFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, "", reason.Invalid(queueProposalFile + ": expected a regular file of at most 1 MiB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil || len(document.Content) != 1 {
		return nil, "", reason.Invalid(queueProposalFile + ": invalid YAML")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, "", reason.Invalid(queueProposalFile + ": expected one YAML document")
	}
	rootNode := document.Content[0]
	if err := proposalKeys(rootNode, "", "version", "sources"); err != nil {
		return nil, "", err
	}
	fields := proposalFields(rootNode)
	if fields["version"] == nil || fields["sources"] == nil || fields["sources"].Kind != yaml.SequenceNode || len(fields["sources"].Content) == 0 || len(fields["sources"].Content) > 8 {
		return nil, "", reason.Invalid(queueProposalFile + ": version and 1 to 8 sources required")
	}
	for i, source := range fields["sources"].Content {
		label := fmt.Sprintf("sources[%d]", i)
		if err := proposalKeys(source, label, "id", "adapter", "workflow", "claims"); err != nil {
			return nil, "", err
		}
		values := proposalFields(source)
		if values["workflow"] != nil {
			if err := proposalKeys(values["workflow"], label+".workflow", "start", "blocked", "review", "complete", "reopen"); err != nil {
				return nil, "", err
			}
		}
		if values["claims"] != nil {
			if err := proposalKeys(values["claims"], label+".claims", "policy", "source"); err != nil {
				return nil, "", err
			}
		}
	}
	var proposal queueProposal
	if err := rootNode.Decode(&proposal); err != nil || proposal.Version != 1 {
		return nil, "", reason.Invalid(queueProposalFile + ": version must be 1 and sources must be valid")
	}
	seen := map[string]bool{}
	for i, source := range proposal.Sources {
		if source.Adapter != "backlog-md" && source.Adapter != "github" {
			return nil, "", reason.Invalid(fmt.Sprintf("%s: sources[%d].adapter must be backlog-md or github", queueProposalFile, i))
		}
		if seen[source.Adapter] {
			return nil, "", reason.Invalid(queueProposalFile + ": duplicate adapter " + source.Adapter + " for checkout")
		}
		seen[source.Adapter] = true
		if source.ID != "" && strings.Contains(source.ID, ":") {
			return nil, "", reason.Invalid(queueProposalFile + ": source id must not contain ':'")
		}
		if source.Claims != nil && (source.Adapter != "backlog-md" || source.Claims.Policy != "generic" || source.Claims.Source == "") {
			return nil, "", reason.Invalid(queueProposalFile + ": claims require backlog-md generic policy and source")
		}
		for intent, value := range source.Workflow {
			if strings.TrimSpace(value) == "" {
				return nil, "", reason.Invalid(fmt.Sprintf("%s: sources[%d].workflow.%s must be nonempty", queueProposalFile, i, intent))
			}
		}
	}
	return &proposal, root, nil
}

func proposalFields(node *yaml.Node) map[string]*yaml.Node {
	fields := map[string]*yaml.Node{}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			fields[node.Content[i].Value] = node.Content[i+1]
		}
	}
	return fields
}

func proposalKeys(node *yaml.Node, label string, allowed ...string) error {
	if node.Kind != yaml.MappingNode {
		return reason.Invalid(queueProposalFile + ": " + label + " must be a mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		name := strings.TrimPrefix(label+"."+key, ".")
		if node.Content[i].Tag != "!!str" || !slices.Contains(allowed, key) || seen[key] {
			return reason.Invalid(queueProposalFile + ": unknown or duplicate key " + name)
		}
		seen[key] = true
	}
	return nil
}

func prepareQueueInitProposal(ctx context.Context, cmd *urfave.Command, proposal *queueProposal, checkout string) (queueInitResult, error) {
	if cmd.IsSet("adapter") || cmd.IsSet("source-id") || cmd.IsSet("portable-claims") {
		return queueInitResult{}, reason.Invalid("--adapter, --source-id and --portable-claims require --ignore-proposal")
	}
	var aggregate queueInitResult
	var snapshot []byte
	for i := range proposal.Sources {
		part, err := prepareQueueInitOne(ctx, cmd, &proposal.Sources[i], snapshot)
		if err != nil {
			return aggregate, err
		}
		if cmd.Bool("enroll-contract") {
			for i := range part.Reports {
				part.Reports[i] = strings.ReplaceAll(part.Reports[i], "(not applied)", "(contract-owned at runtime)")
			}
		}
		if i == 0 {
			aggregate = part
		} else {
			aggregate.Facts = append(aggregate.Facts, part.Facts...)
			aggregate.NextCommands = append(aggregate.NextCommands, part.NextCommands...)
			aggregate.Reports = append(aggregate.Reports, part.Reports...)
			if part.Checklist != "" {
				aggregate.Checklist = part.Checklist
			}
			if part.Identity == "confirmation-required" {
				aggregate.Identity = part.Identity
			}
			if part.Outcome != "unchanged" {
				aggregate.Outcome = "merged"
			}
		}
		aggregate.Sources = append(aggregate.Sources, queueInitSourceResult{ID: part.SourceID, Adapter: part.Adapter, Outcome: part.Outcome, Identity: part.Identity, Facts: part.Facts, Reports: part.Reports})
		aggregate.YAML = part.YAML
		snapshot = []byte(part.YAML)
	}
	aggregate.NextCommands = slices.Compact(aggregate.NextCommands)
	// Do not offer a queue command from an unchanged source when a new source
	// still needs identity confirmation.
	if slices.ContainsFunc(aggregate.Sources, func(part queueInitSourceResult) bool { return part.Identity == "confirmation-required" }) {
		aggregate.NextCommands = slices.DeleteFunc(aggregate.NextCommands, func(command string) bool {
			return strings.HasPrefix(command, "worklease queue") && !strings.Contains(command, " identity confirm ") && !strings.Contains(command, " init")
		})
	}
	// An unchanged final source does not make earlier additions disappear.
	if len(proposal.Sources) > 1 {
		for _, part := range aggregate.Sources {
			if part.Outcome != "unchanged" {
				aggregate.Outcome = "merged"
				break
			}
		}
	}
	cfg, err := config.ValidateQueue(snapshot, os.Getenv)
	if err != nil {
		return aggregate, err
	}
	for _, source := range cfg.Sources {
		if source.Adapter != "backlog-md" && source.Adapter != "github" {
			continue
		}
		resolved, _ := filepath.EvalSymlinks(source.Checkout)
		if source.Adapter == "backlog-md" && resolved != checkout {
			continue
		}
		if source.Adapter == "github" {
			// Only report the repository belonging to this checkout's origin.
			remote, _ := queueInitCommandOutput(ctx, checkout, "git", "remote", "get-url", "origin")
			host, repository, _ := queueInitRemote(remote)
			if !strings.EqualFold(source.Host, host) || !strings.EqualFold(source.Repository, repository) {
				continue
			}
		}
		if !slices.ContainsFunc(aggregate.Sources, func(part queueInitSourceResult) bool { return part.Adapter == source.Adapter }) {
			aggregate.Reports = append(aggregate.Reports, fmt.Sprintf("configured source %s (%s) is absent from %s; not removed", source.ID, source.Adapter, queueProposalFile))
		}
	}
	return aggregate, nil
}

func proposalDifference(name string, configured, proposed any) string {
	if reflect.DeepEqual(configured, proposed) {
		return ""
	}
	before, _ := yaml.Marshal(configured)
	after, _ := yaml.Marshal(proposed)
	return fmt.Sprintf("%s differs (not applied):\n  configured: %s  proposed: %s", name, string(before), string(after))
}

func proposalClaimReport(configured, proposed *config.QueueClaims) string {
	if configured == nil && proposed == nil {
		return ""
	}
	if configured != nil && proposed != nil && *configured == *proposed {
		return ""
	}
	return proposalDifference("claims", configured, proposed) + "Migration checklist: " + queue.MigrationChecklist
}
