package tui

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
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

// repoTop is m's repository top level.
func repoTop(t *testing.T, m Model) string {
	t.Helper()
	top, err := m.svc.Repo().TopLevel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return top
}

// prLinkSteerModel is previewSteerModel with PR #7 (feat/x into main)
// fetched into refs/gg/pr/7 and listed.
func prLinkSteerModel(t *testing.T) (Model, string) {
	t.Helper()
	m, dir := previewSteerModel(t)
	runGit(t, repoTop(t, m), "update-ref", "refs/gg/pr/7", "feat/x")
	m.forgeShown, m.prs = true, testPRs()
	return m, dir
}

// Task 13: a PR link with a file opens the PR's VIEW (prNumber set), then
// the file's diff, then lands the line.
func TestAPRLinkOpensThePRView(t *testing.T) {
	t.Parallel()
	m, dir := prLinkSteerModel(t)
	c := steer.Command{ID: "pr-1", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "refs/gg/pr/7", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	if m.pendingSteer == nil || m.pendingSteer.prNumber != 7 {
		t.Fatalf("pendingSteer = %+v, want PR #7's stage", m.pendingSteer)
	}
	m = drainMsgs(t, m, cmd, 8)
	if m.previewOpen == nil || m.previewOpen.prNumber != 7 {
		t.Fatalf("previewOpen = %+v, want PR #7's view", m.previewOpen)
	}
	if m.diffLayer() == nil {
		t.Fatal("the file's diff must be open")
	}
	rep := readOneReply(t, dir, "pr-1")
	if !rep.OK || !strings.Contains(rep.Detail, "a.txt:1") {
		t.Errorf("reply = %+v", rep)
	}
}

// Task 13: the link's base spelling need not be the view's: the gate is the
// PR's number.
func TestAPRLinkWithABaseSpellingOfItsOwnStillDrains(t *testing.T) {
	t.Parallel()
	m, dir := prLinkSteerModel(t)
	out, err := exec.Command("git", "-C", repoTop(t, m), "rev-parse", "main").Output()
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSpace(string(out))
	c := steer.Command{ID: "pr-2", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "refs/gg/pr/7", Target: base},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	m = drainMsgs(t, m, cmd, 8)
	if m.previewOpen == nil || m.previewOpen.prNumber != 7 || m.diffLayer() == nil {
		t.Fatalf("previewOpen = %+v diff=%v", m.previewOpen, m.diffLayer() != nil)
	}
	if rep := readOneReply(t, dir, "pr-2"); !rep.OK {
		t.Errorf("reply = %+v", rep)
	}
}

// Task 13: a file-less PR link opens the PR's view.
func TestAFileLessPRLinkOpensThePRView(t *testing.T) {
	t.Parallel()
	m, dir := prLinkSteerModel(t)
	c := steer.Command{ID: "pr-3", Cmd: "navigate",
		Target: &steer.Target{State: "preview", Source: "refs/gg/pr/7", Target: "main"}, Wait: true}
	m, cmd := m.applySteer(c)
	m = drainMsgs(t, m, cmd, 8)
	if m.previewOpen == nil || m.previewOpen.prNumber != 7 {
		t.Fatalf("previewOpen = %+v", m.previewOpen)
	}
	if rep := readOneReply(t, dir, "pr-3"); !rep.OK || !strings.Contains(rep.Detail, "pull request #7") {
		t.Errorf("reply = %+v", rep)
	}
}

// Task 13: a PR the list does not hold opens as a plain merge preview, and
// says so.
func TestAnUnlistedPRLinkOpensAMergePreview(t *testing.T) {
	t.Parallel()
	m, _ := prLinkSteerModel(t)
	m.prs = nil
	c := steer.Command{ID: "pr-4", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "refs/gg/pr/7", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	if !strings.Contains(m.statusMsg, "PR #7 is not in the pull request list here") {
		t.Errorf("status = %q", m.statusMsg)
	}
	m = drainMsgs(t, m, cmd, 8)
	if m.previewOpen == nil || m.previewOpen.prNumber != 0 || m.previewOpen.source != "refs/gg/pr/7" {
		t.Fatalf("previewOpen = %+v", m.previewOpen)
	}
}

// Task 13: `gg open <PR link>` at TUI start waits for the first PR read to
// answer (cached rows, a live list, or "no forge") — the landing needs the
// row to open the PR's view instead of a plain merge preview.
func TestStartAtAPRLinkWaitsForThePRList(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m.startAt = mustLink(t, "gg://gigagit@main...refs/gg/pr/7")
	m.startAtPending, m.startAtPreviewsSeen = true, true
	m.forgeProbeKicked, m.prsAnswered = true, false // the startup read is out
	if m.startAtReady() {
		t.Fatal("a PR link was consumed before any PR read answered")
	}
	nm, _ := m.Update(prsCachedMsg{gen: m.prsGen, provider: "github", prs: testPRs()})
	// The rows' arrival is what consumes it (startAtReady is checked after
	// every dispatch).
	if mm := nm.(Model); !mm.prsAnswered || mm.startAtPending {
		t.Fatalf("after the cached rows: answered=%v still pending=%v", mm.prsAnswered, mm.startAtPending)
	}
	m.startAt = mustLink(t, "gg://gigagit@main...feat/x")
	if !m.startAtReady() {
		t.Fatal("a plain preview link must not wait for the PR list")
	}
}
