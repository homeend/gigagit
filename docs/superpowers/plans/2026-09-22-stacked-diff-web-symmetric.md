# Stacked diff view — plan 2: web symmetric compare — Implementation Plan

> **For agentic workers:** this repo FORBIDS subagents (CLAUDE.md "NEVER USE
> SUB AGENTS"). Execute with superpowers:executing-plans, task by task, in THIS
> session. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The `S` stacked view works inside the web symmetric compare: both
aligned lists stay as navigators, the middle column is one scroll of the
VISIBLE rows (filter 1–4 and `x` flip respected), each section fetches the pair
`openSymRow` asks for today, and a row neither set has content for is a header
with today's "neither set has content — …" notice.

**Architecture:** Plan 1's stack (`stack.js` pure, `stackview.js` DOM) already
builds from `activeFileList()`, which in the symmetric view IS `symRows(c)` —
the visible, direction-aware rows. What is missing is (1) the pair a sym row
diffs, pulled out of `openSymRow` into a pure `symPair(f, c)` in `stack.js`
and used by `fileDiffURL` too, so a slot and the single-file view can never
fetch different diffs; (2) sym-aware slots and headers (glyph, each side's
state, the no-content body); (3) lifting `stackOn()`'s `!symActive()` and the
matching guard in `toggleStacked`; (4) the rebuild paths — filter, flip, the
`v` toggle and a live link refresh hand out a NEW `state.files` array, which
is already how the stack knows to rebuild; this plan makes each of them keep
the reader's file, including the case where a second `openFile` lands while
the new stack is still awaiting its first paint.

**Tech Stack:** vanilla ES modules; node (the `stack.js` harness driven from
Go); Go string guards; Playwright (browser probe, scratchpad only).

**Spec:** `docs/superpowers/specs/2026-09-22-stacked-diff-view-design.md`
(§10, §7 "Symmetric headers", §4 toggle). Plan 1:
`docs/superpowers/plans/2026-09-22-stacked-diff-web-core.md`.

## Global Constraints

- Keys unchanged: `S` toggle, `-` fold current, `_` fold all; the view's own
  `v` / `x` / `1`–`4` keep their meaning. No `n`/`p` on the web.
- Pref `stacked_diff` (web `/api/uistate`) and `sym_compare` stay independent.
- No new endpoint: a sym slot fetches `/api/entry-diff` through `fileDiffURL(f)`.
- Below `SYM_MIN_WIDTH` (1200px) the classic list takes over; it stacks too.
- The lists' mirrored scroll and row alignment (`syncGeometry`) stay untouched.
- `stack.js` stays import-free (node-run by `stackjs_test.go`).

## File map

| File | Change |
|------|--------|
| `internal/web/static/stack.js` | + `symPair(f, c)`, `noContentWhy(f)`, `GLYPH`, `KIND_TIP`; `buildSlots` carries `kind`/`left`/`right` and a `none` reason (`conflict` \| `empty`) |
| `internal/web/stackjs_test.go` | harness cases for the above |
| `internal/web/static/symcompare.js` | `openSymRow` uses `symPair`/`noContentWhy`; `GLYPH`/`KIND_TIP` move to stack.js; `setFilter` keeps the reader's file under a stack; `applySym`'s classic branch re-opens the kept file |
| `internal/web/static/files.js` | `fileDiffURL` sym arm; `applyCompareFilter` empty arm tears the stack down; `updateLinkCompareFiles` rebuilds a stack on the cursor's file |
| `internal/web/static/stackview.js` | `stackOn`/`toggleStacked` lift; sym header + no-content body; pending-paint anchor (`st.painted`/`st.want`); list highlight scrolls into view |
| `internal/web/static/style.css` | `.stk-sides`, header `.symg` spacing |
| `internal/web/stackviewjs_test.go` | wiring guards |
| docs | CHANGELOG, `docs/CLAUDE-details.md`, `docs/web-tui-parity.md` if it lists the gap, help text |

---

### Task 1: the pure half — `symPair`, `noContentWhy`, sym slots

**Files:**
- Modify: `internal/web/static/stack.js`
- Test: `internal/web/stackjs_test.go`

**Interfaces:**
- Produces: `symPair(f, c) → {left, right, status} | null` (null = neither side
  has bytes); `noContentWhy(f) → string`; `GLYPH`, `KIND_TIP` (moved here from
  symcompare.js so stack.js is the one shared leaf); slot fields `kind`, `left`, `right`
  (`""` for non-sym rows) and `none` (`"conflict"` | `"empty"` | `""`).

- [ ] **Step 1: failing harness cases** — append to `stackHarness` before the
  final `console.log`:

```js
// 6. a symmetric row's pair, in the arrow's direction; a row's own spec wins
const c = { aSpec: "A", bSpec: "B", flipped: false };
const row = { path: "x", left: "present", right: "deleted", status: "D", kind: "ne", right_spec: "Rx" };
out.pair = S.symPair(row, c);
out.pairFlip = S.symPair({ ...row, status: "A" }, { ...c, flipped: true });
out.pairEq = S.symPair({ path: "y", left: "present", right: "present", status: "=", kind: "eq" }, c).status;
out.pairNone = S.symPair({ path: "z", left: "deleted", right: "absent", status: "D", kind: "or" }, c);
out.why = S.noContentWhy({ left: "deleted", right: "absent" });
// 7. sym slots: glyph data rides along; a no-content row is never fetched
const ss = S.buildSlots([
  { f: row, idx: 0 },
  { f: { path: "z", left: "deleted", right: "absent", status: "D", kind: "or" }, idx: 1 },
]);
out.symKind = ss[0].kind + ss[0].left + ss[0].right + ss[0].load + ss[0].none;
out.symNone = ss[1].load + ":" + ss[1].none;
out.plainNone = S.buildSlots(rows(1))[0].none + "|" + S.buildSlots(rows(1))[0].kind;
out.conflictWhy = ws[2].none;
out.glyphs = Object.values(S.GLYPH).join("");
```

  and to `want` in `TestStackJS`:

```go
"pairEq": "M", "pairNone": nil,
"why":        "the left set deletes it, the right set does not touch it",
"symKind":    "nepresentdeletedidle", "symNone": "none:empty",
"plainNone":  "|", "conflictWhy": "conflict", "glyphs": "≠=◁▷",
```

  plus, after the `idx` helper:

```go
if idx("pair") != `{"left":"A","right":"Rx","status":"D"}` || idx("pairFlip") != `{"left":"Rx","right":"A","status":"A"}` {
	t.Errorf("symPair: %s / flipped %s", idx("pair"), idx("pairFlip"))
}
```

- [ ] **Step 2:** `go test ./internal/web -run TestStackJS` → FAIL (`S.symPair is not a function`).

- [ ] **Step 3: implement** in `stack.js` (after `statusLetter`):

```js
// noContent: a symmetric row (it carries a kind) with bytes on NEITHER side —
// one set deletes the file, the other does not touch it. There is nothing to
// ask the server for.
function noContent(f) {
  return !!f.kind && f.left !== "present" && f.right !== "present";
}

// symPair is the pair of sides a symmetric row's diff reads, in the arrow's
// direction: a row's own spec wins over its set's, and a row that does not
// differ ("=") is asked for as M. openSymRow and the stack's loader (through
// fileDiffURL) both read it, so the two views can never fetch different
// diffs for one row. null = neither side has content.
export function symPair(f, c) {
  if (noContent(f)) return null;
  const fl = c.flipped;
  return {
    left: fl ? f.right_spec || c.bSpec : f.left_spec || c.aSpec,
    right: fl ? f.left_spec || c.aSpec : f.right_spec || c.bSpec,
    status: f.status === "=" ? "M" : f.status,
  };
}

// GLYPH / KIND_TIP: a symmetric row's centre glyph and its tooltip, by kind
// (symcompare.js's lists and the stack's headers both paint them).
export const GLYPH = { ne: "≠", eq: "=", ol: "◁", or: "▷" };
export const KIND_TIP = {
  ne: "differs between the two sets",
  eq: "the same in both sets",
  ol: "only in the left set",
  or: "only in the right set",
};

// noContentWhy says why a symmetric row has no diff, side by side.
export function noContentWhy(f) {
  const say = (st, name) => (st === "deleted" ? `the ${name} set deletes it` : `the ${name} set does not touch it`);
  return say(f.left, "left") + ", " + say(f.right, "right");
}
```

  and in `buildSlots` replace the `load:` line with:

```js
    // a conflict is resolved in the hunk picker, and a symmetric row with no
    // bytes on either side has nothing to diff: both are header-only
    load: f.section === "conflicts" || noContent(f) ? "none" : "idle",
    none: f.section === "conflicts" ? "conflict" : noContent(f) ? "empty" : "",
    kind: f.kind || "", // symmetric rows: ≠ = ◁ ▷ and each side's state
    left: f.left || "",
    right: f.right || "",
```

  In `reconcileSlots`, next to `o.status = n.status;` add `o.none = n.none;`.

- [ ] **Step 4:** `go test ./internal/web -run TestStackJS` → PASS.
- [ ] **Step 5:** commit `feat(web): the stack's pure half knows symmetric rows`.

### Task 2: one pair for both views — `openSymRow` and `fileDiffURL`

**Files:**
- Modify: `internal/web/static/symcompare.js`, `internal/web/static/files.js`
- Test: `internal/web/stackviewjs_test.go`

**Interfaces:**
- Consumes: `symPair`, `noContentWhy` (Task 1).
- Produces: `fileDiffURL(f)` answers a sym row's `/api/entry-diff` URL (null
  when neither side has content).

