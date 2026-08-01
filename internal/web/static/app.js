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
  conflict: null, // {op, source, target, desc, conflicted} while a sequencer op is paused, else null
  filesMode: "commit", // commit | status | compare
  compare: null, // {a, b, aHash, bHash, all, filter, originsError} while comparing two branches
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
  dragBranch: null, // name of the branch being dragged, else null
  solo: "", // branch the commit list is narrowed to ("" = every branch)
  // A parked (backgrounded) long task, and then its result until collected:
  // {label, status: running|done|failed|cancelled, title, path, report, error}
  task: null,
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

const SECTIONS = ["branches", "worktrees", "tags", "stashes"];

// localStorage can throw (private mode); persistence is best-effort.
function lsGet(k) { try { return localStorage.getItem(k); } catch { return null; } }
function lsSet(k, v) { try { localStorage.setItem(k, v); } catch {} }

// --- overlay layer stack ---
// Every overlay surface (decision modal, help, ctx-menu, future popups)
// registers here. One rule: a non-empty stack owns the keyboard — the top
// layer's onKey sees the event first; an unhandled Escape closes the top
// layer. closeLayer(id) removes a layer WHEREVER it sits in the stack:
// the op transport must be able to close a parked decision modal even
// under an open help overlay.
const layers = [];

function pushLayer(id, el, opts) {
  if (layers.some((l) => l.id === id)) return; // one instance per surface
  el.classList.remove("hidden");
  layers.push({ id, el, onKey: (opts && opts.onKey) || null });
}

function closeLayer(id) {
  const i = layers.findIndex((l) => l.id === id);
  if (i < 0) return; // idempotent
  const [l] = layers.splice(i, 1);
  l.el.classList.add("hidden");
}

function topLayer() {
  return layers[layers.length - 1] || null;
}

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

// The Working-tree row renders taller than a commit row (WT_H vs ROW_H);
// the virtualized list accounts for the difference in its spacer and
// scroll math via wtExtra.
const WT_H = 30;
function wtExtra() {
  return state.wt ? WT_H - ROW_H : 0;
}

function applyStatus(st) {
  state.wt = st.files && st.files.length ? st : null;
  state.conflict = st.conflict || null;
  buildStatusEntries();
  renderConflictBar();
}

