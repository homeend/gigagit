package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/shelf" // test-only import; archtest checks non-test imports
)

// pickModel is a bare model (no popup) for probe-result handling tests.
func pickModel() Model {
	return footerModel()
}

func TestPickProbeStaleGenDropped(t *testing.T) {
	m := pickModel()
	m.pickGen = 5
	mm, cmd := m.Update(pickProbeMsg{gen: 4, target: pickTarget{sha: "abc"}, found: true})
	m = mm.(Model)
	if cmd != nil || m.modal != nil {
		t.Fatalf("stale probe must be dropped (cmd=%v modal=%v)", cmd, m.modal)
	}
}

func TestPickProbeFoundOpensCherryPickModal(t *testing.T) {
	m := pickModel()
	msg := pickProbeMsg{
		gen:    m.pickGen,
		target: pickTarget{sha: "a1b2c3d4e5f6a7b8"},
		line:   model.LogLine{Hash: "a1b2c3d", Subject: "fix thing"},
		found:  true,
	}
	mm, _ := m.Update(msg)
	m = mm.(Model)
	if m.modal == nil {
		t.Fatal("found probe must open the confirm modal")
	}
	want := "Cherry-pick a1b2c3d fix thing onto main?"
	if m.modal.req.Prompt != want {
		t.Fatalf("prompt = %q, want %q", m.modal.req.Prompt, want)
	}
	res, cmd := m.modal.onResolve(m, "Cherry-pick")
	m = res.(Model)
	if !m.running || cmd == nil {
		t.Fatalf("confirming must dispatch the cherry-pick op (running=%v cmd=%v)", m.running, cmd)
	}
	if m.topLayer() != nil {
		t.Fatal("layers must be cleared before the op so conflicts land in the main view")
	}
}

func TestPickProbeCancelDoesNothing(t *testing.T) {
	m := pickModel()
	mm, _ := m.Update(pickProbeMsg{gen: m.pickGen, target: pickTarget{sha: "abc"},
		line: model.LogLine{Hash: "abc1234", Subject: "s"}, found: true})
	m = mm.(Model)
	res, cmd := m.modal.onResolve(m, "Cancel")
	m = res.(Model)
	if m.running || cmd != nil {
		t.Fatalf("Cancel must not dispatch (running=%v cmd=%v)", m.running, cmd)
	}
}

func TestPickProbeMissingWithPatchAppliesPatch(t *testing.T) {
	m := pickModel()
	st := shelf.NewFileStore(t.TempDir())
	e, err := st.PutCommit("",
		model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6a7b8"},
		[]byte("tarbytes"), []byte("From a1b2 Mon Sep 17 00:00:00 2001\nSubject: x\n"), "fix")
	if err != nil {
		t.Fatal(err)
	}
	m.svc.SetShelfStore(st)

	msg := pickProbeMsg{
		gen:    m.pickGen,
		target: pickTarget{sha: "a1b2c3d4e5f6a7b8", shelfID: e.ID, hasPatch: true},
		found:  false,
	}
	mm, _ := m.Update(msg)
	m = mm.(Model)
	if m.modal == nil || !strings.Contains(m.modal.req.Prompt, "no longer in the repo") {
		t.Fatalf("missing+patch must open the patch modal, got %+v", m.modal)
	}
	res, cmd := m.modal.onResolve(m, "Apply patch")
	m = res.(Model)
	if !m.running || cmd == nil {
		t.Fatalf("confirming must dispatch the apply op (running=%v cmd=%v)", m.running, cmd)
	}
	if m.pickPatchTemp == "" {
		t.Fatal("the temp patch path must be remembered for cleanup")
	}
	data, err := os.ReadFile(m.pickPatchTemp)
	if err != nil || !strings.HasPrefix(string(data), "From ") {
		t.Fatalf("temp patch file bad: %q, %v", data, err)
	}
	// The opFinishedMsg cleanup removes the temp file.
	tmp := m.pickPatchTemp
	mm, _ = m.Update(opFinishedMsg{})
	m = mm.(Model)
	if m.pickPatchTemp != "" {
		t.Fatal("pickPatchTemp must clear when the op finishes")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("temp file must be removed, stat err=%v", err)
	}
}

func TestPickProbeMissingNoPatchNotices(t *testing.T) {
	m := pickModel()
	// Shelf entry without a patch.
	mm, _ := m.Update(pickProbeMsg{gen: m.pickGen,
		target: pickTarget{sha: "abc", shelfID: "commit-abc-11112222", hasPatch: false}, found: false})
	m = mm.(Model)
	if m.modal != nil || !strings.Contains(m.statusMsg, "no stored patch") {
		t.Fatalf("shelf no-patch notice missing: modal=%v msg=%q", m.modal, m.statusMsg)
	}
	// Bookmark (no shelfID).
	m = pickModel()
	mm, _ = m.Update(pickProbeMsg{gen: m.pickGen, target: pickTarget{sha: "abc"}, found: false})
	m = mm.(Model)
	if m.modal != nil || !strings.Contains(m.statusMsg, "a bookmark stores no snapshot") {
		t.Fatalf("bookmark notice missing: modal=%v msg=%q", m.modal, m.statusMsg)
	}
}

