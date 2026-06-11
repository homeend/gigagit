package tui

import "testing"

// We can't drive a PTY here, but we can assert the model constructs and its
// loading View is nil-safe (no panic) even with a nil repo.
func TestNewModelLoadingViewIsNilSafe(t *testing.T) {
	m := New(nil)
	_ = m.View() // loading state returns early, must not touch repo
}