- [ ] **Step 1: failing guard** — new test in `stackviewjs_test.go`:

```go
// The symmetric view's single-file open and its stack read ONE pair builder:
// a flip must never leave the stack diffing the old direction.
func TestSymPairShared(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	sym := readStatic(t, "symcompare.js")
	if !strings.Contains(files, "const p = symPair(f, c);") {
		t.Error("fileDiffURL does not build a symmetric row's URL through symPair")
	}
	if !strings.Contains(sym, "const p = symPair(f, c);") {
		t.Error("openSymRow does not read its pair from symPair")
	}
	if strings.Contains(sym, "function noContentWhy(") {
		t.Error("symcompare.js keeps its own noContentWhy — stack.js owns it")
	}
}
```

- [ ] **Step 2:** `go test ./internal/web -run TestSymPairShared` → FAIL.

- [ ] **Step 3: implement.** `symcompare.js`: import
  `{ GLYPH, KIND_TIP, noContentWhy, symPair } from "./stack.js"`; delete the
  local `GLYPH`, `KIND_TIP` and `noContentWhy`; `openSymRow` becomes:

```js
function openSymRow(f) {
  const c = state.compare;
  const fl = c.flipped;
  const p = symPair(f, c);
  if (!p) {
    // openFile already put the diff stage up and cleared the hunk state.
    state.detailGen++; // …and a diff still loading must not land over this
    state.diffCtx = null;
    state.notes = [];
    setDiffTitle(f.path);
    $("diff-body").innerHTML = `<div class="notice">neither set has content for this file — ${esc(noContentWhy(f))}</div>`;
    return;
  }
  return openEntryFileDiff({
    left: p.left,
    right: p.right,
    path: f.path,
    leftLabel: fl ? c.b : c.a,
    rightLabel: fl ? c.a : c.b,
    status: p.status,
  });
}
```

  `files.js`: add `symPair` to the
  `./stack.js` import (add the import line if files.js has none), and at the top
  of the links/frozen arm of `fileDiffURL` (AFTER its existing
  `const c = state.compare;` — do not redeclare `c`):

