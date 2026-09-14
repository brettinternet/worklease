// Package gc implements transactional retention for the local authority.
package gc

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

const DefaultRetentionDays = 30.0

// Request describes one retention run. Cutoff is strict: records exactly at
// the cutoff are retained. Now is supplied by callers that own an authority
// clock; a zero value uses the process wall clock.
type Request struct {
	Cutoff        time.Time
	RetentionDays float64
	Apply         bool
	Now           time.Time
}

type Summary struct {
	Count  int        `json:"count"`
	Oldest *time.Time `json:"oldest,omitempty"`
	Newest *time.Time `json:"newest,omitempty"`
}

type Result struct {
	DryRun            bool               `json:"dryRun"`
	CapturedAt        time.Time          `json:"capturedAt"`
	Cutoff            time.Time          `json:"cutoff"`
	RetentionDays     *float64           `json:"retentionDays,omitempty"`
	Eligible          map[string]Summary `json:"eligible"`
	Protected         map[string]Summary `json:"protected"`
	Retired           map[string]Summary `json:"retired,omitempty"`
	Collected         map[string]Summary `json:"collected,omitempty"`
	PrunedThrough     string             `json:"prunedThroughSequence"`
	LastEventSequence string             `json:"lastEventSequence"`
}

type Service struct {
	st *store.Store
	tx *store.Tx
}

func New(st *store.Store) *Service { return &Service{st: st} }

// NewTransaction runs retention inside a caller-owned serialized transaction.
func NewTransaction(tx *store.Tx) *Service { return &Service{tx: tx} }

