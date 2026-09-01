// commits.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, ROW_H, defaultWorktreePath, esc, getJSON, lsGet, lsSet, postJSON, runes, state } from "./core.js";
import { saveUI } from "./uistate.js";
import { copyText, openPrompt, showCtxMenu } from "./layers.js";
import { wtCount, wtExtra, wtRowHTML } from "./status.js";
import { followOp, opBusy, opLine, openCreateBranchPrompt, showLocalConfirm, startOp } from "./ops.js";
import { rev, startReview } from "./review.js";
import { addCommitEntry } from "./sidebar.js";
import { commitMetaLine, drillOut, enterFilesStage, openCompare, openWorkingTree, renderFiles, setFilesMeta } from "./files.js";
import { focusPane, moveCursor } from "./keys.js";
import { extraRows } from "./menus.js";

// Feed-scoped state that lives on the shared object so search.js (which owns
// the filter bar) can set it without this module importing that one — the
// import runs the other way.
state.feedFilter = state.feedFilter || {}; // {path,author,grep,since,until}
state.feedFiltered = false; // the server answered a filtered (subset) page
state.feedGen = 0; // request generation: a page from an older one is dropped
state.cmarks = new Set(); // ◉ marked commit hashes (compare a pair, squash a run)
state.eagerQuery = ""; // the last deep-search query, kept so ctrl+f can repeat
state.eagerGen = 0;

// Left-click on a branch is a READ: jump the commit list to its tip (the
// TUI's enter-on-branch behavior). Mutations (switch) live behind the
// right-click menu — a single stray click must never start an operation.
async function gotoBranchTip(b) {
  // /api/branches carries a SHORT hash (%(objectname:short)); feed rows a
  // full one — match by prefix, never equality.
  let disp = () => state.rows.findIndex((r) => r.hash.startsWith(b.hash));
  let idx = disp();
  let guard = 0;
  while (idx < 0 && state.canLoadMore && guard < 20) {
    await loadCommits(true); // page deeper — an all-branches feed keeps tips near the top
    idx = disp();
    guard++;
  }
  if (idx < 0) {
    opLine("tip of " + b.name + " not in loaded history", true);
    return;
  }
  state.cursor = idx + wtCount();
  state.pane = "commits";
  moveCursor(0); // clamp + scroll into view + render
  focusPane();
}


// --- commits pane (virtualized: only visible rows exist in the DOM) ---

function renderCommits() {
  if (state.cfilter) return renderFilteredCommits();
  const scroll = $("commits-scroll");
  const total = state.rows.length + wtCount();
  $("commits-spacer").style.height = total * ROW_H + wtExtra() + "px";
  const first = Math.max(0, Math.floor(scroll.scrollTop / ROW_H) - 10);
  const last = Math.min(total, Math.ceil((scroll.scrollTop + scroll.clientHeight) / ROW_H) + 10);
  const win = $("commits-window");
  win.style.top = first * ROW_H + (first > 0 ? wtExtra() : 0) + "px";
  let html = "";
  for (let i = first; i < last; i++) {
    // A server-filtered feed is a non-contiguous subset, so its rows draw flat
    // for the same reason the / filter's do: lanes between commits that are
    // not parent and child would be a drawing, not a graph.
    html += state.wt && i === 0 ? wtRowHTML(i) : rowHTML(state.rows[i - wtCount()], i, state.feedFiltered);
  }
  win.innerHTML = html;
  maybeLoadMore(last - wtCount());
}


function rowHTML(row, i, flat) {
  const sel = i === state.cursor ? " sel" : "";
  const fl = row.hash === state.flashHash ? " flash" : "";
  const mark = state.cmarks.has(row.hash) ? "◉ " : "";
  const refs = (row.refs || [])
    .map((r) => `<span class="ref ${r.kind}${r.head ? " head" : ""}">${esc(r.name)}</span>`)
    .join("");
  const when = new Date(row.time * 1000).toISOString().slice(0, 10);
  const graph = flat
    ? (() => { const col = runes(row.cells || "").indexOf("●"); return flatDotSVG(dotColor(row, col >= 0 ? col >> 1 : 0)); })()
    : graphHTML(row, i - wtCount());
  return (
    `<div class="crow${sel}${fl}" data-i="${i}">` +
    `<span class="graph">${graph}</span>` +
    `<span class="subj">${mark}${refs}${esc(row.subject)}</span>` +
    `<span class="meta">${esc(row.author)} · ${row.short} · ${when}</span></div>`
  );
}


// --- commits quick filter (/) ----------------------------------------------
// Client-only narrowing of the LOADED feed rows: case-insensitive substring
// on subject and author, sha PREFIX when the query is hex. Filtered rows
// render flat — lanes are meaningless on a subset. Deeper search is always
// an explicit click on the hint row, never an automatic git walk.
function openCommitFilter() {
  if (state.layout === "diff") {
    opLine("filter works on the commit list — press esc to it first", false);
    return; // commits pane is off-screen
  }
  $("cfilter").classList.remove("hidden");
  const input = $("cfilter-input");
  input.value = state.cfilter ? state.cfilter.q : "";
  applyCommitFilter();
  input.focus();
}


