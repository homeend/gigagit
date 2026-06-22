# Reflog Window Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only HEAD-reflog viewer as a bottom-left TUI tab sharing the Staged slot, with `enter`/`l` opening the entry's commit files-view and a `.` menu offering Copy SHA + Bookmark.

**Architecture:** A new `git.ReflogEntries` verb feeds a `domain.Service.Reflog` read query (joined to the startup `Snapshot`, re-read after every op). The TUI gains a `panelReflog` constant; the bottom-left slot becomes a tab group `{Staged, Reflog}` toggled with `ctrl+←/→`, exactly like the existing `{Files, Tags}` middle slot. Row actions reuse the commit files-view (`loadCommitFilesCmd`) and the commit-bookmark path (`commitBookmark`/`bookmarkAddCmd`).

**Tech Stack:** Go 1.26, Bubble Tea TUI, shells out to system `git` via `gitcmd`/`gitexec`.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26.
- A git verb is ONE invocation built with `gitcmd` and run via `r.Runner.Run`; never shell out directly.
- Frontends (`internal/tui`) never import `internal/git`; they reach git through `internal/domain` (archtest-guarded).
- TUI `Model` is a value receiver; map fields (`m.sel`, etc.) are reference types and persist across copies; plain slice/scalar fields must be reassigned to persist.
- Tests use a real `git` in a `t.TempDir()` (`newTestRepo`/`newRepo` helpers) or `FakeRunner` for argv assertions. Follow TDD.
- `main` is the trunk; branch features off `main`. Run `./test.sh race` before merge.
- Every commit ends with these two trailers verbatim:
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro
  ```
- New `[ui]` config field follows the existing `<=0 = unset` overlay pattern (see `SearchHistorySize`).
- Reflog is HEAD-only, read-only, capped by `[ui] reflog_limit` (default 200). No reset/checkout/CLI/paging in this feature.

---

### Task 1: `model.ReflogEntry` + `git.ReflogEntries` verb

**Files:**
- Modify: `internal/model/model.go` (add `ReflogEntry` type)
- Modify: `internal/git/reflog.go` (add `ReflogEntries`)
- Test: `internal/git/reflog_test.go` (add cases)

**Interfaces:**
- Produces: `model.ReflogEntry{Selector, Hash, ShortHash, Subject, Rel string}`; `func (r *Repo) ReflogEntries(ctx context.Context, limit int) ([]model.ReflogEntry, error)`.

- [ ] **Step 1: Add the model type**

In `internal/model/model.go`, after the `Commit`/`Ref` block, add:

```go
// ReflogEntry is one HEAD reflog record (git reflog), newest first.
type ReflogEntry struct {
	Selector  string // "HEAD@{0}"
	Hash      string // full SHA
	ShortHash string // abbreviated SHA
	Subject   string // %gs, e.g. "commit: add foo" or "checkout: moving from main to dev"
	Rel       string // %gr relative date, e.g. "2 hours ago"
}
```

- [ ] **Step 2: Write the failing verb test**

Add to `internal/git/reflog_test.go`:

```go
func TestReflogEntriesListsHeadActions(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	// Two more HEAD-moving actions on top of the initial commit.
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "second commit")
	gitIn(t, dir, "checkout", "-b", "feature")

	entries, err := repo.ReflogEntries(context.Background(), 50)
	if err != nil {
		t.Fatalf("reflog entries: %v", err)
	}
	if len(entries) < 3 {
		t.Fatalf("want >=3 entries, got %d: %+v", len(entries), entries)
	}
	top := entries[0]
	if top.Selector != "HEAD@{0}" {
		t.Fatalf("top selector = %q, want HEAD@{0}", top.Selector)
	}
	if len(top.Hash) != 40 {
		t.Fatalf("top hash = %q, want a full 40-char SHA", top.Hash)
	}
	if top.ShortHash == "" || top.Subject == "" {
		t.Fatalf("top entry missing short hash or subject: %+v", top)
	}
	// The most recent action was the checkout.
	if !strings.Contains(top.Subject, "checkout") {
		t.Fatalf("top subject = %q, want it to mention checkout", top.Subject)
	}
}

