// review.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
//
// The doors every AI review and AI resolve goes through, and the report
// viewer a resolution overview opens in. A review or resolve is an AI task
// now (launch.js): the launch dialog picks the agent and mode, the task
// scheduler runs it, and its result lands where the TUI puts it — there is no
// lane to hold or park.
import { $, esc, getJSON, postJSON, state } from "./core.js";
import { askApproval, openLaunch } from "./launch.js";
import { mdHTML } from "./markdown.js";
import { closeLayer, copyText, openPrompt, pushLayer, showCtxMenu } from "./layers.js";
import { opLine, refreshAfterOp, showLocalConfirm, startOp } from "./ops.js";
import { closeConsole, consoleSessionId, openConsole, removeSession } from "./console.js";
import { measure } from "./sessions.js";

// startReview is every review row's door: the launch dialog (launch.js), the
// TUI's Review … → launch dialog.
function startReview(target, branch, sha, preview, shas) {
  return openLaunch({ kind: "review", target, branch, sha, preview, shas });
}


// startConflictAI is the conflict banner's tools button — the TUI's [t] in
// the conflict window: a picker with "Resolve with an agent…" (the agent
// resolves the files and leaves the operation paused) and "Resolve &
// complete with an agent…" (it also continues), each offered only when an
// agent is configured for it; either row opens the launch dialog.
async function startConflictAI() {
  // The in-place tools first (per-file mergetools — on the focused conflicted
  // file — and commands that ask for input), then the agents.
  const file = focusedConflict();
  const inPlace = await getJSON("/api/conflict/tools?file=" + encodeURIComponent(file)).then((g) => g.tools || [], () => []);
  const kinds = [
    { kind: "conflict", label: "Resolve with an agent…" },
    { kind: "conflict_complete", label: "Resolve & complete with an agent…" },
  ];
  const offered = await Promise.all(
    kinds.map((k) => getJSON("/api/tasks/launch?kind=" + k.kind).then((g) => (g.choices || []).length > 0, () => false))
  );
  const rows = [
    ...inPlace.map((t) => ({ label: t.name + (t.per_file ? " — " + file : ""), act: () => runConflictTool(t, file) })),
    ...kinds.filter((_, i) => offered[i]).map((k) => ({ label: k.label, act: () => openLaunch({ kind: k.kind }) })),
  ];
  if (!rows.length) {
    if (state.conflict && !state.conflict.op) return opLine("no tool for this conflict — the agents need a paused operation (Settings → External tools)", true);
    return opLine("no external tools configured — Settings → External tools", true);
  }
  const r = $("conflict-ai").getBoundingClientRect();
  showCtxMenu([{ header: "tools for these conflicts" }, ...rows], r.left, r.bottom + 4);
}

$("conflict-ai").addEventListener("click", startConflictAI);


// focusedConflict is the conflicted file under the files cursor ("" when the
// cursor is elsewhere): a per-file mergetool acts on it.
function focusedConflict() {
  if (state.filesMode !== "status") return "";
  const e = (state.statusEntries || [])[state.fileCursor];
  return e && e.section === "conflicts" ? e.path : "";
}


// askValue collects one <user:…> value (the TUI's tool inputs form, one
// field at a time); a closed prompt simply never resolves.
function askValue(label) {
  return new Promise((resolve) => openPrompt({ title: label + ":", allowEmpty: true, onSubmit: (v) => resolve(v) }));
}


// toolRuns are the in-place tools this tab started: session id → name. They
// survive a reload of this tab (sessionStorage — per tab, so another tab
// never takes this one's outcome); the server finishes a run when its
// session ends and keeps the outcome for the page to collect.
const TOOL_RUNS_KEY = "gg-tool-runs";
const toolRuns = new Map(loadToolRuns());

function loadToolRuns() {
  try {
    return JSON.parse(sessionStorage.getItem(TOOL_RUNS_KEY) || "[]");
  } catch {
    return [];
  }
}

function saveToolRuns() {
  try {
    sessionStorage.setItem(TOOL_RUNS_KEY, JSON.stringify([...toolRuns]));
  } catch {
    // no storage: this tab forgets its runs on reload
  }
}

