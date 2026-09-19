package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// prModel is a loaded model with a usable forge and the two fixture PRs, the
// Pull requests tab active and focused.
func prModel(t *testing.T) Model {
	t.Helper()
	m := loadedModel(t)
	m.width, m.height = 140, 40
	m.forgeShown, m.forgeProvider, m.prs, m.prsLoaded = true, "github", testPRs(), true
	return m.activateTab(panelPRs)
}

func TestPRTabCyclesOnlyWhenShown(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m = m.activateTab(panelPreviews)
	nm, _ := m.Update(keyMsg("ctrl+right"))
	if got := nm.(Model).activeLeftTab; got != panelBranches {
		t.Fatalf("hidden: ctrl+→ from Previews = %v, want Branches", got)
	}
	m.forgeShown = true
	nm, _ = m.Update(keyMsg("ctrl+right"))
	mm := nm.(Model)
	if mm.activeLeftTab != panelPRs || mm.focus != panelPRs {
		t.Fatalf("shown: ctrl+→ from Previews = tab %v focus %v, want PRs", mm.activeLeftTab, mm.focus)
	}
}

func TestPRTabHeader(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	if strings.Contains(m.leftPanelLabel(panelBranches), "PR") {
		t.Fatalf("hidden: header %q must not advertise PRs", m.leftPanelLabel(panelBranches))
	}
	m.forgeShown = true
	if got := m.leftPanelLabel(panelBranches); !strings.Contains(got, " P PR") {
		t.Fatalf("shown, inactive: header = %q, want a trailing PR mark", got)
	}
	m = m.activateTab(panelPRs)
	if got := m.leftPanelLabel(panelPRs); !strings.Contains(got, "[Pull requests]") {
		t.Fatalf("active header = %q", got)
	}
	// The header is clickable: the last segment activates the tab.
	segs := m.tabSegsFor(panelPRs)
	if len(segs) != 5 || segs[4].p != panelPRs {
		t.Fatalf("segs = %+v", segs)
	}
}

func TestPRRows(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	m.prs = append(m.prs,
		model.PullRequest{Number: 101, Title: "Fork work", Author: "eve", State: model.PRStateOpen, Draft: true,
			Source: "patch-1", SourceRepo: "eve/r", Target: "main", ReviewState: "changes_requested"},
		model.PullRequest{Number: 5, Title: "Gone", State: model.PRStateUnavailable},
	)
	rows := m.prRows()
	if len(rows) != 4 {
		t.Fatalf("rows = %d", len(rows))
	}
	for _, want := range []string{"#7 ", "Add streaming parser", "octocat", "feat/x → main", "✓"} {
		if !strings.Contains(rows[0], want) {
			t.Errorf("row 0 = %q, missing %q", rows[0], want)
		}
	}
	if !strings.HasPrefix(rows[1], "#12   merged") || strings.Contains(rows[1], "✓") {
		t.Errorf("merged row = %q: the state word replaces the review mark", rows[1])
	}
	// The status cell LEADS the row: the left column is narrow and cuts a
	// row's tail, and the state is the one thing a list must never lose.
	if !strings.HasPrefix(rows[0], "#7    ✓") {
		t.Errorf("row 0 = %q, want the review mark right after the number", rows[0])
	}
	for _, want := range []string{"#101", "eve:patch-1 → main", "draft", "✗"} {
		if !strings.Contains(rows[2], want) {
			t.Errorf("row 2 = %q, missing %q", rows[2], want)
		}
	}
	if !strings.HasPrefix(rows[3], "#5    unavailable") {
		t.Errorf("row 3 = %q", rows[3])
	}
	// Numbers share one column: "#7" is padded to "#101".
	if strings.Index(rows[0], "Add") != strings.Index(rows[2], "Fork") {
		t.Errorf("titles are not aligned:\n%q\n%q", rows[0], rows[2])
	}
}

func TestPRSelectionSurvivesAReorderedReload(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	m.sel[panelPRs] = 1
	if got := m.panelSelKey(panelPRs); got != "12" {
		t.Fatalf("sel key = %q, want the PR number", got)
	}
	pr, ok := m.selectedPR()
	if !ok || pr.Number != 12 {
		t.Fatalf("selectedPR = %+v %v", pr, ok)
	}
	rev := []model.PullRequest{m.prs[1], m.prs[0]}
	m.prsGen, m.prsInflight = 9, true
	nm, _ := m.Update(prsLoadedMsg{gen: 9, status: forgeOK(), prs: rev})
	m = nm.(Model)
	if pr, _ := m.selectedPR(); pr.Number != 12 {
		t.Fatalf("after reload the cursor is on #%d, want #12", pr.Number)
	}
}

func TestPRPanelEmptyAndErrorText(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	m.prs, m.prsLoaded = nil, false
	if got := m.emptyPanelText(panelPRs); !strings.Contains(got, "loading") {
		t.Fatalf("before the first list: %q", got)
	}
	m.prsLoaded = true
	if got := m.emptyPanelText(panelPRs); !strings.Contains(got, "no open pull requests") {
		t.Fatalf("empty: %q", got)
	}
	m.prsErr = "HTTP 502"
	if got := m.emptyPanelText(panelPRs); !strings.Contains(got, "github: HTTP 502") || !strings.Contains(got, "[r]") {
		t.Fatalf("error: %q", got)
	}
	// With rows present the failure rides the header, so the list keeps its indices.
	m.prs = testPRs()
	if got := m.leftPanelLabel(panelPRs); !strings.Contains(got, "HTTP 502") {
		t.Fatalf("header with a stale list = %q", got)
	}
	if got := m.emptyPanelText(panelBranches); !strings.Contains(got, "(none)") {
		t.Fatalf("other panels keep (none): %q", got)
	}
}

func TestPRRowsDimNonOpen(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	_, idx := m.panelViewWindowed(panelPRs, 20)
	decos := m.prDecorators(idx)
	if len(decos) != 2 || decos[0] != nil || decos[1] == nil {
		t.Fatalf("decos = %v: only the merged row is dimmed", decos)
	}
}

func TestPRPanelProtoName(t *testing.T) {
	t.Parallel()
	if got := panelProtoName(panelPRs); got != "prs" {
		t.Fatalf("proto name = %q", got)
	}
	if p, ok := panelFromProtoName("prs"); !ok || p != panelPRs {
		t.Fatalf("round trip = %v %v", p, ok)
	}
}

func TestPRTabRendersInTheFrame(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	out := m.View()
	for _, want := range []string{"[Pull requests]", "#7", "Add streaming parser", "merged"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame is missing %q", want)
		}
	}
}
