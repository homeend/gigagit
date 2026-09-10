package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

func TestLinkForRemoteAndLocalForms(t *testing.T) {
	t.Parallel()
	const sha = "eb759989a1b2c3d4e5f60718293a4b5c6d7e8f90"
	base := func() Model {
		m := newTestModel(t)
		m.currentWorktree = "/mnt/t/others/test-1"
		return m
	}
	named := base()
	named.linkRepoName = "gigagit"

	cases := []struct {
		name string
		m    Model
		addr model.FileAddress
		side model.NoteSide
		line int
		want string
	}{
		{"named working tree", named, model.FileAddress{State: model.StateUnstaged, Path: "a/b.go"}, model.NoteSideNew, 42,
			"gg://gigagit/a/b.go:42"},
		{"named staged", named, model.FileAddress{State: model.StateStaged, Path: "a/b.go"}, model.NoteSideNew, 0,
			"gg://gigagit/a/b.go@staged"},
		{"named commit old side", named, model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "a/b.go"}, model.NoteSideOld, 7,
			"gg://gigagit/a/b.go@" + sha + ":old:7"},
		{"named commit, no path", named, model.FileAddress{State: model.StateCommitted, Commit: sha}, "", 0,
			"gg://gigagit@" + sha},
		// An untracked file has no grammar of its own: it uses the plain
		// working-tree form.
		{"untracked", named, model.FileAddress{State: model.StateUntracked, Path: "new.go"}, model.NoteSideNew, 0,
			"gg://gigagit/new.go"},
		{"local form", base(), model.FileAddress{State: model.StateUnstaged, Path: "src/x.kt"}, model.NoteSideNew, 3,
			"gg:///mnt/t/others/test-1/src/x.kt:3"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := tc.m.linkFor(tc.addr, tc.side, tc.line, 0)
			if !ok {
				t.Fatalf("linkFor(%+v) refused", tc.addr)
			}
			if got != tc.want {
				t.Errorf("linkFor = %q, want %q", got, tc.want)
			}
			if _, err := model.ParseLink(got); err != nil {
				t.Errorf("ParseLink(%q) = %v", got, err)
			}
		})
	}
}

func TestLinkForRefusesWhatTheGrammarCannotHold(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.currentWorktree = "/repo"
	m.linkRepoName = "gigagit"
	for _, addr := range []model.FileAddress{
		{State: model.StateUnstaged, Path: "we@ird.go"},                   // path holds a separator
		{State: model.StateShelf, ShelfID: "s1", Path: "a.go"},            // no grammar for a shelf entry
		{State: model.StateCommitted, Path: "a.go"},                       // committed with no sha
		{State: model.StateCommitted, Commit: "eb759989a1", Path: "a.go"}, // committed with a short (abbreviated) sha
	} {
		if got, ok := m.linkFor(addr, model.NoteSideNew, 0, 0); ok {
			t.Errorf("linkFor(%+v) = %q, want a refusal", addr, got)
		}
	}
}

func TestDiffViewLKeyCopiesTheCursorLink(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, sameRowsTUI(40, 20, 30), []int{20, 30})
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/repo"
	v := m.diffLayer()
	v.title = "a.txt"
	v.noteAddr = model.FileAddress{State: model.StateUnstaged, Path: "a.txt", Worktree: "/repo"}

	// focusBlock(0, body) anchors the cursor on the first change block
	// (index 20; sameRowsTUI marks it Changed with Left/RightNo == 21), so
	// the default anchor is the NEW side, line 21.
	want := "gg://gigagit/a.txt:21"
	got, ok := m.contextLinkText()
	if !ok {
		t.Fatal("contextLinkText refused with a diff open")
	}
	if got != want {
		t.Errorf("contextLinkText = %q, want %q", got, want)
	}
	if _, err := model.ParseLink(got); err != nil {
		t.Fatalf("ParseLink(%q) = %v", got, err)
	}
	// What L actually COPIES: the key runs contextLinkRow's handler, so the
	// row's copyText is the payload — the same seam the other producer tests
	// assert (a non-nil cmd alone would pass even if the key copied the wrong
	// text, or something else entirely).
	row, ok := m.contextLinkRow()
	if !ok {
		t.Fatal("no copy-link row while the diff view is open")
	}
	if row.copyText != want {
		t.Errorf("L would copy %q, want %q", row.copyText, want)
	}
	u, cmd := m.Update(keyMsg("L"))
	if cmd == nil {
		t.Fatal("L in the diff view produced no clipboard command")
	}
	if u.(Model).diffLayer() == nil {
		t.Error("L must not close the diff view")
	}
}

