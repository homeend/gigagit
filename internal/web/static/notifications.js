// notifications.js — part of gg's web client.
//
// The notification centre: the browser's half of the TUI's `!` dialog.
//
// The TUI notices repository problems it can fix and offers the fix behind one
// key; the browser noticed none of them. The finding carried here today is the
// narrowed fetch refspec — a clone whose refspec cannot map a branch, so a push
// succeeds while the ↓↑ markers never move and nobody is told why.
//
// Everything this file remembers lives SERVER-SIDE, in gg's machine-local
// prompt store. `gg web` binds a random loopback port, so every run is a new
// origin and localStorage starts empty — a dismissal kept in the browser would
// come back on the next run, which is exactly the bug the centre must not have.
import { $, esc, getJSON, postJSON } from "./core.js";
import { closeLayer, mountOverlay, pushLayer, topLayer } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { opLine, startOp } from "./ops.js";

// Notice ids are PERMANENT — the server remembers a dismissal by id in
// prompts.toml. This mirrors the constant in internal/web/notifications.go and
// must never be renamed on one side only.
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
/* Unread: the TUI's blinking segment — two phases of ~800ms each. */
#notice-chip.unread { animation: notice-blink 1.6s steps(1, end) infinite; }
@keyframes notice-blink { 50% { background: #f27a6a; color: var(--bg); } }
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

// unread is the server's word (one session for every tab): a notice arrived
// that the centre has not been opened on since.
let unread = false;

function centreOpen() {
  return !$("notify").classList.contains("hidden");
}

async function fetchNotifications() {
  try {
    const resp = await getJSON("/api/notifications");
    notices = resp.notices || [];
    unread = !!resp.unread;
  } catch {
    notices = []; // a centre that cannot load says nothing, it does not shout
    unread = false;
  }
  if (unread && centreOpen()) markRead(); // it arrived while the reader is looking
  chip.textContent = "! " + notices.length + (notices.length === 1 ? " notice" : " notices");
  chip.classList.toggle("hidden", notices.length === 0);
  chip.classList.toggle("unread", !!unread && notices.length > 0 && !centreOpen());
  if (!$("notify").classList.contains("hidden")) renderNotifications();
  return notices;
}


// markRead tells the server the centre was opened (the TUI's
// openNoticeCenter): the blink stops for every tab.
function markRead() {
  unread = false;
  chip.classList.remove("unread");
  postJSON("/api/notifications/read", {}).catch(() => {});
}

function openNotifications() {
  markRead();
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
        `</div>`;
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


$("notify-list").addEventListener("click", async (e) => {
  const btn = e.target.closest("button");
  if (!btn) return;
  const n = notices[Number(btn.dataset.n)];
  if (!n) return;
  // The header buttons are rendered from a FILTERED list, so an action is
  // always resolved against n.actions by its real index — never against the
  // filtered one.
  const a = n.actions[Number(btn.dataset.a)];
  if (!a) return;
  // Every action removes its notice for the session (the TUI's
  // applyNoticeAction) — a fix, Not now, Never — so a re-read cannot
  // resurrect it; a kept one (a suppression of something else, one branch's
  // fix) leaves it to the next read.
  if (!a.keep) {
    try {
      await postJSON("/api/notifications/session", { id: n.id });
    } catch (err) {
      opLine("error: " + (err.message || err), true);
      return;
    }
  }
  if (a.dismiss) await dismissNotice(a.dismiss); // persisted: Never, or the push question's silence
  if (a.op) {
    closeLayer("notify"); // the op's progress line is on the page behind this
    const body = { op: a.op };
    if (a.branch) body.branch = a.branch;
    if (a.paths) body.paths = a.paths;
    startOp(body, a.label);
    return;
  }
  if (a.open === "exttools") {
    closeLayer("notify");
    const { openExtToolsView } = await import("./exttools.js");
    openExtToolsView();
    return;
  }
  fetchNotifications();
});


// dismissNotice persists "never for this repo" through the server. It is the
// TUI's key (the git common dir), so dismissing the shared refspec advice here
// silences the TUI's identical notice too.
async function dismissNotice(id) {
  try {
    await postJSON("/api/notifications/dismiss", { id });
  } catch (e) {
    opLine("dismissed for this session (couldn't persist: " + (e.message || e) + ")", true);
  }
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


registerHelp({
  key: "!",
  html:
    "notifications — what gg found that it can fix or that needs you (a git lock left behind, a missing " +
    "commit-graph, an unmapped fetch refspec, a feature this repository cannot use, external-tool template " +
    "updates, an agent asking to show another worktree). The bell <b>blinks</b> while a new one is unread; " +
    "each offers its fix, <b>Not now</b> (until gg web restarts) or <b>Never for this repo</b> — shared with the TUI",
});

// The bell answers to `!`, like the TUI's. Registered here (not in keys.js) so
// the key and the surface stay in one file; layers own the keyboard first, so
// this only fires when nothing is open — the keys.js rule.
document.addEventListener("keydown", (e) => {
  // Modifiers disqualify it: on a US layout alt+shift+1 IS "!" , and that is
  // the remotes branch-filter key (branchfilter.js). This listener is separate
  // from keys.js's, so consuming the key there cannot stop it — the guard has
  // to live here.
  if (e.key !== "!" || e.altKey || e.ctrlKey || e.metaKey || topLayer()) return;
  if (e.target.closest && e.target.closest("input,textarea")) return;
  e.preventDefault();
  openNotifications();
});

fetchNotifications();

export { fetchNotifications, openNotifications };
