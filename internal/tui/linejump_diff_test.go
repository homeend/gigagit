package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

func TestNewerSide(t *testing.T) {
	t.Parallel()
	commit, err := model.CommitEndpoint("0123456789abcdef0123456789abcdef01234567")
	if err != nil {
		t.Fatal(err)
	}
	wt, idx := model.WorkTreeEndpoint(), model.IndexEndpoint()
	cases := []struct {
		name string
		v    *diffView
		want fileSource
		ok   bool
	}{
		{"unstaged status diff", &diffView{title: "a.go", noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "a.go"}}, fileSource{kind: srcWorktree}, true},
		{"untracked status diff", &diffView{title: "a.go", noteAddr: model.FileAddress{State: model.StateUntracked, Path: "a.go"}}, fileSource{kind: srcWorktree}, true},
		{"staged status diff", &diffView{title: "a.go", noteAddr: model.FileAddress{State: model.StateStaged, Path: "a.go"}}, fileSource{}, false},
		{"commit diff", &diffView{title: "a.go", rev: "abc", noteAddr: model.FileAddress{State: model.StateCommitted, Commit: "abc", Path: "a.go"}}, fileSource{kind: srcCommit, rev: "abc"}, true},
		{"history diff (rev only)", &diffView{title: "a.go", rev: "def"}, fileSource{kind: srcCommit, rev: "def"}, true},
		{"compare to the working tree", &diffView{title: "a.go", cmp: &compareStamp{left: commit, right: wt, path: "a.go"}}, fileSource{kind: srcWorktree}, true},
		{"compare to a commit", &diffView{title: "a.go", cmp: &compareStamp{left: wt, right: commit, path: "a.go"}}, fileSource{kind: srcCommit, rev: commit.Hash()}, true},
		{"compare to the index", &diffView{title: "a.go", cmp: &compareStamp{left: commit, right: idx, path: "a.go"}}, fileSource{}, false},
		{"shelf → working tree", &diffView{title: "a.go"}, fileSource{}, false},
		{"bookmark ↔ bookmark", &diffView{title: "a ↔ b", compare: true, rev: "abc"}, fileSource{}, false},
	}
	for _, c := range cases {
		got, ok := c.v.newerSide()
		if ok != c.ok || got != c.want {
			t.Errorf("%s: (%+v, %v), want (%+v, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

// jumpDiffModel puts v (rebuilt) on a diff model's stack.
func jumpDiffModel(v *diffView) Model {
	v.rebuild()
	m := diffModel()
	m.height, m.width = 24, 120
	return m.pushLayer(v)
}

func diffKeys(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		if k == "enter" {
			msg = enterKey
		} else {
			msg = runeKey(k)
		}
		u, _ := m.Update(msg)
		m = u.(Model)
	}
	return m
}

func cursorRight(t *testing.T, v *diffView) int {
	t.Helper()
	r, ok := v.cursorRow()
	if !ok {
		t.Fatal("no cursor row")
	}
	return r.RightNo
}

func TestJumpDiffShownInPlace(t *testing.T) {
	t.Parallel()
	rows := sameRowsTUI(40, 20)
	v := &diffView{title: "mod.txt", full: rows, fullBlocks: blocksOf(rows), noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "mod.txt"}}
	m := diffKeys(t, jumpDiffModel(v), ":", "3", "0", "enter")
	if m.topLayer() != v {
		t.Fatalf("a shown line must not open anything: top %T", m.topLayer())
	}
	if got := cursorRight(t, v); got != 30 {
		t.Fatalf("cursor on new line %d, want 30", got)
	}
	if v.jump.typing {
		t.Fatal("enter must close the prompt")
	}
}

func TestJumpDiffHiddenOpensWorktreeFile(t *testing.T) {
	t.Parallel()
	rows := sameRowsTUI(40, 20)
	v := &diffView{title: "mod.txt", full: rows, fullBlocks: blocksOf(rows), partial: true, noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "mod.txt"}}
	m := diffKeys(t, jumpDiffModel(v), ":", "5", "enter")
	fv, ok := m.topLayer().(*fileViewer)
	if !ok {
		t.Fatalf("top = %T, want the file viewer", m.topLayer())
	}
	if fv.path != "mod.txt" || fv.src.kind != srcWorktree || fv.pendingLine != 5 {
		t.Fatalf("viewer %q %+v line %d", fv.path, fv.src, fv.pendingLine)
	}
}

