package tui

import (
	"strings"
	"testing"
)

func TestInterruptedSendBecomesANotice(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	nm, _ := m.Update(interruptedMsg{gen: m.noticeGen, pr: 7, review: "PRR_1", keys: []string{"n1", "n2", "n3"}})
	m = nm.(Model)
	n := noticeByID(m, "interrupted_send_7")
	if n == nil || !strings.Contains(n.title, "#7") || !strings.Contains(n.title, "3") {
		t.Fatalf("notice = %+v", n)
	}
	var labels []string
	for _, a := range n.actions {
		labels = append(labels, a.label)
		if !a.sourced {
			t.Errorf("%q dismisses the notice", a.label)
		}
	}
	if strings.Join(labels, ",") != "Finish sending,Discard,Later" {
		t.Fatalf("actions %v", labels)
	}
	// Joined the user's own pending review: never discard it.
	nm, _ = m.Update(interruptedMsg{gen: m.noticeGen, pr: 7, review: "PRR_1", keys: []string{"n1"}, joined: true})
	for _, a := range noticeByID(nm.(Model), "interrupted_send_7").actions {
		if a.label == "Discard" {
			t.Fatal("Discard offered on the user's own pending review")
		}
	}
	// Settled: the notice goes.
	nm, _ = nm.(Model).Update(interruptedMsg{gen: m.noticeGen, pr: 7})
	if noticeByID(nm.(Model), "interrupted_send_7") != nil {
		t.Fatal("a settled send keeps its notice")
	}
}

func TestARefreshAsksForInterruptedSends(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	pr := m.prs[0]
	pr.HeadSHA = m.previewOpen.srcHash
	_, cmd := m.Update(prRevalidatedMsg{n: 7, gen: m.prsGen, pr: pr})
	found := false
	for _, msg := range flattenCmd(t, cmd) {
		if _, ok := msg.(interruptedMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("a successful refresh asks whether a send was interrupted")
	}
}
