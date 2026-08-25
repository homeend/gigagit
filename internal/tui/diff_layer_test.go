package tui

import "testing"

// Compile-time: *diffView must satisfy the layer interface.
var _ layer = (*diffView)(nil)

func TestDiffViewIsFullScreenLayer(t *testing.T) {
	t.Parallel()
	if !isFullScreenLayer(&diffView{}) {
		t.Fatal("a diffView must be a full-screen surface so it folds into a popup backdrop")
	}
}
