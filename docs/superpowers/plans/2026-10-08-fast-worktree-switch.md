# Fast Worktree Switch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo's CLAUDE.md forbids implementer subagents; the one session that wrote the plan executes it). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A switch between worktrees of one repository becomes an instant swap of remembered per-worktree state, triggered by a shown agent/terminal console (look only) or by the user's own switch (look + gg's identity moves).

**Architecture:** The worktree-scoped part of the TUI `Model` (service, Status panel, cursors, open files, working reviews, watcher) is grouped into a `worktreeView` slot kept per worktree path in `Model.views`; the fields stay on the Model as the live copy and `switchView(path)` copies out/in. `showConsole`/`closeConsole` drive the look trigger through the console's return point; `guardedReRoot` takes a fast path for same-repo targets and `adoptView` moves the exit directory, snapshot file, steering inbox, session-registry worktree and web host. Other-repo targets keep `reRoot`.

**Tech Stack:** Go 1.26, Bubble Tea, the repo's own `internal/domain` + `internal/tui` test helpers (`loadedModel`, `startTestSession`, `gittest`), real `git` in `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-10-08-fast-worktree-switch-design.md`

## Global Constraints

- Work only in the worktree `/work/gigagit/.claude/worktrees/fast-worktree-switch` (branch `feat/fast-worktree-switch`); every command `cd`s there or uses `git -C`.
- `internal/tui` never imports `internal/git`; reach git through `internal/domain` (archtest).
- Every user-visible TUI string goes through `i18n.T` with a literal key present in all four bundles `internal/i18n/lang/{ja,ko,zh,ru}.toml` (AST-gate tests fail otherwise).
- Frontends run operations via `domain.Execute` only; no new git verbs here.
- Same repository only: a target outside `m.worktrees` is never a slot.
- Only the live slot is watched or refreshed; hidden slots cost memory only.
- Commit messages end with the attribution lines from the session reminder; `gg add` then `git commit -F <file>` (gg commit has no `-F`).
- Run `./test.sh unit` from the worktree before each commit of a task; `./test.sh race` before merge.

## Review Focus

1. A status read launched for worktree A lands after the user swapped to B: it must be dropped, never written into B's Status panel (Task 2 pins it with `TestSwitchViewDropsAStaleStatusRead`).
2. A worktree removed (`git worktree remove`) while it is the viewed one: the view must fall back to home and the slot must go, without a nil `svc` on screen (Task 3, `TestWorktreesReloadDropsAGoneSlot`).
3. An operation running while a console is shown or closed: the swap must be refused rather than swapping the service under the op's result handler (Task 4, `TestConsoleShowRefusesTheSwapWhileAnOpRuns`).
4. A `reRoot` to another repository while a slot other than home is viewed: the map must be dropped and the console's return view cleared, so a later close does not swap to a path of the old repo (Task 5, `TestRepoSwitchDropsTheSlots`).
5. The hosted web page while a console views A: the page keeps following gg's OWN worktree (home), not the viewed one, until the user adopts (Task 6, `TestAdoptViewRerootsTheWebPageLookDoesNot`).

---

### Task 1: The `worktreeView` slot and its save/load on the Model

**Files:**
- Create: `internal/tui/worktree_view.go`
- Create: `internal/tui/worktree_view_test.go`
- Modify: `internal/tui/model.go` (fields near line 92 `currentWorktree`; the `dataLoadedMsg` success arm near line 1818; `reRoot` near line 4928)

**Interfaces:**
- Produces:
  - `type worktreeView struct` (below)
  - `func (m Model) saveView() Model` — copies the live worktree-scoped fields into `m.views[m.viewed]` (creating the slot).
  - `func (m Model) loadView(v *worktreeView) Model` — copies slot `v` into the live fields and sets `m.viewed = v.path`.
  - `func (m Model) ensureView(path string) *worktreeView` — the slot for a cleaned path, created with `domain.OpenTUI(path)` on first use.
  - `func (m Model) isRepoWorktree(path string) bool` — path is one of `m.worktrees`.
  - Model fields: `views map[string]*worktreeView`, `viewed string`, `home string`.

- [ ] **Step 1: Write the failing round-trip test**

```go
// internal/tui/worktree_view_test.go
package tui

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// addWorktree adds a second worktree on a new branch and returns its path.
func addWorktree(t *testing.T, m Model, name string) string {
	t.Helper()
	other := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("git", "-C", m.currentWorktree, "worktree", "add", "-b", name, other).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return other
}

// The first load seeds home: one slot, viewed = home = the worktree gg runs in.
func TestFirstLoadSeedsTheHomeSlot(t *testing.T) {
	m := loadedModel(t)
	if m.home == "" || m.viewed != m.home || m.home != filepath.Clean(m.currentWorktree) {
		t.Fatalf("home=%q viewed=%q current=%q", m.home, m.viewed, m.currentWorktree)
	}
	if v, ok := m.views[m.home]; !ok || v.svc != m.svc {
		t.Fatalf("views[home] = %+v, want the live service", v)
	}
}

// saveView then loadView is a round trip of the Status panel, its cursor and
// its marks; the live service is the slot's.
func TestSaveAndLoadViewRoundTrip(t *testing.T) {
	m := loadedModel(t)
	m.sel[panelFiles] = 3
	m.fileMarks = map[string]bool{"a.txt": true}
	m = m.saveView()
	home := m.views[m.home]
	other := m.ensureView(addWorktree(t, m, "wt2"))
	m = m.loadView(other)
	if m.viewed != other.path || m.svc != other.svc || m.sel[panelFiles] != 0 || len(m.fileMarks) != 0 || m.currentWorktree != other.path {
		t.Fatalf("after load: viewed=%q sel=%d marks=%v current=%q", m.viewed, m.sel[panelFiles], m.fileMarks, m.currentWorktree)
	}
	m = m.loadView(home)
	if m.sel[panelFiles] != 3 || !m.fileMarks["a.txt"] || m.svc != home.svc {
		t.Fatalf("home not restored: sel=%d marks=%v", m.sel[panelFiles], m.fileMarks)
	}
}

// ensureView is keyed by the cleaned path and reuses the slot.
func TestEnsureViewIsKeyedByCleanPath(t *testing.T) {
	m := loadedModel(t)
	other := addWorktree(t, m, "wt2")
	a := m.ensureView(other + string(filepath.Separator))
	b := m.ensureView(other)
	if a != b || a.path != filepath.Clean(other) {
		t.Fatalf("slots differ: %p %p path=%q", a, b, a.path)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/fast-worktree-switch && go test ./internal/tui -run 'TestFirstLoadSeedsTheHomeSlot|TestSaveAndLoadViewRoundTrip|TestEnsureViewIsKeyedByCleanPath' 2>&1 | head -20`
Expected: build failure, `m.home undefined` / `worktreeView undefined`.

- [ ] **Step 3: Add the slot type and the save/load pair**

