package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

func TestSteerCommandForLink(t *testing.T) {
	t.Parallel()
	pv := func(s string) model.Link {
		l, err := model.ParseLink(s)
		if err != nil {
			t.Fatalf("ParseLink(%q): %v", s, err)
		}
		return l
	}
	c, ok := steerCommandForLink(pv("gg://r/a.txt@main...feat/x:4"))
	if !ok || c.Cmd != "navigate" || c.File != "a.txt" {
		t.Fatalf("preview file link = %+v,%v", c, ok)
	}
	if c.Target == nil || c.Target.State != "preview" || c.Target.Source != "feat/x" || c.Target.Target != "main" {
		t.Fatalf("target = %+v", c.Target)
	}
	if c.Line == nil || c.Line.No != 4 || c.Line.Side != "new" {
		t.Fatalf("line = %+v", c.Line)
	}
	if c.ID != "" || c.Wait {
		t.Errorf("a startup navigate must carry no id and no wait: %+v", c)
	}
	// A repo-level preview link reveals the entry: no file, no line.
	c, ok = steerCommandForLink(pv("gg://r@main...feat/x"))
	if !ok || c.File != "" || c.Line != nil || c.Target.State != "preview" {
		t.Fatalf("preview reveal = %+v,%v", c, ok)
	}
	// A hunk link is REFUSED: gg open lowers it to a line before launching.
	if _, ok := steerCommandForLink(pv("gg://r/a.txt@main...feat/x#2")); ok {
		t.Error("a #hunk link must be refused here, not silently ignored")
	}
	// A commit link with a full sha reveals the commit.
	sha := strings.Repeat("a", 40)
	c, ok = steerCommandForLink(pv("gg://r@" + sha))
	if !ok || c.Commit != sha {
		t.Fatalf("commit link = %+v,%v", c, ok)
	}
	// A working-tree file:line link.
	c, ok = steerCommandForLink(pv("gg://r/a.txt:9"))
	if !ok || c.Target == nil || c.Target.State != "unstaged" || c.Line.No != 9 {
		t.Fatalf("worktree link = %+v,%v", c, ok)
	}
}

// TestSteerCommandForLinkBuildsRefAndPair pins the step-6 regression: before
// this task, `gg open 'gg://repo@ref:branch'` with no live session RESOLVED
// the link, found no live session, launched a fresh TUI in that checkout (a
// real side effect), and only THEN answered the generic "names no place gg
// can open" refusal — the len(Commit) < 40 guard failed closed only by
// accident, since a ref/pair link's Commit is always empty. Both arms must
// build a real command instead.
func TestSteerCommandForLinkBuildsRefAndPair(t *testing.T) {
	t.Parallel()
	c, ok := steerCommandForLink(mustLink(t, "gg://r@ref:feat/x"))
	if !ok || c.Target == nil || c.Target.State != "ref" || c.Target.Ref != "feat/x" {
		t.Fatalf("ref link = %+v,%v", c, ok)
	}
	if c.File != "" || c.Line != nil {
		t.Errorf("a repo-level ref link must carry no file: %+v", c)
	}

	c, ok = steerCommandForLink(mustLink(t, "gg://r/a.txt@ref:feat/x:9"))
	if !ok || c.File != "a.txt" || c.Target == nil || c.Target.State != "ref" || c.Target.Ref != "feat/x" {
		t.Fatalf("ref file link = %+v,%v", c, ok)
	}
	if c.Line == nil || c.Line.No != 9 || c.Line.Side != "new" {
		t.Fatalf("line = %+v", c.Line)
	}

	c, ok = steerCommandForLink(mustLink(t, "gg://r@main..feat/x"))
	if !ok || c.Target == nil || c.Target.State != "pair" || c.Target.A != "main" || c.Target.B != "feat/x" {
		t.Fatalf("pair link = %+v,%v", c, ok)
	}
	if c.File != "" || c.Line != nil {
		t.Errorf("a repo-level pair link must carry no file: %+v", c)
	}

	c, ok = steerCommandForLink(mustLink(t, "gg://r/a.txt@main..feat/x:3"))
	if !ok || c.File != "a.txt" || c.Target == nil || c.Target.State != "pair" || c.Target.A != "main" || c.Target.B != "feat/x" {
		t.Fatalf("pair file link = %+v,%v", c, ok)
	}
	if c.Line == nil || c.Line.No != 3 {
		t.Fatalf("line = %+v", c.Line)
	}
}

