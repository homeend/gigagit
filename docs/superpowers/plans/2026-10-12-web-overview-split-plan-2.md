# Web overview split, plan 2 — the stacked file column (`S`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In gg web's overview split, `S` (from either column) turns the right column into one scroll of every file — each under a header — loaded lazily, with the overview's bands and the review's remarks (or the agent's notes) as read-only boxes; `S` again returns to the single file.

**Architecture:** The stack is a third state of the right column, `split.stack`, beside "a file" and "empty". Entering it backgrounds the single file and clears `view`, so every existing file path (`canFlip`, `viewBands`, `cursorRow`, `viewerFileId`, `reportCursor`, `viewerFileChanged`, `diffChips`) goes quiet by itself. The stack's cursor is a `(k, line)` pair (file index, 1-based line, 0 = the header), never a global row. A new import-free `static/splitstack.js` holds the pure half (which files, cursor moves, header and band steps, restack); the DOM half lives in `viewer.js`, as `splitdiff.js` did. `S` back opens the cursor's file through `openViewer` with a `from` built exactly as `steerSplit` builds it, so the overview's held D choice (`diffPref`) brings a diff back on its own.

**Tech Stack:** vanilla ES modules (gg web's static SPA), Go (`internal/web`, `internal/promptstate`), node-run pure-JS guards, Playwright for the visible check.

**Spec:** `docs/superpowers/specs/2026-10-11-web-overview-split-design.md` §11 (plus §9, §12, §13). User ruling 2026-10-12 (option A, do not re-ask): D acts on the single file only; the stack shows no file | diff switch; D in the stack says `this acts on a single file — S opens it`; S from a diff stacks FILES; S back returns to the single file in the overview's held D choice.

## Global Constraints

- Web strings are English (no i18n bundles on the web).
- Status-line words, verbatim: `review remarks are read-only` (exists), `notes are read-only in the stack — S opens the file`, `this acts on a single file — S opens it`.
- A file the review deleted shows `(deleted at the reviewed commit)` under its header.
- The pref is web-only, `stacked_review_files` in the web layout state (`promptstate.WebUI`), independent of the TUI's `tui_stacked_review_files`.
- At most 4 file reads in flight (spec §11). `stack.js`'s `STACK_MAX_IN_FLIGHT` is 3; this plan uses its own constant 4 (ruling R1 below).
- More than 100 files: all folded but the target (the stacked diff's and the TUI's rule).
- Never `git add -A`; `gg commit` has no `-F`: `gg add <files>` then `git commit -F <msgfile>`.
- Race gate: `env -u GG_INBOX -u GG_MCP_URL -u GG_SESSION_TOKEN -u GG_SESSION_ID ./test.sh race`, green only on `all green` in its log.
- The Playwright script deletes the `GG_*` variables from its environment and waits for the file before pressing keys.

## Rulings taken while planning

- R1 In flight: 4, per the spec's number (the spec's "the stacked diff's rule" names the lazy loader, whose own constant is 3). Cost if wrong: one constant.
- R2 `backIntoSplit` (backspace in a one-document file, the box wide) keeps the file it left in the right column even when the stack pref is on — the spec's table says the file it left is the right column. Cost if wrong: one branch.
- R3 A link that is `plain` or `missing`, or a note link whose note is gone, names no file for the stack. Cost: such a file is not stacked.
- R4 `S` back on a file the review deleted, or one whose read failed, stays in the stack and says `<path> is deleted at the reviewed commit` / the failure — there is no file to open.
- R5 `} {`, `f`, `/`, `@`, `L`, `.` and right-click in the stack say `this acts on a single file — S opens it` (the spec names `/`, `L`, `.`; the others are single-file keys too).
- R6 The gg-overview skill is not changed: it already says gg web shows the overview beside the file (plan 1), and S is a reader's key, not an agent's.
- R7 Agent notes in the stack are a snapshot taken when the file loads (the TUI's stack does the same); a live note change re-reads nothing in the stack.

## Review Focus

1. `S` in the split must never reach `keys.js`'s page-level `toggleStacked()` (it stacks the files pane behind the overlay) — the split's handler returns true for `S` in both columns. Browser check: `#diff-title` does not change.
2. A load that lands after `S` back, esc, a resize below the breakpoint or a new split must not paint (guard `split && split.stack === st`).
3. A step on the overview to a file that is still loading, then another step before it lands: the newest step wins (`st.want` replaced), the earlier one never moves the cursor.
4. The current band must be the steered link's band in ITS file only — `bandOf` matches by range, so the same range in another file must not light up (`stackCurFile` guard).
5. `S` back from a stack that was entered from a diff returns to a diff (`diffPref` kept across the stack, `from.id` unchanged).

---

### Task 1: the `stacked_review_files` pref

**Files:**
- Modify: `internal/promptstate/webui.go` (the `WebUI` struct)
- Modify: `internal/web/uistate.go` (`uiStateWire`, both handlers)
- Modify: `internal/web/static/uistate.js` (`saveUI`'s base object)
- Test: `internal/web/uistate_test.go` (the round-trip body at line 72 and its assertion at line 81)

**Interfaces:**
- Produces: `state.ui.stacked_review_files` (boolean) on the page; `saveUI({ stacked_review_files: bool })`.

- [ ] **Step 1: Write the failing test.** In `uistate_test.go`, add `"stacked_review_files":true` to the PUT body on line 72 and `|| !st.StackedReviewFiles` to the condition on line 81 (and name it in the error message).

- [ ] **Step 2: Run it.** `go test ./internal/web -run UIState -count=1`. Expected: build error, `st.StackedReviewFiles undefined`.

- [ ] **Step 3: Implement.**

`internal/promptstate/webui.go`, after `StackedDiff`:

```go
	// StackedReviewFiles is gg web's overview split with the stacked file
	// column (S): every file in one scroll instead of one at a time. The
	// TUI keeps its own (tui_stacked_review_files).
	StackedReviewFiles bool `toml:"stacked_review_files,omitempty"`
```

`internal/web/uistate.go`, in `uiStateWire` after `StackedDiff`:

```go
	// StackedReviewFiles is the overview split's stacked file column (S):
	// on or off (viewer.js).
	StackedReviewFiles bool `json:"stacked_review_files"`
```

and `StackedReviewFiles: st.StackedReviewFiles,` in `handleUIStateGet`'s literal, `StackedReviewFiles: in.StackedReviewFiles,` in `handleUIStateSet`'s.

`static/uistate.js`, in `saveUI`'s base after `stacked_diff: false,`: `stacked_review_files: false,`.

- [ ] **Step 4: Run it.** `go test ./internal/web ./internal/promptstate -count=1`. Expected: ok.

- [ ] **Step 5: Commit.** `gg add internal/promptstate/webui.go internal/web/uistate.go internal/web/static/uistate.js internal/web/uistate_test.go`, then `git commit -F <msg>` — "web: remember the overview split's stacked file column (stacked_review_files)".

---

### Task 2: the pure stack model (`splitstack.js`)

**Files:**
- Create: `internal/web/static/splitstack.js`
- Test: `internal/web/splitstackjs_test.go`

**Interfaces:**
- Produces (all exported, import-free):
  - `SPLIT_STACK_MAX_IN_FLIGHT = 4`, `SPLIT_STACK_FOLD_OVER = 100`
  - `anchorPath(a) → string` — the file a link names ("" for plain / missing / pathless)
  - `stackFiles(reviewFiles | null, anchors) → [{path, status, deleted}]`
  - `newSlots(files, target) → slots` — `{path, status, load: "idle"|"none", why, collapsed, lines: [], notes: [], remarks: [], n: 0, error: ""}`
  - `slotOf(slots, path) → k | -1`
  - `shows(slot) → bool` — the slot's lines are in the stack
  - `stackMove(slots, cur, delta) → {k, line}`
  - `stackEnd(slots) → {k, line}`
  - `nextHeader(slots, cur, dir) → {k, line: 0} | null`
  - `stackBands(slots, bandsOf) → [{k, start, end, i}]`
  - `stepStackBand(bands, cur, at, dir) → {i, wrapped}`
  - `restack(old, files) → slots`
  - `placeholderRows(slot) → number`
  - `stackTitle(label, k, n) → string`

- [ ] **Step 1: Write the failing test** `internal/web/splitstackjs_test.go`:

```go
package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// splitstack.js is the pure half of the overview split's stacked file
// column (spec 2026-10-11-web-overview-split §11): which files, the
// (file, line) cursor, header and band steps. Import-free: node runs it.
const splitStackHarness = `
import * as S from "./splitstack.mjs";
const out = {};
const anchors = [
  { dest: "a.txt:2", path: "a.txt", start: 2 },
  { dest: "b.txt:1", path: "b.txt", start: 1 },
  { dest: "a.txt:4", path: "a.txt", start: 4, end: 5 },
  { dest: "n1", note: "n1", path: "c.txt", start: 3 },
  { dest: "x", path: "x.txt", plain: true },
  { dest: "m", path: "m.txt", missing: true },
  { dest: "n2", note: "n2" },
];
// 1. first-linked order: duplicates once, a note link's file, plain / missing / pathless skipped
out.linked = S.stackFiles(null, anchors).map((f) => f.path).join(",");
// 2. the review's files in its order, D marked deleted
const rf = S.stackFiles([{ path: "z.txt", status: "M" }, { path: "gone.txt", status: "D" }, { path: "a.txt", status: "A" }], anchors);
out.review = rf.map((f) => f.path + ":" + f.status + ":" + f.deleted).join(",");
// 3. slots: a deleted file is never read and says why; over 100 files all fold but the target
const sl = S.newSlots(rf, 0);
out.deletedSlot = sl[1].load + "|" + sl[1].why;
out.smallOpen = sl.every((s) => !s.collapsed);
const many = S.newSlots(Array.from({ length: 101 }, (_, i) => ({ path: "f" + i, status: "", deleted: false })), 7);
out.manyFolded = many.filter((s) => s.collapsed).length + ":" + many[7].collapsed;
out.slotOf = S.slotOf(sl, "a.txt") + "," + S.slotOf(sl, "nope");
// 4. moves: header (0) then the shown lines; folded / unloaded files are their header only
const st = S.newSlots([{ path: "a" }, { path: "b" }, { path: "c" }], -1);
st[0].load = "ok"; st[0].lines = [{}, {}, {}];
st[2].load = "ok"; st[2].lines = [{}, {}];
const mv = (k, line, d) => { const c = S.stackMove(st, { k, line }, d); return c.k + ":" + c.line; };
out.moves = [mv(0, 0, 1), mv(0, 3, 1), mv(1, 0, 1), mv(2, 0, -1), mv(1, 0, -1), mv(0, 0, -1), mv(2, 2, 5), mv(0, 1, 6)].join(",");
out.end = JSON.stringify(S.stackEnd(st));
out.shows = [S.shows(st[0]), S.shows(st[1]), S.shows({ ...st[0], collapsed: true }), S.shows({ ...st[0], why: "(binary)" })].join(",");
// 5. N / P: the next header; back from inside a file stops on its own header first
const nh = (k, line, d) => JSON.stringify(S.nextHeader(st, { k, line }, d));
out.headers = [nh(0, 2, 1), nh(2, 0, 1), nh(2, 1, -1), nh(2, 0, -1), nh(0, 0, -1)].join(",");
// 6. bands across the stack in reading order; n / p from the current band, else from the cursor, wrapping
const bandsOf = (p, n) => (p === "a" ? [{ start: 1, end: 1, i: 0 }, { start: 3, end: 3, i: 2 }] : p === "c" ? [{ start: 2, end: 2, i: 1 }] : []);
const bands = S.stackBands(st, bandsOf);
out.bands = bands.map((b) => b.k + ":" + b.start + ":" + b.i).join(",");
const step = (cur, k, line, d) => { const r = S.stepStackBand(bands, cur, { k, line }, d); return r.i + (r.wrapped ? "w" : ""); };
out.steps = [step(0, 0, 1, 1), step(-1, 0, 2, 1), step(-1, 1, 0, 1), step(2, 2, 2, 1), step(-1, 0, 0, -1), step(-1, 2, 0, -1), step(0, 0, 1, -1)].join(",");
out.noBands = JSON.stringify(S.stepStackBand([], -1, { k: 0, line: 0 }, 1));
// 7. restack keeps loaded slots by path, drops gone ones, adds new idle ones
const re = S.restack(st, [{ path: "c" }, { path: "new" }, { path: "a" }]);
out.restack = re.map((s) => s.path + ":" + s.load).join(",") + "|" + (re[0] === st[2]);
// 8. placeholder rows, the title
out.ph = [S.placeholderRows({ n: 0 }), S.placeholderRows({ n: 40 }), S.placeholderRows({ n: 9000 })].join(",");
out.title = S.stackTitle("PR #7 review", 1, 3);
console.log(JSON.stringify(out));
`

func TestSplitStackJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "splitstack.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "splitstack.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(splitStackHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("not the harness JSON: %v\n%s", err, out)
	}
	want := map[string]any{
		"linked":      "a.txt,b.txt,c.txt",
		"review":      "z.txt:M:false,gone.txt:D:true,a.txt:A:false",
		"deletedSlot": "none|(deleted at the reviewed commit)",
		"smallOpen":   true,
		"manyFolded":  "100:false",
		"slotOf":      "2,-1",
		"moves":       "0:1,1:0,2:0,1:0,0:3,0:0,2:2,2:2",
		"end":         `{"k":2,"line":2}`,
		"shows":       "true,false,false,false",
		"headers":     `{"k":1,"line":0},null,{"k":2,"line":0},{"k":1,"line":0},null`,
		"bands":       "0:1:0,0:3:2,2:2:1",
		"steps":       "1,1,2,0w,2w,1,2w",
		"noBands":     `{"i":-1,"wrapped":false}`,
		"restack":     "c:ok,new:idle,a:ok|true",
		"ph":          "3,40,2000",
		"title":       "Files: PR #7 review · file 2/3",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}
```

How the expected values follow (for the reader checking them): `moves` — 0:0+1 → 0:1; 0:3 is a's last line, +1 → b's header 1:0; b is not loaded so 1:0+1 → 2:0; 2:0−1 → b's header 1:0; 1:0−1 → a's last line 0:3; 0:0−1 stays 0:0; 2:2+5 clamps at 2:2; 0:1+6 → 0:2,0:3,1:0,2:0,2:1,2:2. `steps` — cur 0 at 0:1 → 1; no cur at 0:2 → the first band after it (0:3) = 1; at b's header → c's band = 2; cur 2 at 2:2, +1 wraps → 0w; at 0:0 backwards nothing before → last, wrapped 2w; at c's header backwards → the band before it (0:3) = 1; cur 0 at 0:1, −1 wraps → 2w.

- [ ] **Step 2: Run it.** `go test ./internal/web -run TestSplitStackJS -count=1`. Expected: FAIL, `open static/splitstack.js: no such file or directory`.

- [ ] **Step 3: Implement** `internal/web/static/splitstack.js`:

```js
// splitstack.js — the overview split's stacked file column (spec
// 2026-10-11-web-overview-split §11): which files the stack holds and every
// (file, line) question it asks. The cursor is a (k, line) pair — the file's
// index and its 1-based line, 0 = the file's header — never one row number:
// line numbers repeat across files. Import-free on purpose, so
// splitstackjs_test.go runs it under node; the DOM half is viewer.js.

export const SPLIT_STACK_MAX_IN_FLIGHT = 4; // file reads out at once (spec §11)
export const SPLIT_STACK_FOLD_OVER = 100; // more files than this open folded, all but the target

// anchorPath is the file an overview link names: a file or line link's
// path, a note link's file while the note lives; "" for a link that cannot
// open (plain, missing) or a note that is gone.
export function anchorPath(a) {
  if (!a || a.plain || a.missing) return "";
  return a.path || "";
}

// stackFiles is the stack's files: the review's, in its order, when the
// page's review view is this overview's review (reviewFiles; status "D" =
// deleted at the reviewed commit), else the files the links name, each
// once, in the order first linked.
export function stackFiles(reviewFiles, anchors) {
  if (reviewFiles) return reviewFiles.filter((f) => f.path).map((f) => ({ path: f.path, status: f.status || "", deleted: f.status === "D" }));
  const seen = new Set(), out = [];
  for (const a of anchors || []) {
    const p = anchorPath(a);
    if (!p || seen.has(p)) continue;
    seen.add(p);
    out.push({ path: p, status: "", deleted: false });
  }
  return out;
}

// newSlots is a fresh stack over files: nothing read yet (a deleted file
// never is — it says why), all folded but target when there are many.
export function newSlots(files, target) {
  const fold = files.length > SPLIT_STACK_FOLD_OVER;
  return files.map((f, k) => ({
    path: f.path,
    status: f.status || "",
    load: f.deleted ? "none" : "idle",
    why: f.deleted ? "(deleted at the reviewed commit)" : "",
    collapsed: fold && k !== target,
    lines: [],
    notes: [],
    remarks: [],
    n: 0, // the line count once known: sizes the placeholder
    error: "",
  }));
}

export function slotOf(slots, path) {
  return slots.findIndex((s) => s.path === path);
}

// shows reports whether the slot's own lines are in the stack: unfolded,
// read, no placeholder, not empty.
export function shows(s) {
  return !!s && !s.collapsed && s.load === "ok" && !s.why && s.lines.length > 0;
}

// stackMove is the cursor moved delta places — a file's header, then its
// shown lines, file after file — clamped at both ends.
export function stackMove(slots, cur, delta) {
  let { k, line } = cur;
  const dir = delta > 0 ? 1 : -1;
  for (let left = Math.abs(delta); left > 0; left--) {
    if (dir > 0) {
      if (shows(slots[k]) && line < slots[k].lines.length) line++;
      else if (k + 1 < slots.length) {
        k++;
        line = 0;
      } else break;
    } else if (line > 0) line--;
    else if (k > 0) {
      k--;
      line = shows(slots[k]) ? slots[k].lines.length : 0;
    } else break;
  }
  return { k, line };
}

// stackEnd is the stack's last place: the last file's last shown line, else
// its header.
export function stackEnd(slots) {
  const k = Math.max(slots.length - 1, 0);
  return { k, line: shows(slots[k]) ? slots[k].lines.length : 0 };
}

// nextHeader is N (dir 1) / P (-1): the next file's header; going back from
// inside a file stops on its own header first; null at the ends.
export function nextHeader(slots, cur, dir) {
  if (dir > 0) return cur.k + 1 < slots.length ? { k: cur.k + 1, line: 0 } : null;
  if (cur.line > 0) return { k: cur.k, line: 0 };
  return cur.k > 0 ? { k: cur.k - 1, line: 0 } : null;
}

// stackBands is every band of the shown files in reading order, each with
// its file: bandsOf(path, nLines) is the viewer's anchorBands over the
// overview's links ({start, end, i}, sorted).
export function stackBands(slots, bandsOf) {
  const out = [];
  slots.forEach((s, k) => {
    if (!shows(s)) return;
    for (const b of bandsOf(s.path, s.lines.length)) out.push({ k, start: b.start, end: b.end, i: b.i });
  });
  return out;
}

// stepStackBand is n (dir 1) / p (-1) across the stack: from band cur when
// the cursor (at) is in it, else from the cursor; wraps (one band never
// "wraps"). i is -1 with no bands.
export function stepStackBand(bands, cur, at, dir) {
  const n = bands.length;
  if (!n) return { i: -1, wrapped: false };
  const inIt = (b) => b.k === at.k && b.start <= at.line && at.line <= b.end;
  if (cur >= 0 && cur < n && inIt(bands[cur])) {
    const next = cur + dir;
    return { i: ((next % n) + n) % n, wrapped: (next < 0 || next >= n) && n > 1 };
  }
  if (dir > 0) {
    const k = bands.findIndex((b) => b.k > at.k || (b.k === at.k && b.start > at.line));
    return k >= 0 ? { i: k, wrapped: false } : { i: 0, wrapped: n > 1 };
  }
  for (let k = n - 1; k >= 0; k--) if (bands[k].k < at.k || (bands[k].k === at.k && bands[k].start < at.line)) return { i: k, wrapped: false };
  return { i: n - 1, wrapped: n > 1 };
}

// restack is the stack after its overview's links changed: a file still
// listed keeps its slot (lines, fold, notes), a new one starts unread, a
// gone one leaves.
export function restack(old, files) {
  const by = new Map(old.map((s) => [s.path, s]));
  return newSlots(files, -1).map((s) => by.get(s.path) || s);
}

// placeholderRows sizes an unread file's placeholder: its line count when
// known (a file read before), else three rows; capped.
export function placeholderRows(s) {
  return s.n > 0 ? Math.min(s.n, 2000) : 3;
}

// stackTitle is the right column's title line in the stack.
export function stackTitle(label, k, n) {
  return "Files: " + label + " · file " + (k + 1) + "/" + n;
}
```

- [ ] **Step 4: Run it.** `go test ./internal/web -run TestSplitStackJS -count=1`. Expected: ok.

- [ ] **Step 5: Commit.** `gg add internal/web/static/splitstack.js internal/web/splitstackjs_test.go`; `git commit -F <msg>` — "web: the overview split's stack model (splitstack.js)".

---

### Task 3: the stack on screen — S, loading, S back

**Files:**
- Modify: `internal/web/static/viewer.js`
- Modify: `internal/web/static/style.css`
- Test: `internal/web/overviewsplitjs_test.go` (a new wiring test)

**Interfaces:**
- Consumes: Task 2's exports; `nextToLoad` is NOT used (its slot shape is the stacked diff's); Task 1's `state.ui.stacked_review_files` and `saveUI`.
- Produces (viewer.js, module-local): `split.stack = {slots, cur: {k, line}, curDest, near: Set, inFlight, want: {path, line, note} | null, observer}`; `enterStack(at)`, `leaveStack()`, `toggleSplitStack()`, `renderStack()`, `repaintStackFile(st, k)`, `pumpStack(st)`, `stackCursorEl()`, `paintStackCursor(block)`, `stackCurFile(st)`, `stackReviewFiles(sp)`; `const SINGLE_ONLY = "this acts on a single file — S opens it"`, `const STACK_READONLY = "notes are read-only in the stack — S opens the file"`.

- [ ] **Step 1: Write the failing wiring test** — append to `overviewsplitjs_test.go`:

```go
// Spec 2026-10-11 §11 (plan 2): S stacks the files in the right column and
// back; the stack is its own state (split.stack) with the single file
// stepped aside; loads are guarded by the stack they were started for; the
// pref is remembered; D and the single-file keys say so in the stack.
func TestOverviewSplitStackIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`from "./splitstack.js"`,
		`const SINGLE_ONLY = "this acts on a single file — S opens it";`,
		`const STACK_READONLY = "notes are read-only in the stack — S opens the file";`,
		`function toggleSplitStack()`,
		`function enterStack(at)`,
		`async function leaveStack(open = false)`,
		`if (split && split.stack) return renderStack();`,
		`if (!split || split.stack !== st) return; // the stack left the screen while this loaded`,
		`saveUI({ stacked_review_files: true });`,
		`saveUI({ stacked_review_files: false });`,
		`state.ui && state.ui.stacked_review_files`,
		`case "S": e.preventDefault(); toggleSplitStack(); return true;`,
		`{ root: $("viewer-body"), rootMargin: "100% 0px" }`,
		`st.inFlight < SPLIT_STACK_MAX_IN_FLIGHT`,
		`await openViewer({ ...target, from, steer: true });`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	// S back must keep the overview's D choice: entering the stack never clears it.
	i := strings.Index(v, "function enterStack(at)")
	j := strings.Index(v[i:], "\n}\n")
	if i < 0 || j < 0 || strings.Contains(v[i:i+j], "diffPref = null") {
		t.Error("enterStack must keep diffPref (S back returns to the held D choice)")
	}
	css := readStatic(t, "style.css")
	for _, want := range []string{`.vstk-head {`, `.vstk-head.vcur`, `.vstk-ph {`} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css lacks %q", want)
		}
	}
}
```

- [ ] **Step 2: Run it.** `go test ./internal/web -run TestOverviewSplitStackIsWired -count=1`. Expected: FAIL listing every missing string.

- [ ] **Step 3: Implement in viewer.js.**

Imports — add `saveUI` and the stack model:

```js
import { saveUI } from "./uistate.js";
import { SPLIT_STACK_MAX_IN_FLIGHT, anchorPath, newSlots, placeholderRows, restack, shows, slotOf, stackFiles, stackTitle } from "./splitstack.js";
```

(Task 4 adds `nextHeader, stackBands, stackEnd, stackMove, stepStackBand` to this import.)

Constants and doc next to `split`'s declaration:

```js
// split.stack (plan 2, spec §11) is the right column as the STACK: every
// file in one scroll (splitstack.js). {slots, cur: {k, line} — line 0 is a
// file's header — curDest (the current band's link), near (files near the
// view), inFlight, want (a steered place whose file is still loading:
// {path, line, note}), observer}. While it shows, view holds no file.
const SINGLE_ONLY = "this acts on a single file — S opens it";
const STACK_READONLY = "notes are read-only in the stack — S opens the file";
```

`leaveSplit` tears the stack down first:

```js
function leaveSplit() {
  if (split && split.stack) leaveStack();
  split = null;
  paintSplitFocus();
}
```

`openSplit`: the new-split steer consults the pref —

```js
  if (!same && steer) {
    if (state.ui && state.ui.stacked_review_files) {
      enterStack(null);
      steerSplit(); // into the stack (stackSteer)
    } else steerSplit();
  }
