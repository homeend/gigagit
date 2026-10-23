// openfiles.js — the ctrl+\ switcher: a tabbed popup like the TUI's.
//   Agents     the agent sessions of this gg (every repo), grouped repo →
//              worktree; enter opens one as a console (console.js), k kills,
//              X kills and removes, x removes an exited one.
//   AI tasks   every AI task (the TUI's Headless tab): live ones, then the
//              history; enter shows a result (a review's note, a commit
//              message with apply, a failure's output) or a running agent's
//              console, k k cancels, x removes an ended record, s retries a
//              review save, / filters (tasks.js does the work).
//   Overviews & notes
//              the worktree's open files, shared by every tab of this gg web
//              (plan 5b), as a tree (spec 2026-10-19-documents-tree §6): every
//              overview, collapsed, with the files its anchors use under it,
//              then Notes (noted files no overview names) and Opened (the
//              rest). ● marks the file THIS tab's viewer shows; enter brings
//              a row back where this tab left it (a file under an overview:
//              the overview beside it, at that file's first anchor; a group
//              toggles), → / ← expand and collapse, x closes it in every tab
//              (a document — an agent's overview, a noted file — goes to the
//              background instead), D deletes an agent's overview document or
//              every note on a file, after asking (spec
//              2026-10-18-gg-session-docs §4.4), / filters; right-click a row
//              for the same.
import { $, activityAttn, activityLabel, charWidth, elidePath, esc, getJSON, postJSON, reportLine, tabId } from "./core.js";
import { closeLayer, mountOverlay, popFoot, pushFoot, pushLayer, showCtxMenu } from "./layers.js";
import { opLine, showLocalConfirm } from "./ops.js";
import { dropViewer, openViewer, viewerClosedFile, viewerFileId } from "./viewer.js";
import { registerHelp } from "./menus.js";
import { consoleSessionId, killSession, openConsole, removeSession } from "./console.js";
import { cancelTask, openTaskResult, removeTask, retryTaskSave } from "./tasks.js";

// --- switcher model (pure; guarded against Go) ---
// An overview's row names it by its title (its path is the TUI's display
// name); its meta is the TUI's "overview · N anchors · author · age".
function switcherRows(files, mine, now = Date.now()) {
  return files.map((f) => ({
    id: f.id,
    mark: f.id === mine ? "●" : "○",
    path: f.source === "overview" ? f.title || f.path : f.path,
    line: f.line > 0 ? ":" + f.line : "",
    source: f.source,
    rev: f.rev || "",
    notes: f.notes || 0,
    meta: f.source === "overview" ? overviewRowMeta(f, now) : "",
  }));
}

function clampSel(sel, n) {
  return n ? Math.min(Math.max(sel, 0), n - 1) : 0;
}

// rowAge is a compact age for a row ("2h", "3d"); "" for an unknown time.
function rowAge(iso, now) {
  const t = Date.parse(iso || "");
  if (!t) return "";
  const s = Math.max(0, Math.floor((now - t) / 1000));
  if (s < 60) return s + "s";
  if (s < 3600) return Math.floor(s / 60) + "m";
  if (s < 86400) return Math.floor(s / 3600) + "h";
  if (s < 86400 * 365) return Math.floor(s / 86400) + "d";
  return Math.floor(s / (86400 * 365)) + "y";
}

// overviewRowMeta is an overview row's meta (the TUI's openFileRowText):
// "overview · N anchors · author · age"; author and age only when known.
function overviewRowMeta(f, now) {
  const parts = ["overview · " + (f.anchors || 0) + " anchors"];
  if (f.author) parts.push(f.author);
  const age = rowAge(f.created, now);
  if (age) parts.push(age);
  return parts.join(" · ");
}

// deleteAction is what D (and the row menu's Delete) does on a row: the
// overview document, a noted file's notes, or nothing.
function deleteAction(f) {
  if (f.source === "overview") return "overview";
  if (f.source === "worktree" && f.notes > 0) return "notes";
  return "";
}

// deletePrompt is the confirm's prompt for deleteAction(f) ("" = nothing).
function deletePrompt(f) {
  switch (deleteAction(f)) {
    case "overview": return "Delete this overview?\n" + (f.title || f.path);
    case "notes": return f.notes === 1 ? "Delete the note on " + f.path + "?" : "Delete the " + f.notes + " notes on " + f.path + "?";
  }
  return "";
}

