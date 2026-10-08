// sendplan.js — the GitHub send confirm, drawn from the plan the server built
// (prsend.go's sendPlanWire). Import-free: sendplanjs_test.go runs it under
// node. The confirm's BUTTONS stay the decision's options (the page only ever
// posts one back); this file words them and draws what will be posted.

export const SEND_CONFIRM_CAP = 30;

const LABELS = {
  comment: "Comment",
  approve: "Approve",
  "request-changes": "Request changes",
  send: "Send",
  "submit-with-pending": "Submit with my pending review",
  discard: "Discard",
  abort: "Cancel",
};

// sendOptionLabel is an option's button text (an unknown option as is).
export function sendOptionLabel(o) {
  return LABELS[o] || o;
}

// skipWords are the domain's skip codes (domain.Skip*) as the confirm says them.
export function skipWords(reason) {
  return "skipped: " + reason;
}

function plural(n, one, many) {
  return n === 1 ? "1 " + one : n + " " + many;
}

// sendConfirmHTML is the modal's body: where it goes, the review body, every
// item (capped) and every item that will NOT be sent, with why.
export function sendConfirmHTML(plan, esc) {
  const p = plan || {};
  let h = `<div class="sc-target">Send to <b>${esc(p.target || "")}</b></div>`;
  if (p.mode === "finish") h += `<div class="sc-note">finish the interrupted send: submit the review it left pending on GitHub</div>`;
  if (p.mode === "discard") h += `<div class="sc-note">discard the interrupted send: delete the review it left pending — none of it is posted</div>`;
  // A review joining the user's own pending one says so (finish/discard ARE that review).
  if (p.has_pending && p.mode === "review") h += `<div class="sc-note">you have a review pending on GitHub: these comments join it</div>`;
  if (p.body) h += `<div class="sc-label">review body</div><pre class="sc-body">${esc(p.body)}</pre>`;
  const items = p.items || [];
  if (items.length && p.mode !== "finish" && p.mode !== "discard") {
    h += `<div class="sc-label">${plural(items.length, "item", "items")}</div><ul class="sc-items">`;
    for (const it of items.slice(0, SEND_CONFIRM_CAP)) {
      const extra = (it.replies ? ` (+ ${plural(it.replies, "reply", "replies")})` : "") + (it.resolve ? " (then resolved)" : "");
      h += `<li>+ ${esc(it.label)}${esc(extra)}</li>`;
    }
    if (items.length > SEND_CONFIRM_CAP) h += `<li class="sc-more">+ ${items.length - SEND_CONFIRM_CAP} more</li>`;
    h += `</ul>`;
  } else if (items.length) {
    h += `<div class="sc-label">${plural(items.length, "item waits", "items wait")} in that pending review</div>`;
  }
  const sk = p.skipped || [];
  if (sk.length) {
    h += `<div class="sc-label">not sent</div><ul class="sc-skipped">`;
    for (const s of sk.slice(0, SEND_CONFIRM_CAP)) h += `<li>− ${esc(s.label)} <span class="sc-why">(${esc(skipWords(s.reason))})</span></li>`;
    if (sk.length > SEND_CONFIRM_CAP) h += `<li class="sc-more">− ${sk.length - SEND_CONFIRM_CAP} more</li>`;
    h += `</ul>`;
  }
  return h;
}

// sendDecision is a forge.send decision event dressed for the modal: the
// plan's HTML and a label per option (the options themselves unchanged).
export function sendDecision(ev, plan, esc) {
  const labels = {};
  for (const o of ev.options || []) labels[o] = sendOptionLabel(o);
  return { ...ev, html: sendConfirmHTML(plan, esc), labels };
}
