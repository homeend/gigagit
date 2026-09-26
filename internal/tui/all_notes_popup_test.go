package tui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/theme"
)

const (
	anLiveSHA    = "926468f8aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	anMissingSHA = "1a2b3c4dbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// allNotesFixture is every group the popup draws: an unstaged file under a
// directory (one user note, one stale agent note), a root-level staged file,
// a live commit and a missing one.
func allNotesFixture() domain.NotesOverview {
	at := time.Now().Add(-2 * time.Hour)
	note := func(id string, line int, summary string, src model.NoteSource, st model.NoteStatus) domain.ResolvedNote {
		r := rootNote(id, line, summary, "", src, st)
		r.Note.Created = at
		return r
	}
	live := func(st model.FileState, path string) model.FileAddress {
		return model.FileAddress{State: st, Worktree: "/repo", Path: path}
	}
	c1 := note("c1", 31, "id not unique", model.NoteSourceAgent, model.NoteActive)
	c1.Replies = []domain.ResolvedNote{{Note: model.Note{ID: "r1", ParentID: "c1", Summary: "agreed"}}}
	return domain.NotesOverview{
		Unstaged: []domain.NoteFileNotes{{
			Addr: live(model.StateUnstaged, "internal/tui/view.go"),
			Notes: []domain.ResolvedNote{
				note("u1", 5, "badge overlaps subject", model.NoteSourceUser, model.NoteActive),
				note("u2", 9, "width cut too early", model.NoteSourceAgent, model.NoteStale),
			},
		}},
		Staged: []domain.NoteFileNotes{{
			Addr:  live(model.StateStaged, "README.md"),
			Notes: []domain.ResolvedNote{note("s1", 3, "typo in heading", model.NoteSourceUser, model.NoteActive)},
		}},
		Commits: []domain.NoteCommitNotes{{
			Hash: anLiveSHA, Subject: "Merge feat", UnixTime: at.Unix(),
			Files: []domain.NoteFileNotes{{
				Addr:   model.FileAddress{State: model.StateCommitted, Commit: anLiveSHA, Path: "internal/domain/tasks.go"},
				Status: "M",
				Notes:  []domain.ResolvedNote{c1},
			}},
		}, {
			Hash: anMissingSHA, Missing: true,
			Files: []domain.NoteFileNotes{{
				Addr:  model.FileAddress{State: model.StateCommitted, Commit: anMissingSHA, Path: "go.mod"},
				Notes: []domain.ResolvedNote{note("m1", 5, "bump chroma", model.NoteSourceUser, model.NoteOrphaned)},
			}},
		}},
	}
}

// allNotesModel opens the popup the way the palette does and delivers the
// fixture as the async load.
func allNotesModel(t *testing.T) (Model, *allNotesPopup) {
	t.Helper()
	m := footerModel()
	m.currentWorktree = "/repo"
	m, cmd := m.openAllNotes()
	if cmd == nil {
		t.Fatal("opening must start the overview load")
	}
	p := layerOf[*allNotesPopup](m)
	if p == nil || !p.loading {
		t.Fatal("the popup must open in its loading state")
	}
	u, _ := m.Update(allNotesMsg{ov: allNotesFixture(), gen: m.loadGen})
	m = u.(Model)
	return m, layerOf[*allNotesPopup](m)
}

func allNotesScreen(m Model) []string {
	return strings.Split(ansi.Strip(m.View()), "\n")
}

// selectNote parks the cursor on the note row with id.
func selectNote(t *testing.T, p *allNotesPopup, id string) {
	t.Helper()
	for i, r := range p.visible() {
		if r.note != nil && r.note.Note.ID == id {
			p.sel = i
			return
		}
	}
	t.Fatalf("no visible row for note %s", id)
}

func TestAllNotesPaletteEntry(t *testing.T) {
	t.Parallel()
	for _, c := range paletteCommands() {
		if c.label == i18n.T("View all notes…") {
			return
		}
	}
	t.Fatal("the palette must offer View all notes…")
}

// The popup is a tree (group → state/commit → directory → file → note) and
// every note row puts its STATUS in the same column.
func TestAllNotesRendersTheTreeWithAlignedColumns(t *testing.T) {
	t.Parallel()
	m, p := allNotesModel(t)
	m.height = 60
	p.maximized = true // the whole fixture on one screen
	screen := allNotesScreen(m)
	joined := strings.Join(screen, "\n")
	for _, want := range []string{"All notes", "STATUS", "Working tree", "Unstaged", "Staged",
		"internal/tui/", "view.go", "README.md", "Commits", "926468f", "Merge feat",
		"1a2b3c4", "(missing — rewritten or deleted)", "id not unique", "↩1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("screen lacks %q:\n%s", want, joined)
		}
	}
	col := -1
	for _, want := range []struct{ status, summary string }{
		{"active", "badge overlaps subject"},
		{"stale", "width cut too early"},
		{"active", "typo in heading"},
		{"active", "id not unique"},
		{"missing", "bump chroma"},
	} {
		found := false
		for _, line := range screen {
			if !strings.Contains(line, want.summary) {
				continue
			}
			found = true
			i := strings.Index(line, want.status)
			if i < 0 {
				t.Fatalf("row %q lacks status %q", line, want.status)
			}
			c := lipgloss.Width(line[:i])
			if col < 0 {
				col = c
			} else if c != col {
				t.Fatalf("status of %q is at column %d, want %d (one column for every row)", want.summary, c, col)
			}
		}
		if !found {
			t.Fatalf("no row for %q", want.summary)
		}
	}
}

