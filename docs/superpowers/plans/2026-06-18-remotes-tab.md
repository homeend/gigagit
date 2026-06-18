# Remotes Tab (read-only) + Behind-Indicator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only **Remotes** tab (third tab in the shared left slot, ordered `Branches · Remotes · Worktrees`) listing `refs/remotes/*`, plus a `(↓N)` behind-indicator on local Branches rows.

**Architecture:** Mirror the existing Branches tab at every layer — a `model.RemoteBranch` type, a `git.Repo.RemoteBranches` verb + parser, a `Snapshot` field, and a `remoteBranchList`/`remoteRows`/`panelRemotes` set in the TUI. The shared-slot tab toggle becomes a 3-way directional cycle. The behind-indicator is a one-line render change in `branchRows()` using the already-populated `Branch.Behind`.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), table-driven `_test.go` tests with a real `git` in `t.TempDir()` or `FakeRunner` for argv assertions.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26. Branch off `main`; the human merges.
- A git verb is ONE invocation built with `gitcmd`, run via `r.Runner.Run`. `internal/tui` never imports `internal/git` — it reaches git through `internal/domain`.
- Frontend reads go through `domain` queries (here: `Snapshot`), never direct `internal/git` calls.
- TUI `Model` is a value receiver; mutate the copy and return it.
- TDD: failing test first, minimal code, green, commit. Run `./test.sh unit` before declaring a task done; `./test.sh race` before the final wrap-up.
- Test-file naming: do NOT end a test filename with a `_GOOS`/`_GOARCH` token before `_test.go` (e.g. `_darwin`, `_windows`, `_amd64`) — it silently excludes the file on other platforms and yields a false "ok". `remote_parse_test.go` is safe.
- Every new keybinding lands in BOTH `help.go` and the footer — **this chunk adds no new keybinding**, so only copy that enumerates the two tabs needs updating.
- After the feature: update `CHANGELOG.md` (always) and `README.md` if a user-facing surface description changed. No CLI surface change → no `agentskill` bump.

---

### Task 1: `model.RemoteBranch` + `git` verb + parser

**Files:**
- Modify: `internal/model/model.go` (add type after `Branch`, ~line 78)
- Create: `internal/git/remote_parse.go`
- Modify: `internal/git/repo.go` (add `RemoteBranches` verb after `Branches`, ~line 37)
- Test: `internal/git/remote_parse_test.go`
- Test: `internal/git/repo_test.go` (verb argv assertion — append)

**Interfaces:**
- Produces: `model.RemoteBranch{Name, Remote, Branch, Hash string; UnixTime int64}`
- Produces: `git.ParseRemoteBranches(data []byte) ([]model.RemoteBranch, error)`
- Produces: `(*git.Repo).RemoteBranches(ctx context.Context) ([]model.RemoteBranch, error)`

- [ ] **Step 1: Write the failing parser test**

Create `internal/git/remote_parse_test.go`:

```go
package git

import (
	"reflect"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestParseRemoteBranches(t *testing.T) {
	// Format: %(refname:short)\x00%(objectname:short)\x00%(committerdate:unix)
	data := []byte(
		"origin/main\x00abc1234\x001700000000\n" +
			"origin/feature/x\x00def5678\x001700000100\n" +
			"upstream/main\x009990000\x001700000200\n" +
			"origin\x00abc1234\x001700000000\n" + // origin/HEAD symref short form -> dropped
			"origin/HEAD\x00abc1234\x001700000000\n" + // explicit HEAD -> dropped
			"\n", // blank -> skipped
	)
	got, err := ParseRemoteBranches(data)
	if err != nil {
		t.Fatalf("ParseRemoteBranches: %v", err)
	}
	want := []model.RemoteBranch{
		{Name: "origin/main", Remote: "origin", Branch: "main", Hash: "abc1234", UnixTime: 1700000000},
		{Name: "origin/feature/x", Remote: "origin", Branch: "feature/x", Hash: "def5678", UnixTime: 1700000100},
		{Name: "upstream/main", Remote: "upstream", Branch: "main", Hash: "9990000", UnixTime: 1700000200},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./internal/git/ -run TestParseRemoteBranches -v`
Expected: FAIL — `undefined: ParseRemoteBranches` and `model.RemoteBranch`.

- [ ] **Step 3: Add the model type**

In `internal/model/model.go`, after the `Branch` struct (~line 78):

