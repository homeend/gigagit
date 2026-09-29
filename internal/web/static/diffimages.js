// diffimages.js — an image pair in the diff: the TUI's three layouts
// (internal/tui/diff_images.go) — side by side → stacked → one at a time.
// The page reads each side's bytes from the diff's OWN url + &img=old|new
// (d.url, set where the diff was fetched), so every diff source — a commit,
// the working tree, a compare, a link compare — shows its images alike.
import { fmtBytes } from "./core.js";

// --- diffimages pure (guarded against Go) ---
const IMG_LAYOUTS = ["side", "stacked", "single"];
const LAYOUT_WORDS = { side: "side by side", stacked: "stacked", single: "one at a time" };

function nextLayout(l) {
  return IMG_LAYOUTS[(IMG_LAYOUTS.indexOf(l) + 1) % IMG_LAYOUTS.length];
}

// imgInfo is a side's info line: "png 640×480, 12.3 KB" (original pixels).
function imgInfo(m) {
  return `${m.kind} ${m.width}×${m.height}, ${fmtBytes(m.size)}`;
}

// hasImages: a binary diff with at least one image side (an added or
// deleted image has one); hasImagePair: both sides are images — only then
// do old:/new: markers, the layouts and the flip mean anything.
function hasImages(d) {
  return !!(d && d.images && (d.images.old || d.images.new));
}
function hasImagePair(d) {
  return !!(d && d.images && d.images.old && d.images.new);
}

function escA(s) {
  return String(s).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

// sideHTML is one side: its info line and <img>, marked old:/new: only in
// a pair.
function sideHTML(d, side, cls = "") {
  const m = d.images[side];
  const label = hasImagePair(d) ? `${side}: ${imgInfo(m)}` : imgInfo(m);
  return (
    `<div class="dimg-side${cls}" data-side="${side}"><div class="notice">${escA(label)}</div>` +
    `<img class="dimg" src="${escA(d.url + "&img=" + side)}" alt=""></div>`
  );
}

// loneSide is a one-sided pair's image: the one there is.
function loneSide(d) {
  return d.images.new ? "new" : "old";
}

// imagePairHTML is the single-file diff's body for an image diff: a one-sided
// pair is its one image, bare; a pair gets the layout chips, the hint and
// the images in the chosen layout (one at a time: showOld picks the side).
function imagePairHTML(d, layout, showOld) {
  if (!hasImagePair(d)) return `<div class="dimg-wrap">${sideHTML(d, loneSide(d))}</div>`;
  const chips = IMG_LAYOUTS.map(
    (l) => `<button data-layout="${l}"${l === layout ? ' class="on"' : ""}>${LAYOUT_WORDS[l]}</button>`,
  ).join("");
  const hint = layout === "single" ? "w layout · tab / click: old ↔ new" : "w layout";
  const body =
    layout === "single"
      ? sideHTML(d, showOld ? "old" : "new", " dimg-flip")
      : `<div class="${layout === "stacked" ? "dimg-stacked" : "dimg-cols"}">${sideHTML(d, "old")}${sideHTML(d, "new")}</div>`;
  return `<div class="dimg-wrap"><div class="dimg-chips">${chips}<span class="dimg-hint">${hint}</span></div>${body}</div>`;
}

// stackImageHTML is a stack section's body: the pair small and side by side,
// no chips, no keys — open the file for the layouts.
function stackImageHTML(d) {
  const inner = hasImagePair(d) ? sideHTML(d, "old") + sideHTML(d, "new") : sideHTML(d, loneSide(d));
  return `<div class="dimg-stk dimg-cols">${inner}</div>`;
}
// --- end diffimages pure ---

export { IMG_LAYOUTS, hasImagePair, hasImages, imagePairHTML, imgInfo, nextLayout, stackImageHTML };
