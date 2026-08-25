package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func lazyModel() Model {
	m := Model{sel: map[panel]int{}, sortModes: map[panel]sortMode{}, focus: panelCommits, width: 120, height: 40}
	m.commits = []model.Commit{
		{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Subject: "first", Source: "main"},
		{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Subject: "second", Source: "feat"},
	}
	return m
}

// The lazy per-index methods must equal the old eager all-n slices entry-for-entry.
func TestLazyCommitListMatchesEagerSlices(t *testing.T) {
	t.Parallel()
	m := lazyModel()
	cl := m.listFor(panelCommits).(commitList)
	wantHay := m.commitHaystacks()
	wantFull := m.commitFullRows()
	wantText := m.commitTextReveals()
	for i := range m.commits {
		if got := cl.Haystack(i); got != wantHay[i] {
			t.Errorf("Haystack(%d) = %q, want %q", i, got, wantHay[i])
		}
		if got := cl.Full(i); got != wantFull[i] {
			t.Errorf("Full(%d) = %q, want %q", i, got, wantFull[i])
		}
		if got := cl.TextReveal(i); got != wantText[i] {
			t.Errorf("TextReveal(%d) = %q, want %q", i, got, wantText[i])
		}
	}
}
