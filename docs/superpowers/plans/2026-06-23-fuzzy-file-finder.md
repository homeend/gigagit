# Fuzzy file finder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A global `F` fuzzy file finder over tracked files; `enter` opens a per-file action menu (View content / Diff / History / Blame / Open in editor / Copy path).

**Architecture:** New `git.LsFiles` verb + `domain.LsFiles` query feed a pure `internal/fuzzy` matcher consumed by a `fileFinderPopup` stack-layer (mirroring `repoPopup`). `enter` builds file-action rows and opens them via the existing action-menu slot; each opens a self-contained surface/layer.

**Tech Stack:** Go 1.26, Bubble Tea, the `internal/tui` layer stack; pure engines like `textdiff`/`commitgraph`.

## Global Constraints

- `internal/tui` and `internal/cli` MUST NOT import `internal/git` (archtest); reach git through `internal/domain`.
- `internal/fuzzy` is pure: imports only the stdlib (no project packages). Mirrors `internal/textdiff`.
- Paths are NUL-delimited (`-z`) end to end — never rely on git's quoted output.
- `Model` is a value receiver; the popup persists via the `*layerStack` pointer (`pushLayer`).
- Run `./test.sh` after each task; `./test.sh race` before the final commit. Measure `fuzzy.Rank` on a 100k-path slice before merge.

---

### Task 1: `git.LsFiles` verb

**Files:** Create `internal/git/ls_files.go`; Test `internal/git/ls_files_test.go`.

**Interfaces:** Produces `func (r *Repo) LsFiles(ctx context.Context) ([]string, error)`.

- [ ] **Step 1: Failing test**

```go
// internal/git/ls_files_test.go
package git

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
)

func TestLsFiles(t *testing.T) {
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git ls-files", gitexec.Result{Stdout: "a.go\x00dir/b — c.txt\x00"})
	r := &Repo{Runner: fr}
	got, err := r.LsFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.go", "dir/b — c.txt"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %q want %q", got, want)
	}
	// argv must use -z (raw paths, no quoting)
	var argv []string
	for _, c := range fr.Calls {
		if c.Name == "git ls-files" {
			argv = c.Argv
		}
	}
	found := false
	for _, a := range argv {
		if a == "-z" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ls-files must pass -z; argv=%v", argv)
	}
}
```

- [ ] **Step 2: Run → FAIL** (`r.LsFiles` undefined). `go test ./internal/git/ -run TestLsFiles`

- [ ] **Step 3: Implement** (mirror `UntrackedFiles` in `compare.go`)

```go
// internal/git/ls_files.go
package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// LsFiles returns every tracked file (paths relative to the working-tree root).
// NUL-delimited (-z) so paths with spaces or non-ASCII bytes come through raw.
func (r *Repo) LsFiles(ctx context.Context) ([]string, error) {
	res, err := r.Runner.Run(ctx, "git ls-files",
		gitcmd.New("ls-files").Arg("-z").ToArgv())
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range strings.Split(res.Stdout, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run → PASS.** Then `go test ./internal/git/`.
- [ ] **Step 5: Commit** `git add internal/git/ && git commit -m "feat(git): LsFiles verb (git ls-files -z)"`

---

### Task 2: `domain.LsFiles` query

**Files:** Modify `internal/domain/query.go`; Test `internal/domain/query_test.go` (or a focused new test).

**Interfaces:** Consumes `git.Repo.LsFiles`. Produces `func (s *Service) LsFiles(ctx context.Context) ([]string, error)`.

- [ ] **Step 1: Failing test** — open a real temp repo (existing `newTestRepo`/helper) with two committed files; assert `svc.LsFiles` returns both. (Follow the existing `TreeFiles`/`Status` domain test pattern in `query_test.go`.)

```go
func TestServiceLsFiles(t *testing.T) {
	_, svc := newRealRepo(t) // existing helper (compare_test.go): real temp repo + Service
	files, err := svc.LsFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("expected tracked files")
	}
}
```

- [ ] **Step 2: Run → FAIL** (`svc.LsFiles` undefined).
- [ ] **Step 3: Implement** (mirror `TreeFiles`)

```go
// internal/domain/query.go
func (s *Service) LsFiles(ctx context.Context) ([]string, error) {
	return query(ctx, s, "ls-files", func(ctx context.Context) ([]string, error) {
		return s.repo.LsFiles(ctx)
	})
}
```

- [ ] **Step 4: Run → PASS.** `./test.sh unit`
- [ ] **Step 5: Commit** `feat(domain): LsFiles query (Read reservation, singleflight)`

---

### Task 3: `internal/fuzzy` pure matcher

**Files:** Create `internal/fuzzy/fuzzy.go`, `internal/fuzzy/fuzzy_test.go`.

**Interfaces (Produces):**
```go
type Match struct { S string; Score int }
func Score(query, candidate string) (int, bool) // ok=false when not a subsequence
func Rank(query string, candidates []string, limit int) []Match // best first, capped
```

- [ ] **Step 1: Failing tests** (assert ORDER, not magic scores)

```go
// internal/fuzzy/fuzzy_test.go
package fuzzy

