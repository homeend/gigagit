// openfiles.js — the ctrl+\ switcher: a tabbed popup like the TUI's.
//   Agents     the agent sessions of this gg (every repo), grouped repo →
//              worktree; enter opens one as a console (console.js), k kills,
//              X kills and removes, x removes an exited one.
//   AI tasks   the AI-task list (read-only for now); enter on a running
//              interactive task opens its console.
//   Open files the worktree's open files, shared by every tab of this gg web,
//              most recently shown first (plan 5b). ● marks the file THIS
//              tab's viewer shows; enter brings one back where this tab left
//              it, x closes it in every tab.
import { $, charWidth, elidePath, esc, getJSON, postJSON, tabId } from "./core.js";
import { closeLayer, mountOverlay, popFoot, pushFoot, pushLayer } from "./layers.js";
import { opLine } from "./ops.js";
import { dropViewer, openViewer, versionLabel, viewerClosedFile, viewerFileId } from "./viewer.js";
import { registerHelp } from "./menus.js";
import { consoleSessionId, killSession, openConsole, removeSession } from "./console.js";

// --- switcher model (pure; guarded against Go) ---
// An overview's row names it by its title (its path is the TUI's display
// name); its meta says overview.
function switcherRows(files, mine) {
  return files.map((f) => ({
    id: f.id,
    mark: f.id === mine ? "●" : "○",
    path: f.source === "overview" ? f.title || f.path : f.path,
    line: f.line > 0 ? ":" + f.line : "",
    source: f.source,
    rev: f.rev || "",
    notes: f.notes || 0,
  }));
}

function clampSel(sel, n) {
  return n ? Math.min(Math.max(sel, 0), n - 1) : 0;
}

function ageOf(iso, now) {
  const s = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  return s < 60 ? s + "s" : s < 3600 ? Math.floor(s / 60) + "m" : Math.floor(s / 3600) + "h";
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
          meta: s.state === "exited" ? "exited (" + s.exit_code + ")" : ageOf(s.started, now),
          state: s.state,
        });
      }
    }
  }
  return rows;
}

function taskRows(list, now) {
  return list.map((t) => ({ id: t.id, key: t.key, agent: t.agent, state: t.state, session: t.session || "", age: ageOf(t.started || t.submitted, now) }));
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
// --- end switcher model ---

const TABS = ["agents", "tasks", "files"];
const sw = { tab: "agents", sessions: [], tasks: [], files: [], sel: 0, query: "", typing: false };
const root = mountOverlay("openfiles");
root.innerHTML =
  `<div id="openfiles-box"><div id="openfiles-title"><div id="openfiles-tabs">` +
  `<span data-tab="agents">Agents <i></i></span><span data-tab="tasks">AI tasks <i></i></span><span data-tab="files">Open files <i></i></span>` +
  `</div></div><div id="openfiles-query" class="hidden"></div><div id="openfiles-list"></div></div>`;
root.addEventListener("click", (e) => {
  if (e.target.id === "openfiles") return closeSwitcher();
  const tab = e.target.closest("#openfiles-tabs span[data-tab]");
  if (tab) return showTab(tab.dataset.tab);
  const row = e.target.closest(".ofrow[data-i]");
  if (!row) return;
  sw.sel = Number(row.dataset.i);
  activate();
});

// The console's ctrl+\ (console.js) asks for the switcher through a document
// event: the two modules must not import each other.
document.addEventListener("gg:switcher", () => (isOpen() ? closeSwitcher() : openSwitcher()));

const FOOT_FILES =
  `<span>↑↓ j k move</span><button data-oact="enter">enter bring back</button>` +
  `<button data-oact="x">x close file</button><button data-oact="tab">tab next tab</button><button data-oact="esc">esc close</button>`;
const FOOT_AGENTS =
  `<span>↑↓ j move</span><button data-oact="enter">enter open console</button>` +
  `<button data-oact="kill">k kill</button><button data-oact="killrm">X kill + remove</button><button data-oact="x">x remove</button>` +
  `<button data-oact="filter">/ filter</button><button data-oact="tab">tab next tab</button><button data-oact="esc">esc close</button>`;
const FOOT_TASKS =
  `<span>↑↓ j k move</span><button data-oact="enter">enter open its console</button>` +
  `<button data-oact="tab">tab next tab</button><button data-oact="esc">esc close</button>`;

$("foot").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-oact]");
  if (!b) return;
  if (b.dataset.oact === "enter") activate();
  else if (b.dataset.oact === "x") sw.tab === "agents" ? agentAct("x") : closeSelected();
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
  sw.tab = tab;
  sw.typing = false;
  $("openfiles-query").classList.add("hidden");
  for (const el of root.querySelectorAll("#openfiles-tabs span")) el.classList.toggle("on", el.dataset.tab === tab);
  const mineFile = viewerFileId();
  const mineSess = consoleSessionId();
  const first = tab === "files" ? Math.max(0, sw.files.findIndex((f) => f.id === mineFile)) : tab === "agents" ? Math.max(0, sw.sessions.findIndex((s) => s.id === mineSess)) : 0;
  sw.sel = first;
  pushFoot("openfiles", tab === "files" ? FOOT_FILES : tab === "agents" ? FOOT_AGENTS : FOOT_TASKS);
  renderSwitcher();
}

// switcherOpenFiles: the list changed while the switcher is up (another tab,
// an eviction, a stream ending) — re-render from it, never act on a stale row.
function switcherOpenFiles(files) {
  const cur = sw.files[sw.sel] && sw.files[sw.sel].id;
  sw.files = files;
  if (!isOpen()) return;
  if (sw.tab === "files") {
    const i = files.findIndex((f) => f.id === cur);
    sw.sel = clampSel(i >= 0 ? i : sw.sel, files.length);
  }
  renderSwitcher();
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
  const c = { agents: sw.sessions.length, tasks: sw.tasks.length, files: sw.files.length };
  for (const el of root.querySelectorAll("#openfiles-tabs span")) el.querySelector("i").textContent = c[el.dataset.tab];
}

