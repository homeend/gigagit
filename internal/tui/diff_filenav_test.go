package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gigagit/gg/internal/model"
)

// treeDiffModel opens a diff over a files-view tree of three files (a/b/c.go,
// under one heading), with the tree selection on the row sel indexes and an
// EMPTY diff (offset 0 — at both top and bottom, so the first home/end primes).
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

// TestDiffEndPrimesThenStepsTree: at the bottom, the first End primes (cue, no
// nav); the second End steps to the next file with an arrival notice.
func TestDiffEndPrimesThenStepsTree(t *testing.T) {
	m := treeDiffModel(1) // on a.go, empty diff (at the bottom)

	u, cmd := m.Update(keyMsg("end")) // first End: prime
	mm := u.(Model)
	if mm.diffView.fileArm != fileArmNext {
		t.Fatalf("first end must prime fileArmNext, got %d", mm.diffView.fileArm)
	}
	if mm.filesView.sel != 1 || mm.diffTag != "commit:abc:start" || cmd != nil {
		t.Fatal("first end must not navigate")
	}
	if mm.diffNotice != "" {
		t.Fatalf("priming shows the cue (from fileArm), not a notice; got %q", mm.diffNotice)
	}

	u2, cmd2 := mm.Update(keyMsg("end")) // second End: step
	mm2 := u2.(Model)
	if mm2.filesView.sel != 2 || mm2.diffTag != "commit:abc:b.go" {
		t.Fatalf("second end must step to b.go, sel=%d tag=%q", mm2.filesView.sel, mm2.diffTag)
	}
	if cmd2 == nil {
		t.Fatal("stepping must return the loader cmd")
	}
	if !strings.Contains(mm2.diffNotice, "b.go") {
		t.Fatalf("arrival notice must name the new file, got %q", mm2.diffNotice)
	}
	if mm2.diffView.fileArm != fileArmNone {
		t.Fatal("the freshly opened file must not be pre-armed")
	}
}

// TestDiffHomePrimesThenStepsTree mirrors End for the previous file.
func TestDiffHomePrimesThenStepsTree(t *testing.T) {
	m := treeDiffModel(2) // on b.go, empty diff (at the top)

	u, _ := m.Update(keyMsg("home")) // first Home: prime
	mm := u.(Model)
	if mm.diffView.fileArm != fileArmPrev {
		t.Fatalf("first home must prime fileArmPrev, got %d", mm.diffView.fileArm)
	}
	if mm.filesView.sel != 2 {
		t.Fatal("first home must not navigate")
	}

	u2, _ := mm.Update(keyMsg("home")) // second Home: step
	mm2 := u2.(Model)
	if mm2.filesView.sel != 1 || mm2.diffTag != "commit:abc:a.go" {
		t.Fatalf("second home must step to a.go, sel=%d tag=%q", mm2.filesView.sel, mm2.diffTag)
	}
	if !strings.Contains(mm2.diffNotice, "a.go") {
		t.Fatalf("arrival notice must name the new file, got %q", mm2.diffNotice)
	}
}

// TestDiffEndScrollsThenPrimesThenSteps: a tall diff at the top needs THREE End
// presses — scroll to bottom, prime, step.
func TestDiffEndScrollsThenPrimesThenSteps(t *testing.T) {
	m := treeDiffModel(1)
	m.height = 12
	v := diffViewWith(sameRowsTUI(40, 20), []int{20})
	v.offset = 0 // tall diff, at the top
	m.diffView = v
	maxOff := len(v.disp) - m.diffBodyRows()

	u, _ := m.Update(keyMsg("end")) // 1: scroll to bottom
	mm := u.(Model)
	if mm.diffView.offset != maxOff || mm.diffView.fileArm != fileArmNone {
		t.Fatalf("first end scrolls only: offset=%d arm=%d", mm.diffView.offset, mm.diffView.fileArm)
	}
	if mm.diffTag != "commit:abc:start" {
		t.Fatal("first end must not change the file")
	}

	u2, _ := mm.Update(keyMsg("end")) // 2: prime
	mm2 := u2.(Model)
	if mm2.diffView.fileArm != fileArmNext || mm2.diffTag != "commit:abc:start" {
		t.Fatalf("second end primes only: arm=%d tag=%q", mm2.diffView.fileArm, mm2.diffTag)
	}

	u3, cmd := mm2.Update(keyMsg("end")) // 3: step
	mm3 := u3.(Model)
	if mm3.filesView.sel != 2 || mm3.diffTag != "commit:abc:b.go" || cmd == nil {
		t.Fatalf("third end steps to b.go, sel=%d tag=%q", mm3.filesView.sel, mm3.diffTag)
	}
}