```

`renderViewer`'s first line:

```js
  if (split && split.stack) return renderStack();
```

`paintTitle`, before `if (view.ov)`:

```js
  if (split && split.stack) {
    const st = split.stack;
    el.style.paddingRight = "";
    el.title = split.ov.title;
    el.textContent = stackTitle(split.ov.title, st.cur.k, st.slots.length);
    return;
  }
```

(`paintModeSwitch()` already ran at its top and hides the switch: `canFlip()` is false with no `view.id`.)

`steerSplit`, after the `if (why) return opLine(why, false);` line:

```js
  if (sp.stack) return stackSteer(a);
```

Task 3's `stackSteer` (Task 4 keeps it):

```js
// stackSteer is a step on the overview while the stack shows (spec §11):
// the link's file unfolded and scrolled to — read first when it has not
// been — the cursor on the link's line and its band current. The newest
// step wins over one still waiting for its file.
function stackSteer(a) {
  const st = split.stack;
  const path = anchorPath(a);
  if (!path) return opLine("note " + a.note + " is gone", false);
  const k = slotOf(st.slots, path);
  if (k < 0) return opLine("no file " + path + " in the stack", false);
  const was = stackCurFile(st);
  st.curDest = a.note ? "" : a.dest;
  const s = st.slots[k];
  if (s.collapsed) s.collapsed = false;
  if (s.load === "idle" || s.load === "loading") {
    st.want = { path, line: a.start || 0, note: a.note || "" };
    st.cur = { k, line: 0 };
    repaintStackFile(st, k);
    if (was >= 0 && was !== k) repaintStackFile(st, was);
    paintStackCursor("start");
    return pumpStack(st);
  }
  st.want = null;
  st.cur = { k, line: shows(s) ? clampLine(stackNoteLine(s, a.note) || a.start || 1, s.lines.length) : 0 };
  repaintStackFile(st, k);
  if (was >= 0 && was !== k) repaintStackFile(st, was);
  paintStackCursor("center");
}