// sessionRows lays sessions out repo → worktree → session (the TUI's
// sessionsPopupRows). mine marks the session this tab's console shows.
function sessionRows(list, mine, now) {
  const byRepo = new Map();
  for (const s of list) {
    if (!byRepo.has(s.repo)) byRepo.set(s.repo, new Map());
    const dirs = byRepo.get(s.repo);
    if (!dirs.has(s.worktree)) dirs.set(s.worktree, []);
    dirs.get(s.worktree).push(s);
  }
  const rows = [];
  for (const repo of [...byRepo.keys()].sort()) {
    rows.push({ h: repo });
    const dirs = byRepo.get(repo);
    for (const wt of [...dirs.keys()].sort()) {
      rows.push({ h2: wt.split("/").pop() + " — " + wt });
      for (const s of dirs.get(wt)) {
        rows.push({
          id: s.id, label: s.label, wt, task: !!s.task,
          glyph: s.state === "exited" ? "○" : "●",
          mark: s.id === mine ? "●" : "○",
          // The row is wide: the age, then the activity ("2m · idle 3m"),
          // then an unanswered report's first line ("done 3m — merged …").
          meta: s.state === "exited" ? "exited (" + s.exit_code + ")" : [rowAge(s.started, now), activityLabel(s, now) + (reportLine(s) ? " — " + reportLine(s) : "")].filter(Boolean).join(" · "),
          state: s.state,
          attn: activityAttn(s),
        });
      }
    }
  }
  return rows;
}

// formatElapsed is the TUI's: 45s, 2m05s, 1h03m.
function formatElapsed(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  const two = (n) => String(n).padStart(2, "0");
  if (s >= 3600) return Math.floor(s / 3600) + "h" + two(Math.floor(s / 60) % 60) + "m";
  if (s >= 60) return Math.floor(s / 60) + "m" + two(s % 60) + "s";
  return s + "s";
}

// coarseAgo is a duration in its largest whole unit: 3h15m → 3h, 55s.
function coarseAgo(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s >= 86400) return Math.floor(s / 86400) + "d";
  if (s >= 3600) return Math.floor(s / 3600) + "h";
  if (s >= 60) return Math.floor(s / 60) + "m";
  return s + "s";
}

// taskRows are the AI tasks tab's rows (the TUI's Headless tab): a live
// task's age is the time since it started (or was queued), an ended one's is
// its local date and time plus how long ago in the largest unit — a settled
// row does not tick. query filters on key + agent.
function taskRows(list, now, query) {
  const q = (query || "").toLowerCase();
  return list
    .filter((t) => !q || (t.key + " " + t.agent).toLowerCase().includes(q))
    .map((t) => {
      let age;
      if (t.live) age = formatElapsed(now - Date.parse(t.started && !t.started.startsWith("0001") ? t.started : t.submitted));
      else {
        const d = new Date(t.ended);
        const two = (n) => String(n).padStart(2, "0");
        age = two(d.getMonth() + 1) + "-" + two(d.getDate()) + " " + two(d.getHours()) + ":" + two(d.getMinutes()) + " · " + coarseAgo(now - d.getTime()) + " ago";
      }
      return {
        id: t.id, key: t.key, agent: t.agent, state: t.state, session: t.session || "", age,
        live: !!t.live, kind: t.kind || "", has_result: !!t.has_result, note_id: t.note_id || "", save_err: t.save_err || "", worktree: t.worktree || "",
      };
    });
}

// taskHint is the row's actions for the foot (the TUI's taskHint): enter
// only when it opens something, s only for a failed save, k k for a live
// task, x for an ended one.
function taskHint(r) {
  const out = [];
  if (r.has_result) out.push("enter result");
  else if (r.live && r.session) out.push("enter console");
  else if (r.state === "failed") out.push("enter output");
  if (r.save_err) out.push("s retry save");
  out.push(r.live ? "k k cancel" : "x remove");
  return out;
}

// taskKey is what k / x / s mean on a row: k arms the cancel (armed holds
// the id armed) and cancels on the second press; x removes an ended record
// and refuses a live one; s retries a failed save.
function taskKey(key, r, armed) {
  if (key === "k") return !r.live ? "none" : armed === r.id ? "cancel" : "arm";
  if (key === "x") return r.live ? "refuse" : "remove";
  if (key === "s") return r.save_err ? "retry" : "none";
  return "none";
}

// freshestTab: agents when any session exists, else files when any file is
// open, else agents (the empty Agents tab explains how to start one).
function freshestTab(sessions, tasks, files) {
  if (sessions.length) return "agents";
  if (files.length) return "files";
  return "agents";
}

