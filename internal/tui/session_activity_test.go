package tui

import (
	"context"
	"github.com/homeend/gigagit/internal/config"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/domain"
)

func TestActivityText(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 7, 0, 0, time.UTC)
	since := now.Add(-7 * time.Minute)
	cases := []struct {
		a    domain.SessionActivity
		want string
		attn bool
	}{
		{domain.SessionActivity{State: domain.ActivityWorking, Since: since}, "working 7m00s", false},
		{domain.SessionActivity{State: domain.ActivityIdle, Since: since}, "idle 7m00s", false},
		{domain.SessionActivity{State: domain.ActivityQuestion, Since: since}, "needs input", true},
		{domain.SessionActivity{State: domain.ActivityWorking, Since: since, Stalled: true}, "stalled · working 7m00s", true},
		{domain.SessionActivity{Stalled: true}, "stalled · no output", true},
		{domain.SessionActivity{}, "", false},
	}
	for _, c := range cases {
		if got := activityText(c.a, now); got != c.want {
			t.Errorf("%+v: text %q want %q", c.a, got, c.want)
		}
		if got := activityAttn(c.a); got != c.attn {
			t.Errorf("%+v: attn %v want %v", c.a, got, c.attn)
		}
	}
}

// Serial: installs a static state watcher. A classified session's activity
// replaces the running age on the narrow sub-row and is appended on the
// wide popup row and console title; an unclassified one reads as before.
func TestSessionRowsShowActivity(t *testing.T) {
	restore := domain.UseSessionStates(domain.NewStaticStates(map[domain.SessionID]domain.SessionActivity{
		"s1": {State: domain.ActivityQuestion, Since: time.Now().Add(-3 * time.Minute)},
	}))
	defer restore()
	classified := domain.SessionInfo{ID: "s1", Label: "Claude", Dir: "/wt/a", Started: time.Now().Add(-2 * time.Minute)}
	plain := domain.SessionInfo{ID: "s9", Label: "sh", Dir: "/wt/a", Started: time.Now().Add(-2 * time.Minute)}
	if got := sessionRowBody(classified); got != "└ ● Claude  needs input" {
		t.Errorf("sub-row = %q", got)
	}
	if got := sessionRowBody(plain); got != "└ ● sh  running 2m00s" {
		t.Errorf("plain sub-row = %q", got)
	}
	if got := sessionStateText(classified); got != "● Claude  running 2m00s · needs input" {
		t.Errorf("popup row = %q", got)
	}
	if got := sessionStateText(plain); got != "● sh  running 2m00s" {
		t.Errorf("plain popup row = %q", got)
	}
	if got := consoleTitle(classified, true); got != "Claude · a · running 2m00s · needs input" {
		t.Errorf("title = %q", got)
	}
	if got := consoleTitle(plain, true); got != "sh · a · running 2m00s" {
		t.Errorf("plain title = %q", got)
	}
	exited := classified
	exited.State = domain.SessionExited
	if got := sessionRowBody(exited); got != "└ ○ Claude  exited (0)" {
		t.Errorf("exited sub-row = %q", got)
	}
}

