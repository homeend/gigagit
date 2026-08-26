// commitai.js — the commit box's two missing lanes: have a configured agent
// WRITE the message (the TUI's ctrl+g), and amend the last commit (the TUI's
// C).
//
// The buttons are appended to the existing commit box at boot rather than
// added to index.html, and the approval surface mounts itself, so this feature
// is one file plus its endpoints (task 0's rules).
//
// Two properties are carried over from the review lane deliberately:
//   - the command text is never sent from here. The client names a TOOL; the
//     server resolves the command from the config and tells us what it will
//     run. What the approval box shows is what the server said.
//   - nothing commits by itself. A generated message lands in the box for the
//     human to read, edit and send.
import { $, esc, getJSON, postJSON, state } from "./core.js";
import { closeLayer, hideCtxMenu, mountOverlay, openPrompt, pushLayer, showCtxMenu } from "./layers.js";
import { registerHelp } from "./menus.js";
import { followOp, opBusy, opLine, showLocalConfirm, startOp } from "./ops.js";

let run = null; // {opID} while a generate run is in flight

function injectStyle() {
  if ($("gg-commitai-style")) return;
  const st = document.createElement("style");
  st.id = "gg-commitai-style";
  // No global `.hidden` rule exists in this sheet (surfaces hide by id), so
  // the approval overlay brings its own — an element trusting a rule that is
  // not there is plainly visible, which has shipped here before.
  st.textContent = `
#gg-approve {
  position: fixed; inset: 0; background: rgba(0,0,0,.55);
  display: flex; align-items: center; justify-content: center; z-index: 12;
}
#gg-approve.hidden { display: none; }
#gg-approve .box {
  background: var(--bg-alt); border: 1px solid #a8843c; border-radius: 6px;
  padding: 18px 24px; width: 680px; max-width: 94vw; font-size: 13px;
}
#gg-approve h2 { margin: 0 0 8px; font-size: 15px; color: #d9a441; }
#gg-approve pre {
  background: var(--bg); border: 1px solid var(--border); border-radius: 4px;
  padding: 8px 10px; margin: 8px 0; white-space: pre-wrap; word-break: break-all;
  font: inherit; font-size: 12px; max-height: 40vh; overflow-y: auto;
}
#gg-approve .note { color: var(--dim); font-size: 11px; }
#gg-approve .btns { display: flex; gap: 8px; margin-top: 12px; }
#gg-approve button {
  background: var(--bg); color: var(--fg); border: 1px solid var(--border);
  border-radius: 4px; padding: 3px 14px; font: inherit; font-size: 12px; cursor: pointer;
}
#gg-approve button:hover { border-color: var(--accent); }
#gg-approve button.go { border-color: #a8843c; color: #d9a441; font-weight: 600; }
`;
  document.head.append(st);
}


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
  gen.addEventListener("click", () => (run ? cancelGenerate() : startGenerate()));
  const amend = document.createElement("button");
  amend.id = "amend-btn";
  amend.title = "rewrite the last commit's message (and include what is staged)";
  amend.textContent = "amend…";
  amend.addEventListener("click", startAmend);
  box.append(gen, amend);
}


function setGenerating(on) {
  const b = $("gen-btn");
  if (!b) return;
  b.textContent = on ? "cancel" : "generate";
  b.title = on
    ? "stop the agent — it holds gg's single operation lane while it runs"
    : "have a configured agent write the message from the staged diff";
}


// --- generate --------------------------------------------------------------
async function startGenerate() {
  if (run) return;
  if (opBusy()) {
    opLine("generate: an operation is already running", true);
    return;
  }
  let info;
  try {
    info = await getJSON("/api/commit-message/tools");
  } catch (e) {
    opLine("generate: " + (e.message || e), true);
    return;
  }
  // The TUI's two refusals, in its words — said here rather than after a
  // round trip that would fail anyway.
  if (!info.staged) {
    opLine("nothing staged to describe", true);
    return;
  }
  const tools = info.tools || [];
  if (!tools.length) {
    opLine(
      'generate: no commit-message tool configured — add a [[tools.command]] block with category = "commit_message" and mode = "capture"',
      true
    );
    return;
  }
  if (tools.length === 1) {
    gateGenerate(tools[0]);
    return;
  }
  // More than one configured: choose, exactly as the TUI's numbered chooser
  // does. Anchored under the button that opened it.
  const r = $("gen-btn").getBoundingClientRect();
  showCtxMenu(
    [
      { header: "commit-message tool" },
      ...tools.map((t) => ({ label: t.name, act: () => gateGenerate(t) })),
    ],
    r.left,
    r.bottom + 4
  );
}


