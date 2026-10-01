package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

func fpLinkModel(t *testing.T, state model.FileState) Model {
	t.Helper()
	rows := sameRowsTUI(40)
	rows[5].Left, rows[5].Right = "  keep", "  keep"
	rows[20] = textdiff.Row{Kind: textdiff.Del, Left: "\tgone", LeftNo: 21}
	rows[10] = textdiff.Row{Kind: textdiff.Add, Right: "", RightNo: 11} // a blank line
	m := openedDiffModel(12, rows, []int{20})
	m.linkRepoName, m.currentWorktree = "gigagit", "/repo"
	v := m.diffLayer()
	v.title = "a.txt"
	v.noteAddr = model.FileAddress{State: state, Path: "a.txt", Worktree: "/repo"}
	return m
}

func TestWorkingTreeLineLinkCarriesAFingerprint(t *testing.T) {
	t.Parallel()
	m := fpLinkModel(t, model.StateUnstaged)
	v := m.diffLayer()
	v.curLine = 5
	row, _ := v.cursorRow()
	if row.Right != "  keep" {
		t.Fatalf("cursor row = %+v", row)
	}
	wantLink(t, m, "gg://gigagit/a.txt:6~"+model.LineFingerprint("keep"))
	v.curLine = 20 // the old side: the index's text, tab-indented
	wantLink(t, m, "gg://gigagit/a.txt:old:21~"+model.LineFingerprint("gone"))
	v.curLine = 10 // blank: the plain form
	wantLink(t, m, "gg://gigagit/a.txt:11")

	m = fpLinkModel(t, model.StateStaged)
	m.diffLayer().curLine = 5
	wantLink(t, m, "gg://gigagit/a.txt@staged:6~"+model.LineFingerprint("keep"))

	m = fpLinkModel(t, model.StateUntracked)
	m.diffLayer().curLine = 5
	wantLink(t, m, "gg://gigagit/a.txt:6~"+model.LineFingerprint("keep"))
}

func TestCommittedLineLinkHasNoFingerprint(t *testing.T) {
	t.Parallel()
	m := fpLinkModel(t, model.StateCommitted)
	v := m.diffLayer()
	v.noteAddr.Commit = cmpShaA
	v.curLine = 5
	got, ok := m.contextLinkText()
	if !ok || strings.Contains(got, "~") {
		t.Fatalf("contextLinkText = %q ok=%v, want no fingerprint on a commit link", got, ok)
	}
}

// A compare against the working tree links the working side WITH a
// fingerprint and the commit side without.
func TestCompareWorkingSideCarriesAFingerprint(t *testing.T) {
	t.Parallel()
	m := compareLinkModel(t, mustCommitEndpoint(cmpShaA), model.WorkTreeEndpoint())
	v := m.diffLayer()
	v.curLine = 5
	v.lines[5].Row.Left, v.lines[5].Row.Right = "keep", "keep"
	row, _ := v.cursorRow()
	if row.RightNo != 6 || row.Right != "keep" {
		t.Fatalf("cursor row = %+v, want new line 6 \"keep\"", row)
	}
	wantLink(t, m, "gg://gigagit/a.txt:6~"+model.LineFingerprint("keep"))
	v.curLine = 20
	wantLink(t, m, "gg://gigagit/a.txt@"+cmpShaA+":21")
}