function closeCommitFilter() {
  const open = !$("cfilter").classList.contains("hidden");
  if (!open && !state.cfilter) return;
  state.cfilter = null;
  $("cfilter-input").value = "";
  $("cfilter-input").blur(); // a focused input would trap all global keys
  $("cfilter").classList.add("hidden");
  // moveCursor(0) re-renders AND rescrolls to the selected row — but only
  // steers the commits list while that pane has focus; otherwise plain render.
  if (state.pane === "commits") moveCursor(0);
  else renderCommits();
}


function applyCommitFilter() {
  const q = $("cfilter-input").value.trim().toLowerCase();
  if (!q) {
    state.cfilter = null; // empty query = unfiltered, bar stays open
    $("cfilter-count").textContent = "";
    renderCommits();
    return;
  }
  const hexish = /^[0-9a-f]+$/.test(q);
  const matches = [];
  state.rows.forEach((r, i) => {
    if (commitMatches(r, q, hexish)) matches.push(i);
  });
  state.cfilter = { q, matches };
  $("cfilter-count").textContent = matches.length + " / " + state.rows.length;
  $("commits-scroll").scrollTop = 0;
  renderCommits();
}


// Filtered render: the same virtualized window, over the match list, plus a
// trailing hint row stating coverage. The working-tree row is not a commit
// and stays out of a filtered list.
function renderFilteredCommits() {
  const scroll = $("commits-scroll");
  const m = state.cfilter.matches;
  const total = m.length + 1; // + hint row
  $("commits-spacer").style.height = total * ROW_H + "px";
  const first = Math.max(0, Math.floor(scroll.scrollTop / ROW_H) - 10);
  const last = Math.min(total, Math.ceil((scroll.scrollTop + scroll.clientHeight) / ROW_H) + 10);
  const win = $("commits-window");
  win.style.top = first * ROW_H + "px";
  let html = "";
  for (let i = first; i < last; i++) {
    if (i === m.length) {
      const tail = state.canLoadMore
        ? ` — <a id="cfilter-more">load more</a> · <a id="cfilter-deeper">search deeper</a> (ctrl+f)`
        : " — all of history searched";
      html += `<div class="crow hintrow">${m.length} of ${state.rows.length} loaded commits match${tail}</div>`;
      continue;
    }
    html += rowHTML(state.rows[m[i]], m[i] + wtCount(), true);
  }
  win.innerHTML = html;
}


// --- deep search (ctrl+f) ---------------------------------------------------
// The / filter narrows what is LOADED. This pages what is not: each press digs
// past the current end of the feed looking for the next match, so a repeated
// press keeps going deeper instead of landing on the same row. The query
// survives the pass (state.eagerQuery) so it can be repeated after the filter
// bar is closed — the TUI's eagerSearch keeps it for the same reason.

// commitMatches is the ONE match predicate: substring on subject and author,
// sha PREFIX when the query is hex. The / filter and the deep search must
// agree — a deeper hit the visible filter then hides is a bug, not a result.
function commitMatches(r, q, hexish) {
  return (
    r.subject.toLowerCase().includes(q) ||
    (r.author || "").toLowerCase().includes(q) ||
    (hexish && r.hash.startsWith(q))
  );
}


// firstFeedMatch returns the feed index of the first match at or after from,
// or -1. from is the floor that makes each press dig deeper.
function firstFeedMatch(query, from) {
  const q = query.toLowerCase();
  const hexish = /^[0-9a-f]+$/.test(q);
  for (let i = Math.max(0, from); i < state.rows.length; i++) {
    if (commitMatches(state.rows[i], q, hexish)) return i;
  }
  return -1;
}


// eagerPages is how many pages one pass walks before asking. The pages are
// server-side (200 commits each by default), so a pass covers thousands of
// commits — but it is bounded, because "search all 600k" is a decision the
// user should get to make, not a default.
const EAGER_PAGES = 20;


async function searchDeeper(query) {
  const q = (query || (state.cfilter && state.cfilter.q) || state.eagerQuery || "").trim();
  if (!q) {
    openCommitFilter(); // nothing to dig for yet — ask for the query first
    return;
  }
  state.eagerQuery = q;
  // Dig PAST what is loaded: a match already on screen is not what this is
  // for, so the floor is the current end of the feed.
  await eagerPass(q, state.rows.length, ++state.eagerGen);
}


async function eagerPass(q, from, gen) {
  opLine("⟳ searching deeper for " + q + "…");
  for (let page = 0; page < EAGER_PAGES; page++) {
    const hit = firstFeedMatch(q, from);
    if (hit >= 0) {
      opLine("found " + q + " " + (from > 0 ? "deeper in history" : "in the loaded list"));
      landOnFeedIdx(hit);
      return;
    }
    if (!state.canLoadMore) {
      opLine("no further match for " + q + " — all of history searched", false);
      return;
    }
    const before = state.rows.length;
    await loadCommits(true);
    if (gen !== state.eagerGen) return; // a newer search superseded this pass
    if (state.rows.length === before) {
      opLine("no further match for " + q + " — all of history searched", false);
      return;
    }
  }
  const hit = firstFeedMatch(q, from);
  if (hit >= 0) {
    landOnFeedIdx(hit);
    return;
  }
  const scanned = state.rows.length - from;
  showLocalConfirm(
    "Searched " + scanned + " more commits, no match for “" + q + "”. Keep digging?",
    ["search deeper", "stop"],
    (o) => {
      if (o === "search deeper") eagerPass(q, from, gen);
    }
  );
}


