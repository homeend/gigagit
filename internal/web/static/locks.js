// locks.js — part of gg's web client. Three things the browser could not do
// and the TUI could: rename a worktree, move one, cut one that KEEPS the
// commit's change — and get out from under a stranded git lockfile.
//
// Everything here registers itself (menus.js / layers.js, task 0) rather than
// editing sidebar.js, commits.js or index.html, so this feature is one file.
import { $, defaultWorktreePath, esc, getJSON, postJSON, state } from "./core.js";
import { mountOverlay, openPrompt } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { doReroot, followOp, opBusy, opLine, refreshAfterOp, startOp } from "./ops.js";

// --- paths -----------------------------------------------------------------
// Worktree paths are absolute and come from git, which reports "/" even on
// Windows — while anything derived from a Windows path may carry "\". Never
// compare or split one without deciding which separator it uses: a raw
// string compare across the two notations is a Windows-only bug that has
// shipped in this repo before. Same idiom as core.js's defaultWorktreePath.
function pathSep(p) {
  return p.includes("\\") && !p.includes("/") ? "\\" : "/";
}


function parentOf(p) {
  const cut = p.lastIndexOf(pathSep(p));
  return cut > 0 ? p.slice(0, cut) : p;
}


function baseOf(p) {
  const cut = p.lastIndexOf(pathSep(p));
  return cut >= 0 ? p.slice(cut + 1) : p;
}


// The main worktree cannot be moved (the engine refuses it), and git always
// lists it first — there is no flag on the row to test instead.
function isMainWorktree(w) {
  const main = state.worktrees[0] && state.worktrees[0].path;
  return !!main && w.path === main;
}


