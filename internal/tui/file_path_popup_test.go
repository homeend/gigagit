package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// lsReady delivers the popup's tracked-file list, as if LsFiles returned.
func lsReady(t *testing.T, m Model, paths ...string) Model {
	t.Helper()
	nm, _ := send(m, filePathLsMsg{paths: paths})
	return nm
}

func TestRepoRelPath(t *testing.T) {
	root := filepath.FromSlash("/repo")
	outside := filepath.FromSlash("/elsewhere/x.go")
	cases := []struct{ name, in, want string }{
		{"already relative", "internal/tui/model.go", "internal/tui/model.go"},
		{"dot-slash relative", "./internal/x.go", "internal/x.go"},
		{"absolute inside repo", filepath.FromSlash("/repo/internal/x.go"), "internal/x.go"},
		{"absolute outside repo", outside, filepath.ToSlash(filepath.Clean(outside))},
		{"blank", "   ", ""},
	}
	for _, c := range cases {
		if got := repoRelPath(root, c.in); got != c.want {
			t.Errorf("%s: repoRelPath(%q,%q)=%q want %q", c.name, root, c.in, got, c.want)
		}
	}
}

// palettePick opens the palette, navigates to the command labelled label, and
// presses enter. Reused by every palette entry's test.
func palettePick(t *testing.T, m Model, label string) (Model, tea.Cmd) {
	t.Helper()
	m, _ = send(m, keyType(tea.KeyCtrlP))
	p := layerOf[*commandPalette](m)
	if p == nil {
		t.Fatal("ctrl+p did not open the palette")
	}
	idx := -1
	for i, c := range p.cmds {
		if c.label == label {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("palette has no command %q", label)
	}
	for j := 0; j < idx; j++ {
		m, _ = send(m, keyType(tea.KeyDown))
	}
	return send(m, keyType(tea.KeyEnter))
}

func TestPaletteFileHistoryOpensPopup(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File history")
	p := layerOf[*filePathPopup](m)
	if p == nil || p.kind != filePathHistory {
		t.Fatal("File history should open a history file-path popup")
	}
	if layerOf[*commandPalette](m) == nil {
		t.Fatal("the palette should stay underneath as the source")
	}
}

func TestFilePathPopupHistoryOpensSurface(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File history")
	m = lsReady(t, m, "README.md")
	m = typeRunes(t, m, "README.md")
	m, _ = send(m, keyType(tea.KeyEnter))
	if layerOf[*filePathPopup](m) != nil || layerOf[*commandPalette](m) != nil {
		t.Fatal("submit must unwind both the popup and the palette")
	}
	hv := layerOf[*historyView](m)
	if hv == nil {
		t.Fatal("submit should open the history surface")
	}
	if hv.ctx.path != "README.md" || hv.ctx.rev != "" {
		t.Errorf("navContext = %+v, want {path:README.md rev:}", hv.ctx)
	}
}

func TestFilePathPopupBlameOpensSurface(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File blame")
	m = lsReady(t, m, "README.md")
	m = typeRunes(t, m, "README.md")
	m, _ = send(m, keyType(tea.KeyEnter))
	bv := layerOf[*blameView](m)
	if bv == nil {
		t.Fatal("submit should open the blame surface")
	}
	if bv.ctx.path != "README.md" || bv.ctx.rev != "" {
		t.Errorf("navContext = %+v, want {path:README.md rev:}", bv.ctx)
	}
}

func TestFilePathPopupEmptyKeepsOpen(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m, _ = send(m, keyType(tea.KeyEnter)) // empty input
	if layerOf[*filePathPopup](m) == nil {
		t.Fatal("enter with an empty path must keep the popup open")
	}
}

func TestFilePathPopupEscRevealsPalette(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File history")
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*filePathPopup](m) != nil {
		t.Fatal("esc should close the file-path popup")
	}
	if p := layerOf[*commandPalette](m); p == nil || p != m.topLayer() {
		t.Fatal("esc should reveal the palette beneath")
	}
}

func TestFilePathPopupAllowsSpaces(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m, _ = send(m, key("a"))
	m, _ = send(m, keyType(tea.KeySpace))
	m, _ = send(m, key("b"))
	p := layerOf[*filePathPopup](m)
	if p == nil || p.input.Value() != "a b" {
		t.Fatalf("path popup must accept spaces; input=%q", p.input.Value())
	}
}

// A space typed into a path survives normalization all the way into the opened
// history surface's navContext — proving the space is preserved end-to-end, not
// just held in the textfield buffer.
func TestFilePathPopupSpaceReachesNavContext(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "a b.txt")
	m = typeRunes(t, m, "a b.txt")
	m, _ = send(m, keyType(tea.KeyEnter))
	hv := layerOf[*historyView](m)
	if hv == nil {
		t.Fatal("submit should open the history surface")
	}
	if hv.ctx.path != "a b.txt" {
		t.Errorf("navContext.path = %q, want %q (space must survive normalization)", hv.ctx.path, "a b.txt")
	}
}

