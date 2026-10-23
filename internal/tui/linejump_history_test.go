package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func histJumpModel(h *historyView) Model {
	m := diffModel()
	m.width, m.height = 100, 30
	return m.pushLayer(h)
}

func histKeys(m Model, h *historyView, keys ...string) Model {
	for _, k := range keys {
		msg := keyMsg(k)
		if k == "enter" {
			msg = enterKey
		}
		m, _ = h.update(m, msg)
	}
	return m
}

func TestJumpHistoryOpensCommitVersion(t *testing.T) {
	t.Parallel()
	h := histFixture()
	m := histKeys(histJumpModel(h), h, ":", "5")
	if !strings.Contains(h.render(m, ""), ":5█") {
		t.Fatal("the header does not show the prompt")
	}
	m = histKeys(m, h, "enter")
	fv, ok := m.topLayer().(*fileViewer)
	if !ok {
		t.Fatalf("top = %T, want the file viewer", m.topLayer())
	}
	if fv.src != (fileSource{kind: srcCommit, rev: "aaaaaaa"}) || fv.path != "a.go" || fv.pendingLine != 5 {
		t.Fatalf("viewer %+v %q line %d", fv.src, fv.path, fv.pendingLine)
	}
	if h.jump.typing {
		t.Fatal("enter must close the prompt")
	}
}

// A commit that deleted the file has no version of it: the jump opens the
// version it deleted (the parent's, under its old name).
func TestJumpHistoryDeletedOpensParent(t *testing.T) {
	t.Parallel()
	h := &historyView{ctx: navContext{path: "a.go"}, commits: []model.FileCommit{
		{Commit: model.Commit{Hash: "ccccccc", Subject: "drop"}, Status: "D", Path: "a.go"},
	}}
	m := histKeys(histJumpModel(h), h, ":", "2", "enter")
	fv, ok := m.topLayer().(*fileViewer)
	if !ok || fv.src != (fileSource{kind: srcCommit, rev: "ccccccc^"}) || fv.path != "a.go" {
		t.Fatalf("top %T %+v", m.topLayer(), fv)
	}
}

func TestJumpHistoryEmptyNoop(t *testing.T) {
	t.Parallel()
	h := &historyView{ctx: navContext{path: "a.go"}, loading: true}
	histKeys(histJumpModel(h), h, ":")
	if h.jump.typing {
		t.Fatal(": opened a prompt with no commit selected")
	}
}
