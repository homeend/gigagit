// commitai.js — the commit box's two extra buttons: generate (the AI-task
// launch dialog for a commit-message agent, the TUI's ctrl+g — launch.js puts
// the message in the box when it arrives) and amend the last commit (the
// TUI's C). Nothing commits by itself: a generated message lands in the box
// for the human to read, edit and send.
import { $, getJSON, state } from "./core.js";
import { openPrompt } from "./layers.js";
import { registerHelp } from "./menus.js";
import { opBusy, opLine, showLocalConfirm, startOp } from "./ops.js";
import { openLaunch } from "./launch.js";

// --- the buttons -----------------------------------------------------------
// Appended to the commit box the app already has. The box itself is hidden
// while the tree is clean or a sequencer op is paused, which is why amend ALSO
// registers a ☰ row: amending is about the last commit, not the working tree,
// and the TUI's C works with nothing staged at all.
function mountButtons() {
  const box = $("commit-box");
  if (!box || $("gen-btn")) return;
  const gen = document.createElement("button");
  gen.id = "gen-btn";
  gen.title = "have a configured agent write the message from the staged diff";
  gen.textContent = "generate";
  // The launch dialog (launch.js): agent × mode, then the task scheduler —
  // the TUI's ctrl+g in the commit box. A running task is cancelled from the
  // AI tasks tab (ctrl+\), not from here: it holds no lane.
  gen.addEventListener("click", () => {
    if (!(state.wt && state.wt.counts && state.wt.counts.staged > 0)) return opLine("nothing staged to describe", true);
    openLaunch({ kind: "commit_message" });
  });
  const amend = document.createElement("button");
  amend.id = "amend-btn";
  amend.title = "rewrite the last commit's message (and include what is staged)";
  amend.textContent = "amend…";
  amend.addEventListener("click", startAmend);
  box.append(gen, amend);
}


// --- amend -----------------------------------------------------------------
// Rewriting the last commit is history editing, so it says so before it opens
// the editor rather than after: declining costs nothing, while declining after
// a paragraph has been typed would throw the paragraph away.
async function startAmend() {
  if (opBusy()) {
    opLine("amend: an operation is already running", true);
    return;
  }
  let head;
  try {
    head = await getJSON("/api/commit-message/head");
  } catch (e) {
    // 409 on a repo with no commit — the honest "nothing to amend", from the
    // only side that can know it.
    opLine("amend: " + (e.message || e), true);
    return;
  }
  showLocalConfirm(
    "Amend the last commit? It rewrites that commit — anything staged joins it, and if it has " +
      "already been pushed the next push must be forced.",
    ["amend", "abort"],
    (o) => {
      if (o !== "amend") return;
      openPrompt({
        title: "Amend the last commit — message:",
        value: head.message || "",
        multiline: true,
        // The prompt refuses an empty submit on its own, so a message is
        // always present here; the server checks again anyway.
        onSubmit: (msg) => startOp({ op: "commit-amend", message: msg }, "amending the last commit"),
      });
    }
  );
}


registerHelp({
  key: "commit box",
  html:
    "<b>generate</b> opens the AI-task launch dialog for a <code>commit_message</code> agent (←/→ agent, ↑/↓ " +
    "interactive in the background / foreground or headless) — its message fills the box, never commits, and an " +
    "unapproved command is shown in full first; a running task is in ctrl+\\ → AI tasks. " +
    "<b>amend…</b> rewrites the last commit's message (also in ☰); it confirms first, because a " +
    "pushed commit then needs a forced push",
});

mountButtons();

export { startAmend };
