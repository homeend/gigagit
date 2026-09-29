package tui

import (
	"io"
	"testing"

	"github.com/homeend/gigagit/internal/changeset"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// The drifted notice offers "Copy preview link" beside Dismiss — and the
// copy leaves the notice standing: it IS the list the user is about to
// research (the dismiss-only-surface lesson).
func TestDriftNoticeCopyLinkKeepsTheNotice(t *testing.T) {
	t.Parallel()
	var copied string
	m := Model{linkRepoName: "gigagit"}
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	report := domain.DriftReport{
		Ref: "refs/gg/versions/feat/1753100000-rebase", Checked: true,
		Report:  changeset.Report{Added: []changeset.Entry{{Status: 'A', Path: "f3.txt"}}},
		Version: model.BranchVersion{Ref: "refs/gg/versions/feat/1753100000-rebase", Op: "rebase", Unix: 1753100000, Base: tvBase, Ours: tvOurs},
	}
	m, _ = m.applyDriftReport("feat", report, false)
	if len(m.notices) != 1 {
		t.Fatalf("notices = %d, want 1", len(m.notices))
	}
	n := m.notices[0]
	act := noticeActionByLabel(t, &n, i18n.T("Copy preview link"))
	m, cmd := m.applyNoticeAction(n, act)
	if cmd == nil {
		t.Fatal("the action must return the copy command")
	}
	cmd()
	if want := "gg://gigagit@" + tvBase + ".." + tvOurs + "?version=1753100000-rebase"; copied != want {
		t.Fatalf("copied %q, want %q", copied, want)
	}
	if len(m.notices) != 1 {
		t.Fatalf("notices = %d after copying, want the notice kept", len(m.notices))
	}
	if m.noticeSessionDismissed[n.id] {
		t.Fatal("copying must not mark the notice dismissed")
	}
}

// A paused-only finding names no record: Dismiss stays alone.
func TestDriftNoticePausedOnlyHasNoCopyAction(t *testing.T) {
	t.Parallel()
	m := Model{linkRepoName: "gigagit"}
	m, _ = m.applyDriftReport("feat", domain.DriftReport{}, true)
	if len(m.notices) != 1 {
		t.Fatalf("notices = %d, want 1", len(m.notices))
	}
	for _, a := range m.notices[0].actions {
		if a.label == i18n.T("Copy preview link") {
			t.Fatal("a paused-only notice must not offer a link")
		}
	}
}
