package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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
// narrow terminal truncates the notice, not the error.
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

// Any message that changes statusMsg must stamp statusMsgAt (the central
// stamp lives in the Update wrapper, so no statusMsg call site needs to know
// about it); a message that leaves statusMsg alone must not re-stamp — the
// stamp is what bounds the two-line error expansion window at render time.
func TestStatusMsgChangeStampsStatusMsgAt(t *testing.T) {
	m := newTestModel(t)
	if !m.statusMsgAt.IsZero() {
		t.Fatal("precondition: a fresh model must have a zero stamp")
	}
	u, _ := m.Update(opFinishedMsg{err: errors.New("boom")})
	m = u.(Model)
	if m.statusMsg == "" {
		t.Fatal("precondition: a failed op must set statusMsg")
	}
	if m.statusMsgAt.IsZero() {
		t.Fatal("a statusMsg change must stamp statusMsgAt")
	}
	was := m.statusMsgAt
	u, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = u.(Model)
	if m.statusMsg == "" || !m.statusMsgAt.Equal(was) {
		t.Fatalf("a message that does not change statusMsg must not re-stamp (was %v, got %v)", was, m.statusMsgAt)
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

// A fresh error too long for one line takes over the footer row: the message
// continues on a second row, the key hints vanish for the duration, and the
// bottom row always ends with the pointer to the full text in the Session
// errors viewer. The message is sized to overflow ONE row (> g.w) but still
// fit within the two-row budget (g.w plus room, room = g.w minus the hint's
// width) — the case where the second row earns its keep by revealing text
// the first row could not hold, rather than being truncated a second time
// (a message too long for even the two-row budget is a different case,
// covered by TestHintSurvivesExtremeTruncation).
func TestLongFreshErrorExpandsOverFooter(t *testing.T) {
	m := sizedModel(t, 80, 30)
	tail := "the-very-end-of-the-error-text"
	m.statusMsg = "error: git push failed (exit 128): ssh: Could not resolve hostname " +
		strings.Repeat("x", 30) + " " + tail
	m.statusMsgAt = time.Now()
	out := ansi.Strip(m.View())
	if strings.Contains(out, "[b]ranch") {
		t.Fatalf("expanded error must replace the footer hints:\n%s", out)
	}
	if !strings.Contains(out, tail) {
		t.Fatalf("the second row must reveal the error tail:\n%s", out)
	}
	if !strings.Contains(out, "full: , → Session errors") {
		t.Fatalf("the bottom row must point at the Session errors viewer:\n%s", out)
	}
}

// A short error keeps today's single-line bar and the footer hints.
func TestShortErrorStaysOneLine(t *testing.T) {
	m := sizedModel(t, 80, 30)
	m.statusMsg = "error: boom"
	m.statusMsgAt = time.Now()
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "[b]ranch") {
		t.Fatalf("a short error must not hide the footer:\n%s", out)
	}
	if strings.Contains(out, "full: , → Session errors") {
		t.Fatalf("a short error needs no viewer pointer:\n%s", out)
	}
}

// A long NON-error message never expands — the footer survives and the
// message truncates as before.
func TestLongNonErrorNeverExpands(t *testing.T) {
	m := sizedModel(t, 80, 30)
	m.statusMsg = "pulled and rebased onto origin/main " + strings.Repeat("y", 120)
	m.statusMsgAt = time.Now()
	if out := ansi.Strip(m.View()); !strings.Contains(out, "[b]ranch") {
		t.Fatalf("a non-error message must not take the footer row:\n%s", out)
	}
}

// The expansion is temporary: past the 30s window the bar collapses back to
// one truncated line and the footer returns.
func TestExpiredErrorCollapses(t *testing.T) {
	m := sizedModel(t, 80, 30)
	m.statusMsg = "error: git push failed (exit 128): " + strings.Repeat("x", 120)
	m.statusMsgAt = time.Now().Add(-statusErrExpandFor - time.Second)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "[b]ranch") {
		t.Fatalf("an expired error must give the footer row back:\n%s", out)
	}
	if strings.Contains(out, "full: , → Session errors") {
		t.Fatalf("an expired error must not keep the pointer row:\n%s", out)
	}
}

// Even when two rows cannot hold the message, the viewer pointer survives at
// the bottom row's tail — truncation eats the message, never the pointer.
// frontToken is placed exactly where row 1 ends (pad fills row 1's g.w
// columns exactly), so it is the first thing row 2's tail truncation must
// decide whether to keep. It sits well inside the front of the whole
// message — this is the regression guard for cutting the tail from the
// wrong end: keeping it (truncate, which keeps the tail's FRONT) means row 2
// continues naturally from row 1; losing it (elideLeft, which keeps the
// tail's END) would mean row 2 jumps straight to trailing "zzzz…" boilerplate
// instead, exactly the bug a real git error hits (see the view.go comment).
func TestHintSurvivesExtremeTruncation(t *testing.T) {
	m := sizedModel(t, 44, 30)
	prefix := "error: "
	pad := strings.Repeat("a", 44-len(prefix)) // fills row 1 (g.w=44) exactly
	frontToken := "DIAGNOSIS-HERE"
	m.statusMsg = prefix + pad + frontToken + strings.Repeat("z", 400)
	m.statusMsgAt = time.Now()
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "full: , → Session errors") {
		t.Fatalf("the pointer must survive extreme truncation:\n%s", out)
	}
	if !strings.Contains(out, frontToken) {
		t.Fatalf("row 2 must continue from where row 1 left off, not jump to the message's trailing boilerplate:\n%s", out)
	}
}

// splitCols is the wrap primitive: head is the widest prefix that fits the
// column budget (no ellipsis), tail the remainder with leading spaces
// dropped; a wide glyph that would straddle the boundary moves wholly into
// tail so neither row can exceed its width.
func TestSplitCols(t *testing.T) {
	head, tail := splitCols("abcdef", 4)
	if head != "abcd" || tail != "ef" {
		t.Fatalf("plain split: got %q %q", head, tail)
	}
	// "ab " exactly fills 3 columns; the tail's leading spaces are dropped so
	// the second row never starts with dead space.
	head, tail = splitCols("ab cdef", 3)
	if head != "ab " || tail != "cdef" {
		t.Fatalf("split near a space: got %q %q", head, tail)
	}
	head, tail = splitCols("ab", 10)
	if head != "ab" || tail != "" {
		t.Fatalf("short input: got %q %q", head, tail)
	}
	// ⏳ is 2 columns wide; with 1 column left it must move to tail entirely.
	head, tail = splitCols("a⏳b", 2)
	if head != "a" || tail != "⏳b" {
		t.Fatalf("wide glyph must not straddle the boundary: got %q %q", head, tail)
	}
}
