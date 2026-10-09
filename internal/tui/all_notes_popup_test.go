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
	u, _ := m.Update(allNotesMsg{ov: allNotesFixture(), gen: m.allNotesGen})
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
	// The note opens where it is stored: this diff draws a range review's
	// notes too, which a commit's own diff leaves to the review.
	if !m.topLayer().(*diffView).rangeNotes {
		t.Fatal("a diff opened from View all notes must draw range-review notes")
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
	u, _ := m.Update(allNotesMsg{gen: m.allNotesGen})
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

// longNotesModel is the popup over one untracked file deep in a long path and
// one commit whose subject and note summary run past the popup's width.
func longNotesModel(t *testing.T) (Model, *allNotesPopup, string, string, string) {
	t.Helper()
	dir := "src/dddd/gggg/qqqqqqqqqqqqqqq/ggggg/hhhhhhhhhhh/dsdsd/sdfsdfsdfsdf/sdsdsdsad/kkkkkkkkkkkk/mmmmmmmmm/werwer"
	subject := "variant B: v2 profile endpoint, shorter LegacyAuth TTL, add CacheConfig, tighten the retry budget END"
	summary := "Live steering demo: this line was annotated by an agent while the user watched the diff END"
	ov := domain.NotesOverview{
		Untracked: []domain.NoteFileNotes{{
			Addr:  model.FileAddress{State: model.StateUntracked, Worktree: "/repo", Path: dir + "/Main2.kt"},
			Notes: []domain.ResolvedNote{rootNote("u1", 3, "test note", "", model.NoteSourceUser, model.NoteActive)},
		}},
		Commits: []domain.NoteCommitNotes{{
			Hash: anLiveSHA, Subject: subject, UnixTime: time.Now().Unix(),
			Files: []domain.NoteFileNotes{{
				Addr:  model.FileAddress{State: model.StateCommitted, Commit: anLiveSHA, Path: "src/PipelineConfig.kt"},
				Notes: []domain.ResolvedNote{rootNote("c1", 42, summary, "", model.NoteSourceAgent, model.NoteActive)},
			}},
		}},
	}
	m := footerModel()
	m.width, m.height = 140, 40 // wider than the popup, so a tooltip has room
	m.currentWorktree = "/repo"
	m, _ = m.openAllNotes()
	u, _ := m.Update(allNotesMsg{ov: ov, gen: m.allNotesGen})
	m = u.(Model)
	return m, layerOf[*allNotesPopup](m), dir, subject, summary
}

// A long directory heading is cut in the MIDDLE: its head and its last
// segment stay readable.
func TestAllNotesElidesALongDirectoryInTheMiddle(t *testing.T) {
	t.Parallel()
	m, _, _, _, _ := longNotesModel(t)
	for _, line := range allNotesScreen(m) {
		if strings.Contains(line, "src/dddd") {
			if !strings.Contains(line, "…") || !strings.Contains(line, "/werwer/") {
				t.Fatalf("directory row %q must be elided in the middle, keeping /werwer/", line)
			}
			return
		}
	}
	t.Fatal("no directory row on screen")
}

// A selected row whose text is cut shows its full text in the bottom bar;
// an unselected one does not.
func TestAllNotesTooltipRevealsCutText(t *testing.T) {
	t.Parallel()
	m, p, dir, subject, summary := longNotesModel(t)
	// A note's summary wraps whole now; what the bar still reveals is a fixed
	// column the layout cut — here a WHERE too long for its cell.
	for _, r := range p.rows {
		if r.note != nil && r.note.Note.ID == "c1" {
			r.note.Range = [2]int{4200, 4299}
		}
	}
	for _, tc := range []struct {
		name string
		pick func(r anRow) bool
		full string
	}{
		{"directory", func(r anRow) bool { return r.kind == anDir }, dir + "/"},
		{"commit heading", func(r anRow) bool { return r.kind == anSub && strings.Contains(r.text, "variant B") }, "tighten the retry budget END"},
		{"note", func(r anRow) bool { return r.note != nil && r.note.Note.ID == "c1" }, "new:4200-4299 · "},
	} {
		joined := strings.Join(allNotesScreen(m), "\n")
		if strings.Contains(joined, tc.full) {
			t.Fatalf("%s: the full text must not show before the row is selected", tc.name)
		}
		p.sel = -1
		for i, r := range p.visible() {
			if tc.pick(r) {
				p.sel = i
				break
			}
		}
		if p.sel < 0 {
			t.Fatalf("%s: no such row", tc.name)
		}
		screen := allNotesScreen(m)
		for len(screen) > 0 && strings.TrimSpace(screen[len(screen)-1]) == "" {
			screen = screen[:len(screen)-1]
		}
		if last := screen[len(screen)-1]; !strings.Contains(last, tc.full) {
			t.Fatalf("%s: the bottom bar must show %q, got %q", tc.name, tc.full, last)
		}
		if last := screen[len(screen)-1]; strings.ContainsAny(last, "▾▸") {
			t.Fatalf("%s: the bottom bar must not carry the fold marker: %q", tc.name, last)
		}
		if n := strings.Count(strings.Join(screen, "\n"), tc.full); n != 1 {
			t.Fatalf("%s: the full text must show once (the bottom bar, not over the row), got %d", tc.name, n)
		}
		p.sel = 0
	}
	_ = subject
	// The summary itself is never cut: its last word is on screen.
	words := strings.Fields(summary)
	if !strings.Contains(strings.Join(allNotesScreen(m), "\n"), words[len(words)-2]) {
		t.Fatalf("the note summary must wrap whole, not be cut")
	}
}

// The bottom bar is quiet: the terminal's own colours, never the tooltip's
// black-on-yellow (painful in low light — user report). Sets the colour
// profile and theme (process-global), so it does NOT call t.Parallel().
func TestAllNotesBottomBarIsNotHighlighted(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark)

	m, p, _, _, _ := longNotesModel(t)
	summary := "new:4200-4299"
	for i, r := range p.visible() {
		if r.note != nil {
			r.note.Range = [2]int{4200, 4299} // a WHERE cut by its cell
			p.sel = i
		}
	}
	lines := strings.Split(m.View(), "\n")
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	last := lines[len(lines)-1]
	if !strings.Contains(ansi.Strip(last), summary) {
		t.Fatalf("precondition: the bar shows the cut note, got %q", ansi.Strip(last))
	}
	tip := st().tooltip.Render("x")
	if sgr := tip[:strings.Index(tip, "x")]; sgr != "" && strings.Contains(last, sgr) {
		t.Fatalf("the bottom bar must not wear the tooltip highlight: %q", last)
	}
}