func TestReflogEntriesRespectsLimit(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	for i := 0; i < 4; i++ {
		gitIn(t, dir, "commit", "--allow-empty", "-m", "c")
	}
	entries, err := repo.ReflogEntries(context.Background(), 2)
	if err != nil {
		t.Fatalf("reflog entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want exactly 2 entries under limit, got %d", len(entries))
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/git/ -run TestReflogEntries -v`
Expected: FAIL — `repo.ReflogEntries undefined`.

- [ ] **Step 4: Implement the verb**

In `internal/git/reflog.go`, add the import for `model` and the function. Top of file becomes:

```go
package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/model"
)
```

Add:

```go
// reflogFmt joins fields with NUL and records with newline. %gs (the reflog
// subject) is single-line, so newline record splitting is safe.
const reflogFmt = "%H%x00%h%x00%gd%x00%gs%x00%gr"

// ReflogEntries returns up to limit HEAD reflog entries, newest first. A repo
// with no reflog yields an empty slice (not an error).
func (r *Repo) ReflogEntries(ctx context.Context, limit int) ([]model.ReflogEntry, error) {
	b := gitcmd.New("reflog").Arg("--format=" + reflogFmt)
	if limit > 0 {
		b = b.Arg("-n", strconv.Itoa(limit))
	}
	res, err := r.Runner.Run(ctx, "git reflog", b.ToArgv())
	if err != nil {
		return nil, err
	}
	var out []model.ReflogEntry
	for _, line := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 5 {
			continue
		}
		out = append(out, model.ReflogEntry{
			Hash:      f[0],
			ShortHash: f[1],
			Selector:  f[2],
			Subject:   f[3],
			Rel:       f[4],
		})
	}
	return out, nil
}
```

Add `"strconv"` to the import block (alphabetical: after `"context"`, before `"strings"`).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/git/ -run TestReflogEntries -v`
Expected: PASS (both cases).

- [ ] **Step 6: Commit**

```bash
git add internal/model/model.go internal/git/reflog.go internal/git/reflog_test.go
git commit -m "feat(git): ReflogEntries verb + model.ReflogEntry

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: `[ui] reflog_limit` config + `domain.Service.Reflog` + Snapshot wiring

**Files:**
- Modify: `internal/config/config.go` (add `ReflogLimit` field + overlay)
- Modify: `internal/domain/query.go` (add `Reflog` query, `Snapshot.Reflog` field, `loadSnapshot` read)
- Modify: `internal/engine/gitops.go` (add `ReflogEntries` to the `GitOps`/repo interface IF domain calls through it — see Step 3)
- Test: `internal/config/config_test.go`, `internal/domain/query_test.go`

**Interfaces:**
- Consumes: `git.(*Repo).ReflogEntries(ctx, limit)` from Task 1; `config.UIConfig`.
- Produces: `config.UIConfig.ReflogLimit int`; `domain.Snapshot.Reflog []model.ReflogEntry`; `func (s *Service) Reflog(ctx context.Context) ([]model.ReflogEntry, error)`; constant default limit `200`.

- [ ] **Step 1: Add the config field + overlay (failing test)**

Add to `internal/config/config_test.go` (mirror an existing overlay test such as the `SearchHistorySize` one):

```go
func TestReflogLimitOverlay(t *testing.T) {
	dst := Defaults()
	src := Config{UI: UIConfig{ReflogLimit: 42}}
	overlay(&dst, src)
	if dst.UI.ReflogLimit != 42 {
		t.Fatalf("ReflogLimit = %d, want 42", dst.UI.ReflogLimit)
	}
}
```

(If the overlay helper has a different name than `overlay`, match the existing test file's call; the assertion stands.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/config/ -run TestReflogLimitOverlay -v`
Expected: FAIL — `ReflogLimit` unknown field.

- [ ] **Step 3: Add the field + overlay clause**

In `internal/config/config.go`, in `UIConfig` after `SearchHistorySize`:

```go
	ReflogLimit int `toml:"reflog_limit"` // max HEAD reflog entries shown in the Reflog panel; <=0 = unset (default 200)
```

In the overlay function (where `SearchHistorySize` is copied, ~line 122):

```go
	if src.ReflogLimit > 0 {
		dst.ReflogLimit = src.ReflogLimit
	}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/config/ -run TestReflogLimitOverlay -v`
Expected: PASS.

- [ ] **Step 5: Add the domain query (failing test)**

First check how domain reaches git verbs: `domain.Service` holds a `repo` whose type exposes git verbs. Confirm the interface that `s.repo` satisfies and whether new verbs must be declared there:

Run: `grep -n "s.repo.Branches\|type .* interface" internal/domain/query.go internal/domain/service.go`

If `s.repo` is `*git.Repo` directly, no interface edit is needed. If it is an interface, add `ReflogEntries(ctx context.Context, limit int) ([]model.ReflogEntry, error)` to that interface.

Add to `internal/domain/query_test.go`:

```go
func TestReflogReturnsEntries(t *testing.T) {
	svc := newServiceForTest(t) // use whatever the file's existing helper is
	// make a second commit so the reflog has >1 entry
	// (use the same repo-setup helper the other query tests use)
	entries, err := svc.Reflog(context.Background())
	if err != nil {
		t.Fatalf("Reflog: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("want at least one reflog entry")
	}
}
```

Adapt `newServiceForTest`/repo setup to match the existing helpers in `internal/domain/query_test.go` (read the top of that file first; reuse its constructor exactly).

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./internal/domain/ -run TestReflogReturnsEntries -v`
Expected: FAIL — `svc.Reflog undefined`.

- [ ] **Step 7: Implement the query + Snapshot field**

In `internal/domain/query.go`, add the default constant near the top of the file:

```go
const defaultReflogLimit = 200
```

Add the query (mirror `Branches`):

```go
// Reflog returns the HEAD reflog entries (newest first), bounded by the
// configured [ui] reflog_limit (default 200). Read reservation, singleflighted.
func (s *Service) Reflog(ctx context.Context) ([]model.ReflogEntry, error) {
	return query(ctx, s, "reflog", func(ctx context.Context) ([]model.ReflogEntry, error) {
		return s.repo.ReflogEntries(ctx, s.reflogLimit())
	})
}

// reflogLimit resolves the configured cap, falling back to the default.
func (s *Service) reflogLimit() int {
	if n := s.cfg.UI.ReflogLimit; n > 0 {
		return n
	}
	return defaultReflogLimit
}
```

Check how `Service` accesses config: `grep -n "cfg\b" internal/domain/service.go`. If `Service` does not already hold a `config.Config`, do NOT add one in this task — instead make `reflogLimit()` return `defaultReflogLimit` unconditionally and drop the `s.cfg` reference (the TUI already clamps via its own cfg when it calls the verb path; the domain default is the floor). Pick whichever matches the existing field; the test only requires non-empty results.

Add the Snapshot field in the `Snapshot` struct (after `Tags`):

```go
	Reflog          []model.ReflogEntry
```

Add a best-effort read in `loadSnapshot` (mirror the `Tags` best-effort block):

```go
	run(func() {
		// Reflog is best-effort: a repo with no reflog must not block startup.
		if rl, err := s.repo.ReflogEntries(ctx, s.reflogLimit()); err == nil {
			mu.Lock()
			snap.Reflog = rl
			mu.Unlock()
		}
	})
```

(If you took the no-`s.cfg` branch above, call `s.repo.ReflogEntries(ctx, defaultReflogLimit)` here instead.)

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/domain/ -run TestReflog -v && go test ./internal/config/ -run TestReflogLimit -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/config/ internal/domain/ internal/engine/gitops.go
git commit -m "feat(domain): Reflog query + Snapshot field + reflog_limit config

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

(Drop `internal/engine/gitops.go` from the `git add` if Step 5 found `s.repo` is `*git.Repo` and no interface change was made.)

---

### Task 3: Reflog panel — constant, bottom tab group, list, layout, render, focus, refresh

**Files:**
- Modify: `internal/tui/model.go` (`panelReflog` const, `bottomTabs`, `bottomTab()`, `activeBottomTab` field, `m.reflog` field, `leftColumnPanels`, `focusOrder`, `ctrl+left/right`)
- Modify: `internal/tui/viewstate.go` (`listFor`, `layout` bottom-slot keying)
- Create: `internal/tui/reflog_view.go` (`reflogList` type + `reflogRows`)
- Modify: `internal/tui/view.go` (`leftPanelLabel` reflog case + `bottomTabLabel`)
- Modify: `internal/tui/load.go` (carry `snap.Reflog` into the model)
- Modify: `internal/tui/model.go` opFinishedMsg path (refresh reflog after ops)
- Test: `internal/tui/reflog_view_test.go` (new)

**Interfaces:**
- Consumes: `domain.Snapshot.Reflog`, `model.ReflogEntry`, `panelList`, `leftColumnPanels`, `nextInOrder`, `displayIndices`, `renderPanel`.
- Produces: `panelReflog panel`; `var bottomTabs = []panel{panelStaged, panelReflog}`; `func (m Model) bottomTab() panel`; `Model.activeBottomTab panel`; `Model.reflog []model.ReflogEntry`; `reflogList` (implements `panelList`); `func (m Model) reflogRows() []string`.

- [ ] **Step 1: Write the failing panel-toggle test**

Create `internal/tui/reflog_view_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func reflogTestModel() Model {
	m := Model{
		sel:       map[panel]int{},
		width:     120,
		height:    40,
		sortModes: map[panel]sortMode{},
		reflog: []model.ReflogEntry{
			{Selector: "HEAD@{0}", Hash: "1111111111111111111111111111111111111111", ShortHash: "1111111", Subject: "checkout: moving from main to feature", Rel: "1 minute ago"},
			{Selector: "HEAD@{1}", Hash: "2222222222222222222222222222222222222222", ShortHash: "2222222", Subject: "commit: second", Rel: "2 minutes ago"},
		},
	}
	return m
}

