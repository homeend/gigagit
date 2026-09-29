# Web Image Diffs Implementation Plan

> **For agentic workers:** executed INLINE by the session that wrote it (project rule: never subagents). Steps use checkbox (`- [ ]`) syntax.

**Goal:** gg web shows an image pair's images (side by side / stacked / one at a time) instead of "binary file".

**Architecture:** the domain differ keeps an image side's original bytes; every web diff handler funnels through `writeDiffJSON`, which adds `images` metadata to the JSON and — for `&img=old|new` — answers that side's bytes. A new `static/diffimages.js` builds the HTML from the payload and the diff URL; files.js, stackview.js and keys.js wire it in.

**Tech Stack:** Go 1.26 stdlib, vanilla ES modules, node for the pure-section JS tests, playwright for the browser check.

**Spec:** `docs/superpowers/specs/2026-09-29-web-image-diff-design.md`

## Global Constraints

- Layouts: `side` → `stacked` → `single`, default `side`; the choice lives in page memory (`state.diffImgLayout`), not storage.
- A one-sided pair: one image, NO `old:`/`new:` marker, no chips, no flip.
- `w` cycles layouts only while a two-sided image pair is on screen in the single-file diff; else it keeps `cycleTextMode`.
- `tab` / image click flip old/new ONLY in `single` with two sides.
- Images never enlarge: `max-width:100%; height:auto`.
- `img=` bytes: `Content-Type: image/<kind>`, `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`; non-image side → 404; bad `img` → 400.
- Stack (S): small side-by-side pair, no chips, no keys. TUI untouched.
- Never `git add -A` (a built gg may sit in the worktree).

## Review Focus

1. A rename of an image (old path ≠ path): `img=old` must serve the OLD path's bytes — the diff already read it; the test pins it via the commit form with `old=`.
2. A working-tree file that stops being an image between the JSON and the `<img>` fetch: the `<img>` error handler must swap in the notice (browser check).
3. A big image (> the 512 px working copy): the page gets full-resolution bytes (`naturalWidth` = original).
4. `w` on a TEXT diff after an image diff was open: must still cycle text mode (state keyed on the current diff, not on a leftover flag).
5. Stack slot with an image + the stack-wide search: an image slot contributes no lines (`hlines([])`) — must not throw.

---

### Task 1: domain keeps image bytes

**Files:** Modify `internal/domain/differ.go`; Test `internal/domain/differ_images_test.go`

**Produces:** `Diff.OldRaw, Diff.NewRaw []byte` — the original bytes of a side that decoded as an image, nil otherwise; `Size()` counts `len(OldRaw)+len(NewRaw)`.

- [ ] **Step 1: failing test** — append to `differ_images_test.go`:

```go
func TestDiffKeepsTheOriginalBytesOfImageSidesOnly(t *testing.T) {
	t.Parallel()
	oldB := pngBytes(t, 8, 4, color.RGBA{255, 0, 0, 255})
	newB := pngBytes(t, 16, 8, color.RGBA{0, 0, 255, 255})
	out, _ := plainDiffer{}.Diff(context.Background(), Request{Old: fixed(oldB), New: fixed(newB)})
	if !bytes.Equal(out.OldRaw, oldB) || !bytes.Equal(out.NewRaw, newB) {
		t.Fatalf("raw bytes not kept: old=%d new=%d", len(out.OldRaw), len(out.NewRaw))
	}
	small := Diff{Binary: true, OldImg: out.OldImg, NewImg: out.NewImg}
	if out.Size() < small.Size()+len(oldB)+len(newB) {
		t.Fatalf("Size() = %d must count the raw bytes", out.Size())
	}
	out, _ = plainDiffer{}.Diff(context.Background(), Request{Old: fixed([]byte("\x00\x01")), New: fixed(newB)})
	if out.OldRaw != nil || out.NewRaw == nil {
		t.Fatalf("a non-image side keeps no bytes: old=%v", out.OldRaw)
	}
}
```

- [ ] **Step 2:** `go test ./internal/domain -run TestDiffKeepsTheOriginalBytes` → FAIL (`OldRaw` undefined).
- [ ] **Step 3: implement** — in `Diff` after `OldDim, NewDim`:

```go
	// OldRaw/NewRaw are the ORIGINAL bytes of a side that decoded as an
	// image (nil otherwise): the web serves them at full resolution.
	OldRaw, NewRaw []byte
```

In `Size()` add `n += len(d.OldRaw) + len(d.NewRaw)`. In `plainDiffer.Diff`'s binary branch after decoding:

```go
		if out.OldImg != nil {
			out.OldRaw = old
		}
		if out.NewImg != nil {
			out.NewRaw = newB
		}
```

- [ ] **Step 4:** `go test ./internal/domain` → PASS.
- [ ] **Step 5:** commit `feat(domain): a diff keeps an image side's original bytes`.

### Task 2: the diff JSON names images; `img=` serves them

**Files:** Modify `internal/web/diff.go`, `internal/web/compare.go`, `internal/web/hunks.go` (only if its `worktreeDiffPayload` call changes); Test `internal/web/diffimages_test.go` (new)

**Consumes:** Task 1's `OldRaw/NewRaw`. **Produces:** JSON `images: {old?: {kind,width,height,size}, new?: {…}}`; `GET <diff url>&img=old|new` → bytes.

- [ ] **Step 1: failing tests** — `internal/web/diffimages_test.go`:

```go
package web

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type imgSide struct {
	Kind   string `json:"kind"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int    `json:"size"`
}
type imgDiffBody struct {
	Binary bool                `json:"binary"`
	Images map[string]imgSide `json:"images"`
}

// imageRepo: c1 adds shot.png (8×4) and blob.bin, c2 changes shot.png (16×8)
// and renames it to moved.png in c3; the working tree then rewrites moved.png
// (20×10, unstaged).
func imageRepo(t *testing.T) (dir string, v1, v2, v3 []byte, c2, c3 string) {
	t.Helper()
	dir = newRepoDir(t, 1)
	v1 = pngFile(t, dir, "shot.png", 8, 4)
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte("\x00\x01\x02"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "add")
	v2 = pngFile(t, dir, "shot.png", 16, 8)
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte("\x00\x09"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "change")
	c2 = strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))
	gitRun(t, dir, "mv", "shot.png", "moved.png")
	gitRun(t, dir, "commit", "-m", "move")
	c3 = strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))
	v3 = pngFile(t, dir, "moved.png", 20, 10)
	return
}

func TestDiffJSONNamesTheImageSides(t *testing.T) {
	t.Parallel()
	dir, v1, v2, _, c2, _ := imageRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	var b imgDiffBody
	if code := getJSON(t, ts, "/api/diff?sha="+c2+"&path=shot.png&status=M", &b); code != 200 || !b.Binary {
		t.Fatalf("code=%d body=%+v", code, b)
	}
	if b.Images["old"] != (imgSide{"png", 8, 4, len(v1)}) || b.Images["new"] != (imgSide{"png", 16, 8, len(v2)}) {
		t.Fatalf("images = %+v", b.Images)
	}
	b = imgDiffBody{}
	getJSON(t, ts, "/api/diff?sha="+c2+"&path=blob.bin&status=M", &b)
	if !b.Binary || b.Images != nil {
		t.Fatalf("a non-image binary names no images: %+v", b)
	}
}

