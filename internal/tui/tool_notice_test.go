package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
)

func findNotice(m Model, id string) *notice {
	for i := range m.notices {
		if m.notices[i].id == id {
			return &m.notices[i]
		}
	}
	return nil
}

func TestToolTemplateNoticeCountsOffers(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	a, b := sampleToolStatus(t), sampleToolStatus(t)
	b.Block.Name, b.New.Name = "Other", "Other"
	nm, _ := m.Update(toolStatusesMsg{gen: m.noticeGen, sts: []domain.ToolTemplateStatus{a, b}})
	m = nm.(Model)
	n := findNotice(m, noticeToolTemplateUpdate)
	if n == nil || !strings.HasSuffix(n.title, ": 2") {
		t.Fatalf("want a 2-offer notice, got %+v", m.notices)
	}
	if !m.noticesUnread {
		t.Fatal("a new tool-template notice must mark notices unread")
	}
	m.promptStore.DeclineToolUpdate(a.OfferKey())
	m.promptStore.DeclineToolUpdate(b.OfferKey())
	m = m.rebuildNotices()
	if findNotice(m, noticeToolTemplateUpdate) != nil {
		t.Fatal("declined offers must not be counted")
	}
}

func TestToolTemplateNoticeForUnsupportedAgent(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	st := sampleToolStatus(t)
	st.Kind, st.AgentVersion = domain.ToolUnsupported, "1.2.0"
	nm, _ := m.Update(toolStatusesMsg{gen: m.noticeGen, sts: []domain.ToolTemplateStatus{st}})
	m = nm.(Model)
	var got *notice
	for i := range m.notices {
		if strings.HasPrefix(m.notices[i].id, "tool_agent_unsupported_") {
			got = &m.notices[i]
		}
	}
	if got == nil || !strings.Contains(got.title, "1.2.0") {
		t.Fatalf("want an unsupported-agent notice, got %+v", m.notices)
	}
	if findNotice(m, noticeToolTemplateUpdate) != nil {
		t.Fatal("an unsupported status is not an update offer")
	}
}

func TestToolStatusesStaleGenDropped(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	nm, _ := m.Update(toolStatusesMsg{gen: m.noticeGen + 1, sts: []domain.ToolTemplateStatus{sampleToolStatus(t)}})
	if len(nm.(Model).toolStatuses) != 0 {
		t.Fatal("a stale-gen read must be dropped")
	}
}

// The trigger case: a web-only Claude headless complete block → take new →
// the TUI conflict picker's tool list includes it.
func TestTakeNewMakesHeadlessCompleteVisibleInTUI(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	st := sampleToolStatus(t)
	m.repoConfigPath = st.Path // the block lives in this file for the test
	m = m.reloadToolConfig()
	if len(m.toolCommands("conflict_complete")) != 0 {
		t.Fatal("precondition: a web-only block must be hidden in the TUI")
	}
	m = m.pushLayer(&toolUpdatePopup{st: st})
	m = sendKey(m, keyRunes("t"))
	if len(m.toolCommands("conflict_complete")) != 1 {
		t.Fatal("after take new the TUI must offer the headless complete tool")
	}
}
