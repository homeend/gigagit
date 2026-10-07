package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

func TestSendReviewPopupRequest(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		p    sendReviewPopup
		want domain.PRSendRequest
	}{
		{sendReviewPopup{pr: 7, group: domain.GroupMine, body: newTextField(" LGTM \n")}, domain.PRSendRequest{PR: 7, Mine: true, Body: "LGTM"}},
		{sendReviewPopup{pr: 7, group: "review:r1", body: newTextField("edited")}, domain.PRSendRequest{PR: 7, Review: "r1", Body: "edited"}},
		{sendReviewPopup{pr: 7, verdict: true}, domain.PRSendRequest{PR: 7, Verdict: true}},
	} {
		if got := c.p.request(); got.PR != c.want.PR || got.Mine != c.want.Mine || got.Review != c.want.Review ||
			got.Verdict != c.want.Verdict || got.Body != c.want.Body {
			t.Errorf("request = %+v, want %+v", got, c.want)
		}
	}
}

func TestSendGroupsOpenAChooserThenTheBody(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	nm, _ := m.Update(sendGroupsMsg{pr: 7, groups: []domain.SendGroup{
		{ID: domain.GroupMine, Count: 2}, {ID: "review:r1", Agent: "claude", Summary: "two nits", Count: 1}}})
	mm := nm.(Model)
	if mm.modal == nil || len(mm.modal.req.Options) != 3 || mm.modal.req.Options[2] != "Cancel" {
		t.Fatalf("chooser = %+v", mm.modal)
	}
	if !strings.Contains(mm.modal.req.Options[0], "2") || !strings.Contains(mm.modal.req.Options[1], "claude") {
		t.Fatalf("labels = %q", mm.modal.req.Options)
	}
	nm, _ = mm.resolveModal(mm.modal.req.Options[0]) // my draft review: no body to load
	p := layerOf[*sendReviewPopup](nm.(Model))
	if p == nil || p.group != domain.GroupMine || p.body.Value() != "" {
		t.Fatalf("body popup = %+v", p)
	}
}

func TestOneSendGroupSkipsTheChooser(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	nm, _ := m.Update(sendGroupsMsg{pr: 7, groups: []domain.SendGroup{{ID: domain.GroupMine, Count: 1}}})
	if p := layerOf[*sendReviewPopup](nm.(Model)); p == nil || nm.(Model).modal != nil {
		t.Fatal("one group goes straight to the body")
	}
	nm, _ = m.Update(sendGroupsMsg{pr: 7})
	if !strings.Contains(nm.(Model).statusMsg, "#7") {
		t.Fatalf("no group: status %q", nm.(Model).statusMsg)
	}
}

func TestSendBodyMsgPrefillsTheReviewSummary(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	nm, _ := m.Update(sendBodyMsg{pr: 7, group: "review:r1", body: "Two nits."})
	if p := layerOf[*sendReviewPopup](nm.(Model)); p == nil || p.body.Value() != "Two nits." {
		t.Fatalf("popup = %+v", p)
	}
}

func TestSendReviewPopupKeys(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m, _ = m.openVerdict(7)
	p := layerOf[*sendReviewPopup](m)
	if p == nil || !p.verdict {
		t.Fatal("Verdict… opens the body popup in verdict mode")
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if p.body.Value() != "\n" {
		t.Fatalf("enter is a newline in the body, got %q", p.body.Value())
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if layerOf[*sendReviewPopup](m) != nil {
		t.Fatal("esc closes it")
	}
}

func TestPRHubAdvertisesAndRunsSendKeys(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m, _ = m.openPRHub(m.prs[0])
	hub := layerOf[*prHubPopup](m)
	if !strings.Contains(hub.keys, "[s]") || !strings.Contains(hub.keys, "[v]") {
		t.Fatalf("hub footer %q", hub.keys)
	}
	nm, _ := hub.update(m, synthKey("v"))
	if layerOf[*sendReviewPopup](nm) == nil {
		t.Fatal("v opens Verdict…")
	}
}