// stackNoteLine is the line note id sits on in slot s (0 = not there).
function stackNoteLine(s, id) {
  if (!id) return 0;
  const n = s.notes.find((x) => x.id === id);
  return n ? n.start : 0;
}

// stackCurFile is the file of the current band's link, or -1: bandOf
// matches a band by its range, so only that file may light one current.
function stackCurFile(st) {
  const a = st.curDest ? split.ov.anchors.find((x) => x.dest === st.curDest) : null;
  return a ? slotOf(st.slots, anchorPath(a)) : -1;
}
```

The stack's core, a new section after `enterSplitFile`:

```js
// ---- the stacked file column (plan 2, spec §11) ----------------------------
// stackReviewFiles is the review's files when the page's review view is this
// overview's review, else null (the links name the files).
function stackReviewFiles(sp) {
  const rv = state.review, st = sp.ov.stored;
  return rv && st && "review-overview:" + rv.id === st.id ? rv.files || [] : null;
}

// toggleSplitStack is S from either column: the single file ⇄ the stack,
// remembered for the next split.
function toggleSplitStack() {
  if (split.stack) {
    const st = split.stack, s = st.slots[st.cur.k];
    if (s && s.why && s.load !== "ok") return opLine(s.status === "D" ? s.path + " is deleted at the reviewed commit" : s.path + ": " + s.why, false); // nothing to open (R4)
    saveUI({ stacked_review_files: false });
    return leaveStack(true);
  }
  saveUI({ stacked_review_files: true });
  const a = split.ov.anchors[split.ov.sel];
  // The stack opens on the file the right column shows (its lines come
  // along: no second read), else on the selected link's.
  if (view.id && !view.ov) return enterStack({ path: view.path, line: view.cur, dest: view.from ? view.from.cur || "" : "", file: view });
  enterStack(a ? { path: anchorPath(a), line: a.start || 0, dest: a.note ? "" : a.dest } : null);
}

