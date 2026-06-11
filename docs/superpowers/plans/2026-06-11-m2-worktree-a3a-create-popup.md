# M2 Worktree A3a — Create Popup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A TUI popup to create a worktree from the selected branch: it fills the configured branch template, collects any `<user:LABEL>` inputs, shows a live preview of the resulting branch and path, lets you edit the final name, and creates the worktree on `w`/Enter — bumping `<seq>` counters only on success.

**Architecture:** A self-contained popup model (`internal/tui/worktree_popup.go`) with a three-state machine (INPUT → ACTION → EDIT) that fully intercepts keys while open. It wires together A1's pure `template` resolver and `config` (loaded into the Model) and A2's `engine.CreateWorktree` op (run through the existing async op bridge). The preview is deterministic per popup session: the `<random>`/`<date>` sources are seeded once at open so the preview doesn't jitter as you type and the created branch matches what was shown.

**Tech Stack:** Go 1.26, Bubble Tea + lipgloss, `math/rand/v2`, existing `internal/{template,config,engine,git,tui}`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-06-11-worktree-management-design.md` §7 (popup, live preview, `e`-edit, counter consumption) and §9 (create a new templated branch off the selected branch).

**Explicitly DEFERRED to A3b (do NOT build here):** the `W` create-and-switch re-root, the `--cwd-file`/`gg shell-init` shell integration, and the `gg worktree` CLI. A3a binds only `w` (create, staying in the current worktree). `W` is intentionally left unbound until A3b.

**Conventions (read before starting):**
- TDD red→green. After each task: `go test ./...`, `go vet ./...`, `gofmt -l internal cmd` clean; `-race` for the goroutine/channel paths.
- LF line endings only (`.gitattributes`; Windows-mounted drive — never reintroduce CRLF).
- Commit messages end with a `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` trailer.
- Plain `fmt.Errorf` for errors.
- TUI key handling idiom: the existing decision modal is handled inside `case tea.KeyMsg:` in `internal/tui/model.go` as `if m.modal != nil { … return m, nil }`. The popup mirrors this. `Model` has a value receiver but `m.popup`/`m.modal` are pointers, so mutating the pointed-to struct persists; reassigning `m.popup = nil` works because the method returns `m`.
- Tests: stdlib `testing`, `keyMsg(s)` helper (`internal/tui/model_test.go`) returns specials by name and otherwise `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}`; `loadedModel(t)` builds a loaded Model on a real repo. Construct a backspace key inline as `tea.KeyMsg{Type: tea.KeyBackspace}`.

---

## File Structure

- `internal/git/worktree.go` (modify): add `GitCommonDir` verb.
- `internal/tui/load.go`, `internal/tui/model.go` (modify): load `config.Config` + the common git dir into the Model; add `popup`/`pendingSeqBump` fields; wire the `w` key, the popup key-interception, and seq-bump-on-success.
- `internal/tui/worktree_popup.go` (new): the popup state machine — `worktreePopup` struct, `popupState`, the deterministic preview (`resolveWorktreeNames`, `tctx`, `recompute`), `openWorktreePopup`, `updatePopupKey`, `startCreateFromPopup`, and `renderWorktreePopup`.
- Tests: `internal/git/worktree_verbs_test.go` (extend), `internal/tui/load_test.go` (extend), `internal/tui/worktree_popup_test.go` (new).

---

## Task 1: `git.GitCommonDir` verb

The `<seq>` counters live in the **common** git dir so all worktrees of one repo share them. `--path-format=absolute` guarantees an absolute path (plain `--git-common-dir` can return a relative `.git`).

**Files:** Modify `internal/git/worktree.go`; modify `internal/git/worktree_verbs_test.go`.

- [ ] **Step 1: Write the failing test** — append to `internal/git/worktree_verbs_test.go`

```go
func TestGitCommonDirIsAbsolute(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	got, err := repo.GitCommonDir(context.Background())
	if err != nil {
		t.Fatalf("GitCommonDir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("GitCommonDir = %q, want an absolute path", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/git/ -run TestGitCommonDirIsAbsolute -v`
Expected: FAIL — `repo.GitCommonDir undefined`.

- [ ] **Step 3: Append to `internal/git/worktree.go`**

```go
// GitCommonDir returns the absolute path of the repository's common git
// directory (`git rev-parse --path-format=absolute --git-common-dir`). For a
// linked worktree this is the main repo's .git, so per-repo state (e.g. <seq>
// counters) is shared across all worktrees.
func (r *Repo) GitCommonDir(ctx context.Context) (string, error) {
	argv := gitcmd.New("rev-parse").Arg("--path-format=absolute", "--git-common-dir").ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse (common-dir)", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/git/ -run TestGitCommonDirIsAbsolute -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/git
git add internal/git/worktree.go internal/git/worktree_verbs_test.go
git commit -m "feat(git): add GitCommonDir verb (absolute common git dir)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: Load config + common git dir into the Model

The popup needs the configured templates and the counter location. Load them in the data snapshot.

**Files:** Modify `internal/tui/load.go`, `internal/tui/model.go`; extend `internal/tui/load_test.go`.

- [ ] **Step 1: Write the failing test** — append to `internal/tui/load_test.go`

```go
func TestLoadIncludesConfigAndCommonDir(t *testing.T) {
	m := loadedModel(t)
	if m.cfg.Worktree.DefaultBranchTemplate == "" {
		t.Error("expected a default branch template from config defaults")
	}
	if m.cfg.Worktree.PathTemplate == "" {
		t.Error("expected a default path template from config defaults")
	}
	if m.gitCommonDir == "" {
		t.Error("expected gitCommonDir to be set")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestLoadIncludesConfigAndCommonDir -v`
Expected: FAIL — `m.cfg` / `m.gitCommonDir` undefined.

- [ ] **Step 3a: Extend the Model** — in `internal/tui/model.go`, add two fields to the `Model` struct (after the `worktrees`/`currentWorktree` fields added in A2):

```go
	worktrees       []model.Worktree
	currentWorktree string

	cfg          config.Config
	gitCommonDir string
```

Add the import `"github.com/gigagit/gg/internal/config"` to `internal/tui/model.go`'s import block. (The `popup` and `pendingSeqBump` fields are added later, in Tasks 4 and 7, once their types exist — keeping every commit compilable.)

- [ ] **Step 3b: Store config/commonDir in the dataLoadedMsg handler** — in `internal/tui/model.go`, extend the `if msg.err == nil {` block in `case dataLoadedMsg:`:

```go
		if msg.err == nil {
			m.status = msg.status
			m.branches = msg.branches
			m.commits = msg.commits
			m.worktrees = msg.worktrees
			m.currentWorktree = msg.currentWorktree
			m.cfg = msg.cfg
			m.gitCommonDir = msg.gitCommonDir
		}
```

- [ ] **Step 3c: Extend the message + loader** — in `internal/tui/load.go`, add fields to `dataLoadedMsg`:

```go
	worktrees       []model.Worktree
	currentWorktree string
	cfg             config.Config
	gitCommonDir    string
	err             error
```

(Insert `cfg`/`gitCommonDir` before the existing `err` field; keep the others.) Add imports `"path/filepath"` and `"github.com/gigagit/gg/internal/config"` to `internal/tui/load.go`.

In `loadCmd`, after the `TopLevel` block (which sets `out.currentWorktree`) and before `return out`, add:

```go
		if top, topErr := repo.TopLevel(ctx); topErr == nil {
			out.currentWorktree = top
			// Config: built-in defaults overlaid by the global file then the repo's
			// committed .gg.toml. Load errors are non-fatal — fall back to whatever
			// Load returns (defaults on a missing/again-default config).
			if cfg, cfgErr := config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml")); cfgErr == nil {
				out.cfg = cfg
			} else {
				out.cfg = config.Defaults()
			}
		} else {
			out.cfg = config.Defaults()
		}
		if gcd, gcdErr := repo.GitCommonDir(ctx); gcdErr == nil {
			out.gitCommonDir = gcd
		}
		return out
```

Note: the existing loader already has a `if top, topErr := repo.TopLevel(ctx); topErr == nil { out.currentWorktree = top }` block from A2 — **replace that block** with the expanded version above (which also loads cfg), then add the `GitCommonDir` block and `return out` after it. Do not call `TopLevel` twice.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run TestLoadIncludesConfigAndCommonDir -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/load.go internal/tui/model.go internal/tui/load_test.go
git commit -m "feat(tui): load worktree config and common git dir into the model

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

(This task adds no reference to `worktreePopup`, so the package compiles and the test runs without Task 3. The popup type and the `popup` Model field arrive in Tasks 3 and 4.)

---

## Task 3: Popup struct + deterministic preview resolver (pure core)

The data model and the pure two-phase resolve that powers the live preview. No key handling yet.

**Files:** Create/replace `internal/tui/worktree_popup.go`; create `internal/tui/worktree_popup_test.go`.

- [ ] **Step 1: Write the failing test** — `internal/tui/worktree_popup_test.go`

```go
package tui

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/gigagit/gg/internal/template"
)

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
	// <branch> in the path template resolves to the (sanitized) branch.
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
	// Edit mode: a fixed branch is used verbatim; the path still resolves <branch>.
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestResolveWorktreeNames -v`
Expected: FAIL — `undefined: resolveWorktreeNames`.

- [ ] **Step 3: Write the implementation** — replace `internal/tui/worktree_popup.go` (the placeholder from Task 2) with:

```go
package tui

import (
	"math/rand/v2"
	"path/filepath"
	"time"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/template"
)

// popupState is the worktree-create popup's mode.
type popupState int

const (
	stInput  popupState = iota // collecting <user:LABEL> field values
	stAction                   // preview shown; choose create / edit / cancel
	stEdit                     // free-editing the resolved branch name
)

// worktreePopup holds the in-flight create-worktree dialog. The <random>/<date>
// sources are fixed at open (seed/now) so the preview is stable across keystrokes
// and the created branch matches what was shown.
type worktreePopup struct {
	startPoint string // selected branch = <parent-branch>
	branchTmpl string
	pathTmpl   string
	repoName   string

	labels   []string          // distinct <user:> labels, in order
	inputs   map[string]string // label -> value
	fieldIdx int               // focused label (stInput)

	seqNames []string       // distinct <seq> names referenced by the templates
	seqs     map[string]int // peeked counter values (reused for preview + create)

	seed uint64    // fixes <random> so the preview does not jitter
	now  time.Time // fixes <date>

	state         popupState
	editBuf       string // stEdit working buffer
	branchOverride string // a confirmed hand-edited branch name; "" = use the template

	previewBranch string
	previewPath   string
	previewErr    error
}

// tctx builds a fresh template.Ctx. A new Rand is created from the fixed seed on
// every call, so repeated resolves of the same fields yield identical output.
func (p *worktreePopup) tctx() template.Ctx {
	return template.Ctx{
		ParentBranch: p.startPoint,
		Repo:         p.repoName,
		Seqs:         p.seqs,
		Now:          func() time.Time { return p.now },
		Rand:         rand.New(rand.NewPCG(p.seed, p.seed^0x9e3779b97f4a7c15)),
	}
}

// resolveWorktreeNames resolves the branch then the path. The path template is
// resolved with ctx.Branch set, so A1's "<branch> is path-template-only" rule is
// satisfied. When fixedBranch != "" (edit mode) it is used verbatim as the
// branch instead of resolving branchTmpl.
func resolveWorktreeNames(branchTmpl, pathTmpl, fixedBranch string, inputs map[string]string, ctx template.Ctx) (branch, path string, err error) {
	if fixedBranch != "" {
		branch = fixedBranch
	} else {
		branch, err = template.Resolve(branchTmpl, inputs, ctx)
		if err != nil {
			return "", "", err
		}
	}
	ctx.Branch = branch
	path, err = template.Resolve(pathTmpl, inputs, ctx)
	if err != nil {
		return branch, "", err
	}
	return branch, path, nil
}

// recompute refreshes the preview from the current fields/state. A confirmed
// hand-edit (branchOverride) wins over the template; while actively editing, the
// live editBuf is shown.
func (p *worktreePopup) recompute() {
	fixed := p.branchOverride
	if p.state == stEdit {
		fixed = p.editBuf
	}
	p.previewBranch, p.previewPath, p.previewErr = resolveWorktreeNames(p.branchTmpl, p.pathTmpl, fixed, p.inputs, p.tctx())
}

// distinctAppend appends s to dst if not already present.
func distinctAppend(dst []string, s string) []string {
	for _, x := range dst {
		if x == s {
			return dst
		}
	}
	return append(dst, s)
}

// peekSeqs reads the next value of each named counter (no mutation).
func peekSeqs(gitCommonDir string, names []string) map[string]int {
	out := make(map[string]int, len(names))
	for _, n := range names {
		out[n] = config.PeekSeq(gitCommonDir, n)
	}
	return out
}

// repoNameFrom returns the <repo> token value for a worktree root path.
func repoNameFrom(root string) string {
	return filepath.Base(root)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run TestResolveWorktreeNames -v`
Expected: PASS (all four subtests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/worktree_popup.go internal/tui/worktree_popup_test.go
git commit -m "feat(tui): worktree popup struct and deterministic preview resolver

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: Open the popup on `w` + key interception

Bind `w` to open the popup on the selected branch, and intercept all keys while it is open (so `q`/`s`/`p` etc. don't fire). Esc cancels.

**Files:** Modify `internal/tui/model.go`; modify `internal/tui/worktree_popup.go`; extend `internal/tui/worktree_popup_test.go`.

- [ ] **Step 1: Write the failing test** — append to `internal/tui/worktree_popup_test.go`

```go
import "github.com/gigagit/gg/internal/config" // add to the import block

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
	// No <user:> labels in this template -> straight to ACTION state.
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
	// While the popup is open, a global key like "s" (switch) must NOT start an op.
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run 'TestOpenPopupOnW|TestPopupSwallowsGlobalKeys|TestPopupEscCancels' -v`
Expected: FAIL — `openWorktreePopup`/`updatePopupKey` undefined, and `w` not handled.

- [ ] **Step 3a: Add open + key handlers** — append to `internal/tui/worktree_popup.go`:

```go
// openWorktreePopup builds a popup for the currently-selected branch. Returns
// (model, false) if there is no branch to base it on.
func (m Model) openWorktreePopup() (Model, bool) {
	if len(m.branches) == 0 {
		return m, false
	}
	bt := m.cfg.Worktree.DefaultBranchTemplate
	pt := m.cfg.Worktree.PathTemplate

	var labels []string
	for _, l := range template.UserLabels(bt) {
		labels = distinctAppend(labels, l)
	}
	for _, l := range template.UserLabels(pt) {
		labels = distinctAppend(labels, l)
	}
	var seqNames []string
	for _, n := range template.SeqNames(bt) {
		seqNames = distinctAppend(seqNames, n)
	}
	for _, n := range template.SeqNames(pt) {
		seqNames = distinctAppend(seqNames, n)
	}

	p := &worktreePopup{
		startPoint: m.branches[m.sel[panelBranches]].Name,
		branchTmpl: bt,
		pathTmpl:   pt,
		repoName:   repoNameFrom(m.currentWorktree),
		labels:     labels,
		inputs:     map[string]string{},
		seqNames:   seqNames,
		seqs:       peekSeqs(m.gitCommonDir, seqNames),
		seed:       rand.Uint64(),
		now:        time.Now(),
	}
	for _, l := range labels {
		p.inputs[l] = ""
	}
	if len(labels) > 0 {
		p.state = stInput
	} else {
		p.state = stAction
	}
	p.recompute()
	m.popup = p
	return m, true
}

// updatePopupKey handles one key while the popup is open. It returns the updated
// model; only the create action (Task 7) returns a non-nil command.
func (m Model) updatePopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.popup
	switch p.state {
	case stInput:
		switch msg.Type {
		case tea.KeyEsc:
			m.popup = nil
		case tea.KeyEnter, tea.KeyTab:
			p.fieldIdx++
			if p.fieldIdx >= len(p.labels) {
				p.fieldIdx = len(p.labels) - 1
				p.state = stAction
			}
			p.recompute()
		case tea.KeyBackspace:
			lbl := p.labels[p.fieldIdx]
			if r := []rune(p.inputs[lbl]); len(r) > 0 {
				p.inputs[lbl] = string(r[:len(r)-1])
			}
			p.recompute()
		case tea.KeyRunes:
			p.inputs[p.labels[p.fieldIdx]] += string(msg.Runes)
			p.recompute()
		case tea.KeySpace:
			p.inputs[p.labels[p.fieldIdx]] += " "
			p.recompute()
		}
		return m, nil
	case stEdit:
		switch msg.Type {
		case tea.KeyEnter:
			// Confirm the edit: it sticks as the branch name from now on.
			p.branchOverride = p.editBuf
			p.state = stAction
			p.recompute()
		case tea.KeyEsc:
			// Discard: fall back to the previously-confirmed name (or the template).
			p.state = stAction
			p.recompute()
		case tea.KeyBackspace:
			if r := []rune(p.editBuf); len(r) > 0 {
				p.editBuf = string(r[:len(r)-1])
			}
			p.recompute()
		case tea.KeyRunes:
			p.editBuf += string(msg.Runes)
			p.recompute()
		case tea.KeySpace:
			p.editBuf += " "
			p.recompute()
		}
		return m, nil
	default: // stAction
		switch msg.String() {
		case "esc":
			m.popup = nil
		case "e":
			p.editBuf = p.previewBranch
			p.state = stEdit
			p.recompute()
		case "w", "enter":
			return m.startCreateFromPopup()
		}
		return m, nil
	}
}
```

Add `tea "github.com/charmbracelet/bubbletea"` to `internal/tui/worktree_popup.go`'s import block. The `engine` import is NOT needed yet — the create path is a stub here (Step 3d) and gets its real body (and the `engine` import) in Task 7.

- [ ] **Step 3b: Add the `popup` Model field** — in `internal/tui/model.go`, add one field to the `Model` struct, right after the `cfg`/`gitCommonDir` fields added in Task 2:

```go
	cfg          config.Config
	gitCommonDir string

	popup *worktreePopup
```

- [ ] **Step 3c: Intercept keys + bind `w`** — in `internal/tui/model.go`, inside `case tea.KeyMsg:`, immediately AFTER the existing `if m.modal != nil { … }` block (which ends with `return m, nil`), add the popup interception:

```go
		if m.popup != nil {
			return m.updatePopupKey(msg)
		}
```

Then, in the normal-key `switch msg.String() {` block, add a `w` case (next to the other op keys):

```go
		case "w":
			if !m.running && !m.loading {
				if mm, ok := m.openWorktreePopup(); ok {
					return mm, nil
				}
			}
```

- [ ] **Step 3d: Stubs so the package compiles** — `updatePopupKey`'s ACTION case calls `startCreateFromPopup` (real body in Task 7) and `render` will call `renderWorktreePopup` (real body in Task 8). Add both as stubs at the end of `internal/tui/worktree_popup.go`:

```go
// startCreateFromPopup gets its real body in Task 7.
func (m Model) startCreateFromPopup() (tea.Model, tea.Cmd) {
	return m, nil
}

// renderWorktreePopup gets its real body in Task 8.
func (m Model) renderWorktreePopup() string {
	return "create worktree…\n"
}
```

Then, to avoid a blank screen while the popup is open, add to `render` (top of the method in `internal/tui/view.go`), right after the existing `if m.modal != nil { return m.renderModal() }`:

```go
	if m.popup != nil {
		return m.renderWorktreePopup()
	}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run 'TestOpenPopupOnW|TestPopupSwallowsGlobalKeys|TestPopupEscCancels' -v`
Expected: PASS. Then `go test ./internal/tui/` — all prior tests still pass.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/model.go internal/tui/view.go internal/tui/worktree_popup.go internal/tui/worktree_popup_test.go
git commit -m "feat(tui): open worktree popup on w; intercept keys while open

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: INPUT-state field entry + live preview

Verify the field-collection state machine and that the preview updates as fields change.

**Files:** Extend `internal/tui/worktree_popup_test.go` (logic already implemented in Task 4 — this task adds the tests that lock the behavior, plus any fix needed if they fail).

- [ ] **Step 1: Write the test** — append to `internal/tui/worktree_popup_test.go`

```go
func TestPopupInputFieldsAndPreview(t *testing.T) {
	// Two user fields; popup opens in INPUT state focused on the first.
	m := modelWithConfig(t, "<user:user>/fix/<user:issue>", "wt/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	if m.popup.state != stInput {
		t.Fatalf("state = %v, want stInput with user fields", m.popup.state)
	}
	if len(m.popup.labels) != 2 || m.popup.labels[0] != "user" || m.popup.labels[1] != "issue" {
		t.Fatalf("labels = %v, want [user issue]", m.popup.labels)
	}

	// Type "alice" into the first field.
	for _, ch := range []string{"a", "l", "i", "c", "e"} {
		updated, _ = m.Update(keyMsg(ch))
		m = updated.(Model)
	}
	if m.popup.inputs["user"] != "alice" {
		t.Fatalf("first field = %q, want alice", m.popup.inputs["user"])
	}

	// Backspace removes one rune.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)
	if m.popup.inputs["user"] != "alic" {
		t.Fatalf("after backspace = %q, want alic", m.popup.inputs["user"])
	}

	// Tab advances to the second field; fill it, Tab again -> ACTION.
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
	// Preview reflects both fields.
	if m.popup.previewBranch != "alic/fix/77" {
		t.Fatalf("preview branch = %q, want alic/fix/77", m.popup.previewBranch)
	}
}

func TestPopupBackspaceOnEmptyField(t *testing.T) {
	m := modelWithConfig(t, "issue/<user:id>", "wt/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	// Backspace on an empty field is a no-op (no panic, stays empty).
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
```

Add `tea "github.com/charmbracelet/bubbletea"` to the test file's import block.

- [ ] **Step 2: Run**

Run: `go test ./internal/tui/ -run 'TestPopupInputFieldsAndPreview|TestPopupBackspaceOnEmptyField|TestPopupMultiByteRune' -v`
Expected: PASS (the logic was implemented in Task 4). If any fail, fix the corresponding handler in `updatePopupKey`/`recompute` — do not weaken the test.

- [ ] **Step 3: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/worktree_popup_test.go
git commit -m "test(tui): popup INPUT-state field entry and live preview

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: EDIT-state free-editing of the branch name

`e` enters EDIT with the current resolved branch prefilled; typing edits it; Enter confirms (preview path follows the edited branch); Esc discards back to the previewed name.

**Files:** Extend `internal/tui/worktree_popup_test.go` (logic implemented in Task 4).

- [ ] **Step 1: Write the test** — append to `internal/tui/worktree_popup_test.go`

```go
func TestPopupEditMode(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	if m.popup.previewBranch != "b/auto" {
		t.Fatalf("preview branch = %q, want b/auto", m.popup.previewBranch)
	}

	// Enter edit mode: editBuf prefilled with the current branch.
	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	if m.popup.state != stEdit {
		t.Fatalf("state = %v, want stEdit", m.popup.state)
	}
	if m.popup.editBuf != "b/auto" {
		t.Fatalf("editBuf = %q, want b/auto", m.popup.editBuf)
	}

	// Replace the buffer with a hand-typed name.
	for len([]rune(m.popup.editBuf)) > 0 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(Model)
	}
	for _, ch := range []string{"m", "y", "/", "b"} {
		updated, _ = m.Update(keyMsg(ch))
		m = updated.(Model)
	}
	// Confirm with Enter -> back to ACTION; preview path uses the edited branch.
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.popup.state != stAction {
		t.Fatalf("state = %v, want stAction after enter", m.popup.state)
	}
	if m.popup.previewBranch != "my/b" {
		t.Fatalf("preview branch = %q, want my/b", m.popup.previewBranch)
	}
	if m.popup.previewPath != "../aaa.worktrees/my-b" {
		// repoName comes from the real repo dir, so only assert the <branch> part.
		if !contains(m.popup.previewPath, "my-b") {
			t.Fatalf("preview path = %q, want it to contain my-b", m.popup.previewPath)
		}
	}
}

func TestPopupEditEscDiscards(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("z")) // type into the edit buffer
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.popup.state != stAction {
		t.Fatalf("state = %v, want stAction after esc", m.popup.state)
	}
	// Discarded edit: preview falls back to the resolved template branch.
	if m.popup.previewBranch != "b/auto" {
		t.Fatalf("preview branch = %q, want b/auto after discard", m.popup.previewBranch)
	}
}
```

Add a tiny `contains` string helper to the test file (substring check) if not already present:
```go
func contains(s, sub string) bool { return strings.Contains(s, sub) }
```
(Add `"strings"` to the test imports. If `contains` already exists in the package's test files, skip redefining it.)

- [ ] **Step 2: Run**

Run: `go test ./internal/tui/ -run 'TestPopupEditMode|TestPopupEditEscDiscards' -v`
Expected: PASS. If a test fails, fix `updatePopupKey`/`recompute` for the EDIT transitions — do not weaken the test.

- [ ] **Step 3: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/worktree_popup_test.go
git commit -m "test(tui): popup EDIT-state name editing and discard

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: Create action + bump `<seq>` counters on success

`w`/Enter in ACTION launches `engine.CreateWorktree` via the op bridge; a preview error blocks it; on success the referenced counters bump once each.

**Files:** Modify `internal/tui/worktree_popup.go` (add `startCreateFromPopup`); modify `internal/tui/model.go` (seq-bump in `opFinishedMsg`); extend `internal/tui/worktree_popup_test.go`.

- [ ] **Step 1: Write the failing test** — append to `internal/tui/worktree_popup_test.go`

```go
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
	// An unknown token makes the preview error; create must refuse to launch.
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
	// Directly exercise the success path's counter bump.
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

var errTest = errTestType("boom")

type errTestType string

func (e errTestType) Error() string { return string(e) }
```

Add `"github.com/gigagit/gg/internal/engine"` to the test imports if not present.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run 'TestPopupCreate|TestSeqBump|TestSeqNoBump' -v`
Expected: FAIL — `startCreateFromPopup` undefined and the seq-bump not wired in `opFinishedMsg`.

- [ ] **Step 3a: Add the `pendingSeqBump` field** — in `internal/tui/model.go`, add one field to the `Model` struct, right after the `popup *worktreePopup` field (added in Task 4):

```go
	popup          *worktreePopup
	pendingSeqBump []string
```

- [ ] **Step 3b: Replace the `startCreateFromPopup` stub** — in `internal/tui/worktree_popup.go`, replace the Task 4 stub:

```go
// startCreateFromPopup gets its real body in Task 7.
func (m Model) startCreateFromPopup() (tea.Model, tea.Cmd) {
	return m, nil
}
```

with the real implementation:

```go
// startCreateFromPopup launches the CreateWorktree op for the previewed names,
// closes the popup, and records which <seq> counters to bump on success. A
// preview error refuses to launch.
func (m Model) startCreateFromPopup() (tea.Model, tea.Cmd) {
	p := m.popup
	if p.previewErr != nil {
		m.statusMsg = "cannot create: " + p.previewErr.Error()
		return m, nil
	}
	op := engine.CreateWorktree{
		StartPoint: p.startPoint,
		Branch:     p.previewBranch,
		Path:       p.previewPath,
	}
	m.pendingSeqBump = p.seqNames
	m.popup = nil
	return m.startOp(op)
}
```

Add `"github.com/gigagit/gg/internal/engine"` to `internal/tui/worktree_popup.go`'s import block.

- [ ] **Step 3c: Bump counters on success** — in `internal/tui/model.go`, replace the `case opFinishedMsg:` body with:

```go
	case opFinishedMsg:
		m.running = false
		m.opMsgs = nil
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
		} else {
			if msg.res.Summary != "" {
				m.statusMsg = msg.res.Summary
			}
			// A successful create consumes the <seq> counters its template used.
			for _, name := range m.pendingSeqBump {
				_, _ = config.BumpSeq(m.gitCommonDir, name)
			}
		}
		m.pendingSeqBump = nil
		return m, m.loadCmd()
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run 'TestPopupCreate|TestSeqBump|TestSeqNoBump' -v`
Expected: PASS. Then `go test ./internal/tui/` and `go test -race ./internal/tui/` — all pass.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/worktree_popup.go internal/tui/model.go internal/tui/worktree_popup_test.go
git commit -m "feat(tui): create worktree from popup; bump <seq> counters on success

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8: Render the popup + footer hint

Replace the render stub with the real popup view, and advertise `w` in the footer.

**Files:** Modify `internal/tui/worktree_popup.go` (real `renderWorktreePopup`); modify `internal/tui/view.go` (footer); extend `internal/tui/worktree_popup_test.go`.

- [ ] **Step 1: Write the failing test** — append to `internal/tui/worktree_popup_test.go`

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestRenderWorktreePopupShowsPreview -v`
Expected: FAIL — the stub renders only "create worktree…", missing the preview branch / start-point.

- [ ] **Step 3a: Real render** — in `internal/tui/worktree_popup.go`, replace the stub `renderWorktreePopup` with:

```go
// renderWorktreePopup draws the create-worktree dialog (fields, live preview,
// and state-specific key hints).
func (m Model) renderWorktreePopup() string {
	p := m.popup
	var b strings.Builder
	b.WriteString("Create worktree from " + p.startPoint + "\n\n")

	for i, lbl := range p.labels {
		cursor := "  "
		if p.state == stInput && i == p.fieldIdx {
			cursor = "> "
		}
		b.WriteString(cursor + lbl + ": " + p.inputs[lbl] + "\n")
	}
	if len(p.labels) > 0 {
		b.WriteString("\n")
	}

	branch := p.previewBranch
	if p.state == stEdit {
		branch = p.editBuf
	}
	b.WriteString("branch: " + branch + "\n")
	b.WriteString("path:   " + p.previewPath + "\n")
	if p.previewErr != nil {
		b.WriteString("\n⚠ " + p.previewErr.Error() + "\n")
	}

	b.WriteString("\n")
	switch p.state {
	case stInput:
		b.WriteString("[type] value  [tab/enter] next field  [esc] cancel")
	case stEdit:
		b.WriteString("[type] edit name  [enter] done  [esc] discard")
	default:
		b.WriteString("[w/enter] create  [e] edit name  [esc] cancel")
	}
	return modalStyle.Render(b.String()) + "\n"
}
```

Add `"strings"` to `internal/tui/worktree_popup.go`'s import block. (`modalStyle` is the existing shared lipgloss style in `view.go`.)

- [ ] **Step 3b: Footer hint** — in `internal/tui/view.go`, in `render`, add `[w]orktree` to the footer string. Change:

```go
	footer := truncate("[p]ull [P]ush [s]witch [S]tash [u]ndo  •  [tab] focus  [r] reload  [q] quit", w)
```
to:
```go
	footer := truncate("[p]ull [P]ush [s]witch [S]tash [u]ndo [w]orktree  •  [tab] focus  [r] reload  [q] quit", w)
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run TestRenderWorktreePopupShowsPreview -v`
Expected: PASS. Then `go test ./internal/tui/` — all pass (existing footer/keys tests unaffected; if a test asserted the exact old footer string, update it to match — search `internal/tui/*_test.go` for the footer text).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/worktree_popup.go internal/tui/view.go internal/tui/worktree_popup_test.go
git commit -m "feat(tui): render the worktree create popup; add footer hint

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 9: Full-package verification

**Files:** none (verification only)

- [ ] **Step 1: Full suite** — `go test ./...` — Expected: all PASS.
- [ ] **Step 2: Race (popup launches the async op bridge + seq bump)** — `go test -race ./internal/tui/ ./internal/engine/` — Expected: PASS, no races.
- [ ] **Step 3: Vet** — `go vet ./...` — Expected: no output.
- [ ] **Step 4: Format** — `gofmt -l internal cmd` — Expected: empty. If anything is listed, `gofmt -w internal cmd` and amend.
- [ ] **Step 5: Manual smoke (optional, document result)** — `go build ./...` then note that `gg` launches; pressing `w` opens the popup on the selected branch. (No automated TTY test.)

No commit needed if everything is already committed.

---

## Self-Review Notes (plan author)

- **Spec coverage:** §7 popup on selected branch (`w`), default branch template filled → Tasks 4,3; one input field per `<user:LABEL>` → Tasks 4,5; live preview of branch+path → Tasks 3,5,8; Enter create-as-is + `w` create → Task 7; `e` edit final name → Tasks 4,6; Esc cancel → Task 4; counter consumption (PeekSeq for preview, BumpSeq once per counter only after success) → Tasks 3,7; §9 new templated branch off the selected start-point → Task 4 (`startPoint = selected branch`), Task 7 (`CreateWorktree{StartPoint,…}`).
- **Determinism:** `<random>`/`<date>` fixed at open via `seed`/`now`, so the preview is stable across keystrokes and the created branch equals the preview (Task 3 `tctx`). Two-phase resolve enforces A1's `<branch>`-path-only rule (Task 3).
- **Deferred to A3b (correctly):** `W` create-and-switch re-root, `--cwd-file`/`gg shell-init`, `gg worktree` CLI. A3a leaves `W` unbound and stays in the current worktree.
- **Key-collision handling:** the three-state machine (INPUT/ACTION/EDIT) means `w`/`e` are actions only in ACTION; in INPUT/EDIT they are text. The popup fully intercepts keys (`if m.popup != nil { return m.updatePopupKey(msg) }`), tested by `TestPopupSwallowsGlobalKeys`.
- **Type consistency:** `worktreePopup`/`popupState`/`stInput`/`stAction`/`stEdit`, `resolveWorktreeNames`, `openWorktreePopup`, `updatePopupKey`, `startCreateFromPopup`, `renderWorktreePopup`, and Model fields `cfg`/`gitCommonDir`/`popup`/`pendingSeqBump` are used consistently across Tasks 2–8. `engine.CreateWorktree{StartPoint,Branch,Path}` matches A2.
