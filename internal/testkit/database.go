package testkit

import "path/filepath"

// DatabasePath returns the Go authority database path for an isolated home.
// Keeping this small path helper in testkit prevents tests from reproducing
// state-layout literals while avoiding any dependency on the store package.
func DatabasePath(home string) string { return filepath.Join(home, "worklease.db") }

// DatabaseSidecarPaths returns the SQLite WAL and SHM paths in stable order.
func DatabaseSidecarPaths(home string) []string {
	path := DatabasePath(home)
	return []string{path + "-wal", path + "-shm"}
}
