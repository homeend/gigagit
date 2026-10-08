package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
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
	m.forgeGen = 3
	_, cmd := m.Update(keyMsg("L"))
	if cmd == nil {
		t.Fatal("L on a PR row did nothing")
	}
	msg, ok := cmd().(prLinkMsg)
	if !ok || msg.n != 7 || msg.gen != 3 {
		t.Fatalf("L → %#v", msg)
	}
	var copied string
	m = withClip(m, &copied)
	_, ccmd := m.Update(prLinkMsg{n: 7, source: "refs/gg/pr/7", target: "main", gen: 3})
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

// B6: a repo switch between L and the answer drops the old repo's pair —
// copying it would hand out a link to the wrong repository.
func TestPRLinkAnswerFromALeftRepoIsDropped(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	var copied string
	m = withClip(m, &copied)
	m.forgeGen = 2
	nm, cmd := m.Update(prLinkMsg{n: 7, source: "refs/gg/pr/7", target: "main", gen: 1})
	if cmd != nil {
		runCopy(t, cmd)
	}
	if copied != "" || strings.Contains(nm.(Model).statusMsg, "copied") {
		t.Fatalf("a stale answer copied %q (status %q)", copied, nm.(Model).statusMsg)
	}
}

// B5: a file-less link to an unlisted PR keeps the notice that says why it
// opened as a merge preview (steerNotice used to overwrite it) — whether
// the TUI started at it or an agent moved the focus.
func TestAFileLessUnlistedPRLinkKeepsItsNotice(t *testing.T) {
	t.Parallel()
	for _, c := range []steer.Command{
		{Cmd: "navigate", Target: &steer.Target{State: "preview", Source: "refs/gg/pr/7", Target: "main"}},
		{ID: "pr-5", Cmd: "navigate", Target: &steer.Target{State: "preview", Source: "refs/gg/pr/7", Target: "main"}, Wait: true},
	} {
		m, _ := prLinkSteerModel(t)
		m.prs = nil
		m, _ = m.applySteer(c)
		if !strings.Contains(m.statusMsg, "PR #7 is not in the pull request list here") {
			t.Errorf("start-at=%v: status = %q", startAtOrigin(c), m.statusMsg)
		}
	}
}

// prLinkUnfetchedModel lists PR #7 but holds no refs/gg/pr/7.
func prLinkUnfetchedModel(t *testing.T) (Model, string) {
	t.Helper()
	m, dir := previewSteerModel(t)
	m.forgeShown, m.prs = true, testPRs()
	return m, dir
}

