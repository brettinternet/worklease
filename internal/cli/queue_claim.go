package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
	tea "github.com/charmbracelet/bubbletea"
)

const queueClaimHold = time.Hour

type queueClaimController struct {
	backend      *authorityContext
	registry     *queue.Registry
	sources      map[string]queue.Source
	claimSources map[string]queue.ClaimSource
	queueSession string
	paths        config.ProfilePaths
	current      func() (queue.ClaimAuthority, uint64)
	blocked      func() bool
	profile      *config.Profile
	profileName  string
	home         string
}

type queueClaimPlan struct {
	item     queue.Item
	source   queue.ClaimSource
	adapter  queue.Adapter
	keys     []string
	preview  queueui.ClaimPreview
	handle   string
	existing *handle.Handle
}

func (c *queueClaimController) Preview(ctx context.Context, item queue.Item) tea.Cmd {
	return func() tea.Msg {
		plan, err := c.prepare(ctx, item)
		if err != nil {
			return queueui.ClaimPreviewMsg{Identity: queueIdentity(item), Err: err}
		}
		return queueui.ClaimPreviewMsg{Identity: queueIdentity(item), Item: plan.item, Preview: &plan.preview}
	}
}

func (c *queueClaimController) AcquireClaim(ctx context.Context, item queue.Item, preview queueui.ClaimPreview) tea.Cmd {
	return func() tea.Msg {
		plan, err := c.prepare(ctx, item)
		if err == nil && !sameQueueClaimPreview(preview, plan.preview) {
			err = reason.New(reason.ReasonOperationRequestMismatch, "claim preview no longer matches current authority, resources, session, or limits; reopen the preview")
		}
		if err != nil {
			return queueui.ClaimResultMsg{Identity: queueIdentity(item), Err: err}
		}
		grant, err := c.acquire(ctx, plan)
		if err != nil {
			return queueui.ClaimResultMsg{Identity: queueIdentity(item), Item: plan.item, Err: err}
		}
		return queueui.ClaimResultMsg{Identity: queueIdentity(item), Item: plan.item, Claim: queue.ClaimObservation{Known: true, Available: false, Active: true, OwnerVerified: true, AuthorityID: grant.AuthorityID, State: "held", AgentID: grant.AgentID, SessionID: grant.SessionID, AcquiredAt: grant.AcquiredAt, ExpiresAt: grant.ExpiresAt, ObservedAt: time.Now().UTC()}, GrantedTTL: grant.ExpiresAt.Sub(grant.AcquiredAt)}
	}
}

