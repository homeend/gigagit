package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/engine"
)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func modelWithConfig(t *testing.T, branchTmpl, pathTmpl string) Model {
	t.Helper()
	m := loadedModel(t)
	m.cfg = config.Config{Worktree: config.WorktreeConfig{
		DefaultBranchTemplate: branchTmpl,
		PathTemplate:          pathTmpl,
	}}
	return m
}

func TestOpenNewBranchPopupOnShiftW(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
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
	updated, _ := m.Update(keyMsg("W"))
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
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("esc"))
	if updated.(Model).popup != nil {
		t.Error("esc should cancel the popup")
	}
}

func TestPopupInputFieldsAndPreview(t *testing.T) {
	m := modelWithConfig(t, "<user:user>/fix/<user:issue>", "wt/<branch>")
	updated, _ := m.Update(keyMsg("W"))
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
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)
	if m.popup.inputs["id"] != "" {
		t.Fatalf("field = %q, want empty", m.popup.inputs["id"])
	}
}

func TestPopupMultiByteRune(t *testing.T) {
	m := modelWithConfig(t, "issue/<user:id>", "wt/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("é")})
	m = updated.(Model)
	if m.popup.inputs["id"] != "é" {
		t.Fatalf("field = %q, want é", m.popup.inputs["id"])
	}
}

func TestPopupEditMode(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
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
	updated, _ := m.Update(keyMsg("W"))
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
	updated, _ := m.Update(keyMsg("W"))
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
	updated, _ := m.Update(keyMsg("W"))
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
	updated, _ := m.Update(keyMsg("W"))
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

func TestPopupCreateAndSwitchSetsPendingSwitch(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("W")) // create AND switch
	m = updated.(Model)
	if m.popup != nil {
		t.Error("popup should close on create-and-switch")
	}
	if !m.running {
		t.Error("create-and-switch should start the op")
	}
	if !m.pendingSwitch {
		t.Error("W should mark pendingSwitch so the model re-roots on success")
	}
}

func TestPlainCreateDoesNotSwitch(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("w")) // plain create
	m = updated.(Model)
	if m.pendingSwitch {
		t.Error("plain create (w) must not set pendingSwitch")
	}
}

func TestOpFinishedSwitchesOnPendingSwitch(t *testing.T) {
	dir, repo := newRepoDir(t)
	m := New(repo)
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)

	// Pre-create a worktree so reRoot has a real target.
	wt := filepath.Join(filepath.Dir(dir), "wt-sw")
	runGit(t, dir, "worktree", "add", "-b", "feature/sw", wt, "main")

	m.pendingSwitch = true
	updated, cmd := m.Update(opFinishedMsg{res: engine.Result{Summary: "created", Changed: true, Path: wt}})
	m = updated.(Model)
	if m.switchTarget != wt {
		t.Fatalf("switchTarget = %q, want %q (should re-root to Result.Path)", m.switchTarget, wt)
	}
	if m.pendingSwitch {
		t.Error("pendingSwitch should be cleared after handling")
	}
	if cmd == nil {
		t.Fatal("expected a reload command from the switch")
	}
}

var errTest = errTestType("boom")

type errTestType string

func (e errTestType) Error() string { return string(e) }

// The created op must carry exactly the previewed (already-resolved) names, so
// the worktree equals what was shown — including after a hand-edit.
func TestCreateOpEqualsPreview(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	op := m.popup.createOp().(engine.CreateWorktree)
	if op.Branch != m.popup.previewBranch || op.Path != m.popup.previewPath {
		t.Fatalf("op {%q,%q} != preview {%q,%q}", op.Branch, op.Path, m.popup.previewBranch, m.popup.previewPath)
	}
	if op.StartPoint != m.popup.startPoint {
		t.Fatalf("op.StartPoint = %q, want %q", op.StartPoint, m.popup.startPoint)
	}

	// After a confirmed edit, the op carries the edited branch.
	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	for len([]rune(m.popup.editBuf)) > 0 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(Model)
	}
	for _, ch := range []string{"h", "f"} {
		updated, _ = m.Update(keyMsg(ch))
		m = updated.(Model)
	}
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if op := m.popup.createOp().(engine.CreateWorktree); op.Branch != "hf" || op.Branch != m.popup.previewBranch {
		t.Fatalf("edited op.Branch = %q, want hf (== preview %q)", op.Branch, m.popup.previewBranch)
	}
}

// Hand-editing the branch away from its <seq> must NOT bump the branch counter;
// only the path template's <seq> (if any) is consumed.
func TestConsumedSeqNamesAfterEdit(t *testing.T) {
	m := modelWithConfig(t, "issue/<seq:issue>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	// Before any edit: the branch <seq:issue> is consumed.
	if got := m.popup.consumedSeqNames(); len(got) != 1 || got[0] != "issue" {
		t.Fatalf("pre-edit consumedSeqNames = %v, want [issue]", got)
	}
	// Hand-edit the branch (override); path template has no <seq>, so nothing is consumed.
	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("x"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if got := m.popup.consumedSeqNames(); len(got) != 0 {
		t.Fatalf("post-edit consumedSeqNames = %v, want [] (branch <seq> no longer used)", got)
	}
}

// The popup is composited centered over the interface: panels remain visible
// behind it, the box is horizontally centered (not flush-left), and a long
// branch/path never pushes any line past the terminal width.
func TestPopupOverlaysInterfaceCenteredAndFits(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 80, 24
	m.popup = &worktreePopup{
		startPoint:    "main",
		state:         stAction,
		previewBranch: "feature/some-longish-branch-name",
		previewPath:   "/home/user/projects/acme-monorepo.worktrees/feature-some-longish-branch-name",
	}
	out := m.View()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	// Fit invariant: nothing exceeds the terminal width.
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d width %d exceeds terminal %d: %q", i, w, m.width, ln)
		}
	}
	// Interface visible behind the popup.
	if !strings.Contains(out, "Commits") {
		t.Error("expected the interface (Commits panel) visible behind the popup")
	}
	// Popup present and horizontally centered (its top border is indented).
	var boxLine string
	for _, ln := range lines {
		if strings.Contains(ln, "╔") {
			boxLine = ln
			break
		}
	}
	if boxLine == "" {
		t.Fatal("expected the popup's double-border box in the output")
	}
	// Centered: the box's top-left corner is not at column 0 (interface shows to
	// its left), and there is interface to its right too.
	idx := strings.Index(boxLine, "╔")
	if idx <= 0 {
		t.Errorf("popup box should be centered (indented), got: %q", boxLine)
	}
	if !strings.Contains(boxLine[strings.Index(boxLine, "╗"):], "│") {
		t.Errorf("expected interface visible to the right of the centered popup: %q", boxLine)
	}
}

// End-to-end: pressing W runs a real CreateWorktree op and the model ends up
// rooted in the worktree that was actually created (closing the seam between
// the popup, the engine op, and reRoot).
func TestPopupCreateAndSwitchEndToEnd(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, cmd := m.Update(keyMsg("W")) // create AND switch
	m = updated.(Model)

	m = driveOp(t, m, cmd) // run the real op to completion

	if m.switchTarget == "" {
		t.Fatal("expected switchTarget set to the created worktree")
	}
	// The worktree was really created and switchTarget points at it.
	if _, err := os.Stat(filepath.Join(m.switchTarget, "README.md")); err != nil {
		t.Fatalf("created worktree not at switchTarget %q: %v", m.switchTarget, err)
	}
}