// TestDiffEndAtLastFileNotice: at the bottom of the last file there is nothing
// to prime — a "no next file" notice shows instead, and nothing navigates.
func TestDiffEndAtLastFileNotice(t *testing.T) {
	m := treeDiffModel(3) // on c.go (last), empty diff
	u, cmd := m.Update(keyMsg("end"))
	mm := u.(Model)
	if mm.diffView.fileArm != fileArmNone {
		t.Fatal("no next file → must not prime")
	}
	if mm.filesView.sel != 3 || mm.diffTag != "commit:abc:start" || cmd != nil {
		t.Fatal("no next file → no navigation")
	}
	if mm.diffNotice != "▸ no next file" {
		t.Fatalf("notice = %q, want '▸ no next file'", mm.diffNotice)
	}
}

func TestDiffHomeAtFirstFileNotice(t *testing.T) {
	m := treeDiffModel(1) // on a.go (first), empty diff
	u, _ := m.Update(keyMsg("home"))
	mm := u.(Model)
	if mm.diffNotice != "▸ no previous file" {
		t.Fatalf("notice = %q, want '▸ no previous file'", mm.diffNotice)
	}
}

func TestDiffEndPrimesThenStepsStatus(t *testing.T) {
	m := statusDiffModelMulti()
	u, _ := m.Update(keyMsg("end")) // prime
	mm := u.(Model)
	if mm.diffView.fileArm != fileArmNext || mm.diffTag != "status:a.txt" {
		t.Fatalf("first end primes: arm=%d tag=%q", mm.diffView.fileArm, mm.diffTag)
	}
	u2, _ := mm.Update(keyMsg("end")) // step
	mm2 := u2.(Model)
	if mm2.sel[panelFiles] != 1 || mm2.diffTag != "status:b.txt" {
		t.Fatalf("second end steps to b.txt, sel=%d tag=%q", mm2.sel[panelFiles], mm2.diffTag)
	}
	if !strings.Contains(mm2.diffNotice, "b.txt") {
		t.Fatalf("arrival notice must name b.txt, got %q", mm2.diffNotice)
	}
}

func TestDiffStepStatusSkipsUnmerged(t *testing.T) {
	m := statusDiffModelMulti()
	m.status.Files = []model.FileStatus{
		{Path: "a.txt", Unstaged: 'M'},
		{Path: "conflict.txt", Staged: 'U', Unstaged: 'U', Kind: model.KindUnmerged},
		{Path: "b.txt", Unstaged: 'M'},
	}
	m.sel[panelFiles] = 0 // a.txt
	u, _ := m.Update(keyMsg("end"))
	mm := u.(Model) // prime (peek skips conflict → b.txt)
	if mm.diffView.fileArm != fileArmNext {
		t.Fatal("must prime: a diffable next file exists past the conflict")
	}
	u2, _ := mm.Update(keyMsg("end"))
	mm2 := u2.(Model)
	if mm2.sel[panelFiles] != 2 || mm2.diffTag != "status:b.txt" {
		t.Fatalf("must skip the conflicted row to b.txt, sel=%d tag=%q", mm2.sel[panelFiles], mm2.diffTag)
	}
}

func TestDiffStepStaged(t *testing.T) {
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
	u, _ := m.Update(keyMsg("end")) // prime
	u2, _ := u.(Model).Update(keyMsg("end"))
	mm := u2.(Model)
	if mm.sel[panelStaged] != 1 || mm.diffTag != "staged:b.txt" {
		t.Fatalf("staged step to b.txt, sel=%d tag=%q", mm.sel[panelStaged], mm.diffTag)
	}
}

