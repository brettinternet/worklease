// Package lease implements the local, transactional claim authority. It has no
// knowledge of command-line paths, handles, transports, or provider APIs.
package lease

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

const (
	minTTL        = time.Second
	maxTTL        = time.Hour
	maxCheckpoint = 8 * 1024
)

// beforeClaimResourceInsert is test-only fault injection used to prove that a
// failed member insert rolls the complete acquisition transaction back.
var beforeClaimResourceInsert func(int, string) error

type Clock interface {
	Now() time.Time
	Monotonic() time.Duration
}
type realClock struct{ started time.Time }

func (c realClock) Now() time.Time           { return time.Now() }
func (c realClock) Monotonic() time.Duration { return time.Since(c.started) }

// IDGenerator supplies identifiers and is deliberately separate from the
// authority so clients can persist their generated credentials before acquire.
type IDGenerator interface{ Generate() string }
type RandomIDs struct{}

func (RandomIDs) Generate() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%032x", atomic.AddUint64(&fallbackID, 1))
	}
	return hex.EncodeToString(b[:])
}

var fallbackID uint64

type Defaults struct{ TTL, PollInterval time.Duration }

type Service struct {
	st       *store.Store
	clock    Clock
	ids      IDGenerator
	defaults Defaults
	remote   *RemotePolicy
}

func New(st *store.Store, clock Clock, ids IDGenerator, defaults Defaults) *Service {
	if clock == nil {
		clock = realClock{started: time.Now()}
	}
	if ids == nil {
		ids = RandomIDs{}
	}
	if defaults.TTL == 0 {
		defaults.TTL = 15 * time.Minute
	}
	if defaults.PollInterval == 0 {
		defaults.PollInterval = 250 * time.Millisecond
	}
	return &Service{st: st, clock: clock, ids: ids, defaults: defaults}
}

// DefaultTTL is the normalized lifecycle default used by client handles.
func (s *Service) DefaultTTL() time.Duration { return s.defaults.TTL }

// NormalizeCheckpoint applies the authority's strict checkpoint validation
// before a client persists a pending lifecycle request.
func NormalizeCheckpoint(data []byte) ([]byte, error) { return strictCheckpoint(data) }

type Credentials struct {
	AuthorityID, ClaimID, Token string
	Revision                    int64
	Actor                       *RemoteActor
	// HandlePath identifies the durable named-handle credential source used for recovery.
	HandlePath string
	// CredentialPath identifies the durable owner-private token source for handleless recovery.
	CredentialPath string
}
type AcquireRequest struct {
	AuthorityID, ClaimID, Token string
	Resources                   []string
	AgentID, SessionID, WorkKey string
	TTL                         time.Duration
	RequestNotAfter             time.Time
	LocalReplaceAllowed         bool
	CoordinationOnly            bool
	// HoldUntil is an authority-enforced expiry ceiling for handle-backed leases.
	HoldUntil time.Time
	// HandlePath identifies the durable named-handle slot for exact recovery.
	HandlePath string
	// CredentialPath identifies the durable owner-private new-claim token source for handleless recovery.
	CredentialPath string
	// MaxHold is remote new-admission input. Remote lifecycle derives the
	// resulting absolute deadline from authority time and persists it.
	MaxHold time.Duration
	Actor   *RemoteActor
	// LegacyRequestHash permits only an adapter-recorded pre-hold-binding request
	// to replay. New operations always persist the hold-bound request hash.
	LegacyRequestHash  string
	Wait, PollInterval time.Duration
}
type Grant struct {
	ClaimID             string     `json:"claimId"`
	Resources           []string   `json:"resources"`
	AgentID             string     `json:"agentId"`
	SessionID           string     `json:"sessionId"`
	WorkKey             string     `json:"workKey"`
	Revision            int64      `json:"revision"`
	AcquiredAt          time.Time  `json:"acquiredAt"`
	ExpiresAt           time.Time  `json:"expiresAt"`
	Guarantee           string     `json:"guarantee"`
	AuthorityID         string     `json:"authorityId"`
	LocalReplaceAllowed bool       `json:"localReplaceAllowed"`
	Active              bool       `json:"active"`
	Receipt             Receipt    `json:"receipt"`
	Recovery            []Recovery `json:"recovery,omitempty"`
	UnknownOperations   []string   `json:"unknownOperations,omitempty"`
	InstallationID      string     `json:"installationId,omitempty"`
	RestoreID           string     `json:"restoreId,omitempty"`
}
type Recovery struct {
	Resource          string `json:"resource"`
	ClaimID           string `json:"claimId"`
	CheckpointPresent bool   `json:"checkpointPresent"`
	Checkpoint        any    `json:"checkpoint,omitempty"`
}
type Selector struct {
	AuthorityID, ClaimID, Resource string
	Resources                      []string
	Actor                          *RemoteActor
}
type ClaimView struct {
	ClaimID             string    `json:"claimId"`
	Resources           []string  `json:"resources"`
	AgentID             string    `json:"agentId"`
	SessionID           string    `json:"sessionId"`
	WorkKey             string    `json:"workKey"`
	Guarantee           string    `json:"guarantee"`
	AuthorityID         string    `json:"authorityId"`
	Revision            int64     `json:"revision"`
	AcquiredAt          time.Time `json:"acquiredAt"`
	HeartbeatAt         time.Time `json:"heartbeatAt"`
	ExpiresAt           time.Time `json:"expiresAt"`
	LocalReplaceAllowed bool      `json:"localReplaceAllowed"`
	Active              bool      `json:"active"`
	CheckpointPresent   bool      `json:"checkpointPresent"`
	UnknownOperations   []string  `json:"unknownOperations,omitempty"`
	InstallationID      string    `json:"installationId,omitempty"`
	RestoreID           string    `json:"restoreId,omitempty"`
}
type ResourceStatus struct {
	Resource string     `json:"resource"`
	State    string     `json:"state"`
	Claim    *ClaimView `json:"claim,omitempty"`
}
type Status struct {
	Claim     *ClaimView       `json:"claim,omitempty"`
	Claims    []ClaimView      `json:"claims"`
	Resources []ResourceStatus `json:"resources"`
}
type Renew struct {
	OperationID     string
	TTL             time.Duration
	RequestNotAfter time.Time
	// HoldUntil prevents a renewal from extending beyond the client's durable hold.
	HoldUntil         time.Time
	LegacyRequestHash string
}
type CheckpointRequest struct {
	OperationID       string
	TTL               time.Duration
	Data              json.RawMessage
	RequestNotAfter   time.Time
	HoldUntil         time.Time
	LegacyRequestHash string
}
type ReleaseRequest struct {
	OperationID, Reason string
	RequestNotAfter     time.Time
}
type TransferRequest struct {
	OperationID, SuccessorClaimID, SuccessorToken string
	ToAgent, ToSession, ToWorkKey                 string
	TTL                                           time.Duration
	RequestNotAfter                               time.Time
	// SuccessorCredentialPath is the durable token source used for exact remote replay.
	SuccessorCredentialPath string
	// SuccessorHandlePath is the client-local destination for the confirmed successor claim.
	SuccessorHandlePath string
}
type Receipt struct {
	OperationID string         `json:"operationId"`
	ClaimID     string         `json:"claimId"`
	Kind        string         `json:"kind"`
	RequestHash string         `json:"requestSha256"`
	Revision    int64          `json:"revision"`
	Idempotent  bool           `json:"idempotent"`
	Committed   bool           `json:"committed"`
	Result      map[string]any `json:"result,omitempty"`
}
type Verification struct {
	Claim             ClaimView `json:"claim"`
	UnknownOperations []string  `json:"unknownOperations,omitempty"`
}
type OperationIntent struct {
	OperationID, Kind, RequestHash string
	Request                        map[string]any
	RequestNotAfter                time.Time
	TTL                            time.Duration
}
type Started struct {
	OperationID string   `json:"operationId"`
	ClaimID     string   `json:"claimId"`
	Kind        string   `json:"kind"`
	Revision    int64    `json:"revision"`
	RequestHash string   `json:"requestSha256"`
	Completed   bool     `json:"completed"`
	Receipt     *Receipt `json:"receipt,omitempty"`
}