func TestJumpDiffHiddenOpensCommitFile(t *testing.T) {
	t.Parallel()
	rows := sameRowsTUI(40, 20)
	v := &diffView{title: "mod.txt", rev: "abc", full: rows, fullBlocks: blocksOf(rows), partial: true, noteAddr: model.FileAddress{State: model.StateCommitted, Commit: "abc", Path: "mod.txt"}}
	m := diffKeys(t, jumpDiffModel(v), ":", "5", "enter")
	fv, ok := m.topLayer().(*fileViewer)
	if !ok {
		t.Fatalf("top = %T, want the file viewer", m.topLayer())
	}
	if fv.src != (fileSource{kind: srcCommit, rev: "abc"}) || fv.pendingLine != 5 {
		t.Fatalf("viewer %+v line %d", fv.src, fv.pendingLine)
	}
}

func TestJumpDiffHiddenStagedUnfolds(t *testing.T) {
	t.Parallel()
	rows := sameRowsTUI(40, 20)
	v := &diffView{title: "mod.txt", full: rows, fullBlocks: blocksOf(rows), partial: true, noteAddr: model.FileAddress{State: model.StateStaged, Path: "mod.txt"}}
	m := diffKeys(t, jumpDiffModel(v), ":", "5", "enter")
	if m.topLayer() != v {
		t.Fatalf("a staged diff has no file to open: top %T", m.topLayer())
	}
	if v.partial {
		t.Fatal("the fold hiding line 5 must open")
	}
	if got := cursorRight(t, v); got != 5 {
		t.Fatalf("cursor on new line %d, want 5", got)
	}
}

func TestJumpDiffPastEndClamps(t *testing.T) {
	t.Parallel()
	rows := sameRowsTUI(40, 20)
	v := &diffView{title: "mod.txt", full: rows, fullBlocks: blocksOf(rows), partial: true, noteAddr: model.FileAddress{State: model.StateStaged, Path: "mod.txt"}}
	m := diffKeys(t, jumpDiffModel(v), ":", "9", "9", "enter")
	if got := cursorRight(t, v); got != 40 {
		t.Fatalf("cursor on new line %d, want 40 (the last)", got)
	}
	if m.diffNotice == "" {
		t.Fatal("a clamped jump says so")
	}
}

func TestJumpDiffDeletedFileUsesOld(t *testing.T) {
	t.Parallel()
	var rows []textdiff.Row
	for i := 1; i <= 10; i++ {
		rows = append(rows, textdiff.Row{Kind: textdiff.Del, Left: "x", LeftNo: i})
	}
	v := &diffView{title: "gone.txt", full: rows, fullBlocks: []int{0}, noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "gone.txt"}}
	m := diffKeys(t, jumpDiffModel(v), ":", "3", "enter")
	r, ok := v.cursorRow()
	if !ok || r.LeftNo != 3 || m.topLayer() != v {
		t.Fatalf("cursor row %+v top %T, want old line 3 in place", r, m.topLayer())
	}
	if !v.onOld {
		t.Fatal("the cursor lands on the side that has the line")
	}
}

func TestJumpDiffNotWhileLoading(t *testing.T) {
	t.Parallel()
	v := &diffView{title: "mod.txt", loading: true}
	m := diffModel()
	m = m.pushLayer(v)
	diffKeys(t, m, ":")
	if v.jump.typing {
		t.Fatal(": opened a prompt over a loading diff")
	}
}

func TestJumpColonIsDiffSearchText(t *testing.T) {
	t.Parallel()
	rows := sameRowsTUI(40, 20)
	v := &diffView{title: "mod.txt", full: rows, fullBlocks: blocksOf(rows)}
	diffKeys(t, jumpDiffModel(v), "/", ":")
	if v.search.query != ":" || v.jump.typing {
		t.Fatalf("query %q jump %v", v.search.query, v.jump.typing)
	}
}

