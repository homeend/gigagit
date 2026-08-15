// core.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.

const ROW_H = 22;


const state = {
  rows: [],
  canLoadMore: false,
  loadingMore: false,
  cursor: 0,
  files: [],
  fileCursor: 0,
  fileSha: null,
  pane: "commits", // commits | files
  layout: "list", // list (commits full-width) | detail (files+diff, list hidden)
  // svg is the only graph renderer: browser rows (22px) are taller than
  // the 13px font box, so text glyphs would leave vertical gaps. g toggles
  // the graph off entirely — a flat ●-gutter list (TUI show_graph parity)
  // with the lane column's space going to subjects.
  graphMode: "svg", // svg | off
  health: null, // /api/health payload (big-repo banner), else null
  wt: null, // /api/status payload while the tree is dirty, else null
  conflict: null, // {op, source, target, desc, conflicted} while a sequencer op is paused, else null
  filesMode: "commit", // commit | status | compare
  compare: null, // {a, b, aHash, bHash, all, filter, originsError} while comparing two branches
  statusEntries: [],
  // status-file paths marked for batch actions (status mode only; a Set so
  // any module may add/remove — mutation, not reassignment)
  marked: new Set(),
  branches: [],
  worktrees: [],
  tags: [],
  tagsTruncated: false,
  stashes: [],
  sidebar: true,
  op: null, // {id, es: EventSource} while an operation is live
  lastDiff: null,
  diffCtx: null, // {path, rev} — the file the diff pane currently shows, else null
  diffBlockIdx: -1,
  detailGen: 0,
  dragBranch: null, // name of the branch being dragged, else null
  solo: "", // branch the commit list is narrowed to ("" = every branch)
  // A parked (backgrounded) long task, and then its result until collected:
  // {label, status: running|done|failed|cancelled, title, path, report, error}
  task: null,
  cfilter: null, // {q, matches: [feedIdx...]} while the commits quick filter (/) is active, else null
  gotoGen: 0, flashHash: "",
};


const $ = (id) => document.getElementById(id);


// Destructive decision options render red in the modal (the ctx-menu
// danger precedent). Options are English protocol values — i18n never
// translates them — so a client-side set is reliable.
const DANGER_OPTIONS = new Set([
  "force", "force-with-lease", "force-delete", "reset", "delete", "drop",
  "unlock-and-remove", "discard", "overwrite", "hard",
  "abort merge", "abort rebase", "abort cherry-pick", "abort revert",
]);


const SECTIONS = ["branches", "remotes", "worktrees", "tags", "stashes", "reflog"];


// localStorage can throw (private mode); persistence is best-effort.
function lsGet(k) { try { return localStorage.getItem(k); } catch { return null; } }

function lsSet(k, v) { try { localStorage.setItem(k, v); } catch {} }


// sessionStorage-backed: the big-repo banner's "not now" dismissal is
// per-tab-session only (re-evaluated next visit), unlike localStorage's
// gg.graph override which persists across sessions.
function ssGet(k) { try { return sessionStorage.getItem(k); } catch { return null; } }

function ssSet(k, v) { try { sessionStorage.setItem(k, v); } catch {} }


async function getJSON(url) {
  const resp = await fetch(url);
  const body = await resp.json();
  if (!resp.ok) throw new Error(body.error || resp.statusText);
  return body;
}


async function postJSON(url, body) {
  const resp = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const data = await resp.json();
  if (!resp.ok) {
    const err = new Error(data.error || resp.statusText);
    err.data = data; // structured refusals (e.g. reroot's repairable handshake)
    throw err;
  }
  return data;
}


function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}


function runes(s) {
  return Array.from(s);
}


// --- path elision (port of internal/tui/elide.go) ---
// The whole UI is monospace, so "display columns" are just characters here and
// the arithmetic matches the TUI's exactly. Keep the two in sync: the browser
// check in the CDP harness compares this against the Go implementation's
// output over a shared case table.

// splitPathSegs tokenizes a path (no trailing separator) into {sep, text}
// segments, preserving each segment's preceding separator run verbatim.
function splitPathSegs(s) {
  const segs = [];
  let cur = { sep: "", text: "" };
  for (const r of runes(s)) {
    if (r === "/" || r === "\\") {
      if (cur.text !== "") { segs.push(cur); cur = { sep: "", text: "" }; }
      cur.sep += r;
    } else {
      cur.text += r;
    }
  }
  segs.push(cur);
  return segs;
}