// TestStartAtRefLinkLandsOnTheTip is the end-to-end half of the step-6
// regression: applySteer must actually land the command steerCommandForLink
// built, not just build it.
func TestStartAtRefLinkLandsOnTheTip(t *testing.T) {
	t.Parallel()
	m, _ := previewSteerModel(t)
	c, ok := steerCommandForLink(mustLink(t, "gg://gigagit@ref:main"))
	if !ok {
		t.Fatal("steerCommandForLink must build a command for a ref link")
	}
	m, cmd := m.applySteer(c)
	m = drainMsgs(t, m, cmd, 6)
	if m.filesView == nil {
		t.Fatal("the ref link must have opened the tip's files")
	}
	if strings.Contains(m.statusMsg, "no place gg can open") {
		t.Error("must not fall back to the generic refusal")
	}
}

// TestSteerCommandForLinkAcceptsALineLessFile pins the OTHER producer: the
// --at startup gate (gg open with no live session launches the TUI on the
// link). It refused l.Line < 1, so a line-less link launched gg nowhere in
// particular instead of on the file.
func TestSteerCommandForLinkAcceptsALineLessFile(t *testing.T) {
	t.Parallel()
	c, ok := steerCommandForLink(model.Link{
		Repo: model.LinkRepo{Abs: "/repo"}, Path: "a.txt",
		Target: model.LinkTarget{State: model.StateUnstaged},
		Side:   model.NoteSideNew,
	})
	if !ok {
		t.Fatal("steerCommandForLink refused a line-less file link")
	}
	if c.File != "a.txt" || c.Line != nil {
		t.Fatalf("command = %+v, want File a.txt and no Line", c)
	}

	// The PREVIEW arm carries the same rule in a second place, and a rule with
	// one test is a rule enforced in one place: reverting only the preview
	// hunk left every other test in this package green.
	pl, err := model.ParseLink("gg://r/a.txt@main...feat/x")
	if err != nil {
		t.Fatal(err)
	}
	c, ok = steerCommandForLink(pl)
	if !ok {
		t.Fatal("steerCommandForLink refused a line-less preview file link")
	}
	if c.File != "a.txt" || c.Line != nil {
		t.Fatalf("preview command = %+v, want File a.txt and no Line", c)
	}
	if c.Target == nil || c.Target.State != "preview" || c.Target.Source != "feat/x" || c.Target.Target != "main" {
		t.Fatalf("preview target = %+v, want main...feat/x", c.Target)
	}
}

// TestStartAtReadyPredicate exercises startAtReady() directly, field by
// field: it must require startAtPending, m.ready (some data has arrived —
// guards the window before the startup fan-out has even begun), opsIdle
// (neither m.loading nor m.running — applySteer's steerRefusal refuses ANY
// navigate while either is true) and a window size; a PREVIEW link
// additionally requires startAtPreviewsSeen, which a non-preview link does
// not.
func TestStartAtReadyPredicate(t *testing.T) {
	t.Parallel()
	base := Model{startAtPending: true, ready: true, width: 100}
	if !base.startAtReady() {
		t.Fatalf("a ready non-preview link should be ready: %+v", base)
	}
	cases := []struct {
		name string
		mut  func(Model) Model
	}{
		{"not pending", func(m Model) Model { m.startAtPending = false; return m }},
		{"nothing has loaded yet", func(m Model) Model { m.ready = false; return m }},
		{"a source is still loading", func(m Model) Model { m.loading = true; return m }},
		{"an operation is running", func(m Model) Model { m.running = true; return m }},
		{"no window size yet", func(m Model) Model { m.width = 0; return m }},
	}
	for _, c := range cases {
		if got := c.mut(base).startAtReady(); got {
			t.Errorf("%s: startAtReady() = true, want false", c.name)
		}
	}

	preview := base
	preview.startAt = model.Link{Target: model.LinkTarget{Preview: &model.LinkPreview{Source: "feat/x", Target: "main"}}}
	if preview.startAtReady() {
		t.Error("a preview link with no previews read yet must not be ready")
	}
	preview.startAtPreviewsSeen = true
	if !preview.startAtReady() {
		t.Error("a preview link with the previews read landed must be ready")
	}
}