// agentKey: what k / X / x mean on a session row (the TUI popup's keys, plus
// its X). An exited row cannot be killed; X on one is x.
function agentKey(key, st) {
  if (key === "k") return st === "exited" ? "none" : "kill";
  if (key === "X") return st === "exited" ? "remove" : "killrm";
  if (key === "x") return st === "exited" ? "remove" : "refuse-remove";
  return "none";
}
// --- the Overviews & notes tree (spec 2026-10-19-documents-tree §6) ---
// docTree is the tab's rows: every overview (the list's order) with — when
// expanded — one child per file its anchors use (the server's children),
// then the Notes group (noted working-tree files no overview names) and
// the Opened group (the rest); a group with nothing in it is not drawn. A
// child's document is the list's working-tree row of its path (or null);
// a document shown as a child is not repeated in a group. A filter shows
// what matches expanded for the filter only (expanded is never written).
function docTree(files, expanded, query, now) {
  const q = (query || "").toLowerCase();
  const hit = (s) => !q || String(s).toLowerCase().includes(q);
  const wtDoc = new Map();
  for (const f of files) if (f.source === "worktree") wtDoc.set(f.path, f);
  const claimed = new Set();
  const out = [];
  for (const ov of files) {
    if (ov.source !== "overview") continue;
    const key = "ov:" + ov.id;
    const kids = (ov.children || []).map((c) => {
      const doc = wtDoc.get(c.path) || null;
      if (doc) claimed.add(doc.id);
      return { kind: "child", id: "child:" + ov.id + ":" + c.path, parent: key, key: "", doc, ov, path: c.path, anchor: c.anchor, group: "", count: 0, expanded: false };
    });
    const self = hit((ov.title || ov.path) + " " + overviewRowMeta(ov, now));
    const shown = q ? (self ? kids : kids.filter((k) => hit(k.path))) : kids;
    if (q && !self && !shown.length) continue;
    const open = q ? shown.length > 0 : expanded.has(key);
    out.push({ kind: "overview", id: key, parent: "", key, doc: ov, ov: null, path: "", anchor: -1, group: "", count: 0, expanded: open });
    if (open) out.push(...shown);
  }
  const groups = { notes: [], opened: [] };
  for (const f of files) {
    if (f.source === "overview" || claimed.has(f.id)) continue;
    groups[f.source === "worktree" && f.notes > 0 ? "notes" : "opened"].push(f);
  }
  for (const g of ["notes", "opened"]) {
    const docs = groups[g];
    if (!docs.length) continue;
    const key = "group:" + g;
    const label = g === "notes" ? "Notes" : "Opened";
    const shown = q ? (hit(label) ? docs : docs.filter((f) => hit(f.path + " " + verLabel(f.source, f.rev)))) : docs;
    if (q && !shown.length) continue;
    const open = q ? true : expanded.has(key);
    out.push({ kind: "group", id: key, parent: "", key, doc: null, ov: null, path: "", anchor: -1, group: g, count: docs.length, expanded: open });
    if (open) for (const f of shown) out.push({ kind: "groupfile", id: "file:" + f.id, parent: key, key: "", doc: f, ov: null, path: f.path, anchor: -1, group: g, count: 0, expanded: false });
  }
  return out;
}

// treeIndex is the cursor after a rebuild: node id if still there, else the
// sibling now at its old index or after it (the next one after a close),
// else the last sibling, else the parent, else a clamp.
function treeIndex(nodes, id, parent, old) {
  const i = nodes.findIndex((n) => n.id === id);
  if (i >= 0) return i;
  if (parent) {
    const sibs = [];
    nodes.forEach((n, j) => { if (n.parent === parent) sibs.push(j); });
    const p = nodes.findIndex((n) => n.id === parent);
    if (sibs.length) {
      const at = sibs.find((j) => j >= old);
      return at !== undefined ? at : sibs[sibs.length - 1];
    }
    if (p >= 0) return p;
  }
  return clampSel(old, nodes.length);
}

// treeKeyAction is what a key does on node (the TUI's treeKey). Under a
// filter nothing is saved: → and enter on a group do nothing, ← only moves
// to the parent.
function treeKeyAction(node, key, filtered) {
  const top = node.kind === "overview" || node.kind === "group";
  if (key === "Enter") {
    if (node.kind === "group") return filtered ? "none" : "toggle";
    return node.kind === "child" ? "open-at" : "open";
  }
  if (key === "ArrowRight") return top && !node.expanded && !filtered ? "expand" : "none";
  if (key === "ArrowLeft") {
    if (top) return node.expanded && !filtered ? "collapse" : "none";
    return filtered ? "parent" : "parent-collapse";
  }
  return "none";
}

function groupCount(n) {
  return n === 1 ? "1 file" : n + " files";
}

// treeRow is a node's row: indent (0 top, 1 under a parent), arrow (▸ / ▾
// on an overview and a group), the ●/○ mark (mine = the file this tab
// shows), name and meta (the flat list's).
function treeRow(n, mine, now) {
  const arrow = n.kind === "overview" || n.kind === "group" ? (n.expanded ? "▾" : "▸") : "";
  if (n.kind === "group") return { indent: 0, arrow, mark: "", name: n.group === "notes" ? "Notes" : "Opened", meta: groupCount(n.count) };
  const indent = n.kind === "overview" ? 0 : 1;
  if (!n.doc) return { indent, arrow, mark: "○", name: n.path, meta: verLabel("worktree", "") };
  const r = switcherRows([n.doc], mine, now)[0];
  const meta = r.source === "overview" ? r.meta : (r.line ? r.line + "  " : "") + verLabel(r.source, r.rev) + (r.notes ? "  · " + r.notes + (r.notes === 1 ? " note" : " notes") : "");
  return { indent, arrow, mark: r.mark, name: r.path, meta };
}

