package tui

import (
	"math/rand/v2"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/template"
)

func fillRunes(f *templateFill, s string) {
	for _, r := range s {
		f.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func neutralFillCtx() template.Ctx {
	return template.Ctx{ParentBranch: "p", Repo: "r", Seqs: map[string]int{}, Now: time.Now, Rand: rand.New(rand.NewPCG(1, 2))}
}

func TestTemplateFillNoLabelsFastPath(t *testing.T) {
	f := newTemplateFill("feat/")
	if f.needsInput() {
		t.Fatal("literal prefix should need no input")
	}
	out, err := template.Resolve("feat/", f.inputs(), neutralFillCtx())
	if err != nil || out != "feat/" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestTemplateFillCollectsLabelThenResolves(t *testing.T) {
	f := newTemplateFill("john/ISSUE-<user:issue-id>")
	if !f.needsInput() {
		t.Fatal("want needsInput")
	}
	fillRunes(&f, "1234")
	done, cancel := f.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !done || cancel {
		t.Fatalf("done=%v cancel=%v", done, cancel)
	}
	out, err := template.Resolve("john/ISSUE-<user:issue-id>", f.inputs(), neutralFillCtx())
	if err != nil {
		t.Fatal(err)
	}
	if out != "john/ISSUE-1234" {
		t.Fatalf("out = %q", out)
	}
}
