// rebase.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, esc, getJSON } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";
import { opBusy, opLine, startOp } from "./ops.js";

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

export { REBASE_ACTIONS, closeRebaseEditor, oldestIndex, openRebaseEditor, reb, renderRebase };
