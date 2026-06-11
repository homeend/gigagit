package tui

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/template"
)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func testCtx() template.Ctx {
	return template.Ctx{
		ParentBranch: "main",
		Repo:         "aaa",
		Seqs:         map[string]int{"issue": 7},
		Now:          func() time.Time { return time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC) },
		Rand:         rand.New(rand.NewPCG(1, 2)),
	}
}

func TestResolveWorktreeNamesTwoPhase(t *testing.T) {
	branch, path, err := resolveWorktreeNames("issue/<seq:issue>", "../<repo>.worktrees/<branch>", "", nil, testCtx())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if branch != "issue/7" {
		t.Fatalf("branch = %q, want issue/7", branch)
	}
	if path != "../aaa.worktrees/issue-7" {
		t.Fatalf("path = %q, want ../aaa.worktrees/issue-7", path)
	}
}

func TestResolveWorktreeNamesFixedBranch(t *testing.T) {
	branch, path, err := resolveWorktreeNames("ignored/<seq:issue>", "wt/<branch>", "hand/edited", nil, testCtx())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if branch != "hand/edited" {
		t.Fatalf("branch = %q, want hand/edited", branch)
	}
	if path != "wt/hand-edited" {
		t.Fatalf("path = %q, want wt/hand-edited", path)
	}
}

func TestResolveWorktreeNamesPropagatesError(t *testing.T) {
	_, _, err := resolveWorktreeNames("b-<bogus>", "p/<branch>", "", nil, testCtx())
	if err == nil {
		t.Fatal("expected unknown-token error to propagate")
	}
}

func TestResolveWorktreeNamesUserInput(t *testing.T) {
	branch, _, err := resolveWorktreeNames("issue/<user:id>", "p/<branch>", "", map[string]string{"id": "42"}, testCtx())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if branch != "issue/42" {
		t.Fatalf("branch = %q, want issue/42", branch)
	}
}

func modelWithConfig(t *testing.T, branchTmpl, pathTmpl string) Model {
	t.Helper()
	m := loadedModel(t)
	m.cfg = config.Config{Worktree: config.WorktreeConfig{
		DefaultBranchTemplate: branchTmpl,
		PathTemplate:          pathTmpl,
	}}
	return m
}

func TestOpenPopupOnW(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	mm := updated.(Model)
	if mm.popup == nil {
		t.Fatal("pressing w should open the worktree popup")
	}
	if mm.popup.startPoint == "" {
		t.Error("popup startPoint (selected branch) should be set")
	}
	if mm.popup.state != stAction {
		t.Errorf("state = %v, want stAction when no user fields", mm.popup.state)
	}
	if mm.popup.previewBranch == "" {
		t.Error("preview should be computed on open")
	}
}

func TestPopupSwallowsGlobalKeys(t *testing.T) {
	m := modelWithConfig(t, "b/x", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("s"))
	m = updated.(Model)
	if m.running {
		t.Error("global keys must not fire while the popup is open")
	}
	if m.popup == nil {
		t.Error("popup should still be open after an inert key")
	}
}

func TestPopupEscCancels(t *testing.T) {
	m := modelWithConfig(t, "b/x", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("esc"))
	if updated.(Model).popup != nil {
		t.Error("esc should cancel the popup")
	}
}

func TestPopupInputFieldsAndPreview(t *testing.T) {
	m := modelWithConfig(t, "<user:user>/fix/<user:issue>", "wt/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	if m.popup.state != stInput {
		t.Fatalf("state = %v, want stInput with user fields", m.popup.state)
	}
	if len(m.popup.labels) != 2 || m.popup.labels[0] != "user" || m.popup.labels[1] != "issue" {
		t.Fatalf("labels = %v, want [user issue]", m.popup.labels)
	}

	for _, ch := range []string{"a", "l", "i", "c", "e"} {
		updated, _ = m.Update(keyMsg(ch))
		m = updated.(Model)
	}
	if m.popup.inputs["user"] != "alice" {
		t.Fatalf("first field = %q, want alice", m.popup.inputs["user"])
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)
	if m.popup.inputs["user"] != "alic" {
		t.Fatalf("after backspace = %q, want alic", m.popup.inputs["user"])
	}

	updated, _ = m.Update(keyMsg("tab"))
	m = updated.(Model)
	if m.popup.fieldIdx != 1 {
		t.Fatalf("fieldIdx = %d, want 1 after tab", m.popup.fieldIdx)
	}
	for _, ch := range []string{"7", "7"} {
		updated, _ = m.Update(keyMsg(ch))
		m = updated.(Model)
	}
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.popup.state != stAction {
		t.Fatalf("state = %v, want stAction after last field", m.popup.state)
	}
	if m.popup.previewBranch != "alic/fix/77" {
		t.Fatalf("preview branch = %q, want alic/fix/77", m.popup.previewBranch)
	}
}

