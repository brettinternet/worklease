// Package ledger exposes redacted operation, event, and epoch history views.
// Private operation payloads are available only after authenticating the
// credential retained for the target epoch.
package ledger

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

const (
	DefaultLimit = 50
	MaxLimit     = 1000
)

type TransactionCheck func(context.Context, *store.Tx) error

type Service struct {
	st    *store.Store
	check TransactionCheck
}

func New(st *store.Store) *Service { return &Service{st: st} }

// NewChecked applies check inside every ledger read transaction.
func NewChecked(st *store.Store, check TransactionCheck) *Service {
	return &Service{st: st, check: check}
}

func (s *Service) checked(ctx context.Context, tx *store.Tx) error {
	if s.check == nil {
		return nil
	}
	return s.check(ctx, tx)
}

type Cursor struct {
	Version     int    `json:"version"`
	AuthorityID string `json:"authorityId"`
	RestoreID   string `json:"restoreId,omitempty"`
	Feed        string `json:"feed"`
	Filter      string `json:"filter"`
	Sequence    string `json:"sequence"`
}

func ParseCursor(value string) (Cursor, error) {
	if value == "" {
		return Cursor{}, nil
	}
	if strings.Contains(value, "=") {
		return Cursor{}, cursorInvalid()
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(b) == 0 || len(b) > 4096 || rejectDuplicateJSON(b) != nil {
		return Cursor{}, cursorInvalid()
	}
	var c Cursor
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Cursor{}, cursorInvalid()
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Cursor{}, cursorInvalid()
	}
	if c.Version != 1 || !validID(c.AuthorityID) || !validID(c.RestoreID) || (c.Feed != "events" && c.Feed != "history") || c.Sequence == "" {
		return Cursor{}, cursorInvalid()
	}
	if _, err := parseSequence(c.Sequence); err != nil {
		return Cursor{}, cursorInvalid()
	}
	return c, nil
}

func ValidateCursor(value, feed, filter string) error {
	if value == "" {
		return nil
	}
	c, err := ParseCursor(value)
	if err != nil {
		return err
	}
	if c.Feed != feed || c.Filter != filter {
		return cursorInvalid()
	}
	return nil
}

