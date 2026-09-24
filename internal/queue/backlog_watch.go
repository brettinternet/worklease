package queue

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type backlogWatcher interface {
	Add(string) error
	Close() error
	Events() <-chan fsnotify.Event
	Errors() <-chan error
}

type nativeBacklogWatcher struct{ watcher *fsnotify.Watcher }

func newNativeBacklogWatcher() (backlogWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return nativeBacklogWatcher{watcher: watcher}, nil
}
func (w nativeBacklogWatcher) Add(path string) error { return w.watcher.Add(path) }
func (w nativeBacklogWatcher) Close() error          { return w.watcher.Close() }
func (w nativeBacklogWatcher) Events() <-chan fsnotify.Event {
	return w.watcher.Events
}
func (w nativeBacklogWatcher) Errors() <-chan error { return w.watcher.Errors }

type backlogWatchTicker interface {
	C() <-chan time.Time
	Stop()
}

type nativeBacklogWatchTicker struct{ ticker *time.Ticker }

func newNativeBacklogWatchTicker(interval time.Duration) backlogWatchTicker {
	return nativeBacklogWatchTicker{ticker: time.NewTicker(interval)}
}
func (t nativeBacklogWatchTicker) C() <-chan time.Time { return t.ticker.C }
func (t nativeBacklogWatchTicker) Stop()               { t.ticker.Stop() }

func nearestExistingWatchDirectory(path string) (string, error) {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		if info, err := os.Stat(current); err == nil && info.IsDir() {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", BacklogDiagnostic{"watch-unavailable", "no existing task ancestor can be watched"}
		}
	}
}

func watchPathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func watchPathRelated(root, path string) bool {
	return watchPathWithin(root, path) || watchPathWithin(path, root)
}

// WatchChanges uses filesystem events only as invalidation hints. Every event
// batch and periodic reconciliation rereads the provider's complete list; a
// lost watch never validates the old edge observations. Callers refresh their
// snapshot after notify. This is deliberately independent of task timestamps.
func (a *BacklogAdapter) WatchChanges(ctx context.Context, source Source, notify func()) error {
	watcher, watcherErr := a.watcherFactory()
	if watcher != nil {
		defer watcher.Close()
	}
	var events <-chan fsnotify.Event
	var watcherErrors <-chan error
	if watcher != nil {
		events = watcher.Events()
		watcherErrors = watcher.Errors()
	}
	backlogDir, err := BacklogDirectory(source.Locator)
	if err != nil {
		return BacklogDiagnostic{"watch-unavailable", "backlog directory unavailable"}
	}
	roots := []string{filepath.Join(backlogDir, "tasks"), filepath.Join(source.Locator, "docs", "backlog", "tasks")}
	registrationFailed := watcherErr != nil || watcher == nil
	watched := map[string]bool{}
	addDirectory := func(path string) error {
		path = filepath.Clean(path)
		if watched[path] {
			return nil
		}
		watched[path] = true
		if watcher == nil {
			registrationFailed = true
			return nil
		}
		if err := watcher.Add(path); err != nil {
			registrationFailed = true
			return err
		}
		return nil
	}
	watchTree := func(root string) error {
		return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return addDirectory(path)
			}
			return nil
		})
	}
	for _, root := range roots {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			if err := watchTree(root); err != nil {
				registrationFailed = true
			}
			continue
		}
		ancestor, err := nearestExistingWatchDirectory(root)
		if err != nil {
			registrationFailed = true
			continue
		}
		if err := addDirectory(ancestor); err != nil {
			registrationFailed = true
		}
	}
	for _, path := range []string{filepath.Join(source.Locator, ".git"), source.Locator} {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if err := addDirectory(path); err != nil {
				registrationFailed = true
			}
		}
	}
	if registrationFailed {
		a.InvalidateEdges(source)
	}
	interval := a.reconcileInterval
	if interval <= 0 {
		interval = time.Minute
	}
	reconcile := a.tickerFactory(interval)
	defer reconcile.Stop()
	var debounce *time.Timer
	var pending <-chan time.Time
	defer func() {
		if debounce != nil {
			debounce.Stop()
		}
	}()
	// The re-list is authoritative even if the event carries no useful path:
	// same-minute edits and prerequisite changes must not retain stale edges.
	refresh := func() {
		a.InvalidateEdges(source)
		if notify != nil {
			notify() // caller re-lists through its normal single-flight refresh
		} else {
			_, _ = a.List(ctx, source, Query{}, "")
		}
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-events:
			if !ok {
				refresh()
				return BacklogDiagnostic{"watch-lost", "filesystem watch closed"}
			}
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					for _, root := range roots {
						if watchPathRelated(root, event.Name) {
							if err := watchTree(event.Name); err != nil {
								refresh()
								return BacklogDiagnostic{"watch-lost", "new task directory could not be watched"}
							}
							break
						}
					}
				}
			}
			if strings.HasSuffix(event.Name, ".md") || strings.HasSuffix(event.Name, ".yml") || filepath.Base(event.Name) == "HEAD" || event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				a.InvalidateEdges(source)
				if debounce == nil {
					debounce = time.NewTimer(150 * time.Millisecond)
					pending = debounce.C
				} else {
					if !debounce.Stop() {
						select {
						case <-debounce.C:
						default:
						}
					}
					debounce.Reset(150 * time.Millisecond)
				}
			}
		case _, ok := <-watcherErrors:
			refresh()
			if !ok {
				return BacklogDiagnostic{"watch-lost", "filesystem watch closed"}
			}
			return BacklogDiagnostic{"watch-lost", "filesystem watch overflow or failure"}
		case <-pending:
			pending = nil
			debounce = nil
			refresh()
		case <-reconcile.C():
			refresh()
		}
	}
}