func TestJumpStackedTargetsCursorFile(t *testing.T) {
	t.Parallel()
	m, v := navStackModel(t, sameRowsTUI(10, 2), sameRowsTUI(10, 2))
	lo, hi := v.fileLineRange(1)
	v.setCursorLine(hi, m.diffBodyRows())
	m = diffKeys(t, m, ":", "7", "enter")
	if v.curLine < lo || v.curLine > hi {
		t.Fatalf("cursor line %d left file 1 [%d,%d]", v.curLine, lo, hi)
	}
	if got := cursorRight(t, v); got != 7 {
		t.Fatalf("cursor on new line %d, want 7", got)
	}
}

func TestJumpStackedUnloadedParks(t *testing.T) {
	t.Parallel()
	m, v := navStackModel(t, sameRowsTUI(10, 2), nil)
	v.setCursorLine(v.stk.files[1].hdr, m.diffBodyRows())
	diffKeys(t, m, ":", "3", "enter")
	if v.stk.jumpLand == nil || v.stk.jumpLand.file != 1 || v.stk.jumpLand.no != 3 {
		t.Fatalf("jumpLand = %+v, want file 1 line 3 parked", v.stk.jumpLand)
	}
}

// The parked jump lands when the file's rows arrive — with no notes address
// to wait for (a compare stack has none), and inside the arriving file.
func TestJumpStackedUnloadedLandsOnArrival(t *testing.T) {
	t.Parallel()
	m, v := navStackModel(t, sameRowsTUI(10, 2), nil)
	v.setCursorLine(v.stk.files[1].hdr, m.diffBodyRows())
	m = diffKeys(t, m, ":", "7", "enter")
	rows := sameRowsTUI(10, 4)
	u, _ := m.Update(stackFileMsg{gen: v.stk.gen, idx: 1, view: diffViewWith(rows, blocksOf(rows))})
	v = u.(Model).diffLayer()
	lo, hi := v.fileLineRange(1)
	if v.curLine < lo || v.curLine > hi || cursorRight(t, v) != 7 {
		t.Fatalf("cursor line %d (file 1 is [%d,%d]), want its line 7", v.curLine, lo, hi)
	}
	if v.stk.jumpLand != nil {
		t.Fatal("the landing is consumed")
	}
}

// Any other key abandons a parked jump: a late file never yanks the cursor.
func TestJumpStackedParkAbandonedByAKey(t *testing.T) {
	t.Parallel()
	m, v := navStackModel(t, sameRowsTUI(10, 2), nil)
	v.setCursorLine(v.stk.files[1].hdr, m.diffBodyRows())
	m = diffKeys(t, m, ":", "7", "enter")
	m = diffKeys(t, m, "k")
	if v.stk.jumpLand != nil {
		t.Fatal("k must abandon the parked jump")
	}
}

// Full mode has no trailing fold: a number past the end still clamps.
func TestJumpDiffPastEndFullClamps(t *testing.T) {
	t.Parallel()
	rows := sameRowsTUI(40, 20)
	v := &diffView{title: "mod.txt", full: rows, fullBlocks: blocksOf(rows), noteAddr: model.FileAddress{State: model.StateStaged, Path: "mod.txt"}}
	m := diffKeys(t, jumpDiffModel(v), ":", "9", "9", "enter")
	if got := cursorRight(t, v); got != 40 {
		t.Fatalf("cursor on new line %d, want 40 (the last)", got)
	}
	if m.diffNotice == "" {
		t.Fatal("a clamped jump says so")
	}
}

// [:] line took [f] part's place in the footer; f stays in the . menu.
func TestDiffPartialMenuRow(t *testing.T) {
	t.Parallel()
	rows := sameRowsTUI(40, 20)
	v := &diffView{title: "mod.txt", full: rows, fullBlocks: blocksOf(rows)}
	m := jumpDiffModel(v)
	var row *actionRow
	for _, r := range availableActions(m) {
		if r.id == "diff-partial" {
			row = &r
		}
	}
	if row == nil || row.key != "f" {
		t.Fatalf("no f row in the diff's . menu: %+v", row)
	}
	row.run(m)
	if !v.partial {
		t.Fatal("the row flips to the changed lines only")
	}
	for _, stacked := range []bool{false, true} {
		if !strings.Contains(diffHintFor(longScroll, stacked, false), "[:] line") {
			t.Errorf("stacked=%v footer lacks [:] line", stacked)
		}
	}
}
