package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// treeDiffModel opens a diff over a files-view tree of three files (a/b/c.go,
// under one heading), with the tree selection on the row sel indexes and an
// EMPTY diff (offset 0 — at both top and bottom, so Home/End step files at once).
func treeDiffModel(sel int) Model {
	m := footerModel()
	m.focus = panelCommits
	m.filesHash = "abc"
	m.filesTitle = "Files abc subj"
	m.filesTreeFocused = true
	m.filesView = &contentPopup{lines: []contentLine{
		{text: "dir/", heading: true},
		{text: "  M a.go", path: "a.go", status: "M"},
		{text: "  M b.go", path: "b.go", status: "M"},
		{text: "  M c.go", path: "c.go", status: "M"},
	}, sel: sel}
	m.diffNav = diffNavTree
	m.diffView = &diffView{}
	m.diffTag = "commit:abc:start"
	return m
}

// statusDiffModelMulti opens a diff over a Status panel of three modified files,
// selection on a.txt, with an EMPTY diff (at top/bottom).
func statusDiffModelMulti() Model {
	m := footerModel()
	m.focus = panelFiles
	m.status.Files = []model.FileStatus{
		{Path: "a.txt", Unstaged: 'M'},
		{Path: "b.txt", Unstaged: 'M'},
		{Path: "c.txt", Unstaged: 'M'},
	}
	m.sel[panelFiles] = 0
	m.diffNav = diffNavStatus
	m.diffView = &diffView{}
	m.diffTag = "status:a.txt"
	return m
}

func TestNextFileRowSkipsHeadings(t *testing.T) {
	vis := []contentLine{
		{text: "dir/", heading: true},
		{path: "a.go"},
		{path: "b.go"},
	}
	if got := nextFileRow(vis, 1, 1); got != 2 {
		t.Fatalf("forward from a.go = %d, want 2 (b.go)", got)
	}
	if got := nextFileRow(vis, 1, -1); got != -1 {
		t.Fatalf("back from a.go must skip the heading and clamp, got %d", got)
	}
	if got := nextFileRow(vis, 2, 1); got != -1 {
		t.Fatalf("forward from the last file must clamp, got %d", got)
	}
}

func TestDiffEndAtBottomStepsNextFileTree(t *testing.T) {
	m := treeDiffModel(1) // on a.go, empty diff (already at the bottom)
	u, cmd := m.Update(keyMsg("end"))
	mm := u.(Model)
	if mm.filesView.sel != 2 {
		t.Fatalf("end at bottom must select b.go (sel 2), got %d", mm.filesView.sel)
	}
	if mm.diffTag != "commit:abc:b.go" {
		t.Fatalf("diffTag = %q, want commit:abc:b.go", mm.diffTag)
	}
	if cmd == nil {
		t.Fatal("stepping must return the loader cmd")
	}
}

func TestDiffHomeAtTopStepsPrevFileTree(t *testing.T) {
	m := treeDiffModel(2) // on b.go, empty diff (already at the top)
	u, cmd := m.Update(keyMsg("home"))
	mm := u.(Model)
	if mm.filesView.sel != 1 {
		t.Fatalf("home at top must select a.go (sel 1), got %d", mm.filesView.sel)
	}
	if mm.diffTag != "commit:abc:a.go" {
		t.Fatalf("diffTag = %q, want commit:abc:a.go", mm.diffTag)
	}
	if cmd == nil {
		t.Fatal("stepping must return the loader cmd")
	}
}

func TestDiffEndScrollsToBottomBeforeStepping(t *testing.T) {
	m := treeDiffModel(1)
	m.height = 12
	v := diffViewWith(sameRowsTUI(40, 20), []int{20})
	v.offset = 0 // tall diff, at the top
	m.diffView = v
	maxOff := len(v.disp) - m.diffBodyRows()

	u, _ := m.Update(keyMsg("end")) // first end: scroll to the bottom only
	mm := u.(Model)
	if mm.diffView.offset != maxOff {
		t.Fatalf("first end offset = %d, want bottom %d", mm.diffView.offset, maxOff)
	}
	if mm.diffTag != "commit:abc:start" || mm.filesView.sel != 1 {
		t.Fatal("first end must not change the file")
	}

	u2, cmd := mm.Update(keyMsg("end")) // second end: now at bottom → next file
	mm2 := u2.(Model)
	if mm2.filesView.sel != 2 || mm2.diffTag != "commit:abc:b.go" {
		t.Fatalf("second end must step to b.go, sel=%d tag=%q", mm2.filesView.sel, mm2.diffTag)
	}
	if cmd == nil {
		t.Fatal("step must return the loader cmd")
	}
}

func TestDiffHomeScrollsToTopBeforeStepping(t *testing.T) {
	m := treeDiffModel(2)
	m.height = 12
	v := diffViewWith(sameRowsTUI(40, 20), []int{20})
	m.diffView = v
	v.offset = len(v.disp) - m.diffBodyRows() // at the bottom

	u, _ := m.Update(keyMsg("home")) // first home: scroll to the top only
	mm := u.(Model)
	if mm.diffView.offset != 0 {
		t.Fatalf("first home must scroll to top, offset = %d", mm.diffView.offset)
	}
	if mm.diffTag != "commit:abc:start" || mm.filesView.sel != 2 {
		t.Fatal("first home must not change the file")
	}

	u2, _ := mm.Update(keyMsg("home")) // second home: now at top → previous file
	mm2 := u2.(Model)
	if mm2.filesView.sel != 1 || mm2.diffTag != "commit:abc:a.go" {
		t.Fatalf("second home must step to a.go, sel=%d tag=%q", mm2.filesView.sel, mm2.diffTag)
	}
}

