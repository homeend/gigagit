// remoteheads.js — "check out remote branch…": type or paste any branch name
// that exists on a remote and check it out.
//
// A picker over GET /api/remote-heads: EVERY branch on the remote (one
// ls-remote) — the ones a narrowed/single-branch monorepo fetch refspec hides
// from the Remotes section AND the ones already tracked, flagged "tracked".
// Listing only the hidden ones left a full-refspec repo with an empty picker
// ("every branch is already tracked") that no pasted name could match. Picking
// a row offers checkout (stay) or checkout-and-switch; either starts the
// checkout-remote-head op, which decides server-side: a tracked branch checks
// out from its remote-tracking ref, an unfetched one gets a per-branch fetch
// mapping and is fetched first.
//
// A pasted "origin/feat/x", "refs/remotes/origin/feat/x" or
// "refs/heads/feat/x" matches feat/x (normaliseQuery). At the remote chooser
// (several remotes) such a paste narrows to its remote and carries feat/x into
// that remote's branch list; text naming no remote ("feat/x") keeps every
// remote listed and is carried into whichever one is picked.
//
// Two phases in one overlay, the TUI popup's shape: a remote chooser first
// when the repo has several remotes, straight to the branch list when there
// is just one.
import { $, esc, getJSON } from "./core.js";
import { POPUP_PAGE, closeLayer, mountOverlay, pushLayer, showCtxMenu } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { opLine, startOp } from "./ops.js";

// z-index 25: above the palette (22) it is opened from, BELOW #ctx-menu (40)
// — a row's checkout menu opens over the picker; at 60 the picker covered it
// and no checkout could be clicked.
//
// This module builds its own DOM: index.html's `hidden` class has NO global
// rule — the overlay must ship its own `#rheads.hidden` selector or it is
// plainly visible on load.
const CSS = `
#rheads { position: fixed; inset: 0; background: rgba(0,0,0,0.45); display: flex; align-items: flex-start; justify-content: center; z-index: 25; }
#rheads.hidden { display: none; }
#rheads-box { margin-top: 8vh; width: min(640px, 92vw); background: var(--bg-alt); border: 1px solid var(--border); border-radius: 6px; box-shadow: 0 8px 30px rgba(0,0,0,0.5); overflow: hidden; }
#rheads-input { width: 100%; box-sizing: border-box; background: var(--bg); color: var(--fg); border: none; border-bottom: 1px solid var(--border); padding: 8px 10px; font: inherit; }
#rheads-input:focus { outline: none; }
#rheads-list { list-style: none; margin: 0; padding: 0; max-height: 50vh; overflow-y: auto; }
#rheads-list li { padding: 3px 10px; cursor: pointer; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
#rheads-list li.sel { background: var(--sel); }
#rheads-list li.empty { color: var(--dim); cursor: default; }
#rheads-list li .rh-tracked { color: var(--dim); margin-left: 8px; font-size: 11px; }
#rheads-note { color: var(--dim); font-size: 11px; padding: 4px 10px; border-top: 1px solid var(--border); }
`;

const styleEl = document.createElement("style");
styleEl.textContent = CSS;
document.head.append(styleEl);

const rheads = mountOverlay("rheads");
rheads.innerHTML =
  `<div id="rheads-box">` +
  `<input id="rheads-input" type="text" autocomplete="off" spellcheck="false" placeholder="type or paste a branch name…">` +
  `<ul id="rheads-list"></ul>` +
  `<div id="rheads-note"></div></div>`;

// rh holds the open picker: the phase ("remotes" chooser or "heads" list),
// the configured remote names, the loaded rows ({name, tracked}), the
// filtered view, and the cursor. gen guards a slow
// ls-remote answer against a picker that was closed and reopened meanwhile.
let rh = null;

let rhGen = 0;

// openRemoteHeads opens the picker; initial is text to search for (the
// Remotes section's "look for … on the remote" row hands its search over).
function openRemoteHeads(initial = "") {
  if (rh) return; // already open
  rh = { phase: "remotes", remote: "", remotes: [], rows: [], filtered: [], sel: 0 };
  $("rheads-input").value = typeof initial === "string" ? initial : "";
  $("rheads-note").textContent = "";
  $("rheads-list").innerHTML = `<li class="empty">loading…</li>`;
  pushLayer("rheads", rheads, { onKey: rheadsKey });
  $("rheads-input").focus();
  loadRemotes();
}

function closeRemoteHeads() {
  rh = null;
  rhGen++; // any in-flight answer is stale now
  $("rheads-input").blur(); // a focused input would swallow every global key
  closeLayer("rheads");
}