// landOnFeedIdx puts the cursor on a feed row and shows it. Unlike
// revealCommit it does NOT clear the / filter: the deep query and the filter
// text are the same thing here, so every hit is visible in the filtered list
// and the next ctrl+f still has something to dig with.
function landOnFeedIdx(i) {
  let guard = 0;
  while (state.layout !== "list" && guard++ < 3) drillOut();
  state.pane = "commits";
  state.cursor = i + wtCount();
  state.flashHash = state.rows[i].hash;
  moveCursor(0); // clamps, scrolls (filtered or not) and renders
  focusPane();
  setTimeout(() => {
    state.flashHash = "";
    renderCommits();
  }, 1700);
}


function graphHTML(row, feedIdx) {
  if (state.graphMode === "off") {
    // flat mode: one dot per row in the commit's lane color — dots keep
    // rows visually separate (full-height bars merged into one line).
    // Drawn as a ONE-CELL SVG with the graph's own geometry so its centre
    // lands exactly on the leftmost lane's centre; a text glyph would
    // centre wherever the font's advance width happens to put it.
    const col = runes(row.cells || "").indexOf("●");
    return flatDotSVG(dotColor(row, col >= 0 ? col >> 1 : 0));
  }
  return graphSVG(row, feedIdx);
}


// dotColor picks a commit's node color: its territory segment when the
// server sent one (a soloed feed — a linear history is one lane, so lane
// color alone can't show where the soloed branch's own commits end and the
// inherited history begins), else its lane. Lines and edges keep lane color.
function dotColor(row, lane) {
  return laneColor(row.seg != null ? row.seg : lane);
}


// flatDotSVG draws a single node dot in a one-cell box, identical in
// geometry to graphSVG's leftmost-lane circle. It keeps the .flatdot class
// so the existing spacing rule still applies — graph mode's own spacing
// must not change.
function flatDotSVG(color) {
  return (
    `<svg class="flatdot" width="${CELL_W}" height="${ROW_H}" viewBox="0 0 ${CELL_W} ${ROW_H}">` +
    `<circle cx="${HALF}" cy="${MID}" r="4" fill="${color}"/></svg>`
  );
}


function toggleGraphMode() {
  applyGraphMode(state.graphMode === "svg" ? "off" : "svg");
  lsSet("gg.graph", state.graphMode); // same-session cache
  saveUI({ graph: state.graphMode }); // the copy that survives a restart
}


// applyGraphMode is the shared "render the graph this way" step, used by the
// toggle, by the big-repo banner and by the layout restored at boot.
function applyGraphMode(mode) {
  state.graphMode = mode === "off" ? "off" : "svg";
  renderCommits();
}


// Restore the persisted graph mode before the first render.
if (lsGet("gg.graph") === "off") state.graphMode = "off";


const CELL_W = 14;

const HALF = CELL_W / 2;

const MID = ROW_H / 2;


const laneColors = [];

function laneColor(i) {
  if (!laneColors.length) {
    const cs = getComputedStyle(document.documentElement);
    for (let k = 0; k < 8; k++) laneColors.push(cs.getPropertyValue(`--lane${k}`).trim());
  }
  return laneColors[i % 8];
}


// Each glyph maps to stroke path(s) inside its CELL_W x ROW_H box, keyed by
// which cell edges the glyph connects (top/bottom at center-x, left/right at
// center-y).
const GLYPH_PATHS = {
  "│": (x) => `M${x + HALF},0 V${ROW_H}`,
  "─": (x) => `M${x},${MID} H${x + CELL_W}`,
  "╭": (x) => `M${x + CELL_W},${MID} Q${x + HALF},${MID} ${x + HALF},${ROW_H}`,
  "╮": (x) => `M${x},${MID} Q${x + HALF},${MID} ${x + HALF},${ROW_H}`,
  "╰": (x) => `M${x + HALF},0 Q${x + HALF},${MID} ${x + CELL_W},${MID}`,
  "╯": (x) => `M${x + HALF},0 Q${x + HALF},${MID} ${x},${MID}`,
  "┬": (x) => `M${x},${MID} H${x + CELL_W} M${x + HALF},${MID} V${ROW_H}`,
  "┴": (x) => `M${x},${MID} H${x + CELL_W} M${x + HALF},0 V${MID}`,
  "┼": (x) => `M${x + HALF},0 V${ROW_H} M${x},${MID} H${x + CELL_W}`,
};


// Node-cell continuity: Lay emits a bare ● on a commit's own row — in a
// terminal the lane's continuation is implied by cell adjacency, but at
// 22px web rows the gap shows, so the node draws up/down stubs whenever the
// neighboring feed row's SAME column carries ink touching the shared edge.
const TOP_TOUCH = new Set(["│", "╰", "╯", "┴", "┼", "●"]); // ink touches its row's top edge

const BOT_TOUCH = new Set(["│", "╭", "╮", "┬", "┼", "●"]); // ink touches its row's bottom edge


