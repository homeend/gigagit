package tui

import (
	"fmt"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// runCopy runs a copy cmd (a batch's members too), so the fake clipboard
// receives what it copies.
func runCopy(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	if b, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range b {
			runCopy(t, c)
		}
	}
}

// withClip makes m's clipboard a recorder.
func withClip(m Model, into *string) Model {
	m.clipWrite = func(_ io.Writer, s string) (string, error) { *into = s; return "fake", nil }
	return m
}

// Task 11: L on a PR row asks for the PR's link pair off the UI thread.
func TestPRListLCopiesThePRLink(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	_, cmd := m.Update(keyMsg("L"))
	if cmd == nil {
		t.Fatal("L on a PR row did nothing")
	}
	msg, ok := cmd().(prLinkMsg)
	if !ok || msg.n != 7 {
		t.Fatalf("L → %#v", msg)
	}
	var copied string
	m = withClip(m, &copied)
	_, ccmd := m.Update(prLinkMsg{n: 7, source: "refs/gg/pr/7", target: "main"})
	if ccmd == nil {
		t.Fatal("no copy")
	}
	runCopy(t, ccmd)
	if !strings.HasSuffix(copied, "@main...refs/gg/pr/7") {
		t.Fatalf("copied %q", copied)
	}
	if !strings.Contains(m.footerLine(), "[L] copy link") {
		t.Errorf("footer %q lacks [L] copy link", m.footerLine())
	}
	if l, ok := actionMenuLabel("pr-link"); !ok || l != "Copy pull request link" {
		t.Errorf("menu label %q %v", l, ok)
	}
}

func TestUnfetchedPRLinkSaysOpenItFirst(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	nm, cmd := m.Update(prLinkMsg{n: 8, err: fmt.Errorf("%w: gg pr fetch 8", domain.ErrPRNotFetched)})
	if cmd != nil || nm.(Model).statusMsg != "PR #8 is not fetched: press enter to open it first" {
		t.Fatalf("cmd=%v status=%q", cmd != nil, nm.(Model).statusMsg)
	}
}

// Task 11: an open PR's . menu copies the whole PR's link — the pair on
// screen, no round trip.
func TestOpenPRMenuHasCopyPRLink(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	var copied string
	m = withClip(m, &copied)
	row, ok := rowByID(availableActions(m), "pr-link-open")
	if !ok || row.label != "Copy pull request link" {
		t.Fatalf("row %+v ok=%v", row, ok)
	}
	_, cmd := row.run(m)
	runCopy(t, cmd)
	if !strings.HasSuffix(copied, "@main...feat/x") {
		t.Fatalf("copied %q", copied)
	}
}

// Task 11: the PR details popup copies the link with L.
func TestPRHubLCopiesTheLink(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	nm, _ := m.openPRHub(m.prs[0])
	hub, ok := nm.topLayer().(*prHubPopup)
	if !ok {
		t.Fatalf("top layer %T", nm.topLayer())
	}
	_, cmd := hub.update(nm, keyMsg("L"))
	if cmd == nil {
		t.Fatal("L in the hub did nothing")
	}
	if msg, ok := cmd().(prLinkMsg); !ok || msg.n != 7 {
		t.Fatalf("L → %#v", msg)
	}
}