// Serial: installs a static state watcher. Notices land on the status line,
// newest last wins, the cursor advances, and the session the focused
// console shows is skipped.
func TestSessionActivityNotices(t *testing.T) {
	w := domain.NewStaticStates(nil)
	restore := domain.UseSessionStates(w)
	defer restore()
	m := Model{actWatch: &activityWatch{}, actSeq: new(uint64)}
	*m.actSeq = w.NoticeSeq()
	w.PostNotice(domain.ActivityNotice{ID: "s1", Kind: "question", Label: "Claude", Dir: "/wt/feat-a"})
	m, _ = m.onSessionActivity()
	if m.statusMsg != "Claude in feat-a needs your input" {
		t.Fatalf("status = %q", m.statusMsg)
	}
	m.statusMsg = ""
	m, _ = m.onSessionActivity()
	if m.statusMsg != "" {
		t.Fatalf("an old notice was shown again: %q", m.statusMsg)
	}
	w.PostNotice(domain.ActivityNotice{ID: "s1", Kind: "idle", Label: "Claude", Dir: "/wt/feat-a"})
	w.PostNotice(domain.ActivityNotice{ID: "s2", Kind: "stalled", Label: "Codex", Dir: "/wt/b", Quiet: 125 * time.Second})
	m, _ = m.onSessionActivity()
	if m.statusMsg != "Codex in b has printed nothing for 2m05s — stalled?" {
		t.Fatalf("newest = %q", m.statusMsg)
	}
	w.PostNotice(domain.ActivityNotice{ID: "s2", Kind: "stalled", Label: "Claude", Dir: "/wt/b", Quiet: 10 * time.Minute, Spinning: true})
	m, _ = m.onSessionActivity()
	if m.statusMsg != "Claude in b has shown only its spinner for 10m00s — stalled?" {
		t.Fatalf("spinning = %q", m.statusMsg)
	}
	// The user is typing into s1: its notices say nothing.
	m.statusMsg = ""
	m.console = &consoleState{id: "s1", focused: true}
	w.PostNotice(domain.ActivityNotice{ID: "s1", Kind: "question", Label: "Claude", Dir: "/wt/feat-a"})
	m, _ = m.onSessionActivity()
	if m.statusMsg != "" {
		t.Fatalf("a focused console's session was announced: %q", m.statusMsg)
	}
	if got := activityNoticeText(domain.ActivityNotice{Kind: "idle", Label: "Claude", Dir: "/wt/feat-a"}); got != "Claude in feat-a finished its turn — idle" {
		t.Fatalf("idle = %q", got)
	}
}

func TestActivityWaitIsQuietInHeadlessMode(t *testing.T) {
	m := Model{quiet: true, actWatch: &activityWatch{}, actSeq: new(uint64)}
	if m.waitActivityCmd() != nil {
		t.Fatal("a never-ending command in quiet mode")
	}
	if (Model{}).waitActivityCmd() != nil {
		t.Fatal("a Model literal has no watch")
	}
}

func TestScreenRulesWarningIsTranslated(t *testing.T) {
	if got := screenRulesWarning("Claude"); !strings.Contains(got, "Claude") || !strings.Contains(got, "built-in") {
		t.Fatalf("warning = %q", got)
	}
}

// reportingSession: a fresh manager with one running session (no parallel:
// the manager and the watcher are process-global).
func reportingSession(t *testing.T) *domain.AgentSession {
	t.Helper()
	restore := domain.UseSessionManager(agentsession.NewManager())
	s, err := domain.Sessions().Start(domain.SessionStartSpec{Label: "w", AgentID: "claude", Repo: "r", Dir: t.TempDir(), Cols: 80, Rows: 24, Argv: []string{"sh", "-c", "sleep 60"}})
	if err != nil {
		restore()
		t.Fatal(err)
	}
	t.Cleanup(func() { domain.Sessions().KillAll(context.Background()); restore() })
	return s
}