// A3: the local form's CHECKOUT path has the same expressibility rule as a
// file path — '@' and '#' are the grammar's own separators.
func TestLinkForRefusesACheckoutPathWithASeparator(t *testing.T) {
	t.Parallel()
	addr := model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}
	for _, wt := range []string{"/home/user@corp/repo", "/mnt/backup#1/repo"} {
		m := diffModel()
		m.linkRepoName = "" // no remote → the local form
		m.currentWorktree = wt
		if got, ok := m.linkFor(addr, model.NoteSideNew, 0, 0); ok {
			t.Errorf("linkFor with worktree %q = %q, want a refusal", wt, got)
		}
	}
	// A ':' in the checkout path is FINE (drive colons, and POSIX allows it):
	// only a NUMBER after the last ':' is read as a line.
	m := diffModel()
	m.linkRepoName = ""
	m.currentWorktree = "/mnt/odd:name/repo"
	got, ok := m.linkFor(addr, model.NoteSideNew, 0, 0)
	if !ok {
		t.Fatal("a ':' in the checkout path must not refuse")
	}
	if _, err := model.ParseLink(got); err != nil {
		t.Errorf("ParseLink(%q) = %v", got, err)
	}
}

func TestActionMenuCarriesACopyLinkRow(t *testing.T) {
	t.Parallel()
	m := diffModel() // Files panel focused, three status rows; sel 0 = mod.txt (tracked, unstaged M)
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/repo"
	r, ok := m.contextLinkRow()
	if !ok {
		t.Fatal("no copy-link row on the Files panel")
	}
	if r.id != "copy-link" {
		t.Errorf("row id = %q, want copy-link", r.id)
	}
	if want := "gg://gigagit/mod.txt"; r.copyText != want {
		t.Errorf("row copyText = %q, want %q", r.copyText, want)
	}
	if _, err := model.ParseLink(r.copyText); err != nil {
		t.Errorf("row copyText %q does not parse: %v", r.copyText, err)
	}
}

// Plan ruling 11: an untracked file gets the plain working-tree link form —
// same as a tracked one, since the grammar has no "untracked" target of its
// own — verified here at the panel-row surface (linkFor's own case is
// covered by TestLinkForRemoteAndLocalForms/untracked).
func TestActionMenuCopyLinkRowUntrackedFile(t *testing.T) {
	t.Parallel()
	m := diffModel() // sel[panelFiles] index 1 = new.txt, KindUntracked
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/repo"
	m.sel[panelFiles] = 1
	r, ok := m.contextLinkRow()
	if !ok {
		t.Fatal("no copy-link row for the untracked file")
	}
	if want := "gg://gigagit/new.txt"; r.copyText != want {
		t.Errorf("row copyText = %q, want %q", r.copyText, want)
	}
}

func TestCommitsPanelLinkIsACommitLink(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/repo"
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "eb759989a1b2c3d4e5f60718293a4b5c6d7e8f90", Subject: "x"}}
	m.sel[panelCommits] = 0
	got, ok := m.contextLinkText()
	if !ok {
		t.Fatal("no link for the Commits panel")
	}
	if got != "gg://gigagit@eb759989a1b2c3d4e5f60718293a4b5c6d7e8f90" {
		t.Errorf("link = %q", got)
	}
}

// contextCopyRows itself is unchanged (contextLinkRow is a separate,
// additive row appended only at availableActions' two call sites), but the
// row DOES appear on both of those call sites whenever contextLinkText
// succeeds — this covers the Files panel (non-content-window) path via
// availableActions directly, and the diff view (content-window) path via
// TestActionMenuCarriesACopyLinkRow/TestDiffViewLKeyCopiesTheCursorLink above.
func TestAvailableActionsCarriesCopyLinkRow(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/repo"
	rows := availableActions(m)
	if _, ok := findRow(rows, "copy-link"); !ok {
		t.Fatalf("availableActions rows = %v, want a copy-link row", rows)
	}
}

// A Del row (right side is a gap) makes noteAnchorAtCursor default to the
// OLD side — the only shape sameRowsTUI's Changed rows (both sides present)
// never produce, so this builds its own rows.
func TestContextLinkTextOldSide(t *testing.T) {
	t.Parallel()
	rows := sameRowsTUI(40)
	rows[20] = textdiff.Row{Kind: textdiff.Del, Left: "gone", LeftNo: 21}
	m := openedDiffModel(12, rows, []int{20})
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/repo"
	v := m.diffLayer()
	v.title = "a.txt"
	v.noteAddr = model.FileAddress{State: model.StateUnstaged, Path: "a.txt", Worktree: "/repo"}
	v.curLine = 20

	side, line, _, has := m.noteAnchorAtCursor()
	if !has || side != model.NoteSideOld || line != 21 {
		t.Fatalf("noteAnchorAtCursor = side=%v line=%d has=%v, want old/21/true", side, line, has)
	}
	want := "gg://gigagit/a.txt:old:21"
	got, ok := m.contextLinkText()
	if !ok {
		t.Fatal("contextLinkText refused with a diff open")
	}
	if got != want {
		t.Errorf("contextLinkText = %q, want %q", got, want)
	}
	if _, err := model.ParseLink(got); err != nil {
		t.Fatalf("ParseLink(%q) = %v", got, err)
	}
}

