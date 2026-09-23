//go:build !darwin && !linux

package queue

import "os"

// Persistent cache is unavailable without a stable checkout-instance identity.
func checkoutInstance(os.FileInfo) (string, bool) { return "", false }