func TestSessionBadgePrecedence(t *testing.T) {
	s := reportingSession(t)
	id := s.Info().ID
	now := time.Now()
	w := domain.NewStaticStates(map[domain.SessionID]domain.SessionActivity{id: {State: domain.ActivityIdle, Since: now.Add(-3 * time.Minute)}})
	defer domain.UseSessionStates(w)()
	if text, attn := sessionBadge(id, now); text != "idle 3m00s" || attn {
		t.Fatalf("no report: %q %v", text, attn)
	}
	if _, err := domain.AgentReportVerb(domain.FullSessionID(id), "merged feat/x\nmore", false); err != nil {
		t.Fatal(err)
	}
	if text, attn := sessionBadge(id, now.Add(2*time.Minute)); !strings.HasPrefix(text, "reported 1m") && !strings.HasPrefix(text, "reported 2m") || !attn {
		t.Fatalf("non-final report: %q %v", text, attn)
	}
	if line := sessionReportLine(id); line != "merged feat/x" {
		t.Fatalf("report line = %q", line)
	}
	if _, err := domain.AgentReportVerb(domain.FullSessionID(id), "all done", true); err != nil {
		t.Fatal(err)
	}
	if text, _ := sessionBadge(id, now.Add(2*time.Minute)); !strings.HasPrefix(text, "done ") {
		t.Fatalf("final report: %q", text)
	}
	// A question beats the report.
	restore2 := domain.UseSessionStates(domain.NewStaticStates(map[domain.SessionID]domain.SessionActivity{id: {State: domain.ActivityQuestion, Since: now}}))
	defer restore2()
	if text, attn := sessionBadge(id, now); text != "needs input" || !attn {
		t.Fatalf("question must win: %q %v", text, attn)
	}
	if line := sessionReportLine(id); line != "" {
		t.Fatalf("a question hides the report line: %q", line)
	}
}

func TestSessionBadgeClearsOnInput(t *testing.T) {
	s := reportingSession(t)
	id := s.Info().ID
	defer domain.UseSessionStates(domain.NewStaticStates(nil))()
	if _, err := domain.AgentReportVerb(domain.FullSessionID(id), "done", true); err != nil {
		t.Fatal(err)
	}
	if text, _ := sessionBadge(id, time.Now()); !strings.HasPrefix(text, "done ") {
		t.Fatalf("%q", text)
	}
	if row := sessionStateText(s.Info()); !strings.Contains(row, " — done") && !strings.HasSuffix(row, " — done") {
		t.Fatalf("the popup row carries the report line: %q", row)
	}
	time.Sleep(2 * time.Millisecond)
	s.SendText("thanks")
	if text, _ := sessionBadge(id, time.Now()); text != "" {
		t.Fatalf("answered report must clear (no activity → empty): %q", text)
	}
	if line := sessionReportLine(id); line != "" {
		t.Fatalf("line must clear too: %q", line)
	}
}

func TestActivityNoticeTextReport(t *testing.T) {
	n := domain.ActivityNotice{Kind: "report", Label: "claude", Dir: "/a/b/wt", Text: "merged feat/x"}
	if got := activityNoticeText(n); got != "claude in wt reports: merged feat/x" {
		t.Fatalf("%q", got)
	}
}

// A model built while the watcher already holds notices starts reading after
// them: a second tui.New in one process never replays old notices.
func TestNewStartsAfterTheNoticesAlreadyPosted(t *testing.T) {
	w := domain.NewStaticStates(nil)
	defer domain.UseSessionStates(w)()
	w.PostNotice(domain.ActivityNotice{ID: "s1", Kind: "question", Label: "Claude", Dir: "/wt/feat-a"})
	repo, _ := newRepoDir(t)
	m := New(domain.Open(repo))
	m, _ = m.onSessionActivity()
	if m.statusMsg != "" {
		t.Fatalf("an old notice was replayed: %q", m.statusMsg)
	}
}

// A start's status line keeps both notes: the worktree's (git fails there
// until repaired) and the command's invalid screen rules.
func TestStartNoteKeepsBothNotes(t *testing.T) {
	bad := config.ToolCommand{Category: "session", Name: "Claude", Command: "claude", ScreenQuestion: []string{`(`}}
	got := startNote("started in /x — git there fails", bad)
	if !strings.Contains(got, "git there fails") || !strings.Contains(got, "screen rules of Claude") {
		t.Fatalf("note = %q", got)
	}
	if got := startNote("", bad); !strings.HasPrefix(got, "screen rules of Claude") {
		t.Fatalf("rules only = %q", got)
	}
	if got := startNote("place", config.ToolCommand{Name: "Claude", Command: "claude"}); got != "place" {
		t.Fatalf("place only = %q", got)
	}
}
