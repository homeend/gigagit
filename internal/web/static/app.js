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
  wt: null, // /api/status payload while the tree is dirty, else null
  filesMode: "commit", // commit | status
  statusEntries: [],
  branches: [],
  worktrees: [],
  tags: [],
  tagsTruncated: false,
  stashes: [],
  sidebar: true,
  op: null, // {id, es: EventSource} while an operation is live
  lastDiff: null,
  diffBlockIdx: -1,
  detailGen: 0,
};

const $ = (id) => document.getElementById(id);

// Destructive decision options render red in the modal (the ctx-menu
// danger precedent). Options are English protocol values — i18n never
// translates them — so a client-side set is reliable.
const DANGER_OPTIONS = new Set([
  "force", "force-with-lease", "force-delete", "reset", "delete", "drop",
  "unlock-and-remove", "discard", "overwrite", "hard",
]);

const SECTIONS = ["branches", "worktrees", "tags", "stashes"];

// localStorage can throw (private mode); persistence is best-effort.
function lsGet(k) { try { return localStorage.getItem(k); } catch { return null; } }
function lsSet(k, v) { try { localStorage.setItem(k, v); } catch {} }

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
  if (!resp.ok) throw new Error(data.error || resp.statusText);
  return data;
}

function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}

function runes(s) {
  return Array.from(s);
}

// --- working-tree row + status state ---

function wtCount() {
  return state.wt ? 1 : 0;
}

function applyStatus(st) {
  state.wt = st.files && st.files.length ? st : null;
  buildStatusEntries();
}

async function fetchStatus() {
  applyStatus(await getJSON("/api/status"));
}

async function fetchBranches() {
  const [b, w, tg, st] = await Promise.all([
    getJSON("/api/branches"),
    getJSON("/api/worktrees").catch(() => ({ worktrees: [] })),
    getJSON("/api/tags").catch(() => ({ tags: [], truncated: false })),
    getJSON("/api/stashes").catch(() => ({ stashes: [] })),
  ]);
  state.branches = b.branches || [];
  state.worktrees = w.worktrees || [];
  state.tags = tg.tags || [];
  state.tagsTruncated = !!tg.truncated;
  state.stashes = st.stashes || [];
  renderBranches();
  renderWorktrees();
  renderTags();
  renderStashes();
}

function renderBranches() {
  $("branches-list").innerHTML = state.branches
    .map((b) => {
      const ab =
        (b.ahead ? "↑" + b.ahead : "") + (b.behind ? (b.ahead ? " " : "") + "↓" + b.behind : "");
      return (
        `<li class="${b.is_head ? "head" : ""}" data-n="${esc(b.name)}">` +
        `${b.is_head ? "✓ " : ""}${esc(b.name)}${ab ? `<span class="ab">${ab}</span>` : ""}</li>`
      );
    })
    .join("");
}

function renderWorktrees() {
  $("worktrees-list").innerHTML = state.worktrees
    .map((w) => {
      const label = w.bare ? "(bare)" : w.detached ? "(detached)" : w.branch || "(?)";
      const base = w.path.split("/").pop();
      const cur = state.worktree && w.path === state.worktree ? " cur" : "";
      return `<li class="${cur.trim()}" data-p="${esc(w.path)}" title="${esc(w.path)}">${cur ? "● " : ""}${esc(label)}<span class="wpath">${esc(base)}</span></li>`;
    })
    .join("");
}

function renderTags() {
  let html = state.tags
    .map(
      (t) =>
        `<li data-h="${esc(t.target)}" data-n="${esc(t.name)}">${esc(t.name)}` +
        (t.subject ? `<span class="tsub">${esc(t.subject)}</span>` : "") +
        `</li>`
    )
    .join("");
  if (state.tagsTruncated) html += `<li class="more">… more (capped at 100)</li>`;
  $("tags-list").innerHTML = html;
}

function renderStashes() {
  $("stashes-list").innerHTML = state.stashes
    .map(
      (s) =>
        `<li data-r="${esc(s.ref)}"${s.sha ? ` data-h="${esc(s.sha)}"` : ""}>${esc(s.ref)}` +
        (s.subject ? `<span class="tsub">${esc(s.subject)}</span>` : "") +
        `</li>`
    )
    .join("");
}

// --- op transport client ---

let opLineTimer = null;
function opLine(text, isErr) {
  const el = $("op-line");
  el.textContent = text || "";
  el.classList.toggle("err", !!isErr);
  el.classList.toggle("hidden", !text);
  clearTimeout(opLineTimer);
  if (!text) return;
  // every message expires after 30s — but never while its op still runs
  // (each op event overwrites the line and re-arms the timer anyway)
  opLineTimer = setTimeout(() => {
    if (state.op) return;
    el.textContent = "";
    el.classList.add("hidden");
  }, 30000);
}

