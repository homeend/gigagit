# Phase 2: the agent lane (gg note / diff --hunks / review --notes / MCP / skill) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give an AI agent a headless lane to leave, list, batch-import and remove anchored review notes in gg — CLI verbs, MCP tools, an AI-review import path and a bundled `reviewing-with-gg` skill — over the phase 1 note store, so a human reads the result in the TUI or web.

**Architecture:** Everything new sits on top of the phase 1 domain surface (`NoteAdd`/`NoteReply`/`NoteRemove`/`NotesAt`/`NoteCounts`, `SetNotesPolicy`/`StartNotesSweep`). Domain grows the reads the agent lane needs (`DiffHunks`, `HunkRange`, `NoteTarget`, `NoteAddresses`, `NoteGet`, `WireNote`, `EffectiveConfig`, `WaitNotesSweep`); a new pure leaf `internal/notebatch` parses the two agent JSON batch shapes; `internal/cli` and `internal/mcp` are thin frontends over both. No TUI or web change beyond what falls out of the domain.

**Tech Stack:** Go 1.26, standard library only (`flag`, `encoding/json`, `regexp`), the existing `gitexec.FakeRunner` / `gittest` test helpers, `modelcontextprotocol/go-sdk` for MCP.

**Spec:** `docs/superpowers/specs/2026-09-08-hunk-parity-roadmap.md` — §4.5 "Phase 2 design: the agent lane" is binding; §4.4 is the phase 1 surface this consumes; §3.2 is the intent; §6 is the convention tax.

**Codemap:** `.superpowers/sdd/2026-09-09-agent-notes/codemap.md` (verified file/line facts).

## Global Constraints

- **Worktree:** all work happens in `/mnt/t/others/gigagit.worktrees/feat-agent-notes` on branch `feat/agent-notes`. Prefix every shell command with `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes &&`; use absolute paths for every file read or write. Never touch the main checkout at `/mnt/t/others/gigagit`.
- **Archtest import bans** (`internal/archtest/import_guard_test.go`): `internal/cli`, `internal/mcp`, `internal/tui`, `internal/web` must NEVER import `internal/git` or `internal/notes` (nor `shelf`/`bookmark`/`profile`/`prefix`/`searchhist`). All git and store access goes through `internal/domain`. The layering DAG test also forbids `domain` importing `cli`/`mcp`/`web`/`tui`.
- **Commit trailers:** every commit message ends with these two lines, in this order:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
  ```
- **No `git commit --amend`** — concurrent agents may share this worktree; always make a new commit.
- **Stage by explicit path** (`git add <path> <path>`), never `git add -A` / `git add .`.
- **gofmt:** run `gofmt -l <changed files>` before each commit; the output must be empty.
- **`t.Parallel()`** on every new test that touches no process globals. Tests that set an environment variable via `t.Setenv` (e.g. `GG_AGENT`, `XDG_STATE_HOME`) MUST NOT call `t.Parallel()` — `t.Setenv` panics in a parallel test.
- **Test isolation:** CLI/MCP tests that touch the note store call `t.Setenv("XDG_STATE_HOME", t.TempDir())` (and `t.Setenv("XDG_CONFIG_HOME", t.TempDir())` when config is read); `internal/domain` tests point one Service at its own store with `svc.UseNotesDir(t.TempDir())`.
- **Windows path notation:** never string-compare `/` against `\`. Compare filesystem paths with `filepath.Clean`/`filepath.Rel`; derive sibling paths with `filepath.Dir`/`filepath.Base`/`filepath.Join`, never `strings.Replace` on a path segment. Repo-relative paths stored in a note are git slash form (`filepath.ToSlash`) and are compared as plain strings only after both sides have been normalised through `filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))`.
- **`./test.sh unit` must be green before the final task's commit**; `./test.sh race` before the human merges.
- **Engine/CLI prose stays English** (agent-facing protocol). No `i18n` work is required by this plan: no new TUI string is added.
- **Config values:** `[notes] max_age_days` default 30 (`-1` = forever), `[notes] max_entries` default 2000 (`-1` = uncapped); `0` means "unset" and falls back to the default. Already shipped in phase 1 — this plan only reads them.

---

### Task 1: Hunk model, the pure `@@` parser, and the domain hunk queries

**Files:**
- Create: `internal/model/hunk.go`
- Create: `internal/domain/hunks.go`
- Create: `internal/domain/hunks_test.go`

**Interfaces:**
- Consumes: `model.DiffSpec{Cached bool, Rev string, Paths []string}`, `(*domain.Service).DiffPatch(ctx, model.DiffSpec) (string, error)` (`internal/domain/query_cli.go:44`), `model.NoteSide` / `model.NoteSideNew` / `model.NoteSideOld`.
- Produces:
  ```go
  // internal/model/hunk.go
  type Hunk struct {
      N      int    // 1-based index within the file, in git's @@ order
      Old    [2]int // 1-based inclusive start/end on the old side; [0,0] = zero-count
      New    [2]int
      Header string // the function-context text after the closing "@@"
  }
  type FileHunks struct {
      Path    string // new-side path, git slash form
      OldPath string // rename/copy source, else ""
      Hunks   []Hunk
  }

  // internal/domain/hunks.go
  func ParseDiffHunks(patch string) []model.FileHunks
  func HunkDiffSpec(cached bool, rev string, paths []string) model.DiffSpec
  func (s *Service) DiffHunks(ctx context.Context, spec model.DiffSpec) ([]model.FileHunks, error)
  func (s *Service) HunkRange(ctx context.Context, spec model.DiffSpec, path string, n int) (model.NoteSide, [2]int, error)
  ```

- [ ] **Step 1: Write the failing parser test**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/domain/hunks_test.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
)

func TestParseDiffHunksCountForms(t *testing.T) {
	t.Parallel()
	patch := "diff --git a/src/search.ts b/src/search.ts\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/src/search.ts\n" +
		"+++ b/src/search.ts\n" +
		"@@ -15,7 +15,9 @@ export function score\n" +
		" ctx\n" +
		"-old\n" +
		"+new\n" +
		"@@ -40 +42 @@\n" +
		"-a\n" +
		"+b\n" +
		"@@ -80,0 +90,3 @@ tail\n" +
		"+x\n" +
		"+y\n" +
		"+z\n"
	got := ParseDiffHunks(patch)
	if len(got) != 1 || got[0].Path != "src/search.ts" || got[0].OldPath != "" {
		t.Fatalf("files = %+v, want one src/search.ts", got)
	}
	want := []model.Hunk{
		{N: 1, Old: [2]int{15, 21}, New: [2]int{15, 23}, Header: "export function score"},
		{N: 2, Old: [2]int{40, 40}, New: [2]int{42, 42}, Header: ""},
		{N: 3, Old: [2]int{0, 0}, New: [2]int{90, 92}, Header: "tail"},
	}
	if len(got[0].Hunks) != len(want) {
		t.Fatalf("hunks = %+v, want %+v", got[0].Hunks, want)
	}
	for i, w := range want {
		if got[0].Hunks[i] != w {
			t.Errorf("hunk %d = %+v, want %+v (omitted count = 1; a zero count is [0,0])", i+1, got[0].Hunks[i], w)
		}
	}
}

func TestParseDiffHunksRenameBinaryAndDeletion(t *testing.T) {
	t.Parallel()
	patch := "diff --git a/old/name.go b/new/name.go\n" +
		"similarity index 90%\n" +
		"rename from old/name.go\n" +
		"rename to new/name.go\n" +
		"--- a/old/name.go\n" +
		"+++ b/new/name.go\n" +
		"@@ -1,2 +1,2 @@\n" +
		"-a\n" +
		"+b\n" +
		" c\n" +
		"diff --git a/img.png b/img.png\n" +
		"index 3333333..4444444 100644\n" +
		"Binary files a/img.png and b/img.png differ\n" +
		"diff --git a/gone.txt b/gone.txt\n" +
		"deleted file mode 100644\n" +
		"--- a/gone.txt\n" +
		"+++ /dev/null\n" +
		"@@ -1,3 +0,0 @@\n" +
		"-one\n" +
		"-two\n" +
		"-three\n" +
		"\\ No newline at end of file\n"
	got := ParseDiffHunks(patch)
	if len(got) != 3 {
		t.Fatalf("files = %+v, want 3", got)
	}
	if got[0].Path != "new/name.go" || got[0].OldPath != "old/name.go" {
		t.Errorf("rename = %q/%q, want new/name.go from old/name.go", got[0].Path, got[0].OldPath)
	}
	if got[1].Path != "img.png" || len(got[1].Hunks) != 0 {
		t.Errorf("binary = %+v, want a file row with zero hunks", got[1])
	}
	if got[2].Path != "gone.txt" || len(got[2].Hunks) != 1 {
		t.Fatalf("deletion = %+v, want gone.txt with one hunk", got[2])
	}
	if got[2].Hunks[0].New != [2]int{0, 0} || got[2].Hunks[0].Old != [2]int{1, 3} {
		t.Errorf("deletion hunk = %+v, want old 1-3 and new [0,0]", got[2].Hunks[0])
	}
}

func TestParseDiffHunksIgnoresBodyLinesThatLookLikeHeaders(t *testing.T) {
	t.Parallel()
	patch := "diff --git a/p.diff b/p.diff\n" +
		"--- a/p.diff\n" +
		"+++ b/p.diff\n" +
		"@@ -1,2 +1,2 @@\n" +
		"-@@ -9,9 +9,9 @@ not a header\n" +
		"+@@ -8,8 +8,8 @@ also not\n"
	got := ParseDiffHunks(patch)
	if len(got) != 1 || len(got[0].Hunks) != 1 {
		t.Fatalf("body lines starting with -/+ must never be read as headers: %+v", got)
	}
}

func TestHunkDiffSpecTargets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		cached bool
		rev    string
		want   model.DiffSpec
	}{
		{"worktree", false, "", model.DiffSpec{Paths: []string{"a.go"}}},
		{"cached", true, "", model.DiffSpec{Cached: true, Paths: []string{"a.go"}}},
		{"single commit is its OWN change", false, "abc123", model.DiffSpec{Rev: "abc123^..abc123", Paths: []string{"a.go"}}},
		{"explicit range passes through", false, "main..HEAD", model.DiffSpec{Rev: "main..HEAD", Paths: []string{"a.go"}}},
	}
	for _, c := range cases {
		got := HunkDiffSpec(c.cached, c.rev, []string{"a.go"})
		if got.Cached != c.want.Cached || got.Rev != c.want.Rev || len(got.Paths) != 1 || got.Paths[0] != "a.go" {
			t.Errorf("%s: HunkDiffSpec = %+v, want %+v", c.name, got, c.want)
		}
	}
}

// hunkRepo is a real repo whose working tree adds lines in two separate places,
// so the patch has two @@ hunks in a known order.
func hunkRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-b", "main")
	body := ""
	for i := 1; i <= 40; i++ {
		body += "line\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-m", "seed")
	lines := []byte(body)
	edited := string(lines[:5*len("line\n")]) + "TOP\n" +
		string(lines[5*len("line\n"):35*len("line\n")]) + "BOTTOM\n" +
		string(lines[35*len("line\n"):])
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDiffHunksAndHunkRangeOnRealRepo(t *testing.T) {
	t.Parallel()
	dir := hunkRepo(t)
	svc := svcIn(t, dir)
	ctx := context.Background()
	files, err := svc.DiffHunks(ctx, HunkDiffSpec(false, "", nil))
	if err != nil {
		t.Fatalf("DiffHunks: %v", err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" || len(files[0].Hunks) != 2 {
		t.Fatalf("files = %+v, want a.txt with 2 hunks", files)
	}
	if files[0].Hunks[0].N != 1 || files[0].Hunks[1].N != 2 {
		t.Fatalf("hunks must be numbered 1..N in @@ order: %+v", files[0].Hunks)
	}
	side, rng, err := svc.HunkRange(ctx, HunkDiffSpec(false, "", []string{"a.txt"}), "a.txt", 2)
	if err != nil {
		t.Fatalf("HunkRange: %v", err)
	}
	if side != model.NoteSideNew || rng != files[0].Hunks[1].New {
		t.Fatalf("HunkRange(2) = %s %v, want new %v", side, rng, files[0].Hunks[1].New)
	}
	if _, _, err := svc.HunkRange(ctx, HunkDiffSpec(false, "", []string{"a.txt"}), "a.txt", 9); err == nil {
		t.Fatal("an out-of-range hunk number must error")
	} else if got := err.Error(); got != "a.txt has 2 hunks" {
		t.Fatalf("error = %q, want %q", got, "a.txt has 2 hunks")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && go test ./internal/domain -run 'TestParseDiffHunks|TestHunkDiffSpec|TestDiffHunksAndHunkRange' -count=1`
Expected: FAIL — `undefined: ParseDiffHunks`, `undefined: HunkDiffSpec`, `model.Hunk` undefined.

- [ ] **Step 3: Add the model types**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/model/hunk.go`:

```go
package model

// Hunk is one git `@@` region of one file's patch, numbered 1-based in the
// order git prints it. Old/New are 1-based INCLUSIVE line ranges on their side;
// a zero-count side (a pure add has none on the old side, a pure delete none on
// the new) is [0,0], never a degenerate [n,n-1].
//
// This numbering is the agent-facing hunk address (`gg diff --hunks`,
// `gg note add --hunk N`). It is NOT the TUI/web hunk picker's numbering, which
// counts textdiff change blocks and merges no context.
type Hunk struct {
	N      int
	Old    [2]int
	New    [2]int
	Header string
}

// FileHunks is one file's hunk list. Path is the NEW-side, repo-relative path
// in git slash form; OldPath carries a rename/copy source and is empty
// otherwise. A binary file appears with an empty Hunks slice.
type FileHunks struct {
	Path    string
	OldPath string
	Hunks   []Hunk
}
```

- [ ] **Step 4: Write the parser and the domain queries**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/domain/hunks.go`:

```go
package domain

// Git `@@` hunk addressing — the agent-facing hunk number behind
// `gg diff --hunks` and `gg note add --hunk N`. The parser is pure so both the
// CLI and MCP reach it through one Service method and can never disagree with
// what `gg diff` printed.

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// hunkHeaderRe matches a unified-diff hunk header. Anchored at the start of the
// line, so a CONTENT line (which always begins with ' ', '+', '-' or '\') can
// never be mistaken for one — a patch of a patch is a real input.
var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

// HunkDiffSpec is the patch `gg diff --hunks` numbers, and the patch
// `--hunk N` resolves against. The three note target states map onto it
// exactly:
//
//	rev == "", cached == false  → index → working tree   (StateUnstaged/Untracked)
//	rev == "", cached == true   → HEAD  → index          (StateStaged)
//	rev == "<commit>"           → parent → commit        (StateCommitted)
//
// A SINGLE commit therefore means that commit's OWN change (<c>^..<c>), the
// pair of texts a commit note anchors to — not `git diff <c>` (working tree vs
// commit), which would number a different patch than the note lands in. An
// explicit A..B / A...B range passes through unchanged.
func HunkDiffSpec(cached bool, rev string, paths []string) model.DiffSpec {
	switch {
	case rev == "":
		return model.DiffSpec{Cached: cached, Paths: paths}
	case strings.Contains(rev, ".."):
		return model.DiffSpec{Rev: rev, Paths: paths}
	default:
		return model.DiffSpec{Rev: rev + "^.." + rev, Paths: paths}
	}
}

// DiffHunks lists each file's `@@` hunks for spec, over the same patch
// DiffPatch prints for it.
func (s *Service) DiffHunks(ctx context.Context, spec model.DiffSpec) ([]model.FileHunks, error) {
	patch, err := s.DiffPatch(ctx, spec)
	if err != nil {
		return nil, err
	}
	return ParseDiffHunks(patch), nil
}

// HunkRange resolves hunk n of path to the side and 1-based inclusive line
// range a note anchored by `--hunk N` covers: the hunk's whole NEW-side span,
// or its old-side span when the hunk only deletes (New == [0,0]).
func (s *Service) HunkRange(ctx context.Context, spec model.DiffSpec, path string, n int) (model.NoteSide, [2]int, error) {
	files, err := s.DiffHunks(ctx, spec)
	if err != nil {
		return "", [2]int{}, err
	}
	want := toGitPath(path)
	for _, f := range files {
		if f.Path != want && f.OldPath != want {
			continue
		}
		if n < 1 || n > len(f.Hunks) {
			return "", [2]int{}, fmt.Errorf("%s has %d hunks", want, len(f.Hunks))
		}
		h := f.Hunks[n-1]
		if h.New == [2]int{0, 0} {
			return model.NoteSideOld, h.Old, nil
		}
		return model.NoteSideNew, h.New, nil
	}
	return "", [2]int{}, fmt.Errorf("%s has no changes in this diff", want)
}

// ParseDiffHunks is the pure `diff --git` / `@@` reader. It never touches git,
// so the CLI, MCP and the review importer all number hunks identically.
func ParseDiffHunks(patch string) []model.FileHunks {
	var out []model.FileHunks
	cur := -1
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			a, b := gitHeaderPaths(line)
			out = append(out, model.FileHunks{Path: b, OldPath: ""})
			cur = len(out) - 1
			if a != "" && a != b {
				out[cur].OldPath = a
			}
		case cur < 0:
			// Header noise before the first file (e.g. a commit header).
		case strings.HasPrefix(line, "rename to "):
			out[cur].Path = unquoteGitPath(strings.TrimPrefix(line, "rename to "))
		case strings.HasPrefix(line, "rename from "):
			out[cur].OldPath = unquoteGitPath(strings.TrimPrefix(line, "rename from "))
		case strings.HasPrefix(line, "+++ "):
			// The +++ line is authoritative for the new-side path; /dev/null
			// (a deletion) leaves the path taken from `diff --git`.
			if p := stripSidePrefix(strings.TrimPrefix(line, "+++ ")); p != "" {
				out[cur].Path = p
			}
		case strings.HasPrefix(line, "@@"):
			m := hunkHeaderRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			h := model.Hunk{
				N:      len(out[cur].Hunks) + 1,
				Old:    sideRange(m[1], m[2]),
				New:    sideRange(m[3], m[4]),
				Header: strings.TrimRight(m[5], " \t\r"),
			}
			out[cur].Hunks = append(out[cur].Hunks, h)
		}
	}
	return out
}

// sideRange turns a "@@" start plus optional count into a 1-based inclusive
// range. An omitted count is 1 (git's rule); a zero count is [0,0] — that side
// has no lines at all.
func sideRange(start, count string) [2]int {
	s, err := strconv.Atoi(start)
	if err != nil {
		return [2]int{0, 0}
	}
	n := 1
	if count != "" {
		n, err = strconv.Atoi(count)
		if err != nil {
			n = 1
		}
	}
	if n <= 0 {
		return [2]int{0, 0}
	}
	return [2]int{s, s + n - 1}
}

// gitHeaderPaths splits `diff --git a/<old> b/<new>` into its two paths. It is
// a FALLBACK: the +++ / rename lines override it whenever they are present,
// because a path containing " b/" makes this split ambiguous.
func gitHeaderPaths(line string) (oldPath, newPath string) {
	rest := strings.TrimPrefix(line, "diff --git ")
	i := strings.Index(rest, " b/")
	if i < 0 {
		return "", ""
	}
	return stripSidePrefix(rest[:i]), stripSidePrefix(rest[i+1:])
}

// stripSidePrefix drops git's a// b/ side prefix and any trailing timestamp,
// unquotes a C-quoted path, and maps /dev/null to "".
func stripSidePrefix(tok string) string {
	tok = strings.TrimSpace(tok)
	if tok == "/dev/null" || tok == "" {
		return ""
	}
	if i := strings.IndexAny(tok, "\t"); i >= 0 {
		tok = tok[:i]
	}
	tok = unquoteGitPath(tok)
	for _, p := range []string{"a/", "b/"} {
		if strings.HasPrefix(tok, p) {
			return tok[len(p):]
		}
	}
	return tok
}

// unquoteGitPath undoes git's C-style quoting of a path with special bytes.
// A path that does not unquote is used verbatim rather than dropped.
func unquoteGitPath(p string) string {
	p = strings.TrimSpace(p)
	if len(p) < 2 || p[0] != '"' {
		return p
	}
	if unq, err := strconv.Unquote(p); err == nil {
		return unq
	}
	return p
}

// toGitPath normalises a caller-supplied path (either notation) to the git
// slash form stored in a note address and printed in a patch. Never compare a
// raw argv path against a patch path: on Windows the two notations differ.
func toGitPath(p string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
}
```

Add `"path/filepath"` to that file's import block — `toGitPath` uses it.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && gofmt -l internal/model/hunk.go internal/domain/hunks.go && go test ./internal/domain -run 'TestParseDiffHunks|TestHunkDiffSpec|TestDiffHunksAndHunkRange' -count=1`
Expected: `gofmt -l` prints nothing; tests PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
git add internal/model/hunk.go internal/domain/hunks.go internal/domain/hunks_test.go && \
git commit -m "feat(domain): git @@ hunk model, pure parser, DiffHunks/HunkRange" \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" \
  -m "Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 2: Domain additions for the agent lane

**Files:**
- Modify: `internal/domain/notes.go` (full-sha normalisation in `NoteAdd`; add `NoteGet`, `NoteAddresses`)
- Modify: `internal/domain/notes_sweep.go` (add `WaitNotesSweep`)
- Create: `internal/domain/notetarget.go` (`NoteTarget`, `ErrNoteTargetUsage`)
- Create: `internal/domain/notewire.go` (`WireNote`, `ToWireNote`)
- Create: `internal/domain/effective_config.go` (`EffectiveConfig`)
- Modify: `internal/cli/review.go:155-167` (`loadConfigFor` becomes a wrapper)
- Modify: `internal/web/notes.go:83-109` (delegate to `domain.WireNote`)
- Create: `internal/domain/notetarget_test.go`
- Modify: `internal/domain/notes_test.go` (add the full-sha + NoteGet + NoteAddresses tests)

**Interfaces:**
- Consumes: `(*Service).RevParse(ctx, rev) (string, error)` (`internal/domain/query.go:490`), `(*Service).Status(ctx) (model.WorkingTreeStatus, error)` (`query.go:293`), `(*Service).TopLevel(ctx)` (`query.go:478`), `model.FileStatus{Path, Kind}` + `model.KindUntracked`, `worktreeScopedNote`, `sameWorktreePath`, `notesStore`, `notesSweepWG`, `config.Load`/`config.DefaultGlobalPath`/`config.ActiveRepoConfigPath`/`config.PrivateRepoPath`.
- Produces:
  ```go
  var ErrNoteTargetUsage = errors.New("note target usage")
  func (s *Service) NoteTarget(ctx context.Context, path string, cached bool, rev string) (model.FileAddress, error)
  func (s *Service) NoteGet(ctx context.Context, id string) (model.Note, error)
  func (s *Service) NoteAddresses(ctx context.Context) ([]model.FileAddress, error)
  func (s *Service) WaitNotesSweep(ctx context.Context) bool
  func (s *Service) EffectiveConfig(ctx context.Context) (config.Config, error)
  type WireNote struct { ID, ParentID, Source, Author, Path, Rev, Side string; Line int; Range [2]int; Summary, Rationale, Status string; Replies []WireNote }
  func ToWireNote(r ResolvedNote) WireNote
  ```

