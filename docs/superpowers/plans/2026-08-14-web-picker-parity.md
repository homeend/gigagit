# Web Conflict-Picker Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The web conflict picker gains the TUI's pick model — ordered line picks, tri-state side toggles (both sides allowed, order = result), decided-empty regions, a live output pane — and the server adopts `ConflictPickerFile` so nested-marker files stop breaking in the browser.

**Architecture:** Server: `loadConflictDoc` regenerates content via `Service.ConflictPickerFile` + `ParseConflictSized`; `/api/resolve-hunks` picks become tagged objects (`ours`/`theirs`/`lines`), validated server-side into `Block.Picks`. Client: the inline `files.js` picker gets per-line checkboxes, tri-state side tags, an ordered pick list per region, and a client-assembled collapsible output pane. No new endpoints, no engine/domain changes.

**Tech Stack:** Go 1.26 (`internal/web` httptest-based tests with real git repos), vanilla ES-module JS (`internal/web/static/files.js`), CSS.

**Spec:** `docs/superpowers/specs/2026-08-14-web-picker-parity-design.md`

## Global Constraints

- Work in the worktree `.claude/worktrees/web-picker-parity` on branch `feat/web-picker-parity` (branched from `web-dev`; merges back into `web-dev`, NEVER main). Prefix every command with `cd /mnt/t/others/gigagit/.claude/worktrees/web-picker-parity &&`; Write/Edit only absolute paths under that worktree.
- Tests FOREGROUND only, timeout 600000 on every test call; NEVER background a test run — backgrounded processes die at turn end.
- Go tests follow the file's existing idiom: real git repos via the file's helpers (`conflictingRepo`, `conflictedMergeState`, `serve`, `getJSON`, `postJSON`-equivalent, `gitRun`) — read them before writing; new repo fixtures must isolate git config the way the file's existing fixtures do (check how `conflictingRepo` handles it — `GIT_CONFIG_GLOBAL`/env; mirror exactly).
- Wire values are English protocol strings — no i18n anywhere in this feature (web frontend is English-only).
- JS: `files.js` is one of 16 hand-split ES modules — if you add cross-module state, exports/imports are maintained BY HAND (no bundler); prefer keeping everything module-local to `files.js`. Match the file's existing style (template literals, `esc()` for all user content, `$()` helper).
- The freshness-hash contract is load-bearing: picks are positional against the exact bytes hashed; GET and POST must derive doc+hash through the SAME loader.
- gofmt-clean for Go; no `console.log` leftovers in JS.

---

### Task 1: server load path — ConflictPickerFile adoption + nested-marker test

**Files:**
- Modify: `internal/web/conflict.go` (`loadConflictDoc`, ~lines 50-92)
- Test: `internal/web/conflict_test.go`

**Interfaces:**
- Consumes: `svc.ConflictPickerFile(ctx, path) ([]byte, int, error)` (domain/conflict.go:181), `hunkpick.ParseConflictSized(content, markerSize)`.
- Produces: the regenerated-content hash — Task 2's POST path uses the same loader unchanged.

- [ ] **Step 1: Write the failing nested-marker test**

