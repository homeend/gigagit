package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

func lockHealth(locks ...model.GitLock) model.RepoHealth {
	return model.RepoHealth{GitCommonDir: "/fake/common/dir", StaleLocks: locks}
}

// fixedNow is an arbitrary fixed instant; ages are computed against it so the
// rendered label never depends on how long the test itself took.
var fixedNow = time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

func aLock(name string, age time.Duration) model.GitLock {
	return model.GitLock{
		Path:    "/fake/common/dir/" + name,
		Name:    name,
		ModTime: fixedNow.Add(-age),
	}
}

func TestStaleLockNoticeBuilder(t *testing.T) {
	t.Parallel()
	now := fixedNow
	// Fires on any lock present.
	n := staleLockNotice(lockHealth(aLock("index.lock", 3*time.Minute)), now)
	if n == nil || n.id != noticeStaleLock {
		t.Fatalf("want the stale-lock notice, got %+v", n)
	}
	if !strings.Contains(n.title, "A git lock file is present") {
		t.Fatalf("singular title = %q", n.title)
	}
	body := strings.Join(n.detail, "\n")
	if !strings.Contains(body, "index.lock") {
		t.Fatal("the notice must name the lock file it found")
	}
	// The user cannot judge staleness without the age.
	if !strings.Contains(body, "3m old") {
		t.Fatalf("detail should carry the lock's age, got:\n%s", body)
	}
	// gg cannot see git processes it did not start, so it must NOT assert the
	// lock is definitely stale — it must warn before offering removal.
	if !strings.Contains(body, "no other git is running") {
		t.Fatal("the notice must warn that a live git may legitimately hold the lock")
	}

	// nil when the repo is clean.
	if got := staleLockNotice(lockHealth(), now); got != nil {
		t.Fatalf("no locks should mean no notice, got %+v", got)
	}
}

func TestStaleLockNoticePluralises(t *testing.T) {
	t.Parallel()
	n := staleLockNotice(lockHealth(
		aLock("index.lock", time.Minute),
		aLock("HEAD.lock", time.Minute),
	), fixedNow)
	if !strings.Contains(n.title, "2 git lock files are present") {
		t.Fatalf("plural title = %q", n.title)
	}
	if n.actions[0].label != "Remove the lock files" {
		t.Fatalf("plural action label = %q", n.actions[0].label)
	}
}

func TestLockAgeLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "less than a minute old"},
		{5 * time.Minute, "5m old"},
		{3 * time.Hour, "3h old"},
		{50 * time.Hour, "2d old"},
	}
	for _, tc := range cases {
		if got := lockAgeLabel(tc.d); got != tc.want {
			t.Errorf("lockAgeLabel(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// The notice must offer removal through the real engine op — no ad-hoc
// os.Remove in the TUI, so the guard and the exclusive reservation apply.
func TestStaleLockNoticeActionDispatchesOp(t *testing.T) {
	t.Parallel()
	m, _ := noticeTestModel(t)
	lock := aLock("index.lock", time.Minute)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: lockHealth(lock)})
	m = nm.(Model)
	if len(m.notices) == 0 || m.notices[0].id != noticeStaleLock {
		t.Fatalf("notices = %+v, want the stale-lock notice first", m.notices)
	}
	// The stale lock outranks advisories: it blocks every other operation.
	if m.notices[0].id != noticeStaleLock {
		t.Fatal("the blocker notice must sort ahead of recommendations")
	}
	if m.notices[0].actions[0].run == nil {
		t.Fatal("the remove action must have a handler")
	}
}

// A failing operation whose error is git's lock message must surface the
// recovery immediately, not wait for the next repo load.
func TestLockErrorArmsHealthRefreshAndPointsAtTheKey(t *testing.T) {
	t.Parallel()
	m, _ := noticeTestModel(t)
	lockErr := fmt.Errorf("git add failed (exit 128): %s",
		"fatal: Unable to create '/r/.git/index.lock': File exists.\n\nAnother git process seems to be running in this repository")

	m.statusMsg = "error: something"
	got := m.maybeStaleLockNotice(lockErr)
	if !got.refreshHealthAfterOp {
		t.Fatal("a lock failure must trigger a health re-read so the notice appears now")
	}
	if !strings.Contains(got.statusMsg, "[!]") {
		t.Fatalf("status must point at the key that fixes it, got %q", got.statusMsg)
	}
}

// An unrelated failure must not arm anything.
func TestNonLockErrorIsIgnored(t *testing.T) {
	t.Parallel()
	m, _ := noticeTestModel(t)
	m.statusMsg = "error: merge conflict"
	got := m.maybeStaleLockNotice(errors.New("error: merge conflict"))
	if got.refreshHealthAfterOp {
		t.Fatal("an ordinary failure must not trigger the lock recovery")
	}
	if got.statusMsg != "error: merge conflict" {
		t.Fatalf("status was modified: %q", got.statusMsg)
	}
}

// Regression: this notice is a BLOCKER, so a session dismissal must not leave
// the user stuck with an unfixable error for the rest of the session. A NEW
// lock failure re-arms it (unlike the advisory notices, where "Not now"
// rightly holds until the next load).
func TestLockFailureReArmsSessionDismissal(t *testing.T) {
	t.Parallel()
	m, _ := noticeTestModel(t)
	m.noticeSessionDismissed = map[string]bool{noticeStaleLock: true}

	// While dismissed, a health read does not resurrect it.
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: lockHealth(aLock("index.lock", time.Minute))})
	if got := nm.(Model); len(got.notices) != 0 {
		t.Fatalf("a session-dismissed notice must stay hidden, got %+v", got.notices)
	}

	// A fresh lock failure is new information — re-arm.
	after := m.maybeStaleLockNotice(errors.New("fatal: Unable to create '/r/.git/index.lock': File exists"))
	if after.noticeSessionDismissed[noticeStaleLock] {
		t.Fatal("a new lock failure must clear the session dismissal, or the user is trapped")
	}
	nm2, _ := after.Update(repoHealthMsg{gen: after.noticeGen, health: lockHealth(aLock("index.lock", time.Minute))})
	if got := nm2.(Model); len(got.notices) != 1 {
		t.Fatalf("the notice must come back after a new failure, got %+v", got.notices)
	}
}

func TestRemoveGitLocksRefreshesStatusOnly(t *testing.T) {
	t.Parallel()
	got := opAffectedSources(engine.RemoveGitLocks{})
	if len(got) != 1 || got[0] != srcStatus {
		t.Fatalf("opAffectedSources = %v, want just srcStatus (never nil — that would fire the remote-tags probe)", got)
	}
}