func reviewsFixture() domain.NotesOverview {
	ov := allNotesFixture()
	at := time.Now().Add(-3 * time.Hour)
	ov.Commits[1].Reviews = []domain.Review{{ID: "rev1", Kind: domain.ReviewWasTip, Commit: anMissingSHA,
		Branch: "feature", Agent: "Claude Code", Summary: "Review: feature (1a2b3c4..5d6e7f8)", Created: at}}
	return ov
}

func TestAllNotesListsAReviewUnderItsCommit(t *testing.T) {
	t.Parallel()
	rows := buildAllNotesRows(reviewsFixture(), "repo")
	var at int = -1
	for i, r := range rows {
		if r.kind == anReview {
			at = i
		}
	}
	if at < 1 || rows[at-1].text != "Reviews" {
		t.Fatalf("no review row under an @notes/ heading: %+v", rows)
	}
	p := &allNotesPopup{rows: rows, folded: map[string]bool{}}
	text := p.anRowText(rows[at], 120, time.Now())
	for _, want := range []string{"review", "Claude C", "was tip", "Review: feature"} {
		if !strings.Contains(text, want) {
			t.Errorf("review row %q lacks %q", text, want)
		}
	}
	// The fixed columns cut long values; the bottom bar carries them whole.
	bar := anBarText(rows[at], time.Now())
	for _, want := range []string{"Claude Code", "was tip feature"} {
		if !strings.Contains(bar, want) {
			t.Errorf("bar text %q lacks %q", bar, want)
		}
	}
	if p.count() != 6 {
		t.Errorf("count = %d, want 6 (5 threads + 1 review)", p.count())
	}
}

func TestAllNotesOpensReviewOfMissingCommit(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.currentWorktree = "/repo"
	m.status.Branch = "feature" // a branch's review is listed on that branch
	m, _ = m.openAllNotes()
	u, _ := m.Update(allNotesMsg{ov: reviewsFixture(), gen: m.allNotesGen})
	m = u.(Model)
	p := layerOf[*allNotesPopup](m)
	for i, r := range p.visible() {
		if r.kind == anReview {
			p.sel = i
		}
	}
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainCmds(t, u.(Model), cmd) // the review is read off-thread, then opens
	v, ok := m.topLayer().(*fileViewer)
	if !ok || v.src.kind != srcNote || v.src.rev != "rev1" {
		t.Fatalf("enter on a review must open it, top = %T (notice %q)", m.topLayer(), p.notice)
	}
}