// startOp is the transport client, op-agnostic: POST /api/op, then follow
// the SSE stream. state.op.kind lets done-handling react per op (a commit
// clears the message box; a switch must not eat a draft).
async function startOp(body, label) {
  if (state.op) return; // one live op; the server would 409 anyway
  let resp;
  try {
    resp = await postJSON("/api/op", body);
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  opLine("⟳ " + label + "…");
  const es = new EventSource("/api/op/" + resp.op_id + "/events");
  state.op = { id: resp.op_id, es, kind: body.op };
  $("pull-btn").disabled = true;
  $("push-btn").disabled = true;
  es.onmessage = (m) => handleOpEvent(JSON.parse(m.data));
  // EventSource auto-retries transient drops (readyState CONNECTING) and
  // the server replays full history on reconnect. A permanent failure
  // (readyState CLOSED — e.g. the server restarted and the op id is gone)
  // or 5 straight failed retries declares the op lost: unlock the UI and
  // refresh so panels show whatever the op actually did.
  let errCount = 0;
  es.onopen = () => { errCount = 0; };
  es.onerror = () => {
    if (!state.op || state.op.es !== es) return; // stale source after done
    errCount++;
    if (es.readyState === EventSource.CLOSED || errCount >= 5) {
      es.close();
      state.op = null;
      $("pull-btn").disabled = false;
      $("push-btn").disabled = false;
      hideModal();
      opLine("error: lost connection to operation — repo state refreshed", true);
      refreshAfterOp();
    } else {
      opLine("⟳ reconnecting…");
    }
  };
}

function startSwitch(branch) {
  startOp({ op: "switch", branch }, "switching " + branch);
}

function doCommit() {
  const message = $("commit-msg").value;
  if (!message.trim()) return;
  startOp({ op: "commit", message }, "committing");
}

function doPull() {
  if (state.op) return;
  // TUI parity: pull is confirmed up front (it may rewrite the working
  // tree); esc maps to abort via the modal's existing rule.
  const branch = $("repo-branch").textContent || "current branch";
  showLocalConfirm("Pull " + branch + "? This may rewrite the working tree.", ["pull", "abort"], (o) => {
    if (o === "pull") startOp({ op: "pull" }, "pulling");
  });
}

function doPush() {
  if (state.op) return;
  startOp({ op: "push" }, "pushing");
}

// toggleSidebar and stageFocused are shared by their keys (b, s/u) and the
// clickable footer chips.
function toggleSidebar() {
  if (state.layout !== "list") return;
  state.sidebar = !state.sidebar;
  lsSet("gg.sidebar.hidden", state.sidebar ? "0" : "1");
  $("panes").classList.toggle("nosb", !state.sidebar);
  renderCommits(); // list width changed
}

function stageFocused(unstage) {
  if (state.pane !== "files" || state.filesMode !== "status") return;
  const f = state.statusEntries[state.fileCursor];
  if (!f || f.section === "conflicts") return;
  if (!unstage && f.section !== "staged") stage({ paths: [f.path] });
  else if (unstage && f.section === "staged") stage({ paths: [f.path], unstage: true });
}

function doStash() {
  if (state.op || !state.wt) return;
  const message = $("commit-msg").value.trim();
  showLocalConfirm("Stash all working-tree changes?", ["stash", "abort"], (o) => {
    if (o === "stash") startOp({ op: "stash", message }, "stashing");
  });
}

function handleOpEvent(ev) {
  if (ev.type === "progress") {
    opLine("⟳ " + ev.step + (ev.detail ? " " + ev.detail : "") + "…");
  } else if (ev.type === "decision") {
    showModal(ev);
  } else if (ev.type === "resolved") {
    hideModal(); // this decision was answered (another tab, or a replay)
  } else if (ev.type === "done") {
    const kind = state.op && state.op.kind;
    // done is terminal: close the source (EventSource would auto-reconnect
    // and replay the history otherwise) and any open modal (covers
    // notify-only decisions whose op already returned).
    if (state.op) state.op.es.close();
    state.op = null;
    $("pull-btn").disabled = false;
    $("push-btn").disabled = false;
    hideModal();
    if (ev.ok && (kind === "commit" || kind === "stash")) $("commit-msg").value = "";
    if (ev.ok) opLine(ev.summary || "done");
    else opLine("error: " + (ev.error || "operation failed"), true);
    if (ev.changed) refreshAfterOp();
    else fetchStatus().then(renderCommits); // a failed switch may still have moved HEAD/stash state
  }
}

// reconcileStatusView keeps an open status screen truthful after any
// status re-read (op done, r, tab focus): the tree may have gone clean or
// shrunk under it.
function reconcileStatusView() {
  if (state.filesMode !== "status") return;
  if (!state.wt) exitStatusToList();
  else {
    state.fileCursor = Math.min(state.fileCursor, Math.max(0, state.statusEntries.length - 1));
    renderFiles();
  }
}

async function refreshAfterOp() {
  await Promise.all([loadRepo(), fetchBranches(), fetchStatus()]);
  // an op can change the working tree while its status screen is open
  // (commit empties it) — reconcile instead of showing stale rows
  reconcileStatusView();
  state.rows = [];
  state.cursor = 0;
  await loadCommits(false);
}

// Files edited while the page is in the background are otherwise invisible
// (no polling; ops are the only other refresh trigger) — re-read the
// status when the tab regains focus, throttled, never during a live op.
let lastFocusRefresh = 0;
window.addEventListener("focus", () => {
  if (state.op || Date.now() - lastFocusRefresh < 2000) return;
  lastFocusRefresh = Date.now();
  fetchStatus()
    .then(() => {
      reconcileStatusView();
      renderCommits();
    })
    .catch(() => {});
});

function showModal(ev) {
  $("modal-prompt").textContent = ev.prompt;
  $("modal-options").innerHTML = (ev.options || [])
    .map((o) => `<button data-o="${esc(o)}"${DANGER_OPTIONS.has(o) ? ' class="danger"' : ""}>${esc(o)}</button>`)
    .join("");
  $("modal").classList.remove("hidden");
  $("modal").dataset.opts = JSON.stringify(ev.options || []);
}

// modalLocalCb, when set, routes the next modal answer to a CLIENT-side
// callback instead of the op decide endpoint — pre-flight confirms (pull)
// reuse the one modal without touching the transport.
let modalLocalCb = null;

function showLocalConfirm(prompt, options, cb) {
  modalLocalCb = cb;
  showModal({ prompt, options });
}

function hideModal() {
  $("modal").classList.add("hidden");
  modalLocalCb = null; // a done-driven close must not leak the callback to the next modal
}

async function answerModal(option) {
  if (modalLocalCb) {
    const cb = modalLocalCb; // capture first — hideModal clears it
    hideModal();
    cb(option);
    return;
  }
  if (!state.op) return hideModal();
  hideModal();
  try {
    await postJSON("/api/op/" + state.op.id + "/decide", { option });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
  }
}

$("modal-options").addEventListener("click", (e) => {
  const btn = e.target.closest("button");
  if (btn) answerModal(btn.dataset.o);
});
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

function hideCtxMenu() {
  $("ctx-menu").classList.add("hidden");
}

// showCtxMenu renders the shared right-click menu at (x,y): safe actions
// first; rows flagged danger render red.
function showCtxMenu(items, x, y) {
  const menu = $("ctx-menu");
  menu._items = items;
  menu.innerHTML = items
    .map((it, i) => `<button data-i="${i}"${it.danger ? ' class="danger"' : ""}>${esc(it.label)}</button>`)
    .join("");
  menu.style.left = Math.min(x, window.innerWidth - 200) + "px";
  menu.style.top = Math.min(y, window.innerHeight - 120) + "px";
  menu.classList.remove("hidden");
}

function showBranchMenu(b, x, y) {
  const items = [{ label: "go to tip", act: () => gotoBranchTip(b) }];
  if (!b.is_head) {
    items.push({ label: "switch to " + b.name, act: () => startSwitch(b.name) });
    items.push({
      label: "delete " + b.name,
      danger: true,
      act: () => startOp({ op: "delete-branch", branch: b.name }, "deleting " + b.name),
    });
  }
  showCtxMenu(items, x, y);
}

$("ctx-menu").addEventListener("click", (e) => {
  const btn = e.target.closest("button");
  const menu = $("ctx-menu");
  if (btn && menu._items) menu._items[Number(btn.dataset.i)].act();
  hideCtxMenu();
});
document.addEventListener("click", (e) => {
  if (!e.target.closest("#ctx-menu")) hideCtxMenu();
});

$("branches-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  const b = state.branches.find((x) => x.name === li.dataset.n);
  if (b) gotoBranchTip(b);
});
$("branches-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  e.preventDefault();
  const b = state.branches.find((x) => x.name === li.dataset.n);
  if (b) showBranchMenu(b, e.clientX, e.clientY);
});