// enterStack puts the stack in the right column, its cursor on at's file
// and line (null: the first header). The single file steps aside to the
// background (the stack is no open file); the overview's D choice is kept
// for S back.
function enterStack(at) {
  const sp = split;
  const files = stackFiles(stackReviewFiles(sp), sp.ov.anchors);
  const k = at ? slotOf(files, at.path) : -1;
  const st = { slots: newSlots(files, k), cur: { k: Math.max(k, 0), line: 0 }, curDest: (at && at.dest) || "", near: new Set(), inFlight: 0, want: null, observer: null };
  const s = k >= 0 ? st.slots[k] : null;
  if (s && at.file && s.load === "idle") { // the single file's read moves into its slot
    Object.assign(s, { load: "ok", lines: at.file.lines, n: at.file.lines.length, notes: at.file.notes, remarks: at.file.remarks, why: at.file.placeholder || (at.file.image ? "(an image — S opens it)" : "") });
    st.cur.line = shows(s) ? clampLine(at.line || 1, s.lines.length) : 0;
  } else if (s && at.line) st.want = { path: s.path, line: at.line, note: "" };
  if (view.id) {
    rememberPlace();
    ofQueue({ op: "background", id: view.id, line: view.cur });
  }
  loadSeq++; // an open in flight must not land over the stack
  Object.assign(view, { id: "", ov: null, src: "worktree", rev: "", path: "", lines: [], notes: [], remarks: [], cur: 0, placeholder: "", image: null, from: null, range: null, rangeOwn: false, diff: null });
  viewerSearchBar.reset();
  sp.stack = st;
  paintTitle();
  renderViewer();
  paintStackCursor("center");
  swapFoot(true);
}