// startAtPreviewFixture builds a repo where feat/x brings a.txt into main,
// with one saved preview, and the --at link landing on it. It returns a FRESH
// unloaded Model (New(svc) only) so the caller controls message ordering.
func startAtPreviewFixture(t *testing.T) (Model, model.Link) {
	t.Helper()
	dir, repo := newRepoDir(t)
	runGit(t, dir, "checkout", "-q", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "add a")
	runGit(t, dir, "checkout", "-q", "main")
	previewsDir := t.TempDir()
	svc := domain.New(repo)
	svc.UsePreviewsDir(previewsDir)
	if _, err := svc.PreviewAdd(context.Background(), "feat/x", "main", "login"); err != nil {
		t.Fatal(err)
	}
	at, err := model.ParseLink("gg://gigagit/a.txt@main...feat/x:1")
	if err != nil {
		t.Fatal(err)
	}
	m := New(svc)
	m.startAt, m.startAtPending = at, true
	return m, at
}

// headSHA reads dir's HEAD commit as a full 40-hex sha.
func headSHA(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// driveStartupFanOut processes the real production bootstrap message
// (configReadyMsg, from bootstrapCmd) and returns the model exactly where
// real `gg`/`gg open` startup lands right after config loads: the
// per-source fan-out (branches, status, notes, identity, previews, …) is
// freshly kicked off and IN FLIGHT (m.loading == true). This is deliberate:
// real startup (Init → bootstrapCmd → configReadyMsg → reloadAllCmd) never
// runs the legacy loadCmd()/dataLoadedMsg path — only reRoot, repo-switch
// and the conflict process do — so a test built on loadCmd() alone would not
// catch a --at that fires too early against the real binary.
func driveStartupFanOut(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(m.bootstrapCmd()())
	m = updated.(Model)
	if !m.loading {
		t.Fatal("setup: the startup fan-out must be in flight right after configReadyMsg")
	}
	return m, cmd
}

// drainDeep is drainMsgs for a NESTED batch: the real startup command is
// tea.Batch(themeCmd, reloadAllCmd's OWN ~10-item batch, startWatchCmd,
// steerCmd) — a batch inside a batch — and drainMsgs only unwraps one level.
// This does a breadth-first unwrap of however many levels a real command
// tree carries, stopping after maxSteps real (non-batch) messages have been
// applied, and returns whatever is left queued so a test can inspect a
// genuine MID-FAN-OUT state and then resume draining it.
func drainDeep(t *testing.T, m Model, cmd tea.Cmd, maxSteps int) (Model, tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	applied := 0
	for len(queue) > 0 && applied < maxSteps {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, bc := range batch {
				if bc != nil {
					queue = append(queue, bc)
				}
			}
			continue // unwrapping a batch is not itself a real message
		}
		applied++
		updated, next := m.Update(msg)
		m = updated.(Model)
		if next != nil {
			queue = append(queue, next)
		}
	}
	var rest tea.Cmd
	if len(queue) > 0 {
		rest = tea.Batch(queue...)
	}
	return m, rest
}