import "testing"

func TestScoreSubsequence(t *testing.T) {
	if _, ok := Score("fvg", "internal/tui/files_view.go"); !ok {
		t.Fatal("fvg should match files_view.go")
	}
	if _, ok := Score("zzz", "files_view.go"); ok {
		t.Fatal("zzz must not match")
	}
	if _, ok := Score("", "anything"); !ok {
		t.Fatal("empty query matches everything")
	}
}

func TestRankFvgoFindsFilesView(t *testing.T) {
	cands := []string{"favorites/go.mod", "internal/tui/files_view.go", "x/y/z.go"}
	got := Rank("fvgo", cands, 10)
	if len(got) == 0 || got[0].S != "internal/tui/files_view.go" {
		t.Fatalf("fvgo should rank files_view.go first; got %v", got)
	}
}

func TestRankBoundaryBeatsScattered(t *testing.T) {
	// "ab" as a word-boundary/contiguous match should outrank a scattered one.
	got := Rank("ab", []string{"xaxbx", "a/b.go"}, 10)
	if got[0].S != "a/b.go" {
		t.Fatalf("boundary/contiguous match should win; got %v", got)
	}
}

func TestRankEmptyQueryIdentity(t *testing.T) {
	cands := []string{"c", "a", "b"}
	got := Rank("", cands, 2)
	if len(got) != 2 || got[0].S != "c" || got[1].S != "a" {
		t.Fatalf("empty query keeps original order, capped; got %v", got)
	}
}
```

- [ ] **Step 2: Run → FAIL.** `go test ./internal/fuzzy/`
- [ ] **Step 3: Implement** (greedy left-to-right subsequence with boundary/contiguity/basename bonuses)

```go
// internal/fuzzy/fuzzy.go
package fuzzy

import (
	"sort"
	"strings"
)

type Match struct {
	S     string
	Score int
}

func isBoundary(b byte) bool {
	return b == '/' || b == '_' || b == '-' || b == '.' || b == ' '
}

// Score reports whether query is a case-insensitive subsequence of candidate and
// a rank score (higher = better). Bonuses: a match at a path/word boundary, a
// contiguous run, and a match in the basename (after the last '/'); mild length
// penalty so tighter/shorter paths win ties.
func Score(query, candidate string) (int, bool) {
	if query == "" {
		return 0, true
	}
	q := strings.ToLower(query)
	c := strings.ToLower(candidate)
	lastSlash := strings.LastIndexByte(c, '/')
	score, qi, prev := 0, 0, -2
	for ci := 0; ci < len(c) && qi < len(q); ci++ {
		if q[qi] != c[ci] {
			continue
		}
		b := 1
		if ci == prev+1 {
			b += 3 // contiguous
		}
		if ci == 0 || isBoundary(c[ci-1]) {
			b += 5 // word/path boundary
		}
		if ci > lastSlash {
			b += 2 // basename
		}
		score += b
		prev = ci
		qi++
	}
	if qi != len(q) {
		return 0, false
	}
	score -= len(c) / 64 // mild length penalty
	return score, true
}

