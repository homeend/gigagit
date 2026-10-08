package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/i18n"
)

func TestSendConfirmTextNamesEveryItemAndSkip(t *testing.T) {
	t.Parallel()
	p := engine.SendPlan{Target: "o/r #7", Mode: engine.SendReview, Body: "Two things.\n\n" + forge.SendMarker("r1"),
		Items: []engine.SendItem{
			{Kind: engine.SendThread, Thread: forge.Thread{Path: "a.go", Line: 12, StartLine: 12}, Summary: "rename this",
				Replies: []engine.SendReplyBody{{Key: "x"}}},
			{Kind: engine.SendThread, Thread: forge.Thread{Path: "b.go"}, Summary: "split this file"},
		},
		Skipped: []engine.SendSkip{{Reason: domain.SkipNotInPR, Path: "c.go", Line: 3, Summary: "elsewhere"},
			{Reason: domain.SkipOnGitHub}},
	}
	got := sendConfirmText(p)
	for _, want := range []string{"Send to o/r #7:", "review body:", "Two things.", "+ a.go:12 rename this (1 reply)",
		"+ b.go (file) split this file", "- c.go:3 elsewhere (skipped: not in this PR)", "- review summary (skipped: already on GitHub)"} {
		if !strings.Contains(got, want) {
			t.Errorf("confirm lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "gg:") {
		t.Errorf("the send marker leaked into the confirm:\n%s", got)
	}
}

// Review Focus 4: a long review fits the screen.
func TestSendConfirmTextIsCapped(t *testing.T) {
	t.Parallel()
	p := engine.SendPlan{Target: "o/r #7", Body: strings.Repeat("line\n", 40)}
	for i := 0; i < 30; i++ {
		p.Items = append(p.Items, engine.SendItem{Kind: engine.SendThread, Thread: forge.Thread{Path: "a.go", Line: i + 1}, Summary: fmt.Sprint("r", i)})
	}
	got := sendConfirmText(p)
	if n := strings.Count(got, "\n") + 1; n > 24 {
		t.Fatalf("%d rows:\n%s", n, got)
	}
	if !strings.Contains(got, "+ 18 more") {
		t.Fatalf("the cut is not said:\n%s", got)
	}
}

// Every reason code has words of its own — never the generic fallback. In
// English the two read alike, so it is checked in Japanese. Serial: switches
// the process-wide language.
func TestEverySkipReasonHasItsOwnWords(t *testing.T) {
	if err := i18n.SetLanguage("ja", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = i18n.SetLanguage("", "") })
	for _, r := range domain.SendSkipReasons() {
		if got := sendSkipReasonText("x", r); got == i18n.T("%[1]s (skipped: %[2]s)", "x", r) {
			t.Errorf("reason %q falls back to the generic text", r)
		}
	}
}

// The engine's English prompt is replaced by the TUI's own; nothing is
// preselected for the user (agents never send, so no wish to honour).
func TestForgeSendDecisionUsesTheTUIsConfirm(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.forgeSend = &forgeSendState{pr: 7, plan: engine.SendPlan{Target: "o/r #7", Verdict: true}}
	req := engine.PromptReq(engine.DecisionSendForge, "Send to %s:\n%s",
		[]string{engine.OptComment, engine.OptApprove, engine.OptRequestChanges, "abort"}, "o/r #7", "ENGLISH")
	nm, _ := m.Update(opDecisionMsg{req: req, reply: make(chan engine.DecisionResponse, 1)})
	mm := nm.(Model)
	if mm.modal == nil || strings.Contains(renderPrompt(mm.modal.req), "ENGLISH") || !strings.HasPrefix(renderPrompt(mm.modal.req), "Send to o/r #7:") {
		t.Fatalf("modal prompt = %q", renderPrompt(mm.modal.req))
	}
	if mm.modal.sel != 0 {
		t.Fatalf("preselected %q", mm.modal.req.Options[mm.modal.sel])
	}
}

// Review Focus 1: while the confirm is open the comment poll stands down,
// so no refresh re-renders the view under the modal.
func TestThePRPollWaitsWhileTheConfirmIsOpen(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.forgeSend = &forgeSendState{pr: 7}
	m.modal = &decisionState{req: engine.DecisionRequest{ID: engine.DecisionSendForge, Options: []string{"send", "abort"}}}
	if _, cmd := m.prCommentsTick(time.Now().Add(24 * time.Hour)); cmd != nil {
		t.Fatal("the PR poll ran under the send confirm")
	}
}

// A send whose plan fails says why; nothing is started.
func TestAFailedPlanStartsNothing(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	nm, cmd := m.Update(forgeSendReadyMsg{req: domain.PRSendRequest{PR: 7, Mine: true},
		err: errors.New("#7's diff is not available here")})
	mm := nm.(Model)
	if cmd != nil || mm.forgeSend != nil || mm.running || !strings.Contains(mm.statusMsg, "not available") {
		t.Fatalf("cmd=%v send=%v running=%v status=%q", cmd != nil, mm.forgeSend != nil, mm.running, mm.statusMsg)
	}
}

func TestSendToForgeRefreshesTheNotes(t *testing.T) {
	t.Parallel()
	srcs := opAffectedSources(engine.SendToForge{})
	if len(srcs) != 1 || srcs[0] != srcNotes {
		t.Fatalf("SendToForge refreshes %v, want [srcNotes]", srcs)
	}
}

// Agents never send (user ruling 2026-10-08): the TUI has no notice source
// for an agent's queued send, so nothing an agent writes can raise "send".
func TestNoNoticeOffersAnAgentsSend(t *testing.T) {
	t.Parallel()
	m := newTestModel(t).rebuildNotices()
	for _, n := range m.notices {
		if strings.HasPrefix(n.id, "pending_send_") {
			t.Fatalf("a pending-send notice exists: %+v", n)
		}
	}
	src, err := os.ReadFile("notify.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "pendingSendNotices") {
		t.Fatal("notify.go still builds pending-send notices")
	}
}

// W2: the body popup always answers the body — an emptied box posts none.
func TestSendReviewPopupAnswersTheBody(t *testing.T) {
	t.Parallel()
	p := &sendReviewPopup{pr: 7, group: "review:r1", body: newTextField("")}
	if req := p.request(); !req.BodySet || req.Body != "" || req.Review != "r1" {
		t.Fatalf("request = %+v", req)
	}
}

// Item 14: an interrupted send's rows say what waits, not a ledger key.
func TestFinishRowsShowSummaries(t *testing.T) {
	t.Parallel()
	got := sendItemText(engine.SendFinish, engine.SendItem{Key: "n-123", Summary: "rename this"})
	if got != "rename this (waiting in the pending review)" {
		t.Fatalf("row = %q", got)
	}
	if got := sendItemText(engine.SendFinish, engine.SendItem{Key: "n-123"}); !strings.Contains(got, "n-123") {
		t.Fatalf("no summary falls back to the key: %q", got)
	}
}

// Item 16: each skip row and the resolve suffix is ONE format — a translation
// orders the whole row.
func TestSendRowsAreWholeFormats(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("forge_send.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`what + " " + sendSkipReasonText`, `" · " + i18n.T("resolved after sending")`} {
		if strings.Contains(string(src), bad) {
			t.Errorf("forge_send.go still composes %q", bad)
		}
	}
	if got := sendSkipText(engine.SendSkip{Path: "a.go", Line: 3, Summary: "x", Reason: domain.SkipNotInPR}); got != "a.go:3 x (skipped: not in this PR)" {
		t.Fatalf("skip row = %q", got)
	}
}