// The real startup fan-out (not merely the previews read) must finish before
// --at fires: applySteer refuses ANY navigate while m.loading is true, and a
// --at fired into that refusal is never retried (see startAtReady's doc).
// This is the regression the !m.loading precondition exists for: checked at
// a genuine MID-fan-out point (some sources landed, m.ready == true; others
// still pending, m.loading == true) — not merely the instant before any
// source has landed at all, which !m.ready alone would already cover.
func TestStartAtWaitsForTheRealStartupFanOutToFinish(t *testing.T) {
	t.Parallel()
	m, _ := startAtPreviewFixture(t)
	m, fanOut := driveStartupFanOut(t, m)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	if !m.startAtPending {
		t.Fatal("--at must not be consumed while the startup fan-out is still in flight")
	}

	// Apply just enough real messages to land AT LEAST ONE source (so
	// m.ready flips true) while leaving the other ~9 pending (m.loading
	// stays true) — the genuine mid-fan-out state. The startup batch is
	// tea.Batch(themeCmd, reloadAllCmd's own ~10-item batch, startWatchCmd,
	// steerCmd), applied breadth-first with batches unwrapped for free (not
	// counted): 4 real (non-batch) hops is themeCmd, startWatchCmd,
	// steerCmd, then the FIRST of reloadAllCmd's ~10 per-source reads — one
	// source landed, the rest still queued. Asserted below rather than
	// assumed.
	var rest tea.Cmd
	m, rest = drainDeep(t, m, fanOut, 4)
	if !m.ready {
		t.Fatal("setup: at least one source should have landed by now")
	}
	if !m.loading {
		t.Fatal("setup: the fan-out should still be partway through")
	}
	if !m.startAtPending {
		t.Fatal("--at must not be consumed while a sibling source is still loading")
	}

	m, _ = drainDeep(t, m, rest, 200)
	if m.loading {
		t.Fatal("setup: drainDeep should have completed the fan-out")
	}
	if m.startAtPending {
		t.Error("--at must be consumed once the fan-out and a window size have both landed")
	}
	if m.previewOpen == nil || m.previewOpen.source != "feat/x" {
		t.Fatalf("previewOpen = %+v, want the --at pair", m.previewOpen)
	}
}

// The other order: the fan-out finishes BEFORE a window size ever arrives —
// the last-landing precondition is then the size, not the fan-out.
func TestStartAtFiresWhenAWindowSizeArrivesAfterTheFanOutAlreadyFinished(t *testing.T) {
	t.Parallel()
	m, _ := startAtPreviewFixture(t)
	m, fanOut := driveStartupFanOut(t, m)
	m, _ = drainDeep(t, m, fanOut, 200)
	if m.loading {
		t.Fatal("setup: drainMsgs should have completed the fan-out")
	}
	if !m.startAtPending {
		t.Fatal("--at must not be consumed before a window size has landed")
	}

	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	if m.startAtPending {
		t.Error("--at must be consumed once the window size lands, the fan-out already done")
	}
	m = drainMsgs(t, m, cmd, 8)
	if m.previewOpen == nil || m.previewOpen.source != "feat/x" {
		t.Fatalf("previewOpen = %+v, want the --at pair", m.previewOpen)
	}
}

// A NON-preview link is gated by the same fan-out (m.loading covers every
// source, previews included, since previews rides the same startup batch) —
// it is only the SPECIFIC previews-read precondition it is exempt from.
func TestStartAtForANonPreviewLinkAlsoWaitsForTheFanOut(t *testing.T) {
	t.Parallel()
	dir, repo := newRepoDir(t)
	sha := headSHA(t, dir)
	at := mustLink(t, "gg://gigagit@"+sha)
	svc := domain.New(repo)
	svc.UsePreviewsDir(t.TempDir()) // hermetic: previews still rides the fan-out
	m := New(svc)
	m.startAt, m.startAtPending = at, true
	m, fanOut := driveStartupFanOut(t, m)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	if !m.startAtPending {
		t.Fatal("--at must not be consumed while the startup fan-out is still in flight, even for a non-preview link")
	}

	// A non-preview link needs no startAtPreviewsSeen, so a mid-fan-out
	// checkpoint here isolates !m.loading specifically (unlike the preview
	// tests above, where startAtPreviewsSeen would ALSO still be blocking it
	// at this point, masking a missing loading check). Same 4-hop count as
	// TestStartAtWaitsForTheRealStartupFanOutToFinish above: themeCmd,
	// startWatchCmd, steerCmd, then the first of reloadAllCmd's per-source
	// reads.
	var rest tea.Cmd
	m, rest = drainDeep(t, m, fanOut, 4)
	if !m.ready {
		t.Fatal("setup: at least one source should have landed by now")
	}
	if !m.loading {
		t.Fatal("setup: the fan-out should still be partway through")
	}
	if !m.startAtPending {
		t.Fatal("--at must not be consumed while a sibling source is still loading")
	}

	m, _ = drainDeep(t, m, rest, 200)
	if m.loading {
		t.Fatal("setup: drainDeep should have completed the fan-out")
	}
	if m.startAtPending {
		t.Error("--at must be consumed once the fan-out and a window size have both landed")
	}
	if m.focus != panelCommits {
		t.Errorf("focus = %v, want the commit reveal to have landed on panelCommits", m.focus)
	}
	if strings.Contains(m.statusMsg, "no place gg can open") {
		t.Errorf("statusMsg = %q, want the commit reveal to have succeeded", m.statusMsg)
	}
}