// leaveStack takes the stack down; open opens the cursor's file as the
// single file (S back) — through the split's own steering, so the
// overview's held D choice brings its diff back.
async function leaveStack(open = false) {
  const st = split.stack;
  if (st.observer) st.observer.disconnect();
  split.stack = null;
  if (!open) return;
  const sp = split, s = st.slots[st.cur.k];
  if (!s) {
    view.placeholder = "(no link in this overview opens a file)";
    paintTitle();
    renderViewer();
    return swapFoot(true);
  }
  const sel = sp.ov.anchors[sp.ov.sel];
  const cur = stackCurFile(st) === st.cur.k ? st.curDest : "";
  const dest = cur || (sel && anchorPath(sel) === s.path ? sel.dest : "");
  const from = { id: sp.id || sp.ov.stored.id, sel: sp.ov.sel, dest, anchors: sp.ov.anchors, stamp: sp.ov.stamp, cur };
  if (sp.ov.stored) from.stored = sp.ov.stored;
  const target = sp.ov.stored ? storedAnchorOpen(sp.ov.stored.tip, { path: s.path, start: st.cur.line }) : { src: "worktree", path: s.path, line: st.cur.line };
  view.placeholder = "(opening " + s.path + "…)";
  paintTitle();
  renderViewer();
  swapFoot(true);
  await openViewer({ ...target, from, steer: true });
}

// stackCols is how many characters a header's path may take.
function stackCols() {
  return Math.max(20, Math.floor(($("viewer-body").clientWidth - 70) / charWidth()));
}

// renderStack paints every file's section and watches them come near.
function renderStack() {
  const st = split.stack, body = $("viewer-body");
  if (!st.slots.length) {
    body.innerHTML = `<div class="notice">(no link in this overview names a file)</div>`;
    return syncHere();
  }
  const cols = stackCols();
  body.innerHTML = `<div class="vstk">${st.slots.map((_, k) => stackSectionHTML(st, k, cols)).join("")}</div>`;
  observeStack(st);
  syncHere();
}

// stackSectionHTML is file k's section: its header, then its lines (bands,
// read-only boxes) or what stands in for them.
function stackSectionHTML(st, k, cols) {
  const s = st.slots[k], here = st.cur.k === k && st.cur.line === 0;
  const head =
    `<div class="vstk-head${here ? " vcur" : ""}" data-k="${k}"><span class="vstk-fold" title="fold (-)">${s.collapsed ? "▸" : "▾"}</span>` +
    (s.status ? `<span class="st ${esc(s.status)}">${esc(s.status)}</span>` : "") +
    `<span class="vstk-path" title="${esc(s.path)}">${esc(elidePath(s.path, cols))}</span></div>`;
  let inner = "";
  if (s.collapsed) inner = "";
  else if (s.why) inner = `<div class="notice">${esc(s.why)}</div>`;
  else if (s.load === "error") inner = `<div class="notice">${esc(s.error)}</div>`;
  else if (s.load !== "ok") inner = `<div class="vstk-ph" style="height:${placeholderRows(s) * stackRowPx()}px">loading…</div>`;
  else inner = stackLinesHTML(st, k);
  return `<section class="vstk-file" data-k="${k}">${head}${inner}</section>`;
}

// stackRowPx is one line's height (a drawn line's, else the 12px font's).
function stackRowPx() {
  const row = $("viewer-body").querySelector(".vline");
  return row ? row.offsetHeight : 17;
}

// stackLinesHTML is file k's lines: syntax tokens as the single file, the
// overview's bands (the current one only in its own file), every box
// read-only (spec §11).
function stackLinesHTML(st, k) {
  const s = st.slots[k], anchors = split.ov.anchors, n = s.lines.length;
  const bands = anchorBands(anchors, s.path, n);
  const curB = stackCurFile(st) === k ? bandOf(bands, anchors, st.curDest, n) : -1;
  const boxes = mergeNotes(s.notes, s.remarks);
  let html = "";
  s.lines.forEach((l, i) => {
    const bk = bandKindAt(bands, curB, i + 1);
    html +=
      `<div class="vline${st.cur.k === k && st.cur.line === i + 1 ? " vcur" : ""}${noteAtLine(boxes, i + 1) ? " vnoted" : ""}${bk ? " vanchor" + (bk === "cur" ? " acur" : "") : ""}" data-k="${k}" data-i="${i}">` +
      `<span class="vno">${i + 1}</span><span class="vtext">${renderCell(l.text, null, l.tok, "", null) || " "}</span></div>`;
    for (const b of boxesAfter(boxes, i)) html += noteBoxHTML(b, false);
  });
  return html;
}