function graphSVG(row, feedIdx) {
  const cells = runes(row.cells || "");
  const prev = runes((state.rows[feedIdx - 1] || {}).cells || "");
  const next = runes((state.rows[feedIdx + 1] || {}).cells || "");
  const w = cells.length * CELL_W;
  let parts = `<svg width="${w}" height="${ROW_H}" viewBox="0 0 ${w} ${ROW_H}">`;
  cells.forEach((ch, col) => {
    const x = col * CELL_W;
    const color = laneColor(col >> 1);
    if (ch === "●") {
      if (BOT_TOUCH.has(prev[col]))
        parts += `<path d="M${x + HALF},0 V${MID}" stroke="${color}" stroke-width="2" fill="none"/>`;
      if (TOP_TOUCH.has(next[col]))
        parts += `<path d="M${x + HALF},${MID} V${ROW_H}" stroke="${color}" stroke-width="2" fill="none"/>`;
      parts += `<circle cx="${x + HALF}" cy="${MID}" r="4" fill="${dotColor(row, col >> 1)}"/>`;
    } else if (GLYPH_PATHS[ch]) {
      parts += `<path d="${GLYPH_PATHS[ch](x)}" stroke="${color}" stroke-width="2" fill="none" stroke-linecap="round"/>`;
    } else if (ch !== " ") {
      parts += `<text x="${x}" y="${ROW_H - 6}" fill="${color}" font-size="12">${esc(ch)}</text>`;
    }
  });
  return parts + "</svg>";
}


// feedQuery builds the commits URL: the paging verb plus the active feed
// filter. The filter travels on EVERY request rather than being remembered
// server-side — one feed serves every tab, so a stored filter would show a
// second tab a narrowed list with no bar to clear it.
function feedQuery(mode) {
  const p = new URLSearchParams();
  if (mode) p.set(mode, "1");
  const f = state.feedFilter || {};
  for (const k of ["path", "author", "grep", "since", "until"]) if (f[k]) p.set(k, f[k]);
  const q = p.toString();
  return "/api/commits" + (q ? "?" + q : "");
}


// more: page deeper. reset: start the list clean (a MANUAL refresh — the
// reconciling reload is right after an op, and useless when the deep tail has
// gone stale because history was rewritten).
async function loadCommits(more, reset) {
  const gen = ++state.feedGen;
  const body = await getJSON(feedQuery(more ? "more" : reset ? "reset" : ""));
  // A page that lands after a newer request went out is stale — the filter
  // moved on while it was in flight — and showing it would put rows on screen
  // the current query does not select. The server drops stale pages the same
  // way, by feed generation; this is that rule's client half.
  if (gen !== state.feedGen) return;
  state.rows = body.rows || [];
  state.canLoadMore = body.can_load_more;
  // A filtered page is a subset of history: the server computes no lanes for
  // it, and the rows render flat.
  state.feedFiltered = !!body.filtered;
  // The scope is server state (one feed for every tab), so it is reported by
  // the very response it scopes rather than tracked client-side. A reload or
  // a second tab therefore shows the chip without asking for it.
  setSoloChip(body.solo || "", body.solo_kind || "");
  if (state.cfilter) applyCommitFilter(); // recompute over the grown/reloaded feed (ends in renderCommits)
  else renderCommits();
}


// refilterFeed re-walks the feed under the filter search.js just changed,
// keeping the cursor on the commit it was on when that commit survives the new
// scope.
//
// The anchor's HASH is read BEFORE the reload, never its index afterwards: the
// working-tree row shifts every display index, so an index read after the fact
// anchors to a different commit — that bug shipped here once already.
async function refilterFeed() {
  const anchor = state.rows[state.cursor - wtCount()];
  const hash = anchor ? anchor.hash : "";
  await loadCommits(false);
  const i = hash ? state.rows.findIndex((r) => r.hash === hash) : -1;
  state.cursor = i >= 0 ? i + wtCount() : wtCount();
  moveCursor(0);
}


// --- solo mode ---
// Narrowing the commit list to one branch is a mode you can get stuck in, so
// the chip is not decoration: it is the exit. It renders from state.solo,
// which survives a failing /api/commits, and clicking it clears the scope.
function setSoloChip(ref, kind) {
  state.solo = ref;
  state.soloKind = ref ? kind || "branch" : "";
  const el = $("solo-chip");
  el.classList.toggle("hidden", !ref);
  if (ref) el.textContent = "solo: " + soloLabel(ref, state.soloKind) + " ✕";
}


// A commit scope is stored as the full 40-hex sha (no short-sha ambiguity in
// the walk) and shown short — a chip is a label, not an identifier.
function soloLabel(ref, kind) {
  return kind === "commit" ? ref.slice(0, 8) : ref;
}


// setSolo scopes the commit list to a branch/tag (the default) or to the
// history reachable from a commit. The server resolves the ref and answers
// with what it stored, so the chip always names the scope actually applied.
async function setSolo(ref, kind) {
  if (opBusy()) return;
  try {
    const got = await postJSON("/api/solo", { kind: ref ? kind || "branch" : "", ref });
    setSoloChip(got.solo || "", got.solo_kind || "");
    await loadCommits(false);
    moveCursor(0);
    opLine(ref ? "commit list scoped to " + soloLabel(ref, kind || "branch") : "showing every branch");
  } catch (e) {
    opLine("error: " + (e.message || e), true);
  }
}

