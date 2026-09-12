// Package doctor provides read-only environment diagnostics.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

// Check is one stable, non-secret diagnostic result.
type Check struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

// Diagnose performs only metadata and read-only authority observations. It
// never reads a handle or database payload and never creates filesystem state.
func Diagnose(ctx context.Context, cfg config.Config, cwd string) []Check {
	checks := make([]Check, 0, 15)
	add := func(id, status, detail, hint string) {
		checks = append(checks, Check{ID: id, Status: status, Detail: output.RedactString(detail), Hint: output.RedactString(hint)})
	}

	sources := make([]string, 0, len(cfg.Sources))
	for key, source := range cfg.Sources {
		sources = append(sources, key+"="+source)
	}
	sort.Strings(sources)
	add("config.sources", "ok", "configuration path: "+cfg.ConfigPath+"; resolved sources: "+strings.Join(sources, ", "), "")

	homeInfo, homeErr := os.Lstat(cfg.Home)
	homeSafetyErr := store.ValidateHome(cfg.Home)
	homeOK := homeErr == nil && homeInfo.Mode().IsDir() && homeInfo.Mode()&os.ModeSymlink == 0 && ownedByCurrentUser(homeInfo) && homeSafetyErr == nil
	switch {
	case homeSafetyErr != nil:
		add("home.path", "fail", "authority home or its ancestry is unsafe", "choose a user-owned private directory with safe ancestors")
	case errors.Is(homeErr, os.ErrNotExist):
		add("home.path", "warn", "authority home is not present", "run worklease acquire to initialize local state")
	case homeErr != nil:
		add("home.path", "fail", "authority home cannot be inspected", "choose an accessible private --home")
	case !homeOK:
		add("home.path", "fail", "authority home is not a private directory", "choose a user-owned directory")
	default:
		add("home.path", "ok", "authority home is present", "")
	}
	if homeSafetyErr != nil || homeErr != nil || !homeOK {
		if errors.Is(homeErr, os.ErrNotExist) && homeSafetyErr == nil {
			add("home.permissions", "unknown", "authority home permissions cannot be checked until it exists", "")
		} else {
			add("home.permissions", "fail", "authority home ownership or directory safety is invalid", "use a user-owned directory with mode 0700")
		}
	} else if homeInfo.Mode().Perm()&0o077 != 0 {
		add("home.permissions", "fail", fmt.Sprintf("authority home mode is %04o", homeInfo.Mode().Perm()), "remove group and other permissions")
	} else {
		add("home.permissions", "ok", fmt.Sprintf("authority home mode is %04o", homeInfo.Mode().Perm()), "")
	}

	var st *store.Store
	dbPath := filepath.Join(cfg.Home, store.DatabaseFileName)
	dbInfo, dbErr := os.Lstat(dbPath)
	if errors.Is(dbErr, os.ErrNotExist) {
		add("db.open", "warn", "authority database is not present", "run worklease acquire to initialize local state")
		add("db.schema", "unknown", "schema cannot be checked without an authority database", "")
	} else if dbErr != nil {
		add("db.open", "fail", "authority database cannot be inspected", "check the database path and home permissions")
		add("db.schema", "unknown", "schema cannot be checked", "")
	} else if dbInfo.Mode()&os.ModeSymlink != 0 || !dbInfo.Mode().IsRegular() {
		add("db.open", "fail", "authority database is not a regular file", "replace the database only through a trusted authority home")
		add("db.schema", "unknown", "schema cannot be checked", "")
	} else {
		opened, err := store.Open(ctx, cfg.Home, store.Options{ReadOnly: true})
		if err != nil {
			if e := reason.As(err); e != nil && (e.Reason == reason.ReasonSchemaCorrupt || e.Reason == reason.ReasonSchemaUnsupported) {
				add("db.open", "ok", "authority database opened read-only", "")
				add("db.schema", "fail", "authority database schema is invalid", "inspect or replace the authority with an explicit migration")
			} else {
				add("db.open", "fail", "authority database could not be opened read-only", "check the authority home and database safety")
				add("db.schema", "unknown", "schema could not be checked", "")
			}
		} else {
			st = opened
			add("db.open", "ok", "authority database opened read-only", "")
			if opened.Empty() {
				add("db.schema", "unknown", "authority database has no readable schema", "")
			} else {
				add("db.schema", "ok", "authority schema is supported", "")
			}
		}
	}
	if st != nil {
		defer st.Close()
	}

	root, contextErr := handle.ContextRoot(cwd, nil)
	if contextErr != nil {
		add("context.root", "fail", "repository context cannot be resolved", "run from an accessible directory")
	} else {
		session := cfg.SessionID
		if session == "" {
			session = "<unscoped>"
		}
		add("context.root", "ok", "context root: "+root+"; session selector: "+session, "")
	}

	handlePath := os.Getenv("WORKLEASE_HANDLE")
	if handlePath == "" && contextErr == nil {
		handlePath = handle.ContextualPath(cfg.Home, root, cfg.SessionID)
	}
	if handlePath == "" {
		add("handle.present", "unknown", "contextual handle path cannot be resolved", "")
		add("handle.permissions", "unknown", "handle permissions cannot be checked", "")
	} else {
		present, err := handle.ValidateMetadata(filepath.Clean(handlePath))
		if errors.Is(err, os.ErrNotExist) {
			add("handle.present", "warn", "selected handle and parent are not present", "acquire a claim to create a private contextual handle")
			add("handle.permissions", "unknown", "handle permissions cannot be checked until its parent exists", "")
		} else if err != nil {
			add("handle.present", "warn", "selected handle is unavailable", "check the handle parent and ownership")
			add("handle.permissions", "fail", "selected handle or its parent is unsafe", "use a private regular parent and mode 0600 handle")
		} else if !present {
			add("handle.present", "warn", "selected handle is not present", "acquire a claim to create a private contextual handle")
			add("handle.permissions", "unknown", "handle permissions cannot be checked until it exists", "")
		} else {
			add("handle.present", "ok", "selected handle is present; contents are not inspected", "")
			add("handle.permissions", "ok", "selected handle is a private regular single-link file", "")
		}
	}

	if cfg.AgentID == "" {
		add("agent.identity", "fail", "agent identity is unavailable", "set WORKLEASE_AGENT_ID")
	} else {
		source := cfg.Sources["agent"]
		if source == "" {
			source = "default"
		}
		add("agent.identity", "ok", "agent identity is resolved from "+source, "")
	}
	if _, err := exec.LookPath("git"); err != nil {
		add("git.available", "warn", "git executable is unavailable", "install git for repository context discovery")
	} else {
		add("git.available", "ok", "git executable is available", "")
	}
	start := time.Now()
	if time.Since(start) >= 0 {
		add("clock.monotonic", "ok", "process monotonic clock is available", "")
	} else {
		add("clock.monotonic", "fail", "process monotonic clock moved backwards", "check the host clock")
	}
	if st == nil || st.Empty() {
		add("clock.authority", "unknown", "authority clock watermark is unavailable", "")
	} else if watermark, err := st.LastObservedAt(ctx); err != nil {
		add("clock.authority", "unknown", "authority clock watermark could not be read", "")
	} else if watermark.After(time.Now().UTC().Add(time.Second)) {
		add("clock.authority", "fail", "local clock is more than one second behind the authority watermark", "correct the host clock before mutating claims")
	} else {
		add("clock.authority", "ok", "local clock is not behind the authority watermark", "")
	}
	if st == nil || st.Empty() {
		add("authority.identity", "unknown", "authority identity is unavailable until state exists", "")
	} else {
		add("authority.identity", "ok", "authority identity: "+st.AuthorityID(), "")
	}
	add("mcp.available", "unknown", "MCP server is not implemented in this staged CLI; doctor cannot verify other hosts or provider-side fencing", "TASK-85.15 adds MCP support")

	legacy := []string{}
	if homeOK {
		entries, err := os.ReadDir(cfg.Home)
		if err == nil {
			for _, entry := range entries {
				name := entry.Name()
				if name == "leases.sqlite3" || name == "locks" || name == "context-leases" || name == "mcp-leases" {
					legacy = append(legacy, name)
				}
			}
		}
	}
	if len(legacy) > 0 {
		sort.Strings(legacy)
		add("state.python-era", "warn", "Python-era state remains: "+joinNames(legacy), "dispose of it only with the documented recoverable disposal procedure; Go never reads it")
	} else {
		add("state.python-era", "ok", "no Python-era state was detected", "")
	}
	return checks
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint32(stat.Uid) == uint32(os.Geteuid())
}

func joinNames(names []string) string {
	result := ""
	for i, name := range names {
		if i > 0 {
			result += ", "
		}
		result += name
	}
	return result
}