- [ ] **Step 1: Write the failing tests**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/domain/notetarget_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
)

// targetRepo has one tracked-and-modified file and one untracked file, so both
// default-state rows of the §4.5 target table are reachable.
func targetRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNoteTargetStateTable(t *testing.T) {
	t.Parallel()
	dir := targetRepo(t)
	svc := svcIn(t, dir)
	ctx := context.Background()

	addr, err := svc.NoteTarget(ctx, "a.txt", false, "")
	if err != nil || addr.State != model.StateUnstaged || addr.Path != "a.txt" {
		t.Fatalf("default = %+v %v, want StateUnstaged a.txt", addr, err)
	}
	addr, err = svc.NoteTarget(ctx, "fresh.txt", false, "")
	if err != nil || addr.State != model.StateUntracked {
		t.Fatalf("untracked path = %+v %v, want StateUntracked", addr, err)
	}
	addr, err = svc.NoteTarget(ctx, "a.txt", true, "")
	if err != nil || addr.State != model.StateStaged {
		t.Fatalf("--cached = %+v %v, want StateStaged", addr, err)
	}
	addr, err = svc.NoteTarget(ctx, "a.txt", false, "HEAD")
	if err != nil {
		t.Fatalf("--rev HEAD: %v", err)
	}
	if addr.State != model.StateCommitted || len(addr.Commit) != 40 {
		t.Fatalf("--rev = %+v, want StateCommitted with a FULL sha", addr)
	}
}