async function loadRemotes() {
  const gen = ++rhGen;
  let body;
  try {
    body = await getJSON("/api/remote-heads");
  } catch (e) {
    if (rh && gen === rhGen) $("rheads-list").innerHTML = `<li class="empty">error: ${esc(e.message || e)}</li>`;
    return;
  }
  if (!rh || gen !== rhGen) return; // closed, or reopened meanwhile
  const names = body.remotes || [];
  rh.remotes = names;
  if (!names.length) {
    closeRemoteHeads();
    opLine("check out remote branch: no remotes configured", true);
    return;
  }
  if (names.length === 1) {
    loadHeads(names[0]);
    return;
  }
  rh.phase = "remotes";
  rh.rows = names.map((name) => ({ name, tracked: false }));
  refilter(); // also writes the note
}

async function loadHeads(remote) {
  const gen = ++rhGen;
  // A query typed to pick the remote is spent — unless it named a branch
  // ("origin/feat/x", or text that matched no remote): that, normalised,
  // filters the list about to load. Typing during the load is kept as it is.
  const typed = $("rheads-input").value.trim();
  // With one remote there was no chooser: whatever was typed while the
  // remote names loaded is a branch name too.
  const keep = rh.phase === "remotes" && (rh.carry || rh.remotes.length === 1);
  const carry = keep ? normaliseQuery(typed, rh.remotes) : "";
  rh.phase = "heads";
  rh.remote = remote;
  rh.rows = [];
  rh.sel = 0;
  $("rheads-input").value = carry;
  $("rheads-list").innerHTML = `<li class="empty">loading… (asking ${esc(remote)})</li>`;
  $("rheads-note").textContent = "";
  let body;
  try {
    body = await getJSON("/api/remote-heads?remote=" + encodeURIComponent(remote));
  } catch (e) {
    if (rh && gen === rhGen) $("rheads-list").innerHTML = `<li class="empty">error: ${esc(e.message || e)}</li>`;
    return;
  }
  if (!rh || gen !== rhGen) return;
  rh.rows = (body.heads || []).map((h) => ({ name: h.name, tracked: !!h.tracked }));
  refilter();
  $("rheads-note").textContent = headsNote(rh.rows, remote, body.error);
}

// headsNote is the picker's footer line for a loaded branch list. err is the
// server's ls-remote failure: the list then holds only the tracked branches.
function headsNote(rows, remote, err) {
  const fresh = rows.filter((r) => !r.tracked).length;
  if (err) return "could not reach " + remote + " (" + err + ") — showing its " + rows.length + " tracked branch(es)";
  if (!rows.length) return "no branches on " + remote;
  return (
    rows.length + " branch(es) on " + remote + (fresh ? " (" + fresh + " not fetched yet)" : "") +
    " — type or paste a name, enter/click to check out"
  );
}

