// notifications.js — part of gg's web client.
//
// The notification centre, and the two push repairs that feed it.
//
// The TUI notices things it can fix and offers the fix behind `!`; the browser
// noticed none of them. Two findings meet here:
//
//   - a clone whose fetch refspec cannot map a branch, so a push succeeds while
//     the ↓↑ markers never move and nobody is told why;
//   - tags on the branch tip that the remote does not have, offered before a
//     push the way the TUI's P does.
//
// Everything this file remembers lives SERVER-SIDE, in gg's machine-local
// prompt store. `gg web` binds a random loopback port, so every run is a new
// origin and localStorage starts empty — a dismissal kept in the browser would
// come back on the next run, which is exactly the bug the centre must not have.
import { $, esc, getJSON, postJSON, state } from "./core.js";
import { closeLayer, mountOverlay, pushLayer, topLayer } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { followOp, opBusy, opLine, refreshAfterOp, showLocalConfirm, startOp } from "./ops.js";

// Notice ids are PERMANENT — the server remembers a dismissal by id in
// prompts.toml. These two mirror the constants in internal/web/notifications.go
// and must never be renamed on one side only.
const NOTICE_REFSPEC = "narrow_fetch_refspec";

// This sheet has NO global .hidden rule — every surface hides itself by id (a
// new element with class="hidden" and no rule behind it is plainly visible;
// that bug has shipped here before). A feature that mounts its own markup
// therefore brings its own styles, which also keeps style.css out of parallel
// merges.
const css = document.createElement("style");
css.textContent = `
#notice-chip {
  margin-left: 10px; background: var(--sel); color: #f27a6a;
  border: 1px solid #f27a6a; border-radius: 3px; padding: 1px 10px;
  font: inherit; cursor: pointer;
}
#notice-chip:hover { background: #f27a6a; color: var(--bg); }
#notice-chip.hidden { display: none; }
#notify { position: fixed; inset: 0; background: rgba(0,0,0,.55); display: flex; align-items: center; justify-content: center; z-index: 11; cursor: pointer; }
#notify.hidden { display: none; }
#notify-box { background: var(--bg-alt); border: 1px solid var(--accent); border-radius: 6px; padding: 18px 24px; width: 90vw; max-width: 720px; max-height: 84vh; overflow-y: auto; cursor: default; }
#notify-box h2 { margin: 0 0 12px; font-size: 15px; }
#notify-box .nempty { color: var(--dim); }
#notify-box .nnotice { border-top: 1px solid var(--border); padding: 12px 0 4px; }
#notify-box .nnotice:first-of-type { border-top: none; }
#notify-box .nhead { display: flex; align-items: baseline; gap: 12px; }
#notify-box .ntitle { flex: 1 1 auto; }
#notify-box .ndetail { color: var(--dim); font-size: 12px; line-height: 1.6; margin: 6px 0 8px; }
#notify-box .nitem { display: flex; align-items: baseline; gap: 12px; padding: 3px 0 3px 14px; }
#notify-box .nname { flex: 1 1 auto; color: var(--accent); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
#notify-box button { background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 3px; padding: 1px 10px; font: inherit; cursor: pointer; }
#notify-box button:hover { border-color: var(--accent); }
`;
document.head.append(css);

// The bell: a chip in the top bar beside the other two, red because every
// finding it carries is something quietly going wrong. Built here rather than
// in index.html so this feature owns all of its own markup.
const chip = document.createElement("button");
chip.id = "notice-chip";
chip.className = "hidden";
chip.title = "repository notices — click to see what gg can fix";
$("top").insertBefore(chip, $("pull-btn"));
chip.addEventListener("click", openNotifications);

// notices is the last answer from the server: the findings worth showing, each
// already carrying the actions that still apply to it.
let notices = [];

async function fetchNotifications() {
  try {
    const resp = await getJSON("/api/notifications");
    notices = resp.notices || [];
  } catch {
    notices = []; // a centre that cannot load says nothing, it does not shout
  }
  chip.textContent = "! " + notices.length + (notices.length === 1 ? " notice" : " notices");
  chip.classList.toggle("hidden", notices.length === 0);
  if (!$("notify").classList.contains("hidden")) renderNotifications();
  return notices;
}


function openNotifications() {
  renderNotifications();
  pushLayer("notify", $("notify"), {
    onKey: (e) => {
      if (e.key === "Escape" || e.key === "!") closeLayer("notify");
      e.preventDefault();
      return true; // the centre owns the keyboard until closed
    },
  });
  fetchNotifications(); // opening is also a refresh — the cheapest honest one
}


