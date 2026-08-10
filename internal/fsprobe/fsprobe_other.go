//go:build !linux && !darwin && !windows

package fsprobe

// Foreign reports false on platforms without a probe implementation: no
// warning on unknown, never a spurious one.
func Foreign(string) bool { return false }
