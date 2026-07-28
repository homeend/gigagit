package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// With GIT_TERMINAL_PROMPT=0 a push/pull/fetch that needs credentials no longer
// hangs the TUI — git fails fast. But its raw message ("could not read Username
// … terminal prompts disabled") reads like a gg defect, so friendlyOpError turns
// it into actionable guidance while staying an "error:" line (so it styles red).
func TestFriendlyOpErrorExplainsMissingCredentials(t *testing.T) {
	raw := errors.New("git push failed (exit 128): fatal: could not read Username for 'https://github.com': terminal prompts disabled")
	got := friendlyOpError(raw)
	if !statusIsError(got) {
		t.Fatalf("friendlyOpError(%q) = %q, want an error: line", raw, got)
	}
	if strings.Contains(got, "terminal prompts disabled") {
		t.Fatalf("friendly message still leaks the raw git noise: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "credential") {
		t.Fatalf("friendly message should mention credentials: %q", got)
	}
}

// gg forces ssh into BatchMode (WithSSHBatchMode) so a host-key prompt can
// never freeze the TUI — first contact with an unknown host fails with "Host
// key verification failed." plus git's misleading access-rights tail. That
// reads like a permissions bug, so friendlyOpError points at the ctrl+o shell
// escape, where ssh can prompt interactively.
func TestFriendlyOpErrorExplainsUnknownHostKey(t *testing.T) {
	raw := errors.New("git push failed (exit 128): Host key verification failed. fatal: Could not read from remote repository. Please make sure you have the correct access rights and the repository exists.")
	got := friendlyOpError(raw)
	if !statusIsError(got) {
		t.Fatalf("friendlyOpError(%q) = %q, want an error: line", raw, got)
	}
	if strings.Contains(got, "Host key verification failed") {
		t.Fatalf("friendly message still leaks the raw git noise: %q", got)
	}
	if !strings.Contains(got, "ctrl+o") {
		t.Fatalf("friendly message should point at the ctrl+o shell escape: %q", got)
	}
}

// A known_hosts MISMATCH (possible MITM) also ends in the generic "Host key
// verification failed." line, but must NOT advise accepting the key — the
// changed-key signature is classified before the unknown-host one.
func TestFriendlyOpErrorExplainsChangedHostKey(t *testing.T) {
	raw := errors.New("git push failed (exit 128): @@@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! @@@ IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY! Host key verification failed. fatal: Could not read from remote repository.")
	got := friendlyOpError(raw)
	if !statusIsError(got) {
		t.Fatalf("friendlyOpError(%q) = %q, want an error: line", raw, got)
	}
	if strings.Contains(got, "Host key verification failed") {
		t.Fatalf("friendly message still leaks the raw git noise: %q", got)
	}
	if !strings.Contains(got, "CHANGED") {
		t.Fatalf("friendly message should say the key changed: %q", got)
	}
	if strings.Contains(got, "ctrl+o") {
		t.Fatalf("a changed key must not advise accepting it: %q", got)
	}
}

func TestFriendlyOpErrorPassesThroughOtherFailures(t *testing.T) {
	raw := errors.New("git stash apply failed (exit 1): some unrecognised git noise")
	got := friendlyOpError(raw)
	if !strings.Contains(got, "some unrecognised git noise") {
		t.Fatalf("unrelated failure should pass through, got: %q", got)
	}
}

// A non-fast-forward rejection (plain push) is git's most common push failure.
// The raw multi-line stderr ("! [rejected] … (non-fast-forward)" + a wall of
// hints) is useless in a one-line status bar, so friendlyOpError rewrites it
// into one actionable sentence — and must not leak the raw "(non-fast-forward)"
// token or the multi-line hint block.
func TestFriendlyOpErrorExplainsNonFastForward(t *testing.T) {
	raw := errors.New("git push failed (exit 1): To ssh://host/repo.git\n " +
		"! [rejected]        br -> br (non-fast-forward)\n" +
		"error: failed to push some refs to 'ssh://host/repo.git'\n" +
		"hint: Updates were rejected because the tip of your current branch is behind")
	got := friendlyOpError(raw)
	if !statusIsError(got) {
		t.Fatalf("want an error: line, got %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("status line must be a single line, got %q", got)
	}
	if strings.Contains(strings.ToLower(got), "non-fast-forward") || strings.Contains(got, "hint:") {
		t.Fatalf("friendly message still leaks raw git noise: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "new commits") {
		t.Fatalf("message should explain the remote is ahead: %q", got)
	}
}

// "stale info" is what --force-with-lease reports when the remote moved since
// the last fetch — the safety net firing, not a defect. The user needs to know
// it means "fetch first", not raw git jargon.
func TestFriendlyOpErrorExplainsStaleInfo(t *testing.T) {
	raw := errors.New("git push failed (exit 1): To ssh://host/repo.git\n " +
		"! [rejected]        br -> br (stale info)\n" +
		"error: failed to push some refs to 'ssh://host/repo.git'")
	got := friendlyOpError(raw)
	if !statusIsError(got) || strings.Contains(got, "\n") {
		t.Fatalf("want a single error: line, got %q", got)
	}
	low := strings.ToLower(got)
	if !strings.Contains(low, "force-with-lease") || !strings.Contains(low, "fetch") {
		t.Fatalf("message should name force-with-lease and steer to fetch: %q", got)
	}
	if strings.Contains(low, "stale info") {
		t.Fatalf("friendly message still leaks the raw '(stale info)' token: %q", got)
	}
}

// A server-side rejection (protected branch / pre-receive hook) is not something
// pull-then-push fixes, so it gets its own message rather than the remote-ahead one.
func TestFriendlyOpErrorExplainsHookRejection(t *testing.T) {
	raw := errors.New("git push failed (exit 1): remote: error: GH006: Protected branch update failed\n" +
		"! [remote rejected] main -> main (pre-receive hook declined)")
	got := friendlyOpError(raw)
	if !statusIsError(got) || strings.Contains(got, "\n") {
		t.Fatalf("want a single error: line, got %q", got)
	}
	low := strings.ToLower(got)
	if !strings.Contains(low, "protected") && !strings.Contains(low, "hook") {
		t.Fatalf("message should name the server-side rejection: %q", got)
	}
}

func TestStatusIsError(t *testing.T) {
	errs := []string{
		"error: git stash apply failed (exit 1)",
		"files: boom", "commits: boom", "amend: boom",
		"interactive rebase: boom", "cannot create: boom",
	}
	for _, s := range errs {
		if !statusIsError(s) {
			t.Errorf("statusIsError(%q) = false, want true", s)
		}
	}
	ok := []string{
		"", "working…", "nothing to stash", "title required",
		"resolve conflicts first", "agent skills: 1 installed, 0 refreshed",
		"resolved foo (kept theirs)", "merge continued",
	}
	for _, s := range ok {
		if statusIsError(s) {
			t.Errorf("statusIsError(%q) = true, want false", s)
		}
	}
}

// TestStatusIsErrorSurvivesTranslation guards the retroactive defect: status
// messages are translated (i18n stages 1-3), so statusIsError must classify
// a TRANSLATED error message as an error too, not just the English original.
// A custom "xx" bundle stands in for any real non-English language.
func TestStatusIsErrorSurvivesTranslation(t *testing.T) {
	dir := t.TempDir()
	body := "[meta]\nname=\"Test\"\n[strings]\n" +
		"\"error: %s\" = \"TESTERR: %s\"\n" +
		"\"amend: %s\" = \"TESTAMEND: %s\"\n" +
		"\"loading…\" = \"TESTLOAD…\"\n" +
		"\"commits: [enter/tab] tree  …\" = \"TESTFOOT: [x] example\"\n"
	if err := os.WriteFile(filepath.Join(dir, "xx.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := i18n.SetLanguage("xx", dir); err != nil {
		t.Fatal(err)
	}
	defer i18n.SetLanguage("", "")

	if got := i18n.T("error: %s", "boom"); !statusIsError(got) {
		t.Errorf("statusIsError(%q) = false, want true (translated error:)", got)
	}
	if got := i18n.T("amend: %s", "boom"); !statusIsError(got) {
		t.Errorf("statusIsError(%q) = false, want true (translated amend:)", got)
	}
	if got := i18n.T("loading…"); statusIsError(got) {
		t.Errorf("statusIsError(%q) = true, want false (not an error key)", got)
	}
	// Guard against verb-less keys sharing error prefixes (e.g. a footer sharing "commits:")
	if statusIsError("TESTFOOT: something") {
		t.Errorf("statusIsError(%q) = true, want false (verb-less footer key excluded by guard)", "TESTFOOT: something")
	}
}

// TestStatusIsErrorEnglishAfterReset guards that resetting to English still
// classifies plain English error prefixes correctly (no regression from the
// translated-prefix derivation).
func TestStatusIsErrorEnglishAfterReset(t *testing.T) {
	dir := t.TempDir()
	body := "[meta]\nname=\"Test\"\n[strings]\n\"error: %s\" = \"TESTERR: %s\"\n"
	if err := os.WriteFile(filepath.Join(dir, "xx.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := i18n.SetLanguage("xx", dir); err != nil {
		t.Fatal(err)
	}
	i18n.SetLanguage("", "")

	if !statusIsError("error: boom") {
		t.Errorf("statusIsError(%q) = false, want true after reset to English", "error: boom")
	}
}

func statusRenderModel() Model {
	return Model{width: 120, height: 30, focus: panelFiles, sel: map[panel]int{}}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// TestStatusErrorLinePreservesText guards that the error-styling path does not
// corrupt or drop the message (the color itself is verified visually — lipgloss
// strips color in the non-TTY test environment).
func TestStatusErrorLinePreservesText(t *testing.T) {
	m := statusRenderModel()
	m.statusMsg = "error: git stash apply failed (exit 1)"
	line := ansi.Strip(lastLine(m.View()))
	if !strings.Contains(line, "error: git stash apply failed") {
		t.Fatalf("error status line lost its text: %q", line)
	}
}

func TestStatusNormalLinePreservesText(t *testing.T) {
	m := statusRenderModel()
	m.statusMsg = "resolved foo (kept theirs)"
	line := ansi.Strip(lastLine(m.View()))
	if !strings.Contains(line, "resolved foo (kept theirs)") {
		t.Fatalf("normal status line lost its text: %q", line)
	}
}

// TestStatusErrorLeadsConflictNotice guards the stash-apply-conflict workflow:
// when an error coexists with the conflict notice, the error must lead so a
// narrow terminal truncates the notice, not the error. statusMsgAt is
// stamped fresh (what production always does via the Update wrapper) rather
// than left zero — with a zero stamp time.Since(zero) is enormous, so the
// two-line expansion's freshness gate happened to be false regardless of
// what this test was asserting, letting it pass for an accidental reason.
func TestStatusErrorLeadsConflictNotice(t *testing.T) {
	m := statusRenderModel()
	m.width = 50 // narrow enough that the whole line can't fit
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "timing3.log", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'},
	}}
	m.statusMsg = "error: git stash apply failed (exit 1)"
	line := ansi.Strip(lastLine(m.View()))
	if !strings.Contains(line, "error:") {
		t.Fatalf("error must lead the status line, got: %q", line)
	}
	if strings.HasPrefix(strings.TrimSpace(line), "⚠") {
		t.Errorf("conflict notice should not lead while an error is shown: %q", line)
	}
}

// sizedModel returns a test model laid out at w×h with no layers open, so
// View() renders the base interface (header/panels/footer/status). ready and
// loading are forced to their post-startup steady state (the
// TestInitialLoadBlanksUntilReady/TestReloadAfterFirstData… precedent): New()
// leaves ready=false/loading=true until the first source's data lands, and
// this helper sends no data — without the override View() would show the
// "gigagit (loading…)" placeholder, and opsIdle() (gated on !loading) would
// stay false and hide every ops-gated footer hint, including [b]ranch. A
// single branch is seeded so that hint (gated on a Branches selection
// existing) actually renders — the assertions below need the ordinary
// footer hints present to prove the error bar hides them.
func sizedModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := newTestModel(t)
	m.ready = true
	m.loading = false
	m.branches = []model.Branch{{Name: "main"}}
	u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return u.(Model)
}

// assertFrameFits checks the two universal invariants any base-interface
// render must hold: exactly wantLines rows (the frame must never render
// taller than the model's own height — the Finding 1 bug: a composed row
// wider than the terminal wraps when actually printed, pushing the header
// off the top of a real terminal, even though the in-memory string here
// stays wantLines "\n"-joined segments either way, which is why the width
// check below is the one that actually catches it), and no single line
// exceeding maxWidth display columns. Width is measured via lipgloss.Width
// on an already ansi.Strip'd string, never len/byte length — byte length
// would silently pass a line that overflows only because of a wide glyph
// (⏳) or a non-Latin script (Cyrillic), exactly the languages this bug hits.
func assertFrameFits(t *testing.T, out string, wantLines, maxWidth int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != wantLines {
		t.Fatalf("frame has %d lines, want %d:\n%s", len(lines), wantLines, out)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > maxWidth {
			t.Fatalf("line %d is %d display columns wide, want <= %d:\n%q\nfull frame:\n%s", i, w, maxWidth, l, out)
		}
	}
}

// A fresh error too long for one line takes over the footer row: the message
// continues on a second row, the key hints vanish for the duration, and the
// bottom row always ends with the pointer to the full text in the Session
// errors viewer. The message is sized to overflow ONE row (> g.w) but still
// fit within the two-row budget (g.w plus room, room = g.w minus the hint's
// width) — the case where the second row earns its keep by revealing text
// the first row could not hold, rather than being truncated a second time
// (a message too long for even the two-row budget is a different case,
// covered by TestHintSurvivesExtremeTruncation).

// An error status line leads with the [E] pointer so the reader knows the full
// text is one key away, and the bar stays ONE line — the footer keeps its row.
func TestErrorLineLeadsWithPointerAndStaysOneLine(t *testing.T) {
	m := sizedModel(t, 80, 30)
	m.statusMsg = "error: git push failed (exit 128): ssh: Could not resolve hostname " + strings.Repeat("x", 90)
	m.lastError = m.statusMsg
	out := ansi.Strip(m.View())
	assertFrameFits(t, out, 30, 80)
	if !strings.Contains(out, "[b]ranch") {
		t.Fatalf("the footer must keep its row — the bar is one line now:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "[E] full details") {
		t.Fatalf("the pointer must LEAD the status line so truncation (which cuts from the back) cannot eat it, got:\n%s", last)
	}
}

// A non-error status message is untouched: no pointer, since there is nothing
// to open.
func TestNonErrorLineHasNoPointer(t *testing.T) {
	m := sizedModel(t, 80, 30)
	m.statusMsg = "pulled and rebased onto origin/main"
	if out := ansi.Strip(m.View()); strings.Contains(out, "[E] full details") {
		t.Fatalf("a successful message must not advertise the error viewer:\n%s", out)
	}
}

// A failure whose prefix statusIsError does not recognise still needs [E]:
// the bar truncated it, so the tail is unreadable without the popup. Seven of
// the eight refresh sources report as "<source>: <git stderr>" and only
// "commits:" happens to be in the error-prefix list, so a repo switch that
// fails while reloading branches/status/worktrees lands here.
func TestLongUnclassifiedMessageIsCaptured(t *testing.T) {
	m := sizedModel(t, 80, 30)
	long := "branches: fatal: could not read from remote repository " + strings.Repeat("x", 120)
	u, _ := m.Update(dataAvailableMsg{source: srcBranches, gen: m.srcGen[srcBranches], manual: true, err: errors.New(strings.TrimPrefix(long, "branches: "))})
	m = u.(Model)
	if m.statusMsg != long {
		t.Fatalf("precondition: status not set as expected: %q", m.statusMsg)
	}
	if m.lastError != long {
		t.Fatalf("a message too long for the bar must be recoverable via [E], got lastError=%q", m.lastError)
	}
}

// The converse: a short message fits, so there is nothing to recover and the
// footer must not advertise [E] for it.
func TestShortUnclassifiedMessageIsNotCaptured(t *testing.T) {
	m := sizedModel(t, 80, 30)
	u, _ := m.Update(dataAvailableMsg{source: srcBranches, gen: m.srcGen[srcBranches], manual: true, err: errors.New("boom")})
	if got := u.(Model).lastError; got != "" {
		t.Fatalf("a message that fits the bar needs no [E] pointer, got %q", got)
	}
}

// …and the pointer is rendered for it, or the user has no way to know E works.
func TestLongUnclassifiedMessageShowsPointer(t *testing.T) {
	m := sizedModel(t, 80, 30)
	m.statusMsg = "branches: fatal: could not read from remote repository " + strings.Repeat("x", 120)
	m.lastError = m.statusMsg
	out := ansi.Strip(m.View())
	assertFrameFits(t, out, 30, 80)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "[E] full details") {
		t.Fatalf("a truncated message must lead with the pointer, got:\n%s", last)
	}
}

// The whole loop for the reported case, driven through Update: an
// unclassified failure arrives, the bar cuts it, and E opens the tail the bar
// could not show.
func TestUnclassifiedFailureIsReadableViaE(t *testing.T) {
	m := sizedModel(t, 80, 30)
	tail := "Temporary-failure-in-name-resolution"
	u, _ := m.Update(dataAvailableMsg{
		source: srcBranches, gen: m.srcGen[srcBranches], manual: true,
		err: errors.New("fatal: could not read from remote repository " + strings.Repeat("x", 120) + " " + tail),
	})
	m = u.(Model)
	if strings.Contains(ansi.Strip(m.View()), tail) {
		t.Fatal("precondition: the one-line bar must have cut the tail")
	}
	u, _ = m.Update(keyMsg("E"))
	m = u.(Model)
	if layerOf[*contentPopup](m) == nil {
		t.Fatal("E must open the viewer for a truncated non-error-prefixed failure")
	}
	// The viewer wraps, so the tail can straddle a row boundary — compare with
	// whitespace removed rather than asserting it lands on one line.
	out := ansi.Strip(m.View())
	if !strings.Contains(squashToWordChars(out), tail) {
		t.Fatalf("the viewer must show the text the bar cut:\n%s", out)
	}
}

// squashToWordChars keeps only letters, digits and hyphens. A wrapped row
// boundary inserts not just a newline but the popup's own box borders between
// the halves of a split word, so dropping whitespace alone is not enough to
// check that a long string reached the screen intact.
func squashToWordChars(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			return r
		}
		return -1
	}, s)
}

