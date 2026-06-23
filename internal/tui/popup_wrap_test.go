package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/repos"
)

func popupHeight(s string) int { return len(strings.Split(ansi.Strip(s), "\n")) }

// assertSameHeight renders a popup with short vs long variable content (same row
// count) and asserts equal height — a body row that wraps onto a continuation
// line makes the long render taller. This catches the modalStyle-padding wrap
// bug without coupling to the exact chrome line count.
func assertSameHeight(t *testing.T, what, short, long string) {
	t.Helper()
	if hs, hl := popupHeight(short), popupHeight(long); hs != hl {
		t.Fatalf("%s: long content wrapped (short=%d, long=%d lines)\nLONG:\n%s", what, hs, hl, ansi.Strip(long))
	}
}

// assertOneLine asserts a and b appear together on one rendered line (i.e. the
// line they share did not wrap).
func assertOneLine(t *testing.T, rendered, a, b string) {
	t.Helper()
	for _, l := range strings.Split(ansi.Strip(rendered), "\n") {
		if strings.Contains(l, a) && strings.Contains(l, b) {
			return
		}
	}
	t.Errorf("%q and %q not on one line (wrapped?):\n%s", a, b, ansi.Strip(rendered))
}

func TestRepoPopupNoWrap(t *testing.T) {
	m := Model{width: 80, height: 30}
	render := func(path string) string {
		m = m.pushLayer(&repoPopup{entries: []repos.Entry{{Path: path, LastOpened: time.Now()}}, now: time.Now()})
		return layerOf[*repoPopup](m).box(m)
	}
	assertSameHeight(t, "repo body", render("/x"), render(strings.Repeat("z", 300)))
	// The hint wraps by whole tokens (like the bookmark/shelf switchers), never
	// mid-token: the first two stay grouped on one line.
	assertOneLine(t, render("/x"), "[enter] switch", "[ctrl+d] forget")
}

func TestConflictPopupNoWrap(t *testing.T) {
	m := Model{width: 80, height: 30}
	render := func(path string) string {
		files := []model.FileStatus{{Path: path, Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'}}
		return conflictListBox(m, files, 0, domain.ConflictState{}, "", 0, 0)
	}
	assertSameHeight(t, "conflict body", render("a.go"), render(strings.Repeat("z", 300)+".go"))
}

func TestPairOpPopupNoWrap(t *testing.T) {
	m := Model{width: 80, height: 30}
	render := func(marked string) string {
		m = m.pushLayer(&pairOpPopup{marked: marked, selected: "main", ops: pairOpsFor(panelBranches)})
		return layerOf[*pairOpPopup](m).box(m)
	}
	assertSameHeight(t, "pair-op body", render("feat/x"), render("feat/"+strings.Repeat("z", 300)))
}

func TestStashActionPopupNoWrap(t *testing.T) {
	m := Model{width: 80, height: 30}
	render := func(subject string) string {
		m2 := m.pushLayer(&stashActionPopup{ref: "stash@{0}", subject: subject})
		a := layerOf[*stashActionPopup](m2)
		return a.box(m2)
	}
	assertSameHeight(t, "stash subject", render("WIP"), render(strings.Repeat("z", 300)))
}
