package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

var tokRuns = [][]syntax.Tok{{{Start: 0, End: 7, Class: syntax.Keyword}}}

// pendingDiffModel opens a diff layer under tag whose view shows sides
// named "a"/"b" — what a TokDefer load leaves before its colours arrive.
func pendingDiffModel(t *testing.T) Model {
	t.Helper()
	m := loadedModelLinearCommits(t, 2)
	v := &diffView{title: "f", loading: true}
	m, _ = m.openPickerDiff(v, "tag1", nil)
	loaded := &diffView{title: "f", oldSum: "a", newSum: "b", lines: wrapLines([]textdiff.Line{{}})}
	nm, _ := m.Update(diffMsg{tag: "tag1", view: loaded})
	return nm.(Model)
}

func TestDiffTokMsgAppliesToLiveView(t *testing.T) {
	t.Parallel()
	m := pendingDiffModel(t)
	nm, _ := m.Update(diffTokMsg{tag: "tag1", out: domain.Diff{OldSum: "a", NewSum: "b", NewTok: tokRuns}})
	if dv := nm.(Model).diffLayer(); dv == nil || !reflect.DeepEqual(dv.newTok, tokRuns) {
		t.Fatal("the colours of the diff on screen must land on it")
	}
}

func TestDiffTokMsgDroppedForStaleTag(t *testing.T) {
	t.Parallel()
	m := pendingDiffModel(t)
	nm, _ := m.Update(diffTokMsg{tag: "tag0", out: domain.Diff{OldSum: "a", NewSum: "b", NewTok: tokRuns}})
	if dv := nm.(Model).diffLayer(); dv.newTok != nil {
		t.Fatal("colours for a file the view has left must be dropped")
	}
}

func TestDiffTokMsgDroppedForOtherSums(t *testing.T) {
	t.Parallel()
	m := pendingDiffModel(t)
	nm, _ := m.Update(diffTokMsg{tag: "tag1", out: domain.Diff{OldSum: "a", NewSum: "c", NewTok: tokRuns}})
	if dv := nm.(Model).diffLayer(); dv.newTok != nil {
		t.Fatal("colours of other bytes (the file changed between the reads) must be dropped")
	}
}

func TestHistoryTokMsgAppliesToHistoryPane(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 2)
	h := &historyView{diffTag: "histdiff:x", diff: &diffView{oldSum: "a", newSum: "b"}}
	_, _ = m.Update(diffTokMsg{tag: "histdiff:x", hist: h, out: domain.Diff{OldSum: "a", NewSum: "b", NewTok: tokRuns}})
	if !reflect.DeepEqual(h.diff.newTok, tokRuns) {
		t.Fatal("the history pane's diff must take its colours")
	}
	h2 := &historyView{diffTag: "histdiff:y", diff: &diffView{oldSum: "a", newSum: "b"}}
	_, _ = m.Update(diffTokMsg{tag: "histdiff:x", hist: h2, out: domain.Diff{OldSum: "a", NewSum: "b", NewTok: tokRuns}})
	if h2.diff.newTok != nil {
		t.Fatal("a history pane that moved on must drop the old row's colours")
	}
}

func TestHistorySelectCancelsPendingTokens(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 2)
	cancelled := false
	h := &historyView{
		commits: []model.FileCommit{{Commit: model.Commit{Hash: strings.Repeat("a", 40)}, Path: "f", Status: "M"}},
		diff:    &diffView{tokCancel: func() { cancelled = true }},
	}
	_ = h.selectCmd(m)
	if !cancelled {
		t.Fatal("moving the history selection must cancel the colouring of the row left")
	}
}

func TestLoadCommitDiffDefersLargeFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	body := strings.Repeat("func f() int { return 1 }\n", 3000)
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte("package p\n\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "one")
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte("package p\n\nvar x = 2\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "two")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.TrimSpace(string(out))
	m := New(domain.Open(dir))
	msg, ok := m.loadCommitDiffCmd(hash, contentLine{path: "big.go", status: "M"})().(diffMsg)
	if !ok || msg.view.err != nil {
		t.Fatalf("load = %#v", msg)
	}
	v := msg.view
	if len(v.full) == 0 || v.newTok != nil || v.tokReq == nil {
		t.Fatalf("rows=%d newTok=%v tokReq=%v; want plain rows with the colours still to come", len(v.full), v.newTok != nil, v.tokReq != nil)
	}
	tm, ok := m.diffTokCmd(v, msg.tag, nil)().(diffTokMsg)
	if !ok || tm.err != nil || tm.out.NewTok == nil || tm.out.NewSum != v.newSum {
		t.Fatalf("diffTokCmd = %#v", tm)
	}
}

// Stepping to another file in the diff view reuses the layer in place: the
// colouring of the file left behind must stop, not run on unseen.
func TestStatusStepCancelsPendingTokens(t *testing.T) {
	t.Parallel()
	m := pendingDiffModel(t)
	cancelled := false
	m.diffLayer().tokCancel = func() { cancelled = true }
	_, _ = m.openStatusDiff(model.FileStatus{Path: "other.txt"}, false)
	if !cancelled {
		t.Fatal("a file step must cancel the colouring of the file it leaves")
	}
}

// A stack seeded from a view still waiting for its colours would keep that
// file plain for good (the stack drops the single view's colour message):
// such a file loads in the stack like any other, and its fetch is stopped.
func TestStackSeedSkipsPendingColours(t *testing.T) {
	t.Parallel()
	m := tempPromptStore(t, treeDiffModelBlocks(2))
	cancelled := false
	dv := m.diffLayer()
	dv.tokReq = &domain.Request{Path: "x.go"}
	dv.tokCancel = func() { cancelled = true }
	u, _ := m.Update(keyMsg("S"))
	v := u.(Model).diffLayer()
	if f := v.stk.files[v.curFile()]; f.load == stackLoaded {
		t.Fatal("a file whose colours are still coming must not be seeded plain into the stack")
	}
	if !cancelled {
		t.Fatal("the seed's colour fetch must be stopped")
	}
}
