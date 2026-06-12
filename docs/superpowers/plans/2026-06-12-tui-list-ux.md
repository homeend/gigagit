# TUI List UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reverse focus cycling, 25%-viewport paging, generic per-panel selectable sort, and `/` filtering on every TUI list panel — with action keys always resolving to the row the user sees.

**Architecture:** One per-panel view pipeline (`filter → sort → visible []int`) written once against a `panelList` interface; each panel implements its own name/date semantics. All selection math, clamping, rendering, and action handlers consume the pipeline's index mapping. Spec: `docs/superpowers/specs/2026-06-12-tui-list-ux-design.md`.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss; real-git tests via existing helpers (`newTestRepo`, `newRepoDir`, `loadedModel`, `keyMsg`, `runGit`).

**Branch:** Create `feat/tui-list-ux` off `main` before Task 1.

## File Structure

- `internal/model/model.go` (modify) — `Branch.UnixTime`.
- `internal/git/repo.go` (modify) — `Branches` format gains `%(committerdate:unix)`.
- `internal/git/branch_parse.go` (modify) — parse the new field.
- `internal/git/log.go` (modify) — new `CommitTimes` batch verb.
- `internal/tui/viewstate.go` (create) — layout geometry helper, `panelList` interface + 4 implementations, the pipeline (`panelView`, `less`, `backingIndex`), `sortMode`, `panelLabel`.
- `internal/tui/model.go` (modify) — new Model fields, `New` init, shift+tab/pgup/pgdown/`o`/`/`/esc keys, filter-input routing, action handlers via `backingIndex`, `panelLen` via pipeline.
- `internal/tui/view.go` (modify) — `renderInterface` uses the shared geometry + `panelView` rows + `panelLabel`; footer hint.
- `internal/tui/load.go` (modify) — load worktree HEAD commit times (non-fatal).
- Tests: extend `internal/git/branch_parse_test.go`, `internal/git/log_test.go`; create `internal/tui/pgnav_test.go`, `internal/tui/viewstate_test.go`, `internal/tui/filter_test.go`.

**Invariant the pipeline relies on:** the existing row builders (`branchRows`, `worktreeRows`, `statusRows`, `commitRows`) are 1:1 with their backing slices — `Row(i)` is the display text of backing element `i`. Do not change that.

---

### Task 1: Branch committer date (git verb + model + parser)

**Files:**
- Modify: `internal/model/model.go` (Branch struct)
- Modify: `internal/git/repo.go` (`Branches` format const)
- Modify: `internal/git/branch_parse.go`
- Test: `internal/git/branch_parse_test.go`, `internal/git/repo_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/git/branch_parse_test.go`:

```go
func TestParseBranchesCommitterDate(t *testing.T) {
	data := "*\x00main\x00origin/main\x00abc1234\x00[ahead 1]\x001717777777\n" +
		" \x00old\x00\x00def5678\x00\x00\n"
	bs, err := ParseBranches([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 2 {
		t.Fatalf("parsed %d branches, want 2", len(bs))
	}
	if bs[0].UnixTime != 1717777777 {
		t.Errorf("UnixTime = %d, want 1717777777", bs[0].UnixTime)
	}
	if bs[1].UnixTime != 0 {
		t.Errorf("empty date field should parse as 0, got %d", bs[1].UnixTime)
	}
}
```

Append to `internal/git/repo_test.go`:

```go
func TestBranchesIncludeCommitterDate(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	bs, err := repo.Branches(context.Background())
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if len(bs) == 0 {
		t.Fatal("expected at least one branch")
	}
	if bs[0].UnixTime == 0 {
		t.Fatalf("expected nonzero UnixTime, got %+v", bs[0])
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/git/ -run 'CommitterDate' -v`
Expected: FAIL — `bs[0].UnixTime` undefined (compile error).

- [ ] **Step 3: Implement**

In `internal/model/model.go`, add to the `Branch` struct (after `Hash string`):

```go
	UnixTime int64 // committer time (unix seconds) of the branch tip; 0 if unknown
```

In `internal/git/repo.go`, change the `Branches` format const to:

```go
	const format = "%(HEAD)%00%(refname:short)%00%(upstream:short)%00%(objectname:short)%00%(upstream:track)%00%(committerdate:unix)"
```

In `internal/git/branch_parse.go`, after the existing `if len(f) >= 5 { ... }` track block, add:

```go
		if len(f) >= 6 {
			b.UnixTime, _ = strconv.ParseInt(strings.TrimSpace(f[5]), 10, 64)
		}
```

