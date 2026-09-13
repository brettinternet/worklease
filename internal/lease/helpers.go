package lease

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

type claimRow struct {
	ClaimID, TokenHash, AgentID, SessionID, WorkKey, Guarantee string
	Revision                                                   int64
	LocalReplaceAllowed                                        bool
	AcquiredAt, TTL, HeartbeatAt, ExpiresAt                    int64
	Checkpoint                                                 string
	Resources                                                  []string
}

func (r *claimRow) scanArgs() []any {
	return []any{&r.ClaimID, &r.TokenHash, &r.Revision, &r.AgentID, &r.SessionID, &r.WorkKey, &r.Guarantee, &r.LocalReplaceAllowed, &r.AcquiredAt, &r.TTL, &r.HeartbeatAt, &r.ExpiresAt, &r.Checkpoint}
}

const claimSelect = `SELECT claim_id,token_hash,revision,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,ttl_us,heartbeat_at,expires_at,coalesce(checkpoint,'') FROM claims`

func readClaim(tx *store.Tx, id string, r *claimRow) (bool, error) {
	var x int
	err := tx.QueryRowContext(context.Background(), claimSelect+` WHERE claim_id=?`, id).Scan(r.scanArgs()...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return false, nil
		}
		return false, storage(err)
	}
	_ = x
	if err := loadResources(tx, r); err != nil {
		return false, err
	}
	return true, nil
}
func loadResources(tx *store.Tx, r *claimRow) error {
	rows, err := tx.QueryContext(context.Background(), `SELECT resource FROM claim_resources WHERE claim_id=? ORDER BY position`, r.ClaimID)
	if err != nil {
		return storage(err)
	}
	defer rows.Close()
	for rows.Next() {
		var resource string
		if err := rows.Scan(&resource); err != nil {
			return storage(err)
		}
		r.Resources = append(r.Resources, resource)
	}
	return storage(rows.Err())
}
func readClaimForResource(tx *store.Tx, res string, r *claimRow) (bool, error) {
	err := tx.QueryRowContext(context.Background(), claimSelect+` WHERE claim_id=(SELECT claim_id FROM claim_resources WHERE resource=?)`, res).Scan(r.scanArgs()...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return false, nil
		}
		return false, storage(err)
	}
	rows, err := tx.QueryContext(context.Background(), `SELECT resource FROM claim_resources WHERE claim_id=? ORDER BY position`, r.ClaimID)
	if err != nil {
		return false, storage(err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return false, storage(err)
		}
		r.Resources = append(r.Resources, v)
	}
	return true, rows.Err()
}
func (r claimRow) view(authority string, now int64) (ClaimView, error) {
	return ClaimView{ClaimID: r.ClaimID, Resources: append([]string(nil), r.Resources...), AgentID: r.AgentID, SessionID: r.SessionID, WorkKey: r.WorkKey, Guarantee: r.Guarantee, AuthorityID: authority, Revision: r.Revision, AcquiredAt: time.UnixMicro(r.AcquiredAt).UTC(), HeartbeatAt: time.UnixMicro(r.HeartbeatAt).UTC(), ExpiresAt: time.UnixMicro(r.ExpiresAt).UTC(), LocalReplaceAllowed: r.LocalReplaceAllowed, Active: now < r.ExpiresAt, CheckpointPresent: r.Checkpoint != ""}, nil
}

type operationRow struct {
	ClaimID, OperationID, Kind, RequestHash, TokenHash, State, Receipt                  string
	RequestNotAfter, ExpectedRevision, StartedAt, StartedSeq, CompletedAt, CompletedSeq int64
}

func readOperation(tx *store.Tx, claim, id string, r *operationRow) (bool, error) {
	err := tx.QueryRowContext(context.Background(), `SELECT o.claim_id,o.operation_id,o.kind,o.request_hash,o.request_not_after,o.expected_revision,o.state,coalesce(o.receipt,''),o.started_at,o.started_seq,coalesce(o.completed_at,0),coalesce(o.completed_seq,0),e.token_hash FROM operations o JOIN epochs e ON e.claim_id=o.claim_id WHERE o.claim_id=? AND o.operation_id=?`, claim, id).Scan(&r.ClaimID, &r.OperationID, &r.Kind, &r.RequestHash, &r.RequestNotAfter, &r.ExpectedRevision, &r.State, &r.Receipt, &r.StartedAt, &r.StartedSeq, &r.CompletedAt, &r.CompletedSeq, &r.TokenHash)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return false, nil
		}
		return false, storage(err)
	}
	return true, nil
}
func unresolvedForResources(tx *store.Tx, resources []string) ([]string, error) {
	return unresolvedForResourcesExcept(tx, resources, "")
}