func (s *Service) Acquire(ctx context.Context, req AcquireRequest) (Grant, error) {
	if req.Wait > 0 {
		deadline := s.clock.Monotonic() + req.Wait
		poll := req.PollInterval
		if poll <= 0 {
			poll = s.defaults.PollInterval
		}
		req.Wait, req.PollInterval = 0, 0
		for {
			grant, err := s.Acquire(ctx, req)
			if err == nil {
				return grant, nil
			}
			e := reason.As(err)
			if e == nil || e.Reason != reason.ReasonAlreadyClaimed {
				return Grant{}, err
			}
			if s.clock.Monotonic() >= deadline {
				return Grant{}, reason.New(reason.ReasonWaitTimeout, "bounded acquire wait expired").With("holder", e.Details["holder"])
			}
			remaining := deadline - s.clock.Monotonic()
			delay := poll
			jitter := poll / 5
			if jitter > 0 {
				if int64(s.clock.Monotonic()/time.Nanosecond)%2 == 0 {
					delay += jitter
				} else {
					delay -= jitter
				}
			}
			if delay > remaining {
				delay = remaining
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return Grant{}, ctx.Err()
			case <-timer.C:
			}
		}
	}
	if err := validateResources(req.Resources); err != nil {
		return Grant{}, err
	}
	if req.ClaimID == "" {
		return Grant{}, reason.Invalid("claim ID is required until handle-backed acquisition is available")
	}
	if req.RequestNotAfter.IsZero() {
		return Grant{}, reason.Invalid("requestNotAfter is required until handle-backed acquisition is available")
	}
	if err := validateToken(req.Token); err != nil {
		return Grant{}, err
	}
	ttl := req.TTL
	if ttl == 0 {
		ttl = s.defaults.TTL
	}
	if err := validateTTL(ttl); err != nil {
		return Grant{}, err
	}
	if req.Actor != nil && (req.Wait != 0 || req.PollInterval != 0 || req.LocalReplaceAllowed || !req.HoldUntil.IsZero() || req.LegacyRequestHash != "") {
		return Grant{}, reason.Invalid("remote acquire contains local-only fields")
	}
	now := s.clock.Now()
	deadline := req.RequestNotAfter
	if !deadline.After(now) || deadline.After(now.Add(24*time.Hour)) {
		return Grant{}, reason.New(reason.ReasonReplayExpired, "request replay deadline must be within 24 hours")
	}
	claimID := req.ClaimID
	if claimID == "" {
		claimID = s.ids.Generate()
	}
	if !validID(claimID) {
		return Grant{}, reason.Invalid("claim ID must be 32 lowercase hex characters")
	}
	agent, session := strings.TrimSpace(req.AgentID), strings.TrimSpace(req.SessionID)
	if agent == "" {
		return Grant{}, reason.New(reason.ReasonAgentIDRequired, "agent identity is required")
	}
	if session == "" {
		session = s.ids.Generate()
	}
	if err := validateIdentity("agent ID", agent); err != nil {
		return Grant{}, err
	}
	if err := validateIdentity("session ID", session); err != nil {
		return Grant{}, err
	}
	work := req.WorkKey
	if work == "" {
		work = strings.Join(req.Resources, ",")
	}
	if err := validatePublicText("work key", work); err != nil {
		return Grant{}, err
	}
	authority := req.AuthorityID
	if authority == "" {
		return Grant{}, reason.Invalid("authority ID is required for stateless acquisition")
	}
	if req.Actor == nil && authority != s.st.AuthorityID() {
		return Grant{}, reason.New(reason.ReasonAuthorityMismatch, "authority identity does not match")
	}
	intent := map[string]any{"kind": "acquire", "authorityId": authority, "claimId": claimID, "resources": req.Resources, "agentId": agent, "sessionId": session, "workKey": work, "ttl": ttl.Microseconds(), "requestNotAfter": deadline.UTC().UnixMicro(), "localReplaceAllowed": req.LocalReplaceAllowed, "coordinationOnly": req.CoordinationOnly}
	if req.Actor != nil {
		intent["protocolVersion"], intent["expectedRestoreId"], intent["installationId"], intent["maxHold"] = "worklease-http/1", req.Actor.ExpectedRestoreID, req.Actor.InstallationID, req.MaxHold.Microseconds()
	}
	var hash string
	if req.Actor == nil {
		hash = lifecycleRequestHash(intent, req.HoldUntil)
	}
	tokenHash := hashToken(req.Token)
	var result Grant
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		effective, err := s.effectiveRemoteNow(tx, req.Actor, "write", now)
		if err != nil {
			return err
		}
		if req.Actor != nil && req.AuthorityID != req.Actor.AuthorityID {
			return reason.New(reason.ReasonAuthorityMismatch, "authority identity does not match")
		}
		if req.Actor != nil {
			intent["installationId"] = req.Actor.InstallationID
			intent["protocolVersion"] = "worklease-http/1"
			hash = lifecycleRequestHash(intent, req.HoldUntil)
		}
		// Acquire is idempotent on claim ID. Authenticate a replay against the
		// retained epoch even when the current claim has already ended.
		var op operationRow
		found, err := readOperation(tx, claimID, claimID, &op)
		if err != nil {
			return err
		}
		if found {
			if subtle.ConstantTimeCompare([]byte(op.TokenHash), []byte(tokenHash)) != 1 {
				return reason.New(reason.ReasonInvalidToken, "credential is invalid")
			}
			if op.Remote && (req.Actor == nil || req.Actor.InstallationID != op.InstallationID) {
				return reason.New(reason.ReasonAuthorizationDenied, "operation belongs to another installation")
			}
			if !requestHashMatches(op.RequestHash, hash, req.LegacyRequestHash) {
				return reason.New(reason.ReasonOperationRequestMismatch, "request intent differs from the recorded operation")
			}
			if effective.UnixMicro() >= op.RequestNotAfter {
				return reason.New(reason.ReasonReplayExpired, "request replay deadline has passed")
			}
			result, err = grantFromClaimOrEpoch(tx, claimID, op.Receipt, true, s.st.AuthorityID(), effective.UnixMicro())
			if err == nil {
				result.Receipt.RequestHash = op.RequestHash
			}
			return err
		}
		unknown, required, err := unresolvedRecoveryClosure(tx, req.Resources)
		if err != nil {
			return err
		}
		inRecovery, _, err := recoveryMode(tx)
		if err != nil {
			return err
		}
		if req.Actor != nil && inRecovery && len(unknown) == 0 {
			return reason.New(reason.ReasonRecoveryRequired, "recovery acquire must cover unresolved predecessor work")
		}
		if len(required) > 32 {
			return reason.New(reason.ReasonRecoveryRequired, "recovery closure exceeds 32 resources").With("requiredResources", required).With("operations", unknown)
		}
		if !sameResourceMembers(req.Resources, required) {
			return reason.New(reason.ReasonOperationInProgress, "acquire must cover every resource of unresolved predecessor operations").With("requiredResources", required).With("operations", unknown)
		}
		if req.Actor != nil {
			if inRecovery {
				if err := s.remoteAdmissionLimits(ttl, req.MaxHold); err != nil {
					return err
				}
			} else if _, err := s.remoteAdmission(req.Resources, ttl, req.MaxHold); err != nil {
				return err
			}
		}
		result.UnknownOperations = append(result.UnknownOperations, unknown...)
		// Preflight the entire request before changing any projection. Preserve
		// caller order for contention and recovery while retiring each distinct
		// expired predecessor only once.
		expired := map[string]claimRow{}
		var expiredOrder []string
		for _, resource := range req.Resources {
			var old claimRow
			exists, err := readClaimForResource(tx, resource, &old)
			if err != nil {
				return err
			}
			if !exists {
				if recovery, found, e := latestRecovery(tx, resource); e != nil {
					return e
				} else if found {
					result.Recovery = append(result.Recovery, recovery)
				}
				continue
			}
			if old.ExpiresAt > effective.UnixMicro() {
				return reason.New(reason.ReasonAlreadyClaimed, "resource is already claimed").With("resource", resource).With("holder", map[string]any{"claimId": old.ClaimID, "agentId": old.AgentID, "workKey": old.WorkKey, "expiresAt": formatMicros(old.ExpiresAt)})
			}
			if _, found := expired[old.ClaimID]; !found {
				expired[old.ClaimID] = old
				expiredOrder = append(expiredOrder, old.ClaimID)
			}
			if old.Checkpoint != "" {
				var checkpoint any
				if e := json.Unmarshal([]byte(old.Checkpoint), &checkpoint); e != nil {
					return storage(e)
				}
				result.Recovery = append(result.Recovery, Recovery{Resource: resource, ClaimID: old.ClaimID, CheckpointPresent: true, Checkpoint: checkpoint})
			} else {
				result.Recovery = append(result.Recovery, Recovery{Resource: resource, ClaimID: old.ClaimID})
			}
		}
		for _, oldID := range expiredOrder {
			if err := endClaim(tx, expired[oldID], effective.UnixMicro(), "expired", "expired-replaced", nil); err != nil {
				return err
			}
		}
		acquired := effective.UnixMicro()
		expires := acquired + ttl.Microseconds()
		admittedTTL, admittedHold := int64(0), int64(0)
		installation, restore, remote := "", "", 0
		if req.Actor != nil {
			admittedTTL = s.remote.MaxTTL.Microseconds()
			admittedHold = acquired + req.MaxHold.Microseconds()
			installation, restore, remote = req.Actor.InstallationID, s.st.RestoreID(), 1
			if expires > admittedHold {
				expires = admittedHold
			}
		} else if !req.HoldUntil.IsZero() && expires > req.HoldUntil.UnixMicro() {
			expires = req.HoldUntil.UnixMicro()
		}
		if expires <= acquired {
			return reason.New(reason.ReasonClaimExpired, "lease hold deadline has passed")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO claims(claim_id,token_hash,revision,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,ttl_us,heartbeat_at,expires_at,checkpoint,admitted_ttl_us,admitted_hold_until,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,NULL,?,?,?,?,?)`, claimID, tokenHash, 1, agent, session, work, "local-coordination", boolInt(req.LocalReplaceAllowed && !req.CoordinationOnly), acquired, ttl.Microseconds(), acquired, expires, nullInt(admittedTTL), nullInt(admittedHold), nullString(installation), nullString(restore), remote); err != nil {
			return storage(err)
		}
		for i, resource := range req.Resources {
			if beforeClaimResourceInsert != nil {
				if err := beforeClaimResourceInsert(i, resource); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO claim_resources(resource,claim_id,position) VALUES(?,?,?)`, resource, claimID, i); err != nil {
				return storage(err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO epochs(claim_id,token_hash,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,acquired_seq,checkpoint,admitted_ttl_us,admitted_hold_until,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,NULL,?,?,?,?,?)`, claimID, tokenHash, agent, session, work, "local-coordination", boolInt(req.LocalReplaceAllowed && !req.CoordinationOnly), acquired, 0, nullInt(admittedTTL), nullInt(admittedHold), nullString(installation), nullString(restore), remote); err != nil {
			return storage(err)
		}
		for i, resource := range req.Resources {
			if _, err := tx.ExecContext(ctx, `INSERT INTO epoch_resources(claim_id,resource,position) VALUES(?,?,?)`, claimID, resource, i); err != nil {
				return storage(err)
			}
		}
		rev := int64(1)
		event := store.Event{At: time.UnixMicro(acquired), Kind: "acquired", ClaimID: claimID, Resources: req.Resources, Revision: &rev, AgentID: agent}
		if req.Actor != nil {
			event.InstallationID, event.RestoreID, event.Remote = installation, restore, true
		}
		seq, err := tx.AppendEvent(event)
		if err != nil {
			return storage(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE epochs SET acquired_seq=? WHERE claim_id=?`, seq, claimID); err != nil {
			return storage(err)
		}
		receipt := map[string]any{"claimId": claimID, "revision": int64(1), "resources": req.Resources, "expiresAt": formatMicros(expires)}
		if len(result.Recovery) > 0 {
			receipt["recovery"] = result.Recovery
		}
		if len(result.UnknownOperations) > 0 {
			receipt["unknownOperations"] = result.UnknownOperations
		}
		encoded, _ := json.Marshal(receipt)
		if _, err := tx.ExecContext(ctx, `INSERT INTO operations(claim_id,operation_id,kind,request_hash,request_not_after,expected_revision,state,receipt,started_at,started_seq,completed_at,completed_seq,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, claimID, claimID, "acquire", hash, deadline.UnixMicro(), 1, "completed", string(encoded), acquired, seq, acquired, seq, nullString(installation), nullString(restore), remote); err != nil {
			return storage(err)
		}
		result.ClaimID = claimID
		result.Resources = append([]string(nil), req.Resources...)
		result = Grant{ClaimID: claimID, Resources: append([]string(nil), req.Resources...), UnknownOperations: append([]string(nil), result.UnknownOperations...), Recovery: append([]Recovery(nil), result.Recovery...), AgentID: agent, SessionID: session, WorkKey: work, Revision: 1, AcquiredAt: time.UnixMicro(acquired).UTC(), ExpiresAt: time.UnixMicro(expires).UTC(), Guarantee: "local-coordination", AuthorityID: authority, LocalReplaceAllowed: req.LocalReplaceAllowed && !req.CoordinationOnly, Active: true, InstallationID: installation, RestoreID: restore, Receipt: Receipt{OperationID: claimID, ClaimID: claimID, Kind: "acquire", RequestHash: hash, Revision: 1, Committed: true, Result: receipt}}
		return nil
	})
	if err != nil {
		return Grant{}, err
	}
	return result, nil
}

func (s *Service) Status(ctx context.Context, sel Selector) (Status, error) {
	if sel.Actor == nil && sel.AuthorityID != "" && sel.AuthorityID != s.st.AuthorityID() {
		return Status{}, reason.New(reason.ReasonAuthorityMismatch, "authority identity does not match")
	}
	var out Status
	err := s.st.Read(ctx, func(tx *store.Tx) error {
		if err := s.authorizeRemote(tx, sel.Actor, "read"); err != nil {
			return err
		}
		if sel.Actor != nil && sel.AuthorityID != sel.Actor.AuthorityID {
			return reason.New(reason.ReasonAuthorityMismatch, "authority identity does not match")
		}
		if sel.ClaimID != "" {
			var row claimRow
			ok, err := readClaim(tx, sel.ClaimID, &row)
			if err != nil {
				return err
			}
			if ok {
				v, err := row.view(s.st.AuthorityID(), s.clock.Now().UnixMicro())
				if err != nil {
					return err
				}
				unknown, err := unresolvedForResources(tx, row.Resources)
				if err != nil {
					return err
				}
				v.UnknownOperations = unknown
				out.Claim = &v
			}
			return nil
		}
		resources := sel.Resources
		if sel.Resource != "" {
			resources = []string{sel.Resource}
		}
		if len(resources) == 0 {
			return nil
		}
		seen := map[string]bool{}
		for _, resource := range resources {
			var row claimRow
			ok, err := readClaimForResource(tx, resource, &row)
			if err != nil {
				return err
			}
			if !ok {
				out.Resources = append(out.Resources, ResourceStatus{Resource: resource, State: "free"})
				continue
			}
			v, e := row.view(s.st.AuthorityID(), s.clock.Now().UnixMicro())
			if e != nil {
				return e
			}
			unknown, e := unresolvedForResources(tx, row.Resources)
			if e != nil {
				return e
			}
			v.UnknownOperations = unknown
			state := "expired"
			if v.Active {
				state = "active"
			}
			out.Resources = append(out.Resources, ResourceStatus{Resource: resource, State: state, Claim: &v})
			if !seen[row.ClaimID] {
				out.Claims = append(out.Claims, v)
				seen[row.ClaimID] = true
			}
		}
		return nil
	})
	return out, err
}
func (s *Service) List(ctx context.Context, filter string, actors ...*RemoteActor) ([]ClaimView, error) {
	var actor *RemoteActor
	if len(actors) > 0 {
		actor = actors[0]
	}
	var out []ClaimView
	err := s.st.Read(ctx, func(tx *store.Tx) error {
		if err := s.authorizeRemote(tx, actor, "read"); err != nil {
			return err
		}
		q := `SELECT DISTINCT c.claim_id,c.token_hash,c.revision,c.agent_id,c.session_id,c.work_key,c.guarantee,c.local_replace_allowed,c.acquired_at,c.ttl_us,c.heartbeat_at,c.expires_at,coalesce(c.checkpoint,''),coalesce(c.admitted_ttl_us,0),coalesce(c.admitted_hold_until,0),coalesce(c.installation_id,''),coalesce(c.restore_id,''),c.remote FROM claims c JOIN claim_resources cr ON cr.claim_id=c.claim_id`
		var args []any
		if filter != "" {
			q += ` WHERE cr.resource=?`
			args = append(args, filter)
		}
		q += ` ORDER BY c.acquired_at,c.claim_id`
		rows, e := tx.QueryContext(ctx, q, args...)
		if e != nil {
			return storage(e)
		}
		defer rows.Close()
		for rows.Next() {
			var r claimRow
			if e := rows.Scan(r.scanArgs()...); e != nil {
				return storage(e)
			}
			if e := loadResources(tx, &r); e != nil {
				return e
			}
			v, e := r.view(s.st.AuthorityID(), s.clock.Now().UnixMicro())
			if e != nil {
				return e
			}
			v.UnknownOperations, e = unresolvedForResources(tx, r.Resources)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) Heartbeat(ctx context.Context, creds Credentials, req Renew) (Receipt, error) {
	ttl := req.TTL
	if ttl == 0 {
		ttl = s.defaults.TTL
	}
	if err := validateTTL(ttl); err != nil {
		return Receipt{}, err
	}
	if req.OperationID == "" || !validID(req.OperationID) {
		return Receipt{}, reason.Invalid("operation ID must be 32 lowercase hex characters")
	}
	now := s.clock.Now()
	if err := validateRequestWindow(req.RequestNotAfter, now); err != nil {
		return Receipt{}, err
	}
	deadline := req.RequestNotAfter
	intent := map[string]any{"kind": "heartbeat", "authorityId": s.st.AuthorityID(), "claimId": creds.ClaimID, "ttl": ttl.Microseconds(), "requestNotAfter": deadline.UTC().UnixMicro()}
	if creds.Actor != nil {
		intent["expectedRestoreId"], intent["installationId"] = creds.Actor.ExpectedRestoreID, creds.Actor.InstallationID
	}
	var hash string
	if creds.Actor == nil {
		hash = lifecycleRequestHash(intent, req.HoldUntil)
	}
	var receipt Receipt
	var err error
	err = s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		effective, e := s.effectiveRemoteNow(tx, creds.Actor, "write", now)
		if e != nil {
			return e
		}
		legacyHash := req.LegacyRequestHash
		if creds.Actor != nil {
			if !req.HoldUntil.IsZero() || req.LegacyRequestHash != "" {
				return reason.Invalid("remote heartbeat contains local-only fields")
			}
			intent["installationId"] = creds.Actor.InstallationID
			intent["protocolVersion"] = "worklease-http/1"
			hash = lifecycleRequestHash(intent, time.Time{})
			legacyHash = ""
		}
		receipt, err = s.mutateCurrent(tx, creds, req.OperationID, "heartbeat", hash, legacyHash, deadline, effective, func(row claimRow, rev int64) (map[string]any, error) {
			if pending, _ := hasStarted(tx, row.ClaimID); pending {
				return nil, reason.New(reason.ReasonOperationInProgress, "a guarded operation is in progress")
			}
			expires, e := extensionExpiry(row, effective.UnixMicro(), ttl, req.HoldUntil)
			if e != nil {
				return nil, e
			}
			if _, e := tx.ExecContext(ctx, `UPDATE claims SET revision=?,ttl_us=?,heartbeat_at=?,expires_at=? WHERE claim_id=?`, rev, ttl.Microseconds(), effective.UnixMicro(), expires, row.ClaimID); e != nil {
				return nil, storage(e)
			}
			return map[string]any{"revision": rev, "expiresAt": formatMicros(expires)}, nil
		})
		return err
	})
	return receipt, err
}

// ValidateCheckpoint canonicalizes and validates checkpoint data without mutating authority state.
// Adapters use it before persisting a pending mutation so rejected input cannot strand a handle.
func ValidateCheckpoint(data []byte, activeToken string) ([]byte, error) {
	if len(data) == 0 || len(data) > maxCheckpoint {
		return nil, reason.Invalid("checkpoint must be canonical JSON no larger than 8 KiB")
	}
	canonical, err := strictCheckpoint(data)
	if err != nil {
		return nil, reason.Invalid("checkpoint must be strict JSON without duplicate or credential-like fields")
	}
	if len(activeToken) == 64 && strings.Contains(string(canonical), activeToken) {
		return nil, reason.Invalid("checkpoint must not contain the active credential")
	}
	if len(canonical) > maxCheckpoint {
		return nil, reason.Invalid("checkpoint exceeds 8 KiB")
	}
	return canonical, nil
}

func (s *Service) Checkpoint(ctx context.Context, creds Credentials, req CheckpointRequest) (Receipt, error) {
	canonical, err := ValidateCheckpoint(req.Data, creds.Token)
	if err != nil {
		return Receipt{}, err
	}
	ttl := req.TTL
	if ttl == 0 {
		ttl = s.defaults.TTL
	}
	if err := validateTTL(ttl); err != nil {
		return Receipt{}, err
	}
	if req.OperationID == "" || !validID(req.OperationID) {
		return Receipt{}, reason.Invalid("operation ID must be 32 lowercase hex characters")
	}
	now := s.clock.Now()
	if err := validateRequestWindow(req.RequestNotAfter, now); err != nil {
		return Receipt{}, err
	}
	deadline := req.RequestNotAfter
	intent := map[string]any{"kind": "checkpoint", "authorityId": s.st.AuthorityID(), "claimId": creds.ClaimID, "ttl": ttl.Microseconds(), "checkpoint": json.RawMessage(canonical), "requestNotAfter": deadline.UTC().UnixMicro()}
	if creds.Actor != nil {
		intent["expectedRestoreId"], intent["installationId"] = creds.Actor.ExpectedRestoreID, creds.Actor.InstallationID
	}
	var hash string
	if creds.Actor == nil {
		hash = lifecycleRequestHash(intent, req.HoldUntil)
	}
	var receipt Receipt
	err = s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		effective, e := s.effectiveRemoteNow(tx, creds.Actor, "write", now)
		if e != nil {
			return e
		}
		legacyHash := req.LegacyRequestHash
		if creds.Actor != nil {
			if !req.HoldUntil.IsZero() || req.LegacyRequestHash != "" {
				return reason.Invalid("remote checkpoint contains local-only fields")
			}
			intent["installationId"] = creds.Actor.InstallationID
			intent["protocolVersion"] = "worklease-http/1"
			hash = lifecycleRequestHash(intent, time.Time{})
			legacyHash = ""
		}
		receipt, e = s.mutateCurrent(tx, creds, req.OperationID, "checkpoint", hash, legacyHash, deadline, effective, func(row claimRow, rev int64) (map[string]any, error) {
			if pending, _ := hasStarted(tx, row.ClaimID); pending {
				return nil, reason.New(reason.ReasonOperationInProgress, "a guarded operation is in progress")
			}
			expires, e := extensionExpiry(row, effective.UnixMicro(), ttl, req.HoldUntil)
			if e != nil {
				return nil, e
			}
			if _, e := tx.ExecContext(ctx, `UPDATE claims SET revision=?,ttl_us=?,heartbeat_at=?,expires_at=?,checkpoint=? WHERE claim_id=?`, rev, ttl.Microseconds(), effective.UnixMicro(), expires, string(canonical), row.ClaimID); e != nil {
				return nil, storage(e)
			}
			return map[string]any{"revision": rev, "expiresAt": formatMicros(expires), "checkpointed": true}, nil
		})
		return e
	})
	return receipt, err
}

func (s *Service) Release(ctx context.Context, creds Credentials, req ReleaseRequest) (Receipt, error) {
	reasonText := strings.TrimSpace(req.Reason)
	if reasonText == "" {
		reasonText = "released"
	}
	if err := validatePublicText("release reason", reasonText); err != nil {
		return Receipt{}, err
	}
	if creds.Token != "" && strings.Contains(reasonText, creds.Token) {
		return Receipt{}, reason.Invalid("release reason must not contain the active credential")
	}
	if req.OperationID == "" || !validID(req.OperationID) {
		return Receipt{}, reason.Invalid("operation ID must be 32 lowercase hex characters")
	}
	now := s.clock.Now()
	if err := validateRequestWindow(req.RequestNotAfter, now); err != nil {
		return Receipt{}, err
	}
	deadline := req.RequestNotAfter
	intent := map[string]any{"kind": "release", "authorityId": s.st.AuthorityID(), "claimId": creds.ClaimID, "reason": reasonText, "requestNotAfter": deadline.UTC().UnixMicro()}
	if creds.Actor != nil {
		intent["expectedRestoreId"], intent["installationId"] = creds.Actor.ExpectedRestoreID, creds.Actor.InstallationID
	}
	var hash string
	if creds.Actor == nil {
		hash = requestHash(intent)
	}
	var receipt Receipt
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		effective, err := s.effectiveRemoteNow(tx, creds.Actor, "write", now)
		if err != nil {
			return err
		}
		if creds.Actor != nil {
			intent["installationId"] = creds.Actor.InstallationID
			intent["protocolVersion"] = "worklease-http/1"
			hash = requestHash(intent)
		}
		receipt, err = s.mutateCurrent(tx, creds, req.OperationID, "release", hash, "", deadline, effective, func(row claimRow, rev int64) (map[string]any, error) {
			pending, _ := hasStarted(tx, row.ClaimID)
			if pending {
				return nil, reason.New(reason.ReasonOperationInProgress, "a guarded operation is in progress")
			}
			return s.releaseInTx(tx, row, effective, rev, req.OperationID, reasonText, hash)
		})
		return err
	})
	return receipt, err
}

func (s *Service) Transfer(ctx context.Context, creds Credentials, req TransferRequest) (Grant, error) {
	if !validID(req.SuccessorClaimID) {
		return Grant{}, reason.Invalid("successor claim ID must be 32 lowercase hex characters")
	}
	if err := validateToken(req.SuccessorToken); err != nil {
		return Grant{}, err
	}
	if req.OperationID == "" || !validID(req.OperationID) {
		return Grant{}, reason.Invalid("operation ID must be 32 lowercase hex characters")
	}
	ttl := req.TTL
	if ttl == 0 {
		ttl = s.defaults.TTL
	}
	if err := validateTTL(ttl); err != nil {
		return Grant{}, err
	}
	if strings.TrimSpace(req.ToAgent) == "" || strings.TrimSpace(req.ToSession) == "" {
		return Grant{}, reason.Invalid("successor identity is required")
	}
	if err := validateIdentity("successor agent ID", req.ToAgent); err != nil {
		return Grant{}, err
	}
	if err := validateIdentity("successor session ID", req.ToSession); err != nil {
		return Grant{}, err
	}
	if req.ToWorkKey != "" {
		if err := validatePublicText("successor work key", req.ToWorkKey); err != nil {
			return Grant{}, err
		}
	}
	now := s.clock.Now()
	if err := validateRequestWindow(req.RequestNotAfter, now); err != nil {
		return Grant{}, err
	}
	deadline := req.RequestNotAfter
	intent := map[string]any{"kind": "transfer", "authorityId": s.st.AuthorityID(), "claimId": creds.ClaimID, "successorClaimId": req.SuccessorClaimID, "toAgent": req.ToAgent, "toSession": req.ToSession, "toWorkKey": req.ToWorkKey, "ttl": ttl.Microseconds(), "requestNotAfter": deadline.UTC().UnixMicro()}
	if creds.Actor != nil {
		intent["expectedRestoreId"], intent["installationId"] = creds.Actor.ExpectedRestoreID, creds.Actor.InstallationID
	}
	var hash string
	if creds.Actor == nil {
		hash = requestHash(intent)
	}
	var grant Grant
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		effective, err := s.effectiveRemoteNow(tx, creds.Actor, "write", now)
		if err != nil {
			return err
		}
		if creds.Actor != nil {
			intent["installationId"] = creds.Actor.InstallationID
			intent["protocolVersion"] = "worklease-http/1"
			hash = requestHash(intent)
		}
		var existing operationRow
		if found, e := readOperation(tx, creds.ClaimID, req.OperationID, &existing); e != nil {
			return e
		} else if found {
			var successorHash string
			if e := tx.QueryRowContext(ctx, `SELECT token_hash FROM epochs WHERE claim_id=?`, req.SuccessorClaimID).Scan(&successorHash); e != nil {
				return storage(e)
			}
			if subtle.ConstantTimeCompare([]byte(successorHash), []byte(hashToken(req.SuccessorToken))) != 1 {
				return reason.New(reason.ReasonOperationRequestMismatch, "successor credential differs from the recorded transfer")
			}
		}
		receipt, err := s.mutateCurrent(tx, creds, req.OperationID, "transfer", hash, "", deadline, effective, func(row claimRow, rev int64) (map[string]any, error) {
			pending, _ := hasStarted(tx, row.ClaimID)
			if pending {
				return nil, reason.New(reason.ReasonOperationInProgress, "a guarded operation is in progress")
			}
			if req.SuccessorClaimID == row.ClaimID {
				return nil, reason.Invalid("successor claim must differ from predecessor")
			}
			expires, err := extensionExpiry(row, effective.UnixMicro(), ttl, time.Time{})
			if err != nil {
				return nil, err
			}
			successorHash := hashToken(req.SuccessorToken)
			if _, err := tx.ExecContext(ctx, `INSERT INTO claims(claim_id,token_hash,revision,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,ttl_us,heartbeat_at,expires_at,checkpoint,admitted_ttl_us,admitted_hold_until,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, req.SuccessorClaimID, successorHash, 1, req.ToAgent, req.ToSession, req.ToWorkKey, row.Guarantee, row.LocalReplaceAllowed, effective.UnixMicro(), ttl.Microseconds(), effective.UnixMicro(), expires, nullString(row.Checkpoint), nullInt(row.AdmittedTTL), nullInt(row.AdmittedHoldUntil), nullString(row.InstallationID), nullString(row.RestoreID), boolInt(row.Remote)); err != nil {
				return nil, storage(err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE claim_resources SET claim_id=? WHERE claim_id=?`, req.SuccessorClaimID, row.ClaimID); err != nil {
				return nil, storage(err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO epochs(claim_id,token_hash,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,acquired_seq,checkpoint,admitted_ttl_us,admitted_hold_until,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, req.SuccessorClaimID, successorHash, req.ToAgent, req.ToSession, req.ToWorkKey, row.Guarantee, row.LocalReplaceAllowed, effective.UnixMicro(), 0, nullString(row.Checkpoint), nullInt(row.AdmittedTTL), nullInt(row.AdmittedHoldUntil), nullString(row.InstallationID), nullString(row.RestoreID), boolInt(row.Remote)); err != nil {
				return nil, storage(err)
			}
			for i, r := range row.Resources {
				if _, err := tx.ExecContext(ctx, `INSERT INTO epoch_resources(claim_id,resource,position) VALUES(?,?,?)`, req.SuccessorClaimID, r, i); err != nil {
					return nil, storage(err)
				}
			}
			event := store.Event{At: effective, Kind: "transferred", ClaimID: row.ClaimID, Resources: row.Resources, OperationID: req.OperationID, Revision: &rev, AgentID: row.AgentID, Detail: map[string]any{"reason": "transferred"}}
			if row.Remote {
				event.InstallationID, event.RestoreID, event.Remote = row.InstallationID, row.RestoreID, true
			}
			seq, err := tx.AppendEvent(event)
			if err != nil {
				return nil, storage(err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE epochs SET ended_at=?,ended_seq=?,ended_recorded_at=?,end_reason='transferred',final_revision=?,successor_claim_id=?,checkpoint=? WHERE claim_id=?`, effective.UnixMicro(), seq, effective.UnixMicro(), rev, req.SuccessorClaimID, nullString(row.Checkpoint), row.ClaimID); err != nil {
				return nil, storage(err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE epochs SET acquired_seq=? WHERE claim_id=?`, seq, req.SuccessorClaimID); err != nil {
				return nil, storage(err)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM claims WHERE claim_id=?`, row.ClaimID); err != nil {
				return nil, storage(err)
			}
			return map[string]any{"successorClaimId": req.SuccessorClaimID, "revision": int64(1), "expiresAt": formatMicros(expires), "resources": row.Resources, "_eventSeq": seq}, nil
		})
		if err == nil {
			var successor claimRow
			if current, readErr := readClaim(tx, req.SuccessorClaimID, &successor); readErr != nil {
				return readErr
			} else if current {
				grant = Grant{ClaimID: successor.ClaimID, Resources: successor.Resources, AgentID: successor.AgentID, SessionID: successor.SessionID, WorkKey: successor.WorkKey, Revision: successor.Revision, AcquiredAt: time.UnixMicro(successor.AcquiredAt).UTC(), ExpiresAt: time.UnixMicro(successor.ExpiresAt).UTC(), Guarantee: successor.Guarantee, AuthorityID: s.st.AuthorityID(), LocalReplaceAllowed: successor.LocalReplaceAllowed, Active: effective.UnixMicro() < successor.ExpiresAt, InstallationID: successor.InstallationID, RestoreID: successor.RestoreID, Receipt: receipt}
			} else {
				grant, err = grantFromClaimOrEpoch(tx, req.SuccessorClaimID, "", true, s.st.AuthorityID(), effective.UnixMicro())
				grant.Receipt = receipt
				grant.Active = false
			}
		}
		return err
	})
	return grant, err
}