// verLabel names a row's version (viewer.js's versionLabel, which this
// block cannot import).
function verLabel(src, rev) {
  if (src === "commit") return "@ " + String(rev || "").slice(0, 7);
  if (src === "shelf") return "shelf";
  if (src === "overview") return "overview";
  return "working tree";
}
// landingIndex is the row the tab opens on: the file this tab shows (mine)
// — when it sits under a collapsed overview or group, that row — else 0.
function landingIndex(files, nodes, mine, now) {
  const i = nodes.findIndex((n) => n.doc && n.doc.id === mine);
  if (i >= 0) return i;
  const all = new Set(docTree(files, new Set(), "", now).map((n) => n.key).filter(Boolean));
  const hid = docTree(files, all, "", now).find((n) => n.doc && n.doc.id === mine);
  return hid ? Math.max(0, nodes.findIndex((n) => n.id === hid.parent)) : 0;
}
// --- end switcher model ---

const TABS = ["agents", "tasks", "files"];
const sw = { tab: "agents", sessions: [], tasks: [], files: [], nodes: [], sel: 0, query: "", typing: false, armed: "" };
// The tree's expanded overviews and groups (their node keys), for this
// page's life: kept across re-renders and list changes, not a reload.
const expandedNodes = new Set();
const root = mountOverlay("openfiles");
root.innerHTML =
  `<div id="openfiles-box"><div id="openfiles-title"><div id="openfiles-tabs">` +
  `<span data-tab="agents">Agents <i></i></span><span data-tab="tasks">AI tasks <i></i></span><span data-tab="files">Overviews &amp; notes <i></i></span>` +
  `</div></div><div id="openfiles-query" class="hidden"></div><div id="openfiles-list"></div></div>`;
root.addEventListener("click", (e) => {
  if (e.target.id === "openfiles") return closeSwitcher();
  const tab = e.target.closest("#openfiles-tabs span[data-tab]");
  if (tab) return showTab(tab.dataset.tab);
  const row = e.target.closest(".ofrow[data-i]");
  if (!row) return;
  sw.sel = Number(row.dataset.i);
  const n = currentNode();
  if (n && e.target.closest(".tw")) { // the ▸ / ▾ folds, the rest of the row opens
    if (n.key && !sw.query) treeAct(n.expanded ? "ArrowLeft" : "ArrowRight");
    else renderSwitcher();
    return;
  }
  activate();
});
// A tree row's menu (spec 2026-10-18-gg-session-docs §4.4: the list's row
// menu carries the delete): open, Expand / Collapse, close, and — on a
// document — Delete.
root.addEventListener("contextmenu", (e) => {
  const row = e.target.closest(".ofrow[data-i]");
  if (!row || sw.tab !== "files") return;
  e.preventDefault();
  sw.sel = Number(row.dataset.i);
  renderSwitcher();
  const n = currentNode();
  const items = n ? nodeMenu(n) : [];
  if (items.length) showCtxMenu(items, e.clientX, e.clientY);
});

function nodeMenu(n) {
  const items = [];
  if (n.kind === "child") items.push({ label: "open beside the overview", act: () => openAt(n) });
  else if (n.doc) items.push({ label: "open", act: () => bringBack(n.doc) });
  if (n.key && !sw.query) items.push({ label: n.expanded ? "Collapse" : "Expand", act: () => treeAct(n.expanded ? "ArrowLeft" : "ArrowRight") });
  if (n.doc) items.push({ label: "close in every tab", act: closeSelected });
  const what = n.doc ? deleteAction(n.doc) : "";
  if (what) items.push({ sep: true }, { label: what === "overview" ? "Delete overview" : "Delete all notes on this file", danger: true, act: deleteSelected });
  return items;
}

// The console's ctrl+\ (console.js) asks for the switcher through a document
// event: the two modules must not import each other.
document.addEventListener("gg:switcher", () => (isOpen() ? closeSwitcher() : openSwitcher()));
// A Worktrees ◆ row asks for its task the same way (sidebar.js).
document.addEventListener("gg:task", (e) => openSwitcherOnTask(e.detail));

const FOOT_FILES =
  `<span>↑↓ j k move</span><button data-oact="enter">enter open</button><span>→ ← expand/collapse</span>` +
  `<button data-oact="x">x close</button><button data-oact="D">D delete</button><button data-oact="filter">/ filter</button>` +
  `<button data-oact="tab">tab next tab</button><button data-oact="esc">esc close</button>`;
const FOOT_AGENTS =
  `<span>↑↓ j move</span><button data-oact="enter">enter open console</button>` +
  `<button data-oact="kill">k kill</button><button data-oact="killrm">X kill + remove</button><button data-oact="x">x remove</button>` +
  `<button data-oact="filter">/ filter</button><button data-oact="tab">tab next tab</button><button data-oact="esc">esc close</button>`;