func TestReflogListLenAndRows(t *testing.T) {
	m := reflogTestModel()
	if m.panelLen(panelReflog) != 2 {
		t.Fatalf("panelLen(reflog) = %d, want 2", m.panelLen(panelReflog))
	}
	rows := m.reflogRows()
	if len(rows) != 2 {
		t.Fatalf("reflogRows = %d, want 2", len(rows))
	}
	if !contains(rows[0], "1111111") || !contains(rows[0], "checkout") {
		t.Fatalf("row 0 = %q, want short hash + subject", rows[0])
	}
}

func TestBottomTabTogglesStagedReflog(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelStaged
	nm, _ := m.Update(keyMsg("ctrl+right"))
	m = nm.(Model)
	if m.focus != panelReflog || m.bottomTab() != panelReflog {
		t.Fatalf("after ctrl+right: focus=%v bottomTab=%v, want panelReflog", m.focus, m.bottomTab())
	}
	nm, _ = m.Update(keyMsg("ctrl+left"))
	m = nm.(Model)
	if m.focus != panelStaged || m.bottomTab() != panelStaged {
		t.Fatalf("after ctrl+left: focus=%v bottomTab=%v, want panelStaged", m.focus, m.bottomTab())
	}
}
```

Use the test file's existing `contains` helper if one exists; otherwise use `strings.Contains` and import `strings`. (Check with `grep -rn "func contains(" internal/tui/`.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run "TestReflogList|TestBottomTab" -v`
Expected: FAIL — `panelReflog`/`bottomTab`/`reflog` undefined (compile error).

- [ ] **Step 3: Add the panel constant + tab group + field**

In `internal/tui/model.go`, in the `panel` const block, add `panelReflog` immediately BEFORE `panelCount` (do not renumber existing values):

```go
	panelCommits
	panelTags
	panelReflog
	panelCount
)
```

Below `filesTabs`, add:

```go
// bottomTabs is the display/cycle order of the bottom-left slot tabs (the Staged
// box shares its slot with the Reflog viewer).
var bottomTabs = []panel{panelStaged, panelReflog}
```

In the `Model` struct, next to `activeFilesTab`, add:

```go
	activeBottomTab panel // Staged or Reflog in the bottom-left slot; zero value resolves to panelStaged via bottomTab()
```

Add the `m.reflog` field next to where `m.tags`/`m.branches` are declared:

```go
	reflog []model.ReflogEntry
```

Add the resolver next to `middleTab()`:

```go
// bottomTab is the active bottom-left slot panel, defaulting to Staged when unset.
func (m Model) bottomTab() panel {
	if m.activeBottomTab == panelStaged || m.activeBottomTab == panelReflog {
		return m.activeBottomTab
	}
	return panelStaged
}
```

- [ ] **Step 4: Route the bottom slot through `bottomTab()` in membership, layout, focus**

In `internal/tui/model.go` `leftColumnPanels()`, replace the `panelStaged` append:

```go
	ps := []panel{m.activeLeftTab, m.middleTab()}
	bodyH := m.height - 3
	if bodyH < 6 {
		bodyH = 6
	}
	if bodyH >= 12 {
		ps = append(ps, m.bottomTab())
	}
	return ps
```