func (s *Service) Verify(ctx context.Context, creds Credentials, expected []string) (Verification, error) {
	if s == nil || s.st == nil || s.st.Empty() || s.st.AuthorityID() == "" {
		return Verification{}, reason.New(reason.ReasonStorageFailure, "authority is not available")
	}
	var out Verification
	err := s.st.Read(ctx, func(tx *store.Tx) error {
		if e := s.authorizeRemote(tx, creds.Actor, "read"); e != nil {
			return e
		}
		var row claimRow
		ok, e := readClaim(tx, creds.ClaimID, &row)
		if e != nil {
			return e
		}
		if !ok {
			return reason.New(reason.ReasonVerifyFailed, "claim is not current").With("cause", reason.ReasonStaleClaim)
		}
		effective, e := s.effectiveNow(tx, s.clock.Now())
		if e != nil {
			return e
		}
		if e := s.authorize(row, creds, effective.UnixMicro(), false); e != nil {
			return reason.New(reason.ReasonVerifyFailed, "ownership verification failed").With("cause", reasonCode(e))
		}
		unknown, e := unresolvedForResources(tx, row.Resources)
		if e != nil {
			return e
		}
		if len(unknown) > 0 {
			out.UnknownOperations = unknown
			return reason.New(reason.ReasonUnknownOutcomePending, "a predecessor operation has unknown outcome")
		}
		if len(expected) > 0 && !containsResources(row.Resources, expected) {
			return reason.New(reason.ReasonVerifyFailed, "claim resources do not match").With("cause", "resource-mismatch")
		}
		v, e := row.view(s.st.AuthorityID(), effective.UnixMicro())
		if e != nil {
			return e
		}
		out.Claim = v
		return nil
	})
	return out, err
}

