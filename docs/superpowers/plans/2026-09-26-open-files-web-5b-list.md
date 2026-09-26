# Open files on the web — plan 5b: the shared open-files list — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline, THIS session — the repo forbids subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** the files a `gg web` viewer opens stay open in ONE list per worktree, owned by the server and shared by every tab: `ctrl+]` sends the viewer to the background, `ctrl+\` lists the open files and brings one back, and working-tree files follow the disk (edits reload in place, a deleted file shows `(file deleted on disk)` until it returns).

**Architecture:** A pure, mutex-guarded registry (`openfiles.go`) on the `Server` (not the live hub — the hub is replaced on every re-root and settings write), keyed by `svc.Root()`. `GET/POST /api/open-files` read and mutate it; every mutation is broadcast on the existing `/api/events` stream as `open_files` (the whole list). A tab names itself with a per-load id carried on its event stream (`?tab=`) and in every POST; the stream's lifetime is what "a tab shows a file" hangs on. A server-side stat poller (`openfiles_watch.go`: shown files every 1 s, background every 5 s, `internal/filewatch` only wakes it) emits `file_changed {id}`. On the page, `viewer.js` registers what it shows, `openfiles.js` is the `ctrl+\` switcher, and `live.js` routes the two new event reasons.

**Tech Stack:** Go 1.26 (`internal/web`), vanilla ES modules in `internal/web/static`, node for the pure-JS guards, playwright for the browser check.

**Spec:** `docs/superpowers/specs/2026-09-26-open-files-web-design.md` (plan 5b row; Architecture "Server" and "Page").

## Global Constraints

