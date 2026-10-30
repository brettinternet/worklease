//go:build !windows

package testkit

import "os"

const windows = false

func chmod(path string, mode os.FileMode) error { return os.Chmod(path, mode) }