// ReplayOperation resolves a previously recorded guarded operation without
// consulting mutable filesystem state. It is used by idempotent guarded
// clients before validating paths that may have changed since the commit.
func (s *Service) ReplayOperation(ctx context.Context, creds Credentials, id, kind string, requestHashes ...string) (Receipt, bool, error) {
	requestHash := ""
	if len(requestHashes) > 0 {
		requestHash = requestHashes[0]
	}
	if err := validateOperationID(id); err != nil {
		return Receipt{}, false, err
	}
	if kind != "exec" && kind != "replace-file" {
		return Receipt{}, false, reason.Invalid("unsupported operation kind")
	}
	if s == nil || s.st == nil || s.st.Empty() || s.st.AuthorityID() == "" {
		return Receipt{}, false, reason.New(reason.ReasonStorageFailure, "authority is not available")
	}
	now := s.clock.Now()
	var out Receipt
	found := false
	err := s.st.Read(ctx, func(tx *store.Tx) error {
		if err := s.authorizeRemote(tx, creds.Actor, "write"); err != nil {
			return err
		}
		var op operationRow
		ok, err := readOperation(tx, creds.ClaimID, id, &op)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		found = true
		if op.Kind != kind {
			return reason.New(reason.ReasonOperationRequestMismatch, "operation kind differs from the recorded operation")
		}
		if subtle.ConstantTimeCompare([]byte(op.TokenHash), []byte(hashToken(creds.Token))) != 1 {
			return reason.New(reason.ReasonInvalidToken, "credential is invalid")
		}
		if op.Remote && (creds.Actor == nil || creds.Actor.InstallationID != op.InstallationID) {
			return reason.New(reason.ReasonAuthorizationDenied, "operation belongs to another installation")
		}
		effective, err := s.effectiveNow(tx, now)
		if err != nil {
			return err
		}
		if requestHash == "" {
			return reason.New(reason.ReasonOperationRequestMismatch, "exact operation request hash is required for replay")
		}
		if err := replayOperation(op, requestHash, effective); err != nil {
			return err
		}
		_, err = receiptFromOperation(op, true, &out)
		return err
	})
	return out, found, err
}

