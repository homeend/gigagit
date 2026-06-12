# Commit Files View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `l` on the Commits panel replaces the three left panels with a follow-live file tree of the selected commit (spec: `docs/superpowers/specs/2026-06-12-commit-files-view-design.md`).

**Architecture:** A new git verb (`CommitFiles` via one `git diff-tree` invocation) feeds a pure tree-builder (`commitFileLines`) that produces `contentLine`s for a reused `contentPopup` instance stored in a new `Model.filesView` field. A thin key-routing layer splits keys between the commit list (j/k follow-live) and the tree (scroll/search); a new renderer draws the popup struct in the left column's geometry instead of as an overlay.

**Tech Stack:** Go 1.26, Bubble Tea + lipgloss, system git via `gitexec.Runner` (`FakeRunner` + real `t.TempDir()` repos in tests).

**Branch:** `feat/commit-files-view` off `main`.

**Conventions that apply to every task:** TDD (write the failing test first, watch it fail, then implement); run `gofmt -w` on touched files before committing; commit messages end with
`Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

## File map

| File | Change |
|---|---|
| `internal/model/model.go` | add `CommitFile` type |
| `internal/git/log.go` | add `CommitFiles` verb + `ParseNameStatus` parser |
| `internal/git/log_test.go` | parser, argv, real-repo tests |
| `internal/tui/files_view.go` | **new** — tree builder, load cmd/msg, key handler, renderer |
| `internal/tui/files_view_test.go` | **new** — all TUI tests for the feature |
| `internal/tui/model.go` | 3 new Model fields, `l` key, routing branch, `commitFilesMsg` case, mouse wheel, reRoot clear |
| `internal/tui/view.go` | left-column swap in `renderInterface` |
| `internal/tui/help.go` | `Commits panel` + `Commit files view (l)` sections |
| `CHANGELOG.md`, `README.md`, `.claude/skills/adding-tui-windows/SKILL.md` | docs |

---

### Task 1: `CommitFiles` git verb + `ParseNameStatus` parser

**Files:**
- Modify: `internal/model/model.go` (after the `Commit` type, ~line 90)
- Modify: `internal/git/log.go`
- Test: `internal/git/log_test.go`

**Context:** `internal/git` holds thin verbs on `*git.Repo`; one verb = one git invocation built with `gitcmd.New(...)` and run via `r.Runner.Run(ctx, "<label>", argv)`. The label (`"git diff-tree"`) is also the `FakeRunner.SetResponse` key. Parsers are pure package-level functions (see `ParseLog` in the same file). `newTestRepo(t)` (in `repo_test.go`) creates a temp repo whose **initial commit contains `README.md`**, and returns `(dir, runner)`.

- [ ] **Step 1: Add the model type**

In `internal/model/model.go`, directly after the `Commit` type:

```go
// CommitFile is one changed path within a commit.
type CommitFile struct {
	Status  string // single letter: A M D R C T (score stripped from R/C)
	Path    string // new path
	OldPath string // set only for renames/copies
}
```

- [ ] **Step 2: Write the failing parser test**

Append to `internal/git/log_test.go`:

```go
func TestParseNameStatus(t *testing.T) {
	raw := []byte("M\tCHANGELOG.md\n" +
		"A\tinternal/tui/files_view.go\n" +
		"D\told.txt\n" +
		"R100\ta/old.go\tb/new.go\n" +
		"T\tlink\n" +
		"\n" +
		"bogus-line-without-tab\n")
	got := ParseNameStatus(raw)
	want := []model.CommitFile{
		{Status: "M", Path: "CHANGELOG.md"},
		{Status: "A", Path: "internal/tui/files_view.go"},
		{Status: "D", Path: "old.txt"},
		{Status: "R", Path: "b/new.go", OldPath: "a/old.go"},
		{Status: "T", Path: "link"},
	}
	if len(got) != len(want) {
		t.Fatalf("files = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("file[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
```

Add `"github.com/gigagit/gg/internal/model"` to the test file's imports.

- [ ] **Step 3: Run it to verify it fails**

Run: `go test ./internal/git -run TestParseNameStatus -v`
Expected: FAIL — `undefined: ParseNameStatus` (and `model.CommitFile` compiles from Step 1).

- [ ] **Step 4: Implement verb + parser**

Append to `internal/git/log.go`:

```go
// CommitFiles returns the files changed by commit hash, in git's path order.
// One invocation. --first-parent -m makes merge commits show their diff
// against the first parent (plain diff-tree prints nothing for merges);
// --root makes the initial commit list its files.
func (r *Repo) CommitFiles(ctx context.Context, hash string) ([]model.CommitFile, error) {
	argv := gitcmd.New("diff-tree").
		Arg("-r", "--root", "--no-commit-id", "--name-status", "-M", "--first-parent", "-m", hash).
		ToArgv()
	res, err := r.Runner.Run(ctx, "git diff-tree", argv)
	if err != nil {
		return nil, err
	}
	return ParseNameStatus([]byte(res.Stdout)), nil
}

// ParseNameStatus parses `--name-status` lines: "M\tpath" or, for renames
// and copies, "R<score>\told\tnew". Blank and malformed lines are skipped;
// the status letter is the first byte of the status field.
func ParseNameStatus(data []byte) []model.CommitFile {
	var out []model.CommitFile
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 2 || f[0] == "" || f[1] == "" {
			continue
		}
		cf := model.CommitFile{Status: f[0][:1], Path: f[1]}
		if (cf.Status == "R" || cf.Status == "C") && len(f) >= 3 && f[2] != "" {
			cf.OldPath = f[1]
			cf.Path = f[2]
		}
		out = append(out, cf)
	}
	return out
}
```

- [ ] **Step 5: Run the parser test to verify it passes**

Run: `go test ./internal/git -run TestParseNameStatus -v`
Expected: PASS

- [ ] **Step 6: Write the argv + real-repo tests**

Append to `internal/git/log_test.go`:

```go
func TestCommitFilesArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff-tree", gitexec.Result{Stdout: "M\tfile.txt\n"})
	repo := &Repo{Runner: f}
	got, err := repo.CommitFiles(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("git calls = %d, want 1", len(f.Calls))
	}
	argv := strings.Join(f.Calls[0].Argv, " ")
	for _, part := range []string{"diff-tree", "-r", "--root", "--no-commit-id", "--name-status", "-M", "--first-parent", "-m", "abc123"} {
		if !strings.Contains(argv, part) {
			t.Fatalf("argv = %q, missing %q", argv, part)
		}
	}
	if len(got) != 1 || got[0] != (model.CommitFile{Status: "M", Path: "file.txt"}) {
		t.Fatalf("files = %+v", got)
	}
}

func TestCommitFilesRealRepo(t *testing.T) {
	dir, runner := newTestRepo(t) // initial commit contains README.md
	repo := &Repo{Runner: runner}

	// Root commit lists its files (--root).
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	root := strings.TrimSpace(string(out))
	files, err := repo.CommitFiles(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Status != "A" || files[0].Path != "README.md" {
		t.Fatalf("root commit files = %+v, want [A README.md]", files)
	}

	// A rename commit reports R with both paths.
	gitIn(t, dir, "mv", "README.md", "DOCS.md")
	gitIn(t, dir, "commit", "-m", "rename")
	out, err = exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(string(out))
	files, err = repo.CommitFiles(context.Background(), head)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Status != "R" || files[0].OldPath != "README.md" || files[0].Path != "DOCS.md" {
		t.Fatalf("rename commit files = %+v, want [R README.md -> DOCS.md]", files)
	}
}
```

(`gitIn(t, dir, args...)` is the existing helper that runs git inside the test repo; `exec` and `gitexec` are already imported by this test file.)

- [ ] **Step 7: Run all the new tests**

Run: `go test ./internal/git -run 'TestParseNameStatus|TestCommitFiles' -v`
Expected: PASS (3 tests)

- [ ] **Step 8: Vet + commit**

```bash
go vet ./... && gofmt -l internal cmd
git add internal/model/model.go internal/git/log.go internal/git/log_test.go
git commit -m "feat(git): CommitFiles verb — name-status file list of one commit"
```

---

### Task 2: `commitFileLines` tree builder

**Files:**
- Create: `internal/tui/files_view.go`
- Test: `internal/tui/files_view_test.go` (new)

**Context:** `contentLine{text string, heading bool}` is defined in `internal/tui/content_popup.go`; headings render bold and survive filtering only while a non-heading line beneath them matches. The builder must emit each directory heading **exactly once** — sorting by full path is NOT enough (`a/b/f, a/c, a/d/g, a/e` path-sorted interleaves dir `a` with its subdirs), so sort dir-major.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/files_view_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestCommitFileLinesGroupsByDirectory(t *testing.T) {
	files := []model.CommitFile{
		{Status: "M", Path: "internal/tui/model.go"},
		{Status: "A", Path: "internal/engine/smart_merge.go"},
		{Status: "M", Path: "CHANGELOG.md"},
		{Status: "A", Path: "internal/tui/mark.go"},
	}
	got := commitFileLines(files)
	want := []contentLine{
		{text: "M  CHANGELOG.md"},
		{text: "internal/engine/", heading: true},
		{text: "  A  smart_merge.go"},
		{text: "internal/tui/", heading: true},
		{text: "  A  mark.go"},
		{text: "  M  model.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("lines = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCommitFileLinesEmitsEachDirHeadingOnce(t *testing.T) {
	// Path-sorted these interleave dir "a" with its subdirs (a/b/f < a/c.go
	// < a/d/g < a/e.go); the dir-major sort must emit heading "a/" once.
	files := []model.CommitFile{
		{Status: "M", Path: "a/c.go"},
		{Status: "M", Path: "a/b/f.go"},
		{Status: "M", Path: "a/e.go"},
		{Status: "M", Path: "a/d/g.go"},
	}
	got := commitFileLines(files)
	count := 0
	for _, l := range got {
		if l.heading && l.text == "a/" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("heading \"a/\" emitted %d times, want 1: %+v", count, got)
	}
}

func TestCommitFileLinesRename(t *testing.T) {
	files := []model.CommitFile{{Status: "R", Path: "b/new.go", OldPath: "a/old.go"}}
	got := commitFileLines(files)
	want := []contentLine{
		{text: "b/", heading: true},
		{text: "  R  a/old.go → new.go"},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("lines = %+v, want %+v", got, want)
	}
}

func TestCommitFileLinesEmpty(t *testing.T) {
	got := commitFileLines(nil)
	if len(got) != 1 || got[0].heading || got[0].text != "(no files)" {
		t.Fatalf("lines = %+v, want one non-heading \"(no files)\"", got)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run TestCommitFileLines -v`
Expected: FAIL — `undefined: commitFileLines`

- [ ] **Step 3: Implement the builder**

Create `internal/tui/files_view.go`:

```go
package tui

import (
	"path"
	"sort"

	"github.com/gigagit/gg/internal/model"
)

// commitFileLines renders a commit's changed files as content lines:
// root-level files first (no heading), then one bold heading per directory
// (its full path) with the directory's files indented beneath. Exactly one
// heading level — no nesting. Sorting is dir-major because a plain path sort
// interleaves a directory's files with its subdirectories, which would emit
// the same heading twice.
func commitFileLines(files []model.CommitFile) []contentLine {
	if len(files) == 0 {
		return []contentLine{{text: "(no files)"}}
	}
	sorted := make([]model.CommitFile, len(files))
	copy(sorted, files)
	sort.SliceStable(sorted, func(a, b int) bool {
		da, db := path.Dir(sorted[a].Path), path.Dir(sorted[b].Path)
		if da != db {
			return da < db // "." sorts before any directory name
		}
		return sorted[a].Path < sorted[b].Path
	})

	out := make([]contentLine, 0, len(sorted))
	lastDir := ""
	for _, f := range sorted {
		dir := path.Dir(f.Path)
		if dir == "." {
			out = append(out, contentLine{text: fileLine(f)})
			continue
		}
		if dir != lastDir {
			out = append(out, contentLine{text: dir + "/", heading: true})
			lastDir = dir
		}
		out = append(out, contentLine{text: "  " + fileLine(f)})
	}
	return out
}

// fileLine renders one file row: "<letter>  <basename>"; renames show the
// full old path and the new basename.
func fileLine(f model.CommitFile) string {
	if f.OldPath != "" {
		return f.Status + "  " + f.OldPath + " → " + path.Base(f.Path)
	}
	return f.Status + "  " + path.Base(f.Path)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui -run TestCommitFileLines -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/tui/files_view.go internal/tui/files_view_test.go
git commit -m "feat(tui): commitFileLines — dir-grouped content lines for a commit's files"
```

---

### Task 3: Model wiring — `l` key, follow-live loading, key routing

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/files_view.go` (append)
- Test: `internal/tui/files_view_test.go` (append)

**Context:** `Model` is a value receiver; state that must persist across the copy uses pointer fields (`contentPopup *contentPopup` is the template). The key-routing chain in `Update` checks popups in order (`modal → popup → repoPopup → settings → branchPopup → contentPopup → pairPopup → …`); the files view slots in **after `pairPopup`, before `filterTyping`**. Async loads follow the `loadCmd` pattern: a `tea.Cmd` closure captures `m.repo`, returns a typed msg; tests invoke the cmd function directly (`m.Update(cmd())`). Test helpers `pressRune`/`pressType` live in `mark_test.go`. `FakeRunner` responses are keyed by the verb label (`"git diff-tree"`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/files_view_test.go` (extend the import block with `tea "github.com/charmbracelet/bubbletea"`, `"strings"`, `"github.com/gigagit/gg/internal/git"`, `"github.com/gigagit/gg/internal/gitexec"`):

```go
// filesModel returns a model focused on the Commits panel whose FakeRunner
// answers diff-tree with a two-directory file list.
func filesModel() Model {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff-tree", gitexec.Result{
		Stdout: "M\tinternal/tui/model.go\nA\tCHANGELOG.md\n",
	})
	return Model{
		repo:   &git.Repo{Runner: f},
		width:  80,
		height: 24,
		commits: []model.Commit{
			{Hash: "1111111aaaa", Subject: "one"},
			{Hash: "2222222bbbb", Subject: "two"},
		},
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		focus:     panelCommits,
	}
}

// openFilesView presses l and feeds the async result back into Update.
func openFilesView(t *testing.T, m Model) Model {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("l on the commits panel must fire the files load")
	}
	updated, _ = m.Update(cmd())
	return updated.(Model)
}

func TestFilesViewOpensOnCommitsPanel(t *testing.T) {
	m := openFilesView(t, filesModel())
	if m.filesView == nil {
		t.Fatal("filesView must be open")
	}
	if m.filesTitle != "Files 1111111 one" {
		t.Fatalf("title = %q", m.filesTitle)
	}
	joined := ""
	for _, l := range m.filesView.lines {
		joined += l.text + "\n"
	}
	if !strings.Contains(joined, "M  model.go") || !strings.Contains(joined, "A  CHANGELOG.md") {
		t.Fatalf("lines = %q", joined)
	}
}

func TestFilesViewNoOpOffCommitsPanel(t *testing.T) {
	m := filesModel()
	m.focus = panelBranches
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(Model)
	if m.filesView != nil || cmd != nil {
		t.Fatal("l must no-op off the commits panel")
	}
}

func TestFilesViewNoOpOnEmptyCommits(t *testing.T) {
	m := filesModel()
	m.commits = nil
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if updated.(Model).filesView != nil || cmd != nil {
		t.Fatal("l must no-op with no commits")
	}
}

func TestFilesViewNarrowTerminalNoOp(t *testing.T) {
	m := filesModel()
	m.width = 30 // layout has no left column below 40
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(Model)
	if m.filesView != nil || cmd != nil {
		t.Fatal("l must not open on a narrow terminal")
	}
	if !strings.Contains(m.statusMsg, "narrow") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestFilesViewFollowsCommitSelection(t *testing.T) {
	m := openFilesView(t, filesModel())
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	if m.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want 1 (j must keep moving commits)", m.sel[panelCommits])
	}
	if m.filesHash != "2222222bbbb" {
		t.Fatalf("filesHash = %q, want the new commit", m.filesHash)
	}
	if cmd == nil {
		t.Fatal("moving the selection must fire a follow-live reload")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.filesTitle != "Files 2222222 two" {
		t.Fatalf("title = %q after follow-live reload", m.filesTitle)
	}
}

func TestFilesViewDropsStaleResult(t *testing.T) {
	m := openFilesView(t, filesModel())
	updated, _ := m.Update(commitFilesMsg{
		hash:    "zzzstale",
		subject: "stale",
		files:   []model.CommitFile{{Status: "A", Path: "stale.txt"}},
	})
	m = updated.(Model)
	if m.filesTitle == "Files zzzstal stale" || strings.Contains(m.filesTitle, "stale") {
		t.Fatalf("stale result applied: title = %q", m.filesTitle)
	}
	for _, l := range m.filesView.lines {
		if strings.Contains(l.text, "stale.txt") {
			t.Fatal("stale result applied: lines updated")
		}
	}
}

func TestFilesViewSearchNarrowsAndKeepsHeading(t *testing.T) {
	m := openFilesView(t, filesModel())
	m = pressRune(t, m, "/")
	m = pressRune(t, m, "model")
	vis := m.filesView.visible()
	joined := ""
	for _, l := range vis {
		joined += l.text + "\n"
	}
	if !strings.Contains(joined, "internal/tui/") || !strings.Contains(joined, "model.go") {
		t.Fatalf("visible = %q, want heading + match", joined)
	}
	if strings.Contains(joined, "CHANGELOG") {
		t.Fatalf("visible = %q, must not contain non-match", joined)
	}
}

func TestFilesViewQuerySurvivesCommitChange(t *testing.T) {
	m := openFilesView(t, filesModel())
	m = pressRune(t, m, "/")
	m = pressRune(t, m, "model")
	m = pressType(t, m, tea.KeyEnter) // commit the search
	m.filesView.sel = 3               // pretend the cursor moved
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.filesView.query != "model" {
		t.Fatalf("query = %q, must survive the commit change", m.filesView.query)
	}
	if m.filesView.sel != 0 {
		t.Fatalf("sel = %d, must reset on new content", m.filesView.sel)
	}
}

func TestFilesViewEscClearsSearchThenCloses(t *testing.T) {
	m := openFilesView(t, filesModel())
	m = pressRune(t, m, "/")
	m = pressRune(t, m, "mo")
	m = pressType(t, m, tea.KeyEnter)
	m = pressType(t, m, tea.KeyEsc) // 1st esc: clear the committed search
	if m.filesView == nil || m.filesView.query != "" {
		t.Fatal("first esc must clear the search, not close")
	}
	m = pressType(t, m, tea.KeyEsc) // 2nd esc: close
	if m.filesView != nil {
		t.Fatal("second esc must close the view")
	}
}

func TestFilesViewToggleClosesOnL(t *testing.T) {
	m := openFilesView(t, filesModel())
	m = pressRune(t, m, "l")
	if m.filesView != nil {
		t.Fatal("l must toggle the view closed")
	}
}

func TestFilesViewSwallowsActionKeys(t *testing.T) {
	m := openFilesView(t, filesModel())
	for _, key := range []string{"p", "s", "m", "b", "d", "w", "o", "R", ",", "r", "?"} {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		mm := updated.(Model)
		if cmd != nil || mm.running || mm.mark != nil || mm.contentPopup != nil {
			t.Fatalf("key %q must be swallowed while the view is open", key)
		}
	}
	before := m.focus
	m = pressType(t, m, tea.KeyTab)
	if m.focus != before || m.filesView == nil {
		t.Fatal("tab must be swallowed while the view is open")
	}
}

func TestFilesViewScrollKeys(t *testing.T) {
	// keyMsg is the existing helper in model_test.go that builds a
	// tea.KeyMsg from its String() form (it supports ctrl+up/ctrl+down).
	m := openFilesView(t, filesModel())
	u, _ := m.Update(keyMsg("ctrl+down"))
	m = u.(Model)
	if m.filesView.sel != 1 {
		t.Fatalf("sel = %d after ctrl+down, want 1", m.filesView.sel)
	}
	u, _ = m.Update(keyMsg("ctrl+up"))
	m = u.(Model)
	if m.filesView.sel != 0 {
		t.Fatalf("sel = %d after ctrl+up, want 0", m.filesView.sel)
	}
}

func TestReRootClearsFilesView(t *testing.T) {
	m := openFilesView(t, filesModel())
	updated, _ := m.reRoot(t.TempDir())
	if updated.(Model).filesView != nil {
		t.Fatal("reRoot must clear the files view")
	}
}
```

Note: `pressRune(t, m, "model")` sends all runes in one `tea.KeyRunes` msg — the typing-mode handler appends `string(msg.Runes)`, so multi-rune strings work (same trick as the filter tests).

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run TestFilesView -v`
Expected: FAIL — `m.filesView undefined` (compile error)

- [ ] **Step 3: Add the Model fields**

In `internal/tui/model.go`, after the `pairPopup` field block:

```go
	filesView  *contentPopup // commit files tree replacing the left column; nil = closed
	filesTitle string        // "Files <short-hash> <subject>", updated with the content
	filesHash  string        // commit the view wants; gates stale async results
```

- [ ] **Step 4: Implement load cmd, msg, key handler**

Append to `internal/tui/files_view.go` (extend its imports with `"context"`, `tea "github.com/charmbracelet/bubbletea"`):

```go
// commitFilesMsg carries one commit's changed files, tagged with the hash so
// stale results from fast j/k movement can be dropped.
type commitFilesMsg struct {
	hash    string
	subject string
	files   []model.CommitFile
	err     error
}

// loadCommitFilesCmd fetches the changed files of commit c off the UI thread.
func (m Model) loadCommitFilesCmd(c model.Commit) tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		files, err := repo.CommitFiles(context.Background(), c.Hash)
		return commitFilesMsg{hash: c.Hash, subject: c.Subject, files: files, err: err}
	}
}

// shortHash truncates a sha to 7 characters for display.
func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// filesPageRows is the tree's visible row capacity: the left column's box
// height minus borders (2), the title line (1), and the hint line (1).
func (m Model) filesPageRows() int {
	n := m.layout().bodyH - 4
	if n < 1 {
		n = 1
	}
	return n
}

// updateFilesViewKey routes keys while the files view is open: the commit
// list keeps selection movement (follow-live reload), the tree gets
// scroll/search keys, q/ctrl+c still quit, everything else is swallowed.
func (m Model) updateFilesViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.filesView
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.typing { // /-input mode captures every key (same as the help window)
		switch msg.Type {
		case tea.KeyEsc:
			p.typing = false
			p.query = ""
			p.sel = 0
		case tea.KeyEnter:
			p.typing = false // commit: search stays active
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
			}
			p.sel = 0
		case tea.KeySpace:
			p.query += " "
			p.sel = 0
		case tea.KeyRunes:
			p.query += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
	}
	switch msg.String() {
	case "q":
		return m, tea.Quit // q quits the app, view or not (top-level key)
	case "esc":
		if p.query != "" { // first esc clears the committed search
			p.query = ""
			p.sel = 0
			return m, nil
		}
		m.filesView = nil
		return m, nil
	case "l":
		m.filesView = nil
		return m, nil
	case "/":
		p.typing = true
		p.query = ""
		p.sel = 0
	case "up", "k":
		return m.moveCommitUnderFilesView(-1)
	case "down", "j":
		return m.moveCommitUnderFilesView(1)
	case "ctrl+up":
		p.move(-1)
	case "ctrl+down":
		p.move(1)
	case "pgup":
		p.move(-m.filesPageRows())
	case "pgdown":
		p.move(m.filesPageRows())
	}
	return m, nil
}