func TestPopupBackspaceOnEmptyField(t *testing.T) {
	m := modelWithConfig(t, "issue/<user:id>", "wt/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)
	if m.popup.inputs["id"] != "" {
		t.Fatalf("field = %q, want empty", m.popup.inputs["id"])
	}
}

func TestPopupMultiByteRune(t *testing.T) {
	m := modelWithConfig(t, "issue/<user:id>", "wt/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("é")})
	m = updated.(Model)
	if m.popup.inputs["id"] != "é" {
		t.Fatalf("field = %q, want é", m.popup.inputs["id"])
	}
}

func TestPopupEditMode(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	if m.popup.previewBranch != "b/auto" {
		t.Fatalf("preview branch = %q, want b/auto", m.popup.previewBranch)
	}

	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	if m.popup.state != stEdit {
		t.Fatalf("state = %v, want stEdit", m.popup.state)
	}
	if m.popup.editBuf != "b/auto" {
		t.Fatalf("editBuf = %q, want b/auto", m.popup.editBuf)
	}

	for len([]rune(m.popup.editBuf)) > 0 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(Model)
	}
	for _, ch := range []string{"m", "y", "/", "b"} {
		updated, _ = m.Update(keyMsg(ch))
		m = updated.(Model)
	}
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.popup.state != stAction {
		t.Fatalf("state = %v, want stAction after enter", m.popup.state)
	}
	if m.popup.previewBranch != "my/b" {
		t.Fatalf("preview branch = %q, want my/b", m.popup.previewBranch)
	}
	if !contains(m.popup.previewPath, "my-b") {
		t.Fatalf("preview path = %q, want it to contain my-b", m.popup.previewPath)
	}
}

func TestPopupEditEscDiscards(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("z"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.popup.state != stAction {
		t.Fatalf("state = %v, want stAction after esc", m.popup.state)
	}
	if m.popup.previewBranch != "b/auto" {
		t.Fatalf("preview branch = %q, want b/auto after discard", m.popup.previewBranch)
	}
}

func TestPopupCreateLaunchesOpAndClearsPopup(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, cmd := m.Update(keyMsg("w")) // create
	m = updated.(Model)
	if m.popup != nil {
		t.Error("popup should close when the create op starts")
	}
	if !m.running {
		t.Error("create should put the model into the running state")
	}
	if cmd == nil {
		t.Error("create should return a command that waits for op messages")
	}
}

func TestPopupCreatePreviewErrorBlocks(t *testing.T) {
	m := modelWithConfig(t, "b-<bogus>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	if m.popup.previewErr == nil {
		t.Fatal("expected a preview error for the bad template")
	}
	updated, _ = m.Update(keyMsg("w")) // attempt create
	m = updated.(Model)
	if m.running {
		t.Error("create must not launch when the preview has an error")
	}
	if m.popup == nil {
		t.Error("popup should stay open when create is blocked")
	}
}

func TestSeqBumpOnSuccess(t *testing.T) {
	dir := t.TempDir() // stand-in git common dir
	m := loadedModel(t)
	m.gitCommonDir = dir
	m.pendingSeqBump = []string{"issue"}

	before := config.PeekSeq(dir, "issue") // 1 (unset)
	updated, _ := m.Update(opFinishedMsg{res: engine.Result{Summary: "worktree created", Changed: true}})
	m = updated.(Model)
	if m.pendingSeqBump != nil {
		t.Error("pendingSeqBump should be cleared after handling")
	}
	after := config.PeekSeq(dir, "issue")
	if after != before+1 {
		t.Fatalf("counter not bumped: before=%d after=%d", before, after)
	}
}

func TestSeqNoBumpOnError(t *testing.T) {
	dir := t.TempDir()
	m := loadedModel(t)
	m.gitCommonDir = dir
	m.pendingSeqBump = []string{"issue"}

	before := config.PeekSeq(dir, "issue")
	updated, _ := m.Update(opFinishedMsg{err: errTest})
	m = updated.(Model)
	after := config.PeekSeq(dir, "issue")
	if after != before {
		t.Fatalf("counter must not bump on error: before=%d after=%d", before, after)
	}
}

func TestRenderWorktreePopupShowsPreview(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>", "../<repo>.worktrees/<branch>")
	m.width, m.height = 80, 24
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)

	out := m.View()
	if !contains(out, m.popup.previewBranch) {
		t.Errorf("popup view should show the preview branch %q:\n%s", m.popup.previewBranch, out)
	}
	if !contains(out, "create") {
		t.Errorf("popup view should show the action hint:\n%s", out)
	}
	if !contains(out, m.popup.startPoint) {
		t.Errorf("popup view should name the start-point branch %q", m.popup.startPoint)
	}
}

var errTest = errTestType("boom")

type errTestType string

func (e errTestType) Error() string { return string(e) }
