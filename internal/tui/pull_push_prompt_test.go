package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// ---- pullPrompt: the p-key confirm names the branch the pull targets ----

func TestPullPromptNamesCurrentBranch(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.focus = panelCommits // off the Branches panel → pull-current
	op := m.pullForFocus()
	if got, want := m.pullPrompt(op), "Pull main? This may rewrite the working tree."; got != want {
		t.Fatalf("pullPrompt = %q, want %q", got, want)
	}
}

func TestPullPromptNamesBackgroundBranch(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.focus = panelBranches
	m.sel[panelBranches] = 1 // feat/a, non-current → background pull
	op := m.pullForFocus()
	if got, want := m.pullPrompt(op), "Pull feat/a (stay here)?"; got != want {
		t.Fatalf("pullPrompt = %q, want %q", got, want)
	}
}

func TestPullPromptFallsBackWithoutBranch(t *testing.T) {
	t.Parallel()
	want := "Pull? This may rewrite the working tree."
	for _, br := range []string{"", "(detached)"} {
		m := markModel()
		m.status = model.WorkingTreeStatus{Branch: br}
		m.focus = panelCommits
		if got := m.pullPrompt(m.pullForFocus()); got != want {
			t.Fatalf("Branch=%q: pullPrompt = %q, want %q", br, got, want)
		}
	}
}

// ---- p key end-to-end: the slow-op confirm carries the branch-naming prompt ----

func TestPullKeyConfirmNamesBranch(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.focus = panelCommits
	mm := pressRune(t, m, "p")
	if mm.modal == nil || !mm.modal.confirm {
		t.Fatal("p must pop the slow-op confirm modal")
	}
	if !strings.Contains(mm.modal.req.Prompt, "main") {
		t.Fatalf("pull confirm prompt %q does not name the branch", mm.modal.req.Prompt)
	}
}

// ---- push-with-tags modal: prompt names the branch being pushed ----

func pushTagModal(t *testing.T, tags []model.Tag, remoteSet map[string]bool) *decisionState {
	t.Helper()
	m := footerModel()
	m.branches = []model.Branch{{Name: "main", IsHead: true, Hash: "abc1234"}}
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.tags = tags
	m.pushCheckGen = 1
	msg := pushTagCheckMsg{gen: 1, tipTags: tagsAtCommit(tags, "abc1234"), remoteSet: remoteSet}
	u, _ := m.Update(msg)
	got := u.(Model)
	if got.modal == nil {
		t.Fatal("unpushed tip tag: modal must open")
	}
	return got.modal
}

func TestPushTagModalPromptNamesBranch(t *testing.T) {
	t.Parallel()
	modal := pushTagModal(t, []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}, map[string]bool{})
	want := "Push main: branch tip has tag v1.0.0 not on the remote. Push too?"
	if modal.req.Prompt != want {
		t.Fatalf("prompt = %q, want %q", modal.req.Prompt, want)
	}
}

func TestPushTagModalPromptPluralNamesBranch(t *testing.T) {
	t.Parallel()
	tags := []model.Tag{{Name: "v1.0.0", Target: "abc1234"}, {Name: "v1.1.0", Target: "abc1234"}}
	modal := pushTagModal(t, tags, map[string]bool{})
	want := "Push main: branch tip has tags v1.0.0, v1.1.0 not on the remote. Push too?"
	if modal.req.Prompt != want {
		t.Fatalf("prompt = %q, want %q", modal.req.Prompt, want)
	}
}