func (c *queueClaimController) prepare(ctx context.Context, item queue.Item) (queueClaimPlan, error) {
	if c.blocked != nil && c.blocked() {
		return queueClaimPlan{}, reason.New(reason.ReasonAuthorityMismatch, "claim actions stopped because authority identity changed; recover the existing queue handle before resubscribing")
	}
	selected, err := c.selectedAuthority()
	if err != nil {
		return queueClaimPlan{}, err
	}
	if err := c.profileIdentityCurrent(); err != nil {
		return queueClaimPlan{}, err
	}
	if err := c.checkQueueHandles(selected); err != nil {
		return queueClaimPlan{}, err
	}
	fresh, err := refreshQueueActionClosure(ctx, c.registry, c.sources, item)
	if err != nil {
		return queueClaimPlan{}, err
	}
	providerCandidate := fresh
	providerCandidate.Claim = queue.ClaimObservation{}
	providerEligibility := queue.EvaluateAction(providerCandidate, queue.ActionStart)
	if !providerEligibility.Eligible {
		return queueClaimPlan{}, reason.New(reason.ReasonInvalidArgument, "claim unavailable: "+strings.Join(providerEligibility.Reasons, ", "))
	}
	source, ok := c.claimSources[item.Ref.SourceID]
	if !ok {
		return queueClaimPlan{}, reason.New(reason.ReasonInvalidArgument, "claim source unavailable")
	}
	adapter, ok := c.registry.Get(source.Source.Adapter)
	if !ok {
		return queueClaimPlan{}, reason.New(reason.ReasonInvalidArgument, "claim source adapter unavailable")
	}
	inputs := queue.IdentityInputs(source, selected.ID)
	resourceKey, err := resource.Resolve(resource.Input{Provider: inputs.Policy, Source: inputs.Source, Item: fresh.Ref.ItemID})
	if err != nil {
		return queueClaimPlan{}, err
	}
	fresh.Resources = []string{resourceKey.Resource}
	fresh.KeyInputs = &resource.Input{Provider: resourceKey.Provider, Source: resourceKey.Source, Item: resourceKey.Item}
	identities, err := config.LoadQueueIdentities(os.Getenv)
	if err != nil {
		return queueClaimPlan{}, reason.New("identity-unknown", "private queue identity record unavailable")
	}
	keys, err := queue.PreAcquireIdentity(ctx, source, adapter, selected, identities.Sources[source.Source.ID], fresh)
	if err != nil {
		return queueClaimPlan{}, err
	}
	if len(keys) == 0 || len(fresh.Resources) != 1 || fresh.Resources[0] == "" {
		return queueClaimPlan{}, reason.New(reason.ReasonInvalidResource, "queue claim resource is unavailable")
	}
	if !containsResource(keys, fresh.Resources[0]) {
		return queueClaimPlan{}, reason.New("identity-unknown", "current resource is outside the confirmed claim domain")
	}
	observed := queue.OverlayClaims(ctx, []queue.Item{fresh}, map[string]queue.ClaimSource{source.Source.ID: source}, selected, c.paths, os.Getenv)[0]
	if observed.Claim.Reason != "" {
		detail := observed.Claim.Detail
		if detail == "" {
			detail = "claim authority observation is unavailable"
		}
		return queueClaimPlan{}, reason.New(observed.Claim.Reason, detail)
	}
	if !observed.Claim.Known || observed.Claim.Stale || observed.Claim.AuthorityID != selected.ID {
		return queueClaimPlan{}, reason.New("claim-unknown", "claim unavailable: authority observation is stale or unknown")
	}
	if observed.Claim.Active {
		return queueClaimPlan{}, reason.New(reason.ReasonAlreadyClaimed, "resource is already claimed").With("holder", map[string]any{"agentId": observed.Claim.AgentID, "expiresAt": observed.Claim.ExpiresAt.UTC()})
	}
	if !observed.Claim.Available {
		return queueClaimPlan{}, reason.New("claim-unknown", "claim unavailable: authority has not verified the resource free")
	}
	observed.Claim.AuthorityID = selected.ID
	if eligibility := queue.ClaimActions(observed)[queue.ActionClaim]; !eligibility.Eligible {
		return queueClaimPlan{}, reason.New(reason.ReasonInvalidArgument, "claim unavailable: "+strings.Join(eligibility.Reasons, ", "))
	}
	profileName := c.profileName
	if profileName == "" {
		profileName = config.LocalProfileName
	}
	scope := "local"
	limits := "local TTL max 1h; requested hold 1h; coordination does not exclude provider or other external writers"
	if selected.Remote {
		scope = "remote"
		limits = "remote maxTTL/maxHold are not advertised; server may reject this request; coordination does not exclude provider or other external writers"
	}
	handlePath, err := queueClaimHandlePath(c.home, c.queueSession, profileName, source.Source, fresh.Ref)
	if err != nil {
		return queueClaimPlan{}, err
	}
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(handlePath)); err != nil {
		return queueClaimPlan{}, err
	}
	var existing *handle.Handle
	if previous, readErr := handle.Read(handlePath); readErr == nil {
		if previous.AuthorityID != selected.ID || previous.SessionID != c.queueSession || selected.Remote && previous.RestoreID != c.profile.RestoreID {
			return queueClaimPlan{}, queueHandleIdentityError(handlePath)
		}
		if previous.State == "pending" || previous.PendingRequest != nil || previous.RecoveryRequest != nil {
			return queueClaimPlan{}, queueHandleRecoveryError(handlePath, selected.Remote)
		}
		if previous.State != "ready" {
			return queueClaimPlan{}, reason.New(reason.ReasonHandleMalformed, "queue claim handle is not ready").With("pendingPath", handlePath)
		}
		status, statusErr := c.backend.API.Status(ctx, lease.Selector{AuthorityID: selected.ID, ClaimID: previous.ClaimID})
		if statusErr != nil {
			return queueClaimPlan{}, reason.New("claim-unknown", "existing queue claim status is unavailable; handle was not replaced")
		}
		view := status.Claim
		if view == nil && len(status.Claims) == 1 {
			view = &status.Claims[0]
		}
		if view != nil && view.ClaimID != previous.ClaimID || len(status.Claims) > 1 || len(status.Resources) > 0 {
			return queueClaimPlan{}, reason.New("claim-unknown", "existing queue claim status did not match its handle")
		}
		if view != nil && view.Active {
			return queueClaimPlan{}, reason.New(reason.ReasonAlreadyClaimed, "this queue session already holds the claim").With("holder", map[string]any{"agentId": view.AgentID, "expiresAt": view.ExpiresAt.UTC()})
		}
		existing = &previous
	} else if !isMissingHandle(readErr) {
		return queueClaimPlan{}, readErr
	}
	preview := queueui.ClaimPreview{Identity: queueIdentity(fresh), Title: fresh.Title, AuthorityProfile: profileName, AuthorityID: selected.ID, Scope: scope, SessionID: c.queueSession, Resources: append([]string(nil), keys...), TTL: c.backend.Config.TTL, Hold: queueClaimHold, CoordinationLimits: limits}
	return queueClaimPlan{item: observed, source: source, adapter: adapter, keys: append([]string(nil), keys...), preview: preview, handle: handlePath, existing: existing}, nil
}

