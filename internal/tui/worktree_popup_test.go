package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
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

func TestPopupSwallowsGlobalKeys(t *testing.T) {
	m := modelWithConfig(t, "b/x", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("s"))
	m = updated.(Model)
	if m.running {
		t.Error("global keys must not fire while the popup is open")
	}
	if layerOf[*worktreePopup](m) == nil {
		t.Error("popup should still be open after an inert key")
	}
}

func TestPopupEscCancels(t *testing.T) {
	m := modelWithConfig(t, "b/x", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("esc"))
	if layerOf[*worktreePopup](updated.(Model)) != nil {
		t.Error("esc should cancel the popup")
	}
}

func TestPopupInputFieldsAndPreview(t *testing.T) {
	m := modelWithConfig(t, "b/x", "wt/<user:user>-<user:issue>/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	p := layerOf[*worktreePopup](m)
	if p.state != stInput {
		t.Fatalf("state = %v, want stInput with user fields", p.state)
	}
	if len(p.labels) != 2 || p.labels[0] != "user" || p.labels[1] != "issue" {
		t.Fatalf("labels = %v, want [user issue]", p.labels)
	}

	for _, ch := range []string{"a", "l", "i", "c", "e"} {
		updated, _ = m.Update(keyMsg(ch))
		m = updated.(Model)
	}
	if layerOf[*worktreePopup](m).inputs["user"].Value() != "alice" {
		t.Fatalf("first field = %q, want alice", layerOf[*worktreePopup](m).inputs["user"].Value())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)
	if layerOf[*worktreePopup](m).inputs["user"].Value() != "alic" {
		t.Fatalf("after backspace = %q, want alic", layerOf[*worktreePopup](m).inputs["user"].Value())
	}

	updated, _ = m.Update(keyMsg("tab"))
	m = updated.(Model)
	if layerOf[*worktreePopup](m).fieldIdx != 1 {
		t.Fatalf("fieldIdx = %d, want 1 after tab", layerOf[*worktreePopup](m).fieldIdx)
	}
	for _, ch := range []string{"7", "7"} {
		updated, _ = m.Update(keyMsg(ch))
		m = updated.(Model)
	}
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if layerOf[*worktreePopup](m).state != stAction {
		t.Fatalf("state = %v, want stAction after last field", layerOf[*worktreePopup](m).state)
	}
	if got := layerOf[*worktreePopup](m).previewPath; !contains(got, "alic-77") {
		t.Fatalf("preview path = %q, want the field values alic-77 in it", got)
	}
	if got, want := layerOf[*worktreePopup](m).previewBranch, layerOf[*worktreePopup](m).startPoint; got != want {
		t.Fatalf("preview branch = %q, want the selection %q (branch is never templated)", got, want)
	}
}

func TestPopupBackspaceOnEmptyField(t *testing.T) {
	m := modelWithConfig(t, "b/x", "wt/<user:id>-<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)
	if layerOf[*worktreePopup](m).inputs["id"].Value() != "" {
		t.Fatalf("field = %q, want empty", layerOf[*worktreePopup](m).inputs["id"].Value())
	}
}

func TestPopupMultiByteRune(t *testing.T) {
	m := modelWithConfig(t, "b/x", "wt/<user:id>-<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("é")})
	m = updated.(Model)
	if layerOf[*worktreePopup](m).inputs["id"].Value() != "é" {
		t.Fatalf("field = %q, want é", layerOf[*worktreePopup](m).inputs["id"].Value())
	}
}

func TestPopupEditMode(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	start := layerOf[*worktreePopup](m).startPoint
	if layerOf[*worktreePopup](m).previewBranch != start {
		t.Fatalf("preview branch = %q, want the selection %q", layerOf[*worktreePopup](m).previewBranch, start)
	}

	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	if layerOf[*worktreePopup](m).state != stEdit {
		t.Fatalf("state = %v, want stEdit", layerOf[*worktreePopup](m).state)
	}
	if layerOf[*worktreePopup](m).editBuf.Value() != start {
		t.Fatalf("editBuf = %q, want the selection %q", layerOf[*worktreePopup](m).editBuf.Value(), start)
	}

	for len([]rune(layerOf[*worktreePopup](m).editBuf.Value())) > 0 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(Model)
	}
	for _, ch := range []string{"m", "y", "/", "b"} {
		updated, _ = m.Update(keyMsg(ch))
		m = updated.(Model)
	}
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if layerOf[*worktreePopup](m).state != stAction {
		t.Fatalf("state = %v, want stAction after enter", layerOf[*worktreePopup](m).state)
	}
	if layerOf[*worktreePopup](m).previewBranch != "my/b" {
		t.Fatalf("preview branch = %q, want my/b", layerOf[*worktreePopup](m).previewBranch)
	}
	if !contains(layerOf[*worktreePopup](m).previewPath, "my-b") {
		t.Fatalf("preview path = %q, want it to contain my-b", layerOf[*worktreePopup](m).previewPath)
	}
}

func TestPopupEditEscDiscards(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	start := layerOf[*worktreePopup](m).startPoint
	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("z"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if layerOf[*worktreePopup](m).state != stAction {
		t.Fatalf("state = %v, want stAction after esc", layerOf[*worktreePopup](m).state)
	}
	if layerOf[*worktreePopup](m).previewBranch != start {
		t.Fatalf("preview branch = %q, want the selection %q after discard", layerOf[*worktreePopup](m).previewBranch, start)
	}
}

func TestPopupCreateLaunchesOpAndClearsPopup(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, cmd := m.Update(keyMsg("w")) // create
	m = updated.(Model)
	if layerOf[*worktreePopup](m) != nil {
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
	m := modelWithConfig(t, "b/x", "wt-<bogus>/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	if layerOf[*worktreePopup](m).previewErr == nil {
		t.Fatal("expected a preview error for the bad path template")
	}
	updated, _ = m.Update(keyMsg("w")) // attempt create
	m = updated.(Model)
	if m.running {
		t.Error("create must not launch when the preview has an error")
	}
	if layerOf[*worktreePopup](m) == nil {
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

	p := layerOf[*worktreePopup](m)
	out := m.View()
	if !contains(out, p.previewBranch) {
		t.Errorf("popup view should show the preview branch %q:\n%s", p.previewBranch, out)
	}
	if !contains(out, "create") {
		t.Errorf("popup view should show the action hint:\n%s", out)
	}
	if !contains(out, p.startPoint) {
		t.Errorf("popup view should name the start-point branch %q", p.startPoint)
	}
}

func TestPopupCreateAndSwitchSetsPendingSwitch(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("W")) // create AND switch
	m = updated.(Model)
	if layerOf[*worktreePopup](m) != nil {
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
	m := New(domain.New(repo))
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
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	p := layerOf[*worktreePopup](m)
	op := p.createOp("").(engine.CreateWorktreeForBranch)
	if op.Branch != p.previewBranch || op.Path != p.previewPath {
		t.Fatalf("op {%q,%q} != preview {%q,%q}", op.Branch, op.Path, p.previewBranch, p.previewPath)
	}

	// After a confirmed edit, the op cuts a NEW branch carrying the edited name.
	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	for len([]rune(layerOf[*worktreePopup](m).editBuf.Value())) > 0 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(Model)
	}
	for _, ch := range []string{"h", "f"} {
		updated, _ = m.Update(keyMsg(ch))
		m = updated.(Model)
	}
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	edited := layerOf[*worktreePopup](m).createOp("").(engine.CreateWorktree)
	if edited.Branch != "hf" || edited.Branch != layerOf[*worktreePopup](m).previewBranch {
		t.Fatalf("edited op.Branch = %q, want hf (== preview %q)", edited.Branch, layerOf[*worktreePopup](m).previewBranch)
	}
	if edited.StartPoint != layerOf[*worktreePopup](m).startPoint {
		t.Fatalf("edited op.StartPoint = %q, want the selection %q", edited.StartPoint, layerOf[*worktreePopup](m).startPoint)
	}
}

// The branch name is never templated in the w/W popup, so a branch template's
// <seq> is never consumed — only the path template's, before and after a rename.
func TestConsumedSeqNamesAfterEdit(t *testing.T) {
	m := modelWithConfig(t, "issue/<seq:issue>", "../<repo>.worktrees/<seq:wt>-<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	if got := layerOf[*worktreePopup](m).consumedSeqNames(); len(got) != 1 || got[0] != "wt" {
		t.Fatalf("pre-edit consumedSeqNames = %v, want [wt] (branch template never runs)", got)
	}
	// Renaming into a new branch changes nothing: still only the path's <seq>.
	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("x"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if got := layerOf[*worktreePopup](m).consumedSeqNames(); len(got) != 1 || got[0] != "wt" {
		t.Fatalf("post-edit consumedSeqNames = %v, want [wt]", got)
	}
}

// The popup is composited centered over the interface: panels remain visible
// behind it, the box is horizontally centered (not flush-left), and a long
// branch/path never pushes any line past the terminal width.
func TestPopupOverlaysInterfaceCenteredAndFits(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 80, 24
	m = m.pushLayer(&worktreePopup{
		startPoint:    "main",
		state:         stAction,
		previewBranch: "feature/some-longish-branch-name",
		previewPath:   "/home/user/projects/acme-monorepo.worktrees/feature-some-longish-branch-name",
	})
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

// End-to-end: W on a non-current branch opens the existing-mode popup, and
// enter (the switch default) runs a real CreateWorktreeForBranch op leaving
// the model pointed at the worktree that was actually created (closing the
// seam between the popup, the engine op, and reRoot).
func TestPopupCreateAndSwitchEndToEnd(t *testing.T) {
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "feature/sw2")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.cfg.Worktree.PathTemplate = "../<repo>.worktrees/<branch>"
	m.focus = panelBranches
	for vi := 0; vi < m.panelLen(panelBranches); vi++ {
		m.sel[panelBranches] = vi
		if bi, ok := m.backingIndex(panelBranches); ok && m.branches[bi].Name == "feature/sw2" {
			break
		}
	}

	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, cmd := m.Update(keyMsg("enter")) // default = create AND switch
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

// TestWorktreeFromCommitCreateOpUsesTypedBranch proves the end-to-end payload:
// after the user types a branch name in commit mode, createOp emits a
// CreateWorktree based at the commit with the TYPED (not templated) branch.
func TestWorktreeFromCommitCreateOpUsesTypedBranch(t *testing.T) {
	var m Model
	full := "cccccccccccccccccccccccccccccccccccccccc"
	p := &worktreePopup{
		startPoint: full,
		fromCommit: true,
		branchTmpl: "b/from-<parent-branch>", // must be bypassed by the typed name
		pathTmpl:   "../<repo>.worktrees/<branch>",
		repoName:   "myrepo",
		inputs:     map[string]textfield{},
		state:      stEdit,
	}
	for _, ch := range "feat-x" {
		p.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // confirm: stEdit → stAction
	op, ok := p.createOp("").(engine.CreateWorktree)
	if !ok {
		t.Fatalf("op = %T, want engine.CreateWorktree", p.createOp(""))
	}
	if op.StartPoint != full {
		t.Fatalf("StartPoint = %q, want the full commit hash", op.StartPoint)
	}
	if op.Branch != "feat-x" {
		t.Fatalf("Branch = %q, want the typed name 'feat-x' (template bypassed)", op.Branch)
	}
	if op.Path != "../myrepo.worktrees/feat-x" {
		t.Fatalf("Path = %q, want it resolved from the typed branch", op.Path)
	}
}

func TestWorktreeCreateOpCarriesHookWhenEnabled(t *testing.T) {
	p := &worktreePopup{previewBranch: "b/x", previewPath: "/tmp/x", runHook: true}
	op := p.createOp("echo hi")
	cw, ok := op.(engine.CreateWorktree)
	if !ok {
		t.Fatalf("op type = %T", op)
	}
	if cw.PostCreateHook != "echo hi" {
		t.Fatalf("PostCreateHook = %q, want 'echo hi'", cw.PostCreateHook)
	}
}

func TestWorktreeCreateOpOmitsHookWhenDisabled(t *testing.T) {
	p := &worktreePopup{startPoint: "b/x", previewBranch: "b/x", previewPath: "/tmp/x", existing: true, runHook: false}
	op := p.createOp("") // startCreateFromPopup passes "" when runHook is false
	cwb := op.(engine.CreateWorktreeForBranch)
	if cwb.PostCreateHook != "" {
		t.Fatalf("PostCreateHook = %q, want empty", cwb.PostCreateHook)
	}
}

func TestWorktreeHKeyTogglesHook(t *testing.T) {
	m := Model{}
	m.cfg.Worktree.PostCreateHook = "echo hi"
	p := &worktreePopup{state: stAction, runHook: true}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if p.runHook {
		t.Fatal("h should toggle runHook off")
	}
}
