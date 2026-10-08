// prsend.js — sending a pull request's notes to GitHub from the page (spec
// 2026-10-07 §4.2). Every send goes through POST /api/pr/send: the server
// plans it outside the repo gate, checks every id against the PR's notes,
// and the op parks on ONE confirm — drawn from that plan (sendplan.js) in
// the ordinary decision modal. Nothing is posted without that answer, and
// agents never send (user ruling 2026-10-08).
import { getJSON, postJSON, state } from "./core.js";
import { openPrompt, showCtxMenu } from "./layers.js";
import { followOp, opBusy, opLine } from "./ops.js";
import { registerHelp, registerRows } from "./menus.js";
import { fetchNotes, refreshNoteCounts } from "./files.js";
import { loadPRCounts } from "./previews.js";
import { sendRows } from "./prsendrows.js";

// Hooks for a finished send (prs.js: the freshness word, the interrupted
// bar) — registered, so this module never imports prs.js (which imports it).
const doneHooks = [];
export function onSendDone(fn) {
  doneHooks.push(fn);
}

// A send refused because the PR moved on GitHub hands the PR to these hooks
// (prs.js: fetch the new head and re-open the diff).
const headMovedHooks = [];
export function onHeadMoved(fn) {
  headMovedHooks.push(fn);
}

// sendToGitHub starts one send of PR n; label names it on the op line. A
// refusal (409 head moved, 400 a stale id, 422 nothing to send) is said and
// nothing runs; onRefused hears about it.
export async function sendToGitHub(n, body, label, onRefused) {
  if (opBusy()) {
    opLine("another operation is running — send again when it ends", true);
    return;
  }
  let resp;
  try {
    resp = await postJSON("/api/pr/send?n=" + n, body);
  } catch (err) {
    if (err.data && err.data.code === "head_moved") {
      // Nothing was sent: the notes would anchor on lines the PR no longer has.
      opLine("#" + n + " has new commits on GitHub — updating the diff; send again once it shows", true);
      for (const fn of headMovedHooks) fn(n);
    } else {
      opLine("send to #" + n + ": " + (err.message || err), true);
    }
    if (onRefused) onRefused(err);
    return;
  }
  followOp(resp.op_id, label, "pr-send", async (ev) => {
    if (ev.ok) opLine(ev.summary || "sent to #" + n);
    else opLine("send to #" + n + ": " + (ev.error || "failed"), true);
    await Promise.all([fetchNotes(), refreshNoteCounts(), loadPRCounts(n)]);
    for (const fn of doneHooks) fn(n, ev);
  });
  // followOp set state.op synchronously; the decision arrives later, on the
  // event stream, and reads the plan from here.
  if (state.op && state.op.id === resp.op_id) state.op.sendPlan = resp.plan;
}

// prOfCtx is the PR a diff context is the diff OF (0 = none): the rows exist
// in a PR's own diff only.
function prOfCtx(ctx) {
  return (ctx && ctx.preview && ctx.preview.pr) || 0;
}

// The pointer of the last right-click: a row's act() gets no event, and
// Send review…'s group pick opens where the menu was.
let lastXY = [200, 200];
document.addEventListener("contextmenu", (e) => (lastXY = [e.clientX, e.clientY]), true);

// replyAndSend writes a local draft reply to a GitHub thread, then sends it.
function replyAndSend(n, pr) {
  openPrompt({
    title: "Reply & send to “" + n.summary + "”",
    placeholder: "summary",
    body: { label: "rationale (optional)" },
    onSubmit: async (summary, rationale) => {
      let r;
      try {
        r = await postJSON("/api/notes/reply", { id: n.id, summary, rationale });
      } catch (e) {
        opLine("reply: " + (e.message || e), true);
        return;
      }
      await fetchNotes();
      sendToGitHub(pr, { kind: "notes", ids: [r.id] }, "sending the reply to #" + pr);
    },
  });
}

registerRows("note", ({ n, rootId, ctx }) => {
  const pr = prOfCtx(ctx);
  const acts = {
    send: () => sendToGitHub(pr, { kind: "notes", ids: [n.id] }, "sending to #" + pr),
    "send-review": () => sendReviewBody(pr, n.group || "mine"),
    "reply-send": () => replyAndSend(n, pr),
    resolve: () =>
      sendToGitHub(pr, { kind: n.resolved ? "unresolve" : "resolve", ids: [rootId] }, (n.resolved ? "reopening" : "resolving") + " a thread on #" + pr),
    "send-drafts": () =>
      sendToGitHub(pr, { kind: "notes", ids: (n.replies || []).filter((r) => r.source !== "forge").map((r) => r.id) }, "sending replies to #" + pr),
  };
  const rows = sendRows(n, pr).map((row) => ({ label: row.label, act: acts[row.id] }));
  return rows.length ? [{ sep: true }, ...rows] : [];
});

