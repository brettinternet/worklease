package store

// trustedSystemSymlink reports false: Windows has no root-owned compatibility
// links among ordinary home ancestors.
func trustedSystemSymlink(string) bool { return false }
