// agentsetup.js — `gg init` in the browser: which AI agents this machine has,
// whether each one already carries gg's "using-gg" skill, and one button to
// write (or refresh) it.
//
// Self-registering (menus.js / layers.js, task 0): no markup in index.html, no
// rule in style.css, no row added to palette.js.
import { $, esc, getJSON, postJSON } from "./core.js";
import { closeLayer, mountOverlay, pushLayer } from "./layers.js";
import { registerHelp } from "./menus.js";
import { opLine } from "./ops.js";

let ag = null; // {version, project, agents, busy} while the overlay is up

// No global `.hidden` rule exists in this sheet — every surface hides by id,
// and an element trusting a rule that is not there is plainly visible (a bug
// that shipped here). Same recipe as the settings overlay.
function injectStyle() {
  if ($("gg-agents-style")) return;
  const st = document.createElement("style");
  st.id = "gg-agents-style";
  st.textContent = `
#gg-agents {
  position: fixed; inset: 0; background: rgba(0,0,0,.55);
  display: flex; align-items: center; justify-content: center; z-index: 11;
}
#gg-agents.hidden { display: none; }
#gg-agents .box {
  background: var(--bg-alt); border: 1px solid var(--accent); border-radius: 6px;
  padding: 18px 24px; width: 720px; max-width: 94vw; max-height: 86vh;
  overflow-y: auto; font-size: 13px;
}
#gg-agents h2 { margin: 0 0 4px; font-size: 15px; }
#gg-agents .arow {
  display: flex; gap: 10px; align-items: center; padding: 5px 4px;
  border-top: 1px solid var(--border);
}
#gg-agents .alabel { flex: none; width: 210px; color: var(--accent); }
#gg-agents .atarget { flex: 1 1 auto; color: var(--dim); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; direction: rtl; text-align: left; }
#gg-agents .astatus { flex: none; min-width: 76px; text-align: right; font-size: 11px; color: var(--dim); }
#gg-agents .astatus.new { color: #d9a441; }
#gg-agents .astatus.ok { color: var(--accent); }
#gg-agents button {
  background: var(--bg); color: var(--fg); border: 1px solid var(--border);
  border-radius: 4px; padding: 2px 12px; font: inherit; font-size: 12px; cursor: pointer;
}
#gg-agents button:hover:not(:disabled) { border-color: var(--accent); }
#gg-agents button:disabled { opacity: .45; cursor: default; }
#gg-agents .foot { margin-top: 12px; color: var(--dim); font-size: 11px; }
#gg-agents .none { color: var(--dim); margin: 10px 0; }
`;
  document.head.append(st);
}


function statusClass(s) {
  if (s === "up to date") return "ok";
  if (s === "new") return "new";
  return ""; // outdated
}


// actionLabel says what the button will DO, not what the row is: "install"
// writes the skill for the first time, "refresh" replaces an older copy, and
// an up-to-date target can still be rewritten (the file may have been edited).
function actionLabel(s) {
  if (s === "new") return "install";
  if (s === "outdated") return "refresh";
  return "reinstall";
}


function renderAgents() {
  const el = mountOverlay("gg-agents");
  if (!ag) return;
  const rows = (ag.agents || [])
    .map(
      (a, i) =>
        `<div class="arow">` +
        `<span class="alabel">${esc(a.label)}${a.custom ? " (custom)" : ""}</span>` +
        `<span class="atarget" title="${esc(a.target)}">${esc(a.target)}</span>` +
        `<span class="astatus ${statusClass(a.status)}">${esc(a.status)}</span>` +
        `<button data-i="${i}"${ag.busy ? " disabled" : ""}>${esc(actionLabel(a.status))}</button>` +
        `</div>`
    )
    .join("");
  el.innerHTML =
    `<div class="box">` +
    `<h2>agent setup</h2>` +
    `<div class="foot" style="margin:0 0 10px">gg ships a skill that teaches an AI agent this CLI. ` +
    `Only agents actually present on this machine are listed — installing writes ` +
    `one file at the path shown (skill version ${esc(String(ag.version))}).</div>` +
    (rows || `<div class="none">no agent detected for this project or your home directory</div>`) +
    `<div class="foot">the same thing <code>gg init</code> does · esc closes</div>` +
    `</div>`;
  el.querySelectorAll("button[data-i]").forEach((b) => {
    b.onclick = () => installAgent(ag.agents[Number(b.dataset.i)]);
  });
}


async function installAgent(a) {
  if (!a || !ag || ag.busy) return;
  ag.busy = true;
  renderAgents();
  let out;
  try {
    out = await postJSON("/api/agents/install", { id: a.id });
  } catch (e) {
    ag.busy = false;
    renderAgents();
    opLine("agent setup: " + (e.message || e), true);
    return;
  }
  ag.busy = false;
  // The row's new status is the file's, re-read by the server — and saying
  // where it landed is the whole point of a surface that writes outside the
  // repository.
  opLine((out.was === "new" ? "installed" : "refreshed") + " " + a.label + " → " + out.target);
  await loadAgents();
}


async function loadAgents() {
  let out;
  try {
    out = await getJSON("/api/agents");
  } catch (e) {
    opLine("agent setup: " + (e.message || e), true);
    return;
  }
  if (!ag) return; // closed while the read was in flight
  ag.version = out.version;
  ag.project = out.project;
  ag.agents = out.agents || [];
  renderAgents();
}


function openAgentSetup() {
  injectStyle();
  ag = { version: "", project: "", agents: [], busy: false };
  const el = mountOverlay("gg-agents");
  el.onclick = (e) => {
    if (e.target === el) closeAgentSetup(); // backdrop
  };
  renderAgents();
  pushLayer("gg-agents", el, {
    onKey: (e) => {
      if (e.key === "Escape") {
        closeAgentSetup();
        return true;
      }
      return false;
    },
  });
  loadAgents();
}


function closeAgentSetup() {
  ag = null;
  closeLayer("gg-agents");
}


registerHelp({
  key: "agent setup",
  html:
    "☰ → <b>agent setup…</b>: the AI agents detected here and whether each carries gg's " +
    "<code>using-gg</code> skill — install or refresh one per row. Writes a single file at the " +
    "path shown, exactly as <code>gg init</code> does",
});

export { openAgentSetup };
