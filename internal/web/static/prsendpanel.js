// prsendpanel.js — the Send to GitHub… panel (spec §5.4, R6/R7): every unsent
// local comment of a pull request — each AI review's remarks, my notes, the
// draft replies — nothing ticked on open; the user ticks any mix, picks a
// body, and ONE GitHub review goes out through the ordinary confirm
// (sendToGitHub → the decision modal). Row building is pure (prsendrows.js,
// node-tested); this file owns the fetch, the overlay, the keys and the
// page state: the ticks and the body are kept per PR while the page lives,
// so enter (a row's file in the diff) and esc do not lose them.
import { $, esc, getJSON, state } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { opLine } from "./ops.js";
import { landNote, openFile } from "./files.js";
import { bodyOptions, panelRequest, panelRows, tickAllInGroup } from "./prsendrows.js";
import { onHeadMoved, onSendDone, sendToGitHub } from "./prsend.js";

const kept = new Map(); // pr -> { ticked: Set, body: {kind, from, typed} }
let panel = null; // { pr, cands, sel, open: Set(row ids unfolded), code: bool, notice }

function keptFor(pr) {
  if (!kept.has(pr)) kept.set(pr, { ticked: new Set(), body: { kind: "none", from: "", typed: "" } });
  return kept.get(pr);
}

export async function openSendPanel(n) {
  let cands;
  try {
    cands = await getJSON("/api/pr/send/candidates?n=" + n);
  } catch (e) {
    opLine("send to #" + n + ": " + (e.message || e), true);
    return;
  }
  if (!(cands.groups || []).length) {
    opLine("nothing to send to #" + n, false);
    return;
  }
  const k = keptFor(n);
  panel = { pr: n, cands, sel: 0, open: new Set(), code: false, notice: "" };
  if (k.body.kind === "review" && !cands.groups.some((g) => g.id === "review:" + k.body.from)) k.body = { kind: "none", from: "", typed: k.body.typed };
  $("prsendpanel-title").textContent = "Send to GitHub — #" + n;
  pushLayer("prsendpanel", $("prsendpanel"), { onKey });
  panel.sel = firstCandidate();
  render();
}

function close() {
  panel = null;
  closeLayer("prsendpanel");
}

function rows() {
  return panelRows(panel.cands, keptFor(panel.pr).ticked);
}

// firstCandidate is where the cursor starts: the first row that can be
// ticked, else the first row at all (every row skipped), else the top.
function firstCandidate() {
  const rs = rows();
  const i = rs.findIndex((r) => r.kind === "row" && r.tickable);
  return Math.max(0, i >= 0 ? i : rs.findIndex((r) => r.kind === "row"));
}

function groupHead(r) {
  if (r.gkind === "review") return (r.agent || "AI") + (r.title ? " · " + r.title : "");
  return r.gkind === "mine" ? "My notes" : "Draft replies";
}