function renderSwitcher() {
  counts();
  const list = $("openfiles-list");
  const cols = Math.floor(list.clientWidth / charWidth()) - 4;
  let html = "";
  if (sw.tab === "files") {
    const rows = switcherRows(sw.files, viewerFileId());
    html = rows
      .map((r, i) => {
        const meta = r.line + "  " + versionLabel(r.source, r.rev) + (r.notes ? "  · " + r.notes + (r.notes === 1 ? " note" : " notes") : "");
        const room = cols - 2 - meta.length;
        return (
          `<div class="ofrow${i === sw.sel ? " sel" : ""}" data-i="${i}" title="${esc(r.path)}">` +
          `${r.mark} ${esc(room > 0 ? elidePath(r.path, room) : r.path)}<span class="ofmeta">${esc(meta)}</span></div>`
        );
      })
      .join("");
    if (!rows.length) html = `<div class="ofempty">no open files — view file on a file row opens one</div>`;
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
          `<div class="ofrow${j === sw.sel ? " sel" : ""}${r.task ? " task" : ""}" data-i="${j}" title="${esc(r.label)}">` +
          `<span class="glyph ${r.state === "exited" ? "ex" : "run"}">${r.glyph}</span> ${esc(elidePath(r.label, Math.max(8, cols - 30)))}` +
          `<span class="ofmeta">${esc(r.meta)}</span></div>`
        );
      })
      .join("");
    if (!rows.length) html = `<div class="ofempty">${sw.query ? "no session matches" : "no agent sessions — a worktree's menu starts one"}</div>`;
  } else {
    const rows = taskRows(sw.tasks, Date.now());
    html = rows
      .map((r, i) =>
        `<div class="ofrow${i === sw.sel ? " sel" : ""}" data-i="${i}" title="${esc(r.key)}">` +
        `<span class="glyph ${r.state === "running" || r.state === "result-ready" ? "run" : "ex"}">◆</span> ${esc(elidePath(r.key, Math.max(8, cols - 34)))}` +
        `<span class="ofmeta">${esc(r.agent + " · " + r.state + " · " + r.age)}</span></div>`)
      .join("");
    if (!rows.length) html = `<div class="ofempty">no AI tasks</div>`;
  }
  list.innerHTML = html;
  const cur = list.querySelector(".ofrow.sel");
  if (cur) cur.scrollIntoView({ block: "nearest" });
}

function currentCount() {
  return sw.tab === "files" ? sw.files.length : sw.tab === "agents" ? visibleSessions().length : sw.tasks.length;
}

// activate: enter on the selected row of the current tab.
function activate() {
  if (sw.tab === "files") return bringBack();
  if (sw.tab === "agents") {
    const s = visibleSessions()[sw.sel];
    if (!s) return;
    closeSwitcher();
    openConsole(s.id);
    return;
  }
  const t = sw.tasks[sw.sel];
  if (!t) return;
  if (t.session) {
    closeSwitcher();
    openConsole(t.session);
  } else {
    opLine("a finished task has no console — its result is not shown here yet", false);
  }
}

function bringBack() {
  const f = sw.files[sw.sel];
  if (!f) return;
  closeSwitcher();
  openViewer({ id: f.id });
}

// closeSelected: x closes the file in EVERY tab (ruling L4). This tab's own
// viewer on it goes first, so it does not read "closed in another tab".
async function closeSelected() {
  if (sw.tab !== "files") return;
  const f = sw.files[sw.sel];
  if (!f) return;
  if (f.id === viewerFileId()) dropViewer();
  else viewerClosedFile(f.id); // still loading here, perhaps: it must not land
  let ans;
  try {
    ans = await postJSON("/api/open-files", { op: "close", id: f.id, tab: tabId, everywhere: true });
  } catch (e) {
    return opLine("close failed: " + (e.message || e), true);
  }
  sw.files = ans.files || [];
  sw.sel = clampSel(sw.sel, sw.files.length);
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
  if (sw.tab !== "agents") return;
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
  switch (e.key) {
    case "ArrowDown": case "j": sw.sel = clampSel(sw.sel + 1, currentCount()); renderSwitcher(); break;
    case "ArrowUp": sw.sel = clampSel(sw.sel - 1, currentCount()); renderSwitcher(); break;
    case "k": // the TUI popup's kill on the Agents tab; "up" on the others
      if (sw.tab === "agents") agentAct("k");
      else { sw.sel = clampSel(sw.sel - 1, currentCount()); renderSwitcher(); }
      break;
    case "X": if (sw.tab === "agents") agentAct("X"); break;
    case "Tab": showTab(TABS[(TABS.indexOf(sw.tab) + (e.shiftKey ? TABS.length - 1 : 1)) % TABS.length]); break;
    case "Enter": activate(); break;
    case "x": if (sw.tab === "agents") agentAct("x"); else closeSelected(); break;
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
  key: "open files",
  html:
    "files opened in the viewer stay open, one list per worktree shared by every tab: <b>ctrl+]</b> in the viewer sends the file to the background, " +
    "<b>ctrl+\\</b> opens the switcher on its Open files tab (<b>●</b> the one this tab shows) — <b>enter</b> brings one back where this tab left it, <b>x</b> closes it in every tab. " +
    "A working-tree file follows the disk.",
});

export { isSwitcherKey, openSwitcher, switcherOpenFiles, switcherSessions };