func TestDiffImgServesEachSidesBytes(t *testing.T) {
	t.Parallel()
	dir, v1, v2, v3, c2, c3 := imageRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	base := "/api/diff?sha=" + c2 + "&path=shot.png&status=M"
	for side, want := range map[string][]byte{"old": v1, "new": v2} {
		code, hdr, body := getRaw(t, ts, base+"&img="+side)
		if code != 200 || hdr.Get("Content-Type") != "image/png" || !bytes.Equal(body, want) {
			t.Fatalf("%s: code=%d type=%q len=%d", side, code, hdr.Get("Content-Type"), len(body))
		}
		if hdr.Get("X-Content-Type-Options") != "nosniff" || hdr.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s headers = %v", side, hdr)
		}
	}
	// A rename: the old side is read at the OLD path.
	if code, _, body := getRaw(t, ts, "/api/diff?sha="+c3+"&path=moved.png&old=shot.png&status=R&img=old"); code != 200 || !bytes.Equal(body, v2) {
		t.Fatalf("rename old side: code=%d len=%d", code, len(body))
	}
	// The working tree form.
	if code, _, body := getRaw(t, ts, "/api/diff?wt=unstaged&path=moved.png&img=new"); code != 200 || !bytes.Equal(body, v3) {
		t.Fatalf("wt new side: code=%d len=%d", code, len(body))
	}
	// The rev-pair form.
	c1 := strings.TrimSpace(gitRun(t, dir, "rev-parse", c2+"^"))
	if code, _, body := getRaw(t, ts, "/api/diff?left="+c1+"&right="+c2+"&path=shot.png&status=M&img=old"); code != 200 || !bytes.Equal(body, v1) {
		t.Fatalf("rev form: code=%d len=%d", code, len(body))
	}
	if code, _, _ := getRaw(t, ts, "/api/diff?sha="+c2+"&path=blob.bin&status=M&img=new"); code != http.StatusNotFound {
		t.Fatalf("a non-image side: code=%d, want 404", code)
	}
	if code, _, _ := getRaw(t, ts, base+"&img=both"); code != http.StatusBadRequest {
		t.Fatalf("bad img: code=%d, want 400", code)
	}
}
```

Plus one entry-diff case in the same test file (the commit-spec form of `/api/entry-diff`): read `parseEntrySide`'s accepted spec for a commit (grep `func parseEntrySide` in compare.go) and request `left=<spec c1>&right=<spec c2>&path=shot.png&status=M&img=new` → `v2`.

- [ ] **Step 2:** `go test ./internal/web -run 'TestDiffJSONNamesTheImageSides|TestDiffImgServesEachSidesBytes'` → FAIL (`images` absent / JSON served for `img=`).
- [ ] **Step 3: implement** in `diff.go`:

```go
// imageMeta is one image side on the wire: its format, ORIGINAL pixel size
// and byte count.
type imageMeta struct {
	Kind   string `json:"kind"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int    `json:"size"`
}

// imagesOf names a binary pair's image sides; nil when neither is one.
func imagesOf(d domain.Diff) map[string]imageMeta {
	m := map[string]imageMeta{}
	if d.OldRaw != nil {
		m["old"] = imageMeta{d.OldKind, d.OldDim.X, d.OldDim.Y, len(d.OldRaw)}
	}
	if d.NewRaw != nil {
		m["new"] = imageMeta{d.NewKind, d.NewDim.X, d.NewDim.Y, len(d.NewRaw)}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// writeDiffImage answers `img=old|new`: that side's original bytes when it
// is an image, 404 when it is not, 400 for any other value. The page's
// <img> reads the very bytes the diff compared, whatever the source.
func writeDiffImage(w http.ResponseWriter, d domain.Diff, side string) {
	var raw []byte
	var kind string
	switch side {
	case "old":
		raw, kind = d.OldRaw, d.OldKind
	case "new":
		raw, kind = d.NewRaw, d.NewKind
	default:
		writeErr(w, http.StatusBadRequest, errors.New("img must be old or new"))
		return
	}
	if raw == nil {
		writeErr(w, http.StatusNotFound, errors.New("that side is no image"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/"+kind)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
```

`writeDiffJSON(w, r, d, hunks)`: `if side := r.URL.Query().Get("img"); side != "" { writeDiffImage(w, d, side); return }` then as before. Update the three callers (diff.go ×2, compare.go ×1) to pass `r`. In `diffPayload` add `if im := imagesOf(d); im != nil { payload["images"] = im }`.

Worktree: split `worktreeDiffPayload` into `worktreeDiff(ctx, svc, wt, path, oldPath, pre) (domain.Diff, *diffHunksMeta, error)` (the current body up to the hunk tagging) and `worktreeDiffPayload` = `d, h, err := worktreeDiff(...); return diffPayload(d, h), err` (keep the signature, so hunks.go is untouched). Add a `wantHunks bool` param to `worktreeDiff`: the tagging block runs only when true; `worktreeDiffPayload` passes true. `handleWorktreeDiff`: if `img` is set, call `worktreeDiff(..., false)` and `writeDiffImage`; else unchanged.

- [ ] **Step 4:** `go test ./internal/web` → PASS (whole package: the signature change touches every diff test).
- [ ] **Step 5:** commit `feat(web): the diff names its image sides and serves their bytes`.

### Task 3: `diffimages.js` pure builders + CSS

**Files:** Create `internal/web/static/diffimages.js`; Modify `internal/web/static/style.css` (next to `.vimg`); Test `internal/web/diffimagesjs_test.go`

**Produces (exports):** `IMG_LAYOUTS`, `nextLayout(l)`, `imgInfo(meta)`, `imagePairHTML(d, layout, showOld)`, `stackImageHTML(d)`, `hasImagePair(d)` (two-sided), `hasImages(d)`.

`d.url` is the diff URL (set by the fetch sites in Task 4); a side's src = `d.url + "&img=" + side`.

- [ ] **Step 1: failing test** — `diffimagesjs_test.go`, modelled on `reviewsjs_test.go`'s `runReviewsPure` (copy it as `runDiffImagesPure` with markers `// --- diffimages pure (guarded against Go) ---` / `// --- end diffimages pure ---`; the pure section must not import):

```go
func TestImagePairHTML(t *testing.T) {
	t.Parallel()
	got := runDiffImagesPure(t, `
const two = { url: "/api/diff?sha=a&path=p.png", images: { old: { kind: "png", width: 8, height: 4, size: 1234 }, new: { kind: "png", width: 16, height: 8, size: 2048 } } };
const added = { url: "/api/diff?sha=a&path=p.png", images: { new: { kind: "gif", width: 3, height: 3, size: 10 } } };
console.log(imgInfo(two.images.old));
console.log(nextLayout("side"), nextLayout("stacked"), nextLayout("single"));
const side = imagePairHTML(two, "side", false);
console.log(side.includes('class="dimg-chips"'), (side.match(/<img /g) || []).length, side.includes("old: png 8×4"), side.includes("&amp;img=old"));
const single = imagePairHTML(two, "single", false);
console.log((single.match(/<img /g) || []).length, single.includes("new: png 16×8"), single.includes("tab"));
console.log(imagePairHTML(two, "single", true).includes("old: png 8×4"));
const one = imagePairHTML(added, "side", false);
console.log(one.includes("dimg-chips"), one.includes("new:"), one.includes("gif 3×3"), (one.match(/<img /g) || []).length);
console.log(hasImagePair(two), hasImagePair(added), hasImages(added), hasImages({ binary: true }));
const stk = stackImageHTML(two);
console.log(stk.includes("dimg-stk"), stk.includes("dimg-chips"), (stk.match(/<img /g) || []).length);
`)
	want := strings.Join([]string{
		"png 8×4, 1.2 KB",
		"stacked single side",
		"true 2 true true",
		"1 true true",
		"true",
		"false false true 1",
		"true false true false",
		"true false 2",
	}, "\n")
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
```

(`imgInfo` uses the same KB rule as viewer.js's `fmtBytes` — check its output for 1234 and adjust the literal ONLY if `fmtBytes` formats differently; copy fmtBytes into the pure section rather than importing.)

- [ ] **Step 2:** `go test ./internal/web -run TestImagePairHTML` → FAIL (file missing).
- [ ] **Step 3: implement** `diffimages.js`:

```js
// diffimages.js — an image pair in the diff: the TUI's three layouts
// (internal/tui/diff_images.go). The page reads each side's bytes from the
// diff's own URL + &img=old|new, so every diff source works alike.
import { esc, state } from "./core.js";

// --- diffimages pure (guarded against Go) ---
const IMG_LAYOUTS = ["side", "stacked", "single"];
const LAYOUT_WORDS = { side: "side by side", stacked: "stacked", single: "one at a time" };

function nextLayout(l) {
  return IMG_LAYOUTS[(IMG_LAYOUTS.indexOf(l) + 1) % IMG_LAYOUTS.length];
}
function fmtImgBytes(n) { /* copy of viewer.js fmtBytes */ }
function imgInfo(m) {
  return `${m.kind} ${m.width}×${m.height}, ${fmtImgBytes(m.size)}`;
}
function hasImages(d) { return !!(d && d.images && (d.images.old || d.images.new)); }
function hasImagePair(d) { return !!(d && d.images && d.images.old && d.images.new); }
function escA(s) { return String(s).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;"); }

// sideHTML: one side's info line and <img>; marked old:/new: only in a pair.
function sideHTML(d, side, cls = "") {
  const m = d.images[side];
  const label = hasImagePair(d) ? `${side}: ${imgInfo(m)}` : imgInfo(m);
  return `<div class="dimg-side${cls}" data-side="${side}"><div class="notice">${escA(label)}</div>` +
    `<img class="dimg" src="${escA(d.url + "&img=" + side)}" alt=""></div>`;
}

function imagePairHTML(d, layout, showOld) {
  if (!hasImagePair(d)) return `<div class="dimg-wrap">${sideHTML(d, d.images.new ? "new" : "old")}</div>`;
  const chips = IMG_LAYOUTS.map((l) => `<button data-layout="${l}"${l === layout ? ' class="on"' : ""}>${LAYOUT_WORDS[l]}</button>`).join("");
  const hint = layout === "single" ? "w layout · tab / click old ↔ new" : "w layout";
  let body;
  if (layout === "single") body = sideHTML(d, showOld ? "old" : "new", " dimg-flip");
  else body = `<div class="dimg-${layout}">${sideHTML(d, "old")}${sideHTML(d, "new")}</div>`;
  return `<div class="dimg-wrap"><div class="dimg-chips">${chips}<span class="dimg-hint">${hint}</span></div>${body}</div>`;
}

// stackImageHTML: the stack section's small side-by-side pair, no controls.
function stackImageHTML(d) {
  const inner = hasImagePair(d) ? sideHTML(d, "old") + sideHTML(d, "new") : sideHTML(d, d.images.new ? "new" : "old");
  return `<div class="dimg-stk dimg-side-by">${inner}</div>`;
}
// --- end diffimages pure ---
```

(The pure section uses its own `escA`, not core.js's `esc`, so node can run it without imports. Drop the `esc` import if unused.)

CSS beside `.vimg`:

```css
.dimg-wrap { padding: 6px 8px; }
.dimg-chips { display: flex; gap: 4px; align-items: center; margin-bottom: 6px; }
.dimg-chips button.on { background: var(--sel-bg); }
.dimg-hint { margin-left: 8px; color: var(--muted); }
.dimg-side, .dimg-stacked { display: grid; gap: 8px; }
.dimg-side-by, .dimg-side { }
.dimg-side { min-width: 0; }
.dimg-wrap > .dimg-side:only-child, .dimg-stacked { grid-template-columns: 1fr; }
.dimg { display: block; max-width: 100%; height: auto; margin-top: 4px; }
.dimg-flip .dimg { cursor: pointer; }
.dimg-stk .dimg { max-height: 240px; }
```

Container rules: the two-column containers (`.dimg-side` layout wrapper — rename it `dimg-cols` to avoid clashing with the per-side `.dimg-side` class, in both JS and CSS; `.dimg-side-by` in the stack → also `dimg-cols`) get `display:grid; grid-template-columns: 1fr 1fr; gap: 8px`. Colour tokens: use the names style.css already defines for a selected chip and muted text (grep `--` in `:root`).

- [ ] **Step 4:** `go test ./internal/web -run TestImagePairHTML` → PASS.
- [ ] **Step 5:** commit `feat(web): diffimages.js — the image pair's HTML`.

### Task 4: wiring — fetch sites, diffHTML, stack, keys, clicks, hints

**Files:** Modify `static/files.js`, `static/stackview.js`, `static/keys.js`, `static/core.js` (state fields), `static/filehist.js` (only if its body goes through `diffHTML`); Test `internal/web/diffimagesjs_test.go` (wiring guards).

**Consumes:** Task 3's exports; Task 2's `img=`.

- [ ] **Step 1: failing wiring test** — using the existing `wiringCheck(t, file, wants...)` helper from reviewsjs_test.go:

```go
func TestImageDiffWiring(t *testing.T) {
	t.Parallel()
	wiringCheck(t, "files.js", "imagePairHTML(", "getDiff(", "dimg-chips")
	wiringCheck(t, "stackview.js", "stackImageHTML(", "getDiff(")
	wiringCheck(t, "keys.js", "cycleImageLayout(", "flipImage(")
	wiringCheck(t, "core.js", "diffImgLayout", "diffImgOld")
}
```

- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: implement.**
  - `core.js` state: `diffImgLayout: "side", // the image pair's layout (w), page memory like the TUI's session default` and `diffImgOld: false, // one at a time: the old side is up (tab / click)`.
  - `files.js`: `async function getDiff(url) { const d = await getJSON(url); d.url = url; return d; }` (export it). Replace the fetches at the `fileDiffURL(f)` sites (openFile ×2, the 3692 site) and the entry-diff fetch (~743) with `getDiff(url)`. stackview.js's two `getJSON(fileDiffURL(s.f))` → `getDiff(fileDiffURL(s.f))`. filehist.js: if `renderHistoryDiff` paints through `diffHTML`, use `getDiff` there too; else leave it.
  - `diffHTML`: `if (d.binary) return (hlines([]), hasImages(d) && d.url ? (hctx ? stackImageHTML(d) : imagePairHTML(d, state.diffImgLayout, state.diffImgOld)) : `<div class="notice">binary file</div>`);` — hctx is present only for the stack (check that the single-file renderDiff passes `null` for hctx — it does, line ~1790).
  - `renderDiff`: when `d !== state.lastDiff`, reset `state.diffImgOld = false`. After painting, if `hasImagePair(d)`, set the footer `textmode` chip's text to `w layout`; else call `applyTextMode(state.textMode)` to restore it.
  - `cycleImageLayout()` / `flipImage()` in files.js (exported): each returns false when the current single-file diff (`!state.stack && state.layout === "diff" && state.lastDiff`) has no two-sided image pair (flip: also when layout ≠ single); otherwise updates state and `renderDiff(state.lastDiff)`, returns true.
  - Clicks: one delegated listener on `#diff-body`: `.dimg-chips button[data-layout]` → set layout + re-render; `.dimg-flip .dimg` → `flipImage()`.
  - `<img>` failure: delegated `error` listener on `#diff-body` with capture (`addEventListener("error", fn, true)`) — for an `img.dimg`, replace its `.dimg-side` innerHTML with `<div class="notice">binary file</div>`.
  - `keys.js`: the `w` branch → `if (!cycleImageLayout()) cycleTextMode();`. Add before it: `else if (e.key === "Tab" && !e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey && flipImage()) { e.preventDefault(); }` — placed so a Tab that is not consumed falls through to the browser (it is an `else if` whose condition calls flipImage; when false the chain continues to later branches, none of which take Tab).
  - Help: `registerHelp({ key: "w · image layout", html: "on an image pair: cycle <b>side by side</b> → <b>stacked</b> → <b>one at a time</b> (the TUI's ctrl+w); in one at a time <b>tab</b> or a click flips old ↔ new. An added or deleted image shows alone." })`.
- [ ] **Step 4:** `go test ./internal/web` → PASS; `node --check` each touched JS file.
- [ ] **Step 5:** commit `feat(web): image pairs in the diff — three layouts, w / tab, stack pair`.

### Task 5: browser check, docs

**Files:** playwright probe in the scratchpad (not committed); Modify `CHANGELOG.md`, `README.md` (web section, if it lists diff features).

- [ ] **Step 1:** scratch fixture under the session scratchpad: repo with `shot.png` 8×4 → 1200×600 (c2, bigger than 512 to prove full resolution), `new.png` added in c2, `moved.png` rename, working copy rewrite; XDG_STATE_HOME/XDG_CONFIG_HOME pinned to scratch dirs.
- [ ] **Step 2:** probe (print binary md5): open c2 → shot.png: two visible `img.dimg`, `naturalWidth` 8 and 1200; chips visible, `side by side` on; `w` → `.dimg-stacked` visible; `w` → one `img`, info says `new:`; `Tab` → `old:`; click img → `new:`; open new.png: one img, no `old:`/`new:` text, no chips; open a text file then `w` → the `#diff-mode` text changes (text mode still cycles); `S` → `.dimg-stk` with 2 imgs visible; working tree: `moved.png` unstaged → 2 imgs.
- [ ] **Step 3:** run the probe against the UNFIXED installed `gg` → failures expected; then against the worktree build → ALL PASS.
- [ ] **Step 4:** CHANGELOG entry (+ README line if applicable); commit `docs: web image diffs`.
- [ ] **Step 5:** `./test.sh race` (gofmt stage first); deliver the verify binary; ask the user before merging.