// footTasks is the AI tasks tab's foot for the row under the cursor: only
// the keys that act on it (taskHint), then the tab's own.
function footTasks() {
  const r = visibleTasks()[sw.sel];
  const acts = { "enter result": "enter", "enter console": "enter", "enter output": "enter", "s retry save": "retry", "k k cancel": "cancel", "x remove": "x" };
  return (
    `<span>↑↓ j move</span>` +
    (r ? taskHint(r).map((h) => `<button data-oact="${acts[h]}">${esc(h)}</button>`).join("") : "") +
    `<button data-oact="filter">/ filter</button><button data-oact="tab">tab next tab</button><button data-oact="esc">esc close</button>`
  );
}

$("foot").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-oact]");
  if (!b) return;
  if (b.dataset.oact === "enter") activate();
  else if (b.dataset.oact === "x") sw.tab === "agents" ? agentAct("x") : sw.tab === "tasks" ? taskAct("x") : closeSelected();
  else if (b.dataset.oact === "cancel") taskAct("k", true);
  else if (b.dataset.oact === "retry") taskAct("s");
  else if (b.dataset.oact === "D") deleteSelected();
  else if (b.dataset.oact === "kill") agentAct("k");
  else if (b.dataset.oact === "killrm") agentAct("X");
  else if (b.dataset.oact === "tab") showTab(TABS[(TABS.indexOf(sw.tab) + 1) % TABS.length]);
  else if (b.dataset.oact === "filter") startFilter();
  else closeSwitcher();
});

function isSwitcherKey(e) {
  return e.ctrlKey && !e.altKey && !e.metaKey && (e.code === "Backslash" || e.key === "\\");
}

function isOpen() {
  return !root.classList.contains("hidden");
}

async function fetchAll() {
  const [s, t, f] = await Promise.all([
    getJSON("/api/sessions").catch(() => ({ sessions: [] })),
    getJSON("/api/tasks").catch(() => ({ tasks: [] })),
    getJSON("/api/open-files").catch(() => ({ files: [] })),
  ]);
  sw.sessions = s.sessions || [];
  sw.tasks = t.tasks || [];
  sw.files = f.files || [];
}

// openSwitcher(tab): open on tab, or on the freshest one.
async function openSwitcher(tab) {
  await fetchAll();
  sw.query = "";
  sw.typing = false;
  root.style.bottom = $("foot").offsetHeight + "px";
  pushLayer("openfiles", root, { onKey: switcherKey });
  showTab(tab || freshestTab(sw.sessions, sw.tasks, sw.files));
}

function closeSwitcher() {
  closeLayer("openfiles");
  popFoot("openfiles");
}

function showTab(tab) {
  if (tab !== sw.tab) sw.query = ""; // a filter is its tab's (the bar hides below)
  sw.tab = tab;
  sw.typing = false;
  $("openfiles-query").classList.add("hidden");
  for (const el of root.querySelectorAll("#openfiles-tabs span")) el.classList.toggle("on", el.dataset.tab === tab);
  const mineFile = viewerFileId();
  const mineSess = consoleSessionId();
  if (tab === "files") sw.nodes = fileNodes();
  const first = tab === "files" ? landingIndex(sw.files, sw.nodes, mineFile, Date.now()) : tab === "agents" ? Math.max(0, sw.sessions.findIndex((s) => s.id === mineSess)) : 0;
  sw.sel = first;
  sw.armed = "";
  pushFoot("openfiles", tab === "files" ? FOOT_FILES : tab === "agents" ? FOOT_AGENTS : footTasks());
  renderSwitcher();
}

// switcherOpenFiles: the list changed while the switcher is up (another tab,
// an eviction, a stream ending) — re-render from it, never act on a stale row.
function switcherOpenFiles(files) {
  const cur = isOpen() ? currentNode() : null, old = sw.sel;
  sw.files = files;
  if (!isOpen()) return;
  if (cur) rebuildKeeping(cur.id, cur.parent, old);
  renderSwitcher();
}

// fileNodes is the tree tab's rows now (docTree over the list).
function fileNodes() {
  return docTree(sw.files, expandedNodes, sw.tab === "files" ? sw.query : "", Date.now());
}

// currentNode is the tree node under the cursor (null off the tree tab).
function currentNode() {
  return sw.tab === "files" ? sw.nodes[sw.sel] || null : null;
}

// rebuildKeeping rebuilds the tree and keeps the cursor on node id (else
// its sibling now at index old, else its parent: treeIndex).
function rebuildKeeping(id, parent, old) {
  sw.nodes = fileNodes();
  sw.sel = treeIndex(sw.nodes, id, parent, old);
}

