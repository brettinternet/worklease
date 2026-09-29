package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
)

const (
	queueContractMissing       = "source-contract-missing"
	queueContractInvalid       = "source-contract-invalid"
	queueContractCheckoutError = "source-contract-checkout-unavailable"
	queueContractSourceMissing = "source-contract-source-missing"
	queueContractIDConflict    = "source-contract-id-conflict"
)

// loadQueueRuntimeConfig applies only contracts explicitly enrolled by the
// owner's private queue.yaml. Legacy init copies remain independent.
func loadQueueRuntimeConfig(ctx context.Context, env func(string) string) (config.QueueConfig, map[string]string, error) {
	cfg, err := config.LoadQueue(env)
	if err != nil {
		return config.QueueConfig{}, nil, err
	}
	cfg, diagnostics := applyQueueContracts(ctx, cfg)
	return cfg, diagnostics, nil
}

type queueContractUpdate struct {
	index  int
	before config.QueueSource
	after  config.QueueSource
}

func applyQueueContracts(ctx context.Context, cfg config.QueueConfig) (config.QueueConfig, map[string]string) {
	diagnostics := make(map[string]string)
	groups := make(map[string][]int)
	for i, source := range cfg.Sources {
		if source.ContractCheckout == "" {
			continue
		}
		root, err := enrolledQueueContractRoot(ctx, source.ContractCheckout)
		if err != nil {
			diagnostics[source.ID] = queueContractCheckoutError
			continue
		}
		groups[root] = append(groups[root], i)
	}

	updates := make([]queueContractUpdate, 0)
	for root, indices := range groups {
		proposal, _, err := readQueueProposalAt(root)
		if err != nil {
			for _, index := range indices {
				diagnostics[cfg.Sources[index].ID] = queueContractInvalid + ": " + err.Error()
			}
			continue
		}
		if proposal == nil {
			for _, index := range indices {
				diagnostics[cfg.Sources[index].ID] = queueContractMissing
			}
			continue
		}
		matches := matchQueueContractSources(cfg.Sources, indices, proposal.Sources)
		for _, index := range indices {
			contractIndex, ok := matches[index]
			if !ok {
				diagnostics[cfg.Sources[index].ID] = queueContractSourceMissing
				continue
			}
			local := cfg.Sources[index]
			contract := proposal.Sources[contractIndex]
			updated := local
			if contract.ID != "" {
				updated.ID = contract.ID
			}
			updated.Adapter = contract.Adapter
			updated.Workflow = cloneQueueWorkflow(contract.Workflow)
			updated.Claims = cloneQueueClaims(contract.Claims)
			updates = append(updates, queueContractUpdate{index: index, before: local, after: updated})
		}
	}

	// IDs are global in owner-private queue.yaml. A contract must not silently
	// shadow another configured source when applying its runtime identity.
	candidate := make(map[int]config.QueueSource, len(updates))
	owners := make(map[string][]int, len(cfg.Sources))
	for i, source := range cfg.Sources {
		candidate[i] = source
	}
	for _, update := range updates {
		candidate[update.index] = update.after
	}
	for index, source := range candidate {
		owners[source.ID] = append(owners[source.ID], index)
	}
	conflicting := map[int]bool{}
	// Do not reuse another private source's ID, even if that source also
	// proposes a rename: a rejected rename must not leave duplicate IDs.
	for _, update := range updates {
		for i, source := range cfg.Sources {
			if i != update.index && source.ID == update.after.ID {
				conflicting[update.index] = true
				diagnostics[update.before.ID] = queueContractIDConflict
			}
		}
	}
	for _, indices := range owners {
		if len(indices) < 2 {
			continue
		}
		for _, index := range indices {
			for _, update := range updates {
				if update.index == index {
					conflicting[index] = true
					diagnostics[update.before.ID] = queueContractIDConflict
					break
				}
			}
		}
	}

	idRemap := make(map[string]string)
	for _, update := range updates {
		if conflicting[update.index] {
			continue
		}
		cfg.Sources[update.index] = update.after
		if update.before.ID != update.after.ID {
			idRemap[update.before.ID] = update.after.ID
		}
	}
	if len(idRemap) > 0 {
		for i := range cfg.Views {
			for j, id := range cfg.Views[i].Sources {
				if replacement, ok := idRemap[id]; ok {
					cfg.Views[i].Sources[j] = replacement
				}
			}
		}
	}
	return cfg, diagnostics
}