Read `conflictingRepo` in the test file first; then add a fixture that commits a file whose BASE content contains literal 7-char marker lines, branches, edits it both ways, and merges to a real conflict. Sketch (adapt to the file's helper idiom — `gitRun` for the plumbing, the same config isolation the existing fixture uses):

```go
// nestedMarkerRepo builds a repo whose conflicted file's content itself
// contains literal 7-char conflict-marker lines (a conflict once committed
// unresolved), the case raw-worktree parsing cannot disambiguate.
func nestedMarkerRepo(t *testing.T) (dir, path string) {
	t.Helper()
	base := "top\n<<<<<<< HEAD\nghost\n=======\nother\n>>>>>>> x\nbottom\nend\n"
	// build: commit base on main; branch feature edits "end"→"END-F";
	// main edits "end"→"END-M"; merge feature → one real conflict region
	// in a file that ALSO contains the literal marker lines above.
	...
	return dir, "weird.txt"
}

func TestConflictHunksNestedMarkers(t *testing.T) {
	dir, path := nestedMarkerRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	var d struct {
		Count int    `json:"count"`
		Hash  string `json:"hash"`
		Items []struct {
			Kind         string   `json:"kind"`
			Lines        []string `json:"lines"`
			Ours, Theirs []string `json:"ours"`
		} `json:"items"`
	}
	if code := getJSON(t, ts, "/api/conflict-hunks?path="+path, &d); code != http.StatusOK {
		t.Fatalf("nested-marker file must load via regeneration, got %d", code)
	}
	if d.Count != 1 {
		t.Fatalf("count = %d, want 1 real region", d.Count)
	}
	// The literal ghost markers are passthrough text, not a block.
	found := false
	for _, it := range d.Items {
		if it.Kind == "text" && strings.Contains(strings.Join(it.Lines, "\n"), "<<<<<<< HEAD") {
			found = true
		}
	}
	if !found {
		t.Fatalf("literal marker lines must survive as passthrough text")
	}
}
```

(Fix the struct tags — `Theirs` needs its own `json:"theirs"` tag. Build the fixture with the same env isolation as `conflictingRepo`. The merge conflict must be on lines AWAY from the ghost markers so both sides keep them intact.)

- [ ] **Step 2: Run red**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-picker-parity && go test ./internal/web/ -run TestConflictHunksNestedMarkers -v` (timeout 600000)
Expected: FAIL — today the raw worktree text has ambiguous markers → 422 "no usable conflict markers" (or a wrong parse; either way the 200/count assertion fails).

- [ ] **Step 3: Swap the load path**

In `loadConflictDoc`, replace the worktree read + parse:

```go
	content, markerSize, err := svc.ConflictPickerFile(ctx, path)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return nil, "", false
	}
	if textdiff.IsBinary(content) {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("binary file — resolve in your editor, then mark resolved"))
		return nil, "", false
	}
	doc, perr := hunkpick.ParseConflictSized(content, markerSize)
	if perr != nil || len(doc.Blocks()) == 0 {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("no usable conflict markers — resolve in your editor, then mark resolved"))
		return nil, "", false
	}
	sum := sha256.Sum256(content)
	return doc, hex.EncodeToString(sum[:]), true
```

Update the function's doc comment: content is regenerated from index stages (nested-marker safe); hash is over the regenerated bytes. Remove the now-unused `WorktreeFile` call; keep everything above (status gate) unchanged.

- [ ] **Step 4: Run green + the file's whole suite**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-picker-parity && go test ./internal/web/ -run 'TestConflict|TestResolve' -v 2>&1 | tail -12 && go test ./internal/web/ 2>&1 | tail -3`
Expected: nested-marker test PASSES; all existing conflict/resolve tests still green (they use real conflicts whose regenerated content parses identically).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/web-picker-parity && git add internal/web/conflict.go internal/web/conflict_test.go && git commit -m "fix(web): conflict picker regenerates from index stages — nested-marker files load"
```

---

### Task 2: server resolve wire — tagged-object picks

**Files:**
- Modify: `internal/web/conflict.go` (`resolveHunksRequest`, `handleResolveHunks`, ~lines 116-169)
- Test: `internal/web/conflict_test.go`

**Interfaces:**
- Consumes: Task 1's loader; `hunkpick.Pick{Side, Line}`, `hunkpick.LineByLine`, `hunkpick.Current/Incoming` (all exported).
- Produces: the wire contract Task 3's client posts: `picks[i]` = `{"mode":"ours"}` | `{"mode":"theirs"}` | `{"mode":"lines","lines":[{"side":"ours","line":0},...]}`.

- [ ] **Step 1: Write the failing tests**

Add to `conflict_test.go` (reuse the file's existing resolve-test scaffolding for building a conflicted repo, GETting the hash, POSTing, and reading `git show :0:<path>` or the worktree result — read the existing resolve test first and mirror its verification method exactly):

```go
func TestResolveHunksLinePicks(t *testing.T) {
	// Conflicted file with one region: ours [oA,oB], theirs [tA].
	// Picks: theirs line 0, ours line 1, ours line 0 → result order tA,oB,oA.
	...
	picks := []map[string]any{{
		"mode": "lines",
		"lines": []map[string]any{
			{"side": "theirs", "line": 0},
			{"side": "ours", "line": 1},
			{"side": "ours", "line": 0},
		},
	}}
	// POST /api/resolve-hunks {path, picks, hash} → 200; staged content's
	// region reads tA\noB\noA in exactly that order.
}

