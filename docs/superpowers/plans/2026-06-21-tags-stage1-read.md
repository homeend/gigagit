# Tags — Stage 1 (Read) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only **Tags** view to `gg` — a `git tag` list verb, a domain query, a Tags tab living in the middle (Files) window via a generalized two-slot tab mechanism, an `enter`-jumps-to-commit nicety, and a `gg tag ls` CLI command.

**Architecture:** A new `git.Repo.Tags()` verb (one `for-each-ref refs/tags` invocation) → a best-effort `domain.Service.Tags()` Read query folded into `Snapshot` → TUI `panelTags` registered with the existing `panelList`/`panelView` machinery → the single hard-wired left-tab slot generalized into two independent slots (top = refs, middle = Files⇄Tags) cycled focus-aware by `ctrl+←/→` → `gg tag ls` CLI mirroring `gg remote ls`.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), `gitcmd` argv builder, `gitexec` Runner/FakeRunner, real-`git`-in-`t.TempDir()` tests, declarative TOML e2e harness.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26. A git verb is **one** git invocation, built with `gitcmd`, run via `r.Runner.Run`. (CLAUDE.md)
- `internal/tui` and `internal/cli` **never** import `internal/git` — they reach git through `internal/domain` (archtest-guarded). (CLAUDE.md)
- Frontend reads go through domain queries, not direct `internal/git` calls. (CLAUDE.md)
- TUI `Model` is a **value receiver** with pointer fields for cross-copy state. (CLAUDE.md)
- Tests use a real `git` in `t.TempDir()` (`newRepoDir`/`gitIn`/`gitOut` helpers) or `FakeRunner` for argv assertions. Follow TDD. (CLAUDE.md)
- Every new keybinding lands in BOTH `help.go` and the context-help footer. (memory: advertise-features-in-help-and-footer)
- Work happens in the worktree `/mnt/t/others/gg-tags` (branch `tags-stage1`). Edit tool uses absolute paths — always under `/mnt/t/others/gg-tags`. (memory: write-edit-absolute-path-ignores-worktree)
- `./test.sh` stages: vet+gofmt → unit → e2e. Run `./test.sh race` before declaring the stage done.

---

### Task 1: `model.Tag` + git `Tags()` verb + parser

**Files:**
- Modify: `internal/model/model.go` (add `Tag` type near `RemoteBranch`)
- Create: `internal/git/tag_parse.go`
- Create: `internal/git/tag_parse_test.go`
- Modify: `internal/git/repo.go` (add `Repo.Tags`, next to `Worktrees` ~line 59)
- Create: `internal/git/tag_verb_test.go`

**Interfaces:**
- Produces:
  - `model.Tag{ Name string; Target string; Annotated bool; Subject string }`
  - `git.ParseTags(data []byte) ([]model.Tag, error)`
  - `(*git.Repo).Tags(ctx context.Context) ([]model.Tag, error)`

- [ ] **Step 1: Write the failing parser test**

Create `internal/git/tag_parse_test.go`:

```go
package git

import "testing"

func TestParseTags(t *testing.T) {
	// fields: name \x00 objecttype \x00 objectname:short \x00 *objectname:short
	//         \x00 contents:subject \x00 creatordate:unix
	data := []byte(
		"v2.0.0\x00tag\x00aaaaaaa\x00bbbbbbb\x00release two\x001700000000\n" +
			"v1.0.0\x00commit\x00ccccccc\x00\x00init commit\x001600000000\n")
	tags, err := ParseTags(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Fatalf("len = %d, want 2", len(tags))
	}
	// Annotated: target is the PEELED object (*objectname), subject is the tag message.
	if got := tags[0]; !got.Annotated || got.Name != "v2.0.0" || got.Target != "bbbbbbb" || got.Subject != "release two" {
		t.Fatalf("annotated tag wrong: %+v", got)
	}
	// Lightweight: objecttype=commit, target is objectname (no peel), subject is the commit's.
	if got := tags[1]; got.Annotated || got.Name != "v1.0.0" || got.Target != "ccccccc" || got.Subject != "init commit" {
		t.Fatalf("lightweight tag wrong: %+v", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/git/ -run TestParseTags`
Expected: FAIL — `undefined: ParseTags` (and `model.Tag`).

- [ ] **Step 3: Add the `model.Tag` type**

In `internal/model/model.go`, after the `RemoteBranch` struct, add:

```go
// Tag is one git tag (refs/tags). Target is the commit the tag resolves to
// (the peeled commit for an annotated tag, the direct commit for a lightweight
// one). Subject is the annotated tag's message subject, or — for a lightweight
// tag — its target commit's subject.
type Tag struct {
	Name      string
	Target    string
	Annotated bool
	Subject   string
}
```

- [ ] **Step 4: Write the parser**

Create `internal/git/tag_parse.go`:

