package queue

import "sort"

// Selection is an observation, not a reservation. Incomplete scope or edges
// prevent selection even when some individual candidates appear ready.
type Selection struct {
	Result     string      `json:"result"`
	Candidates []Item      `json:"candidates"`
	Excluded   []Exclusion `json:"excluded"`
}

type Exclusion struct {
	Ref     Ref      `json:"ref"`
	Reasons []string `json:"reasons"`
}

// SelectWave applies the start/resume policy to a complete view. visible is the
// view-filtered set; scoped includes filtered-out items for no-work diagnostics.
// A wave never contains two items with an overlapping exact resource.
func SelectWave(scoped, visible []Item, sources []string, selectors []Ref, complete bool, limit int, assignedToMe func(Item, string) bool) Selection {
	if len(selectors) > 0 {
		wanted := make(map[string]bool, len(selectors))
		for _, ref := range selectors {
			wanted[ref.Key()] = true
		}
		scoped = selectRefs(scoped, wanted)
		visible = selectRefs(visible, wanted)
	}
	result := Selection{Result: "complete-and-empty", Candidates: []Item{}, Excluded: []Exclusion{}}
	if !complete {
		result.Result = "incomplete"
		for _, item := range scoped {
			reasons := append([]string{}, item.Readiness.Reasons...)
			if item.Closure != CoverageComplete || !item.DependenciesKnown {
				reasons = append(reasons, "incomplete-closure")
			}
			if len(reasons) > 0 {
				result.Excluded = append(result.Excluded, Exclusion{Ref: item.Ref, Reasons: reasons})
			}
		}
		return result
	}
	order := make(map[string]int, len(sources))
	for i, id := range sources {
		if _, exists := order[id]; !exists {
			order[id] = i
		}
	}
	selectorOrder := make(map[string]int, len(selectors))
	for i, ref := range selectors {
		if _, exists := selectorOrder[ref.Key()]; !exists {
			selectorOrder[ref.Key()] = i
		}
	}
	// Explicit selector order precedes source order; provider priority, order,
	// and stable WorkRef break ties only within the same source.
	sort.SliceStable(visible, func(i, j int) bool {
		a, b := visible[i], visible[j]
		if len(selectors) > 0 && selectorOrder[a.Ref.Key()] != selectorOrder[b.Ref.Key()] {
			return selectorOrder[a.Ref.Key()] < selectorOrder[b.Ref.Key()]
		}
		if order[a.Ref.SourceID] != order[b.Ref.SourceID] {
			return order[a.Ref.SourceID] < order[b.Ref.SourceID]
		}
		if a.Priority != b.Priority {
			return a.Priority != 0 && (b.Priority == 0 || a.Priority < b.Priority)
		}
		if a.Order != b.Order {
			return a.Order != "" && (b.Order == "" || a.Order < b.Order)
		}
		return a.Ref.Less(b.Ref)
	})
	visibleRefs := make(map[string]bool, len(visible))
	for _, item := range visible {
		visibleRefs[item.Ref.Key()] = true
	}
	used := make(map[string]bool)
	var unfinished, blocked, claimed, elsewhere, unknown int
	for _, item := range scoped {
		if item.Terminal || item.State == StateComplete {
			continue
		}
		unfinished++
		reasons := []string{}
		if !visibleRefs[item.Ref.Key()] {
			reasons = append(reasons, "view-filter")
		}
		if item.Readiness.Status != Ready {
			reasons = append(reasons, item.Readiness.Reasons...)
			if item.Readiness.Status == Blocked {
				blocked++
			} else {
				unknown++
			}
		}
		if item.Claim.Active {
			reasons = append(reasons, "active-claim")
			claimed++
		} else if !item.Claim.Known || item.Claim.Stale || item.Claim.Reason != "" {
			reasons = append(reasons, "claim-unknown")
			unknown++
		}
		if len(item.AssignedTo) > 0 && len(selectors) == 0 {
			mine := false
			for _, owner := range item.AssignedTo {
				if assignedToMe(item, owner) {
					mine = true
				}
			}
			if !mine {
				reasons = append(reasons, "assigned-elsewhere")
				elsewhere++
			}
		}
		if item.State != StateOpen && item.State != StateInProgress {
			reasons = append(reasons, "not-startable")
		}
		if len(item.Resources) == 0 || item.KeyInputs == nil {
			reasons = append(reasons, "resource-unavailable")
			unknown++
		}
		if len(reasons) > 0 {
			result.Excluded = append(result.Excluded, Exclusion{Ref: item.Ref, Reasons: reasons})
		}
	}
	if unknown > 0 {
		result.Result = "incomplete"
		return result
	}
	// The sorted visible set determines the wave; exclusions from this phase
	// explain exact resource collisions and the caller's bounded group size.
	for _, item := range visible {
		if item.Terminal || item.State == StateComplete || item.Readiness.Status != Ready || item.Claim.Active || !item.Claim.Known || item.Claim.Stale || item.Claim.Reason != "" || item.State != StateOpen && item.State != StateInProgress || len(item.Resources) == 0 || item.KeyInputs == nil {
			continue
		}
		mine := len(selectors) > 0 || len(item.AssignedTo) == 0
		for _, owner := range item.AssignedTo {
			mine = mine || assignedToMe(item, owner)
		}
		if !mine {
			continue
		}
		conflict := false
		for _, key := range item.Resources {
			conflict = conflict || used[key]
		}
		if conflict {
			result.Excluded = append(result.Excluded, Exclusion{Ref: item.Ref, Reasons: []string{"resource-conflict"}})
			continue
		}
		if len(result.Candidates) == limit {
			result.Excluded = append(result.Excluded, Exclusion{Ref: item.Ref, Reasons: []string{"group-limit"}})
			continue
		}
		result.Candidates = append(result.Candidates, item)
		for _, key := range item.Resources {
			used[key] = true
		}
	}
	if len(result.Candidates) > 0 {
		result.Result = "ready"
	} else if unfinished > 0 {
		switch {
		case blocked > 0 && claimed == 0 && elsewhere == 0:
			result.Result = "blocked"
		case claimed > 0 && elsewhere == 0 && blocked == 0:
			result.Result = "active-claims"
		case elsewhere > 0 && claimed == 0 && blocked == 0:
			result.Result = "assigned-elsewhere"
		default:
			result.Result = "ineligible"
		}
	}
	return result
}

func selectRefs(items []Item, wanted map[string]bool) []Item {
	selected := make([]Item, 0, len(items))
	for _, item := range items {
		if wanted[item.Ref.Key()] {
			selected = append(selected, item)
		}
	}
	return selected
}