// The render path draws the popup box for both kinds, exercising p.title(),
// p.box(), and p.render() (guards the green-unit/broken-render class the way
// gotoCommitPopup's TestGotoCommitRendersWithError does).
func TestFilePathPopupRenders(t *testing.T) {
	cases := []struct {
		label string
		title string
	}{
		{"File history", "File history"},
		{"File blame", "File blame"},
	}
	for _, c := range cases {
		m := gotoModel(t, gotoFullHash)
		m, _ = palettePick(t, m, c.label)
		out := m.View()
		if !strings.Contains(out, c.title) {
			t.Errorf("%s render missing title %q", c.label, c.title)
		}
		if !strings.Contains(out, "[enter] show") {
			t.Errorf("%s render missing the %q hint", c.label, "[enter] show")
		}
	}
}

func TestPaletteFindOpensFinder(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Find")
	if layerOf[*fileFinderPopup](m) == nil {
		t.Fatal("Find should open the fuzzy file finder")
	}
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("Find replaces the palette (it does not stay beneath)")
	}
}

func TestPaletteGitConfigOpensExplorer(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Git config explorer")
	if layerOf[*gitConfigPopup](m) == nil {
		t.Fatal("Git config explorer should open the explorer popup")
	}
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("it replaces the palette")
	}
}

func TestPaletteAgentSkillsOpensPickerDirect(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Set up agent skills (using-gg)")
	sp := layerOf[*settingsPopup](m)
	if sp == nil || !sp.picker || !sp.pickerFromPalette {
		t.Fatal("agent skills should open Settings pre-set to the palette-launched picker")
	}
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("it replaces the palette")
	}
}

func TestPaletteAgentSkillsEscReturnsToBase(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Set up agent skills (using-gg)")
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*settingsPopup](m) != nil {
		t.Fatal("esc from a palette-launched picker must close Settings entirely (return to base)")
	}
}

func TestFilePathPopupStartsLsFilesLoad(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, cmd := m.openFilePathPopup(filePathHistory)
	p := layerOf[*filePathPopup](m)
	if p == nil || !p.loading {
		t.Fatal("opening the popup must mark it loading")
	}
	if cmd == nil {
		t.Fatal("opening the popup must start the LsFiles load")
	}
}

func TestFilePathPopupLsDeliveryBuildsSet(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m, _ = send(m, filePathLsMsg{paths: []string{"a/b.go", "README.md"}})
	p := layerOf[*filePathPopup](m)
	if p.loading {
		t.Fatal("delivery must clear loading")
	}
	if _, ok := p.set["README.md"]; !ok || len(p.all) != 2 {
		t.Fatalf("delivery must fill all+set; all=%v", p.all)
	}
}

func TestFilePathPopupLsErrorKeepsPopup(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m, _ = send(m, filePathLsMsg{err: errors.New("boom")})
	p := layerOf[*filePathPopup](m)
	if p == nil || p.loadErr == nil || p.loading {
		t.Fatal("an LsFiles error must be recorded and the popup kept open")
	}
}

func TestFilePathPopupLsLateDeliveryIsNoop(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = send(m, filePathLsMsg{paths: []string{"a"}}) // no popup open — must not panic
	_ = m
}

func TestFilePathPopupNonMatchEntersSuggestionMode(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "internal/tui/model.go", "internal/tui/view.go", "README.md")
	m = typeRunes(t, m, "model.go")
	m, _ = send(m, keyType(tea.KeyEnter))
	p := layerOf[*filePathPopup](m)
	if p == nil || !p.suggesting {
		t.Fatal("a non-tracked path must switch the popup to suggestion mode")
	}
	if p.sel != 0 {
		t.Fatal("suggestion mode must start on the open-as-typed row")
	}
	if len(p.matches) == 0 || p.matches[0].S != "internal/tui/model.go" {
		t.Fatalf("matches must rank tracked files; got %+v", p.matches)
	}
}

func TestFilePathPopupSuggestionEnterOpensHistory(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File history")
	m = lsReady(t, m, "internal/tui/model.go", "README.md")
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter)) // → suggestion mode
	m, _ = send(m, keyType(tea.KeyDown))  // sel=1: first match
	m, _ = send(m, keyType(tea.KeyEnter))
	hv := layerOf[*historyView](m)
	if hv == nil || hv.ctx.path != "internal/tui/model.go" {
		t.Fatalf("enter on a suggestion must open history for it; hv=%+v", hv)
	}
	if layerOf[*filePathPopup](m) != nil || layerOf[*commandPalette](m) != nil {
		t.Fatal("opening must unwind the popup and the palette beneath")
	}
}