```go
// internal/tui/worktree_view.go
package tui

import (
	"path/filepath"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// worktreeView is the worktree-scoped part of the Model, remembered per
// worktree of the repository on screen so a switch between them is a copy,
// not a reload. The Model's own fields are the LIVE copy — every reader and
// renderer is untouched — and switchView copies out to the leaving slot
// and in from the arriving one. Worktrees of one repository share
// branches, commits, stashes, tags and the reflog, so none of those is
// here.
type worktreeView struct {
	path string          // cleaned worktree path (the map key)
	svc  *domain.Service // rooted at path: every read and op of that tree

	status         model.WorkingTreeStatus
	conflict       domain.ConflictState
	filesIdx       []int
	filesIdxReview []int
	stagedIdx      []int
	selFiles       int
	selStaged      int
	fileMarks      map[string]bool

	workingReviews    []domain.WorkingReview
	workingReviewsGen int

	docWatch docWatchState

	watcher        *gitwatch.Watcher
	watchGen       int
	watchSupported bool

	loaded bool // its first status landed (false: the panel shows loading)
}
```

Imports: `"github.com/homeend/gigagit/internal/gitwatch"` (the type of `Model.watcher`, model.go:354).

```go
// isRepoWorktree reports whether path is a worktree of the repository on
// screen — the only paths a slot may be made for.
func (m Model) isRepoWorktree(path string) bool {
	dir := filepath.Clean(path)
	for _, w := range m.worktrees {
		if filepath.Clean(w.Path) == dir {
			return true
		}
	}
	return false
}

// ensureView is the slot for path, made on first use with a service rooted
// there. Home is seeded by the first load (dataLoadedMsg), so a caller
// asking for home gets the live slot back.
func (m Model) ensureView(path string) *worktreeView {
	key := filepath.Clean(path)
	if v, ok := m.views[key]; ok {
		return v
	}
	v := &worktreeView{path: key, svc: domain.OpenTUI(key)}
	m.views[key] = v // views is a map (a pointer): the value receiver writes through
	return v
}

// saveView copies the live worktree-scoped fields into the viewed slot.
func (m Model) saveView() Model {
	if m.viewed == "" {
		return m
	}
	v := m.ensureView(m.viewed)
	v.svc = m.svc
	v.status, v.conflict = m.status, m.conflict
	v.filesIdx, v.filesIdxReview, v.stagedIdx = m.filesIdx, m.filesIdxReview, m.stagedIdx
	v.selFiles, v.selStaged = m.sel[panelFiles], m.sel[panelStaged]
	v.fileMarks = m.fileMarks
	v.workingReviews, v.workingReviewsGen = m.workingReviews, m.workingReviewsGen
	v.docWatch = m.docWatch
	v.watcher, v.watchGen, v.watchSupported = m.watcher, m.watchGen, m.watchSupported
	v.loaded = m.loadedOK
	return m
}

// loadView makes slot v the live one. The open-files registry is already
// keyed per worktree (openFilesReg.byWT) and stays shared.
func (m Model) loadView(v *worktreeView) Model {
	m.viewed = v.path
	m.svc = v.svc
	m.currentWorktree = v.path
	m = m.withStatus(v.status) // recomputes the index slices and the status stack
	m.conflict = v.conflict
	m.sel[panelFiles], m.sel[panelStaged] = v.selFiles, v.selStaged
	m.fileMarks = v.fileMarks
	m.workingReviews, m.workingReviewsGen = v.workingReviews, v.workingReviewsGen
	m.docWatch = v.docWatch
	m.watcher, m.watchGen, m.watchSupported = v.watcher, v.watchGen, v.watchSupported
	return m
}
```

Check `m.sel` is non-nil before indexing in `loadView` (reRoot sets `m.sel = map[panel]int{}`; `New` seeds it — verify in `model.go` near line 547 and add `if m.sel == nil { m.sel = map[panel]int{} }` if a test literal can reach it).

- [ ] **Step 4: Add the Model fields and seed home on the first load**

In `internal/tui/model.go` next to `currentWorktree string` (line ~92):

```go
	// views remembers the worktree-scoped state per worktree of this
	// repository (worktree_view.go); viewed is the slot on screen, home the
	// one gg's identity (exit dir, steering, snapshot) belongs to.
	views  map[string]*worktreeView
	viewed string
	home   string
```

In the constructor map seeding (line ~547, beside `srcGen: map[sourceKey]int{}`): `views: map[string]*worktreeView{},`.

In the `dataLoadedMsg` success arm right after `m.currentWorktree = msg.currentWorktree` (line ~1818):

```go
			if m.views == nil {
				m.views = map[string]*worktreeView{}
			}
			if m.home == "" || m.views[m.viewed] == nil { // first load, or a repo switch dropped the slots
				m.home = filepath.Clean(msg.currentWorktree)
				m.viewed = m.home
				m.views[m.home] = &worktreeView{path: m.home, svc: m.svc}
			}
			m = m.saveView()
```

In `reRoot` (line ~4928), after `m.svc = domain.OpenTUI(path)`:

```go
	m.views = map[string]*worktreeView{} // another repository: its worktrees are not these
	m.viewed, m.home = "", ""
```

- [ ] **Step 5: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/fast-worktree-switch && go test ./internal/tui -run 'TestFirstLoadSeedsTheHomeSlot|TestSaveAndLoadViewRoundTrip|TestEnsureViewIsKeyedByCleanPath' 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 6: Run the package and commit**

Run: `cd /work/gigagit/.claude/worktrees/fast-worktree-switch && go vet ./internal/tui && go test ./internal/tui 2>&1 | tail -3`
Expected: `ok`.

```bash
cd /work/gigagit/.claude/worktrees/fast-worktree-switch && gg add internal/tui/worktree_view.go internal/tui/worktree_view_test.go internal/tui/model.go && git commit -q -F <msgfile>
# msg: feat(tui): worktreeView slots — the worktree-scoped model state per worktree
```

---

### Task 2: `switchView(path)` — the swap, its refresh kick and the stale-read guard

**Files:**
- Modify: `internal/tui/worktree_view.go`
- Modify: `internal/tui/worktree_view_test.go`
- Modify: `internal/tui/model.go` (`Update` post-dispatch hook near line 621)

