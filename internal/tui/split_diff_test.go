package tui

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

// diffSplitModel is a commit that edits a.txt (20 lines: line 3 changed,
// line 5 deleted, l21 added at the end) and gives b.txt and c.txt the same
// new second line, reviewed with a remark on a.txt's new line 3 and one on
// its old line 5; its overview anchors a.txt:3, b.txt:2, c.txt:2 — opened as
// the split, the focus on the file.
func diffSplitModel(t *testing.T) (Model, *splitView) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-b", "main")
	var old []string
	for i := 1; i <= 20; i++ {
		old = append(old, "l"+strconv.Itoa(i))
	}
	write("a.txt", strings.Join(old, "\n")+"\n")
	write("b.txt", "b1\nb2\n")
	write("c.txt", "b1\nb2\n")
	git("add", ".")
	git("commit", "-m", "c0")
	nu := append([]string{"l1", "l2", "L3", "l4"}, old[5:]...)
	write("a.txt", strings.Join(append(nu, "l21"), "\n")+"\n")
	write("b.txt", "b1\nB2\n")
	write("c.txt", "b1\nB2\n")
	git("add", ".")
	git("commit", "-m", "c1")

	m := New(domain.New(testRepo(t, dir)))
	m.width, m.height = 160, 40
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	if len(m.commits) == 0 {
		t.Fatal("no commits loaded")
	}
	m = tempPromptStore(t, m)
	m.svc.UseNotesDir(t.TempDir())
	sha := m.commits[0].Hash
	ov, _ := json.Marshal("Look at [three](a.txt:3), then [b](b.txt:2), then [c](c.txt:2).")
	doc := `{"version":1,"summary":"## Summary\nok","overview":` + string(ov) + `,"files":[{"path":"a.txt","annotations":[{"newRange":[3,3],"summary":"why L3?"},{"oldRange":[5,5],"summary":"why drop l5?"}]}]}`
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: sha + "^.." + sha, Label: shortHash(sha)}
	id, _, err := m.svc.SaveReview(context.Background(), domain.SaveReview{Target: tg, Agent: "Claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	m.sel[panelCommits] = 0
	m, cmd := m.openChangedFiles(m.commits[0])
	m = drainCmds(t, m, cmd)
	m, cmd = m.openReview(id, "review")
	m = drainCmds(t, m, cmd)
	u2, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u2.(Model), cmd)
	sv := m.splitLayer()
	if sv == nil || m.topLayer() != sv {
		t.Fatalf("top = %T, want the split", m.topLayer())
	}
	m = pressDrain(t, m, "enter") // the focus on the file
	if sv.onLeft {
		t.Fatal("enter moves the focus to the file")
	}
	return m, sv
}

func TestDiffShowsRowsAndKeepsTheCursorLineBothWays(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	d := sv.right.doc()
	if d.p.cur != 2 {
		t.Fatalf("file cursor on line %d, want 3", d.p.cur+1)
	}
	m = pressDrain(t, m, "D")
	if d.diff == nil || d.diff.notice != "" {
		t.Fatalf("diff = %+v", d.diff)
	}
	if r := d.diff.rows[d.diff.p.cur]; r.kind != diffAdd || r.nu != 3 {
		t.Fatalf("cursor row %+v, want the added line 3", r)
	}
	if !strings.HasPrefix(d.frameTitle(), "Diff a.txt (") || !strings.Contains(d.frameTitle(), " → ") {
		t.Fatalf("title %q", d.frameTitle())
	}
	m = pressDrain(t, m, "alt+up") // onto the deleted l3 row
	if k := d.diff.rows[d.diff.p.cur].kind; k != diffDel || d.p.cur != 2 {
		t.Fatalf("on a %v row the mirrored file line is %d, want 3 (the next new line)", k, d.p.cur+1)
	}
	m = pressDrain(t, m, "D")
	if d.diff != nil || d.p.cur != 2 {
		t.Fatalf("back on the file: diff %v cursor line %d", d.diff, d.p.cur+1)
	}
}

func TestDiffDrawsSignsBandAndBothSidesRemarks(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	d := sv.right.doc()
	m = pressDrain(t, m, "D")
	screen := ansi.Strip(m.View())
	for _, want := range []string{"3 − l3", "3 + L3", "5 − l5", "20 + l21", "why L3?", "why drop l5?"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("screen lacks %q:\n%s", want, screen)
		}
	}
	bs := d.rowBands()
	if len(bs) != 1 || d.diff.rows[bs[0].start-1].nu != 3 || bs[0].start != bs[0].end {
		t.Fatalf("row bands %+v: the link's line 3 is banded on its added row", bs)
	}
	if !strings.Contains(screen, "┃") {
		t.Fatal("the band's gutter mark shows")
	}
}

func TestDiffOfADeletedFileShowsEveryLineDeleted(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	d := sv.right.doc()
	res := domain.ReviewFileDiff{Changed: true, Status: "D", Left: "a", Right: "b"}
	res.Diff.Result.Rows = []textdiff.Row{{Kind: textdiff.Del, Left: "x", LeftNo: 1}, {Kind: textdiff.Del, Left: "y", LeftNo: 2}}
	d.showDiff(res, nil, false, false, m.docRows(d))
	if d.diff.notice != "" || len(d.diff.rows) != 2 || d.diff.rows[1].kind != diffDel {
		t.Fatalf("diff %+v", d.diff)
	}
	m = pressDrain(t, m, "alt+down")
	if d.diff.p.cur != 1 {
		t.Fatalf("the cursor walks deleted rows: %d", d.diff.p.cur)
	}
}

// The split's diff of a file reads its rows first; a big file's colours
// land later on the same rows (the cursor stays), only for those bytes.
func TestDocDiffTakesLateColours(t *testing.T) {
	t.Parallel()
	d := &openFile{path: "a.go", p: &contentPopup{}}
	res := domain.ReviewFileDiff{Changed: true, Diff: domain.Diff{
		Result: textdiff.Result{Rows: []textdiff.Row{{Kind: textdiff.Same, Left: "package p", Right: "package p", LeftNo: 1, RightNo: 1}, {Kind: textdiff.Add, Right: "var x = 1", RightNo: 2}}},
		OldSum: "o", NewSum: "n", TokPending: true,
	}}
	d.showDiff(res, nil, true, false, 20)
	if d.diff.p.lines[0].cls != nil {
		t.Fatal("the rows come plain first")
	}
	toks := [][]syntax.Tok{{{Start: 0, End: 7, Class: syntax.Keyword}}}
	if d.applyDiffTokens(domain.Diff{OldSum: "o", NewSum: "x", NewTok: toks}) || d.diff.p.lines[0].cls != nil {
		t.Fatal("colours of other bytes must be dropped")
	}
	d.diff.p.cur = 0
	if !d.applyDiffTokens(domain.Diff{OldSum: "o", NewSum: "n", OldTok: toks, NewTok: toks}) {
		t.Fatal("the colours of the shown bytes must land")
	}
	if l := d.diff.p.lines[0]; len(l.cls) == 0 || l.cls[0] != syntax.Keyword {
		t.Fatalf("row 0 cls = %v", l.cls)
	}
}
