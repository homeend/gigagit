package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

func histFixture() *historyView {
	return &historyView{
		ctx: navContext{path: "a.go", rev: ""},
		commits: []model.FileCommit{
			{Commit: model.Commit{Hash: "aaaaaaa", Subject: "edit", Author: "Ada"}, Status: "M", Path: "a.go"},
			{Commit: model.Commit{Hash: "bbbbbbb", Subject: "add", Author: "Bob"}, Status: "A", Path: "a.go"},
		},
	}
}

func TestHistoryRenderListsCommits(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	h := histFixture()
	out := h.render(m, "")
	if !strings.Contains(out, "edit") || !strings.Contains(out, "add") {
		t.Errorf("history render missing commit subjects:\n%s", out)
	}
	if !strings.Contains(out, "a.go") {
		t.Errorf("history header missing path:\n%s", out)
	}
}

func TestHistoryDownMovesSelectionAndReloads(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	h := histFixture()
	m = m.pushLayer(h)
	_, cmd := h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if h.sel != 1 {
		t.Fatalf("j should move selection to 1, got %d", h.sel)
	}
	if cmd == nil {
		t.Fatal("moving selection should fire a right-pane reload cmd")
	}
}

func TestHistoryEscPops(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	h := histFixture()
	m = m.pushLayer(h)
	m, _ = h.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.topLayer() != nil {
		t.Fatal("esc should pop the history surface")
	}
}

// Blame from history must use the file's name *at the selected commit*, not the
// current path. For a commit predating a rename/copy the current name does not
// exist in that commit's tree, so blaming it would fail (git exit 128).
func TestHistoryBlameUsesHistoricalPath(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	h := &historyView{
		ctx: navContext{path: "timing4.log", rev: ""},
		commits: []model.FileCommit{
			{Commit: model.Commit{Hash: "aaaaaaa", Subject: "rename"}, Status: "C", OldPath: "timing.log", Path: "timing4.log"},
			{Commit: model.Commit{Hash: "bbbbbbb", Subject: "add"}, Status: "A", Path: "timing.log"},
		},
		sel: 1, // the pre-rename commit, where the file was "timing.log"
	}
	m = m.pushLayer(h)
	m, _ = h.update(m, keyMsg("b"))
	bv, ok := m.topLayer().(*blameView)
	if !ok {
		t.Fatal("b should push a blameView")
	}
	if bv.ctx.path != "timing.log" {
		t.Errorf("blame must use the historical path; got %q want %q", bv.ctx.path, "timing.log")
	}
	if bv.ctx.rev != "bbbbbbb" {
		t.Errorf("blame should target the selected commit; got rev %q", bv.ctx.rev)
	}
}

// q no longer quits from the history view — only the base layout quits on q.
func TestHistoryQInert(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	h := histFixture()
	m = m.pushLayer(h)
	m, cmd := h.update(m, keyMsg("q"))
	if cmd != nil {
		t.Fatal("q must not quit from the history view (inert)")
	}
	if m.topLayer() == nil {
		t.Fatal("q must leave the history surface on the stack")
	}
}

func TestStatusHOpensHistory(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30, focus: panelFiles, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.go", Unstaged: 'M'}}}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	got := mm.(Model)
	h, ok := got.topLayer().(*historyView)
	if !ok {
		t.Fatal("h on a Status file should push a historyView")
	}
	if h.ctx.path != "a.go" || h.ctx.rev != "" {
		t.Errorf("wrong navContext: %+v", h.ctx)
	}
}

func TestStagedHOpensHistory(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30, focus: panelStaged, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.go", Staged: 'M'}}}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	got := mm.(Model)
	h, ok := got.topLayer().(*historyView)
	if !ok {
		t.Fatal("h on a Staged file should push a historyView")
	}
	if h.ctx.path != "a.go" || h.ctx.rev != "" {
		t.Errorf("wrong navContext: %+v", h.ctx)
	}
}

func TestFilesViewHOpensHistory(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.go", path: "a.go"}}}
	m.filesTreeFocused = true
	m.filesHash = "abc123"
	mm, _ := m.updateFilesViewKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	h, ok := mm.(Model).topLayer().(*historyView)
	if !ok {
		t.Fatal("h on a files-view row should push a historyView")
	}
	if h.ctx.path != "a.go" || h.ctx.rev != "abc123" {
		t.Errorf("wrong navContext: %+v", h.ctx)
	}
}

func TestDiffViewHOpensHistory(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	m = m.pushLayer(&diffView{title: "a.go", rev: "abc123"})
	mm, _ := m.updateDiffViewKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	h, ok := mm.(Model).topLayer().(*historyView)
	if !ok {
		t.Fatal("h in the diff view should push a historyView")
	}
	if h.ctx.path != "a.go" || h.ctx.rev != "abc123" {
		t.Errorf("wrong navContext: %+v", h.ctx)
	}
}