**Interfaces:**
- Consumes: Task 1's slot, `readSourceCmd`, `startWatchCmd`, `syncAgentDocs`, `closeDocWatch`, `checkSwitchTarget`, `opsIdle`.
- Produces:
  - `func (m Model) switchView(path string) (Model, bool)` — synchronous swap; `false` with `m.statusMsg` set when refused (not a repo worktree, unreachable notation, an operation running). Sets `m.viewKick = true` so the next `Update` tail launches the refresh.
  - `func (m Model) viewKickCmd() tea.Cmd` — the status read + watcher start + docs sync for the live slot.
  - Model field `viewKick bool`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/tui/worktree_view_test.go (append)

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// landView runs the swap's refresh kick (what the Update tail launches) and
// applies its status read, as the runtime would.
func landView(t *testing.T, m Model) Model {
	t.Helper()
	cmd := m.viewKickCmd()
	m.viewKick = false
	if cmd == nil {
		return m
	}
	for _, msg := range drainBatch(cmd) {
		if _, ok := msg.(dataAvailableMsg); !ok {
			continue // the watcher/docs results need a live loop; the status read is what we assert
		}
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	return m
}

// drainBatch runs a cmd (a tea.Batch or a single cmd) and collects the
// messages its leaves return.
func drainBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, drainBatch(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// Switching to another worktree shows ITS files; switching back restores
// home's cursor and marks without a reload.
func TestSwitchViewShowsTheOtherWorktreesStatusAndRestoresHome(t *testing.T) {
	m := loadedModel(t)
	other := addWorktree(t, m, "wt2")
	if err := os.WriteFile(filepath.Join(other, "only-there.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.sel[panelFiles] = 1
	m.fileMarks = map[string]bool{"home.txt": true}
	m, ok := m.switchView(other)
	if !ok || m.viewed != filepath.Clean(other) || !m.viewKick {
		t.Fatalf("switch: ok=%v viewed=%q kick=%v msg=%q", ok, m.viewed, m.viewKick, m.statusMsg)
	}
	if m.home == m.viewed {
		t.Fatal("home must stay the worktree gg runs in")
	}
	m = landView(t, m)
	var seen bool
	for _, f := range m.status.Files {
		seen = seen || f.Path == "only-there.txt"
	}
	if !seen {
		t.Fatalf("status after switch = %+v, want wt2's untracked file", m.status.Files)
	}
	m, _ = m.switchView(m.home)
	if m.sel[panelFiles] != 1 || !m.fileMarks["home.txt"] || m.viewed != m.home {
		t.Fatalf("home not restored: sel=%d marks=%v viewed=%q", m.sel[panelFiles], m.fileMarks, m.viewed)
	}
	for _, f := range m.status.Files {
		if f.Path == "only-there.txt" {
			t.Fatal("home shows wt2's file")
		}
	}
}

// A status read launched for the old slot lands after the swap: dropped.
func TestSwitchViewDropsAStaleStatusRead(t *testing.T) {
	m := loadedModel(t)
	other := addWorktree(t, m, "wt2")
	stale := m.readSourceCmd(context.Background(), srcStatus, reloadOpts{manual: true})
	m, _ = m.switchView(other)
	m = landView(t, m)
	before := m.status
	nm, _ := m.Update(stale())
	m = nm.(Model)
	if len(m.status.Files) != len(before.Files) || m.viewed != filepath.Clean(other) {
		t.Fatalf("a stale read landed: %+v", m.status.Files)
	}
}

// The swap is a no-op for the viewed path, refused for a path that is not a
// worktree of this repository, and refused while an operation runs.
func TestSwitchViewRefusals(t *testing.T) {
	m := loadedModel(t)
	other := addWorktree(t, m, "wt2")
	if nm, ok := m.switchView(m.viewed); !ok || nm.viewKick {
		t.Fatalf("same path: ok=%v kick=%v", ok, nm.viewKick)
	}
	if _, ok := m.switchView(t.TempDir()); ok {
		t.Fatal("a foreign path must be refused")
	}
	m.running = true
	if nm, ok := m.switchView(other); ok || nm.statusMsg == "" {
		t.Fatalf("running op: ok=%v msg=%q", ok, nm.statusMsg)
	}
}

// Only the live slot is watched: the slot left behind has no watcher.
func TestSwitchViewPutsTheLeavingSlotToSleep(t *testing.T) {
	m := loadedModel(t)
	other := addWorktree(t, m, "wt2")
	m.watchSupported = true
	m, _ = m.switchView(other)
	if v := m.views[m.home]; v.watcher != nil || v.docWatch.w != nil {
		t.Fatalf("home slot still awake: %+v", v)
	}
	if m.watcher != nil || m.watchSupported {
		t.Fatal("the arriving slot starts with no watcher until its kick lands")
	}
}

// The Update tail launches the kick once and clears the flag.
func TestUpdateTailLaunchesTheViewKick(t *testing.T) {
	m := loadedModel(t)
	other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if nm.(Model).viewKick || cmd == nil {
		t.Fatalf("kick=%v cmd=%v", nm.(Model).viewKick, cmd)
	}
}
```

Add `"context"` to the test file's imports.

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/fast-worktree-switch && go test ./internal/tui -run 'TestSwitchView|TestUpdateTailLaunchesTheViewKick' 2>&1 | head -10`
Expected: build failure, `m.switchView undefined`.

- [ ] **Step 3: Implement `switchView` and the kick**

```go
// internal/tui/worktree_view.go (append)

// switchView makes path's slot the live one. Same repository only: a path
// that is not one of m.worktrees, one unreachable under this environment's
// notation, or a swap asked while an operation runs is refused with a
// status message. The leaving slot sleeps (its watchers close; a read in
// flight carries the old srcStatus generation and is dropped on arrival).
// The arriving slot is on screen at once — its remembered status, or the
// loading marker until its first read — and viewKick makes the Update tail
// launch its refresh, watcher and docs sync (viewKickCmd).
func (m Model) switchView(path string) (Model, bool) {
	key := filepath.Clean(path)
	if key == m.viewed {
		return m, true
	}
	if !m.isRepoWorktree(key) {
		m.statusMsg = i18n.T("%s is not a worktree of this repository", path)
		return m, false
	}
	if verdict, _ := checkSwitchTarget(guardStat, guardGOOS, key); verdict != switchOK {
		m.statusMsg = i18n.T("cannot switch: %s is not reachable from here", path)
		return m, false
	}
	if !m.opsIdle() {
		m.statusMsg = i18n.T("an operation is running — switch once it has finished")
		return m, false
	}
	m = m.saveView()
	// Put the leaving slot to sleep: close its watchers, keep its state.
	if m.watcher != nil {
		_ = m.watcher.Close()
	}
	closeDocWatch(m.docWatch.w)
	if v := m.views[m.viewed]; v != nil {
		v.watcher, v.watchSupported = nil, false
		v.docWatch = docWatchState{gen: v.docWatch.gen + 1}
	}
	m = m.loadView(m.ensureView(key))
	m.watcher, m.watchSupported = nil, false
	m.watchGen++
	m.docWatch = docWatchState{gen: m.docWatch.gen + 1}
	m.srcGen[srcStatus]++ // a status read launched for the old slot cannot land here
	m.srcInflight[srcStatus] = false
	m.srcLoading[srcStatus] = false
	m.workingReviewsGen++ // likewise a reviews read
	m.viewKick = true
	return m, true
}

// viewKickCmd is the live slot's wake-up: a manual status read (never
// cancelled by a background lane), its git watcher and its open-files
// sync. Launched by the Update tail after a switchView.
func (m Model) viewKickCmd() tea.Cmd {
	m.srcInflight[srcStatus] = true
	m.srcLoading[srcStatus] = true
	read := m.readSourceCmd(context.Background(), srcStatus, reloadOpts{manual: true})
	_, docs := m.syncAgentDocs()
	return tea.Batch(read, m.startWatchCmd(m.watchGen), docs)
}
```

`viewKickCmd` marks the maps on a COPY — move the two map writes into the Update tail (below) so the live model records the in-flight read. Add `"context"`, `"github.com/homeend/gigagit/internal/i18n"`, `tea "github.com/charmbracelet/bubbletea"` to the imports.

Model field (next to `viewed`): `viewKick bool // switchView ran; the Update tail launches viewKickCmd once`.

In `Update` (`model.go` ~line 621), before the `startAtReady` check:

```go
	if next.viewKick {
		next.viewKick = false
		next.srcInflight[srcStatus] = true
		next.srcLoading[srcStatus] = true
		cmd = tea.Batch(cmd, next.viewKickCmd())
	}
```

and drop the two map writes from `viewKickCmd`.

- [ ] **Step 4: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/fast-worktree-switch && go test ./internal/tui -run 'TestSwitchView|TestUpdateTailLaunchesTheViewKick' 2>&1 | tail -8`
Expected: `ok`. If `TestSwitchViewShowsTheOtherWorktreesStatusAndRestoresHome` fails on the status not landing, check `landView` delivered a `dataAvailableMsg{source: srcStatus}` with `gen == m.srcGen[srcStatus]` (the kick must be built AFTER the bump — it is, since `viewKickCmd` runs in the tail).

- [ ] **Step 5: Add the four translations**

For each new key add a line under `[strings]` in `internal/i18n/lang/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml` (alphabetical position is not enforced; keep keys grouped near `"cannot switch: %s is not reachable from here"`):

```toml
"%s is not a worktree of this repository" = "%s はこのリポジトリのワークツリーではありません"   # ja
"an operation is running — switch once it has finished" = "操作を実行中です — 完了してから切り替えてください"
```
```toml
"%s is not a worktree of this repository" = "%s은(는) 이 저장소의 워크트리가 아닙니다"   # ko
"an operation is running — switch once it has finished" = "작업이 실행 중입니다 — 완료된 후 전환하세요"
```
```toml
"%s is not a worktree of this repository" = "%s 不是此仓库的工作树"   # zh
"an operation is running — switch once it has finished" = "操作正在运行 — 完成后再切换"
```
```toml
"%s is not a worktree of this repository" = "%s не является рабочим деревом этого репозитория"   # ru
"an operation is running — switch once it has finished" = "выполняется операция — переключитесь после её завершения"
```

Run: `cd /work/gigagit/.claude/worktrees/fast-worktree-switch && go test ./internal/tui -run 'I18n|Vocab|Prose' 2>&1 | tail -3` — Expected: `ok`.

- [ ] **Step 6: Run the package and commit**

Run: `cd /work/gigagit/.claude/worktrees/fast-worktree-switch && go vet ./internal/tui && go test ./internal/tui ./internal/i18n 2>&1 | tail -3`
Expected: `ok`.

```bash
gg add internal/tui/worktree_view.go internal/tui/worktree_view_test.go internal/tui/model.go internal/i18n/lang
git commit -q -F <msgfile>   # feat(tui): switchView — swap a worktree slot in, refresh it, drop the stale read
```

---

### Task 3: Slot lifecycle — a worktree that leaves the list drops its slot

**Files:**
- Modify: `internal/tui/worktree_view.go`
- Modify: `internal/tui/model.go` (`case srcWorktrees:` near line 2089; the `dataLoadedMsg` arm sets `m.worktrees` near line 1812)
- Modify: `internal/tui/worktree_view_test.go`

**Interfaces:**
- Produces: `func (m Model) pruneViews() Model` — drops slots whose path is not in `m.worktrees`; a viewed slot that went falls back to home with a status message.

- [ ] **Step 1: Write the failing test**

```go
// A worktree removed while viewed: the view falls back to home and the
// slot is gone; a removed hidden slot is dropped silently.
func TestWorktreesReloadDropsAGoneSlot(t *testing.T) {
	m := loadedModel(t)
	other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	m = landView(t, m)
	if out, err := exec.Command("git", "-C", m.home, "worktree", "remove", "--force", other).CombinedOutput(); err != nil {
		t.Fatalf("worktree remove: %v\n%s", err, out)
	}
	read := m.readSourceCmd(context.Background(), srcWorktrees, reloadOpts{manual: true})
	nm, _ := m.Update(read())
	m = nm.(Model)
	if m.viewed != m.home || m.views[filepath.Clean(other)] != nil || m.svc != m.views[m.home].svc {
		t.Fatalf("viewed=%q slots=%v", m.viewed, m.views)
	}
	if !strings.Contains(m.statusMsg, "wt2") {
		t.Fatalf("status = %q, want it to name the removed worktree", m.statusMsg)
	}
}
```

Add `"strings"` to the imports.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui -run TestWorktreesReloadDropsAGoneSlot 2>&1 | tail -5`
Expected: FAIL, `viewed=<other>`.

- [ ] **Step 3: Implement `pruneViews` and call it from both worktree-list sites**

```go
// pruneViews drops the slots of worktrees that left the list (removed,
// recycled, pruned). The viewed one going falls back to home: its
// service would point at a tree that is not there.
func (m Model) pruneViews() Model {
	for key, v := range m.views {
		if m.isRepoWorktree(key) || key == m.home {
			continue
		}
		if v.watcher != nil {
			_ = v.watcher.Close()
		}
		closeDocWatch(v.docWatch.w)
		delete(m.views, key)
		if key == m.viewed {
			gone := key
			if home := m.views[m.home]; home != nil {
				m.viewed = "" // the gone slot must not be saved back
				m = m.loadView(home)
				m.srcGen[srcStatus]++
				m.viewKick = true
			}
			m.statusMsg = i18n.T("%s is gone — showing %s", shortWorktreeName(gone), shortWorktreeName(m.home))
		}
	}
	return m
}
```

Call it right after `m.worktrees = p.worktrees` in the `srcWorktrees` arm and after `m.worktrees = msg.worktrees` in the `dataLoadedMsg` arm (the latter only when `m.home != ""`, i.e. not the seeding load). In `switchView`, guard `m.saveView()` with `if m.viewed != ""` (already does).

Translations (four bundles): `"%s is gone — showing %s"` → ja `"%s はなくなりました — %s を表示中"`, ko `"%s이(가) 사라졌습니다 — %s 표시 중"`, zh `"%s 已不存在 — 正在显示 %s"`, ru `"%s больше нет — показано %s"`.

- [ ] **Step 4: Run the tests, then the package; commit**

Run: `go test ./internal/tui -run 'TestWorktreesReloadDropsAGoneSlot|TestSwitchView' 2>&1 | tail -4 && go test ./internal/tui ./internal/i18n 2>&1 | tail -3`
Expected: `ok`.

```bash
gg add internal/tui/worktree_view.go internal/tui/worktree_view_test.go internal/tui/model.go internal/i18n/lang
git commit -q -F <msgfile>   # feat(tui): a worktree leaving the list drops its slot; a viewed one falls back to home
```

---

### Task 4: Trigger 1 — a shown console views its worktree, closing returns

**Files:**
- Modify: `internal/tui/console.go` (`consoleReturn` ~line 45, `showConsole` ~105, `captureReturn` ~151, `forgetConsoleReturn` ~176, `closeConsole` ~263)
- Modify: `internal/tui/agent_tours_open.go` (~line 52)
- Create: `internal/tui/console_view_test.go`

**Interfaces:**
- Consumes: `switchView`, `isRepoWorktree`, `landView`/`addWorktree` test helpers.
- Produces: `consoleReturn.view string` — the viewed path the cycle started from.

- [ ] **Step 1: Write the failing tests**

```go
// internal/tui/console_view_test.go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// startSessionIn starts a shell session in dir (startTestSession is home-bound).
func startSessionIn(t *testing.T, m Model, dir, script string) *domain.AgentSession {
	t.Helper()
	s := startTestSession(t, m, script) // installs the test manager + cleanup
	_ = s
	other, err := m.svc.StartSession(t.Context(), config.ToolCommand{Category: "session", Name: "Shell", Mode: "session", Command: script}, dir, "", 80, 20, nil)
	if err != nil {
		t.Fatal(err)
	}
	return other
}

func pressKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return nm.(Model)
}

// Showing a console of worktree B puts B's files on screen; closing it
// (esc) brings home back with its cursor.
func TestShownConsoleViewsItsWorktreeAndCloseReturns(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	other := addWorktree(t, m, "wt2")
	if err := os.WriteFile(filepath.Join(other, "b-only.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := startSessionIn(t, m, other, `sleep 5`)
	m.sel[panelFiles] = 1
	m, _ = m.showConsole(s.Info().ID, false)
	if m.viewed != filepath.Clean(other) || m.console == nil || m.console.ret.view != m.home {
		t.Fatalf("viewed=%q console=%+v", m.viewed, m.console)
	}
	m = landView(t, m)
	var seen bool
	for _, f := range m.status.Files {
		seen = seen || f.Path == "b-only.txt"
	}
	if !seen {
		t.Fatalf("status = %+v, want B's file", m.status.Files)
	}
	m = m.closeConsole()
	if m.viewed != m.home || m.sel[panelFiles] != 1 {
		t.Fatalf("after close: viewed=%q sel=%d", m.viewed, m.sel[panelFiles])
	}
}

// Looking equals switching: a file staged while B's console is shown lands
// in B's index.
func TestStagingWhileAConsoleIsShownRunsInItsWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	other := addWorktree(t, m, "wt2")
	if err := os.WriteFile(filepath.Join(other, "b-only.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := startSessionIn(t, m, other, `sleep 5`)
	m, _ = m.showConsole(s.Info().ID, false)
	m = landView(t, m)
	if res, ok := m.stageCmd(engine.Stage{Paths: []string{"b-only.txt"}})().(opFinishedMsg); ok && res.err != nil {
		t.Fatal(res.err)
	}
	out, _ := exec.Command("git", "-C", other, "diff", "--cached", "--name-only").Output()
	if !strings.Contains(string(out), "b-only.txt") {
		t.Fatalf("B's index = %q", out)
	}
	out, _ = exec.Command("git", "-C", m.home, "diff", "--cached", "--name-only").Output()
	if strings.Contains(string(out), "b-only.txt") {
		t.Fatal("home's index took the stage")
	}
}
```

`m.stageCmd` (op.go:104) is how the TUI's stage tests run the op (`stage_ignored_test.go:26`); check the message type it returns and its error field name there. Add `"github.com/homeend/gigagit/internal/engine"` to the imports.

```go
// alt+a over sessions in two worktrees: three distinct views, the return
// stop is home.
func TestAltACyclesWorktreesAndReturnsHome(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	wtA := addWorktree(t, m, "wtA")
	wtB := addWorktree(t, m, "wtB")
	startSessionIn(t, m, wtA, `sleep 5`)
	startSessionIn(t, m, wtB, `sleep 5`)
	m = pressAlt(t, m, 'a')
	first := m.viewed
	m = pressAlt(t, m, 'a')
	second := m.viewed
	m = pressAlt(t, m, 'a')
	if m.console != nil || m.viewed != m.home {
		t.Fatalf("return stop: console=%+v viewed=%q", m.console, m.viewed)
	}
	if first == second || first == m.home || second == m.home {
		t.Fatalf("views %q %q %q must differ", first, second, m.home)
	}
}

// A console in gg's own worktree swaps nothing.
func TestConsoleInHomeSwapsNothing(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.showConsole(s.Info().ID, false)
	if m.viewed != m.home || m.viewKick {
		t.Fatalf("viewed=%q kick=%v", m.viewed, m.viewKick)
	}
}

// With an operation running the console still shows, but the view stays
// (the swap is refused and said on the status line).
func TestConsoleShowRefusesTheSwapWhileAnOpRuns(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	other := addWorktree(t, m, "wt2")
	s := startSessionIn(t, m, other, `sleep 5`)
	m.running = true
	m, _ = m.showConsole(s.Info().ID, false)
	if m.console == nil || m.viewed != m.home || m.statusMsg == "" {
		t.Fatalf("console=%+v viewed=%q msg=%q", m.console, m.viewed, m.statusMsg)
	}
}
```

Add `"context"` to the imports. The `startSessionIn` helper starts an extra home session through `startTestSession` only to install the manager; if that extra session breaks the cycle counts in `TestAltACyclesWorktreesAndReturnsHome`, split the helper: `installSessionManager(t)` (the first 10 lines of `startTestSession`) + a start in `dir`.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestShownConsoleViews|TestStagingWhileAConsole|TestAltACyclesWorktrees|TestConsoleInHomeSwapsNothing|TestConsoleShowRefusesTheSwap' 2>&1 | head -12`
Expected: build failure `ret.view undefined`, then FAILs on `viewed`.

- [ ] **Step 3: Carry the view in the return point and swap on show/close**

`consoleReturn` gains a field:

```go
	view         string  // the viewed worktree the console was shown over (where a close returns)
```

`captureReturn` sets `view: m.viewed` in the literal.

In `showConsole`, after `m = m.syncConsoleSizeIfFocused()` and before the return:

```go
	// A shown console ⇔ the viewed worktree is the console's: tab out of
	// it and the panels are already that tree's. A refusal (an op running)
	// keeps the view and says so; the console shows regardless.
	if dir := filepath.Clean(s.Info().Dir); dir != m.viewed && m.isRepoWorktree(dir) {
		m, _ = m.switchView(dir)
	}
```

In `closeConsole`, after `r := m.console.ret` and `m = m.detachConsole()`, at both exits (the `r == nil` branch returns before — restructure so the swap runs in both):

```go
	m = m.returnView(r)
```

with

```go
// returnView brings the worktree a console was shown over back when the
// console closes. A refusal (an op running) leaves the view where it is.
func (m Model) returnView(r *consoleReturn) Model {
	if r == nil || r.view == "" || r.view == m.viewed || !m.isRepoWorktree(r.view) {
		return m
	}
	m, _ = m.switchView(r.view)
	return m
}
```

`forgetConsoleReturn` (a repo switch) adds `r.view = ""`.

`cycleSessions` needs no change: its stops are `showConsole` calls and its return stop is `closeConsole`.

In `agent_tours_open.go` (~line 52), before the `guardedReRoot` branch:

```go
	if m.isRepoWorktree(dir) {
		nm, ok := m.switchView(dir)
		if !ok {
			return nm, nil
		}
		m = nm
		m.consoleSwitch.tour = o.ID // shown once the slot's status (and its overviews sync) lands
		return m, check
	}
```

and make the `srcStatus` arrival arm (model.go ~1995) show a pending tour when `m.consoleSwitch.tour != "" && !m.consoleSwitch.armed` (an in-repo tour; the armed case is the repo-switch settle that already does it): `tour := m.consoleSwitch.tour; m.consoleSwitch.tour = ""; m = m.showTour(tour)` after `syncAgentDocs` has run — place it after the `loadWorkingReviewsCmd` chain line.

- [ ] **Step 4: Run the new tests and the existing console/cycle tests**

Run: `go test ./internal/tui -run 'Console|AltA|AltT|Cycle|Tour' 2>&1 | tail -8`
Expected: `ok`. `TestWorktreeSwitchKeepsTheReposConsole` (reRoot path) still passes: reRoot drops the slots, `settleConsole` keeps the console, the first load re-seeds home.

- [ ] **Step 5: Run the package; commit**

```bash
go vet ./internal/tui && go test ./internal/tui 2>&1 | tail -3
gg add internal/tui/console.go internal/tui/agent_tours_open.go internal/tui/console_view_test.go internal/tui/model.go
git commit -q -F <msgfile>   # feat(tui): a shown console views its worktree; closing it returns the view
```

---

### Task 5: Trigger 2 — the user's own in-repo switch adopts the slot

**Files:**
- Modify: `internal/tui/switch_guard.go` (`guardedReRoot` ~line 30)
- Modify: `internal/tui/worktree_view.go` (`adoptView`)
- Modify: `internal/tui/webhost.go` (`onWebSwitchRequest` ~line 137: `m.reRoot(msg.path)` → `m.guardedReRoot(msg.path, false)`)
- Modify: `internal/tui/avail.go` (`canEnterWorktree` ~line 219)
- Modify: `internal/tui/worktree_view_test.go`, `internal/tui/console_repo_scope_test.go`

**Interfaces:**
- Produces: `func (m Model) adoptView() (Model, tea.Cmd)` — gg's identity moves to the viewed slot: `home`, `switchTarget`, `publishedWT`, the snapshot file, the steering inbox + pending-send watch, the web host.

- [ ] **Step 1: Write the failing tests**

```go
// internal/tui/worktree_view_test.go (append)

// enter on another worktree's row is instant: the view is its, gg's exit
// dir and home follow, the Commits cursor and an open diff survive, the
// screen never blanks.
func TestInRepoSwitchAdoptsWithoutAReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	other := addWorktree(t, m, "wt2")
	m.sel[panelCommits] = 0
	m = m.pushLayer(&diffView{})
	nm, _ := m.guardedReRoot(other, true)
	m = nm.(Model)
	if m.viewed != filepath.Clean(other) || m.home != m.viewed || m.switchTarget != filepath.Clean(other) || publishedWorktree() != filepath.Clean(other) {
		t.Fatalf("viewed=%q home=%q target=%q published=%q", m.viewed, m.home, m.switchTarget, publishedWorktree())
	}
	if !m.ready || m.loading || m.diffLayer() == nil {
		t.Fatalf("the in-repo switch reloaded: ready=%v loading=%v diff=%v", m.ready, m.loading, m.diffLayer())
	}
	if m.snapshotPath != "" {
		t.Fatal("the old snapshot target must be disabled until the new one resolves")
	}
}

// The snapshot target re-resolves for the adopted worktree.
func TestAdoptViewReResolvesTheSnapshotTarget(t *testing.T) {
	m := loadedModel(t)
	other := addWorktree(t, m, "wt2")
	nm, cmd := m.guardedReRoot(other, true)
	m = nm.(Model)
	for _, msg := range drainBatch(cmd) {
		if st, ok := msg.(snapshotTargetMsg); ok {
			nm, _ := m.Update(st)
			m = nm.(Model)
		}
	}
	if filepath.Clean(m.snapshotWorktree) != filepath.Clean(other) {
		t.Fatalf("snapshotWorktree = %q, want %q", m.snapshotWorktree, other)
	}
}

// A target in another repository keeps the full reload.
func TestOtherRepoSwitchStillReRoots(t *testing.T) {
	m := loadedModel(t)
	nm, _ := m.guardedReRoot(gittest.BasicRepo(t, "b\n"), false)
	m = nm.(Model)
	if m.ready || len(m.views) != 0 {
		t.Fatalf("ready=%v views=%v: another repo must reRoot", m.ready, m.views)
	}
}

// A switch asked while a console views B adopts the target and the console's
// return now points there.
func TestSwitchWhileAConsoleIsShownAdoptsAndRetargetsTheReturn(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	other := addWorktree(t, m, "wt2")
	s := startSessionIn(t, m, other, `sleep 5`)
	m, _ = m.showConsole(s.Info().ID, false)
	nm, _ := m.guardedReRoot(other, true)
	m = nm.(Model)
	if m.home != filepath.Clean(other) || m.console == nil || m.console.ret.view != m.home {
		t.Fatalf("home=%q console=%+v", m.home, m.console)
	}
	m = m.closeConsole()
	if m.viewed != filepath.Clean(other) {
		t.Fatalf("close must stay in the adopted worktree, viewed=%q", m.viewed)
	}
}

// reRoot to another repository drops the slots and the console's return
// view, so a later close cannot swap to a path of the old repo.
func TestRepoSwitchDropsTheSlots(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	other := addWorktree(t, m, "wt2")
	s := startSessionIn(t, m, other, `sleep 5`)
	m, _ = m.showConsole(s.Info().ID, false)
	nm, _ := m.reRoot(gittest.BasicRepo(t, "b\n"))
	m = nm.(Model)
	if len(m.views) != 0 || m.viewed != "" || (m.console != nil && m.console.ret.view != "") {
		t.Fatalf("views=%v viewed=%q console=%+v", m.views, m.viewed, m.console)
	}
}
```

Add `"github.com/homeend/gigagit/internal/gittest"` to the imports.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestInRepoSwitchAdopts|TestAdoptViewReResolves|TestOtherRepoSwitchStillReRoots|TestSwitchWhileAConsoleIsShownAdopts|TestRepoSwitchDropsTheSlots' 2>&1 | head -12`
Expected: `TestInRepoSwitchAdopts…` FAILs (`ready=false`: today it reRoots); `TestOtherRepoSwitchStillReRoots` and `TestRepoSwitchDropsTheSlots` may already pass.

- [ ] **Step 3: The fast path in `guardedReRoot` and `adoptView`**

In `switch_guard.go`, at the top of `guardedReRoot`:

```go
	if m.home != "" && m.isRepoWorktree(path) {
		// Same repository: a slot swap, then gg's identity follows. Never a
		// reload — the repo-scoped panels, an open diff and the cursors stay.
		nm, ok := m.switchView(path)
		if !ok {
			return nm, nil
		}
		return nm.adoptView()
	}
```

(`checkSwitchTarget` runs inside `switchView`; the repair offer for a foreign-notation path is still reached because `switchView` refuses with `switchOK != verdict` — keep the repair offer: run `checkSwitchTarget` FIRST in `guardedReRoot` as today, and only take the fast path on `switchOK`.)

```go
// internal/tui/worktree_view.go (append)

// adoptView moves gg's identity to the viewed slot — the user's own switch,
// as opposed to a console looking at another worktree. What reRoot does
// for the identity, without the teardown: the exit directory, the session
// registry's worktree, the session snapshot (disabled now, re-resolved off
// thread), the steering inbox and the pending-send watch (closed for the
// old worktree, re-homed by snapshotTargetMsg → initSteerInbox), the hosted
// web page.
func (m Model) adoptView() (Model, tea.Cmd) {
	if m.viewed == m.home {
		return m, nil
	}
	m.home = m.viewed
	m.switchTarget = m.viewed
	publishedWT.Store(m.viewed)
	removeSnapshotFile(m.snapshotPath)
	m.snapshotPath, m.snapshotCommonDir, m.snapshotWorktree, m.lastSnapshot = "", "", "", nil
	m = m.closeSteerInbox()
	m = m.closePendingWatch()
	m.steerGen++
	m.noticeGen++ // the pending-send watch and a health read of the old worktree
	if m.console != nil && m.console.ret != nil {
		m.console.ret.view = m.home // a close stays where the user asked to be
	}
	m.statusMsg = i18n.T("switched to %s", shortWorktreeName(m.home))
	return m, tea.Batch(snapshotTargetCmd(m.svc), m.pendingWatchCmd(m.noticeGen), m.repoHealthCmd(m.noticeGen), m.webRerootCmd())
}
```

Check `removeSnapshotFile`, `closePendingWatch`, `pendingWatchCmd`, `repoHealthCmd` signatures match their use in `reRoot` (model.go ~4930–5061) and reuse exactly those. If `repoHealthCmd` re-reads something keyed per repository only, drop it from the batch.

`webhost.go` `onWebSwitchRequest`: replace `nm, cmd := m.reRoot(msg.path)` with `nm, cmd := m.guardedReRoot(msg.path, false)` so the page's switch to a worktree of this repo takes the fast path; the `webRerootMsg` handler that answers `m.web.pendingSwitch` is reached through `webRerootCmd` in `adoptView`. Verify in the handler that it does not require `m.loading` to have been true.

`avail.go` `canEnterWorktree`: `wt.Path != m.currentWorktree` → `(filepath.Clean(wt.Path) != m.viewed || m.viewed != m.home)` — a row can be entered when it is not on screen, or when it is on screen only because a console looks at it.

Translation `"switched to %s"`: check it exists in the bundles (`grep -n '^"switched to %s"' internal/i18n/lang/*.toml`); add if missing: ja `"%s に切り替えました"`, ko `"%s(으)로 전환했습니다"`, zh `"已切换到 %s"`, ru `"переключено на %s"`.

- [ ] **Step 4: Run the new tests, then every switch/console test**

Run: `go test ./internal/tui -run 'Switch|Console|AltA|Repo|Steer|Web' 2>&1 | tail -8`
Expected: `ok`. `TestWorktreeSwitchKeepsTheReposConsole` in `console_repo_scope_test.go` calls `m.reRoot` directly and keeps passing; add beside it:

```go
// enter on another worktree of the same repo keeps the console AND keeps
// the screen: no reload, the view is the target.
func TestWorktreeEnterKeepsConsoleAndScreen(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	other := addWorktree(t, m, "wt2")
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	nm, _ := m.guardedReRoot(other, true)
	m = nm.(Model)
	if m.console == nil || !m.ready || m.viewed != filepath.Clean(other) {
		t.Fatalf("console=%+v ready=%v viewed=%q", m.console, m.ready, m.viewed)
	}
}
```

- [ ] **Step 5: Run the package; commit**

```bash
go vet ./internal/tui && go test ./internal/tui ./internal/i18n 2>&1 | tail -3
gg add internal/tui/switch_guard.go internal/tui/worktree_view.go internal/tui/webhost.go internal/tui/avail.go internal/tui/worktree_view_test.go internal/tui/console_repo_scope_test.go internal/i18n/lang
git commit -q -F <msgfile>   # feat(tui): an in-repo worktree switch is a slot swap that adopts gg's identity
```

---

### Task 6: On screen — the Worktrees marker, the status hint, help, and the web stays home

**Files:**
- Modify: `internal/tui/view.go` (~line 1228 worktree rows)
- Modify: `internal/tui/console.go` (`consoleWorktreeHint`, `withConsoleWorktree` ~609–645)
- Modify: `internal/tui/help.go`, `internal/tui/footer.go` (the alt+a / alt+t lines; find with `grep -n 'alt+a' internal/tui/help.go internal/tui/footer.go`)
- Modify: `internal/tui/console_worktree_hint_test.go`, `internal/tui/worktree_view_test.go`, a view test file for the marker (`internal/tui/view_worktrees_test.go`, create)

- [ ] **Step 1: Write the failing tests**

```go
// internal/tui/view_worktrees_test.go
package tui

import (
	"strings"
	"testing"
)

// Home keeps "* "; a viewed worktree that is not home gets "» ".
func TestWorktreeRowsMarkHomeAndTheViewedOne(t *testing.T) {
	m := loadedModel(t)
	other := addWorktree(t, m, "wt2")
	read := m.readSourceCmd(context.Background(), srcWorktrees, reloadOpts{manual: true})
	nm, _ := m.Update(read())
	m = nm.(Model)
	m, _ = m.switchView(other)
	rows := strings.Join(m.worktreeRows(), "\n") // the function that builds the rows at view.go:~1220; use its real name
	if !strings.Contains(rows, "* ") || !strings.Contains(rows, "» ") {
		t.Fatalf("rows = %q, want both markers", rows)
	}
}
```

```go
// internal/tui/console_worktree_hint_test.go (append)

// The hint names the console's worktree only when it is not gg's OWN
// (home): while the console views it, the header already shows it.
func TestConsoleHintIsAboutHomeNotTheView(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	other := addWorktree(t, m, "wt2")
	s := startSessionIn(t, m, other, `sleep 5`)
	m, _ = m.showConsole(s.Info().ID, true)
	if got := m.consoleWorktreeHint(); got != filepath.Clean(other) {
		t.Fatalf("focused console in another worktree: hint = %q, want its path", got)
	}
	nm, _ := m.guardedReRoot(other, true) // adopt: now it IS gg's own
	m = nm.(Model)
	if got := m.consoleWorktreeHint(); got != "" {
		t.Fatalf("after adopt a focused console on home: hint = %q, want none", got)
	}
	if row := m.withConsoleWorktree("x", 80); !strings.Contains(row, "x") {
		t.Fatalf("row = %q", row)
	}
}
```

```go
// internal/tui/worktree_view_test.go (append)

// A console looking at B does not move the hosted web page; adopting does.
func TestAdoptViewRerootsTheWebPageLookDoesNot(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	other := addWorktree(t, m, "wt2")
	m.web = &webHostState{host: &fakeWebHost{}} // webServing() = web != nil && host != nil (webhost.go:209); the fake is in webhost_test.go
	s := startSessionIn(t, m, other, `sleep 5`)
	m, _ = m.showConsole(s.Info().ID, false)
	if cmd := m.viewKickCmd(); hasWebReroot(drainBatch(cmd)) {
		t.Fatal("a look must not reroot the page")
	}
	nm, cmd := m.guardedReRoot(other, true)
	_ = nm
	if !hasWebReroot(drainBatch(cmd)) {
		t.Fatal("adopt must reroot the page")
	}
}

func hasWebReroot(msgs []tea.Msg) bool {
	for _, msg := range msgs {
		if _, ok := msg.(webRerootMsg); ok {
			return true
		}
	}
	return false
}
```

`fakeWebHost.Reroot` must exist on the fake (add a no-op returning nil if `webhost_test.go` lacks it); the assertion is on `webRerootMsg` being produced only by adopt.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestWorktreeRowsMarkHomeAndTheViewedOne|TestConsoleHintIsAboutHomeNotTheView|TestAdoptViewRerootsTheWebPage' 2>&1 | head -10`
Expected: FAIL on the missing `» ` marker and the hint.

- [ ] **Step 3: Implement**

`view.go` (~1228):

```go
		marker := "  "
		switch filepath.Clean(w.Path) {
		case m.home:
			marker = "* "
		case m.viewed:
			marker = "» " // on screen because a console looks at it
		}
```

`console.go` `consoleWorktreeHint`: compare against home, not the view:

```go
	if m.console.focused && filepath.Clean(dir) == m.home {
		return ""
	}
```

`withConsoleWorktree`: the label becomes `i18n.T("showing: %s", …)` with the same width arithmetic (replace both `"worktree: %s"` uses). Translations: ja `"表示中: %s"`, ko `"표시 중: %s"`, zh `"显示中: %s"`, ru `"показано: %s"`. Keep `"worktree: %s"` in the bundles if any other site uses it (`grep -rn '"worktree: %s"' internal/tui`); remove from the four bundles only if unused (the unused-key gate, if one exists, will say).

`help.go` / `footer.go`: the alt+a line reads `i18n.T("show the agent and its worktree")` style — find the existing strings (`grep -n 'alt+a' internal/tui/help.go internal/tui/footer.go`), append " — the panels show its worktree" to the help row's text, and add the four translations of the changed key (the old key goes if it becomes unused).

- [ ] **Step 4: Run the i18n gates and the package; commit**

```bash
go test ./internal/tui -run 'I18n|Vocab|Prose|Menu|Hint|Worktree|Web' 2>&1 | tail -4 && go test ./internal/tui 2>&1 | tail -2
gg add internal/tui/view.go internal/tui/console.go internal/tui/help.go internal/tui/footer.go internal/tui/view_worktrees_test.go internal/tui/console_worktree_hint_test.go internal/tui/worktree_view_test.go internal/i18n/lang
git commit -q -F <msgfile>   # feat(tui): viewed-worktree marker, "showing:" hint about home, help text
```

---

### Task 7: e2e golden screen for the instant worktree switch

**Files:**
- Create: `e2e/scenarios/tui_worktree_switch_fast.toml`
- Golden screens under the harness's golden dir (created by `-update`)

Read the `writing-e2e-scenarios` skill first (`.claude/skills/writing-e2e-scenarios/SKILL.md`) for the `[input]` worktree step and the golden update flag.

- [ ] **Step 1: Write the scenario**

```toml
name = "tui: enter on a Worktrees row switches instantly and keeps the Commits cursor"

# Two worktrees; wt-x has an untracked file the main checkout does not. Enter
# on wt-x's row shows its Status at once (no blank), the header path is wt-x,
# the Commits cursor stays on the second row, and the Worktrees row marks it.
[input]
steps = [
  { write = "a.txt", content = "a\n" }, { commit = "one" },
  { write = "a.txt", content = "a2\n" }, { commit = "two" },
  { worktree = "wt-x", branch = "feature/x" },
  { write = "x-only.txt", content = "x\n", cwd = "wt-x" },
]

[tui]
size = "160x40"

[[tui.step]]
name = "cursor-on-second-commit"
keys = ["tab", "tab", "down"]
screen_contains = ["one"]

[[tui.step]]
name = "switched"
keys = ["tab", "tab", "down", "enter"]
screen_contains = ["?? x-only.txt", "wt-x", "* "]
```

`{ worktree = …, branch = … }` runs `git worktree add` with an EXISTING branch (e2e/builder.go:133), so add `{ branch = "feature/x" }` before it. `cwd` retargets a step to a sandbox-relative dir (scenario.go:118). The panel-focus keys (`tab` counts to reach Worktrees) must be taken from an existing TUI scenario that focuses the Worktrees panel; if none, read `focusNext` in internal/tui to count.

- [ ] **Step 2: Build the binary and record the goldens**

Run: `cd /work/gigagit/.claude/worktrees/fast-worktree-switch && go build -o bin/gg ./cmd/gg && go test ./e2e -run 'TestScenarios/tui_worktree_switch_fast' -update 2>&1 | tail -5`
Expected: goldens written. Open each golden (`cat e2e/golden/tui_worktree_switch_fast/*.txt` or the harness's path) and confirm the second screen shows wt-x's untracked file, its path in the header and the `*` on its row — never a blank or "loading" screen.

- [ ] **Step 3: Run it without `-update`; commit**

Run: `go test ./e2e -run 'TestScenarios/tui_worktree_switch_fast' 2>&1 | tail -3`
Expected: `ok`.

```bash
gg add e2e/scenarios/tui_worktree_switch_fast.toml e2e/golden   # the golden dir the harness uses
git commit -q -F <msgfile>   # test(e2e): golden screens for the instant in-repo worktree switch
```

---

### Task 8: Docs, changelog, skill, details, memory

**Files:**
- Modify: `CHANGELOG.md` (top entry), `README.md` (worktrees / agent consoles sections), `docs/CLAUDE-details.md` (tui: console section + a new "worktree slots" paragraph), `CLAUDE.md` (`tui` row: add "per-worktree view slots (`worktree_view.go`): a shown console views its worktree, an in-repo switch swaps and adopts")
- Memory: update `console-repo-scope-feature.md`, `alt-a-cycle-return-point.md`, `console-worktree-status-hint.md` with the new rule; new `fast-worktree-switch-feature.md`; index line in `MEMORY.md`.

- [ ] **Step 1: CHANGELOG entry**

```markdown
### Fast worktree switch (TUI)
- A switch between worktrees of one repository is now a swap of remembered
  per-worktree state (Status, cursors, marks, open files, working reviews),
  not a reload: the screen never blanks, an open diff and the Commits cursor
  survive, the exit directory, steering inbox, session registry and hosted
  web page follow.
- alt+a / alt+t (and ctrl+\ enter) show the session's console AND the panels
  of the worktree it runs in; tab out and you are there. Closing the console
  brings your own worktree back. Operations launched meanwhile run in the
  shown worktree. The Worktrees panel marks the shown row with `»`, the status
  row says `showing: <path>` when a console runs outside your own worktree.
- Other repositories keep the full reload.
```

- [ ] **Step 2: README + CLAUDE-details + CLAUDE.md row**

In `docs/CLAUDE-details.md` under the tui console section add the invariant ("a shown console ⇔ the viewed worktree is the console's"), the slot field list, `switchView` refusals, `adoptView`'s identity list, and the gotcha: `viewKick` is launched by the Update tail because `closeConsole` returns a Model only.

- [ ] **Step 3: Memory files**

Write `fast-worktree-switch-feature.md` (type project) with the rulings: look == switch; swap on show, never on confirm; both triggers shipped; hint is about home. Update the three console notes' "How to apply" lines to point at `switchView`. Add the index line.

- [ ] **Step 4: Full gate, then commit**

Run: `cd /work/gigagit/.claude/worktrees/fast-worktree-switch && ./test.sh 2>&1 | tail -5` then `./test.sh race 2>&1 | tail -5`
Expected: both end in "all green" (check the log line, not the exit code of a `| tail`).

```bash
gg add CHANGELOG.md README.md docs/CLAUDE-details.md CLAUDE.md
git commit -q -F <msgfile>   # docs: fast worktree switch
```

Then the final whole-branch review as a read-only subagent on the most capable model, fix findings, re-run `./test.sh race`, and ask the user before merging.
