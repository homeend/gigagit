package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestSessionsPopupRowsGroupByRepoAndWorktree(t *testing.T) {
	t.Parallel()
	now := time.Now()
	list := []domain.SessionInfo{
		{ID: "s1", Label: "Claude", Repo: "gigagit", Dir: "/w/main", Started: now},
		{ID: "s2", Label: "Codex", Repo: "other", Dir: "/o/feat", Started: now, State: domain.SessionExited, ExitCode: 3},
		{ID: "s3", Label: "Kimi", Repo: "gigagit", Dir: "/w/main", Started: now},
	}
	rows, ids := sessionsPopupRows(list, "")
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"gigagit", "other", "main", "feat", "● Claude", "● Kimi", "○ Codex", "exited (3)"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("rows missing %q:\n%s", want, joined)
		}
	}
	if len(rows) != len(ids) {
		t.Fatal("rows and ids must align")
	}
	// gigagit's two sessions sit under ONE worktree header.
	if strings.Count(joined, "/w/main") != 1 {
		t.Fatalf("worktree header repeated:\n%s", joined)
	}
	var sessIDs []domain.SessionID
	for _, id := range ids {
		if id != "" {
			sessIDs = append(sessIDs, id)
		}
	}
	if len(sessIDs) != 3 {
		t.Fatalf("selectable ids = %v", sessIDs)
	}
	rows, _ = sessionsPopupRows(list, "codex")
	if j := strings.Join(rows, "\n"); strings.Contains(j, "Claude") || !strings.Contains(j, "Codex") {
		t.Fatalf("filter:\n%s", j)
	}
}

func TestCtrlBackslashOpensPopupAndEnterOpensConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	mm, _ := m.Update(ctrlBackslash())
	m = mm.(Model)
	p, ok := m.topLayer().(*sessionsPopup)
	if !ok {
		t.Fatalf("top = %T", m.topLayer())
	}
	if p.quitMode {
		t.Fatal("ctrl+\\ is not quit mode")
	}
	mm, _ = m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.topLayer() != nil || m.console == nil || m.console.id != s.Info().ID {
		t.Fatalf("enter must open the session's console: console=%+v top=%T", m.console, m.topLayer())
	}
}

func TestSessionsPopupKillAsksThenKills(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openSessionsPopup(false)
	mm, _ := m.Update(keyMsg("k"))
	m = mm.(Model)
	if p := m.topLayer().(*sessionsPopup); p.confirmKill != s.Info().ID {
		t.Fatal("k on a running session must ask first")
	}
	if s.Info().State != domain.SessionRunning {
		t.Fatal("the first k must not kill")
	}
	mm, _ = m.Update(keyMsg("k"))
	m = mm.(Model)
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the confirmed kill did not end the session")
	}
	mm, _ = m.Update(keyMsg("x"))
	m = mm.(Model)
	if _, ok := domain.Sessions().Get(s.Info().ID); ok {
		t.Fatal("x on an exited session must remove it")
	}
}

func TestSessionsPopupEmpty(t *testing.T) {
	m := loadedModel(t)
	startTestSession(t, m, `exit 0`) // fresh manager; remove the one session below
	for _, i := range domain.Sessions().List() {
		deadline := time.Now().Add(5 * time.Second)
		for {
			if s, ok := domain.Sessions().Get(i.ID); !ok || s.Info().State == domain.SessionExited || time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		_ = domain.Sessions().Remove(i.ID)
	}
	m, _ = m.openSessionsPopup(false)
	if m.topLayer() != nil || !strings.Contains(m.statusMsg, "no agent sessions") {
		t.Fatalf("empty: top=%T status=%q", m.topLayer(), m.statusMsg)
	}
}

func TestPopupOpensSessionFromOtherRepoWithoutReRoot(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`)
	other := t.TempDir()
	s, err := domain.Sessions().Start(sessionSpecForTest("Other", other))
	if err != nil {
		t.Fatal(err)
	}
	before, svc := m.currentWorktree, m.svc
	m, _ = m.openSessionsPopup(false)
	p := m.topLayer().(*sessionsPopup)
	for i, id := range p.ids {
		if id == s.Info().ID {
			p.sel = i
		}
	}
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.console == nil || m.console.id != s.Info().ID {
		t.Fatal("the other repo's session must open")
	}
	if m.currentWorktree != before || m.svc != svc {
		t.Fatal("opening a session must not reRoot")
	}
}
