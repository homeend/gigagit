# Add-to-`.gitignore` Files-panel Actions — Implementation Plan

> **For agentic workers:** Steps use checkbox (`- [ ]`) syntax. TDD throughout:
> failing test → run-fails → minimal code → run-passes → commit.

**Goal:** Two TUI Files-panel `.`-menu actions on untracked files — *Add to
.gitignore* (exact file, anchored + escaped) and *Add extension to .gitignore*
(`*.ext`) — appending to the repo-root `.gitignore`, deduped, unstaged.

**Architecture:** New `engine.Ignore{Path, Ext}` op over the existing
`Repo.ReadWorktreeFile`/`WriteWorktreeFile` verbs (no new git verb). Thin TUI
accessors mirror `commitResetRow()`. Menu-only, no keybind. No CLI, no e2e.

**Tech Stack:** Go 1.26, Bubble Tea TUI, real-`git` engine tests.

## Global Constraints

- `internal/tui` MUST NOT import `internal/git` (archtest-guarded) — reach git
  through engine/domain only.
- Ops act on the `GitOps` interface; `*git.Repo` satisfies it.
- The exact-file pattern is `"/" + escapeIgnorePattern(path)`; the extension
  pattern is `"*" + path.Ext(path)` and is NOT escaped.
- `escapeIgnorePattern` backslash-escapes `\ * ? [`, escaping `\` first.
- Untracked-only gate: `f.Kind == model.KindUntracked`.
- Leave the `.gitignore` change unstaged.
- Commit trailers: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`
  and the `Claude-Session:` line.

---

### Task 1: Engine helpers (pure)

**Files:**
- Create: `internal/engine/ignore.go`
- Test: `internal/engine/ignore_test.go`

**Produces:** `ignoreLine(path string, ext bool) string`,
`escapeIgnorePattern(path string) string`,
`alreadyIgnored(content []byte, line string) bool`,
`appendIgnoreLine(content []byte, line string) []byte`.

- [ ] **Step 1: Write failing tests** for the four helpers in `ignore_test.go`:

```go
func TestEscapeIgnorePattern(t *testing.T) {
	cases := map[string]string{
		"a[1].log": `a\[1].log`,
		"a*b":      `a\*b`,
		"a?b":      `a\?b`,
		`a\b`:      `a\\b`,
		"plain.txt": "plain.txt",
	}
	for in, want := range cases {
		if got := escapeIgnorePattern(in); got != want {
			t.Errorf("escapeIgnorePattern(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIgnoreLine(t *testing.T) {
	if got := ignoreLine("a/b.log", false); got != "/a/b.log" {
		t.Errorf("exact = %q", got)
	}
	if got := ignoreLine("a/b.log", true); got != "*.log" {
		t.Errorf("ext = %q", got)
	}
	if got := ignoreLine("a[1].log", false); got != `/a\[1].log` {
		t.Errorf("exact-escaped = %q", got)
	}
}

func TestAlreadyIgnored(t *testing.T) {
	content := []byte("# a comment\n\n/a/b.log\n*.tmp\n")
	if !alreadyIgnored(content, "/a/b.log") {
		t.Error("present exact line not detected")
	}
	if !alreadyIgnored(content, "*.tmp") {
		t.Error("present ext line not detected")
	}
	if alreadyIgnored(content, "/a/b") {
		t.Error("substring must not match")
	}
	if alreadyIgnored(content, "# a comment") {
		t.Error("comment line must not count as a pattern")
	}
	if alreadyIgnored(nil, "/x") {
		t.Error("empty content")
	}
}

func TestAppendIgnoreLine(t *testing.T) {
	if got := appendIgnoreLine(nil, "/x"); string(got) != "/x\n" {
		t.Errorf("empty = %q", got)
	}
	if got := appendIgnoreLine([]byte("/a\n"), "/x"); string(got) != "/a\n/x\n" {
		t.Errorf("trailing-nl = %q", got)
	}
	if got := appendIgnoreLine([]byte("/a"), "/x"); string(got) != "/a\n/x\n" {
		t.Errorf("no-trailing-nl = %q", got)
	}
}
```

- [ ] **Step 2: Run — verify fail.** `go test ./internal/engine/ -run 'TestEscapeIgnorePattern|TestIgnoreLine|TestAlreadyIgnored|TestAppendIgnoreLine'` → FAIL (undefined).

- [ ] **Step 3: Implement the helpers** in `ignore.go`:

```go
package engine

import (
	"path"
	"strings"
)

// escapeIgnorePattern backslash-escapes the gitignore glob metacharacters so a
// literal filename matches itself. Backslash is escaped first so it does not
// double up the escapes inserted for the later metacharacters.
func escapeIgnorePattern(p string) string {
	r := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`)
	return r.Replace(p)
}

// ignoreLine builds the .gitignore pattern for a file. ext true → "*<ext>"
// (the whole extension, inherently a glob, unescaped); else the file anchored
// at the repo root with metacharacters escaped.
func ignoreLine(p string, ext bool) string {
	if ext {
		return "*" + path.Ext(p)
	}
	return "/" + escapeIgnorePattern(p)
}