- Paths in rows/titles are cut in the MIDDLE with `elidePath`, never the file name.
- Key hints live in `#foot` (chips), never inside a box.
- Every wire value is validated → 400 via `writeErr`; a working-tree path must also be `filepath.IsLocal` (the poller stats it — `isGitArgSafe` alone lets `../` through, the 5a bug's twin).
- Protocol/op-line prose is English (the web is not localized); wording follows the TUI where the TUI has words: `closed %s (%d files open)`, `%s is in the background — ctrl+\ lists open files`.
- Cap 20 per worktree (the TUI's `maxOpenFiles`).
- New Go tests call `t.Parallel()` unless they touch the live hub / global state (`isolateGlobal(t)`).
- Pure JS sections run bare under node: no DOM, no imports between the markers.
- Browser checks ASSERT VISIBILITY (computed style / bounding box) and run against the UNFIXED installed build first. Kill test servers by PID.
- Every task-done runs the FULL web package: `go test ./internal/web/`.
- After the merge: `./build.sh install` and `./build.sh web`.

## Rulings (planning — the user reviews these)

| # | Ruling | Why | Cost if wrong |
|---|--------|-----|---------------|
| L1 | **Tab id**: one per page LOAD (`crypto.randomUUID()`, fallback a random string), a module constant in `core.js` — never storage (a duplicated tab copies sessionStorage and would be the same tab). Sent as `/api/events?tab=` and as `tab` in every POST. Valid = `^[A-Za-z0-9_-]{1,64}$`; a POST with a bad tab → 400, a stream with one simply tracks nothing | nothing names a tab today | a rename |
| L2 | **Shown = stream-scoped, refcounted.** The server counts each tab's live `/api/events` streams; `tabShows[tab]` (the ONE file a tab shows) is dropped when the count reaches 0, then `open_files` is broadcast. The page re-reports `shown` on EVERY hello. Ordering relied on: `handleEvents` counts the stream BEFORE writing the hello, and the page posts `shown` only after reading it — so a re-root/settings hub swap (old stream drops shown → new stream → hello → shown) restores it, and an old stream's late drop only decrements | the hub is replaced on re-root and settings writes, which ends every stream | a background→shown flicker |
| L3 | **Ops** (`POST /api/open-files`, write guard; every answer carries the fresh `files` list): `open {src,rev,path,line,tab}` — reuse by (src,rev,path) or create, move first, the tab shows it (`tab` "" = a background open, for 5c), a `line>0` is recorded; `focus {id,tab}` — the tab shows it, move first; `background {id,tab,line}` — the tab stops showing it; `close {id,tab,everywhere}` — see L4; `cursor {id,line}` — record the line, NO broadcast (the switcher GETs fresh on open); `shown {id,tab}` — the tab shows id ("" = nothing), no reorder. Unknown id → 404 `no open file <id>` | spec's op list | an op split |
| L4 | **esc vs x.** `esc`/backdrop in the viewer = `close` for this tab: the entry is removed unless ANOTHER tab shows it (then only this tab lets go). The switcher's `x` = `close everywhere`: removed; a tab whose viewer shows a vanished id closes it with the op line `<path> was closed in another tab`. The viewer's own hand-offs (the `.` menu's diff / history / blame) = `background`, not close | the TUI closes on esc; a shared list must not yank a file off another tab's screen on esc, but `x` is an explicit list edit | one flag |
| L5 | **Baseline timing:** the server stats a working-tree entry inside `open`/`focus` (before the page's content GET — the TUI's stat-before-read), so an edit racing the read shows on the next poll. `/api/file-content` is unchanged | 5a shipped no `disk{}`; the registry owns the baseline | none |
| L6 | **Gate:** `open_files` and `file_changed` go out through `fanOut` (never dropped). The poller SKIPS the whole round while an op is in flight — it neither stats nor advances a baseline — so an op's edit is caught by the first round after it | `emit` drops while an op runs; advancing the baseline there would lose the change | none |
| L7 | **Poll:** one goroutine per server (started in `Serve` after `startLive`, stopped by `Close`), 1 s tick; a working-tree entry shown by any tab is stat'ed every tick, a background one every 5 s; a filewatch event stats ALL working-tree entries now. filewatch is built only when `gitwatch.Supported(root)` (false on `/mnt` 9p) and never retried after a build failure. First stat of an entry only records. Missing↔present is a change | the TUI's `openFilesTick`/`applyDocStats` | none |
| L8 | **Landing line** (page, `pickLine`): a link's line wins; else where THIS tab left the file (its per-tab place: cursor + scroll top); else the server's last line; else line 1. The past-the-end notice only for a link's line | spec: "enter brings it back where this tab left it (else at the server's line)" | none |
| L9 | **Reload keeping the place:** `file_changed` for the shown id re-fetches content; cursor kept (clamped; KEPT unclamped while the deleted placeholder shows, for when the file returns) and scroll top kept. A load-sequence token: `openViewer` bumps it, a reload captures it and drops its result if it moved — so a pending link landing always wins | spec | none |
| L10 | **Footer stack:** `layers.js` gets `pushFoot(owner, html)` / `popFoot(owner)`; the viewer's `swapFoot` becomes a wrapper; the switcher pushes its own chips over the viewer's. The 5a single-slot swap would restore the WRONG chips when the switcher closes the viewer under it | nesting | none |
| L11 | **Eviction message:** the POST answer's `evicted` → op line `closed <path> (20 files open)` in the tab that caused it; the broadcast carries `evicted` too (5c's agent reply), other tabs ignore it. When every entry is shown by some tab nothing is evicted and the list exceeds 20 (the TUI does the same) | TUI | none |
| L12 | **Switcher row:** `●` = the file THIS tab's viewer shows, `○` otherwise (shown-by-another-tab is not marked), then the path (middle-cut), `:line` when > 0, the version (`working tree` / `@ sha7` / `shelf`). Keys ↑↓ j k, enter, x, esc; click = select + enter. Empty list → op line `no open files — view file on a file row opens one`. `ctrl+\` works from the viewer AND from the main page (before the form-field guard, like ctrl+k); `#foot` gets a `ctrl+\ open files` chip | spec | a visual tweak |

## Review Focus

1. **Hub replacement with a viewer open** (re-root, settings write): the file must read `shown` again after the reconnect (L2). Pinned in Task 2 (`TestOpenFilesStreamRefcount`) + the browser check re-root step.
2. **Two tabs racing `open` on the same (src,rev,path)** — one entry, both tabs show it; the mutex makes `open` atomic. Pinned in Task 1 (`TestOpenFilesReuseAcrossTabs`).
3. **An entry closed while the poller stats it** — `applyStat` on a gone id is a no-op, never a panic or a resurrected entry. Pinned in Task 1.
4. **`x` on the file this tab's viewer shows** — the viewer closes locally first (no "closed in another tab" line for its own tab), the switcher re-renders from the answer. Pinned in the browser check.
5. **A background file evicted while the switcher is open** — the switcher re-renders from the `open_files` event (never acts on a stale row: enter on an evicted id answers 404 → op line). Pinned by `TestOpenFilesFocusUnknown` + the switcher's event hook.

---

### Task 1: the registry core (`openfiles.go`)

**Files:** Create `internal/web/openfiles.go`, `internal/web/openfiles_test.go`.

**Interfaces — Produces:**
- `const maxOpenFiles = 20`
- `type ofKey struct{ Src, Rev, Path string }` (Src ∈ `worktree`|`commit`|`shelf`)
- `type diskStat struct{ size int64; mod time.Time; missing, known bool }`; `func statDisk(abs string) diskStat`; `func (a diskStat) same(b diskStat) bool`
- `type openFiles struct{ … }`; `func newOpenFiles() *openFiles`
- `func (r *openFiles) open(wt string, k ofKey, tab string, line int) (f steer.OpenFile, evicted string)`
- `func (r *openFiles) focus(wt, id, tab string) (steer.OpenFile, bool)`
- `func (r *openFiles) background(wt, id, tab string, line int) bool`
- `func (r *openFiles) close(wt, id, tab string, everywhere bool) bool` (false = unknown id)
- `func (r *openFiles) cursor(wt, id string, line int) bool`
- `func (r *openFiles) setShown(wt, tab, id string) bool` (false = unknown non-empty id)
- `func (r *openFiles) streamOpened(tab string)`; `func (r *openFiles) streamClosed(tab string) bool` (true = the tab showed something)
- `func (r *openFiles) list(wt string) []steer.OpenFile`
- `func (r *openFiles) setBaseline(wt, id string, d diskStat)`
- `type ofDue struct{ ID, Path string }`; `func (r *openFiles) due(wt string, now time.Time, all bool, bgEvery time.Duration) []ofDue`
- `func (r *openFiles) applyStat(wt, id string, d diskStat) bool` (true = changed)
- `func (r *openFiles) entryKey(wt, id string) (ofKey, bool)`

- [ ] **Step 1: failing tests** — `internal/web/openfiles_test.go`:

```go
package web

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func wtKey(p string) ofKey { return ofKey{Src: "worktree", Path: p} }

func ids(r *openFiles, wt string) string {
	s := ""
	for _, f := range r.list(wt) {
		s += f.ID + ":" + f.State + " "
	}
	return s
}

func TestOpenFilesOrderReuseAndState(t *testing.T) {
	t.Parallel()
	r := newOpenFiles()
	a, _ := r.open("/wt", wtKey("a.txt"), "t1", 0)
	b, _ := r.open("/wt", wtKey("b.txt"), "t1", 7) // t1 now shows b; a is background
	if a.ID != "f1" || b.ID != "f2" || b.Line != 7 {
		t.Fatalf("ids/line: %+v %+v", a, b)
	}
	if got := ids(r, "/wt"); got != "f2:shown f1:background " {
		t.Fatalf("order/state: %q", got)
	}
	again, _ := r.open("/wt", wtKey("a.txt"), "t1", 0) // reuse, move first
	if again.ID != "f1" || ids(r, "/wt") != "f1:shown f2:background " {
		t.Fatalf("reuse: %+v %q", again, ids(r, "/wt"))
	}
	if len(r.list("/other")) != 0 {
		t.Fatal("lists are per worktree")
	}
	c, _ := r.open("/wt", ofKey{Src: "commit", Rev: "abc", Path: "a.txt"}, "", 0)
	if c.ID != "f3" || c.State != "background" || c.Source != "commit" || c.Rev != "abc" {
		t.Fatalf("a version is its own entry; tab \"\" opens in the background: %+v", c)
	}
}

func TestOpenFilesReuseAcrossTabs(t *testing.T) {
	t.Parallel()
	r := newOpenFiles()
	x, _ := r.open("/wt", wtKey("a.txt"), "t1", 0)
	y, _ := r.open("/wt", wtKey("a.txt"), "t2", 0)
	if x.ID != y.ID || len(r.list("/wt")) != 1 {
		t.Fatalf("one entry per (src,rev,path): %+v %+v", x, y)
	}
	// t1 lets go (esc): t2 still shows it, so it stays.
	if !r.close("/wt", x.ID, "t1", false) || ids(r, "/wt") != "f1:shown " {
		t.Fatalf("close by one tab while another shows: %q", ids(r, "/wt"))
	}
	// t2 lets go too: removed.
	r.close("/wt", x.ID, "t2", false)
	if len(r.list("/wt")) != 0 {
		t.Fatalf("last tab's close removes: %q", ids(r, "/wt"))
	}
}

func TestOpenFilesCloseEverywhereAndUnknown(t *testing.T) {
	t.Parallel()
	r := newOpenFiles()
	f, _ := r.open("/wt", wtKey("a.txt"), "t1", 0)
	if !r.close("/wt", f.ID, "t2", true) || len(r.list("/wt")) != 0 {
		t.Fatal("everywhere removes even while another tab shows it")
	}
	if r.close("/wt", "f99", "t1", false) || r.background("/wt", "f99", "t1", 1) || r.cursor("/wt", "f99", 1) || r.setShown("/wt", "t1", "f99") {
		t.Fatal("an unknown id is refused")
	}
	if _, ok := r.focus("/wt", "f99", "t1"); ok {
		t.Fatal("focus on an unknown id")
	}
}

func TestOpenFilesBackgroundFocusCursorShown(t *testing.T) {
	t.Parallel()
	r := newOpenFiles()
	a, _ := r.open("/wt", wtKey("a.txt"), "t1", 0)
	r.open("/wt", wtKey("b.txt"), "", 0)
	if !r.background("/wt", a.ID, "t1", 12) || ids(r, "/wt") != "f2:background f1:background " {
		t.Fatalf("background keeps the order: %q", ids(r, "/wt"))
	}
	if r.list("/wt")[1].Line != 12 {
		t.Fatal("background records the line")
	}
	r.cursor("/wt", a.ID, 30)
	f, ok := r.focus("/wt", a.ID, "t1")
	if !ok || f.Line != 30 || ids(r, "/wt") != "f1:shown f2:background " {
		t.Fatalf("focus: %+v %q", f, ids(r, "/wt"))
	}
	if !r.setShown("/wt", "t1", "") || ids(r, "/wt") != "f1:background f2:background " {
		t.Fatalf("shown \"\" = the tab shows nothing: %q", ids(r, "/wt"))
	}
	r.setShown("/wt", "t1", "f2")
	if ids(r, "/wt") != "f1:background f2:shown " {
		t.Fatalf("shown does not reorder: %q", ids(r, "/wt"))
	}
}

func TestOpenFilesStreamDropsShown(t *testing.T) {
	t.Parallel()
	r := newOpenFiles()
	r.streamOpened("t1")
	r.streamOpened("t1") // a reconnect racing the old stream's end
	r.open("/wt", wtKey("a.txt"), "t1", 0)
	if r.streamClosed("t1") || ids(r, "/wt") != "f1:shown " {
		t.Fatalf("one of two streams ending keeps the tab: %q", ids(r, "/wt"))
	}
	if !r.streamClosed("t1") || ids(r, "/wt") != "f1:background " {
		t.Fatalf("the last stream ending drops what the tab showed: %q", ids(r, "/wt"))
	}
	if r.streamClosed("t1") {
		t.Fatal("an extra close is a no-op")
	}
}

func TestOpenFilesCapEvictsLeastRecentNotShown(t *testing.T) {
	t.Parallel()
	r := newOpenFiles()
	first, _ := r.open("/wt", wtKey("p0.txt"), "t9", 0) // t9 keeps showing p0
	for i := 1; i < maxOpenFiles; i++ {
		r.open("/wt", wtKey(fmt.Sprintf("p%d.txt", i)), "", 0)
	}
	f, ev := r.open("/wt", wtKey("new.txt"), "", 0)
	if ev != "p1.txt" || len(r.list("/wt")) != maxOpenFiles || f.Path != "new.txt" {
		t.Fatalf("evicted %q (want p1.txt: p0 is shown), len %d", ev, len(r.list("/wt")))
	}
	if _, ok := r.focus("/wt", first.ID, "t9"); !ok {
		t.Fatal("the shown file survived")
	}
	// Every entry shown: nothing can go, the list grows past the cap.
	r2 := newOpenFiles()
	for i := 0; i <= maxOpenFiles; i++ {
		_, ev := r2.open("/wt", wtKey(fmt.Sprintf("q%d.txt", i)), fmt.Sprintf("tab%d", i), 0)
		if ev != "" {
			t.Fatalf("evicted a shown file %q", ev)
		}
	}
	if len(r2.list("/wt")) != maxOpenFiles+1 {
		t.Fatalf("len %d", len(r2.list("/wt")))
	}
}

func TestOpenFilesDueAndApplyStat(t *testing.T) {
	t.Parallel()
	r := newOpenFiles()
	now := time.Unix(1000, 0)
	shown, _ := r.open("/wt", wtKey("s.txt"), "t1", 0)
	bg, _ := r.open("/wt", wtKey("b.txt"), "", 0)
	r.open("/wt", ofKey{Src: "commit", Rev: "abc", Path: "c.txt"}, "", 0) // never polled
	if d := r.due("/wt", now, false, 5*time.Second); len(d) != 2 {
		t.Fatalf("first round: every working-tree entry: %+v", d)
	}
	if d := r.due("/wt", now.Add(time.Second), false, 5*time.Second); len(d) != 1 || d[0].ID != shown.ID {
		t.Fatalf("1 s later only the shown one: %+v", d)
	}
	if d := r.due("/wt", now.Add(2*time.Second), true, 5*time.Second); len(d) != 2 {
		t.Fatalf("a wake polls all: %+v", d)
	}
	if d := r.due("/wt", now.Add(8*time.Second), false, 5*time.Second); len(d) != 2 {
		t.Fatalf("5 s after the last check the background one is due: %+v", d)
	}
	st := diskStat{size: 1, mod: now, known: true}
	if r.applyStat("/wt", bg.ID, st) {
		t.Fatal("the first stat only records")
	}
	if r.applyStat("/wt", bg.ID, st) || !r.applyStat("/wt", bg.ID, diskStat{missing: true, known: true}) {
		t.Fatal("same = no change; missing = change")
	}
	if !r.applyStat("/wt", bg.ID, st) {
		t.Fatal("back from missing = change")
	}
	if r.applyStat("/wt", bg.ID, diskStat{}) {
		t.Fatal("an unknown stat never reads as a change")
	}
	r.close("/wt", bg.ID, "", true)
	if r.applyStat("/wt", bg.ID, diskStat{size: 9, known: true}) || len(r.list("/wt")) != 2 {
		t.Fatal("a stat for a closed entry is dropped, never resurrects it")
	}
}

func TestStatDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	if st := statDisk(p); !st.known || !st.missing {
		t.Fatalf("absent: %+v", st)
	}
	os.WriteFile(p, []byte("abc"), 0o644)
	st := statDisk(p)
	if !st.known || st.missing || st.size != 3 {
		t.Fatalf("present: %+v", st)
	}
	if !st.same(statDisk(p)) || st.same(diskStat{missing: true, known: true}) {
		t.Fatal("same")
	}
}
```

- [ ] **Step 2: run** `go test ./internal/web/ -run 'OpenFiles|StatDisk'` — Expected: build failure (`newOpenFiles` undefined).

- [ ] **Step 3: implement** `internal/web/openfiles.go`:

```go
package web

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

// The open-files list (open files on the web, plan 5b): per worktree, the
// files a viewer opened, most recently shown first — ONE list shared by every
// tab of this gg web, which each tab reads and edits over /api/open-files and
// hears about on /api/events. A tab shows at most one file (its viewer); a
// file is "shown" while any tab with a live event stream shows it. The pure
// core below knows nothing of HTTP; openfiles_http.go and openfiles_watch.go
// drive it. Lives as long as the process (the TUI's openFilesReg).

// maxOpenFiles is how many files one worktree keeps open (the TUI's cap).
const maxOpenFiles = 20

// ofKey is an entry's identity: a version of a path. Src is "worktree",
// "commit" (Rev = the sha) or "shelf" (Rev = the entry id).
type ofKey struct{ Src, Rev, Path string }

// diskStat is what a poll compares: a working-tree file's size and mtime, or
// its absence. known is false for a stat never taken (the TUI's diskStat).
type diskStat struct {
	size    int64
	mod     time.Time
	missing bool
	known   bool
}

// statDisk stats abs. Any error other than "does not exist" reads as unknown
// — never as a deletion.
func statDisk(abs string) diskStat {
	fi, err := os.Stat(abs)
	switch {
	case err == nil:
		return diskStat{size: fi.Size(), mod: fi.ModTime(), known: true}
	case errors.Is(err, fs.ErrNotExist):
		return diskStat{missing: true, known: true}
	}
	return diskStat{}
}

func (a diskStat) same(b diskStat) bool {
	return a.missing == b.missing && a.size == b.size && a.mod.Equal(b.mod)
}

type ofEntry struct {
	id      string
	key     ofKey
	line    int
	disk    diskStat
	checked time.Time
}

type openFiles struct {
	mu       sync.Mutex
	seq      int
	byWT     map[string][]*ofEntry
	tabShows map[string]string // tab → the id its viewer shows
	streams  map[string]int    // tab → its live /api/events streams
}

func newOpenFiles() *openFiles {
	return &openFiles{byWT: map[string][]*ofEntry{}, tabShows: map[string]string{}, streams: map[string]int{}}
}

// shownLocked reports whether any tab shows id.
func (r *openFiles) shownLocked(id string) bool {
	for _, v := range r.tabShows {
		if v == id {
			return true
		}
	}
	return false
}

func (r *openFiles) findLocked(wt, id string) (int, *ofEntry) {
	for i, e := range r.byWT[wt] {
		if e.id == id {
			return i, e
		}
	}
	return -1, nil
}

func (r *openFiles) wireLocked(e *ofEntry) steer.OpenFile {
	st := "background"
	if r.shownLocked(e.id) {
		st = "shown"
	}
	return steer.OpenFile{ID: e.id, Path: e.key.Path, Source: e.key.Src, Rev: e.key.Rev, Line: e.line, State: st}
}

// frontLocked moves e first in wt's list (adding it when new) and, over the
// cap, drops the least recently shown entry no tab shows.
func (r *openFiles) frontLocked(wt string, e *ofEntry) (evicted string) {
	l := []*ofEntry{e}
	for _, o := range r.byWT[wt] {
		if o != e {
			l = append(l, o)
		}
	}
	if len(l) > maxOpenFiles {
		for i := len(l) - 1; i > 0; i-- {
			if !r.shownLocked(l[i].id) {
				evicted = l[i].key.Path
				l = append(l[:i], l[i+1:]...)
				break
			}
		}
	}
	r.byWT[wt] = l
	return evicted
}

// open reuses the entry for k or creates one, moves it first and, for a
// non-empty tab, makes it what that tab shows. line > 0 is recorded.
func (r *openFiles) open(wt string, k ofKey, tab string, line int) (steer.OpenFile, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var e *ofEntry
	for _, o := range r.byWT[wt] {
		if o.key == k {
			e = o
			break
		}
	}
	if e == nil {
		r.seq++
		e = &ofEntry{id: "f" + strconv.Itoa(r.seq), key: k}
	}
	if line > 0 {
		e.line = line
	}
	if tab != "" {
		r.tabShows[tab] = e.id
	}
	ev := r.frontLocked(wt, e)
	return r.wireLocked(e), ev
}

// focus makes id what tab shows and moves it first.
func (r *openFiles) focus(wt, id, tab string) (steer.OpenFile, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, e := r.findLocked(wt, id)
	if e == nil {
		return steer.OpenFile{}, false
	}
	if tab != "" {
		r.tabShows[tab] = id
	}
	r.frontLocked(wt, e) // e is already listed: nothing can be evicted
	return r.wireLocked(e), true
}

// background: tab no longer shows id; line > 0 is recorded.
func (r *openFiles) background(wt, id, tab string, line int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, e := r.findLocked(wt, id)
	if e == nil {
		return false
	}
	if line > 0 {
		e.line = line
	}
	if r.tabShows[tab] == id {
		delete(r.tabShows, tab)
	}
	return true
}

// close: tab lets go of id; the entry is removed unless another tab still
// shows it — or always, when everywhere (the switcher's x).
func (r *openFiles) close(wt, id, tab string, everywhere bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, e := r.findLocked(wt, id)
	if e == nil {
		return false
	}
	if r.tabShows[tab] == id {
		delete(r.tabShows, tab)
	}
	if !everywhere && r.shownLocked(id) {
		return true
	}
	for t, v := range r.tabShows {
		if v == id {
			delete(r.tabShows, t)
		}
	}
	r.byWT[wt] = append(r.byWT[wt][:i:i], r.byWT[wt][i+1:]...)
	return true
}

// cursor records the last line a tab reported.
func (r *openFiles) cursor(wt, id string, line int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, e := r.findLocked(wt, id)
	if e == nil {
		return false
	}
	if line > 0 {
		e.line = line
	}
	return true
}

// setShown records what tab shows ("" = nothing) without reordering — a
// tab's re-report after its event stream reconnected.
func (r *openFiles) setShown(wt, tab, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == "" {
		delete(r.tabShows, tab)
		return true
	}
	if _, e := r.findLocked(wt, id); e == nil {
		return false
	}
	r.tabShows[tab] = id
	return true
}

func (r *openFiles) streamOpened(tab string) {
	r.mu.Lock()
	r.streams[tab]++
	r.mu.Unlock()
}

// streamClosed ends one of tab's streams; the last one ending drops what the
// tab showed (true when it showed something).
func (r *openFiles) streamClosed(tab string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.streams[tab] == 0 {
		return false
	}
	r.streams[tab]--
	if r.streams[tab] > 0 {
		return false
	}
	delete(r.streams, tab)
	_, had := r.tabShows[tab]
	delete(r.tabShows, tab)
	return had
}

// list is wt's open files, most recently shown first, in the wire form.
func (r *openFiles) list(wt string) []steer.OpenFile {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []steer.OpenFile{}
	for _, e := range r.byWT[wt] {
		out = append(out, r.wireLocked(e))
	}
	return out
}

func (r *openFiles) entryKey(wt, id string) (ofKey, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, e := r.findLocked(wt, id); e != nil {
		return e.key, true
	}
	return ofKey{}, false
}

// setBaseline records d as id's disk state (stat-before-read on open).
func (r *openFiles) setBaseline(wt, id string, d diskStat) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, e := r.findLocked(wt, id); e != nil && d.known {
		e.disk = d
	}
}

type ofDue struct{ ID, Path string }

// due is wt's working-tree entries to stat now: a shown one every round, a
// background one every bgEvery — every one when all (a filewatch wake).
func (r *openFiles) due(wt string, now time.Time, all bool, bgEvery time.Duration) []ofDue {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ofDue
	for _, e := range r.byWT[wt] {
		if e.key.Src != "worktree" {
			continue
		}
		if !all && !r.shownLocked(e.id) && !e.checked.IsZero() && now.Sub(e.checked) < bgEvery {
			continue
		}
		e.checked = now
		out = append(out, ofDue{ID: e.id, Path: e.key.Path})
	}
	return out
}

// applyStat compares d with id's baseline: the first known stat only
// records; a different one is recorded and reported as a change.
func (r *openFiles) applyStat(wt, id string, d diskStat) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, e := r.findLocked(wt, id)
	if e == nil || !d.known {
		return false
	}
	if !e.disk.known {
		e.disk = d
		return false
	}
	if e.disk.same(d) {
		return false
	}
	e.disk = d
	return true
}
```

- [ ] **Step 4: run** `go test ./internal/web/ -run 'OpenFiles|StatDisk' -v 2>&1 | tail -20` — Expected: PASS.

- [ ] **Step 5: commit** — `git add internal/web/openfiles.go internal/web/openfiles_test.go && git commit -m "feat(web): the open-files registry core"`; task-done with `go test ./internal/web/`.

---

### Task 2: `/api/open-files`, the SSE events, the tab-scoped stream

**Files:** Create `internal/web/openfiles_http.go`, `internal/web/openfiles_http_test.go`; modify `internal/web/server.go` (field + `New`), `internal/web/live.go` (`liveMsg` fields; `handleEvents` tab hook).

**Interfaces — Consumes:** Task 1's registry. **Produces:**
- `Server.ofs *openFiles` (built in `New`)
- `liveMsg.Files []steer.OpenFile 'json:"files,omitempty"'`, `liveMsg.Evicted string 'json:"evicted,omitempty"'`, `liveMsg.FileID string 'json:"file_id,omitempty"'`; reasons `"open_files"`, `"file_changed"`
- `func validTab(s string) bool`
- `func (s *Server) broadcastOpenFiles(wt, evicted string)`; `func (s *Server) broadcastFileChanged(id string)`
- `GET /api/open-files` → `{files:[…]}`; `POST /api/open-files` (`ofReq`) → `ofAnswer{File *steer.OpenFile 'json:"file,omitempty"'; Evicted string 'json:"evicted,omitempty"'; Files []steer.OpenFile 'json:"files"'}`
- `func (s *Server) ofAbs(wt, path string) string` (working-tree absolute path)

- [ ] **Step 1: failing tests** — `internal/web/openfiles_http_test.go`:

```go
package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

type ofAns struct {
	File    *steer.OpenFile  `json:"file"`
	Evicted string           `json:"evicted"`
	Files   []steer.OpenFile `json:"files"`
}

func postOpenFiles(t *testing.T, ts *httptest.Server, body string) (int, ofAns) {
	t.Helper()
	var a ofAns
	code := postJSON(t, ts, "/api/open-files", body, "application/json", "", &a)
	return code, a
}

func TestOpenFilesHTTPOpenListClose(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	code, a := postOpenFiles(t, ts, `{"op":"open","src":"worktree","path":"f.txt","line":3,"tab":"t1"}`)
	if code != http.StatusOK || a.File == nil || a.File.ID != "f1" || a.File.State != "shown" || a.File.Line != 3 || len(a.Files) != 1 {
		t.Fatalf("open: %d %+v", code, a)
	}
	var l struct{ Files []steer.OpenFile }
	if getJSON(t, ts, "/api/open-files", &l); len(l.Files) != 1 || l.Files[0].Path != "f.txt" {
		t.Fatalf("list: %+v", l)
	}
	if code, a = postOpenFiles(t, ts, `{"op":"background","id":"f1","tab":"t1","line":9}`); code != 200 || a.Files[0].State != "background" || a.Files[0].Line != 9 {
		t.Fatalf("background: %d %+v", code, a)
	}
	if code, a = postOpenFiles(t, ts, `{"op":"focus","id":"f1","tab":"t1"}`); code != 200 || a.File.State != "shown" {
		t.Fatalf("focus: %d %+v", code, a)
	}
	if code, _ = postOpenFiles(t, ts, `{"op":"cursor","id":"f1","line":4}`); code != 200 {
		t.Fatalf("cursor: %d", code)
	}
	if code, a = postOpenFiles(t, ts, `{"op":"close","id":"f1","tab":"t1"}`); code != 200 || len(a.Files) != 0 {
		t.Fatalf("close: %d %+v", code, a)
	}
}

func TestOpenFilesHTTPRefusals(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	for _, c := range []struct {
		body string
		want int
	}{
		{`{"op":"open","src":"worktree","path":"../x","tab":"t1"}`, 400},
		{`{"op":"open","src":"worktree","path":"/etc/passwd","tab":"t1"}`, 400},
		{`{"op":"open","src":"worktree","path":"-x","tab":"t1"}`, 400},
		{`{"op":"open","src":"commit","path":"f.txt","tab":"t1"}`, 400},
		{`{"op":"open","src":"nope","path":"f.txt","tab":"t1"}`, 400},
		{`{"op":"open","src":"worktree","path":"f.txt","tab":"bad tab!"}`, 400},
		{`{"op":"launch"}`, 400},
		{`{"op":"focus","id":"f99","tab":"t1"}`, 404},
		{`{"op":"close","id":"f99","tab":"t1"}`, 404},
	} {
		if code, _ := postOpenFiles(t, ts, c.body); code != c.want {
			t.Errorf("%s: %d, want %d", c.body, code, c.want)
		}
	}
	// The write guard: a form post is refused.
	if code := postJSON(t, ts, "/api/open-files", `{"op":"open"}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Fatalf("write guard: %d", code)
	}
}