func TestResolveHunksEmptyDecided(t *testing.T) {
	// picks[0] = {"mode":"lines","lines":[]} → 200; the staged content
	// contains NEITHER side's lines (region dropped), passthrough intact.
}

func TestResolveHunksLineValidation(t *testing.T) {
	// Table test, each expecting 400 with a message naming the problem:
	//  {"mode":"lines","lines":[{"side":"nope","line":0}]}       → bad side
	//  {"mode":"lines","lines":[{"side":"ours","line":99}]}      → line out of range
	//  {"mode":"lines","lines":[{"side":"ours","line":0},
	//                           {"side":"ours","line":0}]}       → duplicate pick
	//  {"mode":"weird"}                                          → bad mode
	// Plus: wrong picks count → 400 (existing rule), stale hash → 409.
}

func TestResolveHunksFastPathStillWorks(t *testing.T) {
	// {"mode":"ours"} / {"mode":"theirs"} behave exactly like the old
	// string picks (assert final content both ways on a 2-region file).
}
```

Flesh these out fully — the fixtures are small real repos; content assertions are exact byte comparisons of the resolved file (the existing resolve test shows where the resolved content lands — read it rather than assuming index vs worktree).

- [ ] **Step 2: Run red**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-picker-parity && go test ./internal/web/ -run TestResolveHunks -v` (timeout 600000)
Expected: new tests FAIL — the decoder still expects `[]string` (JSON object in a string slot → 400 for the wrong reason; the assertions on 200/content fail).

- [ ] **Step 3: Implement the wire**

```go
type resolvePick struct {
	Mode  string `json:"mode"` // "ours" | "theirs" | "lines"
	Lines []struct {
		Side string `json:"side"` // "ours" | "theirs"
		Line int    `json:"line"`
	} `json:"lines"`
}

type resolveHunksRequest struct {
	Path  string        `json:"path"`
	Picks []resolvePick `json:"picks"` // positional: picks[i] decides block i
	Hash  string        `json:"hash"`
}
```

In `handleResolveHunks`, the per-block loop becomes:

```go
	for i, p := range req.Picks {
		switch p.Mode {
		case "ours":
			blocks[i].Mode = hunkpick.TakeCurrent
		case "theirs":
			blocks[i].Mode = hunkpick.TakeIncoming
		case "lines":
			// Ordered line-pick model: result order = array order, sides may
			// interleave, an empty list is decided-empty (drop both sides).
			seen := map[[2]int]bool{}
			picks := make([]hunkpick.Pick, 0, len(p.Lines))
			for j, ln := range p.Lines {
				var side hunkpick.Side
				var max int
				switch ln.Side {
				case "ours":
					side, max = hunkpick.Current, len(blocks[i].Current)
				case "theirs":
					side, max = hunkpick.Incoming, len(blocks[i].Incoming)
				default:
					writeErr(w, http.StatusBadRequest, fmt.Errorf("pick %d line %d: side %q (want ours|theirs)", i, j, ln.Side))
					return
				}
				if ln.Line < 0 || ln.Line >= max {
					writeErr(w, http.StatusBadRequest, fmt.Errorf("pick %d line %d: %s line %d out of range (0..%d)", i, j, ln.Side, ln.Line, max-1))
					return
				}
				key := [2]int{int(side), ln.Line}
				if seen[key] {
					writeErr(w, http.StatusBadRequest, fmt.Errorf("pick %d: duplicate %s line %d", i, ln.Side, ln.Line))
					return
				}
				seen[key] = true
				picks = append(picks, hunkpick.Pick{Side: side, Line: ln.Line})
			}
			blocks[i].Mode = hunkpick.LineByLine
			blocks[i].Picks = picks
		default:
			writeErr(w, http.StatusBadRequest, fmt.Errorf("pick %d: mode %q (want ours|theirs|lines)", i, p.Mode))
			return
		}
	}
```