```go
// RemoteBranch is one entry from `git for-each-ref refs/remotes`.
type RemoteBranch struct {
	Name     string // short ref, e.g. "origin/feature/x"
	Remote   string // "origin"
	Branch   string // "feature/x" (Name with the remote prefix removed)
	Hash     string // short object name
	UnixTime int64  // committer time (unix seconds); 0 if unknown
}
```

- [ ] **Step 4: Write the parser**

Create `internal/git/remote_parse.go`:

```go
package git

import (
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/model"
)

// ParseRemoteBranches parses `git for-each-ref refs/remotes` output formatted as:
//
//	%(refname:short)\x00%(objectname:short)\x00%(committerdate:unix)
//
// one ref per line. The remote's default symref (listed as the bare remote name,
// e.g. "origin", or explicitly as "origin/HEAD") is dropped — it is a pointer,
// not a branch.
func ParseRemoteBranches(data []byte) ([]model.RemoteBranch, error) {
	var out []model.RemoteBranch
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 2 {
			continue
		}
		name := f[0]
		remote, branch, ok := strings.Cut(name, "/")
		if !ok {
			continue // bare remote name == the default symref short form
		}
		if branch == "HEAD" {
			continue // explicit origin/HEAD symref
		}
		rb := model.RemoteBranch{
			Name:   name,
			Remote: remote,
			Branch: branch,
			Hash:   f[1],
		}
		if len(f) >= 3 {
			rb.UnixTime, _ = strconv.ParseInt(strings.TrimSpace(f[2]), 10, 64)
		}
		out = append(out, rb)
	}
	return out, nil
}
```

- [ ] **Step 5: Run the parser test to confirm green**

Run: `go test ./internal/git/ -run TestParseRemoteBranches -v`
Expected: PASS.

- [ ] **Step 6: Write the failing verb test**

Append to `internal/git/repo_test.go` (use the same `FakeRunner` pattern as the existing `Branches` test in this file — mirror its setup):

```go
func TestRemoteBranchesArgv(t *testing.T) {
	fr := &gitexec.FakeRunner{}
	fr.Out = "origin/main\x00abc1234\x001700000000\n"
	r := &Repo{Runner: fr}
	if _, err := r.RemoteBranches(context.Background()); err != nil {
		t.Fatalf("RemoteBranches: %v", err)
	}
	gotArgv := strings.Join(fr.LastArgv, " ")
	want := "for-each-ref --format=%(refname:short)\x00%(objectname:short)\x00%(committerdate:unix) refs/remotes"
	if gotArgv != want {
		t.Fatalf("argv:\n got %q\nwant %q", gotArgv, want)
	}
}
```

NOTE: match the `FakeRunner` field/method names actually used by the existing
`Branches`/`Status` tests in `repo_test.go` (e.g. how they read the captured
argv and set canned stdout). Adjust the snippet's `fr.Out`/`fr.LastArgv` to the
real field names if they differ — do NOT invent new ones.

- [ ] **Step 7: Run it to confirm it fails**

Run: `go test ./internal/git/ -run TestRemoteBranchesArgv -v`
Expected: FAIL — `r.RemoteBranches undefined`.

- [ ] **Step 8: Write the verb**

In `internal/git/repo.go`, after `Branches` (~line 37):

```go
// RemoteBranches returns remote-tracking branches (refs/remotes), excluding the
// per-remote HEAD symref.
func (r *Repo) RemoteBranches(ctx context.Context) ([]model.RemoteBranch, error) {
	const format = "%(refname:short)\x00%(objectname:short)\x00%(committerdate:unix)"
	argv := gitcmd.New("for-each-ref").Arg("--format="+format, "refs/remotes").ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref (remotes)", argv)
	if err != nil {
		return nil, err
	}
	return ParseRemoteBranches([]byte(res.Stdout))
}
```

- [ ] **Step 9: Run the git package tests**

Run: `go test ./internal/git/ -run 'TestParseRemoteBranches|TestRemoteBranchesArgv' -v`
Expected: PASS both.

- [ ] **Step 10: Commit**

```bash
git add internal/model/model.go internal/git/remote_parse.go internal/git/remote_parse_test.go internal/git/repo.go internal/git/repo_test.go
git commit -m "feat(git): RemoteBranches verb + parser + model.RemoteBranch"
```

---

### Task 2: `Snapshot` wiring in `domain`

**Files:**
- Modify: `internal/domain/query.go` (Snapshot struct ~line 16; `loadSnapshot` ~line 53)
- Test: `internal/domain/query_test.go` (append; mirror the existing Snapshot test)