// A snapshot load that fails AFTER the UI has been up (a repo switch into a
// broken repo) must not replace the whole screen with a bare error line: that
// leaves no status bar, no footer, and no key to press. Surface it like any
// other failure so [E] can show the full text.
func TestFailedReloadKeepsTheInterface(t *testing.T) {
	m := sizedModel(t, 80, 30)
	m.loadedOK = true // a successful load already happened
	boom := "fatal: not a git repository (or any parent up to mount point /mnt) " + strings.Repeat("y", 120)
	u, _ := m.Update(dataLoadedMsg{gen: m.loadGen, err: errors.New(boom)})
	m = u.(Model)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "[b]ranch") {
		t.Fatalf("the interface must survive a failed reload:\n%s", out)
	}
	if !strings.Contains(m.lastError, boom) {
		t.Fatalf("the failure must be recoverable via [E], got lastError=%q", m.lastError)
	}
}

// But a load that fails before anything was ever shown keeps the bare screen:
// there is no interface to preserve, and an empty frame would read as a
// working UI onto an unreadable repo.
func TestFirstLoadFailureStillShowsBareError(t *testing.T) {
	m := sizedModel(t, 80, 30)
	m.loadedOK = false
	u, _ := m.Update(dataLoadedMsg{gen: m.loadGen, err: errors.New("fatal: not a git repository")})
	if out := ansi.Strip(u.(Model).View()); strings.Contains(out, "[b]ranch") {
		t.Fatalf("a first-load failure must not render an empty interface:\n%s", out)
	}
}

