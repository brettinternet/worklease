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

const SchemaGeneration = 2
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
	if version != 0 && version != SchemaGeneration {
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
		if _, err := conn.ExecContext(ctx, `CREATE TABLE entries (partition TEXT NOT NULL, ref TEXT NOT NULL, payload BLOB NOT NULL, observed INTEGER NOT NULL, PRIMARY KEY(partition,ref)); CREATE TABLE partitions (partition TEXT PRIMARY KEY, observed INTEGER NOT NULL, complete INTEGER NOT NULL); CREATE VIRTUAL TABLE search USING fts5(partition UNINDEXED,ref UNINDEXED,title,body); PRAGMA user_version=2`); err != nil {
			return err
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