func TestOpenFilesHTTPEvictionNamed(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	for i := 0; i < maxOpenFiles; i++ {
		postOpenFiles(t, ts, `{"op":"open","src":"commit","rev":"HEAD","path":"p`+strconv.Itoa(i)+`.txt"}`)
	}
	_, a := postOpenFiles(t, ts, `{"op":"open","src":"commit","rev":"HEAD","path":"last.txt"}`)
	if a.Evicted != "p0.txt" {
		t.Fatalf("evicted %q", a.Evicted)
	}
}

// eventsFor opens /api/events as tab and returns a reader of its data messages.
func eventsFor(t *testing.T, ts *httptest.Server, tab string) (next func() liveMsg, stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events?tab="+tab, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(resp.Body)
	msgs := make(chan liveMsg, 32)
	go func() {
		defer close(msgs)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "data: ") {
				var m liveMsg
				if json.Unmarshal([]byte(strings.TrimPrefix(l, "data: ")), &m) == nil {
					msgs <- m
				}
			}
		}
	}()
	next = func() liveMsg {
		select {
		case m := <-msgs:
			return m
		case <-time.After(3 * time.Second):
			t.Fatal("no SSE message")
		}
		return liveMsg{}
	}
	return next, func() { cancel(); resp.Body.Close() }
}

