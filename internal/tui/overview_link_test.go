package tui

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// clipModel makes m's clipboard a slice and its link history a temp dir.
func clipModel(t *testing.T, m Model) (Model, *[]string) {
	t.Helper()
	m.svc.UseLinkHistDir(t.TempDir())
	var wrote []string
	m.clipWrite = func(_ io.Writer, s string) (string, error) {
		wrote = append(wrote, s)
		return "fake", nil
	}
	return m, &wrote
}

// sessionTreePopup is ctrl+\ on its tree tab over an attached session
// overview ov-1 (anchoring a.txt:1), the cursor on the overview row.
func sessionTreePopup(t *testing.T) (Model, *sessionsPopup) {
	t.Helper()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	m, _ = m.openSessionsPopup(false)
	p := m.topLayer().(*sessionsPopup)
	p.tab = tabFiles
	p.refresh(m)
	for i, n := range p.nodes {
		if n.kind == nodeOverview && n.doc.id() == "ov-1" {
			p.sel = i
		}
	}
	return m, p
}

func TestCtrlBackslashYCopiesSessionOverviewLink(t *testing.T) {
	t.Parallel()
	m, p := sessionTreePopup(t)
	m, wrote := clipModel(t, m)
	m, cmd := p.update(m, keyMsg("y"))
	m = drainCmds(t, m, cmd)
	want, err := domain.SessionOverviewLink(m.currentWorktree, "ov-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(*wrote) != 1 || (*wrote)[0] != want {
		t.Fatalf("clipboard = %q, want %q", *wrote, want)
	}
	h := m.svc.LinkHistory(context.Background())
	if len(h) != 1 || h[0].Desc != "overview: ov-1" {
		t.Fatalf("history = %+v", h)
	}
}

func TestCtrlBackslashYOnChildRowDoesNothing(t *testing.T) {
	t.Parallel()
	m, p := sessionTreePopup(t)
	m, wrote := clipModel(t, m)
	n := p.nodes[p.sel]
	m.openFiles.setExpanded(m.currentWorktree, n.expandKey(), true)
	p.refresh(m)
	child := -1
	for i, c := range p.nodes {
		if c.kind == nodeChild {
			child = i
		}
	}
	if child < 0 {
		t.Fatal("no child row")
	}
	p.sel = child
	m, cmd := p.update(m, keyMsg("y"))
	m = drainCmds(t, m, cmd)
	if len(*wrote) != 0 || p.confirmKill != "" {
		t.Fatalf("clipboard = %q confirmKill = %q", *wrote, p.confirmKill)
	}
}

func TestCopyLinkMemoryOverviewRefuses(t *testing.T) {
	t.Parallel()
	m, ov := treeModel(t) // a memory store: nothing attached
	m, wrote := clipModel(t, m)
	u, cmd := m.overviewLinkRow(ov).run(m)
	m = drainCmds(t, u.(Model), cmd)
	if len(*wrote) != 0 || !strings.Contains(m.statusMsg, "not saved to a worktree") {
		t.Fatalf("clipboard = %q status = %q", *wrote, m.statusMsg)
	}
}

func TestCopyLinkCommitOverviewCopiesItsLink(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = openCommitOverviewT(t, m, id)
	m, wrote := clipModel(t, m)
	d := m.splitLayer().left.doc()
	u, cmd := m.overviewLinkRow(d).run(m)
	m = drainCmds(t, u.(Model), cmd)
	want, _ := m.svc.OverviewLink(context.Background(), id)
	if len(*wrote) != 1 || (*wrote)[0] != want {
		t.Fatalf("clipboard = %q, want %q", *wrote, want)
	}
}

func TestCopyLinkReviewOverviewCopiesReviewLink(t *testing.T) {
	t.Parallel()
	m, _ := reviewWithOverview(t)
	id := m.filesReview.id
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	m, wrote := clipModel(t, m)
	d := m.splitLayer().left.doc()
	u, cmd = m.overviewLinkRow(d).run(m)
	m = drainCmds(t, u.(Model), cmd)
	if len(*wrote) != 1 || !strings.HasSuffix((*wrote)[0], "?review="+id) {
		t.Fatalf("clipboard = %q", *wrote)
	}
}

func TestOpenOverviewMenusHaveCopyLink(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = openCommitOverviewT(t, m, id)
	sv := m.splitLayer()
	has := func(rows []actionRow) bool {
		for _, r := range rows {
			if r.id == "overview-copy-link" {
				return true
			}
		}
		return false
	}
	if !has(m.splitOverviewRows(sv, sv.left.doc())) {
		t.Fatal("split overview menu has no Copy gg link")
	}
	if !has(m.overviewRows()) {
		t.Fatal("overview menu has no Copy gg link")
	}
}