$("solo-chip").addEventListener("click", () => setSolo(""));


function maybeLoadMore(lastVisible) {
  if (!state.canLoadMore || state.loadingMore) return;
  if (lastVisible < state.rows.length - 30) return;
  state.loadingMore = true;
  loadCommits(true).finally(() => {
    state.loadingMore = false;
  });
}


async function openCommit(i) {
  if (state.wt && i === 0) return openWorkingTree(i);
  state.cursor = i;
  renderCommits();
  const row = state.rows[i - wtCount()];
  const gen = ++state.detailGen;
  const body = await getJSON("/api/commit/" + row.hash);
  if (gen !== state.detailGen) return; // superseded by a newer open or esc
  state.files = body.files || [];
  state.fileCursor = 0;
  state.fileSha = row.hash;
  state.filesMode = "commit";
  enterFilesStage();
  $("files-title").textContent = row.short + " " + row.subject;
  setFilesMeta(commitMetaLine(body));
  renderFiles();
  focusPane();
}


// openCommitByHash enters commit detail without a feed row — the path for
// sidebar tags (and future non-feed jump-ins).
async function openCommitByHash(hash, title) {
  const gen = ++state.detailGen;
  const body = await getJSON("/api/commit/" + hash);
  if (gen !== state.detailGen) return; // superseded by a newer open or esc
  state.files = body.files || [];
  state.fileCursor = 0;
  state.fileSha = hash;
  state.filesMode = "commit";
  enterFilesStage();
  $("files-title").textContent = title;
  setFilesMeta(commitMetaLine(body));
  renderFiles();
  focusPane();
}


// --- goto commit (#) --------------------------------------------------------
// Reveal-first: the point is the commit IN ITS PLACE in history. Paging stops
// the moment a page adds nothing (feed exhausted — e.g. a solo scope that
// excludes the commit), then falls back to opening the detail directly so the
// user always lands on the commit.
function gotoCommitPrompt() {
  openPrompt({
    title: "Goto commit — sha, branch, tag, or any rev",
    placeholder: "e.g. a1b2c3d or main~3",
    onSubmit: (rev) => gotoCommit(rev),
  });
}


async function gotoCommit(rev) {
  let res;
  try {
    res = await getJSON("/api/resolve?rev=" + encodeURIComponent(rev));
  } catch (e) {
    opLine("cannot resolve " + rev + ": " + (e.message || e), true);
    return;
  }
  const gen = ++state.gotoGen;
  let guard = 0;
  // goto is explicit (the user typed a rev they expect to exist), so the
  // bound is generous — 100 pages vs. the branch-tip jump's 20 — but still
  // finite: an all-branches feed on a huge repo must not page to exhaustion.
  while (guard < 100) {
    const idx = state.rows.findIndex((r) => r.hash === res.hash);
    if (idx >= 0) return revealCommit(idx);
    if (!state.canLoadMore) break;
    const before = state.rows.length;
    await loadCommits(true);
    if (gen !== state.gotoGen) return; // superseded: re-root or a second goto
    if (state.rows.length === before) break; // no growth: feed exhausted
    guard++;
  }
  opLine("commit is not in the current list (scope?) — opening its detail", false);
  openCommitByHash(res.hash, res.hash.slice(0, 8) + " " + (res.subject || ""));
}


function revealCommit(feedIdx) {
  closeCommitFilter(); // reveal happens in the FULL list
  // goto is a jump command: land the user ON the row, not behind whatever
  // stage they were drilled into (diff/files would otherwise hide the
  // commits pane entirely — display:none — so the scroll+flash below would
  // target an invisible pane). Bounded at 2 steps: diff -> files -> list.
  while (state.layout !== "list") drillOut();
  const i = feedIdx + wtCount();
  state.cursor = i;
  const scroll = $("commits-scroll");
  scroll.scrollTop = Math.max(0, i * ROW_H + wtExtra() - scroll.clientHeight / 2);
  state.flashHash = state.rows[feedIdx].hash;
  renderCommits();
  setTimeout(() => {
    state.flashHash = "";
    renderCommits();
  }, 1700);
}


// openStashDetail opens a stash's changes: the stash commit's tracked
// first-parent diff plus, when present, its untracked-files parent
// (stash^3 — a root commit whose file list shows every untracked file as
// added). Untracked rows carry a per-file sha so their diffs read from
// that parent; a failed untracked fetch degrades to the tracked list.
async function openStashDetail(st) {
  const gen = ++state.detailGen;
  const body = await getJSON("/api/commit/" + st.sha);
  if (gen !== state.detailGen) return; // superseded by a newer open or esc
  let files = body.files || [];
  if (st.untracked_sha) {
    const u = await getJSON("/api/commit/" + st.untracked_sha).catch(() => ({ files: [] }));
    if (gen !== state.detailGen) return;
    files = files.concat((u.files || []).map((f) => ({ ...f, sha: st.untracked_sha })));
  }
  state.files = files;
  state.fileCursor = 0;
  state.fileSha = st.sha;
  state.filesMode = "commit";
  enterFilesStage();
  $("files-title").textContent = "≡ " + st.ref;
  renderFiles();
  focusPane();
}


