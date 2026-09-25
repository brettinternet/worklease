package queueindex

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/brettinternet/worklease/internal/queue"
)

// LinearSyncStore keeps metadata-only page checkpoints in a principal- and
// scope-bound partition. Issue payloads are never stored or seeded offline:
// a fresh process must verify live access and enumerate its projection.
type LinearSyncStore struct {
	Index    *Index
	Registry *queue.Registry
}

func (s LinearSyncStore) partition(source queue.Source) (Partition, error) {
	adapter, ok := s.Registry.Get(source.Adapter)
	if !ok {
		return Partition{}, errors.New("linear adapter unavailable")
	}
	identity, ok := adapter.(interface {
		LinearSyncIdentity(queue.Source) (string, string, string, bool)
	})
	if !ok {
		return Partition{}, errors.New("linear sync identity unavailable")
	}
	principal, scope, generation, ok := identity.LinearSyncIdentity(source)
	if !ok || source.Adapter != "linear" || principal == "" || scope == "" || generation == "" {
		return Partition{}, errors.New("linear sync identity unavailable")
	}
	return Partition{Source: source.ID, Principal: principal, Scope: scope, Generation: generation}, nil
}
func (s LinearSyncStore) LockLinearSync(ctx context.Context, source queue.Source) (func(), error) {
	p, err := s.partition(source)
	if err != nil {
		return nil, err
	}
	return s.Index.WaitRefreshLock(ctx, p)
}
func (s LinearSyncStore) LoadLinearSync(ctx context.Context, source queue.Source) (queue.SyncCheckpoint, error) {
	p, err := s.partition(source)
	if err != nil {
		return queue.SyncCheckpoint{}, err
	}
	key, err := p.key()
	if err != nil {
		return queue.SyncCheckpoint{}, err
	}
	var checkpoint queue.SyncCheckpoint
	var committed, scan int64
	err = s.Index.db.QueryRowContext(ctx, `SELECT cursor,committed_watermark,scan_watermark,relation_offset FROM linear_sync WHERE partition=?`, key).Scan(&checkpoint.Cursor, &committed, &scan, &checkpoint.RelationOffset)
	if err == sql.ErrNoRows {
		return checkpoint, nil
	}
	if err != nil {
		return checkpoint, err
	}
	if committed != 0 {
		checkpoint.CommittedWatermark = time.Unix(0, committed).UTC()
	}
	if scan != 0 {
		checkpoint.ScanWatermark = time.Unix(0, scan).UTC()
	}
	return checkpoint, nil
}

// AdvanceLinearRelations records a bounded relation sweep position separately
// from the issue updatedAt window. A new process can continue the sweep.
func (s LinearSyncStore) AdvanceLinearRelations(ctx context.Context, source queue.Source, offset int) error {
	p, err := s.partition(source)
	if err != nil {
		return err
	}
	key, err := p.key()
	if err != nil {
		return err
	}
	if offset < 0 {
		return errors.New("invalid Linear relation offset")
	}
	_, err = s.Index.db.ExecContext(ctx, `UPDATE linear_sync SET relation_offset=? WHERE partition=?`, offset, key)
	return err
}

func (s LinearSyncStore) RestartLinearSync(ctx context.Context, source queue.Source) error {
	p, err := s.partition(source)
	if err != nil {
		return err
	}
	key, err := p.key()
	if err != nil {
		return err
	}
	// A resume cursor alone cannot identify whether it belongs to a full
	// traversal or an incremental window. Restart from a full scan and drop
	// the prior window boundary rather than resume in the wrong mode.
	_, err = s.Index.db.ExecContext(ctx, `UPDATE linear_sync SET cursor='',committed_watermark=0,scan_watermark=0 WHERE partition=?`, key)
	return err
}

// CommitLinearSyncPage stores the cursor after validating the page's scope.
// Only a successfully exhausted window advances the window watermark; it
// never certifies source membership or retires absent rows/relations.
func (s LinearSyncStore) CommitLinearSyncPage(ctx context.Context, source queue.Source, items []queue.Item, cursor string, scan time.Time, finished bool) error {
	p, err := s.partition(source)
	if err != nil {
		return err
	}
	key, err := p.key()
	if err != nil {
		return err
	}
	if scan.IsZero() || finished && cursor != "" {
		return errors.New("invalid Linear sync window")
	}
	tx, err := s.Index.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range items {
		if item.Ref.SourceID != source.ID {
			return errors.New("linear item outside source")
		}
	}
	var committed int64
	err = tx.QueryRowContext(ctx, `SELECT committed_watermark FROM linear_sync WHERE partition=?`, key).Scan(&committed)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if finished {
		committed = scan.UnixNano()
		cursor = ""
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO linear_sync(partition,cursor,committed_watermark,scan_watermark) VALUES(?,?,?,?) ON CONFLICT(partition) DO UPDATE SET cursor=excluded.cursor,committed_watermark=excluded.committed_watermark,scan_watermark=excluded.scan_watermark`, key, cursor, committed, scan.UnixNano())
	if err != nil {
		return err
	}
	return tx.Commit()
}

var _ queue.LinearSyncStore = LinearSyncStore{}