(`strconv` and `strings` are already imported there.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/git/ -run 'CommitterDate|ParseBranches' -v` then `go test ./internal/git/`
Expected: PASS, no regressions (the parser tolerates old 5-field lines).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/model/model.go internal/git/repo.go internal/git/branch_parse.go internal/git/branch_parse_test.go internal/git/repo_test.go
go vet ./internal/git/ ./internal/model/
git add internal/model/model.go internal/git/
git commit -m "feat(git): branches carry committer time for date sorting"
```

---

### Task 2: CommitTimes batch verb

**Files:**
- Modify: `internal/git/log.go`
- Test: `internal/git/log_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/git/log_test.go` (check its imports: it needs `context`, `strings`, `os/exec`, and `github.com/gigagit/gg/internal/gitexec` — add any missing):

```go
func TestCommitTimesBatchesOneInvocation(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log (commit times)", gitexec.Result{Stdout: "aaa\x001000\nbbb\x002000\n"})
	repo := &Repo{Runner: f}
	got, err := repo.CommitTimes(context.Background(), []string{"aaa", "bbb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("git calls = %d, want exactly 1 (batched)", len(f.Calls))
	}
	argv := strings.Join(f.Calls[0].Argv, " ")
	if !strings.Contains(argv, "--no-walk") || !strings.Contains(argv, "aaa") || !strings.Contains(argv, "bbb") {
		t.Fatalf("argv = %q", argv)
	}
	if got["aaa"] != 1000 || got["bbb"] != 2000 {
		t.Fatalf("times = %v", got)
	}
}

func TestCommitTimesEmptyInputMakesNoGitCall(t *testing.T) {
	f := gitexec.NewFakeRunner()
	repo := &Repo{Runner: f}
	got, err := repo.CommitTimes(context.Background(), nil)
	if err != nil || len(got) != 0 || len(f.Calls) != 0 {
		t.Fatalf("got=%v err=%v calls=%d", got, err, len(f.Calls))
	}
}

func TestCommitTimesRealRepo(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(out))
	got, err := repo.CommitTimes(context.Background(), []string{sha})
	if err != nil {
		t.Fatal(err)
	}
	if got[sha] == 0 {
		t.Fatalf("no time for %s: %v", sha, got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/git/ -run CommitTimes -v`
Expected: FAIL — `repo.CommitTimes` undefined (compile error).

- [ ] **Step 3: Implement**

Append to `internal/git/log.go`:

```go
// CommitTimes returns the committer time (unix seconds) for each given commit
// in ONE invocation (`git log --no-walk --format=%H%x00%ct <sha…>`), keeping
// the cost flat for many worktrees. Empty input returns an empty map with no
// git call.
func (r *Repo) CommitTimes(ctx context.Context, shas []string) (map[string]int64, error) {
	out := map[string]int64{}
	if len(shas) == 0 {
		return out, nil
	}
	argv := gitcmd.New("log").Arg("--no-walk", "--format=%H%x00%ct").Arg(shas...).ToArgv()
	res, err := r.Runner.Run(ctx, "git log (commit times)", argv)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Split(strings.TrimSpace(line), "\x00")
		if len(f) != 2 {
			continue
		}
		if t, perr := strconv.ParseInt(f[1], 10, 64); perr == nil {
			out[f[0]] = t
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/git/ -run CommitTimes -v` then `go test ./internal/git/`
Expected: PASS (3 tests), no regressions.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/git/log.go internal/git/log_test.go
go vet ./internal/git/
git add internal/git/log.go internal/git/log_test.go
git commit -m "feat(git): CommitTimes batch verb for worktree HEAD dates"
```

---

### Task 3: Shared layout geometry + Shift+Tab + 25%-viewport paging

**Files:**
- Create: `internal/tui/viewstate.go`
- Modify: `internal/tui/view.go` (`renderInterface`)
- Modify: `internal/tui/model.go` (key cases)
- Modify: `internal/tui/model_test.go` (`keyMsg` helper)
- Test: `internal/tui/pgnav_test.go` (create)

- [ ] **Step 1: Extend the `keyMsg` test helper**

In `internal/tui/model_test.go`, add cases to the `switch s` in `keyMsg` (before `default`):

```go
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/tui/pgnav_test.go`:

```go
package tui

import (
	"fmt"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestShiftTabCyclesBackwards(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelBranches
	u, _ := m.Update(keyMsg("shift+tab"))
	m = u.(Model)
	if m.focus != panelCommits {
		t.Fatalf("focus = %v, want panelCommits", m.focus)
	}
	for i := 0; i < int(panelCount)-1; i++ {
		u, _ = m.Update(keyMsg("shift+tab"))
		m = u.(Model)
	}
	if m.focus != panelBranches {
		t.Fatalf("full reverse cycle should return to panelBranches, got %v", m.focus)
	}
}

func TestPageDownMovesQuarterViewportAndClamps(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 27 // bodyH 24 → commits rowsCap 21 → step 5
	m.focus = panelCommits
	m.commits = make([]model.Commit, 12)
	for i := range m.commits {
		m.commits[i] = model.Commit{Hash: fmt.Sprintf("%040d", i), Subject: fmt.Sprintf("c%d", i), UnixTime: int64(i)}
	}
	if got := m.pageStep(); got != 5 {
		t.Fatalf("pageStep = %d, want 5", got)
	}
	u, _ := m.Update(keyMsg("pgdown"))
	m = u.(Model)
	if m.sel[panelCommits] != 5 {
		t.Fatalf("sel = %d, want 5", m.sel[panelCommits])
	}
	u, _ = m.Update(keyMsg("pgdown"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("pgdown"))
	m = u.(Model)
	if m.sel[panelCommits] != 11 {
		t.Fatalf("sel = %d, want clamped 11", m.sel[panelCommits])
	}
	u, _ = m.Update(keyMsg("pgup"))
	m = u.(Model)
	if m.sel[panelCommits] != 6 {
		t.Fatalf("sel = %d, want 6", m.sel[panelCommits])
	}
}

func TestPageStepFallsBackTo1WhenPanelHidden(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 80, 11 // bodyH 8 < 9: Worktrees panel hidden by layout
	m.focus = panelWorktrees
	if got := m.pageStep(); got != 1 {
		t.Fatalf("pageStep = %d, want fallback 1", got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'ShiftTab|PageDown|PageStep' -v`
Expected: FAIL — `m.pageStep` undefined (compile error).

- [ ] **Step 4: Create the geometry helpers**

Create `internal/tui/viewstate.go`:

```go
package tui

// layoutGeom is the panel geometry renderInterface draws with. boxH holds each
// panel's box height under the current layout; a panel missing from the map
// (or 0) is not visible at this terminal size.
type layoutGeom struct {
	w, h, bodyH   int
	leftW, rightW int
	boxH          map[panel]int
}

// layout computes panel geometry for the current terminal size. It is the
// single source of truth shared by renderInterface and the paging keys, so
// rendering and navigation can never disagree about a panel's viewport.
func (m Model) layout() layoutGeom {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	bodyH := h - 3
	if bodyH < 6 {
		bodyH = 6
	}
	g := layoutGeom{w: w, h: h, bodyH: bodyH, boxH: map[panel]int{}}

	// Narrow terminals: a single commits column.
	if w < 40 {
		g.rightW = w
		g.boxH[panelCommits] = bodyH
		return g
	}

	leftW := w / 3
	if leftW < 16 {
		leftW = 16
	}
	if leftW > w-24 {
		leftW = w - 24
	}
	g.leftW, g.rightW = leftW, w-leftW

	if bodyH >= 9 {
		// Three stacked left panels (each bordered panel needs >=3 rows).
		h1 := bodyH / 3
		h2 := bodyH / 3
		g.boxH[panelBranches] = h1
		g.boxH[panelWorktrees] = h2
		g.boxH[panelStatus] = bodyH - h1 - h2
	} else {
		// Short terminal: Branches over Status only.
		bh := bodyH / 2
		g.boxH[panelBranches] = bh
		g.boxH[panelStatus] = bodyH - bh
	}
	g.boxH[panelCommits] = bodyH
	return g
}

// panelRowsCap is how many data rows panel p can display right now (0 when the
// layout hides it). Mirrors renderPanel: box height minus borders (2) minus
// the label line (1).
func (m Model) panelRowsCap(p panel) int {
	n := m.layout().boxH[p] - 3
	if n < 0 {
		n = 0
	}
	return n
}

// pageStep is the pgup/pgdown jump: 25% of the focused panel's viewport,
// at least 1 row.
func (m Model) pageStep() int {
	s := m.panelRowsCap(m.focus) / 4
	if s < 1 {
		s = 1
	}
	return s
}
```

- [ ] **Step 5: Refactor `renderInterface` onto the shared geometry**

In `internal/tui/view.go`, replace the body of `renderInterface` with (behavior identical — the existing fit tests are the regression guard):

```go
func (m Model) renderInterface() string {
	g := m.layout()

	header := m.headerLine(g.w)
	footer := truncate("[p]ull [P]ush [s]witch [S]tash [u]ndo [w]orktree [d]elete  •  [tab] focus  [r] reload  [q] quit", g.w)
	statusLine := m.statusMsg
	if m.running {
		statusLine = "⏳ " + statusLine
	}
	statusLine = truncate(statusLine, g.w)

	// Narrow terminals: a single commits column (two columns won't fit cleanly).
	if g.w < 40 {
		body := m.renderPanel(panelCommits, "Commits", m.commitRows(), g.w, g.boxH[panelCommits])
		return strings.Join([]string{header, body, footer, statusLine}, "\n")
	}

	var left string
	if g.boxH[panelWorktrees] > 0 {
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(panelBranches, "Branches", m.branchRows(), g.leftW, g.boxH[panelBranches]),
			m.renderPanel(panelWorktrees, "Worktrees", m.worktreeRows(), g.leftW, g.boxH[panelWorktrees]),
			m.renderPanel(panelStatus, "Status", m.statusRows(), g.leftW, g.boxH[panelStatus]),
		)
	} else {
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(panelBranches, "Branches", m.branchRows(), g.leftW, g.boxH[panelBranches]),
			m.renderPanel(panelStatus, "Status", m.statusRows(), g.leftW, g.boxH[panelStatus]),
		)
	}
	right := m.renderPanel(panelCommits, "Commits", m.commitRows(), g.rightW, g.boxH[panelCommits])
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return strings.Join([]string{header, body, footer, statusLine}, "\n")
}
```

- [ ] **Step 6: Add the key cases**

In `internal/tui/model.go`, in the normal-key `switch msg.String()`, immediately after the existing `case "tab":` line, add:

```go
		case "shift+tab":
			m.focus = (m.focus - 1 + panelCount) % panelCount
		case "pgdown":
			if n := m.panelLen(m.focus); n > 0 {
				m.sel[m.focus] += m.pageStep()
				if m.sel[m.focus] > n-1 {
					m.sel[m.focus] = n - 1
				}
			}
		case "pgup":
			if m.sel[m.focus] > 0 {
				m.sel[m.focus] -= m.pageStep()
				if m.sel[m.focus] < 0 {
					m.sel[m.focus] = 0
				}
			}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'ShiftTab|PageDown|PageStep' -v` then `go test ./internal/tui/`
Expected: PASS (3 new tests); ALL existing tui tests still pass (especially the fit/layout tests — they prove the refactor changed nothing visually).

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/tui/
go vet ./internal/tui/
git add internal/tui/
git commit -m "feat(tui): shift+tab reverse focus; pgup/pgdn move 25% of panel viewport"
```

---

### Task 4: Generic panelList pipeline + selectable sort (`o`)

**Files:**
- Modify: `internal/tui/viewstate.go` (interface, implementations, pipeline, sortMode, panelLabel)
- Modify: `internal/tui/model.go` (fields, `New` init, `o` key, `panelLen`, action handlers, `dataLoadedMsg`)
- Modify: `internal/tui/worktree_popup.go` (`openWorktreePopup` branch resolution)
- Modify: `internal/tui/view.go` (`renderInterface` consumes `panelView` + `panelLabel`)
- Modify: `internal/tui/load.go` (headTimes)
- Test: `internal/tui/viewstate_test.go` (create)

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/viewstate_test.go`:

```go
package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// fakeList proves the pipeline is generic: it never inspects concrete types.
type fakeList struct {
	names []string
	dates []int64
}

func (l fakeList) Len() int          { return len(l.names) }
func (l fakeList) Row(i int) string  { return l.names[i] }
func (l fakeList) Name(i int) string { return l.names[i] }
func (l fakeList) Date(i int) int64  { return l.dates[i] }

func sortedNames(l fakeList, mode sortMode) []string {
	idx := make([]int, l.Len())
	for i := range idx {
		idx[i] = i
	}
	sortIndices(l, mode, idx)
	out := make([]string, len(idx))
	for n, i := range idx {
		out[n] = l.names[i]
	}
	return out
}

func TestGenericSortOrders(t *testing.T) {
	l := fakeList{
		names: []string{"Beta", "alpha", "gamma"},
		dates: []int64{200, 0, 100},
	}
	cases := []struct {
		mode sortMode
		want string
	}{
		{sortDefault, "Beta,alpha,gamma"},
		{sortNameAsc, "alpha,Beta,gamma"}, // case-insensitive
		{sortNameDesc, "gamma,Beta,alpha"},
		{sortDateAsc, "gamma,Beta,alpha"},  // 100,200; zero-date alpha LAST
		{sortDateDesc, "Beta,gamma,alpha"}, // 200,100; zero-date alpha LAST
	}
	for _, c := range cases {
		if got := strings.Join(sortedNames(l, c.mode), ","); got != c.want {
			t.Errorf("mode %v: got %s, want %s", c.mode, got, c.want)
		}
	}
}

func TestGenericSortStableOnTies(t *testing.T) {
	l := fakeList{names: []string{"b1", "b2", "a"}, dates: []int64{5, 5, 5}}
	if got := strings.Join(sortedNames(l, sortDateAsc), ","); got != "b1,b2,a" {
		t.Errorf("ties must keep backing order, got %s", got)
	}
}

func TestBranchesDefaultSortIsDateDesc(t *testing.T) {
	m := loadedModel(t)
	m.branches = []model.Branch{
		{Name: "alpha", UnixTime: 100},
		{Name: "zeta", UnixTime: 300},
		{Name: "mid", UnixTime: 200},
	}
	if m.sortModes[panelBranches] != sortDateDesc {
		t.Fatalf("New() should default Branches to date desc, got %v", m.sortModes[panelBranches])
	}
	_, idx := m.panelView(panelBranches)
	if len(idx) != 3 || m.branches[idx[0]].Name != "zeta" || m.branches[idx[2]].Name != "alpha" {
		t.Fatalf("idx = %v (newest-first expected)", idx)
	}
}

func TestOKeyCyclesModesAndLabelShowsThem(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelWorktrees
	if m.sortModes[panelWorktrees] != sortDefault {
		t.Fatalf("worktrees should start in default order")
	}
	u, _ := m.Update(keyMsg("o"))
	m = u.(Model)
	if m.sortModes[panelWorktrees] != sortNameAsc {
		t.Fatalf("after o: %v, want sortNameAsc", m.sortModes[panelWorktrees])
	}
	if got := m.panelLabel(panelWorktrees, "Worktrees"); got != "Worktrees ·name↑" {
		t.Fatalf("label = %q", got)
	}
	for i := 0; i < 4; i++ {
		u, _ = m.Update(keyMsg("o"))
		m = u.(Model)
	}
	if m.sortModes[panelWorktrees] != sortDefault {
		t.Fatalf("five presses must cycle back to default, got %v", m.sortModes[panelWorktrees])
	}
	if got := m.panelLabel(panelWorktrees, "Worktrees"); got != "Worktrees" {
		t.Fatalf("default mode must not decorate the label, got %q", got)
	}
}

func TestActionResolvesThroughSortedView(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-sorted")
	runGit(t, dir, "worktree", "add", "-b", "zzz-newest", wt, "main")
	m := New(repo)
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	m.focus = panelWorktrees
	m.sortModes[panelWorktrees] = sortNameDesc // "zzz-newest" sorts before "main"
	m.sel[panelWorktrees] = 0
	bi, ok := m.backingIndex(panelWorktrees)
	if !ok {
		t.Fatal("backingIndex not ok")
	}
	if m.worktrees[bi].Branch != "zzz-newest" {
		t.Fatalf("selected backing row = %q, want zzz-newest (the visibly-first row)", m.worktrees[bi].Branch)
	}
}

func TestLoadPopulatesWorktreeHeadTimes(t *testing.T) {
	_, repo := newRepoDir(t)
	m := New(repo)
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	if len(m.worktrees) == 0 {
		t.Fatal("expected the primary worktree")
	}
	if m.headTimes[m.worktrees[0].Head] == 0 {
		t.Fatalf("headTimes missing for %s: %v", m.worktrees[0].Head, m.headTimes)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'GenericSort|DefaultSort|OKeyCycles|ActionResolves|HeadTimes' -v`
Expected: FAIL — `sortMode`, `sortIndices`, `panelView`, `backingIndex`, `headTimes` undefined (compile error).

- [ ] **Step 3: Implement the pipeline in `viewstate.go`**

Append to `internal/tui/viewstate.go` (add imports: `os`, `path/filepath`, `sort`, `strings`, `github.com/gigagit/gg/internal/model`):

```go
// sortMode is a panel's display order. Cycled by the `o` key.
type sortMode int

const (
	sortDefault sortMode = iota // git's emission order
	sortNameAsc
	sortNameDesc
	sortDateAsc
	sortDateDesc
	sortModeCount
)

// String is the label suffix; empty for the default order.
func (s sortMode) String() string {
	switch s {
	case sortNameAsc:
		return "name↑"
	case sortNameDesc:
		return "name↓"
	case sortDateAsc:
		return "date↑"
	case sortDateDesc:
		return "date↓"
	}
	return ""
}

// panelList is the per-panel contract behind generic filtering and sorting.
// Each panel implements its own semantics for name and date; the pipeline
// never inspects concrete types. Row(i) must be the display text of backing
// element i (the existing row builders are 1:1 with their slices).
type panelList interface {
	Len() int
	Row(i int) string  // display text — also the filter-match target
	Name(i int) string // what "sort by name" means for THIS panel
	Date(i int) int64  // what "sort by date" means for THIS panel (unix; 0 = unknown)
}

type branchList struct {
	items []model.Branch
	rows  []string
}

func (l branchList) Len() int          { return len(l.items) }
func (l branchList) Row(i int) string  { return l.rows[i] }
func (l branchList) Name(i int) string { return l.items[i].Name }
func (l branchList) Date(i int) int64  { return l.items[i].UnixTime }

type worktreeList struct {
	items []model.Worktree
	rows  []string
	times map[string]int64 // HEAD sha -> committer time
}

func (l worktreeList) Len() int         { return len(l.items) }
func (l worktreeList) Row(i int) string { return l.rows[i] }
func (l worktreeList) Name(i int) string {
	if b := l.items[i].Branch; b != "" {
		return b
	}
	return l.items[i].Path // detached/bare fall back to the path
}
func (l worktreeList) Date(i int) int64 { return l.times[l.items[i].Head] }

type statusList struct {
	files []model.FileStatus
	rows  []string
	root  string
	mtime map[int]int64 // lazy per-view stat cache; 0 = unknown (sorts last)
}

func (l statusList) Len() int          { return len(l.files) }
func (l statusList) Row(i int) string  { return l.rows[i] }
func (l statusList) Name(i int) string { return l.files[i].Path }
func (l statusList) Date(i int) int64 {
	if t, ok := l.mtime[i]; ok {
		return t
	}
	var t int64
	if fi, err := os.Stat(filepath.Join(l.root, l.files[i].Path)); err == nil {
		t = fi.ModTime().Unix()
	}
	l.mtime[i] = t
	return t
}

type commitList struct {
	items []model.Commit
	rows  []string
}

func (l commitList) Len() int          { return len(l.items) }
func (l commitList) Row(i int) string  { return l.rows[i] }
func (l commitList) Name(i int) string { return l.items[i].Subject }
func (l commitList) Date(i int) int64  { return l.items[i].UnixTime }

// listFor builds panel p's panelList from the current model snapshot.
func (m Model) listFor(p panel) panelList {
	switch p {
	case panelBranches:
		return branchList{items: m.branches, rows: m.branchRows()}
	case panelWorktrees:
		return worktreeList{items: m.worktrees, rows: m.worktreeRows(), times: m.headTimes}
	case panelStatus:
		return statusList{files: m.status.Files, rows: m.statusRows(), root: m.currentWorktree, mtime: map[int]int64{}}
	case panelCommits:
		return commitList{items: m.commits, rows: m.commitRows()}
	}
	return commitList{}
}

// sortIndices orders backing indices in place under mode. sortDefault is a
// no-op. Ties and unknown comparisons keep backing order (stable).
func sortIndices(l panelList, mode sortMode, idx []int) {
	if mode == sortDefault {
		return
	}
	sort.SliceStable(idx, func(a, b int) bool { return viewLess(l, mode, idx[a], idx[b]) })
}

// viewLess orders two backing indices under mode. Unknown dates (0) sort last
// in BOTH directions so missing data never floats to the top.
func viewLess(l panelList, mode sortMode, a, b int) bool {
	switch mode {
	case sortNameAsc, sortNameDesc:
		na, nb := strings.ToLower(l.Name(a)), strings.ToLower(l.Name(b))
		if na == nb {
			return false
		}
		if mode == sortNameAsc {
			return na < nb
		}
		return na > nb
	case sortDateAsc, sortDateDesc:
		da, db := l.Date(a), l.Date(b)
		if da == 0 || db == 0 {
			return da != 0 && db == 0
		}
		if da == db {
			return false
		}
		if mode == sortDateAsc {
			return da < db
		}
		return da > db
	}
	return false
}

// panelView applies panel p's sort (and, later, filter), returning the display
// rows and the matching backing indices (display row n shows backing element
// idx[n]). It is the single source of truth for what a panel shows; selection,
// paging, clamping, rendering, and action keys all consume it.
func (m Model) panelView(p panel) (rows []string, idx []int) {
	l := m.listFor(p)
	idx = make([]int, 0, l.Len())
	for i := 0; i < l.Len(); i++ {
		idx = append(idx, i)
	}
	sortIndices(l, m.sortModes[p], idx)
	rows = make([]string, len(idx))
	for n, i := range idx {
		rows[n] = l.Row(i)
	}
	return rows, idx
}

// backingIndex resolves panel p's current selection to an index into its
// backing slice, accounting for the view transforms. ok is false when the
// visible list is empty or the selection is out of range.
func (m Model) backingIndex(p panel) (int, bool) {
	_, idx := m.panelView(p)
	s := m.sel[p]
	if s < 0 || s >= len(idx) {
		return 0, false
	}
	return idx[s], true
}

// panelLabel decorates a panel title with its active sort mode.
func (m Model) panelLabel(p panel, base string) string {
	if s := m.sortModes[p].String(); s != "" {
		base += " ·" + s
	}
	return base
}
```

- [ ] **Step 4: Wire the Model**

In `internal/tui/model.go`:

(a) Add fields to the `Model` struct (near `sel map[panel]int`):

```go
	sortModes map[panel]sortMode // per-panel display order (zero value = default)
	headTimes map[string]int64   // worktree HEAD sha -> committer time (date sort)
```

(b) In `func New(...)`, where the Model literal initializes `sel`, also initialize:

```go
		sortModes: map[panel]sortMode{panelBranches: sortDateDesc},
```

(c) Replace the body of `panelLen` with:

```go
func (m Model) panelLen(p panel) int {
	_, idx := m.panelView(p)
	return len(idx)
}
```

(d) In the `case dataLoadedMsg:` block, inside `if msg.err == nil`, add **before** the clamp loop (the clamp consults `panelLen`, which now reads these):

```go
			m.headTimes = msg.headTimes
```

(e) Add the `o` key case after the `pgup` case:

```go
		case "o":
			if !m.running && !m.loading {
				if m.sortModes == nil {
					m.sortModes = map[panel]sortMode{}
				}
				m.sortModes[m.focus] = (m.sortModes[m.focus] + 1) % sortModeCount
				if n := m.panelLen(m.focus); m.sel[m.focus] >= n && n > 0 {
					m.sel[m.focus] = n - 1
				}
			}
```

(f) Route the action handlers through `backingIndex` — replace the bodies of the `s`, `enter` (worktree switch), and `d` cases:

```go
		case "s":
			if !m.running && !m.loading {
				if bi, ok := m.backingIndex(panelBranches); ok {
					return m.startOp(engine.SmartSwitch{Branch: m.branches[bi].Name})
				}
			}
```

```go
		case "enter":
			if !m.running && !m.loading && m.focus == panelWorktrees {
				if bi, ok := m.backingIndex(panelWorktrees); ok {
					target := m.worktrees[bi].Path
					if target != "" && target != m.currentWorktree {
						return m.reRoot(target)
					}
				}
			}
```

```go
		case "d":
			if !m.running && !m.loading && m.focus == panelWorktrees {
				if bi, ok := m.backingIndex(panelWorktrees); ok {
					wt := m.worktrees[bi]
					return m.startOp(engine.RemoveWorktree{Path: wt.Path, Branch: wt.Branch})
				}
			}
```

(g) In `internal/tui/worktree_popup.go`, `openWorktreePopup` reads the selected branch via direct indexing (search for `m.sel[panelBranches]`). Replace that resolution with:

```go
	bi, ok := m.backingIndex(panelBranches)
	if !ok {
		return m, false
	}
	// then use m.branches[bi] wherever the selected branch was used
```

(keep the function's existing structure; only the index resolution changes).

- [ ] **Step 5: Render through the pipeline**

In `internal/tui/view.go`'s `renderInterface`, replace the row-builder calls and plain labels with `panelView` + `panelLabel`. The two-column block becomes:

```go
	brRows, _ := m.panelView(panelBranches)
	wtRows, _ := m.panelView(panelWorktrees)
	stRows, _ := m.panelView(panelStatus)
	cmRows, _ := m.panelView(panelCommits)

	var left string
	if g.boxH[panelWorktrees] > 0 {
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(panelBranches, m.panelLabel(panelBranches, "Branches"), brRows, g.leftW, g.boxH[panelBranches]),
			m.renderPanel(panelWorktrees, m.panelLabel(panelWorktrees, "Worktrees"), wtRows, g.leftW, g.boxH[panelWorktrees]),
			m.renderPanel(panelStatus, m.panelLabel(panelStatus, "Status"), stRows, g.leftW, g.boxH[panelStatus]),
		)
	} else {
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(panelBranches, m.panelLabel(panelBranches, "Branches"), brRows, g.leftW, g.boxH[panelBranches]),
			m.renderPanel(panelStatus, m.panelLabel(panelStatus, "Status"), stRows, g.leftW, g.boxH[panelStatus]),
		)
	}
	right := m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits"), cmRows, g.rightW, g.boxH[panelCommits])
```

and the narrow branch becomes:

```go
	if g.w < 40 {
		cmRows, _ := m.panelView(panelCommits)
		body := m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits"), cmRows, g.w, g.boxH[panelCommits])
		return strings.Join([]string{header, body, footer, statusLine}, "\n")
	}
```

- [ ] **Step 6: Load head times**

In `internal/tui/load.go`:

(a) Add to `dataLoadedMsg`:

```go
	headTimes map[string]int64
```

(b) In `loadCmd`, after the worktrees fetch succeeds, add:

```go
		// Worktree HEAD commit times power the Worktrees panel's date sort; a
		// failure is non-fatal (dates read as 0 and date sort keeps backing order).
		shas := make([]string, 0, len(out.worktrees))
		for _, w := range out.worktrees {
			if w.Head != "" {
				shas = append(shas, w.Head)
			}
		}
		if times, tErr := repo.CommitTimes(ctx, shas); tErr == nil {
			out.headTimes = times
		}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'GenericSort|DefaultSort|OKeyCycles|ActionResolves|HeadTimes' -v` then `go test ./internal/tui/`
Expected: PASS (7 new tests). **Note:** existing tests that asserted branch-panel ordering may now see date-desc order (New defaults Branches to `sortDateDesc`); if a test fails on ordering only, set `m.sortModes[panelBranches] = sortDefault` in that test — do not weaken assertions.

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/tui/
go vet ./internal/tui/
git add internal/tui/
git commit -m "feat(tui): generic panelList pipeline; o cycles per-panel sort (name/date asc/desc)"
```

---

### Task 5: `/` filter on every list panel

**Files:**
- Modify: `internal/tui/viewstate.go` (`panelView` filter stage, `panelLabel` query, `filterActive`)
- Modify: `internal/tui/model.go` (filter fields, input-mode routing, `/` + esc keys)
- Modify: `internal/tui/view.go` (footer hint)
- Test: `internal/tui/filter_test.go` (create)

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/filter_test.go`:

```go
package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeRunes(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(Model)
	}
	return m
}

func TestSlashFilterLifecycle(t *testing.T) {
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "fix-1")
	runGit(t, dir, "branch", "fix-2")
	runGit(t, dir, "branch", "feat-x")
	m := New(repo)
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	m.focus = panelBranches

	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	if !m.filterTyping || m.filterPanel != panelBranches {
		t.Fatalf("/ should start filter input on the focused panel")
	}
	m = typeRunes(t, m, "fix")
	if n := m.panelLen(panelBranches); n != 2 {
		t.Fatalf("filtered len = %d, want 2 (fix-1, fix-2)", n)
	}
	u, _ = m.Update(keyMsg("backspace"))
	m = u.(Model)
	if m.filterQuery != "fi" {
		t.Fatalf("query = %q, want fi", m.filterQuery)
	}
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.filterTyping || m.filterQuery != "fi" {
		t.Fatalf("enter must commit and keep the filter (typing=%v q=%q)", m.filterTyping, m.filterQuery)
	}
	// esc in normal mode clears the committed filter.
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.filterQuery != "" {
		t.Fatalf("esc should clear the filter, query = %q", m.filterQuery)
	}
	if n := m.panelLen(panelBranches); n != 4 {
		t.Fatalf("unfiltered len = %d, want 4", n)
	}
}

func TestEscDuringTypingCancelsAndClears(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelBranches
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	m = typeRunes(t, m, "xyz")
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.filterTyping || m.filterQuery != "" {
		t.Fatalf("esc while typing must cancel (typing=%v q=%q)", m.filterTyping, m.filterQuery)
	}
}

func TestFilterTypingSwallowsGlobalKeys(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelBranches
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("p")) // would start SmartPull in normal mode
	m = u.(Model)
	if m.running {
		t.Fatal("global key leaked through filter input")
	}
	if m.filterQuery != "p" {
		t.Fatalf("query = %q, want p", m.filterQuery)
	}
	u, _ = m.Update(keyMsg("q")) // would quit in normal mode
	m = u.(Model)
	if m.filterQuery != "pq" {
		t.Fatalf("query = %q, want pq", m.filterQuery)
	}
}

func TestFilteredEnterSwitchesToVisibleWorktree(t *testing.T) {
	dir, repo := newRepoDir(t)
	wtA := filepath.Join(filepath.Dir(dir), "wt-aaa")
	wtB := filepath.Join(filepath.Dir(dir), "wt-bbb")
	runGit(t, dir, "worktree", "add", "-b", "feature/aaa", wtA, "main")
	runGit(t, dir, "worktree", "add", "-b", "feature/bbb", wtB, "main")
	m := New(repo)
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	m.focus = panelWorktrees

	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	m = typeRunes(t, m, "bbb")
	u, _ = m.Update(keyMsg("enter")) // commit the filter
	m = u.(Model)
	if n := m.panelLen(panelWorktrees); n != 1 {
		t.Fatalf("filtered len = %d, want 1", n)
	}
	u, _ = m.Update(keyMsg("enter")) // act on the visible row
	m = u.(Model)
	wantR, _ := filepath.EvalSymlinks(wtB)
	gotR, _ := filepath.EvalSymlinks(m.switchTarget)
	if gotR != wantR {
		t.Fatalf("switchTarget = %q, want %q — action hit the wrong backing row", m.switchTarget, wtB)
	}
}

func TestFilterSurvivesReloadAndMovesBetweenPanels(t *testing.T) {
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "fix-1")
	m := New(repo)
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	m.focus = panelBranches
	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	m = typeRunes(t, m, "fix")
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)

	// Reload: the filter re-applies over fresh data.
	u, _ = m.Update(m.loadCmd()())
	m = u.(Model)
	if m.filterQuery != "fix" || m.panelLen(panelBranches) != 1 {
		t.Fatalf("filter lost on reload: q=%q len=%d", m.filterQuery, m.panelLen(panelBranches))
	}

	// Starting / on another panel moves the filter (the old one clears).
	m.focus = panelCommits
	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	if m.filterPanel != panelCommits || m.filterQuery != "" {
		t.Fatalf("new / must rebind the filter: panel=%v q=%q", m.filterPanel, m.filterQuery)
	}
	if m.panelLen(panelBranches) < 2 {
		t.Fatal("old panel must be unfiltered after the filter moved")
	}
}

