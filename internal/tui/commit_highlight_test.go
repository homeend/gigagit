package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// highlightModel builds a Model with fully-controlled commits: digit-only hashes
// (so a query containing any non-hex letter can only match a subject, never a
// hash) and no refs. Ideal for testing the pure match/scan logic deterministically.
func highlightModel(subjects ...string) Model {
	cs := make([]model.Commit, len(subjects))
	for i, s := range subjects {
		cs[i] = model.Commit{Hash: fmt.Sprintf("%040d", i), Subject: s}
	}
	return Model{commits: cs, sel: map[panel]int{}}
}

func TestCommitMatchesHighlight(t *testing.T) {
	m := highlightModel("row-zero", "row-one", "row-two", "row-three", "row-four", "row-five")
	m.highlightQuery = "three"
	matches := 0
	for i := range m.commits {
		if m.commitMatchesHighlight(i) {
			matches++
		}
	}
	if matches != 1 || !m.commitMatchesHighlight(3) {
		t.Fatalf("query 'three' should match exactly row 3, got %d matches", matches)
	}
	// case-insensitive
	m.highlightQuery = "THREE"
	if !m.commitMatchesHighlight(3) {
		t.Fatal("match must be case-insensitive")
	}
	// empty query matches nothing
	m.highlightQuery = ""
	for i := range m.commits {
		if m.commitMatchesHighlight(i) {
			t.Fatalf("empty query must match nothing (row %d)", i)
		}
	}
}

func TestScanHighlightMatchWrap(t *testing.T) {
	m := highlightModel("row-zero", "row-one", "row-two", "row-three", "row-four", "row-five")
	last := len(m.commits) - 1
	m.highlightQuery = "row" // every row matches

	// exclusive forward from 0 -> 1
	if got, ok := m.scanHighlightMatch(0, +1, false); !ok || got != 1 {
		t.Fatalf("forward from 0 => (%d,%v), want (1,true)", got, ok)
	}
	// exclusive forward from last wraps to 0
	if got, ok := m.scanHighlightMatch(last, +1, false); !ok || got != 0 {
		t.Fatalf("forward wrap from %d => (%d,%v), want (0,true)", last, got, ok)
	}
	// exclusive backward from 0 wraps to last
	if got, ok := m.scanHighlightMatch(0, -1, false); !ok || got != last {
		t.Fatalf("backward wrap from 0 => (%d,%v), want (%d,true)", got, ok, last)
	}
	// inclusive: a matching `from` stays put
	if got, ok := m.scanHighlightMatch(2, +1, true); !ok || got != 2 {
		t.Fatalf("inclusive from matching 2 => (%d,%v), want (2,true)", got, ok)
	}

	// a query matching only row 5; exclusive forward from 0 lands on 5
	m.highlightQuery = "five"
	if got, ok := m.scanHighlightMatch(0, +1, false); !ok || got != last {
		t.Fatalf("'five' forward from 0 => (%d,%v), want (%d,true)", got, ok, last)
	}

	// no-match query returns (from,false)
	m.highlightQuery = "zzz"
	if got, ok := m.scanHighlightMatch(3, +1, false); ok || got != 3 {
		t.Fatalf("no-match => (%d,%v), want (3,false)", got, ok)
	}
}

// commitsModel loads a real linear repo and focuses the Commits panel.
func commitsModel(t *testing.T, n int) Model {
	m := loadedModelLinearCommits(t, n)
	m.focus = panelCommits
	m.sel[panelCommits] = 0
	return m
}

// relabel rewrites commit subjects with distinct non-hex words so a query can
// match exactly one subject without colliding with hex hashes or the "main"
// source label every row carries.
func relabel(m Model) Model {
	words := []string{"zulu", "yankee", "xray", "whiskey", "victor", "uniform", "tango", "sierra"}
	for i := range m.commits {
		m.commits[i].Subject = words[i%len(words)]
	}
	return m
}

func TestHighlightEntryClearsFilter(t *testing.T) {
	m := commitsModel(t, 6)
	m.filterPanel = panelCommits
	m.filterQuery = "c1"
	u, _ := m.Update(keyMsg("@"))
	mm := u.(Model)
	if !mm.highlightTyping {
		t.Fatal("@ must start highlight typing")
	}
	if mm.filterQuery != "" || mm.filterTyping {
		t.Fatalf("@ must clear the / filter, got query=%q typing=%v", mm.filterQuery, mm.filterTyping)
	}
}

