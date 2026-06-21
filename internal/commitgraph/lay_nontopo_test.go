package commitgraph

import "testing"

// TestLayNonTopologicalDoesNotPanic feeds a sequence where a parent row appears
// BEFORE its child (what plain `git log` order can produce on skewed history,
// and what the graph pager uses while no commit-graph exists). Lay must not
// panic or index out of range — at worst it draws a stub/disconnected lane.
func TestLayNonTopologicalDoesNotPanic(t *testing.T) {
	commits := []Commit{
		{Hash: "p", Parents: []string{"g"}}, // parent listed before its child
		{Hash: "c", Parents: []string{"p"}}, // child references a row above it
		{Hash: "g", Parents: nil},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Lay panicked on non-topological input: %v", r)
		}
	}()
	rows, _ := Lay(commits)
	if len(rows) != len(commits) {
		t.Fatalf("Lay returned %d rows, want %d", len(rows), len(commits))
	}
}
