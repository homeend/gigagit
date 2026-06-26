package tui

import (
	"math/rand/v2"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/template"
)

func testResolve(value string, inputs map[string]string) (string, []string, error) {
	ctx := template.Ctx{ParentBranch: "main", Repo: "repo", Seqs: map[string]int{}, Now: time.Now, Rand: rand.New(rand.NewPCG(1, 2))}
	out, err := template.Resolve(value, inputs, ctx)
	return out, template.SeqNames(value), err
}

func newTestPrefixPicker(items []model.Prefix, onPick func(Model, string, []string) (Model, tea.Cmd)) *prefixPicker {
	p := &prefixPicker{items: items, resolve: testResolve, onPick: onPick}
	for _, it := range items {
		p.rows = append(p.rows, it.Value)
	}
	return p
}

func TestPickerLiteralPrefixInsertsImmediately(t *testing.T) {
	var got string
	onPick := func(m Model, resolved string, seq []string) (Model, tea.Cmd) {
		got = resolved
		return m.popLayer(), nil
	}
	p := newTestPrefixPicker([]model.Prefix{{Value: "feat/"}}, onPick)
	m := Model{}
	m = m.pushLayer(p)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if got != "feat/" {
		t.Fatalf("got %q, want feat/", got)
	}
}

func TestPickerTemplatedPrefixCollectsThenInserts(t *testing.T) {
	var got string
	onPick := func(m Model, resolved string, seq []string) (Model, tea.Cmd) {
		got = resolved
		return m.popLayer(), nil
	}
	p := newTestPrefixPicker([]model.Prefix{{Value: "john/ISSUE-<user:issue-id>"}}, onPick)
	m := Model{}
	m = m.pushLayer(p)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // select → fill mode
	for _, r := range "1234" {
		m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // finish fill
	if got != "john/ISSUE-1234" {
		t.Fatalf("got %q", got)
	}
}

func TestBranchPopupPrefixSeedsName(t *testing.T) {
	bp := &branchPopup{startPoint: "main"}
	onPick := bp.onPrefixPicked()
	m := Model{}
	m, _ = onPick(m, "feat/login", []string{"sandbox_seq"})
	if bp.name.Value() != "feat/login" {
		t.Fatalf("name = %q", bp.name.Value())
	}
	if len(bp.prefixSeqNames) != 1 || bp.prefixSeqNames[0] != "sandbox_seq" {
		t.Fatalf("seqNames = %v", bp.prefixSeqNames)
	}
}

func TestWorktreePopupPrefixSeedsEdit(t *testing.T) {
	p := &worktreePopup{startPoint: "main", branchTmpl: "b/<random-alpha:4>", pathTmpl: "../<repo>.worktrees/<branch>", state: stAction}
	onPick := p.onPrefixPicked()
	m := Model{}
	m, _ = onPick(m, "feat/login", []string{"sandbox_seq"})
	if p.state != stEdit {
		t.Fatalf("state = %v, want stEdit", p.state)
	}
	if p.editBuf.Value() != "feat/login" {
		t.Fatalf("editBuf = %q", p.editBuf.Value())
	}
	if !containsAllSeq(p.consumedSeqNames(), []string{"sandbox_seq"}) {
		t.Fatalf("consumed = %v", p.consumedSeqNames())
	}
}

func containsAllSeq(have, want []string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