function copyText(text) {
  navigator.clipboard.writeText(text).catch(() => opLine("copy failed (clipboard unavailable)", true));
}

function showWorktreeMenu(w, x, y) {
  const items = [{ label: "copy path", act: () => copyText(w.path) }];
  // The served worktree's row gets no remove (the engine would refuse it
  // anyway); main is engine-guarded too.
  if (!(state.worktree && w.path === state.worktree)) {
    items.push({
      label: "remove worktree",
      danger: true,
      act: () => startOp({ op: "remove-worktree", path: w.path }, "removing " + w.path.split("/").pop()),
    });
  }
  showCtxMenu(items, x, y);
}
$("worktrees-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.p) return;
  e.preventDefault();
  const w = state.worktrees.find((x) => x.path === li.dataset.p);
  if (w) showWorktreeMenu(w, e.clientX, e.clientY);
});
// drillOut leaves the detail screen for the full-width commit list — the
// esc key and the mouse back button share it.
function drillOut() {
  if (state.layout !== "detail") return;
  state.detailGen++; // invalidate any in-flight detail fetch
  state.pane = "commits";
  setLayout("list");
  focusPane();
}
$("back-btn").addEventListener("click", drillOut);

// Double-click a sidebar section header to collapse/expand its list — long
// branch/tag lists otherwise force constant scrolling.
function toggleSection(name) {
  const collapsed = $(name + "-list").classList.toggle("collapsed");
  $(name + "-header").textContent = (collapsed ? "\u25b8 " : "") + name;
  lsSet("gg.sidebar.collapsed", JSON.stringify(SECTIONS.filter((n) => $(n + "-list").classList.contains("collapsed"))));
}
SECTIONS.forEach((n) => {
  $(n + "-header").addEventListener("dblclick", () => toggleSection(n));
});

