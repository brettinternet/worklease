//go:build !darwin && !linux

package queue

// Persistent cache is unavailable without a stable checkout-instance identity.
func checkoutInstance(string) (string, bool) { return "", false }