// prLinkFileCmd is a navigate to PR #7's a.txt:1.
func prLinkFileCmd(id string) steer.Command {
	return steer.Command{ID: id, Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "refs/gg/pr/7", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
}

// B4: a listed PR whose head is not local is fetched first — the enter
// path — instead of opening an empty view.
func TestAPRLinkToAnUnfetchedPRFetchesIt(t *testing.T) {
	t.Parallel()
	m, _ := prLinkUnfetchedModel(t)
	m, cmd := m.applySteer(prLinkFileCmd("pr-6"))
	if cmd == nil {
		t.Fatal("no open")
	}
	if got := cmd(); func() bool { msg, ok := got.(prFetchReadyMsg); return !ok || msg.pr.Number != 7 }() {
		t.Fatalf("landing → %T, want the PR's fetch", got)
	}
	if m.pendingSteer == nil || m.pendingSteer.prNumber != 7 {
		t.Fatalf("pendingSteer = %+v", m.pendingSteer)
	}
}

// B4: a fetch that cannot run answers the parked landing at once.
func TestAFailedPRLandingFetchAnswersTheAgent(t *testing.T) {
	t.Parallel()
	m, dir := prLinkUnfetchedModel(t)
	m, _ = m.applySteer(prLinkFileCmd("pr-7"))
	nm, cmd := m.Update(prFetchReadyMsg{pr: testPRs()[0], err: errors.New("no forge"), gen: m.forgeGen})
	if nm.(Model).pendingSteer != nil {
		t.Fatal("the landing is still parked")
	}
	if cmd != nil {
		drainMsgs(t, nm.(Model), cmd, 4)
	}
	if rep := readOneReply(t, dir, "pr-7"); rep.OK || !strings.Contains(rep.Error, "no forge") {
		t.Fatalf("reply = %+v", rep)
	}
}

// B4: the whole chain — fetch op armed, the landing stays parked (held
// outside steerPendingTTL while the fetch runs), the op
// finishes, the PR's view opens and the line lands.
func TestAPRLinkToAnUnfetchedPRLandsAfterTheFetch(t *testing.T) {
	t.Parallel()
	m, dir := prLinkUnfetchedModel(t)
	m, _ = m.applySteer(prLinkFileCmd("pr-8"))
	m.pendingSteer.at = time.Now().Add(-4 * time.Second)
	nm, _ := m.Update(prFetchReadyMsg{pr: testPRs()[0], op: engine.FetchPRHead{Remote: "origin", Refspec: "refs/pull/7/head", Number: 7}, gen: m.forgeGen})
	m = nm.(Model)
	if m.pendingPROpen == nil || m.pendingSteer == nil {
		t.Fatalf("pendingPROpen=%v pendingSteer=%+v", m.pendingPROpen != nil, m.pendingSteer)
	}
	if m, _ = m.expirePendingSteer(time.Now().Add(2 * time.Second)); m.pendingSteer == nil {
		t.Fatal("the landing expired while its fetch ran")
	}
	runGit(t, repoTop(t, m), "update-ref", "refs/gg/pr/7", "feat/x") // what the fetch op would have written
	nm, cmd := m.Update(opFinishedMsg{res: engine.Result{Summary: "fetched"}})
	m = drainMsgs(t, nm.(Model), cmd, 12)
	if m.previewOpen == nil || m.previewOpen.prNumber != 7 || m.diffLayer() == nil {
		t.Fatalf("previewOpen = %+v diff=%v", m.previewOpen, m.diffLayer() != nil)
	}
	if rep := readOneReply(t, dir, "pr-8"); !rep.OK {
		t.Fatalf("reply = %+v", rep)
	}
}

// prFetchOp7 is PR #7's fetch op as PRFetchOp would resolve it.
func prFetchOp7() engine.FetchPRHead {
	return engine.FetchPRHead{Remote: "origin", Refspec: "refs/pull/7/head", Number: 7}
}

// The PR's fetch op is network time: a landing parked on it never expires
// while it runs (steerPendingTTL is 5s), and expires as before after.
func TestAPRLandingDoesNotExpireWhileItsFetchRuns(t *testing.T) {
	t.Parallel()
	m, _ := prLinkUnfetchedModel(t)
	m, _ = m.applySteer(prLinkFileCmd("pr-9"))
	pr := testPRs()[0]
	m.pendingPROpen, m.running = &pr, true
	m.pendingSteer.at = time.Now().Add(-10 * time.Second)
	m, _ = m.expirePendingSteer(time.Now())
	if m.pendingSteer == nil {
		t.Fatal("the landing expired while its fetch ran")
	}
	m.running = false
	m.pendingSteer.at = time.Now().Add(-10 * time.Second)
	if m, _ = m.expirePendingSteer(time.Now()); m.pendingSteer != nil {
		t.Fatal("with no fetch running the landing must expire")
	}
}

// Enter on PR #7 while an agent's landing on #7 is fetching (or the other
// way round): the fetch already running opens the view the landing waits
// for — the second resolve must not fail the landing.
func TestEnterOnTheSamePRDuringALandingFetchKeepsTheLanding(t *testing.T) {
	t.Parallel()
	m, dir := prLinkUnfetchedModel(t)
	m, _ = m.applySteer(prLinkFileCmd("pr-10"))
	pr := testPRs()[0]
	m.pendingPROpen, m.running = &pr, true
	nm, _ := m.Update(prFetchReadyMsg{pr: pr, op: prFetchOp7(), gen: m.forgeGen})
	if nm.(Model).pendingSteer == nil {
		t.Fatal("the landing was failed by a fetch of the same PR")
	}
	if _, ok := steer.AwaitReply(dir, "pr-10", 200*time.Millisecond); ok {
		t.Fatal("a reply was written")
	}
	m.pendingPROpen = nil // an unrelated op runs
	nm, cmd := m.Update(prFetchReadyMsg{pr: pr, op: prFetchOp7(), gen: m.forgeGen})
	if nm.(Model).pendingSteer != nil {
		t.Fatal("an unrelated op must fail the landing")
	}
	if cmd != nil {
		drainMsgs(t, nm.(Model), cmd, 4)
	}
	if rep := readOneReply(t, dir, "pr-10"); rep.OK {
		t.Fatalf("reply = %+v", rep)
	}
}

// A file-less landing has replied already: when the fetch cannot start, the
// status line is the only place that says why.
func TestAFileLessPRLandingSaysWhyWhenBusy(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	m.running = true
	nm, _ := m.Update(prFetchReadyMsg{pr: testPRs()[0], op: prFetchOp7(), gen: m.forgeGen})
	if got := nm.(Model).statusMsg; got != "PR #7: an operation is running — open it again when it finishes" {
		t.Fatalf("status = %q", got)
	}
}

// A resolve that lands after a repo switch must not start the old repo's
// fetch in the new one.
func TestAPRFetchReadyFromALeftRepoIsDropped(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	m.forgeGen = 2
	nm, cmd := m.Update(prFetchReadyMsg{pr: testPRs()[0], op: prFetchOp7(), gen: 1})
	if mm := nm.(Model); cmd != nil || mm.pendingPROpen != nil || mm.running {
		t.Fatalf("cmd=%v pendingPROpen=%v running=%v", cmd != nil, mm.pendingPROpen, mm.running)
	}
}

// Follow-ups 4: # with a link to a PR the list holds but this repo has not
// fetched resolves (the resolver no longer refuses it) and the landing
// fetches the PR first.
func TestPastingAnUnfetchedPRLinkFetchesThePR(t *testing.T) {
	t.Parallel()
	m, _ := prLinkUnfetchedModel(t)
	m.statePath = filepath.Join(t.TempDir(), "repos.toml")
	m, cmd := pasteLink(t, m, linkTo(repoTop(t, m), "/a.txt@main...refs/gg/pr/7:1"))
	m, cmd = send(m, cmd())
	if p := layerOf[*gotoCommitPopup](m); p != nil {
		t.Fatalf("the prompt stayed open: %q", p.err)
	}
	if m.pendingSteer == nil || m.pendingSteer.prNumber != 7 {
		t.Fatalf("pendingSteer = %+v", m.pendingSteer)
	}
	if cmd == nil {
		t.Fatal("no open")
	}
	if got := cmd(); func() bool { msg, ok := got.(prFetchReadyMsg); return !ok || msg.pr.Number != 7 }() {
		t.Fatalf("landing → %T, want the PR's fetch", got)
	}
}
