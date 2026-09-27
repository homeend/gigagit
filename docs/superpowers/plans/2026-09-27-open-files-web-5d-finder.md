# Open files on the web — plan 5d: the F finder — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline, THIS session — the repo forbids subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `F` in `gg web` turns the files pane into a fuzzy list of every file in the working tree (tracked minus deleted, plus untracked marked `(untracked)`), the diff pane previews the file under the cursor after a 150 ms settle, `enter` / `.` open its actions (view file, diff, history, blame, copy name / path / absolute path / file link), `ctrl+]` opens a row in the background, and `esc` gives back exactly the screen F covered. It replaces the page's old overlay finder (tracked files only, `enter` = history), as the TUI's F window replaced its popup.

**Architecture:** The list rule moves from the TUI into `domain.WorktreeFileList` (pure; the TUI calls it too). A new `GET /api/worktree-files` (`internal/web/worktreefiles.go`) reads `LsFiles` + `Status` once per F open (`fresh=1`), caches the list per server, and answers a ranked page (a query: `fuzzy.Rank`, cap 200) or a sorted page (no query: 200 rows from `offset`). `/api/files` and its per-HEAD cache are deleted. On the page, `static/wtfinder.js` is a LAYER (it owns the keyboard, so every surface it opens — viewer, history, blame, the `.` menu, the `ctrl+\` switcher — returns to it on esc) whose DOM lives INSIDE the two panes: a `wtf` class on `#panes` shows `#wtf` in the files pane and `#wtf-preview` in the diff pane over whatever layout is up, and hides everything else in those panes without touching it — so esc (drop the class) restores the stage underneath untouched.

**Tech Stack:** Go 1.26 (`internal/domain`, `internal/web`, `internal/tui`), vanilla ES modules in `internal/web/static`, node for the pure-JS guards, playwright for the browser check.

**Spec:** `docs/superpowers/specs/2026-09-26-open-files-web-design.md` — the "F finder (5d)" bullet, ruling 2 (the preview is the diff pane's one exception), ruling 5, the 5d row.

## Global Constraints

- Paths in rows/titles are cut in the MIDDLE with `elidePath`, never the file name; the row's full path rides its `title`.
- Key hints live in `#foot` (chips), never inside a box, never in a toast or an input placeholder.
- A `\` inside a JS template literal or string is written `\\` (the 5c chip bug: `\ ` is an escaped space).
- Protocol/op-line prose is English (the web is not localized); wording follows the TUI where it has words: `Files (working tree)  n/N`, `(untracked)`, `(no files)`, `(file deleted on disk)`, `(file too large to preview)`, `(empty file)`, `closed %s (20 files open)`.
- The list rule is the TUI's, shared: tracked (`ls-files`, the index — a staged new file is listed) minus status `D` (staged or unstaged), plus the untracked files the status already knows; no extra untracked scan.
- Ranked results cap at 200 (the TUI's `fileFinderLimit`); the preview settles 150 ms (the TUI's `wtPreviewSettle`).
- Every wire value is validated → 400 via `writeErr`. `q` never reaches git (it is ranked in process).
- New Go tests call `t.Parallel()`.
- Pure JS sections run bare under node: no DOM, no imports between the markers.
- Browser checks ASSERT VISIBILITY (computed style + bounding box) and run against the UNFIXED installed build first; start `gg web --addr 127.0.0.1:0`, read the URL from its log, isolate `XDG_STATE_HOME`/`XDG_CONFIG_HOME` in the scratchpad, kill only your own server by PID, call `~/go/bin/gg` directly.
- Every task-done runs the FULL web package: `go test ./internal/web/`.
- After the merge: `./build.sh install` and `./build.sh web`. No CLI change → `using-gg.md` and `agentskill.Version` stay.

## Rulings (planning — the user reviews these)

| # | Ruling | Why | Cost if wrong |
|---|--------|-----|---------------|
| D1 | **The server ranks and pages; the page never holds the whole list.** A query → the best 200 (`fuzzy.Rank`), `limited` when the cap is hit. No query → the sorted list, 200 rows per page from `offset`; the answer's `next` is the next page's offset (absent on the last), and the page fetches it when the cursor comes within a screen of the end or the list scrolls to its bottom. Title `Files (working tree)  n/N` (n = the whole list without a query, the matches with one) — the TUI's words | gg's target repos hold 100k+ paths; shipping them all is the O(everything) mistake `/api/files` was written to avoid, and 100k `<li>` never render | a client-side ranker (one big fetch) instead |
| D2 | **The list is read once per F open** (`fresh=1`), cached per server; every keystroke and page reads the cache. Each read gets a build number `gen`; a later page whose `gen` differs from the first page's (another tab re-read the list mid-scroll) restarts the list at the top. The list does not follow the disk while F is up — the TUI's F doesn't either (read once at open); esc + F re-reads | `ls-files` + `status` per keystroke is seconds on the target repos; one cache slot keeps the code small | a stale row until F reopens |
| D3 | **F is a layer, drawn in the panes.** `pushLayer("wtf", #wtf, {onKey})` makes F own the keyboard, so the viewer, history, blame, the `.` menu and the `ctrl+\` switcher open OVER it and their esc lands back in F (the TUI's "esc returns to the window"). A `wtf` class on `#panes` shows F in the files pane and its preview in the diff pane over ANY layout (list / files / diff, sidebar or not, file list folded or not) and hides the panes' own children — nothing underneath is mutated, so esc (drop the class) restores the stage exactly | spec ruling 5 ("takes the files pane, with a live preview in the diff pane"); the stage machinery in `files.js` stays untouched | a CSS rule the grid rules outrank (pinned by the browser check) |
| D4 | **Keys.** List: `↑↓ j k` move, `pgup/pgdn`, `home/end`, `/` opens the filter field, `enter` / `.` the actions menu, `ctrl+]` the row in the background, `ctrl+\` the open-files switcher, `esc` clears a kept query first, then closes F. In the field: typing filters (120 ms debounce), `↑↓` still move, `enter` keeps the query and returns to the list, `esc` clears it. Every other key is swallowed (nothing reaches the page underneath); browser keys with ctrl/meta/alt pass (no `preventDefault`). `F` opens F only when no layer is up and no field has focus (the old finder's guards: typing `F` in the commit box stays a letter) | the TUI's F keys, minus `ctrl+t` / `→` preview focus / `g` `G` switchers (the web preview scrolls with the mouse) | a key added later |
| D5 | **Mouse:** click = select (the preview follows), double-click = view file, right-click = the actions menu at the pointer | the web's row idiom (files list, viewer) | a gesture change |
| D6 | **Actions** (`enter` / `.` / right-click; the TUI's `worktreeFileRows` minus *Edit in editor* and *Commits touching this*, which the spec's list leaves out): **view file** (5a viewer, over F), **diff (working tree changes)**, **file history**, **blame** — the git rows for tracked files only — then **copy path**, **copy absolute path**, **copy file name** (the file rows' `copyPathRows`) and **copy file link** (the content link, via the shared `copyFileLink` presence check) | spec's action list; untracked files have no history | a row added later |
| D7 | **diff closes F** and opens the file's change in the page's diff stage (esc there steps to the working-tree list, not back to F). `/api/diff` has no HEAD ↔ working tree lane — only `wt=unstaged` (index → disk) and `wt=staged` (HEAD → index) — so the row opens the unstaged change, else the staged one, else says `<path> has no changes in the working tree`. The viewer's row shares the helper (`openWorktreeFileDiff`, moved out of `viewerDiffWorktree`) and both are relabelled **diff (working tree changes)** — the old "diff (HEAD ↔ working tree)" label named a lane the page never opened, and a staged-only change read "no changes". NOT taken: drawing the diff inside F's preview pane (the TUI pushes the diff over its window, esc returns) — the web's diff lives in the diff stage, and a second diff renderer inside F is a feature of its own | the 5a viewer's precedent; the page has one diff surface | the TUI parity of esc-returns for the diff row |
| D8 | **`ctrl+]` on a row** = `POST /api/open-files {op:"open", src:"worktree", path, tab:""}` (5b's background open — the same op 5c's agent verb uses): the file joins the shared list without showing; op line `<path> opened in the background` (5c's words, no key hint), `— closed <evicted> (20 files open)` appended on an eviction. F stays up | 5b/5c already built it; a toast carries no key hint (the rule) | none |
| D9 | **The preview is NOT an open file** (the TUI's rule): it reads `/api/file-content?src=worktree` directly and never touches `/api/open-files`, so scrolling past files never fills the `ctrl+\` list. It paints line numbers + syntax colour (the viewer's `renderCell`, `.vline` rows, the shared `w` long-line mode), its title `<path> (working tree)` cut in the middle. A later cursor move drops an earlier answer (a generation counter + path check). A missing file → `(file deleted on disk)`, too large → `(file too large to preview)`, empty → `(empty file)`, a failed read → `(load failed: …)`; no row (empty list / no match) → an empty preview | TUI `wtPreviewSettled`; spec ruling 2 | none |
| D10 | **A steered navigate that lands on the panes closes F first** (the TUI's `steerToPanels` closes its files view); a content navigate and `file_focus` open the viewer OVER F, and esc lands back in F | the agent's navigate must be visible | none |
| D11 | **The old finder goes:** `search.js`'s overlay (`mountOverlay("finder")`, `openFinder`, its `F` key and help row), `GET /api/files`, `trackedFiles`, `fileFinderLimit` and `searchState`'s `filesHead`/`files` cache, and their three Go tests. `palette.js`'s row imports `openFinder` from `wtfinder.js` instead and reads **files in the working tree… (F)**. The main footer gains an `F files` chip (the TUI advertises F in help AND footer) | the TUI deleted `fileFinderPopup` for the F window; nothing else calls `/api/files` (grep: `search.js:218` only) | none |
| D12 | **Footer:** while F is up, `#foot` shows `↑↓ j k move · / filter · enter . actions · ctrl+] background · ctrl+\ open files · esc close` (a `pushFoot("wtf")` chip stack entry; surfaces opened over F push their own and pop back to F's). A key-up guard drops F's class and chips if something else closed the layer (the viewer's guard) | memory: hints only in the bottom bar | a chip's wording |

## Review Focus

1. **F over an open diff (layout `diff`)**: esc brings the diff back with its scroll position and its side-by-side verdict — `resize.js:122` may re-render the diff while `#diff-body` is `display:none`. Pinned in the browser check (step i: `#diff-body` visible, same `scrollTop`, same `table.diff` column count after esc).
2. **F with the file list folded (`nofiles`)**: F's list shows at full width (`#panes.nofiles #files-pane > :not(#files-top)` would otherwise hide `#wtf`); esc restores the fold. Pinned in the browser check (step j) and by the CSS rule's higher specificity (Task 2).
3. **A held arrow over a slow mount**: only the settled row loads; an earlier slow answer never overwrites a later one. Pinned in Task 3 (`wtPreviewFresh` pure test: gen + path) and the browser check (step b waits for the settled file only).
4. **An untracked row**: view + copy rows only; a file deleted between the list read and the preview → `(file deleted on disk)`, never an error. Pinned in Task 1 (`TestWorktreeFilesListsDiskFiles`), Task 4 (`wtActions(true)`), and the browser check (step e + `rm` then preview).
5. **A steered navigate while F is up** closes F; a content navigate opens the viewer over F and esc lands back in F. Pinned in the browser check (step k) and the live.js wiring row.

---

### Task 1: the server list — `domain.WorktreeFileList`, `GET /api/worktree-files`, `/api/files` removed

**Files:**
- Create: `internal/domain/worktreefiles.go`, `internal/domain/worktreefiles_test.go`
- Modify: `internal/tui/files_worktree.go:14-40` (delete `worktreeFileList`, call `domain.WorktreeFileList` in `wtLoaded`), `internal/tui/files_worktree_test.go:16-39` (delete the two moved tests)
- Create: `internal/web/worktreefiles.go`, `internal/web/worktreefiles_test.go`
- Modify: `internal/web/search.go` (delete the `/api/files` route, `fileFinderLimit`, `handleFiles`, `trackedFiles`, `searchState.filesHead/files`), `internal/web/search_test.go` (delete `filesResp`, `TestFilesEndpoint`, `TestFilesEndpointLimits`, `TestFilesCacheFollowsHead`)

**Interfaces — Produces:**
- `func domain.WorktreeFileList(tracked []string, st model.WorkingTreeStatus) (paths []string, untracked map[string]bool)`
- `GET /api/worktree-files?q=&offset=&fresh=1` → `{"files":[{"path":"a.go","untracked":true?}], "total":N, "next":K?, "limited":true?, "gen":G}`; `offset` not a non-negative integer → 400 `invalid offset`; `offset` is ignored with a query.

- [ ] **Step 1: Write the failing domain test** — `internal/domain/worktreefiles_test.go` (the TUI's two tests, moved):

```go
package domain

import (
	"fmt"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestWorktreeFileListDropsDeletedAddsUntracked(t *testing.T) {
	t.Parallel()
	st := model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "b.go", Staged: '.', Unstaged: 'D'},
		{Path: "n.txt", Kind: model.KindUntracked},
		{Path: "a.go", Staged: '.', Unstaged: 'M'},
	}}
	paths, untracked := WorktreeFileList([]string{"d.go", "a.go", "b.go"}, st)
	if got := fmt.Sprint(paths); got != "[a.go d.go n.txt]" {
		t.Fatalf("paths = %s, want [a.go d.go n.txt]", got)
	}
	if len(untracked) != 1 || !untracked["n.txt"] {
		t.Fatalf("untracked = %v, want {n.txt}", untracked)
	}
}

func TestWorktreeFileListStagedDeletionIsGone(t *testing.T) {
	t.Parallel()
	st := model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "gone.go", Staged: 'D', Unstaged: '.'}}}
	paths, _ := WorktreeFileList([]string{"gone.go", "kept.go", "kept.go"}, st)
	if got := fmt.Sprint(paths); got != "[kept.go]" {
		t.Fatalf("paths = %s, want [kept.go]", got)
	}
}
```

- [ ] **Step 2: Run it — expect a compile failure**

Run: `go test ./internal/domain/ -run WorktreeFileList`
Expected: FAIL — `undefined: WorktreeFileList`.

- [ ] **Step 3: Move the function** — create `internal/domain/worktreefiles.go`:

```go
package domain

import (
	"slices"

	"github.com/homeend/gigagit/internal/model"
)

// WorktreeFileList is the working tree's files as F lists them (the TUI's F
// window and gg web's files pane): the tracked files (ls-files, the index —
// so a staged new file is listed) minus those deleted in the working tree or
// the index, plus the untracked files the status already knows — no extra
// git walk (an untracked scan is the slow half of git status on a large
// tree). Sorted and deduplicated; untracked names the untracked ones.
func WorktreeFileList(tracked []string, st model.WorkingTreeStatus) (paths []string, untracked map[string]bool) {
	deleted := map[string]bool{}
	untracked = map[string]bool{}
	for _, f := range st.Files {
		switch {
		case f.Kind == model.KindUntracked:
			untracked[f.Path] = true
		case f.Staged == 'D' || f.Unstaged == 'D':
			deleted[f.Path] = true
		}
	}
	for _, p := range tracked {
		if !deleted[p] {
			paths = append(paths, p)
		}
	}
	for p := range untracked {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return slices.Compact(paths), untracked
}
```

In `internal/tui/files_worktree.go`: delete lines 14–40 (the doc comment and `worktreeFileList`); `wtLoaded`'s `w.all, w.untracked = domain.WorktreeFileList(msg.paths, m.status)`; add the `"github.com/homeend/gigagit/internal/domain"` import and drop `"slices"` / `"github.com/homeend/gigagit/internal/model"` if `go build` names them unused. In `internal/tui/files_worktree_test.go`: delete `TestWorktreeFileListDropsDeletedAddsUntracked` and `TestWorktreeFileListStagedDeletionIsGone` (lines 16–39); drop imports `go vet` names unused.

- [ ] **Step 4: Run the domain + TUI tests**

Run: `go build ./... && go test ./internal/domain/ -run WorktreeFileList && go test ./internal/tui/ -run 'Worktree|Finder|WtWindow'`
Expected: PASS (both moved tests; the TUI's F window tests unchanged).

- [ ] **Step 5: Write the failing web tests** — `internal/web/worktreefiles_test.go`:

```go
package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type wtFilesResp struct {
	Files []struct {
		Path      string `json:"path"`
		Untracked bool   `json:"untracked"`
	} `json:"files"`
	Total   int  `json:"total"`
	Next    int  `json:"next"`
	Limited bool `json:"limited"`
	Gen     int  `json:"gen"`
}

func (r wtFilesResp) rows() string {
	var b []string
	for _, f := range r.Files {
		s := f.Path
		if f.Untracked {
			s += "(u)"
		}
		b = append(b, s)
	}
	return strings.Join(b, " ")
}

func writeFiles(t *testing.T, dir string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The list is the working tree on disk: tracked minus deleted (either side),
// plus untracked (marked), plus a staged new file (it is in the index).
func TestWorktreeFilesListsDiskFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	writeFiles(t, dir, "a.go", "b.go", "dir/c.go", "gone.go")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "c1")
	if err := os.Remove(filepath.Join(dir, "b.go")); err != nil { // unstaged D
		t.Fatal(err)
	}
	gitRun(t, dir, "rm", "-q", "gone.go") // staged D
	writeFiles(t, dir, "n.txt", "s.go")
	gitRun(t, dir, "add", "s.go") // a staged new file
	ts := serve(t, New(domain.Open(dir)))

	var got wtFilesResp
	if code := getJSON(t, ts, "/api/worktree-files?fresh=1", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if want := "a.go dir/c.go n.txt(u) s.go"; got.rows() != want {
		t.Fatalf("rows = %q, want %q", got.rows(), want)
	}
	if got.Total != 4 || got.Next != 0 || got.Limited || got.Gen == 0 {
		t.Fatalf("total/next/limited/gen = %d/%d/%v/%d, want 4/0/false/>0", got.Total, got.Next, got.Limited, got.Gen)
	}
}

// A query ranks fuzzily (a subsequence, not a substring) over the whole list;
// no match is an empty list, never null.
func TestWorktreeFilesFuzzy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	writeFiles(t, dir, "cmd/gg/main.go", "internal/web/search.go", "README.md")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "c1")
	ts := serve(t, New(domain.Open(dir)))

	var got wtFilesResp
	getJSON(t, ts, "/api/worktree-files?fresh=1&q=cgmain", &got)
	if got.rows() != "cmd/gg/main.go" || got.Total != 3 {
		t.Fatalf("rows = %q total %d, want cmd/gg/main.go of 3", got.rows(), got.Total)
	}
	var none wtFilesResp
	getJSON(t, ts, "/api/worktree-files?q=zzzznope", &none)
	if none.Files == nil || len(none.Files) != 0 {
		t.Fatalf("files = %#v, want an empty list", none.Files)
	}
}

// No query pages through the sorted list 200 at a time; a query caps at 200.
func TestWorktreeFilesPages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	var paths []string
	for i := 0; i < 450; i++ {
		paths = append(paths, fmt.Sprintf("f%03d.txt", i))
	}
	writeFiles(t, dir, paths...)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "c1")
	ts := serve(t, New(domain.Open(dir)))

	var p0, p1, p2, far, ranked wtFilesResp
	getJSON(t, ts, "/api/worktree-files?fresh=1", &p0)
	getJSON(t, ts, "/api/worktree-files?offset=200", &p1)
	getJSON(t, ts, "/api/worktree-files?offset=400", &p2)
	getJSON(t, ts, "/api/worktree-files?offset=9999", &far)
	getJSON(t, ts, "/api/worktree-files?q=f&offset=400", &ranked)
	if len(p0.Files) != 200 || p0.Next != 200 || p0.Files[0].Path != "f000.txt" {
		t.Fatalf("page 0 = %d rows next %d first %v", len(p0.Files), p0.Next, p0.Files[0])
	}
	if len(p1.Files) != 200 || p1.Next != 400 || p1.Files[0].Path != "f200.txt" {
		t.Fatalf("page 1 = %d rows next %d", len(p1.Files), p1.Next)
	}
	if len(p2.Files) != 50 || p2.Next != 0 || p2.Files[49].Path != "f449.txt" {
		t.Fatalf("page 2 = %d rows next %d", len(p2.Files), p2.Next)
	}
	if len(far.Files) != 0 || far.Next != 0 {
		t.Fatalf("past the end = %d rows next %d, want none", len(far.Files), far.Next)
	}
	if p0.Gen != p1.Gen || p1.Gen != p2.Gen {
		t.Fatalf("gens %d/%d/%d: pages of one read must share it", p0.Gen, p1.Gen, p2.Gen)
	}
	if len(ranked.Files) != 200 || !ranked.Limited || ranked.Next != 0 {
		t.Fatalf("ranked = %d rows limited %v next %d, want 200/true/0 (offset ignored)", len(ranked.Files), ranked.Limited, ranked.Next)
	}
	for _, bad := range []string{"abc", "-1"} {
		if code := getJSON(t, ts, "/api/worktree-files?offset="+bad, nil); code != http.StatusBadRequest {
			t.Errorf("offset=%s: status %d, want 400", bad, code)
		}
	}
}

// The list is read once per F open (fresh=1); without it the cache answers,
// so a file created since is not listed until the next fresh read.
func TestWorktreeFilesCacheAndFresh(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	var first, cached, fresh wtFilesResp
	getJSON(t, ts, "/api/worktree-files?fresh=1", &first)
	writeFiles(t, dir, "new.txt")
	getJSON(t, ts, "/api/worktree-files", &cached)
	getJSON(t, ts, "/api/worktree-files?fresh=1", &fresh)
	if cached.Gen != first.Gen || cached.rows() != "f.txt" {
		t.Fatalf("cached = %q gen %d, want the first read (gen %d)", cached.rows(), cached.Gen, first.Gen)
	}
	if fresh.Gen == first.Gen || fresh.rows() != "f.txt new.txt(u)" {
		t.Fatalf("fresh = %q gen %d, want a re-read listing new.txt", fresh.rows(), fresh.Gen)
	}
}
```

- [ ] **Step 6: Run them — expect 404s**

Run: `go test ./internal/web/ -run WorktreeFiles`
Expected: FAIL — `status = 404, want 200` (and the others fail on empty rows).

- [ ] **Step 7: Write the handler** — `internal/web/worktreefiles.go`:

```go
package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/fuzzy"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/worktree-files", s.handleWorktreeFiles)
	})
}

// F on the web (plan 5d): the files pane lists every file in the working
// tree. The list can be huge (gg's target repos hold 100k+ paths), so the
// page never holds it: this endpoint ranks a query here (the best
// wtFilesPage) or hands out the sorted list a page at a time.

// wtFilesPage is the most rows one answer carries: the ranked cap (the TUI's
// fileFinderLimit) and the page an unfiltered list scrolls by.
const wtFilesPage = 200

type wtFileRow struct {
	Path      string `json:"path"`
	Untracked bool   `json:"untracked,omitempty"`
}

// wtFilesBody is one answer: the rows, the whole list's size, the offset of
// the next unfiltered page (0 = the last), whether a query hit the cap, and
// the read the rows came from — a page whose gen differs from its first
// page's was cut from a newer read, and the page starts over.
type wtFilesBody struct {
	Files   []wtFileRow `json:"files"`
	Total   int         `json:"total"`
	Next    int         `json:"next,omitempty"`
	Limited bool        `json:"limited,omitempty"`
	Gen     int         `json:"gen"`
}

// wtFilesList is one read of the working tree's files, for one checkout.
type wtFilesList struct {
	root      string
	gen       int
	paths     []string
	untracked map[string]bool
}

// The cache sits beside the Server (searchState's pattern): one read per
// Server, replaced by every fresh read or a re-root. Losing it costs a
// re-read, never a wrong answer.
var (
	wtFilesMu  sync.Mutex
	wtFilesOf  = map[*Server]*wtFilesList{}
	wtFilesGen int
)

func (s *Server) handleWorktreeFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset := 0
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeErr(w, http.StatusBadRequest, errors.New("invalid offset"))
			return
		}
		offset = n
	}
	l, err := s.worktreeFiles(readCtx(r), q.Get("fresh") == "1")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	body := wtFilesBody{Files: []wtFileRow{}, Total: len(l.paths), Gen: l.gen}
	if query := q.Get("q"); query != "" {
		for _, m := range fuzzy.Rank(query, l.paths, wtFilesPage) {
			body.Files = append(body.Files, wtFileRow{Path: m.S, Untracked: l.untracked[m.S]})
		}
		body.Limited = len(body.Files) == wtFilesPage
	} else {
		from := min(offset, len(l.paths))
		to := min(from+wtFilesPage, len(l.paths))
		for _, p := range l.paths[from:to] {
			body.Files = append(body.Files, wtFileRow{Path: p, Untracked: l.untracked[p]})
		}
		if to < len(l.paths) {
			body.Next = to
		}
	}
	writeJSON(w, body)
}

// worktreeFiles returns the cached read, or reads the list anew (fresh, no
// read yet, or the served checkout changed): ls-files and the status, the
// TUI's rule (domain.WorktreeFileList).
func (s *Server) worktreeFiles(ctx context.Context, fresh bool) (*wtFilesList, error) {
	svc := s.service()
	root := svc.Root()
	wtFilesMu.Lock()
	l := wtFilesOf[s]
	wtFilesMu.Unlock()
	if !fresh && l != nil && l.root == root {
		return l, nil
	}
	tracked, err := svc.LsFiles(ctx)
	if err != nil {
		return nil, err
	}
	st, err := svc.Status(ctx)
	if err != nil {
		return nil, err
	}
	paths, untracked := domain.WorktreeFileList(tracked, st)
	wtFilesMu.Lock()
	defer wtFilesMu.Unlock()
	wtFilesGen++
	l = &wtFilesList{root: root, gen: wtFilesGen, paths: paths, untracked: untracked}
	wtFilesOf[s] = l
	return l, nil
}
```

- [ ] **Step 8: Run them — expect PASS**

Run: `go test ./internal/web/ -run WorktreeFiles`
Expected: PASS (4 tests).

- [ ] **Step 9: Delete `/api/files`** — in `internal/web/search.go`: delete the `init` route registration (lines 16–20: no other route lives there — `grep -n RegisterRoutes internal/web/search.go` must show only that one; if another route shares the block, delete only the `/api/files` line), the doc comment's second bullet (`the tracked-path list behind the fuzzy file finder…`) and its "two server-side halves" wording → "the server-side half", `fileFinderLimit` + its comment, `handleFiles`, `trackedFiles`, and the `filesHead`/`files` fields of `searchState` (its comment: "the scope its live feed was last walked under"). Drop the imports `go build` names unused (`strconv`, `fuzzy`, maybe `context`). In `internal/web/search_test.go`: delete `filesResp`, `TestFilesEndpoint`, `TestFilesEndpointLimits`, `TestFilesCacheFollowsHead` and imports `go vet` names unused.

- [ ] **Step 10: Run the web package**

Run: `go build ./... && go vet ./internal/web/ ./internal/domain/ ./internal/tui/ && go test ./internal/web/`
Expected: PASS (the page's `search.js` still calls `/api/files` until Task 2 — no Go test reaches it).

- [ ] **Step 11: Commit**

```bash
git add internal/domain/worktreefiles.go internal/domain/worktreefiles_test.go internal/tui/files_worktree.go internal/tui/files_worktree_test.go internal/web/worktreefiles.go internal/web/worktreefiles_test.go internal/web/search.go internal/web/search_test.go
git commit -m "feat(web): GET /api/worktree-files — F's list on the server (5d)"
```

Task-done test command: `go build ./... && go test ./internal/web/ ./internal/domain/ && go test ./internal/tui/ -run 'Worktree|Finder'`

---

### Task 2: the page — F takes the files pane (`wtfinder.js`), the old finder goes

**Files:**
- Create: `internal/web/static/wtfinder.js`, `internal/web/wtfinderjs_test.go`
- Modify: `internal/web/static/search.js:173-326` (delete the finder), `internal/web/static/palette.js:23,397`, `internal/web/static/app.js` (import), `internal/web/static/index.html:161` (footer chip), `internal/web/static/style.css` (append)

**Interfaces — Consumes:** Task 1's `GET /api/worktree-files`. `pushLayer/closeLayer/topLayer/pushFoot/popFoot/footOwned` (`layers.js`), `elidePath/charWidth/esc/getJSON` (`core.js`), `opLine` (`ops.js`), `isSwitcherKey/openSwitcher` (`openfiles.js`), `registerHelp` (`menus.js`).
**Produces (exports):** `openFinder()`, `closeFinder()`, `finderOn()`. Module-internal, used by Tasks 3–4: `wtf` (state), `selected()`, `cursorMoved()` (a no-op hook in this task), `finderKey(e)`, `menuAtCursor()` (a no-op in this task), `backgroundRow()` (a no-op in this task).

- [ ] **Step 1: Write the failing JS tests** — `internal/web/wtfinderjs_test.go`:

```go
package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wtfPureStart = "// --- finder model (pure; guarded against Go) ---"
const wtfPureEnd = "// --- end finder model ---"

func TestFinderModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "wtfinder.js", wtfPureStart, wtfPureEnd, `
const r = [];
r.push(wtTitle(true, "", 0, 0), wtTitle(false, "", 3, 10), wtTitle(false, "ab", 3, 10));
r.push(wtClamp(5, 0), wtClamp(-1, 3), wtClamp(9, 3), wtClamp(1, 3));
r.push(wtWantMore(190, 200, 200, 15), wtWantMore(100, 200, 200, 15), wtWantMore(199, 200, 0, 15), wtWantMore(0, 0, 200, 15));
r.push(wtPathCols(40, false), wtPathCols(40, true), wtPathCols(10, true), wtPathCols(0, false), wtPathCols(5, false), wtPathCols(3, false));
r.push(wtEmpty(""), wtEmpty("x"));
console.log(r.join("|"));
`)
	want := "Files (working tree)  (loading…)|Files (working tree)  10/10|Files (working tree)  3/10|" +
		"0|0|2|1|true|false|false|false|40|27|0|0|5|0|(no files)|(no match)"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func readStatic(t *testing.T, n string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("static", n))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFinderJSIsWired(t *testing.T) {
	t.Parallel()
	for _, c := range finderWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
	for _, c := range finderGone {
		if strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s still has %q: %s", c.file, c.want, c.why)
		}
	}
}

// The chip names ctrl+\ — a bare "\ " in the source would be an escaped
// space and the chip would read "ctrl+ open files" (the 5c bug).
func TestFinderFootChipKeepsTheBackslash(t *testing.T) {
	t.Parallel()
	if !strings.Contains(readStatic(t, "wtfinder.js"), `ctrl+\\ open files`) {
		t.Fatal(`wtfinder.js: the foot chip must spell ctrl+\\ (escaped) — "\ " is an escaped space`)
	}
}

// finderWiring grows task by task: each entry pins code that exists only once
// the step it names is done.
var finderWiring = []struct{ file, want, why string }{
	// Task 2: the list.
	{"app.js", "./wtfinder.js", "the module must be imported at boot"},
	{"wtfinder.js", `pushLayer("wtf"`, "F rides the layer stack (owns the keyboard, surfaces over it return to it)"},
	{"wtfinder.js", `classList.add("wtf")`, "F shows itself in the panes by a class on #panes"},
	{"wtfinder.js", "/api/worktree-files", "the list comes from the server"},
	{"wtfinder.js", "fresh", "F re-reads the list on open"},
	{"wtfinder.js", "elidePath(", "rows cut the path in the middle"},
	{"wtfinder.js", `pushFoot("wtf"`, "F's keys go in the bottom bar"},
	{"wtfinder.js", `closest("input,textarea")`, "typing F in a field stays a letter"},
	{"wtfinder.js", "registerHelp(", "the ? overlay lists F"},
	{"palette.js", `from "./wtfinder.js"`, "the palette row opens the new F"},
	{"index.html", `data-act="finder"`, "the main footer advertises F"},
	{"style.css", "#panes.wtf.wtf.wtf", "F's grid outranks every layout rule"},
	{"style.css", "#wtf.hidden", "the list hides by id (no global .hidden)"},
	{"style.css", "#wtf-preview.hidden", "the preview hides by id"},
}

// finderGone pins what 5d removes.
var finderGone = []struct{ file, want, why string }{
	{"search.js", `mountOverlay("finder")`, "the overlay finder is replaced by F (the TUI deleted its popup)"},
	{"search.js", "/api/files", "the endpoint is gone"},
	{"search.js", `e.key === "F"`, "F belongs to wtfinder.js"},
	{"palette.js", "openFeedFilter, openFinder", "openFinder no longer comes from search.js"},
}
```

- [ ] **Step 2: Run them — expect failures**

Run: `go test ./internal/web/ -run 'Finder'`
Expected: FAIL — `node: … no such file` for the model test (wtfinder.js missing — `runPureJS` fatals on the read), the wiring test lists every row.

- [ ] **Step 3: Write `static/wtfinder.js`:**

```js
// wtfinder.js — F: the files pane lists every file in the working tree and
// the diff pane previews the one under the cursor (open files on the web,
// plan 5d; the TUI's F window). The list lives on the server
// (/api/worktree-files ranks a query and pages the rest) — gg's repos hold
// 100k+ paths, which a page must never hold.
//
// F is a LAYER: it owns the keyboard, so every surface it opens (the viewer,
// history, blame, the . menu, the ctrl+\ switcher) sits over it and esc lands
// back here. It DRAWS in the panes: a "wtf" class on #panes shows #wtf in the
// files pane and #wtf-preview in the diff pane over whatever stage is up, and
// hides the panes' own children without touching them — esc restores the
// stage exactly as it was.
import { $, charWidth, elidePath, esc, getJSON } from "./core.js";
import { closeLayer, footOwned, popFoot, pushFoot, pushLayer, topLayer } from "./layers.js";
import { opLine } from "./ops.js";
import { isSwitcherKey, openSwitcher } from "./openfiles.js";
import { registerHelp } from "./menus.js";

// --- finder model (pure; guarded against Go) ---
const WT_UNTRACKED = "  (untracked)";

// wtTitle is the list's title: what it shows and how many (the TUI's words).
function wtTitle(loading, query, shown, total) {
  if (loading) return "Files (working tree)  (loading…)";
  return "Files (working tree)  " + (query ? shown : total) + "/" + total;
}

function wtClamp(sel, n) {
  if (n <= 0) return 0;
  return Math.min(Math.max(sel, 0), n - 1);
}

// wtWantMore: an unfiltered list asks for its next page once the cursor is
// within a screen of the rows it has.
function wtWantMore(sel, n, next, page) {
  return next > 0 && n > 0 && sel >= n - Math.max(1, page);
}

// wtPathCols is the path's column budget on a row of cols columns — less the
// "(untracked)" mark; 0 = render the path whole (too narrow to cut usefully).
function wtPathCols(cols, untracked) {
  if (cols <= 0) return 0;
  const c = untracked ? cols - WT_UNTRACKED.length : cols;
  return c >= 4 ? c : 0;
}

function wtEmpty(query) {
  return query ? "(no match)" : "(no files)";
}
// --- end finder model ---

// wtf is the finder while it is up: its rows (one page per unfiltered load,
// or the ranked matches), the cursor, the query, the next page's offset and
// the server read the pages were cut from.
const wtf = { on: false, loading: false, rows: [], sel: 0, query: "", next: 0, gen: 0, total: 0, paging: false };
let reqSeq = 0; // bumped by every list request: a later one wins
let queryTimer = null;

// The markup is built at import, inside the two panes it takes over.
const root = document.createElement("div");
root.id = "wtf";
root.className = "hidden";
root.innerHTML =
  `<div id="wtf-title"></div>` +
  `<div id="wtf-search" class="hidden"><span>/</span><input id="wtf-input" type="text" autocomplete="off" spellcheck="false"></div>` +
  `<ul id="wtf-list"></ul>`;
$("files-pane").append(root);
const preview = document.createElement("div");
preview.id = "wtf-preview";
preview.className = "hidden";
preview.innerHTML = `<div id="wtf-ptitle"></div><div id="wtf-body" tabindex="-1"></div>`;
$("diff-pane").append(preview);

function finderOn() {
  return wtf.on;
}

// openFinder takes the files pane (F). Nothing when a layer is up — F is
// opened from the page, never from under another surface.
function openFinder() {
  if (wtf.on || topLayer()) return;
  Object.assign(wtf, { on: true, loading: true, rows: [], sel: 0, query: "", next: 0, gen: 0, total: 0, paging: false });
  $("wtf-input").value = "";
  $("wtf-search").classList.add("hidden");
  $("panes").classList.add("wtf");
  preview.classList.remove("hidden");
  pushLayer("wtf", root, { onKey: finderKey });
  pushFoot("wtf", WTF_FOOT);
  render();
  load({ fresh: true });
}

// closeFinder gives the panes back: dropping the class is the whole restore.
function closeFinder() {
  if (!wtf.on) return;
  wtf.on = false;
  reqSeq++; // a list request in flight must not repaint
  clearTimeout(queryTimer);
  $("wtf-input").blur();
  $("panes").classList.remove("wtf");
  preview.classList.add("hidden");
  closeLayer("wtf");
  popFoot("wtf");
  closedHook();
}

// load fetches the list: a ranked query, or the unfiltered list from offset.
// A failed first read closes F and says why (the TUI's "file finder: …").
async function load({ fresh = false, offset = 0 } = {}) {
  const seq = ++reqSeq;
  const q = new URLSearchParams({ q: wtf.query });
  if (fresh) q.set("fresh", "1");
  if (offset) q.set("offset", String(offset));
  let body;
  try {
    body = await getJSON("/api/worktree-files?" + q);
  } catch (e) {
    if (seq !== reqSeq || !wtf.on) return;
    wtf.paging = false;
    if (offset) return; // a failed page leaves the rows shown; the next scroll retries
    closeFinder();
    opLine("file finder: " + (e.message || e), true);
    return;
  }
  if (seq !== reqSeq || !wtf.on) return;
  wtf.paging = false;
  if (offset && body.gen !== wtf.gen) return load(); // another tab re-read the list under this scroll: start over
  const files = body.files || [];
  Object.assign(wtf, { loading: false, total: body.total || 0, next: body.next || 0, gen: body.gen || 0 });
  if (offset) {
    wtf.rows = wtf.rows.concat(files);
    render();
    return;
  }
  wtf.rows = files;
  wtf.sel = 0;
  render();
  cursorMoved();
}

function loadMore() {
  if (wtf.query || !wtf.next || wtf.paging) return;
  wtf.paging = true;
  load({ offset: wtf.next });
}

function setQuery(q) {
  clearTimeout(queryTimer);
  if (q === wtf.query) return;
  wtf.query = q;
  wtf.next = 0;
  wtf.paging = false;
  if ($("wtf-input").value !== q) $("wtf-input").value = q;
  paintSearch();
  load();
}

function paintSearch() {
  const typing = document.activeElement === $("wtf-input");
  $("wtf-search").classList.toggle("hidden", !typing && !wtf.query);
}

// rowCols is a row's width in columns (the files list's measure).
function rowCols() {
  const px = $("wtf-list").clientWidth;
  if (px <= 0) return 0;
  return Math.floor((px - 16) / charWidth()) - 1;
}

function render() {
  $("wtf-title").textContent = wtTitle(wtf.loading, wtf.query, wtf.rows.length, wtf.total);
  paintSearch();
  const list = $("wtf-list");
  if (wtf.loading) {
    list.innerHTML = `<li class="empty">(loading…)</li>`;
    return;
  }
  if (!wtf.rows.length) {
    list.innerHTML = `<li class="empty">${wtEmpty(wtf.query)}</li>`;
    return;
  }
  const cols = rowCols();
  list.innerHTML = wtf.rows
    .map((f, i) => {
      const pc = wtPathCols(cols, !!f.untracked);
      return (
        `<li data-i="${i}"${i === wtf.sel ? ' class="sel"' : ""} title="${esc(f.path)}">` +
        esc(pc ? elidePath(f.path, pc) : f.path) +
        (f.untracked ? `<span class="wtf-un">${WT_UNTRACKED}</span>` : "") +
        `</li>`
      );
    })
    .join("");
  paintSel();
}

function paintSel() {
  for (const el of $("wtf-list").querySelectorAll("li.sel")) el.classList.remove("sel");
  const el = $("wtf-list").querySelector(`li[data-i="${wtf.sel}"]`);
  if (el) {
    el.classList.add("sel");
    el.scrollIntoView({ block: "nearest" });
  }
}

function pageRows() {
  const row = $("wtf-list").querySelector("li");
  return row ? Math.max(1, Math.floor($("wtf-list").clientHeight / row.offsetHeight) - 1) : 10;
}

function selected() {
  return wtf.loading ? null : wtf.rows[wtf.sel] || null;
}

function move(delta) {
  if (!wtf.rows.length) return;
  wtf.sel = wtClamp(wtf.sel + delta, wtf.rows.length);
  paintSel();
  if (wtWantMore(wtf.sel, wtf.rows.length, wtf.next, pageRows())) loadMore();
  cursorMoved();
}

// Task 3 fills these: the preview follows the cursor; closing drops it.
function cursorMoved() {}
function closedHook() {}
// Task 4 fills these: the actions and the background open.
function menuAtCursor() {}
function backgroundRow() {}

function openSearch() {
  $("wtf-search").classList.remove("hidden");
  $("wtf-input").focus();
}

function leaveSearch() {
  $("wtf-input").blur();
  paintSearch();
}

// inputKey: a key typed into the filter field. Typing filters (debounced by
// the input listener); ↑↓ still move; enter keeps the query, esc clears it.
function inputKey(e) {
  switch (e.key) {
    case "Escape":
      e.preventDefault();
      leaveSearch();
      setQuery("");
      return true;
    case "Enter":
      e.preventDefault();
      setQuery($("wtf-input").value);
      leaveSearch();
      return true;
    case "ArrowDown":
      e.preventDefault();
      move(1);
      return true;
    case "ArrowUp":
      e.preventDefault();
      move(-1);
      return true;
  }
  return true; // the field's own letter
}

// finderKey: F owns the keyboard. Keys with ctrl/meta/alt that F does not
// use are swallowed WITHOUT preventDefault (the browser's own keys pass);
// every other unused key is swallowed too — nothing reaches the page below.
function finderKey(e) {
  if (e.target === $("wtf-input")) return inputKey(e);
  if (e.ctrlKey && !e.altKey && !e.metaKey && (e.code === "BracketRight" || e.key === "]")) {
    e.preventDefault();
    backgroundRow();
    return true;
  }
  if (isSwitcherKey(e)) {
    e.preventDefault();
    openSwitcher();
    return true;
  }
  if (e.ctrlKey || e.metaKey || e.altKey) return true;
  switch (e.key) {
    case "ArrowDown": case "j": move(1); break;
    case "ArrowUp": case "k": move(-1); break;
    case "PageDown": move(pageRows()); break;
    case "PageUp": move(-pageRows()); break;
    case "Home": move(-wtf.rows.length); break;
    case "End": move(wtf.rows.length); break;
    case "/": openSearch(); break;
    case "Enter": case ".": menuAtCursor(); break;
    case "Escape":
      if (wtf.query) setQuery("");
      else closeFinder();
      break;
    default: return true;
  }
  e.preventDefault();
  return true;
}

$("wtf-input").addEventListener("input", () => {
  clearTimeout(queryTimer);
  const q = $("wtf-input").value;
  queryTimer = setTimeout(() => setQuery(q), 120);
});
$("wtf-input").addEventListener("blur", paintSearch);

$("wtf-list").addEventListener("scroll", () => {
  const l = $("wtf-list");
  if (l.scrollTop + l.clientHeight >= l.scrollHeight - 40) loadMore();
});

// The bottom bar while F is up. ctrl+\\ is escaped: "\ " is a space.
const WTF_FOOT =
  `<span>↑↓ j k move</span><button data-wact="find">/ filter</button><button data-wact="menu">enter . actions</button>` +
  `<button data-wact="bg">ctrl+] background</button><button data-wact="files">ctrl+\\ open files</button><button data-wact="close">esc close</button>`;

$("foot").addEventListener("click", (e) => {
  const main = e.target.closest('button[data-act="finder"]');
  if (main) return openFinder();
  const b = e.target.closest("button[data-wact]");
  if (!b || !wtf.on) return;
  switch (b.dataset.wact) {
    case "find": openSearch(); break;
    case "menu": menuAtCursor(); break;
    case "bg": backgroundRow(); break;
    case "files": openSwitcher(); break;
    case "close": closeFinder(); break;
  }
});

// A layer closed from outside (another surface clearing the stack) must not
// leave the panes taken: the class follows the stack on every key.
document.addEventListener("keyup", () => {
  if (wtf.on && root.classList.contains("hidden")) closeFinder();
  else if (!wtf.on && footOwned("wtf")) popFoot("wtf");
});

// F from the page: the old finder's guards — a layer owns the keyboard, a
// focused field owns every key it can type.
document.addEventListener("keydown", (e) => {
  if (topLayer()) return;
  if (e.target.closest && e.target.closest("input,textarea")) return;
  if (e.key === "F" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    e.preventDefault();
    openFinder();
  }
});

registerHelp({
  key: "F",
  html:
    "<b>files in the working tree</b> — the file list shows every file on disk (untracked ones marked); " +
    "<b>/</b> filters fuzzily, the diff pane previews the file under the cursor, <b>enter</b> / <b>.</b> its actions " +
    "(view file, diff, history, blame, copy), <b>ctrl+]</b> opens it in the background, <b>esc</b> gives the panes back",
});

export { closeFinder, finderOn, openFinder };
```

- [ ] **Step 4: Delete the old finder** — in `internal/web/static/search.js`: delete from `// --- the fuzzy file finder ---` (line 173) through the `finder.addEventListener("click", …)` block (the line before `// --- keys ---`); in the keydown listener delete the `} else if (e.key === "F") { … openFinder(); }` arm; delete the `registerHelp({ key: "F", … })` line; the export becomes `export { clearFeedFilter, openFeedFilter };`; drop `mountOverlay` / `pushLayer` / `closeLayer` from its imports if nothing else in the file uses them (`grep -n "mountOverlay\|pushLayer\|closeLayer" static/search.js`).

- [ ] **Step 5: Palette, app, footer** —
  - `static/palette.js:23`: `import { openFeedFilter } from "./search.js";` and a new line `import { openFinder } from "./wtfinder.js";`; line 397: `{ label: "files in the working tree… (F)", act: () => openFinder() },`.
  - `static/app.js`: after `import "./openfiles.js";` add `import "./wtfinder.js";`.
  - `static/index.html:161`: before `<button data-act="openfiles">` add `<button data-act="finder">F files</button>`.

- [ ] **Step 6: The CSS** — append to `internal/web/static/style.css`:

```css
/* F (wtfinder.js): the files pane lists every file in the working tree and
   the diff pane previews the one under the cursor — over whatever stage is
   up (list / files / diff, sidebar or not, file list folded or not), which
   stays as it was and comes back on esc. Three classes outrank every layout
   rule above ((2,3,0) over #panes.solo #files-pane's (2,1,0)); the panes'
   own children hide by (3,1,0)+ so none of their display rules can win. */
#wtf.hidden, #wtf-preview.hidden { display: none; }
#panes.wtf.wtf.wtf { grid-template-columns: 1fr var(--rs-w) var(--files-w); }
#panes.wtf.wtf.wtf #branches-pane, #panes.wtf.wtf.wtf #rs-sidebar,
#panes.wtf.wtf.wtf #commits-pane, #panes.wtf.wtf.wtf #symleft-pane { display: none; }
#panes.wtf.wtf.wtf #diff-pane { display: block; order: 1; overflow: hidden; }
#panes.wtf.wtf.wtf #rs-detail { display: block; order: 2; }
#panes.wtf.wtf.wtf #files-pane { display: block; order: 3; overflow: hidden; border-left: 1px solid #3a4150; }
#panes.wtf.wtf.wtf #files-pane > :not(#wtf), #panes.wtf.wtf.wtf #diff-pane > :not(#wtf-preview) { display: none; }
#panes.wtf.wtf.wtf #files-pane > #wtf:not(.hidden), #panes.wtf.wtf.wtf #diff-pane > #wtf-preview:not(.hidden) {
  display: flex; flex-direction: column; height: 100%; min-height: 0;
}
#wtf-title, #wtf-ptitle { padding: 4px 8px; border-bottom: 1px solid var(--border); white-space: nowrap; overflow: hidden; }
#wtf-search { display: flex; gap: 4px; padding: 2px 8px; border-bottom: 1px solid var(--border); }
#wtf-search.hidden { display: none; }
#wtf-input { flex: 1; min-width: 0; background: var(--bg); color: var(--fg); border: none; outline: none; font: inherit; }
#wtf-list { list-style: none; flex: 1; min-height: 0; overflow: auto; }
#wtf-list li { padding: 2px 8px; cursor: pointer; white-space: nowrap; overflow: hidden; }
#wtf-list li.sel { background: var(--sel); }
#wtf-list li.empty { color: var(--dim); cursor: default; }
#wtf-list .wtf-un { color: var(--dim); white-space: pre; }
#wtf-body { flex: 1; min-height: 0; overflow: auto; font-family: ui-monospace, monospace; font-size: 12px; padding: 6px 0; }
#wtf-body:focus { outline: none; }
#wtf-body .notice { color: var(--dim); padding: 6px 12px; }
```

- [ ] **Step 7: Run the tests — expect PASS**

Run: `go test ./internal/web/ -run 'Finder|Viewer|Palette'`
Expected: PASS (`TestFinderModelJS`, `TestFinderJSIsWired`, `TestFinderFootChipKeepsTheBackslash`; the viewer wiring rows unchanged).

- [ ] **Step 8: Commit**

```bash
git add internal/web/static/wtfinder.js internal/web/wtfinderjs_test.go internal/web/static/search.js internal/web/static/palette.js internal/web/static/app.js internal/web/static/index.html internal/web/static/style.css
git commit -m "feat(web): F takes the files pane — every working-tree file, fuzzy (5d)"
```

Task-done test command: `go test ./internal/web/`

---

### Task 3: the preview — the diff pane follows the cursor

**Files:** Modify `internal/web/static/wtfinder.js` (the pure section, `cursorMoved`, `closedHook`), `internal/web/static/style.css:230-238` (syntax colours), `internal/web/wtfinderjs_test.go`.

**Interfaces — Consumes:** Task 2's `wtf`, `selected()`, the hooks. `renderCell(text, spans, toks, side, hits)` (`files.js`, exported), `GET /api/file-content?src=worktree&path=` → `{lines:[{text,tok}], missing?, too_large?}`.
**Produces:** `const WT_SETTLE_MS = 150`, `wtPreviewFresh(gen, curGen, path, curPath)` (pure), `wtPlaceholder(body, n)` (pure).

- [ ] **Step 1: Write the failing test** — add to `wtfinderjs_test.go`:

```go
func TestFinderPreviewModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "wtfinder.js", wtfPureStart, wtfPureEnd, `
const r = [];
r.push(WT_SETTLE_MS);
r.push(wtPreviewFresh(3, 3, "a", "a"), wtPreviewFresh(2, 3, "a", "a"), wtPreviewFresh(3, 3, "a", "b"));
r.push(wtPlaceholder({missing: true}, 0), wtPlaceholder({too_large: true}, 0), wtPlaceholder({}, 0), wtPlaceholder({}, 4) === "");
console.log(r.join("|"));
`)
	want := "150|true|false|false|(file deleted on disk)|(file too large to preview)|(empty file)|true"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
```

and to `finderWiring`:

```go
	// Task 3: the preview.
	{"wtfinder.js", "src=worktree&path=", "the preview reads the file on disk"},
	{"wtfinder.js", "renderCell(", "lines paint through the shared cell renderer (syntax colour)"},
	{"wtfinder.js", "setTimeout(() => showPreview(", "the preview waits for the cursor to settle"},
	{"style.css", "#wtf-body .tk-kw", "the preview is syntax-coloured"},
```

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/web/ -run 'Finder'`
Expected: FAIL — `WT_SETTLE_MS is not defined` (node), wiring rows missing.

- [ ] **Step 3: Implement** — in `wtfinder.js`'s pure section, after `wtEmpty`:

```js
// The cursor rests this long before the preview reads the file (the TUI's
// wtPreviewSettle): a held arrow over a slow mount reads only where it stops.
const WT_SETTLE_MS = 150;

// wtPreviewFresh: an answer paints only when no later move superseded it and
// the cursor still sits on its file.
function wtPreviewFresh(gen, curGen, path, curPath) {
  return gen === curGen && path === curPath;
}

// wtPlaceholder is what the preview shows instead of lines ("" = the lines).
function wtPlaceholder(body, n) {
  if (body.missing) return "(file deleted on disk)";
  if (body.too_large) return "(file too large to preview)";
  return n ? "" : "(empty file)";
}
```

Add `renderCell` to the imports: `import { renderCell } from "./files.js";`. Replace the Task 2 `cursorMoved` / `closedHook` stubs with:

```js
// The preview: the file under the cursor, read from disk once the cursor
// rests. NOT an open file (the TUI's rule): it never touches /api/open-files,
// so browsing never fills the ctrl+\ list.
let previewGen = 0;
let previewTimer = null;
let previewPath = ""; // the file the preview shows ("" = none)

function cursorMoved() {
  const gen = ++previewGen;
  clearTimeout(previewTimer);
  const f = selected();
  if (!f) {
    paintPreview("", [], "");
    return;
  }
  if (f.path === previewPath) return;
  previewTimer = setTimeout(() => showPreview(gen, f.path), WT_SETTLE_MS);
}

function closedHook() {
  previewGen++;
  clearTimeout(previewTimer);
  paintPreview("", [], "");
}

async function showPreview(gen, path) {
  let body;
  try {
    body = await getJSON("/api/file-content?src=worktree&path=" + encodeURIComponent(path));
  } catch (e) {
    if (wtf.on && wtPreviewFresh(gen, previewGen, path, (selected() || {}).path)) paintPreview(path, [], "(load failed: " + (e.message || e) + ")");
    return;
  }
  if (!wtf.on || !wtPreviewFresh(gen, previewGen, path, (selected() || {}).path)) return;
  const lines = body.lines || [];
  paintPreview(path, lines, wtPlaceholder(body, lines.length));
}

// paintPreview draws path's lines (or the placeholder) at the top; path ""
// empties the pane. The title cuts the PATH in the middle, never the name.
function paintPreview(path, lines, placeholder) {
  previewPath = path;
  const title = $("wtf-ptitle");
  const tail = " (working tree)";
  const cols = Math.floor((title.clientWidth - 16) / charWidth()) - tail.length;
  title.title = path;
  title.textContent = path ? (cols > 3 ? elidePath(path, cols) : path) + tail : "";
  const body = $("wtf-body");
  if (!path) body.innerHTML = "";
  else if (placeholder) body.innerHTML = `<div class="notice">${esc(placeholder)}</div>`;
  else
    body.innerHTML = lines
      .map((l, i) => `<div class="vline"><span class="vno">${i + 1}</span><span class="vtext">${renderCell(l.text, null, l.tok, "", null) || " "}</span></div>`)
      .join("");
  body.scrollTop = 0;
  body.scrollLeft = 0;
}
```

In `style.css` extend the nine syntax-colour rules (lines 230–238) with the preview body:

Run: `sed -i 's/#viewer-body \.tk-\([a-z]*\), /#viewer-body .tk-\1, #wtf-body .tk-\1, /' internal/web/static/style.css && grep -c '#wtf-body .tk-' internal/web/static/style.css`
Expected: `9`.

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/web/ -run 'Finder'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/wtfinder.js internal/web/static/style.css internal/web/wtfinderjs_test.go
git commit -m "feat(web): F previews the file under the cursor in the diff pane (5d)"
```

Task-done test command: `go test ./internal/web/`

---

### Task 4: the actions, the mouse, `ctrl+]`, and a navigate closes F

**Files:** Modify `internal/web/static/wtfinder.js` (pure `wtActions`, `menuAtCursor`, `backgroundRow`, mouse), `internal/web/static/viewer.js` (`openWorktreeFileDiff`, relabel), `internal/web/static/files.js:4268` (export `copyPathRows`), `internal/web/static/live.js` (`steerNavigate`), `internal/web/wtfinderjs_test.go`.

**Interfaces — Consumes:** Tasks 2–3. `openViewer({src,path})` (viewer.js), `openFileHistory(path, rev)` / `openFileBlame(path, rev)` (filehist.js), `showCtxMenu(rows, x, y)` / `copyText` (layers.js), `copyPathRows(path)` (files.js, exported here), `linkFor(repo, worktree, row, side, line)` / `copyFileLink(path, link)` (links.js), `postJSON` (core.js), `state` (core.js).
**Produces:** `wtActions(untracked) → string[]` (pure); `openWorktreeFileDiff(path)` exported from viewer.js.

- [ ] **Step 1: Write the failing tests** — add to `wtfinderjs_test.go`:

```go
func TestFinderActionsJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "wtfinder.js", wtfPureStart, wtfPureEnd, `
console.log(wtActions(false).join(",") + "|" + wtActions(true).join(","));
`)
	if want := "view,diff,history,blame,copy|view,copy"; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
```

and to `finderWiring`:

```go
	// Task 4: the actions.
	{"wtfinder.js", "showCtxMenu(", "enter / . / right-click open the actions menu"},
	{"wtfinder.js", `label: "view file"`, "view file opens the viewer over F"},
	{"wtfinder.js", "openFileHistory(f.path", "history opens over F"},
	{"wtfinder.js", "openFileBlame(f.path", "blame opens over F"},
	{"wtfinder.js", "openWorktreeFileDiff(", "diff shares the viewer's helper"},
	{"wtfinder.js", "copyPathRows(", "the copy rows are the file rows' own"},
	{"wtfinder.js", "copyFileLink(", "copy file link goes through the shared presence check"},
	{"wtfinder.js", `tab: ""`, "ctrl+] opens the row in the background (no tab shows it)"},
	{"wtfinder.js", "opened in the background", "ctrl+] says so in 5c's words"},
	{"wtfinder.js", `addEventListener("dblclick"`, "a double-click views the file"},
	{"wtfinder.js", `addEventListener("contextmenu"`, "right-click opens the actions"},
	{"files.js", "copyPathRows,", "the copy rows are shared, not copied"},
	{"viewer.js", "async function openWorktreeFileDiff(", "the diff lookup is shared by the viewer and F"},
	{"viewer.js", "diff (working tree changes)", "the row says what it opens"},
	{"live.js", "closeFinder();", "a steered navigate onto the panes closes F first"},
```

and to `finderGone`:

```go
	{"viewer.js", "diff (HEAD ↔ working tree)", "no /api/diff lane opens HEAD ↔ working tree"},
```

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/web/ -run 'Finder'`
Expected: FAIL — `wtActions is not defined`, the wiring rows missing.

- [ ] **Step 3: The shared diff helper** — in `static/viewer.js` replace `viewerDiffWorktree` with:

```js
// openWorktreeFileDiff opens a working-tree file's pending change the way a
// click on its row does: the unstaged change (index → disk), else the staged
// one (HEAD → index) — /api/diff has no HEAD ↔ working tree lane. A file
// with neither says so. Shared by the viewer's menu and F's (plan 5d).
async function openWorktreeFileDiff(path) {
  await openWorkingTree(0);
  let i = state.statusEntries.findIndex((f) => f.path === path && f.section !== "staged");
  if (i < 0) i = state.statusEntries.findIndex((f) => f.path === path && f.section === "staged");
  if (i < 0) return opLine(path + " has no changes in the working tree", false);
  await openFile(i);
}

// viewerDiffWorktree: the viewer steps back to the background first.
async function viewerDiffWorktree(path) {
  closeViewer("background");
  await openWorktreeFileDiff(path);
}
```

Relabel the viewer's row: `items.push({ label: "diff (working tree changes)", act: () => viewerDiffWorktree(view.path) });`. Export: add `openWorktreeFileDiff` to the viewer's export list. `grep -rn "HEAD ↔ working tree" internal/web/` must then show no page file (Go tests or help text naming the old label get the new one).

- [ ] **Step 4: Export the copy rows** — `static/files.js:4268`: add `copyPathRows,` to the export list (alphabetical position is not enforced; put it after `commitMetaLine,`).

- [ ] **Step 5: The actions** — in `wtfinder.js`'s pure section, after `wtPlaceholder`:

```js
// wtActions is the action list of a row (enter / . / right-click): the git
// rows only for a tracked file — an untracked one has no history (the TUI's
// worktreeFileRows).
function wtActions(untracked) {
  return untracked ? ["view", "copy"] : ["view", "diff", "history", "blame", "copy"];
}
```

Extend the imports:

```js
import { $, charWidth, elidePath, esc, getJSON, postJSON, state } from "./core.js";
import { closeLayer, footOwned, popFoot, pushFoot, pushLayer, showCtxMenu, topLayer } from "./layers.js";
import { copyPathRows, renderCell } from "./files.js";
import { copyFileLink, linkFor } from "./links.js";
import { openFileBlame, openFileHistory } from "./filehist.js";
import { openViewer, openWorktreeFileDiff } from "./viewer.js";
```

Replace the Task 2 `menuAtCursor` / `backgroundRow` stubs with:

```js
// menuRows is a row's actions. view file, history and blame open OVER F (esc
// returns here); diff closes F — the page's diff lives in the diff stage.
function menuRows(f) {
  const rows = [];
  for (const a of wtActions(!!f.untracked)) {
    if (a === "view") rows.push({ label: "view file", act: () => openViewer({ src: "worktree", path: f.path }) });
    if (a === "diff") rows.push({ label: "diff (working tree changes)", act: () => { closeFinder(); openWorktreeFileDiff(f.path); } });
    if (a === "history") rows.push({ label: "file history", act: () => openFileHistory(f.path, "") });
    if (a === "blame") rows.push({ label: "blame", act: () => openFileBlame(f.path, "") });
    if (a === "copy") {
      rows.push({ sep: true }, ...copyPathRows(f.path));
      const flink = linkFor(state.repo, state.worktree, { path: f.path, state: "unstaged", hint: { kind: "view", id: "content" } }, "new", 0);
      if (flink) rows.push({ label: "copy file link", act: () => copyFileLink(f.path, flink) });
    }
  }
  return rows;
}

function menuAtCursor() {
  const f = selected();
  if (!f) return;
  const el = $("wtf-list").querySelector(`li[data-i="${wtf.sel}"]`);
  const r = el ? el.getBoundingClientRect() : null;
  showCtxMenu(menuRows(f), r ? r.left + 24 : 80, r ? r.bottom : 80);
}

// backgroundRow opens the row's file in the background (5b's open with no
// tab — the op 5c's agent verb uses): it joins the ctrl+\ list, no tab shows
// it, F stays up.
async function backgroundRow() {
  const f = selected();
  if (!f) return;
  let ans;
  try {
    ans = await postJSON("/api/open-files", { op: "open", src: "worktree", rev: "", path: f.path, line: 0, tab: "" });
  } catch (e) {
    opLine("background failed: " + (e.message || e), true);
    return;
  }
  opLine(f.path + " opened in the background" + (ans.evicted ? " — closed " + ans.evicted + " (20 files open)" : ""), false);
}
```

The mouse — after the `scroll` listener:

```js
function rowAt(e) {
  const li = e.target.closest("li[data-i]");
  if (!li) return null;
  const i = Number(li.dataset.i);
  if (i !== wtf.sel) {
    wtf.sel = i;
    paintSel();
    cursorMoved();
  }
  return wtf.rows[i] || null;
}

$("wtf-list").addEventListener("click", (e) => {
  leaveSearch();
  rowAt(e);
});
$("wtf-list").addEventListener("dblclick", (e) => {
  const f = rowAt(e);
  if (f) openViewer({ src: "worktree", path: f.path });
});
$("wtf-list").addEventListener("contextmenu", (e) => {
  const f = rowAt(e);
  if (!f) return;
  e.preventDefault();
  showCtxMenu(menuRows(f), e.clientX, e.clientY);
});
```

- [ ] **Step 6: A navigate closes F** — in `static/live.js`: add `import { closeFinder } from "./wtfinder.js";` beside the viewer import, and in `steerNavigate` after the content-link line:

```js
async function steerNavigate(s) {
  if (s.hint_kind === "view" && s.hint_id === "content") return steerNavigateContent(s);
  closeFinder(); // the landing is on the panes F covers (the TUI's steerToPanels closes its files view)
  await steerNavigateLand(s);
  if (s.hint_kind) await revealHint(s);
}
```

- [ ] **Step 7: Run — expect PASS**

Run: `go test ./internal/web/ -run 'Finder|Viewer'`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/web/static/wtfinder.js internal/web/static/viewer.js internal/web/static/files.js internal/web/static/live.js internal/web/wtfinderjs_test.go
git commit -m "feat(web): F's actions, the mouse, ctrl+] background; a navigate closes F (5d)"
```

Task-done test command: `go test ./internal/web/`

---

### Task 5: the browser check, the docs

**Files:** Create (scratchpad, not committed) `$SP/check5d.mjs`, `$SP/run5d.sh`, `$SP/of5d-repo`. Modify `README.md` (the web viewer section), `CHANGELOG.md` (top), `docs/CLAUDE-details.md` (after the "Web agent verbs (5c…)" paragraph).

`SP=/tmp/claude-1000/-mnt-t-others-gigagit/ec40a872-e065-44da-b06d-332252b4bca8/scratchpad` (playwright under `$SP/node_modules`). If the session's scratchpad path differs, use the current one and copy `node_modules` (symlink) and `run5c.sh`'s pattern.

- [ ] **Step 1: The fixture** —

```bash
SP=<scratchpad>; rm -rf $SP/of5d-repo && mkdir -p $SP/of5d-repo && cd $SP/of5d-repo
git init -q -b main
seq 1 400 | sed 's/^/line /' > a.txt
mkdir -p dir/deep && printf 'package deep\n\nfunc B() int { return 1 }\n' > dir/deep/b.go
for i in $(seq -w 1 250); do echo "x$i" > "many$i.txt"; done
echo gone > gone.txt
git add -A && git commit -qm c1
echo more >> a.txt && git commit -qam c2
rm gone.txt                 # unstaged deletion: not listed
echo new > n.txt            # untracked: listed, marked
```

- [ ] **Step 2: The check script** — `$SP/check5d.mjs` (args: url repo gg shot), each step one `ok(name, cond, extra)` line, `vis(id)` = computed display ≠ none AND a non-zero bounding box (the 5c helper), plus `rowTexts()` = the `#wtf-list li` texts. Steps:

  - **a** `F` → `vis("wtf")`, `vis("wtf-preview")`, `!vis("commits-pane")`; `#wtf-title` = `Files (working tree)  254/254`; rows include `a.txt`, `n.txt  (untracked)` and no `gone.txt`; the foot text includes `ctrl+\ open files` and `esc close`.
  - **b** after 600 ms: `#wtf-ptitle` includes the first row's path and `(working tree)`; `#wtf-body .vline` count > 0.
  - **c** paging: `End` → rows > 200 within 3 s (the second page arrived).
  - **d** `Home`, `/`, type `deepb`, wait 500 ms → the first row is `dir/deep/b.go`, the title reads `1/254`; `Enter` → the field is blurred; after 600 ms the preview title includes `b.go` and `#wtf-body .tk-kw` exists (syntax colour).
  - **e** `Enter` → `vis("ctx-menu")`, the menu rows include `view file`, `diff (working tree changes)`, `file history`, `blame`, `copy path`, `copy file link`; `Escape` → the menu gone, `vis("wtf")`.
  - **f** `.` → `view file` click → `vis("viewer")`, `#viewer-title` includes `b.go`; `Escape` → `!vis("viewer")` and `vis("wtf")` (esc returned to F).
  - **g** `Escape` (clears the query) → the title `254/254`; select `n.txt` (type `/n.txt`, `Enter`), `Enter` → the menu lacks `file history`; `Escape`.
  - **h** `Control+BracketRight` → `#op-line` includes `n.txt opened in the background`; `Control+Backslash` → `vis("openfiles")`, a row `○ n.txt`; `Escape` → `vis("wtf")`.
  - **i** F over a diff: `Escape` twice (clear, close) → `vis("commits-pane")`, `!vis("wtf")`, the foot includes `F files`; click the first `.crow` (the working-tree row — the fixture is dirty), then the first `#files-list li[data-i]`, so `#diff-body table.diff` is visible; record `#diff-pane` scrollTop after scrolling 200 px and the `table.diff` column count; `F` → `!vis("diff-body")`, `vis("wtf")`; `Escape` → `vis("diff-body")`, same scrollTop, same column count.
  - **j** folded list: click `#files-min` (the list folds), `F` → `vis("wtf")` with width > 200 px; `Escape` → `#panes` has `nofiles` again; click `#files-min` to unfold.
  - **k** navigate: `F`, then `gg session navigate --rev $(git rev-parse HEAD) --file a.txt` (CLI, in the repo) → within 3 s `!vis("wtf")`; then `F`, `gg open --web gg://<repo>/a.txt?view=content` → `vis("viewer")`, `Escape` → `vis("wtf")`; `Escape`.
  - **l** guard: `\` (opens the feed filter bar), type `F` into it → `!vis("wtf")`; `Escape`.
  - **m** a vanished file: `F`, `/a.txt`, `Enter`; `rm a.txt` in the repo; move down and back up → the preview reads `(file deleted on disk)`.
  - End: screenshot, `console.log(res.join("\n"))`, `errors:` = the page errors (must be `[]`).

  `$SP/run5d.sh` = `run5c.sh` with `of5c` → `of5d`, `check5c.mjs` → `check5d.mjs`.

- [ ] **Step 3: Run it against the UNFIXED installed build**

Run: `bash $SP/run5d.sh ~/go/bin/gg unfixed`
Expected: step **a** FAILs (the old overlay finder opens instead; `#wtf` does not exist) and most later steps FAIL — proof the check can see its subject.

- [ ] **Step 4: Build the branch and run it**

Run: `go build -o $SP/gg-5d ./cmd/gg && bash $SP/run5d.sh $SP/gg-5d fixed`
Expected: every line `PASS`, `errors: []`. Look at the screenshot (`Read $SP/of5d-fixed.png`): the list on the right, the preview on the left, nothing else in either pane. Fix anything that fails (a defect found here gets a Go/JS test first when it is testable there) before the docs.

- [ ] **Step 5: The docs** —
  - `CHANGELOG.md`, above `## Open files on the web: agent verbs (5c)`:

```markdown
## Open files on the web: the F finder (5d)

### Added

- **`F` in `gg web` lists every file in the working tree** in the file list —
  tracked files minus deleted ones, plus untracked ones marked `(untracked)` —
  over whatever screen is up. `/` filters fuzzily (the best 200 matches;
  without a query the list pages in as you scroll), the diff pane previews
  the file under the cursor once it rests, and `enter` / `.` / right-click
  open its actions: **view file**, **diff**, **file history**, **blame** and
  the copy rows (path, absolute path, file name, file link); a double-click
  views the file. `ctrl+]` opens a row in the background (`ctrl+\` lists
  it), and `esc` gives the panes back exactly as they were. The footer's
  `F files` chip opens it too.

### Changed

- F replaces the page's old overlay finder (tracked files only, `enter`
  opened the file's history); `GET /api/files` is gone, `GET
  /api/worktree-files` serves the new list.
- The viewer's diff row is **diff (working tree changes)** and opens the
  staged change when the file has no unstaged one (it said "no changes").
```

  - `README.md`, the "The file viewer in `gg web`" section, after the paragraph ending `until it comes back.`:

```markdown
`F` turns the file list into every file in the working tree (untracked ones
marked `(untracked)`), over whatever screen you are on: `/` filters fuzzily,
the diff pane previews the file under the cursor, and `enter` / `.` (or
right-click) offers **view file**, **diff**, **file history**, **blame** and
the copy rows — a double-click views the file. `ctrl+]` opens the row in the
background; `esc` gives the panes back as they were.
```

  - `docs/CLAUDE-details.md`, a new paragraph after the "Web agent verbs (5c…)" one:

```markdown
**Web F finder (5d, 2026-09-27):** `GET /api/worktree-files` (`worktreefiles.go`) serves
F's list — `domain.WorktreeFileList` (moved from the TUI, which calls it too: ls-files minus
status `D` plus the status's untracked), read once per F open (`fresh=1`) and cached per
Server with a build number `gen`; a query ranks with `fuzzy.Rank` (cap 200, `limited`), no
query pages the sorted list 200 at a time (`offset`/`next`; a page whose `gen` moved restarts
the list). `/api/files` and its per-HEAD cache are gone. Page: `static/wtfinder.js` is a LAYER
(so the viewer, history, blame, the `.` menu and the switcher open over it and esc returns)
drawn IN the panes — `#panes.wtf` shows `#wtf` in the files pane and `#wtf-preview` in the
diff pane over any layout (three classes outrank the layout rules; the panes' own children
hide at (3,1,0)+) and touches nothing underneath, so esc restores the stage. The preview is
NOT an open file (it reads `/api/file-content` directly, 150 ms settle, gen + path drop stale
answers); `ctrl+]` = `/api/open-files` `open` with `tab ""`. diff closes F and opens
`openWorktreeFileDiff` (viewer.js — unstaged, else staged: `/api/diff` has no HEAD ↔ working
tree lane). A steered navigate onto the panes closes F (`live.js`); a content navigate opens
the viewer over it.
```

- [ ] **Step 6: Run the web tests, commit**

Run: `go test ./internal/web/`
Expected: PASS.

```bash
git add README.md CHANGELOG.md docs/CLAUDE-details.md
git commit -m "docs: F lists the working tree in gg web (5d)"
```

Task-done test command: `go test ./internal/web/`

---

After Task 5: the whole-branch self-review (executing-plans' Final Review, no subagent), `./test.sh` then `./test.sh race` (≈30 min, the tree untouched meanwhile), the verify binary, then ask before merging.