// moveCommitUnderFilesView shifts the Commits selection by delta and fires
// the follow-live reload when it lands on a different commit.
func (m Model) moveCommitUnderFilesView(delta int) (tea.Model, tea.Cmd) {
	n := m.panelLen(panelCommits)
	s := m.sel[panelCommits] + delta
	if s > n-1 {
		s = n - 1
	}
	if s < 0 {
		s = 0
	}
	if s == m.sel[panelCommits] {
		return m, nil
	}
	m.sel[panelCommits] = s
	bi, ok := m.backingIndex(panelCommits)
	if !ok || m.commits[bi].Hash == m.filesHash {
		return m, nil
	}
	m.filesHash = m.commits[bi].Hash
	return m, m.loadCommitFilesCmd(m.commits[bi])
}
```

- [ ] **Step 5: Wire model.go**

Five edits in `internal/tui/model.go`:

**(a)** Routing branch — after the `pairPopup` check (`if m.pairPopup != nil { … }`), before the `filterTyping` block:

```go
		if m.filesView != nil {
			return m.updateFilesViewKey(msg)
		}
```

**(b)** The `l` key — a new case in the normal-key switch (next to `case "m":`):

```go
		case "l":
			if !m.running && !m.loading && m.focus == panelCommits {
				if m.width > 0 && m.width < 40 {
					m.statusMsg = "terminal too narrow for the files view"
					return m, nil
				}
				if bi, ok := m.backingIndex(panelCommits); ok {
					c := m.commits[bi]
					m.filesView = &contentPopup{lines: []contentLine{{text: "(loading…)"}}}
					m.filesTitle = "Files " + shortHash(c.Hash) + " " + c.Subject
					m.filesHash = c.Hash
					return m, m.loadCommitFilesCmd(c)
				}
			}