// alreadyIgnored reports whether line is already a pattern in content, after
// trimming, skipping blank and #-comment lines.
func alreadyIgnored(content []byte, line string) bool {
	for _, l := range strings.Split(string(content), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if t == line {
			return true
		}
	}
	return false
}

// appendIgnoreLine appends line + "\n" to content, inserting a separating
// newline when content is non-empty and unterminated.
func appendIgnoreLine(content []byte, line string) []byte {
	out := content
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return append(out, []byte(line+"\n")...)
}
```

- [ ] **Step 4: Run — verify pass.** Same command → PASS.

- [ ] **Step 5: Commit.** `feat(engine): .gitignore pattern helpers (escape/line/dedup/append)`

---

### Task 2: `engine.Ignore` op + real-git tests

**Files:**
- Modify: `internal/engine/ignore.go`
- Test: `internal/engine/ignore_test.go`

**Consumes:** the Task 1 helpers; `GitOps.ReadWorktreeFile`/`WriteWorktreeFile`;
`newRepo(t)` real-git harness.
**Produces:** `type Ignore struct{ Path string; Ext bool }` implementing
`Operation`.

- [ ] **Step 1: Write failing real-git tests** appended to `ignore_test.go`
  (import `context`, `os`, `os/exec`, `path/filepath`, `strings`, `testing`):

```go
func gitStatus(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	return string(out)
}

func TestIgnoreExactRemovesUntrackedFromStatus(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "out.log"), []byte("x\n"), 0o644)

	res, err := Ignore{Path: "out.log"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Changed {
		t.Fatalf("first run should report Changed")
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gi), "/out.log") {
		t.Fatalf(".gitignore = %q", gi)
	}
	if strings.Contains(gitStatus(t, dir), "out.log") {
		t.Fatalf("out.log still in status:\n%s", gitStatus(t, dir))
	}

	// Idempotent: second run is a no-op, no duplicate line.
	res2, _ := Ignore{Path: "out.log"}.Run(context.Background(), OpDeps{Repo: repo})
	if res2.Changed {
		t.Fatalf("second run should be no-op")
	}
	gi2, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if strings.Count(string(gi2), "/out.log") != 1 {
		t.Fatalf("duplicate line: %q", gi2)
	}
}

func TestIgnoreMetacharFilenameActuallyIgnored(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "a[1].log"), []byte("x\n"), 0o644)

	if _, err := (Ignore{Path: "a[1].log"}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(gitStatus(t, dir), "a[1].log") {
		t.Fatalf("metachar file not ignored (unescaped [):\n%s", gitStatus(t, dir))
	}
}

func TestIgnoreExtensionRemovesAllMatching(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "one.tmp"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "two.tmp"), []byte("y\n"), 0o644)

	if _, err := (Ignore{Path: "one.tmp", Ext: true}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("run: %v", err)
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gi), "*.tmp") {
		t.Fatalf(".gitignore = %q", gi)
	}
	st := gitStatus(t, dir)
	if strings.Contains(st, "one.tmp") || strings.Contains(st, "two.tmp") {
		t.Fatalf("extension did not ignore both:\n%s", st)
	}
}
```

- [ ] **Step 2: Run — verify fail.** `go test ./internal/engine/ -run TestIgnore` → FAIL (undefined `Ignore`).

- [ ] **Step 3: Implement the op** in `ignore.go` (add `context`, `errors`? no
  decider needed — just `context`):

```go
// Ignore appends a single pattern to the repo-root .gitignore as an unstaged
// change. Path is the repo-relative path of the selected file. Ext true →
// "*<ext>"; else the file anchored at the repo root, metacharacters escaped.
// A pattern already present is a no-op. Default (TreeWrite) lock.
type Ignore struct {
	Path string
	Ext  bool
}

var _ Operation = Ignore{}

