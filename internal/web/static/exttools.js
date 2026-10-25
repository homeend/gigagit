// exttools.js — the External tools overlay: the TUI's Settings → External
// tools wizard (detected tool × catalog template rows; space toggles, enter
// appends the checked new ones to the global config), then the template
// update offers (take the new template / keep mine) and an inventory of the
// configured [[tools.command]] blocks (every category, every frontend).
import { $, esc, getJSON, postJSON } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";
import { opLine } from "./ops.js";

let data = null; // last GET /api/exttools payload
let openOffer = ""; // offer_id whose new template text is shown
let offerMsg = ""; // last answer's result line
let sel = 0; // the wizard's cursor
let checked = []; // the wizard's boxes, by row
let writing = false; // a write in flight: enter does nothing more

// wizardRowText is the TUI's row: cursor, box, "<tool> — <category>: <name>"
// and a configured row's standing.
function wizardRowText(r, isChecked, focused) {
  let text = (focused ? "> " : "  ") + (isChecked ? "[x] " : "[ ] ") + r.label + " — " + r.category + ": " + r.name;
  if (r.configured) {
    if (r.status === "update" && !r.declined) text += " (update available — u)";
    else if (r.status === "unsupported") text += " (agent version unsupported)";
    else text += " (configured)";
  }
  return text;
}

// wizardAddRows names what enter writes: the checked rows not yet configured,
// by key (the server resolves each against a fresh detection).
function wizardAddRows(rows, isChecked) {
  return rows
    .filter((r, i) => isChecked[i] && !r.configured)
    .map((r) => ({ tool: r.tool, category: r.category, name: r.name }));
}

// wizardStatus is the TUI's status line after enter.
function wizardStatus(res, err) {
  if (err) return "external tools: " + err;
  if (!res.written) return "external tools: nothing to write (already configured or unchecked)";
  return "external tools: " + res.written + " command(s) written to " + res.path;
}

async function loadExtTools() {
  const prev = new Map(((data && data.wizard) || []).map((r, i) => [r.tool + "\0" + r.category + "\0" + r.name, checked[i]]));
  data = await getJSON("/api/exttools");
  const rows = data.wizard || [];
  // An offer answer re-reads the rows: the boxes the user set survive it.
  checked = rows.map((r) => {
    const k = r.tool + "\0" + r.category + "\0" + r.name;
    return prev.has(k) ? prev.get(k) : r.checked;
  });
  if (sel >= rows.length) sel = Math.max(0, rows.length - 1);
  renderExtTools();
}

async function openExtToolsView() {
  openOffer = "";
  offerMsg = "";
  data = null;
  sel = 0;
  checked = [];
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
      // A focused button (an offer's take / keep) keeps its own enter/space.
      const onButton = document.activeElement && document.activeElement.tagName === "BUTTON" && $("exttools-box").contains(document.activeElement);
      const rows = (data && data.wizard) || [];
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        if (rows.length) {
          sel = e.key === "ArrowDown" ? Math.min(rows.length - 1, sel + 1) : Math.max(0, sel - 1);
          renderExtTools();
          scrollWizardRow();
        }
        e.preventDefault();
      } else if (e.key === " " && !onButton) {
        if (rows.length) {
          checked[sel] = !checked[sel];
          renderExtTools();
        }
        e.preventDefault();
      } else if (e.key === "u" && !e.ctrlKey && !e.metaKey && !e.altKey) {
        reviewUpdate(sel);
        e.preventDefault();
      } else if (e.key === "Enter" && !onButton) {
        writeWizard();
        e.preventDefault();
      }
      return true; // swallow app hotkeys under the overlay
    },
  });
}

function scrollWizardRow() {
  const el = $("exttools-box").querySelector(".xwrow.sel");
  if (el) el.scrollIntoView({ block: "nearest" });
}