func TestOpenFilesBroadcastAndStreamScope(t *testing.T) {
	isolateGlobal(t)
	srv := New(domain.Open(newRepoDir(t, 1)))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	next, stop := eventsFor(t, ts, "t1")
	if m := next(); m.Reason != "hello" {
		t.Fatalf("hello first: %+v", m)
	}
	postOpenFiles(t, ts, `{"op":"open","src":"worktree","path":"f.txt","tab":"t1"}`)
	m := next()
	if m.Reason != "open_files" || len(m.Files) != 1 || m.Files[0].State != "shown" {
		t.Fatalf("open_files: %+v", m)
	}
	// A second tab's stream watches the first tab's stream end.
	next2, stop2 := eventsFor(t, ts, "t2")
	defer stop2()
	next2() // hello
	stop()
	m = next2()
	if m.Reason != "open_files" || m.Files[0].State != "background" {
		t.Fatalf("a closed stream shows nothing: %+v", m)
	}
}

func TestOpenFilesStreamRefcount(t *testing.T) {
	isolateGlobal(t)
	srv := New(domain.Open(newRepoDir(t, 1)))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next()
	postOpenFiles(t, ts, `{"op":"open","src":"worktree","path":"f.txt","tab":"t1"}`)
	next()
	// A hub swap (re-root / settings write) ends the stream; the page
	// reconnects and re-reports shown after the hello (ruling L2).
	srv.restartLive(context.Background())
	nextB, stopB := eventsFor(t, ts, "t1")
	defer stopB()
	for m := nextB(); m.Reason != "hello"; m = nextB() {
	}
	if code, _ := postOpenFiles(t, ts, `{"op":"shown","id":"f1","tab":"t1"}`); code != 200 {
		t.Fatalf("shown: %d", code)
	}
	var l struct{ Files []steer.OpenFile }
	getJSON(t, ts, "/api/open-files", &l)
	if l.Files[0].State != "shown" {
		t.Fatalf("after the reconnect: %+v", l.Files)
	}
}
```

- [ ] **Step 2: run** `go test ./internal/web/ -run 'OpenFilesHTTP|OpenFilesBroadcast|OpenFilesStreamRefcount'` — Expected: build failure (`liveMsg` has no `Files`).

- [ ] **Step 3: implement.**

`live.go` — `liveMsg` gains (after `Steer`):

```go
	// Open files (openfiles_http.go): Files is the whole list on Reason
	// "open_files" (Evicted names a file dropped over the cap); FileID the
	// changed file on Reason "file_changed".
	Files   []steer.OpenFile `json:"files,omitempty"`
	Evicted string           `json:"evicted,omitempty"`
	FileID  string           `json:"file_id,omitempty"`
```

(import `github.com/homeend/gigagit/internal/steer`). In `handleEvents`, right before `writeLiveSSE(w, liveMsg{… Reason: "hello" …})`:

```go
	// The tab behind this stream (open files): counted BEFORE the hello, so
	// the page's post-hello "shown" always lands on a counted tab (ruling
	// L2); its last stream ending drops what it showed.
	if tab := r.URL.Query().Get("tab"); validTab(tab) {
		s.ofs.streamOpened(tab)
		defer func() {
			if s.ofs.streamClosed(tab) {
				s.broadcastOpenFiles(s.service().Root(), "")
			}
		}()
	}
```

`server.go` — `Server` gains `ofs *openFiles // the open-files list (openfiles.go)` next to `live`; `New` builds it: `s := &Server{closing: make(chan struct{}), ofs: newOpenFiles()}`.

`internal/web/openfiles_http.go`:

```go
package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"

	"github.com/homeend/gigagit/internal/steer"
)

// The open-files endpoints (plan 5b): GET the current worktree's list, POST
// one op on it. Every mutation is broadcast as "open_files" on /api/events.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/open-files", s.handleOpenFilesGet)
		mux.HandleFunc("POST /api/open-files", writeGuard(s.handleOpenFilesPost))
	})
}

var tabRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// validTab reports whether s names a tab (ruling L1).
func validTab(s string) bool { return tabRE.MatchString(s) }

type ofReq struct {
	Op         string `json:"op"`
	ID         string `json:"id"`
	Src        string `json:"src"`
	Rev        string `json:"rev"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Tab        string `json:"tab"`
	Everywhere bool   `json:"everywhere"`
}

type ofAnswer struct {
	File    *steer.OpenFile  `json:"file,omitempty"`
	Evicted string           `json:"evicted,omitempty"`
	Files   []steer.OpenFile `json:"files"`
}

func (s *Server) handleOpenFilesGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"files": s.ofs.list(s.service().Root())})
}

// ofAbs is where a working-tree path lives on disk.
func (s *Server) ofAbs(wt, path string) string { return filepath.Join(wt, filepath.FromSlash(path)) }

// ofKeyOf validates an open's version: a known src, safe values, and a
// working-tree path that stays inside the tree (the poller stats it).
func ofKeyOf(q ofReq) (ofKey, error) {
	k := ofKey{Src: q.Src, Rev: q.Rev, Path: q.Path}
	if k.Src == "" {
		k.Src = "worktree"
	}
	if !isGitArgSafe(k.Path) || !filepath.IsLocal(filepath.FromSlash(k.Path)) || (k.Rev != "" && !isGitArgSafe(k.Rev)) {
		return k, errors.New("invalid path/rev")
	}
	switch k.Src {
	case "worktree":
		k.Rev = ""
	case "commit", "shelf":
		if k.Rev == "" {
			return k, errors.New("a " + k.Src + " version needs rev")
		}
	default:
		return k, errors.New("unknown src " + k.Src)
	}
	return k, nil
}

