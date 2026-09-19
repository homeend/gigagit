package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
)

func TestForgetPROnlyForNonOpenRows(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	if m.canForgetPR() {
		t.Fatal("row 0 is open: it cannot be forgotten (it would come straight back)")
	}
	nm, cmd := m.Update(keyMsg("d"))
	if got := nm.(Model); cmd != nil || got.running || !strings.Contains(got.statusMsg, "closed or merged") {
		t.Fatalf("d on an open row: cmd=%v running=%v status=%q", cmd != nil, got.running, got.statusMsg)
	}
	m.sel[panelPRs] = 1 // the merged one
	if !m.canForgetPR() {
		t.Fatal("a merged row can be forgotten")
	}
	nm, cmd = m.Update(keyMsg("d"))
	mm := nm.(Model)
	if cmd == nil || !mm.running || !mm.pendingPRsReload {
		t.Fatalf("d on a merged row: cmd=%v running=%v reload=%v", cmd != nil, mm.running, mm.pendingPRsReload)
	}
	// The op finished: the list is re-read (the row is gone on the forge side of gg's memory).
	nm, cmd = mm.Update(opFinishedMsg{res: engine.Result{Summary: "forgot pull request #12"}})
	mm = nm.(Model)
	if cmd == nil || mm.pendingPRsReload || !mm.prsInflight {
		t.Fatalf("after forget: cmd=%v reload=%v inflight=%v", cmd != nil, mm.pendingPRsReload, mm.prsInflight)
	}
}

func TestCopyPRURLKey(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	if _, cmd := m.Update(keyMsg("y")); cmd == nil {
		t.Fatal("y on a PR row copies its URL")
	}
	m.prs[0].URL = ""
	nm, cmd := m.Update(keyMsg("y"))
	if cmd != nil || !strings.Contains(nm.(Model).statusMsg, "no URL") {
		t.Fatalf("no URL: cmd=%v status=%q", cmd != nil, nm.(Model).statusMsg)
	}
}

func TestPRFooterAndMenuAdvertiseTheKeys(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	foot := m.footerLine()
	for _, want := range []string{"[enter] open", "[i]nfo", "[y] copy URL"} {
		if !strings.Contains(foot, want) {
			t.Errorf("footer %q is missing %q", foot, want)
		}
	}
	if strings.Contains(foot, "forget") {
		t.Errorf("footer %q: forget is not available on an open row", foot)
	}
	m.sel[panelPRs] = 1
	if foot = m.footerLine(); !strings.Contains(foot, "[d] forget") {
		t.Errorf("footer on a merged row = %q", foot)
	}
	for _, id := range []string{"pr-open", "pr-hub", "pr-copy", "pr-forget"} {
		if _, ok := actionMenuLabel(id); !ok {
			t.Errorf("no . menu label for %q", id)
		}
	}
	// Off the tab nothing PR-ish is advertised.
	other := loadedModel(t)
	other.width, other.height = 140, 40
	if f := other.footerLine(); strings.Contains(f, "[i]nfo") || strings.Contains(f, "copy URL") {
		t.Errorf("footer off the tab = %q", f)
	}
}

func TestHelpHasAPullRequestsSection(t *testing.T) {
	t.Parallel()
	got := hubText(helpContent())
	for _, want := range []string{"Pull requests panel", "[refresh] prs"} {
		if !strings.Contains(got, want) {
			t.Errorf("help is missing %q", want)
		}
	}
}
