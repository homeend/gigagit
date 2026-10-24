// review.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
//
// The doors every AI review and AI resolve goes through, and the report
// viewer a resolution overview opens in. A review or resolve is an AI task
// now (launch.js): the launch dialog picks the agent and mode, the task
// scheduler runs it, and its result lands where the TUI puts it — there is no
// lane to hold or park.
import { $, esc } from "./core.js";
import { openLaunch } from "./launch.js";
import { mdHTML } from "./markdown.js";
import { closeLayer, copyText, pushLayer } from "./layers.js";

// startReview is every review row's door: the launch dialog (launch.js), the
// TUI's Review … → launch dialog.
function startReview(target, branch, sha, preview, shas) {
  return openLaunch({ kind: "review", target, branch, sha, preview, shas });
}


// startConflictAI is the conflict banner's AI resolve: the launch dialog for
// a resolve & complete agent.
function startConflictAI() {
  return openLaunch({ kind: "conflict_complete" });
}

$("conflict-ai").addEventListener("click", startConflictAI);


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

export { openReport, startConflictAI, startReview };
