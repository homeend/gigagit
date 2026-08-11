// exttools.js — the External tools overlay: a READ-ONLY inventory of the
// configured [[tools.command]] blocks (every category, every frontend) and
// the catalog tools detected on this machine. Adding/editing stays in the
// TUI Settings wizard; this surface answers "what is configured, is it
// approved for this repo, and what could I add".
import { $, esc, getJSON } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";

let data = null; // last GET /api/exttools payload

async function openExtToolsView() {
  try {
    data = await getJSON("/api/exttools");
  } catch (e) {
    return;
  }
  renderExtTools();
  pushLayer("exttools", $("exttools"), {
    onKey: (e) => {
      if (e.key === "Escape") {
        closeLayer("exttools");
        e.preventDefault();
        return true;
      }
      return true; // read-only surface: swallow app hotkeys, nothing else
    },
  });
}

function cmdRowHTML(c) {
  const badges = [c.category, c.mode, c.per_file ? "per-file" : "", c.when_op, ...(c.frontends || [])]
    .filter(Boolean)
    .map((b) => `<span class="xbadge">${esc(b)}</span>`)
    .join("");
  const approval = c.approved
    ? `<span class="xok">approved</span>`
    : `<span class="snote">not yet approved</span>`;
  const problem = c.valid ? "" : `<div class="xproblem">${esc(c.problem)}</div>`;
  return `
    <div class="xcmd">
      <div class="srow"><span class="sval">${esc(c.name)}</span>${badges}${approval}</div>
      <div class="xcmdline">${esc(c.command)}</div>
      ${problem}
    </div>`;
}

function detRowHTML(d) {
  const tmpls = (d.templates || [])
    .map((t) => {
      const marks = [t.configured ? "configured ✓" : "not configured", t.opt_in ? "opt-in" : ""]
        .filter(Boolean)
        .join(" · ");
      return `<div class="srow xtmpl"><span class="xbadge">${esc(t.category)}</span><span class="sval">${esc(t.name)}</span><span class="snote">${esc(marks)}</span></div>`;
    })
    .join("");
  return `
    <div class="xdet">
      <div class="srow"><span class="sval">${esc(d.label)}</span><span class="snote">${esc(d.bin)}</span></div>
      ${tmpls}
    </div>`;
}

function renderExtTools() {
  if (!data) return;
  const cmds = (data.commands || []).map(cmdRowHTML).join("");
  const dets = (data.detected || []).map(detRowHTML).join("");
  $("exttools-box").innerHTML = `
    <h2>external tools</h2>
    <h3>configured commands</h3>
    ${cmds || '<div class="srow"><span class="snote">(none configured)</span></div>'}
    <h3>detected on this machine</h3>
    ${dets || '<div class="srow"><span class="snote">(no catalog tools found)</span></div>'}
    <div class="sfoot">read-only — add or edit in the TUI settings (,) → external tools; the wizard writes to ${esc(data.global_config_path || "the global config")} · a command runs only after you approve its full text, per repo · approvals are shared between the TUI and this page · esc closes</div>`;
}

$("exttools").addEventListener("click", (e) => {
  if (e.target === $("exttools")) closeLayer("exttools");
});

export { openExtToolsView };