```

**(c)** The result msg — a new top-level case in `Update` (next to `case dataLoadedMsg:`):

```go
	case commitFilesMsg:
		if m.filesView == nil || msg.hash != m.filesHash {
			return m, nil // view closed, or a stale result from fast movement
		}
		if msg.err != nil {
			m.statusMsg = "files: " + msg.err.Error()
			return m, nil
		}
		m.filesView.lines = commitFileLines(msg.files)
		m.filesView.sel = 0
		m.filesTitle = "Files " + shortHash(msg.hash) + " " + msg.subject
		return m, nil
```

**(d)** Mouse wheel — extend the `tea.MouseMsg` case so the wheel scrolls the files view when the help window is NOT open above it:

```go
	case tea.MouseMsg:
		// Mouse support is scoped to the content popup and the files view
		// (spec non-goal: no panel clicks/wheel).
		if m.contentPopup != nil && msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.contentPopup.move(-contentWheelStep)
			case tea.MouseButtonWheelDown:
				m.contentPopup.move(contentWheelStep)
			}
		} else if m.filesView != nil && msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.filesView.move(-contentWheelStep)
			case tea.MouseButtonWheelDown:
				m.filesView.move(contentWheelStep)
			}
		}
```

**(e)** `reRoot` — alongside `m.mark = nil`:

```go
	m.filesView = nil // the new repo has a different commit list
	m.filesHash = ""
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui -run 'TestFilesView|TestReRootClearsFilesView' -v`
Expected: PASS (13 tests)

- [ ] **Step 7: Run the whole tui package (regressions)**

Run: `go test ./internal/tui`
Expected: PASS

- [ ] **Step 8: Vet + commit**

```bash
go vet ./... && gofmt -l internal cmd
git add internal/tui/files_view.go internal/tui/files_view_test.go internal/tui/model.go
git commit -m "feat(tui): l opens a follow-live commit files view (model wiring)"
```

---

### Task 4: Rendering — files view replaces the left column

**Files:**
- Modify: `internal/tui/view.go` (`renderInterface`, ~line 196)
- Modify: `internal/tui/files_view.go` (append the renderer)
- Test: `internal/tui/files_view_test.go` (append)

**Context:** `renderInterface` builds `left` (three stacked `renderPanel` boxes, or two on short terminals) and joins it with the Commits panel. The renderer must produce a box of EXACTLY `g.leftW × bodyH` cells — `renderPanel` achieves fixed width by padding every line to `innerW` (`boxW - 4`: border 2 + padding 2) and fixed height by filling to `contentH` (`boxH - 2`). `windowRows(rows, n, sel)` returns the visible window. The border style is `bluredPanel` — focus stays on the Commits panel.

- [ ] **Step 1: Write the failing render tests**

Append to `internal/tui/files_view_test.go`:

```go
func TestFilesViewRenderReplacesLeftColumn(t *testing.T) {
	m := openFilesView(t, filesModel())
	out := m.render()
	for _, want := range []string{
		"Files 1111111 one", // title
		"internal/tui/",     // directory heading
		"M  model.go",       // file row with status letter
		"[/] search",        // hint line
		"Commits",           // the right panel is still there
		"2222222 two",       // and still lists commits
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{"Branches", "Worktrees", "Status"} {
		if strings.Contains(out, gone) {
			t.Fatalf("render still shows the %s panel:\n%s", gone, out)
		}
	}
}

func TestFilesViewRenderShowsSearchQuery(t *testing.T) {
	m := openFilesView(t, filesModel())
	m = pressRune(t, m, "/")
	m = pressRune(t, m, "mo")
	out := m.render()
	if !strings.Contains(out, "/mo█") {
		t.Fatalf("render missing the typing-mode query cursor:\n%s", out)
	}
}

func TestFilesViewRenderFitsTerminal(t *testing.T) {
	m := openFilesView(t, filesModel())
	out := m.render()
	lines := strings.Split(out, "\n")
	if len(lines) > 24 {
		t.Fatalf("render = %d lines, must fit height 24", len(lines))
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run TestFilesViewRender -v`
Expected: FAIL — render still shows Branches/Worktrees/Status

- [ ] **Step 3: Implement the renderer**

Append to `internal/tui/files_view.go` (extend imports with `"fmt"`, `"strings"`):

```go
// renderFilesView draws the commit files tree as one full-height left-column
// box; it replaces the Branches/Worktrees/Status panels while open. Blurred
// border: focus stays on the Commits panel.
func (m Model) renderFilesView(boxW, boxH int) string {
	p := m.filesView
	contentH := boxH - 2 // top/bottom border
	if contentH < 1 {
		contentH = 1
	}
	innerW := boxW - 4 // border (2) + horizontal padding (2)
	if innerW < 1 {
		innerW = 1
	}
	rowsCap := contentH - 2 // title + hint lines
	if rowsCap < 1 {
		rowsCap = 1
	}

	title := m.filesTitle
	if p.typing {
		title += " /" + p.query + "█"
	} else if p.query != "" {
		title += " /" + p.query
	}

	vis := p.visible()
	rows := make([]string, len(vis))
	for i, l := range vis {
		switch {
		case i == p.sel:
			// Cursor highlight wins over heading style so the cursor stays
			// visible when it rests on a heading row.
			rows[i] = selectedRow.Render(padRight(truncate("> "+l.text, innerW), innerW))
		case l.heading:
			rows[i] = titleStyle.Render(padRight(truncate(l.text, innerW), innerW))
		default:
			rows[i] = padRight(truncate("  "+l.text, innerW), innerW)
		}
	}
	win, _, _ := windowRows(rows, rowsCap, p.sel)

	lines := make([]string, 0, contentH)
	lines = append(lines, padRight(truncate(title, innerW), innerW))
	if len(win) == 0 {
		lines = append(lines, padRight(truncate("  (no match)", innerW), innerW))
	}
	lines = append(lines, win...)
	for len(lines) < contentH-1 {
		lines = append(lines, padRight("", innerW))
	}
	hint := "[/] search  [esc] close"
	if len(vis) > rowsCap {
		hint = fmt.Sprintf("%d/%d  %s", p.sel+1, len(vis), hint)
	}
	lines = append(lines, padRight(truncate(hint, innerW), innerW))

	return bluredPanel.Render(strings.Join(lines, "\n"))
}
```

- [ ] **Step 4: Wire renderInterface**

In `internal/tui/view.go`, replace the `var left string / if g.boxH[panelWorktrees] > 0 { … } else { … }` block with:

```go
	var left string
	switch {
	case m.filesView != nil:
		left = m.renderFilesView(g.leftW, g.bodyH)
	case g.boxH[panelWorktrees] > 0:
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(panelBranches, m.panelLabel(panelBranches, "Branches"), brRows, g.leftW, g.boxH[panelBranches]),
			m.renderPanel(panelWorktrees, m.panelLabel(panelWorktrees, "Worktrees"), wtRows, g.leftW, g.boxH[panelWorktrees]),
			m.renderPanel(panelStatus, m.panelLabel(panelStatus, "Status"), stRows, g.leftW, g.boxH[panelStatus]),
		)
	default:
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(panelBranches, m.panelLabel(panelBranches, "Branches"), brRows, g.leftW, g.boxH[panelBranches]),
			m.renderPanel(panelStatus, m.panelLabel(panelStatus, "Status"), stRows, g.leftW, g.boxH[panelStatus]),
		)
	}
```

(The narrow `g.w < 40` branch earlier in the function is untouched: there is no left column there, `l` never opens the view, and if a resize shrinks a terminal with the view open, the view is simply not drawn until the terminal widens — esc/l still close it.)

- [ ] **Step 5: Run the render tests**

Run: `go test ./internal/tui -run TestFilesViewRender -v`
Expected: PASS (3 tests)

- [ ] **Step 6: Run the whole tui package + fit tests**

Run: `go test ./internal/tui`
Expected: PASS (fit_test.go guards output dimensions — must stay green)

- [ ] **Step 7: Vet + commit**

```bash
go vet ./... && gofmt -l internal cmd
git add internal/tui/files_view.go internal/tui/files_view_test.go internal/tui/view.go
git commit -m "feat(tui): render the commit files view in place of the left panels"
```

---

### Task 5: Help window rows, docs, full gate

**Files:**
- Modify: `internal/tui/help.go`
- Modify: `CHANGELOG.md`, `README.md`, `.claude/skills/adding-tui-windows/SKILL.md`

**Context:** `helpContent()` in `help.go` is the hand-maintained key table behind `?`. `footerText` (view.go) is NOT changed — `l` is panel-scoped, the footer lists global keys, so `TestHelpFooterCoverage` is unaffected. No agent-skill bump: the CLI surface did not change.

- [ ] **Step 1: Add help sections**

In `internal/tui/help.go`:

**(a)** After the `h("Worktrees panel")` block (its last row is `r("d", "remove the selected worktree")`), insert:

```go
		h("Commits panel"),
		r("l", "show the selected commit's files in the left column"),
```

**(b)** After the `h("Pair-op popup (m on a second row)")` block (its last row is `r("esc", "close, keeping the mark")`), insert:

```go
		h("Commit files view (l)"),
		r("↑/k ↓/j", "move between commits (the file tree follows)"),
		r("ctrl+↑/↓", "scroll the file tree by 1 (pgup/pgdn: page; wheel: 3)"),
		r("/", "search file paths (enter keeps it, esc cancels)"),
		r("esc", "clear the search, then close"),
		r("l", "close"),
```

- [ ] **Step 2: Run the help drift guard**

Run: `go test ./internal/tui -run TestHelp -v`
Expected: PASS

- [ ] **Step 3: Update CHANGELOG.md**

Under `## [Unreleased]` / `### Added`, insert as the FIRST subsection (above `#### Mark-and-pair operations + SmartMerge`):

```markdown
#### Commit files view
- TUI: `l` on the Commits panel shows the selected commit's changed files as
  a directory-grouped tree in the left column (replacing the three left
  panels while open). Follow-live: j/k keeps moving through commits and the
  tree reloads for each one. `/` searches file paths, ctrl+↑/↓ / pgup/pgdn /
  mouse wheel scroll, esc/`l` close. Merge commits show their first-parent
  diff; renames render as `R old → new`.
```

- [ ] **Step 4: Update README.md**

README.md has a key table (one row per key, ~line 38). Insert this row directly after the `m` row (`| \`m\` | mark the selected row; …`):

```markdown
| `l` | on the Commits panel: show the selected commit's files as a directory tree in the left column (`j`/`k` keeps moving through commits and the tree follows; `/` searches paths; `ctrl+↑`/`ctrl+↓`, `pgup`/`pgdn`, mouse wheel scroll; `esc`/`l` close) |
```

- [ ] **Step 5: Update the adding-tui-windows skill**

In `.claude/skills/adding-tui-windows/SKILL.md`, after the "Pair-op popup (two-row operations)" section, add:

```markdown
## In-panel view (column replacement)

The commit files view (`l`) is the template for a view that REPLACES a panel
column instead of overlaying a popup: reuse the `contentPopup` struct (lines,
query, typing, sel, `visible()`, `move()`) for state + filtering, write a
dedicated `render<X>View(boxW, boxH)` that pads every line to `boxW-4` and
fills to `boxH-2` (exact box size, `bluredPanel` border — focus stays on the
surviving panel), branch in `renderInterface` where the column is built, and
add a routing branch in `Update` BEFORE `filterTyping` that splits keys
between the surviving panel and the view. Async per-row content loads carry
an identity tag (commit hash) so stale results from fast movement are
dropped. Clear the view in `reRoot`.
```

- [ ] **Step 6: Full gate**

Run: `./test.sh race`
Expected: all stages green (vet+gofmt → unit → e2e).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md README.md .claude/skills/adding-tui-windows/SKILL.md
git commit -m "docs(tui): help rows + docs for the commit files view"
```

---

## Final review checklist (for the orchestrator)

- Spec coverage: verb+parser (T1), tree builder (T2), open/follow-live/routing/invalidation (T3), rendering (T4), help+docs (T5). Narrow-terminal guard T3; stale-drop T3; query-survival T3; esc-order T3; swallowed keys T3.
- After all tasks: dispatch the final holistic reviewer, fix Important findings, then `superpowers:finishing-a-development-branch` (user picks; they usually merge themselves or ask for conflict-resolved merge).
