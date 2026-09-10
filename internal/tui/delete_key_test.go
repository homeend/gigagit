package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// The user's erase key sends ^[[3~ (tea.KeyDelete), not Backspace. These
// tests pin that Delete at the end of a field erases in the surfaces a user
// actually types into — a textfield-backed popup and the cursor-less
// type-to-filter queries — not only in the colour editor where it was first
// reported.

func TestCommitTitleDeleteAtEndErases(t *testing.T) {
	t.Parallel()
	m := commitGenTestModel(t)
	m = m.pushLayer(&commitPopup{})
	m = typeRunes(t, m, "fix")
	if got := layerOf[*commitPopup](m).title.Value(); got != "fix" {
		t.Fatalf("title = %q, want fix", got)
	}
	u, _ := m.Update(keyMsg("delete"))
	m = u.(Model)
	if got := layerOf[*commitPopup](m).title.Value(); got != "fi" {
		t.Fatalf("title after Delete at end = %q, want fi", got)
	}
	// Mid-field it is still a forward-delete.
	u, _ = m.Update(keyMsg("home"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("delete"))
	m = u.(Model)
	if got := layerOf[*commitPopup](m).title.Value(); got != "i" {
		t.Fatalf("title after Delete at home = %q, want i", got)
	}
}

func TestSlashFilterDeleteErases(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.focus = panelBranches
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	m = typeRunes(t, m, "xyz")
	u, _ = m.Update(keyMsg("delete"))
	m = u.(Model)
	if m.filterQuery != "xy" {
		t.Fatalf("query after Delete = %q, want xy", m.filterQuery)
	}
	for range 3 {
		u, _ = m.Update(keyMsg("delete"))
		m = u.(Model)
	}
	if m.filterQuery != "" || !m.filterTyping {
		t.Fatalf("Delete past empty: query=%q typing=%v", m.filterQuery, m.filterTyping)
	}
}

func TestBookmarkFilterDeleteErases(t *testing.T) {
	t.Parallel()
	m := bmPopupModel(
		model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "app.go"},
	)
	mm, _ := m.Update(keyMsg("/"))
	m = mm.(Model)
	m = typeRunes(t, m, "ap")
	mm, _ = m.Update(keyMsg("delete"))
	m = mm.(Model)
	if got := m.bookmarkSwitcher().filter; got != "a" {
		t.Fatalf("filter after Delete = %q, want a", got)
	}
}