func (s *Server) handleOpenFilesPost(w http.ResponseWriter, r *http.Request) {
	var q ofReq
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if q.Tab != "" && !validTab(q.Tab) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid tab"))
		return
	}
	wt := s.service().Root()
	var ans ofAnswer
	ok, broadcast := true, true
	switch q.Op {
	case "open":
		k, err := ofKeyOf(q)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		f, ev := s.ofs.open(wt, k, q.Tab, q.Line)
		s.baseline(wt, f.ID, k)
		ans.File, ans.Evicted = &f, ev
	case "focus":
		var f steer.OpenFile
		if f, ok = s.ofs.focus(wt, q.ID, q.Tab); ok {
			if k, found := s.ofs.entryKey(wt, f.ID); found {
				s.baseline(wt, f.ID, k)
			}
			ans.File = &f
		}
	case "background":
		ok = s.ofs.background(wt, q.ID, q.Tab, q.Line)
	case "close":
		ok = s.ofs.close(wt, q.ID, q.Tab, q.Everywhere)
	case "cursor":
		ok, broadcast = s.ofs.cursor(wt, q.ID, q.Line), false
	case "shown":
		ok = s.ofs.setShown(wt, q.Tab, q.ID)
	default:
		writeErr(w, http.StatusBadRequest, errors.New("unknown op "+q.Op))
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("no open file "+q.ID))
		return
	}
	if broadcast {
		s.broadcastOpenFiles(wt, ans.Evicted)
	}
	ans.Files = s.ofs.list(wt)
	writeJSON(w, ans)
}

// baseline stats a working-tree entry BEFORE the page reads its content
// (ruling L5): an edit racing that read shows on the next poll.
func (s *Server) baseline(wt, id string, k ofKey) {
	if k.Src == "worktree" {
		s.ofs.setBaseline(wt, id, statDisk(s.ofAbs(wt, k.Path)))
	}
}

// broadcastOpenFiles sends wt's list to every tab — never dropped by the op
// gate (ruling L6): it answers something a tab or agent just did.
func (s *Server) broadcastOpenFiles(wt, evicted string) {
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "open_files", Files: s.ofs.list(wt), Evicted: evicted})
	}
}

func (s *Server) broadcastFileChanged(id string) {
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "file_changed", FileID: id})
	}
}
```

- [ ] **Step 4: run** `go test ./internal/web/ -run 'OpenFiles' -v 2>&1 | tail -30` — Expected: PASS. Then `go test ./internal/web/ -run 'Events|Live'` — Expected: PASS (the hello path unchanged for a tab-less stream).

- [ ] **Step 5: commit** — `feat(web): /api/open-files and the open_files event`; task-done with `go test ./internal/web/`.

---

### Task 3: the stat poller + the filewatch wake

**Files:** Create `internal/web/openfiles_watch.go`, `internal/web/openfiles_watch_test.go`; modify `internal/web/serve.go` (start after `startLive`), `internal/web/live.go` (`Close` stops it).

**Interfaces — Consumes:** Task 1 `due/applyStat`, Task 2 `broadcastFileChanged`, `ofAbs`. **Produces:**
- `var ofTick = time.Second`, `var ofBackgroundEvery = 5 * time.Second` (seams, read ONCE at start)
- `func (s *Server) pollOpenFiles(now time.Time, all bool, bgEvery time.Duration) []string` (ids reported changed)
- `func (s *Server) startOpenFilesWatch()`; `Server.ofStop chan struct{}` + `ofStopOnce sync.Once`

- [ ] **Step 1: failing tests** — `internal/web/openfiles_watch_test.go`:

```go
package web

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestPollOpenFilesReportsEditsAndDeletes(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	wt := srv.service().Root()
	f, _ := srv.ofs.open(wt, ofKey{Src: "worktree", Path: "f.txt"}, "t1", 0)
	srv.baseline(wt, f.ID, ofKey{Src: "worktree", Path: "f.txt"})
	now := time.Unix(5000, 0)
	if got := srv.pollOpenFiles(now, false, 5*time.Second); len(got) != 0 {
		t.Fatalf("unchanged: %v", got)
	}
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("edited, longer\n"), 0o644)
	if got := srv.pollOpenFiles(now.Add(time.Second), false, 5*time.Second); len(got) != 1 || got[0] != f.ID {
		t.Fatalf("edit: %v", got)
	}
	os.Remove(filepath.Join(dir, "f.txt"))
	if got := srv.pollOpenFiles(now.Add(2*time.Second), false, 5*time.Second); len(got) != 1 {
		t.Fatalf("delete: %v", got)
	}
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("back\n"), 0o644)
	if got := srv.pollOpenFiles(now.Add(3*time.Second), false, 5*time.Second); len(got) != 1 {
		t.Fatalf("back: %v", got)
	}
}

func TestPollOpenFilesSkipsWhileAnOpRuns(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	wt := srv.service().Root()
	f, _ := srv.ofs.open(wt, ofKey{Src: "worktree", Path: "f.txt"}, "t1", 0)
	srv.baseline(wt, f.ID, ofKey{Src: "worktree", Path: "f.txt"})
	srv.opMu.Lock()
	srv.cur = &opRun{} // a live op (not done)
	srv.opMu.Unlock()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("the op's edit\n"), 0o644)
	if got := srv.pollOpenFiles(time.Unix(1, 0), true, 5*time.Second); got != nil {
		t.Fatalf("mid-op round must be skipped: %v", got)
	}
	srv.opMu.Lock()
	srv.cur.done = true
	srv.opMu.Unlock()
	if got := srv.pollOpenFiles(time.Unix(2, 0), true, 5*time.Second); len(got) != 1 {
		t.Fatalf("the first round after the op sees the edit (baseline not advanced): %v", got)
	}
}

func TestPollOpenFilesBackgroundCadence(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	wt := srv.service().Root()
	f, _ := srv.ofs.open(wt, ofKey{Src: "worktree", Path: "f.txt"}, "", 0) // background
	srv.baseline(wt, f.ID, ofKey{Src: "worktree", Path: "f.txt"})
	now := time.Unix(9000, 0)
	srv.pollOpenFiles(now, false, 5*time.Second) // first check
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("edited, longer\n"), 0o644)
	if got := srv.pollOpenFiles(now.Add(time.Second), false, 5*time.Second); len(got) != 0 {
		t.Fatalf("a background file waits 5 s: %v", got)
	}
	if got := srv.pollOpenFiles(now.Add(6*time.Second), false, 5*time.Second); len(got) != 1 {
		t.Fatalf("then it is looked at: %v", got)
	}
}
```

- [ ] **Step 2: run** `go test ./internal/web/ -run PollOpenFiles` — Expected: build failure (`pollOpenFiles` undefined).

- [ ] **Step 3: implement** `internal/web/openfiles_watch.go`:

```go
package web

import (
	"time"

	"github.com/homeend/gigagit/internal/filewatch"
	"github.com/homeend/gigagit/internal/gitwatch"
)

// Following the disk (plan 5b): the server stat-polls the current worktree's
// open working-tree files — a shown one every tick, a background one every
// ofBackgroundEvery — and tells the tabs "file_changed". internal/filewatch
// only WAKES the poll where the filesystem delivers events (never on a 9p
// /mnt); the stat decides (the TUI's rule).

// Test seams, read once when the watch starts.
var (
	ofTick            = time.Second
	ofBackgroundEvery = 5 * time.Second
)

const ofWatchDebounce = 150 * time.Millisecond // the TUI's docWatchDebounce

// pollOpenFiles runs one stat round and broadcasts each change; it returns
// the changed ids. A round while an op runs is skipped whole — no stat, no
// baseline moved — so the op's edits are seen by the first round after it
// (ruling L6).
func (s *Server) pollOpenFiles(now time.Time, all bool, bgEvery time.Duration) []string {
	if s.opInFlight() {
		return nil
	}
	wt := s.service().Root()
	var changed []string
	for _, d := range s.ofs.due(wt, now, all, bgEvery) {
		if s.ofs.applyStat(wt, d.ID, statDisk(s.ofAbs(wt, d.Path))) {
			changed = append(changed, d.ID)
			s.broadcastFileChanged(d.ID)
		}
	}
	return changed
}

// worktreeAbs is wt's open working-tree files on disk, for the wake watcher.
func (s *Server) worktreeAbs(wt string) []string {
	var out []string
	for _, f := range s.ofs.list(wt) {
		if f.Source == "worktree" {
			out = append(out, s.ofAbs(wt, f.Path))
		}
	}
	return out
}

// startOpenFilesWatch runs the poll loop until Close.
func (s *Server) startOpenFilesWatch() {
	s.ofStop = make(chan struct{}) // Serve starts it once, before any Close
	stop, tick, bg, now := s.ofStop, ofTick, ofBackgroundEvery, liveNow
	go func() {
		t := time.NewTicker(tick)
		defer t.Stop()
		var w *filewatch.Watcher
		var wake <-chan string
		watchRoot, broken := "", false
		defer func() {
			if w != nil {
				_ = w.Close()
			}
		}()
		for {
			all := false
			select {
			case <-stop:
				return
			case <-s.closing:
				return
			case <-t.C:
			case _, ok := <-wake:
				if !ok {
					wake, w = nil, nil
					continue
				}
				all = true
			}
			wt := s.service().Root()
			if wt != watchRoot { // a re-root: the old tree's watcher goes
				if w != nil {
					_ = w.Close()
				}
				w, wake, watchRoot, broken = nil, nil, wt, false
			}
			if w == nil && !broken && gitwatch.Supported(wt) {
				if nw, err := filewatch.New(ofWatchDebounce); err == nil {
					w, wake = nw, nw.Events()
				} else {
					broken = true
				}
			}
			if w != nil {
				w.Set(s.worktreeAbs(wt))
			}
			s.pollOpenFiles(now(), all, bg)
		}
	}()
}

// stopOpenFilesWatch ends the loop (idempotent; nil-safe before a start).
func (s *Server) stopOpenFilesWatch() {
	if s.ofStop != nil {
		s.ofStopOnce.Do(func() { close(s.ofStop) })
	}
}
```

`server.go` — `Server` gains `ofStop chan struct{}` and `ofStopOnce sync.Once` beside `ofs`. `live.go` — `func (s *Server) Close() { s.stopLive(); s.stopOpenFilesWatch() }`. `serve.go` — after `srv.startLive(ctx)`: `srv.startOpenFilesWatch() // the open files follow the disk (openfiles_watch.go)`.

- [ ] **Step 4: run** `go test ./internal/web/ -run 'PollOpenFiles|OpenFiles|Shutdown|Live' 2>&1 | tail -20` — Expected: PASS.