func (c *queueClaimController) selectedAuthority() (queue.ClaimAuthority, error) {
	selected, _ := c.current()
	if selected.API == nil || selected.ID == "" {
		return queue.ClaimAuthority{}, reason.New("claim-unknown", "claim authority unavailable")
	}
	if selected.Remote && selected.AdmittedPrefixes == nil {
		return queue.ClaimAuthority{}, reason.New("admission-unknown", "remote authority has not advertised resource admission")
	}
	return selected, nil
}

func (c *queueClaimController) profileIdentityCurrent() error {
	if c.profile == nil {
		return nil
	}
	profiles, _, err := config.LoadProfiles(c.paths)
	if err != nil {
		return reason.New(reason.ReasonAuthorityMismatch, "remote profile identity is unavailable; claim actions stopped")
	}
	current, ok := profiles[c.profileName]
	if !ok || current.AuthorityID != c.profile.AuthorityID || current.RestoreID != c.profile.RestoreID || current.Endpoint != c.profile.Endpoint || current.CertificateSHA256 != c.profile.CertificateSHA256 {
		return reason.New(reason.ReasonAuthorityMismatch, "remote profile identity changed; recover existing queue handles before resubscribing")
	}
	return nil
}

func (c *queueClaimController) checkQueueHandles(selected queue.ClaimAuthority) error {
	profileName := c.profileName
	if profileName == "" {
		profileName = config.LocalProfileName
	}
	dir := queueClaimHandleDir(c.home, c.queueSession, profileName)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := handle.EnsureOwnerPrivateDir(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		h, err := handle.Read(path)
		if err != nil {
			return err
		}
		if h.SessionID != c.queueSession {
			return reason.New(reason.ReasonHandleMalformed, "queue-owned handle session does not match").With("pendingPath", path)
		}
		if h.AuthorityID != selected.ID || selected.Remote && h.RestoreID != c.profile.RestoreID || !selected.Remote && h.RestoreID != "" {
			return queueHandleIdentityError(path)
		}
		if h.State == "pending" || h.PendingRequest != nil || h.RecoveryRequest != nil {
			return queueHandleRecoveryError(path, selected.Remote)
		}
	}
	return nil
}