func cursorInvalid() error { return reason.New(reason.ReasonCursorInvalid, "cursor is invalid") }
func rejectDuplicateJSON(data []byte) error {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	var visit func() error
	visit = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok || seen[key] {
					return errors.New("duplicate key")
				}
				seen[key] = true
				if err := visit(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case '[':
			for dec.More() {
				if err := visit(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		default:
			return errors.New("invalid delimiter")
		}
	}
	if err := visit(); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}
func parseSequence(v string) (int64, error) {
	if v == "" || (len(v) > 1 && v[0] == '0') {
		return 0, errors.New("invalid sequence")
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, errors.New("invalid sequence")
	}
	return n, nil
}
func encodeCursor(authority, restore, feed, filter string, seq int64) string {
	b, _ := json.Marshal(Cursor{Version: 1, AuthorityID: authority, RestoreID: restore, Feed: feed, Filter: filter, Sequence: strconv.FormatInt(seq, 10)})
	return base64.RawURLEncoding.EncodeToString(b)
}

// EncodeCursor creates the opaque cursor used by every ledger feed. Callers
// should preserve the filter exactly when continuing a filtered feed.
func EncodeCursor(authority, restore, feed, filter string, seq int64) string {
	return encodeCursor(authority, restore, feed, filter, seq)
}

// ResourcesFilter is the canonical cursor filter for a resource watch. JSON is
// used rather than a delimiter so opaque resource bytes remain unambiguous.
func ResourcesFilter(resources []string) string {
	if len(resources) == 0 {
		return ""
	}
	b, _ := json.Marshal(resources)
	return string(b)
}
func bindCursor(value, authority, restore, feed, filter string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	c, err := ParseCursor(value)
	if err != nil || c.AuthorityID != authority || c.Feed != feed || c.Filter != filter {
		return 0, cursorInvalid()
	}
	if c.RestoreID != restore {
		return 0, reason.New(reason.ReasonAuthorityRestored, "authority was restored").With("restoreId", restore)
	}
	return parseSequence(c.Sequence)
}
func limitValue(v int) (int, error) {
	if v == 0 {
		return DefaultLimit, nil
	}
	if v < 1 || v > MaxLimit {
		return 0, reason.Invalid("limit must be between 1 and 1000")
	}
	return v, nil
}

type Event struct {
	Sequence    string         `json:"sequence"`
	At          time.Time      `json:"at"`
	Kind        string         `json:"kind"`
	ClaimID     string         `json:"claimId,omitempty"`
	Resources   []string       `json:"resources"`
	OperationID string         `json:"operationId,omitempty"`
	Revision    *int64         `json:"revision,omitempty"`
	AgentID     string         `json:"agentId,omitempty"`
	Detail      map[string]any `json:"detail"`
}
type EventsPage struct {
	AuthorityID string  `json:"authorityId"`
	Events      []Event `json:"events"`
	NextCursor  string  `json:"nextCursor"`
	Gap         bool    `json:"gap"`
}

func (s *Service) Events(ctx context.Context, cursor string, limit int) (EventsPage, error) {
	limit, err := limitValue(limit)
	if err != nil {
		return EventsPage{}, err
	}
	position, err := bindCursor(cursor, s.st.AuthorityID(), s.st.RestoreID(), "events", "")
	if err != nil {
		return EventsPage{}, err
	}
	page := EventsPage{AuthorityID: s.st.AuthorityID(), Events: []Event{}}
	err = s.st.Read(ctx, func(tx *store.Tx) error {
		if err := s.checked(ctx, tx); err != nil {
			return err
		}
		last, pruned, err := watermarks(ctx, tx)
		if err != nil {
			return err
		}
		if cursor != "" && position < pruned {
			page.Gap, page.NextCursor = true, encodeCursor(s.st.AuthorityID(), s.st.RestoreID(), "events", "", pruned)
			return nil
		}
		query, args := `SELECT seq,at,kind,coalesce(claim_id,''),resources,coalesce(operation_id,''),revision,coalesce(agent_id,''),detail FROM events`, []any{}
		if cursor != "" {
			query += ` WHERE seq>? ORDER BY seq ASC LIMIT ?`
			args = append(args, position, limit)
		} else {
			query += ` ORDER BY seq DESC LIMIT ?`
			args = append(args, limit)
		}
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return storage(err)
		}
		defer rows.Close()
		for rows.Next() {
			var seq, at int64
			var resources, detail string
			var revision *int64
			var ev Event
			if err := rows.Scan(&seq, &at, &ev.Kind, &ev.ClaimID, &resources, &ev.OperationID, &revision, &ev.AgentID, &detail); err != nil {
				return storage(err)
			}
			ev.Sequence, ev.At, ev.Revision = strconv.FormatInt(seq, 10), time.UnixMicro(at).UTC(), revision
			if json.Unmarshal([]byte(resources), &ev.Resources) != nil || json.Unmarshal([]byte(detail), &ev.Detail) != nil {
				return storage(errors.New("invalid public event JSON"))
			}
			page.Events = append(page.Events, ev)
		}
		if err := rows.Err(); err != nil {
			return storage(err)
		}
		if cursor == "" {
			reverseEvents(page.Events)
		}
		next := position
		if len(page.Events) > 0 {
			next, _ = parseSequence(page.Events[len(page.Events)-1].Sequence)
		} else if cursor == "" {
			next = last
		}
		page.NextCursor = encodeCursor(s.st.AuthorityID(), s.st.RestoreID(), "events", "", next)
		return nil
	})
	return page, err
}