func commitShelfEntry(id, sha, label string) model.ShelfEntry {
	return model.ShelfEntry{
		ID: id, Kind: model.ShelfKindCommit, Label: label,
		Origin: model.FileAddress{State: model.StateCommitted, Commit: sha},
		SHA:    id + "0000", PatchSHA: "p" + id,
	}
}

func bookmarkPopModel(items ...model.Bookmark) Model {
	m := footerModel()
	m.width, m.height = 100, 30
	m = m.pushLayer(newBookmarkPopup(items))
	return m
}

// commitBookmarkFixture builds a commit bookmark test fixture. Named
// distinctly from the production commitBookmark(c model.Commit, label string)
// in bookmark.go, which this would otherwise collide with (same package,
// different signature) — a naming gap not anticipated by the task brief.
func commitBookmarkFixture(id, sha, label string) model.Bookmark {
	return model.Bookmark{ID: id, State: model.StateCommitted, Commit: sha, Label: label}
}

func TestShelfPopupAOnCommitEntryProbes(t *testing.T) {
	m := shelfPopModel(commitShelfEntry("e1", "a1b2c3d4e5f6a7b8", "fix"))
	gen0 := m.pickGen
	mm, cmd := m.Update(keyMsg("a"))
	m = mm.(Model)
	if cmd == nil || m.pickGen != gen0+1 {
		t.Fatalf("a on a commit entry must dispatch the probe (cmd=%v gen=%d)", cmd, m.pickGen)
	}
}

func TestShelfPopupAOnFileEntryNotices(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, cmd := m.Update(keyMsg("a"))
	m = mm.(Model)
	if cmd != nil || !strings.Contains(m.statusMsg, "only for a shelved commit") {
		t.Fatalf("a on a file entry must notice (cmd=%v msg=%q)", cmd, m.statusMsg)
	}
}

func TestBookmarkPopupAOnCommitBookmarkProbes(t *testing.T) {
	m := bookmarkPopModel(commitBookmarkFixture("b1", "a1b2c3d4e5f6a7b8", "fix thing"))
	gen0 := m.pickGen
	mm, cmd := m.Update(keyMsg("a"))
	m = mm.(Model)
	if cmd == nil || m.pickGen != gen0+1 {
		t.Fatalf("a on a commit bookmark must dispatch the probe (cmd=%v gen=%d)", cmd, m.pickGen)
	}
}

func TestBookmarkPopupAOnFileBookmarkNotices(t *testing.T) {
	m := bookmarkPopModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Path: "x.go"})
	mm, cmd := m.Update(keyMsg("a"))
	m = mm.(Model)
	if cmd != nil || !strings.Contains(m.statusMsg, "only for a commit bookmark") {
		t.Fatalf("a on a file bookmark must notice (cmd=%v msg=%q)", cmd, m.statusMsg)
	}
}

func TestSwitcherEscBumpsPickGen(t *testing.T) {
	m := shelfPopModel(commitShelfEntry("e1", "a1b2c3d4e5f6a7b8", "fix"))
	gen0 := m.pickGen
	mm, _ := m.Update(keyMsg("esc"))
	m = mm.(Model)
	if m.pickGen != gen0+1 {
		t.Fatalf("closing the switcher must invalidate an in-flight probe (gen=%d)", m.pickGen)
	}
}

func TestSwitcherCompareModeIgnoresA(t *testing.T) {
	m := shelfPopModel(commitShelfEntry("e1", "a1b2c3d4e5f6a7b8", "fix"))
	ref := model.FileRef{Source: model.SourceUnstaged, Path: "x.go"}
	m.shelfSwitcher().compareRef = &ref
	gen0 := m.pickGen
	mm, cmd := m.Update(keyMsg("a"))
	m = mm.(Model)
	if cmd != nil || m.pickGen != gen0 {
		t.Fatalf("compare mode must ignore a (cmd=%v gen=%d)", cmd, m.pickGen)
	}
}

func TestSwitcherHintsAdvertiseCherryPick(t *testing.T) {
	sm := shelfPopModel(commitShelfEntry("e1", "a1b2c3d4e5f6a7b8", "fix"))
	if out := sm.renderShelfPopupBox(sm.shelfSwitcher()); !strings.Contains(out, "[a] cherry-pick") {
		t.Fatalf("shelf hint line missing [a] cherry-pick:\n%s", out)
	}
	bm := bookmarkPopModel(commitBookmarkFixture("b1", "a1b2c3d4e5f6a7b8", "fix"))
	if out := bm.renderBookmarkPopupBox(bm.bookmarkSwitcher()); !strings.Contains(out, "[a] cherry-pick") {
		t.Fatalf("bookmark hint line missing [a] cherry-pick:\n%s", out)
	}
	for _, lines := range [][]contentLine{bookmarkSwitcherHelp(false), shelfSwitcherHelp(false)} {
		joined := ""
		for _, l := range lines {
			joined += l.text + "\n"
		}
		if !strings.Contains(joined, "cherry-pick") {
			t.Fatalf("cheat sheet missing the cherry-pick row:\n%s", joined)
		}
	}
}