```js
  // The symmetric view diffs a row in the ARROW's direction (symPair — the
  // pair openSymRow opens), which the plain link lane below does not know.
  if (state.filesMode === "compare" && c.links && symActive()) {
    const p = symPair(f, c);
    if (!p) return null; // neither set has content: the stack shows a notice instead
    return "/api/entry-diff?" + new URLSearchParams({ left: p.left, right: p.right, path: f.path, status: p.status });
  }
```

- [ ] **Step 4:** `go test ./internal/web -run 'TestSymPairShared|TestSym|TestFileDiffURL|TestLinkCompare'` → PASS;
  `node --check` on both files.
- [ ] **Step 5:** commit `refactor(web): one symmetric pair builder for the single view and the stack`.

### Task 3: the stack comes on in the symmetric view

**Files:**
- Modify: `internal/web/static/stackview.js`, `internal/web/static/symcompare.js`,
  `internal/web/static/files.js`, `internal/web/static/style.css`
- Test: `internal/web/stackviewjs_test.go`

**Interfaces:**
- Consumes: slot `kind`/`left`/`right`/`none`, `GLYPH`/`KIND_TIP` (Task 1),
  `fileDiffURL` sym arm (Task 2), `teardownStack` (plan 1).
- Produces: `stackOn()` true in the symmetric view; `st.painted`, `st.want`.

- [ ] **Step 1: failing guards** — new test:

```go
// Plan 2: the symmetric view stacks. Each line is a door into it or a path
// that must keep the reader's file across a rebuild.
func TestSymStackWired(t *testing.T) {
	t.Parallel()
	view := readStatic(t, "stackview.js")
	sym := readStatic(t, "symcompare.js")
	files := readStatic(t, "files.js")
	if strings.Contains(view, "!symActive()") || strings.Contains(view, "|| symActive()") {
		t.Error("stackview.js still refuses the symmetric view")
	}
	for _, want := range []string{
		"neither set has content for this file",   // the no-content body
		`class="stk-sides"`,                        // each side's state in the header
		"if (!st.painted) {",                       // an open before the first paint moves the anchor
		`scrollIntoView({ block: "nearest" })`,     // the list keeps the reader's row in view
	} {
		if !strings.Contains(view, want) {
			t.Errorf("stackview.js is missing %q", want)
		}
	}
	if !strings.Contains(sym, "// the stack keeps the file being read when the filter still shows it") {
		t.Error("setFilter no longer keeps the reader's file under a stack")
	}
	for _, want := range []string{
		"if (state.stack && state.layout === \"diff\") return openFile(state.fileCursor);", // live refresh rebuilds
		"teardownStack(); return symEmpty(); }",                                            // an empty view drops the stack
	} {
		if !strings.Contains(files, want) {
			t.Errorf("files.js is missing %q", want)
		}
	}
}
```

- [ ] **Step 2:** run → FAIL.

- [ ] **Step 3a: lift the gate** in `stackview.js`:

```js
// stackOn: the toggle is on. Every file-list screen stacks — the symmetric
// view's stack holds its VISIBLE rows (state.files = symRows).
function stackOn() {
  return !!(state.ui && state.ui.stacked_diff);
}
```

  `toggleStacked`: replace `if (state.layout !== "diff" || symActive()) return;`
  with `if (state.layout !== "diff" || !activeFileList().length) return;`
  (a symmetric view with no visible row has nothing to stack or open). Drop the
  now-unused `symActive` import: stackview.js then imports nothing from
  symcompare.js.

- [ ] **Step 3b: header + body.** Import `GLYPH`, `KIND_TIP`, `noContentWhy`
  from `./stack.js`. In `headHTML`:

```js
const SIDE = { absent: "—", present: "in", deleted: "deletes" };
…
  // a symmetric row says what the two sets have to do with each other, and
  // where each stands (the lists' own glyph and words)
  const glyph = s.kind ? `<span class="symg ${s.kind}" title="${KIND_TIP[s.kind]}">${GLYPH[s.kind]}</span>` : "";
  const sides = s.kind
    ? `<span class="stk-sides">left ${SIDE[s.left] || "—"} · right ${SIDE[s.right] || "—"}</span>`
    : "";
  const letter = s.status === "=" ? "" : esc(s.status); // "=" is the glyph's job
```

  and emit `glyph` before the status span, `letter` inside it, `sides` before
  `stk-counts`. In `bodyHTML`, the `load === "none"` arm:

```js
  if (s.load === "none") {
    if (s.none === "empty") {
      return `<div class="notice">neither set has content for this file — ${esc(noContentWhy(s.f))}</div>`;
    }
    return `<div class="notice">conflicted — <button class="stk-resolve">open resolver</button></div>`;
  }
```

  `style.css` after `.stk-bin`:

```css
.stk-sides { color: var(--dim); white-space: nowrap; font-size: 0.9em; }
.stk-head .symg { flex: 0 0 auto; }
```

- [ ] **Step 3c: an open before the first paint.** `buildStack` awaits the
  counts; `applySym` / `setFilter` open row 0 (through `applyCompareFilter`)
  and then the kept row in the same tick, so the second open lands on a stack
  with no sections yet and its target is lost to the `anchorIdx` the first
  one passed. In `buildStack` create `st` with `painted: false, want: anchorIdx`
  and end with:

```js
  paintStack(st);
  st.painted = true;
  scrollToFile(st, st.want);
```

  In `scrollToFile`, right after the `k < 0` rebuild line:

```js
  if (!st.painted) {
    // still awaiting its counts: remember the target, the first paint lands there
    st.want = i;
    state.fileCursor = i;
    return;
  }
```

- [ ] **Step 3d: the list follows.** Add in `stackview.js`:

```js
// followInList keeps the highlighted row on screen as the reader scrolls the
// stack (the symmetric view's left list mirrors #files-pane's scroll).
function followInList() {
  const sel = document.querySelector("#files-list li.sel");
  if (sel) sel.scrollIntoView({ block: "nearest" });
}
```

  call it after `renderFiles()` in both `scrollToFile` and `syncCursor`.

- [ ] **Step 3e: rebuild paths keep the file.** `symcompare.js`:
  - `setFilter`: remember the reader's path before the switch and, under a
    stack, open it again once the filter re-listed:

