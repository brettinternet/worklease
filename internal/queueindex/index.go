// Package queueindex stores disposable, owner-private queue projections.
package queueindex

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/queue"
	_ "modernc.org/sqlite"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const SchemaGeneration = 7
const Retention = 30 * 24 * time.Hour

type Partition struct{ Source, Principal, Scope, Generation string }

type cacheIdentity interface {
	QueueCacheIdentity(queue.Source) (principal, scope, generation string, available bool)
}

// ForSource returns a partition only when the adapter can establish a stable access boundary.
func ForSource(adapter any, source queue.Source) (Partition, bool) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return Partition{}, false
	}
	provider, ok := adapter.(cacheIdentity)
	if !ok {
		return Partition{}, false
	}
	principal, scope, generation, available := provider.QueueCacheIdentity(source)
	if !available || principal == "" || scope == "" || generation == "" {
		return Partition{}, false
	}
	return Partition{Source: source.ID, Principal: principal, Scope: scope, Generation: generation}, true
}

type Index struct {
	db          *sql.DB
	dir         string
	indexBodies bool
}

// EnableBodyIndex opts into indexing bodies on subsequent refreshes.
func (i *Index) EnableBodyIndex() { i.indexBodies = true }

func CacheDir(getenv func(string) string, home string) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	base := getenv("XDG_CACHE_HOME")
	if base == "" {
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", err
			}
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "worklease", "queue"), nil
}
func Open(ctx context.Context, dir string) (*Index, error) {
	return open(ctx, dir, true)
}

func open(ctx context.Context, dir string, allowRebuild bool) (*Index, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "index.sqlite")
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = os.Chmod(path, 0600); err != nil && !os.IsNotExist(err) {
		db.Close()
		return nil, err
	}
	idx := &Index{db: db, dir: dir}
	if err := idx.migrate(ctx); err != nil {
		_ = db.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !allowRebuild || !rebuildableIndexError(err) {
			return nil, err
		}
		if err := rebuild(path); err != nil {
			return nil, fmt.Errorf("rebuild corrupt queue index: %w", err)
		}
		return open(ctx, dir, false)
	}
	for _, privatePath := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(privatePath, 0600); err != nil && !os.IsNotExist(err) {
			_ = db.Close()
			return nil, err
		}
	}
	return idx, nil
}
func rebuildableIndexError(err error) bool {
	if strings.Contains(err.Error(), "unknown queue index schema generation") || strings.Contains(err.Error(), "invalid queue index schema") {
		return true
	}
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	code := sqliteErr.Code() & 0xff
	return code == sqlite3.SQLITE_CORRUPT || code == sqlite3.SQLITE_NOTADB
}

