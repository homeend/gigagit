package tui

import (
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

func TestTagsCopyRows(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234", Annotated: true}}
	rows := m.contextCopyRows()
	if r, ok := findRow(rows, "copy-tag-name"); !ok || r.copyText != "v1.0.0" {
		t.Fatalf("missing copy-tag-name=v1.0.0; rows=%v", rows)
	}
	if r, ok := findRow(rows, "copy-commit-id"); !ok || r.copyText != "abc1234" {
		t.Fatalf("missing copy-commit-id=abc1234; rows=%v", rows)
	}
	if _, ok := findRow(rows, "copy-commit-sha"); !ok {
		t.Fatalf("missing copy-commit-sha; rows=%v", rows)
	}
}

func TestTagsCopyShaResolvesTarget(t *testing.T) {
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git rev-parse", gitexec.Result{Stdout: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n"})
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234", Annotated: true}}
	m.svc = domain.New(&git.Repo{Runner: fr})
	rows := m.contextCopyRows()
	row, ok := findRow(rows, "copy-commit-sha")
	if !ok {
		t.Fatal("missing copy-commit-sha")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("copy-commit-sha run returned nil cmd")
	}
	// The headline correctness point: rev-parse must resolve the tag's TARGET
	// commit, not the tag name (rev-parse <annotated-tag> would give the tag
	// object, not the commit).
	var resolved string
	for _, c := range fr.Calls {
		if c.Name == "git rev-parse" {
			resolved = c.Argv[len(c.Argv)-1]
		}
	}
	if resolved != "abc1234" {
		t.Fatalf("rev-parse resolved %q, want tag.Target abc1234 (not the tag name)", resolved)
	}
}

func TestEnterOnTagJumpsToCommit(t *testing.T) {
	m := footerModel()
	m.commits = []model.Commit{
		{Hash: "1111111aaaa", Subject: "one"},
		{Hash: "2222222bbbb", Subject: "two"},
	}
	m.tags = []model.Tag{{Name: "v1", Target: "2222222", Annotated: false}}
	m.activeFilesTab = panelTags
	m.focus = panelTags
	m.sel[panelTags] = 0

	u, _ := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.focus != panelCommits {
		t.Fatalf("focus = %v, want panelCommits", mm.focus)
	}
	_, idx := mm.panelView(panelCommits)
	if got := idx[mm.sel[panelCommits]]; got != 1 {
		t.Fatalf("selected commit backing idx = %d, want 1 (the v1 target)", got)
	}
}

// When the tag's target commit is NOT in the loaded feed (common in big repos
// with old release tags), enter opens that commit's files view directly by hash
// — instead of the old "target not in the loaded commits" dead end.
func TestEnterOnTagNotLoadedOpensFilesView(t *testing.T) {
	m := footerModel()
	m.commits = []model.Commit{{Hash: "1111111aaaa", Subject: "one"}}
	m.tags = []model.Tag{{Name: "v1", Target: "9999999", Subject: "rel one"}}
	m.activeFilesTab = panelTags
	m.focus = panelTags
	m.sel[panelTags] = 0
	u, _ := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.filesView == nil {
		t.Fatal("enter on a tag whose target isn't loaded should open the commit's files view")
	}
	if mm.filesHash != "9999999" {
		t.Fatalf("filesHash = %q, want the tag target 9999999", mm.filesHash)
	}
}

func TestTagDeleteRowOpensConfirmThenDeletes(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitIn(t, dir, "tag", "v1.0.0") // before loadModel so the snapshot loads it
	m := loadModel(t, repo)
	m.focus = panelTags
	m.activeFilesTab = panelTags
	m.sel[panelTags] = 0

	row, ok := m.tagDeleteRow()
	if !ok {
		t.Fatal("delete row must appear on the Tags panel with a selection")
	}
	u, _ := row.run(m)
	m = u.(Model)
	if m.modal == nil {
		t.Fatal("delete must open a confirm modal")
	}
	um, cmd := m.modal.onResolve(m, "Delete")
	m = um.(Model)
	for i := 0; i < 100 && m.running; i++ {
		uu, next := m.Update(cmd())
		m = uu.(Model)
		cmd = next
	}
	if exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/tags/v1.0.0").Run() == nil {
		t.Fatal("tag should be gone after confirm")
	}
}