// runConflictTool runs an in-place tool the TUI's way: its inputs first, the
// resolved command shown before its first run, then — there being no
// terminal to hand over — in a session the console shows. Its end is
// handled by toolSessions.
async function runConflictTool(t, file) {
  const inputs = {};
  for (const l of t.labels || []) inputs[l] = await askValue(l);
  const body = { name: t.name, category: t.category, file: t.per_file ? file : "", inputs, ...measure() };
  let got;
  try {
    got = await postJSON("/api/conflict/tool", body);
  } catch (e) {
    if (!(e.data && e.data.needs_approval)) return opLine(t.name + ": " + (e.message || e), true);
    if (!(await askApproval(e.data.tool, e.data.command))) return;
    try {
      got = await postJSON("/api/conflict/tool", { ...body, approve: true });
    } catch (e2) {
      return opLine(t.name + ": " + (e2.message || e2), true);
    }
  }
  toolRuns.set(got.session, t.name);
  saveToolRuns();
  openConsole(got.session);
  toolSessions(state.sessions || []); // a tool that already ended
}


// toolSessions takes every sessions event (live.js): one of this tab's tool
// runs whose session exited — or is gone (killed and removed) — is asked
// about once.
function toolSessions(list) {
  for (const [id, name] of toolRuns) {
    const s = list.find((x) => x.id === id);
    if (s && s.state !== "exited") continue;
    if (!s && !list.length) continue; // no list yet
    toolFinished(id, name, s);
  }
}

// toolFinished is the TUI's toolFinished over the server's outcome: a failure
// is reported (no mark, no overview), an interrupt takes the success path
// with a word, a per-file run that changed its file offers mark resolved, a
// clean resolve & complete run opens its overview; the status reloads.
async function toolFinished(id, name, s) {
  let done;
  try {
    done = await postJSON("/api/conflict/tool/done", { session: id });
  } catch (e) {
    if (e.data && /still running/.test(e.message || "")) return; // the next event asks again
    toolRuns.delete(id);
    saveToolRuns();
    return; // collected elsewhere, or expired
  }
  toolRuns.delete(id);
  saveToolRuns();
  if (consoleSessionId() === id) closeConsole();
  if (s && s.state === "exited") removeSession(s);
  refreshAfterOp();
  if (done.failed) return opLine(name + ": exit status " + done.exit_code, true);
  if (done.interrupted) opLine("tool interrupted");
  if (done.per_file && done.changed) {
    return showLocalConfirm("The tool changed " + done.file + ".\n\nMark it as resolved (git add)?", ["mark resolved", "not now"], (o) => {
      if (o === "mark resolved") startOp({ op: "resolve-conflict", path: done.file, mode: "mark" }, "mark resolved — " + done.file);
    });
  }
  if ((done.overview || "").trim()) return openReport("Resolution overview — " + name, "", done.overview);
  if (done.complete && !done.interrupted) opLine(name + " reported no overview");
}


// reportWhere is the report viewer's path line: the note a review is stored
// as, or — for one the store could not keep — that it was not saved (the
// error line under the viewer is covered while it is open). "" for a
// conflict run's overview.
function reportWhere(noteId, unsaved) {
  if (noteId) return "note " + noteId;
  return unsaved ? "not saved — " + unsaved : "";
}

// The report viewer. A structured review (the gg review document) shows its
// overview as rendered markdown — parsed server-side, as for PR bodies — then
// its meta and one "path:line — summary" row per note; anything else is shown
// as plain text, with a warning when a review was not the document. unsaved
// is the save error of a review the store could not keep.
function openReport(title, noteId, content, doc, unsaved) {
  $("report-title").textContent = title;
  const structured = !!(doc && doc.structured);
  $("report-doc").style.display = structured ? "" : "none";
  $("report-body").style.display = structured ? "none" : "";
  $("report-warn").style.display = doc && !structured ? "" : "none";
  $("report-warn").textContent = doc && !structured ? "not in gg review format — shown as text" : "";
  if (structured) {
    $("report-md").innerHTML = mdHTML(doc.summaryMd, esc);
    $("report-meta").textContent = doc.docMeta;
    $("report-notes").innerHTML = doc.notes
      .map((n) => `<li>${esc(n.path + ":" + n.line + " — " + n.summary)}${n.meta ? `<span class="meta">${esc(n.meta)}</span>` : ""}</li>`)
      .join("");
  }
  $("report-body").textContent = content || "";
  const where = reportWhere(noteId, unsaved);
  $("report-path").textContent = where;
  $("report-path").title = where;
  $("report-body").scrollTop = 0;
  $("report-doc").scrollTop = 0;
  pushLayer("report", $("report"));
}


$("report-close").addEventListener("click", () => closeLayer("report"));

$("report-copy").addEventListener("click", () => copyText($("report-body").textContent, "the report"));
// previews.js starts a preview review through this hand-off (the
// __ggAddPreview precedent): importing review.js there would close a cycle
// through ops.js.
window.__ggStartReview = startReview;

export { openReport, startConflictAI, startReview, toolSessions };
