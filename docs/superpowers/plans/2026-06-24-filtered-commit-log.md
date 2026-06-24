# Filtered Commit Log Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the Commits panel narrow its walk by path, message text, author, and date range — not just by branch.

**Architecture:** Widen the existing `LogScope` (the single carrier of feed narrowing) with `Paths/Author/Grep/Since/Until`; `LogScoped` translates them to native `git log` flags. The TUI grows a `commitFilter` field, a `\` filter popup, status-line chips, a clear action, and a "Commits touching this" seed from the files view and fuzzy finder. The commit-graph is forced off whenever a filter is active. No new subsystem.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`), shells out to system `git`.

## Global Constraints

- A git verb is one invocation, built with `gitcmd` and run via `r.Runner.Run`; never shell out directly.
- `internal/tui` and `internal/cli` never import `internal/git` — reach git through `internal/domain` (archtest-guarded). `domain.LogScope = git.LogScope` (a type alias), so methods on the git type are visible in domain/tui.
- TUI `Model` is a value receiver with pointer fields for persisted state; slices shared across the value copy must be freshly allocated before mutate (see `without`).
- Tests use a real `git` in `t.TempDir()` (`newTestRepo`/`newRepo`/`newRepoDir`/`loadedModel`) or `FakeRunner` for argv assertions. Follow TDD.
- New global keys need a `help.go` row (`TestHelpFooterCoverage` fails otherwise) and a footer hint in `footer.go`.
- Branch is `feat/filtered-commit-log` (already created off `main`). The human merges.
- Run `gofmt -l`, `go vet ./...`, then `./test.sh race` before declaring done.
- YAGNI: no `gg log` CLI, no `--follow`, no regex-type toggles, no saved filters this pass.

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `internal/git/log.go` | `LogScope` type + `LogScoped` argv | Widen struct; add filter flags + trailing pathspec; add `filtered()` method |
| `internal/git/log_test.go` | git-layer real-repo tests | New filter tests |
| `internal/domain/commitfeed_test.go` | feed paging tests | New paging-under-filter test |
| `internal/tui/model.go` | Model state + scope label | Add `commitFilter` field + `commitFilterFields` type; extend `commitScopeLabel`; graph guard already in `commitGraphOn` (view.go) |
| `internal/tui/commit_scope.go` | feed reload + scope rows | `feedScope()` helper; fold filter into reload; clear-filter row |
| `internal/tui/view.go` | `commitGraphOn` predicate | Add `&& !m.commitFilter.filtered()` |
| `internal/tui/commit_filter_popup.go` (new) | the `\` filter popup | Whole file |
| `internal/tui/file_finder.go` | fuzzy-finder action rows | Add "Commits touching this" row |
| `internal/tui/action_menu.go` | files-view `.` menu | Add "Commits touching this" row in the `frontIsFilesView` block |
| `internal/tui/footer.go`, `help.go` | hints/help | `\` hint + help row |
| `CHANGELOG.md`, `README.md`, `CLAUDE.md` | docs | Feature entry + package-map note |

---

## Task 1: Widen `LogScope` and `LogScoped` (git layer)

**Files:**
- Modify: `internal/git/log.go:17-45`
- Test: `internal/git/log_test.go`

**Interfaces:**
- Produces: `LogScope{Branches, Paths []string; Author, Grep, Since, Until string}`; method `func (s LogScope) filtered() bool`; unchanged `LogScoped(ctx, limit, skip int, scope LogScope, dateOrder bool) ([]model.Commit, error)`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/git/log_test.go` (use the existing real-repo helper in this package — check the top of `log_test.go` for the constructor name, e.g. `newTestRepo(t)`; mirror an existing `LogScoped` test's setup of commits):

```go
func TestLogScopedPathFilter(t *testing.T) {
	r, dir := newTestRepo(t)
	writeFile(t, dir, "a.txt", "1")
	commitAll(t, dir, "touch a")
	writeFile(t, dir, "sub/b.txt", "1")
	commitAll(t, dir, "touch sub/b")

	got, err := r.LogScoped(context.Background(), 50, 0, LogScope{Paths: []string{"sub"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Subject != "touch sub/b" {
		t.Fatalf("path filter: want only [touch sub/b], got %v", subjects(got))
	}
}

func TestLogScopedAuthorAndGrep(t *testing.T) {
	r, dir := newTestRepo(t)
	writeFile(t, dir, "a.txt", "1")
	commitAll(t, dir, "fix the race")
	writeFile(t, dir, "a.txt", "2")
	commitAll(t, dir, "unrelated change")

	byGrep, err := r.LogScoped(context.Background(), 50, 0, LogScope{Grep: "RACE"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(byGrep) != 1 || byGrep[0].Subject != "fix the race" {
		t.Fatalf("grep -i: want [fix the race], got %v", subjects(byGrep))
	}

	byAuthor, err := r.LogScoped(context.Background(), 50, 0, LogScope{Author: "Test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(byAuthor) != 2 {
		t.Fatalf("author filter: want 2 commits, got %d", len(byAuthor))
	}
}

func TestLogScopeFilteredPredicate(t *testing.T) {
	if (LogScope{}).filtered() {
		t.Fatal("empty scope must not be filtered")
	}
	if (LogScope{Branches: []string{"main"}}).filtered() {
		t.Fatal("branch-only scope must NOT count as filtered (graph stays on)")
	}
	for _, s := range []LogScope{{Paths: []string{"x"}}, {Author: "a"}, {Grep: "g"}, {Since: "1 day ago"}, {Until: "now"}} {
		if !s.filtered() {
			t.Fatalf("%+v must be filtered", s)
		}
	}
}
```

If `writeFile`/`commitAll`/`subjects` helpers don't already exist in this test file, add small local helpers (or inline the equivalent `gitIn(t, dir, ...)` calls the file already uses). Check the file first and reuse its conventions — do not introduce a second style.

- [ ] **Step 2: Run the tests, verify they fail**

Run: `go test ./internal/git/ -run 'TestLogScoped(PathFilter|AuthorAndGrep)|TestLogScopeFilteredPredicate' -v`
Expected: FAIL (unknown fields `Paths/Author/Grep`, undefined method `filtered`).

- [ ] **Step 3: Widen the struct and `filtered()`**

Replace `internal/git/log.go:17-21` with:

```go
// LogScope selects and narrows the walk. Branches selects refs (empty → all
// local branches plus HEAD). Paths/Author/Grep/Since/Until further FILTER the
// result with native `git log` flags; any of them being set makes the feed a
// non-contiguous subset of history (path scope also rewrites parent linkage),
// which is why frontends suppress the commit-graph while filtered().
type LogScope struct {
	Branches []string
	Paths    []string // → trailing `-- <paths>`
	Author   string   // → --author=<s>
	Grep     string   // → --grep=<s> -i (case-insensitive)
	Since    string   // → --since=<s> (git-parsed: "2 weeks ago", "2026-01-01")
	Until    string   // → --until=<s>
}

// filtered reports whether any non-branch filter axis is set. Branch scope does
// NOT count: a soloed branch still shows contiguous history.
func (s LogScope) filtered() bool {
	return len(s.Paths) > 0 || s.Author != "" || s.Grep != "" || s.Since != "" || s.Until != ""
}
```

- [ ] **Step 4: Add the flags to `LogScoped`**

In `internal/git/log.go`, the builder currently is (lines ~28-39):

```go
	b := gitcmd.New("log").
		Arg("-n", strconv.Itoa(limit)).
		ArgIf(dateOrder, "--date-order").
		Arg("--decorate", "--source", "--format="+logFormat).
		ArgIf(skip > 0, "--skip="+strconv.Itoa(skip))
	if len(scope.Branches) == 0 {
		b = b.Arg("--branches", "HEAD")
	} else {
		b = b.Arg(scope.Branches...)
	}
```

Add the filter flags BEFORE the ref selection, and the pathspec AFTER it:

```go
	b := gitcmd.New("log").
		Arg("-n", strconv.Itoa(limit)).
		ArgIf(dateOrder, "--date-order").
		Arg("--decorate", "--source", "--format="+logFormat).
		ArgIf(skip > 0, "--skip="+strconv.Itoa(skip)).
		ArgIf(scope.Author != "", "--author="+scope.Author).
		ArgIf(scope.Since != "", "--since="+scope.Since).
		ArgIf(scope.Until != "", "--until="+scope.Until)
	if scope.Grep != "" {
		b = b.Arg("--grep="+scope.Grep, "-i")
	}
	if len(scope.Branches) == 0 {
		b = b.Arg("--branches", "HEAD")
	} else {
		b = b.Arg(scope.Branches...)
	}
	if len(scope.Paths) > 0 {
		b = b.Arg("--")
		b = b.Arg(scope.Paths...)
	}
```

(`ToArgv()` and `ParseLog` lines below stay unchanged.)

- [ ] **Step 5: Run the tests, verify they pass**

Run: `go test ./internal/git/ -run 'TestLogScoped(PathFilter|AuthorAndGrep)|TestLogScopeFilteredPredicate' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/git/log.go internal/git/log_test.go
git commit -m "feat(git): widen LogScope with path/author/grep/date filters"
```

---

## Task 2: Paging under a filter (domain)

**Files:**
- Test: `internal/domain/commitfeed_test.go`

**Interfaces:**
- Consumes: `Service.CommitFeed() *CommitFeed`, `(*CommitFeed).SetScope(LogScope)`, `LoadInitial(ctx)`, `LoadMore(ctx)`, `LogScope` (alias of git's). No production change — this task proves `--skip` applies post-filter so paging stays correct.

- [ ] **Step 1: Write the failing test**

Mirror the setup of an existing `commitfeed_test.go` test (find how it builds a `Service` + commits). Create > pageSize commits all touching `target/`, plus some touching only `other/`, then page:

```go
func TestCommitFeedPagesUnderPathFilter(t *testing.T) {
	svc, dir := newFeedService(t) // reuse this file's existing service+repo helper
	for i := 0; i < 30; i++ {
		writeFile(t, dir, "target/f.txt", fmt.Sprintf("%d", i))
		commitAll(t, dir, fmt.Sprintf("target %d", i))
		writeFile(t, dir, "other/g.txt", fmt.Sprintf("%d", i))
		commitAll(t, dir, fmt.Sprintf("other %d", i))
	}
	feed := svc.CommitFeed()
	feed.SetScope(LogScope{Paths: []string{"target"}})
	st, err := feed.LoadInitial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range st.Commits {
		if !strings.HasPrefix(c.Subject, "target ") {
			t.Fatalf("filtered feed leaked a non-target commit: %q", c.Subject)
		}
	}
	// Page until exhausted; every page must stay path-scoped and the skip must
	// not double-count or drop.
	seen := len(st.Commits)
	for !st.Exhausted {
		var more bool
		st, more, err = feed.LoadMore(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
		seen = len(st.Commits)
	}
	if seen != 30 {
		t.Fatalf("want 30 target commits across pages, got %d", seen)
	}
}
```

Check `commitfeed_test.go` for the exact helper names (`newFeedService`/`writeFile`/`commitAll` may differ) and adapt; do not invent a new repo-building style.

- [ ] **Step 2: Run it**

Run: `go test ./internal/domain/ -run TestCommitFeedPagesUnderPathFilter -v`
Expected: PASS immediately (production already supports this; the test locks the behavior). If it FAILS, stop — that's a real paging bug to investigate, not a test to weaken.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/commitfeed_test.go
git commit -m "test(domain): commit feed paging stays correct under a path filter"
```

---

## Task 3: `commitFilter` Model field + `feedScope()` helper

**Files:**
- Modify: `internal/tui/model.go` (Model struct, near `commitScopeBranches` at line ~88)
- Modify: `internal/tui/commit_scope.go:33-41` (`reloadFeedCmd`)
- Test: `internal/tui/commit_scope_test.go` (create if absent)

**Interfaces:**
- Produces: `type commitFilterFields struct { Paths []string; Author, Grep, Since, Until string }`; Model field `commitFilter commitFilterFields`; method `func (f commitFilterFields) filtered() bool`; method `func (m Model) feedScope() domain.LogScope`. Tasks 4-9 consume these.

- [ ] **Step 1: Write the failing test**

```go
func TestFeedScopeFoldsFilterAndBranches(t *testing.T) {
	var m Model
	m.commitScopeBranches = []string{"main"}
	m.commitFilter = commitFilterFields{Paths: []string{"sub"}, Author: "alice", Grep: "race"}
	s := m.feedScope()
	if len(s.Branches) != 1 || s.Branches[0] != "main" {
		t.Fatalf("branches not carried: %+v", s.Branches)
	}
	if len(s.Paths) != 1 || s.Paths[0] != "sub" || s.Author != "alice" || s.Grep != "race" {
		t.Fatalf("filter not folded: %+v", s)
	}
	if !m.commitFilter.filtered() {
		t.Fatal("filtered() should be true")
	}
	if (commitFilterFields{}).filtered() {
		t.Fatal("empty filter must not be filtered")
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/tui/ -run TestFeedScopeFoldsFilterAndBranches -v`
Expected: FAIL (undefined `commitFilterFields`, `feedScope`).

- [ ] **Step 3: Add the field and helpers**

In `internal/tui/model.go`, just below the `commitScopeBranches []string` field (line ~88), add:

```go
	commitFilter commitFilterFields // path/author/grep/date narrowing of the feed
```

Add a new small block (top of `commit_scope.go`, after the imports, or in model.go near the field — keep it next to `feedScope`):

```go
// commitFilterFields holds the active non-branch narrowing of the Commits feed.
type commitFilterFields struct {
	Paths  []string
	Author string
	Grep   string
	Since  string
	Until  string
}

func (f commitFilterFields) filtered() bool {
	return len(f.Paths) > 0 || f.Author != "" || f.Grep != "" || f.Since != "" || f.Until != ""
}

// feedScope builds the LogScope the feed should walk: branch selection plus the
// active filter. Fresh slices: the value-receiver Model shares slice backings.
func (m Model) feedScope() domain.LogScope {
	return domain.LogScope{
		Branches: append([]string(nil), m.commitScopeBranches...),
		Paths:    append([]string(nil), m.commitFilter.Paths...),
		Author:   m.commitFilter.Author,
		Grep:     m.commitFilter.Grep,
		Since:    m.commitFilter.Since,
		Until:    m.commitFilter.Until,
	}
}
```

- [ ] **Step 4: Route `reloadFeedCmd` through `feedScope()`**

Replace the scope line in `commit_scope.go:35`:

```go
	scope := domain.LogScope{Branches: append([]string(nil), m.commitScopeBranches...)}
```

with:

```go
	scope := m.feedScope()
```

- [ ] **Step 5: Run tests, verify pass**

Run: `go test ./internal/tui/ -run TestFeedScopeFoldsFilterAndBranches -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/commit_scope.go internal/tui/commit_scope_test.go
git commit -m "feat(tui): commitFilter field + feedScope folds filter into the reload"
```

---

## Task 4: Suppress the commit-graph while filtered

**Files:**
- Modify: `internal/tui/view.go:1016-1018` (`commitGraphOn`)
- Test: `internal/tui/commit_scope_test.go`

**Interfaces:**
- Consumes: `m.commitFilter.filtered()` (Task 3). The existing `graphActive()` (commit_graph_window.go:7) and footer hint (footer.go:75) call `commitGraphOn`/`graphActive`, so both follow automatically.

- [ ] **Step 1: Write the failing test**

```go
func TestGraphSuppressedWhenFiltered(t *testing.T) {
	var m Model
	// Default: graph allowed (no filter, default sort, in-memory filter off).
	if !m.commitGraphOn() {
		t.Fatal("precondition: graph should be on with no filter")
	}
	m.commitFilter = commitFilterFields{Grep: "race"}
	if m.commitGraphOn() {
		t.Fatal("graph must be suppressed while a commit filter is active")
	}
}
```

(If a bare `Model{}` doesn't satisfy `commitGraphOn`'s other clauses, set `m.sortModes[panelCommits] = sortDefault` and ensure `filterActive(panelCommits)` is false — inspect the two helpers and seed the minimum. Keep the assertion about the *filter* clause.)

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/tui/ -run TestGraphSuppressedWhenFiltered -v`
Expected: FAIL (graph still on with a filter).

- [ ] **Step 3: Add the guard**

Replace `internal/tui/view.go:1016-1018`:

```go
func (m Model) commitGraphOn() bool {
	return !m.filterActive(panelCommits) && m.sortModes[panelCommits] == sortDefault
}
```

with:

```go
func (m Model) commitGraphOn() bool {
	return !m.filterActive(panelCommits) && m.sortModes[panelCommits] == sortDefault &&
		!m.commitFilter.filtered()
}
```

- [ ] **Step 4: Run it, verify pass**

Run: `go test ./internal/tui/ -run TestGraphSuppressedWhenFiltered -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/view.go internal/tui/commit_scope_test.go
git commit -m "feat(tui): suppress commit-graph lanes while a commit filter is active"
```

---

## Task 5: The `\` filter popup

**Files:**
- Create: `internal/tui/commit_filter_popup.go`
- Modify: `internal/tui/model.go` (add `case "\\"` in the Commits-panel key switch, near the other Commits cases ~line 1018)
- Modify: `internal/tui/footer.go` (add a `\` hint gated on `m.focus == panelCommits`)
- Modify: `internal/tui/help.go` (add a `\` row in the Commits section)
- Test: `internal/tui/commit_filter_popup_test.go`

**Interfaces:**
- Consumes: `commitFilterFields` + `m.commitFilter` + `m.startFeedReload()` (Task 3); the `layer` interface (`update(Model, tea.KeyMsg) (Model, tea.Cmd)`, `render(Model, string) string`); `m.pushLayer`/`m.popLayer`; the `textfield` type — **CONFIRMED API** (`internal/tui/textfield.go`): `newTextField(s string) textfield` (prefilled, cursor at end), `func (f textfield) Value() string` (value receiver), `func (f *textfield) HandleEditKey(msg tea.KeyMsg) bool` (inserts runes/space, handles backspace/arrows/word-jump, returns false for keys it ignores). There is **no** `Update`/`withValue`/`SetValue`. `viewField(prefix string, f textfield, focused bool, contentWidth int) string` (`field_style.go`); `overlayCenter`, `clipToHeight`, `modalStyle`, `popupInnerWidth`.

The drive idiom (from `worktree_popup.go:194-196`): `f := p.fields[i]; if f.HandleEditKey(msg) { p.fields[i] = f }`. Since `p` is a pointer and `p.fields` is an array, you may also call `p.fields[p.focus].HandleEditKey(msg)` directly (addressable). Read `worktree_popup.go` and `field_style.go` before writing.

- [ ] **Step 1: Write the failing tests**

```go
func TestCommitFilterPopupOpensOnBackslash(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelCommits
	m2, _ := m.Update(keyMsg("\\"))
	mm := m2.(Model)
	if _, ok := mm.topLayer().(*commitFilterPopup); !ok {
		t.Fatalf("backslash on Commits should open the filter popup, top=%T", mm.topLayer())
	}
}

func TestCommitFilterPopupApplySetsFilter(t *testing.T) {
	m := loadedModel(t)
	p := &commitFilterPopup{}
	p.focus = cfGrep
	for _, r := range "race" { // drive the focused field through HandleEditKey
		p.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = m.pushLayer(p)
	// Enter applies.
	m2, _ := m.Update(keyMsg("enter"))
	mm := m2.(Model)
	if mm.commitFilter.Grep != "race" {
		t.Fatalf("apply should set Grep=race, got %q", mm.commitFilter.Grep)
	}
	if _, ok := mm.topLayer().(*commitFilterPopup); ok {
		t.Fatal("apply should pop the popup")
	}
}

func TestCommitFilterPopupEscCancels(t *testing.T) {
	m := loadedModel(t)
	m = m.pushLayer(&commitFilterPopup{})
	before := m.commitFilter
	m2, _ := m.Update(keyMsg("esc"))
	mm := m2.(Model)
	if _, ok := mm.topLayer().(*commitFilterPopup); ok {
		t.Fatal("esc should pop the popup")
	}
	if mm.commitFilter != before {
		t.Fatal("esc must not change the filter")
	}
}

func TestCommitFilterPopupSwallowsGlobalKeys(t *testing.T) {
	m := loadedModel(t)
	m = m.pushLayer(&commitFilterPopup{})
	m2, cmd := m.Update(keyMsg("p")) // 'p' is pull globally; must NOT fire here
	mm := m2.(Model)
	if mm.running {
		t.Fatal("global key leaked through the popup")
	}
	_ = cmd
}
```

(The test feeds runes through `p.update` exactly as the real keyboard path does — no test-only setter needed.)

- [ ] **Step 2: Run them, verify they fail**

Run: `go test ./internal/tui/ -run TestCommitFilterPopup -v`
Expected: FAIL (undefined `commitFilterPopup`).

- [ ] **Step 3: Write the popup**

Create `internal/tui/commit_filter_popup.go`. Model it on `worktree_popup.go`'s field-focus pattern. Five fields, Tab/down to advance, Enter to apply, Esc to cancel, Ctrl+C to quit, everything else routed to the focused `textfield`. Constants:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type cfField int

const (
	cfPath cfField = iota
	cfAuthor
	cfGrep
	cfSince
	cfUntil
	cfFieldCount
)

var cfLabels = [cfFieldCount]string{"Path:    ", "Author:  ", "Message: ", "Since:   ", "Until:   "}

// commitFilterPopup collects the non-branch feed filter. Opened with `\` on the
// Commits panel; Enter applies (sets m.commitFilter + reloads), Esc cancels.
type commitFilterPopup struct {
	fields [cfFieldCount]textfield
	focus  cfField
}

// newCommitFilterPopup prefills the popup from the active filter so re-opening
// edits the current filter rather than starting blank.
func newCommitFilterPopup(cur commitFilterFields) *commitFilterPopup {
	p := &commitFilterPopup{}
	var path string
	if len(cur.Paths) > 0 {
		path = cur.Paths[0]
	}
	p.fields[cfPath] = newTextField(path)
	p.fields[cfAuthor] = newTextField(cur.Author)
	p.fields[cfGrep] = newTextField(cur.Grep)
	p.fields[cfSince] = newTextField(cur.Since)
	p.fields[cfUntil] = newTextField(cur.Until)
	return p
}

func (p *commitFilterPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		return m.popLayer(), nil
	case "enter":
		m = m.popLayer()
		m.commitFilter = p.collect()
		return m.startFeedReload()
	case "tab", "down":
		p.focus = (p.focus + 1) % cfFieldCount
		return m, nil
	case "shift+tab", "up":
		p.focus = (p.focus + cfFieldCount - 1) % cfFieldCount
		return m, nil
	default:
		// Route the key to the focused field. HandleEditKey returns false for
		// keys it ignores; we swallow either way so nothing leaks to globals.
		p.fields[p.focus].HandleEditKey(msg)
		return m, nil
	}
}

// collect builds the filter from the field values (empty fields contribute no
// axis; an all-empty apply clears the filter).
func (p *commitFilterPopup) collect() commitFilterFields {
	f := commitFilterFields{
		Author: p.fields[cfAuthor].Value(),
		Grep:   p.fields[cfGrep].Value(),
		Since:  p.fields[cfSince].Value(),
		Until:  p.fields[cfUntil].Value(),
	}
	if path := p.fields[cfPath].Value(); path != "" {
		f.Paths = []string{path}
	}
	return f
}

func (p *commitFilterPopup) render(m Model, below string) string {
	w, h := m.width, m.height
	cw := popupInnerWidth(w)
	var b []string
	b = append(b, "Filter commits")
	b = append(b, "")
	for i := cfField(0); i < cfFieldCount; i++ {
		b = append(b, viewField(cfLabels[i], p.fields[i], i == p.focus, cw))
	}
	b = append(b, "")
	b = append(b, "[enter] apply  [tab] next  [esc] cancel")
	box := modalStyle.Width(cw).Render(lipgloss.JoinVertical(lipgloss.Left, b...))
	bh := lipgloss.Height(box)
	return overlayCenter(clipToHeight(below, h), box, w, bh)
}
```

Note: `cfLabels`/`render` use `viewField(prefix, f, focused, contentWidth)` exactly as `field_style.go` defines it. The `textfield` API used above (`newTextField`, `Value`, `HandleEditKey`) is confirmed against `textfield.go` — no other methods are needed.

- [ ] **Step 4: Wire the `\` key**

In `internal/tui/model.go`, near the Commits-panel cases (after `case "@":` ~line 1031), add:

```go
		case "\\":
			if !m.running && !m.loading && m.focus == panelCommits {
				m = m.pushLayer(newCommitFilterPopup(m.commitFilter))
				return m, nil
			}
```

- [ ] **Step 5: Footer + help**

In `internal/tui/footer.go`, add a binding (copy the shape of the `l`/graph entries) keyed `\`, hint `"filter"`, gated `m.focus == panelCommits && !(m.width > 0 && m.width < 40)`.

In `internal/tui/help.go`, in the Commits-panel section (the `r("key","desc")` block ~line 118), add:

```go
		r("\\", "filter commits by path / author / message / date (popup; empty fields clear that axis); graph hides while filtered"),
```

- [ ] **Step 6: Run tests, verify pass**

Run: `go test ./internal/tui/ -run 'TestCommitFilterPopup|TestHelpFooterCoverage' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/commit_filter_popup.go internal/tui/commit_filter_popup_test.go internal/tui/model.go internal/tui/footer.go internal/tui/help.go
git commit -m "feat(tui): \\ opens a path/author/message/date filter popup on Commits"
```

---

## Task 6: Status-line filter chips

**Files:**
- Modify: `internal/tui/model.go:1692-1702` (`commitScopeLabel`)
- Test: `internal/tui/commit_scope_test.go`

**Interfaces:**
- Consumes: `m.commitFilter` (Task 3). `commitScopeLabel()` already feeds the Commits panel header; appending chips surfaces the active filter.

- [ ] **Step 1: Write the failing test**

```go
func TestCommitScopeLabelShowsFilterChips(t *testing.T) {
	var m Model
	m.commitFilter = commitFilterFields{Paths: []string{"sub"}, Grep: "race", Author: "alice"}
	got := m.commitScopeLabel()
	for _, want := range []string{"path=sub", "msg=race", "@alice"} {
		if !strings.Contains(got, want) {
			t.Fatalf("label %q missing chip %q", got, want)
		}
	}
}

func TestCommitScopeLabelPlainWhenUnfiltered(t *testing.T) {
	var m Model
	if got := m.commitScopeLabel(); got != "all" {
		t.Fatalf("unfiltered label should be \"all\", got %q", got)
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/tui/ -run TestCommitScopeLabel -v`
Expected: FAIL (no chips appended).

- [ ] **Step 3: Append chips in `commitScopeLabel`**

Change `commitScopeLabel` (model.go ~1692) to build the branch part as today, then append filter chips:

```go
func (m Model) commitScopeLabel() string {
	var base string
	switch len(m.commitScopeBranches) {
	case 0:
		base = "all"
	case 1:
		base = "solo: " + m.commitScopeBranches[0]
	default:
		base = fmt.Sprintf("%d branches", len(m.commitScopeBranches))
	}
	chips := m.commitFilterChips()
	if chips == "" {
		return base
	}
	return base + " · " + chips
}

// commitFilterChips renders the active filter as compact chips, or "" if none.
func (m Model) commitFilterChips() string {
	f := m.commitFilter
	var parts []string
	if len(f.Paths) > 0 {
		parts = append(parts, "path="+f.Paths[0])
	}
	if f.Grep != "" {
		parts = append(parts, "msg="+f.Grep)
	}
	if f.Author != "" {
		parts = append(parts, "@"+f.Author)
	}
	if f.Since != "" {
		parts = append(parts, "since="+f.Since)
	}
	if f.Until != "" {
		parts = append(parts, "until="+f.Until)
	}
	return strings.Join(parts, " ")
}
```

Ensure `strings` is imported in model.go (it almost certainly already is; if not, add it).

- [ ] **Step 4: Run it, verify pass**

Run: `go test ./internal/tui/ -run TestCommitScopeLabel -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/commit_scope_test.go
git commit -m "feat(tui): show active commit filter as chips in the Commits header"
```

---

## Task 7: Clear-filter action

**Files:**
- Modify: `internal/tui/commit_scope.go` (extend `commitShowAllRow` OR add a sibling `commitClearFilterRow`)
- Test: `internal/tui/commit_scope_test.go`

**Interfaces:**
- Consumes: `m.commitFilter`, `m.startFeedReload()`. Produces a `.`-menu row `commitClearFilterRow() (actionRow, bool)` present only when a filter is active, on the Commits panel.

- [ ] **Step 1: Write the failing test**

```go
func TestClearFilterRowPresentOnlyWhenFiltered(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelCommits
	if _, ok := m.commitClearFilterRow(); ok {
		t.Fatal("no clear-filter row when unfiltered")
	}
	m.commitFilter = commitFilterFields{Grep: "race"}
	row, ok := m.commitClearFilterRow()
	if !ok {
		t.Fatal("clear-filter row should appear when filtered")
	}
	mm, _ := row.run(m)
	if mm.(Model).commitFilter.filtered() {
		t.Fatal("running clear-filter must empty the filter")
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/tui/ -run TestClearFilterRow -v`
Expected: FAIL (undefined `commitClearFilterRow`).

- [ ] **Step 3: Add the row**

In `commit_scope.go` (next to `commitShowAllRow`):

```go
// commitClearFilterRow offers "Clear filter" on the Commits panel when a
// path/author/message/date filter is active.
func (m Model) commitClearFilterRow() (actionRow, bool) {
	if !m.opsIdle() || m.focus != panelCommits || !m.commitFilter.filtered() {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commits-clear-filter",
		label: "Clear filter",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitFilter = commitFilterFields{}
			return m.startFeedReload()
		},
	}, true
}
```

Wire it into the `.` menu where the other commit-context rows are appended (find `appendCommitContextRows` in action_menu.go:183, or wherever `commitShowAllRow` is consumed, and append `commitClearFilterRow` alongside it). Grep for `commitShowAllRow(` to find the call site and mirror it.

- [ ] **Step 4: Run it, verify pass**

Run: `go test ./internal/tui/ -run TestClearFilterRow -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commit_scope.go internal/tui/action_menu.go internal/tui/commit_scope_test.go
git commit -m "feat(tui): Clear filter row on the Commits . menu"
```

---

## Task 8: "Commits touching this" — fuzzy finder

**Files:**
- Modify: `internal/tui/file_finder.go` (inside `fileFinderActionRows(path)`, ~line 292)
- Test: `internal/tui/file_finder_test.go` (or `commit_scope_test.go`)

**Interfaces:**
- Consumes: `m.commitFilter`, `m.startFeedReload()`, `m.popLayer()`, `panelCommits`, the `focus` field. Produces a new action row `id:"ff-commits-touching"` that sets `commitFilter.Paths=[path]` (clearing other axes), focuses Commits, reloads.

- [ ] **Step 1: Write the failing test**

```go
func TestFileFinderCommitsTouchingSeedsPathFilter(t *testing.T) {
	m := loadedModel(t)
	rows := m.fileFinderActionRows("internal/engine/ops_basic.go")
	var run func(Model) (tea.Model, tea.Cmd)
	for _, r := range rows {
		if r.id == "ff-commits-touching" {
			run = r.run
		}
	}
	if run == nil {
		t.Fatal("fuzzy finder missing 'Commits touching this' row")
	}
	mm, _ := run(m)
	got := mm.(Model)
	if len(got.commitFilter.Paths) != 1 || got.commitFilter.Paths[0] != "internal/engine/ops_basic.go" {
		t.Fatalf("path not seeded: %+v", got.commitFilter.Paths)
	}
	if got.commitFilter.Author != "" || got.commitFilter.Grep != "" {
		t.Fatal("seeding a path should clear the other axes")
	}
	if got.focus != panelCommits {
		t.Fatal("should focus Commits after seeding")
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/tui/ -run TestFileFinderCommitsTouching -v`
Expected: FAIL (no such row).

- [ ] **Step 3: Add the row**

Inside `fileFinderActionRows(path string)` (file_finder.go), append to the returned `[]actionRow`:

```go
		{
			id:    "ff-commits-touching",
			label: "Commits touching this",
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.popLayer()
				m.commitFilter = commitFilterFields{Paths: []string{path}}
				m.focus = panelCommits
				return m.startFeedReload()
			},
		},
```

- [ ] **Step 4: Run it, verify pass**

Run: `go test ./internal/tui/ -run TestFileFinderCommitsTouching -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/file_finder.go internal/tui/file_finder_test.go
git commit -m "feat(tui): 'Commits touching this' seed from the fuzzy file finder"
```

---

## Task 9: "Commits touching this" — files view `.` menu

**Files:**
- Modify: `internal/tui/action_menu.go` (the `frontIsFilesView` block, ~line 58-66)
- Modify: `internal/tui/files_view.go` or `action_menu.go` (a `commitsTouchingFileRow()` builder)
- Test: `internal/tui/commit_scope_test.go`

**Interfaces:**
- Consumes: the files-view selected path (find how `viewFileRow()` reads the focused file path — reuse exactly that accessor), `m.commitFilter`, `m.startFeedReload`, `m.closeFilesView` (the chokepoint that tears down the files view — grep for it; the seed should close the files view, set the filter, focus Commits, reload). Produces `commitsTouchingFileRow() (actionRow, bool)`.

- [ ] **Step 1: Read first**

Read `viewFileRow()` (grep `func (m Model) viewFileRow`) to learn how it gets the selected file path and its guard (tree side, has a path, front is files view). Read `closeFilesView` to learn the teardown chokepoint. The new row reuses both.

- [ ] **Step 2: Write the failing test**

```go
func TestFilesViewCommitsTouchingSeedsFilter(t *testing.T) {
	m := loadedModel(t)
	m = openFilesViewOnSomeCommit(t, m) // reuse whatever helper the files-view tests use
	row, ok := m.commitsTouchingFileRow()
	if !ok {
		t.Skip("no selectable file in the harness commit") // keep green if the fixture has no file
	}
	mm, _ := row.run(m)
	got := mm.(Model)
	if len(got.commitFilter.Paths) != 1 {
		t.Fatalf("path not seeded: %+v", got.commitFilter.Paths)
	}
	if got.filesView != nil {
		t.Fatal("seeding should close the files view")
	}
	if got.focus != panelCommits {
		t.Fatal("should focus Commits")
	}
}
```

Adapt `openFilesViewOnSomeCommit` to the real files-view test helpers in `files_view_test.go`. If there is a ready helper that opens a files view with a known file, prefer asserting the exact path instead of `t.Skip`.

- [ ] **Step 3: Run it, verify it fails**

Run: `go test ./internal/tui/ -run TestFilesViewCommitsTouching -v`
Expected: FAIL (undefined `commitsTouchingFileRow`).

- [ ] **Step 4: Add the row builder and wire it**

Add `commitsTouchingFileRow()` mirroring `viewFileRow`'s guard and path accessor:

```go
// commitsTouchingFileRow seeds the Commits feed with a path filter for the
// files-view selected file, closes the files view, and focuses Commits.
func (m Model) commitsTouchingFileRow() (actionRow, bool) {
	path, ok := m.filesViewSelectedPath() // use the SAME accessor viewFileRow uses
	if !ok || path == "" {
		return actionRow{}, false
	}
	return actionRow{
		id:    "files-commits-touching",
		label: "Commits touching this",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m = m.closeFilesView()
			m.commitFilter = commitFilterFields{Paths: []string{path}}
			m.focus = panelCommits
			return m.startFeedReload()
		},
	}, true
}
```

Wire it in `availableActions`' `frontIsFilesView` branch (action_menu.go ~58), after `viewFileRow`:

```go
		} else if frontIsFilesView {
			if r, ok := m.viewFileRow(); ok {
				rows = append(rows, r)
			}
			if r, ok := m.commitsTouchingFileRow(); ok {
				rows = append(rows, r)
			}
			if r, ok := m.openExternalRow(); ok {
				rows = append(rows, r)
			}
		}
```

(If `viewFileRow` uses an inline accessor rather than a named `filesViewSelectedPath`, extract that accessor into a small method and call it from both — DRY.)

- [ ] **Step 5: Run it, verify pass**

Run: `go test ./internal/tui/ -run TestFilesViewCommitsTouching -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/action_menu.go internal/tui/files_view.go internal/tui/commit_scope_test.go
git commit -m "feat(tui): 'Commits touching this' seed from the files view . menu"
```

---

## Task 10: Docs

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`

- [ ] **Step 1: CHANGELOG**

Under `## [Unreleased]` → `### Added`, add:

```markdown
- **Filtered commit log** — `\` on the Commits panel opens a filter popup (path,
  author, message, date range) that narrows the feed via `git log` flags; filters
  compose with branch scope. "Commits touching this" seeds a path filter from the
  fuzzy file finder and the files view. The commit-graph hides while a filter is
  active (the filtered feed is a non-contiguous subset). A "Clear filter" row
  restores the full feed.
```

- [ ] **Step 2: README**

In the Commits-panel key table, add a `\` row: "filter the commit feed by path / author / message / date". If there's a feature-list section, add one line mirroring the CHANGELOG.

- [ ] **Step 3: CLAUDE.md**

In the `domain` package-map row, update the `LogScope` note to: "carries a `LogScope` — branch selection plus path/author/message/date filters (any filter axis suppresses the TUI commit-graph)".

- [ ] **Step 4: Verify the whole suite**

Run: `gofmt -l internal/ && go vet ./... && ./test.sh race`
Expected: gofmt prints nothing; vet clean; all stages PASS.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: filtered commit log (path/author/message/date)"
```

---

## Self-Review

**Spec coverage:** path/author/grep/date axes (Task 1), compose with branches (Task 3), popup entry `\` (Task 5), graph suppression (Task 4), status chips (Task 6), clear (Task 7), seed from finder + files view (Tasks 8-9), dates verbatim to git (Task 1, no parse code), TUI-only / no CLI (nothing adds a CLI command), error path via existing `LoadInitial` error return (no new handling needed — documented in spec). All covered.

**Type consistency:** `commitFilterFields` (TUI) ↔ `LogScope` fields (git) — `feedScope()` (Task 3) is the single mapping point; `filtered()` exists on both `LogScope` (git, Task 1) and `commitFilterFields` (TUI, Task 3) deliberately. `commitGraphOn` guard (Task 4) uses the TUI `filtered()`. Action row ids unique: `ff-commits-touching`, `files-commits-touching`, `commits-clear-filter`.

**Known adaptation points (flagged inline, not placeholders):** the exact `textfield` API (`Value`/`Update`/`withValue`) and the files-view selected-path accessor must be read from source before coding — each task says so and names the file. These are real-API lookups, not undefined behavior.
