package queue

// Recompute uses one memoized traversal per graph node, so overlapping prerequisite
// closures are evaluated in O(items + relationships), not once per selected item.
func Recompute(items map[string]Item, graph CoverageState) map[string]Item {
	const (
		rIncomplete uint16 = 1 << iota
		rStale
		rMissing
		rCycle
		rUnsatisfied
		rUnsupported
		rUnknownCondition
		rProviderBlocker
		rReadFailure
		rAmbiguousProjection
		rProjectStatusUnknown
		rProjectStatusBlocked
		rProjectStatusConflict
	)
	out := cloneItems(items)
	memo := make(map[string]uint16, len(out))
	visiting := make(map[string]bool, len(out))
	var evaluate func(Ref) uint16
	evaluate = func(ref Ref) uint16 {
		key := ref.Key()
		if visiting[key] {
			return rCycle
		}
		if flags, ok := memo[key]; ok {
			return flags
		}
		item, ok := out[key]
		if !ok {
			return rMissing
		}
		visiting[key] = true
		var flags uint16
		if !item.Fresh {
			flags |= rStale
		}
		if item.ProviderBlocked && item.Fresh {
			flags |= rProviderBlocker
		}
		if item.ProjectStatusBound {
			if !item.ProjectStatusKnown || item.ProjectStatusState == "" || item.ProjectStatusState == "unknown" {
				flags |= rProjectStatusUnknown
			}
			if item.ProjectStatusState == "blocked" {
				flags |= rProjectStatusBlocked
			}
			if item.ProjectStatusConflict {
				flags |= rProjectStatusConflict
			}
		}
		if item.ReadOutcome != "" && item.ReadOutcome != "found" {
			flags |= rReadFailure | rIncomplete
		}
		if item.ReadPermission == Denied || item.ReadPermission == PermissionUnknown {
			flags |= rReadFailure | rIncomplete
		}
		if !item.DependenciesKnown || item.Closure != "" && item.Closure != CoverageComplete {
			flags |= rIncomplete
		}
		if item.Relationships == nil {
			for _, prereq := range item.Dependencies {
				p, found := out[prereq.Key()]
				if !found {
					flags |= rMissing
					continue
				}
				if !p.Fresh || !p.TerminalKnown || p.ReadPermission == Denied || p.ReadPermission == PermissionUnknown {
					flags |= rUnknownCondition
				} else if !p.Terminal {
					flags |= rUnsatisfied
				}
				prereqFlags := evaluate(prereq)
				if p.Terminal && p.TerminalKnown {
					// A completed issue satisfies the terminal condition even if its
					// advisory project status is contradictory or unmapped.
					prereqFlags &^= rProjectStatusUnknown | rProjectStatusBlocked | rProjectStatusConflict
				}
				flags |= prereqFlags
			}
		} else {
			if len(item.Dependencies) > 0 && !projectsHardEdges(key, item.Dependencies, item.Relationships) {
				flags |= rAmbiguousProjection
			}
			for _, edge := range item.Relationships {
				if edge.From.Key() != key || (edge.Type != HardPrerequisite && edge.Type != CrossSourcePrerequisite) {
					continue
				}
				if edge.Support == Unsupported {
					flags |= rUnsupported
					continue
				}
				if edge.Support == SupportUnknown {
					flags |= rUnknownCondition
					continue
				}
				if edge.Condition == "" {
					flags |= rUnsupported
					continue
				}
				if !edge.Fresh {
					flags |= rStale | rIncomplete
					flags |= evaluate(edge.To)
					continue
				}
				prerequisite, exists := out[edge.To.Key()]
				if edge.Condition == "terminal" {
					if !exists {
						flags |= rMissing
					} else if !prerequisite.Fresh || !prerequisite.TerminalKnown || prerequisite.ReadPermission == Denied || prerequisite.ReadPermission == PermissionUnknown {
						flags |= rUnknownCondition
					} else if !prerequisite.Terminal {
						flags |= rUnsatisfied
					}
				} else if edge.Interpretation == "unsatisfied" {
					flags |= rUnsatisfied
				} else if edge.Interpretation != "satisfied" {
					flags |= rUnsupported
				}
				prereqFlags := evaluate(edge.To)
				if edge.Condition == "terminal" && exists && prerequisite.Terminal && prerequisite.TerminalKnown {
					prereqFlags &^= rProjectStatusUnknown | rProjectStatusBlocked | rProjectStatusConflict
				}
				flags |= prereqFlags
			}
		}
		visiting[key] = false
		// A cycle can make an in-progress ancestor's result incomplete. Do not
		// memoize that partial result; a later traversal may discover blockers
		// reachable from the rest of the strongly connected component.
		if flags&rCycle == 0 {
			memo[key] = flags
		}
		return flags
	}
	for key, item := range out {
		flags := evaluate(item.Ref)
		if item.Closure == "" && graph != CoverageComplete {
			flags |= rIncomplete
		}
		status := Ready
		if flags&(rUnsatisfied|rProviderBlocker|rProjectStatusBlocked) != 0 {
			status = Blocked
		} else if flags != 0 {
			status = ReadinessUnknown
		}
		reasons := make([]string, 0, 8)
		for bit, name := range map[uint16]string{rIncomplete: "incomplete-closure", rStale: "stale-evidence", rMissing: "missing-prerequisite", rCycle: "hard-cycle", rUnsatisfied: "hard-condition-unsatisfied", rUnsupported: "unsupported-condition", rUnknownCondition: "unknown-condition", rProviderBlocker: "provider-blocker", rReadFailure: "item-read-failed", rAmbiguousProjection: "ambiguous-dependency-projection", rProjectStatusUnknown: "project-status-unmapped", rProjectStatusBlocked: "project-status-blocked", rProjectStatusConflict: "project-issue-state-conflict"} {
			if flags&bit != 0 {
				reasons = append(reasons, name)
			}
		}
		// Small fixed vocabulary: sorting makes diagnostics deterministic.
		for i := 0; i < len(reasons); i++ {
			for j := i + 1; j < len(reasons); j++ {
				if reasons[j] < reasons[i] {
					reasons[i], reasons[j] = reasons[j], reasons[i]
				}
			}
		}
		freshness := Fresh
		if flags&rStale != 0 {
			freshness = Stale
		} else if flags != 0 {
			freshness = FreshnessUnknown
		}
		item.Readiness = Readiness{Status: status, Reasons: reasons, Freshness: freshness}
		out[key] = item
	}
	return out
}