// treeAct is a tree key on the node under the cursor (treeKeyAction): fold
// state changes are saved only without a filter.
function treeAct(key) {
  const n = currentNode();
  if (!n) return;
  const p = sw.nodes.find((o) => o.id === n.parent);
  const what = treeKeyAction(n, key, !!sw.query);
  let at = n;
  switch (what) {
    case "expand": expandedNodes.add(n.key); break;
    case "collapse": expandedNodes.delete(n.key); break;
    case "toggle": if (!expandedNodes.delete(n.key)) expandedNodes.add(n.key); break;
    case "parent": case "parent-collapse":
      if (!p) return;
      if (what === "parent-collapse") expandedNodes.delete(p.key);
      at = p;
      break;
    case "open": return bringBack(n.doc);
    case "open-at": return openAt(n);
    default: return;
  }
  rebuildKeeping(at.id, at.parent, sw.sel);
  renderSwitcher();
}

// openAt is enter on a file under an overview: the overview, at that file.
function openAt(n) {
  closeSwitcher();
  openViewer({ id: n.ov.id, anchor: { path: n.path, index: n.anchor } });
}

// switcherSessions: the session list changed (start, exit, remove).
function switcherSessions(list) {
  const cur = visibleSessions()[sw.sel] && visibleSessions()[sw.sel].id;
  sw.sessions = list;
  if (!isOpen()) return;
  if (sw.tab === "agents") {
    const i = visibleSessions().findIndex((s) => s.id === cur);
    sw.sel = clampSel(i >= 0 ? i : sw.sel, visibleSessions().length);
  }
  renderSwitcher();
}

function visibleSessions() {
  const q = sw.query.toLowerCase();
  return q ? sw.sessions.filter((s) => (s.label + " " + s.worktree + " " + s.repo).toLowerCase().includes(q)) : sw.sessions;
}

function counts() {
  const c = { agents: sw.sessions.length, tasks: sw.tasks.length, files: sw.files.length }; // files: the documents, not the rows
  for (const el of root.querySelectorAll("#openfiles-tabs span")) el.querySelector("i").textContent = c[el.dataset.tab];
}

function renderSwitcher() {
  counts();
  const list = $("openfiles-list");
  const cols = Math.floor(list.clientWidth / charWidth()) - 4;
  let html = "";
  if (sw.tab === "files") {
    sw.nodes = fileNodes();
    sw.sel = clampSel(sw.sel, sw.nodes.length);
    const mine = viewerFileId(), now = Date.now();
    html = sw.nodes
      .map((n, i) => {
        const r = treeRow(n, mine, now);
        const room = cols - 3 - (r.indent ? 4 : 0) - (r.mark ? 2 : 0) - r.meta.length;
        return (
          `<div class="ofrow${i === sw.sel ? " sel" : ""}${r.indent ? " kid" : ""}" data-i="${i}" title="${esc(r.name)}">` +
          `<span class="tw">${r.arrow}</span> ${r.mark ? r.mark + " " : ""}${esc(room > 0 ? elidePath(r.name, room) : r.name)}<span class="ofmeta">${esc(r.meta)}</span></div>`
        );
      })
      .join("");
    if (!sw.nodes.length) html = `<div class="ofempty">${sw.query ? "nothing matches" : "no overviews, notes or open files — view file on a file row opens one"}</div>`;
  } else if (sw.tab === "agents") {
    const vis = visibleSessions();
    const rows = sessionRows(vis, consoleSessionId(), Date.now());
    let i = -1;
    html = rows
      .map((r) => {
        if (r.h) return `<div class="grp">${esc(r.h)}</div>`;
        if (r.h2) return `<div class="grp2">${esc(r.h2)}</div>`;
        i++;
        const j = vis.findIndex((s) => s.id === r.id);
        return (
          `<div class="ofrow${j === sw.sel ? " sel" : ""}${r.task ? " task" : ""}${r.attn ? " attn" : ""}" data-i="${j}" title="${esc(r.label)}">` +
          `<span class="glyph ${r.state === "exited" ? "ex" : "run"}">${r.glyph}</span> ${esc(elidePath(r.label, Math.max(8, cols - 30)))}` +
          `<span class="ofmeta">${esc(r.meta)}</span></div>`
        );
      })
      .join("");
    if (!rows.length) html = `<div class="ofempty">${sw.query ? "no session matches" : "no agent sessions — a worktree's menu starts one"}</div>`;
  } else {
    const rows = visibleTasks();
    sw.sel = clampSel(sw.sel, rows.length);
    html = rows
      .map((r, i) => {
        const meta = r.agent + " · " + r.state + " · " + r.age;
        return (
          `<div class="ofrow${i === sw.sel ? " sel" : ""}" data-i="${i}" title="${esc(r.key)}">` +
          `<span class="glyph ${r.live ? "run" : "ex"}">◆</span> ${esc(elidePath(r.key, Math.max(8, cols - 4 - Array.from(meta).length)))}` +
          `<span class="ofmeta">${esc(meta)}</span></div>`
        );
      })
      .join("");
    if (!rows.length) html = `<div class="ofempty">${sw.query ? "no task matches" : "no AI tasks"}</div>`;
    pushFoot("openfiles", footTasks());
  }
  list.innerHTML = html;
  const cur = list.querySelector(".ofrow.sel");
  if (cur) cur.scrollIntoView({ block: "nearest" });
}