// A commit's files view row (a commit-files tree entry) is the fourth
// producer surface: focusedBookmark resolves it via m.filesView +
// m.filesTreeFocused + m.filesHash (files_view.go's lineHash fallback).
func TestCommitFilesViewLinkRow(t *testing.T) {
	t.Parallel()
	const sha = "0123456789abcdef0123456789abcdef01234567"
	m := diffModel()
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/repo"
	m.filesView = &contentPopup{lines: []contentLine{{text: "dir/f.go", path: "dir/f.go"}}}
	m.filesTreeFocused = true
	m.filesHash = sha

	want := "gg://gigagit/dir/f.go@" + sha
	got, ok := m.contextLinkText()
	if !ok {
		t.Fatal("no link for a commit's files view row")
	}
	if got != want {
		t.Errorf("contextLinkText = %q, want %q", got, want)
	}
	r, ok := m.contextLinkRow()
	if !ok || r.copyText != want {
		t.Errorf("contextLinkRow = %+v ok=%v, want copyText %q", r, ok, want)
	}
}

// Controller ruling P23: m.diffLayer()/diffNoteAddress() search the WHOLE
// layer stack, not just the top, so contextLinkText must not consult them
// while a stack surface (history/blame) owns the keyboard above the diff —
// exactly the precedence contextCopyRows already documents ("the stack
// surface out-ranks the diff view").
func TestContextLinkTextHistoryOverDiffPrefersHistory(t *testing.T) {
	t.Parallel()
	const sha = "eb759989a1b2c3d4e5f60718293a4b5c6d7e8f90"
	m := openedDiffModel(12, sameRowsTUI(40, 20, 30), []int{20, 30})
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/repo"
	dv := m.diffLayer()
	dv.title = "diff-under.go"
	dv.noteAddr = model.FileAddress{State: model.StateUnstaged, Path: "diff-under.go", Worktree: "/repo"}

	hv := newHistoryView(navContext{path: "hist.go", rev: sha})
	hv.commits = []model.FileCommit{{Commit: model.Commit{Hash: sha}, Path: "hist.go"}}
	hv.sel = 0
	m = m.pushLayer(hv)

	want := "gg://gigagit/hist.go@" + sha
	got, ok := m.contextLinkText()
	if !ok {
		t.Fatal("contextLinkText refused with history pushed over a diff")
	}
	if got != want {
		t.Errorf("contextLinkText = %q, want %q (the history surface's commit, not the diff underneath)", got, want)
	}
}

// Controller ruling P23: a two-sided compare view (diffView.compare, no note
// address) sits on top; the Commits panel underneath must not leak through
// as a fallback just because the compare view itself has no link to offer.
func TestContextLinkTextCompareViewBlocksCommitsFallback(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/repo"
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "eb759989a1b2c3d4e5f60718293a4b5c6d7e8f90", Subject: "x"}}
	m.sel[panelCommits] = 0
	m = m.pushLayer(&diffView{title: "a ↔ b", compare: true})

	if got, ok := m.contextLinkText(); ok {
		t.Errorf("contextLinkText = %q, want a refusal — a compare view on top must block the Commits fallback underneath", got)
	}
	if r, ok := m.contextLinkRow(); ok {
		t.Errorf("contextLinkRow = %+v, want no row (row absent) while the compare view is on top", r)
	}
}

// Menu placement (minor fix): "Copy link" sits beside the other copy rows,
// not appended after every row/window binding. On Files/Staged it lands
// right after "copy-file-path"; on Commits — which has no file-path row —
// right after "copy-commit-title" so the id/title pair stays together.
func TestCopyLinkRowSitsBesideTheOtherCopyRows(t *testing.T) {
	t.Parallel()

	t.Run("files panel", func(t *testing.T) {
		t.Parallel()
		m := diffModel()
		m.linkRepoName = "gigagit"
		m.currentWorktree = "/repo"
		rows := availableActions(m)
		wantOrder(t, rows, "copy-file-path", "copy-link")
	})

	t.Run("commits panel", func(t *testing.T) {
		t.Parallel()
		m := footerModel()
		m.linkRepoName = "gigagit"
		m.loading = false
		m.focus = panelCommits
		m.commits = []model.Commit{{Hash: "eb759989a1b2c3d4e5f60718293a4b5c6d7e8f90", Subject: "x"}}
		rows := availableActions(m)
		wantOrder(t, rows, "copy-commit-title", "copy-link")
	})
}

// wantOrder asserts b sits immediately after a in rows.
func wantOrder(t *testing.T, rows []actionRow, a, b string) {
	t.Helper()
	ai, bi := -1, -1
	for i, r := range rows {
		if r.id == a {
			ai = i
		}
		if r.id == b {
			bi = i
		}
	}
	if ai < 0 {
		t.Fatalf("rows = %v, missing row %q", rows, a)
	}
	if bi != ai+1 {
		t.Errorf("rows = %v, want %q immediately after %q (at %d), got %q at %d", rows, b, a, ai, b, bi)
	}
}
