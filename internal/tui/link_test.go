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
	u, cmd := m.Update(keyMsg("L"))
	if cmd == nil {
		t.Fatal("L in the diff view produced no clipboard command")
	}
	if u.(Model).diffLayer() == nil {
		t.Error("L must not close the diff view")
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
