# Drag & drop compare in the Previews section — Implementation Plan

> **For agentic workers:** executed INLINE by the session that wrote it (superpowers:executing-plans; this repo never uses subagents). Steps use checkbox syntax.

**Goal:** drag one Previews row onto another in `gg web` to open the link comparison of the two rows' links; `compare with…` is the menu twin.

**Architecture:** client-only. `links.js` exports the ONE builder of a merge-preview row's link; `previews.js` paints `draggable` rows, owns five delegated drag listeners on `#previews-list`, a drop menu, and the `compare with…` rows. `runLinkCompare` / `openLinkCompareDialog` / `POST /api/saved-compares` are reused unchanged.

**Tech stack:** vanilla ES modules (embedded), Go wiring-guard test, a Playwright probe (scratchpad).

**Spec:** `docs/superpowers/specs/2026-09-21-web-previews-dnd-compare-design.md`

## Global constraints

- Worktree `/mnt/t/others/gigagit/.claude/worktrees/previews-dnd-compare`; every command `cd`s there; absolute paths in Write/Edit.
- No server change, no new preference, no new static file.
- `.drop-target` is styled PER LIST — the CSS rule must name `#previews-list`.
- `sidebar.js` must not import `previews.js` (import cycle).
- Rows are looked up by id at drop time, never held as elements.
- A comparison row (`data-kind="compare"`) is neither source nor target.
- The probe asserts computed style / visibility and must fail with each guard removed.

---

### Task 1: one builder for a merge-preview row's link

**Files:** modify `internal/web/static/links.js`.

- [ ] Extract from `registerRows("preview", …)`:

```js
// previewRowLink is the ONE gg:// link a saved merge-preview row stands for —
// what its "copy gg link" copies and what a drag & drop compare hands the
// server. "" when the grammar cannot carry the row's ref names.
function previewRowLink(e) {
  const ctx = { path: "", state: "commit", compare: true, preview: { source: e.source, target: e.target } };
  return (
    linkFor(state.repo, state.worktree, { ...ctx, hint: { kind: "preview", id: e.id } }) ||
    linkFor(state.repo, state.worktree, ctx)
  );
}
```

`registerRows("preview", (e) => { const link = previewRowLink(e); … })`; add `previewRowLink` to the export list.

- [ ] `node --check` is not available for embedded modules with imports — run `go test ./internal/web -run 'JS|Static' -count=1`. Commit.

### Task 2: draggable rows, the listeners, the drop menu, `compare with…`

**Files:** modify `internal/web/static/previews.js`, `core.js` (state field `dragPreview: null`), `style.css`, `internal/web/previewsjs_test.go`.

- [ ] **Failing guard first** — append to `previewsjs_test.go`:

```go
// Drag & drop compare: the highlight class is styled per list, and each
// listener is load-bearing (dragover's preventDefault IS the drop permission).
func TestPreviewsDragDropIsWired(t *testing.T) {
	t.Parallel()
	read := func(name string) string { b, err := os.ReadFile(filepath.Join("static", name)); if err != nil { t.Fatal(err) }; return string(b) }
	for _, c := range []struct{ file, want, why string }{
		{"previews.js", `draggable="true"`, "preview and pair rows start a drag"},
		{"previews.js", `data-kind="preview"`, "a merge row names its kind for the drop lookup"},
		{"previews.js", `addEventListener("dragstart"`, "drag source"},
		{"previews.js", `addEventListener("dragover"`, "drop permission + highlight"},
		{"previews.js", `addEventListener("dragleave"`, "highlight off"},
		{"previews.js", `addEventListener("dragend"`, "an abandoned drag clears its state"},
		{"previews.js", `addEventListener("drop"`, "the drop opens the pair menu"},
		{"previews.js", `"compare and save…"`, "the drop menu saves"},
		{"previews.js", `"compare with…"`, "the menu twin of the drag"},
		{"previews.js", `previewRowLink`, "one link builder, shared with copy gg link"},
		{"links.js", `previewRowLink`, "exported by links.js"},
		{"style.css", `#previews-list li.drop-target`, "the highlight is styled per list"},
		{"core.js", `dragPreview`, "the drag state lives in core state"},
	} {
		if !strings.Contains(read(c.file), c.want) { t.Errorf("%s: missing %q — %s", c.file, c.want, c.why) }
	}
}
```

Run `go test ./internal/web -run TestPreviewsDragDropIsWired` → FAIL.

- [ ] `rowEntry` must understand `data-kind="preview"`: `const list = li.dataset.kind && li.dataset.kind !== "preview" ? state.savedCompares : state.previews;` — and the click / contextmenu handlers test `kind === "pair"` / `"compare"` instead of truthiness.
- [ ] `rowLink(e)`: `e.kind === "pair" ? e.link || "" : e.kind === "compare" ? "" : previewRowLink(e)`. `rowName(e)`: `e.label`.
- [ ] Rows: `draggable="true"` only when `rowLink(e)` is non-empty.
- [ ] Listeners (mirror `sidebar.js`): state `state.dragPreview = {id, kind}`; `findRow({id, kind})`; `dragover` returns before `preventDefault` for self / no-link rows; `drop` → `showPreviewPairMenu(src, dst, x, y)`.
- [ ] Menu:

```js
function showPreviewPairMenu(src, dst, x, y) {
  const go = (l, r) => runLinkCompare(new URLSearchParams({ left: rowLink(l), right: rowLink(r) }).toString());
  showCtxMenu([
    { label: "compare " + src.label + " ↔ " + dst.label, act: () => go(src, dst) },
    { label: "compare " + dst.label + " ↔ " + src.label, act: () => go(dst, src) },
    { sep: true },
    { label: "compare and save…", act: () => compareAndSave(src, dst) },
    { sep: true },
    { label: "cancel", act: () => {} },
  ], x, y);
}
```

`compareAndSave`: `openPrompt` (title "Save the comparison as:", value `src.label + " vs " + dst.label`) → `postJSON("/api/saved-compares", {left, right, label})`; on success `refresh()` + `runLinkCompare("id=" + out.entry.id)`; on `err.data.id` → op line "already saved as …" + open that id; else red op line.
- [ ] `compare with…` row in `showPreviewMenu` and `showPairMenu` (only when `rowLink(e)`): `openLinkCompareDialog({ left: rowLink(e) })`.
- [ ] CSS: `#branches-list li.drop-target, #previews-list li.drop-target { … }`.
- [ ] Guard green; `go test ./internal/web -count=1`. Commit.

### Task 3: the probe

**Files:** scratchpad `web/dndprobe.mjs` + `run-dndprobe.sh` (fresh state copy per run; delete `gg/prompts.toml`). Fixture: `symrepo` + two saved merge previews (`gg preview add`), its saved pairs and comparisons.

- [ ] Checks (synthetic events with a real `DataTransfer`): rows draggable / comparison row not; target outline `solid` by computed style; self + comparison row never highlight; menu visible with 4 rows; row 1 → `#files-kind` visible, title names dragged first; row 2 reversed; compare-and-save adds a `data-kind="compare"` row and opens; `compare with…` fills `#linkcmp-left`; no page errors.
- [ ] Watch it FAIL with (a) `preventDefault` removed from dragover, (b) the CSS rule removed, (c) the drop listener removed. Restore each.

### Task 4: docs

- [ ] Previews help text (`registerHelp` in `previews.js`), `CHANGELOG.md`, `README.md` (gg web previews paragraph), `docs/web-tui-parity.md`, `docs/CLAUDE-details.md`, spec §"As built". Commit.

### Task 5: finish

- [ ] `git log feat/previews-dnd-compare..main`; merge main in if it moved; `./test.sh race` on the merged tree; verify binaries (Linux + Windows .exe) via SendUserFile; ask before merging.