func TestFilePathPopupSuggestionEnterOpensBlame(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathBlame)
	m = lsReady(t, m, "internal/tui/model.go")
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter))
	m, _ = send(m, keyType(tea.KeyDown))
	m, _ = send(m, keyType(tea.KeyEnter))
	bv := layerOf[*blameView](m)
	if bv == nil || bv.ctx.path != "internal/tui/model.go" {
		t.Fatalf("enter on a suggestion must open blame for it; bv=%+v", bv)
	}
}

func TestFilePathPopupEscapeRowOpensAsTyped(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "internal/tui/model.go")
	m = typeRunes(t, m, "deleted/file.go")
	m, _ = send(m, keyType(tea.KeyEnter)) // suggestion mode; sel=0 = escape row
	m, _ = send(m, keyType(tea.KeyEnter))
	hv := layerOf[*historyView](m)
	if hv == nil || hv.ctx.path != "deleted/file.go" {
		t.Fatalf("the escape row must open the typed path; hv=%+v", hv)
	}
}

func TestFilePathPopupSuggestionTypingReranks(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "aaa/x.go", "bbb/y.go")
	m = typeRunes(t, m, "zzz")
	m, _ = send(m, keyType(tea.KeyEnter))
	p := layerOf[*filePathPopup](m)
	if !p.suggesting || len(p.matches) != 0 {
		t.Fatalf("zzz matches nothing; got %+v", p.matches)
	}
	for range 3 {
		m, _ = send(m, keyType(tea.KeyBackspace))
	}
	m = typeRunes(t, m, "aaa")
	if p = layerOf[*filePathPopup](m); len(p.matches) != 1 || p.matches[0].S != "aaa/x.go" {
		t.Fatalf("typing must re-rank live; got %+v", p.matches)
	}
	if p.sel != 0 {
		t.Fatal("editing the query must reset the cursor to the escape row")
	}
}

func TestFilePathPopupSuggestionNavClamps(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "aaa/x.go", "aab/y.go")
	m = typeRunes(t, m, "aa")
	m, _ = send(m, keyType(tea.KeyEnter))
	p := layerOf[*filePathPopup](m)
	m, _ = send(m, keyType(tea.KeyUp)) // above escape row: clamp
	if p.sel != 0 {
		t.Fatal("up on row 0 must clamp")
	}
	m, _ = send(m, keyType(tea.KeyPgDown)) // far past end: clamp
	if p.sel != len(p.matches) {
		t.Fatalf("pgdown must clamp to the last row; sel=%d", p.sel)
	}
}

func TestFilePathPopupSuggestionEscReturnsToInput(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "aaa/x.go")
	m = typeRunes(t, m, "zzz")
	m, _ = send(m, keyType(tea.KeyEnter))
	m, _ = send(m, keyType(tea.KeyEsc))
	p := layerOf[*filePathPopup](m)
	if p == nil || p.suggesting {
		t.Fatal("esc must drop back to plain input, popup kept")
	}
	if p.input.Value() != "zzz" {
		t.Fatalf("esc must preserve the input; got %q", p.input.Value())
	}
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*filePathPopup](m) != nil {
		t.Fatal("second esc must close the popup")
	}
}

func TestFilePathPopupLoadErrorOpensAsTyped(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m, _ = send(m, filePathLsMsg{err: errors.New("boom")})
	m = typeRunes(t, m, "some/file.go")
	m, _ = send(m, keyType(tea.KeyEnter))
	hv := layerOf[*historyView](m)
	if hv == nil || hv.ctx.path != "some/file.go" {
		t.Fatalf("LsFiles failure must fall through to open-as-typed; hv=%+v", hv)
	}
}

func TestFilePathPopupEnterWhileLoadingThenDelivery(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter)) // list not loaded yet
	p := layerOf[*filePathPopup](m)
	if p == nil || !p.suggesting || !p.loading {
		t.Fatal("enter while loading must enter suggestion mode in the loading state")
	}
	m, _ = send(m, filePathLsMsg{paths: []string{"internal/tui/model.go"}})
	if len(p.matches) != 1 || p.matches[0].S != "internal/tui/model.go" {
		t.Fatalf("delivery while suggesting must fill the list; got %+v", p.matches)
	}
}