// Restore persisted sidebar state (b-key visibility + per-section
// collapse). The collapsed class lives on the persistent <ul> containers,
// so a one-time boot restore survives every re-render.
(function restoreSidebar() {
  let names = [];
  try { names = JSON.parse(lsGet("gg.sidebar.collapsed") || "[]"); } catch {}
  SECTIONS.forEach((n) => {
    if (names.includes(n)) {
      $(n + "-list").classList.add("collapsed");
      $(n + "-header").textContent = "\u25b8 " + n;
    }
  });
  if (lsGet("gg.sidebar.hidden") === "1") {
    state.sidebar = false;
    $("panes").classList.add("nosb");
  }
})();

$("tags-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.h) return;
  openCommitByHash(li.dataset.h, "🏷 " + li.dataset.n);
});

function showTagMenu(tg, x, y) {
  showCtxMenu(
    [
      { label: "show commit", act: () => openCommitByHash(tg.target, "🏷 " + tg.name) },
      { label: "copy name", act: () => copyText(tg.name) },
      {
        label: "delete " + tg.name,
        danger: true,
        // engine.DeleteTag is decision-free, so the confirm lives here — a
        // right-click plus one click must never delete a ref unconfirmed.
        act: () =>
          showLocalConfirm("Delete tag " + tg.name + "?", ["delete", "abort"], (o) => {
            if (o === "delete") startOp({ op: "delete-tag", tag: tg.name }, "deleting tag " + tg.name);
          }),
      },
    ],
    x,
    y
  );
}
$("tags-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  e.preventDefault();
  const tg = state.tags.find((x) => x.name === li.dataset.n);
  if (tg) showTagMenu(tg, e.clientX, e.clientY);
});

$("stashes-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.h) return; // a sha-less row ignores left-click
  const st = state.stashes.find((x) => x.ref === li.dataset.r);
  if (st) openStashDetail(st);
});

function showStashMenu(st, x, y) {
  const items = [];
  if (st.sha) items.push({ label: "show changes", act: () => openStashDetail(st) });
  items.push({ label: "apply", act: () => startOp({ op: "stash-apply", ref: st.ref, sha: st.sha || "" }, "applying " + st.ref) });
  items.push({ label: "pop", act: () => startOp({ op: "stash-pop", ref: st.ref, sha: st.sha || "" }, "popping " + st.ref) });
  items.push({
    label: "drop " + st.ref,
    danger: true,
    // engine.StashDrop is decision-free — the confirm lives here (the
    // delete-tag precedent; the TUI confirms drop with y/n too).
    act: () =>
      showLocalConfirm("Drop " + st.ref + "?", ["drop", "abort"], (o) => {
        if (o === "drop") startOp({ op: "stash-drop", ref: st.ref, sha: st.sha || "" }, "dropping " + st.ref);
      }),
  });
  showCtxMenu(items, x, y);
}
$("stashes-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.r) return;
  e.preventDefault();
  const st = state.stashes.find((x) => x.ref === li.dataset.r);
  if (st) showStashMenu(st, e.clientX, e.clientY);
});