func enrolledQueueContractRoot(ctx context.Context, checkout string) (string, error) {
	if !filepath.IsAbs(checkout) || filepath.Clean(checkout) != checkout {
		return "", os.ErrInvalid
	}
	_, root, err := handle.BindingRoots(checkout, nil)
	if err != nil {
		return "", err
	}
	gitRoot, err := queueInitCommandOutput(ctx, root, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	gitRoot, err = filepath.EvalSymlinks(strings.TrimSpace(gitRoot))
	if err != nil || gitRoot != root {
		return "", os.ErrInvalid
	}
	return root, nil
}

func matchQueueContractSources(configured []config.QueueSource, indices []int, contracts []queueProposalSource) map[int]int {
	matches := make(map[int]int, len(indices))
	usedContracts := make(map[int]bool, len(contracts))
	usedSources := make(map[int]bool, len(indices))
	for contractIndex, contract := range contracts {
		if contract.ID == "" {
			continue
		}
		for _, sourceIndex := range indices {
			if !usedSources[sourceIndex] && configured[sourceIndex].ID == contract.ID {
				matches[sourceIndex] = contractIndex
				usedSources[sourceIndex], usedContracts[contractIndex] = true, true
				break
			}
		}
	}
	for contractIndex, contract := range contracts {
		if usedContracts[contractIndex] {
			continue
		}
		var sourceMatch, sourceCount int
		for _, sourceIndex := range indices {
			if !usedSources[sourceIndex] && configured[sourceIndex].Adapter == contract.Adapter {
				sourceMatch, sourceCount = sourceIndex, sourceCount+1
			}
		}
		var contractCount int
		for otherIndex, other := range contracts {
			if !usedContracts[otherIndex] && other.Adapter == contract.Adapter {
				contractCount++
			}
		}
		if sourceCount == 1 && contractCount == 1 {
			matches[sourceMatch] = contractIndex
			usedSources[sourceMatch], usedContracts[contractIndex] = true, true
		}
	}
	var remainingSources, remainingContracts []int
	for _, sourceIndex := range indices {
		if !usedSources[sourceIndex] {
			remainingSources = append(remainingSources, sourceIndex)
		}
	}
	for contractIndex := range contracts {
		if !usedContracts[contractIndex] {
			remainingContracts = append(remainingContracts, contractIndex)
		}
	}
	if len(remainingSources) == 1 && len(remainingContracts) == 1 {
		matches[remainingSources[0]] = remainingContracts[0]
	}
	return matches
}

func applyQueueContractDiagnostics(snapshot *queue.Snapshot, diagnostics map[string]string) {
	if snapshot.Sources == nil {
		snapshot.Sources = make(map[string]queue.Coverage)
	}
	for sourceID, diagnostic := range diagnostics {
		snapshot.Sources[sourceID] = queue.Coverage{State: queue.CoverageUnknown, TotalAccuracy: queue.TotalUnknown, Reason: diagnostic}
	}
}

func cloneQueueWorkflow(workflow map[string]string) map[string]string {
	if workflow == nil {
		return nil
	}
	result := make(map[string]string, len(workflow))
	for key, value := range workflow {
		result[key] = value
	}
	return result
}

func cloneQueueClaims(claims *config.QueueClaims) *config.QueueClaims {
	if claims == nil {
		return nil
	}
	copy := *claims
	return &copy
}

func validateCurrentQueueContract(ctx context.Context, expected config.QueueSource) error {
	if expected.ContractCheckout == "" {
		return nil
	}
	cfg, diagnostics, err := loadQueueRuntimeConfig(ctx, os.Getenv)
	if err != nil {
		return reason.New("binding-migration-required", "enrolled source contract could not be reloaded; claim actions are disabled")
	}
	if diagnostics[expected.ID] != "" {
		return reason.New("binding-migration-required", "enrolled source contract is unavailable or invalid: "+diagnostics[expected.ID])
	}
	for _, current := range cfg.Sources {
		if current.ID == expected.ID {
			if current.ContractCheckout != expected.ContractCheckout || current.Adapter != expected.Adapter || !reflect.DeepEqual(current.Claims, expected.Claims) {
				return reason.New("binding-migration-required", "claim inputs changed in the enrolled source contract; confirm the source identity before claiming")
			}
			return nil
		}
	}
	return reason.New("binding-migration-required", "source no longer appears in its enrolled contract; confirm the source identity before claiming")
}