// OperationRequestHash returns the canonical digest persisted for a guarded
// operation intent. Clients use it when durably recording a pending lifecycle request.
func OperationRequestHash(kind, authority, claim string, request map[string]any, ttl time.Duration, deadline time.Time) (string, error) {
	return checkedRequestHash(map[string]any{"kind": kind, "authorityId": authority, "claimId": claim, "request": request, "ttl": ttl.Microseconds(), "requestNotAfter": deadline.UTC().UnixMicro()})
}

// RunGuardedOperation first commits the started intent, then executes its local
// effect and records completion in a second serialized authority transaction.
// This makes intent observable before effects while keeping authorization,
// replacement, and completion in one completing transaction.
//
// onStarted, when non-nil, is invoked once the started intent has committed
// and before the effect runs, so adapters can distinguish a pre-dispatch
// failure from one that leaves a started operation behind.
func (s *Service) RunGuardedOperation(ctx context.Context, creds Credentials, op OperationIntent, onStarted func(), effect func(ClaimView) (map[string]any, error)) (Receipt, error) {
	if effect == nil {
		return Receipt{}, reason.Invalid("guarded operation effect is required")
	}
	started, err := s.BeginOperation(ctx, creds, op)
	if err != nil {
		return Receipt{}, err
	}
	if started.Completed && started.Receipt != nil {
		return *started.Receipt, nil
	}
	if onStarted != nil {
		onStarted()
	}
	creds.Revision = started.Revision
	now := s.clock.Now()
	var out Receipt
	var operationErr error
	err = s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		effective, err := s.effectiveRemoteNow(tx, creds.Actor, "write", now)
		if err != nil {
			return err
		}
		var row claimRow
		ok, err := readClaim(tx, creds.ClaimID, &row)
		if err != nil {
			return err
		}
		if !ok {
			return reason.New(reason.ReasonStaleClaim, "claim is not current")
		}
		if err = s.authorize(row, creds, effective.UnixMicro(), true); err != nil {
			return err
		}
		if unknown, err := unresolvedForResourcesExcept(tx, row.Resources, row.ClaimID); err != nil {
			return err
		} else if len(unknown) > 0 {
			return reason.New(reason.ReasonUnknownOutcomePending, "a predecessor operation has unknown outcome").With("operations", unknown)
		}
		var recorded operationRow
		found, err := readOperation(tx, row.ClaimID, op.OperationID, &recorded)
		if err != nil {
			return err
		}
		if !found || recorded.State != "started" || recorded.RequestHash != started.RequestHash {
			return reason.New(reason.ReasonOperationNotFound, "started operation was not found")
		}
		view, err := row.view(s.st.AuthorityID(), effective.UnixMicro())
		if err != nil {
			return err
		}
		result, effectErr := effect(view)
		if effectErr != nil {
			if isUncertainGuardError(effectErr) {
				operationErr = effectErr
				return nil
			}
			if result == nil {
				result = map[string]any{}
			}
			result["ok"], result["error"] = false, effectErr.Error()
			completed, completeErr := s.completeGuardedInTx(ctx, tx, row, op, started.RequestHash, op.RequestNotAfter, effective, row.Revision, result)
			if completeErr != nil {
				return completeErr
			}
			out = completed
			operationErr = effectErr
			return nil
		}
		out, err = s.completeGuardedInTx(ctx, tx, row, op, started.RequestHash, op.RequestNotAfter, effective, row.Revision, result)
		return err
	})
	if err != nil {
		return Receipt{}, err
	}
	if operationErr != nil {
		return out, operationErr
	}
	return out, nil
}