func TestDiffEndStepClampsAtLastFileTree(t *testing.T) {
	m := treeDiffModel(3) // on c.go (last), empty diff
	u, cmd := m.Update(keyMsg("end"))
	mm := u.(Model)
	if mm.filesView.sel != 3 || mm.diffTag != "commit:abc:start" {
		t.Fatalf("end at the last file must be a no-op, sel=%d tag=%q", mm.filesView.sel, mm.diffTag)
	}
	if cmd != nil {
		t.Fatal("no step → no cmd")
	}
}

func TestDiffEndStepsNextFileStatus(t *testing.T) {
	m := statusDiffModelMulti()
	u, cmd := m.Update(keyMsg("end"))
	mm := u.(Model)
	if mm.sel[panelFiles] != 1 || mm.diffTag != "status:b.txt" {
		t.Fatalf("end must step to b.txt, sel=%d tag=%q", mm.sel[panelFiles], mm.diffTag)
	}
	if cmd == nil {
		t.Fatal("step must return the loader cmd")
	}
}

func TestDiffHomeStepsPrevFileStatus(t *testing.T) {
	m := statusDiffModelMulti()
	m.sel[panelFiles] = 2 // on c.txt
	m.diffTag = "status:c.txt"
	u, _ := m.Update(keyMsg("home"))
	mm := u.(Model)
	if mm.sel[panelFiles] != 1 || mm.diffTag != "status:b.txt" {
		t.Fatalf("home must step to b.txt, sel=%d tag=%q", mm.sel[panelFiles], mm.diffTag)
	}
}

func TestDiffStatusStepSkipsUnmerged(t *testing.T) {
	m := statusDiffModelMulti()
	m.status.Files = []model.FileStatus{
		{Path: "a.txt", Unstaged: 'M'},
		{Path: "conflict.txt", Staged: 'U', Unstaged: 'U', Kind: model.KindUnmerged},
		{Path: "b.txt", Unstaged: 'M'},
	}
	m.sel[panelFiles] = 0 // a.txt
	u, _ := m.Update(keyMsg("end"))
	mm := u.(Model)
	if mm.sel[panelFiles] != 2 || mm.diffTag != "status:b.txt" {
		t.Fatalf("end must skip the conflicted row to b.txt, sel=%d tag=%q", mm.sel[panelFiles], mm.diffTag)
	}
}

func TestDiffStagedStepsNextFileStaged(t *testing.T) {
	m := footerModel()
	m.focus = panelStaged
	m.status.Files = []model.FileStatus{
		{Path: "a.txt", Staged: 'M', Unstaged: '.'},
		{Path: "b.txt", Staged: 'M', Unstaged: '.'},
	}
	m.sel[panelStaged] = 0
	m.diffNav = diffNavStaged
	m.diffView = &diffView{}
	m.diffTag = "staged:a.txt"
	u, _ := m.Update(keyMsg("end"))
	mm := u.(Model)
	if mm.sel[panelStaged] != 1 || mm.diffTag != "staged:b.txt" {
		t.Fatalf("end must step to staged b.txt, sel=%d tag=%q", mm.sel[panelStaged], mm.diffTag)
	}
}

func TestDiffFileStepInertWhenNoSource(t *testing.T) {
	m := footerModel()
	m.diffNav = diffNavNone
	m.diffView = &diffView{} // empty: at top and bottom
	m.diffTag = "bookmark2:x:y"
	if u, cmd := m.Update(keyMsg("end")); u.(Model).diffTag != "bookmark2:x:y" || cmd != nil {
		t.Fatal("end with no source list must be a no-op")
	}
	if u, cmd := m.Update(keyMsg("home")); u.(Model).diffTag != "bookmark2:x:y" || cmd != nil {
		t.Fatal("home with no source list must be a no-op")
	}
}

func TestEnterSetsDiffNavTree(t *testing.T) {
	m := filesViewModel() // sel 1 = a real file row
	u, _ := m.Update(keyMsg("enter"))
	if u.(Model).diffNav != diffNavTree {
		t.Fatalf("enter from the files tree must set diffNav=tree, got %d", u.(Model).diffNav)
	}
}

func TestEnterSetsDiffNavStatus(t *testing.T) {
	m := diffModel() // panelFiles, sel 0 = mod.txt
	u, _ := m.Update(keyMsg("enter"))
	if u.(Model).diffNav != diffNavStatus {
		t.Fatalf("enter from the Status panel must set diffNav=status, got %d", u.(Model).diffNav)
	}
}

func TestOpenPickerDiffSetsDiffNavNone(t *testing.T) {
	m := footerModel()
	m.diffNav = diffNavTree // stale from a previous open
	mm, _ := m.openPickerDiff(&diffView{}, "tag", nil)
	if mm.diffNav != diffNavNone {
		t.Fatalf("openPickerDiff must reset diffNav to none, got %d", mm.diffNav)
	}
}