func TestFilterLabelRendering(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.focus = panelBranches
	m.sortModes[panelBranches] = sortDateDesc
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	m = typeRunes(t, m, "fi")
	out := m.View()
	if !strings.Contains(out, "Branches ·date↓ /fi█") {
		t.Fatalf("label missing sort+filter+cursor decoration:\n%s", out)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'Filter|EscDuring' -v`
Expected: FAIL — `m.filterTyping` etc. undefined (compile error).

- [ ] **Step 3: Implement filter state + pipeline stage**

(a) In `internal/tui/model.go`, add Model fields (next to `sortModes`):

```go
	filterPanel  panel  // panel the filter is bound to (meaningful only when filterQuery != "" or filterTyping)
	filterQuery  string // case-insensitive substring; "" = no filter
	filterTyping bool   // true while /-input mode is capturing keys
```

(b) In `internal/tui/viewstate.go`, add:

```go
// filterActive reports whether panel p currently has a committed or in-progress
// filter query.
func (m Model) filterActive(p panel) bool {
	return p == m.filterPanel && m.filterQuery != ""
}
```

(c) In `panelView`, replace the index-building loop with the filtering version:

```go
	q := ""
	if m.filterActive(p) {
		q = strings.ToLower(m.filterQuery)
	}
	idx = make([]int, 0, l.Len())
	for i := 0; i < l.Len(); i++ {
		if q != "" && !strings.Contains(strings.ToLower(l.Row(i)), q) {
			continue
		}
		idx = append(idx, i)
	}
```

(d) Extend `panelLabel`:

```go
func (m Model) panelLabel(p panel, base string) string {
	if s := m.sortModes[p].String(); s != "" {
		base += " ·" + s
	}
	if m.filterTyping && p == m.filterPanel {
		base += " /" + m.filterQuery + "█"
	} else if m.filterActive(p) {
		base += " /" + m.filterQuery
	}
	return base
}
```

- [ ] **Step 4: Implement key routing**

In `internal/tui/model.go`, inside the `case tea.KeyMsg:` block, **after** the `if m.popup != nil { ... }` dispatch and **before** the normal-key `switch msg.String()`, add:

```go
		// Filter-input mode captures every key (the panel label shows the query).
		if m.filterTyping {
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc:
				m.filterTyping = false
				m.filterQuery = ""
			case tea.KeyEnter:
				m.filterTyping = false // commit: filter stays active
			case tea.KeyBackspace:
				if r := []rune(m.filterQuery); len(r) > 0 {
					m.filterQuery = string(r[:len(r)-1])
				}
				m.sel[m.filterPanel] = 0
			case tea.KeySpace:
				m.filterQuery += " "
				m.sel[m.filterPanel] = 0
			case tea.KeyRunes:
				m.filterQuery += string(msg.Runes)
				m.sel[m.filterPanel] = 0
			}
			return m, nil
		}
```

Then add to the normal-key switch (after the `o` case):

```go
		case "/":
			if !m.running && !m.loading {
				m.filterPanel = m.focus
				m.filterQuery = ""
				m.filterTyping = true
				m.sel[m.focus] = 0
			}
		case "esc":
			if m.filterQuery != "" {
				m.filterQuery = ""
			}
```

- [ ] **Step 5: Footer hint**

In `internal/tui/view.go`, update the footer string to:

```go
	footer := truncate("[p]ull [P]ush [s]witch [S]tash [u]ndo [w]orktree [d]elete [o]rder [/]filter  •  [tab] focus  [r] reload  [q] quit", g.w)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'Filter|EscDuring' -v` then `go test ./internal/tui/`
Expected: PASS (7 new tests), no regressions.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/tui/
go vet ./internal/tui/
git add internal/tui/
git commit -m "feat(tui): / filters any list panel; esc clears; actions follow the filtered view"
```

---

### Task 6: Docs + final verification

**Files:**
- Modify: `CHANGELOG.md`, `README.md`

- [ ] **Step 1: CHANGELOG**

In `CHANGELOG.md` under `## [Unreleased]` → `### Added`, append a new subsection:

```markdown
#### TUI list UX
- `shift+tab` cycles panel focus backwards; `pgup`/`pgdn` move the selection by
  25% of the focused panel's viewport.
- `o` cycles a panel's sort order (`default → name ↑/↓ → date ↑/↓`) — each
  panel defines its own name/date semantics (branches: committer date;
  worktrees: HEAD commit date; status files: mtime; commits: commit time).
  Branches default to newest-first.
- `/` filters any list panel by case-insensitive substring (`enter` keeps the
  filter, `esc` clears it); selection, paging, and all action keys operate on
  the filtered, sorted view.
```

- [ ] **Step 2: README key table**

In `README.md`, add rows to the TUI key table:

```markdown
| `shift+tab` | move focus backwards |
| `pgup`/`pgdn` | move selection by 25% of the panel viewport |
| `o` | cycle the focused panel's sort order (name/date, asc/desc) |
| `/` | filter the focused panel (type, then `enter` to keep, `esc` to clear) |
```

- [ ] **Step 3: Full verification**

```bash
gofmt -l internal/ cmd/        # must print nothing
go vet ./...
go test ./... -race
```

Expected: all clean / PASS.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md README.md
git commit -m "docs: record TUI list UX (shift+tab, paging, sort, filter)"
```