// A refusal on the --at path has no CLI to print it: it must reach the status
// bar instead of vanishing (answerSteer writes no reply file for ID == "").
func TestStartAtFailureShowsAStatusMessage(t *testing.T) {
	t.Parallel()
	m, _ := previewSteerModel(t)
	c, ok := steerCommandForLink(mustLink(t, "gg://gigagit/a.txt@main...feat/nope:1"))
	if !ok {
		t.Fatal("the link must build a command")
	}
	m, cmd := m.applySteer(c)
	m = drainMsgs(t, m, cmd, 6)
	if m.statusMsg == "" || !strings.Contains(m.statusMsg, "missing branch feat/nope") {
		t.Errorf("statusMsg = %q, want the refusal", m.statusMsg)
	}
}

// A --at-originated navigate (steerCommandForLink's own zero-id, no-wait
// command) lands a NEUTRAL notice — the user drove this with `gg open`, not
// an agent — while the exact same navigate posted by a real steer client
// (an id assigned, as sendSteer always does) keeps the "agent" wording.
func TestStartAtNoticeSaysOpenedNotAgent(t *testing.T) {
	t.Parallel()
	m, _ := previewSteerModel(t)

	// Reveal-only (no file): steerNavigatePreview's own notice. No diff view
	// is open, so steerNotice lands it on the status bar, not diffNotice.
	startAtReveal := steer.Command{Cmd: "navigate",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"}}
	mm, cmd := m.applySteer(startAtReveal)
	if cmd != nil {
		cmd()
	}
	if !strings.Contains(mm.statusMsg, "opened preview") || strings.Contains(mm.statusMsg, "agent") {
		t.Errorf("start-at reveal notice = %q, want a neutral \"opened preview\", no \"agent\"", mm.statusMsg)
	}

	steeredReveal := steer.Command{ID: "s-1", Cmd: "navigate",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"}, Wait: true}
	mm, cmd = m.applySteer(steeredReveal)
	if cmd != nil {
		cmd()
	}
	if !strings.Contains(mm.statusMsg, "agent moved the focus") {
		t.Errorf("steered reveal notice = %q, want the \"agent\" wording kept", mm.statusMsg)
	}

	// File+line landing: landSteer's own notice.
	startAtLand := steer.Command{Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}}
	mm, cmd = m.applySteer(startAtLand)
	mm = drainMsgs(t, mm, cmd, 6)
	if !strings.Contains(mm.diffNotice, "opened a.txt:1") || strings.Contains(mm.diffNotice, "agent") {
		t.Errorf("start-at land notice = %q, want a neutral \"opened a.txt:1\", no \"agent\"", mm.diffNotice)
	}

	steeredLand := steer.Command{ID: "s-2", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	mm, cmd = m.applySteer(steeredLand)
	mm = drainMsgs(t, mm, cmd, 6)
	if !strings.Contains(mm.diffNotice, "agent opened a.txt:1") {
		t.Errorf("steered land notice = %q, want the \"agent\" wording kept", mm.diffNotice)
	}
}

func mustLink(t *testing.T, s string) model.Link {
	t.Helper()
	l, err := model.ParseLink(s)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
