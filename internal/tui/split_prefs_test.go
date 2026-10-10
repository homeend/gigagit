package tui

import "testing"

// The split's stacked choice is read at startup and written on every flip,
// under its own key — flipping it never moves the diff view's.
func TestReviewStackedPrefRoundTrips(t *testing.T) {
	t.Parallel()
	m := tempPromptStore(t, newTestModel(t))
	if m.reviewStacked {
		t.Fatal("a fresh store reads false")
	}
	m = m.setReviewStackedPref(true)
	if !m.reviewStacked || !m.promptStore.StackedReviewFiles() || m.promptStore.StackedDiff() {
		t.Fatalf("flag %v store %v diff %v", m.reviewStacked, m.promptStore.StackedReviewFiles(), m.promptStore.StackedDiff())
	}
	m.reviewStacked = false
	if !m.loadPrefs().reviewStacked {
		t.Fatal("loadPrefs must read the stored choice")
	}
}