$("commits-scroll").addEventListener("scroll", renderCommits);

$("commits-window").addEventListener("click", async (e) => {
  if (e.target.id === "cfilter-more") {
    await loadCommits(true); // appends server-side; loadCommits re-filters
    return;
  }
  if (e.target.id === "cfilter-deeper") {
    searchDeeper();
    return;
  }
  const row = e.target.closest(".crow");
  if (!row || row.dataset.i === undefined) return;
  const i = Number(row.dataset.i);
  // ctrl/cmd+click MARKS instead of opening — the pair a compare needs, the
  // run a squash needs. The working-tree row is not a commit.
  if ((e.ctrlKey || e.metaKey) && !(state.wt && i === 0)) {
    toggleCommitMark(state.rows[i - wtCount()]);
    return;
  }
  openCommit(i);
});


// --- ◉ marked commits -------------------------------------------------------
// Marks are commit HASHES, so they survive a re-render, a paging load and a
// refilter; a mark on a commit the current scope excludes simply stops being
// listed by markedInFeedOrder (and "unmark all" clears the whole set, visible
// or not).

function toggleCommitMark(c) {
  if (!c) return;
  if (state.cmarks.has(c.hash)) state.cmarks.delete(c.hash);
  else state.cmarks.add(c.hash);
  renderCommits();
}


// markedInFeedOrder returns the marked hashes NEWEST FIRST, dropping marks
// that are not in the loaded feed. Feed order is what both consumers need:
// compare wants older→newer, and squash's range starts at the oldest.
function markedInFeedOrder() {
  return state.rows.filter((r) => state.cmarks.has(r.hash)).map((r) => r.hash);
}


// compareMarked diffs the two marked commits — tree to tree, no ancestry
// needed (the compare view's exact 2-commit semantic). The hashes go over the
// wire as revs so no branch name has to resolve.
function compareMarked() {
  const m = markedInFeedOrder();
  if (m.length !== 2) {
    opLine("mark exactly 2 commits to compare", true);
    return;
  }
  const [newer, older] = m;
  openCompare(older, newer, { revs: 1, aLabel: older.slice(0, 8), bLabel: newer.slice(0, 8) });
}


// squashMarked folds the marked commits into one. The client sends only the
// hashes: the server reads the real range and builds the rebase plan from it,
// refusing (with the branch untouched) a selection that is not on this branch
// or not adjacent.
function squashMarked() {
  const m = markedInFeedOrder();
  if (m.length < 2) {
    opLine("mark at least 2 commits to squash", true);
    return;
  }
  showLocalConfirm(
    "Squash " + m.length + " marked commits into one? The branch is rewritten from the oldest of them up.",
    ["squash", "abort"],
    async (o) => {
      if (o !== "squash" || opBusy()) return;
      let resp;
      try {
        resp = await postJSON("/api/commit-squash", { shas: m });
      } catch (e) {
        opLine("error: " + (e.message || e), true);
        return;
      }
      state.cmarks.clear();
      followOp(resp.op_id, "squashing " + m.length + " commits", "commit-squash", null);
    }
  );
}

$("cfilter-input").addEventListener("input", applyCommitFilter);

// Escape must be handled HERE: the global router's form-field guard eats
// every key typed in an input, so it can never see this one. Ctrl+Enter and
// ctrl+f both start the deep search from the field you typed the query in —
// the point of the query is rarely a commit that is already loaded.
$("cfilter-input").addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    e.preventDefault();
    closeCommitFilter();
  } else if ((e.key === "Enter" || e.key === "f") && (e.ctrlKey || e.metaKey)) {
    e.preventDefault();
    searchDeeper();
  }
});

// Right-click a commit: copy rows plus the single-commit history edits. The
// row is selected first (the files-list menu's rule) so the menu is visibly
// about the line under the pointer.
$("commits-window").addEventListener("contextmenu", (e) => {
  const rowEl = e.target.closest(".crow");
  if (!rowEl) return;
  const i = Number(rowEl.dataset.i);
  if (state.wt && i === 0) return; // the working-tree row is not a commit
  const c = state.rows[i - wtCount()];
  if (!c) return;
  // preventDefault only once there is a menu to show in its place: doing it
  // above would leave the working-tree row with neither menu — a right-click
  // that does nothing at all.
  e.preventDefault();
  state.cursor = i;
  renderCommits();
  showCommitMenu(c, i, e.clientX, e.clientY);
});


// feedDescendant reports whether selHash is a descendant of tipHash, walking
// parent links across the LOADED rows only — the port of the TUI's
// fast_forward_gate.go. `conclusive` is false when the walk runs off the
// loaded window or the tip is not loaded: an unknown answer must offer the
// row, not hide it, because only git can settle it.
function feedDescendant(rows, selHash, tipHash) {
  if (selHash === tipHash) return { descendant: false, conclusive: true }; // not ahead of itself
  const byHash = new Map(rows.map((r) => [r.hash, r]));
  if (!byHash.has(tipHash) || !byHash.has(selHash)) return { descendant: false, conclusive: false };
  const seen = new Set();
  const stack = [selHash];
  let conclusive = true;
  while (stack.length) {
    const h = stack.pop();
    if (h === tipHash) return { descendant: true, conclusive: true };
    if (seen.has(h)) continue;
    seen.add(h);
    for (const p of byHash.get(h).parent_ids || []) {
      if (byHash.has(p)) stack.push(p);
      else conclusive = false; // ran off the loaded window
    }
  }
  return { descendant: false, conclusive };
}