**Interfaces:**
- Consumes: `(*git.Repo).RemoteBranches` (Task 1)
- Produces: `domain.Snapshot.RemoteBranches []model.RemoteBranch`

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/query_test.go`. Find the existing test that builds a
real repo (look for `newTestRepo`/`newRepo` helper used by the current Snapshot
test) and add a sibling that creates a remote-tracking ref, then asserts it
surfaces. Pattern:

```go
func TestSnapshotIncludesRemoteBranches(t *testing.T) {
	// Reuse whatever helper the existing Snapshot test uses to get a Service
	// over a real temp repo. Then fabricate a remote-tracking ref:
	//   git update-ref refs/remotes/origin/main <HEAD-sha>
	svc, repoDir := newSnapshotService(t) // <-- use the real helper name in this file
	mustGit(t, repoDir, "update-ref", "refs/remotes/origin/main", "HEAD")

	snap, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	found := false
	for _, rb := range snap.RemoteBranches {
		if rb.Name == "origin/main" && rb.Remote == "origin" && rb.Branch == "main" {
			found = true
		}
	}
	if !found {
		t.Fatalf("origin/main not in snapshot.RemoteBranches: %#v", snap.RemoteBranches)
	}
}
```

NOTE: replace `newSnapshotService`/`mustGit` with the exact helper names already
present in `query_test.go` (e.g. there is almost certainly a helper that returns
a `*Service` plus the repo dir, and a git-exec helper). Do not introduce new
helpers if equivalents exist.

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./internal/domain/ -run TestSnapshotIncludesRemoteBranches -v`
Expected: FAIL — `snap.RemoteBranches` undefined.

- [ ] **Step 3: Add the Snapshot field**

In `internal/domain/query.go`, add to the `Snapshot` struct (after `Worktrees`, ~line 19):

```go
	RemoteBranches  []model.RemoteBranch
```

- [ ] **Step 4: Load it in `loadSnapshot`**

In `loadSnapshot` (~line 53), add a `run(...)` block alongside the existing
`Branches` one. Make it **best-effort** (a repo with no remotes must not fail
startup): on error, leave the field nil, do NOT call `fatal`.

```go
	run(func() {
		if rbs, err := s.repo.RemoteBranches(ctx); err == nil {
			mu.Lock()
			snap.RemoteBranches = rbs
			mu.Unlock()
		}
	})
```

- [ ] **Step 5: Run the test to confirm green**

Run: `go test ./internal/domain/ -run TestSnapshotIncludesRemoteBranches -v`
Expected: PASS.

- [ ] **Step 6: Run the domain package**

Run: `go test ./internal/domain/`
Expected: ok (no regressions).

- [ ] **Step 7: Commit**

```bash
git add internal/domain/query.go internal/domain/query_test.go
git commit -m "feat(domain): include RemoteBranches in Snapshot (best-effort)"
```

---

### Task 3: TUI plumbing — `panelRemotes` enum, model field, load apply, 3-way tab cycle

**Files:**
- Modify: `internal/tui/model.go` (const block ~line 100; `dataLoadedMsg` apply ~line 222; tab-cycle handler ~line 619)
- Modify: `internal/tui/load.go` (`dataLoadedMsg` struct ~line 34; `out :=` builder ~line 76)
- Test: `internal/tui/nav_test.go` (append cycle test)

**Interfaces:**
- Consumes: `domain.Snapshot.RemoteBranches` (Task 2)
- Produces: `panel` constant `panelRemotes`; `Model.remoteBranches []model.RemoteBranch`; package var `leftTabs []panel`

- [ ] **Step 1: Write the failing cycle test**

Append to `internal/tui/nav_test.go`:

```go
func TestCtrlRightCyclesBranchesRemotesWorktrees(t *testing.T) {
	m := newTestModel(t) // use the helper the other nav tests use
	m.activeLeftTab = panelBranches
	m.focus = panelBranches

	step := func() panel {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{}, Alt: false}) // placeholder
		_ = u
		return m.activeLeftTab
	}
	_ = step

	// ctrl+right: Branches -> Remotes -> Worktrees -> Branches
	send := func(k string) {
		u, _ := m.Update(keyMsg(k)) // use this file's existing key-injection helper
		m = u.(Model)
	}
	send("ctrl+right")
	if m.activeLeftTab != panelRemotes || m.focus != panelRemotes {
		t.Fatalf("after 1x ctrl+right: tab=%v focus=%v, want Remotes", m.activeLeftTab, m.focus)
	}
	send("ctrl+right")
	if m.activeLeftTab != panelWorktrees {
		t.Fatalf("after 2x ctrl+right: tab=%v, want Worktrees", m.activeLeftTab)
	}
	send("ctrl+right")
	if m.activeLeftTab != panelBranches {
		t.Fatalf("after 3x ctrl+right: tab=%v, want Branches (wrap)", m.activeLeftTab)
	}
	// ctrl+left goes back one
	send("ctrl+left")
	if m.activeLeftTab != panelWorktrees {
		t.Fatalf("after ctrl+left: tab=%v, want Worktrees", m.activeLeftTab)
	}
}
```

