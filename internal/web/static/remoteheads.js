// remoteheads.js — browse remote branches a narrowed fetch refspec hides.
//
// A picker over GET /api/remote-heads: branches that exist on the remote but
// have no local remote-tracking ref (the single-branch/narrowed monorepo
// clone case — invisible in the Remotes section, whose rows come from
// refs/remotes). Picking one offers checkout (stay) or checkout-and-switch;
// either starts the checkout-remote-head op, which writes a per-branch fetch
// mapping and fetches exactly that branch.
//
// Two phases in one overlay, the TUI popup's shape: a remote chooser first
// when the repo has several remotes, straight to the branch list when there
// is just one.
import { $, esc, getJSON } from "./core.js";
import { closeLayer, mountOverlay, pushLayer, showCtxMenu } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { opLine, startOp } from "./ops.js";

// This module builds its own DOM: index.html's `hidden` class has NO global
// rule — the overlay must ship its own `#rheads.hidden` selector or it is
// plainly visible on load.
const CSS = `
#rheads { position: fixed; inset: 0; background: rgba(0,0,0,0.45); display: flex; align-items: flex-start; justify-content: center; z-index: 60; }
#rheads.hidden { display: none; }
#rheads-box { margin-top: 8vh; width: min(640px, 92vw); background: var(--bg-alt); border: 1px solid var(--border); border-radius: 6px; box-shadow: 0 8px 30px rgba(0,0,0,0.5); overflow: hidden; }
#rheads-input { width: 100%; box-sizing: border-box; background: var(--bg); color: var(--fg); border: none; border-bottom: 1px solid var(--border); padding: 8px 10px; font: inherit; }
#rheads-input:focus { outline: none; }
#rheads-list { list-style: none; margin: 0; padding: 0; max-height: 50vh; overflow-y: auto; }
#rheads-list li { padding: 3px 10px; cursor: pointer; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
#rheads-list li.sel { background: var(--sel); }
#rheads-list li.empty { color: var(--dim); cursor: default; }
#rheads-note { color: var(--dim); font-size: 11px; padding: 4px 10px; border-top: 1px solid var(--border); }
`;

const styleEl = document.createElement("style");
styleEl.textContent = CSS;
document.head.append(styleEl);

const rheads = mountOverlay("rheads");
rheads.innerHTML =
  `<div id="rheads-box">` +
  `<input id="rheads-input" type="text" autocomplete="off" spellcheck="false" placeholder="browse remote branches…">` +
  `<ul id="rheads-list"></ul>` +
  `<div id="rheads-note"></div></div>`;

// rh holds the open picker: the phase ("remotes" chooser or "heads" list),
// the loaded rows, the filtered view, and the cursor. gen guards a slow
// ls-remote answer against a picker that was closed and reopened meanwhile.
let rh = null;

let rhGen = 0;

function openRemoteHeads() {
  if (rh) return; // already open
  rh = { phase: "remotes", remote: "", rows: [], filtered: [], sel: 0 };
  $("rheads-input").value = "";
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
  if (!names.length) {
    closeRemoteHeads();
    opLine("browse remote branches: no remotes configured", true);
    return;
  }
  if (names.length === 1) {
    loadHeads(names[0]);
    return;
  }
  rh.phase = "remotes";
  rh.rows = names;
  refilter();
  $("rheads-note").textContent = "pick a remote";
}

async function loadHeads(remote) {
  const gen = ++rhGen;
  rh.phase = "heads";
  rh.remote = remote;
  rh.rows = [];
  $("rheads-input").value = "";
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
  rh.rows = (body.heads || []).map((h) => h.name);
  refilter();
  $("rheads-note").textContent = rh.rows.length
    ? rh.rows.length + " branch(es) on " + remote + " not fetched locally — enter/click to check out"
    : "every branch on " + remote + " is already tracked";
}

// refilter rebuilds the visible rows from the query (case-insensitive
// substring — branch names, not fuzzy prose) and clamps the cursor.
function refilter() {
  const q = $("rheads-input").value.trim().toLowerCase();
  rh.filtered = rh.rows.filter((n) => !q || n.toLowerCase().includes(q));
  rh.sel = Math.max(0, Math.min(rh.sel, rh.filtered.length - 1));
  render();
}

function render() {
  if (!rh.filtered.length) {
    $("rheads-list").innerHTML = `<li class="empty">(no match)</li>`;
    return;
  }
  $("rheads-list").innerHTML = rh.filtered
    .map((n, i) => `<li data-i="${i}"${i === rh.sel ? ' class="sel"' : ""}>${esc(n)}</li>`)
    .join("");
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
  const name = rh.filtered[i];
  if (!name) return;
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
registerRows("remote", () => [{ label: "browse remote branches…", act: () => openRemoteHeads() }]);

registerHelp({
  key: "browse remotes",
  html:
    "☰ → <b>browse remote branches…</b>: branches that exist on the remote but were never " +
    "fetched (a narrowed fetch refspec hides them — the Remotes section can't show what " +
    "<code>refs/remotes</code> doesn't have). One <code>ls-remote</code>, type to filter; " +
    "enter checks one out — staying put or switching — adding a per-branch fetch mapping first",
});

export { openRemoteHeads };