function groupCount(g) {
  if (g.id === "mine") return g.count === 1 ? "1 note" : g.count + " notes";
  return g.count === 1 ? "1 remark" : g.count + " remarks";
}

// keptBody is a body a refused or failed send left: the next Send review…
// of the same group (or Verdict…, group "verdict") starts from it, so typing
// is never lost.
let keptBody = null; // {pr, group, text}

// sendReviewBody is Send review… for one group: its body (an AI review's
// summary prefilled, empty for my draft review), then the confirm, whose
// buttons are the verdicts.
async function sendReviewBody(pr, group) {
  let g;
  try {
    const d = await getJSON("/api/pr/send/groups?n=" + pr);
    g = (d.groups || []).find((x) => x.id === group);
  } catch (e) {
    opLine("send review: " + (e.message || e), true);
    return;
  }
  if (!g) {
    opLine("send review: nothing of that group is left to send", true);
    return;
  }
  const kept = keptBody && keptBody.pr === pr && keptBody.group === group ? keptBody.text : null;
  const title =
    group === "mine"
      ? `Send my draft review to #${pr} (${groupCount(g)}) — the review body`
      : `Send ${g.agent || "AI"} review to #${pr} (${groupCount(g)}) — the review body`;
  openPrompt({
    title,
    value: kept !== null ? kept : g.body || "",
    multiline: true,
    allowEmpty: true,
    onSubmit: (text) => {
      keptBody = { pr, group, text };
      // An AI review's box always answers the body: emptied = no body.
      sendToGitHub(pr, { kind: "group", group, body: text, body_set: group !== "mine" }, "sending the review to #" + pr);
    },
  });
}

// verdictBody is Verdict…: an optional body, then the confirm's verdicts.
function verdictBody(pr) {
  const kept = keptBody && keptBody.pr === pr && keptBody.group === "verdict" ? keptBody.text : "";
  openPrompt({
    title: "Verdict on #" + pr + " — the review body (optional)",
    value: kept,
    multiline: true,
    allowEmpty: true,
    onSubmit: (text) => {
      keptBody = { pr, group: "verdict", text };
      sendToGitHub(pr, { kind: "verdict", body: text }, "posting a verdict on #" + pr);
    },
  });
}

// sendReviewPick is the PR menu's Send review…: one group goes straight to
// its body; several are picked first.
async function sendReviewPick(pr) {
  let d;
  try {
    d = await getJSON("/api/pr/send/groups?n=" + pr);
  } catch (e) {
    opLine("send review: " + (e.message || e), true);
    return;
  }
  const gs = d.groups || [];
  if (!gs.length) {
    opLine("#" + pr + " has no local notes to send", false);
    return;
  }
  if (gs.length === 1) return sendReviewBody(pr, gs[0].id);
  showCtxMenu(
    [
      { header: "send which review to #" + pr + "?" },
      ...gs.map((g) => ({
        label: g.id === "mine" ? `my draft review · ${groupCount(g)}` : `${g.agent}: ${g.summary} · ${groupCount(g)}`,
        act: () => sendReviewBody(pr, g.id),
      })),
    ],
    lastXY[0],
    lastXY[1]
  );
}

registerRows("pr", (pr) => {
  if (!pr || pr.state !== "open" || !pr.fetched) return [];
  return [
    { sep: true },
    { label: "Send review…", act: () => sendReviewPick(pr.number) },
    { label: "Verdict…", act: () => verdictBody(pr.number) },
  ];
});

// A send that changed GitHub clears the kept body; a refused, failed or
// aborted one keeps it (the user may edit and send again).
onSendDone((n, ev) => {
  if (ev.ok && ev.changed) keptBody = null;
});

registerHelp({
  key: "send to GitHub",
  html:
    "in a pull request's own diff each note says where it lives — <b>○</b> local, <b>◌</b> sending, " +
    "<b>○!</b> the last send failed (the error under it), <b>●</b> on GitHub — and its left border is its " +
    "group's colour (one per AI review, one for your own notes). <b>Right-click</b> a note: Send to GitHub, " +
    "Send my draft review… / Send this AI review…, and on a GitHub thread Reply &amp; send…, Resolve / Reopen on " +
    "GitHub, Send draft replies. The pull request's right-click menu has <b>Send review…</b> and <b>Verdict…</b>. " +
    "Every send shows what will be posted and waits for your answer; agents never send",
});