// --- rename / move ---------------------------------------------------------
// One operation, two rows. The TUI keeps them apart for the same reason: the
// intentions differ (a new NAME beside the old one, versus a new PLACE), and
// what differs between them is only which destination the prompt starts from.
async function startMove(w, dest, label) {
  if (opBusy()) return;
  const to = (dest || "").trim();
  if (!to || to === w.path) {
    opLine("nothing to move: same path", true);
    return;
  }
  // Whether the SERVED worktree is the one moving has to be decided BEFORE
  // the op runs: afterwards the path the server is standing in no longer
  // exists, and every later read of it would be against a stale value.
  const served = !!state.worktree && w.path === state.worktree;
  let resp;
  try {
    resp = await postJSON("/api/op", { op: "move-worktree", path: w.path, dest: to });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  // followOp's onDone REPLACES the generic done handling, so this branch owns
  // the whole outcome — including the refresh, without which the sidebar row
  // keeps naming a directory that is gone.
  followOp(resp.op_id, label, "move-worktree", (ev) => {
    if (!ev.ok) {
      opLine("error: " + (ev.error || "operation failed"), true);
      refreshAfterOp();
      return;
    }
    opLine(ev.summary || "done");
    if (!ev.changed) return; // cancelled at the locked prompt: nothing moved
    // The server is rooted at a path that no longer exists. Re-rooting at the
    // destination is the honest recovery — doReroot reloads the page, and it
    // carries the cross-environment repair offer, so a worktree linked for
    // the other environment fails with the message that already explains it
    // rather than a new one invented here.
    if (served) doReroot(to);
    else refreshAfterOp();
  });
}


function openRenamePrompt(w) {
  const from = baseOf(w.path);
  openPrompt({
    title: "Rename worktree " + from + " to:",
    value: from, // the basename only, like the TUI's `e`
    onSubmit: (name) => {
      const n = (name || "").trim();
      if (!n) return;
      // Resolved next to the CURRENT parent: a rename never changes where the
      // worktree lives, only what it is called.
      startMove(w, parentOf(w.path) + pathSep(w.path) + n, "renaming " + from);
    },
  });
}


function openMovePrompt(w) {
  openPrompt({
    title: "Move worktree " + baseOf(w.path) + " to:",
    value: w.path, // the full path — this row is about WHERE it goes
    onSubmit: (dest) => startMove(w, dest, "moving " + baseOf(w.path)),
  });
}


registerRows("worktree", (w) => {
  // A bare worktree has no tree to move, and the main one is engine-refused.
  if (!w || w.bare || isMainWorktree(w)) return [];
  return [
    { sep: true }, // registered rows land after the red remove row; fence them
    { label: "rename worktree…", act: () => openRenamePrompt(w) },
    { label: "move worktree…", act: () => openMovePrompt(w) },
  ];
});


// --- create a worktree that KEEPS the commit's change ----------------------
// The branch lands on the commit's PARENT with the commit's diff staged or
// unstaged in the new worktree — "I want to redo this commit elsewhere".
// Only offered on a commit that HAS exactly one parent: on a root or a merge
// the engine refuses, and a row that opens two prompts to reach a refusal is
// worse than no row.
function openKeepPrompt(c, mode) {
  const short = c.short || String(c.hash || "").slice(0, 8);
  openPrompt({
    title: "New branch for the worktree at " + short + " (its change kept " + mode + "):",
    placeholder: "branch name",
    onSubmit: (name) => {
      const n = (name || "").trim();
      if (!n) return;
      openPrompt({
        title: "New worktree for " + n + ", at path:",
        value: defaultWorktreePath(n),
        onSubmit: (path) => {
          const p = (path || "").trim();
          if (!p) return;
          startOp(
            { op: "create-worktree-keep", sha: c.hash, name: n, path: p, mode },
            "creating worktree " + p
          );
        },
      });
    },
  });
}


registerRows("commit", (c) => {
  if (!c || c.parents !== 1) return [];
  return [
    { sep: true },
    { label: "worktree here, keeping the change staged…", act: () => openKeepPrompt(c, "staged") },
    { label: "worktree here, keeping the change unstaged…", act: () => openKeepPrompt(c, "unstaged") },
  ];
});


// --- stranded git locks ----------------------------------------------------
// A lockfile that outlives its git process makes every later git fail with
// "Another git process seems to be running in this repository". The browser
// used to show that error with no way out.
//
// Presence is NOT proof of staleness — a git running right now legitimately
// holds one, and gg cannot see processes it did not start. So the bar reports
// each lock's AGE and leaves the judgement to the human, exactly as the TUI's
// notice does.
let locks = [];

// The sheet has no global `.hidden` rule — every surface hides itself by id,
// and an element relying on a rule that does not exist is plainly visible
// (that has shipped here). style.css belongs to nobody in this wave, so the
// bar brings its own rules, its own `.hidden` among them.
function injectStyle() {
  if ($("gg-locks-style")) return;
  const st = document.createElement("style");
  st.id = "gg-locks-style";
  st.textContent = `
#gg-locks-bar {
  display: flex; gap: 10px; align-items: center; flex-wrap: wrap;
  padding: 4px 12px;
  background: #45191961; border-bottom: 1px solid #7c3a3a;
  color: #f0b8b8; font-size: 13px;
}
#gg-locks-bar.hidden { display: none; }
#gg-locks-bar .lk { color: #f7d9d9; }
#gg-locks-bar button {
  background: var(--bg); color: var(--fg);
  border: 1px solid var(--border); border-radius: 3px;
  padding: 1px 10px; font: inherit; font-size: 12px; cursor: pointer;
}
#gg-locks-bar button:hover:not(:disabled) { border-color: var(--accent); }
`;
  document.head.append(st);
}


// mountOverlay appends to <body>, which would put the bar below the panes.
// It belongs with the other notice bars, directly above them — moving the
// element is this feature's own business and edits nobody's markup.
function mountBar() {
  injectStyle();
  const bar = mountOverlay("gg-locks-bar");
  const panes = $("panes");
  if (panes && bar.nextElementSibling !== panes) document.body.insertBefore(bar, panes);
  return bar;
}


function ageText(sec) {
  const s = Math.max(0, Math.floor(sec));
  if (s < 60) return s + "s old";
  if (s < 3600) return Math.floor(s / 60) + "m old";
  if (s < 86400) return Math.floor(s / 3600) + "h old";
  return Math.floor(s / 86400) + "d old";
}


function renderLocks() {
  const bar = mountBar();
  if (!locks.length) {
    bar.classList.add("hidden");
    bar.innerHTML = "";
    return;
  }
  const list = locks.map((l) => esc(l.name) + " (" + ageText(l.age_seconds) + ")").join(", ");
  bar.innerHTML =
    `<span>⚠ git lock${locks.length > 1 ? "s" : ""} present — git refuses to run while ` +
    `${locks.length > 1 ? "they are" : "it is"} there:</span>` +
    `<span class="lk">${list}</span>` +
    `<button id="gg-locks-clear" title="delete these lockfiles">clear</button>`;
  bar.classList.remove("hidden");
  $("gg-locks-clear").onclick = clearLocks;
}


async function fetchLocks() {
  try {
    const out = await getJSON("/api/locks");
    locks = out.locks || [];
  } catch {
    locks = []; // a failed probe must not leave a stale warning on screen
  }
  renderLocks();
}


async function clearLocks() {
  if (opBusy()) return;
  const paths = locks.map((l) => l.path);
  if (!paths.length) return;
  let resp;
  try {
    resp = await postJSON("/api/op", { op: "remove-locks", paths });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  followOp(resp.op_id, "clearing git locks", "remove-locks", (ev) => {
    if (ev.ok) opLine(ev.summary || "done");
    else opLine("error: " + (ev.error || "operation failed"), true);
    // Re-read rather than assume: a lock may have been recreated between the
    // scan and now, and the bar hides itself only on a genuinely empty list.
    fetchLocks();
    if (ev.changed) refreshAfterOp();
  });
}


// No timer of its own. The client already refreshes when the window regains
// focus (ops.js does the same for the working tree), which is exactly when a
// lock left by a git run outside the browser becomes worth knowing about —
// plus once at boot, and after any clear.
let lastLockCheck = 0;

window.addEventListener("focus", () => {
  if (Date.now() - lastLockCheck < 2000) return;
  lastLockCheck = Date.now();
  fetchLocks();
});

fetchLocks();

registerHelp({
  key: "worktree menu",
  html: "<b>rename</b> or <b>move</b> a worktree; a locked one asks before unlocking it",
});
registerHelp({
  key: "commit menu",
  html: "create a worktree here <b>keeping</b> the commit's change staged or unstaged",
});
registerHelp({
  key: "git locks",
  html: "a stranded <code>index.lock</code> raises a bar at the top — <b>clear</b> removes it",
});

export { ageText, baseOf, fetchLocks, parentOf, pathSep };