// elidePath shortens a filesystem path to at most n columns by dropping WHOLE
// segments from the middle, marked by a single "…". Segments survive by
// priority: the final one (the file/repo name) first, then the directory just
// before it, then the path's FIRST segment, then alternating right/left
// working inward. The dropped run is always contiguous, so the result reads
// head + "…" + tail. When not even "…/<name>" fits, the name itself is cut in
// the middle.
function elidePath(s, n) {
  if (n <= 0) return "";
  if (runes(s).length <= n) return s;
  if (n === 1) return "…";
  const trimmed = s.replace(/[/\\]+$/, "");
  const trail = s.slice(trimmed.length);
  if (trimmed === "") return runes(s).slice(0, n).join(""); // a pure separator run
  const segs = splitPathSegs(trimmed);
  const last = segs.length - 1;
  if (last === 0) return elideNameMiddle(segs[0].text, n - runes(trail).length) + trail;
  // build renders segs[:keepL], a "…" for the dropped middle, then segs[keepR:].
  // The "…" borrows the first dropped segment's separator so it slots into the
  // path ("/mnt/…/name"); with no prefix kept it leads bare ("…/name").
  const build = (keepL, keepR) => {
    let b = "";
    for (let i = 0; i < keepL; i++) b += segs[i].sep + segs[i].text;
    if (keepL < keepR) {
      if (keepL > 0) b += segs[keepL].sep;
      b += "…";
    }
    for (let i = keepR; i < segs.length; i++) b += segs[i].sep + segs[i].text;
    return b + trail;
  };
  if (runes(build(0, last)).length > n) {
    return elideNameMiddle(segs[last].text, n - runes(trail).length) + trail;
  }
  // Grow both kept runs inward, right first, in strict priority order. A side
  // closes permanently once its next segment no longer fits (widths only grow);
  // skipping it for a deeper segment would leave a second gap.
  let keepL = 0, keepR = last, leftNext = 0, rightNext = last - 1;
  let leftOpen = true, rightOpen = true;
  while (leftOpen || rightOpen) {
    if (rightOpen) {
      if (rightNext >= keepL && runes(build(keepL, rightNext)).length <= n) {
        keepR = rightNext;
        rightNext--;
      } else {
        rightOpen = false;
      }
    }
    if (leftOpen) {
      if (leftNext < keepR && runes(build(leftNext + 1, keepR)).length <= n) {
        keepL = leftNext + 1;
        leftNext++;
      } else {
        leftOpen = false;
      }
    }
  }
  return build(keepL, keepR);
}


// elideNameMiddle cuts a bare name (no separators) to at most n columns by
// dropping its MIDDLE: the beginning plus the extension — or, with no
// extension, the ending — survive around a "…".
function elideNameMiddle(s, n) {
  if (n <= 0) return "";
  const r = runes(s);
  if (r.length <= n) return s;
  if (n === 1) return "…";
  let tail = "";
  const dot = s.lastIndexOf("."); // >0: a dotfile is a name, not an extension
  if (dot > 0) tail = s.slice(dot);
  if (tail === "" || runes(tail).length + 2 > n) {
    tail = tailWidth(s, Math.floor((n - 1) / 3));
  }
  return r.slice(0, n - 1 - runes(tail).length).join("") + "…" + tail;
}


// tailWidth returns the longest trailing run of s at most w columns wide.
function tailWidth(s, w) {
  const r = runes(s);
  for (let lo = 0; lo < r.length; lo++) {
    if (r.length - lo <= w) return r.slice(lo).join("");
  }
  return "";
}


// charWidth measures one monospace column in CSS pixels, so a pixel budget can
// be turned into the column count elidePath takes. Measured once against the
// body font and cached; the probe is removed immediately.
let charW = 0;
function charWidth() {
  if (charW) return charW;
  const probe = document.createElement("span");
  probe.textContent = "0".repeat(100);
  probe.style.cssText = "position:absolute;visibility:hidden;white-space:pre;";
  document.body.appendChild(probe);
  charW = probe.getBoundingClientRect().width / 100 || 7.8;
  probe.remove();
  return charW;
}

export { $, DANGER_OPTIONS, ROW_H, SECTIONS, charWidth, elideNameMiddle, elidePath, esc, getJSON, lsGet, lsSet, postJSON, runes, splitPathSegs, ssGet, ssSet, state };