func rebuild(path string) error {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
func (i *Index) migrate(ctx context.Context) error {
	var mode string
	if err := i.db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		return err
	}
	if mode != "wal" {
		return fmt.Errorf("queue index requires WAL mode")
	}
	// Concurrent processes may open a new index at once. BEGIN IMMEDIATE takes
	// the write lock (waiting up to busy_timeout) before the version check, so
	// exactly one process creates the schema and the others observe it.
	conn, err := i.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 0 && version != 2 && version != 3 && version != 4 && version != 5 && version != 6 && version != SchemaGeneration {
		return fmt.Errorf("unknown queue index schema generation %d", version)
	}
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS body_partitions (partition TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	if version == SchemaGeneration {
		for table, requiredColumns := range map[string][]string{
			"entries":         {"partition", "ref", "payload", "observed"},
			"partitions":      {"partition", "observed", "complete"},
			"search":          {"partition", "ref", "title", "body"},
			"body_partitions": {"partition"},
		} {
			rows, err := conn.QueryContext(ctx, "PRAGMA table_info("+table+")")
			if err != nil {
				return err
			}
			columns := map[string]bool{}
			for rows.Next() {
				var cid, notnull, pk int
				var name, kind string
				var defaultValue any
				if err := rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
					rows.Close()
					return err
				}
				columns[name] = true
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
			for _, column := range requiredColumns {
				if !columns[column] {
					return fmt.Errorf("invalid queue index schema: required column %s.%s is missing", table, column)
				}
			}
		}
	}
	if version == 0 {
		for _, statement := range []string{
			`CREATE TABLE entries (partition TEXT NOT NULL, ref TEXT NOT NULL, payload BLOB NOT NULL, observed INTEGER NOT NULL, PRIMARY KEY(partition,ref))`,
			`CREATE TABLE partitions (partition TEXT PRIMARY KEY, observed INTEGER NOT NULL, complete INTEGER NOT NULL)`,
			`CREATE VIRTUAL TABLE search USING fts5(partition UNINDEXED,ref UNINDEXED,title,body)`,
			`PRAGMA user_version=2`,
		} {
			if _, err = conn.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		version = 2
	}
	if version == 2 {
		if _, err = conn.ExecContext(ctx, `CREATE TABLE github_sync (partition TEXT PRIMARY KEY, cursor TEXT NOT NULL, committed_watermark INTEGER NOT NULL, scan_watermark INTEGER NOT NULL, reconciliation_cursor TEXT NOT NULL, reconciliation_generation INTEGER NOT NULL, reconciliation_started INTEGER NOT NULL); PRAGMA user_version=3`); err != nil {
			return err
		}
		version = 3
	}
	if version == 3 {
		if _, err = conn.ExecContext(ctx, `CREATE TABLE github_nodes (partition TEXT NOT NULL, node_id TEXT NOT NULL, ref TEXT NOT NULL, PRIMARY KEY(partition,node_id)); PRAGMA user_version=4`); err != nil {
			return err
		}
		version = 4
	}
	if version == 4 {
		if _, err = conn.ExecContext(ctx, `ALTER TABLE github_nodes ADD COLUMN seen_generation INTEGER NOT NULL DEFAULT 0; PRAGMA user_version=5`); err != nil {
			return err
		}
		version = 5
	}
	if version == 5 {
		if _, err = conn.ExecContext(ctx, `CREATE TABLE github_absences (partition TEXT NOT NULL, ref TEXT NOT NULL, node_id TEXT NOT NULL, classification TEXT NOT NULL CHECK (classification IN ('moved','unknown')), observed INTEGER NOT NULL, PRIMARY KEY(partition,ref)); PRAGMA user_version=6`); err != nil {
			return err
		}
		version = 6
	}
	if version == 6 {
		for _, statement := range []string{
			`CREATE TABLE github_absences_v7 (partition TEXT NOT NULL, ref TEXT NOT NULL, node_id TEXT NOT NULL, classification TEXT NOT NULL CHECK (classification IN ('moved','unknown','inaccessible')), observed INTEGER NOT NULL, PRIMARY KEY(partition,ref))`,
			`INSERT INTO github_absences_v7 SELECT * FROM github_absences`,
			`DROP TABLE github_absences`,
			`ALTER TABLE github_absences_v7 RENAME TO github_absences`,
			`PRAGMA user_version=7`,
		} {
			if _, err = conn.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}
func (i *Index) Close() error { return i.db.Close() }
func (p Partition) key() (string, error) {
	if p.Source == "" || p.Principal == "" || p.Scope == "" || p.Generation == "" {
		return "", errors.New("queue index requires source, principal, access scope, and configuration generation")
	}
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
func (i *Index) Read(ctx context.Context, p Partition, maxAge time.Duration) (queue.Snapshot, time.Time, bool, error) {
	if err := i.purgeExpired(ctx); err != nil {
		return queue.Snapshot{}, time.Time{}, false, err
	}
	key, err := p.key()
	if err != nil {
		return queue.Snapshot{}, time.Time{}, false, err
	}
	s := queue.Snapshot{Items: map[string]queue.Item{}, Sources: map[string]queue.Coverage{}}
	rows, err := i.db.QueryContext(ctx, "SELECT payload,observed FROM entries WHERE partition=?", key)
	if err != nil {
		return s, time.Time{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var b []byte
		var n int64
		if err = rows.Scan(&b, &n); err != nil {
			return s, time.Time{}, false, err
		}
		var item queue.Item
		if err = json.Unmarshal(b, &item); err != nil {
			return s, time.Time{}, false, err
		}
		s.Items[item.Ref.Key()] = item
	}
	if err = rows.Err(); err != nil {
		return s, time.Time{}, false, err
	}
	var observedNS int64
	var complete int
	err = i.db.QueryRowContext(ctx, "SELECT observed,complete FROM partitions WHERE partition=?", key).Scan(&observedNS, &complete)
	if err != nil && err != sql.ErrNoRows {
		return s, time.Time{}, false, err
	}
	observed := time.Unix(0, observedNS)
	if err == sql.ErrNoRows {
		return s, time.Time{}, false, nil
	}
	state := queue.CoveragePartial
	if complete == 1 {
		state = queue.CoverageComplete
	}
	s.Sources[p.Source] = queue.Coverage{State: state, Reason: "cached-index", TotalAccuracy: queue.TotalUnknown}
	fresh := complete == 1 && maxAge >= 0 && time.Since(observed) <= maxAge
	if !fresh {
		for key, item := range s.Items {
			item.Fresh = false
			item.ReadOutcome = "stale"
			s.Items[key] = item
		}
	}
	return s, observed, fresh, nil
}

// Replace reconciles only a completed scan; incomplete scans only upsert observations.
func (i *Index) Replace(ctx context.Context, p Partition, items []queue.Item, complete bool) error {
	return i.ReplaceWithDeletes(ctx, p, items, nil, complete)
}

func (i *Index) ReplaceWithDeletes(ctx context.Context, p Partition, items []queue.Item, deleted []queue.Ref, complete bool) error {
	key, err := p.key()
	if err != nil {
		return err
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if complete {
		if _, err = tx.ExecContext(ctx, "DELETE FROM entries WHERE partition=?", key); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM search WHERE partition=?", key); err != nil {
			return err
		}
	}
	for _, ref := range deleted {
		if _, err = tx.ExecContext(ctx, "DELETE FROM entries WHERE partition=? AND ref=?", key, ref.Key()); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM search WHERE partition=? AND ref=?", key, ref.Key()); err != nil {
			return err
		}
	}
	for _, item := range items {
		b, e := json.Marshal(item)
		if e != nil {
			return e
		}
		observed := item.Observation.ObservedAt
		if observed.IsZero() {
			observed = time.Now()
		}
		if _, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO entries(partition,ref,payload,observed) VALUES(?,?,?,?)", key, item.Ref.Key(), b, observed.UnixNano()); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM search WHERE partition=? AND ref=?", key, item.Ref.Key()); err != nil {
			return err
		}
		body := ""
		if i.indexBodies {
			body = item.Body
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO search(partition,ref,title,body) VALUES(?,?,?,?)", key, item.Ref.Key(), strings.Join([]string{item.Title, item.RawStatus, strings.Join(item.AssignedTo, " ")}, " "), body); err != nil {
			return err
		}
	}
	if complete {
		if _, err = tx.ExecContext(ctx, "INSERT INTO partitions(partition,observed,complete) VALUES(?,?,1) ON CONFLICT(partition) DO UPDATE SET observed=excluded.observed,complete=1", key, time.Now().UnixNano()); err != nil {
			return err
		}
		if i.indexBodies {
			if _, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO body_partitions(partition) VALUES(?)", key); err != nil {
				return err
			}
		} else if _, err = tx.ExecContext(ctx, "DELETE FROM body_partitions WHERE partition=?", key); err != nil {
			return err
		}
	} else {
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO partitions(partition,observed,complete) VALUES(?,0,0)", key); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM body_partitions WHERE partition=?", key); err != nil {
			return err
		}
	}
	cutoff := time.Now().Add(-Retention).UnixNano()
	if _, err = tx.ExecContext(ctx, "UPDATE partitions SET complete=0 WHERE partition IN (SELECT partition FROM entries WHERE observed < ?)", cutoff); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM search WHERE EXISTS (SELECT 1 FROM entries e WHERE e.partition=search.partition AND e.ref=search.ref AND e.observed < ?)`, cutoff); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM entries WHERE observed < ?", cutoff); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM search WHERE NOT EXISTS (SELECT 1 FROM entries e WHERE e.partition=search.partition AND e.ref=search.ref)`); err != nil {
		return err
	}
	return tx.Commit()
}

// GitHubSyncState stores the resumable state of a GitHub synchronization window.
type GitHubSyncState struct {
	Cursor                   string
	CommittedWatermark       time.Time
	ScanWatermark            time.Time
	ReconciliationCursor     string
	ReconciliationGeneration int64
	ReconciliationStarted    time.Time
}

// ReadGitHubSyncState reads synchronization metadata only. It never serves cached issue content.
type githubSyncIdentity interface {
	GitHubSyncIdentity(queue.Source) (principal, origin, repository, generation string, available bool)
}

// ForGitHubSync constructs a metadata-only partition. Unlike ForSource, this does not
// enable generic cached payload reads or offline cache seeding.
func ForGitHubSync(adapter any, source queue.Source) (Partition, bool) {
	identity, ok := adapter.(githubSyncIdentity)
	if !ok {
		return Partition{}, false
	}
	principal, origin, repository, generation, available := identity.GitHubSyncIdentity(source)
	if !available || principal == "" || origin == "" || repository == "" || generation == "" {
		return Partition{}, false
	}
	return Partition{Source: source.ID, Principal: principal, Scope: origin + "/" + repository, Generation: generation}, true
}

type GitHubSyncStore struct {
	Index    *Index
	Registry *queue.Registry
}

func (s GitHubSyncStore) LockGitHubSync(ctx context.Context, source queue.Source) (func(), error) {
	p, ok := s.partition(source)
	if !ok {
		return nil, errors.New("GitHub sync identity unavailable")
	}
	return s.Index.WaitRefreshLock(ctx, p)
}
func (s GitHubSyncStore) partition(source queue.Source) (Partition, bool) {
	adapter, ok := s.Registry.Get(source.Adapter)
	if !ok {
		return Partition{}, false
	}
	return ForGitHubSync(adapter, source)
}
func (s GitHubSyncStore) LoadGitHubSync(ctx context.Context, source queue.Source) (queue.SyncCheckpoint, error) {
	p, ok := s.partition(source)
	if !ok {
		return queue.SyncCheckpoint{}, errors.New("GitHub sync identity unavailable")
	}
	state, err := s.Index.ReadGitHubSyncState(ctx, p)
	return queue.SyncCheckpoint{Cursor: state.Cursor, CommittedWatermark: state.CommittedWatermark, ScanWatermark: state.ScanWatermark, ReconciliationCursor: state.ReconciliationCursor, ReconciliationGeneration: state.ReconciliationGeneration, ReconciliationStarted: state.ReconciliationStarted}, err
}
func (s GitHubSyncStore) StartGitHubReconciliation(ctx context.Context, source queue.Source, now time.Time) (queue.SyncCheckpoint, bool, error) {
	p, ok := s.partition(source)
	if !ok {
		return queue.SyncCheckpoint{}, false, errors.New("GitHub sync identity unavailable")
	}
	state, err := s.Index.StartGitHubReconciliation(ctx, p, now)
	return queue.SyncCheckpoint{Cursor: state.Cursor, CommittedWatermark: state.CommittedWatermark, ScanWatermark: state.ScanWatermark, ReconciliationCursor: state.ReconciliationCursor, ReconciliationGeneration: state.ReconciliationGeneration, ReconciliationStarted: state.ReconciliationStarted}, err == nil && state.ReconciliationCursor != "", err
}
func (s GitHubSyncStore) CommitGitHubReconciliationPage(ctx context.Context, source queue.Source, items []queue.Item, cursor string, state queue.SyncCheckpoint, complete bool) ([]queue.Ref, error) {
	p, ok := s.partition(source)
	if !ok {
		return nil, errors.New("GitHub sync identity unavailable")
	}
	return s.Index.CommitGitHubReconciliationPage(ctx, p, items, cursor, state.ReconciliationGeneration, state.ReconciliationStarted, complete)
}

// ReadGitHubSyncProjection restores earlier pages after a live reconciliation page has validated access.
func (s GitHubSyncStore) ReadGitHubSyncProjection(ctx context.Context, source queue.Source) ([]queue.Item, error) {
	p, ok := s.partition(source)
	if !ok {
		return nil, errors.New("GitHub sync identity unavailable")
	}
	projection, _, _, err := s.Index.Read(ctx, p, -1)
	if err != nil {
		return nil, err
	}
	items := make([]queue.Item, 0, len(projection.Items))
	for _, item := range projection.Items {
		items = append(items, item)
	}
	return items, nil
}

func (s GitHubSyncStore) RestartGitHubSync(ctx context.Context, source queue.Source, reconciliation bool) error {
	p, ok := s.partition(source)
	if !ok {
		return errors.New("GitHub sync identity unavailable")
	}
	return s.Index.RestartGitHubSync(ctx, p, reconciliation, time.Now().UTC())
}
func (s GitHubSyncStore) WithholdGitHubItem(ctx context.Context, source queue.Source, ref queue.Ref) error {
	p, ok := s.partition(source)
	if !ok {
		return errors.New("GitHub sync identity unavailable")
	}
	return s.Index.WithholdGitHubItems(ctx, p, []queue.Ref{ref})
}
func (s GitHubSyncStore) MarkGitHubInaccessible(ctx context.Context, source queue.Source) error {
	p, ok := s.partition(source)
	if !ok {
		return errors.New("GitHub sync identity unavailable")
	}
	return s.Index.MarkGitHubInaccessible(ctx, p)
}
func (s GitHubSyncStore) WithholdGitHubSource(ctx context.Context, source queue.Source) error {
	p, ok := s.partition(source)
	if !ok {
		return errors.New("GitHub sync identity unavailable")
	}
	return s.Index.WithholdGitHubItems(ctx, p, nil)
}

func (s GitHubSyncStore) CommitGitHubSyncPage(ctx context.Context, source queue.Source, items []queue.Item, cursor string, scanWatermark time.Time, complete bool) error {
	p, ok := s.partition(source)
	if !ok {
		return errors.New("GitHub sync identity unavailable")
	}
	return s.Index.CommitGitHubSyncPage(ctx, p, items, cursor, scanWatermark, complete)
}

func (i *Index) ReadGitHubSyncState(ctx context.Context, p Partition) (GitHubSyncState, error) {
	key, err := p.key()
	if err != nil {
		return GitHubSyncState{}, err
	}
	var state GitHubSyncState
	var committed, scan, started int64
	err = i.db.QueryRowContext(ctx, `SELECT cursor,committed_watermark,scan_watermark,reconciliation_cursor,reconciliation_generation,reconciliation_started FROM github_sync WHERE partition=?`, key).Scan(&state.Cursor, &committed, &scan, &state.ReconciliationCursor, &state.ReconciliationGeneration, &started)
	if err == sql.ErrNoRows {
		return state, nil
	}
	if err != nil {
		return GitHubSyncState{}, err
	}
	if committed != 0 {
		state.CommittedWatermark = time.Unix(0, committed)
	}
	if scan != 0 {
		state.ScanWatermark = time.Unix(0, scan)
	}
	if started != 0 {
		state.ReconciliationStarted = time.Unix(0, started)
	}
	return state, nil
}

// CommitGitHubSyncPage persists one page and its resume cursor atomically. The
// committed watermark changes only when complete is true.
func (i *Index) CommitGitHubSyncPage(ctx context.Context, p Partition, items []queue.Item, cursor string, scanWatermark time.Time, complete bool) error {
	key, err := p.key()
	if err != nil {
		return err
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range items {
		payload, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			return marshalErr
		}
		observed := item.Observation.ObservedAt
		if observed.IsZero() {
			observed = time.Now()
		}
		if item.CanonicalID != "" {
			var previousRef string
			lookupErr := tx.QueryRowContext(ctx, `SELECT ref FROM github_nodes WHERE partition=? AND node_id=?`, key, item.CanonicalID).Scan(&previousRef)
			if lookupErr != nil && lookupErr != sql.ErrNoRows {
				return lookupErr
			}
			if previousRef != "" && previousRef != item.Ref.Key() {
				if _, err = tx.ExecContext(ctx, `INSERT INTO github_absences(partition,ref,node_id,classification,observed) VALUES(?,?,?,'moved',?) ON CONFLICT(partition,ref) DO UPDATE SET node_id=excluded.node_id,classification=excluded.classification,observed=excluded.observed`, key, previousRef, item.CanonicalID, time.Now().UnixNano()); err != nil {
					return err
				}
				if _, err = tx.ExecContext(ctx, `DELETE FROM entries WHERE partition=? AND ref=?`, key, previousRef); err != nil {
					return err
				}
				if _, err = tx.ExecContext(ctx, `DELETE FROM search WHERE partition=? AND ref=?`, key, previousRef); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO github_nodes(partition,node_id,ref) VALUES(?,?,?) ON CONFLICT(partition,node_id) DO UPDATE SET ref=excluded.ref`, key, item.CanonicalID, item.Ref.Key()); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM github_absences WHERE partition=? AND ref=?`, key, item.Ref.Key()); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO entries(partition,ref,payload,observed) VALUES(?,?,?,?) ON CONFLICT(partition,ref) DO UPDATE SET payload=excluded.payload,observed=excluded.observed`, key, item.Ref.Key(), payload, observed.UnixNano()); err != nil {
			return err
		}
	}
	var previous int64
	queryErr := tx.QueryRowContext(ctx, `SELECT committed_watermark FROM github_sync WHERE partition=?`, key).Scan(&previous)
	if queryErr != nil && queryErr != sql.ErrNoRows {
		return queryErr
	}
	committed := previous
	if complete {
		committed = scanWatermark.UnixNano()
		cursor = ""
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO github_sync(partition,cursor,committed_watermark,scan_watermark,reconciliation_cursor,reconciliation_generation,reconciliation_started) VALUES(?,?,?,?, '',0,0) ON CONFLICT(partition) DO UPDATE SET cursor=excluded.cursor,committed_watermark=excluded.committed_watermark,scan_watermark=excluded.scan_watermark`, key, cursor, committed, scanWatermark.UnixNano())
	if err != nil {
		return err
	}
	return tx.Commit()
}

// StartGitHubReconciliation starts a daily bounded generation, or resumes an incomplete one.
func (i *Index) StartGitHubReconciliation(ctx context.Context, p Partition, now time.Time) (GitHubSyncState, error) {
	key, err := p.key()
	if err != nil {
		return GitHubSyncState{}, err
	}
	state, err := i.ReadGitHubSyncState(ctx, p)
	if err != nil {
		return GitHubSyncState{}, err
	}
	if state.ReconciliationCursor != "" {
		return state, nil
	}
	if !state.ReconciliationStarted.IsZero() && now.Sub(state.ReconciliationStarted) < 24*time.Hour {
		return state, nil
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return GitHubSyncState{}, err
	}
	defer tx.Rollback()
	var cursor string
	var committed, scan int64
	queryErr := tx.QueryRowContext(ctx, `SELECT cursor,committed_watermark,scan_watermark FROM github_sync WHERE partition=?`, key).Scan(&cursor, &committed, &scan)
	if queryErr != nil && queryErr != sql.ErrNoRows {
		return GitHubSyncState{}, queryErr
	}
	state.ReconciliationGeneration++
	state.ReconciliationStarted = now.UTC()
	state.ReconciliationCursor = "@start"
	if _, err = tx.ExecContext(ctx, `INSERT INTO github_sync(partition,cursor,committed_watermark,scan_watermark,reconciliation_cursor,reconciliation_generation,reconciliation_started) VALUES(?,?,?,?,?,?,?) ON CONFLICT(partition) DO UPDATE SET reconciliation_cursor=excluded.reconciliation_cursor,reconciliation_generation=excluded.reconciliation_generation,reconciliation_started=excluded.reconciliation_started`, key, cursor, committed, scan, state.ReconciliationCursor, state.ReconciliationGeneration, state.ReconciliationStarted.UnixNano()); err != nil {
		return GitHubSyncState{}, err
	}
	if err = tx.Commit(); err != nil {
		return GitHubSyncState{}, err
	}
	return state, nil
}

// GitHubAbsences returns non-sensitive identity recovery evidence. Only an
// observed ref change proves movement; missing nodes remain unknown rather
// than being reported as deleted or inaccessible without provider evidence.
func (i *Index) GitHubAbsences(ctx context.Context, p Partition) (map[queue.Ref]string, error) {
	key, err := p.key()
	if err != nil {
		return nil, err
	}
	rows, err := i.db.QueryContext(ctx, `SELECT ref,classification FROM github_absences WHERE partition=?`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[queue.Ref]string)
	for rows.Next() {
		var refKey, classification string
		if err := rows.Scan(&refKey, &classification); err != nil {
			return nil, err
		}
		parts := strings.SplitN(refKey, "\x00", 2)
		if len(parts) == 2 {
			result[queue.Ref{SourceID: parts[0], ItemID: parts[1]}] = classification
		}
	}
	return result, rows.Err()
}

// MarkGitHubInaccessible records a definitive principal access denial without
// mistaking an ambiguous 404 or scan absence for deletion.
func (i *Index) MarkGitHubInaccessible(ctx context.Context, p Partition) error {
	key, err := p.key()
	if err != nil {
		return err
	}
	_, err = i.db.ExecContext(ctx, `UPDATE github_absences SET classification='inaccessible',observed=? WHERE partition=? AND classification='unknown'`, time.Now().UnixNano(), key)
	return err
}

// WithholdGitHubItems removes stale payloads while retaining node identity/recovery mappings.
// A nil refs slice withholds every visible payload in the partition.
func (i *Index) WithholdGitHubItems(ctx context.Context, p Partition, refs []queue.Ref) error {
	key, err := p.key()
	if err != nil {
		return err
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(refs) == 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO github_absences(partition,ref,node_id,classification,observed) SELECT partition,ref,node_id,'unknown',? FROM github_nodes WHERE partition=? ON CONFLICT(partition,ref) DO UPDATE SET classification=CASE WHEN github_absences.classification='moved' THEN 'moved' ELSE 'unknown' END,observed=excluded.observed`, time.Now().UnixNano(), key); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM search WHERE partition=?`, key); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM entries WHERE partition=?`, key); err != nil {
			return err
		}
	} else {
		for _, ref := range refs {
			if _, err = tx.ExecContext(ctx, `INSERT INTO github_absences(partition,ref,node_id,classification,observed) SELECT partition,ref,node_id,'unknown',? FROM github_nodes WHERE partition=? AND ref=? ON CONFLICT(partition,ref) DO UPDATE SET classification=CASE WHEN github_absences.classification='moved' THEN 'moved' ELSE 'unknown' END,observed=excluded.observed`, time.Now().UnixNano(), key, ref.Key()); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM search WHERE partition=? AND ref=?`, key, ref.Key()); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM entries WHERE partition=? AND ref=?`, key, ref.Key()); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// RestartGitHubSync drops an expired resume cursor without advancing the committed watermark.
func (i *Index) RestartGitHubSync(ctx context.Context, p Partition, reconciliation bool, now time.Time) error {
	key, err := p.key()
	if err != nil {
		return err
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cursor string
	var committed, scan, gen, started int64
	var reconCursor string
	queryErr := tx.QueryRowContext(ctx, `SELECT cursor,committed_watermark,scan_watermark,reconciliation_cursor,reconciliation_generation,reconciliation_started FROM github_sync WHERE partition=?`, key).Scan(&cursor, &committed, &scan, &reconCursor, &gen, &started)
	if queryErr != nil && queryErr != sql.ErrNoRows {
		return queryErr
	}
	if reconciliation {
		gen++
		started = now.UnixNano()
		reconCursor = "@start"
	} else {
		cursor = ""
		scan = now.UnixNano()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO github_sync(partition,cursor,committed_watermark,scan_watermark,reconciliation_cursor,reconciliation_generation,reconciliation_started) VALUES(?,?,?,?,?,?,?) ON CONFLICT(partition) DO UPDATE SET cursor=excluded.cursor,scan_watermark=excluded.scan_watermark,reconciliation_cursor=excluded.reconciliation_cursor,reconciliation_generation=excluded.reconciliation_generation,reconciliation_started=excluded.reconciliation_started`, key, cursor, committed, scan, reconCursor, gen, started)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// CommitGitHubReconciliationPage atomically saves a bounded scan page and cursor.
// It retires absent payload rows only when the generation completes; identity mappings remain.
func (i *Index) CommitGitHubReconciliationPage(ctx context.Context, p Partition, items []queue.Item, cursor string, generation int64, started time.Time, complete bool) ([]queue.Ref, error) {
	key, err := p.key()
	if err != nil {
		return nil, err
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, item := range items {
		payload, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			return nil, marshalErr
		}
		observed := item.Observation.ObservedAt
		if observed.IsZero() {
			observed = time.Now()
		}
		if item.CanonicalID != "" {
			var previous string
			lookupErr := tx.QueryRowContext(ctx, `SELECT ref FROM github_nodes WHERE partition=? AND node_id=?`, key, item.CanonicalID).Scan(&previous)
			if lookupErr != nil && lookupErr != sql.ErrNoRows {
				return nil, lookupErr
			}
			if previous != "" && previous != item.Ref.Key() {
				if _, err = tx.ExecContext(ctx, `INSERT INTO github_absences(partition,ref,node_id,classification,observed) VALUES(?,?,?,'moved',?) ON CONFLICT(partition,ref) DO UPDATE SET node_id=excluded.node_id,classification=excluded.classification,observed=excluded.observed`, key, previous, item.CanonicalID, time.Now().UnixNano()); err != nil {
					return nil, err
				}
				if _, err = tx.ExecContext(ctx, `DELETE FROM entries WHERE partition=? AND ref=?`, key, previous); err != nil {
					return nil, err
				}
				if _, err = tx.ExecContext(ctx, `DELETE FROM search WHERE partition=? AND ref=?`, key, previous); err != nil {
					return nil, err
				}
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO github_nodes(partition,node_id,ref,seen_generation) VALUES(?,?,?,?) ON CONFLICT(partition,node_id) DO UPDATE SET ref=excluded.ref,seen_generation=excluded.seen_generation`, key, item.CanonicalID, item.Ref.Key(), generation); err != nil {
				return nil, err
			}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM github_absences WHERE partition=? AND ref=?`, key, item.Ref.Key()); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO entries(partition,ref,payload,observed) VALUES(?,?,?,?) ON CONFLICT(partition,ref) DO UPDATE SET payload=excluded.payload,observed=excluded.observed`, key, item.Ref.Key(), payload, observed.UnixNano()); err != nil {
			return nil, err
		}
	}
	var retired []queue.Ref
	if complete {
		// A missing node in an accessible scan is not evidence of deletion:
		// permission changes and transfers have the same observable result.
		if _, err = tx.ExecContext(ctx, `INSERT INTO github_absences(partition,ref,node_id,classification,observed) SELECT partition,ref,node_id,'unknown',? FROM github_nodes WHERE partition=? AND seen_generation<>? ON CONFLICT(partition,ref) DO UPDATE SET classification=CASE WHEN github_absences.classification='moved' THEN 'moved' ELSE 'unknown' END,observed=excluded.observed`, time.Now().UnixNano(), key, generation); err != nil {
			return nil, err
		}
		rows, queryErr := tx.QueryContext(ctx, `SELECT e.ref FROM entries e LEFT JOIN github_nodes n ON n.partition=e.partition AND n.ref=e.ref WHERE e.partition=? AND (n.node_id IS NULL OR n.seen_generation<>?)`, key, generation)
		if queryErr != nil {
			return nil, queryErr
		}
		for rows.Next() {
			var refKey string
			if err = rows.Scan(&refKey); err != nil {
				rows.Close()
				return nil, err
			}
			parts := strings.SplitN(refKey, "\x00", 2)
			if len(parts) == 2 {
				retired = append(retired, queue.Ref{SourceID: parts[0], ItemID: parts[1]})
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		if _, err = tx.ExecContext(ctx, `DELETE FROM entries WHERE partition=? AND ref IN (SELECT e.ref FROM entries e LEFT JOIN github_nodes n ON n.partition=e.partition AND n.ref=e.ref WHERE e.partition=? AND (n.node_id IS NULL OR n.seen_generation<>?))`, key, key, generation); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM search WHERE partition=? AND NOT EXISTS (SELECT 1 FROM entries e WHERE e.partition=search.partition AND e.ref=search.ref)`, key); err != nil {
			return nil, err
		}
		cursor = ""
	}
	var syncCursor string
	var committed, scan int64
	queryErr := tx.QueryRowContext(ctx, `SELECT cursor,committed_watermark,scan_watermark FROM github_sync WHERE partition=?`, key).Scan(&syncCursor, &committed, &scan)
	if queryErr != nil && queryErr != sql.ErrNoRows {
		return nil, queryErr
	}
	if complete && started.UnixNano() > committed {
		committed = started.UnixNano()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO github_sync(partition,cursor,committed_watermark,scan_watermark,reconciliation_cursor,reconciliation_generation,reconciliation_started) VALUES(?,?,?,?,?,?,?) ON CONFLICT(partition) DO UPDATE SET committed_watermark=excluded.committed_watermark,reconciliation_cursor=excluded.reconciliation_cursor,reconciliation_generation=excluded.reconciliation_generation,reconciliation_started=excluded.reconciliation_started`, key, syncCursor, committed, scan, cursor, generation, started.UnixNano())
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return retired, nil
}

// Revoke atomically purges every cached projection for a partition after access loss.
func (i *Index) Revoke(ctx context.Context, p Partition) error {
	key, err := p.key()
	if err != nil {
		return err
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"search", "entries", "partitions", "body_partitions"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE partition=?", key); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (i *Index) purgeExpired(ctx context.Context) error {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cutoff := time.Now().Add(-Retention).UnixNano()
	if _, err = tx.ExecContext(ctx, "UPDATE partitions SET complete=0 WHERE partition IN (SELECT partition FROM entries WHERE observed < ?)", cutoff); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM search WHERE EXISTS (SELECT 1 FROM entries e WHERE e.partition=search.partition AND e.ref=search.ref AND e.observed < ?)`, cutoff); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM entries WHERE observed < ?", cutoff); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM search WHERE NOT EXISTS (SELECT 1 FROM entries e WHERE e.partition=search.partition AND e.ref=search.ref)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (i *Index) Search(ctx context.Context, p Partition, text string) ([]string, string, error) {
	if err := i.purgeExpired(ctx); err != nil {
		return nil, "", err
	}
	key, err := p.key()
	if err != nil {
		return nil, "", err
	}
	column := "title"
	coverage := "indexed-summaries"
	if i.indexBodies {
		var indexed int
		err = i.db.QueryRowContext(ctx, "SELECT 1 FROM body_partitions WHERE partition=?", key).Scan(&indexed)
		if err != nil && err != sql.ErrNoRows {
			return nil, "", err
		}
		if err == nil {
			column = "search"
			coverage = "indexed-bodies"
		}
	}
	rows, err := i.db.QueryContext(ctx, "SELECT ref FROM search WHERE partition=? AND "+column+" MATCH ?", key, text)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var refs []string
	for rows.Next() {
		var ref string
		if err = rows.Scan(&ref); err != nil {
			return nil, "", err
		}
		refs = append(refs, ref)
	}
	return refs, coverage, rows.Err()
}