func TestNoteTargetUsageErrors(t *testing.T) {
	t.Parallel()
	dir := targetRepo(t)
	svc := svcIn(t, dir)
	ctx := context.Background()
	for _, c := range []struct {
		name   string
		path   string
		cached bool
		rev    string
		want   string
	}{
		{"range refused", "a.txt", false, "main..HEAD", "a note anchors to one commit; pass the tip commit"},
		{"triple-dot range refused", "a.txt", false, "main...HEAD", "a note anchors to one commit; pass the tip commit"},
		{"cached with rev", "a.txt", true, "HEAD", "--cached and --rev are mutually exclusive"},
		{"no path", "", false, "", "a note needs a file path"},
		{"escaping path", "../outside.txt", false, "", "path escapes the repository"},
	} {
		_, err := svc.NoteTarget(ctx, c.path, c.cached, c.rev)
		if !errors.Is(err, ErrNoteTargetUsage) {
			t.Errorf("%s: err = %v, want ErrNoteTargetUsage", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %q, want it to contain %q", c.name, err, c.want)
		}
	}
	// An unknown rev is a FAILURE (exit 1), not a usage error (exit 2).
	if _, err := svc.NoteTarget(ctx, "a.txt", false, "no-such-rev"); err == nil || errors.Is(err, ErrNoteTargetUsage) {
		t.Fatalf("unknown rev err = %v, want a non-usage error", err)
	}
}

func TestNoteTargetNormalisesPathNotation(t *testing.T) {
	t.Parallel()
	dir := targetRepo(t)
	svc := svcIn(t, dir)
	addr, err := svc.NoteTarget(context.Background(), "./a.txt", false, "")
	if err != nil || addr.Path != "a.txt" {
		t.Fatalf("Path = %q (%v), want the cleaned git slash form a.txt", addr.Path, err)
	}
}
```

Append to `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/domain/notes_test.go`:

```go
func TestNoteAddNormalisesCommitToFullSHA(t *testing.T) {
	t.Parallel()
	dir := noteSideRepo(t)
	svc := svcIn(t, dir)
	svc.UseNotesDir(t.TempDir())
	full := headSHA(t, dir)
	got, err := svc.NoteAdd(context.Background(), model.Note{
		Address: model.FileAddress{State: model.StateCommitted, Commit: full[:7], Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "short sha in",
	})
	if err != nil {
		t.Fatalf("NoteAdd: %v", err)
	}
	if got.Address.Commit != full {
		t.Fatalf("stored commit = %q, want the full sha %q (a CLI note and a TUI note must share one target)", got.Address.Commit, full)
	}
}

// A Service whose runner cannot rev-parse (the FakeRunner suites) must keep the
// commit exactly as given: normalisation is best-effort, never a new failure.
func TestNoteAddKeepsCommitWhenRevParseFails(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	got, err := svc.NoteAdd(context.Background(), model.Note{
		Address:     model.FileAddress{State: model.StateCommitted, Commit: "deadbee", Path: "a.go"},
		Side:        model.NoteSideNew,
		Range:       [2]int{1, 1},
		Summary:     "x",
		ContextHash: "h",
	})
	if err != nil || got.Address.Commit != "deadbee" {
		t.Fatalf("commit = %q err = %v, want the value as given", got.Address.Commit, err)
	}
}

func TestNoteGetAndNoteAddresses(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	root, err := svc.NoteAdd(ctx, model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "root", ContextHash: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteReply(ctx, root.ID, model.Note{Summary: "reply"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateCommitted, Commit: "c0ffee", Path: "z.go"},
		Side:    model.NoteSideNew, Range: [2]int{2, 2}, Summary: "commit note", ContextHash: "h",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.NoteGet(ctx, root.ID)
	if err != nil || got.Summary != "root" {
		t.Fatalf("NoteGet = %+v %v", got, err)
	}
	if _, err := svc.NoteGet(ctx, "nope1234"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("NoteGet(missing) = %v, want ErrNoteNotFound", err)
	}

	addrs, err := svc.NoteAddresses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 2 {
		t.Fatalf("addresses = %+v, want 2 (a reply shares its root's address)", addrs)
	}
	var paths []string
	for _, a := range addrs {
		paths = append(paths, a.Path)
	}
	sort.Strings(paths)
	if paths[0] != "a/b.go" || paths[1] != "z.go" {
		t.Fatalf("paths = %v, want [a/b.go z.go]", paths)
	}
}

func TestWaitNotesSweepBounded(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	// No sweep started: the wait group is empty, so this returns immediately.
	if !svc.WaitNotesSweep(context.Background()) {
		t.Fatal("WaitNotesSweep with no sweep running must report done")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if svc.WaitNotesSweep(ctx) {
		t.Fatal("a cancelled ctx must abandon the wait and report not-done")
	}
}
```

Add `"errors"` and `"sort"` to that file's import block if they are not already there.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && go test ./internal/domain -run 'TestNoteTarget|TestNoteAddNormalises|TestNoteAddKeepsCommit|TestNoteGetAndNoteAddresses|TestWaitNotesSweepBounded' -count=1`
Expected: FAIL — `svc.NoteTarget undefined`, `svc.NoteGet undefined`, `svc.NoteAddresses undefined`, `svc.WaitNotesSweep undefined`.

- [ ] **Step 3: Add the full-sha normalisation to `NoteAdd`**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/domain/notes.go`, inside `NoteAdd`, insert this block immediately BEFORE the `if worktreeScopedNote(n.Address) {` block (it must run before `noteSideLines` reads `<rev>:path`):

```go
	// A commit note's target is its FULL sha, so a CLI note on "HEAD" and a TUI
	// note on the same commit share one address (sameNoteTarget compares Commit
	// verbatim). Best-effort: a runner that cannot rev-parse (the FakeRunner
	// suites, a detached store) keeps the value as given rather than failing a
	// write — the CLI validates the rev up front in NoteTarget.
	if n.Address.State == model.StateCommitted && n.Address.Commit != "" && !isFullSHA(n.Address.Commit) {
		if full, ferr := s.RevParse(ctx, n.Address.Commit); ferr == nil {
			if full = strings.TrimSpace(full); isFullSHA(full) {
				n.Address.Commit = full
			}
		}
	}
```

Append to the same file:

```go
// isFullSHA reports whether s is a 40-character lowercase hex object id — the
// only form worth storing as a note's commit target.
func isFullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// NoteGet returns one stored note by id, replies included. It is the batch
// importer's parent check: `note apply` validates every replyTo BEFORE the
// first write, so a bad batch stores nothing.
func (s *Service) NoteGet(ctx context.Context, id string) (model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return model.Note{}, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return model.Note{}, err
	}
	for _, n := range all {
		if n.ID == id {
			return n, nil
		}
	}
	return model.Note{}, ErrNoteNotFound
}

// NoteAddresses lists the distinct addresses that carry at least one ROOT note
// and are visible from this checkout: every commit and shelf note (those are
// worktree-agnostic) plus the worktree-state notes taken in THIS checkout. It
// is the enumeration door for `gg note list` and `gg note clear --all`, which
// have no path to resolve.
//
// Addresses are deduplicated by the identity sameNoteTarget uses — worktree,
// commit, shelf id and path, NOT State — so a path with both a staged and an
// unstaged note yields ONE address (whose State is the first note's, i.e. the
// side pair NotesAt will read). Sorted by path, then commit, for stable output.
func (s *Service) NoteAddresses(ctx context.Context) ([]model.FileAddress, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return nil, err
	}
	cur, _ := s.TopLevel(ctx) // "" simply hides worktree notes, never fails the query
	seen := map[string]bool{}
	var out []model.FileAddress
	for _, n := range all {
		if n.IsReply() {
			continue
		}
		a := n.Address
		if worktreeScopedNote(a) && !sameWorktreePath(a.Worktree, cur) {
			continue
		}
		key := a.Worktree + "\x00" + a.Commit + "\x00" + a.ShelfID + "\x00" + a.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Commit < out[j].Commit
	})
	return out, nil
}
```

- [ ] **Step 4: Add `WaitNotesSweep`**

Append to `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/domain/notes_sweep.go`:

```go
// WaitNotesSweep blocks until the background sweep started by StartNotesSweep
// has finished, or ctx expires — whichever comes first. It reports whether the
// sweep actually finished.
//
// This is the SHORT-LIVED process's door (`gg note …`, one command then exit):
// the verb does its own work first, then gives housekeeping a bounded budget.
// Abandoning an unfinished sweep is safe and deliberate — it is idempotent and
// lock-protected, so the next gg start simply retries it.
func (s *Service) WaitNotesSweep(ctx context.Context) bool {
	done := make(chan struct{})
	go func() {
		s.notesSweepWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}
```

- [ ] **Step 5: Add `NoteTarget`**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/domain/notetarget.go`:

```go
package domain

// The shared "which diff does this note belong to" rule (spec §4.5). The CLI
// and MCP both call it, so `gg note add`, `gg note apply`, `gg review --notes`
// and the MCP tools can never disagree about what a target flag means:
//
//	Target flags   | Address.State                       | old side | new side
//	---------------|-------------------------------------|----------|----------
//	(none)         | StateUnstaged, or StateUntracked    | index    | worktree
//	               | when git status lists it untracked  |          |
//	--cached       | StateStaged                         | HEAD     | index
//	--rev <commit> | StateCommitted, Commit = FULL sha   | parent   | commit

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNoteTargetUsage marks a caller MISTAKE in the target flags (a range where
// a commit is required, --cached together with --rev, a missing or escaping
// path). Frontends map it to a usage exit code (the CLI: 2); every other error
// from NoteTarget is a genuine failure (exit 1).
var ErrNoteTargetUsage = errors.New("note target usage")

// NoteTarget resolves a path plus the --cached/--rev flags into the address a
// note hangs off. rev is resolved to a full sha here so the stored target is
// stable; a rev naming a RANGE is refused, because a note anchors to exactly
// one pair of texts.
func (s *Service) NoteTarget(ctx context.Context, path string, cached bool, rev string) (model.FileAddress, error) {
	if cached && rev != "" {
		return model.FileAddress{}, fmt.Errorf("%w: --cached and --rev are mutually exclusive", ErrNoteTargetUsage)
	}
	if strings.TrimSpace(path) == "" {
		return model.FileAddress{}, fmt.Errorf("%w: a note needs a file path", ErrNoteTargetUsage)
	}
	p := toGitPath(path)
	if p == ".." || strings.HasPrefix(p, "../") || filepath.IsAbs(path) {
		return model.FileAddress{}, fmt.Errorf("%w: path escapes the repository: %s", ErrNoteTargetUsage, path)
	}
	if rev != "" {
		if strings.Contains(rev, "..") {
			return model.FileAddress{}, fmt.Errorf("%w: a note anchors to one commit; pass the tip commit", ErrNoteTargetUsage)
		}
		full, err := s.RevParse(ctx, rev)
		if err != nil {
			return model.FileAddress{}, fmt.Errorf("unknown revision %q: %w", rev, err)
		}
		return model.FileAddress{State: model.StateCommitted, Commit: strings.TrimSpace(full), Path: p}, nil
	}
	if cached {
		return model.FileAddress{State: model.StateStaged, Path: p}, nil
	}
	// A file git has never seen has no index side, so its note must be stamped
	// StateUntracked or the sweep reads the wrong (absent) old side and drops it.
	st, err := s.Status(ctx)
	if err != nil {
		return model.FileAddress{}, err
	}
	for _, f := range st.Files {
		if toGitPath(f.Path) == p && f.Kind == model.KindUntracked {
			return model.FileAddress{State: model.StateUntracked, Path: p}, nil
		}
	}
	return model.FileAddress{State: model.StateUnstaged, Path: p}, nil
}
```

- [ ] **Step 6: Lift the wire note into domain**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/domain/notewire.go`:

```go
package domain

// WireNote is the JSON projection of one resolved note thread, shared by every
// frontend that speaks JSON: the web handlers, `gg note list --json` and the
// MCP note tools. It lives in domain because internal/cli and internal/mcp
// cannot import internal/web, and three hand-rolled shapes would drift.
type WireNote struct {
	ID        string     `json:"id"`
	ParentID  string     `json:"parent_id,omitempty"`
	Source    string     `json:"source"`
	Author    string     `json:"author,omitempty"`
	Path      string     `json:"path,omitempty"`
	Rev       string     `json:"rev,omitempty"`
	Side      string     `json:"side"`
	Line      int        `json:"line"`
	Range     [2]int     `json:"range"`
	Summary   string     `json:"summary"`
	Rationale string     `json:"rationale,omitempty"`
	Status    string     `json:"status"`
	Replies   []WireNote `json:"replies,omitempty"`
}

// ToWireNote flattens one resolved thread. Line and Range are the RESOLVED
// anchor, not the stored one: a note that moved must render where its text is
// now. Line stays the range END for the web page that already reads it; Range
// carries both ends, which a multi-line hunk anchor needs.
func ToWireNote(r ResolvedNote) WireNote {
	w := WireNote{
		ID: r.Note.ID, ParentID: r.Note.ParentID, Source: string(r.Note.Source),
		Author: r.Note.Author, Path: r.Note.Address.Path, Rev: r.Note.Address.Commit,
		Side: string(r.Note.Side), Line: r.Range[1], Range: r.Range,
		Summary: r.Note.Summary, Rationale: r.Note.Rationale, Status: string(r.Status),
	}
	for _, rep := range r.Replies {
		w.Replies = append(w.Replies, ToWireNote(rep))
	}
	return w
}
```

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/web/notes.go`, replace the local `wireNote` struct (lines 83–94) and `toWireNote` (lines 99–109) with:

```go
// wireNote is domain's shared JSON note shape (see domain.WireNote): the CLI
// and MCP emit the same object, so a page and an agent read one format.
type wireNote = domain.WireNote

func toWireNote(r domain.ResolvedNote) wireNote { return domain.ToWireNote(r) }
```

- [ ] **Step 7: Add `EffectiveConfig` and rewire `loadConfigFor`**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/domain/effective_config.go`:

```go
package domain

// The effective gg config for this repo, resolved once in domain so every
// frontend gets the same answer. internal/mcp cannot import internal/cli, and
// applying the [notes] policy is now something both do.

import (
	"context"
	"path/filepath"

	"github.com/homeend/gigagit/internal/config"
)

// EffectiveConfig loads the effective config (defaults → global → the ACTIVE
// repo file) for this Service's repo: the committed <top>/.gg.toml, overridden
// by a machine-local private file keyed on the MAIN worktree when one exists.
func (s *Service) EffectiveConfig(ctx context.Context) (config.Config, error) {
	top, err := s.TopLevel(ctx)
	if err != nil {
		return config.Config{}, err
	}
	privatePath := ""
	if wts, werr := s.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
		privatePath = config.PrivateRepoPath(wts[0].Path)
	}
	active := config.ActiveRepoConfigPath(filepath.Join(top, ".gg.toml"), privatePath)
	return config.Load(config.DefaultGlobalPath(), active)
}
```

Replace the body of `loadConfigFor` in `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/review.go` (lines 155–167) with:

```go
// loadConfigFor loads the effective config (global + active repo) for svc's
// repo. The resolution itself lives in domain (Service.EffectiveConfig) so the
// MCP frontend, which cannot import internal/cli, shares it.
func loadConfigFor(svc *domain.Service) (config.Config, error) {
	return svc.EffectiveConfig(context.Background())
}
```

Remove the now-unused `"path/filepath"` import from `internal/cli/review.go` if `go build` reports it.

- [ ] **Step 8: Run the tests to verify they pass**

Run:
```
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
gofmt -l internal/domain internal/cli internal/web && \
go test ./internal/domain -run 'TestNoteTarget|TestNoteAddNormalises|TestNoteAddKeepsCommit|TestNoteGetAndNoteAddresses|TestWaitNotesSweepBounded' -count=1 && \
go test ./internal/domain ./internal/web ./internal/cli ./internal/archtest -count=1
```
Expected: `gofmt -l` prints nothing; every package PASSes (the web note handler tests still pass — the wire object only GAINED `path`/`rev`/`range` keys).

- [ ] **Step 9: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
git add internal/domain/notes.go internal/domain/notes_sweep.go internal/domain/notetarget.go \
        internal/domain/notewire.go internal/domain/effective_config.go \
        internal/domain/notetarget_test.go internal/domain/notes_test.go \
        internal/cli/review.go internal/web/notes.go && \
git commit -m "feat(domain): note target rule, full-sha commit notes, wire shape, bounded sweep wait" \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" \
  -m "Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 3: `gg diff --hunks [--json]`

**Files:**
- Modify: `internal/cli/diff.go` (flags, dispatch, `renderHunks`)
- Modify: `internal/cli/diff_test.go` (add the new cases)

**Interfaces:**
- Consumes: `domain.HunkDiffSpec(cached bool, rev string, paths []string) model.DiffSpec`, `(*domain.Service).DiffHunks(ctx, model.DiffSpec) ([]model.FileHunks, error)`, `model.FileHunks`/`model.Hunk` (Task 1); `splitDashDash` (`internal/cli/diff.go:70`).
- Produces: the `gg diff --hunks` surface plus `func renderHunks(w io.Writer, files []model.FileHunks)` and `func hunksJSON(w io.Writer, files []model.FileHunks) error`, consumed by no other task but mirrored by the skill text (Task 10) and `using-gg.md` (Task 11).

- [ ] **Step 1: Write the failing test**

Append to `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/diff_test.go`:

```go
func TestDiffHunksListsNumberedHunks(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	body := ""
	for i := 0; i < 40; i++ {
		body += "line\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	edited := strings.Replace(body, "line\n", "TOP\n", 1)
	edited = edited[:len(edited)-len("line\n")] + "BOTTOM\n"
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errb := runCLI(t, dir, "diff", "--hunks")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "a.txt\n") {
		t.Fatalf("stdout must name the file:\n%s", out)
	}
	if !strings.Contains(out, "  1 @@ -") || !strings.Contains(out, "  2 @@ -") {
		t.Fatalf("stdout must list two numbered hunks:\n%s", out)
	}
}

func TestDiffHunksJSONShape(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCLI(t, dir, "diff", "--hunks", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	var got []struct {
		Path  string `json:"path"`
		Hunks []struct {
			N      int    `json:"n"`
			Old    [2]int `json:"old"`
			New    [2]int `json:"new"`
			Header string `json:"header"`
		} `json:"hunks"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not the documented JSON array: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0].Path != "a.txt" || len(got[0].Hunks) != 1 || got[0].Hunks[0].N != 1 {
		t.Fatalf("json = %+v, want a.txt with hunk n=1", got)
	}
}

// A single commit positional means THAT COMMIT'S OWN change (<c>^..<c>) — the
// same patch a `gg note add --rev <c>` note anchors to — so the number an agent
// reads is the number it can pass back.
func TestDiffHunksSingleCommitIsItsOwnChange(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "grow")
	sha := runGit(t, dir, "rev-parse", "HEAD")

	code, out, errb := runCLI(t, dir, "diff", "--hunks", sha)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, "  1 @@") {
		t.Fatalf("a clean checkout must still show the commit's own hunk:\n%s", out)
	}
}

func TestDiffHunksRejectsStatCombination(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "diff", "--hunks", "--stat")
	if code != 2 {
		t.Fatalf("exit=%d stderr=%s, want 2 (usage error)", code, errb)
	}
}
```

Add `"encoding/json"`, `"os"`, `"path/filepath"` and `"strings"` to that test file's import block if any are missing.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && go test ./internal/cli -run TestDiffHunks -count=1`
Expected: FAIL — `flag provided but not defined: -hunks`, exit 2 where 0 is wanted.

- [ ] **Step 3: Implement `--hunks`**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/diff.go`, add the two flags after `cached`:

```go
	hunks := fs.Bool("hunks", false, "list each file's numbered git @@ hunks instead of the patch")
	asJSON := fs.Bool("json", false, "with --hunks: emit the hunk list as JSON")
```

Replace the mutual-exclusion check and usage line with:

```go
	if *stat && *nameOnly {
		fmt.Fprintln(stderr, "diff: --stat and --name-only are mutually exclusive")
		return 2
	}
	if *hunks && (*stat || *nameOnly) {
		fmt.Fprintln(stderr, "diff: --hunks is mutually exclusive with --stat/--name-only")
		return 2
	}
	if *asJSON && !*hunks {
		fmt.Fprintln(stderr, "diff: --json requires --hunks")
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "usage: gg diff [--stat|--name-only|--hunks [--json]] [--cached] [<rev>|<A..B>] [-- <paths>...]")
		return 2
	}
```

Then, immediately after `rev` is read and BEFORE `spec := model.DiffSpec{...}`, insert:

```go
	if *hunks {
		// --hunks numbers the patch a NOTE anchors to, so a bare commit means
		// that commit's own change (<c>^..<c>), not `git diff <c>`. HunkDiffSpec
		// is the single source of that rule, shared with `gg note add --hunk N`.
		files, err := svc.DiffHunks(context.Background(), domain.HunkDiffSpec(*cached, rev, paths))
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if *asJSON {
			if err := hunksJSON(stdout, files); err != nil {
				fmt.Fprintln(stderr, "error:", err)
				return 1
			}
			return 0
		}
		renderHunks(stdout, files)
		return 0
	}
```

Append the two renderers to the same file:

```go
// renderHunks prints one path line per file followed by its numbered hunks:
//
//	src/search.ts
//	  1 @@ -15,7 +15,9 @@ export function score
//	  2 @@ -40,3 +42,8 @@
//
// The @@ line is REBUILT from the parsed range, so a header git wrote with an
// omitted count ("@@ -40 +42 @@") prints with explicit counts. A file with no
// hunks (binary) still prints its path, so an agent sees it changed.
func renderHunks(w io.Writer, files []model.FileHunks) {
	for _, f := range files {
		name := f.Path
		if f.OldPath != "" {
			name = f.OldPath + " => " + f.Path
		}
		fmt.Fprintln(w, name)
		for _, h := range f.Hunks {
			line := fmt.Sprintf("  %d @@ -%s +%s @@", h.N, hunkSideSpan(h.Old), hunkSideSpan(h.New))
			if h.Header != "" {
				line += " " + h.Header
			}
			fmt.Fprintln(w, line)
		}
	}
}

// hunkSideSpan renders one side of a rebuilt @@ header as "start,count".
func hunkSideSpan(r [2]int) string {
	if r == [2]int{0, 0} {
		return "0,0"
	}
	return fmt.Sprintf("%d,%d", r[0], r[1]-r[0]+1)
}

// wireHunk / wireFileHunks are the --json shape (spec §4.5):
// [{"path":"src/search.ts","hunks":[{"n":1,"old":[15,21],"new":[15,23],"header":"…"}]}]
type wireHunk struct {
	N      int    `json:"n"`
	Old    [2]int `json:"old"`
	New    [2]int `json:"new"`
	Header string `json:"header"`
}

type wireFileHunks struct {
	Path    string     `json:"path"`
	OldPath string     `json:"old_path,omitempty"`
	Hunks   []wireHunk `json:"hunks"`
}

func hunksJSON(w io.Writer, files []model.FileHunks) error {
	out := make([]wireFileHunks, 0, len(files))
	for _, f := range files {
		wf := wireFileHunks{Path: f.Path, OldPath: f.OldPath, Hunks: []wireHunk{}}
		for _, h := range f.Hunks {
			wf.Hunks = append(wf.Hunks, wireHunk{N: h.N, Old: h.Old, New: h.New, Header: h.Header})
		}
		out = append(out, wf)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "")
	return enc.Encode(out)
}
```

Add `"encoding/json"` to `internal/cli/diff.go`'s import block.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && gofmt -l internal/cli/diff.go internal/cli/diff_test.go && go test ./internal/cli -run TestDiff -count=1`
Expected: `gofmt -l` prints nothing; PASS (the pre-existing `TestDiff*` cases stay green).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
git add internal/cli/diff.go internal/cli/diff_test.go && \
git commit -m "feat(cli): gg diff --hunks [--json] lists numbered git @@ hunks" \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" \
  -m "Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 4: `internal/notebatch` — the shared batch parser

**Why a new leaf package and not `internal/domain`:** the parser is pure text
validation with no git, no store and no Service — and BOTH `internal/cli` and
`internal/mcp` need it directly (each builds its own `model.Note` values from
the parsed items). A leaf keeps `domain` free of a second JSON dialect and
matches how this codebase already isolates pure logic (`rebaseplan`,
`template`, `textdiff`, `gitconfdocs`). It imports nothing from gg except
nothing at all — it is a DAG leaf with a standard-library-only import set.

**Files:**
- Create: `internal/notebatch/notebatch.go`
- Create: `internal/notebatch/notebatch_test.go`
- Modify: `internal/archtest/import_guard_test.go` (add the DAG row)

**Interfaces:**
- Consumes: nothing from gg (standard library only).
- Produces:
  ```go
  package notebatch

  type Target struct {
      Hunk    int    // 1-based git @@ hunk number; 0 = unset
      NewLine [2]int // 1-based inclusive new-side range; [0,0] = unset
      OldLine [2]int // 1-based inclusive old-side range; [0,0] = unset
  }
  func (t Target) IsSet() bool

  type Item struct {
      Path       string
      ReplyTo    string
      Target     Target
      Summary    string
      Rationale  string
      Author     string
      Tags       []string
      Confidence float64
  }

  type Batch struct {
      Items    []Item
      Contexts []string
  }

  func Parse(data []byte) (Batch, error)
  ```

- [ ] **Step 1: Write the failing test**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/notebatch/notebatch_test.go`:

```go
package notebatch

import (
	"strings"
	"testing"
)

func TestParseAgentContextV1(t *testing.T) {
	t.Parallel()
	in := `{"version":1,"summary":"overall: solid",
	  "files":[{"path":"src/search.ts","summary":"scoring changed",
	    "annotations":[
	      {"newRange":[15,23],"summary":"prefix beats substring","rationale":"why","author":"sonnet","tags":["perf"],"confidence":"high","markup":"<b>ignored</b>"},
	      {"oldRange":[40,40],"summary":"dead branch"}
	    ]}]}`
	b, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(b.Items) != 2 {
		t.Fatalf("items = %+v, want 2", b.Items)
	}
	i0 := b.Items[0]
	if i0.Path != "src/search.ts" || i0.Target.NewLine != [2]int{15, 23} || i0.Summary != "prefix beats substring" ||
		i0.Rationale != "why" || i0.Author != "sonnet" || len(i0.Tags) != 1 || i0.Confidence != 0.9 {
		t.Fatalf("item 0 = %+v", i0)
	}
	if b.Items[1].Target.OldLine != [2]int{40, 40} || b.Items[1].Target.NewLine != [2]int{0, 0} {
		t.Fatalf("item 1 = %+v, want an old-side anchor only", b.Items[1])
	}
	// Top-level and per-file summaries have no anchor: they are CONTEXT, echoed
	// to stderr by the caller, never stored as notes.
	if len(b.Contexts) != 2 || !strings.Contains(b.Contexts[0], "overall: solid") ||
		!strings.Contains(b.Contexts[1], "scoring changed") {
		t.Fatalf("contexts = %q, want the top-level and file summaries", b.Contexts)
	}
}

func TestParseAgentContextNewRangeWinsOverOld(t *testing.T) {
	t.Parallel()
	b, err := Parse([]byte(`{"files":[{"path":"a.go","annotations":[{"oldRange":[1,2],"newRange":[5,6],"summary":"s"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := b.Items[0].Target
	if got.NewLine != [2]int{5, 6} || got.OldLine != [2]int{0, 0} {
		t.Fatalf("target = %+v, want the new range to win", got)
	}
}

func TestParseCommentApplyShape(t *testing.T) {
	t.Parallel()
	in := `{"comments":[
	  {"filePath":"a.go","newLine":12,"summary":"one"},
	  {"filePath":"b.go","hunk":3,"summary":"two","rationale":"r","author":"gpt"},
	  {"filePath":"c.go","hunkNumber":2,"summary":"three"},
	  {"filePath":"d.go","oldLine":7,"summary":"four"},
	  {"replyTo":"a1b2c3d4","summary":"addressed"}
	]}`
	b, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(b.Items) != 5 {
		t.Fatalf("items = %d, want 5", len(b.Items))
	}
	if b.Items[0].Target.NewLine != [2]int{12, 12} {
		t.Fatalf("newLine → single-line range: %+v", b.Items[0].Target)
	}
	if b.Items[1].Target.Hunk != 3 || b.Items[2].Target.Hunk != 2 {
		t.Fatalf("hunk/hunkNumber must both set Target.Hunk: %+v %+v", b.Items[1].Target, b.Items[2].Target)
	}
	if b.Items[3].Target.OldLine != [2]int{7, 7} {
		t.Fatalf("oldLine → single-line range: %+v", b.Items[3].Target)
	}
	if b.Items[4].ReplyTo != "a1b2c3d4" || b.Items[4].Target.IsSet() || b.Items[4].Path != "" {
		t.Fatalf("a reply names no file and no target: %+v", b.Items[4])
	}
}

func TestParseRejectsBadBatches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"neither key", `{"notes":[]}`, `"files"`},
		{"both keys", `{"files":[],"comments":[]}`, "exactly one"},
		{"not json", `nope`, "invalid JSON"},
		{"file without path", `{"files":[{"annotations":[{"newRange":[1,1],"summary":"s"}]}]}`, "non-empty path"},
		{"annotation without summary", `{"files":[{"path":"a.go","annotations":[{"newRange":[1,1]}]}]}`, "summary"},
		{"annotation without range", `{"files":[{"path":"a.go","annotations":[{"summary":"s"}]}]}`, "oldRange or newRange"},
		{"range not ordered", `{"files":[{"path":"a.go","annotations":[{"newRange":[9,2],"summary":"s"}]}]}`, "ordered"},
		{"range not 1-based", `{"files":[{"path":"a.go","annotations":[{"newRange":[0,3],"summary":"s"}]}]}`, "1-based"},
		{"comment with two targets", `{"comments":[{"filePath":"a.go","newLine":1,"oldLine":2,"summary":"s"}]}`, "exactly one of"},
		{"comment with no target", `{"comments":[{"filePath":"a.go","summary":"s"}]}`, "exactly one of"},
		{"comment with no summary", `{"comments":[{"filePath":"a.go","newLine":1}]}`, "summary"},
		{"unsupported version", `{"version":2,"files":[]}`, "version"},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.in))
		if err == nil {
			t.Errorf("%s: want an error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %q, want it to contain %q", c.name, err, c.want)
		}
	}
}

// One bad item rejects the WHOLE batch: nothing may be stored from a batch that
// does not fully validate.
func TestParseOneBadItemRejectsEverything(t *testing.T) {
	t.Parallel()
	in := `{"files":[{"path":"a.go","annotations":[
	  {"newRange":[1,1],"summary":"fine"},
	  {"newRange":[2,2]}
	]}]}`
	if _, err := Parse([]byte(in)); err == nil || !strings.Contains(err.Error(), "files[0].annotations[1]") {
		t.Fatalf("err = %v, want a rejection naming the bad item's index", err)
	}
}

func TestParseConfidenceWords(t *testing.T) {
	t.Parallel()
	for word, want := range map[string]float64{"low": 0.3, "medium": 0.6, "high": 0.9, "bogus": 0} {
		in := `{"files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"s","confidence":"` + word + `"}]}]}`
		b, err := Parse([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", word, err)
		}
		if b.Items[0].Confidence != want {
			t.Errorf("confidence %q = %v, want %v", word, b.Items[0].Confidence, want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && go test ./internal/notebatch -count=1`
Expected: FAIL — the package does not exist yet (`no Go files` / `undefined: Parse`).

- [ ] **Step 3: Write the parser**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/notebatch/notebatch.go`:

```go
// Package notebatch parses the two JSON batch shapes an AI agent may hand gg
// for `gg note apply --stdin`, the `gg_notes_apply` MCP tool and the
// `gg review --notes` import:
//
//  1. hunk's agent-context v1 sidecar
//     {"version":1,"summary":"…","files":[{"path":"…","summary":"…",
//       "annotations":[{"newRange":[a,b],"summary":"…","rationale":"…"}]}]}
//  2. hunk's `comment apply` batch
//     {"comments":[{"filePath":"…","newLine":12,"summary":"…"}]}
//
// It is a DAG leaf: pure validation over bytes, standard library only, no git
// and no store. The whole batch is validated before the caller writes anything,
// so a single bad item stores nothing. Anchor RESOLUTION (a hunk number → a
// line range, a path → an address) belongs to the caller and to domain; this
// package only says what the JSON meant.
package notebatch

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Target is how one item anchors. Exactly one of Hunk / NewLine / OldLine is
// set on a root item; a reply carries none (it inherits its parent's anchor).
type Target struct {
	Hunk    int
	NewLine [2]int
	OldLine [2]int
}

// IsSet reports whether any anchor field carries a value.
func (t Target) IsSet() bool {
	return t.Hunk != 0 || t.NewLine != [2]int{0, 0} || t.OldLine != [2]int{0, 0}
}

// Item is one validated note to create. Path is the repo-relative path exactly
// as the agent wrote it (the caller normalises notation); ReplyTo, when set,
// makes this a reply and leaves Path and Target empty.
type Item struct {
	Path       string
	ReplyTo    string
	Target     Target
	Summary    string
	Rationale  string
	Author     string
	Tags       []string
	Confidence float64
}

// Batch is a parsed input: the notes to create plus the unanchored prose
// (the top-level and per-file summaries of agent-context v1), which gg has
// nowhere to hang and therefore echoes to stderr instead of storing.
type Batch struct {
	Items    []Item
	Contexts []string
}

// confidenceWords maps hunk's three-value confidence enum onto model.Note's
// float64 field. Anything else is dropped (0 = unset), as hunk does.
var confidenceWords = map[string]float64{"low": 0.3, "medium": 0.6, "high": 0.9}

type rawTop struct {
	Version  *int              `json:"version"`
	Summary  *string           `json:"summary"`
	Files    []json.RawMessage `json:"files"`
	Comments []json.RawMessage `json:"comments"`
}

type rawFile struct {
	Path        string            `json:"path"`
	Summary     *string           `json:"summary"`
	Annotations []json.RawMessage `json:"annotations"`
}

type rawAnnotation struct {
	Summary    *string  `json:"summary"`
	Rationale  *string  `json:"rationale"`
	Author     *string  `json:"author"`
	Tags       []any    `json:"tags"`
	Confidence *string  `json:"confidence"`
	OldRange   []any    `json:"oldRange"`
	NewRange   []any    `json:"newRange"`
}

type rawComment struct {
	FilePath   string  `json:"filePath"`
	ReplyTo    string  `json:"replyTo"`
	NewLine    *int    `json:"newLine"`
	OldLine    *int    `json:"oldLine"`
	Hunk       *int    `json:"hunk"`
	HunkNumber *int    `json:"hunkNumber"`
	Summary    *string `json:"summary"`
	Rationale  *string `json:"rationale"`
	Author     *string `json:"author"`
}

// Parse reads either supported shape, chosen by the top-level key, and returns
// the validated batch. Every error names the offending item's index.
func Parse(data []byte) (Batch, error) {
	var top rawTop
	if err := json.Unmarshal(data, &top); err != nil {
		return Batch{}, fmt.Errorf("invalid JSON batch: %v", err)
	}
	hasFiles, hasComments := top.Files != nil, top.Comments != nil
	if hasFiles == hasComments {
		return Batch{}, fmt.Errorf(`a batch needs exactly one of a top-level "files" array (agent-context v1) or "comments" array (comment apply)`)
	}
	if hasFiles {
		return parseAgentContext(top)
	}
	return parseComments(top)
}

func parseAgentContext(top rawTop) (Batch, error) {
	if top.Version != nil && *top.Version != 0 && *top.Version != 1 {
		return Batch{}, fmt.Errorf("unsupported agent-context version %d (gg reads version 1)", *top.Version)
	}
	var b Batch
	if s := trimPtr(top.Summary); s != "" {
		b.Contexts = append(b.Contexts, s)
	}
	for fi, rawF := range top.Files {
		var f rawFile
		if err := json.Unmarshal(rawF, &f); err != nil {
			return Batch{}, fmt.Errorf("files[%d]: %v", fi, err)
		}
		if strings.TrimSpace(f.Path) == "" {
			return Batch{}, fmt.Errorf("files[%d]: a file entry requires a non-empty path", fi)
		}
		if s := trimPtr(f.Summary); s != "" {
			b.Contexts = append(b.Contexts, f.Path+": "+s)
		}
		for ai, rawA := range f.Annotations {
			where := fmt.Sprintf("files[%d].annotations[%d]", fi, ai)
			var a rawAnnotation
			if err := json.Unmarshal(rawA, &a); err != nil {
				return Batch{}, fmt.Errorf("%s: %v", where, err)
			}
			summary := trimPtr(a.Summary)
			if summary == "" {
				return Batch{}, fmt.Errorf("%s: each annotation requires a summary", where)
			}
			oldR, err := parseRange(a.OldRange, where+".oldRange")
			if err != nil {
				return Batch{}, err
			}
			newR, err := parseRange(a.NewRange, where+".newRange")
			if err != nil {
				return Batch{}, err
			}
			if oldR == ([2]int{0, 0}) && newR == ([2]int{0, 0}) {
				return Batch{}, fmt.Errorf("%s: an annotation needs an oldRange or newRange", where)
			}
			if newR != ([2]int{0, 0}) {
				oldR = [2]int{0, 0} // newRange wins when both are present
			}
			it := Item{
				Path:      f.Path,
				Target:    Target{NewLine: newR, OldLine: oldR},
				Summary:   summary,
				Rationale: trimPtr(a.Rationale),
				Author:    trimPtr(a.Author),
			}
			for _, tag := range a.Tags {
				if s, ok := tag.(string); ok && strings.TrimSpace(s) != "" {
					it.Tags = append(it.Tags, s)
				}
			}
			if a.Confidence != nil {
				it.Confidence = confidenceWords[strings.ToLower(strings.TrimSpace(*a.Confidence))]
			}
			b.Items = append(b.Items, it)
		}
	}
	return b, nil
}

func parseComments(top rawTop) (Batch, error) {
	var b Batch
	for ci, rawC := range top.Comments {
		where := fmt.Sprintf("comments[%d]", ci)
		var c rawComment
		if err := json.Unmarshal(rawC, &c); err != nil {
			return Batch{}, fmt.Errorf("%s: %v", where, err)
		}
		summary := trimPtr(c.Summary)
		if summary == "" {
			return Batch{}, fmt.Errorf("%s: each comment requires a summary", where)
		}
		it := Item{
			Summary:   summary,
			Rationale: trimPtr(c.Rationale),
			Author:    trimPtr(c.Author),
			ReplyTo:   strings.TrimSpace(c.ReplyTo),
		}
		if it.ReplyTo != "" {
			// A reply inherits its parent's anchor: naming a target too is a
			// contradiction, not a refinement.
			if c.FilePath != "" || c.NewLine != nil || c.OldLine != nil || c.Hunk != nil || c.HunkNumber != nil {
				return Batch{}, fmt.Errorf("%s: replyTo takes no filePath or target", where)
			}
			b.Items = append(b.Items, it)
			continue
		}
		if strings.TrimSpace(c.FilePath) == "" {
			return Batch{}, fmt.Errorf("%s: a root comment requires filePath", where)
		}
		hunk := c.Hunk
		if hunk == nil {
			hunk = c.HunkNumber
		} else if c.HunkNumber != nil && *c.HunkNumber != *c.Hunk {
			return Batch{}, fmt.Errorf("%s: hunk and hunkNumber disagree", where)
		}
		set := 0
		var target Target
		if c.NewLine != nil {
			set++
			if *c.NewLine < 1 {
				return Batch{}, fmt.Errorf("%s: newLine must be a 1-based line number", where)
			}
			target.NewLine = [2]int{*c.NewLine, *c.NewLine}
		}
		if c.OldLine != nil {
			set++
			if *c.OldLine < 1 {
				return Batch{}, fmt.Errorf("%s: oldLine must be a 1-based line number", where)
			}
			target.OldLine = [2]int{*c.OldLine, *c.OldLine}
		}
		if hunk != nil {
			set++
			if *hunk < 1 {
				return Batch{}, fmt.Errorf("%s: hunk must be a 1-based hunk number", where)
			}
			target.Hunk = *hunk
		}
		if set != 1 {
			return Batch{}, fmt.Errorf("%s: pass exactly one of hunk, hunkNumber, newLine or oldLine", where)
		}
		it.Path, it.Target = c.FilePath, target
		b.Items = append(b.Items, it)
	}
	return b, nil
}

// parseRange validates a [start,end] tuple: two integers, both >= 1, ordered.
// A missing tuple is [0,0] (unset), never an error.
func parseRange(v []any, where string) ([2]int, error) {
	if v == nil {
		return [2]int{0, 0}, nil
	}
	if len(v) != 2 {
		return [2]int{0, 0}, fmt.Errorf("%s: a range is a [start,end] pair", where)
	}
	var out [2]int
	for i, raw := range v {
		f, ok := raw.(float64)
		if !ok || f != float64(int(f)) {
			return [2]int{0, 0}, fmt.Errorf("%s: ranges must be integer tuples", where)
		}
		out[i] = int(f)
	}
	if out[0] < 1 || out[1] < 1 {
		return [2]int{0, 0}, fmt.Errorf("%s: ranges must use positive 1-based line numbers", where)
	}
	if out[1] < out[0] {
		return [2]int{0, 0}, fmt.Errorf("%s: ranges must be ordered start..end tuples", where)
	}
	return out, nil
}

func trimPtr(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}
```

- [ ] **Step 4: Add the archtest DAG row**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/archtest/import_guard_test.go`, inside `TestLayeringDAG`'s `cases` map, add (keeping the existing rows untouched):

```go
		"notebatch":   {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && gofmt -l internal/notebatch internal/archtest && go test ./internal/notebatch ./internal/archtest -count=1`
Expected: `gofmt -l` prints nothing; both packages PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
git add internal/notebatch/notebatch.go internal/notebatch/notebatch_test.go internal/archtest/import_guard_test.go && \
git commit -m "feat(notebatch): pure parser for agent-context v1 and comment-apply batches" \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" \
  -m "Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 5: `gg note add / reply / rm` and the housekeeping wrapper

**Files:**
- Create: `internal/cli/note.go`
- Create: `internal/cli/note_test.go`
- Modify: `internal/cli/cli.go` (`runOne` case, `commands` map)

**Interfaces:**
- Consumes: `(*domain.Service).NoteTarget/NoteAdd/NoteReply/NoteRemove/NoteGet/HunkRange/EffectiveConfig/SetNotesPolicy/StartNotesSweep/WaitNotesSweep`, `domain.HunkDiffSpec`, `domain.ErrNoteTargetUsage`, `domain.ErrNoteNotFound`, `domain.ToWireNote`, `model.Note`, `model.NoteSourceAgent`/`NoteSourceUser`, `model.NoteSideNew`/`NoteSideOld`, `model.NoteActive`.
- Produces (consumed by Tasks 6, 7, 11):
  ```go
  func cmdNote(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int
  func withNotesHousekeeping(svc *domain.Service, fn func() int) int
  func noteAuthorDefault(given string) string           // --author → $GG_AGENT → "agent"
  func noteSourceValue(given string) (model.NoteSource, error)
  func noteAnchor(ctx context.Context, svc *domain.Service, addr model.FileAddress, cached bool, rev string, hunk, newLine, oldLine int) (model.NoteSide, [2]int, error)
  func printNote(w io.Writer, n model.Note, asJSON bool) error
  ```

- [ ] **Step 1: Write the failing test**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/note_test.go`:

```go
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noteRepo is a real repo with a committed file, a working-tree edit and an
// untracked file — the three default target states of §4.5.
// It sets XDG_STATE_HOME, so it must NOT be used from a t.Parallel() test.
func noteRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	body := "alpha\nbravo\ncharlie\ndelta\n"
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\nBRAVO\ncharlie\ndelta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNoteAddByNewLinePrintsID(t *testing.T) {
	dir := noteRepo(t)
	code, out, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "shouty")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	id := strings.TrimSpace(out)
	if len(id) != 8 {
		t.Fatalf("stdout = %q, want the new 8-hex note id on one line", out)
	}
}

func TestNoteAddJSONCarriesWireShape(t *testing.T) {
	dir := noteRepo(t)
	code, out, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2",
		"--summary", "shouty", "--rationale", "why", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	var got struct {
		ID        string `json:"id"`
		Source    string `json:"source"`
		Author    string `json:"author"`
		Path      string `json:"path"`
		Side      string `json:"side"`
		Line      int    `json:"line"`
		Range     [2]int `json:"range"`
		Summary   string `json:"summary"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not a wire note: %v\n%s", err, out)
	}
	if got.Source != "agent" || got.Author != "agent" {
		t.Errorf("CLI notes default to source/author agent: %+v", got)
	}
	if got.Path != "a.txt" || got.Side != "new" || got.Range != [2]int{2, 2} || got.Line != 2 {
		t.Errorf("anchor = %+v, want a.txt new 2-2", got)
	}
	if got.Summary != "shouty" || got.Rationale != "why" {
		t.Errorf("text = %+v", got)
	}
}

func TestNoteAddSourceUserAndAgentEnvAuthor(t *testing.T) {
	dir := noteRepo(t)
	t.Setenv("GG_AGENT", "sonnet")
	code, out, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1",
		"--summary", "s", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, `"author":"sonnet"`) {
		t.Fatalf("$GG_AGENT must become the default author: %s", out)
	}
	code, out, errb = runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "3",
		"--summary", "human", "--source", "user", "--author", "ada", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, `"source":"user"`) || !strings.Contains(out, `"author":"ada"`) {
		t.Fatalf("explicit --source/--author must win: %s", out)
	}
}

func TestNoteAddByHunkAnchorsTheWholeHunk(t *testing.T) {
	dir := noteRepo(t)
	code, out, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--hunk", "1", "--summary", "hunk note", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	var got struct {
		Side  string `json:"side"`
		Range [2]int `json:"range"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Side != "new" || got.Range[0] < 1 || got.Range[1] < got.Range[0] {
		t.Fatalf("hunk anchor = %+v, want the hunk's whole new-side span", got)
	}
}

func TestNoteAddHunkPastEndIsExit1(t *testing.T) {
	dir := noteRepo(t)
	code, _, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--hunk", "9", "--summary", "s")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "has 1 hunks") {
		t.Fatalf("stderr = %q, want it to name the file's hunk count", errb)
	}
}

func TestNoteAddUntrackedFileIsUntrackedState(t *testing.T) {
	dir := noteRepo(t)
	code, _, errb := runCLI(t, dir, "note", "add", "--file", "fresh.txt", "--new-line", "1", "--summary", "s")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s (an untracked file has no index side; the state must absorb that)", code, errb)
	}
}

func TestNoteAddRevStoresFullSHA(t *testing.T) {
	dir := noteRepo(t)
	sha := runGit(t, dir, "rev-parse", "HEAD")
	code, out, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1",
		"--rev", sha[:7], "--summary", "commit note", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, `"rev":"`+sha+`"`) {
		t.Fatalf("a commit note must store the FULL sha: %s", out)
	}
}

func TestNoteAddUsageErrors(t *testing.T) {
	dir := noteRepo(t)
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"range rev", []string{"note", "add", "--file", "a.txt", "--new-line", "1", "--rev", "main..HEAD", "--summary", "s"}, "one commit"},
		{"cached with rev", []string{"note", "add", "--file", "a.txt", "--new-line", "1", "--cached", "--rev", "HEAD", "--summary", "s"}, "mutually exclusive"},
		{"no target", []string{"note", "add", "--file", "a.txt", "--summary", "s"}, "exactly one of"},
		{"two targets", []string{"note", "add", "--file", "a.txt", "--new-line", "1", "--old-line", "1", "--summary", "s"}, "exactly one of"},
		{"no summary", []string{"note", "add", "--file", "a.txt", "--new-line", "1"}, "--summary"},
		{"bad source", []string{"note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s", "--source", "robot"}, "user or agent"},
	} {
		code, _, errb := runCLI(t, dir, c.args...)
		if code != 2 {
			t.Errorf("%s: exit=%d stderr=%s, want 2", c.name, code, errb)
			continue
		}
		if !strings.Contains(errb, c.want) {
			t.Errorf("%s: stderr = %q, want it to contain %q", c.name, errb, c.want)
		}
	}
}

func TestNoteReplyInheritsAnchorAndRmRemovesThread(t *testing.T) {
	dir := noteRepo(t)
	_, out, _ := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "root")
	root := strings.TrimSpace(out)

	code, out, errb := runCLI(t, dir, "note", "reply", root, "--summary", "addressed", "--json")
	if code != 0 {
		t.Fatalf("reply exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, `"parent_id":"`+root+`"`) || !strings.Contains(out, `"range":[2,2]`) {
		t.Fatalf("a reply must inherit the parent's anchor: %s", out)
	}

	if code, _, errb = runCLI(t, dir, "note", "rm", root); code != 0 {
		t.Fatalf("rm exit=%d stderr=%s", code, errb)
	}
	if code, _, errb = runCLI(t, dir, "note", "rm", root); code != 1 {
		t.Fatalf("removing a gone note = %d stderr=%s, want exit 1", code, errb)
	}
}

func TestNoteUnknownSubcommandIsUsage(t *testing.T) {
	dir := noteRepo(t)
	code, _, errb := runCLI(t, dir, "note", "frobnicate")
	if code != 2 || !strings.Contains(errb, "unknown subcommand") {
		t.Fatalf("exit=%d stderr=%q, want 2 + an unknown-subcommand message", code, errb)
	}
}

func TestNoteIsAKnownCommand(t *testing.T) {
	t.Parallel()
	for _, verb := range []string{"note", "skill"} {
		if !IsCommand(verb) {
			t.Errorf("%q must be in the commands map (cmd/gg routing, help, gg batch)", verb)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && go test ./internal/cli -run TestNote -count=1`
Expected: FAIL — `unknown command "note"` (exit 2 everywhere), `IsCommand("note") == false`.

- [ ] **Step 3: Write `internal/cli/note.go`**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/note.go`:

```go
package cli

// `gg note …` — the agent lane's write surface over the phase 1 note store.
//
// Every verb is short-lived: it applies the effective [notes] policy, does its
// own work FIRST, then gives the startup housekeeping sweep a bounded budget
// (withNotesHousekeeping). A sweep that does not finish in time is abandoned;
// it is idempotent and lock-protected, so the next gg start retries it.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// notesSweepBudget is how long a one-shot `gg note …` waits for housekeeping
// after its own work is done. Short on purpose: the user's command has already
// succeeded, and the sweep is best-effort maintenance.
const notesSweepBudget = 2 * time.Second

// cmdNote dispatches `gg note <add|reply|rm|list|clear|apply> ...`.
func cmdNote(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg note <add|reply|rm|list|clear|apply> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	return withNotesHousekeeping(svc, func() int {
		switch sub {
		case "add":
			return noteAdd(svc, rest, stdout, stderr)
		case "reply":
			return noteReply(svc, rest, stdout, stderr)
		case "rm":
			return noteRemove(svc, rest, stdout, stderr)
		default:
			fmt.Fprintf(stderr, "note: unknown subcommand %q\n", sub)
			return 2
		}
	})
}

// withNotesHousekeeping applies [notes] from the effective config, runs fn, and
// only THEN starts and briefly waits for the sweep — the caller's work must
// never queue behind maintenance.
func withNotesHousekeeping(svc *domain.Service, fn func() int) int {
	if cfg, err := svc.EffectiveConfig(context.Background()); err == nil {
		svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
	}
	code := fn()
	svc.StartNotesSweep()
	ctx, cancel := context.WithTimeout(context.Background(), notesSweepBudget)
	defer cancel()
	svc.WaitNotesSweep(ctx)
	return code
}

// noteTargetFlags is the shared --cached/--rev/--file flag trio.
type noteTargetFlags struct {
	file   *string
	cached *bool
	rev    *string
}

func addTargetFlags(fs *flag.FlagSet) noteTargetFlags {
	return noteTargetFlags{
		file:   fs.String("file", "", "repo-relative path the note anchors to"),
		cached: fs.Bool("cached", false, "anchor to the staged diff (HEAD → index)"),
		rev:    fs.String("rev", "", "anchor to a commit's own change (parent → commit)"),
	}
}

// noteAuthorDefault picks the author label: an explicit --author, else
// $GG_AGENT (the agent harness's own name), else the literal "agent".
func noteAuthorDefault(given string) string {
	if a := strings.TrimSpace(given); a != "" {
		return a
	}
	if a := strings.TrimSpace(os.Getenv("GG_AGENT")); a != "" {
		return a
	}
	return "agent"
}

// noteSourceValue validates --source. The CLI defaults to agent (a human
// scripting notes passes --source user); the TUI and web keep user.
func noteSourceValue(given string) (model.NoteSource, error) {
	switch strings.TrimSpace(given) {
	case "", "agent":
		return model.NoteSourceAgent, nil
	case "user":
		return model.NoteSourceUser, nil
	}
	return "", fmt.Errorf("--source must be user or agent")
}

// noteAnchor turns the three mutually exclusive anchor flags into the side and
// 1-based inclusive range a note occupies. --hunk resolves through the SAME
// patch `gg diff --hunks` numbers for this target.
func noteAnchor(ctx context.Context, svc *domain.Service, addr model.FileAddress, cached bool, rev string, hunk, newLine, oldLine int) (model.NoteSide, [2]int, error) {
	set := 0
	for _, v := range []int{hunk, newLine, oldLine} {
		if v != 0 {
			set++
		}
	}
	if set != 1 {
		return "", [2]int{}, fmt.Errorf("%w: pass exactly one of --hunk, --new-line or --old-line", domain.ErrNoteTargetUsage)
	}
	switch {
	case newLine != 0:
		if newLine < 1 {
			return "", [2]int{}, fmt.Errorf("%w: --new-line must be a 1-based line number", domain.ErrNoteTargetUsage)
		}
		return model.NoteSideNew, [2]int{newLine, newLine}, nil
	case oldLine != 0:
		if oldLine < 1 {
			return "", [2]int{}, fmt.Errorf("%w: --old-line must be a 1-based line number", domain.ErrNoteTargetUsage)
		}
		return model.NoteSideOld, [2]int{oldLine, oldLine}, nil
	default:
		if hunk < 1 {
			return "", [2]int{}, fmt.Errorf("%w: --hunk must be a 1-based hunk number", domain.ErrNoteTargetUsage)
		}
		spec := domain.HunkDiffSpec(cached, rev, []string{addr.Path})
		return svc.HunkRange(ctx, spec, addr.Path, hunk)
	}
}

// noteExit maps a domain error onto an exit code: 2 for a caller mistake in the
// target flags, 1 for everything else (an unreadable side, a missing note).
func noteExit(err error, stderr io.Writer) int {
	if errors.Is(err, domain.ErrNoteTargetUsage) {
		fmt.Fprintln(stderr, "note:", strings.TrimPrefix(err.Error(), "note target usage: "))
		return 2
	}
	fmt.Fprintln(stderr, "error:", err)
	return 1
}

// printNote prints a stored note: its id on one line, or the shared wire object
// with --json. A freshly written note is active by construction, so its wire
// status is "active" and its resolved range is the stored one.
func printNote(w io.Writer, n model.Note, asJSON bool) error {
	if !asJSON {
		_, err := fmt.Fprintln(w, n.ID)
		return err
	}
	wire := domain.ToWireNote(domain.ResolvedNote{Note: n, Status: model.NoteActive, Range: n.Range})
	return json.NewEncoder(w).Encode(wire)
}

func noteAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	hunk := fs.Int("hunk", 0, "anchor to git @@ hunk N of the file (see gg diff --hunks)")
	newLine := fs.Int("new-line", 0, "anchor to a 1-based line on the NEW side")
	oldLine := fs.Int("old-line", 0, "anchor to a 1-based line on the OLD side")
	summary := fs.String("summary", "", "the note (required)")
	rationale := fs.String("rationale", "", "the why, optional")
	author := fs.String("author", "", "author label (default: $GG_AGENT, else agent)")
	source := fs.String("source", "", "user or agent (default agent)")
	asJSON := fs.Bool("json", false, "print the created note as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*summary) == "" {
		fmt.Fprintln(stderr, "note add: --summary is required")
		return 2
	}
	src, err := noteSourceValue(*source)
	if err != nil {
		fmt.Fprintln(stderr, "note add:", err)
		return 2
	}
	ctx := context.Background()
	addr, err := svc.NoteTarget(ctx, *tf.file, *tf.cached, *tf.rev)
	if err != nil {
		return noteExit(err, stderr)
	}
	side, rng, err := noteAnchor(ctx, svc, addr, *tf.cached, *tf.rev, *hunk, *newLine, *oldLine)
	if err != nil {
		return noteExit(err, stderr)
	}
	stored, err := svc.NoteAdd(ctx, model.Note{
		Source: src, Author: noteAuthorDefault(*author), Address: addr,
		Side: side, Range: rng,
		Summary: strings.TrimSpace(*summary), Rationale: strings.TrimSpace(*rationale),
	})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := printNote(stdout, stored, *asJSON); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func noteReply(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note reply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	summary := fs.String("summary", "", "the reply (required)")
	rationale := fs.String("rationale", "", "the why, optional")
	author := fs.String("author", "", "author label (default: $GG_AGENT, else agent)")
	source := fs.String("source", "", "user or agent (default agent)")
	asJSON := fs.Bool("json", false, "print the created reply as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg note reply <note-id> --summary \"…\"")
		return 2
	}
	if strings.TrimSpace(*summary) == "" {
		fmt.Fprintln(stderr, "note reply: --summary is required")
		return 2
	}
	src, err := noteSourceValue(*source)
	if err != nil {
		fmt.Fprintln(stderr, "note reply:", err)
		return 2
	}
	// The reply's anchor is the parent's: domain copies address, side, range and
	// fingerprint, and flattens a reply-to-a-reply onto the thread root.
	stored, err := svc.NoteReply(context.Background(), fs.Arg(0), model.Note{
		Source: src, Author: noteAuthorDefault(*author),
		Summary: strings.TrimSpace(*summary), Rationale: strings.TrimSpace(*rationale),
	})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := printNote(stdout, stored, *asJSON); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func noteRemove(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg note rm <note-id>")
		return 2
	}
	// A root takes its replies with it (domain.NoteRemove).
	if err := svc.NoteRemove(context.Background(), fs.Arg(0)); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
```

- [ ] **Step 4: Wire the verb into the dispatcher**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/cli.go`, add to `runOne`'s switch, right after the `"bookmark"` case:

```go
	case "note":
		return cmdNote(svc, rest, stdin, stdout, stderr)
```

and extend the `commands` map's last line to:

```go
	"review": true, "apply": true, "versions": true, "unlock": true,
	"note": true, "skill": true,
```

(`skill` is added here so Task 10's verb is routed by `cmd/gg` the moment it exists; until then `runOne` returns "unknown command" for it, which no test asserts against.)

Note: `gg batch` reaches `note` for free — `cmdBatch` calls the same `runOne`, and the sweep still runs at most once per process because `StartNotesSweep` is a `sync.Once`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && gofmt -l internal/cli && go test ./internal/cli -run 'TestNote|TestBatch' -count=1`
Expected: `gofmt -l` prints nothing; PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
git add internal/cli/note.go internal/cli/note_test.go internal/cli/cli.go && \
git commit -m "feat(cli): gg note add/reply/rm with the shared target rule and a bounded sweep wait" \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" \
  -m "Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 6: `gg note list` and `gg note clear`

**Files:**
- Modify: `internal/cli/note.go` (two subverbs + the dispatcher cases)
- Modify: `internal/cli/note_test.go`

**Interfaces:**
- Consumes: `(*domain.Service).NotesAt(ctx, model.FileAddress) ([]domain.ResolvedNote, error)`, `(*domain.Service).NoteAddresses(ctx) ([]model.FileAddress, error)`, `(*domain.Service).NoteTarget`, `(*domain.Service).NoteRemove`, `domain.ToWireNote`, `domain.ResolvedNote{Note, Status, Range, Replies}`, `model.NoteSource`; `withNotesHousekeeping`, `addTargetFlags`, `noteExit` (Task 5).
- Produces: `func noteList(...) int`, `func noteClear(...) int`, `func renderNoteLine(w io.Writer, r domain.ResolvedNote, indent bool)`, `func noteTypeMatches(want string, src model.NoteSource) bool` — consumed by the e2e scenario (Task 11) and the skill text (Task 10).

- [ ] **Step 1: Write the failing test**

Append to `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/note_test.go`:

```go
func TestNoteListTextFormat(t *testing.T) {
	dir := noteRepo(t)
	_, out, _ := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "shouty")
	root := strings.TrimSpace(out)
	if code, _, errb := runCLI(t, dir, "note", "reply", root, "--summary", "addressed", "--source", "user"); code != 0 {
		t.Fatalf("reply: %s", errb)
	}

	code, out, errb := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want a root line and one indented reply:\n%s", out)
	}
	for _, want := range []string{root, "[agent]", "a.txt", "new:2-2", "active", "shouty"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("root line %q missing %q", lines[0], want)
		}
	}
	if !strings.HasPrefix(lines[1], "  ") || !strings.Contains(lines[1], "[user] reply") ||
		!strings.Contains(lines[1], "addressed") {
		t.Errorf("reply line = %q, want two-space indent + [user] reply + the text", lines[1])
	}
}

func TestNoteListJSONAndTypeFilter(t *testing.T) {
	dir := noteRepo(t)
	runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "by agent")
	runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "3", "--summary", "by human", "--source", "user")

	code, out, errb := runCLI(t, dir, "note", "list", "--file", "a.txt", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	var all []struct {
		Source  string `json:"source"`
		Status  string `json:"status"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &all); err != nil {
		t.Fatalf("--json must be an array of wire notes: %v\n%s", err, out)
	}
	if len(all) != 2 {
		t.Fatalf("json = %+v, want both notes", all)
	}
	for _, n := range all {
		if n.Status == "" {
			t.Errorf("every wire note carries a resolution status: %+v", n)
		}
	}

	_, out, _ = runCLI(t, dir, "note", "list", "--file", "a.txt", "--type", "user")
	if strings.Contains(out, "by agent") || !strings.Contains(out, "by human") {
		t.Fatalf("--type user must keep only user notes:\n%s", out)
	}
	_, out, _ = runCLI(t, dir, "note", "list", "--file", "a.txt", "--type", "agent")
	if !strings.Contains(out, "by agent") || strings.Contains(out, "by human") {
		t.Fatalf("--type agent must keep only agent notes:\n%s", out)
	}
	if code, _, errb := runCLI(t, dir, "note", "list", "--file", "a.txt", "--type", "robot"); code != 2 {
		t.Fatalf("exit=%d stderr=%s, want 2 for a bad --type", code, errb)
	}
}

// Without --file, list enumerates every address this checkout can see.
func TestNoteListWithoutFileCoversEveryTarget(t *testing.T) {
	dir := noteRepo(t)
	sha := runGit(t, dir, "rev-parse", "HEAD")
	runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "worktree note")
	runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--rev", sha, "--summary", "commit note")

	code, out, errb := runCLI(t, dir, "note", "list")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "worktree note") || !strings.Contains(out, "commit note") {
		t.Fatalf("bare list must show worktree AND commit notes:\n%s", out)
	}
}

func TestNoteClearGuardsAndCount(t *testing.T) {
	dir := noteRepo(t)
	runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "one")
	_, out, _ := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "two")
	root := strings.TrimSpace(out)
	runCLI(t, dir, "note", "reply", root, "--summary", "r")

	for _, c := range []struct {
		name string
		args []string
	}{
		{"no --yes", []string{"note", "clear", "--all"}},
		{"neither file nor all", []string{"note", "clear", "--yes"}},
		{"both file and all", []string{"note", "clear", "--all", "--file", "a.txt", "--yes"}},
	} {
		if code, _, errb := runCLI(t, dir, c.args...); code != 2 {
			t.Errorf("%s: exit=%d stderr=%s, want 2", c.name, code, errb)
		}
	}

	code, out, errb := runCLI(t, dir, "note", "clear", "--all", "--yes")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "removed 2 notes") {
		t.Fatalf("stdout = %q, want the removed THREAD count (a root takes its replies)", out)
	}
	_, out, _ = runCLI(t, dir, "note", "list")
	if strings.TrimSpace(out) != "" {
		t.Fatalf("clear --all must empty the store: %q", out)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && go test ./internal/cli -run 'TestNoteList|TestNoteClear' -count=1`
Expected: FAIL — `note: unknown subcommand "list"` / `"clear"`, exit 2.

- [ ] **Step 3: Implement `list` and `clear`**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/note.go`, add two cases to `cmdNote`'s switch, before `default`:

```go
		case "list":
			return noteList(svc, rest, stdout, stderr)
		case "clear":
			return noteClear(svc, rest, stdout, stderr)
```

Append to the same file:

```go
// noteTypeMatches applies --type. "all" (the default) keeps everything.
func noteTypeMatches(want string, src model.NoteSource) bool {
	switch want {
	case "", "all":
		return true
	case "user":
		return src == model.NoteSourceUser
	case "agent":
		return src == model.NoteSourceAgent
	}
	return false
}

// resolvedNotesFor gathers the resolved threads a --file / bare invocation
// covers: one address when --file is given, else every address this checkout
// can see (NoteAddresses). Orphaned notes never appear — NotesAt drops them by
// contract, the same rule the TUI and web rows follow.
func resolvedNotesFor(ctx context.Context, svc *domain.Service, file string, cached bool, rev string) ([]domain.ResolvedNote, error) {
	if strings.TrimSpace(file) != "" {
		addr, err := svc.NoteTarget(ctx, file, cached, rev)
		if err != nil {
			return nil, err
		}
		return svc.NotesAt(ctx, addr)
	}
	addrs, err := svc.NoteAddresses(ctx)
	if err != nil {
		return nil, err
	}
	var out []domain.ResolvedNote
	for _, a := range addrs {
		got, err := svc.NotesAt(ctx, a)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

// renderNoteLine prints one thread row:
//
//	a1b2c3d4 [agent] src/search.ts new:15-23 active  Prefix matches now outrank …
//	  e5f6a7b8 [user] reply  Addressed in the latest revision
//
// A reply carries no anchor of its own — it inherits the root's — so its line
// says "reply" where the root names its file, side and range.
func renderNoteLine(w io.Writer, r domain.ResolvedNote, indent bool) {
	if indent {
		fmt.Fprintf(w, "  %s [%s] reply  %s\n", r.Note.ID, r.Note.Source, r.Note.Summary)
		return
	}
	fmt.Fprintf(w, "%s [%s] %s %s:%d-%d %s  %s\n",
		r.Note.ID, r.Note.Source, noteTargetLabel(r.Note.Address),
		r.Note.Side, r.Range[0], r.Range[1], r.Status, r.Note.Summary)
}

// noteTargetLabel names a note's target in one column: the path, prefixed with
// the short commit for a commit note so two notes on the same path never look
// identical.
func noteTargetLabel(a model.FileAddress) string {
	if a.State == model.StateCommitted && len(a.Commit) >= 7 {
		return a.Commit[:7] + ":" + a.Path
	}
	return a.Path
}

func noteList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	typ := fs.String("type", "all", "user, agent or all")
	asJSON := fs.Bool("json", false, "emit the wire notes as a JSON array")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !noteTypeMatches(*typ, model.NoteSourceUser) && !noteTypeMatches(*typ, model.NoteSourceAgent) {
		fmt.Fprintln(stderr, "note list: --type must be user, agent or all")
		return 2
	}
	ctx := context.Background()
	res, err := resolvedNotesFor(ctx, svc, *tf.file, *tf.cached, *tf.rev)
	if err != nil {
		return noteExit(err, stderr)
	}
	kept := make([]domain.ResolvedNote, 0, len(res))
	for _, r := range res {
		if noteTypeMatches(*typ, r.Note.Source) {
			kept = append(kept, r)
		}
	}
	if *asJSON {
		wires := make([]domain.WireNote, 0, len(kept))
		for _, r := range kept {
			wires = append(wires, domain.ToWireNote(r))
		}
		if err := json.NewEncoder(stdout).Encode(wires); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}
	for _, r := range kept {
		renderNoteLine(stdout, r, false)
		for _, rep := range r.Replies {
			renderNoteLine(stdout, rep, true)
		}
	}
	return 0
}

func noteClear(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note clear", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	all := fs.Bool("all", false, "clear every note this checkout can see")
	typ := fs.String("type", "all", "user, agent or all")
	yes := fs.Bool("yes", false, "confirm the deletion (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	hasFile := strings.TrimSpace(*tf.file) != ""
	if hasFile == *all {
		fmt.Fprintln(stderr, "note clear: pass exactly one of --file <path> or --all")
		return 2
	}
	if !*yes {
		fmt.Fprintln(stderr, "note clear: refusing to delete without --yes")
		return 2
	}
	ctx := context.Background()
	res, err := resolvedNotesFor(ctx, svc, *tf.file, *tf.cached, *tf.rev)
	if err != nil {
		return noteExit(err, stderr)
	}
	removed := 0
	for _, r := range res {
		if !noteTypeMatches(*typ, r.Note.Source) {
			continue
		}
		// Removing a ROOT takes its replies with it, so only roots are removed
		// and only roots are counted.
		if err := svc.NoteRemove(ctx, r.Note.ID); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		removed++
	}
	fmt.Fprintf(stdout, "removed %d notes\n", removed)
	return 0
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && gofmt -l internal/cli && go test ./internal/cli -run TestNote -count=1`
Expected: `gofmt -l` prints nothing; PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
git add internal/cli/note.go internal/cli/note_test.go && \
git commit -m "feat(cli): gg note list/clear with text and --json output" \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" \
  -m "Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 7: `gg note apply --stdin`

**Files:**
- Create: `internal/cli/noteapply.go`
- Create: `internal/cli/noteapply_test.go`
- Modify: `internal/cli/note.go` (dispatcher case)

**Interfaces:**
- Consumes: `notebatch.Parse([]byte) (notebatch.Batch, error)`, `notebatch.Item`/`Target` (Task 4); `(*domain.Service).NoteTarget/NoteGet/HunkRange/NoteAdd/NoteReply`, `domain.HunkDiffSpec`, `domain.ToWireNote` (Tasks 1–2); `noteAuthorDefault`, `noteExit`, `addTargetFlags` (Task 5).
- Produces (consumed by Task 8's review importer and Task 9's MCP tool):
  ```go
  type plannedNote struct {
      Note    model.Note // ready to store; zero ParentID for a root
      ReplyTo string     // non-empty makes it a reply
  }
  func planNoteBatch(ctx context.Context, svc *domain.Service, b notebatch.Batch, cached bool, rev, author string, sideRule noteSideRule) (planned []plannedNote, skipped int, err error)
  func applyNoteBatch(ctx context.Context, svc *domain.Service, planned []plannedNote) ([]model.Note, error)

  type noteSideRule int
  const (
      sideRuleBoth    noteSideRule = iota // a commit target: both sides are addressable
      sideRuleNewOnly                     // a range or working review: only the new side is
  )
  ```

- [ ] **Step 1: Write the failing test**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/noteapply_test.go`:

```go
package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNoteApplyAgentContextShape(t *testing.T) {
	dir := noteRepo(t)
	in := `{"version":1,"summary":"overall fine","files":[{"path":"a.txt","summary":"scoring",
	  "annotations":[{"newRange":[2,2],"summary":"shouty","rationale":"why"},
	                 {"newRange":[3,4],"summary":"span note"}]}]}`
	code, out, errb := runCLIStdin(t, dir, in, "note", "apply", "--stdin")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	ids := strings.Fields(strings.TrimSpace(out))
	if len(ids) != 2 {
		t.Fatalf("stdout = %q, want one id per stored note", out)
	}
	// Unanchored prose is CONTEXT: echoed, never stored.
	if !strings.Contains(errb, "context: overall fine") || !strings.Contains(errb, "context: a.txt: scoring") {
		t.Fatalf("stderr = %q, want the top-level and file summaries as context lines", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if !strings.Contains(list, "new:3-4") {
		t.Fatalf("a multi-line newRange must anchor the whole span:\n%s", list)
	}
}

func TestNoteApplyCommentShapeWithHunkAndReply(t *testing.T) {
	dir := noteRepo(t)
	_, out, _ := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "root")
	root := strings.TrimSpace(out)
	in := `{"comments":[{"filePath":"a.txt","hunk":1,"summary":"whole hunk"},
	                    {"replyTo":"` + root + `","summary":"addressed"}]}`
	code, out, errb := runCLIStdin(t, dir, in, "note", "apply", "--stdin", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	var wires []struct {
		ParentID string `json:"parent_id"`
		Summary  string `json:"summary"`
		Source   string `json:"source"`
	}
	if err := json.Unmarshal([]byte(out), &wires); err != nil {
		t.Fatalf("--json must be an array of wire notes: %v\n%s", err, out)
	}
	if len(wires) != 2 {
		t.Fatalf("wires = %+v, want 2", wires)
	}
	for _, w := range wires {
		if w.Source != "agent" {
			t.Errorf("every batch note is source agent: %+v", w)
		}
	}
	if wires[1].ParentID != root {
		t.Errorf("replyTo must produce a reply on the named root: %+v", wires[1])
	}
}

func TestNoteApplyAuthorFallbackAndOverride(t *testing.T) {
	dir := noteRepo(t)
	in := `{"files":[{"path":"a.txt","annotations":[
	  {"newRange":[1,1],"summary":"item author","author":"sonnet"},
	  {"newRange":[2,2],"summary":"flag author"}]}]}`
	code, out, errb := runCLIStdin(t, dir, in, "note", "apply", "--stdin", "--author", "reviewer", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, `"author":"sonnet"`) || !strings.Contains(out, `"author":"reviewer"`) {
		t.Fatalf("an item author wins; --author fills the rest: %s", out)
	}
}

// A batch that does not fully validate must store NOTHING.
func TestNoteApplyRejectsWholeBatchOnOneBadItem(t *testing.T) {
	dir := noteRepo(t)
	in := `{"files":[{"path":"a.txt","annotations":[
	  {"newRange":[1,1],"summary":"good"},
	  {"newRange":[2,2]}]}]}`
	code, _, errb := runCLIStdin(t, dir, in, "note", "apply", "--stdin")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "annotations[1]") {
		t.Fatalf("stderr = %q, want the offending item named", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if strings.TrimSpace(list) != "" {
		t.Fatalf("a rejected batch must store nothing, got:\n%s", list)
	}
}

// An unknown replyTo is caught in the VALIDATION pass, before any write.
func TestNoteApplyUnknownReplyToStoresNothing(t *testing.T) {
	dir := noteRepo(t)
	in := `{"comments":[{"filePath":"a.txt","newLine":1,"summary":"good"},
	                    {"replyTo":"ffffffff","summary":"orphan"}]}`
	code, _, errb := runCLIStdin(t, dir, in, "note", "apply", "--stdin")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "ffffffff") {
		t.Fatalf("stderr = %q, want the unknown parent id named", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if strings.TrimSpace(list) != "" {
		t.Fatalf("nothing may be stored: %s", list)
	}
}

func TestNoteApplyRequiresStdinFlag(t *testing.T) {
	dir := noteRepo(t)
	code, _, errb := runCLIStdin(t, dir, `{"files":[]}`, "note", "apply")
	if code != 2 || !strings.Contains(errb, "--stdin") {
		t.Fatalf("exit=%d stderr=%q, want 2 + a --stdin hint", code, errb)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && go test ./internal/cli -run TestNoteApply -count=1`
Expected: FAIL — `note: unknown subcommand "apply"`, exit 2.

- [ ] **Step 3: Implement the importer**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/noteapply.go`:

```go
package cli

// `gg note apply --stdin` — the batch import lane, and the shared planner the
// `gg review --notes` importer and the MCP gg_notes_apply tool reuse.
//
// The contract is ALL-OR-NOTHING: every item is parsed, its address resolved
// and its anchor computed BEFORE the first write. A batch with one bad item
// exits 1 having stored nothing.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// noteSideRule says which diff sides the import target can address. A single
// commit has both (parent → commit); a range review or a working review has an
// old side that is not a note-addressable base (§4.4's old-side table), so
// old-side items are skipped with one warning.
type noteSideRule int

const (
	sideRuleBoth noteSideRule = iota
	sideRuleNewOnly
)

// plannedNote is one validated, fully anchored note waiting to be written.
type plannedNote struct {
	Note    model.Note
	ReplyTo string
}

// planNoteBatch resolves every item to a storable note. cached/rev choose the
// target diff for the WHOLE batch; author fills in for items that carry none.
// Every batch note is Source: agent.
//
// skipped counts old-side items dropped by sideRuleNewOnly — the caller warns
// once rather than per item.
func planNoteBatch(ctx context.Context, svc *domain.Service, b notebatch.Batch, cached bool, rev, author string, sideRule noteSideRule) (planned []plannedNote, skipped int, err error) {
	addrCache := map[string]model.FileAddress{}
	for i, it := range b.Items {
		who := it.Author
		if who == "" {
			who = author
		}
		n := model.Note{
			Source: model.NoteSourceAgent, Author: who,
			Summary: it.Summary, Rationale: it.Rationale,
			Tags: it.Tags, Confidence: it.Confidence,
		}
		if it.ReplyTo != "" {
			// Validate the parent NOW: an unknown id must reject the batch
			// before anything is stored.
			if _, gerr := svc.NoteGet(ctx, it.ReplyTo); gerr != nil {
				return nil, 0, fmt.Errorf("item %d: replyTo %s: %w", i, it.ReplyTo, gerr)
			}
			planned = append(planned, plannedNote{Note: n, ReplyTo: it.ReplyTo})
			continue
		}
		addr, ok := addrCache[it.Path]
		if !ok {
			addr, err = svc.NoteTarget(ctx, it.Path, cached, rev)
			if err != nil {
				return nil, 0, fmt.Errorf("item %d: %w", i, err)
			}
			addrCache[it.Path] = addr
		}
		side, rng, aerr := planAnchor(ctx, svc, addr, cached, rev, it.Target)
		if aerr != nil {
			return nil, 0, fmt.Errorf("item %d: %w", i, aerr)
		}
		if sideRule == sideRuleNewOnly && side == model.NoteSideOld {
			skipped++
			continue
		}
		n.Address, n.Side, n.Range = addr, side, rng
		planned = append(planned, plannedNote{Note: n})
	}
	return planned, skipped, nil
}

// planAnchor turns one parsed target into a side and range: an explicit range
// is used as-is, a hunk number resolves through the same patch
// `gg diff --hunks` numbers for this target.
func planAnchor(ctx context.Context, svc *domain.Service, addr model.FileAddress, cached bool, rev string, t notebatch.Target) (model.NoteSide, [2]int, error) {
	switch {
	case t.NewLine != [2]int{0, 0}:
		return model.NoteSideNew, t.NewLine, nil
	case t.OldLine != [2]int{0, 0}:
		return model.NoteSideOld, t.OldLine, nil
	case t.Hunk != 0:
		return svc.HunkRange(ctx, domain.HunkDiffSpec(cached, rev, []string{addr.Path}), addr.Path, t.Hunk)
	}
	return "", [2]int{}, fmt.Errorf("no anchor")
}

// applyNoteBatch writes a planned batch in order and returns the stored notes.
func applyNoteBatch(ctx context.Context, svc *domain.Service, planned []plannedNote) ([]model.Note, error) {
	out := make([]model.Note, 0, len(planned))
	for i, p := range planned {
		var (
			stored model.Note
			err    error
		)
		if p.ReplyTo != "" {
			stored, err = svc.NoteReply(ctx, p.ReplyTo, p.Note)
		} else {
			stored, err = svc.NoteAdd(ctx, p.Note)
		}
		if err != nil {
			return out, fmt.Errorf("item %d: %w", i, err)
		}
		out = append(out, stored)
	}
	return out, nil
}

// printStoredNotes emits the ids (one per line) or the wire notes as JSON.
func printStoredNotes(w io.Writer, notes []model.Note, asJSON bool) error {
	if !asJSON {
		for _, n := range notes {
			if _, err := fmt.Fprintln(w, n.ID); err != nil {
				return err
			}
		}
		return nil
	}
	wires := make([]domain.WireNote, 0, len(notes))
	for _, n := range notes {
		wires = append(wires, domain.ToWireNote(domain.ResolvedNote{Note: n, Status: model.NoteActive, Range: n.Range}))
	}
	return json.NewEncoder(w).Encode(wires)
}

func noteApply(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	useStdin := fs.Bool("stdin", false, "read the JSON batch from stdin (required)")
	author := fs.String("author", "", "author for items that carry none (default: $GG_AGENT, else agent)")
	asJSON := fs.Bool("json", false, "print the stored notes as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*useStdin {
		fmt.Fprintln(stderr, "usage: gg note apply --stdin [--cached | --rev <commit>] [--author <name>] [--json]")
		return 2
	}
	if strings.TrimSpace(*tf.file) != "" {
		fmt.Fprintln(stderr, "note apply: --file is not used; each batch item names its own path")
		return 2
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintln(stderr, "error: reading stdin:", err)
		return 1
	}
	batch, err := notebatch.Parse(data)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	// Unanchored prose has nowhere to live in gg's model: echo it so the human
	// still sees it, and store only what is anchored.
	for _, c := range batch.Contexts {
		fmt.Fprintln(stderr, "context:", c)
	}
	ctx := context.Background()
	planned, _, err := planNoteBatch(ctx, svc, batch, *tf.cached, *tf.rev, noteAuthorDefault(*author), sideRuleBoth)
	if err != nil {
		return noteExit(err, stderr)
	}
	stored, err := applyNoteBatch(ctx, svc, planned)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := printStoredNotes(stdout, stored, *asJSON); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
```

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/note.go`, add the dispatcher case before `default`:

```go
		case "apply":
			return noteApply(svc, rest, stdin, stdout, stderr)
```

and change `cmdNote`'s closure so it captures `stdin` (the parameter is already there; the closure body simply uses it).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && gofmt -l internal/cli && go test ./internal/cli -run TestNote -count=1`
Expected: `gofmt -l` prints nothing; PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
git add internal/cli/noteapply.go internal/cli/noteapply_test.go internal/cli/note.go && \
git commit -m "feat(cli): gg note apply --stdin imports both agent JSON batch shapes" \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" \
  -m "Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 8: `gg review --notes`

**Files:**
- Modify: `internal/engine/review_changes.go` (`NotesFile` field, env, context paragraph)
- Create: `internal/engine/review_notes_test.go`
- Modify: `internal/domain/review.go` (`ReviewReportNotes`)
- Modify: `internal/cli/review.go` (`--notes` flag and the import)
- Modify: `internal/cli/review_test.go`

**Interfaces:**
- Consumes: `engine.ReviewChanges{Command, Dir, Env, Diff, RangeLabel}`, `(*domain.Service).ReviewReport(ctx, ReviewTarget, string, []string, time.Time) (ReviewResult, error)`, `domain.ReviewTarget{Kind, Range, Label, Diff}`, `domain.ReviewWorking`, `(*domain.Service).RevParse`, `(*domain.Service).NoteTarget`, `notebatch.Parse`, `planNoteBatch`/`applyNoteBatch`/`sideRuleBoth`/`sideRuleNewOnly` (Task 7), `noteAuthorDefault` (Task 5).
- Produces:
  ```go
  // engine
  type ReviewChanges struct { …; NotesFile string }   // new field, zero value = today's behaviour
  // domain
  func (s *Service) ReviewReportNotes(ctx context.Context, target ReviewTarget, resolvedCommand string, env []string, now time.Time, notesFile string) (ReviewResult, error)
  // cli
  func reviewImportTarget(ctx context.Context, svc *domain.Service, target domain.ReviewTarget, arg string) (cached bool, rev string, rule noteSideRule, err error)
  ```

- [ ] **Step 1: Write the failing engine test**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/engine/review_notes_test.go`:

```go
package engine

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// With NotesFile set the tool sees $GG_NOTES_FILE and the context doc gains the
// "## Inline notes (optional)" paragraph naming that path.
func TestReviewChangesNotesFileEnvAndContext(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/cp")
	}
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	stageAndCommit(t, dir, repo, "a.txt", "one\ntwo\n", "c2")

	notesPath := dir + "/notes.json"
	cmd := `cp "$GG_CONTEXT_FILE" "` + dir + `/seen.ctx"; printf '%s' "$GG_NOTES_FILE" > "` + dir + `/seen.env"; printf x > "$GG_MESSAGE_FILE"`
	_, err := ReviewChanges{
		Command: cmd, Dir: dir, Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"},
		RangeLabel: "HEAD~1..HEAD", NotesFile: notesPath,
	}.Run(context.Background(), OpDeps{Repo: repo, CaptureRunner: ShellCaptureRunner{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.TrimSpace(readFile(t, dir+"/seen.env")); got != notesPath {
		t.Fatalf("GG_NOTES_FILE = %q, want %q", got, notesPath)
	}
	ctxSeen := readFile(t, dir+"/seen.ctx")
	for _, want := range []string{
		"## Inline notes (optional)",
		"hunk agent-context JSON (version 1)",
		notesPath,
		`{"version":1,"files":[{"path":"…","annotations":`,
		"1-based in the NEW version of each file",
		"do not annotate every hunk",
	} {
		if !strings.Contains(ctxSeen, want) {
			t.Errorf("context doc missing %q:\n%s", want, ctxSeen)
		}
	}
}

// Without NotesFile nothing changes: no env var, no extra paragraph.
func TestReviewChangesWithoutNotesFileIsUnchanged(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/cp")
	}
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	cmd := `cp "$GG_CONTEXT_FILE" "` + dir + `/seen.ctx"; printf '%s' "${GG_NOTES_FILE:-unset}" > "` + dir + `/seen.env"; printf x > "$GG_MESSAGE_FILE"`
	if _, err := (ReviewChanges{Command: cmd, Dir: dir, Diff: model.DiffSpec{Rev: "HEAD"}, RangeLabel: "HEAD"}).
		Run(context.Background(), OpDeps{Repo: repo, CaptureRunner: ShellCaptureRunner{}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.TrimSpace(readFile(t, dir+"/seen.env")); got != "unset" {
		t.Fatalf("GG_NOTES_FILE must not be set without NotesFile, got %q", got)
	}
	if strings.Contains(readFile(t, dir+"/seen.ctx"), "Inline notes") {
		t.Fatal("the context doc must not mention notes when NotesFile is empty")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && go test ./internal/engine -run TestReviewChangesNotes -count=1`
Expected: FAIL — `unknown field NotesFile in struct literal`.

- [ ] **Step 3: Add `NotesFile` to the engine op**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/engine/review_changes.go`:

Add to the struct (after `RangeLabel`):

```go
	NotesFile  string         // when set: $GG_NOTES_FILE, and the context doc asks for agent-context v1
```

In `Run`, extend the env block:

```go
	env = append(env,
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_REVIEW_DIFF="+diffPath,
		"GG_MESSAGE_FILE="+msgPath,
		"GG_REPO="+op.Dir,
	)
	if op.NotesFile != "" {
		// The CALLER owns this file: ReviewChanges removes only the temp files
		// it created, and the caller must still be able to read the notes after
		// the op returns.
		env = append(env, "GG_NOTES_FILE="+op.NotesFile)
	}
```

At the end of `reviewSummary`, before `return b.String()`:

```go
	if op.NotesFile != "" {
		b.WriteString(notesInstruction(op.NotesFile))
	}
```

Append to the same file:

```go
// notesInstruction is the paragraph appended to $GG_CONTEXT_FILE when the
// caller asked for anchored notes (spec §4.5). The wording is fixed: agents
// trained on hunk's sidecar already emit exactly this shape, and the default
// [[tools.command]] prompt templates are deliberately NOT changed.
func notesInstruction(notesFile string) string {
	return "\n## Inline notes (optional)\n" +
		"Also write anchored notes as hunk agent-context JSON (version 1) to the\n" +
		"file at " + notesFile + ": {\"version\":1,\"files\":[{\"path\":\"…\",\"annotations\":\n" +
		"[{\"newRange\":[a,b],\"summary\":\"…\",\"rationale\":\"…\"}]}]}. Line numbers are\n" +
		"1-based in the NEW version of each file. Comment on what the reader would\n" +
		"not spot; do not annotate every hunk.\n"
}
```

- [ ] **Step 4: Thread it through domain**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/domain/review.go`, rename the body of `ReviewReport` into a new method and keep the old signature as a wrapper (the TUI at `internal/tui/review.go:295` and the web at `internal/web/review.go:128` call `ReviewReport` and must not change):

```go
// ReviewReport runs resolvedCommand over target and persists the captured
// report. The three-frontend entry point; ReviewReportNotes adds the optional
// notes sidecar.
func (s *Service) ReviewReport(ctx context.Context, target ReviewTarget, resolvedCommand string, env []string, now time.Time) (ReviewResult, error) {
	return s.ReviewReportNotes(ctx, target, resolvedCommand, env, now, "")
}

// ReviewReportNotes is ReviewReport plus a caller-owned notes file: when
// notesFile is non-empty the tool is told (via $GG_NOTES_FILE and one context
// paragraph) that it may also write anchored notes as agent-context v1. The
// FILE belongs to the caller — the op never creates or removes it — because the
// caller reads it after the op returns.
func (s *Service) ReviewReportNotes(ctx context.Context, target ReviewTarget, resolvedCommand string, env []string, now time.Time, notesFile string) (ReviewResult, error) {
```

…then paste the existing body of `ReviewReport` under the new signature, with one change to the op literal:

```go
	op := engine.ReviewChanges{
		Command:    resolvedCommand,
		Dir:        s.workdir,
		Env:        env,
		Diff:       target.Diff,
		RangeLabel: label,
		NotesFile:  notesFile,
	}
```

- [ ] **Step 5: Write the failing CLI test**

Append to `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/review_test.go`:

```go
// A tool that writes $GG_NOTES_FILE has its notes imported and the ids listed
// on stderr; the report itself still prints and is still persisted.
func TestReviewNotesImportsSidecarFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeReviewTool(t, dir, "Echo",
		`printf 'THE REPORT\n'; printf '{"version":1,"files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"shouty"}]}]}' > "$GG_NOTES_FILE"`)

	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working", "--notes")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "THE REPORT") {
		t.Fatalf("the freeform report must still print: %q", out)
	}
	if !strings.Contains(errb, "notes:") {
		t.Fatalf("stderr must list the imported ids: %q", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if !strings.Contains(list, "shouty") || !strings.Contains(list, "new:2-2") {
		t.Fatalf("the note must be stored against the working tree:\n%s", list)
	}
}

// When the notes file stays empty but the REPORT itself is agent-context v1,
// that is imported instead (the report body is still the JSON).
func TestReviewNotesFallsBackToJSONReport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeReviewTool(t, dir, "Echo",
		`printf '{"version":1,"files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"from the report"}]}]}\n'`)

	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working", "--notes")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if !strings.Contains(list, "from the report") {
		t.Fatalf("a JSON report must be imported when the notes file is empty:\n%s", list)
	}
}

// Neither channel carried notes: exit 1, naming the contract.
func TestReviewNotesNoNotesIsExit1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Echo", `printf 'just prose, no JSON\n'`)
	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working", "--notes")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "review tool wrote no notes") || !strings.Contains(errb, "GG_NOTES_FILE") {
		t.Fatalf("stderr = %q, want the documented message", errb)
	}
}

// A RANGE review's base is not a note-addressable side: old-side annotations
// are skipped with one warning, and the notes land on the tip commit.
func TestReviewNotesRangeAnchorsTipNewSideOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "shout")
	sha := runGit(t, dir, "rev-parse", "HEAD")
	writeReviewTool(t, dir, "Echo",
		`printf 'R\n'; printf '{"version":1,"files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"kept"},{"oldRange":[2,2],"summary":"dropped"}]}]}' > "$GG_NOTES_FILE"`)

	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--notes", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(errb, "old-side") {
		t.Fatalf("stderr = %q, want one old-side warning", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--rev", sha, "--file", "a.txt")
	if !strings.Contains(list, "kept") || strings.Contains(list, "dropped") {
		t.Fatalf("only the new-side note may land on the tip commit:\n%s", list)
	}
}
```

Add `"os"`, `"path/filepath"` and `"runtime"` to that file's import block if missing.

- [ ] **Step 6: Run it to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && go test ./internal/cli -run TestReviewNotes -count=1`
Expected: FAIL — `flag provided but not defined: -notes` (exit 2).

- [ ] **Step 7: Implement `--notes` in the CLI**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/review.go`, add the flag next to `--working`:

```go
	wantNotes := fs.Bool("notes", false, "also ask the tool for anchored notes (agent-context v1) and import them")
```

Replace the `svc.ReviewReport(...)` call and everything after it with:

```go
	notesPath := ""
	if *wantNotes {
		// The CLI owns this file: the op only names it in the environment, and
		// we must still be able to read it once the op returns.
		f, terr := os.CreateTemp("", "gg-review-notes-*.json")
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		notesPath = f.Name()
		f.Close()
		defer os.Remove(notesPath)
		// A note write must respect the configured entry cap even though this
		// process is not a `gg note` verb.
		if cfg, cerr := loadConfigFor(svc); cerr == nil {
			svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
		}
	}

	res, err := svc.ReviewReportNotes(ctx, target, resolved, []string{"GG_TASK=review"}, time.Now(), notesPath)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	io.WriteString(stdout, res.Content)
	if !strings.HasSuffix(res.Content, "\n") {
		io.WriteString(stdout, "\n")
	}
	fmt.Fprintln(stderr, "report:", res.Path)
	if !*wantNotes {
		return 0
	}
	return importReviewNotes(ctx, svc, target, arg, notesPath, res.Content, cmd.Name, stderr)
```

where `arg` is the positional the target came from — capture it while resolving the target by replacing that switch with:

```go
	arg := ""
	var target domain.ReviewTarget
	switch {
	case *working:
		target = domain.WorkingReviewTarget()
	case fs.NArg() >= 1:
		arg = fs.Arg(0)
		target = reviewTargetForArg(arg)
	default:
		t, err := svc.BranchReviewTarget(ctx, "HEAD")
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		target = t
	}
```

Append to the same file:

```go
// reviewImportTarget decides which diff a review's notes anchor to (§4.5):
//
//	gg review <sha>        → that commit; BOTH sides are addressable
//	gg review A..B / branch→ the TIP commit; NEW side only
//	gg review --working    → the unstaged working tree; NEW side only
//
// The two new-side-only cases exist because a review's base (the merge base, or
// HEAD for --working) is not one of §4.4's note-addressable old sides.
func reviewImportTarget(ctx context.Context, svc *domain.Service, target domain.ReviewTarget, arg string) (cached bool, rev string, rule noteSideRule, err error) {
	if target.Kind == domain.ReviewWorking {
		return false, "", sideRuleNewOnly, nil
	}
	if arg != "" && !strings.Contains(arg, "..") {
		return false, arg, sideRuleBoth, nil // a single commit: parent → commit
	}
	// A range: anchor to its TIP. "A..B" and "A...B" both end at the last
	// non-empty segment.
	parts := strings.Split(target.Range, "..")
	tip := ""
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			tip = s
		}
	}
	if tip == "" {
		return false, "", sideRuleNewOnly, fmt.Errorf("cannot find the tip commit of %q", target.Range)
	}
	full, rerr := svc.RevParse(ctx, tip)
	if rerr != nil {
		return false, "", sideRuleNewOnly, fmt.Errorf("unknown revision %q: %w", tip, rerr)
	}
	return false, strings.TrimSpace(full), sideRuleNewOnly, nil
}

// importReviewNotes reads the tool's notes: the sidecar file when it is
// non-empty, else the captured report when THAT parses as agent-context v1
// (some tools have only one output channel). Neither → exit 1.
func importReviewNotes(ctx context.Context, svc *domain.Service, target domain.ReviewTarget, arg, notesPath, report, toolName string, stderr io.Writer) int {
	data, _ := os.ReadFile(notesPath)
	if len(strings.TrimSpace(string(data))) == 0 {
		if s := strings.TrimSpace(report); strings.HasPrefix(s, "{") {
			data = []byte(s)
		}
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		fmt.Fprintln(stderr, "error: review tool wrote no notes (expected agent-context v1 at $GG_NOTES_FILE)")
		return 1
	}
	batch, err := notebatch.Parse(data)
	if err != nil {
		fmt.Fprintln(stderr, "error: review tool wrote no notes (expected agent-context v1 at $GG_NOTES_FILE):", err)
		return 1
	}
	for _, c := range batch.Contexts {
		fmt.Fprintln(stderr, "context:", c)
	}
	cached, rev, rule, err := reviewImportTarget(ctx, svc, target, arg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	planned, skipped, err := planNoteBatch(ctx, svc, batch, cached, rev, noteAuthorDefault(toolName), rule)
	if err != nil {
		return noteExit(err, stderr)
	}
	if skipped > 0 {
		fmt.Fprintf(stderr, "note: skipped %d old-side annotation(s) — this review's base is not a note-addressable side\n", skipped)
	}
	stored, err := applyNoteBatch(ctx, svc, planned)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	ids := make([]string, 0, len(stored))
	for _, n := range stored {
		ids = append(ids, n.ID)
	}
	fmt.Fprintln(stderr, "notes:", strings.Join(ids, " "))
	return 0
}
```

Add `"os"` and `"github.com/homeend/gigagit/internal/notebatch"` to `internal/cli/review.go`'s import block.

- [ ] **Step 8: Run the tests to verify they pass**

Run:
```
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
gofmt -l internal/engine internal/domain internal/cli && \
go test ./internal/engine -run TestReviewChanges -count=1 && \
go test ./internal/cli -run 'TestReview|TestNote' -count=1 && \
go test ./internal/domain ./internal/tui ./internal/web -run Review -count=1
```
Expected: `gofmt -l` prints nothing; all PASS (the TUI and web review paths still compile against the unchanged `ReviewReport`).

- [ ] **Step 9: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
git add internal/engine/review_changes.go internal/engine/review_notes_test.go \
        internal/domain/review.go internal/cli/review.go internal/cli/review_test.go && \
git commit -m "feat(review): gg review --notes imports agent-context v1 as anchored notes" \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" \
  -m "Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 9: MCP note tools

**Files:**
- Create: `internal/mcp/notes.go`
- Create: `internal/mcp/notes_test.go`
- Modify: `internal/mcp/server.go` (`New` applies the policy + starts the sweep; `sdkServer` registers the tools)

**Interfaces:**
- Consumes: `sdk.AddTool`, `readOnlyAnnotations()`/`mutatingAnnotations()` (`internal/mcp/types.go:18,24`), `(*Server).repoCheck()`/`repoInfo()`, `(*domain.Service).NoteTarget/NotesAt/NoteAddresses/NoteAdd/NoteRemove/NoteGet/HunkRange/EffectiveConfig/SetNotesPolicy/StartNotesSweep`, `domain.HunkDiffSpec`, `domain.ToWireNote`, `domain.WireNote`, `notebatch.Parse`.
- Produces: the four tools `gg_notes_list`, `gg_note_add`, `gg_notes_apply`, `gg_note_rm`, plus `func (s *Server) registerNoteTools(srv *sdk.Server)`.

- [ ] **Step 1: Write the failing test**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/mcp/notes_test.go`:

```go
package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedNoteFile makes a.txt differ from HEAD so the working tree has a diff to
// anchor notes against.
func seedNoteFile(t *testing.T, e *testEnv) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nWORLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNoteAddAndListTools(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e)
	out := e.call(t, "gg_note_add", map[string]any{
		"file": "a.txt", "new_line": 2, "summary": "shouty", "rationale": "why", "author": "sonnet",
	})
	note, ok := out["note"].(map[string]any)
	if !ok || note["summary"] != "shouty" || note["author"] != "sonnet" {
		t.Fatalf("gg_note_add reply = %v", out)
	}
	id, _ := note["id"].(string)
	if len(id) != 8 {
		t.Fatalf("note id = %q, want 8 hex", id)
	}

	list := e.call(t, "gg_notes_list", map[string]any{"file": "a.txt"})
	raw, err := json.Marshal(list["notes"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"summary":"shouty"`) || !strings.Contains(string(raw), `"status":`) {
		t.Fatalf("gg_notes_list must return resolved wire notes: %s", raw)
	}

	e.call(t, "gg_note_rm", map[string]any{"id": id})
	list = e.call(t, "gg_notes_list", map[string]any{"file": "a.txt"})
	raw, _ = json.Marshal(list["notes"])
	if strings.Contains(string(raw), "shouty") {
		t.Fatalf("gg_note_rm must remove the note: %s", raw)
	}
}

func TestNotesApplyToolIsAllOrNothing(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e)
	msg := e.callErr(t, "gg_notes_apply", map[string]any{
		"batch": json.RawMessage(`{"files":[{"path":"a.txt","annotations":[{"newRange":[1,1],"summary":"ok"},{"newRange":[2,2]}]}]}`),
	})
	if !strings.Contains(msg, "annotations[1]") {
		t.Fatalf("a bad item must name itself: %s", msg)
	}
	list := e.call(t, "gg_notes_list", map[string]any{"file": "a.txt"})
	raw, _ := json.Marshal(list["notes"])
	if strings.Contains(string(raw), `"ok"`) {
		t.Fatalf("a rejected batch must store nothing: %s", raw)
	}

	out := e.call(t, "gg_notes_apply", map[string]any{
		"batch":  json.RawMessage(`{"comments":[{"filePath":"a.txt","newLine":2,"summary":"batched"}]}`),
		"author": "reviewer",
	})
	raw, _ = json.Marshal(out["notes"])
	if !strings.Contains(string(raw), `"batched"`) || !strings.Contains(string(raw), `"author":"reviewer"`) {
		t.Fatalf("apply reply = %s", raw)
	}
}

func TestNoteAddToolRejectsRangeRev(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e)
	msg := e.callErr(t, "gg_note_add", map[string]any{
		"file": "a.txt", "new_line": 1, "rev": "main..HEAD", "summary": "s",
	})
	if !strings.Contains(msg, "one commit") {
		t.Fatalf("a range rev must be refused with the documented hint: %s", msg)
	}
}

func TestNoteToolAnnotations(t *testing.T) {
	e := newTestEnv(t)
	tools := e.listTools(t)
	for name, wantReadOnly := range map[string]bool{
		"gg_notes_list": true, "gg_note_add": false, "gg_notes_apply": false, "gg_note_rm": false,
	} {
		ann, ok := tools[name]
		if !ok {
			t.Errorf("tool %q is not registered", name)
			continue
		}
		if ann.ReadOnlyHint != wantReadOnly {
			t.Errorf("%s ReadOnlyHint = %v, want %v", name, ann.ReadOnlyHint, wantReadOnly)
		}
	}
}
```

If `testEnv` has no `listTools` helper, add one beside `call`/`callErr` in `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/mcp/server_test.go`:

```go
// listTools returns each registered tool's annotations by name.
func (e *testEnv) listTools(t *testing.T) map[string]sdk.ToolAnnotations {
	t.Helper()
	res, err := e.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	out := map[string]sdk.ToolAnnotations{}
	for _, tool := range res.Tools {
		if tool.Annotations != nil {
			out[tool.Name] = *tool.Annotations
		}
	}
	return out
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && go test ./internal/mcp -run 'TestNote' -count=1`
Expected: FAIL — tool `gg_note_add` not found.

- [ ] **Step 3: Write the tools**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/mcp/notes.go`:

```go
package mcp

// Review notes over MCP: the same surface `gg note …` exposes on the CLI, so an
// agent driving gg through either door leaves identical records. Reads are
// readOnly; the three mutators carry mutatingAnnotations() and are gated by the
// client's consent prompt like gg_write_to_worktree.

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// noteTargetIn is the shared target trio: none = the unstaged working tree
// (untracked when git says so), cached = HEAD → index, rev = one commit.
type noteTargetIn struct {
	File   string `json:"file,omitempty"`
	Cached bool   `json:"cached,omitempty"`
	Rev    string `json:"rev,omitempty"`
}

type notesListIn struct {
	noteTargetIn
	Type string `json:"type,omitempty"` // user | agent | all (default all)
}

type notesOut struct {
	Repo  RepoInfo           `json:"repo"`
	Notes []domain.WireNote  `json:"notes"`
}

type noteAddIn struct {
	noteTargetIn
	Hunk      int    `json:"hunk,omitempty"`
	NewLine   int    `json:"new_line,omitempty"`
	OldLine   int    `json:"old_line,omitempty"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale,omitempty"`
	Author    string `json:"author,omitempty"`
}

type noteOut struct {
	Repo RepoInfo        `json:"repo"`
	Note domain.WireNote `json:"note"`
}

type notesApplyIn struct {
	noteTargetIn
	Batch  json.RawMessage `json:"batch"`
	Author string          `json:"author,omitempty"`
}

type noteRmIn struct {
	ID string `json:"id"`
}

type okOut struct {
	Repo RepoInfo `json:"repo"`
	OK   bool     `json:"ok"`
}

func (s *Server) registerNoteTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_notes_list",
		Description: "List gg review notes, resolved against the current content. Omit file to list " +
			"every note this checkout can see. Target: none = the unstaged working tree, cached=true " +
			"= the staged diff, rev = one commit's own change. type filters user/agent/all.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in notesListIn) (*sdk.CallToolResult, notesOut, error) {
		out := notesOut{Repo: s.repoInfo(), Notes: []domain.WireNote{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		res, err := s.notesFor(ctx, in.noteTargetIn)
		if err != nil {
			return nil, out, err
		}
		for _, r := range res {
			if in.Type != "" && in.Type != "all" && string(r.Note.Source) != in.Type {
				continue
			}
			out.Notes = append(out.Notes, domain.ToWireNote(r))
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_note_add",
		Description: "Leave one anchored review note. Pass file plus exactly one of hunk (see " +
			"gg diff --hunks), new_line or old_line (1-based). Target: none = the unstaged working " +
			"tree, cached=true = the staged diff, rev = one commit (a range is refused). MUTATES gg's note store.",
		Annotations: mutatingAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in noteAddIn) (*sdk.CallToolResult, noteOut, error) {
		out := noteOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.Summary == "" {
			return nil, out, fmt.Errorf("summary is required")
		}
		addr, err := s.svc.NoteTarget(ctx, in.File, in.Cached, in.Rev)
		if err != nil {
			return nil, out, err
		}
		side, rng, err := s.noteAnchor(ctx, addr, in)
		if err != nil {
			return nil, out, err
		}
		author := in.Author
		if author == "" {
			author = "agent"
		}
		stored, err := s.svc.NoteAdd(ctx, model.Note{
			Source: model.NoteSourceAgent, Author: author, Address: addr,
			Side: side, Range: rng, Summary: in.Summary, Rationale: in.Rationale,
		})
		if err != nil {
			return nil, out, err
		}
		out.Note = domain.ToWireNote(domain.ResolvedNote{Note: stored, Status: model.NoteActive, Range: stored.Range})
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_notes_apply",
		Description: "Import a batch of anchored notes in one call. batch is either hunk's " +
			`agent-context v1 ({"version":1,"files":[{"path":…,"annotations":[{"newRange":[a,b],"summary":…}]}]}) ` +
			`or a comment batch ({"comments":[{"filePath":…,"newLine":N,"summary":…}]}). ` +
			"The whole batch is validated first: one bad item stores nothing. MUTATES gg's note store.",
		Annotations: mutatingAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in notesApplyIn) (*sdk.CallToolResult, notesOut, error) {
		out := notesOut{Repo: s.repoInfo(), Notes: []domain.WireNote{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		batch, err := notebatch.Parse(in.Batch)
		if err != nil {
			return nil, out, err
		}
		author := in.Author
		if author == "" {
			author = "agent"
		}
		stored, err := s.applyBatch(ctx, batch, in.noteTargetIn, author)
		if err != nil {
			return nil, out, err
		}
		for _, n := range stored {
			out.Notes = append(out.Notes, domain.ToWireNote(domain.ResolvedNote{Note: n, Status: model.NoteActive, Range: n.Range}))
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_note_rm",
		Description: "Remove one gg review note by id; a thread root takes its replies with it. MUTATES gg's note store.",
		Annotations: mutatingAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in noteRmIn) (*sdk.CallToolResult, okOut, error) {
		out := okOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.ID == "" {
			return nil, out, fmt.Errorf("id is required")
		}
		if err := s.svc.NoteRemove(ctx, in.ID); err != nil {
			return nil, out, err
		}
		out.OK = true
		return nil, out, nil
	})
}

// notesFor resolves the notes a list request covers: one address when file is
// given, else every address this checkout can see.
func (s *Server) notesFor(ctx context.Context, t noteTargetIn) ([]domain.ResolvedNote, error) {
	if t.File != "" {
		addr, err := s.svc.NoteTarget(ctx, t.File, t.Cached, t.Rev)
		if err != nil {
			return nil, err
		}
		return s.svc.NotesAt(ctx, addr)
	}
	addrs, err := s.svc.NoteAddresses(ctx)
	if err != nil {
		return nil, err
	}
	var out []domain.ResolvedNote
	for _, a := range addrs {
		got, gerr := s.svc.NotesAt(ctx, a)
		if gerr != nil {
			return nil, gerr
		}
		out = append(out, got...)
	}
	return out, nil
}

// noteAnchor turns the three mutually exclusive anchor inputs into a side and
// range, resolving a hunk number over the same patch gg diff --hunks numbers.
func (s *Server) noteAnchor(ctx context.Context, addr model.FileAddress, in noteAddIn) (model.NoteSide, [2]int, error) {
	set := 0
	for _, v := range []int{in.Hunk, in.NewLine, in.OldLine} {
		if v != 0 {
			set++
		}
	}
	if set != 1 {
		return "", [2]int{}, fmt.Errorf("pass exactly one of hunk, new_line or old_line")
	}
	switch {
	case in.NewLine != 0:
		return model.NoteSideNew, [2]int{in.NewLine, in.NewLine}, nil
	case in.OldLine != 0:
		return model.NoteSideOld, [2]int{in.OldLine, in.OldLine}, nil
	default:
		return s.svc.HunkRange(ctx, domain.HunkDiffSpec(in.Cached, in.Rev, []string{addr.Path}), addr.Path, in.Hunk)
	}
}

// applyBatch validates EVERY item (address, anchor, reply parent) before the
// first write, then stores them in order — the CLI's all-or-nothing contract.
func (s *Server) applyBatch(ctx context.Context, b notebatch.Batch, t noteTargetIn, author string) ([]model.Note, error) {
	type plan struct {
		note    model.Note
		replyTo string
	}
	addrs := map[string]model.FileAddress{}
	planned := make([]plan, 0, len(b.Items))
	for i, it := range b.Items {
		who := it.Author
		if who == "" {
			who = author
		}
		n := model.Note{
			Source: model.NoteSourceAgent, Author: who,
			Summary: it.Summary, Rationale: it.Rationale, Tags: it.Tags, Confidence: it.Confidence,
		}
		if it.ReplyTo != "" {
			if _, err := s.svc.NoteGet(ctx, it.ReplyTo); err != nil {
				return nil, fmt.Errorf("item %d: replyTo %s: %v", i, it.ReplyTo, err)
			}
			planned = append(planned, plan{note: n, replyTo: it.ReplyTo})
			continue
		}
		addr, ok := addrs[it.Path]
		if !ok {
			var err error
			addr, err = s.svc.NoteTarget(ctx, it.Path, t.Cached, t.Rev)
			if err != nil {
				return nil, fmt.Errorf("item %d: %v", i, err)
			}
			addrs[it.Path] = addr
		}
		side, rng, err := s.noteAnchor(ctx, addr, noteAddIn{
			noteTargetIn: t,
			Hunk:         it.Target.Hunk,
			NewLine:      firstOfRange(it.Target.NewLine),
			OldLine:      firstOfRange(it.Target.OldLine),
		})
		if err != nil {
			return nil, fmt.Errorf("item %d: %v", i, err)
		}
		// A multi-line range from the batch wins over the single-line anchor
		// noteAnchor derived from its start.
		if it.Target.NewLine != [2]int{0, 0} {
			side, rng = model.NoteSideNew, it.Target.NewLine
		} else if it.Target.OldLine != [2]int{0, 0} {
			side, rng = model.NoteSideOld, it.Target.OldLine
		}
		n.Address, n.Side, n.Range = addr, side, rng
		planned = append(planned, plan{note: n})
	}
	out := make([]model.Note, 0, len(planned))
	for i, p := range planned {
		var (
			stored model.Note
			err    error
		)
		if p.replyTo != "" {
			stored, err = s.svc.NoteReply(ctx, p.replyTo, p.note)
		} else {
			stored, err = s.svc.NoteAdd(ctx, p.note)
		}
		if err != nil {
			return out, fmt.Errorf("item %d: %v", i, err)
		}
		out = append(out, stored)
	}
	return out, nil
}

// firstOfRange projects a range onto the single line number noteAnchor's
// "exactly one target" check counts; [0,0] stays 0 (unset).
func firstOfRange(r [2]int) int { return r[0] }
```

- [ ] **Step 4: Register the tools and start the sweep**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/mcp/server.go`, add to `sdkServer()` after `s.registerWriteTool(srv)`:

```go
	s.registerNoteTools(srv)
```

and at the end of `New`, after `s.worktree` is set:

```go
	// gg mcp is long-lived, so it does the same note housekeeping a TUI does:
	// apply the configured [notes] budget, then sweep once in the background.
	// Best-effort — a config that will not load must never stop the server.
	if cfg, cerr := svc.EffectiveConfig(ctx); cerr == nil {
		svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
	}
	svc.StartNotesSweep()
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && gofmt -l internal/mcp && go test ./internal/mcp ./internal/archtest -count=1`
Expected: `gofmt -l` prints nothing; PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
git add internal/mcp/notes.go internal/mcp/notes_test.go internal/mcp/server.go internal/mcp/server_test.go && \
git commit -m "feat(mcp): gg_notes_list/gg_note_add/gg_notes_apply/gg_note_rm plus the startup sweep" \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" \
  -m "Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 10: The `reviewing-with-gg` skill, two-skill install, `gg skill path`

**Files:**
- Modify: `internal/agentskill/agentskill.go` (the `Skill` value type + two skills)
- Create: `internal/agentskill/reviewing-with-gg.md`
- Modify: `internal/agentskill/agentskill_test.go`
- Modify: `internal/agentinit/agentinit.go` (`TargetFor`, `Detection.ReviewTarget`, combined `Status`, two-skill `Install`)
- Modify: `internal/agentinit/custom.go` (`CustomDetections` fills `ReviewTarget`)
- Modify: `internal/agentinit/agentinit_test.go`
- Create: `internal/cli/skill.go`
- Create: `internal/cli/skill_test.go`
- Modify: `internal/cli/cli.go` (`runOne` case for `skill`)
- Create (generated, committed): `.claude/skills/reviewing-with-gg/SKILL.md`

**Interfaces:**
- Consumes: `os.UserCacheDir`, `agentinit.Detection`, `agentinit.Install`, `IsCommand` (`commands` already carries `"skill"` from Task 5).
- Produces:
  ```go
  // agentskill
  type Skill struct { Name, Description string; Version int; /* unexported: body, re */ }
  func (s Skill) Body() string
  func (s Skill) Marker() string
  func (s Skill) SkillFile() string
  func (s Skill) PlainFile() string
  func (s Skill) Block() string
  func (s Skill) BlockRe() *regexp.Regexp
  func (s Skill) HasMarker(content []byte) bool
  func (s Skill) InstalledVersion(content []byte) int
  var UsingGG, ReviewingWithGG Skill
  func All() []Skill
  const ReviewVersion = 1
  // agentinit
  func (a Agent) TargetFor(sk agentskill.Skill, projDir, homeDir string) string
  type Detection struct { Agent Agent; Target, ReviewTarget string; Status Status }
  // cli
  var SkillCacheDir string
  func cmdSkill(args []string, stdout, stderr io.Writer) int
  ```

- [ ] **Step 1: Write the skill body**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/agentskill/reviewing-with-gg.md` with EXACTLY this content:

````markdown
# Reviewing with gg

gg is a terminal git client with a persistent, machine-local store of anchored
review notes. The TUI (`gg`) and the browser UI (`gg web`) belong to the user —
do NOT launch either. Leave notes with the `gg note` CLI verbs; the user reads
them inline in their own diff view.

Notes survive quitting gg, so you can annotate a repository while nothing is
running. They are working material: gg drops them once they expire or their
anchored lines are gone.

## Workflow

```text
1. gg status                                  # what changed at all
2. gg diff --stat                             # which files, how much
3. gg diff --hunks --json                     # numbered hunks per file
4. gg diff -- <file>                          # read the actual change
5. gg note list --json                        # what has already been said
6. gg note apply --stdin                      # leave several notes in one call
   (or gg note add … for a single remark)
7. summarise your findings in the chat reply
```

Inspect before you annotate. Read the whole change first, then comment on
intent, structure, risk and follow-ups — not on every hunk.

## Choosing the target

A note anchors to ONE base and ONE result. The flags pick which:

| Flags | What the note anchors to | Old side | New side |
|---|---|---|---|
| (none) | the unstaged working tree | the index | the working file |
| `--cached` | the staged diff | HEAD | the index |
| `--rev <commit>` | that commit's own change | its parent | the commit |

`--rev` takes ONE commit; a range (`A..B`) is refused. `--cached` and `--rev`
cannot be combined. Use the same flags for `gg diff --hunks` and for the note,
so the hunk number you read is the hunk number you pass back.

## Commands

### Inspect

```bash
gg status
gg diff --stat [--cached] [<commit>] [-- <paths>...]
gg diff --hunks [--json] [--cached] [<commit>] [-- <paths>...]
gg diff [--cached] [<commit>] [-- <paths>...]
gg show <commit> [--patch]
gg note list [--file <path>] [--type user|agent|all] [--cached | --rev <c>] [--json]
```

`gg diff --hunks` numbers each file's git `@@` hunks 1-based:

```text
src/search.ts
  1 @@ -15,7 +15,9 @@ export function score
  2 @@ -40,3 +42,8 @@
```

### Notes

```bash
gg note add   --file <path> (--hunk N | --new-line N | --old-line N) [--cached | --rev <c>] \
              --summary "…" [--rationale "…"] [--author <name>] [--source user|agent] [--json]
gg note reply <note-id> --summary "…" [--rationale "…"] [--json]
gg note apply --stdin [--cached | --rev <c>] [--author <name>] [--json]
gg note rm    <note-id>
gg note clear (--file <path> | --all) [--type user|agent|all] --yes
```

- `add` and `reply` print the new note id; `--json` prints the note object.
- Line numbers are 1-based. `--new-line` is the line in the NEW version of the
  file, `--old-line` in the old one; `--hunk N` covers the hunk's whole span.
- CLI notes default to `--source agent` and to `$GG_AGENT` as the author.

`note apply --stdin` accepts either JSON shape, chosen by the top-level key:

```json
{"version":1,"summary":"optional overall note","files":[
  {"path":"src/search.ts","summary":"optional file note","annotations":[
    {"newRange":[15,23],"summary":"…","rationale":"…","tags":["perf"],"confidence":"high"}]}]}
```

```json
{"comments":[
  {"filePath":"src/search.ts","newLine":18,"summary":"…","rationale":"…"},
  {"filePath":"src/search.ts","hunk":2,"summary":"…"},
  {"replyTo":"a1b2c3d4","summary":"addressed in the latest revision"}]}
```

An annotation needs a `summary` plus at least one of `newRange` / `oldRange`
(`newRange` wins when both are present). A comment needs `summary` plus either
`replyTo` alone or `filePath` with exactly one of `hunk`, `hunkNumber`,
`newLine`, `oldLine`. The whole batch is validated before anything is stored,
so one bad item leaves the store untouched. Top-level and per-file `summary`
values have no anchor in gg: they are echoed to stderr, not stored.

## Guiding a review

- Comment on what the reader would NOT spot: a broken invariant, a case the
  change forgets, a name that now lies, a risk the diff hides.
- One note per idea. Put the finding in `summary` and the reasoning in
  `rationale`.
- Anchor precisely: a line beats a hunk when you mean one line.
- Read `gg note list --json` first and reply to an existing thread instead of
  repeating it.
- Do not annotate every hunk, and never leave a note that just restates the
  diff.
- Say what you found in your chat reply too — the notes are for the code, the
  reply is for the person.

## Common errors

- `a note anchors to one commit; pass the tip commit` — you passed a range to
  `--rev`. Use the tip commit's sha.
- `--cached and --rev are mutually exclusive` — pick one target.
- `<file> has K hunks` — the `--hunk N` you passed is past the end; re-read
  `gg diff --hunks`.
- `notes: the new side of <path> does not exist` — the file is not in that
  target's new side (wrong `--cached`/`--rev`, or a deleted file — use
  `--old-line`).
- `note: <id>` not found — the note was removed or swept; list again.
- `review tool wrote no notes` — `gg review --notes` found neither a sidecar
  file nor a JSON report.
````

- [ ] **Step 2: Write the failing agentskill test**

Append to `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/agentskill/agentskill_test.go`:

```go
func TestReviewSkillIdentityAndMarker(t *testing.T) {
	sk := ReviewingWithGG
	if sk.Name != "reviewing-with-gg" || sk.Version != ReviewVersion {
		t.Fatalf("skill identity = %q v%d", sk.Name, sk.Version)
	}
	file := sk.SkillFile()
	for _, want := range []string{
		"---\n", "name: reviewing-with-gg", "description: ",
		fmt.Sprintf("gg:reviewing-with-gg:v%d", sk.Version),
	} {
		if !strings.Contains(file, want) {
			t.Errorf("SkillFile missing %q", want)
		}
	}
	if !strings.Contains(file, sk.Body()) {
		t.Error("SkillFile must contain the body verbatim")
	}
	// The two skills must never recognise each other's markers, or init would
	// report one installed when the other is.
	if sk.HasMarker([]byte(UsingGG.Marker())) || UsingGG.HasMarker([]byte(sk.Marker())) {
		t.Error("each skill's marker must be recognised only by that skill")
	}
	if UsingGG.Name != "using-gg" || !strings.Contains(UsingGG.SkillFile(), "gg:using-gg:v") {
		t.Error("the using-gg marker text must not change — installed copies carry it")
	}
}

func TestReviewSkillBodyCoversTheNoteSurface(t *testing.T) {
	b := ReviewingWithGG.Body()
	for _, want := range []string{
		"gg diff --hunks", "gg note add", "gg note reply", "gg note apply --stdin",
		"gg note list", "gg note rm", "gg note clear",
		"--cached", "--rev", "--hunk", "--new-line", "--old-line",
		`"comments"`, `"newRange"`, "do NOT launch",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("review skill body missing %q", want)
		}
	}
	if strings.Contains(b, "gg:reviewing-with-gg") {
		t.Error("body must not contain markers (renderers add them)")
	}
}

func TestAllReturnsBothSkills(t *testing.T) {
	got := All()
	if len(got) != 2 || got[0].Name != "using-gg" || got[1].Name != "reviewing-with-gg" {
		t.Fatalf("All() = %+v, want using-gg then reviewing-with-gg", got)
	}
}

func TestDogfoodReviewSkillCopyInSync(t *testing.T) {
	path := filepath.Join("..", "..", ".claude", "skills", "reviewing-with-gg", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("dogfood copy not present (non-repo checkout?): %v", err)
	}
	if string(data) != ReviewingWithGG.SkillFile() {
		t.Error(".claude/skills/reviewing-with-gg/SKILL.md is out of sync — run `gg init --agents claude-project` and commit the result")
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && go test ./internal/agentskill -count=1`
Expected: FAIL — `undefined: ReviewingWithGG`, `undefined: All`, `undefined: ReviewVersion`.

- [ ] **Step 4: Rewrite `agentskill.go` around a `Skill` value**

Replace the whole body of `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/agentskill/agentskill.go` with:

```go
// Package agentskill carries the skills that teach AI coding agents to drive gg:
// "using-gg" (the git CLI surface) and "reviewing-with-gg" (the review-notes
// lane). The content is compiled into the binary (go:embed); installed copies
// are derived artifacts that change only when a newer binary's init runs.
package agentskill

import (
	_ "embed"
	"fmt"
	"regexp"
	"strconv"
)

//go:embed using-gg.md
var usingBody string

//go:embed reviewing-with-gg.md
var reviewBody string

// Version is bumped whenever using-gg.md (or the rendered wrappers) change.
// Installed copies carry it so init can tell new/outdated/up-to-date apart.
const Version = 61

// ReviewVersion is the same counter for reviewing-with-gg, which starts at 1
// and moves independently of Version.
const ReviewVersion = 1

// Skill is one embedded skill: its identity, its own version counter, and the
// rendered forms init installs. Markers are per-skill
// ("gg:<name>:v<N>"), so two skills in one shared file never overwrite each
// other and each is recognised only by itself.
type Skill struct {
	Name        string
	Description string
	Version     int

	body    string
	verRe   *regexp.Regexp
	blockRe *regexp.Regexp
}

func newSkill(name, description string, version int, body string) Skill {
	return Skill{
		Name: name, Description: description, Version: version, body: body,
		verRe:   regexp.MustCompile(`gg:` + regexp.QuoteMeta(name) + `:v(\d+)`),
		blockRe: regexp.MustCompile(`(?s)<!-- gg:` + regexp.QuoteMeta(name) + `:v\d+:begin -->.*?<!-- gg:` + regexp.QuoteMeta(name) + `:end -->`),
	}
}

// UsingGG teaches the gg CLI. Its marker text is unchanged from before the
// two-skill split so copies installed by older binaries stay recognised.
var UsingGG = newSkill("using-gg",
	"Use when performing git operations (status, commit, pull, push, branch switch, stash, worktrees) in a repository where the gg CLI is available.",
	Version, usingBody)

// ReviewingWithGG teaches the review-notes lane (gg diff --hunks, gg note …).
var ReviewingWithGG = newSkill("reviewing-with-gg",
	"Use when reviewing code changes in a repository where the gg CLI is available: inspect diffs and leave anchored review notes the user reads in gg.",
	ReviewVersion, reviewBody)

// All is the install set, in a stable order.
func All() []Skill { return []Skill{UsingGG, ReviewingWithGG} }

// Body is the canonical markdown body — no frontmatter, no markers.
func (s Skill) Body() string { return s.body }

// Marker is the version stamp embedded in every rendered form.
func (s Skill) Marker() string { return fmt.Sprintf("<!-- gg:%s:v%d -->", s.Name, s.Version) }

// SkillFile renders the Claude Code SKILL.md form: YAML frontmatter + version
// marker + body. The whole file is gg-owned and safe to overwrite.
func (s Skill) SkillFile() string {
	return "---\n" +
		"name: " + s.Name + "\n" +
		"description: " + s.Description + "\n" +
		"---\n\n" +
		s.Marker() + "\n\n" + s.body
}

// PlainFile renders a frontmatter-free whole file (e.g. Cursor rules).
func (s Skill) PlainFile() string { return s.Marker() + "\n\n" + s.body }

// Block renders the managed-block form for shared files (AGENTS.md, …): the
// body wrapped in begin/end markers so init can replace it without touching
// surrounding content — including a second skill's block in the same file.
func (s Skill) Block() string {
	return fmt.Sprintf("<!-- gg:%s:v%d:begin -->\n\n%s\n<!-- gg:%s:end -->", s.Name, s.Version, s.body, s.Name)
}

// BlockRe matches a previously installed block of THIS skill, any version.
func (s Skill) BlockRe() *regexp.Regexp { return s.blockRe }

// HasMarker reports whether content carries this skill's marker (any version,
// any rendered form).
func (s Skill) HasMarker(content []byte) bool { return s.verRe.Match(content) }

// InstalledVersion extracts the version stamped into previously installed
// content of this skill. 0 means no marker present.
func (s Skill) InstalledVersion(content []byte) int {
	m := s.verRe.FindSubmatch(content)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return 0
	}
	return n
}

// The package-level forms are using-gg's, kept so existing callers and tests
// keep compiling unchanged.
func Body() string                     { return UsingGG.Body() }
func SkillFile() string                { return UsingGG.SkillFile() }
func PlainFile() string                { return UsingGG.PlainFile() }
func Block() string                    { return UsingGG.Block() }
func HasMarker(content []byte) bool    { return UsingGG.HasMarker(content) }
func InstalledVersion(content []byte) int { return UsingGG.InstalledVersion(content) }
```

- [ ] **Step 5: Write the failing agentinit test**

Append to `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/agentinit/agentinit_test.go`:

```go
func TestTargetForDerivesTheSecondSkillPath(t *testing.T) {
	proj, home := fixture(t, []string{".claude", ".cursor", "AGENTS.md"}, nil)
	dets := Detect(proj, home)
	for _, c := range []struct {
		id       string
		wantTail string
	}{
		{"claude-project", filepath.Join(".claude", "skills", "reviewing-with-gg", "SKILL.md")},
		{"cursor", filepath.Join(".cursor", "rules", "reviewing-with-gg.mdc")},
		{"agents-md", "AGENTS.md"}, // block mode: the SAME file, a second block
	} {
		d, ok := byID(dets, c.id)
		if !ok {
			t.Errorf("%s not detected", c.id)
			continue
		}
		if !strings.HasSuffix(d.ReviewTarget, c.wantTail) {
			t.Errorf("%s ReviewTarget = %q, want it to end with %q", c.id, d.ReviewTarget, c.wantTail)
		}
	}
}

func TestInstallWritesBothSkillsInEveryMode(t *testing.T) {
	proj, home := fixture(t, []string{".claude", ".cursor", "AGENTS.md"}, nil)
	for _, id := range []string{"claude-project", "cursor", "agents-md"} {
		d, ok := byID(Detect(proj, home), id)
		if !ok {
			t.Fatalf("%s not detected", id)
		}
		if err := Install(d); err != nil {
			t.Fatalf("%s install: %v", id, err)
		}
		using, err := os.ReadFile(d.Target)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !agentskill.UsingGG.HasMarker(using) {
			t.Errorf("%s: using-gg not installed at %s", id, d.Target)
		}
		review, err := os.ReadFile(d.ReviewTarget)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !agentskill.ReviewingWithGG.HasMarker(review) {
			t.Errorf("%s: reviewing-with-gg not installed at %s", id, d.ReviewTarget)
		}
		if id == "agents-md" {
			// Both blocks live in one file and neither may eat the other.
			if !agentskill.UsingGG.HasMarker(review) {
				t.Errorf("block mode must keep BOTH blocks in %s", d.Target)
			}
			if !strings.Contains(string(review), "existing") {
				t.Errorf("block mode must keep surrounding content in %s", d.Target)
			}
		}
	}
}

func TestStatusIsTheWorstOfTheTwoSkills(t *testing.T) {
	proj, home := fixture(t, []string{".claude"}, nil)
	d, _ := byID(Detect(proj, home), "claude-project")
	if d.Status != StatusNew {
		t.Fatalf("fresh = %v, want StatusNew", d.Status)
	}
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	if got, _ := byID(Detect(proj, home), "claude-project"); got.Status != StatusUpToDate {
		t.Fatalf("after install = %v, want StatusUpToDate", got.Status)
	}
	// using-gg present but the review skill missing = outdated, not new: the
	// agent HAS gg's skill, it is just an older shape.
	if err := os.Remove(d.ReviewTarget); err != nil {
		t.Fatal(err)
	}
	if got, _ := byID(Detect(proj, home), "claude-project"); got.Status != StatusOutdated {
		t.Fatalf("review skill missing = %v, want StatusOutdated", got.Status)
	}
	// using-gg itself missing = new.
	if err := os.Remove(d.Target); err != nil {
		t.Fatal(err)
	}
	if got, _ := byID(Detect(proj, home), "claude-project"); got.Status != StatusNew {
		t.Fatalf("using-gg missing = %v, want StatusNew", got.Status)
	}
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && go test ./internal/agentinit -count=1`
Expected: FAIL — `d.ReviewTarget undefined`.

- [ ] **Step 7: Teach `agentinit` about two skills**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/agentinit/agentinit.go`:

Add after the `Agent` struct:

```go
// TargetFor derives where skill sk lands for this agent from the registry's
// using-gg target, so the registry keeps ONE path per agent:
//
//	ModeSkillFile  .claude/skills/using-gg/SKILL.md → .claude/skills/<name>/SKILL.md
//	ModePlainFile  .cursor/rules/using-gg.mdc       → .cursor/rules/<name>.mdc
//	ModeBlock      the same shared file, a second marked block
//
// Derivation is filepath-based (Dir/Base/Join), never a textual replace of a
// path segment: on Windows the registry path's separators are not "/".
func (a Agent) TargetFor(sk agentskill.Skill, projDir, homeDir string) string {
	t := resolve(a.Target, projDir, homeDir)
	if t == "" {
		return ""
	}
	switch a.Mode {
	case ModeSkillFile:
		// <root>/<skill-name>/SKILL.md
		return filepath.Join(filepath.Dir(filepath.Dir(t)), sk.Name, filepath.Base(t))
	case ModePlainFile:
		return filepath.Join(filepath.Dir(t), sk.Name+filepath.Ext(t))
	default: // ModeBlock: one file, two blocks
		return t
	}
}
```

Extend `Detection`:

```go
// Detection is one detected agent with its resolved targets and status. gg
// installs TWO skills per agent; Status is the worst of the two so the TUI
// Settings popup, the web page and `gg init --list` keep one row per agent.
type Detection struct {
	Agent        Agent
	Target       string // absolute: using-gg
	ReviewTarget string // absolute: reviewing-with-gg (== Target in block mode)
	Status       Status
}
```

In `Detect`, replace the append with:

```go
		target := resolve(a.Target, projDir, homeDir)
		review := a.TargetFor(agentskill.ReviewingWithGG, projDir, homeDir)
		out = append(out, Detection{Agent: a, Target: target, ReviewTarget: review, Status: combinedStatus(target, review)})
```

Replace `status` and `blockRe` with:

```go
// skillStatus classifies one target file against one skill's embedded version.
func skillStatus(sk agentskill.Skill, target string) Status {
	data, err := os.ReadFile(target)
	if err != nil {
		return StatusNew
	}
	if !sk.HasMarker(data) {
		return StatusNew
	}
	if sk.InstalledVersion(data) < sk.Version {
		return StatusOutdated
	}
	return StatusUpToDate
}

// combinedStatus folds the two skills into the single row the frontends show:
// using-gg missing = new (this agent has never been set up); using-gg present
// but the review skill missing or behind = outdated (a refresh adds it).
func combinedStatus(usingTarget, reviewTarget string) Status {
	u := skillStatus(agentskill.UsingGG, usingTarget)
	if u == StatusNew {
		return StatusNew
	}
	r := skillStatus(agentskill.ReviewingWithGG, reviewTarget)
	if u == StatusOutdated || r != StatusUpToDate {
		return StatusOutdated
	}
	return StatusUpToDate
}
```

Rewrite `Install` to write both skills:

```go
// Install writes BOTH embedded skills into d's targets according to the agent's
// mode, creating parent directories as needed. Shared files keep all
// surrounding content — and each skill's own block — byte-for-byte. Idempotent.
func Install(d Detection) error {
	for _, sk := range agentskill.All() {
		target := d.Target
		if sk.Name == agentskill.ReviewingWithGG.Name && d.ReviewTarget != "" {
			target = d.ReviewTarget
		}
		if err := installSkill(sk, target, d.Agent.Mode); err != nil {
			return err
		}
	}
	return nil
}

func installSkill(sk agentskill.Skill, target string, mode Mode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	switch mode {
	case ModeSkillFile:
		return os.WriteFile(target, []byte(sk.SkillFile()), 0o644)
	case ModePlainFile:
		return os.WriteFile(target, []byte(sk.PlainFile()), 0o644)
	case ModeBlock:
		block := sk.Block()
		existing, err := os.ReadFile(target)
		if os.IsNotExist(err) {
			return os.WriteFile(target, []byte(block+"\n"), 0o644)
		}
		if err != nil {
			return err
		}
		// Only THIS skill's block is replaced — the sibling block in the same
		// file must survive untouched.
		if sk.BlockRe().Match(existing) {
			return os.WriteFile(target, sk.BlockRe().ReplaceAllLiteral(existing, []byte(block)), 0o644)
		}
		sep := "\n\n"
		if len(existing) == 0 || strings.HasSuffix(string(existing), "\n\n") {
			sep = ""
		} else if strings.HasSuffix(string(existing), "\n") {
			sep = "\n"
		}
		return os.WriteFile(target, []byte(string(existing)+sep+block+"\n"), 0o644)
	}
	return fmt.Errorf("agentinit: unknown mode %d", mode)
}
```

Remove the now-unused `"regexp"` import if `go build` reports it.

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/agentinit/custom.go`, make `CustomDetections` fill `ReviewTarget` and the combined status the same way. Read the file first; for each custom target build a synthetic `Agent` (it already does) and set:

```go
		review := ag.TargetFor(agentskill.ReviewingWithGG, "", "")
		if review == "" {
			review = ag.Target // an absolute custom target resolves to itself
		}
		out = append(out, Detection{Agent: ag, Target: ag.Target, ReviewTarget: review, Status: combinedStatus(ag.Target, review)})
```

If `resolve` returns "" for the custom (absolute) path shape, derive the sibling directly with the same `filepath.Dir`/`Base`/`Join` rule inline rather than through `TargetFor`.

- [ ] **Step 8: Write the failing `gg skill path` test**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/skill_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentskill"
)

func TestSkillPathMaterialisesAndPrints(t *testing.T) {
	cache := t.TempDir()
	old := SkillCacheDir
	SkillCacheDir = cache
	t.Cleanup(func() { SkillCacheDir = old })

	dir := newRepoDir(t)
	code, out, errb := runCLI(t, dir, "skill", "path")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	p := strings.TrimSpace(out)
	if !filepath.IsAbs(p) {
		t.Fatalf("stdout = %q, want an absolute path", p)
	}
	want := filepath.Join(cache, "gg", "skills", "reviewing-with-gg", "SKILL.md")
	if p != want {
		t.Fatalf("path = %q, want %q (review is the default)", p, want)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != agentskill.ReviewingWithGG.SkillFile() {
		t.Fatal("the written file must be the embedded skill")
	}

	// Second call: same path, and a stale file is refreshed.
	if err := os.WriteFile(p, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errb = runCLI(t, dir, "skill", "path"); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if strings.TrimSpace(out) != want {
		t.Fatalf("path changed between calls: %q", out)
	}
	data, _ = os.ReadFile(p)
	if string(data) != agentskill.ReviewingWithGG.SkillFile() {
		t.Fatal("a file whose marker version differs must be rewritten")
	}
}

func TestSkillPathUsingGG(t *testing.T) {
	cache := t.TempDir()
	old := SkillCacheDir
	SkillCacheDir = cache
	t.Cleanup(func() { SkillCacheDir = old })

	dir := newRepoDir(t)
	code, out, errb := runCLI(t, dir, "skill", "path", "using-gg")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	want := filepath.Join(cache, "gg", "skills", "using-gg", "SKILL.md")
	if strings.TrimSpace(out) != want {
		t.Fatalf("path = %q, want %q", out, want)
	}
}

func TestSkillUsageErrors(t *testing.T) {
	dir := newRepoDir(t)
	for _, args := range [][]string{
		{"skill"},
		{"skill", "path", "bogus"},
		{"skill", "frobnicate"},
	} {
		if code, _, errb := runCLI(t, dir, args...); code != 2 {
			t.Errorf("%v: exit=%d stderr=%s, want 2", args, code, errb)
		}
	}
}
```

- [ ] **Step 9: Implement `gg skill path`**

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/skill.go`:

```go
package cli

// `gg skill path [review|using-gg]` materialises an embedded skill in the user
// cache dir and prints its absolute path — so an agent can read gg's skill
// without `gg init` ever having run in this repository.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/agentskill"
)

// SkillCacheDir overrides the base directory `gg skill path` writes into. ""
// uses os.UserCacheDir(). Tests set it: UserCacheDir ignores XDG_CACHE_HOME on
// macOS and Windows, so an env var alone cannot isolate this cross-platform.
var SkillCacheDir string

// cmdSkill implements `gg skill path [review|using-gg]`.
func cmdSkill(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "path" {
		fmt.Fprintln(stderr, "usage: gg skill path [review|using-gg]")
		return 2
	}
	fs := flag.NewFlagSet("skill path", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "usage: gg skill path [review|using-gg]")
		return 2
	}
	name := "review"
	if fs.NArg() == 1 {
		name = fs.Arg(0)
	}
	var sk agentskill.Skill
	switch name {
	case "review", "reviewing-with-gg":
		sk = agentskill.ReviewingWithGG
	case "using-gg", "using":
		sk = agentskill.UsingGG
	default:
		fmt.Fprintf(stderr, "skill: unknown skill %q (use review or using-gg)\n", name)
		return 2
	}
	base := SkillCacheDir
	if base == "" {
		d, err := os.UserCacheDir()
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		base = d
	}
	path := filepath.Join(base, "gg", "skills", sk.Name, "SKILL.md")
	want := sk.SkillFile()
	// Rewrite only when the file is missing or its marker names another
	// version: a materialised skill is a cache, not user-owned content.
	if data, err := os.ReadFile(path); err != nil || sk.InstalledVersion(data) != sk.Version {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
	}
	fmt.Fprintln(stdout, path)
	return 0
}
```

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/cli/cli.go`, add to `runOne`'s switch after the `"init"` case:

```go
	case "skill":
		return cmdSkill(rest, stdout, stderr)
```

(`"skill"` is already in the `commands` map from Task 5.)

- [ ] **Step 10: Generate the dogfood copy of the new skill**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
go run ./cmd/gg init --agents claude-project
```

This writes `.claude/skills/using-gg/SKILL.md` (unchanged content at Version 61) and the new `.claude/skills/reviewing-with-gg/SKILL.md`. Confirm with `git status --short .claude` that only the new file appears (using-gg must be byte-identical until Task 11 bumps the version).

- [ ] **Step 11: Run the tests to verify they pass**

Run:
```
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
gofmt -l internal/agentskill internal/agentinit internal/cli && \
go test ./internal/agentskill ./internal/agentinit ./internal/cli ./internal/tui ./internal/web -count=1
```
Expected: `gofmt -l` prints nothing; PASS everywhere (`internal/tui/settings_popup.go` and `internal/web/agentsetup.go` read `Detection.Status`/`Target`, both of which still exist).

- [ ] **Step 12: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
git add internal/agentskill/agentskill.go internal/agentskill/reviewing-with-gg.md \
        internal/agentskill/agentskill_test.go internal/agentinit/agentinit.go \
        internal/agentinit/custom.go internal/agentinit/agentinit_test.go \
        internal/cli/skill.go internal/cli/skill_test.go internal/cli/cli.go \
        .claude/skills/reviewing-with-gg/SKILL.md && \
git commit -m "feat(agentskill): reviewing-with-gg skill, two-skill install, gg skill path" \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" \
  -m "Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 11: e2e scenario, `using-gg.md`, version bump and the docs tax

**Files:**
- Create: `e2e/scenarios/s88_notes_cli.toml`
- Modify: `internal/agentskill/using-gg.md`
- Modify: `internal/agentskill/agentskill.go` (`Version = 62`)
- Modify: `internal/agentskill/agentskill_test.go` (surface assertions)
- Regenerate (committed): `.claude/skills/using-gg/SKILL.md`
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: every verb produced by Tasks 3, 5, 6, 7, 8, 10 and the MCP tools from Task 9.
- Produces: no new Go API.

- [ ] **Step 1: Write the e2e scenario**

First confirm the next free number: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && ls e2e/scenarios | tail -1` (expected `s87_worktree_move.toml`; use `s88` if so, otherwise the next free index).

Create `/mnt/t/others/gigagit.worktrees/feat-agent-notes/e2e/scenarios/s88_notes_cli.toml`:

```toml
name = "notes CLI: hunks, add, list, batch apply, clear"

[input]
steps = [
  { write = "a.txt", content = "alpha\nbravo\ncharlie\ndelta\n" },
  { commit = "seed" },
  { write = "a.txt", content = "alpha\nBRAVO\ncharlie\nDELTA\n" },
]

# The agent's first look: numbered git @@ hunks per file.
[[run]]
cmd             = ["diff", "--hunks"]
exit            = 0
stdout_contains = ["a.txt", "1 @@ -"]

[[run]]
cmd             = ["note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "shouty bravo"]
exit            = 0

[[run]]
cmd             = ["note", "list", "--file", "a.txt"]
exit            = 0
stdout_contains = ["a.txt", "new:2-2", "active", "shouty bravo"]

# A batch in hunk's agent-context v1 shape; the unanchored summary goes to
# stderr as context, never into the store.
[[run]]
cmd             = ["note", "apply", "--stdin"]
stdin           = '{"version":1,"summary":"overall fine","files":[{"path":"a.txt","annotations":[{"newRange":[4,4],"summary":"shouty delta","rationale":"same problem"}]}]}'
exit            = 0

[[run]]
cmd             = ["note", "list", "--file", "a.txt"]
exit            = 0
stdout_contains = ["shouty bravo", "shouty delta", "new:4-4"]

# A batch with one bad item stores nothing.
[[run]]
cmd            = ["note", "apply", "--stdin"]
stdin          = '{"files":[{"path":"a.txt","annotations":[{"newRange":[1,1],"summary":"good"},{"newRange":[3,3]}]}]}'
exit           = 1

[[run]]
cmd             = ["note", "list", "--file", "a.txt"]
exit            = 0
stdout_excludes = ["good"]

# clear needs --yes and exactly one of --file/--all.
[[run]]
cmd  = ["note", "clear", "--all"]
exit = 2

[[run]]
cmd             = ["note", "clear", "--all", "--yes"]
exit            = 0
stdout_contains = ["removed 2 notes"]

[[run]]
cmd             = ["note", "list"]
exit            = 0
stdout_excludes = ["shouty"]

[expect]
branch = "main"
```

(The scenario deliberately drives `clear --all --yes` rather than `note rm <id>`: the e2e runner has no mechanism to feed one run's stdout into a later run's argv, and note ids are random. `rm` is covered by `TestNoteReplyInheritsAnchorAndRmRemovesThread` in Task 5.)

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && ./test.sh e2e`
Expected: PASS, including the new scenario.

- [ ] **Step 2: Extend `using-gg.md` with the new verbs**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/agentskill/using-gg.md`, inside `## Commands`, add these entries next to the existing `gg diff` / `gg review` rows (match the file's surrounding formatting exactly — read the neighbouring entries first):

````markdown
### Review notes

```bash
gg diff --hunks [--json] [--cached] [<commit>] [-- <paths>...]   # numbered git @@ hunks per file
gg note add   --file <path> (--hunk N | --new-line N | --old-line N) [--cached | --rev <c>] \
              --summary "…" [--rationale "…"] [--author <name>] [--source user|agent] [--json]
gg note reply <note-id> --summary "…" [--json]
gg note apply --stdin [--cached | --rev <c>] [--author <name>] [--json]   # agent-context v1 or a comments batch
gg note list  [--file <path>] [--type user|agent|all] [--cached | --rev <c>] [--json]
gg note rm    <note-id>
gg note clear (--file <path> | --all) [--type user|agent|all] --yes
gg review --notes [--tool <name>] [--working] [<rev>|<A..B>]     # also import the tool's anchored notes
gg skill path [review|using-gg]                                  # print the bundled skill's path
```

Notes are machine-local review remarks anchored to a line range on one side of
one file; the user reads them inline in `gg` and `gg web`. A note targets ONE
diff: no flag = the unstaged working tree, `--cached` = the staged diff,
`--rev <commit>` = that commit's own change (a range is refused). Line numbers
are 1-based. Full guidance: `gg skill path` (the reviewing-with-gg skill).
````

In the `## Interacting over MCP` section, add the four tools to the list:

```markdown
- `gg_notes_list` — review notes, resolved (read-only)
- `gg_note_add` — leave one anchored note (mutates)
- `gg_notes_apply` — import a batch of notes (mutates)
- `gg_note_rm` — remove one note (mutates)
```

- [ ] **Step 3: Bump the version and pin the new surface in the test**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/agentskill/agentskill.go`:

```go
const Version = 62
```

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/internal/agentskill/agentskill_test.go`, extend `TestBodyCoversTheCLISurface`'s want list with:

```go
		"gg diff --hunks", "gg note add", "gg note apply", "gg note list",
		"gg review --notes", "gg skill path", "gg_notes_list", "gg_note_add",
```

- [ ] **Step 4: Regenerate the dogfood skill copies**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
go run ./cmd/gg init --agents claude-project && \
go test ./internal/agentskill -count=1
```
Expected: `TestDogfoodSkillCopyInSync` and `TestDogfoodReviewSkillCopyInSync` both PASS. `git status --short .claude` shows `.claude/skills/using-gg/SKILL.md` modified (the v62 marker + the new section).

- [ ] **Step 5: Update CHANGELOG.md**

Add a new entry at the top of the current unreleased section of `/mnt/t/others/gigagit.worktrees/feat-agent-notes/CHANGELOG.md`, matching the file's existing bullet style:

```markdown
### Review notes — the agent lane (phase 2)

- `gg diff --hunks [--json]` lists each file's numbered git `@@` hunks, over
  the same patch a note anchors to (a bare commit means that commit's own
  change).
- `gg note add | reply | list | rm | clear | apply --stdin` leave, thread, read
  and remove anchored review notes without a TUI. A note targets one diff: no
  flag = the unstaged working tree, `--cached` = the staged diff, `--rev
  <commit>` = that commit's own change (a range is refused). Anchor with
  `--hunk N`, `--new-line N` or `--old-line N`.
- `gg note apply --stdin` imports hunk's `agent-context.json` v1 and its
  `comment apply` batch shape; the whole batch is validated before the first
  write, so one bad item stores nothing.
- `gg review --notes` asks the configured review tool for anchored notes
  (`$GG_NOTES_FILE`, agent-context v1) and imports them, falling back to a
  report that is itself that JSON. The freeform report still prints and is
  still saved.
- MCP gains `gg_notes_list` (read-only) plus the gated `gg_note_add`,
  `gg_notes_apply` and `gg_note_rm`.
- A second embedded skill, `reviewing-with-gg`, teaches agents the lane;
  `gg init` installs both skills per agent and `gg skill path [review|using-gg]`
  prints a materialised copy. `agentskill.Version` → 62.
- A commit note now stores the FULL sha, so a CLI note and a TUI note on the
  same commit share one target.
```

- [ ] **Step 6: Update README.md**

In the CLI section of `/mnt/t/others/gigagit.worktrees/feat-agent-notes/README.md` (find it with `grep -n "gg review" README.md`), add the new verbs to the command list and a short paragraph:

```markdown
**Review notes.** `gg note add --file <path> --new-line 42 --summary "…"` pins a
remark to a line; `gg note list`, `gg note reply`, `gg note rm` and
`gg note clear` manage them, and `gg note apply --stdin` imports a whole batch
of agent annotations at once. `gg diff --hunks` numbers each file's `@@` hunks
so `--hunk N` can address one. Notes are machine-local and expire (see
`[notes]` under Configuration); they render inline in the TUI diff view and in
`gg web`. `gg review --notes` turns an AI review into anchored notes instead of
a text report.
```

Also add `gg skill path [review|using-gg]` beside the existing `gg init` row.

- [ ] **Step 7: Update `docs/CLAUDE-details.md` and `CLAUDE.md`**

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/docs/CLAUDE-details.md`, append to the notes section (find it with `grep -n "notes" docs/CLAUDE-details.md`):

```markdown
**Phase 2 — the agent lane.** `domain.DiffHunks`/`HunkRange` parse git's `@@`
headers (`ParseDiffHunks`, pure) and `domain.HunkDiffSpec` is the ONE rule for
which patch a hunk number refers to: a bare commit means `<c>^..<c>`, the pair
of texts a commit note anchors to, NOT `git diff <c>`. `domain.NoteTarget` is
the shared flags→`FileAddress` rule (`ErrNoteTargetUsage` = exit 2);
`NoteAddresses` enumerates targets for a bare `gg note list`; `NoteGet` lets the
batch importer validate every `replyTo` before the first write.
`domain.WireNote`/`ToWireNote` is the one JSON note shape (web, CLI `--json`,
MCP). `internal/notebatch` is a pure leaf parsing BOTH agent batch shapes
(agent-context v1 and `comment apply`), shared by the CLI, MCP and the
`gg review --notes` importer; hunk's `low|medium|high` confidence maps onto
`model.Note.Confidence` as 0.3/0.6/0.9. Every `gg note …` verb applies
`[notes]` from the effective config, does its work FIRST, then waits at most 2 s
for the startup sweep (`WaitNotesSweep`). `agentskill` now carries a `Skill`
value type with two instances (`UsingGG`, `ReviewingWithGG`), each with its own
`gg:<name>:vN` marker; `agentinit` installs both per agent and reports the WORST
of the two statuses in its single `Detection` row.
```

In `/mnt/t/others/gigagit.worktrees/feat-agent-notes/CLAUDE.md`, update two package-map rows and add one (keep each to one line):

- `agentskill`: `Two embedded skills ("using-gg", "reviewing-with-gg") behind a Skill value type (go:embed + per-skill version marker) that teach AI agents the gg CLI and the review-notes lane.`
- `cli`: append `, gg note, gg skill path` to the verb list.
- New row after `notes`-adjacent entries: `| `notebatch` | Pure parser for the two agent JSON note-batch shapes (hunk agent-context v1, comment apply); shared by CLI, MCP and the review importer. DAG leaf. |`

- [ ] **Step 8: Run the full unit suite**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && gofmt -l ./internal ./cmd && ./test.sh unit`
Expected: `gofmt -l` prints nothing; every stage green.

- [ ] **Step 9: Run the e2e suite**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && ./test.sh e2e`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && \
git add e2e/scenarios/s88_notes_cli.toml internal/agentskill/using-gg.md \
        internal/agentskill/agentskill.go internal/agentskill/agentskill_test.go \
        .claude/skills/using-gg/SKILL.md \
        CHANGELOG.md README.md docs/CLAUDE-details.md CLAUDE.md && \
git commit -m "docs(notes): e2e scenario, using-gg v62, CHANGELOG/README/details for the agent lane" \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" \
  -m "Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

- [ ] **Step 11: Race gate before handing the branch over**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-agent-notes && ./test.sh race`
Expected: PASS. Do NOT merge — the human owns the merge. Report the branch head sha.

---

## Rulings on spec ambiguities

Recorded so an executor does not re-litigate them:

1. **`gg diff --hunks <commit>` numbers the commit's OWN change** (`<c>^..<c>`),
   not `git diff <commit>`. The spec requires the number an agent reads to be
   the number it passes to `--hunk N`, and a `--rev <c>` note anchors to
   parent→commit. `domain.HunkDiffSpec` is the single source of that rule; an
   explicit `A..B` positional still passes through unchanged.
2. **`confidence` mapping.** The codemap says `model.Note.Confidence` is a
   string; the actual field is `float64`. hunk's `low|medium|high` maps to
   0.3 / 0.6 / 0.9; anything else leaves it 0 (unset).
3. **Orphaned notes are never listed**, in text or `--json`. `NotesAt` drops
   them by contract (phase 1), the TUI and web hide them too, and reads must
   never rewrite the store. `--json` still carries `"status"`, so a `stale`
   note is distinguishable.
4. **The notes file for `gg review --notes` is created and removed by the CLI**,
   not by `engine.ReviewChanges` — the op deletes its own temp files on defer,
   and the caller must read the notes after the op returns.
5. **`ReviewReport` keeps its signature**; `ReviewReportNotes` is the new
   entry point and `ReviewReport` delegates with an empty path. The TUI
   (`internal/tui/review.go:295`) and web (`internal/web/review.go:128`) callers
   are untouched.
6. **`NoteAdd`'s sha normalisation is best-effort**: only a successful
   `RevParse` returning 40 hex replaces the stored value. An unconditional
   resolve would fail every phase-1 `FakeRunner` test that stores a short
   commit; the CLI validates the rev up front in `NoteTarget` instead.
7. **The e2e scenario drives `note clear --all --yes`, not `note rm <id>`** —
   the runner cannot feed one run's stdout into a later run's argv and note ids
   are random. `rm` is covered by a Go CLI test.
8. **Batch `version`**: absent, `0` and `1` are accepted; any other value is a
   clear error. hunk ignores the field, but silently misreading a future schema
   is worse than refusing it.
9. **`--author` on a batch fills in only where an item carries none**; an item's
   own `author` always wins. For `gg review --notes` the fallback author is the
   configured tool's `Name`.
10. **The dogfood skill copies are part of this branch**, not a user step:
    `TestDogfoodSkillCopyInSync` compares `.claude/skills/using-gg/SKILL.md`
    byte-for-byte with `SkillFile()`, so the `Version` bump fails `./test.sh
    unit` until the copy is regenerated (Task 11 Step 4).

## Self-review

**Spec coverage (§4.5 → task):**

| §4.5 requirement | Task |
|---|---|
| Target-flag table (unstaged/untracked, `--cached`, `--rev` full sha) | 2 (`NoteTarget`), 5 |
| Range refusal, `--cached`+`--rev` refusal (exit 2) | 2, 5 |
| `NoteAdd` normalises `Address.Commit` to the full sha | 2 |
| Hunk numbering over the same patch; `model.FileHunks`/`Hunk`; `[0,0]` zero side | 1 |
| `gg diff --hunks [--json]` text + JSON output | 3 |
| `--hunk N` → whole new-side span, old side for a pure deletion; "has K hunks" exit 1 | 1, 5 |
| `gg note add/reply` defaults (`--source agent`, `$GG_AGENT` author), id/JSON output | 5 |
| `gg note list` text format, replies indented, `--type`, `--json` | 6 |
| `gg note rm`, `gg note clear` guards + count | 5, 6 |
| Both batch JSON shapes, all-or-nothing, context lines to stderr | 4, 7 |
| Per-verb policy + bounded `WaitNotesSweep`; `gg batch` admits `note` | 2, 5 |
| `ReviewChanges.NotesFile`, `$GG_NOTES_FILE`, the exact context paragraph | 8 |
| Review import rules (commit = both sides; range/working = tip, new side only, one warning) | 8 |
| Report-is-JSON fallback; "review tool wrote no notes" exit 1; ids on stderr | 8 |
| MCP `gg_notes_list`/`gg_note_add`/`gg_notes_apply`/`gg_note_rm`, annotations, sweep in `New` | 9 |
| `agentskill.Skill` type, `UsingGG` marker unchanged, `ReviewingWithGG` v1 | 10 |
| `reviewing-with-gg.md` content (workflow, commands, both shapes, target table) | 10 |
| `agentinit` installs both skills per agent; one `Detection` row; worst-of status | 10 |
| `gg skill path [review|using-gg]` | 10 |
| `using-gg.md` verbs + `agentskill.Version` → 62 | 11 |
| Testing list (parser, both batch shapes, CLI on a real repo, review fake tool, MCP, agentinit matrix, e2e scenario) | 1, 4, 5–11 |

**Placeholder scan:** every code step carries real Go or TOML; no "TBD",
"similar to Task N", or "add error handling". The one place a step says "read
the file first" (Task 10 Step 7, `custom.go`; Task 11 Step 2, `using-gg.md`
formatting) names the exact function/section and gives the code to insert.

**Type consistency:** `model.Hunk`/`model.FileHunks` (Task 1) are used with the
same field names in Tasks 3, 5, 7, 9. `domain.HunkDiffSpec(cached, rev, paths)`
and `(*Service).HunkRange(ctx, spec, path, n) (model.NoteSide, [2]int, error)`
keep one signature across Tasks 1, 3, 5, 7, 9. `domain.WireNote`/`ToWireNote`
(Task 2) is the single JSON shape in Tasks 5, 6, 7, 9 and in `internal/web`.
`notebatch.Batch{Items, Contexts}` / `Item{Path, ReplyTo, Target, Summary,
Rationale, Author, Tags, Confidence}` (Task 4) is consumed unchanged by Tasks 7,
8, 9. `noteSideRule`/`sideRuleBoth`/`sideRuleNewOnly` and
`planNoteBatch`/`applyNoteBatch` (Task 7) are reused by Task 8.
`agentskill.Skill` methods (Task 10) are the only ones `agentinit` and
`internal/cli/skill.go` call.