In `focusOrder()`, replace the Staged branch:

```go
	order := []panel{m.activeLeftTab, m.middleTab()}
	if slices.Contains(m.leftColumnPanels(), m.bottomTab()) { // dropped on a short terminal
		order = append(order, m.bottomTab())
	}
	return append(order, panelCommits)
```

In `internal/tui/viewstate.go` `layout()`, change the two `panelStaged` box-keys to the active bottom tab. Add `bt := m.bottomTab()` next to `mid := m.middleTab()`, then in the `bodyH >= 12` branch:

```go
		g.boxH[bt] = bodyH - h1 - h2
		g.pos[bt] = point{0, 1 + h1 + h2}
```

(Leave the `bodyH < 12` branch unchanged — the bottom slot is already absent there.)

In the maximize block of `layout()` and in `canMaximizeLeft`/the `ctrl+left/right` no-op guard, any literal `panelStaged` that means "the bottom slot" must become `m.bottomTab()`. Search and update: `grep -n "panelStaged" internal/tui/viewstate.go internal/tui/model.go` and change only the bottom-slot-as-layout-position uses (NOT `memberOf`/`listFor`/`isFilesPanel`, which are genuinely about the Staged file list).

- [ ] **Step 5: Add the `reflogList` panelList + `reflogRows`**

Create `internal/tui/reflog_view.go`:

```go
package tui

import "github.com/gigagit/gg/internal/model"

// reflogRows renders the HEAD reflog entries for the panel body.
func (m Model) reflogRows() []string {
	rows := make([]string, len(m.reflog))
	for i, e := range m.reflog {
		rows[i] = e.ShortHash + "  " + e.Subject + "  (" + e.Rel + ")"
	}
	return rows
}

// reflogList adapts the reflog entries to the panelList contract.
type reflogList struct {
	items []model.ReflogEntry
	rows  []string
}

func (l reflogList) Len() int          { return len(l.items) }
func (l reflogList) Row(i int) string  { return l.rows[i] }
func (l reflogList) Name(i int) string { return l.items[i].Subject }
func (l reflogList) Date(i int) int64  { return 0 } // git default order is newest-first; no per-entry timestamp
func (l reflogList) Key(i int) string  { return l.items[i].Selector }

// Haystack lets the filter match the full SHA and selector, not just the row.
func (l reflogList) Haystack(i int) string {
	e := l.items[i]
	return e.Hash + " " + e.Selector + " " + e.Subject
}
```

In `internal/tui/viewstate.go` `listFor`, add a case before the `default`:

```go
	case panelReflog:
		return reflogList{items: m.reflog, rows: m.reflogRows()}
```

- [ ] **Step 6: Render the bottom tab label + body**

In `internal/tui/view.go` `leftPanelLabel`, change the Staged case to handle the bottom tab group:

```go
	case panelStaged, panelReflog:
		return m.panelLabel(p, bottomTabLabel(p, m.panelLen(panelStaged), m.panelLen(panelReflog)))
```

Add (mirroring `filesTabLabel`):

```go
// bottomTabLabel is the bottom-left slot header: the active tab bracketed with
// its count, the inactive tab shown plainly, so Staged ⇄ Reflog reads like the
// Files ⇄ Tags bar above it.
func bottomTabLabel(active panel, stagedN, reflogN int) string {
	staged := fmt.Sprintf("Staged %d", stagedN)
	reflog := fmt.Sprintf("Reflog %d", reflogN)
	if active == panelReflog {
		return staged + " [" + reflog + "]"
	}
	return "[" + staged + "] " + reflog
}
```

The render loop at `view.go:355` already walks `leftColumnPanels()` (now returning `bottomTab()`) and calls `panelView(p)` + `leftPanelLabel(p)`, so the reflog body renders with no further change. Confirm `panelView`/`memberOf` return all rows for `panelReflog` (the `default: return true` in `memberOf` covers it).

- [ ] **Step 7: Cycle the bottom tab on `ctrl+left/right`**

In `internal/tui/model.go`, in the `ctrl+left`, `ctrl+right` case, after the existing `panelFiles || panelTags` branch, add a bottom-slot branch:

```go
			if m.focus == panelStaged || m.focus == panelReflog {
				m.activeBottomTab = nextInOrder(bottomTabs, m.bottomTab(), dir)
				m.focus = m.activeBottomTab
				if m.leftMaxed { // re-pin the newly shown tab so it stays full-height
					m.leftMax = m.focus
				}
				return m, nil
			}
```

Also update the maximize no-op guard just above it: the comment says "Maximized on Staged: Staged has no tab group" — that is now false. Change the guard so a maximized BOTTOM tab still cycles. Replace:

```go
			if m.leftMaxed && m.leftMax == panelStaged {
				return m, nil
			}
```

with: delete it (the bottom-slot branch below now handles the maximized case by re-pinning). Verify the top-tab maximize case is still guarded by its own logic; if removing this line breaks a maximize test, instead narrow it to `m.leftMaxed && m.leftMax == m.activeLeftTab` only where a hidden tab would be focused — run the focus/maximize tests after this change.

- [ ] **Step 8: Carry `snap.Reflog` into the model on load**

In `internal/tui/load.go`, in the `dataLoadedMsg` literal, add:

```go
			reflog:           snap.Reflog,
```

Add the matching `reflog []model.ReflogEntry` field to the `dataLoadedMsg` struct (find it with `grep -n "type dataLoadedMsg struct" internal/tui/`), and in the handler that applies `dataLoadedMsg` to the model (where `m.tags = msg.tags`, `model.go:397` area) add:

```go
			m.reflog = msg.reflog
```

- [ ] **Step 9: Refresh reflog after ops**

Find the `opFinishedMsg` handler that triggers the post-op `Snapshot` reload (`grep -n "opFinishedMsg" internal/tui/*.go`). The reflog rides the same `Snapshot`, so if the op-finished path already reloads the full snapshot and applies it via `dataLoadedMsg`/snapshot apply, Step 8 already covers refresh — confirm by reading that handler. If the op-finished path uses a narrower refresh that does NOT carry reflog, add `m.reflog = <reloaded>.Reflog` there (or extend that refresh command to fetch `svc.Reflog`). Document in the commit which path you took.

- [ ] **Step 10: Run the panel tests + the focus/layout suite**

Run: `go test ./internal/tui/ -run "TestReflog|TestBottomTab|Focus|Maximize|Layout" -v`
Expected: PASS. Fix any focus/maximize regressions surfaced by the `panelStaged`→`bottomTab()` change before continuing.

- [ ] **Step 11: Full TUI build + vet**

Run: `go build ./... && go vet ./internal/tui/ && go test ./internal/tui/`
Expected: clean build, all green.

- [ ] **Step 12: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): Reflog panel as a bottom-left tab sharing the Staged slot

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 4: `enter`/`l` opens the reflog entry's commit files-view

**Files:**
- Modify: `internal/tui/model.go` (enter + `l` handlers)
- Modify: `internal/tui/avail.go` (gate helper, mirror `canShowCommitFiles`)
- Test: `internal/tui/reflog_view_test.go`

