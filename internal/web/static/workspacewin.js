// workspacewin.js — the Workspaces window and the questions a workspace change
// asks on the page (spec 2026-10-31-web-workspaces): the TUI popup's screens and
// keys, every edit saved at once by the workspace's owner.
import { $, esc, getJSON, postJSON, state } from "./core.js";
import { closeLayer, pushLayer, topLayer } from "./layers.js";
import { opLine } from "./ops.js";
import { DROP_OPTIONS, definedError, dropAnswer, launchAnswer, launchOptions, loadWorkspace, wsDefined, wsMembers, wsQuestion } from "./workspace.js";


let win = null;
let picks = [];

function up() {
  return !!topLayer() && topLayer().id === "workspaces";
}

function show() {
  if (!up()) pushLayer("workspaces", $("ws-win"), { onKey });
  render();
}

function close() {
  win = null;
  closeLayer("workspaces");
}

// post sends a request, asking about running sessions when the owner says so.
// It answers true when the change happened.
async function post(req) {
  try {
    const r = await postJSON("/api/workspace", req);
    if (r && r.status) opLine(r.status);
    await loadWorkspace();
    return true;
  } catch (e) {
    if (e.data && e.data.drop_sessions) return askDrop(req, e.data.drop_sessions);
    opLine("error: " + (e.message || e), true);
    return false;
  }
}

function askDrop(req, sessions) {
  return new Promise((resolve) => {
    const back = win;
    const body = sessions.map((s) => s.label || s.name || s.id).join("\n");
    ask(
      "These repositories have running sessions",
      body,
      DROP_OPTIONS,
      (i) => {
        win = back;
        const drop = dropAnswer(i);
        if (!drop) {
          if (win) render();
          else close();
          return resolve(false);
        }
        post({ ...req, drop }).then((ok) => {
          if (win) render();
          else close();
          resolve(ok);
        });
      }
    );
  });
}

function ask(title, body, options, pick, kind = "ask") {
  win = { kind, title, body, options, pick, sel: 0 };
  show();
}

function entry(name) {
  return wsDefined().find((d) => d.name.toLowerCase() === String(name).toLowerCase());
}

function rowsOfWin() {
  if (win.kind === "ask" || win.kind === "launch") return win.options;
  if (win.kind === "list") return wsDefined();
  if (win.kind === "members") return (entry(win.name) || { repos: [] }).repos;
  if (win.kind === "picker") return picks;
  return [];
}

const KEYS = {
  list: ["n new", "r rename", "d delete", "o open now", "enter members", "esc close"],
  members: ["a add", "d remove", "K up", "J down", "s settings repo", "esc back"],
  picker: ["enter add", "esc back"],
};

function row(text, detail, i, cls = "") {
  return `<li class="${i === win.sel ? "sel" : ""} ${cls}" data-i="${i}"><span>${esc(text)}</span><span class="detail">${esc(detail || "")}</span></li>`;
}

function render() {
  const box = $("ws-box");
  const keys = (KEYS[win.kind] || []).map((k) => `<button data-k="${esc(k.split(" ")[0])}">${esc(k)}</button>`).join("");
  const foot = keys ? `<div class="ws-keys">${keys}</div>` : "";
  if (win.kind === "ask" || win.kind === "launch") {
    box.innerHTML = `<h2>${esc(win.title)}</h2>${win.body ? `<pre>${esc(win.body)}</pre>` : ""}<ul id="ws-list">${win.options.map((o, i) => row(o, "", i)).join("")}</ul>`;
    return;
  }
  if (win.kind === "input") {
    box.innerHTML = `<h2>${esc(win.title)}</h2><input id="ws-input" value="${esc(win.value || "")}" autocomplete="off" spellcheck="false"><div class="ws-hint" id="ws-err">enter saves · esc goes back</div>`;
    $("ws-input").focus();
    $("ws-input").select();
    return;
  }
  win.sel = Math.max(0, Math.min(win.sel, rowsOfWin().length - 1));
  if (win.kind === "list") {
    const open = state.ws && state.ws.name;
    box.innerHTML = `<h2>Workspaces</h2><ul id="ws-list">${
      wsDefined().map((d, i) => row((d.name === open ? "● " : "") + d.name, d.repos.map((r) => r.label).join(", "), i)).join("") ||
      `<li class="ws-dim"><span>${esc(definedError() || "no workspaces yet: n creates one")}</span></li>`
    }</ul>${foot}`;
  } else if (win.kind === "members") {
    const e = entry(win.name);
    if (!e) {
      win = { kind: "list", sel: 0 };
      return render();
    }
    box.innerHTML = `<h2>${esc(e.name)} — repositories</h2><ul id="ws-list">${
      e.repos.map((r, i) => row(r.label + (e.settings_repo === r.path ? "  ✎" : "") + (r.missing ? " (missing)" : ""), r.path, i, r.missing ? "ws-dim" : "")).join("") ||
      '<li class="ws-dim"><span>no repositories yet: a adds one</span></li>'
    }</ul>${foot}`;
  } else {
    box.innerHTML = `<h2>Add to ${esc(win.name)}</h2><ul id="ws-list">${picks.map((p, i) => row(p.label, p.path || "type a path", i)).join("")}</ul>${foot}`;
  }
}