func TestDiffFileNavInertWhenNoSource(t *testing.T) {
	m := footerModel()
	m.diffNav = diffNavNone
	m.diffView = &diffView{} // empty: at top and bottom
	m.diffTag = "bookmark2:x:y"
	for _, k := range []string{"end", "home"} {
		u, cmd := m.Update(keyMsg(k))
		mm := u.(Model)
		if mm.diffTag != "bookmark2:x:y" || cmd != nil {
			t.Fatalf("%s with no source list must not navigate", k)
		}
		if mm.diffView.fileArm != fileArmNone || mm.diffNotice != "" {
			t.Fatalf("%s with no source list must not prime or notice", k)
		}
	}
}

// TestDiffArmClearedByOtherKey: a primed step is cancelled by any other key.
func TestDiffArmClearedByOtherKey(t *testing.T) {
	m := treeDiffModel(1)
	u, _ := m.Update(keyMsg("end")) // prime
	if u.(Model).diffView.fileArm != fileArmNext {
		t.Fatal("setup: end must prime")
	}
	u2, _ := u.(Model).Update(keyMsg("j")) // any other key
	if u2.(Model).diffView.fileArm != fileArmNone {
		t.Fatal("j must cancel the primed file-step")
	}
}

// TestDiffNoticeClearedOnNextKey: the arrival notice is transient.
func TestDiffNoticeClearedOnNextKey(t *testing.T) {
	m := treeDiffModel(1)
	u, _ := m.Update(keyMsg("end"))          // prime
	u2, _ := u.(Model).Update(keyMsg("end")) // step → notice set
	if !strings.Contains(u2.(Model).diffNotice, "b.go") {
		t.Fatal("setup: step must post a notice")
	}
	u3, _ := u2.(Model).Update(keyMsg("j")) // next key
	if u3.(Model).diffNotice != "" {
		t.Fatalf("notice must clear on the next key, got %q", u3.(Model).diffNotice)
	}
}

// TestDiffNoticeSurvivesAsyncLoad: the arrival notice must persist across the
// diffMsg that replaces the loading diffView a moment later (diffNotice is a
// Model field, not on diffView) — otherwise it would flash and vanish on load.
func TestDiffNoticeSurvivesAsyncLoad(t *testing.T) {
	m := treeDiffModel(1)
	u, _ := m.Update(keyMsg("end"))          // prime
	u2, _ := u.(Model).Update(keyMsg("end")) // step → loading view + notice
	mm := u2.(Model)
	if !strings.Contains(mm.diffNotice, "b.go") {
		t.Fatal("setup: step must post a notice")
	}
	// The loader's result lands, replacing the loading view (tag must match).
	u3, _ := mm.Update(diffMsg{tag: mm.diffTag, view: &diffView{}})
	if !strings.Contains(u3.(Model).diffNotice, "b.go") {
		t.Fatalf("notice must survive the diffMsg load, got %q", u3.(Model).diffNotice)
	}
}

// TestOpenDiffClearsStaleNotice: opening a diff (enter) drops a leftover notice.
func TestOpenDiffClearsStaleNotice(t *testing.T) {
	m := filesViewModel()
	m.diffNotice = "▸ stale.go"
	u, _ := m.Update(keyMsg("enter"))
	if u.(Model).diffNotice != "" {
		t.Fatalf("enter must clear the stale notice, got %q", u.(Model).diffNotice)
	}
}

// TestWithDiffFileNoticeRendersCueAndNotice: the bottom-left overlay shows the
// primed cue and the arrival notice, and is absent when idle.
func TestWithDiffFileNoticeRendersCueAndNotice(t *testing.T) {
	m := treeDiffModel(1)
	frame := m.renderDiffView()

	if out := ansi.Strip(m.withDiffFileNotice(frame)); strings.Contains(out, "next file") || strings.Contains(out, "▸") {
		t.Fatalf("idle diff must show no notice overlay:\n%s", out)
	}

	m.diffView.fileArm = fileArmNext
	if out := ansi.Strip(m.withDiffFileNotice(frame)); !strings.Contains(out, "next file") {
		t.Fatalf("primed diff must show the cue:\n%s", out)
	}

	m.diffView.fileArm = fileArmNone
	m.diffNotice = "▸ b.go"
	if out := ansi.Strip(m.withDiffFileNotice(frame)); !strings.Contains(out, "b.go") {
		t.Fatalf("notice must render the file name:\n%s", out)
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