// Rank filters candidates to those matching query and sorts best-first (ties
// broken by path for determinism), keeping at most limit (limit<=0 = all).
func Rank(query string, candidates []string, limit int) []Match {
	out := make([]Match, 0, len(candidates))
	for _, c := range candidates {
		if s, ok := Score(query, c); ok {
			out = append(out, Match{S: c, Score: s})
		}
	}
	if query != "" {
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Score != out[j].Score {
				return out[i].Score > out[j].Score
			}
			return out[i].S < out[j].S
		})
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
```

- [ ] **Step 4: Run → PASS.** Adjust the bonus constants only if an ORDER assertion fails (never assert magic scores).
- [ ] **Step 5: Perf microbench** — add a timed test over a synthetic 100k-path slice; assert `Rank` completes well under ~30ms. If it doesn't, switch `Rank` to a bounded top-N selection instead of full sort.
- [ ] **Step 6: Commit** `feat(fuzzy): pure subsequence file-path matcher (Score/Rank)`

---

### Task 4: `fileFinderPopup` + `F` open + load + filter

**Files:** Create `internal/tui/file_finder.go`, `internal/tui/file_finder_test.go`. Modify the content-surface key handlers to wire `F` (base dispatch in `model.go`; `diff_view.go`, `files_view.go`, `history_view.go`, `blame_view.go`, `stash_view.go` — wherever `g`/`G` are already global).

**Interfaces (Produces):**
```go
type fileFinderPopup struct { all []string; loading bool; query string; matches []fuzzy.Match; sel int; mode dispMode; hscroll int }
func (m Model) openFileFinder() (Model, tea.Cmd)
type lsFilesMsg struct { paths []string; err error }
func (m Model) loadLsFilesCmd() tea.Cmd
func (p *fileFinderPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) // satisfies layer
func (p *fileFinderPopup) render(m Model, below string) string            // satisfies layer
```

- [ ] **Step 1: Failing tests**

```go
// internal/tui/file_finder_test.go (sketch — adapt helpers to the real ones)
func TestFileFinderOpensAndLoads(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	p := layerOf[*fileFinderPopup](m)
	if p == nil || !p.loading {
		t.Fatal("F should push a loading file finder")
	}
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a/b.go", "c.txt"}})
	m = nm.(Model)
	p = layerOf[*fileFinderPopup](m)
	if p == nil || p.loading || len(p.all) != 2 {
		t.Fatalf("lsFilesMsg should fill the list; %+v", p)
	}
}

func TestFileFinderFiltersAndClamps(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a/b.go", "c.txt", "files_view.go"}})
	m = nm.(Model)
	// type "fv" -> matches narrow; sel clamps within matches
	for _, r := range "fv" {
		nm, _ = m.Update(keyMsg(string(r)))
		m = nm.(Model)
	}
	p := layerOf[*fileFinderPopup](m)
	if len(p.matches) == 0 || p.matches[0].S != "files_view.go" {
		t.Fatalf("fv should match files_view.go first; %+v", p.matches)
	}
	if p.sel < 0 || p.sel >= len(p.matches) {
		t.Fatalf("sel out of range: %d", p.sel)
	}
}
```

- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement** `fileFinderPopup`, `openFileFinder` (`pushLayer(&fileFinderPopup{loading:true})` + return `loadLsFilesCmd`), `loadLsFilesCmd` (`svc.LsFiles` off-thread → `lsFilesMsg`), the `lsFilesMsg` handler (fill `all`, clear `loading`, re-rank), `update` (runes append to `query` + re-`Rank`; backspace; `↑/↓` clamp `sel`; `z` mode; `esc` `popLayer`; `enter` → Task 5), `render` (window-then-build over `matches`, title shows match count / loading). Mirror `repoPopup`'s render/update shape.
- [ ] **Step 4: Wire `F`** in each content surface's key switch (next to the existing global `g`/`G`): `case "F": return m.openFileFinder()`.
- [ ] **Step 5: Run → PASS;** `./test.sh unit`.
- [ ] **Step 6: Commit** `feat(tui): F fuzzy file finder popup (load + filter)`

---

### Task 5: `enter` → per-file action menu

**Files:** Modify `internal/tui/file_finder.go` (the `enter` case + the row builders + the View-content load); Test `internal/tui/file_finder_actions_test.go`.

**Interfaces (Produces):** `func (m Model) fileFinderActionRows(path string) []actionRow`; a `fileContentLayerMsg`/load for View content that fills a pushed `contentPopup` layer.

- [ ] **Step 1: Failing tests**

```go
func TestFileFinderEnterOpensActionMenu(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a/b.go"}})
	m = nm.(Model)
	nm, _ = m.Update(keyMsg("enter"))
	m = nm.(Model)
	if m.actionMenu == nil {
		t.Fatal("enter should open the file-action menu")
	}
	got := map[string]bool{}
	for _, r := range m.actionMenu.rows {
		got[r.id] = true
	}
	for _, id := range []string{"ff-view", "ff-diff", "ff-history", "ff-blame", "ff-editor", "ff-copy-path"} {
		if !got[id] {
			t.Fatalf("missing %s; rows=%v", id, got)
		}
	}
}