func TestFilePathPopupEnterWhileLoadingThenError(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter))
	m, _ = send(m, filePathLsMsg{err: errors.New("boom")})
	p := layerOf[*filePathPopup](m)
	if p == nil || !p.suggesting || p.loading {
		t.Fatal("a late error must land in suggestion mode with loading cleared")
	}
	// The always-present escape row keeps this from being a dead end.
	m, _ = send(m, keyType(tea.KeyEnter))
	hv := layerOf[*historyView](m)
	if hv == nil || hv.ctx.path != "model" {
		t.Fatalf("escape row must still open as typed after a load error; hv=%+v", hv)
	}
}

func TestFilePathPopupRendersSuggestions(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "internal/tui/model.go")
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter))
	p := layerOf[*filePathPopup](m)
	out := p.box(m)
	if !strings.Contains(out, "open as typed: model") {
		t.Fatalf("box must render the escape row; out=%s", out)
	}
	if !strings.Contains(out, "internal/tui/model.go") {
		t.Fatalf("box must render the fuzzy matches; out=%s", out)
	}
	if !strings.Contains(out, "[esc] back") {
		t.Fatalf("box must render the suggestion-mode hint; out=%s", out)
	}
}

// The maximized box() must resolve its width the same way the outer frame
// does (popupResolveWidth first, text width derived from THAT) — otherwise
// ctrl+t grows the frame but leaves rows/input truncated at the unmaximized
// ~52-column text width.
func TestFilePathPopupMaximizeRendersLongPathUntruncated(t *testing.T) {
	m := gotoModel(t, gotoFullHash) // width 120
	m, _ = m.openFilePathPopup(filePathHistory)
	long := "internal/tui/" + strings.Repeat("nested/", 6) + "very_long_file_name.go"
	if len(long) <= 60 {
		t.Fatalf("test path too short: %d cols", len(long))
	}
	m = lsReady(t, m, long)
	m = typeRunes(t, m, "long")
	m, _ = send(m, keyType(tea.KeyEnter))
	p := layerOf[*filePathPopup](m)
	if p == nil || !p.suggesting || len(p.matches) == 0 {
		t.Fatalf("setup: expected a suggestion match; matches=%+v", p.matches)
	}
	if unmax := p.box(m); strings.Contains(unmax, long) {
		t.Fatalf("sanity: unmaximized box should truncate the long path; out=%s", unmax)
	}
	p.maximized = true
	if maxed := p.box(m); !strings.Contains(maxed, long) {
		t.Fatalf("maximized box must render the long path un-truncated; out=%s", maxed)
	}
}

// While loading, enter falls into suggestion mode (the list isn't built yet).
// When the tracked-file list lands and the typed input turns out to be an
// exact tracked path, the popup should open it right away instead of
// stranding the user in the suggestion list waiting for a second enter.
func TestFilePathPopupExactDeliveryAutoOpens(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File history")
	m = typeRunes(t, m, "README.md")
	m, _ = send(m, keyType(tea.KeyEnter)) // list not loaded yet
	p := layerOf[*filePathPopup](m)
	if p == nil || !p.suggesting || !p.loading {
		t.Fatal("setup: enter while loading must enter suggestion mode in the loading state")
	}
	m, _ = send(m, filePathLsMsg{paths: []string{"README.md"}})
	hv := layerOf[*historyView](m)
	if hv == nil || hv.ctx.path != "README.md" {
		t.Fatalf("delivery of an exact match must auto-open the surface; hv=%+v", hv)
	}
	if layerOf[*filePathPopup](m) != nil || layerOf[*commandPalette](m) != nil {
		t.Fatal("auto-open must unwind both the popup and the palette")
	}
}

// Cursor/navigation keys that HandleEditKey doesn't change the value for
// (Left, Right, Home, End, and unconsumed keys) must not drop the user's
// place in the suggestion list.
func TestFilePathPopupSuggestionCursorKeyPreservesSel(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "internal/tui/model.go", "internal/tui/view.go")
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter))
	m, _ = send(m, keyType(tea.KeyDown)) // sel=1
	p := layerOf[*filePathPopup](m)
	if p.sel != 1 {
		t.Fatalf("setup: expected sel=1, got %d", p.sel)
	}
	wantLen := len(p.matches)
	var wantFirst string
	if wantLen > 0 {
		wantFirst = p.matches[0].S
	}
	m, _ = send(m, keyType(tea.KeyLeft))
	if p.sel != 1 {
		t.Fatalf("Left must not reset sel to 0; got %d", p.sel)
	}
	if len(p.matches) != wantLen || (wantLen > 0 && p.matches[0].S != wantFirst) {
		t.Fatalf("Left must not rerank the matches; got %+v", p.matches)
	}
}

func TestFilePathPopupRendersLoadingList(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter)) // suggesting while loading
	p := layerOf[*filePathPopup](m)
	if out := p.box(m); !strings.Contains(out, "(loading…)") {
		t.Fatalf("box must show the loading placeholder; out=%s", out)
	}
}