func TestTagDeleteRowCancelKeepsTag(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitIn(t, dir, "tag", "v1.0.0")
	m := loadModel(t, repo)
	m.focus = panelTags
	m.activeFilesTab = panelTags
	m.sel[panelTags] = 0
	row, _ := m.tagDeleteRow()
	u, _ := row.run(m)
	m = u.(Model)
	um, _ := m.modal.onResolve(m, "Cancel")
	m = um.(Model)
	if m.running {
		t.Fatal("Cancel must not start a delete")
	}
	if exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/tags/v1.0.0").Run() != nil {
		t.Fatal("tag must survive Cancel")
	}
}

func TestTagDeleteRowInertOffTagsPanel(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches
	if _, ok := m.tagDeleteRow(); ok {
		t.Fatal("delete row must be inert off the Tags panel")
	}
}

func gitCurrentBranch(t *testing.T, dir string) string {
	t.Helper()
	out, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	return strings.TrimSpace(string(out))
}

func TestTagCheckoutRowDetached(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitIn(t, dir, "tag", "v1.0.0")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c2")
	m := loadModel(t, repo)
	m.focus = panelTags
	m.activeFilesTab = panelTags
	m.sel[panelTags] = 0
	row, ok := m.tagCheckoutRow()
	if !ok {
		t.Fatal("checkout row must appear on the Tags panel")
	}
	u, _ := row.run(m)
	m = u.(Model)
	if m.modal == nil {
		t.Fatal("checkout must open the detached/branch modal")
	}
	um, cmd := m.modal.onResolve(m, "Detached")
	m = um.(Model)
	for i := 0; i < 100 && m.running; i++ {
		uu, next := m.Update(cmd())
		m = uu.(Model)
		cmd = next
	}
	if b := gitCurrentBranch(t, dir); b != "" {
		t.Fatalf("expected detached HEAD, on %q", b)
	}
}

func TestTagCheckoutPopupCreatesBranch(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitIn(t, dir, "tag", "v1.0.0")
	m := loadModel(t, repo)
	m = m.pushLayer(&tagCheckoutPopup{tag: "v1.0.0"})
	for _, r := range "rel" {
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(Model)
	for i := 0; i < 100 && m.running; i++ {
		uu, next := m.Update(cmd())
		m = uu.(Model)
		cmd = next
	}
	if b := gitCurrentBranch(t, dir); b != "rel" {
		t.Fatalf("on branch %q, want rel", b)
	}
}

// Detached HEAD (CurrentBranch == "") must still render without panicking.
func TestRenderWithDetachedHead(t *testing.T) {
	m := footerModel()
	m.status.Branch = "" // detached
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "1111111", Subject: "x"}}
	m.sel[panelCommits] = 0
	out := m.View()
	if out == "" {
		t.Fatal("View() must render with a detached HEAD")
	}
}

// "Create branch…" seeds the popup's branch name with the tag name.
func TestTagCheckoutBranchPrefilledFromTag(t *testing.T) {
	m := footerModel()
	m.tags = []model.Tag{{Name: "v1.0.0"}}
	m.focus = panelTags
	m.activeFilesTab = panelTags
	m.sel[panelTags] = 0
	row, ok := m.tagCheckoutRow()
	if !ok {
		t.Fatal("checkout row must appear")
	}
	u, _ := row.run(m)
	m = u.(Model)
	um, _ := m.modal.onResolve(m, "Create branch…")
	m = um.(Model)
	p := layerOf[*tagCheckoutPopup](m)
	if p == nil || p.name.Value() != "v1.0.0" {
		t.Fatalf("branch popup name = %+v, want prefilled v1.0.0", p)
	}
}

// "Create worktree…" opens the worktree dialog seeded with the tag name, and the
// path leaf is the tag name sanitized into a single segment ('/' -> '-').
func TestTagCheckoutWorktreePrefilledAndSanitized(t *testing.T) {
	m := modelWithConfig(t, "wt/<parent-branch>", "../<repo>.worktrees/<branch>")
	m.tags = []model.Tag{{Name: "release/1.0"}}
	m.focus = panelTags
	m.activeFilesTab = panelTags
	m.sel[panelTags] = 0
	row, ok := m.tagCheckoutRow()
	if !ok {
		t.Fatal("checkout row must appear")
	}
	u, _ := row.run(m)
	m = u.(Model)
	um, _ := m.modal.onResolve(m, "Create worktree…")
	m = um.(Model)
	p := layerOf[*worktreePopup](m)
	if p == nil {
		t.Fatal("worktree popup must open")
	}
	if p.editBuf.Value() != "release/1.0" {
		t.Fatalf("branch seed = %q, want release/1.0", p.editBuf.Value())
	}
	if p.startPoint != "release/1.0" {
		t.Fatalf("startPoint = %q, want the tag", p.startPoint)
	}
	if !strings.Contains(p.previewPath, "release-1.0") || strings.Contains(p.previewPath, "release/1.0") {
		t.Fatalf("preview path = %q, want a sanitized leaf release-1.0 (no slash)", p.previewPath)
	}
}