NOTE: delete the `step`/placeholder scaffolding above and use the EXACT key
helper the neighboring nav tests use (look at `TestTabCyclesActiveTabStatusCommits`
in this file for how it constructs a `tea.KeyMsg` for a named key like
`ctrl+right`, and the model constructor it uses). The asserted behavior is what
matters: 3-way wrap forward, one-step back.

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./internal/tui/ -run TestCtrlRightCyclesBranchesRemotesWorktrees -v`
Expected: FAIL — `panelRemotes` undefined (and the toggle only flips two tabs).

- [ ] **Step 3: Add the enum member**

In `internal/tui/model.go`, insert `panelRemotes` after `panelWorktrees` in the const block (~line 100):

```go
const (
	panelBranches panel = iota
	panelWorktrees
	panelRemotes
	panelFiles
	panelStaged
	panelCommits
	panelCount
)
```

Then GREP for any arithmetic/range over panel constants that assumes the old
ordering (e.g. `panelFiles == panelWorktrees+1`, loops `for p := panelBranches;
p < panelCount; p++` that index fixed slices). Run:
`grep -rn 'panelWorktrees+1\|panelFiles - 1\|panel(int' internal/tui/*.go`.
Fix any found so they remain correct with the inserted member.

- [ ] **Step 4: Add the model field + tab-order var**

In `internal/tui/model.go`, add to the `Model` struct (near `activeLeftTab`):

```go
	remoteBranches []model.RemoteBranch
```

Add a package-level var (near the const block):

```go
// leftTabs is the display order of the shared left-slot tabs; the ctrl+←/→
// cycle walks this list (enum value order is unrelated).
var leftTabs = []panel{panelBranches, panelRemotes, panelWorktrees}
```

Ensure `model` is imported in `model.go` (it already imports
`github.com/gigagit/gg/internal/model` — confirm).

- [ ] **Step 5: Rework the tab-cycle handler**

Replace the `case "ctrl+left", "ctrl+right":` block (~line 619) with a
directional walk over `leftTabs`:

```go
		case "ctrl+left", "ctrl+right":
			// Walk the shared-slot tab order; wrap. Switch and focus it.
			cur := 0
			for i, p := range leftTabs {
				if p == m.activeLeftTab {
					cur = i
					break
				}
			}
			if msg.String() == "ctrl+right" {
				cur = (cur + 1) % len(leftTabs)
			} else {
				cur = (cur - 1 + len(leftTabs)) % len(leftTabs)
			}
			m.activeLeftTab = leftTabs[cur]
			m.focus = m.activeLeftTab
			m.lastLeftPanel = m.activeLeftTab
			return m, nil
```

(Use whatever this handler already uses to read the key string — the existing
code is inside a `switch` on the key; `msg.String()` is the typical accessor.
Match the surrounding style.)

- [ ] **Step 6: Carry RemoteBranches through `dataLoadedMsg`**

In `internal/tui/load.go`, add to the `dataLoadedMsg` struct (~line 34):

```go
	remoteBranches []model.RemoteBranch
```

And to the `out := dataLoadedMsg{...}` builder (~line 76):

```go
		remoteBranches:   snap.RemoteBranches,
```

In `internal/tui/model.go`, in the `dataLoadedMsg` apply (the `if msg.err == nil`
block ~line 222, next to `m.branches = msg.branches`):

```go
			m.remoteBranches = msg.remoteBranches
```

- [ ] **Step 7: Run the cycle test + tui build**

Run: `go test ./internal/tui/ -run TestCtrlRightCyclesBranchesRemotesWorktrees -v`
Expected: PASS.
Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/load.go internal/tui/nav_test.go
git commit -m "feat(tui): panelRemotes enum, model field, 3-way ctrl+arrow tab cycle"
```

---

### Task 4: TUI render — Remotes list, 3-way tab label, behind-indicator

**Files:**
- Modify: `internal/tui/viewstate.go` (`listFor` ~line 237; add `remoteBranchList`)
- Modify: `internal/tui/view.go` (`remoteRows` new; `tabBarLabel` ~line 384; `branchRows` ~line 581)
- Test: `internal/tui/fit_test.go` (append render + label + indicator tests)

**Interfaces:**
- Consumes: `Model.remoteBranches`, `panelRemotes`, `leftTabs` (Task 3)
- Produces: `remoteBranchList` (implements `panelList`); `(Model).remoteRows() []string`

- [ ] **Step 1: Write the failing render/label/indicator tests**

Append to `internal/tui/fit_test.go`:

```go
func TestRemoteRowsContent(t *testing.T) {
	m := newTestModel(t)
	m.remoteBranches = []model.RemoteBranch{
		{Name: "origin/main", Remote: "origin", Branch: "main"},
		{Name: "origin/feature/x", Remote: "origin", Branch: "feature/x"},
	}
	rows := m.remoteRows()
	if len(rows) != 2 || !strings.Contains(rows[0], "origin/main") || !strings.Contains(rows[1], "origin/feature/x") {
		t.Fatalf("remoteRows = %#v", rows)
	}
}

func TestTabBarLabelThreeWay(t *testing.T) {
	if got := tabBarLabel(panelRemotes); !strings.Contains(got, "[Remotes]") {
		t.Fatalf("active Remotes: %q", got)
	}
	if got := tabBarLabel(panelBranches); !strings.Contains(got, "[Branches]") || !strings.Contains(got, "Remotes") {
		t.Fatalf("active Branches: %q", got)
	}
	if got := tabBarLabel(panelWorktrees); !strings.Contains(got, "[Worktrees]") {
		t.Fatalf("active Worktrees: %q", got)
	}
}

func TestBranchRowsBehindIndicator(t *testing.T) {
	m := newTestModel(t)
	m.branches = []model.Branch{
		{Name: "feature", Behind: 3},
		{Name: "clean", Behind: 0},
	}
	rows := m.branchRows()
	if !strings.Contains(rows[0], "(↓3)") {
		t.Fatalf("behind row missing indicator: %q", rows[0])
	}
	if strings.Contains(rows[1], "↓") {
		t.Fatalf("clean row should have no indicator: %q", rows[1])
	}
}
```

NOTE: use the real model constructor helper from this test file (`newTestModel`
is illustrative). Ensure `strings` and `model` are imported in the test file.

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/tui/ -run 'TestRemoteRowsContent|TestTabBarLabelThreeWay|TestBranchRowsBehindIndicator' -v`
Expected: FAIL — `remoteRows` undefined; `tabBarLabel` only handles two tabs; no behind indicator.

- [ ] **Step 3: Add `remoteBranchList` + `listFor` case**

In `internal/tui/viewstate.go`, after the `branchList` block (~line 155), add:

```go
type remoteBranchList struct {
	items []model.RemoteBranch
	rows  []string
}

func (l remoteBranchList) Len() int          { return len(l.items) }
func (l remoteBranchList) Row(i int) string  { return l.rows[i] }
func (l remoteBranchList) Name(i int) string { return l.items[i].Name }
func (l remoteBranchList) Date(i int) int64  { return l.items[i].UnixTime }
func (l remoteBranchList) Key(i int) string  { return l.items[i].Name }
```

(Match the EXACT method set of `panelList` as implemented by `branchList` — if
`branchList` has methods beyond these, mirror them.)

In `listFor` (~line 237), add a case alongside `panelBranches`:

```go
	case panelRemotes:
		return remoteBranchList{items: m.remoteBranches, rows: m.remoteRows()}
```

- [ ] **Step 4: Add `remoteRows` + 3-way `tabBarLabel` + behind-indicator**

In `internal/tui/view.go`, add `remoteRows` next to `branchRows` (~line 596):

```go
func (m Model) remoteRows() []string {
	out := make([]string, 0, len(m.remoteBranches))
	for _, rb := range m.remoteBranches {
		out = append(out, "  "+rb.Name)
	}
	return out
}
```

Replace `tabBarLabel` (~line 384) with a 3-way version:

```go
// tabBarLabel is the shared left-slot header: the three tab names with the
// active one bracketed. Plain ASCII so renderPanel's truncate stays safe.
func tabBarLabel(active panel) string {
	name := func(p panel, s string) string {
		if p == active {
			return "[" + s + "]"
		}
		return s
	}
	return name(panelBranches, "Branches") + " " +
		name(panelRemotes, "Remotes") + " " +
		name(panelWorktrees, "Worktrees")
}
```

In `branchRows` (~line 581), after the worktree-marker line and before
`out = append(...)`, add the behind-indicator. Confirm `strconv` is imported in
`view.go` (it is — used by `filesLabel`):

```go
		if b.Behind > 0 {
			row += " (↓" + strconv.Itoa(b.Behind) + ")"
		}
```

- [ ] **Step 5: Run the render tests to confirm green**

Run: `go test ./internal/tui/ -run 'TestRemoteRowsContent|TestTabBarLabelThreeWay|TestBranchRowsBehindIndicator' -v`
Expected: PASS.

- [ ] **Step 6: Run the full tui package**

Run: `go test ./internal/tui/`
Expected: ok. If `TestRenderShowsActiveTabBar` (fit_test.go) or other render
golden tests assert the OLD two-tab label string, update those expectations to
the new 3-way label — they are testing the same surface this task changed.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/viewstate.go internal/tui/view.go internal/tui/fit_test.go
git commit -m "feat(tui): render Remotes tab, 3-way tab label, branch behind-indicator"
```

---

### Task 5: Help/footer copy, focus-test sweep, docs

**Files:**
- Modify: `internal/tui/help.go` (the `?` pane — any text enumerating the two tabs)
- Modify: `internal/tui/footer.go` (only if footer copy names the tabs)
- Modify: `internal/tui/nav_test.go` / `pgnav_test.go` / `focus_test.go` (update any assertion that hard-codes the two-tab toggle)
- Modify: `CHANGELOG.md`
- Modify: `README.md` (only if it describes the Branches/Worktrees tabs)

**Interfaces:** none (copy + docs).

- [ ] **Step 1: Update help copy**

Run `grep -rn 'Branches/Worktrees\|Worktrees tab\|ctrl+←\|ctrl+→\|tab slot' internal/tui/help.go internal/tui/footer.go`.
Update any text that enumerates "Branches/Worktrees" to "Branches/Remotes/Worktrees".
The `ctrl+←/→` hint already exists; ensure its description says it cycles the
three tabs. Do NOT add a new keybinding row (none was added).

- [ ] **Step 2: Sweep focus/nav tests**

Run: `go test ./internal/tui/`
For any failure in `nav_test.go`, `pgnav_test.go`, or `focus_test.go` caused by
the third tab (e.g. a test that asserts `ctrl+right` from Branches lands on
Worktrees), update the expectation to the new 3-way order (Branches → Remotes →
Worktrees). Keep each assertion's intent; only fix the expected tab.

- [ ] **Step 3: Run the full unit + race gate**

Run: `./test.sh unit`
Expected: all green.
Run: `./test.sh race`
Expected: all green (run before declaring done).

- [ ] **Step 4: Update CHANGELOG and README**

Add a CHANGELOG entry under the current unreleased/working section, e.g.:

```markdown
- TUI: new **Remotes** tab (third tab in the Branches/Worktrees slot) listing
  remote-tracking branches; `ctrl+←/→` now cycles Branches · Remotes · Worktrees.
  Local Branches rows show a `(↓N)` indicator when behind their upstream.
```

If `README.md` describes the left tab slot as "Branches/Worktrees", update it to
include Remotes. (Match the CHANGELOG's existing heading style — read the top of
the file first.)

- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go internal/tui/footer.go internal/tui/*_test.go CHANGELOG.md README.md
git commit -m "docs(tui): advertise Remotes tab in help/footer/changelog; focus-test sweep"
```

---

## Self-Review notes

- **Spec coverage:** Remotes tab list (Tasks 1–4), behind-indicator (Task 4),
  3-way tab order (Tasks 3–4), no streaming loader (one `for-each-ref` in Task 1
  + existing windowing), `origin/HEAD` filtered (Task 1 parser). All covered.
- **Deferred items** (`c`/`s`, preview feed, fetch/prune) are explicitly NOT in
  any task — correct per the chunk boundary.
- **Type consistency:** `model.RemoteBranch{Name,Remote,Branch,Hash,UnixTime}`
  is defined in Task 1 and consumed verbatim in Tasks 2–4. `remoteRows`,
  `remoteBranchList`, `panelRemotes`, `leftTabs` are each defined once and
  referenced consistently.
- **Watch-item:** Tasks 3–4 carry explicit grep steps for panel-enum arithmetic
  and golden tab-label assertions, the two likely regression sources.
