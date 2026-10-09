package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/forge"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// A diff opened OVER a PR's files view that is not the PR's own (a commit
// note's diff from View all notes, a working-tree diff from F) is no PR diff:
// no marks, no bars, no send rows.
func TestOnlyThePRsOwnDiffIsAPRDiff(t *testing.T) {
	t.Parallel()
	m := prMenuModel(t) // a PR files view underneath (previewOpen #7)
	pr := &domain.PreviewNoteSet{Source: "refs/gg/pr/7", Tip: "t", Base: "b"}
	m.filesPreviewSet = pr
	v := m.diffLayer()
	v.forgePR = 0
	v.previewSet = nil // a worktree diff
	notes := []domain.ResolvedNote{rootNote("n1", 5, "first", "", model.NoteSourceUser, model.NoteActive)}
	nm, _ := m.Update(notesLoadedMsg{tag: m.diffTag, notes: notes})
	if got := nm.(Model).diffLayer().forgePR; got != 0 {
		t.Fatalf("a non-PR diff over a PR view got forgePR %d", got)
	}
	v.previewSet = pr // the PR's own diff
	nm, _ = m.Update(notesLoadedMsg{tag: m.diffTag, notes: notes})
	if got := nm.(Model).diffLayer().forgePR; got != 7 {
		t.Fatalf("the PR's own diff: forgePR %d", got)
	}
}

// Review Focus 4 for a real AI review: long paragraphs wrap, so the cap
// counts WRAPPED rows, and the whole modal fits the terminal.
func TestSendConfirmFitsTheScreenAfterWrapping(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.width, m.height = 100, 30
	para := strings.Repeat("this overview sentence goes on and on about the change ", 6)
	p := engine.SendPlan{Target: "o/r #7", Verdict: true, Body: strings.Repeat(para+"\n", 5)}
	for i := 0; i < 30; i++ {
		p.Items = append(p.Items, engine.SendItem{Kind: engine.SendThread, Thread: forge.Thread{Path: "a.go", Line: i + 1},
			Summary: strings.Repeat("a long remark summary ", 3)})
	}
	m.forgeSend = &forgeSendState{pr: 7, plan: p}
	req := engine.PromptReq(engine.DecisionSendForge, "Send to %s:\n%s",
		[]string{engine.OptComment, engine.OptApprove, engine.OptRequestChanges, "abort"}, "o/r #7", "x")
	nm, _ := m.Update(opDecisionMsg{req: req, reply: make(chan engine.DecisionResponse, 1)})
	mm := nm.(Model)
	if rows := strings.Count(mm.renderModal(), "\n"); rows > mm.height {
		t.Fatalf("the confirm is %d rows on a %d-row terminal", rows, mm.height)
	}
	if out := ansi.Strip(mm.renderModal()); !strings.Contains(out, "request-changes") || !strings.Contains(out, "Send to o/r #7:") {
		t.Fatalf("the target or the options were cut:\n%s", out)
	}
}

// msgsOf runs cmd (batches flattened, one level of nesting) and returns the
// messages — without feeding anything back.
func msgsOf(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	b, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range b {
		out = append(out, msgsOf(c)...)
	}
	return out
}

// Finishing or discarding an interrupted send re-asks about that PR, open or
// not: the notice must go once GitHub has nothing pending.
func TestAFinishedSendReasksAboutInterruptedSends(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	_, cmd := m.forgeSendFinished(&forgeSendState{pr: 5, plan: engine.SendPlan{Mode: engine.SendFinish}}, engine.Result{}, nil)
	for _, msg := range msgsOf(cmd) {
		if im, ok := msg.(interruptedMsg); ok && im.pr == 5 {
			return
		}
	}
	t.Fatal("no interrupted-send re-check for #5 after its finish")
}

// A send's follow-up runs even when the stash list is open (the
// op-finished handler returns early there): the interrupted-send notice is
// re-asked.
func TestASendsFollowUpRunsWithTheStashListOpen(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.stashView = &stashView{}
	m.running = true
	m.forgeSend = &forgeSendState{pr: 7}
	_, cmd := m.Update(opFinishedMsg{res: engine.Result{Summary: "sent 1 comments to o/r #7"}})
	for _, msg := range msgsOf(cmd) {
		if _, ok := msg.(interruptedMsg); ok {
			return
		}
	}
	t.Fatal("the send's follow-up never ran")
}

// An answer that arrives while another dialog is open never replaces it: a
// replaced op decision would leave its op waiting forever.
func TestASendAnswerNeverReplacesAnOpenDialog(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	open := &decisionState{req: engine.DecisionRequest{ID: "other", Options: []string{"Yes", "No"}}}
	for _, msg := range []tea.Msg{
		sendPanelMsg{pr: 7, cands: panelCands()},
		forgeSendReadyMsg{req: domain.PRSendRequest{PR: 7, Mine: true}},
	} {
		m.modal = open
		nm, _ := m.Update(msg)
		mm := nm.(Model)
		if mm.modal != open || mm.running {
			t.Fatalf("%T replaced the open dialog (running=%v)", msg, mm.running)
		}
		if !strings.Contains(mm.statusMsg, "another dialog") {
			t.Fatalf("%T: status %q", msg, mm.statusMsg)
		}
	}
}

// ctrl+s while another op runs keeps the popup and the typed body.
func TestARefusedSendKeepsTheTypedBody(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m, _ = m.openVerdict(7)
	p := layerOf[*verdictPopup](m)
	p.body = newTextField("a long thought-out body")
	m.running = true
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd != nil || layerOf[*verdictPopup](m) == nil || layerOf[*verdictPopup](m).body.Value() != "a long thought-out body" {
		t.Fatal("the popup (and its body) went with a refused send")
	}
	if !strings.Contains(m.statusMsg, "another operation") {
		t.Fatalf("status %q", m.statusMsg)
	}
}