// showCommitMenu offers the per-commit actions. The history edits are gated
// on a single parent: a merge (2+) and the root (0) are not what "move" and
// "drop" mean here, and the engine refuses a range containing a merge anyway
// — the TUI's commitEditRow applies the same gate.
function showCommitMenu(c, i, x, y) {
  const short = c.short || c.hash.slice(0, 8);
  const items = [
    { label: "show this commit", act: () => openCommit(i) },
    { label: "copy commit id", act: () => copyText(c.hash, "commit id " + short) },
    { label: "copy subject", act: () => copyText(c.subject, "subject") },
    {
      // ONE dialog with both fields on screen, the TUI's tagPopup: a name, a
      // message under it, and the rule stated where you can read it — an empty
      // message makes a lightweight tag, a filled one makes it annotated. The
      // message starts EMPTY: prefilling it would make "annotated" the silent
      // default. Name validation is git's own (check-ref-format server-side).
      label: "create tag here…",
      act: () =>
        openPrompt({
          title: "New tag at " + short + ":",
          placeholder: "tag name",
          body: {
            label: "message — leave empty for a lightweight tag",
            value: "",
            placeholder: "annotation (optional)",
          },
          onSubmit: (name, message) =>
            startOp({ op: "create-tag", tag: name, sha: c.hash, message }, "tagging " + short + " as " + name),
        }),
    },
    { sep: true },
    {
      // Branch off this commit — the same dialog the branch menu and the ☰
      // menu use, prefix lane included; the start point is the sha.
      label: "create branch here…",
      act: () => openCreateBranchPrompt(c.hash, undefined, short),
    },
    {
      // A worktree cut at this commit, on a new branch created there — the
      // TUI's "create worktree here". Two prompts: the branch name, then
      // where it goes (prefilled the way the branch menu's row is).
      label: "create worktree here…",
      act: () =>
        openPrompt({
          title: "New branch for the worktree at " + short + ":",
          placeholder: "branch name",
          onSubmit: (name) =>
            openPrompt({
              title: "New worktree for " + name + ", at path:",
              value: defaultWorktreePath(name),
              onSubmit: (path) =>
                startOp({ op: "create-worktree", sha: c.hash, name, path }, "creating worktree " + path),
            }),
        }),
    },
  ];
  // Advance the current branch to this commit (ff-only). The row is HIDDEN
  // when the commit is conclusively not ahead of the branch tip — the TUI's
  // commitFastForwardRow gate, walked over the loaded feed the same way
  // (feedDescendant): a row that can only fail is noise, and "inconclusive"
  // (tip not loaded, or the walk ran off the loaded window) still offers it
  // and lets the engine be the judge.
  const cur = (state.branches || []).find((b) => b.is_head);
  if (cur && cur.name && !(cur.hash && c.hash.startsWith(cur.hash))) {
    const tip = state.rows.find((r) => cur.hash && r.hash.startsWith(cur.hash));
    const gate = tip ? feedDescendant(state.rows, c.hash, tip.hash) : { conclusive: false };
    if (!(gate.conclusive && !gate.descendant)) {
      items.push({
        label: "fast-forward " + cur.name + " to here",
        // The TUI confirms this one too (confirmOp, default No).
        act: () =>
          showLocalConfirm("Fast-forward to this commit?", ["Yes", "No"], (o) => {
            if (o === "Yes") startOp({ op: "fast-forward", sha: c.hash }, "fast-forwarding " + cur.name + " to " + short);
          }),
      });
    }
  }
  items.push({ sep: true });
  // gg's own stores: a bookmark is a LIVE reference to this commit, a shelf
  // entry freezes its changed files so they outlive a gc or a rewrite. Both
  // ask for a name, prefilled with the subject (the TUI's popup).
  items.push({ label: "bookmark this commit…", act: () => addCommitEntry("bookmarks", c.hash, c.subject) });
  items.push({ label: "shelf this commit…", act: () => addCommitEntry("shelf", c.hash, c.subject) });
  items.push({ sep: true });
  // Review just this commit's own change (sha^..sha, resolved server-side).
  // Offered unconditionally: whether a review tool is configured is the
  // review lane's own answer, and it says so plainly.
  items.push({ label: "review this commit (AI)…", act: () => startReview("commit", "", c.hash) });
  items.push({ sep: true });
  // Apply this commit's change to the current branch, or undo it there. Both
  // run git's sequencer, so a conflict parks the engine's keep/abort decision
  // in the modal; the local confirm here is about STARTING one from a menu
  // click, which the TUI also asks about.
  items.push({
    label: "cherry-pick onto " + (cur ? cur.name : "current branch"),
    act: () =>
      showLocalConfirm(
        "Cherry-pick " + short + " " + c.subject + " onto " + (cur ? cur.name : "the current branch") + "?",
        ["cherry-pick", "abort"],
        (o) => { if (o === "cherry-pick") startOp({ op: "cherry-pick", sha: c.hash }, "cherry-picking " + short); }
      ),
  });
  if (c.parents === 1) {
    items.push({
      label: "revert this commit",
      act: () =>
        showLocalConfirm(
          "Revert " + short + " " + c.subject + "? A new commit undoing it is added on top.",
          ["revert", "abort"],
          (o) => { if (o === "revert") startOp({ op: "revert", sha: c.hash }, "reverting " + short); }
        ),
    });
    // Reword prefills with the commit's CURRENT full message — a body is lost
    // the moment someone has to retype it — so the row reads it first and only
    // opens the (multiline) prompt once it has it.
    items.push({
      label: "reword this commit…",
      act: async () => {
        const got = await getJSON("/api/commit-message?rev=" + encodeURIComponent(c.hash)).catch(() => null);
        if (!got) {
          opLine("could not read the commit message", true);
          return;
        }
        openPrompt({
          title: "Reword " + short + ":",
          value: got.message || "",
          multiline: true,
          onSubmit: (message) => startOp({ op: "reword", sha: c.hash, message }, "rewording " + short),
        });
      },
    });
  }
  if (c.parents === 1) {
    items.push({ label: "move up (newer)", act: () => commitEdit(c, "move-up") });
    items.push({ label: "move down (older)", act: () => commitEdit(c, "move-down") });
    items.push({ sep: true });
    items.push({
      label: "drop this commit",
      danger: true,
      act: () =>
        showLocalConfirm(
          "Drop " + short + " " + c.subject + "? The branch is rewritten from here up.",
          ["drop", "abort"],
          (o) => { if (o === "drop") commitEdit(c, "drop"); }
        ),
    });
  }
  if (cur && cur.name) {
    // The reflog menu's reset, reached from the commit you are looking at:
    // an empty mode means the engine's own soft/mixed/hard picker parks in
    // the modal (with cancel, plus the non-ancestor confirm), so that modal
    // IS the confirmation and there is no local one here.
    items.push({ sep: true });
    items.push({
      label: "reset " + cur.name + " to here…",
      danger: true,
      // The TUI asks BEFORE the engine's picker — moving a branch ref is
      // worth one deliberate yes, and the picker that follows is about how
      // far the working tree comes along, not about whether to do it.
      act: () =>
        showLocalConfirm("Reset to " + short + "? This moves the current branch ref.", ["Yes", "No"], (o) => {
          if (o === "Yes") startOp({ op: "reset", sha: c.hash }, "resetting to " + short);
        }),
    });
  }
  // The multi-commit lane: mark rows, then what a set of marks is for. Both
  // consumers read the marks fresh when they run, so a mark toggled between
  // opening this menu and clicking a row still counts.
  items.push({ sep: true });
  items.push({
    label: state.cmarks.has(c.hash) ? "unmark this commit" : "mark this commit (ctrl+click)",
    act: () => toggleCommitMark(c),
  });
  const marked = markedInFeedOrder();
  if (marked.length === 2) {
    items.push({ label: "compare the 2 marked commits", act: () => compareMarked() });
  }
  if (marked.length >= 2) {
    items.push({ label: "squash " + marked.length + " marked commits…", act: () => squashMarked() });
  }
  if (state.cmarks.size) {
    items.push({
      label: "unmark all (" + state.cmarks.size + ")",
      act: () => {
        state.cmarks.clear();
        renderCommits();
      },
    });
  }
  // Scope the list to this commit's ancestry — the commit-anchored twin of
  // solo-this-branch. The chip clears it, like every other solo.
  items.push({ sep: true });
  items.push(
    state.solo === c.hash
      ? { label: "exit solo (show every branch)", act: () => setSolo("") }
      : { label: "solo from this commit", act: () => setSolo(c.hash, "commit") }
  );
  // Rows contributed by feature modules (menus.js), after the built-ins.
  items.push(...extraRows("commit", c));
  showCtxMenu(items, x, y);
}