// EventsScan is a bounded observation of the events feed. InspectedSequence
// advances only over rows actually read, allowing a timeout to be resumed
// without hiding an event that arrived after the observation began.
type EventsScan struct {
	Events            []Event
	NextCursor        string
	Gap               bool
	InspectedSequence string
}

// ScanEvents scans the durable event rows after cursor in sequence order. It
// applies the resource intersection filter while still inspecting every row,
// rather than using a page limit that could skip a later match.
func (s *Service) ScanEvents(ctx context.Context, cursor, filter string) (EventsScan, error) {
	position, err := bindCursor(cursor, s.st.AuthorityID(), s.st.RestoreID(), "events", filter)
	if err != nil {
		return EventsScan{}, err
	}
	resources := []string{}
	if filter != "" {
		if err := json.Unmarshal([]byte(filter), &resources); err != nil || len(resources) == 0 {
			return EventsScan{}, cursorInvalid()
		}
	}
	result := EventsScan{Events: []Event{}, InspectedSequence: strconv.FormatInt(position, 10)}
	err = s.st.Read(ctx, func(tx *store.Tx) error {
		if err := s.checked(ctx, tx); err != nil {
			return err
		}
		last, pruned, err := watermarks(ctx, tx)
		if err != nil {
			return err
		}
		if cursor != "" && position < pruned {
			result.Gap = true
			result.NextCursor = encodeCursor(s.st.AuthorityID(), s.st.RestoreID(), "events", filter, pruned)
			result.InspectedSequence = strconv.FormatInt(pruned, 10)
			return nil
		}
		rows, err := tx.QueryContext(ctx, `SELECT seq,at,kind,coalesce(claim_id,''),resources,coalesce(operation_id,''),revision,coalesce(agent_id,''),detail FROM events WHERE seq>? AND seq<=? ORDER BY seq ASC`, position, last)
		if err != nil {
			return storage(err)
		}
		defer rows.Close()
		matched := false
		for rows.Next() {
			var seq, at int64
			var encodedResources, detail string
			var revision *int64
			var event Event
			if err := rows.Scan(&seq, &at, &event.Kind, &event.ClaimID, &encodedResources, &event.OperationID, &revision, &event.AgentID, &detail); err != nil {
				return storage(err)
			}
			if json.Unmarshal([]byte(encodedResources), &event.Resources) != nil || json.Unmarshal([]byte(detail), &event.Detail) != nil {
				return storage(errors.New("invalid public event JSON"))
			}
			event.Sequence, event.At, event.Revision = strconv.FormatInt(seq, 10), time.UnixMicro(at).UTC(), revision
			result.InspectedSequence = event.Sequence
			if len(resources) == 0 || eventIntersects(event.Resources, resources) {
				result.Events = append(result.Events, event)
				matched = true
				break
			}
		}
		if err := rows.Err(); err != nil {
			return storage(err)
		}
		inspected, _ := parseSequence(result.InspectedSequence)
		if matched {
			result.NextCursor = encodeCursor(s.st.AuthorityID(), s.st.RestoreID(), "events", filter, inspected)
		} else {
			result.NextCursor = encodeCursor(s.st.AuthorityID(), s.st.RestoreID(), "events", filter, last)
		}
		return nil
	})
	return result, err
}

func eventIntersects(eventResources, requested []string) bool {
	for _, left := range eventResources {
		for _, right := range requested {
			if left == right {
				return true
			}
		}
	}
	return false
}

func reverseEvents(v []Event) {
	for i, j := 0, len(v)-1; i < j; i, j = i+1, j-1 {
		v[i], v[j] = v[j], v[i]
	}
}
func watermarks(ctx context.Context, tx *store.Tx) (int64, int64, error) {
	var lastRaw, prunedRaw string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='last_event_seq'`).Scan(&lastRaw); err != nil {
		return 0, 0, storage(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='pruned_through_seq'`).Scan(&prunedRaw); err != nil {
		return 0, 0, storage(err)
	}
	last, e := parseSequence(lastRaw)
	if e != nil {
		return 0, 0, storage(e)
	}
	pruned, e := parseSequence(prunedRaw)
	return last, pruned, e
}