func (c *queueClaimController) acquire(ctx context.Context, plan queueClaimPlan) (lease.Grant, error) {
	selected, _ := c.current()
	request := lease.AcquireRequest{AuthorityID: selected.ID, ClaimID: randomHex(16), Token: randomHex(32), Resources: append([]string(nil), plan.keys...), AgentID: c.backend.Config.AgentID, SessionID: c.queueSession, WorkKey: strings.Join(plan.keys, ","), TTL: c.backend.Config.TTL, CoordinationOnly: true, HandlePath: plan.handle}
	if selected.Remote {
		request.MaxHold = queueClaimHold
	} else {
		request.RequestNotAfter = time.Now().UTC().Add(24 * time.Hour)
		request.HoldUntil = time.Now().UTC().Add(queueClaimHold)
	}
	if plan.existing != nil && selected.Remote {
		request.PreviousClaimID = plan.existing.ClaimID
		request.PreviousToken = plan.existing.Token
		request.PreviousRevision = plan.existing.Revision
		request.PreviousExpiresAt = plan.existing.ExpiresAt
	}
	if selected.Remote {
		keys, err := c.preAcquireIdentity(ctx, selected, plan)
		if err != nil {
			return lease.Grant{}, err
		}
		if !sameResourceSelection(keys, plan.preview.Resources) {
			return lease.Grant{}, reason.New(reason.ReasonOperationRequestMismatch, "claim resources changed after preview")
		}
		request.Resources = keys
		grant, err := c.backend.API.Acquire(ctx, request)
		if err != nil {
			return lease.Grant{}, mutationFailure(err, request.ClaimID, request.ClaimID, plan.handle)
		}
		return grant, nil
	}
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(plan.handle)); err != nil {
		return lease.Grant{}, err
	}
	lock, err := handle.AcquireLock(ctx, plan.handle+".lock")
	if err != nil {
		return lease.Grant{}, err
	}
	defer lock.Close()
	var current *handle.Handle
	read, readErr := lock.Read(plan.handle)
	if readErr == nil {
		if plan.existing == nil || !reflect.DeepEqual(read, *plan.existing) {
			return lease.Grant{}, reason.New(reason.ReasonHandleInUse, "queue claim handle changed after preview")
		}
		current = &read
	} else if !isMissingHandle(readErr) {
		return lease.Grant{}, readErr
	} else if plan.existing != nil {
		return lease.Grant{}, reason.New(reason.ReasonHandleInUse, "queue claim handle disappeared after preview")
	}
	inputs := acquireInputs(request.AuthorityID, request.ClaimID, request.Resources, request.AgentID, request.SessionID, request.WorkKey, request.TTL, 0, 0, true, false, request.RequestNotAfter)
	inputs["holdUntil"] = request.HoldUntil.UTC().UnixMicro()
	deadline := request.RequestNotAfter
	pending := handle.Handle{SchemaVersion: handle.SchemaVersion, AuthorityID: request.AuthorityID, ClaimID: request.ClaimID, Token: request.Token, Resources: append([]string(nil), request.Resources...), AgentID: request.AgentID, SessionID: request.SessionID, LocalReplaceAllowed: false, State: "pending", PendingRequest: &handle.PendingRequest{OperationID: request.ClaimID, Kind: "acquire", AuthorityID: request.AuthorityID, ClaimID: request.ClaimID, RequestHash: acquireRequestHash(inputs), RequestNotAfter: deadline, Inputs: inputs}}
	if current != nil {
		if err := lock.ReplaceReady(plan.handle, *current, pending); err != nil {
			return lease.Grant{}, err
		}
	} else if err := lock.Write(plan.handle, pending); err != nil {
		return lease.Grant{}, err
	}
	keys, err := c.preAcquireIdentity(ctx, selected, plan)
	if err != nil || !sameResourceSelection(keys, plan.preview.Resources) {
		_ = lock.Remove(plan.handle)
		if err != nil {
			return lease.Grant{}, err
		}
		return lease.Grant{}, reason.New(reason.ReasonOperationRequestMismatch, "claim resources changed after preview")
	}
	request.Resources = keys
	grant, err := c.backend.API.Acquire(ctx, request)
	if err != nil {
		if isDefinitiveNoCommit(err) {
			_ = lock.Remove(plan.handle)
		}
		return lease.Grant{}, mutationFailure(err, request.ClaimID, request.ClaimID, plan.handle)
	}
	ready := pending
	ready.State = "ready"
	ready.Revision, ready.ExpiresAt = grant.Revision, grant.ExpiresAt
	ready.PendingRequest = nil
	if err := lock.Write(plan.handle, ready); err != nil {
		return lease.Grant{}, committedHandleFailure(err, grant.Receipt, plan.handle, request.ClaimID, request.ClaimID)
	}
	return grant, nil
}