func isUncertainGuardError(err error) bool {
	e := reason.As(err)
	if e == nil {
		return true
	}
	switch e.Reason {
	case reason.ReasonUnknownOutcome, reason.ReasonOwnershipLost, reason.ReasonStorageFailure, reason.ReasonChildTimeout, reason.ReasonInterrupted:
		return true
	default:
		return false
	}
}

func (s *Service) completeGuardedInTx(ctx context.Context, tx *store.Tx, row claimRow, op OperationIntent, hash string, deadline, effective time.Time, startedRevision int64, result map[string]any) (Receipt, error) {
	if result == nil {
		result = map[string]any{}
	}
	rev := startedRevision + 1
	kind := op.Kind + "-completed"
	if op.Kind == "replace-file" {
		kind = "replace-completed"
	}
	event := store.Event{At: effective, Kind: kind, ClaimID: row.ClaimID, Resources: row.Resources, OperationID: op.OperationID, Revision: &rev, AgentID: row.AgentID, Detail: publicReceiptDetail(result)}
	if row.Remote {
		event.InstallationID, event.RestoreID, event.Remote = row.InstallationID, row.RestoreID, true
	}
	seq, err := tx.AppendEvent(event)
	if err != nil {
		return Receipt{}, storage(err)
	}
	result["revision"] = rev
	encoded, _ := json.Marshal(result)
	if _, err := tx.ExecContext(ctx, `UPDATE claims SET revision=? WHERE claim_id=?`, rev, row.ClaimID); err != nil {
		return Receipt{}, storage(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE operations SET state='completed',receipt=?,completed_at=?,completed_seq=? WHERE claim_id=? AND operation_id=?`, string(encoded), effective.UnixMicro(), seq, row.ClaimID, op.OperationID); err != nil {
		return Receipt{}, storage(err)
	}
	return Receipt{OperationID: op.OperationID, ClaimID: row.ClaimID, Kind: op.Kind, RequestHash: hash, Revision: rev, Committed: true, Result: result}, nil
}

func (s *Service) BeginOperation(ctx context.Context, creds Credentials, op OperationIntent) (Started, error) {
	if err := validateOperationID(op.OperationID); err != nil {
		return Started{}, err
	}
	if op.Kind != "exec" && op.Kind != "replace-file" {
		return Started{}, reason.Invalid("unsupported operation kind")
	}
	now := s.clock.Now()
	deadline := op.RequestNotAfter
	if err := validateRequestWindow(deadline, now); err != nil {
		return Started{}, err
	}
	ttl := op.TTL
	if ttl == 0 {
		ttl = s.defaults.TTL
	}
	if err := validateTTL(ttl); err != nil {
		return Started{}, err
	}
	intent := map[string]any{"kind": op.Kind, "authorityId": s.st.AuthorityID(), "claimId": creds.ClaimID, "request": op.Request, "ttl": ttl.Microseconds(), "requestNotAfter": deadline.UTC().UnixMicro()}
	if creds.Actor != nil {
		intent["protocolVersion"], intent["expectedRestoreId"] = "worklease-http/1", creds.Actor.ExpectedRestoreID
	}
	var hash string
	if creds.Actor == nil {
		var err error
		hash, err = checkedRequestHash(intent)
		if err != nil {
			return Started{}, err
		}
		if op.RequestHash != "" && op.RequestHash != hash {
			return Started{}, reason.New(reason.ReasonOperationRequestMismatch, "supplied request hash does not match operation intent")
		}
	}
	var started Started
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		effective, e := s.effectiveRemoteNow(tx, creds.Actor, "write", now)
		if e != nil {
			return e
		}
		if creds.Actor != nil {
			intent["installationId"] = creds.Actor.InstallationID
			var hashErr error
			hash, hashErr = checkedRequestHash(intent)
			if hashErr != nil {
				return hashErr
			}
			if op.RequestHash != "" && op.RequestHash != hash {
				return reason.New(reason.ReasonOperationRequestMismatch, "supplied request hash does not match operation intent")
			}
		}
		// Replay belongs to the original authenticated epoch and must be
		// resolved before current-claim or revision authorization.
		var existing operationRow
		found, e := readOperation(tx, creds.ClaimID, op.OperationID, &existing)
		if e != nil {
			return e
		}
		if found {
			if subtle.ConstantTimeCompare([]byte(existing.TokenHash), []byte(hashToken(creds.Token))) != 1 {
				return reason.New(reason.ReasonInvalidToken, "credential is invalid")
			}
			if existing.Remote && (creds.Actor == nil || creds.Actor.InstallationID != existing.InstallationID) {
				return reason.New(reason.ReasonAuthorizationDenied, "operation belongs to another installation")
			}
			if e := replayOperation(existing, hash, effective); e != nil {
				return e
			}
			receipt, e := receiptFromOperation(existing, true, nil)
			if e != nil {
				return e
			}
			started = Started{OperationID: existing.OperationID, ClaimID: existing.ClaimID, Kind: existing.Kind, Revision: receipt.Revision, RequestHash: existing.RequestHash, Completed: true, Receipt: &receipt}
			return nil
		}
		var row claimRow
		ok, e := readClaim(tx, creds.ClaimID, &row)
		if e != nil {
			return e
		}
		if !ok {
			return reason.New(reason.ReasonStaleClaim, "claim is not current")
		}
		if e = s.authorize(row, creds, effective.UnixMicro(), true); e != nil {
			return e
		}
		if unknown, e := unresolvedForResourcesExcept(tx, row.Resources, row.ClaimID); e != nil {
			return e
		} else if len(unknown) > 0 {
			return reason.New(reason.ReasonUnknownOutcomePending, "a predecessor operation has unknown outcome").With("operations", unknown)
		}
		if pending, _ := hasStarted(tx, row.ClaimID); pending {
			return reason.New(reason.ReasonOperationInProgress, "a guarded operation is in progress")
		}
		if creds.Actor != nil {
			mode, _, e := recoveryMode(tx)
			if e != nil {
				return e
			}
			if mode {
				return reason.New(reason.ReasonRecoveryClosed, "new operation starts are closed during recovery")
			}
		}
		expires, e := extensionExpiry(row, effective.UnixMicro(), ttl, time.Time{})
		if e != nil {
			return e
		}
		rev := row.Revision + 1
		event := store.Event{At: effective, Kind: guardEventKind(op.Kind, "started"), ClaimID: row.ClaimID, Resources: row.Resources, OperationID: op.OperationID, Revision: &rev, AgentID: row.AgentID}
		if row.Remote {
			event.InstallationID, event.RestoreID, event.Remote = row.InstallationID, row.RestoreID, true
		}
		seq, e := tx.AppendEvent(event)
		if e != nil {
			return storage(e)
		}
		if _, e := tx.ExecContext(ctx, `UPDATE claims SET revision=?,ttl_us=?,heartbeat_at=?,expires_at=? WHERE claim_id=?`, rev, ttl.Microseconds(), effective.UnixMicro(), expires, row.ClaimID); e != nil {
			return storage(e)
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO operations(claim_id,operation_id,kind,request_hash,request_not_after,expected_revision,state,started_at,started_seq,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, row.ClaimID, op.OperationID, op.Kind, hash, deadline.UnixMicro(), row.Revision, "started", effective.UnixMicro(), seq, nullString(row.InstallationID), nullString(row.RestoreID), boolInt(row.Remote)); e != nil {
			return storage(e)
		}
		started = Started{OperationID: op.OperationID, ClaimID: row.ClaimID, Kind: op.Kind, Revision: rev, RequestHash: hash}
		return nil
	})
	return started, err
}

// RenewOperation is the guard-owned renewal path. It advances the same
// claim revision without creating a second lifecycle operation row.
func (s *Service) RenewOperation(ctx context.Context, creds Credentials, id string, ttl time.Duration) (Receipt, error) {
	if err := validateOperationID(id); err != nil {
		return Receipt{}, err
	}
	if ttl == 0 {
		ttl = s.defaults.TTL
	}
	if err := validateTTL(ttl); err != nil {
		return Receipt{}, err
	}
	now := s.clock.Now()
	var out Receipt
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		effective, err := s.effectiveRemoteNow(tx, creds.Actor, "write", now)
		if err != nil {
			return err
		}
		var row claimRow
		ok, err := readClaim(tx, creds.ClaimID, &row)
		if err != nil {
			return err
		}
		if !ok {
			return reason.New(reason.ReasonStaleClaim, "claim is not current")
		}
		if err := s.authorize(row, creds, effective.UnixMicro(), true); err != nil {
			return err
		}
		var op operationRow
		found, err := readOperation(tx, row.ClaimID, id, &op)
		if err != nil {
			return err
		}
		if !found || op.State != "started" {
			return reason.New(reason.ReasonOperationNotFound, "started operation was not found")
		}
		rev := row.Revision + 1
		expires, err := extensionExpiry(row, effective.UnixMicro(), ttl, time.Time{})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE claims SET revision=?,ttl_us=?,heartbeat_at=?,expires_at=? WHERE claim_id=?`, rev, ttl.Microseconds(), effective.UnixMicro(), expires, row.ClaimID); err != nil {
			return storage(err)
		}
		event := store.Event{At: effective, Kind: "renewed", ClaimID: row.ClaimID, Resources: row.Resources, OperationID: id, Revision: &rev, AgentID: row.AgentID}
		if row.Remote {
			event.InstallationID, event.RestoreID, event.Remote = row.InstallationID, row.RestoreID, true
		}
		seq, err := tx.AppendEvent(event)
		if err != nil {
			return storage(err)
		}
		_ = seq
		out = Receipt{OperationID: id, ClaimID: row.ClaimID, Kind: op.Kind, RequestHash: op.RequestHash, Revision: rev, Committed: true, Result: map[string]any{"revision": rev, "expiresAt": formatMicros(expires)}}
		return nil
	})
	return out, err
}

