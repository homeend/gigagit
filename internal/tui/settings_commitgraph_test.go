package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

func commitGraphMenuIndex(t *testing.T) int {
	t.Helper()
	for i, entry := range settingsMenu {
		if entry == settingsMenuCommitGraph {
			return i
		}
	}
	t.Fatal("Commit-graph entry missing from the settings menu")
	return -1
}

func TestCommitGraphLabelStates(t *testing.T) {
	m, _ := noticeTestModel(t)
	idx := commitGraphMenuIndex(t)

	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "checking") {
		t.Fatalf("unknown health: label = %q, want '(checking…)'", got)
	}
	m.repoHealthKnown = true
	m.repoHealth = model.RepoHealth{}
	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "missing") {
		t.Fatalf("missing graph: label = %q", got)
	}
	m.repoHealth = model.RepoHealth{HasCommitGraph: true, WriteCommitGraphSet: true, WriteCommitGraphValue: "true"}
	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "auto-refresh on") {
		t.Fatalf("present+on: label = %q", got)
	}
	m.repoHealth = model.RepoHealth{HasCommitGraph: true}
	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "auto-refresh off") {
		t.Fatalf("present+off: label = %q", got)
	}
}

func TestCommitGraphRowEnterStartsWriteAndEnable(t *testing.T) {
	m, _ := noticeTestModel(t)
	m.repoHealthKnown = true
	m.repoHealth = model.RepoHealth{} // missing → write+enable
	nm, _ := m.Update(keyMsg(","))
	m = nm.(Model)
	p := layerOf[*settingsPopup](m)
	p.menuSel = commitGraphMenuIndex(t)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if !m.running {
		t.Fatal("enter on the missing-graph row must start the write+enable op")
	}
	if m.pendingNoticeConfig == nil {
		t.Fatal("the SetGitConfig chain must be armed (same code path as the notice action)")
	}
	m = driveOp(t, m, cmd)
}

func TestCommitGraphRowNoOpWhenHealthy(t *testing.T) {
	m, _ := noticeTestModel(t)
	m.repoHealthKnown = true
	m.repoHealth = model.RepoHealth{HasCommitGraph: true, WriteCommitGraphSet: true, WriteCommitGraphValue: "true"}
	nm, _ := m.Update(keyMsg(","))
	m = nm.(Model)
	p := layerOf[*settingsPopup](m)
	p.menuSel = commitGraphMenuIndex(t)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if m.running {
		t.Fatal("already present + auto-refresh on: enter must be a no-op")
	}
	if m.statusMsg == "" {
		t.Fatal("the no-op must say why in the status line")
	}
}

func TestOpenSettingsRefreshesHealth(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, cmd := m.Update(keyMsg(","))
	_ = nm
	if cmd == nil {
		t.Fatal("opening Settings must re-read repo health so the Commit-graph label is fresh")
	}
}
