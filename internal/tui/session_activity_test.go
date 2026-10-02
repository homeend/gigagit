package tui

import (
	"strings"
	"testing"
	"time"

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