func TestHistoryViewWrapMode(t *testing.T) {
	t.Parallel()
	h := &historyView{
		ctx:     navContext{path: "x"},
		commits: []model.FileCommit{{Commit: model.Commit{Hash: "abcdef0", Subject: strings.Repeat("w", 80)}, Status: "M", Path: "x"}},
		mode:    modeWrap,
	}
	m := Model{width: 50, height: 20} // < 60 => list-only, easier to assert
	out := h.render(m, "")
	if strings.Count(out, "w") < 30 {
		t.Errorf("history wrap mode did not expand the subject:\n%s", out)
	}
}

// The left list is capped at 60 columns even on wide terminals; the remaining
// width goes to the diff pane.
func TestHistoryListWidthCappedAt60(t *testing.T) {
	t.Parallel()
	m := Model{width: 200, height: 30}
	h := histFixture()
	out := h.render(m, "")
	lines := strings.Split(out, "\n")
	for i, ln := range lines[1 : len(lines)-1] { // body: skip header + hint
		idx := strings.Index(ln, "│")
		if idx < 0 {
			t.Fatalf("body line %d missing the pane separator:\n%s", i, out)
		}
		if w := lipgloss.Width(ln[:idx]); w != 60 {
			t.Fatalf("left list must be 60 cols wide, got %d: %q", w, ln)
		}
	}
}

// Each history entry spans two lines: date+author+hash on the first, the
// commit subject on the second.
func TestHistoryEntryTwoLines(t *testing.T) {
	t.Parallel()
	ts := int64(1754800000)
	h := &historyView{
		ctx: navContext{path: "a.go"},
		commits: []model.FileCommit{
			{Commit: model.Commit{Hash: "abcdef1234", Subject: "fix parser", Author: "Ada", UnixTime: ts}, Status: "M", Path: "a.go"},
		},
	}
	m := Model{width: 50, height: 20} // list-only
	out := h.render(m, "")
	lines := strings.Split(out, "\n")
	meta := -1
	for i, ln := range lines {
		if strings.Contains(ln, "abcdef1") {
			meta = i
			break
		}
	}
	if meta < 0 {
		t.Fatalf("no meta line carrying the short hash:\n%s", out)
	}
	ml := lines[meta]
	date := time.Unix(ts, 0).Format("2006-01-02 15:04")
	if !strings.Contains(ml, date) {
		t.Errorf("meta line must carry the commit date %q, got %q", date, ml)
	}
	if !strings.Contains(ml, "Ada") {
		t.Errorf("meta line must carry the author, got %q", ml)
	}
	if !strings.HasPrefix(ml, "> ") {
		t.Errorf("selected entry's meta line should carry the cursor, got %q", ml)
	}
	if strings.Contains(ml, "fix parser") {
		t.Errorf("subject must not share the meta line: %q", ml)
	}
	if meta+1 >= len(lines) || !strings.Contains(lines[meta+1], "fix parser") {
		t.Errorf("subject must sit on the line below the meta line:\n%s", out)
	}
}

// A subject longer than the list width wraps onto continuation lines instead
// of being cut off.
func TestHistoryLongSubjectWrapsFully(t *testing.T) {
	t.Parallel()
	h := &historyView{
		ctx: navContext{path: "a.go"},
		commits: []model.FileCommit{
			{Commit: model.Commit{Hash: "abcdef1234", Subject: strings.Repeat("Q", 120), Author: "Ada", UnixTime: 1754800000}, Status: "M", Path: "a.go"},
		},
	}
	m := Model{width: 50, height: 20} // list-only
	out := h.render(m, "")
	if got := strings.Count(out, "Q"); got != 120 {
		t.Errorf("wrapped subject must stay fully visible: want 120 Qs, got %d:\n%s", got, out)
	}
	for _, ln := range strings.Split(out, "\n") {
		if lipgloss.Width(ln) > 50 {
			t.Errorf("line exceeds the view width: %q", ln)
		}
	}
}

// A long author name is truncated in place; the hash stays visible.
func TestHistoryLongAuthorStillShowsHash(t *testing.T) {
	t.Parallel()
	h := &historyView{
		ctx: navContext{path: "a.go"},
		commits: []model.FileCommit{
			{Commit: model.Commit{Hash: "abcdef1234", Subject: "fix", Author: strings.Repeat("A", 80), UnixTime: 1754800000}, Status: "M", Path: "a.go"},
		},
	}
	m := Model{width: 50, height: 20} // list-only
	out := h.render(m, "")
	if !strings.Contains(out, "abcdef1") {
		t.Errorf("hash must survive a long author name:\n%s", out)
	}
}