// [E] opens the last failure in full: the part the one-line bar had to cut is
// visible in the popup, wrapped rather than truncated.
func TestErrorPopupShowsTheTextTheBarCut(t *testing.T) {
	m := sizedModel(t, 80, 30)
	tail := "Temporary-failure-in-name-resolution"
	m.lastError = "error: git push failed (exit 128): ssh: Could not resolve hostname " +
		strings.Repeat("x", 90) + " " + tail
	if strings.Contains(ansi.Strip(m.View()), tail) {
		t.Fatal("precondition: the one-line bar must have cut the tail")
	}
	u, _ := m.Update(keyMsg("E"))
	m = u.(Model)
	if layerOf[*contentPopup](m) == nil {
		t.Fatal("E must open the error viewer")
	}
	// Compare with whitespace and box borders removed, like the sibling test
	// above: the viewer wraps, so the tail can straddle a row boundary — which
	// it now does, the message block having traded two columns of text width
	// for its margins.
	out := ansi.Strip(m.View())
	if !strings.Contains(squashToWordChars(out), tail) {
		t.Fatalf("the popup must show the text the bar cut:\n%s", out)
	}
}

// The viewer wraps by default: a status message is prose, and cutoff mode
// would hide the tail all over again.
func TestErrorPopupWrapsByDefault(t *testing.T) {
	p := newErrorPopup("error: " + strings.Repeat("z", 400))
	if p.mode != modeWrap {
		t.Fatalf("the error viewer must open in wrap mode, got %v", p.mode)
	}
	if !p.danger {
		t.Fatal("the error viewer must use the danger (red) frame")
	}
}