// A partially-staged file appears twice: once under Staged (unstage
// control), once under Changes (stage control) — the git-status model.
function buildStatusEntries() {
  const es = [];
  for (const f of state.wt ? state.wt.files : []) {
    if (f.kind === "conflicted") es.push({ ...f, section: "conflicts" });
    else if (f.kind === "untracked") es.push({ ...f, section: "untracked" });
    else {
      if (f.staged !== ".") es.push({ ...f, section: "staged" });
      if (f.unstaged !== ".") es.push({ ...f, section: "changes" });
    }
  }
  const order = { staged: 0, changes: 1, untracked: 2, conflicts: 3 };
  es.sort((a, b) => order[a.section] - order[b.section] || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
  state.statusEntries = es;
}

function wtRowHTML(i) {
  const sel = i === state.cursor ? " sel" : "";
  const c = state.wt.counts;
  const parts = [];
  if (c.staged) parts.push(c.staged + " staged");
  if (c.unstaged) parts.push(c.unstaged + " changed");
  if (c.untracked) parts.push(c.untracked + " untracked");
  if (c.conflicted) parts.push(c.conflicted + " conflicted");
  return (
    `<div class="crow wt${sel}" data-i="${i}">` +
    `<span class="graph">●</span>` +
    `<span class="subj">Working tree</span>` +
    `<span class="meta">${esc(parts.join(" · "))}</span></div>`
  );
}

// --- commits pane (virtualized: only visible rows exist in the DOM) ---

function renderCommits() {
  const scroll = $("commits-scroll");
  const total = state.rows.length + wtCount();
  $("commits-spacer").style.height = total * ROW_H + "px";
  const first = Math.max(0, Math.floor(scroll.scrollTop / ROW_H) - 10);
  const last = Math.min(total, Math.ceil((scroll.scrollTop + scroll.clientHeight) / ROW_H) + 10);
  const win = $("commits-window");
  win.style.top = first * ROW_H + "px";
  let html = "";
  for (let i = first; i < last; i++) {
    html += state.wt && i === 0 ? wtRowHTML(i) : rowHTML(state.rows[i - wtCount()], i);
  }
  win.innerHTML = html;
  maybeLoadMore(last - wtCount());
}

function rowHTML(row, i) {
  const sel = i === state.cursor ? " sel" : "";
  const refs = (row.refs || [])
    .map((r) => `<span class="ref ${r.kind}${r.head ? " head" : ""}">${esc(r.name)}</span>`)
    .join("");
  const when = new Date(row.time * 1000).toISOString().slice(0, 10);
  return (
    `<div class="crow${sel}" data-i="${i}">` +
    `<span class="graph">${graphHTML(row, i - wtCount())}</span>` +
    `<span class="subj">${refs}${esc(row.subject)}</span>` +
    `<span class="meta">${esc(row.author)} · ${row.short} · ${when}</span></div>`
  );
}

function graphHTML(row, feedIdx) {
  if (state.graphMode === "off") {
    // flat mode: one dot per row in the commit's lane color — dots keep
    // rows visually separate (full-height bars merged into one line)
    const col = runes(row.cells || "").indexOf("●");
    return `<span class="flatdot lane-${col >= 0 ? (col >> 1) % 8 : 0}">●</span>`;
  }
  return graphSVG(row, feedIdx);
}

function toggleGraphMode() {
  state.graphMode = state.graphMode === "svg" ? "off" : "svg";
  lsSet("gg.graph", state.graphMode);
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
      parts += `<circle cx="${x + HALF}" cy="${MID}" r="4" fill="${color}"/>`;
    } else if (GLYPH_PATHS[ch]) {
      parts += `<path d="${GLYPH_PATHS[ch](x)}" stroke="${color}" stroke-width="2" fill="none" stroke-linecap="round"/>`;
    } else if (ch !== " ") {
      parts += `<text x="${x}" y="${ROW_H - 6}" fill="${color}" font-size="12">${esc(ch)}</text>`;
    }
  });
  return parts + "</svg>";
}

async function loadCommits(more) {
  const body = await getJSON(more ? "/api/commits?more=1" : "/api/commits");
  state.rows = body.rows || [];
  state.canLoadMore = body.can_load_more;
  renderCommits();
}

function maybeLoadMore(lastVisible) {
  if (!state.canLoadMore || state.loadingMore) return;
  if (lastVisible < state.rows.length - 30) return;
  state.loadingMore = true;
  loadCommits(true).finally(() => {
    state.loadingMore = false;
  });
}

// --- files + diff panes ---

// Layout mirrors the TUI's drill-in: "list" = the commit list alone, full
// width; "detail" = the opened commit's files + diff, list hidden (esc
// returns). Never both crammed on one screen.
function setLayout(mode) {
  state.layout = mode;
  const p = $("panes");
  p.classList.toggle("solo", mode === "list");
  p.classList.toggle("detail", mode === "detail");
  if (mode === "list") moveCursor(0); // re-render + rescroll: display:none dropped the scroll position
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
  state.pane = "files";
  state.filesMode = "commit";
  setLayout("detail");
  $("files-header").textContent = row.short + " " + row.subject;
  renderFiles();
  focusPane();
  if (state.files.length) openFile(0);
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
  state.pane = "files";
  state.filesMode = "commit";
  setLayout("detail");
  $("files-header").textContent = title;
  renderFiles();
  focusPane();
  if (state.files.length) openFile(0);
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
  state.pane = "files";
  state.filesMode = "commit";
  setLayout("detail");
  $("files-header").textContent = "≡ " + st.ref;
  renderFiles();
  focusPane();
  if (state.files.length) openFile(0);
}

async function openWorkingTree(i) {
  state.cursor = i;
  renderCommits();
  await fetchStatus(); // refresh on open — external changes since boot
  if (!state.wt) {
    renderCommits();
    return;
  }
  state.filesMode = "status";
  state.fileCursor = 0;
  state.pane = "files";
  setLayout("detail");
  $("files-header").textContent = "Working tree";
  renderFiles();
  focusPane();
  if (state.statusEntries.length) openFile(0);
}

const SECTION_LABELS = { staged: "Staged", changes: "Changes", untracked: "Untracked", conflicts: "Conflicts" };