// repaintStackFile repaints file k's section alone, keeping what the
// reader looks at where it is when the section sits above it.
function repaintStackFile(st, k) {
  const body = $("viewer-body"), old = body.querySelector(`.vstk-file[data-k="${k}"]`);
  if (!old) return;
  const top = body.getBoundingClientRect().top;
  let ref = null;
  for (const el of body.querySelectorAll(".vstk-file")) {
    if (el.getBoundingClientRect().bottom > top) {
      ref = el;
      break;
    }
  }
  const above = ref && Number(ref.dataset.k) > k, off = above ? ref.getBoundingClientRect().top - top : 0;
  const t = document.createElement("template");
  t.innerHTML = stackSectionHTML(st, k, stackCols());
  const el = t.content.firstElementChild;
  old.replaceWith(el);
  if (st.observer) {
    st.observer.unobserve(old);
    st.observer.observe(el);
  }
  if (above) body.scrollTop += ref.getBoundingClientRect().top - top - off;
}

// observeStack watches the sections come near the view (a screen above and
// below): those load (pumpStack).
function observeStack(st) {
  if (st.observer) st.observer.disconnect();
  st.near = new Set();
  st.observer = new IntersectionObserver(
    (entries) => {
      if (!split || split.stack !== st) return;
      for (const e of entries) {
        const k = Number(e.target.dataset.k);
        if (e.isIntersecting) st.near.add(k);
        else st.near.delete(k);
      }
      pumpStack(st);
    },
    { root: $("viewer-body"), rootMargin: "100% 0px" }
  );
  for (const el of $("viewer-body").querySelectorAll(".vstk-file")) st.observer.observe(el);
}

// pumpStack starts reads while fewer than SPLIT_STACK_MAX_IN_FLIGHT are out:
// a steered file first, then the near file closest to the cursor's.
function pumpStack(st) {
  while (split && split.stack === st && st.inFlight < SPLIT_STACK_MAX_IN_FLIGHT) {
    const w = st.want ? slotOf(st.slots, st.want.path) : -1;
    let k = w >= 0 && st.slots[w].load === "idle" && !st.slots[w].collapsed ? w : -1;
    if (k < 0) {
      let best = Infinity;
      for (const i of st.near) {
        const s = st.slots[i];
        if (!s || s.collapsed || s.load !== "idle") continue;
        const d = Math.abs(i - st.cur.k);
        if (d < best || (d === best && i > k)) {
          best = d;
          k = i;
        }
      }
    }
    if (k < 0) return;
    loadStackFile(st, k);
  }
}

// loadStackFile reads file k at the overview's version: the reviewed tip
// with the review's remarks (a stored overview), else the working tree with
// the agent's notes.
async function loadStackFile(st, k) {
  const s = st.slots[k], sp = split;
  s.load = "loading";
  st.inFlight++;
  const at = sp.ov.stored ? storedAnchorOpen(sp.ov.stored.tip, { path: s.path }) : { src: "worktree", rev: "" };
  let body = null;
  try {
    body = await fetchContent(at.src, at.rev, s.path, sp.ov.stored ? remarksReview({ stored: sp.ov.stored }) : "");
  } catch (e) {
    s.error = "(load failed: " + (e.message || e) + ")";
  }
  if (!split || split.stack !== st) return; // the stack left the screen while this loaded
  st.inFlight--;
  if (!body) s.load = "error";
  else {
    s.load = "ok";
    s.lines = body.lines || [];
    s.n = s.lines.length;
    s.notes = notesOf(body, at.src);
    s.remarks = body.remarks || [];
    s.why = placeholderFor(body, s.lines) || (imageOf(body, at.src, at.rev, s.path) ? "(an image — S opens it)" : "");
  }
  landStackFile(st, k);
  pumpStack(st);
}

// landStackFile paints file k's arrival: a step waiting for it lands (the
// cursor on its line, centred), else the reader stays where they are.
function landStackFile(st, k) {
  const w = st.want, s = st.slots[k];
  if (w && w.path === s.path) {
    st.want = null;
    st.cur = { k, line: shows(s) ? clampLine(stackNoteLine(s, w.note) || w.line || 1, s.lines.length) : 0 };
    repaintStackFile(st, k);
    return paintStackCursor("center");
  }
  repaintStackFile(st, k);
  syncHere();
}

// stackCursorEl is the cursor's element: a line, or a file's header.
function stackCursorEl() {
  const c = split.stack.cur, body = $("viewer-body");
  return c.line ? body.querySelector(`.vline[data-k="${c.k}"][data-i="${c.line - 1}"]`) : body.querySelector(`.vstk-head[data-k="${c.k}"]`);
}