func TestTagPushRowGating(t *testing.T) {
	m := footerModel()
	m.tags = []model.Tag{{Name: "v1.0.0"}}
	m.focus = panelBranches
	if _, ok := m.tagPushRow(); ok {
		t.Fatal("push row inert off the Tags panel")
	}
	m.focus = panelTags
	m.activeFilesTab = panelTags
	m.sel[panelTags] = 0
	if _, ok := m.tagPushRow(); !ok {
		t.Fatal("push row must appear on the Tags panel")
	}
}

func tagsMergeModel() Model {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m.status.Branch = "main"
	return m
}

func TestTagMergeRebaseRowsPresent(t *testing.T) {
	m := tagsMergeModel()
	got := ids(availableActions(m))
	if !got["tag-merge"] || !got["tag-rebase"] {
		t.Fatalf("expected tag-merge + tag-rebase; got %v", got)
	}
}

func TestTagMergeRebaseHiddenOnDetachedHEAD(t *testing.T) {
	m := tagsMergeModel()
	m.status.Branch = "" // detached
	got := ids(availableActions(m))
	if got["tag-merge"] || got["tag-rebase"] {
		t.Fatalf("merge/rebase must be hidden on detached HEAD; got %v", got)
	}
}

func TestTagMergeRowDispatches(t *testing.T) {
	m := tagsMergeModel()
	row, ok := m.tagMergeRow()
	if !ok {
		t.Fatal("tagMergeRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("merge row run returned nil cmd")
	}
}

func TestTagRebaseRowDispatches(t *testing.T) {
	m := tagsMergeModel()
	row, ok := m.tagRebaseRow()
	if !ok {
		t.Fatal("tagRebaseRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("rebase row run returned nil cmd")
	}
}

func TestTagDeleteRemoteRowPresent(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	got := ids(availableActions(m))
	if !got["tag-delete-remote"] {
		t.Fatalf("expected tag-delete-remote; got %v", got)
	}
}

func TestTagDeleteRemoteRowInertOffTagsPanel(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	if _, ok := m.tagDeleteRemoteRow(); ok {
		t.Fatal("tag-delete-remote must be inert off the Tags panel")
	}
}

func TestTagDeleteRemoteRowDispatches(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	row, ok := m.tagDeleteRemoteRow()
	if !ok {
		t.Fatal("tagDeleteRemoteRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("delete-remote row run returned nil cmd")
	}
}

func TestTagAnnotateRowPresent(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	got := ids(availableActions(m))
	if !got["tag-annotate"] {
		t.Fatalf("expected tag-annotate; got %v", got)
	}
}

func TestAnnotatePopupPrefillsSubject(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234", Annotated: true, Subject: "old message"}}
	m, ok := m.openAnnotateTagPopup()
	if !ok {
		t.Fatal("openAnnotateTagPopup returned false")
	}
	p := layerOf[*annotateTagPopup](m)
	if p == nil || p.message.Value() != "old message" {
		t.Fatalf("popup message = %+v, want prefilled 'old message'", p)
	}
	if p.target != "abc1234" {
		t.Fatalf("popup target = %q, want abc1234", p.target)
	}
}

func TestAnnotatePopupEmptyMessageKeepsOpen(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}} // lightweight → blank subject
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m, _ = m.openAnnotateTagPopup()
	p := layerOf[*annotateTagPopup](m)
	if p == nil {
		t.Fatal("no popup")
	}
	um, _ := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = um
	if layerOf[*annotateTagPopup](m) == nil {
		t.Fatal("empty message must keep the popup open (annotate requires a message)")
	}
}

func TestAnnotatePopupSubmitDispatches(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m, _ = m.openAnnotateTagPopup()
	p := layerOf[*annotateTagPopup](m)
	p.message = newTextField("a message")
	_, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("submit with a message must start the op (non-nil cmd)")
	}
}