function renderFiles() {
  if (state.filesMode !== "status") {
    $("files-actions").classList.add("hidden");
    $("commit-box").classList.add("hidden");
    $("files-list").innerHTML = state.files
      .map(
        (f, i) =>
          `<li class="${i === state.fileCursor ? "sel" : ""}" data-i="${i}">` +
          `<span class="st ${esc(f.status)}">${esc(f.status)}</span>${esc(f.path)}</li>`
      )
      .join("");
    return;
  }
  $("files-actions").classList.remove("hidden");
  $("commit-box").classList.remove("hidden");
  $("commit-btn").disabled = !(state.wt && state.wt.counts.staged > 0) || !!state.op;
  $("stash-btn").disabled = !state.wt || !!state.op;
  let html = "";
  let lastSection = "";
  state.statusEntries.forEach((f, i) => {
    if (f.section !== lastSection) {
      html += `<li class="sect">${SECTION_LABELS[f.section]}</li>`;
      lastSection = f.section;
    }
    const badge = f.section === "staged" ? f.staged : f.unstaged;
    const btn =
      f.section === "conflicts"
        ? ""
        : f.section === "staged"
          ? `<button class="act" data-i="${i}" data-un="1">u</button>`
          : `<button class="act" data-i="${i}">s</button>`;
    html +=
      `<li class="${i === state.fileCursor ? "sel" : ""} ${f.section}" data-i="${i}">` +
      `<span class="st">${esc(badge)}</span>${esc(f.path)}${btn}</li>`;
  });
  $("files-list").innerHTML = html;
}

async function openFile(i) {
  state.fileCursor = i;
  renderFiles();
  updateDiffNav();
  if (state.filesMode === "status") return openStatusDiff(i);
  const f = state.files[i];
  const q = new URLSearchParams({ sha: f.sha || state.fileSha, path: f.path, status: f.status });
  if (f.old_path) q.set("old", f.old_path);
  $("diff-title").textContent = f.path;
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  try {
    renderDiff(await getJSON("/api/diff?" + q));
  } catch (e) {
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
    updateDiffNav();
  }
}

async function openStatusDiff(i) {
  const f = state.statusEntries[i];
  $("diff-title").textContent = f.path;
  if (f.section === "conflicts") {
    $("diff-body").innerHTML = `<div class="notice">conflicted — resolve in the TUI</div>`;
    updateDiffNav();
    return;
  }
  const q = new URLSearchParams({ wt: f.section === "staged" ? "staged" : "unstaged", path: f.path });
  if (f.orig_path) q.set("old", f.orig_path);
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  try {
    renderDiff(await getJSON("/api/diff?" + q));
  } catch (e) {
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
    updateDiffNav();
  }
}

// exitStatusToList tears the status screen down to the full-width list —
// used when the working tree goes clean (all staged changes committed, or
// the last change unstaged away).
function exitStatusToList() {
  state.filesMode = "commit";
  state.pane = "commits";
  state.cursor = 0;
  $("files-list").innerHTML = "";
  $("files-actions").classList.add("hidden");
  $("commit-box").classList.add("hidden");
  $("files-header").textContent = "";
  $("diff-title").textContent = "";
  $("diff-body").innerHTML = "";
  state.lastDiff = null; // a resize must not resurrect the cleared diff
  setLayout("list");
  focusPane();
}

async function stage(body) {
  try {
    applyStatus(await postJSON("/api/stage", body));
  } catch (e) {
    $("files-header").textContent = "error: " + (e.message || e);
    return;
  }
  if (!state.wt) {
    exitStatusToList();
    return;
  }
  state.fileCursor = Math.min(state.fileCursor, state.statusEntries.length - 1);
  renderFiles();
  renderCommits(); // badge counts changed
}

function markSpans(text, spans, side) {
  if (!spans || !spans.length) return esc(text);
  const rs = runes(text);
  let out = "";
  let pos = 0;
  for (const [a, b] of spans) {
    out += esc(rs.slice(pos, a).join(""));
    out += `<mark class="${side}">` + esc(rs.slice(a, b).join("")) + "</mark>";
    pos = b;
  }
  return out + esc(rs.slice(pos).join(""));
}