func (c *queueClaimController) preAcquireIdentity(ctx context.Context, selected queue.ClaimAuthority, plan queueClaimPlan) ([]string, error) {
	if c.blocked != nil && c.blocked() {
		return nil, reason.New(reason.ReasonAuthorityMismatch, "claim actions stopped because authority identity changed")
	}
	if err := c.profileIdentityCurrent(); err != nil {
		return nil, err
	}
	identities, err := config.LoadQueueIdentities(os.Getenv)
	if err != nil {
		return nil, reason.New("identity-unknown", "private queue identity record unavailable")
	}
	return queue.PreAcquireIdentity(ctx, plan.source, plan.adapter, selected, identities.Sources[plan.source.Source.ID], plan.item)
}

func sameQueueClaimPreview(a, b queueui.ClaimPreview) bool {
	return a.Identity == b.Identity && a.AuthorityProfile == b.AuthorityProfile && a.AuthorityID == b.AuthorityID && a.Scope == b.Scope && a.SessionID == b.SessionID && a.TTL == b.TTL && a.Hold == b.Hold && a.CoordinationLimits == b.CoordinationLimits && sameResourceSelection(a.Resources, b.Resources)
}

func queueClaimHandleDir(home, session, profile string) string {
	sum := sha256.Sum256([]byte(profile))
	return filepath.Join(home, "queue-handles", session, hex.EncodeToString(sum[:]))
}

func queueClaimHandlePath(home, session, profile string, source queue.Source, ref queue.Ref) (string, error) {
	identity, err := json.Marshal([]string{source.ID, source.Locator, ref.SourceID, ref.ItemID})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(identity)
	return filepath.Join(queueClaimHandleDir(home, session, profile), "claim-"+hex.EncodeToString(sum[:])+".json"), nil
}

func queueHandleIdentityError(path string) error {
	return reason.New(reason.ReasonAuthorityMismatch, "queue-owned handle belongs to a different authority incarnation; inspect and recover it before claiming or resubscribing").With("pendingPath", path)
}

func queueHandleRecoveryError(path string, remote bool) error {
	hint := "replay the retained acquire with worklease acquire --handle <pendingPath>, the original resources, session, TTL and --coordination-only"
	if remote {
		hint = "use worklease acquire --handle <pendingPath> with the queue view's authority profile to replay the retained acquire request"
	}
	return reason.New(reason.ReasonRecoveryRequired, "queue-owned pending handle requires exact recovery before another claim").With("pendingPath", path).With("recoveryHint", hint)
}