**Interfaces:**
- Consumes: `m.reflog`, `backingIndex(panelReflog)`, `loadCommitFilesCmd`, `m.filesView`/`filesTitle`/`filesHash`.
- Produces: `func (m Model) canShowReflogFiles() bool`; reflog branch in the `enter`/`l` handlers.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/reflog_view_test.go`:

```go
func TestReflogEnterOpensCommitFilesView(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 1 // anchor on the SECOND row, not the default 0
	nm, cmd := m.Update(keyMsg("enter"))
	m = nm.(Model)
	if m.filesView == nil {
		t.Fatal("enter on a reflog row must open the files view")
	}
	if m.filesHash != "2222222222222222222222222222222222222222" {
		t.Fatalf("filesHash = %q, want the SECOND entry's hash (cursor-anchored)", m.filesHash)
	}
	if cmd == nil {
		t.Fatal("expected a files-load command")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestReflogEnterOpens -v`
Expected: FAIL — files view not opened.

- [ ] **Step 3: Add the gate helper**

In `internal/tui/avail.go`, near `canShowCommitFiles`:

```go
// canShowReflogFiles gates l/enter on a reflog row: a resolvable entry under the
// cursor and a wide-enough terminal. Anchors on panelReflog selection only.
func (m Model) canShowReflogFiles() bool {
	if m.focus != panelReflog {
		return false
	}
	if m.width > 0 && m.width < 40 {
		return false
	}
	_, ok := m.backingIndex(panelReflog)
	return ok
}
```

- [ ] **Step 4: Add a shared open helper + wire enter and l**

In `internal/tui/reflog_view.go`, add:

```go
// openReflogFiles opens the commit files-view for the reflog row under the
// cursor, reusing the commit files-view path with a synthesized model.Commit.
func (m Model) openReflogFiles() (Model, tea.Cmd) {
	bi, ok := m.backingIndex(panelReflog)
	if !ok {
		return m, nil
	}
	e := m.reflog[bi]
	c := model.Commit{Hash: e.Hash, Subject: e.Subject}
	m.filesView = &contentPopup{lines: []contentLine{{text: "(loading…)"}}}
	m.filesTitle = "Files " + shortHash(c.Hash) + " " + c.Subject
	m.filesHash = c.Hash
	m.filesTreeFocused = false
	m.filesAllFiles = false
	m.filesPreview = nil
	m.filesPreviewTag = ""
	m.filesReadInflight = true
	return m, m.loadCommitFilesCmd(c)
}
```

Add the `tea` import to `reflog_view.go`: `tea "github.com/charmbracelet/bubbletea"`.

In `internal/tui/model.go`, in the `enter` case after the existing commit/`canShowFileDiff` branches, add:

```go
			if m.canShowReflogFiles() {
				return m.openReflogFiles()
			}
```

In the `l` case, after the `panelCommits` block, add:

```go
			if m.canShowReflogFiles() {
				return m.openReflogFiles()
			}
```

- [ ] **Step 5: Run it to verify it passes**

Run: `go test ./internal/tui/ -run TestReflogEnterOpens -v`
Expected: PASS.

- [ ] **Step 6: Empirically verify dangling-commit diff (build-time check)**

The reflog can point at a commit not on any branch. Confirm `loadCommitFilesCmd` resolves such a SHA. Quick manual check in a scratch repo:

```bash
cd "$(mktemp -d)" && git init -q && git commit -q --allow-empty -m a && \
  git commit -q --allow-empty -m b && git reset -q --hard HEAD~1 && \
  DANGLING=$(git reflog --format=%H | head -1) && git show --stat "$DANGLING" | head -3
```
Expected: `git show` prints the dangling commit's stat (proves the SHA resolves). If it resolves here, `loadCommitFilesCmd` (which runs the same kind of show/diff-tree) will too. Note the result in the commit message.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): enter/l on a reflog row opens the commit files view

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 5: `.` menu — reflog-anchored Copy SHA + Bookmark

**Files:**
- Modify: `internal/tui/action_menu.go` (`contextCopyRows` reflog case)
- Modify: `internal/tui/bookmark.go` (`reflogBookmarkRow`)
- Modify: `internal/tui/action_menu.go` `availableActions` (append `reflogBookmarkRow`)
- Test: `internal/tui/reflog_view_test.go`

**Interfaces:**
- Consumes: `m.reflog`, `backingIndex(panelReflog)`, `copyRow`, `commitBookmark`, `bookmarkAddCmd`, `actionRow`.
- Produces: reflog case in `contextCopyRows`; `func (m Model) reflogBookmarkRow() (actionRow, bool)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/reflog_view_test.go`:

```go
func TestReflogMenuCopyAndBookmark(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 0
	rows := m.contextCopyRows()
	var sawCopy bool
	for _, r := range rows {
		if r.id == "copy-reflog-sha" {
			sawCopy = true
			if r.copyText != "1111111111111111111111111111111111111111" {
				t.Fatalf("copy text = %q, want the cursor row's full hash", r.copyText)
			}
		}
	}
	if !sawCopy {
		t.Fatal("reflog . menu must offer Copy SHA")
	}
	if _, ok := m.reflogBookmarkRow(); !ok {
		t.Fatal("reflog . menu must offer Bookmark this commit")
	}
}

func TestReflogMenuNoCommitLeak(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 0
	// The commit-panel bookmark row is anchored on panelCommits and must NOT
	// fire while focus is on the reflog panel.
	if _, ok := m.commitBookmarkRow(); ok {
		t.Fatal("commit bookmark row leaked into the reflog panel")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run "TestReflogMenu" -v`
Expected: FAIL — `reflogBookmarkRow` undefined / no copy-reflog-sha row.

- [ ] **Step 3: Add the copy case**

In `internal/tui/action_menu.go` `contextCopyRows`, in the final `switch`, add before the `panelCommits` case:

```go
	case m.focus == panelReflog:
		if bi, ok := m.backingIndex(panelReflog); ok {
			e := m.reflog[bi]
			return []actionRow{
				m.copyRow("copy-reflog-sha", "Copy SHA", "Copied SHA "+shortHash(e.Hash), e.Hash),
			}
		}
```

- [ ] **Step 4: Add the bookmark row**

In `internal/tui/bookmark.go`, after `commitBookmarkRow`:

```go
// reflogBookmarkRow offers a path-less commit bookmark for the reflog entry
// under the cursor. Anchored on panelReflog selection only.
func (m Model) reflogBookmarkRow() (actionRow, bool) {
	if m.focus != panelReflog || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelReflog)
	if !ok {
		return actionRow{}, false
	}
	e := m.reflog[bi]
	b := commitBookmark(model.Commit{Hash: e.Hash, Subject: e.Subject})
	return actionRow{
		id:    "reflog-bookmark",
		label: "Bookmark this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.bookmarkAddCmd(b)
		},
	}, true
}
```

Ensure `bookmark.go` imports `model` and `tea` (it already uses both for `commitBookmarkRow`).

In `internal/tui/action_menu.go` `availableActions`, in the base (non-stack) row-assembly section where `commitBookmarkRow`-style helpers are appended via `appendCommitContextRows`, add the reflog row to the base `out` assembly (next to `bookmarkAddRow`):

```go
	if r, ok := m.reflogBookmarkRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 5: Run it to verify it passes**

Run: `go test ./internal/tui/ -run "TestReflogMenu" -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): reflog . menu offers Copy SHA + Bookmark this commit

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 6: Help text + docs

**Files:**
- Modify: `internal/tui/help.go` (Reflog navigation note)
- Modify: `CHANGELOG.md`
- Modify: `README.md`
- Modify: `CLAUDE.md` (package-map / panel-taxonomy note, only if needed)

**Interfaces:** none (docs only).

- [ ] **Step 1: Add the help entry**

In `internal/tui/help.go`, in the left-column / panel keys section, add a line (match the existing `r()`/`h()` helpers — read the file to copy the exact form):

```go
		r("ctrl+←/→", "switch bottom-left tab: Staged ⇄ Reflog"),
		r("enter / l", "Reflog: open the entry's commit files view"),
```

Place these near the existing Staged-panel entries; use whatever helper signature the surrounding entries use.

- [ ] **Step 2: Update CHANGELOG**

In `CHANGELOG.md`, under the Unreleased section, add:

```markdown
- **Reflog window.** The bottom-left panel is now a tab group: `ctrl+←/→`
  toggles Staged ⇄ Reflog. The Reflog tab lists the HEAD reflog (read-only,
  capped by `[ui] reflog_limit`, default 200); `enter`/`l` opens an entry's
  commit files view, and the `.` menu offers Copy SHA and Bookmark this commit.
```

- [ ] **Step 3: Update README**

In `README.md`, in the TUI panels/keys section, add a short Reflog bullet describing the bottom-left Staged ⇄ Reflog tab toggle and the read-only HEAD-reflog viewer with enter/`.`-menu actions. Match the surrounding bullet style.

- [ ] **Step 4: Update CLAUDE.md if the panel taxonomy description changed**

If `CLAUDE.md`'s package map or any panel description enumerates the left-column panels, add Reflog as the Staged-slot sibling tab. If no such enumeration exists, skip (note "no change needed" in the commit body).

- [ ] **Step 5: Verify docs build/tests still pass**

Run: `go test ./internal/tui/ -run Help -v` (if a help test exists) and `go build ./...`
Expected: PASS / clean.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: reflog window — help, changelog, readme

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Final verification (before merge)

- [ ] Run the full race suite: `./test.sh race`. Expected: all green.
- [ ] Live TTY eyeball: open `gg` on a repo with reflog history, `ctrl+→` to Reflog, navigate, `enter` into an entry's files view, open the `.` menu (Copy SHA + Bookmark), confirm short-terminal drops the bottom slot cleanly, and confirm the reflog refreshes after an op (e.g. an empty commit).
- [ ] Use superpowers:finishing-a-development-branch to complete (merge to `main`).
