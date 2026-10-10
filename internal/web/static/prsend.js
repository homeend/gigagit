// prsend.js — sending a pull request's notes to GitHub from the page (spec
// 2026-10-07 §4.2). Every send goes through POST /api/pr/send: the server
// plans it outside the repo gate, checks every id against the PR's notes,
// and the op parks on ONE confirm — drawn from that plan (sendplan.js) in
// the ordinary decision modal. Nothing is posted without that answer, and
// agents never send (user ruling 2026-10-08).
import { postJSON, state } from "./core.js";
import { openPrompt } from "./layers.js";
import { followOp, opBusy, opLine } from "./ops.js";
import { registerHelp, registerRows } from "./menus.js";
import { fetchNotes, refreshNoteCounts } from "./files.js";
import { loadPRCounts } from "./previews.js";
import { sendRows } from "./prsendrows.js";
import { keptAfterSend, keptText } from "./prkept.js";

// Hooks for a finished send (prs.js: the freshness word, the interrupted
// bar) — registered, so this module never imports prs.js (which imports it).
// Each hook gets (n, the done event, the send's request body).
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
    for (const fn of doneHooks) fn(n, ev, body);
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
    "reply-send": () => replyAndSend(n, pr),
    resolve: () =>
      sendToGitHub(pr, { kind: n.resolved ? "unresolve" : "resolve", ids: [rootId] }, (n.resolved ? "reopening" : "resolving") + " a thread on #" + pr),
    "send-drafts": () =>
      sendToGitHub(pr, { kind: "notes", ids: (n.replies || []).filter((r) => r.source !== "forge").map((r) => r.id) }, "sending replies to #" + pr),
  };
  const rows = sendRows(n, pr).map((row) => ({ label: row.label, act: acts[row.id] }));
  return rows.length ? [{ sep: true }, ...rows] : [];
});

// keptBody is a body a refused or failed send left: the next Verdict… (group
// "verdict") starts from it, so typing is never lost.
let keptBody = null; // {pr, group, text}

// verdictBody is Verdict…: an optional body, then the confirm's verdicts.
function verdictBody(pr) {
  const kept = keptText(keptBody, pr, "verdict") ?? "";
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

registerRows("pr", (pr) => {
  if (!pr || pr.state !== "open" || !pr.fetched) return [];
  return [{ sep: true }];
});
// Verdict… follows Send to GitHub… (prsendpanel.js, imported right after
// this module): its contributor is registered once that import has run.
queueMicrotask(() =>
  registerRows("pr", (pr) => {
    if (!pr || pr.state !== "open" || !pr.fetched) return [];
    return [{ label: "Verdict…", act: () => verdictBody(pr.number) }];
  })
);

// Only a send from the kept body's own box that changed GitHub clears it; a
// refused, failed or aborted one — or any other send — keeps it (C8).
onSendDone((n, ev, body) => {
  keptBody = keptAfterSend(keptBody, n, body, ev);
});

registerHelp({
  key: "send to GitHub",
  html:
    "in a pull request's own diff each note says where it lives — <b>○</b> local, <b>◌</b> sending, " +
    "<b>○!</b> the last send failed (the error under it), <b>●</b> on GitHub — and its left border is its " +
    "group's colour (one per AI review, one for your own notes). The pull request's right-click menu has " +
    "<b>Send to GitHub…</b> — one panel over every unsent note, remark and draft reply, nothing ticked on open — and " +
    "<b>Verdict…</b>. Right-click a note: Send as GitHub comment, and on a GitHub thread Reply &amp; send…, " +
    "Resolve / Reopen on GitHub, Send draft replies. Every send shows what will be posted and waits for your answer; " +
    "agents never send",
});

