package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func jumpBlame(n int) (Model, *blameView) {
	m := Model{width: 100, height: 20}
	lines := make([]model.BlameLine, n)
	for i := range lines {
		lines[i] = model.BlameLine{Hash: "aaaaaaa", Author: "Ada", LineNo: i + 1, Content: "x"}
	}
	b := &blameView{ctx: navContext{path: "a.go"}, lines: lines, blocks: groupBlame(lines)}
	return m.pushLayer(b), b
}

func blameKeys(m Model, b *blameView, keys ...string) Model {
	for _, k := range keys {
		m, _ = b.update(m, keyMsg(k))
	}
	return m
}

func TestJumpBlameLands(t *testing.T) {
	t.Parallel()
	m, b := jumpBlame(30)
	m = blameKeys(m, b, ":", "1", "2")
	if !strings.Contains(b.render(m, ""), ":12█") {
		t.Fatal("the header does not show the prompt")
	}
	blameKeys(m, b, "enter")
	if b.sel != 11 || b.jump.typing {
		t.Fatalf("sel %d typing %v, want 11 and closed", b.sel, b.jump.typing)
	}
}

func TestJumpBlamePastEnd(t *testing.T) {
	t.Parallel()
	m, b := jumpBlame(30)
	m = blameKeys(m, b, ":", "9", "9", "enter")
	if b.sel != 29 {
		t.Fatalf("sel %d, want 29 (the last line)", b.sel)
	}
	if !strings.Contains(m.statusMsg, "line 99 is past the end of a.go (30 lines)") {
		t.Fatalf("statusMsg %q", m.statusMsg)
	}
}

func TestJumpBlameLoadingNoop(t *testing.T) {
	t.Parallel()
	m, b := jumpBlame(30)
	b.loading = true
	blameKeys(m, b, ":")
	if b.jump.typing {
		t.Fatal(": opened a prompt over a loading blame")
	}
}

// The jump lands in the file under the cursor: every file of the stream has
// a line 2.
func TestJumpStackPaneLandsInCursorFile(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = pressDrain(t, m, "down") // b.go:3
	m = pressDrain(t, m, "tab")  // the focus on the stack
	if sv.onLeft {
		t.Fatal("tab moves the focus to the stack")
	}
	m = fvKeys(t, typeJump(t, m, "2"), enterKey)
	d := sv.right.doc()
	if want := sp.stream.streamLine(1, 2); d.p.cur != want {
		t.Fatalf("cur %d, want %d (b.go:2)", d.p.cur, want)
	}
	if d.p.jump.typing {
		t.Fatal("enter must close the prompt")
	}
	_ = m
}

// A folded file of the stream shows no lines: the jump says so (as the web's
// review stack does) and the cursor stays put.
func TestJumpStackPaneFoldedFileSaysUnfold(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = pressDrain(t, m, "tab") // the focus on the stack
	sp.stream.files[0].folded = true
	sp.rebuild(m)
	d := sv.right.doc()
	cur := sp.stream.files[0].hdr
	d.p.cur = cur
	m = fvKeys(t, typeJump(t, m, "2"), enterKey)
	if d.p.cur != cur {
		t.Fatalf("cursor moved %d→%d on a folded file", cur, d.p.cur)
	}
	if !strings.Contains(m.statusMsg, "unfold it first") {
		t.Fatalf("statusMsg = %q, want the unfold notice", m.statusMsg)
	}
}