function currentCount() {
  return sw.tab === "files" ? sw.nodes.length : sw.tab === "agents" ? visibleSessions().length : visibleTasks().length;
}

// activate: enter on the selected row of the current tab.
function activate() {
  if (sw.tab === "files") return treeAct("Enter");
  if (sw.tab === "agents") {
    const s = visibleSessions()[sw.sel];
    if (!s) return;
    closeSwitcher();
    openConsole(s.id);
    return;
  }
  const t = visibleTasks()[sw.sel];
  if (!t) return;
  // The TUI's openTask: a result first (a review's note replaces the
  // switcher; a text result opens over it and esc comes back here), then a
  // running agent's console, then a failure's output.
  if (t.has_result || (!t.session && t.state === "failed")) {
    if (t.note_id) closeSwitcher();
    openTaskResult(t);
  } else if (t.save_err) {
    opLine(t.save_err + " — s retry save", true);
  } else if (t.live && t.session) {
    closeSwitcher();
    openConsole(t.session);
  } else {
    opLine("no result yet", false);
  }
}

// visibleTasks is the tab's rows under the filter.
function visibleTasks() {
  return taskRows(sw.tasks, Date.now(), sw.tab === "tasks" ? sw.query : "");
}

// taskAct is k / x / s on the task under the cursor (taskKey). viaButton:
// the foot's "k k cancel" button is one deliberate click, not a stray key.
function taskAct(key, viaButton) {
  const r = visibleTasks()[sw.sel];
  if (!r) return;
  const what = viaButton ? "cancel" : taskKey(key, r, sw.armed);
  sw.armed = "";
  if (what === "arm") {
    sw.armed = r.id;
    opLine("press k again to cancel " + r.key);
  } else if (what === "cancel") cancelTask(r);
  else if (what === "refuse") opLine("cancel it first (k k)", true);
  else if (what === "remove") removeTask(r);
  else if (what === "retry") retryTaskSave(r);
}

// switcherTasks: the task rows changed (a live "tasks" event) — keep the
// cursor on the same task when it is still listed.
function switcherTasks(list) {
  const rows = visibleTasks();
  const cur = rows[sw.sel] && rows[sw.sel].id;
  sw.tasks = list;
  if (!isOpen() || sw.tab !== "tasks") return;
  const i = visibleTasks().findIndex((t) => t.id === cur);
  sw.sel = clampSel(i >= 0 ? i : sw.sel, visibleTasks().length);
  renderSwitcher();
}

// openSwitcherOnTask opens the AI tasks tab with the cursor on task id (a
// Worktrees ◆ row's click).
async function openSwitcherOnTask(id) {
  await openSwitcher("tasks");
  const i = visibleTasks().findIndex((t) => t.id === id);
  if (i >= 0) sw.sel = i;
  renderSwitcher();
}

function bringBack(f) {
  if (!f) return;
  closeSwitcher();
  openViewer({ id: f.id });
}

// deleteSelected: D / the row menu's Delete. The switcher closes first (one
// layer asks at a time, as a kill does), the confirm's cancel is the
// default, and the live agentdocs event takes the row away.
function deleteSelected() {
  const n = currentNode();
  if (!n) return;
  const f = n.doc;
  const what = f ? deleteAction(f) : "";
  if (!what) return opLine("nothing to delete on this row", false);
  closeSwitcher();
  showLocalConfirm(deletePrompt(f), ["cancel", "delete"], async (opt) => {
    if (opt !== "delete") return;
    const body = what === "overview" ? { op: "delete-overview", id: f.id } : { op: "delete-notes", path: f.path };
    let ans;
    try {
      ans = await postJSON("/api/session-docs", body);
    } catch (e) {
      return opLine("delete failed: " + (e.message || e), true);
    }
    opLine(what === "overview" ? "overview " + JSON.stringify(f.title || f.path) + " deleted" : ans.deleted === 1 ? "deleted the note" : "deleted " + ans.deleted + " notes", false);
  });
}

// closeSelected: x closes the file in EVERY tab (ruling L4) — a document
// (an agent's overview, a noted file) goes to the background instead: the
// server decides. This tab's own viewer on it goes first, so it does not
// read "closed in another tab".
async function closeSelected() {
  const n = currentNode();
  if (!n) return;
  const f = n.doc;
  if (!f) return opLine(n.kind === "group" ? "a group has nothing to close" : n.path + " is not open", false);
  if (f.id === viewerFileId()) dropViewer();
  else viewerClosedFile(f.id); // still loading here, perhaps: it must not land
  let ans;
  try {
    ans = await postJSON("/api/open-files", { op: "close", id: f.id, tab: tabId, everywhere: true });
  } catch (e) {
    return opLine("close failed: " + (e.message || e), true);
  }
  const old = sw.sel;
  sw.files = ans.files || [];
  rebuildKeeping(n.id, n.parent, old); // gone: the next row in its group
  renderSwitcher();
}