// normaliseQuery turns what a user pastes into a branch-name filter: the
// refs/heads/ and refs/remotes/ prefixes go, and so does a leading
// "<remote>/" for any configured remote (a branch whose own name starts with
// a remote's name still matches: the raw query is tried too, see matchRank).
function normaliseQuery(q, remotes) {
  let s = q.trim().replace(/^refs\/heads\//, "").replace(/^refs\/remotes\//, "");
  for (const r of remotes || []) {
    if (s.startsWith(r + "/")) return s.slice(r.length + 1);
  }
  return s;
}

// matchRank orders a row for query q (lower-cased, raw and normalised): an
// exact name first — enter on a pasted name takes it — then a prefix, then a
// substring; -1 = no match.
function matchRank(name, qs) {
  const n = name.toLowerCase();
  let best = -1;
  for (const q of qs) {
    if (!q) return 2;
    const r = n === q ? 0 : n.startsWith(q) ? 1 : n.includes(q) ? 2 : -1;
    if (r >= 0 && (best < 0 || r < best)) best = r;
  }
  return best;
}

// refilter rebuilds the visible rows from the query (case-insensitive
// substring — branch names, not fuzzy prose; exact and prefix hits first, so
// a pasted full name is the row enter takes) and clamps the cursor. At the
// remote chooser a pasted "origin/feat/x" keeps origin.
function refilter() {
  const raw = $("rheads-input").value.trim().toLowerCase();
  let qs;
  if (rh.phase === "remotes") {
    // Remote names first; a ref-shaped paste narrows to its remote; text
    // that names no remote is a branch to look for, so every remote stays.
    const bare = raw.replace(/^refs\/remotes\//, "");
    const owner = rh.rows.find((r) => bare.startsWith(r.name.toLowerCase() + "/"));
    const named = owner || rh.rows.some((r) => r.name.toLowerCase().includes(raw));
    rh.carry = !!raw && (!!owner || !named);
    qs = [owner ? owner.name.toLowerCase() : named ? raw : ""];
    $("rheads-note").textContent = rh.carry && !owner ? "pick a remote to look for “" + raw + "” there" : "pick a remote";
  } else {
    qs = [...new Set([raw, normaliseQuery(raw, (rh.remotes || []).map((r) => r.toLowerCase()))])];
  }
  rh.filtered = rh.rows
    .map((row, i) => ({ row, i, rank: matchRank(row.name, qs) }))
    .filter((x) => x.rank >= 0)
    .sort((a, b) => a.rank - b.rank || a.i - b.i)
    .map((x) => x.row);
  rh.sel = Math.max(0, Math.min(rh.sel, rh.filtered.length - 1));
  render();
}

// RENDER_CAP bounds the rows painted per keystroke: a monorepo remote lists
// thousands of branches, and typing narrows them anyway.
const RENDER_CAP = 300;

function render() {
  if (!rh.filtered.length) {
    $("rheads-list").innerHTML = `<li class="empty">(no match)</li>`;
    return;
  }
  // The cursor can page past the cap: the painted window slides with it.
  const start = Math.max(0, Math.min(rh.sel - RENDER_CAP + 1, rh.filtered.length - RENDER_CAP));
  const end = Math.min(rh.filtered.length, start + RENDER_CAP);
  let html = "";
  for (let i = start; i < end; i++) {
    const r = rh.filtered[i];
    const tag = r.tracked ? `<span class="rh-tracked">tracked</span>` : "";
    html += `<li data-i="${i}"${i === rh.sel ? ' class="sel"' : ""}>${esc(r.name)}${tag}</li>`;
  }
  const rest = rh.filtered.length - end;
  if (rest > 0) html += `<li class="empty">… ${rest} more — type to narrow</li>`;
  $("rheads-list").innerHTML = html;
  const sel = $("rheads-list").querySelector("li.sel");
  if (sel) sel.scrollIntoView({ block: "nearest" });
}

function moveSel(d) {
  if (!rh.filtered.length) return;
  rh.sel = Math.max(0, Math.min(rh.filtered.length - 1, rh.sel + d));
  render();
}

// pickRow acts on the selected row: a remote drills into its branch list; a
// branch opens the checkout menu anchored at its row (or at the given click
// position).
function pickRow(i, x, y) {
  const row = rh.filtered[i];
  if (!row) return;
  const name = row.name;
  if (rh.phase === "remotes") {
    loadHeads(name);
    return;
  }
  const remote = rh.remote;
  if (x == null) {
    const li = $("rheads-list").querySelector(`li[data-i="${i}"]`);
    const r = li ? li.getBoundingClientRect() : { left: 40, bottom: 40 };
    x = r.left + 20;
    y = r.bottom;
  }
  showCtxMenu(
    [
      { header: remote + "/" + name },
      // The picker closes FIRST (the TUI pops its popup the same way) so the
      // op line and any parked decision modal render over the base UI.
      {
        label: "check out " + name + " (stay here)",
        act: () => {
          closeRemoteHeads();
          startOp({ op: "checkout-remote-head", remote, branch: name }, "checking out " + remote + "/" + name);
        },
      },
      {
        label: "check out " + name + " and switch to it",
        act: () => {
          closeRemoteHeads();
          startOp({ op: "checkout-remote-head", remote, branch: name, switch: true }, "switching to " + remote + "/" + name);
        },
      },
    ],
    x,
    y,
  );
}

function rheadsKey(e) {
  if (e.key === "Escape") {
    closeRemoteHeads();
    return true;
  }
  if (e.key === "ArrowDown" || (e.key === "n" && e.ctrlKey)) {
    e.preventDefault();
    moveSel(1);
    return true;
  }
  if (e.key === "ArrowUp" || (e.key === "p" && e.ctrlKey)) {
    e.preventDefault();
    moveSel(-1);
    return true;
  }
  if (e.key === "PageDown" || e.key === "PageUp") {
    e.preventDefault();
    moveSel(e.key === "PageDown" ? POPUP_PAGE : -POPUP_PAGE);
    return true;
  }
  if (e.key === "Enter") {
    e.preventDefault();
    pickRow(rh.sel);
    return true;
  }
  return false; // everything else is typing
}

$("rheads-input").addEventListener("input", () => {
  if (rh) refilter();
});

$("rheads-list").addEventListener("click", (e) => {
  const li = e.target.closest("li[data-i]");
  if (!li) return;
  // stopPropagation: a branch row opens the checkout ctx menu, and the
  // document-level outside-click closer would otherwise fire on this same
  // click and close it as it opens (the palette ☰ / versions ⋯ convention).
  e.stopPropagation();
  pickRow(Number(li.dataset.i), e.clientX, e.clientY);
});

rheads.addEventListener("click", (e) => {
  if (e.target === rheads) closeRemoteHeads(); // a click on the dim closes it
});

// Also on every remote row's context menu, so the feature is discoverable
// from the section it extends.
registerRows("remote", () => [{ label: "check out remote branch…", act: () => openRemoteHeads() }]);

registerHelp({
  key: "check out remote",
  html:
    "ctrl+p / ☰ → <b>check out remote branch…</b>: type or paste any branch name on a remote " +
    "(<code>feat/x</code>, <code>origin/feat/x</code> or a full ref) and check it out — staying " +
    "put or switching. One <code>ls-remote</code> lists every branch, the tracked ones flagged; " +
    "one a narrowed fetch refspec never fetched gets a per-branch fetch mapping first",
});

export { openRemoteHeads };