function render() {
  if (!panel) return;
  const k = keptFor(panel.pr), rs = rows();
  const n = rs.filter((r) => r.kind === "row" && r.ticked).length;
  $("prsendpanel-list").innerHTML = rs
    .map((r, i) => {
      const sel = i === panel.sel ? " sel" : "";
      if (r.kind === "group") {
        return (
          `<li class="pgrp g${r.slot}${sel}" data-i="${i}"><input type="checkbox" data-g="${esc(r.id)}"${r.all ? " checked" : ""}${r.ticked && !r.all ? ' class="some"' : ""}> ` +
          `${esc(groupHead(r))}${r.created ? `<span class="pdate">${esc(r.created.slice(0, 10))}</span>` : ""}</li>`
        );
      }
      const box = r.tickable ? `<input type="checkbox" data-id="${esc(r.id)}"${r.ticked ? " checked" : ""}>` : `<span class="pskip">–</span>`;
      const sev = r.severity ? `<span class="psev ${esc(r.severity)}">${esc(r.severity)}</span>` : "";
      const more =
        panel.open.has(r.id) || panel.code
          ? `<div class="pmore">${r.rationale ? `<div class="prat">${esc(r.rationale)}</div>` : ""}${r.code.length ? `<pre class="pcode">${esc(r.code.join("\n"))}</pre>` : ""}</div>`
          : "";
      return (
        `<li class="prow g${r.slot}${sel}${r.tickable ? "" : " pdis"}" data-i="${i}">${box} ${sev}<a class="pwhere" data-id="${esc(r.id)}">${esc(r.where)}</a> ${esc(r.summary)}` +
        `${r.skip ? `<span class="pskipwhy"> (skipped: ${esc(r.skip)})</span>` : ""}<span class="pfold">${r.rationale || r.code.length ? "▸" : ""}</span>${more}</li>`
      );
    })
    .join("");
  const sel = $("prsendpanel-bodysel");
  const opts = bodyOptions(panel.cands, k.ticked);
  let value = k.body.kind === "review" ? "review:" + k.body.from : k.body.kind;
  if (!opts.some((o) => o.value === value)) {
    k.body = { kind: "none", from: "", typed: k.body.typed }; // its review was unticked
    value = "none";
  }
  sel.innerHTML = opts.map((o) => `<option value="${esc(o.value)}"${o.value === value ? " selected" : ""}>${esc(o.label)}</option>`).join("");
  $("prsendpanel-typed").classList.toggle("hidden", k.body.kind !== "typed");
  if (document.activeElement !== $("prsendpanel-typed")) $("prsendpanel-typed").value = k.body.typed || "";
  $("prsendpanel-send").textContent = n ? "Send " + n : "Send";
  $("prsendpanel-send").disabled = n === 0;
  $("prsendpanel-notice").textContent = panel.notice ? "▸ " + panel.notice : "";
  $("prsendpanel-list").querySelector("li.sel")?.scrollIntoView({ block: "nearest" });
}

function move(d) {
  const n = rows().length;
  panel.sel = Math.max(0, Math.min(n - 1, panel.sel + d));
  panel.notice = "";
  render();
}

function tick(r) {
  const k = keptFor(panel.pr);
  if (r.kind === "group") k.ticked = tickAllInGroup(panel.cands, k.ticked, r.id);
  else if (!r.tickable) panel.notice = r.where + " " + r.summary + " — skipped: " + r.skip;
  else k.ticked.has(r.id) ? k.ticked.delete(r.id) : k.ticked.add(r.id);
  render();
}

// cycleBody is b: none → each ticked review's text (newest first) → typed → none.
function cycleBody() {
  const k = keptFor(panel.pr), opts = bodyOptions(panel.cands, k.ticked);
  const cur = k.body.kind === "review" ? "review:" + k.body.from : k.body.kind;
  const next = opts[(Math.max(0, opts.findIndex((o) => o.value === cur)) + 1) % opts.length].value;
  setBody(next);
}

function setBody(value) {
  const k = keptFor(panel.pr);
  if (value === "typed") k.body = { kind: "typed", from: "", typed: k.body.typed };
  else if (value.startsWith("review:")) k.body = { kind: "review", from: value.slice("review:".length), typed: k.body.typed };
  else k.body = { kind: "none", from: "", typed: k.body.typed };
  render();
  if (value === "typed") $("prsendpanel-typed").focus();
}

// openRow is enter / a path click: the row's file in the PR diff, the
// cursor on its thread; the panel closes and reopens with its ticks (A6
// on the web: the page keeps them per PR).
function openRow(r) {
  const po = state.previewOpen, path = r.where.split(":")[0];
  if (!po || po.pr !== panel.pr || state.filesMode !== "compare") {
    panel.notice = "open the pull request to see the file";
    return render();
  }
  const i = (state.files || []).findIndex((f) => f.path === path);
  if (i < 0) {
    panel.notice = path + " is not in the pull request's file list";
    return render();
  }
  close();
  landNote(r.id);
  openFile(i);
}

function send() {
  const k = keptFor(panel.pr);
  if (k.body.kind === "typed") k.body.typed = $("prsendpanel-typed").value;
  const req = panelRequest(panel.cands, k.ticked, k.body);
  if (!req) {
    panel.notice = "tick something to send";
    return render();
  }
  const pr = panel.pr;
  panel.notice = "preparing the send to #" + pr + "…";
  render();
  sendToGitHub(pr, req, "sending to #" + pr, (err) => {
    if (panel && panel.pr === pr) {
      panel.notice = "send: " + (err.message || err);
      render();
    }
  });
}