function renderDiff(d) {
  state.lastDiff = d; // re-rendered on window resize (layout is width-dependent)
  state.diffBlockIdx = -1;
  if (d.binary) {
    $("diff-body").innerHTML = `<div class="notice">binary file</div>`;
    updateDiffNav();
    return;
  }
  if (d.too_large) {
    $("diff-body").innerHTML = `<div class="notice">diff too large</div>`;
    updateDiffNav();
    return;
  }
  const rows = d.rows || [];
  // An all-new or all-deleted file renders single-column: a side-by-side
  // with one permanently empty side wastes half the pane and forces harsh
  // wrapping on the populated half.
  const pureAdd = rows.length > 0 && rows.every((r) => !r.left_no);
  const pureDel = rows.length > 0 && rows.every((r) => !r.right_no);
  let html = `<table class="diff">`;
  if (pureAdd || pureDel) {
    const side = pureAdd ? "r" : "l";
    for (const r of rows) {
      const no = pureAdd ? r.right_no : r.left_no;
      const text = pureAdd ? r.right : r.left;
      const spans = pureAdd ? r.right_spans : r.left_spans;
      html +=
        `<tr class="${r.kind}">` +
        `<td class="no ${side}">${no || ""}</td>` +
        `<td class="side ${side}">${markSpans(text, spans, side)}</td></tr>`;
    }
  } else if ($("diff-pane").clientWidth < 950) {
    // Unified: below ~950px each side-by-side half is too narrow to read
    // (heavy wrapping, context text duplicated on both sides). One
    // full-width column; a changed pair becomes a del row then an add row,
    // keeping the intraline marks of each side.
    for (const r of rows) {
      if (r.kind === "same") {
        html +=
          `<tr class="same"><td class="no l">${r.left_no || ""}</td>` +
          `<td class="no r">${r.right_no || ""}</td>` +
          `<td class="side">${esc(r.right)}</td></tr>`;
      } else {
        if (r.kind !== "add")
          html +=
            `<tr class="del"><td class="no l">${r.left_no || ""}</td><td class="no r"></td>` +
            `<td class="side l">${markSpans(r.left, r.left_spans, "l")}</td></tr>`;
        if (r.kind !== "del")
          html +=
            `<tr class="add"><td class="no l"></td><td class="no r">${r.right_no || ""}</td>` +
            `<td class="side r">${markSpans(r.right, r.right_spans, "r")}</td></tr>`;
      }
    }
  } else {
    for (const r of rows) {
      html +=
        `<tr class="${r.kind}">` +
        `<td class="no l">${r.left_no || ""}</td>` +
        `<td class="side l">${markSpans(r.left, r.left_spans, "l")}</td>` +
        `<td class="no r">${r.right_no || ""}</td>` +
        `<td class="side r">${markSpans(r.right, r.right_spans, "r")}</td></tr>`;
    }
  }
  html += "</table>";
  if (d.truncated) html += `<div class="notice">alignment truncated (size guard)</div>`;
  $("diff-body").innerHTML = html;
  updateDiffNav();
}

// --- diff-pane navigation (the diff-header toolbar) ---

function activeFileList() {
  return state.filesMode === "status" ? state.statusEntries : state.files;
}

function updateDiffNav() {
  const list = activeFileList();
  $("prev-file").disabled = list.length === 0 || state.fileCursor <= 0;
  $("next-file").disabled = list.length === 0 || state.fileCursor >= list.length - 1;
  const any = diffChangeBlocks().length > 0;
  $("prev-change").disabled = !any;
  $("next-change").disabled = !any;
}

function stepFile(delta) {
  const list = activeFileList();
  const i = state.fileCursor + delta;
  if (i < 0 || i >= list.length) return;
  openFile(i);
}

// diffChangeBlocks returns the first row of each contiguous non-"same" run
// in the rendered diff table (add/del/change rows; a unified changed pair
// renders del+add adjacent — still one run). Derived from the live DOM so
// it survives any render mode (side-by-side, unified, single-column).
function diffChangeBlocks() {
  const rows = $("diff-body").querySelectorAll("table.diff tr");
  const blocks = [];
  let inBlock = false;
  rows.forEach((tr) => {
    const change = !tr.classList.contains("same");
    if (change && !inBlock) blocks.push(tr);
    inBlock = change;
  });
  return blocks;
}

function stepChange(delta) {
  const blocks = diffChangeBlocks();
  if (!blocks.length) return;
  const i = Math.max(0, Math.min(blocks.length - 1, state.diffBlockIdx + delta));
  state.diffBlockIdx = i;
  const tr = blocks[i];
  tr.scrollIntoView({ block: "center" });
  tr.classList.add("flash");
  setTimeout(() => tr.classList.remove("flash"), 600);
}

$("prev-file").addEventListener("click", () => stepFile(-1));
$("next-file").addEventListener("click", () => stepFile(1));
$("prev-change").addEventListener("click", () => stepChange(-1));
$("next-change").addEventListener("click", () => stepChange(1));

// --- focus + keyboard ---

function focusPane() {
  document.querySelectorAll(".pane").forEach((p) => p.classList.remove("focused"));
  $(state.pane === "commits" ? "commits-pane" : "files-pane").classList.add("focused");
}

function moveCursor(delta) {
  if (state.pane === "commits") {
    const total = state.rows.length + wtCount();
    if (!total) return;
    state.cursor = Math.max(0, Math.min(total - 1, state.cursor + delta));
    const scroll = $("commits-scroll");
    const top = state.cursor * ROW_H;
    if (top < scroll.scrollTop) scroll.scrollTop = top;
    else if (top + ROW_H > scroll.scrollTop + scroll.clientHeight)
      scroll.scrollTop = top + ROW_H - scroll.clientHeight;
    renderCommits();
  } else {
    const list = state.filesMode === "status" ? state.statusEntries : state.files;
    if (!list.length) return;
    state.fileCursor = Math.max(0, Math.min(list.length - 1, state.fileCursor + delta));
    renderFiles();
  }
}