```go
package git

import (
	"strings"

	"github.com/gigagit/gg/internal/model"
)

// ParseTags parses `git for-each-ref refs/tags` output formatted as:
//
//	%(refname:short)\x00%(objecttype)\x00%(objectname:short)\x00%(*objectname:short)\x00%(contents:subject)\x00%(creatordate:unix)
//
// one ref per line. An annotated tag has objecttype "tag" and a non-empty
// peeled object (*objectname) — its real commit; a lightweight tag points
// straight at the commit (objecttype "commit", empty peel).
func ParseTags(data []byte) ([]model.Tag, error) {
	var out []model.Tag
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 3 {
			continue
		}
		annotated := f[1] == "tag"
		target := f[2]
		if annotated && len(f) >= 4 && f[3] != "" {
			target = f[3] // peeled commit
		}
		t := model.Tag{Name: f[0], Annotated: annotated, Target: target}
		if len(f) >= 5 {
			t.Subject = f[4]
		}
		out = append(out, t)
	}
	return out, nil
}
```

- [ ] **Step 5: Run the parser test to verify it passes**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/git/ -run TestParseTags`
Expected: PASS.

- [ ] **Step 6: Write the failing real-git verb test**

Create `internal/git/tag_verb_test.go` (uses the existing `newRepoDir`/`gitIn`/`gitOut` real-git helpers in this package's tests):

```go
package git

import (
	"context"
	"testing"
)

func TestRepoTags(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	gitIn(t, dir, "tag", "v1.0.0")                       // lightweight
	gitIn(t, dir, "tag", "-a", "v2.0.0", "-m", "rel 2")  // annotated
	commit := gitOut(t, dir, "rev-parse", "--short", "HEAD")

	tags, err := repo.Tags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]bool{}
	for _, tg := range tags {
		byName[tg.Name] = true
		if tg.Target != commit {
			t.Fatalf("%s target = %q, want %q", tg.Name, tg.Target, commit)
		}
		switch tg.Name {
		case "v1.0.0":
			if tg.Annotated {
				t.Fatalf("v1.0.0 must be lightweight")
			}
		case "v2.0.0":
			if !tg.Annotated || tg.Subject != "rel 2" {
				t.Fatalf("v2.0.0 wrong: %+v", tg)
			}
		}
	}
	if !byName["v1.0.0"] || !byName["v2.0.0"] {
		t.Fatalf("missing tags: %+v", tags)
	}
}
```

NOTE: confirm the real-git helper names in this package before running — open `internal/git/worktree_verbs_test.go` and reuse whatever it uses (`newRepoDir`/`gitIn`/`gitOut` or equivalents). Adjust the calls to match.

- [ ] **Step 7: Run it to verify it fails**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/git/ -run TestRepoTags`
Expected: FAIL — `repo.Tags undefined`.

- [ ] **Step 8: Add the `Repo.Tags` verb**

In `internal/git/repo.go`, after `Worktrees` (~line 59), add:

```go
// Tags returns the repository's tags (refs/tags), newest first. The peeled
// object resolves an annotated tag to its commit.
func (r *Repo) Tags(ctx context.Context) ([]model.Tag, error) {
	const format = "%(refname:short)%00%(objecttype)%00%(objectname:short)%00%(*objectname:short)%00%(contents:subject)%00%(creatordate:unix)"
	argv := gitcmd.New("for-each-ref").Arg("--sort=-creatordate", "--format="+format, "refs/tags").ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref (tags)", argv)
	if err != nil {
		return nil, err
	}
	return ParseTags([]byte(res.Stdout))
}
```

- [ ] **Step 9: Run both git tests to verify they pass**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/git/ -run 'TestParseTags|TestRepoTags'`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gg-tags
git add internal/model/model.go internal/git/tag_parse.go internal/git/tag_parse_test.go internal/git/repo.go internal/git/tag_verb_test.go
git commit -m "feat(git): Tags() verb + model.Tag (for-each-ref refs/tags)"
```

---

### Task 2: domain `Tags()` query + `Snapshot` wiring

**Files:**
- Modify: `internal/domain/query.go` (add `Service.Tags`; add `Tags` field to `Snapshot`; add a best-effort `run(...)` in `loadSnapshot`)
- Create: `internal/domain/tags_test.go`

**Interfaces:**
- Consumes: `(*git.Repo).Tags` (Task 1).
- Produces:
  - `(*domain.Service).Tags(ctx) ([]model.Tag, error)`
  - `domain.Snapshot.Tags []model.Tag`

- [ ] **Step 1: Write the failing domain test**