func refreshQueueActionClosure(ctx context.Context, registry *queue.Registry, sources map[string]queue.Source, selected queue.Item) (queue.Item, error) {
	root, ok := sources[selected.Ref.SourceID]
	if !ok {
		return queue.Item{}, reason.New(reason.ReasonInvalidArgument, "claim source unavailable")
	}
	adapter, ok := registry.Get(root.Adapter)
	if !ok {
		return queue.Item{}, reason.New(reason.ReasonInvalidArgument, "claim source adapter unavailable")
	}
	if backlog, ok := adapter.(*queue.BacklogAdapter); ok {
		closure, err := backlog.RefreshActionClosure(ctx, root, selected.Ref)
		if err != nil {
			return queue.Item{}, fmt.Errorf("fresh provider prerequisite closure unavailable: %w", err)
		}
		fresh, ok := closure[selected.Ref.Key()]
		if !ok || fresh.Ref != selected.Ref {
			return queue.Item{}, reason.New("identity-changed", "selected item is absent from its fresh prerequisite closure")
		}
		fresh.CanonicalID = selected.CanonicalID
		return fresh, nil
	}
	closure := make(map[string]queue.Item)
	visiting := make(map[string]bool)
	var visit func(queue.Ref) error
	visit = func(ref queue.Ref) error {
		if _, exists := closure[ref.Key()]; exists || visiting[ref.Key()] {
			return nil
		}
		source, exists := sources[ref.SourceID]
		if !exists {
			return fmt.Errorf("fresh prerequisite %s is outside the configured source scope", ref.String())
		}
		adapter, exists := registry.Get(source.Adapter)
		if !exists {
			return fmt.Errorf("fresh prerequisite adapter is unavailable")
		}
		outcomes := adapter.ReadItems(ctx, source, []queue.Ref{ref}, nil, 100)
		var item *queue.Item
		for _, outcome := range outcomes {
			if outcome.Ref == ref && outcome.Item != nil && outcome.Err == nil && outcome.Kind == "found" {
				copy := *outcome.Item
				copy.ReadOutcome = "found"
				item = &copy
				break
			}
		}
		if item == nil || !item.Fresh || item.ReadPermission == queue.Denied || item.ReadPermission == queue.PermissionUnknown {
			return fmt.Errorf("fresh prerequisite item read is incomplete")
		}
		visiting[ref.Key()] = true
		var edges []queue.Relationship
		cursor := ""
		seen := map[string]bool{}
		for {
			page, err := adapter.ReadDependencies(ctx, source, ref, cursor, 100)
			if err != nil || page.Completeness != queue.CoverageComplete {
				if err == nil {
					err = fmt.Errorf("dependency coverage is incomplete")
				}
				return fmt.Errorf("fresh prerequisite closure unavailable: %w", err)
			}
			edges = append(edges, page.Edges...)
			cursor = page.NextCursor
			if cursor == "" {
				break
			}
			if seen[cursor] {
				return fmt.Errorf("dependency pagination repeated a cursor")
			}
			seen[cursor] = true
		}
		item.Relationships, item.DependenciesKnown, item.Closure = edges, true, queue.CoverageComplete
		if item.Observation.ObservedAt.IsZero() {
			item.Observation.ObservedAt = time.Now().UTC()
		}
		closure[ref.Key()] = *item
		for _, edge := range edges {
			if edge.From != ref || edge.Type != queue.HardPrerequisite && edge.Type != queue.CrossSourcePrerequisite {
				continue
			}
			if err := visit(edge.To); err != nil {
				return err
			}
		}
		visiting[ref.Key()] = false
		return nil
	}
	if err := visit(selected.Ref); err != nil {
		return queue.Item{}, err
	}
	computed := queue.Recompute(closure, queue.CoverageComplete)
	fresh, ok := computed[selected.Ref.Key()]
	if !ok {
		return queue.Item{}, reason.New("identity-changed", "selected item is absent from its fresh prerequisite closure")
	}
	fresh.CanonicalID = selected.CanonicalID
	return fresh, nil
}

func containsResource(resources []string, expected string) bool {
	for _, resource := range resources {
		if resource == expected {
			return true
		}
	}
	return false
}