// git writes multi-line stderr; the viewer keeps that structure instead of
// collapsing it the way the one-line bar does.
func TestErrorPopupKeepsGitsLineStructure(t *testing.T) {
	p := newErrorPopup("error: first line\nsecond line\nthird line")
	if len(p.lines) != 3 {
		t.Fatalf("want one contentLine per stderr line, got %d: %+v", len(p.lines), p.lines)
	}
	if p.lines[2].text != "third line" {
		t.Fatalf("line order/content wrong: %+v", p.lines)
	}
}

// Nothing has failed yet: E is inert rather than opening an empty box.
func TestErrorPopupInertWithoutAnError(t *testing.T) {
	m := sizedModel(t, 80, 30)
	u, _ := m.Update(keyMsg("E"))
	if layerOf[*contentPopup](u.(Model)) != nil {
		t.Fatal("E must do nothing when no failure has been recorded")
	}
}

// The viewer swallows keys: a global action key pressed while it is open must
// not reach the panels underneath.
func TestErrorPopupSwallowsGlobalKeys(t *testing.T) {
	m := sizedModel(t, 80, 30)
	m.lastError = "error: boom"
	u, _ := m.Update(keyMsg("E"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("p")) // would start a pull if it leaked
	m = u.(Model)
	if m.running {
		t.Fatal("a key pressed inside the viewer must not start an operation")
	}
	if layerOf[*contentPopup](m) == nil {
		t.Fatal("the viewer should still be open")
	}
	u, _ = m.Update(keyMsg("esc"))
	if layerOf[*contentPopup](u.(Model)) != nil {
		t.Fatal("esc must close the viewer")
	}
}

// The full text survives in lastError even after a later message replaces the
// status line — E is about the last FAILURE, not the last message.
func TestLastErrorSurvivesALaterMessage(t *testing.T) {
	m := newTestModel(t)
	u, _ := m.Update(opFinishedMsg{err: errors.New("boom: the whole story")})
	m = u.(Model)
	if m.lastError == "" {
		t.Fatal("a failed op must record lastError")
	}
	captured := m.lastError
	u, _ = m.Update(opFinishedMsg{res: engine.Result{Summary: "done"}})
	m = u.(Model)
	if m.lastError != captured {
		t.Fatalf("a later success must not overwrite the recorded failure: %q", m.lastError)
	}
}

// ssh ends its lines with CRLF. A surviving \r makes the terminal jump to
// column 0 mid-line, so the rest of the row overwrites the popup's own border
// — invisible to width math (\r measures zero) and therefore only reproducible
// on a real terminal. Tabs are the mirror image: one column to lipgloss, up to
// eight on screen.
func TestErrorPopupStripsCursorMovingControlBytes(t *testing.T) {
	p := newErrorPopup("error: ssh: Could not resolve hostname host-xyz: Temporary failure\r\nfatal: Could not read from remote repository.\r\n\tindented hint\x1b[31m")
	for i, l := range p.lines {
		if strings.ContainsAny(l.text, "\r\x1b\x08") {
			t.Fatalf("line %d still carries a cursor-moving control byte: %q", i, l.text)
		}
	}
	if len(p.lines) != 3 {
		t.Fatalf("CRLF must split into the same lines LF does, got %d: %+v", len(p.lines), p.lines)
	}
	if !strings.Contains(p.lines[0].text, "Temporary failure") {
		t.Fatalf("the message text itself must survive sanitizing: %q", p.lines[0].text)
	}
	if !strings.HasPrefix(p.lines[2].text, "    indented hint") {
		t.Fatalf("a tab must become width-exact spaces, got %q", p.lines[2].text)
	}
}