(Verify the exported names — `Block.Picks`, `Pick{Side, Line}`, `LineByLine` — against `internal/hunkpick/hunkpick.go` before writing; if `Side` isn't int-convertible for the map key, key on `ln.Side` string + line instead.)

Update the handler comment: picks are tagged objects; the `lines` mode carries the ordered line-pick model.

- [ ] **Step 4: Run green + package**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-picker-parity && go test ./internal/web/ -run TestResolveHunks -v 2>&1 | tail -10 && go test ./internal/web/ 2>&1 | tail -3 && gofmt -l internal/`
Expected: all green; gofmt silent.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/web-picker-parity && git add internal/web/conflict.go internal/web/conflict_test.go && git commit -m "feat(web): resolve-hunks picks carry the ordered line-pick model"
```

---

### Task 3: client — checkboxes, ordered picks, output pane

**Files:**
- Modify: `internal/web/static/files.js` (the conflict-picker section, ~lines 662-790)
- Modify: `internal/web/static/style.css` (the `cf-*` rules)
- Modify: `internal/web/static/index.html` (the resolve-bar span, line ~79, if button labels change)

**Interfaces:**
- Consumes: Task 2's wire (`{mode, lines}` picks); the unchanged GET shape.
- Produces: nothing downstream.

No JS test harness exists — correctness is carried by (a) keeping ALL state transitions in small pure-ish functions as below, (b) the Go wire tests, and (c) the controller's CDP verification after the task. Keep the diff reviewable: replace the picker section wholesale rather than scattering edits.

- [ ] **Step 1: State model**

Replace `choices: Array<null|"ours"|"theirs">` with:

```js
// choices[i] = {picks: Array<{side:"ours"|"theirs", line:number}>, touched:boolean}
// - order of `picks` = order in the assembled result (the TUI rule)
// - touched && picks.length === 0  → decided-empty ("drop both sides")
// - !touched                       → undecided (gates resolve)
let conflictPick = null; // {path, hash, count, items, choices}
```

Keep the raw `d.items` on the state (the output pane and toggles need the lines).

Pure helpers (module-local):

```js
function regionDecided(ch) { return ch.touched; }

function sideState(ch, it, side) {
  // → "all" | "some" | "none" for the tri-state tag
  const total = (side === "ours" ? it.ours : it.theirs)?.length || 0;
  if (!total) return "none";
  const n = ch.picks.filter((p) => p.side === side).length;
  return n === total ? "all" : n ? "some" : "none";
}

function toggleLine(ch, side, line) {
  ch.touched = true;
  const at = ch.picks.findIndex((p) => p.side === side && p.line === line);
  if (at >= 0) ch.picks.splice(at, 1);
  else ch.picks.push({ side, line });
}

function toggleSide(ch, it, side) {
  // TUI ToggleSide: zero-line side is a no-op; fully-on clears that side's
  // picks (others keep order); else append the side's unpicked lines in order.
  const lines = side === "ours" ? it.ours : it.theirs;
  if (!lines || !lines.length) return;
  ch.touched = true;
  if (sideState(ch, it, side) === "all") {
    ch.picks = ch.picks.filter((p) => p.side !== side);
  } else {
    for (let i = 0; i < lines.length; i++)
      if (!ch.picks.some((p) => p.side === side && p.line === i)) ch.picks.push({ side, line: i });
  }
}

function wirePick(ch, it) {
  // Collapse to the fast path when the picks are exactly one full side in order.
  const full = (side) => {
    const lines = side === "ours" ? it.ours : it.theirs;
    return (lines?.length || 0) > 0 && ch.picks.length === lines.length &&
      ch.picks.every((p, i) => p.side === side && p.line === i);
  };
  if (full("ours")) return { mode: "ours" };
  if (full("theirs")) return { mode: "theirs" };
  return { mode: "lines", lines: ch.picks.map((p) => ({ side: p.side, line: p.line })) };
}
```

- [ ] **Step 2: Render**

Rework the block HTML in `openConflictPicker` (keep `cf-text` passthrough and the whitespace/empty-side rendering helpers as-is):

- Side tag: `<div class="cf-tag" data-act="side">[tick] ours · N lines</div>` where tick is `[x]`/`[~]`/`[ ]` from `sideState`; a zero-line side renders its existing `(empty — this side has no lines)` body and NO actionable tick.
- Lines: each line row becomes `<div class="cf-ln" data-side="ours" data-line="3"><span class="cf-tick">[ ]</span>…rendered line…</div>` (reuse the whitespace-glyph rendering per line; `esc()` everything).
- Region header row gains a state suffix: nothing while undecided, `· empty` when decided-empty, `· ours first`/`· theirs first` when both sides picked (first pick's side).
- Below `#cf-doc`, add the output pane:

```html
<div id="cf-out">
  <div id="cf-out-head" data-act="collapse">output — live preview</div>
  <pre id="cf-out-body"></pre>
</div>
```

Assembly (pure, from state):

```js
function assembleOutput(v) {
  const out = [];
  for (const it of v.items) {
    if (it.kind === "text") { out.push(...(it.lines || [])); continue; }
    const ch = v.choices[it.index];
    if (!ch.touched) out.push(`‹region ${it.index + 1} undecided›`);
    else for (const p of ch.picks) out.push((p.side === "ours" ? it.ours : it.theirs)[p.line]);
  }
  return out.join("\n");
}
```

`paintConflictPicks` repaints tick glyphs, `.picked` line classes, the region suffix, the output pane body, and the resolve bar. One repaint function, called after every toggle.

- [ ] **Step 3: Events**

Extend the existing delegated click handler (find where `.cf-side` clicks are handled today — it currently picks a whole side exclusively): clicks now route by `data-act`/`data-side`/`data-line`:
- `.cf-ln` click → `toggleLine`; side tag click → `toggleSide`; `#cf-out-head` → collapse/expand (a `hidden` class on the body).
- The old exclusive side-click behavior is REPLACED by the toggle semantics (clicking a side box body outside a line does nothing — lines and tags are the targets now).
- Resolve bar: `all ours`/`all theirs` become tri-state document toggles: if every region with a non-empty that-side is fully-on for that side → clear that side everywhere (touched stays true); else complete it everywhere (append missing lines in order); skip zero-line sides (the TUI `C`/`I` rule). `resolve` gates on `choices.every(regionDecided)`; count text `N/M decided`.
- POST body: `picks: v.choices.map((ch, i) => wirePick(ch, itemForBlock(v, i)))` — build an index→item lookup once.

- [ ] **Step 4: CSS**

Add/adjust in `style.css` near the existing `cf-*` rules: `.cf-ln` (row hover, pointer cursor), `.cf-ln.picked` (accent background + tick `[x]`), `.cf-tick` (monospace, dim), tri-state tag emphasis (`.cf-tag.some`/`.all`), `#cf-out` (bordered pane, `#cf-out-body` pre with max-height + scroll, `.hidden` collapse), region suffix dim style. Follow the file's existing variable/palette usage — read the current `cf-*` block first and extend in the same style.

- [ ] **Step 5: Manual smoke via Go tests + lint**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-picker-parity && go test ./internal/web/ 2>&1 | tail -3 && node --check internal/web/static/files.js`
Expected: Go green (embedded static changes don't break handlers); `node --check` parses clean. Grep your diff for `console.log` — none.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/web-picker-parity && git add internal/web/static/files.js internal/web/static/style.css internal/web/static/index.html && git commit -m "feat(web): conflict picker gains line picks, tri-state side toggles, live output pane"
```

---

### Task 4: docs

**Files:**
- Modify: `README.md` (the web frontend section — grep `gg web` / the conflict picker paragraph)
- Modify: `CHANGELOG.md`

- [ ] **Step 1: README**

Find the web section's conflict-resolution description (grep for `resolve` near the `gg web` docs) and update it to describe: per-line checkboxes and side toggles (left, right, or both — pick order = result order), decided-empty regions, the live output preview pane, and that files whose content contains literal conflict markers now load (regenerated from index stages). Match the section's existing prose style; keep table rows single-line if the text lands in a table.

- [ ] **Step 2: CHANGELOG**

Top of `## [Unreleased]`, matching neighbors:

```markdown
- **Web: conflict picker parity with the TUI.** The browser conflict picker
  now has per-line checkboxes and tri-state side toggles (left, right, or
  both — pick order = result order), decided-empty regions, and a live
  assembled-output pane; `all ours`/`all theirs` are tri-state document
  toggles. The server regenerates the conflict text from index stages
  (`ConflictPickerFile`), so files whose content contains literal conflict
  markers now load in the web picker too, and picks ride a new tagged wire
  format (`ours`/`theirs`/ordered `lines`).
```

- [ ] **Step 3: Gates + commit**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-picker-parity && gofmt -l internal/ && ./test.sh unit 2>&1 | tail -3` (FOREGROUND, timeout 600000)
Expected: silent; all green.

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/web-picker-parity && git add README.md CHANGELOG.md && git commit -m "docs(web): conflict-picker parity — README + changelog"
```
