package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/steer"
)

func TestMismatchedNavigateAsksInsteadOfLanding(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m.snapshotWorktree = "/w/a"
	c := steer.Command{ID: "n1", Cmd: "navigate", File: "x.go", Wait: true, Worktree: "/w/b", From: dir}
	m, cmd := m.applySteer(c)
	if cmd == nil {
		t.Fatal("the agent must be answered at once")
	}
	runCmds(cmd)
	rep, ok := steer.AwaitReply(dir, "n1", time.Second)
	if !ok || rep.OK || !strings.Contains(rep.Error, "asked the user to switch to /w/b") {
		t.Fatalf("reply = %+v ok=%v", rep, ok)
	}
	if m.steerAsk == nil || !m.noticesUnread {
		t.Fatal("a notice must ask the user")
	}
	if n := noticeByID(m, "steer_switch"); n == nil {
		t.Fatal("steer_switch notice missing")
	}
}

func TestMatchingOrUnboundCommandsApplyAsToday(t *testing.T) {
	t.Parallel()
	m, _ := steerModel(t)
	m.snapshotWorktree = "/w/a"
	for _, c := range []steer.Command{
		{ID: "f1", Cmd: "focus", Panel: "branches", Worktree: "/w/b"},
		{ID: "n2", Cmd: "navigate", File: "x.go", Worktree: "/w/a/"},
		{ID: "n5", Cmd: "navigate", File: "x.go", Worktree: "/w/./a"}, // another spelling (Windows: C:/ vs C:\)
		{ID: "n3", Cmd: "navigate", File: "x.go"},                     // no Worktree: an older CLI / --at
		{ID: "n4", Cmd: "navigate", Commit: "abc1234", Worktree: "/w/b"},
	} {
		if m2, _ := m.applySteer(c); m2.steerAsk != nil {
			t.Fatalf("%s must not ask", c.ID)
		}
	}
}

func TestIgnoringTheAskForgetsIt(t *testing.T) {
	t.Parallel()
	m, _ := steerModel(t)
	m.snapshotWorktree = "/w/a"
	m, _ = m.applySteer(steer.Command{ID: "n1", Cmd: "navigate", File: "x.go", Worktree: "/w/b"})
	n := noticeByID(m, "steer_switch")
	m, _ = m.applyNoticeAction(*n, noticeActionByLabel(t, n, i18n.T("Ignore")))
	m = m.rebuildNotices()
	if m.steerAsk != nil || noticeByID(m, "steer_switch") != nil {
		t.Fatal("Ignore must drop the ask for good")
	}
}

func TestAcceptingTheAskSwitchesAndReplays(t *testing.T) {
	t.Parallel()
	m, _ := steerModel(t)
	other, err := m.svc.TopLevel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m.snapshotWorktree = "/w/a"
	m, _ = m.applySteer(steer.Command{ID: "n1", Cmd: "navigate", File: "x.go", Wait: true, Worktree: other})
	n := noticeByID(m, "steer_switch")
	if n == nil {
		t.Fatal("no ask")
	}
	m, _ = m.applyNoticeAction(*n, noticeActionByLabel(t, n, i18n.T("Switch to %s and show", filepath.Base(other))))
	if m.startAtCmd == nil || !m.startAtPending {
		t.Fatal("accepting must arm the replay")
	}
	if m.startAtCmd.Wait || m.startAtCmd.Worktree != "" || m.startAtCmd.File != "x.go" {
		t.Fatalf("replay = %+v", *m.startAtCmd)
	}
	if m.steerAsk != nil {
		t.Fatal("the ask is answered")
	}
}

func noticeByID(m Model, id string) *notice {
	for i := range m.notices {
		if m.notices[i].id == id {
			return &m.notices[i]
		}
	}
	return nil
}

func noticeActionByLabel(t *testing.T, n *notice, label string) noticeAction {
	t.Helper()
	for _, a := range n.actions {
		if a.label == label {
			return a
		}
	}
	t.Fatalf("notice %s has no action %q", n.id, label)
	return noticeAction{}
}
