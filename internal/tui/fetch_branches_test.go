package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// behindFixture builds a clone whose main tracks origin/main and a second
// clone that pushes one more commit to origin: after a fetch, the first
// clone's main is one behind. Returns the first clone's directory.
func behindFixture(t *testing.T) string {
	t.Helper()
	bare := t.TempDir()
	pushRealGit(t, bare, "init", "--bare", "-b", "main")
	dir := t.TempDir()
	pushRealGit(t, dir, "init", "-b", "main")
	pushRealGit(t, dir, "config", "user.email", "a@b.c")
	pushRealGit(t, dir, "config", "user.name", "a")
	pushRealGit(t, dir, "remote", "add", "origin", bare)
	pushRealGit(t, dir, "commit", "--allow-empty", "-m", "init")
	pushRealGit(t, dir, "push", "-u", "origin", "main")

	other := t.TempDir()
	pushRealGit(t, other, "clone", bare, ".")
	pushRealGit(t, other, "config", "user.email", "a@b.c")
	pushRealGit(t, other, "config", "user.name", "a")
	pushRealGit(t, other, "commit", "--allow-empty", "-m", "upstream work")
	pushRealGit(t, other, "push", "origin", "main")
	return dir
}

// pumpUntil runs cmd and every command its messages chain, feeding each
// message back through Update, until done(m) holds. A command that does not
// return within a second (a tick, a never-ending watcher) is dropped.
func pumpUntil(t *testing.T, m Model, cmd tea.Cmd, done func(Model) bool) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	deadline := time.Now().Add(20 * time.Second)
	for len(queue) > 0 && !done(m) {
		if time.Now().After(deadline) {
			break
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		ch := make(chan tea.Msg, 1)
		go func() { ch <- c() }()
		var msg tea.Msg
		select {
		case msg = <-ch:
		case <-time.After(time.Second):
			continue
		}
		switch v := msg.(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, v...)
		default:
			nm, next := m.Update(v)
			m = nm.(Model)
			queue = append(queue, next)
		}
	}
	if !done(m) {
		t.Fatal("the expected state never landed")
	}
	return m
}

func branchNamed(m Model, name string) (model.Branch, bool) {
	for _, b := range m.branches {
		if b.Name == name {
			return b, true
		}
	}
	return model.Branch{}, false
}

// A fetch that leaves main behind its upstream shows it: the Branches row's
// (↓N) and the header's ↑/↓ — the fetch reloads the branch list, which
// carries the ahead/behind counts, not just the remote-tracking list.
func TestFetchShowsBranchBehind(t *testing.T) {
	t.Parallel()
	m := loadedModelAt(t, behindFixture(t))
	if b, ok := branchNamed(m, "main"); !ok || b.Behind != 0 {
		t.Fatalf("before the fetch main must be level: %+v ok=%v", b, ok)
	}
	m, cmd := m.startOp(engine.Fetch{})
	m = pumpUntil(t, m, cmd, func(m Model) bool {
		b, _ := branchNamed(m, "main")
		return !m.running && b.Behind == 1
	})
	rows := m.branchRows()
	found := false
	for _, r := range rows {
		if contains(r, "main") && contains(r, "(↓1)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the main row must show (↓1): %q", rows)
	}
	if m.status.Behind != 1 {
		t.Fatalf("the header must show main one behind, status=%+v", m.status)
	}
}

// A background fetch queues every read a moved remote-tracking ref dirties:
// remotes, the branch list's ahead/behind, the feed's tips.
func TestBgFetchEnqueuesBranchesAndFeed(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.bgBusy = true
	m.bgActiveItem = fetchItem
	nm, _ := m.Update(bgFetchDoneMsg{dur: time.Second})
	mm := nm.(Model)
	for _, want := range []sourceKey{srcRemotes, srcBranches, srcFeed} {
		found := false
		for _, it := range mm.bgQueue {
			if it == (refreshItem{source: want}) {
				found = true
			}
		}
		if !found {
			t.Errorf("a finished background fetch must queue %s, queue=%+v", sourceNames[want], mm.bgQueue)
		}
	}
}