- [ ] **Step 5: commit** — `feat(web): open working-tree files follow the disk (stat poll + filewatch wake)`; task-done with `go test ./internal/web/`.

---

### Task 4: the page — tab id, the footer stack, the viewer registers what it shows

**Files:** Modify `internal/web/static/core.js` (`tabId`), `internal/web/static/layers.js` (`pushFoot`/`popFoot`/`footOwned`), `internal/web/static/viewer.js`, `internal/web/static/live.js` (EventSource URL only), `internal/web/viewerjs_test.go`.

**Interfaces — Consumes:** Task 2 wire. **Produces (viewer.js exports):** `openViewer({src, rev, path, line, id})` (id → `focus`), `closeViewer(how = "close" | "background")`, `dropViewer()` (local close, no POST), `viewerFileId()` (the id this tab's viewer shows, "" when closed), `viewerHello()`, `viewerOpenFiles(files)`, `viewerFileChanged(id)`; pure `pickLine(link, place, server)`, `keepLine(cur, count, placeholder)`. **core.js:** `tabId`. **layers.js:** `pushFoot(owner, html)`, `popFoot(owner)`, `footOwned(owner)`.

- [ ] **Step 1: failing tests** — extend `TestViewerModelJS` in `viewerjs_test.go` with a second node run, and grow `viewerWiring`:

```go
func TestViewerPlaceJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", viewerPureStart, viewerPureEnd, `
const r = [];
r.push(pickLine(7, {cur: 3}, 5), pickLine(0, {cur: 3}, 5), pickLine(0, null, 5), pickLine(0, null, 0), pickLine(0, {cur: 0}, 4));
r.push(keepLine(9, 4, false), keepLine(9, 0, true), keepLine(0, 4, false), keepLine(2, 4, false));
console.log(r.join("|"));
`)
	if want := "7|3|5|0|4|4|9|1|2"; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
```

`viewerWiring` additions (under a `// Plan 5b Task 4: the list.` comment):

```go
	{"core.js", "tabId", "a page load names itself to the server"},
	{"live.js", `"/api/events?tab="`, "the event stream carries the tab id"},
	{"layers.js", "function pushFoot(", "the footer chips stack (viewer + switcher)"},
	{"viewer.js", `pushFoot("viewer"`, "the viewer's chips ride the footer stack"},
	{"viewer.js", `"/api/open-files"`, "the viewer registers what it shows"},
	{"viewer.js", `op: "background"`, "ctrl+] and the menu hand-offs background the file"},
	{"viewer.js", `op: "cursor"`, "the cursor line is reported"},
	{"viewer.js", "closed in another tab", "x in another tab closes this viewer"},
	{"viewer.js", "files open)", "an eviction names the file (the TUI's words)"},
	{"viewer.js", "is in the background — ctrl+\\\\ lists open files", "ctrl+] says where the file went"},
```

- [ ] **Step 2: run** `go test ./internal/web/ -run 'ViewerPlaceJS|ViewerJSIsWired'` — Expected: FAIL (`pickLine is not defined`; wiring rows missing).

- [ ] **Step 3: implement.**

`core.js` (near `state`; add `tabId` to the export list):

```js
// tabId names this page LOAD to the server (open files: which tab shows
// which file). Never stored: a duplicated tab copies sessionStorage and must
// still be a tab of its own.
const tabId =
  globalThis.crypto && typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : Math.random().toString(36).slice(2) + Date.now().toString(36);
```

`live.js` `connectLive`: `const es = new EventSource("/api/events?tab=" + encodeURIComponent(tabId));` (import `tabId` from `./core.js`).

`layers.js` (export all three):

```js
// --- the footer chip stack ---
// A surface on top swaps #foot's chips for its own keys and gets the ones
// beneath back when it goes — nested surfaces (the open-files switcher over
// the viewer) each pop only their own entry, wherever it sits.
const footStack = [];
let footBase = null;

function pushFoot(owner, html) {
  const foot = $("foot");
  if (footBase === null) footBase = foot.innerHTML;
  const i = footStack.findIndex((f) => f.owner === owner);
  if (i >= 0) footStack.splice(i, 1);
  footStack.push({ owner, html });
  foot.innerHTML = html;
}

function popFoot(owner) {
  const i = footStack.findIndex((f) => f.owner === owner);
  if (i < 0) return;
  footStack.splice(i, 1);
  $("foot").innerHTML = footStack.length ? footStack[footStack.length - 1].html : footBase;
  if (!footStack.length) footBase = null;
}

function footOwned(owner) {
  return footStack.some((f) => f.owner === owner);
}
```

`viewer.js`:

1. Pure section — append before `// --- end viewer model ---`:

```js
// pickLine is where a (re)opened file lands (ruling L8): a link's line, else
// where this tab left it, else the server's last line, else 0 (= line 1).
function pickLine(link, place, server) {
  if (link > 0) return link;
  if (place && place.cur > 0) return place.cur;
  return server > 0 ? server : 0;
}

// keepLine is the cursor after a reload (ruling L9): clamped to the new
// length — but kept as is while the deleted placeholder shows, for when the
// file returns.
function keepLine(cur, count, placeholder) {
  if (placeholder) return cur;
  return clampLine(cur > 0 ? cur : 1, count);
}
```

2. `view` gains `id: ""` (the open-files entry this tab's viewer shows) in its literal. State + helpers (after `const viewerSearch = new Search();`):

```js
const places = new Map(); // id → {cur, top}: where THIS tab left each file
let loadSeq = 0; // bumped by every open: a reload that sees it move drops (L9)
let cursorTimer = null;

function ofPost(body) {
  return postJSON("/api/open-files", { tab: tabId, ...body });
}

function isOpen() {
  return !viewerRoot.classList.contains("hidden");
}

function viewerFileId() {
  return isOpen() ? view.id : "";
}

function rememberPlace() {
  if (view.id) places.set(view.id, { cur: view.cur, top: $("viewer-body").scrollTop });
}

function placeholderFor(body, lines) {
  return body.missing ? "(file deleted on disk)" : body.too_large ? "(file too large to preview)" : lines.length ? "" : "(empty file)";
}

function fetchContent(src, rev, path) {
  return getJSON("/api/file-content?src=" + encodeURIComponent(src) + "&rev=" + encodeURIComponent(rev) + "&path=" + encodeURIComponent(path));
}

// reportCursor tells the server this tab's line, debounced: the switcher of
// every tab shows it (read fresh when a switcher opens — never broadcast).
function reportCursor() {
  clearTimeout(cursorTimer);
  const id = view.id, line = view.cur;
  if (!id || !line) return;
  cursorTimer = setTimeout(() => ofPost({ op: "cursor", id, line }).catch(() => {}), 400);
}
```

3. `openViewer` becomes:

```js
// openViewer shows path at one version and registers it as this tab's open
// file (reused when already open). id (the switcher) brings an entry back by
// id. The cursor lands per pickLine; a second call replaces the file on
// screen, which stays open in the background.
async function openViewer({ src = "worktree", rev = "", path = "", line = 0, id = "" }) {
  const seq = ++loadSeq;
  if (isOpen()) rememberPlace();
  let reg, body;
  try {
    reg = id ? await ofPost({ op: "focus", id }) : await ofPost({ op: "open", src, rev, path, line });
    body = await fetchContent(reg.file.source, reg.file.rev || "", reg.file.path);
  } catch (e) {
    opLine("view failed: " + (e.message || e), true);
    return { ok: false, notice: "" };
  }
  if (seq !== loadSeq) return { ok: false, notice: "" }; // a newer open won
  const f = reg.file;
  Object.assign(view, { id: f.id, src: f.source, rev: f.rev || "", path: f.path, lines: body.lines || [] });
  view.placeholder = placeholderFor(body, view.lines);
  const place = line > 0 ? null : places.get(f.id);
  const want = pickLine(line, place, f.line || 0);
  const landed = line > 0 ? landLine(line, view.lines.length, f.path) : { line: view.placeholder && body.missing ? want : clampLine(want || 1, view.lines.length), notice: "" };
  view.cur = landed.line;
  viewerSearchBar.reset(); // a new file is a new search
  viewerRoot.style.bottom = $("foot").offsetHeight + "px"; // the bar stays in sight
  pushLayer("viewer", viewerRoot, { onKey: viewerKey });
  swapFoot(true);
  paintTitle();
  renderViewer();
  centerCursor();
  if (place) $("viewer-body").scrollTop = place.top;
  $("viewer-body").focus({ preventScroll: true });
  if (landed.notice) opLine(landed.notice, false);
  if (reg.evicted) opLine("closed " + reg.evicted + " (20 files open)", false);
  return { ok: true, notice: landed.notice };
}
```

4. Close / background / drop:

```js
// closeViewer takes the viewer down. "close" (esc, the backdrop) lets go of
// the file — gone from the list unless another tab shows it; "background"
// (ctrl+], the . menu's hand-offs) keeps it open (ruling L4).
function closeViewer(how = "close") {
  const id = view.id, line = view.cur;
  if (id) {
    rememberPlace();
    if (how === "close") places.delete(id);
    ofPost(how === "close" ? { op: "close", id } : { op: "background", id, line }).catch(() => {});
  }
  dropViewer();
}

// dropViewer takes the viewer down WITHOUT telling the server — the file is
// already gone from the list (closed everywhere) or never registered.
function dropViewer() {
  view.id = "";
  loadSeq++; // an open or reload in flight must not bring it back
  clearTimeout(cursorTimer);
  closeLayer("viewer");
  swapFoot(false);
}

function backgroundViewer() {
  const path = view.path;
  closeViewer("background");
  opLine(path + " is in the background — ctrl+\\ lists open files", false);
}
```

The backdrop click stays `closeViewer()`; the three `.` menu hand-offs (`history`, `blame`, and both `viewerDiff*`) call `closeViewer("background")`.

5. `paintCursor` ends with `reportCursor();`.

6. `viewerKey` — before `if (e.ctrlKey || e.metaKey || e.altKey) return false;`:

```js
  if (e.ctrlKey && !e.altKey && !e.metaKey && (e.code === "BracketRight" || e.key === "]")) {
    e.preventDefault();
    backgroundViewer();
    return true;
  }
```

7. Footer: `swapFoot` becomes

```js
function swapFoot(on) {
  if (on) pushFoot("viewer", VIEWER_FOOT);
  else popFoot("viewer");
}
```

`VIEWER_FOOT` gains `<button data-vact="bg">ctrl+] background</button>` before `esc close`; the `$("foot")` click switch gains `case "bg": backgroundViewer(); break;`. The keyup guard becomes:

```js
document.addEventListener("keyup", () => {
  if (footOwned("viewer") && !isOpen()) {
    // Closed from outside (another surface cleared the stack): the file goes
    // to the background, not away.
    if (view.id) closeViewer("background");
    else swapFoot(false);
  }
});
```

(delete `let savedFoot = null;`.)

8. Event hooks (exported; live.js calls them in Task 5):

```js
// viewerHello re-reports what this tab shows after its event stream
// (re)connected — the server dropped it when the old stream ended (L2).
function viewerHello() {
  if (viewerFileId()) ofPost({ op: "shown", id: view.id }).catch(() => {});
}

// viewerOpenFiles: the list changed. A file this viewer shows that left it
// was closed in another tab's switcher (x) — this viewer goes too.
function viewerOpenFiles(files) {
  const id = viewerFileId();
  if (!id || files.some((f) => f.id === id)) return;
  const path = view.path;
  places.delete(id);
  dropViewer();
  opLine(path + " was closed in another tab", false);
}

// viewerFileChanged: the file on disk changed — re-read it keeping this
// tab's place (L9). A newer open (a link landing) wins over the reload.
async function viewerFileChanged(id) {
  if (!id || id !== viewerFileId()) return;
  const seq = loadSeq;
  let body;
  try {
    body = await fetchContent(view.src, view.rev, view.path);
  } catch {
    return; // the next change retries; a dead server paints its own veil
  }
  if (seq !== loadSeq || id !== view.id) return;
  const el = $("viewer-body");
  const top = el.scrollTop, left = el.scrollLeft;
  view.lines = body.lines || [];
  view.placeholder = placeholderFor(body, view.lines);
  view.cur = keepLine(view.cur, view.lines.length, !!body.missing);
  renderViewer();
  el.scrollTop = top;
  el.scrollLeft = left;
}
```

Imports: `postJSON, tabId` from `./core.js`; `footOwned, popFoot, pushFoot` from `./layers.js`. Exports: `closeViewer, dropViewer, openViewer, viewerFileChanged, viewerFileId, viewerHello, viewerOpenFiles`.

- [ ] **Step 4: run** `go test ./internal/web/ -run 'Viewer' -v 2>&1 | tail -15` — Expected: PASS. `node --check internal/web/static/viewer.js internal/web/static/layers.js internal/web/static/core.js` — Expected: no output.

- [ ] **Step 5: commit** — `feat(web): the viewer registers its file in the shared list; ctrl+] backgrounds it`; task-done with `go test ./internal/web/`.

---

### Task 5: live.js routes the list's events

**Files:** Modify `internal/web/static/live.js`, `internal/web/viewerjs_test.go`.

**Interfaces — Consumes:** Task 4 exports; `switcherOpenFiles(files)` from Task 6 is wired THERE (this task routes only the viewer).

- [ ] **Step 1: failing wiring rows** (`viewerWiring`, `// Plan 5b Task 5: events.`):

```go
	{"live.js", `msg.reason === "open_files"`, "the list's broadcast reaches the page"},
	{"live.js", "viewerOpenFiles(msg.files || [])", "an empty list arrives as no files (omitempty)"},
	{"live.js", `msg.reason === "file_changed"`, "a disk change reaches the viewer"},
	{"live.js", "viewerFileChanged(msg.file_id)", "the viewer reloads the changed file"},
	{"live.js", "viewerHello()", "every hello re-reports what this tab shows"},
```

- [ ] **Step 2: run** `go test ./internal/web/ -run ViewerJSIsWired` — Expected: FAIL on the five rows.

- [ ] **Step 3: implement** in `connectLive`'s `onmessage`: inside the `hello` branch, after `connected = true;` add `viewerHello();`. After the `steer` branch:

```js
    // Open files (plan 5b): the shared list changed, or a file on disk did.
    // Neither is a refresh source — they never join the coalescing set.
    if (msg.reason === "open_files") {
      viewerOpenFiles(msg.files || []);
      return;
    }
    if (msg.reason === "file_changed") {
      viewerFileChanged(msg.file_id);
      return;
    }
```

Import `viewerFileChanged, viewerHello, viewerOpenFiles` from `./viewer.js`.

- [ ] **Step 4: run** `go test ./internal/web/ -run 'ViewerJSIsWired|Live'` — Expected: PASS.

- [ ] **Step 5: commit** — `feat(web): the page follows open_files and file_changed`; task-done with `go test ./internal/web/`.

---

### Task 6: the `ctrl+\` switcher (`openfiles.js`)

**Files:** Create `internal/web/static/openfiles.js`, `internal/web/openfilesjs_test.go`; modify `internal/web/static/app.js` (import), `internal/web/static/keys.js` (global `ctrl+\`, the foot `data-act`), `internal/web/static/index.html` (`#foot` chip), `internal/web/static/viewer.js` (`ctrl+\` from the viewer, `data-vact="files"` chip), `internal/web/static/live.js` (`switcherOpenFiles`), `internal/web/static/style.css`.

**Interfaces — Consumes:** Task 4 `openViewer({id})`, `viewerFileId`, `dropViewer`, `versionLabel`; Task 2 wire. **Produces:** `openSwitcher()`, `isSwitcherKey(e)`, `switcherOpenFiles(files)`; pure `switcherRows(files, mine)`, `clampSel(sel, n)`.

- [ ] **Step 1: failing tests** — `internal/web/openfilesjs_test.go`:

```go
package web

import "testing"

const ofPureStart = "// --- switcher model (pure; guarded against Go) ---"
const ofPureEnd = "// --- end switcher model ---"

func TestSwitcherModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const rows = switcherRows([
  {id: "f2", path: "a/b.go", line: 12, source: "worktree", state: "shown"},
  {id: "f1", path: "c.txt", line: 0, source: "commit", rev: "0123456789", state: "shown"},
], "f2");
console.log(JSON.stringify(rows) + "|" + [clampSel(5, 2), clampSel(-1, 2), clampSel(3, 0)].join(","));
`)
	want := `[{"id":"f2","mark":"●","path":"a/b.go","line":":12","source":"worktree","rev":""},{"id":"f1","mark":"○","path":"c.txt","line":"","source":"commit","rev":"0123456789"}]|1,0,0`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
```

`viewerWiring` rows (`// Plan 5b Task 6: the switcher.`):

```go
	{"app.js", "./openfiles.js", "the switcher module is imported at boot"},
	{"openfiles.js", `mountOverlay("openfiles")`, "the switcher is a mounted layer"},
	{"openfiles.js", `pushFoot("openfiles"`, "its keys go in the bottom bar"},
	{"openfiles.js", "elidePath(", "rows cut the path in the middle"},
	{"openfiles.js", "everywhere: true", "x closes the file in every tab"},
	{"openfiles.js", "no open files", "an empty list says so"},
	{"keys.js", "isSwitcherKey(e)", "ctrl+\\ works from the main page"},
	{"keys.js", `case "openfiles":`, "the footer chip opens the switcher"},
	{"index.html", `data-act="openfiles"`, "the main footer advertises ctrl+\\"},
	{"viewer.js", "isSwitcherKey(e)", "ctrl+\\ works from the viewer"},
	{"live.js", "switcherOpenFiles(", "an open switcher follows the list"},
	{"style.css", "#openfiles.hidden", "the overlay hides by id (a global .hidden does not exist)"},
```

- [ ] **Step 2: run** `go test ./internal/web/ -run 'SwitcherModelJS|ViewerJSIsWired'` — Expected: FAIL (`static/openfiles.js` missing).

- [ ] **Step 3: implement** `internal/web/static/openfiles.js`:

```js
// openfiles.js — the ctrl+\ switcher (open files on the web, plan 5b): the
// worktree's open files, shared by every tab of this gg web, most recently
// shown first. ● marks the file THIS tab's viewer shows. enter brings one
// back where this tab left it, x closes it in every tab.
import { $, charWidth, elidePath, esc, getJSON, postJSON, tabId } from "./core.js";
import { closeLayer, mountOverlay, popFoot, pushFoot, pushLayer } from "./layers.js";
import { opLine } from "./ops.js";
import { dropViewer, openViewer, versionLabel, viewerFileId } from "./viewer.js";
import { registerHelp } from "./menus.js";

// --- switcher model (pure; guarded against Go) ---
function switcherRows(files, mine) {
  return files.map((f) => ({
    id: f.id,
    mark: f.id === mine ? "●" : "○",
    path: f.path,
    line: f.line > 0 ? ":" + f.line : "",
    source: f.source,
    rev: f.rev || "",
  }));
}

function clampSel(sel, n) {
  return n ? Math.min(Math.max(sel, 0), n - 1) : 0;
}
// --- end switcher model ---

const sw = { files: [], sel: 0 };
const root = mountOverlay("openfiles");
root.innerHTML = `<div id="openfiles-box"><div id="openfiles-title">Open files</div><div id="openfiles-list"></div></div>`;
root.addEventListener("click", (e) => {
  if (e.target.id === "openfiles") return closeSwitcher();
  const row = e.target.closest(".ofrow[data-i]");
  if (!row) return;
  sw.sel = Number(row.dataset.i);
  bringBack();
});

const SWITCHER_FOOT =
  `<span>↑↓ j k move</span><button data-oact="enter">enter bring back</button>` +
  `<button data-oact="x">x close file</button><button data-oact="esc">esc close</button>`;

$("foot").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-oact]");
  if (!b) return;
  if (b.dataset.oact === "enter") bringBack();
  else if (b.dataset.oact === "x") closeSelected();
  else closeSwitcher();
});

function isSwitcherKey(e) {
  return e.ctrlKey && !e.altKey && !e.metaKey && (e.code === "Backslash" || e.key === "\\");
}

function isOpen() {
  return !root.classList.contains("hidden");
}

async function openSwitcher() {
  let body;
  try {
    body = await getJSON("/api/open-files");
  } catch (e) {
    return opLine("open files: " + (e.message || e), true);
  }
  sw.files = body.files || [];
  if (!sw.files.length) return opLine("no open files — view file on a file row opens one", false);
  const mine = viewerFileId();
  sw.sel = Math.max(0, sw.files.findIndex((f) => f.id === mine));
  root.style.bottom = $("foot").offsetHeight + "px";
  pushLayer("openfiles", root, { onKey: switcherKey });
  pushFoot("openfiles", SWITCHER_FOOT);
  renderSwitcher();
}

function closeSwitcher() {
  closeLayer("openfiles");
  popFoot("openfiles");
}

// switcherOpenFiles: the list changed while the switcher is up (another tab,
// an eviction, a stream ending) — re-render from it, never act on a stale row.
function switcherOpenFiles(files) {
  if (!isOpen()) return;
  const cur = sw.files[sw.sel] && sw.files[sw.sel].id;
  sw.files = files;
  if (!files.length) return closeSwitcher();
  const i = files.findIndex((f) => f.id === cur);
  sw.sel = clampSel(i >= 0 ? i : sw.sel, files.length);
  renderSwitcher();
}

function renderSwitcher() {
  const list = $("openfiles-list");
  const cols = Math.floor(list.clientWidth / charWidth()) - 4;
  const rows = switcherRows(sw.files, viewerFileId());
  list.innerHTML = rows
    .map((r, i) => {
      const meta = r.line + "  " + versionLabel(r.source, r.rev);
      const room = cols - 2 - meta.length;
      return (
        `<div class="ofrow${i === sw.sel ? " sel" : ""}" data-i="${i}" title="${esc(r.path)}">` +
        `${r.mark} ${esc(room > 0 ? elidePath(r.path, room) : r.path)}<span class="ofmeta">${esc(meta)}</span></div>`
      );
    })
    .join("");
  const cur = list.querySelector(".ofrow.sel");
  if (cur) cur.scrollIntoView({ block: "nearest" });
}

function bringBack() {
  const f = sw.files[sw.sel];
  if (!f) return;
  closeSwitcher();
  openViewer({ id: f.id });
}

// closeSelected: x closes the file in EVERY tab (ruling L4). This tab's own
// viewer on it goes first, so it does not read "closed in another tab".
async function closeSelected() {
  const f = sw.files[sw.sel];
  if (!f) return;
  if (f.id === viewerFileId()) dropViewer();
  let ans;
  try {
    ans = await postJSON("/api/open-files", { op: "close", id: f.id, tab: tabId, everywhere: true });
  } catch (e) {
    return opLine("close failed: " + (e.message || e), true);
  }
  sw.files = ans.files || [];
  if (!sw.files.length) return closeSwitcher();
  sw.sel = clampSel(sw.sel, sw.files.length);
  renderSwitcher();
}

function switcherKey(e) {
  switch (e.key) {
    case "ArrowDown": case "j": sw.sel = clampSel(sw.sel + 1, sw.files.length); renderSwitcher(); break;
    case "ArrowUp": case "k": sw.sel = clampSel(sw.sel - 1, sw.files.length); renderSwitcher(); break;
    case "Enter": bringBack(); break;
    case "x": closeSelected(); break;
    case "Escape": closeSwitcher(); break;
    default:
      if (!isSwitcherKey(e)) return true; // swallow: the popup owns the keyboard
      closeSwitcher(); // ctrl+\ again toggles it off
  }
  e.preventDefault();
  return true;
}

registerHelp({
  key: "open files",
  html:
    "files opened in the viewer stay open, one list per worktree shared by every tab: <b>ctrl+]</b> in the viewer sends the file to the background, " +
    "<b>ctrl+\\</b> lists the open files (<b>●</b> the one this tab shows) — <b>enter</b> brings one back where this tab left it, <b>x</b> closes it in every tab. " +
    "A working-tree file follows the disk.",
});

export { isSwitcherKey, openSwitcher, switcherOpenFiles };
```

`viewer.js`: export `versionLabel` (add to the export list); `viewerKey` — right after the ctrl+] branch:

```js
  if (isSwitcherKey(e)) {
    e.preventDefault();
    openSwitcher();
    return true;
  }
```

(import `isSwitcherKey, openSwitcher` from `./openfiles.js`); `VIEWER_FOOT` gains `<button data-vact="files">ctrl+\ open files</button>` after the background chip; the vact switch gains `case "files": openSwitcher(); break;`.

`keys.js`: import `isSwitcherKey, openSwitcher` from `./openfiles.js`; after the palette shortcut block:

```js
  // ctrl+\: the open-files switcher, from anywhere a layer does not own the
  // keyboard (the viewer handles its own) — even from the commit box.
  if (isSwitcherKey(e)) {
    e.preventDefault();
    openSwitcher();
    return;
  }
```

and in the `data-act` switch beside `case "palette":` — `case "openfiles": openSwitcher(); break;`.

`index.html` `#foot`: insert `<button data-act="openfiles">ctrl+\ open files</button>` before the `? help` button.

`app.js`: `import "./openfiles.js";` next to the `./viewer.js` import.

`live.js`: in the `open_files` branch, after `viewerOpenFiles(...)`: `switcherOpenFiles(msg.files || []);` (import from `./openfiles.js`).

`style.css` (after the `#viewer` block; reuse the viewer's variables):

```css
#openfiles { position: fixed; top: 0; left: 0; right: 0; bottom: var(--foot-h, 25px); background: rgba(0,0,0,.45); display: flex; align-items: flex-start; justify-content: center; padding-top: 12vh; z-index: 22; }
#openfiles.hidden { display: none; }
#openfiles-box { background: var(--bg-alt); border: 1px solid var(--accent); border-radius: 6px; width: min(760px, 92vw); max-height: 60vh; display: flex; flex-direction: column; }
#openfiles-title { padding: 8px 12px; border-bottom: 1px solid var(--border); }
#openfiles-list { overflow: auto; font-family: ui-monospace, monospace; font-size: 12px; padding: 4px 0; }
#openfiles-list .ofrow { padding: 2px 12px; white-space: nowrap; cursor: pointer; }
#openfiles-list .ofrow.sel { background: var(--sel-bg); }
#openfiles-list .ofmeta { color: var(--muted); margin-left: 1ch; }
```

(Before committing, `grep -n -- '--sel-bg\|--muted' internal/web/static/style.css` — use whatever names the file defines for the selected-row background and dim text; a missing variable paints nothing.)

- [ ] **Step 4: run** `go test ./internal/web/ -run 'SwitcherModelJS|Viewer' -v 2>&1 | tail -15` and `node --check` on every touched `.js` — Expected: PASS / no output. Then the FULL package: `go test ./internal/web/` — Expected: ok (a static pin on `#foot`'s chips or `index.html` would fail here — update the pin, ledger a ruling).

- [ ] **Step 5: commit** — `feat(web): the ctrl+\ open-files switcher`; task-done with `go test ./internal/web/`.

---

### Task 7: browser check, docs

**Files:** scratchpad playwright script (not committed); `CHANGELOG.md`, `README.md` (web section / *Open files*), `docs/CLAUDE-details.md` ("Content links" → a *Web open files* paragraph).

- [ ] **Step 1: the check, against the UNFIXED installed build first.** A playwright script in the scratchpad (`node_modules` there) against `gg web` on a throwaway repo (`git init` in the scratchpad, two committed files `a.txt` 30 lines, `b.txt`), started with the flags 5a's check used, killed by PID. Each step ASSERTS visibility (`getComputedStyle(el).display !== "none"` and a non-zero bounding box):
  1. `openViewer({path: "a.txt"})` via `page.evaluate(() => import("/static/viewer.js").then((m) => m.openViewer({ path: "a.txt", line: 12 })))`; `#viewer` visible, `.vcur` is line 12.
  2. `ctrl+]` → `#viewer` hidden; the op line contains `is in the background`.
  3. `ctrl+\` → `#openfiles` visible; one `.ofrow` containing `a.txt`, `○`, `:12`, `working tree`; `#foot` contains `enter bring back`.
  4. `Enter` → `#viewer` visible, `.vcur` line 12, `#foot` contains `ctrl+] background`.
  5. Append a line to `a.txt` on disk; within 4 s `#viewer-body` contains the new text and `.vcur` is still line 12.
  6. Delete `a.txt`; within 7 s `#viewer-body` shows `(file deleted on disk)`; restore it → the text is back, cursor 12.
  7. Second page (tab 2) on the same server: `ctrl+\` lists `a.txt` (`○` there); `x` → tab 1's `#viewer` hidden within 3 s and tab 1's op line says `was closed in another tab`; tab 2's switcher closed (list empty).
  8. Tab 1: open `a.txt`, then trigger a settings write that restarts the hub (`POST /api/settings` with the current refresh values — the call 5a's live tests use) → after the reconnect `GET /api/open-files` shows the file `shown`.
  Expected on the unfixed build: step 2 FAILS (ctrl+] does nothing / `/api/open-files` 404). Then `go build -o <scratch>/gg ./cmd/gg` in the worktree and re-run: every step passes. Save a screenshot of step 3 to the scratchpad.

- [ ] **Step 2: docs.** CHANGELOG (Unreleased → web): *Open files on the web: files opened in the viewer stay open in one list per worktree, shared by every tab — ctrl+] sends the viewer to the background, ctrl+\ lists them (● the one this tab shows; enter brings one back where this tab left it, x closes it everywhere); working-tree files follow the disk.* README: the web section's viewer paragraph gains the same two keys. `docs/CLAUDE-details.md` "Content links": a *Web open files (5b)* paragraph — registry on the Server keyed by `svc.Root()` (not the hub), tab id per load + stream refcount + re-report on hello (L2), ops table (L3), esc vs x (L4), stat-before-read in `open` (L5), poll skip while an op runs (L6), footer stack (L10).

- [ ] **Step 3: gates.** `./test.sh` then `./test.sh race` (never edit the tree meanwhile) — Expected: both green.

- [ ] **Step 4: commit** — `docs: open files on the web (5b)`; task-done with `go test ./internal/web/`.