// The banner shows whenever a sequencer op is paused — including with zero
// conflicted files (resolved by hand, never continued): that is exactly when
// Continue lights up. Never leave the user in a paused op with no way out.
function renderConflictBar() {
  const bar = $("conflict-bar"), c = state.conflict;
  if (!c) { bar.classList.add("hidden"); return; }
  bar.classList.remove("hidden");
  // innerHTML so the conflicted count can carry its highlight class — c.op
  // and c.desc come off the wire (desc holds branch names), so both esc().
  $("conflict-msg").innerHTML =
    "⏸ " + esc(c.op) + " paused" + (c.desc ? " (" + esc(c.desc) + ")" : "") +
    (c.conflicted
      ? ` — <span class="conflict-count">${c.conflicted} conflicted</span>`
      : " — all conflicts resolved");
  $("conflict-continue").disabled = !!c.conflicted;
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
        `<li class="${b.is_head ? "head" : ""}" draggable="true" data-n="${esc(b.name)}">` +
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
function hideOpLine() {
  clearTimeout(opLineTimer);
  $("op-text").textContent = "";
  $("op-line").classList.add("hidden");
}

function opLine(text, isErr) {
  const el = $("op-line");
  $("op-text").textContent = text || "";
  el.classList.toggle("err", !!isErr);
  el.classList.remove("task"); // a new message is never the parked-run handle
  el.classList.toggle("hidden", !text);
  clearTimeout(opLineTimer);
  if (!text) return;
  // every message expires after 30s — but never while its op still runs
  // (each op event overwrites the line and re-arms the timer anyway)
  opLineTimer = setTimeout(() => {
    // NOT opBusy(): that reports, and reporting re-arms this timer.
    // A BACKGROUNDED review is exempt from the never-expire-while-running
    // rule: that rule assumes the line is being rewritten by the op's own
    // progress events, and a parked run emits none — so its line would
    // otherwise just sit there for the whole run. The chip is the indicator.
    if (state.op && !parkedRunning()) return;
    hideOpLine();
  }, 30000);
}

// taskLine is the parked-run status line. Unlike every other message it is a
// live HANDLE on something still running, not a notice — so it is clickable
// and exempt from dismissal (see the two listeners below).
function taskLine(text) {
  opLine(text); // clears .task first, then we claim it
  $("op-line").classList.add("task");
}

// Clicking the parked-run line reopens the review. This line is the biggest
// thing on screen mentioning that run, so it is what people click; without
// this the click did nothing and the double-click below deleted it.
$("op-line").addEventListener("click", () => {
  if ($("op-line").classList.contains("task") && parkedRunning()) unparkReview();
});

// Double-click anywhere in the strip dismisses it immediately (the error
// header advertises this) — but NOT the parked-run line, whose dismissal is
// undiscoverable (the header only renders on errors) and which would silently
// remove the most visible mention of a running review.
$("op-line").addEventListener("dblclick", () => {
  if ($("op-line").classList.contains("task")) return;
  hideOpLine();
});

// startOp is the transport client, op-agnostic: POST /api/op, then follow
// the SSE stream. state.op.kind lets done-handling react per op (a commit
// clears the message box; a switch must not eat a draft).
// opBusy is the shared "an operation is already running" guard for every op
// entry point. It REPORTS rather than returning silently: these guards used
// to sit in front of a visible modal or progress line that explained itself,
// but a backgrounded review holds the lane with nothing on screen at all —
// and a button that does nothing without saying why reads as broken.
// parkedRunning: a review is live but backgrounded, so the only thing on
// screen speaking for it is the top-bar chip.
function parkedRunning() {
  return !!(state.task && state.task.status === "running");
}

function opBusy() {
  if (!state.op) return false;
  if (parkedRunning()) {
    const noun = state.task.kind === "conflict" ? "an AI resolve run" : "a review";
    opLine(noun + " is running in the background — open the chip to watch or cancel it", true);
  } else {
    opLine("an operation is already running", true);
  }
  return true;
}

async function startOp(body, label) {
  if (opBusy()) return; // one live op; the server would 409 anyway
  let resp;
  try {
    resp = await postJSON("/api/op", body);
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  followOp(resp.op_id, label, body.op, null);
}

// followOp attaches the SSE client to an already-started run. Split out of
// startOp because the review lane starts its run at a different endpoint but
// wants the identical stream handling — including the lost-connection rules.
// onDone, when given, REPLACES the generic done handling for that run.
function followOp(opID, label, kind, onDone) {
  opLine("⟳ " + label + "…");
  const es = new EventSource("/api/op/" + opID + "/events");
  state.op = { id: opID, es, kind, onDone: onDone || null };
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
      closeReviewLane(); // a review's own overlay would otherwise spin forever
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
  // report instead of a dead click — a silent return here read as
  // "commit is broken" the first time someone hit it mid-merge
  if (!message.trim()) { opLine("write a commit message first", true); return; }
  startOp({ op: "commit", message }, "committing");
}

function doPull() {
  if (opBusy()) return;
  // TUI parity: pull is confirmed up front (it may rewrite the working
  // tree); esc maps to abort via the modal's existing rule.
  const branch = $("repo-branch").textContent || "current branch";
  showLocalConfirm("Pull " + branch + "? This may rewrite the working tree.", ["pull", "abort"], (o) => {
    if (o === "pull") startOp({ op: "pull" }, "pulling");
  });
}

// Pulling a branch you are NOT standing on updates its ref without checking
// it out, so there is nothing to rewrite and nothing to confirm — unlike
// doPull above. The server sends the current branch down the ordinary
// pull-and-stay lane if the two happen to coincide.
function doPullBranch(name) {
  if (opBusy()) return;
  startOp({ op: "pull", branch: name }, "pulling " + name);
}

// Force push. The wire flag does NOT force anything: engine.Push{Force} asks
// the push-force decision (force-with-lease / force / abort) and pushes what
// comes back, so this row's only effect is reaching that modal without
// waiting for a rejection. Hence no client confirm — the modal IS the
// confirm, and its two force options render red via DANGER_OPTIONS.
function doForcePush(name) {
  startOp({ op: "push", branch: name, force: true }, "force-pushing " + name);
}

function doPushBranch(name) {
  if (opBusy()) return;
  startOp({ op: "push", branch: name }, "pushing " + name);
}

function doFetch() {
  if (opBusy()) return;
  startOp({ op: "fetch" }, "fetching");
}

function doPush() {
  if (opBusy()) return;
  startOp({ op: "push" }, "pushing");
}

// doReroot points the server at another root. The whole client state is
// repo-scoped, so a clean reload is the honest reset on success
// (localStorage prefs survive); errors land on the status strip.
async function doReroot(path) {
  if (opBusy()) return;
  try {
    await postJSON("/api/reroot", { path });
    location.reload();
  } catch (e) {
    opLine("error: " + (e.message || e), true);
  }
}

// toggleSidebar and stageFocused are shared by their keys (b, s/u) and the
// clickable footer chips.
function toggleSidebar() {
  if (state.layout === "diff") return; // no sidebar on the diff screen to toggle
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
    const op = state.op;
    const kind = op && op.kind;
    // done is terminal: close the source (EventSource would auto-reconnect
    // and replay the history otherwise) and any open modal (covers
    // notify-only decisions whose op already returned).
    if (op) op.es.close();
    state.op = null;
    $("pull-btn").disabled = false;
    $("push-btn").disabled = false;
    hideModal();
    // A run with its own done handler (the review lane) owns the outcome
    // entirely — it changes nothing in the repo, so none of the refreshing
    // below applies to it.
    if (op && op.onDone) {
      op.onDone(ev, kind);
      return;
    }
    if (ev.ok && (kind === "commit" || kind === "stash")) $("commit-msg").value = "";
    if (ev.ok) opLine(ev.summary || "done");
    // changed && !ok is the engine's deliberate success-with-conflicts shape
    // (a chosen keep-conflicts on merge/rebase/pull/apply-patch/stash-pop):
    // conflicts were left in the tree on purpose, not a failure — the
    // summary already reads as "…has conflicts (left in tree)" etc.
    else if (ev.changed) opLine(ev.summary || "left conflicts in the working tree — resolve them, then commit");
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
  // a status re-read can invalidate an open hunk view (file fully staged or
  // gone): exit rather than offer stale positional picks
  if (diffHunks && !state.statusEntries.some((f) => f.path === diffHunks.path && hunkEligible(f))) clearDiffHunks();
  if (conflictPick && !state.statusEntries.some((f) => f.path === conflictPick.path && f.section === "conflicts")) {
    conflictPick = null;
    renderResolveBar();
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
  $("modal").dataset.opts = JSON.stringify(ev.options || []);
  pushLayer("modal", $("modal"), {
    onKey: (e) => {
      if (e.key === "Escape") {
        const opts = JSON.parse($("modal").dataset.opts || "[]");
        if (opts.includes("abort")) answerModal("abort"); // the TUI's esc rule
      }
      e.preventDefault();
      return true; // the modal owns the keyboard — even over a focused form field
    },
  });
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
  modalLocalCb = null; // a done-driven close must not leak the callback to the next modal
  closeLayer("modal");
}

function openHelp() {
  pushLayer("help", $("help"), {
    onKey: (e) => {
      if (e.key === "Escape" || e.key === "?") closeLayer("help");
      e.preventDefault();
      return true; // help owns the keyboard until closed
    },
  });
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
  closeLayer("ctx");
  // Emptied, not just hidden: a closed menu that still holds its last rows
  // reads as open to anything inspecting the DOM — which is exactly how a
  // browser check of the versions popup passed while the menu was in fact
  // being closed in the same event it opened.
  const menu = $("ctx-menu");
  menu.innerHTML = "";
  menu._items = null;
}

// showCtxMenu renders the shared right-click menu at (x,y): safe actions
// first; rows flagged danger render red. A row with sep:true renders as a
// non-clickable divider (it occupies an index in _items, which the click
// handler resolves by data-i, so alignment is preserved).
function showCtxMenu(items, x, y) {
  const menu = $("ctx-menu");
  menu._items = items;
  menu.innerHTML = items
    .map((it, i) => (it.sep ? `<div class="sep"></div>` : `<button data-i="${i}"${it.danger ? ' class="danger"' : ""}>${esc(it.label)}</button>`))
    .join("");
  menu.style.left = Math.min(x, window.innerWidth - 200) + "px";
  menu.style.top = Math.min(y, window.innerHeight - 120) + "px";
  pushLayer("ctx", menu, {
    onKey: (e) => {
      if (e.key === "Escape") closeLayer("ctx");
      return true; // swallowed without preventDefault (today's behavior)
    },
  });
}

// A branch is checked out in at most one worktree, and both lists are
// already in hand (the sidebar fetches them together), so the join is a
// client-side lookup — same shape as the TUI's worktreeAbsPathForBranch.
// No match (the common case) simply omits the row.
function worktreePathForBranch(name) {
  const w = state.worktrees.find((x) => x.branch === name);
  return w ? w.path : null;
}

// A starting point for the worktree-path prompt, not a decision: a sibling of
// the MAIN worktree named <repo>-<branch>. Anchoring on the main worktree
// (git lists it first) rather than the served one keeps new worktrees from
// nesting inside each other when you create one while inside another — the
// same anchor the TUI's template resolver uses. Slashes in a branch name
// become dashes so `feat/x` does not imply a directory.
function defaultWorktreePath(branch) {
  const main = (state.worktrees[0] && state.worktrees[0].path) || (state.repo && state.repo.worktree) || "";
  if (!main) return "";
  const sep = main.includes("\\") && !main.includes("/") ? "\\" : "/";
  const cut = main.lastIndexOf(sep);
  const parent = cut > 0 ? main.slice(0, cut) : main;
  const name = cut >= 0 ? main.slice(cut + 1) : main;
  return parent + sep + name + "-" + branch.replace(/[^\w.-]+/g, "-");
}

// --- branch versions (the operations history) ---
//
// Every destructive operation snapshots the branch tip before it runs, so
// this list is "what this branch pointed at before each of the last N
// operations" — the undo of last resort. Restoring is itself snapshotted, so
// nothing here is a one-way door.

// versionWhen renders a snapshot's age. Absolute dates read as history;
// what you actually want to know is "how far back do I have to go".
function versionWhen(unix) {
  const secs = Math.max(0, Math.floor(Date.now() / 1000) - unix);
  const units = [
    [86400 * 7, "w"],
    [86400, "d"],
    [3600, "h"],
    [60, "m"],
  ];
  for (const [size, tag] of units) {
    if (secs >= size) return Math.floor(secs / size) + tag + " ago";
  }
  return "just now";
}

// ---- interactive rebase: the plan editor ---------------------------------
//
// ORDER. The wire is git todo order (OLDEST first), both directions. The
// editor shows newest-first — the commit list's order, and the TUI editor's —
// and reverses at exactly two points: when the range arrives, and when the
// plan is sent. Nothing in between flips it.
//
// The client may reorder and annotate; the server rebuilds and revalidates the
// plan against a fresh range, so a stale editor is refused (409) rather than
// applied to a history that moved.
let reb = null; // {branch, onto, rows:[{sha,short,subject,message,action,newMsg}], msgRow, prevAction}

const REBASE_ACTIONS = ["pick", "reword", "squash", "drop"];

async function openRebaseEditor(branch, onto) {
  if (opBusy()) return;
  let body;
  try {
    body = await getJSON(
      "/api/rebase-range?branch=" + encodeURIComponent(branch) + "&onto=" + encodeURIComponent(onto)
    );
  } catch (e) {
    opLine("interactive rebase: " + (e.message || e), true);
    return;
  }
  const commits = body.commits || [];
  if (!commits.length) {
    opLine(branch + " has no commits that " + onto + " does not — nothing to rebase");
    return;
  }
  reb = {
    branch,
    onto,
    // reverse: wire is oldest-first, the editor shows newest-first
    rows: commits
      .slice()
      .reverse()
      .map((c) => ({ ...c, action: "pick", newMsg: "" })),
    msgRow: -1,
    prevAction: "pick",
  };
  reb.orig = reb.rows.map((r) => ({ ...r }));
  renderRebase();
  pushLayer("rebase", $("rebase"), {
    onKey: (e) => {
      if (reb && reb.msgRow >= 0) return false; // the textarea owns its keys
      if (e.key === "Escape") {
        closeRebaseEditor();
        return true;
      }
      return false;
    },
  });
}

function closeRebaseEditor() {
  reb = null;
  closeLayer("rebase");
}

// oldestIndex is the last DISPLAY row — the one with nothing older to squash
// into. Kept as a function so the rule follows the rows when they are moved.
function oldestIndex() {
  return reb ? reb.rows.length - 1 : -1;
}

function renderRebase() {
  if (!reb) return;
  $("rebase-title").textContent = "interactive rebase: " + reb.branch + " onto " + reb.onto;
  const last = oldestIndex();
  $("rebase-list").innerHTML = reb.rows
    .map((r, i) => {
      const opts = REBASE_ACTIONS.map(
        (a) =>
          `<option value="${a}"${a === r.action ? " selected" : ""}` +
          // the oldest row has nothing older to meld into
          `${a === "squash" && i === last ? " disabled" : ""}>${a}</option>`
      ).join("");
      const note = r.action === "reword" && r.newMsg ? `<span class="rb-note">(reworded)</span>` : "";
      return (
        `<li data-i="${i}" class="${r.action === "drop" ? "drop" : ""}">` +
        `<select class="rb-action" data-i="${i}">${opts}</select>` +
        `<button class="rb-move" data-i="${i}" data-dir="-1" title="move up (newer)"${i === 0 ? " disabled" : ""}>↑</button>` +
        `<button class="rb-move" data-i="${i}" data-dir="1" title="move down (older)"${i === last ? " disabled" : ""}>↓</button>` +
        `<span class="rb-short">${esc(r.short)}</span>` +
        `<span class="rb-subj">${esc(r.subject)}</span>${note}</li>`
      );
    })
    .join("");
  const msg = $("rebase-msg");
  if (reb.msgRow >= 0) {
    const r = reb.rows[reb.msgRow];
    $("rebase-msg-title").textContent = "message for " + r.short;
    msg.classList.remove("hidden");
  } else {
    msg.classList.add("hidden");
  }
}

$("rebase-list").addEventListener("change", (e) => {
  const sel = e.target.closest("select.rb-action");
  if (!sel || !reb) return;
  const i = Number(sel.dataset.i);
  const want = sel.value;
  if (want === "reword") {
    // Choosing reword opens the message editor at once: a reword with no new
    // message is refused by the server, so the two are one decision.
    reb.prevAction = reb.rows[i].action;
    reb.rows[i].action = "reword";
    reb.msgRow = i;
    renderRebase();
    $("rebase-msg-text").value = reb.rows[i].newMsg || reb.rows[i].message || "";
    $("rebase-msg-text").focus();
    return;
  }
  reb.rows[i].action = want;
  if (want !== "reword") reb.rows[i].newMsg = "";
  renderRebase();
});

$("rebase-list").addEventListener("click", (e) => {
  const btn = e.target.closest("button.rb-move");
  if (!btn || !reb) return;
  const i = Number(btn.dataset.i);
  const j = i + Number(btn.dataset.dir);
  if (j < 0 || j >= reb.rows.length) return;
  const rows = reb.rows;
  [rows[i], rows[j]] = [rows[j], rows[i]];
  // A row moved to the oldest slot cannot stay a squash.
  if (rows[oldestIndex()].action === "squash") rows[oldestIndex()].action = "pick";
  renderRebase();
  const moved = $("rebase-list").querySelector(`li[data-i="${j}"]`);
  if (moved) moved.classList.add("moved");
});

$("rebase-msg-ok").addEventListener("click", () => {
  if (!reb || reb.msgRow < 0) return;
  const text = $("rebase-msg-text").value.trim();
  if (!text) {
    opLine("a reword needs a message", true);
    return;
  }
  reb.rows[reb.msgRow].newMsg = text + "\n";
  reb.msgRow = -1;
  renderRebase();
});

$("rebase-msg-cancel").addEventListener("click", () => {
  if (!reb || reb.msgRow < 0) return;
  // Cancelling the message cancels the reword itself — a reword with no
  // message would only be refused by the server.
  if (!reb.rows[reb.msgRow].newMsg) reb.rows[reb.msgRow].action = reb.prevAction || "pick";
  reb.msgRow = -1;
  renderRebase();
});

$("rebase-reset").addEventListener("click", () => {
  if (!reb) return;
  reb.rows = reb.orig.map((r) => ({ ...r }));
  reb.msgRow = -1;
  renderRebase();
});

$("rebase-cancel").addEventListener("click", closeRebaseEditor);
$("rebase").addEventListener("click", (e) => {
  if (e.target.id === "rebase") closeRebaseEditor(); // backdrop: nothing has run
});

$("rebase-start").addEventListener("click", () => {
  if (!reb) return;
  if (reb.msgRow >= 0) {
    opLine("finish the message first", true);
    return;
  }
  if (reb.rows.every((r) => r.action === "drop")) {
    opLine("that plan drops every commit — cancel instead if that is what you meant", true);
    return;
  }
  // reverse back to git todo order (oldest first)
  const plan = reb.rows
    .slice()
    .reverse()
    .map((r) => ({ sha: r.sha, action: r.action, msg: r.action === "reword" ? r.newMsg : "" }));
  // Capture before closing: closeRebaseEditor clears reb.
  const branch = reb.branch,
    onto = reb.onto;
  const label = "rebasing " + branch + " onto " + onto;
  closeRebaseEditor();
  startOp({ op: "interactive-rebase", branch, onto, plan }, label);
});

async function openVersions(branch, opts) {
  let body;
  try {
    body = await getJSON("/api/versions?branch=" + encodeURIComponent(branch));
  } catch (e) {
    opLine("versions failed: " + (e.message || e), true);
    return;
  }
  const rows = body.versions || [];
  // The deleted tag matters here: it says the restore row will RECREATE the
  // branch, and why there is no compare row (nothing live to compare with).
  $("versions-title").textContent =
    branch + " — previous versions" + (opts && opts.deleted ? " (branch deleted)" : "");
  $("versions-list").innerHTML = rows.length
    ? rows
        .map(
          (v, i) =>
            `<li data-i="${i}"><span class="vwhen">${esc(versionWhen(v.unix))}</span>` +
            `<span class="vop">${esc(v.op)}</span>` +
            `<span class="vsha">${esc(v.short)}</span>` +
            `<span class="vsub">${esc(v.subject)}</span></li>`
        )
        .join("")
    : `<li class="empty">no recorded versions — they are written as operations run</li>`;
  $("versions-list")._rows = rows;
  $("versions-list")._branch = branch;
  pushLayer("versions", $("versions"));
}

function closeVersions() {
  closeLayer("versions");
}

// The live tip hash of a local branch, or "" when no such branch exists.
// This is the version-compare gate: a deleted branch has no tip to compare
// against (the TUI's "restore it to compare" rule). The value is git's
// abbreviated sha (what /api/branches serves) — fine for the rev form,
// which takes any plain hex id: it stays immutable for the object, and an
// ambiguous abbreviation fails loudly server-side.
function branchTipHash(name) {
  const b = (state.branches || []).find((x) => x.name === name);
  return b ? b.hash : "";
}

// Per-row actions go through the shared ctx-menu rather than inline buttons:
// same interaction language as the sidebar, and the row stays readable.
function showVersionMenu(branch, v, x, y) {
  const items = [];
  const tip = branchTipHash(branch);
  if (tip) {
    items.push({
      label: "compare against current tip",
      act: () => {
        closeVersions();
        closeVersionBranches(); // reached via the picker: the compare replaces it
        openCompare(v.hash, tip, {
          revs: true,
          aLabel: branch + "@" + v.short,
          bLabel: branch + " (tip)",
        });
      },
    });
  }
  showCtxMenu(
    items.concat([
      {
        label: "restore " + branch + " to this version",
        act: () =>
          // The engine only asks when the CURRENT branch is dirty; every
          // other lane moves the ref with no prompt at all. So the confirm
          // is the client's, as with delete-tag and discard.
          showLocalConfirm(
            "Move " + branch + " back to " + v.short + " (" + v.subject + ")?",
            ["restore", "abort"],
            (o) => {
              if (o !== "restore") return;
              closeVersions();
              closeVersionBranches(); // reached via the picker: its rows are now stale
              startOp(
                { op: "restore-version", branch, ref: v.ref },
                "restoring " + branch + " to " + v.short
              );
            }
          ),
      },
      { label: "copy commit id", act: () => copyText(v.hash, "commit id " + v.short) },
      {
        label: "delete this snapshot",
        danger: true,
        act: () =>
          showLocalConfirm("Delete the " + v.op + " snapshot at " + v.short + "?", ["delete", "abort"], (o) => {
            if (o !== "delete") return;
            closeVersions();
            closeVersionBranches(); // reached via the picker: its rows are now stale
            startOp({ op: "delete-version", ref: v.ref }, "deleting snapshot " + v.short);
          }),
      },
    ]),
    x,
    y
  );
}

// Both buttons open the row menu. LEFT click must stopPropagation: the
// document-level outside-click closer would otherwise see this very click
// bubble up and shut the menu in the same event, so the row would look dead
// (the ☰ button's lesson). RIGHT click must preventDefault or the browser's
// own context menu covers ours.
function versionRowMenu(e) {
  const li = e.target.closest("li[data-i]");
  if (!li) return;
  e.preventDefault();
  e.stopPropagation();
  const list = $("versions-list");
  const v = list._rows[Number(li.dataset.i)];
  if (v) showVersionMenu(list._branch, v, e.clientX, e.clientY);
}
$("versions-list").addEventListener("click", versionRowMenu);
$("versions-list").addEventListener("contextmenu", versionRowMenu);
$("versions").addEventListener("click", (e) => {
  if (e.target.id === "versions") closeVersions(); // backdrop
});

// --- all-branches versions picker (the deleted-branch recovery path) ---
//
// Lists every branch with recorded versions, deleted ones tagged — the only
// route to a DELETED branch's snapshots, whose restore recreates the ref.
// Clicking a row opens the versions overlay ON TOP (it sits at z-index 21,
// this picker at 20), so esc drills back out to the picker for free.
async function openVersionBranches() {
  let body;
  try {
    body = await getJSON("/api/version-branches");
  } catch (e) {
    opLine("branch versions failed: " + (e.message || e), true);
    return;
  }
  const rows = body.branches || [];
  $("vbranches-list").innerHTML = rows.length
    ? rows
        .map(
          (b, i) =>
            `<li data-i="${i}"><span class="vbname">${esc(b.branch)}</span>` +
            (b.deleted ? `<span class="vbdel">deleted</span>` : "") +
            `<span class="vbcount">${b.count} snapshot${b.count === 1 ? "" : "s"}</span>` +
            `<span class="vbwhen">${esc(versionWhen(b.latest_unix))}</span></li>`
        )
        .join("")
    : `<li class="empty">no recorded versions anywhere — they are written as operations run</li>`;
  $("vbranches-list")._rows = rows;
  pushLayer("vbranches", $("vbranches"));
}

function closeVersionBranches() {
  closeLayer("vbranches");
}

$("vbranches-list").addEventListener("click", (e) => {
  const li = e.target.closest("li[data-i]");
  if (!li) return;
  const b = $("vbranches-list")._rows[Number(li.dataset.i)];
  if (b) openVersions(b.branch, { deleted: b.deleted });
});
$("vbranches").addEventListener("click", (e) => {
  if (e.target.id === "vbranches") closeVersionBranches(); // backdrop
});

// --- AI review ---
//
// One overlay walks the whole lane — choose a tool, approve its command, wait
// — because the three are steps of one decision, not three surfaces. The
// command text is never sent from here: the server looks it up in the config
// and resolves it, and this only ever names a TOOL. What is shown in the
// approval step is what the server said it would run.

let rev = null; // {target, branch, tools, sel, phase, tool, label, mode}

function reviewTitle() {
  if (!rev) return "";
  if (rev.mode === "conflict") return "AI resolve — " + (rev.label || rev.op || "");
  return "Review " + (rev.label || (rev.target === "working" ? "working changes" : rev.branch)) + " (AI)";
}

async function startReview(target, branch) {
  if (rev) {
    // Parked: the overlay is not on screen, so a silent refusal here looks
    // like the menu row does nothing. The parked run may be the OTHER mode
    // (an AI resolve), so name it correctly rather than always saying
    // "review".
    if (rev.parked) opLine((rev.mode === "conflict" ? "an agent" : "a review") + " is already running in the background — open the chip to watch or cancel it", true);
    return;
  }
  if (state.op) {
    // One lane, and the server would 409 anyway — but a menu row that does
    // nothing at all reads as broken.
    opLine("review: an operation is already running", true);
    return;
  }
  let info;
  try {
    const q = "?target=" + encodeURIComponent(target) + (branch ? "&branch=" + encodeURIComponent(branch) : "");
    info = await getJSON("/api/review/tools" + q);
  } catch (e) {
    opLine("review: " + (e.message || e), true);
    return;
  }
  const tools = info.tools || [];
  if (!tools.length) {
    // Nothing to run and nothing this UI can do about it — say where it comes
    // from rather than opening an empty chooser.
    opLine('review: no review tool configured — add a [[tools.command]] block with category = "review"', true);
    return;
  }
  rev = { mode: "review", target, branch, label: info.label, tools, sel: 0, phase: "choose", tool: null };
  pushLayer("review", $("review"), { onKey: reviewKey });
  if (tools.length === 1) reviewPick(tools[0]);
  else renderReview();
}

// startConflictAI is the conflict-mode twin of startReview above: same
// overlay, same lane, but sourced from the paused-op tools endpoint and
// dispatched to /api/conflict/complete instead of /api/review.
async function startConflictAI() {
  if (rev) {
    if (rev.parked) opLine("an agent is already running in the background — open the chip to watch or cancel it", true);
    return;
  }
  if (state.op) {
    opLine("AI resolve: an operation is already running", true);
    return;
  }
  let info;
  try {
    info = await getJSON("/api/conflict/tools");
  } catch (e) {
    opLine("AI resolve: " + (e.message || e), true);
    return;
  }
  const tools = info.tools || [];
  if (!tools.length) {
    opLine('AI resolve: no headless conflict agent configured — add a [[tools.command]] block with category = "conflict_complete", mode = "capture", frontends = ["web"]', true);
    return;
  }
  rev = { mode: "conflict", op: info.op, label: info.desc || info.op, tools, sel: 0, phase: "choose", tool: null };
  pushLayer("review", $("review"), { onKey: reviewKey });
  if (tools.length === 1) reviewPick(tools[0]);
  else renderReview();
}
$("conflict-ai").addEventListener("click", startConflictAI);

function reviewPick(tool) {
  if (!rev) return;
  rev.tool = tool;
  // The server decides whether an approval is needed; this only skips the
  // step it already told us is unnecessary. An out-of-date "approved" here
  // costs one extra prompt, never an unapproved run.
  if (tool.approved) reviewRun(false);
  else {
    rev.phase = "approve";
    renderReview();
  }
}

async function reviewRun(approve) {
  if (!rev) return;
  const { target, branch, tool } = rev;
  const isConflict = rev.mode === "conflict";
  rev.phase = "running";
  renderReview();
  let resp;
  try {
    resp = await postJSON(
      isConflict ? "/api/conflict/complete" : "/api/review",
      isConflict ? { tool: tool.name, approve: !!approve } : { target, branch, tool: tool.name, approve: !!approve }
    );
  } catch (e) {
    // Most often a 403: the server does not consider this command approved,
    // whatever the tools list said. Fall back to the approval step (the
    // resolved command is already in hand from that list) and show why.
    if (!rev) return;
    rev.phase = "approve";
    renderReview();
    opLine((isConflict ? "AI resolve: " : "review: ") + (e.message || e), true);
    return;
  }
  if (!rev) {
    // Cancelled while the start request was in flight. The run EXISTS —
    // simply returning would leave an agent running with nobody following
    // it, holding the single lane until it finished on its own.
    postJSON("/api/op/" + resp.op_id + "/cancel", {}).catch(() => {});
    opLine(isConflict ? "AI resolve cancelled" : "review cancelled");
    return;
  }
  rev.opID = resp.op_id;
  renderReview();
  followOp(resp.op_id,
    (isConflict ? "AI resolving " : "reviewing ") + (rev.label || ""),
    isConflict ? "conflict_complete" : "review",
    reviewDone);
}

function reviewDone(ev, kind) {
  // kind is the op's own kind ("conflict_complete" | "review"), threaded in
  // from handleOpEvent/state.op — NOT derived from rev.mode. reviewCancel()
  // closes the lane (nulling rev) BEFORE its cancel round-trip completes, so
  // the eventual done event for that cancel arrives with rev already gone;
  // deriving the conflict/review distinction from rev here would silently
  // fall back to "review" on every cancelled (or lost-connection) run,
  // skipping the conflict-only refreshAfterOp() below and misreporting the
  // outcome. rev/state.task are read only for DISPLAY (title, label, tool
  // name) below and degrade gracefully once the lane is gone.
  const isConflict = kind === "conflict_complete";
  const title = isConflict
    ? "Resolution overview — " + ((rev && rev.tool && rev.tool.name) || "agent")
    : reviewTitle() || "Review";
  const parked = !!(rev && rev.parked);
  const label = (state.task && state.task.label) || (rev && rev.label) || "";
  closeReviewLane();
  // A conflict run mutates the repo (or leaves it paused) whether it
  // finished, was cancelled, or was lost — reality first, before anything
  // else in this function decides what to tell the user.
  if (isConflict) refreshAfterOp();
  if (parked) {
    // The whole point of parking is not being interrupted, so the result
    // WAITS: the chip goes loud, and opening it is the user's move.
    state.task = {
      kind: isConflict ? "conflict" : "review",
      label,
      status: ev.ok ? "done" : ev.cancelled ? "cancelled" : "failed",
      title,
      path: ev.path,
      report: ev.report,
      error: ev.error,
    };
    if (state.task.status === "cancelled") state.task = null; // nothing to collect
    renderTaskChip(true);
    // The banner belongs to the RUN, so it goes when the run does — the chip
    // is what announces the result, and a second, permanent status line
    // saying the same thing is clutter you have to dismiss by hand.
    // A failure still speaks: an error that vanished silently would be worse
    // than one line of clutter.
    if (ev.ok) hideOpLine();
    else opLine((isConflict ? "AI resolve failed: " : "review failed: ") + (ev.error || "unknown error"), true);
    return;
  }
  if (ev.ok) {
    if (isConflict) {
      // The agent may have finished its own work but left the sequencer
      // paused (a partial resolve, or a multi-round rebase it didn't
      // finish) — that is not a failure, but it is not "done" either.
      if (ev.still_paused) opLine("the agent left the " + (ev.op || "operation") + " paused — finish it manually or run another agent", true);
      else opLine((ev.op || "operation") + " completed");
      if (ev.report) openReport(title, "", ev.report);
      else if (!ev.still_paused) opLine((ev.op || "operation") + " completed — the agent reported no overview");
      return;
    }
    openReport(title, ev.path, ev.report);
    opLine(ev.summary || "review done");
    return;
  }
  if (ev.cancelled) opLine(isConflict ? "AI resolve cancelled" : "review cancelled");
  else opLine((isConflict ? "AI resolve failed: " : "review failed: ") + (ev.error || "unknown error"), true);
}

// --- parking a running review ---
//
// A review can take minutes and there is nothing to watch while it does.
// Parking hides the overlay and leaves the run streaming into the chip. It
// does NOT free the operation lane — the run still holds it, so another op
// is still refused, but every read (commits, diffs, files, branches) stays
// available, which is what "let me get on with something else" needs.

function parkReview() {
  if (!rev || rev.phase !== "running") return;
  rev.parked = true;
  const kind = rev.mode === "conflict" ? "conflict" : "review";
  state.task = { kind, label: rev.label || "", status: "running" };
  closeLayer("review");
  renderTaskChip(false);
  taskLine((kind === "conflict" ? "AI resolve" : "review") + " running in the background — click here to watch or cancel it");
}

function unparkReview() {
  if (!rev || !rev.parked) return;
  rev.parked = false;
  state.task = null;
  renderTaskChip(false);
  // The lane is back on screen, so its status line has nothing left to say.
  if ($("op-line").classList.contains("task")) hideOpLine();
  pushLayer("review", $("review"), { onKey: reviewKey });
  renderReview();
}

// renderTaskChip paints state.task. blink is passed only on the transition
// into a finished state, so a re-render (or a second tab's refresh) does not
// restart the animation.
function renderTaskChip(blink) {
  const el = $("task-chip");
  const t = state.task;
  el.classList.remove("running", "ready", "failed", "blink");
  if (!t) {
    el.classList.add("hidden");
    return;
  }
  const noun = t.kind === "conflict" ? "AI resolve" : "review";
  el.classList.remove("hidden");
  if (t.status === "running") {
    el.textContent = "⟳ " + noun + (t.label ? ": " + t.label : "");
    el.title = noun + " running in the background — click to watch or cancel it";
    el.classList.add("running");
    return;
  }
  if (t.status === "done") {
    el.textContent = "✓ " + noun + (t.kind === "conflict" ? " done" : " ready");
    el.title = t.kind === "conflict" ? "click to read the overview" : "click to read the report";
    el.classList.add("ready");
  } else {
    el.textContent = "✗ " + noun + " failed";
    el.title = "click for the error";
    el.classList.add("failed");
  }
  if (blink) el.classList.add("blink");
}

function collectTask() {
  const t = state.task;
  if (!t) return;
  if (t.status === "running") {
    unparkReview();
    return;
  }
  const noun = t.kind === "conflict" ? "AI resolve" : "review";
  state.task = null;
  renderTaskChip(false);
  if (t.status === "done") openReport(t.title || "Review", t.path, t.report);
  else opLine(noun + " failed: " + (t.error || "unknown error"), true);
}

$("task-chip").addEventListener("click", collectTask);
$("review-park").addEventListener("click", parkReview);

// Cancelling mid-run is not a nicety: an agent can take minutes, it holds the
// single op lane while it does, and the tab must not be a hostage to it.
async function reviewCancel() {
  const id = rev && rev.opID;
  closeReviewLane();
  if (!id) return;
  try {
    await postJSON("/api/op/" + id + "/cancel", {});
  } catch (e) {
    opLine("cancel: " + (e.message || e), true);
  }
}

function closeReviewLane() {
  rev = null;
  closeLayer("review");
  // A RUNNING parked task has just stopped being a thing (cancelled, or the
  // stream was lost) — its chip must not linger as a spinner nobody is
  // driving. A finished one is re-set by reviewDone right after this.
  if (state.task && state.task.status === "running") {
    state.task = null;
    renderTaskChip(false);
  }
}

function renderReview() {
  if (!rev) return;
  $("review-title").textContent = reviewTitle();
  const body = $("review-body");
  const hint = $("review-hint");
  const runBtn = $("review-run");
  const cancelBtn = $("review-cancel");
  const parkBtn = $("review-park");
  parkBtn.classList.toggle("hidden", rev.phase !== "running"); // nothing to background before it starts
  if (rev.phase === "choose") {
    body.innerHTML =
      "<ul>" +
      rev.tools
        .map(
          (t, i) =>
            `<li data-i="${i}"${i === rev.sel ? ' class="sel"' : ""}>${esc(t.name)}` +
            `<span class="detail">${esc(t.command)}</span></li>`
        )
        .join("") +
      "</ul>";
    hint.textContent = rev.mode === "conflict" ? "choose an agent · enter runs · esc cancels" : "choose a review tool · enter runs · esc cancels";
    runBtn.classList.remove("hidden");
    runBtn.textContent = "run";
    cancelBtn.textContent = "cancel";
    return;
  }
  if (rev.phase === "approve") {
    body.innerHTML =
      `<div class="rcmd">${esc(rev.tool.command)}</div>` +
      `<div class="rnote">This runs on your machine with your permissions. Approval is remembered for this repo until the command text changes.</div>`;
    hint.textContent = "run this command?";
    runBtn.classList.remove("hidden");
    runBtn.textContent = "run";
    cancelBtn.textContent = "cancel";
    return;
  }
  body.innerHTML =
    `<div class="rnote">${esc(rev.tool ? rev.tool.name : "")} ` +
    (rev.mode === "conflict"
      ? "is resolving the conflicts and completing the operation — this can take a few minutes."
      : "is reading the diff — this can take a few minutes.") +
    ` You can put it in the background and carry on reading the repo; the chip in the top bar lights up when it finishes.</div>`;
  hint.textContent = rev.mode === "conflict" ? "⟳ resolving… · esc backgrounds it" : "⟳ reviewing… · esc backgrounds it";
  runBtn.classList.add("hidden"); // nothing to run twice
  cancelBtn.textContent = "cancel the run";
}

function reviewKey(e) {
  if (!rev) return false;
  if (e.key === "Escape") {
    // esc puts a live run in the BACKGROUND rather than killing it: esc means
    // "off my screen" everywhere else in this UI, and destroying minutes of
    // agent work on the key people press reflexively would be a trap.
    // Cancelling stays an explicit, labelled button.
    if (rev.phase === "running") parkReview();
    else closeReviewLane();
    e.preventDefault();
    return true;
  }
  if (rev.phase === "choose" && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
    rev.sel = Math.min(rev.tools.length - 1, Math.max(0, rev.sel + (e.key === "ArrowDown" ? 1 : -1)));
    renderReview();
    e.preventDefault();
    return true;
  }
  if (e.key === "Enter") {
    reviewConfirm();
    e.preventDefault();
    return true;
  }
  return true; // the lane owns the keyboard while it is open
}

function reviewConfirm() {
  if (!rev) return;
  if (rev.phase === "choose") reviewPick(rev.tools[rev.sel]);
  else if (rev.phase === "approve") reviewRun(true);
}

$("review-run").addEventListener("click", reviewConfirm);
$("review-cancel").addEventListener("click", () => {
  if (rev && rev.phase === "running") reviewCancel();
  else closeReviewLane();
});
$("review-body").addEventListener("click", (e) => {
  const li = e.target.closest("li[data-i]");
  if (li && rev && rev.phase === "choose") reviewPick(rev.tools[Number(li.dataset.i)]);
});
$("review").addEventListener("click", (e) => {
  // Backdrop dismisses. While a run is live that means BACKGROUND it, never
  // kill it — a stray click outside the box must not destroy an agent's work.
  if (e.target.id !== "review" || !rev) return;
  if (rev.phase === "running") parkReview();
  else closeReviewLane();
});

// The report viewer: plain text, deliberately not rendered as markdown — a
// review is prose to read, and a parser here would be a dependency and a
// rendering bug surface for no gain.
function openReport(title, path, content) {
  $("report-title").textContent = title;
  $("report-body").textContent = content || "";
  $("report-path").textContent = path || "";
  $("report-path").title = path || "";
  $("report-body").scrollTop = 0;
  pushLayer("report", $("report"));
}

$("report-close").addEventListener("click", () => closeLayer("report"));
$("report-copy").addEventListener("click", () => copyText($("report-body").textContent, "the report"));
// Deliberately NO backdrop-closes handler, unlike the picker overlays. A
// report is a document you read, and the chip that raised it is cleared the
// moment it opens — so a stray click outside the box used to discard the
// only copy in the UI. Double-clicking the ready chip did exactly that: the
// first click opened the report, the second landed on this backdrop.
// Closing stays explicit: the close button, or esc via the layer stack.

// The global create-branch entry (☰ / palette): same op as the branch
// menu's row, but with no start point on the wire — the server reads that as
// HEAD, which is what "new branch" means with nothing selected.
function openCreateBranchPrompt() {
  openPrompt({
    title: "New branch, starting at the current HEAD:",
    placeholder: "branch name",
    onSubmit: (name) => startOp({ op: "create-branch", name }, "creating " + name),
  });
}

function showBranchMenu(b, x, y) {
  const items = [{ label: "go to tip", act: () => gotoBranchTip(b) }];
  if (!b.is_head) items.push({ label: "switch to " + b.name, act: () => startSwitch(b.name) });
  if (b.is_head) {
    // The checked-out branch: pulling it rewrites the working tree, so it
    // goes through the confirming current-branch path the header button uses.
    items.push({ label: "pull " + b.name, act: () => doPull() });
    items.push({ label: "push " + b.name, act: () => doPush() });
  } else {
    items.push({ label: "pull " + b.name + " (stay here)", act: () => doPullBranch(b.name) });
    items.push({ label: "push " + b.name, act: () => doPushBranch(b.name) });
  }
  // Not marked danger: the row opens the force-mode modal, where the actual
  // destructive options are the red ones.
  items.push({ label: "force push " + b.name + "…", act: () => doForcePush(b.name) });
  items.push({
    label: "rename branch…",
    act: () =>
      openPrompt({
        title: "Rename " + b.name + " to:",
        value: b.name,
        onSubmit: (name) => {
          if (name === b.name) return; // no-op, and the engine would refuse it
          startOp({ op: "rename-branch", branch: b.name, name }, "renaming " + b.name);
        },
      }),
  });
  items.push({
    label: "create branch from here…",
    act: () =>
      openPrompt({
        title: "New branch, starting at " + b.name + ":",
        placeholder: "branch name",
        onSubmit: (name) => startOp({ op: "create-branch", name, branch: b.name }, "creating " + name),
      }),
  });
  // Only offered when the branch has no worktree — git allows exactly one,
  // and the engine would refuse a second (same gate as the copy-path row).
  if (!worktreePathForBranch(b.name)) {
    items.push({
      label: "create worktree for this branch…",
      act: () =>
        openPrompt({
          title: "New worktree for " + b.name + ", at path:",
          value: defaultWorktreePath(b.name),
          onSubmit: (path) => startOp({ op: "create-worktree", branch: b.name, path }, "creating worktree " + path),
        }),
    });
  }
  // Not gated on "does this branch have versions" — that would cost a read
  // on every menu open; the popup shows the empty state instead (the TUI's
  // branchVersionsRow rule).
  items.push({ label: "previous versions…", act: () => openVersions(b.name) });
  items.push({ label: "review " + b.name + " (AI)…", act: () => startReview("branch", b.name) });
  if (state.solo === b.name) {
    items.push({ label: "exit solo (show every branch)", act: () => setSolo("") });
  } else {
    items.push({ label: "solo this branch", act: () => setSolo(b.name) });
  }
  items.push({ label: "copy branch name", act: () => copyText(b.name, "branch name " + b.name) });
  // b.hash is git's abbreviated sha (%(objectname:short)) — the same value
  // the TUI's row copies, and short enough to name in full on the line.
  if (b.hash) items.push({ label: "copy commit id", act: () => copyText(b.hash, "commit id " + b.hash) });
  const wt = worktreePathForBranch(b.name);
  if (wt) {
    items.push({ label: "copy worktree absolute path", act: () => copyText(wt, "absolute path " + wt) });
  }
  // Destructive row last, as in the worktree and tag menus.
  if (!b.is_head) {
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

// Drag a branch onto another to merge or rebase. The drop opens the shared
// ctx-menu naming the pair in both directions — the menu row IS the
// confirmation, the same standing the TUI's pair-op popup has.
const branchesList = $("branches-list");

branchesList.addEventListener("dragstart", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  state.dragBranch = li.dataset.n;
  // Required for a drag to start at all in Firefox; also gives the browser
  // its default drag image.
  e.dataTransfer.setData("text/plain", li.dataset.n);
  e.dataTransfer.effectAllowed = "move";
});

branchesList.addEventListener("dragover", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  if (!state.dragBranch || li.dataset.n === state.dragBranch) return;
  // preventDefault is what marks this element as a valid drop target.
  e.preventDefault();
  e.dataTransfer.dropEffect = "move";
  li.classList.add("drop-target");
});

branchesList.addEventListener("dragleave", (e) => {
  const li = e.target.closest("li");
  if (li) li.classList.remove("drop-target");
});

branchesList.addEventListener("dragend", () => {
  state.dragBranch = null;
  clearDropTargets();
});

branchesList.addEventListener("drop", (e) => {
  const li = e.target.closest("li");
  const src = state.dragBranch;
  state.dragBranch = null;
  clearDropTargets();
  if (!li || !li.dataset.n || !src) return;
  const dst = li.dataset.n;
  if (dst === src) return;
  e.preventDefault();
  showBranchPairMenu(src, dst, e.clientX, e.clientY);
});

function clearDropTargets() {
  for (const el of $("branches-list").querySelectorAll(".drop-target")) {
    el.classList.remove("drop-target");
  }
}

// showBranchPairMenu offers the two-branch operations on (dragged, dropped-on).
// Directions are spelled out in the labels so the pair never carries implicit
// meaning: merge ends on dst, rebase rewrites and ends on src.
function showBranchPairMenu(src, dst, x, y) {
  showCtxMenu(
    [
      {
        label: "merge " + src + " into " + dst,
        act: () => startOp({ op: "merge", branch: src, onto: dst }, "merging " + src + " into " + dst),
      },
      {
        label: "rebase " + src + " onto " + dst,
        act: () => startOp({ op: "rebase", branch: src, onto: dst }, "rebasing " + src + " onto " + dst),
      },
      {
        // Opens an editor; nothing runs until you start it there, which is
        // why it sits with the ops rather than below the read-only row.
        label: "interactive rebase " + src + " onto " + dst + "…",
        act: () => openRebaseEditor(src, dst),
      },
      // Read-only, so it sits below the ops that rewrite history.
      { label: "compare " + src + " ↔ " + dst, act: () => openCompare(src, dst) },
    ],
    x,
    y
  );
}

// A clipboard write is otherwise silent — you cannot tell a success from a
// no-op without pasting. `what` names what landed (the TUI reports the same
// way); it defaults to the copied text.
function copyText(text, what) {
  navigator.clipboard.writeText(text).then(
    () => opLine("copied " + (what || text)),
    () => opLine("copy failed (clipboard unavailable)", true),
  );
}

function showWorktreeMenu(w, x, y) {
  const items = [{ label: "copy path", act: () => copyText(w.path) }];
  // Every row except the served worktree can be switched to (the same
  // exemption the remove row uses).
  if (!(state.worktree && w.path === state.worktree)) {
    items.unshift({ label: "switch here", act: () => doReroot(w.path) });
  }
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
// drillOut steps ONE stage back — diff → file list → full-width commit
// list. The esc key, the ← back button, and the footer chip all share it.
function drillOut() {
  if (state.layout === "diff") {
    enterFilesStage(); // also clears the diff a late fetch may repaint
    focusPane();
    return;
  }
  if (state.layout !== "files") return;
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

// --- pane resizing ---
// One drag handle per layout: sidebar↔commits in list mode, and the file
// list's edge in the files/diff stages. rs-sidebar resizes the FIRST grid
// column (width = pointer offset from the #panes left edge); rs-detail
// resizes the LAST — the file list sits on the right — so it carries
// `right: true` and measures from the right edge instead. Branch names
// ellipsize, so a fixed sidebar width left a long name unreadable with no
// recourse. Widths live as CSS custom properties on #panes and persist
// per handle.
const RESIZERS = {
  "rs-sidebar": { prop: "--sb-w", key: "gg.sidebar.width", def: 230 },
  "rs-detail": { prop: "--files-w", key: "gg.panes.files-width", def: 320, right: true },
};
const RS_MIN = 120; // narrower than this and the pane holds nothing readable
const RS_KEEP = 200; // always leave this much for the OTHER pane (the flexible column)

function setPaneWidth(cfg, w) {
  $("panes").style.setProperty(cfg.prop, w + "px");
}

// Clamp against the live #panes width so a drag can never squeeze either
// side to nothing — including on a window far narrower than the defaults,
// where the minimum wins over the keep-back. In the files stage BOTH fixed
// columns are on screen at once, so each handle's keep-back also reserves
// the other handle's column, or the flexible commits pane between them
// could be squeezed to zero.
function clampPaneWidth(cfg, w) {
  const total = $("panes").getBoundingClientRect().width;
  let reserve = RS_KEEP;
  if (state.layout === "files" && state.sidebar) {
    const other = Object.values(RESIZERS).find((c) => c !== cfg);
    if (other) reserve += other.want;
  }
  return Math.round(Math.min(Math.max(RS_MIN, total - reserve), Math.max(RS_MIN, w)));
}

// What the user asked for is stored and persisted; what the window can
// currently afford is what gets applied. Shrinking the window therefore
// squeezes the pane without forgetting the chosen width, and widening it
// again restores that width.
function applyPaneWidths() {
  Object.values(RESIZERS).forEach((cfg) => setPaneWidth(cfg, clampPaneWidth(cfg, cfg.want)));
}

function initResizer(id) {
  const cfg = RESIZERS[id];
  const el = $(id);
  const saved = parseInt(lsGet(cfg.key) || "", 10);
  cfg.want = Number.isFinite(saved) ? saved : cfg.def;

  el.addEventListener("pointerdown", (e) => {
    e.preventDefault(); // no text selection, no native drag
    const rect = $("panes").getBoundingClientRect();
    // Capture keeps the move/up events coming while the pointer is off the
    // 5px handle. It throws when the pointer id is not active (a synthetic
    // event), which must not abort the drag.
    try { el.setPointerCapture(e.pointerId); } catch {}
    el.classList.add("dragging");
    document.body.classList.add("resizing");
    const onMove = (ev) => {
      cfg.want = clampPaneWidth(cfg, cfg.right ? rect.right - ev.clientX : ev.clientX - rect.left);
      setPaneWidth(cfg, cfg.want);
    };
    const onUp = () => {
      el.classList.remove("dragging");
      document.body.classList.remove("resizing");
      el.removeEventListener("pointermove", onMove);
      el.removeEventListener("pointerup", onUp);
      el.removeEventListener("pointercancel", onUp);
      lsSet(cfg.key, String(cfg.want));
    };
    el.addEventListener("pointermove", onMove);
    el.addEventListener("pointerup", onUp);
    el.addEventListener("pointercancel", onUp);
  });

  el.addEventListener("dblclick", () => {
    cfg.want = cfg.def;
    setPaneWidth(cfg, clampPaneWidth(cfg, cfg.want));
    lsSet(cfg.key, String(cfg.want));
  });
}
Object.keys(RESIZERS).forEach(initResizer);
applyPaneWidths();
window.addEventListener("resize", applyPaneWidths);

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

// A partially-staged file appears twice: once under Changes (stage control),
// once under Staged (unstage control) — the git-status model.
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
  // Staged sits LAST: everything above it still wants attention, and what is
  // already staged does not. Staging a hunk otherwise made the file jump
  // upward into a section you were done with, pushing the rest of the work
  // down — the list appeared to reorder itself under the cursor.
  const order = { changes: 0, untracked: 1, conflicts: 2, staged: 3 };
  es.sort((a, b) => order[a.section] - order[b.section] || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
  state.statusEntries = es;
}

function wtRowHTML(i) {
  const sel = i === state.cursor ? " sel" : "";
  const c = state.wt.counts;
  // parts are pre-escaped HTML fragments (counts are numbers, labels are
  // literals) so the conflicted one can carry its highlight class.
  const parts = [];
  if (c.staged) parts.push(c.staged + " staged");
  if (c.unstaged) parts.push(c.unstaged + " changed");
  if (c.untracked) parts.push(c.untracked + " untracked");
  if (c.conflicted) parts.push(`<span class="conflict-count">${c.conflicted} conflicted</span>`);
  return (
    `<div class="crow wt${sel}" data-i="${i}">` +
    `<span class="graph">${flatDotSVG("#e0c06c")}</span>` +
    `<span class="subj">Working tree</span>` +
    `<span class="meta">${parts.join(" · ")}</span></div>`
  );
}

// --- commits pane (virtualized: only visible rows exist in the DOM) ---

function renderCommits() {
  const scroll = $("commits-scroll");
  const total = state.rows.length + wtCount();
  $("commits-spacer").style.height = total * ROW_H + wtExtra() + "px";
  const first = Math.max(0, Math.floor(scroll.scrollTop / ROW_H) - 10);
  const last = Math.min(total, Math.ceil((scroll.scrollTop + scroll.clientHeight) / ROW_H) + 10);
  const win = $("commits-window");
  win.style.top = first * ROW_H + (first > 0 ? wtExtra() : 0) + "px";
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
    // rows visually separate (full-height bars merged into one line).
    // Drawn as a ONE-CELL SVG with the graph's own geometry so its centre
    // lands exactly on the leftmost lane's centre; a text glyph would
    // centre wherever the font's advance width happens to put it.
    const col = runes(row.cells || "").indexOf("●");
    return flatDotSVG(laneColor(col >= 0 ? col >> 1 : 0));
  }
  return graphSVG(row, feedIdx);
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
  // The scope is server state (one feed for every tab), so it is reported by
  // the very response it scopes rather than tracked client-side. A reload or
  // a second tab therefore shows the chip without asking for it.
  setSoloChip(body.solo || "");
  renderCommits();
}

// --- one-line prompt ---
// A name or a path cannot come from a menu row, so this is the shared way to
// ask for one: a layer like any other (esc closes it, the top layer owns the
// keyboard). onSubmit receives the TRIMMED value and is never called with an
// empty string — every caller would have had to check.
let promptCb = null;

function openPrompt({ title, value, placeholder, onSubmit }) {
  promptCb = onSubmit;
  $("prompt-title").textContent = title;
  const input = $("prompt-input");
  input.value = value || "";
  input.placeholder = placeholder || "";
  pushLayer("prompt", $("prompt"), { onKey: promptKey });
  input.focus();
  input.select();
}

function closePrompt() {
  promptCb = null;
  // Blur before closing: the form-field guard keys off the focused element,
  // and a still-focused input would swallow every global key (the palette's
  // hard-won lesson).
  $("prompt-input").blur();
  closeLayer("prompt");
}

function submitPrompt() {
  const v = $("prompt-input").value.trim();
  if (!v) return; // nothing to submit; leave the prompt open
  const cb = promptCb; // capture before closing clears it
  closePrompt();
  if (cb) cb(v);
}

function promptKey(e) {
  if (e.key === "Enter") {
    e.preventDefault();
    submitPrompt();
    return true;
  }
  if (e.key === "Escape") {
    closePrompt();
    return true;
  }
  return false; // everything else is typing
}
$("prompt-ok").addEventListener("click", submitPrompt);

// --- solo mode ---
// Narrowing the commit list to one branch is a mode you can get stuck in, so
// the chip is not decoration: it is the exit. It renders from state.solo,
// which survives a failing /api/commits, and clicking it clears the scope.
function setSoloChip(branch) {
  state.solo = branch;
  const el = $("solo-chip");
  el.classList.toggle("hidden", !branch);
  if (branch) el.textContent = "solo: " + branch + " ✕";
}

async function setSolo(branch) {
  if (opBusy()) return;
  try {
    await postJSON("/api/solo", { branch });
    setSoloChip(branch);
    await loadCommits(false);
    moveCursor(0);
    opLine(branch ? "commit list scoped to " + branch : "showing every branch");
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

// --- files + diff panes ---

// Staged layout (the GitKraken flow): "list" = the commit list alone, full
// width; "files" = a commit is open — commits shrink left, the file list
// takes a fixed column on the right, NO diff yet; "diff" = a file is open —
// the diff replaces the commits area, file list stays right. esc steps one
// stage back (drillOut).
function setLayout(mode) {
  state.layout = mode;
  const p = $("panes");
  p.classList.toggle("solo", mode === "list");
  p.classList.toggle("files", mode === "files");
  p.classList.toggle("detail", mode === "diff");
  if (mode === "list") moveCursor(0); // re-render + rescroll: display:none dropped the scroll position
}

// enterFilesStage swaps to the file-list stage: browse the changed files
// with the commit list still alongside, nothing auto-opened. The diff pane
// is cleared so a later stage-3 entry never flashes the previous drill's
// diff, and a window resize cannot resurrect it through state.lastDiff.
function enterFilesStage() {
  state.pane = "files";
  setLayout("files");
  $("diff-title").textContent = "";
  $("diff-body").innerHTML = "";
  state.lastDiff = null;
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
  $("files-header").textContent = row.short + " " + row.subject;
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
  $("files-header").textContent = title;
  renderFiles();
  focusPane();
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
  $("files-header").textContent = "≡ " + st.ref;
  renderFiles();
  focusPane();
}

// --- branch ↔ branch comparison ---
//
// Opens the same detail screen a commit uses, over the whole tip-to-tip
// changed-file list. Each file's per-side diff runs against the two TIP
// HASHES the server resolved (never the branch names — see compare.go), so
// the diff cache cannot serve a stale side after a commit.
//
// The rev form (opts.revs): a and b are hex commit ids sent with revs=1 —
// no server-side branch resolution — and opts.aLabel/bLabel supply the
// display names, since a bare hash is unreadable in the header and the
// origin-filter buttons. state.compare.a/b hold the LABELS (display-only
// after the fetch; every git-facing consumer reads aHash/bHash).
async function openCompare(a, b, opts) {
  const o = opts || {};
  const gen = ++state.detailGen;
  let body;
  try {
    body = await getJSON(
      "/api/compare?a=" + encodeURIComponent(a) + "&b=" + encodeURIComponent(b) + (o.revs ? "&revs=1" : "")
    );
  } catch (e) {
    opLine("compare failed: " + (e.message || e), true);
    return;
  }
  if (gen !== state.detailGen) return; // superseded by a newer open or esc
  state.compare = {
    a: o.aLabel || a,
    b: o.bLabel || b,
    aHash: body.a_hash,
    bHash: body.b_hash,
    all: body.files || [],
    filter: "all",
    originsError: body.origins_error || "",
  };
  state.filesMode = "compare";
  state.fileSha = null;
  enterFilesStage();
  $("files-header").textContent = state.compare.a + " ↔ " + state.compare.b;
  applyCompareFilter();
  focusPane();
}

// The origin filter (the TUI's f key): "all", or only the files one side
// touched since the two diverged. A file both sides touched stays in both
// filtered views — the TUI's filterCompareFiles rule.
function applyCompareFilter() {
  const c = state.compare;
  state.files =
    c.filter === "all" ? c.all : c.all.filter((f) => f.origin === c.filter || f.origin === "both");
  state.fileCursor = 0;
  renderFiles();
  updateDiffNav();
  if (!state.files.length) {
    // The empty state must live in the FILE LIST: in the files stage the
    // diff pane is not on screen, and stepping back there from an open
    // diff must not strand a stale one.
    $("files-list").innerHTML = `<li class="sect">${
      c.all.length ? "no files match this filter" : "the two branches are identical"
    }</li>`;
    if (state.layout === "diff") drillOut();
    return;
  }
  // Filtering under an open diff keeps a diff open (the pre-staged-layout
  // behavior); in the files stage nothing auto-opens.
  if (state.layout === "diff") openFile(0);
}

function renderCompareBar() {
  const bar = $("compare-bar");
  const c = state.filesMode === "compare" ? state.compare : null;
  if (!c) {
    bar.classList.add("hidden");
    bar.innerHTML = "";
    return;
  }
  bar.classList.remove("hidden");
  // Without a merge base there are no origin sets, so only "all" is
  // meaningful — the comparison itself still stands (compare.go).
  const off = c.originsError
    ? ` disabled title="${esc(c.originsError)} — the per-side filter needs a merge base"`
    : "";
  const rows = [
    ["all", "all (" + c.all.length + ")", ""],
    ["a", "only " + c.a, off],
    ["b", "only " + c.b, off],
  ];
  bar.innerHTML = rows
    .map(
      ([key, label, attrs]) =>
        `<button data-f="${key}"${c.filter === key ? ' class="on"' : ""}${attrs}>${esc(label)}</button>`
    )
    .join("");
}

$("compare-bar").addEventListener("click", (e) => {
  const btn = e.target.closest("button[data-f]");
  if (!btn || !state.compare) return;
  state.compare.filter = btn.dataset.f;
  applyCompareFilter();
});

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
  enterFilesStage();
  $("files-header").textContent = "Working tree";
  renderFiles();
  focusPane();
}

const SECTION_LABELS = { staged: "Staged", changes: "Changes", untracked: "Untracked", conflicts: "Conflicts" };

function renderFiles() {
  // Driven off filesMode, not off state.compare, so the bar cannot linger
  // into the next commit's detail screen.
  renderCompareBar();
  if (state.filesMode !== "status") {
    $("files-actions").classList.add("hidden");
    $("commit-box").classList.add("hidden");
    $("conflict-note").classList.add("hidden");
    $("files-list").innerHTML = state.files
      .map(
        (f, i) =>
          `<li class="${i === state.fileCursor ? "sel" : ""}" data-i="${i}">` +
          `<span class="st ${esc(f.status)}">${esc(f.status)}</span>${esc(f.path)}</li>`
      )
      .join("");
    return;
  }
  // While a sequencer op is paused, the commit box AND the mass staging
  // buttons step aside: finishing the op is the banner's Continue (git
  // supplies the merge message), "stage all" would mark every conflict
  // resolved with the markers still inside the files, and "unstage all"
  // would pull git's auto-merged results back out of the coming merge
  // commit. Per-file actions (mark resolved) stay available.
  if (state.conflict) {
    $("files-actions").classList.add("hidden");
    $("commit-box").classList.add("hidden");
    const note = $("conflict-note");
    note.classList.remove("hidden");
    note.textContent = state.conflict.conflicted
      ? "resolving " + state.conflict.op + " — pick through the conflicts below, then press Continue above"
      : "all conflicts resolved — press Continue above to finish the " + state.conflict.op;
  } else {
    $("files-actions").classList.remove("hidden");
    $("conflict-note").classList.add("hidden");
    $("commit-box").classList.remove("hidden");
    $("commit-btn").disabled = !(state.wt && state.wt.counts.staged > 0) || !!state.op;
    $("stash-btn").disabled = !state.wt || !!state.op;
  }
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
  clearDiffHunks();
  // The layout switch sits in the SYNC prefix: an esc during a slow diff
  // load steps back to the files stage, and the fetch completing later
  // must not be able to undo that.
  if (state.layout !== "diff") {
    state.pane = "files";
    setLayout("diff");
    focusPane();
  }
  state.fileCursor = i;
  renderFiles();
  updateDiffNav();
  if (state.filesMode === "status") return openStatusDiff(i);
  const f = state.files[i];
  const q = new URLSearchParams({ path: f.path, status: f.status });
  if (state.filesMode === "compare") {
    q.set("left", state.compare.aHash);
    q.set("right", state.compare.bHash);
  } else {
    q.set("sha", f.sha || state.fileSha);
  }
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
  clearDiffHunks();
  const f = state.statusEntries[i];
  $("diff-title").textContent = f.path;
  if (f.section === "conflicts") return openConflictPicker(f);
  const q = new URLSearchParams({ wt: f.section === "staged" ? "staged" : "unstaged", path: f.path });
  if (f.orig_path) q.set("old", f.orig_path);
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  try {
    const d = await getJSON("/api/diff?" + q);
    // server tags eligible unstaged diffs with hunk ordinals — arm inline
    // staging BEFORE the render so the rows pick up their hk classes
    if (d.hunks && hunkEligible(f)) {
      diffHunks = { path: f.path, hash: d.hunks.hash, count: d.hunks.count, picks: new Set() };
    }
    renderDiff(d);
    renderHunkBar();
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
  $("conflict-note").classList.add("hidden");
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

// hunkCls/hunkAttr decorate a diff row that belongs to a stageable hunk:
// the data-hunk tag drives click-to-select, the classes the highlight (a
// resize re-render keeps the current picks).
function hunkCls(r) {
  if (r.hunk == null || !diffHunks) return "";
  return " hk" + (diffHunks.picks.has(r.hunk) ? " picked" : "");
}

function hunkAttr(r) {
  return r.hunk == null || !diffHunks ? "" : ` data-hunk="${r.hunk}"`;
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
        `<tr class="${r.kind}${hunkCls(r)}"${hunkAttr(r)}>` +
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
            `<tr class="del${hunkCls(r)}"${hunkAttr(r)}><td class="no l">${r.left_no || ""}</td><td class="no r"></td>` +
            `<td class="side l">${markSpans(r.left, r.left_spans, "l")}</td></tr>`;
        if (r.kind !== "del")
          html +=
            `<tr class="add${hunkCls(r)}"${hunkAttr(r)}><td class="no l"></td><td class="no r">${r.right_no || ""}</td>` +
            `<td class="side r">${markSpans(r.right, r.right_spans, "r")}</td></tr>`;
      }
    }
  } else {
    for (const r of rows) {
      html +=
        `<tr class="${r.kind}${hunkCls(r)}"${hunkAttr(r)}>` +
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

// ---- inline hunk staging (wave 3, reworked from live feedback) -----------
// Hunks are selected IN the unstaged diff itself (TUI-style: full context,
// line numbers, the same view — a separate block list lost the "what is
// what" context). The server tags eligible /api/diff?wt=unstaged rows with
// hunk ordinals + the staging freshness hash; clicking a tagged block
// toggles it and the diff-header bar stages the picked set. Picks are
// POSITIONAL against the exact bytes the server hashed — every staged
// round changes the hash, so the diff is RELOADED after each round (a 409
// means someone else moved the file: same reload).

let diffHunks = null; // {path, hash, count, picks: Set<int>} — set only while an eligible unstaged diff is open

function hunkEligible(f) {
  return !!f && f.section === "changes" && f.kind === "tracked";
}

function clearDiffHunks() {
  diffHunks = null;
  conflictPick = null;
  renderHunkBar();
  renderResolveBar();
}

function renderHunkBar() {
  const bar = $("hunk-bar");
  if (!diffHunks) {
    bar.classList.add("hidden");
    return;
  }
  bar.classList.remove("hidden");
  const n = diffHunks.picks.size;
  $("hunk-stage").disabled = !n;
  $("hunk-stage").textContent = `stage selected (${n})`;
}

// paintHunkPicks flips only the picked classes — no diff re-render, so
// scroll position and text selection survive a toggle.
function paintHunkPicks() {
  document.querySelectorAll("#diff-body tr[data-hunk]").forEach((tr) => {
    tr.classList.toggle("picked", !!diffHunks && diffHunks.picks.has(Number(tr.dataset.hunk)));
  });
  renderHunkBar();
}

async function stageHunksPicked() {
  const v = diffHunks;
  if (!v || !v.picks.size) return;
  let resp;
  try {
    resp = await postJSON("/api/stage-hunks", {
      path: v.path,
      picks: [...v.picks].sort((a, b) => a - b),
      hash: v.hash,
    });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    // 409 = stale picks (the file moved): reload the diff for fresh tags
    if (/file changed/.test(e.message || "")) reopenAfterHunkStage(v.path);
    return;
  }
  applyStatus(resp); // the 200 body IS a fresh /api/status payload
  reconcileStatusView();
  renderFiles();
  reopenAfterHunkStage(v.path);
}

// reopenAfterHunkStage re-opens the freshest view of path after a staging
// round: its unstaged diff while hunks remain, else whatever the cursor
// lands on (the file may have moved wholly into Staged).
function reopenAfterHunkStage(path) {
  const i = state.statusEntries.findIndex((f) => f.path === path && f.section === "changes");
  if (i >= 0) {
    state.fileCursor = i;
    renderFiles();
    openStatusDiff(i);
    return;
  }
  clearDiffHunks();
  const f = state.statusEntries[state.fileCursor];
  if (state.filesMode === "status" && f) {
    openStatusDiff(state.fileCursor);
  } else {
    $("diff-title").textContent = "";
    $("diff-body").innerHTML = "";
    updateDiffNav();
  }
}

$("hunk-stage").addEventListener("click", () => void stageHunksPicked());
$("hunk-all").addEventListener("click", () => {
  if (!diffHunks) return;
  diffHunks.picks = new Set(Array.from({ length: diffHunks.count }, (_, i) => i));
  paintHunkPicks();
});
$("hunk-none").addEventListener("click", () => {
  if (!diffHunks) return;
  diffHunks.picks = new Set();
  paintHunkPicks();
});

$("diff-body").addEventListener("click", (e) => {
  if (!diffHunks) return;
  const tr = e.target.closest("tr[data-hunk]");
  if (!tr || !getSelection().isCollapsed) return; // don't toggle mid text-selection
  const i = Number(tr.dataset.hunk);
  if (diffHunks.picks.has(i)) diffHunks.picks.delete(i);
  else diffHunks.picks.add(i);
  paintHunkPicks();
});

// ---- conflict block picker (conflict surface) -----------------------------
// A conflicted row opens the file's marker regions as pickable ours/theirs
// blocks (GET /api/conflict-hunks). Picks are POSITIONAL against the exact
// bytes the server hashed; resolving POSTs the full pick set and the server
// writes + stages via engine.ResolveConflictHunks. A 409 means the file
// moved: reload the picker (the stage-hunks rule).

let conflictPick = null; // {path, hash, count, choices: Array<null|"ours"|"theirs">} — set only while the picker is open

async function openConflictPicker(f) {
  clearDiffHunks(); // also nulls conflictPick — order matters, set it after
  $("diff-title").textContent = f.path + " — resolve";
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  let d;
  try {
    d = await getJSON("/api/conflict-hunks?" + new URLSearchParams({ path: f.path }));
  } catch (e) {
    // typed 422 refusal (binary / markers gone): show the reason + the way out
    $("diff-body").innerHTML =
      `<div class="notice">${esc(e.message || e)}</div>` +
      `<div class="notice">right-click the file → mark resolved when it is done</div>`;
    return;
  }
  conflictPick = { path: f.path, hash: d.hash, count: d.count, choices: new Array(d.count).fill(null) };
  let html = '<div id="cf-doc">';
  for (const it of d.items) {
    if (it.kind === "text") {
      html += `<pre class="cf-text">${esc((it.lines || []).join("\n"))}</pre>`;
    } else {
      html += `<div class="cf-block" data-b="${it.index}">` +
        `<div class="cf-side cf-ours" data-side="ours"><div class="cf-tag">ours${cfSideCount(it.ours)}</div><pre>${cfSideHTML(it.ours)}</pre></div>` +
        `<div class="cf-side cf-theirs" data-side="theirs"><div class="cf-tag">theirs${cfSideCount(it.theirs)}</div><pre>${cfSideHTML(it.theirs)}</pre></div>` +
        `</div>`;
    }
  }
  $("diff-body").innerHTML = html + "</div>";
  renderResolveBar();
}

// cfSideCount renders the tag's " · N lines" suffix — the disambiguator when
// both sides are visually blank (a conflict between runs of empty lines
// otherwise reads as nothing vs nothing).
function cfSideCount(lines) {
  const n = (lines || []).length;
  return n === 0 ? " · empty" : n === 1 ? " · 1 line" : ` · ${n} lines`;
}

// cfSideHTML renders one side's lines with emptiness made visible: a side
// with no lines at all says so (this side deletes the region), an empty line
// shows a dim ¶, and a whitespace-only line shows its spaces/tabs as ·/→ so
// "3 spaces" and "empty" stop looking identical. A trailing \r (CRLF file —
// ParseConflict keeps it) is ignored for the blank test so a "\r" line reads
// as empty rather than invisible.
function cfSideHTML(lines) {
  if (!lines || !lines.length) return '<span class="cf-empty">(empty — this side has no lines)</span>';
  return lines
    .map((ln) => {
      const bare = ln.replace(/\r$/, "");
      if (!/^\s*$/.test(bare)) return esc(ln);
      const glyphs = bare.length ? bare.replace(/\t/g, "→").replace(/ /g, "·") : "¶";
      return `<span class="cf-ws">${esc(glyphs)}</span>`;
    })
    .join("\n");
}

function paintConflictPicks() {
  document.querySelectorAll("#cf-doc .cf-block").forEach((el) => {
    const choice = conflictPick && conflictPick.choices[Number(el.dataset.b)];
    el.classList.toggle("decided", !!choice);
    el.querySelectorAll(".cf-side").forEach((s) =>
      s.classList.toggle("picked", !!choice && s.dataset.side === choice));
  });
  renderResolveBar();
}

function renderResolveBar() {
  const bar = $("resolve-bar");
  if (!conflictPick) { bar.classList.add("hidden"); return; }
  bar.classList.remove("hidden");
  const n = conflictPick.choices.filter(Boolean).length;
  $("resolve-count").textContent = n + "/" + conflictPick.count + " picked";
  $("resolve-go").disabled = n !== conflictPick.count;
}

function setAllConflictPicks(side) {
  if (!conflictPick) return;
  conflictPick.choices.fill(side);
  paintConflictPicks();
}

async function resolveConflictPicked() {
  const v = conflictPick;
  if (!v || v.choices.some((c) => !c)) return;
  let resp;
  try {
    resp = await postJSON("/api/resolve-hunks", { path: v.path, picks: v.choices, hash: v.hash });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    // 409 = the file moved under the picker: reload it for fresh blocks
    if (/file changed/.test(e.message || "")) {
      const i = state.statusEntries.findIndex((f) => f.path === v.path && f.section === "conflicts");
      if (i >= 0) { state.fileCursor = i; openStatusDiff(i); }
    }
    return;
  }
  const path = v.path;
  conflictPick = null;
  applyStatus(resp); // the 200 body IS a fresh /api/status payload
  reconcileStatusView();
  renderFiles();
  stepToNextConflict(path);
}

// After a resolve: the same path if somehow still conflicted, else the next
// conflicted file, else whatever the cursor lands on (the resolved file
// moved to Staged).
function stepToNextConflict(path) {
  let i = state.statusEntries.findIndex((f) => f.path === path && f.section === "conflicts");
  if (i < 0) i = state.statusEntries.findIndex((f) => f.section === "conflicts");
  if (i >= 0) { state.fileCursor = i; renderFiles(); openStatusDiff(i); return; }
  const f = state.statusEntries[state.fileCursor];
  if (state.filesMode === "status" && f) openStatusDiff(state.fileCursor);
  else { $("diff-title").textContent = ""; $("diff-body").innerHTML = ""; updateDiffNav(); }
}

$("resolve-ours").addEventListener("click", () => setAllConflictPicks("ours"));
$("resolve-theirs").addEventListener("click", () => setAllConflictPicks("theirs"));
$("resolve-go").addEventListener("click", resolveConflictPicked);
$("diff-body").addEventListener("click", (e) => {
  if (!conflictPick) return;
  if (!getSelection().isCollapsed) return; // selecting text is not a pick
  const side = e.target.closest(".cf-side");
  if (!side) return;
  conflictPick.choices[Number(side.closest(".cf-block").dataset.b)] = side.dataset.side;
  paintConflictPicks();
});

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
    const top = state.cursor * ROW_H + (state.cursor > 0 ? wtExtra() : 0);
    const h = state.cursor === 0 && state.wt ? WT_H : ROW_H;
    if (top < scroll.scrollTop) scroll.scrollTop = top;
    else if (top + h > scroll.scrollTop + scroll.clientHeight)
      scroll.scrollTop = top + h - scroll.clientHeight;
    renderCommits();
  } else {
    const list = state.filesMode === "status" ? state.statusEntries : state.files;
    if (!list.length) return;
    state.fileCursor = Math.max(0, Math.min(list.length - 1, state.fileCursor + delta));
    renderFiles();
  }
}

document.addEventListener("keydown", (e) => {
  const top = topLayer();
  if (top) {
    if (top.onKey && top.onKey(e)) return;
    if (e.key === "Escape") closeLayer(top.id); // default close for layers without onKey
    return; // a non-empty stack owns the keyboard
  }
  // Palette shortcut: after layer routing (an open layer keeps the keyboard),
  // before the form-field guard (ctrl+k must work from the commit box).
  if ((e.ctrlKey || e.metaKey) && (e.key === "k" || e.key === "p")) {
    e.preventDefault(); // ctrl+p would open the browser print dialog
    openPalette("cmd");
    return;
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
    openHelp();
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
    case "help": openHelp(); break;
    case "palette": openPalette("cmd"); break;
  }
});

$("commits-scroll").addEventListener("scroll", renderCommits);
$("commits-window").addEventListener("click", (e) => {
  const row = e.target.closest(".crow");
  if (row) openCommit(Number(row.dataset.i));
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
  ];
  if (c.parents === 1) {
    items.push({ label: "move up (newer)", act: () => commitEdit(c, "move-up") });
    items.push({ label: "move down (older)", act: () => commitEdit(c, "move-down") });
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
$("help").addEventListener("click", () => closeLayer("help"));
$("help-box").addEventListener("click", (e) => e.stopPropagation()); // allow selecting/copying text
// Right-click on a working-tree status file: stage/unstage it (per its
// section), bulk actions, copy path. Selects the row for feedback without
// opening its diff.
$("files-list").addEventListener("contextmenu", (e) => {
  if (state.filesMode !== "status") return;
  const li = e.target.closest("li");
  if (!li || li.dataset.i === undefined) return;
  e.preventDefault();
  const i = Number(li.dataset.i);
  const f = state.statusEntries[i];
  if (!f) return;
  state.fileCursor = i;
  renderFiles();
  const items = [];
  if (f.section === "staged") items.push({ label: "unstage " + f.path, act: () => stage({ paths: [f.path], unstage: true }) });
  else if (f.section === "conflicts") items.push({ label: "mark resolved (stage as-is)", act: () => stage({ paths: [f.path] }) });
  else items.push({ label: "stage " + f.path, act: () => stage({ paths: [f.path] }) });
  items.push({ label: "copy path", act: () => copyText(f.path) });
  // the mass rows vanish while an op is paused — same footguns as the
  // hidden #files-actions buttons (stage all = markers staged as resolved,
  // unstage all = auto-merged results pulled out of the merge commit)
  if (!state.conflict) {
    items.push({ label: "stage all", act: () => stage({ all: true }) });
    if (state.statusEntries.some((x) => x.section === "staged")) {
      items.push({
        label: "unstage all",
        act: () => {
          const paths = state.statusEntries.filter((x) => x.section === "staged").map((x) => x.path);
          if (paths.length) stage({ paths, unstage: true }); // engine.Stage{All} can't unstage
        },
      });
    }
  }
  if (f.section === "changes") {
    items.push({
      label: "discard changes", danger: true,
      act: () => showLocalConfirm(
        "Discard changes to " + f.path + "? This cannot be undone.",
        ["discard", "abort"],
        (o) => { if (o === "discard") startOp({ op: "discard", path: f.path }, "discard " + f.path); }
      ),
    });
  } else if (f.section === "untracked") {
    items.push({
      label: "delete untracked file", danger: true,
      act: () => showLocalConfirm(
        "Delete untracked " + f.path + "? This cannot be undone.",
        ["discard", "abort"],
        (o) => { if (o === "discard") startOp({ op: "discard", path: f.path }, "discard " + f.path); }
      ),
    });
  }
  showCtxMenu(items, e.clientX, e.clientY);
});

$("stage-all").addEventListener("click", () => stage({ all: true }));
$("unstage-all").addEventListener("click", () => {
  const paths = state.statusEntries.filter((f) => f.section === "staged").map((f) => f.path);
  if (paths.length) stage({ paths, unstage: true }); // engine.Stage{All} can't unstage
});
$("commit-btn").addEventListener("click", doCommit);
$("pull-btn").addEventListener("click", doPull);
$("push-btn").addEventListener("click", doPush);
$("stash-btn").addEventListener("click", doStash);
$("conflict-continue").addEventListener("click", () => {
  if (opBusy() || !state.conflict) return;
  startOp({ op: "continue" }, "continue " + state.conflict.op);
});
$("conflict-abort").addEventListener("click", () => {
  if (opBusy() || !state.conflict) return;
  const op = state.conflict.op;
  showLocalConfirm(
    "Abort the paused " + op + "? Conflict resolutions so far are discarded.",
    ["abort " + op, "cancel"],
    (o) => { if (o !== "cancel") startOp({ op: "abort" }, "abort " + op); }
  );
});
window.addEventListener("resize", () => {
  renderCommits();
  if (state.lastDiff) renderDiff(state.lastDiff); // unified↔side-by-side is width-dependent
});

async function loadRepo() {
  const repo = await getJSON("/api/repo");
  state.repo = repo; // {name, worktree, branch} — the palette repo-picker filters out the served root
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

// ---- command palette + global ☰ menu (wave 3) ----------------------------
// The palette is a layer with an input INSIDE it: onKey consumes nav keys
// (returns true) and returns false for everything else, so the browser
// delivers the keystroke to the focused input while the router's
// non-empty-stack short-circuit keeps global keys off. Every close path MUST
// go through closePalette() — the input.blur() is load-bearing: a focused
// input after close would trap all global keys in the form-field guard.

let pal = null; // {mode: "cmd"|"repo", fromCmd, rows, filtered, sel}

function paletteCommands() {
  return [
    { label: "pull", detail: "p", run: () => doPull() },
    { label: "push", detail: "P", run: () => doPush() },
    { label: "fetch all remotes", detail: "", run: () => doFetch() },
    { label: "create branch…", detail: "", run: () => openCreateBranchPrompt() },
    { label: "branch versions…", detail: "", run: () => openVersionBranches() },
    { label: "review working changes (AI)…", detail: "", run: () => startReview("working", "") },
    { label: "review this branch (AI)…", detail: "", run: () => startReview("branch", "") },
    { label: "refresh", detail: "r", run: () => { if (!state.op) refreshAfterOp(); } },
    { label: "switch repo…", detail: "", run: null }, // drills into repo mode (runPaletteRow)
    { label: "open working tree", detail: "", run: () => openWorkingTree(0) }, // 0 = the WT row; a bare call would set state.cursor = undefined and break j/k/enter
    { label: "toggle sidebar", detail: "b", run: () => toggleSidebar() },
    { label: "toggle graph", detail: "g", run: () => toggleGraphMode() },
    { label: "help", detail: "?", run: () => openHelp() },
  ];
}

function openPalette(mode, fromCmd) {
  const already = !!pal;
  pal = { mode, fromCmd: !!fromCmd, rows: [], filtered: [], sel: 0 };
  if (!already) pushLayer("palette", $("palette"), { onKey: paletteKey });
  $("palette-input").value = "";
  if (mode === "cmd") {
    pal.rows = paletteCommands();
    filterPalette();
  } else {
    renderPalette([{ label: "loading…", empty: true }]);
    getJSON("/api/repos")
      .then((j) => {
        if (!pal || pal.mode !== "repo") return; // closed or switched meanwhile
        // The SERVER says which row is the repo it is serving. Comparing
        // paths here looked equivalent and was not: /api/repo reports git's
        // forward-slash top-level while the registry stores platform-cleaned
        // paths, so on Windows the served repo never matched and stayed in
        // the list — picking it re-rooted onto the repo already open.
        pal.rows = (j.repos || [])
          .filter((r) => !r.current)
          .map((r) => ({ label: r.name, detail: r.path, path: r.path }));
        filterPalette();
      })
      .catch((e) => {
        closePalette();
        opLine("error: " + (e.message || e), true);
      });
  }
  $("palette-input").focus();
}

function closePalette() {
  closeLayer("palette");
  $("palette-input").blur();
  pal = null;
}

function filterPalette() {
  if (!pal) return;
  const q = $("palette-input").value.trim().toLowerCase();
  pal.filtered = pal.rows.filter(
    (r) => !q || r.label.toLowerCase().includes(q) || (r.detail || "").toLowerCase().includes(q)
  );
  pal.sel = 0;
  renderPalette(pal.filtered.length ? pal.filtered : [{ label: pal.mode === "repo" ? "no other repos" : "no match", empty: true }]);
}

function renderPalette(rows) {
  $("palette-list").innerHTML = rows
    .map((r, i) =>
      r.empty
        ? `<li class="empty">${esc(r.label)}</li>`
        : `<li data-i="${i}"${i === pal.sel ? ' class="sel"' : ""}><span>${esc(r.label)}</span><span class="detail">${esc(r.detail || "")}</span></li>`
    )
    .join("");
}

function runPaletteRow(row) {
  if (!row) return;
  if (pal.mode === "repo") {
    const path = row.path;
    closePalette();
    doReroot(path);
    return;
  }
  if (row.label === "switch repo…") {
    openPalette("repo", true);
    return;
  }
  const run = row.run;
  closePalette();
  run();
}

function paletteKey(e) {
  if (!pal) return false;
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    const n = pal.filtered.length;
    if (n) {
      pal.sel = Math.min(n - 1, Math.max(0, pal.sel + (e.key === "ArrowDown" ? 1 : -1)));
      renderPalette(pal.filtered);
    }
    e.preventDefault();
    return true;
  }
  if (e.key === "Enter") {
    runPaletteRow(pal.filtered[pal.sel]);
    e.preventDefault();
    return true;
  }
  if (e.key === "Escape") {
    if (pal.mode === "repo" && pal.fromCmd) openPalette("cmd");
    else closePalette();
    e.preventDefault();
    return true;
  }
  if (e.key === "Tab") {
    e.preventDefault();
    return true;
  }
  return false; // typing lands in the focused input; its input event re-filters
}

$("palette-input").addEventListener("input", filterPalette);
$("palette").addEventListener("click", closePalette); // backdrop
$("palette-box").addEventListener("click", (e) => e.stopPropagation());
$("palette-list").addEventListener("click", (e) => {
  const li = e.target.closest("li[data-i]");
  if (li && pal) runPaletteRow(pal.filtered[Number(li.dataset.i)]);
});

function openGlobalMenu() {
  const r = $("menu-btn").getBoundingClientRect();
  // Sorted at render so a future entry cannot land unsorted: the menu is a
  // lookup surface (no workflow order to preserve, no destructive rows).
  // help sits alone below a separator — the one fixed anchor.
  const rows = [
    { label: "pull", act: () => doPull() },
    { label: "push", act: () => doPush() },
    { label: "fetch all remotes", act: () => doFetch() },
    { label: "create branch…", act: () => openCreateBranchPrompt() },
    { label: "branch versions…", act: () => openVersionBranches() },
    { label: "review working changes (AI)…", act: () => startReview("working", "") },
    { label: "refresh", act: () => { if (!state.op) refreshAfterOp(); } },
    { label: "switch repo…", act: () => openPalette("repo") },
    { label: "command palette…", act: () => openPalette("cmd") },
    { label: "toggle sidebar", act: () => toggleSidebar() },
    { label: "toggle graph", act: () => toggleGraphMode() },
  ].sort((a, b) => a.label.localeCompare(b.label));
  rows.push({ sep: true }, { label: "help", act: () => openHelp() });
  showCtxMenu(rows, r.left, r.bottom + 4);
}

$("menu-btn").addEventListener("click", (e) => {
  // stopPropagation: the document-level outside-click closer would otherwise
  // see this same click and close the menu the moment it opens.
  e.stopPropagation();
  const t = topLayer();
  if (t && t.id === "ctx") { hideCtxMenu(); return; } // second click toggles closed
  openGlobalMenu();
});