// reviewUpdate is the TUI's u: a row with an update offer (a declined one
// too — "keep mine" is never a trap) opens that offer.
function reviewUpdate(i) {
  const r = ((data && data.wizard) || [])[i];
  if (!r || r.status !== "update" || !r.offer_id) return;
  openOffer = r.offer_id;
  renderExtTools();
  const el = $("exttools-box").querySelector(`.xoffer[data-offer-id="${CSS.escape(r.offer_id)}"]`);
  if (el) el.scrollIntoView({ block: "nearest" });
}

// writeWizard is the TUI's enter: append, say so on the status line, leave.
async function writeWizard() {
  if (writing || !data) return;
  writing = true;
  try {
    const res = await postJSON("/api/exttools/add", { rows: wizardAddRows(data.wizard || [], checked) });
    opLine(wizardStatus(res));
  } catch (e) {
    opLine(wizardStatus(null, String(e.message || e)), true);
  } finally {
    writing = false;
  }
  closeLayer("exttools");
}

function wizardHTML() {
  const rows = data.wizard || [];
  if (!rows.length) {
    return `<div class="srow"><span class="snote">no known tools detected on this machine (looked for: claude, junie, meld)</span></div>`;
  }
  const list = rows
    .map((r, i) => {
      const text = wizardRowText(r, checked[i], i === sel);
      const m = text.match(/ \((configured|update available — u|agent version unsupported)\)$/);
      const base = m ? text.slice(0, m.index) : text;
      // "> [x] " is the box: a click on it toggles the row.
      return `<div class="xwrow${i === sel ? " sel" : ""}" data-wi="${i}"><span class="xwbox">${esc(base.slice(0, 6))}</span>${esc(base.slice(6))}${m ? `<span class="snote">${esc(m[0])}</span>` : ""}</div>`;
    })
    .join("");
  const r = rows[sel];
  const dest = r && r.configured ? "already configured — skipped on apply" : "writes to: " + (data.global_config_path || "");
  const detail = r
    ? `<div class="xwdetail"><div class="snote">${esc(dest)}</div><pre class="xcmdline">${esc(r.command || "")}</pre></div>`
    : "";
  return `<div class="xwlist">${list}</div>${detail}
    <div class="srow"><span class="snote khint">[space] toggle  [u] review update  [enter] write to global config  [esc] back</span><span class="pbtns"><button data-wact="write">write to global config</button></span></div>`;
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
    <div class="xoffer" data-offer-id="${esc(o.offer_id)}">
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
  const offers = (data.template_offers || []).map(offerRowHTML).join("");
  const offersHTML =
    offers || offerMsg
      ? `<h3>template updates</h3>${offerMsg ? `<div class="srow"><span class="snote">${esc(offerMsg)}</span></div>` : ""}${offers}`
      : "";
  $("exttools-box").innerHTML = `
    <h2>External tools — detected</h2>
    ${wizardHTML()}
    ${offersHTML}
    <h3>configured commands</h3>
    ${cmds || '<div class="srow"><span class="snote">(none configured)</span></div>'}
    <div class="sfoot">the wizard adds catalog commands for the tools found here to ${esc(data.global_config_path || "the global config")} — it never changes one you have · a command runs only after you approve its full text, per repo · approvals are shared between the TUI and this page · esc closes</div>`;
}

$("exttools").addEventListener("click", (e) => {
  if (e.target === $("exttools")) {
    closeLayer("exttools");
    return;
  }
  const w = e.target.closest("[data-wact]");
  if (w) {
    writeWizard();
    return;
  }
  const row = e.target.closest("[data-wi]");
  if (row) {
    // Anywhere on a row focuses it (the TUI's cursor); on its box it also
    // toggles (space).
    const i = Number(row.dataset.wi);
    sel = i;
    if (e.target.closest(".xwbox")) checked[i] = !checked[i];
    renderExtTools();
    return;
  }
  const b = e.target.closest("[data-offer-act]");
  if (b) answerOffer(b.dataset.offerAct, b.dataset.offer);
});

export { openExtToolsView };
