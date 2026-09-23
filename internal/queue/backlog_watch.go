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

// WatchChanges uses filesystem events only as invalidation hints. Every event
// batch and periodic reconciliation rereads the provider's complete list; a
// lost watch never validates the old edge observations. Callers refresh their
// snapshot after notify. This is deliberately independent of task timestamps.
func (a *BacklogAdapter) WatchChanges(ctx context.Context, source Source, notify func()) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()
	backlogDir, err := BacklogDirectory(source.Locator)
	if err != nil {
		return BacklogDiagnostic{"watch-unavailable", "backlog directory unavailable"}
	}
	roots := []string{filepath.Join(backlogDir, "tasks"), filepath.Join(source.Locator, "docs", "backlog", "tasks")}
	watched := false
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return watcher.Add(path)
			}
			return nil
		}); err != nil {
			return err
		}
		watched = true
	}
	// An unavailable native watch still reconciles periodically and retries
	// registration after the caller restarts the watcher.
	if !watched {
		if err := watcher.Add(source.Locator); err != nil {
			return err
		}
	}
	for _, path := range []string{filepath.Join(source.Locator, ".git"), source.Locator} {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if err := watcher.Add(path); err != nil {
				return err
			}
		}
	}
	reconcile := time.NewTicker(time.Minute)
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
		case event, ok := <-watcher.Events:
			if !ok {
				refresh()
				return BacklogDiagnostic{"watch-lost", "filesystem watch closed"}
			}
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := watcher.Add(event.Name); err != nil {
						refresh()
						return BacklogDiagnostic{"watch-lost", "new task directory could not be watched"}
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
		case _, ok := <-watcher.Errors:
			refresh()
			if !ok {
				return BacklogDiagnostic{"watch-lost", "filesystem watch closed"}
			}
			return BacklogDiagnostic{"watch-lost", "filesystem watch overflow or failure"}
		case <-pending:
			pending = nil
			debounce = nil
			refresh()
		case <-reconcile.C:
			refresh()
		}
	}
}