Create `internal/domain/tags_test.go` (mirror the existing worktree/remote query tests in this package — reuse their real-git repo helper; check `query.go`'s sibling `*_test.go` for the exact helper name, e.g. `newService`/`newRepoDir`):

```go
package domain

import (
	"context"
	"testing"
)

func TestServiceTags(t *testing.T) {
	dir, svc := newServiceRepo(t) // reuse this package's real-git test helper
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	gitIn(t, dir, "tag", "v1.0.0")

	tags, err := svc.Tags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "v1.0.0" {
		t.Fatalf("tags = %+v", tags)
	}
}

func TestSnapshotIncludesTags(t *testing.T) {
	dir, svc := newServiceRepo(t)
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	gitIn(t, dir, "tag", "v1.0.0")

	snap, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Tags) != 1 || snap.Tags[0].Name != "v1.0.0" {
		t.Fatalf("snap.Tags = %+v", snap.Tags)
	}
}
```

NOTE: replace `newServiceRepo`/`gitIn` with this package's actual helpers (open an existing `internal/domain/*_test.go` that builds a real repo, e.g. the worktrees or status query test, and copy its setup).

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/domain/ -run 'TestServiceTags|TestSnapshotIncludesTags'`
Expected: FAIL — `svc.Tags undefined`, `snap.Tags undefined`.

- [ ] **Step 3: Add the `Tags` field to `Snapshot`**

In `internal/domain/query.go`, in the `Snapshot` struct (near `Worktrees []model.Worktree`, ~line 26), add:

```go
	Tags            []model.Tag
```

- [ ] **Step 4: Add the `Service.Tags` query**

In `internal/domain/query.go`, next to `Worktrees` (~line 181), add:

```go
// Tags is a single gated read for the CLI tag commands and the TUI Tags tab.
func (s *Service) Tags(ctx context.Context) ([]model.Tag, error) {
	return query(ctx, s, "tags", s.repo.Tags)
}
```

- [ ] **Step 5: Load tags in `loadSnapshot` (best-effort)**

In `internal/domain/query.go`, inside `loadSnapshot`, add another `run(...)` block alongside the `RemoteBranches` one (~line 96). Tags must NOT block startup, so it is best-effort:

```go
	run(func() {
		// Tags is best-effort: a repo with no tags (or a failing for-each-ref)
		// must not block startup.
		if tags, err := s.repo.Tags(ctx); err == nil {
			mu.Lock()
			snap.Tags = tags
			mu.Unlock()
		}
	})
```

- [ ] **Step 6: Run the domain tests to verify they pass**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/domain/ -run 'TestServiceTags|TestSnapshotIncludesTags'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gg-tags
git add internal/domain/query.go internal/domain/tags_test.go
git commit -m "feat(domain): Tags() query + Snapshot.Tags (best-effort)"
```

---

### Task 3: TUI data plumbing — `panelTags`, `m.tags`, `tagList`, `listFor`

**Files:**
- Modify: `internal/tui/model.go` (add `panelTags` to the panel enum; add `tags []model.Tag` field)
- Modify: `internal/tui/load.go` (add `tags` to `dataLoadedMsg`; populate from `snap.Tags`)
- Modify: `internal/tui/model.go` (apply `msg.tags` in the `dataLoadedMsg` handler, ~line 297)
- Create: `internal/tui/tags_view.go` (`tagRows()`)
- Modify: `internal/tui/viewstate.go` (add `tagList`; add `panelTags` case to `listFor`)
- Create: `internal/tui/tags_view_test.go`

**Interfaces:**
- Consumes: `domain.Snapshot.Tags` (Task 2), `model.Tag` (Task 1).
- Produces:
  - panel id `panelTags`
  - `m.tags []model.Tag`
  - `(Model).tagRows() []string`
  - `tagList` implementing `panelList`
  - `listFor(panelTags)` returns a `tagList`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/tags_view_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestTagRowsAndPanelView(t *testing.T) {
	m := New(nil)
	m.tags = []model.Tag{
		{Name: "v2.0.0", Target: "aaaaaaa", Annotated: true, Subject: "release two"},
		{Name: "v1.0.0", Target: "ccccccc", Annotated: false, Subject: "init"},
	}
	rows, idx := m.panelView(panelTags)
	if len(rows) != 2 || len(idx) != 2 {
		t.Fatalf("rows=%d idx=%d", len(rows), len(idx))
	}
	if !strings.Contains(rows[0], "v2.0.0") || !strings.Contains(rows[0], "release two") {
		t.Fatalf("annotated row wrong: %q", rows[0])
	}
	// Annotated marker ● vs lightweight ○.
	if !strings.Contains(rows[0], "●") || !strings.Contains(rows[1], "○") {
		t.Fatalf("kind markers wrong: %q | %q", rows[0], rows[1])
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/tui/ -run TestTagRowsAndPanelView`
Expected: FAIL — `undefined: panelTags`.

- [ ] **Step 3: Add the `panelTags` enum value and `tags` field**

In `internal/tui/model.go`, add `panelTags` to the panel `const` block (before `panelCount`):

```go
	panelStaged
	panelCommits
	panelTags
	panelCount
)
```

In the `Model` struct, near `worktrees []model.Worktree` (~line 30) add:

```go
	tags []model.Tag // refs/tags; shown by the Tags tab in the middle slot
```

- [ ] **Step 4: Plumb tags through the load message**

In `internal/tui/load.go`, add to the `dataLoadedMsg` struct (near `worktrees`, ~line 40):

```go
	tags            []model.Tag
```

In the `out := dataLoadedMsg{...}` literal (~line 82), add:

```go
		tags:             snap.Tags,
```

In `internal/tui/model.go`, in the `dataLoadedMsg` handler next to `m.worktrees = msg.worktrees` (~line 304), add:

```go
			m.tags = msg.tags
```

- [ ] **Step 5: Add `tagRows()`**

Create `internal/tui/tags_view.go`:

```go
package tui

import (
	"strings"

	"github.com/gigagit/gg/internal/model"
)

// tagKindMark is the row prefix glyph: ● annotated, ○ lightweight.
func tagKindMark(t model.Tag) string {
	if t.Annotated {
		return "●"
	}
	return "○"
}

// tagRows renders one display row per tag: "<kind> <name>  <short-target>  <subject>".
func (m Model) tagRows() []string {
	rows := make([]string, len(m.tags))
	for i, t := range m.tags {
		row := tagKindMark(t) + " " + t.Name + "  " + shortHash(t.Target)
		if t.Subject != "" {
			row += "  " + t.Subject
		}
		rows[i] = row
	}
	return rows
}
```

(`shortHash` already exists in `files_view.go`; `Target` is already short from `for-each-ref %(objectname:short)`, so `shortHash` is a harmless idempotent trim.)

- [ ] **Step 6: Add `tagList` and the `listFor` case**

In `internal/tui/viewstate.go`, after `worktreeList` (~line 192), add:

```go
type tagList struct {
	items []model.Tag
	rows  []string
}

func (l tagList) Len() int          { return len(l.items) }
func (l tagList) Row(i int) string  { return l.rows[i] }
func (l tagList) Name(i int) string { return l.items[i].Name }
func (l tagList) Date(i int) int64  { return 0 } // no per-tag date in v1; default order is newest-first from git
func (l tagList) Key(i int) string  { return l.items[i].Name }
```

In `listFor` (~line 276), add a case before the `panelCommits` case:

```go
	case panelTags:
		return tagList{items: m.tags, rows: m.tagRows()}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/tui/ -run TestTagRowsAndPanelView`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd /mnt/t/others/gg-tags
git add internal/tui/model.go internal/tui/load.go internal/tui/tags_view.go internal/tui/viewstate.go internal/tui/tags_view_test.go
git commit -m "feat(tui): plumb tags into the model (panelTags, tagList, listFor)"
```

---

### Task 4: Middle tab slot — `activeFilesTab` + focus-aware `ctrl+←/→`

**Files:**
- Modify: `internal/tui/model.go` (`activeFilesTab` field; `filesTabs` var; `focusOrder`; the `ctrl+left`/`ctrl+right` handler ~line 695)
- Create: `internal/tui/middle_tab_test.go`

**Interfaces:**
- Consumes: `panelTags`, `panelFiles` (Task 3).
- Produces:
  - `m.activeFilesTab panel` (zero value resolves to `panelFiles`)
  - `var filesTabs = []panel{panelFiles, panelTags}`
  - `focusOrder()` uses `m.activeFilesTab` in the middle slot.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/middle_tab_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func ctrlRight(t *testing.T, m Model) Model {
	t.Helper()
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlRight})
	return u.(Model)
}