document.addEventListener("keydown", (e) => {
  if (!$("modal").classList.contains("hidden")) {
    if (e.key === "Escape") {
      const opts = JSON.parse($("modal").dataset.opts || "[]");
      if (opts.includes("abort")) answerModal("abort"); // the TUI's esc rule
    }
    e.preventDefault();
    return; // the modal owns the keyboard — even over a focused form field
  }
  if (!$("ctx-menu").classList.contains("hidden")) {
    if (e.key === "Escape") hideCtxMenu();
    return; // the context menu owns the keyboard until closed
  }
  if (!$("help").classList.contains("hidden")) {
    if (e.key === "Escape" || e.key === "?") $("help").classList.add("hidden");
    e.preventDefault();
    return; // the help overlay owns the keyboard until closed
  }
  // Form fields own the keyboard: without this, typing a commit message
  // triggers j/k navigation and s/u staging. Ctrl/Cmd+Enter commits.
  if (e.target.closest && e.target.closest("input,textarea")) {
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey) && e.target.id === "commit-msg") {
      e.preventDefault();
      doCommit();
    }
    return;
  }
  if (e.key === "j" || e.key === "ArrowDown") {
    e.preventDefault();
    moveCursor(1);
  } else if (e.key === "k" || e.key === "ArrowUp") {
    e.preventDefault();
    moveCursor(-1);
  } else if (e.key === "Enter") {
    if (state.pane === "commits") openCommit(state.cursor);
    else if (state.filesMode === "status" ? state.statusEntries.length : state.files.length) openFile(state.fileCursor);
  } else if (e.key === "Escape") {
    drillOut();
  } else if (e.key === "g") {
    toggleGraphMode();
  } else if (e.key === "b") {
    toggleSidebar();
  } else if (e.key === "p") {
    doPull();
  } else if (e.key === "P") {
    doPush();
  } else if (e.key === "?") {
    $("help").classList.remove("hidden");
  } else if (e.key === "r") {
    if (!state.op) refreshAfterOp(); // full soft reload: repo, sidebar, status, commits
  } else if (e.key === "s" || e.key === "u") {
    stageFocused(e.key === "u");
  }
});

// The footer chips execute their key's action on click.
$("foot").addEventListener("click", (e) => {
  const btn = e.target.closest("button[data-act]");
  if (!btn) return;
  switch (btn.dataset.act) {
    case "back": drillOut(); break;
    case "sidebar": toggleSidebar(); break;
    case "graph": toggleGraphMode(); break;
    case "stage": stageFocused(false); break;
    case "unstage": stageFocused(true); break;
    case "pull": doPull(); break;
    case "push": doPush(); break;
    case "refresh": if (!state.op) refreshAfterOp(); break;
    case "help": $("help").classList.remove("hidden"); break;
  }
});

$("commits-scroll").addEventListener("scroll", renderCommits);
$("commits-window").addEventListener("click", (e) => {
  const row = e.target.closest(".crow");
  if (row) openCommit(Number(row.dataset.i));
});
$("files-list").addEventListener("click", (e) => {
  const btn = e.target.closest("button.act");
  if (btn && state.filesMode === "status") {
    const f = state.statusEntries[Number(btn.dataset.i)];
    stage(btn.dataset.un ? { paths: [f.path], unstage: true } : { paths: [f.path] });
    return;
  }
  const li = e.target.closest("li");
  if (li && li.dataset.i !== undefined) {
    state.pane = "files";
    focusPane();
    openFile(Number(li.dataset.i));
  }
});
$("help").addEventListener("click", () => $("help").classList.add("hidden"));
$("help-box").addEventListener("click", (e) => e.stopPropagation()); // allow selecting/copying text
$("stage-all").addEventListener("click", () => stage({ all: true }));
$("unstage-all").addEventListener("click", () => {
  const paths = state.statusEntries.filter((f) => f.section === "staged").map((f) => f.path);
  if (paths.length) stage({ paths, unstage: true }); // engine.Stage{All} can't unstage
});
$("commit-btn").addEventListener("click", doCommit);
$("pull-btn").addEventListener("click", doPull);
$("push-btn").addEventListener("click", doPush);
$("stash-btn").addEventListener("click", doStash);
window.addEventListener("resize", () => {
  renderCommits();
  if (state.lastDiff) renderDiff(state.lastDiff); // unified↔side-by-side is width-dependent
});

async function loadRepo() {
  const repo = await getJSON("/api/repo");
  $("repo-name").textContent = repo.name;
  $("repo-branch").textContent = repo.branch;
  $("repo-worktree").textContent = repo.worktree;
  document.title = "gg web — " + repo.name;
  state.worktree = repo.worktree;
}

async function boot() {
  await loadRepo();
  await fetchStatus().catch(() => {}); // status failing must not block browse
  await fetchBranches().catch(() => {});
  await loadCommits(false);
  focusPane();
}
boot().catch((e) => {
  $("repo-name").textContent = "error: " + (e.message || e);
});