// commitEdit rewrites the checked-out branch so that one commit is dropped or
// swapped with its neighbour. The server builds the rebase plan itself from
// the commit id and this verb — nothing plan-shaped goes on the wire — and
// refuses (leaving the branch untouched) when the commit is not on the
// checked-out branch, or has no neighbour in that direction.
function commitEdit(c, edit) {
  const short = c.short || c.hash.slice(0, 8);
  const what = edit === "drop" ? "dropping " : edit === "move-up" ? "moving up " : "moving down ";
  startOp({ op: "commit-edit", sha: c.hash, edit }, what + short);
}
export { applyGraphMode, BOT_TOUCH, CELL_W, EAGER_PAGES, GLYPH_PATHS, HALF, MID, TOP_TOUCH, applyCommitFilter, closeCommitFilter, commitEdit, commitMatches, compareMarked, feedQuery, firstFeedMatch, flatDotSVG, gotoBranchTip, gotoCommit, gotoCommitPrompt, graphHTML, graphSVG, landOnFeedIdx, laneColor, laneColors, loadCommits, markedInFeedOrder, maybeLoadMore, openCommit, openCommitByHash, openCommitFilter, openStashDetail, refilterFeed, renderCommits, renderFilteredCommits, revealCommit, rowHTML, searchDeeper, setSolo, setSoloChip, showCommitMenu, soloLabel, squashMarked, toggleCommitMark, toggleGraphMode };