func TestAllNotesFoldsAHeading(t *testing.T) {
	t.Parallel()
	m, p := allNotesModel(t)
	for i, r := range p.visible() {
		if r.kind == anGroup && strings.Contains(r.text, "Commits") {
			p.sel = i
		}
	}
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	joined := strings.Join(allNotesScreen(m), "\n")
	if strings.Contains(joined, "id not unique") || strings.Contains(joined, "bump chroma") {
		t.Fatalf("a folded Commits heading must hide its notes:\n%s", joined)
	}
	if !strings.Contains(joined, "badge overlaps subject") {
		t.Fatal("folding Commits must leave the working tree alone")
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = u.(Model)
	if !strings.Contains(strings.Join(allNotesScreen(m), "\n"), "id not unique") {
		t.Fatal("→ must unfold the heading again")
	}
}

// Typing filters notes by text; a match keeps its headings so it still says
// where it lives.
func TestAllNotesFilterKeepsAncestors(t *testing.T) {
	t.Parallel()
	m, p := allNotesModel(t)
	for _, r := range "chroma" {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(Model)
	}
	var notes []string
	var headings []string
	for _, r := range p.visible() {
		if r.note != nil {
			notes = append(notes, r.note.Note.Summary)
		} else {
			headings = append(headings, r.text)
		}
	}
	if len(notes) != 1 || notes[0] != "bump chroma" {
		t.Fatalf("filter kept %v, want only the chroma note", notes)
	}
	joined := strings.Join(headings, "|")
	if !strings.Contains(joined, "Commits") || !strings.Contains(joined, "go.mod") || strings.Contains(joined, "Working tree") {
		t.Fatalf("headings kept = %v, want the match's ancestors only", headings)
	}
}

// Enter on a commit note opens that file's diff OVER the popup and lands on
// the note once its notes load; esc returns to the popup.
func TestAllNotesEnterOpensTheCommitDiffAndLands(t *testing.T) {
	t.Parallel()
	m, p := allNotesModel(t)
	selectNote(t, p, "c1")
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if cmd == nil {
		t.Fatal("enter must start the diff load")
	}
	if _, ok := m.topLayer().(*diffView); !ok {
		t.Fatalf("enter must push the diff, top = %T", m.topLayer())
	}
	if want := "commit:" + anLiveSHA + ":internal/domain/tasks.go"; m.diffTag != want {
		t.Fatalf("diffTag = %q, want %q", m.diffTag, want)
	}
	if layerOf[*allNotesPopup](m) == nil {
		t.Fatal("the popup must stay under the diff")
	}
	m = arriveDiff(m)
	u, _ = m.Update(notesLoadedMsg{tag: m.diffTag, notes: []domain.ResolvedNote{
		rootNote("c0", 3, "earlier", "", model.NoteSourceUser, model.NoteActive),
		rootNote("c1", 31, "id not unique", "", model.NoteSourceAgent, model.NoteActive),
	}})
	m = u.(Model)
	if got := m.diffLayer().curLine; got != 30 {
		t.Fatalf("landed on line %d, want 30 (note c1 on line 31)", got)
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if _, ok := m.topLayer().(*allNotesPopup); !ok {
		t.Fatalf("esc on the diff must return to the popup, top = %T", m.topLayer())
	}
}

// Enter on a working-tree note opens the status diff when the file still has
// changes of that kind.
func TestAllNotesEnterOpensTheStagedDiff(t *testing.T) {
	t.Parallel()
	m, p := allNotesModel(t)
	m.status.Files = []model.FileStatus{{Path: "README.md", Staged: 'M', Unstaged: '.'}}
	selectNote(t, p, "s1")
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if m.diffTag != statusDiffTag("README.md", true) {
		t.Fatalf("diffTag = %q, want the staged diff of README.md", m.diffTag)
	}
	if v := m.diffLayer(); v == nil || v.noteAddr.State != model.StateStaged {
		t.Fatal("the staged diff must carry the staged note address")
	}
}

// A note whose target cannot be opened says why, in the popup, and stays.
func TestAllNotesEnterExplainsAnUnopenableNote(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ id, want string }{
		{"m1", "commit no longer exists"},
		{"u1", "file has no unstaged changes"},
	} {
		m, p := allNotesModel(t)
		selectNote(t, p, tc.id)
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = u.(Model)
		if _, ok := m.topLayer().(*allNotesPopup); !ok {
			t.Fatalf("%s: the popup must stay open, top = %T", tc.id, m.topLayer())
		}
		if !strings.Contains(strings.Join(allNotesScreen(m), "\n"), tc.want) {
			t.Fatalf("%s: the popup must say %q", tc.id, tc.want)
		}
	}
}

func TestAllNotesEmpty(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m, _ = m.openAllNotes()
	u, _ := m.Update(allNotesMsg{gen: m.loadGen})
	m = u.(Model)
	if !strings.Contains(strings.Join(allNotesScreen(m), "\n"), "No notes in this repository.") {
		t.Fatal("an empty store must say so")
	}
}

// Letters extend the filter and never reach the global keymap.
func TestAllNotesSwallowsKeys(t *testing.T) {
	t.Parallel()
	m, p := allNotesModel(t)
	u, cmd := m.Update(keyMsg("q"))
	m = u.(Model)
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("q must not quit while the popup is open")
		}
	}
	if p.query != "q" || layerOf[*allNotesPopup](m) == nil {
		t.Fatalf("q must extend the filter, query=%q", p.query)
	}
}