type Operation struct {
	ClaimID          string         `json:"claimId"`
	OperationID      string         `json:"operationId"`
	Kind             string         `json:"kind"`
	State            string         `json:"state"`
	RequestSHA256    string         `json:"requestSha256,omitempty"`
	ExpectedRevision int64          `json:"expectedRevision,omitempty"`
	RequestNotAfter  *time.Time     `json:"requestNotAfter,omitempty"`
	StartedAt        time.Time      `json:"startedAt"`
	CompletedAt      *time.Time     `json:"completedAt,omitempty"`
	Receipt          map[string]any `json:"receipt,omitempty"`
	Evidence         any            `json:"evidence,omitempty"`
	Outcome          string         `json:"outcome,omitempty"`
}
type InspectRequest struct {
	OperationID, ClaimID, Resource, Token string
	Full                                  bool
	TrustedFull                           bool
	// HandlePath or CredentialPath identifies a durable claim token source for remote private inspection.
	HandlePath, CredentialPath string
}

func (s *Service) Inspect(ctx context.Context, req InspectRequest) (Operation, error) {
	if !validID(req.OperationID) {
		return Operation{}, reason.Invalid("operation ID must be 32 lowercase hex characters")
	}
	var result Operation
	err := s.st.Read(ctx, func(tx *store.Tx) error {
		if err := s.checked(ctx, tx); err != nil {
			return err
		}
		claim := req.ClaimID
		if claim == "" {
			query := `SELECT DISTINCT o.claim_id FROM operations o`
			args := []any{}
			if req.Resource != "" {
				query += ` JOIN epoch_resources er ON er.claim_id=o.claim_id WHERE er.resource=? AND o.operation_id=?`
				args = append(args, req.Resource, req.OperationID)
			} else {
				query += ` WHERE o.operation_id=?`
				args = append(args, req.OperationID)
			}
			query += ` ORDER BY o.claim_id`
			rows, e := tx.QueryContext(ctx, query, args...)
			if e != nil {
				return storage(e)
			}
			defer rows.Close()
			var claims []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					return storage(err)
				}
				claims = append(claims, id)
			}
			if err := rows.Err(); err != nil {
				return storage(err)
			}
			if len(claims) == 0 {
				return reason.New(reason.ReasonOperationNotFound, "operation was not found")
			}
			if len(claims) > 1 {
				return reason.New(reason.ReasonOperationAmbiguous, "operation ID exists in multiple epochs").With("claimIds", claims)
			}
			claim = claims[0]
		}
		var requestHash, state, receipt, tokenHash, evidence, outcome string
		var deadline, expected, started, completed int64
		e := tx.QueryRowContext(ctx, `SELECT o.request_hash,o.state,coalesce(o.receipt,''),e.token_hash,o.kind,o.request_not_after,o.expected_revision,o.started_at,coalesce(o.completed_at,0),coalesce(r.evidence,''),coalesce(r.outcome,'') FROM operations o JOIN epochs e ON e.claim_id=o.claim_id LEFT JOIN reconciliations r ON r.claim_id=o.claim_id AND r.operation_id=o.operation_id WHERE o.claim_id=? AND o.operation_id=?`, claim, req.OperationID).Scan(&requestHash, &state, &receipt, &tokenHash, &result.Kind, &deadline, &expected, &started, &completed, &evidence, &outcome)
		if errors.Is(e, sql.ErrNoRows) {
			return reason.New(reason.ReasonOperationNotFound, "operation was not found")
		}
		if e != nil {
			return storage(e)
		}
		result.ClaimID, result.OperationID, result.State, result.StartedAt = claim, req.OperationID, state, time.UnixMicro(started).UTC()
		result.RequestSHA256 = requestHash
		if completed > 0 {
			t := time.UnixMicro(completed).UTC()
			result.CompletedAt = &t
		}
		if req.Full {
			if !req.TrustedFull && subtle.ConstantTimeCompare([]byte(tokenHash), []byte(hashToken(req.Token))) != 1 {
				return reason.New(reason.ReasonInvalidToken, "credential is invalid")
			}
			requestNotAfter := time.UnixMicro(deadline).UTC()
			result.ExpectedRevision, result.RequestNotAfter = expected, &requestNotAfter
			if receipt != "" && json.Unmarshal([]byte(receipt), &result.Receipt) != nil {
				return storage(errors.New("invalid operation receipt"))
			}
			if evidence != "" && json.Unmarshal([]byte(evidence), &result.Evidence) != nil {
				return storage(errors.New("invalid reconciliation evidence"))
			}
			result.Outcome = outcome
		}
		return nil
	})
	return result, err
}