func (op Ignore) Run(ctx context.Context, deps OpDeps) (Result, error) {
	line := ignoreLine(op.Path, op.Ext)
	existing, _ := deps.Repo.ReadWorktreeFile(ctx, ".gitignore") // absent → nil
	if alreadyIgnored(existing, line) {
		res := Result{Summary: line + " already in .gitignore"}
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	if err := deps.Repo.WriteWorktreeFile(ctx, ".gitignore", appendIgnoreLine(existing, line)); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "ignored " + line, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

- [ ] **Step 4: Run — verify pass.** `go test ./internal/engine/ -run TestIgnore` → PASS.

- [ ] **Step 5: Commit.** `feat(engine): Ignore op — append a file/extension to .gitignore`

---

### Task 3: TUI Files-panel actions + wiring

**Files:**
- Create: `internal/tui/ignore_actions.go`
- Test: `internal/tui/ignore_actions_test.go`
- Modify: `internal/tui/action_menu.go`

**Consumes:** `engine.Ignore`; `actionRow`; `m.startOp`; `m.backingIndex`;
`m.status.Files`; `model.KindUntracked`; `panelFiles`.
**Produces:** `func (m Model) fileIgnoreRow() (actionRow, bool)`,
`func (m Model) fileIgnoreExtRow() (actionRow, bool)`.

- [ ] **Step 1: Write failing predicate tests** in `ignore_actions_test.go`.
  Use the existing Files-panel test fixtures (mirror `discard_test.go` /
  `avail`-style setup — a Model with `m.status.Files` populated, `m.focus =
  panelFiles`, an untracked entry selected). Assert:
  - `fileIgnoreRow` ok=true on an untracked selection; its `run` starts
    `engine.Ignore{Path: <path>}` (verify by checking the row id `"ignore-file"`
    and label).
  - `fileIgnoreExtRow` ok=true for untracked `foo.log` (id `"ignore-ext"`,
    label contains `*.log`); ok=false for untracked `Makefile` (no extension).
  - both ok=false: on `panelStaged`; on a tracked (modified) selection; when
    `m.running` is true.

  (Match the package's existing fixture helpers; inspect `discard_test.go` for
  the exact Model-construction pattern before writing.)

- [ ] **Step 2: Run — verify fail.** `go test ./internal/tui/ -run TestFileIgnore` → FAIL (undefined).

- [ ] **Step 3: Implement** `ignore_actions.go`:

```go
package tui

import (
	"path"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// untrackedFile resolves the Files-panel selection when it is an untracked
// file (the only case where adding to .gitignore is meaningful — git ignores
// only untracked paths). ok is false otherwise.
func (m Model) untrackedFile() (string, bool) {
	if m.focus != panelFiles || !m.opsIdle() {
		return "", false
	}
	bi, ok := m.backingIndex(panelFiles)
	if !ok {
		return "", false
	}
	f := m.status.Files[bi]
	if f.Kind != model.KindUntracked {
		return "", false
	}
	return f.Path, true
}

// fileIgnoreRow offers "Add to .gitignore" on an untracked Files-panel file.
func (m Model) fileIgnoreRow() (actionRow, bool) {
	p, ok := m.untrackedFile()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "ignore-file",
		label: "Add to .gitignore",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.Ignore{Path: p})
		},
	}, true
}

// fileIgnoreExtRow offers "Add extension to .gitignore" — only when the
// untracked file actually has an extension.
func (m Model) fileIgnoreExtRow() (actionRow, bool) {
	p, ok := m.untrackedFile()
	if !ok || path.Ext(p) == "" {
		return actionRow{}, false
	}
	return actionRow{
		id:    "ignore-ext",
		label: "Add extension to .gitignore (*" + path.Ext(p) + ")",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.Ignore{Path: p, Ext: true})
		},
	}, true
}
```

- [ ] **Step 4: Wire into `availableActions`** (action_menu.go), alongside the
  other `*Row()` appenders (e.g. right after the copy/shelf rows, before the
  commit rows):

```go
	if r, ok := m.fileIgnoreRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.fileIgnoreExtRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 5: Run — verify pass.** `go test ./internal/tui/ -run TestFileIgnore` → PASS, and `go test ./internal/tui/` green.

- [ ] **Step 6: Commit.** `feat(tui): Add to .gitignore / Add extension actions on untracked files`

---

### Task 4: Docs

**Files:**
- Modify: `internal/tui/help.go`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Add help entries** in `help.go` for the two menu actions, in the
  Files-panel / `.`-menu section, following the existing `r("...", "...")`
  style (menu-only actions — describe them as `.` menu actions):

```go
	r(".", "Files: Add to .gitignore / Add extension to .gitignore on an untracked file"),
```
  (Place near the other Files-panel `.`-menu descriptions; match surrounding
  wording. If a help test asserts specific text, update it.)

- [ ] **Step 2: Add a CHANGELOG entry** under the current unreleased section
  describing the two untracked-file `.gitignore` actions (TUI-only, anchored +
  escaped exact path, `*.ext` for the extension variant).

- [ ] **Step 3: Run the help tests.** `go test ./internal/tui/ -run TestHelp` → PASS.

- [ ] **Step 4: Commit.** `docs: changelog + help for .gitignore Files-panel actions`

---

## Final verification

- [ ] `./test.sh race` (vet+gofmt → unit → e2e) green.
- [ ] `gofmt -l internal/ | head` empty.
- [ ] Then hand to finishing-a-development-branch (merge to main).

## Self-review notes

- Spec coverage: helpers (T1) + op with metachar real-git test (T2) + TUI
  untracked-only gate & ext-when-extension (T3) + help/changelog (T4) — all
  spec sections mapped.
- Type consistency: `Ignore{Path, Ext}`, `ignoreLine`, `escapeIgnorePattern`,
  `alreadyIgnored`, `appendIgnoreLine`, `untrackedFile`, `fileIgnoreRow`,
  `fileIgnoreExtRow` used identically across tasks.
- The exact label string for `ignore-ext` includes the live `*<ext>`; the T3
  test asserts `*.log` for `foo.log`.
