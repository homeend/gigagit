//go:build !windows

package prcache

// sharingViolation is Windows-only: elsewhere a rename never fails on a reader.
func sharingViolation(error) bool { return false }