func TestAllNotesFitsASmallTerminal(t *testing.T) {
	t.Parallel()
	m, _ := allNotesModel(t)
	m.width, m.height = 60, 16
	lines := allNotesScreen(m)
	if len(lines) > m.height {
		t.Fatalf("%d lines on a %d-row terminal", len(lines), m.height)
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w > m.width {
			t.Fatalf("line %q is %d wide on a %d-column terminal", l, w, m.width)
		}
	}
}

// Nested directories: each directory heading appears once and a root file is
// never drawn under one (the domain hands files over dir-major).
func TestAllNotesDirectoryHeadingsAppearOnce(t *testing.T) {
	t.Parallel()
	var fs []domain.NoteFileNotes
	for i, p := range []string{"z.go", "a/m.go", "a/p.go", "a/n/o.go"} {
		fs = append(fs, domain.NoteFileNotes{
			Addr:  model.FileAddress{State: model.StateUnstaged, Worktree: "/repo", Path: p},
			Notes: []domain.ResolvedNote{rootNote("n"+strconv.Itoa(i), 1, "note on "+p, "", model.NoteSourceUser, model.NoteActive)},
		})
	}
	rows := buildAllNotesRows(domain.NotesOverview{Unstaged: fs}, "repo")
	var order []string
	for _, r := range rows {
		if r.kind == anDir || r.kind == anFile {
			order = append(order, r.text)
		}
	}
	want := "z.go a/ m.go p.go a/n/ o.go"
	if got := strings.Join(order, " "); got != want {
		t.Fatalf("tree = %q, want %q", got, want)
	}
}

// An agent's WHO cell is painted in the agent frame colour. Sets the colour
// profile and theme (process-global), so it does NOT call t.Parallel().
func TestAllNotesPaintsAgentWho(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark)

	m, p := allNotesModel(t)
	p.sel = 0 // keep the agent rows unselected (reverse video paints no colour)
	agent := st().noteFrameAgent.Render("ada")
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "width cut too early") {
			if !strings.Contains(line, agent) {
				t.Fatalf("the agent row's WHO must be painted: %q", line)
			}
			return
		}
	}
	t.Fatal("no agent row on screen")
}