func (s *Service) CompleteOperation(ctx context.Context, creds Credentials, id string, receipt map[string]any) (Receipt, error) {
	if err := validateOperationID(id); err != nil {
		return Receipt{}, err
	}
	now := s.clock.Now()
	var out Receipt
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		effective, e := s.effectiveRemoteNow(tx, creds.Actor, "write", now)
		if e != nil {
			return e
		}
		var op operationRow
		found, e := readOperation(tx, creds.ClaimID, id, &op)
		if e != nil {
			return e
		}
		if !found {
			return reason.New(reason.ReasonOperationNotFound, "operation was not found")
		}
		if subtle.ConstantTimeCompare([]byte(op.TokenHash), []byte(hashToken(creds.Token))) != 1 {
			return reason.New(reason.ReasonInvalidToken, "credential is invalid")
		}
		if op.Remote && (creds.Actor == nil || creds.Actor.InstallationID != op.InstallationID) {
			return reason.New(reason.ReasonAuthorizationDenied, "operation belongs to another installation")
		}
		if op.State != "started" {
			if effective.UnixMicro() >= op.RequestNotAfter {
				return reason.New(reason.ReasonReplayExpired, "request replay deadline has passed")
			}
			_, err := receiptFromOperation(op, true, &out)
			return err
		}
		var row claimRow
		ok, e := readClaim(tx, creds.ClaimID, &row)
		if e != nil {
			return e
		}
		if !ok {
			return reason.New(reason.ReasonUnknownOutcome, "operation outcome is unresolved")
		}
		if e = s.authorize(row, creds, effective.UnixMicro(), true); e != nil {
			return e
		}
		rev := row.Revision + 1
		kind := guardEventKind(op.Kind, "completed")
		event := store.Event{At: effective, Kind: kind, ClaimID: row.ClaimID, Resources: row.Resources, OperationID: id, Revision: &rev, AgentID: row.AgentID, Detail: publicReceiptDetail(receipt)}
		if row.Remote {
			event.InstallationID, event.RestoreID, event.Remote = row.InstallationID, row.RestoreID, true
		}
		seq, e := tx.AppendEvent(event)
		if e != nil {
			return storage(e)
		}
		storedReceipt := make(map[string]any, len(receipt)+1)
		for key, value := range receipt {
			storedReceipt[key] = value
		}
		storedReceipt["revision"] = rev
		encoded, _ := json.Marshal(storedReceipt)
		if _, e := tx.ExecContext(ctx, `UPDATE claims SET revision=? WHERE claim_id=?`, rev, row.ClaimID); e != nil {
			return storage(e)
		}
		if _, e := tx.ExecContext(ctx, `UPDATE operations SET state='completed',receipt=?,completed_at=?,completed_seq=? WHERE claim_id=? AND operation_id=?`, string(encoded), effective.UnixMicro(), seq, row.ClaimID, id); e != nil {
			return storage(e)
		}
		out = Receipt{OperationID: id, ClaimID: row.ClaimID, Kind: op.Kind, RequestHash: op.RequestHash, Revision: rev, Committed: true, Result: storedReceipt}
		return nil
	})
	return out, err
}

func guardEventKind(kind, state string) string {
	if kind == "replace-file" {
		kind = "replace"
	}
	return kind + "-" + state
}