// gateGenerate applies the TUI's two gates in its order: approve an unseen
// command first, then confirm before overwriting a message already typed.
function gateGenerate(tool) {
  hideCtxMenu();
  if (!tool.approved) {
    openApproval(tool);
    return;
  }
  confirmReplace(tool, false);
}


function confirmReplace(tool, approve) {
  if ($("commit-msg").value.trim() === "") {
    dispatchGenerate(tool, approve);
    return;
  }
  showLocalConfirm(
    "Replace the message you have written? Generating overwrites the commit box.",
    ["replace", "abort"],
    (o) => {
      if (o === "replace") dispatchGenerate(tool, approve);
    }
  );
}


async function dispatchGenerate(tool, approve) {
  let resp;
  try {
    resp = await postJSON("/api/commit-message/generate", { tool: tool.name, approve: !!approve });
  } catch (e) {
    // Most often a 403: the server does not consider this command approved,
    // whatever the list said. Show the approval step rather than the error.
    if (e.data && e.data.needs_approval) {
      // The refusal carries the resolved command, which may differ from the
      // one the list showed (an edited config): approve what will RUN.
      openApproval({ ...tool, command: e.data.command || tool.command });
      return;
    }
    opLine("generate: " + (e.message || e), true);
    return;
  }
  run = { opID: resp.op_id };
  setGenerating(true);
  followOp(resp.op_id, "generating a commit message with " + (resp.tool || tool.name), "commit_message", (ev) => {
    run = null;
    setGenerating(false);
    if (!ev.ok) {
      opLine(ev.cancelled ? "generate cancelled" : "generate failed: " + (ev.error || "unknown error"), true);
      return;
    }
    const body = (ev.body || "").trim();
    $("commit-msg").value = ev.subject + (body ? "\n\n" + body : "");
    $("commit-msg").focus();
    // Nothing committed — say so, because a filled box after a wait could
    // otherwise read as "it did the thing".
    opLine("message written into the box — read it, then commit");
  });
}


// cancelGenerate stops an agent that is taking too long. It holds gg's single
// operation lane while it runs, so this is not a nicety.
async function cancelGenerate() {
  if (!run) return;
  try {
    await postJSON("/api/op/" + run.opID + "/cancel", {});
  } catch (e) {
    opLine("cancel: " + (e.message || e), true);
  }
}


// --- the approval box ------------------------------------------------------
// A command runs only after the user has seen it in full. The approval is
// remembered server-side against the config text's hash — the same store the
// TUI uses, so approving here covers the TUI and editing the command re-asks
// in both.
function openApproval(tool) {
  injectStyle();
  const el = mountOverlay("gg-approve");
  el.innerHTML =
    `<div class="box">` +
    `<h2>Run this command?  (${esc(tool.name)})</h2>` +
    `<div class="note">gg would run this on your machine, with the staged diff written to a temporary file:</div>` +
    `<pre>${esc(tool.command)}</pre>` +
    `<div class="note">Approving remembers this command for this repository until you change it.</div>` +
    `<div class="btns"><button class="go" id="gg-approve-go">run it</button>` +
    `<button id="gg-approve-no">cancel</button></div>` +
    `</div>`;
  $("gg-approve-go").onclick = () => {
    closeApproval();
    confirmReplace(tool, true);
  };
  $("gg-approve-no").onclick = closeApproval;
  el.onclick = (e) => {
    if (e.target === el) closeApproval();
  };
  pushLayer("gg-approve", el, {
    onKey: (e) => {
      if (e.key === "Escape") {
        closeApproval();
        return true;
      }
      return false;
    },
  });
}


function closeApproval() {
  closeLayer("gg-approve");
  mountOverlay("gg-approve").innerHTML = "";
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
    "<b>generate</b> runs a configured <code>commit_message</code> agent over the staged diff and " +
    "fills the box — it never commits, and an unapproved command is shown in full first. " +
    "<b>amend…</b> rewrites the last commit's message (also in ☰); it confirms first, because a " +
    "pushed commit then needs a forced push",
});

mountButtons();

export { startAmend, startGenerate };
