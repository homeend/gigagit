package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func TestStatusRowBadgeIsDisplayOnly(t *testing.T) {
	t.Parallel()
	l := statusList{
		files: []model.FileStatus{{Path: "a/b.go", Staged: '.', Unstaged: 'M'}},
		p:     panelFiles,
		mtime: map[int]int64{},
		notes: map[string]int{"a/b.go": 3},
	}
	row := l.Row(0)
	if !strings.Contains(row, "◆3") {
		t.Fatalf("row = %q, want a ◆3 badge", row)
	}
	// The filter haystack must NOT carry the badge: typing "3" must not match
	// a file because of its note count (the sanitize-DISPLAY-not-HAYSTACK rule).
	h, ok := any(l).(haystacker)
	if !ok {
		t.Fatal("statusList must implement haystacker once Row carries a badge")
	}
	if strings.Contains(h.Haystack(0), "◆") {
		t.Fatalf("haystack = %q, must be badge-free", h.Haystack(0))
	}
	// filterMatchFn must prefer the badge-free haystack over Row.
	if match := filterMatchFn(l, "3"); match(0) {
		t.Fatal("the / filter must not match a file on its note count")
	}
	if l.notes = nil; strings.Contains(l.Row(0), "◆") {
		t.Fatal("no counts ⇒ no badge (rows must be unchanged for repos without notes)")
	}
}

func TestCommitRowShowsNoteBadge(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.commits = []model.Commit{{Hash: "c0ffeeaa", Subject: "do a thing"}}
	m.commitListMode = true
	m.noteCounts = domain.NoteCounts{ByCommit: map[string]int{"c0ffeeaa": 2}}
	i := m.wipCount() // the first REAL commit's unified index
	row := m.commitIdentRowAt(i, m.commitIdentWidth(), false, -1)
	if !strings.Contains(row, "◆2") {
		t.Fatalf("commit row = %q, want a ◆2 badge", row)
	}
	// The Commits filter reads commitHaystackAt, which must stay badge-free.
	if strings.Contains(m.commitHaystackAt(i), "◆") {
		t.Fatalf("commit haystack = %q, must be badge-free", m.commitHaystackAt(i))
	}
	m.noteCounts = domain.NoteCounts{}
	if strings.Contains(m.commitIdentRowAt(i, m.commitIdentWidth(), false, -1), "◆") {
		t.Fatal("no counts ⇒ no badge")
	}
}

// A NoteCounts error leaves m.noteCounts at its zero value: nil maps, no badges,
// and no panic on lookup.
func TestZeroNoteCountsBadgeNothing(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.commits = []model.Commit{{Hash: "c0ffeeaa", Subject: "do a thing"}}
	m.commitListMode = true
	i := m.wipCount()
	if got := m.commitIdentRowAt(i, m.commitIdentWidth(), false, -1); strings.Contains(got, "◆") {
		t.Fatalf("commit row = %q, want no badge from a zero NoteCounts", got)
	}
	l := m.listFor(panelFiles)
	for n := 0; n < l.Len(); n++ {
		if strings.Contains(l.Row(n), "◆") {
			t.Fatalf("files row %d = %q, want no badge from a zero NoteCounts", n, l.Row(n))
		}
	}
}

// listFor must feed the Files/Staged lists the cached per-path counts, so a
// srcNotes refresh (which replaces m.noteCounts) shows up on the next render.
func TestListForCarriesNoteCounts(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.noteCounts = domain.NoteCounts{ByPath: map[string]int{"mod.txt": 4}}
	for _, p := range []panel{panelFiles, panelStaged} {
		l := m.listFor(p)
		var seen bool
		for i := 0; i < l.Len(); i++ {
			if strings.Contains(l.Row(i), "mod.txt") && strings.Contains(l.Row(i), "◆4") {
				seen = true
			}
		}
		if !seen {
			t.Fatalf("panel %v: mod.txt row carries no ◆4 badge", p)
		}
	}
}

func TestNoteBadgeFormat(t *testing.T) {
	t.Parallel()
	if got := noteBadge(0); got != "" {
		t.Fatalf("noteBadge(0) = %q, want empty", got)
	}
	if got := noteBadge(-1); got != "" {
		t.Fatalf("noteBadge(-1) = %q, want empty", got)
	}
	if got := noteBadge(12); got != "  ◆12" {
		t.Fatalf("noteBadge(12) = %q", got)
	}
}