type Epoch struct {
	ClaimID       string      `json:"claimId"`
	AgentID       string      `json:"agentId"`
	SessionID     string      `json:"sessionId"`
	WorkKey       string      `json:"workKey"`
	Resources     []string    `json:"resources"`
	AcquiredAt    time.Time   `json:"acquiredAt"`
	EndedAt       *time.Time  `json:"endedAt,omitempty"`
	EndReason     string      `json:"endReason,omitempty"`
	Status        string      `json:"status"`
	FinalRevision *int64      `json:"finalRevision,omitempty"`
	Operations    []Operation `json:"operations"`
}
type HistoryCoverage struct {
	EarliestRetainedSequence *string `json:"earliestRetainedSequence"`
	PrunedThroughSequence    string  `json:"prunedThroughSequence"`
}
type HistoryPage struct {
	AuthorityID string          `json:"authorityId"`
	Resource    string          `json:"resource"`
	Coverage    HistoryCoverage `json:"coverage"`
	Epochs      []Epoch         `json:"epochs"`
	NextCursor  string          `json:"nextCursor"`
	Gap         bool            `json:"gap"`
}

func (s *Service) History(ctx context.Context, resource, cursor string, limit int, full bool) (HistoryPage, error) {
	if strings.TrimSpace(resource) != resource || resource == "" {
		return HistoryPage{}, reason.Invalid("history requires one valid resource")
	}
	limit, e := limitValue(limit)
	if e != nil {
		return HistoryPage{}, e
	}
	pos, e := bindCursor(cursor, s.st.AuthorityID(), s.st.RestoreID(), "history", resource)
	if e != nil {
		return HistoryPage{}, e
	}
	page := HistoryPage{AuthorityID: s.st.AuthorityID(), Resource: resource, Epochs: []Epoch{}}
	e = s.st.Read(ctx, func(tx *store.Tx) error {
		if err := s.checked(ctx, tx); err != nil {
			return err
		}
		last, pruned, err := watermarks(ctx, tx)
		if err != nil {
			return err
		}
		page.Coverage.PrunedThroughSequence = strconv.FormatInt(pruned, 10)
		var earliest *int64
		if err := tx.QueryRowContext(ctx, `SELECT min(e.acquired_seq) FROM epochs e JOIN epoch_resources er ON er.claim_id=e.claim_id WHERE er.resource=?`, resource).Scan(&earliest); err != nil {
			return storage(err)
		}
		if earliest != nil {
			value := strconv.FormatInt(*earliest, 10)
			page.Coverage.EarliestRetainedSequence = &value
		}
		if cursor != "" && pos < pruned {
			page.Gap = true
			page.NextCursor = encodeCursor(s.st.AuthorityID(), s.st.RestoreID(), "history", resource, pruned)
			return nil
		}
		q := `SELECT e.claim_id,e.agent_id,e.session_id,e.work_key,e.acquired_at,coalesce(e.ended_at,0),coalesce(e.end_reason,''),e.final_revision,e.acquired_seq,coalesce(c.expires_at,0) FROM epochs e JOIN epoch_resources er ON er.claim_id=e.claim_id LEFT JOIN claims c ON c.claim_id=e.claim_id WHERE er.resource=?`
		args := []any{resource}
		if cursor != "" {
			q += ` AND e.acquired_seq>? ORDER BY e.acquired_seq ASC LIMIT ?`
			args = append(args, pos, limit)
		} else {
			q += ` ORDER BY e.acquired_seq DESC LIMIT ?`
			args = append(args, limit)
		}
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return storage(err)
		}
		defer rows.Close()
		seqs := []int64{}
		for rows.Next() {
			var ep Epoch
			var acquired, ended, seq, expires int64
			var final *int64
			if err := rows.Scan(&ep.ClaimID, &ep.AgentID, &ep.SessionID, &ep.WorkKey, &acquired, &ended, &ep.EndReason, &final, &seq, &expires); err != nil {
				return storage(err)
			}
			ep.AcquiredAt = time.UnixMicro(acquired).UTC()
			ep.FinalRevision = final
			if ended == 0 {
				ep.Status = "open"
				if expires > 0 && time.Now().UnixMicro() >= expires {
					ep.Status = "expired-open"
				}
			} else {
				t := time.UnixMicro(ended).UTC()
				ep.EndedAt = &t
				ep.Status = "complete"
			}
			ep.Resources, err = epochResources(ctx, tx, ep.ClaimID)
			if err != nil {
				return err
			}
			ep.Operations, err = epochOperations(ctx, tx, ep.ClaimID, full)
			if err != nil {
				return err
			}
			page.Epochs = append(page.Epochs, ep)
			seqs = append(seqs, seq)
		}
		if cursor == "" {
			for i, j := 0, len(page.Epochs)-1; i < j; i, j = i+1, j-1 {
				page.Epochs[i], page.Epochs[j] = page.Epochs[j], page.Epochs[i]
				seqs[i], seqs[j] = seqs[j], seqs[i]
			}
		}
		next := pos
		if len(seqs) > 0 {
			next = seqs[len(seqs)-1]
		} else if cursor == "" {
			next = last
		}
		page.NextCursor = encodeCursor(s.st.AuthorityID(), s.st.RestoreID(), "history", resource, next)
		return rows.Err()
	})
	return page, e
}
func epochResources(ctx context.Context, tx *store.Tx, claim string) ([]string, error) {
	rows, e := tx.QueryContext(ctx, `SELECT resource FROM epoch_resources WHERE claim_id=? ORDER BY position`, claim)
	if e != nil {
		return nil, storage(e)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if e := rows.Scan(&v); e != nil {
			return nil, storage(e)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func epochOperations(ctx context.Context, tx *store.Tx, claim string, full bool) ([]Operation, error) {
	rows, e := tx.QueryContext(ctx, `SELECT operation_id,kind,state,request_hash,started_at,coalesce(completed_at,0) FROM operations WHERE claim_id=? ORDER BY started_seq,operation_id`, claim)
	if e != nil {
		return nil, storage(e)
	}
	defer rows.Close()
	out := []Operation{}
	for rows.Next() {
		var op Operation
		var started, completed int64
		if e := rows.Scan(&op.OperationID, &op.Kind, &op.State, &op.RequestSHA256, &started, &completed); e != nil {
			return nil, storage(e)
		}
		op.ClaimID = claim
		op.StartedAt = time.UnixMicro(started).UTC()
		if completed > 0 {
			t := time.UnixMicro(completed).UTC()
			op.CompletedAt = &t
		}
		if !full {
			op.RequestSHA256 = ""
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

func validID(v string) bool {
	if len(v) != 32 {
		return false
	}
	b, e := hex.DecodeString(v)
	return e == nil && hex.EncodeToString(b) == v
}
func hashToken(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
func storage(err error) error {
	if err == nil {
		return nil
	}
	return reason.New(reason.ReasonStorageFailure, "ledger storage operation failed").With("cause", fmt.Sprint(err))
}