// ctrl+→ while the top slot owns focus still cycles the top slot.
func TestCtrlCycleTopSlotWhenTopFocused(t *testing.T) {
	m := New(nil)
	m.focus = panelBranches
	m.activeLeftTab = panelBranches
	m = ctrlRight(t, m)
	if m.activeLeftTab == panelBranches {
		t.Fatalf("top slot did not cycle: still %v", m.activeLeftTab)
	}
	if m.activeFilesTab == panelTags {
		t.Fatalf("middle slot must not change when top is focused")
	}
}

// ctrl+→ while the middle box owns focus cycles Files⇄Tags.
func TestCtrlCycleMiddleSlotWhenFilesFocused(t *testing.T) {
	m := New(nil)
	m.focus = panelFiles
	m = ctrlRight(t, m)
	if m.activeFilesTab != panelTags {
		t.Fatalf("middle slot did not switch to Tags: %v", m.activeFilesTab)
	}
	if m.focus != panelTags {
		t.Fatalf("focus must follow the now-active middle tab: %v", m.focus)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/tui/ -run 'TestCtrlCycle'`
Expected: FAIL — `undefined: m.activeFilesTab` / wrong cycle behavior.

- [ ] **Step 3: Add the field and the tab list**

In `internal/tui/model.go`, add to the `Model` struct (near `activeLeftTab`):

```go
	activeFilesTab panel // Files or Tags in the middle slot; zero value = panelFiles
```

Below `leftTabs` (~line 112) add:

```go
// filesTabs is the display/cycle order of the middle-slot tabs (the Files box).
var filesTabs = []panel{panelFiles, panelTags}
```

`panelFiles` is non-zero in the enum, so `activeFilesTab`'s zero value is `panelBranches`, NOT `panelFiles`. Resolve it with a helper — add near `focusOrder`:

```go
// middleTab is the active middle-slot panel, defaulting to Files when unset.
func (m Model) middleTab() panel {
	if m.activeFilesTab == panelFiles || m.activeFilesTab == panelTags {
		return m.activeFilesTab
	}
	return panelFiles
}
```

- [ ] **Step 4: Use the middle tab in `focusOrder`**

In `internal/tui/model.go`, change `focusOrder` (~line 1091) to use the middle tab instead of the hard-wired `panelFiles`:

```go
func (m Model) focusOrder() []panel {
	order := []panel{m.activeLeftTab, m.middleTab()}
	if m.layout().boxH[panelStaged] > 0 { // Staged is dropped on a short terminal
		order = append(order, panelStaged)
	}
	return append(order, panelCommits)
}
```

- [ ] **Step 5: Make `ctrl+←/→` focus-aware**

In `internal/tui/model.go`, replace the body of the `case "ctrl+left", "ctrl+right":` handler (~line 695) so it cycles whichever slot owns focus:

```go
		case "ctrl+left", "ctrl+right":
			// Cycle whichever tab slot currently owns focus: the top refs slot
			// (Branches·Remotes·Worktrees) or the middle files slot (Files·Tags).
			dir := 1
			if msg.String() == "ctrl+left" {
				dir = -1
			}
			if m.focus == panelFiles || m.focus == panelTags {
				m.activeFilesTab = nextInOrder(filesTabs, m.middleTab(), dir)
				m.focus = m.activeFilesTab
				return m, nil
			}
			m.activeLeftTab = nextInOrder(leftTabs, m.activeLeftTab, dir)
			m.focus = m.activeLeftTab
			m.lastLeftPanel = m.activeLeftTab
			return m, nil
```

(`nextInOrder` already exists, ~line 1100, and wraps correctly.)

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/tui/ -run 'TestCtrlCycle'`
Expected: PASS.

- [ ] **Step 7: Run the whole tui package (catch focus-order regressions)**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/tui/`
Expected: PASS (if a pre-existing test hard-codes `panelFiles` in a focus-cycle assertion, update it to `m.middleTab()` semantics — the default is still Files).

- [ ] **Step 8: Commit**

```bash
cd /mnt/t/others/gg-tags
git add internal/tui/model.go internal/tui/middle_tab_test.go
git commit -m "feat(tui): generalize the left tab slot into two (top refs + middle Files/Tags)"
```

---

### Task 5: Render the middle slot with a `[Files] Tags` tab bar

**Files:**
- Modify: `internal/tui/view.go` (the left-column composition, ~lines 337-348; add `filesTabLabel`)
- Create: `internal/tui/tags_render_test.go`

**Interfaces:**
- Consumes: `m.middleTab()`, `panelTags` rows via `panelView`.
- Produces: `filesTabLabel(active panel, count int) string` (renders `[Files 3] Tags` / `Files [Tags 12]`).

- [ ] **Step 1: Write the failing render test**

Create `internal/tui/tags_render_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestMiddleSlotRendersTagsWhenActive(t *testing.T) {
	m := New(nil)
	m.width = 100
	m.height = 30
	m.tags = []model.Tag{{Name: "v9.9.9", Target: "abcdef0", Annotated: true, Subject: "big"}}
	m.activeFilesTab = panelTags
	m.focus = panelTags
	out := m.View()
	if !strings.Contains(out, "v9.9.9") {
		t.Fatalf("Tags tab content not rendered:\n%s", out)
	}
	if !strings.Contains(out, "Tags") || !strings.Contains(out, "Files") {
		t.Fatalf("middle tab bar missing Files/Tags labels:\n%s", out)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/tui/ -run TestMiddleSlotRendersTagsWhenActive`
Expected: FAIL — Tags content not shown (the middle box still hard-renders `panelFiles`).

- [ ] **Step 3: Add `filesTabLabel`**

In `internal/tui/view.go`, near `tabBarLabel` (~line 380), add:

```go
// filesTabLabel is the middle-slot header: the active tab spelled out with its
// row count, the inactive tab shown plainly. Mirrors tabBarLabel for the top slot.
func filesTabLabel(active panel, filesN, tagsN int) string {
	files := fmt.Sprintf("Files %d", filesN)
	tags := fmt.Sprintf("Tags %d", tagsN)
	if active == panelTags {
		return files + " [" + tags + "]"
	}
	return "[" + files + "] " + tags
}
```

- [ ] **Step 4: Render the middle box from the active middle tab**

In `internal/tui/view.go`, in the `else` branch that builds `boxes` (~lines 337-348), replace the hard-wired Files box with the active middle tab:

```go
		active := m.activeLeftTab
		atRows, _ := m.panelView(active)
		mt := m.middleTab()
		mtRows, _ := m.panelView(mt)
		boxes := []string{
			m.renderPanel(active, m.panelLabel(active, tabBarLabel(active)), atRows, nil, g.leftW, g.boxH[active]),
			m.renderPanel(mt, m.panelLabel(mt, filesTabLabel(mt, m.panelLen(panelFiles), m.panelLen(panelTags))), mtRows, nil, g.leftW, g.boxH[panelFiles]),
		}
```

NOTE: the box height key stays `g.boxH[panelFiles]` (the middle slot reuses the Files box geometry). Verify the layout function (`m.layout()`) sizes `panelFiles`; the Tags tab borrows that height. If `boxH` is keyed per-panel and `panelTags` has no entry, this is correct as written (we explicitly use the `panelFiles` height for the middle box regardless of which tab is active).

- [ ] **Step 5: Run the render test to verify it passes**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/tui/ -run TestMiddleSlotRendersTagsWhenActive`
Expected: PASS.

- [ ] **Step 6: Eyeball it against a real repo (manual)**

```bash
cd /mnt/t/others/gg-tags && go build ./cmd/gg && (cd /tmp && rm -rf tagdemo && git init -q tagdemo && cd tagdemo && git commit -q --allow-empty -m c1 && git tag v1.0.0 && git tag -a v2.0.0 -m rel2 && /mnt/t/others/gg-tags/gg)
```
Press `ctrl+→` with the middle box focused; confirm the box header flips `[Files] Tags` ⇄ `Files [Tags]` and the tag list (`● v2.0.0`, `○ v1.0.0`) shows. `q` to quit.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gg-tags
git add internal/tui/view.go internal/tui/tags_render_test.go
git commit -m "feat(tui): render the middle slot as a Files/Tags tab bar"
```

---

### Task 6: `enter` on a tag jumps to its commit

**Files:**
- Create: `internal/tui/tags_actions.go` (`tagJumpToCommit`)
- Modify: `internal/tui/model.go` (the `case "enter":` handler ~line 667 — add a `panelTags` branch)
- Create: `internal/tui/tags_actions_test.go`

**Interfaces:**
- Consumes: `panelView(panelcommits)`, `m.commits`, `m.tags`, `backingIndex`.
- Produces: `(Model).tagJumpToCommit() (tea.Model, tea.Cmd)`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/tags_actions_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestEnterOnTagJumpsToCommit(t *testing.T) {
	m := New(nil)
	m.commits = []model.Commit{
		{Hash: "1111111aaaa", Subject: "one"},
		{Hash: "2222222bbbb", Subject: "two"},
	}
	m.tags = []model.Tag{{Name: "v1", Target: "2222222", Annotated: false}}
	m.activeFilesTab = panelTags
	m.focus = panelTags
	m.sel = map[panel]int{panelTags: 0}

	u, _ := m.Update(keyMsg("enter")) // keyMsg helper exists in the tui tests
	mm := u.(Model)
	if mm.focus != panelCommits {
		t.Fatalf("focus = %v, want panelCommits", mm.focus)
	}
	_, idx := mm.panelView(panelCommits)
	if got := idx[mm.sel[panelCommits]]; got != 1 {
		t.Fatalf("selected commit backing idx = %d, want 1 (the v1 target)", got)
	}
}

func TestEnterOnTagNotLoadedNotices(t *testing.T) {
	m := New(nil)
	m.commits = []model.Commit{{Hash: "1111111aaaa", Subject: "one"}}
	m.tags = []model.Tag{{Name: "v1", Target: "9999999"}}
	m.activeFilesTab = panelTags
	m.focus = panelTags
	m.sel = map[panel]int{panelTags: 0}
	u, _ := m.Update(keyMsg("enter"))
	if mm := u.(Model); mm.statusMsg == "" {
		t.Fatal("expected a 'tag target not loaded' notice")
	}
}
```

NOTE: confirm `keyMsg` is the tui test helper for a key string (it appears in `content_popup_test.go`). If the helper is named differently, use it.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/tui/ -run 'TestEnterOnTag'`
Expected: FAIL — enter on a tag does nothing.

- [ ] **Step 3: Add `tagJumpToCommit`**

Create `internal/tui/tags_actions.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// tagJumpToCommit moves the Commits cursor to the selected tag's target commit
// (matched by short-hash prefix) and focuses the Commits panel. A target that
// isn't in the loaded commit page leaves a notice (never-trap: no-op + explain).
func (m Model) tagJumpToCommit() (tea.Model, tea.Cmd) {
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return m, nil
	}
	target := m.tags[bi].Target
	_, idx := m.panelView(panelCommits)
	for di, ci := range idx {
		if ci >= 0 && ci < len(m.commits) && strings.HasPrefix(m.commits[ci].Hash, target) {
			m.sel[panelCommits] = di
			m.focus = panelCommits
			return m, nil
		}
	}
	m.statusMsg = "tag " + m.tags[bi].Name + " target not in the loaded commits"
	return m, nil
}
```

- [ ] **Step 4: Route `enter` on the Tags panel**

In `internal/tui/model.go`, at the top of the `case "enter":` handler (~line 667, before the worktree check), add:

```go
			if m.focus == panelTags {
				return m.tagJumpToCommit()
			}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/tui/ -run 'TestEnterOnTag'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gg-tags
git add internal/tui/tags_actions.go internal/tui/model.go internal/tui/tags_actions_test.go
git commit -m "feat(tui): enter on a tag jumps to its target commit"
```

---

### Task 7: `gg tag ls` CLI + e2e scenario

**Files:**
- Create: `internal/cli/tag.go`
- Modify: `internal/cli/cli.go` (route `case "tag":` ~line 82; help/usage text)
- Create: `internal/cli/tag_test.go`
- Create: `e2e/scenarios/s57_tag_ls.toml`

**Interfaces:**
- Consumes: `(*domain.Service).Tags` (Task 2).
- Produces: `gg tag ls` printing one tag name per line.

- [ ] **Step 1: Write the failing CLI test**

Create `internal/cli/tag_test.go` (mirror the structure of `internal/cli/remote_test.go` if present; otherwise an existing cli test that builds a real repo + svc):

```go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCmdTagList(t *testing.T) {
	dir, svc := newCLIRepo(t) // reuse this package's real-git test helper
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	gitIn(t, dir, "tag", "v1.0.0")
	gitIn(t, dir, "tag", "-a", "v2.0.0", "-m", "rel2")

	var out, errb bytes.Buffer
	code := cmdTag(svc, []string{"ls"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d, stderr=%s", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "v1.0.0") || !strings.Contains(got, "v2.0.0") {
		t.Fatalf("tag ls output = %q", got)
	}
}
```

NOTE: replace `newCLIRepo`/`gitIn` with this package's actual helper names (check `internal/cli/*_test.go`).

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/cli/ -run TestCmdTagList`
Expected: FAIL — `undefined: cmdTag`.

- [ ] **Step 3: Write the CLI command**

Create `internal/cli/tag.go`:

```go
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
)

// cmdTag dispatches the tag subcommands. Stage 1: ls only.
func cmdTag(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0 || args[0] == "ls" || args[0] == "list":
		return cmdTagList(svc, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "tag: unknown subcommand %q (try: ls)\n", args[0])
		return 2
	}
}

// cmdTagList prints each tag name, one per line (newest first).
func cmdTagList(svc *domain.Service, stdout, stderr io.Writer) int {
	tags, err := svc.Tags(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, t := range tags {
		fmt.Fprintln(stdout, t.Name)
	}
	return 0
}
```

- [ ] **Step 4: Route `tag` in `cli.go`**

In `internal/cli/cli.go`, add a case next to `case "remote":` (~line 82):

```go
	case "tag":
		return cmdTag(svc, args[1:], stdout, stderr)
```

Also add `tag` to the usage/help text block in this file (search for the `remote` usage line and add an adjacent `gg tag ls` line).

- [ ] **Step 5: Run the CLI test to verify it passes**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/cli/ -run TestCmdTagList`
Expected: PASS.

- [ ] **Step 6: Write the e2e scenario**

Open an existing tag-free list scenario for the exact schema first: `e2e/scenarios/s48_remote_ls.toml`. Mirror it. Create `e2e/scenarios/s57_tag_ls.toml`:

```toml
name = "tag ls lists tags newest-first"

[[steps]]
run = "git commit --allow-empty -m c1"

[[steps]]
run = "git tag v1.0.0"

[[steps]]
run = "git tag -a v2.0.0 -m rel2"

[[steps]]
gg = "tag ls"
stdout_contains = ["v1.0.0", "v2.0.0"]
```

NOTE: match the EXACT key names the harness uses (`run`/`gg`/`stdout_contains`) by copying `s48_remote_ls.toml`'s schema — adjust if it differs. The `writing-e2e-scenarios` skill documents the schema.

- [ ] **Step 7: Run the e2e scenario**

Run: `cd /mnt/t/others/gg-tags && go test ./e2e/ -run 'TagLs|s57' -v` (or `./test.sh e2e`)
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd /mnt/t/others/gg-tags
git add internal/cli/tag.go internal/cli/cli.go internal/cli/tag_test.go e2e/scenarios/s57_tag_ls.toml
git commit -m "feat(cli): gg tag ls + e2e scenario"
```

---

### Task 8: Advertise + docs + agentskill

**Files:**
- Modify: `internal/tui/help.go` (Tags tab + `enter`-jump + `ctrl+←/→` middle-slot note)
- Modify: the context-help footer (search `internal/tui/footer.go` for the panel-keyed hints; add a `panelTags` hint)
- Modify: `internal/agentskill/using-gg.md` (document `gg tag ls`)
- Modify: `internal/agentskill/agentskill.go` (bump `Version`)
- Modify: `CHANGELOG.md` (Unreleased → Added)
- Modify: `README.md` (if the user-facing TUI/CLI surface table lists tabs/commands)

**Interfaces:** none (docs/help only).

- [ ] **Step 1: Add help + footer entries**

In `internal/tui/help.go`, add lines describing: the **Tags** tab in the middle slot, `ctrl+←/→` cycles the focused slot (top refs / middle Files·Tags), and `enter` on a tag jumps to its commit. In the footer (search `footer.go` for `case panelWorktrees:` / `case panelBranches:`), add a `case panelTags:` hint, e.g. `"[enter] go to commit  [ctrl+←/→] switch tab  [/] filter"`. Keep it tight — the footer truncates to width (memory: advertise-features-in-help-and-footer).

- [ ] **Step 2: Add a help-presence test**

Append to `internal/tui/tags_render_test.go`:

```go
func TestHelpMentionsTags(t *testing.T) {
	m := New(nil)
	m.width = 100
	m.height = 30
	m.showHelp = true // set the field/flow help.go uses to render the ? pane
	out := m.View()
	if !strings.Contains(out, "Tags") {
		t.Fatalf("help pane does not mention Tags:\n%s", out)
	}
}
```

NOTE: confirm how the help pane is opened in tests (the field may be `m.help`/`m.showHelp` or a pushed overlay — check an existing help test in `internal/tui`). Adjust to match; the assertion (help text contains "Tags") is the point.

- [ ] **Step 3: Run the help test**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/tui/ -run TestHelpMentionsTags`
Expected: PASS.

- [ ] **Step 4: Document `gg tag ls` in the agent skill + bump the version**

In `internal/agentskill/using-gg.md`, add `gg tag ls` to the command reference (near `gg remote ls`). In `internal/agentskill/agentskill.go`, bump `Version` by one (find the current `const Version = N`).

- [ ] **Step 5: Refresh the dogfood SKILL.md (the sync gate)**

```bash
cd /mnt/t/others/gg-tags && go build ./cmd/gg && ./gg init --update
```
This regenerates the installed `SKILL.md` copy. Without it, `TestDogfoodSkillCopyInSync` fails the suite (memory: commit-ops-pipeline-backlog gotcha).

- [ ] **Step 6: Verify the sync test passes**

Run: `cd /mnt/t/others/gg-tags && go test ./internal/agentskill/ -run TestDogfoodSkillCopyInSync`
Expected: PASS.

- [ ] **Step 7: Update CHANGELOG + README**

In `CHANGELOG.md` under `## [Unreleased]` → `### Added`, add:

```markdown
- **Tags.** A read-only **Tags** tab in the middle (Files) window — switch
  Files ⇄ Tags with `ctrl+←/→` while that box is focused; each row shows the
  tag (`●` annotated / `○` lightweight), its target, and subject. `enter` jumps
  to the tag's commit in the Commits panel. New `gg tag ls` CLI command.
```

If `README.md` lists the TUI tabs or CLI commands, add Tags / `gg tag ls` there too.

- [ ] **Step 8: Commit**

```bash
cd /mnt/t/others/gg-tags
git add internal/tui/help.go internal/tui/footer.go internal/tui/tags_render_test.go internal/agentskill/ CHANGELOG.md README.md
git commit -m "docs(tags): advertise the Tags tab + gg tag ls; agentskill bump"
```

---

### Final gate: full suite

- [ ] **Step 1: Run the full race suite + e2e**

Run: `cd /mnt/t/others/gg-tags && ./test.sh race`
Expected: `all green`.

- [ ] **Step 2: Hand back for merge**

Report green; the human merges `tags-stage1` into `main` (do not self-merge unless asked). Then Stage 2 (Create) starts on a fresh branch.

---

## Self-Review

**Spec coverage** (against `docs/superpowers/specs/2026-06-21-tags-design.md`, Stage 1 section):
- git verb `Tags()` → Task 1. ✅
- domain `Tags()` query + Snapshot → Task 2. ✅
- `panelTags` + row format (`●`/`○ name short-target subject`) + `tagList` → Task 3. ✅
- Middle slot `[Files] Tags`, focus-aware `ctrl+←/→`, Staged separate → Tasks 4-5. ✅ (Staged box untouched — `focusOrder` still appends it independently.)
- `/`-filter + sort → inherited free from `panelList`/`panelView` registration in Task 3 (no extra code; `tagList.Name` gives name-sort, filter matches `Row`). ✅
- `enter` jumps to target commit (notice when not loaded) → Task 6. ✅
- `gg tag ls` + e2e → Task 7. ✅
- agentskill bump + `gg init --update`, CHANGELOG, help/footer → Task 8. ✅

**Placeholder scan:** No "TBD"/"add error handling"/"similar to Task N". The `NOTE:`s flag exact-helper-name confirmations (real-git test helpers, `keyMsg`, e2e schema keys, help-pane field) the implementer verifies against existing siblings — each names the file to copy from. Acceptable: they are verification anchors, not missing logic.

**Type consistency:** `model.Tag{Name,Target,Annotated,Subject}` used identically in Tasks 1-3, 6-7. `panelTags`, `m.tags`, `m.activeFilesTab`, `m.middleTab()`, `filesTabs`, `tagList`, `tagRows()`, `tagJumpToCommit()`, `filesTabLabel()`, `cmdTag()` — each defined once and consumed with matching signatures. `Date(i)` returns 0 for tags (no per-tag date in v1) — consistent with the `panelList` contract (0 = unknown, sorts last).

**Scope:** Stage 1 (Read) only. Create/Delete/Checkout/Push are explicitly later stages, each its own plan.