// Collect previews or applies retention in one authority transaction.
func (s *Service) Collect(ctx context.Context, req Request) (Result, error) {
	if s == nil || (s.st == nil && s.tx == nil) {
		return Result{}, reason.New(reason.ReasonStorageFailure, "authority is not available")
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	cutoff, days, err := resolveCutoff(now, req.Cutoff, req.RetentionDays)
	if err != nil {
		return Result{}, err
	}
	result := Result{DryRun: !req.Apply, CapturedAt: now, Cutoff: cutoff.UTC(), Eligible: map[string]Summary{}, Protected: map[string]Summary{}, PrunedThrough: "0", LastEventSequence: "0"}
	if days != nil {
		result.RetentionDays = days
	}
	work := func(tx *store.Tx) error {
		authorityNow := now.UnixMicro()
		if req.Apply {
			var clockErr error
			authorityNow, clockErr = checkedNow(tx, authorityNow)
			if clockErr != nil {
				return clockErr
			}
		}
		inventory, err := inspect(tx, cutoff.UnixMicro(), authorityNow)
		if err != nil {
			return err
		}
		result.Eligible, result.Protected = inventory.eligible, inventory.protected
		result.PrunedThrough, result.LastEventSequence = inventory.pruned, inventory.last
		if !req.Apply {
			result.Eligible["events"] = inventory.eventSummary
			return nil
		}
		retired := map[string]Summary{}
		for _, claim := range inventory.retire {
			retirementAt := time.UnixMicro(authorityNow).UTC()
			seq, e := tx.AppendEvent(store.Event{At: retirementAt, Kind: "expired-retired", ClaimID: claim.id, Resources: claim.resources, AgentID: claim.agent, Detail: map[string]any{"reason": "expired"}})
			if e != nil {
				return storage(e)
			}
			if _, e = tx.ExecContext(ctx, `UPDATE epochs SET ended_at=?,ended_seq=?,ended_recorded_at=?,end_reason='expired',final_revision=?,checkpoint=? WHERE claim_id=? AND ended_at IS NULL`, claim.expires, seq, authorityNow, claim.revision, nullable(claim.checkpoint), claim.id); e != nil {
				return storage(e)
			}
			if _, e = tx.ExecContext(ctx, `DELETE FROM claims WHERE claim_id=?`, claim.id); e != nil {
				return storage(e)
			}
			addSummary(retired, "expiredClaims", claim.expires)
		}
		result.Retired = retired
		// Recompute after retirement. The just-retired epochs have a fresh
		// ended_recorded_at and therefore cannot be removed in this run.
		post, e := inspect(tx, cutoff.UnixMicro(), authorityNow)
		if e != nil {
			return e
		}
		result.Eligible, result.Protected = post.eligible, post.protected
		for _, epoch := range post.deleteEpochs {
			if _, e = tx.ExecContext(ctx, `DELETE FROM reconciliations WHERE claim_id=?`, epoch.id); e != nil {
				return storage(e)
			}
			if _, e = tx.ExecContext(ctx, `DELETE FROM operations WHERE claim_id=?`, epoch.id); e != nil {
				return storage(e)
			}
			if _, e = tx.ExecContext(ctx, `DELETE FROM epochs WHERE claim_id=?`, epoch.id); e != nil {
				return storage(e)
			}
		}
		collected := map[string]Summary{}
		for _, epoch := range post.deleteEpochs {
			addSummary(collected, "epochs", epoch.recorded)
		}
		for _, ev := range post.deleteEvents {
			if _, e = tx.ExecContext(ctx, `DELETE FROM events WHERE seq=?`, ev.seq); e != nil {
				return storage(e)
			}
			addSummary(collected, "events", ev.at)
		}
		if len(post.deleteEvents) > 0 {
			lastRemoved := post.deleteEvents[len(post.deleteEvents)-1].seq
			if _, e = tx.ExecContext(ctx, `UPDATE meta SET value=? WHERE key='pruned_through_seq' AND CAST(value AS INTEGER) < ?`, strconv.FormatInt(lastRemoved, 10), lastRemoved); e != nil {
				return storage(e)
			}
			result.PrunedThrough = strconv.FormatInt(lastRemoved, 10)
		}
		// This marker is deliberately appended after all deletions. It is never
		// eligible in the same run and makes the write visible in the feed.
		if _, e = tx.AppendEvent(store.Event{At: time.UnixMicro(authorityNow).UTC(), Kind: "gc-applied", Detail: map[string]any{"count": len(post.deleteEpochs) + len(post.deleteEvents)}}); e != nil {
			return storage(e)
		}
		last, pruned, e := readWatermarks(tx)
		if e != nil {
			return e
		}
		result.LastEventSequence, result.PrunedThrough = strconv.FormatInt(last, 10), strconv.FormatInt(pruned, 10)
		result.Collected = collected
		return nil
	}
	if s.tx != nil {
		err = work(s.tx)
	} else if req.Apply {
		err = s.st.WriteAt(ctx, now, work)
	} else {
		err = s.st.Read(ctx, work)
	}
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

// Run is an alias retained for callers that name the operation after the CLI.
func (s *Service) Run(ctx context.Context, req Request) (Result, error) { return s.Collect(ctx, req) }

func resolveCutoff(now, cutoff time.Time, days float64) (time.Time, *float64, error) {
	if !cutoff.IsZero() && days != 0 {
		return time.Time{}, nil, reason.Invalid("retention-days and cutoff are mutually exclusive")
	}
	if !cutoff.IsZero() {
		cutoff = cutoff.UTC()
		if cutoff.After(now) {
			return time.Time{}, nil, reason.Invalid("gc cutoff cannot be in the future")
		}
		return cutoff, nil, nil
	}
	if days == 0 {
		days = DefaultRetentionDays
	}
	if math.IsNaN(days) || math.IsInf(days, 0) || days <= 0 || days > 36500 {
		return time.Time{}, nil, reason.Invalid("retention-days must be greater than 0 and at most 36500")
	}
	return now.Add(-time.Duration(days * 24 * float64(time.Hour))), &days, nil
}

type claim struct {
	id, agent         string
	resources         []string
	expires, revision int64
	checkpoint        string
}
type epoch struct {
	id                 string
	acquired, recorded int64
}
type event struct{ seq, at int64 }
type inventory struct {
	eligible, protected map[string]Summary
	retire              []claim
	deleteEpochs        []epoch
	deleteEvents        []event
	eventSummary        Summary
	pruned, last        string
}

func inspect(tx *store.Tx, cutoff, now int64) (inventory, error) {
	out := inventory{eligible: map[string]Summary{}, protected: map[string]Summary{}}
	rows, err := tx.QueryContext(context.Background(), `SELECT claim_id,agent_id,expires_at,revision,coalesce(checkpoint,'') FROM claims WHERE expires_at < ? ORDER BY expires_at,claim_id`, cutoff)
	if err != nil {
		return out, storage(err)
	}
	for rows.Next() {
		var c claim
		if err := rows.Scan(&c.id, &c.agent, &c.expires, &c.revision, &c.checkpoint); err != nil {
			rows.Close()
			return out, storage(err)
		}
		resources, e := resourcesFor(tx, c.id)
		if e != nil {
			rows.Close()
			return out, e
		}
		c.resources = resources
		protected, e := hasStarted(tx, c.id)
		if e != nil {
			rows.Close()
			return out, e
		}
		if protected {
			addSummary(out.protected, "expiredClaims", c.expires)
		} else {
			addSummary(out.eligible, "expiredClaims", c.expires)
			out.retire = append(out.retire, c)
		}
	}
	if err := rows.Close(); err != nil {
		return out, storage(err)
	}

	// Epochs are considered as a single ordered stream. Once an epoch is
	// retained, newer epochs cannot be deleted, preserving a truthful prefix.
	epochRows, err := tx.QueryContext(context.Background(), `SELECT claim_id,acquired_seq,ended_recorded_at FROM epochs ORDER BY acquired_seq,claim_id`)
	if err != nil {
		return out, storage(err)
	}
	prefix := true
	var retainedSeq int64
	for epochRows.Next() {
		var e epoch
		var recorded sql.NullInt64
		if err := epochRows.Scan(&e.id, &e.acquired, &recorded); err != nil {
			epochRows.Close()
			return out, storage(err)
		}
		if recorded.Valid {
			e.recorded = recorded.Int64
		} else {
			e.recorded = now
		}
		eligible, recordedAt, err := epochEligible(tx, e.id, cutoff, now)
		if err != nil {
			epochRows.Close()
			return out, err
		}
		if prefix && eligible {
			out.deleteEpochs = append(out.deleteEpochs, e)
			addSummary(out.eligible, "epochs", recordedAt)
		} else {
			prefix = false
			active, activeErr := activeClaim(tx, e.id, now)
			if activeErr != nil {
				epochRows.Close()
				return out, activeErr
			}
			if active {
				addSummary(out.protected, "activeClaims", now)
			}
			if retainedSeq == 0 || e.acquired < retainedSeq {
				retainedSeq = e.acquired
			}
			addSummary(out.protected, "epochs", e.recorded)
		}
	}
	if err := epochRows.Close(); err != nil {
		return out, storage(err)
	}

	// Event deletion is also a prefix. The first retained epoch pins all
	// events at and after its acquisition sequence.
	boundary := retainedSeq
	if boundary == 0 {
		boundary = math.MaxInt64
	}
	eventRows, err := tx.QueryContext(context.Background(), `SELECT seq,at FROM events ORDER BY seq`)
	if err != nil {
		return out, storage(err)
	}
	for eventRows.Next() {
		var e event
		if err := eventRows.Scan(&e.seq, &e.at); err != nil {
			eventRows.Close()
			return out, storage(err)
		}
		if e.seq >= boundary || e.at >= cutoff {
			break
		}
		out.deleteEvents = append(out.deleteEvents, e)
	}
	if err := eventRows.Close(); err != nil {
		return out, storage(err)
	}
	for _, e := range out.deleteEvents {
		addSummary(out.eligible, "events", e.at)
	}
	out.eventSummary = out.eligible["events"]
	last, pruned, err := readWatermarks(tx)
	if err != nil {
		return out, err
	}
	out.last, out.pruned = strconv.FormatInt(last, 10), strconv.FormatInt(pruned, 10)
	return out, nil
}

func addSummary(m map[string]Summary, key string, micros int64) {
	v := m[key]
	v.Count++
	t := time.UnixMicro(micros).UTC()
	if v.Oldest == nil || t.Before(*v.Oldest) {
		v.Oldest = &t
	}
	if v.Newest == nil || t.After(*v.Newest) {
		v.Newest = &t
	}
	m[key] = v
}
func resourcesFor(tx *store.Tx, id string) ([]string, error) {
	rows, err := tx.QueryContext(context.Background(), `SELECT resource FROM epoch_resources WHERE claim_id=? ORDER BY position`, id)
	if err != nil {
		return nil, storage(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, storage(err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func activeClaim(tx *store.Tx, id string, now int64) (bool, error) {
	var expires int64
	if err := tx.QueryRowContext(context.Background(), `SELECT expires_at FROM claims WHERE claim_id=?`, id).Scan(&expires); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, storage(err)
	}
	return expires > now, nil
}
func hasStarted(tx *store.Tx, id string) (bool, error) {
	var n int
	if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM operations WHERE claim_id=? AND state='started'`, id).Scan(&n); err != nil {
		return false, storage(err)
	}
	return n != 0, nil
}
func epochEligible(tx *store.Tx, id string, cutoff, now int64) (bool, int64, error) {
	var ended, recorded sql.NullInt64
	if err := tx.QueryRowContext(context.Background(), `SELECT ended_at,ended_recorded_at FROM epochs WHERE claim_id=?`, id).Scan(&ended, &recorded); err != nil {
		return false, 0, storage(err)
	}
	if !ended.Valid || !recorded.Valid || recorded.Int64 >= cutoff {
		return false, maxInt64(ended.Int64, recorded.Int64), nil
	}
	started, err := hasStarted(tx, id)
	if err != nil {
		return false, 0, err
	}
	if started {
		return false, recorded.Int64, nil
	}
	var newest sql.NullInt64
	if err := tx.QueryRowContext(context.Background(), `SELECT max(request_not_after) FROM operations WHERE claim_id=?`, id).Scan(&newest); err != nil {
		return false, 0, storage(err)
	}
	if newest.Valid && newest.Int64 > now {
		return false, newest.Int64, nil
	}
	var rec sql.NullInt64
	if err := tx.QueryRowContext(context.Background(), `SELECT max(recorded_at) FROM reconciliations WHERE claim_id=?`, id).Scan(&rec); err != nil {
		return false, 0, storage(err)
	}
	if rec.Valid && rec.Int64 >= cutoff {
		return false, rec.Int64, nil
	}
	return true, maxInt64(recorded.Int64, maxInt64(newest.Int64, rec.Int64)), nil
}
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func readWatermarks(tx *store.Tx) (int64, int64, error) {
	var lastRaw, prunedRaw string
	if err := tx.QueryRowContext(context.Background(), `SELECT value FROM meta WHERE key='last_event_seq'`).Scan(&lastRaw); err != nil {
		return 0, 0, storage(err)
	}
	if err := tx.QueryRowContext(context.Background(), `SELECT value FROM meta WHERE key='pruned_through_seq'`).Scan(&prunedRaw); err != nil {
		return 0, 0, storage(err)
	}
	last, err := strconv.ParseInt(lastRaw, 10, 64)
	if err != nil || last < 0 {
		return 0, 0, reason.New(reason.ReasonSchemaCorrupt, "event watermark is invalid")
	}
	pruned, err := strconv.ParseInt(prunedRaw, 10, 64)
	if err != nil || pruned < 0 {
		return 0, 0, reason.New(reason.ReasonSchemaCorrupt, "pruned event watermark is invalid")
	}
	return last, pruned, nil
}
func checkedNow(tx *store.Tx, now int64) (int64, error) {
	var raw string
	if err := tx.QueryRowContext(context.Background(), `SELECT value FROM meta WHERE key='last_observed_at'`).Scan(&raw); err != nil {
		return 0, storage(err)
	}
	last, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, reason.New(reason.ReasonSchemaCorrupt, "authority clock watermark is invalid")
	}
	if now < last-1_000_000 {
		return 0, reason.New(reason.ReasonClockRegression, "authority clock moved backwards")
	}
	if now < last {
		return last, nil
	}
	return now, nil
}
func storage(err error) error {
	if err == nil {
		return nil
	}
	return reason.New(reason.ReasonStorageFailure, "garbage collection storage operation failed").With("cause", fmt.Sprint(err))
}
