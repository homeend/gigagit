package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
)

// noticeTestModel: loaded model + temp prompt store (never the developer's
// real prompts.toml — same rule as promptTestModel in related_prompts_test.go).
func noticeTestModel(t *testing.T) (Model, promptstate.Store) {
	t.Helper()
	m, _ := settingsModel(t)
	st := promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.promptStore = st
	return m, st
}

// bigRepoHealth fabricates a health snapshot that should trigger the
// commit-graph notice: big pack, no graph, config unset.
func bigRepoHealth() model.RepoHealth {
	return model.RepoHealth{
		GitCommonDir: "/fake/common/dir",
		PackBytes:    bigRepoPackBytes + 1,
	}
}

func TestRepoHealthMsgBuildsCommitGraphNotice(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	if len(m.notices) != 1 || m.notices[0].id != noticeCommitGraph {
		t.Fatalf("notices = %+v, want the commit-graph notice", m.notices)
	}
	if !m.noticesUnread {
		t.Fatal("a fresh notice must start unread (blinking)")
	}
	if seg := m.noticeSegment(); !strings.Contains(seg, "1 notice") {
		t.Fatalf("status segment = %q, want '! 1 notice …'", seg)
	}
}

func TestNoNoticeWhenRepoSmallOrGraphPresentOrConfigSet(t *testing.T) {
	m, _ := noticeTestModel(t)
	small := bigRepoHealth()
	small.PackBytes = bigRepoPackBytes - 1
	graphed := bigRepoHealth()
	graphed.HasCommitGraph = true
	configured := bigRepoHealth()
	configured.WriteCommitGraphSet = true
	for name, h := range map[string]model.RepoHealth{"small": small, "graphed": graphed, "configured": configured} {
		nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: h})
		if got := nm.(Model).notices; len(got) != 0 {
			t.Fatalf("%s: notices = %+v, want none", name, got)
		}
	}
}

func TestStaleHealthResultDropped(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen - 1, health: bigRepoHealth()})
	if got := nm.(Model).notices; len(got) != 0 {
		t.Fatalf("stale gen must be dropped, got %+v", got)
	}
}

func TestPersistedDismissalFiltersNotice(t *testing.T) {
	m, st := noticeTestModel(t)
	h := bigRepoHealth()
	if err := st.DismissNotice(h.GitCommonDir, noticeCommitGraph); err != nil {
		t.Fatal(err)
	}
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: h})
	if got := nm.(Model).notices; len(got) != 0 {
		t.Fatalf("persisted dismissal must filter the notice, got %+v", got)
	}
}

func TestSessionDismissalSurvivesHealthRereadWithinSession(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	m = m.removeNotice(noticeCommitGraph)
	m.noticeSessionDismissed[noticeCommitGraph] = true // what "Not now" records
	nm, _ = m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	if got := nm.(Model).notices; len(got) != 0 {
		t.Fatalf("a session-dismissed notice must not resurrect on a mid-session re-read, got %+v", got)
	}
}

func TestBlinkTickStopsWhenRead(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, cmd := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("a fresh unread notice must arm the blink tick")
	}
	before := m.blinkOn
	nm, cmd = m.Update(noticeBlinkMsg{})
	m = nm.(Model)
	if m.blinkOn == before {
		t.Fatal("blink tick must flip the phase while unread")
	}
	if cmd == nil {
		t.Fatal("blink must re-arm while unread")
	}
	m.noticesUnread = false // what opening the dialog does
	_, cmd = m.Update(noticeBlinkMsg{})
	if cmd != nil {
		t.Fatal("blink must stop re-arming once read")
	}
}

func TestUnreadOnlyOnNewNoticeIds(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	m.noticesUnread = false // user opened the dialog (read)
	nm, _ = m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	if nm.(Model).noticesUnread {
		t.Fatal("a re-read carrying the SAME notice ids must not re-blink")
	}
}