func TestFilterEntryClearsHighlight(t *testing.T) {
	m := commitsModel(t, 6)
	m.highlightQuery = "c1"
	u, _ := m.Update(keyMsg("/"))
	mm := u.(Model)
	if !mm.filterTyping {
		t.Fatal("/ must start filter typing")
	}
	if mm.highlightQuery != "" || mm.highlightTyping {
		t.Fatalf("/ must clear the highlight, got query=%q typing=%v", mm.highlightQuery, mm.highlightTyping)
	}
}

func TestHighlightTypingSnapsCursorAndEnterKeeps(t *testing.T) {
	m := relabel(commitsModel(t, 6)) // commits[5].Subject == "uniform" (unique)
	u, _ := m.Update(keyMsg("@"))
	m = u.(Model)
	for _, r := range "uniform" {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	if m.highlightQuery != "uniform" {
		t.Fatalf("query=%q want uniform", m.highlightQuery)
	}
	if m.sel[panelCommits] != len(m.commits)-1 {
		t.Fatalf("cursor=%d want %d (snap to the unique match)", m.sel[panelCommits], len(m.commits)-1)
	}
	// enter keeps the query, ends typing
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.highlightTyping || m.highlightQuery != "uniform" {
		t.Fatalf("enter should keep query and stop typing, got typing=%v query=%q", m.highlightTyping, m.highlightQuery)
	}
	// esc clears
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.highlightActive() {
		t.Fatal("esc must clear highlight")
	}
}

func TestHighlightCtrlNavMovesBetweenMatches(t *testing.T) {
	m := commitsModel(t, 6)
	m.highlightQuery = "c" // every subject c0..c5 matches; committed (not typing)
	m.sel[panelCommits] = 0
	last := len(m.commits) - 1

	u, _ := m.Update(keyMsg("ctrl+down"))
	m = u.(Model)
	if m.sel[panelCommits] != 1 {
		t.Fatalf("ctrl+down => %d, want 1", m.sel[panelCommits])
	}
	u, _ = m.Update(keyMsg("ctrl+up"))
	m = u.(Model)
	if m.sel[panelCommits] != 0 {
		t.Fatalf("ctrl+up => %d, want 0", m.sel[panelCommits])
	}
	// ctrl+up wraps from 0 to last
	u, _ = m.Update(keyMsg("ctrl+up"))
	m = u.(Model)
	if m.sel[panelCommits] != last {
		t.Fatalf("ctrl+up wrap => %d, want %d", m.sel[panelCommits], last)
	}
}

// TestHighlightNavRespectsSortOrder guards the display-vs-backing index trap:
// m.sel is a DISPLAY position, but the match test indexes the backing feed. Under
// a non-default sort the two orders differ, so ctrl-nav must map through
// displayIndices or it lands on the wrong commit.
//
// Data is chosen so the orders genuinely diverge: subjects ascend by backing
// index ("b".."f", then the unique "zzz_target" at backing 5), so sortNameDesc
// puts the match at DISPLAY position 0 — different from its backing index 5. A
// backing-space scan would set sel to 5 and land on the wrong row; only a
// display-space scan lands on the target.
func TestHighlightNavRespectsSortOrder(t *testing.T) {
	m := commitsModel(t, 6)
	subjects := []string{"b", "c", "d", "e", "f", "zzz_target"} // backing 0..5
	for i := range m.commits {
		m.commits[i].Subject = subjects[i]
	}
	m.sortModes[panelCommits] = sortNameDesc
	m.highlightQuery = "zzz_target" // matches backing index 5 only
	m.sel[panelCommits] = 0

	u, _ := m.Update(keyMsg("ctrl+down"))
	m = u.(Model)

	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		t.Fatal("no backing index after ctrl+down")
	}
	if got := m.commits[bi].Subject; got != "zzz_target" {
		t.Fatalf("ctrl+down under sort landed on %q (backing %d), want zzz_target", got, bi)
	}
}

func TestHighlightFooterWhileTyping(t *testing.T) {
	m := commitsModel(t, 3)
	u, _ := m.Update(keyMsg("@"))
	m = u.(Model)
	if got := m.footerLine(); !strings.Contains(got, "highlight") {
		t.Fatalf("footer while @-typing = %q, want it to mention highlight", got)
	}
}