```js
  const path = (state.files[state.fileCursor] || {}).path;
  state.compare.symFilter = id;
  state.fileCursor = 0;
  applyCompareFilter(); // under an open diff this opens row 0 (a new stack)
  if (state.stack) {
    // the stack keeps the file being read when the filter still shows it
    const i = state.files.findIndex((f) => f.path === path);
    if (i > 0) openFile(i);
  } else if (state.files.length && state.layout !== "diff") openFile(0);
```

  - `applySym`, classic branch: after `renderFiles();` add
    `if (state.layout === "diff" && i >= 0) openFile(i); // the kept file, not row 0`.
    (`flip` and turning the view ON already re-open the kept path.)

  `files.js` `applyCompareFilter`: the empty arm becomes
  `if (symActive()) { teardownStack(); return symEmpty(); } // an empty view has no files to stack`
  (every empty path — a filter, a flip, a live refresh, applySym — goes
  through here; files.js already imports `teardownStack`).

  `files.js` `updateLinkCompareFiles`: after `state.fileCursor = …` add

```js
  // A stack holds the OLD rows: rebuild it on the cursor's file. Deliberately
  // BEFORE the drillOut below: when the reader's file is gone, a stack
  // re-anchors on the clamped neighbour (plan 1's working-tree reconcile
  // rule) instead of leaving the screen, as the single-file view does.
  if (state.stack && state.layout === "diff") return openFile(state.fileCursor);
```

- [ ] **Step 4:** `go test ./internal/web` (whole package) → PASS; `node --check`
  each touched `.js`.
- [ ] **Step 5:** commit `feat(web): the stacked view works in the symmetric compare`.

### Task 4: browser probe (Playwright, scratchpad) — every entry path

Fixture: a repo with `main` + `agent-a`/`agent-b` touching overlapping files so
all four kinds exist (≠, =, ◁, ▷) plus one "neither has content" row (one
side deletes a file the other does not touch) and ≥ 12 rows so lazy loading is
observable; a saved comparison `gg compare --save sym @main...agent-a
@main...agent-b` (flags first). Fresh `XDG_STATE_HOME` per run, window
1600×900, `stdio: "ignore"`, kill in `finally`, `/api/repo` checked to be OUR
server. Reuse `stack-probe/probe.mjs` scaffolding.

Assert (VISIBILITY, not just presence), first against the plan-1 build
(`cdc47ccf`, must FAIL), then the new build twice:

1. open the comparison, `v` on, `S` on → `.stk` visible in the middle column,
   `.stk-file` count === visible-row count; both lists still visible.
2. every header of a sym row shows a `.symg` glyph and `.stk-sides`; the
   no-content row's body shows "neither set has content" and NO
   `/api/entry-diff` request is made for its path.
3. network: only near-viewport sections fetched on open (< row count), each
   request is `/api/entry-diff`; a loaded section's header shows `+a −d`
   counts (first browser run of entry-diff-backed slots — plan 1 never probed
   a link compare, so countsFromDiff/bodyHTML meet that shape here first).
4. click row 8 in the LEFT list → its header at the pane top; both lists'
   `.sel` on row 8.
5. scroll the pane to row 3's header → both lists' `.sel` on row 3.
6. `x` flip → stack rebuilt (new sections), row 3 still the pane-top file,
   its fetch URL's `left`/`right` swapped vs step 3.
7. `3` (one side) with row 3 visible under it → row 3 kept at the top;
   `1` with row 3 hidden → stack opens at row 0.
8. `v` off → classic list stack on the same file; `v` on again → sym stack.
9. `S` off → single-file diff of the pane-top row (the no-content row shows its
   notice); `S` on → stack.
10. narrow to 1100px → classic stack; widen → sym stack.

- [ ] Write `stack-probe/sym.mjs`, run against `cdc47ccf` build → FAIL at 1.
- [ ] Run against the branch build twice → all pass. Fix and re-probe anything red.

### Task 5: docs, gate, delivery

- [ ] `stackview.js` help text: drop "The symmetric view keeps its single diff"
  wording if present; say the symmetric view stacks its visible rows.
- [ ] CHANGELOG (Unreleased): "gg web: the stacked diff (S) works in the
  symmetric comparison — …". `docs/CLAUDE-details.md` "Stacked diff view in gg
  web": a symmetric paragraph (symPair shared, painted/want, rebuild paths).
  `docs/web-tui-parity.md` only if it names the gap. README if it says the
  symmetric view is excluded.
- [ ] `./test.sh race` green.
- [ ] Build the verify binary in the worktree, send it (absolute path), plus
  `./build.sh web` is for after the merge. Update memory
  `stacked-diff-view-feature.md`. The user merges.
