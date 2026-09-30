// exttools.js — the External tools overlay: an inventory of the configured
// [[tools.command]] blocks (every category, every frontend) and the catalog
// tools detected on this machine. Adding/editing stays in the TUI Settings
// wizard; the one write here answers a tool-template update offer (take the
// new template / keep mine) — nothing changes until the user picks one.
import { $, esc, getJSON, postJSON } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";

let data = null; // last GET /api/exttools payload
let openOffer = ""; // offer_id whose new template text is shown
let offerMsg = ""; // last answer's result line

async function loadExtTools() {
  data = await getJSON("/api/exttools");
  renderExtTools();
}

async function openExtToolsView() {
  openOffer = "";
  offerMsg = "";
  try {
    await loadExtTools();
  } catch (e) {
    return;
  }
  pushLayer("exttools", $("exttools"), {
    onKey: (e) => {
      if (e.key === "Escape") {
        closeLayer("exttools");
        e.preventDefault();
        return true;
      }
      return true; // swallow app hotkeys; the overlay's only actions are its buttons
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

function offerRowHTML(o) {
  if (o.status === "unsupported") {
    return `
    <div class="xoffer">
      <div class="srow"><span class="sval">${esc(o.name)}</span><span class="xbadge">${esc(o.category)}</span></div>
      <div class="xproblem">${esc(o.reason)}</div>
    </div>`;
  }
  const open = o.offer_id === openOffer;
  const mark = o.declined ? `<span class="snote">kept yours</span>` : `<span class="xwarn">update available</span>`;
  const panel = open
    ? `<div class="xreason">new template (written to ${esc(o.path)}):</div>
       <pre class="xnewtext">${esc(o.new_text)}</pre>`
    : "";
  const btns = open
    ? `<button data-offer-act="take" data-offer="${esc(o.offer_id)}">take new</button>
       <button data-offer-act="keep" data-offer="${esc(o.offer_id)}">keep mine</button>
       <button data-offer-act="close" data-offer="${esc(o.offer_id)}">later</button>`
    : `<button data-offer-act="review" data-offer="${esc(o.offer_id)}">review</button>`;
  return `
    <div class="xoffer">
      <div class="srow"><span class="sval">${esc(o.name)}</span><span class="xbadge">${esc(o.category)}</span>${mark}<span class="pbtns">${btns}</span></div>
      <div class="xreason">${esc(o.reason)}</div>
      ${panel}
    </div>`;
}

async function answerOffer(act, id) {
  if (act === "review" || act === "close") {
    openOffer = act === "review" ? id : "";
    renderExtTools();
    return;
  }
  try {
    await postJSON(act === "take" ? "/api/exttools/update" : "/api/exttools/keep", { offer_id: id });
    offerMsg = act === "take" ? "updated — the new template is in your config" : "kept yours — not offered again until the template changes";
  } catch (e) {
    offerMsg = String(e.message || e);
  }
  openOffer = "";
  try {
    await loadExtTools();
  } catch (e) {
    renderExtTools();
  }
}

function renderExtTools() {
  if (!data) return;
  const cmds = (data.commands || []).map(cmdRowHTML).join("");
  const dets = (data.detected || []).map(detRowHTML).join("");
  const offers = (data.template_offers || []).map(offerRowHTML).join("");
  const offersHTML =
    offers || offerMsg
      ? `<h3>template updates</h3>${offerMsg ? `<div class="srow"><span class="snote">${esc(offerMsg)}</span></div>` : ""}${offers}`
      : "";
  $("exttools-box").innerHTML = `
    <h2>external tools</h2>
    ${offersHTML}
    <h3>configured commands</h3>
    ${cmds || '<div class="srow"><span class="snote">(none configured)</span></div>'}
    <h3>detected on this machine</h3>
    ${dets || '<div class="srow"><span class="snote">(no catalog tools found)</span></div>'}
    <div class="sfoot">add or edit in the TUI settings (,) → external tools; the wizard writes to ${esc(data.global_config_path || "the global config")} · a command runs only after you approve its full text, per repo · approvals are shared between the TUI and this page · esc closes</div>`;
}

$("exttools").addEventListener("click", (e) => {
  if (e.target === $("exttools")) {
    closeLayer("exttools");
    return;
  }
  const b = e.target.closest("[data-offer-act]");
  if (b) answerOffer(b.dataset.offerAct, b.dataset.offer);
});

export { openExtToolsView };