func TestFileFinderHistoryActionOpensHistoryLayer(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a/b.go"}})
	m = nm.(Model)
	rows := m.fileFinderActionRows("a/b.go")
	var run func(Model) (tea.Model, tea.Cmd)
	for _, r := range rows {
		if r.id == "ff-history" {
			run = r.run
		}
	}
	nm, _ = run(m)
	m = nm.(Model)
	if layerOf[*historyView](m) == nil {
		t.Fatal("history action should push a historyView layer")
	}
	if layerOf[*fileFinderPopup](m) != nil {
		t.Fatal("the finder must be popped when an action opens a surface")
	}
}
```

- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement the `enter` case + `fileFinderActionRows(path)`**

`enter` (in `fileFinderPopup.update`): if a match is selected, `m.actionMenu = &actionMenu{rows: m.fileFinderActionRows(sel.S)}; return m, nil`. Rows (each `run` first does `m = m.popLayer()` to drop the finder, then opens the target):

- `ff-view` **View content** — push a `contentPopup` layer + load: `m = m.popLayer(); cp := newContentPopup("View "+path, []contentLine{{text:"(loading…)"}}); m = m.pushLayer(cp); return m, m.loadFileContentLayerCmd(path)`. Add `fileContentLayerMsg{path, lines, err}` + `loadFileContentLayerCmd` (`svc.ShowFile(ctx, "HEAD", path)` → `contentLines`), whose handler fills the **on-stack** `contentPopup` via `layerOf` (tag-gated by path). Do NOT reuse `openPreview`/`loadFileContentCmd` (those target `m.filesPreview`).
- `ff-diff` **Diff** — `m = m.popLayer()`; open a full-screen diff of the path: working tree vs HEAD via the compare-diff path (`Endpoint{Kind:EndpointCommit,Hash:"HEAD"}` ↔ `Endpoint{Kind:EndpointWorkTree}`, `pushLayer(&diffView{loading:true})` + `loadCompareDiffCmd`). Reuse the helper the files-view full-tree `enter` uses (`openDiffForFileLine`-style); if none fits directly, build the `diffView` + `loadCompareDiffCmd(left,right,line)` inline.
- `ff-history` **History** — `m = m.popLayer(); hv := newHistoryView(navContext{path: path}); m = m.pushLayer(hv); return m, m.loadHistoryListCmd(navContext{path: path}, hv.listTag)`.
- `ff-blame` **Blame** — `m = m.popLayer(); bv := newBlameView(navContext{path: path}); m = m.pushLayer(bv); return m, m.loadBlameCmd(navContext{path: path}, bv.tag)`.
- `ff-editor` **Open in editor** — `m = m.popLayer(); return m, m.openInEditorCmd(path, func(ctx) ([]byte,error){ return m.svc.ShowFile(ctx, "HEAD", path) })`.
- `ff-copy-path` **Copy path** — `m = m.popLayer(); return m, m.copyToClipboardCmd("Copied "+path, path)`.

(Confirm `newHistoryView`/`newBlameView` field names — `listTag`/`tag` — against current source while implementing.)

- [ ] **Step 4: Run → PASS;** `./test.sh unit`.
- [ ] **Step 5: Commit** `feat(tui): file finder enter -> per-file action menu`

---

### Task 6: docs + help/footer + race

**Files:** `CHANGELOG.md`; help text / footer hint for `F` (wherever `g`/`G` are advertised); `./test.sh race`.

- [ ] **Step 1: CHANGELOG** (Added):
```markdown
- **Fuzzy file finder.** `F` opens a finder over every tracked file; fuzzy-type a
  path (`fvgo` → `files_view.go`) and `enter` opens a per-file menu: View content,
  Diff, History, Blame, Open in editor, Copy path. Built for tens of thousands of
  files in a monorepo.
```
- [ ] **Step 2: Advertise `F`** in the help sheet / footer where the global `g`/`G` switchers are listed.
- [ ] **Step 3: Race** `./test.sh race` → PASS.
- [ ] **Step 4: Commit** `docs(tui): changelog + help for the fuzzy file finder`

---

## Self-Review

**Spec coverage:** `git.LsFiles` (T1) ✓; `domain.LsFiles` (T2) ✓; pure `internal/fuzzy` matcher + ranking tests + perf (T3) ✓; `fileFinderPopup` + `F` + async load + filter, window-then-build (T4) ✓; `enter` → action menu with the six self-contained actions incl. View-content-as-`contentPopup`-layer (NOT `openPreview`) (T5) ✓; CHANGELOG + help (T6) ✓; archtest (fuzzy pure; tui via domain) — no new cross-imports ✓.

**Placeholder scan:** test-helper names (`loadedModel`/`loadedModelLinearCommits`/`newServiceWithRepo`) flagged to match the real helpers; the matcher's bonus constants are concrete (tests assert order, so constants are tunable without breaking tests).

**Type consistency:** `LsFiles() ([]string, error)` (git+domain); `fuzzy.Score(string,string)(int,bool)`, `Rank(string,[]string,int)[]Match`, `Match{S,Score}`; `fileFinderPopup`, `lsFilesMsg{paths,err}`, `fileFinderActionRows(string)[]actionRow`, `fileContentLayerMsg{path,lines,err}` consistent across tasks.