// The overlay mounts itself: no markup in index.html, so several features can
// add surfaces without ever meeting in that file.
const box = mountOverlay("notify");
box.innerHTML = `<div id="notify-box"><h2>notifications</h2><div id="notify-list"></div></div>`;
box.addEventListener("click", (e) => {
  if (e.target === box) closeLayer("notify"); // click-off, like #help
});


function renderNotifications() {
  const list = $("notify-list");
  if (!notices.length) {
    list.innerHTML = `<div class="nempty">nothing to report — gg found no repository problems it can fix.</div>`;
    return;
  }
  list.innerHTML = notices
    .map((n, ni) => {
      const all = n.actions || [];
      const perItem = all.filter((a) => a.branch);
      // data-a is the index into n.actions, NOT into the filtered list the row
      // is rendered from — the click handler resolves against n.actions, and a
      // filtered index there would fire the wrong action.
      const head =
        `<div class="nhead"><span class="ntitle">${esc(n.title)}</span>` +
        all
          .map((a, ai) => (a.branch ? "" : `<button data-n="${ni}" data-a="${ai}">${esc(a.label)}</button>`))
          .join("") +
        `<button data-n="${ni}" data-dismiss="1">dismiss</button></div>`;
      const detail = (n.detail || []).map((d) => `<div>${esc(d)}</div>`).join("");
      const items = (n.items || [])
        .map((it) => {
          const a = perItem.find((x) => x.branch === it);
          const idx = all.indexOf(a);
          const btn = a ? `<button data-n="${ni}" data-a="${idx}">${esc(a.label)}</button>` : "";
          return `<div class="nitem"><span class="nname">${esc(it)}</span>${btn}</div>`;
        })
        .join("");
      return `<div class="nnotice">${head}<div class="ndetail">${detail}</div>${items}</div>`;
    })
    .join("");
}


$("notify-list").addEventListener("click", (e) => {
  const btn = e.target.closest("button");
  if (!btn) return;
  const n = notices[Number(btn.dataset.n)];
  if (!n) return;
  if (btn.dataset.dismiss) {
    dismissNotice(n.id);
    return;
  }
  // The header buttons are rendered from a FILTERED list, so an action is
  // always resolved against n.actions by its real index — never against the
  // filtered one.
  const a = n.actions[Number(btn.dataset.a)];
  if (!a) return;
  if (a.dismiss) {
    dismissNotice(a.dismiss); // a suppression, not an operation: the centre stays open
    return;
  }
  closeLayer("notify"); // the op's progress line is on the page behind this
  const body = { op: a.op };
  if (a.branch) body.branch = a.branch;
  startOp(body, a.label);
});