// unresolvedRecoveryClosure returns every started predecessor operation that
// touches resources and the transitive ordered union of resources required to
// recover them without splitting responsibility between successors.
func unresolvedRecoveryClosure(tx *store.Tx, resources []string) ([]string, []string, error) {
	required := append([]string(nil), resources...)
	seenResources := make(map[string]bool, len(required))
	for _, resource := range required {
		seenResources[resource] = true
	}
	seenOperations := map[string]bool{}
	var operations []string
	for {
		query := `SELECT DISTINCT o.claim_id,o.operation_id,o.started_seq FROM operations o JOIN epoch_resources er ON er.claim_id=o.claim_id WHERE o.state='started' AND er.resource IN (` + placeholders(len(required)) + `) ORDER BY o.started_seq,o.claim_id,o.operation_id`
		rows, err := tx.QueryContext(context.Background(), query, stringsToAny(required)...)
		if err != nil {
			return nil, nil, storage(err)
		}
		type startedOperation struct{ claimID, operationID string }
		var found []startedOperation
		for rows.Next() {
			var operation startedOperation
			var sequence int64
			if err := rows.Scan(&operation.claimID, &operation.operationID, &sequence); err != nil {
				rows.Close()
				return nil, nil, storage(err)
			}
			found = append(found, operation)
		}
		if err := rows.Close(); err != nil {
			return nil, nil, storage(err)
		}
		changed := false
		for _, operation := range found {
			key := operation.claimID + "\x00" + operation.operationID
			if !seenOperations[key] {
				seenOperations[key] = true
				operations = append(operations, operation.operationID)
			}
			memberRows, err := tx.QueryContext(context.Background(), `SELECT resource FROM epoch_resources WHERE claim_id=? ORDER BY position`, operation.claimID)
			if err != nil {
				return nil, nil, storage(err)
			}
			for memberRows.Next() {
				var resource string
				if err := memberRows.Scan(&resource); err != nil {
					memberRows.Close()
					return nil, nil, storage(err)
				}
				if !seenResources[resource] {
					seenResources[resource] = true
					required = append(required, resource)
					changed = true
				}
			}
			if err := memberRows.Close(); err != nil {
				return nil, nil, storage(err)
			}
		}
		if !changed {
			return operations, required, nil
		}
	}
}
func unresolvedForResourcesExcept(tx *store.Tx, resources []string, exceptClaim string) ([]string, error) {
	if len(resources) == 0 {
		return nil, nil
	}
	query := `SELECT DISTINCT o.operation_id FROM operations o JOIN epoch_resources er ON er.claim_id=o.claim_id WHERE o.state='started' AND er.resource IN (` + placeholders(len(resources)) + `)`
	args := stringsToAny(resources)
	if exceptClaim != "" {
		query += ` AND o.claim_id<>?`
		args = append(args, exceptClaim)
	}
	query += ` ORDER BY o.operation_id`
	rows, err := tx.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, storage(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, storage(err)
		}
		ids = append(ids, id)
	}
	return ids, storage(rows.Err())
}
func placeholders(n int) string {
	p := make([]string, n)
	for i := range p {
		p[i] = "?"
	}
	return strings.Join(p, ",")
}
func stringsToAny(v []string) []any {
	out := make([]any, len(v))
	for i := range v {
		out[i] = v[i]
	}
	return out
}
func latestRecovery(tx *store.Tx, resource string) (Recovery, bool, error) {
	var claim, checkpoint string
	err := tx.QueryRowContext(context.Background(), `SELECT e.claim_id,coalesce(e.checkpoint,'') FROM epochs e JOIN epoch_resources er ON er.claim_id=e.claim_id WHERE er.resource=? AND e.ended_at IS NOT NULL ORDER BY e.acquired_seq DESC LIMIT 1`, resource).Scan(&claim, &checkpoint)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return Recovery{}, false, nil
		}
		return Recovery{}, false, storage(err)
	}
	var value any
	if checkpoint != "" {
		if err := json.Unmarshal([]byte(checkpoint), &value); err != nil {
			return Recovery{}, false, storage(err)
		}
	}
	return Recovery{Resource: resource, ClaimID: claim, CheckpointPresent: checkpoint != "", Checkpoint: value}, true, nil
}
func hasStarted(tx *store.Tx, claim string) (bool, error) {
	var n int
	err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM operations WHERE claim_id=? AND state='started'`, claim).Scan(&n)
	return n > 0, err
}
func (s *Service) effectiveNow(tx *store.Tx, now time.Time) (time.Time, error) {
	var raw string
	if err := tx.QueryRowContext(context.Background(), `SELECT value FROM meta WHERE key='last_observed_at'`).Scan(&raw); err != nil {
		return time.Time{}, storage(err)
	}
	var observed int64
	if _, err := fmt.Sscan(raw, &observed); err != nil {
		return time.Time{}, storage(err)
	}
	n := now.UnixMicro()
	if n < observed-1_000_000 {
		return time.Time{}, reason.New(reason.ReasonClockRegression, "authority clock moved backward")
	}
	if n < observed {
		n = observed
	}
	return time.UnixMicro(n).UTC(), nil
}
func (s *Service) authorize(row claimRow, c Credentials, now int64, allowStarted bool) error {
	if c.AuthorityID != "" && c.AuthorityID != s.st.AuthorityID() {
		return reason.New(reason.ReasonAuthorityMismatch, "authority identity does not match")
	}
	if c.ClaimID != row.ClaimID {
		return reason.New(reason.ReasonStaleClaim, "claim identity is not current")
	}
	if len(c.Token) != 64 {
		return reason.New(reason.ReasonInvalidToken, "credential is invalid")
	}
	if subtle.ConstantTimeCompare([]byte(row.TokenHash), []byte(hashToken(c.Token))) != 1 {
		return reason.New(reason.ReasonInvalidToken, "credential is invalid")
	}
	if now >= row.ExpiresAt {
		return reason.New(reason.ReasonClaimExpired, "claim has expired")
	}
	if allowStarted && c.Revision <= 0 {
		return reason.New(reason.ReasonStaleRevision, "expected revision is required")
	}
	if c.Revision != 0 && c.Revision != row.Revision {
		return reason.New(reason.ReasonStaleRevision, "claim revision is stale")
	}
	return nil
}
func (s *Service) mutateCurrent(tx *store.Tx, c Credentials, id, kind, hash, legacyHash string, deadline time.Time, now time.Time, apply func(claimRow, int64) (map[string]any, error)) (Receipt, error) {
	var row claimRow
	ok, err := readClaim(tx, c.ClaimID, &row)
	if err != nil {
		return Receipt{}, err
	}
	var op operationRow
	if found, e := readOperation(tx, c.ClaimID, id, &op); e != nil {
		return Receipt{}, e
	} else if found {
		if subtle.ConstantTimeCompare([]byte(op.TokenHash), []byte(hashToken(c.Token))) != 1 {
			return Receipt{}, reason.New(reason.ReasonInvalidToken, "credential is invalid")
		}
		if !requestHashMatches(op.RequestHash, hash, legacyHash) {
			return Receipt{}, reason.New(reason.ReasonOperationRequestMismatch, "request intent differs from the recorded operation")
		}
		if now.UnixMicro() >= op.RequestNotAfter {
			return Receipt{}, reason.New(reason.ReasonReplayExpired, "request replay deadline has passed")
		}
		if op.State == "started" {
			return Receipt{}, reason.New(reason.ReasonUnknownOutcome, "operation outcome is unresolved")
		}
		return receiptFromOperation(op, true, nil)
	}
	if !ok {
		return Receipt{}, reason.New(reason.ReasonStaleClaim, "claim is not current")
	}
	if err := s.authorize(row, c, now.UnixMicro(), true); err != nil {
		return Receipt{}, err
	}
	if !deadline.After(now) {
		return Receipt{}, reason.New(reason.ReasonReplayExpired, "request replay deadline has passed")
	}
	rev := row.Revision + 1
	result, err := apply(row, rev)
	if err != nil {
		return Receipt{}, err
	}
	var seq int64
	if existingSeq, ok := result["_eventSeq"].(int64); ok {
		seq = existingSeq
		delete(result, "_eventSeq")
	} else {
		seq, err = tx.AppendEvent(store.Event{At: now, Kind: eventKind(kind), ClaimID: row.ClaimID, Resources: row.Resources, OperationID: id, Revision: &rev, AgentID: row.AgentID, Detail: publicReceiptDetail(result)})
		if err != nil {
			return Receipt{}, storage(err)
		}
	}
	encoded, _ := json.Marshal(result)
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO operations(claim_id,operation_id,kind,request_hash,request_not_after,expected_revision,state,receipt,started_at,started_seq,completed_at,completed_seq) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, row.ClaimID, id, kind, hash, deadline.UnixMicro(), row.Revision, "completed", string(encoded), now.UnixMicro(), seq, now.UnixMicro(), seq); err != nil {
		return Receipt{}, storage(err)
	}
	return Receipt{OperationID: id, ClaimID: row.ClaimID, Kind: kind, RequestHash: hash, Revision: rev, Committed: true, Result: result}, nil
}
func eventKind(kind string) string {
	switch kind {
	case "heartbeat":
		return "renewed"
	case "checkpoint":
		return "checkpointed"
	case "release":
		return "released"
	case "transfer":
		return "transferred"
	default:
		return kind
	}
}
func (s *Service) releaseInTx(tx *store.Tx, row claimRow, now time.Time, rev int64, id, why, hash string) (map[string]any, error) {
	seq, err := tx.AppendEvent(store.Event{At: now, Kind: "released", ClaimID: row.ClaimID, Resources: row.Resources, OperationID: id, Revision: &rev, AgentID: row.AgentID, Detail: map[string]any{"reason": why, "checkpointPresent": row.Checkpoint != ""}})
	if err != nil {
		return nil, storage(err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE epochs SET ended_at=?,ended_seq=?,ended_recorded_at=?,end_reason='released',final_revision=?,checkpoint=? WHERE claim_id=?`, now.UnixMicro(), seq, now.UnixMicro(), rev, nullString(row.Checkpoint), row.ClaimID); err != nil {
		return nil, storage(err)
	}
	if _, err := tx.ExecContext(context.Background(), `DELETE FROM claims WHERE claim_id=?`, row.ClaimID); err != nil {
		return nil, storage(err)
	}
	result := map[string]any{"released": true, "revision": rev, "reason": why, "_eventSeq": seq}
	return result, nil
}
func endClaim(tx *store.Tx, row claimRow, recordedAt int64, endReason, event string, successor *string) error {
	rev := row.Revision
	seq, err := tx.AppendEvent(store.Event{At: time.UnixMicro(recordedAt), Kind: event, ClaimID: row.ClaimID, Resources: row.Resources, Revision: &rev, AgentID: row.AgentID, Detail: map[string]any{"checkpointPresent": row.Checkpoint != ""}})
	if err != nil {
		return storage(err)
	}
	var succ any
	if successor != nil {
		succ = *successor
	}
	endedAt := recordedAt
	if endReason == "expired" {
		endedAt = row.ExpiresAt
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE epochs SET ended_at=?,ended_seq=?,ended_recorded_at=?,end_reason=?,final_revision=?,successor_claim_id=?,checkpoint=? WHERE claim_id=?`, endedAt, seq, recordedAt, endReason, rev, succ, nullString(row.Checkpoint), row.ClaimID); err != nil {
		return storage(err)
	}
	_, err = tx.ExecContext(context.Background(), `DELETE FROM claims WHERE claim_id=?`, row.ClaimID)
	return storage(err)
}
func grantFromClaimOrEpoch(tx *store.Tx, id string, receipt string, idempotent bool, authority string, now int64) (Grant, error) {
	var row claimRow
	ok, err := readClaim(tx, id, &row)
	if err != nil {
		return Grant{}, err
	}
	if ok {
		v, e := row.view(authority, now)
		if e != nil {
			return Grant{}, e
		}
		var result map[string]any
		_ = json.Unmarshal([]byte(receipt), &result)
		return Grant{ClaimID: id, Resources: row.Resources, AgentID: row.AgentID, SessionID: row.SessionID, WorkKey: row.WorkKey, Revision: row.Revision, AcquiredAt: time.UnixMicro(row.AcquiredAt).UTC(), ExpiresAt: time.UnixMicro(row.ExpiresAt).UTC(), Guarantee: row.Guarantee, AuthorityID: authority, LocalReplaceAllowed: row.LocalReplaceAllowed, Active: v.Active, Receipt: Receipt{OperationID: id, ClaimID: id, Kind: "acquire", Revision: resultRevision(result, 1), Idempotent: idempotent, Committed: true, Result: result}}, nil
	}
	var agent, session, work, guarantee, token, checkpoint string
	var replace, acquired, ended, final int64
	err = tx.QueryRowContext(context.Background(), `SELECT token_hash,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,coalesce(ended_at,0),coalesce(final_revision,0),coalesce(checkpoint,'') FROM epochs WHERE claim_id=?`, id).Scan(&token, &agent, &session, &work, &guarantee, &replace, &acquired, &ended, &final, &checkpoint)
	if err != nil {
		return Grant{}, reason.New(reason.ReasonOperationNotFound, "claim operation was not retained")
	}
	var resources []string
	rows, e := tx.QueryContext(context.Background(), `SELECT resource FROM epoch_resources WHERE claim_id=? ORDER BY position`, id)
	if e == nil {
		for rows.Next() {
			var r string
			if rows.Scan(&r) == nil {
				resources = append(resources, r)
			}
		}
		rows.Close()
	}
	var recorded map[string]any
	_ = json.Unmarshal([]byte(receipt), &recorded)
	return Grant{ClaimID: id, Resources: resources, AgentID: agent, SessionID: session, WorkKey: work, Revision: final, AcquiredAt: time.UnixMicro(acquired).UTC(), Guarantee: guarantee, AuthorityID: authority, LocalReplaceAllowed: replace != 0, Active: false, Receipt: Receipt{OperationID: id, ClaimID: id, Kind: "acquire", Revision: resultRevision(recorded, 1), Idempotent: idempotent, Committed: true, Result: recorded}}, nil
}

func resultRevision(result map[string]any, fallback int64) int64 {
	if value, ok := result["revision"]; ok {
		switch v := value.(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return n
			}
		}
	}
	return fallback
}
func replayOperation(op operationRow, hash string, now time.Time) error {
	if op.RequestHash != hash {
		return reason.New(reason.ReasonOperationRequestMismatch, "request intent differs from the recorded operation")
	}
	if now.UnixMicro() >= op.RequestNotAfter {
		return reason.New(reason.ReasonReplayExpired, "request replay deadline has passed")
	}
	if op.State == "started" {
		return reason.New(reason.ReasonUnknownOutcome, "operation outcome is unresolved")
	}
	return nil
}
func receiptFromOperation(op operationRow, idempotent bool, out *Receipt) (Receipt, error) {
	var result map[string]any
	if op.Receipt != "" {
		if err := json.Unmarshal([]byte(op.Receipt), &result); err != nil {
			return Receipt{}, storage(err)
		}
	}
	revision := op.ExpectedRevision
	if value, ok := result["revision"]; ok {
		switch v := value.(type) {
		case float64:
			revision = int64(v)
		case int64:
			revision = v
		case json.Number:
			if n, err := v.Int64(); err == nil {
				revision = n
			}
		}
	}
	r := Receipt{OperationID: op.OperationID, ClaimID: op.ClaimID, Kind: op.Kind, RequestHash: op.RequestHash, Revision: revision, Idempotent: idempotent, Committed: true, Result: result}
	if out != nil {
		*out = r
	}
	return r, nil
}
func lifecycleRequestHash(v map[string]any, holdUntil time.Time) string {
	if !holdUntil.IsZero() {
		v["holdUntil"] = holdUntil.UTC().UnixMicro()
	}
	return requestHash(v)
}

func requestHashMatches(recorded, current, legacy string) bool {
	return recorded == current || (legacy != "" && recorded == legacy)
}

func requestHash(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic("internal request is not JSON-serializable: " + err.Error())
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func checkedRequestHash(v map[string]any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", reason.Invalid("operation intent must be finite JSON")
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func validateToken(token string) error {
	if len(token) != 64 {
		return reason.New(reason.ReasonInvalidToken, "credential must be a 64-character lowercase hexadecimal value")
	}
	if _, err := hex.DecodeString(token); err != nil || token != strings.ToLower(token) {
		return reason.New(reason.ReasonInvalidToken, "credential must be a 64-character lowercase hexadecimal value")
	}
	return nil
}
func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && id == strings.ToLower(id)
}
func strictCheckpoint(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	if err := ensureEnd(dec); err != nil {
		return nil, err
	}
	check := json.NewDecoder(bytes.NewReader(data))
	check.UseNumber()
	if err := scanJSON(check); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
func ensureEnd(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}
func scanJSON(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); ok {
		if d == '{' {
			seen := map[string]bool{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyTok.(string)
				if !ok || seen[key] {
					return fmt.Errorf("duplicate or invalid object key")
				}
				seen[key] = true
				if credentialKey(key) {
					return fmt.Errorf("credential-like checkpoint field")
				}
				if err := scanJSON(dec); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim('}') {
				return fmt.Errorf("invalid object")
			}
			return nil
		}
		if d == '[' {
			for dec.More() {
				if err := scanJSON(dec); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim(']') {
				return fmt.Errorf("invalid array")
			}
			return nil
		}
		return fmt.Errorf("invalid JSON delimiter")
	}
	return nil
}
func credentialKey(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	switch k {
	case "token", "tokenhash", "bearer", "credential", "credentials", "password", "secret", "secretkey":
		return true
	}
	return false
}
func validateRequestWindow(deadline, now time.Time) error {
	if deadline.IsZero() || !deadline.After(now) || deadline.After(now.Add(24*time.Hour)) {
		return reason.New(reason.ReasonReplayExpired, "request replay deadline must be within 24 hours")
	}
	return nil
}
func validateOperationID(id string) error {
	if id == "" || !validID(id) {
		return reason.Invalid("operation ID must be 32 lowercase hex characters")
	}
	return nil
}
func validateTTL(ttl time.Duration) error {
	if ttl < minTTL || ttl > maxTTL {
		return reason.Invalid("ttl must be between 1s and 1h")
	}
	return nil
}
func validateResources(resources []string) error {
	if len(resources) < 1 || len(resources) > 32 {
		return reason.New(reason.ReasonInvalidResource, "claim requires 1 to 32 resources")
	}
	seen := map[string]bool{}
	for _, r := range resources {
		if !utf8.ValidString(r) || r == "" || len([]byte(r)) > 1024 || strings.ContainsAny(r, "\x00\r\n") || strings.TrimSpace(r) != r {
			return reason.New(reason.ReasonInvalidResource, "resource is invalid")
		}
		if seen[r] {
			return reason.New(reason.ReasonInvalidResource, "duplicate resource")
		}
		seen[r] = true
	}
	return nil
}
func validateIdentity(name, value string) error {
	if !utf8.ValidString(value) || len([]byte(value)) < 1 || len([]byte(value)) > 128 || hasControl(value) {
		return reason.Invalid(name + " must be 1 to 128 bytes without control characters")
	}
	return nil
}

func validatePublicText(name, value string) error {
	if !utf8.ValidString(value) || len([]byte(value)) < 1 || len([]byte(value)) > 1024 || hasControl(value) {
		return reason.Invalid(name + " must be 1 to 1024 bytes without control characters")
	}
	return nil
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0
}

func containsResources(have, want []string) bool {
	members := make(map[string]bool, len(have))
	for _, value := range have {
		members[value] = true
	}
	for _, value := range want {
		if !members[value] {
			return false
		}
	}
	return true
}

func sameResourceMembers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	members := make(map[string]bool, len(a))
	for _, resource := range a {
		members[resource] = true
	}
	for _, resource := range b {
		if !members[resource] {
			return false
		}
	}
	return true
}
func formatMicros(v int64) string { return time.UnixMicro(v).UTC().Format(time.RFC3339Nano) }
func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func storage(err error) error {
	if err == nil {
		return nil
	}
	if reason.As(err) != nil {
		return err
	}
	return reason.New(reason.ReasonStorageFailure, "authority storage failure: "+err.Error())
}
func reasonCode(err error) string {
	if e := reason.As(err); e != nil {
		return e.Reason
	}
	return reason.ReasonInternal
}
func publicReceiptDetail(v map[string]any) map[string]any {
	d := map[string]any{}
	for _, k := range []string{"reason", "mutationProtection", "providerMutationFenced", "exitStatus", "timedOut"} {
		if x, ok := v[k]; ok {
			d[k] = x
		}
	}
	return d
}