// agentAct: k / X / x on the selected session. A kill asks through the
// modal, so the switcher closes first — one layer asks at a time; a removed
// row leaves through the live sessions event (switcherSessions).
function agentAct(key) {
  const s = visibleSessions()[sw.sel];
  if (!s) return;
  const what = agentKey(key, s.state);
  if (what === "kill" || what === "killrm") {
    closeSwitcher();
    killSession(s, what === "killrm");
  } else if (what === "remove") removeSession(s);
  else if (what === "refuse-remove") opLine("only an exited session can be removed — kill it first (k)", true);
}

function startFilter() {
  sw.typing = true;
  renderQuery();
}

function renderQuery() {
  const q = $("openfiles-query");
  q.classList.toggle("hidden", !sw.typing && !sw.query);
  q.textContent = "/ " + sw.query + (sw.typing ? "▏" : "");
}

function switcherKey(e) {
  if (sw.typing) {
    if (e.key === "Escape") {
      sw.typing = false;
      sw.query = "";
    } else if (e.key === "Enter") {
      sw.typing = false;
    } else if (e.key === "Backspace") {
      sw.query = sw.query.slice(0, -1);
    } else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
      sw.query += e.key;
    } else if (!isSwitcherKey(e)) {
      return true;
    } else {
      closeSwitcher();
      e.preventDefault();
      return true;
    }
    sw.sel = clampSel(sw.sel, currentCount());
    renderQuery();
    renderSwitcher();
    e.preventDefault();
    return true;
  }
  if (sw.tab === "tasks" && (e.key === "k" || e.key === "x" || e.key === "s")) {
    taskAct(e.key);
    e.preventDefault();
    return true;
  }
  if (e.key !== "k") sw.armed = "";
  switch (e.key) {
    case "ArrowDown": case "j": sw.sel = clampSel(sw.sel + 1, currentCount()); renderSwitcher(); break;
    case "ArrowUp": sw.sel = clampSel(sw.sel - 1, currentCount()); renderSwitcher(); break;
    case "k": // the TUI popup's kill on the Agents tab; "up" on the tree
      if (sw.tab === "agents") agentAct("k");
      else { sw.sel = clampSel(sw.sel - 1, currentCount()); renderSwitcher(); }
      break;
    case "X": if (sw.tab === "agents") agentAct("X"); break;
    case "Tab": showTab(TABS[(TABS.indexOf(sw.tab) + (e.shiftKey ? TABS.length - 1 : 1)) % TABS.length]); break;
    case "Enter": activate(); break;
    case "ArrowRight": case "ArrowLeft": if (sw.tab === "files") treeAct(e.key); break;
    case "x": if (sw.tab === "agents") agentAct("x"); else closeSelected(); break;
    case "D": if (sw.tab === "files") deleteSelected(); break;
    case "/": startFilter(); break;
    case "Escape": closeSwitcher(); break;
    default:
      if (!isSwitcherKey(e)) return true; // swallow: the popup owns the keyboard
      closeSwitcher(); // ctrl+\ again toggles it off
  }
  e.preventDefault();
  return true;
}

registerHelp({
  key: "AI tasks",
  html:
    "<b>ctrl+\\</b> → <b>AI tasks</b>: every AI task, live ones first, then the history (the last 50, every repo). " +
    "<b>enter</b> shows a result — a review opens as its review, a commit message offers <b>apply</b> into the commit box (in its own worktree), a failed task shows its output — or a running agent's console; " +
    "<b>k k</b> cancels a live task, <b>x</b> removes a finished one, <b>s</b> retries a review whose save failed, <b>/</b> filters. " +
    "A worktree's queued and running headless tasks show as <b>◆</b> rows under it in Worktrees: click opens the tab on the task, right-click cancels or shows the result.",
});

registerHelp({
  key: "open files",
  html:
    "files opened in the viewer stay open, one list per worktree shared by every tab: <b>ctrl+]</b> in the viewer sends the file to the background, " +
    "<b>ctrl+\\</b> opens the switcher on its Overviews &amp; notes tab, a tree: every overview (collapsed) with the files its links use under it, then <b>Notes</b> (noted files no overview names) and <b>Opened</b> (the rest); " +
    "<b>→</b> / <b>←</b> expand and collapse, <b>/</b> filters (<b>●</b> the one this tab shows) — <b>enter</b> brings one back where this tab left it (a file under an overview opens the overview beside it, at that file's first link; a group toggles), <b>x</b> closes it in every tab " +
    "(an agent's overview or a noted file goes to the background instead: its document holds it), <b>D</b> deletes an agent's overview document or every note on the file, after asking; right-click a row for the same. " +
    "An overview row shows its author and age. A working-tree file follows the disk.",
});

export { isSwitcherKey, openSwitcher, switcherOpenFiles, switcherSessions, switcherTasks };