// dismissNotice persists "never for this repo" through the server. It is the
// TUI's key (the git common dir), so dismissing the shared refspec advice here
// silences the TUI's identical notice too.
async function dismissNotice(id) {
  try {
    await postJSON("/api/notifications/dismiss", { id });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  fetchNotifications();
}


// --- refresh ---------------------------------------------------------------
// The centre must not poll. ops.js re-enables the pull/push buttons in exactly
// one place — the terminal `done` event — so that flip from disabled to enabled
// IS "an operation finished", and observing it costs nothing and fires never
// otherwise. (ops.js is shared by every parallel branch and must not be edited,
// which is why the signal is read off the DOM rather than called directly.)
new MutationObserver((records) => {
  for (const r of records) {
    if (r.attributeName === "disabled" && !$("push-btn").disabled) {
      fetchNotifications();
      return;
    }
  }
}).observe($("push-btn"), { attributes: true, attributeFilter: ["disabled"] });


// --- push, with the tip-tag question ---------------------------------------

// startPushChecked is the push entry point for every surface that used to call
// doPush: ask the server whether the branch tip carries tags the remote lacks,
// offer them, then push. The check is a NETWORK call, so the server bounds it
// to five seconds and answers checked=false when the budget runs out — a push
// must never hang behind an unreachable remote.
async function startPushChecked(branch) {
  if (opBusy()) return;
  let chk = null;
  try {
    chk = await getJSON("/api/push-check" + (branch ? "?branch=" + encodeURIComponent(branch) : ""));
  } catch {
    chk = null; // a check that failed is a check that offers nothing
  }
  // The push itself may go out with no branch (the server resolves the current
  // one), but the prompt and the tag chain both need a NAME, so the resolved
  // one is read off the check.
  const target = (chk && chk.branch) || branch || (state.repo && state.repo.branch) || "";
  const label = "pushing " + (target || "current branch");
  if (!chk || !chk.checked || !chk.unpushed.length) {
    startPushRun(branch, label, "");
    return;
  }
  // The world may have moved during those seconds. Both guards are the TUI's,
  // and both report rather than pushing under something the user just opened.
  if (state.op) {
    opLine("push cancelled (an operation is running) — press P again", true);
    return;
  }
  if (topLayer()) {
    opLine("push cancelled (another dialog opened) — press P again", true);
    return;
  }
  const tags = chk.unpushed;
  const noun = tags.length === 1 ? "tag " : "tags ";
  showLocalConfirm(
    "Push " + target + ": branch tip has " + noun + tags.join(", ") + " not on the remote. Push too?",
    ["push branch + tags", "push branch only", "cancel"],
    (o) => {
      if (o === "push branch + tags") startPushRun(branch, label, target);
      else if (o === "push branch only") startPushRun(branch, label, "");
    }
  );
}


// startPushRun runs the branch push itself. It carries its own done handler
// because the tag push has to be chained onto a SUCCESSFUL push and nothing
// else may chain onto a failed one. onDone REPLACES the generic done handling
// entirely, so everything that handler did is repeated here.
async function startPushRun(branch, label, tagBranch) {
  if (opBusy()) return;
  const body = { op: "push" };
  if (branch) body.branch = branch;
  let resp;
  try {
    resp = await postJSON("/api/op", body);
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  followOp(resp.op_id, label, "push", (ev) => {
    if (!ev.ok) {
      opLine("error: " + (ev.error || "operation failed"), true);
      // A failed push can still have moved something (a rejection-recovery
      // fork that force-pushed and then failed later), so the panels are
      // refreshed either way — the generic handler's reason for doing the
      // same on its non-changed path.
      refreshAfterOp();
      return; // no tags follow a push that did not land
    }
    opLine(ev.summary || "done");
    if (tagBranch) {
      startTipTagsRun(tagBranch);
      return;
    }
    // The unmapped-branch case is already a fork inside engine.Push, parked in
    // the shared modal — nothing to ask here. What this feature adds around it
    // is the way to turn that fork off and the batch repair (notifications.go);
    // the refresh below is what puts a new finding on the bell.
    refreshAfterOp();
  });
}


// startTipTagsRun chains the tag push after a successful branch push — the
// TUI's pendingPushTags step. It owns its done handling too, so the refresh
// runs once, after the LAST op of the sequence.
async function startTipTagsRun(branch) {
  let resp;
  try {
    resp = await postJSON("/api/op", { op: "push-tip-tags", branch });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    refreshAfterOp();
    return;
  }
  followOp(resp.op_id, "pushing tip tags", "push-tip-tags", (ev) => {
    if (ev.ok) opLine(ev.summary || "done");
    else opLine("error: " + (ev.error || "pushing tags failed"), true);
    refreshAfterOp();
  });
}


// A branch the centre reports as unmapped can be repaired from its own menu —
// the finding is about that branch, and the row is where the user is already
// looking. Rows register themselves; sidebar.js is never edited.
registerRows("branch", (b) => {
  if (!b || !b.name) return [];
  const n = notices.find((x) => x.id === NOTICE_REFSPEC);
  if (!n || !(n.items || []).includes(b.name)) return [];
  return [
    { sep: true },
    {
      label: "add fetch mapping + fetch",
      act: () => startOp({ op: "add-fetch-mappings", branch: b.name }, "mapping " + b.name),
    },
  ];
});


// The push button is bound inside ops.js, which parallel work must not edit, so
// the click is taken one phase earlier: a capture listener on the document runs
// before any listener on the button itself. The key and the palette entries
// call startPushChecked directly instead — a plain call beats an interception
// wherever the call site is reachable.
document.addEventListener(
  "click",
  (e) => {
    if (!e.target.closest || !e.target.closest("#push-btn")) return;
    e.stopPropagation();
    startPushChecked("");
  },
  true
);


registerHelp({ key: "!", html: "notifications — repository problems gg can fix (an unmapped fetch refspec today)" });

// P already has a help row. Appending a second one for the same key would read
// as two different keys, so the existing row is extended in place instead —
// with a registerHelp fallback if that row is ever renamed or removed.
(() => {
  const row = [...document.querySelectorAll("#help-box .hrow")]
    .find((r) => (r.querySelector(".hkey") || {}).textContent === "P");
  const note = " — tags on the branch tip that the remote lacks are offered first";
  const desc = row && row.querySelector("span:not(.hkey)");
  if (desc) desc.textContent += note;
  else registerHelp({ key: "P", html: "push" + note });
})();

// The bell answers to `!`, like the TUI's. Registered here (not in keys.js) so
// the key and the surface stay in one file; layers own the keyboard first, so
// this only fires when nothing is open — the keys.js rule.
document.addEventListener("keydown", (e) => {
  if (e.key !== "!" || topLayer()) return;
  if (e.target.closest && e.target.closest("input,textarea")) return;
  e.preventDefault();
  openNotifications();
});

fetchNotifications();

export { fetchNotifications, openNotifications, startPushChecked };