// projectsHardEdges reports whether legacy dependencies are exactly the item's
// hard-edge prerequisites, as the contract requires when both fields are supplied.
func projectsHardEdges(key string, dependencies []Ref, relationships []Relationship) bool {
	hard := make(map[string]bool)
	for _, edge := range relationships {
		if edge.From.Key() == key && (edge.Type == HardPrerequisite || edge.Type == CrossSourcePrerequisite) {
			hard[edge.To.Key()] = true
		}
	}
	legacy := make(map[string]bool, len(dependencies))
	for _, dependency := range dependencies {
		if !hard[dependency.Key()] {
			return false
		}
		legacy[dependency.Key()] = true
	}
	return len(legacy) == len(hard)
}

func EvaluateAction(item Item, action Action) Eligibility {
	if action == ActionReportBlocked || action == ActionRecordProgress {
		if item.Claim.Known && item.Claim.Active && item.Claim.OwnerVerified {
			return Eligibility{Eligible: true, Reasons: append(append([]string{}, item.Readiness.Reasons...), "owner-maintenance-only"), Requires: []string{"authorization", "verified-claim", "provider-receipt"}}
		}
		return Eligibility{Eligible: false, Reasons: []string{"verified-owner-required"}, Outcome: "ineligible"}
	}
	if action == ActionStart || action == ActionResume {
		if item.Terminal || item.State == StateComplete {
			return Eligibility{Eligible: false, Reasons: []string{"already-terminal"}, Outcome: "complete"}
		}
		if item.Readiness.Status != Ready {
			outcome := "ineligible"
			for _, reason := range item.Readiness.Reasons {
				if reason == "unsupported-condition" {
					outcome = "capability"
				}
			}
			return Eligibility{Eligible: false, Reasons: append([]string{}, item.Readiness.Reasons...), Outcome: outcome}
		}
		if item.Claim.Active {
			return Eligibility{Eligible: false, Reasons: []string{"active-claim"}, Outcome: "active-claims"}
		}
		// The queue's resume policy: only unclaimed work the source reports in progress.
		if action == ActionResume && item.State != StateInProgress {
			return Eligibility{Eligible: false, Reasons: []string{"not-in-progress"}, Outcome: "ineligible"}
		}
		return Eligibility{Eligible: true}
	}
	if action == ActionComplete {
		return Eligibility{Eligible: false, Reasons: []string{"completion-evidence-required"}, Outcome: "capability"}
	}
	return Eligibility{Eligible: false, Reasons: []string{"unsupported-action"}, Outcome: "capability"}
}
