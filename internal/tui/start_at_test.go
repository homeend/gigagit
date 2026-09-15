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
	// stays true) — the genuine mid-fan-out state. 4 clears the two levels
	// of batch-unwrapping (the top tea.Batch, then reloadAllCmd's own
	// ~10-item one) plus enough real reads that at least one has landed,
	// asserted below rather than assumed.
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
	// at this point, masking a missing loading check).
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

func mustLink(t *testing.T, s string) model.Link {
	t.Helper()
	l, err := model.ParseLink(s)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