// paintStackCursor moves the cursor's mark and brings it into view
// (block: "nearest" for a step, "center" / "start" for a landing).
function paintStackCursor(block = "nearest") {
  for (const el of $("viewer-body").querySelectorAll(".vcur")) el.classList.remove("vcur");
  const el = stackCursorEl();
  if (el) {
    el.classList.add("vcur");
    el.scrollIntoView({ block });
  }
  paintTitle();
  syncHere();
}
```

`noteBoxHTML` gains its buttons switch (a stack's box is read-only whoever wrote it):

```js
function noteBoxHTML(n, acts = !n.readonly) {
  ...
    (acts ? `<div class="vnote-acts"><button data-nact="dismiss">dismiss</button><button data-nact="ref">copy reference</button></div>` : "") +
```

`splitKey` takes `S` (and D in the stack) before either column's keys:

```js
function splitKey(e) {
  if (e.key === "Tab") { ... }
  switch (e.key) {
    case "S": e.preventDefault(); toggleSplitStack(); return true;
    case "D": if (!split.stack) break; e.preventDefault(); opLine(SINGLE_ONLY, false); return true;
  }
  if (split.left) return splitOverviewKey(e);
  ...
```

`style.css`, after the `.vline.vd-*` rules:

```css
/* the overview split's stacked file column (plan 2) */
.vstk-head { display: flex; gap: 8px; align-items: baseline; margin-top: 10px; padding: 2px 8px; border-top: 1px solid var(--border); border-bottom: 1px solid var(--border); background: var(--bg-alt); font-weight: 600; cursor: default; white-space: pre; }
.vstk-file:first-child .vstk-head { margin-top: 0; border-top: 0; }
.vstk-head.vcur { background: var(--sel); }
.vstk-fold { color: var(--dim); cursor: pointer; }
.vstk-ph { color: var(--dim); padding: 2px 4.6em; }
```

- [ ] **Step 4: Run it.** `go test ./internal/web -count=1`. Expected: ok (the new wiring test and every older viewer guard).

- [ ] **Step 5: Commit.** `gg add internal/web/static/viewer.js internal/web/static/style.css internal/web/overviewsplitjs_test.go`; `git commit -F <msg>` — "web: S stacks the overview split's files (lazy, bands, read-only boxes) and back".

---

### Task 4: moving in the stack — keys, mouse, the mark, the bars

**Files:**
- Modify: `internal/web/static/viewer.js`
- Test: `internal/web/overviewsplitjs_test.go` (extend the Task 3 test's list)

**Interfaces:**
- Consumes: Task 2 (`nextHeader`, `stackBands`, `stackEnd`, `stackMove`, `stepStackBand`), Task 3's state and painters.
- Produces: `stackKey(e)`, `stepStackAnchor(dir)`, `foldStackFile()`, `foldAllStack()`, the stack branch of `syncHere`, the stack branch of the body's click / contextmenu, `viewerFoot`'s stack chips, the help sentence, `refreshSplitOverview`'s restack.

- [ ] **Step 1: Write the failing test** — add to `TestOverviewSplitStackIsWired`'s list:

```go
		`function stackKey(e)`,
		`if (split.stack) return stackKey(e);`,
		`case "N": case "P": { const h = nextHeader(st.slots, st.cur, e.key === "N" ? 1 : -1); if (h) { st.cur = h; paintStackCursor("start"); } break; }`,
		`case "/": case "@": case "L": case ".": case "}": case "{": case "f": opLine(SINGLE_ONLY, false); break;`,
		`opLine(STACK_READONLY, false)`,
		`function stepStackAnchor(dir)`,
		`function foldStackFile()`,
		`function foldAllStack()`,
		`st.slots = restack(st.slots, stackFiles(stackReviewFiles(sp), sp.ov.anchors));`,
		`<button data-vact="stack">S `,
		`<b>S</b> stacks`,
```

- [ ] **Step 2: Run it.** `go test ./internal/web -run TestOverviewSplitStackIsWired -count=1`. Expected: FAIL naming the new strings.

- [ ] **Step 3: Implement.**

Extend the splitstack import with `nextHeader, stackBands, stackEnd, stackMove, stepStackBand`.

`splitKey`: after `if (split.left) return splitOverviewKey(e);` add `if (split.stack) return stackKey(e);`.

```js
// stackKey is a key on the stack (spec §11): lines across files, bands,
// headers, folds; the single-file keys say S opens the file.
function stackKey(e) {
  const st = split.stack;
  if (!st.slots.length && e.key !== "Escape" && e.key !== "Backspace") return false;
  switch (e.key) {
    case "Backspace": focusSplit(true); break;
    case "ArrowDown": case "j": st.cur = stackMove(st.slots, st.cur, 1); paintStackCursor(); break;
    case "ArrowUp": case "k": st.cur = stackMove(st.slots, st.cur, -1); paintStackCursor(); break;
    case "PageDown": case " ": st.cur = stackMove(st.slots, st.cur, pageRows()); paintStackCursor(); break;
    case "PageUp": st.cur = stackMove(st.slots, st.cur, -pageRows()); paintStackCursor(); break;
    case "Home": case "g": st.cur = { k: 0, line: 0 }; paintStackCursor(); break;
    case "End": case "G": st.cur = stackEnd(st.slots); paintStackCursor(); break;
    case "N": case "P": { const h = nextHeader(st.slots, st.cur, e.key === "N" ? 1 : -1); if (h) { st.cur = h; paintStackCursor("start"); } break; }
    case "n": stepStackAnchor(1); break;
    case "p": stepStackAnchor(-1); break;
    case "-": foldStackFile(); break;
    case "_": foldAllStack(); break;
    case "/": case "@": case "L": case ".": case "}": case "{": case "f": opLine(SINGLE_ONLY, false); break;
    case "d": case "r": {
      const s = st.slots[st.cur.k];
      opLine(s && st.cur.line && noteAtLine(mergeNotes(s.notes, s.remarks), st.cur.line) ? STACK_READONLY : "no note on this line", false);
      break;
    }
    case "w": cycleTextMode(); break;
    case "Escape": closeViewer("background"); break; // W3
    default: return false;
  }
  e.preventDefault();
  return true;
}

// stepStackAnchor is n / p in the stack: the next / previous band across
// the read files, the cursor on its first line, the band current — the
// overview's selection stays (W5).
function stepStackAnchor(dir) {
  const st = split.stack, anchors = split.ov.anchors;
  const bands = stackBands(st.slots, (p, n) => anchorBands(anchors, p, n));
  if (!bands.length) return opLine("no links in the files read so far", false);
  const ck = stackCurFile(st);
  let cur = -1;
  if (ck >= 0 && shows(st.slots[ck])) {
    const s = st.slots[ck], fb = anchorBands(anchors, s.path, s.lines.length), b = fb[bandOf(fb, anchors, st.curDest, s.lines.length)];
    if (b) cur = bands.findIndex((x) => x.k === ck && x.start === b.start && x.end === b.end);
  }
  const r = stepStackBand(bands, cur, st.cur, dir), b = bands[r.i], a = anchors[b.i];
  st.curDest = a.dest;
  st.cur = { k: b.k, line: b.start };
  repaintStackFile(st, b.k);
  if (ck >= 0 && ck !== b.k) repaintStackFile(st, ck);
  paintStackCursor("center");
  opLine("anchor " + (r.i + 1) + "/" + bands.length + " in the stack · " + (a.label || a.dest) + (r.wrapped ? " · wrapped" : ""), false);
}

// foldStackFile is -: the cursor's file folds to its header (or unfolds).
function foldStackFile() {
  const st = split.stack, k = st.cur.k, s = st.slots[k];
  s.collapsed = !s.collapsed;
  st.cur = { k, line: 0 };
  repaintStackFile(st, k);
  paintStackCursor();
  pumpStack(st);
}

// foldAllStack is _: every file folds, or — all folded — every file opens.
function foldAllStack() {
  const st = split.stack, fold = st.slots.some((s) => !s.collapsed);
  for (const s of st.slots) s.collapsed = fold;
  st.cur = { k: st.cur.k, line: 0 };
  renderStack();
  paintStackCursor("start");
}
```

`syncHere` — the stack branch at its top:

```js
function syncHere() {
  if (!split) return;
  let here;
  if (split.stack) {
    const st = split.stack, s = st.slots[st.cur.k], anchors = split.ov.anchors;
    here = -1;
    if (shows(s) && st.cur.line > 0) {
      const n = s.lines.length, bands = anchorBands(anchors, s.path, n);
      const cur = stackCurFile(st) === st.cur.k ? bandOf(bands, anchors, st.curDest, n) : -1;
      const sel = anchors[split.ov.sel];
      const selB = sel && anchorPath(sel) === s.path ? bandOf(bands, anchors, sel.dest, n) : -1;
      here = hereOf(bands, cur, st.cur.line, selB);
    }
  } else {
    const f = view.from, bands = viewBands(), n = view.lines.length;
    const cur = f ? bandOf(bands, f.anchors, f.cur, n) : -1;
    const sel = split.ov.anchors[split.ov.sel];
    const selB = sel && f ? bandOf(bands, f.anchors, sel.dest, n) : -1;
    here = hereOf(bands, cur, view.cur, selB);
  }
  if (here === split.here) return;
  split.here = here;
  paintAnchors();
}
```

`#viewer-body` click — after `if (split && split.left) focusSplit(false);`:

```js
  if (split && split.stack) { // the stack's own lines and headers (spec §11)
    const st = split.stack, head = e.target.closest(".vstk-head"), row = e.target.closest(".vline[data-k]");
    const picked = getSelection();
    if (!picked.isCollapsed && $("viewer-body").contains(picked.anchorNode)) return;
    if (head) {
      st.cur = { k: Number(head.dataset.k), line: 0 };
      if (e.target.closest(".vstk-fold")) return foldStackFile();
    } else if (row) st.cur = { k: Number(row.dataset.k), line: Number(row.dataset.i) + 1 };
    else return;
    return paintStackCursor();
  }
```

`#viewer-body` contextmenu — first lines:

```js
  if (split && split.stack) {
    e.preventDefault();
    return opLine(SINGLE_ONLY, false);
  }
```

`enterSplitFile` — first line: `if (split.stack) return focusSplit(false); // the step already scrolled the stack`.

`refreshSplitOverview` — after `renderSplitOverview();`:

```js
  if (sp.stack) { // the links moved: the files they name follow, the read ones kept
    const st = sp.stack, path = st.slots[st.cur.k]?.path;
    st.slots = restack(st.slots, stackFiles(stackReviewFiles(sp), sp.ov.anchors));
    const k = path ? slotOf(st.slots, path) : -1;
    st.cur = k >= 0 ? { k, line: st.cur.line } : { k: 0, line: 0 };
    if (st.want && slotOf(st.slots, st.want.path) < 0) st.want = null;
    rerenderKeepingScroll();
    paintStackCursor();
  }
```

`viewerFoot` — the S chip on the overview's bar (before `diffChips()`): `` `<button data-vact="stack">S ${split.stack ? "single file" : "stack files"}</button>` ``; a new branch before `if (split) {` for the stack:

```js
  if (split && split.stack) {
    return (
      `<button data-vact="sov">tab overview</button><span>N P files</span><span>n p bands</span>` +
      `<button data-vact="sfold">- fold</button><button data-vact="sfoldall">_ fold all</button><button data-vact="stack">S single file</button>` +
      `<span>↑↓ j k line</span><button data-vact="wrap">w long lines</button><button data-vact="close">esc close</button><button data-vact="files">ctrl+\\ open files</button>`
    );
  }
```

and in the single-file split branch, `` `<button data-vact="stack">S stack files</button>` `` after `bandKeysS`. The footer click switch gains:

```js
    case "stack": if (split) toggleSplitStack(); break;
    case "sfold": if (split && split.stack) foldStackFile(); break;
    case "sfoldall": if (split && split.stack) foldAllStack(); break;
```

Help (`registerHelp` "view file"), after "…a click on a link shows its file; ": `<b>S</b> stacks every file of the review (or every file the links name) in one scroll beside the overview — <b>↑↓</b> lines across files, <b>N / P</b> file headers, <b>n / p</b> bands, <b>- / _</b> fold one / all, the review's remarks and an agent's notes read-only, <b>S</b> again back to the single file at the cursor; `.

- [ ] **Step 4: Run it.** `go test ./internal/web -count=1`. Expected: ok.

- [ ] **Step 5: Commit.** `gg add internal/web/static/viewer.js internal/web/overviewsplitjs_test.go`; `git commit -F <msg>` — "web: keys, mouse, the mark and the bars in the overview split's stack".

---

### Task 5: the visible check, the docs, the gate

**Files:**
- Create (scratch, not committed): `$CLAUDE_JOB_DIR/tmp/websplitdiff/stack.mjs` (a copy of `check.mjs` with the scenarios below)
- Modify: `CHANGELOG.md`, `README.md` (the gg web overview paragraph), `docs/CLAUDE-details.md` (a "gg web: the stacked file column" paragraph after the plan-1 web paragraphs)

- [ ] **Step 1: Build.** `go build -o bin/gg ./cmd/gg` in the worktree.

- [ ] **Step 2: Write the scenarios** (the fixture is `check.mjs`'s: a.txt, b.txt, r.txt, gone.txt deleted, c1/x.txt, c2/x.txt; the stored overview links a.txt:2, a.txt:4-5, b.txt:2, c1/x.txt:1, c2/x.txt:1):
  - `stack`: open the stored overview at 1400px, wait for `View a.txt`; read `#diff-title`'s text; press `S`. Visible: a `.vstk-head` for each of a.txt, b.txt, gone.txt, c1/x.txt, c2/x.txt; `(deleted at the reviewed commit)` under gone.txt's header; `#viewer-title` starts `Files: `; `#viewer-mode` hidden; the remark `why two?` visible as a `.vnote` with no button; `#diff-title` unchanged.
  - `headers`: `Tab` into the stack; `N` → the cursor (`.vstk-head.vcur`) is b.txt's header (the file after a.txt in the review's order — check the order the fixture's review lists); `P` → back on a.txt's header.
  - `steer`: in the stack, `Tab` to the overview, `ArrowDown` twice (to `beta`, b.txt:2) → `.vline.vcur.vanchor.acur` with text `beta` visible.
  - `dInStack`: `D` → `#op-text` says `this acts on a single file — S opens it`; `/` says the same.
  - `diffBack`: fresh page; open; `D` → `Diff a.txt (`; `S` → headers, no `.vd-del` row in the stack; `S` → `#viewer-title` starts `Diff a.txt (` again.
  - `pref`: after `S` on one page, a new page, open the stored overview → `.vstk-head` visible without pressing anything; `S` → single file; a new page opens single (`View a.txt`).
  - `narrow`: stack open at 1400px, `setViewportSize(800, 900)` → the one-document overview (`#viewer-body .vdoc`) and no `.vstk-head`.
- [ ] **Step 3: Run it.** `cd $CLAUDE_JOB_DIR/tmp/websplitdiff && node stack.mjs <worktree>/bin/gg`. Expected: `all checks passed`. A failure is debugged (systematic-debugging) and fixed with a guard test first.
- [ ] **Step 4: Docs.**
  - CHANGELOG, a new top section `## gg web: the overview split stacks its files (S)` with an `### Added` entry: S from either column of the overview split turns the file column into one scroll of every file — the review's files in its order when the review is open, else the files the links name — each under a header with its status letter and path (`(deleted at the reviewed commit)` for one the review deleted), read as they come near, with the links' bands and the review's remarks (or the agent's notes) as read-only boxes; ↑↓ move across files, N / P step the headers, n / p the bands, - / _ fold, a step on the overview scrolls the stack to its line, S again opens the file at the cursor (as a diff when D was on); D, /, L and the . menu say they act on a single file; remembered across splits (`stacked_review_files`).
  - README, after the gg web split sentence ("…the link whose band holds the cursor is marked."): "`S` stacks every file in one scroll beside the overview (`N`/`P` headers, `-`/`_` fold, `S` back)."
  - CLAUDE-details: a paragraph "**gg web: the stacked file column (2026-10-12, spec §11, plan 2).**" naming `split.stack`, the `(k, line)` cursor, `splitstack.js`, the single file stepped aside (view empty), `stackCurFile`'s guard, the 4-read loader with `want`, `S` back through `openViewer({…, from, steer: true})` keeping `diffPref`, the pref, and the tests.
- [ ] **Step 5: The gate.** `env -u GG_INBOX -u GG_MCP_URL -u GG_SESSION_TOKEN -u GG_SESSION_ID ./test.sh race > <ws>/race.log 2>&1`; `grep -c "all green" <ws>/race.log` → 1.
- [ ] **Step 6: Commit.** `gg add CHANGELOG.md README.md docs/CLAUDE-details.md`; `git commit -F <msg>` — "docs: gg web's overview split stacks its files (S)".
