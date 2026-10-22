package doctor

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/output"
)

// LegacyArtifacts reports obsolete 1.9.x files by metadata only. Their contents,
// permissions, and validity never affect claim commands or diagnostic success.
func LegacyArtifacts(home, cwd string, env func(string) string) []Check {
	root := func(key, fallback string) string {
		if value := strings.TrimSpace(env(key)); value != "" {
			return value
		}
		return filepath.Join(env("HOME"), fallback)
	}
	paths := []string{
		filepath.Join(root("XDG_CONFIG_HOME", ".config"), "worklease", "queue.yaml"),
		filepath.Join(root("XDG_CACHE_HOME", ".cache"), "worklease", "queue"),
		filepath.Join(root("XDG_STATE_HOME", ".local/state"), "worklease", "queue-recovery"),
		filepath.Join(home, "queue-handles"),
	}
	if checkout, repository, err := handle.BindingRoots(cwd, nil); err == nil {
		for _, dir := range []string{checkout, repository} {
			if dir != "" {
				paths = append(paths, filepath.Join(dir, ".config", "worklease", "queue-sources.yaml"))
			}
		}
	}
	var checks []Check
	seen := make(map[string]bool)
	for _, path := range paths {
		if !filepath.IsAbs(path) || seen[path] {
			continue
		}
		seen[path] = true
		if _, err := os.Lstat(path); err == nil {
			checks = append(checks, Check{ID: "legacy.artifact", Status: "warn", Detail: output.RedactString("obsolete 1.9.x artifact: " + path), Hint: "No longer used by Worklease; safe to delete."})
		}
	}
	return checks
}