// shelfEntryNotesModel opens the popup over a shelf entry that carries two
// entry notes (a recycle's) and one file note.
func shelfEntryNotesModel(t *testing.T) (Model, *allNotesPopup) {
	t.Helper()
	at := time.Now().Add(-2 * time.Hour)
	entry := func(id, summary, text string) domain.ResolvedNote {
		return domain.ResolvedNote{Note: model.Note{ID: id, Summary: summary, Rationale: text, Author: "gg",
			Source: model.NoteSourceAgent, Created: at, Side: model.NoteSideNew,
			Address: domain.ShelfEntryNote("sh1")}, Status: model.NoteActive, Range: [2]int{1, 1}}
	}
	file := rootNote("f1", 4, "check this", "", model.NoteSourceUser, model.NoteActive)
	ov := domain.NotesOverview{Shelves: []domain.NoteShelfNotes{{
		ID: "sh1", Label: "WIP on foo",
		Entry: []domain.ResolvedNote{entry("e1", "deleted 2 files", "a.go\nb.go"), entry("e2", "renamed 1 file", "c.go → d.go")},
		Files: []domain.NoteFileNotes{{Addr: model.FileAddress{State: model.StateShelf, ShelfID: "sh1", Path: "src/x.go"},
			Notes: []domain.ResolvedNote{file}}},
	}}}
	m := footerModel()
	m, _ = m.openAllNotes()
	u, _ := m.Update(allNotesMsg{ov: ov, gen: m.allNotesGen})
	m = u.(Model)
	return m, layerOf[*allNotesPopup](m)
}

func TestAllNotesListsAShelfEntrysOwnNotes(t *testing.T) {
	t.Parallel()
	m, p := shelfEntryNotesModel(t)
	if got, want := p.count(), 3; got != want {
		t.Fatalf("the list must show every counted thread: %d rows, want %d", got, want)
	}
	screen := strings.Join(allNotesScreen(m), "\n")
	iShelf := strings.Index(screen, "WIP on foo")
	iE1, iE2 := strings.Index(screen, "deleted 2 files"), strings.Index(screen, "renamed 1 file")
	iDir := strings.Index(screen, "src/")
	if iShelf < 0 || iE1 < 0 || iE2 < 0 || iDir < 0 {
		t.Fatalf("shelf row, both entry notes and the file's directory must show:\n%s", screen)
	}
	if !(iShelf < iE1 && iE1 < iE2 && iE2 < iDir) {
		t.Fatalf("entry notes must sit right under the shelf row, oldest first, before its files:\n%s", screen)
	}
	for _, l := range allNotesScreen(m) {
		if strings.Contains(l, "deleted 2 files") && !strings.Contains(l, "shelf") {
			t.Fatalf("an entry note's WHERE is the shelf, not a line: %q", l)
		}
	}
}

func TestAllNotesFilterFindsAShelfEntryNote(t *testing.T) {
	t.Parallel()
	_, p := shelfEntryNotesModel(t)
	p.setQuery("renamed")
	var ids []string
	for _, r := range p.visible() {
		if r.note != nil {
			ids = append(ids, r.note.Note.ID)
		}
	}
	if strings.Join(ids, ",") != "e2" {
		t.Fatalf("the query must keep only the matching entry note, got %v", ids)
	}
}

func TestAllNotesEnterReadsAShelfEntryNote(t *testing.T) {
	t.Parallel()
	m, p := shelfEntryNotesModel(t)
	selectNote(t, p, "e1")
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if _, ok := m.topLayer().(*contentPopup); !ok {
		t.Fatalf("enter on an entry note must open the note window, top = %T", m.topLayer())
	}
	screen := strings.Join(allNotesScreen(m), "\n")
	if !strings.Contains(screen, "deleted 2 files") || !strings.Contains(screen, "b.go") {
		t.Fatalf("the window must show that note's summary and text:\n%s", screen)
	}
	if strings.Contains(screen, "renamed 1 file") {
		t.Fatalf("the window must show only the chosen note:\n%s", screen)
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if _, ok := m.topLayer().(*allNotesPopup); !ok {
		t.Fatalf("esc must return to View all notes, top = %T", m.topLayer())
	}
}

// A recycle's summary is "Recycled from <dir> (<branch>)": a cut row loses
// the path's middle, never the branch at its end.
func TestAllNotesCutsAShelfEntryNoteInTheMiddle(t *testing.T) {
	t.Parallel()
	m, p := shelfEntryNotesModel(t)
	long := "Recycled from /home/someone/work/" + strings.Repeat("deep/", 20) + "wt (feat/x)"
	p.rows[2].note.Note.Summary = long // group, shelf, then its first entry note
	for _, l := range allNotesScreen(m) {
		if strings.Contains(l, "Recycled from") {
			if !strings.Contains(l, "wt (feat/x)") || strings.Contains(l, long) {
				t.Fatalf("the row must keep the branch and cut the path's middle: %q", l)
			}
			return
		}
	}
	t.Fatal("no row for the entry note")
}