// openPicker lists what can join: the repositories open now, recents, then Path….
async function openPicker(name) {
  const e = entry(name);
  const has = (p) => e && e.repos.some((r) => r.path === p);
  const list = await getJSON("/api/repos").catch(() => ({ repos: [] }));
  const seen = new Set();
  picks = [];
  for (const r of [...wsMembers().map((m) => ({ label: m.label, path: m.path })), ...(list.repos || []).map((r) => ({ label: r.name, path: r.path }))]) {
    if (has(r.path) || seen.has(r.path)) continue;
    seen.add(r.path);
    picks.push(r);
  }
  picks.push({ label: "Path…", path: "" });
  win = { kind: "picker", name, sel: 0 };
  render();
}

function input(title, value, done, back) {
  win = { kind: "input", title, value, done, back };
  render();
}

async function act(key) {
  if (win.kind === "ask" || win.kind === "launch") {
    if (key === "Enter") return win.pick(win.sel);
    if (key === "Escape") return win.pick(-1);
    return;
  }
  if (win.kind === "input") {
    if (key === "Escape") {
      win = win.back;
      return render();
    }
    if (key === "Enter") {
      const v = $("ws-input").value.trim();
      const w = win;
      if (await w.done(v)) {
        win = w.next ? w.next(v) : w.back;
        render();
      }
    }
    return;
  }
  if (win.kind === "list") {
    const d = wsDefined()[win.sel];
    if (key === "Escape") return close();
    if (key === "n") {
      input("New workspace", "", (v) => post({ kind: "edit", edit: "new", name: v }), win);
      win.next = (v) => ({ kind: "members", name: v, sel: 0 });
      return;
    }
    if (!d) return;
    if (key === "r") {
      const back = win;
      input("Rename " + d.name, d.name, (v) => post({ kind: "edit", edit: "rename", name: d.name, args: [v] }), back);
    } else if (key === "d") {
      const back = win;
      ask("Delete workspace " + d.name + "?", "", ["Keep it", "Delete"], async (i) => {
        win = back;
        if (i === 1) await post({ kind: "edit", edit: "delete", name: d.name });
        if (win) render();
      });
    } else if (key === "o") {
      const name = d.name;
      if (state.ws && state.ws.name === name) return close();
      if (await post({ kind: "open", name })) close();
    } else if (key === "Enter") {
      win = { kind: "members", name: d.name, sel: 0 };
      render();
    }
    return;
  }
  if (win.kind === "members") {
    const name = win.name;
    const m = (entry(name) || { repos: [] }).repos[win.sel];
    if (key === "Escape") {
      win = { kind: "list", sel: Math.max(0, wsDefined().findIndex((d) => d.name === name)) };
      return render();
    }
    if (key === "a") return openPicker(name);
    if (!m) return;
    if (key === "d") await post({ kind: "edit", edit: "remove", name, args: [m.path] });
    else if (key === "K" || key === "J") {
      const d = key === "K" ? -1 : 1;
      if (await post({ kind: "edit", edit: "move", name, args: [m.path, String(d)] })) win.sel += d;
    } else if (key === "s") await post({ kind: "edit", edit: "settings", name, args: [m.path] });
    if (win) render();
    return;
  }
  if (key === "Escape") {
    win = { kind: "members", name: win.name, sel: 0 };
    return render();
  }
  if (key === "Enter") {
    const name = win.name;
    const p = picks[win.sel];
    const back = { kind: "members", name, sel: 0 };
    if (p && !p.path) return input("Repository path", "", (v) => post({ kind: "edit", edit: "add", name, args: [v] }), back);
    if (p && (await post({ kind: "edit", edit: "add", name, args: [p.path] }))) win = back;
    render();
  }
}

function onKey(e) {
  if (!win) return false;
  if (win.kind === "input" && e.key !== "Enter" && e.key !== "Escape") return true; // the field's
  e.preventDefault();
  const n = rowsOfWin().length;
  if (e.key === "ArrowUp" || (e.key === "k" && win.kind !== "input")) {
    win.sel = Math.max(0, win.sel - 1);
    render();
  } else if (e.key === "ArrowDown" || (e.key === "j" && win.kind !== "input")) {
    win.sel = Math.min(n - 1, win.sel + 1);
    render();
  } else act(e.key);
  return true;
}

$("ws-box").addEventListener("click", (e) => {
  if (!win) return;
  const b = e.target.closest("button[data-k]");
  if (b) return act({ enter: "Enter", esc: "Escape" }[b.dataset.k] || b.dataset.k);
  const li = e.target.closest("li[data-i]");
  if (li) {
    win.sel = Number(li.dataset.i);
    act("Enter");
  }
});

function openWorkspacesWindow() {
  const i = wsDefined().findIndex((d) => state.ws && d.name === state.ws.name);
  win = { kind: "list", sel: Math.max(0, i) };
  loadWorkspace().then(() => win && show());
}

// leaveWorkspace is the palette's Leave workspace.
function leaveWorkspace() {
  return post({ kind: "leave" });
}

// openWorkspaceNamed is R's enter on a workspace.
function openWorkspaceNamed(name) {
  return post({ kind: "open", name });
}

// The launch question: on whichever tab, until any tab answers.
document.addEventListener("gg:workspace-question", () => {
  const q = wsQuestion();
  if (win && win.kind === "launch" && !q) return close();
  if (!q || (win && win.kind === "launch")) return;
  if (topLayer() && !up()) return; // another window owns the keyboard: the next word asks
  ask("This repository belongs to a workspace", "", launchOptions(q), (i) => {
    const name = launchAnswer(q, i);
    close();
    post({ kind: "launch", name });
  }, "launch");
});

export { leaveWorkspace, openWorkspaceNamed, openWorkspacesWindow };