// A send of this PR that changed GitHub closes the panel and drops its
// kept ticks and typed body; an abort (a successful no-op: ok, not
// changed) or a failure leaves everything and says so.
onSendDone((n, ev, body) => {
  if (!body || body.kind !== "notes" || !body.verdict) return; // only the panel's own send
  if (!ev.ok || !ev.changed) {
    if (panel && panel.pr === n) {
      panel.notice = ev.ok ? "the send was cancelled — nothing posted" : "send: " + (ev.error || "failed");
      render();
    }
    return;
  }
  kept.delete(n);
  if (panel && panel.pr === n) close();
});

// The head moved: the diff re-opens on the new head; the panel's list is
// stale — it closes, the ticks stay for the reopen (ids no longer listed
// are ignored by panelRows).
onHeadMoved((n) => {
  if (panel && panel.pr === n) close();
});

function onKey(e) {
  if (!panel) return false;
  if (e.target === $("prsendpanel-typed")) {
    if (e.key === "Escape") {
      $("prsendpanel-typed").blur();
      e.preventDefault();
      return true;
    }
    if (e.key === "s" && e.ctrlKey) {
      e.preventDefault();
      send();
      return true;
    }
    return true; // typing
  }
  const rs = rows(), r = rs[panel.sel];
  const k = keptFor(panel.pr);
  switch (e.key) {
    case "Escape": close(); break;
    case "ArrowDown": case "j": move(1); break;
    case "ArrowUp": case "k": move(-1); break;
    case "PageDown": move(10); break;
    case "PageUp": move(-10); break;
    case "Home": panel.sel = 0; render(); break;
    case "End": panel.sel = rs.length - 1; render(); break;
    case " ": if (r) tick(r); break;
    case "a": if (r) { k.ticked = tickAllInGroup(panel.cands, k.ticked, r.kind === "group" ? r.id : r.group); render(); } break;
    case "b": cycleBody(); break;
    case "c": panel.code = !panel.code; render(); break;
    case "Enter": if (r && r.kind === "row") openRow(r); else if (r) tick(r); break;
    case "s": if (!e.ctrlKey) return false; send(); break;
    default: return false;
  }
  e.preventDefault();
  return true;
}

$("prsendpanel-close").addEventListener("click", close);
$("prsendpanel-send").addEventListener("click", send);
$("prsendpanel-bodysel").addEventListener("change", (e) => setBody(e.target.value));
$("prsendpanel-typed").addEventListener("input", (e) => { if (panel) keptFor(panel.pr).body.typed = e.target.value; });
$("prsendpanel").addEventListener("click", (e) => { if (e.target === $("prsendpanel")) close(); }); // the backdrop
$("prsendpanel-list").addEventListener("click", (e) => {
  if (!panel) return;
  const li = e.target.closest("li");
  if (!li) return;
  const i = Number(li.dataset.i), r = rows()[i];
  panel.sel = i;
  if (e.target.closest("input[type=checkbox]")) { tick(r); return; }
  if (e.target.closest("a.pwhere") && r.kind === "row") { openRow(r); return; }
  if (e.target.closest(".pfold") && r.kind === "row") panel.open.has(r.id) ? panel.open.delete(r.id) : panel.open.add(r.id);
  render();
});

// The PR menu's row — registered here, under prsend.js's separator (app.js
// imports this module right after it), so "Verdict…" follows it.
registerRows("pr", (pr) => {
  if (!pr || pr.state !== "open" || !pr.fetched) return [];
  return [{ label: "Send to GitHub…", act: () => openSendPanel(pr.number) }];
});

registerHelp({
  key: "Send to GitHub…",
  html:
    "a pull request's right-click menu opens one panel over every unsent local comment — each AI review's remarks, " +
    "<b>My notes</b>, <b>Draft replies</b> — nothing ticked. <b>space</b> ticks, <b>a</b> all/none in the group, " +
    "<b>b</b> cycles the body (none, a ticked review's text, typed), <b>c</b> shows the code under every row, " +
    "<b>enter</b> opens the row's file in the diff (reopen the panel from the PR menu — its ticks are kept), <b>ctrl+s</b> / Send posts ONE GitHub " +
    "review behind the ordinary confirm; a skipped row says why and cannot be ticked",
});
