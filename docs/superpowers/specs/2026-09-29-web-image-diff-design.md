# Web image diffs — design

Date: 2026-09-29 · Status: agreed in chat, awaiting spec review

## Goal

When gg web opens a diff whose two sides are a binary pair with at least one
side decodable as an image (PNG/JPEG/GIF — the differ's `decodeImage` rule),
it shows the images instead of the "binary file" notice, in the TUI's three
layouts (`internal/tui/diff_images.go`): side by side → stacked → one at a
time.

## User rulings (2026-09-29)

1. **Layout control:** `w` cycles the layouts while an image pair is shown
   (text diffs keep `w` = scroll/wrap/cutoff), plus a clickable chip row in
   the diff showing the current layout. ctrl+w is impossible in a browser.
2. **Flip old/new:** `tab` and a click on the image, in one-at-a-time ONLY.
3. **Sizing:** fit the box, never enlarge past 100%; full-resolution bytes.
4. **Stack (S):** an image section shows the pair small, side by side — no
   chips, no keys, no cycling. The TUI stack is unchanged.

Carried over from the TUI rulings: a one-sided pair (added / deleted /
untracked) shows the one image with NO `old:`/`new:` marker, no chips and no
flip; the layout choice lasts for the session (page memory, not storage).

## Server

Every web diff source ends in `writeDiffJSON` / `diffPayload`
(`internal/web/diff.go`): the commit form (`sha=`), the rev pair
(`left=`/`right=`), the working tree (`wt=unstaged|staged`) and the entry /
link compare (`/api/entry-diff`, `compare.go`). That is the one choke point.

- **domain:** `Diff` gains `OldRaw, NewRaw []byte` — the ORIGINAL bytes of a
  side that decoded as an image (nil otherwise). `Size()` counts them, so the
  diff cache's byte budget stays honest. The TUI ignores them.
- **JSON:** `diffPayload` adds `images` when either side is an image:
  `{"old": {"kind","width","height","size"}, "new": {…}}`; an absent or
  non-image side is omitted. `width/height` are the ORIGINAL pixels
  (`OldDim/NewDim`), `size` the byte count.
- **Bytes:** the SAME diff URL with `img=old|new` answers the side's raw
  bytes instead of JSON: `Content-Type: image/<kind>`, `X-Content-Type-
  Options: nosniff`, `Cache-Control: no-store`. A side that is not an image
  (or a diff that is not binary) → 404. Any other `img` value → 400. The
  diff handlers pass the request into the choke point
  (`writeDiffJSON(w, r, d, hunks)`). The working-tree branch builds a payload
  map (`worktreeDiffPayload`, shared with hunk staging): its diff step is
  split out (`worktreeDiff` → `domain.Diff` + hunk meta) so the handler can
  answer `img=` from the `domain.Diff` without building hunks.

No new route and no per-source raw reader: an `<img>` works for every diff
source the page can open, with exactly the bytes the diff compared.

## Client

New module `static/diffimages.js`:

- **Pure section** (guarded, Node-tested like `reviewsjs_test.go`):
  `imagePairHTML(d, url, layout, showOld)` → the single-file body;
  `stackImageHTML(d, url)` → the stack section body; `imgInfo(side)` →
  `png 640×480, 12.3 KB`; `sideLabel(d, old)` → `old: …` / `new: …` /
  bare for a one-sided pair; `nextLayout(layout)`.
- **Layouts:** `side` (two columns, each image `max-width:100%`), `stacked`
  (old above new, full width), `single` (one image, the info line says which;
  `tab`/click flips). Never upscaled (`max-width:100%; height:auto`, no
  `width:100%`). A one-sided pair always renders as a single, unmarked image.
- **Chips:** `side by side · stacked · one at a time` above the images,
  current one highlighted, clickable; hidden for a one-sided pair.
- **State:** `state.diffImgLayout` (default `side`) and `state.diffImgOld`
  (false; reset when another file opens).
- **Keys** (`keys.js`): with an image pair on screen in the single-file
  diff, `w` → next layout (instead of `cycleTextMode`); `tab` → flip, only in
  `single` and two-sided (tab is otherwise left to the browser).
- **Hints:** the footer and the `?` help mention `w layout` and (one at a
  time) `tab old/new` while an image pair is shown.
- **Failure:** an `<img>` whose load fails is replaced by that side's
  `binary file` notice (e.g. the working copy stopped being an image between
  the JSON and the bytes).
- **files.js:** `diffHTML`'s `d.binary` branch paints `imagePairHTML` when
  `d.images` is present. **stackview.js:** the binary section paints
  `stackImageHTML` when `d.images` is present.

## Out of scope

The TUI stack's `(binary file)`, image formats beyond PNG/JPEG/GIF, zoom,
pixel-difference overlays.

## Testing

- Go (`internal/domain`): raw bytes kept only for image sides; `Size()`
  counts them.
- Go (`internal/web`): `images` metadata for a two-sided, an added and a
  non-image binary pair; `img=old|new` bytes + headers; 404 for a
  non-image side; 400 for a bad `img`; at least the commit form and the
  working-tree form.
- Node (pure section): layouts, one-sided rules, chip visibility, info text,
  `nextLayout` cycle.
- Playwright against the NEW binary (md5 printed; run the unfixed build
  first): scratch repo (XDG_STATE_HOME/XDG_CONFIG_HOME pinned) with a PNG
  changed across commits, a PNG added, and a modified working copy; assert
  visible `<img>`s with `naturalWidth > 0` in each layout, `w` cycling, `tab`
  and click flipping, no marker on the added image, the stack's small pair.
